package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// StepInfo é uma etapa de receita.
type StepInfo struct {
	ID, Name string
	Reboot   string // "never", "required" ou "if-needed"
}

// RecipeInfo é uma receita do SO atual. Diagnostic separa as abas Receitas e Diagnóstico.
type RecipeInfo struct {
	ID, Name, Description string
	Diagnostic            bool
	RequiresAdmin         bool
	Steps                 []StepInfo
}

// StepRun é o último andamento de uma etapa, lido do state.
type StepRun struct {
	Status string // "pending", "running", "done", "skipped", "failed", "awaiting-reboot"
	Error  string
}

// RecipeRun é o último andamento de uma receita, lido do state.
type RecipeRun struct {
	Status string // "running", "awaiting-reboot", "done", "failed"
	Steps  map[string]StepRun
}

// RecipesData é o que a aba carrega de uma vez. StateErr é falha só do state (as receitas continuam).
type RecipesData struct {
	Recipes   []RecipeInfo
	Runs      map[string]RecipeRun
	StateErr  error
	StatePath string
}

// RecipesBackend lê as receitas e o state (somente leitura; a execução é da Revisão).
type RecipesBackend interface {
	Load(ctx context.Context) (RecipesData, error)
}

// RecipeMark é uma receita marcada para a Revisão.
type RecipeMark struct {
	ID, Name   string
	Diagnostic bool
	// Details: no Diagnóstico, mostra também a saída original (crua) de cada etapa (tecla e).
	Details bool
}

type recipesLoadedMsg struct {
	data RecipesData
	err  error
}

// RecipesTab serve as abas Receitas e Diagnóstico (a mesma tela, filtrada por Diagnostic).
type RecipesTab struct {
	be         RecipesBackend
	ctx        context.Context
	diagnostic bool
	details    bool // aba Diagnóstico: mostra também a saída original (tecla d)
	theme      Theme

	loading bool
	loadErr error
	data    RecipesData
	items   []RecipeInfo // do tipo desta aba, já limpos

	marks  map[string]bool
	order  []string
	filter string
	search bool

	cur, top  int  // lista
	focus     bool // foco nas etapas
	scur, stp int  // etapas
	lastW     int
	lastH     int
	frame     int
	notice    string
}

// NewRecipesTab cria a aba Receitas (diagnostic=false) ou Diagnóstico (diagnostic=true).
func NewRecipesTab(ctx context.Context, be RecipesBackend, th Theme, diagnostic bool) *RecipesTab {
	return &RecipesTab{be: be, ctx: ctx, diagnostic: diagnostic, theme: th, loading: true, marks: map[string]bool{}}
}

// SetContext troca o contexto das chamadas ao backend (o Run cancela o dele ao sair).
func (t *RecipesTab) SetContext(ctx context.Context) { t.ctx = ctx }

// Reload relê as receitas e o andamento (depois de uma execução).
func (t *RecipesTab) Reload() tea.Cmd {
	t.loading = true
	return t.Init()
}

// Title implementa Tab.
func (t *RecipesTab) Title() string {
	if t.diagnostic {
		return "Diagnóstico"
	}
	return "Receitas"
}

// Short implementa Tab.
func (t *RecipesTab) Short() string {
	if t.diagnostic {
		return "Diag"
	}
	return "Rec"
}

// Init carrega as receitas e o state.
func (t *RecipesTab) Init() tea.Cmd {
	be, ctx := t.be, t.ctx
	return tea.Batch(func() tea.Msg {
		d, err := be.Load(ctx)
		return recipesLoadedMsg{data: d, err: err}
	}, t.theme.Tick())
}

// Capturing diz que só a busca recebe todas as teclas.
func (t *RecipesTab) Capturing() bool { return t.search }

// Selected implementa Tab.
func (t *RecipesTab) Selected() int { return len(t.marks) }

// Marked devolve as receitas marcadas, na ordem em que foram marcadas.
func (t *RecipesTab) Marked() []RecipeMark {
	var out []RecipeMark
	for _, id := range t.order {
		if t.marks[id] {
			r := t.byID(id)
			out = append(out, RecipeMark{ID: id, Name: r.Name, Diagnostic: r.Diagnostic, Details: t.details && r.Diagnostic})
		}
	}
	return out
}

