# `toolkit diagnostico`

Lê o estado de saúde da máquina **só para leitura, sem alterar nada e sem pedir administrador**, e responde "o que está
ruim e o que fazer". Cobre hardware, discos (com SMART quando o SO deixa ler sem privilégio), bateria, temperatura, GPU
e os erros e falhas recentes do sistema.

```sh
toolkit diagnostico                       # resumo: o que está ruim e o que fazer
toolkit diagnostico --completo            # resumo + relatório completo (todos os dados lidos), mascarado
toolkit diagnostico --json                # o mesmo em JSON, mascarado
toolkit diagnostico --relatorio rel.txt   # grava resumo + relatório completo (mascarados) num arquivo NOVO (0600)
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
| Hardware | `Win32_ComputerSystem`, `Win32_Processor`, `Win32_BIOS`, uptime | `/sys/class/dmi/id` (fabricante, modelo, BIOS; nunca série/UUID), `/proc/cpuinfo`, `/proc/uptime` | `sysctl` (modelo, CPU, núcleos, boottime), `system_profiler SPHardwareDataType` |
| Memória | `Win32_OperatingSystem`, `Win32_PageFileUsage` | `/proc/meminfo` | `sysctl hw.memsize`, `vm_stat`, `vm.swapusage` |
| Discos | `Get-PhysicalDisk`, `Get-Volume` (espaço livre) | `lsblk -b -d -J`, `df -B1` | `system_profiler SPNVMeDataType SPSerialATADataType SPStorageDataType` |
| SMART | `Get-StorageReliabilityCounter` (temperatura, desgaste, horas) quando legível sem admin | **não lido** (exige root): a instrução é `sudo smartctl -H -A /dev/sdX` ou `sudo nvme smart-log /dev/nvme0` | "Verified"/"Failing" do `system_profiler` |
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

## Dados sensíveis

O relatório completo, o `--json` e o arquivo de `--relatorio` passam por `diagnostico.MaskText`, que mascara: nome do
usuário e nome da máquina (e variáveis `USER`, `USERNAME`, `COMPUTERNAME`, `HOSTNAME`) como `[pessoal]`; pastas pessoais
(`C:\Users\NOME`, `/Users/NOME`, `/home/NOME`) como `[usuário]`; e-mails; compartilhamentos de rede (`\\SERVIDOR\`);
identificadores da máquina (número de série, UUID/GUID, UDID, ECID, WWN, IMEI, MAC: `diagfmt.Redact`) como `[***]`; e IPs
públicos (`A.x.x.x`; IPv6 global `2001:x:x`; IPs privados ficam). Os coletores nem pedem série/UUID/MAC/hostname ao sistema,
mensagens de evento entram só na primeira linha e os relatórios de falha do macOS nunca são abertos. O arquivo de
`--relatorio` é criado com permissão 0600 e o comando **nunca sobrescreve** um arquivo que já existe.

## Testes

`internal/diagnostico` (coletores com saídas falsas por SO, regras, relatório, máscara), `internal/cli/diagnostico_test.go`
(comando, máscara no `--completo`/`--json`/`--relatorio`, não sobrescreve) e `internal/diagfmt/redact_test.go`. Nenhum teste
toca a máquina real; todo `exec.Cmd` dos coletores é `ReadOnly` e sem `Elevate` (verificado nos testes).

## Limites

O SMART no Linux e a temperatura no macOS dependem de administrador e por isso aparecem como "não lido" com a instrução
(o toolkit não eleva privilégio na leitura). No Windows a temperatura e o SMART dependem do que o firmware expõe sem
admin. O `log show` do macOS lê só as últimas 6 horas (as 24 horas passam de 90 s num Mac em uso). Os coletores do Windows
e do Linux seguem os formatos documentados dos comandos e foram testados com saídas falsas; o do macOS também foi rodado
no Mac do desenvolvimento. O smoke manual no Windows e no Linux ainda está por fazer.
