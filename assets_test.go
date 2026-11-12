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

func TestCatalogoEmbutidoTemAsSeteEntradasEUrlSempreTemSha256(t *testing.T) {
	cat, err := catalog.Parse("catalog.yaml", toolkit.CatalogYAML)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"git", "vscode", "python", "discord", "firefox", "vlc", "7zip"}
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
