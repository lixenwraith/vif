# Multi-instance domain model — vi-fighter

Rules D-1..D-24 describe how one world is split between state every instance holds
and state that belongs to one participant, and how the two are kept in agreement.
All twenty-four are implemented and verified; §8 maps each to the test that pins
it, and §9 records current limits and deferred work.

**This document describes the code as it stands.** Phase 6 strategy and scope are in
[Multiplayer enhancement plan](multi-player-enhancement.md). The 2026-08-30
divergence and the option survey behind that decision are in
[Desynchronisation and recovery](desync.md).

Terminology is orthogonal by design: a **network peer** is a transport endpoint; a
**game host** is the Shared-domain authority and current protocol coordinator; a
**game guest** predicts that Shared world; and **Shared** versus **Player** names
simulation ownership, not topology or network role. Relay peers and richer
player-domain groupings remain compatible future directions, not current features.

## 1. Domains

Two per `World`: **Shared**, authoritative on the host and predicted then
periodically corrected on guests, and **Player**, this instance's participant and
never replicated. One `World` per local participant. The roster slot lives on
`CursorComponent`; it is not part of the domain tag.

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
smallest artifact that determines the shared outcome crosses as a Bus event.

Its *destination* changed with Phase 4. A crossing used to be a fact every instance
applied at one agreed tick, which meant the producer waited for its own action
exactly as long as everyone else did. It is a **request to the authority** now: the
producer applies it in the tick it produced it for, sends it, and the host applies
it in its own order — and where the two disagree the next correction repairs the
producer, never the host. The playout lead survives only on the receiving side,
where it is an interpolation buffer that lets a remote participant's artifacts
arrive out of order and be applied in one. The three artifacts that create or
destroy a shared entity are the exception and keep the agreed apply tick on every
instance including their producer; D-11 says why.

The artifacts themselves are unchanged:

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
The producing peer publishes that ordinary crossing immediately, so
`CompositeSystem` destroys the typed member during the same settle and the glyph
is no longer renderable before another tick. Remote peers still apply their copy
on the receive-side schedule; `TestTypedGoldMembersDisappearWithoutATick` pins the
local visual requirement.

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

`Stream` records the generator it issues under its domain and label, which is
what makes the streams *enumerable* for D-19: the inventory comes from the one
factory they all pass through rather than from a list someone maintains. Every
call still issues a fresh generator, because a system re-running `Init` after a
reset must start the new session's sequence rather than resume the finished
game's position; the map holds the live pointer each system kept, so
`LoadStreams` moves the generator the simulation actually draws from.
`vmath.FastRand.SetState` is what lets a stream resume at a recorded position —
a seed reproduces a sequence from its beginning, and only a position reproduces
it from where a run had reached.

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
identical shared entity creation order, identical shared RNG derivation, and
identical shared component values — **on the host**. On a guest, equal to the host
as of the last applied correction, and converging.

The weakening is Phase 4's and it is the point of Phase 4 rather than a concession
to it. Bit-exact agreement at every tick was only ever achievable by making every
instance wait: a crossing applied at one agreed tick everywhere, which charged the
playout lead to the participant that produced it and made a lost artifact permanent,
because nothing re-derives one. Under an authority a guest applies its own artifact
in the tick it produced it for and extrapolates until the host tells it otherwise,
so it differs from the host between corrections *by construction* — and the
difference is repaired on a cadence rather than mourned. What survives unchanged is
the first two clauses: shared entity **identity** and **creation order** are still
identical everywhere, because a capture references entities by id and a correction
that had to repair identity would be repairing the thing it is written in terms of.
That is why the three artifacts which create or destroy a shared entity — an
arrival, a departure, a reset — still apply at one agreed tick on their producer as
well (`barrierBound` in `internal/system/network.go`).

Verified in three places, and the three are different claims. Bit-exactness across
instances is now a *test* invariant for the host's own reproduction: two journals
stripped of player records must be equal, and a replay must reproduce its run.
Convergence is the live invariant — `TestGuestConvergesOnEveryCorrection` and the
two-participant criteria assert that a guest's `SnapshotShared()` equals the host's
as of every correction, and that the two really did disagree in between, since a
criterion a guest could pass by never predicting anything proves nothing. And the
distance in between is measured rather than asserted: `snapshot.correction_entries`,
`snapshot.correction_entities` and `snapshot.correction_cells`.

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
identically or owner-authored and transported — never both.

Phase 4 generalised the shape rather than the list. "The owner applies immediately,
the host arbitrates, everyone else receives" stopped being this rule's special
exception and became the ordinary case for every crossing: a producer applies its
own artifact at once and sends it, the host applies it in its own order, and its
result is what the next correction carries. What still distinguishes the components
above is that they are *transported* rather than arbitrated — no instance but their
owner ever computes them, so a correction does not carry them and a capture that did
would make a receiver adopt the sender's answer to which cursor it drives. Owner-authored state
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
Shield geometry and ember state reproduce the remote cursor's presentation
and owner-local interactions; no shared outcome reads the periodic snapshot.

Two things about the set are stronger than "it travels on its own stream", and
both were written from the same defect.

*No component of a shared entity may name a player-domain one.* This used to read
as a rule about the transport — `CursorViewComponent.Orbs` indexed the local
cursor's weapon orbs by weapon type, `readCursorState` left the array out because
it names player entities (D-4), and that was taken to be the whole of it. It was
not. A shared *capture* copies whole components, so the array travelled in every
correction: the host does not simulate a guest's weapons, its copy of that cursor's
array is zero, and each correction wrote those zeroes over the guest's live
handles. `ensureOrbs` then read a zero as "this weapon has no orb" and created
another — while the entities the zeroes had named stayed in the `Orb` store,
protected from decay, no longer advanced by `updateOrbs`, and still drawn by a
renderer that iterates the store. Three orbs per correction, permanently rendered
and permanently frozen, until the player-domain per-cell limit began rejecting
them. The index is `WeaponSystem`'s now and is derived from the `Orb` store, which
already names each orb's owner and weapon type; the component is values only, and
`TestCaptureNamesNoPlayerDomainEntity` walks a capture for a `core.Entity` naming
any player-domain entity rather than trusting one store's exclusion.

*A receiver keeps its own.* A capture carries the owner-authored components
because a joiner has to materialise a cursor it has never held. But for a cursor
the *receiver* authors, what the capture holds is the sender's mirror of a stream
the sender does not write — a sync period behind at best — so an install that
adopted it made every correction roll the receiver's own energy, heat, shield and
loadout back to whatever the host had last heard. `snapshot_roster.go` reads the
set before the stores are replaced and writes it back for the cursors this
instance still authors once the control assignment has been re-derived, which is
the same seam and the same rule as the control assignment itself. It applies
inside a session only: outside one there is no second author, the capture is this
instance's own world, and an install must mean the same thing whoever is watching.

Shield/species collision used to contradict that last sentence: quasar, swarm,
storm, eye, pylon and snake all re-derived shared knockback from whichever shield
snapshot they held. Each now admits only `SimulatesLocally` cursors and crosses
`EventCombatAttackAreaCrossingRequest` with the exact shared target/member set.

**D-14 Map bounds authority.** `MapWidth`, `MapHeight` and `CropOnResize` are
shared simulation state. Every writer of them must therefore be a function of
state every participant agrees on — and, because a run is reproduced by replaying
its record stream, of state a *reproduction* agrees on too. That second half is
what the rule turns on: a replay holds no transport and no
terminal of their own.

Writers:

- `World.SetupLevel`, driven by `EventLevelSetup` from the map script. Shared and
  replicated; this is the authority.
- `GameContext.HandleResizeLocked`, driven by this instance's terminal, and
  admissible only while `World.MapSizeLocal()` holds. Under a locked map it reflows
  the viewport and re-anchors the camera and nothing else: it announces no cursor
  move, because a same-cell `EventCursorMoved` is a shared event and dirties shared
  state (see D-17).
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

**D-17 Throttled derivations.** A shared derivation that is *cached with a
throttle* makes the cache's phase shared state, not an implementation detail. The
value is a pure function of shared inputs, but *when* it is recomputed is a
function of the dirty history, and between two recomputes a consumer reads a field
of some age. Two instances whose throttles are out of phase therefore read fields
of different ages from identical inputs.

`navigation.FlowFieldCache` is the instance: `MarkDirty` latches, `Update`
recomputes only once `MinTicksBetweenCompute` has elapsed, and a recompute resets
the counter. So every producer of a dirty mark must be shared. They are —
`EventCursorMoved`, `EventCursorDespawned`, the wall lifecycle and
`EventLevelSetup` — and two local view changes were announcing the first of them.
A resize reconciled every cursor even when the map had not moved, and
`CursorSystem.move` announces unconditionally. And `setLocal`, which binds *this*
participant's slot, re-announced the cursor's position so the camera would
re-anchor — so every participant but slot zero advanced its own flow-field phase
at session start, which is why the resulting divergence looked intermittent and
unrelated to anything a player did. Both now re-anchor the view directly and
announce nothing. The symptom in both cases begins in kinetics, not in position,
so nothing looks wrong for a long time.

