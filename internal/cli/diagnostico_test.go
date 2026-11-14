package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagnostico"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

func diagReport() *diagnostico.Report {
	pct := 50
	return &diagnostico.Report{OS: "linux", Generated: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC),
		Hardware: diagnostico.Hardware{Vendor: "ACME", Model: "Note NOTEBOOK-DO-PAULO", CPU: "CPU X"},
		Battery:  &diagnostico.Battery{HealthPct: &pct},
		Events:   []diagnostico.Event{{Source: "kernel", Level: "erro", Count: 2, Message: `falha em /home/paulo/x e ip 203.0.113.7 serial: SN-SECRETO`}},
		Disks:    []diagnostico.Disk{{Name: "SSD", Health: diagnostico.HealthBad, HealthRaw: "FAILING"}}}
}

func diagDeps(t *testing.T, got *struct {
	goos   string
	runner exec.Runner
}) Deps {
	t.Helper()
	return Deps{GOOS: "linux", EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) },
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }},
		Diag: DiagDeps{
			Collect: func(_ context.Context, goos string, d diagnostico.Deps) *diagnostico.Report {
				if got != nil {
					got.goos, got.runner = goos, d.Runner
				}
				return diagReport()
			},
			Names: func() []string { return []string{"paulo", "NOTEBOOK-DO-PAULO"} },
			Now:   func() time.Time { return time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC) },
		}}
}

func runDiag(t *testing.T, deps Deps, args ...string) (out, errOut string, err error) {
	t.Helper()
	var o, e bytes.Buffer
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"diagnostico", "--no-update-check"}, args...))
	err = cmd.ExecuteContext(context.Background())
	return o.String(), e.String(), err
}

func TestDiagnosticoResumoPadrao(t *testing.T) {
	var got struct {
		goos   string
		runner exec.Runner
	}
	out, errOut, err := runDiag(t, diagDeps(t, &got))
	if err != nil {
		t.Fatal(err)
	}
	if got.goos != "linux" || got.runner == nil {
		t.Fatalf("o coletor recebe o SO e o Runner: %+v", got)
	}
	for _, want := range []string{"Diagnóstico da máquina (Linux)", "✗ problema: Disco com problema: SSD", "O que fazer: Faça backup AGORA", "! atenção: Bateria gasta", "toolkit diagnostico --completo"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "== Hardware ==") {
		t.Errorf("o resumo não traz o relatório completo:\n%s", out)
	}
	if !strings.Contains(errOut, "Lendo o estado da máquina") {
		t.Errorf("o andamento vai para stderr: %q", errOut)
	}
}

func TestDiagnosticoCompletoMascaraDadosSensiveis(t *testing.T) {
	out, _, err := runDiag(t, diagDeps(t, nil), "--completo")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"paulo", "PAULO", "203.0.113.7", "SN-SECRETO"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q sobrou:\n%s", bad, out)
		}
	}
	for _, want := range []string{"== Hardware ==", "== Discos ==", "[pessoal]", "203.x.x.x", "[usuário]", "[***]"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
}

func TestDiagnosticoJSON(t *testing.T) {
	out, errOut, err := runDiag(t, diagDeps(t, nil), "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut, "Lendo") {
		t.Errorf("--json não escreve andamento: %q", errOut)
	}
	var doc struct {
		OS       string `json:"os"`
		Findings []struct{ Area, Severity, Title string }
		Report   map[string]any
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, out)
	}
	if doc.OS != "linux" || len(doc.Findings) < 2 || doc.Findings[0].Severity != "problem" || doc.Report == nil {
		t.Errorf("%+v", doc)
	}
	for _, bad := range []string{"paulo", "PAULO", "203.0.113.7", "SN-SECRETO"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q sobrou no JSON:\n%s", bad, out)
		}
	}
}

func TestDiagnosticoRelatorioArquivoNovoMascaradoENaoSobrescreve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relatorio.txt")
	out, _, err := runDiag(t, diagDeps(t, nil), "--relatorio", path)
	if err != nil {
		t.Fatal(err)
	}
	// o caminho da confirmação sai MASCARADO (no runner do Windows o temp fica em C:\Users\<usuário>\..., que vira
	// [usuário]): a expectativa usa a mesma máscara, não o caminho cru. O relatório "FAILING" é a fonte FALSA injetada.
	if want := "Relatório completo salvo em " + diagnostico.MaskText(path, []string{"paulo", "NOTEBOOK-DO-PAULO"}); !strings.Contains(out, want) {
		t.Errorf("saída sem %q:\n%s", want, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "== Hardware ==") || !strings.Contains(string(data), "Diagnóstico da máquina") {
		t.Errorf("o arquivo traz resumo e relatório completo:\n%s", data)
	}
	for _, bad := range []string{"paulo", "PAULO", "203.0.113.7", "SN-SECRETO"} {
		if strings.Contains(string(data), bad) {
			t.Errorf("%q sobrou no arquivo:\n%s", bad, data)
		}
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("permissão %v, quer 0600", fi.Mode().Perm())
		}
	}
	// segunda vez: não sobrescreve
	before := string(data)
	if _, _, err := runDiag(t, diagDeps(t, nil), "--relatorio", path); err == nil || !strings.Contains(err.Error(), "não deu para gravar o relatório") {
		t.Fatalf("arquivo existente deve ser recusado: %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != before {
		t.Error("o arquivo existente foi alterado")
	}
}

