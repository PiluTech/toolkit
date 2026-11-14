# TUI (fase 4)

Especificação da variante **A (abas e uma coluna)**, proposta pelo designer e adotada. As outras duas
variantes avaliadas: B (dois painéis, boa a partir de 120 colunas; em 80 o comando do winget ocupa cinco
linhas) e C (trilho lateral e fila fixa; sobram 13 linhas de lista contra 17 da A e há três regiões para
redimensionar). A A é a única que mostra o plano inteiro em 80 × 24 e funciona sem cor.

A TUI nunca é o único caminho: tudo existe sem ela (`toolkit list`, `install`, `--dry-run`, `--yes`,
`recipe list|run|status|reset`, `resume`, `--no-tui`, log em arquivo). `toolkit` sem argumentos abre a TUI
só com terminal; com `--no-tui`, sem terminal ou (fora do Windows) com `TERM` vazio ou `dumb`, mostra a ajuda. No Windows o
`TERM` não existe (cmd, PowerShell, Windows Terminal) e por isso não conta: basta um console interativo na entrada e na
saída. Em CI (`CI`, `GITHUB_ACTIONS`, `TF_BUILD` e semelhantes ligadas; vazio, `0` e `false` não ativam) o `toolkit` sem argumentos NUNCA abre a TUI sozinho: mostra a ajuda. `toolkit tui` abre a TUI de forma explícita (ignora o CI, é pedido do usuário; só recusa descritor que não é terminal) e, se não puder, diz por quê (no Git Bash/mintty use `winpty toolkit.exe tui`).

## Pacotes

- `internal/tui`: raiz (`Model`), interface `Tab`, tema, layout, ajuda. Bubble Tea **v2**
  (`charm.land/bubbletea/v2`), Lip Gloss v2 (`charm.land/lipgloss/v2`). Bubbles v2 (`charm.land/bubbles/v2`, v2.2.1)
  entra no `go.mod` junto com a primeira aba que o usa (textinput/viewport/spinner). Versões exatas no `go.mod`.
- `internal/textsafe`: `Printable`/`Line`, a limpeza de todo texto de fora antes de ir ao terminal.

## Áreas e seções (fase 3 do redesign, variante A)

A raiz tem **áreas**, cada uma com **seções** (as abas): `0 Início` (objetivos que levam às outras áreas, abaixo), `1 Instalar` (Apps, Receitas), `2 Máquina`
(Diagnóstico, Apps nativos), `3 Segurança` (Privacidade), `4 Rede` (Análise: seção que ainda não existe e aparece com a
explicação, em vez de sumir) e `5 Histórico` (Execuções, o Log). Tudo continua numa lista PLANA de seções (`Model.tabs`):
execução, seleção, `Reloader`, resultados e origens seguem por índice plano; `Model.areas` só agrupa os índices.
`NewWithAreas(o, []AreaDef)` monta o modo áreas (é o que `toolkit` abre, via `tui.RunAreas`); `New(o, tabs...)` é o modo
legado (uma área por aba, números 1 a N, Tab percorre todas, sem a linha 2) e segue valendo para os testes.

- Linha 1: `[n Área]` (ativa entre colchetes), à direita a versão ou `↑ v0.6.0 (u)` (`disponível` a partir de 100 colunas). A palavra
  `toolkit` só a partir de 100 colunas; abaixo de 78 os nomes curtos (`Iníc Inst Máq Seg Rede Hist`). Nome de área nunca leva
  reticências: se não cabe, usa o curto e por último tira o aviso de versão.
- Linha 2: `Área › [Seção]  Outra` e, à direita, `admin: sim/não` (`Options.IsAdmin`, a mesma detecção do diagnóstico), com
  `N marcados ·` antes a partir de 100 colunas. Linha 3: régua. O corpo fica com H−6 linhas.
- Abaixo de 24 linhas a linha 2 some e a seção vai na linha 1 (`[2 Máq › Diag]`); o corpo fica com H−5.
### Início (área 0): "O que você quer fazer?"

Seção interativa (`internal/tui/inicio_tab.go`, montada em `internal/cli/tui_inicio.go`): uma lista por OBJETIVO, cada um com
título, uma frase didática (quebra na largura, nunca leva reticências) e, quando vale, uma etiqueta de estado ou requisito
(símbolo + palavra, nunca só cor). Os sete objetivos: Instalar programas (1 Apps), Preparar a máquina com uma receita (1
Receitas), Ver a saúde da máquina (2 Diagnóstico), Remover apps nativos do Windows (2 Apps nativos; `! só Windows` fora do
Windows), Proteger a privacidade do Windows (3 Privacidade; `! só Windows` fora do Windows, `! pede administrador para alguns
itens` no Windows sem administrador), Analisar a rede (4 Análise; `○ em breve`) e Ver o que já rodou (5 Execuções). As etiquetas
usam só o que o cli já sabe (GOOS e `Options.IsAdmin`): nada é lido do sistema. A etiqueta vai à direita do título quando cabe,
senão numa linha própria. Com folga há uma linha em branco entre os itens (120x40); em 80x24 a lista compacta e cabe inteira.
Abaixo disso rola, mantendo o item do cursor inteiro na tela, e a régua mostra `↓ mais N linhas`.

- `↑↓`/`jk` movem (`g`/`G` vão ao primeiro/último), `Enter` abre a área e a seção do item, `1` a `7` vão direto ao item. A linha de
  estado diz para onde o Enter leva (`Enter abre 2 Máquina › Diagnóstico`). O Enter abre mesmo a seção que não existe no SO
  (ela explica o motivo).
