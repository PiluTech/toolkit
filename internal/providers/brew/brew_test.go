package brew

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
	"github.com/PiluVitu/toolkit/internal/providers"
)

func newP(f *exectest.FakeRunner) *Provider {
	p := New(f)
	p.Euid = func() int { return 501 }
	return p
}

func formula(n string) providers.PackageRef {
	return providers.PackageRef{Provider: "brew", Formula: n}
}
func cask(n string) providers.PackageRef { return providers.PackageRef{Provider: "brew", Cask: n} }

func hasEnv(c exec.Cmd, want ...string) bool {
	for _, w := range want {
		found := false
		for _, e := range c.Env {
			found = found || e == w
		}
		if !found {
			return false
		}
	}
	return true
}

func TestAvailable(t *testing.T) {
	if newP(exectest.New(t)).Available(context.Background()) {
		t.Error("sem brew")
	}
	if !newP(exectest.New(t).WithPath("brew", "/opt/homebrew/bin/brew")).Available(context.Background()) {
		t.Error("com brew")
	}
}

func TestValidate(t *testing.T) {
	p := newP(exectest.New(t))
	for _, r := range []providers.PackageRef{formula("git"), cask("visual-studio-code"), formula("python@3.12"), formula("user/tap/foo")} {
		if err := p.Validate(r); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	bad := []providers.PackageRef{
		{Provider: "brew"},
		{Provider: "brew", Formula: "git", Cask: "git"},
		{Provider: "brew", Formula: "--HEAD"},
		{Provider: "brew", Cask: "-f"},
		{Provider: "brew", Formula: "a b"},
		{Provider: "brew", Formula: "git", Package: "git"},
		{Provider: "brew", Formula: "git", Args: []string{"--force"}},
		{Provider: "brew", Formula: "git", Source: "x"},
	}
	for _, r := range bad {
		if err := p.Validate(r); !errors.Is(err, providers.ErrInvalidRef) {
			t.Errorf("%+v: err = %v", r, err)
		}
	}
}

func TestIsInstalled(t *testing.T) {
	f := exectest.New(t).
		On("brew list --versions --formula git", exectest.Exit(0, "git 2.46.0 2.47.1\n")).
		On("brew list --versions --cask vlc", exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte("Error: Cask 'vlc' is not installed.\n")}}).
		On("brew list --versions --formula wget", exectest.Exit(0, ""))
	p := newP(f)
	ok, v, err := p.IsInstalled(context.Background(), formula("git"))
	if err != nil || !ok || v != "2.47.1" {
		t.Errorf("git: ok=%v v=%q err=%v", ok, v, err)
	}
	if ok, _, err := p.IsInstalled(context.Background(), cask("vlc")); ok || err != nil {
		t.Errorf("vlc: ok=%v err=%v", ok, err)
	}
	if ok, _, _ := p.IsInstalled(context.Background(), formula("wget")); ok {
		t.Error("saída vazia = não instalado")
	}
	for _, c := range f.Calls() {
		if !c.ReadOnly || c.Elevate || !hasEnv(c, "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ENV_HINTS=1") {
			t.Errorf("leitura com marca errada: %+v", c)
		}
	}
}

