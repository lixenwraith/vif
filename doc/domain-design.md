# Multi-instance domain model — vi-fighter

Rules D-1..D-15 describe how one world is split between state every instance
holds and state that belongs to one participant. All fifteen are implemented and
verified; §8 maps each to the test that pins it. §9 records what the model does
*not* yet do, and is the input to the next round of work.

This document supersedes every earlier design note, including the phase plan it
replaces.

## 1. Domains

Two per `World`: **Shared**, identical on every instance and replicated, and
**Player**, this instance's participant and never replicated. One `World` per
local participant. The roster slot lives on `CursorComponent`; it is not part of
the domain tag.

`core.Entity` is `[domain:8][id:56]`. `core.DomainNames` indexes the domain for
seed derivation, telemetry keys and log fields — changing a name re-keys every
stream in that domain.

## 2. Rules

**D-1 Reads.** A player-domain system may read shared state. A shared-domain
system reads shared only. Exceptions are D-12 and D-13, both explicit.

**D-2 Simulation ownership.** Only the instance owning a cursor simulates that
cursor's weapons, projectiles and player species. A remote participant's
player-domain state does not exist locally and is never reconstructed.

The admission check is `World.SimulatesLocally`, and `World.ResolveOwnedCursor`
is `ResolveCursor` narrowed through it. Every writer of the D-13 set goes through
one or the other — the five grant handlers and the five per-tick loops that would
otherwise age a transported value — so a remote cursor's energy has exactly one
authority. Reading `Resources.Player.Entity` is not a violation: every site is
view, input, or a player-domain effect keyed to the local participant
(`internal/render`, `internal/mode`, dust, drain population, motion marker,
splash), which is what D-6 says those are.

**D-3 The crossing.** When a player mechanic affects a shared entity, the
smallest artifact that determines the shared outcome crosses as a Bus event:

| effect | crossing artifact |
|---|---|
| direct hit (rod, cleaner, bullet) | one combat event per shared target |
| area effect (missile impact, dust detonation, disruptor pulse) | one explosion request: centers, radius, duration, attack family, owner cursor |
| drain fusion | one spawn request: header cell only |
| gold member typed | one composite-member destruction: header, member, typist cursor |
| a dying drain donating its hit points | one heal request: target and amount |
| a personal drain death | one progression event with no entity payload |
| the post-typing cursor advance | one cursor move request: the shared cursor and its cell |
| a personal nugget jump | one cursor move request: the shared cursor and the personal nugget's cell |
| a locally owned shield striking a shared species | one area hit: target, struck members and owner cursor |
| a participant entering or leaving terminal heat/energy state | one cursor defeat-state event |

The table is `crossingPushes` in `internal/system/event_class_test.go`, and the
test fails on a player-profile system pushing a replicated event that is not in
it. Nugget destruction is not a row: the nugget is personal, and only its jump's
shared cursor move crosses.

The shared progression FSM consumes `EventDrainDefeated`, not the local drain's
`EventSpeciesKilled`; otherwise one participant's personal population advances
only its own copy of `kills.drain`. The global reset similarly consumes the
crossed combined defeat predicate and resets only when every rostered cursor is
defeated. It never reads the owner-authored `heat.current`/`energy.current` cells.

Effects on player targets do not cross. The producer resolves its own domain
*before* pushing the crossing event; the shared consumer resolves only shared
targets.

The gold row is a keystroke crossing: `TypingSystem` is player-domain and
`EventCompositeMemberDestroyed` names a shared member. Its payload carries the
typist (`CompositeMemberDestroyedPayload.Entity`), which is what makes the credit
a function of shared events rather than of who happened to type last.
`GoldSystem` tallies per roster slot and `GoldCompletionPayload.Entity` names the
cursor that typed the most members, ties resolved to the lowest slot so every
instance credits the same one. Timeout and destruction leave it zero.

**D-4 Payload purity.** A Bus payload names only shared entities. Player emitters
are reduced to coordinates and velocity (`HasOrigin`, `OriginX/Y`, `HasVelocity`,
`OriginVelX/Y`). A Local payload may name player entities freely —
`EventFuseSwarmRequest` and the lightning triple do. Asserted over a soak by
`TestBusPayloadsNameOnlySharedEntities`.

**D-5 Derived, not transported.** Events a shared system produces from a Bus
event are re-derived identically on every instance and must never themselves be
transported. `EventExplosionBatchRequest` crosses; the
`EventCombatAttackAreaRequest`s it produces do not.

**D-6 Effect entities are player-domain.** Lightning, flash, fadeout, splash,
motion marker, dust, decay, blossom, orb, bullet, missile and loot are created
from the player counter and may be created conditionally on local view state
(`Player.IsLocal`). They never feed shared simulation. This is what lets a remote
cursor's damage land without its visuals cluttering the screen.

**D-7 Ambient domain.** `World.WithDomain(d, fn)` mirrors `WithOrigin`;
`PushEventDomain` and `PushLocal` stamp explicitly for producers outside any
scope. One system can serve both domains without splitting: `MaterializeSystem`
gates a shared species spawn and a player drain from one code path, reading the
request's domain rather than being duplicated, and stamps the completion with the
domain of the entity it completed. This is the general answer to generic types
(death, timer, spirit, materialize, species lifecycle) — they are stamped, not
statically classified.

