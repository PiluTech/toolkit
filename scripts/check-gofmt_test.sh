#!/usr/bin/env bash
# Teste do scripts/check-gofmt.sh: limpo passa; mal formatado e sintaxe quebrada reprovam.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

expect() { # expect <0|1> <descrição> <pasta>
  local want="$1" desc="$2" dir="$3" got=0
  "$here/check-gofmt.sh" "$dir" >/dev/null 2>&1 || got=1
  if [ "$got" != "$want" ]; then
    echo "FALHOU: $desc (esperado exit=$want, obtido $got)" >&2
    exit 1
  fi
  echo "ok: $desc"
}

mkdir "$tmp/limpo" "$tmp/feio" "$tmp/quebrado"
printf 'package x\n' >"$tmp/limpo/a.go"
printf 'package x\nfunc  f( ) {}\n' >"$tmp/feio/a.go"
printf 'package x\nfunc f( {\n' >"$tmp/quebrado/a.go"

expect 0 "arquivo formatado passa" "$tmp/limpo"
expect 1 "arquivo mal formatado reprova" "$tmp/feio"
expect 1 "sintaxe quebrada reprova" "$tmp/quebrado"