// Sem o coletor injetado, um SO desconhecido NUNCA executa comando nenhum (o runner falso falharia o teste) e o
// diagnóstico explica que não leu nada.
func TestDiagnosticoSoSemSuporteNaoExecutaNada(t *testing.T) {
	deps := Deps{GOOS: "plan9", EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) },
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }}, Diag: DiagDeps{Names: func() []string { return nil }}}
	out, _, err := runDiag(t, deps)
	if err != nil || !strings.Contains(out, "sistema operacional sem suporte: plan9") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// Lupa #60 (1): o caminho pessoal do --relatorio sai MASCARADO na confirmação e nos erros (o PathError do Open repete o
// caminho). Pelo ExecuteWith: o que o usuário vê no stdout e no stderr.
func TestDiagnosticoCaminhoPessoalMascaradoNaConfirmacaoENosErros(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "paulo")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(path string) (code int, out, errOut string) {
		var o, e bytes.Buffer
		code = ExecuteWith(context.Background(), &o, &e, diagDeps(t, nil), []string{"diagnostico", "--no-update-check", "--relatorio", path})
		return code, o.String(), e.String()
	}
	// sucesso: a confirmação não traz o nome
	ok := filepath.Join(home, "rel.txt")
	code, out, errOut := run(ok)
	if code != 0 || strings.Contains(out, "paulo") || !strings.Contains(out, "Relatório completo salvo em ") || !strings.Contains(out, "[pessoal]") {
		t.Fatalf("confirmação sem o nome (code %d):\n%s\n%s", code, out, errOut)
	}
	// erro do Open (arquivo já existe: PathError com o caminho) e pasta inexistente
	for name, path := range map[string]string{"já existe": ok, "pasta inexistente": filepath.Join(base, "paulo", "nao-existe", "rel.txt")} {
		code, out, errOut = run(path)
		if code == 0 || !strings.Contains(errOut, "não deu para gravar o relatório") || strings.Contains(errOut, "paulo") || strings.Contains(out+errOut, "PAULO") {
			t.Errorf("%s: o erro não pode trazer o nome (code %d):\n%s", name, code, errOut)
		}
	}
}

// Lupa #60 (2): o --json mascara os VALORES já decodificados e só então serializa: chaves e tipos do contrato intactos
// mesmo com nomes pessoais como "report" e "Source", serial com aspas e barra, e caminhos do Windows escapados.
func TestDiagnosticoJSONPreservaChavesETiposEMascaraValores(t *testing.T) {
	deps := diagDeps(t, nil)
	deps.Diag.Names = func() []string { return []string{"report", "Source", "paulo"} }
	deps.Diag.Collect = func(context.Context, string, diagnostico.Deps) *diagnostico.Report {
		r := diagReport()
		r.Events = []diagnostico.Event{{Source: "report-service", Level: "erro", Count: 3,
			Message: `falha: serial: SN"12\34 em C:\Users\paulo\AppData\x.dll e \\SERVIDOR\pasta`}}
		r.Hardware.Model = "report Source Note"
		return r
	}
	out, _, err := runDiag(t, deps, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Report   map[string]any   `json:"report"`
		Findings []map[string]any `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("o JSON mascarado precisa continuar válido: %v\n%s", err, out)
	}
	if doc.Report == nil || len(doc.Findings) == 0 {
		t.Fatalf("chaves do contrato (report, findings) preservadas:\n%s", out)
	}
	evs, _ := doc.Report["Events"].([]any)
	if len(evs) != 1 {
		t.Fatalf("Events: %v", doc.Report["Events"])
	}
	ev, _ := evs[0].(map[string]any)
	for _, k := range []string{"When", "Source", "ID", "Level", "Message", "Count"} {
		if _, ok := ev[k]; !ok {
			t.Errorf("a chave %q do evento foi alterada ou sumiu: %v", k, ev)
		}
	}
	if c, _ := ev["Count"].(float64); c != 3 {
		t.Errorf("o tipo numérico de Count foi preservado: %v", ev["Count"])
	}
	for _, sentinel := range []string{`SN"12`, `12\34`, "SN\\\"12", "paulo", "SERVIDOR", "report-service", "report Source"} {
		if strings.Contains(out, sentinel) {
			t.Errorf("a sentinela %q sobrou no JSON:\n%s", sentinel, out)
		}
	}
	msg, _ := ev["Message"].(string)
	if !strings.Contains(msg, "serial: [***]") || !strings.Contains(msg, `C:\Users\[usuário]\AppData\x.dll`) {
		t.Errorf("valores decodificados mascarados: %q", msg)
	}
}

// Regressão do CI do Windows: o caminho do relatório numa pasta de usuário (C:\Users\NOME, /Users/NOME, /home/NOME)
// aparece mascarado na confirmação, em qualquer SO, e o arquivo é gravado no caminho REAL.
func TestDiagnosticoRelatorioEmPastaDeUsuarioConfirmaCaminhoMascarado(t *testing.T) {
	for _, home := range []string{"Users", "home"} {
		dir := filepath.Join(t.TempDir(), home, "ana")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "rel.txt")
		deps := diagDeps(t, nil)
		deps.Diag.Names = func() []string { return nil }
		out, _, err := runDiag(t, deps, "--relatorio", path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "ana") && strings.Contains(out, "salvo em") && strings.Contains(strings.SplitN(out, "salvo em ", 2)[1], "ana") {
			t.Errorf("%s: o nome na pasta pessoal sai mascarado: %s", home, out)
		}
		if !strings.Contains(out, "[usuário]") {
			t.Errorf("%s: esperava [usuário] no caminho: %s", home, out)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: o arquivo é gravado no caminho real: %v", home, err)
		}
	}
}
