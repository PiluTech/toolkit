package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// rpump roda os comandos (e os de um Batch) e entrega as mensagens à RAIZ, como o Bubble Tea faria.
func rpump(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			rpump(m, c)
		}
	default:
		_, next := m.Update(msg)
		rpump(m, next)
	}
}

func rkey(m *Model, k string) {
	var msg tea.KeyPressMsg
	switch k {
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	default:
		msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
	_, cmd := m.Update(msg)
	rpump(m, cmd)
}

func rresize(m *Model, w, h int) { m.Update(tea.WindowSizeMsg{Width: w, Height: h}) }

func rscreen(m *Model) string { return ansi.Strip(m.render()) }

// resizeLongPlan é um plano longo, com marcadores únicos na primeira e na última linha.
func resizeLongPlan() string {
	var b strings.Builder
	b.WriteString("PRIMEIRA-LINHA-DO-PLANO\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "  item %02d [exige administrador]\n", i)
	}
	b.WriteString("ULTIMA-LINHA-DO-PLANO\n")
	return b.String()
}

// rreachable rola a confirmação e devolve tudo o que apareceu na tela.
func rreachable(m *Model) string {
	for i := 0; i < 80; i++ { // volta ao topo
		rkey(m, "up")
	}
	var all strings.Builder
	all.WriteString(rscreen(m))
	for i := 0; i < 80; i++ {
		rkey(m, "down")
		all.WriteString(rscreen(m))
	}
	return all.String()
}

// Lupa #67: em 60x16 (mínimo suportado) TODAS as linhas da evidência são alcançáveis por rolagem, mesmo com avisos longos.
func TestConfirmacao60x16TodasAsLinhasDoPlanoAlcancaveis(t *testing.T) {
	be := &fakePriv{info: privSample(false), plan: PrivPlan{Text: resizeLongPlan(), Pending: 3, NeedsAdmin: true, IsAdmin: false}}
	tab := NewPrivacyTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	m := New(Options{Mono: true, NoMotion: true}, tab)
	rresize(m, 60, 16)
	rpump(m, tab.Init())
	rkey(m, "a")
	if !tab.Capturing() {
		t.Fatal("a confirmação devia estar aberta")
	}
	all := rreachable(m)
	for _, want := range []string{"PRIMEIRA-LINHA-DO-PLANO", "ULTIMA-LINHA-DO-PLANO", "item 15"} {
		if !strings.Contains(all, want) {
			t.Errorf("%q inalcançável em 60x16", want)
		}
	}
}

func TestConfirmacaoNativosTodosUsuarios60x16TodasAsLinhasAlcancaveis(t *testing.T) {
	be := &fakeNat{info: natSample(true), plan: NatPlan{Text: resizeLongPlan(), Pending: 3, NeedsAdmin: true, IsAdmin: true}}
	tab := NewNativeAppsTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	m := New(Options{Mono: true, NoMotion: true}, tab)
	rresize(m, 60, 16)
	rpump(m, tab.Init())
	rkey(m, "space")
	rkey(m, "u")
	rkey(m, "r")
	if !tab.Capturing() {
		t.Fatal("a confirmação devia estar aberta")
	}
	all := rreachable(m)
	for _, want := range []string{"PRIMEIRA-LINHA-DO-PLANO", "ULTIMA-LINHA-DO-PLANO", "item 15"} {
		if !strings.Contains(all, want) {
			t.Errorf("%q inalcançável em 60x16", want)
		}
	}
}

// Lupa #67: abrir em tamanho normal e ENCOLHER (a raiz só mostra "terminal pequeno"): s/y/Tab+Enter não podem aplicar
// enquanto a confirmação está oculta; ampliar e confirmar aplica o plano EXIBIDO (mesmo ID).
func TestConfirmacaoOcultaPorTerminalPequenoNuncaAplica_Privacidade(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 2}, applyRes: PrivResult{Applied: 2}}
	tab := NewPrivacyTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	m := New(Options{Mono: true, NoMotion: true}, tab)
	rresize(m, 80, 24)
	rpump(m, tab.Init())
	rkey(m, "a")
	if !tab.Capturing() {
		t.Fatal("a confirmação devia estar aberta")
	}
	shown := be.lastID
	rresize(m, 50, 10) // abaixo do mínimo: só a mensagem de terminal pequeno
	for _, k := range []string{"s", "y", "tab", "enter", "S", "Y"} {
		rkey(m, k)
	}
	if len(be.applies) != 0 || be.undoCalls != 0 || len(be.applyIDs) != 0 {
		t.Fatalf("aplicou com a confirmação oculta: applies=%v ids=%v", be.applies, be.applyIDs)
	}
	rresize(m, 80, 24)
	rkey(m, "s")
	if len(be.applyIDs) != 1 || be.applyIDs[0] != shown || shown == "" {
		t.Fatalf("depois de ampliar, confirmar mantém o ID exibido: ids=%v exibido=%q", be.applyIDs, shown)
	}
}

