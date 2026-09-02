# Multiplayer strategy after Phase 6

This is the current multiplayer plan. It replaces the former phase diary, incident
log and answered-question backlog. The implemented domain rules remain in
[Multi-instance domain model](domain-design.md), and the protocol as built is in
its §6; this file is what the phases were for and what is left.

## 1. Vocabulary and boundaries

Use these terms consistently:

- A **network peer** is an endpoint exchanging session frames. A peer may currently
  be a game host or game guest. Relay peers and richer topologies remain compatible
  future possibilities, not current implementation scope.
- The **game host** owns the canonical Shared-domain world, session identity,
  roster and correction stream. It is participant 1 in the current protocol.
- A **game guest** simulates the Shared domain as a predictor and adopts host
  corrections. A guest is not a thin renderer.
- The **Shared domain** is the world all participants can observe: shared species,
  cursors, gold, walls, towers, gateways, map state, shared FSM state and time.
- The **Player domain** belongs to one local participant: corpus glyphs, weapons,
  projectiles, drains, nuggets and visual effects. It is never reconstructed on
  another instance.

Network role, simulation domain and roster ownership are orthogonal. Do not encode
"host" or "guest" into entity domains, and do not assume a network topology from a
player slot. This leaves room for relay peers or future player-domain groupings
without implementing either now.

## 2. Current contract

Phases 1–6 are implemented.

| Area | Current behavior |
|---|---|
| Local input | The producing peer applies ordinary crossings immediately. Remote peers retain the receive-side playout lead. |
| Gold typing | A correctly typed shared gold member is destroyed on the producing peer before the next frame or tick; the destruction still crosses and is corrected authoritatively. |
| Shared authority | The host's Shared world is canonical. Guests run the same deterministic simulation between corrections and may differ provisionally. |
| Player state | Each instance simulates only its local Player domain. D-13 cursor values have exactly one owner and travel as values; a capture carries them so a joiner can materialise a cursor, and a receiver keeps its own over any it authors. No component of a shared entity names a player-domain entity. |
| Corrections | A correction leads with a versioned hash index over the shared capture. Equal roots end it with no state at all; a mismatch descends to pages and repairs only those. Whole keyframes and exact deltas remain the bounded fallback, in the same compressed envelope and the same chunk transport. |
| Local replay | Each instance retains a bounded suffix of its own accepted crossings and replays the ones produced after a correction's baseline, so its own actions survive a correction that predates them. |
| Join/reconnect | A running solo game may use `:host <addr>`; join and reconnect install a current capture through the same staging path. |
| Cadence | Each directly linked peer gets a bounded plan derived from measured link state, priced from the bytes the selective protocol actually sends. The convergence floor remains non-negotiable. |
| Host loss | A guest continues its own game locally from the last authoritative state and shows persistent `HOST LOST:LOCAL` status. There is no coordinated election or state migration yet. |
| Trust | The current session is for trusted peers. Authentication is explicitly deferred. |

The central architectural choice remains: **guests keep simulating**. Determinism is
used to fill the time between corrections, reduce transport demand and preserve
responsive terminal gameplay on constrained systems. Correction magnitude is an
instrument (`snapshot.correction_entities` and related cells), not a reason to
turn guests into thin clients.

Phase 6 made that choice pay directly. A guest that predicted correctly proves it
with a hash and is sent no state; the bytes a session spends are now a function of
what the two instances actually disagree about rather than of how large the world
is.

## 3. Post-Phase 6 operating point

`TestSnapshotCostAtTheStormHighWater`, `TestCorrectionCostAtTheStormHighWater` and
`TestSelectiveCorrectionCostAtTheStormHighWater` measure the real capture, index,
diff, wire codec and install paths. Representative results on the reference
machine, at the storm high water:

| Measurement | Quiet | Storm high water |
|---|---:|---:|
| Shared placements | 12 | ~480 |
| Plain JSON capture | about 11 KiB | about 172 KiB |
| Compressed capture/keyframe | 3.4 KiB | 15.4 KiB |
| Compressed delta one cadence later | — | 7.1 KiB |
| Correction manifest (58 sections) | 1.4 KiB | 1.4 KiB |
| Converged exchange (index out, ack back) | 1.5 KiB | 1.5 KiB |
| One repaired page | — | under 0.3 KiB |
| World read under lock | about 1 ms | about 1 ms |
| Index and hash outside lock | below 1 ms | about 2 ms |

The manifest is a fixed cost: it is one summary per section, and the section list
is a property of the build rather than of the population. That is what makes the
steady state cheap in a busy world as well as a quiet one.

At one keyframe per ten corrections the storm stream is now approximately:

| Cadence | Phase 4 (uncompressed) | Phase 5 (compressed) | Phase 6 (converged) |
|---|---:|---:|---:|
| 5 Hz | 216 KiB/s | 39.6 KiB/s | **14.2 KiB/s** |
| 2 Hz | 86 KiB/s | 15.8 KiB/s | **5.7 KiB/s** |

Four properties are worth stating because they are what the design buys:

