package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type fakeRecipes struct {
	data RecipesData
	err  error
}

func (f *fakeRecipes) Load(context.Context) (RecipesData, error) { return f.data, f.err }

func recipeSample() RecipesData {
	return RecipesData{
		Recipes: []RecipeInfo{
			{ID: "wsl-dev", Name: "Ambiente WSL para desenvolvimento", Description: "Instala o WSL e as ferramentas.", RequiresAdmin: true, Steps: []StepInfo{
				{ID: "feature", Name: "Habilitar o WSL", Reboot: "required"},
				{ID: "distro", Name: "Instalar a distro"},
				{ID: "tools", Name: "Instalar ferramentas", Reboot: "never"},
			}},
			{ID: "linux-dev", Name: "Ambiente Linux", Steps: []StepInfo{{ID: "pkgs", Name: "Pacotes"}}},
			{ID: "diag-memoria", Name: "Diagnóstico de memória", Diagnostic: true, RequiresAdmin: true, Steps: []StepInfo{
				{ID: "ram", Name: "Informações de RAM"}, {ID: "mdsched", Name: "Agendar o diagnóstico", Reboot: "required"},
			}},
			{ID: "diag-rede", Name: "Diagnóstico de rede", Diagnostic: true, Steps: []StepInfo{{ID: "net", Name: "Informações de rede"}}},
		},
		Runs: map[string]RecipeRun{
			"wsl-dev": {Status: "awaiting-reboot", Steps: map[string]StepRun{
				"feature": {Status: "awaiting-reboot"}, "distro": {Status: "pending"}, "tools": {Status: "pending"}}},
			"linux-dev": {Status: "failed", Steps: map[string]StepRun{"pkgs": {Status: "failed", Error: "apt-get saiu com 100. Veja o log."}}},
		},
	}
}

func loadedRecipes(t *testing.T, d RecipesData, diag bool) *RecipesTab {
	t.Helper()
	tab := NewRecipesTab(context.Background(), &fakeRecipes{data: d}, Theme{Mono: true}, diag)
	tab.lastH = 19
	tab.Update(recipesLoadedMsg{data: d})
	return tab
}

func rsend(tab *RecipesTab, keys ...string) {
	for _, k := range keys {
		switch k {
		case "up":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		case "down":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		case "right":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		case "left":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
		case "esc":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		case "enter":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		case "backspace":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		case "space":
			tab.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
		default:
			tab.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
		}
	}
}

func TestRecipesTabsSplitByKind(t *testing.T) {
	r := loadedRecipes(t, recipeSample(), false)
	d := loadedRecipes(t, recipeSample(), true)
	if len(r.items) != 2 || len(d.items) != 2 {
		t.Fatalf("receitas=%d diagnósticos=%d", len(r.items), len(d.items))
	}
	if r.Title() != "Receitas" || r.Short() != "Rec" || d.Title() != "Diagnóstico" || d.Short() != "Diag" {
		t.Fatal("títulos")
	}
	rv, dv := plain(r.View(80, 19)), plain(d.View(80, 19))
	if strings.Contains(rv, "Diagnóstico de") || !strings.Contains(dv, "Diagnóstico de memória") || strings.Contains(dv, "WSL") {
		t.Fatalf("filtro por tipo:\n%s\n---\n%s", rv, dv)
	}
}

func TestRecipesLoadingStateAndSpinner(t *testing.T) {
	tab := NewRecipesTab(context.Background(), &fakeRecipes{}, Theme{Mono: true}, false)
	if v := plain(tab.View(80, 19)); !strings.Contains(v, "| Carregando") || !strings.Contains(v, "Lendo receitas e estado…") {
		t.Fatalf("carregando: %q", v)
	}
	if _, cmd := tab.Update(tickMsg{}); cmd == nil {
		t.Fatal("o spinner roda enquanto carrega")
	}
	tab.Update(recipesLoadedMsg{data: recipeSample()})
	if _, cmd := tab.Update(tickMsg{}); cmd != nil {
		t.Fatal("o spinner para depois de carregar")
	}
}

func TestRecipesGolden80x24Mono(t *testing.T) {
	m := sized(Options{Version: "v0.1.0", Mono: true}, 80, 24, fakes()...)
	m.tabs[1] = loadedRecipes(t, recipeSample(), false)
	press(m, '2', 0, "2")
	golden(t, "recipes_80x24", m.render())
}

