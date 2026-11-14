package cli

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"

	"github.com/PiluTech/toolkit/internal/animacoes"
	"github.com/PiluTech/toolkit/internal/privacidade"
	"github.com/PiluTech/toolkit/internal/textsafe"
	"github.com/PiluTech/toolkit/internal/tui"
)

// animBackend implementa tui.AnimBackend sobre o mesmo núcleo do `toolkit otimizacao animacoes` (mesmo System, mesmo
// Store, mesmas regras: plano antes, backup antes, só HKCU, nunca administrador). O ambiente é montado na primeira
// leitura e reaproveitado (no Windows ele consulta o SID do usuário).
type animBackend struct {
	deps Deps
	mu   sync.Mutex
	env  *animEnv
	// prepared guarda os planos mostrados, por ID opaco. A TUI guarda o ID do plano EXIBIDO e a confirmação chama
	// Apply/Undo(ID): só esse plano é aplicável (uso único) e, ao usar um ID, todos os demais são descartados. Uma
	// preparação cancelada (Esc) que termina tarde grava sob o SEU ID, que a TUI nunca usa. Apply/Undo ainda exigem que o
	// plano recalculado agora seja IDÊNTICO ao mostrado; senão ErrAnimStalePlan.
	prepared map[string]*animPrepared
	nextID   int
}

type animPrepared struct {
	undo    bool
	text    string
	pending int
	backup  *privacidade.Backup
}

func (b *animBackend) prepare(p *animPrepared) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.prepared == nil || len(b.prepared) >= maxPrepared {
		b.prepared = map[string]*animPrepared{}
	}
	b.nextID++
	id := "plano-anim-" + strconv.Itoa(b.nextID)
	b.prepared[id] = p
	return id
}

// take devolve e CONSOME o plano com esse ID (uso único) e descarta todos os outros.
func (b *animBackend) take(id string) *animPrepared {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.prepared[id]
	b.prepared = nil
	return p
}

func (b *animBackend) getEnv(ctx context.Context) (animEnv, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.env != nil {
		return *b.env, nil
	}
	e, err := newAnimEnv(ctx, b.deps)
	if err != nil {
		return animEnv{}, err
	}
	b.env = &e
	return e, nil
}

func (b *animBackend) Load(ctx context.Context) (tui.AnimInfo, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.AnimInfo{}, err
	}
	plan, err := animacoes.Build(ctx, e.sys, e.lang)
	if err != nil {
		return tui.AnimInfo{}, cleanErr(err)
	}
	titles := map[string]string{}
	for _, it := range animacoes.Items() {
		titles[it.ID] = it.Title(e.lang)
	}
	info := tui.AnimInfo{}
	for _, c := range plan.Changes {
		st := tui.AnimApplied
		if c.Pending {
			st = tui.AnimPending
		}
		info.Items = append(info.Items, tui.AnimItem{Title: titles[c.Item], State: st})
	}
	bk, err := e.store.Load() // só para saber se há o que desfazer; backup ilegível aparece no desfazer
	info.HasBackup = err == nil && bk != nil && len(bk.Entries) > 0
	return info, nil
}

func (b *animBackend) Plan(ctx context.Context) (tui.AnimPlan, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.AnimPlan{}, err
	}
	plan, err := animacoes.Build(ctx, e.sys, e.lang)
	if err != nil {
		return tui.AnimPlan{}, cleanErr(err)
	}
	text := plan.Format(e.lang)
	id := b.prepare(&animPrepared{text: text, pending: plan.Pending()})
	return tui.AnimPlan{ID: id, Text: textsafe.Multiline(text), Pending: plan.Pending()}, nil
}

func (b *animBackend) Apply(ctx context.Context, planID string) (tui.AnimResult, error) {
	prep := b.take(planID)
	if prep == nil || prep.undo {
		return tui.AnimResult{}, tui.ErrAnimStalePlan
	}
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.AnimResult{}, err
	}
	plan, err := animacoes.Build(ctx, e.sys, e.lang)
	if err != nil {
		return tui.AnimResult{}, cleanErr(err)
	}
	if plan.Format(e.lang) != prep.text || plan.Pending() != prep.pending {
		return tui.AnimResult{}, tui.ErrAnimStalePlan // o sistema mudou desde o plano mostrado
	}
	if plan.Pending() == 0 {
		return tui.AnimResult{}, nil
	}
	res, err := animacoes.Apply(ctx, e.sys, e.store, plan, e.now())
	return animResult(res), tuiAnimErr(err)
}

func (b *animBackend) UndoPlan(ctx context.Context) (tui.AnimPlan, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.AnimPlan{}, err
	}
	plan, bk, err := animacoes.BuildUndo(ctx, e.sys, e.store, e.lang)
	if err != nil {
		return tui.AnimPlan{}, tuiAnimErr(err)
	}
	text := formatAnimUndo(plan)
	id := b.prepare(&animPrepared{undo: true, text: text, pending: plan.Pending(), backup: bk})
	return tui.AnimPlan{ID: id, Text: textsafe.Multiline(text), Pending: plan.Pending()}, nil
}

func (b *animBackend) Undo(ctx context.Context, planID string) (tui.AnimResult, error) {
	prep := b.take(planID)
	if prep == nil || !prep.undo {
		return tui.AnimResult{}, tui.ErrAnimStalePlan
	}
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.AnimResult{}, err
	}
	plan, bk, err := animacoes.BuildUndo(ctx, e.sys, e.store, e.lang)
	if err != nil {
		return tui.AnimResult{}, tuiAnimErr(err)
	}
	if formatAnimUndo(plan) != prep.text || plan.Pending() != prep.pending || !reflect.DeepEqual(bk, prep.backup) {
		return tui.AnimResult{}, tui.ErrAnimStalePlan // o backup ou o sistema mudou desde o plano mostrado
	}
	if plan.Pending() == 0 { // tudo já está como antes: só o backup é removido (como o comando)
		return tui.AnimResult{Nothing: true}, tuiAnimErr(e.store.Remove())
	}
	res, err := animacoes.ApplyUndo(ctx, e.sys, e.store, plan, bk)
	return animResult(res), tuiAnimErr(err)
}

func animResult(r *animacoes.Result) tui.AnimResult {
	if r == nil {
		return tui.AnimResult{}
	}
	return tui.AnimResult{Applied: len(r.Applied), BackupPath: r.BackupPath}
}

// tuiAnimErr traduz os erros do núcleo para os que a seção entende e limpa o texto dos demais.
func tuiAnimErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, animacoes.ErrNoBackup):
		return tui.ErrAnimNoBackup
	}
	return cleanErr(err)
}
