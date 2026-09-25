# Distribution Packaging

Steps to publish Vi-Fighter through a distribution repository, and the
conventions a change must not break while that work is in progress. Sections 2
onward are checklists to chip away at; section 1 applies to every commit now.

## 1. Shared guidelines

These hold regardless of which repository is targeted first.

- **One external payload.** Everything a user may edit lives in `wad/`, laid
  out exactly as it installs. Everything compiled in lives in `internal/asset`.
  A new data file goes in one of those two, never beside the code that reads it.
- **No working-directory discovery.** A packaged binary must behave the same
  from any `cwd`. Resolution is `-flag` → `-config-dir` → user root → XDG system
  roots → embedded, and nothing else. Never reintroduce a `./config` probe.
- **Honour the environment.** `DESTDIR`, `PREFIX`, `SYSCONFDIR`, `XDG_*`. No
  target may write outside `DESTDIR`, and none may require network access.
- **The Makefile runs under GNU make and FreeBSD make.** `!=` rather than
  `$(shell)`, no conditionals, functions or order-only prerequisites, and
  recipes use only options BSD tools share: `install -d` then `install -m`, never
  `install -D`; `$(MAKE) -s`, never a GNU long option.
- **The build is reproducible and offline.** `-trimpath`, no `go generate` at
  package time (generated files are committed), no CGO. Vendoring or a module
  cache is the packager's choice, not the Makefile's. The manual `doc/vif.6` is
  one of those files: it renders the `-h` table, `TestManualIsTheHelpTable` fails
  when a flag outruns it, and `VIF_WRITE_MANUAL=1` on that test rewrites it.
- **Version comes from VCS.** `vif -version` reports `debug.ReadBuildInfo`. Tag
  releases `vX.Y.Z` so a package's `pkgver` and the binary agree without an
  ldflags contract.
- **Runtime state is XDG.** Logs and journals under `$XDG_STATE_HOME`; nothing
  in `/var`, nothing beside the binary.
- **Licence stays declarable.** BSD-3-Clause, one `LICENSE` at the root. Any
  vendored or redistributed content (typing corpus, images, sound specs) must
  be licence-checked before it enters `wad/`.

## 2. Release surface

`.github/workflows/release.yml` builds one set of archives: Linux amd64 client and
headless server, FreeBSD amd64 client, the browser bundle, the wad, and
`SHA256SUMS`. Nightly (daily or on dispatch) it moves the `nightly` tag and
prerelease and pushes the `vif_headless` image to GHCR as `nightly` and
`sha-<commit>`. A pushed `vX.Y.Z` tag instead drafts release `vX.Y.Z` with the
same archives named for `X.Y.Z` plus `vi-fighter-X.Y.Z.tar.gz`, a `git archive`
of the tag: the byte-stable source archive a distribution hashes. A tag pushes no
image, moves nothing, and publishes nothing until a person publishes the draft;
it fails if the committed generated files are stale, so the binaries are what
the source archive builds.

`vi-fighter-*-wad.tar.gz` is `make wad-archive`, which is `install-config` into
one file: it unpacks over a config root, so a downloaded binary reaches the
installed scenarios and corpus rather than only the embedded scenario. The
Makefile owns its contents so a release and a source install cannot drift.

Downloads are at the repository's
[`nightly` release](https://github.com/lixenwraith/vi-fighter/releases/tag/nightly);
the image is `ghcr.io/lixenwraith/vi-fighter:nightly` or `sha-<commit>`. Neither
build ships the experimental Windows cross-build. Content a client fetches for
itself, rather than one a person downloads and extracts, still needs its trust,
origin and cache policy decided.

## 3. Gaps to close before a first submission

Ordered by what blocks a package review.

| # | Gap | Notes |
|---|---|---|
| 1 | No stable tagged release | Tag a verified nightly commit `v0.1.0` and publish the draft §2 creates. |
| 2 | No `.desktop` entry | Optional for a TUI game, but expected if it should appear in a menu. Needs `Terminal=true` and an icon. |
| 3 | Shell completion | Not generated. `flag` gives no completion data; a hand-written `_vif` is the cheapest route. |

## 4. Arch (AUR)

Target `vi-fighter` (release) with `vi-fighter-git` optional.

1. `depends=()` — the binary is static Go with no runtime library dependency.
   `makedepends=('go')`. `optdepends` for the audio backends the engine probes
   (`pulseaudio`/`pipewire-pulse` for `pacat`, `alsa-utils` for `aplay`).
2. `build()` uses the repository's flags plus Arch's hardening:
   `go build -trimpath -buildmode=pie -mod=readonly -modcacherw`.
   Do not call `make release` — it sets `-s -w`, which Arch's debug packaging
   wants to control.
3. `package()` runs `make install DESTDIR="$pkgdir" PREFIX=/usr SYSCONFDIR=/etc`.
   Confirm the licence lands in `/usr/share/licenses/vi-fighter/`.
4. `check()` runs `go test ./...`. `script/test.sh` binds ports and must not
   run in a build chroot.
5. Namcap the result. Expect a warning about the size of `/etc/xdg/vi-fighter`;
   a config tree that large is intentional and is the answer to it.
6. Submit to `ssh://aur@aur.archlinux.org/vi-fighter.git` with `.SRCINFO`
   regenerated by `makepkg --printsrcinfo`.

## 5. FreeBSD ports

Target `games/vi-fighter`. FreeBSD is a first-class runtime target: the OSS
audio backend exists for it.

1. `USES=go:modules`, `GO_MODULE=github.com/lixenwraith/vi-fighter`,
   `GO_TARGET=./cmd/vif`.
2. Ports staging already sets `DESTDIR`/`PREFIX`; `SYSCONFDIR` defaults to
   `${PREFIX}/etc`, so the system config root becomes
   `/usr/local/etc/xdg/vi-fighter`. Confirm that root is on `XDG_CONFIG_DIRS`
   for the target release, or set it in `pkg-message`.
3. Every installed file needs a `pkg-plist` entry; generate it from
   `make install DESTDIR=$(mktemp -d)` output rather than by hand.
4. `LICENSE=BSD3CLAUSE` with `LICENSE_FILE=${WRKSRC}/LICENSE`.

## 6. Debian and Ubuntu

Lowest priority: Go game packages move slowly through NEW.

1. `debian/rules` with `dh $@`; override `dh_auto_install` to call
   `make install DESTDIR=debian/vi-fighter PREFIX=/usr SYSCONFDIR=/etc`.
2. Debian prefers vendored dependencies or packaged Go modules. The four
   `lixenwraith/*` modules are not in Debian, so vendoring is the realistic
   route; commit a `vendor/` only on the packaging branch.
3. `debian/copyright` in DEP-5 form must enumerate the corpus files in
   `wad/content/` separately if any is third-party source text.
4. Lintian will flag a config tree in `/etc/xdg`; it is correct per XDG and the
   override goes in `debian/vi-fighter.lintian-overrides`.

## 7. Verifying a staged install

```bash
make release
make install DESTDIR=/tmp/stage PREFIX=/usr SYSCONFDIR=/etc
XDG_CONFIG_HOME=/nonexistent XDG_CONFIG_DIRS=/tmp/stage/etc/xdg \
  /tmp/stage/usr/bin/vif -check
```

Every line must report a path under the staged root, not `embedded default`.
That is the whole packaging contract in one command.
