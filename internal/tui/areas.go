package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// AreaDef define uma área da raiz (Instalar, Máquina...) com as suas seções (as abas). Key é o identificador estável
// ("instalar"), Title o nome inteiro ("Instalar") e Short o curto usado abaixo de 78 colunas ("Inst").
type AreaDef struct {
	Key, Title, Short string
	Tabs              []Tab
}

// Area é uma área já montada: Idx são os índices das suas seções em Model.tabs (a lista PLANA de todas as seções).
type Area struct {
	Key, Title, Short string
	Idx               []int
}

// NewWithAreas monta a raiz com áreas: as seções de todas as áreas viram a lista plana de abas (execução, seleção,
// resultados e origens continuam por índice plano) e a barra mostra as áreas, com a linha 2 das seções.
func NewWithAreas(o Options, areas []AreaDef) *Model {
	var tabs []Tab
	m := &Model{opts: o, theme: Theme{Mono: o.Mono, Dark: true, NoMotion: o.NoMotion}, w: 80, h: 24, results: map[int]*Result{}, areaMode: true}
	for _, a := range areas {
		ar := Area{Key: a.Key, Title: a.Title, Short: a.Short}
		for _, t := range a.Tabs {
			ar.Idx = append(ar.Idx, len(tabs))
			tabs = append(tabs, t)
		}
		if len(ar.Idx) == 0 { // área sem seção não existe: nada a mostrar nem a numerar
			continue
		}
		m.areas = append(m.areas, ar)
	}
	m.tabs = tabs
	m.lastSec = make([]int, len(m.areas))
	return m
}

// legacyAreas dá a cada aba uma área de uma seção só (New com abas planas: números 1 a N, sem a linha 2).
func legacyAreas(tabs []Tab) []Area {
	out := make([]Area, len(tabs))
	for i, t := range tabs {
		out[i] = Area{Key: itoa(i + 1), Title: t.Title(), Short: t.Short(), Idx: []int{i}}
	}
	return out
}

// areaOf diz a área e a posição, dentro dela, da seção plana i.
func (m *Model) areaOf(i int) (area, pos int) {
	for a, ar := range m.areas {
		for p, idx := range ar.Idx {
			if idx == i {
				return a, p
			}
		}
	}
	return 0, 0
}

// setActive troca a seção ativa e lembra qual foi a última de cada área.
func (m *Model) setActive(i int) {
	m.active = i
	if m.areaMode {
		a, p := m.areaOf(i)
		m.lastSec[a] = p
	}
}

// gotoArea vai para a última seção visitada da área (a primeira na primeira visita).
func (m *Model) gotoArea(a int) {
	m.setActive(m.areas[a].Idx[m.lastSec[a]])
}

// goTo atende um GoToMsg (a seção Início abre a área e a seção de um objetivo). Só vale na navegação normal: durante a
// Revisão, a execução, um modal, uma pergunta [s/N] ou numa seção que captura teclas (busca, confirmação) é ignorado,
// como as teclas de número. Índice de área fora da lista é ignorado; seção fora da área abre a última visitada.
func (m *Model) goTo(g GoToMsg) {
	if !m.areaMode || m.modal != modalNone || m.mode != modeNav || m.asking != askNone || m.tabs[m.active].Capturing() {
		return
	}
	if g.Area < 0 || g.Area >= len(m.areas) {
		return
	}
	idx := m.areas[g.Area].Idx
	if g.Section < 0 || g.Section >= len(idx) {
		m.gotoArea(g.Area)
	} else {
		m.setActive(idx[g.Section])
	}
	m.notice = ""
}

// cycleSection troca de seção DENTRO da área ativa (dá a volta, nunca sai dela).
func (m *Model) cycleSection(d int) {
	a, p := m.areaOf(m.active)
	n := len(m.areas[a].Idx)
	m.setActive(m.areas[a].Idx[(p+d+n)%n])
}

// areaKey trata Tab, Shift+Tab e os números 0 a 5 no modo áreas.
func (m *Model) areaKey(k string) bool {
	switch k {
	case "tab":
		m.cycleSection(1)
		return true
	case "shift+tab":
		m.cycleSection(-1)
		return true
	}
	if len(k) == 1 && k[0] >= '0' && k[0] <= '9' {
		if a := int(k[0] - '0'); a < len(m.areas) {
			m.gotoArea(a)
			return true
		}
	}
	return false
}

// areaChrome diz quantas linhas o cabeçalho de áreas gasta acima do corpo: 3 (abas, seções, régua) ou, abaixo de 24
// linhas, 2 (a seção sobe para a linha das abas).
func (m *Model) collapsed() bool { return m.h < 24 }

func (m *Model) areaRows() []string {
	rows := []string{m.areaHeader()}
	if !m.collapsed() {
		rows = append(rows, m.areaSections())
	}
	return append(rows, m.rule(0))
}

