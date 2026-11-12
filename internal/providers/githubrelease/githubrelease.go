// Package githubrelease é o provider "github-release": escolhe uma release de um repositório do GitHub, o asset
// pelo glob do catálogo, baixa com SHA-256 obrigatório e instala conforme o tipo (pacote artifact).
//
// Origem do hash do asset, nesta ordem (decisão 18):
//  1. sha256 fixado no catálogo (autoridade: se existe, a API e os checksums nem são consultados);
//  2. arquivo de checksums da própria release (checksums.txt, SHA256SUMS, *_checksums.txt, <asset>.sha256);
//  3. campo digest do asset na API ("sha256:...").
//
// Se (2) e (3) existem e divergem, recusa. Sem nenhum dos três, recusa ANTES de baixar o asset. Só o pin do
// catálogo é independente do GitHub: checksums e digest vêm do mesmo canal da release (protegem contra
// corrupção e CDN, não contra uma release comprometida); para isso, fixe o sha256 no catálogo. Assinatura dos
// checksums (cosign, minisign, gpg) NÃO é verificada: fica para uma fase futura.
//
// A API é consultada com GITHUB_TOKEN opcional, enviado SÓ ao api.github.com, nunca aos downloads. A URL de
// cada asset vem da API mas só vale se for https://github.com/<repo>/releases/download/...; os redirects para
// a CDN passam pela checagem de URL do pacote download. IsInstalled devolve sempre "não" (limite como o do
// provider url). Na simulação (dry-run) a API e o arquivo de checksums são lidos (leituras), o asset não.
package githubrelease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"path"
	"regexp"
	"runtime"
	"strings"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/download"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/providers"
	"github.com/PiluTech/toolkit/internal/providers/artifact"
)

const (
	apiBase      = "https://api.github.com"
	apiVersion   = "2022-11-28"
	maxAPIBytes  = 8 << 20
	maxSumsBytes = 1 << 20
)

var (
	repoRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)
	tokenRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
	hex64Re = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	zeroSHA = strings.Repeat("0", 64)
)

// Options configura o provider; todos os campos têm padrão.
type Options struct {
	GOOS       string               // padrão runtime.GOOS
	Downloader *download.Downloader // padrão: cliente estrito do pacote download
	AppsDir    func() (string, error)
	// Token devolve o token da API (padrão: nenhum). A CLI passa um leitor de GITHUB_TOKEN.
	Token func() string
}

// Provider implementa providers.Provider para o tipo "github-release".
type Provider struct {
	goos  string
	dl    *download.Downloader
	env   artifact.Env
	token func() string
}

// New cria o provider sobre o Runner. Se r for o DryRunRunner (exec.Planner), o provider SIMULA.
func New(r exec.Runner, opts Options) *Provider {
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	dl := opts.Downloader
	if dl == nil {
		dl = &download.Downloader{}
	}
	pl, _ := r.(exec.Planner)
	return &Provider{goos: goos, dl: dl, token: opts.Token,
		env: artifact.Env{GOOS: goos, Rn: providers.NewRunners(r), Planner: pl, Downloader: dl, AppsDir: opts.AppsDir}}
}

// Name devolve "github-release".
func (*Provider) Name() string { return catalog.ProviderGitHubRelease }

// Available é sempre verdadeiro: não depende de gerenciador instalado.
func (*Provider) Available(context.Context) bool { return true }

