package githubrelease

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/PiluTech/toolkit/internal/download"
	"github.com/PiluTech/toolkit/internal/download/downloadtest"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/providers"
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func zipOf(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	_, _ = w.Write([]byte(content))
	_ = zw.Close()
	return buf.Bytes()
}

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest,omitempty"`
	State  string `json:"state"`
}

type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

// fakeGH serve a API e os downloads de um repositório "o/r" com as releases dadas. files: nome do asset -> bytes.
type fakeGH struct {
	mu       sync.Mutex
	releases map[string]release // tag -> release
	latest   string
	files    map[string][]byte // "tag/nome" -> bytes
	reqs     []string          // "host path"
	auths    map[string]string // host -> último Authorization
}

func (g *fakeGH) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.reqs = append(g.reqs, r.Host+r.URL.Path)
	if g.auths == nil {
		g.auths = map[string]string{}
	}
	g.auths[r.Host] = r.Header.Get("Authorization")
	g.mu.Unlock()
	switch {
	case r.Host == "api.github.com" && r.URL.Path == "/repos/o/r/releases/latest":
		_ = json.NewEncoder(w).Encode(g.releases[g.latest])
	case r.Host == "api.github.com" && strings.HasPrefix(r.URL.Path, "/repos/o/r/releases/tags/"):
		rel, ok := g.releases[strings.TrimPrefix(r.URL.Path, "/repos/o/r/releases/tags/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(rel)
	case r.Host == "api.github.com" && r.URL.Path == "/repos/o/r/releases":
		var all []release
		for _, rel := range g.releases {
			all = append(all, rel)
		}
		_ = json.NewEncoder(w).Encode(all)
	case r.Host == "github.com" && strings.HasPrefix(r.URL.Path, "/o/r/releases/download/"):
		rest := strings.TrimPrefix(r.URL.Path, "/o/r/releases/download/")
		if b, ok := g.files[rest]; ok {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (g *fakeGH) count(substr string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, r := range g.reqs {
		if strings.Contains(r, substr) {
			n++
		}
	}
	return n
}

func dl(tag, name string) string {
	return "https://github.com/o/r/releases/download/" + tag + "/" + name
}

// newGH monta v1.2.0 (latest) e v1.1.0, cada uma com tool_<v>_linux_amd64.zip e checksums.txt.
func newGH(t *testing.T) (*fakeGH, map[string][]byte) {
	t.Helper()
	g := &fakeGH{releases: map[string]release{}, files: map[string][]byte{}, latest: "v1.2.0"}
	zips := map[string][]byte{}
	for _, v := range []string{"1.2.0", "1.1.0"} {
		tag := "v" + v
		name := "tool_" + v + "_linux_amd64.zip"
		z := zipOf(t, "tool", "binario "+v)
		zips[v] = z
		sums := fmt.Sprintf("%s  %s\n%s  other_%s.zip\n", sum(z), name, strings.Repeat("a", 64), v)
		g.files[tag+"/"+name] = z
		g.files[tag+"/checksums.txt"] = []byte(sums)
		g.releases[tag] = release{Tag: tag, Assets: []asset{
			{Name: name, URL: dl(tag, name), State: "uploaded"},
			{Name: "tool_" + v + "_windows_amd64.zip", URL: dl(tag, "tool_"+v+"_windows_amd64.zip"), State: "uploaded"},
			{Name: "checksums.txt", URL: dl(tag, "checksums.txt"), State: "uploaded"},
		}}
	}
	return g, zips
}

func newProv(t *testing.T, g http.Handler, r exec.Runner, token string) (*Provider, string) {
	t.Helper()
	d, _ := downloadtest.New(t, g)
	apps := filepath.Join(t.TempDir(), "apps")
	p := New(r, Options{GOOS: "linux", Downloader: d, AppsDir: func() (string, error) { return apps, nil }, Token: func() string { return token }})
	return p, apps
}

type rec struct{ cmds []exec.Cmd }

func (r *rec) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	r.cmds = append(r.cmds, c)
	return exec.Result{}, nil
}
func (*rec) LookPath(string) (string, error) { return "", errors.New("não") }

func ref(asset string) providers.PackageRef {
	return providers.PackageRef{AppID: "tool", Provider: "github-release", Repo: "o/r", Asset: asset}
}

func TestInstallLatestComChecksumsDaRelease(t *testing.T) {
	g, zips := newGH(t)
	p, apps := newProv(t, g, &rec{}, "ghp_abc123")
	res, err := p.Install(context.Background(), ref("tool_*_linux_amd64.zip"), "")
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	got, _ := os.ReadFile(filepath.Join(apps, "tool", "tool"))
	if string(got) != "binario 1.2.0" {
		t.Fatalf("extraiu %q", got)
	}
	_ = zips
	// O token vai só à API, nunca ao host dos downloads.
	if g.auths["api.github.com"] != "Bearer ghp_abc123" {
		t.Errorf("API sem token: %q", g.auths["api.github.com"])
	}
	if a := g.auths["github.com"]; a != "" {
		t.Errorf("o token vazou para o host de download: %q", a)
	}
}

func TestSemTokenNaoMandaAuthorizationECabecalhosDaAPI(t *testing.T) {
	g, _ := newGH(t)
	var seen http.Header
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.github.com" && seen == nil {
			seen = r.Header.Clone()
		}
		g.ServeHTTP(w, r)
	})
	p, _ := newProv(t, h, &rec{}, "")
	if _, err := p.Install(context.Background(), ref("tool_*_linux_amd64.zip"), ""); err != nil {
		t.Fatal(err)
	}
	if seen.Get("Authorization") != "" || seen.Get("Accept") != "application/vnd.github+json" || seen.Get("X-GitHub-Api-Version") != "2022-11-28" {
		t.Fatalf("cabeçalhos: %v", seen)
	}
}

