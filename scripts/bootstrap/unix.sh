#!/bin/sh
# Bootstrap do toolkit para Linux e macOS: baixa o binário da release, confere o SHA-256 pelo
# checksums.txt da mesma release e instala em ~/.local/bin. Nunca usa sudo.
#
#   curl -fsSL https://github.com/PiluTech/toolkit/releases/latest/download/unix.sh | sh
#   ... | sh -s -- --dry-run        # só mostra o que faria
#   TOOLKIT_VERSION=v0.1.0 sh unix.sh   # fixa a versão (padrão: a última release)
set -eu

REPO="PiluTech/toolkit"
DRY=0
for a in "$@"; do
  case "$a" in
    --dry-run) DRY=1 ;;
    *) echo "toolkit: argumento desconhecido: $a (use --dry-run)" >&2; exit 2 ;;
  esac
done

die() { echo "toolkit: $*" >&2; exit 1; }

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

# URL base: a última release ou uma tag fixa. TOOLKIT_RELEASE_BASE existe só para testes locais.
version="${TOOLKIT_VERSION:-latest}"
if [ -n "${TOOLKIT_RELEASE_BASE:-}" ]; then
  base="$TOOLKIT_RELEASE_BASE"
  echo "toolkit: AVISO: usando TOOLKIT_RELEASE_BASE=$base (só para testes)" >&2
elif [ "$version" = latest ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  case "$version" in
    *[!A-Za-z0-9._+-]* | "") die "TOOLKIT_VERSION inválida: use algo como v0.1.0" ;;
  esac
  case "$version" in [0-9]*) version="v$version" ;; esac
  base="https://github.com/$REPO/releases/download/$version"
fi

asset="toolkit_${os}_${arch}"
dest="${TOOLKIT_INSTALL_DIR:-$HOME/.local/bin}"

if [ "$DRY" = 1 ]; then
  echo "toolkit (dry-run): baixaria $base/$asset e $base/checksums.txt"
  echo "toolkit (dry-run): conferiria o SHA-256 e instalaria em $dest/toolkit"
  exit 0
fi

fetch() { # fetch URL ARQUIVO
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$2" "$1"
  else
    die "preciso de curl ou wget"
  fi
}
sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | cut -d' ' -f1
  else die "preciso de sha256sum ou shasum"; fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

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
