# toolkit — índice

Ferramenta em Go para preparar máquinas Windows, Linux e macOS com um comando (catálogo YAML de apps, providers, receitas multi-etapa que sobrevivem a reboot, TUI Bubble Tea, bootstraps). Nome provisório, centralizado em `internal/buildinfo`.

**Estado atual:** proposta escrita e aguardando aprovação do Paulo. Fase 1 ainda não começou.

## Documentos
- `docs/briefing.md`: pedido original do Paulo (fases 1–6, requisitos). Não editar.
- `docs/proposta.md`: estrutura de pastas, interfaces Go, decisões, perguntas em aberto e fontes oficiais.

## Regras do projeto
- Trabalhar por fases do briefing e parar ao fim de cada uma para revisão.
- Nunca executar instaladores, winget, dism, apt ou reboot de verdade na máquina de desenvolvimento: usar o `exec.Runner` mockado (`internal/exec/exectest`) ou `--dry-run`.
- Nada de download sem SHA-256 e nenhum `curl | bash` de terceiros. Nada que contorne licenciamento.
- Dependências com versão exata no `go.mod` e `go.sum` commitado. Fora da lista do briefing, só com justificativa.
- Antes de entregar: `scripts/ci.sh` (gofmt, go vet nos 3 GOOS, golangci-lint, go test, cross-compile dos 6 alvos com CGO_ENABLED=0). O GitHub Actions está sem orçamento, então o gate é local.
- Manter este índice atualizado quando surgir doc novo em `docs/` ou mudar um fluxo.
