package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/PiluTech/toolkit/internal/privacidade"
	"github.com/PiluTech/toolkit/internal/tui"
)

func newAnimBackend(t *testing.T, sys *animSys, st *privStore, goos string) *animBackend {
	t.Helper()
	return &animBackend{deps: animDeps(t, sys, st, goos, false, "")}
}

func TestAnimBackendPlanoMostradoSoAplicaSeOSistemaNaoMudou(t *testing.T) {
	ctx := context.Background()
	sys, st := newAnimSys(), &privStore{}
	sys.set(animStr("1"))
	b := newAnimBackend(t, sys, st, "windows")
	p, err := b.Plan(ctx)
	if err != nil || p.Pending != 1 || p.ID == "" {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	sys.set(animStr("2")) // o sistema muda depois do plano mostrado
	if _, err := b.Apply(ctx, p.ID); !errors.Is(err, tui.ErrAnimStalePlan) {
		t.Fatalf("err=%v", err)
	}
	if sys.writes != 0 || st.b != nil {
		t.Fatal("plano velho não escreve nem grava backup")
	}
}

func TestAnimBackendIDDesconhecidoEUsoUnico(t *testing.T) {
	ctx := context.Background()
	sys, st := newAnimSys(), &privStore{}
	b := newAnimBackend(t, sys, st, "windows")
	if _, err := b.Apply(ctx, "plano-qualquer"); !errors.Is(err, tui.ErrAnimStalePlan) {
		t.Fatalf("ID desconhecido: %v", err)
	}
	p, _ := b.Plan(ctx)
	if _, err := b.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	sys.set(animStr("1"))
	if _, err := b.Apply(ctx, p.ID); !errors.Is(err, tui.ErrAnimStalePlan) {
		t.Fatalf("o plano é de uso único: %v", err)
	}
	if sys.writes != 1 {
		t.Fatalf("writes=%d", sys.writes)
	}
}

func TestAnimBackendUsarUmIDDescartaOsOutros(t *testing.T) {
	ctx := context.Background()
	sys, st := newAnimSys(), &privStore{}
	b := newAnimBackend(t, sys, st, "windows")
	a, _ := b.Plan(ctx)
	c, _ := b.Plan(ctx)
	if _, err := b.Apply(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	sys.set(animStr("1"))
	if _, err := b.Apply(ctx, a.ID); !errors.Is(err, tui.ErrAnimStalePlan) {
		t.Fatalf("o plano antigo foi descartado: %v", err)
	}
}

func TestAnimBackendDesfazerExigePlanoDeDesfazerEBackupIgual(t *testing.T) {
	ctx := context.Background()
	sys, st := newAnimSys(), &privStore{}
	sys.set(animStr("1"))
	b := newAnimBackend(t, sys, st, "windows")
	p, _ := b.Plan(ctx)
	if _, err := b.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	up, err := b.UndoPlan(ctx)
	if err != nil || up.Pending != 1 {
		t.Fatalf("%+v %v", up, err)
	}
	// o backup muda (outro processo): o plano mostrado não vale mais
	st.b.Created = st.b.Created.AddDate(0, 0, 1)
	if _, err := b.Undo(ctx, up.ID); !errors.Is(err, tui.ErrAnimStalePlan) {
		t.Fatalf("err=%v", err)
	}
	// um ID de plano de APLICAR não desfaz
	up2, _ := b.UndoPlan(ctx)
	pa, _ := b.Plan(ctx)
	_ = up2
	if _, err := b.Undo(ctx, pa.ID); !errors.Is(err, tui.ErrAnimStalePlan) {
		t.Fatalf("plano de aplicar usado para desfazer: %v", err)
	}
}

func TestAnimBackendBackupAdulteradoZeroMutacoes(t *testing.T) {
	ctx := context.Background()
	sys := newAnimSys()
	st := &privStore{b: &privacidade.Backup{Version: 1, Entries: map[string]privacidade.Entry{
		`reg:HKLM\SOFTWARE\X|N`: {ID: `reg:HKLM\SOFTWARE\X|N`, Kind: "reg", Key: `HKLM\SOFTWARE\X`, Name: "N", Existed: true}}}}
	b := newAnimBackend(t, sys, st, "windows")
	if _, err := b.UndoPlan(ctx); err == nil || !strings.Contains(err.Error(), "fora do catálogo") {
		t.Fatalf("err=%v", err)
	}
	p, _ := b.Plan(ctx)
	if _, err := b.Apply(ctx, p.ID); err == nil || sys.writes != 0 {
		t.Fatalf("aplicar com backup adulterado: err=%v writes=%d", err, sys.writes)
	}
}

func TestAnimBackendSoEscreveNoHKCU(t *testing.T) {
	ctx := context.Background()
	sys, st := newAnimSys(), &privStore{}
	b := newAnimBackend(t, sys, st, "windows")
	p, _ := b.Plan(ctx)
	if _, err := b.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	up, _ := b.UndoPlan(ctx)
	if _, err := b.Undo(ctx, up.ID); err != nil {
		t.Fatal(err)
	}
	for _, l := range sys.log {
		if !strings.Contains(l, `HKCU\`) || strings.Contains(strings.ToUpper(l), "HKLM") {
			t.Errorf("escrita fora do HKCU: %s", l)
		}
	}
}

func TestAnimBackendForaDoWindowsNaoTocaEmNada(t *testing.T) {
	ctx := context.Background()
	sys, st := newAnimSys(), &privStore{}
	b := newAnimBackend(t, sys, st, "linux")
	if _, err := b.Load(ctx); err == nil {
		t.Fatal("fora do Windows é erro")
	}
	if _, err := b.Plan(ctx); err == nil {
		t.Fatal("fora do Windows é erro")
	}
	if sys.writes != 0 || st.b != nil {
		t.Fatal("nada pode mudar")
	}
}

func TestTUIOtimizacaoTemAParteAnimacoesNoWindows(t *testing.T) {
	var calls int
	tab := otimTab(t, otimAreasWith(t, "windows", &calls, newAnimSys(), &privStore{}))
	_, cmd := tab.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	for _, m := range runBatch(cmd) {
		tab.Update(m)
	}
	v := tab.View(100, 30)
	if !strings.Contains(strings.ToLower(v), "animações do windows") || !strings.Contains(v, "pendente") || calls != 0 {
		t.Fatalf("calls=%d\n%s", calls, v)
	}
	hasKey := func(k string) bool {
		for _, h := range tab.Keys() {
			if h.Key == k {
				return true
			}
		}
		return false
	}
	if !hasKey("a") || !hasKey("d") || !hasKey("p") {
		t.Fatalf("teclas: %v", tab.Keys())
	}
}

func TestTUIOtimizacaoForaDoWindowsNaoOfereceAnimacoes(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		var calls int
		sys := newAnimSys()
		tab := otimTab(t, otimAreasWith(t, goos, &calls, sys, &privStore{}))
		for _, k := range []string{"m", "a", "d"} {
			if _, cmd := tab.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k}); cmd != nil {
				t.Errorf("%s: %s não faz nada", goos, k)
			}
		}
		if sys.writes != 0 {
			t.Errorf("%s: escreveu", goos)
		}
	}
}