func TestListVersions(t *testing.T) {
	f := exectest.New(t).
		On("brew info --json=v2 --formula git", exectest.Exit(0, `{"formulae":[{"name":"git"}],"casks":[]}`)).
		On("brew info --json=v2 --cask nada", exectest.Exit(1, "")).
		On("brew info --json=v2 --formula vazio", exectest.Exit(0, `{"formulae":[],"casks":[]}`)).
		On("brew info --json=v2 --formula lixo", exectest.Exit(0, `não é json`))
	p := newP(f)
	vs, err := p.ListVersions(context.Background(), formula("git"))
	if err != nil || len(vs) != 1 || vs[0] != "latest" {
		t.Errorf("vs=%v err=%v", vs, err)
	}
	if _, err := p.ListVersions(context.Background(), cask("nada")); !errors.Is(err, providers.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	if _, err := p.ListVersions(context.Background(), formula("vazio")); !errors.Is(err, providers.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	if _, err := p.ListVersions(context.Background(), formula("lixo")); err == nil {
		t.Error("JSON inválido devia falhar")
	}
}

func TestSupportsVersionSempreRecusa(t *testing.T) {
	err := newP(exectest.New(t)).SupportsVersion(formula("git"), "2.0")
	if !errors.Is(err, providers.ErrVersionUnsupported) {
		t.Errorf("err = %v", err)
	}
}

func TestInstallFormulaECask(t *testing.T) {
	f := exectest.New(t).
		On("brew install --formula git", exectest.Exit(0, "")).
		On("brew install --cask vlc", exectest.Exit(0, ""))
	p := newP(f)
	for _, r := range []providers.PackageRef{formula("git"), cask("vlc")} {
		if res, err := p.Install(context.Background(), r, ""); err != nil || res.AlreadyInstalled {
			t.Fatalf("%+v: res=%+v err=%v", r, res, err)
		}
	}
	for _, c := range f.Calls() {
		if c.ReadOnly || c.Elevate || !hasEnv(c, "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ENV_HINTS=1") {
			t.Errorf("install: nunca ReadOnly nem sudo, sempre com o Env: %+v", c)
		}
	}
}

func TestInstallVersaoFixadaNaoInstalaALatest(t *testing.T) {
	f := exectest.New(t) // qualquer comando falha o teste
	p := newP(f)
	if _, err := p.Install(context.Background(), formula("git"), "2.0"); !errors.Is(err, providers.ErrVersionUnsupported) {
		t.Errorf("err = %v", err)
	}
	if _, err := p.Install(context.Background(), formula("git"), "1 2; rm"); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("versão hostil: %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Error("nada executado")
	}
	f = exectest.New(t).On("brew install --formula git", exectest.Exit(0, ""))
	if _, err := newP(f).Install(context.Background(), formula("git"), "latest"); err != nil {
		t.Error(err)
	}
}

func TestInstallErros(t *testing.T) {
	f := exectest.New(t).
		On("brew install --formula nada", exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte("Error: No available formula with the name \"nada\".\n")}}).
		On("brew install --formula quebra", exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte("\x1b[31mErro feio\n")}})
	p := newP(f)
	if _, err := p.Install(context.Background(), formula("nada"), ""); !errors.Is(err, providers.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	_, err := p.Install(context.Background(), formula("quebra"), "")
	if err == nil || strings.ContainsRune(err.Error(), 0x1b) || !strings.Contains(err.Error(), "Erro feio") {
		t.Errorf("err = %v", err)
	}
}

func TestPrepareUmUnicoUpdate(t *testing.T) {
	f := exectest.New(t).On("brew update", exectest.Exit(0, ""))
	p := newP(f)
	for i := 0; i < 3; i++ {
		if err := p.Prepare(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("brew update rodou %d vezes", len(calls))
	}
	if calls[0].ReadOnly || calls[0].Elevate || !hasEnv(calls[0], "HOMEBREW_NO_ENV_HINTS=1") {
		t.Errorf("update é mutação sem sudo: %+v", calls[0])
	}
}

func TestPrepareFalhaFicaMemorizada(t *testing.T) {
	f := exectest.New(t).On("brew update", exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte("sem rede\n")}})
	p := newP(f)
	e1 := p.Prepare(context.Background())
	e2 := p.Prepare(context.Background())
	if e1 == nil || e2 == nil || len(f.Calls()) != 1 {
		t.Errorf("e1=%v e2=%v chamadas=%d", e1, e2, len(f.Calls()))
	}
}

