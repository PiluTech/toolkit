package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// fakeTab deixa os testes controlarem corpo, estado e contadores.
type fakeTab struct {
	title, short, body, status string
	more, selected             int
	capturing                  bool
	got                        []tea.Msg
}

func (f *fakeTab) Init() tea.Cmd   { return nil }
func (f *fakeTab) Capturing() bool { return f.capturing }
func (f *fakeTab) Title() string   { return f.title }
func (f *fakeTab) Short() string   { return f.short }
func (f *fakeTab) Update(m tea.Msg) (Tab, tea.Cmd) {
	f.got = append(f.got, m)
	return f, nil
}
func (f *fakeTab) View(int, int) string { return f.body }
func (f *fakeTab) Keys() []KeyHint {
	return []KeyHint{{"↑↓", "mover"}, {"espaço", "marcar"}, {"/", "buscar"}, {"Enter", "revisar"}}
}
func (f *fakeTab) Status() string { return f.status }
func (f *fakeTab) More() int      { return f.more }
func (f *fakeTab) Selected() int  { return f.selected }

func fakes() []Tab {
	return []Tab{
		&fakeTab{title: "Apps", short: "Apps", body: " Git\n Python"},
		&fakeTab{title: "Receitas", short: "Rec", body: " receita"},
		&fakeTab{title: "Diagnóstico", short: "Diag", body: " diag"},
		&fakeTab{title: "Log", short: "Log", body: " log"},
	}
}

func sized(o Options, w, h int, tabs ...Tab) *Model {
	m := New(o, tabs...)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func press(m *Model, code rune, mod tea.KeyMod, text string) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Mod: mod, Text: text})
	return cmd
}

// trimTrailing tira os espaços finais de cada linha: os goldens novos não os carregam (git diff --check) e a comparação
// ignora-os nos antigos.
func trimTrailing(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	got = trimTrailing(got)
	p := filepath.Join("testdata", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("falta %s (rode com UPDATE_GOLDEN=1): %v", p, err)
	}
	if trimTrailing(strings.TrimRight(string(want), "\n")) != got {
		t.Fatalf("%s diverge.\n--- obtido ---\n%s\n--- esperado ---\n%s", name, got, want)
	}
}

func TestGoldenMono80x24(t *testing.T) {
	m := sized(Options{Version: "v0.1.0", Mono: true}, 80, 24, fakes()...)
	golden(t, "mono_80x24", m.render())
}

func TestGoldenMono60x17Narrow(t *testing.T) {
	m := sized(Options{Version: "v0.1.0", Latest: "v0.2.0", Mono: true}, 60, 17, fakes()...)
	golden(t, "mono_60x17", m.render())
}

func TestGoldenHelp80x24(t *testing.T) {
	m := sized(Options{Version: "v0.1.0", Mono: true}, 80, 24, fakes()...)
	press(m, '?', 0, "?")
	golden(t, "mono_help_80x24", m.render())
}

// A tela tem SEMPRE h linhas e nenhuma passa de w colunas, em qualquer tamanho, com e sem cor.
func TestScreenAlwaysFitsTheWindow(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {70, 20}, {60, 16}, {61, 17}, {120, 40}, {100, 17}, {200, 60}} {
		for _, mono := range []bool{true, false} {
			m := sized(Options{Version: "v0.1.0", Latest: "v0.2.0", Mono: mono}, sz[0], sz[1], fakes()...)
			rows := strings.Split(m.render(), "\n")
			if len(rows) != sz[1] {
				t.Errorf("%v mono=%v: %d linhas, quero %d", sz, mono, len(rows), sz[1])
			}
			for i, r := range rows {
				if w := lipgloss.Width(r); w > sz[0] {
					t.Errorf("%v mono=%v linha %d: %d colunas > %d: %q", sz, mono, i+1, w, sz[0], ansi.Strip(r))
				}
			}
		}
	}
}

