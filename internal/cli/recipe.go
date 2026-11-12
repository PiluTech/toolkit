package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	toolkit "github.com/PiluVitu/toolkit"
	"github.com/PiluVitu/toolkit/internal/buildinfo"
	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/msg"
	"github.com/PiluVitu/toolkit/internal/platform"
	"github.com/PiluVitu/toolkit/internal/providers"
	"github.com/PiluVitu/toolkit/internal/recipes"
	"github.com/PiluVitu/toolkit/internal/resume"
	"github.com/PiluVitu/toolkit/internal/state"
)

// RecipeDeps são as dependências dos comandos recipe e resume (injetáveis nos testes; zero values usam o
// sistema real). O Runner vem do Deps (NewRunner).
type RecipeDeps struct {
	// Detect descobre a máquina (nil = platform.SystemDetector sobre o Runner, com a elevação no Windows
	// perguntada ao PowerShell).
	Detect func(ctx context.Context, r exec.Runner) (platform.Info, error)
	Getenv func(string) string
	// Paths resolve os diretórios de estado, log e binário (nil = platform.ResolvePaths).
	Paths        func(platform.Info) (platform.Paths, error)
	Home         func() (string, error)
	Executable   func() (string, error)
	UID          func() string
	NewRegistrar func(resume.Config) (resume.Registrar, error)
	// RecipesFS é a raiz com recipes/<so>/<id>.yaml (nil = as receitas embutidas).
	RecipesFS fs.FS
	Now       func() time.Time
}

func (d RecipeDeps) detect(ctx context.Context, r exec.Runner) (platform.Info, error) {
	if d.Detect != nil {
		return d.Detect(ctx, r)
	}
	return platform.SystemDetector{
		Runner:     r,
		IsElevated: func() bool { return platform.IsElevatedViaPowerShell(ctx, r) },
	}.Detect(ctx)
}

func (d RecipeDeps) getenv(k string) string {
	if d.Getenv != nil {
		return d.Getenv(k)
	}
	return os.Getenv(k)
}

func (d RecipeDeps) home() string {
	f := d.Home
	if f == nil {
		f = os.UserHomeDir
	}
	h, _ := f()
	return h
}

func (d RecipeDeps) executable() (string, error) {
	if d.Executable != nil {
		return d.Executable()
	}
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

func (d RecipeDeps) uid() string {
	if d.UID != nil {
		return d.UID()
	}
	return strconv.Itoa(os.Getuid())
}

func (d RecipeDeps) registrar(c resume.Config) (resume.Registrar, error) {
	if d.NewRegistrar != nil {
		return d.NewRegistrar(c)
	}
	return resume.New(c)
}

func (d RecipeDeps) recipesFS() fs.FS {
	if d.RecipesFS != nil {
		return d.RecipesFS
	}
	return toolkit.Recipes
}

func (d RecipeDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

type recipeFlags struct {
	yes, dryRun, verbose, noTUI, json bool
	os                                string
}

func newRecipeCmds(deps Deps) []*cobra.Command {
	return newRecipeCmdsWith(deps, RecipeDeps{})
}

func newRecipeCmdsWith(deps Deps, rd RecipeDeps) []*cobra.Command {
	recipe := &cobra.Command{Use: "recipe", Short: msg.RecipeShort}

	var lf recipeFlags
	list := &cobra.Command{
		Use: "list", Short: msg.RecipeListShort, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRecipeList(cmd.Context(), cmd.OutOrStdout(), deps, rd, lf)
		},
	}
	list.Flags().StringVar(&lf.os, "os", "", msg.FlagRecipeOS)
	list.Flags().BoolVar(&lf.json, "json", false, msg.FlagRecipeJSON)

	var rf recipeFlags
	run := &cobra.Command{
		Use: "run <id>", Short: msg.RecipeRunShort, Long: msg.RecipeRunLong, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecipeRun(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, rd, rf, args[0])
		},
	}
	run.Flags().BoolVarP(&rf.yes, "yes", "y", false, msg.FlagRecipeYes)
	run.Flags().BoolVar(&rf.dryRun, "dry-run", false, msg.FlagRecipeDryRun)
	run.Flags().BoolVar(&rf.verbose, "verbose", false, msg.FlagVerbose)
	run.Flags().BoolVar(&rf.noTUI, "no-tui", false, msg.FlagNoTUI)

	var sf recipeFlags
	status := &cobra.Command{
		Use: "status", Short: msg.RecipeStatusShort, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRecipeStatus(cmd.Context(), cmd.OutOrStdout(), deps, rd, sf)
		},
	}
	status.Flags().BoolVar(&sf.json, "json", false, msg.FlagRecipeJSON)

	reset := &cobra.Command{
		Use: "reset <id>", Short: msg.RecipeResetShort, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecipeReset(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, rd, args[0])
		},
	}
	recipe.AddCommand(list, run, status, reset)

	var uf recipeFlags
	resumeCmd := &cobra.Command{
		Use: "resume", Short: msg.ResumeShort, Long: msg.ResumeLong, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runResume(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, rd, uf)
		},
	}
	resumeCmd.Flags().BoolVarP(&uf.yes, "yes", "y", false, msg.FlagRecipeYes)
	resumeCmd.Flags().BoolVar(&uf.verbose, "verbose", false, msg.FlagVerbose)
	resumeCmd.Flags().BoolVar(&uf.noTUI, "no-tui", false, msg.FlagNoTUI)
	return []*cobra.Command{recipe, resumeCmd}
}

