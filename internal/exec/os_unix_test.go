//go:build !windows

package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Timeout mata o filho E o neto (grupo de processos), não só o filho direto.
func TestOSRunnerTimeoutMataOsDescendentes(t *testing.T) {
	c := helper("spawn")
	c.Timeout = 500 * time.Millisecond
	res, err := OSRunner{}.Run(context.Background(), c)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("esperava ErrTimeout, veio %v", err)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
	if perr != nil || pid <= 0 {
		t.Fatalf("pid do neto: %q", res.Stdout)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("o neto %d continuou vivo depois do timeout", pid)
}

// stubSudo instala um "sudo" falso no PATH: grava os argumentos em args.txt e executa o script dado.
func stubSudo(t *testing.T, script string) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsFile + "'\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return argsFile
}

func TestOSRunnerSudoNaoPedeSenhaEDevolveErrNotElevated(t *testing.T) {
	argsFile := stubSudo(t, "echo 'sudo: a password is required' >&2\nexit 1")
	res, err := OSRunner{GOOS: "linux"}.Run(context.Background(),
		Cmd{Name: "apt-get", Args: []string{"update"}, Elevate: true})
	if !errors.Is(err, ErrNotElevated) {
		t.Fatalf("err=%v", err)
	}
	if res.ExitCode != 1 {
		t.Fatalf("exit=%d", res.ExitCode)
	}
	got, _ := os.ReadFile(argsFile)
	if !strings.HasPrefix(string(got), "-n\n--\napt-get\nupdate\n") {
		t.Fatalf("o sudo precisa receber -n: %q", got)
	}
}

func TestOSRunnerFalhaDoProprioComandoNaoViraErrNotElevated(t *testing.T) {
	stubSudo(t, "echo 'boom' >&2\nexit 1")
	res, err := OSRunner{GOOS: "linux"}.Run(context.Background(), Cmd{Name: "x", Elevate: true})
	if err != nil || res.ExitCode != 1 {
		t.Fatalf("exit != 0 do comando não é erro do Runner: %v exit=%d", err, res.ExitCode)
	}
}

func TestOSRunnerSudoPassaEnvPorEnvVar(t *testing.T) {
	argsFile := stubSudo(t, "exit 0")
	_, err := OSRunner{GOOS: "linux"}.Run(context.Background(),
		Cmd{Name: "apt-get", Args: []string{"install", "git"}, Env: []string{"DEBIAN_FRONTEND=noninteractive"}, Elevate: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(argsFile)
	want := "-n\n--\nenv\nDEBIAN_FRONTEND=noninteractive\napt-get\ninstall\ngit\n"
	if string(got) != want {
		t.Fatalf("argv do sudo: %q, quer %q", got, want)
	}
}