func (t *RecipesTab) byID(id string) RecipeInfo {
	for _, r := range t.items {
		if r.ID == id {
			return r
		}
	}
	return RecipeInfo{ID: id, Name: id}
}

// visible é a lista filtrada pela busca.
func (t *RecipesTab) visible() []RecipeInfo {
	if t.filter == "" {
		return t.items
	}
	f := strings.ToLower(t.filter)
	var out []RecipeInfo
	for _, r := range t.items {
		if strings.Contains(strings.ToLower(r.Name), f) || strings.Contains(strings.ToLower(r.ID), f) {
			out = append(out, r)
		}
	}
	return out
}

func (t *RecipesTab) current() (RecipeInfo, bool) {
	v := t.visible()
	if t.cur < 0 || t.cur >= len(v) {
		return RecipeInfo{}, false
	}
	return v[t.cur], true
}

// Update trata dados carregados, o spinner e as teclas.
func (t *RecipesTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case recipesLoadedMsg:
		t.loading = false
		if msg.err != nil {
			t.loadErr = msg.err
			return t, nil
		}
		t.data = msg.data
		t.items = t.items[:0]
		for _, r := range msg.data.Recipes {
			if r.Diagnostic == t.diagnostic {
				t.items = append(t.items, cleanRecipe(r))
			}
		}
		t.clamp()
	case tickMsg:
		t.frame++
		if t.loading {
			return t, t.theme.Tick()
		}
	case tea.KeyPressMsg:
		return t, t.key(msg)
	}
	return t, nil
}

func cleanRecipe(r RecipeInfo) RecipeInfo {
	r.ID, r.Name, r.Description = textsafe.Line(r.ID), textsafe.Line(r.Name), textsafe.Line(r.Description)
	steps := make([]StepInfo, len(r.Steps))
	for i, s := range r.Steps {
		steps[i] = StepInfo{ID: textsafe.Line(s.ID), Name: textsafe.Line(s.Name), Reboot: s.Reboot}
	}
	r.Steps = steps
	return r
}

func (t *RecipesTab) clamp() {
	n := len(t.visible())
	t.cur = max(min(t.cur, n-1), 0)
	if r, ok := t.current(); ok {
		t.scur = max(min(t.scur, len(r.Steps)-1), 0)
	} else {
		t.scur = 0
	}
	t.fix()
}

func (t *RecipesTab) listRows() int {
	n := len(t.visible())
	return max(min(n, 6), 1)
}

func (t *RecipesTab) fix() {
	n := t.listRows()
	if t.cur < t.top {
		t.top = t.cur
	}
	if t.cur >= t.top+n {
		t.top = t.cur - n + 1
	}
	t.top = max(t.top, 0)
}

func (t *RecipesTab) key(msg tea.KeyPressMsg) tea.Cmd {
	t.notice = ""
	if t.search {
		return t.keySearch(msg)
	}
	r, has := t.current()
	switch msg.String() {
	case "up", "k":
		t.step(-1)
	case "down", "j":
		t.step(1)
	case "pgup":
		t.step(-t.panelRows())
	case "pgdown":
		t.step(t.panelRows())
	case "g", "home":
		t.step(-1 << 20)
	case "G", "end":
		t.step(1 << 20)
	case "right", "l":
		if has && len(r.Steps) > 0 {
			t.focus = true
		}
	case "left", "h":
		t.focus = false
	case "/":
		t.search = true
	case "esc":
		switch {
		case t.focus:
			t.focus = false
		case t.filter != "":
			t.filter = ""
			t.clamp()
		}
	case "e":
		if t.diagnostic { // detalhes: o relatório também traz a saída original (crua) de cada etapa
			t.details = !t.details
		}
	case "space":
		if has {
			if t.marks[r.ID] {
				delete(t.marks, r.ID)
				t.order = slices.DeleteFunc(t.order, func(x string) bool { return x == r.ID })
			} else {
				t.marks[r.ID] = true
				t.order = append(t.order, r.ID)
			}
		}
	}
	return nil
}

// step anda n itens na lista (ou, com o foco nas etapas, nas etapas).
func (t *RecipesTab) step(n int) {
	r, has := t.current()
	if t.focus && has {
		t.scur = max(min(t.scur+n, len(r.Steps)-1), 0)
		return
	}
	t.cur = max(min(t.cur+n, len(t.visible())-1), 0)
	t.scur, t.stp = 0, 0
	t.fix()
}

