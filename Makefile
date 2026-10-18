BINARY := vif
SRC := ./cmd/vif
BIN_DIR := bin
WEB_DIR := web
# Not GOFLAGS/LDFLAGS: the go command and the C linker read those themselves.
GO_BUILDFLAGS ?= -trimpath
GO_LDFLAGS ?= -s -w
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
# Root extracting an archive restores its owners, so its entries are root's rather
# than the packer's uid. GNU tar and bsdtar name that differently.
TAR_ROOT_OWNER != tar --version 2>/dev/null | grep -q GNU && echo '--owner=0 --group=0 --numeric-owner' || echo '--uid 0 --gid 0'
KEYMAP_SRC := internal/asset/input/keymap.toml
SETTINGS_SRC := internal/asset/vif.toml
BOT_DIR := internal/asset/bot
DESTDIR ?=
# Unstaged, a user who cannot write /usr/local/bin installs for itself: under
# ~/.local, with the user root as the config root. sudo make install is system-wide.
USER_INSTALL != [ -z '$(DESTDIR)' ] && [ "$$(id -u)" != 0 ] && [ ! -w /usr/local/bin ] && echo yes || echo no
PREFIX_DEFAULT != [ '$(USER_INSTALL)' = yes ] && echo "$$HOME/.local" || echo /usr/local
PREFIX ?= $(PREFIX_DEFAULT)
# A /usr package keeps its configuration in /etc; both are system roots vif reads.
SYSCONFDIR_DEFAULT != if [ '$(USER_INSTALL)' = yes ] && [ '$(PREFIX)' = '$(PREFIX_DEFAULT)' ]; then echo '$(VIF_CONFIG_BASE)'; \
	elif [ '$(PREFIX)' = /usr ]; then echo /etc; else echo '$(PREFIX)/etc'; fi
SYSCONFDIR ?= $(SYSCONFDIR_DEFAULT)
# FreeBSD packages each Go release under its own name: go.mod's 1.27 is go127.
GO_DEFAULT != uname -s | grep -q FreeBSD && sed -n 's/^go \([0-9]*\)\.\([0-9]*\).*/go\1\2/p' go.mod 2>/dev/null | grep . || echo go
GO ?= $(GO_DEFAULT)
# purego's FreeBSD port builds its fakecgo as std to link without cgo
GUI_GCFLAGS != [ "$$($(GO) env GOOS 2>/dev/null)" = freebsd ] && echo '-gcflags=github.com/ebitengine/purego/internal/fakecgo=-std' || true

.DEFAULT_GOAL := help
# Goals run in the order given, so make release install installs what it built.
# Generated files are committed, so only dev, test and verify regenerate: a
# packager's build never rewrites the source it was given (doc/packaging.md §1).
.NOTPARALLEL:

.PHONY: help generate dev release headless gui android android-install android-toolchain nolog wasm windows run test verify arch-check clean check-go tools allocator serve install uninstall install-config wad-archive image image-check

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  dev      Build with race detector and debug symbols"
	@echo "  release  Build optimized binary (stripped, trimmed)"
	@echo "  headless Build dedicated-host binary without presentation or audio"
	@echo "  gui      Build bin/vif-gui, the game in a desktop window (Xlib, EGL, GLES 2; Linux, FreeBSD)"
	@echo "  android  Build the Android debug APK as bin/vif-debug.apk; first names any JDK or SDK piece missing"
	@echo "  android-install Install bin/vif-debug.apk on the device adb sees"
	@echo "  android-toolchain Install what android needs: Go and a JDK (asking first), the SDK's"
	@echo "           command-line tools, platform, NDK and platform-tools (android/README.md)"
	@echo "  nolog    Release build without logger"
	@echo "  wasm     Build WebAssembly binary for the browser page in web/ (logging omitted)"
	@echo "  windows  Cross-compile for Windows/amd64 (audio and logging omitted)"
	@echo "  tools    Build all auxiliary tools and cmds (includes vif-log, the log/journal viewer)"
	@echo "  allocator Build the website-to-K3s session allocator"
	@echo "  serve    Build wasm and http-server, then serve web/ directory (use PORT=8080 to change)"
	@echo "  run      Build (dev) and run the game"
	@echo "  test     Run the Go tests with the race detector"
	@echo "  install  Install binary, manual, launcher, completion and docs under $(PREFIX) and the"
	@echo "           wad as $(SYSCONFDIR)/vif; build first, as in make release install. A user who"
	@echo "           cannot write /usr/local/bin gets ~/.local and the user root"
	@echo "  uninstall Remove what install put under $(PREFIX), keeping $(SYSCONFDIR)/vif"
	@echo "  install-config Install the wad, default keymap, bot graphs and vif.toml under $(VIF_CONFIG_DIR)"
	@echo "           (both: CLEAN=1 first removes what an earlier install put there)"
	@echo "  wad-archive Pack the wad as vif/, the config root a player extracts ($(WAD_ARCHIVE))"
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

