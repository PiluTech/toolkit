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
//	A origem exe                                                  (App Paths)
//
// origem: HKLM, HKLM-WOW64 (WOW6432Node) ou HKCU. "atualização" é 1 quando há ParentKeyName ou ReleaseType
// (patch, hotfix). O script é texto FIXO e ASCII: nada do catálogo entra nele (o casamento é feito em Go), então
// não há injeção. Cada raiz tem o seu try: uma que falha (acesso negado, chave ausente) só some da saída e as
// outras continuam. Termina SEMPRE com exit 0: com SilentlyContinue, uma raiz ausente (ex.: o WOW6432Node do
// HKCU) deixa $? falso e o powershell -Command sairia com 1, e antes isso descartava a saída inteira, mesmo
// com os outros apps lidos. Falha de verdade (PowerShell que não abre, timeout) continua vindo como erro ou
// exit != 0 do próprio processo e nunca vira "instalado". A saída em UTF-8 mantém os acentos dos nomes.
const uninstallScript = `$ErrorActionPreference='SilentlyContinue';` +
	`[Console]::OutputEncoding=[Text.Encoding]::UTF8;` +
	`function C($v){if($null -eq $v){''}else{([string]$v) -replace '[\t\r\n]',' '}};` +
	`$t=[char]9;$cv='\SOFTWARE\Microsoft\Windows\CurrentVersion\';` +
	`$u=@(@('HKLM','HKLM:'+$cv+'Uninstall'),@('HKLM-WOW64','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall'),@('HKCU','HKCU:'+$cv+'Uninstall'));` +
	`foreach($r in $u){try{Get-ItemProperty -Path ($r[1]+'\*') | Where-Object { $_.DisplayName } | ForEach-Object { ` +
	`$sc=if($_.SystemComponent -eq 1){'1'}else{'0'};$up=if($_.ParentKeyName -or $_.ReleaseType){'1'}else{'0'};` +
	`'U'+$t+$r[0]+$t+(C $_.DisplayName)+$t+(C $_.DisplayVersion)+$t+(C $_.Publisher)+$t+$sc+$t+$up } }catch{}};` +
	`foreach($r in @(@('HKLM','HKLM:'+$cv+'App Paths'),@('HKCU','HKCU:'+$cv+'App Paths'))){try{Get-ChildItem -Path $r[1] | ForEach-Object { 'A'+$t+$r[0]+$t+(C $_.PSChildName) } }catch{}};` +
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

func (d *Detector) findWindows(ctx context.Context, spec catalog.Detect) (Found, bool) {
	if (spec.DisplayName == "" && spec.AppPath == "") || d.rn == nil {
		return Found{}, false
	}
	res, err := d.rn.Run(ctx, d.readOnly(exec.Cmd{Name: windowsPowerShell,
		Args: []string{"-NoProfile", "-NonInteractive", "-Command", uninstallScript}, Timeout: 30 * time.Second}))
	if err != nil || res.ExitCode != 0 || len(res.Stdout) > maxUninstallOutput {
		return Found{}, false
	}
	want := strings.TrimSpace(spec.DisplayName)
	pub := strings.TrimSpace(spec.Publisher)
	exe := strings.TrimSpace(spec.AppPath)
	var viaPath *Found
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		switch {
		case len(f) == 7 && f[0] == "U" && want != "":
			label := originLabel(f[1])
			name := strings.TrimSpace(f[2])
			if label == "" || name == "" || f[5] == "1" || f[6] == "1" || !MatchDisplayName(name, want) {
				continue
			}
			if pub != "" && !strings.EqualFold(strings.TrimSpace(f[4]), pub) {
				continue
			}
			// O Uninstall traz a versão e tem prioridade sobre o App Paths.
			return Found{Where: `Programas e recursos do Windows [` + label + `] ("` + clean(name, 100) + `")`, Version: clean(f[3], 64)}, true
		case len(f) == 3 && f[0] == "A" && exe != "" && viaPath == nil:
			label := originLabel(f[1])
			if label != "" && strings.EqualFold(strings.TrimSpace(f[2]), exe) {
				viaPath = &Found{Where: "App Paths do Windows [" + label + "] (" + clean(f[2], 100) + ")"}
			}
		}
	}
	if viaPath != nil {
		return *viaPath, true
	}
	return Found{}, false
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
