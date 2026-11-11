package providers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
	"github.com/PiluVitu/toolkit/internal/providers"
	"github.com/PiluVitu/toolkit/internal/providers/apt"
	"github.com/PiluVitu/toolkit/internal/providers/brew"
	"github.com/PiluVitu/toolkit/internal/providers/winget"
)

// IMPORTANTE do Crivo: "toolkit install python@3.12" — a versão já está no id pelo template, então não vai de
// novo ao provider (nada de --version no winget, =versão no apt ou ErrVersionUnsupported no brew).
func TestVersaoJaNoIdNaoVaiAoProvider(t *testing.T) {
	c, err := catalog.Parse("catalog.yaml", []byte(`apps:
  python:
    name: Python
    category: dev
    versions: ["3.13", "3.12"]
    windows: [{ provider: winget, id: "Python.Python.{{major_minor}}" }]
    macos:   [{ provider: brew, formula: "python@{{major_minor}}" }]
    linux:   [{ provider: apt, package: "python{{major_minor}}" }]
`))
	if err != nil {
		t.Fatal(err)
	}
	app, _ := c.Get("python")
	notFound := exec.Result{ExitCode: 1}
	cases := []struct {
		goos    string
		fake    func(*exectest.FakeRunner) *exectest.FakeRunner
		reg     func(*exectest.FakeRunner) providers.Provider
		wantCmd string
	}{
		{
			"darwin",
			func(f *exectest.FakeRunner) *exectest.FakeRunner {
				return f.WithPath("brew", "/opt/homebrew/bin/brew").
					On("brew list --versions --formula python@3.12", exectest.Reply{Result: notFound}).
					On("brew update", exectest.Exit(0, "")).
					On("brew install --formula python@3.12", exectest.Exit(0, ""))
			},
			func(f *exectest.FakeRunner) providers.Provider {
				p := brew.New(f)
				p.Euid = func() int { return 501 }
				return p
			},
			"brew install --formula python@3.12",
		},
		{
			"windows",
			func(f *exectest.FakeRunner) *exectest.FakeRunner {
				return f.WithPath("winget", `C:\winget.exe`).
					On("winget list --id Python.Python.3.12 -e --disable-interactivity", exectest.Exit(int(uint32(0x8A150014)), "")).
					On("winget install --id Python.Python.3.12 -e --source winget --silent --accept-package-agreements --accept-source-agreements --disable-interactivity", exectest.Exit(0, ""))
			},
			func(f *exectest.FakeRunner) providers.Provider { return winget.New(f) },
			"winget install --id Python.Python.3.12 -e --source winget --silent --accept-package-agreements --accept-source-agreements --disable-interactivity",
		},
		{
			"linux",
			func(f *exectest.FakeRunner) *exectest.FakeRunner {
				return f.WithPath("apt-get", "/usr/bin/apt-get").WithPath("apt-cache", "/usr/bin/apt-cache").WithPath("dpkg-query", "/usr/bin/dpkg-query").
					On(`dpkg-query -W "-f=${db:Status-Status}\t${Version}\n" python3.12`, exectest.Reply{Result: notFound}).
					OnElevated("apt-get update", exectest.Exit(0, "")).
					OnElevated("apt-get install -y --no-remove python3.12", exectest.Exit(0, ""))
			},
			func(f *exectest.FakeRunner) providers.Provider { return apt.New(f) },
			"apt-get install -y --no-remove python3.12",
		},
	}
	for _, tc := range cases {
		f := tc.fake(exectest.New(t))
		reg := providers.NewRegistry(tc.reg(f))
		vars, err := app.VersionVars("3.12")
		if err != nil {
			t.Fatal(err)
		}
		cands, skips := reg.Candidates(context.Background(), app, tc.goos, vars)
		if len(cands) != 1 {
			t.Fatalf("%s: cands=%d skips=%v", tc.goos, len(cands), skips)
		}
		if !cands[0].Ref.VersionInID {
			t.Errorf("%s: RefFrom devia marcar VersionInID", tc.goos)
		}
		// o install passa "3.12" (o que o usuário digitou): não pode chegar ao provider
		out, err := providers.InstallApp(context.Background(), cands, "3.12")
		if err != nil {
			t.Errorf("%s: %v", tc.goos, err)
			continue
		}
		if out.Result.AlreadyInstalled {
			t.Errorf("%s: não estava instalado", tc.goos)
		}
		var last exec.Cmd
		for _, c := range f.Calls() {
			if !c.ReadOnly {
				last = c
			}
		}
		if last.String() != tc.wantCmd {
			t.Errorf("%s: install = %q, queria %q", tc.goos, last.String(), tc.wantCmd)
		}
	}
}

func TestVersionInIDSoParaTemplateDeVersaoNoId(t *testing.T) {
	c, err := catalog.Parse("c.yaml", []byte(`apps:
  git:
    name: Git
    category: dev
    versions: true
    linux: [{ provider: apt, package: git }]
`))
	if err != nil {
		t.Fatal(err)
	}
	app, _ := c.Get("git")
	ref, err := providers.RefFrom(app, app.Installers["linux"][0], nil)
	if err != nil || ref.VersionInID {
		t.Errorf("git sem template: ref=%+v err=%v", ref, err)
	}
}

// Crivo #12: o id montado para 3.12 não pode instalar a 3.12 quando o usuário pediu 3.11.
func TestVersaoPedidaDiferenteDaAplicadaNoIdEhRecusada(t *testing.T) {
	c, err := catalog.Parse("c.yaml", []byte(`apps:
  python:
    name: Python
    category: dev
    versions: ["3.13", "3.12", "3.11"]
    linux: [{ provider: apt, package: "python{{major_minor}}" }]
`))
	if err != nil {
		t.Fatal(err)
	}
	app, _ := c.Get("python")
	f := exectest.New(t).WithPath("apt-get", "/x").WithPath("apt-cache", "/x").WithPath("dpkg-query", "/x") // qualquer comando falha o teste
	reg := providers.NewRegistry(apt.New(f))
	vars, _ := app.VersionVars("3.12")
	cands, _ := reg.Candidates(context.Background(), app, "linux", vars)
	if len(cands) != 1 || cands[0].Ref.VersionApplied != "3.12" {
		t.Fatalf("cands = %+v", cands)
	}
	if _, err := providers.InstallApp(context.Background(), cands, "3.11"); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("err = %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Errorf("nada executado: %v", f.Calls())
	}
}
