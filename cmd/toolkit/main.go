// Comando toolkit: só chama o CLI; nada de lógica aqui.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/PiluVitu/toolkit/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Execute(ctx)
	stop()
	os.Exit(code)
}