`nav.recomputes` and `nav.roi_cells` are compared for exactly this reason. They
count recomputes, so they are the direct statement that two instances' throttles
agree, and they were the signal that named this defect.

**D-18 Predicted local state.** A value this participant's own input determines is
applied locally at once. Only player-domain producers and the view read the
prediction; it emits no event and enters no snapshot record outside `view`. An
authoritative value the prediction did not produce replaces it — the prediction is
discarded, never merged.

The instance is the local cursor's cell, and the reason is D-3. A cursor placement
crosses as `EventCursorMoveRequest`, so in a live session `NetworkSystem.Cross`
takes ownership of it and the shared store does not move until its apply tick a
playout lead later. Every producer of the *next* placement resolves from that
store, so a session collapsed everything a player issued between two ticks onto one
stale cell: measured, four of five rapid motions were lost, and five of six correct
keystrokes were scored as typing errors because they resolved against a glyph the
first keystroke had already consumed. Shortening the lead does not help — leads of
3, 2 and 1 lose identically — because the defect is the stale read, not the delay.

The prediction is a bounded queue in `PlayerResource`, oldest first: the cells this
instance has requested and not yet seen announced. `World.PushCursorMove` is its
only producer — it advances the prediction and pushes the crossing in one statement,
so a local placement cannot leave without the answer to it — and there are three
call sites: `mode.OpJump`, `TypingSystem.moveCursorRight` and the nugget jump, the
last two being the D-3 table's two cursor-move rows. `CursorSystem.move` reconciles
through `World.ReconcileLocalCursor`: the applied cell matching the oldest
outstanding prediction pops it, and anything else — a level setup, a wall push-out,
a reset — clears the queue and snaps. A cursor `SimulatesLocally` rejects is never
predicted (D-2); rebinding or retiring the local cursor drops the queue with it; and
a queue that fills is a queue nothing is reconciling, so it is dropped rather than
carried.

`World.LocalCursor()` is the seam, and it was consolidated to one accessor for
exactly this: every input, camera, splash and render site behind it answers with the
prediction unchanged. Three kinds of site did need work. Four copies of the same
read that the consolidation had missed — dust spawn, dust attraction and the motion
marker — now go through it. `World.CursorCell` is the per-cursor form, for producers
scoped to a cursor entity rather than to "the local one", and the shield, ember and
ping rasterizers read it so the ring stays centred on the cursor glyph instead of
trailing it by a playout lead. And `PingAbsoluteBoundsOf` follows it, because every
motion measures its step from those bounds: bounds a lead behind the cursor
accelerate a run of keypresses away from the player, which is how the seam announces
itself if it is left half-installed.

Nothing shared reads it. `TestSystemDomainProfiles` fails a shared-profile system
that calls any of the accessors above (`predictedLocalReads`), which is the same
construction that makes D-13's owner-authored stores mechanical. The shared digest
therefore cannot move: `TestTwoLiveParticipantsConverge*` is untouched by the
rule and is the proof it stayed inside the player domain.

The prediction is private state and no record carries it, so a *reproduction*
derives it from the artifact its producer emitted: `World.PushRecord` predicts a
player-stamped `EventCursorMoveRequest` naming the local cursor, exactly as
`PushCursorMove` did in the run. Without that, a replay would resolve every
player-domain effect keyed to the local cursor against a cell the run never showed
— the dust conversion is the one that finds it first.

**D-19 Restorable shared state.** Every value that can change a future shared
outcome is either a component in a shared entity's store, or declared by its
owning system in `internal/manifest/definition.go` as `Snapshot: "state"` and
carried through `engine.SharedStateSaver`, or provably re-derivable from those
**by** the install.

The last clause used to read "at install time", which is weaker than it looks and
was wrong in exactly the way that matters. Derived state left to the tick *after*
an install is not re-derived by the install: it is re-derived by whatever
condition the next tick finds, from the inputs that tick has, and the derivation
usually overwrites the very phase the carrier just restored. Phase 3 found both
halves of that in `navigation`. The composite passability grid was still the one
computed from the walls the install had replaced, and the flow field was left
underived — so the first tick took `FlowFieldCache.Update`'s `!Field.Valid`
branch, derived from *that* tick's targets rather than the ones the restored phase
belonged to, and zeroed `TicksSinceCompute` on the way. The carrier preserved a
phase that the next tick destroyed, and the 500-tick gate could not see it because
`nav.recomputes` is a per-tick gauge and the gate compared every fifty.
`LoadShared` derives both now, from `LastTargets`, which is also what makes the
installed field the one the sender held.

There was a third of the same kind and it took Phase 4's gateway scenario to find
it. The gateway route graphs are derived too, so no capture carries one — but until
that scenario existed nothing derived them either, so an install left the receiver
holding the graphs *its own* run had built: aimed at cells the sender's were not,
present for gateways the sender has none for, and named by route indices the
installed `NavigationComponent`s carry. `LoadShared` clears the resource and rebuilds
every graph the capture named now, from the source and target cells it named — the
same reason `LastTargets` travels, one layer up. Re-deriving from *this* instance's
current targets would not do: the graph a gateway holds belongs to the cell it was
computed for, not to wherever the target has since gone.

The declaration is checked, not trusted.
`TestSnapshotDeclarationsMatchImplementations` fails in **both** directions: a
system declaring `state` that does not implement the interface, and — the one
that matters — a system that implements it without declaring, whose state would
then reach no capture while looking implemented. `TestSnapshotCarriersAreSharedOrDual`
refuses a player-profile carrier, because a capture describes the shared world
and nothing else (D-1, D-6). This is the same construction that made D-15's
domain profiles mechanical rather than reviewed.

Declared carriers, and why each holds state a store cannot:

| System | What it carries |
|---|---|
| `wall` | the maze generator's position — a `math/rand/v2` source, the one simulation stream that is not a `vmath.FastRand` and so is not in `RandResource`'s inventory |
| `adaptation` | EXP3 route weights, the pre-sampled pool, the consumer head and its fallback rotation, which decide the route a spawned eye takes |
| `genetic` | each species' complete streaming checkpoint (PCG position, archive, queued proposals, pending evaluations/IDs and generation phase), registry scout PCG/counter, live fitness accumulators and pending deaths, plus the telemetry throttle/running type average |
| `navigation` | D-17's recompute phase, `LastTargets`, whether a field has been derived at all, the gateway route-rebuild budget, and the two cells each gateway route graph was computed between; the fields, the passability grid and the route graphs are re-derived *by* `LoadShared`, from those inputs |
| `gold` | sequence liveness, its header entity, both deadlines and per-slot contribution |

The genetic row is intentionally stronger than archive persistence. A seed can
reproduce a PCG stream from its beginning, but it cannot say how far the engine
has advanced; an archive cannot account for offspring already generated from an
older archive or evaluations already attached to live entities. Snapshot schema 2
therefore uses `StreamingEngine.Checkpoint`/`Restore`, while the generic
`Snapshot`/`Inject` pair remains the lighter archive-only persistence contract.
The root `pkg/genetic` implementation uses `math/rand/v2` and the standard library
only. Deterministic queue refill is its default; a wall-clock refill budget is an
explicit opt-in because it cannot promise one seeded output stream.

Beside the declared carriers a capture also holds what belongs to no system:
every RNG stream's position (`RandResource.SaveStreams`, enumerable because the
streams come from one factory rather than from a maintained list), the FSM's
runtime position (`fsm.Machine.Export` — which state each region stands in and
how long it has stood there), the shared component stores, the allocator's next
ID and lifetime counters, and the compared status surface.

*The status surface is in a capture for a different reason from everything else.*
Cumulative species counters — swarms spawned, physics steps taken — affect no
future outcome, which is precisely why no system declares them under this rule.
But D-11 requires two instances to agree on them, so a joiner arriving with its
own totals reads as divergent on the compared surface from its first tick and
never converges. A capture therefore reproduces the *surface* as well as the
state, filtered through the same `sharedKey` predicate `snapshotShared` compares
through.

Durations are written relative to the capture's tick, never as absolute instants.
Since D-21 made the simulation clock tick-derived the two forms agree, but the
relative one stays correct if a capture is rebased.

A shared *component* still carries absolute instants — a genotype's spawn time, a
quasar's last speed step, a shield's last drain — and a capture carries them as
they stand. That is sound for one reason and it is worth naming: `engine.SimEpoch`
is a build constant, so tick N is the same instant in every process of the same
build and there is no per-process origin left for a transfer to get wrong.
`TestSimulationEpochIsSessionIdentity` breaks it deliberately — a receiver whose
epoch differs installs the same bytes and diverges — which puts SimEpoch in
session identity beside the seed.

