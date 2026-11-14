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

### A1 · Cores de aviso, OK e dica não atingem 4,5:1 em temas claros comuns — M (corrigido na Fase 1: a cor ficou só no símbolo)
- **Onde:** `internal/tui/theme.go` (`pick(light, dark)`): em fundo claro `Warn`=ANSI 3 (amarelo), `OK`=ANSI 2 (verde),
  `Muted`=ANSI 8 (dicas, horários, cabeçalho de colunas).
- **Medido** (seção abaixo, 16 cores de cada esquema + script): Terminal.app Basic (padrão atual do macOS) Muted 3,81,
  OK 2,53, Warn 2,39; xterm fundo branco Muted 4,00, OK 2,16, Warn 1,70; Solarized Dark OK 2,79, Warn 3,37, Err 3,26;
  Solarized Light Accent 3,41, OK 2,97, Warn 2,98, Err 4,29. Em Campbell e xterm escuros tudo passa (≥ 5,09).
- **Atenuante:** o estado também tem símbolo e palavra, então a informação não se perde; o critério 1.4.3 vale para o
  texto colorido mesmo assim.
- **Negrito não resolve:** `Warn` e `Err` já são negrito (`Bold(!t.Mono)`), e negrito não muda a razão de contraste
  (só ajuda a legibilidade de texto fino). A correção tem de mudar a cor, não o peso.
- **Corrigir (escolher uma, decisão do Paulo):** (a) texto sem cor (cor padrão do terminal, que o usuário já ajustou
  para ser legível) e cor só no símbolo; o símbolo continua com a palavra ao lado, então o 1.4.11 é atendido pela
  palavra. `Muted` passa a ser a cor padrão com a hierarquia dada pela posição, nunca por cor mais clara; (b) tokens
  em cor fixa (256 cores ou RGB) validados em ≥ 4,5:1 contra os fundos extremos de cada tema (claro e escuro já são
  detectados por `IsDark`), com `NO_COLOR` e o modo Mono intactos; custa o princípio de "só paleta ANSI do usuário";
  (c) `--high-contrast`/`TOOLKIT_HIGH_CONTRAST=1` que liga (a) sob demanda. Recomendação: (a) por padrão.
- **Medir:** teste Go com as paletas de referência (as da seção abaixo, mais One Half Light/Dark e Terminal.app Pro)
  que falha abaixo de 4,5:1 para texto e 3:1 para símbolos em cada token do modo escolhido. Esforço: P–M.

### A2 · Sem opção de reduzir movimento — M (corrigido na Fase 1: `TOOLKIT_REDUCE_MOTION=1` / `--no-motion`)
- **Onde:** `internal/tui/apps.go:70` (`tickEvery = 100ms`, spinner `spinnerFrames`), `logtab.go`, `apps.go:298`.
- **Problema:** a skill exige respeitar "reduzir movimento". Hoje o spinner roda a 10 quadros/s, sem opção; no
  leitor de tela ele também gera redesenhos constantes (ver A4).
- **Corrigir:** `TOOLKIT_REDUCE_MOTION=1` / `--no-motion` (e ler `prefers-reduced-motion` onde o terminal expuser):
  trocar o spinner por texto estático "em andamento…" e redesenhar só quando o estado mudar. Teste: com a opção
  ligada, nenhum `tickMsg` agendado. Esforço: P.

### A3 · A doc afirma "nada pisca mais de 3 vezes por segundo", e o spinner muda 10 vezes por segundo — B (frase corrigida na Fase 1)
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

### A5 · Sem teste automatizado de contraste nem de nomes/estados — M (corrigido na Fase 1: `internal/tui/theme_test.go`)
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

### L10 · Texto com espaço para crescer ~35% (colunas fixas na aba Apps) — M
- **Onde:** `internal/tui/apps.go:714` (`columns`): estado 24 (32 a partir de 100 colunas), versão 10 e via 7 são
  larguras fixas; só o nome é flexível (mínimo 12). `cell` corta com `…` (`apps.go:838`). Em 60–69 colunas a versão
  vai junto do estado (22) e some a coluna via. Hoje cabe porque os textos pt-BR são curtos ("não instalado",
  "instalado (fora do Homebrew)" só a partir de 100 colunas). Abaixo disso o texto CAI para a forma curta
  "instalado (fora)" (`externalText`, `apps.go:790`: usa o longo só se `lipgloss.Width` couber na coluna): a queda
  também precisa de teste nos três idiomas, porque o curto traduzido tem outro tamanho e é ele que carrega o estado.
- **Problema:** a skill exige ~35% de folga para as traduções. Frases como "installed (outside Homebrew)",
  "no se pudo verificar" ou os cabeçalhos "INSTALLED"/"INSTALADO" passam das larguras e viram `…`, perdendo a palavra
  que carrega o estado (e o estado nunca pode depender só do símbolo).
