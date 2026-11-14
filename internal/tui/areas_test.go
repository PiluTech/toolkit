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

// índices planos das seções em areaDefs: 0 Início | 1 Apps 2 Receitas | 3 Diagnóstico 4 Apps nativos | 5 Privacidade | 6 Análise | 7 Execuções
const (
	idxInicio, idxApps, idxRec, idxDiag, idxNat, idxPriv, idxRede, idxExec = 0, 1, 2, 3, 4, 5, 6, 7
)

func areaDefs() []AreaDef {
	return []AreaDef{
		{Key: "inicio", Title: "Início", Short: "Iníc", Tabs: []Tab{&fakeTab{title: "Início", short: "Iníc", body: " início"}}},
		{Key: "instalar", Title: "Instalar", Short: "Inst", Tabs: []Tab{
			&fakeTab{title: "Apps", short: "Apps", body: " Git\n Python"},
			&fakeTab{title: "Receitas", short: "Rec", body: " receita"},
		}},
		{Key: "maquina", Title: "Máquina", Short: "Máq", Tabs: []Tab{
			&fakeTab{title: "Diagnóstico", short: "Diag", body: " diag"},
			&fakeTab{title: "Apps nativos", short: "Nativos", body: " nativos"},
		}},
		{Key: "seguranca", Title: "Segurança", Short: "Seg", Tabs: []Tab{&fakeTab{title: "Privacidade", short: "Priv", body: " priv"}}},
		{Key: "rede", Title: "Rede", Short: "Rede", Tabs: []Tab{&fakeTab{title: "Análise", short: "Anál", body: " rede"}}},
		{Key: "historico", Title: "Histórico", Short: "Hist", Tabs: []Tab{&fakeTab{title: "Execuções", short: "Exec", body: " log"}}},
	}
}

func sizeMsg(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }

func keyShiftTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }

func sizedAreas(o Options, w, h int) *Model {
	m := NewWithAreas(o, areaDefs())
	m.Update(sizeMsg(w, h))
	return m
}

func TestAreasFlattenTabsInOrder(t *testing.T) {
	m := NewWithAreas(Options{}, areaDefs())
	var got []string
	for _, tb := range m.tabs {
		got = append(got, tb.Title())
	}
	want := "Início,Apps,Receitas,Diagnóstico,Apps nativos,Privacidade,Análise,Execuções"
	if strings.Join(got, ",") != want {
		t.Fatalf("abas planas: %v", got)
	}
	if len(m.areas) != 6 || m.areas[2].Key != "maquina" || len(m.areas[2].Idx) != 2 || m.areas[2].Idx[1] != idxNat {
		t.Fatalf("áreas: %+v", m.areas)
	}
}

func TestAreaNumberKeysGoToTheArea(t *testing.T) {
	m := sizedAreas(Options{Mono: true}, 80, 24)
	for _, c := range []struct {
		k    string
		want int
	}{{"2", idxDiag}, {"5", idxExec}, {"3", idxPriv}, {"4", idxRede}, {"1", idxApps}, {"0", idxInicio}} {
		press(m, rune(c.k[0]), 0, c.k)
		if m.Active() != c.want {
			t.Errorf("tecla %s: seção %d, quero %d", c.k, m.Active(), c.want)
		}
	}
	press(m, '6', 0, "6") // não existe a área 6: nada muda
	press(m, '9', 0, "9")
	if m.Active() != idxInicio {
		t.Errorf("área inexistente moveu a tela: %d", m.Active())
	}
}

func TestAreaNumberReturnsToTheLastVisitedSection(t *testing.T) {
	m := sizedAreas(Options{Mono: true}, 80, 24)
	press(m, '2', 0, "2")
	mkey(m, "tab")
	if m.Active() != idxNat {
		t.Fatalf("Tab dentro da área: %d", m.Active())
	}
	press(m, '1', 0, "1")
	press(m, '2', 0, "2")
	if m.Active() != idxNat {
		t.Fatalf("2 volta à última seção visitada de Máquina: %d", m.Active())
	}
	press(m, '1', 0, "1") // Instalar nunca visitada além da primeira: abre Apps
	if m.Active() != idxApps {
		t.Fatalf("1: %d", m.Active())
	}
}

