# Auditoria de acessibilidade e localização (2026-11-13)

Auditoria do toolkit (v0.2.1, `main` em 54416d4) contra a skill `padroes-produto-pilutech`. **Só diagnóstico: nada
foi corrigido.** As correções viram PRs pequenos, na ordem que o Paulo combinar (sugestão no fim).

## Escopo e como ler

O toolkit é CLI + TUI de terminal (Bubble Tea), não site nem app gráfico. Por isso:

- Os critérios do WCAG 2.2 AA que falam de ponteiro, toque e navegador viram o equivalente de terminal (teclado,
  tema do terminal, fonte do usuário, leitor de tela do terminal). **Alvo de 44 pt: não se aplica** (a TUI não usa
  mouse; confirmado: nenhuma habilitação de mouse em `internal/tui` nem em `internal/cli/tui.go`).
- **`llms.txt`: não se aplica ao repositório** (não é site). O texto público do produto sai do `PRODUTO.md` para
  `pilutech.com.br/apps/toolkit`; o `llms.txt` e a rota `/en/`, `/es/` dessa página são do repo `pilutech-sites`
  (achado L8 abaixo, só para registro).
- Severidade: **A** (barreira real, corrigir primeiro), **M** (degrada o uso ou descumpre a skill, sem bloquear),
  **B** (polimento ou verificação). Esforço: P (horas), M (1–2 dias), G (vários PRs).
- Contraste: calculado pela fórmula do WCAG 2.2 (luminância relativa) sobre os valores publicados das paletas
  padrão. A TUI usa só as 16 cores ANSI do terminal, então **o contraste real é o do tema do usuário**; a tabela
  é uma amostra, não uma garantia. Detalhes e script na seção "Medição de contraste".

## O que já cumpre

