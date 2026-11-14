package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Theme reúne os tokens visuais. Três regras (docs/tui.md): cor só no símbolo e na palavra de estado,
// nenhum fundo além do reverso, e nada depende de cor, negrito ou reverso para ser entendido (todo
// estado tem símbolo E palavra; a aba ativa leva colchetes; o cursor leva ">"). Em Mono (NO_COLOR ou
// --no-color) nenhuma sequência de estilo é emitida.
type Theme struct {
	Mono bool // sem cor, negrito nem reverso
	Dark bool // fundo escuro (detectado pelo terminal; padrão escuro)
	// NoMotion: "reduzir movimento" (TOOLKIT_REDUCE_MOTION=1 ou --no-motion): o spinner vira o símbolo fixo "~" e nenhum
	// quadro é agendado, então a tela só muda quando o ESTADO muda (nada é redesenhado a cada 100 ms, o que também
	// poupa leitores de tela).
	NoMotion bool
}

func (t Theme) pick(light, dark int) lipgloss.Style {
	if t.Mono {
		return lipgloss.NewStyle()
	}
	n := light
	if t.Dark {
		n = dark
	}
	return lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(n))
}

func (t Theme) attr(bold, reverse bool) lipgloss.Style {
	if t.Mono {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Bold(bold).Reverse(reverse)
}

// SymBusy é o símbolo fixo de "em andamento" quando o movimento está reduzido (sempre com a palavra do estado ao lado).
const SymBusy = "~"

// Spin devolve o quadro do spinner (ou o símbolo fixo com o movimento reduzido).
func (t Theme) Spin(frame int) string {
	if t.NoMotion {
		return SymBusy
	}
	return spinnerFrames[frame%len(spinnerFrames)]
}

// Tick agenda o próximo quadro do spinner; com o movimento reduzido não agenda nada.
func (t Theme) Tick() tea.Cmd {
	if t.NoMotion {
		return nil
	}
	return tick()
}

// Muted serve a horários, dicas e cabeçalho de colunas.
func (t Theme) Muted() lipgloss.Style { return t.pick(8, 7) }

// Accent serve à marca [x], ao spinner e à seta de versão nova.
func (t Theme) Accent() lipgloss.Style { return t.pick(4, 14) }

// OK serve ao ✓ (sempre com a palavra do estado).
func (t Theme) OK() lipgloss.Style { return t.pick(2, 10) }

// Warn serve ao ! e ao rótulo do aviso.
func (t Theme) Warn() lipgloss.Style { return t.pick(3, 11).Bold(!t.Mono) }

// Err serve ao ✗ e a "erro:".
func (t Theme) Err() lipgloss.Style { return t.pick(1, 9).Bold(!t.Mono) }

// Title serve a categorias e títulos de bloco (em Mono, quem chama escreve em MAIÚSCULAS).
func (t Theme) Title() lipgloss.Style { return t.attr(true, false) }

// Active serve à aba ativa e ao botão em foco.
func (t Theme) Active() lipgloss.Style { return t.attr(true, true) }

// Bold serve ao nome sob o cursor.
func (t Theme) Bold() lipgloss.Style { return t.attr(true, false) }

// Símbolos. Cada um tem a palavra do estado ao lado; o conjunto ASCII (--ascii) fica para depois.
const (
	SymOK      = "✓"
	SymFail    = "✗"
	SymWarn    = "!"
	SymPending = "○"
	SymSkipped = "-"
	SymMore    = "↓"
	SymUpdate  = "↑"
	SymRule    = "─"
)
