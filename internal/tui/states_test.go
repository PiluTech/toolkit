package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Integração dos estados padronizados (stateview.go) nas abas reais: cada aba mostra a forma fixa
// (título com símbolo e palavra, frase, passos, "Nada foi alterado." no erro e na recusa).

func stFlat(s string) string { return strings.Join(strings.Fields(plain(s)), " ") }

func wantAll(t *testing.T, what, got string, parts ...string) {
	t.Helper()
	flat := stFlat(got)
	for _, p := range parts {
		if !strings.Contains(flat, p) {
			t.Errorf("%s: falta %q:\n%s", what, p, got)
		}
	}
}

func TestEstadosAppsCarregandoErroVazio(t *testing.T) {
	tab := NewAppsTab(context.Background(), &fakeBackend{}, Theme{Mono: true, NoMotion: true}, "linux")
	wantAll(t, "apps carregando", tab.View(80, 19), "… Carregando", "Lendo o catálogo…")

	tab = NewAppsTab(context.Background(), &fakeBackend{}, Theme{Mono: true}, "linux")
	if a, b := tab.View(80, 19), (func() string { tab.frame++; return tab.View(80, 19) })(); a == b {
		t.Error("com movimento o quadro do spinner muda a cada tick")
	}

	tab = newApps(&fakeBackend{loadErr: errors.New("cat.yaml:3:5: categoria inválida")})
	tab.Update(tab.loadCmd()())
	wantAll(t, "apps erro", tab.View(80, 19), "✗ Erro", "Não deu para ler o catálogo.", "Motivo: cat.yaml:3:5", "1. Corrija o arquivo", "Nada foi alterado.")

	tab = newApps(&fakeBackend{})
	tab.Update(tab.loadCmd()())
	wantAll(t, "apps vazio", tab.View(80, 19), "i Nada por aqui", "Nenhum app do catálogo tem instalador para Linux.", "1. Veja o que o catálogo cobre: toolkit list --os")
	if strings.Contains(plain(tab.View(80, 19)), "Nada foi alterado.") {
		t.Error("vazio não é erro")
	}
}

func TestEstadosReceitasEDiagnostico(t *testing.T) {
	for _, c := range []struct {
		diag        bool
		load, empty string
		step        string
	}{
		{false, "Lendo receitas e estado…", "Nenhuma receita para este sistema.", "toolkit recipe list"},
		{true, "Lendo diagnósticos e estado…", "Nenhum diagnóstico para este sistema.", "toolkit diagnostico --help"},
	} {
		tab := NewRecipesTab(context.Background(), &fakeRecipes{}, Theme{Mono: true, NoMotion: true}, c.diag)
		wantAll(t, "carregando", tab.View(80, 19), "… Carregando", c.load)
		tab.Update(recipesLoadedMsg{data: RecipesData{}})
		wantAll(t, "vazio", tab.View(80, 19), "i Nada por aqui", c.empty, "1. ", c.step)
		tab.Update(recipesLoadedMsg{err: errors.New("x.yaml:1:1: ruim")})
		wantAll(t, "erro", tab.View(80, 19), "✗ Erro", "Motivo: x.yaml:1:1: ruim", "Nada foi alterado.")
	}
}

func TestEstadosMaquinaCarregandoComEscEErroComC(t *testing.T) {
	fm := &fakeMachine{err: errors.New("falhou")}
	d := NewDiagTab(context.Background(), &fakeRecipes{data: recipeSample()}, fm, Theme{Mono: true, NoMotion: true}, diagfmt.PT)
	d.Update(recipesLoadedMsg{data: recipeSample()})
	cmd := dpress(d, "m")
	wantAll(t, "máquina carregando", d.View(80, 12), "… Carregando", "Lendo a máquina…", "Esc cancela.")
	settle(t, d, cmd)
	wantAll(t, "máquina erro", d.View(80, 12), "✗ Erro", "Não deu para ler a máquina.", "Motivo: falhou", "1. Tente de novo (c).", "Nada foi alterado.")
	// ainda não lida (depois de cancelar com Esc): a dica de como ler
	d.loaded, d.err = false, nil
	wantAll(t, "máquina não lida", d.View(80, 12), "i Nada por aqui", "Esta máquina ainda não foi lida.", "1. Tecle c para ler a máquina.")
}

func TestEstadosPrivacidadeEAppsNativosForaDoWindowsPorSistema(t *testing.T) {
	for _, c := range []struct{ goos, nome string }{{"darwin", "macOS"}, {"linux", "Linux"}} {
		p := privTab(t, &fakePriv{info: PrivInfo{Supported: false, OS: c.goos}})
		wantAll(t, "privacidade "+c.goos, p.View(80, 14), "i Não disponível neste sistema",
			"A privacidade: só existe no Windows (este sistema é "+c.nome+").", "No "+c.nome, "Nada foi alterado.")
		n := natTab(t, &fakeNat{info: NatInfo{Supported: false, OS: c.goos}})
		wantAll(t, "apps nativos "+c.goos, n.View(80, 14), "i Não disponível neste sistema",
			"Os apps nativos: só existe no Windows (este sistema é "+c.nome+").", "No "+c.nome, "Nada foi alterado.")
	}
}

