# Multi-instance domain model — vif

This document describes the multiplayer domain model as implemented. Rules D-1
through D-24 define simulation ownership, transport, correction, admission, and
authority continuity. The operational overview and remaining roadmap are in
[Multiplayer architecture and remaining work](multi-player.md).

Terminology is deliberately orthogonal:

- a **network peer** is a transport endpoint;
- the **game host** is the current Shared-domain authority and coordinator;
- a **game guest** predicts the Shared world and adopts corrections;
- a **relay** forwards and retains authoritative content without authoring it;
- **Shared** and **Player** describe simulation ownership, not network role;
- the **authority term** identifies the generation under which authoritative
  content was produced.

Peer 1 opened the session; it is not permanently synonymous with the authority.
A successful handoff increments the term and moves authorship without changing
participant IDs, roster slots, or entity domains. In this document, `epoch` means
one source's closed tick of crossing production.

## 1. Domains and identity

Every `World` has two domains:

- **Shared** is authoritative on the host and predicted on guests. It contains
  shared species, cursors, gold, walls, towers, gateways, map state, simulation
  time, and the shared FSM.
- **Player** belongs to the local participant. It contains corpus glyphs, drains,
  nuggets, weapons, projectiles, loot, and presentation effects. It is never
  reconstructed on another instance.

`core.Entity` is `[domain:8][id:56]`. Each domain has its own allocator counter;
zero is invalid in both. A cursor's roster slot lives in `CursorComponent` and is
not encoded in the domain tag.

A peer and a cursor are separate things. Every admitted peer holds a participant
identity ([Multi-player](multi-player.md) §1), a roster entry and the authority
term it follows; a roster slot binds it to a cursor. The coordinator of a dedicated
host holds `parameter.NoPlayerSlot`: it authors the Shared world and puts nobody on
the map. It is the only roster entry allowed no cursor, and anything that counts
participants, `parameter.MaxPlayers` included, counts cursors, not roster entries.

## 2. Domain rules

### D-1 — Reads follow ownership

A player-domain system may read Shared state. A shared-domain system reads Shared
state only, except for the narrow geometry and owner-authored seams named by D-12
and D-13. Cross-domain access is declared and checked rather than inferred from a
filename or call stack.

### D-2 — One instance simulates each player

Only the instance that owns a cursor simulates that cursor's weapons, projectiles,
drains, nuggets, and player effects. `World.SimulatesLocally` and
`World.ResolveOwnedCursor` are the admission gates. Remote Player-domain state
does not exist locally.

Bots use the same owner boundary: watching or replaying a bot presents that
instance's private effects. A short lightning zap survives its first driven tick
so the next frame can display it.

The same rule admits owner-authored cursor writes: grants and per-tick ageing do
not update a cursor this instance does not simulate.

### D-3 — Cross the smallest shared outcome

When a player mechanic affects Shared state, it emits the smallest artifact that
fully determines the shared outcome. A crossing applies at one agreed tick, a
playout lead after its production, on its producer as everywhere else; the D-18
prediction answers the keystroke in the meantime. The host applies requests in its
own order; its next correction is canonical. An artifact that instead decides what
the world *is* is barrier-bound: it also closes the capture fence at production and
never enters the replay suffix (D-9).

| Effect | Crossing artifact |
|---|---|
| Direct player hit on a shared target | one combat request per target |
| Missile, dust, or disruptor area effect | immutable centre/radius/attack geometry |
| Drain fusion | one spawn request from the causal participant |
| Shared gold member typed | header, member, and typist cursor |
| Jump to the live gold sequence | cursor |
| Dying drain donation | target and heal amount |
| Personal drain death affecting shared progression | owner cursor |
| Motion, typing or nugget cursor advance | cursor and absolute destination cell |
| Pointer placement | cursor and the newest cell the pointer named before the next tick or input |
| Owned shield or ray striking shared species | target/member set and owner cursor |
| Cursor entering or leaving combined defeat state | cursor and state |
| Scenario-captured key pressed or its button released | cursor, key, and key or pointer source |

Effects on Player targets do not cross. Shared follow-up events derived from a
crossing do not cross again (D-5).

"Fully determines" includes the values a receiver cannot re-derive. A crossing
that misses the lead is applied late by the authority, and a projection re-applies
one at its own tick over a world the capture describes, so anything drawn from a
shared RNG stream at apply time would be assigned to a different artifact on each
instance — a combat knockback in another direction, not a rounding difference. A
crossing therefore carries its own identity, `event.CrossingID`: the producing
participant and that source's wire sequence, stamped by the crossing path before
the frame is encoded, so the producer's own copy and every peer's hold one. A value
of this kind is seeded from that identity and the stream is left alone.

