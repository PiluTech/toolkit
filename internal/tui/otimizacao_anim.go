package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Erros que o backend das animações devolve para a seção mostrar a instrução certa.
var (
	// ErrAnimStalePlan: a aplicação não corresponde ao plano mostrado (o sistema ou o backup mudou, ou não há plano
	// preparado): nada foi alterado e é preciso ver o plano de novo.
	ErrAnimStalePlan = errors.New("animações: o plano mudou")
	// ErrAnimNoBackup: não há backup para desfazer.
	ErrAnimNoBackup = errors.New("animações: sem backup")
)

// Estados de um item das animações lido do sistema.
const (
	AnimApplied = "applied" // o valor já está como o toolkit define
	AnimPending = "pending" // ainda não está
)

// AnimItem é um item do catálogo de animações com o estado atual.
type AnimItem struct {
	Title string
	State string
}

// AnimInfo é o que a parte Animações carrega.
type AnimInfo struct {
	Items     []AnimItem
	HasBackup bool // existe backup para desfazer
}

// AnimPlan é um plano pronto para mostrar (texto já formatado, na língua do usuário).
type AnimPlan struct {
	ID      string // ID opaco do plano: a confirmação aplica SÓ o plano com esse ID
	Text    string
	Pending int
}

// AnimResult é o que uma aplicação ou restauração fez.
type AnimResult struct {
	Applied    int
	BackupPath string
	Nothing    bool // desfazer sem nada a restaurar (o backup foi removido)
}

// AnimBackend liga a parte Animações da seção ao pacote animacoes. Load e Plan/UndoPlan só leem; Apply e Undo mudam o
// registro do usuário e só são chamados depois da confirmação dentro da seção.
type AnimBackend interface {
	Load(ctx context.Context) (AnimInfo, error)
	Plan(ctx context.Context) (AnimPlan, error)
	Apply(ctx context.Context, planID string) (AnimResult, error)
	UndoPlan(ctx context.Context) (AnimPlan, error)
	Undo(ctx context.Context, planID string) (AnimResult, error)
}

type otimPart int

const (
	partGargalos otimPart = iota
	partAnim
)

type animLoadedMsg struct {
	info AnimInfo
	err  error
}

type animPlanMsg struct {
	seq  int
	kind privKind
	plan AnimPlan
	err  error
}

type animDoneMsg struct {
	kind privKind
	res  AnimResult
	err  error
}

// animState é o estado da parte Animações (plano, confirmação, resultado). Mesmo desenho da aba Privacidade.
type animState struct {
	be      AnimBackend
	loading bool
	loaded  bool
	loadErr error
	info    AnimInfo

	busy    privBusy
	seq     int
	changed bool // alguma ação já mudou o sistema nesta sessão: o erro de releitura não pode dizer "Nada foi alterado"
	cancel  context.CancelFunc
	view    bool
	kind    privKind
	plan    AnimPlan
	asking  bool
	conf    *Confirm
	scroll  int
	notice  string
	warn    bool
	res     *Result
}

// SetAnimations liga a parte Animações (o `toolkit otimizacao animacoes` na TUI). Sem backend a seção só tem os gargalos.
func (t *OptimizeTab) SetAnimations(be AnimBackend) { t.a.be = be }

func (t *OptimizeTab) hasAnim() bool { return t.a.be != nil && t.supported() }

func (t *OptimizeTab) inAnim() bool { return t.hasAnim() && t.part == partAnim }

func (t *OptimizeTab) animUsable() bool {
	return t.a.loaded && t.a.loadErr == nil && !t.a.loading
}

// Result implementa Resulter: o resultado da última ação das animações (nil = nada a mostrar).
func (t *OptimizeTab) Result() *Result { return t.a.res }

// Dismiss implementa Resulter: fecha a faixa.
func (t *OptimizeTab) Dismiss() { t.a.res = nil }

func (t *OptimizeTab) animLoadCmd() tea.Cmd {
	a := &t.a
	a.loading = true
	be, ctx := a.be, t.ctx
	return tea.Batch(func() tea.Msg {
		info, err := be.Load(ctx)
		return animLoadedMsg{info: info, err: err}
	}, t.theme.Tick())
}

func (t *OptimizeTab) animSetNotice(s string, warn bool) {
	t.a.notice, t.a.warn = textsafe.Line(s), warn
}

