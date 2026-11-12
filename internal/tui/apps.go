package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// AppInfo é um app do catálogo para o SO atual. NoInstaller marca o que não tem instalador aqui.
type AppInfo struct {
	ID, Name, Category string
	Managers           []string // gerenciadores disponíveis neste SO ("apt", "flatpak"...)
	Versions           bool     // aceita app@versão (lista de versões)
	NoInstaller        bool
}

// AppStatus é o estado de um app no sistema, lido do gerenciador (somente leitura).
type AppStatus struct {
	Installed bool
	Version   string
	// External: instalado FORA do gerenciador do catálogo (o .app do .dmg, o instalador do Windows). Installed
	// também fica true. ExternalManager é o nome do gerenciador esperado ("Homebrew") e ExternalWhere o lugar
	// ("/Applications/Discord.app").
	External        bool
	ExternalManager string
	ExternalWhere   string
	Err             error // não deu para verificar
}

// AppsBackend é o que a aba Apps precisa do resto do toolkit. A implementação real (catálogo + providers
// sobre o Runner) fica em internal/cli; os testes usam um falso.
type AppsBackend interface {
	Load(ctx context.Context) ([]AppInfo, error)
	Status(ctx context.Context, id string) AppStatus
	// Versions devolve as versões do app; "latest" nunca vem na lista (a aba o põe na frente).
	Versions(ctx context.Context, id string) ([]string, error)
}

// Mark é um app marcado para a Revisão (Version vazia = latest).
type Mark struct{ ID, Name, Version string }

type catalogLoadedMsg struct {
	apps []AppInfo
	err  error
}

type appStateMsg struct {
	id string
	st AppStatus
}

type versionsLoadedMsg struct {
	id       string
	versions []string
	err      error
}

type tickMsg struct{}

const (
	maxStatusWorkers = 4
	tickEvery        = 100 * time.Millisecond
)

var spinnerFrames = []string{"|", "/", "-", "\\"}

type appsMode int

const (
	modeList appsMode = iota
	modeSearch
	modeVersions
)

type appRow struct {
	header string // categoria (linha de título); vazio = app
	app    int    // índice em AppsTab.apps
}

// AppsTab é a aba Apps: catálogo agrupado por categoria, busca, multi-seleção e lista de versões.
type AppsTab struct {
	be    AppsBackend
	ctx   context.Context
	theme Theme
	goos  string

	apps    []AppInfo
	loading bool
	loadErr error

	status  map[string]AppStatus
	pending map[string]bool // em verificação
	queue   []string        // ids ainda não pedidos
	marks   map[string]string
	order   []string // ids na ordem em que foram marcados

	filter string
	mode   appsMode

	rows   []appRow
	cur    int // índice em rows (sempre uma linha de app)
	top    int
	lastH  int
	lastW  int
	frame  int
	notice string // aviso da linha de estado ("sem instalador para este sistema")

	ver versionsView
}

type versionsView struct {
	id       string
	loading  bool
	err      error
	versions []string // "latest" + as do gerenciador
	cur, top int
}

// NewAppsTab cria a aba. goos só aparece nos textos ("Linux", "Windows", "macOS").
func NewAppsTab(ctx context.Context, be AppsBackend, th Theme, goos string) *AppsTab {
	return &AppsTab{
		be: be, ctx: ctx, theme: th, goos: goos, loading: true,
		status: map[string]AppStatus{}, pending: map[string]bool{}, marks: map[string]string{},
	}
}

// Marked devolve os apps marcados, na ordem em que foram marcados.
func (t *AppsTab) Marked() []Mark {
	out := make([]Mark, 0, len(t.order))
	for _, id := range t.order {
		if v, ok := t.marks[id]; ok {
			out = append(out, Mark{ID: id, Name: t.appByID(id).Name, Version: v})
		}
	}
	return out
}

// SetContext troca o contexto das chamadas ao backend (o Run entrega o que ele cancela ao sair).
func (t *AppsTab) SetContext(ctx context.Context) { t.ctx = ctx }

