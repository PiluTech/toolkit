package tui

import (
	"context"
	"strings"
	"testing"
)

// A2/A3 da auditoria (docs/auditoria-a11y-l10n.md): com o movimento reduzido (TOOLKIT_REDUCE_MOTION=1 ou --no-motion)
// o spinner vira um símbolo fixo e nenhum quadro é agendado: a tela só muda quando o estado muda.
func TestNoMotionThemeHasFixedSpinnerAndNoTick(t *testing.T) {
	still := Theme{Mono: true, NoMotion: true}
	for f := 0; f < 8; f++ {
		if got := still.Spin(f); got != SymBusy {
			t.Fatalf("frame %d: %q, quer o símbolo fixo %q", f, got, SymBusy)
		}
	}
	if still.Tick() != nil {
		t.Fatal("sem movimento nenhum quadro é agendado")
	}
	moving := Theme{Mono: true}
	if moving.Tick() == nil {
		t.Fatal("com movimento o spinner roda")
	}
	seen := map[string]bool{}
	for f := 0; f < 8; f++ {
		seen[moving.Spin(f)] = true
	}
	if len(seen) != len(spinnerFrames) {
		t.Fatalf("o spinner normal usa os %d quadros: %v", len(spinnerFrames), seen)
	}
}

func TestTabsStopTickingAndShowTheFixedSymbolWithoutMotion(t *testing.T) {
	still := Theme{Mono: true, Dark: true, NoMotion: true}
	// Receitas: carregando = "… Carregando" + "Lendo receitas e estado…", e o tick não re-arma
	rt := NewRecipesTab(context.Background(), &fakeRecipes{}, still, false)
	if v := plain(rt.View(80, 19)); !strings.Contains(v, SymLoad+" Carregando") {
		t.Fatalf("sem movimento o símbolo é fixo, com a palavra: %q", v)
	}
	if v := plain(rt.View(80, 19)); !strings.Contains(v, "Lendo receitas e estado…") {
		t.Fatalf("carregando sem movimento: %q", v)
	}
	if _, cmd := rt.Update(tickMsg{}); cmd != nil {
		t.Fatal("receitas: sem movimento o tick não re-arma")
	}
	// Apps
	at := NewAppsTab(context.Background(), &fakeBackend{}, still, "linux")
	if v := plain(at.View(80, 19)); !strings.Contains(v, SymLoad+" Carregando") {
		t.Fatalf("sem movimento o símbolo é fixo, com a palavra: %q", v)
	}
	if v := plain(at.View(80, 19)); !strings.Contains(v, "Lendo o catálogo…") {
		t.Fatalf("apps carregando sem movimento: %q", v)
	}
	if _, cmd := at.Update(tickMsg{}); cmd != nil {
		t.Fatal("apps: sem movimento o tick não re-arma")
	}
	// Log: item em execução tem símbolo fixo + a palavra "executando"
	lt := NewLogTab(still, "")
	sym, word, _ := lt.itemState(logItem{status: ItemRunning})
	if sym != SymBusy || word != "executando" {
		t.Fatalf("log: %q %q", sym, word)
	}
	if _, cmd := lt.Update(tickMsg{}); cmd != nil {
		t.Fatal("log: sem movimento o tick não re-arma")
	}
}