// animUpdate trata as mensagens da parte Animações; devolve handled=true quando a mensagem era dela.
func (t *OptimizeTab) animUpdate(msg tea.Msg) (tea.Cmd, bool) {
	a := &t.a
	switch m := msg.(type) {
	case animLoadedMsg:
		a.loading = false
		if m.err != nil {
			a.loadErr = m.err
			return nil, true
		}
		a.loadErr, a.loaded = nil, true
		items := make([]AnimItem, len(m.info.Items))
		for i, it := range m.info.Items {
			items[i] = AnimItem{Title: textsafe.Line(it.Title), State: it.State}
		}
		a.info = AnimInfo{Items: items, HasBackup: m.info.HasBackup}
		return nil, true
	case animPlanMsg:
		if m.seq != a.seq || a.busy != busyPlan {
			return nil, true // plano atrasado de uma tecla anterior
		}
		a.busy = busyNone
		if a.cancel != nil {
			a.cancel()
			a.cancel = nil
		}
		t.animOnPlan(m)
		return nil, true
	case animDoneMsg:
		if m.res.Applied > 0 {
			a.changed = true
		}
		t.animOnDone(m)
		return t.animLoadCmd(), true
	}
	return nil, false
}

func (t *OptimizeTab) animOnPlan(m animPlanMsg) {
	a := &t.a
	a.scroll, a.asking = 0, false
	if m.err != nil {
		a.view = false
		switch {
		case errors.Is(m.err, ErrAnimNoBackup):
			t.animSetNotice("Não há backup de animações: nada a desfazer.", false)
		default:
			t.animSetNotice("erro: "+m.err.Error(), true)
		}
		return
	}
	a.view, a.kind, a.plan = true, m.kind, m.plan
	a.plan.Text = textsafe.Multiline(m.plan.Text)
	switch {
	case m.kind == privView:
		t.animSetNotice("Só leitura: nada foi alterado. Esc volta.", false)
	case m.kind == privApply && m.plan.Pending == 0:
		t.animSetNotice("Nada a fazer: as animações já estão como o plano manda.", false)
	default:
		a.asking = true
		a.conf = t.animConfirm()
		a.notice = ""
	}
}

// animConfirm monta a confirmação única a partir do plano devolvido pelo backend (o texto vira o corpo rolável).
func (t *OptimizeTab) animConfirm() *Confirm {
	a := &t.a
	p := a.plan
	var c *Confirm
	switch {
	case a.kind == privUndo && p.Pending == 0:
		c = NewConfirm("Remover backup", "")
		c.Title = "Remover o backup das animações?"
		c.Summary = "Tudo já está como antes: não há o que restaurar. Só o arquivo de backup será removido."
		c.Backup = "o backup será apagado; depois dele não há mais como voltar a estes valores."
		c.Undo = "se quiser reduzir as animações de novo, use a (aplicar)."
	case a.kind == privUndo:
		c = NewConfirm("Restaurar", "")
		c.Title = "Restaurar as animações?"
		c.Summary = fmt.Sprintf("Devolve %d valor(es) ao que tinham antes (valor atual -> original). Valor que não existia é apagado.", p.Pending)
		c.Backup = "os valores originais vêm do backup; ele é removido depois de restaurar."
		c.Undo = "depois de restaurar não há backup; para reduzir de novo, aplique outra vez (a)."
	default:
		c = NewConfirm("Aplicar", "")
		c.Title = "Reduzir as animações? (experimental)"
		c.Summary = fmt.Sprintf("Muda %d valor(es) do usuário atual (HKCU), sem administrador.", p.Pending)
		c.Warn = "Função experimental: os valores ainda não foram confirmados numa máquina de teste. Algumas animações só valem depois de reabrir a sessão do Windows ou o Explorer; o toolkit não reinicia o Explorer sozinho."
		c.Backup = "o valor original de cada item (inclusive \"não existia\") é guardado em backup antes de mudar; o caminho aparece ao fim."
		c.Undo = "na seção Otimização, parte Animações, a tecla d restaura os valores do backup."
	}
	var keep []string
	for _, l := range strings.Split(strings.TrimRight(p.Text, "\n"), "\n") {
		if !isUndoHint(l) { // o comando manual de cada item cansa a leitura; o backup já restaura (tecla d)
			keep = append(keep, l)
		}
	}
	c.Text = strings.Join(keep, "\n")
	return c
}

