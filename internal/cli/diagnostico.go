package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
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
	// Names são os nomes pessoais a mascarar no relatório: usuário e nome da máquina (nil = os reais).
	Names func() []string
	Now   func() time.Time
}

func (d DiagDeps) names() []string {
	if d.Names != nil {
		return d.Names()
	}
	var out []string
	if u, err := user.Current(); err == nil {
		out = append(out, u.Username, u.Name)
	}
	if h, err := os.Hostname(); err == nil {
		out = append(out, h)
	}
	for _, k := range []string{"USER", "USERNAME", "COMPUTERNAME", "HOSTNAME", "LOGNAME"} {
		out = append(out, os.Getenv(k))
	}
	return out
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
	names := deps.Diag.names()
	mask := func(s string) string { return diagnostico.MaskText(s, names) }

	if f.json {
		doc := diagJSON{OS: rep.OS, Generated: rep.Generated, Report: rep}
		for _, x := range findings {
			doc.Findings = append(doc.Findings, diagFindingJSON{Area: x.Area, Severity: severityName(x.Severity), Title: x.Title, Detail: x.Detail, Action: x.Action})
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, mask(string(b)))
		return err
	}

	summary := diagnostico.Summary(rep, findings, lang)
	switch {
	case f.report != "":
		full := summary + "\n" + diagnostico.Full(rep, lang)
		if err := writeReport(f.report, mask(full)); err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, mask(summary))
		_, _ = fmt.Fprintf(out, "\n"+msg.DiagSaved+"\n", printable(f.report))
	case f.full:
		_, _ = fmt.Fprint(out, mask(summary+"\n"+diagnostico.Full(rep, lang)))
	default:
		_, _ = fmt.Fprint(out, mask(summary))
		_, _ = fmt.Fprintln(out, "\n"+msg.DiagHint)
	}
	return nil
}

// writeReport grava o relatório (já mascarado) com permissão 0600, e NUNCA sobrescreve um arquivo que já existe.
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
