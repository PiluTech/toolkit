// Package catalog carrega e valida o catálogo de apps (catalog.yaml).
// A validação roda sobre yaml.Node, então cada erro aponta linha e coluna.
package catalog

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/PiluTech/toolkit/internal/tmpl"
)

// Categories é a lista fixa de categorias. Categoria desconhecida é erro na carga.
var Categories = []string{
	"comunicacao", "dev", "navegadores", "midia", "utilitarios",
	"seguranca", "escritorio", "jogos", "drivers", "sistema",
}

// Chaves das listas de instaladores por SO.
const (
	KeyWindows = "windows"
	KeyMacOS   = "macos"
	KeyLinux   = "linux"
	KeyAny     = "any"
)

// Providers conhecidos.
const (
	ProviderWinget        = "winget"
	ProviderBrew          = "brew"
	ProviderApt           = "apt"
	ProviderDnf           = "dnf"
	ProviderPacman        = "pacman"
	ProviderFlatpak       = "flatpak"
	ProviderGitHubRelease = "github-release"
	ProviderURL           = "url"
)

// Versions descreve a seleção de versão de um app.
type Versions struct {
	Query bool     // versions: true (o gerenciador lista)
	List  []string // versions: [...] (a primeira é a padrão)
}

// Enabled diz se o app aceita app@versão.
func (v Versions) Enabled() bool { return v.Query || len(v.List) > 0 }

// Installer é UMA entrada de instalação (item de windows:/macos:/linux:/any:), ainda com templates.
type Installer struct {
	Provider string
	ID       string // winget, flatpak
	Package  string // apt, dnf, pacman
	Formula  string // brew
	Cask     string // brew
	Source   string // winget source / flatpak remote
	Repo     string // github-release
	Asset    string // github-release
	URL      string // url
	SHA256   string // url (obrigatório) / github-release (opcional)
	Type     string
	Args     []string
	Line     int
	Col      int
}

// Detect diz como achar o app JÁ instalado fora do gerenciador do catálogo (só leitura; bloco opcional
// `detect:` do app, um por SO). Cada campo só vale no SO dele; vazio = não usado.
type Detect struct {
	AppBundle   string // macos: nome do bundle em /Applications ou ~/Applications ("Discord.app")
	BundleID    string // macos: CFBundleIdentifier ("com.hnc.Discord")
	DisplayName string // windows: DisplayName do registro Uninstall
	DesktopID   string // linux: arquivo .desktop ("discord.desktop")
	FlatpakID   string // linux: id do flatpak
	Snap        string // linux: nome do snap
	Line        int
	Col         int
}

// Empty diz se nenhum campo está preenchido.
func (d Detect) Empty() bool {
	return d.AppBundle == "" && d.BundleID == "" && d.DisplayName == "" && d.DesktopID == "" && d.FlatpakID == "" && d.Snap == ""
}

// App é um item do catálogo.
type App struct {
	ID         string
	Name       string
	Category   string
	Versions   Versions
	Installers map[string][]Installer // chaves: windows, macos, linux, any
	Detect     map[string]Detect      // chaves: windows, macos, linux (opcional)
	Line       int
}

// DetectFor devolve o bloco detect do SO ("windows", "darwin", "linux"); ok é false quando não há.
func (a App) DetectFor(goos string) (Detect, bool) {
	key := goos
	if goos == "darwin" {
		key = KeyMacOS
	}
	d, ok := a.Detect[key]
	return d, ok && !d.Empty()
}

// InstallersFor devolve a lista do SO ("windows", "darwin", "linux"), com fallback para any.
func (a App) InstallersFor(goos string) []Installer {
	key := goos
	if goos == "darwin" {
		key = KeyMacOS
	}
	if l := a.Installers[key]; len(l) > 0 {
		return l
	}
	return a.Installers[KeyAny]
}

// Catalog é o conjunto de apps, na ordem do arquivo.
type Catalog struct {
	Apps  map[string]App
	Order []string
}

