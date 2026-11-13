package recipes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/resume"
	"github.com/PiluTech/toolkit/internal/state"
)

// Erros da engine.
var (
	ErrWrongOS        = errors.New("a receita é de outro sistema operacional")
	ErrNeedsAdmin     = errors.New("a receita exige administrador: rode o toolkit elevado")
	ErrBusy           = errors.New("outra receita está em andamento")
	ErrPending        = errors.New("a receita já está em andamento: use \"toolkit resume\" (ou \"toolkit recipe reset\" para cancelar)")
	ErrFailedState    = errors.New("a última execução da receita falhou: use \"toolkit recipe reset\" para tentar de novo")
	ErrNothingResume  = errors.New("não há receita para retomar")
	ErrNotRebooted    = errors.New("a máquina ainda não reiniciou: reinicie para continuar")
	ErrRecipeChanged  = errors.New("a receita mudou desde que começou (binário atualizado): retomada cancelada")
	ErrRecipeNotFound = errors.New("receita do estado não existe nesta versão")
	ErrUnknownRecipe  = errors.New("receita sem estado registrado")
	ErrRebootDeclined = errors.New("reinício recusado: a receita continua no próximo boot (ou rode \"toolkit resume\" depois de reiniciar)")
)

// InstallOutcome é o resultado de um passo `install:`.
type InstallOutcome struct{ RebootRequired bool }

// Installer é a ponte para o catálogo e os providers (passos `install:`).
type Installer interface {
	Install(ctx context.Context, apps []string) (InstallOutcome, error)
}

// Rebooter reinicia a máquina (interface separada: o teste só confere que foi chamado e que o state já estava salvo).
type Rebooter interface {
	Reboot(ctx context.Context) error
}

// Prompter confirma uma ação com a pessoa (TUI, terminal ou "sempre sim").
type Prompter interface {
	Confirm(ctx context.Context, question string) (bool, error)
}

// RunOptions são as flags de uma execução.
type RunOptions struct {
	Yes     bool // não pergunta antes de reiniciar
	DryRun  bool // só o plano: checks (leitura) rodam, o resto não; nada de state, retomada ou reboot
	Verbose bool
}

// Outcome é o resultado de uma etapa no relatório.
type Outcome string

// Resultados de etapa.
const (
	OutcomeDone           Outcome = "done"
	OutcomeSkipped        Outcome = "skipped"
	OutcomeFailed         Outcome = "failed"
	OutcomeAwaitingReboot Outcome = "awaiting-reboot"
	OutcomeWouldRun       Outcome = "would-run"
	OutcomeWouldSkip      Outcome = "would-skip"
	OutcomeNotReached     Outcome = "not-reached"
)

// StepReport é o resultado de uma etapa.
type StepReport struct {
	ID       string
	Name     string
	Outcome  Outcome
	ExitCode int
	Output   string // saída do run (limpa de controles e limitada); é o relatório de um diagnóstico
	Error    string
	Detail   string // dry-run: o que seria feito
}

// Report é o resultado de Run, ResumePending.
type Report struct {
	RecipeID string
	DryRun   bool
	Resumed  bool
	Status   state.RecipeStatus
	Steps    []StepReport
	// RebootPending: a receita parou esperando o reboot (recusado ou em andamento).
	RebootPending bool
	// Rebooting: o Rebooter foi chamado.
	Rebooting bool
}

// Lookup acha a receita pelo id (a retomada recarrega a receita do binário em execução).
type Lookup func(id string) (*Recipe, bool)

// Engine executa receitas e é o único dono do ciclo state/resume/reboot.
type Engine struct {
	Runner     exec.Runner
	Store      state.Store
	Resume     resume.Registrar
	Installer  Installer
	Rebooter   Rebooter
	Prompter   Prompter // nil = nunca confirma (o reboot só acontece com --yes)
	Platform   platform.Info
	Recipes    Lookup
	Executable string // binário que a retomada executa
	LogPath    string
	Clock      func() time.Time
	NewRunID   func() string
	Log        *slog.Logger
	// Details: etapas com render mostram também a saída original (tecla d na TUI, --detalhes na CLI).
	Details bool
	// Getenv escolhe o idioma do resumo (nil = os.Getenv): TOOLKIT_LANG, LC_ALL, LC_MESSAGES, LANG.
	Getenv func(string) string
}

const maxOutput = 64 << 10

func (e *Engine) now() time.Time {
	if e.Clock != nil {
		return e.Clock()
	}
	return time.Now()
}

func (e *Engine) log() *slog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return slog.New(slog.DiscardHandler)
}

