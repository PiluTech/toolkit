package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/PiluTech/toolkit/internal/download"
	"github.com/PiluTech/toolkit/internal/download/downloadtest"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func TestParseVersionAndCompare(t *testing.T) {
	for _, bad := range []string{"", "dev", "v1", "v1.2", "1.2.3.4", "v1.2.3/../x", "v01.2.3", "v1.2.3-", "v1.2.3 ", "latest", "vx.y.z", "v1.2.3-rc 1"} {
		if _, err := ParseVersion(bad); err == nil {
			if strings.TrimSpace(bad) != bad {
				continue // espaços nas pontas são aparados: "v1.2.3 " vale v1.2.3
			}
			t.Errorf("%q deveria ser inválida", bad)
		}
	}
	order := []string{"v0.1.0-rc.1", "v0.1.0-rc.2", "v0.1.0-rc.10", "v0.1.0", "v0.1.1", "v0.2.0", "v1.0.0"}
	for i := range order {
		for j := range order {
			a, _ := ParseVersion(order[i])
			b, _ := ParseVersion(order[j])
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := Compare(a, b); got != want {
				t.Errorf("Compare(%s, %s) = %d, quero %d", order[i], order[j], got, want)
			}
		}
	}
	if v, _ := ParseVersion("0.1.0"); v.String() != "v0.1.0" {
		t.Fatalf("sem o v: %s", v)
	}
}

func TestAssetName(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}: "toolkit_linux_amd64", {"darwin", "arm64"}: "toolkit_darwin_arm64",
		{"windows", "amd64"}: "toolkit_windows_amd64.exe", {"windows", "arm64"}: "toolkit_windows_arm64.exe",
	}
	for in, want := range cases {
		if got, err := AssetName(in[0], in[1]); err != nil || got != want {
			t.Errorf("%v: %q %v", in, got, err)
		}
	}
	for _, in := range [][2]string{{"plan9", "amd64"}, {"linux", "386"}, {"", ""}} {
		if _, err := AssetName(in[0], in[1]); err == nil {
			t.Errorf("%v deveria ser recusado", in)
		}
	}
}

func TestParseChecksums(t *testing.T) {
	h1, h2 := strings.Repeat("a", 64), strings.Repeat("B", 64)
	data := []byte(h1 + "  toolkit_linux_amd64\n" + h2 + " *toolkit_windows_amd64.exe\n\nlixo\n")
	if got, err := ParseChecksums(data, "toolkit_linux_amd64"); err != nil || got != h1 {
		t.Fatalf("linux: %q %v", got, err)
	}
	if got, err := ParseChecksums(data, "toolkit_windows_amd64.exe"); err != nil || got != strings.ToLower(h2) {
		t.Fatalf("modo binário e maiúsculas: %q %v", got, err)
	}
	if _, err := ParseChecksums(data, "toolkit_darwin_arm64"); !errors.Is(err, ErrNotInChecksums) {
		t.Fatalf("ausente: %v", err)
	}
	if _, err := ParseChecksums([]byte("zz  toolkit_x\n"), "toolkit_x"); err == nil {
		t.Fatal("hash curto")
	}
	if _, err := ParseChecksums([]byte(strings.Repeat("z", 64)+"  toolkit_x\n"), "toolkit_x"); err == nil {
		t.Fatal("hash não hexadecimal")
	}
}

// fakeRelease monta o servidor de release: API + checksums.txt + binário, com ganchos para adulterar.
type fakeRelease struct {
	mu         sync.Mutex
	tag, notes string
	asset      string
	binary     []byte
	checksum   string // sobrescreve o hash do checksums.txt (vazio = o certo)
	omitAsset  bool
	hits       map[string]int
	prerelease bool
	draft      bool
}

func (f *fakeRelease) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.hits == nil {
			f.hits = map[string]int{}
		}
		f.hits[r.URL.Path]++
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/latest") || strings.Contains(r.URL.Path, "/api/tags/"):
			b, _ := json.Marshal(map[string]any{"tag_name": f.tag, "body": f.notes, "html_url": "https://example.com/r", "draft": f.draft, "prerelease": f.prerelease})
			_, _ = w.Write(b)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			if f.omitAsset {
				_, _ = fmt.Fprintf(w, "%s  outro_arquivo\n", strings.Repeat("0", 64))
				return
			}
			h := f.checksum
			if h == "" {
				h = sum(f.binary)
			}
			_, _ = fmt.Fprintf(w, "%s  %s\n", h, f.asset)
		case strings.HasSuffix(r.URL.Path, "/"+f.asset):
			_, _ = w.Write(f.binary)
		default:
			http.NotFound(w, r)
		}
	})
}

