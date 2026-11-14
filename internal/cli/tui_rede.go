package cli

import (
	"context"

	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/rede"
	"github.com/PiluTech/toolkit/internal/tui"
)

// redeBackend liga a seção Rede › Análise à coleta do `toolkit rede` (somente leitura: o Runner é o de leitura, nada é
// elevado, nada faz varredura e a leitura não acessa a internet; as consultas de IP público e portal, sob pedido e
// confirmadas, passam por PlanQuery e Query).
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

// PlanQuery resolve e valida o serviço configurado (TOOLKIT_IP_URL ou TOOLKIT_PORTAL_URL) SEM acessar a rede.
func (b *redeBackend) PlanQuery(kind rede.QueryKind) (rede.Plan, error) {
	return b.deps.redePlan(kind, "")
}

// Query envia a requisição do plano JÁ confirmado na TUI (só depois da confirmação; usa a URL do plano exibido).
func (b *redeBackend) Query(ctx context.Context, p rede.Plan) tui.NetQueryResult {
	ctx, cancel := context.WithTimeout(ctx, redeTimeout)
	defer cancel()
	rep := &rede.Report{}
	b.deps.redeQuery(ctx, p, rep)
	return tui.NetQueryResult{IP: rep.PublicIP, Portal: rep.Portal}
}
