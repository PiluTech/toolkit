// Package appsnativos remove apps AppX/MSIX nativos do Windows (Clima, Notícias, Paciência, Dicas...) de uma lista
// FECHADA de apps opcionais que não quebram o sistema, MOSTRANDO antes o que sai, guardando um backup da identidade de
// cada pacote removido e explicando (sem executar) como reinstalar. Não há desfazer automático: o pacote some do disco.
//
// Nunca se remove Microsoft Store, App Installer (winget), WebView2/Edge, componentes do shell, Configurações, Defender,
// runtimes (.NET, VCLibs, UI.Xaml), codecs, Xbox/jogos, nem qualquer pacote marcado como framework, não removível ou com
// assinatura de sistema. Todo acesso ao Windows passa pela interface System; os testes usam uma implementação em memória.
package appsnativos

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Lang é o idioma da saída (o mesmo do diagfmt).
type Lang = diagfmt.Lang

// Perfil é um conjunto pronto de apps.
type Perfil string

// Perfis. O básico é conservador; o completo é o básico mais o resto do catálogo.
const (
	PerfilBasico   Perfil = "basico"
	PerfilCompleto Perfil = "completo"
)

// Categoria agrupa os apps do catálogo.
type Categoria string

// Categorias do catálogo.
const (
	CatInformacao    Categoria = "informacao"
	CatSuporte       Categoria = "suporte"
	CatJogos         Categoria = "jogos"
	CatProdutividade Categoria = "produtividade"
	CatComunicacao   Categoria = "comunicacao"
	CatMidia         Categoria = "midia"
	CatRealidade     Categoria = "realidade-mista"
)

// App é uma entrada do catálogo fechado.
type App struct {
	// ID é o nome curto usado em --app.
	ID string
	// Name é o campo Name do Get-AppxPackage, EXATO (maiúsculas e minúsculas).
	Name      string
	Categoria Categoria
	// Basico marca os apps do perfil básico.
	Basico bool
	// StoreID é o id do produto na Microsoft Store (ex.: "9XXXXXXXXXXX"); vazio quando não se tem certeza. Só com ele a
	// instrução de reinstalação cita o winget; sem ele manda procurar pelo título na Store.
	StoreID string
	Title   func(Lang) string
}

func tr(pt, en, es string) func(Lang) string {
	return func(l Lang) string { return diagfmt.T(l, pt, en, es) }
}

// Erros de seleção.
var (
	ErrAppDesconhecido    = errors.New("app fora do catálogo de apps nativos")
	ErrPerfilDesconhecido = errors.New("perfil desconhecido (use basico ou completo)")
	ErrNadaSelecionado    = errors.New("nada selecionado: informe --perfil ou --app")
)

var catalogo = []App{
	{ID: "bing-weather", Name: "Microsoft.BingWeather", Categoria: CatInformacao, Basico: true, Title: tr("MSN Clima", "MSN Weather", "MSN El Tiempo")},
	{ID: "bing-news", Name: "Microsoft.BingNews", Categoria: CatInformacao, Basico: true, Title: tr("MSN Notícias", "MSN News", "MSN Noticias")},
	{ID: "tips", Name: "Microsoft.Getstarted", Categoria: CatSuporte, Basico: true, Title: tr("Dicas", "Tips", "Consejos")},
	{ID: "solitaire", Name: "Microsoft.MicrosoftSolitaireCollection", Categoria: CatJogos, Basico: true, Title: tr("Microsoft Solitaire Collection", "Microsoft Solitaire Collection", "Microsoft Solitaire Collection")},
	{ID: "office-hub", Name: "Microsoft.MicrosoftOfficeHub", Categoria: CatProdutividade, Basico: true, Title: tr("Microsoft 365 (Office)", "Microsoft 365 (Office)", "Microsoft 365 (Office)")},
	{ID: "feedback-hub", Name: "Microsoft.WindowsFeedbackHub", Categoria: CatSuporte, Basico: true, Title: tr("Central de Comentários", "Feedback Hub", "Centro de opiniones")},
	{ID: "skype", Name: "Microsoft.SkypeApp", Categoria: CatComunicacao, Basico: true, Title: tr("Skype", "Skype", "Skype")},
	{ID: "mixed-reality", Name: "Microsoft.MixedReality.Portal", Categoria: CatRealidade, Basico: true, Title: tr("Portal de Realidade Misturada", "Mixed Reality Portal", "Portal de realidad mixta")},
	{ID: "phone-link", Name: "Microsoft.YourPhone", Categoria: CatComunicacao, Title: tr("Vínculo com o Celular", "Phone Link", "Vincular con el móvil")},
	{ID: "people", Name: "Microsoft.People", Categoria: CatComunicacao, Title: tr("Pessoas", "People", "Contactos")},
	{ID: "clipchamp", Name: "Clipchamp.Clipchamp", Categoria: CatMidia, Title: tr("Clipchamp", "Clipchamp", "Clipchamp")},
	{ID: "todo", Name: "Microsoft.Todos", Categoria: CatProdutividade, Title: tr("Microsoft To Do", "Microsoft To Do", "Microsoft To Do")},
	{ID: "teams-pessoal", Name: "MicrosoftTeams", Categoria: CatComunicacao, Title: tr("Microsoft Teams (pessoal)", "Microsoft Teams (personal)", "Microsoft Teams (personal)")},
}

// Apps devolve uma cópia do catálogo, na ordem de exibição.
func Apps() []App { return append([]App(nil), catalogo...) }

