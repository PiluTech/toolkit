package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Erros que o backend de apps nativos devolve para a aba mostrar a instrução certa.
var (
	// ErrNatNeedsAdmin: o plano exige administrador e o processo não é administrador (nada foi mudado).
	ErrNatNeedsAdmin = errors.New("apps nativos: exige administrador")
	// ErrNatStalePlan: a aplicação não corresponde ao plano que foi mostrado (o sistema mudou, a seleção é outra ou não há
	// plano preparado): nada foi alterado e é preciso ver o plano de novo.
	ErrNatStalePlan = errors.New("apps nativos: o plano mudou")
	// ErrNatNoBackup: não há backup, então não há o que reinstalar.
	ErrNatNoBackup = errors.New("apps nativos: sem backup")
)

// NatApp é um app do catálogo com o estado atual.
type NatApp struct {
	ID        string
	Title     string
	Basic     bool // do perfil básico
	Installed bool
}

// NatInfo é o que a aba carrega. Supported=false fora do Windows: a aba só avisa (Reason explica o porquê por SO).
type NatInfo struct {
	Supported bool
	OS        string
	Reason    string
	IsAdmin   bool
	Apps      []NatApp
}

// NatSelection é o que o usuário marcou: ids do catálogo e se vale para todos os usuários (exige administrador).
type NatSelection struct {
	Apps     []string
	AllUsers bool
}

// NatPlan é um plano pronto para mostrar (texto já formatado, na língua do usuário).
type NatPlan struct {
	ID         string // ID opaco do plano: a confirmação aplica SÓ o plano com esse ID
	Text       string
	Pending    int
	NeedsAdmin bool
	IsAdmin    bool
}

// NatResult é o que uma remoção fez.
type NatResult struct {
	Removed    int
	BackupPath string
}

// NativeAppsBackend liga a aba ao pacote appsnativos. Load, Plan e Reinstall só leem (Reinstall só devolve texto);
// Apply muda o sistema e só é chamado depois da confirmação [s/N] dentro da aba.
type NativeAppsBackend interface {
	Load(ctx context.Context) (NatInfo, error)
	Plan(ctx context.Context, sel NatSelection) (NatPlan, error)
	Apply(ctx context.Context, sel NatSelection, planID string) (NatResult, error)
	// Reinstall devolve o texto de "como reinstalar" lido do backup, sem executar nada.
	Reinstall(ctx context.Context) (string, error)
}

type natKind int

const (
	natView      natKind = iota // só ver o plano
	natRemove                   // plano seguido de confirmação
	natReinstall                // texto de como reinstalar
)

type natBusy int

const (
	natBusyNone natBusy = iota
	natBusyPlan
	natBusyApply
)

type natLoadedMsg struct {
	info NatInfo
	err  error
}

type natPlanMsg struct {
	seq  int
	kind natKind
	plan NatPlan
	err  error
}

type natDoneMsg struct {
	res NatResult
	err error
}

// NativeAppsTab é a aba Apps nativos (só Windows).
type NativeAppsTab struct {
	be    NativeAppsBackend
	ctx   context.Context
	theme Theme

	loading bool
	loadErr error
	info    NatInfo

	cur      int
	marked   map[string]bool
	allUsers bool
	lastW    int
	lastH    int
	scroll   int
	frame    int

	busy   natBusy
	seq    int
	cancel context.CancelFunc
	view   bool
	kind   natKind
	plan   NatPlan
	sel    NatSelection // seleção do plano mostrado
	asking bool
	notice string
	warn   bool
	result string
}

// NewNativeAppsTab cria a aba. O backend decide o que é "suportado" (no real, GOOS == windows).
func NewNativeAppsTab(ctx context.Context, be NativeAppsBackend, th Theme) *NativeAppsTab {
	return &NativeAppsTab{be: be, ctx: ctx, theme: th, loading: true, marked: map[string]bool{}}
}

// SetContext troca o contexto das chamadas ao backend.
func (t *NativeAppsTab) SetContext(ctx context.Context) { t.ctx = ctx }

