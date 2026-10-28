GO      ?= go
NPM     ?= npm
BINARY  := dist/bServer-linux-amd64
WEB     := web
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo 0.1.0)

.PHONY: build build-web build-go build-all release run demo demo-down clean

build: build-web build-go

build-web:
	cd $(WEB) && $(NPM) install && $(NPM) run build

build-go:
	$(GO) build -o $(BINARY) ./cmd/bserver

# Cross-compile semua platform (pure-Go, tanpa cgo). Frontend harus sudah di-build.
build-all: build-web
	@./scripts/build-all.sh

# Bangun semua platform + kemas tarball/zip ke dist/release/.
release: build-all
	@./scripts/package.sh "$(VERSION)"

run: build
	./$(BINARY)

demo:
	cd dev && docker compose up -d

demo-down:
	cd dev && docker compose down

clean:
	rm -f dist/bServer-*
	rm -rf dist/release
	rm -rf $(WEB)/dist data
