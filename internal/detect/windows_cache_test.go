package detect

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/exec"
)

// A TUI pergunta o estado de cada app do catálogo e, no Windows, cada pergunta rodava um PowerShell que lê TODO o
// registro de desinstalação (centenas de ms de CPU por app, 4 em paralelo na partida). Uma leitura serve a todos.
func TestWindowsUmaLeituraDoRegistroServeTodosOsApps(t *testing.T) {
	s := winStub(u("HKLM", "Git", "2.45.1", "P"), u("HKLM", "Discord", "1.0", "P"))
	d := New(s, Options{GOOS: "windows"})
	git, dis := winApp(t, "display_name: Git"), winApp(t, "display_name: Discord")
	for i := 0; i < 10; i++ {
		if f, ok := find(t, d, git); !ok || f.Version != "2.45.1" {
			t.Fatalf("git: %+v %v", f, ok)
		}
		if _, ok := find(t, d, dis); !ok {
			t.Fatal("discord")
		}
	}
	if len(s.calls) != 1 {
		t.Fatalf("PowerShell rodou %d vezes para 20 perguntas (esperado 1)", len(s.calls))
	}
	d.Invalidate() // depois de instalar ou remover algo a leitura é refeita
	find(t, d, git)
	if len(s.calls) != 2 {
		t.Fatalf("Invalidate deve forçar uma leitura nova: %d", len(s.calls))
	}
}

func TestWindowsPerguntasSimultaneasFazemUmaLeitura(t *testing.T) {
	s := winStub(u("HKLM", "Git", "2.45.1", "P"))
	d := New(s, Options{GOOS: "windows"})
	a := winApp(t, "display_name: Git")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = d.Find(context.Background(), a)
		}()
	}
	wg.Wait()
	if len(s.calls) != 1 {
		t.Fatalf("8 perguntas simultâneas fizeram %d leituras (esperado 1)", len(s.calls))
	}
}

// Leitura que falhou também vale pelo prazo: 40 apps não rodam 40 PowerShell de 30 s cada num registro quebrado; o
// resultado continua "leitura incompleta", nunca "não instalado".
func TestWindowsFalhaDaLeituraTambemEhReaproveitada(t *testing.T) {
	failing := &stub{handle: func(_ exec.Cmd) (exec.Result, error) { return exec.Result{ExitCode: 1}, nil }}
	d := New(failing, Options{GOOS: "windows"})
	a := winApp(t, "display_name: Git")
	for i := 0; i < 5; i++ {
		if f, ok := find(t, d, a); ok || !f.Incomplete {
			t.Fatalf("falha deve ser leitura incompleta: %+v %v", f, ok)
		}
	}
	if len(failing.calls) != 1 {
		t.Fatalf("%d execuções para 5 perguntas (esperado 1)", len(failing.calls))
	}
}

// Cancelamento NÃO é cacheado: a próxima pergunta, com contexto vivo, lê de verdade.
func TestWindowsCancelamentoNaoViraCache(t *testing.T) {
	s := winStub(u("HKLM", "Git", "2.45.1", "P"))
	d := New(s, Options{GOOS: "windows"})
	a := winApp(t, "display_name: Git")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := d.Find(ctx, a); err == nil {
		t.Fatal("contexto cancelado devolve erro")
	}
	if f, ok := find(t, d, a); !ok || f.Version != "2.45.1" {
		t.Fatalf("depois do cancelamento a leitura é feita: %+v %v", f, ok)
	}
}

// blockingStub deixa a leitura do registro presa até release ser fechado (e avisa quando ela começou).
func blockingStub(out string) (*stub, chan struct{}, chan struct{}) {
	release, started := make(chan struct{}), make(chan struct{}, 8)
	s := &stub{handle: func(exec.Cmd) (exec.Result, error) {
		started <- struct{}{}
		<-release
		return exec.Result{Stdout: []byte(out)}, nil
	}}
	return s, release, started
}

func within(t *testing.T, what string, d time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s: não voltou em %v", what, d)
	}
}

// Invalidate NUNCA espera uma leitura em andamento (o PowerShell pode levar até 30 s): invalida por GERAÇÃO. A leitura
// que já estava rodando entrega seu resultado a quem a pediu, mas não vira cache (é anterior ao Invalidate).
func TestWindowsInvalidateNaoEsperaALeituraEmAndamento(t *testing.T) {
	out := strings.Join([]string{u("HKLM", "Git", "2.45.1", "P"), st("U", "HKLM", "ok"), st("U", "HKLM-WOW64", "ok"), st("U", "HKCU", "ok"), st("A", "HKLM", "ok"), st("A", "HKCU", "ok")}, "\r\n") + "\r\n"
	s, release, started := blockingStub(out)
	d := New(s, Options{GOOS: "windows"})
	a := winApp(t, "display_name: Git")
	first := make(chan struct{})
	go func() { _, _, _ = d.Find(context.Background(), a); close(first) }()
	<-started
	within(t, "Invalidate durante a leitura", time.Second, d.Invalidate)
	close(release)
	<-first
	// a leitura anterior ao Invalidate não foi guardada: a próxima pergunta lê de novo
	find(t, d, a)
	if len(s.calls) != 2 {
		t.Fatalf("a leitura anterior ao Invalidate virou cache: %d leituras (esperado 2)", len(s.calls))
	}
	// e a nova vale pelo prazo
	find(t, d, a)
	if len(s.calls) != 2 {
		t.Fatalf("a leitura nova deveria ser reaproveitada: %d", len(s.calls))
	}
}

// Quem espera a leitura dos outros respeita o PRÓPRIO contexto: cancelou, volta na hora (não fica preso até o fim do
// PowerShell de outra pergunta).
func TestWindowsEsperaPelaLeituraRespeitaOContexto(t *testing.T) {
	s, release, started := blockingStub("")
	d := New(s, Options{GOOS: "windows"})
	a := winApp(t, "display_name: Git")
	go func() { _, _, _ = d.Find(context.Background(), a) }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	within(t, "pergunta com contexto cancelado esperando a leitura de outra", time.Second, func() {
		if _, _, err := d.Find(ctx, a); err == nil {
			t.Error("contexto cancelado devolve erro")
		}
	})
	close(release)
}

// Relógio controlado: o prazo do cache vale por Options.Now, sem dormir.
func TestWindowsPrazoDoCacheComRelogioControlado(t *testing.T) {
	s := winStub(u("HKLM", "Git", "2.45.1", "P"))
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	d := New(s, Options{GOOS: "windows", Now: func() time.Time { return now }})
	a := winApp(t, "display_name: Git")
	find(t, d, a)
	now = now.Add(cacheTTL - time.Second)
	find(t, d, a)
	if len(s.calls) != 1 {
		t.Fatalf("dentro do prazo deveria reaproveitar: %d leituras", len(s.calls))
	}
	now = now.Add(2 * time.Second)
	find(t, d, a)
	if len(s.calls) != 2 {
		t.Fatalf("passado o prazo deveria ler de novo: %d leituras", len(s.calls))
	}
}