- Contrato: a seção implementa `KeyClaimer` (`Claims(key)`): com a raiz em navegação normal (sem Revisão, execução, modal nem
  pergunta) Enter e `1`..`7` vão a ela ANTES da Revisão e da troca de área; em qualquer outro modo valem as regras da raiz (em
  execução `3` continua sendo a área Segurança, só leitura). Para navegar ela devolve `tui.GoToMsg{Area, Section}` e a RAIZ decide
  (`Model.goTo`): ignora o pedido durante Revisão, execução, modal, pergunta `[s/N]` ou seção que captura teclas
  (confirmação aberta); área inexistente é ignorada e seção inexistente abre a área na última seção visitada. O contrato `Tab`
  não mudou. Dentro do Início, `0` fica no Início e `8`/`9` não fazem nada; as demais áreas se abrem pelo Enter ou pelo número do item.
- Teste integrado (Lupa): com `PrivacyTab` e `NativeAppsTab` reais e backends falsos, uma confirmação aberta não é abandonada por
  `0`..`5`, Tab, Shift+Tab, setas nem `GoToMsg`, nada é aplicado antes do `s`, o ID aplicado é o do plano exibido, e depois de
  cancelar Tab e os números voltam a funcionar.

- `0` a `5` vão para a área (na última seção visitada dela); Tab e Shift+Tab trocam de seção DENTRO da área (dão a volta, não
  saem). Números e Tab não agem durante confirmação, Revisão, execução (só leitura) e modal; seção capturando texto recebe tudo.

## Painel de detalhe (fase 7 do redesign, `internal/tui/detail.go`)

A partir de **110 colunas** a seção ativa que implementa `Detailer` (`tab.go`: `Detail(w, h int) string`) ganha um painel de
**44 colunas à direita**, separado da lista por uma coluna `│` (o tema não tem conjunto ASCII: o `│` vale sempre, em Mono
também). A lista (`View`) recebe `w - 45` colunas e o painel `DetailWidth` (44). Abaixo de 110 nada muda: o `Detail` nem é
chamado e os goldens de 60 e 80 ficam idênticos.

- A divisão é da RAIZ (`Model.detailBody`, chamada por `screen`): cabeçalho, régua, faixa de resultado, linha de estado e barra de
  teclas seguem em largura total. A faixa de resultado continua inteira acima do corpo com o painel aberto.
- O painel só existe na visão normal da lista: some na Revisão, na execução, com pergunta `[s/N]` aberta, na ajuda e no modal de
  versão nova (a raiz decide), e quando a seção devolve `""` (carregando, erro, vazio, não suportado, plano ou confirmação na tela,
  lista de versões aberta, busca sem resultado, visão Máquina do Diagnóstico).
- É SÓ LEITURA: mostra o item sob o cursor e nunca repete ação. Usa só o que a seção JÁ tem em memória (nada é lido do sistema
  para montá-lo). O que a seção não tem, o painel não inventa: diz como ver (ex.: "os nomes aparecem no plano (tecla p)").