Identity descends with the derivation. A re-derived event uses the stream only when
every instance produces it at the same tick, and an event derived from a crossing is
not that, for the same two reasons: it inherits the root's identity. A combat chain
and an explosion's per-composite hits are both that shape. An event with no
crossing above it has no identity and uses the stream, correctly.

The same lateness decides the *effect*, not only the value. Two crossings on one
target can apply in opposite orders on two instances when one of them was late, so
an effect that latches or overwrites keeps a different one on each. A shared
outcome several crossings may reach must therefore compose the same way whichever
order it saw them in: a budget per producer rather than one window per target, and
an accumulation rather than a replacement for the ones that join it. Combat's
damage window is of that shape. Its knockback window is not: it covers every
attacker, so the knockback an instance applies first holds it and the next
correction repairs a difference.

`barrierBound` names the set: arrival, departure and full reset, which create or
destroy shared identity; the swarm and quasar spawn requests, which allocate it; and
a drain's defeat and a cursor's defeat state, which advance the progression a
region gates its spawns on or rebuild the level.

### D-4 — Wire payloads name Shared entities only

A replicated payload may name Shared entities. Player emitters are reduced to
values such as cell, velocity, radius, amount, and owner cursor. Local payloads may
name Player entities.

The same restriction applies inside a capture: a component on a Shared entity may
not hold a Player-domain entity handle. Store-derived local indexes are used where
runtime effects need such handles.

### D-5 — Derived events are not transported

An event produced deterministically from a replicated event is re-derived on every
instance and must not be sent. For example, explosion geometry crosses;
per-target combat requests derived from that geometry do not.

### D-6 — Presentation and personal effects are Player-domain

Lightning, flash, fadeout, splash, motion markers, explosion smoke, pulse rings,
particles (dust, decay and blossom), orbs, bullets, missiles, and loot are
Player-domain; a materialise beam takes its requester's domain (§3). Encounter
level clears explicitly preserve loot; run resets clear it. Shared corrections
retain each receiver's drops and ownership, and loot revalidates its bounds and
walls after an install. Collected weapon charges travel through the existing
owner-state sync (D-13). These effects may depend on local view state and must not
decide a Shared outcome.

An effect keyed to a Shared entity retires with it. An install writes the world
rather than replaying the lifecycle that ends such effects, so the owner's absence
is the signal its own system acts on; an effect with no lifetime of its own and no
owner left would otherwise draw for the rest of the session.

Player-domain does not mean that only one participant sees an effect. A shared FSM
may raise a local effect on every instance. Effects intended for one participant
carry a cursor scope; entity zero means session-wide, while a non-zero cursor is
admitted only by the instance that simulates it.

A weapon mounted on a Shared host follows the same boundary; storm's red circle is
one, a turret the storm arms for each burst. The mount's aim is Shared state
refreshed every tick from Shared positions, and every instance derives its own
Player-domain shots and muzzle from that common aim, so a correction repairs
direction without putting Player-domain identity on the wire. A hit on a cursor is
applied by that cursor's owner alone; equal-distance aims use roster order.

### D-7 — Domain is explicit ambient context

`World.WithDomain`, `PushEventDomain`, `PushLocal`, and `PushCrossing` stamp the
producer or target domain. The declared system profile does not silently change
the ambient tag. Generic systems resolve the request or target domain at runtime.

### D-8 — RNG streams are domain-separated and restorable

`RandResource.Stream(domain, label)` derives from `(session root, domain, label)`.
A dual system chooses the stream from the target domain; a Player-only system uses
its Player stream. Simulation never seeds from wall time.

Every issued stream is inventoried by `RandResource`. A capture stores the Shared
streams' generator positions, not merely their seeds: restoring a seed restarts a
sequence, restoring a position continues it. Player streams stay out of it. They
are drawn by mechanics only their own participant simulates (D-2), so a capture
carrying one installs the sender's position over every receiver's — an authority
that spawns no nuggets of its own pins each guest's nugget glyph and cell to
wherever its own stream last sat. `SaveStreams`/`LoadStreams` take the domain and
refuse a state naming another.

The environment is a deliberate dual-domain exception with one Shared stream.
An active wind draws force and direction exactly once per tick before iterating
entities, so different local drain populations cannot move the Shared RNG. Glyph
drift draws nothing: its per-level phases advance by tick delta alone, so every
instance steps its own Player glyphs on the same ticks.

A Shared stream orders draws by tick, so only work every instance performs at the
same tick, in the same number, may take one. The count matters as much as the tick:
a stream that advanced a different number of steps on two instances hands every
later draw to the wrong work, permanently, because nothing between corrections
rewinds a sequence. Three shapes fail that test, and each has its own remedy:

