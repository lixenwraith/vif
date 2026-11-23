# Distribution Packaging

Steps to publish vif through a distribution repository, and the
conventions a change must not break while that work is in progress. Section 1
applies to every commit, §2 is what the release workflow publishes, §3 to §6 are
checklists to chip away at, §7 verifies a staged install, and §8 is the Steam
depot with what its store needs done by hand.

## 1. Shared guidelines

These hold regardless of which repository is targeted first.

- **One external payload.** Everything a config root receives installs from
  `wad/`, laid out exactly as it installs, or from the embedded copy it overrides
  (the keymap, `vif.toml` and the bot graphs in `internal/asset`). Files installed
  outside a config root live in `deploy/package/`, and the manual is `doc/vif.6`.
  A new data file goes in one of those, never beside the code that reads it.
- **No working-directory discovery.** A packaged binary must behave the same
  from any `cwd`. Resolution is `-flag` → `-config-dir` → user root → checkout
  `wad/` beside a `bin/` binary → system roots → embedded, and nothing else.
  Never add a working-directory probe. A file a scenario names at run time, such
  as a wall image, is a logical name under its category in those roots and must
  be a regular file there: no absolute path and no path relative to `cwd`, even
  for a scenario this run resolved itself, since the same scenario is served to
  joiners and an author's host path means nothing on their machines.
- **Honor the environment.** `DESTDIR`, `PREFIX`, `SYSCONFDIR`. No
  target may write outside `DESTDIR`, and none may require network access.
- **The Makefile runs under GNU make and FreeBSD make.** `!=` rather than
  `$(shell)`, no conditionals, functions or order-only prerequisites, and
  recipes use only options BSD tools share: `install -d` then `install -m`, never
  `install -D`; `$(MAKE) -s`, never a GNU long option.
- **The build is reproducible and offline.** `-trimpath`, no `go generate` at
  package time (generated files are committed), no CGO. Vendoring or a module
  cache is the packager's choice, not the Makefile's. The manual `doc/vif.6` and
  the bash, zsh and fish completion in `deploy/package/` are such files: they
  render the `-h` table, `TestGeneratedFilesAreTheHelpTable` fails when a flag
  outruns them, and `VIF_WRITE_GENERATED=1` on that test rewrites them.
