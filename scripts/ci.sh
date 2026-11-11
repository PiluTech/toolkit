#!/usr/bin/env bash
# Gate local, a fonte da verdade do CI (o workflow roda este mesmo script).
# Não executa instalador, dism, apt nem reboot: os testes usam o exec.Runner falso.
set -euo pipefail
cd "$(dirname "$0")/.."

export GOFLAGS="${GOFLAGS:-} -mod=readonly"
# ~/.local/bin no FIM: um binário do usuário nunca sombreia go/gofmt/ferramentas do sistema.
export PATH="$PATH:$HOME/.local/bin"

step() { printf '\n==> %s\n' "$*"; }

step "go mod verify / tidy"
go mod verify
go mod tidy -diff

step "tools.sha256 consistente com a versão do instalador"
./scripts/instalar-ferramentas_test.sh

step "gofmt (autoteste do verificador + verificação)"
./scripts/check-gofmt_test.sh
./scripts/check-gofmt.sh .

step "go vet (linux, windows, darwin)"
for os in linux windows darwin; do
  GOOS=$os go vet ./...
done

step "golangci-lint"
if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "golangci-lint não encontrado (esperado v2.15.0): rode scripts/instalar-ferramentas.sh" >&2
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
