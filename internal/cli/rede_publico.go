package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/rede"
	"github.com/PiluTech/toolkit/internal/rede/httpclient"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// redeFetcher é o cliente das consultas (nil em RedeDeps = o cliente real: TLS 1.2+, sem proxy, sem cookies, sem seguir
// redirecionamentos). Só os subcomandos ip e portal e a TUI, depois da confirmação, o usam.
func (d Deps) redeFetcher() rede.Fetcher {
	if d.Rede.Fetcher != nil {
		return d.Rede.Fetcher
	}
	return httpclient.New()
}

// redeServiceURL é a URL do serviço: a flag vale mais que a variável de ambiente.
func (d Deps) redeServiceURL(kind rede.QueryKind, flag string) string {
	if strings.TrimSpace(flag) != "" {
		return flag
	}
	if kind == rede.KindPortal {
		return d.UpdateCheck.env(rede.EnvPortalURL)
	}
	return d.UpdateCheck.env(rede.EnvIPURL)
}

// redePlan monta o plano da consulta (a validação da URL acontece aqui, antes de qualquer envio).
func (d Deps) redePlan(kind rede.QueryKind, flag string) (rede.Plan, error) {
	return rede.NewPlan(kind, d.redeServiceURL(kind, flag))
}

type redeQueryFlags struct {
	service string
	yes     bool
	dryRun  bool
}

func newRedeQueryCmd(deps Deps, kind rede.QueryKind) *cobra.Command {
	var f redeQueryFlags
	c := &cobra.Command{Use: "ip", Short: msg.RedeIPShort, Long: msg.RedeIPLong, Args: cobra.NoArgs}
	flagName, flagHelp := "servico-ip", msg.FlagRedeServiceIP
	if kind == rede.KindPortal {
		c.Use, c.Short, c.Long = "portal", msg.RedePortalShort, msg.RedePortalLong
		flagName, flagHelp = "servico-portal", msg.FlagRedeServicePortal
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		return runRedeQuery(cmd.Context(), cmd.OutOrStdout(), deps, kind, f)
	}
	c.Flags().StringVar(&f.service, flagName, "", flagHelp)
	c.Flags().BoolVarP(&f.yes, "yes", "y", false, msg.FlagRedeQueryYes)
	c.Flags().BoolVar(&f.dryRun, "dry-run", false, msg.FlagRedeQueryDry)
	return c
}

// runRedeQuery: URL -> plano -> o que será enviado -> (--dry-run para aqui) -> confirmação -> consulta. Nenhuma requisição
// antes da confirmação; sem URL ou com URL inválida, nenhuma requisição nunca.
func runRedeQuery(ctx context.Context, out io.Writer, deps Deps, kind rede.QueryKind, f redeQueryFlags) error {
	ctx = orBackground(ctx)
	lang := diagfmt.LangFromEnv(deps.UpdateCheck.env)
	plan, err := deps.redePlan(kind, f.service)
	switch {
	case errors.Is(err, rede.ErrNoService):
		_, _ = fmt.Fprintln(out, textsafe.Multiline(strings.Join(rede.NoServiceHelp(kind, lang), "\n")))
		return nil
	case err != nil:
		return errors.New(msg.ErrRedeBadService) // não repete a URL (pode ter usuário, senha ou token)
	}
	_, _ = fmt.Fprintln(out, textsafe.Multiline(strings.Join(plan.Describe(lang), "\n")))
	if f.dryRun {
		_, _ = fmt.Fprintln(out, msg.RedeQueryDryRun)
		return nil
	}
	if !f.yes {
		if !deps.interactive() {
			return errors.New(msg.RedeQueryNeedsYes)
		}
		_, _ = fmt.Fprint(out, msg.RedeQueryConfirm)
		answer, _ := bufio.NewReader(deps.stdin()).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "s", "sim", "y", "yes":
		default:
			_, _ = fmt.Fprintln(out, msg.RedeQueryCancelled)
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, redeTimeout)
	defer cancel()
	rep := &rede.Report{OS: deps.GOOS}
	if kind == rede.KindIP {
		rep = deps.redeCollect(ctx) // leitura local (sem internet), para cruzar portas e firewall com o IP
	}
	deps.redeQuery(ctx, plan, rep)
	_, _ = fmt.Fprint(out, textsafe.Multiline(rede.QuerySummary(rep, lang)))
	return nil
}

// redeQuery envia a consulta do PLANO já confirmado e põe o resultado no relatório. É o único ponto que usa o Fetcher.
func (d Deps) redeQuery(ctx context.Context, plan rede.Plan, rep *rede.Report) {
	f := d.redeFetcher()
	if plan.Kind == rede.KindPortal {
		res := rede.ProbePortal(ctx, f, plan.URL)
		rep.Portal = &res
		return
	}
	res := rede.QueryPublicIP(ctx, f, plan.URL)
	rep.PublicIP = &res
}
