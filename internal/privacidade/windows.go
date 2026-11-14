package privacidade

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/PiluTech/toolkit/internal/exec"
)

// Executáveis do Windows, SEMPRE por caminho absoluto (nunca pelo PATH). O arquivo compila em qualquer SO: só os
// comandos montados aqui dependem do Windows, e os testes usam um Runner falso.
const (
	regExe      = `C:\Windows\System32\reg.exe`
	scExe       = `C:\Windows\System32\sc.exe`
	schtasksExe = `C:\Windows\System32\schtasks.exe`

	cmdTimeout = 60 * time.Second

	editionKey = `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion`
)

// Códigos de saída do sc.exe (erros do Win32) tratados como sucesso ou como "não existe".
const (
	scServiceDoesNotExist = 1060
	scServiceNotActive    = 1062 // stop de serviço já parado
	scServiceAlreadyRuns  = 1056 // start de serviço já em execução
)

// ErrEntradaInvalida indica chave, nome, caminho, tipo de início ou valor fora do formato seguro. É devolvido
// ANTES de executar qualquer comando.
var ErrEntradaInvalida = errors.New("entrada inválida")

var (
	reKey     = regexp.MustCompile(`^HK(CU|LM)\\[A-Za-z0-9_\\ .{}()-]+$`)
	reName    = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.-]*$`)
	reTask    = regexp.MustCompile(`^\\[A-Za-z0-9_\\ .()-]+$`)
	reStartTy = regexp.MustCompile(`(?m)^\s*START_TYPE\s*:\s*(\d+)(.*)$`)
	reState   = regexp.MustCompile(`(?m)^\s*STATE\s*:\s*(\d+)`)
	reEnabled = regexp.MustCompile(`(?i)<Enabled>\s*(true|false)\s*</Enabled>`)
	reTaskTag = regexp.MustCompile(`(?i)<Task[\s>]`)
	reSettins = regexp.MustCompile(`(?is)<Settings\b[^>]*>(.*?)</Settings>`)
)

// windowsSystem implementa System sobre reg.exe, sc.exe e schtasks.exe.
//
// Leituras vão pelo exec.ReadOnlyRunner (ReadOnly=true, nunca Elevate); escritas pelo exec.MutatingRunner
// (ReadOnly=false; Elevate=true em HKLM, serviços e tarefas agendadas; HKCU sem Elevate). Nenhum shell implícito:
// os argumentos vão como lista, depois de validados.
type windowsSystem struct {
	read     exec.Runner
	mutate   exec.Runner
	elevated func() bool
}

// NewWindowsSystem cria o System real do Windows. elevated diz se o processo está elevado (nil = não).
func NewWindowsSystem(rn exec.Runner, elevated func() bool) System {
	return &windowsSystem{read: exec.ReadOnlyRunner(rn), mutate: exec.MutatingRunner(rn), elevated: elevated}
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrEntradaInvalida, fmt.Sprintf(format, a...))
}

// segmentsOK recusa segmento vazio, com espaço nas pontas, com espaços repetidos ou com "..".
func segmentsOK(s string) bool {
	for seg := range strings.SplitSeq(s, `\`) {
		if seg == "" || seg != strings.TrimSpace(seg) || strings.Contains(seg, "  ") || strings.Contains(seg, "..") {
			return false
		}
	}
	return true
}

func checkKey(key string) error {
	if !reKey.MatchString(key) || !segmentsOK(key) {
		return invalid("chave do registro %q fora do formato HKCU\\... ou HKLM\\... (só letras, números e _ \\ espaço . { } ( ) -)", key)
	}
	return nil
}

func checkName(kind, name string) error {
	if !reName.MatchString(name) || strings.Contains(name, "..") {
		return invalid("%s %q inválido (só letras, números, _ . e -, sem '..' e sem começar com '-')", kind, name)
	}
	return nil
}

func checkTaskPath(path string) error {
	if !reTask.MatchString(path) || !segmentsOK(path[1:]) {
		return invalid("caminho de tarefa agendada %q inválido (deve começar por \\ e ter só letras, números, _ \\ espaço . ( ) -)", path)
	}
	return nil
}

func checkRegString(s string) error {
	if strings.HasSuffix(s, `\`) {
		return invalid("valor de texto não pode terminar em barra invertida")
	}
	for _, r := range s {
		if unicode.IsControl(r) || strings.ContainsRune(`"%^&|<>`, r) {
			return invalid("valor de texto com caractere não permitido (U+%04X)", r)
		}
	}
	return nil
}

