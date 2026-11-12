package platform

import (
	"context"
	"strings"

	"github.com/PiluTech/toolkit/internal/exec"
)

// elevatedScript devolve True quando o token do processo tem a função de Administrador.
const elevatedScript = "([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)"

// IsElevatedViaPowerShell diz se o processo atual está elevado no Windows, perguntando ao PowerShell
// (leitura, por caminho absoluto, pelo Runner: mockável e sem dependência de x/sys). Qualquer falha é "não
// elevado": errar para esse lado só faz o toolkit pedir administrador, nunca agir sem ele.
func IsElevatedViaPowerShell(ctx context.Context, r exec.Runner) bool {
	if r == nil {
		return false
	}
	res, err := r.Run(ctx, psCmd(elevatedScript))
	if err != nil || res.ExitCode != 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(res.Stdout)), "True")
}
