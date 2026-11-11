package toolkit_test

import (
	"testing"

	toolkit "github.com/PiluVitu/toolkit"
	"github.com/PiluVitu/toolkit/internal/catalog"
)

func TestCatalogoEmbutidoEValido(t *testing.T) {
	cat, err := catalog.Parse("catalog.yaml", toolkit.CatalogYAML)
	if err != nil {
		t.Fatalf("o catálogo embutido deve validar:\n%v", err)
	}
	if len(cat.Apps) == 0 {
		t.Fatal("catálogo embutido vazio")
	}
	if _, ok := cat.Get("python"); !ok {
		t.Fatal("python deveria estar no catálogo")
	}
}
