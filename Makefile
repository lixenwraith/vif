BINARY := vif
SRC := ./cmd/vif
BIN_DIR := bin
WEB_DIR := web
GOFLAGS := -trimpath
LDFLAGS := -s -w
TAGS ?=
PORT ?= 8080
CONTAINER_ENGINE ?= docker
IMAGE ?= vi-fighter
IMAGE_TAG ?= dev
IMAGE_REVISION_DEFAULT != git rev-parse HEAD 2>/dev/null || echo unknown
IMAGE_REVISION ?= $(IMAGE_REVISION_DEFAULT)
IMAGE_VERSION ?= $(IMAGE_TAG)
VIF_CONFIG_BASE != test -n "$(XDG_CONFIG_HOME)" && echo "$(XDG_CONFIG_HOME)" || echo "$(HOME)/.config"
VIF_CONFIG_DIR ?= $(VIF_CONFIG_BASE)/vi-fighter
VIF_CONFIG_FORCE ?= 0
WAD_DIR := wad
WAD_ARCHIVE ?= $(BIN_DIR)/vi-fighter-wad.tar.gz
KEYMAP_SRC := internal/asset/input/keymap.toml
DESTDIR ?=
PREFIX ?= /usr
SYSCONFDIR ?= /etc
# FreeBSD packages each Go release under its own name: go.mod's 1.27 is go127.
GO_DEFAULT != uname -s | grep -q FreeBSD && sed -n 's/^go \([0-9]*\)\.\([0-9]*\).*/go\1\2/p' go.mod 2>/dev/null | grep . || echo go
GO ?= $(GO_DEFAULT)

.DEFAULT_GOAL := help

.PHONY: help generate dev release headless nolog wasm windows run test verify arch-check clean check-go tools allocator serve install install-config install-config-force wad-archive image image-check

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  dev      Build with race detector and debug symbols"
	@echo "  release  Build optimized binary (stripped, trimmed)"
	@echo "  headless Build dedicated-host binary without presentation or audio"
	@echo "  nolog    Release build without logger"
	@echo "  wasm     Build WebAssembly binary for xterm.js (audio and logging omitted)"
	@echo "  windows  Cross-compile for Windows/amd64 (audio and logging omitted)"
	@echo "  tools    Build all auxiliary tools and cmds (includes vif-log, the log/journal viewer)"
	@echo "  allocator Build the website-to-K3s session allocator"
	@echo "  serve    Build wasm and http-server, then serve web/ directory (use PORT=8080 to change)"
	@echo "  run      Build (dev) and run the game"
	@echo "  install  Stage binary, wad and docs under DESTDIR/PREFIX for a distro package"
	@echo "  install-config Install the wad and default keymap under $(VIF_CONFIG_DIR)"
	@echo "  install-config-force Replace files previously installed there"
	@echo "  wad-archive Pack the wad as the config root a player extracts ($(WAD_ARCHIVE))"
	@echo "  image    Build the dedicated-session container image (scratch, static, non-root)"
	@echo "  image-check Run the image's own config validation as its numeric user"
	@echo "  verify   Run tests, vet, and multi-arch compilation checks"
	@echo "  arch-check Verify pkg/ packages do not import internal/ (non-blocking)"
	@echo "  clean    Remove build artifacts"

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

$(WEB_DIR):
	mkdir -p $(WEB_DIR)

check-go:
	@if ! command -v $(GO) >/dev/null 2>&1; then \
		echo "Go compiler ($(GO)) not found."; \
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
			echo "Install Go 1.27.1+ manually:"; \
			echo "  1. Download: https://go.dev/dl/"; \
			echo "  2. Extract to /usr/local"; \
			echo "  3. Add /usr/local/go/bin to PATH"; \
			exit 1; \
		fi; \
	fi

generate: check-go
	$(GO) generate ./internal/manifest/...

dev: generate $(BIN_DIR)
	$(GO) build -race -tags "$(TAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

serve: wasm $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/http-server ./tool/http-server
	./$(BIN_DIR)/http-server -dir $(WEB_DIR) -port $(PORT)

release: generate $(BIN_DIR)
	$(GO) build $(GOFLAGS) -tags "$(TAGS)" -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

# headless is a build profile, not merely -serve at runtime: renderer and audio
# constructors are absent from the dependency graph.
headless: generate $(BIN_DIR)
	$(GO) build $(GOFLAGS) -tags "vif_headless $(TAGS)" -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-headless $(SRC)

# nolog strips logging; internal/vlog is not linked
nolog: generate $(BIN_DIR)
	$(GO) build $(GOFLAGS) -tags "novlog $(TAGS)" -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

# wasm selects vlog/stub.go and the audio-free system manifest automatically.
wasm: generate $(WEB_DIR)
	GOOS=js GOARCH=wasm $(GO) build $(GOFLAGS) -tags "vif_noaudio $(TAGS)" -ldflags="$(LDFLAGS)" -o $(WEB_DIR)/$(BINARY).wasm $(SRC)

# windows is experimental and untested
windows: generate $(BIN_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -tags "novlog vif_noaudio $(TAGS)" -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY).exe $(SRC)

tools: $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/ ./cmd/ascimage ./cmd/soundlab ./tool/...

allocator: $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/vif-allocator ./tool/vif-allocator

test: generate
	$(GO) test -race ./...

