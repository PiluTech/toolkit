# Decisões

> As 22 perguntas da [proposta](proposta.md#4-perguntas-para-o-paulo-respondidas), respondidas pelo Paulo em
> 2026-10-31: todas as recomendações foram aceitas. Cada item aponta onde a decisão está detalhada.
> A fase 1 ainda aguarda aprovação para começar.

| # | Tema | Decisão | Onde |
|---|---|---|---|
| 1 | Versão com id em template | Quando o id tem `{{...}}`, o YAML declara a lista (`versions: ["3.13", "3.12"]`, a primeira é a padrão). `versions: true` só para id fixo, em que o gerenciador lista as versões. O validador exige a lista quando há template. | [catalogo.md](catalogo.md#versões) |
| 2 | Visibilidade do repositório | Planejado como público, no dono `PiluVitu`. Está **privado por enquanto**, por decisão do Paulo. O conteúdo continua escrito para ser público (nada de dado privado). | [arquitetura.md](arquitetura.md#39-ci-local) |
| 3 | URL dos bootstraps | Começa com os assets da release do GitHub (`.../releases/latest/download/win.ps1` e `unix.sh`). Depois, um domínio próprio redireciona para `/win` e `/sh`. | [README.md](../README.md) |
| 4 | Biblioteca YAML | `go.yaml.in/yaml/v3` (o `gopkg.in/yaml.v3` está arquivado). | [arquitetura.md](arquitetura.md#310-dependências-supply-chain) |
| 5 | Bubble Tea | v2 (`charm.land/bubbletea/v2`), com Bubbles e Lip Gloss v2. | idem |
| 6 | `golang.org/x/sys/windows` | Aceito, para checar o token elevado e chamar `ShellExecuteW runas`. | [arquitetura.md](arquitetura.md#27-elevação) |
| 7 | Go e ferramentas locais | Go 1.27.3 e golangci-lint v2.15.0, instalados com hash conferido (Go via checksum DB do `GOTOOLCHAIN`, golangci-lint pelo binário oficial com SHA-256). | [arquitetura.md](arquitetura.md#310-dependências-supply-chain) |
| 8 | `setup-inside` da receita `wsl-dev` | Sem `curl \| bash`: o Windows baixa o `unix.sh` da mesma versão do binário, confere o hash pelo `checksums.txt` e roda dentro do WSL via `/mnt/c`. | [receitas.md](receitas.md#setup-com-reboot-windows) |
| 9 | Retomada no Linux | No boot, headless: unit systemd de sistema rodando `toolkit resume --yes --no-tui`. | [receitas.md](receitas.md#ciclo-de-execução-e-retomada) |
| 10 | Retomada no macOS | LaunchAgent no login do usuário, abrindo o Terminal. | idem |
| 11 | Elevação no Windows | UAC só quando algo escolhido exige admin. O relançamento leva o plano escolhido na TUI. | [arquitetura.md](arquitetura.md#36-elevação) |
| 12 | Usuário da tarefa agendada | O próprio usuário que iniciou a receita (logon interativo, privilégio mais alto). Sem SYSTEM e sem autologon. | [arquitetura.md](arquitetura.md#35-retomada-por-so) |
| 13 | Anti-loop | 1 tentativa depois da retomada. Se falhar, para, desregistra a retomada e reporta. `recipe reset <id>` libera nova tentativa manual. | [receitas.md](receitas.md#ciclo-de-execução-e-retomada) |
| 14 | Usuário recusa o reboot | A retomada continua registrada, o estado fica `awaiting-reboot` e a receita continua no próximo boot ou logon. | idem |
| 15 | Categorias | Lista fixa no código. | [catalogo.md](catalogo.md#categorias) |
| 16 | `--catalog` | Caminho local livre. URL só com `--catalog-sha256 <hash>`. O externo é mesclado sobre o embutido (mesmo id substitui), e `--catalog-replace` troca o embutido inteiro. | [catalogo.md](catalogo.md) |
| 17 | Windows sem winget | O diagnóstico detecta e oferece o comando oficial `Add-AppxPackage -RegisterByFamilyName -MainPackage Microsoft.DesktopAppInstaller_8wekyb3d8bbwe`. O App Installer não é baixado por fora. | [arquitetura.md](arquitetura.md#33-providers) |
| 18 | Hash do `github-release` | Aceita o `checksums.txt` da release, o `digest` publicado pelo GitHub ou um `sha256` fixado no YAML. Sem nenhum dos três, recusa. | [catalogo.md](catalogo.md#exemplos) |
| 19 | dnf e pacman | Entram na fase 2, junto com winget, brew, apt e flatpak. | [arquitetura.md](arquitetura.md#33-providers) |
| 20 | Teste real no Windows | Smoke manual na VM Windows 11 do Paulo ao fim das fases 2 e 3, seguindo um roteiro entregue junto, sempre começando por `--dry-run`. | [arquitetura.md](arquitetura.md#39-ci-local) |
| 21 | Idioma | Identificadores e nomes de arquivo em inglês. Mensagens ao usuário, docs e comentários em pt-BR, com as strings de UI centralizadas num pacote. | [CONTRIBUTING.md](../CONTRIBUTING.md) |
| 22 | Identificadores e licença | Label do launchd e nome da tarefa agendada `dev.pilutech.<name>`, derivado do nome em build. Licença MIT (Copyright (c) 2026 PiluTech). | [LICENSE](../LICENSE) |

Também decidido: os commits mantêm o e-mail de autor atual.
