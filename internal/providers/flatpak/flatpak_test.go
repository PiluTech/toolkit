package flatpak

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
	"github.com/PiluVitu/toolkit/internal/providers"
)

func ref(id string) providers.PackageRef { return providers.PackageRef{Provider: "flatpak", ID: id} }

func fail(code int, stderr string) exectest.Reply {
	return exectest.Reply{Result: exec.Result{ExitCode: code, Stderr: []byte(stderr)}}
}

func hasLC(c exec.Cmd) bool {
	for _, e := range c.Env {
		if e == "LC_ALL=C" {
			return true
		}
	}
	return false
}

const (
	listLine    = "flatpak list --app --columns=application,version"
	installLine = "flatpak install --system -y --noninteractive flathub org.mozilla.firefox"
)

func TestAvailableEValidate(t *testing.T) {
	if New(exectest.New(t)).Available(context.Background()) || !New(exectest.New(t).WithPath("flatpak", "/usr/bin/flatpak")).Available(context.Background()) {
		t.Error("Available")
	}
	p := New(exectest.New(t))
	ok := []providers.PackageRef{ref("org.mozilla.firefox"), {Provider: "flatpak", ID: "com.discordapp.Discord", Source: "flathub"}, ref("org.gnome.Platform_x-y")}
	for _, r := range ok {
		if err := p.Validate(r); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	bad := []providers.PackageRef{
		ref(""), ref("-y"), ref("--user"), ref("a b"), ref("x.flatpakref"), ref("./x.flatpak"), ref("dir/x"), ref("app/org.x/x86_64/stable"), ref("a@b"),
		{Provider: "flatpak", ID: "org.x.App", Source: "--user"},
		{Provider: "flatpak", ID: "org.x.App", Source: "x.flatpakrepo"},
		{Provider: "flatpak", ID: "org.x.App", Package: "x"},
		{Provider: "flatpak", ID: "org.x.App", Args: []string{"--user"}},
	}
	for _, r := range bad {
		if err := p.Validate(r); !errors.Is(err, providers.ErrInvalidRef) {
			t.Errorf("%+v: err = %v", r, err)
		}
	}
}

func TestIsInstalled(t *testing.T) {
	out := "org.mozilla.firefox\t133.0\norg.videolan.VLC\t\ncom.x.Appfoo\t1.0\n"
	f := exectest.New(t).On(listLine, exectest.Exit(0, out))
	p := New(f)
	ok, v, err := p.IsInstalled(context.Background(), ref("org.mozilla.firefox"))
	if err != nil || !ok || v != "133.0" {
		t.Errorf("firefox: ok=%v v=%q err=%v", ok, v, err)
	}
	if ok, v, _ := p.IsInstalled(context.Background(), ref("org.videolan.VLC")); !ok || v != "" {
		t.Errorf("vlc sem versão: ok=%v v=%q", ok, v)
	}
	if ok, _, _ := p.IsInstalled(context.Background(), ref("com.x.App")); ok {
		t.Error("id parcial não casa")
	}
	for _, c := range f.Calls() {
		if !c.ReadOnly || c.Elevate || !hasLC(c) {
			t.Errorf("list: leitura com LC_ALL=C: %+v", c)
		}
	}
	f = exectest.New(t).On(listLine, fail(1, "error: sem permissão\n"))
	if _, _, err := New(f).IsInstalled(context.Background(), ref("org.mozilla.firefox")); err == nil {
		t.Error("falha do flatpak devia ser erro")
	}
}

func TestListVersions(t *testing.T) {
	f := exectest.New(t).On("flatpak remote-info flathub org.mozilla.firefox", exectest.Exit(0, "Ref: app/org.mozilla.firefox\n"))
	vs, err := New(f).ListVersions(context.Background(), ref("org.mozilla.firefox"))
	if err != nil || len(vs) != 1 || vs[0] != "latest" {
		t.Errorf("vs=%v err=%v", vs, err)
	}
	if c := f.Calls()[0]; !c.ReadOnly || c.Elevate || !hasLC(c) {
		t.Errorf("remote-info: %+v", c)
	}
	f = exectest.New(t).On("flatpak remote-info flathub org.nada", fail(1, "error: Nothing matches org.nada in remote flathub\n"))
	if _, err := New(f).ListVersions(context.Background(), ref("org.nada")); !errors.Is(err, providers.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestInstall(t *testing.T) {
	f := exectest.New(t).OnElevated(installLine, exectest.Exit(0, ""))
	if _, err := New(f).Install(context.Background(), ref("org.mozilla.firefox"), ""); err != nil {
		t.Fatal(err)
	}
	if c := f.Calls()[0]; !c.Elevate || c.ReadOnly || !hasLC(c) {
		t.Errorf("install: %+v", c)
	}
	f = exectest.New(t).OnElevated("flatpak install --system -y --noninteractive meu-remote org.mozilla.firefox", exectest.Exit(0, ""))
	r := providers.PackageRef{Provider: "flatpak", ID: "org.mozilla.firefox", Source: "meu-remote"}
	if _, err := New(f).Install(context.Background(), r, "latest"); err != nil {
		t.Error(err)
	}
}

func TestInstallVersaoFixadaEHostis(t *testing.T) {
	f := exectest.New(t)
	p := New(f)
	if _, err := p.Install(context.Background(), ref("org.mozilla.firefox"), "133.0"); !errors.Is(err, providers.ErrVersionUnsupported) {
		t.Errorf("fixada: %v", err)
	}
	if err := p.SupportsVersion(ref("org.mozilla.firefox"), "1"); !errors.Is(err, providers.ErrVersionUnsupported) {
		t.Errorf("SupportsVersion: %v", err)
	}
	if _, err := p.Install(context.Background(), ref("org.mozilla.firefox"), "--user"); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("hostil: %v", err)
	}
	if _, err := p.Install(context.Background(), ref("x.flatpakref"), ""); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("arquivo: %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Error("nada executado")
	}
}

func TestInstallErros(t *testing.T) {
	cases := []struct {
		stderr string
		want   error
	}{
		{"error: No remote refs found for 'org.nada'\n", providers.ErrNotFound},
		{"error: Remote \"flathub\" not found\n", providers.ErrNotFound},
		{"error: Insufficient permissions to install\n", providers.ErrNeedsAdmin},
	}
	for _, c := range cases {
		f := exectest.New(t).OnElevated(installLine, fail(1, c.stderr))
		if _, err := New(f).Install(context.Background(), ref("org.mozilla.firefox"), ""); !errors.Is(err, c.want) {
			t.Errorf("%q: err = %v", c.stderr, err)
		}
	}
	// remote ausente: a mensagem diz como adicionar
	f := exectest.New(t).OnElevated(installLine, fail(1, "error: Remote \"flathub\" not found\n"))
	_, err := New(f).Install(context.Background(), ref("org.mozilla.firefox"), "")
	if err == nil || !strings.Contains(err.Error(), "remote-add") {
		t.Errorf("err = %v", err)
	}
	f = exectest.New(t).OnElevated(installLine, fail(1, "error: org.mozilla.firefox/x86_64/stable is already installed\n"))
	if res, err := New(f).Install(context.Background(), ref("org.mozilla.firefox"), ""); err != nil || !res.AlreadyInstalled {
		t.Errorf("já instalado: res=%+v err=%v", res, err)
	}
	f = exectest.New(t).OnElevated(installLine, fail(1, "\x1b[2Jfeio\n"))
	if _, err := New(f).Install(context.Background(), ref("org.mozilla.firefox"), ""); err == nil || strings.ContainsRune(err.Error(), 0x1b) {
		t.Errorf("sanitização: %v", err)
	}
	f = exectest.New(t).OnElevated(installLine, exectest.Reply{Err: exec.ErrNotElevated})
	if _, err := New(f).Install(context.Background(), ref("org.mozilla.firefox"), ""); !errors.Is(err, providers.ErrNeedsAdmin) {
		t.Errorf("sudo: %v", err)
	}
}

var _ providers.Provider = (*Provider)(nil)
