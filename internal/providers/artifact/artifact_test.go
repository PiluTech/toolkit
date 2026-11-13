package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
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

// rec é um Runner que só registra e responde por exit code; nada chega a um processo real.
type rec struct {
	cmds   []exec.Cmd
	exit   map[string]int // por Name
	stderr string
	paths  map[string]bool
	err    error
}

func (r *rec) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	r.cmds = append(r.cmds, c)
	if r.err != nil {
		return exec.Result{}, r.err
	}
	code, ok := r.exit[c.Name]
	if !ok {
		code = r.exit[""] // padrão para comandos cujo Name é um caminho temporário
	}
	return exec.Result{ExitCode: code, Stderr: []byte(r.stderr)}, nil
}

func (r *rec) LookPath(n string) (string, error) {
	if r.paths[n] {
		return "/usr/bin/" + n, nil
	}
	return "", errors.New("não achou")
}

var body = []byte("conteudo-do-instalador")

func newEnv(t *testing.T, goos string, rn exec.Runner, handler http.HandlerFunc) (Env, *atomic.Int32) {
	t.Helper()
	dl, hits := downloadtest.New(t, handler)
	e := Env{GOOS: goos, Rn: providers.NewRunners(rn), Downloader: dl, AppsDir: func() (string, error) { return filepath.Join(t.TempDir(), "apps"), nil }}
	return e, hits
}

func serve(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }

func item(name, typ string, args ...string) Item {
	return Item{AppID: "meuapp", URL: "https://example.com/" + name, SHA256: sum(body), Type: typ, Name: name, Args: args}
}

func TestTypeOf(t *testing.T) {
	for in, want := range map[string]string{
		"a.msi": "msi", "A.EXE": "exe", "a.pkg": "pkg", "a.dmg": "dmg", "a.deb": "deb", "a.rpm": "rpm",
		"a.zip": "zip", "a.tar.gz": "tar.gz", "a.tgz": "tar.gz", "a.AppImage": "appimage", "a.bin": "", "a": "",
	} {
		if got := TypeOf(in, ""); got != want {
			t.Errorf("TypeOf(%q) = %q, quer %q", in, got, want)
		}
	}
	if TypeOf("a.bin", "msi") != "msi" {
		t.Error("type explícito deve vencer")
	}
}

