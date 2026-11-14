package toolkit_test

import (
	"reflect"
	"strings"
	"testing"

	toolkit "github.com/PiluTech/toolkit"
	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/recipes"
)

func TestCatalogoEmbutidoEValido(t *testing.T) {
	cat, err := catalog.Parse("catalog.yaml", toolkit.CatalogYAML)
	if err != nil {
		t.Fatalf("o catálogo embutido deve validar:\n%v", err)
	}
	if len(cat.Apps) == 0 {
		t.Fatal("catálogo embutido vazio")
	}
	if _, ok := cat.Get("python"); !ok {
		t.Fatal("python deveria estar no catálogo")
	}
}

func TestCatalogoEmbutidoTemAsEntradasEsperadasEUrlSempreTemSha256(t *testing.T) {
	cat, err := catalog.Parse("catalog.yaml", toolkit.CatalogYAML)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"git", "vscode", "python", "discord", "firefox", "vlc", "7zip", "brave", "chrome", "libreoffice", "peazip", "rustdesk"}
	if !reflect.DeepEqual(cat.Order, want) {
		t.Fatalf("entradas do catálogo embutido: %v, quer %v", cat.Order, want)
	}
	for _, app := range cat.List() {
		for _, list := range app.Installers {
			for _, in := range list {
				if in.Provider == catalog.ProviderURL && in.SHA256 == "" {
					t.Errorf("%s: provider url sem sha256", app.ID)
				}
				if in.URL != "" {
					if err := catalog.CheckURL(in.URL, true); err != nil {
						t.Errorf("%s: %v", app.ID, err)
					}
				}
			}
		}
	}
}

// O bloco detect do catálogo embutido só tem ids conhecidos (Discord, Firefox, VLC, VS Code, 7-Zip, Git no Windows).
func TestCatalogoEmbutidoTemDeteccaoDosAppsConhecidos(t *testing.T) {
	cat, err := catalog.Parse("catalog.yaml", toolkit.CatalogYAML)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := cat.Get("discord")
	m, ok := d.DetectFor("darwin")
	if !ok || m.AppBundle != "Discord.app" || m.BundleID != "com.hnc.Discord" {
		t.Fatalf("discord macOS: %+v", m)
	}
	if w, _ := d.DetectFor("windows"); w.DisplayName != "Discord" {
		t.Fatalf("discord windows: %+v", w)
	}
	for _, id := range []string{"vscode", "firefox", "vlc"} {
		a, _ := cat.Get(id)
		for _, goos := range []string{"darwin", "windows", "linux"} {
			if _, ok := a.DetectFor(goos); !ok {
				t.Errorf("%s sem detect para %s", id, goos)
			}
		}
	}
	// Sem certeza, sem bloco: python não tem; git e 7zip só no Windows.
	if py, _ := cat.Get("python"); len(py.Detect) != 0 {
		t.Errorf("python não deveria ter detect: %+v", py.Detect)
	}
	for _, id := range []string{"git", "7zip"} {
		a, _ := cat.Get(id)
		if _, ok := a.DetectFor("windows"); !ok {
			t.Errorf("%s sem detect no Windows", id)
		}
		if _, ok := a.DetectFor("darwin"); ok {
			t.Errorf("%s é linha de comando no macOS: sem detect", id)
		}
	}
}

func TestReceitasEmbutidasValidamEUsamAppsDoCatalogo(t *testing.T) {
	set, err := recipes.LoadFS(toolkit.Recipes, "recipes")
	if err != nil {
		t.Fatalf("as receitas embutidas devem validar:\n%v", err)
	}
	cat, err := catalog.Parse("catalog.yaml", toolkit.CatalogYAML)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := set.Get(platform.Windows, "wsl-dev"); !ok {
		t.Fatal("wsl-dev deveria estar embutida")
	}
	for _, r := range set.All() {
		for _, st := range r.Steps {
			for _, ref := range st.Install {
				id, _, _ := strings.Cut(ref, "@")
				if _, ok := cat.Get(id); !ok {
					t.Errorf("%s/%s: app %q não existe no catálogo", r.OS, r.ID, id)
				}
			}
		}
	}
}

// PR13 (decisão 3 do plano): o RustDesk entra com aviso de segurança, só com os ids VERIFICADOS (cask rustdesk no
// macOS, flatpak com.rustdesk.RustDesk verificado na Flathub) e SEM instalador no Windows (o id do winget não
// existe no repositório winget-pkgs em 2026-11-14). Nunca url sem hash.
func TestCatalogoEmbutidoRustDeskComAvisoEIdsVerificados(t *testing.T) {
	cat, err := catalog.Parse("catalog.yaml", toolkit.CatalogYAML)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := cat.Get("rustdesk")
	if !ok {
		t.Fatal("rustdesk deveria estar no catálogo")
	}
	if a.Category != "utilitarios" || a.Name != "RustDesk" {
		t.Fatalf("rustdesk: %+v", a)
	}
	if len(a.InstallersFor("windows")) != 0 {
		t.Errorf("Windows: sem instalador até o id do winget ser verificado: %+v", a.InstallersFor("windows"))
	}
	mac := a.InstallersFor("darwin")
	if len(mac) != 1 || mac[0].Provider != catalog.ProviderBrew || mac[0].Cask != "rustdesk" {
		t.Errorf("macOS: %+v", mac)
	}
	lin := a.InstallersFor("linux")
	if len(lin) != 1 || lin[0].Provider != catalog.ProviderFlatpak || lin[0].ID != "com.rustdesk.RustDesk" {
		t.Errorf("Linux: %+v", lin)
	}
	for _, in := range append(mac, lin...) {
		if in.Provider == catalog.ProviderURL || in.Provider == catalog.ProviderGitHubRelease {
			t.Errorf("sem url/github-release para o RustDesk: %+v", in)
		}
	}
	// O aviso é o do plano e diz a versão mínima, a data e a fonte, sem afirmar que a instalada cumpre.
	for _, quer := range []string{"controle", "senha forte", "temporária", "acesso permanente", "servidor próprio", "1.5.0", "2026-11-14", "NVD", "não confere"} {
		if !strings.Contains(a.Note, quer) {
			t.Errorf("aviso sem %q: %s", quer, a.Note)
		}
	}
}
