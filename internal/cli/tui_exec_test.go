package cli

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf16"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/providers"
	"github.com/PiluTech/toolkit/internal/recipes"
	"github.com/PiluTech/toolkit/internal/resume"
	"github.com/PiluTech/toolkit/internal/resume/resumetest"
	"github.com/PiluTech/toolkit/internal/tui"
)

const statusPy = `dpkg-query -W "-f=${db:Status-Status}\t${Version}\n" python3.13`

// tuiEnv monta Deps e RecipeDeps com runner falso: nada executa de verdade.
type tuiEnv struct {
	t    *testing.T
	run  *exectest.FakeRunner
	root string
	info platform.Info
	reg  *resumetest.Fake
	fs   fstest.MapFS
	euid int
}

func newTUIEnv(t *testing.T) *tuiEnv {
	return &tuiEnv{
		t: t, run: aptFake(t), root: t.TempDir(), reg: &resumetest.Fake{}, euid: 1000,
		info: platform.Info{OS: platform.Linux, Arch: "amd64", User: "ana", BootID: "boot-1", IsAdmin: true},
		fs: fstest.MapFS{
			"recipes/linux/simples.yaml":    {Data: []byte(recSimple)},
			"recipes/linux/com-reboot.yaml": {Data: []byte(recReboot)},
			"recipes/linux/diag.yaml":       {Data: []byte(recDiag)},
		},
	}
}

func (e *tuiEnv) deps() Deps {
	return Deps{GOOS: "linux", Euid: func() int { return e.euid }, EmbeddedYML: []byte(embedded),
		NewRunner: func() exec.Runner { return e.run }, CheckElevation: func(exec.Cmd) error { return nil },
		Interactive: func() bool { return false }}
}

func (e *tuiEnv) rd() RecipeDeps {
	return RecipeDeps{
		Detect: func(context.Context, exec.Runner) (platform.Info, error) { return e.info, nil },
		Paths: func(platform.Info) (platform.Paths, error) {
			return platform.Paths{StateDir: filepath.Join(e.root, "state"), LogDir: filepath.Join(e.root, "logs"), BinDir: filepath.Join(e.root, "bin")}, nil
		},
		Home:         func() (string, error) { return e.root, nil },
		Executable:   func() (string, error) { return "/opt/toolkit", nil },
		UID:          func() string { return "501" },
		NewRegistrar: func(resume.Config) (resume.Registrar, error) { return e.reg, nil },
		Now:          func() time.Time { return time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC) },
		RecipesFS:    e.fs,
	}
}

func (e *tuiEnv) planner() *tuiPlanner { return &tuiPlanner{deps: e.deps(), rd: e.rd()} }

func (e *tuiEnv) runner(logDir string) *tuiRunner {
	r := &tuiRunner{deps: e.deps(), rd: e.rd()}
	if logDir != "" {
		r.logDir = func() string { return logDir }
	}
	return r
}

// approve calcula o plano da seleção e a devolve com Approved preenchido, como a raiz da TUI faz ao executar.
func (e *tuiEnv) approve(sel tui.Selection) tui.Selection {
	e.t.Helper()
	plan, err := e.planner().Plan(context.Background(), sel)
	if err != nil {
		e.t.Fatal(err)
	}
	sel.Approved = plan.Approved
	return sel
}

func gitMark() tui.Mark { return tui.Mark{ID: "git", Name: "Git"} }

func onlyReadsRunner(t *testing.T, f *exectest.FakeRunner) {
	t.Helper()
	for _, c := range f.Calls() {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("o plano executou uma mutação: %s", c)
		}
	}
}

// ---- Planner ----

