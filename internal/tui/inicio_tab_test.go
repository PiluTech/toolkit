package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// inicioItems espelha os sete objetivos do Início real (internal/cli/tui_inicio.go) na ordem de areaDefs.
func inicioItems(win, admin bool) []InicioItem {
	nat, priv := "", ""
	if !win {
		nat, priv = "! só Windows", "! só Windows"
	} else if !admin {
		priv = "! pede administrador para alguns itens"
	}
	return []InicioItem{
		{Title: "Instalar programas", Desc: "Escolha os programas do catálogo e instale todos de uma vez.", Area: 1, Section: 0, Dest: "1 Instalar › Apps"},
		{Title: "Preparar a máquina com uma receita", Desc: "Uma receita faz vários passos em sequência e continua após reiniciar.", Area: 1, Section: 1, Dest: "1 Instalar › Receitas"},
		{Title: "Ver a saúde da máquina", Desc: "Diagnóstico só de leitura: hardware, série, UUID, MAC e o que está ruim.", Area: 2, Section: 0, Dest: "2 Máquina › Diagnóstico"},
		{Title: "Remover apps nativos do Windows", Desc: "Remove só os apps do Windows que você escolher, depois de ver o plano.", Tag: nat, Area: 2, Section: 1, Dest: "2 Máquina › Apps nativos"},
		{Title: "Proteger a privacidade do Windows", Desc: "Ajustes de privacidade, um por um, com confirmação e como desfazer.", Tag: priv, Area: 3, Section: 0, Dest: "3 Segurança › Privacidade"},
		{Title: "Analisar a rede", Desc: "IPs, Wi-Fi, DNS e portas da máquina.", Tag: "○ em breve", Unavailable: true, Area: 4, Section: 0, Dest: "4 Rede › Análise"},
		{Title: "Ver o que já rodou", Desc: "O que já rodou e o que falhou, com o registro de cada execução.", Area: 5, Section: 0, Dest: "5 Histórico › Execuções"},
	}
}

func newInicio(win, admin bool) *InicioTab {
	return NewInicioTab(Theme{Mono: true, NoMotion: true}, "Início", "Iníc", "O que você quer fazer?", inicioItems(win, admin))
}

func areasWithInicio(o Options, win, admin bool) *Model {
	ds := areaDefs()
	ds[0].Tabs[0] = newInicio(win, admin)
	return NewWithAreas(o, ds)
}

// drive executa o comando devolvido pela raiz e entrega as mensagens de volta, como o Bubble Tea faria.
func drive(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch x := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range x {
			drive(m, c)
		}
	default:
		_, next := m.Update(x)
		drive(m, next)
	}
}

