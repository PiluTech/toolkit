// Package platform detecta o sistema em que o toolkit roda e resolve os diretórios de estado, log e binário.
// Nada aqui usa build tags: as leituras passam pelo exec.Runner e por funções injetáveis,
// então tudo é testável em qualquer SO.
package platform

import (
	"context"
	"os"
	"runtime"
	"strings"

	"github.com/PiluVitu/toolkit/internal/exec"
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
	User     string   // usuário real (SUDO_USER se rodando via sudo)
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
// em vez de derrubar o programa: o diagnóstico mostra o que conseguiu.
func (d SystemDetector) Detect(ctx context.Context) (Info, error) {
	info := Info{OS: OS(d.goos()), Arch: NormalizeArch(d.goarch())}
	info.User = d.user()

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
		info.Version = d.read(ctx, exec.Cmd{Name: "sw_vers", Args: []string{"-productVersion"}})
		info.BootID = d.read(ctx, exec.Cmd{Name: "sysctl", Args: []string{"-n", "kern.boottime"}})
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

func psCmd(script string) exec.Cmd {
	return exec.Cmd{Name: "powershell.exe", Args: []string{"-NoProfile", "-NonInteractive", "-Command", script}}
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

// user devolve o usuário real: SUDO_USER quando rodando via sudo.
func (d SystemDetector) user() string {
	for _, k := range []string{"SUDO_USER", "USER", "USERNAME"} {
		if v := d.getenv(k); v != "" {
			return v
		}
	}
	return ""
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
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			d.ID = v
		case "ID_LIKE":
			d.IDLike = strings.Fields(v)
		case "VERSION_ID":
			d.VersionID = v
		}
	}
	return d
}
