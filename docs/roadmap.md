# Roadmap depois da v0.1.0

Ordem combinada com o Paulo; um PR por vez, com revisão (Lupa/Crivo).

1. **PR 5c: Planner e Runner reais** (a TUI passa a executar): feito (`docs/tui.md`).
2. **`toolkit update`**: feito (`CLAUDE.md`, pacote `internal/update`). **Aviso de versão nova**: feito (PR empilhado
   no `toolkit update`; 1 consulta/dia, cache em `os.UserCacheDir()/toolkit`, desligável, nunca bloqueia; `u` na
   TUI abre as notas). Detalhes em `docs/tui.md`.
3. **Ícone** "Porca de comando" (variante A): **feito** (README, `assets/icon/`, PNG gerados do SVG por
   `scripts/gerar-icone.sh`; PNG do site em `assets/icon/toolkit-site.png`). Ainda não há modelo de notas de release
   no repositório para levar o ícone. Possível melhoria: `.ico`/`.syso` no exe do Windows (fora desta etapa).
4. **`toolkit --version`** (hoje só existe `toolkit version`).
5. **Menores da fase 5**: lista completa de IPs reservados no `download`, dmg sem EULA automático, assinatura de
   pkg/dmg, nomes reservados e ADS no Windows.
6. **Smoke manual** (`docs/smoke.md`) em Windows 11, macOS e Linux, e a validação do `win.ps1` e do PATH do usuário
   no Windows.

## Apps instalados fora do gerenciador (feito, com sobras)

Feito: bloco `detect` no catálogo, `internal/detect`, install pulando, aba Apps e `list --installed`
(`docs/catalogo.md`). Sobras, sem pressa:
- `--reinstall` (ou `--force`) no `install` para trocar a instalação de fora pela do gerenciador: não é barato nem
  seguro (o `brew install --cask` precisa de `--force` e sobrescreve o `.app` do usuário; no Windows seria
  desinstalar antes). Hoje o aviso manda remover o app e rodar de novo.
- No Windows cada app "não instalado" custa uma chamada ao PowerShell (~0,3 a 1 s); um cache da listagem do
  registro por execução (invalidado depois de uma instalação) resolve para o `list --installed` e a TUI.
- `.app` de versões versionadas (`python@3.12`) e instalações do python.org ficam sem `detect` até haver ids confirmados.
- Detecção de CLI no PATH (git, 7zip no macOS/Linux) não existe: só app com bundle, entrada do registro, .desktop,
  flatpak ou snap.

## Menores da revisão do #48 (registro)

- (c) Registro do Crivo: sem ação neste PR. O `realDetector` é um singleton por processo (o Runner da 1ª chamada
  vale); se um dia houver Runners com semântica diferente, passar o Runner a cada `Find`.

## Menores da revisão do `toolkit update` (#42, não bloqueiam)

- Assinatura/attestation dos artefatos: GitHub artifact attestations (provenance) no `release.yml` para os binários e o
  `checksums.txt`, e o `toolkit update` verificar a attestation além do SHA-256 (hoje o `checksums.txt` vem do mesmo
  canal da release e não é assinado).
- Tornar a lista de hosts do update configurável por política de empresa (proxy/espelho interno) sem abrir mão do
  SHA-256 pelo `checksums.txt`.
- A API do GitHub sem token limita 60 consultas/hora por IP: considerar resolver a última versão pelo redirecionamento
  de `releases/latest` (sem API) quando só a tag importa.

## Menores da revisão do PR 5c (não bloqueiam)

- (a) O `ReadOnly` do `Cmd` vem de quem o monta: um check (leitura) passa pela lista de permissão sem comparação.
  Reforço possível: o `approvedRunner` validar também que o `Cmd` ReadOnly não tem `Elevate` e que o nome está
  entre os comandos de leitura conhecidos.
- (b) O registro da retomada e o reinício usam o Runner puro (fora da lista de permissão); o reinício só acontece
  depois do `s` do usuário. Reforço possível: lista própria para esses dois comandos.
- (c) A lista de permissão compara pela linha EXIBIDA (`OSRunner.Display`), não pelo `Cmd` estruturado. Reforço
  possível: comparar nome + args + env + `Elevate` (e `Stdin` por hash).

## Padrões de produto da PiluTech (skill `padroes-produto-pilutech`)

### Acessibilidade da TUI (meta equivalente ao WCAG 2.2 AA)
Já cumprido: tudo por teclado (setas/jk, Tab, 1-4, Enter, Espaço, Esc, `?`, `q`; nada depende de mouse); nada
depende só de cor (todo estado tem símbolo E palavra, a aba ativa leva colchetes, o cursor leva `>`, o botão em
foco leva `>`); modo sem cor (`NO_COLOR`) com layout idêntico e testado; cores só pela paleta ANSI do terminal
(o contraste segue o tema do usuário, claro ou escuro); o único movimento é um spinner de um caractere que muda
10 vezes por segundo (abaixo do limiar de área do WCAG 2.3.1 para flash); com `TOOLKIT_REDUCE_MOTION=1` ou `--no-motion` ele vira um símbolo fixo
e nenhum quadro é desenhado; funciona em 60x16 e redimensiona; erros dizem o que houve e o que fazer.
Reduzir movimento (Fase 1): `TOOLKIT_REDUCE_MOTION=1` ou `--no-motion` (regra do `envOn`). Falta: teste automatizado de contraste para o par de cores escuro/claro dos tokens (medir, não estimar); verificar com leitor de tela (VoiceOver no
Terminal do macOS, NVDA no Windows Terminal) que a ordem de leitura da tela cheia faz sentido e que a linha de
estado e as perguntas `[s/N]` são anunciadas; anúncio de mudança de aba e de fim de execução.

### Localização (pt-BR, en, es)
Hoje todos os textos da CLI (`internal/msg`) e da TUI (`internal/tui`) estão em pt-BR no código. Fica para PR
PRÓPRIO: catálogo de textos por idioma (`pt-BR`, `en`, `es`, pt-BR de origem e padrão) com chaves estáveis,
idioma pelo ambiente (`LC_ALL`/`LANG`) com `--lang`/`TOOLKIT_LANG` para trocar, plural por regra do idioma,
datas/números pelo formatador, espaço para o texto crescer ~35% na TUI (colunas flexíveis e quebra por
grafema já existem), teste que falha se uma chave existir em um idioma e faltar nos outros. Termos técnicos,
nomes de comandos e de apps não se traduzem. As receitas e o catálogo são dados (nome/descrição por idioma
viriam como campos opcionais).