All three cleaner producers — nugget beacon, weapon, and the `:cleaner` command —
push `core.DomainPlayer`, so every cleaner is player-domain and its request
events are `Local`. `CleanerSystem` still resolves both and keeps its `dual`
profile, which is defensive rather than exercised.

The ambient tag is **not** derived from the declared system profile:
`UpdateLocked` sets the audit scope from `SystemDef.Domain` but leaves
`World.domain` alone, so an unscoped `PushEvent` from a player-profile system
still stamps `shared`. Opting in is the producer's job — see D-10.

**D-8 RNG.** `RandResource.Stream(domain, label)` derives from `(sessionRoot,
domain, label)`. A system resolving both domains holds one stream per domain and
selects by the target's domain; `CombatSystem` and `SoftCollisionSystem` are the
only two. A wholly player-domain system draws one player stream: `FuseSystem`,
`DrainSystem`, `LootSystem`, `LightningSystem`. No simulation path seeds from a
clock; `TimeResource.GameTimeNano` is explicitly not a seed source.

**D-9 Entity identity.** `World.nextEntityID [2]uint64`; `CreateEntity(domain)`
explicit at every call site; `Clear()` resets both. Zero remains invalid in both
domains. Created and destroyed counts are tracked per domain
(`CreatedCountDomain`, `DestroyedCountDomain`); the aggregate accessors sum them.

**D-10 Event domain.** `GameEvent.Domain` is stamped at push from the ambient
domain and carried through to `JournalRecord.Domain`, which the vlog sink writes
and `internal/journal` parses. Registry classes: `Shared` (emitted and consumed
shared, replicated), `Bus` (player-originated, affects shared, replicated),
`Local` (never replicated), `Stamped` (class determined per-event from the domain
tag). The class is declared in the `type.go` doc comment beside the payload —
`// EventFoo (FooPayload) [bus] ...` — and generated into `eventClasses` in
`internal/event/registry_gen.go`. The generator refuses an unclassified constant.

Two facts constrain how the table can be built.

*The tag is opt-in.* `core.DomainShared` is the zero value and the ambient domain
defaults to it, so a bare `PushEvent` leaves a record reading "shared" whatever
produced it. A soak census over three seeds tagged 70 of 91 observed types
`shared`, including unambiguous D-6 effects emitted from FSM actions. `Shared`,
`Bus` and `Local` are therefore *declarations*, checked statically against the
pushing system's profile (`TestEventClassMatchesSystemProfile`); only `Stamped`
is read from the tag, and `TestStampedEventsAreExplicitlyStamped` rejects a
`Stamped` declaration no producer resolves.

*`Stamped` is a function of the payload, not of the producer.*
`EventCombatAttackDirectRequest` forces the distinction: the same producer, in
the same tick, under the same ambient domain, pushes a hit that crosses when the
target is shared and does not when the target is player. No static per-type table
can carry that. Combat producers stamp `GameEvent.Domain` from the target's own
domain at all four push sites, and the filter reads the tag.

*Compared is not sent.* `event.Replicated(type, domain)` answers "must both
instances hold this record", which is what the journal filter needs.
`event.OnWire` answers "must a peer receive it", and is strictly narrower: a
`Shared` event is re-derived identically on every instance, so sending it applies
it twice.

The wire set is not `Bus` either. **Many Bus types have producers of both kinds**:
`EventCompositeMemberDestroyed` from typing and from pylon, tower, storm and
snake; `EventExplosionRequest` from a missile and from an eye;
`EventSwarmSpawnRequest` from the fuse and from a storm. A shared producer's copy
is re-derived everywhere; only the player-domain one crosses. So the tag decides
here too: `World.PushCrossing` stamps the D-3 artifact `DomainPlayer`, and
`OnWire` requires it. Crossing-only Bus types such as `EventDrainDefeated` use
the same explicit stamp; class alone never opts an event onto the wire.
`TestCrossingPushesAreLive` fails a `crossingPushes` entry that does not use it.

For `Stamped` the tag means the *target's* domain instead, so the same rule reads
inverted: `stampedCrossings` names the one Stamped type a player-domain producer
aims at a shared target (`EventCombatAttackDirectRequest`), every other
Stamped-shared event having come from a shared system. A chain follow-up is in
the transported set but not on the wire — the receiver derives it from the root —
and opts out through the `event.Derived` payload interface (D-5).

**D-11 Determinism invariants.** Across instances: identical shared event order,
identical shared entity creation order, identical shared RNG derivation,
identical shared component values except where D-13 applies. Verified by
comparing `App.SnapshotShared()` between instances and by stripping player
records from two journals and asserting equality.

**D-12 Claimed geometry.** A shared system that *claims* cells — spawn footprint
clear, composite sweep-over, wall push-out — enumerates both domains and acts on
every occupant. Not a D-1 violation: the shared outcome is a function of the cell
set and protection masks alone, so it is identical on every instance; player
victims differ per instance and are player-domain effects by D-6. The constraint
is on *emission*: victims leave as one death batch per domain
(`internal/system/sweep.go`, `cellSweep`), so a shared record never names a
player entity. The cross-domain reads this needs are exempted one at a time in
`allowedDomainAccess`, and `TestAllowedDomainAccessIsLive` fails an exemption
that outlives the access it excuses.

