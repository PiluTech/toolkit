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

// Erros que o backend de privacidade devolve para a aba mostrar a instrução certa.
var (
	// ErrPrivNeedsAdmin: o plano tem itens de administrador e o processo não é administrador (nada foi mudado).
	ErrPrivNeedsAdmin = errors.New("privacidade: exige administrador")
	// ErrPrivStalePlan: a aplicação não corresponde ao plano que foi mostrado (o sistema ou o backup mudou, ou não há plano
	// preparado): nada foi alterado e é preciso ver o plano de novo.
	ErrPrivStalePlan = errors.New("privacidade: o plano mudou")
	// ErrPrivNoBackup: não há backup para desfazer.
	ErrPrivNoBackup = errors.New("privacidade: sem backup")
)

// PrivProfile é um perfil de privacidade ("basico" ou "recomendado").
type PrivProfile string

// Perfis da aba.
const (
	PrivBasico      PrivProfile = "basico"
	PrivRecomendado PrivProfile = "recomendado"
)

// Estados de um ajuste lido do sistema.
const (
	PrivApplied = "applied" // todos os valores já estão como o perfil manda
	PrivPending = "pending" // nenhum está
	PrivPartial = "partial" // alguns
	PrivAbsent  = "absent"  // nada a mudar nesta versão do Windows (serviço ou tarefa ausente)
)

// PrivSetting é um ajuste do catálogo com o estado atual.
type PrivSetting struct {
	Title string
	Admin bool // exige administrador (só o perfil recomendado o inclui)
	State string
}

// PrivInfo é o que a aba carrega. Supported=false fora do Windows: a aba só avisa.
type PrivInfo struct {
	Supported bool
	OS        string
	Edition   string
	IsAdmin   bool
	Settings  []PrivSetting
}

// PrivPlan é um plano pronto para mostrar (texto já formatado, na língua do usuário).
type PrivPlan struct {
	ID         string // ID opaco do plano: a confirmação aplica SÓ o plano com esse ID
	Text       string
	Pending    int
	NeedsAdmin bool // alguma mudança pendente exige administrador
	IsAdmin    bool
}

// PrivResult é o que uma aplicação ou restauração fez.
type PrivResult struct {
	Applied    int
	BackupPath string
	Nothing    bool // desfazer sem nada a restaurar (o backup foi removido)
}

// PrivacyBackend liga a aba ao pacote privacidade. Load e Plan/UndoPlan só leem; Apply e Undo mudam o sistema e
// só são chamados depois da confirmação (botão Aplicar/Restaurar ou "s") dentro da aba.
type PrivacyBackend interface {
	Load(ctx context.Context) (PrivInfo, error)
	Plan(ctx context.Context, p PrivProfile, onlyUser bool) (PrivPlan, error)
	Apply(ctx context.Context, p PrivProfile, onlyUser bool, planID string) (PrivResult, error)
	UndoPlan(ctx context.Context) (PrivPlan, error)
	Undo(ctx context.Context, planID string) (PrivResult, error)
}

type privKind int

const (
	privView privKind = iota // só ver o plano
	privApply
	privUndo
)

type privBusy int

const (
	busyNone privBusy = iota
	busyPlan
	busyApply
)

type privLoadedMsg struct {
	info PrivInfo
	err  error
}

type privPlanMsg struct {
	seq  int
	kind privKind
	plan PrivPlan
	err  error
}

type privDoneMsg struct {
	kind privKind
	res  PrivResult
	err  error
}