func (t *OptimizeTab) animOnDone(m animDoneMsg) {
	a := &t.a
	a.busy, a.asking, a.view, a.scroll, a.conf = busyNone, false, false, 0, nil
	what := "aplicar"
	if m.kind == privUndo {
		what = "restaurar"
	}
	bk := ""
	if m.res.BackupPath != "" {
		bk = "Backup em " + textsafe.Line(m.res.BackupPath)
	}
	undo := "Para desfazer: tecle d na parte Animações"
	switch {
	case m.err != nil && errors.Is(m.err, ErrAnimStalePlan):
		t.animSetNotice("O plano mudou: nada foi alterado.", true)
		a.res = &Result{Kind: ResultInfo, Title: "O plano mudou", Lines: []string{"O sistema ou o backup mudou desde o plano mostrado: nada foi alterado.", "Veja o plano de novo (p, a ou d) antes de confirmar."}}
	case m.err != nil && m.res.Applied > 0:
		t.animSetNotice("erro: "+m.err.Error(), true)
		lines := []string{fmt.Sprintf("Antes da falha: %d mudança(s)", m.res.Applied), "Erro: " + m.err.Error()}
		if bk != "" {
			lines = append(lines, bk+". Ele guarda os valores originais.")
		}
		a.res = &Result{Kind: ResultPartial, Title: "Parou no meio", Lines: append(lines, undo)}
	case m.err != nil:
		t.animSetNotice("erro: "+m.err.Error(), true)
		a.res = &Result{Kind: ResultError, Title: "Não foi possível " + what, Lines: []string{"Erro: " + m.err.Error(), "Nenhuma mudança foi contada antes da falha."}}
	case m.res.Nothing:
		t.animSetNotice("Nada a restaurar: tudo já estava como antes.", false)
		a.res = &Result{Kind: ResultInfo, Title: "Nada a restaurar", Lines: []string{"O backup existia, mas tudo já estava como antes.", "O backup foi removido."}}
	case m.kind == privUndo:
		t.animSetNotice(fmt.Sprintf("Restaurado: %d item(ns).", m.res.Applied), false)
		a.res = &Result{Kind: ResultOK, Title: "Animações restauradas", Lines: []string{fmt.Sprintf("%d item(ns) voltaram ao valor original", m.res.Applied), "O backup foi removido."}}
	default:
		t.animSetNotice(fmt.Sprintf("Aplicado: %d mudança(s).", m.res.Applied), false)
		lines := []string{fmt.Sprintf("%d mudança(s)", m.res.Applied)}
		if bk != "" {
			lines = append(lines, bk)
		}
		lines = append(lines, "Algumas animações só valem depois de reabrir a sessão do Windows ou o Explorer (função experimental)")
		a.res = &Result{Kind: ResultOK, Title: "Animações reduzidas", Lines: append(lines, undo)}
	}
	a.res = cleanResult(a.res)
}

// animKey trata as teclas da parte Animações. Mudar o registro só acontece depois do plano na tela e do "s" (ou "y").
func (t *OptimizeTab) animKey(k string) tea.Cmd {
	a := &t.a
	if a.busy == busyPlan && (k == "esc" || k == "escape") {
		// Esc cancela a PREPARAÇÃO do plano: invalida a resposta (seq) e o contexto; uma aplicação já iniciada não é cancelável.
		a.seq++
		if a.cancel != nil {
			a.cancel()
			a.cancel = nil
		}
		a.busy, a.asking, a.view = busyNone, false, false
		t.animSetNotice("Preparação do plano cancelada: nada foi alterado.", false)
		return nil
	}
	if a.loading || a.busy != busyNone {
		return nil
	}
	if a.asking {
		switch a.conf.Update(k) {
		case ConfirmAccept:
			return t.animConfirmed()
		case ConfirmCancel:
			a.asking, a.view, a.conf, a.scroll = false, false, nil, 0
			t.animSetNotice("Cancelado: nada foi alterado.", false)
		}
		return nil
	}
	if a.view {
		switch k {
		case "up", "down", "pgup", "pgdown":
			t.animScroll(k)
		case "esc", "enter", "left", "backspace":
			a.view, a.scroll, a.notice = false, 0, ""
		}
		return nil
	}
	a.notice = ""
	switch k {
	case "m":
		t.part = partGargalos
	case "c":
		return t.animLoadCmd()
	case "up", "k":
		t.animScroll("up")
	case "down", "j":
		t.animScroll("down")
	case "pgup", "pgdown":
		t.animScroll(k)
	case "p":
		return t.animPlanCmd(privView)
	case "a":
		return t.animPlanCmd(privApply)
	case "d":
		return t.animPlanCmd(privUndo)
	}
	return nil
}

