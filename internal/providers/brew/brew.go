// Package brew é o provider do Homebrew (macOS e Linux). Nunca usa sudo: o Homebrew roda como a conta dona
// da instalação e recusa root. Só instala a versão atual (fórmulas versionadas como python@3.12 são escolhidas
// pelo id, via template do catálogo).
package brew

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/providers"
)

// ErrRoot indica que o Homebrew não roda como root.
var ErrRoot = errors.New("o Homebrew não roda como root: rode o toolkit como o usuário dono da instalação, sem sudo")

// env: sem auto-update a cada pacote (um único "brew update" por execução, em Prepare) e sem dicas.
var env = []string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ENV_HINTS=1"}

const timeout = 30 * time.Minute

// Provider implementa providers.Provider e providers.Preparer para o brew.
type Provider struct {
	rn providers.Runners
	// Euid devolve o uid efetivo (os testes trocam). root = recusa.
	Euid func() int

	once    sync.Once
	prepErr error
}

// New cria o provider sobre o Runner.
func New(r exec.Runner) *Provider { return &Provider{rn: providers.NewRunners(r), Euid: os.Geteuid} }

// Name devolve "brew".
func (*Provider) Name() string { return catalog.ProviderBrew }

// Available diz se o brew está no PATH.
func (p *Provider) Available(context.Context) bool {
	_, err := p.rn.Read.LookPath("brew")
	return err == nil
}

// Validate exige exatamente um entre formula e cask, sem outros campos.
func (*Provider) Validate(ref providers.PackageRef) error {
	if err := providers.OnlyFields(ref, "formula", "cask"); err != nil {
		return err
	}
	if (ref.Formula == "") == (ref.Cask == "") {
		return fmt.Errorf("%w: brew exige exatamente um entre formula e cask", providers.ErrInvalidRef)
	}
	if ref.Formula != "" {
		return providers.CheckName("formula", ref.Formula)
	}
	return providers.CheckName("cask", ref.Cask)
}

func (p *Provider) notRoot() error {
	if p.Euid != nil && p.Euid() == 0 {
		return ErrRoot
	}
	return nil
}

// target devolve a flag (--formula ou --cask) e o nome.
func target(ref providers.PackageRef) (flag, name string) {
	if ref.Cask != "" {
		return "--cask", ref.Cask
	}
	return "--formula", ref.Formula
}

func readCmd(args ...string) exec.Cmd {
	return exec.Cmd{Name: "brew", Args: args, Env: env, ReadOnly: true, Timeout: 2 * time.Minute}
}

func listCmd(ref providers.PackageRef) exec.Cmd {
	flag, name := target(ref)
	return readCmd("list", "--versions", flag, name)
}

func infoCmd(ref providers.PackageRef) exec.Cmd {
	flag, name := target(ref)
	return readCmd("info", "--json=v2", flag, name)
}

func updateCmd() exec.Cmd {
	return exec.Cmd{Name: "brew", Args: []string{"update"}, Env: env, Timeout: timeout}
}

func installCmd(ref providers.PackageRef) exec.Cmd {
	flag, name := target(ref)
	return exec.Cmd{Name: "brew", Args: []string{"install", flag, name}, Env: env, Timeout: timeout}
}

// IsInstalled roda "brew list --versions" (ReadOnly). Saída "nome v1 v2": a última é a mais nova. Falha do
// comando (brew sai com 1 para o que não está instalado) = não instalado.
func (p *Provider) IsInstalled(ctx context.Context, ref providers.PackageRef) (bool, string, error) {
	if err := p.notRoot(); err != nil {
		return false, "", err
	}
	res, err := p.rn.Read.Run(ctx, listCmd(ref))
	if err != nil {
		return false, "", err
	}
	if res.ExitCode != 0 {
		return false, "", nil
	}
	fields := strings.Fields(string(res.Stdout))
	if len(fields) < 2 {
		return false, "", nil
	}
	v := fields[len(fields)-1]
	if !providers.ValidVersion(v) {
		v = ""
	}
	return true, v, nil
}

// ListVersions confere que o pacote existe (brew info --json=v2) e devolve ["latest"]: fórmula e cask
// oficiais expõem uma versão estável só.
func (p *Provider) ListVersions(ctx context.Context, ref providers.PackageRef) ([]string, error) {
	if err := p.notRoot(); err != nil {
		return nil, err
	}
	res, err := p.rn.Read.Run(ctx, infoCmd(ref))
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("brew info: %w (%s)", providers.ErrNotFound, providers.Snippet(res.Stderr))
	}
	var doc struct {
		Formulae []json.RawMessage `json:"formulae"`
		Casks    []json.RawMessage `json:"casks"`
	}
	if err := json.Unmarshal(res.Stdout, &doc); err != nil {
		return nil, fmt.Errorf("brew info: saída JSON inválida: %w", err)
	}
	if len(doc.Formulae)+len(doc.Casks) == 0 {
		return nil, fmt.Errorf("brew info: %w", providers.ErrNotFound)
	}
	return []string{providers.Latest}, nil
}

// SupportsVersion recusa qualquer versão: o brew só instala a atual.
func (*Provider) SupportsVersion(_ providers.PackageRef, version string) error {
	return fmt.Errorf("%w: o brew só instala a versão atual (%q pedida); para outra versão use uma fórmula versionada no id (ex.: python@3.12)", providers.ErrVersionUnsupported, version)
}

// Prepare roda um único "brew update" por execução (mutação).
func (p *Provider) Prepare(ctx context.Context) error {
	if err := p.notRoot(); err != nil {
		return err
	}
	p.once.Do(func() {
		res, err := p.rn.Mutate.Run(ctx, updateCmd())
		switch {
		case err != nil:
			p.prepErr = err
		case res.ExitCode != 0:
			p.prepErr = fmt.Errorf("brew update saiu com código %d: %s", res.ExitCode, providers.Snippet(res.Stderr))
		}
	})
	return p.prepErr
}

// Install roda "brew install --formula|--cask X". Versão fixada = ErrVersionUnsupported.
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
	if err := p.notRoot(); err != nil {
		return providers.InstallResult{}, err
	}
	res, err := p.rn.Mutate.Run(ctx, installCmd(ref))
	if err != nil {
		return providers.InstallResult{}, err
	}
	if res.ExitCode != 0 {
		stderr := providers.Snippet(res.Stderr)
		low := strings.ToLower(stderr)
		if strings.Contains(low, "no available formula") || strings.Contains(low, "no cask with this name") {
			return providers.InstallResult{}, fmt.Errorf("brew install: %w (%s)", providers.ErrNotFound, stderr)
		}
		return providers.InstallResult{}, fmt.Errorf("brew install saiu com código %d: %s", res.ExitCode, stderr)
	}
	return providers.InstallResult{}, nil
}
