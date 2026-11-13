package recipes

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

func init() {
	diagfmt.Register("t-resumo", func(raw string, l diagfmt.Lang) (string, error) {
		return diagfmt.T(l, "Resumo: ", "Summary: ", "Resumen: ") + strings.TrimSpace(raw), nil
	})
	diagfmt.Register("t-erro", func(string, diagfmt.Lang) (string, error) { return "", errors.New("formato inesperado") })
}

const diagRender = `id: diag
name: Diag
os: linux
kind: diagnostic
steps:
  - id: mem
    name: Memória
    run: { sh: le-memoria }
    render: t-resumo
`

func TestRenderDeEtapaSoNoDiagnosticoComRunENomeConhecido(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"desconhecido":  {"id: d\nname: D\nos: linux\nkind: diagnostic\nsteps:\n  - id: a\n    run: { sh: x }\n    render: nao-existe\n", "render \"nao-existe\" desconhecido"},
		"receita setup": {"id: d\nname: D\nos: linux\nsteps:\n  - id: a\n    run: { sh: x }\n    render: t-resumo\n", "só vale em etapa \"run\" de receita de diagnóstico"},
		"sem run":       {"id: d\nname: D\nos: linux\nkind: diagnostic\nsteps:\n  - id: a\n    install: [git]\n    render: t-resumo\n", "não instala apps"},
	}
	for name, c := range cases {
		if _, err := Parse("d.yaml", []byte(c.src)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: erro %v, quer %q", name, err, c.want)
		}
	}
	r, err := Parse("diag.yaml", []byte(diagRender))
	if err != nil || r.Steps[0].Render != "t-resumo" {
		t.Fatalf("receita válida: %v %+v", err, r)
	}
}

func runRender(t *testing.T, details bool, getenv func(string) string, out exectest.Reply) StepReport {
	t.Helper()
	e := newEnv(t, linuxInfo())
	e.eng.Details, e.eng.Getenv = details, getenv
	e.run.On(sh("le-memoria"), out)
	rep, err := e.eng.Run(context.Background(), e.recipe(diagRender), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return rep.Steps[0]
}

func envMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestEngineMostraSoOResumoAteHaverDetalhes(t *testing.T) {
	sr := runRender(t, false, envMap(nil), exectest.Exit(0, "8589934592 bytes\n"))
	if sr.Output != "Resumo: 8589934592 bytes" || sr.Outcome != OutcomeDone {
		t.Fatalf("resumo: %q (%s)", sr.Output, sr.Outcome)
	}
	sr = runRender(t, true, envMap(nil), exectest.Exit(0, "8589934592 bytes\n"))
	for _, want := range []string{"Resumo: 8589934592 bytes", "-- Detalhes (saída original) --", "8589934592 bytes\n"} {
		if !strings.Contains(sr.Output, want) {
			t.Errorf("detalhes sem %q:\n%s", want, sr.Output)
		}
	}
	if strings.Index(sr.Output, "Resumo") > strings.Index(sr.Output, "Detalhes") {
		t.Errorf("o resumo vem antes dos detalhes:\n%s", sr.Output)
	}
}

func TestEngineResumoNosTresIdiomas(t *testing.T) {
	for env, want := range map[string]string{"en_US.UTF-8": "Summary: x", "es_ES.UTF-8": "Resumen: x", "pt_BR.UTF-8": "Resumo: x", "": "Resumo: x"} {
		sr := runRender(t, false, envMap(map[string]string{"LANG": env}), exectest.Exit(0, "x"))
		if sr.Output != want {
			t.Errorf("LANG=%q: %q, quer %q", env, sr.Output, want)
		}
	}
	sr := runRender(t, true, envMap(map[string]string{"TOOLKIT_LANG": "en"}), exectest.Exit(0, "x"))
	if !strings.Contains(sr.Output, "-- Details (original output) --") {
		t.Errorf("cabeçalho dos detalhes em inglês: %q", sr.Output)
	}
}

func TestEngineRenderQueFalhaMostraAOriginalComAviso(t *testing.T) {
	e := newEnv(t, linuxInfo())
	e.eng.Getenv = envMap(nil)
	src := strings.Replace(diagRender, "t-resumo", "t-erro", 1)
	e.run.On(sh("le-memoria"), exectest.Exit(0, "saida crua\n"))
	rep, err := e.eng.Run(context.Background(), e.recipe(src), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	out := rep.Steps[0].Output
	if rep.Steps[0].Outcome != OutcomeDone || !strings.Contains(out, "não deu para resumir") || !strings.Contains(out, "saida crua") {
		t.Errorf("render que falha deve mostrar a original com aviso, e a etapa continua ok:\n%s", out)
	}
}

func TestEngineRenderNaoRodaQuandoAEtapaFalha(t *testing.T) {
	e := newEnv(t, linuxInfo())
	e.run.On(sh("le-memoria"), exectest.Exit(2, "erro do comando"))
	rep, err := e.eng.Run(context.Background(), e.recipe(diagRender), RunOptions{})
	if err == nil || rep.Steps[0].Outcome != OutcomeFailed || strings.Contains(rep.Steps[0].Output, "Resumo") {
		t.Fatalf("etapa que falha não é resumida: %v %+v", err, rep.Steps[0])
	}
}
