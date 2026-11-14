package cli

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/PiluTech/toolkit/internal/otimizacao"
	"github.com/PiluTech/toolkit/internal/tui"
)

// otimAreas monta as áreas reais com um coletor falso que conta as chamadas.
func otimAreas(t *testing.T, goos string, calls *int) []tui.AreaDef {
	t.Helper()
	deps := otimDeps(t, goos, otimReport(), nil)
	deps.Otim.Collect = func(_ context.Context, g string, _ otimizacao.Deps) *otimizacao.Report {
		*calls++
		r := otimReport()
		r.OS = g
		return r
	}
	return tuiAreas(context.Background(), deps, RecipeDeps{}, tui.Theme{Mono: true, NoMotion: true}, false)
}

func otimTab(t *testing.T, areas []tui.AreaDef) tui.Tab {
	t.Helper()
	for _, a := range areas {
		if a.Key != "maquina" {
			continue
		}
		for _, tb := range a.Tabs {
			if tb.Title() == "Otimização" {
				return tb
			}
		}
	}
	t.Fatal("a seção Otimização não está na área Máquina")
	return nil
}

func TestTUIOtimizacaoNaAreaMaquinaDepoisDeAppsNativos(t *testing.T) {
	var calls int
	areas := otimAreas(t, "windows", &calls)
	var titles []string
	for _, tb := range areas[2].Tabs {
		titles = append(titles, tb.Title())
	}
	if areas[2].Key != "maquina" || strings.Join(titles, ",") != "Diagnóstico,Apps nativos,Otimização" {
		t.Fatalf("seções da Máquina: %v", titles)
	}
	if calls != 0 {
		t.Fatal("montar a TUI não lê nada")
	}
}

func TestTUIOtimizacaoWindowsLeSoQuandoPedido(t *testing.T) {
	var calls int
	tab := otimTab(t, otimAreas(t, "windows", &calls))
	if tab.Init() != nil || calls != 0 {
		t.Fatal("nada é lido ao abrir")
	}
	_, cmd := tab.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd == nil {
		t.Fatal("c deveria ler")
	}
	if msgs := runBatch(cmd); len(msgs) == 0 {
		t.Fatal("a leitura devolve a resposta")
	} else {
		for _, m := range msgs {
			tab.Update(m)
		}
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	if v := tab.View(100, 30); !strings.Contains(v, "Otimização do Windows") {
		t.Fatalf("resumo na tela:\n%s", v)
	}
}

func TestTUIOtimizacaoForaDoWindowsNaoLe(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		var calls int
		tab := otimTab(t, otimAreas(t, goos, &calls))
		_, cmd := tab.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
		if cmd != nil || calls != 0 {
			t.Errorf("%s: não lê nada", goos)
		}
		if v := tab.View(80, 16); !strings.Contains(v, "só existe no Windows") {
			t.Errorf("%s:\n%s", goos, v)
		}
	}
}

func TestInicioTemOObjetivoDeixarOWindowsMaisLeve(t *testing.T) {
	ini, areas := inicioDe(t, "windows", true)
	var found bool
	for _, it := range ini.Items() {
		if it.Title != "Deixar o Windows mais leve" {
			continue
		}
		found = true
		if got := areas[it.Area].Tabs[it.Section].Title(); got != "Otimização" || it.Area != 2 {
			t.Errorf("abre %q (área %d), queria Otimização na área 2", got, it.Area)
		}
		if strings.Contains(strings.ToLower(it.Desc), "animaç") || strings.Contains(strings.ToLower(it.Desc), "acelera") {
			t.Errorf("a descrição só promete o que existe (leitura de gargalos): %q", it.Desc)
		}
		if it.Unavailable || it.Tag != "" {
			t.Errorf("no Windows o objetivo está disponível e sem etiqueta: %+v", it)
		}
	}
	if !found {
		t.Fatal("faltou o objetivo")
	}
	ini, _ = inicioDe(t, "linux", false)
	for _, it := range ini.Items() {
		if it.Title == "Deixar o Windows mais leve" && it.Tag != "! só Windows" {
			t.Errorf("fora do Windows a etiqueta é '! só Windows': %q", it.Tag)
		}
	}
}

func runBatch(cmd tea.Cmd) []tea.Msg {
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			if c == nil {
				continue
			}
			if m := c(); m != nil {
				out = append(out, m)
			}
		}
		return out
	}
	return []tea.Msg{msg}
}