// Validate confere repo, glob do asset, sha256 opcional e se o tipo instala neste SO.
func (p *Provider) Validate(ref providers.PackageRef) error {
	if err := providers.OnlyFields(ref, "repo", "asset", "sha256", "type", "args"); err != nil {
		return err
	}
	if !repoRe.MatchString(ref.Repo) || strings.Contains(ref.Repo, "..") || len(ref.Repo) > providers.MaxFieldLen {
		return fmt.Errorf("%w: repo %q deve ser dono/repositório", providers.ErrInvalidRef, ref.Repo)
	}
	if ref.Asset == "" || len(ref.Asset) > providers.MaxFieldLen || strings.ContainsAny(ref.Asset, `/\`) {
		return fmt.Errorf("%w: asset %q inválido (glob de um nome de arquivo, sem barras)", providers.ErrInvalidRef, ref.Asset)
	}
	if _, err := path.Match(ref.Asset, "x"); err != nil {
		return fmt.Errorf("%w: asset %q: %w", providers.ErrInvalidRef, ref.Asset, err)
	}
	if ref.SHA256 != "" && !hex64Re.MatchString(ref.SHA256) {
		return fmt.Errorf("%w: sha256 deve ter 64 caracteres hexadecimais", providers.ErrInvalidRef)
	}
	// O tipo vem do glob (extensão) ou de type; o hash real só existe depois da API, então aqui vai um marcador.
	return artifact.ValidateItem(p.goos, artifact.Item{AppID: ref.AppID, SHA256: zeroSHA, Type: ref.Type, Name: ref.Asset, Args: ref.Args})
}

// IsInstalled devolve sempre "não instalado" (ver o comentário do pacote).
func (*Provider) IsInstalled(context.Context, providers.PackageRef) (bool, string, error) {
	return false, "", nil
}

// SupportsVersion aceita qualquer versão: ela escolhe a tag da release.
func (*Provider) SupportsVersion(providers.PackageRef, string) error { return nil }

type apiAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	State  string `json:"state"`
}

type apiRelease struct {
	Tag        string     `json:"tag_name"`
	Draft      bool       `json:"draft"`
	Prerelease bool       `json:"prerelease"`
	Assets     []apiAsset `json:"assets"`
}

func (p *Provider) api(ctx context.Context, apiPath string) ([]byte, error) {
	h := http.Header{}
	h.Set("Accept", "application/vnd.github+json")
	h.Set("X-GitHub-Api-Version", apiVersion)
	if p.token != nil {
		if tok := p.token(); tok != "" {
			if !tokenRe.MatchString(tok) {
				return nil, errors.New("GITHUB_TOKEN com caracteres inválidos (esperado só letras, números, _ . -)")
			}
			h.Set("Authorization", "Bearer "+tok)
		}
	}
	b, err := p.dl.Metadata(ctx, apiBase+apiPath, maxAPIBytes, h)
	var se *download.StatusError
	if errors.As(err, &se) && (se.Code == http.StatusForbidden || se.Code == http.StatusTooManyRequests) {
		return nil, fmt.Errorf("%w (limite de requisições da API do GitHub? defina GITHUB_TOKEN)", err)
	}
	return b, err
}

func isNotFound(err error) bool {
	var se *download.StatusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}

func (p *Provider) release(ctx context.Context, repo, version string) (apiRelease, error) {
	var rel apiRelease
	if version == "" || version == providers.Latest {
		b, err := p.api(ctx, "/repos/"+repo+"/releases/latest")
		if err != nil {
			if isNotFound(err) {
				return rel, fmt.Errorf("%w: %s não tem release publicada", providers.ErrNotFound, repo)
			}
			return rel, err
		}
		if err := json.Unmarshal(b, &rel); err != nil {
			return rel, fmt.Errorf("resposta da API do GitHub inválida: %w", err)
		}
		if rel.Draft || rel.Prerelease {
			return rel, fmt.Errorf("%w: a release mais recente de %s é rascunho ou pré-release", providers.ErrNotFound, repo)
		}
		return rel, nil
	}
	for _, tag := range []string{"v" + version, version} {
		b, err := p.api(ctx, "/repos/"+repo+"/releases/tags/"+neturl.PathEscape(tag))
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return rel, err
		}
		if err := json.Unmarshal(b, &rel); err != nil {
			return rel, fmt.Errorf("resposta da API do GitHub inválida: %w", err)
		}
		if rel.Draft {
			continue
		}
		return rel, nil
	}
	return rel, fmt.Errorf("%w: %s não tem release para a versão %q", providers.ErrNotFound, repo, version)
}

// ListVersions lista as tags publicadas (sem rascunho nem pré-release), sem o "v" inicial.
func (p *Provider) ListVersions(ctx context.Context, ref providers.PackageRef) ([]string, error) {
	if err := p.Validate(ref); err != nil {
		return nil, err
	}
	b, err := p.api(ctx, "/repos/"+ref.Repo+"/releases?per_page=50")
	if err != nil {
		return nil, err
	}
	var rels []apiRelease
	if err := json.Unmarshal(b, &rels); err != nil {
		return nil, fmt.Errorf("resposta da API do GitHub inválida: %w", err)
	}
	var vs []string
	for _, r := range rels {
		v := strings.TrimPrefix(r.Tag, "v")
		if r.Draft || r.Prerelease || !providers.ValidVersion(v) {
			continue
		}
		vs = append(vs, v)
	}
	if len(vs) == 0 {
		return nil, fmt.Errorf("%w: %s sem releases", providers.ErrNotFound, ref.Repo)
	}
	return vs, nil
}

// assetURL aceita só https://github.com/<repo>/releases/download/... (sem userinfo, query ou fragmento).
func assetURL(repo, raw string) error {
	u, err := neturl.Parse(raw)
	prefix := "/" + strings.ToLower(repo) + "/releases/download/"
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		!strings.HasPrefix(strings.ToLower(u.Path), prefix) {
		return fmt.Errorf("%w: a API apontou para uma URL fora de https://github.com/%s/releases/download/", providers.ErrInvalidRef, repo)
	}
	return catalog.CheckURL(raw, false)
}

func pick(rel apiRelease, glob string) (apiAsset, error) {
	var matches []apiAsset
	for _, a := range rel.Assets {
		if a.State != "" && a.State != "uploaded" {
			continue
		}
		if ok, _ := path.Match(glob, a.Name); ok {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return apiAsset{}, fmt.Errorf("%w: nenhum asset da release %q casa com %q", providers.ErrNotFound, rel.Tag, glob)
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, fmt.Sprintf("%q", m.Name))
	}
	if len(names) > 5 {
		names = append(names[:5], "...")
	}
	return apiAsset{}, fmt.Errorf("%w: o glob %q é ambíguo na release %q (%s); torne-o mais específico", providers.ErrInvalidRef, glob, rel.Tag, strings.Join(names, ", "))
}

// ParseChecksums lê "hex  arquivo", "hex *arquivo" e "SHA256 (arquivo) = hex" e devolve arquivo -> hex (minúsculo).
// Linha malformada é ignorada; o mesmo arquivo com dois hashes diferentes é descartado (ambíguo).
func ParseChecksums(data []byte) map[string]string {
	out := map[string]string{}
	conflict := map[string]bool{}
	add := func(name, h string) {
		name = strings.TrimPrefix(name, "./")
		if name == "" || !hex64Re.MatchString(h) {
			return
		}
		h = strings.ToLower(h)
		if old, ok := out[name]; ok && old != h {
			conflict[name] = true
		}
		out[name] = h
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "SHA256 ("); ok {
			if name, h, ok := strings.Cut(rest, ") = "); ok {
				add(name, h)
			}
			continue
		}
		if h, name, ok := strings.Cut(line, " "); ok {
			add(strings.TrimPrefix(strings.TrimSpace(name), "*"), h)
		}
	}
	for n := range conflict {
		delete(out, n)
	}
	return out
}

var sumNames = map[string]bool{"checksums.txt": true, "sha256sums": true, "sha256sums.txt": true, "sha256sum.txt": true, "checksums.sha256": true}

func isSumsFile(name, asset string) bool {
	low := strings.ToLower(name)
	return sumNames[low] || strings.HasSuffix(low, "_checksums.txt") || strings.HasSuffix(low, "_sha256sums.txt") ||
		low == strings.ToLower(asset)+".sha256" || low == strings.ToLower(asset)+".sha256sum"
}

// checksumFor procura o hash de a nos arquivos de checksums da release (vazio = nenhum arquivo o lista).
func (p *Provider) checksumFor(ctx context.Context, repo string, rel apiRelease, a apiAsset) (string, error) {
	for _, f := range rel.Assets {
		if f.Name == a.Name || !isSumsFile(f.Name, a.Name) || (f.State != "" && f.State != "uploaded") {
			continue
		}
		if err := assetURL(repo, f.URL); err != nil {
			return "", err
		}
		b, err := p.dl.Metadata(ctx, f.URL, maxSumsBytes, nil)
		if err != nil {
			return "", fmt.Errorf("baixando %q da release: %w", f.Name, err)
		}
		if h := ParseChecksums(b)[a.Name]; h != "" {
			return h, nil
		}
		// Arquivo .sha256 só com o hash (sem nome).
		if strings.EqualFold(f.Name, a.Name+".sha256") || strings.EqualFold(f.Name, a.Name+".sha256sum") {
			if fields := strings.Fields(string(b)); len(fields) > 0 && hex64Re.MatchString(fields[0]) {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	return "", nil
}

func digestOf(a apiAsset) string {
	if h, ok := strings.CutPrefix(a.Digest, "sha256:"); ok && hex64Re.MatchString(h) {
		return strings.ToLower(h)
	}
	return ""
}

// resolveHash aplica a ordem do pacote: pin, depois checksums e digest (que, se ambos existem, têm de coincidir).
func (p *Provider) resolveHash(ctx context.Context, ref providers.PackageRef, rel apiRelease, a apiAsset) (string, error) {
	if ref.SHA256 != "" {
		return strings.ToLower(ref.SHA256), nil
	}
	sums, err := p.checksumFor(ctx, ref.Repo, rel, a)
	if err != nil {
		return "", err
	}
	dig := digestOf(a)
	switch {
	case sums != "" && dig != "" && sums != dig:
		return "", fmt.Errorf("%w: o arquivo de checksums e o digest da API divergem para %q; nada foi baixado", providers.ErrHashMismatch, a.Name)
	case sums != "":
		return sums, nil
	case dig != "":
		return dig, nil
	}
	return "", fmt.Errorf("%w: a release %q não traz checksums nem digest para %q; fixe o sha256 no catálogo", providers.ErrInvalidRef, rel.Tag, a.Name)
}

// Install escolhe a release (a versão, se vier, é a tag com ou sem "v"), o asset e o hash, e instala.
func (p *Provider) Install(ctx context.Context, ref providers.PackageRef, version string) (providers.InstallResult, error) {
	if err := p.Validate(ref); err != nil {
		return providers.InstallResult{}, err
	}
	rel, err := p.release(ctx, ref.Repo, version)
	if err != nil {
		return providers.InstallResult{}, err
	}
	a, err := pick(rel, ref.Asset)
	if err != nil {
		return providers.InstallResult{}, err
	}
	if err := assetURL(ref.Repo, a.URL); err != nil {
		return providers.InstallResult{}, err
	}
	sha, err := p.resolveHash(ctx, ref, rel, a)
	if err != nil {
		return providers.InstallResult{}, err
	}
	return p.env.Install(ctx, artifact.Item{AppID: ref.AppID, URL: a.URL, SHA256: sha, Type: ref.Type, Name: a.Name, Args: ref.Args})
}
