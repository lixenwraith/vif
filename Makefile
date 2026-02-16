BINARY := vi-fighter
SRC := ./cmd/vi-fighter
BIN_DIR := bin
GOFLAGS := -trimpath
LDFLAGS := -s -w

.DEFAULT_GOAL := help

.PHONY: help generate dev release wasm run clean check-go

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  dev      Build with race detector and debug symbols"
	@echo "  release  Build optimized binary (stripped, trimmed)"
	@echo "  wasm     Build WebAssembly binary for xterm.js"
	@echo "  run      Build (dev) and run the game"
	@echo "  clean    Remove build artifacts"

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

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
			echo "Install Go 1.25+ manually:"; \
			echo "  1. Download: https://go.dev/dl/"; \
			echo "  2. Extract to /usr/local"; \
			echo "  3. Add /usr/local/go/bin to PATH"; \
			exit 1; \
		fi; \
	fi

generate: check-go
	go generate ./manifest/...

dev: generate | $(BIN_DIR)
	go build -race -o $(BIN_DIR)/$(BINARY) $(SRC)

release: generate | $(BIN_DIR)
	go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

wasm: generate | $(BIN_DIR)
	GOOS=js GOARCH=wasm go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY).wasm $(SRC)

run: dev
	./$(BIN_DIR)/$(BINARY)

clean:
	rm -rf $(BIN_DIR)