generate: check-go
	$(GO) generate ./internal/manifest/...

dev: generate $(BIN_DIR)
	$(GO) build -race -tags "$(TAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

serve: wasm $(BIN_DIR)
	$(GO) build $(GO_BUILDFLAGS) -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/http-server ./tool/http-server
	./$(BIN_DIR)/http-server -dir $(WEB_DIR) -port $(PORT)

release: check-go $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build $(GO_BUILDFLAGS) -tags "$(TAGS)" -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

# headless is a build profile, not merely -serve at runtime: renderer and audio
# constructors are absent from the dependency graph.
headless: check-go $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build $(GO_BUILDFLAGS) -tags "vif_headless $(TAGS)" -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-headless $(SRC)

# gui loads Xlib, EGL and GLES 2 at run time through purego, so cgo stays off
gui: check-go $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build $(GO_BUILDFLAGS) $(GUI_GCFLAGS) -tags "vif_gui $(TAGS)" -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-gui $(SRC)

# android is its own Go module and Gradle project: gomobile binds the Go host and
# the APK packages wad/ as its assets. toolchain.sh checks the JDK and SDK before
# Gradle runs; only android-toolchain downloads them (android/README.md).
android: check-go $(BIN_DIR)
	android/toolchain.sh gradlew assembleDebug
	install -m 0644 android/app/build/outputs/apk/debug/app-debug.apk $(BIN_DIR)/$(BINARY)-debug.apk

android-install:
	android/toolchain.sh adb install -r $(BIN_DIR)/$(BINARY)-debug.apk

android-toolchain: check-go
	@android/toolchain.sh install

# nolog strips logging: internal/vlog builds as a no-op stub and lixenwraith/log is not linked
nolog: check-go $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build $(GO_BUILDFLAGS) -tags "novlog $(TAGS)" -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(SRC)

# wasm selects vlog/stub.go automatically; the mixer streams to the page's speakers.
# wasm_exec.js is the loader of the Go that built the binary, so it is copied
# from that toolchain on every build rather than kept as it was committed.
wasm: check-go $(WEB_DIR)
	GOOS=js GOARCH=wasm $(GO) build $(GO_BUILDFLAGS) -tags "$(TAGS)" -ldflags="$(GO_LDFLAGS)" -o $(WEB_DIR)/$(BINARY).wasm $(SRC)
	install -m 0644 "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" $(WEB_DIR)/

