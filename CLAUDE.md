# toolkit — índice

Ferramenta em Go para preparar máquinas Windows, Linux e macOS com um comando (catálogo YAML de apps, providers, receitas multi-etapa que sobrevivem a reboot, TUI Bubble Tea, bootstraps). Nome provisório, centralizado em `internal/buildinfo`.

**Estado atual:** repositório **público** em `PiluVitu/toolkit`. Documentação base escrita. Proposta aguardando as respostas do Paulo às perguntas em aberto; a fase 1 ainda não começou. Licença a definir (não adicionar LICENSE antes da decisão).

## Documentos
- `README.md`: o que é, comandos planejados, plataformas, o que o toolkit não faz.
- `docs/arquitetura.md`: pastas, interfaces Go, decisões e fontes (derivado da proposta, sem as perguntas).
- `docs/catalogo.md`: formato do `catalog.yaml`, campos por provider, templates, exemplos.
- `docs/receitas.md`: formato das receitas, ciclo de reboot e retomada, exemplos.
- `SECURITY.md`: como reportar e as regras garantidas (hash obrigatório etc.).
- `CONTRIBUTING.md`: como adicionar um app ou receita.
- `docs/briefing.md`: pedido original do Paulo (fases 1–6, requisitos). Não editar.
- `docs/proposta.md`: proposta com as perguntas em aberto e fontes oficiais. Histórico; decisões fechadas vão para `docs/arquitetura.md`.

## Regras do projeto
- Repositório público: nada de dado de cliente, segredo, IP, e-mail ou referência a projetos privados em código, docs, testes ou mensagens de commit.
- Trabalhar por fases do briefing e parar ao fim de cada uma para revisão.
- Nunca executar instaladores, winget, dism, apt ou reboot de verdade na máquina de desenvolvimento: usar o `exec.Runner` mockado (`internal/exec/exectest`) ou `--dry-run`.
- Nada de download sem SHA-256 e nenhum `curl | bash` de terceiros. Nada que contorne licenciamento.
- Dependências com versão exata no `go.mod` e `go.sum` commitado. Fora da lista do briefing, só com justificativa.
- Antes de entregar: `scripts/ci.sh` (gofmt, go vet nos 3 GOOS, golangci-lint, go test, cross-compile dos 6 alvos com CGO_ENABLED=0). O gate é local; os workflows do GitHub Actions ficam em `workflow_dispatch` até a fase 1 estabilizar.
- Manter este índice atualizado quando surgir doc novo em `docs/` ou mudar um fluxo.
