# Multi-instance domain model — vi-fighter

Rules D-1..D-16 describe how one world is split between state every instance
holds and state that belongs to one participant. All sixteen are implemented and
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
| area effect (missile impact, dust detonation, disruptor pulse) | one explosion request: centers, radius, attack family, owner cursor |
| shared progression selecting a drain fusion | one spawn request from the causal participant: header cell only |
| a personal collision selecting a drain fusion | one spawn request for that participant's distinct causal occurrence |
| gold member typed | one composite-member destruction: header, member, typist cursor |
| a dying drain donating its hit points | one heal request: target and amount |
| a personal drain death | one progression event naming the owner cursor |
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

The cursor on `EventDrainDefeated` is causal metadata, not the defeated personal
entity. It elects the one player-domain fuse that may turn a shared escalation
into a spawn crossing (D-16). The personal swarm fusion does not need an election:
each drain collision is already a separate participant-owned occurrence.

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
motion marker, explosion smoke, fuse materialize beams, dust, decay, blossom,
orb, bullet, missile and loot are created from the player counter and may be
created conditionally on local view state (`Player.IsLocal`). They never feed
shared simulation. This is what lets a remote cursor's damage land without its
visuals cluttering the screen. In particular, explosion geometry crosses and is
resolved unconditionally by the shared `ExplosionSystem`; the independently
mergeable and evictable visual center stays in `TransientSystem` and never
decides combat.

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
`EventSwarmSpawnRequest` from the fuse and from a storm; and
`EventGameResetRequest` from the shared monitor FSM or the coordinator's operator
surface. A shared producer's copy is re-derived everywhere; only the
player-domain one crosses. So the tag decides here too: `World.PushCrossing`
stamps the D-3 artifact `DomainPlayer`, and `OnWire` requires it. Crossing-only
Bus types such as `EventDrainDefeated` use the same explicit stamp; class alone
never opts an event onto the wire.
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

The boot script's heat and energy values are a cursor-creation template, not a
second runtime authority. Session admission and full reset copy that template to
every rostered cursor in deterministic slot order, then `EventCursorArmRequest`
restores only the cursor `ResolveOwnedCursor` selects on each instance. This
keeps configuration in the FSM without addressing the roster through one
`player_entity` variable.

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
shared simulation state. Every writer of them must therefore be a function of
state every participant agrees on — and, because a run is reproduced by replaying
its record stream, of state a *reproduction* agrees on too. That second half is
what the rule turns on: a replay and a mid-run catch-up hold no transport and no
terminal of their own.

Writers:

- `World.SetupLevel`, driven by `EventLevelSetup` from the map script. Shared and
  replicated; this is the authority.
- `GameContext.HandleResizeLocked`, driven by this instance's terminal, and
  admissible only while `World.MapSizeLocal()` holds.
- `MetaSystem`'s full reset and its zero-dimension level setup, which return the
  map to the viewport — the same terminal derivation, under the same guard.

`MapSizeLocal()` is `!SessionShared()`, and `SessionShared` is a second rostered
cursor, a bound session transport, or the run's own latch. The roster size is shared.
The latch is not derivable from anything else and is not shared simulation state,
so it travels in the journal anchor (schema 11): a run that opened or joined a
session sets it, and any reproduction of that run adopts it. Reading it
off the live transport instead — which is what it used to do — made a replay crop
where the run it reproduced did not, and left the map croppable in two windows
where it must not be: while a host waits in a lobby, holding out an anchor whose
bounds a joiner has already adopted, and after every participant leaves, changing
the bounds a returning one would replay onto.

*The bounds a participant reproduces must be in place before its FSM boots.* The
boot script spawns cursor slot zero at the centre of the map, and it runs inside
`New`. A joiner that adopted the session's bounds afterwards therefore held that
shared cursor on its own terminal's centre while everyone else held it on the
session's — a shared position, diverging from tick zero, which no crossing
corrects. `Config.MapWidth`/`MapHeight`/`CropOnResize`/`LockMap` carry it, filled
by `ConfigForJoin` and `ConfigFromAnchor`, and `App.applyMapLatch` installs it
before any system is built.

Consequence, instrumented rather than closed: a map script may branch an FSM
guard on `viewport_width`, `viewport_height`, `camera_x`, `camera_y` or
`color_mode`, which are per-instance, and under a locked map those take a
different arm on each instance. The whole script-visible surface is
`internal/engine/config_access.go` — eight keys, of which `map_width`,
`map_height` and `crop_on_resize` are replicated and the other five are not. Both
accessors warn once per key when a non-replicated one is read while
`World.SessionShared()` holds. The keys are retained: D-14 keeps the surface,
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

**D-16 Causal fan-out.** A shared trigger may not fan out to every participant's
player-domain mechanic when that mechanic later crosses one logical shared
result. The shared event must deterministically select one causal cursor before
any participant-owned state is read, and only the instance that simulates that
cursor may produce the crossing. `EventDrainDefeated.Entity` does this for the
tenth-drain quasar escalation: both FSMs enter `QuasarFuse`, but only the
participant that produced the triggering defeat fuses drains and emits the
quasar spawn. Canonical event order selects one cursor if several defeats arrive
at the threshold together.

