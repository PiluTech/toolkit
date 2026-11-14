package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/appsnativos"
	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// AppsNatDeps são os pontos injetáveis do `toolkit apps-nativos` (os testes nunca tocam o Windows real).
type AppsNatDeps struct {
	// System é o Windows visto pelo toolkit (nil = appsnativos.NewWindowsSystem sobre o Runner do Deps, com a
	// elevação perguntada ao PowerShell).
	System appsnativos.System
	// Store guarda o backup (nil = arquivo por SID na pasta de estado do toolkit).
	Store appsnativos.Store
	Now   func() time.Time
}

type appsNatFlags struct {
	perfil string
	apps   []string
	todos  bool
	dryRun bool
	yes    bool
}

func newAppsNativosCmd(deps Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "apps-nativos",
		Short: msg.AppsNatShort,
		Long:  msg.AppsNatLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAppsNatState(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	var f appsNatFlags
	remove := &cobra.Command{
		Use:   "remover",
		Short: msg.AppsNatRemoveShort,
		Long:  msg.AppsNatRemoveLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAppsNatRemove(cmd.Context(), cmd.OutOrStdout(), deps, f)
		},
	}
	remove.Flags().StringVar(&f.perfil, "perfil", "", msg.FlagAppsNatPerfil)
	remove.Flags().StringArrayVar(&f.apps, "app", nil, msg.FlagAppsNatApp)
	remove.Flags().BoolVar(&f.todos, "todos-usuarios", false, msg.FlagAppsNatAll)
	remove.Flags().BoolVar(&f.dryRun, "dry-run", false, msg.FlagAppsNatDryRun)
	remove.Flags().BoolVarP(&f.yes, "yes", "y", false, msg.FlagAppsNatYes)

	reinstall := &cobra.Command{
		Use:   "reinstalar",
		Short: msg.AppsNatReinstShort,
		Long:  msg.AppsNatReinstLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAppsNatReinstall(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	c.AddCommand(remove, reinstall)
	return c
}

type appsNatEnv struct {
	sys   appsnativos.System
	store appsnativos.Store
	lang  diagfmt.Lang
	now   func() time.Time
}

// newAppsNatEnv junta o que os três modos usam. Fora do Windows devolve o erro ANTES de montar qualquer coisa.
func newAppsNatEnv(ctx context.Context, deps Deps) (appsNatEnv, error) {
	lang := diagfmt.LangFromEnv(deps.UpdateCheck.env)
	if m := appsnativos.UnsupportedMessage(deps.GOOS, lang); m != "" {
		return appsNatEnv{}, errors.New(textsafe.Line(m))
	}
	e := appsNatEnv{lang: lang, now: deps.AppsNat.Now, sys: deps.AppsNat.System, store: deps.AppsNat.Store}
	if e.now == nil {
		e.now = time.Now
	}
	if e.sys == nil {
		r := deps.runner()
		e.sys = appsnativos.NewWindowsSystem(r, func() bool { return platform.IsElevatedViaPowerShell(ctx, r) })
	}
	if e.store == nil {
		paths, err := platform.ResolvePaths(platform.Info{OS: platform.Windows, IsAdmin: e.sys.IsAdmin(ctx)},
			platform.Env{Getenv: os.Getenv, Home: userHome()})
		if err != nil {
			return appsNatEnv{}, err
		}
		// um backup por usuário (SID), com o dono gravado dentro
		sid, err := appsnativos.UserSID(ctx, e.sys)
		if err != nil {
			return appsNatEnv{}, cleanErr(err)
		}
		e.store, err = appsnativos.NewStore(paths.StateDir, sid)
		if err != nil {
			return appsNatEnv{}, err
		}
	}
	return e, nil
}

// runAppsNatState lista o catálogo com o estado de cada app. Só leitura, só o usuário atual.
func runAppsNatState(ctx context.Context, out io.Writer, deps Deps) error {
	ctx = orBackground(ctx)
	e, err := newAppsNatEnv(ctx, deps)
	if err != nil {
		return err
	}
	pkgs, err := e.sys.ListInstalled(ctx, false)
	if err != nil {
		return cleanErr(err)
	}
	have := map[string]bool{}
	for _, p := range pkgs {
		have[p.Name] = true
	}
	_, _ = fmt.Fprintln(out, msg.AppsNatStateHeader)
	for _, a := range appsnativos.Apps() {
		state, perfil := "ausente", "completo"
		if have[a.Name] {
			state = "instalado"
		}
		if a.Basico {
			perfil = "basico"
		}
		_, _ = fmt.Fprintln(out, textsafe.Line(fmt.Sprintf(msg.AppsNatStateLine, a.ID, state, perfil, a.Title(e.lang))))
	}
	_, _ = fmt.Fprintln(out, "\n"+msg.AppsNatStateHint)
	return nil
}

func runAppsNatRemove(ctx context.Context, out io.Writer, deps Deps, f appsNatFlags) error {
	ctx = orBackground(ctx)
	e, err := newAppsNatEnv(ctx, deps)
	if err != nil {
		return err
	}
	sel := appsnativos.Selection{Apps: f.apps, TodosUsuarios: f.todos}
	if f.perfil != "" {
		p, perr := appsnativos.ParsePerfil(f.perfil)
		if perr != nil {
			return fmt.Errorf(msg.AppsNatBadPerfil, printable(f.perfil))
		}
		sel.Perfil = p
	}
	if sel.Perfil == "" && len(sel.Apps) == 0 {
		return errors.New(msg.AppsNatNeedSelection)
	}
	for _, id := range f.apps { // valida os ids antes de ler o sistema
		if _, ok := appsnativos.Find(strings.ToLower(strings.TrimSpace(id))); !ok {
			return fmt.Errorf(msg.AppsNatBadApp, printable(id))
		}
	}
	plan, err := appsnativos.Build(ctx, e.sys, sel, e.lang)
	if err != nil {
		return cleanErr(err)
	}
	_, _ = fmt.Fprint(out, textsafe.Multiline(plan.Format(e.lang)))
	if plan.Pending() == 0 {
		_, _ = fmt.Fprintln(out, msg.AppsNatNothingToDo)
		return nil
	}
	if f.dryRun {
		_, _ = fmt.Fprintln(out, msg.AppsNatDryRunDone)
		return nil
	}
	if plan.NeedsAdmin() && !e.sys.IsAdmin(ctx) {
		return errors.New(msg.AppsNatNeedsAdmin)
	}
	if ok, err := appsNatConfirm(ctx, out, deps, f.yes); !ok || err != nil {
		return err
	}
	res, err := appsnativos.Apply(ctx, e.sys, e.store, plan, e.now(), appsnativos.Options{})
	if err != nil {
		if res != nil && len(res.Removed) > 0 {
			_, _ = fmt.Fprintf(out, msg.AppsNatRemovedPart+"\n", len(res.Removed), printable(res.BackupPath))
		}
		if errors.Is(err, appsnativos.ErrNeedsAdmin) {
			return errors.New(msg.AppsNatNeedsAdmin)
		}
		return cleanErr(err)
	}
	_, _ = fmt.Fprintf(out, msg.AppsNatRemoved+"\n", len(res.Removed), printable(res.BackupPath))
	return nil
}

func runAppsNatReinstall(ctx context.Context, out io.Writer, deps Deps) error {
	ctx = orBackground(ctx)
	e, err := newAppsNatEnv(ctx, deps)
	if err != nil {
		return err
	}
	rp, err := appsnativos.BuildReinstall(e.store)
	if errors.Is(err, appsnativos.ErrNoBackup) {
		_, _ = fmt.Fprintln(out, msg.AppsNatNoBackup)
		return err
	}
	if err != nil {
		return cleanErr(err)
	}
	_, _ = fmt.Fprintln(out, msg.AppsNatReinstHeader)
	_, _ = fmt.Fprint(out, textsafe.Multiline(rp.Format(e.lang)))
	_, _ = fmt.Fprintln(out, msg.AppsNatReinstWarn)
	return nil
}

// appsNatConfirm pede [s/N] com o plano na tela. Sem --yes e sem terminal é erro (nada foi mudado). Resposta que não é
// sim devolve false sem erro: o comando termina sem mudar nada.
func appsNatConfirm(ctx context.Context, out io.Writer, deps Deps, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !deps.interactive() {
		return false, errors.New(msg.AppsNatNeedsYes)
	}
	ok, err := ttyPrompter{in: bufio.NewReader(deps.stdin()), out: out}.Confirm(ctx, msg.AppsNatConfirm)
	if err != nil {
		return false, err
	}
	if !ok {
		_, _ = fmt.Fprintln(out, msg.AppsNatCancelled)
	}
	return ok, nil
}
