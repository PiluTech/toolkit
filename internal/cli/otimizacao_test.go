package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/otimizacao"
)

func otimReport() *otimizacao.Report {
	deg := true
	return &otimizacao.Report{OS: "windows", Supported: true, Generated: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC),
		CPU:     &otimizacao.CPU{Samples: 3, AvgPct: 91, PeakPct: 99, Top: []otimizacao.Process{{Name: "chrome", PID: 7, CPUPct: 50}}},
		Disk:    &otimizacao.Disk{Drive: "C", Medium: "HDD", SizeBytes: 1000},
		Startup: &otimizacao.Startup{Total: 14, Items: []otimizacao.StartupItem{{Name: "AppUm", Command: "um.exe"}}},
		Boot:    &otimizacao.Boot{Found: true, DurationMs: 99000, Degraded: &deg},
		Skipped: []otimizacao.Skipped{{Area: "servicos", Reason: "x"}}}
}

type otimGot struct {
	goos   string
	runner exec.Runner
	deps   otimizacao.Deps
	calls  int
}

func otimDeps(t *testing.T, goos string, rep *otimizacao.Report, got *otimGot) Deps {
	t.Helper()
	return Deps{GOOS: goos, EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) },
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }},
		Otim: OtimDeps{
			Collect: func(_ context.Context, g string, d otimizacao.Deps) *otimizacao.Report {
				if got != nil {
					got.goos, got.runner, got.deps = g, d.Runner, d
					got.calls++
				}
				return rep
			},
			IsAdmin: func(context.Context, string, exec.Runner) bool { return true },
			Now:     func() time.Time { return time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC) },
		}}
}

func runOtim(t *testing.T, deps Deps, args ...string) (out, errOut string, err error) {
	t.Helper()
	var o, e bytes.Buffer
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"otimizacao", "--no-update-check"}, args...))
	err = cmd.ExecuteContext(context.Background())
	return o.String(), e.String(), err
}

func TestOtimizacaoResumoPadrao(t *testing.T) {
	var got otimGot
	out, errOut, err := runOtim(t, otimDeps(t, "windows", otimReport(), &got))
	if err != nil {
		t.Fatal(err)
	}
	if got.goos != "windows" || got.runner == nil || !got.deps.Admin || got.deps.Now == nil {
		t.Fatalf("o coletor recebe o SO, o Runner, o relógio e o estado de admin: %+v", got)
	}
	for _, want := range []string{"Otimização do Windows", "! atenção: CPU com uso alto", "O que fazer:", "- info: Muitos itens", "Segurança: só leitura", "toolkit otimizacao --completo"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "== CPU ==") {
		t.Errorf("o resumo não traz o relatório completo:\n%s", out)
	}
	if !strings.Contains(errOut, "Lendo os gargalos") {
		t.Errorf("o andamento vai para stderr: %q", errOut)
	}
}

func TestOtimizacaoCompleto(t *testing.T) {
	out, _, err := runOtim(t, otimDeps(t, "windows", otimReport(), nil), "--completo")
	if err != nil || !strings.Contains(out, "== CPU ==") || !strings.Contains(out, "chrome") || !strings.Contains(out, "== Disco do sistema ==") {
		t.Fatalf("err=%v\n%s", err, out)
	}
}

func TestOtimizacaoJSONValidoESemControleCru(t *testing.T) {
	rep := otimReport()
	rep.CPU.Top[0].Name = "x\u001b[31m\u202eevil\u2028y"
	out, errOut, err := runOtim(t, otimDeps(t, "windows", rep, nil), "--json")
	if err != nil {
		t.Fatal(err)
	}
	if errOut != "" {
		t.Errorf("--json não escreve andamento: %q", errOut)
	}
	var doc struct {
		OS       string
		Findings []struct{ Area, Severity string }
		Report   map[string]any
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, out)
	}
	if doc.OS != "windows" || len(doc.Findings) == 0 || doc.Report["cpu"] == nil {
		t.Fatalf("doc: %+v", doc)
	}
	for _, bad := range []string{"\u001b", "\u202e", "\u2028"} {
		if strings.Contains(out, bad) {
			t.Errorf("o JSON trouxe %U cru", []rune(bad)[0])
		}
	}
}

