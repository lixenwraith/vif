# TODO

Scratch notes that travel with the repository. Each item states what remains and
the smallest thing that would close it. Delete an item when it lands; this file
is not a changelog. Source files do not carry parallel TODO comments, so a
code-originated item names its source here.

Priorities: P0 blocks a release, P1 is wanted next, P2 is convenient follow-up,
and P3 is an idea.

## Browser sessions and mobile

### Add the native WebSocket transport

- Priority: P0
- Affected files: `internal/network`, `internal/app`, `internal/engine`, browser
  launch code
- Prerequisite: generalize lobby and session composition away from the concrete
  `network.SocketPort`

Implement binary WebSocket as a second `engine.NetworkPort` adapter while keeping
the existing framed TCP adapter for native clients. The pod must speak WebSocket
itself; do not put a WebSocket-to-TCP translation layer between the allocator and
the game. Preserve frame and queue bounds, timeouts, close propagation,
backpressure, and the current handshake before enabling `-join` in WASM.

### Expand vif-allocator into the browser session adapter

- Priority: P0
- Affected files: `tool/vif-allocator`, `deploy/k3s`,
  `deploy/website/vif.nginx.example`
- Prerequisite: the pod-native WebSocket listener above

Build the public path in this order:

1. Reconcile each live session identifier to its one ready pod IP and private
   WebSocket port; Kubernetes objects remain the authority after restart.
2. Accept only an Upgrade request at the final route
   `/vif/ws/<session>`; this endpoint's scope has no version segment.
3. Validate method, session syntax, same-origin `Origin`, liveness, readiness,
   connection ceiling, and handshake deadline before upgrading; callers never
   select an upstream address.
4. Reverse-proxy the upgraded connection to that pod's native WebSocket listener
   with bounded buffers and coupled cancellation when either side or the session
   ends; do not translate it to TCP.
5. Add the private container port and the narrow ingress policy needed for the
   node allocator, without publishing another NodePort; retain raw TCP for native
   clients.
6. Return `wss://lixen.com/vif/ws/<session>` for browser launch and pass it through
   the existing page-to-WASM argument bridge, then verify the production TLS/CSP
   path under slow links, tab suspension, reconnects, and session expiry.

At that point allocation, routing, and admission are separate interfaces inside
one process. Reassess the `vif-allocator` name and split boundary before adding
more control-plane duties.

### Add browser admission authentication

- Priority: P1
- Affected files: allocator/session adapter, website, future authentication
  dependency
- Prerequisite: the unauthenticated native WebSocket path is bounded and measured

Issue a short-lived, session-scoped admission credential after authentication and
consume it during the WebSocket handshake without putting it in page history or
logs. Decide whether the expanded allocator remains the credential boundary or is
renamed/split before adding the planned `github.com/lixenwraith/auth`
Argon2-SCRAM dependency.

### Add verified downloadable content bundles

- Priority: P1
- Affected files: resource providers, browser/native admission, release assets
- Prerequisite: decide the bundle format, compressed/expanded size limits,
  publisher trust, allowed origins, and cache policy

The scenario half is done: `resource.Scenario` is content-addressed, a coordinator
serves its own digest during the join handshake, and the receiver verifies before
constructing its `App`. What remains is the corpus, which travels as a fingerprint
and not as bytes, and a download surface for a client that has neither — the
nightly release page is the first candidate.

### Extract the renderer-neutral Android host model

- Priority: P1
- Affected files: terminal-shaped input/cell/color values, `internal/app`, host
  entry points
- Prerequisite: define the minimum visual and semantic-input contract for the app

Target a minimally polished Android build within one month of development time.
Move terminal-specific visual and input values behind positive renderer/host
adapters, add a library entry point, and keep lifecycle, simulation, networking,
and resource providers common.

## Packaging

Distribution-specific detail lives in [Packaging](packaging.md).

### Promote a nightly build to the first stable release

- Priority: P0
- Affected files: `.github/workflows/nightly.yml`, release artifacts and
  `doc/packaging.md`
- Prerequisite: choose a nightly commit after the complete verification gate
  passes

Promote the selected commit to `v0.1.0` and publish a byte-stable source tarball
with its checksum. Keep the moving nightly prerelease and headless GHCR image as
development artifacts rather than treating them as the stable source archive.

### Install a manual page

- Priority: P1
- Affected files: `cmd/vif/usage.go`, `doc/vif.1`, `Makefile`
- Prerequisite: keep the flag table as the source of truth

