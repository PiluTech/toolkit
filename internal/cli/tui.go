package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/buildinfo"
	"github.com/PiluTech/toolkit/internal/providers"
	"github.com/PiluTech/toolkit/internal/recipes"
	"github.com/PiluTech/toolkit/internal/tui"
)

// shouldOpenTUI decide se `toolkit` sem argumentos abre a TUI: só com terminal de verdade (entrada E saída),
// sem --no-tui e sem TERM=dumb (ou vazio). Em qualquer outro caso o chamador mostra a ajuda.
func shouldOpenTUI(stdinTTY, stdoutTTY bool, noTUI bool, term string) bool {
	if noTUI || !stdinTTY || !stdoutTTY {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(term))
	return t != "" && t != "dumb"
}

func stdoutIsTerminal() bool { return fileIsTerminal(os.Stdout) }

// fileIsTerminal diz se f é um terminal de verdade (isatty/console do SO). Só "dispositivo de caractere" NÃO
// basta: /dev/null também é, e a TUI abriria com a saída descartada.
func fileIsTerminal(f *os.File) bool { return term.IsTerminal(f.Fd()) }

// TUIDeps são os pontos injetáveis da abertura da TUI (os testes trocam Run e os terminais).
type TUIDeps struct {
	StdoutIsTerminal func() bool
	Term             func() string
	// Run abre a TUI (nil = tui.Run).
	Run func(ctx context.Context, o tui.Options, tabs ...tui.Tab) error
}

func (d TUIDeps) stdoutTTY() bool {
	if d.StdoutIsTerminal != nil {
		return d.StdoutIsTerminal()
	}
	return stdoutIsTerminal()
}

func (d TUIDeps) term() string {
	if d.Term != nil {
		return d.Term()
	}
	return os.Getenv("TERM")
}

func (d TUIDeps) run(ctx context.Context, o tui.Options, tabs ...tui.Tab) error {
	if d.Run != nil {
		return d.Run(ctx, o, tabs...)
	}
	return tui.Run(ctx, o, tabs...)
}

