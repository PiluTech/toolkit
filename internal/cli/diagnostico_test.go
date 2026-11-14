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
			Now: func() time.Time { return time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC) },
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

// Decisão do Paulo: o toolkit mostra as informações do PC SEM máscara (é usado para análise). Nada de [usuário],
// série, UUID, MAC, e-mail, IP ou nome da máquina escondidos, em nenhum formato de saída.
var unmaskedSentinels = []string{"/home/paulo/x", "NOTEBOOK-DO-PAULO", "203.0.113.7", "serial: SN-SECRETO", "paulo@example.com", "4C4C4544-0042-5710-8039-B2C04F485932", "aa:bb:cc:dd:ee:ff"}

func unmaskedReport() *diagnostico.Report {
	r := diagReport()
	r.Hardware.Model = "Note NOTEBOOK-DO-PAULO"
	r.Events = []diagnostico.Event{{Source: "kernel", Level: "erro", Count: 2,
		Message: "falha em /home/paulo/x ip 203.0.113.7 serial: SN-SECRETO dono paulo@example.com uuid 4C4C4544-0042-5710-8039-B2C04F485932 mac aa:bb:cc:dd:ee:ff"}}
	return r
}

func unmaskedDeps(t *testing.T) Deps {
	deps := diagDeps(t, nil)
	deps.Diag.Collect = func(context.Context, string, diagnostico.Deps) *diagnostico.Report { return unmaskedReport() }
	return deps
}

func assertUnmasked(t *testing.T, where, out string) {
	t.Helper()
	for _, want := range unmaskedSentinels {
		if !strings.Contains(out, want) {
			t.Errorf("%s: %q foi escondido (o toolkit não mascara nada):\n%s", where, want, out)
		}
	}
	for _, bad := range []string{"[***]", "[pessoal]", "[usuário]", "[e-mail]", "[servidor]", ".x.x.x", ":x:x"} {
		if strings.Contains(out, bad) {
			t.Errorf("%s: sobrou máscara %q:\n%s", where, bad, out)
		}
	}
}

func TestDiagnosticoCompletoNaoMascaraNada(t *testing.T) {
	out, _, err := runDiag(t, unmaskedDeps(t), "--completo")
	if err != nil {
		t.Fatal(err)
	}
	assertUnmasked(t, "--completo", out)
	if !strings.Contains(out, "== Hardware ==") {
		t.Errorf("relatório completo:\n%s", out)
	}
}

func TestDiagnosticoJSONNaoMascaraENaoMexeNasChaves(t *testing.T) {
	out, errOut, err := runDiag(t, unmaskedDeps(t), "--json")
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
	// o JSON é válido, então as sentinelas aparecem DECODIFICADAS (sem escapes)
	var all strings.Builder
	ev, _ := doc.Report["Events"].([]any)
	for _, e := range ev {
		m, _ := e.(map[string]any)
		s, _ := m["Message"].(string)
		all.WriteString(s)
	}
	hw, _ := doc.Report["Hardware"].(map[string]any)
	all.WriteString(" " + hw["Model"].(string))
	assertUnmasked(t, "--json", all.String())
}

func TestDiagnosticoRelatorioArquivoNovoSemMascaraENaoSobrescreve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relatorio.txt")
	out, _, err := runDiag(t, unmaskedDeps(t), "--relatorio", path)
	if err != nil {
		t.Fatal(err)
	}
	// a confirmação mostra o caminho como ele é
	if !strings.Contains(out, "Relatório completo salvo em "+path) {
		t.Errorf("saída sem o caminho real %q:\n%s", path, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "== Hardware ==") || !strings.Contains(string(data), "Diagnóstico da máquina") {
		t.Errorf("o arquivo traz resumo e relatório completo:\n%s", data)
	}
	assertUnmasked(t, "arquivo", string(data))
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("permissão %v, quer 0600", fi.Mode().Perm())
		}
	}
	// segunda vez: não sobrescreve (O_EXCL), e o erro mostra o caminho real
	before := string(data)
	if _, _, err := runDiag(t, unmaskedDeps(t), "--relatorio", path); err == nil || !strings.Contains(err.Error(), "não deu para gravar o relatório") || !strings.Contains(err.Error(), path) {
		t.Fatalf("arquivo existente deve ser recusado, com o caminho real no erro: %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != before {
		t.Error("o arquivo existente foi alterado")
	}
}

// Sem o coletor injetado, um SO desconhecido NUNCA executa comando nenhum (o runner falso falharia o teste) e o
// diagnóstico explica que não leu nada.
func TestDiagnosticoSoSemSuporteNaoExecutaNada(t *testing.T) {
	deps := Deps{GOOS: "plan9", EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) },
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }}}
	out, _, err := runDiag(t, deps)
	if err != nil || !strings.Contains(out, "sistema operacional sem suporte: plan9") {
		t.Fatalf("%v\n%s", err, out)
	}
}
