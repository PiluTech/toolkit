package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/tui"
)

func TestShouldOpenTUI(t *testing.T) {
	cases := []struct {
		name              string
		stdin, stdout, no bool
		term              string
		want              bool
	}{
		{"terminal completo", true, true, false, "xterm-256color", true},
		{"--no-tui", true, true, true, "xterm", false},
		{"sem terminal na entrada", false, true, false, "xterm", false},
		{"saída redirecionada", true, false, false, "xterm", false},
		{"TERM=dumb", true, true, false, "dumb", false},
		{"TERM=DUMB", true, true, false, "DUMB", false},
		{"TERM vazio", true, true, false, "", false},
		{"TERM só com espaços", true, true, false, "  ", false},
	}
	for _, c := range cases {
		if got := shouldOpenTUI("linux", c.stdin, c.stdout, c.no, c.term); got != c.want {
			t.Errorf("%s: %v, quero %v", c.name, got, c.want)
		}
	}
}

// rootRun executa `toolkit [args]` com terminais e TERM injetados e um tui.Run espião.
func rootRun(t *testing.T, stdin, stdout bool, term string, args ...string) (out string, opened bool, tabs []tui.Tab) {
	t.Helper()
	return rootRunOS(t, "linux", stdin, stdout, term, args...)
}

// rootRunOS é o rootRun com o SO escolhido (no Windows o TERM não existe).
func rootRunOS(t *testing.T, goos string, stdin, stdout bool, term string, args ...string) (out string, opened bool, tabs []tui.Tab) {
	t.Helper()
	var o, e bytes.Buffer
	deps := Deps{
		GOOS: goos, EmbeddedYML: []byte(embedded), Interactive: func() bool { return stdin },
		TUI: TUIDeps{
			StdoutIsTerminal: func() bool { return stdout },
			Term:             func() string { return term },
			Run: func(_ context.Context, _ tui.Options, ts ...tui.Tab) error {
				opened, tabs = true, ts
				return nil
			},
		},
	}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, e.String())
	}
	return o.String(), opened, tabs
}

func TestRootWithoutArgsOpensTUIOnlyWithTerminal(t *testing.T) {
	out, opened, tabs := rootRun(t, true, true, "xterm")
	if !opened || len(tabs) != 4 {
		t.Fatalf("deve abrir a TUI com 4 abas: opened=%v abas=%d (%s)", opened, len(tabs), out)
	}
	var titles []string
	for _, tb := range tabs {
		titles = append(titles, tb.Title())
	}
	if strings.Join(titles, ",") != "Apps,Receitas,Diagnóstico,Log" {
		t.Fatalf("abas: %v", titles)
	}
}

func TestRootWithoutArgsShowsHelpInsteadOfTUI(t *testing.T) {
	cases := map[string]struct {
		stdin, stdout bool
		term          string
		args          []string
	}{
		"--no-tui":                {true, true, "xterm", []string{"--no-tui"}},
		"sem terminal na entrada": {false, true, "xterm", nil},
		"saída redirecionada":     {true, false, "xterm", nil},
		"TERM=dumb":               {true, true, "dumb", nil},
	}
	for name, c := range cases {
		out, opened, _ := rootRun(t, c.stdin, c.stdout, c.term, c.args...)
		if opened {
			t.Errorf("%s: abriu a TUI", name)
		}
		if !strings.Contains(out, "Available Commands") && !strings.Contains(out, "install") {
			t.Errorf("%s: sem ajuda:\n%s", name, out)
		}
	}
}

// Subcomandos continuam funcionando, e argumento solto no `toolkit` ainda é erro (não abre a TUI).
func TestRootSubcommandsUnaffected(t *testing.T) {
	_, opened, _ := rootRun(t, true, true, "xterm", "version")
	if opened {
		t.Fatal("`toolkit version` não abre a TUI")
	}
	var o, e bytes.Buffer
	deps := Deps{GOOS: "linux", EmbeddedYML: []byte(embedded), Interactive: func() bool { return true },
		TUI: TUIDeps{StdoutIsTerminal: func() bool { return true }, Term: func() string { return "xterm" },
			Run: func(context.Context, tui.Options, ...tui.Tab) error { t.Fatal("não abre"); return nil }}}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"coisa-desconhecida"})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("comando desconhecido deve ser erro")
	}
}

