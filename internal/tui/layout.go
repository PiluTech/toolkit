// Package tui é a interface Bubble Tea do toolkit (fase 4): abas Apps, Receitas, Diagnóstico e Log.
// A especificação completa está em docs/tui.md.
package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Estrutura da tela (H = altura), igual à especificação em docs/tui.md:
//
//	1       abas + versão
//	2       régua
//	3..    faixa de resultado da aba ativa, quando há (result.go)
//	..H-3  corpo da aba (H-5 linhas, menos as da faixa)
//	H-2     régua (com "↓ mais N linhas" à direita quando há rolagem)
//	H-1     linha de estado
//	H       barra de teclas
func (m *Model) screen() string {
	strip := m.strip()
	head := []string{m.header(), m.rule(0)}
	if m.areaMode {
		head = m.areaRows()
	}
	bodyH := m.h - 3 - len(head) - len(strip)
	var body string
	reviewMore := 0
	switch {
	case m.modal == modalHelp:
		body = m.helpBody()
	case m.modal == modalUpdate:
		body = m.updateBody(bodyH)
	case m.mode == modeReview && m.review != nil:
		all := m.review.lines(m.w)
		m.review.scroll = max(min(m.review.scroll, max(len(all)-bodyH, 0)), 0)
		end := min(m.review.scroll+bodyH, len(all))
		body = strings.Join(all[m.review.scroll:end], "\n")
		reviewMore = len(all) - end
	default:
		body = m.tabs[m.active].View(m.w, bodyH)
	}
	lines := fit(strings.Split(strings.TrimRight(body, "\n"), "\n"), m.w, bodyH)

	rows := make([]string, 0, m.h)
	rows = append(rows, head...)
	rows = append(rows, strip...)
	rows = append(rows, lines...)
	more := reviewMore
	if m.modal == modalNone && m.mode != modeReview {
		more = m.tabs[m.active].More()
	}
	rows = append(rows, m.rule(more), m.statusLine(), m.keyBar())
	return strings.Join(rows, "\n")
}

