package platform

import (
	"errors"
	"testing"
)

func TestResolvePaths(t *testing.T) {
	home := "/home/u"
	cases := []struct {
		name string
		info Info
		env  Env
		want Paths
	}{
		{"windows admin (ProgramData fixo)", Info{OS: Windows, IsAdmin: true}, Env{},
			Paths{`C:\ProgramData\toolkit`, `C:\ProgramData\toolkit\logs`, `C:\ProgramData\toolkit\bin`}},
		{"windows admin ignora ProgramData hostil", Info{OS: Windows, IsAdmin: true},
			Env{Getenv: env(map[string]string{"ProgramData": `C:\Users\Public\evil`})},
			Paths{`C:\ProgramData\toolkit`, `C:\ProgramData\toolkit\logs`, `C:\ProgramData\toolkit\bin`}},
		{"windows usuário", Info{OS: Windows}, Env{Getenv: env(map[string]string{"LOCALAPPDATA": `C:\L`})},
			Paths{`C:\L\toolkit`, `C:\L\toolkit\logs`, `C:\L\toolkit\bin`}},
		{"windows usuário, LOCALAPPDATA relativo cai no Home", Info{OS: Windows},
			Env{Home: `C:\Users\u`, Getenv: env(map[string]string{"LOCALAPPDATA": `rel\x`})},
			Paths{`C:\Users\u\AppData\Local\toolkit`, `C:\Users\u\AppData\Local\toolkit\logs`, `C:\Users\u\AppData\Local\toolkit\bin`}},
		{"windows usuário sem LOCALAPPDATA", Info{OS: Windows}, Env{Home: `C:\Users\u`},
			Paths{`C:\Users\u\AppData\Local\toolkit`, `C:\Users\u\AppData\Local\toolkit\logs`, `C:\Users\u\AppData\Local\toolkit\bin`}},
		{"linux root", Info{OS: Linux, IsAdmin: true}, Env{},
			Paths{"/var/lib/toolkit", "/var/lib/toolkit/logs", "/usr/local/bin"}},
		{"linux usuário com XDG", Info{OS: Linux}, Env{Home: home, Getenv: env(map[string]string{"XDG_STATE_HOME": "/x"})},
			Paths{"/x/toolkit", "/x/toolkit/logs", home + "/.local/bin"}},
		{"linux usuário, XDG relativo é ignorado", Info{OS: Linux}, Env{Home: home, Getenv: env(map[string]string{"XDG_STATE_HOME": "relativo/x"})},
			Paths{home + "/.local/state/toolkit", home + "/.local/state/toolkit/logs", home + "/.local/bin"}},
		{"linux usuário sem XDG", Info{OS: Linux}, Env{Home: home},
			Paths{home + "/.local/state/toolkit", home + "/.local/state/toolkit/logs", home + "/.local/bin"}},
		{"macos admin", Info{OS: Darwin, IsAdmin: true}, Env{},
			Paths{"/Library/Application Support/toolkit", "/Library/Application Support/toolkit/logs", "/usr/local/bin"}},
		{"macos usuário", Info{OS: Darwin}, Env{Home: "/Users/u"},
			Paths{"/Users/u/Library/Application Support/toolkit", "/Users/u/Library/Application Support/toolkit/logs", "/Users/u/.local/bin"}},
	}
	for _, c := range cases {
		got, err := ResolvePaths(c.info, c.env)
		if err != nil || got != c.want {
			t.Errorf("%s:\n got  %+v (%v)\n want %+v", c.name, got, err, c.want)
		}
	}
}

func TestResolvePathsErros(t *testing.T) {
	cases := []struct {
		name string
		info Info
		env  Env
		want error
	}{
		{"linux usuário sem Home e sem XDG", Info{OS: Linux}, Env{}, ErrNoHome},
		{"linux usuário, XDG relativo e sem Home", Info{OS: Linux}, Env{Getenv: env(map[string]string{"XDG_STATE_HOME": "rel"})}, ErrNoHome},
		{"linux usuário com Home relativo", Info{OS: Linux}, Env{Home: "home/u"}, ErrNoHome},
		{"macos usuário sem Home", Info{OS: Darwin}, Env{}, ErrNoHome},
		{"windows usuário sem LOCALAPPDATA nem Home", Info{OS: Windows}, Env{}, ErrNoHome},
		{"windows usuário, Home relativo", Info{OS: Windows}, Env{Home: `Users\u`}, ErrNoHome},
		{"SO desconhecido", Info{OS: "plan9"}, Env{Home: "/h"}, ErrUnsupportedOS},
	}
	for _, c := range cases {
		if got, err := ResolvePaths(c.info, c.env); !errors.Is(err, c.want) || got != (Paths{}) {
			t.Errorf("%s: %+v err=%v, quer %v", c.name, got, err, c.want)
		}
	}
}
