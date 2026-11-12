package resume

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
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
	BinDir    string                       // cópia do binário do ROOT; vazio = RootBinDir. Tem de ficar em /usr ou /opt
	StatePath string                       // ConditionPathExists: só roda se o state.json existe; tem de ficar em /var/lib
	UnitDir   string                       // padrão /etc/systemd/system
	ReadFile  func(string) ([]byte, error) // padrão os.ReadFile (injetável nos testes)
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
//
// O binário de origem é lido UMA vez (os bytes e o SHA-256 vêm da mesma leitura) e entregue ao `tee` por
// stdin: não há segunda abertura do arquivo do usuário que ele possa trocar no meio (TOCTOU). Antes do
// `enable` o SHA-256 do destino é conferido de novo, e o state tem de ser de root (dono e modo), não só
// estar num prefixo certo.
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
	read := s.ReadFile
	if read == nil {
		read = os.ReadFile
	}
	bin, err := read(sp.Executable)
	if err != nil {
		return fmt.Errorf("ler o binário: %w", err)
	}
	sum := sha256.Sum256(bin)
	want := hex.EncodeToString(sum[:])

	if err := s.checkStateOwnership(ctx); err != nil {
		return err
	}

	tmp := s.exe() + ".new"
	steps := []exec.Cmd{s.cmd("install", false, "-d", "-m", "0755", "-o", "root", "-g", "root", s.binDir())}
	if path.Clean(sp.Executable) != path.Clean(s.exe()) {
		put := s.cmd("tee", false, "--", tmp)
		put.Stdin = bytes.NewReader(bin)
		steps = append(steps, put,
			s.cmd("chown", false, "root:root", "--", tmp),
			s.cmd("chmod", false, "0755", "--", tmp),
		)
	}
	for _, c := range steps {
		if err := runOK(ctx, s.Runner, c, "registrar a retomada ("+c.Name+")"); err != nil {
			return err
		}
	}
	if path.Clean(sp.Executable) != path.Clean(s.exe()) {
		if err := s.verifyHash(ctx, tmp, want); err != nil {
			return err
		}
		if err := runOK(ctx, s.Runner, s.cmd("mv", false, "-f", "--", tmp, s.exe()), "registrar a retomada (mv)"); err != nil {
			return err
		}
	}
	write := s.cmd("tee", false, "--", s.unitPath())
	write.Stdin = strings.NewReader(unit)
	for _, c := range []exec.Cmd{
		write,
		s.cmd("chmod", false, "0644", "--", s.unitPath()),
		s.cmd("systemctl", false, "daemon-reload"),
	} {
		if err := runOK(ctx, s.Runner, c, "registrar a retomada ("+c.Name+")"); err != nil {
			return err
		}
	}
	// o destino final, conferido de novo logo antes de habilitar
	if err := s.verifyHash(ctx, s.exe(), want); err != nil {
		return err
	}
	return runOK(ctx, s.Runner, s.cmd("systemctl", false, "enable", "--", UnitName()), "registrar a retomada (systemctl)")
}

// ErrBinaryMismatch: o binário que ficou em RootBinDir não tem o SHA-256 do que foi lido.
var ErrBinaryMismatch = errors.New("o binário instalado não confere com o original (SHA-256)")

// verifyHash confere o SHA-256 de um arquivo de root (sha256sum, leitura).
func (s Systemd) verifyHash(ctx context.Context, file, want string) error {
	res, err := s.Runner.Run(ctx, s.cmd("sha256sum", true, "--", file))
	if err != nil {
		return fmt.Errorf("conferir o binário: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("conferir o binário: código %d: %s", res.ExitCode, snippet(res))
	}
	f := strings.Fields(string(res.Stdout))
	if len(f) == 0 || !strings.EqualFold(f[0], want) {
		return fmt.Errorf("%w: %s", ErrBinaryMismatch, file)
	}
	return nil
}

// checkStateOwnership exige que o state e todos os diretórios acima dele, até /var/lib, sejam de root
// (uid 0) e sem escrita para grupo/outros: um prefixo em /var/lib não prova nada se o diretório for do usuário.
func (s Systemd) checkStateOwnership(ctx context.Context) error {
	p := s.StatePath
	for {
		res, err := s.Runner.Run(ctx, s.cmd("stat", true, "-c", "%u %a", "--", p))
		if err != nil {
			return fmt.Errorf("conferir o dono de %s: %w", p, err)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("%w: não consegui ler o dono/modo de %s (código %d)", ErrUnsafeRootPath, p, res.ExitCode)
		}
		var uid, mode int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(res.Stdout)), "%d %o", &uid, &mode); err != nil {
			return fmt.Errorf("%w: saída inesperada do stat em %s", ErrUnsafeRootPath, p)
		}
		if uid != 0 || mode&0o022 != 0 {
			return fmt.Errorf("%w: %s deve ser de root e sem escrita para grupo/outros (dono %d, modo %o)", ErrUnsafeRootPath, p, uid, mode)
		}
		if p == "/var/lib" {
			return nil
		}
		p = path.Dir(p)
	}
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
