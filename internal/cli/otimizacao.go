package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/animacoes"
	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/otimizacao"
	"github.com/PiluTech/toolkit/internal/privacidade"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// OtimDeps são os pontos injetáveis do `toolkit otimizacao` (os testes nunca tocam a máquina real).
type OtimDeps struct {
	// Collect lê os gargalos (nil = otimizacao.Collect sobre o Runner de leitura).
	Collect func(ctx context.Context, goos string, d otimizacao.Deps) *otimizacao.Report
	Now     func() time.Time
	// IsAdmin diz se o terminal é de administrador (nil = a mesma detecção do diagnóstico). Só muda a mensagem de uma leitura
	// que o Windows negou: nada é elevado.
	IsAdmin func(ctx context.Context, goos string, r exec.Runner) bool
	// AnimSystem é o registro do usuário das animações (nil = animacoes.NewWindowsSystem sobre o Runner do Deps).
	AnimSystem animacoes.System
	// AnimStore guarda o backup das animações (nil = arquivo por usuário na pasta de estado do usuário).
	AnimStore privacidade.Store
}

// otimTimeout é o prazo da leitura (comando e TUI).
const otimTimeout = 3 * time.Minute

func (d OtimDeps) collect(ctx context.Context, deps Deps) *otimizacao.Report {
	ctx, cancel := context.WithTimeout(ctx, otimTimeout)
	defer cancel()
	collect := d.Collect
	if collect == nil {
		collect = otimizacao.Collect
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	rn := deps.runner()
	admin := false
	if deps.GOOS == "windows" { // fora do Windows nada é lido nem detectado
		if d.IsAdmin != nil {
			admin = d.IsAdmin(ctx, deps.GOOS, rn)
		} else {
			admin = deps.Diag.admin(ctx, deps.GOOS, rn)
		}
	}
	return collect(ctx, deps.GOOS, otimizacao.Deps{Runner: rn, Now: now, Lang: deps.diagLang(), Admin: admin})
}

func newOtimizacaoCmd(deps Deps) *cobra.Command {
	var f diagFlags
	c := &cobra.Command{
		Use:   "otimizacao",
		Short: msg.OtimShort,
		Long:  msg.OtimLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOtimizacao(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, f)
		},
	}
	c.Flags().BoolVar(&f.full, "completo", false, msg.FlagOtimFull)
	c.Flags().BoolVar(&f.json, "json", false, msg.FlagOtimJSON)
	c.Flags().StringVar(&f.report, "relatorio", "", msg.FlagOtimReport)
	c.AddCommand(newAnimacoesCmd(deps))
	return c
}

// otimJSON é a forma de --json.
type otimJSON struct {
	OS        string             `json:"os"`
	Generated time.Time          `json:"generated"`
	Findings  []diagFindingJSON  `json:"findings"`
	Report    *otimizacao.Report `json:"report"`
}

func runOtimizacao(ctx context.Context, out, errOut io.Writer, deps Deps, f diagFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// fora do Windows termina antes de ler ou gravar qualquer coisa (como a privacidade)
	if deps.GOOS != "windows" {
		return fmt.Errorf(msg.OtimOnlyWindows, printable(deps.GOOS))
	}
	lang := diagfmt.LangFromEnv(deps.UpdateCheck.env)
	if !f.json {
		_, _ = fmt.Fprintln(errOut, msg.OtimReading)
	}
	rep := deps.Otim.collect(ctx, deps)
	findings := otimizacao.Analyze(rep, lang)
	if f.json {
		doc := otimJSON{OS: rep.OS, Generated: rep.Generated, Report: rep, Findings: []diagFindingJSON{}}
		for _, x := range findings {
			doc.Findings = append(doc.Findings, diagFindingJSON{Area: x.Area, Severity: severityName(x.Severity), Title: x.Title, Detail: x.Detail, Action: x.Action})
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, jsonSafe(string(b)))
		return err
	}
	// Nome de processo, de serviço e de item de inicialização vêm do sistema: a limpeza é no limite da saída textual.
	summary := textsafe.Multiline(otimizacao.Summary(rep, findings, lang))
	switch {
	case f.report != "":
		if err := writeReport(f.report, summary+"\n"+textsafe.Multiline(otimizacao.Full(rep, lang))); err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, summary)
		_, _ = fmt.Fprintf(out, "\n"+msg.DiagSaved+"\n", printable(f.report))
	case f.full:
		_, _ = fmt.Fprint(out, summary+"\n"+textsafe.Multiline(otimizacao.Full(rep, lang)))
	default:
		_, _ = fmt.Fprint(out, summary)
		_, _ = fmt.Fprintln(out, "\n"+msg.OtimHint)
	}
	return nil
}