// List devolve os apps na ordem do arquivo.
func (c *Catalog) List() []App {
	out := make([]App, 0, len(c.Order))
	for _, id := range c.Order {
		out = append(out, c.Apps[id])
	}
	return out
}

// Get procura um app pelo id.
func (c *Catalog) Get(id string) (App, bool) {
	a, ok := c.Apps[id]
	return a, ok
}

// clone faz cópia profunda: o catálogo mesclado não compartilha mapas nem fatias com os de origem.
func (a App) clone() App {
	out := a
	out.Versions.List = append([]string(nil), a.Versions.List...)
	out.Installers = make(map[string][]Installer, len(a.Installers))
	for k, list := range a.Installers {
		cp := make([]Installer, len(list))
		for i, in := range list {
			in.Args = append([]string(nil), in.Args...)
			cp[i] = in
		}
		out.Installers[k] = cp
	}
	if a.Detect != nil {
		out.Detect = make(map[string]Detect, len(a.Detect))
		for k, d := range a.Detect {
			out.Detect[k] = d
		}
	}
	return out
}

// Merge devolve base com ext por cima: o mesmo id substitui. Com replace, devolve só ext. base e ext
// nil valem como vazios. O segundo valor lista, na ordem de ext, os ids de base que foram substituídos
// (o chamador deve avisar o usuário: um catálogo local pode esconder o app embutido). A cópia é profunda.
func Merge(base, ext *Catalog, replace bool) (*Catalog, []string) {
	out := &Catalog{Apps: map[string]App{}}
	if base == nil {
		base = &Catalog{}
	}
	if ext == nil {
		ext = &Catalog{}
	}
	if !replace {
		for _, id := range base.Order {
			out.Apps[id] = base.Apps[id].clone()
			out.Order = append(out.Order, id)
		}
	}
	var replaced []string
	for _, id := range ext.Order {
		if _, exists := out.Apps[id]; exists {
			replaced = append(replaced, id)
		} else {
			out.Order = append(out.Order, id)
		}
		out.Apps[id] = ext.Apps[id].clone()
	}
	return out, replaced
}

// VersionVars monta as variáveis de versão ({{version}}, {{major}}, {{major_minor}}) para a versão pedida
// (vazia = a padrão, a primeira de Versions.List). Com lista, a versão TEM de estar nela; sem lista só a
// consulta ao gerenciador (versions: true) aceita versão, e então só {{version}} é definida. O valor
// precisa passar em tmpl.ValidValue.
func (a App) VersionVars(requested string) (map[string]string, error) {
	v := requested
	switch {
	case len(a.Versions.List) > 0:
		if v == "" {
			v = a.Versions.List[0]
		}
		if !slices.Contains(a.Versions.List, v) {
			return nil, fmt.Errorf("app %q: versão %q fora da lista (%s)", a.ID, v, strings.Join(a.Versions.List, ", "))
		}
	case a.Versions.Query:
		if v == "" {
			return map[string]string{}, nil
		}
	default:
		if v != "" {
			return nil, fmt.Errorf("app %q não aceita escolha de versão", a.ID)
		}
		return map[string]string{}, nil
	}
	if !tmpl.ValidValue(v) {
		return nil, fmt.Errorf("app %q: versão %q com caracteres não permitidos", a.ID, v)
	}
	vars := map[string]string{tmpl.Version: v}
	if len(a.Versions.List) > 0 {
		parts := strings.Split(v, ".")
		vars[tmpl.Major] = parts[0]
		vars[tmpl.MajorMinor] = strings.Join(parts[:min(2, len(parts))], ".")
	}
	return vars, nil
}

// Error é um problema de validação com posição.
type Error struct {
	File string
	Line int
	Col  int
	Msg  string
}

func (e Error) Error() string {
	if e.Line == 0 {
		return fmt.Sprintf("%s: %s", e.File, e.Msg)
	}
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Msg)
}

// Errors reúne todos os problemas de um arquivo (a carga relata todos de uma vez).
type Errors []Error

func (es Errors) Error() string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "\n")
}

func sortedKeys(m map[string]bool) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
