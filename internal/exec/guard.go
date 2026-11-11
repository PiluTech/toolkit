package exec

import (
	"context"
	"errors"
	"fmt"
)

// ErrWrongRunner indica que o Cmd foi enviado ao Runner do tipo errado (mutação no Runner de leitura, ou leitura no de mutação).
var ErrWrongRunner = errors.New("comando enviado ao Runner do tipo errado")

type guardRunner struct {
	inner    Runner
	readOnly bool
}

// ReadOnlyRunner devolve um Runner que só aceita leitura (ReadOnly=true e sem Elevate). Um provider usa este
// para Available, IsInstalled e ListVersions: um comando que pode alterar o sistema nunca passa por ele, e
// o contrato "leitura" deixa de depender da disciplina de cada builder.
func ReadOnlyRunner(inner Runner) Runner { return guardRunner{inner: inner, readOnly: true} }

// MutatingRunner devolve um Runner que recusa Cmd com ReadOnly=true. Um provider usa este para Install e Prepare:
// um comando de mutação marcado como leitura seria EXECUTADO pelo dry-run, então o código força o contrário.
func MutatingRunner(inner Runner) Runner { return guardRunner{inner: inner, readOnly: false} }

func (g guardRunner) Run(ctx context.Context, c Cmd) (Result, error) {
	if g.readOnly && (!c.ReadOnly || c.Elevate) {
		return Result{}, fmt.Errorf("%w: %q não é leitura (ReadOnly=false ou Elevate)", ErrWrongRunner, c.Name)
	}
	if !g.readOnly && c.ReadOnly {
		return Result{}, fmt.Errorf("%w: %q está marcado ReadOnly mas é mutação", ErrWrongRunner, c.Name)
	}
	return g.inner.Run(ctx, c)
}

func (g guardRunner) LookPath(name string) (string, error) { return g.inner.LookPath(name) }
