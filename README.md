# toolkit

Prepara uma máquina Windows, Linux ou macOS com um comando colado no terminal: instala apps a partir
de um catálogo, escolhe versões e roda receitas de várias etapas que continuam depois de um reboot
(por exemplo: habilitar o WSL, reiniciar, instalar o Ubuntu e configurar o ambiente de dev dentro dele).

> **Estado: em desenvolvimento, sem release ainda.** Os comandos abaixo mostram como a ferramenta vai
> funcionar. Nada disso está publicado. "toolkit" é um nome provisório.

## Comandos de uma linha (planejados)

Windows (PowerShell 5.1 ou 7, como administrador quando a receita pedir):

```powershell
irm https://github.com/PiluTech/toolkit/releases/latest/download/win.ps1 | iex
```

Linux e macOS:

```sh
curl -fsSL https://github.com/PiluTech/toolkit/releases/latest/download/unix.sh | bash
```

Os bootstraps são curtos e legíveis (cerca de 20 linhas cada). Eles baixam o binário da release, conferem
o SHA-256 pelo `checksums.txt` da mesma release e só então executam. Para fixar uma versão, defina
`TOOLKIT_VERSION` antes de rodar (padrão: a última release).

Depois de instalado:

```sh
toolkit                         # abre a TUI (abas Apps, Receitas, Diagnóstico, Log)
toolkit list                    # lista o catálogo e o que já está instalado
toolkit install python@3.12 discord git
toolkit install --dry-run vscode   # mostra o que faria, sem alterar nada
toolkit recipe list
toolkit recipe run wsl-dev
toolkit recipe status
toolkit resume                  # chamado sozinho depois do reboot; pode ser rodado à mão
```

Flags para automação: `--no-tui`, `--yes`, `--dry-run`, `--verbose` e `--catalog <arquivo>` (uma URL
só com `--catalog-sha256 <hash>`).

## Plataformas

Binários estáticos (sem CGO) para:

| SO | amd64 | arm64 |
|---|---|---|
| Windows 10/11 | sim | sim |
| Linux | sim | sim |
| macOS | sim | sim |

Gerenciadores usados conforme o que existe na máquina: winget (Windows), Homebrew (macOS), apt, dnf,
pacman e Flatpak (Linux). Também baixa releases do GitHub e URLs diretas, sempre com hash.

## Como funciona

### Catálogo

Cada app é declarado uma vez em `catalog/catalog.yaml`, com uma lista de formas de instalar por SO, em
ordem de preferência. Você escolhe "Discord" e o toolkit usa o primeiro gerenciador que existir na máquina.

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
```

O catálogo vai embutido no binário e é validado na carga: um erro aponta o arquivo, a linha e a coluna.
Rodar a instalação duas vezes não reinstala o que já existe. Formato completo em
[docs/catalogo.md](docs/catalogo.md).

### Receitas

Uma receita é uma sequência de etapas em YAML. Cada etapa pode ter um `check`: se ele passar, a etapa é
pulada. Quando uma etapa pede reboot, o toolkit salva o estado, registra a retomada (Tarefa Agendada no
Windows, unit systemd no Linux, LaunchAgent no macOS), reinicia e continua de onde parou. Se a mesma
etapa falhar depois da retomada, ele para e reporta, sem entrar em loop de reboot.

Diagnósticos (RAM, disco, `sfc`, DISM, rede) também são receitas. Formato completo em
[docs/receitas.md](docs/receitas.md).

## O que o toolkit não faz

- **Não ativa nem contorna licenciamento.** Nada de ativadores de Windows ou Office, cracks ou
  "keys". O Office entra só pelo Office Deployment Tool oficial, com `config.xml` declarado.
- **Não baixa nada sem SHA-256.** Uma entrada de URL direta sem `sha256` é recusada na validação do
  catálogo, e uma release do GitHub sem checksum é recusada na instalação.
- **Não roda `curl | bash` de terceiros** dentro das receitas. Scripts só do próprio repositório, numa
  versão fixada e com hash conferido.
- **Não roda a TUI inteira como root.** No Linux e no macOS, o `sudo` vai só nos comandos que precisam.
  No Windows, o toolkit pede elevação (UAC) só quando algo escolhido exige admin.
- **Não grava segredos no log.** Tokens como o `GITHUB_TOKEN` são mascarados.

## Documentação

- [docs/arquitetura.md](docs/arquitetura.md): pastas, interfaces Go e decisões técnicas.
- [docs/decisoes.md](docs/decisoes.md): as decisões tomadas sobre a proposta, uma por linha.
- [docs/catalogo.md](docs/catalogo.md): formato do catálogo de apps.
- [docs/receitas.md](docs/receitas.md): formato das receitas e ciclo de reboot e retomada.
- [CONTRIBUTING.md](CONTRIBUTING.md): como adicionar um app ou uma receita.
- [SECURITY.md](SECURITY.md): como reportar uma vulnerabilidade.
- [docs/briefing.md](docs/briefing.md) e [docs/proposta.md](docs/proposta.md): pedido original e
  proposta de arquitetura (histórico do projeto).

## Licença

[MIT](LICENSE). Copyright (c) 2026 PiluTech.
