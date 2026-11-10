# TODO

Scratch notes that travel with the repository. Each item states what remains and
the smallest thing that would close it. Delete an item when it lands; this file
is not a changelog. Source files do not carry parallel TODO comments, so a
code-originated item names its source here.

Priorities: P0 blocks a release or leads the feature development push, P1 is
wanted next, P2 is convenient follow-up, and P3 is an idea.

## Vanguard

The side-scroller's refinements after its first play test (2026-10-27) lead, grouped
by area so an agent can take a group. The faster scroll, the jagged cave, the stone
art, the raider types by cave, the scenery text, the boss stage, the machine's
certain faults, navigation's cost over the cave and the position package
([Position](position-package.md)) have landed. What is left is the machine
follow-ups. A cave's shape stays in `pkg/cave` and its scenario, the rock behind
`Positions.Rock()` and the wall system and renderers that read it.

### Machine follow-ups

What vanguard met in the machine and is still open, its certain faults having
landed.

#### Give a program's entrance an entering state

- Priority: P2
- Affected files: `internal/behavior/program.go`, `internal/system/behavior.go`
  (`enter`), `internal/component/behavior.go` (`BehaviorEntrance`)
- Guide: [Behavior machines §4.4](machine-design.md#44-spawns-that-ride-their-materializes-and-spirits)

A prototype's `entrance = "materialize"` plays its own materialize style over the
footprint for its `entrance_ms`, the style's whole when that is 0, and lands the body
as it ends, beside any rock that came over it, and nothing of it reaches the machine,
which starts in `initial` the tick the body stands. Lixen expects entrances to grow for machines (2026-10-27).
What a program would declare next:

- Whether a countdown shows over the materialize.
- An entering state the machine runs while the materialize holds, so a program can
  telegraph, re-aim or cancel its arrival; while it runs the body neither strikes
  nor takes damage, as no body stands yet.
- A landing its probe chooses (`FindLanding` or its own) as the materialize ends,
  in place of the nearest clear footprint.

The materialize is Shared, so the entering state's clock and probe must run alike
on every instance and a capture mid-materialize must restore them
(`snapshot.Schema`).

### Position, blocking and navigation

The move into `pkg/position` is done ([Position](position-package.md)); what
remains stands on its own.

#### Play a volume: the third axis's remaining foundation

- Priority: P3, not now: it needs other foundation changes first
- Affected files: `internal/engine/position.go`, `internal/app/internal/config`,
  `internal/snapshot`, `internal/system/navigation.go`, `pkg/navigation`,
  `internal/render/renderer`, `pkg/vmath`, `pkg/position/surface.go`
- Guide: [Position §3](position-package.md#3-the-volume),
  [Rendering §2](rendering.md#2-render-context-and-coordinates)

The index, the math and the physics are laid for a volume: `pkg/position` holds
`Depth` layers and answers a `Point3`, a `Box` and a `Footprint3` on any of them,
its surfaces on the layer they name, and `pkg/vmath` and its physics have a form
through a volume beside each flat one. What a game in depth still needs is not
laid:

- The binding and the records: a depth in the config the binding resizes to
  (`Position.Resize` passes one layer), and a row that carries its layer, which
  moves the journal, the captures and the digest, a row being a flat `Point`.
- Navigation: a footprint with depth and a passability per layer, where one
  flat grid is derived from `BlockBits`.
- Rendering: a flat renderer reading the layer it looks at, and a render in
  depth ordering overlap by a depth buffer over every layer projected
  (`sandbox/3d`).
- The forms left flat for want of a use: the cell strip `Ray` and `Octants`,
  the arcs on a sphere, a depth in `vmath.Aspect`; the searches that cross
  layers are [their own item](#placement-searches-that-cross-a-volumes-layers).
- A heightfield along the third axis: a height for each column and row, solid
  through the layers under or over it. A `Surface` a layer is a field over
  columns and layers only.
- The ceiling: `MaxCells` bounds a volume as it does a map (`ClampVolume`), 2 MB
  of cells, 100×50×25 or 200×60 ten layers deep. A game in depth raises the one
  constant, and past what stays dense needs cells chunked on demand.

#### An index typed by its coordinate, `Index[E, P]`

- Priority: P3, to evaluate when flat and volume games are to run on the engine together
- Affected files: `pkg/position`, `internal/engine/position.go`,
  `internal/engine/world.go`, every call site of a query
- Guide: [Position §3](position-package.md#3-the-volume)

The choice made: one `Index[E]` whose depth is set at `Resize`, each query a
generic method taking a `Point`, its cell on the first layer, or a `Point3`, the
two told apart by size at compile time, at no cost to a probe.

What the other form does: the point is the index's second type parameter, a flat
map an `Index[E, Point]` and a volume an `Index[E, Point3]`. Its methods are plain
ones taking `P`, the block log is in `P`, and the compiler refuses a `Point3` on
a flat map.

What it entails: the engine's store embeds the index and every game shares one
`World`, so `P` is one type for the whole binary. A volume game beside flat ones
then needs a `World` generic over `P`, or a second store, or `Point3`
throughout: every game's rows, log and captures carrying a Z, and every flat
call site, the 155 the conversion rewrote among them, passing a `Point3`. Go
derives no area or footprint type from `P`, so those queries stay split by name
or stay generic methods.

Why it was not chosen: with one `World` for every game, a flat system passes the
`Point` its rows hold and a volume's a `Point3` to the same index, a flat row
stays two ints, and a volume comes up a system at a time. The form taken pays
for that with generic methods, new in Go 1.27, which no interface can hold and
tools may trail, a type parameter on every signature, and nothing at compile
time between a `Point3` and a flat map. A probe costs the same under both.

Evaluate it when a flat game and a volume game run on the engine in one build:
if each then has a world of its own type, the point belongs on the index.

#### Placement searches that cross a volume's layers

- Priority: P3, when a game in a volume needs a place found off its origin's layer
- Affected files: `pkg/position/search.go`, `pkg/vmath` (`Aspect`, the diagonal scale)
- Guide: [Position §3](position-package.md#3-the-volume)

The spiral, the pattern search and the exclusion search each work the layer
their origin names, a body with depth tested through the layers it spans, and so
does the directional scan. What a layer means, a storey or a slice of depth, is
the game's, so none was guessed. A form of all three that crosses layers
searches 26 directions where a layer has 8, and has to settle: the order the
layers are tried in, a depth in `vmath.Aspect` beside the cell's width and
height, the scale of a corner diagonal (1/√3, beside the 1/√2 of
`DiagonalNum`/`DiagonalDen`), what clockwise means off the plane, whether the
pattern's area gains a depth, and the slots before and behind an exclusion that
has one. It goes beside the layer forms and changes no caller of them.

#### Profile td's storm peaks

- Priority: P2
- Affected files: `internal/system/wall.go`, `internal/system/death.go`,
  `internal/system/navigation.go`
- Guide: [Navigation §10](navigation-design.md#10-costs)

Under a raised storm with eight cursors walking, td's ticks where hundreds of
walls go reach 40–60 ms and compute no field, the wall system's event peaking at
34–51 ms and navigation at 15–17 ms. The box characters' neighbour test, once the
suspect, reads a bit of the index's planes now, and td's storms draw none: the
peak is elsewhere in the walls' death, to be profiled before anything is changed.

## Bots

### Open fleet sessions to bots

- Priority: P2
- Affected files: the Hugo site's `vif-lobby.js`, `pkg/network/endpoint.go`,
  `internal/mode/config_form.go`, `tool/vif-router`, `deploy/guest/vif-router.env`,
  `deploy/k3s/10-quota.yaml`
- Guide: [Session fleet](kubernetes-fleet.md) §6

The router takes `bots`, but `bots_max` stays 0 until these hold:
- The site's `vif-lobby.js` sends `bots` and reads `bots_max`; the native client
  (`network.RequestSession`, the menu's request form) sends `bots`.
- The quota's 1000m of CPU fills at three sessions of three bots: raise it, or
  accept fewer sessions. A create the quota cannot hold waits 75 s for a pod that
  never schedules and answers 504 `session_not_ready`; sum the live sessions'
  requests against the quota and refuse it as `fleet_full` instead.
- The CPU limit stays 500m while each bot adds 64m of request, and six bots with
  one person use about 0.5 core: grow the limit with bots (the `LimitRange` allows
  1 CPU), or bound `bots_max` by CPU as well as memory.
- td: refuse bots on ceiling-sized maps by map cells, or leave td off the offered
  scenarios, until the map-limit trim below lands.
- K=3 measured on the node with `./script/test.sh load` or the deploy guide,
  replacing the sandbox figures (2 sessions of 2 bots for 30 s: about 190–200 ms
  of CPU a second and 90–95 MiB peak per host, no evictions).
- Digest mismatches watched as networked bot load grows; that run counted 0–5 per
  host.

### Expose a bot difficulty knob

- Priority: P3
- Affected files: `internal/app/internal/membership/seat.go`, `cmd/vif`,
  `internal/mode/config_form.go`, `tool/vif-router`

`actions_per_second` is the only knob, and nothing caps it. Options: a rate suffix
such as `-bot 2:default@6` that `BotSpecs` parses, so `-bot`, `:bot add` and the
menu share it; graph variants per level; or a ceiling the driver clamps every
graph to. The fleet request carries it once the site exposes one.

### Let a watched holder pause with only its own bots

- Priority: P3
- Affected files: `internal/app/play.go`, `internal/app/internal/membership/seat.go`

A watched bot with seats is in a session, so the viewer forces it unpaused and
ignores pause keys. A person's run pauses with only its own bots; the viewer's pause
key could start the session hold `:pause` starts when the holder is its session's
host, which already stops its seats at one tick.

### Train bot graphs offline

- Priority: P3
- Affected files: `internal/bot`, `lixenwraith/genetic`,
  `internal/system/nugget.go`
- Guide: [Bots](bots.md#fitness-and-comparing-graphs)

First decide how a graph declares bounded tunable numbers: a `[bot.params]` table,
or bounded macro parameters shared with the editor's macro metadata. A genome is
those numbers as a bounded vector; `lixenwraith/genetic`'s `Engine` evaluates
whole headless runs over a matrix of scenarios and seeds, and
`lixenwraith/genetic/fitness` scalarises the result. A solo run depends only on
its seed, so evaluation needs no mesh. Decide where trained values ship. Bot
genomes never enter the in-game genetic registry, which captures carry. Fitness
weighs no nugget, since nothing pushes `EventNuggetCollected`: push it in
`NuggetSystem.collectNugget` and drop that function's own `statCollected`
increment, which the event's handler repeats.

### Adapt bots to the session's players

- Priority: P3
- Prerequisite: [Train bot graphs offline](#train-bot-graphs-offline)

A small model of each other participant from Shared state, keyed by participant
for the session and dropped at its end, and a bandit over trained genomes. Decide
what is modelled and whether players see it; nothing is persisted.

## Authoring

### Build the map and scenario editor

- Priority: P2
- Affected files: `lixenwraith/toml/macro`, `internal/fsm`, `internal/manifest/schema.go`,
  `internal/mode`, `internal/render/renderer`, `internal/resource`
- Design: [TOML macros](toml-macros.md)

The editor writes ordinary scenarios: a directory under the user root's `scenario/`
that loads, digests, transfers and lists like a hand-written one, so nothing at run
time knows an editor made it.

- **Documents, not a format.** It edits the parsed trees of the entry, region and
  macro files and saves them with `toml.Marshal`, which drops comments, so it writes
  only files it owns (a blocks file, a region file per edited region) and opens
  hand-written ones read-only.
- **Blocks are macros.** The palette is the scenario's macro set and a block's form
  its parameters, defaults and `doc`. A placed block is an invocation, and one
  placed as a state (`Name = { macro = ... }`) is a node the graph view draws. A
  block from a shared library is copied into the scenario's macro files on first
  use, since expansion reads nothing outside the scenario.
- **Macro metadata still needed**, each additive to `lixenwraith/toml/macro`: reference kinds
  (`state`, `region`, `var`, `event`, `status`) the expander checks as strings, so
  the editor offers pickers and draws an edge per `state` argument; a `point` kind
  the map canvas shows as a draggable marker for pylons, towers and spawns;
  parameter order and per-parameter docs, since a TOML table has no order; root
  `[[expand]]` for blocks spanning several states or a whole region; and provenance,
  so a load error such as an unknown target lands on the block that produced it.
- **Vocabulary.** Action, guard and event pickers read `-schema`, which lists each
  vocabulary's actions and guards with their arguments and every event's payload
  fields ([Behavior machines §5](machine-design.md#5-dialect-kernel)).
- **Map layer.** Geometry is generated at run time (`EventLevelSetup`, maze rooms,
  `.aimg` walls). A painted map needs a static form the digest covers, and
  `.aimg` assets come from the `image/` category, which it does not and a joiner
  is not sent. Decide between a TOML wall encoding (rectangles or run-length rows)
  and admitting `.aimg` to the scenario container under its bounds.
- **In the game.** An editor mode beside command and visual in `internal/mode` acts on
  a paused world and draws through an overlay like `:graph`. Applying an edit
  validates with `resource.ValidateScenario` and restarts on it as `:n` does, since
  a run plays one scenario. Editing inside a multiplayer session is out of scope.

In order: reference kinds, order and provenance; a read-only graph of a scenario's
regions, states and blocks; placing and editing blocks into editor-owned files; the
map layer and point markers; root `[[expand]]` blocks and library copies.

### Write a macro as another with arguments fixed

- Priority: P3
- Affected files: `lixenwraith/toml/macro`, `wad/scenario/base/vanguard/macros.toml`,
  `wad/scenario/base/td/td_macros.toml`
- Design: [TOML macros](toml-macros.md)

Eight of the shipped scenarios' macros are one invocation of another: vanguard's
four ranks over `Raider`, and td's `Tower`, `QuadrantTower` and `Pylon` over
`SpawnAwait` and `FlankGateway` over `Gateway`. Each declares again the parameters
it passes through and forwards them one by one, 34 arguments written
`x = "${x}"`, so a parameter added to `Raider` is four more edits. The expander
could take a definition that names its base and the arguments it fixes, every
other parameter of the base staying one with its type and default. The form is
the TOML module's to add, and the audio bank waits on the same one for nine of
its nineteen macros.

### Add the cheap vim keys, and let the tutor override the game's

- Priority: P3
- Affected files: `internal/input/machine.go`, `actionnames.go`,
  `internal/asset/input/keymap.toml`, `wad/scenario/base/vimtutor/scenario.toml`
- Guide: [Vim tutorial](vim-tutorial.md), [Input and modes
  §4](input-and-modes.md#4-default-bindings)

The tutor's last lesson captures the keys vif reads its own way (`a` auto-fire, `p`
pause, `o` `O` column finds, `u` cursor undo, `m` the middle column) and the ones it
binds to nothing (`y`, `c`, `r`), and answers each press with what the key does
here; it tutors none. Two steps remain. The cheap vim keys in code: `c` with a
motion, `cc` and `C` (the delete operator's grammar, then Insert), `*` and `#`
(search for the glyph run under the cursor), `A` and `I` (Insert at the line's end
or first glyph), `.` (the last change recorded in the input machine) and `r`
(retype the glyph under the cursor, which needs a glyph replace event and a rule
for whether it counts as typing); `y` and `p` have nothing to yank or put in a
glyph field. And the tutor overriding the game's bindings for its run: it has no
gun to auto-fire and no clock to pause, so `a` should append and `p` do nothing
there, which a scenario keymap over the player's table
([Input and modes §10](input-and-modes.md#10-keymap-override-files)) would give it,
or a capture that answers `a` by entering Insert one cell on, which needs a mode
change a scenario can request. Rewriting intents in the router for one scenario
would be a second mechanism beside the key table and the capture; not that.

### Grow the vim tutor

- Priority: P3
- Affected files: `wad/scenario/base/vimtutor/`
- Guide: [Vim tutorial §3](vim-tutorial.md#3-the-next-round)

The first round teaches `h j k l`, `w b e`, `0 ^ $`, `f t ; ,`, counts, `x` then
`dd`, `i` and typing, `%`, `/ n`, `gg G`, then the taken keys and free practice;
`s` skips and `S` goes back. The next adds the column motions, `[ ]`, the markers
and macros, `d` with a motion, `v`, lays a lesson again on a resize, centres from
the map's edge, and cues a lesson cleared.

## Browser sessions

### Fetch a content-addressed bundle over HTTP

- Priority: P1
- Affected files: `internal/resource`, `cmd/vif/browser.go`, `pkg/browser`,
  `web/terminal.js`, `web/worker.js`, `Makefile` (`wasm`); on the site, `make vif`,
  `make compress` and `_partials/apps/publish.html`
- Prerequisite: the decisions in the last paragraph

The page plays the embedded `main` alone: a guest is served its coordinator's
scenario and a native player unpacks the wad, but nothing reaches a client with no
config root and no peer, so `wad/scenario/` is out of the site's solo Play. The
corpus needs no route, being player domain. A URL cannot carry one: the launcher
takes 1,024 characters an argument, `vanguard` deflates to about 13 KB, and a query
is logged and kept in history
([Multi-platform §6](multi-platform.md#6-browser-launch-arguments)).

The container is decided: `resource.Scenario`'s canonical form, one file a
scenario, its SHA-256 the digest a session names. The plan, same origin only:

1. `make wasm` has `vif` write `web/scenario/<name>.vifscen`, each scenario's
   `Marshal`, uncompressed so the file's SHA-256 is its digest (the site gzips it
   as it does the binary), and `index.json` beside them: name, description,
   players, fixed map, digest. The bytes are then the ones a coordinator sends.
2. The site publishes each under its content hash, which is that digest (`make
   vif` refuses a mismatch), and the index under its own, named to the launcher
   as `data-scenarios`, as `data-wasm` names the binary.
3. An HTTP provider in `internal/resource`, on the `net/http` `pkg/network`
   already links (fetch in a page), reads the index, fetches a file, holds it to
   `UnmarshalScenario`'s bounds and the index's digest, and hands it on as
   `Options.Provided`. The page's `-s` (`?arg=-s=td`) resolves through it, the
   menu's Scenarios page lists the index, and a hosting page
   ([Publish a player's session](#publish-a-players-session-at-the-router)) offers
   any listed scenario.

Served from the binary's own origin, a scenario adds no trust root, and the page's
CSP (`connect-src 'self'`) already admits the fetch. To decide: the extension, and
how the site's edge caches and precompresses it (the site's `doc/nginx.md`); every
`wad/scenario/` or only those a scenario marks for publishing; where the site
offers them (a picker by the lobby's Play solo, links per scenario); a visitor's
own scenario, picked or dropped on the start screen or carried in a link's
fragment, which no server sees (about 17 KB for `vanguard`), and whether one from
a stranger may run; a native `-s https://…`, which needs an index at a fixed path,
and from another origin a publisher's signature; any cache by digest beyond
HTTP's own, which a native run has in its downloads.

### Manage the downloads from the menu and the command line

- Priority: P2
- Affected files: `internal/mode` (`config_menu.go`, `commands.go`),
  `internal/resource` (`SaveDownload`), `cmd/vif/browser.go`, `web/terminal.js`
- Guide: [Filesystem layout §2](filesystem-layout.md#2-installed-configuration-tree)

`[session] save_downloads` keeps every scenario a host sends in
`scenario/downloads/`, and only a file manager removes one, so a player who joins
many sessions collects them, up to 1 MiB each. The Scenarios page would list the
downloads apart from the player's own, with size and age, and remove one or all
behind a confirm, as a command would (`:downloads`, `:downloads rm <name>`); a
Settings row would turn saving on where `vif.toml` is out of reach, as on Android.
A browser saves none: a page has only the site's storage, a few MiB that the
settings and places share, so saving there waits on these controls, a cap in
bytes that evicts the oldest, and the digest as the key (IndexedDB). A save cut
off mid-write leaves a hidden `.<name>.*` directory beside them for the same
clean-up.

### Launch the page and Android without their start screen

- Priority: P3
- Affected files: `web/terminal.js`, `web/worker.js`, `pkg/browser`,
  `android/app/src/main/java/com/lixen/vif/StartScreen.java`

Both open on their start screen whatever `[play] wait` says. The page's is up
before its program runs, so it cannot read the setting in time, and its press is
also the gesture a browser needs before it plays sound; Android's could close at
once, but would flash. Running the page's program behind its screen, as Android's
runs behind its own (`Host.Away`), would let `wait = false` skip both.

### Join a pasted game link from the Android app

- Priority: P2
- Affected files: `android/app/src/main/java/com/lixen/vif/StartScreen.java`,
  `MainActivity.java`, `android/game/game.go`

The site hands out a game's link in three shapes: the terminal command (`vif -join
vif://lixen.com:7777/<game>`), the bare `vif://` link, and the browser's
`https://lixen.com/vif/wasm/?arg=-join=wss://…`. The app takes none of them short
of typing `:join` and the link on the soft keyboard. A Join button on its start
screen could read the clipboard (or a field to paste into), strip a leading `vif
-join` and the browser link's `?arg=-join=` wrapper, keep the `vif://` or `wss://`
link and join it as `:join` does; a `vif://` intent filter would open links
tapped elsewhere. Unchecked without an emulator or a phone, so not built yet.

### Warn in the site's lobby when a session runs another build

- Priority: P2
- Affected files: the Hugo site's `vif-lobby.js`, `.github/workflows/release.yml`

Each row of the router's session list names the build its session runs
(`state.build`), as the refusal a page of another build reads does. The site's
lobby can show it, and warn before a Play that would be refused once it knows the
page's own build, which the release's web bundle does not name yet.

## Packaging

Distribution-specific detail lives in [Packaging](packaging.md).

### Promote a nightly build to the first stable release

- Priority: P1
- Affected files: `doc/packaging.md`, `README.md`
- Prerequisite: choose a nightly commit after the complete verification gate
  passes

Tag the selected commit `v0.1.0` and publish the draft release the tag creates,
which already carries the source archive and its checksum; then drop the "no final
release" lines in `README.md` and [Packaging](packaging.md) §2.

### Sign the Android build with a release key

- Priority: P2
- Affected files: `.github/workflows/release.yml`, `android/app/build.gradle.kts`,
  `android/README.md`

The nightly and a tagged build are signed with `android/debug.keystore`, which
anyone can sign with, so a build from elsewhere installs over them. Before a
release a device should trust: generate a release keystore (`keytool -genkeypair
-keyalg RSA -keysize 4096 -validity 10000`), add it base64-encoded as
`ANDROID_KEYSTORE_BASE64` with its passwords and alias as the three other secrets
the workflow reads, and keep the keystore out of the repository; a key once
published cannot be rotated for installed apps. The first signed build will not
install over a debug-signed one: uninstall first, which drops the app's settings.
A store listing (Play wants an AAB and a `targetSdk` within a year of the newest
release) is a separate decision.

### Publish a development AUR package

- Priority: P3
- Affected files: AUR packaging metadata and `doc/packaging.md`
- Prerequisite: establish the release-package metadata first

Publish `vif-git` alongside the release-based AUR package.

### Move the Android toolchain from sdkmanager to the Android CLI

- Priority: P3
- Affected files: `android/toolchain.sh`, `android/README.md`

The command-line tools `toolchain.sh` pins (22.0) warn that `sdkmanager` is
deprecated for `android sdk`, which ships in the same package. Switch `setup` to it
once its install, licence and SDK-root flags are settled, bumping the pinned build
and its three SHA-256s together.

### Give the Windows zip sound, and confirm it in Windows Terminal

- Priority: P3
- Affected files: `Makefile`, `.github/workflows/release.yml`
- Guide: [Build profiles](multi-platform.md) §4

The zip has no sound: try a build without `vif_noaudio` with ffplay on `PATH`.
Its mouse comes from Windows 10's own console host, and likely its flicker (§4);
confirm that Windows Terminal has neither, with the terminal module's synchronized
frames and re-hidden cursor. `sandbox/termbench` tells hosts apart: `rawin` prints the
records a mouse report arrives as, `live -lib terminal|tcell` plays a scene on
each library's own backend.

### Report key releases on Windows

- Priority: P3
- Affected files: the terminal module's Windows backend and key protocol
- Guide: [Input and modes](input-and-modes.md) §1

A key on Windows is a tap: the console reads as VT input, which carries presses
alone. A console host with win32-input-mode (Windows Terminal's) reports each
key's press and release once asked with DECSET 9001, as `CSI Vk;Sc;Uc;Kd;Cs;Rc _`
(tcell asks for it and drops the releases). A Windows key source would ask for it
as the protocol source asks for the key protocol, answer presses where the host
lacks it, and pair releases through `KeyHolds`. Decide first whether holds on
Windows are worth it.

## Fleet

The design and what it cost are in [The session fleet](kubernetes-fleet.md), and
the procedure in [Fleet deploy](kube-docker-deploy.md).

### Hand off a deployment a stranger can install

- Priority: P1
- Affected files: `doc/kube-docker-deploy.md`, `deploy/`
- Guide: [Fleet deploy](kube-docker-deploy.md#13-first-session) §13

Rehearse the procedure from bare Arch Linux and from bare Ubuntu, record their
package and service differences, and fix every command that assumes the
production node. Done when both reach a first session with no step their operator
had to invent, and every resource value in `deploy/k3s/30-session.yaml` cites a
number from [Measure a full roster](#measure-a-full-roster).

### Measure a full roster

- Priority: P1
- Affected files: `deploy/k3s/30-session.yaml`, `deploy/k3s/10-quota.yaml`,
  `tool/vif-router/manifest.go`
- Guide: [Session fleet](kubernetes-fleet.md#6-resources) §6

Drive nine sessions with headless joiners for the fleet-level readings (CPU,
memory, tmpfs, log rate, rotations), plus one real four-player session over real
links, through a tower and a storm and on `wad/scenario/base/td`, for the hour the
Job's deadline allows, which tick slips and correction magnitude need. Requests,
limits and the quota totals then come from that measurement rather than from
single-guest history and estimates. It passes with no OOM, liveness restart,
sustained tick slip or growing correction magnitude, the deadline ending each
session. The socket readings (`TestNetcodeReadingsOverSockets`) cannot seat
sixteen td guests inside their join wait, so the td-16 column of netcode design
§3.1 has not been re-read since phase 1 and is this measurement's to take.

### Apply the fleet log retention on the node

- Priority: P1
- Affected files: `deploy/guest/vif-fleet-log-cleanup.py`, `doc/kube-docker-deploy.md`

Decided: the cleanup's stale age is 75 minutes, just past the session Job's
`activeDeadlineSeconds`, so the tmpfs holds live sessions and the ones that just
ended instead of five hours of finished ones; a byte budget or a larger tmpfs
were the alternatives. Apply on the node: reinstall the cleanup script as
[Fleet deploy](kube-docker-deploy.md) §7 does, then watch `findmnt` after a full
hour of play.

### Promote verified images to the node

- Priority: P2
- Affected files: `deploy/guest/update-vif-image.sh`, `.github/workflows/release.yml`,
  `deploy/k3s/30-session.yaml`, `tool/vif-router`
- Guide: [Fleet deploy](kube-docker-deploy.md#8-the-session-image) §8

Nightly CI publishes the headless image to GHCR under moving and commit-addressed
tags, and the node builds and imports its own. What remains is choosing the
node's registry and promotion policy, authenticating pulls without a long-lived
off-node deployment credential, and moving new sessions to a verified digest while
existing ones finish on theirs.

### Put a cluster through its failures

- Priority: P2
- Affected files: `doc/kube-docker-deploy.md`
- Guide: [Session fleet](kubernetes-fleet.md#7-verification) §7

Not yet tried on a cluster: `tc netem` latency, loss and reordering, which should
recover at the next bounded correction or keyframe; a pod delete or a node
failure, whose connected-client behaviour should be recorded, with no replacement
advertised as the same match; and a full log tmpfs, which should lose a bounded
amount of log without ending a game.

### Provision audio to the fleet when a host needs it

- Priority: P3
- Affected files: `deploy/guest/update-vif-wad.sh`, `tool/vif-router/manifest.go`,
  `deploy/k3s/30-session.yaml`

What a fleet session reads from the node volume is one list in three places —
`categories` in the installer, `wadCategories` in the router, and the mounts in
the template. `scenario` and `image` are on it. `audio` goes on it the day a
dedicated host renders anything, and `content` never does. Nothing else changes;
the volume already carries whatever the installer puts there.

## Multiplayer

### Hear a participant again once its link reopens

- Priority: P1
- Affected files: `internal/system/network.go` (`announceDeparture`,
  `removeParticipant`, `addParticipant`, `receiveAuthority`, `receiveDeparture`),
  `internal/converge/authority.go` (`onReport`, `takeBack`, `reconcile`),
  `internal/converge/reach.go` (`admitPeerLink`), `internal/app/migration_test.go`
- Guide: [Multi-player §5.1](multi-player.md#51-succession),
  [§5.2](multi-player.md#52-the-roster-after-a-loss),
  [§5.3](multi-player.md#53-reachability-under--authority-migrate)

An instance marks a participant departed the moment its own link to it drops
(`announceDeparture`), and the coordinator's crossing marks it on every instance
(`removeParticipant`). Only an arrival clears the mark (`addParticipant`), and while
it stands `receiveAuthority` and `receiveDeparture` drop whatever that participant
sends. A reopened link clears nothing: `admitPeerLink` admits any roster member, a
Wraith included, so the link stands and the succession hears nothing on it. A person
whose link to its host drops is unaffected, since its redial proves its key and
the key's arrival clears the mark. Two cases are not:
- Two guests' warm link (§5.3 item 3) drops and reopens while the host lives.
  Neither end coordinates, so nothing crosses and nothing clears, and each ignores
  the other for the rest of the session. When the host goes, the candidate takes
  the term and drops the other's notices (`MsgAuthorityReport`), the other drops its
  record (`MsgAuthorityHandoff`), and at its own turn takes the next term.
- A survivor's link to the candidate is down when the candidate takes the term and
  reopens before the survivor adopts a record. The new host crossed its departure,
  so the notices on the reopened link are dropped, the survivor is never answered,
  and it takes a term of its own. Only a report another participant relays passes
  the mark; one on the survivor's own link never reaches `takeBack`.

Either way the session splits: two hosts, each holding the other and the lost host
as Wraiths. A survivor with no path to the candidate is meant to split off (§5.1);
one whose link came back is not. A three-participant `meshSession` probe (links
1-2, 2-3, 1-3; 2-3 cut with `MeshPort.Disconnect` and linked again with `Mesh.Peer`,
before the host closes or across 2 taking term 2) ended in all five runs with 2 on
term 2 and 3 on term 3, two Wraiths each; without the cut both were on term 2.

What would close it:
- A link admitted after the mark was set clears it, so a mark nothing crossed is
  gone the moment the link is back. That fixes the first case alone.
- For a participant whose departure crossed, the reopened link carries its walk: the
  host answers its notice, takes its membership report, and gives the place back
  through `CrossReturns`, whose arrival clears the mark everywhere. `takeBack`
  declines a report that lands before the floor (`reconcileAt` still set), and
  `recordReport` never offers it again, so the floor has to give back an away
  participant that reported at its term.
- The coordinator still crosses one departure per participant: clearing the mark
  must not let a second drop of the same link cross a second departure for a Wraith.
- A test per case on the probe's shape, with `meshSession` handing back its mesh:
  participant 3 follows 2 at term 2, and no Wraith but the lost host's remains.

### Refuse a combat crossing that acts as another cursor or emitter

- Priority: P1
- Affected files: `internal/system/network.go` (`crossingActors`,
  `admissibleFromSource`), `internal/system/combat.go` (`applyHitDirect`,
  `applyHitArea`), `internal/app/correction_test.go`
- Guide: [Multiplayer §2](multi-player.md#2-current-contract) (Trust)

`crossingActors` holds no row for `EventCombatAttackDirectRequest`, so a modified
participant's rod, tracer or bullet hit may name another participant's cursor as
its `OwnerEntity`: every instance then spends that cursor's immunity allowance
against the target (its own hits are refused for the window), credits it the kill,
and its own instance pays it the drain reward. Neither combat crossing checks
`OriginEntity`, the area crossing's row reading the owner alone, yet both resolve
the attacker's profile from it: an area crossing naming an eye with
`CombatAttackSelfDestruct` deals the eye's 5 against a Shared species where a
cursor weapon deals 1. Every legitimate combat crossing names its own cursor as
both, since a Player-domain emitter is never named and a Shared one (an eye, a
mount) never crosses. What would close it: a row for the direct request on its
owner, and on both requests an emitter that is zero or the owner; a refusal test
for each beside the `network.artifacts_refused` ones.

### Cross a departure a reset dropped

- Priority: P2
- Affected files: `internal/system/network.go` (`Init`, `announceDeparture`),
  `internal/app/transport_test.go`

A link lost within a lead of a reset loses its departure: the reset clears the
barrier (`NetworkSystem.Init` drops `scheduled` and `departed`) before the
departure crossed for it applies, and the next game rebuilds that participant's
cursor from the roster as a live one nobody drives, which never turns Wraith, never
times out and holds the slot its key comes back for: a player who quits as its
defeat ends the game can leave one. `TestTwoLiveParticipantsConvergeOverTCP` shows
it once its scripted defeat's reset is moved into the disconnect's lead. What would
close it: after a reset the authority crosses, once, the departure of every
rostered participant it neither links nor carries.

### Publish a player's session at the router

- Priority: P3
- Affected files: `tool/vif-router`, `internal/app/internal/membership/host.go`,
  `internal/network`, `web/terminal.js`
- Guide: [Multiplayer](multi-player.md#56-relay-doors-and-the-router) §5.6

A run publishes its `-relay` door through the router's reverse tunnel and is
reached at the site; a host's door published the same way is a player's own session
reached there, the router neither allocating nor authoring. The deployment this
leads to serves as many pods as the node has resources for (`-sessions-max`, ten
today) and routes every session beyond them. The tunnel carries a door already, and
the foundation stands: a name is resumed only by the run it was issued to, within
`NetworkTunnelResumeWindow`, so no published name shadows a pod's; one address
holds at most `-doors-per-address` doors; every connection placed is accounted for
as it closes (`session route closed`: session, door or pod, bytes each way,
seconds); and the listing's `limits` carry `sessions_max`, `doors` and `doors_max`,
so the site can say when the fleet is full and offer a hosted session instead.
Joins and creates are budgeted by account where a sign-in names one; a publication
is budgeted by address alone, and its `Resume` token is the last bearer secret on
the wire ([Auth and session design §2](auth-session-design.md#2-what-authenticates)).

What the feature itself needs, best shaped by deploying it step by step:
- `-host vif://<site>:7777` opening the door through the router rather than binding
  one, and the host's run reporting its state on the control connection (players,
  ceiling, scenario, whether it admits) so the router lists it without a `/health`
  read through the tunnel.
- The session's row in `/vif/api/sessions`, with a `kind` beside the pods', its
  lifetime (the control connection's, plus the resume window), and the create
  budget charged to the publisher's address as a pod's is.
- The publish ticket: the publish frame carries the run's key and, over TLS, its
  sign-in (`PublishRequest` is JSON, so a field is added without a protocol change);
  the router asks which account the sign-in names and refuses a publication by none
  (D12: publishing a door takes an account), budgets doors by account beside
  `-doors-per-address`, and a resume signs the router's nonce with the run's key in
  place of presenting the bearer `Resume` token, so no bearer secret is left on the
  wire ([Auth and session design §3.6](auth-session-design.md#36-admission-tickets-and-accounts),
  [§3.7](auth-session-design.md#37-budgets)). Implication: a `-relay` or hosted
  publication needs `:login` first; an anonymous run still joins through a published
  door.
- The front door's `-tunnels` ceiling split between relay doors and hosted
  sessions, or one budget for both, decided from how the first deployments fill.
- A page as the publisher. `DoorTunnel` dials through `link.Dial`, which speaks
  `wss://` in a page, but the browser route wants a session id in its path before
  the first frame, so a publish needs a path of its own. A hosting page must be
  exempt from `LEAVE_HIDDEN_MS`, holds only the embedded `main` until bundles are
  fetched over HTTP, and is never a successor door. Wasm runs host work about 3.9x
  slower than a native core: `main` with eight participants takes under half the
  page's thread and `td` does not fit, so the authority belongs in a Worker of its
  own.

### Project a correction outside the live lock

- Priority: P2
- Affected files: `internal/app/internal/corrector/snapshot_stage.go`, `internal/system/network.go`
- Guide: [Netcode design](netcode-design.md#less-work-under-the-live-lock)

An install compares the staging world's stores against the live ones in place and
writes the cells that differ, with no capture of either (`ReconcileSharedFrom`); the
deepest td install of netcode design §3.7 holds the live lock 8 ms, 7.7 of them the
write, down from 48. What the lock still holds is the projection: each projected
tick adds a simulation tick, 2.6 ms at td, so a repair 16 ticks behind holds it
past 45 ms. Moving it out: project to the live tick off the lock, then under it feed
what the barrier retained since the first feed, project the ticks the live world
advanced meanwhile, and write. Any out-of-lock or incremental design must keep
[Multi-player](multi-player.md) §3.3: fence membership, pending barrier crossings
not fed, the ledger settled before projection, the journal's place and mark, and no
live tick between the final projection and the write; the second feed needs a
retention watermark the barrier does not keep, since `RetainedAfter` selects by
tick and would feed the first feed's records again. WASM gains scheduling time, not
parallel CPU.

### Split combat telemetry by domain

- Priority: P3
- Affected files: `internal/system/combat.go`, `internal/status/key.go`
- Guide: [Domain model §7](domain-design.md#7-current-limits)

Combat's counters mix Shared results with per-instance ones, so two instances'
Shared combat outcomes do not compare directly. Splitting them is a telemetry
redesign rather than a domain-boundary fix.

### Offer lixen.com games on the start screen

- Priority: P1
- Affected files: `android/app/src/main/java/com/lixen/vif/StartScreen.java`,
  `android/app/src/main/java/com/lixen/vif/MainActivity.java`, `android/game/game.go`

Build it once the join refactor lands and its join matrix passes. Android opens on
a start screen with one choice, "Tap to start", a solo run; the site is reached only
from inside the run, through the menu's Multiplayer page (join a link, or request a
session from `https://lixen.com`, its default; `:register` and `:login` keep a
sign-in in the app's files, which a request to the site sends). Put a second choice beside the tap:
host a game at the site, as that request does, and/or join one of the running games
the site lists (`GET /vif/api/sessions`), so `game.Start` takes a join target. The
page needs nothing: the site's lobby (`/vif/lobby/`) is its choice, and its Play
link opens the page with `-join`, which joins at its start screen's tap. A run that
ends in an error, such as a refused join, shows it on the start screen, as the page
does; today Android finishes the activity and leaves the reason in logcat. Open:
host, list or both; a second screen or rows on this one; what a session that ends
returns to.

### Find games on the local network

- Priority: P2
- Affected files: `pkg/network`, `internal/network`,
  `internal/app/internal/membership/host.go`, `internal/mode/config_menu.go`,
  `internal/mode/commands.go`, `internal/paths/settings.go`, `internal/parameter`,
  `android/game/game.go`, `android/app/src/main/java/com/lixen/vif/MainActivity.java`
- Guide: [Multiplayer](multi-player.md#50-who-listens-who-authors-who-can-be-reached) §5.0

Players on one home network reach a host only by typing its address. A visible
host binds UDP on its game port (7777 beside TCP 7777), broadcasts a beacon every
2 s to each interface's directed broadcast and 255.255.255.255, and answers a probe
with the same beacon, unicast to its source. A run showing its game list binds that
port too and probes from it at open and every 2 s, so a reply reaches the port a
firewall rule already opens; a row is joined as `:join` joins, at the datagram's
source. The join, the session stream and the simulation are unchanged.
- Switch: "Visible on this network" on the Multiplayer page, `:lan on|off`, and
  `[play] lan` written back as `relay` is; off by default, it applies whenever the
  run hosts. A successor announces at its own door only if its own `lan` is on;
  the offer and the handoff record carry nothing.
- Datagrams: magic, version and kind. A beacon is at most 512 bytes of JSON:
  session nonce, name, scenario, players and ceiling, start wait, protocol and
  build, doors as scheme and port. A probe is padded to 512 bytes, so no reply
  outweighs it. Both decoders are bounded, strict and fuzzed.
- Host: one goroutine started and stopped with its port, reading an atomic
  summary so it takes no world or session lock. It answers only full-size probes
  from private, link-local or on-link sources, within a per-source and a total
  budget.
- Client: listens only while the Multiplayer page's list is open or a command
  asks, and stops once it joins. Rows are keyed by nonce and source, expire after
  5 s, at most 64.
- Android: a Wi-Fi multicast lock only while the run listens, as a client or a
  visible host, dropped while paused; broadcast addresses from Java, since
  `net.Interfaces` fails there (golang/go#40569); at targetSdk 37,
  `ACCESS_LOCAL_NETWORK` on the player's gesture, which a typed LAN `:join` needs
  too.
- Browser: no UDP, so its list reads "Nearby games unavailable in a browser". It
  plays a LAN game through a relay at a node it knows (the site's router, or a LAN
  run with a `ws://` door), or directly over `ws://` where the browser allows it
  (Chrome, after its Local Network Access prompt).
- Not chosen: beacon only (no instant list, no host on its own hotspot),
  mDNS/DNS-SD (a DNS codec, port 5353 shared with the system's daemon), a
  multicast group, a subnet scan, listing by the site.

### Watch a session as a spectator

- Priority: P2
- Affected files: `internal/network/session.go` (`RosterEntry`, `SessionOffer`),
  `internal/app/internal/membership/session.go`, `join.go`,
  `internal/engine/world.go` (`FollowLocalCursor`), `internal/mode/config_menu.go`,
  `cmd/vif`, `web/terminal.js`,
  `internal/app/internal/scribe/journal.go`, `internal/app/replay.go`
- Guide: [Domain design §1](domain-design.md#1-domains-and-identity),
  [Multi-player §1](multi-player.md#1-vocabulary-and-boundaries)

A spectator joins a session to watch: it holds no cursor and views the world from
one it picks, a person's or a bot's, among those whose owners allow it. It is
reached by a flag (`-spectate <link> [slot]`; `-watch` already means a watched
bot), a command (`:spectate <link> [slot]`, and `:spectate <slot>` or a key to
switch target inside the session), the Multiplayer page, and the page's launch
arguments as `-join` is passed, with a Watch link beside Play in the site's lobby.

Most of it exists. A dedicated host's coordinator is a cursorless roster entry
(`NoPlayerSlot`), and a joiner holds the sentinel until its own cursor exists, so
every "is this my cursor" test already answers no for one. The shared world
reaches it the ordinary way, a capture at join and then committed epochs and
corrections, not a journal, and it crosses nothing. Each cursor's owner-authored
values reach every instance through the syncs (D-13), so a HUD can show the
target's. What is new:
- A second kind of cursorless entry: admitted, paced like a guest, not counted in
  `MaxPlayers` (domain design §1 allows only the coordinator none today), never a
  successor, and not a stall for the start gate or the hold.
- A follow target apart from the input slot: the camera follows the target's
  cursor and the status bar reads its values; a target that leaves, or turns
  watching off, moves the view to the next allowed cursor.
- Consent: a game option ("Let others watch", on by default for now) on the
  Multiplayer page, saved with the rest of `Play` and carried on the roster entry
  as `Key` is, so the authority and every spectator read the same answer. Seats
  take their holder's.
- Capacity: spectators cost the authority a link and its corrections each, so a
  `-spectators N` ceiling, reported in the router's listing; relay doors can fan
  them out ([Multi-player §5.6](multi-player.md#56-relay-doors-and-the-router)).
- Admission, as the auth design left it: `AdmissionKind` already names `watch`
  beside `play`, on the wire by name, so a ticket's and a pass's kind tells a
  spectator from a player at every door, the router mints a `watch` ticket for a
  dial that asks (an `AuthRequest` field, the same frames), `TicketGate` admits the
  kind the door allows, and the pass a spectator is issued spares its redials the
  gate as a player's does; the keep-out list, the budgets by address and account,
  and the roster entry's `Account` and `Name` apply unchanged, and a dropped
  spectator is kept out as a player is
  ([Auth and session design §3.1](auth-session-design.md#31-primitives),
  [§3.4](auth-session-design.md#34-session-passes)).

A spectator sees the shared world, what any remote participant sees, centred on
the target: Player-domain state is never rebuilt on another instance (D-2), so the
target's glyphs, drains, nuggets, projectiles and loot stay on its run, and nothing
beyond admission and the follow target goes over the wire for it.

The journal's slot handling needs a review alongside. A journal records its slot
from `LocalSlot` at the opening (`JournalCapture.Slot`, `JournalAnchor.Slot`),
its replay binds input there (`Scribe.OpenedLocked`), and later installs bind by
identity (`RebindCursorRoster`). Three cases want a test: a journal a joiner opens
before its arrival, which may now record `NoPlayerSlot` and must bind when the arrival
or a correction brings the cursor; a spectator's journal, whose target switches are
presentation and must be journaled local events so its replay shows the same view;
and `:journal start <slot>` on a spectator, which journals its own run, since a
remote player's run cannot be journaled. A bot-only session is the natural thing
to watch: [Run a scenario with no players](#run-a-scenario-with-no-players).

## Netcode

### Price the log's and journal's 50 ms sync

- Priority: P3
- Affected files: `internal/vlog/vlog.go`

With `-log DEBUG` and a journal open, the same session's terminal ran 14% system CPU
and about 6,000 block writes a second (`proc.io_write_s`) for some 80 kB/s of
records: both files sync every `flushIntervalMs` (50 ms). The journal's crash
promise needs it; the log's does not. Measure a log that syncs on its own cadence,
or only on WARN and above.

### Read a touch client's uplink

- Priority: P3
- Affected files: `internal/mode/router.go`, `android/game/view.go`,
  `internal/system/network.go`

`network.link_bps` is the rate the far end reports receiving this instance's bytes
at, so a client reading two to seven times another's on the same link, as the
Android client did against a terminal at the pod, is sending that much more unless
`network.link_saturated` says the link was the limit. The candidate is input shape:
a drag moves the cursor to every cell the finger crosses (`Router.moveMouseCursor`,
`PushPointerMove`), a crossing each, where a key moves one cell a press, and god
mode multiplies the actions. Compare the two clients' `network.sent_epoch_bytes`,
`network.sent_sync_bytes` and `network.crossings_sent` over one window, and
`network.pace_late_ticks` for a client the OS throttles; if the epochs carry it,
decide whether a drag's intermediate cells are a move each or one move to the cell
the tick ends on.

### Give a lone host the authority's one-tick lead

- Priority: P3
- Affected files: `internal/system/network.go`

`driveOwnLead` returns while `PeerCount()` is 0, so a host with no guests keeps the
3-tick default (`NetworkBarrierDelayTicks`) where `ownLead` gives an authority 1,
and a guest's arrival lowers it only after the 300-tick renegotiation window. Each
crossing a host playing alone makes lands two ticks later than it needs to. Push
the authority's lead when hosting starts, or let `driveOwnLead` run for an
authority with no peers.

### Let the selective-apply test see an adopted owner-authored cell

- Priority: P2
- Affected files: `internal/app/migration_test.go`

`TestSelectiveApplyKeepsItsExclusionsAcrossAHandoffAndARelay` lowers the
follower's energy so a correction adopting it would show, but owner sync hands
the successor the new value first and the receiver compares its tick-close
capture, so no correction carries a differing cell: the test stays green with
every D-13 guard off at once (`applyShard`'s owner check, the encoder's
exclusion, both owner-authored cursor sets, `RebindCursorRoster`'s restore).
Perturb where the successor's mirror still differs at a tick close, and show the
test then fails with the guards off.

## Session membership

A suggestion for the words, not a decision: a *session* is one world timeline under
one authority — a fleet pod, a terminal host, any instance — and its participants,
and solo play is a session of one. Commands then move participants between sessions:
`:open`/`:close` in place of `:host`, since every instance already is its own
session's authority; `:join`/`:leave` for a participant; `:drop` for an authority
removing one; `:merge`/`:split` for whole sessions.

### A passphrase prompt in the TUI and the launcher

- Priority: P3, in a session of its own
- Affected files: `internal/mode`, `internal/app/loop.go`, `web/terminal.js`,
  `web/index.html`
- Guide: [Auth and session design §3.5](auth-session-design.md#35-passphrase-doors),
  [§3.6](auth-session-design.md#36-admission-tickets-and-accounts)

A dial that meets a passphrase door with none is refused with the advice to join
with one, and a player retries with `:join <target> <passphrase>`: the refusal is
the prompt. A prompt proper would ask in place, in the command line's form on a
terminal and in the launcher's join row on a page, and dial again with the answer;
the same form takes `:login`'s name and password, which today ride the command
line and so the terminal's history. Implication: the account commands become
usable on a page, where a password on the command line is not.

### Tighten what a relay and a successor are trusted with

- Priority: P2, in a session of its own; every mechanism it audits is in place
- Affected files: `internal/network`, `internal/app/internal/membership`,
  `internal/converge`, `tool/vif-router`
- Guide: [Auth and session design §3.4](auth-session-design.md#34-session-passes),
  [§3.5](auth-session-design.md#35-passphrase-doors),
  [Multiplayer §5](multi-player.md#5-membership-topology-and-authority-continuity)

The premise: a participant, once admitted, is trusted with the session. A relay
carries others' crossings and runs the admission exchange for newcomers; a successor
takes the term, signs the handoff record and issues passes; both hold the passphrase
door's verifier, with which a participant can test guesses offline at Argon2's cost
and stand in for the door to others. A host that took over through a router-published
door cannot publish again through the router, so its session is reached through
relays alone. The scope: list what each role can do that the admission did not grant
it, decide which of it needs a check (the authority confirming a relay's admission,
a successor's record carrying no more than the roster lets it, the verifier's reach),
which needs a limit, and which is accepted as the price of play without a server; then
plug the holes found, one PR each.

### Drop a participant and drain from the router

- Priority: P3
- Affected files: `tool/vif-router` (a loopback listener, `allocator.go`),
  `internal/app/internal/membership/host.go`, the pod's `/health` neighbour,
  `deploy/guest/vif-router.env`, `deploy/update.sh`
- Guide: [Auth and session design D16](auth-session-design.md#0-decisions),
  [§3.4](auth-session-design.md#34-session-passes),
  [Session fleet §9](kubernetes-fleet.md#9-the-session-routes)

`:player drop <slot>` drops a player with its bots, or one bot, keeps the dropped
keys and account out of the session at every door and tells the dropped person, who
plays on alone as after `:leave`; a dedicated host has no console to type it on. The
operator's authority is a loopback listener of the router's (`-operator-listen`),
reached over ssh and nothing else: the sessions with their participants by slot and
subject; a drop of one by slot, key or account, carried to the pod on an operator
port the network policy admits from the node alone, where the pod runs the drop
its console would; and a drain that stops creates, lets the sessions end and
reports when the fleet is empty, which `update.sh`'s empty-fleet gate reads
instead of polling the listing. Implication: no operator account exists, so the
listener binds loopback and takes no credential; its routes are not under
`/vif/api/` and the edge never publishes them.

### Name a fleet session's creator to its pod

- Priority: P3
- Affected files: `tool/vif-router/server.go`, `tcp_proxy.go`,
  `internal/network/admission.go` (`AdmitCreate`), `internal/app/internal/membership`
- Guide: [Auth and session design D12, D16](auth-session-design.md#0-decisions),
  [§3.6](auth-session-design.md#36-admission-tickets-and-accounts)

A fleet session is created by whoever posts to `/vif/api/sessions`, and the pod
knows nothing of them: on a dedicated host nobody may `:player drop`. The create
ticket: when an account creates a session, the router mints a ticket of kind
`create` naming the account and hands it out with the link; the creator's first
dial shows it beside its play ticket, the pod records the account as the session's
owner on the roster entry, and the owner may drop a participant, as a host's console
can, and nothing more. An anonymous create names no owner, and the operator's
route (above) stays the only other authority. Open: whether an owner's right
outlives its own departure, and whether a later `:login` can claim an unowned
session.

### Take the auth readings

- Priority: P2
- Affected files: `tool/vif-router/tcp_proxy.go` (`splice`, `stallBounded`),
  `script/`, `doc/auth-session-design.md` §8
- Guide: [Auth and session design §4](auth-session-design.md#8-ten-thousand-routed-players-on-one-machine),
  [D10](auth-session-design.md#0-decisions)

What the design projects and has not measured: the router holding 10,000
connections through a stand-in door on a second process, its RSS and CPU before
and after moving the splice's stall deadline off the writer so TCP-to-TCP copies
take the kernel's `splice(2)` path, the admission exchange's cost per connection,
and `-route-max` and `routePlacing` tuned from them; the passphrase door's Argon2
(64 MiB, three passes, one lane) timed on a page and a phone, since a dial pays it
once a join; the Android binary's growth from `lixenwraith/auth`, which the native
and wasm readings put under one percent. The identity service's own readings (RSS at
10,000 verifiers, the launch-day introspection rate) are its repository's todo.
Implication: the budget in §8 stands as a projection until these are in; a reading
that misses it changes `-route-max`, `MemoryMax` or the splice before anything else.

### Refresh the architecture and development documents

- Priority: P2
- Affected files: `doc/architecture.md`, `doc/development.md`

Both were last read whole before the netcode, kinetics, audio and menu work; the
session layer's parts were brought current with the auth design. The rest wants a
pass against the code: the containers and control flow in `architecture.md` §2 to
§11 against `package-map.md`, and in `development.md` the build workflow, the
generated code, the CLI tables against `cmd/vif/usage.go`, the validation sweep
against `Makefile` and `script/test.sh`, the platform matrix and the source map.
Implication: a newcomer reads these two first, so a stale claim there costs more
than one in a reference document.

### Keep a Wraith's stats in the session

- Priority: P2
- Affected files: `internal/app/internal/membership/place.go`, `identity.go`,
  `host.go`, `internal/system/cursor.go`, `internal/system/network.go`
- Guide: [Multi-player](multi-player.md#54-wraiths) §5.4

A player coming back to its Wraith brings its cursor's stats itself
(`event.CursorRecord`: energy, heat, health and loadout): the last owner-state sync
its run made in that session, frozen once the run leaves it, so play in a fork since
never comes back. This is a compromise. The values are owner-authored
([D-13](domain-design.md#d-13--owner-authored-shared-values-have-one-writer)) and a
Wraith gives up everything the world reaches it through, so neither the host nor the
shared world keeps them, and the session takes the returning run's word for them.
`CursorSystem.restore` writes them as brought, held to none of the ranges a sync is
(`NetworkSystem.stateInRange`), so a record past one stands on every instance, its
owner's syncs dropped, until the owner's own systems move the value back inside.

The fix shares some data of every cursor, so the session holds what each Wraith
resumes with and a successor inherits it. That moves those values off the one-writer
rule (D-2, D-13) for every system that writes them owner-side, and costs bandwidth,
which is why it waits. A cheaper step, a Wraith keeping the copy the sync committed
at its departure tick, is outside the hash and the repair (D-13), so nothing holds
the instances' copies equal.

The record is built to be replaced: one type, made in one place
(`NetworkResource.OnOwnState` into the place keeper) and read in one
(`CursorSystem.restore`), carried by the arrival crossing the coordinator fills
(`ParticipantJoinedPayload.Record`). What would close it: decide which values the
session holds, have the coordinator fill the arrival from its own world instead of
the joiner's report, and drop the record from the keeper and the report.

### Place an arrival where its scenario places a cursor

- Priority: P2
- Affected files: `internal/system/network.go`,
  `internal/app/internal/membership/join.go`, `internal/engine/resource.go`

A participant's cursor lands on the map centre whatever the scenario's boot request
asked: `addParticipant` and a joiner's roster both push `Center: true` with the
template's heat and energy, so a simon guest starts at the centre and not below the
buttons where the boot cursor stands. Anything hostile a scenario puts at the centre
strikes each arrival on its first ticks. Keep the boot request's placement beside
the heat and energy template (`PlayerResource.SetInitialResources`) and spawn
arrivals from it, or let a scenario name spawn cells by slot.

### Merge two sessions

- Priority: P3
- Prerequisite: a redirect message, "rejoin at this link", which the split shares

Two sessions, each under its own authority, become one by keeping one world: B's
authority redirects its participants to A's link, they rejoin A as fresh players, and
B's world ends — `:leave` and the drop above are the same move for one participant.
Merging two worlds is not planned: two timelines share no state to reconcile, which
is why a higher term from a fork is refused today.

### Split a session under a second authority

- Priority: P3
- Prerequisite: the redirect message, and an authority seeded from a live world

For a session that outgrows its authority — a large roster or a heavy scenario
lagging — twelve participants under A become seven under A and five under B. B is a
participant at first, ideally a fresh fleet pod. A hands B its current world under a
new session identity rather than a new term, redirects the five, and the two worlds
diverge from there. Late joiners already install a mid-run world; what is missing is
the redirect and an authority, for a pod a router request, that starts from a
transferred world instead of a scenario.

### Run a scenario with no players

- Priority: P3
- Affected files: `internal/app/internal/membership/session.go`,
  `internal/app/serve.go`, `wad/scenario/base/td`
- Guide: [FSM and configuration](fsm-and-configuration.md#3-root-schema) §3

`min_players = 0` is accepted and, like 1, waits for one player. A game that runs
with nobody in it, such as td's two sides attacking each other's towers, needs a
lobby quorum of 0 so a server starts without a guest, `holdVacant` parking only
below the scenario's minimum, the ownerless boot cursor dropped without a park, and
spawns that do not anchor on a cursor: the gold cycle fails and retries with none.

## Combat

### Let mounted weapons strike more than cursors

- Priority: P3
- Affected files: `internal/system/mount.go`, `internal/profile/combat.go`

A mounted ray destroying walls and glyphs, mounted weapons striking other species, and each instance's own drains; Shared outcomes must resolve from Shared geometry, not a Player-domain shot.

### Let cursor weapons strike other cursors

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/system/bullet.go`, `internal/system/pulse.go`, `internal/system/interaction.go`, `internal/profile/combat.go`

PvP: a hit on a remote cursor crosses as its impact and the victim's owner applies it, as `strikeCursor` does for mounts.

A cursor then takes what an attack family does rather than a fixed `CursorDamage`, and a mount's pulse already carries its family for it (`PulseSpec.Attack`, `Charged`, set per mount), so mounted weapons gain the same effects:
- Profiles with `CombatEntityCursor` as the defender, a row per family that may strike a cursor; `strikeCursor` resolves one, a mount striking as its host's combat row.
- Owner-authored cursor statuses (D-13): a stun the owner's input and its D-18 prediction obey, never refreshed while it runs; a burn ticking heat on the owner each period.
- Knockback as a push: the owner moves its own cursor along the impulse through `PushCursorMove` to a free cell, or a Shared pulse re-derives the move on every instance, as the cave rock's push does.
- Per-attacker, per-family immunity on the cursor, as species keep it, so a sustained pulse cannot stun-lock.

### Place a pulse on a map cell

- Priority: P3
- Affected files: `internal/system/mount.go`, `internal/event/payload.go`

`EventMountRequest` naming a cell and no host places a bare Shared anchor carrying the mount, so a scenario can lay a sustained, hidden pulse over part of the map, such as one that pushes cursors back once they take pushes; a level clear and a request remove it.

### Let a scenario shape the cursor's pulse

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/event/payload.go`

A request overriding the disruptor's `PulseSpec` for every cursor field by field, as a mount's is, carried in the weapon system's local save as the gun's aim is (`EventGunAimRequest`).

### Let a scenario mount and shape a shield

- Priority: P3
- Affected files: `internal/system/shield.go`, `internal/system/interaction.go`,
  `internal/event/payload.go`

A request naming an entity, a radius and a colour, so a scenario mounts a shield on
a tower or a wall and sizes or tints it from what it watches, as a cursor's energy
turns its shield on and off and sets its polarity. `ShieldComponent` carries both,
the capture with it, and any shield draws at them; missing are the request, which
the entity's owner applies as a cursor's owner writes its own shield (D-2), and a
contact rule: `CursorContactAt` reads cursors' shields alone and only a cursor's
drains energy, so a mounted shield guards nothing yet.

## Species

### Play test the death visuals

- Priority: P1
- Affected files: `internal/parameter/effect.go`, `internal/parameter/kraken.go`,
  `internal/parameter/visual/spirit.go`, `internal/parameter/visual/kraken.go`,
  `internal/system/composite.go` (`bodyOf`, `leaveBody`, `burstShell`),
  `internal/system/transient.go` (`updateKrakenBurns`)
- Design: [ECS §7](ecs-and-events.md#death-effects)

A species' end leaves an effect, and none has been judged in play. What to look at,
each with the smallest change that would answer it:

- A correction inside an effect. An effect is its instance's alone and outlives
  what raised it, so a species a correction brings back stands again while its
  scatter, flash or fade runs on under it, for half a second at most. Only a
  Kraken's burn ends as its Kraken returns (`KrakenBurn.Entity`). Play a fight over
  the divergence harness's four link shapes. If a body back over its own effect
  reads badly, a body's cells need a batch of their own naming the dead header,
  which the effect's system drops once that entity stands again, as
  `updateKrakenBurns` does.
- The timings, each a constant: a scatter's 350 ms and reach
  (`SpiritScatterDuration`, `SpiritScatter*`), the Kraken's burn of a second and
  how hard its legs thrash, draw in, crinkle and swing (`KrakenBurn*`), and how
  far apart a burst's shards stand (`ShellShardSpacing`).
- A chipped cell fades in the color its kind goes critical in, not the color it
  showed, so a healthy cell an attack takes whole turns that color as it goes.
  `bodyOf` reads one ground for the body, because a member's own shade is its
  renderer's (hit points, gradient, dither); reading it would take that shading as
  a shared read, as a Kraken's shape is.
- A tower's burst is in its type's glow color whether or not the tower was active.
- A glyph fading alone, a flash and a scattered part dim toward black and not
  toward what is under them, so their last frames are darker than the background.
  Mixing toward `RenderBuffer.BackgroundAt` would end them in it.
- A storm circle's end was offered a collapse instead of the burst: the dying
  circle shrinking to a bright core, about 40 lines in the storm renderer, with
  nothing for a pylon or a tower. The burst was chosen as one mechanism for all
  three. The collapse is worth building if a circle's burst reads too much like a
  pylon's.

### Show a species' machine state in a debug overlay

- Priority: P3
- Affected files: `pkg/machine/machine.go` (`Graph.States`, `Graph.Flags`),
  `internal/render/renderer`

`Graph.States` lists a graph's states and `Graph.Flags` a state's flags, for an
overlay that labels each instance of a species with its state and progress, as
`:graph` draws a route graph. Nothing reads either, and only tests read
`Graph.Name`: build the overlay, or delete the two reads until one is wanted.

### Push a data species with the wind

- Priority: P3
- Affected files: `internal/system/environment.go` (`applyPlanarWind`)

`applyPlanarWind` accelerates each mobile species by its profile mass, and a data
species is none of them, so a wind leaves a raider where it stands though its
prototype has a `mass`. The smallest fix ranges the `Behavior` store in the Shared
pass with each prototype's mass from `Resources.Behaviors`, skipping a massless one.

### Count a data species' stalled transitions

- Priority: P3
- Affected files: `internal/behavior/compile.go`, `internal/system/behavior.go`

A program's transition whose candidates all fail retries or holds without a trace,
where the swarm counts its in `swarm.transition_stalls`. The swarm's observer is a
method value bound in `NewSwarmSystem`, reaching its counter through the system:
one graph, one counter. A program's graph is compiled before the generic system
exists, with no observer, so a per-program `behavior.<program>.transition_stalls`
needs an observer that reaches a counter bound later, in `NewBehaviorSystem` with
the lifecycle rows.

### Let a player mechanic or a program fuse composites

- Priority: P3
- Affected files: `internal/system/fuse.go`, `internal/event/type.go`,
  `internal/system/behavior.go` (`EmitEvent`)
- Design: [D-3](domain-design.md#d-3--cross-the-smallest-shared-outcome)

A fusion takes unit composites only from a Shared request, one a region raises on
every instance, each consuming them alike. A participant's own request, pushed
player, takes its drains alone and fails naming a composite, and a program's
`EmitEvent` takes no stamped event, so neither a player mechanic nor a program can
fuse composites. A player mechanic would cross the Shared outcome as a bus artifact
naming the composites and the target's landing, barrier-bound since it retires
Shared entities; a program's request would push the request Shared, as a region's
does.

## Environment

### Carry the cursor with the text it is on

- Priority: P2
- Affected files: `internal/system/environment.go`, `internal/system/cursor.go`

Glyph drift moves the line under a cursor but not the cursor, so a player chases
moving text. Carrying it would move each local cursor with the glyph it stands on,
as a Player-domain cursor write per layer step; a peer's cursor would follow
through its owner's move, never through the drift.

### Decide what a delete removes under a drift stack

- Priority: P2
- Affected files: `internal/system/typing.go`

`handleDeleteRequest` removes every player glyph in its range, including farther
layers hidden under a cell's top glyph, while typing, rendering and motions act
on the top glyph only. Deleting only each cell's `TopGlyphAt` would make a
delete remove exactly what is shown.

### Refresh motion markers as glyphs drift

- Priority: P2
- Affected files: `internal/system/motion_marker.go`

Markers are recomputed on cursor movement and mode changes, so under a drift they
sit on cells the glyphs have left. Recomputing after a drift step, which the
environment could announce as a local event, would keep them on the text.

### Widen what drifts

- Priority: P3
- Affected files: `internal/system/environment.go`, `internal/parameter/environment.go`

Composite members (gold), particles, nuggets and species stay put; a side-scroller
may want some of them carried with the nearest layer. Decay and blossom change a
glyph's level, so a glyph they touch changes layer and speed mid-flight. In 256
colours normal and bright glyphs share one console entry, so only the dark layer
reads as farther; a distinct attribute for bright would separate the three.

## Rendering

### Open a console of the command and message history on backtick

- Priority: P1
- Affected files: `internal/asset/input/keymap.toml`, `internal/mode/router.go`,
  `internal/mode/commands.go`, `internal/engine/game_context.go`,
  `internal/render/renderer/status_bar.go`, a console renderer and its row in
  `internal/manifest/definition.go`, `android/app/src/main/java/com/lixen/vif/GameView.java`,
  `web/terminal.js`, `doc/input-and-modes.md`, `doc/rendering.md`

Backtick, unbound in normal mode today, drops a panel from the top of the screen
as Quake's console does: several rows of the messages the run posted and the
commands typed, newest at the foot, scrolled back with the keys an overlay
already takes, over the command line; backtick again or Escape raises it. The
status bar keeps showing the newest message, rising onto the items' row when it
needs two lines, and cuts a longer one: Android's 85-character log path at 18 dp
in portrait, and anything past about 15 characters in the one-row strip above its
keyboard at the default zoom. The console is where such a message reads whole, on
every host alike. `GameContext` keeps only the message shown (`statusMessage`),
so it needs a bounded history of them (kind, wall time, text); the router's
256-command ring (`cmdHistory`) already holds the commands. Open for its own
session: whether the console's line is command mode's or a second one; how a
touch host opens it, as no soft keyboard shows backtick on its first page (a key
strip button, a gesture, a tap on the bar's message); what a backtick is in
insert mode, where it types; whether a solo run pauses while it is down, as the
menu does; its height and what a resize does; and whether the history outlives
the run in the state directory, as a shell's does.

### Configuration menu follow-up

- Priority: P2
- Affected files: `internal/mode/config_menu.go`, `internal/paths/settings.go`
- `[play]` persists the audio channels, sfx volume, auto-fire, mouse, pointer
  following and the HUD, and a scenario change carries them over. What `:new`
  keeps beyond those (rate, pins, standing breakpoints) a restart or scenario
  change still drops. A Music volume row under Sound, stepped as Effects volume
  is and kept as `[play] music_volume`, needs a music gain in the mixer with an
  authoritative readback first; the row then follows `sfx_volume`'s shape on
  every host, the native menu's stepper included. Keymap, backend, paths and
  colour depth remain startup settings; `:d break` stays on the command line,
  since a row cannot take its arguments.

### Use sixteen console backgrounds on Linux

- Priority: P3
- Affected files: `internal/render/console.go`

The framebuffer console shows a bright background under SGR 5, which vgacon blinks instead; telling them apart would give Linux FreeBSD's sixteen backgrounds.

### Move the browser and Android frame pipeline into terminal

- Priority: P2
- Affected files: `pkg/browser`, `pkg/glyph` (`Frames`, `Raster`, `Texture`),
  `android/game/view.go`, `web/terminal.js`, `web/worker.js`, lixenwraith/terminal

What turns a cell frame into what a page or an Android view draws (the frames a
drawer takes, the texture, the raster fallback, the page's bridge and its key and
pointer events) lives in vif. Move it into lixenwraith/terminal as backends of its
own, so another program renders in a browser or on Android through the same
pipeline without wrapping vif; vif then takes them from terminal. For its own
session.

## Audio

The audio library's own items are in [its todo](https://github.com/lixenwraith/audio/blob/main/doc/todo.md),
and those of stapes (formerly soundlab), the rip's among them, in [its
own](https://github.com/lixenwraith/stapes/blob/main/doc/todo.md).

### Bring stapes to Android (stapes mobile)

- Priority: P1
- Affected files: `lixenwraith/stapes` (`cmd/stapes`), `android/game`,
  `android/app`, `doc/multi-platform.md`

Stapes runs on Android in-process, as the game does, over the same engine and
the user root's `audio/` documents, so a pattern written on the phone plays in the
phone's game. The foundation is in place: the `stapes` package holds the session,
documents, verbs and TUI, `NewSession` takes the host's speakers as its sink
(`Options.Sink`), and `RunTUI` runs over any `terminal.Terminal`, the host posting
`EventClosed` to end it; the Android host already turns taps, drags, keys and the
system keyboard's text into `terminal.Event`s and flushes cell frames through
`glyph.Frames`. What remains:

1. A `terminal.Terminal` over the Android view (its grid as `Size`, its event
   queue as `PollEvent`, `glyph.Frames.Flush`) runs it, started by a second bound
   entry beside `game.Start` that hands `NewSession` the `Speaker` as its sink;
   the start screen offers stapes beside the game.
2. Touch: the TUI reads keys alone. A tap hit-tests what was drawn: a key of the
   keyboard plays its note, a step toggles, a tab or a pane takes the keys, a mixer
   row mutes its slot, a browser row selects. A strip of buttons stands for the
   keys a phone lacks (record, play, delete, save, new, edit, tempo, Tab, Esc), and
   the system keyboard serves `:` and the prompts, as in the game.
3. Size: the views need 60×16 cells. A phone held landscape fits at the default
   cell; portrait needs the pinch zoom out, or a keyboard of fewer keys.
4. Files: the verbs name paths, which reach stapes' root and the user root's
   `audio/` in the app's storage; a `.mid` or a `.wav` another app holds comes
   through the system's picker, copied into the user root's `audio/` first, and
   a written file goes back out through it ([Multi-platform
   §8](multi-platform.md#stapes-files)).

### Rip live on Android

- Priority: P2
- Affected files: `android/game`, `android/app`, `internal/app`,
  `lixenwraith/audio` (`RipStream`)

The library rips a recording as it plays (`RipStream`: blocks of samples in,
each bar frozen once two follow it from the sixth bar on, `Final` the whole rip
once the recording ends). What remains is the host: the microphone's blocks
into the stream, the frozen bars shown a few seconds behind the music and
revised until frozen, a `Final` the player asks for, and the song saved into the
user root's `audio/` as a rip of a file is ([Rip design
§7](https://github.com/lixenwraith/stapes/blob/main/doc/rip-design.md#7-plan)).
The smallest close: the stream over a file first, in stapes, measured against
the offline rip of the same recording.

The live view is to show the rip being made: a scrollable stack with the
recording's spectrum on top, each stage under it (the planes the models are
given, the pitches, strikes and drums they hear, the instrument, what the
decoder keeps and drops, the open bars) and the note lines at the bottom. The
stream gives the bottom lane alone (`Bars`): its frames sit in a private window,
the open bars' notes go after each read and the decoder's choices are never
kept, so the view waits on three reads from the library: the frames heard since
a frame, the open bars as last decoded, and a read's candidates with what became
of each. Measured through the trained models on twelve recordings (stapes'
audit of 9 November): the stream finds 95 in a hundred of the offline rip's
notes, but reads its bar grid on three recordings only, since tempo, meter and
phase hold from the first six bars (five read at another tempo, a rubato piano
take keeps 5 to 38 in a hundred of the offline downbeats, Billie Jean's intro
sits half a bar off); it costs three times the offline rip, each model run
three times over where its half-second steps overlap, and its worst block took
a second of CPU, so it runs off the audio thread.

### Render a conductor timeline offline

- Priority: P3
- Affected files: `cmd/vif-log`, `internal/system/music.go`

`RenderTimeline` plays a timeline of slot patterns offline and stapes' `song`
writes a `SongDef` through it; the conductor's own timeline, patterns by tier from an APM
trace, is what still lacks a path to it. The offline conductor render that
compared bass voicings for the music-smoothing review, an APM timeline through
the sequencer with no device, is a test-only
harness in a scratch repository. `AudioEngine.RenderMusic` now renders placed
patterns offline; a verb of vif's, since stapes imports nothing of the game,
would drive an APM timeline through the conductor's tier and tempo policy over
it and write the WAV.

### Play a song in the game

- Priority: P3
- Affected files: `internal/system/music.go`

The conductor plays tiers, not bars. A level that wants a fixed piece would play
a `SongDef` through the sequencer as stapes conducts one, each bar's patterns
scheduled while the bar before plays.

### Add binaural beats to the music

- Priority: P3
- Affected files: `lixenwraith/audio` (`mixer.go`, `engine.go`, `command.go`,
  `doc/audio.md` §2 and §6); vif `internal/parameter/music.go`,
  `internal/system/music.go`, `internal/paths/settings.go`,
  `internal/mode/config_menu.go`, `internal/mode/router.go`, `internal/event`,
  `doc/audio.md`, `doc/config-menu.md`, `doc/vif.6`
- Guide: [Audio §2](audio.md#2-adaptive-game-conductor), library
  [§2](https://github.com/lixenwraith/audio/blob/main/doc/audio.md#2-stream-contract)

The music's tempo already follows the player's pace (100 to 200 BPM off the
five-second APM). A binaural beat would add a sensation of that pace: a tone
of `f − Δ/2` in the left ear and `f + Δ/2` in the right, heard as a beat at
`Δ`. No shipped pattern, test song or rip carries one. Off by default, as a
setting with three modes:

- **off**: today's output, byte for byte (L equal to R);
- **fixed**: a preset's `Δ`, whatever the pace; presets in one table in
  `internal/parameter` (a row each), first rows `relax` 10 Hz (alpha), `focus`
  15 Hz (low beta), `hyper` 25 Hz (high beta);
- **dynamic**: `Δ` follows the APM, by an algorithm still to choose (below).

What the docs and the code say today: the stream is stereo end to end, every
backend opens two channels (`pacat`, `pw-cat`, `aplay`, `sox`, `ffplay`, the
WAV sink), Android's `AudioTrack` is `CHANNEL_OUT_STEREO` and the browser's
worklet writes `left` and `right` apart. The mix is not: the music and sfx buses
are one `float64` channel, and `floatToBytes` writes each sample to both
(library §2, "the mix is mono, written to both"). So nothing of the hosts
changes; the mixer's last stage does.

Where the beat is made, each a choice with what it costs:

1. **A stereo bed in the mixer** (recommended). A generator the mixer owns, two
   phase-continuous sines summed into L and R after the music bus, before the
   pause ramp and the limiter, which becomes stereo-linked (its envelope the
   larger of the two channels). `floatToBytes` takes two inputs; nothing before
   the mix changes: voices, tracks, the sequencer, its crossfades, reveals,
   fills and draws stay mono and unaware. Cost: two oscillators a sample, a new
   command (`SetBinaural{Carrier, Beat, Level}` on `AudioEngine`, slewed inside
   the mixer so a change never clicks), the music tap written in stereo so `-mw`
   records what plays. Smallest change, one owner, every host at once; the bed
   is a tone under the music rather than part of it.
2. **One more track.** A pattern track whose voice holds a note per ear. It reads
   as one track in the bank, but a track renders into the mono music bus, so it
   still needs option 1's stereo output and a stereo voice besides; and in a slot
   it is redrawn on phrase downbeats, faded by crossfades, rebuilt by reveals one
   track a bar, replaced by slot 2's fill and moved with the group every four
   phrases, each a gap in the beat, while `Δ` changes on the step grid, not
   continuously. Kept out of the draw it becomes a fifth slot, which is option 1
   inside the sequencer. Its one gain: a group could author its own carrier in
   `music.toml`.
3. **A stereo music bus.** The sequencer renders L and R through every slot,
   player and voice, and the beat is an ear offset of a constant `Δ` Hz on a
   tonal voice (the pad), so every partial pair beats at `Δ` inside the music.
   Most musical, and it brings panning per track with it, which the engine lacks
   ([Can it run Doom](can-it-run-doom.md) notes `PlayBuffer` has no pan); but it
   touches the whole pipeline, doubles the voices' render cost, the offline
   renders (`RenderMusic`, `RenderTimeline`) and stapes' with it, and the beat
   sounds only while the pad plays.

The carrier `f`: binaural beats are heard best on carriers of a few hundred
hertz and fade above about 1 kHz, and `Δ` blurs into roughness above about
30 Hz. Tuning `f` to the harmony's root (default E2, 82 Hz, so E3 or E4 at 165
or 330 Hz) keeps the bed consonant with the key; it then glides on a key change.
A fixed `f` is simpler and may clash.

Dynamic `Δ`, candidates, each glided at most about 0.5 Hz a second:

- **follow**: `Δ` rises with the APM on the tempo's own square root, from
  `relax` at rest to `hyper` at the ceiling. Reinforces the pace, as the tempo
  does; it may also feed a frantic player more.
- **tempo-locked**: `Δ = k·BPM/60`, so it inherits the conductor's curve and
  slew with one constant; `k = 8` (a 32nd note) runs 13.3 to 26.7 Hz over 100 to
  200 BPM, all beta, `k = 4` 6.7 to 13.3 Hz, theta to alpha. Coherent with the
  rhythm, but it never leaves its band.
- **by tier**: one `Δ` a tier, a row each in the preset table, through
  `TierTracker`'s hysteresis; steps the glide smooths, five values to tune.
- **counter**: `Δ` falls as the APM rises, steadying a busy player against the
  rising tempo; the two may read as at odds.

Decided with the implementation: whether the bed rides the music bit of the
audio mask and the `music` system's gate (simplest, no new bit) or a toggle of
its own; whether it ducks under sfx (amplitude only, `Δ` holds either way); a
level of its own or a fixed one under the music.

vif's side, wherever the beat is made: `[play] binaural = "off" | "fixed" |
"dynamic"` and `binaural_preset`, validated in `Play.check` as `auto_fire` is and
saved by the one settings rule; a Sound row in the configuration menu
(`kindChoice`) and the native menu's, the preset row live under fixed; a
command and an event pushed `OriginDevice` as the mute is, so the journal
carries none and a replay plays the viewer's setting, dynamic following the
replayed APM; `MusicSystem` sends the target each update and `music.binaural`
shows the mode and `Δ` on the music card. The beat needs headphones: through
speakers the ears share both tones and it becomes a quieter monaural beat, and a
phone's single speaker loses it; the row's description says so, the same on
every host. The evidence that a beat entrains anything is mixed: the manual
describes it as a sound, not an effect.

Tests, one per rule: off renders L equal to R; on, each channel's frequency
(Goertzel) is `f ∓ Δ/2` and a change of `Δ` keeps the phase continuous; the
linked limiter holds both channels under its ceiling; the dynamic mapping in
`parameter`'s music test. The audio half bumps the library's minor version,
which vif then requires; stapes could audition the bed with a verb, an item of
its own todo. New terms (binaural beat, bed, carrier) go in both term indexes
when they land.

## Engine

### Give the composite lifecycle methods a user

- Priority: P3
- Affected files: `internal/system/composite.go`

`CompositeSystem` carries `handleEmptyComposite`, `CreateHeader`, `AddMember`,
`DestroyComposite` and `GetAnchorForMember`, designed for building and tearing
down composites at run time, but no system or scenario uses them. A species or
scenario that assembles composites would; it reaches them through events, since
another system may not call them directly.

### Trim what a tick spends outside the position index

- Priority: P2
- Affected files: `internal/system/interaction.go`, `internal/engine/store.go`,
  `internal/engine/world.go`, `internal/event/queue.go`
- Guide: [Position §5](position-package.md#5-costs)

A td tick with sixteen cursors on its 500×250 map (`TestTickProfileTD16`, sixteen
runs merged) is led by costs outside the index, the math and the physics. Each
share is measured and none of its code changed:

- A species' contact with the cursors (`CheckCursorOverlaps`, `overlapOf`), 15%:
  every entity reads each roster slot's cursor and shield again before it tests
  its cells against them. Reading the roster once a tick is the smallest step.
- The stores' entity lookup (`sparseIndex.get`), 9.5% across the systems, a
  third of it under a position read.
- The route graph (`RouteSolver.Compute`), 12%, and the maze a reset lays again
  (`executeBatchSpawn`), 15%, a third of that `World.componentMask`, a Go map
  written once a component.
- Three allocations every tick: a `Stamp` in `EventQueue.BeginTick`, one in
  `NextBoundary`, and the system list the membership's off-tick drain copies
  (`World.Systems`).