// Init carrega o catálogo e liga o spinner.
func (t *AppsTab) Init() tea.Cmd {
	return tea.Batch(t.loadCmd(), tick())
}

func tick() tea.Cmd { return tea.Tick(tickEvery, func(time.Time) tea.Msg { return tickMsg{} }) }

func (t *AppsTab) loadCmd() tea.Cmd {
	be, ctx := t.be, t.ctx
	return func() tea.Msg {
		apps, err := be.Load(ctx)
		return catalogLoadedMsg{apps: apps, err: err}
	}
}

// Reload relê o estado de todos os apps (depois de uma execução): zera o que já foi lido e refaz a fila.
func (t *AppsTab) Reload() tea.Cmd {
	t.status = map[string]AppStatus{}
	t.queue = t.queue[:0]
	for _, a := range t.apps {
		if !a.NoInstaller {
			t.queue = append(t.queue, a.ID)
		}
	}
	t.prioritizeVisible()
	return tea.Batch(t.pump(), tick())
}

// Title implementa Tab.
func (t *AppsTab) Title() string { return "Apps" }

// Short implementa Tab.
func (t *AppsTab) Short() string { return "Apps" }

// Capturing diz que a busca e a lista de versões recebem todas as teclas.
func (t *AppsTab) Capturing() bool { return t.mode != modeList }

// Selected implementa Tab.
func (t *AppsTab) Selected() int { return len(t.marks) }

func (t *AppsTab) busy() bool {
	return t.loading || len(t.pending) > 0 || t.ver.loading
}

// Update trata dados carregados, o spinner e as teclas da aba.
func (t *AppsTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case catalogLoadedMsg:
		t.loading = false
		if msg.err != nil {
			t.loadErr = msg.err
			return t, nil
		}
		t.apps = make([]AppInfo, 0, len(msg.apps))
		for _, a := range msg.apps {
			t.apps = append(t.apps, cleanApp(a))
		}
		t.rebuild("")
		for _, a := range t.apps {
			if !a.NoInstaller {
				t.queue = append(t.queue, a.ID)
			}
		}
		t.prioritizeVisible()
		return t, t.pump()
	case appStateMsg:
		delete(t.pending, msg.id)
		st := msg.st
		st.Version = textsafe.Line(st.Version)
		st.ExternalManager = textsafe.Line(st.ExternalManager)
		st.ExternalWhere = textsafe.Line(st.ExternalWhere)
		if st.Err != nil {
			st.Err = fmt.Errorf("%s", textsafe.Line(st.Err.Error()))
		}
		t.status[msg.id] = st
		return t, t.pump()
	case versionsLoadedMsg:
		if msg.id != t.ver.id || t.mode != modeVersions {
			return t, nil
		}
		t.ver.loading = false
		if msg.err != nil {
			t.ver.err = fmt.Errorf("%s", textsafe.Line(msg.err.Error()))
			return t, nil
		}
		// o destaque segue o MESMO item (por valor) quando a lista nova chega: a lista de antes tinha só
		// latest e a versão já marcada, então o índice não vale na nova. Sumiu da lista = volta a latest.
		sel := t.ver.highlighted()
		list := []string{"latest"}
		for _, v := range msg.versions {
			if v = textsafe.Line(v); v != "" && v != "latest" {
				list = append(list, v)
			}
		}
		t.ver.versions = list
		t.ver.cur = max(slices.Index(list, sel), 0)
		t.ver.top = 0
		return t, nil
	case tickMsg:
		t.frame++
		if t.busy() {
			return t, tick()
		}
		return t, nil
	case tea.KeyPressMsg:
		switch t.mode {
		case modeSearch:
			return t, t.keySearch(msg)
		case modeVersions:
			return t, t.keyVersions(msg)
		}
		return t, t.keyList(msg)
	}
	return t, nil
}

func cleanApp(a AppInfo) AppInfo {
	a.ID, a.Name, a.Category = textsafe.Line(a.ID), textsafe.Line(a.Name), textsafe.Line(a.Category)
	m := make([]string, len(a.Managers))
	for i, x := range a.Managers {
		m[i] = textsafe.Line(x)
	}
	a.Managers = m
	return a
}

