GO ?= go
NODE ?= npm
AETHER_HOME ?= $(CURDIR)/.aether-dev
export AETHER_HOME
export PATH := $(HOME)/.local/go/bin:$(PATH)
export GOPATH := $(HOME)/.local/gopath
export GOMODCACHE := $(HOME)/.local/gomodcache
export GOTOOLCHAIN := local

.PHONY: test vet build frontend quality tidy

tidy:
	$(GO) mod tidy

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

build: tidy
	mkdir -p bin
	$(GO) build -o bin/aetherd ./cmd/aetherd
	$(GO) build -o bin/aether ./cmd/aether

frontend:
	cd desktop/frontend && $(NODE) install && $(NODE) test && $(NODE) run build

quality: vet test build frontend
