package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PiluTech/toolkit/internal/download"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Endereços padrão (a release do repositório público).
const (
	DefaultAPIBase      = "https://api.github.com/repos/PiluTech/toolkit/releases"
	DefaultDownloadBase = "https://github.com/PiluTech/toolkit/releases/download"

	maxAPIBytes       = 1 << 20   // resposta da API
	maxChecksumsBytes = 64 << 10  // checksums.txt
	maxBinaryBytes    = 128 << 20 // binário
	maxNotesLines     = 20
)

// Erros.
var (
	// ErrDevBuild indica que o binário em execução é de desenvolvimento ("dev"): não há versão para comparar.
	ErrDevBuild = errors.New("este binário é de desenvolvimento (sem versão): use --version vX.Y.Z")
	// ErrRateLimited indica que a API do GitHub recusou por limite de consultas (60 por hora por IP sem token).
	ErrRateLimited = errors.New("limite de consultas da API do GitHub: tente mais tarde ou defina GITHUB_TOKEN")
	// ErrNotInChecksums indica que o checksums.txt da release não lista o binário deste SO. Nada é baixado.
	ErrNotInChecksums = errors.New("o checksums.txt da release não lista o binário deste sistema")
	// ErrNotWritable indica que o diretório do binário não aceita gravação (instalação de sistema); o toolkit não usa sudo.
	ErrNotWritable = errors.New("sem permissão para gravar no diretório do binário")
	// ErrManagedInstall indica que o binário é um link simbólico (gerenciador de pacotes, ex.: brew): atualize por ele.
	ErrManagedInstall = errors.New("o binário é um link simbólico (instalado por gerenciador de pacotes)")
	// ErrStagedMismatch indica que o arquivo preparado ao lado do binário não confere com o hash esperado.
	ErrStagedMismatch = errors.New("o arquivo preparado não confere com o SHA-256 esperado")
)

// Config reúne o que o update precisa; nada é global (os testes injetam tudo).
type Config struct {
	APIBase      string // padrão DefaultAPIBase
	DownloadBase string // padrão DefaultDownloadBase
	Current      string // versão em execução ("v0.1.0" ou "dev")
	GOOS, GOARCH string
	Exe          string // caminho do binário em execução (os.Executable)
	Downloader   *download.Downloader
	// Token devolve o token da API do GitHub (nil = sem token). Só vai ao api.github.com padrão.
	Token func() string
	// Rename é injetável para testar a troca (padrão os.Rename).
	Rename func(oldpath, newpath string) error
}

func (c Config) api() string {
	if c.APIBase != "" {
		return strings.TrimRight(c.APIBase, "/")
	}
	return DefaultAPIBase
}

func (c Config) dl() string {
	if c.DownloadBase != "" {
		return strings.TrimRight(c.DownloadBase, "/")
	}
	return DefaultDownloadBase
}

func (c Config) rename() func(string, string) error {
	if c.Rename != nil {
		return c.Rename
	}
	return os.Rename
}

func (c Config) downloader() *download.Downloader {
	if c.Downloader != nil {
		return c.Downloader
	}
	return &download.Downloader{}
}

// Release é o que a API do GitHub diz da release.
type Release struct {
	Tag   string // "v0.2.0"
	Notes string // corpo da release, já limpo (textsafe)
	URL   string // página da release
}

// Latest consulta a última release publicada (a API não devolve rascunho nem pré-release aqui).
func Latest(ctx context.Context, c Config) (Release, error) {
	return fetchRelease(ctx, c, c.api()+"/latest")
}

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// apiHeaders: o token (se houver e for válido) só vai ao api.github.com padrão, nunca a outro host.
func (c Config) apiHeaders(url string) http.Header {
	h := http.Header{"Accept": {"application/vnd.github+json"}}
	if c.Token != nil && strings.HasPrefix(url, DefaultAPIBase) {
		if t := c.Token(); t != "" && tokenRe.MatchString(t) {
			h.Set("Authorization", "Bearer "+t)
		}
	}
	return h
}