This rule does not collapse genuinely personal causes. A drain collision that
requests a swarm is one occurrence per participant and may produce one crossing
per occurrence. Nor does D-16 require making drains shared: the election carries
only a shared cursor identity across the boundary, leaving the mechanic that
reads owner-authored drain state in the player domain.

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
| **Shared** | cursor, quasar, swarm, storm, snake, eye, pylon, tower, gateway, wall, gold, marker, FSM, time |
| **Player** | glyph, nugget, dust, drain, decay, blossom, bullet, missile, orb, lightning, flash, fadeout, splash, motion marker, explosion centers, loot |
| **Stamped** | cleaner (request-stamped; every current producer is player), materialize (shared when it gates a shared storm spawn, player for drain and fuse presentation), spirit (shared unless the requester is player-domain, which today is always the fuse) |

Cursor components split three ways: shared-and-replicated (position),
owner-authored (energy, heat, boost, shield, weapon, combat — D-13), and pure
local view (`CursorViewComponent`, `PingComponent`, `PulseComponent`).

`TransientResource` holds local explosion presentation and is player-domain.
Merge distance, visual lifetime and cap eviction may differ freely between
instances. The crossing artifact is instead the immutable geometry in
`EventExplosionRequest`/`EventExplosionBatchRequest`; `ExplosionSystem` consumes
it without consulting `TransientResource`. `ViewResource` (grayout, strobe) is
also player-domain.

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
on timeout or destruction. The FSM carries that entity into the local heat and
energy grant, so only the winning cursor's owner applies the reward. Only the
reward is owner-authored (D-13).

Nugget is personal and uncontested: each instance owns its player-domain spawn,
collection area, destruction and reward, and a remote cursor cannot claim it. A
nugget jump crosses only the resulting shared cursor move. This puts nugget
beside loot, which is also rolled and owned per participant because its mechanic
reads owner-authored state.

Quasar progression is shared but its source drains are personal. D-16 makes the
threshold defeat's cursor the causal owner of the one fusion, avoiding both an
N-way shared spawn and a migration of drains into the shared domain. A swarm
fusion remains personal from trigger through drain selection; only its resulting
shared spawn crosses.

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

**The barrier.** The barrier belongs to the *run*, not to the link. A session's
crossings are deferred by a fixed playout lead and apply at an absolute tick, so a
stretch with no peer attached — a lobby still waiting, every participant gone, or a
replay reproducing the whole session — defers them by the same lead. Deriving it
from the live peer count instead applied a re-derived crossing earlier than the run
had, and a reproduction drifted by exactly the lead. Whether there is anyone to
*send* to is the separate question, and the port answers it.

`event.WireSink.Cross` ends production by encoding and
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

Outside a session `Cross` declines ownership, `Receive` returns zero and the
scheduler creates no wire settle group. The original queue/journal/publication
path is therefore unchanged for a solo run.

A journaled crossing is stamped where it was *consumed*, which is already past the
lead, so a replay republishes it directly through `World.PushRecord` rather than
offering it to the barrier a second time. Its re-derived siblings — the crossings
whose producer is the simulation, which carry `OriginSystem` and are never
journaled — go through the ordinary push and take the lead exactly as the recorded
run did. Both halves are needed: either one alone shifts a reproduction by one
playout lead, which surfaces as a whole gameplay cycle once an FSM deadline falls
inside it.
`network.barrier_{deferred,applied_local,applied_peer,late,ran_without_peer,peer_lag_ticks,peer_artifacts}`
and `network.barrier_peer_applied` expose the barrier state.

**The mesh.** A session is a graph of links, not a star with an authority: an
instance sends only to the peers it dialled or accepted, so an artifact reaches
everyone else by being forwarded. Every node floods each epoch it has not seen to
every link except the one it arrived on. What terminates the flood is the
per-source epoch window — a copy arriving by a second path is recognised and
neither applied nor forwarded again — so each node handles each epoch exactly
once whatever the topology; `parameter.NetworkRelayHopLimit` is a backstop, not
the termination argument. A relay preserves `Source`, `ProducedTick` and every
frame's `ApplyTick` and sequence, which is what lets a relayed artifact apply at
the same absolute tick however many links it crossed. Owner-authored state syncs
relay on the same rule, using the per-slot sequence in place of the epoch window.

The window matters because a mesh reorders. One stream delivers a source's epochs
in order and a high-water mark suffices; a mesh delivers the same source by paths
of different lengths, where an out-of-order epoch is indistinguishable from a
duplicate and would be discarded without ever being applied.
`parameter.NetworkEpochWindow` admits each epoch once in any arrival order, over a
64-epoch backlog — three seconds at 20 ticks/s, beyond any path that could still
meet its apply tick. `network.relay_forwarded` and `network.relay_duplicates`
expose the flood.

**Runtime parity.** Every six completed ticks, each instance sends its direct
neighbours a digest of exactly the state surface `SnapshotShared` compares. The
sample names the run and absolute tick and carries category hashes for position,
kinetic, combat, context and status diagnosis; a ring holds local samples so
sequential polling or different link latency cannot compare unlike ticks. Digest
messages do not flood: equality on every edge implies equality across a connected
graph. A mismatch increments `network.digest_mismatches`, logs its first differing
category on the transition and holds a high-priority `DESYNC` status-bar item.
Once every neighbour agrees again, a green `SYNCED` acknowledgement remains for
twenty ticks and disappears. This detects divergence; it neither chooses an
authoritative copy nor repairs one, and it deliberately excludes D-13
owner-authored values.