// recipeRuntime é o que os comandos precisam depois de detectar a máquina.
type recipeRuntime struct {
	info   platform.Info
	paths  platform.Paths
	set    *recipes.Set
	store  *state.FileStore
	runner exec.Runner
}

func setupRecipes(ctx context.Context, deps Deps, rd RecipeDeps) (*recipeRuntime, error) {
	runner := deps.runner()
	info, err := rd.detect(ctx, runner)
	if err != nil {
		return nil, err
	}
	resolve := rd.Paths
	if resolve == nil {
		resolve = func(i platform.Info) (platform.Paths, error) {
			return platform.ResolvePaths(i, platform.Env{Getenv: rd.getenv, Home: rd.home()})
		}
	}
	paths, err := resolve(info)
	if err != nil {
		return nil, err
	}
	set, err := recipes.LoadFS(rd.recipesFS(), "recipes")
	if err != nil {
		return nil, err
	}
	return &recipeRuntime{
		info: info, paths: paths, set: set, runner: runner,
		store: &state.FileStore{Dir: paths.StateDir, ToolkitVersion: buildinfo.Version},
	}, nil
}

func runRecipeList(ctx context.Context, out io.Writer, deps Deps, rd RecipeDeps, f recipeFlags) error {
	rt, err := setupRecipes(ctx, deps, rd)
	if err != nil {
		return err
	}
	target := rt.info.OS
	switch f.os {
	case "":
	case "windows":
		target = platform.Windows
	case "linux":
		target = platform.Linux
	case "macos", "darwin":
		target = platform.Darwin
	default:
		return fmt.Errorf(msg.ErrRecipeOSUnknown, printable(f.os))
	}
	rs := rt.set.For(target)
	if f.json {
		type item struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Description   string `json:"description,omitempty"`
			Kind          string `json:"kind"`
			RequiresAdmin bool   `json:"requires_admin"`
			Steps         int    `json:"steps"`
		}
		items := make([]item, 0, len(rs))
		for _, r := range rs {
			items = append(items, item{r.ID, r.Name, r.Description, string(r.Kind), r.RequiresAdmin, len(r.Steps)})
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}
	if len(rs) == 0 {
		_, _ = fmt.Fprintln(out, msg.RecipeListEmpty)
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", msg.RecipeHeaderID, msg.RecipeHeaderName, msg.RecipeHeaderKind, msg.RecipeHeaderAdmin, msg.RecipeHeaderSteps)
	for _, r := range rs {
		admin := msg.RecipeNo
		if r.RequiresAdmin {
			admin = msg.RecipeYes
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n", printable(r.ID), printable(r.Name), r.Kind, admin, len(r.Steps))
	}
	return tw.Flush()
}

// hasReboot diz se alguma etapa pode reiniciar a máquina.
func hasReboot(r *recipes.Recipe) bool {
	for _, s := range r.Steps {
		if s.Reboot != recipes.RebootNever {
			return true
		}
	}
	return false
}

func runRecipeRun(ctx context.Context, out, errOut io.Writer, deps Deps, rd RecipeDeps, f recipeFlags, id string) error {
	rt, err := setupRecipes(ctx, deps, rd)
	if err != nil {
		return err
	}
	r, ok := rt.set.Get(rt.info.OS, id)
	if !ok {
		return fmt.Errorf(msg.ErrRecipeUnknown, printable(id))
	}
	if !f.dryRun {
		// A retomada de Linux roda como root e lê /var/lib/toolkit: o estado tem de estar lá.
		if rt.info.OS == platform.Linux && hasReboot(r) && !rt.info.IsAdmin {
			return fmt.Errorf(msg.ErrRecipeNeedsSudo, r.ID)
		}
		if rt.info.OS == platform.Windows && r.RequiresAdmin && !rt.info.IsAdmin {
			return fmt.Errorf(msg.ErrRecipeNeedsAdminWin, r.ID)
		}
	}
	eng, closeLog, err := buildEngine(rt, deps, rd, f, r.ID, out, errOut)
	if err != nil {
		return err
	}
	defer closeLog()
	if f.dryRun {
		_, _ = fmt.Fprintf(out, msg.RecipeDryHeader+"\n", printable(r.ID))
	} else {
		_, _ = fmt.Fprintf(out, msg.RecipeRunHeader+"\n", printable(r.ID), printable(r.Name))
	}
	rep, err := eng.Run(ctx, r, recipes.RunOptions{Yes: f.yes, DryRun: f.dryRun, Verbose: f.verbose})
	return finishReport(out, rep, err)
}

func runResume(ctx context.Context, out, errOut io.Writer, deps Deps, rd RecipeDeps, f recipeFlags) error {
	rt, err := setupRecipes(ctx, deps, rd)
	if err != nil {
		return err
	}
	eng, closeLog, err := buildEngine(rt, deps, rd, f, "resume", out, errOut)
	if err != nil {
		return err
	}
	defer closeLog()
	rep, err := eng.ResumePending(ctx)
	if errors.Is(err, recipes.ErrNothingResume) {
		_, _ = fmt.Fprintln(out, msg.ResumeNothing) // sucesso: uma retomada registrada à toa não pode falhar a cada boot
		return nil
	}
	return finishReport(out, rep, err)
}

func runRecipeReset(ctx context.Context, out, errOut io.Writer, deps Deps, rd RecipeDeps, id string) error {
	rt, err := setupRecipes(ctx, deps, rd)
	if err != nil {
		return err
	}
	eng, closeLog, err := buildEngine(rt, deps, rd, recipeFlags{}, "reset", out, errOut)
	if err != nil {
		return err
	}
	defer closeLog()
	if err := eng.Reset(ctx, id); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, msg.RecipeResetDone+"\n", printable(id))
	return nil
}

func runRecipeStatus(ctx context.Context, out io.Writer, deps Deps, rd RecipeDeps, f recipeFlags) error {
	rt, err := setupRecipes(ctx, deps, rd)
	if err != nil {
		return err
	}
	file, err := rt.store.Load(ctx)
	if err != nil {
		return err
	}
	if f.json {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(file)
	}
	if len(file.Recipes) == 0 {
		_, _ = fmt.Fprintln(out, msg.RecipeStatusEmpty)
		return nil
	}
	ids := make([]string, 0, len(file.Recipes))
	for id := range file.Recipes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rs := file.Recipes[id]
		marker := ""
		if file.Active == id {
			marker = " *"
		}
		_, _ = fmt.Fprintf(out, msg.RecipeStatusLine+"\n", printable(id)+marker, rs.Status, rs.StartedAt.Format(time.RFC3339))
		for _, s := range rs.Steps {
			line := string(s.Status)
			if s.Error != "" {
				line += " (" + s.Error + ")"
			}
			_, _ = fmt.Fprintf(out, msg.RecipeStatusStep+"\n", printable(s.ID), printable(line))
		}
	}
	return nil
}