# windows is experimental and untested
windows: check-go $(BIN_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(GO_BUILDFLAGS) -tags "novlog vif_noaudio $(TAGS)" -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/$(BINARY).exe $(SRC)

tools: check-go $(BIN_DIR)
	$(GO) build $(GO_BUILDFLAGS) -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/ ./cmd/ascimage ./cmd/soundlab ./tool/...

allocator: check-go $(BIN_DIR)
	$(GO) build $(GO_BUILDFLAGS) -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/vif-allocator ./tool/vif-allocator

test: generate
	$(GO) test -race ./...

# verify covers the build-tag matrix a single-target build would miss
verify: generate test
	$(GO) build ./...
	$(GO) build -tags novlog ./...
	$(GO) build -tags vif_noaudio ./...
	$(GO) build -tags vif_headless ./...
	CGO_ENABLED=0 $(GO) build $(GUI_GCFLAGS) -tags vif_gui ./...
	$(GO) test -tags vif_noaudio ./internal/... $(SRC)
	$(GO) test -tags vif_headless ./internal/manifest ./internal/system $(SRC)
	cd android && $(GO) vet ./... && $(GO) test ./...
	CGO_ENABLED=0 $(GO) test $(GUI_GCFLAGS) -tags vif_gui ./pkg/window
	GOOS=js GOARCH=wasm $(GO) build ./...
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -tags "novlog vif_noaudio" ./...
	@if $(GO) list -deps -tags vif_headless $(SRC) | grep -Eq '/internal/render($$|/)|/pkg/audio$$'; then \
		echo "FAIL: vif_headless imports presentation or audio implementation"; exit 1; \
	fi
	$(GO) vet ./...
	CGO_ENABLED=0 $(GO) vet $(GUI_GCFLAGS) -tags vif_gui ./pkg/window $(SRC)

run: dev
	./$(BIN_DIR)/$(BINARY)

# put creates only missing directories, so an install changes no existing mode.
# BSD install makes missing parents under the umask, and a private one carried
# through sudo would hide a system root from every other user.
INSTALL_FUNCS = umask 022; put() { [ -d "$${3%/*}" ] || install -d -m 0755 "$${3%/*}"; install -m "$$1" "$$2" "$$3"; echo "install $$3"; }; \
	keep() { if [ -e "$$3" ]; then echo "keep    $$3"; else put "$$@"; fi; }
# The wad is the whole external payload and mirrors the installed layout, so a
# copy is the install: shipped files are replaced, files a player added kept,
# and vif.toml and the keymap, the player's settings, written only when absent.
# CLEAN=1 first removes every shipped category and those two. A variable, not
# a sub-make, which make -n would run: install and wad-archive share it.
INSTALL_ROOT = case "$$root" in ''|/) echo "refusing to install into '$$root'" >&2; exit 1 ;; esac; \
	if [ '$(CLEAN)' = 1 ]; then \
		for dir in $(WAD_DIR)/*/ bot/; do dir=$${dir%/}; rm -rf "$$root/$${dir\#\#*/}"; echo "remove  $$root/$${dir\#\#*/}"; done; \
		rm -f "$$root/input/keymap.toml" "$$root/vif.toml"; \
	fi; \
	for src in $$(find $(WAD_DIR) -type f); do put 0644 "$$src" "$$root/$${src\#$(WAD_DIR)/}"; done; \
	for src in $(BOT_DIR)/*.toml; do put 0644 "$$src" "$$root/bot/$${src\#\#*/}"; done; \
	keep 0644 $(KEYMAP_SRC) "$$root/input/keymap.toml"; \
	keep 0644 $(SETTINGS_SRC) "$$root/vif.toml"; \
	[ -d "$$root/audio" ] || install -d -m 0755 "$$root/audio"

install-config:
	@set -eu; $(INSTALL_FUNCS); root='$(VIF_CONFIG_DIR)'; $(INSTALL_ROOT)

# wad-archive is that same install as one file, unpacking as vif/ into the
# directory that holds a config root: ~/.config, /usr/local/etc or %AppData%. The
# release publishes it because a binary alone resolves only the embedded scenario.
wad-archive: $(BIN_DIR)
	@set -eu; $(INSTALL_FUNCS); \
	stage=$$(mktemp -d); \
	trap 'rm -rf "$$stage"' EXIT; \
	root="$$stage/vif"; install -d -m 0755 "$$root"; \
	{ $(INSTALL_ROOT); } >/dev/null; \
	install -m 0644 LICENSE "$$root/LICENSE"; \
	tar -C "$$stage" $(TAR_ROOT_OWNER) -czf $(WAD_ARCHIVE) vif; \
	echo "packed $(WAD_ARCHIVE)"

