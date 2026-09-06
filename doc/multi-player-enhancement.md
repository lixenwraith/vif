# Multiplayer architecture and remaining work

This document is the operational summary of multiplayer as it exists now. The
domain invariants and their implementation details live in
[Multi-instance domain model](domain-design.md); this document explains the
runtime contract, the correction/order boundaries, the current operating point,
and the work that remains.

## 1. Vocabulary and boundaries

Use these terms consistently:

- A **network peer** is an endpoint exchanging session frames.
- The **game host** owns the canonical Shared-domain world, session identity,
  roster, and correction stream. It is the participant currently authoring,
  which is participant 1 until a handoff moves authorship.
- A **game guest** runs the same Shared simulation as a predictor and adopts host
  corrections. It is not a thin renderer.
- A **relay** forwards authoritative artifacts for participants behind it and
  retains authoritative captures so it can answer their selective repair
  requests. It authors nothing.
- The **authority term** is a monotonically increasing session generation.
  Authoritative artifacts carry it; a term advances once per successful handoff
  and never moves backwards on an instance.
- The **Shared domain** contains state every participant can observe: cursors,
  shared species, gold, walls, towers, gateways, map state, shared FSM state, and
  simulation time.
- The **Player domain** belongs to one local participant: corpus glyphs, weapons,
  projectiles, drains, nuggets, and visual effects. It is never reconstructed on
  another instance.

Network role, simulation domain, roster ownership, topology, and authority term
are orthogonal. Do not infer authorship from participant ID, infer topology from a
roster slot, or encode host/guest roles in entity domains.

## 2. Current contract

| Area | Current behaviour |
|---|---|
| Local input | The producer applies ordinary crossings immediately. Remote copies retain the receive-side playout lead. |
| Shared authority | The host's Shared world is canonical. A guest's predicted result is provisional until the next correction. |
| Player state | Each instance simulates only its Player domain. Owner-authored cursor values have one writer and travel as values; a receiver keeps the values it authors across an install. |
| Corrections | A correction starts with a versioned hash index. Equal roots send no state. Mismatches descend to independently proved pages. Compressed whole keyframes remain the bounded fallback. |
| Local replay | A guest retains a bounded canonical suffix of its own accepted crossings and replays the portion later than the installed authority baseline. |
| Crossing ordering | Snapshot schema 5 carries one applied-sequence fence per participant. A receiver removes ordinary frames the installed world already holds — including ones whose nominal receive tick is still ahead — and keeps the ones it does not, including ones whose receive tick is long past. |
| Local FSM lifecycle | A live install replays only config-marked persistent `ClassLocal` exit/entry events for crossed state paths; staging and all ordinary actions remain side-effect free. |
| Join and reconnect | A running game can begin hosting; join and reconnect install a current capture through the same staging path. Every host arms the same mid-run gate once its own lobby is done, so a reconnect takes one path whether the session started with `-host`, `-serve`, a script, or `:host`. |
| Roster | A participant holds an identity, a term and a vote; a roster slot binds it to a cursor. The coordinator of a dedicated host holds no slot, so a session can consist entirely of its guests. |
| Cadence | Each direct link gets a bounded correction plan derived from round-trip time, variation, delivered bytes, saturation, and correction demand. The whole-world convergence floor is fixed. |
| Mesh and relay | Epochs, owner state, corrections, and authority records flood with per-source duplicate suppression. A relay with retained authority content keeps selective repair available to participants behind it. |
| Host loss | A reachable majority can elect an eligible retained successor under the next term. A component without a majority continues as an explicit local fork and does not merge later. |
| Trust | Links are plaintext and unauthenticated by decision. What the coordinator *does* check is identity: a joiner reports its protocol, simulation fingerprint, capture and journal schemas, tick interval, seed, configuration and corpus, and a peer that does not match the offer is refused before it takes a roster slot. |
| Allocated lifetime | A dedicated host may bound its own life: a first-guest window, an empty-roster grace, and a drain a termination signal opens. Draining and expired sessions refuse a dial with `ErrSessionEnding`, distinct from the retryable `ErrSessionStarting`. See [Runtime](runtime.md) §1.2. |

