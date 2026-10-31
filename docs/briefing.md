# Prompt — Toolkit multiplataforma de setup de máquinas (Go)

> Briefing do Paulo, recebido em 2026-10-31. Texto original, sem edição.

## Contexto

Sou técnico de informática e engenheiro DevOps/SRE. Quero uma ferramenta para preparar máquinas (formatação, setup de cliente, ambiente de dev) com **um único comando colado no terminal**, como o WinUtil do Chris Titus (`irm https://christitus.com/win | iex`), só que:

- **multiplataforma**: Windows, Linux e macOS;
- **modular**: adicionar um app ou receita nova = editar um arquivo YAML ou criar um arquivo de receita, sem mexer no núcleo;
- **sem baixar .exe manualmente**: tudo via gerenciadores de pacote ou download verificado por hash;
- **com seleção de versão** quando o app tiver mais de uma disponível;
- **com receitas de várias etapas que sobrevivem a reboot** (ex.: habilitar WSL → reiniciar → instalar distro → configurar ambiente dentro dela).

Nome provisório do projeto: `toolkit` (vou renomear depois; deixe o nome centralizado numa constante/variável de build).

## Arquitetura desejada

```
toolkit/
├── cmd/toolkit/main.go
├── internal/
│   ├── catalog/      # parse e validação do catalog.yaml (+ embed do catálogo padrão)
│   ├── platform/     # detecção de SO, distro, arquitetura, admin/root, gerenciadores disponíveis
│   ├── providers/    # winget, brew, apt, dnf, pacman, flatpak, github-release, url
│   ├── recipes/      # engine de receitas multi-etapa
│   ├── state/        # persistência de estado e retomada pós-reboot
│   ├── resume/       # registro de retomada por SO (Scheduled Task / systemd / LaunchAgent)
│   ├── exec/         # interface Runner para executar comandos (mockável em testes)
│   └── tui/          # interface Bubble Tea
├── catalog/catalog.yaml
├── recipes/{windows,linux,macos}/*.yaml
├── bootstrap/
│   ├── win.ps1       # usado com: irm <url>/win | iex
│   └── unix.sh       # usado com: curl -fsSL <url>/sh | bash
├── .goreleaser.yaml
└── .github/workflows/{ci.yml,release.yml}
```

### Stack
- Go (versão estável atual), **Cobra** para os subcomandos, **Bubble Tea + Bubbles + Lip Gloss** para a TUI, `gopkg.in/yaml.v3`.
- Binário estático, sem CGO, para `windows/amd64`, `windows/arm64`, `linux/amd64`, `linux/arm64`, `darwin/amd64` e `darwin/arm64`.
- Release via **GoReleaser** no GitHub Actions, gerando `checksums.txt`.

## Requisitos funcionais

### 1. Catálogo de apps (`catalog.yaml`)
Cada app declara como instalar em cada SO. O usuário vê só "Discord", e o toolkit escolhe o provider disponível na máquina, em ordem de preferência.

```yaml
apps:
  discord:
    name: Discord
    category: comunicacao
    windows: [{ provider: winget, id: Discord.Discord }]
    macos:   [{ provider: brew, cask: discord }]
    linux:
      - { provider: flatpak, id: com.discordapp.Discord }
      - { provider: apt, package: discord }
  python:
    name: Python
    category: dev
    versions: true            # habilita seleção de versão na TUI
    windows: [{ provider: winget, id: "Python.Python.{{major_minor}}" }]
    macos:   [{ provider: brew, formula: "python@{{major_minor}}" }]
    linux:   [{ provider: apt, package: "python{{major_minor}}" }]
  minha-ferramenta:
    name: Exemplo via GitHub
    any:
      - provider: github-release
        repo: dono/repo
        asset: "*_{{os}}_{{arch}}.zip"   # glob resolvido na release escolhida
```

- Validar o catálogo na carga (schema e campos obrigatórios), com erro claro apontando a linha.
- O catálogo padrão vai embutido no binário (`embed`), e a flag `--catalog <path|url>` permite usar um externo.

