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

	FlagCatalog        = "catálogo (YAML) para mesclar sobre o embutido: caminho local ou URL https (a URL exige --catalog-sha256)"
	FlagCatalogSHA256  = "SHA-256 do catálogo (obrigatório para URL)"
	FlagCatalogReplace = "usa só o catálogo de --catalog, sem o embutido"
	FlagCategory       = "mostra só uma categoria"
	FlagOS             = "sistema a consultar: windows, linux ou darwin (padrão: o atual)"
	FlagJSON           = "saída em JSON"

	ErrCatalogURLNeedsSHA = "--catalog por URL exige --catalog-sha256 <hash>: sem o SHA-256 do arquivo a URL é recusada"
	ErrCatalogSHAInvalid  = "--catalog-sha256 deve ter 64 caracteres hexadecimais"
	ErrSHAWithoutURL      = "--catalog-sha256 só vale com --catalog apontando para uma URL"
	ErrReplaceNoCat       = "--catalog-replace exige --catalog"
	ErrUnknownOS          = "sistema desconhecido %q (use windows, linux ou darwin)"
	ErrUnknownCat         = "categoria desconhecida %q (use: %s)"
	WarnReplaced          = "aviso: o catálogo local substituiu o app embutido %q"
	ListEmpty             = "Nenhum app encontrado."
	ListHeaderID          = "ID"
	ListHeaderName        = "NOME"
	ListHeaderCat         = "CATEGORIA"
	ListHeaderManager     = "GERENCIADORES"
	ListHeaderVersion     = "VERSÕES"
	ListNoInstaller       = "(nenhum neste sistema)"
	FlagInstalled         = "mostra também se cada app está instalado (lê o gerenciador e os apps instalados fora dele; só leitura)"
	ErrInstalledOtherOS   = "--installed só vale para o sistema atual: --os %s, mas a máquina é %s"
	ListHeaderState       = "INSTALADO"
	ListStateYes          = "instalado"
	ListStateExternal     = "instalado (fora do %s)"
	ListStateExternalAt   = " em %s"
	ListStateNo           = "não instalado"
	ListStateUnknown      = "não deu para verificar"
	ListVersionsAny       = "sim"
	ListVersionsNone      = "-"
)

// Textos do comando install.
const (
	InstallShort = "Instala apps do catálogo"
	InstallLong  = "Instala um ou mais apps do catálogo pelo gerenciador do sistema (winget, brew, apt, dnf, pacman, flatpak).\n\nUse app@versão para escolher a versão (a lista do catálogo, ou a versão do gerenciador nos apps com versions: true). Rodar de novo não reinstala o que já está instalado.\n\nCom --dry-run nada é instalado: o toolkit só lê o que já está instalado e mostra o plano. Em Linux e macOS o sudo é chamado sem pedir senha (sudo -n): rode \"sudo -v\" antes."

	FlagDryRun = "não instala nada: lê o estado e mostra o plano"

	FlagYes = "não pergunta antes de instalar (obrigatório sem terminal)"

	ErrInstallBadTarget  = "argumento %q inválido: use app ou app@versão"
	ErrInstallUnknownApp = "app %q não está no catálogo (veja: toolkit list)"
	ErrInstallNoVersions = "o app %q não aceita escolha de versão"
	ErrInstallFailed     = "falhou: %s"
	HintNeedsAdmin       = "dica: rode \"sudo -v\" e tente de novo (o toolkit usa sudo -n e nunca pede senha)"

	UpdateShort          = "atualiza o toolkit para a última release (ou para --version)"
	UpdateLong           = "Baixa a release do GitHub, confere o SHA-256 pelo checksums.txt da mesma release e troca o binário de forma atômica. Nunca usa sudo: instale pelo bootstrap no PATH do usuário."
	UpdateFlagCheck      = "só diz se há versão nova e mostra as notas (não baixa nada)"
	UpdateFlagDryRun     = "mostra o que faria, sem baixar o binário nem trocar nada"
	UpdateFlagVersion    = "instala esta versão (vX.Y.Z), inclusive mais antiga que a atual"
	UpdateUpToDate       = "toolkit %s já é a última versão."
	UpdateAvailable      = "toolkit %s → %s disponível."
	UpdateNotice         = "aviso: toolkit %s disponível (você usa %s). Para atualizar: toolkit update (desligar este aviso: --no-update-check)"
	UpdateNotes          = "Notas da release:"
	UpdateHowTo          = "Para atualizar: toolkit update"
	UpdateDevBuild       = "toolkit %s é um build de desenvolvimento: não há versão para comparar. Use: toolkit update --version vX.Y.Z"
	UpdateNewerInstalled = "o toolkit instalado (%s) é mais novo que a última release (%s): nada a fazer. Para trocar mesmo assim: toolkit update --version %s"
	UpdateDowngrade      = "aviso: voltando de %s para %s (versão pedida com --version)."
	UpdateSame           = "toolkit %s já está instalado: nada a fazer."
	UpdateDryRunLine1    = "toolkit update (dry-run): baixaria %s e %s"
	UpdateDryRunLine2    = "toolkit update (dry-run): conferiria o SHA-256 e trocaria %s por %s"
	UpdateNoNotes        = "aviso: a API do GitHub recusou (limite de consultas); seguindo sem as notas da release."
	UpdateDone           = "toolkit atualizado: %s → %s (SHA-256 conferido). Abra um novo terminal para usar a nova versão."
	UpdateDoneFrom       = "toolkit atualizado para %s (SHA-256 conferido)."

	RootNoTUI              = "não abre a interface de terminal (mostra a ajuda)"
	FlagNoMotion           = "desliga a animação da interface (o spinner vira um símbolo fixo); também vale TOOLKIT_REDUCE_MOTION=1"
	InstallHeader          = "==> %s (%s)"
	InstallAlready         = "já instalado (%s%s)"
	InstallExternal        = "já instalado fora do %s em %s%s: nada a fazer"
	InstallExternalForced  = "aviso: já instalado fora do %s em %s%s; --force: instalando mesmo assim (o gerenciador pode falhar ou sobrescrever o app existente)"
	FlagForce              = "instala mesmo que o app já esteja instalado fora do gerenciador (com aviso)"
	InstallExternalVersion = " (versão %s)"
	InstallDone            = "instalado com %s"
	InstallDoneSimul       = "seria instalado com %s (simulado)"
	InstallReboot          = "reinicie o sistema para concluir a instalação"
	InstallPlanHeader      = "Plano (dry-run: nada foi executado; as leituras do estado foram reais):"
	InstallPlanAdmin       = "  [exige administrador] %s"
	InstallPlanLine        = "  %s"
	InstallNoPlan          = "Nada a fazer."
	InstallPlanHeaderReal  = "Plano (ainda não executado; as leituras do estado foram reais):"
	InstallConfirm         = "Executar este plano? [s/N] "
	InstallCancelled       = "cancelado: nada foi instalado"
	ErrInstallNeedsYes     = "sem terminal para confirmar: confira o plano com --dry-run e rode de novo com --yes"
	WarnPlanSudo           = "aviso: este plano usa sudo -n (nunca pede senha): rode \"sudo -v\" antes ou a execução real falhará com a dica de sudo"
	InstallFlatpakNote     = "aviso: o flatpak baixa também os runtimes e as dependências do app (podem ser GB) e só instala no sistema (--system); \"já instalado\" vale também para a instalação --user"
	FlagNoUpdateCheck      = "não consulta nem avisa de versão nova (também: TOOLKIT_NO_UPDATE_CHECK=1)"
)

