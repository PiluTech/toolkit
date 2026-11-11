package dnf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
	"github.com/PiluVitu/toolkit/internal/providers"
)

func ref(p string) providers.PackageRef { return providers.PackageRef{Provider: "dnf", Package: p} }

func fixture(t *testing.T, n string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", n))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fail(code int, stderr string) exectest.Reply {
	return exectest.Reply{Result: exec.Result{ExitCode: code, Stderr: []byte(stderr)}}
}

var rpmGit = rpmCmd(ref("git")).String()

const (
	listGit    = "dnf list --showduplicates git"
	installGit = "dnf install -y git"
)

func TestAvailable(t *testing.T) {
	if New(exectest.New(t).WithPath("dnf", "/usr/bin/dnf")).Available(context.Background()) {
		t.Error("falta rpm")
	}
	if !New(exectest.New(t).WithPath("dnf", "/usr/bin/dnf").WithPath("rpm", "/usr/bin/rpm")).Available(context.Background()) {
		t.Error("com os dois")
	}
}

func TestValidate(t *testing.T) {
	p := New(exectest.New(t))
	for _, n := range []string{"git", "p7zip", "python3.13", "g++"} {
		if err := p.Validate(ref(n)); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	for _, n := range []string{"", "-y", "--nogpgcheck", "dir/x.rpm", "x.rpm", "X.RPM", "a/b", "a@b", "a b"} {
		if err := p.Validate(ref(n)); !errors.Is(err, providers.ErrInvalidRef) {
			t.Errorf("%q: err = %v", n, err)
		}
	}
	for _, r := range []providers.PackageRef{{Provider: "dnf", Package: "git", Args: []string{"--force"}}, {Provider: "dnf", Package: "git", URL: "https://x.com/a"}} {
		if err := p.Validate(r); !errors.Is(err, providers.ErrInvalidRef) {
			t.Errorf("%+v: err = %v", r, err)
		}
	}
}

func TestIsInstalled(t *testing.T) {
	f := exectest.New(t).On(rpmGit, exectest.Exit(0, "2.43.0-1.fc40\n"))
	ok, v, err := New(f).IsInstalled(context.Background(), ref("git"))
	if err != nil || !ok || v != "2.43.0-1.fc40" {
		t.Errorf("ok=%v v=%q err=%v", ok, v, err)
	}
	if c := f.Calls()[0]; !c.ReadOnly || c.Elevate {
		t.Errorf("rpm -q é leitura: %+v", c)
	}
	f = exectest.New(t).On(rpmGit, exectest.Exit(1, "package git is not installed\n"))
	if ok, _, err := New(f).IsInstalled(context.Background(), ref("git")); ok || err != nil {
		t.Errorf("não instalado: ok=%v err=%v", ok, err)
	}
	f = exectest.New(t).On(rpmGit, fail(2, "error: rpmdb corrompido\n"))
	if _, _, err := New(f).IsInstalled(context.Background(), ref("git")); err == nil {
		t.Error("falha do rpm devia ser erro")
	}
	f = exectest.New(t).On(rpmGit, exectest.Exit(0, "1:2.43.0-1.fc40\n"))
	if _, v, _ := New(f).IsInstalled(context.Background(), ref("git")); v != "1:2.43.0-1.fc40" {
		t.Errorf("época: %q", v)
	}
}

func TestListVersionsDnf4EDnf5(t *testing.T) {
	f := exectest.New(t).On(listGit, exectest.Exit(0, fixture(t, "list_dnf4.txt")))
	vs, err := New(f).ListVersions(context.Background(), ref("git"))
	if err != nil || len(vs) != 2 || vs[0] != "2.43.0-1.fc40" || vs[1] != "2.42.0-1.fc40" {
		t.Errorf("dnf4: vs=%v err=%v", vs, err)
	}
	if c := f.Calls()[0]; !c.ReadOnly || c.Elevate {
		t.Errorf("list é leitura: %+v", c)
	}
	f = exectest.New(t).On(listGit, exectest.Exit(0, fixture(t, "list_dnf5.txt")))
	vs, err = New(f).ListVersions(context.Background(), ref("git"))
	if err != nil || len(vs) != 2 || vs[0] != "2.43.0-1.fc40" {
		t.Errorf("dnf5: vs=%v err=%v", vs, err)
	}
}

func TestParseListNomeLongoQuebradoENomeExato(t *testing.T) {
	out := []byte(fixture(t, "list_dnf4.txt"))
	vs := ParseList(out, "libreoffice-langpack-pt-BR")
	if len(vs) != 1 || vs[0] != "1:24.2.1-2.fc40" {
		t.Errorf("vs = %v", vs)
	}
	if vs := ParseList(out, "git-lfs"); len(vs) != 1 {
		t.Errorf("git-lfs = %v", vs)
	}
	if len(ParseList(out, "gi")) != 0 {
		t.Error("nome parcial não casa")
	}
}

func TestListVersionsNaoEncontrado(t *testing.T) {
	f := exectest.New(t).On(listGit, fail(1, "Error: No matching Packages to list\n"))
	if _, err := New(f).ListVersions(context.Background(), ref("git")); !errors.Is(err, providers.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	f = exectest.New(t).On(listGit, exectest.Exit(0, "Available Packages\n"))
	if _, err := New(f).ListVersions(context.Background(), ref("git")); !errors.Is(err, providers.ErrNotFound) {
		t.Errorf("sem linhas: %v", err)
	}
}

func TestInstall(t *testing.T) {
	f := exectest.New(t).OnElevated(installGit, exectest.Exit(0, ""))
	if _, err := New(f).Install(context.Background(), ref("git"), ""); err != nil {
		t.Fatal(err)
	}
	if c := f.Calls()[0]; !c.Elevate || c.ReadOnly {
		t.Errorf("install: Elevate, sem ReadOnly: %+v", c)
	}
	f = exectest.New(t).OnElevated("dnf install -y git-2.42.0-1.fc40", exectest.Exit(0, ""))
	if res, err := New(f).Install(context.Background(), ref("git"), "2.42.0-1.fc40"); err != nil || res.Version != "2.42.0-1.fc40" {
		t.Errorf("res=%+v err=%v", res, err)
	}
}

func TestInstallRecusaHostis(t *testing.T) {
	f := exectest.New(t)
	p := New(f)
	if _, err := p.Install(context.Background(), ref("git"), "--nogpgcheck"); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("versão: %v", err)
	}
	if _, err := p.Install(context.Background(), ref("dir/x.rpm"), ""); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("rpm local: %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Error("nada executado")
	}
}

func TestInstallErrosPeloStderr(t *testing.T) {
	cases := []struct {
		stderr, ver string
		want        error
	}{
		{"Error: Unable to find a match: git\n", "", providers.ErrNotFound},
		{"No match for argument: git\n", "", providers.ErrNotFound},
		{"No match for argument: git-9.9\n", "9.9", providers.ErrVersionUnsupported},
		{"Error: This command has to be run with superuser privileges (under the root user on most systems).\n", "", providers.ErrNeedsAdmin},
		{"Error: Could not lock /var/lib/rpm\n", "", providers.ErrRetryable},
	}
	for _, c := range cases {
		line := installGit
		if c.ver != "" {
			line += "-" + c.ver
		}
		f := exectest.New(t).OnElevated(line, fail(1, c.stderr))
		_, err := New(f).Install(context.Background(), ref("git"), c.ver)
		if !errors.Is(err, c.want) {
			t.Errorf("%q: err = %v, queria %v", c.stderr, err, c.want)
		}
	}
	f := exectest.New(t).OnElevated(installGit, fail(1, "\x1b[2Jerro estranho\n"))
	if _, err := New(f).Install(context.Background(), ref("git"), ""); err == nil || strings.ContainsRune(err.Error(), 0x1b) {
		t.Errorf("err = %v", err)
	}
}

func TestSudoSemCredencial(t *testing.T) {
	f := exectest.New(t).OnElevated(installGit, exectest.Reply{Err: exec.ErrNotElevated})
	if _, err := New(f).Install(context.Background(), ref("git"), ""); !errors.Is(err, providers.ErrNeedsAdmin) {
		t.Errorf("err = %v", err)
	}
}

var _ providers.Provider = (*Provider)(nil)

func TestTodoComandoTemLCALLC(t *testing.T) {
	r := ref("git")
	for name, c := range map[string]exec.Cmd{"rpm": rpmCmd(r), "list": listCmd(r), "install": installCmd(r, "")} {
		found := false
		for _, e := range c.Env {
			found = found || e == "LC_ALL=C"
		}
		if !found {
			t.Errorf("%s sem LC_ALL=C", name)
		}
	}
}
