package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"time"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/textsafe"
	"github.com/PiluTech/toolkit/internal/usb"
)

// UsbDeps são os pontos injetáveis do `toolkit usb` e da seção USB da TUI (os testes nunca tocam o Windows real).
type UsbDeps struct {
	// System é o Windows visto pelo toolkit (nil = usb.NewWindowsSystem sobre o Runner do Deps, com a elevação perguntada
	// ao PowerShell). Nada é elevado: sem administrador o aplicar recusa.
	System usb.System
	// Store guarda o snapshot (nil = usb.FileStore na pasta protegida C:\ProgramData\toolkit).
	Store usb.Store
	Now   func() time.Time
}

type usbFlags struct {
	prazo  int
	dryRun bool
	yes    bool
}

func newUsbCmd(deps Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "usb",
		Short: msg.UsbShort,
		Long:  msg.UsbLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUsbState(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	f := usbFlags{prazo: usb.PrazoPadrao}
	apply := &cobra.Command{
		Use:   "aplicar",
		Short: msg.UsbApplyShort,
		Long:  msg.UsbApplyLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUsbApply(cmd.Context(), cmd.OutOrStdout(), deps, f)
		},
	}
	apply.Flags().IntVar(&f.prazo, "prazo", usb.PrazoPadrao, msg.FlagUsbPrazo)
	apply.Flags().BoolVar(&f.dryRun, "dry-run", false, msg.FlagPrivDryRun)
	apply.Flags().BoolVarP(&f.yes, "yes", "y", false, msg.FlagPrivYes)

	confirm := &cobra.Command{
		Use:   "confirmar",
		Short: msg.UsbConfirmShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUsbConfirm(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}

	var u usbFlags
	undo := &cobra.Command{
		Use:   "desfazer",
		Short: msg.UsbUndoShort,
		Long:  msg.UsbUndoLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUsbUndo(cmd.Context(), cmd.OutOrStdout(), deps, u)
		},
	}
	undo.Flags().BoolVar(&u.dryRun, "dry-run", false, msg.FlagPrivDryRun)
	undo.Flags().BoolVarP(&u.yes, "yes", "y", false, msg.FlagPrivYes)

	// subcomando OCULTO executado pela tarefa de reversão (SYSTEM, sem terminal): restaura o snapshot sem perguntar.
	revert := &cobra.Command{
		Use:    "reverter",
		Short:  msg.UsbRevertShort,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUsbRevert(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	c.AddCommand(apply, confirm, undo, revert)
	return c
}

// usbEnv junta o que os modos (e a TUI) usam. Fora do Windows devolve o erro ANTES de montar qualquer coisa.
type usbEnv struct {
	sys   usb.System
	store usb.Store
	now   func() time.Time
}

func newUsbEnv(ctx context.Context, deps Deps) (usbEnv, error) {
	if deps.GOOS != "windows" {
		return usbEnv{}, fmt.Errorf(msg.UsbOnlyWindows, printable(deps.GOOS))
	}
	e := usbEnv{now: deps.Usb.Now, sys: deps.Usb.System, store: deps.Usb.Store}
	if e.now == nil {
		e.now = time.Now
	}
	if e.sys == nil {
		r := deps.runner()
		e.sys = usb.NewWindowsSystem(r, func() bool { return platform.IsElevatedViaPowerShell(ctx, r) })
	}
	if e.store == nil {
		e.store = usb.FileStore{Dir: usb.ProtectedDir}
	}
	return e, nil
}

func usbErr(err error) error {
	if errors.Is(err, usb.ErrNeedsAdmin) {
		return errors.New(msg.UsbNeedsAdmin)
	}
	if errors.Is(err, usb.ErrPlanChanged) {
		return errors.New(msg.UsbStale)
	}
	return cleanErr(err)
}

func fmtDeadline(t time.Time) string { return t.Local().Format("15:04") }

func runUsbState(ctx context.Context, out io.Writer, deps Deps) error {
	ctx = orBackground(ctx)
	e, err := newUsbEnv(ctx, deps)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, usb.TextExperimental)
	st, err := usb.ReadStatus(ctx, e.sys, e.store)
	if err != nil {
		return usbErr(err)
	}
	if st.RevertNote != "" {
		_, _ = fmt.Fprintln(out, textsafe.Line(st.RevertNote)+".")
	}
	switch st.Kind {
	case usb.StatusClean:
		_, _ = fmt.Fprintln(out, msg.UsbStateClean)
	case usb.StatusArmed:
		_, _ = fmt.Fprintf(out, msg.UsbStateArmed+"\n", fmtDeadline(st.Task.Next))
	case usb.StatusActive:
		_, _ = fmt.Fprintln(out, msg.UsbStateActive)
	case usb.StatusStale:
		_, _ = fmt.Fprintln(out, msg.UsbStateStale)
	case usb.StatusUnknown:
		_, _ = fmt.Fprintln(out, msg.UsbStateUnknown)
		_, _ = fmt.Fprintln(out, textsafe.Line(st.Reason)+".")
	}
	_, _ = fmt.Fprintln(out, usb.TextLimit)
	return nil
}

func runUsbApply(ctx context.Context, out io.Writer, deps Deps, f usbFlags) error {
	ctx = orBackground(ctx)
	e, err := newUsbEnv(ctx, deps)
	if err != nil {
		return err
	}
	if f.prazo < usb.PrazoMinimo || f.prazo > usb.PrazoMaximo {
		return fmt.Errorf("%w: use de %d a %d minutos", usb.ErrPrazo, usb.PrazoMinimo, usb.PrazoMaximo)
	}
	plan, err := usb.BuildPlan(ctx, e.sys, nil)
	if err != nil {
		return usbErr(err)
	}
	plan.Prazo = f.prazo
	_, _ = fmt.Fprint(out, textsafe.Multiline(plan.Format()+usbExeWarningText()))
	if !plan.OK() {
		return fmt.Errorf("recusado, nada foi alterado: %w", usbErr(plan.Err()))
	}
	if f.dryRun {
		_, _ = fmt.Fprintln(out, msg.UsbDryRunApply)
		return nil
	}
	if !e.sys.IsAdmin(ctx) { // recusa ANTES de perguntar e antes de qualquer mudança
		return errors.New(msg.UsbNeedsAdmin)
	}
	if ok, err := privConfirm(ctx, out, deps, f.yes, msg.UsbConfirmApply); !ok || err != nil {
		return err
	}
	// o plano é recalculado DEPOIS da confirmação e antes de qualquer backup, tarefa ou chave: o que será gravado tem de ser
	// idêntico ao que foi mostrado (vale também com --yes).
	fresh, err := usb.BuildPlan(ctx, e.sys, nil)
	if err != nil {
		return usbErr(err)
	}
	fresh.Prazo = f.prazo
	if fresh.Format() != plan.Format() {
		return errors.New(msg.UsbStale)
	}
	res, err := usb.Apply(ctx, e.sys, e.store, usb.ApplyOptions{Prazo: f.prazo, Now: e.now(), ExpectedPlan: plan.Format()})
	if err != nil {
		if res != nil && res.Restored {
			_, _ = fmt.Fprintln(out, msg.UsbAppliedPart)
		}
		return usbErr(err)
	}
	_, _ = fmt.Fprintf(out, msg.UsbApplied+"\n", res.Entries, fmtDeadline(res.Deadline), printable(res.BackupPath))
	return nil
}

func runUsbConfirm(ctx context.Context, out io.Writer, deps Deps) error {
	ctx = orBackground(ctx)
	e, err := newUsbEnv(ctx, deps)
	if err != nil {
		return err
	}
	if err := usb.Confirm(ctx, e.sys, e.store); err != nil {
		return usbErr(err)
	}
	_, _ = fmt.Fprintln(out, msg.UsbConfirmed)
	return nil
}

func runUsbUndo(ctx context.Context, out io.Writer, deps Deps, f usbFlags) error {
	ctx = orBackground(ctx)
	e, err := newUsbEnv(ctx, deps)
	if err != nil {
		return err
	}
	plan, err := usb.BuildUndo(ctx, e.sys, e.store)
	if err != nil {
		return usbErr(err)
	}
	_, _ = fmt.Fprint(out, textsafe.Multiline(plan.Format()))
	switch plan.Kind {
	case usb.UndoNothing:
		return nil
	case usb.UndoUnknown, usb.UndoConflict:
		if f.dryRun {
			_, _ = fmt.Fprintln(out, msg.PrivDryRunDone)
			return nil
		}
		_, err := usb.Undo(ctx, e.sys, e.store) // devolve o erro tipado sem escrever nada
		return usbErr(err)
	}
	if f.dryRun {
		_, _ = fmt.Fprintln(out, msg.PrivDryRunDone)
		return nil
	}
	if !e.sys.IsAdmin(ctx) {
		return errors.New(msg.UsbNeedsAdmin)
	}
	if ok, err := privConfirm(ctx, out, deps, f.yes, msg.UsbConfirmUndo); !ok || err != nil {
		return err
	}
	// snapshot e sistema são relidos DEPOIS da confirmação e comparados ao que foi mostrado, antes de restaurar.
	fresh, err := usb.BuildUndo(ctx, e.sys, e.store)
	if err != nil {
		return usbErr(err)
	}
	if fresh.Format() != plan.Format() || fresh.Kind != plan.Kind || !reflect.DeepEqual(fresh.Status.Snapshot, plan.Status.Snapshot) {
		return errors.New(msg.UsbStale)
	}
	res, err := usb.Undo(ctx, e.sys, e.store)
	if err != nil {
		return usbErr(err)
	}
	_, _ = fmt.Fprintf(out, msg.UsbRestored+"\n", res.Restored)
	return nil
}

// runUsbRevert é o que a tarefa de reversão executa: o MESMO caminho do desfazer, sem pergunta (não há terminal).
func runUsbRevert(ctx context.Context, out io.Writer, deps Deps) error {
	ctx = orBackground(ctx)
	e, err := newUsbEnv(ctx, deps)
	if err != nil {
		return err
	}
	res, err := usb.Revert(ctx, e.sys, e.store) // SYSTEM, sem IsAdmin; grava o rastro na pasta protegida
	if err != nil {
		return usbErr(err)
	}
	if res.Nothing {
		_, _ = fmt.Fprintln(out, msg.UsbNothingToUndo)
		return nil
	}
	_, _ = fmt.Fprintf(out, msg.UsbRestored+"\n", res.Restored)
	return nil
}

// usbExeWarningText é o aviso (com quebra de linha na frente) de que o toolkit roda de uma pasta que usuário comum grava;
// vazio quando a pasta é de administrador. Só texto de exibição: nunca entra no plano comparado na confirmação.
func usbExeWarningText() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if w := usb.ExeFolderWarning(p); w != "" {
		return "\n" + msg.UsbWarnPrefix + w + "\n"
	}
	return ""
}
