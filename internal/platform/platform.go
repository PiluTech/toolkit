// Package platform detecta o sistema em que o toolkit roda e resolve os diretórios de estado, log e binário.
// Nada aqui usa build tags: as leituras passam pelo exec.Runner e por funções injetáveis,
// então tudo é testável em qualquer SO.
package platform

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"runtime"
	"strings"

	"github.com/PiluTech/toolkit/internal/exec"
)

// OS é o sistema operacional.
type OS string

// Sistemas suportados.
const (
	Windows OS = "windows"
	Linux   OS = "linux"
	Darwin  OS = "darwin"
)

// Info descreve a máquina.
type Info struct {
	OS       OS
	Arch     string   // amd64 | arm64 (outros valores passam sem alteração)
	Distro   Distro   // vazio fora do Linux
	Version  string   // build do Windows, versão do macOS, VERSION_ID no Linux
	IsAdmin  bool     // token elevado (Windows) / euid 0 (Unix)
	User     string   // usuário real: SUDO_USER só se euid==0, o usuário existe e o SUDO_UID bate; senão o dono do processo
	BootID   string   // muda a cada boot; detecta se o reboot aconteceu
	Managers []string // gerenciadores no PATH (informativo; quem decide é Provider.Available)
}

// Distro vem do /etc/os-release.
type Distro struct {
	ID        string   // ubuntu, fedora, arch...
	IDLike    []string // debian, rhel...
	VersionID string
}

// Detector detecta a máquina atual.
type Detector interface {
	Detect(ctx context.Context) (Info, error)
}

// KnownManagers são os executáveis que o diagnóstico procura no PATH.
var KnownManagers = []string{"winget", "brew", "apt-get", "dnf", "pacman", "flatpak"}

// SystemDetector é o Detector real. Os campos são injetáveis para teste; zero values usam o sistema.
type SystemDetector struct {
	Runner     exec.Runner
	GOOS       string
	GOARCH     string
	Getenv     func(string) string
	ReadFile   func(string) ([]byte, error)
	Euid       func() int
	IsElevated func() bool // Windows: token elevado (a implementação real vem do pacote elevate)
	// LookupUser devolve o uid (texto) do usuário nomeado; CurrentUser, o nome do dono do processo.
	// Zero values: os/user.Lookup e os/user.Current.
	LookupUser  func(name string) (uid string, err error)
	CurrentUser func() (name string, err error)
}

func (d SystemDetector) goos() string {
	if d.GOOS != "" {
		return d.GOOS
	}
	return runtime.GOOS
}

func (d SystemDetector) goarch() string {
	if d.GOARCH != "" {
		return d.GOARCH
	}
	return runtime.GOARCH
}

func (d SystemDetector) getenv(k string) string {
	if d.Getenv != nil {
		return d.Getenv(k)
	}
	return os.Getenv(k)
}

func (d SystemDetector) readFile(p string) ([]byte, error) {
	if d.ReadFile != nil {
		return d.ReadFile(p)
	}
	return os.ReadFile(p)
}

func (d SystemDetector) euid() int {
	if d.Euid != nil {
		return d.Euid()
	}
	return os.Geteuid()
}

// Detect monta o Info. Falha de uma leitura opcional (versão, boot ID) deixa o campo vazio,
// em vez de derrubar o programa: o diagnóstico mostra o que conseguiu. SO fora de windows/linux/darwin
// devolve ErrUnsupportedOS (com Info.OS preenchido). Os comandos de leitura vão por caminho absoluto e
// ReadOnly, para não executar um binário plantado no PATH (importante quando admin) e rodar no dry-run.
func (d SystemDetector) Detect(ctx context.Context) (Info, error) {
	info := Info{OS: OS(d.goos()), Arch: NormalizeArch(d.goarch())}
	switch info.OS {
	case Linux, Darwin, Windows:
	default:
		return info, fmt.Errorf("%w: %q", ErrUnsupportedOS, info.OS)
	}
	info.User = d.user(info.OS)

	switch info.OS {
	case Linux:
		if b, err := d.readFile("/etc/os-release"); err == nil {
			info.Distro = ParseOSRelease(string(b))
			info.Version = info.Distro.VersionID
		}
		if b, err := d.readFile("/proc/sys/kernel/random/boot_id"); err == nil {
			info.BootID = strings.TrimSpace(string(b))
		}
		info.IsAdmin = d.euid() == 0
	case Darwin:
		info.Version = d.read(ctx, exec.Cmd{Name: "/usr/bin/sw_vers", Args: []string{"-productVersion"}, ReadOnly: true})
		info.BootID = d.read(ctx, exec.Cmd{Name: "/usr/sbin/sysctl", Args: []string{"-n", "kern.boottime"}, ReadOnly: true})
		info.IsAdmin = d.euid() == 0
	case Windows:
		info.Version = d.read(ctx, psCmd("(Get-CimInstance Win32_OperatingSystem).BuildNumber"))
		info.BootID = d.read(ctx, psCmd("(Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToString('o')"))
		info.IsAdmin = d.IsElevated != nil && d.IsElevated()
	}

	if d.Runner != nil {
		for _, m := range KnownManagers {
			if _, err := d.Runner.LookPath(m); err == nil {
				info.Managers = append(info.Managers, m)
			}
		}
	}
	return info, nil
}

