# Distribution Packaging

Steps to publish vif through a distribution repository, and the
conventions a change must not break while that work is in progress. Section 1
applies to every commit, §2 is what the release workflow publishes, §3 to §6 are
checklists to chip away at, and §7 verifies a staged install.

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
- **Honour the environment.** `DESTDIR`, `PREFIX`, `SYSCONFDIR`. No
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
  GitHub's archives honour too), and a checkout build reads Go's VCS stamp, so
  `vif -version` names the tag without an ldflags contract. A Go module proxy
  zip carries neither, so `go install …/cmd/vif@vX.Y.Z` names the version and no
  commit, and a build of an unpacked zip reports `(devel)`.
- **Runtime state is XDG.** Logs, journals and music recordings under
  `$XDG_STATE_HOME`; nothing in `/var`, nothing beside the binary.
- **Licence stays declarable.** BSD-3-Clause, one `LICENSE` at the root. Any
  vendored or redistributed content (typing corpus, images, sound specs) must
  be licence-checked before it enters `wad/` or a release archive, and its
  notice travels with it: the corpus in `wad/content/` is Go standard-library
  source under the Go Authors' BSD licence, `wad/content/LICENSE`, so the wad
  archive and every install carry it (the content loader reads only `.txt` and
  `.toml`); and a Linux or FreeBSD binary links purego (Apache-2.0) for its
  desktop window, so `deploy/package/LICENSE.purego` travels in those archives and
  `make install` puts it beside `LICENSE`; a `vif_nogui` build links none.

## 2. Release surface

`.github/workflows/release.yml` builds one set of archives: Linux amd64 and arm64
clients and the amd64 headless server, FreeBSD amd64 client, an experimental
Windows amd64 zip, the browser bundle, the wad, the Android APK, and
`SHA256SUMS` over all of them. A build that fails `go vet ./...` or `go test ./...`
publishes nothing. Each binary and browser archive unpacks into one directory
named for it, and every tarball's entries are root's, so extracting one as root
neither scatters files nor hands them to the builder's uid; a client archive
carries `LICENSE.purego` for the desktop window its binary links. Each archive's
`README.md` is written for its platform: `make release-readme` renders
`deploy/package/release-readme.md`, one source whose blocks are tagged by target.

- **Nightly** (daily or on dispatch) is never a release. It replaces the assets
  of the `nightly` prerelease, titled and described as a development build,
  moves the `nightly` tag, and pushes the `vif_headless` image to GHCR as
  `nightly` and `sha-<commit>`, keeping only that image version.
- **A `vX.Y.Z-suffix` tag** (`v0.1.0-rc.1`) drafts a GitHub pre-release whose
  notes open by saying it is not a final release.
- **A plain `vX.Y.Z` tag** drafts the final release. None has been pushed.
- **The Android APK** (`vif-<label>-android.apk`; Android 11 and newer, arm64 and
  x86_64) is built by `android/toolchain.sh gradlew assembleRelease` on the
  runner's SDK, its `versionCode` the workflow's run number, which climbs across
  nightlies and tags, so a device takes each build as an update of the one before.
  It is signed with the repository's debug key, `android/debug.keystore`, which
  `make android` shares, unless the `ANDROID_KEYSTORE_BASE64`,
  `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD`
  secrets hold a release key ([todo](todo.md#sign-the-android-build-with-a-release-key)).
  A store listing is not on this path.

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
config root, the licence, `man6/vif.6`, a `Terminal=true` desktop entry whose
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
   Confirm the licence lands in `/usr/share/licenses/vif/`.
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
2. Debian prefers vendored dependencies or packaged Go modules. The four
   `lixenwraith/*` modules are not in Debian, so vendoring is the realistic
   route; commit a `vendor/` only on the packaging branch.
3. `debian/copyright` in DEP-5 form enumerates the corpus files in
   `wad/content/` separately: they are Go standard-library source under the Go
   Authors' BSD licence.
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
