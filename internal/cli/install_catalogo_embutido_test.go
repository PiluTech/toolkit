package cli

import (
	"context"
	"strings"
	"sync"
	"testing"

	toolkit "github.com/PiluTech/toolkit"
	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/providers"
)

// leitorFalso responde "não instalado" a qualquer leitura e registra tudo. Mutação nunca deve chegar aqui
// (o --dry-run só a registra): se chegar, o teste falha. Todos os gerenciadores existem no PATH falso.
type leitorFalso struct {
	mu    sync.Mutex
	calls []exec.Cmd
}

func (l *leitorFalso) LookPath(name string) (string, error) { return "/fake/" + name, nil }

func (l *leitorFalso) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	l.mu.Lock()
	l.calls = append(l.calls, c)
	l.mu.Unlock()
	switch c.Name {
	case "winget":
		return exec.Result{ExitCode: -1978335212}, nil // 0x8A150014: não instalado
	case "flatpak":
		return exec.Result{}, nil // lista vazia
	default:
		return exec.Result{ExitCode: 1}, nil // brew/apt: não instalado
	}
}

// Contrato do catálogo embutido: cada app novo tem plano por SO só com leitura, com o provider esperado.
func TestInstallDryRunCatalogoEmbutidoNovosApps(t *testing.T) {
	casos := []struct {
		app, goos, quer string // quer: trecho do plano ("" = app sem instalador no SO)
	}{
		{"brave", "windows", "winget install --id Brave.Brave"},
		{"brave", "darwin", "brew install --cask brave-browser"},
		{"brave", "linux", "flatpak install --system -y --noninteractive flathub com.brave.Browser"},
		{"chrome", "windows", "winget install --id Google.Chrome"},
		{"libreoffice", "windows", "winget install --id TheDocumentFoundation.LibreOffice "},
		{"libreoffice", "darwin", "brew install --cask libreoffice"},
		{"libreoffice", "linux", "flatpak install --system -y --noninteractive flathub org.libreoffice.LibreOffice"},
		{"peazip", "windows", "winget install --id Giorgiotani.Peazip"},
		{"peazip", "linux", "flatpak install --system -y --noninteractive flathub io.github.peazip.PeaZip"},
	}
	for _, c := range casos {
		t.Run(c.app+"/"+c.goos, func(t *testing.T) {
			f := &leitorFalso{}
			var o, e strings.Builder
			deps := Deps{GOOS: c.goos, EmbeddedYML: toolkit.CatalogYAML, NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false }}
			cmd := NewRootCmdWith(&o, &e, deps)
			cmd.SetArgs([]string{"install", c.app, "--dry-run"})
			if err := cmd.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("%v\n%s%s", err, o.String(), e.String())
			}
			if !strings.Contains(o.String(), c.quer) {
				t.Errorf("plano sem %q:\n%s", c.quer, o.String())
			}
			for _, call := range f.calls {
				if !call.ReadOnly || call.Elevate {
					t.Errorf("o dry-run deixou passar uma mutação: %s", call.String())
				}
			}
		})
	}
}

// App sem instalador no SO (PeaZip no macOS: cask não confirmado) falha no plano em vez de inventar id.
func TestInstallPeaZipSemInstaladorNoMacOS(t *testing.T) {
	f := &leitorFalso{}
	var o, e strings.Builder
	deps := Deps{GOOS: "darwin", EmbeddedYML: toolkit.CatalogYAML, NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false }}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"install", "peazip", "--dry-run"})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatalf("peazip no macOS não tem instalador confirmado, esperava erro:\n%s", o.String())
	}
}

// Lupa #78: o Chrome entra SÓ no Windows (winget, hash conferido). No macOS o cask oficial google-chrome declara
// sha256 :no_check (não há hash a conferir: viola a regra do projeto) e no Linux o com.google.Chrome do Flathub é
// comunitário, não verificado pelo Google: nos dois o Chrome fica SEM instalador (o plano falha, nunca inventa).
func TestInstallChromeSemInstaladorNoMacOSENoLinux(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		f := &leitorFalso{}
		var o, e strings.Builder
		deps := Deps{GOOS: goos, EmbeddedYML: toolkit.CatalogYAML, NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false }}
		cmd := NewRootCmdWith(&o, &e, deps)
		cmd.SetArgs([]string{"install", "chrome", "--dry-run"})
		if err := cmd.ExecuteContext(context.Background()); err == nil {
			t.Errorf("%s: o Chrome não pode ter instalador fora do Windows, esperava erro:\n%s", goos, o.String())
		}
		if strings.Contains(o.String(), "google-chrome") || strings.Contains(o.String(), "com.google.Chrome") {
			t.Errorf("%s: o plano não pode citar cask/flatpak do Chrome:\n%s", goos, o.String())
		}
	}
}

// Todo instalador de todo app embutido passa no Validate do próprio provider, sem args e sem url sem hash.
func TestCatalogoEmbutidoInstaladoresValidamNosProviders(t *testing.T) {
	cat, err := catalog.Parse("catalog.yaml", toolkit.CatalogYAML)
	if err != nil {
		t.Fatal(err)
	}
	reg := map[string]providers.Provider{}
	deps := Deps{GOOS: "linux"}
	for _, p := range deps.providerList(&leitorFalso{}) {
		reg[p.Name()] = p
	}
	for _, app := range cat.List() {
		for goos, list := range app.Installers {
			for _, in := range list {
				p, ok := reg[in.Provider]
				if !ok {
					t.Errorf("%s/%s: provider %q sem implementação", app.ID, goos, in.Provider)
					continue
				}
				if len(in.Args) != 0 && in.Provider != catalog.ProviderURL && in.Provider != catalog.ProviderGitHubRelease {
					t.Errorf("%s/%s: args só existem para url/github-release", app.ID, goos)
				}
				if in.Provider == catalog.ProviderURL && in.SHA256 == "" {
					t.Errorf("%s/%s: url sem sha256", app.ID, goos)
				}
				if in.Provider == catalog.ProviderURL || in.Provider == catalog.ProviderGitHubRelease {
					continue
				}
				vars, err := app.VersionVars("")
				if err != nil {
					t.Errorf("%s/%s: %v", app.ID, goos, err)
					continue
				}
				ref, err := providers.RefFrom(app, in, vars)
				if err != nil {
					t.Errorf("%s/%s: %v", app.ID, goos, err)
					continue
				}
				if err := p.Validate(ref); err != nil {
					t.Errorf("%s/%s (%s): %v", app.ID, goos, in.Provider, err)
				}
			}
		}
	}
}
