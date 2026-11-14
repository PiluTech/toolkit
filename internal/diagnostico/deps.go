package diagnostico

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/recipes"
)

// Deps são as portas do mundo: tudo que os coletores usam passa por aqui, então os testes injetam saídas falsas.
type Deps struct {
	// Runner executa os comandos de leitura (exec.Cmd com ReadOnly=true). O pacote o embrulha com exec.ReadOnlyRunner:
	// um comando que não seja leitura, ou com Elevate, é recusado.
	Runner   exec.Runner
	ReadFile func(path string) ([]byte, error)      // nil = os.ReadFile
	Glob     func(pattern string) ([]string, error) // nil = filepath.Glob
	Now      func() time.Time                       // nil = time.Now
	Lang     diagfmt.Lang
	// Home é a pasta pessoal do usuário (DiagnosticReports no macOS); vazio = os.UserHomeDir.
	Home string
}

func (d Deps) readFile(p string) ([]byte, error) {
	if d.ReadFile != nil {
		return d.ReadFile(p)
	}
	return os.ReadFile(p)
}

func (d Deps) glob(p string) ([]string, error) {
	if d.Glob != nil {
		return d.Glob(p)
	}
	return filepath.Glob(p)
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Deps) home() string {
	if d.Home != "" {
		return d.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

// run executa um comando de LEITURA (Cmd.ReadOnly=true) e devolve a saída. Um erro de execução ou um exit != 0 volta
// como erro: o chamador registra em Skipped e segue (uma área sem leitura nunca derruba o diagnóstico).
func (d Deps) run(ctx context.Context, c exec.Cmd) (string, error) {
	c.ReadOnly = true
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	res, err := exec.ReadOnlyRunner(d.Runner).Run(ctx, c)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return string(res.Stdout), &exitError{code: res.ExitCode, stderr: string(res.Stderr)}
	}
	return string(res.Stdout), nil
}

type exitError struct {
	code   int
	stderr string
}

func (e *exitError) Error() string {
	if e.stderr != "" {
		return "o comando saiu com código " + itoa(e.code) + ": " + firstLine(e.stderr)
	}
	return "o comando saiu com código " + itoa(e.code)
}

// readCmd monta um Cmd de leitura.
func readCmd(name string, args ...string) exec.Cmd {
	return exec.Cmd{Name: name, Args: args, ReadOnly: true}
}

// psCmd monta um Cmd de leitura que roda um script do PowerShell (caminho absoluto, -EncodedCommand), como as receitas.
func psCmd(script string, timeout time.Duration) exec.Cmd {
	c, _ := recipes.BuildCmd(&recipes.Action{PowerShell: script}, platform.Windows, timeout, false, true)
	return c
}

// Collect lê o estado da máquina do SO indicado. Cada área é independente: uma que falha vira Skipped e a coleta segue.
func Collect(ctx context.Context, goos string, d Deps) *Report {
	r := &Report{OS: goos, Generated: d.now()}
	switch goos {
	case "windows":
		collectWindows(ctx, d, r)
	case "linux":
		collectLinux(ctx, d, r)
	case "darwin":
		collectDarwin(ctx, d, r)
	default:
		r.Skipped = append(r.Skipped, Skipped{Area: "sistema", Reason: diagfmt.T(d.Lang, "sistema operacional sem suporte: "+goos, "unsupported operating system: "+goos, "sistema operativo no compatible: "+goos)})
	}
	return r
}
