#!/usr/bin/env bash
# Instala o golangci-lint oficial (release v2.15.0) e o shellcheck (release v0.11.0) conferindo o SHA-256 versionado em tools.sha256.
# Uso: scripts/instalar-ferramentas.sh [pasta-destino]   (padrão: ~/.local/bin)
# O arquivo baixado só é extraído se o hash bater; qualquer divergência aborta sem instalar nada.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="2.15.0"
SC_VERSION="0.11.0"
SUMS="${TOOLS_SHA256_FILE:-tools.sha256}"

# Confere que todo hash de tools.sha256 é da VERSION deste script (um arquivo de outra versão, ou misturado,
# aprovaria o binário errado). Uso: scripts/instalar-ferramentas.sh --verificar
check_sums() {
  local n=0 sc=0 line file
  while IFS= read -r line; do
    case "$line" in '' | '#'*) continue ;; esac
    file="$(printf '%s\n' "$line" | awk '{ print $2 }')"
    case "$file" in
      golangci-lint-"${VERSION}"-*) n=$((n + 1)) ;;
      shellcheck-v"${SC_VERSION}".*) sc=$((sc + 1)) ;;
      *) echo "$SUMS: a entrada \"$file\" não é da versão $VERSION (VERSION/SC_VERSION em instalar-ferramentas.sh)" >&2; return 1 ;;
    esac
  done <"$SUMS"
  if [ "$n" -eq 0 ]; then echo "$SUMS: sem nenhuma entrada da versão $VERSION" >&2; return 1; fi
  if [ "$sc" -eq 0 ]; then echo "$SUMS: sem nenhuma entrada do shellcheck v$SC_VERSION" >&2; return 1; fi
  if ! grep -q "release v${SC_VERSION} " "$SUMS"; then
    echo "$SUMS: o comentário de cabeçalho não cita a release v$SC_VERSION do shellcheck" >&2; return 1
  fi
  if ! grep -q "release v${VERSION} " "$SUMS"; then
    echo "$SUMS: o comentário de cabeçalho não cita a release v$VERSION" >&2; return 1
  fi
}
check_sums
if [ "${1:-}" = "--verificar" ]; then echo "tools.sha256 consistente com golangci-lint $VERSION e shellcheck $SC_VERSION"; exit 0; fi
DEST="${1:-$HOME/.local/bin}"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  MINGW* | MSYS* | CYGWIN*) os=windows ;;
  *) echo "sistema não suportado: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "arquitetura não suportada: $(uname -m)" >&2; exit 1 ;;
esac

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$DEST"

# baixa $2 (url), confere o SHA-256 de $1 (nome do arquivo) em tools.sha256 e deixa o arquivo em $tmp/$1
fetch_checked() {
  local name="$1" url="$2" expected actual
  expected="$(awk -v n="$name" '$2 == n { print $1 }' "$SUMS")"
  if [ -z "$expected" ]; then echo "sem SHA-256 versionado para $name em $SUMS" >&2; return 1; fi
  echo "baixando $url"
  curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 --output "$tmp/$name" "$url"
  actual="$(sha256_of "$tmp/$name")"
  if [ "$actual" != "$expected" ]; then
    echo "SHA-256 diverge para $name" >&2
    echo "  esperado: $expected" >&2
    echo "  obtido:   $actual" >&2
    return 1
  fi
  echo "SHA-256 conferido: $actual"
}

# golangci-lint
ext=tar.gz
bin=golangci-lint
if [ "$os" = windows ]; then ext=zip; bin=golangci-lint.exe; fi
name="golangci-lint-${VERSION}-${os}-${arch}.${ext}"
fetch_checked "$name" "https://github.com/golangci/golangci-lint/releases/download/v${VERSION}/${name}"
mkdir -p "$tmp/x"
if [ "$ext" = zip ]; then
  unzip -q "$tmp/$name" -d "$tmp/x"
else
  tar -xzf "$tmp/$name" -C "$tmp/x"
fi
install -m 0755 "$tmp/x/golangci-lint-${VERSION}-${os}-${arch}/$bin" "$DEST/$bin"
echo "instalado em $DEST/$bin"

# analisador de scripts (sem build para Windows nesta lista: lá o ci.sh pula a análise estática)
if [ "$os" != windows ]; then
  case "$arch" in amd64) sc_arch=x86_64 ;; arm64) sc_arch=aarch64 ;; esac
  sc_name="shellcheck-v${SC_VERSION}.${os}.${sc_arch}.tar.gz"
  fetch_checked "$sc_name" "https://github.com/koalaman/shellcheck/releases/download/v${SC_VERSION}/${sc_name}"
  mkdir -p "$tmp/sc"
  tar -xzf "$tmp/$sc_name" -C "$tmp/sc"
  install -m 0755 "$tmp/sc/shellcheck-v${SC_VERSION}/shellcheck" "$DEST/shellcheck"
  echo "instalado em $DEST/shellcheck"
fi
