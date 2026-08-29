# Multi-instance domain model — vi-fighter

Status: Phases 4, 5 and 5.5 landed, verification included. Rules D-1..D-15 are
implemented unless marked. Supersedes every earlier design note.

## 1. Domains

Two per `World`: **Shared**, identical on every instance and replicated, and
**Player**, this instance's participant and never replicated. One `World` per
local participant. The roster slot lives on `CursorComponent`; it is not part
of the domain tag.

`core.Entity` is `[domain:8][id:56]`. `core.DomainNames` indexes the domain for
seed derivation, telemetry keys and log fields — changing a name re-keys every
stream in that domain.

## 2. Rules

**D-1 Reads.** A player-domain system may read shared state. A shared-domain
system reads shared only. Exceptions are D-12 and D-13, both explicit.

**D-2 Simulation ownership.** Only the instance owning a cursor simulates that
cursor's weapons, projectiles and player species. A remote participant's
player-domain state does not exist locally and is never reconstructed.

**D-3 The crossing.** When a player mechanic affects a shared entity, the
smallest artifact that determines the shared outcome crosses as a Bus event:

| effect | crossing artifact |
|---|---|
| direct hit (rod, cleaner, bullet) | one combat event per shared target |
| area effect (missile impact, dust detonation) | one explosion request: centers, radius, duration, attack family, owner cursor |
| drain fusion | one spawn request: header cell only |
| gold member typed | one composite-member destruction: header, member, typist cursor |
| decay or drain reaching a shared nugget | one nugget destruction: the nugget identity |
| a dying drain donating its hit points | one heal request: target and amount |
| the post-typing cursor advance | one cursor move request: the shared cursor and its cell |

The table is `crossingPushes` in `internal/system/event_class_test.go`, and the
test fails on a player-profile system pushing a replicated event that is not in
it. The last three rows were found that way: they are crossings the design did
not name, and each needs a wire path in Phase 7 exactly as the first four do.

Effects on player targets do not cross. The producer resolves its own domain
*before* pushing the crossing event; the shared consumer resolves only shared
targets.

The gold row is a keystroke crossing: `TypingSystem` is player-domain and
`EventCompositeMemberDestroyed` names a shared member. Its payload carries the
typist (`CompositeMemberDestroyedPayload.Entity`), which is what makes the
credit a function of shared events rather than of who happened to type last.
`GoldSystem` tallies per roster slot and `GoldCompletionPayload.Entity` names
the cursor that typed the most members, ties resolved to the lowest slot so
every instance credits the same one. Timeout and destruction leave it zero.

**D-4 Payload purity.** A Bus payload names only shared entities. Player
emitters are reduced to coordinates and velocity (`HasOrigin`, `OriginX/Y`,
`HasVelocity`, `OriginVelX/Y`). A Local payload may name player entities
freely — `EventFuseSwarmRequest` and the lightning triple do. Asserted over a
soak by `TestBusPayloadsNameOnlySharedEntities` in `internal/app`.

**D-5 Derived, not transported.** Events a shared system produces from a Bus
event are re-derived identically on every instance and must never themselves be
transported. `EventExplosionBatchRequest` crosses; the
`EventCombatAttackAreaRequest`s it produces do not.

**D-6 Effect entities are player-domain.** Lightning, flash, fadeout, splash,
motion marker, dust, decay, blossom, orb, bullet, missile and loot are created
from the player counter and may be created conditionally on local view state
(`Player.IsLocal`). They never feed shared simulation. This is what lets a
remote cursor's damage land without its visuals cluttering the screen.

**D-7 Ambient domain.** `World.WithDomain(d, fn)` mirrors `WithOrigin`;
`PushEventDomain` and `PushLocal` stamp explicitly for producers outside any
scope. One system can serve both domains without splitting: `MaterializeSystem`
gates a shared species spawn and a player drain from one code path, reading the
request's domain rather than being duplicated, and stamps the completion with
the domain of the entity it completed. This is the general answer to generic
types (death, timer, spirit, materialize, species lifecycle) — they are
stamped, not statically classified.