func appsBE(f *exectest.FakeRunner, goos string) *appsBackend {
	return &appsBackend{deps: Deps{GOOS: goos, EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return f }, Interactive: func() bool { return false }}}
}

func TestAppsBackendLoad(t *testing.T) {
	apps, err := appsBE(aptFake(t), "linux").Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]tui.AppInfo{}
	for _, a := range apps {
		by[a.ID] = a
	}
	if g := by["git"]; g.Name != "Git" || g.Category != "dev" || !g.Versions || strings.Join(g.Managers, ",") != "apt,pacman" || g.NoInstaller {
		t.Fatalf("git: %+v", g)
	}
	if p := by["python"]; !p.Versions || strings.Join(p.Managers, ",") != "apt" {
		t.Fatalf("python: %+v", p)
	}
	if f := by["firefox"]; strings.Join(f.Managers, ",") != "flatpak" || f.Versions {
		t.Fatalf("firefox (any): %+v", f)
	}
	// SO sem instalador próprio: só o que tem `any`
	apps, _ = appsBE(aptFake(t), "plan9").Load(context.Background())
	for _, a := range apps {
		if (a.ID == "firefox") == a.NoInstaller {
			t.Fatalf("plan9: %+v", a)
		}
	}
}

func TestAppsBackendStatusReadsTheManager(t *testing.T) {
	f := aptFake(t).On(statusGit, exectest.Exit(0, "installed\t1:2.43.0-1\n"))
	st := appsBE(f, "linux").Status(context.Background(), "git")
	if st.Err != nil || !st.Installed || st.Version == "" {
		t.Fatalf("instalado: %+v", st)
	}
	f = aptFake(t).On(statusGit, notInstalled())
	st = appsBE(f, "linux").Status(context.Background(), "git")
	if st.Err != nil || st.Installed {
		t.Fatalf("não instalado não é erro: %+v", st)
	}
	for _, c := range f.Calls() {
		if !c.ReadOnly || c.Elevate {
			t.Fatalf("a aba Apps só lê: %s", c)
		}
	}
}

func TestAppsBackendStatusErrors(t *testing.T) {
	st := appsBE(aptFake(t), "linux").Status(context.Background(), "nao-existe")
	if st.Err == nil || !strings.Contains(st.Err.Error(), "fora do catálogo") {
		t.Fatalf("app desconhecido: %+v", st)
	}
	// sem nenhum gerenciador na máquina: erro (a aba mostra ✗), nunca pânico
	st = appsBE(exectest.New(t), "linux").Status(context.Background(), "git")
	if st.Err == nil {
		t.Fatalf("sem gerenciador deve ser erro: %+v", st)
	}
}

func recBE(t *testing.T, fsys fstest.MapFS) *recipesBackend {
	t.Helper()
	root := t.TempDir()
	return &recipesBackend{
		deps: Deps{GOOS: "linux", EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) }},
		rd: RecipeDeps{
			Detect: func(context.Context, exec.Runner) (platform.Info, error) {
				return platform.Info{OS: platform.Linux, User: "ana"}, nil
			},
			Paths: func(platform.Info) (platform.Paths, error) {
				return platform.Paths{StateDir: filepath.Join(root, "state"), LogDir: filepath.Join(root, "logs")}, nil
			},
			RecipesFS: fsys,
		},
	}
}

