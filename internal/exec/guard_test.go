package exec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

func TestReadOnlyRunnerSoAceitaLeitura(t *testing.T) {
	f := exectest.New(t).On("ls", exectest.Exit(0, "ok")).WithPath("ls", "/bin/ls")
	r := exec.ReadOnlyRunner(f)
	if _, err := r.Run(context.Background(), exec.Cmd{Name: "ls", ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []exec.Cmd{{Name: "ls"}, {Name: "ls", ReadOnly: true, Elevate: true}, {Name: "ls", Elevate: true}} {
		if _, err := r.Run(context.Background(), c); !errors.Is(err, exec.ErrWrongRunner) {
			t.Errorf("%+v: err = %v", c, err)
		}
	}
	if len(f.Calls()) != 1 {
		t.Errorf("só a leitura chega ao Runner interno: %d chamadas", len(f.Calls()))
	}
	if p, err := r.LookPath("ls"); err != nil || p != "/bin/ls" {
		t.Errorf("LookPath = %q, %v", p, err)
	}
}

func TestMutatingRunnerRecusaReadOnly(t *testing.T) {
	f := exectest.New(t).On("rm", exectest.Exit(0, "")).OnElevated("rm", exectest.Exit(0, ""))
	r := exec.MutatingRunner(f)
	for _, c := range []exec.Cmd{{Name: "rm"}, {Name: "rm", Elevate: true}} {
		if _, err := r.Run(context.Background(), c); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	if _, err := r.Run(context.Background(), exec.Cmd{Name: "rm", ReadOnly: true}); !errors.Is(err, exec.ErrWrongRunner) {
		t.Errorf("err = %v", err)
	}
	if len(f.Calls()) != 2 {
		t.Errorf("chamadas = %d", len(f.Calls()))
	}
}