func (e *Engine) runID() string {
	if e.NewRunID != nil {
		return e.NewRunID()
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Run executa a receita do começo.
func (e *Engine) Run(ctx context.Context, r *Recipe, opt RunOptions) (Report, error) {
	rep := Report{RecipeID: r.ID, DryRun: opt.DryRun}
	if r.OS != e.Platform.OS {
		return rep, fmt.Errorf("%w: %s é de %s", ErrWrongOS, r.ID, osName(r.OS))
	}
	if opt.DryRun {
		return e.plan(ctx, r, rep)
	}
	if r.RequiresAdmin && e.Platform.OS == platform.Windows && !e.Platform.IsAdmin {
		return rep, ErrNeedsAdmin
	}
	ids := make([]string, len(r.Steps))
	for i, s := range r.Steps {
		ids[i] = s.ID
	}
	err := e.Store.Update(ctx, func(f *state.File) error {
		if f.Active != "" && f.Active != r.ID {
			return fmt.Errorf("%w: %q (termine ou use \"toolkit recipe reset %s\")", ErrBusy, f.Active, f.Active)
		}
		if old := f.Recipes[r.ID]; old != nil {
			switch old.Status {
			case state.RecipeRunning, state.RecipeAwaitingReboot:
				return ErrPending
			case state.RecipeFailed:
				return ErrFailedState
			case state.RecipeDone:
			}
		}
		f.Recipes[r.ID] = state.NewRecipeState(r.ID, r.Hash, e.runID(), e.Platform.User, ids,
			state.Options{Yes: opt.Yes, Verbose: opt.Verbose}, e.now())
		f.Active = r.ID
		return nil
	})
	if err != nil {
		return rep, err
	}
	e.log().Info("receita iniciada", "recipe", r.ID, "steps", len(r.Steps))
	_, err = e.runFrom(ctx, r, 0, opt, &rep)
	return rep, err
}

// plan é o dry-run: checks rodam (leitura, nunca elevados), o resto só é descrito.
func (e *Engine) plan(ctx context.Context, r *Recipe, rep Report) (Report, error) {
	for _, st := range r.Steps {
		sr := StepReport{ID: st.ID, Name: st.Name, Outcome: OutcomeWouldRun, Detail: describe(st)}
		if st.Check != nil {
			code, _, err := e.runAction(ctx, r, st, st.Check, true, true)
			if err == nil && st.Check.OKExit(code) {
				sr.Outcome = OutcomeWouldSkip
				sr.Detail = "check passou: etapa seria pulada"
			}
		}
		rep.Steps = append(rep.Steps, sr)
	}
	return rep, nil
}

func describe(st Step) string {
	var d string
	switch {
	case len(st.Install) > 0:
		d = "instalaria: " + strings.Join(st.Install, ", ")
	case st.Run != nil:
		d = "executaria o script da etapa"
	}
	if st.Reboot != RebootNever {
		d += " (reboot: " + string(st.Reboot) + ")"
	}
	return d
}

// mutate aplica fn ao andamento da receita e grava.
func (e *Engine) mutate(ctx context.Context, id string, fn func(*state.RecipeState)) error {
	return e.Store.Update(ctx, func(f *state.File) error {
		rs := f.Recipes[id]
		if rs == nil {
			return fmt.Errorf("%w: %s", ErrUnknownRecipe, id)
		}
		fn(rs)
		rs.UpdatedAt = e.now()
		return nil
	})
}

func (e *Engine) mutateStep(ctx context.Context, id, stepID string, fn func(*state.StepState)) error {
	return e.mutate(ctx, id, func(rs *state.RecipeState) {
		if s := rs.Step(stepID); s != nil {
			fn(s)
		}
	})
}

func (e *Engine) finishStep(ctx context.Context, r *Recipe, st Step, status state.StepStatus, code int, msg string) error {
	t := e.now()
	return e.mutate(ctx, r.ID, func(rs *state.RecipeState) {
		if s := rs.Step(st.ID); s != nil {
			s.Status, s.ExitCode, s.Error, s.FinishedAt = status, code, msg, &t
		}
		if status == state.StepFailed {
			rs.Status = state.RecipeFailed
		}
	})
}

// failRecipe grava a falha e libera o Active.
func (e *Engine) failRecipe(ctx context.Context, r *Recipe, st Step, code int, msg string) error {
	e.log().Error("etapa falhou", "recipe", r.ID, "step", st.ID, "exit", code, "error", msg)
	t := e.now()
	return e.Store.Update(ctx, func(f *state.File) error {
		rs := f.Recipes[r.ID]
		if rs == nil {
			return nil
		}
		if s := rs.Step(st.ID); s != nil {
			s.Status, s.ExitCode, s.Error, s.FinishedAt = state.StepFailed, code, msg, &t
		}
		rs.Status, rs.UpdatedAt = state.RecipeFailed, t
		if f.Active == r.ID {
			f.Active = ""
		}
		return nil
	})
}

func (e *Engine) elevate() bool {
	return e.Platform.OS != platform.Windows && !e.Platform.IsAdmin
}

// runAction roda uma ação (check ou run). noElevate: dry-run nunca eleva.
func (e *Engine) runAction(ctx context.Context, r *Recipe, st Step, a *Action, readOnly, dry bool) (int, string, error) {
	elevate := r.RequiresAdmin && e.elevate() && !dry
	cmd, err := BuildCmd(a, e.Platform.OS, st.Timeout, elevate, readOnly)
	if err != nil {
		return 0, "", err
	}
	res, err := e.Runner.Run(ctx, cmd)
	if err != nil {
		return 0, "", err
	}
	out := string(res.Stdout)
	if res.ExitCode != 0 && len(res.Stderr) > 0 {
		out += "\n" + string(res.Stderr)
	}
	return res.ExitCode, clean(out), nil
}

// render resume a saída crua da etapa (campo render): o resumo no idioma do usuário e, no modo detalhes, a saída
// original logo depois. Se o resumo não puder ser feito, mostra a original com um aviso: o diagnóstico nunca some.
func (e *Engine) render(st Step, raw string) string {
	get := e.Getenv
	if get == nil {
		get = os.Getenv
	}
	l := diagfmt.LangFromEnv(get)
	summary, err := diagfmt.Render(st.Render, raw, l)
	if err != nil || strings.TrimSpace(summary) == "" {
		return clean(diagfmt.T(l, "(não deu para resumir esta saída; mostrando a original)", "(could not summarize this output; showing the original)", "(no se pudo resumir esta salida; se muestra la original)") + "\n" + raw)
	}
	summary = clean(summary)
	if !e.Details {
		return summary
	}
	return summary + "\n" + diagfmt.T(l, "-- Detalhes (saída original) --", "-- Details (original output) --", "-- Detalles (salida original) --") + "\n" + raw
}

// clean tira controles (ESC etc.), deixa \n e \t e limita o tamanho: a saída vai para o terminal e o log.
func clean(s string) string {
	if len(s) > maxOutput {
		s = s[:maxOutput] + "\n[saída cortada]"
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r == '\r' {
			return -1
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) || r == unicode.ReplacementChar {
			return -1
		}
		return r
	}, s)
}

// runFrom executa as etapas a partir de start. keep=true quando a retomada deve continuar registrada
// (uma etapa pediu reboot).
func (e *Engine) runFrom(ctx context.Context, r *Recipe, start int, opt RunOptions, rep *Report) (keep bool, err error) {
	for i := start; i < len(r.Steps); i++ {
		st := r.Steps[i]
		sr, reboot, err := e.execStep(ctx, r, st)
		rep.Steps = append(rep.Steps, sr)
		if err != nil {
			rep.Status = state.RecipeFailed
			e.notReached(r, i+1, rep)
			return false, err
		}
		if reboot {
			rep.Steps[len(rep.Steps)-1].Outcome = OutcomeAwaitingReboot
			rep.Status = state.RecipeAwaitingReboot
			rep.RebootPending = true
			e.notReached(r, i+1, rep)
			return true, e.requestReboot(ctx, r, st, opt, rep)
		}
	}
	rep.Status = state.RecipeDone
	err = e.Store.Update(ctx, func(f *state.File) error {
		if rs := f.Recipes[r.ID]; rs != nil {
			rs.Status, rs.UpdatedAt = state.RecipeDone, e.now()
		}
		if f.Active == r.ID {
			f.Active = ""
		}
		return nil
	})
	e.log().Info("receita concluída", "recipe", r.ID)
	return false, err
}

func (e *Engine) notReached(r *Recipe, from int, rep *Report) {
	for _, st := range r.Steps[from:] {
		rep.Steps = append(rep.Steps, StepReport{ID: st.ID, Name: st.Name, Outcome: OutcomeNotReached})
	}
}

// execStep executa UMA etapa. reboot=true quando ela pediu reinício (já gravada como awaiting-reboot
// só depois, em requestReboot). Erro = a etapa falhou (o state já foi gravado como failed).
func (e *Engine) execStep(ctx context.Context, r *Recipe, st Step) (StepReport, bool, error) {
	sr := StepReport{ID: st.ID, Name: st.Name}
	t := e.now()
	if err := e.mutateStep(ctx, r.ID, st.ID, func(s *state.StepState) {
		s.Status, s.Attempts, s.StartedAt, s.FinishedAt, s.Error, s.ExitCode = state.StepRunning, s.Attempts+1, &t, nil, "", 0
	}); err != nil {
		sr.Outcome, sr.Error = OutcomeFailed, err.Error()
		return sr, false, err
	}
	fail := func(code int, format string, a ...any) (StepReport, bool, error) {
		msg := fmt.Sprintf(format, a...)
		sr.Outcome, sr.ExitCode, sr.Error = OutcomeFailed, code, msg
		if ferr := e.failRecipe(ctx, r, st, code, msg); ferr != nil {
			return sr, false, errors.Join(fmt.Errorf("etapa %q: %s", st.ID, msg), ferr)
		}
		return sr, false, fmt.Errorf("etapa %q: %s", st.ID, msg)
	}

	if st.Check != nil {
		code, _, err := e.runAction(ctx, r, st, st.Check, true, false)
		switch {
		case err != nil:
			return fail(0, "check não executou: %v", err)
		case st.Check.OKExit(code):
			sr.Outcome = OutcomeSkipped
			if err := e.finishStep(ctx, r, st, state.StepSkipped, 0, ""); err != nil {
				return sr, false, err
			}
			return sr, false, nil
		}
	}

	rebootNeeded := st.Reboot == RebootRequired
	switch {
	case len(st.Install) > 0:
		if e.Installer == nil {
			return fail(0, "esta execução não tem como instalar apps")
		}
		out, err := e.Installer.Install(ctx, st.Install)
		if err != nil {
			return fail(0, "instalação falhou: %v", err)
		}
		rebootNeeded = rebootNeeded || (st.Reboot == RebootIfNeeded && out.RebootRequired)
	case st.Run != nil:
		code, out, err := e.runAction(ctx, r, st, st.Run, false, false)
		sr.Output = out
		if err != nil {
			return fail(0, "não executou: %v", err)
		}
		sr.ExitCode = code
		if !st.Run.OKExit(code) {
			return fail(code, "saiu com código %d", code)
		}
		if st.Render != "" {
			sr.Output = e.render(st, out)
		}
		rebootNeeded = rebootNeeded || (st.Reboot == RebootIfNeeded && code == 3010)
	}

	sr.Outcome = OutcomeDone
	if rebootNeeded {
		// O status final (awaiting-reboot) é gravado por requestReboot, junto do BootID.
		return sr, true, nil
	}
	if err := e.finishStep(ctx, r, st, state.StepDone, sr.ExitCode, ""); err != nil {
		return sr, false, err
	}
	return sr, false, nil
}

// requestReboot: salva awaiting-reboot, registra a retomada, confirma e reinicia, nesta ordem. Nunca há
// reboot sem a retomada registrada.
func (e *Engine) requestReboot(ctx context.Context, r *Recipe, st Step, opt RunOptions, rep *Report) error {
	t := e.now()
	if err := e.mutate(ctx, r.ID, func(rs *state.RecipeState) {
		if s := rs.Step(st.ID); s != nil {
			s.Status, s.BootID, s.FinishedAt = state.StepAwaitingReboot, e.Platform.BootID, &t
		}
		rs.Status = state.RecipeAwaitingReboot
	}); err != nil {
		return err
	}
	args := []string{"resume"}
	if opt.Yes {
		args = append(args, "--yes")
	}
	if opt.Verbose {
		args = append(args, "--verbose")
	}
	if err := e.Resume.Register(ctx, resume.Spec{
		Executable: e.Executable, Args: args, User: e.Platform.User, Elevated: r.RequiresAdmin, LogPath: e.LogPath,
	}); err != nil {
		return fmt.Errorf("não consegui registrar a retomada (não reiniciei): %w", err)
	}
	ok := opt.Yes
	if !ok && e.Prompter != nil {
		var err error
		if ok, err = e.Prompter.Confirm(ctx, fmt.Sprintf("A etapa %q pede reiniciar o computador. Reiniciar agora?", st.ID)); err != nil {
			return err
		}
	}
	if !ok {
		return ErrRebootDeclined
	}
	e.log().Info("reiniciando", "recipe", r.ID, "step", st.ID)
	rep.Rebooting = true
	if e.Rebooter == nil {
		return errors.New("esta execução não tem como reiniciar")
	}
	return e.Rebooter.Reboot(ctx)
}