# verify covers the build-tag matrix a single-target build would miss
verify: generate test
	$(GO) build ./...
	$(GO) build -tags novlog ./...
	$(GO) build -tags vif_noaudio $(SRC)
	$(GO) build -tags vif_headless $(SRC)
	$(GO) test -tags vif_noaudio ./internal/... $(SRC)
	$(GO) test -tags vif_headless ./internal/manifest ./internal/system $(SRC)
	GOOS=js GOARCH=wasm $(GO) build $(SRC)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -tags "novlog vif_noaudio" $(SRC)
	@if $(GO) list -deps -tags vif_headless $(SRC) | grep -Eq '/internal/render($$|/)|/pkg/audio$$'; then \
		echo "FAIL: vif_headless imports presentation or audio implementation"; exit 1; \
	fi
	@if GOOS=js GOARCH=wasm $(GO) list -deps $(SRC) | grep -Eq '/pkg/audio$$'; then \
		echo "FAIL: wasm imports the audio implementation"; exit 1; \
	fi
	$(GO) vet ./...

run: dev
	./$(BIN_DIR)/$(BINARY)

# The wad is the whole external payload and mirrors the installed layout, so a
# copy is the install. Edits are never replaced by default; override
# VIF_CONFIG_DIR for staging and use the force target only deliberately.
install-config:
	@set -eu; \
	root='$(VIF_CONFIG_DIR)'; force='$(VIF_CONFIG_FORCE)'; \
	copy_file() { \
		if [ -e "$$2" ] && [ "$$force" != 1 ]; then \
			echo "keep    $$2"; \
		else \
			install -D -m 0644 "$$1" "$$2"; \
			echo "install $$2"; \
		fi; \
	}; \
	for src in $$(find $(WAD_DIR) -type f); do copy_file "$$src" "$$root/$${src#$(WAD_DIR)/}"; done; \
	copy_file $(KEYMAP_SRC) "$$root/input/keymap.toml"; \
	install -d -m 0755 "$$root/audio"

install-config-force:
	@$(MAKE) --no-print-directory install-config VIF_CONFIG_FORCE=1

# wad-archive is that same install as one file, so what a player extracts over a
# config root is what install-config would have written there. The release
# publishes it because a binary alone resolves only the embedded scenario.
wad-archive: $(BIN_DIR)
	@set -eu; \
	stage=$$(mktemp -d); \
	trap 'rm -rf "$$stage"' EXIT; \
	$(MAKE) --no-print-directory install-config \
		VIF_CONFIG_DIR="$$stage" VIF_CONFIG_FORCE=1 >/dev/null; \
	install -m 0644 LICENSE "$$stage/LICENSE"; \
	tar -C "$$stage" -czf $(WAD_ARCHIVE) .; \
	echo "packed $(WAD_ARCHIVE)"

# install stages a distro package: the binary, the wad as a system config root
# ($(SYSCONFDIR)/xdg is the XDG_CONFIG_DIRS default the resolver already
# searches), the licence, and the documentation. Build first; nothing here
# compiles, so a packager controls the build flags.
install:
	install -D -m 0755 $(BIN_DIR)/$(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)
	@set -eu; \
	root='$(DESTDIR)$(SYSCONFDIR)/xdg/vi-fighter'; \
	for src in $$(find $(WAD_DIR) -type f); do \
		install -D -m 0644 "$$src" "$$root/$${src#$(WAD_DIR)/}"; \
	done; \
	install -D -m 0644 $(KEYMAP_SRC) "$$root/input/keymap.toml"
	install -D -m 0644 LICENSE $(DESTDIR)$(PREFIX)/share/licenses/vi-fighter/LICENSE
	@set -eu; \
	for src in README.md doc/*.md; do \
		install -D -m 0644 "$$src" "$(DESTDIR)$(PREFIX)/share/doc/vi-fighter/$${src#doc/}"; \
	done

# image builds the deployment artifact from the repository root, which is the
# context deploy/docker/Dockerfile expects. The revision is stamped as an OCI
# label; the Go toolchain stamps the same commit into the binary's build info.
image:
	$(CONTAINER_ENGINE) build \
		-f deploy/docker/Dockerfile \
		--build-arg VERSION=$(IMAGE_VERSION) \
		--build-arg REVISION=$(IMAGE_REVISION) \
		-t $(IMAGE):$(IMAGE_TAG) .
	@echo "built $(IMAGE):$(IMAGE_TAG) at revision $(IMAGE_REVISION)"

# image-check runs what the workload's init container runs, against the image that
# would be deployed and as the user it would run as. A scratch image has no shell,
# so this is also the only way to prove the binary starts at all.
image-check:
	$(CONTAINER_ENGINE) run --rm --read-only --user 65532:65532 \
		--network none --cap-drop ALL --security-opt no-new-privileges \
		$(IMAGE):$(IMAGE_TAG) -check -d

# arch-check covers architectural boundaries, isolated from standard build blockers.
# The list of packages is snapshot dynamically at execution to avoid build delays across other targets.
arch-check:
	@pkgs="$(ARCH_LEAF_PKGS)"; pkgs="$${pkgs:-$$($(GO) list ./pkg/... 2>/dev/null | tr '\n' ' ')}"; \
	if [ -z "$$pkgs" ]; then echo "arch-check: no packages found in pkg/"; exit 0; fi; \
	bad=$$($(GO) list -deps $$pkgs | grep 'vi-fighter/internal' || true); \
	if [ -n "$$bad" ]; then echo "FAIL: leaf package(s) import internal:"; echo "$$bad"; exit 1; fi; \
	echo "arch-check: $$pkgs clean"

clean:
	rm -rf $(BIN_DIR)
