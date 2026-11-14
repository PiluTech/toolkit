package cli

import (
	"context"

	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/rede"
	"github.com/PiluTech/toolkit/internal/tui"
)

// redeBackend liga a seção Rede › Análise à coleta do `toolkit rede` (somente leitura: o Runner é o de leitura, nada é
// elevado, nada faz varredura e nada acessa a internet).
type redeBackend struct{ deps Deps }

func (b *redeBackend) Load(ctx context.Context) (*rede.Report, error) {
	ctx, cancel := context.WithTimeout(ctx, redeTimeout)
	defer cancel()
	return b.deps.redeCollect(ctx), ctx.Err()
}

// newRedeTab monta a seção Análise da área Rede.
func newRedeTab(ctx context.Context, deps Deps, th tui.Theme) *tui.RedeTab {
	return tui.NewRedeTab(ctx, &redeBackend{deps: deps}, th, deps.diagLang(), msg.TUISecaoAnalise, msg.TUISecaoAnaliseShort)
}
