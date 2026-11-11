package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"runtime"
	"time"
)

// OSRunner executa processos de verdade. Os testes não o usam para nada que mude o sistema.
type OSRunner struct {
	// GOOS e IsElevated são injetáveis para testar a lógica de elevação em qualquer SO.
	// Zero values: runtime.GOOS e "não elevado".
	GOOS       string
	IsElevated func() bool
}

func (r OSRunner) goos() string {
	if r.GOOS != "" {
		return r.GOOS
	}
	return runtime.GOOS
}

// argv devolve o nome e os argumentos finais, aplicando a regra de elevação.
func (r OSRunner) argv(c Cmd) (string, []string, error) {
	if !c.Elevate {
		return c.Name, c.Args, nil
	}
	if r.goos() == "windows" {
		if r.IsElevated == nil || !r.IsElevated() {
			return "", nil, ErrNotElevated
		}
		return c.Name, c.Args, nil
	}
	if os.Geteuid() == 0 && r.GOOS == "" {
		return c.Name, c.Args, nil
	}
	return "sudo", append([]string{"--", c.Name}, c.Args...), nil
}

// Run executa o comando e devolve o exit code bruto.
func (r OSRunner) Run(ctx context.Context, c Cmd) (Result, error) {
	name, args, err := r.argv(c)
	if err != nil {
		return Result{}, err
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := osexec.CommandContext(ctx, name, args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	cmd.Stdin = c.Stdin
	var stdout, stderr bytes.Buffer
	var out io.Writer = &stdout
	if c.Stdout != nil {
		out = io.MultiWriter(&stdout, c.Stdout)
	}
	cmd.Stdout, cmd.Stderr = out, &stderr
	cmd.WaitDelay = 2 * time.Second

	start := time.Now()
	runErr := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Duration: time.Since(start)}

	if runErr == nil {
		return res, nil
	}
	if c.Timeout > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return res, fmt.Errorf("%w: %s", ErrTimeout, c)
	}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	var exitErr *osexec.ExitError
	if errors.As(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	return res, fmt.Errorf("não foi possível iniciar %q: %w", c.Name, runErr)
}

// LookPath procura o executável no PATH.
func (OSRunner) LookPath(name string) (string, error) { return osexec.LookPath(name) }
