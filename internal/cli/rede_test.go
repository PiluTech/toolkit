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

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/rede"
)

// netReportCLI é uma leitura SINTÉTICA (documentação: 192.0.2.x, 203.0.113.x, 2001:db8::, REDE-FAKE).
func netReportCLI() *rede.Report {
	return &rede.Report{OS: "linux", Generated: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC),
		Interfaces: []rede.Interface{{Name: "wlan0", Kind: "wifi", MAC: "02:00:00:00:00:01", IPv4: []rede.Addr{{IP: "192.0.2.10", PrefixLen: 24}}}},
		Gateways:   []string{"192.0.2.1"}, GatewaysRead: true,
		WiFi:      &rede.WiFi{Interface: "wlan0", SSID: "REDE-FAKE", Security: rede.SecOpen, Raw: "--"},
		DNS:       rede.DNS{Servers: []rede.DNSServer{{Addr: "10.0.0.1"}}, Encrypted: rede.No},
		Listeners: []rede.Listener{{Proto: "tcp", Addr: "0.0.0.0", Port: 23, Process: "telnetd", PID: 7}}, ListenersRead: true,
		Firewalls: []rede.Firewall{{Name: "ufw", Enabled: rede.No}},
		Skipped:   []rede.Skipped{{Area: "processos-das-portas", Reason: "exige root", NeedsAdmin: true}}}
}

func redeDeps(t *testing.T, got *rede.Deps, rep *rede.Report) Deps {
	t.Helper()
	if rep == nil {
		rep = netReportCLI()
	}
	return Deps{GOOS: "linux", EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) },
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }},
		Rede: RedeDeps{
			Collect: func(_ context.Context, _ string, d rede.Deps) *rede.Report {
				if got != nil {
					*got = d
				}
				return rep
			},
			Now:     func() time.Time { return time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC) },
			IsAdmin: func(context.Context, string, exec.Runner) bool { return false },
		}}
}

func runRedeCmd(t *testing.T, deps Deps, args ...string) (out, errOut string, err error) {
	t.Helper()
	var o, e bytes.Buffer
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"rede", "--no-update-check"}, args...))
	err = cmd.ExecuteContext(context.Background())
	return o.String(), e.String(), err
}

func TestRedeResumoPadrao(t *testing.T) {
	var got rede.Deps
	out, errOut, err := runRedeCmd(t, redeDeps(t, &got, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.Runner == nil || got.Admin {
		t.Fatalf("o coletor recebe o Runner e o estado de administrador: %+v", got)
	}
	for _, want := range []string{"Análise de rede (Linux)", "✗ alta: Rede Wi-Fi aberta", "REDE-FAKE", "✗ alta: Firewall desligado", "Porta 23 (Telnet)", "IP público: não consultado", "O que fazer:", "toolkit rede --completo", "Com administrador a análise também lê"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "== Interfaces ==") {
		t.Errorf("o resumo não traz o relatório completo:\n%s", out)
	}
	if !strings.Contains(errOut, "Lendo a rede") || !strings.Contains(errOut, "sem varredura") {
		t.Errorf("o andamento vai para stderr: %q", errOut)
	}
}

func TestRedePassaOAdminAoColetor(t *testing.T) {
	var got rede.Deps
	deps := redeDeps(t, &got, nil)
	deps.Rede.IsAdmin = func(context.Context, string, exec.Runner) bool { return true }
	if _, _, err := runRedeCmd(t, deps); err != nil || !got.Admin {
		t.Fatalf("admin: %v %+v", err, got)
	}
	// sem IsAdmin da rede, usa a detecção do diagnóstico
	deps = redeDeps(t, &got, nil)
	deps.Rede.IsAdmin = nil
	deps.Diag.IsAdmin = func(context.Context, string, exec.Runner) bool { return true }
	if _, _, err := runRedeCmd(t, deps); err != nil || !got.Admin {
		t.Fatalf("fallback: %v %+v", err, got)
	}
}

func TestRedeCompletoNaoMascaraNada(t *testing.T) {
	out, _, err := runRedeCmd(t, redeDeps(t, nil, nil), "--completo")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"== Interfaces ==", "02:00:00:00:00:01", "192.0.2.10/24", "SSID: REDE-FAKE", "tcp 0.0.0.0:23  telnetd (pid 7)", "== Firewall ==", "Contém dados da rede local"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"[***]", ".x.x.x", ":x:x", "[servidor]"} {
		if strings.Contains(out, bad) {
			t.Errorf("sobrou máscara %q", bad)
		}
	}
}

func TestRedeJSONValidoComAvisoNoStderr(t *testing.T) {
	out, errOut, err := runRedeCmd(t, redeDeps(t, nil, nil), "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut, "Lendo") {
		t.Errorf("--json não escreve andamento: %q", errOut)
	}
	if !strings.Contains(errOut, "dados da rede local") || strings.Contains(out, "dados da rede local") {
		t.Errorf("o aviso de privacidade vai para o stderr (o stdout é só JSON): stderr=%q", errOut)
	}
	var doc struct {
		OS       string `json:"os"`
		Findings []struct{ ID, Area, Severity, Title string }
		Report   map[string]any
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, out)
	}
	if doc.OS != "linux" || doc.Report == nil || len(doc.Findings) < 3 {
		t.Fatalf("%+v", doc)
	}
	sev := map[string]string{}
	for _, f := range doc.Findings {
		sev[f.ID] = f.Severity
	}
	if sev["wifi-aberto"] != "high" || sev["firewall-desligado"] != "high" || sev["ip-publico"] != "info" {
		t.Errorf("severidades: %v", sev)
	}
	wifi, _ := doc.Report["WiFi"].(map[string]any)
	if wifi["Security"] != "open" || wifi["SSID"] != "REDE-FAKE" {
		t.Errorf("tipos com nome estável: %v", wifi)
	}
	fw, _ := doc.Report["Firewalls"].([]any)
	if len(fw) != 1 || fw[0].(map[string]any)["Enabled"] != "no" {
		t.Errorf("Tri com nome estável: %v", fw)
	}
}

