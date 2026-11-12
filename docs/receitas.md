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

## Regras para receitas

- Nada de `curl | bash` de terceiros. Scripts só do próprio repositório, na versão do binário em
  execução e com hash conferido.
- Nada que ative ou contorne licenciamento.
- Prefira `install:` (catálogo) a baixar instalador no `run`. Se precisar baixar, use um app `url` ou
  `github-release` no catálogo, que exige hash.
- Toda etapa que altera o sistema deve ter `check`, para a receita ser idempotente e a retomada segura.
