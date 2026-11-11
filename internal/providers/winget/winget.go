// Package winget é o provider do Windows Package Manager. Monta exec.Cmd e interpreta saída e exit codes
// (HRESULTs comparados como uint32). Nada aqui executa processo fora do exec.Runner.
package winget

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/providers"
)

// Exit codes do winget (https://github.com/microsoft/winget-cli/blob/master/doc/windows/package-manager/winget/returnCodes.md).
const (
	codeNoApplicationsFound   uint32 = 0x8A150014
	codeUpdateNotApplicable   uint32 = 0x8A15002B
	codePackageAlreadyInst    uint32 = 0x8A150061
	codeInstallAlreadyInst    uint32 = 0x8A15010D
	codeInstallDowngrade      uint32 = 0x8A15010E
	codeRebootRequiredFinish  uint32 = 0x8A150109
	codeRebootInitiated       uint32 = 0x8A15010B
	codeRebootRequiredInstall uint32 = 0x8A15010A
	codePackageInUse          uint32 = 0x8A150101
	codeFileInUse             uint32 = 0x8A150103
	codeInstallInProgress     uint32 = 0x8A150102
	codeNoApplicableInstaller uint32 = 0x8A150010
	codeHashMismatch          uint32 = 0x8A150011
	codeBlockedByPolicy       uint32 = 0x8A15010F
	codeMissingDependency     uint32 = 0x8A150104
	codeDiskFull              uint32 = 0x8A150105
	codeNoNetwork             uint32 = 0x8A150107
)

const (
	defaultSource = "winget"
	maxAttempts   = 3
	timeout       = 30 * time.Minute
)

// Provider implementa providers.Provider para o winget.
type Provider struct {
	rn providers.Runners // Read: só leitura; Mutate: só mutação (guardas do exec)
	// Sleep espera entre as tentativas de INSTALL_IN_PROGRESS; os testes trocam por uma versão instantânea.
	Sleep func(ctx context.Context, d time.Duration) error
}

// New cria o provider sobre o Runner.
func New(r exec.Runner) *Provider {
	return &Provider{rn: providers.NewRunners(r), Sleep: sleep}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Name devolve "winget".
func (*Provider) Name() string { return catalog.ProviderWinget }

// Available diz se o winget está no PATH.
func (p *Provider) Available(context.Context) bool {
	_, err := p.rn.Read.LookPath("winget")
	return err == nil
}

// Validate aceita só id (obrigatório) e source.
func (*Provider) Validate(ref providers.PackageRef) error {
	if err := providers.OnlyFields(ref, "id", "source"); err != nil {
		return err
	}
	if err := providers.CheckName("id", ref.ID); err != nil {
		return err
	}
	if ref.Source != "" {
		return providers.CheckName("source", ref.Source)
	}
	return nil
}

func source(ref providers.PackageRef) string {
	if ref.Source != "" {
		return ref.Source
	}
	return defaultSource
}

// common: não interativo e sem perguntas de acordo de fonte.
var common = []string{"--accept-source-agreements", "--disable-interactivity"}

func listCmd(ref providers.PackageRef) exec.Cmd {
	args := append([]string{"list", "--id", ref.ID, "-e"}, common...)
	return exec.Cmd{Name: "winget", Args: args, ReadOnly: true, Timeout: 2 * time.Minute}
}

func showVersionsCmd(ref providers.PackageRef) exec.Cmd {
	args := append([]string{"show", "--id", ref.ID, "-e", "--versions", "--source", source(ref)}, common...)
	return exec.Cmd{Name: "winget", Args: args, ReadOnly: true, Timeout: 2 * time.Minute}
}

func installCmd(ref providers.PackageRef, version string) exec.Cmd {
	args := []string{"install", "--id", ref.ID, "-e", "--source", source(ref), "--silent",
		"--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"}
	if version != "" && version != providers.Latest {
		args = append(args, "--version", version)
	}
	// Sem Elevate: o winget pede o UAC sozinho quando o instalador exige. O relançamento elevado do toolkit
	// (fase 3) cobre o resto.
	return exec.Cmd{Name: "winget", Args: args, Timeout: timeout}
}

// IsInstalled roda "winget list --id X -e" (ReadOnly). 0x8A150014 = não instalado.
func (p *Provider) IsInstalled(ctx context.Context, ref providers.PackageRef) (bool, string, error) {
	res, err := p.rn.Read.Run(ctx, listCmd(ref))
	if err != nil {
		return false, "", err
	}
	switch code := uint32(res.ExitCode); code {
	case 0:
		rows := ParseTable(res.Stdout)
		for _, row := range rows {
			if len(row) >= 3 && strings.EqualFold(row[1], ref.ID) {
				return true, cleanVersion(row[2]), nil
			}
		}
		return false, "", nil
	case codeNoApplicationsFound:
		return false, "", nil
	default:
		return false, "", fmt.Errorf("winget list: %w", exitError(code, res))
	}
}

// ListVersions roda "winget show --id X -e --versions" (ReadOnly).
func (p *Provider) ListVersions(ctx context.Context, ref providers.PackageRef) ([]string, error) {
	res, err := p.rn.Read.Run(ctx, showVersionsCmd(ref))
	if err != nil {
		return nil, err
	}
	if code := uint32(res.ExitCode); code != 0 {
		return nil, fmt.Errorf("winget show: %w", exitError(code, res))
	}
	var out []string
	for _, row := range ParseTable(res.Stdout) {
		if len(row) > 0 && providers.ValidVersion(row[0]) {
			out = append(out, row[0])
		}
	}
	if len(out) == 0 {
		return []string{providers.Latest}, nil
	}
	return out, nil
}

// SupportsVersion aceita sempre: o winget instala versões específicas (--version), então sempre aceita.
func (*Provider) SupportsVersion(providers.PackageRef, string) error { return nil }

// Install instala e traduz o exit code. version vazia ou "latest" = a padrão.
func (p *Provider) Install(ctx context.Context, ref providers.PackageRef, version string) (providers.InstallResult, error) {
	if version != "" && version != providers.Latest && !providers.ValidVersion(version) {
		return providers.InstallResult{}, fmt.Errorf("%w: versão %q", providers.ErrInvalidRef, version)
	}
	if err := p.Validate(ref); err != nil {
		return providers.InstallResult{}, err
	}
	var last error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := p.rn.Mutate.Run(ctx, installCmd(ref, version))
		if err != nil {
			return providers.InstallResult{}, err
		}
		code := uint32(res.ExitCode)
		out, err := interpretInstall(code, version, res)
		if code != codeInstallInProgress || err == nil {
			return out, err
		}
		last = err
		if attempt < maxAttempts {
			if serr := p.Sleep(ctx, time.Duration(attempt)*5*time.Second); serr != nil {
				return providers.InstallResult{}, serr
			}
		}
	}
	return providers.InstallResult{}, last
}

