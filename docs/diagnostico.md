# `toolkit diagnostico`

Lê o estado de saúde da máquina **só para leitura, sem alterar nada**. Funciona sem administrador e, quando você abre o terminal como administrador, lê mais coisas (veja "Administrador" abaixo). Responde "o que está
ruim e o que fazer". Cobre hardware, discos (com SMART quando o SO deixa ler sem privilégio), bateria, temperatura, GPU
e os erros e falhas recentes do sistema.

```sh
toolkit diagnostico                       # resumo: o que está ruim e o que fazer
toolkit diagnostico --completo            # resumo + relatório completo (todos os dados lidos)
toolkit diagnostico --json                # o mesmo em JSON
toolkit diagnostico --relatorio rel.txt   # grava resumo + relatório completo num arquivo NOVO (0600)
```

O andamento ("Lendo o estado da máquina…") vai para o stderr; `--json` não escreve andamento. O comando sempre sai com 0
(achado ruim é informação, não falha do comando). O idioma da saída vem de `TOOLKIT_LANG`, `LC_ALL`, `LC_MESSAGES` ou
`LANG` (pt-BR por padrão; en e es).

## O que cada SO lê

Tudo passa pelo `exec.Runner` de leitura (`exec.ReadOnlyRunner`: um comando que não seja leitura ou que peça elevação é
recusado) e pelas funções de arquivo injetadas. Cada área é independente: uma que falha vira "não lido" (com o motivo e a
instrução) e o resto continua.

| Área | Windows (PowerShell, sem admin) | Linux | macOS |
|---|---|---|---|
| Hardware | `Win32_ComputerSystem` (nome da máquina), `Win32_Processor`, `Win32_BIOS` (série), `Win32_ComputerSystemProduct` (UUID), uptime; `Get-NetAdapter -Physical` (MAC) | `/sys/class/dmi/id` (fabricante, modelo, BIOS, número de série, UUID), `/proc/sys/kernel/hostname`, `/sys/class/net` (MAC das placas físicas), `/proc/cpuinfo`, `/proc/uptime` | `sysctl` (modelo, CPU, núcleos, boottime), `system_profiler SPHardwareDataType` (série, UUID), `scutil --get ComputerName`, `networksetup -listallhardwareports` (MAC) |
| Memória | `Win32_OperatingSystem`, `Win32_PageFileUsage` | `/proc/meminfo` | `sysctl hw.memsize`, `vm_stat`, `vm.swapusage` |
| Discos | `Get-PhysicalDisk`, `Get-Volume` (espaço livre) | `lsblk -b -d -J` (com série do disco), `df -B1` | `system_profiler SPNVMeDataType SPSerialATADataType SPStorageDataType` |
| SMART | `Get-StorageReliabilityCounter` (temperatura, desgaste, horas) quando legível sem admin | sem root: **não lido**, com a instrução `sudo toolkit diagnostico`; como root: `smartctl --json=o -H -A /dev/<disco>` (precisa do pacote smartmontools) | "Verified"/"Failing" do `system_profiler` |
| Bateria | `Win32_Battery`, `root\wmi` (capacidade de projeto e atual) | `/sys/class/power_supply/BAT*` | `system_profiler SPPowerDataType`, `pmset -g batt` |
| Temperatura | `MSAcpi_ThermalZoneTemperature` quando legível; senão "não lido" | `/sys/class/thermal`, `/sys/class/hwmon` | **não lido** (exige root): `sudo powermetrics --samplers smc -n 1` |
| GPU | `Win32_VideoController` | `lspci -mm` (nome do produto, sem endereço) ou `/sys/class/drm` | `system_profiler SPDisplaysDataType` |
| Erros recentes | `Get-WinEvent` (System e Application, níveis crítico e erro, 7 dias), agrupados por provedor + id | `journalctl -p err -b`, com `dmesg` de reserva (pode exigir o grupo `systemd-journal`/`adm`) | `log show --last 6h --predicate 'messageType == error'` (agrupado por processo; leitura limitada a 2 MB) |
| Falhas graves | tela azul (evento 1001 do WER, com o código de parada), desligamento inesperado (Kernel-Power 41), falha de aplicativo (Application Error 1000), índice de confiabilidade | `Kernel panic`, `Oops`, `segfault`, `Out of memory: Killed process` no journal | relatórios de `DiagnosticReports` (só os NOMES e datas dos arquivos; o conteúdo nunca é lido): `.panic` e `kernel*` são panic, o resto é falha de aplicativo |

## Resumo: regras e limiares

Cada achado tem gravidade (✗ problema, ! atenção, - info, ✓ ok), o que está ruim e **o que fazer**. Sem nenhum problema, o
resumo diz "Nada de ruim encontrado". Os problemas saem primeiro.

