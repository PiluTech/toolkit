package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

// selection reúne o que as abas têm marcado.
func (m *Model) selection() Selection {
	var s Selection
	for _, t := range m.tabs {
		switch x := t.(type) {
		case *AppsTab:
			s.Apps = append(s.Apps, x.Marked()...)
		case *RecipesTab:
			s.Recipes = append(s.Recipes, x.Marked()...)
		}
	}
	return s
}

func (m *Model) logTab() (*LogTab, int) {
	for i, t := range m.tabs {
		if l, ok := t.(*LogTab); ok {
			return l, i
		}
	}
	return nil, -1
}

// execKey trata as teclas da Revisão, da execução e das perguntas [s/N]; false = segue o fluxo normal.
func (m *Model) execKey(k string) (tea.Model, tea.Cmd, bool) {
	// pergunta [s/N] pendente: só s confirma (padrão N); qualquer outra tecla responde não
	if m.asking != askNone {
		yes := k == "s" || k == "S"
		kind, reply := m.asking, m.reply
		m.asking, m.question, m.reply = askNone, "", nil
		if l, _ := m.logTab(); l != nil {
			l.Ask("")
		}
		switch kind {
		case askInterrupt:
			if yes && m.cancel != nil {
				m.cancel() // encerra o item atual e descarta a fila
			}
		case askReboot:
			if reply != nil {
				reply <- yes
			}
		}
		return m, nil, true
	}
	switch m.mode {
	case modeReview:
		return m.reviewKey(k)
	case modeRun:
		switch k {
		case "ctrl+c", "q":
			m.asking, m.question = askInterrupt, "Interromper? O item atual é encerrado e a fila é descartada."
			if l, _ := m.logTab(); l != nil {
				l.Ask(m.question)
			}
			return m, nil, true
		case "space", "enter", "v", "/":
			m.notice = "bloqueado durante a execução: espere terminar ou interrompa (q)"
			return m, nil, true
		}
		m.notice = ""
		return m, nil, false
	}
	if k == "enter" && !m.tabs[m.active].Capturing() && m.modal == modalNone && m.planner != nil {
		return m, m.startReview(), true
	}
	return m, nil, false
}

func (m *Model) startReview() tea.Cmd {
	sel := m.selection()
	if sel.Empty() {
		m.notice = "marque ao menos um item (espaço) e tecle Enter"
		return nil
	}
	m.notice = ""
	m.mode, m.review = modeReview, newReview(m.theme)
	p, ctx := m.planner, m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return tea.Batch(func() tea.Msg {
		plan, err := p.Plan(ctx, sel)
		return planReadyMsg{plan: plan, err: err}
	}, tick())
}

func (m *Model) reviewKey(k string) (tea.Model, tea.Cmd, bool) {
	r := m.review
	switch k {
	case "ctrl+c":
		m.quit = true
		return m, tea.Quit, true
	case "q", "esc":
		if k == "q" {
			m.quit = true
			return m, tea.Quit, true
		}
		m.mode, m.review = modeNav, nil
	case "tab", "shift+tab", "left", "right", "h", "l":
		if r.canRun() {
			r.focus = 1 - r.focus
		}
	case "up", "k":
		r.scroll = max(r.scroll-1, 0)
	case "down", "j":
		r.scroll++
	case "pgup":
		r.scroll = max(r.scroll-(m.h-6), 0)
	case "pgdown":
		r.scroll += m.h - 6
	case "g", "home":
		r.scroll = 0
	case "enter":
		if r.focus == 1 && r.canRun() {
			return m, m.execute(), true
		}
		m.mode, m.review = modeNav, nil
	case "s":
		if r.canRun() {
			return m, m.execute(), true
		}
	}
	return m, nil, true
}

// execute dispara a execução: o executor roda em goroutine própria e conta por canal; a TUI só lê.
func (m *Model) execute() tea.Cmd {
	sel := m.selection()
	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan Event, 64)
	m.cancel, m.events, m.mode, m.review = cancel, ch, modeRun, nil
	if _, i := m.logTab(); i >= 0 {
		m.active = i
	}
	r := m.runner
	go r.Run(ctx, sel, ch)
	return tea.Batch(waitEvent(ch), tick())
}

func waitEvent(ch <-chan Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return runClosedMsg{}
		}
		return runEventMsg{ev: ev}
	}
}

func (m *Model) onRunEvent(ev Event) tea.Cmd {
	l, _ := m.logTab()
	if l != nil {
		switch ev.Kind {
		case EventRebootPrompt:
			m.asking, m.reply, m.question = askReboot, ev.Reply, ev.Prompt
			l.Ask(ev.Prompt)
		default:
			l.Apply(ev)
		}
	}
	return waitEvent(m.events)
}

func (m *Model) finishRun() {
	if m.cancel != nil {
		m.cancel()
	}
	m.cancel, m.events, m.mode = nil, nil, modeNav
	if l, _ := m.logTab(); l != nil && l.Running() {
		l.Apply(Event{Kind: EventDone}) // o executor fechou o canal sem EventDone: fecha o resumo
	}
}
