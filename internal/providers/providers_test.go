package providers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluVitu/toolkit/internal/catalog"
)

type fakeProvider struct {
	name      string
	available bool
	validate  error
	installed bool
	have      string
	isErr     error
	installEr error
	noVersion bool
	calls     []string
	prepared  int
}

func (f *fakeProvider) Name() string                   { return f.name }
func (f *fakeProvider) Available(context.Context) bool { return f.available }
func (f *fakeProvider) Validate(PackageRef) error      { return f.validate }
func (f *fakeProvider) IsInstalled(context.Context, PackageRef) (bool, string, error) {
	f.calls = append(f.calls, "is")
	return f.installed, f.have, f.isErr
}
func (f *fakeProvider) ListVersions(context.Context, PackageRef) ([]string, error) {
	return []string{Latest}, nil
}
func (f *fakeProvider) SupportsVersion(PackageRef, string) error {
	f.calls = append(f.calls, "supports")
	if f.noVersion {
		return ErrVersionUnsupported
	}
	return nil
}
func (f *fakeProvider) Install(_ context.Context, _ PackageRef, v string) (InstallResult, error) {
	f.calls = append(f.calls, "install:"+v)
	if f.installEr != nil {
		return InstallResult{}, f.installEr
	}
	return InstallResult{Version: "1.0"}, nil
}
func (f *fakeProvider) Prepare(context.Context) error {
	f.calls = append(f.calls, "prepare")
	f.prepared++
	return nil
}