func ikey(m *Model, k string) {
	var msg tea.KeyPressMsg
	switch k {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		msg = keyShiftTab()
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "left":
		msg = tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		msg = tea.KeyPressMsg{Code: tea.KeyRight}
	default:
		msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
	_, cmd := m.Update(msg)
	drive(m, cmd)
}

func TestInicioEnterAbreAreaESecaoDoItem(t *testing.T) {
	m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
	m.Update(sizeMsg(80, 24))
	for _, c := range []struct {
		downs int
		want  int
	}{{0, idxApps}, {1, idxRec}, {2, idxDiag}, {3, idxNat}, {4, idxPriv}, {5, idxInicio}, {6, idxExec}} { // 5: "Analisar a rede" ainda não existe: não navega
		ikey(m, "0")
		ikey(m, "g")
		for i := 0; i < c.downs; i++ {
			ikey(m, "down")
		}
		ikey(m, "enter")
		if m.Active() != c.want {
			t.Errorf("%d downs + Enter: seção %d, queria %d", c.downs, m.Active(), c.want)
		}
	}
}

func TestInicioNumeroVaiDiretoAoItem(t *testing.T) {
	for n, want := range []int{idxApps, idxRec, idxDiag, idxNat, idxPriv, idxInicio, idxExec} { // 6: rede ainda não existe
		m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
		m.Update(sizeMsg(80, 24))
		ikey(m, fmt.Sprint(n+1))
		if m.Active() != want {
			t.Errorf("tecla %d: seção %d, queria %d", n+1, m.Active(), want)
		}
	}
	// 8, 9: sem item, nada acontece; 0 fica no Início
	m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
	m.Update(sizeMsg(80, 24))
	for _, k := range []string{"8", "9", "0"} {
		ikey(m, k)
		if m.Active() != idxInicio {
			t.Errorf("tecla %s saiu do Início: %d", k, m.Active())
		}
	}
}

func TestInicioMoveComSetasEJK(t *testing.T) {
	tab := newInicio(true, true)
	send := func(k string) { tab.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k}) }
	send("j")
	send("j")
	if tab.Cursor() != 2 {
		t.Fatalf("j j: %d", tab.Cursor())
	}
	send("k")
	tab.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	tab.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if tab.Cursor() != 3 {
		t.Fatalf("k, ↓, ↓: %d", tab.Cursor())
	}
	for i := 0; i < 20; i++ {
		tab.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if tab.Cursor() != 6 {
		t.Fatalf("o cursor para no último: %d", tab.Cursor())
	}
	for i := 0; i < 20; i++ {
		tab.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if tab.Cursor() != 0 {
		t.Fatalf("o cursor para no primeiro: %d", tab.Cursor())
	}
	if !strings.Contains(tab.Status(), "1 Instalar › Apps") {
		t.Errorf("status: %q", tab.Status())
	}
}

func TestInicioMoverNaoNavega(t *testing.T) {
	m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
	m.Update(sizeMsg(80, 24))
	ikey(m, "down")
	ikey(m, "j")
	ikey(m, "up")
	if m.Active() != idxInicio {
		t.Fatalf("mover não abre nada: %d", m.Active())
	}
}

func TestInicioEnterNaoAbreRevisaoNemAviso(t *testing.T) {
	m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
	m.Update(sizeMsg(80, 24))
	ikey(m, "enter")
	if m.mode != modeNav || m.notice != "" || m.review != nil {
		t.Fatalf("Enter do Início não é Revisão: mode=%v notice=%q", m.mode, m.notice)
	}
}

func TestGoToIgnoradoForaDaNavegacaoNormal(t *testing.T) {
	cases := map[string]func(m *Model){
		"revisão":  func(m *Model) { m.mode, m.review = modeReview, newReview(m.theme, 1, Selection{}) },
		"execução": func(m *Model) { m.mode = modeRun },
		"ajuda":    func(m *Model) { m.modal = modalHelp },
		"versão":   func(m *Model) { m.modal = modalUpdate },
		"pergunta": func(m *Model) { m.asking = askInterrupt },
		"capturando": func(m *Model) {
			m.tabs[idxApps] = &fakeTab{title: "Apps", short: "Apps", capturing: true}
			m.setActive(idxApps)
		},
	}
	for name, setup := range cases {
		m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
		m.Update(sizeMsg(80, 24))
		setup(m)
		before := m.Active()
		m.Update(GoToMsg{Area: 3, Section: 0})
		if m.Active() != before {
			t.Errorf("%s: GoTo trocou de seção (%d -> %d)", name, before, m.Active())
		}
	}
}

func TestGoToValidaIndices(t *testing.T) {
	m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
	m.Update(sizeMsg(80, 24))
	for _, g := range []GoToMsg{{-1, 0}, {6, 0}, {99, 0}} {
		m.Update(g)
		if m.Active() != idxInicio {
			t.Errorf("%+v saiu do Início", g)
		}
	}
	m.Update(GoToMsg{Area: 2, Section: 9}) // seção fora: abre a área na última seção visitada
	if m.Active() != idxDiag {
		t.Errorf("seção inexistente: %d", m.Active())
	}
	m.Update(GoToMsg{Area: 2, Section: 1})
	if m.Active() != idxNat {
		t.Errorf("GoTo 2/1: %d", m.Active())
	}
}

func TestGoToNoModoLegadoNaoFazNada(t *testing.T) {
	m := sized(Options{Mono: true}, 80, 24, fakes()...)
	m.Update(GoToMsg{Area: 1, Section: 0})
	if m.Active() != 0 {
		t.Fatal("modo legado ignora GoTo")
	}
}

func TestInicioNaoNavegaDuranteExecucaoEPergunta(t *testing.T) {
	for _, setup := range []func(m *Model){
		func(m *Model) { m.mode = modeRun },
		func(m *Model) { m.asking = askInterrupt },
		func(m *Model) { m.modal = modalHelp },
	} {
		m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
		m.Update(sizeMsg(80, 24))
		setup(m)
		ikey(m, "enter")
		if m.Active() != idxInicio {
			t.Errorf("Enter navegou fora da navegação normal")
		}
	}
	// em execução o número continua sendo a área (leitura), nunca o item
	m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
	m.Update(sizeMsg(80, 24))
	m.mode = modeRun
	ikey(m, "3")
	if m.Active() != idxPriv {
		t.Errorf("em execução 3 é a área Segurança: %d", m.Active())
	}
}

func TestInicioEtiquetasPorSOEAdmin(t *testing.T) {
	view := func(win, admin bool) string { return newInicio(win, admin).View(120, 40) }
	lin := view(false, false)
	if strings.Count(lin, "! só Windows") != 2 || !strings.Contains(lin, "○ em breve") {
		t.Errorf("fora do Windows:\n%s", lin)
	}
	w := view(true, false)
	if strings.Contains(w, "só Windows") || !strings.Contains(w, "! pede administrador para alguns itens") {
		t.Errorf("Windows sem admin:\n%s", w)
	}
	a := view(true, true)
	if strings.Contains(a, "só Windows") || strings.Contains(a, "pede administrador") {
		t.Errorf("Windows admin:\n%s", a)
	}
}

func TestInicioLarguras(t *testing.T) {
	for _, w := range []int{60, 80, 120} {
		for _, win := range []bool{true, false} {
			tab := newInicio(win, false)
			for cur := 0; cur < 7; cur++ {
				tab.cur = cur
				out := tab.View(w, 60)
				for _, l := range strings.Split(out, "\n") {
					if lipgloss.Width(l) > w {
						t.Fatalf("w=%d: linha larga (%d): %q", w, lipgloss.Width(l), l)
					}
				}
				if strings.Contains(out, "…") {
					t.Fatalf("w=%d: a frase nunca é cortada com reticências:\n%s", w, out)
				}
			}
			// toda a frase de cada item aparece (quebrada, mas inteira)
			flat := strings.Join(strings.Fields(strings.NewReplacer("\n", " ").Replace(tab.View(w, 60))), " ")
			for _, it := range tab.items {
				if !strings.Contains(flat, strings.Join(strings.Fields(it.Desc), " ")) {
					t.Errorf("w=%d: frase cortada ou perdida: %q", w, it.Desc)
				}
			}
		}
	}
}

func TestInicioAlturaMinimaRolaComMore(t *testing.T) {
	tab := newInicio(true, false)
	out := tab.View(60, 12)
	if n := strings.Count(out, "\n") + 1; n > 12 {
		t.Fatalf("passou da altura: %d linhas", n)
	}
	if tab.More() == 0 {
		t.Fatal("em 12 linhas sobra item abaixo da dobra")
	}
	// o cursor no último item rola até mostrá-lo inteiro
	tab.cur = 6
	out = tab.View(60, 12)
	if !strings.Contains(out, "> 7 Ver o que já rodou") || tab.More() != 0 {
		t.Fatalf("cursor no fim:\n%s\nmore=%d", out, tab.More())
	}
	// voltando ao primeiro, o cabeçalho volta
	tab.cur = 0
	out = tab.View(60, 12)
	if !strings.Contains(strings.ToLower(out), "o que você quer fazer?") || !strings.Contains(out, "> 1 Instalar programas") {
		t.Fatalf("cursor no começo:\n%s", out)
	}
	// com folga o espaço entre itens aparece e nada rola
	if big := newInicio(true, true); big.View(120, 40) == "" || big.More() != 0 {
		t.Error("em 40 linhas nada rola")
	}
}

func TestInicioMonoSimboloEPalavra(t *testing.T) {
	out := newInicio(false, false).View(80, 30)
	for _, w := range []string{"> 1 Instalar programas", "! só Windows", "○ em breve"} {
		if !strings.Contains(out, w) {
			t.Errorf("falta %q:\n%s", w, out)
		}
	}
	if ansi.Strip(out) != out {
		t.Error("Mono não leva estilo")
	}
}

func TestInicioTextoDeForaEhLimpo(t *testing.T) {
	tab := NewInicioTab(Theme{Mono: true}, "Início", "Iníc", "Pergunta\x1b[31m?", []InicioItem{
		{Title: "Item\x1b]0;x\x07 A", Desc: "frase \u202e com controle\x1b[2J", Tag: "! etiqueta\x1b[31m", Dest: "dest\x1b[0m"},
	})
	out := tab.View(80, 10) + tab.Status()
	if strings.ContainsAny(out, "\x1b\x07\u202e") {
		t.Fatalf("texto sem limpeza: %q", out)
	}
}

func TestInicioSemItensNaoQuebra(t *testing.T) {
	tab := NewInicioTab(Theme{Mono: true}, "Início", "Iníc", "Pergunta?", nil)
	tab.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	tab.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if tab.Claims("enter") || tab.Claims("1") || tab.Status() != "" || tab.View(60, 5) == "" {
		t.Fatal("lista vazia")
	}
}

func TestInicioClaimsSoEnterENumeros(t *testing.T) {
	tab := newInicio(true, true)
	for _, k := range []string{"enter", "1", "7"} {
		if !tab.Claims(k) {
			t.Errorf("deveria reclamar %q", k)
		}
	}
	for _, k := range []string{"0", "8", "9", "tab", "q", "?", "x", "up", "space"} {
		if tab.Claims(k) {
			t.Errorf("não deveria reclamar %q", k)
		}
	}
}

func TestInicioQEAjudaContinuamDaRaiz(t *testing.T) {
	m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
	m.Update(sizeMsg(80, 24))
	ikey(m, "?")
	if m.modal != modalHelp {
		t.Fatal("? abre a ajuda")
	}
	ikey(m, "esc")
	ikey(m, "q")
	if !m.Quitting() {
		t.Fatal("q sai")
	}
}

func TestInicioTerminalPequenoNaoNavega(t *testing.T) {
	m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
	m.Update(sizeMsg(50, 10))
	ikey(m, "enter")
	ikey(m, "3")
	if m.Active() != idxInicio {
		t.Fatal("terminal pequeno: nenhuma tecla age no que está oculto")
	}
}

func TestInicioGoldens(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {60, 17}} {
		m := areasWithInicio(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, true, false)
		m.Update(sizeMsg(sz[0], sz[1]))
		golden(t, fmt.Sprintf("areas_inicio_%dx%d", sz[0], sz[1]), m.render())
	}
	// fora do Windows (80x24): as etiquetas "só Windows"
	m := areasWithInicio(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, false, false)
	m.Update(sizeMsg(80, 24))
	golden(t, "areas_inicio_linux_80x24", m.render())
}