1. Capture stays a bounded read under the world lock. Partitioning, marshalling,
   hashing, diffing, proof work and compression are all outside it, and
   `snapshot.hash_us` beside `snapshot.capture_us` is what says so.
2. A repair is never worse than the thing it replaces. Past the measured keyframe
   size the host sends the whole world instead, so the selective path cannot cost
   more than the Phase 5 stream.
3. Every refusal ends at a keyframe the host was going to send anyway, so the
   convergence floor and the maximum repair age are exactly what Phase 5 left.
4. The remaining floor is the *disagreement*, not the protocol. In a storm where a
   guest's prediction has genuinely diverged, the repair is proportional to the
   divergence — which is the honest number, and the one the correction magnitude
   was already reporting.

## 4. Decisions carried forward

### 4.1 Shared gold must disappear immediately

This is gameplay-critical, not optional polish. Gold is a shared composite, and a
correct local keystroke publishes `EventCompositeMemberDestroyed` immediately on
the producing peer. `CompositeSystem` tombstones and destroys that member during
the same settle; the transported copy reaches other peers on their receive-side
schedule. `TestTypedGoldMembersDisappearWithoutATick` prevents a return to visual
latency, and `TestAGoldSequenceSurvivesACorrectionWithoutATick` now also prevents a
correction from putting a typed run back on the screen.

### 4.2 Bandwidth is proportional to disagreement

The deflate envelope was Phase 5's safe reduction; the hash index is Phase 6's
structural one. What a correction does now:

- send a compact root and section summary first;
- when the roots match, send no state body and record a hash-only correction;
- when they differ, descend through the mismatching sections' page hashes and send
  only those pages;
- retain infrequent compressed keyframes as the bounded recovery fallback.

Blind striping was rejected and remains so: it lowers the instantaneous rate but
does not prove which state converged, can repair an important mismatch several
cadences late, and complicates supersession. Hashes select content; they never
substitute for sending the content that differs.

Two exclusions keep the index honest about D-13, and both were failure modes
before they were rules. A cursor's owner-authored cells are hashed only when the
authority owns that cursor, and the control assignment every instance re-derives
at install is zeroed for hashing and repair alike. Either one hashed would leave a
root disagreement no shard could close, and the protocol would fall back to a
whole world every correction for the life of the session.

### 4.3 Host loss means explicit local continuation

When a guest loses the host, its scheduler and simulation continue. The last
authoritative Shared world plus subsequent local prediction become that guest's
independent local game. A persistent status-bar badge and `:session` text make the
fork unmistakable.

This is **not** coordinated host migration:

- guests separated from the host can diverge from each other;
- the old roster has no new authority;
- the local continuation cannot accept a guest into the old session identity;
- no election, relay promotion or in-flight admission transfer is implied.

Coordinated migration remains future work and must transfer authoritative state and
membership before electing a successor. The current local-continuation behavior is
useful and honest without that machinery.

### 4.4 Simulation and terminology remain extensible

Game guests continue simulating both the replicated Shared predictor and their own
Player domain. Keep role checks at authority/session seams, domain checks at
simulation ownership seams and participant IDs at roster seams. Relay roles,
nested player domains and hierarchy remain unimplemented; the selective exchange is
deliberately shaped so a relay that could answer for the participants behind it
would be a *role* with retention rather than a change of topology.

### 4.5 Tower ownership is deferred with a minimal rule

The optional `config/td` and `config/main/tower.toml` paths currently derive tower
ownership from `player_entity`, normally slot 0. Do not interleave shared towers
with cursor roster slots: slots identify participant cursors.

When tower gameplay is settled, use the existing zero entity as the documented
sentinel for a session-owned/uncredited tower; a nonzero entity means explicitly
cursor-owned. Teach `TowerSystem` that rule and remove the accidental slot-zero
binding from the two config regions and gateways. Do this in a later phase, with no
new ownership type or configuration variable unless the sentinel proves
insufficient.

### 4.6 Authentication is deferred

A peer can lie in its scheduling hints and ask the host to publish sooner, but it
cannot make that hinted state authoritative. It can also ask for a repair or a
keyframe, and the host will read its world to answer — which is a cost a hostile
peer could raise and a trusted one cannot. Existing cadence, floor and retention
bounds limit it; authentication is what would remove it, and it is still deferred.

### 4.7 A solo run's later lobby cap is explicit

`-players N` may be supplied on a solo run and is inherited by a later
`:host <addr>`. If the run did not specify `-players`, mid-run hosting uses
`parameter.MaxPlayers`. A startup `-host` with no explicit count keeps its
two-participant default. `-players` is rejected for `-join`.

## 5. What a level transition taught the correction path

Two defects reached from the same place — an FSM region's entry actions at the
tower transition — and both are worth recording because they are shapes rather
than incidents.

A species declaration is *shared* (both instances derive it from the same FSM) but
its effect is *private* (a registry outside every component store). A guest
predicts the transition, so the two instances register at different ticks, and a
carrier that required the registered sets to match exactly refused every
correction from then on. Declared private state (D-19) has to be adoptable in both
directions, not merely comparable: the declarations now travel in the capture
beside the populations, and an install reconciles the set.

