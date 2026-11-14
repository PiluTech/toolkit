package appsnativos

import (
	"context"
	"fmt"
	"time"
)

// Options muda o comportamento de Apply.
type Options struct {
	// DryRun só valida e devolve o que seria removido (Result.Planned): não grava backup, não remove e não exige administrador.
	DryRun bool
}

// Result é o que Apply fez.
type Result struct {
	Removed    []Item
	Planned    []Item // só no DryRun
	DryRun     bool
	BackupPath string
}

// Apply remove os itens do plano, nesta ordem de segurança:
//  1. revalida CADA item contra o catálogo e a lista de negação (e as marcas framework/não removível/sistema): qualquer
//     item fora do catálogo é ErrItemInvalido e nada é gravado nem removido;
//  2. se o plano exige administrador e o processo não está elevado, devolve ErrNeedsAdmin SEM tocar em nada;
//  3. lê o backup existente (dono e catálogo conferidos) e grava a identidade de TODOS os itens ANTES da primeira remoção;
//  4. remove um a um; se algo falha, para, devolve o erro com o que já saiu e deixa no backup só o que foi removido, mais o
//     item que falhou (pode ter mudado pela metade).
//
// A confirmação do usuário é de quem chama: Apply nunca pergunta.
func Apply(ctx context.Context, sys System, store Store, plan *Plan, now time.Time, opt Options) (*Result, error) {
	res := &Result{BackupPath: store.Path(), DryRun: opt.DryRun}
	if plan.Scope != ScopeUser && plan.Scope != ScopeTodos {
		return res, fmt.Errorf("%w: escopo %q", ErrItemInvalido, printableShort(string(plan.Scope)))
	}
	for _, it := range plan.Items {
		if err := ValidateItem(it); err != nil {
			return res, err
		}
		if it.Scope != plan.Scope {
			return res, fmt.Errorf("%w: o escopo do item não é o do plano", ErrItemInvalido)
		}
	}
	if len(plan.Items) == 0 {
		return res, nil
	}
	if opt.DryRun {
		res.Planned = append([]Item(nil), plan.Items...)
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
	if err := ValidateBackup(bk); err != nil {
		return res, err
	}
	added := map[string]bool{}
	for _, it := range plan.Items {
		id := string(it.Kind) + ":" + it.FullName
		if _, ok := bk.Entries[id]; ok {
			continue
		}
		bk.Entries[id] = Entry{ID: id, Kind: string(it.Kind), App: it.App, Name: it.Name, PackageFullName: it.FullName,
			PackageFamilyName: it.FamilyName, Version: it.Version, Publisher: it.Publisher, Scope: string(it.Scope), Date: now}
		added[id] = true
	}
	if err := store.Save(bk); err != nil {
		return res, fmt.Errorf("não deu para gravar o backup em %s (nada foi alterado): %w", store.Path(), err)
	}
	for i, it := range plan.Items {
		if err := remove(ctx, sys, it); err != nil {
			// o backup fica só com o que mudou (removidos + o que falhou); os que nem foram tentados saem
			for _, rest := range plan.Items[i+1:] {
				if id := string(rest.Kind) + ":" + rest.FullName; added[id] {
					delete(bk.Entries, id)
				}
			}
			_ = store.Save(bk)
			return res, fmt.Errorf("%s: %w", it.Name, err)
		}
		res.Removed = append(res.Removed, it)
	}
	return res, nil
}

func remove(ctx context.Context, sys System, it Item) error {
	switch {
	case it.Kind == KindProvisioned:
		return sys.RemoveProvisioned(ctx, it.FullName)
	case it.Scope == ScopeTodos:
		return sys.RemoveAllUsers(ctx, it.FullName)
	}
	return sys.RemoveForUser(ctx, it.FullName)
}
