// Package resume registra, por sistema operacional, o que faz `toolkit resume` rodar sozinho depois
// de um reboot (Tarefa Agendada no Windows, unit systemd no Linux, LaunchAgent no macOS).
package resume

import "context"

// Spec descreve a retomada a registrar.
type Spec struct {
	Executable string   // caminho estável do binário (copiado para BinDir antes de registrar)
	Args       []string // sempre ["resume"] + flags herdadas (--yes, --verbose)
	User       string   // usuário que iniciou a receita
	Elevated   bool     // a retomada precisa de privilégio (receita requires_admin)
	LogPath    string
}

// Registrar registra e remove a retomada. As implementações só montam comandos (exec.Cmd) e o conteúdo
// de arquivos (unit, plist); nada além disso é executado fora do exec.Runner.
type Registrar interface {
	Register(ctx context.Context, s Spec) error
	Unregister(ctx context.Context) error // idempotente: não registrado = sucesso
	IsRegistered(ctx context.Context) (bool, error)
}
