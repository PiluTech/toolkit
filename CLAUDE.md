# toolkit — índice

Ferramenta em Go para preparar máquinas Windows, Linux e macOS com um comando (catálogo YAML de apps, providers, receitas multi-etapa que sobrevivem a reboot, TUI Bubble Tea, bootstraps). Nome provisório, centralizado em `internal/buildinfo`.

**Estado atual:** repositório `PiluVitu/toolkit`, **privado por enquanto** (planejado para ser público). Licença MIT. As 22 perguntas da proposta foram respondidas, com todas as recomendações aceitas (`docs/decisoes.md`). **Fase 1 (esqueleto) em andamento** desde 2026-11-11, em PRs pequenos: PR1 módulo + Cobra `version` + gate local (feito), PR2 `exec` (feito), PR3 `platform`, PR4 `catalog`, PR5 `toolkit list`.

## Documentos
- `README.md`: o que é, comandos planejados, plataformas, o que o toolkit não faz.
- `docs/arquitetura.md`: pastas, interfaces Go, decisões técnicas e fontes (derivado da proposta, com as decisões aplicadas).
- `docs/decisoes.md`: as 22 decisões sobre a proposta, uma por linha, com link para onde cada uma está detalhada.
- `docs/catalogo.md`: formato do `catalog.yaml`, campos por provider, templates, exemplos.
- `docs/receitas.md`: formato das receitas, ciclo de reboot e retomada, exemplos.
- `SECURITY.md`: como reportar e as regras garantidas (hash obrigatório etc.).
- `CONTRIBUTING.md`: como adicionar um app ou receita.
- `docs/briefing.md`: pedido original do Paulo (fases 1–6, requisitos). Não editar.
- `docs/proposta.md`: proposta original com as perguntas (todas respondidas). Histórico; não editar além da marcação de respondidas.
- `LICENSE`: MIT, Copyright (c) 2026 PiluTech.

## Código (fase 1)
- `cmd/toolkit/main.go`: só chama `cli.Execute`. `internal/cli`: comandos Cobra (hoje só `version`). `internal/buildinfo`: nome, versão, commit e data (via `-ldflags -X`). `internal/msg`: strings de UI em pt-BR.
- `internal/exec`: `Runner` (Run/LookPath), `OSRunner` (exit code != 0 não é erro; `Timeout` vira `ErrTimeout`; `Elevate` = `sudo --` no Unix, `ErrNotElevated` no Windows sem token), `DryRunRunner` (executa leitura, só registra `Mutates`; `Planned()`). `internal/exec/exectest.FakeRunner`: respostas por linha de comando (`On`, `WithPath`, `Exit`), comando sem roteiro falha o teste. Testes do `OSRunner` só re-executam o binário de teste (helper process), nunca comando do sistema.
- Toolchain: Go 1.27.3 (o `go.mod` baixa sozinho via `GOTOOLCHAIN`) e golangci-lint v2.15.0 em `~/.local/bin` (SHA-256 conferido). Config em `.golangci.yml` (sem `misspell`: ele é só inglês e acusa pt-BR).
- Gate: `scripts/ci.sh` (ou `make ci`). Heavy pelo build-slot: `~/.maestri-local/build-slot.sh ./scripts/ci.sh`. `.github/workflows/ci.yml` só em `workflow_dispatch`.

## Regras do projeto
- Escrever como repositório público, mesmo enquanto ele está privado: nada de dado de cliente, segredo, IP, e-mail ou referência a projetos privados em código, docs, testes ou mensagens de commit.
- Trabalhar por fases do briefing e parar ao fim de cada uma para revisão.
- Nunca executar instaladores, winget, dism, apt ou reboot de verdade na máquina de desenvolvimento: usar o `exec.Runner` mockado (`internal/exec/exectest`) ou `--dry-run`.
- Nada de download sem SHA-256 e nenhum `curl | bash` de terceiros. Nada que contorne licenciamento.
- Dependências com versão exata no `go.mod` e `go.sum` commitado. Fora da lista do briefing, só com justificativa.
- Antes de entregar: `scripts/ci.sh` (gofmt, go vet nos 3 GOOS, golangci-lint, go test, cross-compile dos 6 alvos com CGO_ENABLED=0). O gate é local; os workflows do GitHub Actions ficam em `workflow_dispatch` até a fase 1 estabilizar.
- Manter este índice atualizado quando surgir doc novo em `docs/` ou mudar um fluxo.