func TestTabCyclesSectionsInsideTheArea(t *testing.T) {
	m := sizedAreas(Options{Mono: true}, 80, 24)
	press(m, '1', 0, "1")
	mkey(m, "tab")
	if m.Active() != idxRec {
		t.Fatalf("Tab: %d", m.Active())
	}
	mkey(m, "tab") // dá a volta DENTRO da área, não sai dela
	if m.Active() != idxApps {
		t.Fatalf("Tab dá a volta na área: %d", m.Active())
	}
	m.Update(keyShiftTab())
	if m.Active() != idxRec {
		t.Fatalf("Shift+Tab volta: %d", m.Active())
	}
	m.Update(keyShiftTab())
	if m.Active() != idxApps {
		t.Fatalf("Shift+Tab: %d", m.Active())
	}
	// área de uma seção só: Tab não faz nada (e não vai para a área vizinha)
	press(m, '3', 0, "3")
	mkey(m, "tab")
	m.Update(keyShiftTab())
	if m.Active() != idxPriv {
		t.Fatalf("área de uma seção: %d", m.Active())
	}
}

func TestCapturingTabKeepsDigitsAndTab(t *testing.T) {
	d := areaDefs()
	d[1].Tabs[0].(*fakeTab).capturing = true
	m := NewWithAreas(Options{Mono: true}, d)
	m.Update(sizeMsg(80, 24))
	press(m, '1', 0, "1")
	press(m, '2', 0, "2")
	mkey(m, "tab")
	if m.Active() != idxApps {
		t.Fatalf("aba digitando recebe os números e o Tab: %d", m.Active())
	}
	if n := len(d[1].Tabs[0].(*fakeTab).got); n != 2 {
		t.Fatalf("a aba devia receber 3 teclas (2 chegaram depois do 1): %d", n)
	}
}

func TestAreaHeaderFitsEveryWidthWithoutEllipsisInAreaNames(t *testing.T) {
	for _, w := range []int{60, 70, 77, 78, 79, 80, 99, 100, 120} {
		for _, h := range []int{16, 20, 24, 40} {
			for _, latest := range []string{"", "v0.6.0"} {
				m := sizedAreas(Options{Version: "v0.1.0", Latest: latest, Mono: true}, w, h)
				for _, key := range []string{"0", "2", "3", "5"} {
					press(m, rune(key[0]), 0, key)
					rows := strings.Split(m.render(), "\n")
					if len(rows) != h {
						t.Fatalf("%dx%d: %d linhas", w, h, len(rows))
					}
					if lw := lipgloss.Width(rows[0]); lw > w {
						t.Errorf("%dx%d latest=%q: cabeçalho com %d colunas: %q", w, h, latest, lw, rows[0])
					}
					short := w < 78
					if strings.Contains(rows[0], "…") {
						t.Errorf("%dx%d: reticências no cabeçalho: %q", w, h, rows[0])
					}
					for _, a := range []string{"Início", "Instalar", "Máquina", "Segurança", "Histórico"} {
						if short && h >= 24 && strings.Contains(rows[0], a) {
							t.Errorf("%dx%d: abaixo de 78 colunas os nomes são curtos: %q", w, h, rows[0])
						}
					}
					for i, r := range rows {
						if lw := lipgloss.Width(r); lw > w {
							t.Errorf("%dx%d linha %d com %d colunas", w, h, i+1, lw)
						}
					}
				}
			}
		}
	}
}

