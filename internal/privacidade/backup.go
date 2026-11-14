package privacidade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Entry é o estado ORIGINAL de um item, guardado antes da primeira vez em que o toolkit o muda.
type Entry struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // "reg", "service" ou "task"
	// registro
	Key     string  `json:"key,omitempty"`
	Name    string  `json:"name,omitempty"`
	Existed bool    `json:"existed,omitempty"`
	RegKind RegKind `json:"reg_kind,omitempty"`
	DWORD   uint32  `json:"dword,omitempty"`
	Str     string  `json:"str,omitempty"`
	// serviço
	Service   string `json:"service,omitempty"`
	StartType string `json:"start_type,omitempty"`
	Running   bool   `json:"running,omitempty"`
	// tarefa
	Task    string `json:"task,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

// Backup reúne os estados originais. O PRIMEIRO original de cada item vence: aplicar duas vezes nunca sobrescreve o que
// o Windows tinha antes do toolkit.
type Backup struct {
	Version int              `json:"version"`
	Created time.Time        `json:"created"`
	Entries map[string]Entry `json:"entries"`
}

// Store guarda o backup (nil, nil = ainda não existe).
type Store interface {
	Load() (*Backup, error)
	Save(*Backup) error
	Remove() error
	Path() string
}

// FileStore guarda o backup num arquivo JSON (0600) por escrita atômica (temporário + rename).
type FileStore struct{ File string }

// Path devolve o caminho do arquivo.
func (s FileStore) Path() string { return s.File }

// Load lê o backup; arquivo ausente = nil, nil. Arquivo ilegível ou de versão desconhecida é erro (nunca se "desfaz"
// de um backup que não se entende).
func (s FileStore) Load() (*Backup, error) {
	data, err := os.ReadFile(s.File)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var b Backup
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("o backup %s está ilegível: %w", s.File, err)
	}
	if b.Version != 1 {
		return nil, fmt.Errorf("o backup %s é de uma versão desconhecida (%d)", s.File, b.Version)
	}
	if b.Entries == nil {
		b.Entries = map[string]Entry{}
	}
	return &b, nil
}

// Save grava o backup.
func (s FileStore) Save(b *Backup) error {
	if err := os.MkdirAll(filepath.Dir(s.File), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.File), ".privacidade-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // some se o rename deu certo; limpa se algo falhou
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.File)
}

// Remove apaga o arquivo (ausente não é erro).
func (s FileStore) Remove() error {
	if err := os.Remove(s.File); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
