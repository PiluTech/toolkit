package exectest_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
)

func TestFakeRespostasEmSequenciaERepeteUltima(t *testing.T) {
	f := exectest.New(t).On("git status", exectest.Exit(1, "a"), exectest.Exit(0, "b"))
	c := exec.Cmd{Name: "git", Args: []string{"status"}}
	var got []string
	for range 3 {
		r, err := f.Run(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(r.Stdout))
	}
	if got[0] != "a" || got[1] != "b" || got[2] != "b" {
		t.Fatalf("%v", got)
	}
	if len(f.Calls()) != 3 {
		t.Fatalf("calls=%d", len(f.Calls()))
	}
}

func TestFakeTeeStdout(t *testing.T) {
	var buf bytes.Buffer
	f := exectest.New(t).On("x", exectest.Exit(0, "saida"))
	_, _ = f.Run(context.Background(), exec.Cmd{Name: "x", Stdout: &buf})
	if buf.String() != "saida" {
		t.Fatalf("%q", buf.String())
	}
}

// Comando sem roteiro precisa falhar o teste: verificado com um TB espião.
type spy struct {
	testing.TB
	failed bool
}

func (s *spy) Helper()               {}
func (s *spy) Errorf(string, ...any) { s.failed = true }

func TestFakeComandoInesperadoFalha(t *testing.T) {
	s := &spy{TB: t}
	f := exectest.New(s)
	if _, err := f.Run(context.Background(), exec.Cmd{Name: "rm", Args: []string{"-rf", "x"}}); err == nil {
		t.Fatal("esperava erro")
	}
	if !s.failed {
		t.Fatal("o teste deveria ter sido marcado como falho")
	}
}

func TestFakeElevateFazParteDaChave(t *testing.T) {
	s := &spy{TB: t}
	f := exectest.New(s).OnElevated("apt-get update", exectest.Exit(0, "ok"))
	if _, err := f.Run(context.Background(), exec.Cmd{Name: "apt-get", Args: []string{"update"}}); err == nil || !s.failed {
		t.Fatal("sem Elevate não pode casar com o roteiro elevado")
	}
	s.failed = false
	r, err := f.Run(context.Background(), exec.Cmd{Name: "apt-get", Args: []string{"update"}, Elevate: true})
	if err != nil || string(r.Stdout) != "ok" || s.failed {
		t.Fatalf("elevado: %v %q", err, r.Stdout)
	}
}
