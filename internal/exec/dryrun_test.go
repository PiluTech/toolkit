package exec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
)

func TestDryRunExecutaSoLeituraExplicitaERegistraORestante(t *testing.T) {
	fake := exectest.New(t).On("winget list", exectest.Exit(0, "Git 2.45"))
	dry := exec.NewDryRunRunner(fake)

	res, err := dry.Run(context.Background(), exec.Cmd{Name: "winget", Args: []string{"list"}, ReadOnly: true})
	if err != nil || string(res.Stdout) != "Git 2.45" {
		t.Fatalf("leitura explícita deveria passar ao Runner interno: %v %q", err, res.Stdout)
	}

	// O FakeRunner falharia o teste se recebesse qualquer um destes.
	install := exec.Cmd{Name: "winget", Args: []string{"install", "Git.Git"}}
	if res, err = dry.Run(context.Background(), install); err != nil || res.ExitCode != 0 {
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

// Regressão do bloqueante do Crivo: um Cmd sem nenhuma flag NÃO pode chegar ao Runner interno.
func TestDryRunCmdSemFlagNaoChegaAoRunnerInterno(t *testing.T) {
	fake := exectest.New(t) // sem roteiro: qualquer chamada falha o teste
	dry := exec.NewDryRunRunner(fake)
	if _, err := dry.Run(context.Background(), exec.Cmd{Name: "touch", Args: []string{"/x"}}); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.Calls()); n != 0 {
		t.Fatalf("Runner interno recebeu %d chamada(s)", n)
	}
	if len(dry.Planned()) != 1 {
		t.Fatalf("deveria estar no plano: %v", dry.Planned())
	}
}

func TestDryRunElevateSempreContaComoMutacao(t *testing.T) {
	fake := exectest.New(t)
	dry := exec.NewDryRunRunner(fake)
	c := exec.Cmd{Name: "apt-get", Args: []string{"update"}, ReadOnly: true, Elevate: true}
	if _, err := dry.Run(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls()) != 0 || len(dry.Planned()) != 1 {
		t.Fatalf("calls=%v plano=%v", fake.Calls(), dry.Planned())
	}
}

func TestDryRunSinalizaErrNotElevatedNoPlano(t *testing.T) {
	dry := exec.NewDryRunRunner(exectest.New(t))
	dry.CheckElevation = exec.OSRunner{GOOS: "windows"}.CheckElevation // Windows sem token
	_, err := dry.Run(context.Background(), exec.Cmd{Name: "dism", Elevate: true})
	if !errors.Is(err, exec.ErrNotElevated) {
		t.Fatalf("err=%v", err)
	}
	plan := dry.Plan()
	if len(plan) != 1 || !errors.Is(plan[0].Err, exec.ErrNotElevated) {
		t.Fatalf("plano: %+v", plan)
	}
	// Sem Elevate não consulta nada.
	if _, err := dry.Run(context.Background(), exec.Cmd{Name: "x"}); err != nil {
		t.Fatal(err)
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

func TestDryRunRecusaEnvInvalidoComoORunnerReal(t *testing.T) {
	dry := exec.NewDryRunRunner(exectest.New(t))
	_, err := dry.Run(context.Background(), exec.Cmd{Name: "x", Env: []string{"-i"}, Elevate: true})
	if !errors.Is(err, exec.ErrInvalidEnv) {
		t.Fatalf("esperava ErrInvalidEnv, veio %v", err)
	}
	if len(dry.Planned()) != 0 {
		t.Errorf("comando inválido não deve entrar no plano: %v", dry.Planned())
	}
}