The central choice is that guests keep simulating. Determinism fills time between
corrections, makes a converged exchange hash-only, and preserves responsive local
terminal input. Correction magnitude measures prediction distance; it is not a
request to turn the guest into a renderer.

## 3. Event timing and correction containment

### 3.1 Ordinary and barrier-bound crossings

Every wire frame carries four ordering values:

- `Source`: the participant that produced it;
- `ProducedTick`: the source epoch that batched it;
- `ApplyTick`: the absolute receive-side simulation tick;
- `Seq`: the source-local order within the crossing stream.

An ordinary crossing has two application times by design. Its local copy is
published immediately; remote copies wait until `ApplyTick` and are ordered by
`(ApplyTick, Source, Seq)`. This removes the playout lead from the player who
generated the input while keeping a receive buffer for reordered remote traffic.

Participant arrival, participant departure, and full reset are `barrierBound`.
They create or destroy shared identity, so their producer also waits for the
agreed apply tick. A correction must not repair divergent entity allocation or run
numbering.

### 3.2 The capture boundary

`ApplyTick` does not say whether a capture contains a given ordinary frame, for
either side of the session, and the reason is the same in both directions: an
ordinary crossing applies immediately on its producer and a playout lead later
everywhere else, so the tick a copy was *scheduled* to run at is not the tick the
world took it in.

That splits two ways:

- The authority applies its own frame first, so a capture at tick T can already
  contain one whose remote `ApplyTick` is T+1, T+2 or T+3.
- A guest whose link misses the lead produces a frame for T+3 that has not reached
  the authority when it reads its world at T+9, so the capture is missing one whose
  `ApplyTick` is already six ticks old.

Snapshot schema 5 therefore records `CaptureHeader.Crossings`: one fence per
participant, `{source, seq}`, naming the source-local sequence through which this
world contains that participant's ordinary crossings. The local entry is the
contiguous prefix its own dispatchers have completed — the event queue returns each
sequence to `NetworkSystem` only after every local handler has run — and each remote
entry is the highest this instance has applied from that source. The capture body,
tick, map bounds and the whole fence vector are read under one world lock.

An install classifies queued and later-arriving frames as follows:

| Frame | Already represented by the capture when |
|---|---|
| Ordinary frame, any source | `frame.Seq <= Header.Crossings.Seq(source)` |
| Ordinary frame whose source the header does not name | never — nothing is claimed about it |
| Barrier-bound frame, any source | `frame.ApplyTick <= Header.Tick` |

The sequence rule is evaluated before the tick rule for ordinary frames, and the
tick rule is the whole rule for barrier-bound ones: an arrival, a departure and a
reset apply at one agreed tick on every instance including their producer, so the
tick is exact for them and nothing else is needed.

This closes both rollback patterns with one boundary. The host-cursor pattern —
a correction installs a new host position and queued older absolute positions then
walk the guest backward — is closed because an already-applied frame is discarded
even when its receive deadline is still in the future. The guest-action pattern —
a correction undoes the player's own keystroke, and the next one puts it back a
cadence later — is closed because a frame the capture never saw is kept even when
its receive deadline is long past. The same fences are retained after installation,
so a stale batch arriving later is classified the same way.

Two deliberate asymmetries in how the fences are computed:

- The **local** entry is a contiguous prefix rather than the highest assigned
  sequence, because local dispatch can complete out of order: a capture racing an
  input that has been encoded but not yet dispatched must not claim it. Completions
  that arrive ahead of a gap are held until the gap closes.
