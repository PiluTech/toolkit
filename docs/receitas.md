# Receitas

> Formato decidido ([decisoes.md](decisoes.md)), ainda sem implementação (fase 3). Detalhes da engine em
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

### Reboot

- `never`: nunca reinicia.
- `required`: reinicia sempre que a etapa rodar (não reinicia se ela foi pulada pelo `check`).
- `if-needed`: reinicia só se o `run` sair com 3010 ou se algum app do `install` pedir reboot.

## Ciclo de execução e retomada

1. Antes de cada etapa, o estado é salvo como `running`. Depois, como `done`, `skipped` ou `failed`.
2. Quando uma etapa pede reboot, o toolkit, nesta ordem: salva `awaiting-reboot`, registra a retomada,
   pede confirmação (ou segue direto com `--yes`) e reinicia. Nunca há reboot sem a retomada registrada.
3. Depois do boot, `toolkit resume` continua da etapa pendente:
   - Windows: Tarefa Agendada no logon do usuário que iniciou a receita, com privilégio elevado.
   - Linux: unit systemd de sistema, oneshot, no boot e depois de `network-online.target`. Roda sem
     terminal (`toolkit resume --yes --no-tui`), e o resultado fica no log e em `toolkit recipe status`.
   - macOS: LaunchAgent no login do usuário, que abre o Terminal para mostrar o progresso.
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

O log de cada execução fica ao lado do estado, em `logs/`.

## Comandos

```sh
toolkit recipe list
toolkit recipe run wsl-dev [--yes] [--dry-run]
toolkit recipe status
toolkit recipe reset wsl-dev     # libera uma nova tentativa depois de uma falha
toolkit resume
```

## Exemplos

### Setup com reboot (Windows)

```yaml
id: wsl-dev
name: Ambiente WSL + Ubuntu para dev
os: windows
requires_admin: true
steps:
  - id: enable-features
    check: { powershell: "(Get-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform).State -eq 'Enabled'" }
    run:
      powershell: >-
        dism /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart;
        dism /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /all /norestart
      success_codes: [3010]
    reboot: required
  - id: install-distro
    check: { powershell: "wsl -l -q | Select-String -Quiet Ubuntu" }
    run:   { powershell: "wsl --install -d Ubuntu --no-launch" }
  - id: setup-inside
    # o unix.sh vem da mesma versão do toolkit em execução, com hash conferido pelo checksums.txt
    run: { wsl: { distro: Ubuntu, bash: "bash /mnt/c/ProgramData/toolkit/bin/unix.sh recipe linux-dev" } }
  - id: apps
    install: [vscode, git, docker-desktop]
```

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
