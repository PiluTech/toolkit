// Package catalog carrega e valida o catálogo de apps (catalog.yaml).
// A validação roda sobre yaml.Node, então cada erro aponta linha e coluna.
package catalog

import (
	"fmt"
	"sort"
	"strings"
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

// App é um item do catálogo.
type App struct {
	ID         string
	Name       string
	Category   string
	Versions   Versions
	Installers map[string][]Installer // chaves: windows, macos, linux, any
	Line       int
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

// Merge devolve base com ext por cima: o mesmo id substitui. Com replace, devolve só ext.
func Merge(base, ext *Catalog, replace bool) *Catalog {
	out := &Catalog{Apps: map[string]App{}}
	if !replace {
		for _, id := range base.Order {
			out.Apps[id] = base.Apps[id]
			out.Order = append(out.Order, id)
		}
	}
	for _, id := range ext.Order {
		if _, exists := out.Apps[id]; !exists {
			out.Order = append(out.Order, id)
		}
		out.Apps[id] = ext.Apps[id]
	}
	return out
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
