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
//	3..H-3  corpo da aba (H-5 linhas)
//	H-2     régua (com "↓ mais N linhas" à direita quando há rolagem)
//	H-1     linha de estado
//	H       barra de teclas
func (m *Model) screen() string {
	bodyH := m.h - 5
	var body string
	reviewMore := 0
	switch {
	case m.modal == modalHelp:
		body = m.helpBody()
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
	rows = append(rows, m.header(), m.rule(0))
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
	case m.mode == modeReview:
		hints = []KeyHint{{"Tab", "botão"}, {"s", "executar"}, {"Enter", "acionar"}, {"Esc", "voltar"}, {"↑↓", "rolar"}}
		tail = []KeyHint{{"q", "sair"}}
	case m.mode == modeRun:
		hints = []KeyHint{{"Tab", "abas (leitura)"}, {"↑↓", "itens"}, {"f", "seguir o fim"}}
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

// tooSmall é a tela única para janelas abaixo de 60x16: só avisa, e q continua funcionando.
func (m *Model) tooSmall() string {
	return "Janela pequena demais (mínimo " + itoa(MinWidth) + " × " + itoa(MinHeight) + ").\n" +
		"Aumente a janela ou use: toolkit --no-tui\n\nq sai."
}

// helpBody: a tabela de teclas, no lugar do corpo (modal sem caixa flutuante).
func (m *Model) helpBody() string {
	rows := [][2]string{
		{"Tab / Shift+Tab", "próxima aba / anterior"},
		{"1 a 4", "Apps, Receitas, Diagnóstico, Log"},
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
	}
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
