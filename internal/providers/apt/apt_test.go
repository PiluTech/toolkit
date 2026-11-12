package apt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/providers"
)

func ref(p string) providers.PackageRef { return providers.PackageRef{Provider: "apt", Package: p} }

func fixture(t *testing.T, n string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", n))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func reply(code int, stderr string) exectest.Reply {
	return exectest.Reply{Result: exec.Result{ExitCode: code, Stderr: []byte(stderr)}}
}

var (
	statusLine  = statusCmd(ref("git")).String() // a linha entre aspas é como o FakeRunner indexa
	madisonLine = "apt-cache madison git"
)

func hasEnv(c exec.Cmd, w string) bool {
	for _, e := range c.Env {
		if e == w {
			return true
		}
	}
	return false
}

func TestAvailable(t *testing.T) {
	all := exectest.New(t).WithPath("apt-get", "/usr/bin/apt-get").WithPath("apt-cache", "/usr/bin/apt-cache").WithPath("dpkg-query", "/usr/bin/dpkg-query")
	if !New(all).Available(context.Background()) {
		t.Error("com os três")
	}
	part := exectest.New(t).WithPath("apt-get", "/usr/bin/apt-get")
	if New(part).Available(context.Background()) {
		t.Error("falta apt-cache e dpkg-query")
	}
}

