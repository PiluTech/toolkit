package privacidade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	Version int `json:"version"`
	// Owner é quem pode usar este backup: o SID do usuário (escopo usuário) ou "maquina". Vazio só em FileStore sem Owner.
	Owner   string           `json:"owner,omitempty"`
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

// FileStore guarda o backup num arquivo JSON (0600) por escrita atômica (temporário + rename). Com Owner, o arquivo
// só é aceito se o dono gravado dentro dele for o mesmo (ErrBackupOutroDono) e o Save grava o dono.
type FileStore struct {
	File  string
	Owner string
}

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
	if s.Owner != "" && b.Owner != s.Owner {
		return nil, fmt.Errorf("%w (%s)", ErrBackupOutroDono, s.File)
	}
	if b.Entries == nil {
		b.Entries = map[string]Entry{}
	}
	return &b, nil
}

// Save grava o backup.
func (s FileStore) Save(b *Backup) error {
	if s.Owner != "" {
		cp := *b
		cp.Owner = s.Owner
		b = &cp
	}
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

// machineOwner é o dono do arquivo do escopo máquina (HKLM, serviços e tarefas: valem para todos os usuários).
const machineOwner = "maquina"

// ScopedStore separa o estado em dois arquivos na mesma pasta: um POR USUÁRIO (SID; só os valores HKCU, que são de
// cada perfil) e um da MÁQUINA (HKLM, serviços e tarefas, compartilhados). Cada arquivo grava o dono e só abre com
// o mesmo dono. Load junta os dois; Save separa por escopo (escopo vazio apaga o arquivo); Remove apaga os dois.
type ScopedStore struct {
	user, machine FileStore
}

// NewScopedStore valida o SID (vira parte do nome do arquivo) e monta o Store.
func NewScopedStore(dir, sid string) (ScopedStore, error) {
	if !validSID(sid) {
		return ScopedStore{}, errors.New("SID do usuário inválido")
	}
	return ScopedStore{
		user:    FileStore{File: filepath.Join(dir, "privacidade-backup-"+sid+".json"), Owner: sid},
		machine: FileStore{File: filepath.Join(dir, "privacidade-backup-"+machineOwner+".json"), Owner: machineOwner},
	}, nil
}

// Path devolve o arquivo do usuário (o que a mensagem ao usuário mostra).
func (s ScopedStore) Path() string { return s.user.File }

func userScoped(e Entry) bool { return e.Kind == "reg" && strings.HasPrefix(e.Key, `HKCU\`) }

// Load junta os dois escopos; nenhum arquivo = nil, nil.
func (s ScopedStore) Load() (*Backup, error) {
	u, err := s.user.Load()
	if err != nil {
		return nil, err
	}
	m, err := s.machine.Load()
	if err != nil {
		return nil, err
	}
	if u == nil && m == nil {
		return nil, nil
	}
	out := &Backup{Version: 1, Entries: map[string]Entry{}}
	for _, part := range []*Backup{m, u} {
		if part == nil {
			continue
		}
		if out.Created.IsZero() || part.Created.Before(out.Created) {
			out.Created = part.Created
		}
		for id, e := range part.Entries {
			out.Entries[id] = e
		}
	}
	return out, nil
}

// Save separa por escopo. Falha em um escopo antes de gravar qualquer um não deixa estado pela metade: os dois
// arquivos são preparados (e o dono conferido) antes da primeira escrita.
func (s ScopedStore) Save(b *Backup) error {
	u := &Backup{Version: 1, Created: b.Created, Entries: map[string]Entry{}}
	m := &Backup{Version: 1, Created: b.Created, Entries: map[string]Entry{}}
	for id, e := range b.Entries {
		if userScoped(e) {
			u.Entries[id] = e
		} else {
			m.Entries[id] = e
		}
	}
	// máquina primeiro: é o que o Apply precisa ter gravado antes de mudar HKLM
	for _, x := range []struct {
		fs FileStore
		b  *Backup
	}{{s.machine, m}, {s.user, u}} {
		if _, err := x.fs.Load(); err != nil { // dono diferente = recusa sem sobrescrever
			return err
		}
		if len(x.b.Entries) == 0 {
			if err := x.fs.Remove(); err != nil {
				return err
			}
			continue
		}
		if err := x.fs.Save(x.b); err != nil {
			return err
		}
	}
	return nil
}

// Remove apaga os dois arquivos (se o dono de algum não confere, recusa sem apagar nada).
func (s ScopedStore) Remove() error {
	for _, fs := range []FileStore{s.user, s.machine} {
		if _, err := fs.Load(); err != nil {
			return err
		}
	}
	if err := s.user.Remove(); err != nil {
		return err
	}
	return s.machine.Remove()
}
