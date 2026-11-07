BINARY := ascimage
SRC := ./cmd/ascimage
BIN_DIR := bin
# Not GOFLAGS/LDFLAGS: the go command and the C linker read those themselves.
GO_BUILDFLAGS ?= -trimpath
GO_LDFLAGS ?= -s -w
DESTDIR ?=
# Unstaged, a user who cannot write /usr/local/bin installs for itself, under
# ~/.local. sudo make install is system-wide.
USER_INSTALL != [ -z '$(DESTDIR)' ] && [ "$$(id -u)" != 0 ] && [ ! -w /usr/local/bin ] && echo yes || echo no
PREFIX_DEFAULT != [ '$(USER_INSTALL)' = yes ] && echo "$$HOME/.local" || echo /usr/local
PREFIX ?= $(PREFIX_DEFAULT)
# FreeBSD packages each Go release under its own name: go.mod's 1.27 is go127.
GO_DEFAULT != uname -s | grep -q FreeBSD && sed -n 's/^go \([0-9]*\)\.\([0-9]*\).*/go\1\2/p' go.mod 2>/dev/null | grep . || echo go
GO ?= $(GO_DEFAULT)
FUZZTIME ?= 60s

.DEFAULT_GOAL := help
# Goals run in the order given, so make release install installs what it built.
.NOTPARALLEL:

.PHONY: help check-go dev release install uninstall test verify fuzz clean

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  dev       Build $(BIN_DIR)/$(BINARY) with the race detector and debug symbols"
	@echo "  release   Build $(BIN_DIR)/$(BINARY) optimized (stripped, trimmed, cgo off)"
	@echo "  install   Install the binary, licence and docs under $(PREFIX); build first, as in"
	@echo "            make release install. A user who cannot write /usr/local/bin gets ~/.local"
	@echo "  uninstall Remove what install put under $(PREFIX)"
	@echo "  test      Run the Go tests with the race detector"
	@echo "  verify    Run test, vet, gofmt, the .aimg reader's fuzzing and the other hosts' builds"
	@echo "  fuzz      Fuzz the .aimg reader for FUZZTIME ($(FUZZTIME))"
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
# can write there. Nothing here compiles, so a packager controls the build flags.
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

uninstall:
	@set -eu; dest='$(DESTDIR)$(PREFIX)'; \
	if [ '$(USER_INSTALL)' = yes ] && [ '$(PREFIX)' = '$(PREFIX_DEFAULT)' ]; then \
		echo "/usr/local/bin is not writable: uninstalling under $(PREFIX) (sudo make uninstall: system-wide)"; fi; \
	for f in bin/$(BINARY) share/doc/$(BINARY) share/licenses/$(BINARY); do \
		if [ -e "$$dest/$$f" ]; then rm -rf "$$dest/$$f"; echo "remove  $$dest/$$f"; fi; \
	done

test: check-go
	$(GO) test -race ./...

fuzz: check-go
	$(GO) test -run '^$$' -fuzz FuzzReadDualMode -fuzztime $(FUZZTIME) .

# verify is what CI runs: the race suite, vet, gofmt by the toolchain's own
# gofmt, the reader's fuzzing for a while, and the builds of the other hosts
verify: test
	$(GO) vet ./...
	@out=$$("$$($(GO) env GOROOT)/bin/gofmt" -l .); [ -z "$$out" ] || { echo "gofmt -l:"; echo "$$out"; exit 1; }
	$(MAKE) --no-print-directory fuzz FUZZTIME=30s
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 $(GO) build ./...
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build ./...
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build ./...

clean:
	rm -rf $(BIN_DIR)
