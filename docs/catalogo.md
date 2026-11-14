# Catálogo de apps

> Formato decidido ([decisoes.md](decisoes.md)), implementado na fase 1 (`internal/catalog`) e, para a carga por URL, na fase 5 (`internal/cli` + `internal/download`). Detalhes de cada
> provider em [arquitetura.md](arquitetura.md#33-providers).

O catálogo padrão fica em `catalog/catalog.yaml` e vai embutido no binário. Um catálogo externo entra com
`--catalog`:

- `--catalog <arquivo>`: caminho local, aceito sem hash.
- `--catalog <url> --catalog-sha256 <hash>`: URL só com o SHA-256 do arquivo (64 hex). Sem o hash, ou com hash malformado, é recusada **antes** de qualquer
  requisição. Vale para `toolkit list` e `toolkit install`. A URL segue a mesma política do provider `url` (https, host público, redirects revalidados, sem proxy
  de ambiente), o arquivo é lido em memória até 1 MiB e o hash é conferido **antes** de o YAML ser lido; hash divergente = nada é carregado. Erros de YAML citam
  `host/caminho:linha:coluna`. `--catalog-sha256` com arquivo local continua sendo erro.
- Por padrão o externo é **mesclado** sobre o embutido, e um app com o mesmo id substitui o original.
  Com `--catalog-replace`, o externo substitui o embutido inteiro.

## Estrutura

```yaml
apps:
  <id-do-app>:
    name: <nome exibido>
    category: <categoria>
    note: <aviso curto>          # opcional: texto puro mostrado no plano, na Revisão e no detalhe (ver Aviso)
    versions: true | [lista]     # opcional: habilita a seleção de versão (ver Versões)
    windows: [<instalador>, ...] # em ordem de preferência
    macos:   [<instalador>, ...]
    linux:   [<instalador>, ...]
    any:     [<instalador>, ...] # usado quando não há lista para o SO atual
    detect:                      # opcional: como achar o app já instalado FORA do gerenciador
      macos:   { app_bundle: ..., bundle_id: ... }
      windows: { display_name: ... }
      linux:   { desktop_id: ..., flatpak_id: ..., snap: ... }
```

- **id**: minúsculas, números e hífen (`^[a-z0-9][a-z0-9-]*$`). É o nome usado na CLI
  (`toolkit install discord`) e nas receitas (`install: [discord]`).
- **category**: uma das categorias fixas (ver [Categorias](#categorias)).
- **Ordem**: o toolkit percorre a lista do SO atual e usa o primeiro instalador cujo gerenciador existe na
  máquina. Se o pacote não tiver instalador aplicável (por exemplo, sem build para arm64 no winget), ele
  tenta o próximo da lista.
- **Validação**: na carga, com erro no formato `catalog.yaml:42:7: app "discord": provider "winget"
  exige "id"`. Chave desconhecida também é erro (pega `pakage:` no lugar de `package:`).

## Aviso (`note`)

Campo opcional por app: um texto curto (até 600 caracteres, em pt-BR, **sem template** `{{ }}`) que o toolkit mostra **antes de
confirmar** a instalação: no plano do `toolkit install` (e do `--dry-run`) como `aviso (<id>): <texto>`, na lista de avisos da Revisão da TUI
e no painel de detalhe da aba Apps (seção Aviso). Vale a mesma regra de todo texto do catálogo: caractere de controle ou de formatação
(ESC, quebra de linha, bidi) é erro na carga, e na saída o texto ainda passa por `printable`. O aviso só sai quando o app vai ser instalado (não quando já está
instalado). É informação, nunca bloqueio nem configuração do app.

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
- Cada versão da lista segue o mesmo formato dos valores de template (ver Templates): letra ou número na frente, sem `-` inicial.

### Texto sem caractere de controle

Qualquer texto do arquivo (chaves e valores: `name`, versões, `args`...) com caractere de controle (ESC, BEL, quebra de linha, tab, DEL, C1), de formatação (Cf: bidi como U+202E e U+2066, zero width U+200B, BOM U+FEFF, ZWJ U+200D) ou separador de linha e de parágrafo (U+2028, U+2029) é erro de carga. Esses textos vão para o terminal: controle reescreveria a tela, bidi inverteria o nome (um app que parece outro) e U+2028 quebraria a linha da tabela. Efeito colateral aceito: emoji composto com ZWJ no `name` é recusado; acentos e emoji simples valem.

## Apps instalados fora do gerenciador (`detect`)

Sem isto, um app instalado à mão (o Discord que veio do `.dmg`, o programa do instalador do Windows) aparece como
"não instalado" e o gerenciador falha ao tentar instalar por cima (`brew`: "It seems there is already an App at
/Applications/Discord.app"). O bloco `detect` do app ensina o toolkit a achar essa instalação, **só por leitura**.

| SO | Campo | O que é | Como é lido |
|---|---|---|---|
| `macos` | `app_bundle` | nome do bundle (`Discord.app`) | existe como pasta em `/Applications` ou `~/Applications`; a versão vem de `Contents/Info.plist` (`CFBundleShortVersionString`, senão `CFBundleVersion`) |
| `macos` | `bundle_id` | `CFBundleIdentifier` (`com.hnc.Discord`) | se o nome não existir, olha o `Info.plist` dos `.app` dessas duas pastas (até 500 por pasta), para achar um app renomeado |
| `windows` | `display_name` | `DisplayName` do registro Uninstall (`Discord`) | um PowerShell de texto fixo (caminho absoluto, só leitura) lista `HKLM` e `HKCU`, visões 64 e 32 bits; a versão é o `DisplayVersion` |
| `linux` | `desktop_id` | arquivo `.desktop` (`discord.desktop`) | existe em `/usr/share/applications` ou `~/.local/share/applications` |
| `linux` | `flatpak_id` | id do flatpak | `flatpak list --app` (também vale quando o instalador do catálogo é o próprio flatpak: a segunda leitura é redundante, mas inofensiva) |
| `linux` | `snap` | nome do snap | `snap list <nome>` |

Regras:

- Cada campo só vale no SO dele (`macos: { snap: x }` é erro) e `any` não existe em `detect`. O bloco de um SO tem de ter
  ao menos um campo. Vale a mesma validação rigorosa do resto do arquivo: chave desconhecida ou repetida, texto com
  controle ou formatação (`rejectControl`), erro com `arquivo:linha:coluna`. `app_bundle` termina em `.app` e não tem
  `/`, `\` nem `..` (acentos e espaço valem); `bundle_id` e `flatpak_id` só usam letras, números e `. -` (`_` no flatpak);
  `desktop_id` termina em `.desktop`; `snap` é minúsculo com hífen; `display_name` é texto sem espaço nas pontas.
- Catálogo antigo, sem o bloco, continua válido e se comporta como antes.
- **Só é consultado depois que o gerenciador diz "não instalado"**: app instalado pelo gerenciador nunca vira "fora" (nem quando ele tem outra versão que a pedida). Com versão pedida (`app@versão`), o app de fora só conta como "nada a fazer" se a versão lida for a mesma; versão diferente ou ilegível é erro (`ErrExternalVersion`: remova o app antes), nunca um "já instalado" falso.
- O `display_name` casa sem diferenciar maiúsculas com o `DisplayName` do registro **depois de tirar do fim** os grupos
  entre parênteses e os tokens que são só versão: `7-Zip 24.08 (x64)` casa com `7-Zip`, `Mozilla Firefox (x64 pt-BR)`
  com `Mozilla Firefox`; `Git Extensions` **não** casa com `Git`. Escreva o nome base.
- A detecção é de melhor esforço: `plutil`, PowerShell, `flatpak` ou `snap` ausentes ou falhando viram "não
  detectado", nunca um erro que trave o install. Todo texto lido (plist, registro, saída de comando, nome de
  arquivo) passa por `textsafe` e é limitado em tamanho.
- Efeito: `toolkit install` **pula** o app com `já instalado fora do Homebrew em /Applications/Discord.app (versão
  0.0.350): nada a fazer` (nada é preparado nem instalado, o código de saída é 0 e o plano fica vazio); a aba Apps
  mostra `✓ instalado (fora do Homebrew)`; `toolkit list --installed` (e `--json`) traz o estado e o lugar. Não há
  `--reinstall` ainda (ver `docs/roadmap.md`): para trocar pela versão do gerenciador, remova o app antes.

Exemplo:

```yaml
discord:
  name: Discord
  category: comunicacao
  windows: [{ provider: winget, id: Discord.Discord }]
  macos:   [{ provider: brew, cask: discord }]
  linux:   [{ provider: flatpak, id: com.discordapp.Discord }]
  detect:
    macos:   { app_bundle: Discord.app, bundle_id: com.hnc.Discord }
    windows: { display_name: Discord }
    linux:   { desktop_id: discord.desktop, snap: discord }
```

## Apps do catálogo embutido

Navegadores: Firefox, Brave, Google Chrome. Escritório: LibreOffice. Utilitários: 7-Zip, PeaZip, RustDesk (acesso remoto, com aviso). Os ids vêm de páginas públicas (winget-pkgs, API do
Homebrew, Flathub); o que não foi confirmado fica de fora (PeaZip no macOS, LibreOffice por apt/dnf). Brave, LibreOffice e PeaZip no Linux entram só por
flatpak (apt/dnf exigiriam repositório de terceiros).

- **Chrome só no Windows.** No macOS o cask oficial `google-chrome` declara `sha256 :no_check` (nada a conferir: o projeto nunca instala sem hash) e no Linux o `com.google.Chrome` do Flathub é comunitário, não verificado pelo Google. Nos dois o Chrome fica **sem instalador** (o plano diz isso; nada é inventado). No Windows o winget confere o hash.
- **Chrome e hash divergente.** O instalador do winget aponta para uma URL do fabricante cujo conteúdo muda a cada versão. Se o manifesto ainda não acompanhou,
  a instalação falha com "hash divergente". É o comportamento correto: o toolkit nunca usa `--ignore-security-hash`. Tente de novo depois que o manifesto for
  atualizado.
- **RustDesk (acesso remoto, AGPL-3.0)** entra só com o `note:` de segurança e só com ids verificados em 2026-11-14: cask `rustdesk` (macOS) e flatpak
  `com.rustdesk.RustDesk` (Linux, verificado na Flathub pelo site do fabricante). **Windows: sem instalador**, porque o id do winget não existe no winget-pkgs
  (a opção por `github-release` com hash fixado exigiria um PR por versão e ficou de fora). A versão mínima sem CVE alto ou crítico conhecido sem correção é a **1.5.0**
  (NVD, 2026-11-14; lista dos CVEs em `docs/plano-seguranca-e-tui.md`). O toolkit instala a versão **atual** do gerenciador e **não confere** a versão instalada
  (`SupportsVersion` do brew e do flatpak sempre recusa fixar versão): o aviso diz a mínima e manda conferir no app, nunca afirma que a instalada cumpre.
  Quem mexer na entrada deve reconferir o NVD no mesmo PR.
- Licenças de Brave, LibreOffice e PeaZip ainda a confirmar na fonte de cada projeto (`docs/plano-seguranca-e-tui.md`, pendência 11).

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

`type` é um de `msi`, `exe`, `zip`, `tar.gz`, `pkg`, `dmg`, `deb`, `rpm` e `appimage`. Se ficar vazio, vem da
extensão do arquivo. `args` são os argumentos silenciosos de um `exe` ou `msi`; cada item tem de ser um
texto (use aspas em números e booleanos) e passa pela validação de templates como os outros campos.

### Regras da `url`

- Só `https`, sem usuário/senha (`user:senha@`), sem fragmento (`#`), sem espaço, barra invertida nem caractere de controle.
- O host é um nome público em minúsculas, com ao menos um ponto (IDN em punycode). **`localhost`, nomes de um
  só rótulo e IP literal (v4 e v6) são recusados**, para o catálogo não apontar para a rede local. Porta explícita só se for `443`.
- Variável de template só vale **depois do host** (no caminho ou na query), nunca no host.
- A URL é validada na carga (com os templates) e de novo depois da expansão (`catalog.CheckURL`).
- O `sha256` é obrigatório no provider `url`.

### Como o provider `url` instala

O arquivo é baixado (`internal/download`: https, até 5 redirects revalidados, só IP público, limite de tamanho),
o `sha256` é conferido **antes** de qualquer uso (divergência = `ErrHashMismatch`, sem retry) e só então roda o instalador do `type`:

| `type` | SO | O que roda |
|---|---|---|
| `msi` | Windows | `msiexec.exe /i <arquivo> /qn /norestart <args>` (3010 e 1641 = reiniciar; exige administrador) |
| `exe` | Windows | `<arquivo> <args>`; `args` é **obrigatório** (sem eles o instalador abriria a tela interativa) |
| `pkg` | macOS | `installer -pkg <arquivo> -target /` (`sudo -n`) |
| `dmg` | macOS | monta, instala o `.pkg` ou copia o `.app` (exatamente um dos dois na raiz) para `/Applications`, desmonta |
| `deb` | Linux | `apt-get install -y --no-remove <arquivo>` (`sudo -n`) |
| `rpm` | Linux | `dnf install -y <arquivo>` (ou `rpm -U` sem dnf; `sudo -n`) |
| `zip`, `tar.gz` | todos | extrai em `<pasta de apps do usuário>/<id do app>` (Windows `%LOCALAPPDATA%\toolkit\apps`, Linux `$XDG_DATA_HOME/toolkit/apps` ou `~/.local/share/toolkit/apps`, macOS `~/Library/Application Support/toolkit/apps`). Recusa caminho absoluto, `..`, link simbólico, dispositivo e entrada repetida; a extração é feita num diretório de staging e só então substitui a versão anterior. **Não** põe nada no PATH |

`args` só valem para `msi` e `exe` e vão sempre **depois** do arquivo. `appimage` ainda não é suportado. O `--dry-run` mostra o
download (`baixar <url> (sha256 ..., até N MiB)`) e os comandos como serão executados, sem baixar nem executar nada.

Limites: o provider `url` não sabe se o app já está instalado (`IsInstalled` é sempre "não": rodar de novo reinstala);
só existe a versão da URL (para `app@versão`, use `{{version}}` na URL; o `sha256` fixado cobre UMA versão, as outras falham no hash);
o proxy do ambiente não é usado.

### Como o provider `github-release` instala

`repo` (`dono/repositório`) e `asset` (glob de um nome de arquivo, ex.: `tool_*_{{os}}_{{arch}}.tar.gz`; sem barras) escolhem o arquivo da release
mais recente (rascunho e pré-release não valem). `toolkit install app@1.2.3` escolhe a tag `v1.2.3` (ou `1.2.3`). O glob tem de casar com **exatamente um**
asset (zero = não encontrado, vários = erro de ambiguidade). O tipo vem de `type` ou da extensão do glob; depois a instalação é a da tabela acima.

O hash do asset vem, nesta ordem (decisão 18):

1. `sha256` fixado no catálogo: é a autoridade (a API e os checksums nem são consultados);
2. o arquivo de checksums da própria release (`checksums.txt`, `SHA256SUMS`, `sha256sums.txt`, `*_checksums.txt`, `<asset>.sha256`);
3. o campo `digest` do asset na API (`sha256:...`).

Se (2) e (3) existem e divergem, a instalação é recusada. Sem nenhum dos três, recusa **antes de baixar o asset**. Só o pin do catálogo é independente do GitHub (checksums e
`digest` vêm do mesmo canal da release): para quem precisa de mais que integridade de transporte, fixe o `sha256`. A assinatura dos checksums (cosign, minisign, gpg) **não** é verificada ainda.

A URL do asset vem da API mas só vale se for `https://github.com/<repo>/releases/download/...`; os redirects para a CDN do GitHub passam pela mesma checagem de
URL do `url`. A API aceita `GITHUB_TOKEN` (opcional, contra o limite de requisições): ele é enviado **só** ao `api.github.com`, nunca aos downloads, e fora do formato de token
(letras, números, `_ . -`) a execução é recusada. O `--dry-run` consulta a API e o arquivo de checksums (leituras) e mostra o asset e o hash que seriam usados, sem baixá-lo.
`IsInstalled` é sempre "não" (como no `url`).

### Chaves repetidas e limites

Chave repetida em qualquer nível (topo, app, instalador, `provider`) é erro: a última não vence em silêncio.
Âncoras, aliases e a chave de merge (`<<`) do YAML não são aceitos. O arquivo tem no máximo 1 MiB e o catálogo
no máximo 2000 apps. Ao mesclar um catálogo local (`--catalog`) sobre o embutido, os ids substituídos são avisados.

## Templates

Campos de texto aceitam variáveis entre chaves duplas. Variável desconhecida é erro na carga. Na expansão, todo valor
tem de casar com `^[A-Za-z0-9][A-Za-z0-9._+~-]*$` (nunca começa com `-`, e sem `..`), e a versão pedida tem de estar em `versions` (`App.VersionVars`).

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