func TestTokenInvalidoEhRecusado(t *testing.T) {
	g, _ := newGH(t)
	p, _ := newProv(t, g, &rec{}, "abc\r\nX-Evil: 1")
	if _, err := p.Install(context.Background(), ref("tool_*_linux_amd64.zip"), ""); err == nil || strings.Contains(err.Error(), "X-Evil") {
		t.Fatalf("err = %v", err)
	}
	if g.count("") != 0 {
		t.Fatal("fez requisição com token inválido")
	}
}

func TestVersaoEscolheATag(t *testing.T) {
	g, _ := newGH(t)
	p, apps := newProv(t, g, &rec{}, "")
	if _, err := p.Install(context.Background(), ref("tool_*_linux_amd64.zip"), "1.1.0"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(apps, "tool", "tool")); string(got) != "binario 1.1.0" {
		t.Fatalf("%q", got)
	}
	// Tag sem "v": cai no nome puro quando "v<versão>" dá 404.
	g.releases["1.0.0"] = release{Tag: "1.0.0", Assets: []asset{{Name: "tool_1.0.0_linux_amd64.zip", URL: dl("1.0.0", "tool_1.0.0_linux_amd64.zip"), Digest: "sha256:" + sum(zipOf(t, "tool", "binario 1.0.0")), State: "uploaded"}}}
	g.files["1.0.0/tool_1.0.0_linux_amd64.zip"] = zipOf(t, "tool", "binario 1.0.0")
	if _, err := p.Install(context.Background(), ref("tool_*_linux_amd64.zip"), "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Install(context.Background(), ref("tool_*_linux_amd64.zip"), "9.9.9"); !errors.Is(err, providers.ErrNotFound) {
		t.Fatalf("versão inexistente: %v", err)
	}
}