- Texto quebra na largura (`wrap`, palavra comprida vira várias linhas com `\`), nunca reticências; tudo passa por `textsafe`; em
  Mono os rótulos de bloco vão em MAIÚSCULAS e todo estado leva símbolo e palavra. Se não cabe na altura, a última linha vira
  `↓ mais N linhas` (sem rolagem: o painel acompanha o cursor da lista).
- Conteúdo por seção: Apps (nome, id, categoria, estado e versão instalada, onde está quando instalado fora do gerenciador,
  marcação, gerenciadores do SO na ordem em que o toolkit tenta, o que cada um exige: sudo -n, UAC, sem root, SHA-256, versão fixa pela tecla v);
  Receitas e Diagnóstico (descrição, estado, administrador, reinício, etapas em ordem com estado e `reinicia`/`pode reiniciar`, erro da
  etapa; `>` na etapa em foco); Apps nativos (título, pacote, perfil básico/completo, instalado/ausente, escopo, como reinstalar: tecla i,
  só texto); Privacidade (o perfil sob o cursor: o que faz, contagem dos ajustes por estado, administrador, `só usuário`, como desfaz:
  backup e tecla d); Início (para que serve, destino do Enter, requisito).
- Testes: `detail_test.go` (divisão por largura e altura, `Detail` de cada seção com dados falsos, quebra e textsafe, painel que some)
  e `detail_golden_test.go` (goldens `detalhe_*_120x40` e `_110x30`). Os goldens de 120x40 que já existiam e têm seção com painel
  (Início, faixas de Apps/Privacidade/Apps nativos) foram regenerados.

## Estrutura da tela (H = altura)

Esta tabela é a do modo legado; no modo áreas o cabeçalho tem as 3 linhas descritas acima (ver "Áreas e seções").

| Linha | Conteúdo |
|---|---|
| 1 | ` toolkit  [1 Apps]  2 Receitas   3 Diagnóstico   4 Log` e, à direita, `↑ v0.2.0 disponível (u)` ou a versão atual |
| 2 | régua `─` |
| 3 a H−3 | corpo da aba (altura H − 5) |
| H−2 | régua, com `↓ mais N linhas` à direita quando há rolagem |
| H−1 | linha de estado |
| H | barra de teclas, sempre visível e contextual |

- Aba ativa em negrito e reverso **com colchetes**; as inativas levam espaços no lugar dos colchetes (não andam).
- Linha de estado: à esquerda id, nome e gerenciador do item sob o cursor (ou o caminho do log); à direita
  `Enter revisa N marcados`. As perguntas `[s/N]` também aparecem ali.
- Modais (versões, ajuda, notas da release) trocam o corpo inteiro; cabeçalho e teclas continuam.
- Colunas da aba Apps em 80: margem 1, cursor 1, marca 3, nome 24 (flexível, mínimo 12), estado 24, versão
  10, via 7, 2 espaços entre elas. Categorias são títulos em maiúsculas e negrito, sem linha vazia.
- Receitas e Diagnóstico: lista em cima (até 6 linhas), régua com título (`Etapas de <id>` e
  `N de M · exige administrador · reinicia 1 vez`), e embaixo as etapas ou o relatório, com rolagem.
- **Revisão** (tela cheia, "Revisão do plano"), nesta ordem: resumo ("Vai instalar 3 apps e rodar 1
  receita. Nada foi executado ainda." + SO, arquitetura, usuário), AVISOS (N), O QUE MUDA, PLANO,
  EXATAMENTE COMO SERÁ EXECUTADO. Avisos antes do plano. Botões `> [ Voltar ]     [ Executar ]` na linha
  de estado, foco inicial em Voltar.
- **Execução**: confirmar leva à aba Log: itens em cima (símbolo, nome, estado, duração) e embaixo o log do
  item sob o cursor. As outras abas ficam navegáveis só para leitura.
- **Resumo**: a mesma tela, parada. Título "Terminou com 1 falha · 2 ok · 1 falhou · 1 aguardando
  reinício", cursor na primeira falha, bloco "O QUE FAZER". O pedido de reinício fica na linha de estado
  como `[s/N]`, padrão N.

## Modelos e mensagens (Bubble Tea)

```
internal/tui
  Model (raiz): largura, altura, aba, modo, modal,
                Selecao (apps com id e versão, receitas; é da raiz porque atravessa abas),
                UpdateInfo (versão atual, nova, notas)
  ├─ apps.Model      lista agrupada, filtro (textinput)
  │   └─ versions.Model (modal)
  ├─ recipes.Model   lista + etapas (viewport)
  ├─ diag.Model      lista + relatório (viewport)
  ├─ logview.Model   itens + log (viewport), seguir o fim
  ├─ review.Model    plano (viewport) + botões
  ├─ notes.Model     (modal) notas da release
  └─ help.Model      (modal) teclas
modo:  Navegar → Revisar → Executar → Resumo → Navegar
modal: nenhum | versões | notas | ajuda | sair
```

A raiz trata tamanho, troca de aba, modal e modo e repassa o resto à aba ativa. Cada aba expõe `Update`,
`View(w, h)`, `Keys()`, `Status()`, `More()` e `Selected()` (interface `Tab`). As dependências entram pelo
mesmo `Deps` do CLI (testes com `FakeRunner`). A execução roda fora do `Update`, num comando que lê um
canal e devolve o próximo lote.

| Mensagem | Quem trata |
|---|---|
| `tea.WindowSizeMsg` | raiz: recalcula e repassa |
| `catalogLoadedMsg{apps, err}` | apps |
| `appStateMsg{id, instalado, versão, fora do gerenciador, err}` | apps; uma por app, concorrência limitada |
| `versionsLoadedMsg{id, versões, err}` | versions |
| `recipesLoadedMsg{receitas, estados, err}` | recipes e diag (separa por `kind`) |
| `diagReportMsg{id, etapas}` | diag |
| `planReadyMsg{plano, avisos, err}` | review; vem do passe com `DryRunRunner` |
| `runStartedMsg{itens}` | raiz: modo Executar, vai para a aba Log |
| `itemStartedMsg{i}`, `itemDoneMsg{i, resultado, err}` | logview |
| `logBatchMsg{i, linhas}` | logview; lote a cada 50 ms no máximo |
| `rebootPromptMsg{receita}` | raiz: pergunta na linha de estado |
| `runDoneMsg{resumo}` | raiz: modo Resumo; apps recarrega os estados |
| `updateAvailableMsg{atual, nova, notas}` | raiz; falha na consulta é silenciosa |
| `spinner.TickMsg` | raiz; só há tick enquanto algo carrega ou roda |

## Tokens (Lip Gloss v2, `internal/tui/theme.go`)

Cores ANSI por índice (nunca hexadecimal), claro/escuro escolhidos pela detecção do terminal
(`tea.BackgroundColorMsg`; padrão escuro).

| Token | Uso | Claro | Escuro | Atributo | Sem cor |
|---|---|---|---|---|---|
| Text | texto corrido | terminal | terminal | | igual |
| Muted | horários, dicas, cabeçalho de colunas | 8 | 7 | | igual ao texto |
| Accent | `[x]`, spinner, `↑` da faixa | 4 | 14 | | `[x]` já diz |
| Ok | `✓` | 2 | 10 | | `✓` + palavra |
| Warn | `!` e rótulo do aviso | 3 | 11 | negrito | `!` + palavra |
| Err | `✗` e `erro:` | 1 | 9 | negrito | `✗` + palavra |
| Cursor | linha sob o cursor | — | — | negrito no nome | `>` na coluna 2 |
| TabActive | aba ativa, botão em foco | — | — | negrito + reverso | colchetes; `>` antes do botão |
| Title | categorias, títulos de bloco | — | — | negrito | MAIÚSCULAS |

Três regras: cor SÓ no símbolo (✓ ✗ ! ○ [x], o spinner), nunca na palavra de estado nem na frase (a palavra sai na cor padrão do terminal: as 16 cores ANSI do usuário não chegam a 4,5:1 em vários temas, `docs/auditoria-a11y-l10n.md` A1; `Paint` em `theme.go`); nenhum fundo além do reverso;
nada depende de negrito, reverso ou cor para ser entendido. Com `NO_COLOR` (`Options.Mono`) **nenhuma**
sequência de estilo sai e o layout é idêntico ao colorido (teste `TestMonoHasNoStyleAndSameLayoutAsColor`).

**Reduzir movimento:** `TOOLKIT_REDUCE_MOTION=1` ou `--no-motion` (no `toolkit` e no `toolkit tui`): o spinner vira o símbolo fixo `~` nos itens (sempre com a palavra `executando`, `verificando` ou `em andamento`) e, no estado Carregando, o `…` fixo com a palavra `Carregando` e nenhum quadro é agendado, então a tela só muda quando o estado muda (nada é redesenhado a cada 100 ms). Sem a opção o spinner é um caractere de 4 quadros a 10 por segundo.

Símbolos (sempre com a palavra do estado): `✓` instalado/feito (`✓ instalado (fora do Homebrew)` quando o app já veio de fora do gerenciador, como o .dmg no macOS; em coluna estreita vira `✓ instalado (fora)`), `✗` falhou, `!` aviso/aguardando
reinício, `○` pendente/não instalado/na fila, `-` pulado/sem instalador, `| / - \` verificando/executando,
`[x] [ ]` marca, `(•) ( )` escolha única, `▾` tem lista, `↑` versão nova, `↓` há mais. Conjunto ASCII
atrás de `--ascii`: depois.

Bordas: réguas `─`; só os modais têm caixa (`lipgloss.NormalBorder()`, até 46 de largura). Movimento: só o
spinner (4 quadros, 100 ms). Troca de aba, modal e cursor são instantâneos; o log entra em lotes de 50 ms.

## Estados padronizados (fase 5 do redesign, `internal/tui/stateview.go`)

Toda tela que não é a lista normal tem UMA forma, de cima para baixo: **título** (símbolo e palavra na frente), **uma frase** do que houve, **passos** numerados (`1. ...`) e, no erro e na recusa, a linha **`Nada foi alterado.`**. Cada função devolve exatamente `h` linhas de até `w` colunas, quebra nos espaços (nunca corta com `…`), passa todo texto de fora por `textsafe` e, com pouca altura, abandona primeiro o que vale menos: título, frase, 1ª linha do motivo, `Nada foi alterado.`, passos, resto do motivo, linha em branco. Cor só no símbolo; em Mono só símbolo e palavra.

| Função | Título | Frase | Onde aparece |
|---|---|---|---|
| `StateLoading(th, w, h, what, frame, hints...)` | `\| Carregando` (spinner de 4 quadros); com `--no-motion` `… Carregando` fixo | `Lendo <what>…`; `hints` entram como linhas sem número (ex.: `Esc cancela.`) | Apps (catálogo), Receitas e Diagnóstico, Máquina (com `Esc cancela.`), Privacidade, Apps nativos |
| `StateError(th, w, h, what, err, steps...)` | `✗ Erro` | `Não deu para ler <what>.` + `Motivo: <err>` (o erro pode ter várias linhas; vem escapado) | idem; Máquina leva `1. Tente de novo (c).`; Apps leva `1. Corrija o arquivo ou rode sem --catalog.` |
| `StateNeedsAdmin(th, w, h, what, steps...)` | `! Precisa de administrador` | `<What>: precisa de administrador e este terminal não é. O toolkit nunca eleva sozinho.` | passos por sistema em `AdminSteps(goos, cmd)`: Windows `Botão direito no Terminal > Executar como administrador`, Linux/macOS `sudo toolkit <cmd>`. Nenhuma aba substitui o corpo por ele hoje (o administrador só barra a ação, que mostra a faixa de resultado com as mesmas linhas: `adminResultLines`) |
| `StateUnsupported(th, w, h, what, reason, osName)` | `i Não disponível neste sistema` | `<What>: só existe no Windows (este sistema é <SO>).` + o motivo por sistema | Privacidade e Apps nativos fora do Windows |
| `StateEmpty(th, w, h, what, steps...)` | `i Nada por aqui` | a frase inteira (ex.: `Nenhuma receita para este sistema.`) + o que fazer | Apps sem instalador, Receitas e Diagnóstico vazios, Máquina ainda não lida, Histórico/Execuções (`Nada rodou nesta sessão.`) |
| `StateSoon(th, w, h, what, steps...)` | `i Ainda não existe nesta versão` | a explicação | Rede/Análise (`InfoTab`) |
| `StateTooSmall(th, w, h, needW, needH)` | `i Terminal pequeno` | `A interface precisa de 60×16 e este tem LxA.` | raiz, abaixo do mínimo; `q` e Ctrl+C continuam saindo |

O vazio de uma BUSCA (`Nada com "xyz". Esc limpa a busca.`) e os estados por item (`✗ não deu para verificar`, spinner por linha) continuam inline na lista: a lista já é navegável. A faixa de resultado (fase 2) e a confirmação (fase 1) não mudaram.

## Estados por aba

| Aba | Carregando | Vazio | Erro |
|---|---|---|---|
| Apps | `\| Lendo o catálogo…`; depois spinner por linha, lista já navegável | "Nenhum app do catálogo tem instalador para <SO>. Veja: toolkit list --os". Busca: "Nada com "xyz". Esc limpa a busca." | Catálogo local inválido: `arquivo:linha:col` com rolagem e "Corrija o arquivo ou rode sem --catalog". Por app: `✗ não deu para verificar`, motivo na linha de estado, continua marcável. `- sem instalador`: não marcável; espaço mostra o porquê |
| Versões | `\| Consultando o <gerenciador>…`, `latest` já selecionável | só `latest`: "O gerenciador não informou outras versões." | "Não deu para listar as versões (motivo). latest continua valendo." |
| Receitas | `\| Lendo receitas e estado…` | "Nenhuma receita para este sistema." | "Não deu para ler o estado em <caminho>. Rode: toolkit recipe status". Receita falha: etapa, saída e "toolkit recipe reset <id> libera nova tentativa" |
| Diagnóstico | spinner no item; relatório etapa a etapa | "Nenhum diagnóstico para este sistema." Nunca rodou: "Enter roda." | etapa com `✗`, código de saída e saída como veio. Ferramenta ausente: `- pulado: smartctl não está instalado` |
| Log | itens e log ao vivo | "Nada rodou nesta sessão. Marque apps ou receitas e tecle Enter. Logs anteriores: <pasta>" | "! Não deu para gravar o log em arquivo; a execução continua e o log fica só na tela." |
| Revisão | `\| Lendo o estado do sistema…`; só Voltar até o plano chegar | "Nada a fazer: tudo já está instalado." Só Voltar | item não planejável: lista os erros, "Nada foi executado", só Voltar. Sem credencial do sudo: aviso com `sudo -v`, Executar continua disponível |

## Teclas

**Legenda única** (`internal/tui/keys.go`): a barra de teclas do rodapé (`Tab.Keys()`), a tela `?` e esta tabela nascem da mesma fonte. Mesma letra = mesmo sentido em todas as seções; o teste `TestVocabularioDeTeclasIgualEmTodasAsSecoes` percorre as seções reais (montadas por `NewWithAreas`) e reprova letra fora da legenda ou com outro rótulo. Rótulo com reticências (`aplicar…`, `desfazer…`, `remover…`) avisa que a tecla abre uma confirmação: nada muda antes de você confirmar.

Em qualquer lugar (globais):

| Tecla | O que faz |
|---|---|
| 0 a 5 (modo áreas; 1 a N no legado) | vai direto para a área: Início, Instalar, Máquina, Segurança, Rede, Histórico. No Início os números escolhem o objetivo (a seção os usa para si) |
| Tab / Shift+Tab | próxima seção da área / anterior (nunca sai da área; na Revisão e nas confirmações troca o botão em foco) |
| ↑ ↓ j k | move o cursor ou rola |
| g G / PgUp PgDn | início, fim, página |
| → l / ← h | foco nas etapas ou no relatório / volta à lista (Receitas, Diagnóstico) |
| / | busca onde há lista (letras e números vão ao filtro, inclusive q, j, 1; só Enter, Esc e setas são comandos); Esc limpa |
| Enter | abre, revisa os marcados (sem marcados, o item sob o cursor) ou aciona o botão em foco (começa em Cancelar) |
| Esc | volta um nível ou cancela: modal, busca, foco, Revisão, confirmação; na raiz não faz nada |
| x | fecha a faixa de resultado |
| ? | ajuda (abre e fecha); ↑↓ rolam; Esc, ? ou q fecham |
| q / Ctrl+C | sai; durante a execução pergunta "Interromper? O item atual é encerrado e a fila é descartada. [s/N]" |

Ações por sentido (a mesma letra em todas as seções):

| Tecla | Sentido | Onde |
|---|---|---|
| a | aplicar… (abre confirmação) | Privacidade |
| d | desfazer… (abre confirmação) | Privacidade |
| p | ver plano (só leitura) | Privacidade, Apps nativos |
| c | ler de novo (recarrega) | Diagnóstico, visão Máquina |
| r | remover… (abre confirmação) | Apps nativos |
| m | alterna o escopo ou a visão: `só usuário` (Privacidade), `todos os usuários` (Apps nativos, exige administrador), `máquina` / `receitas` (Diagnóstico) | Privacidade, Apps nativos, Diagnóstico |
| i | como reinstalar (só texto; nada é executado) | Apps nativos |
| b | marca o perfil básico | Apps nativos |
| t | marca todo o catálogo (o perfil completo) | Apps nativos |
| e | detalhes: o relatório traz também a saída original (crua) de cada etapa; a dica vira `e detalhes ✓` | Diagnóstico |
| v | lista de versões (app com `▾`) | Apps |
| Espaço | marca ou desmarca (bloqueado durante a execução, com aviso na linha de estado) | Apps, Receitas, Diagnóstico, Apps nativos |
| s | executa na Revisão, confirma uma confirmação e responde sim a `[s/N]` | Revisão, confirmações, perguntas |
| f | liga/desliga seguir o fim do log (rolar para cima desliga sozinho) | Execuções, execução |
| u | notas da versão nova (só quando há; Fechar é o padrão, "Atualizar" sai da TUI e roda `toolkit update`); indisponível durante a execução | global |

Decisões da fase 6: (1) em Apps nativos o perfil completo passou de `c` para `t` (o `c` é "ler de novo" em todas as seções) e o `u` "todos os usuários" passou para `m` (o `u` é global da versão nova e ganhava da seção); `b` continua o básico. (2) No Diagnóstico os detalhes passaram de `d` para `e` (o `d` é desfazer). (3) A dica de teclas da confirmação fica SÓ na barra; a linha de estado diz o estado ("Confirmação aberta: nada muda até você confirmar."), sem repetir as teclas. Comportamento seguro (confirmação, planos por ID, Esc nas preparações) inalterado.

### Tela de ajuda `?`

Gerada (`layout.go helpBody`, `keys.go`): três blocos, na ordem **Nesta seção: <nome>** (as teclas da seção ativa com uma frase cada, na ordem da barra, via `SectionHelper.HelpKeys()` ou, sem ele, as dicas da barra; vem primeiro para que em 80x24 as ações da seção não fiquem abaixo da dobra), **Em qualquer lugar** (as globais acima) e **Como ler a tela** (símbolos `✓ ! ✗ i ○ -`, faixa de resultado, `admin: sim/não`, confirmação com foco `>[ ]<`). Quebra na largura (nunca corta com reticências) e rola com ↑↓ j k, PgUp/PgDn, g/G; "↓ mais N linhas" na régua. Seção sem ação agora (Apps nativos e Privacidade fora do Windows, carregando ou com erro) mostra só uma frase ("Esta seção só existe no Windows; aqui não há ações.") em vez das teclas, como a barra. Cabe em 80x24 e 60x16; em Mono os títulos vão em MAIÚSCULAS. Goldens: `ajuda_{apps,maquina,priv}_{80x24,120x40,60x16}.golden`.

## Abaixo de 80 colunas e redimensionamento

| Tamanho | O que muda |
|---|---|
| 100 ou mais | como 80, com a coluna de estado em 32: cabe `✓ instalado (fora do Homebrew)` |
| 80 ou mais | layout completo; acima de 80 só o nome e as linhas de comando crescem |
| 70 a 79 | some a coluna VIA (vai para a linha de estado); o nome encolhe até 12 |
| 60 a 69 | abas `2 Rec` e `3 Diag`; VERSÃO junta ao nome (`Python 3.12`) ou ao estado (`✓ 2.43.0 → latest`); faixa `↑ v0.2.0 (u)`; barra de teclas só com mover, marcar, buscar, ajuda, sair |
| < 60 colunas ou < 16 linhas | tela única no estado padronizado `i Terminal pequeno` / "A interface precisa de 60×16 e este tem LxA." / "1. Aumente a janela." "2. Ou rode toolkit list / toolkit install ... sem a interface." / "q sair" (L×A = tamanho atual); cada linha quebra na largura da janela e, se faltar altura, o "q sair" sobrevive e o resto cede por prioridade; `q` funciona, a execução em andamento continua e redimensionar volta à interface normal |
| altura | só o corpo encolhe; listas rolam com `↓ mais N` |
| redimensionar | recalcula a cada `WindowSizeMsg`, mantém o cursor visível e o log no fim se estiver seguindo; comandos quebram com recuo de 5 e nunca são cortados |

## Pior caso de dados

Nome longo corta no fim com `…` (inteiro na linha de estado). Versão longa (`1:2.43.0-1ubuntu7.3+esm2~24.04.1`)
nunca corta: a coluna VERSÃO se alarga até a maior versão visível e as outras cedem na ordem via, depois nome
(piso de 12 colunas); se ainda assim não cabe (lista estreita com o painel de detalhe, 110 a 119 colunas), o
estado vira símbolo e versão, a coluna VERSÃO some e o nome cede até 6 colunas (inteiro no painel e na linha de
estado). Na Privacidade a descrição do perfil e o título do ajuste quebram na largura com recuo, e o aviso "Nunca
rodou" e o erro de etapa das Receitas também: texto de frase nunca leva reticências, só nome de item. 2000 apps: só as linhas
visíveis são desenhadas, o estado é lido primeiro para o que está na tela. Contagens no singular e plural
("1 marcado"/"3 marcados", "5 de 2000 apps" com filtro). Mais de 300 versões rolam no modal, `/` filtra.
Comando de 140+ colunas quebra em espaços com recuo (argumento sem espaço quebra com `\`). Receita de 14
etapas: painel rola, título "N de M". Linha de log com `\r` substitui a última linha. Erro de 300
caracteres: primeira frase na lista, texto inteiro no log do item.

## Segurança

0. Registro (sem ação): a `colorprofile` (dependência do Bubble Tea) consulta o `tmux` pelo PATH quando o terminal
   é tmux/screen, para detectar o perfil de cor. É leitura, mas é um binário achado pelo PATH.

1. Todo texto de fora (saída de gerenciador, nomes do catálogo, notas da release, erros) passa por
   `textsafe.Printable`/`Line` antes de ir à tela: uma sequência de escape no log redesenha o terminal.
   Testes com ESC, OSC e bidi por cada caminho.
2. A Revisão mostra o plano **exatamente como será executado**, vindo do mesmo passe do `toolkit install`
   (`DryRunRunner` + `OSRunner.Display`), sem um segundo planejador. Com `-EncodedCommand` mostra o script
   legível com a nota "(enviado como -EncodedCommand)"; teste que o texto mostrado é o script codificado.
3. A execução reaproveita `providers.InstallApp` e `recipes.Engine`; a TUI injeta o próprio `Prompter`.
   Nenhum teste roda comando real.

## Decisões (variante A confirmada pelo Paulo; as decisões 1 a 5 abaixo foram mantidas por ele)

1. **App com versões**: seguir o briefing (Espaço/Enter abre a lista já em `latest`, um Enter confirma).
   *Proposta do designer, não adotada:* Espaço marca com `latest` e `v` abre a lista.
2. Revisão: foco inicial em Voltar, `s` executa (igual ao `[s/N]` do CLI).
3. Ordem do lote: apps, depois receitas, a que reinicia por último.
4. Diagnóstico só leitura direto exigiria um campo novo no schema da receita (fase 3 já aprovada): **todo
   diagnóstico passa pela Revisão** (confirmado). Campo novo só com OK do Paulo.
5. `q` durante a execução pergunta e encerra o item atual, descartando a fila.
6. Conjunto ASCII atrás de `--ascii`, depois.

## Faixa de resultado (PR 2 do redesign, `internal/tui/result.go`)

O resultado de cada ação aparece na própria tela, entre o cabeçalho e o corpo, e PERSISTE até a pessoa teclar `x` (nem timeout nem troca de aba a fecham; a faixa é por aba).

- Interface opcional `Resulter{Result() *Result; Dismiss()}` (`tab.go`); `Result{Kind (ok|parcial|erro|info|pendente), Title, Lines}`. A raiz (`layout.go`) desenha a faixa quando a aba ativa tem resultado (próprio ou guardado pela raiz) e passa `h` menor ao corpo. Teclas: `x` fecha a faixa da aba ativa SÓ quando a aba não captura teclas (busca/texto/confirmação: `x` continua texto) e há faixa na tela; nenhuma outra tecla é consumida.
- Forma: linha do título (`✓ feito  <título>` + `[x] fechar`), até 4 linhas de detalhe QUEBRADAS (nunca cortadas), régua. Abaixo de 20 linhas de janela (ou se a faixa deixasse menos de 8 linhas ao corpo) vira uma linha só: símbolo, título e `[x] fechar`. Em Mono sem cor (cor só no símbolo, via `Paint`). Texto estável, sem animação; todo texto passa por `textsafe`.
- Privacidade e Apps nativos implementam `Resulter` (resultado de aplicar/restaurar/remover, backup e como desfazer). Execuções (Apps, Receitas, Diagnóstico): o dono é a RAIZ, não a aba (`Model.results`, por índice de aba): ao fechar a execução, `LogTab.Outcome()` resume e a raiz guarda uma cópia nas abas que tinham marcados (`origins`), com "Instalação concluída: N ok, M falharam", "Falharam: <nomes>" e "Detalhes: aba Log"; interrompida vira "Execução interrompida"; nova execução substitui a faixa anterior. Escolha do menor desenho: as abas de lista não conhecem a execução, então não ganham estado nem interface nova.

## Planner e Runner reais (PR 5c, feito)

`internal/cli/tui_exec.go`, ligados em `runRoot` por `tui.Options.Planner/Runner` (o `tui.run` chama `SetExec` com o
ctx que ele cancela ao sair).

- **`tuiPlanner`**: o MESMO passe do `toolkit install --dry-run` (`installPass` com `DryRunRunner`, leituras reais,
  mutações só registradas), sem segundo planejador. `Commands` por `exec.OSRunner{GOOS, Euid}.Display` (como root
  não mostra `sudo`); aviso de `sudo -n` por `OSRunner.UsesSudo`; flatpak/reinício em `Warnings`; app que falha no
  plano, versão fora da lista e app fora do catálogo viram `Problems` (nada é instalado, o resto continua
  planejado). Receitas e diagnósticos: `Changes` ("receita x (Nome): N etapas"), `Warnings` (administrador,
  reinício), pré-checagens do `recipe run` como `Problems` (reinício no Linux sem sudo, `requires_admin` no Windows
  sem terminal elevado) e `Commands` por etapa; script PowerShell LEGÍVEL com "(enviado como -EncodedCommand)"
  (teste: o texto exibido é exatamente o que `BuildCmd` codifica). `Env` e `Summary` ("Vai instalar N apps e rodar M
  receitas. Nada foi executado ainda."); `NothingToDo` quando tudo já está instalado.
- **`tuiRunner`**: fila apps -> receitas sem reinício -> receitas com reinício (`buildQueue`); app por `installOne`
  (dica `sudo -v` no log), receita por `buildEngine` com o `eventPrompter` no lugar do `ttyPrompter` (`prompter`
  nil = o do terminal); o `Confirm` do reinício vira `EventRebootPrompt` com `Reply` bufferizado (cap 1) e espera
  a resposta OU o ctx. Reinício pedido (ou recusado) para a fila (`ItemReboot`, o resto `ItemNotRun`). Cancelar o
  ctx encerra o item atual e marca o resto `ItemNotRun`. A saída dos comandos vai em `EventLog` por linha
  (`lineWriter`, `\r` preservado). Log em arquivo `<LogDir>/tui-<data>.log` (0600) só com o ciclo de vida (item,
  estado, erro), NUNCA a saída dos comandos; se não der para gravar, `EventWarn` e a execução continua.
- **Plano = execução (Crivo no #41)**: a elevação das receitas segue a MESMA regra da engine (`runAction`): só com
  `requires_admin`, fora do Windows e sem ser administrador, e vale para o `check` E para o `run` (o plano mostra
  `verificar: ...` e o comando; o aviso de `sudo -n` acende por `OSRunner.UsesSudo` de cada comando, inclusive de
  receita). Passo `install:` passa pelo MESMO passe do plano do install (`planTargets`) e mostra os comandos
  reais, com sudo.
- **Lista de permissão**: `Plan.Approved` (cada comando que muda o sistema, como o runner real o executa) viaja em
  `Selection.Approved` e o `approvedRunner` só deixa mutação passar se a linha exibida estiver nela (leituras
  passam); fora do plano = erro "comando fora do plano aprovado (mudou desde a Revisão)", sem executar. Falha
  fechado (sem plano, nenhuma mutação). O filtro envolve só o Runner dos passos e do `install:`
  (`buildEngine(..., wrap)`); o registro da retomada e o reinício seguem no Runner puro.
- **Log em arquivo**: só status + CLASSE do erro (`needs-admin`, `not-found`, `hash-mismatch`, `not-approved`,
  `cancelled`, `timeout`, `recipe-failed`, `error`...). A mensagem completa do erro (que os providers montam com a
  1ª linha do stderr, `providers.Snippet`, podendo ter URL ou token) só vai à tela. Teste com stderr de URL/token.
- **Limites documentados**: (a) o cancelamento (`q`+`s`) manda SIGTERM/SIGKILL ao grupo de processos do filho, mas
  um filho que roda como ROOT via `sudo` não é alcançado por um SIGKILL do usuário: o item pode terminar sozinho
  depois do cancelamento (o toolkit não o abandona, a engine registra o estado); (b) no Windows sem terminal
  elevado, receita `requires_admin` já é `Problem` no plano (Executar fica bloqueado na Revisão), e um passo
  elevado de app falha seguro por item (`ErrNotElevated`), sem afetar os demais.
- **Depois da execução** (`runClosedMsg`) as abas `Reloader` (Apps, Receitas e Diagnóstico) releem o estado.
- Pendente deste item: lotes de log a cada 50 ms (hoje cada linha é um evento) e o `RecipeMark.Reboots` (a ordem
  por reinício consulta o conjunto de receitas no Runner).

## Pendências fora da TUI (registradas aqui para a próxima sessão)

- **`toolkit update`** (depois da fase 4; entra antes da v0.1.0 se couber): baixa a última release do GitHub
  (`PiluTech/toolkit`), confere o SHA-256 pelo `checksums.txt` com o MESMO código do `internal/download` e do
  bootstrap (nunca instala sem hash), e troca o binário de forma atômica (escreve `<exe>.new` na mesma pasta com
  0755, confere de novo, renomeia por cima; no Windows renomeia o `.exe` em uso para `.old` e o apaga no
  próximo start). Flags: `--version vX.Y.Z`, `--dry-run`, `--check` (só diz se há versão nova). Recusa
  rodar de um binário que não é o instalado pelo bootstrap quando o destino não é gravável (sem `sudo`).
  Notas da release vêm da API do GitHub e passam por `textsafe`.
- **Aviso de versão nova** (FEITO, PR empilhado no `toolkit update`): faixa `↑ v0.2.0 disponível (u)` na TUI
  (`Options.Latest`, preenchida pelo cache; uma consulta em andamento chega por `Options.Updates` e preenche a faixa
  se o cache estava vazio) e UMA linha em stderr no fim dos comandos (nunca em stdout, nunca com `--json`, também
  quando o comando falha). A tecla `u` abre o modal com as notas (`Options.LatestNotes/LatestURL`, passadas por
  `textsafe`, rolam com ↑↓): Fechar é o padrão (Enter/Esc), `a` ou Tab+Enter em Atualizar sai da TUI
  (`tui.ErrUpdateRequested`) e roda `toolkit update`; durante a execução `u` só avisa que está indisponível.
  No máximo 1 consulta concluída por dia, cache em `os.UserCacheDir()/toolkit/update-check.json` (0600; só versão,
  data, `retry_after` e notas limpas), desligável por `--no-update-check` e `TOOLKIT_NO_UPDATE_CHECK=1`; também
  desligado com `CI`, stdout/stderr fora de terminal, build `dev`, `--json` e nos comandos `update`/`help`/
  `completion`. NUNCA bloqueia: a consulta roda em goroutine (timeout 4 s), o fim do comando a cancela sem esperar.
  A API sem token limita 60/h por IP (já deu 403): falha também vai para o cache (backoff de 3 h, 6 h no 403/429),
  usa `GITHUB_TOKEN` quando houver e não insiste. Só leitura e só no host padrão (mesmo `download`: https, host
  público, limite de tamanho). Texto em `internal/msg` (`UpdateNotice`, `FlagNoUpdateCheck`) e no `tui`, em pt-BR
  como os demais (a localização em en/es é o PR próprio do roadmap).
- Menores da fase 5 (PR à parte): lista completa de IPs reservados no `download`; dmg não aceitar EULA
  sozinho; conferir assinatura de pkg/dmg; nomes reservados e ADS no Windows.
- Release v0.1.0: `scripts/release.sh` pronto; assinatura/notarização macOS e `gh release` só no dia, com o
  Touch ID do Paulo. Smoke manual das fases 2 e 3 (e da TUI) em Windows 11, macOS e Linux ainda por fazer.

**Pendente de reinício.** Uma receita que espera reinício (`ItemReboot`) nunca vira "feito": a faixa usa o tipo `pendente` (`!`) e o título começa pela pendência ("Pendente de reinício: 1 aguardando reinício, 2 ok"), para a faixa compacta (menos de 20 linhas) não perdê-la.
