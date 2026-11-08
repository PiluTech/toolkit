# toolkit — índice

Ferramenta em Go para preparar máquinas Windows, Linux e macOS com um comando (catálogo YAML de apps, providers, receitas multi-etapa que sobrevivem a reboot, TUI Bubble Tea, bootstraps). Nome provisório, centralizado em `internal/buildinfo`.

**Estado atual:** repositório `PiluVitu/toolkit`, **privado por enquanto** (planejado para ser público). Licença MIT. As 22 perguntas da proposta foram respondidas, com todas as recomendações aceitas (`docs/decisoes.md`). Por decisão do Paulo, o projeto fica **só na documentação**: nenhuma fase começa até nova ordem.

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
- `PRODUTO.md`: texto público da página do catálogo (ver a seção abaixo).

## Página do catálogo (PRODUTO.md)

- `PRODUTO.md`, na raiz, alimenta a seção "Mais sobre" de https://pilutech.com.br/apps/toolkit: o build do repo `pilutech-sites` o puxa da main. O texto foi aprovado pelo Paulo (proposta em `pilutech-sites`, `docs/propostas-produto-md/toolkit.md`).
- Mudar o `PRODUTO.md` é mudar texto público: nada interno, sem preço, sem promessa, e só o que o produto já faz. Se o produto mudar (primeira versão publicada, fase nova), proponha o texto novo antes, em vez de editar direto.

## Regras do projeto
- Escrever como repositório público, mesmo enquanto ele está privado: nada de dado de cliente, segredo, IP, e-mail ou referência a projetos privados em código, docs, testes ou mensagens de commit.
- Trabalhar por fases do briefing e parar ao fim de cada uma para revisão.
- Nunca executar instaladores, winget, dism, apt ou reboot de verdade na máquina de desenvolvimento: usar o `exec.Runner` mockado (`internal/exec/exectest`) ou `--dry-run`.
- Nada de download sem SHA-256 e nenhum `curl | bash` de terceiros. Nada que contorne licenciamento.
- Dependências com versão exata no `go.mod` e `go.sum` commitado. Fora da lista do briefing, só com justificativa.
- Antes de entregar: `scripts/ci.sh` (gofmt, go vet nos 3 GOOS, golangci-lint, go test, cross-compile dos 6 alvos com CGO_ENABLED=0). O gate é local; os workflows do GitHub Actions ficam em `workflow_dispatch` até a fase 1 estabilizar.
- Manter este índice atualizado quando surgir doc novo em `docs/` ou mudar um fluxo.