func (t *RecipesTab) panelRows() int { return max(t.lastH/2, 1) }

func (t *RecipesTab) keySearch(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		t.filter, t.search = "", false
		t.clamp()
	case "enter":
		t.search = false
	case "up":
		t.step(-1)
	case "down":
		t.step(1)
	case "backspace":
		if r := []rune(t.filter); len(r) > 0 {
			t.filter = string(r[:len(r)-1])
			t.clamp()
		}
	default:
		if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			t.filter += textsafe.Line(msg.Text)
			t.clamp()
		}
	}
	return nil
}

// Keys implementa Tab.
func (t *RecipesTab) Keys() []KeyHint {
	if t.search {
		return []KeyHint{{"Enter", "fixar"}, {"Esc", "limpar"}, {"↑↓", "mover"}}
	}
	if t.focus {
		return []KeyHint{{"↑↓", "rolar etapas"}, {"←", "voltar à lista"}, {"Esc", "voltar"}}
	}
	keys := []KeyHint{{"↑↓", "mover"}, {"espaço", "marcar"}, {"→", "etapas"}}
	if t.diagnostic {
		// a lista de diagnósticos é curta: a dica de busca ("/" continua valendo) cede o lugar à de detalhes, para
		// "Enter revisar" caber em 80 colunas
		keys = append(keys, act("e").on(t.details).Hint())
	} else {
		keys = append(keys, KeyHint{"/", "buscar"})
	}
	return append(keys, KeyHint{"Enter", "revisar"})
}

// HelpKeys implementa SectionHelper: as teclas da seção, uma frase por tecla (legenda única em keys.go).
func (t *RecipesTab) HelpKeys() []KeyDef {
	out := []KeyDef{
		navDef("↑↓", "mover", "move o cursor pela lista."),
		act("espaço").says("marca ou desmarca para a Revisão."),
		navDef("→", "etapas", "foco nas etapas da receita; ← ou Esc volta à lista."),
	}
	if t.diagnostic {
		out = append(out, act("e").says("liga e desliga os detalhes: o relatório traz também a saída original de cada etapa."))
	} else {
		out = append(out, navDef("/", "buscar", "filtra a lista; Enter fixa, Esc limpa."))
	}
	return append(out, navDef("Enter", "revisar", "abre a Revisão dos marcados (sem marcados, do item sob o cursor); nada roda antes do plano e do s."))
}

// Status implementa Tab.
func (t *RecipesTab) Status() string {
	if t.notice != "" {
		return t.notice
	}
	r, ok := t.current()
	if !ok {
		return ""
	}
	s := r.ID + " — " + r.Name
	if r.Description != "" {
		s += ": " + r.Description
	}
	return s
}

// More implementa Tab: etapas abaixo da dobra.
func (t *RecipesTab) More() int {
	r, ok := t.current()
	if !ok || t.loading || t.loadErr != nil {
		return 0
	}
	sr := t.renderedSteps(r, t.lastW)
	t.fixSteps(sr)
	return max(len(sr.lines)-(t.stp+t.stepRows()), 0)
}

func (t *RecipesTab) spin() string { return t.theme.Spin(t.frame) }

// loadingWhat é o que a aba lê (estado carregando); errorWhat é o objeto do erro de leitura.
func (t *RecipesTab) loadingWhat() string {
	if t.diagnostic {
		return "diagnósticos e estado"
	}
	return "receitas e estado"
}

func (t *RecipesTab) errorWhat() string {
	if t.diagnostic {
		return "os diagnósticos"
	}
	return "as receitas"
}

// emptySteps diz como ter conteúdo na aba vazia.
func (t *RecipesTab) emptySteps() []string {
	if t.diagnostic {
		return []string{"Veja o que o toolkit sabe diagnosticar: toolkit diagnostico --help"}
	}
	return []string{"Veja as receitas do catálogo: toolkit recipe list"}
}

func (t *RecipesTab) emptyText() string {
	if t.diagnostic {
		return "Nenhum diagnóstico para este sistema."
	}
	return "Nenhuma receita para este sistema."
}

// stepRows é a altura útil do painel de etapas (depois da lista, da régua e do aviso do state).
func (t *RecipesTab) stepRows() int {
	h := t.lastH - t.listRows() - 1
	if t.data.StateErr != nil {
		h -= 2
	}
	if t.search || t.filter != "" {
		h--
	}
	return max(h, 1)
}