Divergence has two degrees, because one disagreeing sample and a permanent one
call for different statements. An artifact that missed its apply tick lands on one
side a tick late and the next sample finds the two equal again, so the indicator
waits for `NetworkDesyncSamples` consecutive disagreements before reporting.
`NetworkDivergedSamples` of them is past anything the participants could still
resolve between themselves — nothing re-derives a missing artifact — so the
session publishes `network.diverged`, logs at error, and the indicator turns from
amber `DESYNC` to red `DIVERGED`. `network.sync_part` and `network.sync_tick` name
the first differing category and the tick it was first seen on, so the diagnosis
survives into `:d` and the journal. Agreement clears both degrees.

**Membership.** A roster change is shared state, so it travels as an artifact
rather than as a local reaction to a link event. A disconnect is observed only by
a direct neighbour, and at a moment of that neighbour's own transport's choosing:
acting on it where it is seen would remove a shared cursor at a different tick on
every instance, and not at all on one that never linked to the departing
participant. Exactly one instance therefore turns the observation into a
crossing — the coordinator, the one participant every topology this session can
build has a path to — and a neighbour that is not the coordinator forwards a
`MsgDisconnect` notice instead, deduped by departing participant. An arrival
crosses the same way, so every instance creates the new cursor at one agreed tick
and its shared entity is identical everywhere (D-11). Both carry `OriginSession`,
which is journaled: nothing else in the record stream implies a roster change.

**Session control.** Time control remains an instance-local operator facility,
so pause, speed and step requests are refused while a live peer is attached.
Command and overlay modes remain usable for inspection without stopping the
simulation. A full game reset is different: it is one logical shared action and
the coordinator is its single producer. `:new`/`:new!` on the coordinator crosses
`EventGameResetRequest`; a guest request is refused, while a reset emitted by the
shared monitor FSM is still re-derived rather than sent. The agreed reset event
snapshots the closed roster, clears the world and barrier, then rebuilds every
cursor in slot order from the boot template. Thus reset changes the run without
silently reducing the session to slot zero.

**Mid-run join.** A participant arriving after tick zero reproduces the session
rather than receiving it. A run is a pure function of its anchor and its
non-system record stream, which is what replay already relies on, so a host
retains that stream for the life of the session and the handshake carries it. The
log is unbounded and a frame is capped at 64 KiB, so it crosses as sequenced
chunks; past the offer the deadlines are per-write, because the transfer is as
long as the session rather than as long as the link. `App.CatchUp` replays it,
ticks over the quiet stretch the records do not cover, and discards the barrier
artifacts the log already applied. It does not pre-adopt the D-14 latch: the
records carry the level setup that produced it, and adopting as well would run
that event twice. The cost is memory — the log is complete from tick zero because
replay is, and grows with session length.

**The stream.** The real endpoint is `network.SocketPort`. Every message has a
fixed 12-byte header whose final field is payload length; `Decode` uses
`io.ReadFull` for both header and payload, and `Encode` completes short writes.
Transport goroutines append only `network.Inbound` values to the port buffer;
`NetworkSystem` drains that buffer under the world lock, preserving the poll
boundary. Idle peers exchange framed heartbeats; read and write deadlines close a
silent stream without blocking a tick. The resulting disconnect notification is
drained through the same path and removes only cursors owned by that participant.
The steady-state simulation stream has three message kinds — one closed barrier
epoch, one owner-authored cursor sync and one shared-state digest — plus the
membership notice and four-step startup handshake; anything else is counted as a
drop rather than translated.

Loss that happens outside the barrier is counted, because either kind
desynchronises silently: `network.transport_lost_in` is inbound notifications a
full poll buffer discarded, `network.transport_lost_out` is outbound frames a
peer's send queue refused. A new loss is also logged once.

**Startup.** The handshake sends the existing `JoinAnchor` inside `SessionOffer`.
The coordinator allocates a participant ID and a roster slot per accepted
connection and releases it when a handshake fails or a participant departs, so the
lobby grows to `parameter.MaxPlayers` rather than being fixed at two; `-players`
sets the size it waits for. `App.Join` checks identity as soon as a joiner
arrives, so schema, tick interval, seed, session, config, corpus and D-14 latch
mismatches are refused before the rest of the lobby waits on it. A rejected
connection never enters the peer manager. Canonical participant IDs, not
connection-local accept order, key the barrier sort and roster cleanup.

The roster every instance builds from arrives with the **start gate**, not with
the offer: a joiner that dialled early saw only the participants ahead of it, and
building from that partial view would give each instance a different shared
creation order (D-11). The gate is otherwise startup coordination only — no
per-tick round trip — and it is what gives every participant the same tick origin
the barrier's absolute apply ticks presume. The interactive game clock is frozen
before the FSM creates any deadline and released only after this gate. Without
that hold, the host's lobby wait aged its ten-second gold timer before tick one
while a late-created joiner's timer remained fresh.

