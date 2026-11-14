package tui

import (
	"context"
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
	// LatestNotes e LatestURL são as notas e a página da versão nova (a tecla u as mostra).
	LatestNotes, LatestURL string
	// Updates entrega a versão nova que a consulta em segundo plano achar DEPOIS da abertura (nil = sem consulta).
	Updates <-chan UpdateInfo
	// IsAdmin diz se o processo roda como administrador (Windows) ou root (Unix): a linha 2 do cabeçalho mostra "admin: sim/não".
	IsAdmin  bool
	Mono     bool // sem cor, negrito nem reverso (NO_COLOR)
	NoMotion bool // reduzir movimento: spinner fixo e nenhum quadro agendado
	// Planner e Runner ligam a Revisão e a execução (nil: Enter só avisa que a execução não está ligada).
	Planner Planner
	Runner  Runner
}

type modal int

const (
	modalNone modal = iota
	modalHelp
	modalUpdate // notas da versão nova, com Fechar e Atualizar
)

// Model é a raiz: guarda tamanho, aba ativa e modal, e repassa o resto à aba ativa.
type Model struct {
	opts   Options
	theme  Theme
	tabs   []Tab // TODAS as seções, em lista plana (índice plano: execução, seleção, resultados e origens)
	active int   // índice plano da seção ativa
	// áreas (areas.go): cada uma agrupa seções por índice plano; lastSec lembra a última seção visitada de cada área.
	// areaMode falso = modo legado (New com abas planas: números e Tab percorrem as abas, sem a linha 2).
	areas    []Area
	areaMode bool
	lastSec  []int
	modal    modal
	w, h     int
	quit     bool

	// revisão e execução
	mode     appMode
	review   *reviewScreen
	planner  Planner
	runner   Runner
	ctx      context.Context
	cancel   context.CancelFunc // cancela a execução em andamento
	events   <-chan Event
	question string // pergunta [s/N] da raiz ("Interromper?")
	notice   string // aviso da linha de estado (espaço bloqueado durante a execução...)
	reply    chan<- bool
	asking   askKind
	// pedido de reinício que chegou com outro diálogo aberto: espera a vez (um diálogo por vez)
	pending   *rebootAsk
	reviewSeq int
	runDone   <-chan struct{} // ctx da execução: encerra as entregas de resposta pendentes

	updFocus   int  // botão do modal de versão nova: 0 Fechar (padrão), 1 Atualizar
	wantUpdate bool // o usuário escolheu Atualizar: o Run devolve ErrUpdateRequested
	updScroll  int

	// resultados de execuções por índice de aba (a faixa da aba de origem); origins são as abas que tinham marcados
	results map[int]*Result
	origins []int
}

// UpdateInfo é uma versão nova (tag, notas já limpas, página da release).
type UpdateInfo struct{ Tag, Notes, URL string }

// UpdateInfoMsg entrega uma versão nova achada depois da abertura (o Run a envia a partir de Options.Updates).
type UpdateInfoMsg UpdateInfo

// UpdateRequested diz se o usuário escolheu Atualizar no modal de versão nova.
func (m *Model) UpdateRequested() bool { return m.wantUpdate }

type rebootAsk struct {
	prompt string
	reply  chan<- bool
}

type askKind int

const (
	askNone askKind = iota
	askInterrupt
	askReboot
)

// SetExec liga o planejador e o executor (a TUI só orquestra: reaproveita o plano do install e a engine).
func (m *Model) SetExec(ctx context.Context, p Planner, r Runner) {
	m.ctx, m.planner, m.runner = ctx, p, r
}

