#!/usr/bin/env bash
# Testa a separação provisionar x gerar do ícone, SEM rede e sem baixar nada: `npm` é FALSO (registra a chamada e
# falha se for usado onde não deve) e o renderizador é um stub. Regressão (Lupa no #43): a geração roda offline.
set -euo pipefail
cd "$(dirname "$0")/.."
repo="$PWD"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FALHOU (gerar-icone): $*" >&2; exit 1; }
sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }

# npm falso: registra os argumentos; com NPM_CI_STUB=<dir> cria o renderizador stub no `npm ci`
fake="$tmp/fakebin"; npmlog="$tmp/npm.log"; mkdir "$fake"; : >"$npmlog"
cat >"$fake/npm" <<EOF2
#!/bin/sh
echo "npm \$*" >>"$npmlog"
if [ "\$1" = ci ] && [ -n "\${NPM_CI_STUB:-}" ]; then
  mkdir -p node_modules/@resvg/resvg-js
  printf '{"name":"@resvg/resvg-js","version":"%s"}' "\$NPM_CI_STUB" > node_modules/@resvg/resvg-js/package.json
fi
exit 0
EOF2
chmod +x "$fake/npm"

# renderizador stub (mesma API que o render.cjs usa) provisionado à mão
provision_stub() { # <dir> <versão> <hash do lock no marcador>
  mkdir -p "$1/icone/node_modules/@resvg/resvg-js"
  printf '{"name":"@resvg/resvg-js","version":"%s"}' "$2" >"$1/icone/node_modules/@resvg/resvg-js/package.json"
  cat >"$1/icone/node_modules/@resvg/resvg-js/index.js" <<'JS'
class Resvg { constructor(svg, opts) { this.w = opts.fitTo.value; }
  render() { const w = this.w; return { asPng: () => Buffer.from('PNG-STUB-' + w) }; } }
module.exports = { Resvg };
JS
  printf '%s' "$3" >"$1/icone/.provisionado"
}
lock="$(sha256 scripts/icone/package-lock.json)"
offline() { PATH="$fake:$PATH" npm_config_registry=http://127.0.0.1:9 npm_config_offline=true HTTPS_PROXY=http://127.0.0.1:9 HTTP_PROXY=http://127.0.0.1:9 "$@"; }

# 1. sem provisionar: falha com instrução clara e NÃO tenta npm/rede
mkdir "$tmp/t1"
if out="$(offline env TOOLKIT_TOOLS_DIR="$tmp/t1" ICONE_SAIDA="$tmp/out1" ./scripts/gerar-icone.sh 2>&1)"; then fail "gerou sem renderizador provisionado"; fi
echo "$out" | grep -q 'scripts/provisionar-icone.sh' || fail "a mensagem não manda provisionar: $out"
[ ! -s "$npmlog" ] || fail "a geração chamou o npm: $(cat "$npmlog")"
[ ! -e "$tmp/out1" ] || fail "criou a saída sem renderizador"

# 2. provisionado: gera os 6 arquivos OFFLINE (registry/proxy inválidos, npm falso nunca chamado)
mkdir "$tmp/t2"; provision_stub "$tmp/t2" 2.6.2 "$lock"
offline env TOOLKIT_TOOLS_DIR="$tmp/t2" ICONE_SAIDA="$tmp/out2" ./scripts/gerar-icone.sh >"$tmp/o2" 2>&1 || fail "a geração offline falhou: $(cat "$tmp/o2")"
for f in toolkit-1024.png toolkit-512.png toolkit-256.png toolkit-128.png toolkit-64.png toolkit-site.png toolkit.svg.sha256; do
  [ -s "$tmp/out2/$f" ] || fail "faltou $f"
done
[ "$(cat "$tmp/out2/toolkit-256.png")" = "PNG-STUB-256" ] || fail "o PNG 256 não veio do renderizador"
cmp -s "$tmp/out2/toolkit-1024.png" "$tmp/out2/toolkit-site.png" || fail "toolkit-site.png deve ser o de 1024"
[ ! -s "$npmlog" ] || fail "a geração offline chamou o npm: $(cat "$npmlog")"
grep -q "$(sha256 assets/icon/toolkit.svg)  toolkit.svg" "$tmp/out2/toolkit.svg.sha256" || fail "hash do SVG"

# 3. versão provisionada diferente da fixa: falha
mkdir "$tmp/t3"; provision_stub "$tmp/t3" 2.6.1 "$lock"
if offline env TOOLKIT_TOOLS_DIR="$tmp/t3" ICONE_SAIDA="$tmp/out3" ./scripts/gerar-icone.sh >"$tmp/o3" 2>&1; then fail "aceitou versão diferente da fixa"; fi
grep -q '2.6.2' "$tmp/o3" || fail "a mensagem não cita a versão fixa: $(cat "$tmp/o3")"

# 4. lockfile mudou desde a provisão (marcador de outro hash): falha e manda provisionar de novo
mkdir "$tmp/t4"; provision_stub "$tmp/t4" 2.6.2 "hash-antigo"
if offline env TOOLKIT_TOOLS_DIR="$tmp/t4" ICONE_SAIDA="$tmp/out4" ./scripts/gerar-icone.sh >"$tmp/o4" 2>&1; then fail "aceitou marcador de outro lockfile"; fi
grep -q 'provisionar-icone.sh' "$tmp/o4" || fail "sem instrução"

# 5. provisionar: lockfile adulterado (hash diferente do versionado) é recusado ANTES de qualquer npm
cp -R "$repo/scripts" "$tmp/copia_scripts"; mkdir "$tmp/repo5"; mv "$tmp/copia_scripts" "$tmp/repo5/scripts"
printf '\n' >>"$tmp/repo5/scripts/icone/package-lock.json"
: >"$npmlog"
if offline env TOOLKIT_TOOLS_DIR="$tmp/t5" "$tmp/repo5/scripts/provisionar-icone.sh" >"$tmp/o5" 2>&1; then fail "provisionou com lockfile adulterado"; fi
grep -q 'não bate com o hash versionado' "$tmp/o5" || fail "mensagem: $(cat "$tmp/o5")"
[ ! -s "$npmlog" ] || fail "o npm foi chamado com lockfile adulterado"

# 6. provisionar: usa `npm ci --ignore-scripts` (integridade do lockfile), nunca `npm install`, e é idempotente
: >"$npmlog"
NPM_CI_STUB=2.6.2 offline env TOOLKIT_TOOLS_DIR="$tmp/t6" ./scripts/provisionar-icone.sh >"$tmp/o6" 2>&1 || fail "provisionar falhou: $(cat "$tmp/o6")"
grep -q '^npm ci --ignore-scripts' "$npmlog" || fail "deve usar npm ci --ignore-scripts: $(cat "$npmlog")"
grep -q 'npm install' "$npmlog" && fail "não pode usar npm install"
[ "$(cat "$tmp/t6/icone/.provisionado")" = "$lock" ] || fail "marcador com o hash do lockfile"
: >"$npmlog"
offline env TOOLKIT_TOOLS_DIR="$tmp/t6" ./scripts/provisionar-icone.sh >"$tmp/o6b" 2>&1 || fail "2ª provisão falhou"
[ ! -s "$npmlog" ] || fail "já provisionado: não deve chamar o npm de novo"
# versão instalada diferente da fixa: falha
NPM_CI_STUB=2.6.1 offline env TOOLKIT_TOOLS_DIR="$tmp/t7" ./scripts/provisionar-icone.sh >"$tmp/o7" 2>&1 && fail "aceitou versão instalada diferente"

# 7. todo pacote do lockfile tem integridade sha512 e o lockfile fixa a versão de package.json
python3 -I - <<'PY'
import json, sys
lock = json.load(open("scripts/icone/package-lock.json"))
pkg = json.load(open("scripts/icone/package.json"))
assert pkg["dependencies"]["@resvg/resvg-js"] == "2.6.2", "versão exata em package.json"
bad = [k for k, v in lock["packages"].items() if k and not str(v.get("integrity", "")).startswith("sha512-")]
assert not bad, "sem integridade sha512: %s" % bad
assert lock["packages"]["node_modules/@resvg/resvg-js"]["version"] == "2.6.2"
PY

echo "ok gerar-icone_test"