- **Version comes from the source, never from flags.** `git archive` expands
  `internal/asset/version.txt` to the tag and commit (`export-subst`, which
  GitHub's archives honor too), and a checkout build reads Go's VCS stamp, so
  `vif -version` names the tag without an ldflags contract. A Go module proxy
  zip carries neither, so `go install …/cmd/vif@vX.Y.Z` names the version and no
  commit, and a build of an unpacked zip reports `(devel)`.
- **Runtime state is XDG.** Logs, journals and music recordings under
  `$XDG_STATE_HOME`; nothing in `/var`, nothing beside the binary.
- **License stays declarable.** BSD-3-Clause, one `LICENSE` at the root. Any
  vendored or redistributed content (typing corpus, images, sound specs) must
  be license-checked before it enters `wad/` or a release archive, and its
  notice travels with it. The corpus in `wad/content/` is Go standard-library
  source under the Go Authors' BSD license, `wad/content/LICENSE`, which the wad
  archive and every install carry (the content loader reads only `.txt` and
  `.toml`). What a binary links travels as `NOTICES`: `make notices` writes the
  licenses of Go and of every module one build links, the host's release or the
  one `GOOS`, `GOARCH` and `TAGS` name, from the module cache or `vendor/` and
  with no network, so a new dependency brings its own and one with no license
  file stops the target; purego's (Apache-2.0, for the desktop window) is in it
  wherever the build links purego. `make release` writes it and `make install`
  stops without it; the release workflow writes each archive's for that
  archive's build, Gradle the Android library's (`NOTICES_OF=android`, which
  fetches `golang.org/x/mobile` where the cache lacks it) and the session
  image's builder the headless build's, which the image holds as `/NOTICES`
  beside `/LICENSE`. The font the window, Android and `vif.wasm` draw cells
  with is Hack, under the MIT license and Bitstream Vera's, which the terminal
  module embeds; its license is that module's `atlas/LICENSE`, below the root
  `notices` reads, so it travels beside `NOTICES` as `LICENSE.font`: `make
  release` copies it to `bin/` and `make install` stops without it, and the
  release workflow and Gradle copy it from the module they build with. The
  page's own text is lixen.com's Hack Proportional and the Android app's Hack
  Nerd Font, both under Hack's two licenses and Nerd Fonts' OFL, whose texts lie
  beside their files in `web/fonts/` and the APK's `assets/fonts/`, so the web
  archive and the APK carry them. The About overlay points to `NOTICES`.

## 2. Release surface

`.github/workflows/release.yml` builds one set of archives: Linux amd64 and arm64
clients and the amd64 headless server, FreeBSD amd64 client, an experimental
Windows amd64 zip, the browser bundle, the wad, the Android APK, and
`SHA256SUMS` over all of them. A build that fails `go vet ./...` or `go test ./...`
publishes nothing. Each binary and browser archive unpacks into one directory
named for it, and every tarball's entries are root's, so extracting one as root
neither scatters files nor hands them to the builder's uid; each carries `LICENSE`
and its build's `NOTICES` (§1), a client archive `LICENSE.font` as well, and the
browser bundle `LICENSE.font` and `fonts/LICENSE`. Each archive's `README.md` is
written for its platform: `make release-readme` renders
`deploy/package/release-readme.md`, one source whose blocks are tagged by target.

- **Nightly** (daily or on dispatch) is never a release. It replaces the assets
  of the `nightly` prerelease, titled and described as a development build,
  moves the `nightly` tag, and pushes the `vif_headless` image to GHCR as
  `nightly` and `sha-<commit>`, keeping only that image version.
- **A `vX.Y.Z-suffix` tag** (`v0.1.0-rc.1`) drafts a GitHub pre-release whose
  notes open by saying it is not a final release.
- **A plain `vX.Y.Z` tag** drafts the final release. None has been pushed.
- **The Android APKs** (`vif-<label>-android-arm64.apk` for a phone and
  `vif-<label>-android-x86_64.apk` for an emulator; Android 11 and newer) are one
  per ABI, so a phone downloads and keeps its own library alone: 18.3 MB, where
  the one APK carrying both was 37.9 MB. They are built by
  `android/toolchain.sh gradlew assembleRelease` on the runner's SDK, their
  `versionCode` the workflow's run number, which climbs across nightlies and tags,
  so a device takes each build as an update of the one before. They are signed
  with the repository's debug key, `android/debug.keystore`, which `make android`
  shares, unless the `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`,
  `ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD` secrets hold a release key
  ([todo](todo.md#sign-the-android-build-with-a-release-key)). Each holds
  `LICENSE`, `LICENSE.font`, `NOTICES` and `fonts/LICENSE` as assets. A store
  listing is not on this path.

A tag adds `vif-X.Y.Z.tar.gz`, a `git archive` of the tag and the byte-stable
source archive a distribution hashes. It pushes no image, moves nothing, and
publishes nothing until a person publishes the draft; it fails if the committed
generated files are stale, so the binaries are what the source archive builds.

`vif-*-wad.tar.gz` is `make wad-archive`, which is `install-config` into
one file: it unpacks as `vif/`, the config root, into the directory that holds
one (`~/.config`, `/usr/local/etc`, `%AppData%`), so a downloaded binary reaches
the installed scenarios and corpus rather than only the embedded scenario. The
Makefile owns its contents so a release and a source install cannot drift.

Downloads are at the repository's
[`nightly` release](https://github.com/lixenwraith/vif/releases/tag/nightly);
the image is `ghcr.io/lixenwraith/vif:nightly`, or `sha-<commit>` of that build.
Content a client fetches for itself, rather than one a person downloads and
extracts, still needs its trust, origin and cache policy decided.

## 3. Gaps to close before a first submission

`make install` stages everything a package review expects: the binary, the system
config root, the licenses (`LICENSE`, `NOTICES`, `LICENSE.font`), `man6/vif.6`, a
`Terminal=true` desktop entry whose
second action plays in the window (`vif -g`), scalable and symbolic icons, and
bash, zsh and fish completion. What remains is
[the first stable release](todo.md#promote-a-nightly-build-to-the-first-stable-release).

## 4. Arch (AUR)

Target `vif` (release) with `vif-git` optional.

1. `depends=('glibc')`: the binary links libc to load the desktop window's
   libraries at run time (a `vif_nogui` build is static). `makedepends=('go')`.
   `optdepends` for the window, `libx11` and `libglvnd` with a Mesa or vendor
   driver, and for the audio players the engine probes, in its order:
   `pulseaudio`/`pipewire-pulse` for `pacat`, `pipewire` for `pw-cat`,
   `alsa-utils` for `aplay`, `sox` for `play`, `ffmpeg` for `ffplay`.
2. `build()` uses the repository's flags plus Arch's hardening:
   `CGO_ENABLED=0 go build -trimpath -buildmode=pie -mod=readonly -modcacherw`.
   `make release GO_LDFLAGS=` does the same with Arch's `GOFLAGS` exported: plain
   `make release` sets `-s -w`, which Arch's debug packaging wants to control.
3. `package()` runs `make install DESTDIR="$pkgdir" PREFIX=/usr SYSCONFDIR=/etc`.
   Confirm the licenses land in `/usr/share/licenses/vif/`; a `build()` that
   runs `go build` itself also runs `make notices` and copies the font's license
   to `bin/LICENSE.font`, as `make release` does: `install` stops without either.
4. `check()` runs `go test ./...`. `script/test.sh` binds ports and must not
   run in a build chroot.
5. Namcap the result. Expect a warning about the size of `/etc/vif`;
   a config tree that large is intentional and is the answer to it.
6. Submit to `ssh://aur@aur.archlinux.org/vif.git` with `.SRCINFO`
   regenerated by `makepkg --printsrcinfo`.

## 5. FreeBSD ports

Target `games/vif`. FreeBSD is a first-class runtime target: the OSS
audio backend exists for it.

1. `USES=go:modules`, `GO_MODULE=github.com/lixenwraith/vif`,
   `GO_TARGET=./cmd/vif`, and
   `GO_BUILDFLAGS+=-gcflags=github.com/ebitengine/purego/internal/fakecgo=-std`,
   which `make release` passes as `GUI_GCFLAGS`: without it purego's `fakecgo`,
   which the desktop window links, stops the build (`go:cgo_export_dynamic ...
   only allowed in cgo-generated code`).
2. Ports staging sets `DESTDIR` and `PREFIX`; `SYSCONFDIR` defaults to
   `$(PREFIX)/etc`. The system root is `/usr/local/etc/vif`, which every Unix
   binary searches, so no `pkg-message` is needed.
3. Every installed file needs a `pkg-plist` entry; generate it from
   `make install DESTDIR=$(mktemp -d)` output rather than by hand.
4. `LICENSE=BSD3CLAUSE` with `LICENSE_FILE=${WRKSRC}/LICENSE`.

## 6. Debian and Ubuntu

Lowest priority: Go game packages move slowly through NEW.

1. `debian/rules` with `dh $@`; override `dh_auto_install` to call
   `make install DESTDIR=debian/vif PREFIX=/usr SYSCONFDIR=/etc`.
2. Debian prefers vendored dependencies or packaged Go modules. The
   `lixenwraith/*` modules are not in Debian, so vendoring is the realistic
   route; commit a `vendor/` only on the packaging branch.
3. `debian/copyright` in DEP-5 form enumerates the corpus files in
   `wad/content/` separately: they are Go standard-library source under the Go
   Authors' BSD license. So is the vendored cell font,
   `vendor/github.com/lixenwraith/terminal/atlas/hack.*`, under Hack's MIT and
   Bitstream Vera licenses (its `atlas/LICENSE`), and so are the page's and the
   app's font, `web/fonts/hack-proportional-*.woff2` and
   `android/app/src/main/assets/fonts/hack-400.ttf`, under those and Nerd Fonts'
   OFL (the `LICENSE` beside each).
4. Lintian may flag the size of the config tree in `/etc/vif`; the override
   goes in `debian/vif.lintian-overrides`.

## 7. Verifying a staged install

```bash
make release
make install DESTDIR=/tmp/stage PREFIX=/usr SYSCONFDIR=/etc
export XDG_CONFIG_HOME=/nonexistent VIF_SYSTEM_ROOTS=/tmp/stage/etc/vif
/tmp/stage/usr/bin/vif -check
/tmp/stage/usr/bin/vif -check -s td
```

The settings, keymap, content and installed bot-graph lines must name paths under
the staged root, and so must the scenario line of the second check. The main
scenario, the sound and music banks and the `embedded default` bot line are
embedded by design. That is the packaging contract in two commands.

## 8. Steam

Steam carries the game as a paid app at a low price. Every other build stays
free, so a purchase is support, and the store's description says so: the source
is BSD-3-Clause and free on GitHub, and buying funds its development. The depot
is the desktop window alone, since Steam starts a binary with no terminal.

`make steam-linux` assembles the Linux depot in `bin/steam/linux/`, from an empty
directory because Steam installs a depot whole:

- `bin/vif`: the `vif_gui` build for linux/amd64, which opens its window by
  default. It links `libc.so.6`, `libdl.so.2` and `libpthread.so.0` with no
  versioned glibc symbol, and loads the window's libraries and the sound
  server's client library as it runs
  ([platforms §8](multi-platform.md#8-adding-another-renderer-or-android-host)).
- `wad/`: the config root an install lays out, by the recipe of §2's wad archive.
  The binary finds it beside its `bin/` and never writes there
  ([Filesystem §3](filesystem-layout.md#3-resolution-policy)), so an update that
  replaces it costs a player nothing.
- `LICENSE`, that build's `NOTICES`, `LICENSE.font` (§1) and a `README.md`
  (`release-readme`'s `steam-linux`).

It holds nothing of Valve's: the build links no Steamworks SDK
([todo](todo.md#bind-the-steamworks-sdk-behind-a-tag-of-its-own)).

### In the runtime

A depot is checked in the container Steam runs it in, with no Steam client:
`run` in Valve's runtime archive is the launch Steam makes, the runtime's `/usr`
with the host's graphics drivers and its display and sound sockets.

```sh
make steam-linux
curl -O https://repo.steampowered.com/steamrt4/images/latest-public-stable/SteamLinuxRuntime_4.tar.xz
tar -xJf SteamLinuxRuntime_4.tar.xz
SteamLinuxRuntime_4/run -- "$PWD/bin/steam/linux/bin/vif" -check -s td
SteamLinuxRuntime_4/run -- "$PWD/bin/steam/linux/bin/vif" -fullscreen
```

The check's lines must name files under the depot's `wad/`, as §7's name the
staged root. Steam Linux Runtime 4.0 (4.0.20261021, Debian 13) holds
`libX11.so.6`, `libEGL.so.1`, `libGL.so.1`, `libOpenGL.so.0` and
`libpulse-simple.so.0`, and of the players the engine finds by name `pw-cat`
alone: no `pacat`, `aplay`, `play` or `ffplay`. It holds no `libGLESv2.so.2`,
which a launch has only where the host does
([todo](todo.md#open-the-window-where-the-host-has-no-gles-2)). So read, both
checks passed there on a machine with no GPU (Xvfb, Mesa's llvmpipe); a Steam
Deck and the Steam client's overlay are checked by hand.

### Upload

`make steam-upload STEAM_APP=<app> STEAM_DEPOT_LINUX=<depot> STEAM_USER=<account>`
fills the ids into the build scripts of `deploy/package/steam/`, writes them
beside the depot and runs `steamcmd`, which asks for the builder account's
password and Steam Guard code itself. It sets nothing live: a build goes to the
default branch on the partner site's builds page.

The release workflow's `steam` job does the same for a tag and is unplugged: it
runs once the repository variable `STEAM_UPLOAD` is `true`, with `STEAM_APP` and
`STEAM_DEPOT_LINUX` as variables and two secrets, `STEAM_USER` and
`STEAM_CONFIG_VDF`, the base64 of the `config/config.vdf` that account's first
`steamcmd +login` left. The account is a builder's own, holding Edit App Metadata
and Publish App Changes To Steam and nothing else, since the secret is its login.
The job has never run: its first run is a tag's, watched.

### On the partner site

Set by hand, and nothing in the repository reads them:

- **Installation, General.** A launch option for Linux: executable `bin/vif`,
  arguments `-fullscreen`.
- **Installation, Linux Runtime.** Steam Linux Runtime 4.0, offered once a Linux
  launch option exists; left unset, a native game runs in Scout, the first.
- **Depots.** One for Linux, 64-bit.
- **Steam Cloud**, by Auto-Cloud, which needs no SDK: the player's settings,
  keymaps and what their bots learned, each a root path under the user root
  (`os.UserConfigDir`, so `~/.config/vif`), marked cross-platform:
  - `vif.toml` in `.config/vif`;
  - `*.toml` in `.config/vif/input`;
  - `*` in `.config/vif/bot`, recursive: a player's graphs and each population
    ([Bots](bots.md#the-population-file)).

  The root is `LinuxHome`; Valve's list has none for `$XDG_CONFIG_HOME`, so a
  player who moved it syncs nothing. Logs, journals, recordings and places (the
  state root) and `scenario/downloads` stay on the machine. `vif.toml` carries the
  window's zoom (`[presentation] cell`) to a player's other machines with the
  rest.
- **Steam Input**, the official configuration for a Steam Deck and a controller,
  in the words the help uses:
  - right trackpad: the mouse; R2: left click, which fires the main weapon; L2:
    right click, the special weapon;
  - d-pad: `h`, `j`, `k`, `l`; A: Enter; B: Escape; X: Space;
  - Start: F10, the settings menu; a button for Steam's on-screen keyboard.

  Under gamescope the touchscreen is the mouse.
- **Steam Deck.** Gamescope runs the window through Xwayland, and its smallest
  cell is 12 pixels tall where Valve asks for 9 at 1280x800. The aim is
  Playable, which opening the on-screen keyboard by hand still is; Verified
  wants the default controls to reach everything, the Deck's glyphs in help and
  prompts and the SDK's text entry
  ([todo](todo.md#bind-the-steamworks-sdk-behind-a-tag-of-its-own)).

### Store checklist

- Steam Direct: 100 USD an app, returned after 1,000 USD of adjusted gross
  revenue; signing up as a person is allowed, and the tax interview can take up
  to 30 days.
- Release comes 21 days or more after the fee is paid, and after a Coming Soon
  page has been up two weeks.
- Valve reviews the store page and the build before either goes live: submit
  seven business days ahead.
- Assets: capsules, five screenshots or more, a trailer, the content survey.
- Licenses: BSD-3-Clause allows the sale, and Hack's MIT and Bitstream Vera
  licenses allow bundling the font. Valve's redistributables would ride in a
  depot alone, never in the repository or a GitHub archive.
