# Roadmap depois da v0.1.0

Ordem combinada com o Paulo; um PR por vez, com revisão (Lupa/Crivo).

1. **PR 5c: Planner e Runner reais** (a TUI passa a executar): feito (`docs/tui.md`).
2. **`toolkit update`** (SHA-256 pelo `checksums.txt` com o mesmo código do `internal/download`, troca atômica,
   `.old` no Windows, `--check`, `--dry-run`, `--version`) e **aviso de versão nova** (1 consulta/dia, cache em
   `~/.cache`, desligável, nunca bloqueia). Detalhes em `docs/tui.md`.
3. **Ícone** "Porca de comando" (variante A): **feito** (README, `assets/icon/`, PNG gerados do SVG por
   `scripts/gerar-icone.sh`; PNG do site em `assets/icon/toolkit-site.png`). Ainda não há modelo de notas de release
   no repositório para levar o ícone. Possível melhoria: `.ico`/`.syso` no exe do Windows (fora desta etapa).
4. **`toolkit --version`** (hoje só existe `toolkit version`).
5. **Menores da fase 5**: lista completa de IPs reservados no `download`, dmg sem EULA automático, assinatura de
   pkg/dmg, nomes reservados e ADS no Windows.
6. **Smoke manual** (`docs/smoke.md`) em Windows 11, macOS e Linux, e a validação do `win.ps1` e do PATH do usuário
   no Windows.

## Padrões de produto da PiluTech (skill `padroes-produto-pilutech`)

### Acessibilidade da TUI (meta equivalente ao WCAG 2.2 AA)
Já cumprido: tudo por teclado (setas/jk, Tab, 1-4, Enter, Espaço, Esc, `?`, `q`; nada depende de mouse); nada
depende só de cor (todo estado tem símbolo E palavra, a aba ativa leva colchetes, o cursor leva `>`, o botão em
foco leva `>`); modo sem cor (`NO_COLOR`) com layout idêntico e testado; cores só pela paleta ANSI do terminal
(o contraste segue o tema do usuário, claro ou escuro); o único movimento é o spinner de 4 quadros (nada pisca
mais de 3 vezes por segundo); funciona em 60x16 e redimensiona; erros dizem o que houve e o que fazer.
Falta: teste automatizado de contraste para o par de cores escuro/claro dos tokens (medir, não estimar); opção
para desligar o spinner (`TOOLKIT_REDUCE_MOTION=1`/`--no-motion`); verificar com leitor de tela (VoiceOver no
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