| Regra | Atenção | Problema |
|---|---|---|
| Disco: saúde informada pelo próprio disco | `Warning` | `Failing`/`Unhealthy` |
| SSD: desgaste | ≥ 80% | ≥ 90% |
| Setores realocados | ≥ 1 | ≥ 100 |
| Espaço livre do disco | < 10% | < 5% |
| Temperatura do disco | ≥ 65 °C | ≥ 75 °C |
| Temperatura de CPU/placa | ≥ 80 °C | ≥ 90 °C |
| Bateria: capacidade restante | < 60% | < 40% |
| Bateria: condição do macOS | `Service Recommended` | `Replace Soon`/`Replace Now` |
| Memória em uso / swap em uso | ≥ 90% / ≥ 80% | (só atenção) |
| Erro repetido no log | ≥ 20 ocorrências no período | (só atenção) |
| Tela azul (30 dias) | ≥ 1 | ≥ 3 |
| Desligamento inesperado (30 dias) | ≥ 1 | (só atenção) |
| Kernel panic | (sempre problema) | ≥ 1 |
| Mesmo aplicativo falhando | ≥ 5 vezes | (só atenção) |
| Índice de confiabilidade do Windows | < 5 | < 3 |

O que não pôde ser lido (administrador, ferramenta ausente, log restrito) aparece como `- info: Não foi possível ler: …`
com a instrução (por exemplo, `sudo smartctl …`), nunca como silêncio.

## Informações como o sistema informa

O toolkit é uma ferramenta de **análise**: o resumo, o `--completo`, o `--json` e o arquivo de `--relatorio` mostram as
informações da máquina **exatamente como o sistema as informa**, sem máscara nenhuma (nome do usuário e da máquina, pastas
pessoais, e-mails, IPs, UUIDs, MACs e números de série aparecem inteiros quando o sistema os informa). O único tratamento é
deixar os números legíveis (tamanhos em GB, frequências em MT/s, porcentagens). Duas coisas continuam por segurança do
arquivo, e nenhuma esconde dado: o `--relatorio` cria o arquivo com permissão 0600 e **nunca sobrescreve** um arquivo que já
existe. Os coletores leem número de série (máquina e discos), UUID, MAC das placas físicas e nome da máquina, e o relatório os mostra
(seções "Rede (placas físicas)" e identificadores no hardware); valores de firmware sem sentido ("To Be Filled By O.E.M.",
zeros) viram vazio; mensagens de evento entram só na primeira linha, e os relatórios de falha do
macOS nunca são abertos (só os nomes dos arquivos). Como o relatório mostra tudo, **trate o arquivo gerado como dado pessoal**
antes de compartilhar.

Única limpeza que fica (não esconde dado, protege o terminal): todo texto que vem do sistema (nome de disco, modelo, mensagem de log)
passa por `textsafe.Multiline` no limite da saída (resumo, `--completo`, arquivo de `--relatorio` e todo erro do comando): LF e Tab e os
dados legíveis ficam; ESC, BEL, CSI, OSC (como o OSC 52 que escreve na área de transferência), C1 e bidi saem escapados e
visíveis (`\x1b`), sem efeito. No `--json` os caracteres C1 e de formatação viram `\uXXXX` (JSON válido, mesmo dado).

## Administrador

O toolkit **nunca eleva sozinho**: não usa `sudo` nem pede senha, e todo comando de leitura é `ReadOnly`. Rode o comando já como
administrador (`sudo toolkit diagnostico` no Linux/macOS; terminal "Executar como administrador" no Windows) para ler mais:

| SO | Só com administrador |
|---|---|
| Linux | SMART via `smartctl` (saúde, horas ligado, setores realocados, desgaste NVMe) e, quando o firmware restringe, série e UUID de `/sys/class/dmi/id` |
| macOS | temperaturas pelo `powermetrics` |
| Windows | contadores de confiabilidade do disco (`Get-StorageReliabilityCounter`; o total de erros de leitura e os não corrigidos aparecem em campos próprios, nunca como setores realocados; os não corrigidos geram um aviso) |

Sem administrador tudo continua funcionando; o que ficou de fora aparece no resumo como "Com administrador o diagnóstico também
lê: …" com o jeito de rodar, e no `--completo` como "não lido" com `NeedsAdmin`. Com administrador e sem dado (hardware ou driver
não expõe), o aviso diz isso em vez de pedir administrador de novo.

## Testes

`internal/diagnostico` (coletores com saídas falsas por SO, regras, relatório), `internal/cli/diagnostico_test.go`
(comando; `--completo`, `--json` e `--relatorio` sem máscara; não sobrescreve). Nenhum teste
toca a máquina real; todo `exec.Cmd` dos coletores é `ReadOnly` e sem `Elevate` (verificado nos testes).

## Limites

O SMART no Linux e a temperatura no macOS dependem de administrador (veja "Administrador"); no Apple Silicon o `powermetrics`
quase nunca expõe sensores de temperatura. No Windows a temperatura e o SMART dependem do que o firmware e o driver expõem. O `log show` do macOS lê só as últimas 6 horas (as 24 horas passam de 90 s num Mac em uso). Os coletores do Windows
e do Linux seguem os formatos documentados dos comandos e foram testados com saídas falsas; o do macOS também foi rodado
no Mac do desenvolvimento. O smoke manual no Windows e no Linux ainda está por fazer.