Cleaner was this rule's original example and is no longer one. All three
producers — nugget beacon, weapon, and the `:cleaner` command — push
`core.DomainPlayer`, the beacon since Phase 5, so every cleaner is
player-domain and its request events are `Local`. `CleanerSystem` still resolves
both and keeps its `dual` profile, which is defensive rather than exercised.

The ambient tag is **not** derived from the declared system profile:
`UpdateLocked` sets the audit scope from `SystemDef.Domain` but leaves
`World.domain` alone, so an unscoped `PushEvent` from a player-profile system
still stamps `shared`. Opting in is the producer's job — see D-10.

**D-8 RNG.** `RandResource.Stream(domain, label)` derives from
`(sessionRoot, domain, label)`. A system resolving both domains holds one
stream per domain and selects by the target's domain; `CombatSystem` and
`SoftCollisionSystem` are the only two, asserted by
`TestCombatKnockbackDrawsFromTheTargetsStream` and
`TestSoftCollisionImpulseDrawsFromTheTargetsStream`. A wholly player-domain
system draws one player stream: `FuseSystem`, `DrainSystem`, `LootSystem`,
`LightningSystem`. No simulation path seeds from a clock;
`TimeResource.GameTimeNano` is explicitly not a seed source.

**D-9 Entity identity.** `World.nextEntityID [2]uint64`; `CreateEntity(domain)`
explicit at every call site; `Clear()` resets both. Zero remains invalid in
both domains. Created and destroyed counts are tracked per domain
(`CreatedCountDomain`, `DestroyedCountDomain`); the aggregate accessors sum
them.

**D-10 Event domain.** `GameEvent.Domain` is stamped at push from the ambient
domain and carried through to `JournalRecord.Domain`, which the vlog sink
writes and `internal/journal` parses. Registry classes: `Shared` (emitted and
consumed shared, replicated), `Bus` (player-originated, affects shared,
replicated), `Local` (never replicated), `Stamped` (class determined per-event
from the domain tag). The registry table itself is Phase 6; today the class is
documented, not declared.

Two facts constrain how the table can be built:

- The tag is opt-in. `World.PushLocal` is the only opt-in in wide use, and it
  hard-codes `DomainPlayer`; everything else inherits the shared default (D-7).
  A soak census over three seeds tags 70 of the 91 observed types `shared`,
  including unambiguous D-6 effects that are emitted from FSM actions rather
  than from a system. A `Local` class is therefore a *claim about producers*
  that has to be enforced, not a fact readable off today's tags.
- `Stamped` is genuinely per-instance for at least two types.
  `EventCombatAttackDirectRequest` crosses only when
  `ChainDepth == 0 && TargetEntity.Domain() == DomainShared`; the same census
  shows `EventSpeciesKilled` mixed (53 player, 1 shared) from one producer. No
  static per-type table can carry either. Either the filter holds the same
  predicate, or the producers stamp `GameEvent.Domain` from the target's domain
  and the filter keys on the tag.

The class is declared in the `type.go` doc comment beside the payload —
`// EventFoo (FooPayload) [bus] ...` — and generated into `eventClasses` in
`internal/event/registry_gen.go`. `event.Replicated(type, domain)` is the
transported set. The generator refuses an unclassified constant.

*Resolved: what `Stamped` actually means.* It is not "the ambient domain at
push". `EventCombatAttackDirectRequest` forces the distinction: the same
producer, in the same tick, under the same ambient domain, pushes a hit that
crosses when the target is shared and does not when the target is player. The
class is a function of the payload, not of the producer, so no static per-type
table can carry it. Combat producers now stamp from the target's own domain at
all four push sites, and the filter reads the tag — the cheaper of the two
options, and the one D-10 already had a mechanism for.

*The tag is only information where a producer set it.* `core.DomainShared` is
the zero value and the ambient domain defaults to it, so a bare `PushEvent`
leaves a record reading "shared" whatever produced it. `Shared`, `Bus` and
`Local` are therefore declarations, checked statically against the pushing
system's profile (`TestEventClassMatchesSystemProfile`); only `Stamped` is read
from the tag, and `TestStampedEventsAreExplicitlyStamped` rejects a `Stamped`
declaration no producer resolves.

