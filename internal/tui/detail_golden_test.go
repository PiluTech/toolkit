package tui

import (
	"fmt"
	"testing"
)

// Goldens do painel de detalhe (fase 7): 120x40 e 110x30, sempre em Mono, com as seções reais e backends falsos
// (Windows falso para Apps nativos e Privacidade). Abaixo de 110 colunas o painel não existe (os goldens de 60 e 80 não mudam).
func detailGoldenSizes() [][2]int { return [][2]int{{120, 40}, {110, 30}} }

func detailGoldenApps(t *testing.T) *AppsTab {
	t.Helper()
	be := &fakeBackend{
		apps: []AppInfo{
			{ID: "git", Name: "Git", Category: "dev", Managers: []string{"apt", "flatpak"}},
			{ID: "python", Name: "Python", Category: "dev", Managers: []string{"apt"}, Versions: true},
			{ID: "firefox", Name: "Firefox", Category: "navegadores", Managers: []string{"flatpak"}},
			{ID: "vlc", Name: "VLC", Category: "midia", NoInstaller: true},
		},
		status: map[string]AppStatus{
			"git":     {Installed: true, Version: "1:2.43.0-1ubuntu7"},
			"firefox": {Installed: true, Version: "133.0"},
		},
	}
	return loaded(t, be)
}

func detailGoldenModel(w, h int, active int, tabs map[int]Tab) *Model {
	ds := areaDefs()
	ds[0].Tabs[0] = tabs[idxInicio]
	ds[1].Tabs = []Tab{tabs[idxApps], tabs[idxRec]}
	ds[2].Tabs = []Tab{tabs[idxDiag], tabs[idxNat]}
	ds[3].Tabs = []Tab{tabs[idxPriv]}
	m := NewWithAreas(Options{Version: "v0.1.0", Mono: true, NoMotion: true, IsAdmin: true}, ds)
	m.Update(sizeMsg(w, h))
	m.setActive(active)
	return m
}

func detailGoldenTabs(t *testing.T) map[int]Tab {
	t.Helper()
	return map[int]Tab{
		idxInicio: newInicio(true, false),
		idxApps:   detailGoldenApps(t),
		idxRec:    loadedRecipes(t, recipeSample(), false),
		idxDiag:   newDiag(t, &fakeMachine{}),
		idxNat:    natTab(t, &fakeNat{info: natSample(true)}),
		idxPriv:   privTab(t, &fakePriv{info: privSample(false)}),
	}
}

func TestGoldenDetalheApps(t *testing.T) {
	for _, sz := range detailGoldenSizes() {
		tabs := detailGoldenTabs(t)
		send(tabs[idxApps].(*AppsTab), "down") // Python: aceita versão fixa
		m := detailGoldenModel(sz[0], sz[1], idxApps, tabs)
		golden(t, fmt.Sprintf("detalhe_apps_%dx%d", sz[0], sz[1]), m.render())
	}
}

func TestGoldenDetalheReceitas(t *testing.T) {
	for _, sz := range detailGoldenSizes() {
		m := detailGoldenModel(sz[0], sz[1], idxRec, detailGoldenTabs(t))
		golden(t, fmt.Sprintf("detalhe_receitas_%dx%d", sz[0], sz[1]), m.render())
	}
}

func TestGoldenDetalheAppsNativos(t *testing.T) {
	for _, sz := range detailGoldenSizes() {
		tabs := detailGoldenTabs(t)
		nkey(tabs[idxNat].(*NativeAppsTab), "down", "space")
		m := detailGoldenModel(sz[0], sz[1], idxNat, tabs)
		golden(t, fmt.Sprintf("detalhe_nativos_%dx%d", sz[0], sz[1]), m.render())
	}
}

func TestGoldenDetalhePrivacidade(t *testing.T) {
	for _, sz := range detailGoldenSizes() {
		tabs := detailGoldenTabs(t)
		pkey(tabs[idxPriv].(*PrivacyTab), "down")
		m := detailGoldenModel(sz[0], sz[1], idxPriv, tabs)
		golden(t, fmt.Sprintf("detalhe_priv_%dx%d", sz[0], sz[1]), m.render())
	}
}

func TestGoldenDetalheInicio(t *testing.T) {
	for _, sz := range detailGoldenSizes() {
		tabs := detailGoldenTabs(t)
		tabs[idxInicio].(*InicioTab).Update(key('j', "j"))
		tabs[idxInicio].(*InicioTab).Update(key('j', "j"))
		tabs[idxInicio].(*InicioTab).Update(key('j', "j"))
		m := detailGoldenModel(sz[0], sz[1], idxInicio, tabs)
		golden(t, fmt.Sprintf("detalhe_inicio_%dx%d", sz[0], sz[1]), m.render())
	}
}

// Em 109 colunas a mesma tela não tem painel (a lista ocupa a largura toda).
func TestDetalheAbaixoDe110NaoMudaAAba(t *testing.T) {
	for _, active := range []int{idxApps, idxRec, idxNat, idxPriv, idxInicio} {
		wide := detailGoldenModel(109, 30, active, detailGoldenTabs(t)).render()
		if containsSep(wide) {
			t.Errorf("seção %d em 109 colunas não devia ter painel:\n%s", active, wide)
		}
	}
}

func containsSep(s string) bool {
	for _, r := range s {
		if r == '│' {
			return true
		}
	}
	return false
}
