BINARY := vif
SRC := ./cmd/vif
BIN_DIR := bin
WEB_DIR := web
GOFLAGS := -trimpath
LDFLAGS := -s -w
TAGS ?=
PORT ?= 8080
CONTAINER_ENGINE ?= docker
IMAGE ?= vif
IMAGE_TAG ?= dev
IMAGE_REVISION_DEFAULT != git rev-parse HEAD 2>/dev/null || echo unknown
IMAGE_REVISION ?= $(IMAGE_REVISION_DEFAULT)
IMAGE_VERSION ?= $(IMAGE_TAG)
# The user root the binary reads is Go's os.UserConfigDir, so this mirrors it.
VIF_CONFIG_BASE != case "$$(uname -s)" in \
	*_NT*) cygpath -u '$(APPDATA)' ;; \
	Darwin) echo "$(HOME)/Library/Application Support" ;; \
	*) test -n "$(XDG_CONFIG_HOME)" && echo "$(XDG_CONFIG_HOME)" || echo "$(HOME)/.config" ;; \
	esac
VIF_CONFIG_DIR ?= $(VIF_CONFIG_BASE)/vif
CLEAN ?= 0
WAD_DIR := wad
PKG_DIR := deploy/package
WAD_ARCHIVE ?= $(BIN_DIR)/vif-wad.tar.gz
KEYMAP_SRC := internal/asset/input/keymap.toml
SETTINGS_SRC := internal/asset/vif.toml
BOT_DIR := internal/asset/bot
DESTDIR ?=
PREFIX ?= /usr/local
# A /usr package keeps its configuration in /etc; both are system roots vif reads.
SYSCONFDIR_DEFAULT != [ '$(PREFIX)' = /usr ] && echo /etc || echo '$(PREFIX)/etc'
SYSCONFDIR ?= $(SYSCONFDIR_DEFAULT)
# FreeBSD packages each Go release under its own name: go.mod's 1.27 is go127.
GO_DEFAULT != uname -s | grep -q FreeBSD && sed -n 's/^go \([0-9]*\)\.\([0-9]*\).*/go\1\2/p' go.mod 2>/dev/null | grep . || echo go
GO ?= $(GO_DEFAULT)

.DEFAULT_GOAL := help

.PHONY: help generate dev release headless nolog wasm windows run test verify arch-check clean check-go tools allocator serve install install-config wad-archive image image-check

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
	@echo "  install  Install binary, wad, manual, launcher, completion and docs under $(PREFIX),"
	@echo "           or under ~/.local and $(VIF_CONFIG_DIR) without write access there"
	@echo "  install-config Install the wad, default keymap, bot graphs and vif.toml under $(VIF_CONFIG_DIR)"
	@echo "           (both: CLEAN=1 first removes what an earlier install put there)"
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
			echo "Install Go 1.27.2+ manually:"; \
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
	$(GO) build -tags vif_noaudio ./...
	$(GO) build -tags vif_headless ./...
	$(GO) test -tags vif_noaudio ./internal/... $(SRC)
	$(GO) test -tags vif_headless ./internal/manifest ./internal/system $(SRC)
	GOOS=js GOARCH=wasm $(GO) build ./...
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -tags "novlog vif_noaudio" ./...
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
# copy is the install. Shipped files are replaced and files a player added are
# kept; vif.toml and the keymap are the player's settings, written only when
# absent. CLEAN=1 first removes every category the install ships, and those two.
install-config:
	@set -eu; \
	root='$(VIF_CONFIG_DIR)'; \
	case "$$root" in ''|/) echo "refusing to install into '$$root'" >&2; exit 1 ;; esac; \
	if [ '$(CLEAN)' = 1 ]; then \
		for dir in $(WAD_DIR)/*/ bot/; do dir=$${dir%/}; rm -rf "$$root/$${dir##*/}"; echo "remove  $$root/$${dir##*/}"; done; \
		rm -f "$$root/input/keymap.toml" "$$root/vif.toml"; \
	fi; \
	put() { install -d -m 0755 "$${2%/*}"; install -m 0644 "$$1" "$$2"; echo "install $$2"; }; \
	keep() { if [ -e "$$2" ]; then echo "keep    $$2"; else put "$$1" "$$2"; fi; }; \
	for src in $$(find $(WAD_DIR) -type f); do put "$$src" "$$root/$${src#$(WAD_DIR)/}"; done; \
	for src in $(BOT_DIR)/*.toml; do put "$$src" "$$root/bot/$${src##*/}"; done; \
	keep $(KEYMAP_SRC) "$$root/input/keymap.toml"; \
	keep $(SETTINGS_SRC) "$$root/vif.toml"; \
	install -d -m 0755 "$$root/audio"

