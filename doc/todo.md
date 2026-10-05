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
replay landed in the next pass. Relays (phase 6) precede fleet work (4), richer
decision logic (5), and learning.
Seat instance log tags and whether seats should inherit their holder's terminal
geometry remain open. Permanent holder-local admission as a guest waits for relays.
The overlapping host-and-bot departure defect is covered by the holder-subtree
handoff rule and socket regression in troubleshooting §13. Admission through a
still-guest holder after the original host leaves remains relay work.

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

### Reduce exact snapshot and navigation costs at the map limit

- Priority: P1
- Affected files: `internal/snapshot`, `internal/app/capture.go`,
  `internal/engine/snapshot_delta.go`, `internal/gen-manifest`, `pkg/navigation`

At td with 16 players the host spends about 1.2 of its 1.4 CPU-s/s on protocol and
an instance averages about 250 MiB of heap, against the pod's 500m and 160 MiB
(multi-player §6). Per round the seal (about 65 ms) now costs twice the index, whose
remainder is mostly the static wall positions. Each step below must stay exact:

- Reuse the wall store's own encoding in `Integrity` and the capture encoders, under
  the builder's owned-value comparison: each seal and keyframe re-encodes every wall.
- Reuse the row encodings of unchanged entries in every store. That also replaces
  the wall special case in `ManifestBuilder` with a table indexed by store.
- Generate typed comparisons for `DiffCapture` and `countStoreDifference`, which
  call `reflect.DeepEqual` per entry.
- Measure navigation's share again, then exact storage and loop work in
  `FlowField.Compute` with identical directions, distances and throttle phase.

Per-store write counters stay rejected: a cache compares values it owns.

### Delegate convergence to relays

- Priority: P1; bots phase 6, ahead of fleet (4) and decision logic (5)
- Affected files: `internal/converge/relay.go`, `internal/converge/selective.go`,
  `internal/converge/correction.go`, `internal/system/network.go`, `internal/network`
- Plan: [Bots](todo-bots.md) §4

Goal: a player who cannot reach the host joins through another player, and a player
who relays lends the session uplink and CPU without becoming its host, though it
stays a succession candidate. The host commits alone, so this is not a merge: a relay
that committed crossings or sent up a merged world would give one crossing two apply
ticks and let a peer write the canonical world. The relay owns its subtree's
convergence instead, in the order the plan gives: admission through a relay with the
join served from its own proved world, one subtree proof upstream, bundled epochs,
ingress only through the relay, then people behind relays with re-parenting and NAT.
A guest's bot is the first leaf: no hop, shared fate, no hole punching.

Expected: leaves converge like direct guests without the stopgap's whole world per
floor window through every link, which then goes with the lead sampled from returned
commits. The host's correction work and fan-out follow its direct links: sixteen
players as four relays of three cut its epoch fan-out from (N−1)² to about 4N copies
a tick, while each relay pays its leaves' share. About 800–1,000 lines with tests;
high complexity: two-hop staleness, root verification of a relay's world, a new
request field and keyframe marker behind a `ManifestVersion` bump, re-parenting, and
relayed topologies under link shapes. §4.2 of domain-design and multi-player §2, §4
and §5 change with it.

### Decide the authority's fan-out and publication cadence

- Priority: P2
- Affected files: `internal/system/network.go`, `internal/converge/correction.go`

A star's authority relays every participant's epochs and owner syncs to every other:
at 16 players 4,444 epochs and 736 syncs a second, 1.4 MB/s framed on the tower map
and 2.8 MB/s on td before deflate. Rounds follow the union of peer deadlines, 7.2 a
second at td, and whole bodies add 2.2 MB/s there; while anyone is behind a relay the
floor adds one per window through every link. Candidates, each a message-set or
cadence change that cuts constants where delegated relays move the quadratic term
off the host: one bundle per guest per tick carrying every source's committed frames
with their epoch markers, which pacing and fences read; owner syncs as deltas; and
rounds aligned across peers.

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

### Pace repairs as bodies are paced

- Priority: P3
- Affected files: `internal/converge/selective.go`, `internal/snapshot/shard.go`

A repair set leaves as one frame of up to `SnapshotShardBytesMax` (48 kB), so on a
link at the tower's floor it can hold the epochs behind it for two seconds, where a
body now waits behind one 4 KiB chunk ([Troubleshooting](troubleshooting.md) §16).
Carrying it in the correction chunk envelope through the per-peer outbox bounds that
too. Repairs at 20 kB/s have so far been a few kilobytes.

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

### Tune and extend Kraken after gameplay trials