// areaHeader é a linha 1: "[0 Início]  1 Instalar ..." (a ativa entre colchetes, nunca só cor) e, à direita, a versão.
// Nunca corta nome de área: se não cabe, passa para os nomes curtos e, por último, tira o aviso de versão.
func (m *Model) areaHeader() string {
	ai, _ := m.areaOf(m.active)
	secLong, secShort := m.tabs[m.active].Title(), m.tabs[m.active].Short()
	type level struct{ shortArea, shortSec bool }
	levels := []level{{false, false}, {false, true}, {true, false}, {true, true}}
	if m.w < 78 {
		levels = levels[2:]
	}
	if !m.collapsed() {
		levels = []level{{false, false}, {true, false}}
		if m.w < 78 {
			levels = levels[1:]
		}
	}
	build := func(l level) string {
		var b strings.Builder
		b.WriteString(" ")
		if m.w >= 100 {
			b.WriteString(m.theme.Title().Render("toolkit") + " ")
		}
		for i, a := range m.areas {
			name := a.Title
			if l.shortArea {
				name = a.Short
			}
			label := itoa(i) + " " + name
			if i == ai {
				if m.collapsed() && secLong != a.Title { // área de uma seção com o mesmo nome (Início) não repete
					sec := secLong
					if l.shortSec {
						sec = secShort
					}
					label += " › " + sec
				}
				b.WriteString(m.theme.Active().Render("[" + label + "]"))
			} else {
				b.WriteString(" " + label + " ")
			}
		}
		return b.String()
	}
	var last string
	for _, bn := range m.bannerChain() {
		for _, l := range levels {
			left := build(l)
			last = left
			gap := m.w - lipgloss.Width(left) - lipgloss.Width(bn)
			if gap < 0 || gap == 0 && bn != "" && strings.HasSuffix(ansi.Strip(left), "]") {
				continue
			}
			if gap >= 2 || bn == "" { // um espaço de margem à direita quando sobra
				gap--
				bn += " "
			}
			return left + strings.Repeat(" ", max(gap, 0)) + bn
		}
	}
	return clip(last, m.w)
}

// bannerChain são as formas do canto direito da linha 1, da mais completa à vazia: "↑ v0.6.0 disponível (u)" (a partir
// de 100 colunas) ou "↑ v0.6.0 (u)" quando há versão nova, senão a versão atual; depois a versão sem o "(u)" e nada.
func (m *Model) bannerChain() []string {
	if m.opts.Latest == "" {
		return []string{m.theme.Muted().Render(textsafe.Line(m.opts.Version)), ""}
	}
	bare := SymUpdate + " " + textsafe.Line(m.opts.Latest)
	full := bare + " (u)"
	if m.w >= 100 {
		full = bare + " disponível (u)"
	}
	a := m.theme.Accent()
	return []string{a.Render(full), a.Render(bare), ""}
}

// areaSections é a linha 2: "Máquina ›  [Diagnóstico]  Apps nativos" e, à direita, "admin: sim/não" (a partir de 100
// colunas também "N marcados ·"). Área de uma seção com o mesmo nome mostra só o nome da área.
func (m *Model) areaSections() string {
	ai, _ := m.areaOf(m.active)
	ar := m.areas[ai]
	left := func(short bool) string {
		if len(ar.Idx) == 1 && m.tabs[ar.Idx[0]].Title() == ar.Title {
			return " " + ar.Title
		}
		var b strings.Builder
		b.WriteString(" " + ar.Title + " ›")
		for _, idx := range ar.Idx {
			name := m.tabs[idx].Title()
			if short {
				name = m.tabs[idx].Short()
			}
			if idx == m.active {
				b.WriteString(" " + m.theme.Active().Render("["+name+"]"))
			} else {
				b.WriteString("  " + name + " ")
			}
		}
		return b.String()
	}
	ctx := "admin: não"
	if m.opts.IsAdmin {
		ctx = "admin: sim"
	}
	if n := m.totalSelected(); n > 0 && m.w >= 100 {
		ctx = plural(n, "marcado", "marcados") + " · " + ctx
	}
	l := left(false)
	if lipgloss.Width(l)+lipgloss.Width(ctx)+2 > m.w {
		l = left(true)
	}
	gap := m.w - lipgloss.Width(l) - lipgloss.Width(ctx) - 1
	if gap < 1 {
		return clip(l, m.w)
	}
	return l + strings.Repeat(" ", gap) + m.theme.Muted().Render(ctx) + " "
}

func (m *Model) totalSelected() int {
	n := 0
	for _, t := range m.tabs {
		n += t.Selected()
	}
	return n
}

// InfoTab é a seção só de texto: orientação (Início) ou explicação de uma seção que ainda não existe (Rede). Não
// captura teclas nem tem estado.
type InfoTab struct {
	title, short, text string
}

// NewInfoTab monta uma seção só de texto (o texto pode ter várias linhas; cada uma quebra na largura da tela).
func NewInfoTab(title, short, text string) *InfoTab {
	return &InfoTab{title: title, short: short, text: text}
}

// Init não carrega nada.
func (t *InfoTab) Init() tea.Cmd { return nil }

// Capturing é sempre falso: a seção não recebe texto.
func (t *InfoTab) Capturing() bool { return false }

// Title devolve o nome inteiro da seção.
func (t *InfoTab) Title() string { return t.title }

// Short devolve o nome curto da seção.
func (t *InfoTab) Short() string { return t.short }

// Update ignora tudo.
func (t *InfoTab) Update(tea.Msg) (Tab, tea.Cmd) { return t, nil }

// View desenha o texto com um espaço de margem, quebrado na largura.
func (t *InfoTab) View(w, _ int) string {
	var out []string
	for _, l := range strings.Split(t.text, "\n") {
		out = append(out, wrap(" "+textsafe.Line(strings.TrimLeft(l, " ")), w, 1)...)
	}
	return strings.Join(out, "\n")
}

// Keys devolve nada: a seção não tem atalhos próprios.
func (t *InfoTab) Keys() []KeyHint { return nil }

// Status não tem item sob o cursor.
func (t *InfoTab) Status() string { return "" }

// More não rola.
func (t *InfoTab) More() int { return 0 }

// Selected não marca nada.
func (t *InfoTab) Selected() int { return 0 }
