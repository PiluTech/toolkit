// Package toolkit embute os arquivos de dados do binário (catálogo padrão).
// Fica na raiz porque //go:embed não alcança diretórios acima do pacote.
package toolkit

import _ "embed"

// CatalogYAML é o catálogo padrão (catalog/catalog.yaml).
//
//go:embed catalog/catalog.yaml
var CatalogYAML []byte
