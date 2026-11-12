package resume

import (
	"fmt"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/platform"
)

// Config reúne o que cada registrador precisa.
type Config struct {
	Info      platform.Info
	Paths     platform.Paths
	Runner    exec.Runner
	FS        FS     // padrão OSFS
	StatePath string // Linux: ConditionPathExists
	Home      string // macOS
	UID       string // macOS
}

// New escolhe o registrador do SO: schtask (Windows), systemd (Linux), launchd (macOS).
func New(c Config) (Registrar, error) {
	fsys := c.FS
	if fsys == nil {
		fsys = OSFS{}
	}
	switch c.Info.OS {
	case platform.Windows:
		return SchTask{Runner: c.Runner, FS: fsys, BinDir: c.Paths.BinDir}, nil
	case platform.Linux:
		// BinDir fica vazio de propósito: o root executa a cópia em RootBinDir, nunca o diretório do usuário.
		return Systemd{Runner: c.Runner, IsRoot: c.Info.IsAdmin, StatePath: c.StatePath}, nil
	case platform.Darwin:
		return Launchd{Runner: c.Runner, FS: fsys, Home: c.Home, UID: c.UID, BinDir: c.Paths.BinDir}, nil
	}
	return nil, fmt.Errorf("%w: %q", platform.ErrUnsupportedOS, c.Info.OS)
}