func interpretInstall(code uint32, version string, res exec.Result) (providers.InstallResult, error) {
	switch code {
	case 0:
		return providers.InstallResult{Version: versionIfPinned(version)}, nil
	case codeUpdateNotApplicable, codePackageAlreadyInst:
		return providers.InstallResult{AlreadyInstalled: true}, nil
	case codeInstallAlreadyInst:
		if version == "" || version == providers.Latest {
			return providers.InstallResult{AlreadyInstalled: true}, nil
		}
		return providers.InstallResult{}, fmt.Errorf("outra versão já está instalada e difere de %s (desinstale antes)", version)
	case codeRebootRequiredFinish, codeRebootInitiated:
		return providers.InstallResult{RebootRequired: true, Version: versionIfPinned(version)}, nil
	}
	return providers.InstallResult{}, fmt.Errorf("winget install: %w", exitError(code, res))
}

func versionIfPinned(v string) string {
	if v == providers.Latest {
		return ""
	}
	return v
}

// exitError traduz um exit code de falha em erro tipado, com mensagem em pt-BR.
func exitError(code uint32, res exec.Result) error {
	switch code {
	case codeNoApplicationsFound:
		return fmt.Errorf("%w (0x%08X)", providers.ErrNotFound, code)
	case codeNoApplicableInstaller:
		return fmt.Errorf("%w (0x%08X)", providers.ErrNoApplicableInstaller, code)
	case codeHashMismatch:
		return fmt.Errorf("%w (0x%08X); não retento nem uso --ignore-security-hash", providers.ErrHashMismatch, code)
	case codePackageInUse, codeFileInUse:
		return fmt.Errorf("%w: feche o app e tente de novo (0x%08X)", providers.ErrRetryable, code)
	case codeInstallInProgress:
		return fmt.Errorf("%w: outra instalação em andamento (0x%08X)", providers.ErrRetryable, code)
	case codeRebootRequiredInstall:
		return fmt.Errorf("%w: reinicie o Windows e tente de novo (0x%08X)", providers.ErrRetryable, code)
	case codeInstallDowngrade:
		return fmt.Errorf("uma versão maior já está instalada (0x%08X)", code)
	case codeBlockedByPolicy:
		return fmt.Errorf("instalação bloqueada por política (0x%08X)", code)
	case codeMissingDependency:
		return fmt.Errorf("dependência ausente (0x%08X): %s", code, providers.Snippet(res.Stderr))
	case codeDiskFull:
		return fmt.Errorf("disco cheio (0x%08X)", code)
	case codeNoNetwork:
		return fmt.Errorf("sem rede (0x%08X)", code)
	}
	if s := providers.Snippet(res.Stderr); s != "" {
		return fmt.Errorf("saiu com código 0x%08X: %s", code, s)
	}
	if s := providers.Snippet(res.Stdout); s != "" {
		return fmt.Errorf("saiu com código 0x%08X: %s", code, s)
	}
	return fmt.Errorf("saiu com código 0x%08X", code)
}

func cleanVersion(v string) string { return strings.TrimSpace(strings.TrimLeft(v, "<>~ ")) }