// Title implementa Tab.
func (t *NativeAppsTab) Title() string { return "Apps nativos" }

// Short implementa Tab.
func (t *NativeAppsTab) Short() string { return "Nativos" }

// Init lê o estado (assíncrono: nunca bloqueia o Update).
func (t *NativeAppsTab) Init() tea.Cmd { return tea.Batch(t.loadCmd(), t.theme.Tick()) }

// Reload relê o estado depois de uma execução.
func (t *NativeAppsTab) Reload() tea.Cmd { return t.loadCmd() }

func (t *NativeAppsTab) loadCmd() tea.Cmd {
	be, ctx := t.be, t.ctx
	return func() tea.Msg {
		info, err := be.Load(ctx)
		return natLoadedMsg{info: info, err: err}
	}
}

// Capturing implementa Tab: durante a confirmação [s/N] e a remoção todas as teclas são da aba.
func (t *NativeAppsTab) Capturing() bool { return t.asking || t.busy == natBusyApply }

// Selected implementa Tab: quantos apps estão marcados.
func (t *NativeAppsTab) Selected() int { return 0 }

func (t *NativeAppsTab) usable() bool { return !t.loading && t.loadErr == nil && t.info.Supported }

func (t *NativeAppsTab) selection() NatSelection {
	var ids []string
	for _, a := range t.info.Apps {
		if t.marked[a.ID] {
			ids = append(ids, a.ID)
		}
	}
	return NatSelection{Apps: ids, AllUsers: t.allUsers}
}

// Update trata dados carregados, planos, resultados, o spinner e as teclas.
func (t *NativeAppsTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case natLoadedMsg:
		t.loading = false
		if msg.err != nil {
			t.loadErr = msg.err
			return t, nil
		}
		t.loadErr = nil
		t.info = cleanNatInfo(msg.info)
		t.cur = max(min(t.cur, len(t.info.Apps)-1), 0)
		if !t.info.IsAdmin {
			t.allUsers = false // sem administrador "todos os usuários" nunca fica ligado
		}
	case natPlanMsg:
		if msg.seq != t.seq || t.busy != natBusyPlan {
			return t, nil // plano atrasado (cancelado com Esc ou de uma tecla anterior)
		}
		t.busy = natBusyNone
		if t.cancel != nil {
			t.cancel()
			t.cancel = nil
		}
		t.onPlan(msg)
	case natDoneMsg:
		t.onDone(msg)
		return t, t.loadCmd()
	case tickMsg:
		t.frame++
		if t.loading || t.busy != natBusyNone {
			return t, t.theme.Tick()
		}
	case tea.KeyPressMsg:
		return t, t.key(msg.String())
	}
	return t, nil
}

func cleanNatInfo(i NatInfo) NatInfo {
	i.OS, i.Reason = textsafe.Line(i.OS), textsafe.Line(i.Reason)
	apps := make([]NatApp, len(i.Apps))
	for k, a := range i.Apps {
		apps[k] = NatApp{ID: textsafe.Line(a.ID), Title: textsafe.Line(a.Title), Basic: a.Basic, Installed: a.Installed}
	}
	i.Apps = apps
	return i
}

func (t *NativeAppsTab) setNotice(s string, warn bool) { t.notice, t.warn = textsafe.Line(s), warn }

func (t *NativeAppsTab) onPlan(m natPlanMsg) {
	t.scroll, t.asking = 0, false
	if m.err != nil {
		t.view = false
		if errors.Is(m.err, ErrNatNoBackup) {
			t.setNotice("Não há backup de apps nativos removidos: nada a reinstalar.", false)
		} else {
			t.setNotice("erro: "+m.err.Error(), true)
		}
		return
	}
	t.view, t.kind, t.plan = true, m.kind, m.plan
	t.plan.Text = textsafe.Multiline(m.plan.Text)
	switch {
	case m.kind == natView:
		t.setNotice("Só leitura: nada foi alterado. Esc volta.", false)
	case m.kind == natReinstall:
		t.setNotice("Só texto: o toolkit não reinstala nada. Esc volta.", false)
	case m.plan.Pending == 0:
		t.setNotice("Nada a remover: os apps marcados já estão ausentes.", false)
	case m.plan.NeedsAdmin && !m.plan.IsAdmin:
		t.setNotice("Este plano tem itens que exigem administrador e este terminal não é: nada foi alterado. Abra o terminal como administrador (botão direito, \"Executar como administrador\") e rode o toolkit de novo, ou volte (Esc) e desligue \"todos os usuários\" (u). O toolkit nunca eleva sozinho.", true)
	default:
		t.asking = true
		t.notice = ""
	}
}