func (t *AppsTab) appByID(id string) AppInfo {
	for _, a := range t.apps {
		if a.ID == id {
			return a
		}
	}
	return AppInfo{ID: id, Name: id}
}

// pump mantém até maxStatusWorkers verificações em andamento (cada uma pode ser um `winget list`).
func (t *AppsTab) pump() tea.Cmd {
	var cmds []tea.Cmd
	for len(t.pending) < maxStatusWorkers && len(t.queue) > 0 {
		id := t.queue[0]
		t.queue = t.queue[1:]
		if _, done := t.status[id]; done || t.pending[id] {
			continue
		}
		t.pending[id] = true
		be, ctx := t.be, t.ctx
		cmds = append(cmds, func() tea.Msg { return appStateMsg{id: id, st: be.Status(ctx, id)} })
	}
	if len(cmds) > 0 && len(t.pending) == len(cmds) {
		cmds = append(cmds, tick()) // religa o spinner se estava parado
	}
	return tea.Batch(cmds...)
}

// prioritizeVisible põe na frente da fila o que está na tela (2000 apps: o estado do que se vê vem primeiro).
func (t *AppsTab) prioritizeVisible() {
	h := t.lastH
	if h <= 0 {
		h = 20
	}
	var front []string
	for i := t.top; i < len(t.rows) && i < t.top+h+h; i++ {
		if r := t.rows[i]; r.header == "" {
			id := t.apps[r.app].ID
			if _, done := t.status[id]; !done && !t.pending[id] && slices.Contains(t.queue, id) {
				front = append(front, id)
			}
		}
	}
	if len(front) == 0 {
		return
	}
	rest := t.queue[:0:0]
	for _, id := range t.queue {
		if !slices.Contains(front, id) {
			rest = append(rest, id)
		}
	}
	t.queue = slices.Concat(front, rest)
}

// rebuild reconstrói as linhas (categorias + apps filtrados) e mantém o cursor no app de antes, se ainda existir.
func (t *AppsTab) rebuild(keepID string) {
	if keepID == "" && t.cur < len(t.rows) && t.rows[t.cur].header == "" {
		keepID = t.apps[t.rows[t.cur].app].ID
	}
	f := strings.ToLower(t.filter)
	cats := map[string][]int{}
	var order []string
	for i, a := range t.apps {
		if f != "" && !strings.Contains(strings.ToLower(a.Name), f) && !strings.Contains(strings.ToLower(a.ID), f) {
			continue
		}
		if _, ok := cats[a.Category]; !ok {
			order = append(order, a.Category)
		}
		cats[a.Category] = append(cats[a.Category], i)
	}
	slices.Sort(order)
	t.rows = t.rows[:0]
	for _, c := range order {
		t.rows = append(t.rows, appRow{header: c})
		for _, i := range cats[c] {
			t.rows = append(t.rows, appRow{app: i})
		}
	}
	t.cur = 0
	for i, r := range t.rows {
		if r.header == "" && (keepID == "" || t.apps[r.app].ID == keepID) {
			t.cur = i
			break
		}
	}
	t.fixTop()
}

func (t *AppsTab) matches() int {
	n := 0
	for _, r := range t.rows {
		if r.header == "" {
			n++
		}
	}
	return n
}

func (t *AppsTab) current() (AppInfo, bool) {
	if t.cur < 0 || t.cur >= len(t.rows) || t.rows[t.cur].header != "" {
		return AppInfo{}, false
	}
	return t.apps[t.rows[t.cur].app], true
}

// move anda n apps (pulando títulos de categoria); n < 0 sobe.
func (t *AppsTab) move(n int) {
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	for ; n > 0; n-- {
		i := t.cur + step
		for i >= 0 && i < len(t.rows) && t.rows[i].header != "" {
			i += step
		}
		if i < 0 || i >= len(t.rows) {
			break
		}
		t.cur = i
	}
	t.fixTop()
	t.prioritizeVisible()
}

