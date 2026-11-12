# TUI (fase 4)

Especificação da variante **A (abas e uma coluna)**, proposta pelo designer e adotada. As outras duas
variantes avaliadas: B (dois painéis, boa a partir de 120 colunas; em 80 o comando do winget ocupa cinco
linhas) e C (trilho lateral e fila fixa; sobram 13 linhas de lista contra 17 da A e há três regiões para
redimensionar). A A é a única que mostra o plano inteiro em 80 × 24 e funciona sem cor.

A TUI nunca é o único caminho: tudo existe sem ela (`toolkit list`, `install`, `--dry-run`, `--yes`,
`recipe list|run|status|reset`, `resume`, `--no-tui`, log em arquivo). `toolkit` sem argumentos abre a TUI
só com terminal; com `--no-tui`, sem terminal ou com `TERM=dumb`, mostra a ajuda.

## Pacotes

- `internal/tui`: raiz (`Model`), interface `Tab`, tema, layout, ajuda. Bubble Tea **v2**
  (`charm.land/bubbletea/v2`), Lip Gloss v2 (`charm.land/lipgloss/v2`). Bubbles v2 (`charm.land/bubbles/v2`, v2.2.1)
  entra no `go.mod` junto com a primeira aba que o usa (textinput/viewport/spinner). Versões exatas no `go.mod`.
- `internal/textsafe`: `Printable`/`Line`, a limpeza de todo texto de fora antes de ir ao terminal.

## Estrutura da tela (H = altura)

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
| `appStateMsg{id, instalado, versão, err}` | apps; uma por app, concorrência limitada |
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

Três regras: cor só no símbolo e na palavra de estado, nunca na frase; nenhum fundo além do reverso;
nada depende de negrito, reverso ou cor para ser entendido. Com `NO_COLOR` (`Options.Mono`) **nenhuma**
sequência de estilo sai e o layout é idêntico ao colorido (teste `TestMonoHasNoStyleAndSameLayoutAsColor`).

Símbolos (sempre com a palavra do estado): `✓` instalado/feito, `✗` falhou, `!` aviso/aguardando
reinício, `○` pendente/não instalado/na fila, `-` pulado/sem instalador, `| / - \` verificando/executando,
`[x] [ ]` marca, `(•) ( )` escolha única, `▾` tem lista, `↑` versão nova, `↓` há mais. Conjunto ASCII
atrás de `--ascii`: depois.

Bordas: réguas `─`; só os modais têm caixa (`lipgloss.NormalBorder()`, até 46 de largura). Movimento: só o
spinner (4 quadros, 100 ms). Troca de aba, modal e cursor são instantâneos; o log entra em lotes de 50 ms.

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

| Tecla | Onde | O que faz |
|---|---|---|
| Tab / Shift+Tab | Navegar, Executar, Resumo | próxima aba / anterior (na Revisão troca o botão em foco) |
| 1 a 4 | idem | Apps, Receitas, Diagnóstico, Log |
| ↑ ↓ j k | tudo | move o cursor ou rola |
| g / G / PgUp / PgDn | listas e rolagens | início, fim, página |
| → l / ← h | Receitas, Diagnóstico | foco nas etapas ou no relatório / volta à lista |
| Espaço | Apps, Receitas, Diagnóstico | marca ou desmarca; bloqueado durante a execução, com aviso na linha de estado |
| v | Apps, em app com `▾` | abre a lista de versões |
| / | Apps, Receitas, Diagnóstico | busca; com ela ativa letras e números vão ao filtro (inclusive q, j, 1) e só Enter, Esc e setas são comandos |
| Enter | Navegar | abre a Revisão dos marcados; sem marcados, revisa o item sob o cursor |
| Enter | Revisão, modais | aciona o botão em foco (começa em Voltar); no modal, escolhe |
| s | Revisão e perguntas `[s/N]` | executa / responde sim |
| f | Log | liga/desliga seguir o fim; rolar para cima desliga sozinho |
| u | quando há versão nova | abre as notas; Fechar é o padrão, "Atualizar" sai da TUI e roda `toolkit update`; indisponível durante a execução |
| ? | tudo | ajuda |
| Esc | tudo | volta um nível: modal, busca, foco no painel, Revisão; na raiz não faz nada |
| q / Ctrl+C | tudo | sai; durante a execução pergunta "Interromper? O item atual é encerrado e a fila é descartada. [s/N]" |

## Abaixo de 80 colunas e redimensionamento

| Tamanho | O que muda |
|---|---|
| 80 ou mais | layout completo; acima de 80 só o nome e as linhas de comando crescem |
| 70 a 79 | some a coluna VIA (vai para a linha de estado); o nome encolhe até 12 |
| 60 a 69 | abas `2 Rec` e `3 Diag`; VERSÃO junta ao nome (`Python 3.12`) ou ao estado (`✓ 2.43.0 → latest`); faixa `↑ v0.2.0 (u)`; barra de teclas só com mover, marcar, buscar, ajuda, sair |
| < 60 colunas ou < 16 linhas | tela única "Janela pequena demais (mínimo 60 × 16). Aumente a janela ou use toolkit --no-tui."; `q` funciona e a execução em andamento continua |
| altura | só o corpo encolhe; listas rolam com `↓ mais N` |
| redimensionar | recalcula a cada `WindowSizeMsg`, mantém o cursor visível e o log no fim se estiver seguindo; comandos quebram com recuo de 5 e nunca são cortados |

## Pior caso de dados

Nome longo corta no fim com `…` (inteiro na linha de estado). Versão longa (`1:2.43.0-1ubuntu7.3+esm2~24.04.1`)
nunca corta: o estado vira símbolo e versão, o nome cede e a coluna VERSÃO some. 2000 apps: só as linhas
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

## Decisões (padrões adotados; as marcadas esperam o OK do Paulo)

1. **App com versões**: seguir o briefing (Espaço/Enter abre a lista já em `latest`, um Enter confirma).
   *Proposta do designer, a validar:* Espaço marca com `latest` e `v` abre a lista.
2. Revisão: foco inicial em Voltar, `s` executa (igual ao `[s/N]` do CLI).
3. Ordem do lote: apps, depois receitas, a que reinicia por último.
4. Diagnóstico só leitura direto exigiria um campo novo no schema da receita (fase 3 já aprovada): **por
   ora todo diagnóstico passa pela Revisão**. Campo novo só com OK do Paulo.
5. `q` durante a execução pergunta e encerra o item atual, descartando a fila.
6. Conjunto ASCII atrás de `--ascii`, depois.

## Pendências fora da TUI (registradas aqui para a próxima sessão)

- `toolkit update` (baixa a última release, confere o SHA-256 pelo `checksums.txt` com o mesmo código do
  download/bootstrap, troca o binário de forma atômica) e aviso de versão nova na TUI e no fim dos comandos
  (no máximo 1 consulta por dia, cache em `~/.cache`, desligável por flag/variável, nunca bloqueia nem
  atrasa o comando, mostra as notas da release do GitHub). Entra antes da v0.1.0 se couber.
- Menores da fase 5 (PR à parte): lista completa de IPs reservados no `download`; dmg não aceitar EULA
  sozinho; conferir assinatura de pkg/dmg; nomes reservados e ADS no Windows.
- `printPlan` com o mesmo GOOS/euid do runner real (como root o plano não mostra `sudo`) e cabeçalhos do
  plano sem repetir no `install` real: corrigir antes ou dentro do PR da Revisão, com teste.
