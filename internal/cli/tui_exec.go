package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/providers"
	"github.com/PiluTech/toolkit/internal/recipes"
	"github.com/PiluTech/toolkit/internal/state"
	"github.com/PiluTech/toolkit/internal/tui"
)

// ---------------------------------------------------------------------------------------------------------
// Planner: o MESMO passe do `toolkit install --dry-run` (DryRunRunner: leituras reais, mutações só
// registradas), mais as receitas. Nunca um segundo planejador.
// ---------------------------------------------------------------------------------------------------------

type tuiPlanner struct {
	deps Deps
	rd   RecipeDeps
}

func (p *tuiPlanner) euid() func() int {
	if p.deps.Euid != nil {
		return p.deps.Euid
	}
	return os.Geteuid
}

// Plan calcula o plano da seleção; nada é executado.
func (p *tuiPlanner) Plan(ctx context.Context, sel tui.Selection) (tui.Plan, error) {
	var plan tui.Plan
	info, err := p.rd.detect(ctx, p.deps.runner())
	if err != nil {
		return plan, err
	}
	plan.Env = fmt.Sprintf("%s %s · usuário %s", info.OS, info.Arch, info.User)

	if len(sel.Apps) > 0 {
		if err := p.planApps(ctx, sel.Apps, &plan); err != nil {
			return plan, err
		}
	}
	var rt *recipeRuntime
	if len(sel.Recipes) > 0 {
		if rt, err = setupRecipes(ctx, p.deps, p.rd); err != nil {
			return plan, err
		}
		p.planRecipes(rt, sel.Recipes, &plan)
	}
	plan.Summary = planSummary(len(sel.Apps), sel.Recipes)
	plan.NothingToDo = len(sel.Recipes) == 0 && len(plan.Problems) == 0 && len(plan.Commands) == 0
	return plan, nil
}

func planSummary(apps int, recs []tui.RecipeMark) string {
	var parts []string
	if apps > 0 {
		parts = append(parts, "instalar "+countWord(apps, "app", "apps"))
	}
	var rc, dg int
	for _, r := range recs {
		if r.Diagnostic {
			dg++
		} else {
			rc++
		}
	}
	if rc > 0 {
		parts = append(parts, "rodar "+countWord(rc, "receita", "receitas"))
	}
	if dg > 0 {
		parts = append(parts, "rodar "+countWord(dg, "diagnóstico", "diagnósticos"))
	}
	return "Vai " + strings.Join(parts, " e ") + ". Nada foi executado ainda."
}

func countWord(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (p *tuiPlanner) planApps(ctx context.Context, marks []tui.Mark, plan *tui.Plan) error {
	cat, err := loadCatalog(ctx, p.deps, listFlags{}, io.Discard)
	if err != nil {
		return err
	}
	var targets []installTarget
	for _, m := range marks {
		app, ok := cat.Get(m.ID)
		if !ok {
			plan.Problems = append(plan.Problems, fmt.Sprintf("app %q fora do catálogo", m.ID))
			continue
		}
		vars, version, err := resolveVersion(app, m.Version)
		if err != nil {
			plan.Problems = append(plan.Problems, fmt.Sprintf("%s: %v", app.ID, err))
			continue
		}
		targets = append(targets, installTarget{app, vars, version})
	}
	if len(targets) == 0 {
		return nil
	}
	var buf, errBuf bytes.Buffer
	failed, dry := installPass(ctx, &buf, &errBuf, p.deps, targets, true)
	for _, id := range failed {
		plan.Problems = append(plan.Problems, id+": "+failReason(errBuf.String()))
	}

	// O que muda: o texto do passe do plano por app; avisos (flatpak, reinício) vão para AVISOS.
	cur := ""
	for _, l := range strings.Split(buf.String(), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case l == "":
		case strings.HasPrefix(l, "==> "):
			cur = strings.TrimPrefix(l, "==> ")
		case l == msg.InstallFlatpakNote || l == msg.InstallReboot:
			plan.Warnings = appendUnique(plan.Warnings, l)
		default:
			plan.Changes = append(plan.Changes, cur+": "+l)
		}
	}
	// Comandos exatamente como o runner real os executa (mesmo GOOS/euid).
	display := exec.OSRunner{GOOS: p.deps.GOOS, Euid: p.euid()}
	usesSudo := false
	for _, step := range dry.Plan() {
		if step.Note != "" {
			plan.Commands = append(plan.Commands, step.Note)
			continue
		}
		line := display.Display(step.Cmd)
		if display.UsesSudo(step.Cmd) {
			usesSudo = true
		}
		if step.Cmd.Elevate && step.Err != nil { // Windows sem token de administrador
			line = "[exige administrador] " + line
			plan.Warnings = appendUnique(plan.Warnings, "este plano exige administrador: abra o terminal como administrador")
		}
		plan.Commands = append(plan.Commands, line)
	}
	if usesSudo {
		plan.Warnings = appendUnique(plan.Warnings, msg.WarnPlanSudo)
	}
	return nil
}