func (t *AppsTab) edge(first bool) {
	idx := -1
	for i, r := range t.rows {
		if r.header == "" {
			idx = i
			if first {
				break
			}
		}
	}
	if idx >= 0 {
		t.cur = idx
	}
	t.fixTop()
	t.prioritizeVisible()
}

// bodyRows é quantas linhas de lista cabem no corpo (a busca ocupa uma quando ativa).
func (t *AppsTab) bodyRows() int {
	h := t.lastH
	if t.mode == modeSearch || t.filter != "" {
		h--
	}
	return max(h, 1)
}

// fixTop mantém o cursor visível; um título de categoria logo acima do cursor sobe junto.
func (t *AppsTab) fixTop() {
	n := t.bodyRows()
	if t.cur < t.top {
		t.top = t.cur
		if t.top > 0 && t.rows[t.top-1].header != "" {
			t.top--
		}
	}
	if t.cur >= t.top+n {
		t.top = t.cur - n + 1
	}
	t.top = max(min(t.top, max(len(t.rows)-1, 0)), 0)
}

func (t *AppsTab) keyList(msg tea.KeyPressMsg) tea.Cmd {
	t.notice = ""
	switch msg.String() {
	case "up", "k":
		t.move(-1)
	case "down", "j":
		t.move(1)
	case "pgup":
		t.move(-t.bodyRows())
	case "pgdown":
		t.move(t.bodyRows())
	case "g", "home":
		t.edge(true)
	case "G", "end":
		t.edge(false)
	case "/":
		t.mode = modeSearch
		t.fixTop()
	case "esc":
		if t.filter != "" {
			t.filter = ""
			t.rebuild("")
		}
	case "space":
		return t.toggle()
	case "v":
		return t.openVersions(true)
	}
	return nil
}

// toggle marca/desmarca o app sob o cursor. App com versões abre a lista (já em latest): um Enter confirma.
func (t *AppsTab) toggle() tea.Cmd {
	a, ok := t.current()
	if !ok {
		return nil
	}
	if a.NoInstaller {
		t.notice = "sem instalador para " + osName(t.goos) + ": este app não pode ser marcado"
		return nil
	}
	if _, marked := t.marks[a.ID]; marked {
		delete(t.marks, a.ID)
		t.order = slices.DeleteFunc(t.order, func(x string) bool { return x == a.ID })
		return nil
	}
	if a.Versions {
		return t.openVersions(false)
	}
	t.marks[a.ID] = ""
	t.order = append(t.order, a.ID)
	return nil
}

func (t *AppsTab) openVersions(explicit bool) tea.Cmd {
	a, ok := t.current()
	if !ok || a.NoInstaller {
		return nil
	}
	if !a.Versions {
		if explicit {
			t.notice = "este app não tem lista de versões"
		}
		return nil
	}
	t.mode = modeVersions
	t.ver = versionsView{id: a.ID, loading: true, versions: []string{"latest"}}
	if v, ok := t.marks[a.ID]; ok && v != "" {
		t.ver.versions = append(t.ver.versions, v) // já mostra a escolhida enquanto a lista chega
		t.ver.cur = 1
	}
	be, ctx, id := t.be, t.ctx, a.ID
	return tea.Batch(func() tea.Msg {
		v, err := be.Versions(ctx, id)
		return versionsLoadedMsg{id: id, versions: v, err: err}
	}, tick())
}

// highlighted devolve a versão destacada ("" se a lista estiver vazia ou o cursor fora dela).
func (v versionsView) highlighted() string {
	if v.cur < 0 || v.cur >= len(v.versions) {
		return ""
	}
	return v.versions[v.cur]
}

// clamp prende o cursor a [0, len): a lista pode encolher e nunca pode ficar vazia para o Enter.
func (v *versionsView) clamp() {
	if len(v.versions) == 0 {
		v.versions = []string{"latest"}
	}
	v.cur = max(min(v.cur, len(v.versions)-1), 0)
}

