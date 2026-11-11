// Package apt é o provider do apt (Debian, Ubuntu e derivadas). Leituras por dpkg-query e apt-cache
// (ReadOnly); apt-get update e install por sudo -n (Elevate) com DEBIAN_FRONTEND=noninteractive.
package apt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/providers"
)

// env: sem perguntas do debconf. Com Elevate vira "sudo -n -- env DEBIAN_FRONTEND=noninteractive apt-get ...".
var env = []string{"DEBIAN_FRONTEND=noninteractive"}

const timeout = 30 * time.Minute

// Provider implementa providers.Provider e providers.Preparer para o apt.
type Provider struct {
	rn providers.Runners

	once    sync.Once
	prepErr error
}

// New cria o provider sobre o Runner.
func New(r exec.Runner) *Provider { return &Provider{rn: providers.NewRunners(r)} }

// Name devolve "apt".
func (*Provider) Name() string { return catalog.ProviderApt }

// Available diz se apt-get, apt-cache e dpkg-query estão no PATH.
func (p *Provider) Available(context.Context) bool {
	for _, n := range []string{"apt-get", "apt-cache", "dpkg-query"} {
		if _, err := p.rn.Read.LookPath(n); err != nil {
			return false
		}
	}
	return true
}

// Validate aceita só package. "pkg/suite" é sintaxe do apt (escolhe o suite): ver providers.CheckName.
func (*Provider) Validate(ref providers.PackageRef) error {
	if err := providers.OnlyFields(ref, "package"); err != nil {
		return err
	}
	if err := providers.CheckName("package", ref.Package); err != nil {
		return err
	}
	if strings.HasSuffix(strings.ToLower(ref.Package), ".deb") {
		return fmt.Errorf("%w: package %q (o apt-get lê nome terminado em .deb como arquivo local)", providers.ErrInvalidRef, ref.Package)
	}
	return nil
}

// base é o nome sem "/suite": dpkg-query e apt-cache madison usam só o nome.
func base(ref providers.PackageRef) string {
	name, _, _ := strings.Cut(ref.Package, "/")
	return name
}

func statusCmd(ref providers.PackageRef) exec.Cmd {
	return exec.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f=${db:Status-Status}\\t${Version}\\n", base(ref)}, ReadOnly: true, Timeout: time.Minute}
}

func madisonCmd(ref providers.PackageRef) exec.Cmd {
	return exec.Cmd{Name: "apt-cache", Args: []string{"madison", base(ref)}, ReadOnly: true, Timeout: time.Minute}
}

func updateCmd() exec.Cmd {
	return exec.Cmd{Name: "apt-get", Args: []string{"update"}, Env: env, Elevate: true, Timeout: timeout}
}

func installCmd(ref providers.PackageRef, version string) exec.Cmd {
	target := ref.Package
	if version != "" && version != providers.Latest {
		target = base(ref) + "=" + version
	}
	return exec.Cmd{Name: "apt-get", Args: []string{"install", "-y", target}, Env: env, Elevate: true, Timeout: timeout}
}

// IsInstalled roda "dpkg-query -W" (ReadOnly). Pacote desconhecido (exit 1) ou só com configs = não instalado.
func (p *Provider) IsInstalled(ctx context.Context, ref providers.PackageRef) (bool, string, error) {
	if err := p.Validate(ref); err != nil {
		return false, "", err
	}
	res, err := p.rn.Read.Run(ctx, statusCmd(ref))
	if err != nil {
		return false, "", err
	}
	if res.ExitCode != 0 {
		return false, "", nil
	}
	status, ver, _ := strings.Cut(strings.TrimSpace(string(res.Stdout)), "\t")
	if status != "installed" {
		return false, "", nil
	}
	if !providers.ValidVersion(ver) {
		ver = ""
	}
	return true, ver, nil
}

