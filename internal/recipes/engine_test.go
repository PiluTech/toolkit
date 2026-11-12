package recipes

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
	"github.com/PiluVitu/toolkit/internal/platform"
	"github.com/PiluVitu/toolkit/internal/resume/resumetest"
	"github.com/PiluVitu/toolkit/internal/state"
)

type fakeInstaller struct {
	calls  [][]string
	out    InstallOutcome
	err    error
	panics bool
}

func (f *fakeInstaller) Install(_ context.Context, apps []string) (InstallOutcome, error) {
	if f.panics {
		panic("boom")
	}
	f.calls = append(f.calls, apps)
	return f.out, f.err
}

type fakeRebooter struct {
	log   *[]string
	store state.Store
	n     int
	// o que o state dizia no momento do reboot
	statusAtReboot state.RecipeStatus
	stepAtReboot   state.StepStatus
}

func (f *fakeRebooter) Reboot(ctx context.Context) error {
	f.n++
	*f.log = append(*f.log, "reboot")
	if file, err := f.store.Load(ctx); err == nil && file.Active != "" {
		rs := file.Recipes[file.Active]
		f.statusAtReboot = rs.Status
		for _, s := range rs.Steps {
			if s.Status == state.StepAwaitingReboot {
				f.stepAtReboot = s.Status
			}
		}
	}
	return nil
}

type fakePrompter struct {
	answer bool
	asked  int
}

func (f *fakePrompter) Confirm(context.Context, string) (bool, error) {
	f.asked++
	return f.answer, nil
}

type env struct {
	t       *testing.T
	store   *state.FileStore
	reg     *resumetest.Fake
	run     *exectest.FakeRunner
	inst    *fakeInstaller
	reboot  *fakeRebooter
	prompt  *fakePrompter
	order   *[]string
	eng     *Engine
	recipes map[string]*Recipe
}

func newEnv(t *testing.T, info platform.Info) *env {
	t.Helper()
	order := &[]string{}
	e := &env{
		t:       t,
		store:   &state.FileStore{Dir: filepath.Join(t.TempDir(), "st"), ToolkitVersion: "t", Sleep: func(time.Duration) {}},
		reg:     &resumetest.Fake{Log: order},
		run:     exectest.New(t),
		inst:    &fakeInstaller{},
		prompt:  &fakePrompter{answer: true},
		order:   order,
		recipes: map[string]*Recipe{},
	}
	e.reboot = &fakeRebooter{log: order, store: e.store}
	e.eng = &Engine{
		Runner: e.run, Store: e.store, Resume: e.reg, Installer: e.inst, Rebooter: e.reboot, Prompter: e.prompt,
		Platform: info, Executable: "/opt/toolkit", LogPath: "/var/log/t.log",
		Recipes:  func(id string) (*Recipe, bool) { r, ok := e.recipes[id]; return r, ok },
		Clock:    func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) },
		NewRunID: func() string { return "run-1" },
	}
	return e
}

func linuxInfo() platform.Info {
	return platform.Info{OS: platform.Linux, User: "ana", BootID: "boot-1", IsAdmin: true}
}

func (e *env) recipe(src string) *Recipe {
	e.t.Helper()
	r, err := Parse("x.txt", []byte(src))
	if err != nil {
		e.t.Fatal(err)
	}
	e.recipes[r.ID] = r
	return r
}

func sh(script string) string { return `/bin/sh -c ` + quoteIf(script) }

