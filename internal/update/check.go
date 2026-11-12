package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Prazos do aviso de versão nova. A API do GitHub sem token limita 60 consultas por hora por IP (e já deu 403),
// então a FALHA também é lembrada: o backoff é de horas e o toolkit não insiste.
const (
	CheckInterval    = 24 * time.Hour // no máximo 1 consulta CONCLUÍDA por dia
	attemptBackoff   = 15 * time.Minute
	rateLimitBackoff = 6 * time.Hour
	failBackoff      = 3 * time.Hour
	defaultTimeout   = 4 * time.Second
	maxCacheBytes    = 64 << 10
	cacheFile        = "update-check.json"
)

// CacheEntry é o conteúdo do cache: só a versão, a data da checagem e as notas já limpas (nada de segredo).
type CacheEntry struct {
	Tag        string    `json:"tag,omitempty"`
	Notes      string    `json:"notes,omitempty"`
	URL        string    `json:"url,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`  // última consulta concluída
	RetryAfter time.Time `json:"retry_after"` // nenhuma consulta antes disto (tentativa ou falha)
}

// Checker consulta a última release no máximo 1 vez por dia e guarda o resultado em um arquivo (0600).
// Relógio, consulta e caminho são injetáveis: nenhum teste toca a rede.
type Checker struct {
	Path    string                                 // arquivo do cache (absoluto)
	Now     func() time.Time                       // nil = time.Now
	Fetch   func(context.Context) (Release, error) // a consulta (update.Latest com a Config real)
	Timeout time.Duration                          // prazo da consulta (0 = 4s)
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// CachePath é <UserCacheDir>/toolkit/update-check.json; a pasta tem de ser absoluta.
func CachePath(userCacheDir func() (string, error)) (string, error) {
	if userCacheDir == nil {
		userCacheDir = os.UserCacheDir
	}
	d, err := userCacheDir()
	if err != nil {
		return "", fmt.Errorf("pasta de cache: %w", err)
	}
	if !filepath.IsAbs(d) {
		return "", errors.New("pasta de cache não é absoluta")
	}
	return filepath.Join(d, "toolkit", cacheFile), nil
}

// load lê o cache; ausente, grande demais ou corrompido vale como vazio (nunca quebra o comando).
func (c *Checker) load() CacheEntry {
	f, err := os.Open(c.Path)
	if err != nil {
		return CacheEntry{}
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxCacheBytes+1))
	if err != nil || len(b) > maxCacheBytes {
		return CacheEntry{}
	}
	var e CacheEntry
	if json.Unmarshal(b, &e) != nil {
		return CacheEntry{}
	}
	return e
}

func (c *Checker) save(e CacheEntry) error {
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Pasta que já existia mais aberta (0755 de uma versão antiga ou criada à mão): aperta para 0700 (melhor esforço).
	if fi, err := os.Stat(dir); err == nil && fi.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(dir, 0o700)
	}
	c.removeStaleTemps(dir)
	tmp, err := os.CreateTemp(dir, cacheFile+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	b, _ := json.Marshal(e)
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(name)
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(name, c.Path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// staleTempAge: um update-check.json.* mais velho que isto é sobra de um processo que morreu entre o
// CreateTemp e o Rename (outro processo vivo termina a escrita em milissegundos).
const staleTempAge = 10 * time.Minute

// removeStaleTemps apaga as sobras update-check.json.* (só arquivos regulares, só os velhos, só neste padrão).
func (c *Checker) removeStaleTemps(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, cacheFile+".") || name == cacheFile || !e.Type().IsRegular() {
			continue
		}
		if fi, err := e.Info(); err == nil && c.now().Sub(fi.ModTime()) > staleTempAge {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// Due diz se uma consulta nova é permitida agora: passou 1 dia da última concluída E acabou o backoff.
func (c *Checker) Due() bool {
	e := c.load()
	now := c.now()
	if now.Before(e.RetryAfter) {
		return false
	}
	return e.CheckedAt.IsZero() || e.CheckedAt.After(now) || now.Sub(e.CheckedAt) >= CheckInterval
}

// Cached devolve a release do cache quando é MAIS NOVA que a versão em execução (build dev ou versão ilegível
// nunca avisam). Notas e URL são limpas de novo: o arquivo é entrada não confiável.
func (c *Checker) Cached(current string) (Release, bool) {
	cur, err := ParseVersion(current)
	if err != nil {
		return Release{}, false
	}
	e := c.load()
	v, err := ParseVersion(e.Tag)
	if err != nil || Compare(v, cur) <= 0 {
		return Release{}, false
	}
	return Release{Tag: v.String(), Notes: cleanNotes(e.Notes), URL: textsafe.Line(e.URL)}, true
}

// Refresh faz a consulta (quem chama já checou Due) e grava o resultado. Antes de ir à rede grava uma marca de
// tentativa (15 min): sem poder gravar o cache NÃO consulta, senão seria 1 consulta por execução. Falha
// (inclusive 403/429) guarda o backoff de horas e mantém a versão antiga do cache.
func (c *Checker) Refresh(ctx context.Context) (Release, error) {
	if c.Fetch == nil {
		return Release{}, errors.New("sem consulta configurada")
	}
	e := c.load()
	now := c.now()
	e.RetryAfter = now.Add(attemptBackoff)
	if err := c.save(e); err != nil {
		return Release{}, fmt.Errorf("gravar o cache: %w", err)
	}
	to := c.Timeout
	if to <= 0 {
		to = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	rel, err := c.Fetch(ctx)
	if err != nil {
		switch {
		case errors.Is(err, ErrRateLimited):
			e.RetryAfter = now.Add(rateLimitBackoff)
		case errors.Is(err, context.Canceled):
			// o comando terminou antes da resposta: a marca de 15 min já vale, sem castigar o resto do dia
		default:
			e.RetryAfter = now.Add(failBackoff)
		}
		_ = c.save(e)
		return Release{}, err
	}
	e = CacheEntry{Tag: rel.Tag, Notes: rel.Notes, URL: rel.URL, CheckedAt: now}
	if err := c.save(e); err != nil {
		return rel, fmt.Errorf("gravar o cache: %w", err)
	}
	return rel, nil
}
