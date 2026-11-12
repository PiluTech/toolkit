package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
)

// ContextSetter é implementada pelas abas que fazem trabalho assíncrono (verificar apps, ler versões):
// o Run entrega a elas o contexto que ELE cancela ao sair.
type ContextSetter interface{ SetContext(ctx context.Context) }

// ErrUpdateRequested é o que o Run devolve quando o usuário escolheu Atualizar no modal de versão nova: quem
// chama fecha a TUI e roda `toolkit update`.
var ErrUpdateRequested = errors.New("atualização pedida")

// Run abre a TUI e devolve quando o usuário sai. Quem chama decide ANTES se há terminal (sem terminal,
// --no-tui ou TERM=dumb, a TUI não abre: ver internal/cli).
//
// Ao sair (q, Ctrl+C ou cancelamento de ctx) o contexto das abas é cancelado: nenhuma verificação de
// app ou consulta de versão continua viva depois que o Run volta.
func Run(ctx context.Context, o Options, tabs ...Tab) error {
	return run(ctx, o, nil, tabs...)
}

func run(ctx context.Context, o Options, popts []tea.ProgramOption, tabs ...Tab) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, t := range tabs {
		if cs, ok := t.(ContextSetter); ok {
			cs.SetContext(ctx)
		}
	}
	popts = append(popts, tea.WithContext(ctx))
	m := New(o, tabs...)
	m.SetExec(ctx, o.Planner, o.Runner)
	p := tea.NewProgram(m, popts...)
	if o.Updates != nil {
		go func() { // termina com o ctx (cancelado ao sair): não sobra goroutine
			select {
			case u := <-o.Updates:
				p.Send(UpdateInfoMsg(u))
			case <-ctx.Done():
			}
		}()
	}
	_, err := p.Run()
	cancel() // antes de voltar: as chamadas em andamento acordam e terminam
	if err == nil && m.UpdateRequested() {
		return ErrUpdateRequested
	}
	return err
}