Generate `vif.1` from the flag table and install it through `make install`.

### Add a desktop launcher

- Priority: P2
- Affected files: packaging assets and `Makefile`
- Prerequisite: select an installable icon

Add a `.desktop` entry with `Terminal=true` and install its icon.

### Add shell completion

- Priority: P2
- Affected files: `cmd/vif/usage.go`, completion assets, `Makefile`
- Prerequisite: choose generated or maintained completion definitions

Provide shell completion for `vif` and install it in the appropriate data path.

### Publish a development AUR package

- Priority: P3
- Affected files: AUR packaging metadata and `doc/packaging.md`
- Prerequisite: establish the release-package metadata first

Publish `vi-fighter-git` alongside the release-based AUR package.

### Let the whole tree build headless

- Priority: P3
- Affected files: `cmd/soundlab`

`go build -tags=vif_headless ./...` fails: `cmd/soundlab` is fourteen untagged
files over `parameter.BuiltinSounds`, which the tag removes. The gates and
`script/test.sh deploy` therefore build `./cmd/vif` alone, as the image does, so a
break confined to soundlab is invisible. Tag the package out, or give it a stub.

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

### Serve a scenario over HTTP for browser builds

- Priority: P3
- Affected files: `internal/resource`, browser resource provider

A WASM build has no config roots and no peer to receive from until it has joined.
An HTTP-backed provider reading the same content-addressed container is what
[Multi-platform](multi-platform.md) already anticipates.

## Audio

### Surface malformed user sound configuration during play

- Priority: P1
- Affected files: `internal/service/adapter_audio.go`
- Prerequisite: choose the in-game error surface

Fallback currently succeeds silently during play while `-check` reports the
error. Expose the latched `AudioEngine.SpecError()` without making fallback
fatal.

### Move built-in music patterns into an editable asset

- Priority: P2
- Affected files: `cmd/soundlab/session.go`, `pkg/audio`,
  `internal/asset/audio/music.toml`
- Prerequisite: define and validate the pattern document

Replace the built-in Go registry literals with `internal/asset/audio/music.toml`
so `soundlab` edits the same data the game loads.

### Make the mixer buffer configurable

- Priority: P3
- Affected files: `pkg/audio/params.go`
- Prerequisite: define a construction-time option and recompute dependent buffer
  sizes before the mixer or backend starts

Replace the compile-time 50 ms mixer buffer with a construction-time setting.

### Decide reveal behavior for manual intensity decreases

- Priority: P3
- Affected files: `internal/system/music.go`
- Prerequisite: deterministic rising and falling transition coverage around
  `applyArrangement`

Manual intensity changes always use per-bar track reveal, while automatic
changes reveal only when intensity rises. Decide whether manual decreases should
retain that distinction.

## Multiplayer

Diagnoses and what each item follows from are in
[Troubleshooting](troubleshooting.md).

### Split "compared" from "carried" in the status surface

- Priority: P1
- Affected files: `internal/snapshot/surface.go`, `internal/app/capture.go`
- Prerequisite: name every excluded cell an FSM guard or system reads

`SharedKey` decides both what two instances compare and what a capture carries,
so a mixed-domain cell is dropped from the correction as well. `meta` carries
`kills.*` and `energy.damage_multiplier` around it; a second predicate removes the
workaround.

### Make kill credit independent of crossing order

- Priority: P2
- Affected files: `internal/system/combat.go`, `internal/component/combat.go`
- Prerequisite: a rule for which of two participants owns a shared kill

`CombatComponent.LastDamagedBy` is the last writer's cursor, and two participants
damaging one target write it in opposite orders when one hit missed the lead and
the authority applied it late. The authority's correction repairs it, so the
divergence is one cadence of credit rather than a permanent one. The boost is no longer awarded
inside that cadence — the prediction ledger holds it until a world proves the death
(multi-player.md §3.4) — but the credit it then pays is the one the local
prediction recorded, and the entity is gone from the authority's world by the time
that world arrives, so nothing can read the credit back off it. The knockback
window's answer was a budget per attacker; credit's is a choice about who owns the
kill, made where the kill is produced.

### Key the prediction ledger on more than an entity id

- Priority: P2
- Affected files: `internal/engine/prediction.go`, `internal/engine/world.go`
- Prerequisite: a generation on shared entity ids, which the allocator rollback
  already wants for its own reasons

