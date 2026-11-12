package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/spf13/cobra"

	toolkit "github.com/PiluTech/toolkit"
	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/providers"
)

// Deps são as dependências injetáveis do CLI.
type Deps struct {
	GOOS        string // sistema padrão para consultas
	EmbeddedYML []byte // catálogo embutido
	// NewRunner devolve o Runner que executa os comandos do install (nil = exec.OSRunner). Os testes injetam o
	// exectest.FakeRunner: nenhum teste chega a um gerenciador de verdade.
	NewRunner func() exec.Runner
	// Providers monta os providers sobre o Runner (nil = os seis: winget, brew, apt, dnf, pacman, flatpak).
	Providers func(exec.Runner) []providers.Provider
	// CheckElevation avisa, no dry-run, quando um passo exigiria administrador (nil = OSRunner).
	CheckElevation func(exec.Cmd) error
	// Stdin e Interactive decidem a confirmação do install real: sem terminal (Interactive false) exige --yes.
	// Nil = os.Stdin e "stdin é um terminal".
	Stdin       io.Reader
	Interactive func() bool
}

// DefaultDeps usa o sistema atual e o catálogo embutido no binário.
func DefaultDeps() Deps {
	return Deps{GOOS: runtime.GOOS, EmbeddedYML: toolkit.CatalogYAML}
}

type listFlags struct {
	catalog, sha256, category, os string
	replace, json                 bool
}

type listItem struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Providers []string `json:"providers"`
	Versions  []string `json:"versions,omitempty"`
	// VersionsQuery é true quando o gerenciador lista as versões (versions: true).
	VersionsQuery bool `json:"versions_query,omitempty"`
}

func newListCmd(deps Deps) *cobra.Command {
	var f listFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: msg.ListShort,
		Long:  msg.ListLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.catalog, "catalog", "", msg.FlagCatalog)
	fl.StringVar(&f.sha256, "catalog-sha256", "", msg.FlagCatalogSHA256)
	fl.BoolVar(&f.replace, "catalog-replace", false, msg.FlagCatalogReplace)
	fl.StringVar(&f.category, "category", "", msg.FlagCategory)
	fl.StringVar(&f.os, "os", "", msg.FlagOS)
	fl.BoolVar(&f.json, "json", false, msg.FlagJSON)
	return cmd
}

func isURL(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

func loadCatalog(deps Deps, f listFlags, errOut io.Writer) (*catalog.Catalog, error) {
	if f.replace && f.catalog == "" {
		return nil, fmt.Errorf("%s", msg.ErrReplaceNoCat)
	}
	if f.sha256 != "" && !isURL(f.catalog) {
		return nil, fmt.Errorf("%s", msg.ErrSHAWithoutURL)
	}
	if isURL(f.catalog) {
		return nil, fmt.Errorf("%s", msg.ErrCatalogURL)
	}
	base, err := catalog.Parse("catalog.yaml (embutido)", deps.EmbeddedYML)
	if err != nil {
		return nil, err
	}
	if f.catalog == "" {
		return base, nil
	}
	ext, err := catalog.LoadFile(f.catalog)
	if err != nil {
		return nil, err
	}
	merged, replaced := catalog.Merge(base, ext, f.replace)
	for _, id := range replaced { // stderr: não polui a saída --json
		_, _ = fmt.Fprintf(errOut, msg.WarnReplaced+"\n", id)
	}
	return merged, nil
}

func runList(out, errOut io.Writer, deps Deps, f listFlags) error {
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
	cat, err := loadCatalog(deps, f, errOut)
	if err != nil {
		return err
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
		items = append(items, it)
	}

	if f.json {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}
	return writeTable(out, items)
}

// printable devolve s com tudo que não é imprimível (controle, ESC, quebra de linha, U+2028...) escapado
// como \xNN, \uNNNN ou \UNNNNNNNN: a tabela nunca repassa sequência de terminal. (A carga já recusa esses
// caracteres; isto é a segunda camada.)
func printable(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsPrint(r) || r == ' ':
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	return b.String()
}

func writeTable(out io.Writer, items []listItem) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, msg.ListEmpty)
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", msg.ListHeaderID, msg.ListHeaderName, msg.ListHeaderCat, msg.ListHeaderManager, msg.ListHeaderVersion)
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
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", printable(it.ID), printable(it.Name), printable(it.Category), printable(managers), printable(versions))
	}
	return tw.Flush()
}
