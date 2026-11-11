package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PiluVitu/toolkit/internal/buildinfo"
)

func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := NewRootCmd(&out, &errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestVersion(t *testing.T) {
	out, _, err := run(t, "version")
	if err != nil {
		t.Fatalf("version falhou: %v", err)
	}
	if !strings.Contains(out, buildinfo.Version) || !strings.Contains(out, buildinfo.Name) {
		t.Fatalf("saída inesperada: %q", out)
	}
}

func TestVersionRejeitaArgumentos(t *testing.T) {
	if _, _, err := run(t, "version", "extra"); err == nil {
		t.Fatal("esperava erro com argumento extra")
	}
}

func TestComandoDesconhecido(t *testing.T) {
	if _, _, err := run(t, "nao-existe"); err == nil {
		t.Fatal("esperava erro para comando desconhecido")
	}
}

func TestAjudaMostraComandos(t *testing.T) {
	out, _, err := run(t, "--help")
	if err != nil {
		t.Fatalf("--help falhou: %v", err)
	}
	if !strings.Contains(out, "version") {
		t.Fatalf("ajuda sem o comando version: %q", out)
	}
}
