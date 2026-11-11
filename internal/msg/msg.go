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

// Textos do comando install.
const (
	InstallShort = "Instala apps do catálogo"
	InstallLong  = "Instala um ou mais apps do catálogo pelo gerenciador do sistema (winget, brew, apt, dnf, pacman, flatpak).\n\nUse app@versão para escolher a versão (a lista do catálogo, ou a versão do gerenciador nos apps com versions: true). Rodar de novo não reinstala o que já está instalado.\n\nCom --dry-run nada é instalado: o toolkit só lê o que já está instalado e mostra o plano. Em Linux e macOS o sudo é chamado sem pedir senha (sudo -n): rode \"sudo -v\" antes."

	FlagDryRun = "não instala nada: lê o estado e mostra o plano"

	ErrInstallBadTarget  = "argumento %q inválido: use app ou app@versão"
	ErrInstallUnknownApp = "app %q não está no catálogo (veja: toolkit list)"
	ErrInstallNoVersions = "o app %q não aceita escolha de versão"
	ErrInstallFailed     = "falhou: %s"
	HintNeedsAdmin       = "dica: rode \"sudo -v\" e tente de novo (o toolkit usa sudo -n e nunca pede senha)"

	InstallHeader      = "==> %s (%s)"
	InstallAlready     = "já instalado (%s%s)"
	InstallDone        = "instalado com %s"
	InstallDoneSimul   = "seria instalado com %s (simulado)"
	InstallReboot      = "reinicie o sistema para concluir a instalação"
	InstallPlanHeader  = "Plano (dry-run: nada foi executado; as leituras do estado foram reais):"
	InstallPlanAdmin   = "  [exige administrador] %s"
	InstallPlanLine    = "  %s"
	InstallNoPlan      = "Nada a fazer."
	InstallFlatpakNote = "aviso: o flatpak baixa também os runtimes e as dependências do app (podem ser GB) e só instala no sistema (--system); \"já instalado\" vale também para a instalação --user"
)
