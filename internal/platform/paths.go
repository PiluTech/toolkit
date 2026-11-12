package platform

import (
	"errors"
	"fmt"
	"strings"

	"github.com/PiluTech/toolkit/internal/buildinfo"
)

// Erros de ResolvePaths e Detect.
var (
	// ErrNoHome: o diretório de estado do usuário depende de um Home absoluto e não há como obtê-lo.
	ErrNoHome = errors.New("não foi possível determinar o diretório do usuário (home ausente ou relativo)")
	// ErrUnsupportedOS: só windows, linux e darwin são suportados.
	ErrUnsupportedOS = errors.New("sistema operacional não suportado")
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

// windowsProgramData é fixo de propósito: o estado do admin vive numa pasta que só administrador
// escreve, e a variável %ProgramData% do ambiente herdado é controlável por quem chamou.
const windowsProgramData = `C:\ProgramData`

// ResolvePaths segue o briefing:
//   - Windows admin: C:\ProgramData\toolkit (fixo, nunca a variável de ambiente); usuário: %LOCALAPPDATA%\toolkit
//     (só se absoluta; senão <Home>\AppData\Local).
//   - Linux root: /var/lib/toolkit; usuário: $XDG_STATE_HOME/toolkit (só se absoluto; senão ~/.local/state/toolkit).
//   - macOS admin: /Library/Application Support/toolkit; usuário: ~/Library/Application Support/toolkit.
//
// Para usuário sem Home absoluto utilizável devolve ErrNoHome (nunca um caminho relativo à raiz); SO fora
// da lista devolve ErrUnsupportedOS. O separador segue o SO de destino, para ser testável em qualquer máquina.
func ResolvePaths(i Info, env Env) (Paths, error) {
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
			state, bin = j(windowsProgramData, name), j(windowsProgramData, name, "bin")
		} else {
			la := get("LOCALAPPDATA")
			if !isWindowsAbs(la) {
				if !isWindowsAbs(env.Home) {
					return Paths{}, ErrNoHome
				}
				la = j(strings.TrimRight(env.Home, `\`), "AppData", "Local")
			}
			la = strings.TrimRight(la, `\`)
			state, bin = j(la, name), j(la, name, "bin")
		}
		return Paths{StateDir: state, LogDir: j(state, "logs"), BinDir: bin}, nil
	case Darwin:
		if i.IsAdmin {
			state, bin = "/Library/Application Support/"+name, "/usr/local/bin"
		} else {
			home, err := unixHome(env)
			if err != nil {
				return Paths{}, err
			}
			state, bin = home+"/Library/Application Support/"+name, home+"/.local/bin"
		}
	case Linux:
		if i.IsAdmin {
			state, bin = "/var/lib/"+name, "/usr/local/bin"
		} else {
			home, err := unixHome(env)
			if err != nil {
				return Paths{}, err
			}
			base := strings.TrimRight(get("XDG_STATE_HOME"), "/")
			if !strings.HasPrefix(base, "/") { // XDG relativo ou vazio é ignorado (regra da especificação XDG)
				base = home + "/.local/state"
			}
			state, bin = base+"/"+name, home+"/.local/bin"
		}
	default:
		return Paths{}, fmt.Errorf("%w: %q", ErrUnsupportedOS, i.OS)
	}
	return Paths{StateDir: state, LogDir: state + "/logs", BinDir: bin}, nil
}

func unixHome(env Env) (string, error) {
	h := strings.TrimRight(env.Home, "/")
	if !strings.HasPrefix(h, "/") {
		return "", ErrNoHome
	}
	return h, nil
}

// isWindowsAbs aceita X:\... e \\servidor\... (não aceita relativo nem "C:" sem barra).
func isWindowsAbs(p string) bool {
	if len(p) >= 3 && p[1] == ':' && p[2] == '\\' && (p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z') {
		return true
	}
	return strings.HasPrefix(p, `\\`)
}