func TestAreaHeaderShowsToolkitWordOnlyFrom100(t *testing.T) {
	if r := strings.Split(sizedAreas(Options{Version: "v0.1.0", Mono: true}, 99, 30).render(), "\n")[0]; strings.Contains(r, "toolkit") {
		t.Errorf("99 colunas sem a palavra toolkit: %q", r)
	}
	if r := strings.Split(sizedAreas(Options{Version: "v0.1.0", Mono: true}, 100, 30).render(), "\n")[0]; !strings.Contains(r, "toolkit") {
		t.Errorf("100 colunas com a palavra toolkit: %q", r)
	}
}

func TestAreaHeaderMarksTheActiveAreaAndShowsTheBanner(t *testing.T) {
	m := sizedAreas(Options{Version: "v0.1.0", Latest: "v0.6.0", Mono: true}, 80, 30)
	press(m, '2', 0, "2")
	r := strings.Split(m.render(), "\n")[0]
	for _, want := range []string{"[2 Máquina]", " 1 Instalar ", "0 Início", "↑ v0.6.0 (u)"} {
		if !strings.Contains(r, want) {
			t.Errorf("cabeçalho sem %q: %q", want, r)
		}
	}
	m = sizedAreas(Options{Version: "v0.1.0", Latest: "v0.6.0", Mono: true}, 120, 30)
	if r := strings.Split(m.render(), "\n")[0]; !strings.Contains(r, "↑ v0.6.0 disponível (u)") {
		t.Errorf("a partir de 100 colunas: %q", r)
	}
	m = sizedAreas(Options{Version: "v0.1.0", Mono: true}, 80, 30)
	if r := strings.Split(m.render(), "\n")[0]; !strings.HasSuffix(strings.TrimRight(r, " "), "v0.1.0") {
		t.Errorf("versão à direita: %q", r)
	}
}

func TestAreaSecondLineShowsSectionsAndAdmin(t *testing.T) {
	m := sizedAreas(Options{Version: "v0.1.0", Mono: true}, 80, 24)
	press(m, '2', 0, "2")
	mkey(m, "tab")
	rows := strings.Split(m.render(), "\n")
	for _, want := range []string{"Máquina ›", "Diagnóstico", "[Apps nativos]", "admin: não"} {
		if !strings.Contains(rows[1], want) {
			t.Errorf("linha 2 sem %q: %q", want, rows[1])
		}
	}
	if strings.Trim(rows[2], "─") != "" {
		t.Errorf("linha 3 é a régua: %q", rows[2])
	}
	m = sizedAreas(Options{Version: "v0.1.0", Mono: true, IsAdmin: true}, 80, 24)
	if r := strings.Split(m.render(), "\n")[1]; !strings.Contains(r, "admin: sim") {
		t.Errorf("admin: sim: %q", r)
	}
}

func TestAreaSecondLineShowsMarkedFrom100Columns(t *testing.T) {
	d := areaDefs()
	d[1].Tabs[0].(*fakeTab).selected = 2
	d[1].Tabs[1].(*fakeTab).selected = 1
	for w, want := range map[int]bool{99: false, 100: true, 120: true} {
		m := NewWithAreas(Options{Version: "v0.1.0", Mono: true}, d)
		m.Update(sizeMsg(w, 30))
		r := strings.Split(m.render(), "\n")[1]
		if got := strings.Contains(r, "3 marcados · admin: não"); got != want {
			t.Errorf("%d colunas: marcados=%v quero %v: %q", w, got, want, r)
		}
	}
	m := NewWithAreas(Options{Mono: true}, areaDefs())
	m.Update(sizeMsg(120, 30))
	if r := strings.Split(m.render(), "\n")[1]; strings.Contains(r, "marcados") {
		t.Errorf("sem marcados a palavra não aparece: %q", r)
	}
}

type heightTab struct {
	fakeTab
	gotH int
}

func (h *heightTab) View(_, hh int) string { h.gotH = hh; return h.body }