func (t *OptimizeTab) animScroll(k string) {
	a := &t.a
	page := max(t.lastH/2, 1)
	switch k {
	case "up":
		a.scroll = max(a.scroll-1, 0)
	case "down":
		a.scroll++
	case "pgup":
		a.scroll = max(a.scroll-page, 0)
	case "pgdown":
		a.scroll += page
	}
}

func (t *OptimizeTab) animPlanCmd(kind privKind) tea.Cmd {
	a := &t.a
	a.busy = busyPlan
	a.seq++
	pctx, cancel := context.WithCancel(t.ctx)
	a.cancel = cancel
	seq, be := a.seq, a.be
	return tea.Batch(func() tea.Msg {
		var plan AnimPlan
		var err error
		if kind == privUndo {
			plan, err = be.UndoPlan(pctx)
		} else {
			plan, err = be.Plan(pctx)
		}
		return animPlanMsg{seq: seq, kind: kind, plan: plan, err: err}
	}, t.theme.Tick())
}

func (t *OptimizeTab) animConfirmed() tea.Cmd {
	a := &t.a
	a.asking, a.conf, a.busy, a.notice, a.res = false, nil, busyApply, "", nil
	be, ctx, kind, id := a.be, t.ctx, a.kind, a.plan.ID
	return tea.Batch(func() tea.Msg {
		var res AnimResult
		var err error
		if kind == privUndo {
			res, err = be.Undo(ctx, id)
		} else {
			res, err = be.Apply(ctx, id)
		}
		return animDoneMsg{kind: kind, res: res, err: err}
	}, t.theme.Tick())
}

func (t *OptimizeTab) animKeys() []KeyHint {
	a := &t.a
	switch {
	case a.loading || a.loadErr != nil:
		return []KeyHint{act("c").Hint(), act("m").as("gargalos").Hint()}
	case a.busy == busyApply:
		return nil
	case a.busy == busyPlan:
		return []KeyHint{{"Esc", "cancelar"}}
	case a.asking:
		return a.conf.keyHints()
	case a.view:
		return []KeyHint{{"↑↓", "rolar"}, {"Esc", "voltar"}}
	}
	return []KeyHint{act("p").Hint(), act("a").Hint(), act("d").Hint(), act("c").Hint(), act("m").as("gargalos").Hint()}
}

func (t *OptimizeTab) animHelp() []KeyDef {
	a := &t.a
	if a.loading || a.loadErr != nil {
		return []KeyDef{act("c").says("lê o estado das animações outra vez."), act("m").as("gargalos").says("volta para os gargalos.")}
	}
	return []KeyDef{
		act("p").says("mostra o plano das animações: o que muda, sem alterar nada."),
		act("a").says("reduz as animações depois da confirmação; o backup e como desfazer aparecem antes."),
		act("d").says("desfaz o que o toolkit aplicou nas animações, a partir do backup, depois da confirmação."),
		act("c").says("lê o estado das animações outra vez."),
		act("m").as("gargalos").says("alterna entre as animações e a leitura de gargalos."),
	}
}

func (t *OptimizeTab) animStatus() string {
	a := &t.a
	switch {
	case a.asking && a.conf != nil:
		return a.conf.Hint()
	case a.busy == busyApply:
		return t.theme.Spin(t.frame) + " Aplicando… não feche a janela."
	case a.busy == busyPlan:
		return t.theme.Spin(t.frame) + " Preparando o plano… Esc cancela."
	case a.loading:
		return "Lendo o estado das animações…"
	case a.loadErr != nil:
		return "Falhou. c lê de novo."
	case a.notice != "" && a.warn:
		return SymWarn + " " + a.notice
	case a.notice != "":
		return a.notice
	}
	return "Animações (experimental) — só o usuário atual; nada muda antes de confirmar"
}

func (t *OptimizeTab) animMore() int {
	a := &t.a
	if !t.animUsable() || a.asking {
		return 0 // a confirmação mostra o próprio "↓ mais N linhas"
	}
	if a.view {
		return max(len(t.animPlanLines(t.lastW))-(a.scroll+t.lastH-1), 0)
	}
	return max(len(t.animMainLines(t.lastW))-(a.scroll+t.lastH), 0)
}

