package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"

	toolkit "github.com/PiluTech/toolkit"
	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/detect"
	"github.com/PiluTech/toolkit/internal/download"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/providers"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Deps são as dependências injetáveis do CLI.
type Deps struct {
	GOOS string // sistema padrão para consultas
	// Euid é o uid efetivo (nil: os.Geteuid). O plano do install mostra o comando como o runner real o executa:
	// como root, sem sudo.
	Euid func() int
	// ForceExternal: o `install --force` instala mesmo com o app achado fora do gerenciador (com aviso).
	ForceExternal bool
	// TUI são os pontos injetáveis da abertura da interface (zero = terminal e tui.Run reais).
	TUI         TUIDeps
	EmbeddedYML []byte // catálogo embutido
	// NewRunner devolve o Runner que executa os comandos do install (nil = exec.OSRunner). Os testes injetam o
	// exectest.FakeRunner: nenhum teste chega a um gerenciador de verdade.
	NewRunner func() exec.Runner
	// Providers monta os providers sobre o Runner (nil = os oito: winget, brew, apt, dnf, pacman, flatpak, url e github-release).
	Providers func(exec.Runner) []providers.Provider
	// CheckElevation avisa, no dry-run, quando um passo exigiria administrador (nil = OSRunner).
	CheckElevation func(exec.Cmd) error
	// Downloader baixa instaladores (provider url) e catálogos por URL (nil = cliente estrito do pacote download).
	// Os testes injetam o de internal/download/downloadtest.
	Downloader *download.Downloader
	// AppsDir é onde os arquivos compactados (zip, tar.gz) do provider url são extraídos (nil = pasta do usuário).
	AppsDir func() (string, error)
	// Update são os pontos injetáveis do `toolkit update` (zero = a release do GitHub e o binário em execução).
	Update UpdateDeps
	// UpdateCheck são os pontos injetáveis do aviso de versão nova (zero = ambiente real; build dev nunca consulta).
	UpdateCheck UpdateCheckDeps
	// Diag são os pontos injetáveis do `toolkit diagnostico` (zero = a máquina real, só leitura).
	Diag DiagDeps
	// Rede são os pontos injetáveis do `toolkit rede` (zero = a rede real, só leitura).
	Rede RedeDeps
	// Priv são os pontos injetáveis do `toolkit privacidade` (zero = o Windows real e o backup na pasta de estado).
	Priv PrivDeps
	// AppsNat são os pontos injetáveis do `toolkit apps-nativos` (zero = o Windows real e o backup por SID na pasta de estado).
	AppsNat AppsNatDeps
	// GitHubToken devolve o token da API de releases do GitHub (nil = variável GITHUB_TOKEN). Só vai ao api.github.com.
	GitHubToken func() string
	// Detector monta o detector de apps instalados FORA do gerenciador (.dmg no macOS, instalador no Windows,
	// .desktop/flatpak/snap no Linux) sobre o Runner. Nil = detecção desligada: os testes nunca leem o disco nem o
	// registro reais; DefaultDeps liga o detector real.
	Detector func(exec.Runner) ExternalDetector
	// Stdin e Interactive decidem a confirmação do install real: sem terminal (Interactive false) exige --yes.
	// Nil = os.Stdin e "stdin é um terminal".
	Stdin       io.Reader
	Interactive func() bool
}

// DefaultDeps usa o sistema atual e o catálogo embutido no binário.
func DefaultDeps() Deps {
	return Deps{GOOS: runtime.GOOS, EmbeddedYML: toolkit.CatalogYAML, Detector: realDetector}
}

// ExternalDetector procura um app instalado fora do gerenciador (só leitura); *detect.Detector o implementa.
type ExternalDetector interface {
	Find(ctx context.Context, app catalog.App) (detect.Found, bool, error)
}

// realDetector usa o disco real (raiz do sistema no Unix, aberta SEM bloquear em FIFO) e a pasta do usuário. É UM
// Detector por processo: a varredura de .app por bundle_id fica em cache nele (a TUI pergunta vários apps em
// seguida) e quem muda o sistema (a execução da TUI) chama Invalidate.
var (
	realDetectorOnce sync.Once
	realDetectorVal  *detect.Detector
)

func realDetector(r exec.Runner) ExternalDetector {
	realDetectorOnce.Do(func() {
		o := detect.Options{GOOS: runtime.GOOS}
		if runtime.GOOS != "windows" {
			o.FS = detect.OSFS{}
		}
		if h, err := os.UserHomeDir(); err == nil && filepath.IsAbs(h) {
			o.Home = filepath.ToSlash(h)
		}
		realDetectorVal = detect.New(r, o)
	})
	return realDetectorVal
}