func (t *AppsTab) keyVersions(msg tea.KeyPressMsg) tea.Cmd {
	t.ver.clamp()
	n := len(t.ver.versions)
	page := max(t.lastH-4, 1)
	switch msg.String() {
	case "esc":
		t.mode = modeList
	case "up", "k":
		t.ver.cur = max(t.ver.cur-1, 0)
	case "down", "j":
		t.ver.cur = min(t.ver.cur+1, n-1)
	case "pgup":
		t.ver.cur = max(t.ver.cur-page, 0)
	case "pgdown":
		t.ver.cur = min(t.ver.cur+page, n-1)
	case "g", "home":
		t.ver.cur = 0
	case "G", "end":
		t.ver.cur = n - 1
	case "enter", "space":
		v := t.ver.highlighted()
		if v == "latest" {
			v = ""
		}
		id := t.ver.id
		if _, had := t.marks[id]; !had {
			t.order = append(t.order, id)
		}
		t.marks[id] = v
		t.mode = modeList
	}
	t.ver.clamp()
	return nil
}

func (t *AppsTab) keySearch(msg tea.KeyPressMsg) tea.Cmd {
	switch k := msg.String(); k {
	case "esc":
		t.filter, t.mode = "", modeList
		t.rebuild("")
	case "enter":
		t.mode = modeList
		t.fixTop()
	case "up":
		t.move(-1)
	case "down":
		t.move(1)
	case "backspace":
		if r := []rune(t.filter); len(r) > 0 {
			t.filter = string(r[:len(r)-1])
			t.rebuild("")
			t.prioritizeVisible()
		}
	default:
		if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			t.filter += textsafe.Line(msg.Text)
			t.rebuild("")
			t.prioritizeVisible()
			return t.pump()
		}
	}
	return nil
}

// Keys implementa Tab (barra de teclas contextual).
func (t *AppsTab) Keys() []KeyHint {
	switch t.mode {
	case modeSearch:
		return []KeyHint{{"Enter", "fixar"}, {"Esc", "limpar"}, {"↑↓", "mover"}}
	case modeVersions:
		return []KeyHint{{"↑↓", "mover"}, {"Enter", "escolher"}, {"Esc", "cancelar"}}
	}
	return []KeyHint{{"↑↓", "mover"}, {"espaço", "marcar"}, {"/", "buscar"}, {"Enter", "revisar"}, {"v", "versões"}}
}

// Status implementa Tab: o item sob o cursor (id, nome completo, gerenciadores) ou o aviso do momento.
func (t *AppsTab) Status() string {
	if t.notice != "" {
		return t.notice
	}
	if t.mode == modeVersions {
		a := t.appByID(t.ver.id)
		return "versões de " + a.Name
	}
	a, ok := t.current()
	if !ok {
		return ""
	}
	s := a.ID + " — " + a.Name
	if len(a.Managers) > 0 {
		s += " (" + strings.Join(a.Managers, ", ") + ")"
	}
	if st, ok := t.status[a.ID]; ok && st.Err != nil {
		s += " — não deu para verificar: " + st.Err.Error()
	}
	return s
}

// More implementa Tab: linhas da lista abaixo da dobra.
func (t *AppsTab) More() int {
	if t.mode == modeVersions || t.loading || t.loadErr != nil {
		return 0
	}
	return max(len(t.rows)-(t.top+t.bodyRows()), 0)
}

func osName(goos string) string {
	switch goos {
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	}
	return goos
}

func (t *AppsTab) spin() string { return spinnerFrames[t.frame%len(spinnerFrames)] }

