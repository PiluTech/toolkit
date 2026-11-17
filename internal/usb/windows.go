package usb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/privacidade"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Executáveis do Windows, SEMPRE por caminho absoluto (nunca pelo PATH). O arquivo compila em qualquer SO: só os comandos
// montados aqui dependem do Windows, e os testes usam um Runner falso.
const (
	regExe = `C:\Windows\System32\reg.exe`
	psExe  = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`

	cmdTimeout = 60 * time.Second
	pnpTimeout = 180 * time.Second

	editionKey      = `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	restrictionsDir = `HKLM\SOFTWARE\Policies\Microsoft\Windows\DeviceInstall`
	maxRegOutput    = 4 << 20
)

// windowsSystem implementa System sobre reg.exe e powershell.exe. Leituras vão pelo exec.ReadOnlyRunner (ReadOnly=true, nunca
// Elevate); escritas pelo exec.MutatingRunner (ReadOnly=false) e NUNCA levam Elevate: o administrador é conferido antes
// (IsAdmin) e o toolkit nunca eleva sozinho. Nenhum shell implícito: os argumentos vão como lista e os scripts do PowerShell
// são texto FIXO em ASCII, com só números e constantes validados interpolados.
type windowsSystem struct {
	read, mutate exec.Runner
	elevated     func() bool
	dir          string // pasta protegida esperada em ProtectDir/StageBinary/ExportReg (ProtectedDir; os testes trocam)
	stage        func(dir string) (StagedBinary, error)
}

// NewWindowsSystem cria o System real do Windows. elevated diz se o processo está elevado (nil = não).
func NewWindowsSystem(rn exec.Runner, elevated func() bool) System {
	return &windowsSystem{read: exec.ReadOnlyRunner(rn), mutate: exec.MutatingRunner(rn), elevated: elevated, dir: ProtectedDir, stage: stageSelf}
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrEntradaInvalida, fmt.Sprintf(format, a...))
}

// asciiOnly recusa script com qualquer byte fora de ASCII imprimível (ou quebra de linha/tab): é checado em TODO script antes
// de executar (e nos testes sobre as constantes).
func asciiOnly(s string) error {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 0x20 && c != '\n' && c != '\t') || c > 0x7e {
			return invalid("o script do PowerShell tem um byte fora de ASCII (posição %d)", i)
		}
	}
	return nil
}

