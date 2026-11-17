package cli

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/PiluTech/toolkit/internal/textsafe"
	"github.com/PiluTech/toolkit/internal/tui"
	"github.com/PiluTech/toolkit/internal/usb"
)

// newUsbTab monta a seção "USB permitidos (experimental)" da TUI sobre o mesmo núcleo do `toolkit usb` (mesmo System, mesmo
// Store, mesmas regras: prova antes, backup antes, tarefa de reversão conferida antes da primeira chave, nunca eleva
// sozinho). Fora do Windows (deps.GOOS) a seção só avisa, sem chamar nada.
func newUsbTab(ctx context.Context, deps Deps, th tui.Theme) tui.Tab {
	return tui.NewUsbTab(ctx, &usbBackend{deps: deps}, th)
}

// usbBackend implementa tui.UsbBackend. O ambiente é montado na primeira leitura e reaproveitado.
type usbBackend struct {
	deps Deps
	mu   sync.Mutex
	env  *usbEnv
	// prepared guarda os planos mostrados, por ID opaco. A TUI guarda o ID do plano EXIBIDO e a confirmação chama
	// Apply/Keep/Undo(ID): só esse plano é utilizável (uso único) e, ao usar um ID, todos os demais são descartados. Uma
	// preparação cancelada (Esc) que termina tarde grava sob o SEU ID, que a TUI nunca usa. A ação ainda exige que o plano
	// recalculado agora seja IDÊNTICO ao mostrado; senão ErrUsbStalePlan.
	prepared map[string]*usbPrepared
	nextID   int
}

type usbPrepared struct {
	kind     string // "apply", "keep" ou "undo"
	text     string
	excluded []string
	prazo    int
	snapshot *usb.Snapshot
}

func (b *usbBackend) prepare(p *usbPrepared) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.prepared == nil || len(b.prepared) >= maxPrepared {
		b.prepared = map[string]*usbPrepared{}
	}
	b.nextID++
	id := "plano-usb-" + strconv.Itoa(b.nextID)
	b.prepared[id] = p
	return id
}

// take devolve e CONSOME o plano com esse ID (uso único) e descarta todos os outros.
func (b *usbBackend) take(id string) *usbPrepared {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.prepared[id]
	b.prepared = nil
	return p
}

func (b *usbBackend) getEnv(ctx context.Context) (usbEnv, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.env != nil {
		return *b.env, nil
	}
	e, err := newUsbEnv(ctx, b.deps)
	if err != nil {
		return usbEnv{}, err
	}
	b.env = &e
	return e, nil
}

func (b *usbBackend) Load(ctx context.Context) (tui.UsbInfo, error) {
	if b.deps.GOOS != "windows" {
		return tui.UsbInfo{OS: printable(b.deps.GOOS)}, nil
	}
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.UsbInfo{}, err
	}
	plan, err := usb.BuildPlan(ctx, e.sys, nil)
	if err != nil {
		return tui.UsbInfo{}, cleanErr(err)
	}
	info := tui.UsbInfo{Supported: true, OS: "windows", Edition: plan.Edition, EditionOK: usb.EditionSupported(plan.Edition), IsAdmin: plan.IsAdmin, State: tui.UsbClean}
	for _, u := range plan.Units {
		info.Devices = append(info.Devices, tui.UsbDevice{Key: u.Key, Name: u.Name, VIDPID: u.VIDPID, Class: u.Class, Essential: u.Essential})
	}
	st, err := usb.ReadStatus(ctx, e.sys, e.store)
	if err != nil {
		return tui.UsbInfo{}, cleanErr(err)
	}
	info.RevertNote = st.RevertNote
	switch st.Kind {
	case usb.StatusArmed:
		info.State, info.Deadline = tui.UsbArmed, fmtDeadline(st.Task.Next)
	case usb.StatusActive:
		info.State = tui.UsbActive
	case usb.StatusStale:
		info.State = tui.UsbStale
	case usb.StatusUnknown:
		info.State, info.Reason = tui.UsbUnknown, st.Reason
	}
	return info, nil
}

func (b *usbBackend) Plan(ctx context.Context, excluded []string) (tui.UsbPlan, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.UsbPlan{}, err
	}
	plan, err := usb.BuildPlan(ctx, e.sys, excluded)
	if err != nil {
		return tui.UsbPlan{}, cleanErr(err)
	}
	text := plan.Format()
	id := b.prepare(&usbPrepared{kind: "apply", text: text, excluded: excluded, prazo: plan.Prazo})
	out := tui.UsbPlan{ID: id, Text: textsafe.Multiline(text + usbExeWarningText()), OK: plan.OK(), Prazo: plan.Prazo, IsAdmin: plan.IsAdmin, Refusal: textsafe.Line(strings.Join(plan.Refusals, "; "))}
	for _, u := range plan.Units {
		switch {
		case u.Selected:
			out.Allowed++
		default:
			out.Deselected++
			if u.Blocked {
				out.Blocked++
			}
		}
	}
	return out, nil
}