**D-13 Owner-authored shared state.** A shared entity may carry components
written by exactly one instance and replicated as values rather than re-derived.
The complete list: cursor gameplay components (energy, heat, boost, shield,
weapon, combat), `CursorComponent.Control`/`PeerID`, and
`CursorViewComponent`/`PingComponent`/`PulseComponent`, which are pure local
view. D-11 is refined: shared entity *identity* and *creation order* are
identical on every instance; shared component *values* are either re-derived
identically or owner-authored and transported — never both. Owner-authored state
must not appear in a cross-instance digest, and the metric keys mirroring it are
excluded by `denySharedPrefix` in `internal/app/snapshot.go`.

The static check keys on store name, so it covers only the cursor-exclusive half:
`ownerAuthoredStores` in `internal/system/domain_test.go` lists Energy, Heat,
Boost, Weapon, CursorView, Ping and Pulse. A shared-profile system may neither
write nor read one of those stores. `Shield` and `Combat` are excluded
deliberately — they also carry quasar, loot and species state, which is
re-derived, and the store name alone cannot separate the two populations.

The set is closed against the code: a live cursor carries exactly Cursor,
Protection, Energy, Heat, Shield, Boost, Weapon, Ping, CursorView, Combat and
Position, plus Pulse while a disruptor pulse runs. Position is shared and crosses
as `EventCursorMoveRequest`; Protection is a creation constant; the rest is this
list.

The transport is `CursorStatePayload`, written by `NetworkSystem` and by nothing
else, and only onto a cursor `SimulatesLocally` rejects and whose roster slot
matches the payload's. Shield and Combat travel as their cursor fields alone.
`CursorViewComponent.Orbs` does not travel: it names player-domain entities
(D-4). Shield geometry and ember state reproduce the remote cursor's presentation
and owner-local interactions; no shared outcome reads the periodic snapshot.

Shield/species collision used to contradict that last sentence: quasar, swarm,
storm, eye, pylon and snake all re-derived shared knockback from whichever shield
snapshot they held. Each now admits only `SimulatesLocally` cursors and crosses
`EventCombatAttackAreaCrossingRequest` with the exact shared target/member set.

**D-14 Map bounds authority.** `MapWidth`, `MapHeight` and `CropOnResize` are
shared simulation state with two writers:

- `World.SetupLevel`, driven by `EventLevelSetup` from the map script. Shared and
  replicated; this is the authority.
- `GameContext.HandleResizeLocked`, driven by this instance's terminal, and
  admissible only while `mapSizeLocal()` holds. *When more than one player is
  present, crop is disabled and map size is locked.*

The join race is accepted: a resize already in flight when the second participant
appears may land, a resize after it will not. The window is one event dispatch
and the divergence is bounded by the guard immediately after. Suppression
publishes `context.map_locked` and logs once per resize.

Consequence, instrumented rather than closed: a map script may branch an FSM
guard on `viewport_width`, `viewport_height`, `camera_x`, `camera_y` or
`color_mode`, which are per-instance, and under a locked map those take a
different arm on each instance. The whole script-visible surface is
`internal/engine/config_access.go` — eight keys, of which `map_width`,
`map_height` and `crop_on_resize` are replicated and the other five are not. Both
accessors warn once per key when a non-replicated one is read while
`World.MapSizeLocal()` is false. The keys are retained: D-14 keeps the surface,
and the warning only marks where a script has made itself instance-dependent.

**D-15 Declared classification.** Every system declares its domain profile
(shared, player, dual) and its dependencies (required, optional) as data in
`internal/manifest/definition.go`. That file is the sole declaration site:
`System` carries no `Domain()` or `Requires()` method, `World.AddSystem` takes a
`manifest.ProfileFor(name)`, and `TestSystemsDeclareNoDomainMethod` fails a
system that reintroduces either. The generator emits
`internal/manifest/build_gen.go` and `internal/engine/component_domain_gen.go`;
the boundary suite asserts the code matches the declaration, and the FSM's
`enabled_systems`/`disabled_systems` validation rejects a map that disables a
required dependency. Filename lists are not a classification mechanism and are
not maintained — the one unattributed set (`blast.go`, `interaction.go`,
`sweep.go`, `targeting.go`, `telemetry.go`) is pinned by
`TestHelperFilesArePinned` so a new helper is visible rather than silently
unattributed. Dependency order is initialization and requirement order, resolved
topologically by the shared resolver in `internal/core`; it is distinct from
`System.Priority()`, which orders `Update()` within a tick, and the two are
permitted to correlate without being conflated.

## 3. Spatial partition

`Cell` = `Count uint8 + SharedCount uint8 + [6]byte + [31]Entity` = 256 bytes,
asserted in `spatial_grid_test.go`. Invariant: shared occupy
`Entities[:SharedCount]`, player occupy `Entities[SharedCount:Count]`.

`engine.DomainScope` — `ScopeShared`, `ScopePlayer`, `ScopeBoth` — with
`Selects(entity)` for component-store iteration.
`parameter.ReservedPlayerPerCell = 12` guarantees 19 slots to shared, so a pile
of local effects can never starve shared placement. Insertion is a soft clip:
`Set` returns false rather than evicting.

Scoped APIs: `GetEntitiesAt(Into)`, `HasAnySharedEntityAt`, `ScanLine(First)`,
`FindClosestEntityInDirection`, `SpatialGrid.HasAnyEntityInArea`, and the
targeting triple (`HasCombatTargetAt`, `FindNearestTargets`,
`FindTargetsInEllipse`). Weapons and missiles pass `ScopeBoth`; shared species
pass `ScopeShared`. `PositionBatch.CommitShared` is the shared placement gate.