func TestAreaBodyLosesOneLineAndLine2CollapsesBelow24(t *testing.T) {
	ht := &heightTab{fakeTab: fakeTab{title: "Apps", short: "Apps", body: " x"}}
	d := areaDefs()
	d[1].Tabs[0] = ht
	for _, c := range []struct{ h, body int }{{40, 34}, {24, 18}, {23, 18}, {17, 12}, {16, 11}} {
		m := NewWithAreas(Options{Version: "v0.1.0", Mono: true}, d)
		m.Update(sizeMsg(80, c.h))
		press(m, '1', 0, "1")
		rows := strings.Split(m.render(), "\n")
		if ht.gotH != c.body {
			t.Errorf("h=%d: corpo %d, quero %d", c.h, ht.gotH, c.body)
		}
		if len(rows) != c.h {
			t.Errorf("h=%d: %d linhas", c.h, len(rows))
		}
		if c.h >= 24 {
			if !strings.Contains(rows[1], "Instalar ›") {
				t.Errorf("h=%d: linha 2: %q", c.h, rows[1])
			}
			continue
		}
		// colapsada: a seção sobe para a linha 1 e a linha 2 é a régua
		if !strings.Contains(rows[0], "›") || !strings.Contains(rows[0], "Apps") {
			t.Errorf("h=%d: a seção vai na linha 1: %q", c.h, rows[0])
		}
		if strings.Trim(rows[1], "─") != "" {
			t.Errorf("h=%d: sem linha 2, a régua vem logo depois: %q", c.h, rows[1])
		}
	}
}

func TestAreaSingleSectionAreaShowsTheAreaNameOnLine2(t *testing.T) {
	m := sizedAreas(Options{Version: "v0.1.0", Mono: true}, 80, 24)
	press(m, '5', 0, "5")
	if r := strings.Split(m.render(), "\n")[1]; !strings.Contains(r, "Histórico ›") || !strings.Contains(r, "[Execuções]") {
		t.Errorf("linha 2: %q", r)
	}
}

func TestAreaHelpListsAreas(t *testing.T) {
	m := sizedAreas(Options{Mono: true}, 80, 30)
	press(m, '?', 0, "?")
	out := strings.Join(strings.Fields(ansi.Strip(m.render())), " ")
	for _, want := range []string{"0-5", "Início, Instalar, Máquina, Segurança, Rede, Histórico", "Shift+Tab", "seção"} {
		if !strings.Contains(out, want) {
			t.Errorf("ajuda sem %q:\n%s", want, out)
		}
	}
}

func TestInfoTabExplainsAndIsStatic(t *testing.T) {
	tb := NewInfoTab(Theme{Mono: true}, "Análise", "Anál", "A análise de rede ainda não existe nesta versão.")
	if tb.Title() != "Análise" || tb.Short() != "Anál" || tb.Capturing() || tb.Selected() != 0 {
		t.Fatalf("contrato: %+v", tb)
	}
	if v := tb.View(80, 10); !strings.Contains(v, "A análise de rede ainda não existe nesta versão.") {
		t.Fatalf("texto: %q", v)
	}
	if v := tb.View(20, 10); lipgloss.Width(strings.Split(v, "\n")[0]) > 20 {
		t.Fatalf("quebra na largura: %q", v)
	}
}

