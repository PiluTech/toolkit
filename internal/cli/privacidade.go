package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/privacidade"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// PrivDeps são os pontos injetáveis do `toolkit privacidade` (os testes nunca tocam o Windows real).
type PrivDeps struct {
	// System é o Windows visto pelo toolkit (nil = privacidade.NewWindowsSystem sobre o Runner do Deps, com a
	// elevação perguntada ao PowerShell).
	System privacidade.System
	// Store guarda o backup (nil = arquivo na pasta de estado do toolkit).
	Store privacidade.Store
	Now   func() time.Time
}

type privFlags struct {
	perfil   string
	dryRun   bool
	yes      bool
	onlyUser bool
}

func newPrivacidadeCmd(deps Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "privacidade",
		Short: msg.PrivShort,
		Long:  msg.PrivLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPrivState(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	var f privFlags
	apply := &cobra.Command{
		Use:   "aplicar",
		Short: msg.PrivApplyShort,
		Long:  msg.PrivApplyLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPrivApply(cmd.Context(), cmd.OutOrStdout(), deps, f)
		},
	}
	apply.Flags().StringVar(&f.perfil, "perfil", "", msg.FlagPrivPerfil)
	apply.Flags().BoolVar(&f.dryRun, "dry-run", false, msg.FlagPrivDryRun)
	apply.Flags().BoolVarP(&f.yes, "yes", "y", false, msg.FlagPrivYes)
	apply.Flags().BoolVar(&f.onlyUser, "somente-usuario", false, msg.FlagPrivUser)

	var u privFlags
	undo := &cobra.Command{
		Use:   "desfazer",
		Short: msg.PrivUndoShort,
		Long:  msg.PrivUndoLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPrivUndo(cmd.Context(), cmd.OutOrStdout(), deps, u)
		},
	}
	undo.Flags().BoolVar(&u.dryRun, "dry-run", false, msg.FlagPrivDryRun)
	undo.Flags().BoolVarP(&u.yes, "yes", "y", false, msg.FlagPrivYes)
	c.AddCommand(apply, undo)
	return c
}

// privEnv junta o que os três modos usam. Fora do Windows devolve o erro ANTES de montar qualquer coisa.
type privEnv struct {
	sys   privacidade.System
	store privacidade.Store
	lang  diagfmt.Lang
	now   func() time.Time
}

func newPrivEnv(ctx context.Context, deps Deps) (privEnv, error) {
	if deps.GOOS != "windows" {
		return privEnv{}, fmt.Errorf(msg.PrivOnlyWindows, printable(deps.GOOS))
	}
	e := privEnv{lang: diagfmt.LangFromEnv(deps.UpdateCheck.env), now: deps.Priv.Now, sys: deps.Priv.System, store: deps.Priv.Store}
	if e.now == nil {
		e.now = time.Now
	}
	if e.sys == nil {
		r := deps.runner()
		e.sys = privacidade.NewWindowsSystem(r, func() bool { return platform.IsElevatedViaPowerShell(ctx, r) })
	}
	if e.store == nil {
		paths, err := platform.ResolvePaths(platform.Info{OS: platform.Windows, IsAdmin: e.sys.IsAdmin(ctx)},
			platform.Env{Getenv: os.Getenv, Home: userHome()})
		if err != nil {
			return privEnv{}, err
		}
		// backup por usuário (SID) e da máquina em arquivos separados, cada um com o dono gravado
		sid, err := privacidade.UserSID(ctx, e.sys)
		if err != nil {
			return privEnv{}, cleanErr(err)
		}
		e.store, err = privacidade.NewScopedStore(paths.StateDir, sid)
		if err != nil {
			return privEnv{}, err
		}
	}
	return e, nil
}

func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}

