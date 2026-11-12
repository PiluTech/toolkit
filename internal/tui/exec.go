package tui

import (
	"context"
	"time"
)

// Selection é o que o usuário marcou nas abas (apps e receitas/diagnósticos), na ordem do lote:
// apps primeiro, depois receitas, a que reinicia por último (docs/tui.md, decisão 3).
type Selection struct {
	Apps    []Mark
	Recipes []RecipeMark
}

// Empty diz se não há nada marcado.
func (s Selection) Empty() bool { return len(s.Apps) == 0 && len(s.Recipes) == 0 }

// Plan é o que a Revisão mostra, já pronto: o mesmo passe do `toolkit install --dry-run` (DryRunRunner
// com leituras reais), nunca um segundo planejador. Commands vêm exatamente como serão executados
// (sudo -n, Env, `--`); script PowerShell vai legível com a nota "(enviado como -EncodedCommand)".
type Plan struct {
	Summary  string   // "Vai instalar 3 apps e rodar 1 receita. Nada foi executado ainda."
	Env      string   // "Linux amd64 · usuário ana"
	Warnings []string // AVISOS: sudo -n, administrador, reinício, runtimes do flatpak...
	Changes  []string // O QUE MUDA
	Commands []string // EXATAMENTE COMO SERÁ EXECUTADO
	// Problems: itens não planejáveis. Com algum, nada é instalado e a Revisão só oferece Voltar.
	Problems []string
	// NothingToDo: tudo já está instalado.
	NothingToDo bool
}

// Planner calcula o plano de uma seleção (leituras reais, mutações só registradas).
type Planner interface {
	Plan(ctx context.Context, sel Selection) (Plan, error)
}

// ItemStatus é o resultado de um item do lote.
type ItemStatus string

// Resultados de item.
const (
	ItemQueued   ItemStatus = "queued"
	ItemRunning  ItemStatus = "running"
	ItemOK       ItemStatus = "ok"
	ItemSkipped  ItemStatus = "skipped"
	ItemFailed   ItemStatus = "failed"
	ItemReboot   ItemStatus = "reboot"    // aguardando reinício
	ItemNotRun   ItemStatus = "not-run"   // descartado (interrompido ou falha anterior)
	ItemCanceled ItemStatus = "cancelled" // interrompido
)

// Event é o que o executor conta à TUI. Os campos usados dependem de Kind.
type Event struct {
	Kind     EventKind
	Index    int    // item do lote
	Name     string // EventItems/EventItemStarted
	Line     string // EventLog
	Status   ItemStatus
	Err      string
	Duration time.Duration
	Items    []string // EventItems: nomes de todos os itens, na ordem
	Prompt   string   // EventRebootPrompt: texto da pergunta
	Reply    chan<- bool
}

// EventKind é o tipo de Event.
type EventKind int

// Tipos de evento.
const (
	EventItems EventKind = iota // a fila (nomes) antes de começar
	EventItemStarted
	EventLog
	EventItemDone
	EventRebootPrompt // pergunta [s/N] na linha de estado; a resposta vai em Reply
	EventDone
)

// Runner executa a seleção reaproveitando providers.InstallApp e recipes.Engine (a TUI só injeta o
// Prompter). Envia eventos até o fim e fecha o canal; cancelar o ctx encerra o item atual e descarta a fila.
type Runner interface {
	Run(ctx context.Context, sel Selection, events chan<- Event)
}
