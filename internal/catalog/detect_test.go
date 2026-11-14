package catalog

import (
	"errors"
	"strings"
	"testing"
)

const detectBase = `
apps:
  discord:
    name: Discord
    category: comunicacao
    windows: [{ provider: winget, id: Discord.Discord }]
    macos:   [{ provider: brew, cask: discord }]
    linux:   [{ provider: flatpak, id: com.discordapp.Discord }]
`

func TestDetectCatalogoAntigoContinuaValido(t *testing.T) {
	c, err := Parse("catalog.yaml", []byte(detectBase))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Get("discord")
	if _, ok := a.DetectFor("darwin"); ok {
		t.Fatal("sem bloco detect não deve haver detecção")
	}
}

func TestDetectValido(t *testing.T) {
	src := detectBase + `    detect:
      macos:   { app_bundle: "Discord.app", bundle_id: com.hnc.Discord }
      windows: { display_name: Discord }
      linux:   { desktop_id: discord.desktop, flatpak_id: com.discordapp.Discord, snap: discord }
`
	c, err := Parse("catalog.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Get("discord")
	m, ok := a.DetectFor("darwin")
	if !ok || m.AppBundle != "Discord.app" || m.BundleID != "com.hnc.Discord" {
		t.Fatalf("macos: %+v", m)
	}
	w, _ := a.DetectFor("windows")
	if w.DisplayName != "Discord" {
		t.Fatalf("windows: %+v", w)
	}
	l, _ := a.DetectFor("linux")
	if l.DesktopID != "discord.desktop" || l.FlatpakID != "com.discordapp.Discord" || l.Snap != "discord" {
		t.Fatalf("linux: %+v", l)
	}
	// Merge faz cópia profunda do bloco.
	m2, _ := Merge(c, nil, false)
	a2, _ := m2.Get("discord")
	if d, _ := a2.DetectFor("linux"); d.Snap != "discord" {
		t.Fatalf("merge perdeu o detect: %+v", d)
	}
}

func TestDetectAceitaNomeComAcentoEEspaco(t *testing.T) {
	src := detectBase + `    detect:
      macos: { app_bundle: "Visual Studio Código.app" }
      windows: { display_name: "Microsoft Visual Studio Code" }
`
	if _, err := Parse("catalog.yaml", []byte(src)); err != nil {
		t.Fatal(err)
	}
}

func TestDetectErros(t *testing.T) {
	cases := []struct{ name, block, want string }{
		{"os desconhecido", "    detect:\n      beos: { snap: x }\n", `chave desconhecida "beos"`},
		{"any não vale", "    detect:\n      any: { snap: x }\n", `chave desconhecida "any"`},
		{"campo desconhecido", "    detect:\n      macos: { app_bundl: Discord.app }\n", `campo desconhecido "app_bundl"`},
		{"campo de outro SO", "    detect:\n      macos: { snap: discord }\n", `não vale para detect.macos`},
		{"os repetido", "    detect:\n      macos: { app_bundle: A.app }\n      macos: { app_bundle: B.app }\n", `repetida`},
		{"campo repetido", "    detect:\n      macos: { app_bundle: A.app, app_bundle: B.app }\n", `repetida`},
		{"detect repetido", "    detect:\n      macos: { app_bundle: A.app }\n    detect:\n      macos: { app_bundle: B.app }\n", `repetida`},
		{"bloco vazio", "    detect:\n      macos: {}\n", `ao menos um campo`},
		{"detect vazio", "    detect: {}\n", `ao menos um SO`},
		{"detect não é mapa", "    detect: [a]\n", `deve ser um mapa`},
		{"os não é mapa", "    detect:\n      macos: [a]\n", `deve ser um mapa`},
		{"bundle sem .app", "    detect:\n      macos: { app_bundle: Discord }\n", `app_bundle`},
		{"bundle com barra", "    detect:\n      macos: { app_bundle: \"../Discord.app\" }\n", `app_bundle`},
		{"bundle com ..", "    detect:\n      macos: { app_bundle: \"a..b.app\" }\n", `app_bundle`},
		{"bundle id inválido", "    detect:\n      macos: { bundle_id: \"com.x y\" }\n", `bundle_id`},
		{"desktop sem sufixo", "    detect:\n      linux: { desktop_id: discord }\n", `desktop_id`},
		{"desktop com barra", "    detect:\n      linux: { desktop_id: \"a/b.desktop\" }\n", `desktop_id`},
		{"snap maiúsculo", "    detect:\n      linux: { snap: Discord }\n", `snap`},
		{"flatpak com espaço", "    detect:\n      linux: { flatpak_id: \"a b\" }\n", `flatpak_id`},
		{"display vazio", "    detect:\n      windows: { display_name: \"  \" }\n", `display_name`},
		{"display com borda em branco", "    detect:\n      windows: { display_name: \" Discord\" }\n", `display_name`},
		{"valor não é texto", "    detect:\n      linux: { snap: [a] }\n", `snap`},
		{"ESC no valor", "    detect:\n      windows: { display_name: \"Dis\\ecord\" }\n", `U+001B`},
		{"publisher em outro SO", "    detect:\n      macos: { publisher: X }\n", `não vale para detect.macos`},
		{"app_path em outro SO", "    detect:\n      linux: { app_path: chrome.exe }\n", `não vale para detect.linux`},
		{"app_path sem .exe", "    detect:\n      windows: { app_path: chrome }\n", `app_path`},
		{"app_path com barra", "    detect:\n      windows: { app_path: \"a\\\\b.exe\" }\n", `app_path`},
		{"app_path com ..", "    detect:\n      windows: { app_path: \"a..b.exe\" }\n", `app_path`},
		{"publisher vazio", "    detect:\n      windows: { publisher: \"  \" }\n", `publisher`},
		{"publisher com ESC", "    detect:\n      windows: { publisher: \"Go\\eogle\" }\n", `U+001B`},
		{"publisher gigante", "    detect:\n      windows: { publisher: \"" + strings.Repeat("a", 201) + "\" }\n", `publisher`},
		{"bidi", "    detect:\n      windows: { display_name: \"Dis\\u202Ecord\" }\n", `U+202E`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("catalog.yaml", []byte(detectBase+tc.block))
			if err == nil {
				t.Fatalf("esperava erro contendo %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("erro %q não contém %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "catalog.yaml:") {
				t.Fatalf("erro sem arquivo:linha: %q", err)
			}
		})
	}
}

func TestDetectErroTemLinhaEColuna(t *testing.T) {
	_, err := Parse("catalog.yaml", []byte(detectBase+"    detect:\n      macos: { app_bundle: Discord }\n"))
	var es Errors
	if !errors.As(err, &es) || es[0].Line != 10 || es[0].Col == 0 {
		t.Fatalf("posição: %v", err)
	}
}

func TestDetectWindowsPublisherEAppPath(t *testing.T) {
	src := detectBase + `    detect:
      windows: { display_name: Google Chrome, publisher: "Google LLC", app_path: chrome.exe }
`
	c, err := Parse("catalog.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Get("discord")
	w, ok := a.DetectFor("windows")
	if !ok || w.Publisher != "Google LLC" || w.AppPath != "chrome.exe" {
		t.Fatalf("%+v", w)
	}
	// So app_path (sem display_name) também é um bloco válido, e o Merge copia os campos novos.
	only := detectBase + "    detect:\n      windows: { app_path: chrome.exe }\n"
	c2, err := Parse("catalog.yaml", []byte(only))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := Merge(c2, nil, false)
	a2, _ := m.Get("discord")
	if d, ok := a2.DetectFor("windows"); !ok || d.AppPath != "chrome.exe" {
		t.Fatalf("merge: %+v %v", d, ok)
	}
}