### 2. Providers
Interface comum:

```go
type Provider interface {
    Name() string
    Available(ctx context.Context) bool
    IsInstalled(ctx context.Context, pkg PackageRef) (bool, string, error) // instalado? versão?
    ListVersions(ctx context.Context, pkg PackageRef) ([]string, error)
    Install(ctx context.Context, pkg PackageRef, version string) error
}
```

- **winget**: `winget show --id X -e --versions` para listar, `winget install --id X -e --silent --accept-package-agreements --accept-source-agreements [--version V]`. Tratar os exit codes conhecidos (ex.: "já instalado").
- **brew** (formula/cask), **apt**, **dnf**, **pacman**, **flatpak**: listar versões quando o gerenciador suportar; quando não suportar, retornar só "latest".
- **github-release**: API `GET /repos/{repo}/releases` (respeitar `GITHUB_TOKEN` opcional por causa de rate limit), escolher o asset pelo glob, baixar, **verificar o SHA-256** (pelo arquivo de checksums da release quando existir) e instalar/extrair conforme o tipo (msi, exe com args silenciosos declarados no YAML, zip, tar.gz, dmg, deb, rpm, AppImage).
- **url**: download direto, **com `sha256` obrigatório no YAML**. Sem hash, recusar a instalação.

### 3. Seleção de versão
- Na TUI, um app com `versions: true` abre uma lista com as versões de `ListVersions`, sendo "latest" a opção padrão.
- Na CLI: `toolkit install python@3.11 discord`.

### 4. Receitas multi-etapa com reboot
Receitas em YAML, por SO. Cada etapa tem um `check` (idempotência) e uma `run`:

```yaml
id: wsl-dev
name: Ambiente WSL + Ubuntu para dev
os: windows
requires_admin: true
steps:
  - id: enable-features
    check: { powershell: "(Get-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform).State -eq 'Enabled'" }
    run:   { powershell: "dism /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart; dism /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /all /norestart" }
    reboot: required          # required | if-needed (checar exit code 3010) | never
  - id: install-distro
    check: { powershell: "wsl -l -q | Select-String -Quiet Ubuntu" }
    run:   { powershell: "wsl --install -d Ubuntu --no-launch" }
  - id: setup-inside
    run:   { wsl: { distro: Ubuntu, bash: "curl -fsSL {{base_url}}/sh | bash -s -- recipe linux-dev" } }
  - id: apps
    install: [vscode, git, docker-desktop]   # reaproveita o catálogo
```

Engine:
- Executa as etapas em ordem. Se o `check` passar, marca a etapa como `skipped`.
- Persiste o estado após **cada** etapa:
  - Windows: `%ProgramData%\toolkit\state.json`
  - Linux: `/var/lib/toolkit/state.json` (root) ou `$XDG_STATE_HOME/toolkit/` (user)
  - macOS: `/Library/Application Support/toolkit/` ou `~/Library/Application Support/toolkit/`
- Quando a etapa pede reboot: registra a retomada, pede confirmação ao usuário (ou segue direto com `--yes`) e reinicia.
- Retomada por SO, sempre chamando `toolkit resume`:
  - Windows: **Scheduled Task** "At logon" com privilégio elevado
  - Linux: unit **systemd** oneshot
  - macOS: **LaunchAgent/LaunchDaemon**
- `toolkit resume` lê o estado, continua da etapa pendente e, ao terminar (sucesso ou falha), **remove o registro de retomada**.
- Evitar loop de reboot: se a mesma etapa falhar após a retomada, parar e reportar, sem reiniciar de novo.
- Comandos: `toolkit recipe list`, `toolkit recipe run <id>`, `toolkit resume`, `toolkit recipe status`, `toolkit recipe reset <id>`.

### 5. Elevação de privilégio
- Detectar se está rodando como admin/root.
- Windows: se uma operação exigir admin, relançar o próprio binário com `runas` (ShellExecute) mantendo os argumentos. Linux/macOS: usar `sudo` só nos comandos que precisam, nunca rodar a TUI inteira como root sem necessidade.