func isHKLM(key string) bool { return strings.HasPrefix(key, `HKLM\`) }

// snippet resume a saída de erro de um comando: sem controles, uma linha, até 200 caracteres.
func snippet(r exec.Result) string {
	s := strings.TrimSpace(string(r.Stderr))
	if s == "" {
		s = strings.TrimSpace(string(r.Stdout))
	}
	s = strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return ' '
		}
		return c
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

func cmdFailed(c exec.Cmd, r exec.Result) error {
	exe := c.Name[strings.LastIndex(c.Name, `\`)+1:]
	return fmt.Errorf("%s terminou com código %d: %s", exe, r.ExitCode, snippet(r))
}

func (w *windowsSystem) readCmd(ctx context.Context, name string, args ...string) (exec.Cmd, exec.Result, error) {
	c := exec.Cmd{Name: name, Args: args, ReadOnly: true, Timeout: cmdTimeout}
	r, err := w.read.Run(ctx, c)
	return c, r, err
}

func (w *windowsSystem) writeCmd(ctx context.Context, elevate bool, name string, args ...string) (exec.Cmd, exec.Result, error) {
	c := exec.Cmd{Name: name, Args: args, Elevate: elevate, Timeout: cmdTimeout}
	r, err := w.mutate.Run(ctx, c)
	return c, r, err
}

// IsAdmin devolve o callback injetado (nil = false).
func (w *windowsSystem) IsAdmin(context.Context) bool { return w.elevated != nil && w.elevated() }

// Edition lê HKLM\...\CurrentVersion\EditionID. Sem o valor, EditionOther.
func (w *windowsSystem) Edition(ctx context.Context) (string, error) {
	v, ok, err := w.RegRead(ctx, editionKey, "EditionID")
	if err != nil {
		return "", err
	}
	if !ok {
		return EditionOther, nil
	}
	id := strings.ToLower(v.Str)
	switch {
	case strings.HasPrefix(id, "core"):
		return EditionHome, nil
	case strings.HasPrefix(id, "professional"):
		return EditionPro, nil
	case strings.HasPrefix(id, "enterprise"), strings.HasPrefix(id, "server"):
		return EditionEnterprise, nil
	case strings.HasPrefix(id, "education"):
		return EditionEducation, nil
	}
	return EditionOther, nil
}

// RegRead: reg.exe query <key> /v <name>. Exit 1 = valor ou chave inexistente (não é erro; limite: o reg.exe usa
// o mesmo código 1 para outras falhas, e a mensagem é traduzida, então não dá para separar). Outro código = erro.
// O parse procura a linha que começa por espaços + nome + espaços + REG_xxx; o cabeçalho (qualquer idioma) é ignorado.
func (w *windowsSystem) RegRead(ctx context.Context, key, name string) (RegValue, bool, error) {
	if err := checkKey(key); err != nil {
		return RegValue{}, false, err
	}
	if err := checkName("nome do valor", name); err != nil {
		return RegValue{}, false, err
	}
	c, r, err := w.readCmd(ctx, regExe, "query", key, "/v", name)
	if err != nil {
		return RegValue{}, false, fmt.Errorf("reg query %s: %w", key, err)
	}
	if r.ExitCode == 1 {
		return RegValue{}, false, nil
	}
	if r.ExitCode != 0 {
		return RegValue{}, false, cmdFailed(c, r)
	}
	v, found, err := parseRegQuery(string(r.Stdout), name)
	if err != nil {
		return RegValue{}, false, fmt.Errorf("reg query %s\\%s: %w", key, name, err)
	}
	if !found {
		return RegValue{}, false, fmt.Errorf("reg query %s\\%s: valor não encontrado na saída", key, name)
	}
	return v, true, nil
}

func parseRegQuery(out, name string) (RegValue, bool, error) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r\n \t")
		t := strings.TrimLeft(line, " \t")
		if len(t) == len(line) || len(t) <= len(name) || !strings.EqualFold(t[:len(name)], name) {
			continue
		}
		rest := t[len(name):]
		if rest[0] != ' ' && rest[0] != '\t' {
			continue
		}
		typ, data, _ := strings.Cut(strings.TrimLeft(rest, " \t"), " ")
		if !strings.HasPrefix(typ, "REG_") {
			continue
		}
		data = strings.TrimSpace(data)
		switch typ {
		case "REG_DWORD":
			n, err := strconv.ParseUint(data, 0, 32)
			if err != nil {
				return RegValue{}, false, fmt.Errorf("REG_DWORD ilegível %q", data)
			}
			return DWORD(uint32(n)), true, nil
		case "REG_SZ", "REG_EXPAND_SZ":
			return RegValue{Kind: RegString, Str: data}, true, nil
		}
		return RegValue{}, false, fmt.Errorf("tipo %s não suportado", typ)
	}
	return RegValue{}, false, nil
}

// RegWrite: reg.exe add <key> /v <name> /t REG_DWORD|REG_SZ /d <valor> /f. Elevate só em HKLM.
func (w *windowsSystem) RegWrite(ctx context.Context, key, name string, v RegValue) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if err := checkName("nome do valor", name); err != nil {
		return err
	}
	var typ, data string
	switch v.Kind {
	case RegDWORD:
		typ, data = "REG_DWORD", strconv.FormatUint(uint64(v.DWORD), 10)
	case RegString:
		if err := checkRegString(v.Str); err != nil {
			return err
		}
		typ, data = "REG_SZ", v.Str
	default:
		return invalid("tipo de valor do registro desconhecido (%d)", int(v.Kind))
	}
	c, r, err := w.writeCmd(ctx, isHKLM(key), regExe, "add", key, "/v", name, "/t", typ, "/d", data, "/f")
	if err != nil {
		return fmt.Errorf("reg add %s: %w", key, err)
	}
	if r.ExitCode != 0 {
		return cmdFailed(c, r)
	}
	return nil
}

// RegDelete: confere com reg query (leitura) se o valor existe e só então roda reg.exe delete <key> /v <name> /f.
// Valor inexistente = sucesso silencioso, sem executar o delete (o exit 1 do delete é ambíguo, a mensagem é traduzida).
func (w *windowsSystem) RegDelete(ctx context.Context, key, name string) error {
	_, exists, err := w.RegRead(ctx, key, name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	c, r, err := w.writeCmd(ctx, isHKLM(key), regExe, "delete", key, "/v", name, "/f")
	if err != nil {
		return fmt.Errorf("reg delete %s: %w", key, err)
	}
	if r.ExitCode != 0 {
		return cmdFailed(c, r)
	}
	return nil
}

// ServiceRead: sc.exe qc <nome> (START_TYPE, pelo número; DELAYED marca delayed-auto) e sc.exe query <nome> (STATE;
// 4 = running). Exit 1060 = serviço inexistente.
func (w *windowsSystem) ServiceRead(ctx context.Context, name string) (ServiceState, error) {
	if err := checkName("nome do serviço", name); err != nil {
		return ServiceState{}, err
	}
	c, r, err := w.readCmd(ctx, scExe, "qc", name)
	if err != nil {
		return ServiceState{}, fmt.Errorf("sc qc %s: %w", name, err)
	}
	if r.ExitCode == scServiceDoesNotExist {
		return ServiceState{}, nil
	}
	if r.ExitCode != 0 {
		return ServiceState{}, cmdFailed(c, r)
	}
	m := reStartTy.FindStringSubmatch(string(r.Stdout))
	if m == nil {
		return ServiceState{}, fmt.Errorf("sc qc %s: START_TYPE não encontrado na saída", name)
	}
	st := ServiceState{Exists: true}
	switch m[1] {
	case "2":
		st.StartType = "automatic"
		if strings.Contains(strings.ToUpper(m[2]), "DELAYED") {
			st.StartType = "delayed-auto"
		}
	case "3":
		st.StartType = "manual"
	case "4":
		st.StartType = "disabled"
	default:
		return ServiceState{}, fmt.Errorf("sc qc %s: tipo de início %s não suportado", name, m[1])
	}

	c, r, err = w.readCmd(ctx, scExe, "query", name)
	if err != nil {
		return ServiceState{}, fmt.Errorf("sc query %s: %w", name, err)
	}
	if r.ExitCode == scServiceDoesNotExist {
		return ServiceState{}, nil // removido entre as duas leituras
	}
	if r.ExitCode != 0 {
		return ServiceState{}, cmdFailed(c, r)
	}
	s := reState.FindStringSubmatch(string(r.Stdout))
	if s == nil {
		return ServiceState{}, fmt.Errorf("sc query %s: STATE não encontrado na saída", name)
	}
	st.Running = s[1] == "4"
	return st, nil
}

// ServiceConfigure: sc.exe config <nome> start= auto|delayed-auto|demand|disabled ("start=" e o valor são
// argumentos separados). Sempre com Elevate.
func (w *windowsSystem) ServiceConfigure(ctx context.Context, name, startType string) error {
	if err := checkName("nome do serviço", name); err != nil {
		return err
	}
	var v string
	switch startType {
	case "automatic":
		v = "auto"
	case "delayed-auto":
		v = "delayed-auto"
	case "manual":
		v = "demand"
	case "disabled":
		v = "disabled"
	default:
		return invalid("tipo de início %q desconhecido (use automatic, delayed-auto, manual ou disabled)", startType)
	}
	c, r, err := w.writeCmd(ctx, true, scExe, "config", name, "start=", v)
	if err != nil {
		return fmt.Errorf("sc config %s: %w", name, err)
	}
	if r.ExitCode != 0 {
		return cmdFailed(c, r)
	}
	return nil
}

// ServiceStop: sc.exe stop <nome> (1062, já parado, é sucesso). Com Elevate.
func (w *windowsSystem) ServiceStop(ctx context.Context, name string) error {
	return w.scAction(ctx, "stop", name, scServiceNotActive)
}

// ServiceStart: sc.exe start <nome> (1056, já em execução, é sucesso). Com Elevate.
func (w *windowsSystem) ServiceStart(ctx context.Context, name string) error {
	return w.scAction(ctx, "start", name, scServiceAlreadyRuns)
}

func (w *windowsSystem) scAction(ctx context.Context, verb, name string, okCode int) error {
	if err := checkName("nome do serviço", name); err != nil {
		return err
	}
	c, r, err := w.writeCmd(ctx, true, scExe, verb, name)
	if err != nil {
		return fmt.Errorf("sc %s %s: %w", verb, name, err)
	}
	if r.ExitCode != 0 && r.ExitCode != okCode {
		return cmdFailed(c, r)
	}
	return nil
}

// TaskRead: schtasks.exe /Query /TN <caminho> /XML. O XML independe de idioma; só <Settings><Enabled> conta
// (os gatilhos também têm <Enabled>). Exit 1 = tarefa inexistente, EXCETO quando a mensagem indica acesso negado
// (reconhecida em vários idiomas), que é erro. Qualquer outro código também é erro.
func (w *windowsSystem) TaskRead(ctx context.Context, path string) (TaskState, error) {
	if err := checkTaskPath(path); err != nil {
		return TaskState{}, err
	}
	c, r, err := w.readCmd(ctx, schtasksExe, "/Query", "/TN", path, "/XML")
	if err != nil {
		return TaskState{}, fmt.Errorf("schtasks /Query %s: %w", path, err)
	}
	if r.ExitCode == 1 {
		low := strings.ToLower(decodeText(r.Stderr) + " " + decodeText(r.Stdout))
		for _, marca := range []string{"denied", "negado", "denegado", "verweigert", "refus"} {
			if strings.Contains(low, marca) {
				return TaskState{}, cmdFailed(c, r)
			}
		}
		return TaskState{}, nil
	}
	if r.ExitCode != 0 {
		return TaskState{}, cmdFailed(c, r)
	}
	xml := decodeText(r.Stdout)
	if !reTaskTag.MatchString(xml) {
		return TaskState{}, fmt.Errorf("schtasks /Query %s: saída sem XML de tarefa", path)
	}
	st := TaskState{Exists: true, Enabled: true}
	if s := reSettins.FindStringSubmatch(xml); s != nil {
		if e := reEnabled.FindStringSubmatch(s[1]); e != nil && strings.EqualFold(e[1], "false") {
			st.Enabled = false
		}
	}
	return st, nil
}

// TaskSetEnabled: schtasks.exe /Change /TN <caminho> /ENABLE ou /DISABLE. Com Elevate.
func (w *windowsSystem) TaskSetEnabled(ctx context.Context, path string, enabled bool) error {
	if err := checkTaskPath(path); err != nil {
		return err
	}
	flag := "/DISABLE"
	if enabled {
		flag = "/ENABLE"
	}
	c, r, err := w.writeCmd(ctx, true, schtasksExe, "/Change", "/TN", path, flag)
	if err != nil {
		return fmt.Errorf("schtasks /Change %s: %w", path, err)
	}
	if r.ExitCode != 0 {
		return cmdFailed(c, r)
	}
	return nil
}

// decodeText converte a saída do schtasks para texto: UTF-16 (com BOM, ou com bytes NUL alternados) vira UTF-8; o
// BOM UTF-8 some. Outro texto passa intacto.
func decodeText(b []byte) string {
	switch {
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return utf16Decode(b[2:], false)
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return utf16Decode(b[2:], true)
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return string(b[3:])
	case len(b) >= 4 && b[1] == 0 && b[3] == 0:
		return utf16Decode(b, false)
	}
	return string(b)
}

func utf16Decode(b []byte, bigEndian bool) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return string(utf16.Decode(u))
}
