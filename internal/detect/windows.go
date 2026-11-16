package detect

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
)

// windowsPowerShell é fixo: um powershell.exe achado no PATH poderia ser plantado.
const windowsPowerShell = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`

// uninstallScript lê o registro, SÓ LEITURA, e imprime uma linha por item, separada por TAB:
//
//	U origem nome versão publisher SystemComponent atualização   (chave de desinstalação)
//	A origem subchave caminho folha                               (App Paths: valor PADRÃO da subchave)
//	S tipo origem estado                                          (estado da raiz: ok, ausente ou falhou)
//
// origem: HKLM, HKLM-WOW64 (WOW6432Node) ou HKCU. "atualização" é 1 quando há ParentKeyName ou ReleaseType
// (patch, hotfix). "caminho" é o valor padrão da subchave de App Paths, sem aspas externas e com as variáveis de
// ambiente expandidas ([Environment]::ExpandEnvironmentVariables, só isso); "folha" é 1 quando esse caminho é
// local absoluto (X:\...), termina em .exe, está em unidade LOCAL (DriveType Fixed, conferido ANTES de qualquer
// acesso: letra mapeada para a rede não passa e o Test-Path nem roda) e é um ARQUIVO que existe (Test-Path
// -PathType Leaf; nada é executado).
// O script é texto FIXO e ASCII: nada do catálogo entra nele (o casamento é feito em Go), então não há injeção.
//
// Os caminhos do registro são literais COMPLETOS, sem concatenação: no PowerShell a vírgula tem precedência
// maior que o +, então @('HKLM','HKLM:'+$cv+'X') viraria uma lista de 4 elementos. Cada raiz tem o seu
// try/catch com -ErrorAction Stop: ItemNotFoundException = "ausente" (normal para HKCU\WOW6432Node e App Paths);
// qualquer outro erro (acesso negado) = "falhou", e as leituras das outras raízes continuam valendo. O script
// termina SEMPRE com exit 0; falha de verdade (PowerShell que não abre, timeout) continua vindo como erro ou
// exit != 0 do próprio processo e nunca vira "instalado". A saída em UTF-8 mantém os acentos dos nomes.
const uninstallScript = `$ErrorActionPreference='Stop';` +
	`[Console]::OutputEncoding=[Text.Encoding]::UTF8;` +
	`function C($v){if($null -eq $v){''}else{([string]$v) -replace '[\t\r\n]',' '}};` +
	`$t=[char]9;` +
	`$u=@(@{N='HKLM';P='HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall'},` +
	`@{N='HKLM-WOW64';P='HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall'},` +
	`@{N='HKCU';P='HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall'});` +
	`$a=@(@{N='HKLM';P='HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths'},` +
	`@{N='HKCU';P='HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths'});` +
	`foreach($r in $u){$s='ok';try{foreach($k in @(Get-ChildItem -Path $r.P)){try{$n=$k.GetValue('DisplayName');if($n){` +
	`$sc=if($k.GetValue('SystemComponent') -eq 1){'1'}else{'0'};` +
	`$up=if($k.GetValue('ParentKeyName') -or $k.GetValue('ReleaseType')){'1'}else{'0'};` +
	`'U'+$t+$r.N+$t+(C $n)+$t+(C $k.GetValue('DisplayVersion'))+$t+(C $k.GetValue('Publisher'))+$t+$sc+$t+$up}}catch{$s='falhou'}}` +
	`}catch [System.Management.Automation.ItemNotFoundException]{$s='ausente'}catch{$s='falhou'};` +
	`'S'+$t+'U'+$t+$r.N+$t+$s};` +
	`foreach($r in $a){$s='ok';try{foreach($k in @(Get-ChildItem -Path $r.P)){try{$p='';$l='0';` +
	`$v=$k.GetValue('',$null,'DoNotExpandEnvironmentNames');` +
	`if($v -is [string]){$p=[Environment]::ExpandEnvironmentVariables($v.Trim().Trim('"').Trim());` +
	`if(($p -match '^[A-Za-z]:\\[^\\]') -and ($p -match '\.exe$')){$dt='';try{$dt=[string][IO.DriveInfo]::new($p.Substring(0,1)).DriveType}catch{};` +
	`if($dt -eq 'Fixed'){if(Test-Path -LiteralPath $p -PathType Leaf){$l='1'}}}};` +
	`'A'+$t+$r.N+$t+(C $k.PSChildName)+$t+(C $p)+$t+$l}catch{$s='falhou'}}` +
	`}catch [System.Management.Automation.ItemNotFoundException]{$s='ausente'}catch{$s='falhou'};` +
	`'S'+$t+'A'+$t+$r.N+$t+$s};` +
	`exit 0`

const maxUninstallOutput = 4 << 20

// originLabel é o nome da raiz do registro para o usuário; "" = origem desconhecida (linha descartada).
func originLabel(o string) string {
	switch o {
	case "HKLM":
		return "HKLM"
	case "HKLM-WOW64":
		return "HKLM-WOW64"
	case "HKCU":
		return "HKCU"
	}
	return ""
}

// rootKey identifica uma raiz do registro lida pelo script: tipo "U" (Uninstall) ou "A" (App Paths) e origem.
type rootKey struct{ kind, origin string }

// windowsRoots são as raízes que o script lê. Uma que não reportou estado conta como falha (saída truncada).
var (
	uninstallRoots = []rootKey{{"U", "HKLM"}, {"U", "HKLM-WOW64"}, {"U", "HKCU"}}
	appPathsRoots  = []rootKey{{"A", "HKLM"}, {"A", "HKCU"}}
)

// localExe diz se p é um caminho local absoluto de arquivo .exe ("X:\...\app.exe"): nada de UNC ("\\servidor\..."),
// caminho relativo, pasta de unidade nem outra extensão.
func localExe(p string) bool {
	if len(p) < 4 || p[1] != ':' || p[2] != '\\' || p[3] == '\\' {
		return false
	}
	c := p[0] | 0x20
	return c >= 'a' && c <= 'z' && strings.HasSuffix(strings.ToLower(p), ".exe")
}

// incomplete diz se alguma das raízes relevantes não foi lida com sucesso ("ok" ou "ausente").
func incomplete(status map[rootKey]string, roots ...[]rootKey) bool {
	for _, rs := range roots {
		for _, k := range rs {
			if st := status[k]; st != "ok" && st != "ausente" {
				return true
			}
		}
	}
	return false
}

// readWindowsRegistry roda o script UMA vez e reaproveita a saída pelo prazo do cache (cacheTTL), tanto o sucesso
// quanto a falha: a TUI pergunta o estado de cada app do catálogo e cada pergunta rodava um PowerShell que lê todo o
// registro (centenas de ms de CPU por app, vários em paralelo na partida). Perguntas simultâneas esperam a mesma
// leitura (o lock fica preso enquanto ela roda). Cancelamento não é guardado. Invalidate descarta (depois de
// instalar ou remover algo).
func (d *Detector) readWindowsRegistry(ctx context.Context) ([]byte, bool) {
	d.winMu.Lock()
	defer d.winMu.Unlock()
	if d.winBuilt && d.now().Sub(d.winAt) < cacheTTL {
		return d.winOut, d.winOK
	}
	res, err := d.rn.Run(ctx, d.readOnly(exec.Cmd{Name: windowsPowerShell,
		Args: []string{"-NoProfile", "-NonInteractive", "-Command", uninstallScript}, Timeout: 30 * time.Second}))
	if ctx.Err() != nil {
		return nil, false // cancelado: não vira cache
	}
	ok := err == nil && res.ExitCode == 0 && len(res.Stdout) <= maxUninstallOutput
	d.winOut, d.winOK, d.winAt, d.winBuilt = nil, ok, d.now(), true
	if ok {
		d.winOut = res.Stdout
	}
	return d.winOut, ok
}

func (d *Detector) findWindows(ctx context.Context, spec catalog.Detect) (Found, bool) {
	if (spec.DisplayName == "" && spec.AppPath == "") || d.rn == nil {
		return Found{}, false
	}
	out, readOK := d.readWindowsRegistry(ctx)
	if !readOK {
		// Nada foi lido: não é "não instalado", é detecção incompleta.
		return Found{Incomplete: true}, false
	}
	want := strings.TrimSpace(spec.DisplayName)
	pub := strings.TrimSpace(spec.Publisher)
	exe := strings.TrimSpace(spec.AppPath)
	var (
		viaPath *Found
	)
	status := map[rootKey]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		switch {
		case len(f) == 4 && f[0] == "S":
			status[rootKey{f[1], f[2]}] = strings.TrimSpace(f[3])
		case len(f) == 7 && f[0] == "U" && want != "":
			label := originLabel(f[1])
			name := strings.TrimSpace(f[2])
			if label == "" || name == "" || f[5] == "1" || f[6] == "1" {
				continue
			}
			if !MatchDisplayName(name, want) {
				continue
			}
			if pub != "" && !strings.EqualFold(strings.TrimSpace(f[4]), pub) {
				continue
			}
			// O Uninstall traz a versão e tem prioridade sobre o App Paths.
			return Found{Where: `Programas e recursos do Windows [` + label + `] ("` + clean(name, 100) + `")`, Version: clean(f[3], 64)}, true
		case len(f) == 5 && f[0] == "A" && exe != "" && pub == "" && viaPath == nil:
			label := originLabel(f[1])
			// Só vale a subchave do exe pedido com valor padrão apontando para um .exe local que EXISTE (a
			// folha vem do Test-Path do script; o formato é conferido de novo aqui).
			if label != "" && strings.EqualFold(strings.TrimSpace(f[2]), exe) && f[4] == "1" && localExe(strings.TrimSpace(f[3])) {
				viaPath = &Found{Where: "App Paths do Windows [" + label + "] (" + clean(f[2], 100) + ")"}
			}
		}
	}
	// App Paths não traz publisher: com publisher exigido pelo catálogo o fallback é recusado (outro programa do
	// mesmo publisher não prova que o executável é o app); só o Uninstall com nome e publisher certos vale.
	if viaPath != nil {
		return *viaPath, true
	}
	var rs [][]rootKey
	if want != "" {
		rs = append(rs, uninstallRoots)
	}
	if exe != "" {
		rs = append(rs, appPathsRoots)
	}
	return Found{Incomplete: incomplete(status, rs...)}, false
}

var versionTokenRe = regexp.MustCompile(`^[vV]?[0-9]+([._][0-9A-Za-z]+)*$`)

// MatchDisplayName diz se o DisplayName do registro é o app procurado: igual sem diferenciar maiúsculas, ou
// igual depois de tirar do FIM os grupos entre parênteses ("(x64)", "(User)") e os tokens que só são versão
// ("24.08"). "7-Zip 24.08 (x64)" casa com "7-Zip"; "Git Extensions" NÃO casa com "Git".
func MatchDisplayName(registry, want string) bool {
	if strings.EqualFold(registry, want) {
		return true
	}
	return strings.EqualFold(baseName(registry), want)
}

func baseName(s string) string {
	s = strings.TrimSpace(s)
	for {
		switch {
		case strings.HasSuffix(s, ")"):
			i := strings.LastIndex(s, "(")
			if i <= 0 {
				return s
			}
			s = strings.TrimSpace(s[:i])
		default:
			i := strings.LastIndexAny(s, " ")
			if i <= 0 || !versionTokenRe.MatchString(s[i+1:]) {
				return s
			}
			s = strings.TrimSpace(s[:i])
		}
	}
}
