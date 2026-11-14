package animacoes

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/PiluTech/toolkit/internal/privacidade"
)

// Result é o que Apply (ou ApplyUndo) fez.
type Result struct {
	Applied    []Change
	AlreadyOK  int
	BackupPath string
}

// Apply aplica as mudanças PENDENTES do plano, nesta ordem de segurança:
//  1. carrega o backup existente e o valida contra o catálogo (adulterado ou de outro dono = recusa, nada é escrito);
//  2. grava o backup com o estado ORIGINAL de cada item que vai mudar (o primeiro original de cada item vence);
//  3. só então muda, um a um. Se algo falha, para e devolve o erro com o que já foi aplicado (o backup já tem tudo).
//
// A confirmação do usuário é de quem chama: Apply nunca pergunta.
func Apply(ctx context.Context, sys System, store privacidade.Store, plan *Plan, now time.Time) (*Result, error) {
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
	bk, err := store.Load()
	if err != nil {
		return res, err
	}
	if bk == nil {
		bk = &privacidade.Backup{Version: 1, Created: now, Entries: map[string]privacidade.Entry{}}
	}
	if err := validate(bk); err != nil {
		return res, err
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

// BuildUndo monta o plano de DESFAZER a partir do backup. Backup ausente = ErrNoBackup. TODAS as entradas são validadas
// contra o catálogo ANTES de qualquer leitura do sistema (ErrBackupInvalido; o arquivo não é tocado).
func BuildUndo(ctx context.Context, sys System, store privacidade.Store, l Lang) (*Plan, *privacidade.Backup, error) {
	bk, err := store.Load()
	if err != nil {
		return nil, nil, err
	}
	if bk == nil || len(bk.Entries) == 0 {
		return nil, nil, ErrNoBackup
	}
	if err := validate(bk); err != nil {
		return nil, nil, err
	}
	plan := &Plan{Undo: true}
	ids := make([]string, 0, len(bk.Entries))
	for id := range bk.Entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := bk.Entries[id]
		cur, exists, err := sys.RegRead(ctx, e.Key, e.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("%s\\%s: %w", e.Key, e.Name, err)
		}
		orig := RegValue{Kind: e.RegKind, DWORD: e.DWORD, Str: e.Str}
		c := Change{Item: "desfazer", ID: id, Target: e.Key + `\` + e.Name,
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
	}
	return plan, bk, nil
}

// ApplyUndo restaura o que o plano de desfazer manda. Depois de restaurar TUDO, o backup é apagado; se algo falha, os
// itens já restaurados saem do backup e os demais ficam para uma nova tentativa.
func ApplyUndo(ctx context.Context, sys System, store privacidade.Store, plan *Plan, bk *privacidade.Backup) (*Result, error) {
	res := &Result{BackupPath: store.Path()}
	var pending []Change
	for _, c := range plan.Changes {
		if c.Pending {
			pending = append(pending, c)
		} else {
			res.AlreadyOK++
		}
	}
	for _, c := range pending {
		if err := c.apply(ctx, sys); err != nil {
			for _, done := range res.Applied {
				delete(bk.Entries, done.ID)
			}
			if len(res.Applied) > 0 {
				_ = store.Save(bk)
			}
			return res, fmt.Errorf("%s: %w", c.Target, err)
		}
		res.Applied = append(res.Applied, c)
	}
	return res, store.Remove()
}
