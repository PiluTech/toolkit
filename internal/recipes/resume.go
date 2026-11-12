package recipes

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiluTech/toolkit/internal/state"
)

// ResumePending implementa `toolkit resume`: continua a receita ativa depois do reboot. Termina SEMPRE
// com Unregister (sucesso, falha ou panic, via defer), exceto quando uma etapa NOVA pede reboot (aí a
// retomada continua registrada) ou quando nada avançou porque a máquina ainda não reiniciou.
func (e *Engine) ResumePending(ctx context.Context) (rep Report, err error) {
	f, err := e.Store.Load(ctx)
	if err != nil {
		return rep, err
	}
	rep.Resumed = true
	id := f.Active
	if id == "" {
		// Nada ativo: um registro esquecido não pode ficar rodando a cada boot.
		_ = e.Resume.Unregister(ctx)
		return rep, ErrNothingResume
	}
	rep.RecipeID = id
	rs := f.Recipes[id]

	keep := false
	defer func() {
		if keep {
			return
		}
		if uerr := e.Resume.Unregister(ctx); uerr != nil {
			err = errors.Join(err, fmt.Errorf("não consegui remover a retomada: %w", uerr))
		}
	}()

	r, ok := e.lookup(id)
	if !ok {
		e.markFailed(ctx, id, "receita não existe nesta versão do toolkit")
		rep.Status = state.RecipeFailed
		return rep, fmt.Errorf("%w: %s", ErrRecipeNotFound, id)
	}
	if r.Hash != rs.RecipeHash {
		e.markFailed(ctx, id, "o YAML da receita mudou desde o início")
		rep.Status = state.RecipeFailed
		return rep, ErrRecipeChanged
	}
	if r.OS != e.Platform.OS {
		return rep, fmt.Errorf("%w: %s", ErrWrongOS, id)
	}
	opt := RunOptions{Yes: rs.Options.Yes, Verbose: rs.Options.Verbose}

	idx := -1
	for i, st := range r.Steps {
		if s := rs.Step(st.ID); s == nil || (s.Status != state.StepDone && s.Status != state.StepSkipped) {
			idx = i
			break
		}
	}
	if idx < 0 { // tudo pronto mas o Active ficou: só fecha
		rep.Status = state.RecipeDone
		return rep, e.Store.Update(ctx, func(f *state.File) error {
			if x := f.Recipes[id]; x != nil {
				x.Status, x.UpdatedAt = state.RecipeDone, e.now()
			}
			f.Active = ""
			return nil
		})
	}
	st := r.Steps[idx]
	ss := rs.Step(st.ID)
	if ss == nil {
		return rep, fmt.Errorf("etapa %q ausente no state da receita %q (arquivo alterado ou de outra versão): use `recipe reset %s`", st.ID, id, id)
	}

	switch ss.Status {
	case state.StepAwaitingReboot:
		if ss.BootID != "" && ss.BootID == e.Platform.BootID {
			keep = true // nada avança e a retomada continua registrada
			rep.Status, rep.RebootPending = state.RecipeAwaitingReboot, true
			return rep, ErrNotRebooted
		}
		if serr := e.mutateStep(ctx, id, st.ID, func(s *state.StepState) { s.ResumeAttempts++ }); serr != nil {
			return rep, serr
		}
		if st.Check != nil {
			code, _, cerr := e.runAction(ctx, r, st, st.Check, true, false)
			if cerr != nil || !st.Check.OKExit(code) {
				msg := "o check não passou depois do reboot"
				if cerr != nil {
					msg = fmt.Sprintf("o check não executou depois do reboot: %v", cerr)
				}
				rep.Status = state.RecipeFailed
				rep.Steps = append(rep.Steps, StepReport{ID: st.ID, Name: st.Name, Outcome: OutcomeFailed, Error: msg})
				e.notReached(r, idx+1, &rep)
				if ferr := e.failRecipe(ctx, r, st, code, msg); ferr != nil {
					return rep, ferr
				}
				return rep, fmt.Errorf("etapa %q: %s", st.ID, msg)
			}
		}
		if serr := e.finishStep(ctx, r, st, state.StepDone, 0, ""); serr != nil {
			return rep, serr
		}
		rep.Steps = append(rep.Steps, StepReport{ID: st.ID, Name: st.Name, Outcome: OutcomeDone})
		idx++
	case state.StepRunning:
		// Caiu no meio da etapa (queda de energia, reboot sem pedido). Só repete com check.
		if st.Check == nil {
			msg := "etapa interrompida e sem check: confira o estado da máquina e use \"toolkit recipe reset " + id + "\""
			rep.Status = state.RecipeFailed
			rep.Steps = append(rep.Steps, StepReport{ID: st.ID, Name: st.Name, Outcome: OutcomeFailed, Error: msg})
			e.notReached(r, idx+1, &rep)
			if ferr := e.failRecipe(ctx, r, st, 0, msg); ferr != nil {
				return rep, ferr
			}
			return rep, fmt.Errorf("etapa %q: %s", st.ID, msg)
		}
		if ss.ResumeAttempts >= 1 {
			msg := "a etapa já foi repetida uma vez depois de uma interrupção"
			rep.Status = state.RecipeFailed
			rep.Steps = append(rep.Steps, StepReport{ID: st.ID, Name: st.Name, Outcome: OutcomeFailed, Error: msg})
			e.notReached(r, idx+1, &rep)
			if ferr := e.failRecipe(ctx, r, st, 0, msg); ferr != nil {
				return rep, ferr
			}
			return rep, fmt.Errorf("etapa %q: %s", st.ID, msg)
		}
		if serr := e.mutateStep(ctx, id, st.ID, func(s *state.StepState) { s.ResumeAttempts++ }); serr != nil {
			return rep, serr
		}
		// execStep roda o check de novo (passou = skipped) e senão repete a etapa.
	}

	keep, err = e.runFrom(ctx, r, idx, opt, &rep)
	if errors.Is(err, ErrRebootDeclined) {
		keep = true
	}
	return rep, err
}

func (e *Engine) lookup(id string) (*Recipe, bool) {
	if e.Recipes == nil {
		return nil, false
	}
	return e.Recipes(id)
}

func (e *Engine) markFailed(ctx context.Context, id, msg string) {
	_ = e.Store.Update(ctx, func(f *state.File) error {
		if rs := f.Recipes[id]; rs != nil {
			rs.Status, rs.UpdatedAt = state.RecipeFailed, e.now()
			for i := range rs.Steps {
				if s := &rs.Steps[i]; s.Status == state.StepRunning || s.Status == state.StepAwaitingReboot {
					s.Status, s.Error = state.StepFailed, msg
				}
			}
		}
		if f.Active == id {
			f.Active = ""
		}
		return nil
	})
}

// Reset descarta o estado da receita (libera uma nova tentativa manual) e remove a retomada se ela
// estava pendente.
func (e *Engine) Reset(ctx context.Context, id string) error {
	var wasPending bool
	err := e.Store.Update(ctx, func(f *state.File) error {
		rs := f.Recipes[id]
		if rs == nil {
			return fmt.Errorf("%w: %s", ErrUnknownRecipe, id)
		}
		wasPending = rs.Status == state.RecipeRunning || rs.Status == state.RecipeAwaitingReboot
		delete(f.Recipes, id)
		if f.Active == id {
			f.Active = ""
		}
		return nil
	})
	if err != nil {
		return err
	}
	if wasPending {
		return e.Resume.Unregister(ctx)
	}
	return nil
}
