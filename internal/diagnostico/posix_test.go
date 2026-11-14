package diagnostico

import (
	"os"
	"strings"
	"testing"
)

// Os caminhos que os coletores de Linux e de macOS usam (sysfs, /Library/Logs, ...) são POSIX em QUALQUER SO, e esses
// coletores rodam nos testes do Windows com um sistema de arquivos falso. O path/filepath segue as regras do SO que
// compila: no Windows o Dir desse pacote nunca devolve "/" e um laço de teste travou o CI por 10 minutos. Os coletores e
// os helpers de teste usam o pacote path.
func usesFilepath(src string) bool {
	return strings.Contains(src, `"path/filepath"`) || strings.Contains(src, "filepath.")
}

func TestColetoresPosixNaoUsamFilepath(t *testing.T) {
	for _, f := range []string{"coleta_lin.go", "coleta_mac.go", "coleta_lin_test.go", "coleta_mac_test.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if usesFilepath(string(b)) {
			t.Errorf("%s usa path/filepath: caminhos do Linux/macOS são POSIX, use o pacote path", f)
		}
	}
	// o detector em si: o código que travou o CI do Windows é pego
	if !usesFilepath("import \"path/filepath\"\nfor q := p; q != \"/\"; q = filepath.Dir(q) {}") {
		t.Error("o detector não pega o uso de filepath")
	}
}