Three things an install does not adopt, and all three are D-13 rather than D-19.
The slot→entity roster mirrors the cursor store and nothing updated it, so after
the shared entities were replaced it still named the destroyed ones. A cursor's
*control assignment* travels inside a shared component: a capture carries the
sender's answer to which cursor it drives, and a receiver that adopted it would
start simulating the sender's cursor and stop simulating its own. Both are rebuilt
from the installed store by `rebindCursorRosterLocked`, keyed by the participant
identity the handshake assigned. The third is the owner-authored set on the cursors
the receiver itself authors: it is not re-derived but *kept*, read before the write
and put back after it by the same function, because the capture's copy of it is the
sender's mirror of a stream the sender does not write. D-13 says why.

**D-20 A shared region is steered only by replicated events.** Every FSM region
is shared state and `fsm.<region>` is compared across the session, so a region
can stay in agreement only if every event that moves it is one every instance
holds. A `ClassLocal` trigger is not: it never replicates, so the region advances
on the one instance whose participant produced it and nowhere else, and nothing
re-derives a missing local event.

`TestFSMTriggersAreReplicated` enforces this over every config tree a build can
boot, the embedded copy included, naming the state and the class it found.

The rule was written from a defect. `MonitorActive` transitioned on
`EventHeatBurst`, which `HeatSystem` pushes with `PushLocal` for the cursor that
overheated; in the 2026-08-31 session that fired at tick 1903 and the session was
marked `DIVERGED` at 1934. The state name recovered a tick later, which is why
the transition looked harmless — what stayed apart was the region's *elapsed
time*, measured from a re-entry only one instance made. The sweep it wanted is a
per-instance effect (D-6) and `HeatSystem` emits it directly now.

**D-21 The simulation instant is a function of the tick.** `engine.SimTime(tick,
interval)` measured from `engine.SimEpoch` is the only value a system may treat
as "now". The pacing clock still decides *when* a tick runs, and `RealTime` still
reports the wall.

Game time was read from each process's `PausableClock`, which projects
`time.Now()`, so two participants read different instants at the same tick:
different origins, and different scheduler jitter on top. Every shared reader
measures a difference against a stored instant — a quasar's speed step, a gold
deadline, adaptation drain ages, genotype ages — and a difference against a
wall-paced clock crosses its threshold on a different *tick* per instance. That
is the 2026-08-31 kinetics divergence: a quasar spawned at tick 712, one instance
stepped its `SpeedMultiplier` a tick before the other, and because the multiplier
compounds the two velocity streams never re-converged.

The rule also makes `DeltaTime` honest. A tick has always advanced the simulation
by exactly `tickInterval`; only the instant stamped beside it drifted.
`time.game_elapsed_ms` is in the compared surface as a result — it is `tick *
interval` now, and comparing it is what pins the clock. Its previous exclusion is
why nothing caught the divergence at its source.

**D-22 An arrival is admitted before the world is read for it.** A participant
joining a running session becomes a peer — receiving this instance's crossings —
*before* the capture it will install is taken, and holds that traffic until the
world it applies to exists. The artifacts a capture already contains are refused
rather than applied a second time: an installed world has applied everything due
at or before its own tick, and `NetworkSystem.AdoptSnapshot` records that tick as
the floor.

The obvious order is the other one, and it loses data silently. Read the world at
tick T, transfer it, then admit the joiner, and every artifact produced between T
and the admission reaches nobody: it is not in the capture, because the capture
predates it, and it is not on the wire, because the participant was not yet a
peer. Nothing detects that. It is the same class of failure as a missing crossing,
which is what this whole plan exists to stop having.

What the ordering costs is a gap in *time* rather than in state: a world read at
tick T is installed some milliseconds later, by which point the session is at T+k.
Left open, k is permanent and every crossing the new participant produces arrives
k ticks late. So the join closes it by simulating those k ticks before its own
clock starts — reading the target from the epochs the session closes, since every
tick closes one — and refuses the join if what remains exceeds the playout lead.
k is a function of world size and link speed, never of how long the session has
been running, which is the property that makes join-anytime possible at all.

Phase 4 found the half of the gap this ordering does not close, and closed it. An
epoch produced *before* the admission — and flushed to the peers this instance had
at that moment — reaches the new participant not at all, and a capture taken at the
admission tick does not contain it either, because its apply tick is still a playout
lead ahead and the floor above does not drop it. So a join asks for a world a lead
further on: by tick A+lead every artifact produced before the admission has applied
into the capture, and the copies that do arrive are recognised as already-contained.
It costs the join three ticks. It is also no longer a world read of its own — the
join takes the publication cadence's most recent keyframe when one is fresh enough,
which is what makes the read per-cadence rather than per-participant.

**D-23 The host's world is the correction.** The host is the sole authority for
shared state. It publishes its world on a cadence — a whole capture, or a delta
against the last whole one — and a guest applies what arrives into a staging world
and swaps it in between two ticks. Where a guest's derivation and the host's
disagree, the host wins, with no negotiation and no acknowledgement; the distance
between them is telemetry.

The capture or correction envelope is a transport detail: JSON remains the schema
and integrity surface, then a versioned deflate envelope declares the plain size
and compresses it before chunking. Decompression enforces the same 64 MiB ceiling
as reassembly. Capture/diff semantics are unchanged, while cadence pricing,
admission and telemetry see actual compressed wire bytes.

Four properties are what the rule is for, and each one is a thing the previous
design could not do:

- **Nothing acknowledges a correction, and nothing retransmits one.** A keyframe
  supersedes everything before it, so a lost delta costs freshness until the next
  keyframe and never correctness. There is no repair path because there is nothing
  to repair. `SnapshotKeyframeCorrections` bounds the wait.
- **A correction cannot fail.** It arrives late, or it names a keyframe this
  instance does not hold, or a fresher one overtakes it before it is applied. Each
  of those is a counter — `snapshot.corrections_refused`,
  `snapshot.corrections_superseded` — because the next correction is self-sufficient.
- **A delta is exact or it is refused.** Applying one to the baseline it names
  reproduces the sender's capture byte for byte, entity order included, and what
  proves it is the capture's own integrity hash rather than a comparison. A delta
  that rebuilt an *equivalent* world — the same entities in a different store
  order — passes every value check and fails that hash, which is why the delta
  carries store order at all.
- **The magnitude is measured, not asserted.** How far a guest's prediction had
  drifted when the authority arrived is read inside the same critical section that
  writes the correction, and it is what a cadence gets chosen from.

Two things the install does that a join's install does not. It reconciles the live
world onto the capture — removing what the authority no longer has, writing what
differs, leaving the rest — so the write is the size of the correction rather than
the size of the world; `TestReconcileMatchesAFullInstall` is what says the two
produce the same world. And it resolves into a staging world that is built once and
re-used, because constructing one costs 9 to 31 ms and a correction happens five
times a second; `TestStagingWorldIsBuiltOnceAndReused` is what says re-use leaves
what a fresh world would.

What the host validates is deliberately narrow, and the boundary is worth stating.
Most of its authority is structural: it applies what reaches it in its own order and
its result is what ships, so a guest cannot make the session believe something — it
can only make *itself* believe it until the next correction. The exception is the
roster, because an arrival and a departure do not describe a shared outcome, they
create and destroy a shared entity at one agreed tick; the coordinator is their only
producer and an artifact of either kind from anyone else is refused
(`network.artifacts_refused`). Everything past that — a participant attributing a
crossing to a cursor it does not drive — is an *authentication* question, and this
plan puts authentication before anything beyond trusted peers.

**D-24 The cadence is a function of the link; the convergence floor is not.** The
host publishes to each peer on a cadence and a
keyframe interval chosen from that link's measured round-trip time, its
variation, the rate bytes are actually arriving at, and how much that participant
has at stake in the next correction. Both are bounded, and their product — the
ticks a participant may go without a whole authoritative world — may never exceed
`SnapshotFloorKeyframeTicks`. A link that cannot carry one whole world per floor
window is refused at admission and reported as an unrecoverable operating
condition mid-session. It is never adapted past in silence.

The rule has four parts and each is a thing the constant it replaced could not do.

