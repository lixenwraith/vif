# Multiplayer strategy after Phase 5

This is the current multiplayer plan. It replaces the former phase diary, incident
log and answered-question backlog. The implemented domain rules remain in
[Multi-instance domain model](domain-design.md); the next implementation hand-off
is [Phase 6 implementation prompt](phase6-implementation-prompt.md).

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

Phases 1–5 are implemented.

| Area | Current behavior |
|---|---|
| Local input | The producing peer applies ordinary crossings immediately. Remote peers retain the receive-side playout lead. |
| Gold typing | A correctly typed shared gold member is destroyed on the producing peer before the next frame or tick; the destruction still crosses and is corrected authoritatively. |
| Shared authority | The host's Shared world is canonical. Guests run the same deterministic simulation between corrections and may differ provisionally. |
| Player state | Each instance simulates only its local Player domain. D-13 cursor values have exactly one owner and travel as values; a capture carries them so a joiner can materialise a cursor, and a receiver keeps its own over any it authors. No component of a shared entity names a player-domain entity. |
| Corrections | Whole keyframes and exact deltas use a versioned, bounded deflate envelope, then the existing chunk transport. |
| Join/reconnect | A running solo game may use `:host <addr>`; join and reconnect install a current capture through the same staging path. |
| Cadence | Each directly linked peer gets a bounded plan derived from measured link state. The convergence floor remains non-negotiable. |
| Host loss | A guest continues its own game locally from the last authoritative state and shows persistent `HOST LOST:LOCAL` status. There is no coordinated election or state migration yet. |
| Trust | The current session is for trusted peers. Authentication is explicitly deferred. |

The central architectural choice remains: **guests keep simulating**. Determinism is
used to fill the time between corrections, reduce transport demand and preserve
responsive terminal gameplay on constrained systems. Correction magnitude is an
instrument (`snapshot.correction_entities` and related cells), not a reason to
turn guests into thin clients.

## 3. Post-Phase 5 operating point

`TestSnapshotCostAtTheStormHighWater` and
`TestCorrectionCostAtTheStormHighWater` measure the real capture, diff, wire codec
and install paths. Representative results on the reference machine are:

| Measurement | Quiet | Storm high water |
|---|---:|---:|
| Shared placements | 12 | 492 |
| Plain JSON capture | about 11 KiB | about 172 KiB |
| Compressed capture/keyframe | 3.4 KiB | 15.4 KiB |
| Compressed delta one cadence later | — | 7.1 KiB |
| World read under lock | about 1 ms | about 1 ms |
| Compression outside lock | below 0.3 ms | about 1.1 ms |

At one keyframe per ten corrections, the storm stream is now approximately:

| Cadence | Before compression | Current compressed wire |
|---|---:|---:|
| 5 Hz | 216 KiB/s | **39.6 KiB/s** |
| 2 Hz | 86 KiB/s | **15.8 KiB/s** |

Full compressed keyframes would be about 76.9 KiB/s at 5 Hz and 30.8 KiB/s at
2 Hz. The important properties are:

1. Compression happens after capture and diff, outside the world lock.
2. The envelope declares and enforces the uncompressed size, capped by
   `network.MaxSnapshotBytes`, so decompression cannot become an unbounded
   allocation.
3. Admission, cadence pricing and telemetry see compressed bytes because the
   encoding happens before chunking.
4. Compression is a large immediate reduction, but a continuous 40 KiB/s is still
   too much for some target terminals and links. Phase 6 must make healthy traffic
   proportional to disagreement rather than continuously carrying the whole
   correction surface.

## 4. Decisions carried forward

### 4.1 Shared gold must disappear immediately

This is gameplay-critical, not optional polish. Gold is a shared composite, and a
correct local keystroke publishes `EventCompositeMemberDestroyed` immediately on
the producing peer. `CompositeSystem` tombstones and destroys that member during
the same settle; the transported copy reaches other peers on their receive-side
schedule. `TestTypedGoldMembersDisappearWithoutATick` prevents a return to visual
latency.

### 4.2 Bandwidth reduction continues in Phase 6

The deflate envelope is the safe pre-Phase 6 improvement. Phase 6 should add
hash-guided selective repair:

- send a compact root/version summary first;
- when roots match, send no state body;
- when they differ, descend through stable section/page hashes and send only the
  mismatching shards;
- retain infrequent compressed keyframes as a bounded recovery fallback.

Blindly sending one fifth of a payload per correction is not the primary design:
it lowers the instantaneous rate but does not prove which state converged, can
repair an important mismatch several cadences late, and complicates supersession.
A rotating stripe may be considered only if it is paired with an integrity and
coverage contract. Hashes are evidence for selecting content, not a substitute
for sending mismatching content.

