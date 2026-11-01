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
landed. Open items elsewhere it meets:
[payload field names at load](#check-payload-field-names-against-their-event-at-load),
[a program spawning another](#let-a-program-spawn-another-program),
and [attack and materialize rows](#give-data-species-attack-and-materialize-rows-of-their-own).

#### Give a program's entrance an entering state

- Priority: P2
- Affected files: `internal/behavior/program.go`, `internal/system/behavior.go`
  (`enter`), `internal/component/behavior.go` (`BehaviorEntrance`)
- Guide: [Behavior machines §4.4](machine-design.md#44-spawns-that-ride-their-materializes-and-spirits)

A prototype's `entrance = "materialize"` plays a `swarm`-style materialize over
the footprint for its `entrance_ms`, a second when that is 0, and lands the body as
it ends, beside any rock that came over it, and nothing of it reaches the machine,
which starts in `initial` the tick the body stands. Lixen expects entrances to grow for machines (2026-10-27).
What a program would declare next:

- The materialize's style, `swarm` or `nugget` until a program has
  [rows of its own](#give-data-species-attack-and-materialize-rows-of-their-own),
  and whether a countdown shows over it.
- An entering state the machine runs while the materialize holds, so a program can
  telegraph, re-aim or cancel its arrival; while it runs the body neither strikes
  nor takes damage, as no body stands yet.
- A landing its probe chooses (`FindLanding` or its own) as the materialize ends,
  in place of the nearest clear footprint.

The materialize is Shared, so the entering state's clock and probe must run alike
on every instance and a capture mid-materialize must restore them
(`snapshot.Schema`).

#### Name a spawn's cell from the map's edge

- Priority: P3
- Affected files: `internal/event/payload.go` (`BehaviorSpawnRequestPayload`),
  `wad/scenario/vanguard/scenario.toml` (`RaidSpawn`)

Vanguard's raid spends seven actions naming its spawn's cell, the column past the
right edge on a row drawn two inside the top and bottom: two `ConfigToVar`,
`DecrementVar`, two `IncrementVar`, `RandomVar` and `ModuloVar`, the draw's index
bumped by hand. A map-relative anchor on the request (an edge and an inset) or a
draw advancing its own index would shorten it.

#### Draw a countdown in a smaller splash font

- Priority: P3
- Affected files: `internal/asset/splash_font.go`, `internal/system/splash.go`
  (`calculateTimerOffset`), `internal/render/renderer/splash.go`

A `Telegraph` countdown draws a splash digit, a 12×12 box whose ink spans at most
8×9, and is placed by that ink, taking the slot that overlaps the fewest blocked
cells when none is clear. In 16 headless vanguard games (8 seeds at 120×40 and 200×60, 2,400 ticks
each) 961 of 10,560 countdown ticks still ink a cell over rock, 3,538 of 272,730
inked cells. A smaller font, chosen by the body's size or the room around it, would
read in vanguard's narrowest cave (9 rows).

### Position, blocking and navigation

The move into `pkg/position` is done ([Position](position-package.md)); what
remains stands on its own.

#### Play a volume: the third axis's remaining foundation

- Priority: P3, not now: it needs other foundation changes first
- Affected files: `pkg/position`, `internal/engine/position.go`,
  `internal/app/config.go`, `internal/system/navigation.go`, `pkg/navigation`,
  `internal/render/renderer`
- Guide: [Position §3](position-package.md#3-the-volume),
  [Rendering §2](rendering.md#2-render-context-and-coordinates)

The index holds a volume one layer deep on every map, and a game in depth asks it
by the point. What such a game still needs is not laid: a depth in the config the
binding resizes to (`Position.Resize` passes one layer), the placement searches
(spiral, pattern, exclusion) in a 3D form, a footprint with depth and a
passability per layer in navigation, a flat renderer reading the layer it looks
at, a render in depth ordering overlap by a depth buffer over every layer
projected (`sandbox/3d`), a heightfield as a volume's surface (the surfaces lie on
the first layer), and cells chunked on demand for a volume past the ceiling
(`ClampVolume` holds a volume to the map's 125,000 cells), which also serves
[a ceiling-sized empty map](#shrink-the-spatial-grid-on-ceiling-sized-maps).

#### A generic index over the coordinate type

- Priority: P3, with the first 3D game, once `vmath` and the physics go to three axes
- Affected files: `pkg/position`, `pkg/vmath`, `pkg/vmath/physics`,
  `internal/engine/position.go`
- Guide: [Position §3](position-package.md#3-the-volume)

The index takes a flat `Point` and a `Point3` apart: the flat queries read ints
and the volume's a `Point3`, which keeps a flat game's rows at two ints and its
call sites as they are. One API shape for both would be an index generic over
the coordinate type, `Index[E, P]`, every coordinate read through a constraint's
methods, each shape specialised by the compiler at no runtime cost, and every
call site building a `P`. That churn pays only when a game in depth exists and
`vmath`, its vectors and the physics speak three axes alike; take it then, with
the volume's foundation above.

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
- Affected files: `internal/app/seat.go`, `cmd/vif`, `internal/mode/config_form.go`,
  `tool/vif-router`

`actions_per_second` is the only knob, and nothing caps it. Options: a rate suffix
such as `-bot 2:default@6` that `BotSpecs` parses, so `-bot`, `:bot add` and the
menu share it; graph variants per level; or a ceiling the driver clamps every
graph to. The fleet request carries it once the site exposes one.

### Let a watched holder pause with only its own bots

- Priority: P3
- Affected files: `internal/app/play.go`, `internal/app/seat.go`

A watched bot with seats is in a session, so the viewer forces it unpaused and
ignores pause keys. A person's run pauses with only its own bots; the viewer's pause
key could start the session hold `:pause` starts when the holder is its session's
host, which already stops its seats at one tick.

### Name participants beyond their slot

- Priority: P3
- Affected files: `internal/network/session.go`, `internal/snapshot`,
  `internal/render/renderer`

A hexadecimal slot identifies a cursor for one session, and participants have no
names. A label needs a roster and join-reply field, a capture schema change to
reach late joiners, and a trust decision, since each peer would choose its own.

### Train bot graphs offline

- Priority: P3
- Affected files: `internal/bot`, `pkg/genetic`, `internal/system/nugget.go`
- Guide: [Bots](bots.md#fitness-and-comparing-graphs)

First decide how a graph declares bounded tunable numbers: a `[bot.params]` table,
or bounded macro parameters shared with the editor's macro metadata. A genome is
those numbers as a bounded vector; `pkg/genetic`'s `Engine` evaluates whole
headless runs over a matrix of scenarios and seeds, and `pkg/genetic/fitness`
scalarises the result. A solo run depends only on its seed, so evaluation needs no
mesh. Decide where trained values ship. Bot genomes never enter the in-game
genetic registry, which captures carry. Fitness weighs no nugget, since nothing
pushes `EventNuggetCollected`: push it in `NuggetSystem.collectNugget` and drop
that function's own `statCollected` increment, which the event's handler repeats.

### Adapt bots to the session's players

- Priority: P3
- Prerequisite: [Train bot graphs offline](#train-bot-graphs-offline)

A small model of each other participant from Shared state, keyed by participant
for the session and dropped at its end, and a bandit over trained genomes. Decide
what is modelled and whether players see it; nothing is persisted.

## Authoring

### Check payload field names against their event at load

- Priority: P2
- Affected files: `internal/fsm/loader.go`, `internal/fsm/std/payload.go`,
  `internal/fsm/std/args.go`, `internal/bot/mind.go`, `internal/resource/check.go`

A payload guard's `field` (bot's `Own` too), a transition's `capture_vars` and an
`EmitEvent`'s `payload_vars` name payload fields looked up only when an event
arrives, so a misspelt one fails its guard or is skipped while `-check` prints
`scenario ok` (verified on a td copy with `field = "specie"` and
`capture_vars = { xx = "dead_x" }`). Shipped scenarios and the default bot graph
name only fields their events carry (a scratch scan of all 174 names). The smallest
fix resolves each name with `fsm.FieldByTag` at load: against the trigger's payload
prototype where `compileTransitions` resolves the trigger, which the payload guard
builders then take, and against the decoded payload in `compileEmitEvent`; a payload
guard on a `Tick` transition or as an action guard, which see no payload, fails too.
A behavior spawn or cancel request naming no program of the scenario is the same
gap one level down: the behavior system drops it and counts
`behavior.unknown_program_rejects`. `ValidateScenario`, which holds the machine and
the compiled programs, can resolve each request's `program` against `[behaviors]`.

### Build the map and scenario editor

- Priority: P2
- Affected files: `pkg/tomlmacro`, `internal/fsm`, `internal/manifest/schema.go`,
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
- **Macro metadata still needed**, each additive to `pkg/tomlmacro`: reference kinds
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
  `.vifimg` walls). A painted map needs a static form the digest covers, and
  `.vifimg` assets come from the `image/` category, which it does not and a joiner
  is not sent. Decide between a TOML wall encoding (rectangles or run-length rows)
  and admitting `.vifimg` to the scenario container under its bounds.
- **In the game.** An editor mode beside command and visual in `internal/mode` acts on
  a paused world and draws through an overlay like `:graph`. Applying an edit
  validates with `resource.ValidateScenario` and restarts on it as `:n` does, since
  a run plays one scenario. Editing inside a multiplayer session is out of scope.

In order: reference kinds, order and provenance; a read-only graph of a scenario's
regions, states and blocks; placing and editing blocks into editor-owned files; the
map layer and point markers; root `[[expand]]` blocks and library copies.

## Browser sessions

### Budget a fleet pod's joins per player

- Priority: P3
- Affected files: `internal/network/session.go` (`AdmissionLimiter`),
  `tool/vif-router`

A pod sees every player the router splices at the node's address, so its own
budget of 6 joins a minute is one per session rather than one per player: a
seventh join into one session within a minute is refused, whoever makes it,
though the router has already budgeted each player. Close it when a session
that busy appears, by exempting the node's address at the pod or by having the
router name the player to it.

### Add browser admission authentication

- Priority: P2
- Affected files: router/session adapter, website, future authentication
  dependency
- Prerequisite: every route carries real play without transport faults, the
  built-in WebSocket included; no auth layer goes in while that is being settled

Issue a short-lived, session-scoped admission credential after authentication and
consume it during the WebSocket handshake without putting it in page history or
logs. Creation takes one too: `network.RequestSession` is the terminal's side of
`POST /vif/api/sessions`, and already shows a 401 or 403 as the router's
message. Decide whether the expanded router remains the credential boundary or
is renamed/split before adding the planned `github.com/lixenwraith/auth`
Argon2-SCRAM dependency. A terminal's own door takes the same handshake: a
`-selfsign` door encrypts with no authority and a native joiner takes it as it is,
open to a man in the middle until the SCRAM exchange with `WithChannelBinding` over
the leaf certificate's digest authenticates both ends on that channel, as
`lixenwraith/chess` authenticates; `-ca` pins it meanwhile. A browser trusts a
self-signed door only by being told to: pinning from a page needs WebTransport's
`serverCertificateHashes`, which the route does not speak.

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
HTTP's own.

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

### Fix the Windows zip's mouse, sound and status bar flicker

- Priority: P3
- Affected files: `Makefile`, `.github/workflows/release.yml`, the terminal
  module's Windows backend
- Guide: [Build profiles](multi-platform.md) §4

Played in Alacritty, the zip ignores the mouse, has no sound and flickers status
bar cells on redraw. Try the mouse in Windows Terminal, whose console host is newer
than the system's, to tell a console-host gap from a backend one; try a build
without `vif_noaudio` with ffplay on `PATH`; and try the flicker with the frame
wrapped in synchronized output.

### Cache a received scenario to the user root

- Priority: P3
- Affected files: `internal/resource`, `internal/app/session.go`

A scenario received from a coordinator lives in memory and is dropped at exit, so
rejoining the same session downloads it again. Writing it under the user root
behind an explicit opt-in would keep it, and needs a trust decision first: the
bytes came from a peer, and nothing about a plaintext link says they are the
operator's.

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
links, through a tower and a storm and on `wad/scenario/td`, for the hour the
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

### Shrink the spatial grid on ceiling-sized maps

- Priority: P3
- Affected files: `pkg/position/grid.go`
- Guide: [Session fleet](kubernetes-fleet.md#6-resources) §6

The grid is sized to the map, so only a ceiling-sized one such as `td` (500×250)
reserves about 30.5 MiB per world, and a guest's staging world a second: 92 MB of
the 240 MiB a td host, guest and staging world held after a collection, the largest
owner (netcode design §3.7). A sparse or tiled grid for a map this empty, or one
staging world sharing the live grid's shape without its cells, takes it.

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
whose link to its host drops is unaffected, since its redial is a claim and the
claim's arrival clears the mark. Two cases are not:
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

### Cross a departure a reset dropped

- Priority: P2
- Affected files: `internal/system/network.go` (`Init`, `announceDeparture`),
  `internal/app/transport_test.go`

A link lost within a lead of a reset loses its departure: the reset clears the
barrier (`NetworkSystem.Init` drops `scheduled` and `departed`) before the
departure crossed for it applies, and the next game rebuilds that participant's
cursor from the roster as a live one nobody drives, which never turns Wraith, never
times out and holds the slot its claim comes back for: a player who quits as its
defeat ends the game can leave one. `TestTwoLiveParticipantsConvergeOverTCP` shows
it once its scripted defeat's reset is moved into the disconnect's lead. What would
close it: after a reset the authority crosses, once, the departure of every
rostered participant it neither links nor carries.

### Publish a player's session at the router

- Priority: P3
- Affected files: `tool/vif-router`, `internal/app/host.go`, `internal/network`, `web/terminal.js`
- Guide: [Multiplayer](multi-player.md#56-relay-doors-and-the-router) §5.6

A run publishes its `-relay` door through the router's reverse tunnel and is
reached at the site; a host's door published the same way is a player's own session
reached there, the router neither allocating nor authoring. The deployment this
leads to serves as many pods as the node has resources for (`-port-first`/`-port-last`,
ten today) and routes every session beyond them, with accounting and
authentication added later. The tunnel carries a door already, and the foundation
stands: a name is resumed only by the run it was issued to, within
`NetworkTunnelResumeWindow`, so no published name shadows a pod's; one address
holds at most `-doors-per-address` doors; every connection placed is accounted for
as it closes (`session route closed`: session, door or pod, bytes each way,
seconds); and the listing's `limits` carry `sessions_max`, `doors` and `doors_max`,
so the site can say when the fleet is full and offer a hosted session instead.

What the feature itself needs, best shaped by deploying it step by step:
- `-host vif://<site>:7777` opening the door through the router rather than binding
  one, and the host's run reporting its state on the control connection (players,
  ceiling, scenario, whether it admits) so the router lists it without a `/health`
  read through the tunnel.
- The session's row in `/vif/api/sessions`, with a `kind` beside the pods', its
  lifetime (the control connection's, plus the resume window), and the create
  budget charged to the publisher's address as a pod's is.
- A credential in the publish frame (`PublishRequest` is JSON, so a field is added
  without a protocol change) once authentication exists, and accounting keyed by
  that identity rather than the address, over the per-connection lines above.
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
- Affected files: `internal/app/snapshot_stage.go`, `internal/system/network.go`
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
session from `https://lixen.com`, its default). Put a second choice beside the tap:
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
- Affected files: `pkg/network`, `internal/network`, `internal/app/host.go`,
  `internal/mode/config_menu.go`, `internal/mode/commands.go`,
  `internal/paths/settings.go`, `internal/parameter`, `android/game/game.go`,
  `android/app/src/main/java/com/lixen/vif/MainActivity.java`
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
  `internal/app/session.go`, `internal/app/join.go`, `internal/engine/world.go`
  (`FollowLocalCursor`), `internal/mode/config_menu.go`, `cmd/vif`, `web/terminal.js`,
  `internal/app/journal.go`, `internal/app/replay.go`
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
  as `Claim` is, so the authority and every spectator read the same answer. Seats
  take their holder's.
- Capacity: spectators cost the authority a link and its corrections each, so a
  `-spectators N` ceiling, reported in the router's listing; relay doors can fan
  them out ([Multi-player §5.6](multi-player.md#56-relay-doors-and-the-router)).

A spectator sees the shared world, what any remote participant sees, centred on
the target: Player-domain state is never rebuilt on another instance (D-2), so the
target's glyphs, drains, nuggets, projectiles and loot stay on its run, and nothing
beyond admission and the follow target goes over the wire for it.

The journal's slot handling needs a review alongside. A journal records its slot
from `LocalSlot` at the opening (`JournalCapture.Slot`, `JournalAnchor.Slot`),
its replay binds input there (`openedLocked`), and later installs bind by
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

## Session membership

A suggestion for the words, not a decision: a *session* is one world timeline under
one authority — a fleet pod, a terminal host, any instance — and its participants,
and solo play is a session of one. Commands then move participants between sessions:
`:open`/`:close` in place of `:host`, since every instance already is its own
session's authority; `:join`/`:leave` for a participant; `:drop` for an authority
removing one; `:merge`/`:split` for whole sessions.

### Let an authority drop a participant

- Priority: P2
- Affected files: `internal/network`, `internal/app`, `internal/mode/commands.go`,
  `tool/vif-router`

`:player drop <slot>` lets a host drop a player with its bots, or one bot, and the
dropped person starts a new local game. What remains: a reason the dropped client
prints, playing on alone as after `:leave` instead of starting over, and an
operator route on the router, since a dedicated host has no console. Keeping a
dropped player out is a separate decision: the session name is a routing key, not
an identity.

### Keep a Wraith's stats in the session

- Priority: P2
- Affected files: `internal/app/claim.go`, `internal/app/identity.go`,
  `internal/app/host.go`, `internal/system/cursor.go`, `internal/system/network.go`
- Guide: [Multi-player](multi-player.md#54-wraiths) §5.4

A player coming back to its Wraith brings its cursor's stats itself
(`event.CursorRecord`: energy, heat, health and loadout): the last owner-state sync
its run made in that session, frozen once the run leaves it, so play in a fork since
never comes back. This is a compromise. The values are owner-authored
([D-13](domain-design.md#d-13--owner-authored-shared-values-have-one-writer)) and a
Wraith gives up everything the world reaches it through, so neither the host nor the
shared world keeps them, and the session takes the returning run's word for them.

The fix shares some data of every cursor, so the session holds what each Wraith
resumes with and a successor inherits it. That moves those values off the one-writer
rule (D-2, D-13) for every system that writes them owner-side, and costs bandwidth,
which is why it waits. A cheaper step, a Wraith keeping the copy the sync committed
at its departure tick, is outside the hash and the repair (D-13), so nothing holds
the instances' copies equal.

The record is built to be replaced: one type, made in one place
(`NetworkResource.OnOwnState` into the claim keeper) and read in one
(`CursorSystem.restore`), carried by the arrival crossing the coordinator fills
(`ParticipantJoinedPayload.Record`). What would close it: decide which values the
session holds, have the coordinator fill the arrival from its own world instead of
the joiner's report, and drop the record from the keeper and the report.

### Place an arrival where its scenario places a cursor

- Priority: P2
- Affected files: `internal/system/network.go`, `internal/app/join.go`,
  `internal/engine/resource.go`

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
- Affected files: `internal/app/session.go`, `internal/app/serve.go`, `wad/scenario/td`
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

## Species

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

### Let a program spawn another program

- Priority: P3
- Affected files: `internal/system/behavior.go` (`SpawnSpecies`)

`SpawnSpecies` raises a swarm or a quasar by their spawn requests; a program cannot
raise another program, or more of itself, because the vocabulary is built before
any program compiles. Its build could take any program name and push
`EventBehaviorSpawnRequest`, which drops and counts an unknown one; the load-time
check of that name belongs with the payload names under Authoring.

### Give data species attack and materialize rows of their own

- Priority: P3
- Affected files: `internal/profile/combat.go`, `internal/component/materialize.go`

A prototype names a combat row (`swarm` or `quasar`) and a program's materializes
a style (`swarm` or `nugget`), so a new attack profile or materialize style is a Go
change ([Behavior machines §6.4](machine-design.md#64-beyond-the-machine)). Rows of
its own would grow those tables per scenario from the prototype, as soft collision
builds a data species' rows from its `mass` and loot rolls the drop table its
`drops` names.

### Fuse single-cell species and unit composites into any species

- Priority: P3
- Affected files: `internal/system/fuse.go`, `internal/event/payload.go`
  (`FuseSwarmRequestPayload`, `FuseQuasarRequestPayload`),
  `internal/system/drain.go` (`detectSwarmFusions`), `internal/system/behavior.go`
  (`spawnRequests`), `internal/asset/scenario/quasar.toml`,
  `wad/scenario/td/td_main.toml`
- Design: [D-3](domain-design.md#d-3--cross-the-smallest-shared-outcome)

A fusion takes drains alone, into one of two species: two enraged drains into a
swarm, every drain of the elected cursor into a quasar. The idea grows it to any
number of sources of any single-cell species, the drain today and any later one, and
of any unit composite (`CompositeTypeUnit`: damage on the header, the members its
hitboxes; the swarm, quasar, eye, kraken and every data species), fused into any
species, so quasars and swarms fuse as drains do.

- **One request.** An `EventFuseRequest` names its sources, by species and count or
  as a cursor's, the target species and the `FuseEffect`, and
  `EventFuseSwarmRequest` and `EventFuseQuasarRequest` become two uses of it: the
  drain system's pairs and the quasar scenarios' triggers play as now.
- **Domains.** Drains are Player-domain, so a drain fusion is local and crosses one
  spawn request from the causal participant (D-3). A composite is Shared, so
  consuming one is a Shared outcome every peer applies at one tick, from a Shared
  trigger or as the crossing a player's request sends, the composite going with no
  reward by `EventCompositeDestroyRequest`, as the rock crushes one. A fusion of
  both kinds splits by domain, each batch domain-pure as `killDrains` keeps it, and
  lands one target. A composite with a `ParentHeader`, the snake's head, is no
  source.
- **Any target.** The target lands by its own spawn request: `landFusion`'s two
  cases become a table every species' request joins, `spawnRequests` in
  `behavior.go` the start of it, a data species by `EventBehaviorSpawnRequest`,
  named at load as `PayloadSpeciesIn` names one. Its footprint, a code species'
  parameters or a prototype's, places it at the sources' centroid by
  `FindFreeAreaInside`.
- **Visuals.** Each source flies one spirit, a composite's from its header, and the
  first names the target its arrival lands; the materialize effect plays over the
  footprint instead. A Shared fusion's spirits stay each instance's own, as a drain
  fusion's do.

In order: the one request carrying the two drain fusions unchanged in play, then any
target from drains, then composites as Shared sources, one test per rule.

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

### Spare the overlay cards their per-frame titles

- Priority: P3
- Affected files: `internal/render/renderer/overlay.go`

The help and telemetry overlays run with the game live, and `renderCard` builds
each visible card's padded title string every frame, a dozen small allocations
a frame. Keeping the titles beside the masonry items at relayout, where the
layout is already cached, removes them. The menu and form overlays allocate
likewise but show while `PauseForInput` holds the simulation, so they cost play
nothing.


## Audio

### Bring soundlab to Android (soundlab mobile)

- Priority: P1
- Affected files: `cmd/soundlab`, `android/game`, `android/app`,
  `doc/multi-platform.md`

Soundlab runs on Android in-process, as the game does, over the same engine and
the user root's `audio/` documents, so a pattern written on the phone plays in the
phone's game. The foundation is in place: `internal/soundlab` holds the session,
documents, verbs and TUI, `NewSession` takes the host's speakers as its sink
(`Options.Sink`), and `RunTUI` runs over any `terminal.Terminal`, the host posting
`EventClosed` to end it; the Android host already turns taps, drags, keys and the
system keyboard's text into `terminal.Event`s and flushes cell frames through
`glyph.Frames`. What remains:

1. A `terminal.Terminal` over the Android view (its grid as `Size`, its event
   queue as `PollEvent`, `glyph.Frames.Flush`) runs it, started by a second bound
   entry beside `game.Start` that hands `NewSession` the `Speaker` as its sink;
   the start screen offers soundlab beside the game.
2. Touch: the TUI reads keys alone. A tap hit-tests what was drawn: a key of the
   keyboard plays its note, a step toggles, a tab or a pane takes the keys, a mixer
   row mutes its slot, a browser row selects. A strip of buttons stands for the
   keys a phone lacks (record, play, delete, save, new, edit, tempo, Tab, Esc), and
   the system keyboard serves `:` and the prompts, as in the game.
3. Size: the views need 60×16 cells. A phone held landscape fits at the default
   cell; portrait needs the pinch zoom out, or a keyboard of fewer keys.

### Tune the rip

- Priority: P1
- Affected files: `pkg/audio/rip.go`, `pkg/audio/rip_test.go`, `internal/soundlab`

`RipSamples` ([Audio §11](audio.md#11-ripping-a-recording)) reads tempo, bars,
key, drums, melody and bass off a recording under `RipParams`, through the
built-in analysis or `ripnet`; [Rip design](rip-design.md) is the whole plan.
The search over the parameters is in place (`tune`), as are the corpus the
engine exports (`corpus`), the model, its trainer (`tool/rip-train`) and the
front-end seam. What remains, in the design's order:

1. A corpus beyond the shipped bank: `corpus ... aug gen` puts a room, a mix
   and voices of random genes behind each loop and draws beats, melodies and
   bass lines from the key; what it still lacks is a codec, chords and two
   lines in one register among the generated parts, chance steps graded as
   optional rather than as misses, and a small set of real recordings with
   hand-checked notes held out for validation, so neither a model nor a tuned
   set learns the synthesizer.
2. The model's pitch: on 20000 loops over 20000 steps the held-out decoders'
   loss was still falling (1.04) and the model passed the analysis on
   augmented loops (0.51 against 0.48) on its drums while losing clean loops
   on tempo and both on pitch (melody 0.11 against 0.17); 300 loops
   overfitted in 12000 steps. The corpus is rendered at A440 and the sample is
   forty cents flat, which the analysis now reads and the model cannot: render
   the loops at random offsets from A440 (`corpus ... aug`), then more loops
   and steps. Measure by the trainer's held-out loss, with `bench` against the
   analysis on clean and augmented loops and by ear on the held-out recordings
   (`lixenwraith/scratch`'s `one_more_bite_intro.wav` first), then the weights
   and a tuned parameter set shipped as data, `DefaultRipParams` kept for a
   recording nothing tuned for. [Rip design §8](rip-design.md#8-measured-on-the-sample-recording)
   has the runs' numbers.
3. A bass attack reads as a kick and a chord's attack as a snare in the analysis:
   a harmonic check on the hit (a percussive rise with a pitched tail is a note)
   until the model takes the front end.
4. Key changes within a recording are outside the model: a key per phrase (a
   chromatic note is an accidental on its step now). Two voices in one register have their second pass now; a
   third, and a chord's inner notes, do not.
5. A sustained note with a wide vibrato can still read as two: the sample's
   wobble was its tuning, forty cents off A440, which the front end reads first
   now, and its notes hold for four to eight steps; a vibrato wider than a
   quarter tone would still cross rows. A pitch prior that holds a semitone
   wobble (a higher cost for a one-semitone move than per semitone of a leap,
   or the salience smoothed across the neighbours before the path), measured
   on a rendered vibrato voice once the voices have one.

### Close the rip's loop

- Priority: P1
- Affected files: `pkg/audio/rip.go`, `pkg/audio/song.go`, `internal/soundlab`

[Rip design](rip-design.md) §2 stage 7. The voice fit (`rip`, `fit`), the
dictionary and the song (`compress`, `song`) and the song's score against the
rip are in place; what remains:

1. The refinement loop (`refine`) searches the voices and sweeps the tonal
   steps, a semitone or an octave either way or silence per step, judged by
   `Fidelity` against the recording's own frames; what it does not yet move is
   a step's position or length or the drum part, and a note the front end
   never heard it cannot add. Measure its convergence on rendered loops whose
   truth is known.
2. Matching by reconstruction: a bar's distance to a member as the rip of the
   member's render against the bar, in place of `BarDistance`, with `Memoize`
   paying for each render once; measure it against the mechanical grouping.
3. Several voices in a register: the second line has its own fitted voice now;
   what one line still mixes is two instruments taking turns as the louder,
   which the first path hears as one. Cluster a line's notes by their genes,
   or by their lengths and attacks until a model names them, and fit a voice
   per cluster.
4. The fit's sample notes are the mix around them: subtract the other register's
   estimate from the recording before the mel frames, or fit on the frames a
   trained front end attributes to the note.
5. A song in the game: the conductor plays tiers, not bars; a `SongDef` played
   by the sequencer as soundlab conducts it, when a level wants a fixed piece.

### Render a conductor timeline offline

- Priority: P3
- Affected files: `internal/soundlab`, `cmd/vif-log`

`RenderTimeline` plays a timeline of slot patterns offline and `song` writes a
`SongDef` through it; the conductor's own timeline, patterns by tier from an APM
trace, is what still lacks a path to it. The offline conductor render that
compared bass voicings for the music-smoothing review, an APM timeline through
the sequencer with no device, is a test-only
harness in a scratch repository. `AudioEngine.RenderMusic` now renders placed
patterns offline; the verb would drive an APM timeline through the conductor's
tier and tempo policy over it and write the WAV.

### Spin off `pkg/audio` as its own module

- Priority: P2
- Affected files: `pkg/audio`, `pkg/audio/model`, `internal/parameter`,
  `internal/service`, `internal/resource`, `internal/soundlab`, `go.mod`

No in-repository import remains: beside the standard library the package imports
only `lixenwraith/toml` and its `macro` expander, which moved there for this.
What remains is the move itself: a `lixenwraith/audio` repository with the
package at its root and `model` under it, the package README as its README with
import paths rewritten, the repository LICENSE and a `go.mod` at this module's
Go version; then this module's importers take the new path and `go.mod` requires
it.

## Genetic library

### Spin off `pkg/genetic` as its own module

- Priority: P2
- Affected files: `pkg/genetic`, `pkg/genetic/registry`, `pkg/genetic/fitness`,
  `pkg/genetic/tracking`, `pkg/genetic/persistence`, `sandbox/genetic`,
  `internal/system/genetic.go`, `internal/engine/resource.go`,
  `internal/parameter/genetic.go`, `go.mod`

The audit's items have landed: the pending table probes a window, selectors
cache by archive version, the batch engine runs a worker set over recycled
buffers, `registry.Stats` and `PerturbationStdDev` are gone, the game's metric
and context keys left `fitness` and `tracking`, and `persistence` defaults to
JSON, so nothing under `pkg/genetic` imports anything outside the standard
library. What remains is the move itself: a `lixenwraith/genetic` repository
with the five packages at its root (`genetic`, `genetic/registry`, ...), the
lander sandbox as its `examples/`, the package README as its README with import
paths rewritten, the repository LICENSE, and a `go.mod` at the Go version this
module builds with; then this module's four importers take the new path and
`go.mod` requires it. The sandbox's `-headless` table and `go test ./...` are
the check on both sides.

## Engine

### Give the composite lifecycle methods a user

- Priority: P3
- Affected files: `internal/system/composite.go`

`CompositeSystem` carries `handleEmptyComposite`, `CreateHeader`, `AddMember`,
`DestroyComposite` and `GetAnchorForMember`, designed for building and tearing
down composites at run time, but no system or scenario uses them. A species or
scenario that assembles composites would; it reaches them through events, since
another system may not call them directly.

## Structure

### Move the three run types to packages

- Priority: P3
- Affected files: `internal/app`

`corrector`, `membership` and `scribe` each read the run through one interface
(`correctionRun`, `membershipRun`, `scribeRun`), and `App` is the composition
root the package map names, but all three stay in `internal/app`, which is
still 33,000 lines. Each builds runs (the staging world, the journal twin, a
dial's run, a seat's bot), so a package for one needs the run's constructor as a
second seam, and the tests drive whole Apps through `NewHeadless`, so none could
move its tests with it. A package per type waits on a constructor seam the tests
can be written against.
