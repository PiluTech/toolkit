package exec

import (
	"context"
	"sync"
)

// DryRunRunner envolve outro Runner: executa os comandos de leitura (Mutates=false), porque o
// dry-run precisa mostrar o que já está instalado, e só registra os que alteram o sistema.
type DryRunRunner struct {
	Inner Runner

	mu      sync.Mutex
	skipped []Cmd
}

// NewDryRunRunner cria o wrapper sobre inner.
func NewDryRunRunner(inner Runner) *DryRunRunner { return &DryRunRunner{Inner: inner} }

// Run executa leituras e apenas registra as mutações (exit code 0, sem saída).
func (d *DryRunRunner) Run(ctx context.Context, c Cmd) (Result, error) {
	if !c.Mutates {
		return d.Inner.Run(ctx, c)
	}
	d.mu.Lock()
	d.skipped = append(d.skipped, c)
	d.mu.Unlock()
	return Result{}, nil
}

// LookPath delega ao Runner interno.
func (d *DryRunRunner) LookPath(name string) (string, error) { return d.Inner.LookPath(name) }

// Planned devolve, em ordem, os comandos que seriam executados.
func (d *DryRunRunner) Planned() []Cmd {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Cmd(nil), d.skipped...)
}
