package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/tui"
	"github.com/PiluTech/toolkit/internal/update"
)

// checkEnv monta Deps com TUDO injetado: terminais, ambiente, cache em pasta temporária, relógio e consulta.
type checkEnv struct {
	deps  Deps
	calls atomic.Int32
	env   map[string]string
	clock time.Time
	dir   string
	ran   chan struct{} // fechado quando a consulta termina (inclusive cancelada)
	once  sync.Once
}

func newCheckEnv(t *testing.T, fetch func(ctx context.Context) (update.Release, error)) *checkEnv {
	t.Helper()
	e := &checkEnv{env: map[string]string{}, clock: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC), dir: t.TempDir(), ran: make(chan struct{})}
	if fetch == nil {
		fetch = func(context.Context) (update.Release, error) {
			return update.Release{Tag: "v0.2.0", Notes: "novidades da 0.2", URL: "https://github.com/PiluTech/toolkit/releases/tag/v0.2.0"}, nil
		}
	}
	e.deps = Deps{
		GOOS: "linux", EmbeddedYML: []byte(embedded),
		Update: UpdateDeps{Current: "v0.1.0"},
		UpdateCheck: UpdateCheckDeps{
			Env:       func(k string) string { return e.env[k] },
			StdoutTTY: func() bool { return true },
			StderrTTY: func() bool { return true },
			CacheDir:  func() (string, error) { return e.dir, nil },
			Now:       func() time.Time { return e.clock },
			Join:      true,
			Fetch: func(ctx context.Context, _ update.Config) (update.Release, error) {
				e.calls.Add(1)
				defer e.once.Do(func() { close(e.ran) })
				return fetch(ctx)
			},
		},
	}
	return e
}

func (e *checkEnv) cachePath(t *testing.T) string {
	t.Helper()
	p, err := update.CachePath(func() (string, error) { return e.dir, nil })
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// seed grava no cache uma versão nova já consultada (para o comando só ler).
func (e *checkEnv) seed(t *testing.T, tag string, checked time.Time) {
	t.Helper()
	p := e.cachePath(t)
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	data := `{"tag":"` + tag + `","notes":"notas do cache","url":"https://github.com/PiluTech/toolkit/releases/tag/` + tag + `","checked_at":"` + checked.Format(time.RFC3339) + `","retry_after":"0001-01-01T00:00:00Z"}`
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (e *checkEnv) run(t *testing.T, args ...string) (out, errOut string, code int) {
	t.Helper()
	var o, er bytes.Buffer
	code = ExecuteWith(context.Background(), &o, &er, e.deps, args)
	return o.String(), er.String(), code
}

func TestNoticeFromCacheGoesToStderrOnlyOnce(t *testing.T) {
	e := newCheckEnv(t, nil)
	e.seed(t, "v0.2.0", e.clock.Add(-time.Hour)) // consultado há 1h: não consulta de novo
	out, errOut, code := e.run(t, "list")
	if code != 0 {
		t.Fatalf("código %d: %s", code, errOut)
	}
	if strings.Count(errOut, "v0.2.0 disponível") != 1 || strings.Count(errOut, "\n") != 1 {
		t.Fatalf("stderr deve ter UMA linha de aviso: %q", errOut)
	}
	if strings.Contains(out, "disponível") {
		t.Fatalf("o aviso nunca vai ao stdout: %q", out)
	}
	if e.calls.Load() != 0 {
		t.Fatal("cache de 1h: sem consulta")
	}
}

func TestNoNoticeWhenUpToDate(t *testing.T) {
	e := newCheckEnv(t, nil)
	e.seed(t, "v0.1.0", e.clock.Add(-time.Hour))
	_, errOut, _ := e.run(t, "list")
	if errOut != "" {
		t.Fatalf("sem versão nova não há aviso: %q", errOut)
	}
}

func TestDisabledSwitches(t *testing.T) {
	cases := map[string]func(e *checkEnv) []string{
		"flag":     func(*checkEnv) []string { return []string{"list", "--no-update-check"} },
		"variável": func(e *checkEnv) []string { e.env["TOOLKIT_NO_UPDATE_CHECK"] = "1"; return []string{"list"} },
		"CI":       func(e *checkEnv) []string { e.env["CI"] = "true"; return []string{"list"} },
		"stdout": func(e *checkEnv) []string {
			e.deps.UpdateCheck.StdoutTTY = func() bool { return false }
			return []string{"list"}
		},
		"stderr": func(e *checkEnv) []string {
			e.deps.UpdateCheck.StderrTTY = func() bool { return false }
			return []string{"list"}
		},
		"build dev": func(e *checkEnv) []string { e.deps.Update.Current = "dev"; return []string{"list"} },
		"--json":    func(*checkEnv) []string { return []string{"list", "--json"} },
		"update":    func(*checkEnv) []string { return []string{"update", "--help"} },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := newCheckEnv(t, nil)
			args := setup(e)
			e.seed(t, "v0.2.0", e.clock.Add(-48*time.Hour)) // vencido E com versão nova: se ligado, avisaria e consultaria
			out, errOut, _ := e.run(t, args...)
			if strings.Contains(errOut, "disponível") || strings.Contains(out, "disponível") {
				t.Fatalf("desligado por %s mas avisou: out=%q err=%q", name, out, errOut)
			}
			if e.calls.Load() != 0 {
				t.Fatalf("desligado por %s mas consultou", name)
			}
		})
	}
}