Telemetry: `spatial.player_budget_rejects`, `spatial.indexed_shared`.

## 4. Entity classification

| Domain | Entities |
|---|---|
| **Shared** | cursor, quasar, swarm, storm, snake, eye, pylon, tower, gateway, wall, gold, marker, explosion centers, FSM, time |
| **Player** | glyph, nugget, dust, drain, decay, blossom, bullet, missile, orb, lightning, flash, fadeout, splash, motion marker, loot |
| **Stamped** | cleaner (request-stamped; every current producer is player), materialize (shared when it gates a shared spawn, player for drain), spirit (shared unless the requester is player-domain, which today is always the fuse) |

Cursor components split three ways: shared-and-replicated (position),
owner-authored (energy, heat, boost, shield, weapon, combat — D-13), and pure
local view (`CursorViewComponent`, `PingComponent`, `PulseComponent`).

`TransientResource` holds explosion centers and stays shared: they *are* the
crossing artifact. `ViewResource` (grayout, strobe) is player-domain.

**Glyph.** Content glyphs are player-domain: the corpus and the map are the only
inputs, so every instance derives the same text from its own player counter and
types against its own copy, and `GlyphSystem` carries a `player` profile. The
exception is a gold sequence member, a shared composite member that happens to
carry `GlyphComponent` — which is why `GlyphBit` stays unlisted in
`manifest.Components`: the bit legitimately attaches in either domain and no
static rule can separate the two populations. The mechanism is a guard, not a
mask: player-domain mechanics that sweep glyphs (dust conversion, cleaner sweep,
decay, blossom, splash, typing, drain) skip `e.Domain() != core.DomainPlayer`,
one invariant replacing three accidental protections (protection masks, component
absence, iteration order). `TestSharedGlyphsAreGoldMembersOnly` asserts the other
direction: every shared-domain glyph is a gold composite member.

**Contested vs personal.** A mechanic is *contested* when its outcome is a
function of shared state alone, and *personal* when it reads owner-authored
state.

Gold is contested: any participant may claim it, and the claim is a deterministic
function of the shared event stream — `GoldSystem` tallies
`EventCompositeMemberDestroyed` per roster slot, `GoldCompletionPayload.Entity`
names the cursor that typed the most members, ties break to the lowest slot, zero
on timeout or destruction. Only the reward is owner-authored (D-13).

Nugget is personal and uncontested: each instance owns its player-domain spawn,
collection area, destruction and reward, and a remote cursor cannot claim it. A
nugget jump crosses only the resulting shared cursor move. This puts nugget
beside loot, which is also rolled and owned per participant because its mechanic
reads owner-authored state.

## 5. System classification

Per D-15 the declarations live in `internal/manifest/definition.go` and are
generated into `manifest.systemProfiles`. The list is not restated here; read
`Systems` and `ContextSystems` in that file, where each entry carries a one-line
rationale. What this document owns is the invariants the list must satisfy:

- A `dual` profile means the system resolves the domain per request or per target
  (D-7, D-8) — not that it writes both domains indiscriminately.
- A `shared` profile that reads a player store needs an `allowedDomainAccess`
  exemption naming the D-12 site that justifies it.
- A `player` profile may write the D-13 owner-authored set; a `shared` profile may
  not, except `cursor`, which creates the entity and writes constants that shared
  creation order already carries.

`ContextSystems` holds the systems `App` registers directly because they take a
`GameContext` rather than a `World`; `meta` is the only member. Its profile is
`shared`: its world writes are replicated or are the D-14 map-bounds writer, and
the context state it writes is not world state. `unregisteredSystems` in
`internal/system/domain_test.go` is empty.

## 6. Transport

`network` carries a `dual` profile: it replays a peer's crossings in the domain
their producer stamped (D-7) and is the sole writer of a remote cursor's
owner-authored set (D-13). It runs first — `parameter.PriorityNetwork` — but its
transport work is not in `Update`.

**The barrier.** `event.WireSink.Cross` ends production by encoding and
withholding each local artifact. `Flush` closes the tick's epoch and sends it
asynchronously, including an empty marker. `Receive` opens the next tick by
applying local and peer artifacts whose fixed playout deadline has arrived, then
`settleLocked("wire")` completes that dedicated between-tick settle group before
`BeginTick`. Existing pre/post settle groups are neither merged nor split, so
replay keeps their exact granularity. Both copies of an artifact — the producer's
and the receiver's — therefore apply at the same absolute tick; without the
barrier a crossing applied locally in its producing settle and remotely at the
next tick opening, a one-tick 50 ms divergence window.

The default delay is three 50 ms ticks. It is session metadata rather than a
round-trip gate: simulation never waits for a peer, and a deployment can
negotiate a larger lead for a higher-latency path. Artifacts sort by apply tick,
participant ID and per-source sequence — the shape required beyond two
participants. A crossing produced by the wire settle belongs to the production
epoch about to run and gets one complete delay of its own; it never recurses into
the apply pass.

With no live peer `Cross` declines ownership, `Receive` returns zero and the
scheduler creates no wire settle group. The original queue/journal/publication
path is therefore unchanged.
`network.barrier_{deferred,applied_local,applied_peer,late,ran_without_peer,peer_lag_ticks,peer_artifacts}`
and `network.barrier_peer_applied` expose the barrier state.

