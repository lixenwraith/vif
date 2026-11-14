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
├── scenario/                    ├── scenario/  fallback scenario
│   └── base/   the shipped      ├── content/   fallback corpus
│               scenarios        ├── input/     default keymap
├── content/    typing corpus    ├── bot/       shipped bot graphs
└── image/      .aimg assets     ├── font/      cell font atlas
                                 ├── vif.toml   default settings
                                 ├── version.txt source-archive stamp
                                 └── splash_font.go
```

The main campaign lives only in `internal/asset/scenario/`. Native, headless,
and browser builds embed that same tree; `-s main` falls back to it when no
external override exists. To author a variant, copy that directory into a
configuration root's `scenario/` under a name of its own. A scenario named `main`
in any root takes precedence, a copy an earlier release installed included.

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
├── scenario/    scenarios, each rooted at scenario.toml, in groups
│   ├── base/    the shipped ones; vanguard/behavior/ holds its programs
│   ├── downloads/  what sessions' hosts sent, when the player saves it
│   ├── main/    optional default override
│   └── test/puzzle/tic-tac-toe/  a player's own, filed as they like
├── input/       keymap.toml
├── audio/       music.toml, sounds.toml (optional overrides)
├── content/     .txt and .toml typing corpus
├── image/       .aimg wall assets
└── bot/         bot graphs, one <name>.toml each
```

