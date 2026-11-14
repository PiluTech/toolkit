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

func privBackend(t *testing.T, sys *privSys, st *privStore, goos string) *privacyBackend {
	t.Helper()
	return &privacyBackend{deps: privDeps(t, sys, st, goos, true, "")}
}

func TestPrivacyBackendForaDoWindowsNaoToca(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	b := privBackend(t, sys, st, "linux")
	info, err := b.Load(context.Background())
	if err != nil || info.Supported || info.OS != "linux" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if _, err := b.Plan(context.Background(), tui.PrivBasico, false); err == nil || !strings.Contains(err.Error(), "só existe no Windows") {
		t.Errorf("plan: %v", err)
	}
	if _, err := b.Apply(context.Background(), tui.PrivBasico, false); err == nil {
		t.Error("apply fora do Windows deveria falhar")
	}
	if sys.writes != 0 || st.b != nil {
		t.Error("nada pode mudar")
	}
}

func TestPrivacyBackendLoadEstados(t *testing.T) {
	sys, st := newPrivSys(false), &privStore{}
	b := privBackend(t, sys, st, "windows")
	info, err := b.Load(context.Background())
	if err != nil || !info.Supported || info.IsAdmin || info.Edition != privacidade.EditionPro {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if len(info.Settings) != len(privacidade.Settings()) {
		t.Fatalf("ajustes=%d", len(info.Settings))
	}
	for _, s := range info.Settings {
		if s.State != tui.PrivPending {
			t.Errorf("%s: %s (nada aplicado ainda)", s.Title, s.State)
		}
	}
	if _, err := b.Apply(context.Background(), tui.PrivBasico, false); err != nil {
		t.Fatal(err)
	}
	info, _ = b.Load(context.Background())
	applied := 0
	for _, s := range info.Settings {
		if s.State == tui.PrivApplied {
			applied++
			if s.Admin {
				t.Errorf("%s aplicado sem administrador", s.Title)
			}
		}
	}
	if applied != 6 {
		t.Errorf("aplicados=%d", applied)
	}
}

func TestPrivacyBackendPlanoNaoMudaESemAdminRecusa(t *testing.T) {
	sys, st := newPrivSys(false), &privStore{}
	b := privBackend(t, sys, st, "windows")
	ctx := context.Background()
	plan, err := b.Plan(ctx, tui.PrivRecomendado, false)
	if err != nil || !plan.NeedsAdmin || plan.IsAdmin || plan.Pending == 0 || !strings.Contains(plan.Text, "exige administrador") {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if _, err := b.Apply(ctx, tui.PrivRecomendado, false); !errors.Is(err, tui.ErrPrivNeedsAdmin) {
		t.Fatalf("err=%v", err)
	}
	if sys.writes != 0 || st.b != nil {
		t.Fatalf("recusa antes de mudar: writes=%d backup=%v", sys.writes, st.b)
	}
	user, err := b.Plan(ctx, tui.PrivRecomendado, true)
	if err != nil || user.NeedsAdmin || user.Pending == 0 {
		t.Fatalf("somente usuário: %+v err=%v", user, err)
	}
	res, err := b.Apply(ctx, tui.PrivRecomendado, true)
	if err != nil || res.Applied == 0 || res.BackupPath == "" || st.b == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestPrivacyBackendAplicarEDesfazer(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	b := privBackend(t, sys, st, "windows")
	ctx := context.Background()
	if _, err := b.UndoPlan(ctx); !errors.Is(err, tui.ErrPrivNoBackup) {
		t.Fatalf("sem backup: %v", err)
	}
	res, err := b.Apply(ctx, tui.PrivRecomendado, false)
	if err != nil || res.Applied == 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if again, err := b.Apply(ctx, tui.PrivRecomendado, false); err != nil || again.Applied != 0 {
		t.Fatalf("idempotente: %+v %v", again, err)
	}
	up, err := b.UndoPlan(ctx)
	if err != nil || up.Pending == 0 || !strings.Contains(up.Text, "->") {
		t.Fatalf("undo plan: %+v %v", up, err)
	}
	ur, err := b.Undo(ctx)
	if err != nil || ur.Applied == 0 || st.b != nil {
		t.Fatalf("undo: %+v %v backup=%v", ur, err, st.b)
	}
}

func TestPrivacyBackendPerfilInvalido(t *testing.T) {
	b := privBackend(t, newPrivSys(true), &privStore{}, "windows")
	if _, err := b.Plan(context.Background(), "xis", false); err == nil {
		t.Error("perfil inválido deveria falhar")
	}
}

// pumpTUI roda os comandos do Init como o Bubble Tea e devolve a tela depois de entregar as mensagens à raiz.
func pumpTUI(m *tui.Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			pumpTUI(m, c)
		}
	default:
		_, next := m.Update(msg)
		pumpTUI(m, next)
	}
}

func screenOf(t *testing.T, deps Deps, keys ...tea.KeyPressMsg) string {
	t.Helper()
	tab := newPrivacyTab(context.Background(), deps, tui.Theme{Mono: true, NoMotion: true})
	m := tui.New(tui.Options{Mono: true, NoMotion: true}, tab)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	pumpTUI(m, m.Init())
	for _, k := range keys {
		_, cmd := m.Update(k)
		pumpTUI(m, cmd)
	}
	return m.View().Content
}

func TestNewPrivacyTabMacMostraSoWindows(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	scr := screenOf(t, privDeps(t, sys, st, "darwin", true, ""))
	if !strings.Contains(scr, "Privacidade") || !strings.Contains(scr, "A privacidade só existe no Windows") {
		t.Fatalf("tela no Mac:\n%s", scr)
	}
	scr = screenOf(t, privDeps(t, sys, st, "darwin", true, ""), tea.KeyPressMsg{Code: 'a', Text: "a"}, tea.KeyPressMsg{Code: 's', Text: "s"})
	if sys.writes != 0 || st.b != nil || strings.Contains(scr, "Aplicado") {
		t.Errorf("no Mac nada muda:\n%s", scr)
	}
}

func TestNewPrivacyTabWindowsFluxoCompleto(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	deps := privDeps(t, sys, st, "windows", true, "")
	scr := screenOf(t, deps)
	for _, want := range []string{"Privacidade", "basico", "recomendado", "pendente", "ID de publicidade", "p ver plano", "a aplicar", "d desfazer"} {
		if !strings.Contains(scr, want) {
			t.Errorf("tela inicial sem %q:\n%s", want, scr)
		}
	}
	key := func(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }
	scr = screenOf(t, deps, key("p"))
	if !strings.Contains(scr, "não definido -> 0") || sys.writes != 0 {
		t.Errorf("plano:\n%s", scr)
	}
	scr = screenOf(t, deps, key("a"))
	if !strings.Contains(scr, "[s/N]") || sys.writes != 0 || st.b != nil {
		t.Errorf("confirmação:\n%s", scr)
	}
	scr = screenOf(t, deps, key("a"), key("s"))
	if !strings.Contains(scr, "Aplicado:") || sys.writes == 0 || st.b == nil {
		t.Errorf("aplicado:\n%s", scr)
	}
	scr = screenOf(t, deps, key("d"), key("s"))
	if !strings.Contains(scr, "Restaurado:") || st.b != nil {
		t.Errorf("desfeito:\n%s", scr)
	}
}

func TestNewPrivacyTabWindowsSemAdminAvisa(t *testing.T) {
	sys, st := newPrivSys(false), &privStore{}
	deps := privDeps(t, sys, st, "windows", true, "")
	key := func(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }
	scr := screenOf(t, deps)
	if !strings.Contains(scr, "não é administrador") {
		t.Errorf("sem aviso de administrador:\n%s", scr)
	}
	scr = screenOf(t, deps, key("j"), key("a"), key("s"))
	if sys.writes != 0 || st.b != nil || !strings.Contains(scr, "nada foi alterado") {
		t.Errorf("recomendado sem admin deveria recusar:\n%s", scr)
	}
}