func TestDiagnosticGolden80x24Mono(t *testing.T) {
	m := sized(Options{Version: "v0.1.0", Mono: true}, 80, 24, fakes()...)
	m.tabs[2] = loadedRecipes(t, recipeSample(), true)
	press(m, '3', 0, "3")
	golden(t, "diag_80x24", m.render())
}

func TestRecipesStatesHaveSymbolAndWord(t *testing.T) {
	tab := loadedRecipes(t, recipeSample(), false)
	v := plain(tab.View(80, 19))
	for _, want := range []string{"! aguardando reinício", "✗ falhou", "admin · reinicia", "Etapas de wsl-dev", "0 de 3 · exige administrador · reinicia 1 vez", "○ pendente", "Habilitar o WSL"} {
		if !strings.Contains(v, want) {
			t.Errorf("faltou %q:\n%s", want, v)
		}
	}
	rsend(tab, "down")
	v = plain(tab.View(80, 19))
	for _, want := range []string{"Etapas de linux-dev", "erro: apt-get saiu com 100.", "toolkit recipe reset linux-dev libera nova tentativa", "✗ falhou"} {
		if !strings.Contains(v, want) {
			t.Errorf("falha: faltou %q:\n%s", want, v)
		}
	}
}

func TestRecipesStepFocusAndScroll(t *testing.T) {
	d := RecipesData{Recipes: []RecipeInfo{{ID: "longa", Name: "Receita longa"}}, Runs: map[string]RecipeRun{}}
	for i := 0; i < 14; i++ {
		d.Recipes[0].Steps = append(d.Recipes[0].Steps, StepInfo{ID: fmt.Sprintf("s%02d", i), Name: fmt.Sprintf("Etapa %02d", i)})
	}
	tab := loadedRecipes(t, d, false)
	v := plain(tab.View(80, 9)) // lista 1 + régua 1 + 7 etapas
	if !strings.Contains(v, "0 de 14") || strings.Contains(v, "Etapa 13") || tab.More() <= 0 {
		t.Fatalf("o painel rola:\n%s (more=%d)", v, tab.More())
	}
	rsend(tab, "right")
	if !tab.focus {
		t.Fatal("→ foca as etapas")
	}
	rsend(tab, "G")
	v = plain(tab.View(80, 9))
	if !strings.Contains(v, "> ") || !strings.Contains(v, "Etapa 13") || tab.More() != 0 {
		t.Fatalf("G vai à última etapa:\n%s", v)
	}
	rsend(tab, "left")
	if tab.focus {
		t.Fatal("← volta à lista")
	}
	rsend(tab, "right", "esc")
	if tab.focus {
		t.Fatal("Esc tira o foco das etapas")
	}
}

func TestRecipesMarkAndSearch(t *testing.T) {
	tab := loadedRecipes(t, recipeSample(), false)
	rsend(tab, "space", "down", "space")
	m := tab.Marked()
	if len(m) != 2 || m[0].ID != "wsl-dev" || m[1].ID != "linux-dev" || tab.Selected() != 2 {
		t.Fatalf("marcados: %+v", m)
	}
	rsend(tab, "space")
	if tab.Selected() != 1 {
		t.Fatal("espaço desmarca")
	}
	rsend(tab, "/")
	if !tab.Capturing() {
		t.Fatal("a busca captura as teclas")
	}
	rsend(tab, "l", "i", "n", "q", "1")
	if tab.filter != "linq1" || !strings.Contains(plain(tab.View(80, 19)), `Nada com "linq1"`) {
		t.Fatalf("filtro %q:\n%s", tab.filter, plain(tab.View(80, 19)))
	}
	rsend(tab, "backspace", "backspace", "enter")
	if tab.Capturing() || tab.filter != "lin" || len(tab.visible()) != 1 {
		t.Fatalf("Enter fixa: %q %d", tab.filter, len(tab.visible()))
	}
	rsend(tab, "esc")
	if tab.filter != "" || len(tab.visible()) != 2 {
		t.Fatal("Esc limpa o filtro")
	}
}