- Priority: P2
- Affected files: `internal/{component,system,render/renderer}/kraken.go`,
  `internal/parameter/kraken.go`, `internal/profile/mass.go`, `wad/scenario/{kraken,main}/`

Tune health, contact damage, mass and pacing in the looping Kraken scenario and main maze.
Measure the sampled tentacle hitboxes and retained member pool under multiple
Krakens; add swept tentacle contact if fast motion skips cursor cells. The first
renderer uses the sandbox's default void body; other body profiles remain optional.

### Let mounted weapons strike more than cursors

- Priority: P3
- Affected files: `internal/system/mount.go`, `internal/profile/combat.go`

A mounted beam destroying walls and glyphs, mounted weapons striking other species, and each instance's own drains; Shared outcomes must resolve from Shared geometry, not a Player-domain shot.

### Let cursor weapons strike other cursors

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/system/bullet.go`, `internal/profile/combat.go`

PvP: a hit on a remote cursor crosses as its impact and the victim's owner applies it, as `strikeCursor` does for mounts.

### Fan a fully charged beam into spokes

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/component/beam.go`

Charges beyond the ray could add rays 360°/n apart, one through the orb; a sweep already passes each target once a turn, so spokes trade coverage for crossings.

### Move storm's green circle onto a disruptor mount

- Priority: P3
- Affected files: `internal/system/storm.go`

Its area pulse is the hosted disruptor's shape; the red circle's turret mount is the pattern.

### Route species contact damage through strikeCursor

- Priority: P3
- Affected files: `internal/system/{quasar,swarm,storm,eye,pylon,snake,drain}.go`

Each still writes the shield-drain-or-heat pair by hand; `TestSharedCursorOverlapOutcomesStayOwnerResolved` counts must move with it.

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

## Replay

### Step a replay back past its trailing copies

- Priority: P2
- Affected files: `internal/app/play.go`, `internal/engine`, `internal/snapshot`

`,` presents the `parameter.ReplayBackSpares` copies trailing a replay at once; each
tick further back waits for a copy replaying from the stream's start, about 0.75 ms a
tick at four players, 63% of it `FlowField.Compute`. Replaying from a checkpoint
instead needs a whole-world clone: the player domain, each system's private state,
the queue and the replay cursor, which a shared capture leaves out.

### Journal a world digest and name the first tick a replay leaves its run

- Priority: P2, for a second evaluation
- Affected files: `internal/event/journal.go`, `internal/journal/replay.go`,
  `internal/snapshot/digest.go`, `internal/app/replay.go`

A replay that leaves its run is caught only at the next written world ("the world
before it differed"), and a host journal has none. Finding the tick took per-tick
world dumps from the live and replayed runs and a diff of the two. A
`snapshot.DigestWorld` line journaled every N ticks as a note, and compared by
`ReplayDriver`, would name the first tick and component class that differ; a
headless verify of a journal would run it unattended. Measure the digest's cost
per tick at the map limit before choosing N.

## Logging

### Audit logging across the repository

- Priority: P2, a session of its own
- Affected files: `internal/vlog`, every producer

vlog is process-global and most records say `app`, so a log cannot tell which App,
seat or replay copy wrote a record, and a scope selects little:

- About 150 of 180 call sites use `app`: network, session, convergence, bots,
  journal and replay share it. `net`, `system` and `domain` are absent from
  `subScope` and fall to `tap`, which also gates the domain audit.
- Each App owns a correlation but records carry the global one, so a seat's records
  bear its holder's tick, or none under `-headless`, and a replay needed `vlog.Mute`
  and a lock to keep its copies out. A per-App handle carrying stamp, tag and gate
  would replace both and settle the seat log tags left open under bots.
- Levels and keys drift: FSM transitions at Info run to thousands a session, lock
  "long hold" warns on CPU contention rather than a defect, duration fields mix `ms`
  and `us`, and `msg` is a noun phrase in some places and a verb in others.

Settle one table of subs, scopes and levels with message and field rules, check
that hot sites guard with `vlog.On`, then bring every call site to it.

## Fleet logging

### Repin LogWisp past the fleet stream fixes

- Priority: P1
- Affected files: `deploy/logwisp/REVISION`
- Prerequisite: the quiet-stream keepalive and the rotated-file resume reaching
  LogWisp `main`, which the installer requires the pin to descend from

`REVISION` names v0.18.1, which predates both. Until it moves, a vacant node still
idle-expires a connected viewer and evicts it on the next session's first record,
and a session crossing the 8 MiB file cap still replays its rotated log whole,
spending the rate limit on duplicates while live records drop.
[Deploying the session fleet](kube-docker-deploy.md) §10 states what the pin must
carry.