The refusal also arrived too late to be safe. A carrier is loaded after the store
pass, so a rejection left a world that was neither the receiver's nor the
authority's — and the staging pass could not catch this one, because a staging
world has never entered a level region and therefore accepted exactly what the
live world refused. A carrier whose acceptance depends on live state now answers
`CheckShared` before anything is written.

The general rule, for the next carrier that grows this shape: a staging world
answers "can this build load this", not "can this instance load this". Anything
that depends on where the instance has *been* has to be asked of the instance.

## 6. Non-goals that remain non-goals

- cryptographic authentication or TLS;
- coordinated host election/migration or partition merging;
- relay-peer roles, multi-link CLI or nested Player domains;
- tower ownership/config changes;
- adaptive playout lead;
- replacing deterministic guest simulation with thin clients.

These are explicit scope boundaries, not forgotten questions.

## 7. Known behaviour worth naming

Two things the current protocol does that a reader should not mistake for defects,
and one that is genuinely open.

**A relayed session keeps the Phase 5 stream.** The selective exchange is between
an authority and a receiver that can answer it, and only a directly linked
participant can: a relayed one receives the flood but its request would go to the
neighbour that forwarded it, which holds no retention and no authority. Rather
than add a routing layer this protocol deliberately does not have, a session with
any participant behind a relay publishes whole bodies throughout.

**Hash-only convergence needs tick alignment.** The clock-derived half of the
compared surface — a region's time in state, a gold deadline, the elapsed
counters — moves every tick, so a receiver comparing a world one tick past the one
the manifest describes disagrees about it. The correction exchange is therefore
drained and answered *between* two ticks rather than as part of one
(`DrainOffTick`), which makes the comparison tick-aligned in the ordinary case. A
receiver that is genuinely behind still repairs; it simply repairs more.

**A shared region's per-instance effects name the cursor they belong to.** A
region is session-wide by construction: every instance runs the same machine and
enters the same state, so an effect its actions raise reaches every participant
whether or not the thing that caused it was theirs. That is right for a storm,
which is one encounter every participant is inside, and wrong for a quasar, which
is fused from *one* cursor's drains — before the scope, one player's quasar
darkened every player's screen and stopped every player's drains.

`EventGrayoutStart`, `EventGrayoutEnd`, `EventDrainPause` and `EventDrainResume`
therefore carry `CursorScopePayload`, a single cursor entity. Entity zero is the
session-wide form and is what an `EmitEvent` action with no payload table already
produces — `compileEmitEvent` allocates the registered prototype — so the storm,
the tower defence and the reset paths keep their existing behaviour with no
configuration change. The quasar's actions inject `fuse_owner`, the variable the
fuse request beside them already elects by, and an instance that does not simulate
that cursor ignores them: the same `ResolveOwnedCursor` admission the D-13
owner-authored set uses.

Because the two shapes overlap — a quasar inside a storm — `DrainSystem` holds a
set of reasons rather than a flag: a session-wide hold plus one per owning cursor,
bounded by the roster, published as `drain.paused` for the cursor this instance
drives. A resume naming no cursor clears every hold, which is what a terminating
region and a reset both emit; an overflow of the bound falls back to the pause
rather than dropping a hold nothing would release.

The shared half is untouched, which is the point: the region is still spawned by
one shared decision, the fusion is still elected by D-16 to exactly one causal
cursor, and `EventQuasarSpawnRequest` still crosses once. Only the two standing
per-instance effects changed. What remains session-wide is the strobe — a 200 ms
flash rather than a standing state, and scoping it needs a field on a payload it
shares with other callers — and the region itself, one quasar at a time, because a
per-cursor region would mean nested player-domain state machines, which §6 rules
out.

## 8. Verification gates

Phase 6 is proved by:

- a healthy equal guest receives hash metadata but no state body;
- one injected store/page mismatch repairs only that shard and restores the root;
- several mismatches in different sections repair without an unrelated section
  travelling;
- a missing or corrupt proof is refused and reaches a bounded keyframe fallback;
- selective apply never changes Player-domain state, nor the D-13 owner-authored
  set of a cursor the receiver authors, and a persistent hash disagreement over
  those cells does not degrade into a repeated keyframe fallback;
- loss, duplication and supersession cannot assemble mixed-baseline state;
- rapid local cursor motion and a full gold sequence remain tick-free locally, and
  survive a correction that predates them exactly once;
- an unavailable replay suffix falls back to the authority and says so;
- link pacing and admission are priced from measured manifest and shard bytes;
- host loss leaves the guest ticking with persistent `HOST LOST:LOCAL` status;
- the storm report publishes actual compressed, hash and shard wire costs.

Run the repository's full test and generation gates after focused tests:

```sh
go generate ./internal/event ./internal/manifest
go test ./...
go vet ./...
```

The two-process acceptance is `script/phase6-selective.sh`, which runs the
checked-in host/guest script pair over a real socket, applies the same `tc netem`
stages as Phase 5 when the machine has `tc` and root, and reads the index, repair
and replay counters out of both logs.

Keep performance measurements reported rather than tied to wall-time assertions;
assert correctness, bounded allocation and meaningful byte reduction.