// PrivacyTab é a aba Privacidade (só Windows).
type PrivacyTab struct {
	be    PrivacyBackend
	ctx   context.Context
	theme Theme

	loading bool
	loadErr error
	info    PrivInfo

	cur      int  // 0 basico, 1 recomendado
	onlyUser bool // aplicar só os itens do usuário (--somente-usuario)
	scroll   int
	lastW    int
	lastH    int
	frame    int

	busy    privBusy
	seq     int
	changed bool               // alguma ação já mudou o sistema nesta sessão: o erro de releitura não pode dizer "Nada foi alterado"
	cancel  context.CancelFunc // cancela a preparação do plano em andamento (Esc)
	view    bool               // mostrando um plano
	kind    privKind
	plan    PrivPlan
	profile PrivProfile // perfil do plano mostrado
	asking  bool
	conf    *Confirm // confirmação única (só existe com asking)
	only    bool     // onlyUser do plano mostrado
	notice  string
	warn    bool    // o aviso é de problema (símbolo "!")
	res     *Result // resultado da última ação, mostrado na faixa da raiz até a pessoa fechar (x)
}

// NewPrivacyTab cria a aba. O backend decide o que é "suportado" (no real, GOOS == windows).
func NewPrivacyTab(ctx context.Context, be PrivacyBackend, th Theme) *PrivacyTab {
	return &PrivacyTab{be: be, ctx: ctx, theme: th, loading: true}
}

// SetContext troca o contexto das chamadas ao backend.
func (t *PrivacyTab) SetContext(ctx context.Context) { t.ctx = ctx }

// Title implementa Tab.
func (t *PrivacyTab) Title() string { return "Privacidade" }

// Short implementa Tab.
func (t *PrivacyTab) Short() string { return "Priv" }

// Init lê o estado (assíncrono: nunca bloqueia o Update).
func (t *PrivacyTab) Init() tea.Cmd { return tea.Batch(t.loadCmd(), t.theme.Tick()) }

// Reload relê o estado depois de uma execução.
func (t *PrivacyTab) Reload() tea.Cmd { return t.loadCmd() }

func (t *PrivacyTab) loadCmd() tea.Cmd {
	be, ctx := t.be, t.ctx
	return func() tea.Msg {
		info, err := be.Load(ctx)
		return privLoadedMsg{info: info, err: err}
	}
}

// Capturing implementa Tab: durante a confirmação e a aplicação, todas as teclas são da aba (q, Tab e os números não saem).
// Capturing diz que, durante a confirmação e a aplicação, todas as teclas são da aba (q, Tab e os números não saem).
func (t *PrivacyTab) Capturing() bool { return t.asking || t.busy == busyApply }

// Selected implementa Tab.
func (t *PrivacyTab) Selected() int { return 0 }

func (t *PrivacyTab) profileNow() PrivProfile {
	if t.cur == 1 {
		return PrivRecomendado
	}
	return PrivBasico
}

func (t *PrivacyTab) usable() bool { return !t.loading && t.loadErr == nil && t.info.Supported }

// Update trata dados carregados, planos, resultados, o spinner e as teclas.
func (t *PrivacyTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case privLoadedMsg:
		t.loading = false
		if msg.err != nil {
			t.loadErr = msg.err
			return t, nil
		}
		t.loadErr = nil
		t.info = cleanPrivInfo(msg.info)
	case privPlanMsg:
		if msg.seq != t.seq || t.busy != busyPlan {
			return t, nil // plano atrasado de uma tecla anterior
		}
		t.busy = busyNone
		if t.cancel != nil {
			t.cancel()
			t.cancel = nil
		}
		t.onPlan(msg)
	case privDoneMsg:
		if msg.res.Applied > 0 {
			t.changed = true
		}
		t.onDone(msg)
		return t, t.loadCmd()
	case tickMsg:
		t.frame++
		if t.loading || t.busy != busyNone {
			return t, t.theme.Tick()
		}
	case tea.KeyPressMsg:
		return t, t.key(msg.String())
	}
	return t, nil
}

func cleanPrivInfo(i PrivInfo) PrivInfo {
	i.OS, i.Edition = textsafe.Line(i.OS), textsafe.Line(i.Edition)
	s := make([]PrivSetting, len(i.Settings))
	for k, v := range i.Settings {
		s[k] = PrivSetting{Title: textsafe.Line(v.Title), Admin: v.Admin, State: v.State}
	}
	i.Settings = s
	return i
}

