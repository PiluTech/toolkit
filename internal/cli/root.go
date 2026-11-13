// Package cli monta os comandos Cobra. Nenhuma lógica de negócio fica aqui.
package cli

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/buildinfo"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/update"
)

// NewRootCmd cria o comando raiz com saída e erro injetáveis (testes usam buffers).
func NewRootCmd(out, errOut io.Writer) *cobra.Command {
	return NewRootCmdWith(out, errOut, DefaultDeps())
}

// NewRootCmdWith permite injetar as dependências (testes).
func NewRootCmdWith(out, errOut io.Writer, deps Deps) *cobra.Command {
	root, _ := newRoot(out, errOut, deps)
	return root
}

// newRoot monta o comando raiz e o notificador de versão nova (que o Execute fecha no fim do comando).
func newRoot(out, errOut io.Writer, deps Deps) (*cobra.Command, *updateNotifier) {
	notifier := newUpdateNotifier(deps)
	root := &cobra.Command{
		Use:           buildinfo.Name,
		Short:         msg.RootShort,
		Long:          msg.RootLong,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	var noTUI, noUpdateCheck bool
	root.Flags().BoolVar(&noTUI, "no-tui", false, msg.RootNoTUI)
	root.PersistentFlags().BoolVar(&noUpdateCheck, "no-update-check", false, msg.FlagNoUpdateCheck)
	root.PersistentPreRun = func(cmd *cobra.Command, _ []string) { notifier.start(cmd, noUpdateCheck) }
	root.Args = cobra.NoArgs
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		return runRoot(cmd, deps, RecipeDeps{}, noTUI, notifier)
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.AddCommand(newVersionCmd(), newListCmd(deps), newInstallCmd(deps), newUpdateCmd(deps), newTUICmd(deps, RecipeDeps{}, notifier))
	root.AddCommand(newRecipeCmds(deps)...)
	return root, notifier
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: msg.VersionShort,
		Long:  msg.VersionLong,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println(buildinfo.String())
		},
	}
}

// Execute roda o CLI e devolve o código de saída do processo.
func Execute(ctx context.Context) int {
	if exe, err := os.Executable(); err == nil {
		update.CleanupOld(exe) // sobra ".old" de uma atualização no Windows (melhor esforço)
	}
	return ExecuteWith(ctx, os.Stdout, os.Stderr, DefaultDeps(), os.Args[1:])
}

// ExecuteWith é o Execute com saída e dependências injetáveis (testes). No fim, com sucesso OU erro, escreve a
// linha de versão nova em errOut (uma só, nunca em out) quando houver.
func ExecuteWith(ctx context.Context, out, errOut io.Writer, deps Deps, args []string) int {
	root, notifier := newRoot(out, errOut, deps)
	if args == nil {
		args = []string{}
	}
	root.SetArgs(args)
	code := 0
	if err := root.ExecuteContext(ctx); err != nil {
		_, _ = io.WriteString(errOut, "erro: "+err.Error()+"\n")
		code = 1
	}
	notifier.finish(errOut)
	return code
}
