package recipes

import (
	"encoding/base64"
	"errors"
	"time"
	"unicode/utf16"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/platform"
)

// Caminhos absolutos no Windows: nunca resolver powershell.exe/wsl.exe pelo PATH (admin).
const (
	windowsPowerShell = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
	windowsWSL        = `C:\Windows\System32\wsl.exe`
	windowsShutdown   = `C:\Windows\System32\shutdown.exe`
)

// ErrBadAction indica que a ação não pode ser montada para este SO (a validação da carga já deveria ter recusado).
var ErrBadAction = errors.New("ação inválida para este sistema")

// EncodePowerShell codifica o script como o -EncodedCommand espera: UTF-16LE em base64. Elimina os
// problemas de aspas entre Go, cmd e PowerShell.
func EncodePowerShell(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// BuildCmd monta o exec.Cmd da ação. readOnly marca o Cmd de um check (não muda o sistema); elevate
// prefixa sudo -n no Unix.
func BuildCmd(a *Action, goos platform.OS, timeout time.Duration, elevate, readOnly bool) (exec.Cmd, error) {
	c := exec.Cmd{Timeout: timeout, Elevate: elevate, ReadOnly: readOnly}
	switch {
	case a.PowerShell != "" && goos == platform.Windows:
		c.Name = windowsPowerShell
		c.Args = []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", EncodePowerShell(a.PowerShell)}
	case a.WSL != nil && goos == platform.Windows:
		c.Name = windowsWSL
		c.Args = []string{"-d", a.WSL.Distro}
		if a.WSL.User != "" {
			c.Args = append(c.Args, "-u", a.WSL.User)
		}
		c.Args = append(c.Args, "--", "bash", "-c", a.WSL.Bash)
	case a.Sh != "" && goos != platform.Windows:
		c.Name = "/bin/sh"
		c.Args = []string{"-c", a.Sh}
	case a.Bash != "" && goos != platform.Windows:
		c.Name = "bash"
		c.Args = []string{"-c", a.Bash}
	default:
		return exec.Cmd{}, ErrBadAction
	}
	return c, nil
}
