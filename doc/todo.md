# TODO

Scratch notes that travel with the repository. Each item states what remains and
the smallest thing that would close it. Delete an item when it lands; this file
is not a changelog. Source files do not carry parallel TODO comments, so a
code-originated item names its source here.

Priorities: P0 blocks a release or leads the feature development push, P1 is
wanted next, P2 is convenient follow-up, and P3 is an idea.

## Bots

### Open fleet sessions to bots

- Priority: P2
- Affected files: the Hugo site's `vif-lobby.js`, `pkg/network/endpoint.go`,
  `internal/mode/config_form.go`, `tool/vif-allocator`, `deploy/guest/vif-allocator.env`,
  `deploy/k3s/10-quota.yaml`
- Guide: [Session fleet](kubernetes-fleet.md) §6

The allocator takes `bots`, but `bots_max` stays 0 until these hold:
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
  `tool/vif-allocator`

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

### Refuse unknown keys and `op` values at load

- Priority: P1
- Affected files: `internal/fsm/loader.go`, `internal/fsm/std/guard.go`,
  `internal/fsm/std/payload.go`, `internal/resource/check.go`, `doc/fsm-reference.md`

The scenario and bot-graph decode drops keys outside `ScenarioDoc`'s tags, so a
misspelt `on_entr` loads as absent and `-check` is silent, and `CompareInt` reads
an omitted or unknown `op` as `eq` (`guard.go`), unchecked by the guard factories,
so `op = "ge"` tests equality (verified by reading; `:d break stat` spells `ne`,
`ge`, `le`). The fix is the strict `Op` and `Args` of
[Behavior machines](machine-design.md) §7's dialect kernel, which the editor also
needs; until it lands, the guard factories could refuse an unknown `op` alone.
Shipped scenarios and the default bot graph use only known keys and ops.

### Author the tonal instruments as data

- Priority: P3
- Affected files: `pkg/audio/voice.go`, `pkg/audio/sound_spec.go`, `cmd/soundlab`

The tonal voices (bass, piano, pad, lead, pluck) are a table in code, so a new
timbre or a retuned bass is a Go change and soundlab cannot audition one. A
`[[voice]]` table in `sounds.toml` with the same fields, loaded like a sound and
hot-swapped like one, would put them under the author's hand; the bass voicing
alternatives rendered for the music-smoothing review would then be a file edit.
The offline conductor render that produced those comparisons (an APM timeline
through the sequencer, no device) is a test-only harness in the review branch's
scratch repository, and would be a `soundlab` or `vif-log` verb.

### Build the map and scenario editor

- Priority: P2
- Affected files: `pkg/tomlmacro`, `internal/fsm`, `internal/manifest/schema.go`,
  `internal/mode`, `internal/render/renderer`, `internal/resource`
- Prerequisite: the argument catalog of the dialect kernel
  ([Behavior machines](machine-design.md) §7)
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
- **Vocabulary.** Action, guard and event pickers read `-schema`, which lists actions
  and guards by name only, without arguments, until the argument catalog lands.
- **Map layer.** Geometry is generated at run time (`EventLevelSetup`, maze rooms,
  `.vifimg` walls). A painted map needs a static form the digest covers, and
  `.vifimg` assets come from the `image/` category, which it does not and a joiner
  is not sent. Decide between a TOML wall encoding (rectangles or run-length rows)
  and admitting `.vifimg` to the scenario container under its bounds.
- **In the game.** An editor mode beside command and visual in `internal/mode` acts on
  a paused world and draws through an overlay like `:graph`. Applying an edit
  validates with `resource.ValidateScenario` and restarts on it as `:n` does, since
  a run plays one scenario. Editing inside a multiplayer session is out of scope.

In order: the argument catalog; reference kinds, order and provenance; a read-only
graph of a scenario's regions, states and blocks; placing and editing blocks into
editor-owned files; the map layer and point markers; root `[[expand]]` blocks and
library copies.

## Browser sessions

### Bound navigation's worst tick on busy maps

- Priority: P2
- Affected files: `pkg/navigation/flowfield.go`, `internal/system/navigation.go`,
  `internal/system/loot.go`