func TestValidate(t *testing.T) {
	p := New(exectest.New(t))
	for _, n := range []string{"git", "p7zip-full", "python3.13", "g++", "git/noble-backports"} {
		if err := p.Validate(ref(n)); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	bad := []providers.PackageRef{
		ref(""), ref("-y"), ref("dir/x.deb"), ref("X.DEB"), ref("--allow-unauthenticated"), ref("a b"), ref("git;ls"),
		{Provider: "apt", Package: "git", ID: "x"},
		{Provider: "apt", Package: "git", Args: []string{"--force-yes"}},
	}
	for _, r := range bad {
		if err := p.Validate(r); !errors.Is(err, providers.ErrInvalidRef) {
			t.Errorf("%+v: err = %v", r, err)
		}
	}
}

func TestIsInstalled(t *testing.T) {
	f := exectest.New(t).On(statusLine, exectest.Exit(0, "installed\t1:2.43.0-1ubuntu7\n"))
	ok, v, err := New(f).IsInstalled(context.Background(), ref("git"))
	if err != nil || !ok || v != "1:2.43.0-1ubuntu7" {
		t.Errorf("ok=%v v=%q err=%v", ok, v, err)
	}
	if c := f.Calls()[0]; !c.ReadOnly || c.Elevate {
		t.Errorf("leitura: %+v", c)
	}
	for _, out := range []struct {
		code int
		out  string
	}{{1, ""}, {0, "config-files\t1:2.0\n"}, {0, "not-installed\t\n"}} {
		f := exectest.New(t).On(statusLine, exectest.Exit(out.code, out.out))
		if ok, _, err := New(f).IsInstalled(context.Background(), ref("git")); ok || err != nil {
			t.Errorf("%+v: ok=%v err=%v", out, ok, err)
		}
	}
}

func TestIsInstalledUsaSoONomeSemSuite(t *testing.T) {
	f := exectest.New(t).On(statusLine, exectest.Exit(0, "installed\t1.0\n"))
	if ok, _, err := New(f).IsInstalled(context.Background(), ref("git/noble-backports")); !ok || err != nil {
		t.Errorf("ok=%v err=%v", ok, err)
	}
}

func TestListVersionsComEpoca(t *testing.T) {
	f := exectest.New(t).On(madisonLine, exectest.Exit(0, fixture(t, "madison.txt")))
	vs, err := New(f).ListVersions(context.Background(), ref("git"))
	if err != nil || len(vs) != 2 || vs[0] != "1:2.43.0-1ubuntu7.1" || vs[1] != "1:2.43.0-1ubuntu7" {
		t.Fatalf("vs=%v err=%v", vs, err)
	}
	// round-trip: a versão devolvida passa pela validação de versão do núcleo (época com ':')
	for _, v := range vs {
		if !providers.ValidVersion(v) {
			t.Errorf("%q não passa em ValidVersion", v)
		}
	}
	if c := f.Calls()[0]; !c.ReadOnly || c.Elevate {
		t.Errorf("madison é leitura: %+v", c)
	}
	f = exectest.New(t).On(madisonLine, exectest.Exit(0, ""))
	if _, err := New(f).ListVersions(context.Background(), ref("git")); !errors.Is(err, providers.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	f = exectest.New(t).On(madisonLine, reply(1, "E: erro\n"))
	if _, err := New(f).ListVersions(context.Background(), ref("git")); err == nil {
		t.Error("exit 1 devia falhar")
	}
}

func TestInstallSudoEEnv(t *testing.T) {
	f := exectest.New(t).OnElevated("apt-get install -y --no-remove git", exectest.Exit(0, ""))
	if _, err := New(f).Install(context.Background(), ref("git"), ""); err != nil {
		t.Fatal(err)
	}
	c := f.Calls()[0]
	if !c.Elevate || c.ReadOnly || !hasEnv(c, "DEBIAN_FRONTEND=noninteractive") {
		t.Errorf("install: Elevate, sem ReadOnly, com DEBIAN_FRONTEND: %+v", c)
	}
}

func TestInstallVersaoFixadaUsaIgual(t *testing.T) {
	f := exectest.New(t).OnElevated("apt-get install -y --no-remove git=1:2.43.0-1ubuntu7", exectest.Exit(0, ""))
	res, err := New(f).Install(context.Background(), ref("git/noble"), "1:2.43.0-1ubuntu7")
	if err != nil || res.Version != "1:2.43.0-1ubuntu7" {
		t.Errorf("res=%+v err=%v", res, err)
	}
}

func TestInstallRecusaVersaoEPacoteHostis(t *testing.T) {
	f := exectest.New(t)
	p := New(f)
	if _, err := p.Install(context.Background(), ref("git"), "--allow-downgrades"); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("versão: %v", err)
	}
	if _, err := p.Install(context.Background(), ref("-y"), ""); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("pacote: %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Error("nada executado")
	}
}

func TestInstallExit100ClassificaPeloStderr(t *testing.T) {
	cases := []struct {
		stderr string
		want   error
	}{
		{"E: Unable to locate package nada\n", providers.ErrNotFound},
		{"E: Version '9.9' for 'git' was not found\n", providers.ErrVersionUnsupported},
		{"E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 123\n", providers.ErrRetryable},
		{"E: Unable to acquire the dpkg frontend lock (/var/lib/dpkg/lock-frontend), is another process using it?\n", providers.ErrRetryable},
		{"E: Could not open lock file - open (13: Permission denied)\n", providers.ErrNeedsAdmin},
	}
	for _, c := range cases {
		f := exectest.New(t).OnElevated("apt-get install -y --no-remove git", reply(100, c.stderr))
		_, err := New(f).Install(context.Background(), ref("git"), "")
		if !errors.Is(err, c.want) {
			t.Errorf("%q: err = %v, queria %v", c.stderr, err, c.want)
		}
	}
	f := exectest.New(t).OnElevated("apt-get install -y --no-remove git", reply(100, "\x1b[2JE: algo estranho\n"))
	_, err := New(f).Install(context.Background(), ref("git"), "")
	if err == nil || strings.ContainsRune(err.Error(), 0x1b) || !strings.Contains(err.Error(), "algo estranho") {
		t.Errorf("err = %v", err)
	}
}

func TestSudoSemCredencialViraErrNeedsAdmin(t *testing.T) {
	f := exectest.New(t).OnElevated("apt-get install -y --no-remove git", exectest.Reply{Err: exec.ErrNotElevated})
	_, err := New(f).Install(context.Background(), ref("git"), "")
	if !errors.Is(err, providers.ErrNeedsAdmin) {
		t.Errorf("err = %v", err)
	}
}

func TestPrepareUmUnicoUpdateComSudo(t *testing.T) {
	f := exectest.New(t).OnElevated("apt-get update", exectest.Exit(0, ""))
	p := New(f)
	for i := 0; i < 3; i++ {
		if err := p.Prepare(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	calls := f.Calls()
	if len(calls) != 1 || !calls[0].Elevate || calls[0].ReadOnly || !hasEnv(calls[0], "DEBIAN_FRONTEND=noninteractive") {
		t.Errorf("calls = %+v", calls)
	}
}

func TestPrepareFalhaMemorizada(t *testing.T) {
	f := exectest.New(t).OnElevated("apt-get update", reply(100, "E: falhou\n"))
	p := New(f)
	e1, e2 := p.Prepare(context.Background()), p.Prepare(context.Background())
	if e1 == nil || e2 == nil || len(f.Calls()) != 1 {
		t.Errorf("e1=%v e2=%v chamadas=%d", e1, e2, len(f.Calls()))
	}
}

func TestSupportsVersionAceita(t *testing.T) {
	if err := New(exectest.New(t)).SupportsVersion(ref("git"), "1:2.0"); err != nil {
		t.Error(err)
	}
}

var (
	_ providers.Provider = (*Provider)(nil)
	_ providers.Preparer = (*Provider)(nil)
)

// Crivo #12: a classificação lê texto do apt; LC_ALL=C em todo comando, e --no-remove no install.
func TestTodoComandoTemLCALLCeInstallTemNoRemove(t *testing.T) {
	r := ref("git")
	for name, c := range map[string]exec.Cmd{"dpkg-query": statusCmd(r), "madison": madisonCmd(r), "update": updateCmd(), "install": installCmd(r, "")} {
		if !hasEnv(c, "LC_ALL=C") {
			t.Errorf("%s sem LC_ALL=C", name)
		}
	}
	args := strings.Join(installCmd(r, "").Args, " ")
	if !strings.Contains(args, "--no-remove") {
		t.Errorf("install sem --no-remove: %s", args)
	}
}
