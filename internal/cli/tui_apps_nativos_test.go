package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/PiluTech/toolkit/internal/appsnativos"
	"github.com/PiluTech/toolkit/internal/tui"
)

func natBackend(t *testing.T, sys appsnativos.System, st *natStore, goos string) *appsNativosBackend {
	t.Helper()
	return &appsNativosBackend{deps: natDeps(t, sys, st, goos, true, "")}
}

// lastID devolve o ID do plano preparado mais recente (só para os testes que não precisam do ID devolvido).
func (b *appsNativosBackend) lastID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	best, bestN := "", -1
	for id := range b.prepared {
		var n int
		if _, err := fmt.Sscan(strings.TrimPrefix(id, "plano-"), &n); err == nil && n > bestN {
			best, bestN = id, n
		}
	}
	return best
}

var oneWeather = tui.NatSelection{Apps: []string{"bing-weather"}}

func TestAppsNativosBackendForaDoWindowsNaoToca(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "volume protegido", "linux": "não remove pacotes da distribuição"} {
		sys, st := newNatSys(true, "Microsoft.BingWeather"), &natStore{}
		b := natBackend(t, sys, st, goos)
		info, err := b.Load(context.Background())
		if err != nil || info.Supported || info.OS != goos || !strings.Contains(info.Reason, want) {
			t.Fatalf("%s: info=%+v err=%v", goos, info, err)
		}
		if _, err := b.Plan(context.Background(), oneWeather); err == nil {
			t.Errorf("%s: plan deveria falhar", goos)
		}
		if _, err := b.Apply(context.Background(), oneWeather, b.lastID()); err == nil {
			t.Errorf("%s: apply deveria falhar", goos)
		}
		if len(sys.removes) != 0 || st.b != nil {
			t.Errorf("%s: nada pode mudar", goos)
		}
	}
}

