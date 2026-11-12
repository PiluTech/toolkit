package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

// Run abre a TUI e devolve quando o usuário sai. Quem chama decide ANTES se há terminal (sem terminal,
// --no-tui ou TERM=dumb, a TUI não abre: ver internal/cli).
func Run(ctx context.Context, o Options, tabs ...Tab) error {
	p := tea.NewProgram(New(o, tabs...), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}
