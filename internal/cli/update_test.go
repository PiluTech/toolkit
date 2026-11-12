package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/PiluTech/toolkit/internal/download/downloadtest"
)

type updRelease struct {
	mu     sync.Mutex
	tag    string
	notes  string
	binary []byte
	hits   map[string]int
}

func (r *updRelease) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hits == nil {
		r.hits = map[string]int{}
	}
	r.hits[req.URL.Path]++
	h := sha256.Sum256(r.binary)
	switch {
	case strings.HasSuffix(req.URL.Path, "/api/latest"), strings.Contains(req.URL.Path, "/api/tags/"):
		tag := r.tag
		if i := strings.Index(req.URL.Path, "/api/tags/"); i >= 0 {
			tag = req.URL.Path[i+len("/api/tags/"):]
		}
		b, _ := json.Marshal(map[string]any{"tag_name": tag, "body": r.notes, "html_url": "https://example.com/rel"})
		_, _ = w.Write(b)
	case strings.HasSuffix(req.URL.Path, "/checksums.txt"):
		_, _ = fmt.Fprintf(w, "%s  toolkit_linux_amd64\n", hex.EncodeToString(h[:]))
	case strings.HasSuffix(req.URL.Path, "/toolkit_linux_amd64"):
		_, _ = w.Write(r.binary)
	default:
		http.NotFound(w, req)
	}
}

func (r *updRelease) count(suffix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for p, c := range r.hits {
		if strings.HasSuffix(p, suffix) {
			n += c
		}
	}
	return n
}

func runUpdateCmd(t *testing.T, rel *updRelease, current string, args ...string) (out string, exe string, err error) {
	t.Helper()
	d, _ := downloadtest.New(t, rel)
	exe = filepath.Join(t.TempDir(), "toolkit")
	if werr := os.WriteFile(exe, []byte("ANTIGO"), 0o755); werr != nil {
		t.Fatal(werr)
	}
	var o, e bytes.Buffer
	deps := Deps{GOOS: "linux", EmbeddedYML: []byte(embedded), Downloader: d,
		Update: UpdateDeps{APIBase: "https://example.com/api", DownloadBase: "https://example.com/dl", Current: current,
			GOOS: "linux", GOARCH: "amd64", Exe: func() (string, error) { return exe, nil }}}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"update"}, args...))
	err = cmd.ExecuteContext(context.Background())
	return o.String(), exe, err
}

func newRel() *updRelease {
	return &updRelease{tag: "v0.2.0", notes: "Novidades:\n- ícone novo\n\x1b[2Jlimpa", binary: []byte("NOVO-0.2.0")}
}

func TestUpdateCheckNewerShowsNotesAndDownloadsNothing(t *testing.T) {
	rel := newRel()
	out, exe, err := runUpdateCmd(t, rel, "v0.1.0", "--check")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"toolkit v0.1.0 → v0.2.0 disponível.", "Notas da release:", "ícone novo", "Para atualizar: toolkit update", "https://example.com/rel"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("notas com escape vazaram: %q", out)
	}
	if rel.count("/toolkit_linux_amd64") != 0 || rel.count("/checksums.txt") != 0 {
		t.Fatal("--check não baixa binário nem checksums")
	}
	if b, _ := os.ReadFile(exe); string(b) != "ANTIGO" {
		t.Fatal("--check não troca nada")
	}
}