// selection, Reloader, resultados e origens seguem os índices PLANOS: a execução marcada na área Instalar leva ao
// Histórico (Log) e a faixa volta na seção de origem.
func TestAreasKeepExecutionFlowWithFlatIndexes(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	apps := loaded(t, be)
	rec := loadedRecipes(t, recipeSample(), false)
	diag := loadedRecipes(t, recipeSample(), true)
	log := NewLogTab(Theme{Mono: true}, "/var/log/toolkit")
	spy := &reloadSpy{fakeTab: fakeTab{title: "Apps nativos", short: "Nativos"}}
	m := NewWithAreas(Options{Version: "v0.1.0", Mono: true}, []AreaDef{
		{Key: "inicio", Title: "Início", Short: "Iníc", Tabs: []Tab{NewInfoTab(Theme{Mono: true}, "Início", "Iníc", "oi")}},
		{Key: "instalar", Title: "Instalar", Short: "Inst", Tabs: []Tab{apps, rec}},
		{Key: "maquina", Title: "Máquina", Short: "Máq", Tabs: []Tab{diag, spy}},
		{Key: "historico", Title: "Histórico", Short: "Hist", Tabs: []Tab{log}},
	})
	p, r := &fakePlanner{plan: okPlan()}, &fakeRunner{script: scriptOK}
	m.SetExec(context.Background(), p, r)
	m.Update(sizeMsg(80, 24))

	press(m, '1', 0, "1")
	mkey(m, "space") // marca Git
	cmd := mkey(m, "enter")
	if m.mode != modeReview || cmd == nil {
		t.Fatal("Enter abre a Revisão a partir da área Instalar")
	}
	// números e Tab não agem durante a Revisão
	press(m, '3', 0, "3")
	mkey(m, "tab")
	if m.Active() != idxApps {
		t.Fatalf("a Revisão ignora números e Tab: %d", m.Active())
	}
	m.Update(cmd().(tea.BatchMsg)[0]())
	if len(p.got.Apps) != 1 || p.got.Apps[0].ID != "git" {
		t.Fatalf("seleção: %+v", p.got)
	}
	mkey(m, "tab")
	mkey(m, "enter") // Executar
	pump(t, m)
	_, logIdx := m.logTab()
	if logIdx != len(m.tabs)-1 {
		t.Fatalf("o Log é a última seção plana: %d", logIdx)
	}
	if m.Active() != logIdx {
		t.Fatalf("a execução leva ao Histórico: %d", m.Active())
	}
	if m.results[idxApps] == nil {
		t.Fatalf("faixa de resultado na seção de origem (índice plano): %v", m.results)
	}
	if spy.reloads != 1 {
		t.Fatalf("Reloader: %d", spy.reloads)
	}
	// voltando à área de origem a faixa aparece, e x a fecha
	press(m, '1', 0, "1")
	if m.Active() != idxApps {
		t.Fatalf("1: %d", m.Active())
	}
	out := m.render()
	if !strings.Contains(out, "falhou") && !strings.Contains(out, "parcial") && !strings.Contains(out, "!") {
		t.Fatalf("sem faixa:\n%s", out)
	}
	press(m, 'x', 0, "x")
	if m.results[idxApps] != nil {
		t.Fatal("x fecha a faixa")
	}
}

// o modo legado (New com abas planas) continua igual: números pulam para a aba, Tab percorre todas e não há linha 2.
func TestLegacyModeKeepsFlatTabs(t *testing.T) {
	m := sized(Options{Version: "v0.1.0", Mono: true}, 80, 24, fakes()...)
	press(m, '3', 0, "3")
	if m.Active() != 2 {
		t.Fatalf("3: %d", m.Active())
	}
	mkey(m, "tab")
	mkey(m, "tab")
	if m.Active() != 0 {
		t.Fatalf("Tab percorre todas as abas: %d", m.Active())
	}
	rows := strings.Split(m.render(), "\n")
	if !strings.Contains(rows[0], "[1 Apps]") || strings.Contains(rows[1], "admin") || strings.Trim(rows[1], "─") != "" {
		t.Fatalf("sem linha 2 no modo legado:\n%s", m.render())
	}
}

func TestAreaGoldens(t *testing.T) {
	d := func() []AreaDef {
		ds := areaDefs()
		ds[4].Tabs[0] = NewInfoTab(Theme{Mono: true}, "Análise", "Anál", "A análise de rede ainda não existe nesta versão.")
		return ds
	}
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {60, 17}} {
		for _, k := range []string{"1", "2", "3", "4", "5"} { // o Início tem os seus goldens em inicio_tab_test.go
			m := NewWithAreas(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, d())
			m.Update(sizeMsg(sz[0], sz[1]))
			press(m, rune(k[0]), 0, k)
			golden(t, fmt.Sprintf("areas_%s_%dx%d", m.areas[int(k[0]-'0')].Key, sz[0], sz[1]), m.render())
		}
	}
}
