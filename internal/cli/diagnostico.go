package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/diagnostico"
	"github.com/PiluTech/toolkit/internal/msg"
)

// DiagDeps são os pontos injetáveis do `toolkit diagnostico` (os testes nunca tocam a máquina real).
type DiagDeps struct {
	// Collect lê a máquina (nil = diagnostico.Collect sobre o Runner de leitura e o disco reais).
	Collect func(ctx context.Context, goos string, d diagnostico.Deps) *diagnostico.Report
	Now     func() time.Time
}

type diagFlags struct {
	full, json bool
	report     string
}

func newDiagnosticoCmd(deps Deps) *cobra.Command {
	var f diagFlags
	c := &cobra.Command{
		Use:   "diagnostico",
		Short: msg.DiagShort,
		Long:  msg.DiagLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiagnostico(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, f)
		},
	}
	c.Flags().BoolVar(&f.full, "completo", false, msg.FlagDiagFull)
	c.Flags().BoolVar(&f.json, "json", false, msg.FlagDiagJSON)
	c.Flags().StringVar(&f.report, "relatorio", "", msg.FlagDiagReport)
	return c
}

// diagJSON é a forma de --json.
type diagJSON struct {
	OS        string              `json:"os"`
	Generated time.Time           `json:"generated"`
	Findings  []diagFindingJSON   `json:"findings"`
	Report    *diagnostico.Report `json:"report"`
}

type diagFindingJSON struct {
	Area     string `json:"area"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Action   string `json:"action,omitempty"`
}

func severityName(s diagnostico.Severity) string {
	switch s {
	case diagnostico.SevBad:
		return "problem"
	case diagnostico.SevWarn:
		return "attention"
	case diagnostico.SevInfo:
		return "info"
	}
	return "ok"
}

func runDiagnostico(ctx context.Context, out, errOut io.Writer, deps Deps, f diagFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	lang := diagfmt.LangFromEnv(deps.UpdateCheck.env)
	if !f.json {
		_, _ = fmt.Fprintln(errOut, msg.DiagReading)
	}
	collect := deps.Diag.Collect
	if collect == nil {
		collect = diagnostico.Collect
	}
	now := deps.Diag.Now
	if now == nil {
		now = time.Now
	}
	rep := collect(ctx, deps.GOOS, diagnostico.Deps{Runner: deps.runner(), Now: now, Lang: lang})
	findings := diagnostico.Analyze(rep, lang)
	if f.json {
		doc := diagJSON{OS: rep.OS, Generated: rep.Generated, Report: rep}
		for _, x := range findings {
			doc.Findings = append(doc.Findings, diagFindingJSON{Area: x.Area, Severity: severityName(x.Severity), Title: x.Title, Detail: x.Detail, Action: x.Action})
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(b))
		return err
	}

	// O toolkit mostra as informações do PC como elas são (é uma ferramenta de análise): nenhum dado é escondido.
	summary := diagnostico.Summary(rep, findings, lang)
	switch {
	case f.report != "":
		if err := writeReport(f.report, summary+"\n"+diagnostico.Full(rep, lang)); err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, summary)
		_, _ = fmt.Fprintf(out, "\n"+msg.DiagSaved+"\n", printable(f.report))
	case f.full:
		_, _ = fmt.Fprint(out, summary+"\n"+diagnostico.Full(rep, lang))
	default:
		_, _ = fmt.Fprint(out, summary)
		_, _ = fmt.Fprintln(out, "\n"+msg.DiagHint)
	}
	return nil
}

// writeReport grava o relatório com permissão 0600 e NUNCA sobrescreve um arquivo que já existe (O_EXCL): não esconde nada,
// só evita apagar um arquivo seu.
func writeReport(path, text string) (err error) {
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf(msg.ErrDiagReport, printable(path), err)
	}
	defer func() {
		if cerr := fh.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = fh.WriteString(text)
	return err
}
