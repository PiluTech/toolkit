package recipes_test

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	toolkit "github.com/PiluTech/toolkit"
	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/recipes"
)

// Smoke de formato com a máquina REAL do runner (só leitura: WMI/CIM, /proc, lsblk, sysctl, system_profiler): a saída
// verdadeira de cada etapa do diag-memoria tem de ser entendida pelo renderizador. É o que pega a diferença entre a
// saída que imaginamos e a do sistema (o Windows só roda aqui no runner windows-latest do CI). Nada é gravado.
func TestDiagMemoriaRealOutputIsUnderstoodByTheRenderers(t *testing.T) {
	var os platform.OS
	switch runtime.GOOS {
	case "windows":
		os = platform.Windows
	case "linux":
		os = platform.Linux
	case "darwin":
		os = platform.Darwin
	default:
		t.Skip("SO sem diag-memoria")
	}
	set, err := recipes.LoadFS(toolkit.Recipes, "recipes")
	if err != nil {
		t.Fatal(err)
	}
	r, ok := set.Get(os, "diag-memoria")
	if !ok {
		t.Fatal("diag-memoria ausente")
	}
	for _, st := range r.Steps {
		if st.Render == "" {
			continue
		}
		cmd, err := recipes.BuildCmd(st.Run, os, st.Timeout, false, true)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		// só o stdout, como a engine (o stderr só entra no relatório quando o comando falha): o PowerShell do runner
		// escreve registros CLIXML de progresso no stderr
		var stdout, stderr bytes.Buffer
		c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
		c.Stdout, c.Stderr = &stdout, &stderr
		err = c.Run()
		cancel()
		out := stdout.Bytes()
		if err != nil {
			t.Logf("%s: o comando saiu com erro (%v); a saída é avaliada mesmo assim\nstderr: %.500s", st.ID, err, stderr.String())
		}
		summary, rerr := diagfmt.Render(st.Render, string(out), diagfmt.PT)
		if rerr != nil || strings.TrimSpace(summary) == "" {
			t.Errorf("%s: o renderizador %s não entendeu a saída real: %v\n--- saída crua (até 2000 bytes) ---\n%.2000s", st.ID, st.Render, rerr, string(out))
			continue
		}
		t.Logf("%s:\n%s", st.ID, summary)
	}
}