// --- integração com as seções reais (Lupa): a confirmação nunca é abandonada nem aplica por engano ---

func realAreas(t *testing.T, priv *fakePriv, nat *fakeNat) *Model {
	t.Helper()
	ctx := context.Background()
	th := Theme{Mono: true, NoMotion: true}
	ds := areaDefs()
	ds[0].Tabs[0] = newInicio(true, true)
	ds[2].Tabs[1] = NewNativeAppsTab(ctx, nat, th)
	ds[3].Tabs[0] = NewPrivacyTab(ctx, priv, th)
	m := NewWithAreas(Options{Mono: true, NoMotion: true}, ds)
	m.Update(sizeMsg(100, 30))
	drive(m, m.Init())
	return m
}

func TestIntegradoConfirmacaoDaPrivacidadeNaoEscapa(t *testing.T) {
	priv := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 1, IsAdmin: true}, applyRes: PrivResult{Applied: 1, BackupPath: `C:\b.json`}}
	nat := &fakeNat{info: natSample(true)}
	m := realAreas(t, priv, nat)
	ikey(m, "5") // Início: item 5 = Privacidade
	if m.Active() != idxPriv {
		t.Fatalf("Início 5 não abriu a Privacidade: %d", m.Active())
	}
	ikey(m, "a") // aplicar: abre a confirmação com o plano
	if !m.tabs[idxPriv].Capturing() || !strings.Contains(m.render(), "[ Cancelar ]") {
		t.Fatalf("a confirmação não abriu:\n%s", m.render())
	}
	for _, k := range []string{"0", "1", "2", "3", "4", "5", "tab", "shift+tab", "up", "down", "left", "right", "x", "u", "?"} {
		ikey(m, k)
		if m.Active() != idxPriv || !m.tabs[idxPriv].Capturing() {
			t.Fatalf("a tecla %q tirou a pessoa da confirmação (seção %d, capturing=%v)", k, m.Active(), m.tabs[idxPriv].Capturing())
		}
		if len(priv.applies) != 0 || priv.undoCalls != 0 || len(priv.applyIDs) != 0 {
			t.Fatalf("a tecla %q aplicou algo: applies=%v ids=%v undo=%d", k, priv.applies, priv.applyIDs, priv.undoCalls)
		}
	}
	// o pedido de navegação também é ignorado com a confirmação aberta
	m.Update(GoToMsg{Area: 0, Section: 0})
	if m.Active() != idxPriv || len(priv.applies) != 0 {
		t.Fatal("GoTo abandonou a confirmação")
	}
	ikey(m, "n") // cancela
	if m.tabs[idxPriv].Capturing() || len(priv.applies) != 0 || len(priv.applyIDs) != 0 {
		t.Fatalf("cancelar não pode aplicar: applies=%v ids=%v", priv.applies, priv.applyIDs)
	}
	// depois de cancelar, Tab e os números voltam a funcionar
	ikey(m, "0")
	if m.Active() != idxInicio {
		t.Fatalf("0 depois de cancelar: %d", m.Active())
	}
	ikey(m, "3")
	if m.Active() != idxDiag {
		t.Fatalf("3 no Início é o item Ver a saúde: %d", m.Active())
	}
	ikey(m, "tab")
	if m.Active() != idxNat {
		t.Fatalf("Tab depois de cancelar: %d", m.Active())
	}
	// confirmar de novo: aplica uma vez, com o ID do plano exibido
	ikey(m, "5")
	m.Update(GoToMsg{Area: 3, Section: 0})
	ikey(m, "a")
	shown := priv.lastID
	ikey(m, "s")
	if len(priv.applies) != 1 || len(priv.applyIDs) != 1 || priv.applyIDs[0] != shown {
		t.Fatalf("confirmar: applies=%v ids=%v plano exibido=%q", priv.applies, priv.applyIDs, shown)
	}
}