func TestRecipesBackendLoad(t *testing.T) {
	be := recBE(t, fstest.MapFS{
		"recipes/linux/a.yaml": {Data: []byte("id: a\nname: A\ndescription: Faz A.\nos: linux\nsteps:\n  - id: s1\n    name: Um\n    run: { sh: x }\n  - id: s2\n    name: Dois\n    reboot: required\n    run: { sh: y }\n")},
		"recipes/linux/d.yaml": {Data: []byte("id: d\nname: D\nos: linux\nkind: diagnostic\nsteps:\n  - id: s\n    run: { sh: z }\n")},
	})
	d, err := be.Load(context.Background())
	if err != nil || d.StateErr != nil {
		t.Fatalf("err=%v stateErr=%v", err, d.StateErr)
	}
	by := map[string]tui.RecipeInfo{}
	for _, r := range d.Recipes {
		by[r.ID] = r
	}
	if a := by["a"]; a.Name != "A" || a.Description != "Faz A." || a.Diagnostic || len(a.Steps) != 2 || a.Steps[1].Reboot != "required" {
		t.Fatalf("a: %+v", a)
	}
	if !by["d"].Diagnostic {
		t.Fatal("kind: diagnostic vira Diagnostic")
	}
	if len(d.Runs) != 0 || !strings.HasSuffix(d.StatePath, "state.json") {
		t.Fatalf("sem estado ainda: %+v %q", d.Runs, d.StatePath)
	}
}

func TestRecipesBackendCorruptStateIsOnlyAWarning(t *testing.T) {
	be := recBE(t, fstest.MapFS{"recipes/linux/a.yaml": {Data: []byte("id: a\nname: A\nos: linux\nsteps:\n  - id: s1\n    run: { sh: x }\n")}})
	d, err := be.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// agora estraga o state e carrega de novo: as receitas continuam, o erro vai em StateErr
	rt, _ := setupRecipes(context.Background(), be.deps, be.rd)
	if err := mkdirWrite(rt.store.Path(), "{ isto não é json"); err != nil {
		t.Fatal(err)
	}
	d, err = be.Load(context.Background())
	if err != nil || d.StateErr == nil || len(d.Recipes) != 1 {
		t.Fatalf("err=%v stateErr=%v receitas=%d", err, d.StateErr, len(d.Recipes))
	}
}

func TestRecipesBackendDetectErrorIsLoadError(t *testing.T) {
	be := recBE(t, fstest.MapFS{})
	boom := errors.New("sem SO")
	be.rd.Detect = func(context.Context, exec.Runner) (platform.Info, error) { return platform.Info{}, boom }
	if _, err := be.Load(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
}

func mkdirWrite(path, data string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(data), 0o600)
}

// Lupa no #40 (1): /dev/null é dispositivo de caractere mas NÃO é terminal; pipe e arquivo comum também não.
func TestFileIsTerminalRejectsNonTerminals(t *testing.T) {
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip("sem /dev/null")
	}
	defer func() { _ = devnull.Close() }()
	if fileIsTerminal(devnull) {
		t.Error("/dev/null não é terminal: a TUI não pode abrir com a saída em /dev/null")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	if fileIsTerminal(w) || fileIsTerminal(r) {
		t.Error("pipe não é terminal")
	}
	regular, err := os.CreateTemp(t.TempDir(), "saida")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = regular.Close() }()
	if fileIsTerminal(regular) {
		t.Error("arquivo comum não é terminal")
	}
	closed, _ := os.Open(os.DevNull)
	_ = closed.Close()
	if fileIsTerminal(closed) {
		t.Error("descritor fechado não é terminal")
	}
}

// Lupa no #40 (2): a lista de versões do catálogo (versions: [...]) é a que a aba oferece, sem consultar o
// gerenciador; só versions: true consulta o provider.
func TestAppsBackendVersionsUsesCatalogListWithoutProvider(t *testing.T) {
	f := aptFake(t) // qualquer comando ao provider falha o teste (sem roteiro)
	vs, err := appsBE(f, "linux").Versions(context.Background(), "python")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(vs, ",") != "3.13,3.12" {
		t.Fatalf("python deve oferecer 3.13 e 3.12 do catálogo, ofereceu %v", vs)
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("lista estática não consulta o provider: %v", f.Calls())
	}
}

