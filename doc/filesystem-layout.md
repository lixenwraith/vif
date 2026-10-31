# External Filesystem Layout

vif treats external files as user-owned overrides of an executable that
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
anything. The embedded keymap, bot graphs and `vif.toml` have no `wad/` copy: they
are installed from `internal/asset/input/keymap.toml`, `internal/asset/bot/` and
`internal/asset/vif.toml`, so the file the binary falls back to and the file a user
edits are one source.

```text
wad/                            internal/asset/
├── scenario/   one directory    ├── scenario/  fallback scenario
│               per scenario     ├── content/   fallback corpus
├── content/    typing corpus    ├── input/     default keymap
└── image/      .vifimg assets   ├── audio/     sound and music banks
                                 ├── bot/       shipped bot graphs
                                 ├── font/      cell font atlas
                                 ├── vif.toml   default settings
                                 ├── version.txt source-archive stamp
                                 └── splash_font.go
```

The main campaign lives only in `internal/asset/scenario/`. Native, headless,
and browser builds embed that same tree; `-s main` falls back to it when no
external override exists. To author a variant, copy that directory into a
configuration root as `scenario/<name>/`. A `scenario/main/` in any root takes
precedence, a copy an earlier release installed included.

## 2. Installed configuration tree

The user root is Go's `os.UserConfigDir` plus `vif`:
`~/.config/vif` on Linux and FreeBSD (`$XDG_CONFIG_HOME/vif` when that is set),
`%AppData%\vif` on Windows, `~/Library/Application Support/vif` on macOS.
`make install-config` writes that same root. `make install` writes the system
root `$(SYSCONFDIR)/vif`: `/usr/local/etc/vif` for a source install, `/etc/vif`
for a distribution package. System roots exist on Linux, the BSDs and the other
Unix-like targets `internal/paths` lists, not on macOS or Windows.

```text
vif/
├── vif.toml     settings: flag defaults (§3)
├── scenario/    named scenarios, each rooted at scenario.toml
│   ├── main/    optional default override
│   └── vanguard/behavior/  behavior programs its scenario.toml lists
├── input/       keymap.toml
├── audio/       music.toml, sounds.toml (optional overrides)
├── content/     .txt and .toml typing corpus
├── image/       .vifimg wall assets
└── bot/         bot graphs, one <name>.toml each
```

