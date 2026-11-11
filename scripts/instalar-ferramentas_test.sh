#!/usr/bin/env bash
# Autoteste da conferência de versão do tools.sha256 (sem rede): o arquivo real passa; versão trocada,
# entrada de outra versão e arquivo sem entrada falham.
set -euo pipefail
cd "$(dirname "$0")/.."

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
run() { TOOLS_SHA256_FILE="$1" ./scripts/instalar-ferramentas.sh --verificar >/dev/null 2>&1; }

run tools.sha256 || { echo "FALHOU: o tools.sha256 real deveria passar" >&2; exit 1; }

sed 's/2\.15\.0/2.14.0/g' tools.sha256 >"$tmp/outra-versao"
if run "$tmp/outra-versao"; then echo "FALHOU: arquivo de outra versão passou" >&2; exit 1; fi

{ cat tools.sha256; echo "0000000000000000000000000000000000000000000000000000000000000000  golangci-lint-2.14.0-linux-amd64.tar.gz"; } >"$tmp/misturado"
if run "$tmp/misturado"; then echo "FALHOU: arquivo misturado passou" >&2; exit 1; fi

echo "# só comentário release v2.15.0 " >"$tmp/vazio"
if run "$tmp/vazio"; then echo "FALHOU: arquivo sem entradas passou" >&2; exit 1; fi

echo "ok instalar-ferramentas_test"
