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

// uninstallScript lista "DisplayName<TAB>DisplayVersion" do registro Uninstall nas quatro visões: HKLM e HKCU,
// 64 e 32 bits (WOW6432Node). É texto FIXO: nada do catálogo entra no script (o casamento é feito em Go), então
// não há injeção. A saída em UTF-8 mantém os acentos dos nomes.
const uninstallScript = `$ErrorActionPreference='SilentlyContinue';` +
	`[Console]::OutputEncoding=[Text.Encoding]::UTF8;` +
	`foreach($h in 'HKLM:','HKCU:'){foreach($s in 'SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall','SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall'){` +
	`Get-ItemProperty -Path ($h+'\'+$s+'\*') | Where-Object { $_.DisplayName } | ForEach-Object { ($_.DisplayName -replace '[\t\r\n]',' ') + [char]9 + ($_.DisplayVersion -replace '[\t\r\n]',' ') } } }`

const maxUninstallOutput = 4 << 20

func (d *Detector) findWindows(ctx context.Context, spec catalog.Detect) (Found, bool) {
	if spec.DisplayName == "" || d.rn == nil {
		return Found{}, false
	}
	res, err := d.rn.Run(ctx, d.readOnly(exec.Cmd{Name: windowsPowerShell,
		Args: []string{"-NoProfile", "-NonInteractive", "-Command", uninstallScript}, Timeout: 30 * time.Second}))
	if err != nil || res.ExitCode != 0 || len(res.Stdout) > maxUninstallOutput {
		return Found{}, false
	}
	want := strings.TrimSpace(spec.DisplayName)
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		line = strings.TrimRight(line, "\r")
		name, ver, _ := strings.Cut(line, "\t")
		name = strings.TrimSpace(name)
		if name == "" || !MatchDisplayName(name, want) {
			continue
		}
		return Found{Where: `Programas e recursos do Windows ("` + clean(name, 100) + `")`, Version: clean(ver, 64)}, true
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
