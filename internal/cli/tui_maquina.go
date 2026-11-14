package cli

import (
	"context"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/diagnostico"
	"github.com/PiluTech/toolkit/internal/tui"
)

// maquinaTimeout é o prazo da leitura da máquina na TUI (o mesmo do `toolkit diagnostico`).
const maquinaTimeout = 5 * time.Minute

// maquinaBackend liga a visão "Máquina" da aba Diagnóstico à coleta do `toolkit diagnostico` (somente leitura:
// o Runner é o de leitura, nada é elevado nem executado a mais).
type maquinaBackend struct{ deps Deps }

func (b *maquinaBackend) Load(ctx context.Context) (*diagnostico.Report, error) {
	ctx, cancel := context.WithTimeout(ctx, maquinaTimeout)
	defer cancel()
	collect := b.deps.Diag.Collect
	if collect == nil {
		collect = diagnostico.Collect
	}
	now := b.deps.Diag.Now
	if now == nil {
		now = time.Now
	}
	rn := b.deps.runner()
	d := diagnostico.Deps{Runner: rn, Now: now, Lang: b.deps.diagLang(), Admin: b.deps.Diag.admin(ctx, b.deps.GOOS, rn)}
	return collect(ctx, b.deps.GOOS, d), ctx.Err()
}

func (d Deps) diagLang() diagfmt.Lang { return diagfmt.LangFromEnv(d.UpdateCheck.env) }

// newDiagTab monta a aba Diagnóstico completa (receitas de diagnóstico + visão Máquina). É o que o openTUI deve usar
// no lugar de tui.NewRecipesTab(ctx, ..., th, true).
func newDiagTab(ctx context.Context, deps Deps, rd RecipeDeps, th tui.Theme) *tui.DiagTab {
	return tui.NewDiagTab(ctx, &recipesBackend{deps: deps, rd: rd}, &maquinaBackend{deps: deps}, th, deps.diagLang())
}
