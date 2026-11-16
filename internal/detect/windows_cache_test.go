package detect

import (
	"context"
	"sync"
	"testing"

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
