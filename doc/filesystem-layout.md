# External Filesystem Layout

Vi-Fighter treats external files as user-owned overrides of an executable that
remains self-contained. Native builds discover one categorized configuration
tree and write runtime output to the platform user-state tree. Browser builds
skip host discovery and use embedded assets.

## 1. Installed configuration tree

On Linux and FreeBSD, the user root is
`$XDG_CONFIG_HOME/vi-fighter` (normally `~/.config/vi-fighter`). The supported
layout is:

```text
vi-fighter/
├── game/
│   ├── game.toml
│   └── *.toml
├── input/
│   └── keymap.toml
├── audio/
│   ├── music.toml
│   └── sounds.toml
├── content/
│   ├── *.txt
│   └── *.toml
└── games/
    ├── blank/
    └── td/
```

`game/` is the discovered encounter bundle. `games/` holds named alternatives
selected explicitly with `-g`; it is packaging structure, not an additional
automatic search path. Audio files are optional.

## 2. Resolution policy

An individual resource flag (`-g`, `-f`, `-k`, `-config-music`, or
`-config-sounds`) is strict and always wins. Without one, every resource uses
the same root hierarchy:

1. `-config-dir <root>`;
2. the user configuration root;
3. each root in `$XDG_CONFIG_DIRS` (default `/etc/xdg`);
4. deprecated working-directory locations;
5. the embedded fallback.

Within each configuration root, the categorized path is checked before its
legacy flat equivalent. The whole user root is exhausted before a system root,
so an older user layout still overrides a newer system installation.

| Resource | Categorized path | Legacy path in each root | Deprecated working-directory path | Final fallback |
|---|---|---|---|---|
| FSM entry | `game/game.toml` | `game.toml` | `./game.toml`, `./config/game.toml` | embedded FSM bundle |
| Keymap | `input/keymap.toml` | `keymap.toml` | `./keymap.toml`, `./config/keymap.toml` | embedded keymap TOML |
| Music | `audio/music.toml` | `music.toml` | `./music.toml`, `./config/music.toml` | built-in patterns |
| Sounds | `audio/sounds.toml` | `sounds.toml` | `./sounds.toml`, `./config/sounds.toml` | built-in sounds |
| Content | `content/` | `data/` | `./data/` | embedded tutorial corpus |

An explicit game directory still means a bundle whose entry is directly at
`<directory>/game.toml`. An explicit content file pins delivery to that file.
Missing explicit paths are errors; absent discovered overrides are normal.

`-d` bypasses FSM and content discovery only. Keymap and audio overrides remain
local participant preferences and retain their ordinary resolution.

## 3. Installation and migration

Run:

```bash
make install-config
```

The target installs `config/main` as `game/`, the alternate scenarios under
`games/`, the repository corpus as `content/`, and the exact keymap embedded by
the binary as `input/keymap.toml`. Existing files are retained so an update does
not overwrite user edits. Use `make install-config-force` only when replacement
is intended.

Packaging and staged installs can select another root:

```bash
make install-config VIF_CONFIG_DIR=/path/to/stage/vi-fighter
```

The old flat user layout and repository-relative `game.toml`, `config/`,
`keymap.toml`, `music.toml`, `sounds.toml`, and `data/` probes remain readable
for migration, but are last-resort compatibility paths. New documentation and
installers should emit only the categorized layout.

## 4. Logs, journals, and runtime output

Native Unix builds use `$XDG_STATE_HOME` (normally `~/.local/state`) and keep
the streams separate:

| Output | Default | Override |
|---|---|---|
| Session logs, snapshots, recorder files, runtime stderr capture | `$XDG_STATE_HOME/vi-fighter/log/` | `-l=DIR` |
| Replay journals | `$XDG_STATE_HOME/vi-fighter/journal/` | `-j=DIR` |

On platforms without an XDG state root, the platform user-cache directory is
used. Only when no user location can be resolved does either stream fall back
to the deprecated `./log/` directory. `/var/log` is never assumed.

Bare `-l` and `-j` enable their streams at the defaults. Because both are Go
boolean-style flags, a directory must use the equals form.

## 5. Package ownership and WASM

`internal/paths` owns platform directory discovery and names. `internal/resource`
owns resource selection and strict explicit-path behavior. Loaders in
`internal/fsm`, `internal/input`, `internal/content`, and `internal/service`
receive already-resolved files or filesystem capabilities; they do not invent
search orders.

`internal/asset` continues to embed the FSM and content fallbacks. The default
keymap is embedded beside `internal/input`, and `pkg/audio` embeds its built-in
specifications. A `js/wasm` build performs no host-directory discovery, so it
remains playable without external files.
