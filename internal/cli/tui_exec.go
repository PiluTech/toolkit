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
		p.planRecipes(ctx, rt, sel.Recipes, &plan)
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
	p.planTargets(ctx, targets, "", plan)
	return nil
}

// planTargets roda o passe do plano (o MESMO do `toolkit install --dry-run`: DryRunRunner, leituras reais,
// mutações só registradas) para estes alvos e acrescenta ao plano o que muda, os comandos COMO O RUNNER REAL OS
// EXECUTA (mesmo GOOS/euid), os avisos e os problemas. prefix marca de onde vem (passo de receita).
func (p *tuiPlanner) planTargets(ctx context.Context, targets []installTarget, prefix string, plan *tui.Plan) {
	if len(targets) == 0 {
		return
	}
	var buf, errBuf bytes.Buffer
	failed, dry := installPass(ctx, &buf, &errBuf, p.deps, targets, true)
	for _, id := range failed {
		plan.Problems = append(plan.Problems, prefix+id+": "+failReason(errBuf.String()))
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
			plan.Changes = append(plan.Changes, prefix+cur+": "+l)
		}
	}
	display := exec.OSRunner{GOOS: p.deps.GOOS, Euid: p.euid()}
	for _, step := range dry.Plan() {
		if step.Note != "" {
			plan.Commands = append(plan.Commands, prefix+step.Note)
			continue
		}
		line := display.Display(step.Cmd)
		p.noteCommand(display, step.Cmd, plan)
		plan.Approved = appendUnique(plan.Approved, line)
		if step.Cmd.Elevate && step.Err != nil { // Windows sem token de administrador
			line = "[exige administrador] " + line
			plan.Warnings = appendUnique(plan.Warnings, "este plano exige administrador: abra o terminal como administrador")
		}
		plan.Commands = append(plan.Commands, prefix+line)
	}
}

// noteCommand acende o aviso de sudo -n quando ESTE comando rodaria via sudo (mesma decisão do argv).
func (p *tuiPlanner) noteCommand(display exec.OSRunner, c exec.Cmd, plan *tui.Plan) {
	if display.UsesSudo(c) {
		plan.Warnings = appendUnique(plan.Warnings, msg.WarnPlanSudo)
	}
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
// como Problems. Os comandos seguem a MESMA regra de elevação da engine (runAction): só com requires_admin, fora
// do Windows e sem ser administrador, e valem para o check e para o run. Passos `install:` passam pelo mesmo
// passe do plano do install e mostram os comandos reais. O script PowerShell vai LEGÍVEL, com a nota de que
// segue como -EncodedCommand.
func (p *tuiPlanner) planRecipes(ctx context.Context, rt *recipeRuntime, marks []tui.RecipeMark, plan *tui.Plan) {
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
		elevate := r.RequiresAdmin && rt.info.OS != platform.Windows && !rt.info.IsAdmin // como Engine.runAction
		display := exec.OSRunner{GOOS: string(rt.info.OS), Euid: p.euid()}
		for i, st := range r.Steps {
			tag := fmt.Sprintf("[%s %d/%d] ", r.ID, i+1, len(r.Steps))
			if len(st.Install) > 0 {
				p.planInstallStep(ctx, st, tag, plan)
				continue
			}
			if st.Check != nil {
				if line, cmd, ok := actionDisplay(display, rt.info.OS, st.Check, st.Timeout, elevate, true); ok {
					plan.Commands = append(plan.Commands, tag+"verificar: "+line)
					p.noteCommand(display, cmd, plan)
				}
			}
			if st.Run != nil {
				if line, cmd, ok := actionDisplay(display, rt.info.OS, st.Run, st.Timeout, elevate, false); ok {
					plan.Commands = append(plan.Commands, tag+line)
					plan.Approved = appendUnique(plan.Approved, display.Display(cmd))
					p.noteCommand(display, cmd, plan)
				}
			}
		}
	}
}