func TestAppsNativosBackendLoadEstados(t *testing.T) {
	sys := newNatSys(false, "Microsoft.BingWeather", "Microsoft.Todos")
	info, err := natBackend(t, sys, &natStore{}, "windows").Load(context.Background())
	if err != nil || !info.Supported || info.IsAdmin || len(info.Apps) != len(appsnativos.Apps()) {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	got := map[string]tui.NatApp{}
	for _, a := range info.Apps {
		got[a.ID] = a
	}
	if !got["bing-weather"].Installed || !got["bing-weather"].Basic || !got["todo"].Installed || got["todo"].Basic || got["skype"].Installed {
		t.Errorf("estados: %+v", got)
	}
	if got["bing-weather"].Title != "MSN Clima" {
		t.Errorf("título: %q", got["bing-weather"].Title)
	}
}

func TestAppsNativosBackendPlanoNaoMudaEAplicaERemove(t *testing.T) {
	sys, st := newNatSys(false, "Microsoft.BingWeather"), &natStore{}
	b := natBackend(t, sys, st, "windows")
	ctx := context.Background()
	plan, err := b.Plan(ctx, oneWeather)
	if err != nil || plan.ID == "" || plan.Pending != 1 || plan.NeedsAdmin || !strings.Contains(plan.Text, "Microsoft.BingWeather") {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if len(sys.removes) != 0 || st.b != nil {
		t.Fatal("plano só lê")
	}
	res, err := b.Apply(ctx, oneWeather, plan.ID)
	if err != nil || res.Removed != 1 || res.BackupPath == "" || st.b == nil || len(sys.removes) != 1 || !strings.HasPrefix(sys.removes[0], "user:") {
		t.Fatalf("res=%+v err=%v removes=%v", res, err, sys.removes)
	}
}

func TestAppsNativosBackendTodosUsuariosSemAdminRecusa(t *testing.T) {
	sys, st := newNatSys(false, "Microsoft.BingWeather"), &natStore{}
	b := natBackend(t, sys, st, "windows")
	sel := tui.NatSelection{Apps: []string{"bing-weather"}, AllUsers: true}
	if _, err := b.Plan(context.Background(), sel); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(context.Background(), sel, b.lastID()); err != nil && !errors.Is(err, tui.ErrNatStalePlan) && !errors.Is(err, tui.ErrNatNeedsAdmin) {
		t.Fatalf("err=%v", err)
	}
	if len(sys.removes) != 0 || st.b != nil {
		t.Fatalf("recusa antes de mudar: removes=%v backup=%v", sys.removes, st.b)
	}
}

func TestAppsNativosBackendSemPlanoPreparadoRecusa(t *testing.T) {
	sys, st := newNatSys(true, "Microsoft.BingWeather"), &natStore{}
	b := natBackend(t, sys, st, "windows")
	for _, id := range []string{"", "plano-999"} {
		if _, err := b.Apply(context.Background(), oneWeather, id); !errors.Is(err, tui.ErrNatStalePlan) {
			t.Fatalf("id %q sem plano deve recusar: %v", id, err)
		}
	}
	if len(sys.removes) != 0 || st.b != nil {
		t.Fatal("nada pode mudar")
	}
}

func TestAppsNativosBackendMudarSistemaEntrePlanoEAplicarRecusa(t *testing.T) {
	sys, st := newNatSys(true, "Microsoft.BingWeather"), &natStore{}
	b := natBackend(t, sys, st, "windows")
	ctx := context.Background()
	plan, err := b.Plan(ctx, oneWeather)
	if err != nil {
		t.Fatal(err)
	}
	sys.installed[0].Version = "9.9.9.9" // o app foi atualizado depois de o plano ser mostrado
	if _, err := b.Apply(ctx, oneWeather, plan.ID); !errors.Is(err, tui.ErrNatStalePlan) {
		t.Fatalf("plano divergente deve recusar: %v", err)
	}
	if len(sys.removes) != 0 || st.b != nil {
		t.Fatalf("nada pode mudar: removes=%v backup=%v", sys.removes, st.b)
	}
}

func TestAppsNativosBackendPlanoDeUsoUnico(t *testing.T) {
	sys, st := newNatSys(true, "Microsoft.BingWeather"), &natStore{}
	b := natBackend(t, sys, st, "windows")
	ctx := context.Background()
	plan, err := b.Plan(ctx, oneWeather)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(ctx, oneWeather, plan.ID); err != nil {
		t.Fatal(err)
	}
	n := len(sys.removes)
	if _, err := b.Apply(ctx, oneWeather, plan.ID); !errors.Is(err, tui.ErrNatStalePlan) {
		t.Fatalf("o plano é de uso único: %v", err)
	}
	if len(sys.removes) != n {
		t.Fatal("segunda aplicação não pode remover")
	}
}

func TestAppsNativosBackendSelecaoDiferenteDoPlanoNaoAplica(t *testing.T) {
	sys, st := newNatSys(true, "Microsoft.BingWeather", "Microsoft.Todos"), &natStore{}
	b := natBackend(t, sys, st, "windows")
	plan, err := b.Plan(context.Background(), oneWeather)
	if err != nil {
		t.Fatal(err)
	}
	other := tui.NatSelection{Apps: []string{"bing-weather", "todo"}}
	if _, err := b.Apply(context.Background(), other, plan.ID); !errors.Is(err, tui.ErrNatStalePlan) {
		t.Fatalf("seleção diferente da mostrada: %v", err)
	}
	if len(sys.removes) != 0 {
		t.Fatal("nada pode mudar")
	}
}

func TestAppsNativosBackendSemSelecaoERecusa(t *testing.T) {
	b := natBackend(t, newNatSys(true), &natStore{}, "windows")
	if _, err := b.Plan(context.Background(), tui.NatSelection{}); err == nil {
		t.Error("sem apps marcados deve falhar")
	}
	if _, err := b.Plan(context.Background(), tui.NatSelection{Apps: []string{"xis"}}); err == nil {
		t.Error("app fora do catálogo deve falhar")
	}
}

func TestAppsNativosBackendReinstalarSoTexto(t *testing.T) {
	sys, st := newNatSys(true, "Microsoft.BingWeather"), &natStore{}
	b := natBackend(t, sys, st, "windows")
	ctx := context.Background()
	if _, err := b.Reinstall(ctx); !errors.Is(err, tui.ErrNatNoBackup) {
		t.Fatalf("sem backup: %v", err)
	}
	plan, _ := b.Plan(ctx, oneWeather)
	if _, err := b.Apply(ctx, oneWeather, plan.ID); err != nil {
		t.Fatal(err)
	}
	n := len(sys.removes)
	text, err := b.Reinstall(ctx)
	if err != nil || !strings.Contains(text, "MSN Clima") || !strings.Contains(text, "Microsoft Store") {
		t.Fatalf("texto=%q err=%v", text, err)
	}
	if len(sys.removes) != n || strings.ContainsRune(text, 0x1b) {
		t.Fatal("reinstalar não executa nada")
	}
}

// gatedNatSys bloqueia ListInstalled DEPOIS de ler (devolve o estado de quando entrou) até o teste liberar.
type gatedNatSys struct {
	*natSys
	armed   atomic.Bool
	entered chan struct{}
	gate    chan struct{}
}

func (g *gatedNatSys) ListInstalled(ctx context.Context, all bool) ([]appsnativos.Package, error) {
	out, err := g.natSys.ListInstalled(ctx, all)
	if g.armed.CompareAndSwap(true, false) {
		g.entered <- struct{}{}
		<-g.gate
	}
	return out, err
}

// A começa, Esc (a TUI abandona A), B começa e termina e é mostrado, A termina TARDE e grava; confirmar B aplica B (não A),
// mesmo com a mesma seleção; o ID de A nunca é aplicável. Backend REAL, System/Store falsos.
func TestAppsNativosBackendPreparacaoCanceladaTardiaNaoSubstituiOPlanoMostrado(t *testing.T) {
	g := &gatedNatSys{natSys: newNatSys(true, "Microsoft.BingWeather"), entered: make(chan struct{}, 1), gate: make(chan struct{})}
	st := &natStore{}
	b := natBackend(t, g, st, "windows")
	ctx := context.Background()
	if _, err := b.Plan(ctx, oneWeather); err != nil { // aquece o ambiente (SID etc.) sem bloquear
		t.Fatal(err)
	}
	b.take(b.lastID())

	g.armed.Store(true)
	type res struct {
		p   tui.NatPlan
		err error
	}
	aDone := make(chan res, 1)
	go func() { p, err := b.Plan(ctx, oneWeather); aDone <- res{p, err} }() // A: bloqueado depois de ler
	<-g.entered
	g.natSys.installed[0].Version = "2.0.0.0" // o sistema mudou; B vê o estado novo
	planB, err := b.Plan(ctx, oneWeather)
	if err != nil {
		t.Fatal(err)
	}
	close(g.gate) // A é liberado TARDE
	a := <-aDone
	if a.err != nil || a.p.ID == "" || a.p.ID == planB.ID || a.p.Text == planB.Text {
		t.Fatalf("A=%+v err=%v B=%+v", a.p, a.err, planB)
	}
	got, err := b.Apply(ctx, oneWeather, planB.ID)
	if err != nil || got.Removed != 1 || len(g.removes) != 1 || !strings.Contains(g.removes[0], "_1.2.3.0_") && !strings.Contains(g.removes[0], "BingWeather") {
		t.Fatalf("confirmar B deve aplicar B: res=%+v err=%v removes=%v", got, err, g.removes)
	}
	n := len(g.removes)
	if _, err := b.Apply(ctx, oneWeather, a.p.ID); !errors.Is(err, tui.ErrNatStalePlan) {
		t.Fatalf("o plano de A nunca é aplicável: %v", err)
	}
	if len(g.removes) != n {
		t.Fatal("A não pode remover")
	}
}

func natScreen(t *testing.T, deps Deps, keys ...tea.KeyPressMsg) string {
	t.Helper()
	tab := newAppsNativosTab(context.Background(), deps, tui.Theme{Mono: true, NoMotion: true})
	m := tui.New(tui.Options{Mono: true, NoMotion: true}, tab)
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	pumpTUI(m, m.Init())
	for _, k := range keys {
		_, cmd := m.Update(k)
		pumpTUI(m, cmd)
	}
	return m.View().Content
}

func nk(s string) tea.KeyPressMsg {
	if s == "space" {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func TestNewAppsNativosTabForaDoWindowsMostraSoWindows(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "volume protegido", "linux": "não remove pacotes da distribuição"} {
		sys, st := newNatSys(true, "Microsoft.BingWeather"), &natStore{}
		scr := natScreen(t, natDeps(t, sys, st, goos, true, ""), nk("space"), nk("b"), nk("r"), nk("s"))
		flat := strings.Join(strings.Fields(scr), " ")
		if !strings.Contains(flat, "i Não disponível neste sistema") || !strings.Contains(flat, "Os apps nativos: só existe no Windows") || !strings.Contains(flat, want) {
			t.Fatalf("%s:\n%s", goos, scr)
		}
		if len(sys.removes) != 0 || st.b != nil {
			t.Errorf("%s: nada muda", goos)
		}
	}
}

func TestNewAppsNativosTabWindowsFluxoCompleto(t *testing.T) {
	sys, st := newNatSys(true, "Microsoft.BingWeather", "Microsoft.Todos"), &natStore{}
	deps := natDeps(t, sys, st, "windows", true, "")
	scr := natScreen(t, deps)
	low := strings.ToLower(scr)
	for _, want := range []string{"apps nativos do windows", "MSN Clima", "instalado", "ausente", "administrador: sim"} {
		if !strings.Contains(scr, want) && !strings.Contains(low, want) {
			t.Errorf("tela inicial sem %q:\n%s", want, scr)
		}
	}
	scr = natScreen(t, deps, nk("space"), nk("p"))
	if !strings.Contains(scr, "Microsoft.BingWeather") || len(sys.removes) != 0 {
		t.Errorf("plano:\n%s", scr)
	}
	scr = natScreen(t, deps, nk("space"), nk("r"))
	if !strings.Contains(scr, ">[ Cancelar ]<") || len(sys.removes) != 0 || st.b != nil {
		t.Errorf("confirmação:\n%s", scr)
	}
	scr = natScreen(t, deps, nk("space"), nk("r"), nk("n"))
	if !strings.Contains(scr, "Cancelado") || len(sys.removes) != 0 {
		t.Errorf("cancelado:\n%s", scr)
	}
	scr = natScreen(t, deps, nk("space"), nk("r"), nk("s"))
	if !strings.Contains(scr, "Removidos: 1 app(s)") || len(sys.removes) != 1 || st.b == nil {
		t.Errorf("removido:\n%s", scr)
	}
	scr = natScreen(t, deps, nk("i"))
	if !strings.Contains(strings.ToLower(scr), "como reinstalar") || !strings.Contains(scr, "MSN Clima") || len(sys.removes) != 1 {
		t.Errorf("reinstalar:\n%s", scr)
	}
}

func TestNewAppsNativosTabSemAdminAvisaETodosUsuarios(t *testing.T) {
	sys, st := newNatSys(false, "Microsoft.BingWeather"), &natStore{}
	deps := natDeps(t, sys, st, "windows", true, "")
	scr := natScreen(t, deps)
	if !strings.Contains(scr, "não é administrador") {
		t.Errorf("sem aviso:\n%s", scr)
	}
	scr = natScreen(t, deps, nk("u"))
	if strings.Contains(scr, "[x] todos os usuários") || !strings.Contains(scr, "exige administrador") {
		t.Errorf("u sem admin:\n%s", scr)
	}
	natScreen(t, deps, nk("space"), nk("r"), nk("s"))
	if len(sys.removes) != 1 || !strings.HasPrefix(sys.removes[0], "user:") {
		t.Errorf("sem admin só remove para o usuário: %v", sys.removes)
	}
}

// Lupa #65 (3): o plano guarda os itens ESTRUTURADOS; mudar só o PackageFullName (ex.: outra arquitetura) entre o plano e a
// aplicação não passa (o texto do plano pode não mostrar o FullName).
func TestAppsNativosBackendMudarSoOPackageFullNameRecusa(t *testing.T) {
	for _, field := range []string{"FullName", "FamilyName", "Publisher"} {
		sys, st := newNatSys(true, "Microsoft.BingWeather"), &natStore{}
		b := natBackend(t, sys, st, "windows")
		ctx := context.Background()
		plan, err := b.Plan(ctx, oneWeather)
		if err != nil {
			t.Fatal(err)
		}
		switch field {
		case "FullName":
			sys.installed[0].PackageFullName = "Microsoft.BingWeather_1.2.3.0_arm64__8wekyb3d8bbwe"
		case "FamilyName":
			sys.installed[0].PackageFamilyName = "Microsoft.BingWeather_zzzzzzzzzzzzz"
		case "Publisher":
			sys.installed[0].Publisher = "CN=Outro Publicador"
		}
		if _, err := b.Apply(ctx, oneWeather, plan.ID); !errors.Is(err, tui.ErrNatStalePlan) {
			t.Fatalf("%s: mudar só esse campo deve recusar: %v", field, err)
		}
		if len(sys.removes) != 0 || st.b != nil {
			t.Fatalf("%s: zero remoções e sem backup: removes=%v backup=%v", field, sys.removes, st.b)
		}
	}
}