var outcomeLabel = map[recipes.Outcome]string{
	recipes.OutcomeDone: "ok", recipes.OutcomeSkipped: "pulada", recipes.OutcomeFailed: "falhou",
	recipes.OutcomeAwaitingReboot: "reiniciar", recipes.OutcomeWouldRun: "faria", recipes.OutcomeWouldSkip: "pularia",
	recipes.OutcomeNotReached: "não executada",
}

// finishReport imprime o relatório e traduz o erro: reinício recusado não é falha (a retomada continua
// registrada), o resto é.
func finishReport(out io.Writer, rep recipes.Report, err error) error {
	for _, s := range rep.Steps {
		name := s.ID
		if s.Name != "" {
			name += " (" + s.Name + ")"
		}
		_, _ = fmt.Fprintf(out, msg.RecipeStepLine+"\n", outcomeLabel[s.Outcome], printable(name))
		if s.Detail != "" {
			_, _ = fmt.Fprintf(out, msg.RecipeStepDetail+"\n", printable(s.Detail))
		}
		for _, line := range strings.Split(strings.TrimRight(s.Output, "\n"), "\n") {
			if line != "" {
				_, _ = fmt.Fprintf(out, msg.RecipeStepOutput+"\n", printable(line))
			}
		}
		if s.Error != "" {
			_, _ = fmt.Fprintf(out, msg.RecipeStepError+"\n", printable(s.Error))
		}
	}
	switch {
	case errors.Is(err, recipes.ErrRebootDeclined):
		_, _ = fmt.Fprintln(out, msg.RecipePending)
		return nil
	case err != nil:
		return err
	case rep.Rebooting:
		_, _ = fmt.Fprintln(out, msg.RecipeRebooting)
	case rep.Status == state.RecipeDone && !rep.DryRun:
		_, _ = fmt.Fprintln(out, msg.RecipeDone)
	}
	return nil
}