// planInstallStep passa um passo `install:` pelo mesmo passe do plano do install: comandos reais (com sudo).
func (p *tuiPlanner) planInstallStep(ctx context.Context, st recipes.Step, tag string, plan *tui.Plan) {
	cat, err := loadCatalog(ctx, p.deps, listFlags{}, io.Discard)
	if err != nil {
		plan.Problems = append(plan.Problems, tag+err.Error())
		return
	}
	var targets []installTarget
	for _, ref := range st.Install {
		id, requested, err := parseTarget(ref)
		if err != nil {
			plan.Problems = append(plan.Problems, tag+err.Error())
			continue
		}
		app, ok := cat.Get(id)
		if !ok {
			plan.Problems = append(plan.Problems, tag+fmt.Sprintf("app %q fora do catálogo", id))
			continue
		}
		vars, version, err := resolveVersion(app, requested)
		if err != nil {
			plan.Problems = append(plan.Problems, tag+fmt.Sprintf("%s: %v", id, err))
			continue
		}
		targets = append(targets, installTarget{app, vars, version})
	}
	p.planTargets(ctx, targets, tag, plan)
}

// actionDisplay mostra uma ação de receita: PowerShell legível + nota, o resto como o runner o executa.
func actionDisplay(display exec.OSRunner, goos platform.OS, a *recipes.Action, timeout time.Duration, elevate, readOnly bool) (string, exec.Cmd, bool) {
	cmd, err := recipes.BuildCmd(a, goos, timeout, elevate, readOnly)
	if err != nil {
		return "", exec.Cmd{}, false
	}
	if a.PowerShell != "" && goos == platform.Windows {
		return a.PowerShell + " (enviado como -EncodedCommand)", cmd, true
	}
	return display.Display(cmd), cmd, true
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

// ---------------------------------------------------------------------------------------------------------
// Runner: providers.InstallApp (installOne) e recipes.Engine, contando tudo por eventos.
// ---------------------------------------------------------------------------------------------------------

type tuiRunner struct {
	approved func(exec.Runner) exec.Runner
	deps     Deps
	rd       RecipeDeps
	logDir   func() string // pasta do log em arquivo (nil = não grava)
	now      func() time.Time
	rtCache  *recipeRuntime
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

// errNotApproved: o executor recusa um comando que não estava no plano aprovado.
var errNotApproved = errors.New("comando fora do plano aprovado (mudou desde a Revisão)")

// approvedRunner só deixa passar comando que muda o sistema se ele estava no plano EXIBIDO e aprovado na
// Revisão (comparação pela linha exibida, como o runner real a executa). Leituras (ReadOnly) passam. Falha
// fechado: sem plano aprovado nenhuma mutação roda.
type approvedRunner struct {
	inner   exec.Runner
	display exec.OSRunner
	allowed map[string]bool
}

func (a approvedRunner) Run(ctx context.Context, c exec.Cmd) (exec.Result, error) {
	if !c.ReadOnly {
		if line := a.display.Display(c); !a.allowed[line] {
			return exec.Result{}, fmt.Errorf("%w: %s", errNotApproved, line)
		}
	}
	return a.inner.Run(ctx, c)
}

func (a approvedRunner) LookPath(name string) (string, error) { return a.inner.LookPath(name) }

// guard devolve o envoltório do Runner com a lista de permissão do plano aprovado.
func (r *tuiRunner) guard(approved []string) func(exec.Runner) exec.Runner {
	allowed := make(map[string]bool, len(approved))
	for _, l := range approved {
		allowed[l] = true
	}
	euid := r.deps.Euid
	if euid == nil {
		euid = os.Geteuid
	}
	display := exec.OSRunner{GOOS: r.deps.GOOS, Euid: euid}
	return func(in exec.Runner) exec.Runner { return approvedRunner{inner: in, display: display, allowed: allowed} }
}

// errClass reduz um erro à CLASSE para o log em arquivo: a mensagem completa (que pode trazer a 1ª linha do
// stderr do gerenciador, com URL ou token) fica só na tela.
func errClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errNotApproved):
		return "not-approved"
	case errors.Is(err, providers.ErrNeedsAdmin):
		return "needs-admin"
	case errors.Is(err, providers.ErrNotFound):
		return "not-found"
	case errors.Is(err, providers.ErrNoApplicableInstaller):
		return "no-installer"
	case errors.Is(err, providers.ErrHashMismatch):
		return "hash-mismatch"
	case errors.Is(err, providers.ErrRetryable):
		return "retryable"
	case errors.Is(err, providers.ErrVersionUnsupported):
		return "version-unsupported"
	case errors.Is(err, providers.ErrInvalidRef):
		return "invalid-ref"
	case errors.Is(err, exec.ErrNotElevated):
		return "not-elevated"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return "error"
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

	r.approved = r.guard(sel.Approved)
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
		var errText, class string
		emit := func(line string) { send(tui.Event{Kind: tui.EventLog, Index: i, Line: line}) }
		w := &lineWriter{emit: emit}
		switch {
		case it.app != nil:
			status, errText, class = r.runApp(ctx, w, *it.app)
		case it.recipe != nil:
			status, errText, class, stop = r.runRecipe(ctx, w, it.recipe, eventPrompter{send: send})
		}
		w.Flush()
		if ctx.Err() != nil && status == tui.ItemFailed {
			status = tui.ItemCanceled
		}
		dur := time.Since(start)
		send(tui.Event{Kind: tui.EventItemDone, Index: i, Status: status, Err: errText, Duration: dur})
		// log em arquivo: só status + CLASSE do erro; o texto do erro (que pode trazer stderr com URL/token) só vai à tela
		if class != "" {
			fl.line("item %d (%s): %s [%s]", i+1, it.name, status, class)
		} else {
			fl.line("item %d (%s): %s", i+1, it.name, status)
		}
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

func (r *tuiRunner) runApp(ctx context.Context, w io.Writer, t installTarget) (tui.ItemStatus, string, string) {
	_, _ = fmt.Fprintln(w, fmt.Sprintf(msg.InstallHeader, t.app.ID, t.app.Name))
	reg := providers.NewRegistry(r.deps.providerList(r.approved(r.deps.runner()))...)
	if err := installOne(ctx, w, reg, r.deps.GOOS, t.app, t.vars, t.version, false); err != nil {
		if errors.Is(err, providers.ErrNeedsAdmin) {
			_, _ = fmt.Fprintln(w, msg.HintNeedsAdmin)
		}
		return tui.ItemFailed, err.Error(), errClass(err)
	}
	return tui.ItemOK, "", ""
}

// runRecipe roda a receita pela engine, com o prompter por evento. stop = a fila para (reinício pedido).
func (r *tuiRunner) runRecipe(ctx context.Context, w io.Writer, rec *recipes.Recipe, prompter recipes.Prompter) (status tui.ItemStatus, errText, class string, stop bool) {
	rt := r.rtCache
	if rt == nil {
		return tui.ItemFailed, "receitas indisponíveis", "error", false
	}
	if err := recipePrecheck(rt, rec); err != nil {
		return tui.ItemFailed, err.Error(), "precheck", false
	}
	eng, closeLog, err := buildEngine(rt, r.deps, r.rd, recipeFlags{}, rec.ID, prompter, r.approved, w, w)
	if err != nil {
		return tui.ItemFailed, err.Error(), errClass(err), false
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
		return tui.ItemReboot, "", "", true
	case err != nil:
		c := errClass(err)
		if c == "error" {
			c = "recipe-failed"
		}
		return tui.ItemFailed, err.Error(), c, false
	case rep.Status == state.RecipeFailed:
		return tui.ItemFailed, "a receita falhou", "recipe-failed", false
	}
	return tui.ItemOK, "", "", false
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