// windowsPowerShell é fixo: um powershell.exe achado no PATH poderia ser plantado.
const windowsPowerShell = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`

func psCmd(script string) exec.Cmd {
	return exec.Cmd{Name: windowsPowerShell, Args: []string{"-NoProfile", "-NonInteractive", "-Command", script}, ReadOnly: true}
}

// read executa um comando de leitura e devolve a saída aparada, ou "" se falhar.
func (d SystemDetector) read(ctx context.Context, c exec.Cmd) string {
	if d.Runner == nil {
		return ""
	}
	res, err := d.Runner.Run(ctx, c)
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(string(res.Stdout))
}

var (
	unixUserRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}\$?$`)
	osIDRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

// validUsername aceita só o formato de nome de usuário Unix (sem barras, espaços nem ".." no começo).
func validUsername(n string) bool { return unixUserRE.MatchString(n) && !strings.Contains(n, "..") }

func (d SystemDetector) lookupUser(name string) (string, error) {
	if d.LookupUser != nil {
		return d.LookupUser(name)
	}
	u, err := user.Lookup(name)
	if err != nil {
		return "", err
	}
	return u.Uid, nil
}

func (d SystemDetector) currentUser() (string, error) {
	if d.CurrentUser != nil {
		return d.CurrentUser()
	}
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

// user devolve o usuário real. SUDO_USER (variável que qualquer chamador pode forjar) só vale com euid 0
// e se o nome tem formato válido, o usuário existe e o SUDO_UID bate com o uid dele. Fora disso vale o
// dono do processo (os/user.Current); as variáveis USER/USERNAME nunca são usadas.
func (d SystemDetector) user(goos OS) string {
	if goos != Windows && d.euid() == 0 {
		if su := d.getenv("SUDO_USER"); validUsername(su) {
			if uid, err := d.lookupUser(su); err == nil && uid != "" && uid == d.getenv("SUDO_UID") {
				return su
			}
		}
	}
	name, err := d.currentUser()
	if err != nil {
		return ""
	}
	if i := strings.LastIndexByte(name, '\\'); i >= 0 { // Windows: DOMINIO\usuario
		name = name[i+1:]
	}
	if name == "" || len(name) > 256 || strings.ContainsAny(name, "/\x00\r\n") || strings.Contains(name, "..") {
		return ""
	}
	return name
}

// NormalizeArch converte nomes de arquitetura para amd64 ou arm64 quando aplicável.
func NormalizeArch(a string) string {
	switch strings.ToLower(a) {
	case "x86_64", "x64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	}
	return a
}

// ParseOSRelease lê o conteúdo de /etc/os-release.
func ParseOSRelease(content string) Distro {
	var d Distro
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		k = strings.TrimSpace(strings.TrimPrefix(k, "export "))
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "ID":
			// minúsculas (o catálogo e as comparações assumem isso); formato fora do padrão do os-release vira vazio
			if v = strings.ToLower(v); osIDRE.MatchString(v) {
				d.ID = v
			} else {
				d.ID = ""
			}
		case "ID_LIKE":
			d.IDLike = strings.Fields(strings.ToLower(v))
		case "VERSION_ID":
			d.VersionID = v
		}
	}
	return d
}
