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
	if !strings.Contains(out, "Relatório completo salvo em "+path) {
		t.Errorf("saída: %s", out)
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