func TestDiagnosticNeverRanAndEmptyStates(t *testing.T) {
	d := loadedRecipes(t, recipeSample(), true)
	if v := plain(d.View(80, 19)); !strings.Contains(v, "Nunca rodou.") || !strings.Contains(v, "a Revisão mostra o que será executado") {
		t.Fatalf("nunca rodou:\n%s", v)
	}
	empty := loadedRecipes(t, RecipesData{}, true)
	if v := plain(empty.View(80, 19)); !strings.Contains(v, "Nenhum diagnóstico para este sistema.") {
		t.Fatalf("vazio diag: %q", v)
	}
	empty = loadedRecipes(t, RecipesData{}, false)
	if v := plain(empty.View(80, 19)); !strings.Contains(v, "Nenhuma receita para este sistema.") {
		t.Fatalf("vazio rec: %q", v)
	}
}

func TestRecipesErrorStates(t *testing.T) {
	tab := NewRecipesTab(context.Background(), &fakeRecipes{}, Theme{Mono: true}, false)
	tab.Update(recipesLoadedMsg{err: errors.New("receita x.yaml:3:5: id inválido")})
	if v := plain(tab.View(80, 19)); !strings.Contains(v, "✗ Erro") || !strings.Contains(v, "Não deu para ler as receitas.") || !strings.Contains(v, "x.yaml:3:5") {
		t.Fatalf("erro de carga: %q", v)
	}
	d := recipeSample()
	d.StateErr, d.StatePath = errors.New("json corrompido"), "/var/lib/toolkit/state.json"
	tab = loadedRecipes(t, d, false)
	v := plain(tab.View(80, 19))
	if !strings.Contains(v, "aviso: não deu para ler o estado em /var/lib/toolkit/state.json") || !strings.Contains(v, "Rode: toolkit recipe status") || !strings.Contains(v, "Ambiente Linux") {
		t.Fatalf("erro do state (as receitas continuam):\n%s", v)
	}
}

func TestRecipesNeverBreakLayout(t *testing.T) {
	d := recipeSample()
	d.Recipes[0].Name = strings.Repeat("Nome comprido ", 12)
	d.Recipes[0].Steps[0].Name = strings.Repeat("Etapa comprida ", 10)
	for _, diag := range []bool{false, true} {
		for _, sz := range [][2]int{{60, 11}, {70, 15}, {80, 19}, {120, 30}} {
			tab := loadedRecipes(t, d, diag)
			rsend(tab, "right", "down")
			rows := strings.Split(tab.View(sz[0], sz[1]), "\n")
			if len(rows) > sz[1] {
				t.Errorf("diag=%v %v: %d linhas > %d", diag, sz, len(rows), sz[1])
			}
			for i, l := range rows {
				if lipgloss.Width(l) > sz[0] {
					t.Errorf("diag=%v %v linha %d: %d colunas: %q", diag, sz, i, lipgloss.Width(l), plain(l))
				}
			}
		}
	}
}

func TestRecipesOutsideTextIsSanitized(t *testing.T) {
	d := RecipesData{
		Recipes:  []RecipeInfo{{ID: "x\x1b]0;pwn\x07", Name: "Ev\x1b[2Jil\u202e", Description: "d\x1b[31m", Steps: []StepInfo{{ID: "s", Name: "e\x1b[2Jtapa"}}}},
		Runs:     map[string]RecipeRun{"x\x1b]0;pwn\x07": {Status: "failed", Steps: map[string]StepRun{"s": {Status: "failed", Error: "falha\x1b[2J\nsegunda"}}}},
		StateErr: errors.New("erro\x1b[2J"), StatePath: "/p\x1b[31m/state.json",
	}
	tab := loadedRecipes(t, d, false)
	for _, s := range []string{tab.View(80, 19), tab.Status()} {
		if strings.ContainsAny(s, "\x1b\x07\u202e") {
			t.Fatalf("sequência de controle vazou: %q", s)
		}
	}
}

