// Package pacman é o provider do pacman (Arch e derivadas). O pacman só conhece a versão atual do repositório
// (instalar uma antiga seria atualização parcial, que o Arch não suporta): versão fixada = ErrVersionUnsupported.
// Nunca roda "pacman -Sy" sozinho.
package pacman

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/providers"
)

const timeout = 30 * time.Minute

// Provider implementa providers.Provider para o pacman.
type Provider struct{ rn providers.Runners }

// New cria o provider sobre o Runner.
func New(r exec.Runner) *Provider { return &Provider{rn: providers.NewRunners(r)} }

// Name devolve "pacman".
func (*Provider) Name() string { return catalog.ProviderPacman }

// Available diz se o pacman está no PATH.
func (p *Provider) Available(context.Context) bool {
	_, err := p.rn.Read.LookPath("pacman")
	return err == nil
}

// Validate aceita só package. "repo/pacote" é sintaxe do pacman (escolhe o repositório): ver providers.CheckName.
func (*Provider) Validate(ref providers.PackageRef) error {
	if err := providers.OnlyFields(ref, "package"); err != nil {
		return err
	}
	return providers.CheckName("package", ref.Package)
}

func queryCmd(ref providers.PackageRef) exec.Cmd {
	return exec.Cmd{Name: "pacman", Args: []string{"-Q", ref.Package}, ReadOnly: true, Timeout: time.Minute}
}

func infoCmd(ref providers.PackageRef) exec.Cmd {
	return exec.Cmd{Name: "pacman", Args: []string{"-Si", ref.Package}, ReadOnly: true, Timeout: time.Minute}
}

func installCmd(ref providers.PackageRef) exec.Cmd {
	return exec.Cmd{Name: "pacman", Args: []string{"-S", "--needed", "--noconfirm", ref.Package}, Elevate: true, Timeout: timeout}
}

// IsInstalled roda "pacman -Q pkg" (ReadOnly): "nome versão"; exit 1 "was not found" = não instalado.
func (p *Provider) IsInstalled(ctx context.Context, ref providers.PackageRef) (bool, string, error) {
	if err := p.Validate(ref); err != nil {
		return false, "", err
	}
	// -Q só conhece o nome, sem "repo/".
	q := ref
	if _, name, ok := strings.Cut(ref.Package, "/"); ok {
		q.Package = name
	}
	res, err := p.rn.Read.Run(ctx, queryCmd(q))
	if err != nil {
		return false, "", err
	}
	if res.ExitCode != 0 {
		if strings.Contains(string(res.Stderr), "was not found") {
			return false, "", nil
		}
		return false, "", fmt.Errorf("pacman -Q saiu com código %d: %s", res.ExitCode, providers.Snippet(res.Stderr))
	}
	f := strings.Fields(string(res.Stdout))
	if len(f) < 2 {
		return false, "", nil
	}
	v := f[1]
	if !providers.ValidVersion(v) {
		v = ""
	}
	return true, v, nil
}

// ListVersions confere que o pacote existe nos repositórios (pacman -Si, ReadOnly) e devolve ["latest"].
func (p *Provider) ListVersions(ctx context.Context, ref providers.PackageRef) ([]string, error) {
	if err := p.Validate(ref); err != nil {
		return nil, err
	}
	res, err := p.rn.Read.Run(ctx, infoCmd(ref))
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("pacman -Si: %w (%s)", providers.ErrNotFound, providers.Snippet(res.Stderr))
	}
	return []string{providers.Latest}, nil
}

// SupportsVersion recusa qualquer versão: só a atual do repositório.
func (*Provider) SupportsVersion(_ providers.PackageRef, version string) error {
	return fmt.Errorf("%w: o pacman só instala a versão atual do repositório (%q pedida; versão antiga é atualização parcial, sem suporte no Arch)", providers.ErrVersionUnsupported, version)
}

// Install roda "pacman -S --needed --noconfirm pkg" com sudo -n. Versão fixada = ErrVersionUnsupported.
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
			return providers.InstallResult{}, fmt.Errorf("pacman -S: %w (o sudo -n não tem credencial)", providers.ErrNeedsAdmin)
		}
		return providers.InstallResult{}, err
	}
	if res.ExitCode != 0 {
		return providers.InstallResult{}, fmt.Errorf("pacman -S saiu com código %d: %w", res.ExitCode, classify(res))
	}
	return providers.InstallResult{}, nil
}

func classify(res exec.Result) error {
	snippet := providers.Snippet(res.Stderr)
	low := strings.ToLower(string(res.Stderr))
	switch {
	case strings.Contains(low, "target not found"):
		return fmt.Errorf("%w: %s", providers.ErrNotFound, snippet)
	case strings.Contains(low, "unable to lock database") || strings.Contains(low, "db.lck"):
		return fmt.Errorf("%w: outro pacman em andamento (%s)", providers.ErrRetryable, snippet)
	case strings.Contains(low, "you cannot perform this operation unless you are root") || strings.Contains(low, "permission denied"):
		return fmt.Errorf("%w: %s", providers.ErrNeedsAdmin, snippet)
	}
	if snippet == "" {
		return errors.New("sem mensagem de erro")
	}
	return errors.New(snippet)
}
