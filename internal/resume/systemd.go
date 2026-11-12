package resume

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/PiluTech/toolkit/internal/buildinfo"
	"github.com/PiluTech/toolkit/internal/exec"
)

// RootBinDir é onde a unit instala a cópia do binário que o ROOT executa no boot: um diretório de root
// (0755 root:root), nunca o ~/.local/bin de quem iniciou a receita (gravável pelo usuário = escalada de
// privilégio no boot).
const RootBinDir = "/usr/local/lib/toolkit"

// ErrUnsafeRootPath é devolvido quando o binário, o state ou o log que a unit (root) usaria ficam em
// um lugar que um usuário comum pode alterar. Nada é executado nesse caso.
var ErrUnsafeRootPath = errors.New("caminho gravável por não-root na retomada do boot")

// rootOnly diz se p fica numa árvore que só o root escreve por convenção (prefixos); caminho não
// limpo ou com ".." nunca passa.
func rootOnly(p string, prefixes ...string) bool {
	if p == "" || p != path.Clean(p) || !path.IsAbs(p) || slices.Contains(strings.Split(p, "/"), "..") {
		return false
	}
	for _, pre := range prefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// Systemd registra a retomada como unit systemd de SISTEMA, oneshot, no boot e depois de
// network-online.target. Roda como root, sem terminal (`toolkit resume --no-tui`); a saída vai para o
// journal e para o log. Tudo pelo Runner (sudo -n quando o processo não é root): nada é escrito direto.
type Systemd struct {
	Runner    exec.Runner
	IsRoot    bool
	BinDir    string // cópia do binário do ROOT; vazio = RootBinDir. Tem de ficar em /usr ou /opt
	StatePath string // ConditionPathExists: só roda se o state.json existe; tem de ficar em /var/lib
	UnitDir   string // padrão /etc/systemd/system
}

var _ Registrar = Systemd{}

// UnitName é o nome da unit: <name>-resume.service.
func UnitName() string { return buildinfo.Name + "-resume.service" }

func (s Systemd) unitPath() string {
	d := s.UnitDir
	if d == "" {
		d = "/etc/systemd/system"
	}
	return d + "/" + UnitName()
}

func (s Systemd) binDir() string {
	if s.BinDir == "" {
		return RootBinDir
	}
	return s.BinDir
}

func (s Systemd) exe() string { return s.binDir() + "/" + buildinfo.Name }

// checkRootPaths recusa binário, state ou log em lugar gravável por não-root: a unit roda como root,
// então tudo o que ela executa ou lê tem de ser de root. Um usuário sem privilégio que controle o
// binário (ou o state) controla o root no boot.
func (s Systemd) checkRootPaths(sp Spec) error {
	if !rootOnly(s.binDir(), "/usr/", "/opt/") {
		return fmt.Errorf("%w: o binário da retomada (%s) deve ficar em /usr ou /opt", ErrUnsafeRootPath, s.binDir())
	}
	if !rootOnly(s.StatePath, "/var/lib/") {
		return fmt.Errorf("%w: o state (%s) deve ficar em /var/lib; receita com reboot no Linux exige rodar com sudo", ErrUnsafeRootPath, s.StatePath)
	}
	if sp.LogPath != "" && !rootOnly(sp.LogPath, "/var/log/", "/var/lib/") {
		return fmt.Errorf("%w: o log (%s) deve ficar em /var/log ou /var/lib", ErrUnsafeRootPath, sp.LogPath)
	}
	return nil
}

func (s Systemd) cmd(name string, readOnly bool, args ...string) exec.Cmd {
	return exec.Cmd{Name: name, Args: args, Elevate: !s.IsRoot && !readOnly, ReadOnly: readOnly}
}

// UnitFile monta o conteúdo da unit (público para o teste conferir o texto).
func (s Systemd) UnitFile(sp Spec) (string, error) {
	if err := ValidArgs(sp.Args); err != nil {
		return "", err
	}
	if err := s.checkRootPaths(sp); err != nil {
		return "", err
	}
	paths := []string{s.exe(), s.StatePath}
	if sp.LogPath != "" {
		paths = append(paths, sp.LogPath)
	}
	for _, p := range paths {
		if err := validPath(p, true); err != nil {
			return "", err
		}
	}
	args := slices.Clone(sp.Args)
	if !slices.Contains(args, "--no-tui") {
		args = append(args, "--no-tui")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "[Unit]\nDescription=%s: continuar a receita depois do reboot\n", buildinfo.Name)
	b.WriteString("Wants=network-online.target\nAfter=network-online.target\n")
	fmt.Fprintf(&b, "ConditionPathExists=%s\n\n", s.StatePath)
	// ProtectHome: mesmo por engano, o processo root não lê nem executa nada de /home.
	b.WriteString("[Service]\nType=oneshot\nProtectHome=true\n")
	fmt.Fprintf(&b, "ExecStart=%s %s\n", s.exe(), strings.Join(args, " "))
	if sp.LogPath != "" {
		fmt.Fprintf(&b, "StandardOutput=append:%s\nStandardError=append:%s\n", sp.LogPath, sp.LogPath)
	}
	b.WriteString("\n[Install]\nWantedBy=multi-user.target\n")
	return b.String(), nil
}

// Register instala o binário, grava a unit, recarrega o systemd e habilita (enable, não start: ela roda no boot).
func (s Systemd) Register(ctx context.Context, sp Spec) error {
	unit, err := s.UnitFile(sp)
	if err != nil {
		return err
	}
	if sp.Executable == "" {
		return errors.New("falta o caminho do binário")
	}
	if err := validPath(sp.Executable, false); err != nil {
		return err
	}
	steps := []exec.Cmd{s.cmd("install", false, "-d", "-m", "0755", "-o", "root", "-g", "root", s.binDir())}
	if path.Clean(sp.Executable) != path.Clean(s.exe()) {
		steps = append(steps, s.cmd("install", false, "-m", "0755", "-o", "root", "-g", "root", "--", sp.Executable, s.exe()))
	}
	write := s.cmd("tee", false, "--", s.unitPath())
	write.Stdin = strings.NewReader(unit)
	steps = append(steps, write,
		s.cmd("chmod", false, "0644", "--", s.unitPath()),
		s.cmd("systemctl", false, "daemon-reload"),
		s.cmd("systemctl", false, "enable", "--", UnitName()),
	)
	for _, c := range steps {
		if err := runOK(ctx, s.Runner, c, "registrar a retomada ("+c.Name+")"); err != nil {
			return err
		}
	}
	return nil
}

// Unregister desabilita e apaga a unit. Não registrada = sucesso.
func (s Systemd) Unregister(ctx context.Context) error {
	reg, err := s.IsRegistered(ctx)
	if err != nil {
		return err
	}
	if !reg {
		return nil
	}
	for _, c := range []exec.Cmd{
		s.cmd("systemctl", false, "disable", "--", UnitName()),
		s.cmd("rm", false, "-f", "--", s.unitPath()),
		s.cmd("systemctl", false, "daemon-reload"),
	} {
		if err := runOK(ctx, s.Runner, c, "remover a retomada ("+c.Name+")"); err != nil {
			return err
		}
	}
	return nil
}

// IsRegistered diz se o arquivo da unit existe (`test -f`, leitura).
func (s Systemd) IsRegistered(ctx context.Context) (bool, error) {
	res, err := s.Runner.Run(ctx, s.cmd("test", true, "-f", s.unitPath()))
	if err != nil {
		return false, err
	}
	switch res.ExitCode {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, fmt.Errorf("consultar a unit: código %d", res.ExitCode)
}
