// Package url é o provider "url": baixa um instalador de uma URL https do catálogo, com SHA-256 obrigatório, e o
// instala conforme o tipo (msi, exe, pkg, dmg, deb, rpm, zip, tar.gz; ver o pacote artifact).
//
// Limites documentados: (1) uma URL não tem nome de pacote para consultar, então IsInstalled devolve sempre
// "não instalado": rodar duas vezes baixa e instala de novo (os gerenciadores reais são idempotentes; este
// provider não sabe). (2) só existe a versão da URL (ListVersions = ["latest"]); versão escolhida pelo usuário
// vale só quando a URL tem {{version}} (o catálogo marca VersionInID) e o sha256 fixado cobre UMA versão: as
// outras falham no hash, em segurança. (3) o proxy do ambiente não é usado (pacote download).
package url

import (
	"context"
	"fmt"
	neturl "net/url"
	"runtime"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/download"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/providers"
	"github.com/PiluTech/toolkit/internal/providers/artifact"
)

// Options configura o provider; todos os campos têm padrão.
type Options struct {
	GOOS       string               // padrão runtime.GOOS
	Downloader *download.Downloader // padrão: cliente estrito do pacote download
	AppsDir    func() (string, error)
}

// Provider implementa providers.Provider para o tipo "url".
type Provider struct {
	goos string
	env  artifact.Env
}

// New cria o provider sobre o Runner. Se r for o DryRunRunner (exec.Planner), o provider SIMULA: não baixa nem
// executa, só registra o plano.
func New(r exec.Runner, opts Options) *Provider {
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	pl, _ := r.(exec.Planner)
	return &Provider{goos: goos, env: artifact.Env{GOOS: goos, Rn: providers.NewRunners(r), Planner: pl, Downloader: opts.Downloader, AppsDir: opts.AppsDir}}
}

// Name devolve "url".
func (*Provider) Name() string { return catalog.ProviderURL }

// Available é sempre verdadeiro: não depende de gerenciador instalado (a rede e os comandos do tipo são checados ao instalar).
func (*Provider) Available(context.Context) bool { return true }

func item(ref providers.PackageRef) (artifact.Item, error) {
	u, err := neturl.Parse(ref.URL)
	if err != nil {
		return artifact.Item{}, fmt.Errorf("%w: url: %w", providers.ErrInvalidRef, err)
	}
	return artifact.Item{AppID: ref.AppID, URL: ref.URL, SHA256: ref.SHA256, Type: ref.Type, Name: download.SafeName(u.Path), Args: ref.Args}, nil
}

// Validate exige url https válida (catalog.CheckURL), sha256 e um tipo instalável neste SO.
func (p *Provider) Validate(ref providers.PackageRef) error {
	if err := providers.OnlyFields(ref, "url", "sha256", "type", "args"); err != nil {
		return err
	}
	if err := catalog.CheckURL(ref.URL, false); err != nil {
		return fmt.Errorf("%w: %w", providers.ErrInvalidRef, err)
	}
	it, err := item(ref)
	if err != nil {
		return err
	}
	return artifact.ValidateItem(p.goos, it)
}

// IsInstalled devolve sempre "não instalado" (ver o comentário do pacote).
func (*Provider) IsInstalled(context.Context, providers.PackageRef) (bool, string, error) {
	return false, "", nil
}

// ListVersions devolve ["latest"].
func (*Provider) ListVersions(context.Context, providers.PackageRef) ([]string, error) {
	return []string{providers.Latest}, nil
}

// SupportsVersion recusa: a versão é a da URL.
func (*Provider) SupportsVersion(providers.PackageRef, string) error {
	return fmt.Errorf("%w: o provider url instala o que a URL aponta (use {{version}} na URL)", providers.ErrVersionUnsupported)
}

// Install baixa, confere o hash e instala.
func (p *Provider) Install(ctx context.Context, ref providers.PackageRef, version string) (providers.InstallResult, error) {
	if err := p.Validate(ref); err != nil {
		return providers.InstallResult{}, err
	}
	if version != "" && version != providers.Latest {
		return providers.InstallResult{}, p.SupportsVersion(ref, version)
	}
	it, err := item(ref)
	if err != nil {
		return providers.InstallResult{}, err
	}
	return p.env.Install(ctx, it)
}
