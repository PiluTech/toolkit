package cli

import (
	"os"
	"testing"
)

// A detecção do stdin é a mesma da saída (fileIsTerminal): nenhum dispositivo de caracteres que não seja
// terminal (/dev/null, /dev/zero) conta como alguém para responder.
func TestStdinIsTerminalRejectsCharacterDevicesThatAreNotTerminals(t *testing.T) {
	for _, path := range []string{os.DevNull, "/dev/zero"} {
		f, err := os.Open(path)
		if err != nil {
			continue // /dev/zero não existe no Windows
		}
		old := os.Stdin
		os.Stdin = f
		got := stdinIsTerminal()
		os.Stdin = old
		_ = f.Close()
		if got {
			t.Errorf("%s não é um terminal", path)
		}
	}
	regular, err := os.CreateTemp(t.TempDir(), "entrada")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = regular.Close() }()
	old := os.Stdin
	os.Stdin = regular
	defer func() { os.Stdin = old }()
	if stdinIsTerminal() {
		t.Error("arquivo comum (stdin redirecionado) não é terminal")
	}
}