*The measurement is a real round trip, and it lives in the transport.* Nothing in
this protocol used to make one: every number a session had was one-directional —
what this instance sent, and how far behind the newest tick it had *heard about*
it stood — so a cadence had a constant and nothing else to be chosen from.
`MsgLinkProbe` and `MsgLinkEcho` are that measurement, and they are answered by
the receiving port before the frame could reach a tick. So what the round trip
measures is the wire rather than how often an instance runs a tick, and — the
part that matters for D-11 — no timing value ever has to enter the world for the
cadence to exist. The world publishes one opaque `LinkReport` (its tick, its
staleness, its last correction magnitude, its cursor cell) and reads back one
estimate it may schedule transport from. Both directions are copies; neither is a
simulation input. `TestLinkMeasurementNeverEntersTheComparedSurface` is what says
the estimate stays out of the compared surface, and the round trip is the only
value in this design that a `shared`-profile system reads at all — through
`engine.LinkMeasuringPort`, whose whole contract is those two copies.

*A delivery rate is not a capacity unless the link was the limit.* The estimator
reports `Saturated` beside its throughput, from a standing backlog or a round trip
inflated past its own baseline, and a controller given an unsaturated rate keeps
its nominal point. Without that distinction every quiet moment reads as a narrow
link and the session throttles itself for having nothing to say.

*Demand is a rise and a comparison, never a count.* An absolute threshold on the
correction magnitude is a threshold about the *world*: measured on the shipped
storm, a correction moves the whole shared population every cadence, so a fixed
magnitude fires permanently and spends the entire uplink on a condition that is
simply what a storm looks like. What is fed to the controller is therefore how far
a participant's magnitude stands above *its own* recent level, and how far its
share of what the correction moves stands above the *session's* mean. The first
says the cadence is falling behind; the second says which of several participants
is served first. A participant with neither settles at a slower cadence, which is
what pays for the other two.

*Relevance moves the schedule and never the content.* A correction is still
computed once against one baseline and is still exact — the whole world, or the
byte-for-byte difference from the last whole one, proved by the capture's own
integrity hash (D-23). Scoping a correction to the entities near a participant
would leave nothing for that hash to be about and would hand a receiver a world
assembled from two ticks; both are Phase 6's to solve if they are worth solving.
So what per-peer scheduling changes is *which* corrections a peer is sent, and a
keyframe goes to every peer whatever their cadence, because a guest that missed
one refuses every delta that follows it.

The floor is the part that is not negotiable, and it binds in three places. The
controller's search space is bounded by it before capacity is consulted, so no
plan it can return violates it. Admission prices the floor against the link the
join's own transfer measured and refuses one that cannot carry it, which is the
same answer the join already gives when its catch-up gap exceeds the playout lead
(D-22). And a receiver measures the guarantee from its own end —
`snapshot.cadence_keyframe_age_ticks` — because the host's estimate that its link
*could* carry a whole world is a different claim from one having arrived.

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

**The receive lead.** The schedule belongs to the *run*, not to whether a link is
attached at this instant. Every crossing is encoded into an epoch with an absolute
apply tick. For an ordinary D-3 request, `WireSink.Cross` keeps only the peer copy
and declines queue ownership, so the producer publishes its original immediately;
the remote copies wait out the fixed lead. That is the Phase 4 local-first rule,
not lockstep: on a guest the early result is provisional, and the host's next
correction wins.

Three `barrierBound` artifacts still transfer queue ownership and wait on their
producer too: participant arrival, participant departure and full reset. They
create or destroy shared identity rather than changing values in an existing
world, so applying them on different ticks would give captures different entity or
run numbers (D-11).

`Flush` closes and asynchronously sends each tick's epoch, including an empty
marker. `Receive` opens the next tick by applying remote and barrier-bound local
artifacts whose deadline has arrived; `settleLocked("wire")` completes that
dedicated between-tick group before `BeginTick`. Due artifacts sort by apply tick,
participant ID and sequence. The default lead is three 50 ms ticks and simulation
never waits for a round trip. A crossing produced by the wire settle belongs to
the production epoch about to run and receives a complete lead of its own.

Outside a session `Cross` declines ownership without retaining a peer copy and the
solo queue/journal path is unchanged. A journaled crossing is stamped where it was
consumed, so replay republishes it directly rather than applying the receive lead a
second time; a re-derived crossing follows the same local/remote rule its source
run did. `network.barrier_*`, `network.local_crossings_now`, peer-lag and late
counters expose this schedule.

**The mesh.** Authority and topology are separate. The coordinator/host owns the
canonical shared world, while a session may still be a graph of links: an instance
sends only to its direct neighbours, so an artifact or authoritative correction
reaches everyone else by being forwarded. Every node floods each epoch it has not seen to
every link except the one it arrived on. What terminates the flood is the
per-source epoch window — a copy arriving by a second path is recognised and
neither applied nor forwarded again — so each node handles each epoch exactly
once whatever the topology; `parameter.NetworkRelayHopLimit` is a backstop, not
the termination argument. A relay preserves `Source`, `ProducedTick` and every
frame's `ApplyTick` and sequence, which is what lets a relayed artifact apply at
the same absolute tick however many links it crossed. Owner-authored state syncs
relay on the same rule, using the per-slot sequence in place of the epoch window;
correction chunks carry the authority's capture tick and are relayed unchanged.

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
graph.

What a mismatch *means* changed with D-11. Before Phase 4 both instances re-derived
the shared world from one artifact stream, so a disagreement meant one of them had
lost an artifact and nothing would ever re-derive it: two consecutive disagreeing
samples raised an amber `DESYNC`, five turned it red `DIVERGED`, and the session was
over. Under an authority none of that holds — a guest applies its own input at once
and extrapolates between corrections, so it is *expected* to differ from the host —
and the escalation was retired with the failure state it described.

The measurement stayed. A mismatch increments `network.digest_mismatches` and names
the surface that moved in `network.drift_part`, with `network.drift_tick`. That is a
gauge: it says how often and where two instances stand apart, which is the same
thing the correction magnitude says from the other side. Nothing escalates, nothing
is logged at error, and no status item reports it. The per-record breakdown is no
longer requested on the wire either — a guest disagrees with the host between every
pair of corrections by design, so asking for detail would mean sending a map of
per-record hashes for the whole session. `sharedDigestLocked` still produces it, for
the tests and for a tool comparing two runs offline.

What replaced the verdict is two numbers with better claims, and they are in the
`snapshot` and `network` groups rather than in a status word:

- **The correction magnitude** — `snapshot.correction_entries`,
  `snapshot.correction_entities` and `snapshot.correction_cells` — is how far this
  instance's prediction had drifted at the moment the authority arrived, measured
  inside the same critical section that writes the correction. It is telemetry, not
  an error, and the status bar shows it as `COR n` only while it is non-zero.
- **The staleness** — `network.lag_ticks` and `network.stale` — is how far behind the
  newest tick any peer has been seen closing this instance stands, taken every tick.
  Past the playout lead this participant's own crossings are reaching the host after
  the ticks they name, which is when a player should be told the link rather than
  the game is the problem: the status bar shows `LAG n`. Phase 3 measured this once,
  at admission, and never again.
- **The operating point** — the `snapshot.cadence` group and `network.link` beside
  it — is what Phase 5 added and is a third kind of claim again. The first two say
  how far apart two instances stand; this says what the session is *doing about
  it*: the cadence in force, the ticks between whole worlds, the measured round
  trip and its variation, what the link was priced at, and which of two conditions
  holds. `cadence_constrained` is the design working — the link narrowed, the
  cadence slowed, prediction is carrying more. `cadence_floor_breached` is not: no
  schedule the controller may choose delivers a whole world inside the guaranteed
  window. The status bar draws them differently for that reason, `LNK` against
  `LINK!`, and `:session` prints the whole set on request.

`SnapshotShared` remains a comparison surface, not a restorable world checkpoint,
and the digest still deliberately excludes D-13 owner-authored values.

**The link, and the cadence it decides.** The correction cadence was a constant
through Phase 4 and is a bounded controller per peer now (D-24). What changed
underneath it is that the protocol makes a round trip: `MsgLinkProbe` leaves every
`NetworkProbeInterval` and `MsgLinkEcho` answers it inside the receiving port,
before the frame could reach a tick. The echo returns the probe's own timestamp
untouched — so neither end has to agree with the other about what time it is — plus
the bytes that end has received on the link, and the opaque `LinkReport` its world
last published.

Two counters and one timestamp are all the estimator needs. The round trip and its
variation come from the timestamp; the delivery rate from two consecutive
echoes' byte counts; and the *backlog* — what this end has queued against what the
far end says arrived — from the difference, which is what separates a fast link
from an idle sender. A rate measured while nothing was queued is a lower bound on
capacity and not a measurement of it, so the estimator reports `Saturated` beside
it and a controller given an unsaturated rate keeps its nominal point.

The two byte counters have no shared origin: this end starts counting when it
accepted the stream and the far end when its port took the stream over, which for
a mid-run join are separated by a whole capture. The meter therefore measures
*growth* from a re-based origin rather than an absolute difference — without it a
join would leave a standing backlog the size of the world it installed and the
link would read as permanently saturated for the rest of the session.