// View desenha o corpo (h linhas, no máximo w colunas).
func (t *AppsTab) View(w, h int) string {
	t.lastW, t.lastH = w, h
	if t.mode == modeVersions {
		return t.versionsBody(w, h)
	}
	switch {
	case t.loading:
		return " " + t.theme.Accent().Render(t.spin()) + " Lendo o catálogo…"
	case t.loadErr != nil:
		return t.errorBody()
	case len(t.apps) == 0:
		return " Nenhum app do catálogo tem instalador para " + osName(t.goos) + ".\n Veja: toolkit list --os"
	}
	t.fixTop()
	var lines []string
	if t.mode == modeSearch || t.filter != "" {
		lines = append(lines, t.searchBar())
	}
	if t.matches() == 0 {
		lines = append(lines, " Nada com \""+textsafe.Line(t.filter)+"\". Esc limpa a busca.")
		return strings.Join(lines, "\n")
	}
	cols := t.columns(w)
	n := t.bodyRows()
	for i := t.top; i < len(t.rows) && i < t.top+n; i++ {
		lines = append(lines, t.rowLine(i, cols))
	}
	return strings.Join(lines, "\n")
}

func (t *AppsTab) searchBar() string {
	cursor := ""
	if t.mode == modeSearch {
		cursor = "▌"
	}
	right := ""
	if t.filter != "" {
		right = fmt.Sprintf("  %d de %d apps", t.matches(), len(t.apps))
	}
	return " " + t.theme.Accent().Render("/") + " " + textsafe.Line(t.filter) + cursor + t.theme.Muted().Render(right)
}

func (t *AppsTab) errorBody() string {
	var b strings.Builder
	b.WriteString(" " + t.theme.Err().Render(SymFail+" erro:") + " não deu para ler o catálogo\n")
	for _, l := range strings.Split(textsafe.Printable(t.loadErr.Error()), `\x0a`) {
		b.WriteString(" " + l + "\n")
	}
	b.WriteString("\n Corrija o arquivo ou rode sem --catalog.")
	return b.String()
}

// cols são as larguras das colunas, por largura da janela (docs/tui.md): >= 80 completo; 70 a 79 sem VIA;
// 60 a 69 sem VIA e sem a coluna VERSÃO (a versão vai junto do estado).
type cols struct{ name, state, ver, via int }

func (t *AppsTab) columns(w int) cols {
	c := cols{state: 24, ver: 10, via: 7}
	switch {
	case w < 70:
		c.via, c.ver, c.state = 0, 0, 22
	case w < 80:
		c.via = 0
	case w >= 100:
		c.state = 32 // cabe "✓ instalado (fora do Homebrew)"
	}
	// margem 1 + cursor 1 + espaço + marca 3 + espaço = 7; 2 espaços entre colunas
	fixed := 7
	gaps := 0
	for _, x := range []int{c.state, c.ver, c.via} {
		if x > 0 {
			fixed += x
			gaps += 2
		}
	}
	c.name = max(w-fixed-gaps, 12)
	return c
}

func (t *AppsTab) rowLine(i int, c cols) string {
	r := t.rows[i]
	if r.header != "" {
		return " " + t.theme.Title().Render(titleCase(Theme{Mono: true}, r.header)) // categoria em MAIÚSCULAS
	}
	a := t.apps[r.app]
	cursor := " "
	if i == t.cur {
		cursor = ">"
	}
	mark := "[ ]"
	if _, ok := t.marks[a.ID]; ok {
		mark = t.theme.Accent().Render("[x]")
	}
	name := cell(a.Name, c.name)
	if a.Versions && c.name > 4 { // ▾ = tem lista de versões (tecla v)
		name = cell(a.Name, c.name-2) + " ▾"
	}
	if i == t.cur {
		name = t.theme.Bold().Render(name)
	}
	state, ver := t.stateText(a, c.state)
	if c.ver == 0 && ver != "" { // 60 a 69 colunas: a versão junto do estado
		state += " " + ver
		ver = ""
	}
	if v, ok := t.marks[a.ID]; ok && v != "" {
		ver = "→ " + v
		if c.ver == 0 {
			state = strings.TrimSuffix(state, " ") + " " + ver
			ver = ""
		}
	}
	line := " " + cursor + " " + mark + " " + name + "  " + t.styleState(a, cell(state, c.state))
	if c.ver > 0 {
		line += "  " + t.theme.Muted().Render(cell(ver, c.ver))
	}
	if c.via > 0 {
		via := ""
		if len(a.Managers) > 0 {
			via = a.Managers[0]
		}
		line += "  " + t.theme.Muted().Render(cell(via, c.via))
	}
	return line
}

