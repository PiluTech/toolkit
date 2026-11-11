package exec

import (
	"context"
	"sync"
)

// Step é um comando que o dry-run deixou de executar. Err é ErrNotElevated quando o comando
// exige elevação que o processo atual não tem (o plano já mostra que a execução real falharia).
type Step struct {
	Cmd Cmd
	Err error
}

// DryRunRunner envolve outro Runner e FALHA FECHADO: só executa o que é declarado explicitamente
// somente leitura (Cmd.ReadOnly=true, e sem Elevate). Todo o resto, inclusive um Cmd sem flag
// nenhuma, é apenas registrado e nunca chega ao Runner interno.
type DryRunRunner struct {
	Inner Runner
	// CheckElevation, se definido, é consultado para comandos com Elevate (ex.: OSRunner.CheckElevation):
	// um erro entra no plano e é devolvido por Run. Nil = não verifica.
	CheckElevation func(Cmd) error

	mu    sync.Mutex
	steps []Step
}

// NewDryRunRunner cria o wrapper sobre inner.
func NewDryRunRunner(inner Runner) *DryRunRunner { return &DryRunRunner{Inner: inner} }

// Run executa só leituras explícitas; registra o resto (exit code 0, sem saída).
func (d *DryRunRunner) Run(ctx context.Context, c Cmd) (Result, error) {
	if c.ReadOnly && !c.Elevate {
		return d.Inner.Run(ctx, c)
	}
	var err error
	if c.Elevate && d.CheckElevation != nil {
		err = d.CheckElevation(c)
	}
	d.mu.Lock()
	d.steps = append(d.steps, Step{Cmd: c, Err: err})
	d.mu.Unlock()
	return Result{}, err
}

// LookPath delega ao Runner interno.
func (d *DryRunRunner) LookPath(name string) (string, error) { return d.Inner.LookPath(name) }

// Plan devolve, em ordem, os passos que não foram executados (com o erro de elevação, se houver).
func (d *DryRunRunner) Plan() []Step {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Step(nil), d.steps...)
}

// Planned devolve só os comandos de Plan.
func (d *DryRunRunner) Planned() []Cmd {
	steps := d.Plan()
	cmds := make([]Cmd, len(steps))
	for i, s := range steps {
		cmds[i] = s.Cmd
	}
	return cmds
}