What the controller decides is a cadence in ticks and a keyframe interval in
corrections, per peer, inside `SnapshotCadenceMinTicks`/`MaxTicks` and
`SnapshotKeyframeMinCorrections`/`MaxCorrections`, and never past the convergence
floor. Under pressure the keyframe interval stretches before the cadence slows,
because a keyframe costs several times a delta on this world and stretching it
spends recovery time the floor already bounds rather than freshness a player sees.
Degradation is immediate and recovery is stepped: a link that has narrowed has
already narrowed, and one sample taken during a quiet moment is not evidence that
it came back.

The session composes the per-peer plans into one publication timeline. The base
cadence is the fastest peer's; the keyframe period is the *longest* any peer
planned, capped by the floor — longest rather than shortest because every peer has
to hold the keyframe a delta names, so the session pays the cheapest whole-world
period that still honours everyone's floor. A peer receives a delta when its own
cadence says it is due and a keyframe always. In a relayed topology a participant
reached through a neighbour rides that neighbour's schedule, because the flood
forwards what the neighbour was sent; per-peer cadence is a property of a direct
link.

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
The observing instance also raises a local status message immediately. In
particular, a game guest losing participant one records `network.host_lost`, says
that it is continuing locally from the last authoritative state, and keeps a
persistent `HOST LOST:LOCAL` status through game resets. No digest could report
that failure after its comparison edge disappeared. This is an independent local
fork, not coordinated authority migration; other guests may continue differently.

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

**Join and reconnect.** A participant may arrive at tick zero or during a running
session. A solo instance can open the session with `:host <addr>`. The coordinator
admits the connection before reading the world for it (D-22), then sends a chunked
authoritative keyframe; the joiner resolves it in the reusable staging world,
commits between ticks, and catches up only the bounded transfer gap. A reconnect is
the same path at a later capture tick.

No record history is retained for admission. The retired replay-from-zero path had
unbounded session-length cost and no sound tick-phase handoff; sending what the
world *is* makes transfer cost a function of world size. A join normally reuses the
publication cadence's recent keyframe, so simultaneous arrivals share the world
read rather than each stopping the host for another capture.

**The stream.** The real endpoint is `network.SocketPort`. Every message has a
fixed 12-byte header whose final field is payload length; `Decode` uses
`io.ReadFull` for both header and payload, and `Encode` completes short writes.
Transport goroutines append only `network.Inbound` values to the port buffer;
`NetworkSystem` drains that buffer under the world lock, preserving the poll
boundary. Idle peers exchange framed heartbeats; read and write deadlines close a
silent stream without blocking a tick. The resulting disconnect notification is
drained through the same path, raises a local status message, and—while the
coordinator remains reachable—produces the crossing that removes only cursors
owned by that participant.
The steady-state stream carries closed crossing epochs, owner-authored cursor
syncs, shared-state digests and authoritative correction chunks. Membership
notices and the offer/gate/snapshot handshake surround admission; anything else is
counted as a drop rather than translated.

Loss outside the receive lead is counted: `network.transport_lost_in` is inbound
notifications a full poll buffer discarded, `network.transport_lost_out` is
outbound frames a peer's send queue refused. A missing crossing can move a guest's
prediction, but the next authoritative keyframe supersedes it; a new loss is still
logged once because it raises correction magnitude and staleness.

**Admission.** The handshake sends `JoinAnchor` inside `SessionOffer`. The
coordinator allocates a participant ID and roster slot, releasing both on a failed
handshake or departure. Identity is checked before state is accepted: schema, tick
interval, seed/session, config, corpus and D-14 map latch must agree. Canonical
participant IDs, not connection accept order, key epoch ordering and cleanup.

A `-host ... -players N` launch still has a tick-zero start gate. Its closed roster
arrives with the gate rather than the earlier partial offer, so every participant
creates the same cursors in the same order; the clock stays frozen until that gate
so lobby time cannot age simulation deadlines. A joiner dials before constructing
its `App`, allowing `ConfigForJoin` to install the authority's seed, configuration,
corpus identity and map bounds before the FSM boots.

A solo run may also carry `-players N` into a later `:host <addr>`. With no
explicit count, mid-run hosting admits up to `parameter.MaxPlayers`; the startup
`-host` default remains two. A joining guest cannot set the host's lobby cap.

Mid-run admission replaces the closed-roster boot with D-22's snapshot path. The
existing participants keep ticking, the joiner installs the authority's current
world, and `EventParticipantJoined` creates its cursor at one agreed future tick.
Both paths activate the crossing sink before participant input is accepted. A host
remains playable and listening after a disconnect; later connections take a fresh
join snapshot rather than being limited to the original gate.

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

Authoritative state is the larger stream and has its own measurements. At the
storm high water the schema is about 176 KiB for a keyframe and 29 KiB for a delta;
the bounded deflate envelope reduces those to about 15.4 KiB and 7.1 KiB. With one
keyframe per ten corrections that is about **39.6 KiB/s at 5 Hz** and 15.8 KiB/s
at 2 Hz, down from 216 and 86 KiB/s. Snapshot schema 2 adds exact genetic
continuation to the opaque `genetic` carrier; delta generation already treats
every carrier record as whole state, so compression needs no carrier special case.

The link measurement adds 12 bytes out and 45 bytes back per peer per
`NetworkProbeInterval` — under half a kilobyte a second at `MaxPlayers`, small
beside even the compressed correction stream. The estimator smooths over
eight samples, so the interval is also how quickly a link change becomes
steerable: about a second and a half.

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
counters, `nav.entities`, `content.served`/`content.rejected`/`content.file` beside
the corpus fingerprint, and `engine.tick_slips`, a missed-deadline count that is
this process's pacing rather than simulation state.

`time.game_elapsed_ms` used to sit beside it on the same argument, and that
argument was wrong in a way that cost a session: it held only while the
simulation instant came from the pacing clock. D-21 derives it from the tick, so
the value is `tick * interval` on every instance, it is compared, and comparing
it is what pins the clock deterministic — `TestSharedSnapshotComparesElapsedGameTime`
fails if a forged value does not move the surface. `gold.timer` is compared too:
the join path no longer replays an FSM one tick ahead, and the gold carrier writes
both deadlines relative to the capture tick. `TestSnapshotJoinCarriesTheGoldDeadline`
pins the mid-sequence case.
The corpus trio is the whole of what a draw writes — the count and the file the
cursor has reached — because content glyphs are player-domain and two participants
who type differently roll onto different files. What remains beside them describes
the corpus rather than a position in it. `nav.recomputes` and `nav.roi_cells`
deliberately stay compared: they count recomputes of a throttled shared derivation,
which is what says the two instances' cache phases agree (D-17). The shared
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

