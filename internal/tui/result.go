package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

const (
	resultMaxDetail = 4  // linhas de detalhe (cada uma quebra, nunca é cortada)
	resultMinBody   = 8  // linhas que o corpo da aba mantém, no mínimo, com a faixa cheia
	resultCompactH  = 20 // abaixo disso a faixa vira uma linha
	resultDismiss   = "[x] fechar"
)

// kindLook devolve o símbolo, a palavra e a pintura do tipo (a cor fica só no símbolo; em Mono nada é emitido).
func kindLook(th Theme, k ResultKind) (sym, word string, p Paint) {
	switch k {
	case ResultOK:
		return SymOK, "feito", th.OK()
	case ResultPartial:
		return SymWarn, "parcial", th.Warn()
	case ResultError:
		return SymFail, "falhou", th.Err()
	case ResultPending:
		return SymWarn, "pendente", th.Warn()
	}
	return "i", "info", th.Muted()
}

// renderResult desenha a faixa em w colunas. h é a altura da JANELA: abaixo de resultCompactH, ou quando a
// faixa cheia deixaria menos de resultMinBody linhas ao corpo, ela vira uma linha só (símbolo + título + [x]).
// A faixa cheia é: título (símbolo e palavra na frente), até 4 linhas de detalhe quebradas e a régua de baixo.
func renderResult(th Theme, r Result, w, h int) []string {
	sym, word, paint := kindLook(th, r.Kind)
	title := textsafe.Line(r.Title)
	hint := th.Accent().Render(resultDismiss)
	hw := lipgloss.Width(resultDismiss)
	if h >= resultCompactH {
		if full := fullResult(th, r, sym, word, title, paint, w); len(full) <= max(h-5-resultMinBody, 1) {
			return full
		}
	}
	// compacta: símbolo + título (única linha: o título pode perder o fim com reticências) + [x]
	room := w - 1 - lipgloss.Width(sym) - 1 - hw - 2
	t := title
	if lipgloss.Width(t) > room {
		t = ansi.Truncate(t, max(room, 1), "…")
	}
	left := paint.Render(sym + " " + t)
	gap := max(w-1-lipgloss.Width(sym)-1-lipgloss.Width(t)-hw-1, 1)
	return []string{clip(" "+left+strings.Repeat(" ", gap)+hint, w)}
}

func fullResult(th Theme, r Result, sym, word, title string, paint Paint, w int) []string {
	hint := th.Accent().Render(resultDismiss)
	hw := lipgloss.Width(resultDismiss)
	head := wrap(sym+" "+word+"  "+title, w-1, 3)
	out := make([]string, 0, 8)
	first := " " + paint.Render(head[0])
	if len(head) == 1 && lipgloss.Width(first)+2+hw+1 <= w {
		out = append(out, first+strings.Repeat(" ", w-lipgloss.Width(first)-hw-1)+hint)
		head = nil
	} else {
		out = append(out, first)
		for _, l := range head[1:] {
			out = append(out, " "+l)
		}
		head = []string{""} // marca: a dica vai numa linha própria no fim
	}
	for i, l := range r.Lines {
		if i == resultMaxDetail {
			break
		}
		for _, p := range wrap(textsafe.Line(l), w-3, 0) {
			out = append(out, "   "+p)
		}
	}
	if head != nil {
		out = append(out, strings.Repeat(" ", max(w-hw-1, 0))+hint)
	}
	return append(out, strings.Repeat(SymRule, w))
}

// resultFor é o resultado da aba i: o da própria aba (Resulter) ou o que a raiz guardou para ela (execuções).
func (m *Model) resultFor(i int) *Result {
	if rs, ok := m.tabs[i].(Resulter); ok {
		if r := rs.Result(); r != nil {
			return r
		}
	}
	return m.results[i]
}

// currentResult é o resultado da aba ativa.
func (m *Model) currentResult() *Result { return m.resultFor(m.active) }

// dismissResult fecha a faixa da aba ativa; false = não havia faixa.
func (m *Model) dismissResult() bool {
	if m.currentResult() == nil {
		return false
	}
	if rs, ok := m.tabs[m.active].(Resulter); ok && rs.Result() != nil {
		rs.Dismiss()
	}
	delete(m.results, m.active)
	return true
}

// strip são as linhas da faixa da aba ativa (nenhuma sem resultado, com modal ou na Revisão).
func (m *Model) strip() []string {
	if m.modal != modalNone || m.mode == modeReview {
		return nil
	}
	r := m.currentResult()
	if r == nil {
		return nil
	}
	return renderResult(m.theme, *r, m.w, m.h)
}
