package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// staticTab é a aba de estado vazio: serve até cada aba ganhar a sua implementação. Mostra o estado padronizado:
// carregando (loading) ou vazio, com os passos do que fazer.
type staticTab struct {
	title, short, what string
	loading            bool
	steps              []string
	theme              Theme
	hints              []KeyHint
}

// DefaultTabs são as quatro abas da barra, ainda com o estado vazio de cada uma.
func DefaultTabs(th Theme) []Tab {
	return []Tab{
		&staticTab{title: "Apps", short: "Apps", what: "o catálogo", loading: true, theme: th, hints: []KeyHint{{"↑↓", "mover"}, {"espaço", "marcar"}, {"/", "buscar"}, {"Enter", "revisar"}}},
		&staticTab{title: "Receitas", short: "Rec", what: "Nenhuma receita para este sistema.", theme: th, steps: []string{"Veja as receitas do catálogo: toolkit recipe list"}, hints: []KeyHint{{"↑↓", "mover"}, {"espaço", "marcar"}, {"Enter", "revisar"}}},
		&staticTab{title: "Diagnóstico", short: "Diag", what: "Nenhum diagnóstico para este sistema.", theme: th, steps: []string{"Veja o que o toolkit sabe diagnosticar: toolkit diagnostico --help"}, hints: []KeyHint{{"↑↓", "mover"}, {"Enter", "rodar"}}},
		&staticTab{title: "Log", short: "Log", what: "Nada rodou nesta sessão.", theme: th, steps: []string{"Marque apps ou receitas e tecle Enter."}, hints: []KeyHint{{"↑↓", "rolar"}, {"f", "seguir o fim"}}},
	}
}

func (t *staticTab) Init() tea.Cmd                 { return nil }
func (t *staticTab) Capturing() bool               { return false }
func (t *staticTab) Title() string                 { return t.title }
func (t *staticTab) Short() string                 { return t.short }
func (t *staticTab) Update(tea.Msg) (Tab, tea.Cmd) { return t, nil }
func (t *staticTab) View(w, h int) string {
	if t.loading {
		return strings.Join(StateLoading(t.theme, w, h, t.what, 0), "\n")
	}
	return strings.Join(StateEmpty(t.theme, w, h, t.what, t.steps...), "\n")
}
func (t *staticTab) Keys() []KeyHint { return t.hints }
func (t *staticTab) Status() string  { return "" }
func (t *staticTab) More() int       { return 0 }
func (t *staticTab) Selected() int   { return 0 }