// Texto do estado "instalado fora do gerenciador": completo quando a coluna comporta, curto senão.
const (
	stateExternalFull  = "instalado (fora do %s)"
	stateExternalShort = "instalado (fora)"
)

// externalText escolhe o texto de "instalado fora do gerenciador" que cabe em width colunas (com o símbolo).
func externalText(st AppStatus, width int) string {
	if st.ExternalManager != "" {
		if full := SymOK + " " + fmt.Sprintf(stateExternalFull, st.ExternalManager); lipgloss.Width(full) <= width {
			return full
		}
	}
	return SymOK + " " + stateExternalShort
}

// stateText devolve o estado em símbolo + palavra, e a versão instalada. width é a largura da coluna de estado.
func (t *AppsTab) stateText(a AppInfo, width int) (state, ver string) {
	if a.NoInstaller {
		return SymSkipped + " sem instalador", ""
	}
	if t.pending[a.ID] {
		return t.spin() + " verificando", ""
	}
	st, ok := t.status[a.ID]
	switch {
	case !ok:
		return SymPending + " na fila", ""
	case st.Err != nil:
		return SymFail + " não deu para verificar", ""
	case st.Installed && st.External:
		return externalText(st, width), st.Version
	case st.Installed:
		return SymOK + " instalado", st.Version
	}
	return SymPending + " não instalado", ""
}

// styleState põe cor SÓ no símbolo e na palavra de estado (nunca na linha toda).
func (t *AppsTab) styleState(a AppInfo, s string) string {
	st := t.status[a.ID]
	switch {
	case a.NoInstaller:
		return t.theme.Muted().Render(s)
	case st.Err != nil:
		return t.theme.Err().Render(s)
	case st.Installed:
		return t.theme.OK().Render(s)
	case t.pending[a.ID]:
		return t.theme.Accent().Render(s)
	}
	return s
}

// cell ajusta s a exatamente w colunas: corta com … ou completa com espaços.
func cell(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if d := w - lipgloss.Width(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// versionsBody é o modal da lista de versões (troca o corpo; cabeçalho e teclas continuam).
func (t *AppsTab) versionsBody(w, h int) string {
	t.ver.clamp()
	a := t.appByID(t.ver.id)
	var b strings.Builder
	b.WriteString(" " + t.theme.Title().Render(titleCase(t.theme, "Versões de "+a.Name)) + "\n")
	switch {
	case t.ver.err != nil:
		b.WriteString(" " + t.theme.Err().Render(SymFail+" erro:") + " não deu para listar as versões (" + t.ver.err.Error() + "). latest continua valendo.\n")
	case t.ver.loading:
		b.WriteString(" " + t.theme.Accent().Render(t.spin()) + " Consultando " + joinManagers(a) + "…\n")
	case len(t.ver.versions) == 1:
		b.WriteString(" O gerenciador não informou outras versões.\n")
	default:
		b.WriteString("\n")
	}
	n := max(h-3, 1)
	if t.ver.cur < t.ver.top {
		t.ver.top = t.ver.cur
	}
	if t.ver.cur >= t.ver.top+n {
		t.ver.top = t.ver.cur - n + 1
	}
	for i := t.ver.top; i < len(t.ver.versions) && i < t.ver.top+n; i++ {
		cursor, radio := " ", "( )"
		if i == t.ver.cur {
			cursor, radio = ">", "(•)"
		}
		b.WriteString(" " + cursor + " " + radio + " " + cell(t.ver.versions[i], max(w-8, 1)) + "\n")
	}
	if rest := len(t.ver.versions) - (t.ver.top + n); rest > 0 {
		b.WriteString(" " + t.theme.Muted().Render(SymMore+" mais "+plural(rest, "versão", "versões")) + "\n")
	}
	return b.String()
}

func joinManagers(a AppInfo) string {
	if len(a.Managers) == 0 {
		return "o gerenciador"
	}
	return strings.Join(a.Managers, ", ")
}
