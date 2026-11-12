package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FileName é o nome do arquivo de estado dentro de Dir.
const FileName = "state.json"

// FileStore guarda o state.json em Dir (platform.Paths.StateDir). Escrita atômica (tmp no mesmo diretório,
// fsync, rename) sob um lock de arquivo (state.json.lock, criado com O_EXCL).
type FileStore struct {
	Dir            string
	ToolkitVersion string
	// LockTimeout é quanto esperar pelo lock (padrão 10s). Lock mais velho que StaleLock (padrão 10 min)
	// é de um processo que morreu e é removido.
	LockTimeout time.Duration
	StaleLock   time.Duration
	// Now e Sleep são injetáveis nos testes.
	Now   func() time.Time
	Sleep func(time.Duration)
}

var _ Store = (*FileStore)(nil)

// Path devolve o caminho do state.json.
func (s *FileStore) Path() string { return filepath.Join(s.Dir, FileName) }

func (s *FileStore) lockPath() string { return s.Path() + ".lock" }

func (s *FileStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *FileStore) sleep(d time.Duration) {
	if s.Sleep != nil {
		s.Sleep(d)
		return
	}
	time.Sleep(d)
}

// Load lê o state. Arquivo inexistente = File vazio.
func (s *FileStore) Load(ctx context.Context) (*File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.load()
}

func (s *FileStore) load() (*File, error) {
	p := s.Path()
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return NewFile(s.ToolkitVersion), nil
	}
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", ErrUnsafePath, p)
	}
	fh, err := os.Open(p) //nolint:gosec // caminho vem de platform.ResolvePaths, checado acima
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()
	data, err := io.ReadAll(io.LimitReader(fh, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("%w: maior que %d MiB", ErrCorrupt, MaxFileSize>>20)
	}
	// A versão é lida primeiro e sem rigor, para que um schema novo dê ErrNewerSchema, não "campo desconhecido".
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if head.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("%w (schema %d, este binário entende até %d)", ErrNewerSchema, head.SchemaVersion, SchemaVersion)
	}
	if head.SchemaVersion < 1 {
		return nil, fmt.Errorf("%w: schema_version ausente ou inválido", ErrCorrupt)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: conteúdo depois do JSON", ErrCorrupt)
	}
	if f.Recipes == nil {
		f.Recipes = map[string]*RecipeState{}
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Save grava f de forma atômica, sob lock.
func (s *FileStore) Save(ctx context.Context, f *File) error {
	unlock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return s.write(f)
}

// Update faz Load + fn + Save sob o mesmo lock. Se fn devolver erro, nada é gravado.
func (s *FileStore) Update(ctx context.Context, fn func(*File) error) error {
	unlock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		return err
	}
	return s.write(f)
}

func (s *FileStore) write(f *File) error {
	f.SchemaVersion = SchemaVersion
	f.ToolkitVersion = s.ToolkitVersion
	if f.Recipes == nil {
		f.Recipes = map[string]*RecipeState{}
	}
	if err := f.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if fi, err := os.Lstat(s.Path()); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrUnsafePath, s.Path())
	}
	tmp, err := os.CreateTemp(s.Dir, FileName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, s.Path()); err != nil {
		cleanup()
		return err
	}
	syncDir(s.Dir)
	return nil
}

// syncDir faz fsync do diretório para a renomeação sobreviver a uma queda de energia. Falha é ignorada:
// no Windows não é possível abrir diretório assim.
func syncDir(dir string) {
	d, err := os.Open(dir) //nolint:gosec // diretório do próprio state
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func (s *FileStore) lock(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, err
	}
	timeout := s.LockTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	stale := s.StaleLock
	if stale == 0 {
		stale = 10 * time.Minute
	}
	deadline := s.now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fh, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(fh, "%d\n", os.Getpid())
			_ = fh.Close()
			return func() { _ = os.Remove(s.lockPath()) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if fi, statErr := os.Lstat(s.lockPath()); statErr == nil && s.now().Sub(fi.ModTime()) > stale && s.removeStaleLock(fi) {
			continue
		}
		if !s.now().Before(deadline) {
			return nil, ErrLocked
		}
		s.sleep(50 * time.Millisecond)
	}
}

// removeStaleLock apaga o lock velho SÓ se ele ainda é o mesmo arquivo observado e o processo que o
// criou não existe mais. Lock de processo vivo nunca é "velho" (uma execução longa não pode perder o
// lock para um segundo processo); conteúdo ilegível não identifica dono e vale só a idade. A
// conferência de identidade (mesmo arquivo, mesma data) logo antes do Remove evita apagar o lock novo
// de outro processo que acabou de trocá-lo.
func (s *FileStore) removeStaleLock(seen os.FileInfo) bool {
	data, err := os.ReadFile(s.lockPath())
	if err == nil {
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(data))); perr == nil && pid > 0 && pidAlive(pid) {
			return false
		}
	}
	now, err := os.Lstat(s.lockPath())
	if err != nil || !os.SameFile(seen, now) || !now.ModTime().Equal(seen.ModTime()) {
		return false
	}
	return os.Remove(s.lockPath()) == nil
}