// ListVersions roda "apt-cache madison" (ReadOnly) e devolve as versões dos repositórios configurados, sem
// repetir, na ordem do apt (a mais nova primeiro). Sem nenhuma = ErrNotFound.
func (p *Provider) ListVersions(ctx context.Context, ref providers.PackageRef) ([]string, error) {
	if err := p.Validate(ref); err != nil {
		return nil, err
	}
	res, err := p.rn.Read.Run(ctx, madisonCmd(ref))
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("apt-cache madison saiu com código %d: %s", res.ExitCode, providers.Snippet(res.Stderr))
	}
	vs := ParseMadison(res.Stdout)
	if len(vs) == 0 {
		return nil, fmt.Errorf("apt-cache madison: %w", providers.ErrNotFound)
	}
	return vs, nil
}

// ParseMadison lê "  pacote | versão | origem" e devolve as versões válidas, sem repetir.
func ParseMadison(out []byte) []string {
	var vs []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		v := strings.TrimSpace(parts[1])
		if v == "" || seen[v] || !providers.ValidVersion(v) {
			continue
		}
		seen[v] = true
		vs = append(vs, v)
	}
	return vs
}

// SupportsVersion aceita: o apt instala "pacote=versão". Se a versão não existir nos repositórios, o Install
// devolve ErrVersionUnsupported pelo stderr do apt-get.
func (*Provider) SupportsVersion(providers.PackageRef, string) error { return nil }

// Prepare roda um único "apt-get update" por execução (mutação, com sudo).
func (p *Provider) Prepare(ctx context.Context) error {
	p.once.Do(func() {
		res, err := p.rn.Mutate.Run(ctx, updateCmd())
		switch {
		case err != nil:
			p.prepErr = wrapRun("apt-get update", err)
		case res.ExitCode != 0:
			p.prepErr = fmt.Errorf("apt-get update saiu com código %d: %w", res.ExitCode, classify(res))
		}
	})
	return p.prepErr
}

// Install roda "apt-get install -y pacote[=versão]" com sudo -n.
func (p *Provider) Install(ctx context.Context, ref providers.PackageRef, version string) (providers.InstallResult, error) {
	if err := providers.CheckVersionArg(version); err != nil {
		return providers.InstallResult{}, err
	}
	if err := p.Validate(ref); err != nil {
		return providers.InstallResult{}, err
	}
	res, err := p.rn.Mutate.Run(ctx, installCmd(ref, version))
	if err != nil {
		return providers.InstallResult{}, wrapRun("apt-get install", err)
	}
	if res.ExitCode != 0 {
		return providers.InstallResult{}, fmt.Errorf("apt-get install saiu com código %d: %w", res.ExitCode, classify(res))
	}
	if version == providers.Latest {
		version = ""
	}
	return providers.InstallResult{Version: version}, nil
}

// wrapRun traduz a recusa de elevação do Runner em ErrNeedsAdmin.
func wrapRun(what string, err error) error {
	if errors.Is(err, exec.ErrNotElevated) {
		return fmt.Errorf("%s: %w (o sudo -n não tem credencial: rode \"sudo -v\" antes ou use um usuário com sudo sem senha)", what, providers.ErrNeedsAdmin)
	}
	return err
}

// classify transforma o stderr do apt-get (que sai com 100 em qualquer erro) em erro tipado.
func classify(res exec.Result) error {
	snippet := providers.Snippet(res.Stderr)
	low := strings.ToLower(string(res.Stderr))
	switch {
	case strings.Contains(low, "unable to locate package"):
		return fmt.Errorf("%w: %s", providers.ErrNotFound, snippet)
	case strings.Contains(low, "was not found") && strings.Contains(low, "version"):
		return fmt.Errorf("%w: %s", providers.ErrVersionUnsupported, snippet)
	case strings.Contains(low, "could not get lock"), strings.Contains(low, "unable to acquire the dpkg frontend lock"),
		strings.Contains(low, "unable to lock directory"):
		return fmt.Errorf("%w: outro apt/dpkg em andamento (%s)", providers.ErrRetryable, snippet)
	case strings.Contains(low, "permission denied") || strings.Contains(low, "are you root"):
		return fmt.Errorf("%w: %s", providers.ErrNeedsAdmin, snippet)
	}
	if snippet == "" {
		return errors.New("sem mensagem de erro")
	}
	return errors.New(snippet)
}
