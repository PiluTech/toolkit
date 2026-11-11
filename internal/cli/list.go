package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	toolkit "github.com/PiluVitu/toolkit"
	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/msg"
)

// Deps são as dependências injetáveis do CLI.
type Deps struct {
	GOOS        string // sistema padrão para consultas
	EmbeddedYML []byte // catálogo embutido
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
			return runList(cmd.OutOrStdout(), deps, f)
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

func loadCatalog(deps Deps, f listFlags) (*catalog.Catalog, error) {
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
	return catalog.Merge(base, ext, f.replace), nil
}

func runList(out io.Writer, deps Deps, f listFlags) error {
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
	cat, err := loadCatalog(deps, f)
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
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", it.ID, it.Name, it.Category, managers, versions)
	}
	return tw.Flush()
}