An install restores the allocator counter, so a shared id can be issued twice
inside the ledger's window and a held derivation would then be proved or refused
against a different entity. A generation would make the key exact.

### Agree which knockback opens a shared window

- Priority: P2
- Affected files: `internal/component/combat.go`, `internal/system/combat.go`
- Prerequisite: a decision on whether a crossing-carried hit may ever override

`SpendKineticImmunity` reports `opened` from a timer, and `opened` is what picks
the override profile over the additive one. Every copy of a hit now applies at the
agreed tick, so the window opens together; what still differs is a hit that missed
the lead, which the authority applies late in arrival order, so for the length of
that lateness a second attacker's hit answers `opened` differently on two
instances. The per-attacker budget and the additive join closed the composition;
which hit owns the override is the same choice as kill credit above.

### Carry a maximum-sized world across a session

- Priority: P1
- Affected files: `internal/network/snapshot.go`, `internal/snapshot/capture.go`,
  `internal/system/wall.go`, `internal/engine/spatial_grid.go`
- Prerequisite: a decision on whether a generated wall grid has to travel as
  entities at all

`-serve -s td` refuses every join with `snapshot encode: 10112276 plain bytes is
outside 1..4194304`. `wad/scenario/td` is sized to the largest map the spatial grid
holds, 500x250, and fills it with a generated maze, so its start state alone is
more than 50,000 entities. `MaxSnapshotBytes` is 4 MiB and documented for a world
whose captures are single-digit kilobytes.

Raising the ceiling moves a number that bounds what one peer can make another
allocate, so it is not the fix on its own. The maze is a function of the scenario
and a seed both sides already hold, which is the first thing to weigh: a capture
that names the generator instead of its output is three orders of magnitude
smaller. This is a network and capture sizing task rather than a scenario one, and
it wants its own measurement pass. Until it lands, `td` is solo-only and the fleet
cannot serve it.

### Retain peer crossings for the projection

- Priority: P2
- Affected files: `internal/system/network.go`, `internal/app/replay_suffix.go`
- Prerequisite: none

A projection re-applies this instance's own suffix and the agreed artifacts, but
an ordinary crossing from another participant applied inside the projected window
is not retained, so the projection lacks it until the next correction carries it.
Retaining applied peer artifacts under the same age bound closes it.

### Predict a typed gold member instead of publishing it

- Priority: P2
- Affected files: `internal/system/network.go`, `internal/system/typing.go`
- Prerequisite: a local tombstone the leftmost-member check can read

`EventCompositeMemberDestroyed` is the one crossing its producer still applies at
once, because `isLeftmostMember` validates the next keystroke against the live run;
a correction inside the lead shows the member once more for a tick. A player-domain
tombstone the check consults would let the member cross like everything else.

### Keep the correction magnitude to the shared surface

- Priority: P3
- Affected files: `internal/engine/snapshot_world_gen.go`, `tools/`
- Prerequisite: none

`SharedWorldDifference` counts the owner-authored set of a cursor this instance
authors, which the reconcile then restores, so a projected install reports one
phantom entity per owned cursor.

### Find what makes a networkless soak load-sensitive

- Priority: P2
- Affected files: `internal/app/soak_test.go`, `internal/event/pool.go`,
  `internal/event/batch_pool.go`, `internal/engine/component_domain.go`
- Prerequisite: a reproduction; 36 runs under parallel load here stayed clean

`TestSoakAppsAreIndependent` failed twice in nine loaded suite runs with two
worlds of one seed differing in their position digest, so the simulation itself
took a different path. The headless clock is virtual and the audit gate is the only
process-wide switch a world reads; what parallel Apps share is the payload pools
(`CharacterTypedPayloadPool`, the batch pools) and `auditScope`, so a payload read
after its release to a shared pool is the candidate. Moving the pools onto the
World, or `-count` under load until it reproduces, is the smallest step.

### Rename the participant identity type

- Priority: P3
- Affected files: `internal/network/connection.go` and the 230-odd sites naming
  `PeerID`, `CursorComponent.PeerID` included
- Prerequisite: none; it is mechanical, and the component field is a capture
  schema change that both sides of a session already have to match on

`network.PeerID` is a participant identity that the transport also uses to
address a link. The vocabulary calls the first a participant and the second a
peer, so the type contradicts the document that defines it. Presentation now
says `participant` everywhere; the type is what remains.