`cmd/vif` exposes that gate as startup flags rather than ex commands: `-host
<bind-address>`, `-join <host:port>` and `-players <n>`. A host initializes its
terminal, world and listener, renders a lobby message, and holds the scheduler at
tick zero until every expected participant is ready. A joiner dials before
constructing its `App`, so `ConfigForJoin` installs the host seed, config and
corpus identity before `initWorld` can draw a seed or load content. Both sides
activate the crossing sink before terminal input is consumed. The host remains
playable and listening after a disconnect. The App/transport layer can catch a
later connection up from the retained log rather than refusing it, but `cmd/vif`
does not yet complete the running-host tick-phase handoff described in §9.4.

**Cost.** The wire keeps journal TOML payloads inside a JSON epoch envelope. The
measured complete frames, including the 12-byte header, are 44 bytes for an empty
epoch, 567 bytes for four cursor moves, 1,771 bytes for six resolved three-member
shield hits, and 703 bytes for one D-13 owner-state sync. At 20 ticks/s with the
six-tick state cadence, that is about 3.2 KB/s idle, 13.7 KB/s at four crossings
per tick, or 37.8 KB/s at the deliberately busy shield rate, per direction and
owned cursor; the small run/tick/hash probe and its category hashes arrive at the
same six-tick cadence.
A denser payload codec does not justify a second registry/schema path at these
rates. `TestWireEncodingBudget` pins the representative budgets;
`TestFrameRoundTripSurvivesShortStreamIO` pins framing.

Journal schema is 11, and the wire shares its encoder: 7 made `Domain` meaningful,
8 added the D-14 map latch to the anchor, 9 moved the nugget event family out of
the replicated record set after the mechanic became personal, 10 separates
explosion combat from presentation while adding the roster template and causal
fusion fields, and 11 adds `SessionShared`, the D-14 crop admissibility a
reproduction has to adopt rather than derive from a transport it does not hold.

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
counters, `nav.entities`, `content.served`/`content.rejected` beside the corpus
fingerprint, and `engine.tick_slips`, `time.game_elapsed_ms` and `gold.timer`,
which are local scheduler/wall-time gauges rather than shared state. The shared
tick, FSM state and timeout result remain compared. A `.buf_*_hwm` suffix drops
scratch high-water marks, which
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

