# Catálogo de apps

> Formato decidido ([decisoes.md](decisoes.md)), implementado na fase 1 (`internal/catalog`; a carga por URL com `--catalog-sha256` ainda não). Detalhes de cada
> provider em [arquitetura.md](arquitetura.md#33-providers).

O catálogo padrão fica em `catalog/catalog.yaml` e vai embutido no binário. Um catálogo externo entra com
`--catalog`:

- `--catalog <arquivo>`: caminho local, aceito sem hash.
- `--catalog <url> --catalog-sha256 <hash>`: URL só com o SHA-256 do arquivo. Sem o hash, é recusada.
- Por padrão o externo é **mesclado** sobre o embutido, e um app com o mesmo id substitui o original.
  Com `--catalog-replace`, o externo substitui o embutido inteiro.

## Estrutura

```yaml
apps:
  <id-do-app>:
    name: <nome exibido>
    category: <categoria>
    versions: true | [lista]     # opcional: habilita a seleção de versão (ver Versões)
    windows: [<instalador>, ...] # em ordem de preferência
    macos:   [<instalador>, ...]
    linux:   [<instalador>, ...]
    any:     [<instalador>, ...] # usado quando não há lista para o SO atual
```

- **id**: minúsculas, números e hífen (`^[a-z0-9][a-z0-9-]*$`). É o nome usado na CLI
  (`toolkit install discord`) e nas receitas (`install: [discord]`).
- **category**: uma das categorias fixas (ver [Categorias](#categorias)).
- **Ordem**: o toolkit percorre a lista do SO atual e usa o primeiro instalador cujo gerenciador existe na
  máquina. Se o pacote não tiver instalador aplicável (por exemplo, sem build para arm64 no winget), ele
  tenta o próximo da lista.
- **Validação**: na carga, com erro no formato `catalog.yaml:42:7: app "discord": provider "winget"
  exige "id"`. Chave desconhecida também é erro (pega `pakage:` no lugar de `package:`).

## Categorias

Lista fixa definida no código. Categoria desconhecida é erro, para que um erro de digitação não crie uma
aba nova na TUI. Uma categoria nova é uma linha na lista do código.

`comunicacao`, `dev`, `navegadores`, `midia`, `utilitarios`, `seguranca`, `escritorio`, `jogos`,
`drivers`, `sistema`.

## Versões

- `versions: true`: só para id fixo. As versões vêm do gerenciador (winget, apt, dnf). Nos que não
  listam (brew, pacman, flatpak), a única opção é `latest`.
- `versions: ["3.13", "3.12", "3.11"]`: obrigatório quando o id tem `{{...}}`, porque o id muda com a
  versão e o gerenciador não sabe quais existem. A primeira da lista é a padrão. O validador recusa
  id com template sem essa lista.
- Sem `versions`, instala a versão padrão do gerenciador e não aceita `app@versão`.

## Campos por provider

| provider | Campos obrigatórios | Opcionais | Versões |
|---|---|---|---|
| `winget` | `id` | `source` (padrão `winget`) | lista do `winget show --versions` |
| `brew` | `formula` ou `cask` | | só `latest` (versões via fórmula versionada, ex.: `python@3.12`) |
| `apt` | `package` | | lista do `apt-cache madison` |
| `dnf` | `package` | | lista do `dnf list --showduplicates` |
| `pacman` | `package` | | só `latest` |
| `flatpak` | `id` | `source` (remote, padrão `flathub`) | só `latest` |
| `github-release` | `repo`, `asset` | `sha256`, `type`, `args` | releases do repositório |
| `url` | `url`, `sha256` | `type`, `args` | uma, a da URL |

`type` é um de `msi`, `exe`, `zip`, `tar.gz`, `dmg`, `deb`, `rpm` e `appimage`. Se ficar vazio, vem da
extensão do arquivo. `args` são os argumentos silenciosos de um `exe` ou `msi`.

## Templates

Campos de texto aceitam variáveis entre chaves duplas. Variável desconhecida é erro na carga.

| Variável | Exemplo |
|---|---|
| `{{os}}` | `windows`, `linux`, `darwin` |
| `{{arch}}` | `amd64`, `arm64` |
| `{{version}}` | `3.12.7` |
| `{{major}}` | `3` |
| `{{major_minor}}` | `3.12` |
| `{{toolkit_version}}` | versão do binário em execução |

Não há lógica nem funções dentro do YAML: é só substituição de texto.

## Exemplos

App simples, com alternativa no Linux:

```yaml
apps:
  discord:
    name: Discord
    category: comunicacao
    windows: [{ provider: winget, id: Discord.Discord }]
    macos:   [{ provider: brew, cask: discord }]
    linux:
      - { provider: flatpak, id: com.discordapp.Discord }
      - { provider: apt, package: discord }
```

Versão escolhida pelo usuário. Como o id muda com a versão, a lista de versões fica declarada (a primeira
é a padrão):

```yaml
  python:
    name: Python
    category: dev
    versions: ["3.13", "3.12", "3.11"]
    windows: [{ provider: winget, id: "Python.Python.{{major_minor}}" }]
    macos:   [{ provider: brew, formula: "python@{{major_minor}}" }]
    linux:   [{ provider: apt, package: "python{{major_minor}}" }]
```

```sh
toolkit install python@3.12
```

Quando o id é fixo e o gerenciador lista as versões, basta `versions: true`:

```yaml
  git:
    name: Git
    category: dev
    versions: true
    windows: [{ provider: winget, id: Git.Git }]
    macos:   [{ provider: brew, formula: git }]
    linux:
      - { provider: apt, package: git }
      - { provider: dnf, package: git }
      - { provider: pacman, package: git }
```

Release do GitHub, para qualquer SO. O hash vem do `checksums.txt` da release ou do `digest` que o GitHub
publica para cada asset. Sem nenhum dos dois, informe `sha256:` ou a instalação é recusada:

```yaml
  minha-ferramenta:
    name: Exemplo via GitHub
    category: utilitarios
    any:
      - provider: github-release
        repo: dono/repo
        asset: "*_{{os}}_{{arch}}.zip"
```

URL direta, com hash obrigatório:

```yaml
  exemplo-msi:
    name: Exemplo MSI
    category: utilitarios
    windows:
      - provider: url
        url: https://exemplo.com/download/app-1.2.3-x64.msi
        sha256: 3f5a...e9c1   # 64 caracteres hexadecimais
        args: ["/qn", "/norestart"]
```

Uma entrada `url` sem `sha256` não passa na validação do catálogo.
