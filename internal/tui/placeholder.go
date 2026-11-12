package tui

import (
	tea "charm.land/bubbletea/v2"
)

// staticTab é a aba de estado vazio: serve até cada aba ganhar a sua implementação.
type staticTab struct {
	title, short, empty string
	hints               []KeyHint
}

// DefaultTabs são as quatro abas da barra, ainda com o estado vazio de cada uma.
func DefaultTabs() []Tab {
	return []Tab{
		&staticTab{"Apps", "Apps", "Lendo o catálogo…", []KeyHint{{"↑↓", "mover"}, {"espaço", "marcar"}, {"/", "buscar"}, {"Enter", "revisar"}}},
		&staticTab{"Receitas", "Rec", "Nenhuma receita para este sistema.", []KeyHint{{"↑↓", "mover"}, {"espaço", "marcar"}, {"Enter", "revisar"}}},
		&staticTab{"Diagnóstico", "Diag", "Nenhum diagnóstico para este sistema.", []KeyHint{{"↑↓", "mover"}, {"Enter", "rodar"}}},
		&staticTab{"Log", "Log", "Nada rodou nesta sessão. Marque apps ou receitas e tecle Enter.", []KeyHint{{"↑↓", "rolar"}, {"f", "seguir o fim"}}},
	}
}

func (t *staticTab) Init() tea.Cmd                 { return nil }
func (t *staticTab) Capturing() bool               { return false }
func (t *staticTab) Title() string                 { return t.title }
func (t *staticTab) Short() string                 { return t.short }
func (t *staticTab) Update(tea.Msg) (Tab, tea.Cmd) { return t, nil }
func (t *staticTab) View(int, int) string          { return " " + t.empty }
func (t *staticTab) Keys() []KeyHint               { return t.hints }
func (t *staticTab) Status() string                { return "" }
func (t *staticTab) More() int                     { return 0 }
func (t *staticTab) Selected() int                 { return 0 }
