package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Lupa #73: redimensionar 109 <-> 110 <-> 120 colunas com o painel de detalhe e a confirmação REAL. O painel só existe
// na lista normal em 110 ou mais; durante a confirmação a tela fica em largura total e o ID aplicado é o do plano exibido.

// seqNat emite um ID NOVO a cada plano e só aceita o ID do último plano exibido (como o backend real).
type seqNat struct {
	*fakeNat
	seq    int
	lastID string
}

func (s *seqNat) Plan(ctx context.Context, sel NatSelection) (NatPlan, error) {
	pl, err := s.fakeNat.Plan(ctx, sel)
	s.seq++
	s.lastID = fmt.Sprintf("nat-plano-%d", s.seq)
	pl.ID = s.lastID
	return pl, err
}

func (s *seqNat) Apply(ctx context.Context, sel NatSelection, id string) (NatResult, error) {
	if id != s.lastID {
		return NatResult{}, fmt.Errorf("plano velho: %s", id)
	}
	return s.fakeNat.Apply(ctx, sel, id)
}

const detailMarker = "pacote: bing-weather"

func resizeNatModel(t *testing.T, be NativeAppsBackend, w int) (*Model, *NativeAppsTab) {
	t.Helper()
	tab := NewNativeAppsTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	m := NewWithAreas(Options{Mono: true, NoMotion: true}, []AreaDef{{Key: "maquina", Title: "Máquina", Short: "Máq", Tabs: []Tab{tab}}})
	rresize(m, w, 40)
	rpump(m, tab.Init())
	return m, tab
}

func TestDetalheListaNormalSoApareceDe110Colunas(t *testing.T) {
	m, _ := resizeNatModel(t, &fakeNat{info: natSample(true)}, 109)
	for _, c := range []struct {
		w    int
		want bool
	}{{109, false}, {110, true}, {120, true}, {109, false}} {
		rresize(m, c.w, 40)
		if got := strings.Contains(rscreen(m), detailMarker); got != c.want {
			t.Fatalf("%d colunas: painel=%v, esperado %v", c.w, got, c.want)
		}
	}
}

func TestConfirmacaoNativosRedimensionaSemPainelEAplicaOIDExibido(t *testing.T) {
	sizes := []int{109, 110, 120}
	for _, openW := range sizes {
		for _, confirmW := range sizes {
			t.Run(fmt.Sprintf("abre%d_confirma%d", openW, confirmW), func(t *testing.T) {
				be := &seqNat{fakeNat: &fakeNat{info: natSample(true), plan: NatPlan{Text: natPlanText, Pending: 1, IsAdmin: true}, applyRes: NatResult{Removed: 1}}}
				m, tab := resizeNatModel(t, be, openW)
				rkey(m, "space")
				rkey(m, "r")
				if !tab.Capturing() {
					t.Fatal("a confirmação devia estar aberta")
				}
				shown := be.lastID
				for _, w := range []int{120, 109, 110, 120, 109, confirmW} {
					rresize(m, w, 40)
					scr := rscreen(m)
					if strings.Contains(scr, detailMarker) {
						t.Fatalf("%d colunas: painel apareceu durante a confirmação:\n%s", w, scr)
					}
					if !strings.Contains(scr, "Será removido") {
						t.Fatalf("%d colunas: o plano sumiu da confirmação:\n%s", w, scr)
					}
					rkey(m, "down") // rolável em qualquer largura
					rkey(m, "up")
					if !tab.Capturing() || len(be.applies) != 0 {
						t.Fatalf("%d colunas: confirmação perdida ou aplicou sozinha (%v)", w, be.applies)
					}
				}
				rresize(m, 50, 10) // oculta: nenhuma tecla de aplicar age
				for _, k := range []string{"s", "y", "enter", "S"} {
					rkey(m, k)
				}
				if len(be.applies) != 0 {
					t.Fatalf("aplicou com a tela oculta: %v", be.applyIDs)
				}
				rresize(m, confirmW, 40)
				if be.seq != 1 {
					t.Fatalf("o plano foi refeito ao redimensionar (%d planos)", be.seq)
				}
				rkey(m, "s")
				if len(be.applyIDs) != 1 || be.applyIDs[0] != shown {
					t.Fatalf("ID aplicado %v, exibido %q", be.applyIDs, shown)
				}
				scr := rscreen(m)
				if tab.Capturing() || strings.Contains(scr, "Será removido") {
					t.Fatalf("a confirmação devia ter fechado:\n%s", scr)
				}
				// a faixa de resultado não reabre nada ao redimensionar
				for _, w := range []int{109, 110, 120, 109} {
					rresize(m, w, 40)
					if tab.Capturing() || len(be.applies) != 1 {
						t.Fatalf("%d colunas: reabriu ou reaplicou (%v)", w, be.applies)
					}
				}
			})
		}
	}
}