Selective repair must preserve D-23's exactness. Each shard needs stable identity,
baseline/version information and its own integrity proof. Applying a shard must not
adopt a host tick for unrelated state or assemble one logical object from
incompatible baselines.

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
simulation ownership seams and participant IDs at roster seams. Do not introduce
relay roles, nested player domains or hierarchy in Phase 6; only avoid designs that
make them impossible.

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
cannot make that hinted state authoritative. Existing cadence and floor bounds
limit the cost. Keep counters and validation; do not make TLS or authentication a
Phase 6 prerequisite while functional correction and bandwidth work remains.

### 4.7 A solo run's later lobby cap is now explicit

`-players N` may be supplied on a solo run and is inherited by a later
`:host <addr>`. If the run did not specify `-players`, mid-run hosting uses
`parameter.MaxPlayers`. A startup `-host` with no explicit count keeps its
two-participant default. `-players` is rejected for `-join`.

## 5. Phase 6 strategy

Phase 6 has two coupled deliverables, ordered by user-visible cost.

### 5.1 Content-addressed selective correction

Build a deterministic correction index over stable capture sections and bounded
pages. The index must exclude Player-domain state exactly as captures do, and must
handle the D-13 owner-authored set the way an install does rather than the way its
absence was once assumed: a capture *carries* those components, because a joiner
has to materialise a cursor it has never held, and the receiver keeps its own
values for the cursors it authors instead of adopting the sender's mirror
(`snapshot_roster.go`). A page hash computed over a receiver-authored cursor will
therefore mismatch forever, and a selective repair that "fixed" it would undo the
owner. Either exclude those cells from the hashed surface or make the repair a
no-op for them; do not let a root disagreement that no shard can close drive an
endless keyframe fallback. A peer compares the host root with its matching
baseline:

1. Equal root: record convergence and send no correction body.
2. Different root: exchange the minimum child hashes needed to identify mismatched
   pages.
3. Send compressed page replacements/deltas carrying schema, capture/baseline tick,
   page identity and proof.
4. Reconcile only those pages, then verify the new root.
5. On an unavailable baseline, proof failure, loss or bounded age, request/accept a
   compressed keyframe.

Prefer generated metadata from the existing snapshot manifest over a second hand
maintained store list. Keep hashing, diffing, compression and proof validation
outside the world lock. Capture under the lock remains a bounded read.

Required telemetry includes manifest bytes, hash-only corrections, requested and
applied shard bytes, repaired pages/entities, proof failures, keyframe fallbacks and
wire bytes per peer. The storm cost tests should report both schema and wire sizes.

### 5.2 Bounded rollback and replay

A guest currently installs a correction at the host's earlier capture tick. Phase 6
must retain the canonical suffix of that guest's own accepted local crossings,
install the authority, and replay only the suffix after the correction baseline to
the former predicted present. Requirements:

- one canonical representation, shared with journal/wire encoding where practical;
- strict bounds by tick, count and bytes;
- deterministic ordering and deduplication;
- no replay of remote or Shared-derived events;
- no duplicate rewards, entity creation or owner-state application;
- a clean fallback to ordinary correction when the suffix is unavailable.

Gold removal and cursor response must remain immediate while replay is added.

## 6. Phase 6 non-goals

- cryptographic authentication or TLS;
- coordinated host election/migration or partition merging;
- relay-peer roles, multi-link CLI or nested Player domains;
- tower ownership/config changes;
- adaptive playout lead;
- replacing deterministic guest simulation with thin clients.

These are explicit scope boundaries, not forgotten questions.

## 7. Verification gates

At minimum, Phase 6 must prove:

- a healthy equal guest receives hash metadata but no state body;
- one injected store/page mismatch repairs only that shard and restores the root;
- a missing or corrupt proof is refused and reaches a bounded keyframe fallback;
- selective apply never changes Player-domain state, nor the D-13 owner-authored
  set of a cursor the receiver authors, and a persistent hash disagreement over
  those cells does not degrade into a repeated keyframe fallback;
- loss, duplication and supersession cannot assemble mixed-baseline state;
- rapid local cursor motion and a full gold sequence remain tick-free locally;
- replay preserves local actions issued after the authority's baseline exactly once;
- two processes converge under the existing shaped-link scripts;
- host loss leaves the guest ticking with persistent `HOST LOST:LOCAL` status;
- the storm report publishes actual compressed/hash/shard wire costs.

Run the repository's full test and generation gates after focused tests:

```sh
go generate ./internal/event ./internal/manifest
go test ./...
go vet ./...
```

Keep performance measurements reported rather than tied to wall-time assertions;
assert correctness, bounded allocation and meaningful byte reduction.