func TestAppsBackendVersionsQueriesProviderOnlyForVersionsTrue(t *testing.T) {
	f := aptFake(t).On("apt-cache madison git", exectest.Exit(0, " git | 1:2.43.0-1ubuntu7 | http://archive.ubuntu.com/ubuntu noble/main amd64 Packages\n"))
	vs, err := appsBE(f, "linux").Versions(context.Background(), "git")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) == 0 {
		t.Fatal("git (versions: true) lista pelo gerenciador")
	}
	for _, c := range f.Calls() {
		if !c.ReadOnly || c.Elevate {
			t.Fatalf("só leitura: %s", c)
		}
	}
	// firefox não tem versões: nada a oferecer, nenhuma consulta
	f2 := aptFake(t)
	if vs, err := appsBE(f2, "linux").Versions(context.Background(), "firefox"); err != nil || len(vs) != 0 || len(f2.Calls()) != 0 {
		t.Fatalf("firefox: %v %v %v", vs, err, f2.Calls())
	}
	if _, err := appsBE(aptFake(t), "linux").Versions(context.Background(), "nao-existe"); err == nil {
		t.Fatal("app desconhecido é erro")
	}
}

// Lupa no #40 (3): o fluxo COMPLETO de `toolkit` sem argumentos, com o stdin real trocado: /dev/zero (dispositivo
// de caracteres que não é terminal), pipe, arquivo comum e /dev/null => ajuda, nunca a TUI.
func TestRootFullFlowWithRealStdinNeverOpensTUIWithoutTerminal(t *testing.T) {
	zero, err := os.Open("/dev/zero")
	if err != nil {
		t.Skip("sem /dev/zero")
	}
	defer func() { _ = zero.Close() }()
	devnull, _ := os.Open(os.DevNull)
	defer func() { _ = devnull.Close() }()
	pr, pw, _ := os.Pipe()
	defer func() { _ = pr.Close(); _ = pw.Close() }()
	regular, _ := os.CreateTemp(t.TempDir(), "entrada")
	defer func() { _ = regular.Close() }()

	for name, f := range map[string]*os.File{"/dev/zero": zero, "/dev/null": devnull, "pipe": pr, "arquivo": regular} {
		old := os.Stdin
		os.Stdin = f
		var o, e bytes.Buffer
		opened := false
		deps := Deps{ // Interactive NÃO é injetado: vale a detecção real do stdin
			GOOS: "linux", EmbeddedYML: []byte(embedded),
			TUI: TUIDeps{StdoutIsTerminal: func() bool { return true }, Term: func() string { return "xterm-256color" },
				Run: func(context.Context, tui.Options, ...tui.Tab) error { opened = true; return nil }},
		}
		cmd := NewRootCmdWith(&o, &e, deps)
		cmd.SetArgs(nil)
		err := cmd.ExecuteContext(context.Background())
		os.Stdin = old
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if opened {
			t.Errorf("stdin=%s: a TUI abriu sem terminal na entrada", name)
		}
		if !strings.Contains(o.String(), "install") {
			t.Errorf("stdin=%s: sem ajuda:\n%s", name, o.String())
		}
	}
}

// Uma função só decide "isto é um terminal" para a entrada e para a saída.
func TestStdinAndStdoutShareTheSameTerminalDetection(t *testing.T) {
	zero, err := os.Open("/dev/zero")
	if err != nil {
		t.Skip("sem /dev/zero")
	}
	defer func() { _ = zero.Close() }()
	old := os.Stdin
	os.Stdin = zero
	defer func() { os.Stdin = old }()
	if stdinIsTerminal() != fileIsTerminal(zero) || stdinIsTerminal() {
		t.Fatal("stdin=/dev/zero não é terminal, e stdin e saída usam a mesma detecção (fileIsTerminal)")
	}
}

