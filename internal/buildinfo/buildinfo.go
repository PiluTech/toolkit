// Package buildinfo centraliza o nome do produto e os dados de build.
//
// O nome é provisório: trocá-lo aqui basta para renomear o binário nos textos,
// no label do launchd e na tarefa agendada (dev.pilutech.<Name>).
package buildinfo

import "fmt"

// Name é o nome do produto e do binário.
const Name = "toolkit"

// Preenchidos em build com -ldflags "-X github.com/PiluVitu/toolkit/internal/buildinfo.Version=..." etc.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Identifier devolve o identificador reverso usado em launchd e tarefas agendadas.
func Identifier() string { return "dev.pilutech." + Name }

// String descreve a versão em uma linha.
func String() string {
	return fmt.Sprintf("%s %s (commit %s, build %s)", Name, Version, Commit, Date)
}