The `snapshot` group is three cards for three questions, split by prefix in
`internal/status/key.go`: `snapshot.correction` is how far this instance's
prediction stood from the authority and how much of the authority arrived,
`snapshot.cadence` is what operating point the link put the session at (D-24), and
what is left describes what one capture cost. `network.link` gained the estimate
the cadence is chosen from — round trip in both milliseconds and microseconds,
because a loopback round trip rounds to zero in the first and reads as "not
measured", jitter, delivery rate, probe loss and whether the link was the limit
while that rate was measured. Every one of them is per-instance transport state
and every one is already dropped from the shared view by the `network.` and
`snapshot.` rules above; `TestLinkMeasurementNeverEntersTheComparedSurface` is
what says so from the other direction, over a shaped link.

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
| `TestCaptureNamesNoPlayerDomainEntity` | `internal/app` | D-4/D-13: a reflective walk of a capture's world finds no `core.Entity` naming a player-domain entity, so a component of a shared entity cannot carry a handle that means nothing on the receiver |
| `TestCorrectionsLeaveOneOrbPerArmedWeapon`, `TestOrbsAreRecoveredFromTheStoreRatherThanDuplicated` | `internal/app`, `internal/system` | D-4/D-2: repeated corrections leave an armed guest exactly one orb per weapon, and the store-derived index recovers an existing orb, drops a duplicate, a remote-owned one and one whose charges are gone |
| `TestCorrectionKeepsTheReceiversOwnCursorState` | `internal/app` | D-13: a correction published with no tick between the grant and the capture cannot carry the guest's loadout, and does not overwrite it |
| `TestBusPayloadsNameOnlySharedEntities` | `internal/app` | D-4 over a soak, via a dispatch tap |
| `TestLocalEventsCarryThePlayerDomain` | `internal/app` | D-10: a Local-class record is tagged player, against a shrinking exemption set |
| `TestDomainAuditSoakClean` | `internal/app` | Zero component-domain violations over a 3,000-step soak |
| `TestMapSizeLockedWithSecondCursor`, `TestMapSizeCropsWithOneCursor` | `internal/app` | D-14, with the crop path as its own negative control |
| `TestJoinerOnAnotherTerminalSharesTheMapFromTickZero` | `internal/app` | D-14/D-11: a participant on a different terminal holds the boot cursor on the session's cell, not its own |
| `TestSessionRunNeverCropsItsMap` | `internal/app` | D-14: a run that opened a session keeps its bounds through a resize, so the anchor it offers cannot move |
| `TestLocalViewChangesLeaveTheFlowFieldPhaseAlone` | `internal/app` | D-17: neither a resize that moves no cursor nor a local rebind announces one, with the cropping resize as the negative control |
| `TestOneKeypressMovesTheLocalCursorWithoutATick`, `TestFiveKeypressesBetweenTicksReachFiveCells`, `TestFastTypingOverAGlyphRunScoresNoErrors` | `internal/app` | D-18: the same probe solo and on the producing instance of a live session must agree — one press before any tick, five cells from five presses, six correct keystrokes and no typing error — while remote peers retain the receive-side lead |
| `TestTypedGoldMembersDisappearWithoutATick` | `internal/app` | D-3/D-18: each correctly typed shared gold member is destroyed and non-renderable on the producing peer before a tick advances |
| `TestPredictedLocalCursorReconcilesAndSnaps` | `internal/app` | D-18: an authoritative placement the prediction did not produce clears the queue instead of merging with it |
| `TestCrossingHelpersPushWhatTheyDeclare` | `internal/system` | D-3/D-18: the helper the D-3 table trusts by name really pushes the crossing it claims |
| `TestParticipantsShareTheCorpusFingerprintNotItsCursor` | `internal/app` | §7: the corpus fingerprint is compared and the cursor's position in it is not, over a multi-file corpus the embedded one cannot express |
| `TestSharedGlyphsAreGoldMembersOnly` | `internal/app` | §4: every shared-domain glyph is a gold composite member |
| `TestSharedSnapshotParityAcrossTerminalSizes` | `internal/app` | D-11: two instances of one seed on different terminal sizes agree at every step |
| `TestObserverSharedStateTracksTheLiveParticipant` | `internal/app` | 1,200 steps of an observer whose shared state arrives over the wire rather than re-derived |
| `TestTwoLiveParticipantsConvergeOnCorrections` | `internal/app` | 1,200 steps, two live participants, both moving, both crossing, both nonzero APM; D-11 as weakened — the guest equals the host as of every correction rather than at every tick |
| `TestTwoLiveParticipantsConvergeOverTCP` | `internal/app` | The same criterion through `127.0.0.1`, plus handshake, roster, framing, a chunked correction off a real socket and clean remote-cursor removal on disconnect |
| `TestChainRelayReachesANonAdjacentParticipant` | `internal/app` | §6: a crossing reaches a participant its producer never linked to, at the same tick; fails without the relay |
| `TestMeshPropagatesEveryParticipantToEveryOther` | `internal/app` | Five participants in 1—2, 2—3, 3—4, 3—5 agree on every shared record through 240 driven steps |
| `TestDepartureReachesTheWholeMesh` | `internal/app` | A departure removes the cursor on an instance that never linked to the departing participant |
| `TestThreeParticipantLobbyClosesOnOneRoster` | `internal/app` | The socket handshake for a lobby larger than a pair: partial offers, one closed roster |
| `TestSnapshotJoinLeavesEachParticipantDrivingItsOwnCursor`, `TestRosterCrossingsKeepTheAgreedApplyTick` | `internal/app` | A joined participant drives its own cursor after adopting the host world; the arrival itself remains barrier-bound and lands at one agreed tick |
| `TestSessionRosterStartsAndRestartsEveryParticipant` | `internal/app` | Every closed-roster cursor receives the boot template at admission and survives the monitor's global reset |
| `TestLiveSessionRefusesAnInstanceLocalPause`, `TestCoordinatorResetCrossesAndPreservesRoster` | `internal/app` | Live operator policy: time cannot stop on one instance; the coordinator serialises a full reset without collapsing membership |
| `TestExplosionPresentationStaysWithItsProducer`, `TestExplosionCombatDoesNotDependOnVisualMergeState` | `internal/app`, `internal/system` | D-3/D-6: smoke remains local while immutable geometry always resolves shared combat |
| `TestRuntimeDigestIsADriftGaugeRatherThanAVerdict`, `TestStatusBarSyncIndicatorReportsStalenessAndCorrection` | `internal/app`, `internal/render/renderer` | D-11/D-23: a deliberate shared corruption is counted and its surface named, nothing escalates, the retired `DESYNC`/`DIVERGED` keys are gone, and a correction closes it; the indicator reports staleness and correction size instead |
| `TestGuestConvergesOnEveryCorrection`, `TestCorrectionMagnitudeIsMeasuredNotAsserted` | `internal/app` | D-11/D-23: a guest that predicted and drifted is exactly equal to the host as of every correction, and the drift it had is published rather than escalated |
| `TestCorrectionDeltaRoundTripsExactly`, `TestCorrectionDeltaRefusesAForeignBaseline`, `TestCorrectionCarriesTheWholeDeclaredSurface` | `internal/app` | D-23: a delta applied to the baseline it names reproduces the sender's capture byte for byte over three seeds and is smaller than it; applied to any other baseline, or with a body its header does not describe, it is refused; every declared carrier, stream and FSM region travels whole in both shapes |
| `TestSnapshotWireEnvelopeRoundTripsAndIsBounded`, `TestCorrectionCostAtTheStormHighWater` | `internal/app` | D-23/D-24: the versioned compressed envelope round-trips, rejects corrupt/version/size violations, and materially reduces both storm keyframe and delta wire bodies |
| `TestStagingWorldIsBuiltOnceAndReused`, `TestReconcileMatchesAFullInstall` | `internal/app` | D-23: two captures installed into one re-used staging world leave what a world built for the second alone would, and a reconciled live world equals a fully re-installed one — then and 60 ticks later |
| `TestLocalCrossingSkipsThePlayoutLead`, `TestRosterCrossingsKeepTheAgreedApplyTick` | `internal/app` | D-3/D-11: a producer's own crossing lands in the tick it produced it for while the peer keeps the lead; an arrival still applies at one agreed tick everywhere and takes the same shared entity on both |
| `TestHostRefusesARosterCrossingFromAnyoneElse` | `internal/app` | D-23: an arrival produced by a participant that is not the coordinator creates no cursor and is counted as refused |
| `TestCoordinatorLossRaisesLocalStatus`, `TestStatusBarHostLossIndicatorPersistsAndOutranksLinkState` | `internal/system`, `internal/render/renderer` | Host loss: the game guest announces local continuation, retains the run-level fact across reset, and renders it ahead of stale link telemetry |
| `TestMidRunHostUsesConfiguredCapOrMaximum` | `internal/app` | A later `:host` inherits an explicit solo `-players` cap and otherwise uses `MaxPlayers` |
| `TestSessionLagIsMeasuredEveryTick` | `internal/app` | D-23: a participant in step reports no lag, and one left behind the session reports it and is flagged stale without anything asking |
| `TestJoinReusesTheCadencesKeyframe`, `TestMidRunJoinWaitsOutThePlayoutLead` | `internal/app` | D-22/D-23: a second join takes the keyframe the first read rather than reading the world again, a keyframe for a tick the session has not reached is refused, and a join waits for a world a playout lead past its admission |
| `TestLinkSmoothsTheRoundTripAndTracksItsMinimum`, `TestLinkJitterRisesWithVariationAndFallsWithout`, `TestLinkReportsSaturationOnlyWhenTheLinkWasTheLimit` | `pkg/linkpace` | D-24: the estimator's three claims, on an explicit clock the caller carries — a smoothed round trip with a baseline an excursion cannot move, jitter that rises and settles, and a delivery rate that is only called capacity when the link, not the sender, was the limit |
| `TestTheFloorIsNeverCrossed`, `FuzzControllerHoldsItsEnvelope` | `pkg/linkpace` | D-24: over a swept and then a fuzzed input space, no plan leaves its declared bounds and none leaves more than `FloorKeyframeTicks` between whole worlds |
| `TestABreachIsReportedRatherThanAdaptedTo`, `TestAdmitRefusesALinkThatCannotCarryTheFloor` | `pkg/linkpace` | D-24: a link below the floor is reported and clamped *at* it rather than published past, and refused at admission — while no measurement at all is admitted rather than refused |
| `TestKeyframesStretchBeforeTheCadenceSlows`, `TestDegradationIsImmediateAndRecoveryIsStepped`, `TestDemandDecidesWhereInsideTheFeasibleRangeAPeerSits` | `pkg/linkpace` | D-24: the degradation order, the asymmetry between narrowing and recovering, and that demand chooses inside the feasible range and never widens it |
| `TestTheMeshMeasuresARoundTripOnItsOwnClock`, `TestTheMeshProbeNeverReachesTheGame`, `TestTheReportReachesTheProbingPeer` | `internal/network` | D-24: the round trip is measured on a clock the harness advances in ticks, a probe and its echo are answered inside the transport and never reach a drain, and the far end's report arrives with it |
| `TestAHealthyLinkStaysAtTheNominalOperatingPoint`, `TestAConstrainedLinkSlowsTheCadenceAndSaysSo`, `TestASlowPeerDoesNotSlowAFastOne` | `internal/app` | D-24: an unshaped link is not adapted away from Phase 4's cadence, a shaped one moves the operating point and reports it, and two participants on one host with different links get different schedules |
| `TestTheFloorBoundsEveryScheduleAShapedLinkProduces`, `TestCorrectionMagnitudeStaysBoundedOnAConstrainedLink`, `TestAGuestRecoversAtTheFloorAfterTheLinkComesBack` | `internal/app` | D-24 over the composition rather than one plan: the session timeline honours the floor under every shape, the magnitude rises and stays bounded, and a link that carried nothing for two hundred ticks re-converges on the next whole world with nothing restarted |
| `TestLinkMeasurementNeverEntersTheComparedSurface`, `TestTheOperatingPointIsPublished` | `internal/app` | D-24/D-11: no timing value reaches the surface two instances agree on, and every value the cadence adapts is readable from telemetry and `:session` |
| `TestStagedLinkShapingKeepsCorrectionsBoundedAndRecovers` | `internal/app` | The same claims over a kernel-shaped socket rather than an in-process link: four `tc netem` stages, bounded magnitude through all of them, and recovery when the qdisc is removed. Opt-in behind `VIF_NETEM=1`, because a qdisc on `lo` is a machine-wide change |
| `TestSharedSnapshotExcludesLocalSchedulerTiming` | `internal/app` | Runtime parity ignores deadline-slip telemetry while keeping absolute simulation tick/state |
| `TestSharedSnapshotComparesElapsedGameTime` | `internal/app` | D-21: two instances driven the same number of ticks report the same elapsed game time, it equals `tick * interval`, and a forged value moves the shared surface |
| `TestSimTimeIsAFunctionOfTheTick`, `TestSimTimeAdvancesByExactlyTheTickInterval`, `TestManualEpochIsTheSimEpoch` | `internal/engine` | D-21: the instant is decided by the tick alone, advances by exactly `DeltaTime`, and a 20-tick threshold lands on tick 20 rather than 19 |
| `TestFSMTriggersAreReplicated` | `internal/app` | D-20: no FSM transition in any bootable config tree, the embedded copy included, triggers on a `ClassLocal` or unclassified event |
| `TestSnapshotDeclarationsMatchImplementations` | `internal/manifest` | D-19 in both directions: a declared carrier must implement `SharedStateSaver`, and an implementer must be declared |
| `TestSnapshotStateSystemsRoundTrip`, `TestSnapshotCarriersAreSharedOrDual` | `internal/manifest` | D-19: every declared carrier writes bytes it can read back, and none is player-profile |
| `TestSaveStreamsReportsEveryIssuedStream`, `TestLoadStreamsResumesTheGeneratorsSystemsHold`, `TestLoadStreamsNamesUnknownStreams`, `TestStreamIssuesAFreshGeneratorPerDraw` | `internal/engine` | D-8/D-19: the stream inventory is complete, restoring moves the generator a system holds, an unknown name is reported, and a re-draw does not resume the finished game |
| `TestSetStateResumesTheSequence`, `TestSetStateRejectsZero` | `pkg/vmath` | A recorded position reproduces a sequence from where a run reached, and a zero state cannot produce a dead stream |
| `TestStreaming_Deterministic`, `TestStreaming_CheckpointContinuesTheExactStream`, `TestStreaming_RestoreRejectsInvalidStateWithoutMutation` | `pkg/genetic` | D-19: deterministic refill makes seed plus operations one stream; a JSON-round-tripped checkpoint preserves PCG/queue/pending/ID state for 250 further transitions; a rejected state is atomic |
| `TestRegistry_ExportImportContinuesSamplesAndScouts` | `pkg/genetic/registry` | Registry continuation includes the ordinary engine plus the independent scout PCG and bin counter, matched by exact species ID/name |
| `TestCaptureReconstructsTheSharedWorld`, `TestCaptureCarriesEveryDeclaredSystem`, `TestCaptureCarriesNoPlayerState`, `TestVerifyCaptureRejectsATamperedBody` | `internal/app` | D-19: a capture encoded, decoded and installed leaves the shared surface equal; every declared carrier is present; no player placement reaches a capture; a modified body and a foreign seed are both refused |
| `TestInstalledWorldStaysIdenticalForFiveHundredTicks` | `internal/app` | D-19's construction proof over three seeds: an installed world's *future* matches for 500 further ticks with shared species live. Player-domain production is stopped first, because a capture carries no player state and a crossing is Phase 4's subject |
| `TestCaptureContinuesInAnotherProcess` | `internal/app` | The same gate with the two halves in **different processes**: the capture is bytes on a disk, the two start at different wall instants, and the receiver paces its 500 ticks in bursts. Nothing about the pacing clock can reach the simulation (D-21) |
| `TestSimulationEpochIsSessionIdentity` | `internal/app` | The control behind D-19's absolute instants: a receiver whose `SimEpoch` differs installs the same bytes and diverges, so the epoch is session identity beside the seed |
| `TestNavigationPhaseIsLoadBearing`, `TestNavigationPhaseSurvivesAnInstall` | `internal/app` | D-17/D-19: a capture carrying the phase a world with *no* carrier would hold, or targets the sender never had, diverges one tick after the install; the unmodified capture holds for 200 |
| `TestNavigationRouteRebuildPhaseIsLoadBearing`, `TestNavigationRouteRebuildSurvivesAnInstall` | `internal/app` | D-17/D-19: the gateway half, in the tower region — the only scenario any shipped config has that makes `route_rebuild_ticks` pace anything. A zeroed budget rebuilds its route graphs on different ticks than the sender within 22; the unmodified capture rebuilds on the sender's for 200 |
| `TestGeneticContinuationSurvivesAnInstall` | `internal/app` | D-19: the gateway world keeps spawning for 200 ticks after an install and every captured genotype/EvalID agrees; the former archive-only carrier failed within ten |
| `TestSnapshotJoinCarriesTheGoldDeadline` | `internal/app` | The Phase 2 defect, closed: a joiner arriving mid-sequence reads the same remaining time as its host, and keeps reading it. `gold.timer` is in the compared surface again |
| `TestSnapshotJoinTakesTheHostsWorldNotItsOwn`, `TestSnapshotJoinLeavesEachParticipantDrivingItsOwnCursor` | `internal/app` | A joiner adopts the host's world and record position rather than re-deriving them, and the D-13 control assignment is re-derived rather than adopted with the component that carries it |
| `TestSoloRunBecomesAHostAndAdmitsAParticipantMidRun` | `internal/app` | D-22 end to end over a socket: a solo run opens a port hundreds of ticks in, a joiner installs the world it is sent, closes the tick gap the transfer opened, and takes its cursor from the arrival crossing |
| `TestAReconnectIsTheSameJoin` | `internal/app` | A dropped participant returns through the same path, at a tick the host has moved well past |
| `TestSnapshotChunksRoundTrip`, `TestSnapshotAssemblyRefusesAConfusedTransfer` | `internal/network` | The capture's chunk framing over the sizes that occur, and the four confusions its header refuses — a skipped predecessor, a frame from another capture, a truncated frame, an empty body |
| `TestSnapshotCostAtTheStormHighWater`, `TestCorrectionCostAtTheStormHighWater` | `internal/app` | Report rather than assert: the bytes, host stall, install cost and allocation peak a cadence is chosen from, and the same for a correction — the delta against the keyframe, the diff and apply cost, and the difference between a join's install and a correction's into the same receiver |
| `TestSwarmKeepsIntegratingWhenLockCannotResolve`, `TestSwarmLeavesLockWhenChargeCannotResolve` | `internal/system` | A refused species state entry is a delay, never a wedge: the chase keeps integrating and the lock is not held frozen and enraged |
| `TestLinkLossDoesNotDespawnWhereItIsObserved` | `internal/system` | A lost link produces an artifact, not a removal, and a second notice is a duplicate |
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
`App.SetDispatchTap`; `journal.FuzzDriver` with `Step()` for lockstep driving;
`journal.FuzzOptions.Resizes`/`MapSetups`/`MapMotionsOnly`; `FastRand.State()`.

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
under the session. Both criteria carry them, the socket one included.

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
neither status bar may show `LAG`: a resize reflows one instance's view and touches
no shared state. The latch stays `LOCK` for the life of a session run, including
before a joiner has arrived and after it leaves, so `NET:WAIT/LOCK` and
`NET:DOWN/LOCK` are both expected.

