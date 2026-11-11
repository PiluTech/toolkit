// Package dnf é o provider do dnf (Fedora, RHEL e derivadas; dnf4 e dnf5). Leituras por rpm e dnf list
// (ReadOnly); install por sudo -n (Elevate).
package dnf

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

// Provider implementa providers.Provider para o dnf.
type Provider struct{ rn providers.Runners }

// New cria o provider sobre o Runner.
func New(r exec.Runner) *Provider { return &Provider{rn: providers.NewRunners(r)} }

// Name devolve "dnf".
func (*Provider) Name() string { return catalog.ProviderDnf }

// Available diz se dnf e rpm estão no PATH.
func (p *Provider) Available(context.Context) bool {
	for _, n := range []string{"dnf", "rpm"} {
		if _, err := p.rn.Read.LookPath(n); err != nil {
			return false
		}
	}
	return true
}

// Validate aceita só package, sem "/" nem "@": para o dnf, "dir/x.rpm" instala um arquivo local e "@grupo" um
// grupo de pacotes.
func (*Provider) Validate(ref providers.PackageRef) error {
	if err := providers.OnlyFields(ref, "package"); err != nil {
		return err
	}
	if err := providers.CheckName("package", ref.Package); err != nil {
		return err
	}
	if strings.ContainsAny(ref.Package, "/@") || strings.HasSuffix(strings.ToLower(ref.Package), ".rpm") {
		return fmt.Errorf("%w: package %q (o dnf lê / e .rpm como arquivo e @ como grupo)", providers.ErrInvalidRef, ref.Package)
	}
	return nil
}

func rpmCmd(ref providers.PackageRef) exec.Cmd {
	return exec.Cmd{Name: "rpm", Args: []string{"-q", "--qf", "%{EVR}\\n", ref.Package}, ReadOnly: true, Timeout: time.Minute}
}

func listCmd(ref providers.PackageRef) exec.Cmd {
	return exec.Cmd{Name: "dnf", Args: []string{"list", "--showduplicates", ref.Package}, ReadOnly: true, Timeout: 5 * time.Minute}
}

func installCmd(ref providers.PackageRef, version string) exec.Cmd {
	target := ref.Package
	if version != "" && version != providers.Latest {
		target += "-" + version
	}
	return exec.Cmd{Name: "dnf", Args: []string{"install", "-y", target}, Elevate: true, Timeout: timeout}
}

// IsInstalled roda "rpm -q --qf %{EVR}" (ReadOnly): exit 1 ("is not installed") = não instalado.
func (p *Provider) IsInstalled(ctx context.Context, ref providers.PackageRef) (bool, string, error) {
	if err := p.Validate(ref); err != nil {
		return false, "", err
	}
	res, err := p.rn.Read.Run(ctx, rpmCmd(ref))
	if err != nil {
		return false, "", err
	}
	if res.ExitCode != 0 {
		if strings.Contains(string(res.Stdout)+string(res.Stderr), "is not installed") {
			return false, "", nil
		}
		return false, "", fmt.Errorf("rpm -q saiu com código %d: %s", res.ExitCode, providers.Snippet(res.Stderr))
	}
	line := strings.TrimSpace(strings.SplitN(string(res.Stdout), "\n", 2)[0])
	if !providers.ValidVersion(line) {
		line = ""
	}
	return true, line, nil
}

// ListVersions roda "dnf list --showduplicates" (ReadOnly): versões disponíveis, sem repetir.
func (p *Provider) ListVersions(ctx context.Context, ref providers.PackageRef) ([]string, error) {
	if err := p.Validate(ref); err != nil {
		return nil, err
	}
	res, err := p.rn.Read.Run(ctx, listCmd(ref))
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("dnf list: %w (%s)", providers.ErrNotFound, providers.Snippet(res.Stderr))
	}
	vs := ParseList(res.Stdout, ref.Package)
	if len(vs) == 0 {
		return nil, fmt.Errorf("dnf list: %w", providers.ErrNotFound)
	}
	return vs, nil
}

// ParseList lê a saída de "dnf list --showduplicates" do dnf4 e do dnf5: linhas "nome.arch versão repo".
// O dnf4 quebra a linha quando o nome é longo (nome.arch sozinho, versão na linha seguinte): une as duas.
// Só devolve as linhas do pacote pedido, sem repetir.
func ParseList(out []byte, pkg string) []string {
	var vs []string
	seen := map[string]bool{}
	pending := ""
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if pending != "" {
			f = append([]string{pending}, f...)
			pending = ""
		}
		if len(f) == 1 && strings.Contains(f[0], ".") {
			pending = f[0]
			continue
		}
		if len(f) < 3 {
			continue // cabeçalhos ("Available Packages", "Installed packages")
		}
		name := f[0]
		if i := strings.LastIndex(name, "."); i > 0 {
			name = name[:i] // tira o .arch
		}
		if name != pkg || seen[f[1]] || !providers.ValidVersion(f[1]) {
			continue
		}
		seen[f[1]] = true
		vs = append(vs, f[1])
	}
	return vs
}

// SupportsVersion aceita: o dnf instala "pacote-versão". Versão inexistente volta de Install como
// ErrVersionUnsupported.
func (*Provider) SupportsVersion(providers.PackageRef, string) error { return nil }

// Install roda "dnf install -y pacote[-versão]" com sudo -n.
func (p *Provider) Install(ctx context.Context, ref providers.PackageRef, version string) (providers.InstallResult, error) {
	if err := providers.CheckVersionArg(version); err != nil {
		return providers.InstallResult{}, err
	}
	if err := p.Validate(ref); err != nil {
		return providers.InstallResult{}, err
	}
	res, err := p.rn.Mutate.Run(ctx, installCmd(ref, version))
	if err != nil {
		if errors.Is(err, exec.ErrNotElevated) {
			return providers.InstallResult{}, fmt.Errorf("dnf install: %w (o sudo -n não tem credencial)", providers.ErrNeedsAdmin)
		}
		return providers.InstallResult{}, err
	}
	if res.ExitCode != 0 {
		return providers.InstallResult{}, fmt.Errorf("dnf install saiu com código %d: %w", res.ExitCode, classify(res, version != "" && version != providers.Latest))
	}
	if version == providers.Latest {
		version = ""
	}
	return providers.InstallResult{Version: version}, nil
}

func classify(res exec.Result, pinned bool) error {
	text := string(res.Stderr) + "\n" + string(res.Stdout)
	snippet := providers.Snippet(res.Stderr)
	if snippet == "" {
		snippet = providers.Snippet(res.Stdout)
	}
	low := strings.ToLower(text)
	switch {
	case strings.Contains(low, "no match for argument") || strings.Contains(low, "unable to find a match"):
		if pinned {
			return fmt.Errorf("%w: %s", providers.ErrVersionUnsupported, snippet)
		}
		return fmt.Errorf("%w: %s", providers.ErrNotFound, snippet)
	case strings.Contains(low, "superuser privileges") || strings.Contains(low, "permission denied") || strings.Contains(low, "must be root"):
		return fmt.Errorf("%w: %s", providers.ErrNeedsAdmin, snippet)
	case strings.Contains(low, "could not lock") || strings.Contains(low, "another process"):
		return fmt.Errorf("%w: %s", providers.ErrRetryable, snippet)
	}
	if snippet == "" {
		return errors.New("sem mensagem de erro")
	}
	return errors.New(snippet)
}