func failReason(errText string) string {
	for _, l := range strings.Split(errText, "\n") {
		if l = strings.TrimSpace(strings.TrimPrefix(l, "erro:")); l != "" && !strings.HasPrefix(l, "dica:") {
			return l
		}
	}
	return "falhou no plano"
}

func appendUnique(s []string, v string) []string {
	if slices.Contains(s, v) {
		return s
	}
	return append(s, v)
}

// planRecipes lista cada receita (etapas, administrador, reinício) e traz as pré-checagens do `recipe run`
// como Problems. O script PowerShell vai LEGÍVEL, com a nota de que segue como -EncodedCommand.
func (p *tuiPlanner) planRecipes(rt *recipeRuntime, marks []tui.RecipeMark, plan *tui.Plan) {
	for _, m := range marks {
		r, ok := rt.set.Get(rt.info.OS, m.ID)
		if !ok {
			plan.Problems = append(plan.Problems, fmt.Sprintf("receita %q não existe para %s", m.ID, rt.info.OS))
			continue
		}
		if err := recipePrecheck(rt, r); err != nil {
			plan.Problems = append(plan.Problems, err.Error())
		}
		kind := "receita"
		if r.Kind == recipes.KindDiagnostic {
			kind = "diagnóstico"
		}
		plan.Changes = append(plan.Changes, fmt.Sprintf("%s %s (%s): %s", kind, r.ID, r.Name, countWord(len(r.Steps), "etapa", "etapas")))
		if r.RequiresAdmin {
			plan.Warnings = appendUnique(plan.Warnings, fmt.Sprintf("%s exige administrador", r.ID))
		}
		if hasReboot(r) {
			plan.Warnings = appendUnique(plan.Warnings, fmt.Sprintf("%s pode reiniciar a máquina (o toolkit pergunta antes e retoma sozinho)", r.ID))
		}
		elevate := rt.info.OS != platform.Windows && !rt.info.IsAdmin
		display := exec.OSRunner{GOOS: string(rt.info.OS), Euid: p.euid()}
		for i, st := range r.Steps {
			plan.Commands = append(plan.Commands, fmt.Sprintf("[%s %d/%d] %s", r.ID, i+1, len(r.Steps), stepDisplay(display, rt.info.OS, st, elevate)))
		}
	}
}

// recipePrecheck repete o que o `toolkit recipe run` confere antes de começar.
func recipePrecheck(rt *recipeRuntime, r *recipes.Recipe) error {
	if rt.info.OS == platform.Linux && hasReboot(r) && !rt.info.IsAdmin {
		return fmt.Errorf(msg.ErrRecipeNeedsSudo, r.ID)
	}
	if rt.info.OS == platform.Windows && r.RequiresAdmin && !rt.info.IsAdmin {
		return fmt.Errorf(msg.ErrRecipeNeedsAdminWin, r.ID)
	}
	return nil
}

// stepDisplay mostra o que a etapa executa. PowerShell: o script legível + nota; WSL, sh e bash: o comando
// como o runner o executa; install: os apps do catálogo.
func stepDisplay(display exec.OSRunner, goos platform.OS, st recipes.Step, elevate bool) string {
	if len(st.Install) > 0 {
		return "instalar pelo catálogo: " + strings.Join(st.Install, ", ")
	}
	if st.Run == nil {
		return st.Name
	}
	if st.Run.PowerShell != "" && goos == platform.Windows {
		return st.Run.PowerShell + " (enviado como -EncodedCommand)"
	}
	cmd, err := recipes.BuildCmd(st.Run, goos, st.Timeout, elevate, false)
	if err != nil {
		return st.Name
	}
	return display.Display(cmd)
}

// ---------------------------------------------------------------------------------------------------------
// Runner: providers.InstallApp (installOne) e recipes.Engine, contando tudo por eventos.
// ---------------------------------------------------------------------------------------------------------