func (t *PrivacyTab) setNotice(s string, warn bool) { t.notice, t.warn = textsafe.Line(s), warn }

func (t *PrivacyTab) onPlan(m privPlanMsg) {
	t.scroll, t.asking = 0, false
	if m.err != nil {
		t.view = false
		switch {
		case errors.Is(m.err, ErrPrivNoBackup):
			t.setNotice("Não há backup de privacidade: nada a desfazer.", false)
		default:
			t.setNotice("erro: "+m.err.Error(), true)
		}
		return
	}
	t.view, t.kind, t.plan = true, m.kind, m.plan
	t.plan.Text = textsafe.Multiline(m.plan.Text)
	switch {
	case m.kind == privView:
		t.setNotice("Só leitura: nada foi alterado. Esc volta.", false)
	case m.kind == privApply && m.plan.Pending == 0:
		t.setNotice("Nada a fazer: tudo já está como o perfil manda.", false)
	default:
		t.asking = true
		t.conf = t.buildConfirm()
		t.notice = ""
	}
}

// buildConfirm monta a tela de confirmação a partir do plano que o backend devolveu (o texto vira corpo rolável).
// Plano com itens de administrador num terminal sem administrador entra com o botão de confirmar indisponível.
func (t *PrivacyTab) buildConfirm() *Confirm {
	p := t.plan
	blocked := p.NeedsAdmin && !p.IsAdmin
	reason := ""
	if blocked {
		reason = "sem administrador"
	}
	var c *Confirm
	switch {
	case t.kind == privUndo && p.Pending == 0:
		c = NewConfirm("Remover backup", reason)
		c.Title = "Remover o backup de privacidade?"
		c.Summary = "Tudo já está como antes: não há o que restaurar. Só o arquivo de backup será removido."
		c.Backup = "o backup será apagado; depois dele não há mais como voltar a estes valores."
		c.Undo = "se quiser os valores atuais de volta, aplique o perfil de novo (a)."
	case t.kind == privUndo:
		c = NewConfirm("Restaurar", reason)
		c.Title = "Restaurar o estado anterior?"
		c.Summary = fmt.Sprintf("Devolve %d item(ns) ao valor que tinham antes do perfil (valor atual -> original).", p.Pending)
		c.Backup = "os valores originais vêm do backup; ele é removido depois de restaurar."
		c.Undo = "depois de restaurar não há backup; para voltar ao perfil, aplique-o de novo (a)."
	default:
		c = NewConfirm("Aplicar", reason)
		c.Title = "Aplicar o perfil " + string(t.profile) + "?"
		c.Summary = fmt.Sprintf("Muda %d ajuste(s) de privacidade do Windows", p.Pending)
		if t.only {
			c.Summary += ", só os itens do usuário."
		} else {
			c.Summary += "."
		}
		c.Backup = "o valor original de cada item é guardado em backup antes de mudar; o caminho aparece ao fim."
		c.Undo = "na aba Privacidade, a tecla d restaura os valores do backup."
	}
	switch {
	case blocked && t.kind == privUndo:
		c.Warn = "O backup tem itens que exigem administrador e este terminal não é: nada será alterado. Cancele, abra o terminal como administrador (botão direito, \"Executar como administrador\") e tente de novo. O toolkit nunca eleva sozinho."
	case blocked:
		c.Warn = "Este plano tem itens que exigem administrador e este terminal não é: nada será alterado. Cancele e abra o terminal como administrador (botão direito, \"Executar como administrador\"), ou cancele e use m para aplicar só o que é do usuário. O toolkit nunca eleva sozinho."
	case p.NeedsAdmin:
		c.Warn = "Parte das mudanças é de sistema e exige administrador (este terminal é)."
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

func (t *PrivacyTab) onDone(m privDoneMsg) {
	t.busy, t.asking, t.view, t.scroll = busyNone, false, false, 0
	what := "aplicar"
	if m.kind == privUndo {
		what = "restaurar"
	}
	bk := ""
	if m.res.BackupPath != "" {
		bk = "Backup em " + textsafe.Line(m.res.BackupPath)
	}
	undo := "Para desfazer: tecle d nesta aba"
	switch {
	case m.err != nil && errors.Is(m.err, ErrPrivStalePlan):
		t.setNotice("O plano mudou: nada foi alterado.", true)
		t.res = &Result{Kind: ResultInfo, Title: "O plano mudou", Lines: []string{"O sistema ou o backup mudou desde o plano mostrado: nada foi alterado.", "Veja o plano de novo (p, a ou d) antes de confirmar."}}
	case m.err != nil && errors.Is(m.err, ErrPrivNeedsAdmin):
		t.setNotice("Precisa de administrador: nada foi alterado.", true)
		t.res = &Result{Kind: ResultError, Title: "Precisa de administrador", Lines: adminResultLines()}
	case m.err != nil && m.res.Applied > 0:
		t.setNotice("erro: "+m.err.Error(), true)
		lines := []string{fmt.Sprintf("Antes da falha: %d mudança(s)", m.res.Applied), "Erro: " + m.err.Error()}
		if bk != "" {
			lines = append(lines, bk+". Ele guarda os valores originais.")
		}
		t.res = &Result{Kind: ResultPartial, Title: "Parou no meio", Lines: append(lines, undo)}
	case m.err != nil:
		t.setNotice("erro: "+m.err.Error(), true)
		t.res = &Result{Kind: ResultError, Title: "Não foi possível " + what, Lines: []string{"Erro: " + m.err.Error(), "Nenhuma mudança foi contada antes da falha."}}
	case m.res.Nothing:
		t.setNotice("Nada a restaurar: tudo já estava como antes.", false)
		t.res = &Result{Kind: ResultInfo, Title: "Nada a restaurar", Lines: []string{"O backup existia, mas tudo já estava como antes.", "O backup foi removido."}}
	case m.kind == privUndo:
		t.setNotice(fmt.Sprintf("Restaurado: %d item(ns).", m.res.Applied), false)
		t.res = &Result{Kind: ResultOK, Title: "Restaurado", Lines: []string{fmt.Sprintf("%d item(ns) voltaram ao valor original", m.res.Applied), "O backup foi removido."}}
	default:
		t.setNotice(fmt.Sprintf("Aplicado: %d mudança(s).", m.res.Applied), false)
		lines := []string{fmt.Sprintf("%d mudança(s)", m.res.Applied)}
		if bk != "" {
			lines = append(lines, bk)
		}
		t.res = &Result{Kind: ResultOK, Title: "Perfil " + string(t.profile) + " aplicado", Lines: append(lines, undo)}
	}
	t.res = cleanResult(t.res)
}

// Result implementa Resulter: o resultado da última ação (nil = nada a mostrar).
func (t *PrivacyTab) Result() *Result { return t.res }

// Dismiss implementa Resulter: fecha a faixa.
func (t *PrivacyTab) Dismiss() { t.res = nil }

// cleanResult passa todo texto do resultado por textsafe (erros do backend podem trazer saída externa).
func cleanResult(r *Result) *Result {
	r.Title = textsafe.Line(r.Title)
	for i, l := range r.Lines {
		r.Lines[i] = textsafe.Line(l)
	}
	return r
}

// key trata as teclas. Mudar o sistema só acontece depois do plano na tela e do "s" (ou "y") na confirmação.
func (t *PrivacyTab) key(k string) tea.Cmd {
	if t.busy == busyPlan && (k == "esc" || k == "escape") {
		// Esc cancela a PREPARAÇÃO do plano: invalida a resposta (seq) e o contexto; uma aplicação já iniciada não é cancelável.
		t.seq++
		if t.cancel != nil {
			t.cancel()
			t.cancel = nil
		}
		t.busy, t.asking, t.view = busyNone, false, false
		t.setNotice("Preparação do plano cancelada: nada foi alterado.", false)
		return nil
	}
	if t.loading || t.busy != busyNone || !t.usable() {
		return nil
	}
	if t.asking {
		return t.confirmKey(k)
	}
	if t.view {
		switch k {
		case "up", "down", "pgup", "pgdown":
			t.scrollKey(k)
		case "esc", "enter", "left", "backspace":
			t.view, t.scroll, t.notice = false, 0, ""
		}
		return nil
	}
	t.notice = ""
	switch k {
	case "up", "k":
		t.cur = 0
	case "down", "j":
		t.cur = 1
	case "pgup", "pgdown":
		t.scrollKey(k)
	case "m":
		t.onlyUser = !t.onlyUser
	case "p":
		return t.planCmd(privView)
	case "a":
		return t.planCmd(privApply)
	case "d":
		return t.planCmd(privUndo)
	}
	return nil
}

// confirmKey entrega a tecla à confirmação: só o botão de confirmar ativo aplica; Cancelar, "n" e Esc voltam sem mudar nada.
func (t *PrivacyTab) confirmKey(k string) tea.Cmd {
	switch t.conf.Update(k) {
	case ConfirmAccept:
		return t.confirm()
	case ConfirmCancel:
		t.asking, t.view, t.conf, t.scroll = false, false, nil, 0
		t.setNotice("Cancelado: nada foi alterado.", false)
	}
	return nil
}

func (t *PrivacyTab) scrollKey(k string) {
	page := max(t.lastH/2, 1)
	switch k {
	case "up":
		t.scroll = max(t.scroll-1, 0)
	case "down":
		t.scroll++
	case "pgup":
		t.scroll = max(t.scroll-page, 0)
	case "pgdown":
		t.scroll += page
	}
}

func (t *PrivacyTab) planCmd(kind privKind) tea.Cmd {
	t.busy = busyPlan
	t.seq++
	pctx, cancel := context.WithCancel(t.ctx)
	t.cancel = cancel
	seq, be, ctx, p, only := t.seq, t.be, pctx, t.profileNow(), t.onlyUser
	t.profile, t.only = p, only
	return tea.Batch(func() tea.Msg {
		var plan PrivPlan
		var err error
		if kind == privUndo {
			plan, err = be.UndoPlan(ctx)
		} else {
			plan, err = be.Plan(ctx, p, only && kind == privApply)
		}
		return privPlanMsg{seq: seq, kind: kind, plan: plan, err: err}
	}, t.theme.Tick())
}

func (t *PrivacyTab) confirm() tea.Cmd {
	t.asking, t.conf, t.busy, t.notice, t.res = false, nil, busyApply, "", nil
	be, ctx, p, only, kind, id := t.be, t.ctx, t.profile, t.only, t.kind, t.plan.ID
	return tea.Batch(func() tea.Msg {
		var res PrivResult
		var err error
		if kind == privUndo {
			res, err = be.Undo(ctx, id)
		} else {
			res, err = be.Apply(ctx, p, only, id)
		}
		return privDoneMsg{kind: kind, res: res, err: err}
	}, t.theme.Tick())
}

// Keys implementa Tab.
func (t *PrivacyTab) Keys() []KeyHint {
	switch {
	case t.loading || t.loadErr != nil || !t.info.Supported:
		return nil
	case t.busy == busyApply:
		return nil
	case t.asking:
		return t.conf.keyHints()
	case t.view:
		return []KeyHint{{"↑↓", "rolar"}, {"Esc", "voltar"}}
	}
	return []KeyHint{{"↑↓", "perfil"}, act("p").Hint(), act("a").Hint(), act("d").Hint(), act("m").as("só usuário").on(t.onlyUser).Hint()}
}

// HelpKeys implementa SectionHelper: as teclas da seção, uma frase por tecla (legenda única em keys.go).
func (t *PrivacyTab) HelpKeys() []KeyDef {
	if !t.usable() {
		return noActionsHelp(t.loading || t.loadErr != nil)
	}
	return []KeyDef{
		navDef("↑↓", "perfil", "escolhe o perfil de privacidade (do mais leve ao mais forte)."),
		act("p").says("mostra o plano do perfil: o que muda, sem alterar nada."),
		act("a").says("aplica o perfil depois da confirmação; o backup e como desfazer aparecem antes."),
		act("d").says("desfaz o que o toolkit aplicou, a partir do backup, depois da confirmação."),
		act("m").as("só usuário").says("liga e desliga \"só usuário\": aplica só ao usuário atual, sem tocar na máquina."),
	}
}

// Status implementa Tab: a dica de teclas da confirmação, o aviso ou o resultado.
func (t *PrivacyTab) Status() string {
	switch {
	case t.asking && t.conf != nil:
		return t.conf.Hint()
	case t.busy == busyApply:
		return t.theme.Spin(t.frame) + " Aplicando… não feche a janela."
	case t.notice != "" && t.warn:
		return SymWarn + " " + t.notice
	case t.notice != "":
		return t.notice
	}
	return ""
}

// More implementa Tab.
func (t *PrivacyTab) More() int {
	if !t.usable() {
		return 0
	}
	var n int
	if t.asking {
		return 0 // a confirmação mostra o próprio "↓ mais N linhas"
	}
	if t.view {
		n = len(t.planLines(t.lastW)) - (t.scroll + t.lastH - 1)
	} else {
		n = len(t.mainLines(t.lastW)) - (t.scroll + t.lastH)
	}
	return max(n, 0)
}

// View desenha o corpo.
func (t *PrivacyTab) View(w, h int) string {
	t.lastW, t.lastH = w, h
	switch {
	case t.loading:
		return strings.Join(StateLoading(t.theme, w, h, "o estado de privacidade", t.frame), "\n")
	case t.loadErr != nil && t.changed:
		return strings.Join(StateErrorAfterChange(t.theme, w, h, "a privacidade", t.loadErr, "Saia (q) e abra o toolkit de novo para reler o estado."), "\n")
	case t.loadErr != nil:
		return strings.Join(StateError(t.theme, w, h, "a privacidade", t.loadErr, "Saia (q) e abra o toolkit de novo."), "\n")
	case !t.info.Supported:
		return strings.Join(StateUnsupported(t.theme, w, h, "a privacidade", unsupportedWhy(t.info.OS, "os ajustes de privacidade"), osName(t.info.OS)), "\n")
	}
	var lines []string
	if t.asking && t.conf != nil {
		return strings.Join(t.conf.View(t.theme, w, h), "\n")
	}
	if t.view {
		lines = t.fitPlan(w, h)
	} else {
		t.scroll = max(min(t.scroll, max(len(t.mainLines(w))-h, 0)), 0)
		all := t.mainLines(w)
		lines = all[min(t.scroll, len(all)):min(t.scroll+h, len(all))]
	}
	for i := range lines {
		lines[i] = clip(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

func (t *PrivacyTab) fitPlan(w, h int) []string {
	all := t.planLines(w)
	room := max(h-1, 1)
	t.scroll = max(min(t.scroll, max(len(all)-room, 0)), 0)
	title := "Plano do perfil " + string(t.profile)
	switch t.kind {
	case privUndo:
		title = "O que será restaurado (valor atual -> original)"
	case privApply:
		if t.only {
			title += " · só itens do usuário"
		}
	}
	head := " " + t.theme.Title().Render(titleCase(t.theme, title))
	return append([]string{head}, all[min(t.scroll, len(all)):min(t.scroll+room, len(all))]...)
}

func (t *PrivacyTab) planLines(w int) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(t.plan.Text, "\n"), "\n") {
		if isUndoHint(l) { // o comando manual de cada item cansa a leitura; o backup já restaura (tecla d)
			continue
		}
		out = append(out, wrapLine(" "+l, w)...)
	}
	return out
}

func (t *PrivacyTab) mainLines(w int) []string {
	var out []string
	if !t.info.IsAdmin {
		out = append(out, wrapLine(" "+t.theme.Warn().Render(SymWarn+" aviso:")+" este terminal não é administrador. O perfil recomendado só aplica num terminal aberto como administrador (botão direito, \"Executar como administrador\"); aqui, m aplica só os itens do usuário. O toolkit nunca eleva sozinho.", w)...)
	}
	head := "Privacidade do Windows"
	adm := "administrador: não"
	if t.info.Edition != "" {
		adm = "edição " + t.info.Edition + " · " + adm
	}
	if t.info.IsAdmin {
		adm = strings.Replace(adm, "administrador: não", "administrador: sim", 1)
	}
	out = append(out, " "+t.theme.Title().Render(titleCase(t.theme, head))+" · "+adm)
	profiles := []struct {
		id   PrivProfile
		desc string
	}{
		{PrivBasico, "só o que é do usuário · não exige administrador"},
		{PrivRecomendado, "básico + política, serviço e tarefas · exige administrador"},
	}
	for i, p := range profiles {
		cursor, name := " ", cell(string(p.id), 12)
		if i == t.cur {
			cursor, name = ">", t.theme.Bold().Render(name)
		}
		out = append(out, clip(" "+cursor+" "+name+"  "+p.desc, w))
	}
	if t.onlyUser {
		out = append(out, " "+t.theme.Accent().Render("[x]")+" aplicar só os itens do usuário (--somente-usuario): o que exige administrador fica de fora")
	}
	title := " Ajustes do perfil " + string(t.profileNow()) + " "
	out = append(out, SymRule+t.theme.Title().Render(title)+strings.Repeat(SymRule, max(w-lipgloss.Width(title)-1, 1)))
	shown := 0
	for _, s := range t.info.Settings {
		if t.cur == 0 && s.Admin {
			continue
		}
		shown++
		sym, word, style := t.settingState(s.State)
		adm := ""
		if s.Admin {
			adm = " [admin]"
		}
		out = append(out, clip(" "+style.Render(cell(sym+" "+word, 16))+" "+s.Title+adm, w))
	}
	if shown == 0 {
		out = append(out, " Nenhum ajuste para este perfil.")
	}
	return out
}

func (t *PrivacyTab) settingState(st string) (string, string, Paint) {
	switch st {
	case PrivApplied:
		return SymOK, "aplicado", t.theme.OK()
	case PrivPartial:
		return SymWarn, "parcial", t.theme.Warn()
	case PrivAbsent:
		return SymSkipped, "não se aplica", t.theme.Muted()
	}
	return SymPending, "pendente", Paint{}
}

// isUndoHint reconhece a linha "desfazer: <comando>" do plano (nas três línguas).
func isUndoHint(l string) bool {
	l = strings.TrimSpace(l)
	return strings.HasPrefix(l, "desfazer:") || strings.HasPrefix(l, "undo:") || strings.HasPrefix(l, "deshacer:")
}

// wrapLine quebra s em linhas de no máximo w colunas, nos espaços, mantendo o recuo da primeira.
func wrapLine(s string, w int) []string {
	if w < 10 || lipgloss.Width(s) <= w {
		return []string{s}
	}
	indent := s[:len(s)-len(strings.TrimLeft(s, " "))]
	var out []string
	cur := indent
	for _, word := range strings.Fields(s) {
		for lipgloss.Width(word) > w-len(indent)-2 { // palavra maior que a linha (caminho de registro): corta
			if room := w - lipgloss.Width(cur) - 1; strings.TrimSpace(cur) != "" && room >= 12 {
				head, rest := splitWidth(word, room)
				out = append(out, cur+" "+head)
				cur, word = indent+"  ", rest
				continue
			}
			if strings.TrimSpace(cur) != "" {
				out = append(out, cur)
				cur = indent + "  "
			}
			head, rest := splitWidth(word, max(w-len(indent)-2, 1))
			out = append(out, cur+head)
			cur, word = indent+"  ", rest
		}
		switch {
		case strings.TrimSpace(cur) == "":
			cur += word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = indent + "  " + word
		}
	}
	if strings.TrimSpace(cur) != "" {
		out = append(out, cur)
	}
	return out
}

// splitWidth separa s em um começo de no máximo w colunas e o resto.
func splitWidth(s string, w int) (string, string) {
	n := 0
	for i, r := range s {
		rw := lipgloss.Width(string(r))
		if n+rw > w {
			return s[:i], s[i:]
		}
		n += rw
	}
	return s, ""
}

// Detail implementa Detailer: o perfil sob o cursor (a aba não tem cursor por ajuste), só com o que ela já tem em memória:
// o que o perfil faz, quantos ajustes estão em cada estado, administrador e como desfazer. Sem painel enquanto carrega,
// com erro, fora do Windows, com o plano ou a confirmação na tela, ou durante a preparação/aplicação.
func (t *PrivacyTab) Detail(w, h int) string {
	if !t.usable() || t.asking || t.view || t.busy != busyNone {
		return ""
	}
	prof := t.profileNow()
	d := newDetail(t.theme, w)
	d.name("Perfil " + string(prof))

	d.section("O que faz")
	if prof == PrivBasico {
		d.para("Ajusta só o que é do usuário atual. Não exige administrador.")
	} else {
		d.para("Faz o básico e também ajusta política, serviço e tarefas da máquina. Exige administrador.")
	}
	d.para("Os nomes de cada chave, serviço e tarefa aparecem no plano (tecla p), antes de qualquer mudança.")

	var applied, pending, partial, absent, total int
	for _, s := range t.info.Settings {
		if prof == PrivBasico && s.Admin {
			continue
		}
		total++
		switch s.State {
		case PrivApplied:
			applied++
		case PrivPartial:
			partial++
		case PrivAbsent:
			absent++
		default:
			pending++
		}
	}
	d.section("Ajustes")
	if total == 0 {
		d.para("Nenhum ajuste para este perfil.")
	} else {
		d.para(plural(total, "ajuste", "ajustes") + ": " + strings.Join(t.settingCounts(applied, pending, partial, absent), ", "))
	}

	d.section("Administrador")
	switch {
	case prof == PrivBasico:
		d.para("Não exige administrador.")
	case t.info.IsAdmin:
		d.para("Exige administrador: este terminal é administrador.")
	default:
		d.para("Exige administrador: este terminal não é. Aqui só vale aplicar os itens do usuário (tecla m). O toolkit nunca eleva sozinho.")
	}
	if t.onlyUser {
		d.para("[x] aplica só os itens do usuário; o que exige administrador fica de fora")
	}

	d.section("Como desfaz")
	d.para("Antes de aplicar o toolkit guarda um backup dos valores originais. A tecla d restaura a partir dele, depois da confirmação.")
	return d.render(h)
}

// settingCounts escreve só as contagens que não são zero, na ordem aplicado, pendente, parcial, não se aplica.
func (t *PrivacyTab) settingCounts(applied, pending, partial, absent int) []string {
	var out []string
	if applied > 0 {
		out = append(out, plural(applied, "aplicado", "aplicados"))
	}
	if pending > 0 {
		out = append(out, plural(pending, "pendente", "pendentes"))
	}
	if partial > 0 {
		out = append(out, plural(partial, "parcial", "parciais"))
	}
	if absent > 0 {
		out = append(out, plural(absent, "não se aplica", "não se aplicam"))
	}
	return out
}