func TestOrigemDoHash(t *testing.T) {
	z := zipOf(t, "tool", "bin")
	other := strings.Repeat("b", 64)
	cases := []struct {
		name   string
		pin    string
		digest string
		sums   string // conteúdo do checksums.txt ("" = sem arquivo)
		want   error  // nil = instala
	}{
		{"só checksums", "", "", sum(z) + "  a.zip\n", nil},
		{"só digest", "", "sha256:" + sum(z), "", nil},
		{"pin sozinho", sum(z), "", "", nil},
		{"pin vence a API divergente", sum(z), "sha256:" + other, other + "  a.zip\n", nil},
		{"pin errado", other, "sha256:" + sum(z), sum(z) + "  a.zip\n", providers.ErrHashMismatch},
		{"checksums e digest divergem", "", "sha256:" + other, sum(z) + "  a.zip\n", providers.ErrHashMismatch},
		{"checksums não bate com o arquivo", "", "", other + "  a.zip\n", providers.ErrHashMismatch},
		{"sem nenhuma fonte", "", "", "", providers.ErrInvalidRef},
		{"checksums sem a linha do asset", "", "", sum(z) + "  outro.zip\n", providers.ErrInvalidRef},
		{"digest de outro algoritmo", "", "sha512:abc", "", providers.ErrInvalidRef},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := &fakeGH{releases: map[string]release{}, files: map[string][]byte{"v1/a.zip": z}, latest: "v1"}
			assets := []asset{{Name: "a.zip", URL: dl("v1", "a.zip"), Digest: c.digest, State: "uploaded"}}
			if c.sums != "" {
				g.files["v1/SHA256SUMS"] = []byte(c.sums)
				assets = append(assets, asset{Name: "SHA256SUMS", URL: dl("v1", "SHA256SUMS"), State: "uploaded"})
			}
			g.releases["v1"] = release{Tag: "v1", Assets: assets}
			p, apps := newProv(t, g, &rec{}, "")
			r := ref("a.zip")
			r.SHA256 = c.pin
			_, err := p.Install(context.Background(), r, "")
			if c.want == nil {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, quer %v", err, c.want)
			}
			if _, statErr := os.Stat(filepath.Join(apps, "tool")); statErr == nil {
				t.Fatal("instalou apesar do erro")
			}
			if errors.Is(c.want, providers.ErrInvalidRef) && g.count("/releases/download/v1/a.zip") != 0 {
				t.Fatal("baixou o asset sem ter hash")
			}
		})
	}
}

func TestParseChecksums(t *testing.T) {
	h1, h2, h3 := strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64)
	data := h1 + "  a.zip\n" + h2 + " *b.zip\r\nSHA256 (c.zip) = " + h3 + "\nlixo\n" + strings.ToUpper(h1) + "  d.zip\n" + "abc  e.zip\n"
	got := ParseChecksums([]byte(data))
	want := map[string]string{"a.zip": h1, "b.zip": h2, "c.zip": h3, "d.zip": h1}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, quer %q", k, got[k], v)
		}
	}
}

func TestSelecaoDoAsset(t *testing.T) {
	g, _ := newGH(t)
	p, _ := newProv(t, g, &rec{}, "")
	if _, err := p.Install(context.Background(), ref("nada_*.zip"), ""); !errors.Is(err, providers.ErrNotFound) {
		t.Fatalf("sem casamento: %v", err)
	}
	if _, err := p.Install(context.Background(), ref("tool_*.zip"), ""); !errors.Is(err, providers.ErrInvalidRef) || !strings.Contains(err.Error(), "ambíguo") {
		t.Fatalf("ambíguo: %v", err)
	}
}

func TestUrlDoAssetForaDoRepositorioEhRecusada(t *testing.T) {
	z := zipOf(t, "x", "x")
	for name, u := range map[string]string{
		"outro host": "https://evil.example.com/o/r/releases/download/v1/a.zip",
		"outro repo": "https://github.com/x/y/releases/download/v1/a.zip",
		"caminho":    "https://github.com/o/r/archive/a.zip",
		"http":       "http://github.com/o/r/releases/download/v1/a.zip",
		"ip":         "https://127.0.0.1/o/r/releases/download/v1/a.zip",
	} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGH{releases: map[string]release{"v1": {Tag: "v1", Assets: []asset{{Name: "a.zip", URL: u, Digest: "sha256:" + sum(z), State: "uploaded"}}}}, files: map[string][]byte{}, latest: "v1"}
			p, _ := newProv(t, g, &rec{}, "")
			_, err := p.Install(context.Background(), ref("a.zip"), "")
			if !errors.Is(err, providers.ErrInvalidRef) {
				t.Fatalf("err = %v", err)
			}
			if n := g.count("/download/"); n != 0 {
				t.Fatalf("baixou: %d", n)
			}
		})
	}
}

