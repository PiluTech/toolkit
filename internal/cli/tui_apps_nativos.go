package cli

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"

	"github.com/PiluTech/toolkit/internal/appsnativos"
	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/textsafe"
	"github.com/PiluTech/toolkit/internal/tui"
)

// newAppsNativosTab monta a aba Apps nativos da TUI sobre o mesmo núcleo do `toolkit apps-nativos` (mesmo System, mesmo
// Store, mesmas regras: plano antes, backup antes, nunca eleva sozinho). Fora do Windows (deps.GOOS) a aba só avisa.
func newAppsNativosTab(ctx context.Context, deps Deps, th tui.Theme) tui.Tab {
	return tui.NewNativeAppsTab(ctx, &appsNativosBackend{deps: deps}, th)
}

// appsNativosBackend implementa tui.NativeAppsBackend. O ambiente (System e Store) é montado na primeira leitura e
// reaproveitado: no Windows isso consulta o PowerShell (SID do usuário), então não se repete a cada tecla.
type appsNativosBackend struct {
	deps Deps
	mu   sync.Mutex
	env  *appsNatEnv
	// prepared guarda os planos mostrados, por ID opaco. A TUI guarda o ID do plano EXIBIDO e a confirmação chama
	// Apply(ID): só esse plano é aplicável (uso único) e, ao aplicar, todos os demais são descartados. Uma preparação
	// cancelada (Esc) que termina tarde grava sob o SEU ID, que a TUI nunca usa: nunca é aplicável. Apply ainda exige que
	// o plano recalculado agora seja IDÊNTICO ao mostrado (o sistema não mudou); senão ErrNatStalePlan.
	prepared map[string]*natPrepared
	nextID   int
}

type natPrepared struct {
	sel     tui.NatSelection
	text    string
	pending int
	// items é a CÓPIA ESTRUTURADA dos itens do plano mostrado (nome, FullName, família, publicador, versão, tipo, escopo e as
	// marcas): o texto do plano não traz todos os campos, e Apply compara a identidade executada, não só o texto.
	items []appsnativos.Item
	scope appsnativos.Scope
}

// maxNatPrepared limita os planos guardados (preparações abandonadas); passando disso, os antigos são descartados.
const maxNatPrepared = 16

func (b *appsNativosBackend) prepare(p *natPrepared) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.prepared == nil || len(b.prepared) >= maxNatPrepared {
		b.prepared = map[string]*natPrepared{}
	}
	b.nextID++
	id := "plano-" + strconv.Itoa(b.nextID)
	b.prepared[id] = p
	return id
}

// take devolve e CONSOME o plano com esse ID (uso único) e descarta todos os outros.
func (b *appsNativosBackend) take(id string) *natPrepared {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.prepared[id]
	b.prepared = nil
	return p
}

func (b *appsNativosBackend) getEnv(ctx context.Context) (appsNatEnv, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.env != nil {
		return *b.env, nil
	}
	e, err := newAppsNatEnv(ctx, b.deps)
	if err != nil {
		return appsNatEnv{}, err
	}
	b.env = &e
	return e, nil
}

func (b *appsNativosBackend) Load(ctx context.Context) (tui.NatInfo, error) {
	if b.deps.GOOS != "windows" {
		l := diagfmt.LangFromEnv(b.deps.UpdateCheck.env)
		return tui.NatInfo{OS: printable(b.deps.GOOS), Reason: textsafe.Line(appsnativos.UnsupportedMessage(b.deps.GOOS, l))}, nil
	}
	e, err := b.getEnv(ctx)
	if err != nil {
		return tui.NatInfo{}, err
	}
	pkgs, err := e.sys.ListInstalled(ctx, false)
	if err != nil {
		return tui.NatInfo{}, cleanErr(err)
	}
	have := map[string]bool{}
	for _, p := range pkgs {
		have[p.Name] = true
	}
	info := tui.NatInfo{Supported: true, OS: "windows", IsAdmin: e.sys.IsAdmin(ctx)}
	for _, a := range appsnativos.Apps() {
		info.Apps = append(info.Apps, tui.NatApp{ID: a.ID, Title: a.Title(e.lang), Basic: a.Basico, Installed: have[a.Name]})
	}
	return info, nil
}

// build calcula o plano da seleção.
func (b *appsNativosBackend) build(ctx context.Context, sel tui.NatSelection) (*appsnativos.Plan, appsNatEnv, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return nil, e, err
	}
	if len(sel.Apps) == 0 {
		return nil, e, appsnativos.ErrNadaSelecionado
	}
	plan, err := appsnativos.Build(ctx, e.sys, appsnativos.Selection{Apps: sel.Apps, TodosUsuarios: sel.AllUsers}, e.lang)
	if err != nil {
		return nil, e, cleanErr(err)
	}
	return plan, e, nil
}

func (b *appsNativosBackend) Plan(ctx context.Context, sel tui.NatSelection) (tui.NatPlan, error) {
	plan, e, err := b.build(ctx, sel)
	if err != nil {
		return tui.NatPlan{}, err
	}
	text := plan.Format(e.lang)
	id := b.prepare(&natPrepared{sel: sel, text: text, pending: plan.Pending(), items: append([]appsnativos.Item(nil), plan.Items...), scope: plan.Scope})
	return tui.NatPlan{ID: id, Text: textsafe.Multiline(text), Pending: plan.Pending(), NeedsAdmin: plan.NeedsAdmin(), IsAdmin: e.sys.IsAdmin(ctx)}, nil
}

func (b *appsNativosBackend) Apply(ctx context.Context, sel tui.NatSelection, planID string) (tui.NatResult, error) {
	prep := b.take(planID)
	if prep == nil || !reflect.DeepEqual(prep.sel, sel) {
		return tui.NatResult{}, tui.ErrNatStalePlan
	}
	plan, e, err := b.build(ctx, sel)
	if err != nil {
		return tui.NatResult{}, err
	}
	if plan.Format(e.lang) != prep.text || plan.Pending() != prep.pending || plan.Scope != prep.scope || !reflect.DeepEqual(plan.Items, prep.items) {
		return tui.NatResult{}, tui.ErrNatStalePlan // o sistema mudou desde o plano mostrado
	}
	if plan.Pending() == 0 {
		return tui.NatResult{}, nil
	}
	if plan.NeedsAdmin() && !e.sys.IsAdmin(ctx) { // recusa ANTES de mudar qualquer coisa
		return tui.NatResult{}, tui.ErrNatNeedsAdmin
	}
	res, err := appsnativos.Apply(ctx, e.sys, e.store, plan, e.now(), appsnativos.Options{})
	var out tui.NatResult
	if res != nil {
		out = tui.NatResult{Removed: len(res.Removed), BackupPath: res.BackupPath}
	}
	if errors.Is(err, appsnativos.ErrNeedsAdmin) {
		return out, tui.ErrNatNeedsAdmin
	}
	if err != nil {
		return out, cleanErr(err)
	}
	return out, nil
}

// Reinstall mostra o "como reinstalar" do backup. Só texto: nada é executado.
func (b *appsNativosBackend) Reinstall(ctx context.Context) (string, error) {
	e, err := b.getEnv(ctx)
	if err != nil {
		return "", err
	}
	rp, err := appsnativos.BuildReinstall(e.store)
	if errors.Is(err, appsnativos.ErrNoBackup) {
		return "", tui.ErrNatNoBackup
	}
	if err != nil {
		return "", cleanErr(err)
	}
	return textsafe.Multiline(rp.Format(e.lang)), nil
}
