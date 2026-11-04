BINARY := stapes
SRC := ./cmd/stapes
BIN_DIR := bin
# Not GOFLAGS/LDFLAGS: the go command and the C linker read those themselves.
GO_BUILDFLAGS ?= -trimpath
GO_LDFLAGS ?= -s -w
# The root the binary reads is Go's os.UserConfigDir plus stapes, so this mirrors it.
STAPES_ROOT ?= $(or $(XDG_CONFIG_HOME),$(HOME)/.config)/stapes
DESTDIR ?=
# Unstaged, a user who cannot write /usr/local/bin installs for itself, under
# ~/.local. sudo make install is system-wide.
USER_INSTALL != [ -z '$(DESTDIR)' ] && [ "$$(id -u)" != 0 ] && [ ! -w /usr/local/bin ] && echo yes || echo no
PREFIX_DEFAULT != [ '$(USER_INSTALL)' = yes ] && echo "$$HOME/.local" || echo /usr/local
PREFIX ?= $(PREFIX_DEFAULT)
# FreeBSD packages each Go release under its own name: go.mod's 1.27 is go127.
GO_DEFAULT != uname -s | grep -q FreeBSD && sed -n 's/^go \([0-9]*\)\.\([0-9]*\).*/go\1\2/p' go.mod 2>/dev/null | grep . || echo go
GO ?= $(GO_DEFAULT)
# The trainer's Python: venv makes it with PYTHON3, and smoke runs PYTHON, which
# can name another environment's interpreter.
VENV := tool/rip-train/.venv
PYTHON3 ?= python3
PYTHON ?= $(CURDIR)/$(VENV)/bin/python3
# A CPU PyTorch, installed first, or requirements.txt pulls PyPI's CUDA build;
# tool/rip-train/README.md names the GPU builds.
TORCH_INDEX ?= https://download.pytorch.org/whl/cpu

.DEFAULT_GOAL := help
# Goals run in the order given, so make release install installs what it built.
.NOTPARALLEL:

.PHONY: help check-go dev release install uninstall test verify venv smoke clean

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  dev       Build $(BIN_DIR)/$(BINARY) with the race detector and debug symbols"
	@echo "  release   Build $(BIN_DIR)/$(BINARY) optimized (stripped, trimmed, cgo off)"
	@echo "  install   Install the binary, licence and docs under $(PREFIX); build first, as in"
	@echo "            make release install. A user who cannot write /usr/local/bin gets ~/.local"
	@echo "  uninstall Remove what install put under $(PREFIX), keeping the root $(STAPES_ROOT)"
	@echo "  test      Run the Go tests with the race detector"
	@echo "  verify    Run test, vet, gofmt and the FreeBSD and Android builds"
	@echo "  venv      Make the trainer's Python in $(VENV), with a CPU PyTorch"
	@echo "  smoke     Run the training path end to end at smoke scale in a temporary folder,"
	@echo "            its own root included, with PYTHON ($(VENV) by default)"
	@echo "  clean     Remove build artifacts"

check-go:
	@if ! command -v $(GO) >/dev/null 2>&1; then \
		echo "Go compiler ($(GO)) not found."; \
		if [ -n "$${SUDO_USER:-}" ]; then \
			echo "sudo may reset PATH: make release as $$SUDO_USER, then sudo make install"; exit 1; \
		fi; \
		CMD=""; \
		if [ -f /etc/arch-release ]; then \
			CMD="sudo pacman -S go"; \
		elif [ -f /etc/debian_version ]; then \
			if command -v snap >/dev/null 2>&1; then \
				CMD="sudo snap install go --classic"; \
			fi; \
		elif [ "$$(uname)" = "FreeBSD" ]; then \
			CMD="sudo pkg install $$(sed -n 's/^go \([0-9]*\)\.\([0-9]*\).*/go\1\2/p' go.mod 2>/dev/null | grep . || echo go)"; \
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
			echo "Install Go $$(sed -n 's/^go //p' go.mod)+ manually:"; \
			echo "  1. Download: https://go.dev/dl/"; \
			echo "  2. Extract to /usr/local"; \
			echo "  3. Add /usr/local/go/bin to PATH"; \
			exit 1; \
		fi; \
	fi

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

dev: check-go $(BIN_DIR)
	$(GO) build -race -o $(BIN_DIR)/$(BINARY) $(SRC)

release: check-go $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build $(GO_BUILDFLAGS) -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