// Find acha um app pelo ID curto.
func Find(id string) (App, bool) {
	for _, a := range catalogo {
		if a.ID == id {
			return a, true
		}
	}
	return App{}, false
}

// FindByName acha um app pelo nome EXATO do pacote.
func FindByName(name string) (App, bool) {
	for _, a := range catalogo {
		if a.Name == name {
			return a, true
		}
	}
	return App{}, false
}

// ParaPerfil devolve os apps de um perfil (nil para perfil desconhecido).
func ParaPerfil(p Perfil) []App {
	switch p {
	case PerfilBasico:
		var out []App
		for _, a := range catalogo {
			if a.Basico {
				out = append(out, a)
			}
		}
		return out
	case PerfilCompleto:
		return Apps()
	}
	return nil
}

// ParsePerfil converte o texto da linha de comando.
func ParsePerfil(s string) (Perfil, error) {
	switch p := Perfil(strings.ToLower(strings.TrimSpace(s))); p {
	case PerfilBasico, PerfilCompleto:
		return p, nil
	}
	return "", ErrPerfilDesconhecido
}

// reName é o formato fechado de nome de pacote que pode ir para qualquer script.
var reName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.]{0,100}$`)

// ValidName diz se o nome tem o formato seguro de nome de pacote.
func ValidName(s string) bool { return reName.MatchString(s) }

// protegidosExatos e protegidosPrefixo (minúsculas) formam a lista de NEGAÇÃO: o que nunca entra no catálogo nem é removido,
// mesmo que alguém o coloque lá por engano. Cobre Store, winget, WebView2/Edge, shell, Configurações, busca, Defender,
// runtimes, terminal, Fotos/Calculadora/Bloco de Notas, Xbox/serviços de jogos e codecs.
var protegidosExatos = map[string]bool{
	"microsoft.windowsstore":            true,
	"microsoft.gethelp":                 true, // hospeda os solucionadores de problemas do Windows (Update, rede, áudio, BITS)
	"microsoft.storepurchaseapp":        true,
	"microsoft.desktopappinstaller":     true,
	"microsoft.sechealthui":             true,
	"microsoft.windowsterminal":         true,
	"microsoft.windowsnotepad":          true,
	"microsoft.windowscalculator":       true,
	"microsoft.windowscamera":           true,
	"microsoft.paint":                   true,
	"microsoft.screensketch":            true,
	"microsoft.lockapp":                 true,
	"microsoft.accountscontrol":         true,
	"microsoft.aad.brokerplugin":        true,
	"microsoft.creddialoghost":          true,
	"microsoft.asynctextservice":        true,
	"microsoft.ecapp":                   true,
	"microsoft.bioenrollment":           true,
	"microsoft.win32webviewhost":        true,
	"microsoft.gamingservices":          true,
	"microsoft.xboxidentityprovider":    true,
	"microsoft.xboxgamingoverlay":       true,
	"microsoft.xboxgamecallableui":      true,
	"microsoft.xboxspeechtotextoverlay": true,
}

var protegidosPrefixo = []string{
	"microsoft.windows.", // ShellExperienceHost, StartMenuExperienceHost, Search, Cortana, Photos, CloudExperienceHost...
	"microsoftwindows.",  // MicrosoftWindows.Client.*
	"windows.",           // Windows.ImmersiveControlPanel (Configurações) e demais
	"microsoft.net.",     // .NET Native / runtimes
	"microsoft.vclibs.",  // VCLibs
	"microsoft.ui.",      // UI.Xaml
	"microsoft.windowsappruntime.",
	"microsoft.webview2",
	"microsoft.microsoftedge",
	"microsoft.edge",
	"microsoft.xbox", // identidade, overlay e demais pacotes Xbox
	"microsoft.gamingservices",
	"microsoft.heifimageextension",
	"microsoft.hevcvideoextension",
	"microsoft.vp9videoextensions",
	"microsoft.webpimageextension",
	"microsoft.rawimageextension",
	"microsoft.av1videoextension",
	"microsoft.mpeg2videoextension",
	"microsoft.webmediaextensions",
	"microsoft.services.store.",
	"microsoft.directx",
}

// Protected diz se o nome é de um pacote que o toolkit NUNCA remove (comparação sem diferenciar maiúsculas).
func Protected(name string) bool {
	n := strings.ToLower(name)
	if protegidosExatos[n] {
		return true
	}
	for _, p := range protegidosPrefixo {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// Selecionar monta a lista de apps de um perfil mais os ids pedidos (sem repetir, na ordem do catálogo). Perfil vazio com
// ids é permitido; sem nenhum dos dois é ErrNadaSelecionado.
func Selecionar(p Perfil, ids []string) ([]App, error) {
	if p == "" && len(ids) == 0 {
		return nil, ErrNadaSelecionado
	}
	want := map[string]bool{}
	if p != "" {
		ps := ParaPerfil(p)
		if ps == nil {
			return nil, ErrPerfilDesconhecido
		}
		for _, a := range ps {
			want[a.ID] = true
		}
	}
	for _, id := range ids {
		a, ok := Find(strings.ToLower(strings.TrimSpace(id)))
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrAppDesconhecido, printableShort(id))
		}
		want[a.ID] = true
	}
	var out []App
	for _, a := range catalogo {
		if want[a.ID] {
			out = append(out, a)
		}
	}
	return out, nil
}

// printableShort limita e limpa texto vindo da linha de comando para a mensagem de erro.
func printableShort(s string) string {
	r := []rune(s)
	if len(r) > 60 {
		r = append(r[:60], '…')
	}
	return textsafe.Line(string(r))
}
