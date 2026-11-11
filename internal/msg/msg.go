// Package msg concentra as mensagens ao usuário (pt-BR), para permitir outro idioma depois.
package msg

// Textos do comando raiz e do version.
const (
	RootShort = "Prepara máquinas Windows, Linux e macOS com um comando"
	RootLong  = "Prepara máquinas Windows, Linux e macOS com um comando: catálogo de apps, receitas multi-etapa e diagnóstico."

	VersionShort = "Mostra a versão"
	VersionLong  = "Mostra a versão, o commit e a data do build."
)

// Textos do comando list.
const (
	ListShort = "Lista os apps do catálogo"
	ListLong  = "Lista os apps do catálogo e os gerenciadores que cada um usa no sistema atual.\n\nSem --catalog, usa o catálogo embutido. Um catálogo local é mesclado sobre o embutido (o mesmo id substitui); com --catalog-replace ele troca o embutido inteiro."

	FlagCatalog        = "caminho de um catálogo local (YAML) para mesclar sobre o embutido"
	FlagCatalogSHA256  = "SHA-256 do catálogo (obrigatório para URL)"
	FlagCatalogReplace = "usa só o catálogo de --catalog, sem o embutido"
	FlagCategory       = "mostra só uma categoria"
	FlagOS             = "sistema a consultar: windows, linux ou darwin (padrão: o atual)"
	FlagJSON           = "saída em JSON"

	ErrCatalogURL     = "catálogo por URL ainda não é suportado; baixe o arquivo, confira o SHA-256 e use o caminho local"
	ErrSHAWithoutURL  = "--catalog-sha256 só vale com --catalog apontando para uma URL"
	ErrReplaceNoCat   = "--catalog-replace exige --catalog"
	ErrUnknownOS      = "sistema desconhecido %q (use windows, linux ou darwin)"
	ErrUnknownCat     = "categoria desconhecida %q (use: %s)"
	WarnReplaced      = "aviso: o catálogo local substituiu o app embutido %q"
	ListEmpty         = "Nenhum app encontrado."
	ListHeaderID      = "ID"
	ListHeaderName    = "NOME"
	ListHeaderCat     = "CATEGORIA"
	ListHeaderManager = "GERENCIADORES"
	ListHeaderVersion = "VERSÕES"
	ListNoInstaller   = "(nenhum neste sistema)"
	ListVersionsAny   = "sim"
	ListVersionsNone  = "-"
)