A healthy run may show `COR n` and should not show `LAG`. The first is the size of
the last correction and a small one is the ordinary condition — a guest predicts
between corrections and is told what it got wrong. The second says this instance is
far enough behind the session that its own crossings reach the host after the ticks
they name, and it is about the link rather than the game. Quit the joiner: the host
must change to `NET:DOWN/LOCK`, remove only the remote cursor and continue accepting
local input. `:d save` is refused while peers are live: its synchronous logger drain
holds the world lock and can overrun the playout lead. On a solo or replayed copy it
is still not a byte-for-byte parity diagnostic because it deliberately includes
local view and owner-authored metrics; the runtime digest compares only the shared
surface. What is worth chasing is a correction magnitude that *grows* rather than
one that is non-zero, a persistent `LAG`, a different shared actor or progression
result after a correction, or a nonzero
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

## 9. Current limits and deferred work

### 9.1 Who decides what

Authority is explicit, but it is not uniform across every component.

| State | Authority | Mechanism |
|---|---|---|
| Shared simulation | **Game host** | The host simulates the canonical world and publishes keyframe/delta corrections (D-23). Game guests run the same deterministic code as predictors; their result is provisional. |
| Owner-authored shared state (D-13) | **Per cursor**, the instance that simulates it | `SimulatesLocally` admits exactly one writer; the value is transported, never re-derived. This is per-object, not per-session, and does not depend on topology. |
| Session identity, map bounds, roster changes and live operator reset | **Game host** | The `JoinAnchor` (schema, tick rate, seed, session counter, config and corpus identity), the D-14 map latch, participant IDs, roster slots, barrier delay, arrival/departure crossings, and serialization of the exceptional session-wide reset command. |