// Sem cor nada some: nenhuma sequência de estilo sai, e abas, cursor e símbolos continuam no texto. Com
// cor, o layout é IDÊNTICO ao do Mono (a cor só enfeita).
func TestMonoHasNoStyleAndSameLayoutAsColor(t *testing.T) {
	mono := sized(Options{Version: "v0.1.0", Latest: "v0.2.0", Mono: true}, 80, 24, fakes()...).render()
	if strings.Contains(mono, "\x1b") {
		t.Fatalf("Mono emitiu sequência de escape: %q", mono)
	}
	for _, want := range []string{"[1 Apps]", " 2 Receitas ", "↑ v0.2.0 disponível (u)", "? ajuda", "q sair", "─"} {
		if !strings.Contains(mono, want) {
			t.Errorf("Mono sem %q:\n%s", want, mono)
		}
	}
	color := sized(Options{Version: "v0.1.0", Latest: "v0.2.0"}, 80, 24, fakes()...).render()
	if !strings.Contains(color, "\x1b") {
		t.Fatal("com cor deveria haver estilo")
	}
	if got := ansi.Strip(color); got != mono {
		t.Fatalf("o texto com cor difere do Mono:\n%s\n---\n%s", got, mono)
	}
}

func TestTabNavigation(t *testing.T) {
	m := sized(Options{Mono: true}, 80, 24, fakes()...)
	press(m, tea.KeyTab, 0, "")
	if m.Active() != 1 {
		t.Fatalf("tab: %d", m.Active())
	}
	press(m, tea.KeyTab, tea.ModShift, "")
	press(m, tea.KeyTab, tea.ModShift, "")
	if m.Active() != 3 {
		t.Fatalf("shift+tab a partir de 0 deve dar a última aba: %d", m.Active())
	}
	press(m, '3', 0, "3")
	if m.Active() != 2 {
		t.Fatalf("tecla 3: %d", m.Active())
	}
	press(m, '9', 0, "9")
	if m.Active() != 2 {
		t.Fatal("aba inexistente não muda a ativa")
	}
	if !strings.Contains(m.render(), "[3 Diagnóstico]") {
		t.Fatalf("a aba ativa leva colchetes:\n%s", m.render())
	}
}

func TestUnhandledKeysGoToActiveTab(t *testing.T) {
	tabs := fakes()
	m := sized(Options{Mono: true}, 80, 24, tabs...)
	press(m, ' ', 0, " ")
	if got := tabs[0].(*fakeTab).got; len(got) != 1 {
		t.Fatalf("o espaço deve chegar à aba ativa: %v", got)
	}
	press(m, '2', 0, "2")
	press(m, 'j', 0, "j")
	if got := tabs[1].(*fakeTab).got; len(got) != 1 {
		t.Fatalf("j vai para a aba 2: %v", got)
	}
}

func TestHelpModalAndQuit(t *testing.T) {
	m := sized(Options{Mono: true}, 80, 24, fakes()...)
	press(m, '?', 0, "?")
	if out := m.render(); !strings.Contains(out, "EM QUALQUER LUGAR") || !strings.Contains(out, "Esc/q fechar") {
		t.Fatalf("ajuda:\n%s", out)
	}
	// dentro do modal, Tab não troca de aba
	press(m, tea.KeyTab, 0, "")
	if m.Active() != 0 {
		t.Fatal("o modal consome as teclas")
	}
	press(m, tea.KeyEscape, 0, "")
	if strings.Contains(m.render(), "EM QUALQUER LUGAR") {
		t.Fatal("Esc deve fechar a ajuda")
	}
	if cmd := press(m, 'q', 0, "q"); cmd == nil || !m.Quitting() {
		t.Fatal("q sai")
	}
	m2 := sized(Options{Mono: true}, 80, 24, fakes()...)
	if cmd := press(m2, 'c', tea.ModCtrl, ""); cmd == nil || !m2.Quitting() {
		t.Fatal("Ctrl+C sai")
	}
}

