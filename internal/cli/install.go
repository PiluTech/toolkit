package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/download"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/providers"
	"github.com/PiluTech/toolkit/internal/providers/apt"
	"github.com/PiluTech/toolkit/internal/providers/brew"
	"github.com/PiluTech/toolkit/internal/providers/dnf"
	"github.com/PiluTech/toolkit/internal/providers/flatpak"
	"github.com/PiluTech/toolkit/internal/providers/githubrelease"
	"github.com/PiluTech/toolkit/internal/providers/pacman"
	urlprovider "github.com/PiluTech/toolkit/internal/providers/url"
	"github.com/PiluTech/toolkit/internal/providers/winget"
)

type installFlags struct {
	catalog, sha256 string
	replace, dryRun bool
	yes             bool
}

func newInstallCmd(deps Deps) *cobra.Command {
	var f installFlags
	cmd := &cobra.Command{
		Use:   "install app[@versão]...",
		Short: msg.InstallShort,
		Long:  msg.InstallLong,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, f, args)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.catalog, "catalog", "", msg.FlagCatalog)
	fl.StringVar(&f.sha256, "catalog-sha256", "", msg.FlagCatalogSHA256)
	fl.BoolVar(&f.replace, "catalog-replace", false, msg.FlagCatalogReplace)
	fl.BoolVar(&f.dryRun, "dry-run", false, msg.FlagDryRun)
	fl.BoolVarP(&f.yes, "yes", "y", false, msg.FlagYes)
	return cmd
}

func (d Deps) runner() exec.Runner {
	if d.NewRunner != nil {
		return d.NewRunner()
	}
	return exec.OSRunner{}
}

func (d Deps) providerList(r exec.Runner) []providers.Provider {
	if d.Providers != nil {
		return d.Providers(r)
	}
	return []providers.Provider{winget.New(r), brew.New(r), apt.New(r), dnf.New(r), pacman.New(r), flatpak.New(r), urlprovider.New(r, urlprovider.Options{GOOS: d.GOOS, Downloader: d.Downloader, AppsDir: d.AppsDir}),
		githubrelease.New(r, githubrelease.Options{GOOS: d.GOOS, Downloader: d.Downloader, AppsDir: d.AppsDir, Token: d.githubToken})}
}

func (d Deps) downloader() *download.Downloader {
	if d.Downloader != nil {
		return d.Downloader
	}
	return &download.Downloader{}
}

func (d Deps) githubToken() string {
	if d.GitHubToken != nil {
		return d.GitHubToken()
	}
	return os.Getenv("GITHUB_TOKEN")
}

func (d Deps) stdin() io.Reader {
	if d.Stdin != nil {
		return d.Stdin
	}
	return os.Stdin
}

func (d Deps) interactive() bool {
	if d.Interactive != nil {
		return d.Interactive()
	}
	return stdinIsTerminal()
}

// stdinIsTerminal: o stdin é um dispositivo de caracteres que NÃO é o descarte (/dev/null e NUL também são
// dispositivos de caracteres, mas não são alguém para responder). Sem isso "toolkit install x </dev/null"
// pareceria interativo.
func stdinIsTerminal() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	nul, err := os.Stat(os.DevNull)
	return isTerminal(st, nul, err == nil)
}

func isTerminal(stdin, devNull os.FileInfo, haveNull bool) bool {
	if stdin.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return !haveNull || !os.SameFile(stdin, devNull)
}

func (d Deps) checkElevation() func(exec.Cmd) error {
	if d.CheckElevation != nil {
		return d.CheckElevation
	}
	return exec.OSRunner{GOOS: d.GOOS}.CheckElevation
}

// parseTarget separa "app" de "app@versão".
func parseTarget(arg string) (id, version string, err error) {
	id, version, hasVersion := strings.Cut(arg, "@")
	if id == "" || (hasVersion && version == "") {
		return "", "", fmt.Errorf(msg.ErrInstallBadTarget, printable(arg))
	}
	return id, version, nil
}

// resolveVersion devolve as variáveis de template e a versão a passar ao InstallApp:
//   - app com lista (versions: [...]): a versão vai para o template (VersionVars valida: tem de estar na lista;
//     vazia = a primeira) e o InstallApp confere que o id montado é o dela (VersionApplied). Sem versão pedida,
//     nada é passado ao provider (a padrão só vale nos templates);
//   - app com versions: true: a versão (com época do Debian, por exemplo) vai direto ao provider, validada pelo
//     InstallApp (ValidVersion), nunca por tmpl.ValidValue;
//   - app sem versions: não aceita app@versão.
func resolveVersion(app catalog.App, requested string) (map[string]string, string, error) {
	switch {
	case len(app.Versions.List) > 0:
		vars, err := app.VersionVars(requested)
		if err != nil {
			return nil, "", err
		}
		// Passa o que o usuário PEDIU (vazio = padrão): a versão padrão da lista só entra nos templates. Se a
		// passássemos, um instalador sem template (apt pkg=3.13, winget --version, brew recusando) ficaria
		// preso a uma versão que ninguém pediu.
		return vars, requested, nil
	case app.Versions.Query:
		return map[string]string{}, requested, nil
	}
	if requested != "" {
		return nil, "", fmt.Errorf(msg.ErrInstallNoVersions, app.ID)
	}
	return map[string]string{}, "", nil
}

