# CHANGED: Full Makefile replacement
BINARY := vi-fighter
SRC := ./cmd/vi-fighter
BIN_DIR := bin

# Default to help if no target is specified
.DEFAULT_GOAL := help

.PHONY: help generate dev release wasm run clean

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

generate:
	go generate ./manifest/...

dev: generate | $(BIN_DIR)
	go build -race -o $(BIN_DIR)/$(BINARY) $(SRC)

release: generate | $(BIN_DIR)
	go build -ldflags="-s -w" -trimpath -o $(BIN_DIR)/$(BINARY) $(SRC)

wasm: generate | $(BIN_DIR)
	GOOS=js GOARCH=wasm go build -o $(BIN_DIR)/$(BINARY).wasm $(SRC)

run: dev
	./$(BIN_DIR)/$(BINARY)

clean:
	rm -rf $(BIN_DIR)