package recipes

import (
	"context"
	"fmt"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/platform"
)

// SystemRebooter reinicia a máquina pelo exec.Runner (nunca chama o SO direto, então --dry-run e os
// testes não reiniciam nada): `shutdown.exe /r /t 5` no Windows, `shutdown -r now` no Linux e macOS
// (com sudo -n se o processo não é root).
type SystemRebooter struct {
	Runner exec.Runner
	Info   platform.Info
}

// Reboot reinicia.
func (s SystemRebooter) Reboot(ctx context.Context) error {
	var c exec.Cmd
	if s.Info.OS == platform.Windows {
		c = exec.Cmd{Name: windowsShutdown, Args: []string{"/r", "/t", "5", "/c", "toolkit: reiniciando para continuar a receita"}}
	} else {
		c = exec.Cmd{Name: "shutdown", Args: []string{"-r", "now"}, Elevate: !s.Info.IsAdmin}
	}
	res, err := s.Runner.Run(ctx, c)
	if err != nil {
		return fmt.Errorf("reiniciar: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("reiniciar: o comando saiu com código %d", res.ExitCode)
	}
	return nil
}
