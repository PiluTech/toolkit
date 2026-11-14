package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/rede"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// RedeDeps são os pontos injetáveis do `toolkit rede` (os testes nunca tocam a máquina real).
type RedeDeps struct {
	// Collect lê a rede (nil = rede.Collect sobre o Runner de leitura e o disco reais).
	Collect func(ctx context.Context, goos string, d rede.Deps) *rede.Report
	Now     func() time.Time
	// IsAdmin diz se o processo roda como administrador (Windows) ou root (Unix). Nil = a mesma detecção do diagnóstico.
	IsAdmin func(ctx context.Context, goos string, r exec.Runner) bool
}

// redeTimeout é o prazo da leitura da rede (CLI e TUI).
const redeTimeout = 3 * time.Minute

func (d Deps) redeAdmin(ctx context.Context, r exec.Runner) bool {
	if d.Rede.IsAdmin != nil {
		return d.Rede.IsAdmin(ctx, d.GOOS, r)
	}
	return d.Diag.admin(ctx, d.GOOS, r)
}

// redeCollect lê a rede com as dependências injetadas (o mesmo caminho do CLI e da TUI).
func (d Deps) redeCollect(ctx context.Context) *rede.Report {
	collect := d.Rede.Collect
	if collect == nil {
		collect = rede.Collect
	}
	now := d.Rede.Now
	if now == nil {
		now = time.Now
	}
	rn := d.runner()
	return collect(ctx, d.GOOS, rede.Deps{Runner: rn, Now: now, Lang: d.diagLang(), Admin: d.redeAdmin(ctx, rn)})
}

type redeFlags struct {
	full, json bool
	report     string
}

func newRedeCmd(deps Deps) *cobra.Command {
	var f redeFlags
	c := &cobra.Command{
		Use:   "rede",
		Short: msg.RedeShort,
		Long:  msg.RedeLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRede(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, f)
		},
	}
	c.Flags().BoolVar(&f.full, "completo", false, msg.FlagRedeFull)
	c.Flags().BoolVar(&f.json, "json", false, msg.FlagRedeJSON)
	c.Flags().StringVar(&f.report, "relatorio", "", msg.FlagRedeReport)
	return c
}

// redeJSON é a forma de --json.
type redeJSON struct {
	OS        string            `json:"os"`
	Generated time.Time         `json:"generated"`
	Findings  []redeFindingJSON `json:"findings"`
	Report    *rede.Report      `json:"report"`
}

type redeFindingJSON struct {
	ID       string `json:"id"`
	Area     string `json:"area"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Action   string `json:"action,omitempty"`
}

func runRede(ctx context.Context, out, errOut io.Writer, deps Deps, f redeFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, redeTimeout)
	defer cancel()
	lang := diagfmt.LangFromEnv(deps.UpdateCheck.env)
	if !f.json {
		_, _ = fmt.Fprintln(errOut, msg.RedeReading)
	}
	rep := deps.redeCollect(ctx)
	findings := rede.Analyze(rep, lang)
	if f.json {
		// o aviso vai para o stderr, antes: o stdout continua sendo só o JSON
		_, _ = fmt.Fprintln(errOut, msg.RedePrivacy)
		doc := redeJSON{OS: rep.OS, Generated: rep.Generated, Report: rep}
		for _, x := range findings {
			doc.Findings = append(doc.Findings, redeFindingJSON{ID: x.ID, Area: x.Area, Severity: x.Severity.String(), Title: x.Title, Detail: x.Detail, Action: x.Action})
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, jsonSafe(string(b)))
		return err
	}

	// IPs, MACs e nomes de rede aparecem como o sistema os informa (é uma ferramenta de análise). Nenhum campo do sistema
	// chega ao terminal (nem ao arquivo) como sequência de controle: a limpeza é no limite da saída textual (textsafe).
	summary := textsafe.Multiline(rede.Summary(rep, findings, lang))
	switch {
	case f.report != "":
		_, _ = fmt.Fprintln(errOut, msg.RedePrivacy)
		if err := writeReport(f.report, summary+"\n"+textsafe.Multiline(rede.Full(rep, lang))); err != nil {
			return err // writeReport já devolve o erro limpo e com o caminho
		}
		_, _ = fmt.Fprint(out, summary)
		_, _ = fmt.Fprintf(out, "\n"+msg.RedeSaved+"\n", printable(f.report))
	case f.full:
		_, _ = fmt.Fprint(out, summary+"\n"+textsafe.Multiline(rede.Full(rep, lang)))
	default:
		_, _ = fmt.Fprint(out, summary)
		_, _ = fmt.Fprintln(out, "\n"+msg.RedeHint)
	}
	return nil
}
