#!/bin/sh
# Bootstrap do toolkit para Linux e macOS: baixa o binário da release, confere o SHA-256 pelo
# checksums.txt da mesma release e instala em ~/.local/bin. Nunca usa sudo.
#
#   curl -fsSL https://github.com/PiluTech/toolkit/releases/latest/download/unix.sh | sh
#   ... | sh -s -- --dry-run        # só mostra o que faria
#   TOOLKIT_VERSION=v0.1.0 sh unix.sh   # fixa a versão (padrão: a última release)
#   ... | sh -s -- --versao v0.4.0-rc.1   # fixa a versão por argumento (vence TOOLKIT_VERSION; também -v)
#   ... | sh -s -- --no-launch      # não abre a TUI no fim (ou TOOLKIT_NO_LAUNCH=1)
#
# Depois de instalar, abre "toolkit tui" na própria janela, mas só com terminal real (/dev/tty) e
# nunca em CI. Se a TUI falhar, a instalação continua valendo (exit 0).
#
# Tudo roda dentro de main(), chamada na ÚLTIMA linha: se o download deste script for cortado no
# meio (curl | sh), o shell não chega a executar nada.
set -eu

REPO="PiluTech/toolkit"
tmp=""

die() { echo "toolkit: $*" >&2; exit 1; }
cleanup() { [ -z "$tmp" ] || rm -rf "$tmp"; }

# fetch URL ARQUIVO: só HTTPS (inclusive em redirecionamento), TLS 1.2+. TOOLKIT_RELEASE_BASE (só testes
# locais) libera http e file.
fetch() {
  if [ -n "${TOOLKIT_RELEASE_BASE:-}" ]; then proto='=https,http,file'; else proto='=https'; fi
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --proto "$proto" --proto-redir "$proto" --tlsv1.2 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    if [ "$proto" = '=https' ]; then wget -q --https-only -O "$2" "$1"; else wget -q -O "$2" "$1"; fi
  else
    die "preciso de curl ou wget"
  fi
}

# truthy VALOR: mesma regra do envOn do Go: ligado = definido e, sem espaços NAS PONTAS e sem distinguir
# maiúsculas, diferente de vazio, 0, false, no e off ("f alse" não é "false").
# Formato real da tag (mesma regra do release.yml): v opcional + X.Y.Z + sufixo opcional "-rc.1" (letras, números e
# pontos, sem ponto no começo, no fim nem repetido). "." e ".." não passam.
valid_tag() {
  vt="${1#v}"
  vcore="${vt%%-*}"
  case "$vcore" in "" | *[!0-9.]* | .* | *. | *..*) return 1 ;; esac
  [ "$(printf '%s' "$vcore" | tr -d '0-9')" = ".." ] || return 1
  [ "$vt" = "$vcore" ] && return 0
  vsuf="${vt#*-}"
  case "$vsuf" in "" | *[!0-9A-Za-z.]* | .* | *. | *..*) return 1 ;; esac
  return 0
}

