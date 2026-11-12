package resumetest

import (
	"io/fs"
	"sync"

	"github.com/PiluVitu/toolkit/internal/resume"
)

// MemFS é um resume.FS em memória: os testes dos registradores nunca tocam o disco real.
type MemFS struct {
	mu    sync.Mutex
	Files map[string][]byte
	Perms map[string]fs.FileMode
	Dirs  map[string]bool
	// Src são os arquivos "do disco" que CopyFile pode copiar.
	Src map[string][]byte
}

var _ resume.FS = (*MemFS)(nil)

// NewMemFS cria um FS vazio.
func NewMemFS() *MemFS {
	return &MemFS{Files: map[string][]byte{}, Perms: map[string]fs.FileMode{}, Dirs: map[string]bool{}, Src: map[string][]byte{}}
}

// MkdirAll registra o diretório.
func (m *MemFS) MkdirAll(p string, _ fs.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Dirs[p] = true
	return nil
}

// WriteFile grava em memória.
func (m *MemFS) WriteFile(p string, data []byte, perm fs.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Files[p], m.Perms[p] = append([]byte(nil), data...), perm
	return nil
}

// CopyFile copia de Src.
func (m *MemFS) CopyFile(src, dst string, perm fs.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.Src[src]
	if !ok {
		return fs.ErrNotExist
	}
	m.Files[dst], m.Perms[dst] = append([]byte(nil), data...), perm
	return nil
}

// Remove apaga.
func (m *MemFS) Remove(p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Files, p)
	delete(m.Perms, p)
	return nil
}

// Exists diz se o arquivo existe.
func (m *MemFS) Exists(p string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.Files[p]
	return ok
}
