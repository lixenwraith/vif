BINARY := vif
SRC := ./cmd/vif
BIN_DIR := bin
WEB_DIR := web
GOFLAGS := -trimpath
LDFLAGS := -s -w
TAGS ?=
PORT ?= 8080

.DEFAULT_GOAL := help

.PHONY: help generate dev release nolog wasm windows run test verify arch-check clean check-go tools serve

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  dev      Build with race detector and debug symbols"
	@echo "  release  Build optimized binary (stripped, trimmed)"
	@echo "  nolog    Release build without logger"
	@echo "  wasm     Build WebAssembly binary for xterm.js (sound and logging disabled)"
	@echo "  windows  Cross-compile for Windows (amd64, requires Windows Terminal, sound/log disabled)"
	@echo "  tools    Build all auxiliary tools and cmds (includes vif-log, the log/journal viewer)"
	@echo "  serve    Build wasm and http-server, then serve web/ directory (use PORT=8080 to change)"
	@echo "  run      Build (dev) and run the game"
	@echo "  verify   Run tests, vet, and multi-arch compilation checks"
	@echo "  arch-check Verify pkg/ packages do not import internal/ (non-blocking)"
	@echo "  clean    Remove build artifacts"

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

$(WEB_DIR):
	mkdir -p $(WEB_DIR)

check-go:
	@if ! command -v go >/dev/null 2>&1; then \
		echo "Go compiler not found."; \
		CMD=""; \
		if [ -f /etc/arch-release ]; then \
			CMD="sudo pacman -S go"; \
		elif [ -f /etc/debian_version ]; then \
			if command -v snap >/dev/null 2>&1; then \
				CMD="sudo snap install go --classic"; \
			fi; \
		elif [ "$$(uname)" = "FreeBSD" ]; then \
			CMD="sudo pkg install lang/go"; \
		fi; \
		if [ -n "$$CMD" ]; then \
			echo "Proposed installation: $$CMD"; \
			printf "Install now? [y/N] "; \
			read yn; \
			case "$$yn" in \
				[Yy]*) $$CMD ;; \
				*) echo "Aborted. Install Go manually to continue."; exit 1 ;; \
			esac; \
		else \
			echo "Automatic installation unavailable (or apt packages outdated)."; \
			echo "Install Go 1.26+ manually:"; \
			echo "  1. Download: https://go.dev/dl/"; \
			echo "  2. Extract to /usr/local"; \
			echo "  3. Add /usr/local/go/bin to PATH"; \
			exit 1; \
		fi; \
	fi

generate: check-go
	go generate ./internal/manifest/...

dev: generate | $(BIN_DIR)
	go build -race -tags "$(TAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

serve: wasm | $(BIN_DIR)
	go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/http-server ./tool/http-server
	./$(BIN_DIR)/http-server -dir $(WEB_DIR) -port $(PORT)

release: generate | $(BIN_DIR)
	go build $(GOFLAGS) -tags "$(TAGS)" -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

# nolog strips logging; internal/vlog is not linked
nolog: generate | $(BIN_DIR)
	go build $(GOFLAGS) -tags "novlog $(TAGS)" -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

# wasm selects vlog/stub.go automatically
wasm: generate | $(WEB_DIR)
	GOOS=js GOARCH=wasm go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(WEB_DIR)/$(BINARY).wasm $(SRC)

# windows is experimental and untested
windows: generate | $(BIN_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY).exe $(SRC)

tools: | $(BIN_DIR)
	go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/ ./cmd/ascimage ./cmd/soundlab ./tool/...

test: generate
	go test -race ./...

# verify covers the build-tag matrix a single-target build would miss
verify: generate test
	go build ./...
	go build -tags novlog ./...
	GOOS=js GOARCH=wasm go build ./...
	go vet ./...

run: dev
	./$(BIN_DIR)/$(BINARY)

# arch-check covers architectural boundaries, isolated from standard build blockers.
# The list of packages is snapshot dynamically at execution to avoid build delays across other targets.
arch-check:
	@pkgs="$(if $(ARCH_LEAF_PKGS),$(ARCH_LEAF_PKGS),$$(go list ./pkg/... 2>/dev/null | tr '\n' ' '))"; \
	if [ -z "$$pkgs" ]; then echo "arch-check: no packages found in pkg/"; exit 0; fi; \
	bad=$$(go list -deps $$pkgs | grep 'vi-fighter/internal' || true); \
	if [ -n "$$bad" ]; then echo "FAIL: leaf package(s) import internal:"; echo "$$bad"; exit 1; fi; \
	echo "arch-check: $$pkgs clean"

clean:
	rm -rf $(BIN_DIR)