func snippet(r exec.Result) string {
	s := strings.TrimSpace(string(r.Stderr))
	if s == "" {
		s = strings.TrimSpace(string(r.Stdout))
	}
	s = strings.Join(strings.Fields(textsafe.Line(s)), " ")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

func cmdFailed(c exec.Cmd, r exec.Result) error {
	exe := c.Name[strings.LastIndex(c.Name, `\`)+1:]
	return fmt.Errorf("%s terminou com código %d: %s", exe, r.ExitCode, snippet(r))
}

func (w *windowsSystem) readCmd(ctx context.Context, timeout time.Duration, name string, args ...string) (exec.Cmd, exec.Result, error) {
	c := exec.Cmd{Name: name, Args: args, ReadOnly: true, Timeout: timeout}
	r, err := w.read.Run(ctx, c)
	return c, r, err
}

func (w *windowsSystem) writeCmd(ctx context.Context, name string, args ...string) (exec.Cmd, exec.Result, error) {
	c := exec.Cmd{Name: name, Args: args, Timeout: cmdTimeout} // nunca Elevate
	r, err := w.mutate.Run(ctx, c)
	return c, r, err
}

func psArgs(script string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-Command", script}
}

func (w *windowsSystem) psRead(ctx context.Context, timeout time.Duration, script string) (exec.Result, error) {
	if err := asciiOnly(script); err != nil {
		return exec.Result{}, err
	}
	c, r, err := w.readCmd(ctx, timeout, psExe, psArgs(script)...)
	if err != nil {
		return r, fmt.Errorf("powershell: %w", err)
	}
	if r.ExitCode != 0 {
		return r, cmdFailed(c, r)
	}
	return r, nil
}

func (w *windowsSystem) psWrite(ctx context.Context, script string) error {
	if err := asciiOnly(script); err != nil {
		return err
	}
	c, r, err := w.writeCmd(ctx, psExe, psArgs(script)...)
	if err != nil {
		return fmt.Errorf("powershell: %w", err)
	}
	if r.ExitCode != 0 {
		return cmdFailed(c, r)
	}
	return nil
}

// IsAdmin devolve o callback injetado (nil = false).
func (w *windowsSystem) IsAdmin(context.Context) bool { return w.elevated != nil && w.elevated() }

// Edition lê EditionID (leitura; é a ÚNICA leitura fora da chave Restrictions). Só Professional, Enterprise e Education
// (e as variantes que começam assim) são aceitas pela política; Core (Home), Server, IoT e o resto são EditionOther/Home.
func (w *windowsSystem) Edition(ctx context.Context) (string, error) {
	c, r, err := w.readCmd(ctx, cmdTimeout, regExe, "query", editionKey, "/v", "EditionID")
	if err != nil {
		return "", fmt.Errorf("reg query: %w", err)
	}
	if r.ExitCode != 0 {
		return "", cmdFailed(c, r)
	}
	vals, _, err := parseRegList(string(r.Stdout), editionKey)
	if err != nil {
		return "", err
	}
	v, ok := vals["EditionID"]
	if !ok {
		return EditionOther, nil
	}
	id := strings.ToLower(v.Str)
	switch {
	case strings.HasPrefix(id, "core"):
		return EditionHome, nil
	case strings.HasPrefix(id, "professional"):
		return EditionPro, nil
	case strings.HasPrefix(id, "enterprise"):
		return EditionEnterprise, nil
	case strings.HasPrefix(id, "education"):
		return EditionEducation, nil
	}
	return EditionOther, nil
}

// --- registro ---

func keyOf(rel string) string {
	if rel == "" {
		return RestrictionsKey
	}
	return RestrictionsKey + `\` + rel
}

var reRegValue = regexp.MustCompile(`^ {4}(.+?) {4}(REG_[A-Z_]+)(?: {4}(.*))?$`)

// parseRegList lê a saída de `reg query <base> /s` (ou de um valor só). Devolve os valores (nome -> valor) da chave base
// quando não há /s (usado por Edition) e, para a árvore, é chamada por parseRegTree. Só REG_DWORD, REG_SZ e REG_EXPAND_SZ:
// outro tipo é erro (um valor que não dá para restaurar exatamente nunca é sobrescrito).
func parseRegList(out, base string) (map[string]RegValue, []string, error) {
	vals := map[string]RegValue{}
	var subs []string
	root := regRootName(base)
	cur := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			if !strings.EqualFold(line, root) && !strings.HasPrefix(strings.ToLower(line), strings.ToLower(root)+`\`) {
				return nil, nil, fmt.Errorf("cabeçalho de chave inesperado na saída do reg.exe")
			}
			cur = strings.TrimPrefix(line[len(root):], `\`)
			if cur != "" {
				subs = append(subs, cur)
			}
			continue
		}
		m := reRegValue.FindStringSubmatch(line)
		if m == nil {
			return nil, nil, fmt.Errorf("linha de valor em formato inesperado na saída do reg.exe")
		}
		name, typ, data := m[1], m[2], m[3]
		if name == "(Default)" {
			if data == "(value not set)" {
				continue
			}
			return nil, nil, fmt.Errorf("valor padrão preenchido na chave: tipo de valor que o toolkit não restaura")
		}
		var v RegValue
		switch typ {
		case "REG_DWORD":
			n, err := strconv.ParseUint(strings.TrimSpace(data), 0, 32)
			if err != nil {
				return nil, nil, fmt.Errorf("REG_DWORD ilegível")
			}
			v = DWORD(uint32(n))
		case "REG_SZ":
			v = String(data)
		case "REG_EXPAND_SZ":
			v = RegValue{Kind: privacidade.RegExpandString, Str: data}
		default:
			return nil, nil, fmt.Errorf("o valor %q é do tipo %s, que o toolkit não consegue restaurar exatamente: recusado", textsafe.Line(name), typ)
		}
		path := name
		if cur != "" {
			path = cur + `\` + name
		}
		vals[path] = v
	}
	return vals, subs, nil
}

// regRootName converte "HKLM\..." no nome longo que o reg.exe imprime.
func regRootName(key string) string {
	if rest, ok := strings.CutPrefix(key, `HKLM\`); ok {
		return `HKEY_LOCAL_MACHINE\` + rest
	}
	return key
}

// ReadTree: reg query <Restrictions> /s. Exit 0 = existe (parse). Exit 1 é AMBÍGUO (ausente ou acesso negado, texto
// traduzido): a ausência só é aceita quando PROVADA pela listagem de um ancestral; na dúvida é erro (nada é mudado).
func (w *windowsSystem) ReadTree(ctx context.Context) (RegTree, error) {
	c, r, err := w.readCmd(ctx, cmdTimeout, regExe, "query", RestrictionsKey, "/s")
	if err != nil {
		return RegTree{}, fmt.Errorf("reg query: %w", err)
	}
	if len(r.Stdout) > maxRegOutput {
		return RegTree{}, fmt.Errorf("saída do reg.exe grande demais")
	}
	switch r.ExitCode {
	case 0:
		vals, subs, err := parseRegList(string(r.Stdout), RestrictionsKey)
		if err != nil {
			return RegTree{}, err
		}
		tr := RegTree{KeyExists: true, Entries: vals}
		seen := map[string]bool{}
		for _, s := range subs {
			if !seen[s] {
				seen[s] = true
				tr.Subkeys = append(tr.Subkeys, s)
			}
		}
		return tr, nil
	case 1:
		if err := w.proveAbsent(ctx, c, r); err != nil {
			return RegTree{}, err
		}
		return RegTree{Entries: map[string]RegValue{}}, nil
	}
	return RegTree{}, cmdFailed(c, r)
}

// proveAbsent sobe pelos ancestrais de Restrictions até um que LISTE (exit 0) e prova pela listagem dele que a próxima
// subchave não existe. Se ela aparece, existe mas não abriu (acesso negado): erro.
func (w *windowsSystem) proveAbsent(ctx context.Context, c exec.Cmd, first exec.Result) error {
	cur, below := RestrictionsKey, ""
	for i := 0; i < 6; i++ {
		j := strings.LastIndex(cur, `\`)
		if j < 0 {
			break
		}
		below, cur = cur, cur[:j]
		_, pr, err := w.readCmd(ctx, cmdTimeout, regExe, "query", cur)
		if err != nil {
			return fmt.Errorf("reg query: %w", err)
		}
		if pr.ExitCode == 1 {
			continue
		}
		if pr.ExitCode != 0 {
			return cmdFailed(c, pr)
		}
		full := regRootName(cur) + `\` + below[strings.LastIndex(below, `\`)+1:]
		for _, line := range strings.Split(string(pr.Stdout), "\n") {
			if strings.EqualFold(strings.TrimSpace(line), full) {
				return fmt.Errorf("a chave Restrictions existe mas não pôde ser lida (acesso negado?): %s", snippet(first))
			}
		}
		return nil
	}
	return fmt.Errorf("não foi possível confirmar que a chave Restrictions não existe: %s", snippet(first))
}

func splitRel(rel string) (key, name string) {
	if sub, n, ok := strings.Cut(rel, `\`); ok {
		return keyOf(sub), n
	}
	return RestrictionsKey, rel
}

// RegWrite: reg.exe add <key> /v <name> /t REG_DWORD|REG_SZ /d <valor> /f. Caminho e valor conferidos contra a lista fechada
// ANTES de executar; nunca Elevate.
func (w *windowsSystem) RegWrite(ctx context.Context, rel string, v RegValue) error {
	if err := checkWrite(rel, v); err != nil {
		return err
	}
	key, name := splitRel(rel)
	typ, data := "REG_DWORD", strconv.FormatUint(uint64(v.DWORD), 10)
	if v.Kind == privacidade.RegString {
		typ, data = "REG_SZ", v.Str
	}
	c, r, err := w.writeCmd(ctx, regExe, "add", key, "/v", name, "/t", typ, "/d", data, "/f")
	if err != nil {
		return fmt.Errorf("reg add: %w", err)
	}
	if r.ExitCode != 0 {
		return cmdFailed(c, r)
	}
	return nil
}

// RegDelete: confere (leitura) que o valor existe e só então roda reg.exe delete <key> /v <name> /f.
func (w *windowsSystem) RegDelete(ctx context.Context, rel string) error {
	if err := checkRel(rel); err != nil {
		return err
	}
	tr, err := w.ReadTree(ctx)
	if err != nil {
		return err
	}
	if _, ok := tr.Entries[rel]; !ok {
		return nil
	}
	key, name := splitRel(rel)
	c, r, err := w.writeCmd(ctx, regExe, "delete", key, "/v", name, "/f")
	if err != nil {
		return fmt.Errorf("reg delete: %w", err)
	}
	if r.ExitCode != 0 {
		return cmdFailed(c, r)
	}
	return nil
}

// KeyDelete apaga a subchave (AllowDeviceIDs ou DenyDeviceClasses) ou a própria Restrictions SÓ quando está vazia (relida
// agora): reg delete <key> /f apagaria tudo o que houver dentro, inclusive política de outra origem.
func (w *windowsSystem) KeyDelete(ctx context.Context, rel string) error {
	if rel != "" && rel != nameAllow && rel != nameDeny {
		return invalid("subchave %q fora da lista fechada", printableRel(rel))
	}
	tr, err := w.ReadTree(ctx)
	if err != nil {
		return err
	}
	if !tr.KeyExists {
		return nil
	}
	if rel == "" {
		if len(tr.Entries) > 0 || len(tr.Subkeys) > 0 {
			return fmt.Errorf("a chave Restrictions não está vazia: não será apagada")
		}
	} else {
		if !hasString(tr.Subkeys, rel) {
			return nil
		}
		if hasEntriesUnder(tr, rel) {
			return fmt.Errorf("a subchave %s não está vazia: não será apagada", rel)
		}
	}
	c, r, err := w.writeCmd(ctx, regExe, "delete", keyOf(rel), "/f")
	if err != nil {
		return fmt.Errorf("reg delete: %w", err)
	}
	if r.ExitCode != 0 {
		return cmdFailed(c, r)
	}
	return nil
}

// ExportReg: reg.exe export <Restrictions> <arquivo> /y. O arquivo tem de ser o .reg da pasta protegida.
func (w *windowsSystem) ExportReg(ctx context.Context, file string) error {
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "/", `\`)) }
	if norm(file) != norm(w.dir)+`\`+strings.ToLower(RegFile) {
		return invalid("o .reg só pode ser exportado para a pasta protegida")
	}
	c, r, err := w.writeCmd(ctx, regExe, "export", RestrictionsKey, file, "/y")
	if err != nil {
		return fmt.Errorf("reg export: %w", err)
	}
	if r.ExitCode != 0 {
		return cmdFailed(c, r)
	}
	return nil
}

// --- pasta protegida ---

// Scripts FIXOS (ASCII, sem aspas duplas). A pasta é sempre a constante ProtectedDir.
// icaclsExe é por caminho ABSOLUTO (nunca pelo PATH: o icacls roda com privilégio).
const icaclsExe = `C:\Windows\System32\icacls.exe`

const protectDirScript = `$ErrorActionPreference='Stop'
$d='` + ProtectedDir + `'
if(Test-Path -LiteralPath $d){ $i=Get-Item -LiteralPath $d -Force; if(($i.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){ throw 'reparse point' }; foreach($c in @(Get-ChildItem -LiteralPath $d -Force)){ if(($c.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){ throw 'reparse point dentro da pasta' } } } else { New-Item -ItemType Directory -Path $d | Out-Null }
$sys=New-Object System.Security.Principal.SecurityIdentifier('S-1-5-18')
$adm=New-Object System.Security.Principal.SecurityIdentifier('S-1-5-32-544')
$acl=New-Object System.Security.AccessControl.DirectorySecurity
$acl.SetOwner($adm)
$acl.SetAccessRuleProtection($true,$false)
foreach($s in @($sys,$adm)){ $acl.AddAccessRule((New-Object System.Security.AccessControl.FileSystemAccessRule($s,'FullControl','ContainerInherit,ObjectInherit','None','Allow'))) }
Set-Acl -LiteralPath $d -AclObject $acl
foreach($c in @(Get-ChildItem -LiteralPath $d -Force)){ & '` + icaclsExe + `' $c.FullName /reset /t /c /l | Out-Null; if($LASTEXITCODE -ne 0){ throw 'icacls reset' }; & '` + icaclsExe + `' $c.FullName /setowner '*S-1-5-32-544' /t /c /l | Out-Null; if($LASTEXITCODE -ne 0){ throw 'icacls setowner' } }
[IO.File]::Delete((Join-Path $d '` + RegFile + `'))
`

const readDirScript = `$ErrorActionPreference='Stop'
$d='` + ProtectedDir + `'
$i=Get-Item -LiteralPath $d -Force
$ch=@()
foreach($c in @(Get-ChildItem -LiteralPath $d -Force)){ $ch+=[pscustomobject]@{name=[string]$c.Name;reparse=(($c.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0);sddl=(Get-Acl -LiteralPath $c.FullName).GetSecurityDescriptorSddlForm('All')} }
[pscustomobject]@{reparse=(($i.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0);sddl=(Get-Acl -LiteralPath $d).GetSecurityDescriptorSddlForm('All');children=$ch} | ConvertTo-Json -Compress -Depth 3
`

type childInfo struct {
	Name    string `json:"name"`
	Reparse bool   `json:"reparse"`
	SDDL    string `json:"sddl"`
}

type dirInfo struct {
	Reparse  bool        `json:"reparse"`
	SDDL     string      `json:"sddl"`
	Children []childInfo `json:"children"`
}

// checkChildSDDL confere a ACL de um FILHO da pasta protegida depois do icacls /reset e /setowner: dono Administradores,
// DACL que HERDA (sem P) e só duas ACEs de permissão FullControl herdadas (flag ID) para SYSTEM e Administradores. Um
// arquivo plantado por usuário comum, com ACL própria dele, é recusado. (O /t do icacls reseta a subárvore; a conferência
// por SDDL cobre o primeiro nível, que é onde ficam o backup, o .reg e a cópia do binário.)
func checkChildSDDL(name, sddl string) error {
	bad := func(why string) error {
		return fmt.Errorf("a ACL de %q na pasta protegida não é a esperada: %s", textsafe.Line(name), why)
	}
	o := reSDDLOwner.FindStringSubmatch(sddl)
	if o == nil || (o[1] != "BA" && o[1] != "S-1-5-32-544") {
		return bad("o dono não é Administradores")
	}
	d := reSDDLDACL.FindStringSubmatch(sddl)
	if d == nil {
		return bad("sem DACL")
	}
	if strings.Contains(d[1], "P") {
		return bad("a DACL não herda da pasta")
	}
	aces := regexp.MustCompile(`\(([^)]*)\)`).FindAllStringSubmatch(d[2], -1)
	got := map[string]bool{}
	for _, a := range aces {
		f := strings.Split(a[1], ";")
		if len(f) != 6 || f[0] != "A" || f[2] != "FA" || !strings.Contains(f[1], "ID") {
			return bad("há uma ACE que não é de FullControl herdado")
		}
		switch f[5] {
		case "SY", "S-1-5-18":
			got["SY"] = true
		case "BA", "S-1-5-32-544":
			got["BA"] = true
		default:
			return bad("há uma ACE para " + f[5])
		}
	}
	if len(aces) != 2 || !got["SY"] || !got["BA"] {
		return bad("deveria haver só SYSTEM e Administradores")
	}
	return nil
}

var (
	reSDDLOwner = regexp.MustCompile(`O:([A-Z]{2}|S-[0-9-]+)`)
	reSDDLDACL  = regexp.MustCompile(`D:([A-Z]*)((?:\([^)]*\))*)`)
)

// checkDirSDDL confere que a ACL é EXATAMENTE: dono Administradores (BA), DACL protegida (sem herança) e só duas ACEs de
// permissão FullControl (FA) com herança de objeto e contêiner (OICI) para SYSTEM (SY) e Administradores (BA). Nenhuma ACE
// para Users, Authenticated Users ou Everyone. (A forma exata do SDDL que o Windows devolve é A VERIFICAR em VM.)
func checkDirSDDL(sddl string) error {
	bad := func(why string) error { return fmt.Errorf("a ACL da pasta protegida não é a esperada: %s", why) }
	o := reSDDLOwner.FindStringSubmatch(sddl)
	if o == nil || (o[1] != "BA" && o[1] != "S-1-5-32-544") {
		return bad("o dono não é Administradores")
	}
	d := reSDDLDACL.FindStringSubmatch(sddl)
	if d == nil {
		return bad("sem DACL")
	}
	if !strings.Contains(d[1], "P") {
		return bad("a herança não foi desligada")
	}
	aces := regexp.MustCompile(`\(([^)]*)\)`).FindAllStringSubmatch(d[2], -1)
	got := map[string]bool{}
	for _, a := range aces {
		f := strings.Split(a[1], ";")
		if len(f) != 6 || f[0] != "A" || f[2] != "FA" || f[1] != "OICI" {
			return bad("há uma ACE que não é de FullControl herdável")
		}
		switch f[5] {
		case "SY", "S-1-5-18":
			got["SY"] = true
		case "BA", "S-1-5-32-544":
			got["BA"] = true
		default:
			return bad("há uma ACE para " + f[5])
		}
	}
	if len(aces) != 2 || !got["SY"] || !got["BA"] {
		return bad("deveria haver só SYSTEM e Administradores")
	}
	return nil
}

// ProtectDir cria e protege a pasta e RELÊ a ACL (SDDL, que não depende do idioma); ACL diferente da esperada é erro.
func (w *windowsSystem) ProtectDir(ctx context.Context, dir string) error {
	if dir != w.dir {
		return invalid("pasta de backup inesperada")
	}
	if err := w.psWrite(ctx, protectDirScript); err != nil {
		return fmt.Errorf("não deu para criar e proteger a pasta: %w", err)
	}
	r, err := w.psRead(ctx, cmdTimeout, readDirScript)
	if err != nil {
		return fmt.Errorf("não deu para reler a ACL da pasta: %w", err)
	}
	var info dirInfo
	if err := json.Unmarshal(r.Stdout, &info); err != nil {
		return fmt.Errorf("leitura da ACL ilegível")
	}
	if info.Reparse {
		return fmt.Errorf("a pasta protegida é um ponto de reparse (link): recusada")
	}
	if err := checkDirSDDL(info.SDDL); err != nil {
		return err
	}
	// o icacls /c segue depois de um erro e o código de saída final não é garantido: relê cada filho e confere
	for _, c := range info.Children {
		if c.Reparse {
			return fmt.Errorf("%q na pasta protegida é um ponto de reparse (link): recusado", textsafe.Line(c.Name))
		}
		if err := checkChildSDDL(c.Name, c.SDDL); err != nil {
			return err
		}
	}
	return nil
}

// stageSelf copia o toolkit em execução para dir e confere o SHA-256 da cópia.
func stageSelf(dir string) (StagedBinary, error) {
	src, err := os.Executable()
	if err != nil {
		return StagedBinary{}, err
	}
	return copyAndHash(src, dir)
}

func copyAndHash(src, dir string) (StagedBinary, error) {
	in, err := os.Open(src)
	if err != nil {
		return StagedBinary{}, err
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(dir, ".revert-*.tmp")
	if err != nil {
		return StagedBinary{}, err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), in); err != nil {
		_ = tmp.Close()
		return StagedBinary{}, err
	}
	if err := tmp.Close(); err != nil {
		return StagedBinary{}, err
	}
	dst := filepath.Join(dir, RevertBinary)
	if err := os.Rename(name, dst); err != nil {
		return StagedBinary{}, err
	}
	srcSum := hex.EncodeToString(h.Sum(nil))
	out, err := os.Open(dst)
	if err != nil {
		return StagedBinary{}, err
	}
	defer func() { _ = out.Close() }()
	h2 := sha256.New()
	if _, err := io.Copy(h2, out); err != nil {
		return StagedBinary{}, err
	}
	return StagedBinary{Path: dir + `\` + RevertBinary, SourceSHA: srcSum, CopySHA: hex.EncodeToString(h2.Sum(nil))}, nil
}

// StageBinary copia o toolkit para a pasta protegida (a tarefa de reversão executa essa cópia, com ACL só de SYSTEM e
// Administradores, nunca um binário editável por usuário comum).
func (w *windowsSystem) StageBinary(_ context.Context, dir string) (StagedBinary, error) {
	if dir != w.dir {
		return StagedBinary{}, invalid("pasta de backup inesperada")
	}
	return w.stage(dir)
}

// --- tarefa agendada ---

func armTaskScript(minutes int) string {
	return `$ErrorActionPreference='Stop'
$a=New-ScheduledTaskAction -Execute '` + taskExe() + `' -Argument '` + RevertArgs + `'
$t=New-ScheduledTaskTrigger -Once -At ((Get-Date).AddMinutes(` + strconv.Itoa(minutes) + `)) -RepetitionInterval (New-TimeSpan -Minutes 1) -RepetitionDuration (New-TimeSpan -Minutes 30)
$p=New-ScheduledTaskPrincipal -UserId 'S-1-5-18' -LogonType ServiceAccount -RunLevel Highest
$s=New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Minutes 5) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName '` + TaskName + `' -TaskPath '` + TaskPath + `' -Action $a -Trigger $t -Principal $p -Settings $s | Out-Null
`
}

const readTaskScript = `$ErrorActionPreference='Stop'
$t=Get-ScheduledTask -TaskName '` + TaskName + `' -TaskPath '` + TaskPath + `' -ErrorAction SilentlyContinue
if(-not $t){ [pscustomobject]@{exists=$false} | ConvertTo-Json -Compress; exit 0 }
$i=Get-ScheduledTaskInfo -TaskName '` + TaskName + `' -TaskPath '` + TaskPath + `'
$a=@($t.Actions)
$n=$null
if($i.NextRunTime -and $i.NextRunTime.Year -gt 2000){ $n=$i.NextRunTime.ToUniversalTime().ToString('o') }
[pscustomobject]@{exists=$true;enabled=($t.State.ToString() -ne 'Disabled');exe=[string]$a[0].Execute;args=[string]$a[0].Arguments;user=[string]$t.Principal.UserId;next=$n;actions=$a.Count;triggers=@($t.Triggers).Count} | ConvertTo-Json -Compress
`

const deleteTaskScript = `$ErrorActionPreference='Stop'
Unregister-ScheduledTask -TaskName '` + TaskName + `' -TaskPath '` + TaskPath + `' -Confirm:$false
`

// ArmTask cria a ÚNICA tarefa do toolkit: SYSTEM, execução única daqui a Minutes, "executar assim que possível" se o horário
// passar (StartWhenAvailable), comando fixo. Programa, argumento e prazo são conferidos ANTES de montar o script. Sem -Force:
// uma tarefa de mesmo nome já existente é erro, nunca sobrescrita.
func (w *windowsSystem) ArmTask(ctx context.Context, spec TaskSpec) error {
	if !strings.EqualFold(spec.Exe, taskExe()) || spec.Args != RevertArgs {
		return invalid("a tarefa só pode executar o binário da pasta protegida com o argumento fixo")
	}
	if spec.Minutes < PrazoMinimo || spec.Minutes > PrazoMaximo {
		return fmt.Errorf("%w: %d minutos", ErrPrazo, spec.Minutes)
	}
	return w.psWrite(ctx, armTaskScript(spec.Minutes))
}

type taskJSON struct {
	Exists   bool    `json:"exists"`
	Enabled  bool    `json:"enabled"`
	Exe      string  `json:"exe"`
	Args     string  `json:"args"`
	User     string  `json:"user"`
	Next     *string `json:"next"`
	Actions  int     `json:"actions"`
	Triggers int     `json:"triggers"`
}

// ReadTask lê a tarefa (Get-ScheduledTask e Get-ScheduledTaskInfo, em JSON: não depende do idioma). Mais de uma ação ou
// gatilho vira um programa/comando que nunca confere (a conferência do chamador recusa).
func (w *windowsSystem) ReadTask(ctx context.Context) (TaskInfo, error) {
	r, err := w.psRead(ctx, cmdTimeout, readTaskScript)
	if err != nil {
		return TaskInfo{}, err
	}
	var tj taskJSON
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(string(r.Stdout)), "\ufeff")), &tj); err != nil {
		return TaskInfo{}, fmt.Errorf("leitura da tarefa ilegível")
	}
	if !tj.Exists {
		return TaskInfo{}, nil
	}
	ti := TaskInfo{Exists: true, Enabled: tj.Enabled, Exe: textsafe.Line(tj.Exe), Args: textsafe.Line(tj.Args), User: textsafe.Line(tj.User)}
	if tj.Actions != 1 || tj.Triggers != 1 {
		ti.Exe = "(a tarefa tem " + strconv.Itoa(tj.Actions) + " ação(ões) e " + strconv.Itoa(tj.Triggers) + " gatilho(s))"
	}
	if tj.Next != nil {
		t, err := time.Parse(time.RFC3339Nano, *tj.Next)
		if err != nil {
			return TaskInfo{}, fmt.Errorf("próximo disparo ilegível")
		}
		ti.Next = t.UTC()
	}
	return ti, nil
}

// DeleteTask remove a tarefa do toolkit (nome fixo).
func (w *windowsSystem) DeleteTask(ctx context.Context) error {
	return w.psWrite(ctx, deleteTaskScript)
}

// --- PnP ---

const readDevicesScript = `$ErrorActionPreference='Stop'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding $false
$o=@()
foreach($x in @(Get-PnpDevice -PresentOnly)){
$p=@(Get-PnpDeviceProperty -InstanceId $x.InstanceId -KeyName 'DEVPKEY_Device_Parent','DEVPKEY_Device_HardwareIds','DEVPKEY_Device_CompatibleIds' -ErrorAction SilentlyContinue)
$h=@{}
foreach($r in $p){ $h[$r.KeyName]=$r.Data }
$n=[string]$x.FriendlyName
if(-not $n){ $n=[string]$x.Name }
$o+=[pscustomobject]@{InstanceId=[string]$x.InstanceId;Parent=[string]$h['DEVPKEY_Device_Parent'];Name=$n;Class=[string]$x.Class;ClassGuid=[string]$x.ClassGuid;HardwareIds=@($h['DEVPKEY_Device_HardwareIds']);CompatibleIds=@($h['DEVPKEY_Device_CompatibleIds'])}
}
[pscustomobject]@{count=$o.Count;devices=$o} | ConvertTo-Json -Compress -Depth 4
`

// Devices lê o PnP (só leitura): dispositivos presentes com pai, classe e IDs, em JSON com a contagem (para detectar leitura
// cortada). Formato e disponibilidade por versão do Windows: A VERIFICAR em VM.
func (w *windowsSystem) Devices(ctx context.Context) ([]Device, error) {
	r, err := w.psRead(ctx, pnpTimeout, readDevicesScript)
	if err != nil {
		return nil, err
	}
	return ParseDevices(r.Stdout)
}