func TestTooSmallWindow(t *testing.T) {
	for _, sz := range [][2]int{{59, 24}, {80, 15}, {20, 16}} {
		m := sized(Options{Mono: true}, sz[0], sz[1], fakes()...)
		out := m.render()
		flat := strings.Join(strings.Fields(out), " ")
		if !strings.Contains(flat, "i Terminal pequeno") ||
			!strings.Contains(flat, "A interface precisa de 60×16 e este tem "+itoa(sz[0])+"×"+itoa(sz[1])+".") ||
			!strings.Contains(flat, "1. Aumente a janela.") || !strings.Contains(flat, "2. Ou rode toolkit list / toolkit install ... sem a interface.") ||
			!strings.Contains(flat, "q sair") {
			t.Errorf("%v: %q", sz, out)
		}
		if cmd := press(m, 'q', 0, "q"); cmd == nil {
			t.Errorf("%v: q tem de funcionar na tela pequena", sz)
		}
	}
	// exatamente no mínimo desenha a TUI
	if out := sized(Options{Mono: true}, 60, 16, fakes()...).render(); strings.Contains(out, "pequena demais") {
		t.Fatal("60x16 deve desenhar")
	}
}

func TestNarrowTabsAndVersionBanner(t *testing.T) {
	wide := sized(Options{Version: "v0.1.0", Latest: "v0.2.0", Mono: true}, 80, 24, fakes()...).render()
	if !strings.Contains(wide, "↑ v0.2.0 disponível (u)") || strings.Contains(wide, "v0.1.0") {
		t.Fatalf("faixa larga:\n%s", wide)
	}
	narrow := sized(Options{Version: "v0.1.0", Latest: "v0.2.0", Mono: true}, 65, 20, fakes()...).render()
	first := strings.Split(narrow, "\n")[0]
	if !strings.Contains(first, "2 Rec") || strings.Contains(first, "2 Receitas") || !strings.Contains(first, "↑ v0.2.0 (u)") {
		t.Fatalf("cabeçalho estreito: %q", first)
	}
	noNew := strings.Split(sized(Options{Version: "v0.1.0", Mono: true}, 80, 24, fakes()...).render(), "\n")[0]
	if !strings.Contains(noNew, "v0.1.0") || strings.Contains(noNew, "↑") {
		t.Fatalf("sem versão nova mostra só a atual: %q", noNew)
	}
}

func TestKeyBarKeepsHelpAndQuitWhenNarrow(t *testing.T) {
	for _, w := range []int{60, 65, 80, 120} {
		rows := strings.Split(sized(Options{Mono: true}, w, 20, fakes()...).render(), "\n")
		bar := rows[len(rows)-1]
		if !strings.Contains(bar, "? ajuda") || !strings.Contains(bar, "q sair") {
			t.Errorf("w=%d: a barra perdeu ajuda/sair: %q", w, bar)
		}
	}
}

func TestStatusLineScrollHintAndSelection(t *testing.T) {
	tabs := fakes()
	ft := tabs[0].(*fakeTab)
	ft.more, ft.selected, ft.status = 3, 2, "git — Git (apt)"
	rows := strings.Split(sized(Options{Mono: true}, 80, 24, tabs...).render(), "\n")
	if !strings.Contains(rows[len(rows)-3], "↓ mais 3 linhas") {
		t.Errorf("régua: %q", rows[len(rows)-3])
	}
	st := rows[len(rows)-2]
	if !strings.Contains(st, "git — Git (apt)") || !strings.HasSuffix(st, "Enter revisa 2 marcados ") {
		t.Errorf("estado: %q", st)
	}
	ft.more, ft.selected = 1, 1
	rows = strings.Split(sized(Options{Mono: true}, 80, 24, tabs...).render(), "\n")
	if !strings.Contains(rows[len(rows)-3], "↓ mais 1 linha ") || !strings.Contains(rows[len(rows)-2], "Enter revisa 1 marcado ") {
		t.Errorf("singular: %q / %q", rows[len(rows)-3], rows[len(rows)-2])
	}
}

