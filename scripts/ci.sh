#!/usr/bin/env bash
# Gate local, a fonte da verdade do CI (o workflow roda este mesmo script).
# Não executa instalador, dism, apt nem reboot: os testes usam o exec.Runner falso.
set -euo pipefail
cd "$(dirname "$0")/.."

export GOFLAGS="${GOFLAGS:-} -mod=readonly"
export PATH="$HOME/.local/bin:$PATH"

step() { printf '\n==> %s\n' "$*"; }

step "go mod verify / tidy"
go mod verify
go mod tidy -diff

step "gofmt"
unformatted="$(gofmt -l cmd internal *.go 2>/dev/null || true)"
if [ -n "$unformatted" ]; then
  echo "arquivos sem gofmt:" >&2
  echo "$unformatted" >&2
  exit 1
fi

step "go vet (linux, windows, darwin)"
for os in linux windows darwin; do
  GOOS=$os go vet ./...
done

step "golangci-lint"
if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "golangci-lint não encontrado (esperado v2.15.0)" >&2
  exit 1
fi
golangci-lint run ./...

step "go test -race"
go test -race ./...

step "compila os testes dos outros SOs (sem executar)"
for os in linux windows; do
  for pkg in $(go list ./...); do
    GOOS=$os go test -c -o /dev/null "$pkg"
  done
done

step "cross-compile dos 6 alvos (CGO_ENABLED=0)"
for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  CGO_ENABLED=0 GOOS="${target%/*}" GOARCH="${target#*/}" go build -trimpath -o /dev/null ./cmd/toolkit
  echo "ok $target"
done

if command -v goreleaser >/dev/null 2>&1 && [ -f .goreleaser.yaml ]; then
  step "goreleaser check"
  goreleaser check
fi

printf '\nCI local: tudo certo\n'
