package tui

import (
	"context"
	"slices"

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
		case *DiagTab:
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
	// pergunta [s/N] aberta: só s/S (sim) e n/N/Esc (não) respondem; qualquer outra tecla é ignorada, nunca
	// vira "não" por acidente nem abre outro diálogo por cima (um diálogo por vez).
	if m.asking != askNone {
		switch k {
		case "s", "S":
			m.answer(true)
		case "n", "N", "esc":
			m.answer(false)
		}
		return m, nil, true
	}
	switch m.mode {
	case modeReview:
		return m.reviewKey(k)
	case modeRun:
		switch k {
		case "ctrl+c", "q":
			m.open(askInterrupt, "Interromper? O item atual é encerrado e a fila é descartada.", nil)
			return m, nil, true
		case "u":
			if m.opts.Latest != "" {
				m.notice = "atualizar fica indisponível durante a execução: espere terminar ou interrompa (q)"
				return m, nil, true
			}
		case "space", "enter", "v", "/":
			m.notice = "bloqueado durante a execução: espere terminar ou interrompa (q)"
			return m, nil, true
		}
		m.notice = ""
		return m, nil, false
	}
	if k == "enter" && !m.tabs[m.active].Capturing() && m.modal == modalNone {
		if m.planner == nil || m.runner == nil {
			// transição: a execução pela TUI chega no PR seguinte; os comandos do CLI já fazem tudo
			m.notice = "executar pela TUI ainda não está ligado: use toolkit install ou toolkit recipe run"
			return m, nil, true
		}
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
	m.reviewSeq++
	id := m.reviewSeq
	m.mode, m.review = modeReview, newReview(m.theme, id, sel)
	p, ctx := m.planner, m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return tea.Batch(func() tea.Msg {
		plan, err := p.Plan(ctx, sel)
		return planReadyMsg{id: id, plan: plan, err: err}
	}, m.theme.Tick())
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
	// Executar vale só para o plano EXIBIDO: a seleção é a que foi planejada, não a das abas agora
	sel := m.review.sel
	sel.Approved = slices.Clone(m.review.plan.Approved)
	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan Event, 64)
	m.cancel, m.events, m.mode, m.review, m.runDone = cancel, ch, modeRun, nil, ctx.Done()
	m.origins = m.origins[:0]
	for i, t := range m.tabs {
		var n int
		switch x := t.(type) {
		case *AppsTab:
			n = len(x.Marked())
		case *RecipesTab:
			n = len(x.Marked())
		case *DiagTab:
			n = len(x.Marked())
		}
		if n > 0 {
			m.origins = append(m.origins, i)
			delete(m.results, i) // a faixa da execução anterior dá lugar à desta
		}
	}
	if _, i := m.logTab(); i >= 0 {
		m.setActive(i)
	}
	r := m.runner
	go r.Run(ctx, sel, ch)
	return tea.Batch(waitEvent(ch), m.theme.Tick())
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
			if m.asking != askNone { // outro diálogo aberto: este espera a vez
				m.pending = &rebootAsk{prompt: ev.Prompt, reply: ev.Reply}
			} else {
				m.open(askReboot, ev.Prompt, ev.Reply)
			}
		default:
			l.Apply(ev)
		}
	}
	return waitEvent(m.events)
}

// open abre UM diálogo [s/N] (só um por vez).
func (m *Model) open(k askKind, q string, reply chan<- bool) {
	m.asking, m.question, m.reply = k, q, reply
	if l, _ := m.logTab(); l != nil {
		l.Ask(q)
	}
}

// answer fecha o diálogo aberto com a resposta e abre o pedido de reinício que esperava a vez, se houver.
func (m *Model) answer(yes bool) {
	kind, reply := m.asking, m.reply
	m.asking, m.question, m.reply = askNone, "", nil
	if l, _ := m.logTab(); l != nil {
		l.Ask("")
	}
	switch kind {
	case askInterrupt:
		if yes && m.cancel != nil {
			m.cancel()            // encerra o item atual e descarta a fila
			if m.pending != nil { // a execução acabou: o reinício pendente recebe "não"
				m.deliver(m.pending.reply, false)
				m.pending = nil
			}
		}
	case askReboot:
		m.deliver(reply, yes)
	}
	if m.asking == askNone && m.pending != nil {
		p := m.pending
		m.pending = nil
		m.open(askReboot, p.prompt, p.reply)
	}
}

// deliver entrega a resposta SEM bloquear o loop da TUI: se o executor já saiu (ou o Reply não tem leitor),
// a goroutine desiste quando a execução termina (ctx cancelado em finishRun).
func (m *Model) deliver(reply chan<- bool, v bool) {
	if reply == nil {
		return
	}
	select { // com leitor esperando (ou buffer livre) entrega na hora, sem depender de sorteio com o cancelamento
	case reply <- v:
		return
	default:
	}
	done := m.runDone
	go func() {
		select {
		case reply <- v:
		case <-done:
		}
	}()
}

func (m *Model) finishRun() {
	if m.cancel != nil {
		m.cancel()
	}
	m.asking, m.question, m.reply, m.pending = askNone, "", nil, nil
	m.cancel, m.events, m.mode = nil, nil, modeNav
	if l, _ := m.logTab(); l != nil && l.Running() {
		l.Apply(Event{Kind: EventDone}) // o executor fechou o canal sem EventDone: fecha o resumo
	}
	// o resultado vai para a faixa das abas de ORIGEM (a raiz é a dona: Apps/Receitas/Diagnóstico não conhecem a execução)
	if l, _ := m.logTab(); l != nil {
		if out := l.Outcome(); out != nil {
			for _, i := range m.origins {
				cp := *out
				m.results[i] = &cp
			}
		}
	}
	m.origins = nil
}
