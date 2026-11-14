package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/diagnostico"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/textsafe"
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
		_, err = fmt.Fprintln(out, jsonSafe(string(b)))
		return err
	}

	// O toolkit mostra as informações do PC como elas são (é uma ferramenta de análise): nenhum dado é escondido.
	// Nenhum campo do sistema chega ao terminal (nem ao arquivo) como sequência de controle: a limpeza é no limite da saída
	// textual e preserva os dados legíveis, LF e Tab (textsafe.Multiline).
	summary := textsafe.Multiline(diagnostico.Summary(rep, findings, lang))
	switch {
	case f.report != "":
		if err := writeReport(f.report, summary+"\n"+textsafe.Multiline(diagnostico.Full(rep, lang))); err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, summary)
		_, _ = fmt.Fprintf(out, "\n"+msg.DiagSaved+"\n", printable(f.report))
	case f.full:
		_, _ = fmt.Fprint(out, summary+"\n"+textsafe.Multiline(diagnostico.Full(rep, lang)))
	default:
		_, _ = fmt.Fprint(out, summary)
		_, _ = fmt.Fprintln(out, "\n"+msg.DiagHint)
	}
	return nil
}

// jsonSafe escapa, dentro das strings do JSON, os caracteres que o encoding/json deixa passar mas que um terminal pode
// tratar como controle: DEL (U+007F), C1 (U+0080 a U+009F), formatação/bidi/tags (categoria Cf, inclusive acima de U+FFFF) e
// os separadores de linha e de parágrafo (U+2028 e U+2029). Vira \uXXXX; uma runa acima de U+FFFF vira o PAR SUBSTITUTO
// UTF-16 (\ud8xx\udcxx), que decodifica para a mesma runa: o escape nunca altera o dado.
func jsonSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == 0x7f || (r >= 0x80 && r <= 0x9f) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029 {
			if r > 0xFFFF {
				hi, lo := utf16.EncodeRune(r)
				fmt.Fprintf(&b, `\u%04x\u%04x`, hi, lo)
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// writeReport grava o relatório com permissão 0600 e NUNCA sobrescreve um arquivo que já existe (O_EXCL): não esconde nada,
// só evita apagar um arquivo seu. Todo erro (Open, Write ou Close) sai limpo: o os.PathError repete o caminho, e o caminho
// pode ter controle no nome.
func writeReport(path, text string) (err error) {
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return cleanErr(fmt.Errorf(msg.ErrDiagReport, path, err))
	}
	defer func() {
		if cerr := fh.Close(); err == nil && cerr != nil {
			err = cleanErr(cerr)
		}
	}()
	if _, werr := fh.WriteString(text); werr != nil {
		return cleanErr(werr)
	}
	return nil
}

// cleanErr devolve um erro com a mensagem limpa para o terminal (controles escapados, quebras de linha preservadas).
func cleanErr(err error) error { return errors.New(textsafe.Multiline(err.Error())) }
