package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
	"github.com/PiluVitu/toolkit/internal/platform"
	"github.com/PiluVitu/toolkit/internal/resume"
	"github.com/PiluVitu/toolkit/internal/resume/resumetest"
)

const recSimple = `id: simples
name: Receita simples
description: duas etapas
os: linux
steps:
  - id: a
    name: Primeira
    check: { sh: chk-a }
    run: { sh: do-a }
  - id: b
    run: { sh: do-b }
`

const recReboot = `id: com-reboot
name: Com reboot
os: linux
requires_admin: true
steps:
  - id: a
    run: { sh: do-a }
    reboot: required
  - id: b
    run: { sh: do-b }
`

const recDiag = `id: diag
name: Diagnóstico
os: linux
kind: diagnostic
steps:
  - id: mem
    run: { sh: free }
`

const recInstall = `id: apps
name: Apps
os: linux
steps:
  - id: git
    install: [git]
`

const recMac = `id: so-mac
name: Só mac
os: macos
steps:
  - id: a
    run: { sh: x }
`

type recEnv struct {
	t     *testing.T
	root  string
	run   *exectest.FakeRunner
	reg   *resumetest.Fake
	info  platform.Info
	inter bool
	stdin string
	now   time.Time
}

func newRecEnv(t *testing.T) *recEnv {
	t.Helper()
	return &recEnv{
		t: t, root: t.TempDir(), run: aptFake(t), reg: &resumetest.Fake{},
		info: platform.Info{OS: platform.Linux, User: "ana", BootID: "boot-1", IsAdmin: true},
		now:  time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC),
	}
}

func (e *recEnv) exec(args ...string) (string, string, error) {
	e.t.Helper()
	var o, er bytes.Buffer
	deps := Deps{
		GOOS: "linux", EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return e.run },
		CheckElevation: func(exec.Cmd) error { return nil },
		Interactive:    func() bool { return e.inter },
		Stdin:          strings.NewReader(e.stdin),
	}
	rd := RecipeDeps{
		Detect: func(context.Context, exec.Runner) (platform.Info, error) { return e.info, nil },
		Paths: func(platform.Info) (platform.Paths, error) {
			return platform.Paths{StateDir: filepath.Join(e.root, "state"), LogDir: filepath.Join(e.root, "state", "logs"), BinDir: filepath.Join(e.root, "bin")}, nil
		},
		Home:         func() (string, error) { return e.root, nil },
		Executable:   func() (string, error) { return "/opt/toolkit", nil },
		UID:          func() string { return "501" },
		NewRegistrar: func(resume.Config) (resume.Registrar, error) { return e.reg, nil },
		Now:          func() time.Time { return e.now },
		RecipesFS: fstest.MapFS{
			"recipes/linux/simples.yaml":    {Data: []byte(recSimple)},
			"recipes/linux/com-reboot.yaml": {Data: []byte(recReboot)},
			"recipes/linux/diag.yaml":       {Data: []byte(recDiag)},
			"recipes/linux/apps.yaml":       {Data: []byte(recInstall)},
			"recipes/macos/so-mac.yaml":     {Data: []byte(recMac)},
		},
	}
	root := NewRootCmdWith(&o, &er, deps)
	root.ResetCommands()
	root.AddCommand(newRecipeCmdsWith(deps, rd)...)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return o.String(), er.String(), err
}

func sh(s string) string { return "/bin/sh -c " + s }

func TestRecipeList(t *testing.T) {
	e := newRecEnv(t)
	out, _, err := e.exec("recipe", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID", "com-reboot", "Com reboot", "setup", "sim", "diag", "diagnostic", "simples"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "so-mac") {
		t.Error("receita de macos não aparece no linux")
	}
	out, _, _ = e.exec("recipe", "list", "--os", "macos")
	if !strings.Contains(out, "so-mac") || strings.Contains(out, "simples") {
		t.Errorf("--os macos:\n%s", out)
	}
	out, _, err = e.exec("recipe", "list", "--json")
	if err != nil || !strings.Contains(out, `"id": "simples"`) || !strings.Contains(out, `"requires_admin": true`) {
		t.Errorf("json: %v\n%s", err, out)
	}
	if _, _, err = e.exec("recipe", "list", "--os", "plan9"); err == nil {
		t.Error("so desconhecido")
	}
	out, _, _ = e.exec("recipe", "list", "--os", "windows")
	if !strings.Contains(out, "Nenhuma receita") {
		t.Errorf("vazio:\n%s", out)
	}
}