truthy() {
  v="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  v="${v#"${v%%[![:space:]]*}"}"
  v="${v%"${v##*[![:space:]]}"}"
  case "$v" in
    "" | 0 | false | no | off) return 1 ;;
  esac
  return 0
}

# in_ci: algum sistema de CI conhecido está ativo.
in_ci() {
  truthy "${CI:-}" || truthy "${GITHUB_ACTIONS:-}" || truthy "${TF_BUILD:-}" || truthy "${GITLAB_CI:-}" ||
    truthy "${BUILDKITE:-}" || truthy "${CIRCLECI:-}" || truthy "${TRAVIS:-}" || truthy "${APPVEYOR:-}" ||
    truthy "${TEAMCITY_VERSION:-}" || truthy "${JENKINS_URL:-}" || truthy "${CODEBUILD_BUILD_ID:-}"
}

# stdout_is_tty: a saída deste script é um terminal? Sem nenhum gancho de ambiente: os testes trocam esta função
# numa CÓPIA do script (scripts/bootstrap/unix_test.sh), nunca no script distribuído.
stdout_is_tty() { [ -t 1 ]; }

# stdin_kind: tty, pipe ou file. O pipe é o do próprio "curl | sh" (a exceção atendida por /dev/tty); uma
# entrada de arquivo ("sh unix.sh < arquivo") ou /dev/null não é alguém para usar a interface.
stdin_kind() {
  if [ -t 0 ]; then echo tty
  elif [ -p /dev/stdin ]; then echo pipe
  else echo file; fi
}

# launch_tui BINARIO: abre "toolkit tui" depois de instalar. Só com saída E entrada de terminal: a saída tem de
# ser um terminal (com "> log" ou "| tee" não abre) e a entrada, um terminal ou o pipe do próprio curl | sh; neste
# caso a TUI lê de /dev/tty (só como stdin; a saída continua sendo a do script). Nunca falha o bootstrap.
launch_tui() {
  tty="/dev/tty"
  if truthy "${TOOLKIT_NO_LAUNCH:-}" || in_ci; then return 0; fi
  if ! stdout_is_tty || [ "$(stdin_kind)" = file ] || ! ( : <"$tty" ) 2>/dev/null; then
    echo "Instalação concluída. Sem terminal interativo para abrir a interface agora; para abri-la depois, rode: toolkit tui"
    return 0
  fi
  if ! "$1" tui <"$tty"; then
    echo "Instalação concluída, mas não foi possível abrir a interface. Para abri-la depois, rode: toolkit tui"
  fi
  return 0
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | cut -d' ' -f1
  else die "preciso de sha256sum ou shasum"; fi
}

main() {
  DRY=0
  NOLAUNCH=0
  # Versão: --versao/-v <tag> (ou --versao=<tag>) vence TOOLKIT_VERSION, que vence "latest".
  version="${TOOLKIT_VERSION:-latest}"
  want_ver=0
  for a in "$@"; do
    if [ "$want_ver" = 1 ]; then
      case "$a" in "" | -*) die "--versao exige um valor, por exemplo: --versao v0.4.0-rc.1" ;; esac
      version="$a"; want_ver=0; continue
    fi
    case "$a" in
      --dry-run) DRY=1 ;;
      --no-launch) NOLAUNCH=1 ;;
      --versao | -v) want_ver=1 ;;
      --versao=*) version="${a#--versao=}"; [ -n "$version" ] || die "--versao exige um valor, por exemplo: --versao v0.4.0-rc.1" ;;
      *) echo "toolkit: argumento desconhecido: $a (use --dry-run, --no-launch ou --versao)" >&2; exit 2 ;;
    esac
  done
  [ "$want_ver" = 0 ] || die "--versao exige um valor, por exemplo: --versao v0.4.0-rc.1"

  case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) die "sistema não suportado: $(uname -s)" ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "arquitetura não suportada: $(uname -m)" ;;
  esac

  # URL base: a última release ou uma tag fixa.
  if [ -n "${TOOLKIT_RELEASE_BASE:-}" ]; then
    base="$TOOLKIT_RELEASE_BASE"
    echo "toolkit: AVISO: usando TOOLKIT_RELEASE_BASE=$base (só para testes)" >&2
  elif [ "$version" = latest ]; then
    base="https://github.com/$REPO/releases/latest/download"
  else
    valid_tag "$version" || die "versão inválida (--versao ou TOOLKIT_VERSION): use algo como v0.1.0 ou v0.4.0-rc.1"
    case "$version" in [0-9]*) version="v$version" ;; esac
    base="https://github.com/$REPO/releases/download/$version"
  fi

  asset="toolkit_${os}_${arch}"
  dest="${TOOLKIT_INSTALL_DIR:-$HOME/.local/bin}"

  if [ "$DRY" = 1 ]; then
    echo "toolkit (dry-run): versão $version"
    echo "toolkit (dry-run): baixaria $base/$asset e $base/checksums.txt"
    echo "toolkit (dry-run): conferiria o SHA-256 e instalaria em $dest/toolkit"
    return 0
  fi

  # Ctrl-C e kill também limpam o temporário e ABORTAM (sem isso o script seguiria para o próximo passo).
  trap cleanup EXIT
  trap 'cleanup; exit 130' INT
  trap 'cleanup; exit 143' TERM
  tmp="$(mktemp -d)"

  fetch "$base/$asset" "$tmp/$asset" || die "falha ao baixar $asset"
  fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "falha ao baixar checksums.txt"

  # Formato do sha256sum: "<hash>  <arquivo>" (o "*" do modo binário é aceito).
  want="$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")"
  [ -n "$want" ] || die "checksums.txt não lista $asset; nada foi instalado"
  got="$(sha256 "$tmp/$asset")"
  [ "$got" = "$want" ] || die "SHA-256 diverge para $asset (esperado $want, obtido $got); nada foi instalado"

  mkdir -p "$dest"
  chmod 755 "$tmp/$asset"
  cp "$tmp/$asset" "$dest/toolkit.new"
  mv -f "$dest/toolkit.new" "$dest/toolkit"
  echo "toolkit instalado em $dest/toolkit (SHA-256 conferido)"
  case ":$PATH:" in
    *":$dest:"*) ;;
    *) echo "Adicione ao PATH: export PATH=\"$dest:\$PATH\"" ;;
  esac
  # O binário novo pode não estar no PATH desta sessão: abre pelo caminho completo.
  if [ "$NOLAUNCH" = 0 ]; then launch_tui "$dest/toolkit"; fi
}

main "$@"