- A **crossing**, and anything derived from one, is seeded from the artifact (D-3)
  rather than from a position the two reach a lead apart.
- A **conditional** draw whose count depends on the entities it concerns is seeded
  from the tick and that pair. Soft collision is the case: one swarm member the
  producer had already killed cost the two instances a different number of draws,
  and from that tick every shared impulse read a different point in one sequence.
  A mount's shot spread is seeded from the tick and its host for the same reason.
- A draw a **filter** stands in front of is moved ahead of the filter instead, so
  the count is the budget rather than the outcome. Spawn placement retries are that
  shape: they test live cursor cells and a wall set a correction repairs, and both
  differ across the lead.

### D-9 — Entity identity is domain-local and deterministic

`CreateEntity(domain)` uses one counter per domain. Shared entity creation order
must be identical wherever the same Shared world is represented. Creation and
destruction telemetry is tracked per domain; aggregate values are sums.

A crossing that allocates Shared identity, or advances the progression a region
gates its spawns on, is therefore barrier-bound (D-3): a producer applying it a
playout lead early would number the world differently from everyone else, and a
correction would be repairing identity rather than state.

### D-10 — Event class and event domain answer different questions

Each event type declares one class:

| Class | Meaning |
|---|---|
| `Shared` | emitted and consumed in Shared simulation; re-derived, not sent |
| `Bus` | may be a Player-originated request affecting Shared state |
| `Local` | never replicated |
| `Stamped` | class depends on the event's explicit domain tag |

`event.Replicated` answers whether two instances should hold the record.
`event.OnWire` answers whether another peer must receive it. The wire set is
narrower: sending a re-derived Shared event would apply it twice. A Bus type is on
the wire only when a Player producer explicitly used the crossing path. A Stamped
type is on the wire only when its tag names a Shared target and `stampedCrossings`
lists it, which today is the direct combat request alone. A payload that reports
itself `Derived`, a combat chain follow-up, never is (D-5).

`EventWindStart` and `EventWindCancel` are Shared events: a Shared FSM can
re-derive them on every instance, so sending them would apply the same wind
twice. Each instance applies the sampled wind to its own Player drains as well as
the predicted Shared species. A future player-originated wind trigger needs a
separate Bus artifact rather than changing these derived events into wire data.
`EventDriftStart` and `EventDriftCancel` follow the same rule.

### D-11 — The host is exact; guests converge

The host owns the canonical Shared event order, entity creation order, RNG
progress, and component values. A guest equals the most recently installed host
world plus its bounded local prediction suffix; it may differ between corrections
by design.

Shared entity identity and creation order stay exact. Component values are either
re-derived deterministically or owner-authored and transported, never both.

Correction containment judges an ordinary frame by its own source's fence in the
capture and a barrier-bound frame by the capture tick (§4.1).

### D-12 — Shared geometry may inspect both domains

A Shared system that claims cells—spawn footprint clearing, composite sweep, or
wall push-out—may enumerate both domains. Its Shared outcome depends only on the
cell set and protection masks; Player victims remain local effects. Emission is
split by domain so a Shared record never names a Player entity.

### D-13 — Owner-authored Shared values have one writer

A Shared cursor carries values written by exactly one instance rather than
re-derived: its energy, heat, shield, boost, weapon and combat stores, and the
`CursorViewComponent` and `PingComponent` presentation. For every cursor a
participant owns, the authority's included, those cells stay out of the manifest
hash, the correction magnitude and selective repair, and every one but
`PingComponent` travels on the owner-state sync. Ping is the exception because
nothing reads a remote cursor's copy — the renderer reads the local cursor's — so a
mirror frozen at its creation value costs nothing. The two lists are otherwise the
same and have to stay so: a value on one and not the other is either hashed and
unrepairable, or repairable and silently overwritten. The sync is committed like a
crossing: the authority writes a guest's at its own next tick and relays it with
that tick, so every instance writes it at one tick.

`CursorComponent.PeerID` is set when the cursor is created, by the arrival
crossing or from the roster on a reset or a join, and is compared like any Shared
value. `Control` is re-derived on every instance from `PeerID` and the local
participant, and the hash reads it as zero.

Position is different: it is Shared and changes through
`EventCursorMoveRequest`. Protection is a deterministic creation constant.

A capture carries owner-authored values so a joiner can materialise remote
cursors. During an in-session correction, the receiver preserves the values for
cursors it authors and rebuilds control/roster binding from participant identity.
No shared-profile system computes a remote owner's values.

The sync runs every `NetworkSyncTicks`, so the authority's mirror of a guest's
cursor is that stale at worst and a correction can carry the stale copy to a third
participant. What bounds it is that an install leaves the sync sequence and its
cadence counter alone, so the owner's next sync still lands over whatever the
correction wrote.