// runRoot é o `toolkit` sem argumentos: abre a TUI quando há terminal, senão mostra a ajuda.
func runRoot(cmd *cobra.Command, deps Deps, rd RecipeDeps, noTUI bool, notifier *updateNotifier) error {
	if !shouldOpenTUI(deps.interactive(), deps.TUI.stdoutTTY(), noTUI, deps.TUI.term()) {
		return cmd.Help()
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	th := tui.Theme{Mono: os.Getenv("NO_COLOR") != "", Dark: true}
	opts := tui.Options{
		Version: buildinfo.Version, Mono: th.Mono,
		Planner: &tuiPlanner{deps: deps, rd: rd},
		Runner:  &tuiRunner{deps: deps, rd: rd, logDir: func() string { return runLogDir(deps, rd) }},
	}
	// aviso de versão nova: o cache já traz a faixa; uma consulta em andamento a preenche quando chegar
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if rel, ok := notifier.latest(); ok {
		opts.Latest, opts.LatestNotes, opts.LatestURL = rel.Tag, rel.Notes, rel.URL
	} else if notifier != nil && notifier.result != nil {
		ch := make(chan tui.UpdateInfo, 1)
		opts.Updates = ch
		go func() { // termina com o ctx (fim da TUI): nada sobra
			select {
			case rel := <-notifier.result:
				ch <- tui.UpdateInfo{Tag: rel.Tag, Notes: rel.Notes, URL: rel.URL}
			case <-ctx.Done():
			}
		}()
	}
	if notifier != nil {
		notifier.silence = true // a faixa da TUI já avisou: sem linha no fim
	}
	tabs := []tui.Tab{
		tui.NewAppsTab(ctx, &appsBackend{deps: deps}, th, deps.GOOS),
		tui.NewRecipesTab(ctx, &recipesBackend{deps: deps, rd: rd}, th, false),
		tui.NewRecipesTab(ctx, &recipesBackend{deps: deps, rd: rd}, th, true),
		tui.NewLogTab(th, ""),
	}
	err := deps.TUI.run(ctx, opts, tabs...)
	if errors.Is(err, tui.ErrUpdateRequested) {
		// o usuário escolheu Atualizar: a TUI já fechou; roda o mesmo `toolkit update` do terminal
		return runUpdate(ctx, cmd.OutOrStdout(), deps, updateFlags{})
	}
	return err
}

// appsBackend liga a aba Apps ao catálogo e aos providers (somente leitura).
type appsBackend struct {
	deps Deps
}

func (b *appsBackend) registry() *providers.Registry {
	return providers.NewRegistry(b.deps.providerList(b.deps.runner())...)
}

func (b *appsBackend) Load(ctx context.Context) ([]tui.AppInfo, error) {
	cat, err := loadCatalog(ctx, b.deps, listFlags{}, io.Discard)
	if err != nil {
		return nil, err
	}
	var out []tui.AppInfo
	for _, a := range cat.List() {
		info := tui.AppInfo{ID: a.ID, Name: a.Name, Category: a.Category, Versions: a.Versions.Enabled()}
		for _, in := range a.InstallersFor(b.deps.GOOS) {
			info.Managers = append(info.Managers, in.Provider)
		}
		info.NoInstaller = len(info.Managers) == 0
		out = append(out, info)
	}
	return out, nil
}

// candidate acha o primeiro gerenciador disponível para o app (mesma regra do install).
func (b *appsBackend) candidate(ctx context.Context, id string) (providers.Candidate, error) {
	cat, err := loadCatalog(ctx, b.deps, listFlags{}, io.Discard)
	if err != nil {
		return providers.Candidate{}, err
	}
	app, ok := cat.Get(id)
	if !ok {
		return providers.Candidate{}, fmt.Errorf("app %q fora do catálogo", id)
	}
	vars, _, err := resolveVersion(app, "")
	if err != nil {
		vars = map[string]string{}
	}
	cands, skips := b.registry().Candidates(ctx, app, b.deps.GOOS, vars)
	if len(cands) == 0 {
		return providers.Candidate{}, providers.NoInstallerError(app, b.deps.GOOS, skips)
	}
	return cands[0], nil
}

func (b *appsBackend) Status(ctx context.Context, id string) tui.AppStatus {
	cat, err := loadCatalog(ctx, b.deps, listFlags{}, io.Discard)
	if err != nil {
		return tui.AppStatus{Err: err}
	}
	app, ok := cat.Get(id)
	if !ok {
		return tui.AppStatus{Err: fmt.Errorf("app %q fora do catálogo", id)}
	}
	rn := b.deps.runner()
	st, err := readState(ctx, b.deps, rn, providers.NewRegistry(b.deps.providerList(rn)...), app)
	if err != nil {
		return tui.AppStatus{Err: err}
	}
	out := tui.AppStatus{Installed: st.Installed, Version: st.Version}
	if st.External != nil {
		out.External, out.ExternalManager, out.ExternalWhere = true, st.External.Manager, st.External.Where
		out.Version = st.External.Version
	}
	return out
}

// Versions devolve o que a aba oferece: a lista ESTÁTICA do catálogo (versions: [...], a primeira é a
// padrão) quando existe, sem consultar o gerenciador; só versions: true consulta o provider; app sem versões
// não oferece nada.
func (b *appsBackend) Versions(ctx context.Context, id string) ([]string, error) {
	cat, err := loadCatalog(ctx, b.deps, listFlags{}, io.Discard)
	if err != nil {
		return nil, err
	}
	app, ok := cat.Get(id)
	if !ok {
		return nil, fmt.Errorf("app %q fora do catálogo", id)
	}
	if len(app.Versions.List) > 0 {
		return slices.Clone(app.Versions.List), nil
	}
	if !app.Versions.Query {
		return nil, nil
	}
	c, err := b.candidate(ctx, id)
	if err != nil {
		return nil, err
	}
	vs, err := c.Provider.ListVersions(ctx, c.Ref)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if v != "latest" {
			out = append(out, v)
		}
	}
	return out, nil
}

// recipesBackend lê as receitas do SO e o state (só leitura).
type recipesBackend struct {
	deps Deps
	rd   RecipeDeps
}

func (b *recipesBackend) Load(ctx context.Context) (tui.RecipesData, error) {
	rt, err := setupRecipes(ctx, b.deps, b.rd)
	if err != nil {
		return tui.RecipesData{}, err
	}
	data := tui.RecipesData{Runs: map[string]tui.RecipeRun{}, StatePath: rt.store.Path()}
	for _, r := range rt.set.For(rt.info.OS) {
		data.Recipes = append(data.Recipes, recipeInfo(r))
	}
	f, err := rt.store.Load(ctx)
	if err != nil {
		data.StateErr = err // as receitas continuam; só o andamento não veio
		return data, nil
	}
	for id, rs := range f.Recipes {
		run := tui.RecipeRun{Status: string(rs.Status), Steps: map[string]tui.StepRun{}}
		for _, s := range rs.Steps {
			run.Steps[s.ID] = tui.StepRun{Status: string(s.Status), Error: s.Error}
		}
		data.Runs[id] = run
	}
	return data, nil
}

func recipeInfo(r *recipes.Recipe) tui.RecipeInfo {
	info := tui.RecipeInfo{ID: r.ID, Name: r.Name, Description: r.Description, Diagnostic: r.Kind == recipes.KindDiagnostic, RequiresAdmin: r.RequiresAdmin}
	for _, s := range r.Steps {
		info.Steps = append(info.Steps, tui.StepInfo{ID: s.ID, Name: s.Name, Reboot: string(s.Reboot)})
	}
	return info
}

// runLogDir é a pasta dos logs de execução (ao lado do estado); vazio se não der para resolver.
func runLogDir(deps Deps, rd RecipeDeps) string {
	rt, err := setupRecipes(context.Background(), deps, rd)
	if err != nil {
		return ""
	}
	return rt.paths.LogDir
}