// Textos do comando tui.
const (
	TUIShort = "Abre a interface de terminal (TUI)"
	TUILong  = "Abre a interface de terminal: apps, receitas, diagnósticos e log. É o mesmo que rodar o toolkit sem argumentos, mas diz por que não abriu quando não há terminal interativo."

	ErrTUITerm             = "a TUI precisa da variável TERM com um terminal de verdade (ela está vazia ou é \"dumb\"): rode num terminal comum ou use os comandos (list, install, recipe) sem a TUI"
	ErrTUINoTerminal       = "a TUI precisa de um terminal interativo na entrada e na saída (nada redirecionado ou em pipe): rode direto no terminal, ou use os comandos (list, install, recipe) sem a TUI"
	ErrTUINoConsoleWindows = "a TUI precisa de um console interativo do Windows (entrada e saída sem redirecionamento): abra o Windows Terminal, o PowerShell ou o cmd e rode \"toolkit tui\"; no Git Bash (mintty) use \"winpty toolkit.exe tui\"; ou use os comandos (list, install, recipe) sem a TUI"
)

// Textos do comando diagnostico.
const (
	DiagShort = "Diagnostica a máquina (só leitura): hardware, discos, bateria, temperatura e erros"
	DiagLong  = "Lê o estado de saúde da máquina SEM alterar nada e SEM pedir administrador: hardware, discos com SMART, bateria, temperatura, GPU e os erros e falhas recentes do sistema (Windows: Eventos, Confiabilidade e telas azuis; macOS: relatórios de falha e log de erros; Linux: journalctl e dmesg). Mostra um resumo do que está ruim e do que fazer. O que exigir administrador (SMART no Linux, temperatura no macOS) aparece como \"não lido\" com a instrução. As informações aparecem como o sistema as informa: nada é mascarado."

	FlagDiagFull   = "mostra também o relatório completo (todos os dados lidos)"
	FlagDiagJSON   = "saída em JSON (resumo e dados lidos)"
	FlagDiagReport = "grava o resumo e o relatório completo neste arquivo novo (permissão 0600; não sobrescreve)"

	DiagReading   = "Lendo o estado da máquina (só leitura; pode levar até um minuto)…"
	DiagSaved     = "Relatório completo salvo em %s"
	DiagHint      = "Para ver todos os dados lidos: toolkit diagnostico --completo (ou --relatorio arquivo.txt)."
	ErrDiagReport = "não deu para gravar o relatório em %s: %w"
)

