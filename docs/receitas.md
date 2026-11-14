# Receitas

> Formato decidido ([decisoes.md](decisoes.md)). Fase 3 implementada: parse/validação e engine (`internal/recipes`), estado (`internal/state`), retomada por SO (`internal/resume`) e os comandos `recipe` e `resume`. Falta o smoke em máquina real (WSL e reboot de verdade). Detalhes da engine em
> [arquitetura.md](arquitetura.md#34-receitas-e-engine).

Uma receita é uma sequência de etapas para um SO, num arquivo YAML em `recipes/<so>/<id>.yaml`. Serve
para setups que levam mais de um comando (WSL, ambiente de dev, Office pelo ODT, impressoras) e para
diagnósticos.

## Estrutura

```yaml
id: <id-da-receita>            # igual ao nome do arquivo
name: <nome exibido>
description: <uma frase>       # opcional
os: windows | linux | macos
kind: setup | diagnostic       # padrão: setup
requires_admin: true | false   # padrão: false
steps:
  - id: <id-da-etapa>
    name: <nome exibido>       # opcional
    check: <ação>              # opcional: saiu com 0 = já está feito, etapa pulada
    run: <ação>                # exatamente um entre run e install
    install: [<app>, ...]      # ids do catálogo, aceita "python@3.12"
    render: <nome>             # só diagnóstico, com run: resumo legível da saída (ver "Resumo legível")
    reboot: never | required | if-needed   # padrão: never
    timeout: 30m               # opcional
```

### Ações

Cada `check` ou `run` tem exatamente um destes campos:

| Campo | Executa | SO |
|---|---|---|
| `powershell` | `powershell.exe -NoProfile -NonInteractive -EncodedCommand ...` | Windows |
| `sh` | `/bin/sh -c ...` | Linux, macOS |
| `bash` | `bash -c ...` | Linux, macOS |
| `wsl` | `wsl.exe -d <distro> [-u <user>] -- bash -c ...` | Windows |

`success_codes` aceita exit codes além do 0, por exemplo `[3010]` para o dism que pede reboot.

**PowerShell e exit code.** O `powershell.exe -EncodedCommand` só devolve um exit code diferente de 0 e 1 se o script terminar com `exit N`; uma expressão booleana (`... -eq 'Enabled'`) sai sempre com 0. Por isso todo `check` em PowerShell deve terminar com `exit 0` / `exit 1`, e um `run` com `success_codes` deve terminar com `exit $LASTEXITCODE`.

### Reboot

- `never`: nunca reinicia.
- `required`: reinicia sempre que a etapa rodar (não reinicia se ela foi pulada pelo `check`).
- `if-needed`: reinicia só se o `run` sair com 3010 ou se algum app do `install` pedir reboot.

## Ciclo de execução e retomada

1. Antes de cada etapa, o estado é salvo como `running`. Depois, como `done`, `skipped` ou `failed`.
2. Quando uma etapa pede reboot, o toolkit, nesta ordem: salva `awaiting-reboot`, registra a retomada,
   pede confirmação (ou segue direto com `--yes`) e reinicia. Nunca há reboot sem a retomada registrada.
3. Depois do boot, `toolkit resume` continua da etapa pendente:
   - Windows: Tarefa Agendada (`Register-ScheduledTask`) no logon do usuário que iniciou a receita, com
     privilégio elevado quando a receita é `requires_admin`. O binário é copiado para a pasta `bin` do
     toolkit antes de registrar.
   - Linux (segurança): o root do boot só executa e lê o que é de root. O binário é COPIADO para
     `/usr/local/lib/toolkit/toolkit` (root:root 0755), nunca o `~/.local/bin` de quem iniciou a receita; o
     `state.json` e o log têm de ficar em `/var/lib` (ou `/var/log`). O binário é lido uma vez e entregue por stdin, com SHA-256 conferido na cópia e de novo antes do `enable`; o `state.json` e os diretórios até `/var/lib` têm de ser de root (uid 0, sem escrita de grupo/outros). Fora disso o registro é recusado
     (`ErrUnsafeRootPath`) antes de qualquer comando, e a máquina não reinicia: receita com reboot no
     Linux exige rodar com `sudo`.
   - Linux: unit systemd de sistema (`toolkit-resume.service`), oneshot, no boot e depois de
     `network-online.target`, com `ConditionPathExists` no `state.json` e `ProtectHome=true`. Roda como root e sem terminal
     (`toolkit resume --no-tui`); sem `--yes` um reboot novo é recusado e a receita espera o próximo boot.
     O resultado fica no log e em `toolkit recipe status`. Caminhos com espaço não são aceitos.
   - macOS: LaunchAgent do usuário (`RunAtLoad`, `LaunchOnlyOnce`), que abre o Terminal com um
     `.command` para mostrar o progresso. Só no login: não é no boot.
   - Nomes: a tarefa agendada e o label do launchd usam `dev.pilutech.<name>`.
4. Ao terminar, com sucesso ou falha, o registro de retomada é removido.

Proteções:

- Se o `resume` rodar sem que a máquina tenha reiniciado de fato (o boot ID não mudou), nada avança.
- Cada etapa tem 1 tentativa depois da retomada. Se o `check` falhar depois do reboot, ou a etapa falhar,
  a receita para, desregistra a retomada e reporta. Não há segundo reboot. `toolkit recipe reset <id>`
  libera uma nova tentativa manual.
- Se o usuário recusar o reboot, a retomada continua registrada e a receita fica `awaiting-reboot`. Ela
  continua no próximo boot (Linux) ou logon (Windows e macOS).
- Se o YAML da receita mudar entre os boots (binário atualizado), a retomada para em vez de continuar
  uma receita diferente.
- Uma etapa que ficou `running` (queda de energia) só é repetida se tiver `check`. Sem `check`, ela
  vira `failed` e pede intervenção.

Onde fica o estado:

| SO | Admin/root | Usuário |
|---|---|---|
| Windows | `%ProgramData%\toolkit\state.json` | — |
| Linux | `/var/lib/toolkit/state.json` | `$XDG_STATE_HOME/toolkit/state.json` |
| macOS | `/Library/Application Support/toolkit/` | `~/Library/Application Support/toolkit/` |

O log de cada execução (`logs/<receita>-<data>.log`, permissão 0600) fica ao lado do estado e guarda só
eventos (etapa, código de saída, erro), nunca a saída dos comandos. `--verbose` também o mostra no terminal.
`toolkit recipe status` lê o estado do privilégio atual (admin ou usuário).

### Privilégio

- **Windows:** receita `requires_admin` exige terminal elevado (o relançamento automático com UAC ainda
  não existe). O toolkit descobre a elevação perguntando ao PowerShell.
- **Linux:** a retomada roda como root e lê `/var/lib/toolkit`. Por isso uma receita que pode reiniciar
  (`reboot` diferente de `never`) tem de ser iniciada com `sudo`. Receita sem reboot roda como usuário e
  usa `sudo -n` só nos comandos de uma receita `requires_admin`.
- **macOS:** tudo como usuário (o brew não roda como root).

### `--dry-run`

Só os `check` rodam (leitura, nunca elevados); o resto é descrito. Não cria estado, log nem retomada e
nunca reinicia. Um `check` precisa ser só leitura: o dry-run o executa de verdade.

## Comandos

```sh
toolkit recipe list [--os windows|linux|macos] [--json]
toolkit recipe run wsl-dev [--yes] [--dry-run] [--verbose]
toolkit recipe status [--json]
toolkit recipe reset wsl-dev     # descarta o andamento: libera uma nova tentativa depois de uma falha
toolkit resume [--yes] [--verbose] [--no-tui]
```

## Exemplos

### Setup com reboot (Windows)

É a `recipes/windows/wsl-dev.yaml` embutida no binário (resumida aqui). Repare que cada `check` em
PowerShell termina com `exit 0` / `exit 1`, e que o `run` com `success_codes` termina com `exit $reboot`.

```yaml
id: wsl-dev
name: Ambiente WSL + Ubuntu para dev
os: windows
requires_admin: true
steps:
  - id: enable-features
    check: { powershell: "if ((Get-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform).State -eq 'Enabled') { exit 0 } else { exit 1 }" }
    run:
      powershell: |-
        $reboot = 0
        foreach ($f in 'VirtualMachinePlatform', 'Microsoft-Windows-Subsystem-Linux') {
          dism /online /enable-feature /featurename:$f /all /norestart
          if ($LASTEXITCODE -eq 3010) { $reboot = 3010 }
          elseif ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        }
        exit $reboot
      success_codes: [3010]
    reboot: if-needed
  - id: install-distro
    check: { powershell: "if (wsl -l -q | Select-String -Quiet Ubuntu) { exit 0 } else { exit 1 }" }
    run:   { powershell: "wsl --install -d Ubuntu --no-launch" }
  - id: apps
    install: [git, vscode]
```

A etapa `setup-inside` (rodar o `unix.sh` da mesma versão dentro da distro, com hash conferido) entra
junto com os bootstraps, na fase 6.

### Setup simples (Linux)

```yaml
id: linux-dev
name: Ferramentas de dev no Linux
os: linux
steps:
  - id: apps
    install: [git, python@3.12]
  - id: git-defaults
    check: { sh: "git config --global init.defaultBranch" }
    run:   { sh: "git config --global init.defaultBranch main" }
```

### Diagnóstico (Windows)

Diagnósticos são receitas com `kind: diagnostic`, sem reboot. A saída de cada etapa vai para o relatório
mostrado na aba Diagnóstico.

```yaml
id: diag-memoria
name: Diagnóstico de memória
os: windows
kind: diagnostic
steps:
  - id: modulos
    run: { powershell: "Get-CimInstance Win32_PhysicalMemory | Select-Object Manufacturer,PartNumber,Capacity,Speed | Format-Table -AutoSize" }
  - id: disco
    run: { powershell: "Get-PhysicalDisk | Select-Object FriendlyName,MediaType,HealthStatus,OperationalStatus | Format-Table -AutoSize" }
```

### Resumo legível dos diagnósticos (`render`)

Uma etapa de diagnóstico pode ter `render: <nome>`: a saída crua do comando (JSON do PowerShell, `/proc/meminfo`,
`system_profiler`...) passa por um renderizador de `internal/diagfmt` e o relatório mostra um **resumo** em pt-BR, ou em
`en`/`es` por `TOOLKIT_LANG`, `LC_ALL`, `LC_MESSAGES` ou `LANG` (o primeiro valor não vazio decide; outro idioma cai em
pt-BR). A saída original só aparece no **modo detalhes**: tecla `d` na aba Diagnóstico da TUI (a dica mostra
`d detalhes ✓` quando ligado; vale para os diagnósticos marcados na próxima execução) ou `toolkit recipe run <id>
--detalhes`; ela vem depois do resumo, sob `-- Detalhes (saída original, sem os identificadores da máquina) --`. **Identificadores nunca
saem:** antes de qualquer saída da etapa (detalhes, falha do comando e o log do smoke de teste) o toolkit remove número
de série, UUID/GUID, UDID, ECID, WWN, IMEI, endereço MAC e afins (`diagfmt.Redact`: o valor vira `[***]`, a chave fica),
inclusive em JSON truncado. JSON válido é lido de verdade e redigido por chave (aspas escapadas, unicode, valores de qualquer
tipo); o resto (truncado, tabela do `Format-Table` ou de colunas alinhadas, linhas `Chave: valor`) passa por um fallback
conservador por padrões, e UUIDs e MACs soltos somem sempre. O RESUMO também é redigido no último ponto antes da saída (um
nome de disco com UUID ou MAC não reaparece), com e sem detalhes e no smoke de teste. As consultas do Windows nem pedem
`SerialNumber` nem UUID (os campos são escolhidos um a um no `Select-Object`); o `system_profiler` do macOS traz os
seus de qualquer jeito, e por isso a redação. Se o renderizador não entender a saída (formato novo do sistema), a etapa NÃO falha, mas a
saída crua NÃO aparece por padrão: só o aviso "não deu para resumir esta saída; use --detalhes ... para ver a original
sem os identificadores da máquina". O `render`
só vale em etapa `run` de receita `kind: diagnostic`; um nome desconhecido é erro de carga. Renderizadores:

| Nome | Entrada | Resumo |
|---|---|---|
| `windows-memory` | JSON de `Win32_PhysicalMemory`, `Win32_PhysicalMemoryArray` e `Win32_OperatingSystem` | `Memória: 8 GB no total (1 de 2 slots usados) · DDR4 2400 MT/s`, um `Slot N` por módulo (capacidade · fabricante · peça) e `Em uso agora: 5,1 de 7,9 GB (65%)` |
| `windows-disks` | JSON de `Get-PhysicalDisk` | `Disco: LITEON CV8-CE256-HP · SSD · 256 GB · saudável` (Healthy = saudável, Warning = atenção, Unhealthy = com problema) |
| `linux-memory` | linhas de `/proc/meminfo` | total, em uso (total menos `MemAvailable`) e swap; os pentes exigem root e não são mostrados |
| `linux-disks` | JSON de `lsblk -b -d -J` | uma linha por disco (SSD/HDD/NVMe e tamanho); a saúde SMART exige root e não é verificada |
| `macos-memory` | `sysctl hw.memsize`, `vm_stat` e `system_profiler SPMemoryDataType` | total, tipo quando o macOS informa, em uso aproximado |
| `macos-disks` | `system_profiler` (NVMe, SATA, armazenamento) | `Disco: <modelo> · SSD · 500 GB · S.M.A.R.T.: verificado` |

Capacidade de memória usa GB binário (8589934592 bytes = 8 GB, como o Windows mostra) e a de disco, GB decimal (como o
fabricante anuncia); decimais com vírgula em pt-BR e es e com ponto em en. Fabricante da RAM no Windows vem de uma tabela de
códigos JEDEC comuns (80AD SK hynix, 80CE Samsung, 802C Micron, 859B Crucial, 0198 Kingston, 029E Corsair, 04CB A-DATA, 04EF Team Group,
04CD G.Skill, 830B Nanya); um código fora da tabela aparece como `JEDEC <código>` exatamente como veio (só o preenchimento
provado de 16 dígitos, código de 4 mais 12 zeros, é descartado; `8010` nunca vira `0801`). O tipo da RAM vem do `SMBIOSMemoryType`
(DSP0134, Type 17: 20 é DDR2 FB-DIMM, 21 a 23 são reservados) e, só quando ele vem 0, do `MemoryType` legado do WMI (20 é DDR):
são tabelas diferentes e não se misturam. No macOS o disco é identificado pelo nome BSD (`disk0`): a mesma identidade em
duas seções do `system_profiler` vira um disco (vale o pior status SMART) e dois discos do mesmo modelo continuam dois;
a visão de volumes (Storage) só se junta a um disco NVMe/SATA com identidade FÍSICA comprovada: o disco inteiro do volume
(`disk0s2` é do `disk0`) ou, para volume de contêiner APFS (`disk3s5`, do contêiner `disk3`), o disco físico do contêiner
pelo mapa do `diskutil apfs list -plist` (a receita emite as seções `### profiler` e `### apfs`: `disk3` -> `disk0s2` ->
`disk0`). Ter o MESMO MODELO nunca prova nada (um externo igual ao interno é outro disco). Sem prova o volume vira disco
próprio, um por disco físico (`disk6s1` e `disk6s2` são do `disk6`; `disk6` e `disk7` são dois), e sem `bsd_name` fica
sozinho. O mapa APFS é um complemento OPCIONAL: o `diskutil` só entra na seção `### apfs` quando termina com sucesso (ausente
ou com erro, a seção sai vazia, a etapa NÃO falha e o resumo sai só com o profiler; só a falha do `system_profiler` é a falha
da etapa). O plist é lido com XML estrito (precisa fechar `</plist>` e terminar: truncado, mesmo depois de um dict completo, não
vale). Sem o mapa um volume de contêiner não se junta a ninguém e o disco interno pode aparecer também como disco do Storage. As receitas continuam só de leitura e sem
administrador. Cada etapa do Log da TUI sai como `✓ Memória` (no lugar de `etapa modulos: done`).

Testes: `internal/diagfmt/*_test.go` (saídas falsas: código JEDEC desconhecido, slot vazio, 2+ módulos, formatos truncados,
en/es), `internal/recipes/render_test.go` (engine: resumo, detalhes, idioma, renderizador que falha) e
`internal/recipes/diagmemoria_real_test.go`, que roda a etapa de verdade (só leitura) no runner e confere que o renderizador
entende a saída real (no Windows isto acontece no CI, no `windows-latest`).

### Diagnóstico de rede (Linux e macOS): `diag-rede`

`toolkit recipe run diag-rede` (ou a aba Diagnóstico da TUI). **Só leitura**: nada de `sudo`, nada que mude rede, DNS,
firewall ou rotas, nada gravado em arquivo. Cinco etapas, cada uma com saída resumida:

| Etapa | Linux | macOS |
|---|---|---|
| `interfaces` | `ip -brief address`, estado e MTU (`ip -o link`), rota padrão (IPv4 e IPv6) | `ifconfig -a` (estado, MTU e endereços das interfaces com endereço), `route -n get default` |
| `dns` | servidores em uso (`resolvectl status` ou `/etc/resolv.conf`) e resolução de um nome (`getent`, `host` ou `nslookup`) | servidores do `scutil --dns` (sem os resolvedores com escopo) e resolução por `dscacheutil` |
| `alcance` | ping ao gateway e a um alvo, e uma requisição HTTPS (`curl`) com tempos de TCP, TLS e total | o mesmo (`ping -t 6`) |
| `wifi` | adaptador e sinal/canal/taxa pelo NetworkManager (`nmcli`); sem NetworkManager, avisa e segue | adaptador e energia (`networksetup`); o nome da rede não aparece |
| `portas` | portas TCP em escuta (`ss -ltn`) | portas TCP em escuta (`netstat -an -p tcp`) |

**Alvos externos** (variáveis de ambiente, todas opcionais; o valor é validado e um valor inválido é recusado na
etapa sem chamar nada):

| Variável | Padrão | Aceita |
|---|---|---|
| `TOOLKIT_DIAG_HOST` | `1.1.1.1` | letras, dígitos, `.`, `:`, `_`, `-`; nunca começa com `-` |
| `TOOLKIT_DIAG_NAME` | `example.com` | letras, dígitos, `.`, `_`, `-`; nunca começa com `-` |
| `TOOLKIT_DIAG_URL` | `https://example.com/` | só `https://`, com host e caminho em letras, dígitos e `. _ ~ : / -` |

O `curl` roda como `env -u SSLKEYLOGFILE curl -q ...`: o `-q` (primeiro argumento) impede a leitura da `.curlrc` do usuário
(que poderia mandar corpo, `Authorization`, `request`, gravar `cookie-jar` ou `trace`) e a variável de chave TLS não
chega a ele; só segue redirecionamentos para `https` (`--proto-redir =https`), no máximo 3, com `--connect-timeout 5` e
`--max-time 10`. O corpo da resposta é descartado. O `nslookup` (último recurso no Linux) roda com `HOME=/`, sem ler o
`~/.nslookuprc`; `ping`, `ip`, `ss`, `getent`, `nmcli`, `route` e `netstat` não leem configuração do usuário.

**Dados sensíveis.** A saída mostra no terminal (a engine nunca grava a saída dos comandos no log) e mesmo assim:
o MAC nunca aparece; o nome da rede Wi-Fi (SSID) vira `[nome oculto]`; endereços IPv4 públicos viram `A.x.x.x`
(privados, loopback, link-local e CGNAT continuam) e IPv6 globais viram `2001:x:x`; não há consulta de "meu IP
público"; as portas aparecem sem endereço (só "só esta máquina" ou "toda a rede"). As respostas da resolução de DNS e as
caudas de erro do ping e do curl passam pela mesma máscara (se o nome resolver para a própria máquina, o IP público
não reaparece). Exceção documentada: os alvos configurados, os servidores DNS e o gateway aparecem como são.

**Idioma.** A saída sai em pt-BR por padrão e em `en` ou `es` por `TOOLKIT_LANG`, `LC_ALL`, `LC_MESSAGES` ou `LANG`
(nessa ordem; `pt_BR`, `en_US`, `es_ES`... e qualquer outro idioma cai em pt-BR). Cada etapa tem um bloco
`case "$lang"` com as mesmas chaves `m_*` nos três idiomas (um teste confere que as chaves são idênticas e que toda
`$m_x` usada está traduzida). O nome e a descrição da receita no YAML ainda são só pt-BR (o formato de receita não tem
campo de idioma; ver `docs/auditoria-a11y-l10n.md`, L5).

**Limites.** Um diagnóstico nunca falha por causa do achado: rede fora do ar, ping sem resposta ou falta de uma
ferramenta aparecem na saída (`falhou`, "o comando 'x' não existe") e a etapa termina com status 0. No macOS, o nome
da rede Wi-Fi não é lido de propósito; o sistema também pode dizer "sem conexão" quando a permissão de Localização não
foi dada. A `diag-rede` do Windows é mais simples (adaptadores, endereços e DNS) e ainda não mascara nem aceita alvos.

**Testes** (`internal/recipes/diagrede_test.go`, sem rede real): validação do YAML por plataforma; lista de comandos
proibidos (nada de `sudo`, escrita de rede, redirecionamento para arquivo); sintaxe `sh -n`; chaves de idioma; e a
execução de cada etapa em `/bin/sh` com um PATH que só tem ferramentas de texto reais e `ip`, `ping`, `curl`,
`networksetup` etc. FALSAS, que registram cada chamada (toda chamada tem de estar na lista de leitura permitida):
máscara de IP/MAC/SSID, idioma, alvos configurados, alvos inválidos recusados, falha de rede sem erro e ausência de
ferramentas. Os testes dos scripts não rodam no Windows.

## Regras para receitas

- Nada de `curl | bash` de terceiros. Scripts só do próprio repositório, na versão do binário em
  execução e com hash conferido.
- Nada que ative ou contorne licenciamento.
- Prefira `install:` (catálogo) a baixar instalador no `run`. Se precisar baixar, use um app `url` ou
  `github-release` no catálogo, que exige hash.
- Toda etapa que altera o sistema deve ter `check`, para a receita ser idempotente e a retomada segura.