func TestEnvZeroKeepsItOn(t *testing.T) {
	e := newCheckEnv(t, nil)
	e.env["TOOLKIT_NO_UPDATE_CHECK"] = "0"
	e.env["CI"] = "false"
	e.seed(t, "v0.2.0", e.clock.Add(-time.Hour))
	if _, errOut, _ := e.run(t, "list"); !strings.Contains(errOut, "v0.2.0") {
		t.Fatalf("=0 e CI=false não desligam: %q", errOut)
	}
}

func TestJSONOutputStaysClean(t *testing.T) {
	e := newCheckEnv(t, nil)
	e.seed(t, "v0.2.0", e.clock.Add(-time.Hour))
	out, errOut, code := e.run(t, "list", "--json")
	if code != 0 || errOut != "" || !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("--json limpo: code=%d out=%.40q err=%q", code, out, errOut)
	}
}

func TestFailingCommandStillShowsNoticeOnce(t *testing.T) {
	e := newCheckEnv(t, nil)
	e.seed(t, "v0.2.0", e.clock.Add(-time.Hour))
	out, errOut, code := e.run(t, "install", "app-que-nao-existe", "--dry-run")
	if code == 0 {
		t.Fatalf("deveria falhar: %q", out)
	}
	if strings.Count(errOut, "v0.2.0 disponível") != 1 || !strings.Contains(errOut, "erro:") {
		t.Fatalf("erro E um aviso: %q", errOut)
	}
}

