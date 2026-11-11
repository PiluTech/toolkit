package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
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

	name, args, err := OSRunner{GOOS: "linux"}.argv(c)
	if err != nil || name != "sudo" || strings.Join(args, " ") != "-- apt-get install git" {
		t.Fatalf("linux: %s %v %v", name, args, err)
	}

	if _, _, err := (OSRunner{GOOS: "windows"}).argv(c); !errors.Is(err, ErrNotElevated) {
		t.Fatalf("windows sem elevação: %v", err)
	}
	elevated := OSRunner{GOOS: "windows", IsElevated: func() bool { return true }}
	name, args, err = elevated.argv(c)
	if err != nil || name != "apt-get" || len(args) != 2 {
		t.Fatalf("windows elevado: %s %v %v", name, args, err)
	}

	name, _, _ = OSRunner{GOOS: "linux"}.argv(Cmd{Name: "git"})
	if name != "git" {
		t.Fatalf("sem Elevate não pode prefixar sudo: %s", name)
	}
}

func TestOSRunnerLookPath(t *testing.T) {
	if _, err := (OSRunner{}).LookPath("toolkit-nao-existe-xyz"); err == nil {
		t.Fatal("esperava erro")
	}
}