**The stream.** The real endpoint is `network.SocketPort`. Every message has a
fixed 12-byte header whose final field is payload length; `Decode` uses
`io.ReadFull` for both header and payload, and `Encode` completes short writes.
Transport goroutines append only `network.Inbound` values to the port buffer;
`NetworkSystem` drains that buffer under the world lock, preserving the poll
boundary. Idle peers exchange framed heartbeats; read and write deadlines close a
silent stream without blocking a tick. The resulting disconnect notification is
drained through the same path and removes only cursors owned by that participant.
Two message kinds are live in a session — one closed barrier epoch and one
owner-authored cursor sync — plus the four-step startup handshake; anything else
is counted as a drop rather than translated.

Loss that happens outside the barrier is counted, because either kind
desynchronises silently: `network.transport_lost_in` is inbound notifications a
full poll buffer discarded, `network.transport_lost_out` is outbound frames a
peer's send queue refused. A new loss is also logged once.

**Startup.** The handshake sends the existing `JoinAnchor` inside `SessionOffer`,
then the host assigns participant IDs and roster slots. `App.JoinSession` calls
`App.Join`, so schema, tick interval, seed, session, config, corpus and D-14
latch mismatches return the existing join error. A rejected connection never
enters the peer manager. Canonical participant IDs, not connection-local accept
order, key the barrier sort and disconnect roster cleanup. The tick-zero
start/ready gate is startup coordination only; no per-tick round trip was added,
and it is what gives every participant the same tick origin the barrier's
absolute apply ticks presume.

`cmd/vif` exposes that gate as startup flags rather than ex commands: `-host
<bind-address>` and `-join <host:port>`. A host initializes its terminal, world
and listener, renders a lobby message, and holds the scheduler at tick zero until
one participant is ready. A joiner dials before constructing its `App`, so
`ConfigForJoin` installs the host seed, config and corpus identity before
`initWorld` can draw a seed or load content. Both sides activate the crossing
sink before terminal input is consumed. The host remains playable and listening
after a disconnect; a later connection receives the current nonzero position and
is rejected with `ErrJoinMidRun`, because no world snapshot exists.

**Cost.** The wire keeps journal TOML payloads inside a JSON epoch envelope. The
measured complete frames, including the 12-byte header, are 44 bytes for an empty
epoch, 567 bytes for four cursor moves, 1,771 bytes for six resolved three-member
shield hits, and 703 bytes for one D-13 owner-state sync. At 20 ticks/s with the
six-tick state cadence, that is about 3.2 KB/s idle, 13.7 KB/s at four crossings
per tick, or 37.8 KB/s at the deliberately busy shield rate, per direction and
owned cursor. A denser payload codec does not justify a second registry/schema
path at these rates. `TestWireEncodingBudget` pins the representative budgets;
`TestFrameRoundTripSurvivesShortStreamIO` pins framing.

Journal schema is 9, and the wire shares its encoder: 7 made `Domain` meaningful,
8 added the D-14 map latch to the anchor, and 9 moved the nugget event family out
of the replicated record set after the mechanic became personal.

## 7. Telemetry and snapshots

`status.GroupGate`: `GateAlways`, `GateSentinel` (gated on a roster slot's entity
cell), `GateActivity` (any non-zero member). Declared by prefix in
`activityGatedGroups`; honoured by `VisibleViews`, `Snapshot` and the flight
recorder. Add new wide-but-usually-silent groups by prefix, not by
special-casing a consumer.

Three snapshot views over one reading:

| view | drops | used by |
|---|---|---|
| `Snapshot` | nothing | `:d save`, perturbation test |
| `SnapshotSimulation` | operator surface (`denySim`, session record) | replay vs. source run |
| `SnapshotShared` | owner-authored state (`denySharedPrefix`, `denySharedField`, view and session records, local digest scope) | cross-instance comparison |

The shared view is four rules, not one list. `denySharedPrefix` drops a group:
the per-slot `player.` group; `context.screen_`, `context.camera_` and
`context.mode`, which mirror fields the `view` record already drops; `event.` and
`spatial.`, instance-local traffic and index counts; `network.`, which is the
exact complement of a peer's counters; `entity.` and `kills.`, aggregates that
sum both domains; and every player- or dual-profile system's own group — the
effect systems, plus `glyph.`, `fuse.`, `shield.`, `cleaner.`, `camera.`,
`transient.`, `motion_marker.`, `materialize.`, `soft_collision.`, `audio.`,
`music.`, `death.`, `timer.` and `combat.`. The rule is the profile in
`manifest.Systems`, not the name. `denySharedKey` drops a single key from an
otherwise comparable group: `engine.apm` and `engine.music_apm` beside the tick
counters, `nav.entities`, and `content.served`/`content.rejected` beside the
corpus fingerprint. A `.buf_*_hwm` suffix drops scratch high-water marks, which
`newBufferTelemetry` names for every system that publishes one. A
`.protected_player_rejects` suffix drops the player-victim half of otherwise
shared species protection telemetry; the unsuffixed counter contains only shared
victims. `allowSharedKey` re-admits `spatial.indexed_shared`, which its group
prefix would otherwise deny. `denySharedField` drops
`created_local`/`destroyed_local` from the otherwise shared `world` record.

Most of that list was invisible while both parity instances ran identical
player-domain simulations. A real second participant drives its own cursor, so
every mixed-domain counter moves independently; `combat.` is the loss worth
naming, since it resolves targets in both domains from one set of counters and
would return to the comparison if those were split per domain.