func TestQueryHappensOnceADayInBackground(t *testing.T) {
	e := newCheckEnv(t, nil)
	// 1ª execução: sem cache -> dispara a consulta em segundo plano
	e.run(t, "version")
	select {
	case <-e.ran:
	case <-time.After(2 * time.Second):
		t.Fatal("a consulta não foi disparada")
	}
	// espera o cache ser gravado
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(e.cachePath(t)); err == nil {
			if c := (&update.Checker{Path: e.cachePath(t), Now: func() time.Time { return e.clock }}); !c.Due() {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	n := e.calls.Load()
	if n != 1 {
		t.Fatalf("calls = %d", n)
	}
	// 2ª execução no mesmo dia: lê o cache (tem versão nova) e NÃO consulta
	_, errOut, _ := e.run(t, "version")
	if !strings.Contains(errOut, "v0.2.0 disponível") {
		t.Fatalf("a 2ª execução avisa pelo cache: %q", errOut)
	}
	if e.calls.Load() != 1 {
		t.Fatalf("consultou de novo no mesmo dia: %d", e.calls.Load())
	}
	// no dia seguinte consulta de novo
	e.clock = e.clock.Add(25 * time.Hour)
	e.once = sync.Once{}
	e.ran = make(chan struct{})
	e.run(t, "version")
	select {
	case <-e.ran:
	case <-time.After(2 * time.Second):
		t.Fatal("no dia seguinte deveria consultar")
	}
}

func TestRateLimitIsCachedAndNotRetried(t *testing.T) {
	e := newCheckEnv(t, func(context.Context) (update.Release, error) { return update.Release{}, update.ErrRateLimited })
	e.run(t, "version")
	<-e.ran
	time.Sleep(50 * time.Millisecond) // deixa o backoff ser gravado
	e.clock = e.clock.Add(2 * time.Hour)
	e.run(t, "version")
	e.run(t, "version")
	time.Sleep(50 * time.Millisecond)
	if n := e.calls.Load(); n != 1 {
		t.Fatalf("depois do 403 não insiste dentro do backoff: %d consultas", n)
	}
}

func TestCorruptCacheDoesNotBreakTheCommand(t *testing.T) {
	e := newCheckEnv(t, nil)
	p := e.cachePath(t)
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte("{{{ nem json"), 0o600)
	out, _, code := e.run(t, "list")
	if code != 0 || !strings.Contains(out, "git") {
		t.Fatalf("o comando segue normal: code=%d out=%.60q", code, out)
	}
}

func TestSlowQueryNeverDelaysCommandAndIsCancelled(t *testing.T) {
	cancelled := make(chan struct{})
	e := newCheckEnv(t, func(ctx context.Context) (update.Release, error) {
		<-ctx.Done()
		close(cancelled)
		return update.Release{}, ctx.Err()
	})
	start := time.Now()
	e.run(t, "version")
	if d := time.Since(start); d > time.Second {
		t.Fatalf("o comando esperou pela consulta: %v", d)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("a consulta lenta não foi cancelada no fim do comando (goroutine vazou)")
	}
}

func TestTUIGetsBannerFromCache(t *testing.T) {
	e := newCheckEnv(t, nil)
	e.seed(t, "v0.2.0", e.clock.Add(-time.Hour))
	var got tui.Options
	e.deps.Interactive = func() bool { return true }
	e.deps.TUI = TUIDeps{StdoutIsTerminal: func() bool { return true }, Term: func() string { return "xterm" },
		Run: func(_ context.Context, o tui.Options, _ ...tui.AreaDef) error { got = o; return nil }}
	_, errOut, code := e.run(t)
	if code != 0 {
		t.Fatal(errOut)
	}
	if got.Latest != "v0.2.0" || got.LatestNotes != "notas do cache" || !strings.Contains(got.LatestURL, "v0.2.0") {
		t.Fatalf("a TUI deve receber versão, notas e URL do cache: %+v", got)
	}
	if errOut != "" {
		t.Fatalf("depois da TUI não há linha extra: %q", errOut)
	}
}

// Determinístico: a consulta fica BLOQUEADA até a TUI abrir (Run) e só então é liberada; o resultado TEM de chegar pelo
// canal Updates (Latest começa vazio).
func TestTUIReceivesLateResultThroughChannel(t *testing.T) {
	release := make(chan struct{})
	e := newCheckEnv(t, func(ctx context.Context) (update.Release, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return update.Release{}, ctx.Err()
		}
		return update.Release{Tag: "v0.2.0", Notes: "novidades da 0.2", URL: "https://github.com/PiluTech/toolkit/releases/tag/v0.2.0"}, nil
	})
	var got tui.UpdateInfo
	var latestAtStart string
	e.deps.Interactive = func() bool { return true }
	e.deps.TUI = TUIDeps{StdoutIsTerminal: func() bool { return true }, Term: func() string { return "xterm" },
		Run: func(_ context.Context, o tui.Options, _ ...tui.AreaDef) error {
			latestAtStart = o.Latest
			close(release) // só agora a consulta termina
			select {
			case got = <-o.Updates:
			case <-time.After(5 * time.Second):
				t.Error("o resultado da consulta não chegou à TUI pelo canal")
			}
			return nil
		}}
	e.run(t)
	if latestAtStart != "" {
		t.Errorf("sem cache a faixa começa vazia: %q", latestAtStart)
	}
	if got.Tag != "v0.2.0" || got.Notes != "novidades da 0.2" {
		t.Errorf("update tardio: %+v", got)
	}
}

func TestTUIUpdateRequestedRunsUpdate(t *testing.T) {
	e := newCheckEnv(t, nil)
	e.seed(t, "v0.2.0", e.clock.Add(-time.Hour))
	e.deps.Interactive = func() bool { return true }
	// release de teste sem rede: o runUpdate real falharia no host; o importante é que ele FOI chamado
	e.deps.Update.APIBase = "https://127.0.0.1.invalid/x"
	e.deps.Update.Exe = func() (string, error) { return filepath.Join(t.TempDir(), "toolkit"), nil }
	e.deps.TUI = TUIDeps{StdoutIsTerminal: func() bool { return true }, Term: func() string { return "xterm" },
		Run: func(context.Context, tui.Options, ...tui.AreaDef) error { return tui.ErrUpdateRequested }}
	_, errOut, code := e.run(t)
	if code != 1 || !strings.Contains(errOut, "erro:") {
		t.Fatalf("Atualizar deve rodar o toolkit update (que aqui falha sem rede): code=%d %q", code, errOut)
	}
	if errors.Is(errors.New(errOut), tui.ErrUpdateRequested) {
		t.Fatal("o sentinela não pode vazar")
	}
}