func fetchRelease(ctx context.Context, c Config, url string) (Release, error) {
	body, err := c.downloader().Metadata(ctx, url, maxAPIBytes, c.apiHeaders(url))
	if err != nil {
		var se *download.StatusError
		if errors.As(err, &se) && (se.Code == http.StatusForbidden || se.Code == http.StatusTooManyRequests) {
			return Release{}, fmt.Errorf("consultar a release: %w", ErrRateLimited)
		}
		return Release{}, fmt.Errorf("consultar a release: %w", err)
	}
	var raw struct {
		Tag        string `json:"tag_name"`
		Body       string `json:"body"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Release{}, fmt.Errorf("resposta da API ilegível: %w", err)
	}
	v, err := ParseVersion(raw.Tag)
	if err != nil {
		return Release{}, fmt.Errorf("tag da release: %w", err)
	}
	if raw.Draft {
		return Release{}, errors.New("a release ainda é um rascunho")
	}
	return Release{Tag: v.String(), Notes: cleanNotes(raw.Body), URL: textsafe.Line(raw.HTMLURL)}, nil
}

// ReleaseByTag consulta uma release específica ("v0.1.0").
func ReleaseByTag(ctx context.Context, c Config, tag string) (Release, error) {
	v, err := ParseVersion(tag)
	if err != nil {
		return Release{}, err
	}
	return fetchRelease(ctx, c, c.api()+"/tags/"+v.String())
}

// cleanNotes limpa o texto de fora (escapes viram \x1b visível) e limita as linhas.
func cleanNotes(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []string
	for _, l := range lines {
		out = append(out, textsafe.Printable(strings.TrimRight(l, " \t\r")))
		if len(out) == maxNotesLines {
			out = append(out, "…")
			break
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Plan descreve o que o update faria.
type Plan struct {
	Current       string // versão em execução ("dev" se for de desenvolvimento)
	Target        Version
	Newer         bool   // o alvo é MAIOR que o atual
	Same          bool   // o alvo é o atual
	Downgrade     bool   // o alvo é MENOR que o atual (só com versão pedida)
	Asset         string // nome do binário
	AssetURL      string
	ChecksumsURL  string
	Exe           string
	CurrentParsed *Version // nil em build de desenvolvimento
}

// MakePlan resolve o alvo (a release já consultada) contra a versão em execução e monta as URLs.
func MakePlan(c Config, rel Release) (Plan, error) {
	asset, err := AssetName(c.GOOS, c.GOARCH)
	if err != nil {
		return Plan{}, err
	}
	target, err := ParseVersion(rel.Tag)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{
		Current: c.Current, Target: target, Asset: asset, Exe: c.Exe,
		AssetURL:     c.dl() + "/" + target.String() + "/" + asset,
		ChecksumsURL: c.dl() + "/" + target.String() + "/checksums.txt",
	}
	if cur, err := ParseVersion(c.Current); err == nil {
		p.CurrentParsed = &cur
		switch Compare(target, cur) {
		case 1:
			p.Newer = true
		case 0:
			p.Same = true
		default:
			p.Downgrade = true
		}
	}
	return p, nil
}

// ParseChecksums acha o hash do arquivo no formato do sha256sum ("<hash>  <arquivo>", com ou sem o "*").
func ParseChecksums(data []byte, name string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || strings.TrimPrefix(f[1], "*") != name {
			continue
		}
		if len(f[0]) != 64 {
			return "", fmt.Errorf("checksums.txt: hash inválido para %s", name)
		}
		if _, err := hex.DecodeString(f[0]); err != nil {
			return "", fmt.Errorf("checksums.txt: hash inválido para %s", name)
		}
		return strings.ToLower(f[0]), nil
	}
	return "", fmt.Errorf("%w: %s", ErrNotInChecksums, name)
}

// Apply baixa o binário (hash conferido ANTES de qualquer uso), prepara o arquivo ao lado do binário em
// execução, confere o hash de novo e troca. No Unix o rename é atômico; no Windows o executável em uso é
// renomeado para .old (apagado na próxima execução) e o novo entra no lugar, com volta em caso de falha.
func Apply(ctx context.Context, c Config, p Plan) error {
	exe, err := checkTarget(c.Exe)
	if err != nil {
		return err
	}
	sums, err := c.downloader().Metadata(ctx, p.ChecksumsURL, maxChecksumsBytes, nil)
	if err != nil {
		return fmt.Errorf("baixar o checksums.txt: %w", err)
	}
	want, err := ParseChecksums(sums, p.Asset)
	if err != nil {
		return err
	}
	tmp, cleanup, err := download.NewTempDir()
	if err != nil {
		return err
	}
	defer cleanup()
	res, err := c.downloader().File(ctx, download.Request{URL: p.AssetURL, SHA256: want, MaxSize: maxBinaryBytes, Dir: tmp, Name: p.Asset})
	if err != nil {
		return fmt.Errorf("baixar %s: %w", p.Asset, err)
	}
	staged, err := stage(res.Path, exe, want)
	if err != nil {
		return err
	}
	if err := swap(c, exe, staged); err != nil {
		_ = os.Remove(staged)
		return err
	}
	return nil
}

// checkTarget confere que o binário é um arquivo regular de um diretório gravável e NÃO um link simbólico.
func checkTarget(exe string) (string, error) {
	if exe == "" {
		return "", errors.New("não achei o caminho do binário em execução")
	}
	fi, err := os.Lstat(exe)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %s", ErrManagedInstall, exe)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("o binário não é um arquivo regular: %s", exe)
	}
	probe, err := os.CreateTemp(filepath.Dir(exe), ".toolkit-update-probe-*")
	if err != nil {
		return "", fmt.Errorf("%w: %s (instale de novo pelo bootstrap, no PATH do usuário)", ErrNotWritable, filepath.Dir(exe))
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return exe, nil
}

// stage copia o binário baixado para um arquivo ao lado do atual (mesmo volume: o rename depois é atômico),
// com o modo do atual (ou 0755), e confere o SHA-256 do que foi gravado.
func stage(src, exe, want string) (string, error) {
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(exe); err == nil {
		mode = fi.Mode().Perm() | 0o111
	}
	in, err := os.Open(src) //nolint:gosec // arquivo do download, em diretório temporário nosso
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()
	out, err := os.CreateTemp(filepath.Dir(exe), ".toolkit-new-*")
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNotWritable, err)
	}
	name := out.Name()
	h := sha256.New()
	_, cerr := io.Copy(io.MultiWriter(out, h), in)
	if cerr == nil {
		cerr = out.Sync()
	}
	if err := out.Close(); cerr == nil {
		cerr = err
	}
	if cerr == nil {
		cerr = os.Chmod(name, mode)
	}
	if cerr != nil {
		_ = os.Remove(name)
		return "", cerr
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		_ = os.Remove(name)
		return "", fmt.Errorf("%w: esperado %s, gravado %s", ErrStagedMismatch, want, got)
	}
	return name, nil
}

// swap troca o binário pelo preparado. Unix: rename atômico por cima. Windows: o executável em uso não pode ser
// sobrescrito, mas pode ser renomeado: vai para .old e o novo entra; se a segunda troca falhar, volta atrás.
func swap(c Config, exe, staged string) error {
	rename := c.rename()
	if c.GOOS != "windows" {
		if err := rename(staged, exe); err != nil {
			return fmt.Errorf("trocar o binário: %w", err)
		}
		return nil
	}
	old := exe + ".old"
	_ = os.Remove(old) // sobra de uma atualização anterior
	if err := rename(exe, old); err != nil {
		return fmt.Errorf("mover o binário em uso para .old: %w", err)
	}
	if err := rename(staged, exe); err != nil {
		if rerr := rename(old, exe); rerr != nil {
			return fmt.Errorf("trocar o binário (%w) e desfazer (%w): o original está em %s", err, rerr, old)
		}
		return fmt.Errorf("trocar o binário: %w", err)
	}
	return nil
}

// CleanupOld apaga a sobra ".old" de uma atualização no Windows (melhor esforço, nunca falha).
func CleanupOld(exe string) {
	if exe != "" {
		_ = os.Remove(exe + ".old")
	}
}