type tuiRunner struct {
	deps    Deps
	rd      RecipeDeps
	logDir  func() string // pasta do log em arquivo (nil = não grava)
	now     func() time.Time
	rtCache *recipeRuntime
}

// lineWriter quebra o que é escrito em linhas (\n) e as entrega a emit; o resto fica no buffer.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		w.emit(line)
	}
	return len(b), nil
}

func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}

// eventPrompter faz o Confirm do reinício virar uma pergunta [s/N] na TUI. Reply é bufferizado (cap 1) e a
// espera termina com o ctx: nenhum lado bloqueia o outro.
type eventPrompter struct {
	send func(tui.Event) bool
}

func (p eventPrompter) Confirm(ctx context.Context, question string) (bool, error) {
	reply := make(chan bool, 1)
	if !p.send(tui.Event{Kind: tui.EventRebootPrompt, Prompt: question, Reply: reply}) {
		return false, ctx.Err()
	}
	select {
	case v := <-reply:
		return v, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

type queueItem struct {
	name   string
	app    *installTarget
	recipe *recipes.Recipe
}

// Run executa o lote: apps primeiro, depois receitas, a que reinicia por último. Fecha ch ao terminar.
func (r *tuiRunner) Run(ctx context.Context, sel tui.Selection, ch chan<- tui.Event) {
	defer close(ch)
	send := func(ev tui.Event) bool {
		select {
		case ch <- ev:
			return true
		case <-ctx.Done():
			// depois do cancelamento ainda precisamos contar o fim: tenta sem esperar o ctx
			select {
			case ch <- ev:
				return true
			default:
				return false
			}
		}
	}
	fl := r.openFileLog(send)
	defer fl.close()

	items, problems := r.buildQueue(ctx, sel)
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.name
	}
	send(tui.Event{Kind: tui.EventItems, Items: names})
	for _, p := range problems {
		send(tui.Event{Kind: tui.EventWarn, Line: p})
	}
	stop := false
	for i, it := range items {
		if stop || ctx.Err() != nil {
			st := tui.ItemNotRun
			if ctx.Err() != nil && i > 0 {
				st = tui.ItemNotRun
			}
			send(tui.Event{Kind: tui.EventItemDone, Index: i, Status: st})
			fl.line("item %d (%s): não executado", i+1, it.name)
			continue
		}
		send(tui.Event{Kind: tui.EventItemStarted, Index: i, Name: it.name})
		fl.line("item %d (%s): início", i+1, it.name)
		start := time.Now()
		var status tui.ItemStatus
		var errText string
		emit := func(line string) { send(tui.Event{Kind: tui.EventLog, Index: i, Line: line}) }
		w := &lineWriter{emit: emit}
		switch {
		case it.app != nil:
			status, errText = r.runApp(ctx, w, *it.app)
		case it.recipe != nil:
			status, errText, stop = r.runRecipe(ctx, w, it.recipe, eventPrompter{send: send})
		}
		w.Flush()
		if ctx.Err() != nil && status == tui.ItemFailed {
			status = tui.ItemCanceled
		}
		dur := time.Since(start)
		send(tui.Event{Kind: tui.EventItemDone, Index: i, Status: status, Err: errText, Duration: dur})
		fl.line("item %d (%s): %s %s", i+1, it.name, status, errText)
	}
	send(tui.Event{Kind: tui.EventDone})
}

// buildQueue monta a fila: apps na ordem marcada, receitas sem reinício e, por último, as que reiniciam.
func (r *tuiRunner) buildQueue(ctx context.Context, sel tui.Selection) (items []queueItem, problems []string) {
	if len(sel.Apps) > 0 {
		cat, err := loadCatalog(ctx, r.deps, listFlags{}, io.Discard)
		for _, m := range sel.Apps {
			var app catalog.App
			ok := err == nil
			if ok {
				app, ok = cat.Get(m.ID)
			}
			if !ok {
				problems = append(problems, fmt.Sprintf("app %q indisponível no catálogo", m.ID))
				items = append(items, queueItem{name: m.Name})
				continue
			}
			vars, version, verr := resolveVersion(app, m.Version)
			if verr != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", m.ID, verr))
				items = append(items, queueItem{name: app.Name})
				continue
			}
			items = append(items, queueItem{name: app.Name, app: &installTarget{app, vars, version}})
		}
	}
	if len(sel.Recipes) > 0 {
		rt, err := setupRecipes(ctx, r.deps, r.rd)
		var plain, rebooting []queueItem
		for _, m := range sel.Recipes {
			var rec *recipes.Recipe
			ok := err == nil
			if ok {
				rec, ok = rt.set.Get(rt.info.OS, m.ID)
			}
			if !ok {
				problems = append(problems, fmt.Sprintf("receita %q indisponível", m.ID))
				plain = append(plain, queueItem{name: m.Name})
				continue
			}
			it := queueItem{name: rec.Name, recipe: rec}
			if hasReboot(rec) {
				rebooting = append(rebooting, it)
			} else {
				plain = append(plain, it)
			}
		}
		items = append(items, plain...)
		items = append(items, rebooting...)
		r.rtCache = rt
	}
	return items, problems
}

