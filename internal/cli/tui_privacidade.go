package cli

import (
	"context"
	"errors"
	"sync"

	"github.com/PiluTech/toolkit/internal/privacidade"
	"github.com/PiluTech/toolkit/internal/textsafe"
	"github.com/PiluTech/toolkit/internal/tui"
)

// newPrivacyTab monta a aba Privacidade da TUI sobre o mesmo núcleo do `toolkit privacidade` (mesmo System, mesmo
// Store, mesmas regras: plano antes, backup antes, nunca eleva sozinho). Fora do Windows (deps.GOOS) a aba só avisa.
// O orquestrador a coloca na lista de abas de openTUI.
func newPrivacyTab(ctx context.Context, deps Deps, th tui.Theme) tui.Tab {
	return tui.NewPrivacyTab(ctx, &privacyBackend{deps: deps}, th)
}

// privacyBackend implementa tui.PrivacyBackend. O ambiente (System e Store) é montado na primeira leitura e
// reaproveitado: no Windows isso consulta o PowerShell (SID do usuário), então não se repete a cada tecla.
type privacyBackend struct {
	deps Deps
	mu   sync.Mutex
	env  *privEnv
}

func (b *privacyBackend) getEnv(ctx context.Context) (privEnv, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.env != nil {
		return *b.env, nil
	}
	e, err := newPrivEnv(ctx, b.deps)
	if err != nil {
		return privEnv{}, err
	}
	b.env = &e
	return e, nil
}

func (b *privacyBackend) Load(ctx context.Context) (tui.PrivInfo, error) {
	if b.deps.GOOS != "windows" {
		return tui.PrivInfo{OS: printable(b.deps.GOOS)}, nil
	}
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.PrivInfo{}, err
	}
	plan, err := privacidade.Build(ctx, e.sys, privacidade.PerfilRecomendado, e.lang)
	if err != nil {
		return tui.PrivInfo{}, cleanErr(err)
	}
	groups := map[string][]privacidade.Change{}
	for _, c := range plan.Changes {
		groups[c.Setting] = append(groups[c.Setting], c)
	}
	info := tui.PrivInfo{Supported: true, OS: "windows", Edition: plan.Edition, IsAdmin: plan.IsAdmin}
	for _, s := range privacidade.Settings() {
		cs := groups[s.ID]
		pending := 0
		for _, c := range cs {
			if c.Pending {
				pending++
			}
		}
		state := tui.PrivPartial
		switch {
		case len(cs) == 0:
			state = tui.PrivAbsent
		case pending == 0:
			state = tui.PrivApplied
		case pending == len(cs):
			state = tui.PrivPending
		}
		info.Settings = append(info.Settings, tui.PrivSetting{Title: s.Title(e.lang), Admin: s.Admin, State: state})
	}
	return info, nil
}

// build calcula o plano do perfil (com ou sem os itens de administrador).
func (b *privacyBackend) build(ctx context.Context, p tui.PrivProfile, onlyUser bool) (*privacidade.Plan, privEnv, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return nil, e, err
	}
	var perfil privacidade.Perfil
	switch p {
	case tui.PrivBasico:
		perfil = privacidade.PerfilBasico
	case tui.PrivRecomendado:
		perfil = privacidade.PerfilRecomendado
	default:
		return nil, e, errors.New("perfil desconhecido: use basico ou recomendado")
	}
	plan, err := privacidade.Build(ctx, e.sys, perfil, e.lang)
	if err != nil {
		return nil, e, cleanErr(err)
	}
	if onlyUser {
		onlyUserItems(plan)
	}
	return plan, e, nil
}

func (b *privacyBackend) Plan(ctx context.Context, p tui.PrivProfile, onlyUser bool) (tui.PrivPlan, error) {
	plan, e, err := b.build(ctx, p, onlyUser)
	if err != nil {
		return tui.PrivPlan{}, err
	}
	text := plan.Format(e.lang)
	return tui.PrivPlan{Text: textsafe.Multiline(text), Pending: plan.Pending(), NeedsAdmin: plan.NeedsAdmin(), IsAdmin: e.sys.IsAdmin(ctx)}, nil
}

func (b *privacyBackend) Apply(ctx context.Context, p tui.PrivProfile, onlyUser bool) (tui.PrivResult, error) {
	plan, e, err := b.build(ctx, p, onlyUser)
	if err != nil {
		return tui.PrivResult{}, err
	}
	if plan.Pending() == 0 {
		return tui.PrivResult{}, nil
	}
	if plan.NeedsAdmin() && !e.sys.IsAdmin(ctx) { // recusa ANTES de mudar qualquer coisa
		return tui.PrivResult{}, tui.ErrPrivNeedsAdmin
	}
	res, err := privacidade.Apply(ctx, e.sys, e.store, plan, e.now())
	return privResult(res), tuiPrivErr(err)
}

func (b *privacyBackend) UndoPlan(ctx context.Context) (tui.PrivPlan, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.PrivPlan{}, err
	}
	plan, _, err := privacidade.BuildUndo(ctx, e.sys, e.store, e.lang)
	if err != nil {
		return tui.PrivPlan{}, tuiPrivErr(err)
	}
	return tui.PrivPlan{Text: textsafe.Multiline(formatUndo(plan)), Pending: plan.Pending(), NeedsAdmin: plan.NeedsAdmin(), IsAdmin: e.sys.IsAdmin(ctx)}, nil
}

func (b *privacyBackend) Undo(ctx context.Context) (tui.PrivResult, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.PrivResult{}, err
	}
	plan, bk, err := privacidade.BuildUndo(ctx, e.sys, e.store, e.lang)
	if err != nil {
		return tui.PrivResult{}, tuiPrivErr(err)
	}
	if plan.Pending() == 0 { // tudo já está como antes: só o backup é removido (como o comando)
		return tui.PrivResult{Nothing: true}, tuiPrivErr(e.store.Remove())
	}
	if plan.NeedsAdmin() && !e.sys.IsAdmin(ctx) {
		return tui.PrivResult{}, tui.ErrPrivNeedsAdmin
	}
	res, err := privacidade.ApplyUndo(ctx, e.sys, e.store, plan, bk)
	return privResult(res), tuiPrivErr(err)
}

func privResult(r *privacidade.Result) tui.PrivResult {
	if r == nil {
		return tui.PrivResult{}
	}
	return tui.PrivResult{Applied: len(r.Applied), BackupPath: r.BackupPath}
}

// tuiPrivErr traduz os erros do núcleo para os que a aba entende e limpa o texto dos demais.
func tuiPrivErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, privacidade.ErrNoBackup):
		return tui.ErrPrivNoBackup
	case errors.Is(err, privacidade.ErrNeedsAdmin):
		return tui.ErrPrivNeedsAdmin
	}
	return cleanErr(err)
}