The runtime digest reuses this filter rather than maintaining a second idea of
parity. It folds the shared snapshot records plus canonical shared position,
kinetic and non-cursor combat digests into FNV-1a 64. The hash is a detector, not
a proof or a repair protocol: collision risk is accepted for a frequent warning
signal, while `SnapshotShared`, `FirstDiff` and `Diff` remain the diagnostic that
names the first differing record in tests.

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
| `TestOneSharedQuasarTriggerProducesOneSpawn` | `internal/app` | D-16: a shared progression trigger elects one causal player fuse and yields one shared spawn |
| `TestPersonalNuggetUsesPlayerDomainAndLocalCursor`, `TestPersonalNuggetJumpCrossesOnlyCursorMove` | `internal/system` | §4: nugget is personal; only the cursor move crosses |
| `TestSharedSpeciesCrossesOnlyOwnedShieldImpact`, `TestCursorDefeatTransitionCrossesCombinedOwnerState`, `TestMetaDefeatGateRequiresEveryRosteredCursor` | `internal/system` | D-13: a remote shield cannot produce a second impact; defeat state crosses instead of being read from slot zero |
| `TestRemoteCursorRejectsOwnerAuthoredWrites`, `TestRemoteCursorStateDoesNotAgeLocally` | `internal/system` | D-2: neither a grant nor a per-tick loop writes a cursor this instance does not simulate |
| `TestCursorStateSyncWritesOnlyACoherentRemoteCursor`, `TestDepartureReleasesTheSlotSyncSequence` | `internal/system` | D-13 receive side: entity and slot must agree, sequences gate replays, a released slot accepts a successor |
| `TestBusPayloadsNameOnlySharedEntities` | `internal/app` | D-4 over a soak, via a dispatch tap |
| `TestLocalEventsCarryThePlayerDomain` | `internal/app` | D-10: a Local-class record is tagged player, against a shrinking exemption set |
| `TestDomainAuditSoakClean` | `internal/app` | Zero component-domain violations over a 3,000-step soak |
| `TestMapSizeLockedWithSecondCursor`, `TestMapSizeCropsWithOneCursor` | `internal/app` | D-14, with the crop path as its own negative control |
| `TestJoinerOnAnotherTerminalSharesTheMapFromTickZero` | `internal/app` | D-14/D-11: a participant on a different terminal holds the boot cursor on the session's cell, not its own |
| `TestSessionRunNeverCropsItsMap` | `internal/app` | D-14: a run that opened a session keeps its bounds through a resize, so the anchor it offers cannot move |
| `TestSharedGlyphsAreGoldMembersOnly` | `internal/app` | §4: every shared-domain glyph is a gold composite member |
| `TestSharedSnapshotParityAcrossTerminalSizes` | `internal/app` | D-11: two instances of one seed on different terminal sizes agree at every step |
| `TestObserverSharedStateTracksTheLiveParticipant` | `internal/app` | 1,200 steps of an observer whose shared state arrives over the wire rather than re-derived |
| `TestTwoLiveParticipantsStayInLockstep` | `internal/app` | 1,200 steps, two live participants, both moving, both crossing, both nonzero APM |
| `TestTwoLiveParticipantsStayInLockstepOverTCP` | `internal/app` | The same criterion through `127.0.0.1`, plus handshake, roster, framing, clean remote-cursor removal on disconnect, and a real mid-run join |
| `TestChainRelayReachesANonAdjacentParticipant` | `internal/app` | §6: a crossing reaches a participant its producer never linked to, at the same tick; fails without the relay |
| `TestMeshPropagatesEveryParticipantToEveryOther` | `internal/app` | Five participants in 1—2, 2—3, 3—4, 3—5 agree on every shared record through 240 driven steps |
| `TestDepartureReachesTheWholeMesh` | `internal/app` | A departure removes the cursor on an instance that never linked to the departing participant |
| `TestThreeParticipantLobbyClosesOnOneRoster` | `internal/app` | The socket handshake for a lobby larger than a pair: partial offers, one closed roster |
| `TestLateJoinerReplaysTheSessionToTheHostPosition` | `internal/app` | Mid-run join: replaying the log onto a different terminal reaches byte-identical shared state |
| `TestCatchUpReproducesALiveSessionsCrossings` | `internal/app` | §6: a reproduction of a *session* takes the playout lead on re-derived crossings and not on journaled ones; either mistake alone drifts it by the lead |
| `TestLateJoinerTakesTheRosterAndStaysInLockstep` | `internal/app` | The arrival crossing lands on both instances at one tick, and both then drive their own cursor in lockstep |
| `TestSessionRosterStartsAndRestartsEveryParticipant` | `internal/app` | Every closed-roster cursor receives the boot template at admission and survives the monitor's global reset |
| `TestLiveSessionRefusesAnInstanceLocalPause`, `TestCoordinatorResetCrossesAndPreservesRoster` | `internal/app` | Live operator policy: time cannot stop on one instance; the coordinator serialises a full reset without collapsing membership |
| `TestExplosionPresentationStaysWithItsProducer`, `TestExplosionCombatDoesNotDependOnVisualMergeState` | `internal/app`, `internal/system` | D-3/D-6: smoke remains local while immutable geometry always resolves shared combat |
| `TestRuntimeDigestReportsAndClearsSharedDivergence`, `TestStatusBarSyncIndicatorUsesAlertAndRecoveryColors` | `internal/app`, `internal/render/renderer` | A deliberate shared corruption is not reported on its first sample, becomes amber `DESYNC`, escalates to red `DIVERGED`, and equality clears both through a transient green `SYNCED` |
| `TestSharedSnapshotExcludesLocalSchedulerTiming` | `internal/app` | Runtime parity ignores independent wall origins and deadline-slip telemetry while keeping absolute simulation tick/state |
| `TestLinkLossDoesNotDespawnWhereItIsObserved` | `internal/system` | A lost link produces an artifact, not a removal, and a second notice is a duplicate |
| `TestSessionLogSplitsAndRoundTrips`, `TestSessionLogChunksFitOneFrame` | `internal/event` | The catch-up transfer is lossless and every chunk fits one frame |
| `TestActivatedSessionDefersCrossingBeforeFirstTick` | `internal/app` | Input arriving before the first system update enters the barrier rather than applying locally |
| `TestAppsScopeOperatorState` | `internal/app` | Two Apps drive resize and debug mutations without cross-talk |
| `TestWireEncodingBudget`, `TestFrameRoundTripSurvivesShortStreamIO` | `internal/event`, `internal/network` | Representative stream cost; framing survives short stream I/O |
| `TestBroadcastReportsRefusedFrames` | `internal/network` | A refused outbound frame is counted rather than swallowed |

The mesh harness is `network.Mesh`, an in-process link graph: what a node sends,
its direct neighbours drain on their next tick. A real socket adds framing and
latency, neither of which the domain rules depend on, but unlike a single stream
it can express a topology that is not a star — which is the only way to test that
an artifact reached a participant its producer never sent it to.

Supporting machinery: `engine.PinDomainAudit`/`DomainMismatches`/
`DomainViolations`; per-system audit attribution in `UpdateLocked`, falling back
to `"event"` for settle-pass attaches; `ClockScheduler.SetDispatchTap` and
`App.SetDispatchTap`; `ScriptDriver` with `Step()` for lockstep driving;
`ScriptOptions.Resizes`/`MapSetups`/`MapMotionsOnly`; `FastRand.State()`.

The two-live harness owns one tick per participant per step. It disables random
script ticks and the overlay round trip (whose driver explicitly ticks one App)
so neither App can outrun the three-tick playout lead. The long random criterion
holds `MapSetups`, FSM `Regions`, resets and ex commands fixed to isolate
participant gameplay and avoid deliberately restarting the run.

Two things it deliberately does *not* hold fixed any more, because holding them
fixed is what let a resize desynchronise a live session with every test passing.
`pair` joins a second participant on a **different terminal size**, so no
viewport-derived value can match by accident; and `liveScript` drives **resizes**
and **viewport-relative motions**, so each participant's terminal and camera move
under the session. Both criteria carry them, the socket one included — and that one
ends in a mid-run catch-up, so the same profile also proves the reproduction.