**D-11 Determinism invariants.** Across instances: identical shared event
order, identical shared entity creation order, identical shared RNG derivation,
identical shared component values except where D-13 applies. Verified by
comparing `App.SnapshotShared()` between instances
(`TestSharedSnapshotParityAcrossTerminalSizes`) and by stripping player records
from two journals and asserting equality.

**D-12 Claimed geometry.** A shared system that *claims* cells — spawn
footprint clear, composite sweep-over, wall push-out — enumerates both domains
and acts on every occupant. Not a D-1 violation: the shared outcome is a
function of the cell set and protection masks alone, so it is identical on
every instance; player victims differ per instance and are player-domain
effects by D-6. The constraint is on *emission*: victims leave as one death
batch per domain (`internal/system/sweep.go`, `cellSweep`), so a shared record
never names a player entity. The cross-domain reads this needs are exempted
one at a time in `allowedDomainAccess`, and `TestAllowedDomainAccessIsLive`
fails an exemption that outlives the access it excuses.

**D-13 Owner-authored shared state.** A shared entity may carry components
written by exactly one instance and replicated as values rather than
re-derived. The complete list: cursor gameplay components (energy, heat, boost,
shield, weapon, combat), `CursorComponent.Control`/`PeerID`, and
`CursorViewComponent`/`PingComponent`/`PulseComponent`, which are pure local
view. D-11 is refined: shared entity *identity* and *creation order* are
identical on every instance; shared component *values* are either re-derived
identically or owner-authored and transported — never both. Owner-authored
state must not appear in a cross-instance digest, and the metric keys mirroring
it are excluded by `denySharedPrefix` in `internal/app/snapshot.go`.

The static check keys on store name, so it covers only the cursor-exclusive
half: `ownerAuthoredStores` in `internal/system/domain_test.go` lists Energy,
Heat, Boost, Weapon, CursorView, Ping and Pulse. `Shield` and `Combat` are
excluded deliberately — they also carry quasar, loot and species state, which
is re-derived, and the store name alone cannot separate the two populations.

**D-14 Map bounds authority.** `MapWidth`, `MapHeight` and `CropOnResize` are
shared simulation state with two writers:

- `World.SetupLevel`, driven by `EventLevelSetup` from the map script. Shared
  and replicated; this is the authority.
- `GameContext.HandleResizeLocked`, driven by this instance's terminal, and
  admissible only while `mapSizeLocal()` holds. *When more than one player is
  present, crop is disabled and map size is locked.*

The join race is accepted: a resize already in flight when the second
participant appears may land, a resize after it will not. The window is one
event dispatch and the divergence is bounded by the guard immediately after.
Suppression publishes `context.map_locked` and logs once per resize. Both
branches are covered: `TestMapSizeLockedWithSecondCursor` and
`TestMapSizeCropsWithOneCursor` as its negative control.

Consequence, instrumented rather than closed: a map script may branch an FSM
guard on `viewport_width`, `viewport_height`, `camera_x`, `camera_y` or
`color_mode`, which are per-instance, and under a locked map those take a
different arm on each instance. The whole script-visible surface is
`internal/engine/config_access.go` — eight keys, of which `map_width`,
`map_height` and `crop_on_resize` are replicated and the other five are not.
Both accessors warn once per key when a non-replicated one is read while
`World.MapSizeLocal()` is false. The keys are retained: D-14 keeps the surface
and the warning only marks where a script has made itself instance-dependent.

