package pacman

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
	"github.com/PiluVitu/toolkit/internal/providers"
)

func ref(p string) providers.PackageRef { return providers.PackageRef{Provider: "pacman", Package: p} }

func fail(code int, stderr string) exectest.Reply {
	return exectest.Reply{Result: exec.Result{ExitCode: code, Stderr: []byte(stderr)}}
}

const installGit = "pacman -S --needed --noconfirm git"

func TestAvailableEValidate(t *testing.T) {
	if New(exectest.New(t)).Available(context.Background()) || !New(exectest.New(t).WithPath("pacman", "/usr/bin/pacman")).Available(context.Background()) {
		t.Error("Available")
	}
	p := New(exectest.New(t))
	for _, n := range []string{"git", "p7zip", "g++", "extra/git", "python-pip", "lib32-glibc"} {
		if err := p.Validate(ref(n)); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	for _, n := range []string{"", "-U", "--noconfirm", "a b", "a;b", "../x"} {
		if err := p.Validate(ref(n)); !errors.Is(err, providers.ErrInvalidRef) {
			t.Errorf("%q: err = %v", n, err)
		}
	}
	if err := p.Validate(providers.PackageRef{Provider: "pacman", Package: "git", Args: []string{"-x"}}); !errors.Is(err, providers.ErrInvalidRef) {
		t.Error("args")
	}
}

func TestIsInstalled(t *testing.T) {
	f := exectest.New(t).On("pacman -Q git", exectest.Exit(0, "git 2.47.1-1\n"))
	ok, v, err := New(f).IsInstalled(context.Background(), ref("git"))
	if err != nil || !ok || v != "2.47.1-1" {
		t.Errorf("ok=%v v=%q err=%v", ok, v, err)
	}
	if c := f.Calls()[0]; !c.ReadOnly || c.Elevate {
		t.Errorf("-Q é leitura: %+v", c)
	}
	f = exectest.New(t).On("pacman -Q git", fail(1, "error: package 'git' was not found\n"))
	if ok, _, err := New(f).IsInstalled(context.Background(), ref("git")); ok || err != nil {
		t.Errorf("ok=%v err=%v", ok, err)
	}
	f = exectest.New(t).On("pacman -Q git", fail(1, "error: failed to initialize alpm library\n"))
	if _, _, err := New(f).IsInstalled(context.Background(), ref("git")); err == nil {
		t.Error("falha do pacman devia ser erro")
	}
	// "repo/pacote": -Q usa só o nome
	f = exectest.New(t).On("pacman -Q git", exectest.Exit(0, "git 2.47.1-1\n"))
	if ok, _, _ := New(f).IsInstalled(context.Background(), ref("extra/git")); !ok {
		t.Error("extra/git")
	}
}

func TestListVersions(t *testing.T) {
	f := exectest.New(t).On("pacman -Si git", exectest.Exit(0, "Repository : extra\nName : git\n"))
	vs, err := New(f).ListVersions(context.Background(), ref("git"))
	if err != nil || len(vs) != 1 || vs[0] != "latest" {
		t.Errorf("vs=%v err=%v", vs, err)
	}
	if c := f.Calls()[0]; !c.ReadOnly || c.Elevate {
		t.Errorf("-Si é leitura: %+v", c)
	}
	f = exectest.New(t).On("pacman -Si nada", fail(1, "error: package 'nada' was not found\n"))
	if _, err := New(f).ListVersions(context.Background(), ref("nada")); !errors.Is(err, providers.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestVersaoFixadaNuncaInstalaALatest(t *testing.T) {
	f := exectest.New(t)
	p := New(f)
	if err := p.SupportsVersion(ref("git"), "2.0"); !errors.Is(err, providers.ErrVersionUnsupported) {
		t.Errorf("SupportsVersion: %v", err)
	}
	if _, err := p.Install(context.Background(), ref("git"), "2.0"); !errors.Is(err, providers.ErrVersionUnsupported) {
		t.Errorf("Install: %v", err)
	}
	if _, err := p.Install(context.Background(), ref("git"), "--force"); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("hostil: %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Error("nada executado")
	}
}

func TestInstall(t *testing.T) {
	f := exectest.New(t).OnElevated(installGit, exectest.Exit(0, ""))
	if _, err := New(f).Install(context.Background(), ref("git"), ""); err != nil {
		t.Fatal(err)
	}
	if c := f.Calls()[0]; !c.Elevate || c.ReadOnly {
		t.Errorf("install: %+v", c)
	}
	f = exectest.New(t).OnElevated(installGit, exectest.Exit(0, ""))
	if _, err := New(f).Install(context.Background(), ref("git"), "latest"); err != nil {
		t.Error(err)
	}
	// nunca -Sy (atualização parcial)
	for _, a := range f.Calls()[0].Args {
		if a == "-Sy" || a == "-y" {
			t.Errorf("arg proibido %s", a)
		}
	}
}

func TestInstallErrosPeloStderr(t *testing.T) {
	cases := []struct {
		stderr string
		want   error
	}{
		{"error: target not found: nada\n", providers.ErrNotFound},
		{"error: failed to init transaction (unable to lock database)\n", providers.ErrRetryable},
		{"error: you cannot perform this operation unless you are root.\n", providers.ErrNeedsAdmin},
	}
	for _, c := range cases {
		f := exectest.New(t).OnElevated(installGit, fail(1, c.stderr))
		if _, err := New(f).Install(context.Background(), ref("git"), ""); !errors.Is(err, c.want) {
			t.Errorf("%q: err = %v", c.stderr, err)
		}
	}
	f := exectest.New(t).OnElevated(installGit, fail(1, "\x1b[2Jfeio\n"))
	if _, err := New(f).Install(context.Background(), ref("git"), ""); err == nil || strings.ContainsRune(err.Error(), 0x1b) {
		t.Errorf("err = %v", err)
	}
	f = exectest.New(t).OnElevated(installGit, exectest.Reply{Err: exec.ErrNotElevated})
	if _, err := New(f).Install(context.Background(), ref("git"), ""); !errors.Is(err, providers.ErrNeedsAdmin) {
		t.Errorf("sudo: %v", err)
	}
}

var _ providers.Provider = (*Provider)(nil)

func TestTodoComandoTemLCALLC(t *testing.T) {
	r := ref("git")
	for name, c := range map[string]exec.Cmd{"-Q": queryCmd(r), "-Si": infoCmd(r), "-S": installCmd(r)} {
		found := false
		for _, e := range c.Env {
			found = found || e == "LC_ALL=C"
		}
		if !found {
			t.Errorf("%s sem LC_ALL=C", name)
		}
	}
}

// Crivo #13: espelho com a base velha (404) manda atualizar o sistema, nunca roda -Sy sozinho.
func TestInstall404DeEspelhoPedeSyuSemSyAutomatico(t *testing.T) {
	for _, msg := range []string{"error: failed retrieving file 'git-2.47.1-1-x86_64.pkg.tar.zst' from mirror : The requested URL returned error: 404\n", "error: failed to commit transaction (failed to retrieve some files)\n"} {
		f := exectest.New(t).OnElevated(installGit, fail(1, msg))
		_, err := New(f).Install(context.Background(), ref("git"), "")
		if !errors.Is(err, providers.ErrRetryable) || !strings.Contains(err.Error(), "sudo pacman -Syu") {
			t.Errorf("%q: err = %v", msg, err)
		}
		if len(f.Calls()) != 1 {
			t.Errorf("nenhum -Sy automático: %d chamadas", len(f.Calls()))
		}
	}
}
