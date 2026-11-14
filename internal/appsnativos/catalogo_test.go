package appsnativos

import (
	"errors"
	"strings"
	"testing"
)

func TestCatalogoNaoCasaComDenylist(t *testing.T) {
	for _, a := range Apps() {
		if Protected(a.Name) {
			t.Errorf("%s (%s) está no catálogo mas casa com a lista de negação", a.ID, a.Name)
		}
		if !ValidName(a.Name) {
			t.Errorf("%s: nome %q fora da regex fechada", a.ID, a.Name)
		}
	}
}

func TestCatalogoFechadoEEstavel(t *testing.T) {
	apps := Apps()
	if len(apps) < 12 || len(apps) > 16 {
		t.Fatalf("catálogo com %d apps, esperava de 12 a 16", len(apps))
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, a := range apps {
		if ids[a.ID] || names[a.Name] {
			t.Errorf("repetido: %s / %s", a.ID, a.Name)
		}
		ids[a.ID], names[a.Name] = true, true
		if a.ID != strings.ToLower(a.ID) || strings.ContainsAny(a.ID, " _.") {
			t.Errorf("id %q deveria ser minúsculo, sem espaço nem ponto", a.ID)
		}
		for _, l := range []Lang{0, 1, 2} {
			if a.Title(l) == "" {
				t.Errorf("%s sem título no idioma %d", a.ID, l)
			}
		}
		if a.Categoria == "" {
			t.Errorf("%s sem categoria", a.ID)
		}
		if a.StoreID != "" && !strings.HasPrefix(a.StoreID, "9") && !strings.HasPrefix(a.StoreID, "X") {
			t.Errorf("%s: StoreID %q com formato estranho", a.ID, a.StoreID)
		}
	}
}

func TestDenylistProtegeOCritico(t *testing.T) {
	for _, n := range []string{
		"Microsoft.WindowsStore", "Microsoft.StorePurchaseApp", "Microsoft.DesktopAppInstaller", "Microsoft.WebView2Runtime",
		"Microsoft.MicrosoftEdge.Stable", "Microsoft.MicrosoftEdgeDevToolsClient", "Microsoft.Windows.ShellExperienceHost",
		"Microsoft.Windows.StartMenuExperienceHost", "Windows.ImmersiveControlPanel", "windows.immersivecontrolpanel",
		"Microsoft.Windows.Search", "Microsoft.Windows.Cortana", "Microsoft.SecHealthUI", "Microsoft.NET.Native.Runtime.2.2",
		"Microsoft.VCLibs.140.00", "Microsoft.UI.Xaml.2.8", "Microsoft.WindowsTerminal", "Microsoft.Windows.Photos",
		"Microsoft.WindowsCalculator", "Microsoft.WindowsNotepad", "Microsoft.XboxIdentityProvider", "Microsoft.GamingServices",
		"Microsoft.XboxGamingOverlay", "Microsoft.HEIFImageExtension", "Microsoft.HEVCVideoExtension", "Microsoft.VP9VideoExtensions",
		"Microsoft.WebpImageExtension", "MicrosoftWindows.Client.CBS", "Microsoft.WindowsAppRuntime.1.5",
	} {
		if !Protected(n) {
			t.Errorf("%s deveria ser protegido", n)
		}
	}
	for _, n := range []string{"Microsoft.BingWeather", "Microsoft.WindowsMaps", "Microsoft.WindowsFeedbackHub", "Clipchamp.Clipchamp"} {
		if Protected(n) {
			t.Errorf("%s não deveria ser protegido (é do catálogo)", n)
		}
	}
}

func TestRegexDeNomes(t *testing.T) {
	good := []string{"Microsoft.BingWeather", "A", "9abc.def", "MicrosoftTeams"}
	bad := []string{"", ".abc", "-abc", "a b", "a;b", "a'b", "a\"b", "a$b", "a`b", "a|b", "a\nb", "a_b", "a-b", "é", strings.Repeat("a", 102), "a\x00"}
	for _, s := range good {
		if !ValidName(s) {
			t.Errorf("%q deveria passar", s)
		}
	}
	for _, s := range bad {
		if ValidName(s) {
			t.Errorf("%q deveria ser recusado", s)
		}
	}
	if !ValidName(strings.Repeat("a", 101)) {
		t.Error("101 caracteres é o limite e deveria passar")
	}
}

func TestPerfis(t *testing.T) {
	basico := map[string]bool{}
	for _, a := range ParaPerfil(PerfilBasico) {
		basico[a.Name] = true
	}
	for _, n := range []string{"Microsoft.BingWeather", "Microsoft.BingNews", "Microsoft.GetHelp", "Microsoft.Getstarted",
		"Microsoft.MicrosoftSolitaireCollection", "Microsoft.MicrosoftOfficeHub", "Microsoft.WindowsFeedbackHub", "Microsoft.SkypeApp",
		"Microsoft.MixedReality.Portal", "Microsoft.Microsoft3DViewer"} {
		if !basico[n] {
			t.Errorf("%s deveria estar no básico", n)
		}
	}
	if len(basico) != 10 {
		t.Errorf("básico com %d apps, esperava 10", len(basico))
	}
	if len(ParaPerfil(PerfilCompleto)) != len(Apps()) {
		t.Error("o completo deve ter todo o catálogo")
	}
	if ParaPerfil("x") != nil {
		t.Error("perfil desconhecido não tem apps")
	}
	if _, err := ParsePerfil("Completo "); err != nil {
		t.Errorf("ParsePerfil: %v", err)
	}
	if _, err := ParsePerfil("tudo"); err == nil {
		t.Error("perfil inválido deveria falhar")
	}
}

func TestSelecionar(t *testing.T) {
	got, err := Selecionar(PerfilBasico, []string{"maps", "bing-weather"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 11 {
		t.Errorf("básico + maps (bing-weather repetido) = 11, veio %d", len(got))
	}
	got, err = Selecionar("", []string{"todo", "clipchamp"})
	if err != nil || len(got) != 2 || got[0].ID != "clipchamp" {
		t.Errorf("ordem do catálogo, sem repetição: %v %v", got, err)
	}
	if _, err := Selecionar("", nil); !errors.Is(err, ErrNadaSelecionado) {
		t.Errorf("nada selecionado: %v", err)
	}
	if _, err := Selecionar("", []string{"windows-store"}); err == nil || !strings.Contains(err.Error(), "fora do catálogo") {
		t.Errorf("app desconhecido: %v", err)
	}
	if _, err := Selecionar("", []string{"a\x1b[2Jb"}); err == nil || strings.Contains(err.Error(), "\x1b") {
		t.Errorf("a mensagem não pode repassar ESC: %v", err)
	}
	if _, err := Selecionar("zzz", nil); !errors.Is(err, ErrPerfilDesconhecido) {
		t.Errorf("perfil: %v", err)
	}
}
