package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/msg"
	"github.com/PiluVitu/toolkit/internal/providers"
	"github.com/PiluVitu/toolkit/internal/providers/apt"
	"github.com/PiluVitu/toolkit/internal/providers/brew"
	"github.com/PiluVitu/toolkit/internal/providers/dnf"
	"github.com/PiluVitu/toolkit/internal/providers/flatpak"
	"github.com/PiluVitu/toolkit/internal/providers/pacman"
	"github.com/PiluVitu/toolkit/internal/providers/winget"
)

type installFlags struct {
	catalog, sha256 string
	replace, dryRun bool
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
	return []providers.Provider{winget.New(r), brew.New(r), apt.New(r), dnf.New(r), pacman.New(r), flatpak.New(r)}
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
//     vazia = a primeira) e o InstallApp confere que o id montado é o dela (VersionApplied);
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
		return vars, vars["version"], nil
	case app.Versions.Query:
		return map[string]string{}, requested, nil
	}
	if requested != "" {
		return nil, "", fmt.Errorf(msg.ErrInstallNoVersions, app.ID)
	}
	return map[string]string{}, "", nil
}

func runInstall(ctx context.Context, out, errOut io.Writer, deps Deps, f installFlags, args []string) error {
	cat, err := loadCatalog(deps, listFlags{catalog: f.catalog, sha256: f.sha256, replace: f.replace}, errOut)
	if err != nil {
		return err
	}
	// Valida tudo antes de executar qualquer coisa.
	type target struct {
		app     catalog.App
		vars    map[string]string
		version string
	}
	var targets []target
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
		targets = append(targets, target{app, vars, version})
	}

	rn := deps.runner()
	var dry *exec.DryRunRunner
	if f.dryRun {
		dry = exec.NewDryRunRunner(rn)
		dry.CheckElevation = deps.checkElevation()
		rn = dry
	}
	reg := providers.NewRegistry(deps.providerList(rn)...)

	var failed []string
	for _, t := range targets {
		_, _ = fmt.Fprintln(out, fmt.Sprintf(msg.InstallHeader, printable(t.app.ID), printable(t.app.Name)))
		if err := installOne(ctx, out, reg, deps.GOOS, t.app, t.vars, t.version, f.dryRun); err != nil {
			_, _ = fmt.Fprintln(errOut, "erro: "+printable(err.Error()))
			if errors.Is(err, providers.ErrNeedsAdmin) {
				_, _ = fmt.Fprintln(errOut, msg.HintNeedsAdmin)
			}
			failed = append(failed, t.app.ID)
		}
	}
	if dry != nil {
		printPlan(out, dry)
	}
	if len(failed) > 0 {
		return fmt.Errorf(msg.ErrInstallFailed, printable(strings.Join(failed, ", ")))
	}
	return nil
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

func printPlan(out io.Writer, dry *exec.DryRunRunner) {
	plan := dry.Plan()
	if len(plan) == 0 {
		_, _ = fmt.Fprintln(out, msg.InstallNoPlan)
		return
	}
	_, _ = fmt.Fprintln(out, msg.InstallPlanHeader)
	for _, st := range plan {
		line := printable(st.Cmd.String())
		if st.Cmd.Elevate {
			line = "sudo -n " + line
			if st.Err != nil {
				_, _ = fmt.Fprintln(out, fmt.Sprintf(msg.InstallPlanAdmin, line))
				continue
			}
		}
		_, _ = fmt.Fprintln(out, fmt.Sprintf(msg.InstallPlanLine, line))
	}
}
