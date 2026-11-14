package cli

import (
	"context"

	"github.com/PiluTech/toolkit/internal/otimizacao"
	"github.com/PiluTech/toolkit/internal/tui"
)

// otimBackend liga a seção Otimização da TUI à coleta do `toolkit otimizacao` (somente leitura: o Runner é o de leitura,
// nada é elevado nem executado a mais).
type otimBackend struct{ deps Deps }

func (b *otimBackend) Load(ctx context.Context) (*otimizacao.Report, error) {
	rep := b.deps.Otim.collect(ctx, b.deps)
	return rep, ctx.Err()
}

// newOtimizacaoTab monta a seção Otimização (área Máquina). Fora do Windows (deps.GOOS) ela só avisa, sem chamar o backend.
func newOtimizacaoTab(ctx context.Context, deps Deps, th tui.Theme) tui.Tab {
	return tui.NewOptimizeTab(ctx, &otimBackend{deps: deps}, th, deps.diagLang(), deps.GOOS)
}
