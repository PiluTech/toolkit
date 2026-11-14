package cli

import (
	"context"
	"errors"
	"strconv"
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
	if _, err := b.Apply(context.Background(), tui.PrivBasico, false, b.lastID()); err == nil {
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
	if _, err := b.Plan(context.Background(), tui.PrivBasico, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(context.Background(), tui.PrivBasico, false, b.lastID()); err != nil {
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
	if _, err := b.Apply(ctx, tui.PrivRecomendado, false, b.lastID()); !errors.Is(err, tui.ErrPrivNeedsAdmin) {
		t.Fatalf("err=%v", err)
	}
	if sys.writes != 0 || st.b != nil {
		t.Fatalf("recusa antes de mudar: writes=%d backup=%v", sys.writes, st.b)
	}
	user, err := b.Plan(ctx, tui.PrivRecomendado, true)
	if err != nil || user.NeedsAdmin || user.Pending == 0 {
		t.Fatalf("somente usuário: %+v err=%v", user, err)
	}
	res, err := b.Apply(ctx, tui.PrivRecomendado, true, b.lastID())
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
	if _, err := b.Plan(ctx, tui.PrivRecomendado, false); err != nil {
		t.Fatal(err)
	}
	res, err := b.Apply(ctx, tui.PrivRecomendado, false, b.lastID())
	if err != nil || res.Applied == 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := b.Plan(ctx, tui.PrivRecomendado, false); err != nil {
		t.Fatal(err)
	}
	if again, err := b.Apply(ctx, tui.PrivRecomendado, false, b.lastID()); err != nil || again.Applied != 0 {
		t.Fatalf("idempotente: %+v %v", again, err)
	}
	up, err := b.UndoPlan(ctx)
	if err != nil || up.Pending == 0 || !strings.Contains(up.Text, "->") {
		t.Fatalf("undo plan: %+v %v", up, err)
	}
	ur, err := b.Undo(ctx, b.lastID())
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

// Lupa #66 (1): o "s" aplica EXATAMENTE o plano mostrado; se o sistema mudou entre o plano e a aplicação, recusa.
func TestPrivacyBackendAplicarSemPlanoPreparadoRecusa(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	b := privBackend(t, sys, st, "windows")
	if _, err := b.Apply(context.Background(), tui.PrivRecomendado, false, ""); !errors.Is(err, tui.ErrPrivStalePlan) {
		t.Fatalf("sem plano preparado deve recusar: %v", err)
	}
	if sys.writes != 0 || st.b != nil {
		t.Fatalf("nada pode mudar: writes=%d backup=%v", sys.writes, st.b)
	}
}

func TestPrivacyBackendMudarSystemEntrePlanoEAplicarRecusa(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	b := privBackend(t, sys, st, "windows")
	ctx := context.Background()
	if _, err := b.Plan(ctx, tui.PrivRecomendado, false); err != nil {
		t.Fatal(err)
	}
	// alguém muda o sistema depois de o plano ser mostrado
	for k, tk := range sys.tasks {
		tk.Enabled = false
		_ = k
		break
	}
	sys.edit = privacidade.EditionEnterprise
	if _, err := b.Apply(ctx, tui.PrivRecomendado, false, b.lastID()); !errors.Is(err, tui.ErrPrivStalePlan) {
		t.Fatalf("plano divergente deve recusar: %v", err)
	}
	if sys.writes != 0 || st.b != nil {
		t.Fatalf("nada pode mudar: writes=%d backup=%v", sys.writes, st.b)
	}
}

func TestPrivacyBackendPlanoDeUsoUnico(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	b := privBackend(t, sys, st, "windows")
	ctx := context.Background()
	if _, err := b.Plan(ctx, tui.PrivBasico, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(ctx, tui.PrivBasico, false, b.lastID()); err != nil {
		t.Fatal(err)
	}
	w := sys.writes
	if _, err := b.Apply(ctx, tui.PrivBasico, false, b.lastID()); !errors.Is(err, tui.ErrPrivStalePlan) {
		t.Fatalf("o plano é de uso único: %v", err)
	}
	if sys.writes != w {
		t.Fatal("segunda aplicação não pode escrever")
	}
}

func TestPrivacyBackendPlanoDeOutroPerfilNaoAplica(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	b := privBackend(t, sys, st, "windows")
	ctx := context.Background()
	if _, err := b.Plan(ctx, tui.PrivBasico, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(ctx, tui.PrivRecomendado, false, b.lastID()); !errors.Is(err, tui.ErrPrivStalePlan) {
		t.Fatalf("perfil diferente do plano mostrado: %v", err)
	}
	if sys.writes != 0 {
		t.Fatal("nada pode mudar")
	}
}

// Lupa #66 (1): trocar o backup entre UndoPlan e Undo nunca restaura outro plano em silêncio.
func TestPrivacyBackendTrocarBackupEntreUndoPlanEUndoRecusa(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	b := privBackend(t, sys, st, "windows")
	ctx := context.Background()
	if _, err := b.Plan(ctx, tui.PrivRecomendado, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(ctx, tui.PrivRecomendado, false, b.lastID()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.UndoPlan(ctx); err != nil {
		t.Fatal(err)
	}
	// o backup é trocado por outro (um valor original diferente) depois do plano de desfazer
	orig := st.b
	swapped := *orig
	swapped.Entries = map[string]privacidade.Entry{}
	for k, v := range orig.Entries {
		v.Existed = !v.Existed
		swapped.Entries[k] = v
		break
	}
	st.b = &swapped
	w := sys.writes
	if _, err := b.Undo(ctx, b.lastID()); !errors.Is(err, tui.ErrPrivStalePlan) {
		t.Fatalf("backup trocado deve recusar: %v", err)
	}
	if sys.writes != w || st.b == nil {
		t.Fatalf("nada pode mudar nem o backup ser removido: writes=%d->%d backup=%v", w, sys.writes, st.b)
	}
}

func TestPrivacyBackendUndoSemUndoPlanRecusa(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	b := privBackend(t, sys, st, "windows")
	ctx := context.Background()
	if _, err := b.Plan(ctx, tui.PrivBasico, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(ctx, tui.PrivBasico, false, b.lastID()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Undo(ctx, b.lastID()); !errors.Is(err, tui.ErrPrivStalePlan) {
		t.Fatalf("sem UndoPlan deve recusar: %v", err)
	}
	if st.b == nil {
		t.Fatal("o backup não pode ser removido")
	}
}

// lastID devolve o ID do plano preparado mais recente (só para os testes que não precisam do ID devolvido).
func (b *privacyBackend) lastID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	best, bestN := "", -1
	for id := range b.prepared {
		if n, err := strconv.Atoi(strings.TrimPrefix(id, "plano-")); err == nil && n > bestN {
			best, bestN = id, n
		}
	}
	return best
}

// Lupa #66 (3): A começa, Esc (a TUI abandona A), B começa e termina e é mostrado, A termina TARDE e grava; confirmar B
// aplica B (não A), mesmo perfil com estados diferentes; o ID de A nunca é aplicável. Backend REAL, System/Store falsos.
func TestPrivacyBackendPreparacaoCanceladaTardiaNaoSubstituiOPlanoMostrado(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	sys.entered, sys.gate = make(chan struct{}, 1), make(chan struct{})
	b := privBackend(t, sys, st, "windows")
	ctx := context.Background()
	if _, err := b.Plan(ctx, tui.PrivBasico, false); err != nil { // aquece o ambiente (SID etc.) sem bloquear
		t.Fatal(err)
	}
	b.take(b.lastID())

	sys.armed.Store(true)
	type res struct {
		p   tui.PrivPlan
		err error
	}
	aDone := make(chan res, 1)
	go func() { p, err := b.Plan(ctx, tui.PrivBasico, false); aDone <- res{p, err} }() // A: bloqueado em Edition
	<-sys.entered
	// (Esc na TUI: A é abandonado.) B: o MESMO perfil, mas o sistema agora está diferente (um item já aplicado)
	for _, st := range privacidade.Settings() {
		for _, r := range st.Regs {
			if !st.Admin {
				sys.regs[pk(r.Key, r.Name)] = r.Value(privacidade.EditionPro)
				goto changed
			}
		}
	}
changed:
	planB, err := b.Plan(ctx, tui.PrivBasico, false)
	if err != nil {
		t.Fatal(err)
	}
	close(sys.gate) // A é liberado TARDE
	a := <-aDone
	if a.err != nil || a.p.ID == "" || a.p.ID == planB.ID {
		t.Fatalf("A=%+v err=%v B=%+v", a.p, a.err, planB)
	}
	// confirmar B aplica B
	before := sys.writes
	got, err := b.Apply(ctx, tui.PrivBasico, false, planB.ID)
	if err != nil || got.Applied != planB.Pending {
		t.Fatalf("confirmar B deve aplicar B: res=%+v err=%v (B pendências=%d)", got, err, planB.Pending)
	}
	if sys.writes == before {
		t.Fatal("B não escreveu nada")
	}
	// o plano de A nunca é aplicável (descartado ao aplicar B)
	w := sys.writes
	if _, err := b.Apply(ctx, tui.PrivBasico, false, a.p.ID); !errors.Is(err, tui.ErrPrivStalePlan) {
		t.Fatalf("A não pode ser aplicado: %v", err)
	}
	if sys.writes != w {
		t.Fatal("A não pode escrever")
	}
}

// Mesmo cenário com o UndoPlan: a preparação de desfazer cancelada que termina tarde não substitui a exibida.
func TestPrivacyBackendUndoPlanCanceladoTardioNaoSubstituiOExibido(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	sys.entered, sys.gate = make(chan struct{}, 1), make(chan struct{})
	b := privBackend(t, sys, st, "windows")
	ctx := context.Background()
	if _, err := b.Plan(ctx, tui.PrivRecomendado, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(ctx, tui.PrivRecomendado, false, b.lastID()); err != nil {
		t.Fatal(err)
	}
	sys.armed.Store(true)
	aDone := make(chan tui.PrivPlan, 1)
	go func() { p, _ := b.UndoPlan(ctx); aDone <- p }() // A bloqueado
	<-sys.entered
	// o estado muda (um valor foi revertido à mão) e B é preparado e exibido
	for k := range sys.regs {
		delete(sys.regs, k)
		break
	}
	planB, err := b.UndoPlan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	close(sys.gate)
	a := <-aDone
	if a.ID == "" || a.ID == planB.ID {
		t.Fatalf("A=%+v B=%+v", a, planB)
	}
	// confirmar B restaura B (e descarta os demais planos preparados)
	got, err := b.Undo(ctx, planB.ID)
	if err != nil || got.Applied == 0 || st.b != nil {
		t.Fatalf("confirmar B deve restaurar B: res=%+v err=%v backup=%v", got, err, st.b)
	}
	if _, err := b.Undo(ctx, a.ID); !errors.Is(err, tui.ErrPrivStalePlan) {
		t.Fatalf("o plano de A (tardio) não pode ser aplicado depois de B: %v", err)
	}
}
