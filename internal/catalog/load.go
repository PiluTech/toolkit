package catalog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LoadFile lê e valida um catálogo local (aceito sem hash, conforme a decisão 16). Abre o arquivo,
// exige arquivo regular (nada de FIFO, dispositivo ou diretório) e lê no máximo MaxFileSize+1 bytes,
// sem Stat prévio por caminho (que abriria uma janela entre a checagem e a leitura).
func LoadFile(path string) (*Catalog, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("catálogo: %w", err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("catálogo: %w", err)
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("catálogo %s: não é um arquivo regular", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("catálogo: %w", err)
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("catálogo %s: arquivo maior que %d MiB", path, MaxFileSize>>20)
	}
	return Parse(filepath.Base(path), data)
}