func TestIntegradoConfirmacaoDosAppsNativosNaoEscapa(t *testing.T) {
	priv := &fakePriv{info: privSample(true)}
	nat := &fakeNat{info: natSample(true), plan: NatPlan{ID: "plano-7", Text: natPlanText, Pending: 1, IsAdmin: true}, applyRes: NatResult{Removed: 1, BackupPath: `C:\b.json`}}
	m := realAreas(t, priv, nat)
	ikey(m, "4") // item 4 = Apps nativos
	if m.Active() != idxNat {
		t.Fatalf("Início 4: %d", m.Active())
	}
	ikey(m, "space")
	ikey(m, "r")
	if !m.tabs[idxNat].Capturing() {
		t.Fatalf("a confirmação não abriu:\n%s", m.render())
	}
	for _, k := range []string{"0", "1", "2", "3", "4", "5", "tab", "shift+tab", "up", "down", "left", "right"} {
		ikey(m, k)
		if m.Active() != idxNat || !m.tabs[idxNat].Capturing() {
			t.Fatalf("a tecla %q tirou a pessoa da confirmação (seção %d)", k, m.Active())
		}
		if len(nat.applies) != 0 {
			t.Fatalf("a tecla %q removeu algo", k)
		}
	}
	m.Update(GoToMsg{Area: 1, Section: 0})
	if m.Active() != idxNat || len(nat.applies) != 0 {
		t.Fatal("GoTo abandonou a confirmação")
	}
	ikey(m, "esc")
	if len(nat.applies) != 0 || m.tabs[idxNat].Capturing() {
		t.Fatal("Esc cancela sem remover")
	}
	ikey(m, "1")
	if m.Active() != idxApps {
		t.Fatalf("depois de cancelar os números voltam: %d", m.Active())
	}
	ikey(m, "0")
	ikey(m, "4")
	if m.Active() != idxNat {
		t.Fatalf("Início 4: %d", m.Active())
	}
	if len(m.tabs[idxNat].(*NativeAppsTab).selection().Apps) == 0 {
		ikey(m, "space")
	}
	ikey(m, "r")
	ikey(m, "s")
	if len(nat.applies) != 1 || nat.applyIDs[0] != "plano-7" {
		t.Fatalf("confirmar: applies=%v ids=%v", nat.applies, nat.applyIDs)
	}
}

