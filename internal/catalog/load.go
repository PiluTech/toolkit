package catalog

import (
	"fmt"
	"os"
	"path/filepath"
)

// maxFileSize evita ler um arquivo gigante por engano.
const maxFileSize = 4 << 20

// LoadFile lê e valida um catálogo local (aceito sem hash, conforme a decisão 16).
func LoadFile(path string) (*Catalog, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("catálogo: %w", err)
	}
	if st.Size() > maxFileSize {
		return nil, fmt.Errorf("catálogo %s: arquivo maior que %d MiB", path, maxFileSize>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("catálogo: %w", err)
	}
	return Parse(filepath.Base(path), data)
}
