# External Filesystem Layout

Vi-Fighter treats external files as user-owned overrides of an executable that
remains self-contained. Native builds discover one categorized configuration
tree and write runtime output to the platform user-state tree. Browser builds
skip host discovery and use embedded assets.

## 1. Two payloads in the repository

| Payload | Location | Reaches the user as |
|---|---|---|
| External | `wad/` | Files installed into a configuration root |
| Embedded | `internal/asset/` | Bytes compiled into the binary |

`wad/` mirrors the installed tree exactly, so installing it is a copy and
`vif -config-dir wad` plays the repository checkout without installing
anything. The embedded keymap has no `wad/` copy: it is installed from
`internal/asset/input/keymap.toml`, so the file the binary falls back to and the
file a user edits are one source.

```text
wad/                          internal/asset/
├── game/       named games   ├── config/    fallback FSM bundle
│   ├── main/   default       ├── content/   fallback corpus
│   ├── blank/  scaffold      ├── input/     default keymap
│   └── td/     tower defence ├── audio/     built-in sound bank
├── content/    typing corpus └── splash_font.go
└── image/      .vifimg assets
```

The external `main` game and embedded fallback are intentionally separate. The
external game is an editable, extended scenario; the embedded bundle is the
self-contained fallback required by native and browser binaries. They need not
contain the same optional regions.

## 2. Installed configuration tree

On Linux and FreeBSD the user root is `$XDG_CONFIG_HOME/vi-fighter` (normally
`~/.config/vi-fighter`); a distribution package installs the same tree under
`/etc/xdg/vi-fighter`, which is the `XDG_CONFIG_DIRS` default.

```text
vi-fighter/
├── game/        named bundles, each rooted at game.toml
│   ├── main/    discovered default
│   ├── blank/   authoring scaffold
│   └── td/      tower-defence scenario
├── input/       keymap.toml
├── audio/       music.toml, sounds.toml (optional overrides)
├── content/     .txt and .toml typing corpus
└── image/       .vifimg wall assets
```

`game/main/` is the automatically discovered encounter bundle. The other
directories under `game/` are selected by name (`-g td`) or explicit path.
`audio/` is empty until a user or `soundlab` writes an override. `image/` is the
discovery category for `.vifimg` assets named by a `WallPatternSpawnRequest`.

## 3. Resolution policy

An individual resource flag (`-g`, `-f`, `-k`, `-config-music`, or
`-config-sounds`) is strict and always wins. `-g` first accepts an existing
`game.toml` path or bundle directory; a single name such as `td` then resolves
as `game/td/game.toml` through the roots below. Without an override, every
resource walks the same roots in order:

1. `-config-dir <root>`;
2. the user configuration root;
3. each root in `$XDG_CONFIG_DIRS` (default `/etc/xdg`).

The first root holding the categorized path wins, so an older user layout still
overrides a newer system installation. A resource absent from every root uses
the embedded fallback.

| Resource | Path in each root | Final fallback |
|---|---|---|
| FSM entry | `game/main/game.toml` | embedded FSM bundle |
| Keymap | `input/keymap.toml` | embedded keymap |
| Music | `audio/music.toml` | built-in patterns |
| Sounds | `audio/sounds.toml` | built-in sound bank |
| Content | `content/` | embedded tutorial corpus |
| Wall image | `image/<name>.vifimg` | none; failure is reported in the game status |

An explicit game directory means a bundle whose entry is directly at
`<directory>/game.toml`. A named game is searched under `game/<name>/` in root
priority order. An explicit content file pins delivery to that file. For a wall
image, an existing absolute or relative path is explicit; otherwise the event's
path is a logical name below each root's `image/` directory, and it may include
nested directories but not `..`. New configurations should use a logical name or
an absolute path; the relative-path check remains for compatibility. Missing
explicit paths or names are errors; absent discovered overrides are normal.

`-d` bypasses FSM and content discovery only. Keymap and audio overrides remain
local participant preferences and retain their ordinary resolution.

## 4. Installation

```bash
make install-config                                  # into the user root
make install-config VIF_CONFIG_DIR=/path/to/stage    # into a staging root
make install DESTDIR=/pkg PREFIX=/usr SYSCONFDIR=/etc # distribution package
```

`install-config` copies `wad/` plus the embedded keymap and retains existing
files, so an update never overwrites user edits; `install-config-force` replaces
them. `install` stages a package: binary, `wad/` as a system configuration root,
licence, and documentation. See [Packaging](packaging.md).

## 5. Logs, journals, and runtime output

Native Unix builds use `$XDG_STATE_HOME` (normally `~/.local/state`) and keep
the streams separate:

| Output | Default | Override |
|---|---|---|
| Session logs, snapshots, recorder files, runtime stderr capture | `$XDG_STATE_HOME/vi-fighter/log/` | `-l=DIR` |
| Replay journals | `$XDG_STATE_HOME/vi-fighter/journal/` | `-j=DIR` |

On platforms without an XDG state root, the platform user-cache directory is
used. Only when no user location can be resolved does either stream fall back to
`./log/`. `/var/log` is never assumed.

Bare `-l` and `-j` enable their streams at the defaults. Because both are Go
boolean-style flags, a directory must use the equals form.

## 6. Package ownership and WASM

`internal/paths` owns platform directory discovery and names. `internal/resource`
owns composition-time resource selection. Loaders in `internal/fsm`,
`internal/input`, `internal/content`, and `internal/service` receive
already-resolved files or filesystem capabilities; they do not invent search
orders. Runtime categorized opens, including strict explicit-path handling,
cross the generic `FileService` capability contributed to the world; systems do
not open host paths directly.

`internal/asset` owns every embedded group and is the only package with an
`embed` directive for shipped data. `pkg/audio` carries no specs of its own:
`internal/parameter.BuiltinSounds` parses the embedded bank and hands it to the
engine as `AudioConfig.BaseSounds`, which is what keeps `pkg/` free of
`internal/` imports. A `js/wasm` build performs no host-directory discovery, so
it remains playable without external files.