| Item | Evidência |
|---|---|
| Tudo por teclado, sem armadilha de foco | setas/jk, Tab, 1–4, Enter, Espaço, Esc, `?`, `q`; sem mouse (`docs/tui.md`) |
| Nada depende só de cor | todo estado tem símbolo E palavra (✓ ok, ✗ falhou, ! aviso, ○ pendente); aba ativa com colchetes, cursor `>`, botão em foco `>` (`internal/tui/theme.go`) |
| Modo sem cor | `NO_COLOR` e `--no-color`; layout idêntico, testado; em Mono não sai sequência de estilo |
| Segue o tema do usuário | só cores ANSI do terminal (nenhum RGB fixo); detecta fundo claro/escuro (`msg.IsDark()`) |
| Movimento mínimo | único movimento é um spinner ASCII de 4 quadros (`\|/-\`) num caractere |
| Nunca só a TUI | `list`, `install`, `--dry-run`, `--yes`, `recipe`, `resume`, `--no-tui`, log em arquivo; sem terminal ou com `TERM=dumb` não abre a TUI |
| Erros dizem o que houve e o que fazer | ex.: `ErrNeedsAdmin` com a dica `sudo -v`; mensagens de `internal/msg` |
| Tela pequena | funciona em 60×16 e redimensiona (a confirmar abaixo de 60×16, achado A7) |
| Texto de fora é sanitizado | `internal/textsafe` limpa ESC/OSC/bidi antes de ir ao terminal (também protege leitor de tela de lixo) |
| Textos centralizados em parte | `internal/msg` concentra 125 mensagens da CLI (base para o catálogo de idiomas) |

## Achados de acessibilidade

### A1 · Cores de aviso e de OK não atingem 4,5:1 em temas claros comuns — M
- **Onde:** `internal/tui/theme.go` (`pick(light, dark)`): em fundo claro `Warn`=ANSI 3 (amarelo), `OK`=ANSI 2 (verde),
  `Muted`=ANSI 8.
- **Medido** (seção abaixo): xterm claro, Warn 1,70:1, OK 2,16:1, Muted 4,00:1; Terminal.app Basic, Warn 3,04, OK 3,25;
  Solarized Dark, OK 2,79 e Warn 3,37 (as cores de índice alto do Solarized são cinzas de baixo contraste); Solarized
  Light, OK 2,97, Warn 2,98, Accent 3,41. Em Campbell escuro e xterm escuro tudo passa (≥ 5,09).
- **Atenuante:** o estado também tem símbolo e palavra, então a informação não se perde; o requisito 1.4.3 vale para o
  texto colorido mesmo assim.
- **Corrigir:** (a) para `OK`/`Warn`, usar negrito e deixar a cor só no símbolo (símbolo de ≥ 3:1 vale como
  componente gráfico) e a palavra na cor padrão do terminal; ou (b) pedir `--theme high-contrast` que troca para
  texto em cor padrão + negrito. Não dá para "consertar" a paleta do usuário.
- **Medir:** teste Go com as paletas de referência (Campbell, xterm, Terminal.app Basic/Pro, Solarized, One Half
  Light/Dark) falhando abaixo de 4,5:1 para texto e 3:1 para símbolos. Esforço: P–M.

### A2 · Sem opção de reduzir movimento — M
- **Onde:** `internal/tui/apps.go:70` (`tickEvery = 100ms`, spinner `spinnerFrames`), `logtab.go`, `apps.go:298`.
- **Problema:** a skill exige respeitar "reduzir movimento". Hoje o spinner roda a 10 quadros/s, sem opção; no
  leitor de tela ele também gera redesenhos constantes (ver A4).
- **Corrigir:** `TOOLKIT_REDUCE_MOTION=1` / `--no-motion` (e ler `prefers-reduced-motion` onde o terminal expuser):
  trocar o spinner por texto estático "em andamento…" e redesenhar só quando o estado mudar. Teste: com a opção
  ligada, nenhum `tickMsg` agendado. Esforço: P.

### A3 · A doc afirma "nada pisca mais de 3 vezes por segundo", e o spinner muda 10 vezes por segundo — B
- **Onde:** `docs/roadmap.md` ("o único movimento é o spinner… nada pisca mais de 3 vezes por segundo").
- **Análise:** o WCAG 2.3.1 limita *flashes* de área grande e alto brilho; um caractere ASCII não atinge o limiar de
  área, então não reprova. Mas a frase está imprecisa.
- **Corrigir:** reescrever a frase (área minúscula, abaixo do limiar de flash; 2,5 voltas/s) e resolver junto com A2.
  Esforço: P.

### A4 · Leitor de tela com TUI em tela cheia: comportamento não verificado — A (verificação)
- **Onde:** `internal/tui` inteiro (tela alternativa, redesenho completo a cada quadro).
- **Problema:** nunca foi testado com VoiceOver (Terminal.app/iTerm2), NVDA/Narrator (Windows Terminal) ou Orca.
  Riscos conhecidos: o spinner redesenha a tela a cada 100 ms e pode fazer o leitor reler a linha; a troca de aba, a
  pergunta `[s/N]` e o fim da execução podem não ser anunciados; a ordem de leitura de uma tela cheia é linha a linha.
- **Como medir:** roteiro manual com os 3 leitores: abrir, marcar um app, revisar, executar (em dry-run), trocar de
  aba, responder `[s/N]`; registrar o que é anunciado.
- **Corrigir (depende do resultado):** modo "acessível" por padrão quando houver leitor de tela (variável
  `TOOLKIT_ACCESSIBLE=1` / `--accessible`, e `--no-tui` já existe): sem tela alternativa, texto linear, sem spinner
  (A2), anúncio de mudança de aba e de fim de execução em linha própria. Esforço: M–G.

### A5 · Sem teste automatizado de contraste nem de nomes/estados — M
- **Problema:** a skill pede "medir, não estimar", e hoje o contraste não tem teste. O modo `NO_COLOR` tem teste de
  layout, mas não há teste que prove "todo estado tem símbolo E palavra" para todas as telas.
- **Corrigir:** (a) o teste de contraste do A1; (b) teste de tabela que percorre os estados (`ok`, `falhou`,
  `pendente`, `pulado`, `aviso`) e confirma símbolo + palavra em Mono. Esforço: P.

### A6 · Zoom/fonte maior: sem verificação a 200% — B
- **Problema:** com a fonte a 200%, um terminal de 80×24 vira 40×12, abaixo do mínimo declarado (60×16). Não achei
  uma tela "terminal pequeno demais" com instrução (nenhum `minW/minH` em `layout.go`/`model.go`); o comportamento
  abaixo do mínimo precisa ser verificado.
- **Corrigir:** abaixo de 60×16, mostrar uma linha "terminal pequeno: aumente a janela ou use `--no-tui`" (nunca
  tela quebrada), com teste de tamanho. Esforço: P.

### A7 · Teclas de uma letra e sem remapeamento — B
- A barra de teclas é contextual e sempre visível (bom). Não há como remapear teclas; atalhos de uma letra (`u`,
  `s`, `q`) colidem com tecnologia assistiva em alguns terminais. **Corrigir (se for relevante pelos testes de A4):**
  confirmar que `s` para "sim" exige foco na pergunta. Esforço: P, só depois de A4.

### A8 · Símbolos Unicode sem alternativa ASCII — B
- **Onde:** `SymOK ✓`, `SymFail ✗`, `↑`, `↓`, `─` (`theme.go`); o próprio código registra "o conjunto ASCII (`--ascii`)
  fica para depois". Em Windows Terminal/consoles antigos ou com fontes sem o glifo, aparecem quadrados.
- **Corrigir:** `--ascii` (`[ok]`, `[x]`, `!`, `-`, `^`, `v`, `-`). Esforço: P.

## Achados de localização

Estado: **tudo em pt-BR, no código.** Não existe `--lang`, `TOOLKIT_LANG` nem leitura de `LC_ALL`/`LANG` para o
idioma. A estrutura só ajuda em parte: `internal/msg` tem 125 constantes (`msg.go` 85, `recipe.go` 40), mas há
literais acentuados fora dele (contagem de linhas com literal acentuado, testes inclusos: `internal/tui` 218,
`internal/cli` 201, `internal/providers` 182, `internal/recipes` 86, `internal/update` 70, `internal/resume` 50,
`internal/detect` 29, `internal/state` 17).

### L1 · Sem catálogo de textos por idioma — A (base para o resto)
- **Onde:** todo o `internal/msg` e os literais acima.
- **Corrigir:** pacote `internal/i18n` com chaves estáveis e três catálogos (`pt-BR` origem e padrão, `en`, `es`),
  escolha do idioma por `--lang` > `TOOLKIT_LANG` > `LC_ALL`/`LC_MESSAGES`/`LANG` > pt-BR. Migrar por pacote, em PRs
  pequenos (msg → cli → tui → recipes/update/detect/providers). Esforço: G (vários PRs; o primeiro entrega o
  mecanismo + `msg`).

### L2 · Sem teste de chaves entre idiomas — A (entra com L1)
- **Corrigir:** teste que falha se uma chave existir em um idioma e faltar em outro, ou se o formato (`%s`, `%d`)
  divergir entre idiomas. Esforço: P (dentro do PR de L1).

### L3 · Plural montado à mão em português — M
- **Onde:** `internal/cli/tui_exec.go:91` (`countWord(n, one, many)`), `internal/tui/apps.go:695`
  (`"%d de %d apps"`), `logtab.go:145-150` (`"%d ok"`, `"%d falhou"`), `recipes_tab.go:517`.
- **Corrigir:** regra de plural por idioma no catálogo (pt/es/en têm as formas `one`/`other`; cuidado com 0 em pt-BR, "0
  apps" e "1 app"), sem concatenação de palavras no código. Esforço: P–M (junto de L1).

### L4 · Datas e números sem formatador do sistema — M
- **Onde:** `internal/cli/recipe.go:373` (`StartedAt.Format(time.RFC3339)` na saída humana de `recipe status`); `%d`
  em contagens. Os nomes de arquivo de log (`20060102-150405`) são técnicos e devem ficar.
- **Corrigir:** saída para humanos com data/hora no formato do idioma e fuso do usuário (RFC 3339 só em `--json` e
  em log); números com separador do idioma. Go não tem `Intl` na biblioteca padrão: avaliar `golang.org/x/text`
  (dependência nova, **precisa de justificativa e do OK**, por supply chain) ou formatadores próprios para os 3
  idiomas (menor, suficiente: data curta + hora 24 h). Esforço: M.

### L5 · Catálogo de apps e receitas só em pt-BR — M
- **Onde:** `catalog/catalog.yaml`, `recipes/*/*.yaml` (`name:`, `description:`), `docs/catalogo.md`, `docs/receitas.md`.
- **Corrigir:** campos opcionais `name_en`/`name_es` (ou mapa `i18n:`) nos YAML, com fallback para pt-BR; nomes de
  apps e de produtos não se traduzem. O validador do catálogo precisa aceitar o campo novo (hoje chave desconhecida
  é erro). Esforço: M.

### L6 · Mensagens que vêm do gerenciador e do sistema não são traduzíveis — B
- **Onde:** trechos de `stderr` do winget/apt/brew (via `providers.Snippet`) aparecem como vieram; o toolkit já
  força `LC_ALL=C` para classificar erros, então o texto que ele mostra é em inglês.
- **Corrigir:** não traduzir a saída de terceiros; rotular ("saída do apt:") no idioma do usuário. Esforço: P.

### L7 · Bootstraps (`unix.sh`, `win.ps1`) e notas da release em pt-BR só — M
- **Onde:** `scripts/bootstrap/*` (mensagens `echo`/`Write-Host`), notas das releases no GitHub, o aviso de versão e o
  modal "notas da release" (texto vem da release).
- **Corrigir:** bootstraps: `en`/`es` por `LANG`/`Get-UICulture` com pt-BR padrão (teste de chaves também); notas da
  release: seção curta em `en` e `es` no fim de cada nota (processo, não código). Esforço: M.

### L8 · Página pública do produto sem `/en/`, `/es/` e `llms.txt` (fora deste repo) — B (registro)
- **Onde:** `pilutech.com.br/apps/toolkit` (repo `pilutech-sites`); o `PRODUTO.md` daqui é o texto-fonte.
- **Corrigir (no `pilutech-sites`):** rota por idioma com `hreflang`, entrada no `/llms.txt` do site e um `PRODUTO`
  traduzido. **Texto público precisa do OK do Paulo.** Esforço: M, fora do toolkit.

### L9 · Idioma do README e dos docs — B
- README e `docs/` em pt-BR; para um repositório público, um `README.en.md`/`README.es.md` curto (o que é, instalar,
  `toolkit update`) amplia o alcance. Texto público: precisa de OK. Esforço: P.

## Medição de contraste

Fórmula do WCAG 2.2: `L = 0,2126 R + 0,7152 G + 0,0722 B` (canais linearizados); razão = `(L1 + 0,05) / (L2 + 0,05)`.
Fundo = fundo padrão do esquema; cada token usa o índice ANSI que `theme.go` escolhe para o tema detectado
(claro: `Muted` 8, `Accent` 4, `OK` 2, `Warn` 3, `Err` 1; escuro: 7, 14, 10, 11, 9). **Meta: 4,5:1 (texto).**

| Esquema (fundo) | Muted | Accent | OK | Warn | Err |
|---|---|---|---|---|---|
| Windows Terminal Campbell (0C0C0C, escuro) | 12,18 | 11,27 | 8,49 | 16,91 | 5,09 |
| xterm (000000, escuro) | 16,67 | 16,75 | 15,30 | 19,56 | 5,25 |
| Terminal.app Basic (FFFFFF, claro) | 5,74 | 12,78 | **3,25** | **3,04** | 8,92 |
| xterm (FFFFFF, claro) | **4,00** | 9,40 | **2,16** | **1,70** | 5,84 |
| Solarized Dark (002B36, escuro) | 12,25 | 5,61 | **2,79** | **3,37** | **3,26** |
| Solarized Light (FDF6E3, claro) | 13,92 | **3,41** | **2,97** | **2,98** | **4,29** |

Negrito = abaixo de 4,5:1. As paletas são os valores publicados dos esquemas padrão; a conferência final deve usar o
terminal real (o usuário pode ter mudado as cores). Itens que ainda faltam medir: One Half Light/Dark, Terminal.app
Pro, iTerm2 Default, GNOME Terminal, e o modo de alto contraste do Windows.

## Ordem sugerida das correções (PRs pequenos)

1. **A5 + A1**: teste de contraste e símbolos, e a correção das cores `OK`/`Warn` (P–M).
2. **A2 + A3**: `--no-motion` / `TOOLKIT_REDUCE_MOTION` e ajuste da frase (P).
3. **A4**: verificação com leitor de tela (roteiro manual do Paulo ou de quem tiver o leitor) e, conforme o
   resultado, modo `--accessible` (M–G).
4. **L1 + L2 (+ L3)**: mecanismo de idiomas, teste de chaves e plural, migrando `internal/msg` (1º PR de l10n).
5. **L1 (migração)** da TUI e dos demais pacotes, um por PR.
6. **L4** (datas/números; decidir antes sobre `golang.org/x/text`), **L5** (catálogo/receitas), **L7** (bootstraps e
   notas).
7. **A6, A7, A8, L6, L9** (baixo risco, polimento) e **L8** no repo do site.

## Decisões que preciso do Paulo

- `golang.org/x/text` como dependência nova (L4), ou formatadores próprios?
- Quem faz o roteiro com leitor de tela (A4): VoiceOver no Mac é direto; NVDA/Narrator exigem o Windows.
- Idiomas do `README` (L9) e das notas da release (L7): entram agora ou depois?
