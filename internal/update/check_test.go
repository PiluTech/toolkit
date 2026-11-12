package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time { return f.t }

func newChecker(t *testing.T, fetch func(context.Context) (Release, error)) (*Checker, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)}
	return &Checker{Path: filepath.Join(t.TempDir(), "toolkit", "update-check.json"), Now: clk.now, Fetch: fetch}, clk
}

func okFetch(calls *int) func(context.Context) (Release, error) {
	return func(context.Context) (Release, error) {
		*calls++
		return Release{Tag: "v0.2.0", Notes: "novidades", URL: "https://github.com/PiluTech/toolkit/releases/tag/v0.2.0"}, nil
	}
}

func TestCheckerOneQueryPerDay(t *testing.T) {
	calls := 0
	c, clk := newChecker(t, okFetch(&calls))
	if !c.Due() {
		t.Fatal("sem cache deve consultar")
	}
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || c.Due() {
		t.Fatalf("depois de consultar não deve estar vencido (calls=%d)", calls)
	}
	clk.t = clk.t.Add(23*time.Hour + 59*time.Minute)
	if c.Due() {
		t.Fatal("antes de 24h não consulta")
	}
	clk.t = clk.t.Add(2 * time.Minute)
	if !c.Due() {
		t.Fatal("passadas 24h consulta de novo")
	}
}

func TestCheckerCacheFileIsPrivateAndClean(t *testing.T) {
	calls := 0
	c, _ := newChecker(t, func(context.Context) (Release, error) {
		calls++
		return Release{Tag: "v0.2.0", Notes: "ok\x1b[2J", URL: "https://x.example/r"}, nil
	})
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "\x1b") {
		t.Fatalf("o cache não pode guardar escape: %q", b)
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(c.Path)
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("modo %v, quero 0600", st.Mode().Perm())
		}
	}
}

func TestCheckerBackoffAfterRateLimit(t *testing.T) {
	c, clk := newChecker(t, func(context.Context) (Release, error) { return Release{}, ErrRateLimited })
	if _, err := c.Refresh(context.Background()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if c.Due() {
		t.Fatal("logo depois do 403 não insiste")
	}
	clk.t = clk.t.Add(5 * time.Hour)
	if c.Due() {
		t.Fatal("o backoff é de horas")
	}
	clk.t = clk.t.Add(2 * time.Hour)
	if !c.Due() {
		t.Fatal("passado o backoff pode tentar de novo")
	}
}

func TestCheckerFailureKeepsOldTagAndBacksOff(t *testing.T) {
	fail := false
	c, clk := newChecker(t, func(context.Context) (Release, error) {
		if fail {
			return Release{}, errors.New("rede fora")
		}
		return Release{Tag: "v0.2.0"}, nil
	})
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(25 * time.Hour)
	fail = true
	if _, err := c.Refresh(context.Background()); err == nil {
		t.Fatal("deveria falhar")
	}
	if c.Due() {
		t.Fatal("falha qualquer também tem backoff")
	}
	if rel, ok := c.Cached("v0.1.0"); !ok || rel.Tag != "v0.2.0" {
		t.Fatalf("a versão antiga continua no cache: %v %v", rel, ok)
	}
}

func TestCheckerCorruptCacheIsIgnored(t *testing.T) {
	calls := 0
	c, _ := newChecker(t, okFetch(&calls))
	_ = os.MkdirAll(filepath.Dir(c.Path), 0o700)
	for _, junk := range []string{"{", "\x00\x01", `{"tag":"../../etc","checked_at":"ontem"}`, strings.Repeat("a", 200<<10), `[]`} {
		_ = os.WriteFile(c.Path, []byte(junk), 0o600)
		if !c.Due() {
			t.Fatalf("cache corrompido %.20q deve valer como ausente", junk)
		}
		if _, ok := c.Cached("v0.1.0"); ok {
			t.Fatalf("cache corrompido %.20q não pode gerar aviso", junk)
		}
	}
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh sobre cache ruim: %v", err)
	}
	if _, ok := c.Cached("v0.1.0"); !ok {
		t.Fatal("o cache foi regravado")
	}
}

func TestCheckerCachedOnlyWhenNewer(t *testing.T) {
	calls := 0
	c, _ := newChecker(t, okFetch(&calls))
	_, _ = c.Refresh(context.Background())
	for cur, want := range map[string]bool{"v0.1.0": true, "v0.2.0": false, "v0.3.0": false, "dev": false, "": false, "lixo": false} {
		if _, ok := c.Cached(cur); ok != want {
			t.Errorf("Cached(%q) = %v, quero %v", cur, ok, want)
		}
	}
}

func TestCheckerSanitizesTamperedCache(t *testing.T) {
	calls := 0
	c, _ := newChecker(t, okFetch(&calls))
	_ = os.MkdirAll(filepath.Dir(c.Path), 0o700)
	_ = os.WriteFile(c.Path, []byte(`{"tag":"v0.2.0","notes":"a\u001b[31mb","url":"https://x.example/\u001b]0;x","checked_at":"2026-11-12T09:00:00Z"}`), 0o600)
	rel, ok := c.Cached("v0.1.0")
	if !ok || strings.Contains(rel.Notes, "\x1b") || strings.Contains(rel.URL, "\x1b") {
		t.Fatalf("notas e URL precisam passar por textsafe: %+v", rel)
	}
}

func TestCheckerUnwritableCacheNeverQueries(t *testing.T) {
	calls := 0
	c, _ := newChecker(t, okFetch(&calls))
	file := filepath.Join(t.TempDir(), "arquivo")
	_ = os.WriteFile(file, []byte("x"), 0o600)
	c.Path = filepath.Join(file, "sub", "update-check.json") // o pai é um arquivo: não dá para gravar
	if _, err := c.Refresh(context.Background()); err == nil {
		t.Fatal("sem poder gravar o cache não se consulta (senão seria 1 consulta por execução)")
	}
	if calls != 0 {
		t.Fatalf("consultou %d vezes", calls)
	}
}

func TestCheckerTimeoutEndsTheQuery(t *testing.T) {
	started := make(chan struct{})
	c, _ := newChecker(t, func(ctx context.Context) (Release, error) {
		close(started)
		<-ctx.Done()
		return Release{}, ctx.Err()
	})
	c.Timeout = 20 * time.Millisecond
	done := make(chan error, 1)
	go func() { _, err := c.Refresh(context.Background()); done <- err }()
	<-started
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("o timeout não encerrou a consulta")
	}
	if c.Due() {
		t.Fatal("logo depois de uma tentativa não repete")
	}
}

func TestCheckerCancelledByCallerAllowsRetryLater(t *testing.T) {
	c, clk := newChecker(t, func(ctx context.Context) (Release, error) { <-ctx.Done(); return Release{}, ctx.Err() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = c.Refresh(ctx)
	clk.t = clk.t.Add(time.Hour)
	if !c.Due() {
		t.Fatal("tentativa cancelada pelo fim do comando deve poder ser repetida em pouco tempo, não em 1 dia")
	}
}

func TestCachePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "c")
	p, err := CachePath(func() (string, error) { return root, nil })
	if err != nil || filepath.Base(p) != "update-check.json" || filepath.Base(filepath.Dir(p)) != "toolkit" {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := CachePath(func() (string, error) { return "relativo", nil }); err == nil {
		t.Fatal("pasta relativa deve ser recusada")
	}
	if _, err := CachePath(func() (string, error) { return "", errors.New("sem home") }); err == nil {
		t.Fatal("erro do UserCacheDir deve voltar")
	}
}
