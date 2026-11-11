package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
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
		Getenv:      env(map[string]string{"SUDO_USER": "paulo", "SUDO_UID": "1000", "USER": "root"}),
		LookupUser:  func(n string) (string, error) { return map[string]string{"paulo": "1000"}[n], nil },
		CurrentUser: func() (string, error) { return "root", nil },
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
		On("/usr/bin/sw_vers -productVersion", exectest.Exit(0, "15.1\n")).
		On("/usr/sbin/sysctl -n kern.boottime", exectest.Exit(0, "{ sec = 1700000000, usec = 5 } Tue Nov 14\n")).
		WithPath("brew", "/opt/homebrew/bin/brew")
	d := SystemDetector{Runner: r, GOOS: "darwin", GOARCH: "arm64", Getenv: env(map[string]string{"USER": "outro"}), Euid: func() int { return 501 },
		CurrentUser: func() (string, error) { return "u", nil }}
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
	d := SystemDetector{Runner: r, GOOS: "windows", GOARCH: "amd64", Getenv: env(map[string]string{"USERNAME": "falso"}),
		CurrentUser: func() (string, error) { return `PC\paulo`, nil }, IsElevated: func() bool { return true }}
	info, _ := d.Detect(context.Background())
	if info.Version != "26100" || info.BootID != "2026-11-11T08:00:00.0000000-03:00" || !info.IsAdmin ||
		info.User != "paulo" || !reflect.DeepEqual(info.Managers, []string{"winget"}) {
		t.Fatalf("%+v", info)
	}
}

func TestDetectLeituraComFalhaDeixaCampoVazio(t *testing.T) {
	r := exectest.New(t).
		On("/usr/bin/sw_vers -productVersion", exectest.Exit(1, "")).
		On("/usr/sbin/sysctl -n kern.boottime", exectest.Reply{Err: errors.New("boom")})
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

func linuxDetector(getenv map[string]string, euid int, lookup func(string) (string, error), cur func() (string, error)) SystemDetector {
	return SystemDetector{
		GOOS: "linux", GOARCH: "amd64", Getenv: env(getenv),
		ReadFile:    func(string) ([]byte, error) { return nil, errors.New("x") },
		Euid:        func() int { return euid },
		LookupUser:  lookup,
		CurrentUser: cur,
	}
}

func TestUsuarioSudoUserSoValeComRootEUidConferido(t *testing.T) {
	lookup := func(n string) (string, error) {
		if n == "paulo" {
			return "1000", nil
		}
		return "", errors.New("user: unknown user " + n)
	}
	cur := func() (string, error) { return "dono", nil }
	cases := []struct {
		name string
		env  map[string]string
		euid int
		want string
	}{
		{"root via sudo, uid confere", map[string]string{"SUDO_USER": "paulo", "SUDO_UID": "1000"}, 0, "paulo"},
		{"euid != 0: SUDO_USER é ignorado", map[string]string{"SUDO_USER": "paulo", "SUDO_UID": "1000"}, 1000, "dono"},
		{"uid não bate", map[string]string{"SUDO_USER": "paulo", "SUDO_UID": "0"}, 0, "dono"},
		{"sem SUDO_UID", map[string]string{"SUDO_USER": "paulo"}, 0, "dono"},
		{"usuário inexistente", map[string]string{"SUDO_USER": "fantasma", "SUDO_UID": "1000"}, 0, "dono"},
		{"nome inválido (path)", map[string]string{"SUDO_USER": "../../etc", "SUDO_UID": "1000"}, 0, "dono"},
		{"nome inválido (espaço/flag)", map[string]string{"SUDO_USER": "-rf x", "SUDO_UID": "1000"}, 0, "dono"},
		{"USER nunca é usado", map[string]string{"USER": "paulo"}, 0, "dono"},
	}
	for _, c := range cases {
		info, _ := linuxDetector(c.env, c.euid, lookup, cur).Detect(context.Background())
		if info.User != c.want {
			t.Errorf("%s: User=%q, quer %q", c.name, info.User, c.want)
		}
	}
}

func TestUsuarioSemDonoDoProcessoFicaVazio(t *testing.T) {
	d := linuxDetector(map[string]string{"USER": "x"}, 1000, nil, func() (string, error) { return "", errors.New("sem passwd") })
	if info, _ := d.Detect(context.Background()); info.User != "" {
		t.Fatalf("User=%q", info.User)
	}
}

func TestValidUsername(t *testing.T) {
	for _, ok := range []string{"paulo", "_apt", "user.name", "u-1", "Paulo", "svc$"} {
		if !validUsername(ok) {
			t.Errorf("%q deveria ser válido", ok)
		}
	}
	for _, bad := range []string{"", "../../etc", "a/b", "-x", ".x", "a b", "a\nb", "a..b/c", strings.Repeat("a", 40), "é"} {
		if validUsername(bad) {
			t.Errorf("%q deveria ser inválido", bad)
		}
	}
}

func TestParseOSReleaseIDEmMinusculasEMalformado(t *testing.T) {
	if d := ParseOSRelease("ID=Ubuntu\nID_LIKE=\"Debian RHEL\"\n"); d.ID != "ubuntu" || !reflect.DeepEqual(d.IDLike, []string{"debian", "rhel"}) {
		t.Fatalf("minúsculas: %+v", d)
	}
	cases := map[string]string{
		"sem igual":        "ID ubuntu\nlixo",
		"aspas sem fechar": "ID=\"ubuntu\nVERSION_ID=\"24",
		"id inválido":      "ID=../../x\n",
		"só comentários":   "# ID=ubuntu\n\n",
		"binário":          "\x00\x01\xff=\x00\n\r\r",
	}
	for name, in := range cases {
		if d := ParseOSRelease(in); d.ID != "" && name != "aspas sem fechar" {
			t.Errorf("%s: %+v", name, d)
		}
	}
	if d := ParseOSRelease("export ID=fedora\r\nID=arch\r\n"); d.ID != "arch" {
		t.Fatalf("export/CRLF, última vence: %+v", d)
	}
}

func TestDetectSORecusado(t *testing.T) {
	_, err := SystemDetector{GOOS: "plan9", GOARCH: "amd64", Getenv: env(nil)}.Detect(context.Background())
	if !errors.Is(err, ErrUnsupportedOS) {
		t.Fatalf("err=%v", err)
	}
}

func TestComandosDeLeituraSaoReadOnlyEPorCaminhoAbsoluto(t *testing.T) {
	for _, c := range []interface{ String() string }{psCmd("x")} {
		if !strings.HasPrefix(c.String(), `"C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe"`) {
			t.Fatalf("powershell por caminho absoluto: %s", c)
		}
	}
	if !psCmd("x").ReadOnly {
		t.Fatal("psCmd precisa ser ReadOnly para executar no dry-run")
	}
	r := exectest.New(t).On("/usr/bin/sw_vers -productVersion", exectest.Exit(0, "15\n")).On("/usr/sbin/sysctl -n kern.boottime", exectest.Exit(0, "b\n"))
	_, _ = SystemDetector{Runner: r, GOOS: "darwin", GOARCH: "arm64", Getenv: env(nil), Euid: func() int { return 1 },
		CurrentUser: func() (string, error) { return "u", nil }}.Detect(context.Background())
	for _, c := range r.Calls() {
		if !c.ReadOnly {
			t.Errorf("%s sem ReadOnly", c)
		}
	}
}
