package url

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/download/downloadtest"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/providers"
)

var body = []byte("instalador")

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type rec struct {
	cmds []exec.Cmd
	exit int
}

func (r *rec) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	r.cmds = append(r.cmds, c)
	return exec.Result{ExitCode: r.exit}, nil
}
func (*rec) LookPath(string) (string, error) { return "", errors.New("não") }

func ref(name, typ string, args ...string) providers.PackageRef {
	return providers.PackageRef{AppID: "app", Provider: "url", URL: "https://example.com/dl/" + name, SHA256: sum(body), Type: typ, Args: args}
}

func TestValidate(t *testing.T) {
	p := New(&rec{}, Options{GOOS: "windows"})
	if err := p.Validate(ref("a.msi", "", "ALLUSERS=1")); err != nil {
		t.Fatal(err)
	}
	bad := map[string]providers.PackageRef{
		"sem sha":          {AppID: "app", Provider: "url", URL: "https://example.com/a.msi"},
		"http":             {AppID: "app", Provider: "url", URL: "http://example.com/a.msi", SHA256: sum(body)},
		"ip":               {AppID: "app", Provider: "url", URL: "https://10.0.0.1/a.msi", SHA256: sum(body)},
		"campo alheio":     {AppID: "app", Provider: "url", URL: "https://example.com/a.msi", SHA256: sum(body), ID: "x"},
		"tipo de outro so": ref("a.deb", ""),
		"exe sem args":     ref("a.exe", ""),
	}
	for name, r := range bad {
		if err := p.Validate(r); !errors.Is(err, providers.ErrInvalidRef) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestContratoSemEstado(t *testing.T) {
	p := New(&rec{}, Options{GOOS: "windows"})
	if !p.Available(context.Background()) || p.Name() != "url" {
		t.Fatal("name/available")
	}
	if ok, v, err := p.IsInstalled(context.Background(), ref("a.msi", "")); ok || v != "" || err != nil {
		t.Fatalf("IsInstalled = %v %q %v", ok, v, err)
	}
	if vs, _ := p.ListVersions(context.Background(), ref("a.msi", "")); len(vs) != 1 || vs[0] != "latest" {
		t.Fatalf("%v", vs)
	}
	if err := p.SupportsVersion(ref("a.msi", ""), "1.2"); !errors.Is(err, providers.ErrVersionUnsupported) {
		t.Fatalf("%v", err)
	}
	if _, err := p.Install(context.Background(), ref("a.msi", ""), "1.2"); !errors.Is(err, providers.ErrVersionUnsupported) {
		t.Fatalf("%v", err)
	}
}

func TestInstallBaixaEInstala(t *testing.T) {
	dl, hits := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dl/a.msi" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write(body)
	}))
	r := &rec{}
	p := New(r, Options{GOOS: "windows", Downloader: dl})
	if _, err := p.Install(context.Background(), ref("a.msi", "", "ALLUSERS=1"), ""); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || len(r.cmds) != 1 || r.cmds[0].Name != "msiexec.exe" || r.cmds[0].Args[len(r.cmds[0].Args)-1] != "ALLUSERS=1" {
		t.Fatalf("hits=%d cmds=%v", hits.Load(), r.cmds)
	}
}

func TestInstallNaSimulacaoNaoBaixaNemExecuta(t *testing.T) {
	inner := &rec{}
	dry := exec.NewDryRunRunner(inner)
	p := New(dry, Options{GOOS: "windows", Downloader: downloadtest.Forbidden(t)})
	if _, err := p.Install(context.Background(), ref("a.msi", ""), ""); err != nil {
		t.Fatal(err)
	}
	plan := dry.Plan()
	if len(inner.cmds) != 0 || len(plan) != 2 || !strings.HasPrefix(plan[0].Note, "baixar https://example.com/dl/a.msi") {
		t.Fatalf("plano: %+v", plan)
	}
}

func TestInstallHashDivergente(t *testing.T) {
	dl, _ := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("adulterado")) }))
	r := &rec{}
	p := New(r, Options{GOOS: "windows", Downloader: dl})
	if _, err := p.Install(context.Background(), ref("a.msi", ""), ""); !errors.Is(err, providers.ErrHashMismatch) || len(r.cmds) != 0 {
		t.Fatalf("err=%v cmds=%v", err, r.cmds)
	}
}

// A URL com {{version}} marca VersionInID: "toolkit install app@1.2" não vai ao provider como versão (que ele
// recusaria) e o catálogo confere que o pedido é a versão que o template aplicou.
func TestVersaoNaURLPassaPeloInstallApp(t *testing.T) {
	c, err := catalog.Parse("catalog.yaml", []byte(`apps:
  app:
    name: App
    category: utilitarios
    versions: ["1.2", "1.1"]
    windows: [{ provider: url, url: "https://example.com/dl/app-{{version}}.msi", sha256: `+sum(body)+` }]
`))
	if err != nil {
		t.Fatal(err)
	}
	app, _ := c.Get("app")
	vars, err := app.VersionVars("1.2")
	if err != nil {
		t.Fatal(err)
	}
	dl, _ := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dl/app-1.2.msi" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write(body)
	}))
	r := &rec{}
	reg := providers.NewRegistry(New(r, Options{GOOS: "windows", Downloader: dl}))
	cands, skips := reg.Candidates(context.Background(), app, "windows", vars)
	if len(cands) != 1 {
		t.Fatalf("skips: %v", skips)
	}
	if !cands[0].Ref.VersionInID || cands[0].Ref.VersionApplied != "1.2" {
		t.Fatalf("ref: %+v", cands[0].Ref)
	}
	if _, err := providers.InstallApp(context.Background(), cands, "1.2"); err != nil {
		t.Fatal(err)
	}
	if len(r.cmds) != 1 {
		t.Fatalf("cmds: %v", r.cmds)
	}
	if _, err := providers.InstallApp(context.Background(), cands, "1.1"); !errors.Is(err, providers.ErrInvalidRef) {
		t.Fatalf("versão diferente da aplicada: %v", err)
	}
}
