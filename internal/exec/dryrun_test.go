package exec_test

import (
	"context"
	"testing"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
)

func TestDryRunExecutaLeituraERegistraMutacao(t *testing.T) {
	fake := exectest.New(t).On("winget list", exectest.Exit(0, "Git 2.45"))
	dry := exec.NewDryRunRunner(fake)

	res, err := dry.Run(context.Background(), exec.Cmd{Name: "winget", Args: []string{"list"}})
	if err != nil || string(res.Stdout) != "Git 2.45" {
		t.Fatalf("leitura deveria passar ao Runner interno: %v %q", err, res.Stdout)
	}

	// Mutação: o FakeRunner falharia o teste se recebesse o comando.
	install := exec.Cmd{Name: "winget", Args: []string{"install", "Git.Git"}, Mutates: true}
	res, err = dry.Run(context.Background(), install)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("mutação: %v exit=%d", err, res.ExitCode)
	}
	planned := dry.Planned()
	if len(planned) != 1 || planned[0].String() != "winget install Git.Git" {
		t.Fatalf("planejado: %v", planned)
	}
	if len(fake.Calls()) != 1 {
		t.Fatalf("o Runner interno só deve ver a leitura: %v", fake.Calls())
	}
}

func TestDryRunLookPathDelega(t *testing.T) {
	fake := exectest.New(t).WithPath("brew", "/opt/homebrew/bin/brew")
	dry := exec.NewDryRunRunner(fake)
	if p, err := dry.LookPath("brew"); err != nil || p != "/opt/homebrew/bin/brew" {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := dry.LookPath("apt"); err == nil {
		t.Fatal("esperava erro")
	}
}