func (r *tuiRunner) runApp(ctx context.Context, w io.Writer, t installTarget) (tui.ItemStatus, string) {
	_, _ = fmt.Fprintln(w, fmt.Sprintf(msg.InstallHeader, t.app.ID, t.app.Name))
	reg := providers.NewRegistry(r.deps.providerList(r.deps.runner())...)
	if err := installOne(ctx, w, reg, r.deps.GOOS, t.app, t.vars, t.version, false); err != nil {
		if errors.Is(err, providers.ErrNeedsAdmin) {
			_, _ = fmt.Fprintln(w, msg.HintNeedsAdmin)
		}
		return tui.ItemFailed, err.Error()
	}
	return tui.ItemOK, ""
}

// runRecipe roda a receita pela engine, com o prompter por evento. stop = a fila para (reinício pedido).
func (r *tuiRunner) runRecipe(ctx context.Context, w io.Writer, rec *recipes.Recipe, prompter recipes.Prompter) (tui.ItemStatus, string, bool) {
	rt := r.rtCache
	if rt == nil {
		return tui.ItemFailed, "receitas indisponíveis", false
	}
	if err := recipePrecheck(rt, rec); err != nil {
		return tui.ItemFailed, err.Error(), false
	}
	eng, closeLog, err := buildEngine(rt, r.deps, r.rd, recipeFlags{}, rec.ID, prompter, w, w)
	if err != nil {
		return tui.ItemFailed, err.Error(), false
	}
	defer closeLog()
	rep, err := eng.Run(ctx, rec, recipes.RunOptions{Yes: false, Verbose: true})
	for _, s := range rep.Steps {
		_, _ = fmt.Fprintf(w, "etapa %s: %s\n", s.ID, s.Outcome)
		for _, l := range strings.Split(s.Output, "\n") {
			if l != "" {
				_, _ = fmt.Fprintln(w, "  "+l)
			}
		}
		if s.Error != "" {
			_, _ = fmt.Fprintln(w, "  erro: "+s.Error)
		}
	}
	switch {
	case rep.RebootPending || rep.Rebooting || rep.Status == state.RecipeAwaitingReboot:
		return tui.ItemReboot, "", true
	case err != nil:
		return tui.ItemFailed, err.Error(), false
	case rep.Status == state.RecipeFailed:
		return tui.ItemFailed, "a receita falhou", false
	}
	return tui.ItemOK, "", false
}

// ---- log em arquivo (só ciclo de vida: nunca a saída dos comandos, então não há segredo a redigir) ----

type fileLog struct {
	f *os.File
}

func (r *tuiRunner) openFileLog(send func(tui.Event) bool) *fileLog {
	if r.logDir == nil {
		return &fileLog{}
	}
	dir := r.logDir()
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	warn := func(err error) {
		send(tui.Event{Kind: tui.EventWarn, Line: "Não deu para gravar o log em arquivo; a execução continua e o log fica só na tela."})
		_ = err
	}
	if dir == "" {
		warn(nil)
		return &fileLog{}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		warn(err)
		return &fileLog{}
	}
	p := filepath.Join(dir, "tui-"+now().UTC().Format("20060102-150405")+".log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		warn(err)
		return &fileLog{}
	}
	return &fileLog{f: f}
}

func (l *fileLog) line(format string, a ...any) {
	if l.f != nil {
		_, _ = fmt.Fprintf(l.f, strings.TrimRight(format, " ")+"\n", a...)
	}
}

func (l *fileLog) close() {
	if l.f != nil {
		_ = l.f.Close()
	}
}