func (t *OptimizeTab) animView(w, h int) string {
	a := &t.a
	switch {
	case a.loading:
		return strings.Join(StateLoading(t.theme, w, h, "o estado das animações", t.frame), "\n")
	case a.loadErr != nil && a.changed:
		return strings.Join(StateErrorAfterChange(t.theme, w, h, "as animações", a.loadErr, "Tecle c para reler o estado."), "\n")
	case a.loadErr != nil:
		return strings.Join(StateError(t.theme, w, h, "as animações", a.loadErr, "Tente de novo (c)."), "\n")
	case !a.loaded:
		return strings.Join(StateLoading(t.theme, w, h, "o estado das animações", t.frame), "\n")
	}
	if a.asking && a.conf != nil {
		return strings.Join(a.conf.View(t.theme, w, h), "\n")
	}
	var lines []string
	if a.view {
		lines = t.animFitPlan(w, h)
	} else {
		all := t.animMainLines(w)
		a.scroll = max(min(a.scroll, max(len(all)-h, 0)), 0)
		lines = all[min(a.scroll, len(all)):min(a.scroll+h, len(all))]
	}
	for i := range lines {
		lines[i] = clip(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

func (t *OptimizeTab) animFitPlan(w, h int) []string {
	a := &t.a
	all := t.animPlanLines(w)
	room := max(h-1, 1)
	a.scroll = max(min(a.scroll, max(len(all)-room, 0)), 0)
	title := "Plano das animações (experimental)"
	if a.kind == privUndo {
		title = "O que será restaurado (valor atual -> original)"
	}
	head := " " + t.theme.Title().Render(titleCase(t.theme, title))
	return append([]string{head}, all[min(a.scroll, len(all)):min(a.scroll+room, len(all))]...)
}

func (t *OptimizeTab) animPlanLines(w int) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(t.a.plan.Text, "\n"), "\n") {
		if isUndoHint(l) {
			continue
		}
		out = append(out, wrapLine(" "+l, w)...)
	}
	return out
}

func (t *OptimizeTab) animMainLines(w int) []string {
	a := &t.a
	var out []string
	out = append(out, " "+t.theme.Title().Render(titleCase(t.theme, "Animações do Windows"))+" · "+t.theme.Warn().Render("[experimental]")+" · só o usuário atual")
	out = append(out, wrapLine(" Reduz as animações de janela do usuário atual (HKCU), sem administrador. Os valores ainda não foram confirmados numa máquina de teste; antes de mudar o valor original vai para um backup, e a tecla d desfaz.", w)...)
	title := " Ajustes "
	out = append(out, SymRule+t.theme.Title().Render(title)+strings.Repeat(SymRule, max(w-lipgloss.Width(title)-1, 1)))
	for _, it := range a.info.Items {
		sym, word, style := SymPending, "pendente", Paint{}
		if it.State == AnimApplied {
			sym, word, style = SymOK, "aplicado", t.theme.OK()
		}
		out = append(out, hangLines(" "+style.Render(cell(sym+" "+word, 16))+" ", 1+16+1, it.Title, w)...)
	}
	if len(a.info.Items) == 0 {
		out = append(out, " Nenhum ajuste.")
	}
	out = append(out, "")
	if a.info.HasBackup {
		out = append(out, wrapLine(" Há um backup: d restaura os valores originais.", w)...)
	} else {
		out = append(out, wrapLine(" Sem backup: nada foi aplicado por aqui.", w)...)
	}
	out = append(out, wrapLine(" Reduzir animação melhora a sensação de resposta; não é promessa de ganho de desempenho.", w)...)
	return out
}

// animDetail é o painel da parte Animações (o que muda, backup, desfazer, limites). Vazio quando há plano, confirmação ou
// uma ação em andamento.
func (t *OptimizeTab) animDetail(w, h int) string {
	a := &t.a
	if !t.animUsable() || a.asking || a.view || a.busy != busyNone {
		return ""
	}
	d := newDetail(t.theme, w)
	d.name("Animações (experimental)")
	d.section("O que faz")
	d.para("Define o valor MinAnimate do usuário atual para \"0\": as janelas minimizam e maximizam sem animação. Nada além disso é alterado.")
	d.section("Efeito")
	d.para("Algumas animações só valem depois de reabrir a sessão do Windows ou o Explorer. O toolkit não reinicia o Explorer sozinho.")
	d.section("Como desfaz")
	d.para("O valor original (inclusive \"não existia\") vai para um backup antes da primeira mudança. A tecla d restaura tipo e conteúdo; reaplicar não sobrescreve o original.")
	d.section("Limites")
	d.para("Só HKCU, sem administrador, sem serviços, tarefas ou HKLM. Função experimental até os valores serem confirmados numa máquina de teste.")
	return d.render(h)
}
