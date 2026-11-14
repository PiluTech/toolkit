package cli

import (
	"context"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagnostico"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/tui"
)

func TestMaquinaBackendUsaDepsDoDiagnostico(t *testing.T) {
	var gotOS string
	var gotDeps diagnostico.Deps
	deps := Deps{GOOS: "linux"}
	deps.Diag = DiagDeps{
		Collect: func(_ context.Context, goos string, d diagnostico.Deps) *diagnostico.Report {
			gotOS, gotDeps = goos, d
			return &diagnostico.Report{OS: goos, Hardware: diagnostico.Hardware{Serial: "SN1"}}
		},
		Now:     func() time.Time { return time.Unix(0, 0) },
		IsAdmin: func(context.Context, string, exec.Runner) bool { return true },
	}
	rep, err := (&maquinaBackend{deps: deps}).Load(context.Background())
	if err != nil || rep == nil || rep.Hardware.Serial != "SN1" {
		t.Fatalf("rep=%v err=%v", rep, err)
	}
	if gotOS != "linux" || !gotDeps.Admin || gotDeps.Runner == nil {
		t.Fatalf("os=%q admin=%v runner=%v", gotOS, gotDeps.Admin, gotDeps.Runner)
	}
}

func TestMaquinaBackendCanceladoDevolveErro(t *testing.T) {
	deps := Deps{GOOS: "linux"}
	deps.Diag = DiagDeps{
		Collect: func(context.Context, string, diagnostico.Deps) *diagnostico.Report { return &diagnostico.Report{} },
		IsAdmin: func(context.Context, string, exec.Runner) bool { return false },
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&maquinaBackend{deps: deps}).Load(ctx); err == nil {
		t.Fatal("contexto cancelado deveria virar erro")
	}
}

func TestNewDiagTabMontaAAba(t *testing.T) {
	tab := newDiagTab(context.Background(), Deps{GOOS: "linux"}, RecipeDeps{}, tui.Theme{Mono: true})
	if tab.Title() != "Diagnóstico" {
		t.Fatalf("title=%q", tab.Title())
	}
}