// fit devolve exatamente h linhas, cada uma recortada em w colunas (nunca estoura a largura).
func fit(lines []string, w, h int) []string {
	out := make([]string, 0, h)
	for _, l := range lines {
		if len(out) == h {
			break
		}
		out = append(out, clip(l, w))
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out
}

// clip recorta s em w colunas com reticências; ansi.Truncate não corta sequência de estilo no meio.
func clip(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func (m *Model) rule(more int) string {
	if more > 0 {
		hint := " " + SymMore + " mais " + plural(more, "linha", "linhas") + " "
		hw := lipgloss.Width(hint)
		return strings.Repeat(SymRule, max(m.w-hw, 0)) + m.theme.Muted().Render(hint)
	}
	return strings.Repeat(SymRule, m.w)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// header: " toolkit  [1 Apps]  2 Receitas   3 Diagnóstico   4 Log" e, à direita, a versão. A aba ativa
// leva colchetes (nunca só cor); as inativas levam espaços no lugar dos colchetes, para não andarem.
func (m *Model) header() string {
	narrow := m.w < 70
	var left strings.Builder
	left.WriteString(" " + m.theme.Title().Render("toolkit") + "  ")
	for i, t := range m.tabs {
		name := t.Title()
		if narrow {
			name = t.Short()
		}
		label := itoa(i+1) + " " + name
		if i == m.active {
			left.WriteString(m.theme.Active().Render("[" + label + "]"))
		} else {
			left.WriteString(" " + label + " ")
		}
		if i < len(m.tabs)-1 {
			left.WriteString(" ")
		}
	}
	right := m.versionBanner(narrow)
	gap := m.w - lipgloss.Width(left.String()) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	return clip(left.String()+strings.Repeat(" ", gap)+right+" ", m.w)
}

// versionBanner: "↑ v0.2.0 disponível (u)" quando há versão nova (nunca rouba foco), senão a versão atual.
func (m *Model) versionBanner(narrow bool) string {
	if m.opts.Latest != "" {
		txt := SymUpdate + " " + textsafe.Line(m.opts.Latest)
		if narrow {
			txt += " (u)"
		} else {
			txt += " disponível (u)"
		}
		return m.theme.Accent().Render(txt)
	}
	return m.theme.Muted().Render(textsafe.Line(m.opts.Version))
}

// statusLine: item sob o cursor à esquerda, "Enter revisa N marcados" à direita.
func (m *Model) statusLine() string {
	left := " " + textsafe.Line(m.tabs[m.active].Status())
	switch {
	case m.modal == modalUpdate:
		left = " " + m.updateButtons()
	case m.modal != modalNone:
		left = " Ajuda"
	case m.mode == modeReview && m.review != nil:
		left = " " + m.review.buttons()
	case m.notice != "":
		left = " " + textsafe.Line(m.notice)
	case m.question != "":
		left = " " + textsafe.Line(m.question) + " [s/N]"
	}
	right := ""
	if n := m.tabs[m.active].Selected(); n > 0 && m.modal == modalNone && m.mode == modeNav {
		right = "Enter revisa " + plural(n, "marcado", "marcados") + " "
	}
	room := m.w - lipgloss.Width(right) - 1
	left = clip(left, max(room, 0))
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + right
}

// keyBar: atalhos da aba (por prioridade) e, sempre, "? ajuda  q sair". Sai o que não cabe, do fim para o
// começo da lista da aba; abaixo de 70 colunas a lista é a mínima.
func (m *Model) keyBar() string {
	tail := []KeyHint{{"?", "ajuda"}, {"q", "sair"}}
	var hints []KeyHint
	switch {
	case m.modal == modalHelp:
		hints = []KeyHint{{"Esc", "fechar"}}
		tail = []KeyHint{{"q", "sair"}}
	case m.modal == modalUpdate:
		hints = []KeyHint{{"Tab", "botão"}, {"a", "atualizar"}, {"Enter", "acionar"}, {"Esc", "fechar"}, {"↑↓", "rolar"}}
		tail = []KeyHint{{"q", "sair"}}
	case m.mode == modeReview:
		hints = []KeyHint{{"Tab", "botão"}, {"s", "executar"}, {"Enter", "acionar"}, {"Esc", "voltar"}, {"↑↓", "rolar"}}
		tail = []KeyHint{{"q", "sair"}}
	case m.mode == modeRun:
		hints = []KeyHint{{"Tab", "abas (leitura)"}, {"↑↓", "itens"}, {"f", "seguir o fim"}}
		if m.areaMode {
			hints = []KeyHint{{"0-" + itoa(len(m.areas)-1), "áreas (leitura)"}, {"Tab", "seções"}, {"↑↓", "itens"}, {"f", "seguir o fim"}}
		}
		tail = []KeyHint{{"q", "interromper"}}
	default:
		hints = m.tabs[m.active].Keys()
	}
	if m.w < 70 && len(hints) > 3 {
		hints = hints[:3]
	}
	for n := len(hints); n >= 0; n-- {
		line := renderHints(append(append([]KeyHint{}, hints[:n]...), tail...), m.theme)
		if lipgloss.Width(line)+1 <= m.w || n == 0 {
			return clip(" "+line, m.w)
		}
	}
	return ""
}

func renderHints(hs []KeyHint, th Theme) string {
	parts := make([]string, 0, len(hs))
	for _, h := range hs {
		parts = append(parts, th.Bold().Render(h.Key)+" "+h.Label)
	}
	return strings.Join(parts, "  ")
}

// tooSmall é a tela única para janelas abaixo de 60x16 (StateTooSmall): título, a frase com o tamanho atual, os dois
// caminhos (aumentar a janela ou usar os comandos sem a TUI) e "q sair". Cada linha quebra na largura da janela (nada
// é cortado); se faltar altura, o "q sair" (última linha) sobrevive e o resto cede, por prioridade. Redimensionar
// volta à interface normal (o estado das abas não muda).
func (m *Model) tooSmall() string {
	return strings.Join(StateTooSmall(m.theme, m.w, max(m.h, 1), MinWidth, MinHeight), "\n")
}

// helpBody: a tabela de teclas, no lugar do corpo (modal sem caixa flutuante).
func (m *Model) helpBody() string {
	rows := [][2]string{
		{"Tab / Shift+Tab", "próxima aba / anterior"},
		{"1 a 6", "Apps, Receitas, Diagnóstico, Log, Privacidade, Apps nativos"},
	}
	if m.areaMode {
		titles := make([]string, len(m.areas))
		for i, a := range m.areas {
			titles[i] = a.Title
		}
		rows = [][2]string{
			{"Tab Shift+Tab", "seções da área (não sai dela)"},
			{"0 a " + itoa(len(m.areas)-1), strings.Join(titles, ", ")},
		}
	}
	rows = append(rows, [][2]string{
		{"↑ ↓  j k", "mover o cursor ou rolar"},
		{"g / G  PgUp / PgDn", "início, fim, página"},
		{"→ l  ← h", "foco nas etapas / volta à lista (Receitas, Diagnóstico)"},
		{"espaço", "marcar ou desmarcar"},
		{"v", "lista de versões (app com versões)"},
		{"/", "buscar (Esc limpa)"},
		{"Enter", "revisar o que está marcado"},
		{"s", "executar na Revisão / responder sim"},
		{"f", "seguir o fim do log"},
		{"u", "notas da versão nova (quando há)"},
		{"Esc", "volta um nível: modal, busca, Revisão"},
		{"q  Ctrl+C", "sair (durante a execução pergunta antes)"},
	}...)
	var b strings.Builder
	b.WriteString(" " + m.theme.Title().Render(titleCase(m.theme, "Teclas")) + "\n\n")
	for _, r := range rows {
		b.WriteString(" " + padRight(r[0], 20) + " " + r[1] + "\n")
	}
	return b.String()
}

// titleCase: em Mono os títulos vão em MAIÚSCULAS (sem negrito para diferenciá-los).
func titleCase(t Theme, s string) string {
	if t.Mono {
		return strings.ToUpper(s)
	}
	return s
}

func padRight(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// updateBody: as notas da versão nova no lugar do corpo (modal sem caixa flutuante, como a ajuda).
func (m *Model) updateBody(h int) string {
	var all []string
	all = append(all, " "+m.theme.Title().Render(titleCase(m.theme, "Versão nova")))
	all = append(all, wrap(" "+textsafe.Line(m.opts.Version)+" "+SymUpdate+" "+textsafe.Line(m.opts.Latest), m.w, 1)...)
	all = append(all, "")
	if m.opts.LatestNotes == "" {
		all = append(all, " Sem notas desta versão.")
	} else {
		for _, l := range strings.Split(m.opts.LatestNotes, "\n") {
			all = append(all, wrap("   "+textsafe.Line(l), m.w, 3)...)
		}
	}
	if m.opts.LatestURL != "" {
		all = append(all, "", " "+textsafe.Line(m.opts.LatestURL))
	}
	all = append(all, "", " Atualizar fecha a TUI e roda: toolkit update (confere o SHA-256).")
	m.updScroll = max(min(m.updScroll, max(len(all)-h, 0)), 0)
	return strings.Join(all[m.updScroll:min(m.updScroll+h, len(all))], "\n")
}

// updateButtons é a linha de estado do modal: o botão em foco leva ">" (nunca só reverso).
func (m *Model) updateButtons() string {
	closeBtn, upd := "  [ Fechar ]", "  [ Atualizar ]"
	if m.updFocus == 0 {
		closeBtn = "> [ Fechar ]"
	} else {
		upd = "> [ Atualizar ]"
	}
	return closeBtn + "     " + upd
}
