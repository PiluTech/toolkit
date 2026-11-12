#!/usr/bin/env bash
# Provisiona o rasterizador do ícone (@resvg/resvg-js) com VERSÃO FIXA e INTEGRIDADE verificada. É o único passo
# que usa a rede, e só roda quando o renderizador falta (scripts/gerar-icone.sh nunca baixa nada).
#
#   scripts/provisionar-icone.sh
#
# Verificação (a regra do repo é "nada baixado sem hash"):
#   1. scripts/icone/package-lock.json tem de bater com o SHA-256 versionado em package-lock.sha256;
#   2. `npm ci --ignore-scripts` instala SÓ o que o lockfile fixa e o npm confere o `integrity` (sha512) de cada
#      tarball contra o lockfile (hash diferente = erro, nada é instalado); scripts de instalação ficam desligados;
#   3. a versão instalada tem de ser a fixa (2.6.2).
# O renderizador fica fora do repositório: $TOOLKIT_TOOLS_DIR/icone (padrão ~/.cache/toolkit-tools/icone).
set -euo pipefail
cd "$(dirname "$0")/.."

RESVG_VERSION="2.6.2"
LOCK_DIR="scripts/icone"
TOOLS="${TOOLKIT_TOOLS_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/toolkit-tools}"
DIR="$TOOLS/icone"

die() { echo "provisionar-icone: $*" >&2; exit 1; }
sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }

want="$(awk '{print $1}' "$LOCK_DIR/package-lock.sha256")"
got="$(sha256 "$LOCK_DIR/package-lock.json")"
if [ -z "$want" ] || [ "$got" != "$want" ]; then die "o package-lock.json não bate com o hash versionado (package-lock.sha256): esperado '$want', obtido '$got'"; fi

# já provisionado com este lockfile e esta versão: nada a fazer (e nenhuma rede)
if [ -f "$DIR/.provisionado" ] && [ "$(cat "$DIR/.provisionado")" = "$got" ] \
  && grep -Eq "\"version\"[[:space:]]*:[[:space:]]*\"$RESVG_VERSION\"" "$DIR/node_modules/@resvg/resvg-js/package.json" 2>/dev/null; then
  echo "provisionar-icone: já provisionado em $DIR (resvg-js $RESVG_VERSION)"
  exit 0
fi

command -v npm >/dev/null 2>&1 || die "npm não encontrado (precisa de Node 18+ só para provisionar)"
rm -rf "$DIR"
mkdir -p "$DIR"
cp "$LOCK_DIR/package.json" "$LOCK_DIR/package-lock.json" "$DIR/"
(cd "$DIR" && npm ci --ignore-scripts --no-audit --no-fund >/dev/null)

grep -Eq "\"version\"[[:space:]]*:[[:space:]]*\"$RESVG_VERSION\"" "$DIR/node_modules/@resvg/resvg-js/package.json" \
  || die "a versão instalada de @resvg/resvg-js não é a fixa ($RESVG_VERSION)"
printf '%s' "$got" >"$DIR/.provisionado"
echo "provisionar-icone: ok, @resvg/resvg-js $RESVG_VERSION (integridade conferida pelo npm contra o lockfile) em $DIR"