Effort is tiered rather than fixed. `soakScale(short, normal, full)` picks a
repetition or step count per profile: `-short` for a smoke run, the default for
what a change is validated against, and `VIF_SOAK=full` for the wide seed sweep.
The default profile keeps every seed reproducible from its name while bringing a
`-race` run of the whole tree to about two and a half minutes; `internal/app`
alone used to take nearly six. Separate
multi-participant tests exercise the live operator policy: instance-local time,
system, raw event and FSM controls are refused, while the coordinator's reset is
transported under D-10.

### Manual two-terminal proof

```bash
# terminal 1
./bin/vif -d -host 127.0.0.1:7777

# terminal 2
./bin/vif -join 127.0.0.1:7777
```

Both status bars must reach `NET:1P/LOCK`; each terminal must show both cursors,
and both local cursors must begin at heat 10 and energy 100. Movement, typing,
combat and scoring from either side must resolve onto the same shared actors.
Open `:` or an overlay on one terminal and leave it open: both simulations keep
running; `:speed` and `:step` report that they are unavailable. The host's `:new`
must reset both terminals while preserving two cursors; the joiner's `:new` must
be refused. The tenth drain defeat must produce one quasar, and missile smoke
must remain on the firing terminal even though its damage resolves on both.

Give the two terminals **different sizes**, and resize one of them mid-run — a
tmux pane change is the ordinary case. The map must not move on either side and
neither status bar may show `DESYNC`: a resize reflows one instance's view and
touches no shared state. The latch stays `LOCK` for the life of a session run,
including before a joiner has arrived and after it leaves, so `NET:WAIT/LOCK` and
`NET:DOWN/LOCK` are both expected.

No healthy run should show the amber `DESYNC` item, and none should ever reach red
`DIVERGED`, which says the two are past resolving it between themselves. Quit the
joiner: the host must change to `NET:DOWN/LOCK`, remove only the remote cursor and
continue accepting local input. `:d save` is refused while peers are live: its
synchronous logger drain holds the world lock and can overrun the playout lead.
On a solo or replayed copy it is still not a byte-for-byte parity diagnostic
because it deliberately includes local view and owner-authored metrics; the
runtime digest compares only the shared surface. A divergence is a
`DESYNC` indication, a different shared actor, position, kill or progression
result, or a nonzero `network.barrier_late`/
`network.barrier_ran_without_peer`/`network.transport_lost_*` trend under an
otherwise healthy link.

For a larger lobby the host names the count it waits for and each participant
joins the same address:

```bash
./bin/vif -d -host 127.0.0.1:7777 -players 4
```

The status bar reaches `NET:<n>P/LOCK` once the lobby closes, and every terminal
must show every cursor. The same binary works on a LAN by binding the host to
`:7777` or `0.0.0.0:7777` and joining its reachable address. Internet use is the
same socket path but remains a trusted-peer proof: it requires external
firewall/NAT routing and currently carries plaintext with no authentication.

## 9. Analysis: authority, topology and open work

### 9.1 Who decides what

There is no single authority, and asking "which peer is the host" only has a
useful answer for one of the three kinds of state.

| State | Authority | Mechanism |
|---|---|---|
| Shared simulation | **None** | Every instance re-derives it from the same seed, config, corpus, map script and ordered artifact stream (D-11). Agreement is a property of determinism, not a decision. Nothing is sent: `OnWire` excludes the `Shared` class precisely because sending it would apply it twice. |
| Owner-authored shared state (D-13) | **Per cursor**, the instance that simulates it | `SimulatesLocally` admits exactly one writer; the value is transported, never re-derived. This is per-object, not per-session, and does not depend on topology. |
| Session identity, map bounds, roster changes and live operator reset | **The coordinator** | The `JoinAnchor` (schema, tick rate, seed, session counter, config and corpus identity), the D-14 map latch, participant IDs, roster slots, barrier delay, arrival/departure crossings, and serialization of the exceptional session-wide reset command. |

The coordinator is not a state authority. It owns its own cursor's D-13 cells and
nothing else; it cannot correct, override or arbitrate another participant's
shared state, because it holds no copy more authoritative than anyone else's.
What it owns is *allocation and serialization* — deciding who is in the session
and ensuring a session-wide operator reset has one producer, not choosing a
gameplay outcome. A roster change or reset it announces is applied by everyone
from the same artifact at the same tick, exactly like a crossing any participant
could have produced.

So the chain question — *A joins B, B joins C; who is the host for A and C?* — has
two halves, and they separate cleanly.

**Identity** is answered by the model rather than by the graph: **the coordinator
is whoever issued the `SessionOffer` a participant adopted**, and the right
behaviour for a chain is not to elect a host per link but to propagate one session
identity. Every participant in one session adopts the same anchor, the same
participant-ID space and the same barrier delay, whoever physically hands it over.
A relay forwards an offer; it does not mint its own.

**Propagation** is not an authority question at all, and was the real gap. A
crossing A produces has to reach C, which A never sends to — and it does, because
every node floods each artifact it has not seen to every link but the one it
arrived on, and every artifact names the absolute tick it applies at. C applies
A's attack on the same tick A did, having received it from B. Nothing about that
depends on who the host is; see §6 and §9.2.

### 9.2 Topology

