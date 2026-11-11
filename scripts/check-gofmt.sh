#!/usr/bin/env bash
# Falha se algum .go estiver sem gofmt OU se o gofmt não conseguir processar (ex.: sintaxe quebrada).
# Uso: scripts/check-gofmt.sh [pasta...]   (padrão: .)
set -euo pipefail

if [ "$#" -eq 0 ]; then set -- .; fi

# O stderr do gofmt aparece para o usuário; o exit code != 0 reprova.
if ! unformatted="$(gofmt -l "$@")"; then
  echo "gofmt falhou (sintaxe inválida ou erro de leitura)" >&2
  exit 1
fi
if [ -n "$unformatted" ]; then
  echo "arquivos sem gofmt:" >&2
  echo "$unformatted" >&2
  exit 1
fi
