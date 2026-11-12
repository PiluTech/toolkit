package update

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/download/downloadtest"
)

func TestHostAllowed(t *testing.T) {
	cases := map[string]bool{
		"github.com": true, "api.github.com": true, "GitHub.COM": true, "github.com.": true,
		"objects.githubusercontent.com": true, "release-assets.githubusercontent.com": true,
		"githubusercontent.com": false, "evil-github.com": false, "github.com.evil.example": false,
		"githubusercontent.com.evil.example": false, "xgithub.com": false, "evil.example": false, "": false,
	}
	for host, want := range cases {
		if got := hostAllowed(host, DefaultAllowedHosts); got != want {
			t.Errorf("hostAllowed(%q) = %v, quero %v", host, got, want)
		}
	}
}

// Crivo no #42: o host é preso também no REDIRECIONAMENTO: o asset que redireciona para outro host é recusado.
func TestRedirectToAnotherHostIsRefused(t *testing.T) {
	c, f, _ := setup(t, "linux")
	d, _ := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/"+f.asset) {
			http.Redirect(w, r, "https://evil.example/binario", http.StatusFound)
			return
		}
		f.handler().ServeHTTP(w, r)
	}))
	c.Downloader = d
	err := applyPlan(t, c)
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(c.Exe); string(got) != "BINARIO-ANTIGO-v0.1.0" {
		t.Fatal("o binário não pode mudar")
	}
}

// Redirecionamento para host PERMITIDO (o *.githubusercontent.com dos assets reais) funciona.
func TestRedirectToAnAllowedHostWorks(t *testing.T) {
	c, f, _ := setup(t, "linux")
	c.AllowedHosts = []string{"example.com", "*.githubusercontent.com"}
	d, _ := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/"+f.asset) && r.Host != "objects.githubusercontent.com" {
			http.Redirect(w, r, "https://objects.githubusercontent.com/asset", http.StatusFound)
			return
		}
		if r.URL.Path == "/asset" {
			_, _ = w.Write(f.binary)
			return
		}
		f.handler().ServeHTTP(w, r)
	}))
	c.Downloader = d
	if err := applyPlan(t, c); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(c.Exe); string(got) != "BINARIO-NOVO-v0.2.0" {
		t.Fatalf("binário: %q", got)
	}
}

func TestURLOutsideTheAllowlistIsRefusedBeforeAnyRequest(t *testing.T) {
	c, f, _ := setup(t, "linux")
	c.AllowedHosts = nil // padrão: só GitHub; as URLs de teste são example.com
	if _, err := Latest(context.Background(), c); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("err = %v", err)
	}
	if len(f.hits) != 0 {
		t.Fatalf("nenhuma requisição para host fora da lista: %v", f.hits)
	}
}

func TestCheckInstallRefusesPackageManagers(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		filepath.Join(dir, "Cellar", "toolkit", "0.1.0", "bin", "toolkit"),
		filepath.Join(dir, "nix", "store", "abc-toolkit", "bin", "toolkit"),
		filepath.Join(dir, ".linuxbrew", "bin", "toolkit"),
		"/opt/homebrew/Homebrew/bin/toolkit",
	} {
		err := CheckInstall("", p)
		if !errors.Is(err, ErrManagedInstall) {
			t.Errorf("%s: err = %v", p, err)
		}
	}
	if err := CheckInstall("", "/home/ana/.local/bin/toolkit"); err != nil {
		t.Fatalf("instalação do bootstrap: %v", err)
	}
	if err := CheckInstall("/usr/local/Cellar/toolkit/bin/toolkit", "/home/x/toolkit"); !errors.Is(err, ErrManagedInstall) || !strings.Contains(err.Error(), "brew upgrade toolkit") {
		t.Fatalf("o caminho usado conta e a dica cita o gerenciador: %v", err)
	}
}