func TestRecipesBackendErrorViaInit(t *testing.T) {
	tab := NewRecipesTab(context.Background(), &fakeRecipes{data: recipeSample()}, Theme{Mono: true}, false)
	cmd := tab.Init()
	msg := cmd()
	b, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init devolve um lote: %T", msg)
	}
	var got tea.Msg
	for _, c := range b {
		if c == nil {
			continue
		}
		if m := c(); m != nil {
			if _, is := m.(recipesLoadedMsg); is {
				got = m
			}
		}
	}
	if got == nil {
		t.Fatal("Init deve carregar as receitas")
	}
	tab.Update(got)
	if len(tab.items) != 2 {
		t.Fatalf("%d itens", len(tab.items))
	}
}

func TestRecipesRootRoutesDataToBackgroundTab(t *testing.T) {
	m := sized(Options{Mono: true}, 80, 24, fakes()...)
	rt := NewRecipesTab(context.Background(), &fakeRecipes{}, Theme{Mono: true}, false)
	m.tabs[1] = rt
	// a aba ativa é a 1 (Apps): os dados da aba 2 chegam mesmo assim
	m.Update(recipesLoadedMsg{data: recipeSample()})
	if rt.loading || len(rt.items) != 2 {
		t.Fatalf("a aba de fundo perdeu o resultado: loading=%v itens=%d", rt.loading, len(rt.items))
	}
	// e o diagnóstico não pega os dados da receita
	dt := NewRecipesTab(context.Background(), &fakeRecipes{}, Theme{Mono: true}, true)
	m.tabs[2] = dt
	m.Update(recipesLoadedMsg{data: recipeSample()})
	if len(dt.items) != 2 || dt.items[0].ID == "wsl-dev" {
		t.Fatalf("diagnóstico recebeu %+v", dt.items)
	}
}

// --- Lupa no #36: a rolagem e o More contam as linhas RENDERIZADAS (aviso "Nunca rodou" e linhas de erro
// também ocupam o corpo), não as etapas ---

func longRecipe(diag bool, failedAt int) RecipesData {
	r := RecipeInfo{ID: "longa", Name: "Receita longa", Diagnostic: diag}
	run := RecipeRun{Steps: map[string]StepRun{}}
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("s%02d", i)
		r.Steps = append(r.Steps, StepInfo{ID: id, Name: fmt.Sprintf("Etapa %02d", i)})
		switch {
		case failedAt >= 0 && i < failedAt:
			run.Steps[id] = StepRun{Status: "done"}
		case i == failedAt:
			run.Steps[id] = StepRun{Status: "failed", Error: "saiu com código 1. Veja o log."}
		}
	}
	d := RecipesData{Recipes: []RecipeInfo{r}, Runs: map[string]RecipeRun{}}
	if failedAt >= 0 {
		run.Status = "failed"
		d.Runs[r.ID] = run
	}
	return d
}

// Diagnóstico com 14 etapas em corpo 80x9, nunca rodou: → e G selecionam a etapa 14 e ela TEM de aparecer.
func TestDiagnosticNeverRanLastStepIsVisible(t *testing.T) {
	tab := loadedRecipes(t, longRecipe(true, -1), true)
	first := plain(tab.View(80, 9))
	if !strings.Contains(first, "Nunca rodou.") || tab.More() <= 0 {
		t.Fatalf("antes de rolar:\n%s (more=%d)", first, tab.More())
	}
	rsend(tab, "right", "G")
	v := plain(tab.View(80, 9))
	if !strings.Contains(v, "> ") || !strings.Contains(v, "14 Etapa 13") {
		t.Fatalf("a etapa 14 selecionada some da tela:\n%s", v)
	}
	if tab.More() != 0 {
		t.Fatalf("no fim More deve ser 0, é %d", tab.More())
	}
	if n := strings.Count(v, "\n") + 1; n > 9 {
		t.Fatalf("o corpo passou de 9 linhas: %d", n)
	}
}

// Receita cuja etapa 8 falhou: a linha de erro ocupa uma linha a mais; a etapa 14 ainda tem de aparecer.
func TestRecipeWithFailedStepLastStepIsVisible(t *testing.T) {
	tab := loadedRecipes(t, longRecipe(false, 7), false)
	rsend(tab, "right", "G")
	v := plain(tab.View(80, 9))
	if !strings.Contains(v, "> ") || !strings.Contains(v, "14 Etapa 13") || tab.More() != 0 {
		t.Fatalf("a etapa 14 selecionada some da tela (more=%d):\n%s", tab.More(), v)
	}
	// na etapa que falhou, a linha de erro aparece junto dela
	rsend(tab, "g")
	for i := 0; i < 7; i++ {
		rsend(tab, "down")
	}
	v = plain(tab.View(80, 9))
	if !strings.Contains(v, "8 Etapa 07") || !strings.Contains(v, "erro: saiu com código 1.") {
		t.Fatalf("a etapa que falhou e o erro dela ficam juntos:\n%s", v)
	}
}