### D-14 — Map bounds are Shared authority

`MapWidth` and `MapHeight` are Shared simulation state, carried in every capture
header. `CropOnResize` is not in the capture: it rides the join anchor with the
bounds, and only `EventLevelSetup` and a reset write it, each applied alike on
every instance, so scripts may read it as agreed. The cell aspect rides the anchor
the same way, and a guest plays at the host's. Session admission installs all
four before the FSM boots. While `World.SessionShared()` is true, a terminal
resize changes only local viewport/camera state; it cannot crop the map or announce
a Shared cursor move. A scenario's `[map]` floor bounds every one of these writes
from below; it travels with the scenario, so every instance holds the same one.

The session-shared latch survives lobby wait, disconnect, and journal replay. A
map script asking for `viewport_width`/`viewport_height` is asking for the extent it
draws on, so under the latch it reads the map instead of the terminal; `camera_*`
and `color_mode` have no shared counterpart and a script reading one is warned,
because it can choose a different FSM branch on different terminals.

### D-15 — Classification is declared once

System domain profiles, required/optional dependencies, and snapshot participation
are declared in `internal/manifest/definition.go` and generated into runtime
tables. Initialization dependency order is distinct from per-tick system priority.
A `dual` profile means the system resolves domain per request or target; it does
not grant unrestricted cross-domain writes.

### D-16 — Shared triggers elect one causal player

A Shared trigger must not fan out into every participant's Player mechanic when
those mechanics would each cross one logical Shared result. The trigger selects a
causal cursor deterministically before any Player state is read, and only that
cursor's owner may emit the crossing.

Personal causes remain distinct. Separate participant-owned drain collisions may
each produce their own swarm request.

### D-17 — Throttle phase is state

A cached Shared derivation with a recompute throttle carries both its inputs and
its phase. Dirty history changes the age of the value consumers read. Navigation
therefore snapshots recompute counters, last targets, and gateway route rebuild
budget, then re-derives fields and graphs during install from the captured inputs,
keeping any it holds from the same inputs on a wall grid that has not moved since.

Local view changes do not mark Shared navigation dirty. Whether a field is
computed when due or at its first reader is local too: the phase is the throttle's
decision, which is the same either way.

### D-18 — Local prediction is private

A value selected by this participant's input is reflected locally at once. The
local cursor prediction is a bounded FIFO of requested absolute cells not yet
announced by Shared simulation. Input, camera, rendering and the weapons read it
through `World.CursorCell` — shots and orbs leave the cell the player sees, and
what they hit crosses as Shared events like any other; Shared systems do not.

`World.PushCursorMove` advances prediction and emits the crossing atomically from
the producer's perspective. A pointer places the cursor on every cell it names, but
crosses only the newest once a tick, before the tick opens or before this
instance's next journaled push, so placements keep the order they were made in;
cells it passed between ticks are never placed.

This instance's own placements land in production order, so each consumes the
oldest outstanding prediction by its crossing identity, whatever cell a clamp made
of it; one without identity (solo play, replay) pops on a matching cell. An
authoritative move the prediction did not produce clears the queue and snaps, and
the own placements already in flight are then consumed without touching a newer
prediction. A sweep that outruns the ring sheds its oldest cells the same way, and
an own placement an install discards as already applied is consumed there. Replay
reconstructs prediction from the `EventCursorPredicted` note each placement journals.

`World.PushInputPress` is the same pairing for a scenario's input hook: a journaled
local echo lights and sounds the pressed button on this instance at once, and the
crossing lights the Shared button a lead later. The echo presents the gesture, not
the verdict, so nothing rolls back when the machine ignores the press. The
renderer shows this instance's own presses from the echo and every other from the
Shared button, and the button sounds an applied press only when it came from a peer.

### D-19 — Every future-affecting Shared value is restorable

A value that can change a future Shared outcome is one of:

1. a component on a Shared entity;
2. declared system state carried through `engine.SharedStateSaver`; or
3. state re-derived by the install itself from captured inputs.

Declared system carriers are:

| System | Private Shared state |
|---|---|
| `wall` | maze generator position |
| `adaptation` | route weights, sample pool, consumer head, fallback rotation |
| `genetic` | streaming checkpoints, archives, pending evaluations, IDs, scout state, fitness accumulators |
| `navigation` | group and cursor-pursuit cache phases, targets, route rebuild budget, route endpoints |
| `gold` | sequence liveness, spawn gate, header, per-slot contribution; the deadline rides the header's timer in the stores |
| `storm` | live root entity, swarm spawns pending a blue attack |
| `meta` | kill tallies, defeat latches and reset tick, cycle damage multiplier |
| `environment` | active base wind, remaining duration, enable/applied phase; drift speed, direction, remaining duration and per-level sub-cell phases |