A session is a **mesh**: participants exchange artifacts over whatever links they
have, and everything below the session layer is participant-shaped rather than
pair-shaped. `SessionOffer.Participants` is a slice, the epoch window is indexed
by participant ID, the barrier sorts by *(apply tick, participant ID, sequence)*,
and `ScheduledWireFrame.ApplyTick` is an **absolute** tick, not a relative offset.
That last property is what makes relaying sound: a forwarded artifact still
applies at the same tick everywhere, because the tick it names travelled with it.
Propagation itself is a flood with per-source suppression (§6), so A—B—C reaches
C without A ever sending to it.

What the coordinator still is, and only is: the allocator of participant
identities and roster slots, the source of the anchor and the D-14 latch, and the
single producer of roster-change and live operator-reset crossings. It holds no
shared state anyone else does not.

Three things remain open, and each is a property of the graph rather than of the
transport:

1. **Delay is a constant, not a diameter.** `NetworkBarrierDelayTicks = 3` is
   negotiated once at 150 ms. An artifact crossing several links must still
   arrive before its absolute apply tick, or it lands late and the instances
   diverge. `network.barrier_late` is the signal that the lead is too small; the
   lead should become a function of the worst-case path.
2. **Departure needs a reachable coordinator.** One producer is what gives a
   roster change a single apply tick, and the coordinator is the participant every
   topology the session can currently build has a path to. If it departs, or a
   partition puts it out of reach, no departure is announced. Electing a
   replacement is a membership-agreement problem, not a transport one.
3. **A partition has no session-wide detector.** Direct neighbours observe their
   lost link, but after a graph splits there is no digest edge between the two
   components. Both can keep simulating and each can agree internally while their
   shared states diverge.

The links themselves are still built as a star, because `-join` dials one address.
The relay is what makes any other shape work; wiring a participant to dial more
than one peer is a CLI change, not a protocol one.

### 9.3 Toward a trusted branch

The eventual "which branch do we trust" question is easier here than in a system
that replicates state, and the reason is worth recording. Because shared state is
re-derived rather than transported, there is nothing to reconcile *except the
artifact stream*. A divergence is always a disagreement about which artifacts, in
which order, at which tick — never about the resulting world. The frame already
carries the three fields such a log needs (source, per-source sequence, absolute
apply tick), and the ordering is already independent of arrival order.

The periodic shared digest now makes such a disagreement loud while the peers
remain connected. It intentionally stops there: a hash does not identify the
missing or extra artifact, establish which branch is trusted, cross a partition,
or authorize overwriting one participant's state. The retained record stream and
`FirstDiff` remain the evidence for diagnosis; identity and an agreement rule are
still prerequisites for automated recovery.

Rewind is no longer the obstacle it looks like. A run is a pure function of its
anchor and its record stream, and a host retains that stream, so any position in
the session can be reconstructed by replaying to it — which is exactly what a
mid-run join now does. Rejecting a branch after the fact is therefore replay to
the fork point and forward along the other one, not a snapshot-and-rollback
mechanism the engine lacks. What it costs is time proportional to session length,
which is what a periodic world snapshot would bound.

What is genuinely missing is **identity**. The link is plaintext and
unauthenticated; `MsgAuthRequest`/`MsgAuthResponse` are reserved and unused, and
`Config.TLS` is never populated by the CLI. A participant cannot prove that an
artifact attributed to it is its own, so there is nothing for an agreement rule to
bind to. That is the prerequisite, not rewind.

### 9.4 Known limitations

**Session and transport**

- The playout lead is a constant rather than a function of the graph's diameter,
  and a partition leaves no digest edge between its components. See §9.2.
- Mid-run join is bounded by memory and by wall clock: the retained log is
  complete from tick zero, so it grows with session length, and catching up costs
  time proportional to it. A periodic world snapshot is what bounds both.
- Mid-run join is not yet driven from `cmd/vif` against a *live* host. The
  mechanism, the transfer and the roster crossing are in place and proven over the
  socket, but a running host advances while a joiner replays, so the handoff needs
  either the host to hold its advance across it or the joiner to buffer epochs and
  fast-forward onto the session's tick phase. Only the second scales.

  What the handoff needs is now specific. Three pieces, none of them large on its
  own: the coordinator serving a *delta* log from the tick a joiner reached rather
  than only a complete one, so the residual gap shrinks geometrically across two or
  three rounds instead of being whatever the first replay cost; the coordinator
  re-sending its last `NetworkBarrierDelayTicks` epochs to a peer that has just
  connected, because those artifacts apply after the log ends and were broadcast
  before the joiner attached; and the joiner ticking to the session's phase before
  its scheduler starts, which the barrier already supports since every artifact
  names an absolute apply tick. Nothing below them is missing: reproducing a live
  session's crossings is exact, and is pinned by
  `TestCatchUpReproducesALiveSessionsCrossings`.

- Reconnect reuses the same machinery and is not separately wired: an identity is
  released when its participant departs, and a returning participant catches up
  like any other late arrival.
- There is deliberately no live pause, slow motion or stepping. Suspending one
  participant for minutes needs the same buffered catch-up and tick-phase handoff
  as reconnect; the retained log is the source, but `cmd/vif` does not yet drive
  that transition.