func TestRecusaRoot(t *testing.T) {
	f := exectest.New(t)
	p := New(f)
	p.Euid = func() int { return 0 }
	ctx := context.Background()
	if _, _, err := p.IsInstalled(ctx, formula("git")); !errors.Is(err, ErrRoot) {
		t.Errorf("IsInstalled: %v", err)
	}
	if _, err := p.ListVersions(ctx, formula("git")); !errors.Is(err, ErrRoot) {
		t.Errorf("ListVersions: %v", err)
	}
	if err := p.Prepare(ctx); !errors.Is(err, ErrRoot) {
		t.Errorf("Prepare: %v", err)
	}
	if _, err := p.Install(ctx, formula("git"), ""); !errors.Is(err, ErrRoot) {
		t.Errorf("Install: %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Error("como root nada é executado")
	}
}

var (
	_ providers.Provider = (*Provider)(nil)
	_ providers.Preparer = (*Provider)(nil)
)

// IMPORTANTE do Crivo: sem Validate, "brew list --versions --formula -rf" rodaria (o brew não tem "--").
func TestIsInstalledEListVersionsValidam(t *testing.T) {
	f := exectest.New(t)
	p := newP(f)
	if _, _, err := p.IsInstalled(context.Background(), formula("-rf")); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("IsInstalled: %v", err)
	}
	if _, err := p.ListVersions(context.Background(), cask("--force")); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("ListVersions: %v", err)
	}
	if _, _, err := p.IsInstalled(context.Background(), providers.PackageRef{Provider: "brew"}); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("sem formula nem cask: %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Errorf("nada executado: %v", f.Calls())
	}
}

func TestIsInstalledDiferenciaNaoInstaladoDeFalha(t *testing.T) {
	for _, msg := range []string{"Error: No such keg: /opt/homebrew/Cellar/git\n", "Error: No available formula with the name \"git\"\n", ""} {
		f := exectest.New(t).On("brew list --versions --formula git", exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte(msg)}})
		if ok, _, err := newP(f).IsInstalled(context.Background(), formula("git")); ok || err != nil {
			t.Errorf("%q: ok=%v err=%v", msg, ok, err)
		}
	}
	f := exectest.New(t).On("brew list --versions --formula git", exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte("Error: Cellar corrompido\n")}})
	if _, _, err := newP(f).IsInstalled(context.Background(), formula("git")); err == nil || !strings.Contains(err.Error(), "Cellar corrompido") {
		t.Errorf("falha do brew devia ser erro: %v", err)
	}
}

func TestInstallCaskQuePedeSenhaViraErrNeedsAdmin(t *testing.T) {
	for _, msg := range []string{"sudo: a terminal is required to read the password\n", "sudo: a password is required\n"} {
		f := exectest.New(t).On("brew install --cask zoom", exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte(msg)}})
		_, err := newP(f).Install(context.Background(), cask("zoom"), "")
		if !errors.Is(err, providers.ErrNeedsAdmin) {
			t.Errorf("%q: err = %v", msg, err)
		}
	}
}

func TestOutroSudoNaoViraErrNeedsAdmin(t *testing.T) {
	for _, msg := range []string{"sudo: command not found\n", "Error: o pacote falhou (veja sudo: a password is required na doc)\n"} {
		f := exectest.New(t).On("brew install --cask zoom", exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte(msg)}})
		_, err := newP(f).Install(context.Background(), cask("zoom"), "")
		if err == nil || errors.Is(err, providers.ErrNeedsAdmin) {
			t.Errorf("%q: err = %v (não é pedido de senha)", msg, err)
		}
	}
	f := exectest.New(t).On("brew install --cask zoom", exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte("==> Installing Cask zoom\nsudo: a terminal is required to read the password\n")}})
	if _, err := newP(f).Install(context.Background(), cask("zoom"), ""); !errors.Is(err, providers.ErrNeedsAdmin) {
		t.Errorf("linha própria: %v", err)
	}
}

func TestTodoComandoTemLCALLC(t *testing.T) {
	r := formula("git")
	for name, c := range map[string]exec.Cmd{"list": listCmd(r), "info": infoCmd(r), "update": updateCmd(), "install": installCmd(r)} {
		if !hasEnv(c, "LC_ALL=C") {
			t.Errorf("%s sem LC_ALL=C", name)
		}
	}
}
