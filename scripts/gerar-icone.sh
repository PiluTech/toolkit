#!/usr/bin/env bash
# Regenera os PNG do ícone a partir de assets/icon/toolkit.svg (nunca reamostra PNG pequeno).
# Instala @resvg/resvg-js numa pasta TEMPORÁRIA fora do repositório (apagada por trap) e rasteriza o SVG
# em cada tamanho. Registra o hash do SVG em assets/icon/toolkit.svg.sha256 (conferido por teste Go).
#
#   scripts/gerar-icone.sh
#
# Saída em assets/icon/: toolkit-1024.png, -512, -256, -128, -64 e toolkit-site.png (1024, para o site).
set -euo pipefail
cd "$(dirname "$0")/.."

RESVG_VERSION="2.6.2" # versão exata do @resvg/resvg-js
SVG="assets/icon/toolkit.svg"
OUT="assets/icon"
[ -f "$SVG" ] || { echo "gerar-icone: falta $SVG" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
ROOT="$PWD"

(cd "$TMP" && npm init -y >/dev/null && npm install --no-audit --no-fund --ignore-scripts "@resvg/resvg-js@${RESVG_VERSION}" >/dev/null)

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
(cd "$TMP" && node render.cjs "$ROOT/$SVG" "$ROOT/$OUT")

cp "$OUT/toolkit-1024.png" "$OUT/toolkit-site.png"
if command -v shasum >/dev/null 2>&1; then H="$(shasum -a 256 "$SVG")"; else H="$(sha256sum "$SVG")"; fi
printf '%s  toolkit.svg\n' "${H%% *}" >"$OUT/toolkit.svg.sha256"
echo "ok: PNG e hash gerados em $OUT"