func (f *fakeRelease) count(suffix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for p, c := range f.hits {
		if strings.HasSuffix(p, suffix) {
			n += c
		}
	}
	return n
}

func setup(t *testing.T, goos string) (Config, *fakeRelease, string) {
	t.Helper()
	f := &fakeRelease{tag: "v0.2.0", notes: "Novidades\n- coisa nova", binary: []byte("BINARIO-NOVO-v0.2.0"), asset: "toolkit_linux_amd64"}
	if goos == "windows" {
		f.asset = "toolkit_windows_amd64.exe"
	}
	d, _ := downloadtest.New(t, f.handler())
	dir := t.TempDir()
	exe := filepath.Join(dir, "toolkit")
	if err := os.WriteFile(exe, []byte("BINARIO-ANTIGO-v0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Config{APIBase: "https://example.com/api", DownloadBase: "https://example.com/dl", Current: "v0.1.0", GOOS: goos, GOARCH: "amd64", Exe: exe, Downloader: d, AllowedHosts: []string{"example.com"}}, f, dir
}

func TestLatestAndPlan(t *testing.T) {
	c, f, _ := setup(t, "linux")
	f.notes = "ok\x1b]0;pwn\x07 fim"
	rel, err := Latest(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.2.0" || strings.ContainsAny(rel.Notes, "\x1b\x07") || !strings.Contains(rel.Notes, `\x1b`) {
		t.Fatalf("release: %+v", rel)
	}
	p, err := MakePlan(c, rel)
	if err != nil || !p.Newer || p.Same || p.Downgrade || p.AssetURL != "https://example.com/dl/v0.2.0/toolkit_linux_amd64" || p.ChecksumsURL != "https://example.com/dl/v0.2.0/checksums.txt" {
		t.Fatalf("plano: %+v %v", p, err)
	}
	c.Current = "v0.2.0"
	if p, _ := MakePlan(c, rel); !p.Same || p.Newer {
		t.Fatalf("mesma versão: %+v", p)
	}
	c.Current = "v0.3.0"
	if p, _ := MakePlan(c, rel); !p.Downgrade {
		t.Fatalf("versão mais nova instalada: %+v", p)
	}
	c.Current = "dev"
	if p, _ := MakePlan(c, rel); p.CurrentParsed != nil || p.Newer || p.Same {
		t.Fatalf("dev não compara: %+v", p)
	}
	f.tag = "latest/../x"
	if _, err := Latest(context.Background(), c); err == nil {
		t.Fatal("tag fora do formato é recusada")
	}
	f.tag, f.draft = "v0.2.0", true
	if _, err := Latest(context.Background(), c); err == nil {
		t.Fatal("rascunho é recusado")
	}
}

func TestReleaseByTagValidatesBeforeAnyRequest(t *testing.T) {
	c, f, _ := setup(t, "linux")
	if _, err := ReleaseByTag(context.Background(), c, "v1.0.0/../../x"); err == nil {
		t.Fatal("tag inválida")
	}
	if len(f.hits) != 0 {
		t.Fatalf("nenhuma requisição para tag inválida: %v", f.hits)
	}
	f.tag = "v0.1.0"
	if rel, err := ReleaseByTag(context.Background(), c, "0.1.0"); err != nil || rel.Tag != "v0.1.0" {
		t.Fatalf("%+v %v", rel, err)
	}
}

func applyPlan(t *testing.T, c Config) error {
	t.Helper()
	rel, err := Latest(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	p, err := MakePlan(c, rel)
	if err != nil {
		t.Fatal(err)
	}
	return Apply(context.Background(), c, p)
}

func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	ents, _ := os.ReadDir(dir)
	var out []string
	for _, e := range ents {
		if e.Name() != "toolkit" && e.Name() != "toolkit.old" {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestApplyReplacesTheBinaryAtomicallyAndKeepsTheMode(t *testing.T) {
	c, _, dir := setup(t, "linux")
	if err := applyPlan(t, c); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(c.Exe)
	if string(got) != "BINARIO-NOVO-v0.2.0" {
		t.Fatalf("conteúdo: %q", got)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(c.Exe); fi.Mode().Perm()&0o111 == 0 {
			t.Fatalf("o novo binário tem de ser executável: %v", fi.Mode())
		}
	}
	if l := leftovers(t, dir); len(l) != 0 {
		t.Fatalf("sobras no diretório: %v", l)
	}
}

func TestApplyRefusesAWrongHashAndLeavesTheBinaryUntouched(t *testing.T) {
	c, f, dir := setup(t, "linux")
	f.checksum = strings.Repeat("0", 64) // o checksums.txt não confere com o binário servido
	err := applyPlan(t, c)
	if !errors.Is(err, download.ErrHashMismatch) {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(c.Exe); string(got) != "BINARIO-ANTIGO-v0.1.0" {
		t.Fatalf("o binário mudou: %q", got)
	}
	if l := leftovers(t, dir); len(l) != 0 {
		t.Fatalf("sobras: %v", l)
	}
}

func TestApplyAssetMissingFromChecksumsDownloadsNothing(t *testing.T) {
	c, f, _ := setup(t, "linux")
	f.omitAsset = true
	if err := applyPlan(t, c); !errors.Is(err, ErrNotInChecksums) {
		t.Fatalf("err = %v", err)
	}
	if f.count("/"+f.asset) != 0 {
		t.Fatal("sem hash no checksums.txt o binário nem é baixado")
	}
}

func TestApplyRefusesSymlinkAndUnwritableDir(t *testing.T) {
	c, f, dir := setup(t, "linux")
	link := filepath.Join(dir, "toolkit-link")
	if err := os.Symlink(c.Exe, link); err != nil {
		t.Skip("sem symlink: " + err.Error())
	}
	c2 := c
	c2.Exe = link
	if err := applyPlan(t, c2); !errors.Is(err, ErrManagedInstall) {
		t.Fatalf("symlink: %v", err)
	}
	if f.count("/"+f.asset) != 0 {
		t.Fatal("recusou antes de baixar")
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()
	if err := applyPlan(t, c); !errors.Is(err, ErrNotWritable) {
		t.Fatalf("diretório sem escrita: %v", err)
	}
}

// Windows: o executável em uso vai para .old e o novo entra; se a segunda troca falha, volta atrás.
func TestWindowsSwapUsesDotOldAndRollsBack(t *testing.T) {
	c, _, dir := setup(t, "windows")
	c.Exe = filepath.Join(dir, "toolkit.exe")
	if err := os.WriteFile(c.Exe, []byte("ANTIGO"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := applyPlan(t, c); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(c.Exe); string(got) != "BINARIO-NOVO-v0.2.0" {
		t.Fatalf("novo: %q", got)
	}
	if old, _ := os.ReadFile(c.Exe + ".old"); string(old) != "ANTIGO" {
		t.Fatalf(".old: %q", old)
	}
	CleanupOld(c.Exe)
	if _, err := os.Stat(c.Exe + ".old"); !os.IsNotExist(err) {
		t.Fatal("CleanupOld apaga o .old")
	}

	// a segunda troca (staged -> exe) falha: o original volta
	if err := os.WriteFile(c.Exe, []byte("ANTIGO2"), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c.Rename = func(oldp, newp string) error {
		calls++
		if calls == 2 { // staged -> exe
			return errors.New("arquivo em uso")
		}
		return os.Rename(oldp, newp)
	}
	err := applyPlan(t, c)
	if err == nil || !strings.Contains(err.Error(), "arquivo em uso") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(c.Exe); string(got) != "ANTIGO2" {
		t.Fatalf("o original deve voltar: %q", got)
	}
	if l := leftovers(t, dir); len(l) != 0 && (len(l) != 1 || l[0] != "toolkit.exe") {
		t.Fatalf("sobras: %v", l)
	}
}

func TestApplyOversizeChecksumsOrBinaryIsRefused(t *testing.T) {
	c, f, _ := setup(t, "linux")
	f.binary = []byte(strings.Repeat("x", 10))
	d, _ := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/checksums.txt") {
			_, _ = w.Write([]byte(strings.Repeat("a", 70<<10))) // passa de 64 KiB
			return
		}
		f.handler().ServeHTTP(w, r)
	}))
	c.Downloader = d
	if err := applyPlan(t, c); !errors.Is(err, download.ErrTooLarge) {
		t.Fatalf("checksums enorme: %v", err)
	}
}