func TestConfirmacaoNativosCancelarERedimensionarNaoReabreNemAplica(t *testing.T) {
	be := &seqNat{fakeNat: &fakeNat{info: natSample(true), plan: NatPlan{Text: natPlanText, Pending: 1, IsAdmin: true}}}
	m, tab := resizeNatModel(t, be, 120)
	rkey(m, "space")
	rkey(m, "r")
	rresize(m, 109, 40)
	rkey(m, "esc")
	if tab.Capturing() {
		t.Fatal("Esc devia cancelar")
	}
	for _, w := range []int{110, 120, 109, 120} {
		rresize(m, w, 40)
		scr := rscreen(m)
		if tab.Capturing() || strings.Contains(scr, "Será removido") {
			t.Fatalf("%d colunas: a confirmação reabriu:\n%s", w, scr)
		}
		if want := w >= 110; strings.Contains(scr, detailMarker) != want {
			t.Fatalf("%d colunas: painel=%v, esperado %v", w, !want, want)
		}
	}
	if len(be.applies) != 0 {
		t.Fatalf("aplicou depois de cancelar: %v", be.applyIDs)
	}
}

func TestConfirmacaoPrivacidadeRedimensionaEAplicaOIDExibido(t *testing.T) {
	sizes := []int{109, 110, 120}
	for _, openW := range sizes {
		for _, confirmW := range sizes {
			t.Run(fmt.Sprintf("abre%d_confirma%d", openW, confirmW), func(t *testing.T) {
				be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 2}, applyRes: PrivResult{Applied: 2}}
				tab := NewPrivacyTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
				m := NewWithAreas(Options{Mono: true, NoMotion: true}, []AreaDef{{Key: "seguranca", Title: "Segurança", Short: "Seg", Tabs: []Tab{tab}}})
				rresize(m, openW, 40)
				rpump(m, tab.Init())
				rkey(m, "a")
				if !tab.Capturing() {
					t.Fatal("a confirmação devia estar aberta")
				}
				shown := be.lastID
				for _, w := range []int{120, 109, 110, 120, 109, confirmW} {
					rresize(m, w, 40)
					if !strings.Contains(rscreen(m), "ID de publicidade desligado") {
						t.Fatalf("%d colunas: o plano sumiu", w)
					}
					rkey(m, "down")
					rkey(m, "up")
					if !tab.Capturing() || len(be.applies) != 0 || len(be.applyIDs) != 0 {
						t.Fatalf("%d colunas: perdeu a confirmação ou chamou Apply", w)
					}
				}
				rresize(m, 50, 10)
				for _, k := range []string{"s", "y", "enter"} {
					rkey(m, k)
				}
				if len(be.applyIDs) != 0 {
					t.Fatalf("aplicou com a tela oculta: %v", be.applyIDs)
				}
				rresize(m, confirmW, 40)
				if len(be.plans) != 1 {
					t.Fatalf("o plano foi refeito: %v", be.plans)
				}
				rkey(m, "s")
				if len(be.applyIDs) != 1 || be.applyIDs[0] != shown || len(be.applies) != 1 {
					t.Fatalf("ID aplicado %v, exibido %q", be.applyIDs, shown)
				}
			})
		}
	}
}

func TestConfirmacaoPrivacidadeCancelarERedimensionarNaoReabre(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 2}}
	tab := NewPrivacyTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	m := NewWithAreas(Options{Mono: true, NoMotion: true}, []AreaDef{{Key: "seguranca", Title: "Segurança", Short: "Seg", Tabs: []Tab{tab}}})
	rresize(m, 120, 40)
	rpump(m, tab.Init())
	rkey(m, "a")
	rresize(m, 109, 40)
	rkey(m, "esc")
	for _, w := range []int{110, 120, 109} {
		rresize(m, w, 40)
		if tab.Capturing() {
			t.Fatalf("%d colunas: a confirmação reabriu", w)
		}
	}
	if len(be.applyIDs) != 0 || len(be.plans) != 1 {
		t.Fatalf("chamadas indevidas: ids=%v planos=%v", be.applyIDs, be.plans)
	}
}
