# TODO

Scratch notes that travel with the repository. Each item states what remains and
the smallest thing that would close it. Delete an item when it lands; this file
is not a changelog. Source files do not carry parallel TODO comments, so a
code-originated item names its source here.

Priorities: P0 blocks a release or leads the feature development push, P1 is
wanted next, P2 is convenient follow-up, and P3 is an idea.

## Feature development push

Bots come before the Android host. They need no presentation refactor, build on
the scripted and headless paths that exist, give the session stack and the fleet
continuous real play, and make the site playable alone. A bot is also the first
non-terminal source of semantic input, the contract the Android extraction has to
define first; doing Android first would define it without a second consumer.

### Add bots that play as participants

- Priority: P1, leading the push; its P0 phases have landed
- Affected files: `internal/network`, `internal/app`, `internal/render/renderer`,
  `tool/vif-allocator`, `internal/bot`
- Plan: [Bots](todo-bots.md)

A bot is its own headless instance whose FSM graph drives its router with intents,
keys and mouse alike, bound by the same barrier, admission and eviction as a person.
Bots belong to the instance that started them: a host's stay as participants, a
guest's leave with it, and every composition of people and bots joins through the
ordinary handshake. Phases 1–3, hexadecimal cursor slots and the merged `default`
policy landed. Holder ownership, slot removal, explicit bot run modes and bounded
replay landed in the next pass, and a guest's bots now join and play through it
(phase 6, R1–R4). People behind relays (R5) precede fleet work (4), richer
decision logic (5), and learning.
Whether seats should inherit their holder's terminal geometry remains open.
The overlapping host-and-bot departure defect is covered by the holder-subtree
handoff rule and socket regression in troubleshooting §13.

### Extract the renderer-neutral Android host model

- Priority: P1, after bots
- Affected files: terminal-shaped input/cell/color values, `internal/app`, host
  entry points
- Prerequisite: the bots' input seam; define the minimum visual contract

Target a minimally polished Android build within one month of development time.
Move terminal-specific visual and input values behind positive renderer/host
adapters, add a library entry point, and keep lifecycle, simulation, networking,
and resource providers common. Touch input maps onto the semantic input bots
already drive, and [Multi-platform](multi-platform.md) lists the terminal-shaped
values that remain.

## Authoring

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
- **Vocabulary.** Action, guard and event pickers read `-schema`, which lists names
  only until the argument catalog lands.
- **Map layer.** Geometry is generated at run time (`EventLevelSetup`, maze rooms,
  `.vifimg` walls). A painted map needs a static form the digest covers, and
  `.vifimg` assets come from the `image/` category, which it does not and a joiner
  is not sent. Decide between a TOML wall encoding (rectangles or run-length rows)
  and admitting `.vifimg` to the scenario container under its bounds.
- **In the game.** An editor mode beside command and visual in `internal/mode` acts on
  a paused world and draws through an overlay like `:graph`. Applying an edit
  validates with `resource.ValidateScenario` and restarts on it as `:n` does, since
  a run plays one scenario. Editing inside a multiplayer session is out of scope.

Phases: the argument catalog; reference kinds, order and provenance; a read-only
graph of a scenario's regions, states and blocks; placing and editing blocks into
editor-owned files; the map layer and point markers; root `[[expand]]` blocks and
library copies.

## Browser sessions

### Serve wss from vif's own listener

- Priority: P3
- Affected files: `cmd/vif`, `internal/network/websocket_other.go`

`-host`/`-serve ws://` is plain: a page served over `https` cannot open it. Taking a
certificate and key would wrap the listener's TCP port in `tls.NewListener` and let
`wss://host:port` bind; decide where the key lives before adding the flags.

### Tighten the per-address budgets

- Priority: P2
- Affected files: `deploy/guest/vif-allocator.env`, `tool/vif-allocator`

The allocator budgets each player address 6 joins a minute over both proxied
routes and 4 creations per `-first-join` window, loose while one address carries
several test clients; the target is one of each. One NAT is one address, so loosen
joins before creations if households report refusals, and key IPv6 on its /64
first.

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
Argon2-SCRAM dependency.

### Fetch a content-addressed bundle over HTTP