func (b *usbBackend) Apply(ctx context.Context, planID string) (tui.UsbResult, error) {
	prep := b.take(planID)
	if prep == nil || prep.kind != "apply" {
		return tui.UsbResult{}, tui.ErrUsbStalePlan
	}
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.UsbResult{}, err
	}
	plan, err := usb.BuildPlan(ctx, e.sys, prep.excluded)
	if err != nil {
		return tui.UsbResult{}, cleanErr(err)
	}
	if !plan.OK() || plan.Format() != prep.text {
		return tui.UsbResult{}, tui.ErrUsbStalePlan // o sistema, a seleção ou a prova mudaram desde o plano mostrado
	}
	if !e.sys.IsAdmin(ctx) { // recusa ANTES de qualquer mudança
		return tui.UsbResult{}, tui.ErrUsbNeedsAdmin
	}
	res, err := usb.Apply(ctx, e.sys, e.store, usb.ApplyOptions{Prazo: prep.prazo, Now: e.now(), Excluded: prep.excluded, ExpectedPlan: prep.text})
	out := tui.UsbResult{}
	if res != nil {
		out = tui.UsbResult{Entries: res.Entries, BackupPath: res.BackupPath, RestoredNow: res.Restored}
		if !res.Deadline.IsZero() {
			out.Deadline = fmtDeadline(res.Deadline)
		}
	}
	return out, tuiUsbErr(err)
}

func (b *usbBackend) KeepPlan(ctx context.Context) (tui.UsbPlan, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.UsbPlan{}, err
	}
	st, err := usb.ReadStatus(ctx, e.sys, e.store)
	if err != nil {
		return tui.UsbPlan{}, cleanErr(err)
	}
	if st.Kind != usb.StatusArmed {
		return tui.UsbPlan{Text: "Não há reversão automática pendente.", Refusal: "não há reversão pendente para confirmar"}, nil
	}
	text := keepText(st)
	id := b.prepare(&usbPrepared{kind: "keep", text: text, snapshot: st.Snapshot})
	return tui.UsbPlan{ID: id, Text: textsafe.Multiline(text), OK: true, IsAdmin: e.sys.IsAdmin(ctx)}, nil
}

func keepText(st *usb.Status) string {
	return fmt.Sprintf("Manter o modo empresa de USB (EXPERIMENTAL)\n\nA tarefa de reversão \"%s%s\" (próximo disparo às %s) será removida: a política continua aplicada e o snapshot continua em %s para o desfazer.\n\n%s\n", usb.TaskPath, usb.TaskName, fmtDeadline(st.Task.Next), usb.ProtectedDir, usb.TextLimit)
}

func (b *usbBackend) Keep(ctx context.Context, planID string) (tui.UsbResult, error) {
	prep := b.take(planID)
	if prep == nil || prep.kind != "keep" {
		return tui.UsbResult{}, tui.ErrUsbStalePlan
	}
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.UsbResult{}, err
	}
	st, err := usb.ReadStatus(ctx, e.sys, e.store)
	if err != nil {
		return tui.UsbResult{}, cleanErr(err)
	}
	if st.Kind != usb.StatusArmed || keepText(st) != prep.text || !reflect.DeepEqual(st.Snapshot, prep.snapshot) {
		return tui.UsbResult{}, tui.ErrUsbStalePlan
	}
	if !e.sys.IsAdmin(ctx) {
		return tui.UsbResult{}, tui.ErrUsbNeedsAdmin
	}
	return tui.UsbResult{}, tuiUsbErr(usb.Confirm(ctx, e.sys, e.store))
}

func (b *usbBackend) UndoPlan(ctx context.Context) (tui.UsbPlan, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.UsbPlan{}, err
	}
	plan, err := usb.BuildUndo(ctx, e.sys, e.store)
	if err != nil {
		return tui.UsbPlan{}, cleanErr(err)
	}
	text := plan.Format()
	id := b.prepare(&usbPrepared{kind: "undo", text: text, snapshot: plan.Status.Snapshot})
	out := tui.UsbPlan{ID: id, Text: textsafe.Multiline(text), OK: plan.Kind == usb.UndoRestore, IsAdmin: e.sys.IsAdmin(ctx)}
	switch plan.Kind {
	case usb.UndoNothing:
		out.Refusal = "nunca houve aplicação: nada a desfazer"
	case usb.UndoUnknown:
		out.Refusal = "ESTADO ORIGINAL DESCONHECIDO: nada é restaurado às cegas (veja a recuperação manual acima)"
	case usb.UndoConflict:
		out.Refusal = textsafe.Line(plan.Reason)
	}
	return out, nil
}

func (b *usbBackend) Undo(ctx context.Context, planID string) (tui.UsbResult, error) {
	prep := b.take(planID)
	if prep == nil || prep.kind != "undo" {
		return tui.UsbResult{}, tui.ErrUsbStalePlan
	}
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.UsbResult{}, err
	}
	plan, err := usb.BuildUndo(ctx, e.sys, e.store)
	if err != nil {
		return tui.UsbResult{}, cleanErr(err)
	}
	if plan.Kind != usb.UndoRestore || plan.Format() != prep.text || !reflect.DeepEqual(plan.Status.Snapshot, prep.snapshot) {
		return tui.UsbResult{}, tui.ErrUsbStalePlan // o backup ou o sistema mudou desde o plano mostrado
	}
	if !e.sys.IsAdmin(ctx) {
		return tui.UsbResult{}, tui.ErrUsbNeedsAdmin
	}
	res, err := usb.Undo(ctx, e.sys, e.store)
	out := tui.UsbResult{}
	if res != nil {
		out = tui.UsbResult{Restored: res.Restored, Nothing: res.Nothing}
	}
	return out, tuiUsbErr(err)
}

// tuiUsbErr traduz os erros do núcleo para os que a seção entende e limpa o texto dos demais.
func tuiUsbErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, usb.ErrNeedsAdmin):
		return tui.ErrUsbNeedsAdmin
	case errors.Is(err, usb.ErrPlanChanged):
		return tui.ErrUsbStalePlan
	}
	return cleanErr(err)
}