func TestRecipeRunDryRunTouchesNothing(t *testing.T) {
	e := newRecEnv(t)
	e.run.On(sh("chk-a"), exectest.Exit(0, ""))
	out, _, err := e.exec("recipe", "run", "simples", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"plano da receita simples", "[pularia] a (Primeira)", "[faria] b", "check passou"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(e.root, "state")); err == nil {
		t.Error("dry-run não pode criar estado nem log")
	}
	if e.reg.Registered || len(e.reg.Specs) != 0 {
		t.Error("dry-run não registra retomada")
	}
	for _, c := range e.run.Calls() {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("dry-run executou mutação: %s", c)
		}
	}
}

func TestRecipeRunDryRunRebootRecipeWithoutRoot(t *testing.T) {
	e := newRecEnv(t)
	e.info.IsAdmin = false
	out, _, err := e.exec("recipe", "run", "com-reboot", "--dry-run")
	if err != nil || !strings.Contains(out, "reboot: required") {
		t.Fatalf("o plano deve funcionar sem sudo: %v\n%s", err, out)
	}
}

func TestRecipeRunSimpleWritesLogAndState(t *testing.T) {
	e := newRecEnv(t)
	e.run.On(sh("chk-a"), exectest.Exit(1, "")).On(sh("do-a"), exectest.Exit(0, "feito\x1b[31m")).On(sh("do-b"), exectest.Exit(0, ""))
	out, _, err := e.exec("recipe", "run", "simples", "--verbose")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"==> receita simples (Receita simples)", "[ok] a (Primeira)", "| feito[31m", "[ok] b", "Receita concluída."} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q:\n%s", want, out)
		}
	}
	logs, _ := filepath.Glob(filepath.Join(e.root, "state", "logs", "simples-20261111-120000.log"))
	if len(logs) != 1 {
		t.Fatalf("log esperado: %v", logs)
	}
	if fi, _ := os.Stat(logs[0]); fi.Mode().Perm() != 0o600 {
		t.Errorf("log com permissão %v", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(logs[0])
	if !strings.Contains(string(b), "receita concluída") || strings.Contains(string(b), "feito") {
		t.Errorf("log deve ter eventos e nunca a saída dos comandos:\n%s", b)
	}
	st, _, err := e.exec("recipe", "status")
	if err != nil || !strings.Contains(st, "simples: done") || !strings.Contains(st, "a: done") {
		t.Fatalf("status: %v\n%s", err, st)
	}
	js, _, _ := e.exec("recipe", "status", "--json")
	if !strings.Contains(js, `"schema_version": 1`) {
		t.Errorf("status --json:\n%s", js)
	}
}

func TestRecipeRunFailure(t *testing.T) {
	e := newRecEnv(t)
	e.run.On(sh("chk-a"), exectest.Exit(1, "")).On(sh("do-a"), exectest.Exit(3, "boom"))
	out, _, err := e.exec("recipe", "run", "simples")
	if err == nil || !strings.Contains(err.Error(), "código 3") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(out, "[falhou] a") || !strings.Contains(out, "[não executada] b") {
		t.Errorf("saída:\n%s", out)
	}
	if _, _, err := e.exec("recipe", "run", "simples"); err == nil || !strings.Contains(err.Error(), "reset") {
		t.Errorf("reexecutar falha pede reset: %v", err)
	}
	out, _, err = e.exec("recipe", "reset", "simples")
	if err != nil || !strings.Contains(out, "descartado") {
		t.Fatalf("%v %s", err, out)
	}
	if _, _, err := e.exec("recipe", "reset", "simples"); err == nil {
		t.Error("reset de receita sem estado deve falhar")
	}
}

func TestRecipeUnknownAndWrongArgs(t *testing.T) {
	e := newRecEnv(t)
	if _, _, err := e.exec("recipe", "run", "nao-existe"); err == nil || !strings.Contains(err.Error(), "não existe para este sistema") {
		t.Errorf("err=%v", err)
	}
	if _, _, err := e.exec("recipe", "run", "so-mac"); err == nil {
		t.Error("receita de outro SO")
	}
	if _, _, err := e.exec("recipe", "run"); err == nil {
		t.Error("falta o id")
	}
}

func TestRecipeRebootFlowAndResume(t *testing.T) {
	e := newRecEnv(t)
	e.run.On(sh("do-a"), exectest.Exit(0, "")).On(sh("do-b"), exectest.Exit(0, "")).On("shutdown -r now", exectest.Exit(0, ""))
	out, _, err := e.exec("recipe", "run", "com-reboot", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "[reiniciar] a") || !strings.Contains(out, "Reiniciando o computador") {
		t.Errorf("saída:\n%s", out)
	}
	if !e.reg.Registered || len(e.reg.Specs) != 1 {
		t.Fatal("a retomada deve estar registrada antes do reboot")
	}
	sp := e.reg.Specs[0]
	if sp.Executable != "/opt/toolkit" || strings.Join(sp.Args, " ") != "resume --yes" || sp.User != "ana" || !sp.Elevated || sp.LogPath == "" {
		t.Fatalf("spec: %+v", sp)
	}
	var rebooted bool
	for _, c := range e.run.Calls() {
		rebooted = rebooted || c.String() == "shutdown -r now"
	}
	if !rebooted {
		t.Fatal("o reboot deve passar pelo Runner")
	}
	// resume antes de reiniciar: mesmo BootID
	if _, _, err := e.exec("resume"); err == nil || !strings.Contains(err.Error(), "ainda não reiniciou") {
		t.Fatalf("err=%v", err)
	}
	e.info.BootID = "boot-2"
	out, _, err = e.exec("resume")
	if err != nil || !strings.Contains(out, "[ok] a") || !strings.Contains(out, "[ok] b") || !strings.Contains(out, "Receita concluída.") {
		t.Fatalf("%v\n%s", err, out)
	}
	if e.reg.Registered {
		t.Fatal("o resume termina desregistrando")
	}
	out, _, err = e.exec("resume")
	if err != nil || !strings.Contains(out, "Não há receita para retomar") {
		t.Fatalf("sem nada a retomar não é falha: %v %s", err, out)
	}
}

func TestRecipeRebootDeclinedWithoutYesOrTerminal(t *testing.T) {
	e := newRecEnv(t)
	e.run.On(sh("do-a"), exectest.Exit(0, ""))
	out, _, err := e.exec("recipe", "run", "com-reboot")
	if err != nil {
		t.Fatalf("recusar o reboot não é falha: %v", err)
	}
	if !strings.Contains(out, "esperando o reinício") || !e.reg.Registered {
		t.Errorf("registered=%v\n%s", e.reg.Registered, out)
	}
	for _, c := range e.run.Calls() {
		if c.String() == "shutdown -r now" {
			t.Fatal("sem --yes e sem terminal nunca reinicia")
		}
	}
}

func TestRecipeRebootPromptsOnTerminal(t *testing.T) {
	for _, tc := range []struct {
		answer string
		reboot bool
	}{{"s\n", true}, {"n\n", false}, {"", false}} {
		e := newRecEnv(t)
		e.inter, e.stdin = true, tc.answer
		e.run.On(sh("do-a"), exectest.Exit(0, "")).On("shutdown -r now", exectest.Exit(0, ""))
		out, _, err := e.exec("recipe", "run", "com-reboot")
		if err != nil {
			t.Fatal(err)
		}
		var rebooted bool
		for _, c := range e.run.Calls() {
			rebooted = rebooted || c.String() == "shutdown -r now"
		}
		if rebooted != tc.reboot || !strings.Contains(out, "Reiniciar agora? [s/N]") {
			t.Errorf("resposta %q: reboot=%v\n%s", tc.answer, rebooted, out)
		}
	}
}

func TestRecipeNeedsSudoOnLinuxWhenItCanReboot(t *testing.T) {
	e := newRecEnv(t)
	e.info.IsAdmin = false
	_, _, err := e.exec("recipe", "run", "com-reboot", "--yes")
	if err == nil || !strings.Contains(err.Error(), "rode com sudo") {
		t.Fatalf("err=%v", err)
	}
	if len(e.run.Calls()) != 0 {
		t.Fatal("não executa nada antes de recusar")
	}
	// sem reboot, não precisa de sudo
	e.run.On(sh("chk-a"), exectest.Exit(0, "")).On(sh("do-b"), exectest.Exit(0, ""))
	if _, _, err := e.exec("recipe", "run", "simples"); err != nil {
		t.Fatalf("receita sem reboot roda sem root: %v", err)
	}
}

func TestRecipeNeedsAdminOnWindows(t *testing.T) {
	e := newRecEnv(t)
	e.info = platform.Info{OS: platform.Windows, User: "ana", IsAdmin: false}
	e.exec2Windows(t)
}

func (e *recEnv) exec2Windows(t *testing.T) {
	var o, er bytes.Buffer
	deps := Deps{GOOS: "windows", EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return e.run }, Interactive: func() bool { return false }}
	rd := RecipeDeps{
		Detect: func(context.Context, exec.Runner) (platform.Info, error) { return e.info, nil },
		Paths: func(platform.Info) (platform.Paths, error) {
			return platform.Paths{StateDir: filepath.Join(e.root, "s"), LogDir: filepath.Join(e.root, "s", "l")}, nil
		},
		Executable:   func() (string, error) { return `C:\t.exe`, nil },
		NewRegistrar: func(resume.Config) (resume.Registrar, error) { return e.reg, nil },
		RecipesFS:    fstest.MapFS{"recipes/windows/w.yaml": {Data: []byte("id: w\nname: W\nos: windows\nrequires_admin: true\nsteps:\n  - id: a\n    run: { powershell: x }\n")}},
	}
	root := NewRootCmdWith(&o, &er, deps)
	root.ResetCommands()
	root.AddCommand(newRecipeCmdsWith(deps, rd)...)
	root.SetArgs([]string{"recipe", "run", "w", "--yes"})
	err := root.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exige administrador") {
		t.Fatalf("err=%v", err)
	}
}