func TestDraftEPrereleaseNaoSaoLatest(t *testing.T) {
	g := &fakeGH{releases: map[string]release{"v2": {Tag: "v2", Draft: true}}, latest: "v2"}
	p, _ := newProv(t, g, &rec{}, "")
	if _, err := p.Install(context.Background(), ref("a.zip"), ""); !errors.Is(err, providers.ErrNotFound) {
		t.Fatalf("draft: %v", err)
	}
}

func TestSimulacaoLeAAPIMasNaoBaixaNemInstala(t *testing.T) {
	g, _ := newGH(t)
	d, _ := downloadtest.New(t, g)
	inner := &rec{}
	dry := exec.NewDryRunRunner(inner)
	apps := filepath.Join(t.TempDir(), "apps")
	p := New(dry, Options{GOOS: "linux", Downloader: d, AppsDir: func() (string, error) { return apps, nil }})
	if _, err := p.Install(context.Background(), ref("tool_*_linux_amd64.zip"), ""); err != nil {
		t.Fatal(err)
	}
	if g.count("/releases/download/v1.2.0/tool_1.2.0_linux_amd64.zip") != 0 {
		t.Fatal("a simulação baixou o asset")
	}
	if g.count("/releases/latest") != 1 || g.count("/checksums.txt") != 1 {
		t.Fatalf("leituras: %v", g.reqs)
	}
	plan := dry.Plan()
	if len(plan) != 2 || !strings.Contains(plan[0].Note, dl("v1.2.0", "tool_1.2.0_linux_amd64.zip")) || !strings.Contains(plan[1].Note, filepath.Join(apps, "tool")) {
		t.Fatalf("plano: %+v", plan)
	}
	if _, err := os.Stat(apps); err == nil {
		t.Fatal("criou a pasta de apps")
	}
}

func TestListVersions(t *testing.T) {
	g, _ := newGH(t)
	g.releases["v2.0.0-rc1"] = release{Tag: "v2.0.0-rc1", Prerelease: true}
	g.releases["v0.9"] = release{Tag: "v0.9", Draft: true}
	g.releases["nightly weird"] = release{Tag: "nightly weird"}
	p, _ := newProv(t, g, &rec{}, "")
	vs, err := p.ListVersions(context.Background(), ref("a.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(vs, ",") != "1.2.0,1.1.0" && strings.Join(vs, ",") != "1.1.0,1.2.0" {
		t.Fatalf("%v", vs)
	}
}

func TestLimiteDeRequisicoesTemDica(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "rate limit", http.StatusForbidden) })
	p, _ := newProv(t, h, &rec{}, "")
	_, err := p.Install(context.Background(), ref("a.zip"), "")
	var se *download.StatusError
	if !errors.As(err, &se) || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidate(t *testing.T) {
	p := New(&rec{}, Options{GOOS: "linux"})
	for _, ok := range []providers.PackageRef{ref("tool_*_linux_amd64.zip"), ref("x.deb"), func() providers.PackageRef { r := ref("x*"); r.Type = "tar.gz"; return r }()} {
		if err := p.Validate(ok); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	mk := func(f func(*providers.PackageRef)) providers.PackageRef { r := ref("a.zip"); f(&r); return r }
	bad := map[string]providers.PackageRef{
		"repo vazio":       mk(func(r *providers.PackageRef) { r.Repo = "" }),
		"repo sem dono":    mk(func(r *providers.PackageRef) { r.Repo = "semdono" }),
		"repo ..":          mk(func(r *providers.PackageRef) { r.Repo = "../x" }),
		"asset vazio":      mk(func(r *providers.PackageRef) { r.Asset = "" }),
		"asset com barra":  mk(func(r *providers.PackageRef) { r.Asset = "a/b.zip" }),
		"glob inválido":    mk(func(r *providers.PackageRef) { r.Asset = "[a.zip" }),
		"sha curto":        mk(func(r *providers.PackageRef) { r.SHA256 = "abc" }),
		"campo alheio":     mk(func(r *providers.PackageRef) { r.URL = "https://example.com/a" }),
		"tipo de outro so": ref("a.msi"),
		"sem tipo":         ref("tool_*"),
		"args em zip":      mk(func(r *providers.PackageRef) { r.Args = []string{"/S"} }),
	}
	for name, r := range bad {
		if err := p.Validate(r); !errors.Is(err, providers.ErrInvalidRef) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