// buildEngine monta a Engine sobre o runtime. Em --dry-run o Runner é o DryRunRunner (só leituras
// executam), nenhum log é criado e nada toca state, retomada ou reboot.
func buildEngine(rt *recipeRuntime, deps Deps, rd RecipeDeps, f recipeFlags, logName string, out, errOut io.Writer) (*recipes.Engine, func(), error) {
	runner := rt.runner
	if f.dryRun {
		dr := exec.NewDryRunRunner(rt.runner)
		dr.CheckElevation = deps.checkElevation()
		runner = dr
	}
	exe, err := rd.executable()
	if err != nil {
		return nil, nil, fmt.Errorf("não achei o caminho do binário: %w", err)
	}
	reg, err := rd.registrar(resume.Config{
		Info: rt.info, Paths: rt.paths, Runner: runner, StatePath: rt.store.Path(), Home: rd.home(), UID: rd.uid(),
	})
	if err != nil {
		return nil, nil, err
	}
	logger, logPath, closeLog := openRunLog(rt, rd, f, logName, errOut)

	idepsRunner := deps
	idepsRunner.NewRunner = func() exec.Runner { return runner }
	eng := &recipes.Engine{
		Runner:     runner,
		Store:      rt.store,
		Resume:     reg,
		Installer:  catalogInstaller{deps: idepsRunner, out: out, errOut: errOut, goos: string(rt.info.OS)},
		Rebooter:   recipes.SystemRebooter{Runner: runner, Info: rt.info},
		Platform:   rt.info,
		Recipes:    func(id string) (*recipes.Recipe, bool) { return rt.set.Get(rt.info.OS, id) },
		Executable: exe,
		LogPath:    logPath,
		Clock:      rd.now,
		Log:        logger,
	}
	if deps.interactive() {
		eng.Prompter = ttyPrompter{in: bufio.NewReader(deps.stdin()), out: out}
	}
	return eng, closeLog, nil
}