func TestRecipeDiagnosticOutput(t *testing.T) {
	e := newRecEnv(t)
	e.run.On(sh("free"), exectest.Exit(0, "Mem: 16G\nSwap: 2G\n"))
	out, _, err := e.exec("recipe", "run", "diag")
	if err != nil || !strings.Contains(out, "| Mem: 16G") || !strings.Contains(out, "| Swap: 2G") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestRecipeInstallStepUsesCatalogProviders(t *testing.T) {
	e := newRecEnv(t)
	e.run.On(statusGit, notInstalled()).
		OnElevated("apt-get update", exectest.Exit(0, "")).
		OnElevated("apt-get install -y --no-remove git", exectest.Exit(0, ""))
	out, _, err := e.exec("recipe", "run", "apps")
	if err != nil || !strings.Contains(out, "==> git (Git)") || !strings.Contains(out, "instalado com apt") || !strings.Contains(out, "[ok] git") {
		t.Fatalf("%v\n%s", err, out)
	}
	// dry-run: só lê, nunca instala
	e2 := newRecEnv(t)
	e2.run.On(statusGit, notInstalled())
	out, _, err = e2.exec("recipe", "run", "apps", "--dry-run")
	if err != nil || !strings.Contains(out, "instalaria: git") {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, c := range e2.run.Calls() {
		if !c.ReadOnly {
			t.Errorf("dry-run executou mutação: %s", c)
		}
	}
}

func TestRecipeInstallStepFailure(t *testing.T) {
	e := newRecEnv(t)
	e.run.On(statusGit, notInstalled()).OnElevated("apt-get update", exectest.Exit(0, "")).
		OnElevated("apt-get install -y --no-remove git", exectest.Exit(100, ""))
	_, _, err := e.exec("recipe", "run", "apps")
	if err == nil || !strings.Contains(err.Error(), "instalação falhou") {
		t.Fatalf("err=%v", err)
	}
}

func TestRecipeStatusEmpty(t *testing.T) {
	e := newRecEnv(t)
	out, _, err := e.exec("recipe", "status")
	if err != nil || !strings.Contains(out, "Nenhuma receita") {
		t.Fatalf("%v %s", err, out)
	}
}

func TestRecipeDetectError(t *testing.T) {
	var o, er bytes.Buffer
	deps := Deps{EmbeddedYML: []byte(embedded)}
	boom := errors.New("sem SO")
	rd := RecipeDeps{Detect: func(context.Context, exec.Runner) (platform.Info, error) { return platform.Info{}, boom }}
	root := NewRootCmdWith(&o, &er, deps)
	root.ResetCommands()
	root.AddCommand(newRecipeCmdsWith(deps, rd)...)
	root.SetArgs([]string{"recipe", "list"})
	if err := root.ExecuteContext(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
}

func TestRootHasRecipeAndResume(t *testing.T) {
	out, _, err := run(t, "recipe", "--help")
	if err != nil || !strings.Contains(out, "run") || !strings.Contains(out, "status") || !strings.Contains(out, "reset") {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, _, err = run(t, "resume", "--help"); err != nil || !strings.Contains(out, "--yes") || !strings.Contains(out, "--no-tui") {
		t.Fatalf("%v\n%s", err, out)
	}
}
