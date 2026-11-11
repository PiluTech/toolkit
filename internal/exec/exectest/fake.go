// Package exectest traz o FakeRunner: respostas roteirizadas por linha de comando.
// Um comando sem roteiro falha o teste, então nada chega a um processo real.
package exectest

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"testing"

	toolexec "github.com/PiluVitu/toolkit/internal/exec"
)

// Reply é a resposta roteirizada para uma linha de comando.
type Reply struct {
	Result toolexec.Result
	Err    error
}

// FakeRunner implementa exec.Runner sem executar nada.
type FakeRunner struct {
	t       testing.TB
	mu      sync.Mutex
	replies map[string][]Reply
	paths   map[string]string
	calls   []toolexec.Cmd
}

// New cria um FakeRunner ligado ao teste t.
func New(t testing.TB) *FakeRunner {
	t.Helper()
	return &FakeRunner{t: t, replies: map[string][]Reply{}, paths: map[string]string{}}
}

// elevatedPrefix distingue, na chave do roteiro, o mesmo comando com e sem Elevate:
// um teste que esquece o Elevate falha em vez de passar em silêncio. ReadOnly não entra na chave
// (é semântica do dry-run): assere-se por Calls().
const elevatedPrefix = "[elevate] "

func key(c toolexec.Cmd) string {
	if c.Elevate {
		return elevatedPrefix + c.String()
	}
	return c.String()
}

// On roteiriza respostas para um comando SEM Elevate, pela linha (Cmd.String; com Secret os Args
// somem da linha). Cada chamada consome a próxima resposta; a última se repete.
func (f *FakeRunner) On(line string, replies ...Reply) *FakeRunner {
	return f.script(line, replies)
}

// OnElevated é como On, para comandos com Elevate=true.
func (f *FakeRunner) OnElevated(line string, replies ...Reply) *FakeRunner {
	return f.script(elevatedPrefix+line, replies)
}

func (f *FakeRunner) script(k string, replies []Reply) *FakeRunner {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[k] = append(f.replies[k], replies...)
	return f
}

// Exit atalho para uma resposta com exit code e stdout.
func Exit(code int, stdout string) Reply {
	return Reply{Result: toolexec.Result{ExitCode: code, Stdout: []byte(stdout)}}
}

// WithPath declara que name existe no PATH em path.
func (f *FakeRunner) WithPath(name, path string) *FakeRunner {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths[name] = path
	return f
}

// Run devolve a resposta roteirizada ou falha o teste se não houver.
func (f *FakeRunner) Run(_ context.Context, c toolexec.Cmd) (toolexec.Result, error) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	line := key(c)
	rs := f.replies[line]
	if len(rs) == 0 {
		f.t.Errorf("exectest: comando inesperado: %s", line)
		return toolexec.Result{}, fmt.Errorf("comando inesperado: %s", line)
	}
	r := rs[0]
	if len(rs) > 1 {
		f.replies[line] = rs[1:]
	}
	if c.Stdout != nil {
		_, _ = c.Stdout.Write(r.Result.Stdout)
	}
	return r.Result, r.Err
}

// LookPath responde só aos nomes declarados com WithPath.
func (f *FakeRunner) LookPath(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.paths[name]; ok {
		return p, nil
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// Calls devolve os comandos recebidos, em ordem.
func (f *FakeRunner) Calls() []toolexec.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]toolexec.Cmd(nil), f.calls...)
}
