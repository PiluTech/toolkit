#!/usr/bin/env bash
# Gera o checksums.txt de uma pasta de release (SHA-256 de todos os arquivos, em ordem C, sem o próprio
# checksums.txt). Usado pelo release.sh e pelo release-sign-macos.sh: SEMPRE depois da assinatura, porque assinar
# muda o binário. Uso: scripts/release-checksums.sh <pasta>
set -euo pipefail
dir="${1:?uso: release-checksums.sh <pasta>}"
[ -d "$dir" ] || { echo "release-checksums: pasta inexistente: $dir" >&2; exit 1; }
cd "$dir"
export LC_ALL=C
rm -f checksums.txt
sums=""
for f in *; do
  [ -f "$f" ] || continue
  if command -v sha256sum >/dev/null 2>&1; then sums+="$(sha256sum "$f")"$'\n'; else sums+="$(shasum -a 256 "$f")"$'\n'; fi
done
printf '%s' "$sums" >checksums.txt
