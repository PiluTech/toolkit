package platform

import (
	"strings"

	"github.com/PiluVitu/toolkit/internal/buildinfo"
)

// Paths são os diretórios do briefing, por SO e por admin/usuário.
type Paths struct {
	StateDir string // state.json e logs
	LogDir   string // <StateDir>/logs
	BinDir   string // onde o binário é instalado
}

// Env é o que ResolvePaths precisa do ambiente (injetável).
type Env struct {
	Getenv func(string) string
	Home   string
}

// ResolvePaths segue o briefing:
//   - Windows admin: %ProgramData%\toolkit; usuário: %LOCALAPPDATA%\toolkit.
//   - Linux root: /var/lib/toolkit; usuário: $XDG_STATE_HOME/toolkit (ou ~/.local/state/toolkit).
//   - macOS admin: /Library/Application Support/toolkit; usuário: ~/Library/Application Support/toolkit.
//
// O separador segue o SO de destino, para o resultado ser testável em qualquer máquina.
func ResolvePaths(i Info, env Env) Paths {
	get := env.Getenv
	if get == nil {
		get = func(string) string { return "" }
	}
	name := buildinfo.Name
	var state, bin string

	switch i.OS {
	case Windows:
		j := func(p ...string) string { return strings.Join(p, `\`) }
		if i.IsAdmin {
			pd := firstNonEmpty(get("ProgramData"), `C:\ProgramData`)
			state = j(pd, name)
			bin = j(pd, name, "bin")
		} else {
			la := firstNonEmpty(get("LOCALAPPDATA"), j(env.Home, "AppData", "Local"))
			state = j(la, name)
			bin = j(la, name, "bin")
		}
		return Paths{StateDir: state, LogDir: j(state, "logs"), BinDir: bin}
	case Darwin:
		if i.IsAdmin {
			state, bin = "/Library/Application Support/"+name, "/usr/local/bin"
		} else {
			state, bin = env.Home+"/Library/Application Support/"+name, env.Home+"/.local/bin"
		}
	default: // linux
		if i.IsAdmin {
			state, bin = "/var/lib/"+name, "/usr/local/bin"
		} else {
			base := firstNonEmpty(get("XDG_STATE_HOME"), env.Home+"/.local/state")
			state, bin = base+"/"+name, env.Home+"/.local/bin"
		}
	}
	return Paths{StateDir: state, LogDir: state + "/logs", BinDir: bin}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