// openRunLog cria o log da execução ao lado do estado (<LogDir>/<nome>-<data>.log, 0600). Só vão para ele
// ids, códigos de saída e mensagens de erro: nunca a saída dos comandos, então não há segredo a redigir.
func openRunLog(rt *recipeRuntime, rd RecipeDeps, f recipeFlags, name string, errOut io.Writer) (*slog.Logger, string, func()) {
	noop := func() {}
	var w io.Writer
	path := ""
	closeFn := noop
	if !f.dryRun {
		if err := os.MkdirAll(rt.paths.LogDir, 0o700); err == nil {
			sep := "/"
			if rt.info.OS == platform.Windows {
				sep = `\`
			}
			p := rt.paths.LogDir + sep + name + "-" + rd.now().UTC().Format("20060102-150405") + ".log"
			if fh, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil { //nolint:gosec // LogDir vem do ResolvePaths
				w, path, closeFn = fh, p, func() { _ = fh.Close() }
			} else {
				_, _ = fmt.Fprintln(errOut, "aviso: não consegui criar o log: "+printable(err.Error()))
			}
		}
	}
	if f.verbose {
		if w != nil {
			w = io.MultiWriter(w, errOut)
		} else {
			w = errOut
		}
	}
	if w == nil {
		return slog.New(slog.DiscardHandler), path, closeFn
	}
	return slog.New(slog.NewTextHandler(w, nil)), path, closeFn
}

// ttyPrompter pergunta [s/N] no terminal.
type ttyPrompter struct {
	in  *bufio.Reader
	out io.Writer
}

func (p ttyPrompter) Confirm(_ context.Context, question string) (bool, error) {
	_, _ = fmt.Fprint(p.out, printable(question)+" [s/N] ")
	answer, _ := p.in.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "s", "sim", "y", "yes":
		return true, nil
	}
	return false, nil
}

// catalogInstaller é a ponte dos passos `install:` com o catálogo e os providers (os mesmos do toolkit install).
type catalogInstaller struct {
	deps   Deps
	out    io.Writer
	errOut io.Writer
	goos   string
}

func (c catalogInstaller) Install(ctx context.Context, apps []string) (recipes.InstallOutcome, error) {
	var outcome recipes.InstallOutcome
	cat, err := loadCatalog(c.deps, listFlags{}, c.errOut)
	if err != nil {
		return outcome, err
	}
	type target struct {
		app     catalog.App
		vars    map[string]string
		version string
	}
	var targets []target
	for _, ref := range apps { // valida tudo antes de instalar qualquer coisa
		id, requested, err := parseTarget(ref)
		if err != nil {
			return outcome, err
		}
		app, ok := cat.Get(id)
		if !ok {
			return outcome, fmt.Errorf(msg.ErrInstallUnknownApp, printable(id))
		}
		vars, version, err := resolveVersion(app, requested)
		if err != nil {
			return outcome, err
		}
		targets = append(targets, target{app, vars, version})
	}
	reg := providers.NewRegistry(c.deps.providerList(c.deps.runner())...)
	var failed []string
	for _, t := range targets {
		_, _ = fmt.Fprintf(c.out, msg.InstallHeader+"\n", printable(t.app.ID), printable(t.app.Name))
		cands, skips := reg.Candidates(ctx, t.app, c.goos, t.vars)
		if len(cands) == 0 {
			err = providers.NoInstallerError(t.app, c.goos, skips)
		} else {
			var res providers.Outcome
			if res, err = providers.InstallApp(ctx, cands, t.version); err == nil {
				if res.Result.AlreadyInstalled {
					_, _ = fmt.Fprintf(c.out, msg.InstallAlready+"\n", res.Provider, "")
				} else {
					_, _ = fmt.Fprintf(c.out, msg.InstallDone+"\n", res.Provider)
				}
				outcome.RebootRequired = outcome.RebootRequired || res.Result.RebootRequired
			}
		}
		if err != nil {
			_, _ = fmt.Fprintln(c.errOut, "erro: "+printable(err.Error()))
			failed = append(failed, t.app.ID)
		}
	}
	if len(failed) > 0 {
		return outcome, fmt.Errorf(msg.ErrInstallFailed, printable(strings.Join(failed, ", ")))
	}
	return outcome, nil
}
