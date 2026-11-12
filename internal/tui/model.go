package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Mínimo para desenhar a TUI: abaixo disso a tela vira um aviso (docs/tui.md).
const (
	MinWidth  = 60
	MinHeight = 16
)

// Options configura a TUI.
type Options struct {
	Version string // versão em execução ("v0.1.0")
	Latest  string // versão nova disponível, vazia = nenhuma (faixa "↑ v0.2.0 disponível (u)")
	Mono    bool   // sem cor, negrito nem reverso (NO_COLOR)
}

type modal int

const (
	modalNone modal = iota
	modalHelp
)

// Model é a raiz: guarda tamanho, aba ativa e modal, e repassa o resto à aba ativa.
type Model struct {
	opts   Options
	theme  Theme
	tabs   []Tab
	active int
	modal  modal
	w, h   int
	quit   bool
}

// New monta a raiz com as abas dadas (a ordem é a da barra: 1 Apps, 2 Receitas, 3 Diagnóstico, 4 Log).
func New(o Options, tabs ...Tab) *Model {
	if len(tabs) == 0 {
		tabs = DefaultTabs()
	}
	// tema escuro até a detecção do terminal chegar
	return &Model{opts: o, theme: Theme{Mono: o.Mono, Dark: true}, tabs: tabs, w: 80, h: 24}
}

// Init pede a cor de fundo do terminal (claro ou escuro).
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestBackgroundColor}
	for _, t := range m.tabs {
		cmds = append(cmds, t.Init())
	}
	return tea.Batch(cmds...)
}

// Active devolve o índice da aba ativa (para os testes).
func (m *Model) Active() int { return m.active }

// Quitting diz se a TUI foi encerrada.
func (m *Model) Quitting() bool { return m.quit }

// Update trata tamanho, teclas globais e repassa o resto à aba ativa.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case tea.BackgroundColorMsg:
		if !m.opts.Mono {
			m.theme.Dark = msg.IsDark()
		}
		return m, nil
	case tea.KeyPressMsg:
		// aba digitando ou com modal: as teclas são dela (só Ctrl+C sai sempre)
		if m.modal == modalNone && m.tabs[m.active].Capturing() && msg.String() != "ctrl+c" {
			return m, m.toActive(msg)
		}
		if next, cmd, handled := m.global(msg.String()); handled {
			return next, cmd
		}
		return m, m.toActive(msg)
	}
	// o resto (dados carregados, spinner, fim de execução) vai a TODAS as abas: cada uma ignora o que
	// não é dela, e a aba de fundo não perde resultado.
	cmds := make([]tea.Cmd, 0, len(m.tabs))
	for i, t := range m.tabs {
		tab, cmd := t.Update(msg)
		m.tabs[i] = tab
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) toActive(msg tea.Msg) tea.Cmd {
	tab, cmd := m.tabs[m.active].Update(msg)
	m.tabs[m.active] = tab
	return cmd
}

// global trata as teclas da raiz: abas, ajuda, voltar e sair.
func (m *Model) global(k string) (tea.Model, tea.Cmd, bool) {
	// o modal consome tudo, menos sair
	if m.modal != modalNone {
		switch k {
		case "esc", "?", "enter", "q":
			m.modal = modalNone
		case "ctrl+c":
			m.quit = true
			return m, tea.Quit, true
		}
		return m, nil, true
	}
	switch k {
	case "ctrl+c", "q":
		m.quit = true
		return m, tea.Quit, true
	case "?":
		m.modal = modalHelp
		return m, nil, true
	case "tab":
		m.active = (m.active + 1) % len(m.tabs)
		return m, nil, true
	case "shift+tab":
		m.active = (m.active + len(m.tabs) - 1) % len(m.tabs)
		return m, nil, true
	}
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
		if i := int(k[0] - '1'); i < len(m.tabs) {
			m.active = i
			return m, nil, true
		}
	}
	return m, nil, false
}

// View desenha a tela inteira.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *Model) render() string {
	if m.w < MinWidth || m.h < MinHeight {
		return strings.TrimRight(m.tooSmall(), "\n")
	}
	return m.screen()
}
