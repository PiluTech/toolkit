package detect

import (
	"os"
	"testing"
)

// embeddedCatalog lê o catálogo embutido do repositório (somente leitura).
func embeddedCatalog(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../catalog/catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}
