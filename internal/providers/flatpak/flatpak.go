// Package flatpak é o provider do Flatpak. A instalação é de SISTEMA (--system, com sudo -n); o flatpak não
// lista versões utilizáveis, então só a atual (ListVersions = ["latest"]) e versão fixada é recusada.
// Não adiciona remotes sozinho: se o remote não existe, a mensagem diz como adicioná-lo.
//
// Limites documentados: (1) só instala no SISTEMA (--system); não há --user, então sem sudo a instalação falha com
// ErrNeedsAdmin. (2) "já instalado" vale também para a instalação --user: "flatpak list" mostra as duas.
// (3) o -y baixa também os runtimes e as dependências do app (podem ser GB): a CLI avisa disso.
package flatpak

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/providers"
)

const (
	defaultRemote = "flathub"
	timeout       = 30 * time.Minute
)

// env: a classificação dos erros lê texto do flatpak, que não pode depender do idioma do sistema.
var env = []string{"LC_ALL=C"}

// Provider implementa providers.Provider para o flatpak.
type Provider struct{ rn providers.Runners }

// New cria o provider sobre o Runner.
func New(r exec.Runner) *Provider { return &Provider{rn: providers.NewRunners(r)} }

// Name devolve "flatpak".
func (*Provider) Name() string { return catalog.ProviderFlatpak }

// Available diz se o flatpak está no PATH.
func (p *Provider) Available(context.Context) bool {
	_, err := p.rn.Read.LookPath("flatpak")
	return err == nil
}

// Validate aceita id (obrigatório) e source (o remote). Sem "/" nem "@" e sem sufixo de arquivo: o
// "flatpak install" lê "x.flatpakref", "x.flatpak" e caminhos como arquivos locais, não como id.
func (*Provider) Validate(ref providers.PackageRef) error {
	if err := providers.OnlyFields(ref, "id", "source"); err != nil {
		return err
	}
	if err := checkToken("id", ref.ID); err != nil {
		return err
	}
	if ref.Source != "" {
		return checkToken("source", ref.Source)
	}
	return nil
}

func checkToken(field, s string) error {
	if err := providers.CheckName(field, s); err != nil {
		return err
	}
	low := strings.ToLower(s)
	if strings.ContainsAny(s, "/@") || strings.HasSuffix(low, ".flatpak") || strings.HasSuffix(low, ".flatpakref") || strings.HasSuffix(low, ".flatpakrepo") {
		return fmt.Errorf("%w: %s %q (o flatpak lê / e .flatpak* como arquivo)", providers.ErrInvalidRef, field, s)
	}
	return nil
}

func remote(ref providers.PackageRef) string {
	if ref.Source != "" {
		return ref.Source
	}
	return defaultRemote
}

func listCmd() exec.Cmd {
	return exec.Cmd{Name: "flatpak", Args: []string{"list", "--app", "--columns=application,version"}, Env: env, ReadOnly: true, Timeout: time.Minute}
}

func remoteInfoCmd(ref providers.PackageRef) exec.Cmd {
	return exec.Cmd{Name: "flatpak", Args: []string{"remote-info", remote(ref), ref.ID}, Env: env, ReadOnly: true, Timeout: 2 * time.Minute}
}

func installCmd(ref providers.PackageRef) exec.Cmd {
	return exec.Cmd{Name: "flatpak", Args: []string{"install", "--system", "-y", "--noninteractive", remote(ref), ref.ID}, Env: env, Elevate: true, Timeout: timeout}
}

// IsInstalled roda "flatpak list --app --columns=application,version" (ReadOnly) e procura o id exato.
func (p *Provider) IsInstalled(ctx context.Context, ref providers.PackageRef) (bool, string, error) {
	if err := p.Validate(ref); err != nil {
		return false, "", err
	}
	res, err := p.rn.Read.Run(ctx, listCmd())
	if err != nil {
		return false, "", err
	}
	if res.ExitCode != 0 {
		return false, "", fmt.Errorf("flatpak list saiu com código %d: %s", res.ExitCode, providers.Snippet(res.Stderr))
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		cols := strings.Split(line, "\t")
		if strings.TrimSpace(cols[0]) != ref.ID {
			continue
		}
		v := ""
		if len(cols) > 1 {
			v = strings.TrimSpace(cols[1])
		}
		if !providers.ValidVersion(v) {
			v = ""
		}
		return true, v, nil
	}
	return false, "", nil
}

