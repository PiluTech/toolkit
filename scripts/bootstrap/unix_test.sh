#!/usr/bin/env bash
# Testa o unix.sh sem rede e sem instalar nada fora de um diretório temporário: release falsa em
# file:// e num servidor HTTP local, checksums adulterados (deve falhar e não instalar), dry-run.
set -euo pipefail
cd "$(dirname "$0")"
script="$PWD/unix.sh"

tmp="$(mktemp -d)"
srv=""
cleanup() {
  if [ -n "$srv" ]; then kill "$srv" 2>/dev/null || true; wait "$srv" 2>/dev/null || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT

fail() { echo "FALHOU: $*" >&2; exit 1; }

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) echo "SO sem teste"; exit 0 ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; arm64 | aarch64) arch=arm64 ;; *) echo "arch sem teste"; exit 0 ;; esac
asset="toolkit_${os}_${arch}"

hash_of() { if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }

rel="$tmp/release"
mkdir -p "$rel"
printf '#!/bin/sh\necho toolkit-falso\n' >"$rel/$asset"
printf '%s  %s\n' "$(hash_of "$rel/$asset")" "$asset" >"$rel/checksums.txt"

# run <base> [args...]: roda o bootstrap com HOME e destino isolados; devolve o status.
run() {
  local base="$1"; shift
  rm -rf "${tmp:?}/home"; mkdir -p "$tmp/home"
  HOME="$tmp/home" TOOLKIT_RELEASE_BASE="$base" sh "$script" "$@" >"$tmp/out" 2>&1
}
installed() { [ -x "$tmp/home/.local/bin/toolkit" ]; }

# 1. caminho feliz
run "file://$rel" || fail "instalação válida falhou: $(cat "$tmp/out")"
installed || fail "binário não foi instalado em ~/.local/bin"
[ "$("$tmp/home/.local/bin/toolkit")" = "toolkit-falso" ] || fail "binário instalado não é o baixado"
grep -q "SHA-256 conferido" "$tmp/out" || fail "faltou a confirmação do hash"

# 2. hash adulterado: aborta e não instala
printf '%s  %s\n' "$(printf 'x' | { sha256sum 2>/dev/null || shasum -a 256; } | cut -d' ' -f1)" "$asset" >"$rel/checksums.txt"
run "file://$rel" && fail "checksums adulterado foi aceito"
installed && fail "instalou mesmo com hash divergente"
grep -q "diverge" "$tmp/out" || fail "mensagem de divergência ausente: $(cat "$tmp/out")"

# 3. binário adulterado depois de gerar o checksums
printf '%s  %s\n' "$(hash_of "$rel/$asset")" "$asset" >"$rel/checksums.txt"
printf 'malicioso' >>"$rel/$asset"
run "file://$rel" && fail "binário adulterado foi aceito"
installed && fail "instalou binário adulterado"
printf '#!/bin/sh\necho toolkit-falso\n' >"$rel/$asset"

# 4. asset ausente do checksums.txt
printf '%s  outro_arquivo\n' "$(hash_of "$rel/$asset")" >"$rel/checksums.txt"
run "file://$rel" && fail "asset fora do checksums.txt foi aceito"
installed && fail "instalou sem entrada no checksums.txt"
grep -q "não lista" "$tmp/out" || fail "mensagem de asset ausente: $(cat "$tmp/out")"

# 5. checksums.txt inexistente
rm "$rel/checksums.txt"
run "file://$rel" && fail "sem checksums.txt foi aceito"
installed && fail "instalou sem checksums.txt"
printf '%s  %s\n' "$(hash_of "$rel/$asset")" "$asset" >"$rel/checksums.txt"

# 6. dry-run não baixa nem instala (base inexistente prova que não houve download)
run "file://$tmp/nao-existe" --dry-run || fail "dry-run falhou: $(cat "$tmp/out")"
installed && fail "dry-run instalou"
grep -q "dry-run" "$tmp/out" || fail "dry-run sem saída"

# 7. argumento desconhecido e versão inválida
run "file://$rel" --nao-existe && fail "argumento desconhecido foi aceito"
rm -rf "${tmp:?}/home"; mkdir -p "$tmp/home"
if HOME="$tmp/home" TOOLKIT_VERSION='v1/../x' sh "$script" --dry-run >"$tmp/out" 2>&1; then fail "versão inválida aceita"; fi
HOME="$tmp/home" TOOLKIT_VERSION='0.1.0' sh "$script" --dry-run >"$tmp/out" 2>&1 || fail "versão 0.1.0 recusada"
grep -q "releases/download/v0.1.0" "$tmp/out" || fail "versão sem v não foi normalizada: $(cat "$tmp/out")"
HOME="$tmp/home" sh "$script" --dry-run >"$tmp/out" 2>&1 || fail "dry-run padrão falhou"
grep -q "releases/latest/download" "$tmp/out" || fail "URL padrão não é latest"
grep -q "github.com/PiluTech/toolkit" "$tmp/out" || fail "repositório errado"

# 8. SO não suportado (uname falso no PATH)
mkdir "$tmp/fakebin"
printf '#!/bin/sh\necho Plan9\n' >"$tmp/fakebin/uname"; chmod +x "$tmp/fakebin/uname"
if PATH="$tmp/fakebin:$PATH" HOME="$tmp/home" sh "$script" --dry-run >"$tmp/out" 2>&1; then fail "SO não suportado aceito"; fi

# 9. servidor HTTP local (quando há python3): caminho feliz e hash adulterado
if command -v python3 >/dev/null 2>&1; then
  port=$((20000 + RANDOM % 20000))
  (cd "$rel" && exec python3 -m http.server "$port" --bind 127.0.0.1 >/dev/null 2>&1) &
  srv=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do curl -fs "http://127.0.0.1:$port/checksums.txt" >/dev/null 2>&1 && break; sleep 0.3; done
  run "http://127.0.0.1:$port" || fail "instalação via HTTP falhou: $(cat "$tmp/out")"
  installed || fail "HTTP: não instalou"
  printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "$asset" >"$rel/checksums.txt"
  run "http://127.0.0.1:$port" && fail "HTTP: hash adulterado aceito"
  installed && fail "HTTP: instalou com hash adulterado"
fi

echo "ok unix_test"