A carrier is also where a status cell the compared surface excludes has to live.
`SharedKey` answers what two instances must agree on, and a capture reads through
it, so an excluded cell is carried by nobody: `kills.*` is a mixed-domain
aggregate and `energy.damage_multiplier` sits under a player-profile prefix, yet
both steer shared FSM regions. `meta` carries them.

A capture also carries every Shared RNG stream position (D-8), FSM runtime state,
Shared component stores, allocator counters, the compared status surface, and one
applied crossing fence per participant (`CaptureHeader.Crossings`). Durations are
relative to capture tick. Absolute component instants are sound because
`engine.SimEpoch` and tick interval are session identity. Delayed FSM work names
its deterministic compiled action identity, so transition-delayed actions restore
independently of their queue position. An install preserves receiver-owned
cursor values, rebuilds roster/control binding, derives spatial/navigation caches,
and restores status only after carriers have loaded.

Persistent Player-domain effects controlled by Shared FSM state are re-derived by
the install itself. Only immediate, unguarded `ClassLocal` lifecycle events marked
`reconcile = true` participate: old paths release with old variables, new paths
acquire with imported variables, and a changed scope variable is treated as a
boundary crossing. Staging imports remain side-effect free.

That covers the *level-triggered* half: a hold that follows a Shared region is
re-derived from the imported state. The *edge-triggered* half is a ledger, because
a one-shot reward cannot be re-derived from state that no longer says it happened.
A Shared-domain `EventSpeciesKilled` raised while the instance predicts is stamped
`event.PhasePredicted` and held by dying entity; an install proves it by the
entity's absence from the world it wrote and raises `EventSpeciesKillConfirmed`
once, which is what `LootSystem` and `BoostSystem` consume. Recording is idempotent,
so the re-derivation a rollback forces costs nothing. See
[Multiplayer](multi-player.md) §3.4.

### D-20 — Shared FSM regions use replicated triggers

Every FSM region is Shared state. A transition trigger must therefore be present
on every instance; a `Local` event cannot steer a Shared region. Per-instance
effects are emitted by the local owning system or by a cursor-scoped action without
moving the region differently.

### D-21 — Simulation time is tick-derived

`engine.SimTime(tick, interval)` measured from `engine.SimEpoch` is the only
simulation instant. Wall time paces ticks and presentation but cannot select a
Shared transition or kinetics threshold. `DeltaTime` and
`time.game_elapsed_ms` are functions of tick interval and completed tick.

### D-22 — Admit before capturing

A running join becomes a transport peer before the authority capture is read. It
buffers traffic until the world it applies to exists, closing the interval in
which an event could otherwise be in neither the capture nor the wire stream.

The join uses a sufficiently recent cadence keyframe, waits through the receive
lead so pre-admission epochs are represented, then simulates only a bounded
transfer gap. Admission refuses a gap beyond the lead or a link that cannot carry
one keyframe inside the convergence floor.

After install, the barrier remembers the capture tick and each source's fence.
Already-contained queued artifacts are discarded, and later arrivals are tested
against the same boundary.

### D-23 — The host world is the correction

The host publishes an authoritative `SharedCapture` on a cadence. JSON remains the
schema and integrity surface; a bounded versioned deflate envelope precedes
chunking. A guest resolves into a reusable staging world and commits between
ticks; a body is proved by its integrity hash as it is resolved and a repair by the
root, so neither is hashed again to stage.

A correction exchange is selective:

1. the authority sends the root of a deterministic manifest, and its section hashes
   to a receiver whose root differs;
2. an equal root produces a hash-only acknowledgement and no state body;
3. a mismatch descends only into differing sections and pages;
4. returned pages prove their own hash and must reconstruct the authority root;
5. any refusal leaves the live world untouched and falls back to a keyframe.

The manifest root excludes tick-local header values so worlds at different
prediction ticks can compare. A repair must match the manifest's authority term,
participant, and per-source crossing fences. Capture integrity may differ when an
equal canonical world has another dense-store order.

Correction magnitude is measured during commit. It is telemetry, not a verdict.
Runtime shared digests identify drift surfaces between corrections but do not
escalate to a terminal desynchronisation state.

An install never moves the clock backwards. A capture behind it is projected: the
staging world takes the capture, this instance's owner-authored values, its own
encoded ordinary crossings past the capture's fence for its source, the peer
crossings it applied past their source's fence, and the applied barrier-bound
artifacts due after the capture's tick, simulates to the live tick, and the
projection is what the live world is reconciled to — clock and barrier alone when
the two are already equal. A hole in the retained suffix makes it unavailable and
selects authority-only recovery. A capture ahead of the clock waits for its tick
within `parameter.NetworkHoldTicks`, newest wins, and one further ahead is adopted
at its own tick as the jump it is (multi-player.md §3.3, §4.1).