func TestRedeRelatorioArquivoNovoComAvisoNaoSobrescreve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rede.txt")
	out, errOut, err := runRedeCmd(t, redeDeps(t, nil, nil), "--relatorio", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "dados da rede local") {
		t.Errorf("aviso de privacidade antes de gravar: %q", errOut)
	}
	if !strings.Contains(out, "Relatório completo salvo em "+path) {
		t.Errorf("saída sem o caminho:\n%s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "== Interfaces ==") || !strings.Contains(string(data), "Análise de rede") || !strings.Contains(string(data), "REDE-FAKE") {
		t.Errorf("arquivo sem resumo e relatório completo:\n%s", data)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("permissão %v, quer 0600", fi.Mode().Perm())
		}
	}
	before := string(data)
	if _, _, err := runRedeCmd(t, redeDeps(t, nil, nil), "--relatorio", path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("arquivo existente deve ser recusado: %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != before {
		t.Error("o arquivo existente foi alterado")
	}
}

func hostileNetReport() *rede.Report {
	r := netReportCLI()
	r.WiFi.SSID = "A" + escCSI + "B\u202eC" + escOSC
	r.Listeners[0].Process = "x" + escC1 + escOSC
	r.Interfaces[0].Name = "wlan" + escCSI
	return r
}

func TestRedeSaidaSemSequenciasDeEscape(t *testing.T) {
	for name, args := range map[string][]string{"resumo": nil, "--completo": {"--completo"}} {
		out, _, err := runRedeCmd(t, redeDeps(t, nil, hostileNetReport()), args...)
		if err != nil {
			t.Fatal(err)
		}
		noRawControls(t, name, out)
		if strings.ContainsRune(out, '\u202e') {
			t.Errorf("%s: o bidi U+202E chegou à saída", name)
		}
	}
	// o arquivo também
	path := filepath.Join(t.TempDir(), "r.txt")
	if _, _, err := runRedeCmd(t, redeDeps(t, nil, hostileNetReport()), "--relatorio", path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	noRawControls(t, "arquivo", string(data))
	// o JSON escapa o que o encoding/json deixa passar, sem alterar o dado
	out, _, err := runRedeCmd(t, redeDeps(t, nil, hostileNetReport()), "--json")
	if err != nil {
		t.Fatal(err)
	}
	noRawControls(t, "json", out)
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("JSON inválido: %v", err)
	}
}

// Sem o coletor injetado, um SO desconhecido NUNCA executa comando nenhum (o runner falso falharia o teste).
func TestRedeSoSemSuporteNaoExecutaNada(t *testing.T) {
	deps := Deps{GOOS: "plan9", EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) },
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }},
		Rede:        RedeDeps{IsAdmin: func(context.Context, string, exec.Runner) bool { return false }}}
	out, _, err := runRedeCmd(t, deps)
	if err != nil || !strings.Contains(out, "Nada da rede pôde ser lido") || strings.Contains(out, "nada de ruim encontrado") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestRedeNaoAceitaArgumentosEAjudaDizOQueNaoFaz(t *testing.T) {
	if _, _, err := runRedeCmd(t, redeDeps(t, nil, nil), "192.0.2.1"); err == nil {
		t.Error("o comando não recebe alvo: nenhum host digitado vira comando")
	}
	var o, e bytes.Buffer
	cmd := NewRootCmdWith(&o, &e, redeDeps(t, nil, nil))
	cmd.SetArgs([]string{"rede", "--help"})
	_ = cmd.ExecuteContext(context.Background())
	for _, w := range []string{"SEM alterar nada", "NÃO faz varredura", "NÃO acessa a internet", "senha do Wi-Fi", "--completo", "--json", "--relatorio"} {
		if !strings.Contains(o.String(), w) {
			t.Errorf("a ajuda deve dizer %q:\n%s", w, o.String())
		}
	}
}

// A TUI usa o MESMO coletor: o backend devolve o relatório do coletor injetado.
func TestRedeBackendDaTUIUsaOMesmoColetor(t *testing.T) {
	var got rede.Deps
	b := &redeBackend{deps: redeDeps(t, &got, nil)}
	rep, err := b.Load(context.Background())
	if err != nil || rep == nil || rep.WiFi == nil || got.Runner == nil {
		t.Fatalf("%v %+v %+v", err, rep, got)
	}
}
