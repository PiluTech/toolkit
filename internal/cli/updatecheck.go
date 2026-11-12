package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/buildinfo"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/textsafe"
	"github.com/PiluTech/toolkit/internal/update"
)

// UpdateCheckDeps são os pontos injetáveis do aviso de versão nova (zero = ambiente real). Os testes trocam o
// relógio, a pasta de cache, o ambiente, os terminais e a consulta: nenhum teste chega à rede.
type UpdateCheckDeps struct {
	Env       func(string) string // nil = os.Getenv
	StdoutTTY func() bool         // nil = stdout é terminal
	StderrTTY func() bool         // nil = stderr é terminal
	CacheDir  func() (string, error)
	Now       func() time.Time
	// Fetch consulta a última release (nil = update.Latest no host padrão, com GITHUB_TOKEN quando houver).
	Fetch   func(ctx context.Context, cfg update.Config) (update.Release, error)
	Timeout time.Duration
	// Join faz o fim do comando esperar a goroutine da consulta terminar (já cancelada). Só para testes: no uso
	// real o comando NUNCA espera pela consulta.
	Join bool
}

func (d UpdateCheckDeps) env(k string) string {
	if d.Env != nil {
		return d.Env(k)
	}
	return os.Getenv(k)
}

func (d UpdateCheckDeps) stdoutTTY() bool {
	if d.StdoutTTY != nil {
		return d.StdoutTTY()
	}
	return fileIsTerminal(os.Stdout)
}

func (d UpdateCheckDeps) stderrTTY() bool {
	if d.StderrTTY != nil {
		return d.StderrTTY()
	}
	return fileIsTerminal(os.Stderr)
}

// envOn: variável ligada = definida e diferente de vazio, "0" e "false".
func envOn(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// skipsUpdateCheck: comandos que não avisam (o update já trata disso; ajuda e completar não são "fim de comando").
func skipsUpdateCheck(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "update", "help", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	}
	if cmd.Parent() != nil && cmd.Parent().Name() == "completion" {
		return true
	}
	if f := cmd.Flags().Lookup("json"); f != nil && f.Value.String() == "true" {
		return true // --json: nem uma linha a mais, nem em stderr
	}
	return false
}

// updateNotifier cuida do aviso de versão nova de UMA execução: lê o cache (rápido), dispara a consulta (se
// vencida) em goroutine e, no fim do comando, escreve uma linha em STDERR. Nunca espera pela consulta: o
// fim do comando a cancela (o resultado, se chegou, entra; senão fica para a próxima execução).
type updateNotifier struct {
	deps    Deps
	started bool
	checker *update.Checker
	current string

	cached  *update.Release
	result  chan update.Release // resultado da consulta que chegou a tempo (cap 1)
	done    chan struct{}       // fechado quando a goroutine da consulta termina
	cancel  context.CancelFunc
	silence bool // a TUI já mostrou a faixa: sem linha no fim
}

func newUpdateNotifier(deps Deps) *updateNotifier { return &updateNotifier{deps: deps} }

// start decide se o aviso vale para este comando e, se sim, lê o cache e dispara a consulta.
func (n *updateNotifier) start(cmd *cobra.Command, off bool) {
	if n == nil || off || skipsUpdateCheck(cmd) {
		return
	}
	d := n.deps.UpdateCheck
	if envOn(d.env("TOOLKIT_NO_UPDATE_CHECK")) || envOn(d.env("CI")) || !d.stdoutTTY() || !d.stderrTTY() {
		return
	}
	cur := n.deps.Update.Current
	if cur == "" {
		cur = buildinfo.Version
	}
	if _, err := update.ParseVersion(cur); err != nil {
		return // build de desenvolvimento: não há versão para comparar
	}
	path, err := update.CachePath(d.CacheDir)
	if err != nil {
		return
	}
	cfg := passiveConfig(n.deps, cur)
	fetch := d.Fetch
	if fetch == nil {
		fetch = update.Latest
	}
	n.checker = &update.Checker{Path: path, Now: d.Now, Timeout: d.Timeout,
		Fetch: func(ctx context.Context) (update.Release, error) { return fetch(ctx, cfg) }}
	n.current, n.started = cur, true
	if rel, ok := n.checker.Cached(cur); ok {
		n.cached = &rel
	}
	if !n.checker.Due() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel, n.result, n.done = cancel, make(chan update.Release, 1), make(chan struct{})
	go func() {
		defer close(n.done)
		if _, err := n.checker.Refresh(ctx); err != nil {
			return // a falha já ficou no cache (backoff); o comando nunca vê erro
		}
		if rel, ok := n.checker.Cached(n.current); ok {
			n.result <- rel
		}
	}()
}

// latest devolve a versão nova conhecida agora (cache, ou resultado que já chegou), sem esperar.
func (n *updateNotifier) latest() (update.Release, bool) {
	if n == nil || !n.started {
		return update.Release{}, false
	}
	if n.cached != nil {
		return *n.cached, true
	}
	select {
	case rel := <-n.result:
		n.cached = &rel
		return rel, true
	default:
		return update.Release{}, false
	}
}

// finish cancela a consulta em andamento e, se há versão nova, escreve UMA linha em stderr.
func (n *updateNotifier) finish(errOut io.Writer) {
	if n == nil || !n.started {
		return
	}
	rel, ok := n.latest()
	if n.cancel != nil {
		n.cancel()
		if n.deps.UpdateCheck.Join {
			<-n.done
		}
	}
	if !ok || n.silence {
		return
	}
	_, _ = fmt.Fprintf(errOut, msg.UpdateNotice+"\n", textsafe.Line(rel.Tag), textsafe.Line(n.current))
}

// passiveConfig é a configuração da checagem PASSIVA de versão: NUNCA leva GITHUB_TOKEN/GH_TOKEN (60 consultas por
// hora sem token bastam para 1 por dia, e um token do ambiente não deve sair numa consulta que o usuário não pediu).
// O `toolkit update` (pedido explícito) continua usando o token.
func passiveConfig(deps Deps, current string) update.Config {
	return update.Config{APIBase: deps.Update.APIBase, Current: current, Downloader: deps.downloader()}
}