### Audit what a pruned crossing loses

- Priority: P1
- Affected files: `internal/system/network.go`, `internal/app/barrier.go`
- Prerequisite: the split above, so a counter can be carried instead of replayed

An authority frame the capture's fence already claims is discarded on the
receiver. That is correct for shared component state and wrong for every other
effect the frame would have had. Enumerate them.

### Retire a zap bolt when a correction ends the zap

- Priority: P2
- Affected files: `internal/system/quasar.go`, `internal/system/lightning.go`
- Prerequisite: none

A bolt whose owner an install removed is now retired by `LightningSystem`, but an
install that clears `IsZapping` under a live quasar leaves one: the despawn is on
the range transition and the corrected state arrives without one. `QuasarSystem` is
shared-profile and cannot read the player store to find the bolt, so the answer is a
lease the owner renews while zapping rather than a lookup.

### Give a splash anchor a generation

- Priority: P2
- Affected files: `internal/system/splash.go`, `internal/component/splash.go`
- Prerequisite: decide whether shared ids may be re-issued at all

A capture restores `NextEntity`, so an install that rolls the allocator back
re-issues shared ids. A timer splash whose anchor died can find a different
composite under the same id and keep counting.

### Confirm the storm skip

- Priority: P2
- Affected files: `wad/scenario/main/storm.toml`, `internal/system/storm.go`
- Prerequisite: a run that reaches three quasar kills with a crowded map centre

The spawn retry and the carried kill counters address both candidate mechanisms
without either being confirmed. `storm.spawn_failures` and a `StormSetupRetry` in
`fsm.storm` tell them apart. `wad/scenario/td/td_storm.toml` still waits blind.

### Prove or rule out a stale gold surviving a correction

- Priority: P1
- Affected files: `internal/converge/selective.go`, `internal/snapshot/manifest.go`
- Prerequisite: a two-instance repro that leaves the guest holding two sequences

A guest was seen holding a gold the host had destroyed, and once two at a time.
Every destruction path clears the carrier and destroys the composite, and
`ReconcileSharedWorld` drops shared entities the capture does not name, so the
remaining candidate is a selective repair whose page reconstruction keeps an
entity only the receiver holds.

### Let a scripted participant survive a tick jump

- Priority: P2
- Affected files: `internal/journal/script.go`
- Prerequisite: decide whether a replay must still refuse the same overshoot

`ScriptDriver.applyCurrent` fails the run when the world tick has passed an
action's target tick. A correction moves the world tick, so a scripted guest in a
live session ends itself for a reason the session is entitled to. A replay has no
corrections and should keep refusing it.

### Keep shared FSM guards off owner-authored keys

- Priority: P3
- Affected files: `wad/scenario/main/monitor.toml`
- Prerequisite: a replicated liveness signal to replace the slot mirror

`MonitorWarmup` guards on `player.0.heat.current` and `player.0.energy.current`.
Both are owner-authored, so a receiver never writes them and no capture carries
them. The state is unreachable today, which is why it is a latent hole.

### Drop the three shared streams nothing draws from

- Priority: P3
- Affected files: `internal/system/quasar.go`, `swarm.go`, `snake.go`
- Prerequisite: retune `TestASlowPeerDoesNotSlowAFastOne`'s shaped budget

Each issues a Shared stream in `Init` and never draws from it, so every capture
carries and every correction restores three dead positions. Removing them shrinks
the capture enough that the cadence test's 500 B/tick shape stops saturating, so it
needs a lower budget in the same change. Costs a capture schema bump.

### Let a refused link cost one participant, not the session

- Priority: P2
- Affected files: `internal/app/session.go`
- Prerequisite: none

The start gate now excuses a participant that leaves, but `AdmitMeasuredLink`
refusing one still fails `startHostSessionOn` and ends the run — so with two guests
in the lobby, one unusable link takes the other's match with it. Refuse the
participant and continue with the rest, the way the mid-run gate already does.

### Say why a session reads as unavailable

- Priority: P2
- Affected files: `tool/vif-allocator/allocator.go`
- Prerequisite: none

`listSessions` swallows a health-probe error and reports the session as
`phase=starting`, `reason=health unavailable`. That hid a parse bug for the whole
life of the endpoint: every session carrying a health `reason` read as starting.
The parse is fixed and pinned; the swallow is not, and the next cause will be as
invisible. It needs a logger the allocator does not have, at a rate the fleet page
polls.

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
