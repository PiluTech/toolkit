// Package exec define o Runner: a única porta pela qual o toolkit executa processos.
// Tudo que altera o sistema passa por aqui, o que permite testar com um Runner falso
// e simular com --dry-run.
package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Erros devolvidos pelos Runners.
var (
	// ErrNotElevated: o Cmd exige elevação e o processo no Windows não está elevado.
	ErrNotElevated = errors.New("o comando exige privilégio de administrador")
	// ErrInvalidEnv: uma entrada de Cmd.Env não é NOME=valor.
	ErrInvalidEnv = errors.New("Cmd.Env só aceita entradas NOME=valor")
	// ErrTimeout: o Timeout do Cmd estourou (não o prazo do ctx do chamador: esse volta como erro do ctx).
	ErrTimeout = errors.New("tempo limite do comando esgotado")
)

// Cmd descreve um processo a executar. Nunca passa por shell implícito:
// quem quiser PowerShell/bash monta Name="powershell.exe" e Args explícitos.
type Cmd struct {
	Name     string
	Args     []string
	Env      []string      // acrescentado ao ambiente atual (ex.: HOMEBREW_NO_AUTO_UPDATE=1); só NOME=valor (validateEnv)
	Dir      string        // diretório de trabalho
	Stdin    io.Reader     // entrada opcional
	Stdout   io.Writer     // opcional: tee para o log ao vivo (TUI e --verbose)
	Timeout  time.Duration // 0 = só o ctx
	Elevate  bool          // Unix: prefixa "sudo -n" (nunca pede senha). Windows: exige processo já elevado. Conta sempre como mutação no dry-run
	ReadOnly bool          // true = só lê o sistema. Default (false) = pode alterar: o DryRunRunner só registra, não executa
	Secret   bool          // não mostra Args (ex.: comando com token)
}

// String devolve a linha de comando legível, sem Args quando Secret.
func (c Cmd) String() string {
	if c.Secret {
		if len(c.Args) == 0 {
			return c.Name
		}
		return c.Name + " ***"
	}
	parts := make([]string, 0, len(c.Args)+1)
	parts = append(parts, quote(c.Name))
	for _, a := range c.Args {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}

func quote(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"'$`\\") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// Result traz o exit code bruto. Exit code != 0 NÃO é erro do Runner:
// cada provider interpreta o seu (winget usa HRESULTs, msi usa 3010 etc.).
type Result struct {
	ExitCode int // no Windows, comparar como uint32 (HRESULT 0x8A15...)
	Stdout   []byte
	Stderr   []byte
	Duration time.Duration
	// Truncated: stdout ou stderr passou do teto de memória do OSRunner e o excedente foi descartado.
	Truncated bool
}

// Runner executa processos.
type Runner interface {
	// Run retorna erro só se o processo não pôde ser iniciado, o ctx expirou ou o Timeout estourou.
	Run(ctx context.Context, c Cmd) (Result, error)
	// LookPath existe no Runner para que Available() também seja mockável.
	LookPath(name string) (string, error)
}

// validateEnv exige NOME=valor em cada entrada (NOME = [A-Za-z_][A-Za-z0-9_]*, valor sem NUL). Com Elevate o Env
// vira argumentos de "sudo -n -- env ...": "-i", "--version" ou "ls" seriam lidos pelo env como opção ou como o
// comando a executar.
func validateEnv(env []string) error {
	for _, e := range env {
		name, val, ok := strings.Cut(e, "=")
		if !ok || name == "" || strings.ContainsRune(val, 0) {
			return fmt.Errorf("%w: %q", ErrInvalidEnv, e)
		}
		for i, r := range name {
			letter := r == '_' || (r|0x20 >= 'a' && r|0x20 <= 'z')
			if !letter && (i == 0 || r < '0' || r > '9') {
				return fmt.Errorf("%w: %q", ErrInvalidEnv, e)
			}
		}
	}
	return nil
}
