package tui

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// blockingBackend: Status e Versions só voltam quando o ctx é cancelado (como um `winget list` preso).
type blockingBackend struct {
	apps     []AppInfo
	inflight atomic.Int32
	started  atomic.Int32
}

func (b *blockingBackend) Load(context.Context) ([]AppInfo, error) { return b.apps, nil }
func (b *blockingBackend) Status(ctx context.Context, _ string) AppStatus {
	b.started.Add(1)
	b.inflight.Add(1)
	defer b.inflight.Add(-1)
	<-ctx.Done()
	return AppStatus{Err: ctx.Err()}
}
func (b *blockingBackend) Versions(ctx context.Context, _ string) ([]string, error) {
	b.inflight.Add(1)
	defer b.inflight.Add(-1)
	<-ctx.Done()
	return nil, ctx.Err()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("tempo esgotado esperando: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Ao sair da TUI as verificações em andamento são canceladas: nenhuma goroutine de Status/Versions
// continua viva depois que Run volta (Crivo no #35).
func TestRunCancelsBackendCallsOnQuit(t *testing.T) {
	be := &blockingBackend{apps: []AppInfo{
		{ID: "a", Name: "A", Category: "dev", Versions: true}, {ID: "b", Name: "B", Category: "dev"},
		{ID: "c", Name: "C", Category: "dev"}, {ID: "d", Name: "D", Category: "dev"}, {ID: "e", Name: "E", Category: "dev"},
	}}
	// o ctx que o chamador passa NÃO é cancelado: quem cancela é o Run, ao sair
	apps := NewAppsTab(context.Background(), be, Theme{Mono: true}, "linux")

	in, out := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- run(context.Background(), Options{Mono: true},
			[]tea.ProgramOption{tea.WithInput(in), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24)}, apps)
	}()
	waitFor(t, "as verificações começarem", func() bool { return be.inflight.Load() == maxStatusWorkers })

	// abre a lista de versões do primeiro app: mais uma chamada presa
	if _, err := out.Write([]byte(" ")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a consulta de versões começar", func() bool { return be.inflight.Load() > maxStatusWorkers })

	// sai: Esc fecha o modal e q sai
	if _, err := out.Write([]byte("\x1b")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := out.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("o Run não voltou depois do q")
	}
	waitFor(t, "nenhuma chamada do backend viva depois de sair", func() bool { return be.inflight.Load() == 0 })
}

// O ctx do chamador também encerra tudo (Ctrl+C no processo, cancelamento externo).
func TestRunCancelsWhenCallerContextIsCancelled(t *testing.T) {
	be := &blockingBackend{apps: []AppInfo{{ID: "a", Name: "A", Category: "dev"}}}
	apps := NewAppsTab(context.Background(), be, Theme{Mono: true}, "linux")
	ctx, cancel := context.WithCancel(context.Background())
	in, _ := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, Options{Mono: true}, []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24)}, apps)
	}()
	waitFor(t, "a verificação começar", func() bool { return be.inflight.Load() == 1 })
	cancel()
	select {
	case err := <-done:
		t.Logf("Run voltou: %v", err) // o erro de cancelamento é esperado
	case <-time.After(5 * time.Second):
		t.Fatal("o Run não voltou depois do cancelamento")
	}
	waitFor(t, "nenhuma chamada do backend viva", func() bool { return be.inflight.Load() == 0 })
}

// SetContext troca o ctx das chamadas seguintes do backend.
func TestAppsTabSetContext(t *testing.T) {
	be := &blockingBackend{apps: []AppInfo{{ID: "a", Name: "A", Category: "dev"}}}
	tab := NewAppsTab(context.Background(), be, Theme{Mono: true}, "linux")
	ctx, cancel := context.WithCancel(context.Background())
	tab.SetContext(ctx)
	_, cmd := tab.Update(tab.loadCmd()())
	done := make(chan struct{})
	go func() { _ = runBatch(cmd); close(done) }()
	waitFor(t, "chamada presa", func() bool { return be.inflight.Load() == 1 })
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a chamada não respeitou o ctx novo")
	}
}

// runBatch executa os comandos de um lote que devolvem mensagens de estado (ignora o tick).
func runBatch(cmd tea.Cmd) tea.Msg {
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			if c == nil {
				continue
			}
			if m := c(); m != nil {
				if _, isState := m.(appStateMsg); isState {
					return m
				}
			}
		}
	}
	return msg
}

// RunAreas entrega o ctx às seções de TODAS as áreas e volta quando o ctx do chamador é cancelado.
func TestRunAreasCancelsAndStopsEveryArea(t *testing.T) {
	be := &blockingBackend{apps: []AppInfo{{ID: "a", Name: "A", Category: "dev"}}}
	apps := NewAppsTab(context.Background(), be, Theme{Mono: true}, "linux")
	ctx, cancel := context.WithCancel(context.Background())
	in, _ := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- runAreas(ctx, Options{Mono: true}, []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24)},
			[]AreaDef{{Key: "inicio", Title: "Início", Short: "Iníc", Tabs: []Tab{NewInfoTab("Início", "Iníc", "oi")}}, {Key: "instalar", Title: "Instalar", Short: "Inst", Tabs: []Tab{apps}}})
	}()
	waitFor(t, "a verificação começar", func() bool { return be.inflight.Load() == 1 })
	cancel()
	select {
	case err := <-done:
		t.Logf("RunAreas voltou: %v", err) // o erro de cancelamento é esperado
	case <-time.After(5 * time.Second):
		t.Fatal("o RunAreas não voltou depois do cancelamento")
	}
	waitFor(t, "nenhuma chamada do backend viva", func() bool { return be.inflight.Load() == 0 })
}
