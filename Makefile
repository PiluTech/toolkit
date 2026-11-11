.PHONY: ci test build build-all hooks

ci:
	./scripts/ci.sh

test:
	go test -race ./...

build:
	go build -trimpath -o bin/toolkit ./cmd/toolkit

build-all:
	for t in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		CGO_ENABLED=0 GOOS=$${t%/*} GOARCH=$${t#*/} go build -trimpath -o /dev/null ./cmd/toolkit || exit 1; \
	done

# Hook pre-push opcional: roda o gate local antes de subir.
hooks:
	printf '#!/bin/sh\nexec ./scripts/ci.sh\n' > .git/hooks/pre-push && chmod +x .git/hooks/pre-push