# wad-archive is that same install as one file, so what a player extracts over a
# config root is what install-config would have written there. The release
# publishes it because a binary alone resolves only the embedded scenario.
wad-archive: $(BIN_DIR)
	@set -eu; \
	stage=$$(mktemp -d); \
	trap 'rm -rf "$$stage"' EXIT; \
	$(MAKE) -s install-config VIF_CONFIG_DIR="$$stage" >/dev/null; \
	install -m 0644 LICENSE "$$stage/LICENSE"; \
	tar -C "$$stage" -czf $(WAD_ARCHIVE) .; \
	echo "packed $(WAD_ARCHIVE)"

# install puts the binary, the wad as the system config root $(SYSCONFDIR)/vif,
# the licence, the manual, the desktop entry and icons, shell completion and the
# documentation under PREFIX. Unstaged and without write access there, it installs
# for this user under ~/.local and the user config root instead. Build first;
# nothing here compiles, so a packager controls the build flags.
install:
	@set -eu; \
	[ -x $(BIN_DIR)/$(BINARY) ] || { echo "$(BIN_DIR)/$(BINARY) is not built; run make release" >&2; exit 1; }; \
	prefix='$(PREFIX)'; root='$(SYSCONFDIR)/vif'; \
	writable() { d=$$1; while [ ! -e "$$d" ]; do d=$$(dirname "$$d"); done; [ -w "$$d" ]; }; \
	if [ -z '$(DESTDIR)' ] && ! { writable "$$prefix/bin" && writable "$$prefix/share" && writable "$$root"; }; then \
		prefix='$(HOME)/.local'; root='$(VIF_CONFIG_DIR)'; \
		echo "no write access under $(PREFIX) or $(SYSCONFDIR); installing for $$(id -un) under $$prefix and $$root"; \
	fi; \
	$(MAKE) -s install-config VIF_CONFIG_DIR='$(DESTDIR)'"$$root" CLEAN='$(CLEAN)'; \
	dest='$(DESTDIR)'"$$prefix"; \
	put() { install -d -m 0755 "$${3%/*}"; install -m "$$1" "$$2" "$$3"; echo "install $$3"; }; \
	put 0755 $(BIN_DIR)/$(BINARY) "$$dest/bin/$(BINARY)"; \
	put 0644 LICENSE "$$dest/share/licenses/vif/LICENSE"; \
	put 0644 doc/vif.6 "$$dest/share/man/man6/vif.6"; \
	put 0644 $(PKG_DIR)/vif.desktop "$$dest/share/applications/vif.desktop"; \
	put 0644 $(PKG_DIR)/vif.svg "$$dest/share/icons/hicolor/scalable/apps/vif.svg"; \
	put 0644 $(PKG_DIR)/vif-symbolic.svg "$$dest/share/icons/hicolor/symbolic/apps/vif-symbolic.svg"; \
	put 0644 $(PKG_DIR)/vif.bash "$$dest/share/bash-completion/completions/vif"; \
	put 0644 $(PKG_DIR)/_vif "$$dest/share/zsh/site-functions/_vif"; \
	put 0644 $(PKG_DIR)/vif.fish "$$dest/share/fish/vendor_completions.d/vif.fish"; \
	for src in README.md doc/*.md; do \
		put 0644 "$$src" "$$dest/share/doc/vif/$${src#doc/}"; \
	done; \
	[ -n '$(DESTDIR)' ] || case ":$$PATH:" in *":$$prefix/bin:"*) ;; *) echo "note: $$prefix/bin is not on PATH" ;; esac

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
	bad=$$($(GO) list -deps $$pkgs | grep 'vif/internal' || true); \
	if [ -n "$$bad" ]; then echo "FAIL: leaf package(s) import internal:"; echo "$$bad"; exit 1; fi; \
	echo "arch-check: $$pkgs clean"

clean:
	rm -rf $(BIN_DIR)