- Guide: [Navigation §10](navigation-design.md#10-costs)

On td with eight walking cursors and seven snake heads a storm tick peaks at 30 ms
against 6.4 ms on average, and no wall change computes more than two fields in a
tick. What is left, for a need rather than a measurement: a per-tick field budget
in canonical group order, if a browser frame still stalls, and a footprint tier
registry deriving the grids to keep from the live navigators, so a data-driven
species' footprint costs one row. A 2×2 coarse field is not planned: it changes
the answer, and the shared grid already took the cost it would have saved.

### Budget a fleet pod's joins per player

- Priority: P3
- Affected files: `internal/network/session.go` (`AdmissionLimiter`),
  `tool/vif-allocator`

A pod sees every player the allocator splices at the node's address, so its own
budget of 6 joins a minute is one per session rather than one per player: a
seventh join into one session within a minute is refused, whoever makes it,
though the allocator has already budgeted each player. Close it when a session
that busy appears, by exempting the node's address at the pod or by having the
allocator name the player to it.

### Add browser admission authentication

- Priority: P2
- Affected files: allocator/session adapter, website, future authentication
  dependency
- Prerequisite: every route carries real play without transport faults, the
  built-in WebSocket included; no auth layer goes in while that is being settled

Issue a short-lived, session-scoped admission credential after authentication and
consume it during the WebSocket handshake without putting it in page history or
logs. Creation takes one too: `network.RequestSession` is the terminal's side of
`POST /vif/api/sessions`, and already shows a 401 or 403 as the allocator's
message. Decide whether the expanded allocator remains the credential boundary or
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
- Affected files: `internal/resource`, browser resource provider
- Prerequisite: decide publisher trust, allowed origins, and cache policy; the
  format and the limits are `resource.Scenario`'s and are already decided

Two of the three routes to content exist. A native player downloads the release
wad and extracts it as a config root; a guest joining a session is served the
coordinator's scenario and verifies its digest before constructing its `App`. The
corpus needs neither: it is player domain, resolved per instance and never
reconciled, so a peer holding a different one is not a disagreement to settle.

The third route is a client with no config root and no peer — a WASM build before
it has joined anything, which has no roots at all. An HTTP-backed provider reading
the same content-addressed container is what [Multi-platform](multi-platform.md)
anticipates, and it is one provider for both the browser case and a native player
who would rather fetch a scenario than unpack one. What it needs before it is
written is whose signature makes bytes trustworthy, which origins may serve them,
and how long a fetched container is kept.

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

Each row of the allocator's session list names the build its session runs
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
  `tool/vif-allocator/manifest.go`
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
  `deploy/k3s/30-session.yaml`, `tool/vif-allocator`
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
- Affected files: `deploy/guest/update-vif-wad.sh`, `tool/vif-allocator/manifest.go`,
  `deploy/k3s/30-session.yaml`

What a fleet session reads from the node volume is one list in three places —
`categories` in the installer, `wadCategories` in the allocator, and the mounts in
the template. `scenario` and `image` are on it. `audio` goes on it the day a
dedicated host renders anything, and `content` never does. Nothing else changes;
the volume already carries whatever the installer puts there.

### Shrink the spatial grid on ceiling-sized maps

- Priority: P3
- Affected files: `internal/engine/spatial_grid.go`
- Guide: [Session fleet](kubernetes-fleet.md#6-resources) §6

The grid is sized to the map, so only a ceiling-sized one such as `td` (500×250)
reserves about 30.5 MiB per world, and a guest's staging world a second: 92 MB of
the 240 MiB a td host, guest and staging world held after a collection, the largest
owner (netcode design §3.7). A sparse or tiled grid for a map this empty, or one
staging world sharing the live grid's shape without its cells, takes it.

## Multiplayer

### Trim what remains of a tick and a checkpoint at the map limit

- Priority: P2
- Affected files: `pkg/navigation/flowfield.go`, `internal/system/interaction.go`,
  `internal/engine/world_copy.go`

Measure a td tick with 16 players at `GOMAXPROCS=1` and trim the larger of field
derivation and `checkCursorOverlap`'s position lookups; with eager, heap-searched
fields on a 3-tick cooldown they were about 45% and a quarter of a 2.6 ms tick.
Field derivation has since halved (lazy fields on one shared grid per tier, the
snake lock by one search); what it still costs is in
[Bound navigation's worst tick](#bound-navigations-worst-tick-on-busy-maps). A
checkpoint scans the whole grid for its occupied cells, most of its 10 ms. Each must
stay exact; per-store write counters stay rejected, since a cache compares values it
owns.

A td participant costs about 300 MB, of which the 32 MB spatial grid is a minority;
whole-world captures and the staging world dominate. In a sandbox a `-serve` host
with one seat held 672 MB resident and 1255 MB with two more, its ticks slipped from
two seats on, and networked joiners stalled ("no authoritative world inside the
convergence floor") or lost their links within seconds.

### Reach a relay without an address in hand

- Priority: P3
- Affected files: `internal/app/relay.go`, `internal/converge/reach.go`, `internal/network`

A person who cannot reach the host joins at a guest's `-relay` address and plays
through it; the session is a tree, raw artifacts go up and the authority's down,
and a relayed person whose relay leaves walks the chain to another participant
relaying for others (multi-player.md §5). What is left is finding that address: a relay's address is
handed out of band like the host's, nothing publishes which participants relay,
and a NAT between a person and every relay is not crossed. A browser relays only
its own bots. Relay trees the authority chooses are dropped
([netcode design §4, Dropped](netcode-design.md#dropped)): a relay stays what a
person chooses, and this item and the two after it are its correctness. Verify a
relayed topology under `LinkShape`s, asserting hash-only
convergence and the authority's per-link fan-out.

### Move a relayed survivor to its host

- Priority: P3
- Affected files: `internal/converge/authority.go`, `internal/system/network.go`
- Guide: [Multi-player](multi-player.md#51-succession) §5.1

A survivor that reached its new host only through a candidate relaying for others
follows that host through it for the rest of the session, its artifacts a hop
longer than they need be and the relay's player carrying traffic it offered for a
host loss. What would close it: the far survivor, once following, dials the host at
its chain address and moves its up link there, the relay letting it go.

### Redial a relay that left

- Priority: P3
- Affected files: `internal/app/converge.go`, `internal/converge/reach.go`

A person whose relay's link dropped walks the chain for a participant that relays
for others, and forks when none does; it does not redial the relay's `-relay`
door, which a relay whose link only blipped would answer, as a guest redials the
host it lost ([Multi-player](multi-player.md#51-succession) §5.1).

### Carry a dropped guest back to a live host

- Priority: P3
- Affected files: `internal/app/relay.go`, `internal/converge/authority.go`

A guest whose own link to a host still running dropped, and whose redial cannot
reach that host, walks a list nobody else is walking and forks, though a peer
relaying for others could carry it: the host keeps its place as a Wraith, and
taking that back needs a join under its claim through the peer, which a peer's port
takes only once that peer hosts. What would close it: the port relaying a rejoin
before then, as the `-relay` door does, when its player relays for others.

### Serve a relay address on a run that took the session over

- Priority: P3
- Affected files: `internal/app/relay.go`

A guest's `-relay` door asks the authority on its own link for every join it
relays. Once that guest takes the session over it is the authority, so the door
turns every dial away as the session changing hands, which the dialer retries, while
the guest's own port takes joins. What would close it: the door admitting as the
host's does once its run hosts, as that port does.

### Keep a late survivor in its host's group

- Priority: P3
- Affected files: `internal/converge/authority.go`, `internal/app/session.go`

A host sends away every independent participant that reported no membership at its
term a convergence floor after it resumes, so a survivor whose report arrives later
finds its own cursor a Wraith, which only a return under its claim gives back
([Multi-player](multi-player.md#8-remaining-gaps) §8). What would close it: a
membership report after the floor taking the place back as a claim does, over the
link it already holds.

### Project a correction outside the live lock

- Priority: P2
- Affected files: `internal/app/snapshot_stage.go`
- Guide: [Netcode design](netcode-design.md#less-work-under-the-live-lock)

An answer no longer captures under the lock (netcode design §4, converged
answers); an install still holds it: the deepest td install of netcode design §3.7
held 48 ms with no tick projected, 11 the staging world's capture and 36 the live
capture, the comparison and the write, and each projected tick adds a simulation
tick, so a repair 16 ticks behind holds it past 90 ms. First, compare the staging
world's stores against the live ones in place, as the in-place index reads the live
world, and write the cells that differ, with no capture of either; then move the
projection out.
Any out-of-lock or incremental design must keep [Multi-player](multi-player.md) §3.3:
fence membership, pending barrier crossings not fed, the ledger settled before
projection, the journal's place and mark, and no live tick between the final
projection and the write. WASM gains scheduling time, not parallel CPU.

### Close the edges of a session hold

- Priority: P2
- Affected files: `internal/app/host.go`, `internal/app/relay.go`,
  `internal/app/join.go`, `internal/system/meta.go`, `internal/app/script.go`

The host's hold ([Multi-player](multi-player.md#36-holding-a-session) §3.6) leaves
these gaps, which the start wait makes common: each new game a session's host plays
stands held until its press.
- A join that meets a hold mid-gate fails and is retried until the host resumes: a
  relay's gate, and a host's whose playout-lead wait the hold cuts short, wait for
  a capture past the held tick. Both could gate as a held host does.
- A join whose gate reads the held world fails when the session resumes before the
  read ends, as a successor does once its group reported. The gate could start over
  running.
- A joiner's landing hold is not journaled, so a guest journal recorded across a
  join into a held session replays the install running and the host's resume
  there announces nothing. The world a join installs could carry it, as an
  opening carries a hold.
- `:pause off` before the host's own hold has landed does nothing, and the hold
  then needs another `:pause`. The end could apply at the landing instead.
- A script that `:pause`s itself never resumes: its driven loop takes no step
  while held, so its timeline never reaches the next action. Refuse it there.

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

## Netcode

### Price the log's and journal's 50 ms sync

- Priority: P3
- Affected files: `internal/vlog/vlog.go`

With `-log DEBUG` and a journal open, the same session's terminal ran 14% system CPU
and about 6,000 block writes a second (`proc.io_write_s`) for some 80 kB/s of
records: both files sync every `flushIntervalMs` (50 ms). The journal's crash
promise needs it; the log's does not. Measure a log that syncs on its own cadence,
or only on WARN and above.

### Lay keyframe sections out in columns

- Priority: P2
- Affected files: `internal/gen-manifest`, `internal/engine/snapshot_*_gen.go`,
  `internal/snapshot/encoder.go`, `internal/snapshot/codec.go`, `tool/vif-log`
- Guide: [Netcode design §3.7, §4](netcode-design.md#a-columnar-keyframe-codec)

A td keyframe deflates to 355 kB as JSON rows and to 14 kB as the same cells in
columns, entities as varint deltas in canonical order and a column per field, since
a row's entropy is its entity id (netcode design §3.7); binary rows alone deflate
5% larger than the JSON. Generate the column layout per component beside the
stores, send a keyframe and a delta's changed rows in it, keep the manifest's shape
over each row's canonical encoding, render a body as rows in `vif-log`; `Schema`
and the protocol bump. The guest's 141 ms decode of 10.6 MB of JSON goes with it.

### Hold retained indexes by reference

- Priority: P3
- Affected files: `internal/snapshot/encoder.go`, `internal/snapshot/manifest.go`,
  `internal/converge/selective.go`
- Guide: [Netcode design §3.7, §4](netcode-design.md#retention-by-reference)

Retained captures are about 3 MB each at td; their indexes hold 47 MB of per-row
arrays and cloned row JSON over four captures (`newStoreSection`, `ManifestRow`),
beside the row encoders' 24 MB of owned cells. Let a retained index share the pages
whose rows held with the index before it and reference the encoder's immutable cell
bytes rather than clone them, about a fifth of a td participant's heap. The spatial
grid is [its own item](#shrink-the-spatial-grid-on-ceiling-sized-maps).

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

### Read the cadence against a thin command stream

- Priority: P3
- Affected files: `pkg/linkpace`, `pkg/network/probe.go`

The divergence harness's heavy shape (netcode design §3.5) delivered the guest 134
manifests in 1,200 ticks before the binary wire codec, 132 with its envelope and 97
with its payload, late counts unchanged: the link meter reads throughput from the
bytes it delivered, and a command stream of a fifth the bytes reads as a slower
link, so the cadence lengthened under loss. Read what the meter and the floor
decide on a thin stream, and whether the probe's bytes should carry the reading.
The saturation signal's round-trip half reads inflation against the least trip
seen, and a link queued from its join on, as the cadence test's 200-byte-per-tick
link is, never shows its idle trip, so the flag flickers once a run settles near
1.5× of a congested baseline; the test reads the run rather than its last sample.

### Fold an unguarded chain into one crossing

- Priority: P3
- Affected files: `internal/fsm/machine.go`, `internal/engine/scheduler.go`
- Guide: [Netcode design §4 batch 2](netcode-design.md#4-phase-2)

A transition is the authority's and applies a playout lead after it was taken
([D-20](domain-design.md#d-20--shared-fsm-regions-move-on-the-authoritys-transitions)),
so a chain of states that each hand on by an unguarded `Tick`, the tower's setup
of ten hops, pays a lead a hop: 30 ticks on the harness, seconds at the site.
Taking the unguarded hop in place on every instance is safe only where nothing
else could have moved the region first: no guarded `Tick` before it in the leaf
and no event transition on the leaf or its ancestors, since the authority evaluates
those and a guest does not. Decide whether such states are marked at load and
taken in place, or whether the authority crosses the chain's end.

### Make the td readings repeatable

- Priority: P3
- Affected files: `internal/app/readings_test.go`

td's rows in `TestNetcodeReadingsOverSockets` vary run to run with what the storm
does to the roster: at four players keyframes ran at none, 0.13 and 0.53 a second
across runs on one machine, so a row compares two items only where their keyframe
rate agrees. Seed what varies, or run the row several times and report the median.

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
  `tool/vif-allocator`

`:player drop <slot>` lets a host drop a player with its bots, or one bot, and the
dropped person starts a new local game. What remains: a reason the dropped client
prints, playing on alone as after `:leave` instead of starting over, and an
operator route on the allocator, since a dedicated host has no console. Keeping a
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

### Drop the cursors a linked fork cannot reach

- Priority: P3
- Affected files: `internal/app/session.go`, `internal/app/converge.go`

`dropAbandonedCursors` acts only on an instance left with no link, so a script's or
a bot's fork still linked to its own bots keeps every roster entry, and a
participant it has no path to stays a cursor nothing moves
([Multi-player](multi-player.md#52-the-roster-after-a-loss) §5.2). A host already
keeps those its group lost as Wraiths; such a fork could cross the same departures
for its bots to apply.

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
the redirect and an authority, for a pod an allocator request, that starts from a
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
- Affected files: `internal/system/weapon.go`, `internal/system/bullet.go`, `internal/profile/combat.go`

PvP: a hit on a remote cursor crosses as its impact and the victim's owner applies it, as `strikeCursor` does for mounts.

### Tune the charge-cap effects after play tests

- Priority: P1
- Affected files: `internal/parameter/{combat,player,effect}.go`,
  `internal/system/weapon.go`

Arc reward (an arc pays the lightning's full energy reward; halve it if the cap doubles the rod's income too far), the sustained ray's scale (three damage per covered member per immunity window, continuously), the burn figures and whether the charged pulse's stun should shorten to offset them, `LightningArcRange`, `WeaponStreamInterval` and `TurretStream`, the gun's lane offsets at two charges, `LightningZapDuration` for an arc, and a burn tint on species renderers, which show only the hit flash per tick now.

### Direct the gun by scenario

- Priority: P3
- Affected files: `internal/component/weapon.go`, `internal/system/weapon.go`, `internal/fsm`

`WeaponSpec.Directions` is a cardinal mask the spawn already reads; a side-scroller needs a level to set one direction, which waits on scenario authoring.

### Move storm's green circle onto a disruptor mount

- Priority: P3
- Affected files: `internal/system/storm.go`

Its area pulse is the hosted disruptor's shape; the red circle's turret mount is the pattern.

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

### Give the configuration menu a touch target on Android

- Priority: P1
- Affected files: `internal/engine/overlay_geometry.go`, `internal/render/renderer/overlay.go`,
  `internal/mode/config_menu.go`, `internal/app/presentation.go`, `android/game/view.go`,
  `android/app/src/main/java/com/lixen/vif/GameView.java`

A menu row is one cell, 12 dp on a phone against Android's 48 dp touch target, so
a tap lands on a neighbour. Three ways, in rising cost:

1. A row pitch on a touch host. `OverlayGeometry` gains a pitch the renderer and
   `pointConfigMenu` share: 1 on a terminal, 3 or 4 where the presentation
   reports touch (one method on `Presentation`, true for Android and the browser's
   touch strip), so a row is 36 to 48 dp and the list shows a third of its rows a
   page. The menu stays the `tui` one; every host behaves the same; smallest change.
2. A menu the host draws. The overlay content (`core.OverlayMenu`: rows with key,
   label, value, description and disabled reason; the form) crosses the binding as
   it changes, through an optional `menuPresentation` on `Presentation`
   (`ShowMenu`, `HideMenu`), and the host answers with the keys the router already
   takes plus a select-by-key the terminal vocabulary has no event for, so a new
   event or a call into the router under the world lock. Java draws a radial menu
   (the console style) or a list with 48 dp targets; the browser could do the same
   in HTML. A second renderer for one screen and a protocol for its state.
3. A separate Activity or bottom sheet: 2 with Android's own widgets, and the
   game's Surface left behind while it shows.

Recommended: 1 now, since it serves the browser's touch strip too; 2 only if a
radial menu is wanted for its own sake, after 1 shows what a bigger row is worth.
The typing strip above the keyboard already repeats the status row from the same
picture (`CellDrawer.drawStrip`), the pattern 2 would extend.

### Configuration menu follow-up

- Priority: P2
- Affected files: `internal/mode/config_menu.go`, `internal/paths/settings.go`
- `[play]` persists the audio channels, sfx volume, auto-fire, mouse, pointer
  following and the HUD, and a scenario change carries them over. What `:new`
  keeps beyond those (rate, pins, standing breakpoints) a restart or scenario
  change still drops. Music gain needs an authoritative readback before joining
  the sfx volume in the menu. Keymap, backend, paths and colour depth remain
  startup settings; `:d break` stays on the command line, since a row cannot take
  its arguments.

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

### Take the ember further only with a visual trade-off

- Priority: P3
- Affected files: `internal/render/renderer/ember.go`

The painter's remaining cost is the ring pass, about two fifths of a low-heat
paint, and the per-frame 256-bucket jagged table; both are exact today. What is
left needs a compromise: a coarser table for the ring Gaussian keyed by the
squared plane distance, fewer jagged buckets, or ring evaluation only inside the
band the pulse can light. None is visible in principle, none is bit-identical.
Revisit only if `:d prof` ranks `draw ember` high again.

### Sample the kraken's tentacles more coarsely

- Priority: P3
- Affected files: `internal/parameter/kraken.go`, `internal/component/kraken.go`

What the kraken still costs scales with its tentacle samples, about 780 discs a
tick at `KrakenSampleStep` 0.35, which the footprint and the shading each stamp.
A step of 0.7 in the browser build took the first frame after a tick from about
0.35 to 0.24 ms and the kraken region's tick from about 0.9 to 0.8 ms, at a
visible cost over the same poses: 0.6% of drawn cells appear or vanish at the
edges, 1.8% take another rim glyph and 0.5% another color, and the footprint's
edges move by a cell in places, so contact and wall clearing shift with them.
Revisit only if `:d prof` ranks `sys kraken` or `draw kraken` high again.

## Audio

### Rename the audio mute event to the audio vocabulary

- Priority: P2
- Affected files: `internal/event/type.go`, `internal/event/payload.go`,
  `internal/system/audio.go`, `internal/mode/config_menu.go`, `doc/audio.md`

`EventSoundMuteToggle` changes the audio channel mask, sfx and music alike, so it
is `EventAudioMuteToggle`, the request beside `EventAudioMuteChanged`, with its
payload. It waits for the netcode work to settle the event registry and journal
schema the rename regenerates and bumps.

### Spin off `pkg/audio` as its own module

- Priority: P2
- Affected files: `pkg/audio`, `pkg/audio/model`, `pkg/tomlmacro`, `cmd/soundlab`

Two in-repository imports remain: `pkg/vmath` for three hash and stream
constants, which inline, and `pkg/tomlmacro` for the macros both document
loaders expand, which moves with it or into `lixenwraith/toml`.

## Genetic library

### Spin off `pkg/genetic` as its own module

- Priority: P2
- Affected files: `pkg/genetic`, `pkg/genetic/registry`, `pkg/genetic/fitness`,
  `pkg/genetic/tracking`, `pkg/genetic/persistence`, `sandbox/genetic`

The audit before the spin-off left these for the new module, none of which the
game needs. The pending table is direct-mapped, so a long evaluation is evicted
by issue distance even when the table is nearly empty; bounded linear probing
that evicts the oldest id in its window would make `PendingCapacity` a live-count
bound, at the cost of a checkpoint-format version. The streaming engine breeds as
soon as two outcomes exist; breeding only once the archive is full was measured
on the lander example and changed nothing, so it stays unimplemented. The batch
`Engine` spawns one goroutine per candidate under a semaphore and reallocates
offspring every generation; a fixed worker set over an atomic index and a free
list would remove both. `RouletteSelector` rebuilds its cumulative table on every
`Select`; cache it by archive version. `registry.Stats` restates
`genetic.PoolStats` field for field and `SpeciesConfig.PerturbationStdDev`
restates `EngineConfig.PerturbationStrength`; collapse each pair. The `fitness`
context keys and most `tracking` metric keys are conventions of this game, and
the `persistence` default codec is the one external dependency: make JSON the
default and move the TOML codec to the caller, or drop the subpackage.

## Engine

### Initialize systems from the engine at startup too

- Priority: P3
- Affected files: `internal/app/app.go` (`initWorld`, `initScheduler`), each
  system's constructor in `internal/system`, the system tests

A reset re-initializes every system through `World.InitSystems` in dependency
order; at startup each constructor still calls its own `Init`. Moving that call
into the engine has to keep three things: the journal anchor reads the RNG session
the construction-time draws advance, before the scheduler exists; `MetaSystem` is
added once the context exists; and the system tests construct systems standalone
and expect them usable.

### Give the composite lifecycle methods a user

- Priority: P3
- Affected files: `internal/system/composite.go`

`CompositeSystem` carries `handleEmptyComposite`, `CreateHeader`, `AddMember`,
`DestroyComposite` and `GetAnchorForMember`, designed for building and tearing
down composites at run time, but no system or scenario uses them. A species or
scenario that assembles composites would; it reaches them through events, since
another system may not call them directly.

### Let a paused run sleep

- Priority: P2
- Affected files: `internal/engine/scheduler.go` (`eventLoop`), `internal/app/loop.go`
  (`Loop`'s tickers), `android/app/src/main/java/com/lixen/vif/GameView.java` (`doFrame`)

A paused run keeps every clock of a running one: the event loop wakes each 4 ms
with or without a queued event, the input tick each 16 ms, the frame tick at 60 Hz,
and on Android the render thread asks for a frame at each vsync. On the Pixel 7
emulator that is 19 to 23% of a core on the start screen and 9% with the app in
the background, where nothing is drawn (2026-10-22); the background is what costs
a phone its battery. While a solo run is away, the event loop could wait on a push
instead of its ticker, the app loop drop its input and frame tickers until a report
or event comes, and the render thread stop its vsync callback once it has painted
the last frame flushed.

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