### D-24 — Cadence adapts; the convergence floor does not

Each direct peer receives a cadence and keyframe interval selected from measured
round-trip time, jitter, delivered bytes, saturation, and relative correction
demand. Degradation is immediate; recovery is stepped. Keyframes stretch before
cadence slows because whole worlds are more expensive than manifests and ordinary
repairs.

Every participant receives a whole world, or proves with a hash-only answer that
it holds the authority's, within every `SnapshotFloorKeyframeTicks`; a keyframe is
skipped while every participant has proved it within half the floor. A link that
cannot meet the floor is refused at admission or reported as an unrecoverable
operating condition. Relevance changes when a peer receives a correction, not
which version of Shared state is authoritative.

## 3. Spatial and entity classification

`Cell` is 256 bytes: count, Shared count, alignment padding, and 31 entities.
Shared occupants are the prefix `Entities[:SharedCount]`; Player occupants follow.
`ReservedPlayerPerCell` preserves Shared capacity, and insertion rejects rather
than evicts when full.

Spatial queries take `ScopeShared`, `ScopePlayer`, or `ScopeBoth`. Shared species
target Shared entities; local weapons may target both. `PositionBatch.CommitShared`
is the Shared placement gate.

| Domain | Entities |
|---|---|
| **Shared** | cursor, quasar, swarm, storm, snake, eye, pylon, kraken, tower, gateway, wall, gold, marker, button, FSM, time |
| **Player** | glyph, nugget, drain, particle (dust/decay/blossom), tracer, bullet, missile, orb, lightning, flash, fadeout, splash, motion marker, explosion presentation, loot |
| **Stamped** | materialise, spirit; domain is resolved per request |

Gold is contested Shared state: typing contribution is tallied per roster slot and
ties resolve to the lowest slot. Nuggets and loot are personal and cannot be
claimed by a remote cursor. Loot steers by its owner's private group, the field
navigation keeps toward each cursor; the roster's field answers nearest-target
questions and cannot answer "mine".

## 4. Transport and ordering

### 4.1 Receive lead

A local crossing is scheduled on this instance's own barrier at the `ApplyTick` it
stamps and encoded into the source's next epoch for peers. Every other copy
applies at the tick the authority commits: the stamp when on time, the authority's
next tick when late, nowhere once more than `NetworkCommitLateTicks` late
(multi-player.md §3.5). At tick open, due frames sort by
`(ApplyTick, Source, Seq)` and settle before `BeginTick`. There is no exception: a
typed gold member leaves at its tick too, and the typist's next keystroke validates
against the run without the members it has already typed.

A correction never rewinds the world tick or the source's production epoch. Peers
use `ProducedTick` as a replay key, and an install moves the epoch forward only
past empty ones: an epoch holding a pending crossing keeps its key.

Snapshot containment is not a tick comparison. Every ordinary frame uses its
source's entry in `Header.Crossings`; barrier-bound frames use the capture tick,
because those apply at one agreed tick on every instance including their producer.
A source the header does not name is claimed for nothing and its frames are kept.
Both the already-scheduled queue and later arrivals apply the same classification.

### 4.2 Mesh relay

The session is a tree rooted at the authority, and every link is up, down or
beside. A raw epoch travels only up: a participant sends its own and those of the
participants behind it as one `MsgBundle` a tick on its link to the authority,
else to the relay it joined through or adopted, and the authority takes a
participant's raw artifacts on its own link, else on its holder's for a bot, else —
a person behind a relay — on any. When the authority commits one it is the copy's
origin: the hop count restarts and the copy goes down every link, the producer's
included, so the producer knows what was committed; each relay passes a copy it
admitted down to the links behind it. A per-source 64-epoch window admits
out-of-order paths once, a 512-sequence window takes each crossing once whatever
epoch carries it, and the hop limit bounds forwarding. Owner-state syncs take the
same paths; a peer link to the successor carries only succession records and
departure notices.

Owner-state sync uses a per-slot sequence. Correction chunks carry the authority
term and capture tick unchanged. A relay serves selective pages only from retained
authority captures whose root it can prove; an unavailable retained tick is
reported rather than answered from another baseline. Its own manifest answer names
each participant behind it with the tick that one last proved, so the authority's
flood gate and keyframe floor count subtrees.

### 4.3 Runtime parity

Instances exchange category digests for the same surface as `SnapshotShared`. A
mismatch counts in `network.digest_mismatches` and `network.drift_part` names the
first part that differs: `positions`, `kinetics`, `combat`, `context`, `status`,
`snapshot` (the compared status surface), or `combined`. Guests are expected to
differ provisionally, so a digest is diagnostic rather than a failure state.

