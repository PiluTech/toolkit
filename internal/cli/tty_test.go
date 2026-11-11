package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsTerminalNaoConfundeDevNullComTerminal(t *testing.T) {
	null, err := os.Stat(os.DevNull)
	if err != nil {
		t.Skip("sem " + os.DevNull)
	}
	if isTerminal(null, null, true) {
		t.Error("/dev/null não é um terminal")
	}
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, _ := os.Stat(p)
	if isTerminal(reg, null, true) {
		t.Error("arquivo comum (stdin redirecionado) não é terminal")
	}
}
