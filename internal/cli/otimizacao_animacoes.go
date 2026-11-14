package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/animacoes"
	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/privacidade"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

type animFlags struct{ dryRun, yes bool }

func newAnimacoesCmd(deps Deps) *cobra.Command {
	var f animFlags
	c := &cobra.Command{
		Use:   "animacoes",
		Short: msg.AnimShort,
		Long:  msg.AnimLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAnimApply(cmd.Context(), cmd.OutOrStdout(), deps, f)
		},
	}
	c.Flags().BoolVar(&f.dryRun, "dry-run", false, msg.FlagPrivDryRun)
	c.Flags().BoolVarP(&f.yes, "yes", "y", false, msg.FlagPrivYes)
	var u animFlags
	undo := &cobra.Command{
		Use:   "desfazer",
		Short: msg.AnimUndoShort,
		Long:  msg.AnimUndoLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAnimUndo(cmd.Context(), cmd.OutOrStdout(), deps, u)
		},
	}
	undo.Flags().BoolVar(&u.dryRun, "dry-run", false, msg.FlagPrivDryRun)
	undo.Flags().BoolVarP(&u.yes, "yes", "y", false, msg.FlagPrivYes)
	c.AddCommand(undo)
	return c
}

// animEnv junta o que os dois modos (e a TUI) usam. Fora do Windows devolve o erro ANTES de montar qualquer coisa.
type animEnv struct {
	sys   animacoes.System
	store privacidade.Store
	lang  diagfmt.Lang
	now   func() time.Time
}

func newAnimEnv(ctx context.Context, deps Deps) (animEnv, error) {
	if deps.GOOS != "windows" {
		return animEnv{}, fmt.Errorf(msg.AnimOnlyWindows, printable(deps.GOOS))
	}
	d := deps.Otim
	e := animEnv{lang: diagfmt.LangFromEnv(deps.UpdateCheck.env), now: d.Now, sys: d.AnimSystem, store: d.AnimStore}
	if e.now == nil {
		e.now = time.Now
	}
	if e.sys == nil {
		e.sys = animacoes.NewWindowsSystem(deps.runner(), func() bool { return false })
	}
	if e.store == nil {
		// o backup é do USUÁRIO (HKCU): sempre na pasta de estado do usuário, com ou sem elevação
		paths, err := platform.ResolvePaths(platform.Info{OS: platform.Windows}, platform.Env{Getenv: os.Getenv, Home: userHome()})
		if err != nil {
			return animEnv{}, err
		}
		sid, err := animacoes.UserSID(ctx, e.sys)
		if err != nil {
			return animEnv{}, cleanErr(err)
		}
		e.store, err = animacoes.NewStore(paths.StateDir, sid)
		if err != nil {
			return animEnv{}, err
		}
	}
	return e, nil
}

func runAnimApply(ctx context.Context, out io.Writer, deps Deps, f animFlags) error {
	ctx = orBackground(ctx)
	e, err := newAnimEnv(ctx, deps)
	if err != nil {
		return err
	}
	plan, err := animacoes.Build(ctx, e.sys, e.lang)
	if err != nil {
		return cleanErr(err)
	}
	_, _ = fmt.Fprint(out, textsafe.Multiline(plan.Format(e.lang)))
	if plan.Pending() == 0 {
		_, _ = fmt.Fprintln(out, msg.AnimNothingToDo)
		return nil
	}
	if f.dryRun {
		_, _ = fmt.Fprintln(out, msg.PrivDryRunDone)
		return nil
	}
	if ok, err := privConfirm(ctx, out, deps, f.yes, msg.AnimConfirmApply); !ok || err != nil {
		return err
	}
	// Depois da confirmação e ANTES de qualquer backup ou escrita: recalcula e exige o MESMO plano que foi mostrado
	// (outro processo pode ter mudado o valor durante a pergunta; o backup guardaria o "original" errado).
	fresh, err := animacoes.Build(ctx, e.sys, e.lang)
	if err != nil {
		return cleanErr(err)
	}
	if fresh.Format(e.lang) != plan.Format(e.lang) || fresh.Pending() != plan.Pending() {
		return errors.New(msg.AnimStateChanged)
	}
	res, err := animacoes.Apply(ctx, e.sys, e.store, fresh, e.now())
	if err != nil {
		if res != nil && len(res.Applied) > 0 {
			_, _ = fmt.Fprintf(out, msg.AnimAppliedPart+"\n", len(res.Applied), printable(res.BackupPath))
		}
		return cleanErr(err)
	}
	_, _ = fmt.Fprintf(out, msg.AnimApplied+"\n", len(res.Applied), printable(res.BackupPath))
	return nil
}

func runAnimUndo(ctx context.Context, out io.Writer, deps Deps, f animFlags) error {
	ctx = orBackground(ctx)
	e, err := newAnimEnv(ctx, deps)
	if err != nil {
		return err
	}
	plan, bk, err := animacoes.BuildUndo(ctx, e.sys, e.store, e.lang)
	if errors.Is(err, animacoes.ErrNoBackup) {
		_, _ = fmt.Fprintln(out, msg.AnimNoBackup)
		return err
	}
	if err != nil {
		return cleanErr(err)
	}
	_, _ = fmt.Fprint(out, textsafe.Multiline(formatAnimUndo(plan)))
	if f.dryRun { // dry-run nunca remove nem escreve nada
		_, _ = fmt.Fprintln(out, msg.PrivDryRunDone)
		return nil
	}
	zero := plan.Pending() == 0 // tudo já está como antes: só o backup seria removido, mas com a MESMA cautela do restaurar
	question := msg.AnimConfirmUndo
	if zero {
		_, _ = fmt.Fprintln(out, msg.AnimUndoNothing)
		question = msg.AnimConfirmUndoNothing
	}
	if ok, err := privConfirm(ctx, out, deps, f.yes, question); !ok || err != nil {
		return err
	}
	// Reconfere backup (mesmo conteúdo) e estado atual (mesmo plano) antes de restaurar qualquer coisa.
	fresh, freshBk, err := animacoes.BuildUndo(ctx, e.sys, e.store, e.lang)
	if err != nil {
		if errors.Is(err, animacoes.ErrNoBackup) {
			return errors.New(msg.AnimStateChanged)
		}
		return cleanErr(err)
	}
	if formatAnimUndo(fresh) != formatAnimUndo(plan) || fresh.Pending() != plan.Pending() || !reflect.DeepEqual(freshBk, bk) {
		return errors.New(msg.AnimStateChanged)
	}
	if zero {
		if err := e.store.Remove(); err != nil {
			return cleanErr(err)
		}
		_, _ = fmt.Fprintln(out, msg.AnimBackupRemoved) // só DEPOIS de Remove() retornar nil
		return nil
	}
	res, err := animacoes.ApplyUndo(ctx, e.sys, e.store, fresh, freshBk)
	if err != nil {
		if res != nil && len(res.Applied) > 0 {
			_, _ = fmt.Fprintf(out, msg.AnimRestoredPart+"\n", len(res.Applied))
		}
		return cleanErr(err)
	}
	_, _ = fmt.Fprintf(out, msg.AnimRestored+"\n", len(res.Applied))
	return nil
}

// formatAnimUndo mostra o que o desfazer restaura (valor atual -> original).
func formatAnimUndo(p *animacoes.Plan) string {
	var b strings.Builder
	b.WriteString(msg.AnimUndoHeader + "\n")
	for _, c := range p.Changes {
		if c.Pending {
			fmt.Fprintf(&b, msg.PrivPendingFormat+"\n", c.Target, c.Before, c.After)
		}
	}
	return b.String()
}