`SnapshotContext` emits five records: `context`, `world` and `player` are emitted
into the shared view, `view` and `session` are dropped from it. The `player`
record carries `count`, the shared roster size, and nothing else: the local
binding — `entity`, `slot`, `x`, `y` — lives in `view`, where a remote
participant binding a different slot to a different entity is expected rather
than divergent. `worldDigestScopedLocked` takes a `DomainScope`, so the shared
digest excludes player entities, and its combat digest additionally excludes
cursors, whose `CombatComponent` is owner-authored (D-13).

## 8. Verification

The boundary is asserted by construction rather than by review. Each rule below
fails the build when the code stops matching the declaration.

| Test | Package | Asserts |
|---|---|---|
| `TestSystemDomainProfiles` | `internal/system` | D-15: each declared profile against the RNG streams, entity domains and component stores its file touches |
| `TestAllowedDomainAccessIsLive` | `internal/system` | D-12: the exemption list cannot outlive the access it excuses |
| `TestHelperFilesArePinned` | `internal/system` | The unattributed file set is fixed, so a new helper is visible |
| `TestSystemsDeclareNoDomainMethod` | `internal/system` | D-15: the manifest is the only declaration site |
| `TestCombatKnockbackDrawsFromTheTargetsStream`, `TestSoftCollisionImpulseDrawsFromTheTargetsStream` | `internal/system` | D-8: a player-target impulse never advances the shared stream, the shared case proving it non-vacuous |
| `TestEventClassMatchesSystemProfile`, `TestCrossingPushesAreLive` | `internal/system` | D-3/D-10: every player-profile push of a replicated event is a named crossing, and every named crossing stamps |
| `TestPersonalNuggetUsesPlayerDomainAndLocalCursor`, `TestPersonalNuggetJumpCrossesOnlyCursorMove` | `internal/system` | §4: nugget is personal; only the cursor move crosses |
| `TestSharedSpeciesCrossesOnlyOwnedShieldImpact`, `TestCursorDefeatTransitionCrossesCombinedOwnerState`, `TestMetaDefeatGateRequiresEveryRosteredCursor` | `internal/system` | D-13: a remote shield cannot produce a second impact; defeat state crosses instead of being read from slot zero |
| `TestRemoteCursorRejectsOwnerAuthoredWrites`, `TestRemoteCursorStateDoesNotAgeLocally` | `internal/system` | D-2: neither a grant nor a per-tick loop writes a cursor this instance does not simulate |
| `TestCursorStateSyncWritesOnlyACoherentRemoteCursor`, `TestPeerDespawnReleasesTheSlotSyncSequence` | `internal/system` | D-13 receive side: entity and slot must agree, sequences gate replays, a released slot accepts a successor |
| `TestBusPayloadsNameOnlySharedEntities` | `internal/app` | D-4 over a soak, via a dispatch tap |
| `TestLocalEventsCarryThePlayerDomain` | `internal/app` | D-10: a Local-class record is tagged player, against a shrinking exemption set |
| `TestDomainAuditSoakClean` | `internal/app` | Zero component-domain violations over a 3,000-step soak |
| `TestMapSizeLockedWithSecondCursor`, `TestMapSizeCropsWithOneCursor` | `internal/app` | D-14, with the crop path as its own negative control |
| `TestSharedGlyphsAreGoldMembersOnly` | `internal/app` | §4: every shared-domain glyph is a gold composite member |
| `TestSharedSnapshotParityAcrossTerminalSizes` | `internal/app` | D-11: two instances of one seed on different terminal sizes agree at every step |
| `TestObserverSharedStateTracksTheLiveParticipant` | `internal/app` | 1,200 steps of an observer whose shared state arrives over the wire rather than re-derived |
| `TestTwoLiveParticipantsStayInLockstep` | `internal/app` | 1,200 steps, two live participants, both moving, both crossing, both nonzero APM |
| `TestTwoLiveParticipantsStayInLockstepOverTCP` | `internal/app` | The same criterion through `127.0.0.1`, plus handshake, roster, framing and clean remote-cursor removal on disconnect |
| `TestActivatedSessionDefersCrossingBeforeFirstTick` | `internal/app` | Input arriving before the first system update enters the barrier rather than applying locally |
| `TestAppsScopeOperatorState` | `internal/app` | Two Apps drive resize and debug mutations without cross-talk |
| `TestWireEncodingBudget`, `TestFrameRoundTripSurvivesShortStreamIO` | `internal/event`, `internal/network` | Representative stream cost; framing survives short stream I/O |
| `TestBroadcastReportsRefusedFrames` | `internal/network` | A refused outbound frame is counted rather than swallowed |

Supporting machinery: `engine.PinDomainAudit`/`DomainMismatches`/
`DomainViolations`; per-system audit attribution in `UpdateLocked`, falling back
to `"event"` for settle-pass attaches; `ClockScheduler.SetDispatchTap` and
`App.SetDispatchTap`; `ScriptDriver` with `Step()` for lockstep driving;
`ScriptOptions.Resizes`/`MapSetups`/`MapMotionsOnly`; `FastRand.State()`.

The two-live harness owns one tick per participant per step. It disables random
script ticks and the overlay round trip so neither App can outrun the three-tick
playout lead. `Resizes`, `MapSetups`, FSM `Regions`, resets and ex commands are
held fixed: each is an operator injection applied only to the App receiving it,
and several intentionally rewrite shared scheduler or simulation state. They are
not participant gameplay and are not transported under D-10.