// Linux: os.Executable() devolve o caminho resolvido, então o link só aparece no caminho USADO para chamar.
func TestCheckInstallRefusesSymlinkPointingOutsideItsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink")
	}
	binDir, realDir := t.TempDir(), t.TempDir()
	realBin := filepath.Join(realDir, "toolkit")
	if err := os.WriteFile(realBin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "toolkit")
	if err := os.Symlink(realBin, link); err != nil {
		t.Skip(err)
	}
	if err := CheckInstall(link, realBin); !errors.Is(err, ErrManagedInstall) {
		t.Fatalf("link para fora do diretório: %v", err)
	}
	// link para um arquivo do MESMO diretório (toolkit -> toolkit_v1) é uma instalação nossa
	sameReal := filepath.Join(binDir, "toolkit_v1")
	if err := os.WriteFile(sameReal, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	link2 := filepath.Join(binDir, "toolkit2")
	if err := os.Symlink(sameReal, link2); err != nil {
		t.Skip(err)
	}
	if err := CheckInstall(link2, sameReal); err != nil {
		t.Fatalf("link no mesmo diretório: %v", err)
	}
	// arquivo comum e caminho inexistente passam
	if err := CheckInstall(realBin, realBin); err != nil {
		t.Fatal(err)
	}
	if err := CheckInstall(filepath.Join(binDir, "nao-existe"), realBin); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRefusesInvokedSymlinkBeforeDownloading(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink")
	}
	c, f, _ := setup(t, "linux")
	other := t.TempDir()
	link := filepath.Join(other, "toolkit")
	if err := os.Symlink(c.Exe, link); err != nil {
		t.Skip(err)
	}
	c.Invoked = link // o Exe (resolvido, como no Linux) é um arquivo comum; o caminho USADO é o link
	if err := applyPlan(t, c); !errors.Is(err, ErrManagedInstall) {
		t.Fatalf("err = %v", err)
	}
	if f.count("/"+f.asset) != 0 || f.count("/checksums.txt") != 0 {
		t.Fatal("recusa antes de baixar qualquer coisa")
	}
	if got, _ := os.ReadFile(c.Exe); string(got) != "BINARIO-ANTIGO-v0.1.0" {
		t.Fatal("o binário não muda")
	}
}

// Menor (c): o hash do arquivo preparado é relido IMEDIATAMENTE antes da troca (diretório com escrita
// compartilhada): arquivo trocado nessa janela nunca vira o binário.
func TestStagedFileIsRehashedRightBeforeTheSwap(t *testing.T) {
	c, _, dir := setup(t, "linux")
	c.beforeSwap = func(staged string) {
		if err := os.WriteFile(staged, []byte("MALICIOSO"), 0o755); err != nil {
			t.Error(err)
		}
	}
	err := applyPlan(t, c)
	if !errors.Is(err, ErrStagedMismatch) {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(c.Exe); string(got) != "BINARIO-ANTIGO-v0.1.0" {
		t.Fatalf("o binário mudou: %q", got)
	}
	if l := leftovers(t, dir); len(l) != 0 {
		t.Fatalf("o arquivo preparado deve ser removido: %v", l)
	}
}

// Menor (b): Windows, rename duplo. Se o processo morrer no meio só o .old resta; o próximo comando restaura.
func TestCleanupOldRestoresWhenTheBinaryIsMissing(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "toolkit.exe")
	_ = os.WriteFile(exe+".old", []byte("ORIGINAL"), 0o755)
	CleanupOld(exe) // exe ausente + .old presente: restaura
	if got, err := os.ReadFile(exe); err != nil || string(got) != "ORIGINAL" {
		t.Fatalf("restauração: %q %v", got, err)
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal("o .old foi consumido")
	}
	_ = os.WriteFile(exe+".old", []byte("SOBRA"), 0o755)
	CleanupOld(exe) // exe presente: o .old é só sobra
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal("a sobra deve ser apagada")
	}
	if got, _ := os.ReadFile(exe); string(got) != "ORIGINAL" {
		t.Fatal("o binário não muda")
	}
	CleanupOld(filepath.Join(dir, "nada.exe")) // nada a fazer
	CleanupOld("")
}
