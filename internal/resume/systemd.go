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

// Systemd registra a retomada como unit systemd de SISTEMA, oneshot, no boot e depois de
// network-online.target. Roda como root, sem terminal (`toolkit resume --no-tui`); a saída vai para o
// journal e para o log. Tudo pelo Runner (sudo -n quando o processo não é root): nada é escrito direto.
type Systemd struct {
	Runner    exec.Runner
	IsRoot    bool
	BinDir    string // o binário é instalado aqui antes de registrar
	StatePath string // ConditionPathExists: só roda se o state.json existe
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

func (s Systemd) exe() string { return s.BinDir + "/" + buildinfo.Name }

func (s Systemd) cmd(name string, readOnly bool, args ...string) exec.Cmd {
	return exec.Cmd{Name: name, Args: args, Elevate: !s.IsRoot && !readOnly, ReadOnly: readOnly}
}

// UnitFile monta o conteúdo da unit (público para o teste conferir o texto).
func (s Systemd) UnitFile(sp Spec) (string, error) {
	if err := ValidArgs(sp.Args); err != nil {
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
	b.WriteString("[Service]\nType=oneshot\n")
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
	steps := []exec.Cmd{s.cmd("install", false, "-d", "-m", "0755", s.BinDir)}
	if path.Clean(sp.Executable) != path.Clean(s.exe()) {
		steps = append(steps, s.cmd("install", false, "-m", "0755", "--", sp.Executable, s.exe()))
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