### Manual two-terminal proof

```bash
# terminal 1
./bin/vif -d -host 127.0.0.1:7777

# terminal 2
./bin/vif -join 127.0.0.1:7777
```

Both status bars must reach `NET:1P/LOCK`; each terminal must show both cursors,
and movement, typing, combat and scoring from either side must resolve onto the
same shared actors. Quit the joiner: the host must change to `NET:DOWN/OPEN`,
remove only the remote cursor and continue accepting local input. `:d save` is
not a byte-for-byte parity diagnostic because it deliberately includes local view
and owner-authored metrics; a divergence is a different shared actor, position,
kill or progression result, or a nonzero
`network.barrier_late`/`network.barrier_ran_without_peer`/`network.transport_lost_*`
trend under an otherwise healthy link.

The same binary works on a LAN by binding the host to `:7777` or `0.0.0.0:7777`
and joining its reachable address. Internet use is the same socket path but
remains a trusted-peer proof: it requires external firewall/NAT routing and
currently carries plaintext with no authentication.

## 9. Analysis: authority, topology and open work

### 9.1 Who decides what

There is no single authority, and asking "which peer is the host" only has a
useful answer for one of the three kinds of state.

| State | Authority | Mechanism |
|---|---|---|
| Shared simulation | **None** | Every instance re-derives it from the same seed, config, corpus, map script and ordered artifact stream (D-11). Agreement is a property of determinism, not a decision. Nothing is sent: `OnWire` excludes the `Shared` class precisely because sending it would apply it twice. |
| Owner-authored shared state (D-13) | **Per cursor**, the instance that simulates it | `SimulatesLocally` admits exactly one writer; the value is transported, never re-derived. This is per-object, not per-session, and does not depend on topology. |
| Session identity and map bounds | **The coordinator**, at startup only | The `JoinAnchor` (schema, tick rate, seed, session counter, config and corpus identity), the D-14 map latch, participant IDs, roster slots and the barrier delay. |

The host is a coordinator, not a state authority, and it stops being even that
once the tick-zero gate releases. After startup a host is an ordinary
participant: it owns its own cursor's D-13 cells and nothing else. It has no
ability to correct, override or arbitrate another participant's shared state,
because it holds no copy that is more authoritative than anyone else's.

So the chain question — *A joins B, B joins C; who is the host for A and C?* —
is answered by the model rather than by the graph: **the coordinator is whoever
issued the `SessionOffer` a participant adopted**, and the correct behaviour for
a chain is not to elect a host per link but to propagate one session identity.
Every participant in one session must adopt the same anchor, the same
participant-ID space and the same barrier delay, whoever physically hands it
over. A relay forwards an offer; it does not mint its own.

### 9.2 Topology today, and what a graph needs

The implementation is a **star of exactly two**, and the limit lives in three
separate places rather than one.

1. *The session layer is fixed at two.* `hostParticipantID`/`joinParticipantID`
   are constants, `hostOffer` builds a two-entry roster, the host's transport
   sets `MaxPeers = 1`, and `startHostSessionOn` waits for `remoteCount = 1`.
2. *The transport is single-hop.* `flushCrossings` broadcasts only this
   instance's own epoch. A peer's decoded batch is scheduled locally and never
   forwarded. In A—B—C, B applies A's artifacts and C never sees them.
3. *Membership is link-local.* `despawnPeer` removes the cursors of a directly
   connected participant on its own disconnect notification. A participant two
   hops away leaving is invisible.

Everything below the session layer is already vector-shaped, which is why this is
extension rather than rewrite: `SessionOffer.Participants` is a slice,
`lastPeerTick` is indexed by participant ID, the barrier sorts by *(apply tick,
participant ID, sequence)*, and `ScheduledWireFrame.ApplyTick` is an **absolute**
tick, not a relative offset. That last property is what makes a relay
tractable — a forwarded artifact still applies at the same tick everywhere,
because the tick it names travelled with it.

Reaching A—B, B—C, (C,D)—E, with A's action visible to E and back, needs five
things, in dependency order:

1. **Artifact relay.** Forward a decoded batch unchanged, preserving `Source`,
   `ProducedTick` and `ApplyTick`. The existing per-source
   `batch.ProducedTick <= lastPeerTick[Source]` check is already the idempotence
   a relay needs — a second copy arriving by another path is rejected. What is
   missing is the forward itself, and a hop bound, because a graph with a cycle
   re-delivers forever.
2. **A playout lead that covers the graph diameter, not one link.**
   `NetworkBarrierDelayTicks = 3` is negotiated once at 150 ms. An artifact
   crossing several hops must still arrive before its absolute apply tick, or it
   lands late and the instances diverge. `network.barrier_late` is already the
   signal that the lead is too small; the delay has to become a function of the
   worst-case path rather than a constant.
3. **Session-wide participant-ID allocation.** IDs must be globally unique and
   stable, because they key `lastPeerTick`, the barrier sort and the roster slot.
   A participant joining through a relay must receive its ID from the same
   allocator, forwarded, not from the peer it happened to dial.
4. **Membership as shared state.** Join and leave must cross as artifacts every
   instance applies at the same tick, rather than being derived from a local
   link event — a cursor despawn is shared state, and today two instances agree
   only because both are endpoints of the same link.
