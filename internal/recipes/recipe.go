// Package recipes carrega e valida receitas multi-etapa (recipes/<so>/<id>.yaml) e as executa (engine).
// A validação roda sobre yaml.Node, então cada erro aponta arquivo, linha e coluna, com o mesmo rigor
// do catálogo.
package recipes

import (
	"time"

	"github.com/PiluTech/toolkit/internal/platform"
)

// Kind é o tipo da receita.
type Kind string

// Tipos de receita.
const (
	KindSetup      Kind = "setup"
	KindDiagnostic Kind = "diagnostic"
)

// RebootPolicy diz quando uma etapa reinicia a máquina.
type RebootPolicy string

// Políticas de reboot.
const (
	RebootNever    RebootPolicy = "never"
	RebootRequired RebootPolicy = "required"
	RebootIfNeeded RebootPolicy = "if-needed"
)

// Recipe é uma receita validada.
type Recipe struct {
	ID            string
	Name          string
	Description   string
	OS            platform.OS
	Kind          Kind
	RequiresAdmin bool
	Steps         []Step
	// Hash é o sha256 (hex) do YAML: a retomada recusa a receita se ela mudou entre os boots.
	Hash string
}

// Step é uma etapa. Exatamente um entre Run e Install.
type Step struct {
	ID      string
	Name    string
	Check   *Action  // opcional: saiu com 0 = já está feito, etapa pulada
	Run     *Action  // exatamente um entre Run e Install
	Install []string // ids do catálogo, aceita "python@3.12"
	// Render (só diagnóstico, com run): nome de um renderizador de internal/diagfmt que transforma a saída crua da
	// etapa num resumo legível. A saída original só aparece no modo detalhes.
	Render  string
	Reboot  RebootPolicy
	Timeout time.Duration
	Line    int
}

// Action tem exatamente um campo preenchido (validado na carga).
type Action struct {
	PowerShell   string
	Sh           string
	Bash         string
	WSL          *WSLAction
	SuccessCodes []int // além do 0 (ex.: [3010] para o dism que pede reboot)
}

// WSLAction roda um bash dentro de uma distro WSL.
type WSLAction struct {
	Distro string
	User   string // vazio: usuário padrão da distro
	Bash   string
}

// OKExit diz se o exit code conta como sucesso para a ação.
func (a *Action) OKExit(code int) bool {
	if code == 0 {
		return true
	}
	for _, c := range a.SuccessCodes {
		if c == code {
			return true
		}
	}
	return false
}

// Script devolve o texto executado pela ação (para o plano).
func (a *Action) Script() string {
	switch {
	case a.PowerShell != "":
		return a.PowerShell
	case a.Sh != "":
		return a.Sh
	case a.Bash != "":
		return a.Bash
	case a.WSL != nil:
		return a.WSL.Bash
	}
	return ""
}