func TestOtimizacaoSaidaTextualSemEscape(t *testing.T) {
	rep := otimReport()
	rep.CPU.Top[0].Name = "x\u001b]0;pwn\u0007\u202eevil"
	rep.Startup.Items[0].Name = "i\u001b[2Jtem"
	for _, args := range [][]string{nil, {"--completo"}} {
		out, _, err := runOtim(t, otimDeps(t, "windows", rep, nil), args...)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"\u001b", "\u0007", "\u202e"} {
			if strings.Contains(out, bad) {
				t.Errorf("%v: a saída trouxe %U do sistema", args, []rune(bad)[0])
			}
		}
	}
}

func TestOtimizacaoRelatorioNovo0600SemSobrescrever(t *testing.T) {
	path := filepath.Join(t.TempDir(), "otim.txt")
	out, _, err := runOtim(t, otimDeps(t, "windows", otimReport(), nil), "--relatorio", path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "== CPU ==") || !strings.Contains(string(b), "Otimização do Windows") {
		t.Fatalf("o arquivo traz resumo e completo: %v\n%s", err, b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("permissão %v, quer 0600", fi.Mode().Perm())
	}
	if !strings.Contains(out, "Relatório completo salvo em") {
		t.Errorf("faltou a linha do arquivo salvo:\n%s", out)
	}
	if _, _, err := runOtim(t, otimDeps(t, "windows", otimReport(), nil), "--relatorio", path); err == nil {
		t.Error("não pode sobrescrever um arquivo que já existe")
	}
	if b2, _ := os.ReadFile(path); string(b2) != string(b) {
		t.Error("o arquivo existente foi alterado")
	}
}

func TestOtimizacaoForaDoWindowsNaoLeNada(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		for _, args := range [][]string{nil, {"--json"}, {"--completo"}} {
			var got otimGot
			path := filepath.Join(t.TempDir(), "r.txt")
			a := append([]string{}, args...)
			a = append(a, "--relatorio", path)
			out, _, err := runOtim(t, otimDeps(t, goos, &otimizacao.Report{OS: goos}, &got), a...)
			if err == nil || !strings.Contains(err.Error(), "só existe no Windows") || !strings.Contains(err.Error(), goos) {
				t.Errorf("%s %v: erro esperado 'só existe no Windows': %v", goos, args, err)
			}
			if got.calls != 0 || out != "" {
				t.Errorf("%s %v: não lê nada nem escreve: calls=%d out=%q", goos, args, got.calls, out)
			}
			if _, statErr := os.Stat(path); statErr == nil {
				t.Errorf("%s %v: não cria o relatório", goos, args)
			}
		}
	}
}

// Sem Otim.Collect injetado, o comando usa o coletor real sobre o Runner da Deps: os comandos são de leitura e sem Elevate.
func TestOtimizacaoColetorRealSoEmiteLeitura(t *testing.T) {
	rr := &otimRecorder{}
	deps := otimDeps(t, "windows", nil, nil)
	deps.Otim.Collect = nil
	deps.NewRunner = func() exec.Runner { return rr }
	if _, _, err := runOtim(t, deps); err != nil {
		t.Fatal(err) // áreas sem leitura viram "não lido"; o comando não falha
	}
	if len(rr.cmds) == 0 {
		t.Fatal("o coletor real devia chamar o Runner")
	}
	for _, c := range rr.cmds {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("comando fora do contrato: ReadOnly=%v Elevate=%v", c.ReadOnly, c.Elevate)
		}
	}
}

type otimRecorder struct{ cmds []exec.Cmd }

func (r *otimRecorder) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	r.cmds = append(r.cmds, c)
	return exec.Result{ExitCode: 1}, nil
}

func (r *otimRecorder) LookPath(string) (string, error) { return "", errors.New("não achado") }