func TestConfirmacaoOcultaPorTerminalPequenoNuncaAplica_Nativos(t *testing.T) {
	be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "plano-exibido", Text: natPlanText, Pending: 1, IsAdmin: true}, applyRes: NatResult{Removed: 1}}
	tab := NewNativeAppsTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	m := New(Options{Mono: true, NoMotion: true}, tab)
	rresize(m, 80, 24)
	rpump(m, tab.Init())
	rkey(m, "space")
	rkey(m, "r")
	if !tab.Capturing() {
		t.Fatal("a confirmação devia estar aberta")
	}
	rresize(m, 50, 10)
	for _, k := range []string{"s", "y", "tab", "enter", "S", "Y"} {
		rkey(m, k)
	}
	if len(be.applies) != 0 {
		t.Fatalf("aplicou com a confirmação oculta: %v", be.applyIDs)
	}
	rresize(m, 80, 24)
	rkey(m, "s")
	if len(be.applyIDs) != 1 || be.applyIDs[0] != "plano-exibido" {
		t.Fatalf("depois de ampliar, confirmar mantém o ID exibido: %v", be.applyIDs)
	}
}

// Lupa #82: a mesma classe de bug (Load falhou -> plano/confirmação invisíveis aplicados por s/y/Enter) nas outras abas
// com confirmação. Ambas já recusam as teclas com a tela sem uso (usable); estes testes travam isso no Model real.
func TestLoadFalhouNaoAplicaPlanoInvisivelPrivacidadeENativos(t *testing.T) {
	keys := []string{"s", "y", "S", "Y", "tab", "enter"}
	t.Run("privacidade", func(t *testing.T) {
		be := &fakePriv{info: privSample(false), loadErr: errors.New("falha de leitura"), plan: PrivPlan{Text: planText, Pending: 2}, undoPlan: PrivPlan{Text: planText, Pending: 2}}
		tab := NewPrivacyTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
		m := NewWithAreas(Options{Mono: true, NoMotion: true}, []AreaDef{{Key: "seguranca", Title: "Segurança", Short: "Seg", Tabs: []Tab{tab}}})
		rresize(m, 100, 30)
		rpump(m, tab.Init())
		for _, k := range append([]string{"a", "d"}, keys...) {
			rkey(m, k)
		}
		if len(be.applies) != 0 || be.undoCalls != 0 || tab.Capturing() {
			t.Fatalf("aplicou com a tela de erro: %v %d", be.applies, be.undoCalls)
		}
	})
	t.Run("nativos", func(t *testing.T) {
		be := &fakeNat{info: natSample(true), loadErr: errors.New("falha de leitura"), plan: NatPlan{Text: planText, Pending: 2}}
		tab := NewNativeAppsTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
		m := NewWithAreas(Options{Mono: true, NoMotion: true}, []AreaDef{{Key: "maquina", Title: "Máquina", Short: "Máq", Tabs: []Tab{tab}}})
		rresize(m, 100, 30)
		rpump(m, tab.Init())
		for _, k := range append([]string{"space", "r", "u"}, keys...) {
			rkey(m, k)
		}
		if len(be.applies) != 0 || tab.Capturing() {
			t.Fatalf("aplicou com a tela de erro: %v", be.applies)
		}
	})
}