func TestUpdateCheckUpToDateAndDev(t *testing.T) {
	out, _, err := runUpdateCmd(t, newRel(), "v0.2.0", "--check")
	if err != nil || !strings.Contains(out, "toolkit v0.2.0 já é a última versão.") {
		t.Fatalf("%v\n%s", err, out)
	}
	out, _, err = runUpdateCmd(t, newRel(), "dev", "--check")
	if err != nil || !strings.Contains(out, "build de desenvolvimento") || !strings.Contains(out, "Última release: v0.2.0") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestUpdateReplacesTheBinaryAfterCheckingTheHash(t *testing.T) {
	rel := newRel()
	out, exe, err := runUpdateCmd(t, rel, "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NOVO-0.2.0" {
		t.Fatalf("binário: %q", b)
	}
	if !strings.Contains(out, "toolkit atualizado: v0.1.0 → v0.2.0 (SHA-256 conferido)") {
		t.Fatalf("saída:\n%s", out)
	}
	if rel.count("/checksums.txt") != 1 || rel.count("/toolkit_linux_amd64") != 1 {
		t.Fatalf("downloads: %v", rel.hits)
	}
}

func TestUpdateDryRunOnlyAsksTheAPI(t *testing.T) {
	rel := newRel()
	out, exe, err := runUpdateCmd(t, rel, "v0.1.0", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"toolkit update (dry-run): baixaria https://example.com/dl/v0.2.0/toolkit_linux_amd64 e https://example.com/dl/v0.2.0/checksums.txt", "conferiria o SHA-256 e trocaria"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
	if rel.count("/toolkit_linux_amd64") != 0 || rel.count("/checksums.txt") != 0 {
		t.Fatal("dry-run não baixa nada além da API")
	}
	if b, _ := os.ReadFile(exe); string(b) != "ANTIGO" {
		t.Fatal("dry-run não troca")
	}
}

func TestUpdateSameVersionAndNewerInstalled(t *testing.T) {
	out, _, err := runUpdateCmd(t, newRel(), "v0.2.0")
	if err != nil || !strings.Contains(out, "já está instalado: nada a fazer") {
		t.Fatalf("%v\n%s", err, out)
	}
	rel := newRel()
	out, exe, err := runUpdateCmd(t, rel, "v0.3.0")
	if err != nil || !strings.Contains(out, "mais novo que a última release") || !strings.Contains(out, "toolkit update --version v0.2.0") {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(exe); string(b) != "ANTIGO" || rel.count("/toolkit_linux_amd64") != 0 {
		t.Fatal("não faz downgrade sem --version")
	}
}

func TestUpdateDevBuildNeedsExplicitVersion(t *testing.T) {
	_, exe, err := runUpdateCmd(t, newRel(), "dev")
	if err == nil || !strings.Contains(err.Error(), "build de desenvolvimento") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "ANTIGO" {
		t.Fatal("nada muda")
	}
	out, exe, err := runUpdateCmd(t, newRel(), "dev", "--version", "v0.2.0")
	if err != nil || !strings.Contains(out, "toolkit atualizado para v0.2.0") {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NOVO-0.2.0" {
		t.Fatal("com --version o dev atualiza")
	}
}

func TestUpdateExplicitOlderVersionWarnsAndWorks(t *testing.T) {
	out, exe, err := runUpdateCmd(t, newRel(), "v0.3.0", "--version", "v0.2.0")
	if err != nil || !strings.Contains(out, "aviso: voltando de v0.3.0 para v0.2.0") {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NOVO-0.2.0" {
		t.Fatal("trocou")
	}
}

// A versão pedida entra na URL: só "vX.Y.Z[-pré]" passa, e a recusa vem ANTES de qualquer requisição.
func TestUpdateRejectsBadVersionBeforeAnyRequest(t *testing.T) {
	rel := newRel()
	for _, bad := range []string{"latest", "v1", "../../etc/passwd", "v1.0.0/../x", "v1.0.0?x=1", "v1.0.0%2f"} {
		if _, _, err := runUpdateCmd(t, rel, "v0.1.0", "--version", bad); err == nil {
			t.Errorf("--version %q deveria ser recusada", bad)
		}
	}
	if len(rel.hits) != 0 {
		t.Fatalf("nenhuma requisição para versão inválida: %v", rel.hits)
	}
}

func TestUpdateWrongHashLeavesTheBinaryAlone(t *testing.T) {
	rel := newRel()
	d, _ := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/toolkit_linux_amd64") {
			_, _ = w.Write([]byte("ADULTERADO")) // não é o que o checksums.txt promete
			return
		}
		rel.ServeHTTP(w, r)
	}))
	exe := filepath.Join(t.TempDir(), "toolkit")
	_ = os.WriteFile(exe, []byte("ANTIGO"), 0o755)
	var o, e bytes.Buffer
	deps := Deps{GOOS: "linux", EmbeddedYML: []byte(embedded), Downloader: d,
		Update: UpdateDeps{APIBase: "https://example.com/api", DownloadBase: "https://example.com/dl", Current: "v0.1.0",
			GOOS: "linux", GOARCH: "amd64", Exe: func() (string, error) { return exe, nil }}}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"update"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "ANTIGO" {
		t.Fatalf("o binário mudou: %q", b)
	}
}

func TestRootListsUpdateCommand(t *testing.T) {
	out, _, err := run(t, "--help")
	if err != nil || !strings.Contains(out, "update") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// Com a API limitada e --version explícita, segue sem as notas (o download não usa a API); sem --version, erro
// com a dica do GITHUB_TOKEN.
func TestUpdateRateLimitedAPI(t *testing.T) {
	rel := newRel()
	limited := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/") {
			http.Error(w, "rate limit", http.StatusForbidden)
			return
		}
		rel.ServeHTTP(w, r)
	})
	run2 := func(args ...string) (string, string, error) {
		d, _ := downloadtest.New(t, limited)
		exe := filepath.Join(t.TempDir(), "toolkit")
		_ = os.WriteFile(exe, []byte("ANTIGO"), 0o755)
		var o, e bytes.Buffer
		deps := Deps{GOOS: "linux", EmbeddedYML: []byte(embedded), Downloader: d,
			Update: UpdateDeps{APIBase: "https://example.com/api", DownloadBase: "https://example.com/dl", Current: "v0.1.0",
				GOOS: "linux", GOARCH: "amd64", Exe: func() (string, error) { return exe, nil }}}
		cmd := NewRootCmdWith(&o, &e, deps)
		cmd.SetArgs(append([]string{"update"}, args...))
		err := cmd.ExecuteContext(context.Background())
		b, _ := os.ReadFile(exe)
		return o.String(), string(b), err
	}
	if _, _, err := run2(); err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("sem --version: %v", err)
	}
	out, bin, err := run2("--version", "v0.2.0")
	if err != nil || bin != "NOVO-0.2.0" || !strings.Contains(out, "seguindo sem as notas") {
		t.Fatalf("com --version: %v bin=%q\n%s", err, bin, out)
	}
}