// Textos do comando privacidade (só Windows).
const (
	PrivShort = "Reduz a coleta de dados do Windows (mostra o plano antes, guarda backup e desfaz)"
	PrivLong  = "Mostra o estado de privacidade do Windows e os dois perfis (basico e recomendado) SEM alterar nada. Para aplicar use `toolkit privacidade aplicar --perfil basico|recomendado`: o plano (valor atual -> novo) é mostrado antes, o valor original de cada item é guardado em backup e `toolkit privacidade desfazer` restaura tudo. O perfil recomendado mexe em política, serviços e tarefas e exige um terminal aberto como administrador; o toolkit nunca eleva sozinho. Só existe no Windows."

	PrivApplyShort = "Aplica um perfil de privacidade (mostra o plano e pede confirmação)"
	PrivApplyLong  = "Calcula o plano do perfil, mostra o que muda (valor atual -> novo) e o que exige administrador, e pede confirmação [s/N] (sem terminal exige --yes). Antes de mudar qualquer coisa grava o backup. Use --dry-run para só ver o plano. Sem administrador, o que exige administrador NÃO é aplicado: o comando recusa antes de mudar qualquer coisa (ou use --somente-usuario)."
	PrivUndoShort  = "Desfaz a privacidade aplicada (restaura do backup)"
	PrivUndoLong   = "Mostra o que será restaurado a partir do backup e restaura. Sem backup, não há o que desfazer."

	FlagPrivPerfil = "perfil a aplicar: basico (só o que é do usuário) ou recomendado (inclui política, serviços e tarefas; exige administrador)"
	FlagPrivDryRun = "só mostra o plano, sem mudar nada"
	FlagPrivYes    = "não pergunta: aplica direto depois de mostrar o plano"
	FlagPrivUser   = "aplica só os itens do usuário (sem administrador) e ignora os que exigem administrador"

	PrivOnlyWindows   = "o comando privacidade só existe no Windows (este sistema é %s); nada foi alterado"
	PrivStateHeader   = "Estado de privacidade do Windows (só leitura, nada foi alterado)"
	PrivProfileSep    = "=== perfil %s ==="
	PrivStateHint     = "Para aplicar: toolkit privacidade aplicar --perfil basico|recomendado (use --dry-run para só ver o plano). Para desfazer: toolkit privacidade desfazer."
	PrivNeedPerfil    = "informe o perfil: --perfil basico ou --perfil recomendado"
	PrivBadPerfil     = "perfil %q desconhecido: use basico ou recomendado"
	PrivNothingToDo   = "Nada a fazer: tudo já está como o perfil manda."
	PrivDryRunDone    = "--dry-run: nada foi alterado."
	PrivUserOnlyNote  = "--somente-usuario: os itens que exigem administrador foram ignorados (ficam para uma execução num terminal como administrador)."
	PrivNeedsAdmin    = "este plano tem itens que exigem administrador e este terminal não é administrador: nada foi alterado. Abra o terminal como administrador (botão direito, \"Executar como administrador\") e rode o mesmo comando de novo, ou use --somente-usuario para aplicar só o que é do usuário. O toolkit nunca eleva sozinho"
	PrivUndoNeedsAdm  = "o backup tem itens que exigem administrador e este terminal não é administrador: nada foi alterado. Abra o terminal como administrador e rode `toolkit privacidade desfazer` de novo"
	PrivNeedsYes      = "sem terminal para confirmar: nada foi alterado; rode de novo com --yes (depois de conferir o plano com --dry-run)"
	PrivConfirmApply  = "Aplicar estas mudanças? O valor original de cada item será guardado em backup."
	PrivConfirmUndo   = "Restaurar estes itens do backup?"
	PrivCancelled     = "Cancelado: nada foi alterado."
	PrivApplied       = "Aplicado: %d mudança(s). Backup em %s. Para desfazer: toolkit privacidade desfazer."
	PrivAppliedPart   = "Aplicadas antes da falha: %d mudança(s). O backup em %s guarda os valores originais; `toolkit privacidade desfazer` restaura."
	PrivRestored      = "Restaurado: %d item(ns). O backup foi removido."
	PrivNoBackup      = "Não há backup de privacidade: nada a desfazer."
	PrivUndoNothing   = "O backup existe, mas tudo já está como antes: nada a restaurar (o backup foi removido)."
	PrivUndoHeader    = "O que será restaurado (valor atual -> original):"
	PrivRestoredPart  = "Restaurados antes da falha: %d item(ns); o que sobrou continua no backup."
	PrivPendingFormat = "  ! %s: %s -> %s"
	PrivUndoLine      = "      desfazer: %s"
	PrivAdminTag      = " [exige administrador]"
	PrivMissing       = "Não existe nesta versão do Windows (ignorado): %s"
)
