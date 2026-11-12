// Package cli monta os comandos Cobra. Nenhuma lógica de negócio fica aqui.
package cli

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/PiluVitu/toolkit/internal/buildinfo"
	"github.com/PiluVitu/toolkit/internal/msg"
)

// NewRootCmd cria o comando raiz com saída e erro injetáveis (testes usam buffers).
func NewRootCmd(out, errOut io.Writer) *cobra.Command {
	return NewRootCmdWith(out, errOut, DefaultDeps())
}

// NewRootCmdWith permite injetar as dependências (testes).
func NewRootCmdWith(out, errOut io.Writer, deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           buildinfo.Name,
		Short:         msg.RootShort,
		Long:          msg.RootLong,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.AddCommand(newVersionCmd(), newListCmd(deps), newInstallCmd(deps))
	root.AddCommand(newRecipeCmds(deps)...)
	return root
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
	root := NewRootCmd(os.Stdout, os.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		_, _ = io.WriteString(os.Stderr, "erro: "+err.Error()+"\n")
		return 1
	}
	return 0
}
