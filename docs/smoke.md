# Roteiro de smoke (manual)

> O toolkit não instala nada de verdade nos testes automáticos (FakeRunner e dry-run). Este roteiro é o que o
> Paulo roda à mão ao fim da fase 2 (decisão 20): VM Windows 11, Mac e uma VM Linux. **Sempre `--dry-run`
> primeiro**; só depois a execução real, numa VM descartável. Nunca como root no Mac (o brew recusa).

## 0. Capturar as saídas reais (substituem as fixtures sintéticas)
Cada `testdata/` de provider tem um README dizendo que as fixtures são sintéticas. Capturar e substituir:
- **winget** (VM Windows 11, en-US e pt-BR): `winget list --id Git.Git -e` e `winget show --id Git.Git -e --versions` → `internal/providers/winget/testdata/` (conferir também sem `--accept-source-agreements`: a primeira execução pode perguntar dos termos da fonte).
- **apt** (Debian/Ubuntu): `apt-cache madison git` e `dpkg-query -W -f='${db:Status-Status}\t${Version}\n' git` → `internal/providers/apt/testdata/`.
- **dnf** (dnf4 numa Rocky/Alma e dnf5 numa Fedora): `dnf list --showduplicates git` e `rpm -q --qf '%{EVR}\n' git` → `internal/providers/dnf/testdata/`.
- pacman (`pacman -Q`, `-Si`), flatpak (`flatpak list --app --columns=application,version`) e brew (`brew list --versions`, `brew info --json=v2`): conferir que o formato bate com o parser.

## 1. Windows 11 (VM)
1. Sem administrador: `toolkit list`, `toolkit install git --dry-run` → o plano mostra `winget install --id Git.Git -e --source winget --silent ...` e nada é instalado (`winget list --id Git.Git -e` antes e depois: igual).
2. `toolkit install git@2.47.1 --dry-run` (app com `versions: true`) → o plano leva `--version 2.47.1`.
3. `toolkit install python@3.12 --dry-run` → `--id Python.Python.3.12`, **sem** `--version`.
4. Execução real numa VM descartável: `toolkit install 7zip`. Rodar de novo: tem de dizer "já instalado" sem reinstalar.
5. App que exige reboot (se houver um à mão): conferir `reinicie o sistema para concluir`.
6. Conferir o UAC: o winget pede sozinho quando o instalador exige; o toolkit não se relança elevado nesta fase.

## 2. Mac (brew)
1. `toolkit install git --dry-run` → `brew update` e `brew install --formula git` no plano; `brew list --versions git` real antes.
2. Execução real: um `brew update` só por execução, mesmo com vários apps (`toolkit install git vlc`).
3. Conferir se um cask de instalador `.pkg` (que pede a senha do sudo ao próprio brew) **falha rápido** com `ErrNeedsAdmin` ou **fica esperando o terminal até o timeout** (30 min). Se esperar, o `Cmd` do install precisa de `Stdin` fechado ou de um timeout curto para casks.
4. Rodar como root tem de recusar (`o Homebrew não roda como root`).

## 3. Linux (VM Debian/Ubuntu, Fedora, Arch)
1. `sudo -v` e depois `toolkit install git --dry-run`; o plano mostra `sudo -n apt-get update` e `sudo -n apt-get install -y --no-remove git`.
2. Sem `sudo -v` (credencial expirada), a execução real falha com a dica `rode "sudo -v"` (o toolkit nunca pede senha).
3. `toolkit install git@<versão do apt-cache madison>` (com época, `1:2.43.0-...`) instala `git=<versão>`.
4. Fedora: dnf5; Rocky/Alma: dnf4. Arch: um espelho com a base velha dá o erro que manda rodar `sudo pacman -Syu` (o toolkit nunca roda `-Sy`).
5. Flatpak: o `-y` baixa runtimes (GB); conferir o aviso e que o remote `flathub` existe (senão a mensagem ensina o `flatpak remote-add`).

## 4. Dados a conferir em todos
- `--dry-run` nunca altera a máquina: comparar a lista de pacotes antes e depois.
- A saída não tem sequência de terminal vinda de pacote/catálogo (tudo passa por `printable`/`Snippet`).