// Lupa #70: objetivo indisponível ("em breve") não emite GoTo: Enter e a tecla do item ficam no Início, sem Revisão nem
// executor, e a linha de estado diz que ainda não existe.
func TestInicioItemIndisponivelNaoNavegaEDizNoStatus(t *testing.T) {
	m := areasWithInicio(Options{Mono: true, NoMotion: true}, true, true)
	m.Update(sizeMsg(80, 24))
	for _, via := range []string{"enter", "6"} {
		ikey(m, "0")
		ikey(m, "g")
		if via == "enter" {
			for i := 0; i < 5; i++ {
				ikey(m, "down")
			}
		}
		ikey(m, via)
		if m.Active() != idxInicio {
			t.Fatalf("%s no item indisponível saiu do Início: seção %d", via, m.Active())
		}
		if m.review != nil || m.mode != modeNav {
			t.Fatalf("%s não pode abrir Revisão nem executor: review=%v mode=%v", via, m.review, m.mode)
		}
		st := m.tabs[idxInicio].Status()
		if !strings.Contains(st, "ainda não existe") || strings.Contains(st, "Enter abre") {
			t.Errorf("%s: o status deve dizer que ainda não existe: %q", via, st)
		}
	}
}

func TestInicioDisponibilidadeEhCampoNaoEtiqueta(t *testing.T) {
	it := InicioItem{Title: "X", Tag: "○ em breve", Area: 1, Section: 0}
	tab := NewInicioTab(Theme{Mono: true}, "Início", "Iníc", "?", []InicioItem{it})
	if _, cmd := tab.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Error("a etiqueta sozinha não bloqueia: só o campo Unavailable")
	}
	it.Unavailable = true
	tab = NewInicioTab(Theme{Mono: true}, "Início", "Iníc", "?", []InicioItem{it})
	if _, cmd := tab.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Error("Unavailable não pode emitir GoTo")
	}
}
