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
	diagfmt.Register("t-fixo", func(string, diagfmt.Lang) (string, error) { return "Resumo fixo", nil })
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
	for _, want := range []string{"Resumo: 8589934592 bytes", "-- Detalhes (saída original, sem os identificadores da máquina) --", "8589934592 bytes\n"} {
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
	if !strings.Contains(sr.Output, "-- Details (original output, without the machine identifiers) --") {
		t.Errorf("cabeçalho dos detalhes em inglês: %q", sr.Output)
	}
}

// Lupa #56: sem --detalhes a saída crua NUNCA aparece (nem no fallback do renderizador que falha); com --detalhes
// aparece sem os identificadores da máquina.
func TestEngineRenderQueFalhaNaoMostraACruaSemDetalhes(t *testing.T) {
	src := strings.Replace(diagRender, "t-resumo", "t-erro", 1)
	for name, raw := range map[string]string{
		"json válido":   `{"disks":[{"_name":"SSD","device_serial":"SN-SECRETO-1","volume_uuid":"4C4C4544-0042-5710-8039-B2C04F485932"}]}`,
		"json truncado": `{"disks":[{"_name":"SSD","device_serial":"SN-SECRETO-1`,
		"texto":         "Model: X\nSerial Number: SN-SECRETO-1\n",
	} {
		for _, details := range []bool{false, true} {
			e := newEnv(t, linuxInfo())
			e.eng.Getenv, e.eng.Details = envMap(nil), details
			e.run.On(sh("le-memoria"), exectest.Exit(0, raw))
			rep, err := e.eng.Run(context.Background(), e.recipe(src), RunOptions{})
			if err != nil {
				t.Fatal(err)
			}
			out := rep.Steps[0].Output
			if rep.Steps[0].Outcome != OutcomeDone || !strings.Contains(out, "não deu para resumir") || !strings.Contains(out, "--detalhes") {
				t.Errorf("%s detalhes=%v: aviso ausente (a etapa continua ok):\n%s", name, details, out)
			}
			if strings.Contains(out, "SN-SECRETO-1") || strings.Contains(out, "4C4C4544") {
				t.Errorf("%s detalhes=%v: identificador na saída:\n%s", name, details, out)
			}
			if !details && (strings.Contains(out, "_name") || strings.Contains(out, "Model: X")) {
				t.Errorf("%s: sem --detalhes a saída crua não aparece:\n%s", name, out)
			}
			if details && !strings.Contains(out, "[***]") {
				t.Errorf("%s: com --detalhes a original aparece redigida:\n%s", name, out)
			}
		}
	}
}

// Com resumo ok, --detalhes mostra a original redigida; a falha do comando também não vaza identificador.
func TestEngineDetalhesEFalhaDoComandoRedigemIdentificadores(t *testing.T) {
	raw := `{"x":[{"device_serial":"SN-SECRETO-1"}]} 8589934592 bytes`
	e0 := newEnv(t, linuxInfo())
	e0.eng.Details, e0.eng.Getenv = true, envMap(nil)
	e0.run.On(sh("le-memoria"), exectest.Exit(0, raw))
	rep0, err0 := e0.eng.Run(context.Background(), e0.recipe(strings.Replace(diagRender, "t-resumo", "t-fixo", 1)), RunOptions{})
	if err0 != nil {
		t.Fatal(err0)
	}
	sr := rep0.Steps[0]
	if strings.Contains(sr.Output, "SN-SECRETO-1") || !strings.Contains(sr.Output, "8589934592 bytes") || !strings.Contains(sr.Output, "[***]") {
		t.Errorf("detalhes redigidos:\n%s", sr.Output)
	}
	e := newEnv(t, linuxInfo())
	e.run.On(sh("le-memoria"), exectest.Exit(3, `{"device_serial":"SN-SECRETO-1"} falhou`))
	rep, err := e.eng.Run(context.Background(), e.recipe(strings.Replace(diagRender, "t-resumo", "t-fixo", 1)), RunOptions{})
	if err == nil || rep.Steps[0].Outcome != OutcomeFailed || strings.Contains(rep.Steps[0].Output, "SN-SECRETO-1") {
		t.Errorf("a saída de uma falha não pode ter identificador: %v\n%+v", err, rep.Steps[0])
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
