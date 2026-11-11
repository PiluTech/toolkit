package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"
	"testing"
	"time"
)

// Os testes re-executam o próprio binário de teste como processo filho inofensivo
// (padrão TestHelperProcess): nada de instalador nem de comando do sistema.
func TestHelperProcess(_ *testing.T) {
	if os.Getenv("TOOLKIT_HELPER") != "1" {
		return
	}
	switch os.Getenv("TOOLKIT_MODE") {
	case "echo":
		_, _ = fmt.Fprint(os.Stdout, "saida")
		_, _ = fmt.Fprint(os.Stderr, "erro")
	case "exit3010":
		os.Exit(3010 % 256)
	case "sleep":
		time.Sleep(30 * time.Second)
	case "spawn": // cria um neto que dorme e informa o pid dele
		gc := osexec.CommandContext(context.Background(), os.Args[0], "-test.run=TestHelperProcess")
		gc.Env = append(os.Environ(), "TOOLKIT_MODE=sleep")
		if err := gc.Start(); err != nil {
			os.Exit(2)
		}
		_, _ = fmt.Fprint(os.Stdout, gc.Process.Pid)
		time.Sleep(30 * time.Second)
	case "flood":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("x", 100_000))
	case "env":
		_, _ = fmt.Fprint(os.Stdout, os.Getenv("TOOLKIT_X"))
	case "stdin":
		var b bytes.Buffer
		_, _ = b.ReadFrom(os.Stdin)
		_, _ = fmt.Fprint(os.Stdout, strings.ToUpper(b.String()))
	}
	os.Exit(0)
}

func helper(mode string, extraEnv ...string) Cmd {
	return Cmd{
		Name: os.Args[0],
		Args: []string{"-test.run=TestHelperProcess"},
		Env:  append([]string{"TOOLKIT_HELPER=1", "TOOLKIT_MODE=" + mode}, extraEnv...),
	}
}

func TestOSRunnerStdoutStderrTee(t *testing.T) {
	var tee bytes.Buffer
	c := helper("echo")
	c.Stdout = &tee
	res, err := OSRunner{}.Run(context.Background(), c)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("err=%v exit=%d", err, res.ExitCode)
	}
	if string(res.Stdout) != "saida" || string(res.Stderr) != "erro" || tee.String() != "saida" {
		t.Fatalf("stdout=%q stderr=%q tee=%q", res.Stdout, res.Stderr, tee.String())
	}
}

func TestOSRunnerExitCodeNaoEErro(t *testing.T) {
	res, err := OSRunner{}.Run(context.Background(), helper("exit3010"))
	if err != nil {
		t.Fatalf("exit code != 0 não pode ser erro: %v", err)
	}
	if res.ExitCode != 3010%256 {
		t.Fatalf("exit=%d", res.ExitCode)
	}
}

func TestOSRunnerEnvEStdin(t *testing.T) {
	res, _ := OSRunner{}.Run(context.Background(), helper("env", "TOOLKIT_X=valor"))
	if string(res.Stdout) != "valor" {
		t.Fatalf("env: %q", res.Stdout)
	}
	c := helper("stdin")
	c.Stdin = strings.NewReader("abc")
	res, _ = OSRunner{}.Run(context.Background(), c)
	if string(res.Stdout) != "ABC" {
		t.Fatalf("stdin: %q", res.Stdout)
	}
}

func TestOSRunnerTimeout(t *testing.T) {
	c := helper("sleep")
	c.Timeout = 200 * time.Millisecond
	_, err := OSRunner{}.Run(context.Background(), c)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("esperava ErrTimeout, veio %v", err)
	}
}