# put creates only missing directories, so an install changes no existing mode.
# BSD install makes missing parents under the umask, and a private one carried
# through sudo would hide them from every other user.
INSTALL_FUNCS = umask 022; put() { [ -d "$${3%/*}" ] || install -d -m 0755 "$${3%/*}"; install -m "$$1" "$$2" "$$3"; echo "install $$3"; }

# install puts the binary, the licence and the documentation under PREFIX once it
# can write there. Nothing here compiles, so a packager controls the build flags;
# the root is the user's, made by the first run that writes to it.
install:
	@set -eu; \
	[ -x $(BIN_DIR)/$(BINARY) ] || { echo "$(BIN_DIR)/$(BINARY) is not built: make release first, as in make release install" >&2; exit 1; }; \
	d='$(DESTDIR)$(PREFIX)'; while [ ! -e "$$d" ]; do d=$$(dirname "$$d"); done; \
	[ -w "$$d" ] || { echo "no write access to $$d: sudo make install, or set PREFIX" >&2; exit 1; }; \
	if [ '$(USER_INSTALL)' = yes ] && [ '$(PREFIX)' = '$(PREFIX_DEFAULT)' ]; then \
		echo "/usr/local/bin is not writable: installing for $$(id -un) under $(PREFIX) (sudo make install: system-wide)"; fi; \
	$(INSTALL_FUNCS); dest='$(DESTDIR)$(PREFIX)'; \
	put 0755 $(BIN_DIR)/$(BINARY) "$$dest/bin/$(BINARY)"; \
	put 0644 LICENSE "$$dest/share/licenses/$(BINARY)/LICENSE"; \
	for src in README.md doc/*.md; do put 0644 "$$src" "$$dest/share/doc/$(BINARY)/$$src"; done; \
	[ -n '$(DESTDIR)' ] || case ":$$PATH:" in *':$(PREFIX)/bin:'*) ;; *) echo "note:   $(PREFIX)/bin is not on PATH" ;; esac

# uninstall removes what install put under PREFIX. It keeps the root: it holds
# the user's settings, rip parameters and model.
uninstall:
	@set -eu; dest='$(DESTDIR)$(PREFIX)'; \
	if [ '$(USER_INSTALL)' = yes ] && [ '$(PREFIX)' = '$(PREFIX_DEFAULT)' ]; then \
		echo "/usr/local/bin is not writable: uninstalling under $(PREFIX) (sudo make uninstall: system-wide)"; fi; \
	for f in bin/$(BINARY) share/doc/$(BINARY) share/licenses/$(BINARY); do \
		if [ -e "$$dest/$$f" ]; then rm -rf "$$dest/$$f"; echo "remove  $$dest/$$f"; fi; \
	done; \
	[ ! -d '$(STAPES_ROOT)' ] || echo "keep    $(STAPES_ROOT) (settings, rip parameters and model)"

# The suite renders and hears music; under the race detector on a busy machine
# it nears go test's ten-minute default.
test: check-go
	$(GO) test -race -timeout 20m ./...

# verify is what CI runs: the race suite, vet, gofmt by the toolchain's own
# gofmt, and the builds of the other hosts
verify: test
	$(GO) vet ./...
	@out=$$("$$($(GO) env GOROOT)/bin/gofmt" -l .); [ -z "$$out" ] || { echo "gofmt -l:"; echo "$$out"; exit 1; }
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 $(GO) build ./...
	CGO_ENABLED=0 GOOS=android GOARCH=arm64 $(GO) build ./...

# venv makes the trainer's Python: PyTorch from TORCH_INDEX, then the rest of
# requirements.txt
venv:
	$(PYTHON3) -m venv $(VENV)
	$(VENV)/bin/pip install torch --index-url $(TORCH_INDEX)
	$(VENV)/bin/pip install -r tool/rip-train/requirements.txt

# smoke is the trainer README's smoke test of the whole path, corpus, train, bench
# and install, in a folder of its own that holds the corpus, the weights and the
# root they install to, so the root stapes hears with is never touched. The
# folder is kept for a look and named at the start.
smoke: check-go
	@command -v '$(PYTHON)' >/dev/null || { echo "no Python at $(PYTHON): make venv, or name one with PYTHON=" >&2; exit 1; }
	@set -eu; dir=$$(mktemp -d "$${TMPDIR:-/tmp}/stapes-smoke.XXXXXX"); echo "== smoke in $$dir"; \
	LOOPS=200 STEPS=300 HOLDOUT=20 EVERY=100 BENCH_LOOPS=4 CORPUS="$$dir/corpus" ROOT="$$dir/root" \
		PYTHON='$(PYTHON)' tool/rip-train/pipeline.sh all

clean:
	rm -rf $(BIN_DIR)