- Priority: P1
- Affected files: `internal/resource`, browser resource provider
- Prerequisite: decide publisher trust, allowed origins, and cache policy; the
  format and the limits are `resource.Scenario`'s and are already decided

Two of the three routes to content exist. A native player downloads the release
wad and extracts it over a config root; a guest joining a session is served the
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

## Packaging

Distribution-specific detail lives in [Packaging](packaging.md).

### Promote a nightly build to the first stable release

- Priority: P0
- Affected files: `doc/packaging.md`, `README.md`
- Prerequisite: choose a nightly commit after the complete verification gate
  passes

Tag the selected commit `v0.1.0` and publish the draft release the tag creates,
which already carries the source archive and its checksum; then drop the "no final
release" lines in `README.md` and [Packaging](packaging.md) §2.

### Publish a development AUR package

- Priority: P3
- Affected files: AUR packaging metadata and `doc/packaging.md`
- Prerequisite: establish the release-package metadata first

Publish `vif-git` alongside the release-based AUR package.

### Let a request name the map size

- Priority: P2
- Affected files: `tool/vif-allocator/config.go`, `tool/vif-allocator/allocator.go`,
  the Hugo site's `vif-fleet.js`

`-map-size` is the deployment's for every session, and the session page cannot
select it. A scenario that fixes its own dimensions ignores it — `td` emits
`EventLevelSetup` at 500x250 — so the flag only decides the scenarios that do not,
and those all run at one size. Offer it the way `scenarios` is offered, bounded by
`parameter.MaxMapCells` rather than by a list: `limits` names the ceiling, a
request names a size under it, and a scenario that sets its own still wins.

### Provision audio to the fleet when a host needs it

- Priority: P3
- Affected files: `deploy/guest/update-vif-wad.sh`, `tool/vif-allocator/manifest.go`,
  `deploy/k3s/30-session.yaml`

What a fleet session reads from the node volume is one list in three places —
`categories` in the installer, `wadCategories` in the allocator, and the mounts in
the template. `scenario` and `image` are on it. `audio` goes on it the day a
dedicated host renders anything, and `content` never does. Nothing else changes;
the volume already carries whatever the installer puts there.

### Cache a received scenario to the user root

- Priority: P3
- Affected files: `internal/resource`, `internal/app/session.go`

A scenario received from a coordinator lives in memory and is dropped at exit, so
rejoining the same session downloads it again. Writing it under the user root
behind an explicit opt-in would keep it, and needs a trust decision first: the
bytes came from a peer, and nothing about a plaintext link says they are the
operator's.

## Multiplayer

Diagnoses and what each item follows from are in
[Troubleshooting](troubleshooting.md).

### Trim what remains of a tick and a checkpoint at the map limit

- Priority: P2
- Affected files: `pkg/navigation/flowfield.go`, `internal/system/interaction.go`,
  `internal/engine/world_copy.go`

At td with 16 players a tick is about 2.6 ms at `GOMAXPROCS=1`: flow field derivation
is about 45% of it and `checkCursorOverlap`'s position lookups a quarter. A
checkpoint scans the whole grid for its occupied cells, most of its 10 ms. Each must
stay exact; per-store write counters stay rejected, since a cache compares values it
owns.

### Delegate convergence to relays

- Priority: P1; bots phase 6 (R5), ahead of fleet (4) and decision logic (5)
- Affected files: `internal/app/relay.go`, `internal/converge/relay.go`,
  `internal/converge/authority.go`, `internal/system/network.go`, `internal/network`
- Plan: [Bots](todo-bots.md) §4

Goal: a player who cannot reach the host joins through another player, and a player
who relays lends the session uplink and CPU without becoming its host, though it
stays a succession candidate. The host commits alone and the relay owns its
subtree's convergence. A guest's bots already work that way (R1–R4): they join on
its in-process stream and install its proved world, their proofs ride its manifest
answer, their epochs its one bundle a tick, and the authority takes their traffic
only on its link, budgeted per participant it carries.

What remains is people behind a relay (R5). A person does not share a relay's fate,
so the relation needs a parent other than `Holder`, which a relay's departure
re-parents rather than retires. A relay for people is native and serves the same
relayed admission on a declared port, with a way through NAT; a browser relays only
its own bots. A leaf's lead is advertised along its path instead of sampled from
returned commits.

