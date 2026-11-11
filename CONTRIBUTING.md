# Como contribuir

O código está na fase 1 (esqueleto). Os passos de catálogo e receitas abaixo valem a partir das fases 1 e 3.

## Adicionar um app

1. Edite `catalog/catalog.yaml` seguindo [docs/catalogo.md](docs/catalogo.md).
2. Use ids oficiais: `winget search`, `brew info`, `apt-cache policy`, `flatpak search`.
3. Liste os instaladores em ordem de preferência para cada SO. Prefira gerenciador de pacotes a `url`.
4. Para `url`, o `sha256` é obrigatório. Calcule a partir do arquivo oficial (`sha256sum` ou
   `shasum -a 256`) e cite a página de download no PR.
5. Rode `toolkit list --catalog catalog/catalog.yaml` e `toolkit install --dry-run <id>` para conferir.

## Adicionar uma receita

1. Crie `recipes/<windows|linux|macos>/<id>.yaml` seguindo [docs/receitas.md](docs/receitas.md).
2. Dê um `check` a toda etapa que altera o sistema.
3. Rode `toolkit recipe run <id> --dry-run` e descreva no PR onde você testou de verdade (SO, versão,
   VM ou máquina física).

## Não será aceito

- Ativadores, cracks ou qualquer coisa que contorne licenciamento.
- Download sem SHA-256 ou `curl | bash` de terceiros.
- Dado pessoal ou de terceiros no catálogo, nas receitas ou nos testes (nomes, e-mails, IPs, tokens).

## Código

- Antes de abrir o PR: `scripts/ci.sh` (gofmt, go vet nos 3 SOs, golangci-lint, go test e
  cross-compile dos 6 alvos).
- Testes nunca executam instalador, gerenciador de pacotes ou reboot: use o `Runner` falso de
  `internal/exec/exectest`.
- Dependência nova só com justificativa no PR, versão exata no `go.mod` e `go.sum` commitado.
- Mensagens ao usuário, docs e comentários em pt-BR, com as strings de UI no pacote central de
  mensagens. Identificadores e nomes de arquivo em inglês.
- Ao contribuir, você concorda em licenciar sua contribuição sob a [licença MIT](LICENSE).