type listFlags struct {
	catalog, sha256, category, os string
	replace, json                 bool
	installed                     bool
}

type listItem struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Providers []string `json:"providers"`
	Versions  []string `json:"versions,omitempty"`
	// VersionsQuery é true quando o gerenciador lista as versões (versions: true).
	VersionsQuery bool `json:"versions_query,omitempty"`
	// Campos de --installed (omitidos sem a flag). Installed é true para instalado pelo gerenciador E para
	// instalado fora dele; Source diz qual: "manager" ou "external".
	Installed        *bool  `json:"installed,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
	Source           string `json:"installed_source,omitempty"`
	// External* só existem quando Source é "external": o nome do gerenciador esperado e o lugar do app.
	ExternalManager string `json:"external_manager,omitempty"`
	ExternalPath    string `json:"external_path,omitempty"`
	// StatusError: não deu para verificar (sem instalador aplicável, leitura falhou).
	StatusError string `json:"status_error,omitempty"`
}

func newListCmd(deps Deps) *cobra.Command {
	var f listFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: msg.ListShort,
		Long:  msg.ListLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.catalog, "catalog", "", msg.FlagCatalog)
	fl.StringVar(&f.sha256, "catalog-sha256", "", msg.FlagCatalogSHA256)
	fl.BoolVar(&f.replace, "catalog-replace", false, msg.FlagCatalogReplace)
	fl.StringVar(&f.category, "category", "", msg.FlagCategory)
	fl.StringVar(&f.os, "os", "", msg.FlagOS)
	fl.BoolVar(&f.json, "json", false, msg.FlagJSON)
	fl.BoolVar(&f.installed, "installed", false, msg.FlagInstalled)
	return cmd
}

func isURL(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

var catalogSHARe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// loadRemoteCatalog baixa um catálogo por URL: --catalog-sha256 é OBRIGATÓRIO (64 hex, validado antes de qualquer
// requisição), o arquivo é lido em memória até catalog.MaxFileSize, o hash é conferido ANTES de o YAML ser lido
// e só então o conteúdo passa pelo Parse (que cita a URL nas mensagens de erro). A URL segue a política do
// pacote download (https, host público, redirects revalidados). O catálogo NÃO importa o download: a carga
// remota mora aqui no CLI.
func loadRemoteCatalog(ctx context.Context, deps Deps, rawURL, sha string) (*catalog.Catalog, error) {
	if sha == "" {
		return nil, fmt.Errorf("%s", msg.ErrCatalogURLNeedsSHA)
	}
	if !catalogSHARe.MatchString(sha) {
		return nil, fmt.Errorf("%s", msg.ErrCatalogSHAInvalid)
	}
	data, err := deps.downloader().Bytes(ctx, download.Request{URL: rawURL, SHA256: sha, MaxSize: catalog.MaxFileSize})
	if err != nil {
		return nil, fmt.Errorf("catálogo %s: %w", printable(rawURL), err)
	}
	name := rawURL
	if u, err := neturl.Parse(rawURL); err == nil {
		name = u.Host + u.Path
	}
	return catalog.Parse(printable(name), data)
}

func loadCatalog(ctx context.Context, deps Deps, f listFlags, errOut io.Writer) (*catalog.Catalog, error) {
	if f.replace && f.catalog == "" {
		return nil, fmt.Errorf("%s", msg.ErrReplaceNoCat)
	}
	if f.sha256 != "" && !isURL(f.catalog) {
		return nil, fmt.Errorf("%s", msg.ErrSHAWithoutURL)
	}
	base, err := catalog.Parse("catalog.yaml (embutido)", deps.EmbeddedYML)
	if err != nil {
		return nil, err
	}
	if f.catalog == "" {
		return base, nil
	}
	var ext *catalog.Catalog
	if isURL(f.catalog) {
		ext, err = loadRemoteCatalog(ctx, deps, f.catalog, f.sha256)
	} else {
		ext, err = catalog.LoadFile(f.catalog)
	}
	if err != nil {
		return nil, err
	}
	merged, replaced := catalog.Merge(base, ext, f.replace)
	for _, id := range replaced { // stderr: não polui a saída --json
		_, _ = fmt.Fprintf(errOut, msg.WarnReplaced+"\n", id)
	}
	return merged, nil
}

func runList(ctx context.Context, out, errOut io.Writer, deps Deps, f listFlags) error {
	goos := f.os
	if goos == "" {
		goos = deps.GOOS
	}
	if !slices.Contains([]string{"windows", "linux", "darwin"}, goos) {
		return fmt.Errorf(msg.ErrUnknownOS, goos)
	}
	if f.category != "" && !slices.Contains(catalog.Categories, f.category) {
		return fmt.Errorf(msg.ErrUnknownCat, f.category, strings.Join(catalog.Categories, ", "))
	}
	if f.installed && goos != deps.GOOS {
		return fmt.Errorf(msg.ErrInstalledOtherOS, goos, deps.GOOS)
	}
	cat, err := loadCatalog(ctx, deps, f, errOut)
	if err != nil {
		return err
	}
	var rn exec.Runner
	var reg *providers.Registry
	if f.installed {
		rn = deps.runner()
		reg = providers.NewRegistry(deps.providerList(rn)...)
	}

	items := []listItem{}
	for _, app := range cat.List() {
		if f.category != "" && app.Category != f.category {
			continue
		}
		it := listItem{ID: app.ID, Name: app.Name, Category: app.Category, Providers: []string{},
			Versions: app.Versions.List, VersionsQuery: app.Versions.Query}
		for _, in := range app.InstallersFor(goos) {
			it.Providers = append(it.Providers, in.Provider)
		}
		if f.installed {
			fillState(ctx, &it, deps, rn, reg, app)
		}
		items = append(items, it)
	}

	if f.json {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}
	return writeTable(out, items, f.installed)
}

// fillState preenche os campos de --installed (leitura): gerenciador, fora do gerenciador ou não instalado.
func fillState(ctx context.Context, it *listItem, deps Deps, rn exec.Runner, reg *providers.Registry, app catalog.App) {
	if len(it.Providers) == 0 {
		it.StatusError = msg.ListNoInstaller
		return
	}
	st, err := readState(ctx, deps, rn, reg, app)
	if err != nil {
		it.StatusError = printable(err.Error())
		return
	}
	inst := st.Installed
	it.Installed = &inst
	it.InstalledVersion = printable(st.Version)
	switch {
	case st.External != nil:
		it.Source = "external"
		it.InstalledVersion = printable(st.External.Version)
		it.ExternalManager = printable(st.External.Manager)
		it.ExternalPath = printable(st.External.Where)
	case st.Installed:
		it.Source = "manager"
	}
}

// stateCell é o texto da coluna INSTALADO (palavras, sem depender de símbolo ou cor).
func stateCell(it listItem) string {
	switch {
	case it.StatusError != "":
		return msg.ListStateUnknown
	case it.Installed == nil || !*it.Installed:
		return msg.ListStateNo
	}
	text := msg.ListStateYes
	if it.Source == "external" {
		text = fmt.Sprintf(msg.ListStateExternal, it.ExternalManager)
	}
	if it.InstalledVersion != "" {
		text += " " + it.InstalledVersion
	}
	if it.Source == "external" && it.ExternalPath != "" {
		text += fmt.Sprintf(msg.ListStateExternalAt, it.ExternalPath)
	}
	return text
}

// printable devolve s com tudo que não é imprimível (controle, ESC, quebra de linha, U+2028...) escapado
// como \xNN, \uNNNN ou \UNNNNNNNN: a tabela nunca repassa sequência de terminal. (A carga já recusa esses
// caracteres; isto é a segunda camada.)
func printable(s string) string { return textsafe.Printable(s) }

func writeTable(out io.Writer, items []listItem, withState bool) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, msg.ListEmpty)
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	header := fmt.Sprintf("%s\t%s\t%s\t%s\t%s", msg.ListHeaderID, msg.ListHeaderName, msg.ListHeaderCat, msg.ListHeaderManager, msg.ListHeaderVersion)
	if withState {
		header += "\t" + msg.ListHeaderState
	}
	_, _ = fmt.Fprintln(tw, header)
	for _, it := range items {
		managers := msg.ListNoInstaller
		if len(it.Providers) > 0 {
			managers = strings.Join(it.Providers, ", ")
		}
		versions := msg.ListVersionsNone
		switch {
		case len(it.Versions) > 0:
			versions = strings.Join(it.Versions, ", ")
		case it.VersionsQuery:
			versions = msg.ListVersionsAny
		}
		row := fmt.Sprintf("%s\t%s\t%s\t%s\t%s", printable(it.ID), printable(it.Name), printable(it.Category), printable(managers), printable(versions))
		if withState {
			row += "\t" + printable(stateCell(it))
		}
		_, _ = fmt.Fprintln(tw, row)
	}
	return tw.Flush()
}