// New monta a raiz com as abas dadas (a ordem é a da barra: 1 Apps, 2 Receitas, 3 Diagnóstico, 4 Log).
func New(o Options, tabs ...Tab) *Model {
	if len(tabs) == 0 {
		tabs = DefaultTabs()
	}
	// tema escuro até a detecção do terminal chegar
	return &Model{opts: o, theme: Theme{Mono: o.Mono, Dark: true, NoMotion: o.NoMotion}, tabs: tabs, areas: legacyAreas(tabs), lastSec: make([]int, len(tabs)), w: 80, h: 24, results: map[int]*Result{}}
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
	case UpdateInfoMsg:
		if m.opts.Latest == "" { // o cache vem primeiro; a consulta só preenche quando não havia nada
			m.opts.Latest, m.opts.LatestNotes, m.opts.LatestURL = msg.Tag, msg.Notes, msg.URL
		}
		return m, nil
	case GoToMsg:
		m.goTo(msg)
		return m, nil
	case planReadyMsg:
		// só o plano DESTA Revisão vale: um plano atrasado de uma Revisão anterior (outra seleção) é ignorado
		if m.review != nil && m.review.id == msg.id {
			m.review.loading, m.review.plan, m.review.err = false, msg.plan, msg.err
		}
		return m, nil
	case runEventMsg:
		return m, m.onRunEvent(msg.ev)
	case runClosedMsg:
		m.finishRun()
		// depois da execução as abas que mostram o estado do sistema o releem (Apps, Receitas)
		var cmds []tea.Cmd
		for _, t := range m.tabs {
			if r, ok := t.(Reloader); ok {
				cmds = append(cmds, r.Reload())
			}
		}
		return m, tea.Batch(cmds...)
	case tickMsg:
		if m.review != nil && m.review.loading {
			m.review.frame++
		}
	case tea.KeyPressMsg:
		if m.w < MinWidth || m.h < MinHeight {
			// Terminal pequeno demais: a tela só mostra o aviso, então nenhuma tecla pode agir sobre o que está oculto (uma
			// confirmação aberta nunca aceita às cegas). Só Ctrl+C (sair) e Esc (cancelar, que é seguro) passam.
			k := msg.String()
			quit := k == "q" && m.modal == modalNone && !m.tabs[m.active].Capturing() // sair continua possível (não é confirmação)
			if k != "ctrl+c" && k != "esc" && !quit {
				return m, nil
			}
		}
		// a seção que usa Enter e os números para si (Início) os recebe antes da Revisão e da troca de área, mas só na
		// navegação normal: com Revisão, execução, modal ou pergunta aberta valem as regras da raiz.
		if m.modal == modalNone && m.mode == modeNav && m.asking == askNone {
			if c, ok := m.tabs[m.active].(KeyClaimer); ok && c.Claims(msg.String()) {
				return m, m.toActive(msg)
			}
		}
		if next, cmd, handled := m.execKey(msg.String()); handled {
			return next, cmd
		}
		// aba digitando ou com modal: as teclas são dela (só Ctrl+C sai sempre)
		if m.modal == modalNone && m.tabs[m.active].Capturing() && msg.String() != "ctrl+c" {
			return m, m.toActive(msg)
		}
		// x fecha a faixa de resultado da aba ativa (só com a faixa na tela; sem ela a tecla é da aba)
		if msg.String() == "x" && m.modal == modalNone && m.dismissResult() {
			return m, nil
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
	if m.modal == modalUpdate {
		return m.updateModalKey(k)
	}
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
	case "u":
		if m.opts.Latest == "" {
			return m, nil, false // sem versão nova a tecla é da aba
		}
		m.modal, m.updFocus, m.updScroll = modalUpdate, 0, 0
		return m, nil, true
	}
	if m.areaMode {
		return m, nil, m.areaKey(k)
	}
	switch k {
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

// updateModalKey: o modal de versão nova. Fechar é o padrão (Enter e Esc fecham); Atualizar sai da TUI e o
// chamador roda `toolkit update` (nunca atualiza por dentro da tela).
func (m *Model) updateModalKey(k string) (tea.Model, tea.Cmd, bool) {
	switch k {
	case "ctrl+c", "q":
		m.quit = true
		return m, tea.Quit, true
	case "esc", "u":
		m.modal = modalNone
	case "tab", "shift+tab", "left", "right", "h", "l":
		m.updFocus = 1 - m.updFocus
	case "up", "k":
		m.updScroll = max(m.updScroll-1, 0)
	case "down", "j":
		m.updScroll++
	case "a":
		return m.requestUpdate()
	case "enter":
		if m.updFocus == 1 {
			return m.requestUpdate()
		}
		m.modal = modalNone
	}
	return m, nil, true
}

func (m *Model) requestUpdate() (tea.Model, tea.Cmd, bool) {
	m.wantUpdate, m.quit = true, true
	return m, tea.Quit, true
}
