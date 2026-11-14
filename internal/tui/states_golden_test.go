package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Goldens dos estados padronizados, em 80x24 e 60x17, sempre em Mono (sem estilo): carregando (com e sem movimento),
// erro, sem administrador, fora do Windows (darwin), vazio e terminal pequeno.
func stateSizes() [][2]int { return [][2]int{{80, 24}, {60, 17}} }

func stGolden(t *testing.T, name string, o Options, tab Tab) {
	t.Helper()
	for _, sz := range stateSizes() {
		m := sized(o, sz[0], sz[1], tab)
		golden(t, fmt.Sprintf("estado_%s_%dx%d", name, sz[0], sz[1]), m.render())
	}
}

func TestGoldenEstadoCarregandoComMovimento(t *testing.T) {
	tab := NewAppsTab(context.Background(), &fakeBackend{}, Theme{Mono: true, Dark: true}, "linux")
	stGolden(t, "carregando_movimento", Options{Version: "v0.1.0", Mono: true}, tab)
}

func TestGoldenEstadoCarregandoSemMovimento(t *testing.T) {
	tab := NewAppsTab(context.Background(), &fakeBackend{}, Theme{Mono: true, Dark: true, NoMotion: true}, "linux")
	stGolden(t, "carregando_semmovimento", Options{Version: "v0.1.0", Mono: true, NoMotion: true}, tab)
}

func TestGoldenEstadoErro(t *testing.T) {
	tab := NewAppsTab(context.Background(), &fakeBackend{loadErr: errors.New("meu.yaml:3:5: app \"x\": categoria inválida\nmeu.yaml:9:1: chave repetida")}, Theme{Mono: true, NoMotion: true}, "linux")
	tab.Update(tab.loadCmd()())
	stGolden(t, "erro", Options{Version: "v0.1.0", Mono: true, NoMotion: true}, tab)
}

func TestGoldenEstadoSemAdmin(t *testing.T) {
	th := Theme{Mono: true, NoMotion: true}
	for _, sz := range stateSizes() {
		tab := &fakeTab{title: "Privacidade", short: "Priv", body: strings.Join(StateNeedsAdmin(th, sz[0], sz[1]-5, "a privacidade", AdminSteps("windows", "")...), "\n")}
		m := sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, sz[0], sz[1], tab)
		golden(t, fmt.Sprintf("estado_semadmin_%dx%d", sz[0], sz[1]), m.render())
	}
}

func TestGoldenEstadoNaoSuportado(t *testing.T) {
	tab := privTab(t, &fakePriv{info: PrivInfo{Supported: false, OS: "darwin"}})
	stGolden(t, "naosuportado_darwin", Options{Version: "v0.1.0", Mono: true, NoMotion: true}, tab)
}

func TestGoldenEstadoVazio(t *testing.T) {
	tab := NewLogTab(Theme{Mono: true, NoMotion: true}, "/home/ana/.local/state/toolkit/logs")
	stGolden(t, "vazio", Options{Version: "v0.1.0", Mono: true, NoMotion: true}, tab)
}

func TestGoldenEstadoTerminalPequeno(t *testing.T) {
	for _, sz := range [][2]int{{50, 12}, {59, 16}} {
		m := sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, sz[0], sz[1], fakes()...)
		golden(t, fmt.Sprintf("estado_terminalpequeno_%dx%d", sz[0], sz[1]), m.render())
	}
}