- Each **remote** entry is a maximum rather than a contiguous prefix. Within one
  link a source's frames arrive in order, so the two are the same number in every
  topology the CLI builds; where they could differ — a frame overtaking a lower one
  across a relay — the maximum costs one cadence of a frame looking contained when
  it is not, and the contiguous prefix would instead stall on a frame the receiver
  refused for a full queue and will never see, making that producer replay a growing
  suffix at every correction for the rest of the session. The bounded, self-healing
  failure is the one worth having.

### 3.3 Guest replay

A correction may describe a host tick behind the guest's predicted present. The
guest retains its own ordinary crossings in their encoded wire representation and
replays those past the capture's fence for its own source. Production ticks bound
retention age; they do not choose replay membership, and neither does the apply
tick. Arrival, departure, and reset are never replayed.

The suffix is bounded by ticks, records, and encoded bytes. If retention has a
hole, the guest installs the authority alone and reports the skipped replay rather
than guessing at a partial history.

Consecutive simulation events are not coalesced on the wire. Two cursor placements
may consume different glyphs or cause different collision and progression effects;
discarding the intermediate event would change gameplay. Batching, selective
state repair, and presentation work are the safe optimisation layers.

## 4. Correction pipeline

The host publishes one authoritative capture on the session timeline, then serves
each direct peer according to that link's cadence:

1. Build a deterministic manifest over component stores, allocator values, RNG
   streams, declared system state, status surface, and shared FSM state.
2. Send the root and section summaries.
3. If the receiver produces the same root, acknowledge a hash-only correction and
   install the authority header without transferring state.
4. Otherwise compare page hashes only in differing sections and return the pages
   that differ.
5. Validate every page hash, reconstruct the authority root, reconcile through a
   reusable staging world, and commit between ticks.
6. Refuse stale, foreign, malformed, or unverifiable repairs and recover at the
   next compressed keyframe.

Owner-authored cursor cells are excluded when the receiver owns that cursor, and
cursor control assignment is normalised for both hashing and repair. Those values
are re-bound locally after installation.

The manifest root intentionally excludes tick-local header metadata so a predictor
can compare state with an earlier authority tick. A shard set must nevertheless
match the manifest's authority term, participant, and crossing fence; this binds
the ordering metadata used to prune queued events. Its capture integrity may
differ on a relay whose equal canonical state has another dense-store order.

Nothing acknowledges or retransmits an ordinary correction. A newer keyframe
supersedes older state, so loss costs freshness rather than permanent correctness.

## 5. Membership, topology, and authority continuity

Roster changes are shared artifacts produced by the coordinator. A direct
neighbour reports a disconnect; the authority turns it into one departure at one
apply tick. A mid-run join is admitted to transport before its capture is read, so
traffic produced during transfer is buffered rather than lost. The join waits for
a capture far enough past admission to include pre-admission epochs and refuses an
unbounded catch-up gap.

The session protocol supports a mesh even though the shipped CLI normally builds
a star. Each source epoch is admitted once within a bounded replay window and
forwarded to every neighbour except the arrival edge. Corrections retain the
authority's term, tick, hashes, and chunks across relays.

If the authority disappears, survivors exchange reachability and retention
reports, vote once for the lowest eligible current candidate, and adopt one
majority-backed handoff record. Eligibility requires both a reachable majority and
retention as current as the newest survivor reports. The handoff carries roster,
slot assignments, session anchor, and barrier delay. The successor seeds its
baseline from retained authoritative state, avoiding a keyframe fan-out when the
survivors already agree.

A partition without a strict majority elects nobody. Its members continue locally
with `network.fork` and persistent `HOST LOST:LOCAL`; encountering a higher term
later is refused because partition merging is not implemented. A fork left with no
link at all — every session the CLI's star builds — drops the participants it can
no longer reach as it forks: the authority that went, and behind it the guests that
were only ever reachable through it. That removal is local rather than a crossing,
and being alone is what makes it exact: a departure is produced once at one agreed
tick because two instances must destroy the same shared entity together, and there
is no second instance. A fork that still holds links keeps its roster, which is
part of gap 4 below.