// Bug "toolkit tui não abre no Windows": cmd, PowerShell e Windows Terminal NÃO definem TERM, e a TUI
// recusava abrir com TERM vazio (caía na ajuda em silêncio). No Windows o TERM não conta.
func TestRootOpensTUIOnWindowsWithoutTERM(t *testing.T) {
	for _, term := range []string{"", "  "} {
		_, opened, tabs := rootRunOS(t, "windows", true, true, term)
		if !opened || len(tabs) != 4 {
			t.Fatalf("no Windows com TERM=%q a TUI deve abrir: opened=%v abas=%d", term, opened, len(tabs))
		}
	}
}

// Fora do Windows o TERM vazio continua recusando; no Windows --no-tui, entrada ou saída redirecionada também.
func TestRootWindowsStillRespectsNoTUIAndRedirects(t *testing.T) {
	for name, c := range map[string]struct {
		stdin, stdout bool
		args          []string
	}{
		"--no-tui":            {true, true, []string{"--no-tui"}},
		"entrada sem console": {false, true, nil},
		"saída redirecionada": {true, false, nil},
	} {
		_, opened, _ := rootRunOS(t, "windows", c.stdin, c.stdout, "", c.args...)
		if opened {
			t.Errorf("%s: abriu a TUI", name)
		}
	}
	for _, goos := range []string{"linux", "darwin"} {
		if _, opened, _ := rootRunOS(t, goos, true, true, ""); opened {
			t.Errorf("%s com TERM vazio não abre a TUI", goos)
		}
	}
}

// `toolkit tui` abre a TUI de forma explícita (e diz por que não abriu, em vez de mostrar a ajuda).
func TestTuiSubcommandOpensTUI(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		term := ""
		if goos == "linux" {
			term = "xterm"
		}
		_, opened, tabs := rootRunOS(t, goos, true, true, term, "tui")
		if !opened || len(tabs) != 4 {
			t.Fatalf("%s: `toolkit tui` deve abrir a TUI: opened=%v abas=%d", goos, opened, len(tabs))
		}
	}
}

func TestTuiSubcommandWithoutTerminalExplainsWhy(t *testing.T) {
	for name, c := range map[string]struct {
		goos          string
		stdin, stdout bool
		term, want    string
	}{
		"windows sem console":   {"windows", false, true, "", "Windows Terminal"},
		"windows redirecionado": {"windows", true, false, "", "Windows Terminal"},
		"linux TERM=dumb":       {"linux", true, true, "dumb", "TERM"},
		"linux sem terminal":    {"linux", false, false, "xterm", "terminal"},
	} {
		var o, e bytes.Buffer
		deps := Deps{GOOS: c.goos, EmbeddedYML: []byte(embedded), Interactive: func() bool { return c.stdin },
			TUI: TUIDeps{StdoutIsTerminal: func() bool { return c.stdout }, Term: func() string { return c.term },
				Run: func(context.Context, tui.Options, ...tui.Tab) error {
					t.Errorf("%s: não deveria abrir", name)
					return nil
				}}}
		cmd := NewRootCmdWith(&o, &e, deps)
		cmd.SetArgs([]string{"tui"})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: erro %v, quero mencionar %q", name, err, c.want)
		}
	}
}

// Com as dependências REAIS (GOOS e detecção de console do próprio SO), `toolkit tui` sem console devolve o erro
// explicativo do SO, nunca a ajuda em silêncio. No runner do Windows isto exercita o caminho real do bug.
func TestTuiSubcommandRealDepsWithoutConsole(t *testing.T) {
	if fileIsTerminal(os.Stdin) {
		t.Skip("o teste roda com a entrada ligada a um terminal de verdade")
	}
	var o, e bytes.Buffer
	code := ExecuteWith(context.Background(), &o, &e, DefaultDeps(), []string{"tui", "--no-update-check"})
	if code == 0 {
		t.Fatalf("sem console `toolkit tui` deve falhar:\n%s%s", o.String(), e.String())
	}
	want := "terminal interativo"
	if runtime.GOOS == "windows" {
		want = "console interativo do Windows"
	}
	if !strings.Contains(e.String(), want) {
		t.Fatalf("erro sem %q:\n%s", want, e.String())
	}
}