- The runtime digest detects connected-peer divergence after its six-tick sample
  cadence but neither stops play nor repairs it. `SYNCED` means the compared
  surface became equal again; it does not explain why. `DIVERGED` states that it
  will not: past `NetworkDivergedSamples` nothing re-derives the missing artifact,
  and the only recovery the model admits is reproducing the session from the
  coordinator's log — which is the mid-run join above, with its prerequisites.
- Plaintext and unauthenticated; no CLI TLS surface.
- No lag compensation. A slow peer produces late artifacts and divergence rather
  than a stall — deliberate, since simulation never waits, but it means the
  playout lead is the only defence.
- A refused outbound frame is counted and logged but not retransmitted. The stream
  itself is reliable and ordered; the loss is in the bounded send queue ahead of it.
- `float64` simulation means cross-platform bit-exact lockstep is not claimed; the
  guarantee is per implementation build.
- A coordinated game reset deliberately discards all already-scheduled future
  artifacts and restarts the epoch. Every participant does so at the reset's
  agreed apply tick; there is no selective carry-over into the new run.

**Domain boundary**

- `unstampedLocal` in `internal/app/local_stamp_test.go` still holds 18
  `Local`-class types pushed with the ambient shared tag from app, engine, fsm and
  the shared species systems. Not a transport gate — the class keeps them off the
  wire regardless — but the journal record is dishonest about them.
- Operator grant commands in `internal/mode/commands.go` push the owner-authored
  family with the ambient shared tag under `OriginCommand`. Harmless while those
  types are `Local`; retagging them changes recorded record domains.
- The live ex-command policy refuses time control, system toggles, raw `:emit`,
  FSM region operations and synchronous `:d save`; overlays and non-blocking
  inspection remain local and do not pause. Snapshot save is excluded because
  its world-lock hold can exceed the fixed playout lead even though it does not
  mutate simulation state.
  Public embedder calls such as `App.SetupLevel` can still inject shared scheduler
  state directly and remain the caller's responsibility. The random parity script
  holds those programmatic operator paths fixed.
- Player grant and effect commands remain available live because they author only
  the invoking cursor's D-13 state or enter the ordinary player-domain crossing
  path. They are development cheats, not a determinism exception. In a live
  `:new!`, only the coordinator's own operator preferences are purged; peers reset
  simulation but retain their local mouse, auto-fire and overlay choices.
- The optional `config/main/tower.toml` and `config/td` scripts still capture one
  `player_entity` and assign every shared tower to that cursor (deterministically
  slot zero). They no longer collapse or mis-arm the roster, but deciding whether
  towers should be session-owned or participant-owned is a separate gameplay
  rule, not required by the default map defects fixed here.
- `event.EmitDeath` writes the queue directly, bypassing `PushEvent`, so
  `WithDomain` does not reach death records. Batches are already domain-pure, which
  is what determinism needs; the domain travels as an explicit parameter.
- `uint32(entity)` narrowing at `gateway.go` and `adaptation.go` is safe only while
  route-graph anchors are shared (tag 0).
- `combat.` telemetry is a mixed aggregate and is dropped whole from the shared
  snapshot. *Closed as not worth doing*: the shared world digest already hashes
  hit points, enrage, stun and both immunities for every non-cursor combatant, so
  splitting twenty-odd counters per target domain would restore a comparison that
  is already covered.

**Presentation**

- A recording or session on a terminal smaller than the map is clipped by the
  render buffer. The pan offset in `play.go` is the seam a windowed composite
  replaces. Pure presentation, no shared state, no abuse surface; its own task.

### 9.5 Next work

1. **Live mid-run join in `cmd/vif`,** on the three pieces §9.4 now names, and the
   catch-up fidelity defect ahead of them. This is what turns the proven mechanism
   into a feature a player can use; it carries reconnect and a future
   suspend/resume form of live pause with it, and it is the one recovery a
   `DIVERGED` session can be given — the participant rejoins and reproduces rather
   than being repaired in place.
2. **A playout lead derived from the graph.** Negotiate it from the worst-case path
   rather than fixing it at three ticks, and act on `network.barrier_late` instead
   of only reporting it.
3. **Periodic world snapshot.** Bounds both the retained log and catch-up time, and
   is the same primitive a branch-agreement rule would rewind to.
4. **Multi-link topology from the CLI.** The relay makes any graph work; `-join`
   dialling more than one address is what lets an operator build one.
5. **Partition and membership health.** The shared digest detects divergence on
   connected edges, not a graph split. Add a membership rule that detects a
   partition and survives losing the coordinator.
6. **Authentication and transport security.** Populate `Config.TLS` from the CLI and
   give `MsgAuthRequest`/`MsgAuthResponse` a meaning, so a participant identity
   exists to attribute artifacts to. This is the prerequisite for any agreement
   rule, and the only one still entirely missing.
7. **Close the programmatic operator surface.** Make embedder-level map/FSM
   mutation explicitly session-aware rather than relying on the interactive
   command policy and harness discipline.
8. **Empty `unstampedLocal`,** then delete it and its exemption.
9. **Settle tower ownership in optional maps.** Replace their slot-zero
   `player_entity` convention with an explicit session-owned or participant-owned
   rule.
10. **Windowed composite / vision box.**