The player-visible distinction is one badge. A connected link shows
`nP:s <round trip>` — n players and this instance's hexadecimal slot s, omitted for
a cursorless coordinator (remote cursors show their slots as `0`–`F`) — coloured
green, amber or red by the worst of the round trip and its qualifier. At most one
qualifier shows, worst first:

- `slow!`: no feasible cadence satisfies the keyframe floor;
- `desync t`: this instance is t ticks behind, far enough that the receive lead
  is being missed;
- `loss p%`: probes went unanswered often enough for the link to be the cause;
- `slow`: the cadence backed off and prediction is carrying more.

Three states replace the badge, in this order: `Host lost` (this guest lost its
link to the authority or continues as a local fork, until a handoff is adopted),
`Migrating [k]` (a succession is open or a handoff was just adopted, k passes down
the succession list so far), and `Net: down` or `Net: wait` while the link is down
or still connecting. The measurements behind each are in the status snapshot and in
`:session`.

### 4.4 Membership and session control

Arrival and departure are coordinator-authored barrier crossings. A roster
artifact is refused unless its source held the authority when it arrived or holds
it when it applies: a dropped guest sees the link close before its dismissal's
tick and takes the term itself. Full reset is likewise serialized by the coordinator;
it preserves the closed roster and rebuilds cursors in slot order.

A bot's holder identity travels in admission, the roster and the Shared cursor.
One departure crossing removes the holder and its bots. A host may remove one bot
without removing its holder; a guest may remove its own bots.

When the authority goes, the successor is the first surviving independent
participant in the succession chain, falling back to the lowest independent
roster identity. Bot leaves cannot inherit authority. Every survivor computes
the same choice; a receiver refuses a record naming anyone else. A survivor that
cannot reach the successor continues as an explicit local fork.

The one instance that removes a participant without a crossing is an instance with
no link left: a fork, or the successor of a star. Nothing there can produce or
receive one, so it drops every cursor it does not simulate as local roster state.
Agreeing on a tick needs a second instance, and there is none; an instance that
still holds links leaves its roster alone, which is part of the unimplemented
partition case below.

A departure also clears what the instance had applied from that participant. The
identity returns to the pool and a crossing sequence starts at one, so a fence
kept from the previous holder would claim a capture already contains crossings the
next holder has yet to produce.

Speed, step, breakpoints and raw Shared mutation (`:system`, `:emit`, `:region`)
are instance-local operator actions and are refused while peers are live; so is
pause, unless every linked participant is a bot seat this instance holds
(`SeatsOnly`). Only the coordinator may reset a live session, and the reset
crosses. The programmatic surface takes the same rule through one guard:
`App.SetupLevel`, `App.Region` and `App.Reset` read the event's declared class, so
the coordinator crosses a `Bus` request, a guest's is refused, and any other class
is refused outright. Inspection modes and the telemetry save remain available
without stopping simulation.

### 4.5 Join and stream framing

Before state is accepted the handshake compares the build — protocol, simulation
fingerprint, capture and journal schemas, tick interval — and then the session:
seed, session and scenario digest, which a joiner adopts from the anchor. The
corpus is the player's and is not compared. The handshake also carries the map
latch, authority term, participant ID, and roster slot. A slot of
`parameter.NoPlayerSlot` is the coordinator declaring it drives nothing. A mid-run
join installs a current keyframe and catches up the bounded transfer gap. Reconnect
uses the same path.

`network.SocketPort` uses a fixed 12-byte frame header and complete short-read and
short-write handling. Heartbeats and deadlines close silent streams. Bounded input
and output queue refusal is counted; application events are not retransmitted.
Newer corrections and keyframes recover state.

At 20 ticks/s the event stream is small relative to authoritative state: an empty
epoch is 44 bytes, four cursor moves are about 567 bytes, and one owner-state sync
is about 703 bytes. At the storm high-water fixture, a plain capture is about
172 KiB, a compressed keyframe about 15.4 KiB, a compressed delta about 7.1 KiB,
and a converged manifest exchange about 1.5 KiB.

## 5. Telemetry and snapshot views

Three views share one status reading:

| View | Excludes | Purpose |
|---|---|---|
| `Snapshot` | nothing | local diagnostics |
| `SnapshotSimulation` | operator/session surface | replay versus source run |
| `SnapshotShared` | Player, owner-authored, view, network, and local scheduler state | cross-instance comparison |