func TestEstadosPrivacidadeEAppsNativosCarregandoEErro(t *testing.T) {
	still := Theme{Mono: true, NoMotion: true}
	wantAll(t, "privacidade carregando", NewPrivacyTab(context.Background(), &fakePriv{}, still).View(80, 14), "… Carregando", "Lendo o estado de privacidade…")
	wantAll(t, "nativos carregando", NewNativeAppsTab(context.Background(), &fakeNat{}, still).View(80, 14), "… Carregando", "Lendo os apps nativos…")
	wantAll(t, "privacidade erro", privTab(t, &fakePriv{loadErr: errors.New("sem powershell")}).View(80, 14), "✗ Erro", "Não deu para ler a privacidade.", "Motivo: sem powershell", "Nada foi alterado.")
	wantAll(t, "nativos erro", natTab(t, &fakeNat{loadErr: errors.New("sem powershell")}).View(80, 14), "✗ Erro", "Não deu para ler os apps nativos.", "Motivo: sem powershell", "Nada foi alterado.")
}

func TestEstadosSemAdminNaFaixaDeResultado(t *testing.T) {
	for _, l := range [][]string{adminResultLines()} {
		flat := strings.Join(l, " ")
		for _, w := range []string{"Nada foi alterado.", "1. Feche o toolkit (q).", "Botão direito no Terminal > Executar como administrador", "O toolkit nunca eleva sozinho."} {
			if !strings.Contains(flat, w) {
				t.Errorf("falta %q em %v", w, l)
			}
		}
		if len(l) > 4 {
			t.Errorf("a faixa aceita até 4 linhas: %d", len(l))
		}
	}
}

func TestEstadosHistoricoVazioERede(t *testing.T) {
	lt := NewLogTab(Theme{Mono: true, NoMotion: true}, "/home/ana/.local/state/toolkit/logs")
	wantAll(t, "histórico", lt.View(80, 12), "i Nada por aqui", "Nada rodou nesta sessão.", "1. Marque apps ou receitas e tecle Enter.", "2. Logs anteriores: /home/ana/.local/state/toolkit/logs")
	wantAll(t, "histórico sem pasta", NewLogTab(Theme{Mono: true}, "").View(80, 12), "Nada rodou nesta sessão.", "1. Marque apps")
	rede := newNet(&fakeNet{rep: netReport()})
	wantAll(t, "rede", rede.View(80, 10), "i Nada por aqui", "A rede ainda não foi lida.", "1. Tecle c para ler a rede")
	// o InfoTab (seção só de texto) continua valendo para a próxima função que ainda não existir
	soon := NewInfoTab(Theme{Mono: true}, "Análise", "Anál", "Esta função ainda não existe nesta versão.")
	wantAll(t, "em breve", soon.View(80, 10), "i Ainda não existe nesta versão", "Esta função ainda não existe nesta versão.")
}

func TestEstadosDefaultTabs(t *testing.T) {
	m := sized(Options{Mono: true, NoMotion: true}, 80, 24)
	for i, want := range []string{"… Carregando", "i Nada por aqui", "i Nada por aqui", "i Nada por aqui"} {
		press(m, rune('1'+i), 0, string(rune('1'+i)))
		wantAll(t, fmt.Sprintf("aba %d", i+1), m.render(), want)
	}
}

func TestEstadosPiorCasoNaoQuebraOLayout(t *testing.T) {
	evil := "\x1b[2J\x1b]0;preso\x07 \u202e" + strings.Repeat("Nome-Muito-Longo-Sem-Espacos-", 12) + "\r\n\x00"
	rt := newNet(&fakeNet{err: errors.New(evil)})
	netSettle(t, rt, netKey(rt, "c"))
	tabs := []Tab{
		newApps(&fakeBackend{loadErr: errors.New(evil)}),
		privTab(t, &fakePriv{loadErr: errors.New(evil)}),
		natTab(t, &fakeNat{info: NatInfo{Supported: false, OS: evil, Reason: evil}}),
		NewInfoTab(Theme{Mono: true}, "Análise", "Anál", evil),
		rt,
		NewLogTab(Theme{Mono: true}, evil),
	}
	tabs[0].(*AppsTab).Update(tabs[0].(*AppsTab).loadCmd()())
	for _, sz := range [][2]int{{60, 16}, {60, 17}, {70, 20}, {80, 24}} {
		for i, tb := range tabs {
			m := sized(Options{Mono: true, NoMotion: true}, sz[0], sz[1], tb)
			out := m.render()
			rows := strings.Split(out, "\n")
			if len(rows) != sz[1] {
				t.Errorf("aba %d %v: %d linhas, quer %d", i, sz, len(rows), sz[1])
			}
			for _, r := range rows {
				if w := len([]rune(plain(r))); w > sz[0]*2 { // grosseiro; a largura exata é conferida em stateview_test
					t.Errorf("aba %d %v: linha larga: %q", i, sz, r)
				}
			}
			for _, bad := range []string{"\x1b[2J", "\x07", "\u202e", "\x00"} {
				if strings.Contains(out, bad) {
					t.Errorf("aba %d %v: %q chegou à tela", i, sz, bad)
				}
			}
		}
	}
}

// Terminal pequeno: abaixo do mínimo a raiz mostra a forma fixa e q continua saindo.
func TestEstadosTerminalPequenoNaRaiz(t *testing.T) {
	m := sized(Options{Mono: true, NoMotion: true}, 50, 12, fakes()...)
	wantAll(t, "tooSmall", m.render(), "i Terminal pequeno", "A interface precisa de 60×16 e este tem 50×12.", "1. Aumente a janela.", "2. Ou rode toolkit list / toolkit install ... sem a interface.", "q sair")
	if cmd := press(m, 'q', 0, "q"); cmd == nil || !m.Quitting() {
		t.Fatal("q sai da tela pequena")
	}
	m2 := sized(Options{Mono: true}, 50, 12, fakes()...)
	if cmd := press(m2, 'c', tea.ModCtrl, ""); cmd == nil || !m2.Quitting() {
		t.Fatal("Ctrl+C sai da tela pequena")
	}
}

func stripANSI(s string) string { return ansi.Strip(s) }