Expected: the host's correction work and fan-out follow its direct links: sixteen
players as four relays of three cut its epoch fan-out from (N−1)² to about 4N copies
a tick, while each relay pays its leaves' share. Verified by relayed topologies under
link shapes, asserting hash-only convergence and the authority's per-link fan-out.

### Decide the authority's fan-out and publication cadence

- Priority: P2
- Affected files: `internal/system/network.go`, `internal/converge/correction.go`

A star's authority relays every participant's epochs and owner syncs to every other:
at 16 players 4,444 epochs and 736 syncs a second, 1.4 MB/s framed on the tower map
and 2.8 MB/s on td before deflate. Rounds follow the union of peer deadlines, 7.2 a
second at td, and whole bodies add 2.2 MB/s there. Candidates, each a message-set or
cadence change that cuts constants where delegated relays move the quadratic term
off the host: one bundle per guest per tick carrying every source's committed frames
with their epoch markers, which pacing and fences read, as `MsgBundle` already does
upstream; owner syncs as deltas; and rounds aligned across peers.

### Project a correction outside the live lock

- Priority: P2
- Affected files: `internal/app/snapshot_stage.go`

At td an install that projects nothing already holds the live lock 40–70 ms for the
capture pair, comparison and write, and each projected tick adds up to a simulation
tick, so a repair 16 ticks behind holds it past 100 ms. Reduce the O(E) part first.
Any out-of-lock or incremental design must keep §3.3: fence membership, pending
barrier crossings not fed, the ledger settled before projection, the journal's place
and mark, and no live tick between the final projection and the write. WASM gains
scheduling time, not parallel CPU.

### Hold the next run's keyframe through a reset

- Priority: P3
- Affected files: `internal/converge/correction.go`

A guest still behind its host at a reset, by up to the barrier lead, refuses the
host's first keyframe of the new run as another run's, and every delta against it,
until the next keyframe: at most `SnapshotFloorKeyframeTicks` of whole-body silence.
Holding a keyframe of the next run until the guest enters it, through
`followRunLocked`, closes that window.

### Tell both ends why a join failed

- Priority: P3
- Affected files: `internal/app/session.go`, `internal/network/session.go`,
  `internal/network/endpoint.go`, `internal/app/loop.go`

A bind or dial now names the player's next step; these gaps remain:
- A playing host never learns why a joiner was refused: accept and handshake
  failures only reach the log, and `port.Errors()` is drained only by a lobby.
- A refusal written after the joiner's reply goes out raw while the joiner reads
  deflate, so it reads a stream error instead of the reason (from reading; unrun).
- A site request can run 2 minutes, past the 5 s its "Joining" notice lasts.
- A rejoin shows nothing for up to `SessionRejoinWindow`.
- Windows' WSAE codes miss `plainNetError` and keep the system's words.

### Pace repairs as bodies are paced

- Priority: P3
- Affected files: `internal/converge/selective.go`, `internal/snapshot/shard.go`

A repair set leaves as one frame of up to `SnapshotShardBytesMax` (48 kB), so on a
link at the tower's floor it can hold the epochs behind it for two seconds, where a
body now waits behind one 4 KiB chunk ([Troubleshooting](troubleshooting.md) §16).
Carrying it in the correction chunk envelope through the per-peer outbox bounds that
too. Repairs at 20 kB/s have so far been a few kilobytes.

### Give the silent-peer test room under the bulk floor

- Priority: P2
- Affected files: `internal/app/correction_test.go`, `internal/network/mesh.go`

`TestASilentPeerIsSentWholeBodies` keeps the guest from ticking, so nothing drains the
loopback link and the host's backlog stands at 8188 bytes of the 8192-byte
`SnapshotBulkQueueBytes` floor on the publish that owes the body; the loopback link
measures no throughput, so the floor is the whole limit. Any growth of a cursor's
snapshot entry trips `bulkHeld` and the body is withheld as if the link were
saturated. Either have the loopback link report a throughput, so the limit scales as
it does on a real link, or drain the guest without answering the index.

## Session membership

A suggestion for the words, not a decision: a *session* is one world timeline under
one authority — a fleet pod, a terminal host, any instance — and its participants,
and solo play is a session of one. Commands then move participants between sessions:
`:open`/`:close` in place of `:host`, since every instance already is its own
session's authority; `:join`/`:leave` for a participant; `:drop` for an authority
removing one; `:merge`/`:split` for whole sessions.