func runInstall(ctx context.Context, out, errOut io.Writer, deps Deps, f installFlags, args []string) error {
	cat, err := loadCatalog(ctx, deps, listFlags{catalog: f.catalog, sha256: f.sha256, replace: f.replace}, errOut)
	if err != nil {
		return err
	}
	// Valida tudo antes de executar qualquer coisa.
	var targets []installTarget
	for _, arg := range args {
		id, requested, err := parseTarget(arg)
		if err != nil {
			return err
		}
		app, ok := cat.Get(id)
		if !ok {
			return fmt.Errorf(msg.ErrInstallUnknownApp, printable(id))
		}
		vars, version, err := resolveVersion(app, requested)
		if err != nil {
			return err
		}
		targets = append(targets, installTarget{app, vars, version})
	}

	// Pré-passo: o plano. Leituras reais, mutações só registradas (DryRunRunner). No dry-run é a saída final; no
	// install real é o que o usuário confirma antes de qualquer mutação.
	failed, plan := installPass(ctx, out, errOut, deps, targets, true)
	printPlan(out, deps, plan, f.dryRun)
	if len(failed) > 0 {
		return fmt.Errorf(msg.ErrInstallFailed, printable(strings.Join(failed, ", ")))
	}
	if f.dryRun || len(plan.Plan()) == 0 {
		return nil
	}
	if !f.yes {
		if !deps.interactive() {
			return errors.New(msg.ErrInstallNeedsYes)
		}
		_, _ = fmt.Fprint(out, msg.InstallConfirm)
		answer, _ := bufio.NewReader(deps.stdin()).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "s", "sim", "y", "yes":
		default:
			_, _ = fmt.Fprintln(out, msg.InstallCancelled)
			return nil
		}
	}
	failed, _ = installPass(ctx, out, errOut, deps, targets, false)
	if len(failed) > 0 {
		return fmt.Errorf(msg.ErrInstallFailed, printable(strings.Join(failed, ", ")))
	}
	return nil
}

type installTarget struct {
	app     catalog.App
	vars    map[string]string
	version string
}

func installOne(ctx context.Context, out io.Writer, reg *providers.Registry, goos string, app catalog.App, vars map[string]string, version string, dryRun bool) error {
	cands, skips := reg.Candidates(ctx, app, goos, vars)
	if len(cands) == 0 {
		return providers.NoInstallerError(app, goos, skips)
	}
	res, err := providers.InstallApp(ctx, cands, version)
	if err != nil {
		return err
	}
	switch {
	case res.Result.AlreadyInstalled:
		v := ""
		if res.Result.Version != "" {
			v = ", versão " + res.Result.Version
		}
		_, _ = fmt.Fprintln(out, fmt.Sprintf(msg.InstallAlready, res.Provider, printable(v)))
	case dryRun:
		_, _ = fmt.Fprintln(out, fmt.Sprintf(msg.InstallDoneSimul, res.Provider))
	default:
		_, _ = fmt.Fprintln(out, fmt.Sprintf(msg.InstallDone, res.Provider))
	}
	if res.Provider == "flatpak" && !res.Result.AlreadyInstalled {
		_, _ = fmt.Fprintln(out, msg.InstallFlatpakNote)
	}
	if res.Result.RebootRequired {
		_, _ = fmt.Fprintln(out, msg.InstallReboot)
	}
	return nil
}

// printPlan mostra cada passo COMO SERÁ EXECUTADO (sudo -n -- env VAR=... cmd, ou VAR=... cmd), avisa quando o
// plano usa sudo -n no Unix e marca o que exige administrador no Windows sem token.
func printPlan(out io.Writer, deps Deps, dry *exec.DryRunRunner, dryRun bool) {
	plan := dry.Plan()
	if len(plan) == 0 {
		_, _ = fmt.Fprintln(out, msg.InstallNoPlan)
		return
	}
	header := msg.InstallPlanHeader
	if !dryRun {
		header = msg.InstallPlanHeaderReal
	}
	_, _ = fmt.Fprintln(out, header)
	display := exec.OSRunner{GOOS: deps.GOOS}
	usesSudo := false
	for _, step := range plan {
		if step.Note != "" {
			_, _ = fmt.Fprintln(out, fmt.Sprintf(msg.InstallPlanLine, printable(step.Note)))
			continue
		}
		line := printable(display.Display(step.Cmd))
		if step.Cmd.Elevate && deps.GOOS != "windows" {
			usesSudo = true
		}
		if step.Cmd.Elevate && step.Err != nil {
			_, _ = fmt.Fprintln(out, fmt.Sprintf(msg.InstallPlanAdmin, line))
			continue
		}
		_, _ = fmt.Fprintln(out, fmt.Sprintf(msg.InstallPlanLine, line))
	}
	if usesSudo {
		_, _ = fmt.Fprintln(out, msg.WarnPlanSudo)
	}
}

// installPass instala (ou, com dry, só planeja) cada app e devolve os que falharam e o DryRunRunner (nil no
// passe real). Uma falha não impede os demais.
func installPass(ctx context.Context, out, errOut io.Writer, deps Deps, targets []installTarget, dry bool) ([]string, *exec.DryRunRunner) {
	rn := deps.runner()
	var dr *exec.DryRunRunner
	if dry {
		dr = exec.NewDryRunRunner(rn)
		dr.CheckElevation = deps.checkElevation()
		rn = dr
	}
	reg := providers.NewRegistry(deps.providerList(rn)...)
	var failed []string
	for _, t := range targets {
		_, _ = fmt.Fprintln(out, fmt.Sprintf(msg.InstallHeader, printable(t.app.ID), printable(t.app.Name)))
		if err := installOne(ctx, out, reg, deps.GOOS, t.app, t.vars, t.version, dry); err != nil {
			_, _ = fmt.Fprintln(errOut, "erro: "+printable(err.Error()))
			if errors.Is(err, providers.ErrNeedsAdmin) {
				_, _ = fmt.Fprintln(errOut, msg.HintNeedsAdmin)
			}
			failed = append(failed, t.app.ID)
		}
	}
	return failed, dr
}