// ListVersions confere que o app existe no remote (remote-info, ReadOnly) e devolve ["latest"].
func (p *Provider) ListVersions(ctx context.Context, ref providers.PackageRef) ([]string, error) {
	if err := p.Validate(ref); err != nil {
		return nil, err
	}
	res, err := p.rn.Read.Run(ctx, remoteInfoCmd(ref))
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("flatpak remote-info: %w", classify(res, ref))
	}
	return []string{providers.Latest}, nil
}

// SupportsVersion recusa qualquer versão: o flatpak instala o commit atual do remote.
func (*Provider) SupportsVersion(_ providers.PackageRef, version string) error {
	return fmt.Errorf("%w: o flatpak só instala a versão atual do remote (%q pedida)", providers.ErrVersionUnsupported, version)
}

// Install roda "flatpak install --system -y --noninteractive remote id" com sudo -n. "já instalado" volta
// como AlreadyInstalled.
func (p *Provider) Install(ctx context.Context, ref providers.PackageRef, version string) (providers.InstallResult, error) {
	if err := providers.CheckVersionArg(version); err != nil {
		return providers.InstallResult{}, err
	}
	if version != "" && version != providers.Latest {
		return providers.InstallResult{}, p.SupportsVersion(ref, version)
	}
	if err := p.Validate(ref); err != nil {
		return providers.InstallResult{}, err
	}
	res, err := p.rn.Mutate.Run(ctx, installCmd(ref))
	if err != nil {
		if errors.Is(err, exec.ErrNotElevated) {
			return providers.InstallResult{}, fmt.Errorf("flatpak install: %w (o sudo -n não tem credencial)", providers.ErrNeedsAdmin)
		}
		return providers.InstallResult{}, err
	}
	if res.ExitCode != 0 {
		if strings.Contains(strings.ToLower(string(res.Stderr)), "already installed") {
			return providers.InstallResult{AlreadyInstalled: true}, nil
		}
		return providers.InstallResult{}, fmt.Errorf("flatpak install saiu com código %d: %w", res.ExitCode, classify(res, ref))
	}
	return providers.InstallResult{}, nil
}

// remoteNotFoundRe casa a mensagem do remote ausente ("Remote "flathub" not found"), e não qualquer texto que
// tenha "remote" e "not found" em pontos diferentes.
var remoteNotFoundRe = regexp.MustCompile(`(?i)remote\s+["'‘]?[A-Za-z0-9._-]+["'’]?\s+not found`)

func classify(res exec.Result, ref providers.PackageRef) error {
	snippet := providers.Snippet(res.Stderr)
	low := strings.ToLower(string(res.Stderr))
	switch {
	case strings.Contains(low, "no remote refs found") || strings.Contains(low, "nothing matches"):
		return fmt.Errorf("%w: %s", providers.ErrNotFound, snippet)
	case remoteNotFoundRe.MatchString(string(res.Stderr)):
		return fmt.Errorf("%w: o remote %q não existe nesta máquina (adicione com \"flatpak remote-add --if-not-exists %s <url do .flatpakrepo>\"): %s",
			providers.ErrNotFound, remote(ref), remote(ref), snippet)
	case strings.Contains(low, "permission denied") || strings.Contains(low, "not allowed") || strings.Contains(low, "insufficient permissions"):
		return fmt.Errorf("%w: %s", providers.ErrNeedsAdmin, snippet)
	}
	if snippet == "" {
		return errors.New("sem mensagem de erro")
	}
	return errors.New(snippet)
}