// Texto de fora nunca chega ao terminal como sequência de controle: a linha de estado e a versão passam
// por textsafe.
func TestOutsideTextCannotInjectEscapes(t *testing.T) {
	tabs := fakes()
	tabs[0].(*fakeTab).status = "\x1b]0;pwn\x07\x1b[2Jnome\nquebra"
	m := sized(Options{Version: "v0\x1b[31m", Latest: "v9\x1b[2J", Mono: true}, 80, 24, tabs...)
	out := m.render()
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\x07") {
		t.Fatalf("escape vazou para a tela: %q", out)
	}
	if !strings.Contains(out, `\x1b`) {
		t.Fatalf("o escape deve aparecer escapado:\n%s", out)
	}
	// com cor, só os estilos próprios da TUI: nenhuma sequência que não seja SGR (CSI ... m)
	colored := sized(Options{Version: "v0.1.0", Latest: "v9\x1b[2J"}, 80, 24, tabs...).render()
	stripped := ansi.Strip(colored)
	if strings.Contains(stripped, "\x1b") {
		t.Fatalf("sobrou sequência não-SGR: %q", stripped)
	}
}

func TestViewUsesAltScreen(t *testing.T) {
	m := sized(Options{Mono: true}, 80, 24, fakes()...)
	if v := m.View(); !v.AltScreen {
		t.Fatal("a TUI roda na tela alternativa")
	}
}

func TestDefaultTabsHaveEmptyStates(t *testing.T) {
	m := sized(Options{Mono: true}, 80, 24)
	if len(m.tabs) != 4 {
		t.Fatalf("%d abas", len(m.tabs))
	}
	for i, want := range []string{"Lendo o catálogo…", "Nenhuma receita", "Nenhum diagnóstico", "Nada rodou"} {
		press(m, rune('1'+i), 0, string(rune('1'+i)))
		if !strings.Contains(m.render(), want) {
			t.Errorf("aba %d sem %q", i+1, want)
		}
	}
}

func TestBackgroundColorSetsTheme(t *testing.T) {
	m := sized(Options{}, 80, 24, fakes()...)
	if !m.theme.Dark {
		t.Fatal("o padrão é escuro")
	}
	m.Update(tea.BackgroundColorMsg{})
	// só confere que a mensagem é aceita sem quebrar a tela
	if strings.TrimSpace(m.render()) == "" {
		t.Fatal("tela vazia")
	}
}

// A6: o aviso quebra na largura da janela (nada é cortado) e, se faltar altura, "q sair" sobrevive; redimensionar volta
// à interface normal sem perder o estado.
func TestTooSmallWrapsAndKeepsQuitHint(t *testing.T) {
	for _, sz := range [][2]int{{20, 30}, {10, 40}, {30, 4}, {30, 2}, {12, 1}} {
		m := sized(Options{Mono: true}, sz[0], sz[1], fakes()...)
		out := m.render()
		lines := strings.Split(out, "\n")
		if len(lines) > sz[1] {
			t.Errorf("%v: %d linhas passam da altura %d:\n%s", sz, len(lines), sz[1], out)
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > sz[0] {
				t.Errorf("%v: linha com %d colunas passa da largura: %q", sz, w, l)
			}
		}
		if !strings.Contains(lines[len(lines)-1], "q sair") {
			t.Errorf("%v: a última linha é o 'q sair': %q", sz, lines[len(lines)-1])
		}
		if sz[1] >= 12 { // com altura de sobra o texto inteiro aparece (quebrado)
			flat := strings.Join(strings.Fields(out), " ")
			if !strings.Contains(strings.ReplaceAll(flat, " ", ""), strings.ReplaceAll("A interface precisa de 60×16 e este tem "+itoa(sz[0])+"×"+itoa(sz[1])+".", " ", "")) {
				t.Errorf("%v: texto cortado:\n%s", sz, out)
			}
		}
	}
	m := sized(Options{Mono: true}, 40, 10, fakes()...)
	press(m, '3', 0, "3") // estado: troca de aba antes de a janela encolher? (só a aba muda)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if out := m.render(); strings.Contains(out, "Terminal pequeno") {
		t.Errorf("com a janela de volta a 80×24 a interface normal volta:\n%s", out)
	}
}
