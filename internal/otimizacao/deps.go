package otimizacao

import (
	"context"
	"errors"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/recipes"
)

// Deps são as portas do mundo: tudo que o coletor usa passa por aqui, então os testes injetam saídas falsas.
type Deps struct {
	// Runner executa os comandos de leitura. O pacote o embrulha com exec.ReadOnlyRunner: comando que não seja de
	// leitura, ou com Elevate, é recusado.
	Runner exec.Runner
	Now    func() time.Time // nil = time.Now
	Lang   Lang
	// Admin: o terminal é de administrador (só muda a mensagem de uma leitura que o Windows negou). Nada é elevado.
	Admin bool
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Deps) t(pt, en, es string) string { return diagfmt.T(d.Lang, pt, en, es) }

var errNoRunner = errors.New("sem Runner")

// run executa um comando de LEITURA. Erro de execução ou exit != 0 volta como erro: o chamador registra Skipped e segue.
func (d Deps) run(ctx context.Context, c exec.Cmd) (string, error) {
	if d.Runner == nil {
		return "", errNoRunner
	}
	c.ReadOnly = true
	res, err := exec.ReadOnlyRunner(d.Runner).Run(ctx, c)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return string(res.Stdout), errors.New("o comando saiu com código " + itoa(res.ExitCode))
	}
	return string(res.Stdout), nil
}

// psCmd monta um Cmd de leitura que roda um script do PowerShell (caminho absoluto, -EncodedCommand), como as receitas.
func psCmd(script string, timeout time.Duration) exec.Cmd {
	c, _ := recipes.BuildCmd(&recipes.Action{PowerShell: script}, platform.Windows, timeout, false, true)
	return c
}
