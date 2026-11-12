// Package toolkit embute os arquivos de dados do binário (catálogo padrão e receitas).
// Fica na raiz porque //go:embed não alcança diretórios acima do pacote.
package toolkit

import "embed"

// CatalogYAML é o catálogo padrão (catalog/catalog.yaml).
//
//go:embed catalog/catalog.yaml
var CatalogYAML []byte

// Recipes são as receitas embutidas (recipes/<so>/<id>.yaml).
//
//go:embed recipes
var Recipes embed.FS