func TestValidateItem(t *testing.T) {
	ok := []struct {
		goos string
		it   Item
	}{
		{"windows", item("a.msi", "msi")}, {"windows", item("a.exe", "exe", "/S")}, {"darwin", item("a.pkg", "pkg")},
		{"darwin", item("a.dmg", "dmg")}, {"linux", item("a.deb", "deb")}, {"linux", item("a.rpm", "rpm")},
		{"linux", item("a.zip", "zip")}, {"windows", item("a.tar.gz", "tar.gz")}, {"windows", item("a.msi", "", "ALLUSERS=1")},
	}
	for _, c := range ok {
		if err := ValidateItem(c.goos, c.it); err != nil {
			t.Errorf("%s %s: %v", c.goos, c.it.Name, err)
		}
	}
	bad := map[string]struct {
		goos string
		it   Item
	}{
		"msi no linux":      {"linux", item("a.msi", "msi")},
		"deb no windows":    {"windows", item("a.deb", "deb")},
		"pkg no linux":      {"linux", item("a.pkg", "pkg")},
		"exe sem args":      {"windows", item("a.exe", "exe")},
		"args em deb":       {"linux", item("a.deb", "deb", "--x")},
		"args em zip":       {"linux", item("a.zip", "zip", "--x")},
		"tipo desconhecido": {"linux", item("a.bin", "")},
		"appimage":          {"linux", item("a.AppImage", "appimage")},
		"app id inválido":   {"linux", Item{AppID: "", URL: "https://example.com/a.zip", SHA256: sum(body), Type: "zip", Name: "a.zip"}},
		"arg vazio":         {"windows", item("a.exe", "exe", "")},
		"sha ausente":       {"linux", Item{AppID: "x", URL: "https://example.com/a.zip", Type: "zip", Name: "a.zip"}},
	}
	for name, c := range bad {
		if err := ValidateItem(c.goos, c.it); !errors.Is(err, providers.ErrInvalidRef) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestMSIArgsDepoisDoArquivoEReboot(t *testing.T) {
	for exit, want := range map[int]struct {
		reboot, already bool
		err             error
	}{
		0: {}, 3010: {reboot: true}, 1641: {reboot: true}, 1638: {already: true}, 1618: {err: providers.ErrRetryable}, 1925: {err: providers.ErrNeedsAdmin}, 1603: {err: errAny},
	} {
		r := &rec{exit: map[string]int{"msiexec.exe": exit}}
		e, _ := newEnv(t, "windows", r, serve)
		res, err := e.Install(context.Background(), item("a.msi", "", "ALLUSERS=1", "INSTALLDIR=C:\\x"))
		switch {
		case want.err == nil && err != nil, want.err != nil && err == nil:
			t.Fatalf("exit %d: err = %v", exit, err)
		case want.err != nil && !errors.Is(want.err, errAny) && !errors.Is(err, want.err):
			t.Fatalf("exit %d: err = %v", exit, err)
		}
		if res.RebootRequired != want.reboot || res.AlreadyInstalled != want.already {
			t.Errorf("exit %d: %+v", exit, res)
		}
		if len(r.cmds) != 1 {
			t.Fatalf("cmds = %d", len(r.cmds))
		}
		c := r.cmds[0]
		if c.Name != "msiexec.exe" || c.Args[0] != "/i" || !strings.HasSuffix(c.Args[1], "a.msi") || !filepath.IsAbs(c.Args[1]) {
			t.Fatalf("args = %v", c.Args)
		}
		if got := strings.Join(c.Args[2:], " "); got != "/qn /norestart ALLUSERS=1 INSTALLDIR=C:\\x" {
			t.Fatalf("args depois do arquivo: %q", got)
		}
		if c.ReadOnly || !c.Elevate {
			t.Errorf("msi: ReadOnly=%v Elevate=%v", c.ReadOnly, c.Elevate)
		}
	}
}

var errAny = errors.New("qualquer")

func TestExeRodaOArquivoComArgsDepois(t *testing.T) {
	r := &rec{exit: map[string]int{}}
	e, _ := newEnv(t, "windows", r, serve)
	if _, err := e.Install(context.Background(), item("setup.exe", "", "/S", "/D=C:\\x")); err != nil {
		t.Fatal(err)
	}
	c := r.cmds[0]
	if !filepath.IsAbs(c.Name) || !strings.HasSuffix(c.Name, "setup.exe") || strings.Join(c.Args, " ") != "/S /D=C:\\x" || c.Elevate {
		t.Fatalf("%+v", c)
	}
	r = &rec{exit: map[string]int{}}
	r.exit[""] = 0
	e, _ = newEnv(t, "windows", r, serve)
	r.exit[""] = 740
	if _, err := e.Install(context.Background(), item("setup.exe", "", "/S")); !errors.Is(err, providers.ErrNeedsAdmin) {
		t.Fatalf("740: %v", err)
	}
}

func TestPkgDebRpm(t *testing.T) {
	r := &rec{exit: map[string]int{}}
	e, _ := newEnv(t, "darwin", r, serve)
	if _, err := e.Install(context.Background(), item("a.pkg", "")); err != nil {
		t.Fatal(err)
	}
	if c := r.cmds[0]; c.Name != "installer" || c.Args[0] != "-pkg" || !strings.HasSuffix(c.Args[1], "a.pkg") || strings.Join(c.Args[2:], " ") != "-target /" || !c.Elevate {
		t.Fatalf("%+v", c)
	}

	r = &rec{exit: map[string]int{}}
	e, _ = newEnv(t, "linux", r, serve)
	if _, err := e.Install(context.Background(), item("a.deb", "")); err != nil {
		t.Fatal(err)
	}
	c := r.cmds[0]
	if c.Name != "apt-get" || strings.Join(c.Args[:3], " ") != "install -y --no-remove" || !strings.HasSuffix(c.Args[3], "a.deb") || !filepath.IsAbs(c.Args[3]) || !c.Elevate {
		t.Fatalf("%+v", c)
	}
	if strings.Join(c.Env, " ") != "DEBIAN_FRONTEND=noninteractive LC_ALL=C" {
		t.Fatalf("env %v", c.Env)
	}

	r = &rec{exit: map[string]int{}, paths: map[string]bool{"dnf": true}}
	e, _ = newEnv(t, "linux", r, serve)
	if _, err := e.Install(context.Background(), item("a.rpm", "")); err != nil {
		t.Fatal(err)
	}
	if c := r.cmds[0]; c.Name != "dnf" || c.Args[0] != "install" || c.Args[1] != "-y" || !strings.HasSuffix(c.Args[2], "a.rpm") || !c.Elevate {
		t.Fatalf("dnf: %+v", c)
	}
	r = &rec{exit: map[string]int{}}
	e, _ = newEnv(t, "linux", r, serve)
	if _, err := e.Install(context.Background(), item("a.rpm", "")); err != nil {
		t.Fatal(err)
	}
	if c := r.cmds[0]; c.Name != "rpm" || c.Args[0] != "-U" || c.Args[1] != "--" || !c.Elevate {
		t.Fatalf("rpm: %+v", c)
	}
}

func TestFalhaDeElevacaoViraNeedsAdminEErroDeExitTemSnippet(t *testing.T) {
	r := &rec{err: exec.ErrNotElevated}
	e, _ := newEnv(t, "linux", r, serve)
	if _, err := e.Install(context.Background(), item("a.deb", "")); !errors.Is(err, providers.ErrNeedsAdmin) {
		t.Fatalf("%v", err)
	}
	r = &rec{exit: map[string]int{"apt-get": 100}, stderr: "E: Could not get lock /var/lib/dpkg/lock\n"}
	e, _ = newEnv(t, "linux", r, serve)
	if _, err := e.Install(context.Background(), item("a.deb", "")); !errors.Is(err, providers.ErrRetryable) {
		t.Fatalf("%v", err)
	}
	r = &rec{exit: map[string]int{"apt-get": 100}, stderr: "E: dpkg: permission denied\n"}
	e, _ = newEnv(t, "linux", r, serve)
	if _, err := e.Install(context.Background(), item("a.deb", "")); !errors.Is(err, providers.ErrNeedsAdmin) {
		t.Fatalf("%v", err)
	}
	r = &rec{exit: map[string]int{"apt-get": 100}, stderr: "E: algo deu errado\n"}
	e, _ = newEnv(t, "linux", r, serve)
	if _, err := e.Install(context.Background(), item("a.deb", "")); err == nil || !strings.Contains(err.Error(), "algo deu errado") {
		t.Fatalf("%v", err)
	}
}

func TestHashDivergenteNaoExecutaENaoDeixaTemp(t *testing.T) {
	r := &rec{exit: map[string]int{}}
	e, _ := newEnv(t, "windows", r, serve)
	var tmp string
	e.TempDir = func() (string, func(), error) {
		d, c, err := download.NewTempDir()
		tmp = d
		return d, c, err
	}
	it := item("a.msi", "")
	it.SHA256 = sum([]byte("outro"))
	_, err := e.Install(context.Background(), it)
	if !errors.Is(err, providers.ErrHashMismatch) || !errors.Is(err, download.ErrHashMismatch) {
		t.Fatalf("err = %v", err)
	}
	if len(r.cmds) != 0 {
		t.Fatalf("executou com hash divergente: %v", r.cmds)
	}
	if _, err := os.Stat(tmp); err == nil {
		t.Fatal("pasta temporária sobrou")
	}
}

func TestSimulacaoNaoBaixaNemExecutaERegistraOPlano(t *testing.T) {
	inner := &rec{exit: map[string]int{}}
	dry := exec.NewDryRunRunner(inner)
	e := Env{GOOS: "windows", Rn: providers.NewRunners(dry), Planner: dry, Downloader: downloadtest.Forbidden(t),
		AppsDir: func() (string, error) { return "/x/apps", nil }}
	res, err := e.Install(context.Background(), item("a.msi", "", "ALLUSERS=1"))
	if err != nil || res.AlreadyInstalled {
		t.Fatalf("%+v %v", res, err)
	}
	if len(inner.cmds) != 0 {
		t.Fatalf("a simulação executou: %v", inner.cmds)
	}
	plan := dry.Plan()
	if len(plan) != 2 || !strings.Contains(plan[0].Note, "baixar https://example.com/a.msi") || !strings.Contains(plan[0].Note, sum(body)) {
		t.Fatalf("plano: %+v", plan)
	}
	if plan[1].Cmd.Name != "msiexec.exe" || !strings.Contains(plan[1].Cmd.String(), "ALLUSERS=1") {
		t.Fatalf("plano: %+v", plan[1])
	}
	// Arquivo compactado: só a nota de extração.
	dry = exec.NewDryRunRunner(inner)
	e = Env{GOOS: "linux", Rn: providers.NewRunners(dry), Planner: dry, Downloader: downloadtest.Forbidden(t), AppsDir: func() (string, error) { return "/x/apps", nil }}
	if _, err := e.Install(context.Background(), item("a.zip", "")); err != nil {
		t.Fatal(err)
	}
	plan = dry.Plan()
	if len(plan) != 2 || !strings.Contains(plan[1].Note, filepath.Join("/x/apps", "meuapp")) {
		t.Fatalf("plano zip: %+v", plan)
	}
}

// dmgFixture cria uma pasta com os arquivos dados (nomes terminados em "/" são diretórios).
func dmgFixture(t *testing.T, names ...string) func(string) ([]fs.DirEntry, error) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if strings.HasSuffix(n, "/") {
			if err := os.Mkdir(filepath.Join(dir, n), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return func(string) ([]fs.DirEntry, error) { return os.ReadDir(dir) }
}

func TestDmgComAppCopiaEDesmonta(t *testing.T) {
	r := &rec{exit: map[string]int{}}
	e, _ := newEnv(t, "darwin", r, serve)
	e.ReadDir = dmgFixture(t, "MeuApp.app/", "README.txt")
	if _, err := e.Install(context.Background(), item("a.dmg", "")); err != nil {
		t.Fatal(err)
	}
	if len(r.cmds) != 3 {
		t.Fatalf("cmds: %v", r.cmds)
	}
	att, cp, det := r.cmds[0], r.cmds[1], r.cmds[2]
	if att.Name != "hdiutil" || att.Args[0] != "attach" || !strings.Contains(strings.Join(att.Args, " "), "-nobrowse") || !strings.Contains(strings.Join(att.Args, " "), "-readonly") {
		t.Fatalf("attach: %+v", att)
	}
	if cp.Name != "ditto" || len(cp.Args) != 2 || !strings.HasSuffix(cp.Args[0], "MeuApp.app") || cp.Args[1] != "/Applications/MeuApp.app" {
		t.Fatalf("ditto: %+v", cp)
	}
	if det.Name != "hdiutil" || det.Args[0] != "detach" {
		t.Fatalf("detach: %+v", det)
	}
}

func TestDmgComPkgInstalaEDesmontaMesmoComFalha(t *testing.T) {
	r := &rec{exit: map[string]int{"installer": 1}, stderr: "installer: falhou"}
	e, _ := newEnv(t, "darwin", r, serve)
	e.ReadDir = dmgFixture(t, "Setup.pkg")
	_, err := e.Install(context.Background(), item("a.dmg", ""))
	if err == nil {
		t.Fatal("esperava erro")
	}
	if len(r.cmds) != 3 || r.cmds[1].Name != "installer" || !r.cmds[1].Elevate || r.cmds[2].Args[0] != "detach" {
		t.Fatalf("cmds: %v", r.cmds)
	}
}

func TestDmgAmbiguoOuVazioEhErro(t *testing.T) {
	for name, files := range map[string][]string{
		"vazio": {"README.txt"}, "dois apps": {"A.app/", "B.app/"}, "app e pkg": {"A.app/", "B.pkg"},
	} {
		r := &rec{exit: map[string]int{}}
		e, _ := newEnv(t, "darwin", r, serve)
		e.ReadDir = dmgFixture(t, files...)
		if _, err := e.Install(context.Background(), item("a.dmg", "")); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
		if last := r.cmds[len(r.cmds)-1]; last.Args[0] != "detach" {
			t.Errorf("%s: o volume ficou montado: %v", name, r.cmds)
		}
	}
}

func TestDmgSimulacaoRegistraPlano(t *testing.T) {
	inner := &rec{exit: map[string]int{}}
	dry := exec.NewDryRunRunner(inner)
	e := Env{GOOS: "darwin", Rn: providers.NewRunners(dry), Planner: dry, Downloader: downloadtest.Forbidden(t)}
	if _, err := e.Install(context.Background(), item("a.dmg", "")); err != nil {
		t.Fatal(err)
	}
	plan := dry.Plan()
	if len(inner.cmds) != 0 || len(plan) != 4 || plan[1].Cmd.Args[0] != "attach" || !strings.Contains(plan[2].Note, "/Applications") || plan[3].Cmd.Args[0] != "detach" {
		t.Fatalf("plano: %+v", plan)
	}
}

func TestArquivoCompactadoExtraiParaOAppsDirSubstituindo(t *testing.T) {
	zipBytes := makeZip(t, map[string]string{"bin/tool": "x", "LEIAME": "y"})
	r := &rec{exit: map[string]int{}}
	dl, _ := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(zipBytes) }))
	apps := filepath.Join(t.TempDir(), "apps")
	e := Env{GOOS: "linux", Rn: providers.NewRunners(r), Downloader: dl, AppsDir: func() (string, error) { return apps, nil }}
	it := item("tool.zip", "")
	it.SHA256 = sum(zipBytes)
	if err := os.MkdirAll(filepath.Join(apps, "meuapp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apps, "meuapp", "velho"), []byte("v"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Install(context.Background(), it); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(apps, "meuapp", "bin", "tool")); string(got) != "x" {
		t.Fatalf("não extraiu: %q", got)
	}
	if _, err := os.Stat(filepath.Join(apps, "meuapp", "velho")); err == nil {
		t.Fatal("a versão anterior não foi substituída")
	}
	entries, _ := os.ReadDir(apps)
	if len(entries) != 1 {
		t.Fatalf("sobrou staging: %v", entries)
	}
	if len(r.cmds) != 0 {
		t.Fatal("extrair não usa o Runner")
	}
}

func TestArquivoMalicosoNaoSubstituiOQueJaEstava(t *testing.T) {
	bad := makeZip(t, map[string]string{"../fora": "x"})
	dl, _ := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(bad) }))
	apps := filepath.Join(t.TempDir(), "apps")
	if err := os.MkdirAll(filepath.Join(apps, "meuapp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apps, "meuapp", "velho"), []byte("v"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := Env{GOOS: "linux", Rn: providers.NewRunners(&rec{}), Downloader: dl, AppsDir: func() (string, error) { return apps, nil }}
	it := item("tool.zip", "")
	it.SHA256 = sum(bad)
	if _, err := e.Install(context.Background(), it); err == nil {
		t.Fatal("esperava erro")
	}
	if _, err := os.Stat(filepath.Join(apps, "meuapp", "velho")); err != nil {
		t.Fatal("o app anterior foi apagado por uma extração que falhou")
	}
	if entries, _ := os.ReadDir(apps); len(entries) != 1 {
		t.Fatalf("sobrou staging: %v", entries)
	}
}

func TestDefaultAppsDir(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		goos string
		env  map[string]string
		home string
		want string
	}{
		{"linux", nil, "/home/u", "/home/u/.local/share/toolkit/apps"},
		{"linux", map[string]string{"XDG_DATA_HOME": "/data"}, "/home/u", "/data/toolkit/apps"},
		{"linux", map[string]string{"XDG_DATA_HOME": "relativo"}, "/home/u", "/home/u/.local/share/toolkit/apps"},
		{"darwin", nil, "/Users/u", "/Users/u/Library/Application Support/toolkit/apps"},
	}
	for _, c := range cases {
		if runtime.GOOS == "windows" {
			continue // os caminhos do Linux e do macOS (/home/u) não são absolutos no Windows
		}
		got, err := DefaultAppsDir(c.goos, env(c.env), c.home)
		if err != nil || filepath.ToSlash(got) != c.want {
			t.Errorf("%s: %q %v", c.goos, got, err)
		}
	}
	if _, err := DefaultAppsDir("linux", env(nil), ""); err == nil {
		t.Error("sem home deveria falhar")
	}
}