**D-15 Declared classification.** Every system declares its domain profile
(shared, player, dual) and its dependencies (required, optional) as data in
`internal/manifest/definition.go`. That file is the sole declaration site:
`System` carries no `Domain()` or `Requires()` method, `World.AddSystem` takes
a `manifest.ProfileFor(name)`, and `TestSystemsDeclareNoDomainMethod` fails a
system that reintroduces either. The generator emits
`internal/manifest/build_gen.go` and `internal/engine/component_domain_gen.go`;
the boundary suite asserts the code matches the declaration, and the FSM's
`enabled_systems`/`disabled_systems` validation rejects a map that disables a
required dependency. Filename lists are not a classification mechanism and are
not maintained — the one unattributed set (`blast.go`, `interaction.go`,
`sweep.go`, `targeting.go`, `telemetry.go`) is pinned by
`TestHelperFilesArePinned` so a new helper is visible rather than silently
unattributed. Dependency order is initialization and requirement order,
resolved topologically by the shared resolver in `internal/core`; it is
distinct from `System.Priority()`, which orders `Update()` within a tick, and
the two are permitted to correlate without being conflated.

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
| **Shared** | cursor, quasar, swarm, storm, snake, eye, pylon, tower, gateway, wall, nugget, gold, marker, explosion centers, FSM, time |
| **Player** | glyph, dust, drain, decay, blossom, bullet, missile, orb, lightning, flash, fadeout, splash, motion marker, loot |
| **Stamped** | cleaner (nugget-spawned shared, weapon-spawned player), materialize (shared when it gates a shared spawn, player for drain), spirit (shared unless the requester is player-domain, which today is always the fuse) |

Cursor components split three ways: shared-and-replicated (position),
owner-authored (energy, heat, boost, shield, weapon, combat — D-13), and pure
local view (`CursorViewComponent`, `PingComponent`, `PulseComponent`).

`TransientResource` holds explosion centers and stays shared: they *are* the
crossing artifact. `ViewResource` (grayout, strobe) is player-domain.

**Glyph.** Content glyphs are player-domain: the corpus and the map are the
only inputs, so every instance derives the same text from its own player
counter and types against its own copy. Gold sequence members are the only
shared entities carrying `GlyphComponent`, which is why the bit stays unlisted
in `ComponentDef` — it attaches in either domain and the audit cannot key on
it. `TestSharedGlyphsAreGoldMembersOnly` asserts the split. Player-domain
mechanics that iterate glyphs — dust conversion, cleaner sweep, decay, blossom,
splash, typing, drain — guard by `e.Domain() != core.DomainPlayer`. One invariant
stated at the loop, replacing three accidental mechanisms (protection masks,
component absence, iteration order) that happened to hold.

**Contested objectives.** Nugget and gold are shared entities that any
participant may claim, and the claim itself is a shared outcome every instance
agrees on: `NuggetSystem.collectionCursor` and `GoldSystem.handleJumpRequest`
resolve over the whole roster and their sequence state is shared. Only the
*reward* is owner-authored (D-13). Credit is a deterministic function of the
shared event stream: `GoldSystem` tallies `EventCompositeMemberDestroyed` per
roster slot and `GoldCompletionPayload.Entity` names the cursor that typed the
most members, ties breaking to the lowest slot, zero on timeout or destruction.
This is the deliberate opposite of loot, which is rolled and owned per
participant (D-6) precisely because its drop table reads per-cursor inventory.
A mechanic is contested when the outcome is a function of shared state alone;
personal when it reads owner-authored state.

**Glyph.** Content glyphs are player-domain — every instance derives the same
corpus from the same seed, so a glyph is re-derived rather than replicated, and
`GlyphSystem` carries a `player` profile. The exception is a gold sequence
member, which is a shared composite member that happens to carry
`GlyphComponent`. `GlyphBit` therefore stays unlisted in `manifest.Components`:
the bit legitimately attaches in either domain, and no static rule can separate
the two populations. The mechanism is a guard, not a mask — player-domain
mechanics that sweep glyphs (dust conversion, cleaner, decay, blossom, splash,
drain, typing) skip `e.Domain() != core.DomainPlayer`, one invariant replacing
three accidental protections. `TestSharedGlyphsAreGoldMembersOnly` asserts the
other direction: every shared-domain glyph is a gold composite member.

## 5. System classification

Per D-15 the declarations live in `internal/manifest/definition.go` and are
generated into `manifest.systemProfiles`. The list is not restated here; read
`Systems` and `ContextSystems` in that file, where each entry carries a
one-line rationale for its profile. What the document owns is the invariants
the list must satisfy:

- A `dual` profile means the system resolves the domain per request or per
  target (D-7, D-8) — not that it writes both domains indiscriminately.
- A `shared` profile that reads a player store needs an `allowedDomainAccess`
  exemption naming the D-12 site that justifies it.
- A `player` profile may write the D-13 owner-authored set; a `shared` profile
  may not, except `cursor`, which creates the entity and writes constants that
  shared creation order already carries.