// View desenha o corpo.
func (t *RecipesTab) View(w, h int) string {
	t.lastW, t.lastH = w, h
	switch {
	case t.loading:
		return strings.Join(StateLoading(t.theme, w, h, t.loadingWhat(), t.frame), "\n")
	case t.loadErr != nil:
		return strings.Join(StateError(t.theme, w, h, t.errorWhat(), t.loadErr, "Rode: toolkit recipe status"), "\n")
	case len(t.items) == 0:
		return strings.Join(StateEmpty(t.theme, w, h, t.emptyText(), t.emptySteps()...), "\n")
	}
	var lines []string
	if err := t.data.StateErr; err != nil {
		lines = append(lines,
			" "+t.theme.Warn().Render(SymWarn+" aviso:")+" não deu para ler o estado em "+textsafe.Line(t.data.StatePath)+" ("+textsafe.Line(err.Error())+").",
			" Rode: toolkit recipe status")
	}
	if t.search || t.filter != "" {
		cursor := ""
		if t.search {
			cursor = "▌"
		}
		lines = append(lines, " "+t.theme.Accent().Render("/")+" "+textsafe.Line(t.filter)+cursor)
	}
	vis := t.visible()
	if len(vis) == 0 {
		lines = append(lines, " Nada com \""+textsafe.Line(t.filter)+"\". Esc limpa a busca.")
		return strings.Join(lines, "\n")
	}
	t.fix()
	for i := t.top; i < len(vis) && i < t.top+t.listRows(); i++ {
		lines = append(lines, t.listLine(vis[i], i == t.cur, w))
	}
	r := vis[t.cur]
	lines = append(lines, t.panelRule(r, w))
	lines = append(lines, t.stepLines(r, w)...)
	for i := range lines {
		lines[i] = clip(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

func (t *RecipesTab) runOf(id string) RecipeRun { return t.data.Runs[id] }

func (t *RecipesTab) listLine(r RecipeInfo, cur bool, w int) string {
	cursor := " "
	if cur && !t.focus {
		cursor = ">"
	} else if cur {
		cursor = "·"
	}
	mark := "[ ]"
	if t.marks[r.ID] {
		mark = t.theme.Accent().Render("[x]")
	}
	stateW, flagsW := 24, 18
	if w < 80 {
		flagsW = 0
	}
	used := 7 + stateW + 2
	if flagsW > 0 {
		used += flagsW + 2
	}
	nameW := max(w-used, 12)
	name := cell(r.Name, nameW)
	if cur {
		name = t.theme.Bold().Render(name)
	}
	txt, style := t.recipeState(r)
	line := " " + cursor + " " + mark + " " + name + "  " + style.Render(cell(txt, stateW))
	if flagsW > 0 {
		line += "  " + t.theme.Muted().Render(cell(flagsText(r), flagsW))
	}
	return line
}

func flagsText(r RecipeInfo) string {
	var f []string
	if r.RequiresAdmin {
		f = append(f, "admin")
	}
	if n := rebootSteps(r); n > 0 {
		f = append(f, "reinicia")
	}
	return strings.Join(f, " · ")
}

func rebootSteps(r RecipeInfo) int {
	n := 0
	for _, s := range r.Steps {
		if s.Reboot == "required" || s.Reboot == "if-needed" {
			n++
		}
	}
	return n
}

// recipeState devolve símbolo+palavra do último andamento (vazio = nunca rodou) e o estilo.
func (t *RecipesTab) recipeState(r RecipeInfo) (string, Paint) {
	run, ok := t.data.Runs[r.ID]
	if !ok {
		return "", Paint{}
	}
	switch run.Status {
	case "done":
		return SymOK + " concluída", t.theme.OK()
	case "failed":
		return SymFail + " falhou", t.theme.Err()
	case "awaiting-reboot":
		return SymWarn + " aguardando reinício", t.theme.Warn()
	case "running":
		return t.spin() + " em andamento", t.theme.Accent()
	}
	return "", Paint{}
}

// panelRule: a régua com o título do painel e o resumo "N de M · exige administrador · reinicia 1 vez".
func (t *RecipesTab) panelRule(r RecipeInfo, w int) string {
	done := 0
	for _, s := range t.runOf(r.ID).Steps {
		if s.Status == "done" || s.Status == "skipped" {
			done++
		}
	}
	title := " Etapas de " + r.ID + " "
	parts := []string{fmt.Sprintf("%d de %d", done, len(r.Steps))}
	if r.RequiresAdmin {
		parts = append(parts, "exige administrador")
	}
	if n := rebootSteps(r); n > 0 {
		parts = append(parts, "reinicia "+plural(n, "vez", "vezes"))
	}
	// janela estreita: o resumo perde partes do fim (admin, reinício), nunca estoura a largura
	right := ""
	for n := len(parts); n >= 1; n-- {
		right = " " + strings.Join(parts[:n], " · ") + " "
		if lipgloss.Width(title)+lipgloss.Width(right)+2 <= w {
			break
		}
	}
	fill := max(w-lipgloss.Width(title)-lipgloss.Width(right)-2, 1)
	return clip(SymRule+t.theme.Title().Render(title)+strings.Repeat(SymRule, fill)+right, w)
}

// stepRender é o painel de etapas já em LINHAS: o aviso "Nunca rodou", a linha de erro de uma etapa que
// falhou e a dica de reset também ocupam o corpo, então a rolagem e o More contam estas linhas, não as
// etapas. first/last dão, por etapa, a primeira e a última linha dela.
type stepRender struct {
	lines       []string
	first, last []int
}

func (t *RecipesTab) renderedSteps(r RecipeInfo, w int) stepRender {
	var sr stepRender
	if len(r.Steps) == 0 {
		sr.lines = []string{" Esta receita não tem etapas."}
		return sr
	}
	run := t.runOf(r.ID)
	if len(run.Steps) == 0 && r.Diagnostic {
		sr.lines = append(sr.lines, " Nunca rodou. Marque e tecle Enter: a Revisão mostra o que será executado.")
	}
	if run.Status == "failed" { // no topo: lido primeiro e nunca escondido abaixo da dobra
		sr.lines = append(sr.lines, clip(" toolkit recipe reset "+r.ID+" libera nova tentativa", w))
	}
	for i, s := range r.Steps {
		cursor := " "
		if t.focus && i == t.scur {
			cursor = ">"
		}
		st := run.Steps[s.ID]
		txt, style := stepState(t.theme, st.Status)
		sr.first = append(sr.first, len(sr.lines))
		sr.lines = append(sr.lines, clip(fmt.Sprintf(" %s %2d %s  %s", cursor, i+1, cell(s.Name, max(w-34, 12)), style.Render(txt)), w))
		if st.Status == "failed" && st.Error != "" {
			sr.lines = append(sr.lines, clip("       "+t.theme.Err().Render("erro:")+" "+firstSentence(textsafe.Line(st.Error)), w))
		}
		sr.last = append(sr.last, len(sr.lines)-1)
	}
	return sr
}

// fixSteps prende a janela (t.stp, em linhas) para a etapa selecionada aparecer inteira (se não couber,
// a primeira linha dela) e para a janela nunca passar do fim.
func (t *RecipesTab) fixSteps(sr stepRender) {
	rows := t.stepRows()
	if t.scur >= 0 && t.scur < len(sr.first) {
		first, last := sr.first[t.scur], sr.last[t.scur]
		if first < t.stp {
			t.stp = first
		}
		if last >= t.stp+rows {
			t.stp = max(last-rows+1, min(first, t.stp))
			if last-first+1 > rows {
				t.stp = first
			}
		}
	}
	t.stp = max(min(t.stp, max(len(sr.lines)-rows, 0)), 0)
}

func (t *RecipesTab) stepLines(r RecipeInfo, w int) []string {
	sr := t.renderedSteps(r, w)
	t.fixSteps(sr)
	rows := t.stepRows()
	return sr.lines[t.stp:min(t.stp+rows, len(sr.lines))]
}

func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i+1]
	}
	return s
}

func stepState(th Theme, status string) (string, Paint) {
	switch status {
	case "done":
		return SymOK + " feita", th.OK()
	case "skipped":
		return SymSkipped + " pulada", th.Muted()
	case "failed":
		return SymFail + " falhou", th.Err()
	case "awaiting-reboot":
		return SymWarn + " aguardando reinício", th.Warn()
	case "running":
		return "| executando", th.Accent()
	}
	return SymPending + " pendente", Paint{}
}