The Shared filter is driven by system profiles and explicit key/field exclusions.
It keeps tick-derived elapsed time, Shared FSM state, Shared navigation phase, map
bounds, and canonical Shared entity digests. It drops terminal, camera, network,
pacing, Player aggregate, corpus, owner-authored cursor, and local effect state.

Correction/order diagnostics include:

- `snapshot.correction_entries`, `snapshot.correction_entities`,
  `snapshot.correction_cells`, `snapshot.corrections_held`;
- `snapshot.replay_records`, `snapshot.replay_skipped`,
  `snapshot.replay_suffix_unavailable`;
- `network.artifacts_pre_install` and
  `network.artifacts_authority_superseded`;
- `network.barrier_late`, `network.lag_ticks`, `network.stale`;
- `network.transport_lost_in`, `network.transport_lost_out`;
- `snapshot.cadence_*`, `network.link_*`, and authority-term counters.

## 6. Verification model

The boundaries are enforced mechanically:

- generated manifests classify systems, dependencies, snapshot carriers, and
  event types;
- static boundary checks compare declared profiles with component/RNG/event use;
- deterministic capture/install criteria continue a world after transfer;
- two-participant, TCP, and mesh criteria drive local prediction and convergence;
- selective repair criteria cover equal roots, sparse disagreement, corrupt
  proofs, supersession, relay retention, and keyframe fallback;
- correction ordering criteria cover local replay, queued authority frames, late
  authority frames, and captures racing undispatched input;
- link criteria cover cadence bounds, saturation, recovery, and floor refusal;
- succession criteria cover the designated successor, retention eligibility, term gates,
  roster continuity, and explicit fork fallback.

The criteria live where what they assert lives. `internal/app` drives real
instances over a mesh or a socket, so it owns anything whose subject is a world:
convergence, parity, the compared surface, the roster a crossing creates.
`internal/converge` owns the protocol itself — page proofs, supersession, the
relay role, the term gate, the succession rule — against a stub world that answers
with the capture it was handed. That split is why the protocol exports no knob for
fabricating its own state: a criterion that needs one is in the package that holds
it.

Regenerating the tables and the repository gates are in
[Development](development.md) §3 and §5.

Manual host/guest acceptance should use different terminal sizes and rapid input
from both participants. After a correction, a remote cursor must not walk backward
through older absolute positions. Resize must not move the map, host reset must
preserve the roster, guest reset must be refused, disconnect must remove only the
departing cursor, and reconnect must take a current world.

## 7. Current limits and next work

| Area | Current limit |
|---|---|
| Trust | Transport, participant claims and handoff records are unauthenticated and plaintext. Structural checks prevent races, not hostility. |
| Guest replay | Suffix membership uses the capture's per-source fence, so a frame that missed the playout lead is replayed rather than discarded. Remote entries are a maximum rather than a contiguous prefix: on a relay a frame that overtakes a lower one can leave the lower one looking contained for one cadence. |
| Playout | Each participant stamps the lead its own link to the authority asks for, capped at `NetworkBarrierMaxDelayTicks` (multi-player.md §3.5). A crossing that misses it still lands later on the authority than on its producer, and one past `NetworkCommitLateTicks` is void everywhere but its producer until a correction repairs it. |
| Topology | The protocol relays over a graph. `-join` dials one address, and in a migrate session every participant then dials the current successor from the published succession chain, so the shape is a star with a successor chain over it. |
| Partition | The **first survivor in the succession chain**, else the lowest independent roster identity, succeeds without a vote; losing that successor once it holds the term runs the next succession, but losing it together with the authority elects nobody, because the roster still names it, and the survivors fork. A survivor with no link walks the succession list, and one that hears no record within the succession window forks. Merging an explicit local fork does not work. An instance with no link left drops the participants it can no longer reach; one that still holds links keeps them, because it has peers to agree a tick with and no authority to name one. |
| Relay scheduling | A relayed participant inherits its neighbour's cadence and repair pricing. |
| Progression | `kills.drain` is a session total, so quasar progression is session-wide rather than per cursor. |
| Presentation | Small terminals clip the map; remote cursor presentation has no interpolation beyond receive scheduling. |
| Portability | `float64` determinism is a same-build guarantee, not arbitrary cross-platform bit-exact lockstep. |

Domain-boundary debt is named rather than merely present:

- a shared system pushing a Local-class event is a per-instance effect (D-6) and is
  not checked; the reverse, a player-domain push of a replicated event, must name its
  crossing in `crossingPushes` or `TestEventClassMatchesSystemProfile` fails;
- splitting mixed combat telemetry so Shared results compare directly is what
  remains, and it is a telemetry redesign rather than a boundary fix;
- tower and per-cursor progression ownership still have to be defined together.

Authentication is the next security-shaped change. Adaptive playout and
presentation interpolation must remain outside Shared simulation decisions.