# install puts the binary, the wad as the config root $(SYSCONFDIR)/vif, the
# licence, the manual, the desktop entry and icons, shell completion and the
# documentation under PREFIX, once it can write all three places. Nothing here
# compiles, so a packager controls the build flags.
install:
	@set -eu; \
	[ -x $(BIN_DIR)/$(BINARY) ] || { echo "$(BIN_DIR)/$(BINARY) is not built: make release first, as in make release install" >&2; exit 1; }; \
	for d in '$(PREFIX)/bin' '$(PREFIX)/share' '$(SYSCONFDIR)/vif'; do d='$(DESTDIR)'"$$d"; \
		while [ ! -e "$$d" ]; do d=$$(dirname "$$d"); done; \
		[ -w "$$d" ] || { echo "no write access to $$d: sudo make install, or set PREFIX and SYSCONFDIR" >&2; exit 1; }; \
	done; \
	if [ '$(USER_INSTALL)' = yes ] && [ '$(PREFIX)' = '$(PREFIX_DEFAULT)' ]; then \
		echo "/usr/local/bin is not writable: installing for $$(id -un) under $(PREFIX) and $(SYSCONFDIR)/vif (sudo make install: system-wide)"; fi; \
	$(INSTALL_FUNCS); root='$(DESTDIR)$(SYSCONFDIR)/vif'; $(INSTALL_ROOT); \
	dest='$(DESTDIR)$(PREFIX)'; \
	put 0755 $(BIN_DIR)/$(BINARY) "$$dest/bin/$(BINARY)"; \
	put 0644 LICENSE "$$dest/share/licenses/vif/LICENSE"; \
	put 0644 doc/vif.6 "$$dest/share/man/man6/vif.6"; \
	put 0644 $(PKG_DIR)/vif.desktop "$$dest/share/applications/vif.desktop"; \
	put 0644 $(PKG_DIR)/vif.svg "$$dest/share/icons/hicolor/scalable/apps/vif.svg"; \
	put 0644 $(PKG_DIR)/vif-symbolic.svg "$$dest/share/icons/hicolor/symbolic/apps/vif-symbolic.svg"; \
	put 0644 $(PKG_DIR)/vif.bash "$$dest/share/bash-completion/completions/vif"; \
	put 0644 $(PKG_DIR)/_vif "$$dest/share/zsh/site-functions/_vif"; \
	put 0644 $(PKG_DIR)/vif.fish "$$dest/share/fish/vendor_completions.d/vif.fish"; \
	for src in README.md doc/*.md; do put 0644 "$$src" "$$dest/share/doc/vif/$$src"; done; \
	[ -n '$(DESTDIR)' ] || case ":$$PATH:" in *':$(PREFIX)/bin:'*) ;; *) echo "note:   $(PREFIX)/bin is not on PATH" ;; esac

# uninstall removes what install put under PREFIX. It keeps the config root:
# a user install's holds the player's settings.
uninstall:
	@set -eu; dest='$(DESTDIR)$(PREFIX)'; \
	if [ '$(USER_INSTALL)' = yes ] && [ '$(PREFIX)' = '$(PREFIX_DEFAULT)' ]; then \
		echo "/usr/local/bin is not writable: uninstalling under $(PREFIX) (sudo make uninstall: system-wide)"; fi; \
	for f in bin/$(BINARY) share/man/man6/vif.6 share/applications/vif.desktop \
		share/icons/hicolor/scalable/apps/vif.svg share/icons/hicolor/symbolic/apps/vif-symbolic.svg \
		share/bash-completion/completions/vif share/zsh/site-functions/_vif \
		share/fish/vendor_completions.d/vif.fish share/doc/vif share/licenses/vif; do \
		if [ -e "$$dest/$$f" ]; then rm -rf "$$dest/$$f"; echo "remove  $$dest/$$f"; fi; \
	done; \
	root='$(DESTDIR)$(SYSCONFDIR)/vif'; [ ! -d "$$root" ] || echo "keep    $$root (the wad and settings)"

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
	rm -rf $(BIN_DIR) android/build android/app/build