func (t *NativeAppsTab) onDone(m natDoneMsg) {
	t.busy, t.asking, t.view, t.scroll = natBusyNone, false, false, 0
	switch {
	case errors.Is(m.err, ErrNatStalePlan):
		t.setNotice("O sistema ou a seleção mudou desde o plano mostrado: nada foi alterado. Veja o plano de novo (p ou r) antes de confirmar.", true)
		t.result = ""
	case errors.Is(m.err, ErrNatNeedsAdmin):
		t.setNotice("Precisa de administrador: nada foi alterado. Abra o terminal como administrador e tente de novo.", true)
		t.result = ""
	case m.err != nil:
		t.setNotice("erro: "+m.err.Error(), true)
		t.result = ""
		if m.res.Removed > 0 {
			t.result = fmt.Sprintf("Removidos antes da falha: %d app(s). Backup em %s. i mostra como reinstalar.", m.res.Removed, textsafe.Line(m.res.BackupPath))
		}
	default:
		t.marked = map[string]bool{}
		msg := fmt.Sprintf("Removidos: %d app(s). Backup em %s. Para reinstalar, tecle i.", m.res.Removed, textsafe.Line(m.res.BackupPath))
		t.setNotice(msg, false)
		t.result = msg
	}
}

// key trata as teclas. Mudar o sistema só acontece depois do plano na tela e do "s" (ou "y") na confirmação.
func (t *NativeAppsTab) key(k string) tea.Cmd {
	if t.busy == natBusyPlan && (k == "esc" || k == "escape") {
		// Esc cancela a PREPARAÇÃO do plano: invalida a resposta (seq) e o contexto; uma remoção já iniciada não é cancelável.
		t.seq++
		if t.cancel != nil {
			t.cancel()
			t.cancel = nil
		}
		t.busy, t.asking, t.view = natBusyNone, false, false
		t.setNotice("Preparação cancelada: nada foi alterado.", false)
		return nil
	}
	if t.loading || t.busy != natBusyNone || !t.usable() {
		return nil
	}
	if t.asking {
		switch k {
		case "up", "down", "pgup", "pgdown":
			t.scrollKey(k)
			return nil
		case "s", "S", "y", "Y":
			return t.confirm()
		}
		t.asking, t.view = false, false
		t.setNotice("Cancelado: nada foi alterado.", false)
		return nil
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
		t.cur = max(t.cur-1, 0)
	case "down", "j":
		t.cur = max(min(t.cur+1, len(t.info.Apps)-1), 0)
	case "pgup":
		t.cur = max(t.cur-max(t.lastH-4, 1), 0)
	case "pgdown":
		t.cur = max(min(t.cur+max(t.lastH-4, 1), len(t.info.Apps)-1), 0)
	case "space", " ":
		if t.cur < len(t.info.Apps) {
			id := t.info.Apps[t.cur].ID
			if t.marked[id] {
				delete(t.marked, id)
			} else {
				t.marked[id] = true
			}
		}
	case "b":
		t.markWhere(func(a NatApp) bool { return a.Basic })
	case "c":
		t.markWhere(func(NatApp) bool { return true })
	case "u":
		t.toggleAllUsers()
	case "p":
		return t.planCmd(natView)
	case "r":
		return t.planCmd(natRemove)
	case "i":
		return t.planCmd(natReinstall)
	}
	return nil
}

