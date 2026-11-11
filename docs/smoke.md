# Roteiro de smoke (manual)

> Esqueleto da fase 2. O toolkit não instala nada de verdade nos testes automáticos: este roteiro é o que o
> Paulo roda à mão (decisão 20), sempre começando por `--dry-run`. O roteiro completo do Windows 11 chega com o
> comando `toolkit install` (último PR da fase 2).

## Capturar as saídas reais (substituem as fixtures sintéticas)
- winget (VM Windows 11, en-US e pt-BR): `winget list --id Git.Git -e` e `winget show --id Git.Git -e --versions` → `internal/providers/winget/testdata/`.
- apt (VM Debian/Ubuntu): `apt-cache madison git` e `dpkg-query -W -f='${db:Status-Status}\t${Version}\n' git` → `internal/providers/apt/testdata/`.
- dnf (dnf4 e dnf5), pacman, flatpak: ver os `testdata/` de cada provider quando existirem.

## Mac (brew)
- Conferir se um cask de instalador `.pkg` (que pede a senha do sudo ao próprio brew) **falha rápido** com `ErrNeedsAdmin` ou **fica esperando o terminal até o timeout** (30 min). Se esperar, o `Cmd` do install precisa de `Stdin` fechado ou de um timeout curto para casks.
- Rodar sempre `--dry-run` antes; nunca como root.