The game host is participant one and also implements the protocol's coordinator.
Its two roles are related
but distinct: it allocates identity and serializes membership, and its world is the
answer when predicted shared values disagree. It is *not* the writer of another
cursor's D-13 cells; those remain per-owner values and travel on their own stream.
`event.OnWire` still excludes re-derived `Shared` events because sending them would
apply them twice. What crosses from a participant is the request, and what makes
the host's resulting state canonical is the correction.

In an A—B—C link chain, the coordinator that issued the adopted offer remains the
authority for all three. B relays A's request to the host path and relays the
host's correction toward C; it does not mint a second session or become a local
authority.

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

Crossing epochs, owner-state syncs and corrections all relay. Epoch deduplication
is per source and correction deduplication is per authority capture/chunk, so a
second path neither applies nor forwards the same information twice. A relayed
correction retains the host's capture tick and integrity proof.

Three things remain open, and each is a property of the graph rather than of the
transport:

1. **Delay is a constant, not a diameter.** `NetworkBarrierDelayTicks = 3` is
   negotiated once at 150 ms. An artifact crossing several links must still
   arrive before its absolute apply tick, or the host sees it late and the guest
   predicts longer than intended. `network.barrier_late` and `network.stale` are
   the signals. Phase 5 made the *cadence* and admission a function of measured
   link conditions (D-24) and deliberately left the playout lead alone: the lead
   decides the tick an artifact applies at, so changing it mid-session would
   change an agreed apply tick and is a protocol change rather than a scheduling
   one. Per-peer cadence is also a property of a *direct* link — a participant
   reached by relay rides its neighbour's schedule, because the flood forwards
   what the neighbour was sent.
2. **Departure and authority need a reachable game host.** One producer gives a
   roster change one apply tick, and one world supplies corrections. If participant
   one departs or a partition makes it unreachable, no coordinated election or
   state migration replaces either role. Each affected game guest continues its
   own local fork from the last authoritative state and displays
   `HOST LOST:LOCAL`; it cannot extend the old roster or claim authority for other
   peers.
3. **A partition has no session-wide detector.** Direct neighbours observe their
   lost link, but after a graph splits there is no digest edge between the two
   components. A component without the host can keep predicting, but it cannot
   receive authoritative corrections. Its prediction becomes local game state,
   not a promoted authority shared with the other component.

The links themselves are still built as a star, because `-join` dials one address.
The relay is what makes any other shape work; wiring a participant to dial more
than one peer is a CLI change, not a protocol one.

### 9.3 Trust, rollback and host migration

The trusted branch now exists: it is the host's versioned `SharedCapture`, and a
guest installs it rather than asking a digest which peer is right. Keyframes make
loss recoverable without an artifact ledger; deltas are accepted only against the
named keyframe and only when the rebuilt capture passes its own integrity hash.
The digest remains diagnostic telemetry between corrections.

What is not built is rollback *and replay*. Installing a correction adopts the
authority's earlier capture tick, so the guest has not yet replayed its own
outstanding requests from that point to its former predicted present. That is the
bounded flicker and repeated simulation Phase 6 would remove. The deterministic
capture contract—including exact genetic stream continuation in snapshot schema
2—is the state prerequisite for that work; a retained, canonical input suffix is
the remaining replay prerequisite.

Trust is still operational rather than cryptographic and authentication is
deliberately deferred while functional correction and bandwidth work remains.
Links are plaintext,
`MsgAuthRequest`/`MsgAuthResponse` remain reserved, and `Config.TLS` has no CLI
surface. The host structurally rejects roster artifacts from non-coordinators, but
cannot authenticate a participant's claim to ordinary crossings. Host migration
would additionally require transferring the newest authority, membership and
in-flight admission state before electing a replacement; election alone would
promote an arbitrary predictor.

### 9.4 Known limitations

| Area | Current limit |
|---|---|
| Playout | The three-tick receive lead is constant, not graph-diameter aware. Late artifacts are measured; there is no lag compensation. |
| Topology | The protocol can relay, but `-join` dials one address. Per-peer cadence and relevance describe direct links; a relayed participant inherits its neighbour's schedule. |
| Correction content | Relevance changes *when* a whole correction is sent, not its content. Phase 6 supplies hash-guided, independently proved shards and partial reconcile. |
| Host loss | Each affected game guest continues an explicit local fork. There is no election, shared roster authority, partition merge or automatic migration. |
| Join | Admission refuses an excessive catch-up gap or a link below the convergence floor. Join gates serialize transfer, though arrivals can share a cadence keyframe read. |
| Rewind | A correction adopts its host tick and does not yet replay later local crossings. Phase 6 adds a bounded canonical suffix and replay. |
| Operations | Live pause/speed/step, raw shared mutation and synchronous snapshot save are refused. Programmatic embedder mutation remains the caller's responsibility. |
| Trust | Links are plaintext and unauthenticated. Authentication is deferred; roster artifacts are structurally host-only. |
| Transport loss | A bounded-queue refusal is counted and logged, not application-retransmitted. Newer corrections and keyframes provide state recovery. |
| Portability | `float64` simulation claims determinism within one implementation build, not cross-platform bit-exact lockstep. |
| Presentation | A terminal smaller than the map clips the render buffer; a windowed composite/vision box is separate presentation work. |

Domain-boundary debt that remains visible to tests:

- `unstampedLocal` still exempts Local-class pushes with an ambient Shared tag;
  empty it, then remove the exemption.
- `event.EmitDeath` bypasses `PushEvent`; batches remain domain-pure only because
  callers pass the domain explicitly.
- `uint32(entity)` in gateway/adaptation code assumes route anchors stay Shared.
- Mixed `combat.` telemetry is excluded as a whole; authoritative component state
  already covers the shared gameplay result.
- Optional tower configs still bind to `player_entity`, normally slot 0. Roster
  slots remain cursor-only. The deferred rule is entity zero for
  session-owned/uncredited, nonzero for explicitly cursor-owned, applied to both
  tower regions and their gateways without a new ownership type or config value.

### 9.5 Next work

Phase 6 first makes steady-state bytes proportional to disagreement through a
versioned hash hierarchy and selectively proved shards, with compressed keyframes
as bounded fallback. It also adds bounded rollback *and replay* so a game guest's
own outstanding crossings survive a correction that predates them. Exact scope and
acceptance gates are in [Multiplayer enhancement plan](multi-player-enhancement.md)
and [Phase 6 implementation prompt](phase6-implementation-prompt.md).

Authentication is deferred. Coordinated host migration remains a later project
that must transfer authority, membership and in-flight admission before election;
the current independent local continuation does not substitute for it.

What remains outstanding and independent of that work:

1. **Close the programmatic operator surface.** Make embedder-level map/FSM
   mutation explicitly session-aware rather than relying on the interactive
   command policy and harness discipline.
2. **Empty `unstampedLocal`,** then delete it and its exemption.
3. **Settle tower ownership in optional maps.** Apply the documented zero-entity
   session-owned sentinel to both tower regions and their gateways; keep cursor
   roster slots cursor-only.
4. **Windowed composite / vision box.**