- **Corrigir:** (a) larguras calculadas pelo texto mais largo do idioma ativo (`ansi.StringWidth` por catálogo), não por
  constante; (b) palavra de estado curta e estável por idioma (a explicação longa vai para a linha de estado); (c)
  quebra de linha em vez de reticências onde o estado for a única informação.
- **Teste:** tabela de largura (80, 70, 60 colunas e o mínimo 60×16) × idiomas (pt-BR, en, es) × estados (instalado,
  fora do gerenciador com versão, não instalado, não deu para verificar, erro) que falha se a palavra do estado for
  cortada (`…` dentro da célula de estado) ou a linha passar da largura. Depende de L1 (catálogo de idiomas) para
  rodar nos três idiomas; antes disso, rodar em pt-BR com o texto alargado em 35% como simulação. Esforço: M.

## Medição de contraste

Fórmula do WCAG 2.2: `L = 0,2126 R + 0,7152 G + 0,0722 B` (canais linearizados); razão = `(L1 + 0,05) / (L2 + 0,05)`.
Fundo = fundo padrão do esquema; cada token usa o índice ANSI que `theme.go` escolhe para o tema detectado
(claro: `Muted` 8, `Accent` 4, `OK` 2, `Warn` 3, `Err` 1; escuro: 7, 14, 10, 11, 9). **Meta: 4,5:1 (texto).**
Em negrito = abaixo da meta. Cada célula mostra a cor usada (hex) e a razão.

| Esquema (fundo) | Muted | Accent | OK | Warn | Err |
|---|---|---|---|---|---|
| Windows Terminal Campbell (`0C0C0C`, escuro) | 7 `CCCCCC` 12,18 | 14 `61D6D6` 11,27 | 10 `16C60C` 8,49 | 11 `F9F1A5` 16,91 | 9 `E74856` 5,09 |
| xterm (`000000`, escuro) | 7 `E5E5E5` 16,67 | 14 `00FFFF` 16,75 | 10 `00FF00` 15,30 | 11 `FFFF00` 19,56 | 9 `FF0000` 5,25 |
| Terminal.app Basic, padrão atual do macOS (`FFFFFF`, claro) | 8 `818383` **3,81** | 4 `492EE1` 7,61 | 2 `25BC24` **2,53** | 3 `ADAD27` **2,39** | 1 `C23621` 5,46 |
| xterm (`FFFFFF`, claro) | 8 `7F7F7F` **4,00** | 4 `0000EE` 9,40 | 2 `00CD00` **2,16** | 3 `CDCD00` **1,70** | 1 `CD0000` 5,84 |
| Solarized Dark (`002B36`, escuro) | 7 `EEE8D5` 12,25 | 14 `93A1A1` 5,61 | 10 `586E75` **2,79** | 11 `657B83` **3,37** | 9 `CB4B16` **3,26** |
| Solarized Light (`FDF6E3`, claro) | 8 `002B36` 13,92 | 4 `268BD2` **3,41** | 2 `859900` **2,97** | 3 `B58900` **2,98** | 1 `DC322F` **4,29** |

**Correção da v1 deste relatório:** a linha do Terminal.app tinha vindo de uma tabela antiga do perfil Basic (Muted
5,74, Accent 12,78, OK 3,25, Warn 3,04, Err 8,92). A paleta do macOS atual é a da tabela acima (`C23621`, `25BC24`,
`ADAD27`, `492EE1`… e `818383` para o preto brilhante), que dá 3,81 / 7,61 / 2,53 / 2,39 / 5,46, como o Crivo mediu.
Conferi nesta máquina: o perfil Basic não guarda cores ANSI próprias (`ANSI*Color` ausente no plist de
`com.apple.Terminal`), ou seja, usa o padrão do sistema, que é essa paleta. Se o Terminal.app de outra versão do
macOS usar valores diferentes, o script abaixo refaz a conta com os 16 hex dele.

As 16 cores de cada esquema (índices 0–15), como usadas no script:

- Campbell: `0C0C0C C50F1F 13A10E C19C00 0037DA 881798 3A96DD CCCCCC 767676 E74856 16C60C F9F1A5 3B78FF B4009E 61D6D6 F2F2F2`
- xterm: `000000 CD0000 00CD00 CDCD00 0000EE CD00CD 00CDCD E5E5E5 7F7F7F FF0000 00FF00 FFFF00 5C5CFF FF00FF 00FFFF FFFFFF`
- Terminal.app Basic (atual): `000000 C23621 25BC24 ADAD27 492EE1 D338D3 33BBC8 CBCCCD 818383 FC391F 31E722 EAEC23 5833FF F935F8 14F0F0 E9EBEB`
- Solarized (Dark e Light, só muda o fundo): `073642 DC322F 859900 B58900 268BD2 D33682 2AA198 EEE8D5 002B36 CB4B16 586E75 657B83 839496 6C71C4 93A1A1 FDF6E3`