func quoteIf(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"'$`\\") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func (e *env) load() *state.File {
	e.t.Helper()
	f, err := e.store.Load(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return f
}

func steps(rep Report) string {
	var out []string
	for _, s := range rep.Steps {
		out = append(out, s.ID+"="+string(s.Outcome))
	}
	return strings.Join(out, ",")
}

const twoSteps = `id: demo
name: Demo
os: linux
steps:
  - id: a
    run: { sh: echo-a }
  - id: b
    run: { sh: echo-b }
`

func TestRunSuccess(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(twoSteps)
	e.run.On(sh("echo-a"), exectest.Exit(0, "a-out\x1b[31m")).On(sh("echo-b"), exectest.Exit(0, "b-out"))
	rep, err := e.eng.Run(context.Background(), r, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if steps(rep) != "a=done,b=done" || rep.Status != state.RecipeDone {
		t.Fatalf("%s / %s", steps(rep), rep.Status)
	}
	if rep.Steps[0].Output != "a-out[31m" { // ESC removido
		t.Fatalf("saída: %q", rep.Steps[0].Output)
	}
	f := e.load()
	rs := f.Recipes["demo"]
	if f.Active != "" || rs.Status != state.RecipeDone || rs.RunID != "run-1" || rs.User != "ana" || rs.RecipeHash != r.Hash {
		t.Fatalf("%+v active=%q", rs, f.Active)
	}
	for _, s := range rs.Steps {
		if s.Status != state.StepDone || s.Attempts != 1 || s.StartedAt == nil || s.FinishedAt == nil {
			t.Fatalf("etapa %+v", s)
		}
	}
	if e.reg.Unregisters != 0 || e.reg.Registered {
		t.Fatal("sem reboot não há retomada")
	}
	// rodar de novo (done) é permitido e idempotente
	if _, err := e.eng.Run(context.Background(), r, RunOptions{}); err != nil {
		t.Fatalf("rerun: %v", err)
	}
}

func TestCheckSkipsStep(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(`id: demo
name: D
os: linux
steps:
  - id: a
    check: { sh: chk }
    run: { sh: do-it }
`)
	e.run.On(sh("chk"), exectest.Exit(0, "")) // do-it sem roteiro: se rodasse, o FakeRunner falharia o teste
	rep, err := e.eng.Run(context.Background(), r, RunOptions{})
	if err != nil || steps(rep) != "a=skipped" {
		t.Fatalf("%v %s", err, steps(rep))
	}
	if s := e.load().Recipes["demo"].Steps[0]; s.Status != state.StepSkipped {
		t.Fatalf("%+v", s)
	}
}

func TestCheckFailsThenRuns(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(`id: demo
name: D
os: linux
steps:
  - id: a
    check: { sh: chk }
    run: { sh: do-it }
`)
	e.run.On(sh("chk"), exectest.Exit(1, "")).On(sh("do-it"), exectest.Exit(0, ""))
	rep, err := e.eng.Run(context.Background(), r, RunOptions{})
	if err != nil || steps(rep) != "a=done" {
		t.Fatalf("%v %s", err, steps(rep))
	}
	for _, c := range e.run.Calls() {
		if strings.Contains(c.String(), "chk") && !c.ReadOnly {
			t.Fatal("o check deve ser ReadOnly")
		}
		if strings.Contains(c.String(), "do-it") && c.ReadOnly {
			t.Fatal("o run não pode ser ReadOnly")
		}
	}
}

func TestStepFailureStopsAndBlocksRerun(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(twoSteps)
	e.run.On(sh("echo-a"), exectest.Exit(2, "x"), exectest.Exit(0, "")).On(sh("echo-b"), exectest.Exit(0, ""))
	rep, err := e.eng.Run(context.Background(), r, RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "código 2") {
		t.Fatalf("err=%v", err)
	}
	if steps(rep) != "a=failed,b=not-reached" || rep.Status != state.RecipeFailed {
		t.Fatalf("%s", steps(rep))
	}
	f := e.load()
	if f.Active != "" || f.Recipes["demo"].Status != state.RecipeFailed || f.Recipes["demo"].Steps[0].ExitCode != 2 {
		t.Fatalf("%+v", f.Recipes["demo"])
	}
	if _, err := e.eng.Run(context.Background(), r, RunOptions{}); !errors.Is(err, ErrFailedState) {
		t.Fatalf("rerun deve exigir reset: %v", err)
	}
	if err := e.eng.Reset(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.eng.Run(context.Background(), r, RunOptions{}); err != nil {
		t.Fatalf("depois do reset: %v", err)
	}
	if e.reg.Unregisters != 0 {
		t.Fatal("reset de falha não mexe na retomada")
	}
}

func TestSuccessCodes(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(`id: demo
name: D
os: linux
steps:
  - id: a
    run: { sh: x, success_codes: [3010] }
`)
	e.run.On(sh("x"), exectest.Exit(3010, ""))
	rep, err := e.eng.Run(context.Background(), r, RunOptions{})
	if err != nil || steps(rep) != "a=done" {
		t.Fatalf("%v %s", err, steps(rep)) // 3010 vale, reboot: never (padrão)
	}
	if e.reboot.n != 0 {
		t.Fatal("reboot: never não reinicia")
	}
}

const rebootRecipe = `id: demo
name: D
os: linux
steps:
  - id: a
    run: { sh: step-a }
    reboot: required
  - id: b
    check: { sh: chk-b }
    run: { sh: step-b }
  - id: c
    run: { sh: step-c }
`

func TestRebootOrderAndResume(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(rebootRecipe)
	e.run.On(sh("step-a"), exectest.Exit(0, ""))
	rep, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true, Verbose: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Rebooting || !rep.RebootPending || rep.Status != state.RecipeAwaitingReboot || steps(rep) != "a=awaiting-reboot,b=not-reached,c=not-reached" {
		t.Fatalf("%+v %s", rep, steps(rep))
	}
	// a ordem importa: registrar a retomada ANTES de reiniciar, e o state já salvo quando reinicia
	if strings.Join(*e.order, ",") != "register,reboot" {
		t.Fatalf("ordem: %v", *e.order)
	}
	if e.reboot.statusAtReboot != state.RecipeAwaitingReboot || e.reboot.stepAtReboot != state.StepAwaitingReboot {
		t.Fatalf("o state precisa estar salvo como awaiting-reboot antes do reboot: %+v", e.reboot)
	}
	if e.prompt.asked != 0 {
		t.Fatal("--yes não pergunta")
	}
	spec := e.reg.Specs[0]
	if spec.Executable != "/opt/toolkit" || strings.Join(spec.Args, " ") != "resume --yes --verbose" || spec.User != "ana" || spec.LogPath != "/var/log/t.log" {
		t.Fatalf("spec: %+v", spec)
	}
	f := e.load()
	if f.Active != "demo" || f.Recipes["demo"].Steps[0].BootID != "boot-1" || !f.Recipes["demo"].Options.Yes {
		t.Fatalf("%+v", f.Recipes["demo"])
	}

	// resume sem a máquina ter reiniciado (mesmo BootID): nada avança, retomada continua
	rep, err = e.eng.ResumePending(context.Background())
	if !errors.Is(err, ErrNotRebooted) || !rep.RebootPending {
		t.Fatalf("err=%v", err)
	}
	if e.reg.Unregisters != 0 || !e.reg.Registered {
		t.Fatal("não pode desregistrar sem ter reiniciado")
	}
	if e.load().Recipes["demo"].Steps[0].ResumeAttempts != 0 {
		t.Fatal("nada avançou")
	}

	// reiniciou: boot novo
	e.eng.Platform.BootID = "boot-2"
	e.run.On(sh("chk-b"), exectest.Exit(1, "")).On(sh("step-b"), exectest.Exit(0, "")).On(sh("step-c"), exectest.Exit(0, ""))
	rep, err = e.eng.ResumePending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if steps(rep) != "a=done,b=done,c=done" || rep.Status != state.RecipeDone || !rep.Resumed {
		t.Fatalf("%s %+v", steps(rep), rep)
	}
	f = e.load()
	if f.Active != "" || f.Recipes["demo"].Status != state.RecipeDone || f.Recipes["demo"].Steps[0].Status != state.StepDone || f.Recipes["demo"].Steps[0].ResumeAttempts != 1 {
		t.Fatalf("%+v", f.Recipes["demo"])
	}
	if e.reg.Registered || e.reg.Unregisters != 1 {
		t.Fatal("o resume termina com Unregister")
	}
	if e.reboot.n != 1 {
		t.Fatal("só um reboot")
	}
}

func TestRebootDeclinedKeepsResume(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(rebootRecipe)
	e.prompt.answer = false
	e.run.On(sh("step-a"), exectest.Exit(0, ""))
	rep, err := e.eng.Run(context.Background(), r, RunOptions{})
	if !errors.Is(err, ErrRebootDeclined) || rep.Rebooting || !rep.RebootPending {
		t.Fatalf("err=%v %+v", err, rep)
	}
	if e.prompt.asked != 1 || e.reboot.n != 0 || !e.reg.Registered {
		t.Fatalf("asked=%d reboot=%d registered=%v", e.prompt.asked, e.reboot.n, e.reg.Registered)
	}
	if e.load().Recipes["demo"].Status != state.RecipeAwaitingReboot {
		t.Fatal("continua awaiting-reboot")
	}
	// rodar de novo enquanto pendente: pede resume
	if _, err := e.eng.Run(context.Background(), r, RunOptions{}); !errors.Is(err, ErrPending) {
		t.Fatalf("err=%v", err)
	}
}

func TestRebootNeedsPrompterOrYes(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(rebootRecipe)
	e.eng.Prompter = nil
	e.run.On(sh("step-a"), exectest.Exit(0, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{}); !errors.Is(err, ErrRebootDeclined) || e.reboot.n != 0 {
		t.Fatalf("err=%v", err)
	}
}

func TestRegisterFailureNeverReboots(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(rebootRecipe)
	e.reg.RegisterErr = errors.New("sem permissão")
	e.run.On(sh("step-a"), exectest.Exit(0, ""))
	_, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true})
	if err == nil || !strings.Contains(err.Error(), "não reiniciei") || e.reboot.n != 0 {
		t.Fatalf("err=%v reboot=%d", err, e.reboot.n)
	}
}

func TestResumeCheckFailsAfterReboot(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(`id: demo
name: D
os: linux
steps:
  - id: a
    check: { sh: chk-a }
    run: { sh: step-a }
    reboot: required
  - id: b
    run: { sh: step-b }
`)
	e.run.On(sh("chk-a"), exectest.Exit(1, ""), exectest.Exit(1, "")).On(sh("step-a"), exectest.Exit(0, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	e.eng.Platform.BootID = "boot-2"
	rep, err := e.eng.ResumePending(context.Background())
	if err == nil || !strings.Contains(err.Error(), "check não passou depois do reboot") {
		t.Fatalf("err=%v", err)
	}
	if steps(rep) != "a=failed,b=not-reached" {
		t.Fatal(steps(rep))
	}
	if e.reboot.n != 1 {
		t.Fatal("não há segundo reboot")
	}
	f := e.load()
	if f.Active != "" || f.Recipes["demo"].Status != state.RecipeFailed || e.reg.Registered {
		t.Fatalf("%+v registered=%v", f.Recipes["demo"], e.reg.Registered)
	}
}

func TestResumeNewRebootKeepsRegistration(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(`id: demo
name: D
os: linux
steps:
  - id: a
    run: { sh: step-a }
    reboot: required
  - id: b
    run: { sh: step-b }
    reboot: required
  - id: c
    run: { sh: step-c }
`)
	e.run.On(sh("step-a"), exectest.Exit(0, "")).On(sh("step-b"), exectest.Exit(0, "")).On(sh("step-c"), exectest.Exit(0, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	e.eng.Platform.BootID = "boot-2"
	rep, err := e.eng.ResumePending(context.Background())
	if err != nil || !rep.Rebooting || steps(rep) != "a=done,b=awaiting-reboot,c=not-reached" {
		t.Fatalf("err=%v %s", err, steps(rep))
	}
	if !e.reg.Registered || e.reg.Unregisters != 0 || e.reboot.n != 2 {
		t.Fatalf("registered=%v unreg=%d reboots=%d", e.reg.Registered, e.reg.Unregisters, e.reboot.n)
	}
	if e.load().Recipes["demo"].Steps[1].BootID != "boot-2" {
		t.Fatal("novo BootID")
	}
	e.eng.Platform.BootID = "boot-3"
	rep, err = e.eng.ResumePending(context.Background())
	if err != nil || rep.Status != state.RecipeDone || e.reg.Registered {
		t.Fatalf("err=%v %+v", err, rep)
	}
}

func TestIfNeeded(t *testing.T) {
	src := `id: demo
name: D
os: linux
steps:
  - id: a
    run: { sh: x, success_codes: [3010] }
    reboot: if-needed
`
	e := newEnv(t, linuxInfo())
	r := e.recipe(src)
	e.run.On(sh("x"), exectest.Exit(0, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true}); err != nil || e.reboot.n != 0 {
		t.Fatalf("exit 0 não reinicia: %v", err)
	}
	e = newEnv(t, linuxInfo())
	r = e.recipe(src)
	e.run.On(sh("x"), exectest.Exit(3010, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true}); err != nil || e.reboot.n != 1 {
		t.Fatalf("3010 reinicia: %v n=%d", err, e.reboot.n)
	}
}

func TestIfNeededInstall(t *testing.T) {
	src := `id: demo
name: D
os: linux
steps:
  - id: apps
    install: [git, "python@3.12"]
    reboot: if-needed
`
	e := newEnv(t, linuxInfo())
	r := e.recipe(src)
	if _, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true}); err != nil || e.reboot.n != 0 {
		t.Fatalf("sem pedido: %v", err)
	}
	if len(e.inst.calls) != 1 || strings.Join(e.inst.calls[0], ",") != "git,python@3.12" {
		t.Fatalf("calls=%v", e.inst.calls)
	}
	e = newEnv(t, linuxInfo())
	r = e.recipe(src)
	e.inst.out = InstallOutcome{RebootRequired: true}
	if _, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true}); err != nil || e.reboot.n != 1 {
		t.Fatalf("com pedido: %v", err)
	}
}

func TestInstallFailures(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe("id: demo\nname: D\nos: linux\nsteps:\n  - id: apps\n    install: [git]\n")
	e.inst.err = errors.New("falhou feio")
	if _, err := e.eng.Run(context.Background(), r, RunOptions{}); err == nil || !strings.Contains(err.Error(), "falhou feio") {
		t.Fatalf("err=%v", err)
	}
	e = newEnv(t, linuxInfo())
	r = e.recipe("id: demo\nname: D\nos: linux\nsteps:\n  - id: apps\n    install: [git]\n")
	e.eng.Installer = nil
	if _, err := e.eng.Run(context.Background(), r, RunOptions{}); err == nil {
		t.Fatal("sem Installer deve falhar")
	}
}

func TestResumeRecipeChanged(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(rebootRecipe)
	e.run.On(sh("step-a"), exectest.Exit(0, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	e.recipes["demo"] = e.recipe(strings.Replace(rebootRecipe, "step-c", "step-c2", 1)) // binário atualizado
	e.eng.Platform.BootID = "boot-2"
	_, err := e.eng.ResumePending(context.Background())
	if !errors.Is(err, ErrRecipeChanged) {
		t.Fatalf("err=%v", err)
	}
	f := e.load()
	if f.Active != "" || f.Recipes["demo"].Status != state.RecipeFailed || e.reg.Registered {
		t.Fatalf("%+v", f.Recipes["demo"])
	}
}

func TestResumeUnknownRecipe(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(rebootRecipe)
	e.run.On(sh("step-a"), exectest.Exit(0, ""))
	_, _ = e.eng.Run(context.Background(), r, RunOptions{Yes: true})
	delete(e.recipes, "demo")
	e.eng.Platform.BootID = "boot-2"
	if _, err := e.eng.ResumePending(context.Background()); !errors.Is(err, ErrRecipeNotFound) || e.reg.Registered {
		t.Fatalf("err=%v", err)
	}
}

func TestResumeNothingActive(t *testing.T) {
	e := newEnv(t, linuxInfo())
	e.reg.Registered = true
	if _, err := e.eng.ResumePending(context.Background()); !errors.Is(err, ErrNothingResume) {
		t.Fatalf("err=%v", err)
	}
	if e.reg.Registered {
		t.Fatal("um registro esquecido deve ser removido")
	}
}

func TestResumeInterruptedStep(t *testing.T) {
	// simula a queda de energia: a etapa ficou "running"
	crash := func(e *env, r *Recipe) {
		err := e.store.Update(context.Background(), func(f *state.File) error {
			rs := state.NewRecipeState("demo", r.Hash, "run-1", "ana", []string{"a", "b"}, state.Options{}, time.Now())
			rs.Steps[0].Status = state.StepRunning
			rs.Steps[0].Attempts = 1
			f.Recipes["demo"], f.Active = rs, "demo"
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Run("sem check vira failed", func(t *testing.T) {
		e := newEnv(t, linuxInfo())
		r := e.recipe(twoSteps)
		crash(e, r)
		rep, err := e.eng.ResumePending(context.Background())
		if err == nil || !strings.Contains(err.Error(), "sem check") || steps(rep) != "a=failed,b=not-reached" {
			t.Fatalf("err=%v %s", err, steps(rep))
		}
		if e.load().Recipes["demo"].Status != state.RecipeFailed || e.reg.Unregisters != 1 {
			t.Fatal("falha e desregistra")
		}
	})
	src := `id: demo
name: D
os: linux
steps:
  - id: a
    check: { sh: chk }
    run: { sh: step-a }
  - id: b
    run: { sh: step-b }
`
	t.Run("com check que passa vira skipped", func(t *testing.T) {
		e := newEnv(t, linuxInfo())
		r := e.recipe(src)
		crash(e, r)
		e.run.On(sh("chk"), exectest.Exit(0, "")).On(sh("step-b"), exectest.Exit(0, ""))
		rep, err := e.eng.ResumePending(context.Background())
		if err != nil || steps(rep) != "a=skipped,b=done" {
			t.Fatalf("err=%v %s", err, steps(rep))
		}
	})
	t.Run("com check que falha repete uma vez", func(t *testing.T) {
		e := newEnv(t, linuxInfo())
		r := e.recipe(src)
		crash(e, r)
		e.run.On(sh("chk"), exectest.Exit(1, "")).On(sh("step-a"), exectest.Exit(0, "")).On(sh("step-b"), exectest.Exit(0, ""))
		rep, err := e.eng.ResumePending(context.Background())
		if err != nil || steps(rep) != "a=done,b=done" {
			t.Fatalf("err=%v %s", err, steps(rep))
		}
		if e.load().Recipes["demo"].Steps[0].ResumeAttempts != 1 {
			t.Fatal("ResumeAttempts")
		}
	})
	t.Run("segunda queda falha", func(t *testing.T) {
		e := newEnv(t, linuxInfo())
		r := e.recipe(src)
		crash(e, r)
		_ = e.store.Update(context.Background(), func(f *state.File) error { f.Recipes["demo"].Steps[0].ResumeAttempts = 1; return nil })
		_, err := e.eng.ResumePending(context.Background())
		if err == nil || !strings.Contains(err.Error(), "já foi repetida") {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestResumeUnregistersOnPanic(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe("id: demo\nname: D\nos: linux\nsteps:\n  - id: a\n    run: { sh: x }\n    reboot: required\n  - id: b\n    install: [git]\n")
	e.run.On(sh("x"), exectest.Exit(0, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	e.eng.Platform.BootID = "boot-2"
	e.inst.panics = true
	func() {
		defer func() { _ = recover() }()
		_, _ = e.eng.ResumePending(context.Background())
	}()
	if e.reg.Registered {
		t.Fatal("o defer deve desregistrar mesmo com panic")
	}
}

func TestBusyAndPending(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(rebootRecipe)
	other := e.recipe("id: other\nname: O\nos: linux\nsteps:\n  - id: a\n    run: { sh: x }\n")
	e.run.On(sh("step-a"), exectest.Exit(0, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.eng.Run(context.Background(), other, RunOptions{}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err=%v", err)
	}
}

func TestResetPendingUnregisters(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(rebootRecipe)
	e.run.On(sh("step-a"), exectest.Exit(0, ""))
	_, _ = e.eng.Run(context.Background(), r, RunOptions{Yes: true})
	if err := e.eng.Reset(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	f := e.load()
	if f.Active != "" || len(f.Recipes) != 0 || e.reg.Registered {
		t.Fatalf("%+v", f)
	}
	if err := e.eng.Reset(context.Background(), "demo"); !errors.Is(err, ErrUnknownRecipe) {
		t.Fatalf("err=%v", err)
	}
}

func TestDryRun(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe(`id: demo
name: D
os: linux
steps:
  - id: a
    check: { sh: chk-a }
    run: { sh: step-a }
  - id: b
    install: [git]
    reboot: if-needed
  - id: c
    run: { sh: step-c }
    reboot: required
`)
	e.run.On(sh("chk-a"), exectest.Exit(0, "")) // só o check pode rodar
	rep, err := e.eng.Run(context.Background(), r, RunOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if steps(rep) != "a=would-skip,b=would-run,c=would-run" || !rep.DryRun {
		t.Fatal(steps(rep))
	}
	if !strings.Contains(rep.Steps[1].Detail, "instalaria: git") || !strings.Contains(rep.Steps[2].Detail, "reboot: required") {
		t.Fatalf("%+v", rep.Steps)
	}
	if _, serr := e.store.Load(context.Background()); serr != nil {
		t.Fatal(serr)
	}
	if e.reg.Registered || len(*e.order) != 0 || e.reboot.n != 0 || len(e.inst.calls) != 0 {
		t.Fatal("dry-run não registra, não reinicia, não instala")
	}
	if got := e.run.Calls(); len(got) != 1 || !got[0].ReadOnly {
		t.Fatalf("só o check roda: %v", got)
	}
}

func TestDryRunNeverElevatesChecks(t *testing.T) {
	info := linuxInfo()
	info.IsAdmin = false
	e := newEnv(t, info)
	r := e.recipe("id: demo\nname: D\nos: linux\nrequires_admin: true\nsteps:\n  - id: a\n    check: { sh: chk }\n    run: { sh: x }\n")
	e.run.On(sh("chk"), exectest.Exit(1, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if c := e.run.Calls()[0]; c.Elevate {
		t.Fatal("dry-run não eleva")
	}
}

func TestWrongOSAndAdmin(t *testing.T) {
	e := newEnv(t, linuxInfo())
	r := e.recipe("id: demo\nname: D\nos: macos\nsteps:\n  - id: a\n    run: { sh: x }\n")
	if _, err := e.eng.Run(context.Background(), r, RunOptions{}); !errors.Is(err, ErrWrongOS) {
		t.Fatalf("err=%v", err)
	}
	win := newEnv(t, platform.Info{OS: platform.Windows, User: "ana", BootID: "b"})
	rw := win.recipe("id: demo\nname: D\nos: windows\nrequires_admin: true\nsteps:\n  - id: a\n    run: { powershell: x }\n")
	if _, err := win.eng.Run(context.Background(), rw, RunOptions{}); !errors.Is(err, ErrNeedsAdmin) {
		t.Fatalf("err=%v", err)
	}
	if _, err := win.eng.Run(context.Background(), rw, RunOptions{DryRun: true}); err != nil {
		t.Fatalf("dry-run de receita admin sem admin deve funcionar: %v", err)
	}
}

func TestUnixNonRootElevatesRequiresAdmin(t *testing.T) {
	info := linuxInfo()
	info.IsAdmin = false
	e := newEnv(t, info)
	r := e.recipe("id: demo\nname: D\nos: linux\nrequires_admin: true\nsteps:\n  - id: a\n    run: { sh: x }\n")
	e.run.OnElevated(sh("x"), exectest.Exit(0, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if !e.run.Calls()[0].Elevate {
		t.Fatal("deve elevar com sudo -n")
	}
}

func TestWindowsCommands(t *testing.T) {
	info := platform.Info{OS: platform.Windows, User: "ana", BootID: "b", IsAdmin: true}
	e := newEnv(t, info)
	r := e.recipe(`id: demo
name: D
os: windows
steps:
  - id: a
    run: { powershell: "Write-Output ok" }
  - id: b
    run: { wsl: { distro: Ubuntu, user: dev, bash: "echo oi" } }
`)
	ps := exec.Cmd{Name: windowsPowerShell, Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", EncodePowerShell("Write-Output ok")}}
	wsl := exec.Cmd{Name: windowsWSL, Args: []string{"-d", "Ubuntu", "-u", "dev", "--", "bash", "-c", "echo oi"}}
	e.run.On(ps.String(), exectest.Exit(0, "ok")).On(wsl.String(), exectest.Exit(0, ""))
	if _, err := e.eng.Run(context.Background(), r, RunOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestEncodePowerShell(t *testing.T) {
	if got := EncodePowerShell("A"); got != "QQA=" {
		t.Fatalf("got %q", got)
	}
	if got := EncodePowerShell("é"); got != "6QA=" { // U+00E9 em UTF-16LE
		t.Fatalf("got %q", got)
	}
}

func TestBuildCmdWrongOS(t *testing.T) {
	if _, err := BuildCmd(&Action{PowerShell: "x"}, platform.Linux, 0, false, false); !errors.Is(err, ErrBadAction) {
		t.Fatal("powershell em linux")
	}
	c, err := BuildCmd(&Action{Bash: "echo"}, platform.Darwin, time.Minute, true, true)
	if err != nil || c.Name != "bash" || !c.Elevate || !c.ReadOnly || c.Timeout != time.Minute {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestSystemRebooter(t *testing.T) {
	r := exectest.New(t)
	r.On(`shutdown -r now`, exectest.Exit(0, ""))
	if err := (SystemRebooter{Runner: r, Info: platform.Info{OS: platform.Linux, IsAdmin: true}}).Reboot(context.Background()); err != nil {
		t.Fatal(err)
	}
	r = exectest.New(t)
	r.OnElevated(`shutdown -r now`, exectest.Exit(0, ""))
	if err := (SystemRebooter{Runner: r, Info: platform.Info{OS: platform.Darwin}}).Reboot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.Calls()[0].Elevate {
		t.Fatal("não-root usa sudo -n")
	}
	r = exectest.New(t)
	win := exec.Cmd{Name: windowsShutdown, Args: []string{"/r", "/t", "5", "/c", "toolkit: reiniciando para continuar a receita"}}
	r.On(win.String(), exectest.Exit(1, ""))
	if err := (SystemRebooter{Runner: r, Info: platform.Info{OS: platform.Windows}}).Reboot(context.Background()); err == nil {
		t.Fatal("exit != 0 deve virar erro")
	}
}