A departure, however it is produced, also clears what the instance had applied from
that participant. Identities return to the pool and a crossing sequence starts at
one, so a fence kept from the previous holder would claim a capture already contains
crossings the next holder of that identity has not produced — and §3.2's install
rule would then discard exactly those.

## 6. Current operating point

Representative measurements at the storm high-water fixture are:

| Measurement | Current value |
|---|---:|
| Plain JSON capture | about 172 KiB |
| Compressed keyframe | about 15.4 KiB |
| Compressed delta one cadence later | about 7.1 KiB |
| Manifest over 58 sections | about 1.4 KiB |
| Converged exchange | about 1.5 KiB |
| One repaired page | under 0.3 KiB |
| Capture read under lock | about 1 ms |
| Index and hash outside lock | about 2 ms |

With one keyframe per ten corrections, a converged storm session is about
14.2 KiB/s at 5 Hz or 5.7 KiB/s at 2 Hz. These are observations, not wall-time
acceptance thresholds. Correctness, bounded allocation, and meaningful byte
reduction are the enforced properties.

Capture remains a bounded world-lock read. Indexing, page marshalling, hashing,
diffing, proof work, and compression run after the lock is released. A selective
repair is abandoned when it would be wider than the keyframe it replaces.

## 7. Diagnostics and operations

The useful runtime signals are:

- `snapshot.correction_entries`, `snapshot.correction_entities`, and
  `snapshot.correction_cells`: how far prediction moved when authority arrived;
- `snapshot.replay_records`, `snapshot.replay_skipped`, and
  `snapshot.replay_suffix_unavailable`: whether local predicted work survived;
- `network.artifacts_pre_install`: frames discarded because an installed capture
  already represented them;
- `network.artifacts_authority_superseded`: the subset discarded by the authority
  sequence fence rather than by tick;
- `network.lag_ticks`, `network.stale`, and `network.barrier_late`: whether the
  receive lead is being missed;
- `network.transport_lost_in` and `network.transport_lost_out`: bounded queue
  refusal;
- `snapshot.cadence_*` and `network.link_*`: the selected operating point and the
  measurements behind it;
- `network.term_stale`, `network.term_refused`, `network.fork`, and
  `network.migrations`: authority continuity.

At debug level, `snapshot pruned crossings` records the capture tick, authority,
authority sequence, scheduled drops, sequence-fence drops, and pending local
drops. `snapshot refused stale authority crossings` records batches that arrived
after installation but were already covered by the authority fence.
`import reconciled local lifecycle` reports the number of persistent local
exit/entry actions emitted by an FSM install. It should appear only when a
correction crosses such a boundary or changes a marked action's scope variable.

A healthy run may show a small non-zero correction magnitude; it should not show a
growing correction, persistent lag, repeated suffix unavailability, transport
loss on a healthy local link, or shared actors changing backward after a
correction.

Live pause, speed, step, raw shared mutation, and synchronous diagnostic saves are
refused while peers are attached. They are instance-local operations and cannot
stop or mutate only one copy of a live session.

## 8. Remaining gaps

1. **Authentication and confidentiality — deferred by decision.** Links are
   plaintext and a session is reached by its address alone. Participant claims,
   votes, retention reports and handoff voter lists are structurally checked but
   not authenticated; the rules prevent races, not a hostile peer. The deployed
   fleet accepts this and hardens the open port instead; see the
   [fleet plan](kubernetes-fleet.md) §4 for what bounds a stranger today and what
   does not.
2. **Adaptive playout lead.** The three-tick lead is fixed and not graph-diameter
   aware. Cadence adapts per direct link; apply deadlines do not. It decides how
   often a link misses the lead at all, and therefore how often §3.2's fences have
   work to do — they make a missed lead harmless, not rare.