Script para repetir a medição (`python3 -I medir.py`; foi o que gerou a tabela):

```python
#!/usr/bin/env python3
# Contraste WCAG 2.2 dos tokens da TUI (internal/tui/theme.go) sobre paletas ANSI. Uso: python3 -I medir.py
def lum(h):
    f = lambda c: c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4
    r, g, b = (f(int(h[i:i + 2], 16) / 255) for i in (0, 2, 4))
    return 0.2126 * r + 0.7152 * g + 0.0722 * b
def ratio(a, b):
    hi, lo = sorted((lum(a), lum(b)), reverse=True)
    return (hi + 0.05) / (lo + 0.05)
# índices ANSI que theme.go usa: (claro, escuro)
TOKENS = {"Muted": (8, 7), "Accent": (4, 14), "OK": (2, 10), "Warn": (3, 11), "Err": (1, 9)}
X = "000000 CD0000 00CD00 CDCD00 0000EE CD00CD 00CDCD E5E5E5 7F7F7F FF0000 00FF00 FFFF00 5C5CFF FF00FF 00FFFF FFFFFF"
SOL = "073642 DC322F 859900 B58900 268BD2 D33682 2AA198 EEE8D5 002B36 CB4B16 586E75 657B83 839496 6C71C4 93A1A1 FDF6E3"
PALETAS = [  # (nome, fundo, escuro?, 16 cores)
    ("Windows Terminal Campbell", "0C0C0C", True, "0C0C0C C50F1F 13A10E C19C00 0037DA 881798 3A96DD CCCCCC 767676 E74856 16C60C F9F1A5 3B78FF B4009E 61D6D6 F2F2F2"),
    ("xterm (fundo preto)", "000000", True, X),
    ("Terminal.app Basic (padrão do macOS atual)", "FFFFFF", False, "000000 C23621 25BC24 ADAD27 492EE1 D338D3 33BBC8 CBCCCD 818383 FC391F 31E722 EAEC23 5833FF F935F8 14F0F0 E9EBEB"),
    ("xterm (fundo branco)", "FFFFFF", False, X),
    ("Solarized Dark", "002B36", True, SOL),
    ("Solarized Light", "FDF6E3", False, SOL),
]
for nome, fundo, escuro, p in PALETAS:
    p = p.split()
    linha = []
    for tok, (claro, esc) in TOKENS.items():
        i = esc if escuro else claro
        r = ratio(p[i], fundo)
        linha.append(f"{tok} {i}={p[i]} {r:.2f}{'' if r >= 4.5 else ' <4,5'}")
    print(f"{nome} (fundo {fundo}): " + " | ".join(linha))
```

As paletas são os valores publicados dos esquemas padrão; a conferência final deve usar o terminal real (o usuário
pode ter mudado as cores). Ainda faltam medir: One Half Light/Dark, Terminal.app Pro, iTerm2 Default, GNOME Terminal
e o modo de alto contraste do Windows.

## Ordem sugerida das correções (PRs pequenos)

1. **A5 + A1**: teste de contraste e símbolos, e a correção das cores `OK`/`Warn` (P–M).
2. **A2 + A3**: `--no-motion` / `TOOLKIT_REDUCE_MOTION` e ajuste da frase (P).
3. **A4**: verificação com leitor de tela (roteiro manual do Paulo ou de quem tiver o leitor) e, conforme o
   resultado, modo `--accessible` (M–G).
4. **L1 + L2 (+ L3)**: mecanismo de idiomas, teste de chaves e plural, migrando `internal/msg` (1º PR de l10n).
5. **L1 (migração)** da TUI e dos demais pacotes, um por PR; **L10** (larguras e teste nos 3 idiomas) junto da migração da TUI.
6. **L4** (datas/números; decidir antes sobre `golang.org/x/text`), **L5** (catálogo/receitas), **L7** (bootstraps e
   notas).
7. **A6, A7, A8, L6, L9** (baixo risco, polimento) e **L8** no repo do site.

## Decisões que preciso do Paulo

- `golang.org/x/text` como dependência nova (L4), ou formatadores próprios?
- Quem faz o roteiro com leitor de tela (A4): VoiceOver no Mac é direto; NVDA/Narrator exigem o Windows.
- Idiomas do `README` (L9) e das notas da release (L7): entram agora ou depois?