func TestPlannerAppsAsUserShowsSudoCommandsAndWarning(t *testing.T) {
	e := newTUIEnv(t)
	e.run.On(statusGit, notInstalled())
	plan, err := e.planner().Plan(context.Background(), tui.Selection{Apps: []tui.Mark{gitMark()}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary != "Vai instalar 1 app. Nada foi executado ainda." || plan.Env != "linux amd64 · usuário ana" || plan.NothingToDo || len(plan.Problems) != 0 {
		t.Fatalf("plano: %+v", plan)
	}
	if !contains(plan.Commands, "sudo -n -- env DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get install -y --no-remove git") {
		t.Fatalf("comandos: %v", plan.Commands)
	}
	if !contains(plan.Warnings, msg.WarnPlanSudo) || !containsSub(plan.Changes, "seria instalado com apt (simulado)") {
		t.Fatalf("avisos %v / muda %v", plan.Warnings, plan.Changes)
	}
	onlyReadsRunner(t, e.run)
}

func TestPlannerAppsAsRootShowsNoSudo(t *testing.T) {
	e := newTUIEnv(t)
	e.euid = 0
	e.run.On(statusGit, notInstalled())
	plan, err := e.planner().Plan(context.Background(), tui.Selection{Apps: []tui.Mark{gitMark()}})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(append(plan.Commands, plan.Warnings...), "\n")
	if strings.Contains(all, "sudo") {
		t.Fatalf("como root o plano não mostra sudo:\n%s", all)
	}
	if !containsSub(plan.Commands, "DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get install -y --no-remove git") {
		t.Fatalf("comandos: %v", plan.Commands)
	}
}

func TestPlannerEverythingInstalledIsNothingToDo(t *testing.T) {
	e := newTUIEnv(t)
	e.run.On(statusGit, exectest.Exit(0, "installed\t1:2.43.0-1\n"))
	plan, err := e.planner().Plan(context.Background(), tui.Selection{Apps: []tui.Mark{gitMark()}})
	if err != nil || !plan.NothingToDo || len(plan.Commands) != 0 {
		t.Fatalf("err=%v plano=%+v", err, plan)
	}
}

func TestPlannerProblems(t *testing.T) {
	e := newTUIEnv(t)
	e.run.On(statusGit, notInstalled())
	// firefox só tem flatpak (ausente), versão 9.9 não está na lista do python e "x" não existe: tudo Problems;
	// o git continua planejado
	plan, err := e.planner().Plan(context.Background(), tui.Selection{Apps: []tui.Mark{
		gitMark(), {ID: "firefox", Name: "Firefox"}, {ID: "python", Name: "Python", Version: "9.9"}, {ID: "x", Name: "X"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Problems) != 3 {
		t.Fatalf("problemas: %v", plan.Problems)
	}
	for _, want := range []string{"firefox", "python", `app "x" fora do catálogo`} {
		if !containsSub(plan.Problems, want) {
			t.Errorf("faltou problema sobre %q: %v", want, plan.Problems)
		}
	}
	if !containsSub(plan.Commands, "apt-get install -y --no-remove git") {
		t.Fatalf("o git ainda é planejado: %v", plan.Commands)
	}
}

func TestPlannerRecipesPrechecksAndWarnings(t *testing.T) {
	e := newTUIEnv(t)
	e.info.IsAdmin = false // Linux sem sudo: receita com reinício é recusada (como `recipe run`)
	plan, err := e.planner().Plan(context.Background(), tui.Selection{Recipes: []tui.RecipeMark{{ID: "com-reboot", Name: "Com reboot"}, {ID: "diag", Name: "Diagnóstico", Diagnostic: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSub(plan.Problems, "com-reboot") {
		t.Fatalf("receita com reinício sem sudo é Problem: %v", plan.Problems)
	}
	if plan.Summary != "Vai rodar 1 receita e rodar 1 diagnóstico. Nada foi executado ainda." {
		t.Fatalf("resumo: %q", plan.Summary)
	}
	if !containsSub(plan.Warnings, "com-reboot exige administrador") || !containsSub(plan.Warnings, "com-reboot pode reiniciar a máquina") {
		t.Fatalf("avisos: %v", plan.Warnings)
	}
	if !containsSub(plan.Changes, "receita com-reboot (Com reboot): 2 etapas") || !containsSub(plan.Changes, "diagnóstico diag (Diagnóstico): 1 etapa") {
		t.Fatalf("muda: %v", plan.Changes)
	}
	if !containsSub(plan.Commands, "[diag 1/1] ") {
		t.Fatalf("comandos: %v", plan.Commands)
	}
	// como admin (sudo) a mesma receita passa nas pré-checagens
	e.info.IsAdmin = true
	plan, _ = e.planner().Plan(context.Background(), tui.Selection{Recipes: []tui.RecipeMark{{ID: "com-reboot", Name: "Com reboot"}}})
	if len(plan.Problems) != 0 {
		t.Fatalf("como admin: %v", plan.Problems)
	}
}

func TestPlannerUnknownRecipeAndWrongOS(t *testing.T) {
	e := newTUIEnv(t)
	plan, err := e.planner().Plan(context.Background(), tui.Selection{Recipes: []tui.RecipeMark{{ID: "nao-existe", Name: "X"}}})
	if err != nil || !containsSub(plan.Problems, `receita "nao-existe" não existe`) {
		t.Fatalf("err=%v %v", err, plan.Problems)
	}
}

// O script PowerShell vai LEGÍVEL na Revisão, e é exatamente o que BuildCmd codifica em -EncodedCommand.
func TestPlannerPowerShellScriptIsReadableAndMatchesWhatIsEncoded(t *testing.T) {
	script := "Get-CimInstance Win32_PhysicalMemory | Select-Object Capacity"
	e := newTUIEnv(t)
	e.info = platform.Info{OS: platform.Windows, Arch: "amd64", User: "ana", IsAdmin: true}
	e.fs = fstest.MapFS{"recipes/windows/w.yaml": {Data: []byte("id: w\nname: W\nos: windows\nkind: diagnostic\nsteps:\n  - id: ram\n    name: RAM\n    run: { powershell: \"" + script + "\" }\n")}}
	d := e.deps()
	d.GOOS = "windows"
	pl := &tuiPlanner{deps: d, rd: e.rd()}
	plan, err := pl.Plan(context.Background(), tui.Selection{Recipes: []tui.RecipeMark{{ID: "w", Name: "W", Diagnostic: true}}})
	if err != nil {
		t.Fatal(err)
	}
	want := "[w 1/1] " + script + " (enviado como -EncodedCommand)"
	if !contains(plan.Commands, want) {
		t.Fatalf("comandos: %v", plan.Commands)
	}
	rec := mustRecipe(t, e, "w")
	cmd, err := recipes.BuildCmd(rec.Steps[0].Run, platform.Windows, 0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded := cmd.Args[len(cmd.Args)-1]
	if got := decodePS(t, encoded); got != script {
		t.Fatalf("o texto exibido não é o que vai codificado: %q != %q", got, script)
	}
	if encoded != recipes.EncodePowerShell(script) {
		t.Fatal("a codificação não bate com EncodePowerShell do script exibido")
	}
}

func mustRecipe(t *testing.T, e *tuiEnv, id string) *recipes.Recipe {
	t.Helper()
	rt, err := setupRecipes(context.Background(), e.deps(), e.rd())
	if err != nil {
		t.Fatal(err)
	}
	r, ok := rt.set.Get(e.info.OS, id)
	if !ok {
		t.Fatalf("receita %s", id)
	}
	return r
}

func decodePS(t *testing.T, b64 string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func containsSub(s []string, sub string) bool {
	for _, x := range s {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

// ---- Runner ----

func drain(ch <-chan tui.Event) []tui.Event {
	var out []tui.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func runAll(ctx context.Context, t *testing.T, r *tuiRunner, sel tui.Selection) []tui.Event {
	t.Helper()
	ch := make(chan tui.Event, 256)
	done := make(chan struct{})
	go func() { r.Run(ctx, sel, ch); close(done) }()
	evs := drain(ch)
	<-done
	return evs
}

func statuses(evs []tui.Event) string {
	var out []string
	for _, ev := range evs {
		if ev.Kind == tui.EventItemDone {
			out = append(out, string(ev.Status))
		}
	}
	return strings.Join(out, ",")
}

func logText(evs []tui.Event) string {
	var b strings.Builder
	for _, ev := range evs {
		if ev.Kind == tui.EventLog {
			b.WriteString(ev.Line + "\n")
		}
	}
	return b.String()
}

func TestRunnerInstallsAppAndCountsEverythingByEvents(t *testing.T) {
	e := newTUIEnv(t)
	e.run.On(statusGit, notInstalled(), notInstalled()).
		OnElevated("apt-get update", exectest.Exit(0, "")).
		OnElevated("apt-get install -y --no-remove git", exectest.Exit(0, ""))
	evs := runAll(context.Background(), t, e.runner(""), e.approve(tui.Selection{Apps: []tui.Mark{gitMark()}}))
	if evs[0].Kind != tui.EventItems || len(evs[0].Items) != 1 || evs[0].Items[0] != "Git" {
		t.Fatalf("a fila primeiro: %+v", evs[0])
	}
	if evs[len(evs)-1].Kind != tui.EventDone || statuses(evs) != "ok" {
		t.Fatalf("fim/estado: %s %+v", statuses(evs), evs[len(evs)-1])
	}
	if !strings.Contains(logText(evs), "instalado com apt") || !strings.Contains(logText(evs), "==> git (Git)") {
		t.Fatalf("log:\n%s", logText(evs))
	}
	var elevated int
	for _, c := range e.run.Calls() {
		if c.Elevate {
			elevated++
		}
	}
	if elevated != 2 {
		t.Fatalf("update + install: %d", elevated)
	}
}

func TestRunnerFailureOfOneAppDoesNotStopTheNext(t *testing.T) {
	e := newTUIEnv(t)
	e.run.On(statusGit, notInstalled(), notInstalled()).
		OnElevated("apt-get update", exectest.Exit(0, "")).
		OnElevated("apt-get install -y --no-remove git", exectest.Exit(100, "")).
		On(statusPy, notInstalled(), notInstalled()).
		OnElevated("apt-get install -y --no-remove python3.13", exectest.Exit(0, ""))
	evs := runAll(context.Background(), t, e.runner(""), e.approve(tui.Selection{Apps: []tui.Mark{gitMark(), {ID: "python", Name: "Python"}}}))
	if got := statuses(evs); got != "failed,ok" {
		t.Fatalf("estados: %s", got)
	}
	for _, ev := range evs {
		if ev.Kind == tui.EventItemDone && ev.Status == tui.ItemFailed && ev.Err == "" {
			t.Fatal("a falha traz o motivo")
		}
	}
}

func TestRunnerCancelledBeforeStartMarksEverythingNotRun(t *testing.T) {
	e := newTUIEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	evs := runAll(ctx, t, e.runner(""), tui.Selection{Apps: []tui.Mark{gitMark(), {ID: "python", Name: "Python"}}})
	if got := statuses(evs); got != "not-run,not-run" || evs[len(evs)-1].Kind != tui.EventDone {
		t.Fatalf("estados %s", got)
	}
	if len(e.run.Calls()) != 0 {
		t.Fatalf("nada executa depois do cancelamento: %v", e.run.Calls())
	}
}

func TestRunnerOrdersAppsThenRecipesWithRebootLast(t *testing.T) {
	e := newTUIEnv(t)
	r := e.runner("")
	items, problems := r.buildQueue(context.Background(), tui.Selection{
		Apps:    []tui.Mark{gitMark()},
		Recipes: []tui.RecipeMark{{ID: "com-reboot", Name: "Com reboot"}, {ID: "simples", Name: "Receita simples"}, {ID: "diag", Name: "Diagnóstico", Diagnostic: true}},
	})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	var names []string
	for _, it := range items {
		names = append(names, it.name)
	}
	if strings.Join(names, "|") != "Git|Receita simples|Diagnóstico|Com reboot" {
		t.Fatalf("ordem: %v", names)
	}
}

func TestRunnerRecipeRunsThroughTheEngine(t *testing.T) {
	e := newTUIEnv(t)
	e.run.On(sh("chk-a"), exectest.Exit(1, "")).On(sh("do-a"), exectest.Exit(0, "feito a")).On(sh("do-b"), exectest.Exit(0, "feito b"))
	evs := runAll(context.Background(), t, e.runner(""), e.approve(tui.Selection{Recipes: []tui.RecipeMark{{ID: "simples", Name: "Receita simples"}}}))
	if statuses(evs) != "ok" {
		t.Fatalf("estado %s\n%s", statuses(evs), logText(evs))
	}
	if !strings.Contains(logText(evs), "etapa a: done") || !strings.Contains(logText(evs), "etapa b: done") {
		t.Fatalf("log:\n%s", logText(evs))
	}
}

// Reinício: a engine pergunta pelo prompter da TUI (evento com Reply cap 1); "não" mantém a retomada e o item
// fica "aguardando reinício", e a fila para.
func TestRunnerRebootQuestionGoesThroughEventsAndDeclineKeepsResume(t *testing.T) {
	e := newTUIEnv(t)
	// simples roda primeiro (chk-a, do-a, do-b); com-reboot por último (do-a e depois a pergunta de reinício)
	e.run.On(sh("chk-a"), exectest.Exit(1, "")).On(sh("do-a"), exectest.Exit(0, ""), exectest.Exit(0, "")).On(sh("do-b"), exectest.Exit(0, ""))
	ch := make(chan tui.Event, 256)
	done := make(chan struct{})
	r := e.runner("")
	go func() {
		r.Run(context.Background(), e.approve(tui.Selection{Recipes: []tui.RecipeMark{{ID: "com-reboot", Name: "Com reboot"}, {ID: "simples", Name: "Receita simples"}}}), ch)
		close(done)
	}()
	var evs []tui.Event
	for ev := range ch {
		evs = append(evs, ev)
		if ev.Kind == tui.EventRebootPrompt {
			if cap(ev.Reply) != 1 {
				t.Fatalf("Reply deve ser bufferizado (cap 1): cap=%d", cap(ev.Reply))
			}
			if !strings.Contains(ev.Prompt, "com-reboot") && !strings.Contains(strings.ToLower(ev.Prompt), "reinici") {
				t.Fatalf("pergunta: %q", ev.Prompt)
			}
			ev.Reply <- false
		}
	}
	<-done
	if !strings.Contains(statuses(evs), "reboot") {
		t.Fatalf("estados %s", statuses(evs))
	}
	if !e.reg.Registered {
		t.Fatal("a retomada fica registrada quando o reinício é recusado")
	}
}

func TestEventPrompterNeverBlocksWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan tui.Event, 1)
	p := eventPrompter{send: func(ev tui.Event) bool { ch <- ev; return true }}
	res := make(chan bool, 1)
	go func() { v, _ := p.Confirm(ctx, "Reiniciar?"); res <- v }()
	ev := <-ch
	if ev.Kind != tui.EventRebootPrompt || cap(ev.Reply) != 1 {
		t.Fatalf("evento: %+v", ev)
	}
	cancel() // ninguém respondeu: a espera termina com o ctx
	select {
	case v := <-res:
		if v {
			t.Fatal("cancelado = não")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("o Confirm travou depois do cancelamento")
	}
	// resposta com leitor ausente também não bloqueia quem responde (buffer)
	ev.Reply <- true
}

func TestFileLogHasOnlyLifecycleAndWarnsWhenItCannotWrite(t *testing.T) {
	e := newTUIEnv(t)
	dir := filepath.Join(t.TempDir(), "logs")
	e.run.On(statusGit, notInstalled(), notInstalled()).
		OnElevated("apt-get update", exectest.Exit(0, "")).
		OnElevated("apt-get install -y --no-remove git", exectest.Exit(0, "SAIDA-SECRETA-DO-COMANDO"))
	runAll(context.Background(), t, e.runner(dir), e.approve(tui.Selection{Apps: []tui.Mark{gitMark()}}))
	files, _ := filepath.Glob(filepath.Join(dir, "tui-*.log"))
	if len(files) != 1 {
		t.Fatalf("um log por execução: %v", files)
	}
	fi, _ := os.Stat(files[0])
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("modo %v", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(files[0])
	if !strings.Contains(string(b), "item 1 (Git): início") || !strings.Contains(string(b), "item 1 (Git): ok") || strings.Contains(string(b), "SECRETA") {
		t.Fatalf("o log tem só o ciclo de vida:\n%s", b)
	}

	// pasta impossível (um ARQUIVO no lugar): aviso, e a execução continua
	blocker := filepath.Join(t.TempDir(), "arquivo")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	e2 := newTUIEnv(t)
	e2.run.On(statusGit, exectest.Exit(0, "installed\t1:2.43.0-1\n"), exectest.Exit(0, "installed\t1:2.43.0-1\n"))
	evs := runAll(context.Background(), t, e2.runner(filepath.Join(blocker, "logs")), e2.approve(tui.Selection{Apps: []tui.Mark{gitMark()}}))
	var warned bool
	for _, ev := range evs {
		if ev.Kind == tui.EventWarn && strings.Contains(ev.Line, "Não deu para gravar o log em arquivo") {
			warned = true
		}
	}
	if !warned || statuses(evs) != "ok" {
		t.Fatalf("aviso=%v estados=%s", warned, statuses(evs))
	}
}

func TestLineWriterSplitsLinesAndFlushesTheRest(t *testing.T) {
	var got []string
	w := &lineWriter{emit: func(s string) { got = append(got, s) }}
	_, _ = w.Write([]byte("a\nb"))
	_, _ = w.Write([]byte("c\r50%\n\nfim"))
	w.Flush()
	if strings.Join(got, "|") != "a|bc\r50%||fim" {
		t.Fatalf("linhas: %q", got)
	}
}

// ---- Crivo no #41 ----

func hasSudo(lines []string) bool { return containsSub(lines, "sudo -n --") }

// (1) O plano das receitas usa a MESMA regra de elevação da engine: sem requires_admin nada vai com sudo; com
// requires_admin o check E o run vão, e o aviso de sudo acende.
func TestPlannerRecipeElevationMatchesTheEngine(t *testing.T) {
	e := newTUIEnv(t)
	e.info.IsAdmin = false
	e.fs = fstest.MapFS{
		"recipes/linux/livre.yaml": {Data: []byte("id: livre\nname: Livre\nos: linux\nsteps:\n  - id: a\n    check: { sh: chk }\n    run: { sh: faz }\n")},
		"recipes/linux/admin.yaml": {Data: []byte("id: admin\nname: Admin\nos: linux\nrequires_admin: true\nsteps:\n  - id: a\n    check: { sh: chk }\n    run: { sh: faz }\n")},
	}
	plan, err := e.planner().Plan(context.Background(), tui.Selection{Recipes: []tui.RecipeMark{{ID: "livre", Name: "Livre"}}})
	if err != nil {
		t.Fatal(err)
	}
	if hasSudo(plan.Commands) || contains(plan.Warnings, msg.WarnPlanSudo) {
		t.Fatalf("receita sem requires_admin NÃO roda com sudo (a engine usa Elevate=false):\n%v\n%v", plan.Commands, plan.Warnings)
	}
	if !containsSub(plan.Commands, "[livre 1/1] verificar: /bin/sh -c chk") || !containsSub(plan.Commands, "[livre 1/1] /bin/sh -c faz") {
		t.Fatalf("comandos: %v", plan.Commands)
	}
	plan, _ = e.planner().Plan(context.Background(), tui.Selection{Recipes: []tui.RecipeMark{{ID: "admin", Name: "Admin"}}})
	if !containsSub(plan.Commands, "verificar: sudo -n -- /bin/sh -c chk") || !containsSub(plan.Commands, "] sudo -n -- /bin/sh -c faz") || !contains(plan.Warnings, msg.WarnPlanSudo) {
		t.Fatalf("com requires_admin o check e o run vão com sudo e o aviso acende:\n%v\n%v", plan.Commands, plan.Warnings)
	}
	// e a engine de verdade executa exatamente isso: o comando do plano é o que roda
	e.info.IsAdmin = false
	e.run.OnElevated("/bin/sh -c chk", exectest.Exit(1, "")).OnElevated("/bin/sh -c faz", exectest.Exit(0, ""))
	e.run2(t, "admin")
}

// run2 roda a receita e confere que TODA chamada elevada do runner é a que o plano mostrou.
func (e *tuiEnv) run2(t *testing.T, id string) {
	t.Helper()
	sel := e.approve(tui.Selection{Recipes: []tui.RecipeMark{{ID: id, Name: id}}})
	evs := runAll(context.Background(), t, e.runner(""), sel)
	if statuses(evs) != "ok" {
		t.Fatalf("estado %s\n%s", statuses(evs), logText(evs))
	}
	for _, c := range e.run.Calls() {
		if strings.Contains(c.String(), "/bin/sh -c") && !c.Elevate {
			t.Fatalf("a engine rodou sem Elevate o que o plano mostrou com sudo: %s", c)
		}
	}
}

// (1b) Passo `install:` de receita passa pelo MESMO passe do install e mostra os comandos reais (com sudo).
func TestPlannerRecipeInstallStepShowsRealCommands(t *testing.T) {
	e := newTUIEnv(t)
	e.info.IsAdmin = false
	e.fs = fstest.MapFS{"recipes/linux/apps.yaml": {Data: []byte(recInstall)}}
	e.run.On(statusGit, notInstalled())
	plan, err := e.planner().Plan(context.Background(), tui.Selection{Recipes: []tui.RecipeMark{{ID: "apps", Name: "Apps"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSub(plan.Commands, "[apps 1/1] sudo -n -- env DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get install -y --no-remove git") {
		t.Fatalf("o passo install: mostra o comando real, não só os ids:\n%v", plan.Commands)
	}
	if containsSub(plan.Commands, "instalar pelo catálogo") || !contains(plan.Warnings, msg.WarnPlanSudo) {
		t.Fatalf("comandos %v / avisos %v", plan.Commands, plan.Warnings)
	}
	if !contains(plan.Approved, "sudo -n -- env DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get install -y --no-remove git") {
		t.Fatalf("o comando do passo install: entra na lista aprovada: %v", plan.Approved)
	}
	onlyReadsRunner(t, e.run)
}

// (3) O executor só aceita o que estava no plano aprovado.
func TestRunnerRefusesCommandsOutsideTheApprovedPlan(t *testing.T) {
	e := newTUIEnv(t)
	e.run.On(statusGit, notInstalled())
	// sem plano aprovado (Approved vazio): nenhuma mutação roda, falha fechado
	evs := runAll(context.Background(), t, e.runner(""), tui.Selection{Apps: []tui.Mark{gitMark()}})
	if statuses(evs) != "failed" {
		t.Fatalf("estado %s", statuses(evs))
	}
	var failed tui.Event
	for _, ev := range evs {
		if ev.Kind == tui.EventItemDone {
			failed = ev
		}
	}
	if !strings.Contains(failed.Err, "mudou desde a Revisão") {
		t.Fatalf("erro: %q", failed.Err)
	}
	for _, c := range e.run.Calls() {
		if c.Elevate || !c.ReadOnly {
			t.Fatalf("executou mutação sem plano aprovado: %s", c)
		}
	}
}

func TestRunnerRefusesAChangedCommandButRunsTheApprovedOnes(t *testing.T) {
	e := newTUIEnv(t)
	e.run.On(statusGit, notInstalled(), notInstalled()).
		OnElevated("apt-get update", exectest.Exit(0, "")).
		OnElevated("apt-get install -y --no-remove git", exectest.Exit(0, ""))
	sel := e.approve(tui.Selection{Apps: []tui.Mark{gitMark()}})
	// o plano aprovado era do git; "mudou desde a Revisão": trocam o app por python (comando fora do plano)
	e.run.On(statusPy, notInstalled())
	bad := tui.Selection{Apps: []tui.Mark{{ID: "python", Name: "Python"}}, Approved: sel.Approved}
	evs := runAll(context.Background(), t, e.runner(""), bad)
	if statuses(evs) != "failed" {
		t.Fatalf("python não estava no plano: %s", statuses(evs))
	}
	for _, c := range e.run.Calls() {
		if strings.Contains(c.String(), "python3.13") && c.Elevate {
			t.Fatalf("o comando fora do plano rodou: %s", c)
		}
	}
	// o plano aprovado de verdade roda
	if got := statuses(runAll(context.Background(), t, e.runner(""), sel)); got != "ok" {
		t.Fatalf("o plano aprovado roda: %s", got)
	}
}

func TestRunnerRefusesRecipeStepOutsideTheApprovedPlan(t *testing.T) {
	e := newTUIEnv(t)
	e.run.On(sh("chk-a"), exectest.Exit(1, ""))
	sel := tui.Selection{Recipes: []tui.RecipeMark{{ID: "simples", Name: "Receita simples"}}} // sem Approved
	evs := runAll(context.Background(), t, e.runner(""), sel)
	if statuses(evs) != "failed" {
		t.Fatalf("estado %s\n%s", statuses(evs), logText(evs))
	}
	for _, c := range e.run.Calls() {
		if strings.Contains(c.String(), "do-a") {
			t.Fatalf("a etapa fora do plano aprovado rodou: %s", c)
		}
	}
}

// (2) O log em arquivo grava só status + classe do erro: stderr com URL/token fica só na tela.
func TestFileLogNeverGetsErrorTextWithSecrets(t *testing.T) {
	e := newTUIEnv(t)
	dir := filepath.Join(t.TempDir(), "logs")
	secretErr := "E: Falha em https://usuario:tok3n-SECRETO@evil.example/pool/git.deb Authorization: Bearer abc123"
	e.run.On(statusGit, notInstalled(), notInstalled()).
		OnElevated("apt-get update", exectest.Exit(0, "")).
		OnElevated("apt-get install -y --no-remove git", exectest.Reply{Result: exec.Result{ExitCode: 100, Stderr: []byte(secretErr)}})
	evs := runAll(context.Background(), t, e.runner(dir), e.approve(tui.Selection{Apps: []tui.Mark{gitMark()}}))
	var screen tui.Event
	for _, ev := range evs {
		if ev.Kind == tui.EventItemDone {
			screen = ev
		}
	}
	if screen.Status != tui.ItemFailed || screen.Err == "" {
		t.Fatalf("a tela recebe a falha com a mensagem: %+v", screen)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "tui-*.log"))
	if len(files) != 1 {
		t.Fatalf("log: %v", files)
	}
	b, _ := os.ReadFile(files[0])
	for _, leak := range []string{"tok3n", "SECRETO", "evil.example", "Bearer", "abc123", "https://", "Falha em"} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("o log em arquivo vazou %q:\n%s", leak, b)
		}
	}
	if !strings.Contains(string(b), "item 1 (Git): failed [error]") {
		t.Fatalf("o log tem status + classe:\n%s", b)
	}
}

func TestErrClass(t *testing.T) {
	cases := map[string]error{
		"needs-admin":         providers.ErrNeedsAdmin,
		"not-found":           fmt.Errorf("x: %w", providers.ErrNotFound),
		"hash-mismatch":       providers.ErrHashMismatch,
		"not-approved":        fmt.Errorf("%w: cmd", errNotApproved),
		"cancelled":           context.Canceled,
		"timeout":             context.DeadlineExceeded,
		"not-elevated":        exec.ErrNotElevated,
		"error":               errors.New("qualquer coisa com https://u:tok@h"),
		"version-unsupported": providers.ErrVersionUnsupported,
	}
	for want, err := range cases {
		if got := errClass(err); got != want {
			t.Errorf("errClass(%v) = %q, quero %q", err, got, want)
		}
	}
	if errClass(nil) != "" {
		t.Error("nil = vazio")
	}
}

// (5) Windows sem terminal elevado: receita requires_admin é Problem no plano (Executar bloqueado na Revisão).
func TestPlannerBlocksRequiresAdminRecipeOnWindowsWithoutElevation(t *testing.T) {
	e := newTUIEnv(t)
	e.info = platform.Info{OS: platform.Windows, Arch: "amd64", User: "ana", IsAdmin: false}
	e.fs = fstest.MapFS{"recipes/windows/w.yaml": {Data: []byte("id: w\nname: W\nos: windows\nrequires_admin: true\nsteps:\n  - id: a\n    run: { powershell: Get-Date }\n")}}
	d := e.deps()
	d.GOOS = "windows"
	plan, err := (&tuiPlanner{deps: d, rd: e.rd()}).Plan(context.Background(), tui.Selection{Recipes: []tui.RecipeMark{{ID: "w", Name: "W"}}})
	if err != nil || !containsSub(plan.Problems, "w") {
		t.Fatalf("err=%v problemas=%v", err, plan.Problems)
	}
}