`ContextSystems` holds the systems `App` registers directly because they take a
`GameContext` rather than a `World`; `meta` is the only member. Its profile is
`shared`: its world writes are replicated or are the D-14 map-bounds writer,
and the context state it writes is not world state.

`internal/system/network.go` declares no profile because `NetworkSystem` is
written but registered nowhere; `TODO(phase7)` marks it, and
`TestSystemDomainProfiles` exempts it by name.

## 6. Telemetry and snapshots

`status.GroupGate`: `GateAlways`, `GateSentinel` (gated on a roster slot's
entity cell), `GateActivity` (any non-zero member). Declared by prefix in
`activityGatedGroups`; honoured by `VisibleViews`, `Snapshot` and the flight
recorder. Add new wide-but-usually-silent groups by prefix, not by
special-casing a consumer.

Three snapshot views over one reading:

| view | drops | used by |
|---|---|---|
| `Snapshot` | nothing | `:d save`, perturbation test |
| `SnapshotSimulation` | operator surface (`denySim`, session record) | replay vs. source run |
| `SnapshotShared` | owner-authored state (`denySharedPrefix`, `denySharedField`, view and session records, local digest scope) | cross-instance comparison |

`denySharedPrefix` covers, besides the per-slot `player.` group and the
per-system effect groups: `context.screen_`, `context.camera_` and
`context.mode`, which mirror fields the `view` record already drops; and
`event.` and `spatial.`, which are instance-local traffic and index counts.
`denySharedField` drops `created_local`/`destroyed_local` from the otherwise
shared `world` record.

`SnapshotContext` emits five records: `context`, `world` and `player` are
emitted into the shared view, `view` and `session` are dropped from it.
`worldDigestScopedLocked` takes a `DomainScope`, so the shared digest excludes
player entities.

The `player` record is misplaced. It carries `count`, which is the shared
roster size, alongside `entity`, `slot`, `x` and `y`, which are this instance's
binding to its own cursor. Parity holds today only because both instances in
`TestSharedSnapshotParityAcrossTerminalSizes` bind slot 0. The fix is a split:
`count` stays in a shared record, the binding moves to `view`.

## 7. Known gaps

- `event.EmitDeath` writes the queue directly, bypassing `PushEvent`, so
  `WithDomain` does not reach death records. Batches are already domain-pure,
  which is what determinism needs. `TODO(phase6)` in `sweep.go` and `fuse.go`.
- Every system-side producer of the owner-authored grant family
  (`EventEnergyAddRequest`, `EventHeatAddRequest`, `EventShieldDrainRequest`,
  `EventWeaponAddRequest`, and the storm and bullet drains) already pushes
  through `PushLocal`. The remaining producers are the operator commands in
  `internal/mode/commands.go`, which push with the ambient shared tag under
  `OriginCommand` and are therefore journaled: retagging them changes recorded
  record domains, so it belongs in the Phase 6 batch with the journal filter.
- `spatial.indexed_shared` is genuinely comparable across instances and is
  dropped with the rest of the `spatial.` prefix. It wants an allow-list, not a
  prefix deny; the shared position digest covers it meanwhile.
- The `ctx|player` snapshot record carries the local binding — see §6.
- `World.UpdateBoundsRadius` writes `PingComponent` for every rostered cursor
  including remote ones. Harmless under D-13, since ping is pure local view and
  reaches no digest; restricting it to the local slot forces `setLocal` to
  clear the departing slot.
- `uint32(entity)` narrowing at `gateway.go` and `adaptation.go` is safe only
  while route-graph anchors are shared (tag 0).
- `internal/journal` has zero test coverage. Its one non-test importer is
  `internal/app/play.go`, so a `DomainNames` or field-name change breaks
  `vif play` with nothing to catch it.
- A recording wider than the terminal is clipped by the render buffer. The pan
  offset in `play.go` is the seam a windowed composite replaces. Deferred; it
  is a presentation problem with no shared-state component.
- Journal schema is 6. Records already carry `Domain`, written by `vlogSink`
  and parsed by `internal/journal/read.go`, so the Phase 6 filter needs a bump
  only if it adds a field.