### 6. TUI
- Abas: **Apps** (por categoria, com busca e multi-seleção, mostrando o status instalado/versão), **Receitas**, **Diagnóstico** e **Log**.
- Tela de revisão antes de executar ("vai instalar X, Y, Z; receita W exige reboot").
- Progresso por item com log ao vivo, e resumo final com sucesso/falha.
- Tudo também disponível sem TUI (`--no-tui`, `--yes`, `--dry-run`) para automação.

### 7. Diagnóstico (módulo Windows primeiro, depois equivalentes)
Comandos prontos: informações de RAM (`Get-CimInstance Win32_PhysicalMemory`), agendar o Diagnóstico de Memória do Windows (`mdsched.exe`), `sfc /scannow`, `DISM /Online /Cleanup-Image /RestoreHealth`, saúde de disco (`Get-PhysicalDisk`) e informações de rede. No Linux/mac: `free`, `smartctl` (se existir), `lsblk`/`diskutil` etc. Implementar como receitas de diagnóstico, sem lógica especial no núcleo.

### 8. Bootstraps (finos, ~20 linhas cada)
- `win.ps1`: força TLS 1.2, detecta a arquitetura, baixa da **release fixada** (tag passada via variável, com padrão "latest") o binário certo + `checksums.txt`, **verifica o SHA-256**, salva em `%ProgramData%\toolkit\bin\` e executa repassando os argumentos. Precisa funcionar no **Windows PowerShell 5.1** (sem depender do pwsh 7).
- `unix.sh`: POSIX sh/bash, detecta `uname -s`/`uname -m`, usa `curl` (com fallback para `wget`), verifica o hash (`sha256sum` ou `shasum -a 256`), instala em `~/.local/bin` ou `/usr/local/bin` e executa.

## Requisitos não funcionais
- **Segurança**: nada de download sem verificação de hash, nenhum `curl | bash` de terceiros dentro das receitas (só do próprio repo, numa versão fixada), logs sem segredos.
- **Escopo**: o toolkit **não** inclui nada que contorne licenciamento ou ative software sem licença (ativadores de Windows/Office, cracks). Office entra só pelo **Office Deployment Tool** oficial com `config.xml` declarado.
- **Idempotência**: rodar duas vezes não pode quebrar nem reinstalar à toa.
- **Logs**: arquivo de log por execução, ao lado do state; `--verbose` para ver no terminal.
- **Testes**: a interface `exec.Runner` precisa ser mockável. Testes unitários para catalog, seleção de provider, engine de receitas (incluindo a simulação de reboot/retomada) e para os parsers de saída do winget/brew/apt. CI em matriz `windows-latest`, `ubuntu-latest` e `macos-latest` rodando `go test ./...`, `go vet` e `golangci-lint`.

## Ordem de entrega (faça por fases e pare ao fim de cada uma para eu revisar)
1. **Esqueleto**: módulo Go, Cobra, `platform` (detecção), `exec.Runner`, carga/validação do catálogo, `toolkit list`, CI básico.
2. **Providers** winget, brew, apt e flatpak + `toolkit install app[@versão]` + `--dry-run`.
3. **Engine de receitas + state + resume** (Windows primeiro, com a receita `wsl-dev` completa), depois systemd e LaunchAgent.
4. **TUI** Bubble Tea.
5. Providers **github-release** e **url** com verificação de hash; receitas de Office (ODT) e de impressoras (`pnputil /add-driver` + `Add-PrinterDriver`/`Add-Printer`, com os drivers declarados no YAML).
6. **Bootstraps + GoReleaser + workflow de release** e um README com os comandos de uma linha.

## Antes de começar
Leia este prompt inteiro, proponha a estrutura final de pastas e as interfaces principais (`Provider`, `Step`, `State`, `Runner`) e me pergunte o que estiver ambíguo. Não comece a fase 1 antes de eu aprovar.
