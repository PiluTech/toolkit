package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/PiluVitu/toolkit/internal/exec/exectest"
)

const ubuntuRelease = `PRETTY_NAME="Ubuntu 24.04 LTS"
# comentário
ID=ubuntu
ID_LIKE=debian
VERSION_ID="24.04"
`

func TestParseOSRelease(t *testing.T) {
	got := ParseOSRelease(ubuntuRelease)
	want := Distro{ID: "ubuntu", IDLike: []string{"debian"}, VersionID: "24.04"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if d := ParseOSRelease("ID=arch\nID_LIKE='rhel fedora'\n"); d.ID != "arch" || len(d.IDLike) != 2 {
		t.Fatalf("%+v", d)
	}
	if d := ParseOSRelease(""); d.ID != "" {
		t.Fatalf("vazio: %+v", d)
	}
}

func TestNormalizeArch(t *testing.T) {
	for in, want := range map[string]string{"x86_64": "amd64", "AMD64": "amd64", "aarch64": "arm64", "arm64": "arm64", "riscv64": "riscv64"} {
		if got := NormalizeArch(in); got != want {
			t.Errorf("%s -> %s, quer %s", in, got, want)
		}
	}
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDetectLinux(t *testing.T) {
	fs := map[string]string{
		"/etc/os-release":                 ubuntuRelease,
		"/proc/sys/kernel/random/boot_id": "abc-123\n",
	}
	r := exectest.New(t).WithPath("apt-get", "/usr/bin/apt-get").WithPath("flatpak", "/usr/bin/flatpak")
	d := SystemDetector{
		Runner: r, GOOS: "linux", GOARCH: "arm64",
		Getenv: env(map[string]string{"SUDO_USER": "paulo", "USER": "root"}),
		ReadFile: func(p string) ([]byte, error) {
			if v, ok := fs[p]; ok {
				return []byte(v), nil
			}
			return nil, errors.New("sem arquivo")
		},
		Euid: func() int { return 0 },
	}
	info, err := d.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.OS != Linux || info.Arch != "arm64" || info.Distro.ID != "ubuntu" || info.Version != "24.04" ||
		info.BootID != "abc-123" || !info.IsAdmin || info.User != "paulo" ||
		!reflect.DeepEqual(info.Managers, []string{"apt-get", "flatpak"}) {
		t.Fatalf("%+v", info)
	}
}

func TestDetectLinuxSemArquivosNaoFalha(t *testing.T) {
	d := SystemDetector{
		GOOS: "linux", GOARCH: "amd64", Getenv: env(nil),
		ReadFile: func(string) ([]byte, error) { return nil, errors.New("x") },
		Euid:     func() int { return 1000 },
	}
	info, err := d.Detect(context.Background())
	if err != nil || info.IsAdmin || info.BootID != "" || info.Distro.ID != "" {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestDetectDarwin(t *testing.T) {
	r := exectest.New(t).
		On("sw_vers -productVersion", exectest.Exit(0, "15.1\n")).
		On("sysctl -n kern.boottime", exectest.Exit(0, "{ sec = 1700000000, usec = 5 } Tue Nov 14\n")).
		WithPath("brew", "/opt/homebrew/bin/brew")
	d := SystemDetector{Runner: r, GOOS: "darwin", GOARCH: "arm64", Getenv: env(map[string]string{"USER": "u"}), Euid: func() int { return 501 }}
	info, _ := d.Detect(context.Background())
	if info.Version != "15.1" || info.BootID != "{ sec = 1700000000, usec = 5 } Tue Nov 14" ||
		info.IsAdmin || info.User != "u" || !reflect.DeepEqual(info.Managers, []string{"brew"}) {
		t.Fatalf("%+v", info)
	}
}

func TestDetectWindows(t *testing.T) {
	buildCmd := psCmd("(Get-CimInstance Win32_OperatingSystem).BuildNumber").String()
	bootCmd := psCmd("(Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToString('o')").String()
	r := exectest.New(t).
		On(buildCmd, exectest.Exit(0, "26100\r\n")).
		On(bootCmd, exectest.Exit(0, "2026-11-11T08:00:00.0000000-03:00\r\n")).
		WithPath("winget", `C:\winget.exe`)
	d := SystemDetector{Runner: r, GOOS: "windows", GOARCH: "amd64", Getenv: env(map[string]string{"USERNAME": "paulo"}),
		IsElevated: func() bool { return true }}
	info, _ := d.Detect(context.Background())
	if info.Version != "26100" || info.BootID != "2026-11-11T08:00:00.0000000-03:00" || !info.IsAdmin ||
		info.User != "paulo" || !reflect.DeepEqual(info.Managers, []string{"winget"}) {
		t.Fatalf("%+v", info)
	}
}

func TestDetectLeituraComFalhaDeixaCampoVazio(t *testing.T) {
	r := exectest.New(t).
		On("sw_vers -productVersion", exectest.Exit(1, "")).
		On("sysctl -n kern.boottime", exectest.Reply{Err: errors.New("boom")})
	info, err := SystemDetector{Runner: r, GOOS: "darwin", GOARCH: "amd64", Getenv: env(nil), Euid: func() int { return 1 }}.Detect(context.Background())
	if err != nil || info.Version != "" || info.BootID != "" {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestDetectRealNaoQuebra(t *testing.T) {
	// Sem Runner: nenhuma leitura por processo; só valida que o detector real não entra em pânico.
	info, err := SystemDetector{}.Detect(context.Background())
	if err != nil || info.OS == "" || info.Arch == "" {
		t.Fatalf("%+v %v", info, err)
	}
}
