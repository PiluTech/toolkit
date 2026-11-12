package state

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	return &FileStore{Dir: filepath.Join(t.TempDir(), "toolkit"), ToolkitVersion: "9.9.9", Sleep: func(time.Duration) {}}
}

func sample() *File {
	f := NewFile("x")
	rs := NewRecipeState("wsl-dev", "abc", "run1", "ana", []string{"a", "b"}, Options{Yes: true}, t0)
	f.Recipes["wsl-dev"] = rs
	f.Active = "wsl-dev"
	return f
}

func TestLoadMissingIsEmpty(t *testing.T) {
	s := newStore(t)
	f, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if f.SchemaVersion != SchemaVersion || len(f.Recipes) != 0 || f.ToolkitVersion != "9.9.9" {
		t.Fatalf("%+v", f)
	}
	if _, err := os.Stat(s.Dir); err == nil {
		t.Fatal("Load não deve criar o diretório")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	f := sample()
	st := f.Recipes["wsl-dev"].Step("a")
	st.Status = StepAwaitingReboot
	st.BootID = "boot-1"
	now := t0
	st.StartedAt = &now
	if err := s.Save(ctx, f); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ToolkitVersion != "9.9.9" || got.Active != "wsl-dev" {
		t.Fatalf("%+v", got)
	}
	r := got.Recipes["wsl-dev"]
	if r.RunID != "run1" || r.User != "ana" || !r.Options.Yes || len(r.Steps) != 2 {
		t.Fatalf("%+v", r)
	}
	if a := r.Step("a"); a.Status != StepAwaitingReboot || a.BootID != "boot-1" || a.StartedAt == nil || !a.StartedAt.Equal(t0) {
		t.Fatalf("%+v", a)
	}
	if r.Step("nao") != nil {
		t.Fatal("etapa inexistente deve ser nil")
	}
	// Sem sobras: nem tmp nem lock.
	ents, _ := os.ReadDir(s.Dir)
	for _, e := range ents {
		if e.Name() != FileName {
			t.Fatalf("sobra no diretório: %s", e.Name())
		}
	}
	if fi, _ := os.Stat(s.Path()); fi.Mode().Perm() != 0o600 {
		t.Fatalf("permissão %v, quer 0600", fi.Mode().Perm())
	}
}

func TestSaveOverwritesAtomically(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	f := sample()
	if err := s.Save(ctx, f); err != nil {
		t.Fatal(err)
	}
	f.Recipes["wsl-dev"].Status = RecipeDone
	f.Active = ""
	if err := s.Save(ctx, f); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Load(ctx)
	if got.Recipes["wsl-dev"].Status != RecipeDone || got.Active != "" {
		t.Fatalf("%+v", got)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]struct {
		body string
		want error
	}{
		"json quebrado":      {`{"schema_version":`, ErrCorrupt},
		"sem schema":         {`{"recipes":{}}`, ErrCorrupt},
		"schema novo":        {`{"schema_version":2,"coisa_nova":true}`, ErrNewerSchema},
		"campo desconhecido": {`{"schema_version":1,"x":1}`, ErrCorrupt},
		"lixo no fim":        {`{"schema_version":1} {}`, ErrCorrupt},
		"active inexistente": {`{"schema_version":1,"active":"a","recipes":{}}`, ErrCorrupt},
		"chave diferente":    {`{"schema_version":1,"recipes":{"a":{"recipe_id":"b","status":"running"}}}`, ErrCorrupt},
		"status ruim":        {`{"schema_version":1,"recipes":{"a":{"recipe_id":"a","status":"x"}}}`, ErrCorrupt},
		"etapa ruim":         {`{"schema_version":1,"recipes":{"a":{"recipe_id":"a","status":"running","steps":[{"id":"s","status":"?"}]}}}`, ErrCorrupt},
		"etapa duplicada":    {`{"schema_version":1,"recipes":{"a":{"recipe_id":"a","status":"running","steps":[{"id":"s","status":"done"},{"id":"s","status":"done"}]}}}`, ErrCorrupt},
		"receita nula":       {`{"schema_version":1,"recipes":{"a":null}}`, ErrCorrupt},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			if err := os.MkdirAll(s.Dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.Path(), []byte(c.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := s.Load(context.Background())
			if !errors.Is(err, c.want) {
				t.Fatalf("err=%v, quer %v", err, c.want)
			}
		})
	}
}

func TestLoadTooBig(t *testing.T) {
	s := newStore(t)
	_ = os.MkdirAll(s.Dir, 0o700)
	if err := os.WriteFile(s.Path(), []byte(strings.Repeat(" ", MaxFileSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err=%v", err)
	}
}

func TestSymlinkRefused(t *testing.T) {
	s := newStore(t)
	_ = os.MkdirAll(s.Dir, 0o700)
	target := filepath.Join(t.TempDir(), "alvo.json")
	_ = os.WriteFile(target, []byte(`{"schema_version":1}`), 0o600)
	if err := os.Symlink(target, s.Path()); err != nil {
		t.Skip("sem symlink:", err)
	}
	if _, err := s.Load(context.Background()); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("load: %v", err)
	}
	if err := s.Save(context.Background(), sample()); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("save: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != `{"schema_version":1}` {
		t.Fatal("o alvo do symlink foi alterado")
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	s := newStore(t)
	f := sample()
	f.Active = "outra"
	if err := s.Save(context.Background(), f); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(s.Path()); err == nil {
		t.Fatal("não deve gravar state inválido")
	}
}

func TestUpdate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.Update(ctx, func(f *File) error {
		f.Recipes["wsl-dev"] = NewRecipeState("wsl-dev", "h", "r", "", []string{"a"}, Options{}, t0)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if err := s.Update(ctx, func(f *File) error {
		f.Recipes["wsl-dev"].Status = RecipeDone
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	got, _ := s.Load(ctx)
	if got.Recipes["wsl-dev"].Status != RecipeRunning {
		t.Fatal("erro em fn não pode gravar")
	}
}

func TestUpdateConcurrent(t *testing.T) {
	s := newStore(t)
	s.Sleep = nil // espera real, curta
	s.LockTimeout = 30 * time.Second
	ctx := context.Background()
	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Update(ctx, func(f *File) error {
				if f.Recipes["c"] == nil {
					f.Recipes["c"] = NewRecipeState("c", "h", "r", "", []string{"a"}, Options{}, t0)
				}
				f.Recipes["c"].Step("a").Attempts++
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a := got.Recipes["c"].Step("a").Attempts; a != n {
		t.Fatalf("attempts=%d, quer %d (corrida no read-modify-write)", a, n)
	}
}

func TestLockHeldTimesOut(t *testing.T) {
	s := newStore(t)
	s.LockTimeout = time.Second
	clock := time.Now()
	s.Now = func() time.Time { return clock }
	s.Sleep = func(d time.Duration) { clock = clock.Add(d) }
	_ = os.MkdirAll(s.Dir, 0o700)
	if err := os.WriteFile(s.lockPath(), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := s.Save(context.Background(), sample())
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(s.lockPath()); statErr != nil {
		t.Fatal("o lock de outro processo não pode ser removido")
	}
}

func TestStaleLockRemoved(t *testing.T) {
	s := newStore(t)
	_ = os.MkdirAll(s.Dir, 0o700)
	if err := os.WriteFile(s.lockPath(), []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(s.lockPath(), old, old); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(context.Background(), sample()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.lockPath()); err == nil {
		t.Fatal("o lock deve ser liberado ao fim")
	}
}

// Lock velho mas de um processo AINDA VIVO não é "velho": quem está rodando é o dono (sem checar a
// identidade, um segundo processo apagaria o lock de uma execução longa e as duas gravariam o state).
func TestStaleLockOfLiveProcessIsKept(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sem checagem de pid no Windows: vale só a idade")
	}
	s := newStore(t)
	s.LockTimeout = 100 * time.Millisecond
	_ = os.MkdirAll(s.Dir, 0o700)
	if err := os.WriteFile(s.lockPath(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(s.lockPath(), old, old); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(context.Background(), sample()); !errors.Is(err, ErrLocked) {
		t.Fatalf("lock de processo vivo foi removido: %v", err)
	}
	if _, err := os.Stat(s.lockPath()); err != nil {
		t.Fatal("o lock do processo vivo tem de continuar")
	}
}

// Conteúdo ilegível não identifica dono: vale só a idade.
func TestStaleLockWithGarbageIsRemoved(t *testing.T) {
	s := newStore(t)
	_ = os.MkdirAll(s.Dir, 0o700)
	if err := os.WriteFile(s.lockPath(), []byte("lixo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(s.lockPath(), old, old)
	if err := s.Save(context.Background(), sample()); err != nil {
		t.Fatal(err)
	}
}

func TestContextCancelled(t *testing.T) {
	s := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("load: %v", err)
	}
	if err := s.Save(ctx, sample()); !errors.Is(err, context.Canceled) {
		t.Fatalf("save: %v", err)
	}
}

func TestJSONShape(t *testing.T) {
	// O formato em disco é contrato da retomada entre versões: chaves em snake_case.
	b, _ := json.Marshal(sample())
	for _, k := range []string{`"schema_version"`, `"toolkit_version"`, `"recipe_hash"`, `"run_id"`, `"resume_attempts"`, `"started_at"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("falta a chave %s em %s", k, b)
		}
	}
}
