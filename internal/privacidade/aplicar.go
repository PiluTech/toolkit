package privacidade

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Result é o que Apply fez.
type Result struct {
	Applied    []Change
	AlreadyOK  int
	BackupPath string
}

// Apply aplica as mudanças PENDENTES do plano, nesta ordem de segurança:
//  1. se alguma exige administrador e o processo não está elevado, devolve ErrNeedsAdmin SEM tocar em nada;
//  2. grava o backup com o estado ORIGINAL de cada item que vai mudar (o primeiro original de cada item vence);
//  3. só então muda, um a um. Se algo falha, para e devolve o erro com o que já foi aplicado (o backup já tem tudo).
//
// A confirmação do usuário é de quem chama: Apply nunca pergunta.
func Apply(ctx context.Context, sys System, store Store, plan *Plan, now time.Time) (*Result, error) {
	res := &Result{BackupPath: store.Path()}
	var pending []Change
	for _, c := range plan.Changes {
		if c.Pending {
			pending = append(pending, c)
		} else {
			res.AlreadyOK++
		}
	}
	if len(pending) == 0 {
		return res, nil
	}
	if plan.NeedsAdmin() && !sys.IsAdmin(ctx) {
		return res, ErrNeedsAdmin
	}
	bk, err := store.Load()
	if err != nil {
		return res, err
	}
	if bk == nil {
		bk = &Backup{Version: 1, Created: now, Entries: map[string]Entry{}}
	}
	for _, c := range pending {
		if _, ok := bk.Entries[c.ID]; !ok {
			bk.Entries[c.ID] = c.orig
		}
	}
	if err := store.Save(bk); err != nil {
		return res, fmt.Errorf("não deu para gravar o backup em %s (nada foi alterado): %w", store.Path(), err)
	}
	for _, c := range pending {
		if err := c.apply(ctx, sys); err != nil {
			return res, fmt.Errorf("%s: %w", c.Target, err)
		}
		res.Applied = append(res.Applied, c)
	}
	return res, nil
}

// BuildUndo monta o plano de DESFAZER a partir do backup: cada item volta ao estado original. Backup ausente =
// ErrNoBackup. Serviço ou tarefa que não existe mais é ignorado. Todas as entradas são validadas contra o catálogo
// ANTES de qualquer coisa (ErrBackupInvalido; o arquivo não é tocado): o backup é dado não confiável.
func BuildUndo(ctx context.Context, sys System, store Store, l Lang) (*Plan, *Backup, error) {
	bk, err := store.Load()
	if err != nil {
		return nil, nil, err
	}
	if bk == nil || len(bk.Entries) == 0 {
		return nil, nil, ErrNoBackup
	}
	ix := newCatalogIndex()
	for id, e := range bk.Entries { // TODAS as entradas são conferidas antes de ler ou mudar qualquer coisa
		if err := ix.validateEntry(id, e); err != nil {
			return nil, nil, err
		}
	}
	ed, _ := sys.Edition(ctx)
	plan := &Plan{Perfil: "desfazer", Edition: ed, IsAdmin: sys.IsAdmin(ctx)}
	ids := make([]string, 0, len(bk.Entries))
	for id := range bk.Entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := bk.Entries[id]
		switch e.Kind {
		case "reg":
			cur, exists, err := sys.RegRead(ctx, e.Key, e.Name)
			if err != nil {
				return nil, nil, fmt.Errorf("%s\\%s: %w", e.Key, e.Name, err)
			}
			orig := RegValue{Kind: e.RegKind, DWORD: e.DWORD, Str: e.Str}
			c := Change{Setting: "desfazer", Kind: "reg", ID: id, Target: e.Key + `\` + e.Name, Admin: isHKLM(e.Key),
				Before: describeReg(cur, exists, l), After: describeReg(orig, e.Existed, l), Pending: exists != e.Existed || (exists && cur != orig),
				Undo: regUndo(e.Key, e.Name, cur, exists)}
			key, name, existed := e.Key, e.Name, e.Existed
			c.apply = func(ctx context.Context, s System) error {
				if existed {
					return s.RegWrite(ctx, key, name, orig)
				}
				return s.RegDelete(ctx, key, name)
			}
			plan.Changes = append(plan.Changes, c)
		case "service":
			st, err := sys.ServiceRead(ctx, e.Service)
			if err != nil {
				return nil, nil, fmt.Errorf("serviço %s: %w", e.Service, err)
			}
			if !st.Exists {
				plan.Missing = append(plan.Missing, "serviço "+e.Service)
				continue
			}
			origState := ServiceState{Exists: true, StartType: e.StartType, Running: e.Running}
			c := Change{Setting: "desfazer", Kind: "service", ID: id, Target: "serviço " + e.Service, Admin: true,
				Before: describeService(st, l), After: describeService(origState, l), Pending: st.StartType != e.StartType || st.Running != e.Running,
				Undo: serviceUndo(e.Service, st)}
			name := e.Service
			c.apply = func(ctx context.Context, s System) error {
				if err := s.ServiceConfigure(ctx, name, origState.StartType); err != nil {
					return err
				}
				switch {
				case origState.Running && !st.Running:
					return s.ServiceStart(ctx, name)
				case !origState.Running && st.Running:
					return s.ServiceStop(ctx, name)
				}
				return nil
			}
			plan.Changes = append(plan.Changes, c)
		case "task":
			st, err := sys.TaskRead(ctx, e.Task)
			if err != nil {
				return nil, nil, fmt.Errorf("tarefa %s: %w", e.Task, err)
			}
			if !st.Exists {
				plan.Missing = append(plan.Missing, "tarefa "+e.Task)
				continue
			}
			c := Change{Setting: "desfazer", Kind: "task", ID: id, Target: "tarefa " + e.Task, Admin: true,
				Before: describeTask(st.Enabled, l), After: describeTask(e.Enabled, l), Pending: st.Enabled != e.Enabled, Undo: taskUndo(e.Task, st.Enabled)}
			path, enabled := e.Task, e.Enabled
			c.apply = func(ctx context.Context, s System) error { return s.TaskSetEnabled(ctx, path, enabled) }
			plan.Changes = append(plan.Changes, c)
		}
	}
	return plan, bk, nil
}

// ApplyUndo restaura o que o plano de desfazer manda. Mesmas garantias do Apply (administrador checado antes de tocar em
// qualquer coisa). Depois de restaurar TUDO, o backup é apagado; se algo falha, os itens já restaurados saem do backup e os
// demais ficam para uma nova tentativa.
func ApplyUndo(ctx context.Context, sys System, store Store, plan *Plan, bk *Backup) (*Result, error) {
	res := &Result{BackupPath: store.Path()}
	var pending []Change
	for _, c := range plan.Changes {
		if c.Pending {
			pending = append(pending, c)
		} else {
			res.AlreadyOK++
		}
	}
	if plan.NeedsAdmin() && !sys.IsAdmin(ctx) {
		return res, ErrNeedsAdmin
	}
	for _, c := range pending {
		if err := c.apply(ctx, sys); err != nil {
			// salva o que sobrou
			for _, done := range res.Applied {
				delete(bk.Entries, done.ID)
			}
			_ = store.Save(bk)
			return res, fmt.Errorf("%s: %w", c.Target, err)
		}
		res.Applied = append(res.Applied, c)
	}
	return res, store.Remove()
}