An optional `scenario/main/` override is the automatically discovered scenario. The other directories
under `scenario/` are selected by name (`-s td`) or explicit path. A scenario's
region, macro and behavior program files sit beside its `scenario.toml` or below
it; `behavior/` is where the shipped `vanguard` keeps its raiders' programs
([FSM reference](fsm-reference.md#behavior-programs)).
`audio/` is empty until a user or `soundlab` writes an override. `image/` is the
discovery category for `.vifimg` assets named by a `WallPatternSpawnRequest`.

## 3. Resolution policy

An individual resource flag (`-s`, `-f`, `-k`, `-config-music`, or
`-config-sounds`) is strict and always wins. `-s` first accepts an existing
`scenario.toml` path or scenario directory; a single name such as `td` then
resolves as `scenario/td/scenario.toml` through the roots below. Without an override, every
resource walks the same roots in order:

1. `-config-dir <root>`;
2. the user configuration root;
3. the checkout's `wad/`, when the binary sits in that checkout's `bin/`, so
   `bin/vif -s td` plays the repository uninstalled;
4. the system roots `/usr/local/etc/vif` then `/etc/vif`; `VIF_SYSTEM_ROOTS`
   names others in their place, separated as `PATH` is, and set empty names none.

The first root holding the categorized path wins, so an older user layout still
overrides a newer system installation, a copy in the user root shadows the
checkout's, and the checkout's shadows a system install; `-config-dir wad` puts
the checkout first. A resource absent from every root uses the embedded fallback.

| Resource | Path in each root | Final fallback |
|---|---|---|
| Scenario | `scenario/main/scenario.toml` | embedded scenario |
| Keymap | `input/keymap.toml` | embedded keymap |
| Music | `audio/music.toml` | built-in patterns |
| Sounds | `audio/sounds.toml` | built-in sound bank |
| Content | `content/` | embedded tutorial corpus |
| Wall image | `image/<name>.vifimg` | none; failure is reported in the game status |
| Bot graph | `bot/<name>.toml`, or the path `-bot` names | the embedded graph of that name |

An explicit scenario directory means one whose entry is directly at
`<directory>/scenario.toml`. A named scenario is searched under
`scenario/<name>/` in root priority order. An explicit content file pins delivery to that file. For a wall
image, the event's path is a logical name below each root's `image/` directory,
opened only as a regular file there; it may include nested directories but not
`..`, and a host path, absolute or relative, is never opened, since the scenario
naming it may have come from a peer. Missing explicit paths or names other than
the built-in `main` are errors; absent discovered overrides are normal.

`-d` bypasses scenario and content discovery only, and refuses `-s` and `-f`.
Keymap, audio and bot-graph overrides remain local participant preferences and
retain their ordinary resolution.

A joiner that does not resolve the session's scenario to the same digest is served
the coordinator's during the join and holds it in memory only; nothing is written to
a root ([Services §7.2](services-and-networking.md#72-join-identity-and-who-decides-it)).

A fleet node carries a partial root. `/var/db/vif/wad` holds `scenario/` and
`image/` and nothing else, mounted read-only at `/wad` in every session pod, so a
session resolves its scenario and wall images from the node and everything else
from the binary.
Content is not a category there: a dedicated host spawns no glyphs, each guest
reads its own corpus, and a guest without the session's scenario is served it at
the join.

`vif.toml` at the top of a root holds what a flag would otherwise say on every run:
`[paths]` names another root (`root`, read as `-config-dir` is), the log,
journal and music directories, and the scenario, content and keymap `-s`, `-f` and `-k`
take; `[audio] buffer_ms` sets the mixer period; `[presentation] window = true` opens
the desktop window by default, as `-g` does, and `maximized` or `fullscreen` the
state it opens in, and `cell` is the zoom Android and a browser open at; `[play]` is
how a run starts and what the configuration menu changes in it: the audio channels
(`sfx`, `music`), the sfx volume (`sfx_volume`), auto-fire, mouse input, pointer
following, the telemetry HUD, relaying for others, each new game waiting for
its player's press, and the most frames drawn a second (`max_fps`). It is found like any resource, from `-config-dir` down to the
system roots; the first file wins whole, a key it omits keeps the embedded
default, and the `vif.toml` inside its own `root` is not read. Every key is a default: a flag given on the command line wins, and `-d`
still takes the embedded scenario and content. A relative path is relative to the
file (a bare scenario name stays a name the roots resolve), `~/` is the home
directory. A key this build does not know is dropped: the run reads every key it
knows, removes each line setting another and each table another heads from the
document it writes back to, with the comments that are theirs (a comment group
belongs to the side fewer blank lines part it from, below on a tie), every other
line and comment kept, and names them on its status line, so a build older than
the file drops the newer keys too. `-check` names them and writes nothing, and
names the file it read. Music and sound overrides stay out of it, because a run
that creates no audio refuses them and a default must not break `-serve`.

One rule holds on every host: a flag sets its value for its run alone, `vif.toml`
holds the value every run starts from, and the menu writes back what the player
changes. A run writes each `[play]` value, and the zoom as `[presentation] cell`,
back as the player changes it, through a menu row or the command behind it, so the
next starts the same: into the `vif.toml` it read when that is the player's own, in
the user root or the `-config-dir` root, else into the user root's, created at the
first change as a copy of the one it read (relative paths made absolute) that
shadows it from then on; dropping unknown keys creates it at once. A checkout's or
a system root's file is never written.
Only a value changed in the run is written, so one a flag set (`-mute`'s channels,
`-nw`'s wait) stays out unless the player then changes it. No flag saves the
others: a flag is what an alias, a script or a link repeats, and writing it back
would let the last command line decide every later run. A browser keeps them in
the site's storage ([Multi-platform §7](multi-platform.md#7-external-maps-and-assets)).

## 4. Installation

```bash
make release install                                 # build, then /usr/local, else ~/.local and the user root
make install-config                                  # the user root only
make install-config VIF_CONFIG_DIR=/path/to/stage    # into a staging root
make install DESTDIR=/pkg PREFIX=/usr                # distribution package, root /etc/vif
make uninstall                                       # what install put under PREFIX
```

Both copy `wad/`, the bot graphs, the keymap and `vif.toml`. A reinstall replaces
every shipped file and keeps files a player added; the keymap and `vif.toml` are
the player's settings and are written only when absent. `CLEAN=1` first removes
the shipped categories (`scenario/`, `content/`, `image/`, `bot/`) and those two
files, so the root holds only the fresh install plus `audio/` and anything else
a player put beside them. `install` adds the binary, licence, manual, desktop
entry and icons, completion and documentation under `PREFIX` with the wad at
`$(SYSCONFDIR)/vif`.
A user who cannot write `/usr/local/bin` gets, unstaged, `PREFIX=~/.local` and the
user root as the defaults; `sudo make install` installs system-wide, and `make
help` prints the values in effect. `install` writes nothing until it can write
all of `PREFIX` and the root, and leaves the mode of existing directories alone.
`uninstall` keeps the root, which in a user install holds the player's settings.
See [Packaging](packaging.md).

## 5. Logs, journals, and runtime output

Linux, the BSDs and the other Unix-like targets use `$XDG_STATE_HOME` when it is
an absolute path, else `~/.local/state`, for every install, and keep the streams
separate:

| Output | Default | Override |
|---|---|---|
| Session logs, snapshots, recorder files, runtime stderr capture, profiles and traces | `$XDG_STATE_HOME/vif/log/` | `-l=DIR`, `paths.log` |
| Replay journals | `$XDG_STATE_HOME/vif/journal/` | `-j=DIR`, `paths.journal` |
| Music recordings | `$XDG_STATE_HOME/vif/music/` | `-mw=DIR`, `paths.music` |

On macOS and Windows the platform user-cache directory is the base. Only when no
user location can be resolved does a stream fall back to `./vif/<stream>/` in the
working directory. `/var/log` is never assumed.

Bare `-l`, `-j` and `-mw` enable their streams at the defaults. Because they are
Go boolean-style flags, a directory must use the equals form.

## 6. Package ownership and WASM

`internal/paths` owns platform directory discovery, names and `vif.toml`. `internal/resource`
owns composition-time resource selection. Loaders in `internal/fsm`,
`internal/input`, `internal/content`, and `internal/service` receive
already-resolved files or filesystem capabilities; they do not invent search
orders. Runtime categorized opens cross the generic `FileService` capability
contributed to the world, which serves regular files under a category in the
roots and nothing else; systems do not open host paths directly.

`internal/asset` owns every embedded group and is the only package with an
`embed` directive for shipped data. In an audio-capable build, `pkg/audio`
carries no specs or patterns of its own: `internal/parameter.BuiltinSounds` and
`BuiltinPatterns` parse the embedded banks and the audio service hands them to the
engine as `AudioConfig.BaseSounds` and `BasePatterns`, which is what keeps `pkg/`
free of `internal/` imports. Audio-free builds omit those loaders and the audio
engine.

A `js/wasm` build performs no host-directory discovery, so it remains playable
from the embedded scenario, content, and keymap, and as a guest it is served the
session's scenario like any joiner. Page launch arguments do not make external URLs
into files; downloadable `wad/` content needs an HTTP-backed resource provider. See
[Build profiles and platform boundaries](multi-platform.md).
