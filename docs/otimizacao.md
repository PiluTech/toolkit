# `toolkit otimizacao` (só Windows): leitura de gargalos

Lê o que pesa no Windows **só para leitura, sem alterar nada e sem administrador**, e diz o que fazer **em texto**. É a parte A
da função 3 do [plano](plano-seguranca-e-tui.md); o comando `toolkit otimizacao` (sem subcomando) e a leitura de gargalos da TUI não
mudam nenhuma configuração. A parte B (reduzir animações, com plano, backup e desfazer, **EXPERIMENTAL**) é o subcomando
`toolkit otimizacao animacoes`, descrito [no fim deste documento](#animações-experimental-toolkit-otimizacao-animacoes).

```sh
toolkit otimizacao                       # resumo: o que pesa e o que fazer
toolkit otimizacao --completo            # resumo + relatório completo (todos os dados lidos)
toolkit otimizacao --json                # o mesmo em JSON (findings + report)
toolkit otimizacao --relatorio rel.txt   # grava resumo + relatório completo num arquivo NOVO (0600, não sobrescreve)
```

O andamento vai para o stderr; `--json` não escreve andamento. A leitura leva alguns segundos (amostra de CPU de 3 leituras, 1 s
entre elas). O idioma da saída segue `TOOLKIT_LANG`/`LC_ALL`/`LC_MESSAGES`/`LANG` (pt-BR, en, es), como o diagnóstico. Fora do
Windows o comando termina com erro `o comando otimizacao só existe no Windows (este sistema é <SO>); nada foi alterado`, sem ler
nem gravar nada (como a privacidade). Código: `internal/otimizacao` (coleta, análise, relatório) e `internal/cli/otimizacao.go`.

## O que lê (PowerShell de texto fixo, só ASCII, JSON compacto, `SilentlyContinue`)

Tudo passa pelo `exec.ReadOnlyRunner` (recusa comando que não seja de leitura ou que peça elevação), pelo caminho absoluto
`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`. Um teste percorre todos os scripts: ASCII puro, nenhum verbo que
escreve, remove, para ou cria (`Set-`, `Remove-`, `Stop-`, `New-`, `Disable-`, `reg `, `sc `, `>`...). Cada área é independente: a que
falha vira "não lido" (com o motivo) e o resto continua.

| Área | Fonte | Por quê |
|---|---|---|
| CPU | `Win32_PerfFormattedData_PerfOS_Processor` (`_Total`), 3 leituras com 1 s; processos: `Win32_PerfFormattedData_PerfProc_Process` (percentual do momento, dividido pelos processadores lógicos) | Nada de `Get-Counter`: o caminho do contador é traduzido (não existe em Windows em português); as classes CIM têm nome fixo. O `.CPU` do `Get-Process` é tempo **acumulado**, não diz quem pesa agora. Amostras todas zero = "sem leitura válida", nunca "CPU ociosa" |
| Memória | `Win32_ComputerSystem.TotalPhysicalMemory`, `Win32_PerfFormattedData_PerfOS_Memory` (disponível, commit e limite), `Win32_PageFileUsage`, `Get-Process` por conjunto de trabalho | "Disponível" inclui o cache reutilizável (o do Gerenciador de Tarefas); o "livre" puro assustaria sem motivo |
| Disco do sistema | `SystemDrive` -> `Win32_LogicalDisk` (espaço), `Get-Partition` -> `Get-PhysicalDisk` (tipo), `Win32_PerfRawData_PerfDisk_PhysicalDisk` da instância do volume (latência e fila, ambas pelos contadores brutos) | `Unspecified` (máquina virtual, RAID) é **desconhecido**, nunca HDD. Só o `drive` (que o script sempre preenche) não é leitura: sem tamanho e livre, tipo, fila ou latência a área fica em "não lido" |
| Inicialização | `Win32_StartupCommand` + chaves `Run` de HKCU e HKLM (só leitura; duplicata sai por nome) | Contagem de itens **registrados**. A fonte não diz o peso de cada um nem se o usuário já o desligou em Configurações (`StartupApproved` não é lido: A VERIFICAR); por isso não há "os mais pesados" |
| Serviços | `Win32_Service` com `StartMode='Auto'` | Conta os fora da pasta do Windows (heurística: a maioria é de terceiros) e lista os parados com `ExitCode` de erro (0 = parada normal e 1077 = nunca tentou iniciar, por gatilho ou início atrasado, não contam) |
| Boot | `Get-WinEvent` no log `Microsoft-Windows-Diagnostics-Performance/Operational`, evento **100**, o mais recente, campos lidos por nome no XML (`BootTime`, `BootIsDegradation`) | `try/catch` separa os casos: `ok`, `none` (sem evento), `nolog`, `denied` e `error` (com `SilentlyContinue` "sem evento" e "acesso negado" ficam iguais) |

**Latência do disco (contadores brutos).** O `AvgDisksecPerTransfer` das classes `Win32_PerfFormattedData_*` é um inteiro
(`uint32`) em **segundos**: 30 ms viraria `0` e nunca bateria o limiar de 25 ms. Por isso a latência vem de
`Win32_PerfRawData_PerfDisk_PhysicalDisk` ([classe](https://learn.microsoft.com/en-us/previous-versions/aa394262(v=vs.85)),
contador `PERF_AVERAGE_TIMER`), em 3 amostras com 1 s: `AvgDisksecPerTransfer` (N, ticks acumulados),
`AvgDisksecPerTransfer_Base` (D, transferências acumuladas) e `Frequency_PerfTime` (F, ticks por segundo). Entre a primeira e a
última amostra: `latência = ((N1 - N0) / F) / (D1 - D0)` segundos por transferência, mostrada em ms (função `latencyMs`). Amostra
única, base (D) sem aumento, N que voltou ou F <= 0 = **sem leitura** (campo ausente, "sem dado" no relatório completo), nunca
"0 ms" nem "ok". Os testes exercitam o caminho todo (script -> JSON -> cálculo) com contadores brutos sintéticos: 30 ms bate o
limiar, 24,9 ms não.

**Presença dos campos.** Dado ausente não é zero. `AvailableBytes` é ponteiro: `avail` ausente fica `nil` (a RAM disponível não
é avaliada, o relatório diz "sem dado" e a área aparece em "não lido" com o motivo); um **zero real** de RAM disponível é medida e
indica pressão de memória (achado). Só o total de memória, ou só o `drive` do disco, não são leitura: a área fica sem leitura.

**Boot, com honestidade.** O evento 100 é o aviso de que o Windows iniciou e **uma lentidão foi detectada**; algumas builds não o
geram e o log pode vir vazio. Por isso "sem evento" e "log ausente" viram **"sem dado"** (com a frase `Isto NÃO significa que o
boot seja rápido: significa que não há medida`), nunca "rápido". Acesso negado sem administrador = `Skipped.NeedsAdmin` (o resumo
diz "abra o terminal como administrador"; o toolkit nunca eleva). Os eventos 101 e 102 (aplicativo e driver) e o 200
(desligamento) **não** são lidos: o mapeamento não foi confirmado em fonte oficial (A VERIFICAR). Os nomes dos campos do evento 100 vêm de relato da comunidade (A VERIFICAR em
máquina real): campo ausente = sem dado.

## Limiares dos achados

Só números, limiares explícitos e **nenhuma pontuação**. Cada comparação tem teste abaixo, igual e acima da fronteira. Os de
memória e de espaço livre são os mesmos do [diagnóstico](diagnostico.md): as duas telas não se contradizem na mesma máquina.

| Achado | Regra (constante em `internal/otimizacao/analise.go`) | Gravidade |
|---|---|---|
| CPU com uso alto | média da amostra `>= 80%` (a leitura é um instante: o texto diz isso) | atenção |
| Pouca memória disponível | em uso `>= 90%` do total (disponível `<= 10%`); disponível não lido não gera achado | atenção |
| Memória virtual (commit) perto do limite | commit `>= 90%` do limite | atenção |
| Arquivo de paginação muito usado | em uso `>= 80%` do alocado | info |
| Disco do sistema é HDD | tipo lido igual a `HDD` (SSD e desconhecido não geram) | atenção |
| Pouco espaço livre no disco do sistema | livre `< 10%` do tamanho (atenção) e `< 5%` (problema); livre desconhecido não gera | atenção / problema |
| Fila alta no disco | fila média `>= 2`, calculada pelos contadores BRUTOS: `(q1-q0)/(t1-t0)` com `AvgDiskQueueLength` (PERF_COUNTER_100NS_QUEUELEN_TYPE) e `Timestamp_Sys100NS`; o valor formatado do CIM é inteiro e truncaria (1,9 viraria 1). Amostra única, tempo parado ou contador que voltou = sem leitura (nunca 0) | atenção |
| Disco lento para responder | latência média `>= 25 ms` | atenção |
| Muitos itens registrados para iniciar | mais de 10 | info |
| Muitos serviços automáticos fora da pasta do Windows | mais de 25 | info |
| Serviço automático parado com erro | qualquer um (informativo) | info |
| Boot com lentidão | o Windows marcou o último boot como degradado (evento 100) | info |
| Boot sem dado | sem evento ou sem log (nunca "rápido") | info |

Sem nenhum achado e com ao menos uma área lida (CPU, memória ou disco com medida, inicialização, serviços ou boot com evento): `nenhum
gargalo evidente` (com o aviso de que são números de um instante, não garantia). **Nada lido: `Resultado inconclusivo: nada foi lido`**
(pt/en/es), com o achado `sistema` de informação "Nada foi lido: resultado inconclusivo" no `--json` e na TUI, nunca "tudo certo". A legenda de limiares aparece no painel de detalhe da TUI (`otimizacao.Legend`).

## O que NÃO faz (lista fixa, em teste de regressão)

`otimizacao.NeverList` (nos três idiomas) e `NeverRecommendTerms`: um teste roda a análise de relatórios que disparam todos os
achados, em pt, en e es, e reprova se título, detalhe ou ação citar um desses termos. Os achados de serviço também não podem falar
em desativar, desabilitar ou parar.

- Não desativar serviço de segurança (Defender, Firewall, Atualização do Windows, Centro de Segurança), nem qualquer serviço: nesta função **nenhum serviço é alterado**.
- Nada de "otimizadores" agressivos: limpar o registro, desligar o arquivo de paginação, o SmartScreen ou o UAC, mexer em HKLM, desligar a telemetria (isso é a função de privacidade).
- Não remover componentes do sistema nem do Windows Update, não apagar arquivos do sistema, não usar o dism.
- Não encerrar processo, nem desligar item de inicialização ou tarefa agendada: o toolkit só mostra; quem decide é você.
- Sem "pontuação de PC" nem promessa de ganho de desempenho.

O "o que fazer" é sempre texto (ex.: "abra Configurações > Aplicativos > Inicialização e desligue os que você não usa").

## Segurança

Só leitura (`ReadOnly`, sem `Elevate`), sem escrita em disco (o `--relatorio` grava só o arquivo que você pede: novo, 0600, sem
sobrescrever), sem rede. Nome de processo, de serviço e de item de inicialização vêm do sistema e são limpos em três camadas:
no parse (controle, ESC, bidi e separadores viram espaço, até 80 letras), na saída textual (`textsafe.Multiline`) e no `--json`
(`jsonSafe`). Listas curtas (25 itens; a contagem é sempre a inteira) e teto de itens lidos.

## TUI

Área 2 Máquina, seção **Otimização** (3ª, depois de Apps nativos; abre também pelo objetivo "Deixar o Windows mais leve" do
Início, com a etiqueta `! só Windows` fora do Windows). Não lê nada ao abrir a TUI (a amostra de CPU concorreria com as outras
leituras): `c` (ou Enter) lê, `Esc` cancela uma leitura em andamento, `c` lê de novo, `↑↓`/`g G`/PgUp PgDn rolam o resumo e os dados
completos. A partir de 110 colunas o painel de detalhe mostra os limiares e a lista do que o toolkit nunca faz. Estados
padronizados (carregando, erro, ainda não lido, cancelado); fora do Windows: `só existe no Windows (este sistema é <SO>)`. Ver
[tui.md](tui.md).

## Animações (EXPERIMENTAL): `toolkit otimizacao animacoes`

Parte B da função 3 do [plano](plano-seguranca-e-tui.md). **Experimental** enquanto os valores não forem confirmados numa VM:
o selo aparece no título do plano, na confirmação e na seção da TUI, e o resumo do plano avisa.

```sh
toolkit otimizacao animacoes --dry-run   # só o plano (valor atual -> novo); nada muda
toolkit otimizacao animacoes             # plano, [s/N], backup, aplica (sem terminal exige --yes)
toolkit otimizacao animacoes desfazer    # plano do que volta (atual -> original), [s/N], restaura, remove o backup
```

`--dry-run` e `--yes`/`-y` existem nos dois. O plano vem sempre antes; fora do Windows os comandos terminam com `só existe no
Windows` sem tocar em nada. Código: `internal/animacoes` (núcleo), `internal/cli/otimizacao_animacoes.go` (comando) e
`internal/cli/tui_otimizacao_animacoes.go` + `internal/tui/otimizacao_anim.go` (TUI).

**O que muda (catálogo FECHADO, só HKCU, `animacoes.Items()`).**

| Valor (HKCU) | Tipo | O toolkit grava | Efeito | Fonte |
|---|---|---|---|---|
| `Control Panel\Desktop\WindowMetrics` `MinAnimate` | REG_SZ | `"0"` | janelas minimizam e maximizam sem animação | referência de chaves de registro do Windows no TechNet (REG_SZ, 0 ou 1, padrão 1) |

Só entra no catálogo o que tem fonte oficial confirmada (teste de catálogo fechado: só HKCU, só estes valores). Nada de
`UserPreferencesMask` (máscara binária frágil), HKLM, serviços, tarefas, transparência ou energia (decisão 10 do plano).

**Backup e desfazer.** Antes da **primeira** escrita o estado original de cada valor (tipo e conteúdo, ou "não existia") vai para
`otimizacao-animacoes-backup-<SID>.json` (0600, atômico, dono = SID do usuário, em `%LOCALAPPDATA%\toolkit`; é por usuário, então
com ou sem elevação é o mesmo arquivo). O backup é conferido contra o catálogo **antes de qualquer leitura ou escrita**: entrada
que aponta para HKLM, outra chave, outro valor ou tipo desconhecido, ou backup de outro usuário, é recusado sem mudar nada.
Reaplicar não sobrescreve o original. Desfazer restaura exatamente (tipo preservado; só apaga o que não existia) e remove o
backup; se algo falha no meio, o que não foi restaurado fica no backup.

**Revalidação depois da confirmação (CLI).** Depois do `[s/N]` (e também com `--yes`, logo antes de aplicar) e ANTES de qualquer
backup ou escrita, o comando recalcula o plano e exige o MESMO texto e as mesmas pendências do que foi mostrado; no desfazer
reconfere também o backup (mesmo conteúdo). Divergência (outro processo mexeu no valor durante a pergunta) =
`o estado mudou desde o plano mostrado; nada foi alterado; veja o plano de novo`, zero mutações e backup intacto.

**Leitura do registro.** Pelo mesmo `reg.exe` da privacidade (caminho absoluto, `ReadOnlyRunner` para ler e `MutatingRunner` para
escrever, ausência de valor **provada**, nunca "exit 1 = ausente"), mas o System das animações expõe só o registro e recusa
qualquer chave que não comece com `HKCU\` antes de executar um comando (`ErrSoHKCU`). Nunca há `Elevate` e nunca administrador.

**Efeito e honestidade.** Algumas animações só valem depois de reabrir a sessão do Windows ou o Explorer; o toolkit **não**
reinicia o Explorer (só avisa em texto). Reduzir animação melhora a sensação de resposta, não prova ganho de desempenho.

**TUI.** Seção Máquina › Otimização: `m` alterna entre Gargalos (leitura, `c`) e Animações. Em Animações: `p` ver plano
(só leitura), `a` aplicar… e `d` desfazer… abrem a confirmação única (foco em Cancelar, backup e como desfazer na tela), `c` lê de
novo, `m` volta aos gargalos. O plano mostrado tem ID de uso único: a confirmação aplica só o plano exibido e só se o sistema (ou o
backup) continua igual; `Esc` cancela a preparação; o resultado vira faixa (`x` fecha); releitura com erro depois de mudar não diz
"nada foi alterado" nem promete backup. Sem estado utilizável (carregando, erro de leitura) as teclas `a`/`d`/`p` e a
confirmação são recusadas: nunca há plano armado fora da tela (só `c` e `m`).

### A VERIFICAR (animações)

- `TaskbarAnimations` (DWORD em `Explorer\Advanced`): só relatos da comunidade (0 desliga, 1 liga; o padrão aparece como 1 ou 0
  conforme a fonte). **Fora do catálogo** até haver fonte oficial e teste em VM.
- `VisualFXSetting` (DWORD em `Explorer\VisualEffects`; relatos: 0 deixar o Windows escolher, 1 melhor aparência, 2 melhor
  desempenho, 3 personalizado): só comunidade. **Fora do catálogo.**
- `SystemParametersInfo` (`SPI_SETANIMATION`, `SPI_SETCLIENTAREAANIMATION`, `SPIF_SENDCHANGE`): a API documentada aplicaria na hora e
  alimentaria o backup pela leitura `SPI_GET...`, mas este PR grava só o valor de registro (via `reg.exe`); a regra do plano de
  preferir a API fica para depois da confirmação em VM.
- Em VM: o `MinAnimate` gravado sozinho muda o comportamento sem sair da sessão? Quais animações só valem depois de reabrir a sessão
  ou o Explorer? A reversão devolve o mesmo estado visual?
- A pasta do backup quando o usuário que eleva é outro (SID e `%LOCALAPPDATA%` diferentes): coberto pelo dono gravado no arquivo,
  mas não testado numa máquina real.
