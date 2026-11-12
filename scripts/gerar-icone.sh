#!/usr/bin/env bash
# Regenera os PNG do ícone a partir de assets/icon/toolkit.svg (nunca reamostra PNG pequeno).
# SEM REDE: usa o renderizador já provisionado por scripts/provisionar-icone.sh (versão fixa, integridade
# verificada) e falha com instrução clara se ele faltar. Registra o hash do SVG em assets/icon/toolkit.svg.sha256
# (conferido por teste Go).
#
#   scripts/provisionar-icone.sh   # uma vez (único passo com rede)
#   scripts/gerar-icone.sh
#
# Saída em assets/icon/: toolkit-1024.png, -512, -256, -128, -64 e toolkit-site.png (1024, para o site).
set -euo pipefail
cd "$(dirname "$0")/.."

RESVG_VERSION="2.6.2"
SVG="${ICONE_SVG:-assets/icon/toolkit.svg}"
OUT="${ICONE_SAIDA:-assets/icon}"
TOOLS="${TOOLKIT_TOOLS_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/toolkit-tools}"
DIR="$TOOLS/icone"

die() { echo "gerar-icone: $*" >&2; exit 1; }
sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }

[ -f "$SVG" ] || die "falta $SVG"
command -v node >/dev/null 2>&1 || die "node não encontrado"

# o renderizador provisionado: marcador com o hash do lockfile versionado e a versão fixa (nada de rede aqui)
lock="$(sha256 scripts/icone/package-lock.json)"
if [ ! -f "$DIR/.provisionado" ] || [ "$(cat "$DIR/.provisionado")" != "$lock" ]; then
  die "o renderizador não está provisionado (ou o lockfile mudou). Rode UMA vez: scripts/provisionar-icone.sh (único passo que usa a rede, com integridade verificada)"
fi
grep -Eq "\"version\"[[:space:]]*:[[:space:]]*\"$RESVG_VERSION\"" "$DIR/node_modules/@resvg/resvg-js/package.json" 2>/dev/null \
  || die "a versão provisionada de @resvg/resvg-js não é a fixa ($RESVG_VERSION): rode scripts/provisionar-icone.sh de novo"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cat >"$TMP/render.cjs" <<'JS'
const { Resvg } = require('@resvg/resvg-js');
const fs = require('fs');
const [svgPath, outDir] = process.argv.slice(2);
const svg = fs.readFileSync(svgPath);
// Sem `background`: os cantos arredondados do SVG ficam com alfa 0.
for (const size of [1024, 512, 256, 128, 64]) {
  const png = new Resvg(svg, { fitTo: { mode: 'width', value: size } }).render().asPng();
  fs.writeFileSync(`${outDir}/toolkit-${size}.png`, png);
}
JS
mkdir -p "$OUT"
abs() { case "$1" in /*) printf '%s' "$1" ;; *) printf '%s/%s' "$PWD" "$1" ;; esac; }
NODE_PATH="$DIR/node_modules" node "$TMP/render.cjs" "$(abs "$SVG")" "$(abs "$OUT")"

cp "$OUT/toolkit-1024.png" "$OUT/toolkit-site.png"
printf '%s  toolkit.svg\n' "$(sha256 "$SVG")" >"$OUT/toolkit.svg.sha256"
echo "ok: PNG e hash gerados em $OUT"
