package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/download/downloadtest"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

func urlCatalog(sha string) string {
	return `apps:
  exemplo:
    name: Exemplo
    category: utilitarios
    windows: [{ provider: url, url: "https://example.com/dl/exemplo.msi", sha256: ` + sha + `, args: ["ALLUSERS=1"] }]
    macos: [{ provider: url, url: "https://example.com/dl/exemplo.zip", sha256: ` + sha + ` }]
`
}

func hashOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// O plano do provider url mostra o download e o comando, e --dry-run NÃO faz nenhuma requisição.
func TestInstallURLDryRunNaoBaixaNada(t *testing.T) {
	f := exectest.New(t)
	var o, e bytes.Buffer
	deps := Deps{GOOS: "windows", EmbeddedYML: []byte(urlCatalog(hashOf([]byte("x")))), NewRunner: func() exec.Runner { return f },
		CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false }, Downloader: downloadtest.Forbidden(t)}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"install", "exemplo", "--dry-run"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, e.String())
	}
	out := o.String()
	for _, want := range []string{"baixar https://example.com/dl/exemplo.msi (sha256 " + hashOf([]byte("x")), "msiexec.exe /i", "ALLUSERS=1", "Plano (dry-run"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
	if len(f.Calls()) != 0 {
		t.Errorf("o Runner foi usado: %v", f.Calls())
	}
}

// No install real, o plano é impresso e NADA é baixado antes da confirmação; recusar não baixa nada.
func TestInstallURLRecusarConfirmacaoNaoBaixa(t *testing.T) {
	f := exectest.New(t)
	dl, hits := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) }))
	var o, e bytes.Buffer
	deps := Deps{GOOS: "windows", EmbeddedYML: []byte(urlCatalog(hashOf([]byte("x")))), NewRunner: func() exec.Runner { return f },
		CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return true }, Stdin: strings.NewReader("n\n"), Downloader: dl}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"install", "exemplo"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, e.String())
	}
	if hits.Load() != 0 || !strings.Contains(o.String(), "cancelado") || !strings.Contains(o.String(), "baixar https://example.com/dl/exemplo.msi") {
		t.Fatalf("hits=%d\n%s", hits.Load(), o.String())
	}
}

// Arquivo compactado no macOS: plano com a nota de extração, sem processo.
func TestInstallURLZipPlanoSoComNota(t *testing.T) {
	var o, e bytes.Buffer
	deps := Deps{GOOS: "darwin", EmbeddedYML: []byte(urlCatalog(hashOf([]byte("x")))), NewRunner: func() exec.Runner { return exectest.New(t) },
		CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false }, Downloader: downloadtest.Forbidden(t),
		AppsDir: func() (string, error) { return "/apps", nil }}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"install", "exemplo", "--dry-run"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, e.String())
	}
	if !strings.Contains(o.String(), "extrair o zip em /apps/exemplo") {
		t.Fatalf("saída:\n%s", o.String())
	}
}

func TestInstallGitHubReleaseDryRunLeAAPIMasNaoBaixaOAsset(t *testing.T) {
	z := []byte("zip")
	dl, hits := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "api.github.com":
			_, _ = w.Write([]byte(`{"tag_name":"v1.0.0","assets":[{"name":"tool_linux.tar.gz","state":"uploaded","digest":"sha256:` + hashOf(z) + `","browser_download_url":"https://github.com/o/r/releases/download/v1.0.0/tool_linux.tar.gz"}]}`))
		default:
			t.Errorf("download inesperado no dry-run: %s%s", r.Host, r.URL.Path)
		}
	}))
	var o, e bytes.Buffer
	yml := "apps:\n  tool:\n    name: Tool\n    category: dev\n    linux: [{ provider: github-release, repo: o/r, asset: \"tool_*.tar.gz\" }]\n"
	deps := Deps{GOOS: "linux", EmbeddedYML: []byte(yml), NewRunner: func() exec.Runner { return exectest.New(t) }, CheckElevation: func(exec.Cmd) error { return nil },
		Interactive: func() bool { return false }, Downloader: dl, GitHubToken: func() string { return "" }, AppsDir: func() (string, error) { return "/apps", nil }}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"install", "tool", "--dry-run"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, e.String())
	}
	if hits.Load() != 1 || !strings.Contains(o.String(), "baixar https://github.com/o/r/releases/download/v1.0.0/tool_linux.tar.gz (sha256 "+hashOf(z)) || !strings.Contains(o.String(), "extrair o tar.gz em /apps/tool") {
		t.Fatalf("hits=%d\n%s", hits.Load(), o.String())
	}
}