func (t *NativeAppsTab) markWhere(f func(NatApp) bool) {
	t.marked = map[string]bool{}
	for _, a := range t.info.Apps {
		if f(a) {
			t.marked[a.ID] = true
		}
	}
}

func (t *NativeAppsTab) toggleAllUsers() {
	switch {
	case t.allUsers:
		t.allUsers = false
	case !t.info.IsAdmin:
		t.setNotice("\"Todos os usuários\" exige administrador e este terminal não é: nada foi ligado. Abra o terminal como administrador (botão direito, \"Executar como administrador\") e rode o toolkit de novo. O toolkit nunca eleva sozinho.", true)
	default:
		t.allUsers = true
	}
}

func (t *NativeAppsTab) scrollKey(k string) {
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

func (t *NativeAppsTab) planCmd(kind natKind) tea.Cmd {
	sel := t.selection()
	if kind != natReinstall && len(sel.Apps) == 0 {
		t.setNotice("Marque ao menos um app (espaço, b ou c).", false)
		return nil
	}
	t.busy = natBusyPlan
	t.seq++
	pctx, cancel := context.WithCancel(t.ctx)
	t.cancel = cancel
	t.sel = sel
	seq, be := t.seq, t.be
	return tea.Batch(func() tea.Msg {
		var plan NatPlan
		var err error
		if kind == natReinstall {
			var text string
			text, err = be.Reinstall(pctx)
			plan = NatPlan{Text: text}
		} else {
			plan, err = be.Plan(pctx, sel)
		}
		return natPlanMsg{seq: seq, kind: kind, plan: plan, err: err}
	}, t.theme.Tick())
}

func (t *NativeAppsTab) confirm() tea.Cmd {
	t.asking, t.busy, t.notice = false, natBusyApply, ""
	be, ctx, sel, id := t.be, t.ctx, t.sel, t.plan.ID
	return tea.Batch(func() tea.Msg {
		res, err := be.Apply(ctx, sel, id)
		return natDoneMsg{res: res, err: err}
	}, t.theme.Tick())
}

// Keys implementa Tab.
func (t *NativeAppsTab) Keys() []KeyHint {
	switch {
	case !t.usable() || t.busy == natBusyApply:
		return nil
	case t.asking:
		return []KeyHint{{"s", "confirmar"}, {"n", "cancelar"}, {"↑↓", "rolar"}}
	case t.view:
		return []KeyHint{{"↑↓", "rolar"}, {"Esc", "voltar"}}
	}
	all := "todos os usuários"
	if t.allUsers {
		all += " ✓"
	}
	return []KeyHint{{"↑↓", "mover"}, {"espaço", "marcar"}, {"p", "ver plano"}, {"r", "remover"}, {"b", "básico"}, {"c", "completo"}, {"u", all}, {"i", "reinstalar"}}
}

// Status implementa Tab: a pergunta [s/N], o aviso ou o resultado.
func (t *NativeAppsTab) Status() string {
	switch {
	case t.asking:
		return fmt.Sprintf("Remover %d app(s)? O backup guarda a identidade de cada um, mas não há desfazer automático. [s/N]", t.plan.Pending)
	case t.busy == natBusyApply:
		return t.theme.Spin(t.frame) + " Removendo… não feche a janela."
	case t.notice != "" && t.warn:
		return SymWarn + " " + t.notice
	case t.notice != "":
		return t.notice
	}
	return ""
}

// More implementa Tab.
func (t *NativeAppsTab) More() int {
	if !t.usable() {
		return 0
	}
	if t.view {
		return max(len(t.planLines(t.lastW))-(t.scroll+t.lastH-1), 0)
	}
	return max(len(t.mainLines(t.lastW))-(t.scroll+t.lastH), 0)
}

// View desenha o corpo.
func (t *NativeAppsTab) View(w, h int) string {
	t.lastW, t.lastH = w, h
	switch {
	case t.loading:
		return " " + t.theme.Accent().Render(t.theme.Spin(t.frame)) + " Lendo os apps nativos…"
	case t.loadErr != nil:
		return " " + t.theme.Err().Render(SymFail+" erro:") + " não deu para ler os apps nativos (" + textsafe.Line(t.loadErr.Error()) + ")"
	case !t.info.Supported:
		lines := wrapLine(" Os apps nativos só existem no Windows (este sistema é "+t.info.OS+"). Nada a mudar aqui.", w)
		if t.info.Reason != "" {
			lines = append(lines, wrapLine(" "+t.info.Reason, w)...) // quebra a explicação em vez de cortar com reticências
		}
		return strings.Join(lines, "\n")
	}
	var lines []string
	if t.view {
		lines = t.fitPlan(w, h)
	} else {
		all := t.mainLines(w)
		hdr := len(all) - len(t.info.Apps)
		if row := hdr + t.cur; row < t.scroll {
			t.scroll = row
		} else if row >= t.scroll+h {
			t.scroll = row - h + 1
		}
		t.scroll = max(min(t.scroll, max(len(all)-h, 0)), 0)
		lines = all[min(t.scroll, len(all)):min(t.scroll+h, len(all))]
	}
	for i := range lines {
		lines[i] = clip(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

func (t *NativeAppsTab) fitPlan(w, h int) []string {
	all := t.planLines(w)
	room := max(h-1, 1)
	t.scroll = max(min(t.scroll, max(len(all)-room, 0)), 0)
	title := "Plano de remoção"
	if t.kind == natReinstall {
		title = "Como reinstalar (só texto, nada é executado)"
	} else if t.sel.AllUsers {
		title += " · todos os usuários"
	}
	head := " " + t.theme.Title().Render(titleCase(t.theme, title))
	return append([]string{head}, all[min(t.scroll, len(all)):min(t.scroll+room, len(all))]...)
}

func (t *NativeAppsTab) planLines(w int) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(t.plan.Text, "\n"), "\n") {
		out = append(out, wrapLine(" "+l, w)...)
	}
	return out
}

// mainLines desenha o cabeçalho e a lista; as últimas len(Apps) linhas são os apps (a View segue o cursor).
func (t *NativeAppsTab) mainLines(w int) []string {
	var out []string
	if !t.info.IsAdmin {
		out = append(out, wrapLine(" "+t.theme.Warn().Render(SymWarn+" aviso:")+" este terminal não é administrador. \"Todos os usuários\" (u) e o provisionamento só funcionam num terminal aberto como administrador (botão direito, \"Executar como administrador\"); aqui só se remove para o usuário atual. O toolkit nunca eleva sozinho.", w)...)
	}
	adm := "administrador: não"
	if t.info.IsAdmin {
		adm = "administrador: sim"
	}
	out = append(out, " "+t.theme.Title().Render(titleCase(t.theme, "Apps nativos do Windows"))+" · "+adm)
	if t.result != "" {
		out = append(out, " "+t.theme.OK().Render(SymOK+" "+t.result))
	}
	scope := "só o usuário atual"
	if t.allUsers {
		scope = t.theme.Accent().Render("[x]") + " todos os usuários e o provisionamento (exige administrador)"
	}
	out = append(out, " Escopo: "+scope+" · marcados: "+fmt.Sprint(len(t.marked)))
	if len(t.info.Apps) == 0 {
		return append(out, " Nenhum app no catálogo.")
	}
	for i, a := range t.info.Apps {
		cursor, box := " ", "[ ]"
		if i == t.cur {
			cursor = ">"
		}
		if t.marked[a.ID] {
			box = "[x]"
		}
		state, style := SymPending+" ausente", t.theme.Muted()
		if a.Installed {
			state, style = SymOK+" instalado", t.theme.OK()
		}
		profile := "completo"
		if a.Basic {
			profile = "basico"
		}
		title := a.Title
		if i == t.cur {
			title = t.theme.Bold().Render(title)
		}
		out = append(out, clip(" "+cursor+" "+box+" "+style.Render(cell(state, 12))+" "+cell(profile, 9)+" "+title, w))
	}
	return out
}
