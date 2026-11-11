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
func (f *fakeProvider) Install(_ context.Context, _ PackageRef, v string) (InstallResult, error) {
	f.calls = append(f.calls, "install:"+v)
	if f.installEr != nil {
		return InstallResult{}, f.installEr
	}
	return InstallResult{Version: "1.0"}, nil
}
func (f *fakeProvider) Prepare(context.Context) error { f.prepared++; return nil }

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
	if strings.Join(p.calls, ",") != "is,install:3.0" || p.prepared != 1 {
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
	for _, bad := range []error{ErrHashMismatch, ErrVersionUnsupported, ErrNeedsAdmin, ErrRetryable} {
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
