# Arquitetura

> Derivada da [proposta](proposta.md), com as decisões de [decisoes.md](decisoes.md) já aplicadas. O
> código está em construção: a fase 1 começou em 2026-11-11 (PR1: módulo, Cobra e gate local).
> Fontes oficiais em [Fontes](#fontes), citadas com [n].

Resumo: a lógica de cada SO fica em **funções puras que montam comandos** (`[]exec.Cmd`), sem build
tags. Assim tudo é testável em qualquer SO com um `Runner` falso, e nada executa instalador, dism ou
reboot de verdade durante os testes. Build tags só nas chamadas de sistema que não têm alternativa
(token elevado do Windows, `ShellExecute runas` e `geteuid`).

## 1. Estrutura final de pastas

```
toolkit/
├── cmd/toolkit/main.go            # só chama cli.Execute(); nada de lógica aqui
├── assets.go                      # package toolkit: //go:embed catalog/catalog.yaml recipes
├── internal/
│   ├── buildinfo/                 # Name, Version, Commit, Date (via -ldflags -X); nome centralizado
│   ├── cli/                       # comandos Cobra: list, install, recipe {list,run,status,reset}, resume, diag, version
│   ├── catalog/                   # parse com yaml.Node (linha/coluna nos erros), validação, merge com catálogo externo
│   ├── platform/                  # Detect(): SO, distro (/etc/os-release), arch, admin, gerenciadores, boot ID
│   ├── elevate/                   # IsElevated + Relaunch (Windows: ShellExecute "runas"); Unix: prefixo sudo por Cmd
│   ├── exec/                      # interface Runner, OSRunner, DryRunRunner
│   │   └── exectest/              # FakeRunner: respostas roteirizadas por linha de comando + registro das chamadas
│   ├── providers/                 # interface Provider, registro, Selector (ordem de preferência)
│   │   ├── winget/                #   cada provider: builders de Cmd + parsers de saída + tabela de exit codes
│   │   ├── brew/
│   │   ├── apt/
│   │   ├── dnf/
│   │   ├── pacman/
│   │   ├── flatpak/
│   │   ├── githubrelease/
│   │   └── url/
│   ├── download/                  # HTTP com SHA-256 obrigatório (recusa sem hash), limite de tamanho, retry
│   ├── archive/                   # extração zip/tar.gz com proteção contra zip-slip e symlink fora do destino
│   ├── tmpl/                      # substituição {{var}} estrita (variável desconhecida = erro), sem text/template
│   ├── recipes/                   # parse/validação de receitas + Engine (check → run → reboot → retomada)
│   ├── state/                     # Store JSON com escrita atômica (tmp + fsync + rename) e lock de arquivo
│   ├── resume/                    # ResumeRegistrar: schtask (Windows), systemd (Linux), launchd (macOS)
│   ├── logx/                      # log por execução ao lado do state, redação de segredos (tokens), --verbose
│   └── tui/                       # Bubble Tea: abas Apps, Receitas, Diagnóstico, Log; tela de revisão; progresso
├── catalog/catalog.yaml           # catálogo padrão (embutido)
├── recipes/
│   ├── windows/                   # wsl-dev.yaml, diag-*.yaml, office-odt.yaml, printers-*.yaml
│   ├── linux/                     # linux-dev.yaml, diag-*.yaml
│   └── macos/                     # diag-*.yaml
├── bootstrap/
│   ├── win.ps1                    # irm <url>/win | iex  (Windows PowerShell 5.1)
│   └── unix.sh                    # curl -fsSL <url>/sh | bash  (POSIX sh)
├── testdata/                      # saídas reais capturadas de winget/brew/apt/dnf/pacman/flatpak para os parsers
├── scripts/ci.sh                  # o "CI local": o mesmo que o workflow roda (ver seção 3.9)
├── Makefile                       # atalhos: make ci, make build-all, make snapshot
├── .golangci.yml                  # golangci-lint v2 (version: "2")
├── .goreleaser.yaml
├── .github/workflows/{ci.yml,release.yml}
├── docs/                          # briefing.md, proposta.md e, a partir da fase 1, arquitetura.md, catalogo.md, receitas.md
├── CLAUDE.md                      # índice curto apontando para docs/
├── go.mod / go.sum                # versões exatas, go.sum commitado
└── README.md                      # fase 6: comandos de uma linha
```

Por que `assets.go` na raiz: `//go:embed` não acessa diretórios acima do pacote. Um arquivo na raiz
do módulo embute `catalog/` e `recipes/` sem duplicar arquivos e sem script de geração.

## 2. Interfaces principais

Esboço do contrato. Os nomes podem mudar na fase 1, a forma não.

### 2.1 exec.Runner

```go
package exec

// Cmd descreve um processo a executar. Nunca passa por shell implícito:
// quem quiser PowerShell/bash monta Name="powershell.exe" e Args explícitos.
type Cmd struct {
	Name    string
	Args    []string
	Env     []string      // acrescentado ao ambiente atual (ex.: HOMEBREW_NO_AUTO_UPDATE=1)
	Dir     string
	Stdin   io.Reader
	Stdout  io.Writer     // opcional: tee para o log ao vivo (TUI/--verbose)
	Timeout time.Duration // 0 = só o ctx
	Elevate bool          // Unix: "sudo -n" (nunca pede senha; ErrNotElevated se faltar credencial). Windows: exige processo já elevado. Sempre conta como mutação no dry-run
	ReadOnly bool         // true = só lê. Default false = pode alterar: o DryRunRunner não executa, só registra
	Secret  bool          // não loga Args (ex.: comando com token)
}

// Result traz o exit code bruto. Exit code != 0 NÃO é erro do Runner:
// cada provider interpreta o seu (winget usa HRESULTs, msi usa 3010 etc.).
type Result struct {
	ExitCode int // no Windows, comparar como uint32 (HRESULT 0x8A15...)
	Stdout   []byte
	Stderr   []byte
	Duration time.Duration
}

type Runner interface {
	// Run retorna erro só se o processo não pôde ser iniciado, o ctx expirou ou o Timeout estourou.
	Run(ctx context.Context, c Cmd) (Result, error)
	// LookPath existe no Runner para que Available() também seja mockável.
	LookPath(name string) (string, error)
}
```

`DryRunRunner` envolve o `OSRunner` e **falha fechado**: só **executa** o que é declarado leitura
(`ReadOnly=true` e sem `Elevate`, como `winget list` e `dpkg-query`), porque o dry-run precisa mostrar o que
já está instalado; todo o resto, inclusive um `Cmd` sem flag, é só **registrado** (`Plan()`, com
`ErrNotElevated` no passo quando o Windows não está elevado). O `OSRunner` encerra o grupo de processos no
timeout (Job Object no Windows), usa `sudo -n` com `env VAR=...` para o `Env`, e limita stdout/stderr em memória. Nos testes nada chega ao `OSRunner`: o `exectest.FakeRunner` responde a
partir de roteiros e falha o teste se receber um comando inesperado.

### 2.2 platform

```go
package platform

type OS string // "windows" | "linux" | "darwin"

type Info struct {
	OS       OS
	Arch     string   // runtime.GOARCH normalizado: amd64 | arm64
	Distro   Distro   // vazio fora do Linux
	Version  string   // build do Windows, versão do macOS
	IsAdmin  bool     // token elevado (Windows) / euid 0 (Unix)
	User     string   // usuário real (SUDO_USER se rodando via sudo)
	BootID   string   // muda a cada boot; serve para detectar se o reboot de fato aconteceu
	Managers []string // gerenciadores encontrados no PATH (informativo; quem decide é Provider.Available)
}

type Distro struct {
	ID        string   // ubuntu, fedora, arch...
	IDLike    []string // debian, rhel...
	VersionID string
}

type Detector interface {
	Detect(ctx context.Context) (Info, error)
}

// Paths resolve os diretórios do briefing (state, log, bin) por SO e por admin/usuário.
type Paths struct{ StateDir, LogDir, BinDir string }
func ResolvePaths(i Info) Paths
```

`BootID` vem de `/proc/sys/kernel/random/boot_id` no Linux, de `sysctl kern.boottime` no macOS e de
`Win32_OperatingSystem.LastBootUpTime` no Windows. A leitura passa pelo Runner ou por um fs injetado.

### 2.3 PackageRef e Provider

```go
package providers

// PackageRef é UMA entrada de instalação do catálogo (um item da lista windows:/linux:/macos:/any:),
// já com os templates {{...}} resolvidos. Campos não usados pelo provider ficam vazios;
// Provider.Validate recusa campos que não são dele.
type PackageRef struct {
	AppID    string // chave no catálogo ("discord")
	Provider string // "winget", "brew", ...
	ID       string // winget id / flatpak app id
	Package  string // apt, dnf, pacman
	Formula  string // brew
	Cask     string // brew
	Source   string // winget --source / flatpak remote (padrão: winget / flathub)
	Repo     string // github-release: "dono/repo"
	Asset    string // github-release: glob do asset
	URL      string // url
	SHA256   string // url: obrigatório; github-release: opcional (pin explícito)
	Type     string // msi|exe|zip|tar.gz|dmg|deb|rpm|appimage (inferido da extensão se vazio)
	Args     []string // argumentos silenciosos para exe/msi
	Line     int      // linha no YAML, para mensagens de erro
}

// InstallResult substitui o "error" puro do briefing: instalar pode dar certo E pedir reboot.
type InstallResult struct {
	AlreadyInstalled bool   // nada foi feito (idempotência)
	RebootRequired   bool   // winget 0x8A150109, msi/dism 3010
	Version          string // versão que ficou instalada, se o provider souber
}

type Provider interface {
	Name() string
	Available(ctx context.Context) bool
	Validate(pkg PackageRef) error                                           // checa os campos na carga do catálogo
	IsInstalled(ctx context.Context, pkg PackageRef) (bool, string, error)   // instalado? versão?
	ListVersions(ctx context.Context, pkg PackageRef) ([]string, error)      // ["latest"] se o gerenciador não lista
	Install(ctx context.Context, pkg PackageRef, version string) (InstallResult, error)
}

// Selector escolhe, na ordem do YAML, o primeiro PackageRef cujo provider está Available.
type Selector interface {
	Select(ctx context.Context, app catalog.App, p platform.Info) (Provider, PackageRef, error)
}
```

As duas mudanças em relação à interface do briefing são `Validate` e o `InstallResult`. O motivo
está na seção 3.3. Erros tipados: `ErrNotFound`, `ErrNoApplicableInstaller` (o Selector tenta o
próximo provider), `ErrHashMismatch`, `ErrNeedsAdmin` e `ErrRetryable` (pacote em uso, outra
instalação em andamento).

### 2.4 Recipe e Step

```go
package recipes

type Kind string // "setup" | "diagnostic"

type Recipe struct {
	ID            string
	Name          string
	Description   string
	OS            platform.OS
	Kind          Kind
	RequiresAdmin bool
	Steps         []Step
	Hash          string // sha256 do YAML: a retomada recusa a receita se ela mudou entre os boots
}

type RebootPolicy string // "never" (padrão) | "required" | "if-needed"

type Step struct {
	ID      string
	Name    string
	Check   *Action      // opcional; exit 0 = já está feito → skipped
	Run     *Action      // exatamente um entre Run e Install
	Install []string     // ids do catálogo, aceita "python@3.12"
	Reboot  RebootPolicy // if-needed: reboot se Run sair com 3010 ou se algum Install pedir
	Timeout time.Duration
}

// Action: exatamente um campo preenchido (validado na carga).
type Action struct {
	PowerShell string     // powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand <base64 UTF-16LE>
	Sh         string     // /bin/sh -c (Linux/macOS)
	Bash       string     // bash -c
	WSL        *WSLAction // wsl.exe -d <distro> [-u <user>] -- bash -c
	SuccessCodes []int    // além do 0 (ex.: [3010] para dism com reboot: if-needed)
}

type WSLAction struct {
	Distro string
	User   string // padrão: usuário padrão da distro
	Bash   string
}

// Engine executa receitas e é o único dono do ciclo state/resume/reboot.
type Engine struct {
	Runner    exec.Runner
	Store     state.Store
	Resume    resume.Registrar
	Installer Installer   // ponte para o catálogo + providers (passos "install:")
	Rebooter  Rebooter    // shutdown /r /t 5, systemctl reboot, shutdown -r now (tudo via Runner)
	Prompter  Prompter    // TUI, terminal ou "sempre sim" (--yes)
	Platform  platform.Info
	Clock     func() time.Time
	Log       *slog.Logger
}

func (e *Engine) Run(ctx context.Context, r Recipe, opt RunOptions) (Report, error)
func (e *Engine) ResumePending(ctx context.Context) (Report, error) // implementa `toolkit resume`
```

O PowerShell recebe `-EncodedCommand` para eliminar os problemas de aspas entre Go, cmd e PowerShell.
O `Rebooter` é uma interface separada, para que o teste do reboot seja só "o `Rebooter.Reboot` foi
chamado e o state foi salvo antes".

### 2.5 State

```go
package state

type StepStatus string // pending | running | done | skipped | failed | awaiting-reboot

type File struct {
	SchemaVersion  int                     `json:"schema_version"`
	ToolkitVersion string                  `json:"toolkit_version"`
	Active         string                  `json:"active,omitempty"` // receita em andamento (no máximo uma)
	Recipes        map[string]*RecipeState `json:"recipes"`
}

type RecipeState struct {
	RecipeID   string      `json:"recipe_id"`
	RecipeHash string      `json:"recipe_hash"`
	RunID      string      `json:"run_id"`
	Status     string      `json:"status"` // running | awaiting-reboot | done | failed
	Options    RunOptions  `json:"options"` // flags originais (--yes, versões escolhidas) para a retomada
	Steps      []StepState `json:"steps"`
	StartedAt  time.Time   `json:"started_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type StepState struct {
	ID             string     `json:"id"`
	Status         StepStatus `json:"status"`
	Attempts       int        `json:"attempts"`
	ResumeAttempts int        `json:"resume_attempts"` // proteção contra loop de reboot
	BootID         string     `json:"boot_id,omitempty"` // boot em que o reboot foi pedido
	ExitCode       int        `json:"exit_code,omitempty"`
	Error          string     `json:"error,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type Store interface {
	Load(ctx context.Context) (*File, error) // arquivo inexistente = File vazio, não erro
	Save(ctx context.Context, f *File) error // atômico: tmp + fsync + rename, sob lock
	Path() string
}
```

### 2.6 ResumeRegistrar

```go
package resume

type Spec struct {
	Executable string   // caminho estável do binário (copiado para BinDir antes de registrar)
	Args       []string // sempre: ["resume"] + flags herdadas (--yes, --verbose)
	User       string   // usuário que iniciou a receita
	Elevated   bool
	LogPath    string
}

type Registrar interface {
	Register(ctx context.Context, s Spec) error
	Unregister(ctx context.Context) error // idempotente: não registrado = sucesso
	IsRegistered(ctx context.Context) (bool, error)
}
```

As implementações (`schtask`, `systemd`, `launchd`) só montam `[]exec.Cmd` e o conteúdo dos
arquivos (unit, plist). Isso é testado com golden files no macOS. Detalhes por SO na seção 3.5.

### 2.7 Elevação

```go
package elevate

func IsElevated() (bool, error)                      // build tags: windows (token) / unix (geteuid)
func Relaunch(ctx context.Context, args []string) error // Windows: ShellExecuteW("runas", self, args); Unix: não usado
```

## 3. Decisões e motivos

### 3.1 Testabilidade sem Windows nem CI pago

A lógica de cada SO não fica atrás de build tags. `winget.installCmd(ref, ver) exec.Cmd`,
`schtask.registerScript(spec) string` e `systemd.unitFile(spec) string` são funções puras, testadas
em qualquer SO com golden files. Só `elevate` e a leitura de token usam `//go:build windows`. O
`scripts/ci.sh` roda `go vet` e compila os testes (`go test -c`) também com `GOOS=windows` e
`GOOS=linux`, o que pega erro de build do código do Windows em qualquer máquina de desenvolvimento. **Motivo:**
nenhum teste pode executar winget, dism ou reboot de verdade, e o gate de qualidade precisa rodar localmente.

### 3.2 Catálogo

- Decodifico para `yaml.Node` e valido em cima dele. Cada erro sai como `catalog.yaml:42:7: app
  "discord": provider "winget" exige "id"`. Chave desconhecida é erro, o que pega erro de digitação
  em `pakage:`.
- O id do app segue `^[a-z0-9][a-z0-9-]*$`. A categoria vem de uma lista fixa definida no código.
- Quando o id tem `{{...}}`, o YAML declara a lista de versões. `versions: true` fica para ids fixos.
- `--catalog` aceita caminho local sem hash. URL só com `--catalog-sha256`. O externo é mesclado sobre
  o embutido (mesmo id substitui), e `--catalog-replace` troca o embutido inteiro.
- `os: any` vale como fallback: se a lista do SO atual não existir, o Selector usa `any`.
- Os templates `{{os}}`, `{{arch}}`, `{{version}}`, `{{major}}`, `{{major_minor}}` e
  `{{toolkit_version}}` são resolvidos por `tmpl`, um substituidor estrito. Não uso `text/template`,
  para não abrir funções e lógica dentro do YAML. Variável desconhecida é erro na carga.

### 3.3 Providers

- **`InstallResult` no lugar de `error`**: uma instalação pode dar certo e pedir reboot. O winget
  devolve `0x8A150109 INSTALL_REBOOT_REQUIRED_TO_FINISH` [1], e msi/dism devolvem 3010. O passo
  `install:` com `reboot: if-needed` precisa dessa informação.
- **`Validate`**: cada provider declara os campos de que precisa. O catálogo falha na carga, não no
  meio de uma instalação.
- **winget** [1][2][3][4]:
  - Instala com `winget install --id X -e --source winget --silent --accept-package-agreements
    --accept-source-agreements --disable-interactivity [--version V]`. `--source winget` evita os
    acordos e ids da msstore. `--disable-interactivity` está documentado para install, show e list.
  - Para listar versões, uso `winget show --id X -e --versions` [3].
  - Para checar o que está instalado, uso `winget list --id X -e`. "Não encontrado" vem como
    `0x8A150014 NO_APPLICATIONS_FOUND` [1].
  - A saída do winget é uma **tabela localizada** (o cabeçalho muda com o idioma do Windows). O
    parser localiza as colunas pela linha de traços `----`, nunca pelo texto do cabeçalho, e
    descarta os caracteres de spinner e progresso. Saídas reais em pt-BR e en-US ficam em `testdata/`.
  - O módulo `Microsoft.WinGet.Client` devolveria objetos estruturados, mas não vem instalado [5].
    Instalá-lo seria baixar módulo da PSGallery, então fico no CLI.
  - Tabela de exit codes (comparados como `uint32`), conforme [1]:

    | Código | Símbolo | Tratamento |
    |---|---|---|
    | 0 | — | sucesso |
    | 0x8A15002B | UPDATE_NOT_APPLICABLE | já instalado na versão mais nova → `AlreadyInstalled` |
    | 0x8A150061 | PACKAGE_ALREADY_INSTALLED | → `AlreadyInstalled` |
    | 0x8A15010D | INSTALL_ALREADY_INSTALLED ("outra versão instalada") | sem `@versão`: `AlreadyInstalled`; com `@versão` diferente: erro claro |
    | 0x8A15010E | INSTALL_DOWNGRADE | erro claro ("versão maior já instalada") |
    | 0x8A150109 | INSTALL_REBOOT_REQUIRED_TO_FINISH | sucesso + `RebootRequired` |
    | 0x8A15010B | INSTALL_REBOOT_INITIATED | sucesso + `RebootRequired` (o state é salvo antes) |
    | 0x8A15010A | INSTALL_REBOOT_REQUIRED_FOR_INSTALL | falha; pede reboot e retenta após a retomada |
    | 0x8A150101 / 0x8A150103 | PACKAGE_IN_USE / FILE_IN_USE | `ErrRetryable` com mensagem "feche o app" |
    | 0x8A150102 | INSTALL_IN_PROGRESS | `ErrRetryable`, até 3 tentativas com backoff |
    | 0x8A150014 | NO_APPLICATIONS_FOUND | `ErrNotFound` |
    | 0x8A150010 | NO_APPLICABLE_INSTALLER | `ErrNoApplicableInstaller` → o Selector tenta o próximo provider |
    | 0x8A150011 | INSTALLER_HASH_MISMATCH | `ErrHashMismatch`, nunca retenta nem usa `--ignore-security-hash` |
    | 0x8A15010F | INSTALL_BLOCKED_BY_POLICY | erro claro |
    | 0x8A150104 / 0x8A150105 / 0x8A150107 | MISSING_DEPENDENCY / DISK_FULL / NO_NETWORK | erro com a mensagem oficial |

  - Se o winget não estiver instalado ou registrado (primeiro login ainda não registrou o App Installer [5]), o
    diagnóstico oferece `Add-AppxPackage -RegisterByFamilyName -MainPackage
    Microsoft.DesktopAppInstaller_8wekyb3d8bbwe`, que é o comando da documentação oficial. Não baixo o App Installer por fora.
- **brew** [6][7]:
  - Lê `brew info --json=v2 <nome>`, que vale para formula e cask, e `brew list --versions`.
  - Formula e cask oficiais expõem **uma** versão estável, então `ListVersions` devolve `["latest"]`.
    Versões múltiplas existem como fórmulas versionadas (`python@3.12`). Quando o id do app tem `{{...}}`, a lista de versões fica declarada no YAML (ver [catalogo.md](catalogo.md)).
  - Rodo sempre com `HOMEBREW_NO_AUTO_UPDATE=1` (evita um `brew update` a cada pacote; faço um único
    `brew update` por execução) e `HOMEBREW_NO_ENV_HINTS=1` [6].
  - Nunca rodo brew com sudo: o FAQ do Homebrew explica que ele roda como a conta dona da instalação [7].
- **apt** [8][9]:
  - Versões vêm de `apt-cache madison <pkg>`, que mostra as versões que existem nos repositórios
    configurados [8]. Instalo com `apt-get install -y pkg=versão` [9].
  - O estado instalado vem de `dpkg-query -W -f='${Status}\t${Version}' pkg`.
  - O apt-get sai com 100 em qualquer erro [9], então o diagnóstico precisa vir do stderr.
  - Rodo um único `apt-get update` por execução, com `DEBIAN_FRONTEND=noninteractive`.
- **dnf** [10]: `dnf list --showduplicates <pkg>` mostra todas as versões [10] e a instalação usa
  `pkg-versão`. Cubro dnf5 e dnf4: a saída de `list` é parecida e entra nos testes com as duas em
  `testdata/`.
- **pacman** [11]: o pacman só conhece a versão atual do repositório. `-Si` mostra uma versão e o
  manual não oferece listagem de versões antigas [11]. Instalar uma versão antiga do repositório é
  atualização parcial, que o Arch não suporta. `ListVersions` → `["latest"]`, a instalação usa
  `pacman -S --needed --noconfirm` e o estado instalado vem de `pacman -Q pkg`.
- **flatpak** [12]: instalo com `flatpak install -y --noninteractive <remote> <id>` e leio o estado
  com `flatpak list --app --columns=application,version`. Existe `remote-info --log` com os commits
  antigos e `update --commit` para voltar a um commit [12], mas isso não é "escolher versão" de forma
  utilizável. `ListVersions` → `["latest"]`. O padrão é instalação de sistema, e `--user` pode ser
  pedido por app.
- **github-release** (fase 5) [13][14]:
  - Uso `GET /repos/{repo}/releases` com `GITHUB_TOKEN` opcional (redigido no log) e
    `X-GitHub-Api-Version`.
  - Hash, nesta ordem: (a) arquivo de checksums da release (`checksums.txt`, `*.sha256`,
    `SHA256SUMS`); (b) campo `digest` do asset, que o GitHub calcula no upload desde 2025-06-03
    [13][14]; (c) `sha256:` fixado no YAML. Sem nenhum dos três, **recuso**.
- **url**: `sha256` obrigatório. A validação do catálogo já recusa uma entrada `url` sem hash.
- **download/archive**: tamanho máximo configurável, o arquivo é gravado em temp e só é movido depois
  do hash conferir, e a extração recusa caminho absoluto, `..` e symlink que aponte para fora do destino.

### 3.4 Receitas e engine

- O state é salvo **antes** de cada etapa (`running`) e **depois** dela (`done`, `skipped` ou `failed`).
  Uma queda de energia no meio de uma etapa deixa `running`. Na retomada, a etapa reexecuta o
  `check`: se passar, vira `done`; senão, roda de novo, desde que tenha `check`. Etapa `running` sem
  `check` vira `failed` e pede intervenção, porque não tenho como saber se repetir é seguro.
- Fluxo do reboot:
  1. A etapa termina.
  2. O state recebe `awaiting-reboot` com o `BootID` atual.
  3. O `Registrar.Register` é chamado.
  4. O usuário confirma (ou não, se rodou com `--yes`).
  5. O `Rebooter` é chamado.

  A ordem garante que nunca exista reboot sem a retomada já registrada.
- **Anti-loop**:
  1. Se o `resume` rodar e o `BootID` não mudou, o reboot não aconteceu. Nada avança e o usuário
     recebe "reinicie para continuar".
  2. Depois do reboot, se a etapa tiver `check`, ele precisa passar. Se falhar, a etapa vira
     `failed`, a retomada é desregistrada e o toolkit **não** reinicia de novo.
  3. Há um limite de `ResumeAttempts` = 1 por etapa.
- O `resume` sempre termina com `Unregister`, em caso de sucesso, falha ou panic (via `defer`), a não
  ser que uma **nova** etapa tenha pedido reboot. Nesse caso o registro continua.
- `RecipeHash`: se o YAML da receita mudou entre os boots (binário atualizado), a retomada para e
  reporta. Não continuo uma receita diferente da que comecei.
- **Diagnóstico** é só uma receita com `kind: diagnostic`, sem `reboot`. A saída dos comandos vai
  para o relatório, que a aba Diagnóstico mostra. Nada especial no núcleo, como o briefing pede.

### 3.5 Retomada por SO

- **Windows** [15][16][17]:
  - Registro com `Register-ScheduledTask` via PowerShell, com trigger `New-ScheduledTaskTrigger
    -AtLogOn -User <usuário>` e principal `New-ScheduledTaskPrincipal -UserId <usuário> -LogonType
    Interactive -RunLevel Highest` [15][16].
  - **Por que o usuário e não o SYSTEM**: (1) a conta System não tem logon interativo e o usuário não
    veria a janela [17]; (2) `wsl --install -d Ubuntu` e a configuração da distro são **por usuário**,
    e como SYSTEM a distro seria instalada para a conta errada.
  - **Por que não `schtasks /create`**: o `/tr` tem limite de 262 caracteres [17] e o `/rl HIGHEST`
    com `/sc onlogon` dispara para *qualquer* usuário [17]. O cmdlet permite fixar o usuário.
  - Antes de registrar, copio o próprio binário para `%ProgramData%\toolkit\bin\`. A pasta Downloads
    ou a temp podem sumir entre os boots.
- **Linux** [18][19]:
  - `/etc/systemd/system/toolkit-resume.service`, com `Type=oneshot` (para oneshot o timeout de start
    vem desligado por padrão [18]), `Wants=` e `After=network-online.target` (a forma documentada de
    esperar a rede [19]), `WantedBy=multi-user.target` [19] e `ConditionPathExists=<state.json>`.
  - O registro faz `systemctl daemon-reload` e `systemctl enable` (não `start`). A unit roda
    `toolkit resume --yes --no-tui` sem terminal, e a saída vai para o journal e para o log.
- **macOS** [20]:
  - `~/Library/LaunchAgents/<label>.plist` com `RunAtLoad` e `LaunchOnlyOnce`, rodando no login do
    usuário.
  - O man page desaconselha `RunAtLoad` de modo geral [20], mas aqui a execução no load é exatamente
    o que se quer, e o arquivo é removido logo depois.
  - **LaunchAgent e não LaunchDaemon**: brew não roda como root [7], e o LaunchDaemon (em
    `/Library/LaunchDaemons`, de sistema [20]) rodaria como root sem sessão.
  - O agente abre o Terminal com um `.command` para o usuário ver o progresso e digitar a senha do
    sudo se alguma etapa pedir.

### 3.6 Elevação

- **Windows**: `toolkit` sem admin pode listar, ver o diagnóstico, rodar o dry-run e navegar na TUI.
  Ao executar algo que exige admin (winget com escopo de máquina, dism, receitas `requires_admin`),
  o toolkit se relança **uma vez**, elevado, via `ShellExecuteW` com verbo `runas`, repassando os
  argumentos e um `--plan <arquivo>` com o que foi escolhido na TUI. É só um prompt do UAC.
- **Unix**: a TUI roda como usuário e o `sudo` vai só nos `Cmd` com `Elevate=true` (apt, dnf, pacman,
  flatpak de sistema, escrita em `/etc/systemd`). Antes de executar, um `sudo -v` pede a senha uma vez
  e mantém o cache. Se o toolkit for chamado com sudo, uso `SUDO_USER` para brew e para os caminhos
  de usuário.

### 3.7 Bootstraps

- `win.ps1`, no Windows PowerShell 5.1:
  1. Força TLS 1.2.
  2. Detecta a arquitetura por `$env:PROCESSOR_ARCHITECTURE`, mais `PROCESSOR_ARCHITEW6432` para o
     caso de processo 32 bits em sistema 64.
  3. Baixa o zip e o `checksums.txt` da tag `$env:TOOLKIT_VERSION` (padrão: latest).
  4. Confere com `Get-FileHash -Algorithm SHA256`.
  5. Grava em `%ProgramData%\toolkit\bin\` e executa repassando `$args`.
- `unix.sh` segue o briefing, com `set -eu`, `trap` para limpar o temp e sem bashismos (pode ser
  lido por `sh`).
- **Receita `wsl-dev`, etapa `setup-inside`**: o exemplo do briefing faz `curl | bash` dentro do WSL.
  Proponho que o lado Windows baixe o `unix.sh` **da mesma versão do binário em execução**
  (`{{toolkit_version}}`), confira o hash pelo `checksums.txt` da release e rode
  `wsl -d Ubuntu -- bash /mnt/c/ProgramData/toolkit/bin/unix.sh recipe linux-dev`. Fica pinado e
  verificado, nada de pipe da internet.

### 3.8 Logs e segredos

- `slog` da stdlib. O arquivo de log por execução fica em `<StateDir>/logs/<data>-<runid>.log`, e
  `--verbose` duplica o log no terminal.
- O redator mascara `GITHUB_TOKEN`, `Authorization:` e qualquer `Cmd{Secret:true}`.
- A rotação mantém os últimos 20 arquivos.

### 3.9 CI local

O repositório está privado por enquanto, então o GitHub Actions consome a cota da conta, que está sem
orçamento. Por isso o gate é local.

Fatos [21]: runners padrão são **gratuitos em repositório público**. Em privado, o plano Free inclui
US$ 12/mês de compute, e o minuto custa US$ 0,006 no Linux, US$ 0,010 no Windows e US$ 0,062 no
macOS, cerca de 10 vezes o Linux. Sem forma de pagamento, o uso é bloqueado quando o incluído acaba.

Proposta:
1. **Fonte da verdade = `scripts/ci.sh`**, igual no local e no workflow:
   1. `gofmt -l` vazio.
   2. `go vet ./...` para `GOOS` linux, windows e darwin.
   3. `golangci-lint run` (v2.15.0 pinada [22]).
   4. `go test -race ./...`.
   5. `go test -c` para `GOOS=windows` e `GOOS=linux`, que compila os testes dos outros SOs.
   6. Cross-compile dos 6 alvos com `CGO_ENABLED=0`.
   7. `goreleaser check`, quando o GoReleaser estiver instalado.
2. **Hook `pre-push` opcional** (`make hooks`) rodando o `ci.sh`, que funciona como o "CI" de fato
   enquanto não houver Actions.
3. **`ci.yml` escrito com a matriz pedida** (`windows-latest`, `ubuntu-latest`, `macos-latest`), mas
   com `on: workflow_dispatch` apenas. Não dispara sozinho nem gasta cota. Quando o repositório for
   público, os runners padrão deixam de ser cobrados [21], e basta acrescentar `push` e `pull_request`.
4. **`release.yml`** dispara por tag `v*`. Enquanto não houver Actions, a release sai localmente com
   `goreleaser release --clean` (precisa de `GITHUB_TOKEN`), e `make snapshot` testa sem publicar.
5. **Lacuna real**: sem runner Windows, nada roda de fato no Windows. Ao fim das fases 2 e 3, o Paulo
   faz um smoke manual numa VM Windows 11 seguindo um roteiro entregue junto, sempre com `--dry-run`
   primeiro.

### 3.10 Dependências (supply chain)

Versões exatas em `go.mod`, `go.sum` commitado e `go mod verify` no `ci.sh`.

| Módulo | Versão hoje | Observação |
|---|---|---|
| Go | 1.27.3 [23] | o Bubble Tea v2 exige `go 1.26` [25] |
| github.com/spf13/cobra | v1.10.2 [24] | |
| charm.land/bubbletea/v2 | v2.1.1 [25] | v2 mudou o caminho do módulo para `charm.land/...` |
| charm.land/bubbles/v2, charm.land/lipgloss/v2 | versão exata fixada na fase 4 | mesmo esquema de caminho |
| YAML | `go.yaml.in/yaml/v3` [27] | `gopkg.in/yaml.v3` está **arquivado e sem manutenção** desde 2025-04-01 [26] |
| golang.org/x/sys | já vem transitiva do Bubble Tea [25] | uso direto em `elevate` (aceito fora da lista do briefing) |

Ferramentas locais: Go 1.27.3 via `GOTOOLCHAIN` (conferido pelo checksum DB do Go) e golangci-lint
v2.15.0 pelo binário oficial da release, com SHA-256 conferido.

### 3.11 Idioma e identificadores

- Identificadores e nomes de arquivo em inglês. Mensagens ao usuário, docs e comentários em pt-BR.
- Strings de UI centralizadas num pacote, para permitir outro idioma depois.
- Label do launchd e nome da tarefa agendada: `dev.pilutech.<name>`, derivado de `buildinfo.Name`.
- Licença MIT.

## Fontes


1. winget return codes — https://github.com/microsoft/winget-cli/blob/master/doc/windows/package-manager/winget/returnCodes.md
2. winget install — https://learn.microsoft.com/en-us/windows/package-manager/winget/install
3. winget show (`--versions`) — https://learn.microsoft.com/en-us/windows/package-manager/winget/show
4. winget list — https://learn.microsoft.com/en-us/windows/package-manager/winget/list
5. WinGet visão geral (App Installer, `Add-AppxPackage -RegisterByFamilyName`, Microsoft.WinGet.Client) — https://learn.microsoft.com/en-us/windows/package-manager/winget/
6. Homebrew manpage (`info --json=v2`, `list --versions`, `HOMEBREW_NO_AUTO_UPDATE`) — https://docs.brew.sh/Manpage
7. Homebrew FAQ ("Why does Homebrew say sudo is bad?") — https://docs.brew.sh/FAQ
8. apt-cache(8) (`madison`, `policy`) — https://manpages.debian.org/trixie/apt/apt-cache.8.en.html
9. apt-get(8) (`pkg=versão`, `-y`, exit 100) — https://manpages.debian.org/trixie/apt/apt-get.8.en.html
10. dnf5 list (`--showduplicates`) — https://dnf5.readthedocs.io/en/latest/commands/list.8.html
11. pacman(8) — https://man.archlinux.org/man/pacman.8.en
12. Flatpak command reference (`install --noninteractive`, `list --columns`, `remote-info --log`, `update --commit`) — https://docs.flatpak.org/en/latest/flatpak-command-reference.html
13. GitHub changelog: releases expõem digests dos assets (2025-06-03) — https://github.blog/changelog/2025-06-03-releases-now-expose-digests-for-release-assets/
14. GitHub REST: release assets (`digest`) — https://docs.github.com/en/rest/releases/assets
15. New-ScheduledTaskTrigger (`-AtLogOn`, `-User`) — https://learn.microsoft.com/en-us/powershell/module/scheduledtasks/new-scheduledtasktrigger
16. New-ScheduledTaskPrincipal (`-RunLevel Highest`, `-LogonType Interactive`) — https://learn.microsoft.com/en-us/powershell/module/scheduledtasks/new-scheduledtaskprincipal
17. schtasks create (`/sc onlogon` = qualquer usuário, `/tr` ≤ 262 caracteres, `/rl`, System sem logon interativo) — https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/schtasks-create
18. systemd.service (`Type=oneshot`, `RemainAfterExit`, timeout de oneshot) — https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html
19. systemd.special (`network-online.target`, `multi-user.target`) — https://www.freedesktop.org/software/systemd/man/latest/systemd.special.html
20. launchd.plist(5) (`RunAtLoad`, `LaunchOnlyOnce`, diretórios de agents e daemons) — https://keith.github.io/xcode-man-pages/launchd.plist.5.html
21. Cobrança do GitHub Actions (grátis em público, US$ 12 no Free, preço por minuto) — https://docs.github.com/en/billing/concepts/product-billing/github-actions
22. golangci-lint releases (v2.15.0, 2026-10-16) — https://github.com/golangci/golangci-lint/releases
23. Go downloads (1.27.3 estável) — https://go.dev/dl/
24. Cobra releases (v1.10.2, migrou para go.yaml.in/yaml/v3) — https://github.com/spf13/cobra/releases
25. Bubble Tea `go.mod` (módulo `charm.land/bubbletea/v2`, `go 1.26.0`, `golang.org/x/sys`) e releases (v2.1.1) — https://github.com/charmbracelet/bubbletea
26. go-yaml/yaml arquivado em 2025-04-01 — https://github.com/go-yaml/yaml
27. yaml/go-yaml (v3 estável e congelado em `go.yaml.in/yaml/v3`; v4 em desenvolvimento) — https://github.com/yaml/go-yaml