func testApp(t *testing.T) catalog.App {
	t.Helper()
	src := `apps:
  py:
    name: Py
    category: dev
    versions: ["3.13", "3.12"]
    linux:
      - { provider: flatpak, id: org.example.Py }
      - { provider: apt, package: "python{{major_minor}}" }
      - { provider: pacman, package: python }
      - { provider: dnf, package: python }
`
	c, err := catalog.Parse("t.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Get("py")
	return a
}

func TestCheckName(t *testing.T) {
	ok := []string{"git", "Git.Git", "python3.13", "p7zip-full", "g++", "org.mozilla.firefox", "user/tap/foo", "python@3.12"}
	bad := []string{"", "-rf", "--version", "a b", "a;b", "a$(x)", "../x", "a..b", "a\nb", "é", strings.Repeat("a", 201)}
	for _, s := range ok {
		if err := CheckName("package", s); err != nil {
			t.Errorf("CheckName(%q) = %v, queria ok", s, err)
		}
	}
	for _, s := range bad {
		if err := CheckName("package", s); !errors.Is(err, ErrInvalidRef) {
			t.Errorf("CheckName(%q) = %v, queria ErrInvalidRef", s, err)
		}
	}
}

func TestValidVersionAceitaEpocaDoDebian(t *testing.T) {
	for _, v := range []string{"1:2.39.2-1", "2.39.2-1ubuntu1", "1.0~rc1", "3.13"} {
		if !ValidVersion(v) {
			t.Errorf("ValidVersion(%q) = false", v)
		}
	}
	for _, v := range []string{"", "-1", ":1", "1 2", "1;2", "1..2", "$(x)"} {
		if ValidVersion(v) {
			t.Errorf("ValidVersion(%q) = true", v)
		}
	}
}

func TestOnlyFields(t *testing.T) {
	ref := PackageRef{Provider: "apt", Package: "git", URL: "https://x.com/a"}
	if err := OnlyFields(ref, "package"); !errors.Is(err, ErrInvalidRef) {
		t.Errorf("campo alheio devia falhar: %v", err)
	}
	if err := OnlyFields(PackageRef{Package: "git"}, "package", "source"); err != nil {
		t.Error(err)
	}
}

func TestRefFromExpandeTemplates(t *testing.T) {
	app := testApp(t)
	vars, err := app.VersionVars("3.12")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := RefFrom(app, app.Installers["linux"][1], vars)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Package != "python3.12" || ref.Provider != "apt" || ref.AppID != "py" {
		t.Errorf("ref = %+v", ref)
	}
	if _, err := RefFrom(app, app.Installers["linux"][1], map[string]string{}); err == nil {
		t.Error("variável sem valor devia falhar")
	}
}

func TestCandidatesPulaIndisponiveisEInvalidos(t *testing.T) {
	app := testApp(t)
	vars, _ := app.VersionVars("")
	flat := &fakeProvider{name: "flatpak", available: false}
	apt := &fakeProvider{name: "apt", available: true}
	pac := &fakeProvider{name: "pacman", available: true, validate: ErrInvalidRef}
	reg := NewRegistry(flat, apt, pac) // dnf não registrado
	cands, skips := reg.Candidates(context.Background(), app, "linux", vars)
	if len(cands) != 1 || cands[0].Provider.Name() != "apt" || cands[0].Ref.Package != "python3.13" {
		t.Fatalf("cands = %+v", cands)
	}
	if len(skips) != 3 {
		t.Fatalf("skips = %+v", skips)
	}
	err := NoInstallerError(app, "linux", skips)
	if !errors.Is(err, ErrNoApplicableInstaller) || !strings.Contains(err.Error(), "dnf: provider ainda não suportado") {
		t.Errorf("erro = %v", err)
	}
}

func TestCandidatesSemInstaladorParaOSO(t *testing.T) {
	app := testApp(t)
	cands, skips := NewRegistry().Candidates(context.Background(), app, "windows", nil)
	if len(cands) != 0 || len(skips) != 0 {
		t.Fatal("windows não tem instalador")
	}
	if err := NoInstallerError(app, "windows", skips); !errors.Is(err, ErrNoApplicableInstaller) {
		t.Error(err)
	}
}

func cand(p *fakeProvider) Candidate {
	return Candidate{Provider: p, Ref: PackageRef{Provider: p.name}}
}

func TestInstallAppJaInstaladoNaoReinstala(t *testing.T) {
	p := &fakeProvider{name: "apt", installed: true, have: "2.0"}
	out, err := InstallApp(context.Background(), []Candidate{cand(p)}, "")
	if err != nil || !out.Result.AlreadyInstalled || out.Result.Version != "2.0" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if strings.Join(p.calls, ",") != "is" || p.prepared != 0 {
		t.Errorf("calls=%v prepared=%d", p.calls, p.prepared)
	}
}

func TestInstallAppVersaoDiferenteReinstala(t *testing.T) {
	p := &fakeProvider{name: "apt", installed: true, have: "2.0"}
	out, err := InstallApp(context.Background(), []Candidate{cand(p)}, "3.0")
	if err != nil || out.Result.AlreadyInstalled {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if strings.Join(p.calls, ",") != "is,supports,prepare,install:3.0" || p.prepared != 1 {
		t.Errorf("calls=%v prepared=%d", p.calls, p.prepared)
	}
}

func TestInstallAppTentaOProximoEmNaoAplicavel(t *testing.T) {
	a := &fakeProvider{name: "winget", installEr: ErrNoApplicableInstaller}
	b := &fakeProvider{name: "brew"}
	out, err := InstallApp(context.Background(), []Candidate{cand(a), cand(b)}, "")
	if err != nil || out.Provider != "brew" || len(out.Tried) != 1 || out.Tried[0] != "winget" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestInstallAppErroFatalNaoTentaOProximo(t *testing.T) {
	for _, bad := range []error{ErrHashMismatch, ErrNeedsAdmin, ErrRetryable} {
		a := &fakeProvider{name: "winget", installEr: bad}
		b := &fakeProvider{name: "brew"}
		_, err := InstallApp(context.Background(), []Candidate{cand(a), cand(b)}, "")
		if !errors.Is(err, bad) || len(b.calls) != 0 {
			t.Errorf("%v: err=%v callsB=%v", bad, err, b.calls)
		}
	}
}

func TestInstallAppTodosNaoAplicaveis(t *testing.T) {
	a := &fakeProvider{name: "winget", installEr: ErrNotFound}
	_, err := InstallApp(context.Background(), []Candidate{cand(a)}, "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	if _, err := InstallApp(context.Background(), nil, ""); !errors.Is(err, ErrNoApplicableInstaller) {
		t.Errorf("sem candidatos: %v", err)
	}
}

func TestInstallAppErroDeLeituraEncerra(t *testing.T) {
	boom := errors.New("boom")
	a := &fakeProvider{name: "apt", isErr: boom}
	if _, err := InstallApp(context.Background(), []Candidate{cand(a)}, ""); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func TestInstallAppCtxCancelado(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &fakeProvider{name: "apt"}
	if _, err := InstallApp(ctx, []Candidate{cand(a)}, ""); !errors.Is(err, context.Canceled) || len(a.calls) != 0 {
		t.Errorf("err=%v calls=%v", err, a.calls)
	}
}

// BLOQUEANTE do Crivo: a versão é validada uma vez, no começo, sem chamar o provider.
func TestInstallAppValidaVersaoAntesDeQualquerMetodo(t *testing.T) {
	for _, v := range []string{"--force", "-1", "1;2", "1 2", "1..2", "$(x)", "1\n2", ":1"} {
		p := &fakeProvider{name: "apt"}
		_, err := InstallApp(context.Background(), []Candidate{cand(p)}, v)
		if !errors.Is(err, ErrInvalidRef) {
			t.Errorf("versão %q: err = %v", v, err)
		}
		if len(p.calls) != 0 || p.prepared != 0 {
			t.Errorf("versão %q: o provider foi chamado: %v", v, p.calls)
		}
	}
	// vazia, latest e época do Debian passam
	for _, v := range []string{"", Latest, "1:2.39.2-1", "3.13"} {
		p := &fakeProvider{name: "apt"}
		if _, err := InstallApp(context.Background(), []Candidate{cand(p)}, v); err != nil {
			t.Errorf("versão %q: %v", v, err)
		}
	}
}

// IMPORTANTE (2): versão não suportada é checada sem mutação e passa ao próximo candidato.
func TestInstallAppVersaoNaoSuportadaNaoPreparaEPassaAoProximo(t *testing.T) {
	a := &fakeProvider{name: "brew", noVersion: true}
	b := &fakeProvider{name: "apt"}
	out, err := InstallApp(context.Background(), []Candidate{cand(a), cand(b)}, "3.0")
	if err != nil || out.Provider != "apt" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if strings.Join(a.calls, ",") != "is,supports" || a.prepared != 0 {
		t.Errorf("brew: calls=%v (sem Prepare nem Install)", a.calls)
	}
	if len(out.Tried) != 1 || out.Tried[0] != "brew" {
		t.Errorf("tried = %v", out.Tried)
	}
}

func TestInstallAppTodosRecusamAVersao(t *testing.T) {
	a := &fakeProvider{name: "brew", noVersion: true}
	b := &fakeProvider{name: "pacman", noVersion: true}
	_, err := InstallApp(context.Background(), []Candidate{cand(a), cand(b)}, "3.0")
	if !errors.Is(err, ErrVersionUnsupported) || !strings.Contains(err.Error(), "pacman") {
		t.Errorf("err = %v", err)
	}
	if a.prepared+b.prepared != 0 {
		t.Error("nenhum Prepare quando todos recusam")
	}
}

func TestInstallAppSemVersaoNaoChamaSupportsVersion(t *testing.T) {
	p := &fakeProvider{name: "brew", noVersion: true}
	if _, err := InstallApp(context.Background(), []Candidate{cand(p)}, Latest); err != nil {
		t.Fatal(err)
	}
	for _, c := range p.calls {
		if c == "supports" {
			t.Error("latest não consulta SupportsVersion")
		}
	}
}

// MENOR: a época do Debian não faz a versão instalada parecer diferente.
func TestInstallAppEpocaDoDebianNaoReinstala(t *testing.T) {
	for _, want := range []string{"2.45", "2.45-1", "1:2.45-1"} {
		p := &fakeProvider{name: "apt", installed: true, have: "1:2.45-1"}
		out, err := InstallApp(context.Background(), []Candidate{cand(p)}, want)
		if err != nil || !out.Result.AlreadyInstalled {
			t.Errorf("want %q: out=%+v err=%v", want, out, err)
		}
	}
	p := &fakeProvider{name: "apt", installed: true, have: "1:2.45-1"}
	out, _ := InstallApp(context.Background(), []Candidate{cand(p)}, "2.4")
	if out.Result.AlreadyInstalled {
		t.Error("2.4 não é 2.45")
	}
}

func TestSameVersionENormalize(t *testing.T) {
	if NormalizeVersion("1:2.45-1") != "2.45-1" || NormalizeVersion("2.45") != "2.45" || NormalizeVersion("a:1") != "a:1" {
		t.Error("NormalizeVersion")
	}
	if SameVersion("2.45.1-1", "2.45") {
		t.Error("2.45.1 não casa com 2.45")
	}
	if !SameVersion("2.45-1", "2.45") || SameVersion("2.45", "2.45-1") {
		t.Error("revisão só casa quando o pedido não a traz")
	}
}

// Args do catálogo só existem para url/github-release (fase 5); os providers de gerenciador os recusam.
func TestOnlyFieldsRecusaArgs(t *testing.T) {
	if err := OnlyFields(PackageRef{Provider: "apt", Package: "git", Args: []string{"--force"}}, "package"); !errors.Is(err, ErrInvalidRef) {
		t.Errorf("err = %v", err)
	}
}

func TestSnippetLimpaControleELimita(t *testing.T) {
	if got := Snippet([]byte("\n  \n\x1b[2Jerro\x07 feio\r\n")); got != "[2Jerro feio" {
		t.Errorf("got %q", got)
	}
	if got := Snippet([]byte("\r   - \rerro de verdade\n")); got != "erro de verdade" {
		t.Errorf("spinner: %q", got)
	}
	if got := Snippet([]byte(strings.Repeat("a", 300))); len([]rune(got)) != 201 {
		t.Errorf("len = %d", len([]rune(got)))
	}
	if Snippet(nil) != "" {
		t.Error("vazio")
	}
}

// Menor do Crivo no #9: época no PEDIDO é comparada.
func TestSameVersionComparaEpocaDoPedido(t *testing.T) {
	cases := []struct {
		have, want string
		same       bool
	}{
		{"1:2.45-1", "3:2.45", false},
		{"1:2.45-1", "1:2.45", true},
		{"1:2.45-1", "1:2.45-1", true},
		{"2.45-1", "0:2.45", true},
		{"2.45-1", "1:2.45", false},
		{"1:2.45-1", "2.45", true},
		{"1:2.45-1", "0:2.45", false},
	}
	for _, c := range cases {
		if got := SameVersion(c.have, c.want); got != c.same {
			t.Errorf("SameVersion(%q, %q) = %v, queria %v", c.have, c.want, got, c.same)
		}
	}
}