// Invariante: para qualquer altura e qualquer etapa selecionada, a etapa aparece inteira e o More é
// exatamente o que sobrou abaixo, em LINHAS.
func TestRecipesStepWindowInvariant(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    RecipesData
		diag bool
	}{{"diagnóstico nunca rodou", longRecipe(true, -1), true}, {"etapa 8 falhou", longRecipe(false, 7), false}, {"etapa 1 falhou", longRecipe(false, 0), false}} {
		for h := 6; h <= 22; h++ {
			tab := loadedRecipes(t, tc.d, tc.diag)
			rsend(tab, "right")
			for i := 0; i < 14; i++ {
				v := plain(tab.View(80, h))
				want := fmt.Sprintf("%2d Etapa %02d", i+1, i)
				if !strings.Contains(v, want) {
					t.Fatalf("%s h=%d: a etapa selecionada %d não aparece:\n%s", tc.name, h, i+1, v)
				}
				if n := strings.Count(v, "\n") + 1; n > h {
					t.Fatalf("%s h=%d: %d linhas", tc.name, h, n)
				}
				shown := strings.Count(v, "\n") + 1
				total := len(tab.renderedSteps(tab.visible()[0], 80).lines)
				below := total - (tab.stp + stepsShown(v))
				if tab.More() != max(below, 0) {
					t.Fatalf("%s h=%d etapa %d: More=%d, abaixo da janela=%d (linhas mostradas %d)", tc.name, h, i+1, tab.More(), below, shown)
				}
				rsend(tab, "down")
			}
		}
	}
}

// stepsShown conta as linhas do painel de etapas na tela (depois da régua "Etapas de").
func stepsShown(view string) int {
	n, in := 0, false
	for _, l := range strings.Split(view, "\n") {
		if in {
			n++
		}
		if strings.Contains(l, "Etapas de ") {
			in = true
		}
	}
	return n
}

// Tecla e (aba Diagnóstico): liga/desliga os detalhes (saída original) e a marca leva o pedido ao executor.
func TestDiagnosticEKeyTogglesDetailsAndMarksCarryIt(t *testing.T) {
	tab := loadedRecipes(t, recipeSample(), true)
	hasKey := func(label string) bool {
		for _, k := range tab.Keys() {
			if k.Key == "e" && k.Label == label {
				return true
			}
		}
		return false
	}
	if !hasKey("detalhes") {
		t.Fatalf("dica da tecla e desligada: %v", tab.Keys())
	}
	tab.Update(key(' ', "space"))
	if m := tab.Marked(); len(m) != 1 || m[0].Details {
		t.Fatalf("sem e, a marca não pede detalhes: %+v", m)
	}
	tab.Update(key('e', "e"))
	if !hasKey("detalhes ✓") {
		t.Fatalf("depois do e a dica mostra o estado ligado: %v", tab.Keys())
	}
	if m := tab.Marked(); len(m) != 1 || !m[0].Details {
		t.Fatalf("com e, a marca pede detalhes: %+v", m)
	}
	tab.Update(key('e', "e"))
	if m := tab.Marked(); m[0].Details || !hasKey("detalhes") {
		t.Fatalf("e de novo desliga: %+v", m)
	}
}

func TestRecipesTabIgnoresEKey(t *testing.T) {
	tab := loadedRecipes(t, recipeSample(), false)
	tab.Update(key(' ', "space"))
	tab.Update(key('e', "e"))
	if m := tab.Marked(); len(m) != 1 || m[0].Details {
		t.Fatalf("a aba Receitas não tem detalhes: %+v", m)
	}
	for _, k := range tab.Keys() {
		if k.Key == "e" {
			t.Fatalf("a aba Receitas não anuncia a tecla e: %v", tab.Keys())
		}
	}
}