func TestOSRunnerContextoCancelado(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := OSRunner{}.Run(ctx, helper("sleep"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("esperava Canceled, veio %v", err)
	}
}

func TestOSRunnerNaoIniciaComandoInexistente(t *testing.T) {
	_, err := OSRunner{}.Run(context.Background(), Cmd{Name: "toolkit-nao-existe-xyz"})
	if err == nil || !strings.Contains(err.Error(), "não foi possível iniciar") {
		t.Fatalf("err=%v", err)
	}
}

func TestOSRunnerArgvElevacao(t *testing.T) {
	c := Cmd{Name: "apt-get", Args: []string{"install", "git"}, Elevate: true}

	name, args, viaSudo, err := OSRunner{GOOS: "linux"}.argv(c)
	if err != nil || name != "sudo" || !viaSudo || strings.Join(args, " ") != "-n -- apt-get install git" {
		t.Fatalf("linux: %s %v %v", name, args, err)
	}

	// O sudo descarta o ambiente: o Env vai por "env VAR=valor".
	c.Env = []string{"DEBIAN_FRONTEND=noninteractive"}
	_, args, _, _ = OSRunner{GOOS: "linux"}.argv(c)
	if strings.Join(args, " ") != "-n -- env DEBIAN_FRONTEND=noninteractive apt-get install git" {
		t.Fatalf("env com sudo: %v", args)
	}
	c.Env = nil

	if _, _, _, err := (OSRunner{GOOS: "windows"}).argv(c); !errors.Is(err, ErrNotElevated) {
		t.Fatalf("windows sem elevação: %v", err)
	}
	elevated := OSRunner{GOOS: "windows", IsElevated: func() bool { return true }}
	name, args, viaSudo, err = elevated.argv(c)
	if err != nil || name != "apt-get" || len(args) != 2 || viaSudo {
		t.Fatalf("windows elevado: %s %v %v", name, args, err)
	}

	name, _, _, _ = OSRunner{GOOS: "linux"}.argv(Cmd{Name: "git"})
	if name != "git" {
		t.Fatalf("sem Elevate não pode prefixar sudo: %s", name)
	}
}

func TestOSRunnerCheckElevation(t *testing.T) {
	c := Cmd{Name: "dism", Elevate: true}
	if err := (OSRunner{GOOS: "windows"}).CheckElevation(c); !errors.Is(err, ErrNotElevated) {
		t.Fatalf("windows sem token: %v", err)
	}
	if err := (OSRunner{GOOS: "windows", IsElevated: func() bool { return true }}).CheckElevation(c); err != nil {
		t.Fatalf("windows elevado: %v", err)
	}
	if err := (OSRunner{GOOS: "linux"}).CheckElevation(c); err != nil {
		t.Fatalf("unix não checa antes: %v", err)
	}
	if err := (OSRunner{GOOS: "windows"}).CheckElevation(Cmd{Name: "x"}); err != nil {
		t.Fatalf("sem Elevate: %v", err)
	}
}

// O prazo do chamador sai como erro do ctx; só o Timeout do Cmd vira ErrTimeout.
func TestOSRunnerPrazoDoChamadoNaoEErrTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	c := helper("sleep")
	c.Timeout = time.Minute
	_, err := OSRunner{}.Run(ctx, c)
	if errors.Is(err, ErrTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("esperava DeadlineExceeded do pai, veio %v", err)
	}
}

func TestOSRunnerTetoDeMemoria(t *testing.T) {
	var tee bytes.Buffer
	c := helper("flood")
	c.Stdout = &tee
	res, err := OSRunner{MaxOutput: 1024}.Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stdout) != 1024 || !res.Truncated {
		t.Fatalf("len=%d truncated=%v", len(res.Stdout), res.Truncated)
	}
	if tee.Len() != 100_000 {
		t.Fatalf("o tee deve receber tudo: %d", tee.Len())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disco cheio") }

func TestOSRunnerFalhaNoTeeEReportadaSemMatarOProcesso(t *testing.T) {
	c := helper("echo")
	c.Stdout = failingWriter{}
	res, err := OSRunner{}.Run(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "saída ao vivo") || !strings.Contains(err.Error(), "disco cheio") {
		t.Fatalf("err=%v", err)
	}
	if res.ExitCode != 0 || string(res.Stdout) != "saida" {
		t.Fatalf("o processo deve terminar normalmente e a saída ser guardada: exit=%d %q", res.ExitCode, res.Stdout)
	}
}

func TestOSRunnerLookPath(t *testing.T) {
	if _, err := (OSRunner{}).LookPath("toolkit-nao-existe-xyz"); err == nil {
		t.Fatal("esperava erro")
	}
}

// Cmd.Env só aceita NOME=valor: com Elevate o Env vira argumentos de "sudo -n -- env ...", e uma entrada
// como "-i", "--version" ou "ls" seria lida pelo env como opção ou como o comando a executar.
func TestEnvInvalidoEhRecusado(t *testing.T) {
	bad := [][]string{{"-i"}, {"--version"}, {"ls"}, {"=x"}, {"A B=1"}, {"1A=x"}, {"A=b\x00c"}, {""}, {"-C=/"}, {"A-B=1"}}
	for _, env := range bad {
		for _, elevate := range []bool{false, true} {
			r := OSRunner{GOOS: "linux"}
			_, err := r.Run(context.Background(), Cmd{Name: "echo-que-nunca-roda", Env: env, Elevate: elevate})
			if !errors.Is(err, ErrInvalidEnv) {
				t.Errorf("env %q (elevate=%v): esperava ErrInvalidEnv, veio %v", env, elevate, err)
			}
		}
	}
	for _, env := range [][]string{{"A=1"}, {"HOMEBREW_NO_AUTO_UPDATE=1"}, {"_X=", "Y9=a=b c"}} {
		if err := validateEnv(env); err != nil {
			t.Errorf("env %q deveria valer: %v", env, err)
		}
	}
}