3. **Topology surface.** The protocol relays over arbitrary graphs, but `-join`
   dials one address, so ordinary CLI sessions still form a star. A relayed peer
   inherits its neighbour's cadence.
4. **Partition merge.** Majority succession is implemented; reconciling an
   explicit local fork back into a higher term is not. A fork with links still
   standing also keeps the participants on the far side of the loss: it has peers
   to agree an apply tick with and no authority to name one, so its roster stays
   as it was. A fork alone does not have that problem and does not have it (§5).
5. **Programmatic operator mutation.** Interactive controls are session-aware;
   embedder-level map and FSM mutations still rely on caller discipline.
6. **Domain-boundary debt.** Remaining ambient-local stamping exemptions,
   `event.EmitDeath`'s direct path, route-anchor casts, and mixed combat telemetry
   should be made explicit or removed.
7. **Tower and progression ownership.** Optional tower configurations still bind
   ownership to the slot-zero cursor, and quasar progression still uses a session
   drain total. Both need an explicit session-owned versus cursor-owned rule.
8. **Presentation.** A small terminal clips the map, and remote cursor motion is
   rendered at simulation arrival ticks. Windowed views and optional presentation
   interpolation are separate from simulation ordering.
9. **Portability.** Determinism is guaranteed within one implementation build,
    not as cross-platform bit-exact lockstep for arbitrary `float64` behaviour.

## 9. Verification

The automated suite covers domain boundaries, deterministic continuation,
two-participant and mesh convergence, selective repair and fallback, replay
retention, correction ordering, join/reconnect, link shaping, relay retention, and
authority succession. It also forces a capture to enter and retire a quasar while
the receiver skips the release transition, and round-trips a delayed transition
action by compiled identity. Run the generation and repository gates after
focused network tests:

```sh
go generate ./internal/event ./internal/manifest
go test ./...
go vet ./...
```

For a manual two-terminal check:

```sh
# terminal 1
./bin/vif -d -host 127.0.0.1:7777

# terminal 2
./bin/vif -join 127.0.0.1:7777
```

One side can be scripted instead, which holds it constant across runs while the
other is played by hand:

```sh
# terminal 1
./bin/vif -script script/sparring-host.toml -host 127.0.0.1:7777 -players 2

# terminal 2
./bin/vif -join 127.0.0.1:7777
```

A scripted participant is an ordinary one: it takes a roster slot, produces
crossings at the agreed apply ticks, and is corrected like any other guest. It is
wall-paced at the game interval so it cannot outrun its peers; `-speed` selects
another rate and `-watch` presents the scripted side on its own terminal. See
[Runtime](runtime.md) §1.1.

Neither side has to be a person. `-serve` runs a dedicated host: the shared world,
the authority, the correction cadence and the roster, with no terminal and no
cursor of its own, so a session can consist entirely of the guests that join it. It
starts on its first guest and admits the rest as they arrive, so `-players` there
is a ceiling and omitting it holds the whole roster.

```sh
./bin/vif -serve :7777 -size 120x40 -l -lv info
```

See [Runtime](runtime.md) §1.2.

Exercise rapid `h`/`l` sequences on both participants across several correction
cadences. A corrected cursor must not subsequently visit an older cell because of
a delayed copy. Also verify typing, gold destruction, combat, reset, disconnect,
and reconnect while watching the diagnostics in §7.

Two membership checks are worth running by hand on every host shape, because they
exercise the paths a two-terminal session reaches and nothing else does:

- The guest leaves with `:q` and dials again. It should install a current capture
  and take back the slot its departure released. While it waits at the start gate
  it is not yet in a session and has no world, but the wait is still leavable:
  Ctrl-Q, Ctrl-C and a terminal resize are answered there.
- The host leaves instead. The guest reports `HOST LOST:LOCAL` and keeps playing,
  and the host's cursor must be gone from its map rather than standing where it
  was left.