A directory under `scenario/` holding `scenario.toml` is a scenario, and what lies
below it is its own: its region, macro and behavior program files sit beside the
entry or below it, as the shipped `vanguard` keeps its raiders' programs in
`behavior/` ([FSM reference](fsm-reference.md#behavior-programs)). Any other
directory is a group of scenarios and groups, to any depth, so a player can file
their own as `test/puzzle/tic-tac-toe/`. A name beginning with a dot is passed
over, and a link counts as a scenario but is not read as a group. A scenario is
selected by name (`-s td`), by its path below `scenario/` or by an explicit path
(§3); an optional one named `main` is the automatically discovered scenario.

`downloads/` (`[paths] downloads`) holds a scenario a session's host sent when
`[session] save_downloads` asks for it, named for it and its digest
(`td-3f2a9c1b0e7d`): no name a host sends climbs out, hides, or takes one a
player's own could have. A later join reads it there by digest instead of
downloading it again, and `-s` and the menu play it by that name; a copy the
player edits answers no join and is never written over. Saving is off by default,
since the host, not the player, chose what it holds, and a run that cannot write
there plays on and says why on its status line. `make install-config` creates the
directory empty and `CLEAN=1` leaves it.
`audio/` is empty until a user or stapes (formerly soundlab), the audio authoring
tool, writes an override: stapes reads and writes the user root's `audio/`
documents and keeps its own files in a root of its own ([Audio §4](audio.md#4-stapes)).
`image/` is the discovery category for `.aimg` and `.aimgz` assets named by a
`WallPatternSpawnRequest`.

## 3. Resolution policy

An individual resource flag (`-s`, `-f`, `-k`, `-config-music`, or
`-config-sounds`) is strict and always wins. `-s` first accepts an existing
`scenario.toml` path or scenario directory; a name such as `td` then resolves
through the roots below to the shallowest directory of that name under
`scenario/`, the first in path order among equals, and a path below `scenario/`,
such as `test/td`, to that one alone. Without an override, every resource walks
the same roots in order:

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
| Scenario | `scenario/**/main/scenario.toml`, as `-s main` finds it | embedded scenario |
| Keymap | `input/keymap.toml` | embedded keymap |
| Music | `audio/music.toml` | built-in patterns |
| Sounds | `audio/sounds.toml` | built-in sound bank and voices |
| Content | `content/` | embedded tutorial corpus |
| Wall image | `image/<name>.aimg`, or `.aimgz` | none; failure is reported in the game status |
| Bot graph | `bot/<name>.toml`, or the path `-bot` names | the embedded graph of that name |

An explicit scenario directory means one whose entry is directly at
`<directory>/scenario.toml`. A named scenario is searched root by root, each
root's whole tree before the next, so a user root's `test/td` shadows a system
root's `td`; the menu's list names a scenario its name does not reach by its path
instead, and one a higher root holds at the same path not at all. An explicit content file pins delivery to that file. For a wall
image, the event's path is a logical name below each root's `image/` directory,
opened only as a regular file there; it may include nested directories but not
`..`, and a host path, absolute or relative, is never opened, since the scenario
naming it may have come from a peer. Missing explicit paths or names other than
the built-in `main` are errors; absent discovered overrides are normal.

`-d` bypasses scenario and content discovery only, and refuses `-s` and `-f`.
Keymap, audio and bot-graph overrides remain local participant preferences and
retain their ordinary resolution.

A joiner that does not resolve the session's scenario to the same digest, from its
roots or its downloads, is served the coordinator's during the join and holds it in
memory, writing it nowhere but its downloads, and there only when asked
([Services §7.2](services-and-networking.md#72-join-identity-and-who-decides-it)).

A fleet node carries a partial root. `/var/db/vif/wad` holds `scenario/` and
`image/` and nothing else, mounted read-only at `/wad` in every session pod, so a
session resolves its scenario and wall images from the node and everything else
from the binary.
Content is not a category there: a dedicated host spawns no glyphs, each guest
reads its own corpus, and a guest without the session's scenario is served it at
the join.

`vif.toml` at the top of a root holds what a flag would otherwise say on every run:
`[paths]` names another root (`root`, read as `-config-dir` is), the log,
journal and music directories, the scenario, content and keymap `-s`, `-f` and `-k`
take, and the downloads (`downloads`, default the user root's
`scenario/downloads`); `[session]` what a run keeps of its sessions past the
process, its places (§5) and with `save_downloads` the scenarios hosts send;
`[audio] buffer_ms` sets the mixer period; `[presentation] window = true` opens
the desktop window by default, as `-g` does, and `maximized` or `fullscreen` the
state it opens in, `cell` is the zoom Android and a browser open at, and
`tmux_frame_cap` (true) draws the terminal at most 24 frames a second inside tmux
or screen ([Rendering §1](rendering.md#1-frame-pipeline)); `[play]` is
how a run starts and what the configuration menu changes in it: the audio channels
(`sfx`, `music`), the sfx volume (`sfx_volume`), auto-fire, mouse input, pointer
following, the telemetry HUD, relaying for others, taking over as host
(`takeover`), announcing its games on the
local network (`lan`), each game started or restarted
waiting for its player's press, the most frames drawn a second (`max_fps`), and
whether the menu or the command line pauses a game that can pause (`pause_menu`,
`pause_command`), and how near the view's sides the cursor comes before the view
follows it (`camera_edge`). It is found like any resource, from `-config-dir` down to the
system roots; the first file wins whole, a key it omits keeps the embedded
default, and the `vif.toml` inside its own `root` is not read. Every key is a default: a flag given on the command line wins, and `-d`
still takes the embedded scenario and content. A relative path is relative to the
file (a scenario name, or a path below `scenario/` such as `base/td`, stays a
name the roots resolve, as `-s` takes it; `./` makes it a path), `~/` is the home
directory. Music and sound overrides stay out of it, because a run that creates no
audio refuses them and a default must not break `-serve`.

Every run, on every host, reads `vif.toml` as this build brings it up to date
(`paths.UpgradeSettings`). A key this build does not take, or whose value it
refuses (a `buffer_ms` the mixer cannot run at among them), goes with the comments
that are its own (a comment group belongs to the side fewer blank lines part it
from, below on a tie), so a build older than the file drops the newer keys and a
refused value starts at its default. Each key the shipped file sets that it lacks
then comes with its own comments after the key the shipped file puts before it, a
table it lacks where the shipped file heads it (toml `Document.Fill`). Every other
line, comment and value stays, one equal to its default included, so a default a
later build changes reaches only a file that lacks the key. A table written inline
(`play = { mouse = false }`) is the exception, since toml edits no key inside a
value: a key dropped from it goes as the table is written again without it, in
toml's own spelling and without the comments inside it, and a key it lacks is not
added but reads as its default ([TODO](todo.md#let-toml-edit-a-key-inside-an-inline-value)).
Starting, vif writes the result back to the player's own file (below), once, and
names the changes on its status line, or on standard output for a run that draws
nothing (a headless bot or replay, `-script` without `-watch`, `-serve`), counting
the added keys past five (`parameter.UpgradeNamed`). `-check` names them, writes
nothing, and says of the file it read whether a run writes it back, or reads it up
to date as another's or as one a write refuses (below). The keymap is brought up to
date the same way ([Input §10](input-and-modes.md#10-keymap-override-files)).

One rule holds on every host: a flag sets its value for its run alone, `vif.toml`
holds the value every run starts from, and the menu writes back what the player
changes. A run writes each `[play]` value, and the zoom as `[presentation] cell`,
back as the player changes it, through a menu row or the command behind it, so the
next starts the same: into the `vif.toml` it read when that is the player's own, in
the user root or the `-config-dir` root, else into the user root's, created at the
first change as a copy of the one it read, brought up to date with its relative
paths made absolute, that shadows it from then on. A file in the checkout's `wad/`
or a system root is never written, whatever names it, `-config-dir` included, and
neither is a special file such as a link to `/dev/null` or a pipe: a run reads it
brought up to date and leaves it as it is, and only an install brings a system
root's up to date (§4). A write replaces the file whole by rename, through a link to
the file it names, keeping its mode and, as far as the writer may, its owner; a new
file is made 0644. One no one may write, or a link to nothing, is refused, and the
status line says so. A rename leaves a hard link's other names on the old file
([TODO](todo.md#write-a-hard-linked-settings-file-in-place)). Only a value changed in the run is written,
so one a flag set (`-mute`'s channels, `-nw`'s wait) stays out unless the player
then changes it. No flag saves the others: a flag is what an alias, a script or a
link repeats, and writing it back would let the last command line decide every
later run. A browser keeps them in the site's storage
([Multi-platform §7](multi-platform.md#7-external-maps-and-assets)).

## 4. Installation

```bash
make release install                                 # build, then /usr/local, else ~/.local and the user root
make install-config                                  # the user root only
make install-config VIF_CONFIG_DIR=/path/to/stage    # into a staging root
make install DESTDIR=/pkg PREFIX=/usr                # distribution package, root /etc/vif
make uninstall                                       # what install put under PREFIX
```

Both copy `wad/`, the bot graphs, the keymap and `vif.toml`. A reinstall replaces
every shipped file and keeps files a player added. The keymap and `vif.toml` are
the player's settings: one already there is brought up to this build's as a run
brings it and written as §3 writes (`tool/vif-settings`), and the install names
the changes. Without Go on `PATH` it keeps the file as it is, which a run reads
brought up to date and writes back only when it is the player's own (§3); one that
does not parse it keeps for the player to fix, as a run refuses it too, and a
special file or one no one may write it leaves as a run does. `CLEAN=1` first
removes the shipped categories (`content/`, `image/`, `bot/`, and of `scenario/`
its `base/` and the copies of shipped scenarios an install from before `base/`
left at its top, which would shadow them) and those two files, so the root holds
only the fresh install plus `audio/`, a player's own scenarios and anything else a
player put beside them. `install` adds the binary, licence, manual, desktop entry
and icons, completion and documentation under `PREFIX` with the wad at
`$(SYSCONFDIR)/vif`. A user who cannot write `/usr/local/bin` gets, unstaged,
`PREFIX=~/.local` and the user root as the defaults; `sudo make install` installs
system-wide, and `make help` prints the values in effect. `install` writes nothing
until it can write all of `PREFIX` and the root, and leaves the mode of existing
directories alone. `uninstall` keeps the root, which in a user install holds the
player's settings. See [Packaging](packaging.md).

## 5. Logs, journals, and runtime output

Linux, the BSDs and the other Unix-like targets use `$XDG_STATE_HOME` when it is
an absolute path, else `~/.local/state`, for every install, and keep the streams
separate:

| Output | Default | Override |
|---|---|---|
| Session logs, snapshots, recorder files, runtime stderr capture, profiles and traces | `$XDG_STATE_HOME/vif/log/` | `-l=DIR`, `paths.log` |
| Replay journals | `$XDG_STATE_HOME/vif/journal/` | `-j=DIR`, `paths.journal` |
| Music recordings | `$XDG_STATE_HOME/vif/music/` | `-mw=DIR`, `paths.music` |
| The places a run comes back to its sessions with, the key each is held under, for the user alone (mode 0600) | `$XDG_STATE_HOME/vif/places.json` | `[session] resume_after_restart = false` keeps none |

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

`internal/asset` owns every embedded group of this module and is its only
package with an `embed` directive for shipped data. The sound and music banks are
the audio module's `bank` package, and its root package carries no specs or
patterns of its own: in an audio-capable build the audio service parses them with
`bank.Sounds` and `bank.Patterns` and hands them to the engine as
`AudioConfig.BaseSounds`, `BaseVoices` and `BasePatterns`. Audio-free builds omit
those loaders and the audio engine.

A `js/wasm` build performs no host-directory discovery, so it remains playable
from the embedded scenario, content, and keymap, and as a guest it is served the
session's scenario like any joiner. Page launch arguments do not make external URLs
into files; downloadable `wad/` content needs an HTTP-backed resource provider. See
[Build profiles and platform boundaries](multi-platform.md).
