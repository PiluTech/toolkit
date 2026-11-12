package cli

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/download/downloadtest"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

const remoteYML = `apps:
  remoto:
    name: App Remoto
    category: utilitarios
    linux: [{ provider: apt, package: remoto }]
  git:
    name: Git do catálogo remoto
    category: dev
    linux: [{ provider: apt, package: git-remoto }]
`

func serveBytes(b []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(b) })
}

func runWith(t *testing.T, deps Deps, args ...string) (out, errOut string, err error) {
	t.Helper()
	var o, e bytes.Buffer
	deps.GOOS, deps.EmbeddedYML = "linux", []byte(embedded)
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(context.Background())
	return o.String(), e.String(), err
}

func TestCatalogoPorURLComHashMesclaSobreOEmbutido(t *testing.T) {
	dl, hits := downloadtest.New(t, serveBytes([]byte(remoteYML)))
	out, errOut, err := runWith(t, Deps{Downloader: dl}, "list", "--catalog", "https://example.com/cat/c.yaml", "--catalog-sha256", strings.ToUpper(hashOf([]byte(remoteYML))))
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut)
	}
	if hits.Load() != 1 || !strings.Contains(out, "remoto") || !strings.Contains(out, "firefox") || !strings.Contains(out, "Git do catálogo remoto") {
		t.Fatalf("hits=%d\n%s", hits.Load(), out)
	}
	if !strings.Contains(errOut, `substituiu o app embutido "git"`) {
		t.Errorf("sem aviso de substituição: %q", errOut)
	}
}

func TestCatalogoPorURLComReplace(t *testing.T) {
	dl, _ := downloadtest.New(t, serveBytes([]byte(remoteYML)))
	out, _, err := runWith(t, Deps{Downloader: dl}, "list", "--catalog", "https://example.com/c.yaml", "--catalog-sha256", hashOf([]byte(remoteYML)), "--catalog-replace")
	if err != nil || strings.Contains(out, "firefox") || !strings.Contains(out, "remoto") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestCatalogoPorURLNoInstallDryRun(t *testing.T) {
	dl, _ := downloadtest.New(t, serveBytes([]byte(remoteYML)))
	f := exectest.New(t).WithPath("apt-get", "/usr/bin/apt-get").WithPath("apt-cache", "/usr/bin/apt-cache").WithPath("dpkg-query", "/usr/bin/dpkg-query").
		On(`dpkg-query -W "-f=${db:Status-Status}\t${Version}\n" remoto`, exectest.Reply{Result: exec.Result{ExitCode: 1}})
	out, errOut, err := runWith(t, Deps{Downloader: dl, NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false }},
		"install", "remoto", "--dry-run", "--catalog", "https://example.com/c.yaml", "--catalog-sha256", hashOf([]byte(remoteYML)))
	if err != nil || !strings.Contains(out, "apt-get install -y --no-remove remoto") {
		t.Fatalf("%v\n%s\n%s", err, out, errOut)
	}
}

func TestCatalogoPorURLSemHashOuHashInvalidoNaoFazRequisicao(t *testing.T) {
	forbidden := downloadtest.Forbidden(t)
	for name, args := range map[string][]string{
		"sem hash":         {"list", "--catalog", "https://example.com/c.yaml"},
		"hash curto":       {"list", "--catalog", "https://example.com/c.yaml", "--catalog-sha256", "abc"},
		"hash não hex":     {"list", "--catalog", "https://example.com/c.yaml", "--catalog-sha256", strings.Repeat("z", 64)},
		"install sem hash": {"install", "git", "--dry-run", "--catalog", "https://example.com/c.yaml"},
	} {
		_, _, err := runWith(t, Deps{Downloader: forbidden}, args...)
		if err == nil {
			t.Errorf("%s: esperava erro", name)
		}
		if name == "sem hash" && !strings.Contains(err.Error(), "--catalog-sha256") {
			t.Errorf("mensagem: %v", err)
		}
	}
}

func TestCatalogoPorURLHashDivergenteNaoCarregaNada(t *testing.T) {
	dl, _ := downloadtest.New(t, serveBytes([]byte(remoteYML)))
	out, _, err := runWith(t, Deps{Downloader: dl}, "list", "--catalog", "https://example.com/c.yaml", "--catalog-sha256", strings.Repeat("a", 64))
	if err == nil || !strings.Contains(err.Error(), "não confere") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(out, "remoto") {
		t.Fatalf("carregou conteúdo não verificado:\n%s", out)
	}
}

func TestCatalogoPorURLRecusaHTTPEHostLocal(t *testing.T) {
	for _, u := range []string{"http://example.com/c.yaml", "https://127.0.0.1/c.yaml", "https://localhost/c.yaml", "https://u:p@example.com/c.yaml", "HTTPS://example.com/c.yaml"} {
		_, _, err := runWith(t, Deps{Downloader: downloadtest.Forbidden(t)}, "list", "--catalog", u, "--catalog-sha256", strings.Repeat("a", 64))
		if err == nil {
			t.Errorf("%s: esperava erro", u)
		}
	}
}

func TestCatalogoPorURLMaiorQueOLimite(t *testing.T) {
	big := bytes.Repeat([]byte("#"), 1<<20+10)
	dl, _ := downloadtest.New(t, serveBytes(big))
	_, _, err := runWith(t, Deps{Downloader: dl}, "list", "--catalog", "https://example.com/c.yaml", "--catalog-sha256", hashOf(big))
	if err == nil || !strings.Contains(err.Error(), "limite") {
		t.Fatalf("err = %v", err)
	}
}

func TestCatalogoPorURLInvalidoCitaAURLELinha(t *testing.T) {
	ruim := []byte("apps:\n  x:\n    name: X\n    category: dev\n    pakage: y\n    any: [{ provider: apt, package: x }]\n")
	dl, _ := downloadtest.New(t, serveBytes(ruim))
	_, _, err := runWith(t, Deps{Downloader: dl}, "list", "--catalog", "https://example.com/cat/ruim.yaml", "--catalog-sha256", hashOf(ruim))
	if err == nil || !strings.Contains(err.Error(), "example.com/cat/ruim.yaml:5:5") {
		t.Fatalf("err = %v", err)
	}
}

func TestCatalogoPorURLRedirectParaIPERecusado(t *testing.T) {
	dl, _ := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://127.0.0.1/c.yaml", http.StatusFound)
	}))
	_, _, err := runWith(t, Deps{Downloader: dl}, "list", "--catalog", "https://example.com/c.yaml", "--catalog-sha256", strings.Repeat("a", 64))
	if err == nil || !strings.Contains(err.Error(), "url recusada") {
		t.Fatalf("err = %v", err)
	}
}

func TestHashSemURLContinuaErro(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p, []byte(remoteYML), 0o600)
	if _, _, err := runWith(t, Deps{Downloader: downloadtest.Forbidden(t)}, "list", "--catalog", p, "--catalog-sha256", strings.Repeat("a", 64)); err == nil {
		t.Fatal("--catalog-sha256 com arquivo local deve continuar recusado")
	}
}
