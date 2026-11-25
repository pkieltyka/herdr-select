# Build herdr-select into ./bin/. The version matches the Nix flake: v<VERSION>+<short rev>.

BIN     := bin/herdr-select
REV     := $(shell git rev-parse --short=6 HEAD 2>/dev/null || echo unknown)
DIRTY   := $(shell git diff --quiet HEAD 2>/dev/null || echo .dirty)
VERSION := v$(shell cat VERSION)+$(REV)$(DIRTY)

.PHONY: build clean

build:
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/herdr-select

clean:
	rm -rf bin