5. **Partition handling.** In a graph, one link failure splits a session into two
   components that both keep simulating happily. Nothing detects this.

A star with N joiners is a much smaller step than a general graph: it needs (1)
in its simplest form — the host rebroadcasting each peer's epoch — plus (3), (4)
and a roster of N. It is the sensible next increment.

### 9.3 Toward a trusted branch

The eventual "which branch do we trust" question is easier here than in a
system that replicates state, and the reason is worth recording. Because shared
state is re-derived rather than transported, there is nothing to reconcile
*except the artifact stream*. A divergence is always a disagreement about which
artifacts, in which order, at which tick — never about the resulting world. The
frame already carries the three fields such a log needs (source, per-source
sequence, absolute apply tick), and the ordering is already independent of
arrival order.

Two prerequisites do not exist:

- **Rewind.** An applied artifact cannot be taken back. There is no world
  snapshot and no rollback, which is also why `ErrJoinMidRun` refuses a late
  joiner. Any scheme that rejects a branch after the fact needs a snapshot to
  return to and a re-simulation from it.
- **Identity.** The link is plaintext and unauthenticated; `MsgAuthRequest`/
  `MsgAuthResponse` are reserved and unused, and `Config.TLS` is never populated
  by the CLI. A participant cannot prove that an artifact attributed to it is
  its own, so there is nothing for an agreement rule to bind to.

Snapshot-and-resume is the higher-leverage of the two: it unlocks mid-run join,
reconnect, and rollback in one piece of work.

### 9.4 Known limitations

**Session and transport**

- Two participants, startup-only. No mid-run join (`ErrJoinMidRun`), no
  reconnect, no world snapshot.
- Plaintext and unauthenticated; no CLI TLS surface.
- No lag compensation. A slow peer produces late artifacts and divergence rather
  than a stall — deliberate, since simulation never waits, but it means the
  playout lead is the only defence.
- A refused outbound frame is now counted and logged but not retransmitted. The
  stream itself is reliable and ordered; the loss is in the bounded send queue
  ahead of it.
- `float64` simulation means cross-platform bit-exact lockstep is not claimed;
  the guarantee is per implementation build.
- `NetworkSystem.Init` on a game reset clears scheduled artifacts and restarts the
  epoch, which is symmetric only because both instances reset at the same
  derived tick.

**Domain boundary**

- `combat.` telemetry is a mixed aggregate and is dropped whole from the shared
  snapshot. Splitting the counters per target domain would return the group to
  the comparison.
- `unstampedLocal` in `internal/app/local_stamp_test.go` still holds 19
  `Local`-class types pushed with the ambient shared tag from app, engine, fsm
  and the shared species systems. Not a transport gate — the class keeps them off
  the wire regardless — but the journal record is dishonest about them.
- Operator grant commands in `internal/mode/commands.go` push the owner-authored
  family with the ambient shared tag under `OriginCommand`. Harmless while those
  types are `Local`; retagging them changes recorded record domains.
- `EventLevelSetup` and FSM region ops are `Shared` but operator-injectable. Both
  are replicated only because every instance runs the same map script; one
  injected into a single participant rewrites shared state its peers never see.
  `ScriptOptions.MapSetups` holds the first fixed for a parity run, as `Resizes`
  already does; the FSM op is held by `Regions`.
- Ex commands and overlays are operator-injectable, not participant input. The
  two-live criterion disables both: commands include direct scheduler and system
  mutations that are not sent to a peer, while an overlay advances only its App's
  paused clock. These remain valid in replay and single-instance soaks and are
  outside the D-10 wire set by design — but it does mean any operator action
  desynchronises a live session.
- `event.EmitDeath` writes the queue directly, bypassing `PushEvent`, so
  `WithDomain` does not reach death records. Batches are already domain-pure,
  which is what determinism needs; the domain travels as an explicit parameter.
- `uint32(entity)` narrowing at `gateway.go` and `adaptation.go` is safe only
  while route-graph anchors are shared (tag 0).

**Presentation**

- A recording or session on a terminal smaller than the map is clipped by the
  render buffer. The pan offset in `play.go` is the seam a windowed composite
  replaces. Pure presentation, no shared state, no abuse surface; needs its own
  focused session.

### 9.5 Next work

Roughly in dependency order; the first three are one coherent piece.

1. **N-participant session layer.** Replace the two constants and the fixed
   roster with coordinator slot allocation; raise `MaxPeers` and the startup gate
   to the negotiated participant count.
2. **Host rebroadcast.** The star form of the relay: the coordinator forwards
   each peer's epoch to the others, deduped by the existing per-source epoch
   check. Enough for an N-joiner star without touching the barrier's shape.
3. **Membership as a crossing.** Join and leave become artifacts applied at a
   common tick, so a roster change is shared state rather than a local inference
   from a link event.
4. **World snapshot and resume.** Unlocks mid-run join, reconnect and — later —
   the rewind any branch-agreement rule needs.
5. **General relay.** Hop bound, cycle rejection, and a playout lead derived from
   the graph diameter rather than a constant.
6. **Authentication and transport security.** Populate `Config.TLS` from the CLI
   and give `MsgAuthRequest`/`MsgAuthResponse` a meaning, so a participant
   identity exists to attribute artifacts to.
7. **Split `combat.` telemetry per target domain**, returning the group to the
   shared snapshot comparison.
8. **Empty `unstampedLocal`,** then delete it and its exemption.
9. **Windowed composite / vision box.**