func runPrivState(ctx context.Context, out io.Writer, deps Deps) error {
	ctx = orBackground(ctx)
	e, err := newPrivEnv(ctx, deps)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, msg.PrivStateHeader)
	for _, p := range []privacidade.Perfil{privacidade.PerfilBasico, privacidade.PerfilRecomendado} {
		plan, err := privacidade.Build(ctx, e.sys, p, e.lang)
		if err != nil {
			return cleanErr(err)
		}
		_, _ = fmt.Fprintf(out, "\n"+msg.PrivProfileSep+"\n", p)
		_, _ = fmt.Fprint(out, textsafe.Multiline(plan.Format(e.lang)))
	}
	_, _ = fmt.Fprintln(out, "\n"+msg.PrivStateHint)
	return nil
}

func orBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// onlyUserItems tira do plano os itens que exigem administrador (o que sobra roda sem elevação).
func onlyUserItems(p *privacidade.Plan) {
	kept := p.Changes[:0:0]
	for _, c := range p.Changes {
		if !c.Admin {
			kept = append(kept, c)
		}
	}
	p.Changes = kept
}

func runPrivApply(ctx context.Context, out io.Writer, deps Deps, f privFlags) error {
	ctx = orBackground(ctx)
	e, err := newPrivEnv(ctx, deps)
	if err != nil {
		return err
	}
	var perfil privacidade.Perfil
	switch f.perfil {
	case "":
		return errors.New(msg.PrivNeedPerfil)
	case string(privacidade.PerfilBasico), string(privacidade.PerfilRecomendado):
		perfil = privacidade.Perfil(f.perfil)
	default:
		return fmt.Errorf(msg.PrivBadPerfil, printable(f.perfil))
	}
	plan, err := privacidade.Build(ctx, e.sys, perfil, e.lang)
	if err != nil {
		return cleanErr(err)
	}
	if f.onlyUser {
		onlyUserItems(plan)
	}
	_, _ = fmt.Fprint(out, textsafe.Multiline(plan.Format(e.lang)))
	if f.onlyUser {
		_, _ = fmt.Fprintln(out, msg.PrivUserOnlyNote)
	}
	if plan.Pending() == 0 {
		_, _ = fmt.Fprintln(out, msg.PrivNothingToDo)
		return nil
	}
	if f.dryRun {
		_, _ = fmt.Fprintln(out, msg.PrivDryRunDone)
		return nil
	}
	if plan.NeedsAdmin() && !e.sys.IsAdmin(ctx) {
		return errors.New(msg.PrivNeedsAdmin)
	}
	if ok, err := privConfirm(ctx, out, deps, f.yes, msg.PrivConfirmApply); !ok || err != nil {
		return err
	}
	// o plano é recalculado DEPOIS da confirmação e antes de qualquer backup ou escrita: o que será gravado tem de ser
	// idêntico ao que foi mostrado (outro processo pode ter mudado o sistema durante a pergunta; vale também com --yes).
	fresh, err := privacidade.Build(ctx, e.sys, perfil, e.lang)
	if err != nil {
		return cleanErr(err)
	}
	if f.onlyUser {
		onlyUserItems(fresh)
	}
	if fresh.Format(e.lang) != plan.Format(e.lang) || fresh.Pending() != plan.Pending() {
		return errors.New(msg.PrivStale)
	}
	plan = fresh
	res, err := privacidade.Apply(ctx, e.sys, e.store, plan, e.now())
	if err != nil {
		if res != nil && len(res.Applied) > 0 {
			_, _ = fmt.Fprintf(out, msg.PrivAppliedPart+"\n", len(res.Applied), printable(res.BackupPath))
		}
		return privErr(err)
	}
	_, _ = fmt.Fprintf(out, msg.PrivApplied+"\n", len(res.Applied), printable(res.BackupPath))
	return nil
}