### Leave a session and play on alone

- Priority: P2
- Affected files: `internal/mode/commands.go`, `internal/app/loop.go`,
  `internal/help/topics.go`

`:leave` ends this instance's part in a session on purpose and keeps its world: it
closes its link, so the authority crosses the departure now rather than on a
timeout, and continues as its own authority. A guest takes the local-fork path it
takes on losing its host ([Multi-player](multi-player.md) §5.1) without the
`Host lost` badge; an authority closes its listener first, and its guests succeed
or fork as its `-authority` policy says.

### Start a host's journal mid-session

- Priority: P3
- Affected files: `internal/app/app.go`, `internal/app/loop.go`

`:journal start` replaces the run, and a host's world holds its guests' cursors, so a
resumed host would carry them into a run they are not in. A host would resume with
those participants departed and reopen its door, as a scenario change does, for its
guests to rejoin.

### Let an authority drop a participant

- Priority: P2
- Affected files: `internal/network`, `internal/app`, `internal/mode/commands.go`

`:drop <participant>` closes that participant's link with a reason its client prints,
and the dropped instance plays on alone as after `:leave`. A dedicated host has no
console, so a fleet drop is a later operator route on the allocator. Keeping a dropped
player out is a separate decision: the session name is a routing key, not an identity.

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

## Combat

### Let stronger knockback override the active immunity window

- Priority: P2
- Affected files: `internal/component/combat.go`, `internal/system/combat.go`,
  `internal/profile/combat.go`

Record the strength of the knockback that opened immunity. A stronger incoming
knockback should reset the immunity duration and apply its impulse; equal or weaker
hits remain blocked across all weapons and players. Carry the recorded strength
through snapshots and synchronization. Stun immunity remains target-wide.

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
- Affected files: `internal/parameter/{combat,player}.go`, `internal/component/tracer.go`

Arc reward (an arc pays the lightning's full energy reward; halve it if the cap doubles the rod's income too far), the sustained ray's scale (three damage per covered member per immunity window, continuously), the burn figures and whether the charged pulse's stun should shorten to offset them, `LightningArcRange`, `WeaponStreamInterval` and `TurretStream`, the gun's lane offsets at two charges, `LightningZapDuration` for an arc, and a burn tint on species renderers, which show only the hit flash per tick now.

### Drop the gun's charges as loot

- Priority: P2
- Affected files: `internal/component/loot.go`, `internal/profile/loot.go`

Only a scenario's `EventWeaponAddRequest` or god mode raises the gun past one charge; a loot row with a drop tier would let play do it.

### Direct the gun by scenario

- Priority: P3
- Affected files: `internal/component/weapon.go`, `internal/system/tracer.go`, `internal/fsm`

`WeaponSpec.Directions` is a cardinal mask the spawn already reads; a side-scroller needs a level to set one direction, which waits on scenario authoring.

### Fan a fully charged ray into spokes

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/component/ray.go`

Charges beyond the ray could add rays 360°/n apart, one through the sustained orb ray; a sweep already passes each target once a turn, so spokes trade coverage for crossings.

### Move storm's green circle onto a disruptor mount

- Priority: P3
- Affected files: `internal/system/storm.go`

Its area pulse is the hosted disruptor's shape; the red circle's turret mount is the pattern.

## Rendering

### Configuration menu follow-up

- Priority: P2
- Affected files: `internal/mode/config_menu.go`, `internal/paths/settings.go`
- Persist operator preferences through an explicit save action and startup
  loading; the live menu currently changes only this run. Music gain needs an
  authoritative readback before joining effects volume in the menu. Keymap,
  backend, paths and colour depth remain startup settings.

### Use sixteen console backgrounds on Linux

- Priority: P3
- Affected files: `internal/render/console.go`

The framebuffer console shows a bright background under SGR 5, which vgacon blinks instead; telling them apart would give Linux FreeBSD's sixteen backgrounds.

### Draw the flow-field debug view from console glyphs

- Priority: P3
- Affected files: `internal/render/renderer/flow_debug.go`

Its diagonal arrows and `◆●` are missing from console fonts, so in 256 colours the view shows their fallbacks.

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