func runPrivUndo(ctx context.Context, out io.Writer, deps Deps, f privFlags) error {
	ctx = orBackground(ctx)
	e, err := newPrivEnv(ctx, deps)
	if err != nil {
		return err
	}
	plan, bk, err := privacidade.BuildUndo(ctx, e.sys, e.store, e.lang)
	if errors.Is(err, privacidade.ErrNoBackup) {
		_, _ = fmt.Fprintln(out, msg.PrivNoBackup)
		return err
	}
	if err != nil {
		return cleanErr(err)
	}
	if plan.Pending() == 0 {
		_, _ = fmt.Fprintln(out, textsafe.Multiline(formatUndo(plan)))
		if f.dryRun { // dry-run nunca remove nem escreve nada
			_, _ = fmt.Fprintln(out, msg.PrivDryRunDone)
			return nil
		}
		_, _ = fmt.Fprintln(out, msg.PrivUndoNothing)
		return e.store.Remove()
	}
	_, _ = fmt.Fprint(out, textsafe.Multiline(formatUndo(plan)))
	if f.dryRun {
		_, _ = fmt.Fprintln(out, msg.PrivDryRunDone)
		return nil
	}
	if plan.NeedsAdmin() && !e.sys.IsAdmin(ctx) {
		return errors.New(msg.PrivUndoNeedsAdm)
	}
	if ok, err := privConfirm(ctx, out, deps, f.yes, msg.PrivConfirmUndo); !ok || err != nil {
		return err
	}
	// backup e sistema são relidos DEPOIS da confirmação e comparados ao que foi mostrado, antes de restaurar qualquer coisa.
	fresh, freshBk, err := privacidade.BuildUndo(ctx, e.sys, e.store, e.lang)
	if err != nil {
		return cleanErr(err)
	}
	if formatUndo(fresh) != formatUndo(plan) || fresh.Pending() != plan.Pending() || !reflect.DeepEqual(freshBk, bk) {
		return errors.New(msg.PrivStale)
	}
	plan, bk = fresh, freshBk
	res, err := privacidade.ApplyUndo(ctx, e.sys, e.store, plan, bk)
	if err != nil {
		if res != nil && len(res.Applied) > 0 {
			_, _ = fmt.Fprintf(out, msg.PrivRestoredPart+"\n", len(res.Applied))
		}
		return privErr(err)
	}
	_, _ = fmt.Fprintf(out, msg.PrivRestored+"\n", len(res.Applied))
	return nil
}

// formatUndo mostra o que o desfazer restaura (o Plan.Format é do aplicar, com os ajustes do catálogo).
func formatUndo(p *privacidade.Plan) string {
	var b strings.Builder
	b.WriteString(msg.PrivUndoHeader + "\n")
	for _, c := range p.Changes {
		if !c.Pending {
			continue
		}
		admin := ""
		if c.Admin {
			admin = msg.PrivAdminTag
		}
		fmt.Fprintf(&b, msg.PrivPendingFormat+"%s\n", c.Target, c.Before, c.After, admin)
	}
	if len(p.Missing) > 0 {
		fmt.Fprintf(&b, "\n"+msg.PrivMissing+"\n", strings.Join(p.Missing, "; "))
	}
	return b.String()
}

// privConfirm pede [s/N] com o plano na tela. Sem --yes e sem terminal é erro (nada foi mudado). Resposta que não é
// sim devolve false sem erro: o comando termina sem mudar nada.
func privConfirm(ctx context.Context, out io.Writer, deps Deps, yes bool, question string) (bool, error) {
	if yes {
		return true, nil
	}
	if !deps.interactive() {
		return false, errors.New(msg.PrivNeedsYes)
	}
	ok, err := ttyPrompter{in: bufio.NewReader(deps.stdin()), out: out}.Confirm(ctx, question)
	if err != nil {
		return false, err
	}
	if !ok {
		_, _ = fmt.Fprintln(out, msg.PrivCancelled)
	}
	return ok, nil
}

// privErr deixa a mensagem de erro limpa e troca o ErrNeedsAdmin do núcleo pela instrução do toolkit.
func privErr(err error) error {
	if errors.Is(err, privacidade.ErrNeedsAdmin) {
		return errors.New(msg.PrivNeedsAdmin)
	}
	return cleanErr(err)
}
