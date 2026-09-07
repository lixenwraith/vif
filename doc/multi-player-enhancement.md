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
| Host loss | `-authority migrate` (default off `-serve`): the lowest surviving identity in the closed roster — the first guest admitted — takes the next term, with no vote, because every survivor computes it from the roster alone. `-authority host` (default on `-serve`): nobody takes it and every survivor continues alone. Either way a successor authors but does not listen, so migration moves authorship and not reachability (§5.0). |
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

### 5.0 Who listens, who authors, who can be reached

Three questions the rest of this section depends on, and they have different
answers:

| Question | Answer |
|---|---|
| Who binds a port? | Exactly one instance: the one started with `-host` or `-serve`, or a solo run that opened itself with `:host`. Nothing else in the protocol ever calls `listen(2)` — `network.NewSocketPort` has two callers, the network service and `beginHostingLocked`, and both run at that instance's own request. |
| Who authors? | The participant holding the current term. It is the coordinator until a handoff moves it, and a handoff moves *authorship only*. |
| How does a guest find a session? | `-join <address>`, typed by the operator. There is no discovery, no rendezvous and no address anywhere in the protocol: an offer carries identity, roster, term and bounds, and a handoff record carries membership. Neither carries a way to reach anybody. |

Everything below follows from the second and third rows disagreeing. **A successor
authors but cannot be dialled.** It does not bind the port its predecessor held —
it is usually on another machine and could not bind that address anyway — and no
guest has ever been told where it is. So in the star every `-join` builds:

- the successor keeps authoring, alone, because no other survivor had a link to it;
- every other survivor waits out the succession window and forks;
- a guest that quits cannot come back: the address it knows belonged to the
  participant that went;
- the outcome is one solo game per survivor, and the only difference the succession
  makes is which of them believes it is hosting one.

Because nothing rebinds, none of the questions a rebinding would raise apply: there
is no `TIME_WAIT` race to back off from, and no window in which the address is
half-released. The cost is the one above instead.

Migration is therefore for the topology the *protocol* supports and the CLI does
not build: a mesh or a relay chain, where survivors already share links, the
handoff reaches them, and the session continues with the participants that could
always reach each other. `-authority host` pins authorship for every session where
that is not the shape — which is the default for `-serve`, where the address *is*
the session and an orchestrator replacing the pod at the same address is the
reconnect its guests actually want. It is also the setting for a deployment where
the world may only ever live on the machine that started it.

### 5.1 Succession

If the authority disappears, the successor is **the lowest surviving identity in
the closed roster**: the first guest the coordinator admitted, because identities
are handed out lowest-free-first in arrival order. It is a pure function of the
roster and the participant that went, both of which every survivor already holds,
so every survivor names the same successor without exchanging anything.

That determinism *is* the split-brain rule. At most one instance can conclude that
it is the successor, so at most one can ever claim the term — which is what a
quorum used to buy, and what a quorum cannot buy here. The shipped CLI dials one
address, so a session is a star, and when a star's centre goes every survivor is
left alone: none can reach another, none can ever collect a vote, and a rule
requiring a strict majority of the closed roster elects nobody in the one shape
every real session has. The two-participant session is the plainest case — one
survivor of a roster of two is not a majority of two, and it is the whole session.

The procedure on each survivor is:

1. **Flood a loss notice.** It decides nothing, but only a direct neighbour of the
   authority sees the link drop, and the departure crossing that would have carried
   that news is produced by the participant that is gone. A survivor two links away
   opens the same succession from the notice.
2. **If the session allows the term to move and the roster names this instance,
   take it.** Immediately — there is
   nothing to collect and nobody to ask, and the session is stalled until somebody
   authors. The one self-check is retention: a successor with no retained
   authoritative record has no baseline for a delta to name and would answer the
   first manifest with a whole world for every survivor at once. A join capture
   counts as retention, so a guest admitted seconds before the loss is eligible.
3. **Otherwise wait for the record.** A handoff carries the roster, the slot
   assignments, the session anchor and the barrier delay, so adopting it is one
   decision rather than a term change followed by a roster negotiation. A receiver
   refuses one naming anyone but the successor its *own* roster designates, which
   is the half of the rule a receiver checks for itself.
4. **After `parameter.NetworkSuccessionTicks`, give up.** No record arrived, so
   this instance cannot reach the successor and continues as an explicit local
   fork.

Retention *ordering* is deliberately not an eligibility test any more. With one
designated candidate there is no alternative to prefer, and a successor a cadence
behind a peer moves that peer back by a cadence — which is what a correction is.
What the rule gives up instead is stated in §8.

A survivor that forks continues locally with `network.fork` and persistent
`HOST LOST:LOCAL`; encountering a higher term later is refused because partition
merging is not implemented.

### 5.2 The roster after a loss

An instance left with **no link at all** — the successor of a star, and every
survivor that could not reach it — drops every cursor it does not simulate: the
authority that went, and behind it the guests that were only ever reachable through
it. Having no link is what makes that exact rather than convenient: a departure is
produced once at one agreed tick because two instances must destroy the same shared
entity together, and here there is no second instance. On a fork the removal is
therefore local; the successor produces the predecessor's as the ordinary crossing,
because it may.

An instance that still holds links keeps its roster, which is the worse half of
gap 5 below: peers to agree an apply tick with, and no authority to name one.

A departure, however it is produced, also clears what the instance had applied from
that participant. Identities return to the pool and a crossing sequence starts at
one, so a fence kept from the previous holder would claim a capture already contains
crossings the next holder of that identity has not produced — and §3.2's install
rule would then discard exactly those.

### 5.3 Reachability under `-authority migrate` — proposed

Not built. This is the design §8's gaps 3, 4 and 5 are held open against, written
down so the implementation is a transcription rather than a rediscovery.

`-authority host` does not change: the address is the session, one instance binds
it, and authorship never leaves the machine that started the world. It stays the
default for `-serve` and the honest setting for any deployment where the world may
only live where it started.

`-authority migrate` gains the half it is missing. Authorship already moves
correctly; what no survivor has is a way to reach the participant it moved to.
The proposal is that a guest in a migrate session **also binds a listening port**,
and that the authority **publishes the confirmed addresses** so every participant
holds the same reachability map before it is needed.

#### The sequence

1. A guest joins as it does today. Its address is published to nobody, and it
   binds a listening port of its own — the host's port by default, `-listen <addr>`
   to pin one.
2. The guest shows a warning that its address will be shared with the other
   participants in five seconds, which is the window in which quitting costs
   nothing.
3. The host dials the guest's declared address once. That round trip is the
   **bind confirmation**: it turns "the guest says it is listening" into "the
   session has reached it there", which is what makes the map worth publishing.
   A guest behind NAT, behind a firewall, or on a port already taken fails this
   step and stays a leaf.
4. Five seconds after confirmation, and only if the guest is still connected, the
   authority broadcasts the updated map on `MsgPeerList` — reserved, unused, and
   the message this is for.
5. Peers dial from the map. The lower identity dials the higher, so a pair opens
   one link rather than two.

#### What the design has to answer, and how

**An address is not roster identity.** `SessionParticipant` is compared by value:
`SameRoster` sorts two rosters and calls `slices.Equal`, and `HandoffRecord.Validate`
refuses a record whose roster is not byte-identical to the one the session closed
on. An `Addr` field on that struct would make a guest that rebound its port look
like a different roster and fail every handoff. The map is therefore a **separate
replicated table keyed by identity**, carried beside the roster in the offer, the
handoff and `MsgPeerList` — never inside it.

**Reachability must be decided globally or not at all.** The whole of the
split-brain rule is that `DesignatedSuccessor` is a pure function of state every
survivor holds identically. If each instance filtered candidates by its own dial
results, two survivors would compute two successors, which is the one outcome the
current design rules out. So: the **authority-published, confirmed** map is an
input to succession; a local dial failure is not. A survivor that cannot reach the
elected successor falls back to the existing timeout and forks — strictly better
than today, where nobody reaches anybody.

**Then eligibility should use it.** Once the map is authoritative, the rule
becomes "the lowest surviving identity **that the map confirms**", which also
closes most of gap 5: losing the authority and the participant after it still
elects somebody, as long as one confirmed participant survives. A guest that
failed confirmation stays in the session, plays normally, and is skipped as a
candidate.

**A stale map must not resurrect a departed peer.** The broadcast carries the
authority's term and is refused below the term the receiver holds, like every
other authoritative artifact.

**Ports collide.** Two guests on one machine cannot both bind the host's port,
and neither can a guest on the host's own machine. The bind tries the default,
falls back to an OS-assigned port, and declares whatever it actually bound;
`-listen` overrides. A bind that fails entirely is not fatal — the participant is
a leaf.

**Binding is eager, not lazy.** Binding at the moment of succession would fail at
the worst possible time and could not be confirmed in advance. The port is held
for the session and used only after a handoff.

#### Left for the user to decide

| Question | Options | Recommendation |
|---|---|---|
| Mesh shape | Full mesh (everyone dials everyone): N² links, relay redundancy. Successor chain (each guest dials only the confirmed successor): N links, closes the gap and nothing more. | Successor chain first; the map is the same either way, so the shape is one dial policy. |
| Declining advertisement | A `-no-advertise` guest plays as a leaf and is never a candidate, or a migrate session refuses it. | Leaf. Refusing turns a privacy preference into a lockout. |
| Default listen port | The host's port (memorable, one firewall rule) or always ephemeral (never collides). | Host's port, falling back to ephemeral, declaring what was bound. |
| Rejoin after a handoff | A departed guest keeps the last map and retries each member, or reconnect stays `-authority host` plus a stable address. | Both; the map costs nothing extra once it exists. |

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

Reviewed against the code on 2026-09-07. Each entry says what is actually absent
rather than what is imperfect, and how to see it.

### Deferred by decision

1. **Authentication and confidentiality.** Links are plaintext and a session is
   reached by its address alone. Participant claims, loss notices and handoff
   records are structurally checked but not authenticated; the rules prevent
   races, not a hostile peer. The deployed fleet accepts this and hardens the open
   port instead; see the [fleet plan](kubernetes-fleet.md) §4 for what bounds a
   stranger today and what does not. §5.3's address map does not change this: it
   publishes reachability inside a session that was already unauthenticated.

### Open

2. **The playout lead is carried but never chosen.** The mechanism is whole:
   `BarrierDelayTicks` travels in `SessionOffer`, survives a handoff in
   `HandoffRecord`, reaches `NetworkResource`, and sets `NetworkSystem.delayTicks`.
   Nothing ever gives it a value other than `parameter.NetworkBarrierDelayTicks`,
   so a 150 ms budget is what every deployment gets, and it is not
   graph-diameter aware — a relayed peer inherits its neighbour's hop count with
   no allowance for it. The *correction cadence* does adapt per link, between
   `SnapshotCadenceMinTicks` and `SnapshotCadenceMaxTicks`; apply deadlines do not.
   Missing the lead is survivable — §3.2's fences make a late artifact harmless,
   not rare — so this is a quality gap, not a correctness one.

   *To see it:* run a session across a link with more than 150 ms of round trip
   (`tc qdisc add dev lo root netem delay 100ms` between two local instances is
   enough) and read the guest's status snapshot. `network.barrier_late` counts
   artifacts that arrived after the tick they named, `network.lag_ticks` is how
   far behind the newest peer this instance is, and `network.stale` latches once
   that lag passes the lead. On a healthy local session all three stay at zero;
   under the added delay `barrier_late` climbs monotonically. The fix is to
   negotiate the value from the measured round trip the link probe already
   collects, rather than to make the fixed number bigger.

   This should become a tracked issue rather than an architectural gap: the
   design is right and one input is unwired.

3. **Guests do not bind, so the CLI builds a star.** The protocol relays over
   arbitrary graphs — each source epoch is admitted once inside a bounded replay
   window and forwarded to every neighbour but the arrival edge — but
   `network.NewSocketPort` has exactly two callers, the network service and
   `beginHostingLocked`, and both run on the instance that was asked to host. A
   guest never listens, so `-join` can only ever produce a star.

4. **Authorship migrates, reachability does not.** A successor authors but cannot
   be dialled, and no artifact in the protocol carries an address: an offer carries
   identity, roster, term and bounds; a handoff carries membership. In a star the
   result is one solo game per survivor, and the only thing succession decides is
   which of them believes it is hosting (§5.0).

5. **A successor lost with the authority elects nobody.** `DesignatedSuccessor`
   names one identity and no other instance may take the term, so losing both the
   authority and the participant after it forks every survivor — including
   survivors that can still reach each other and would have agreed. Reconciling an
   explicit fork back into a higher term is not implemented either, and a fork that
   still holds links is the worse shape: it has peers to agree an apply tick with
   and no authority to name one. A fork alone does not have that problem (§5.2).

   3, 4 and 5 are one gap seen from three sides, and §5.3 is the proposed
   closure: a guest that binds, a confirmed address map published by the
   authority, and a successor rule that reads it. 3 is the mechanism, 4 is what
   the mechanism is for, and 5 is what the map makes decidable. The decisions
   §5.3 leaves open are the ones to settle before the work starts.

6. **Shared-domain mutation through the embedder API is unchecked.**
   `App.Reset` is session-aware — it refuses on a guest and publishes a crossing
   on the authority. `App.SetupLevel` and `App.Region` are not: both push
   `event.OriginDebug` on the local instance only, and both carry `ClassShared`
   events (`EventLevelSetup`, `EventFSMRegionRequest`). Calling either on a guest
   in a live session changes that guest's map bounds or FSM regions and nobody
   else's, which is a divergence the domain rules exist to prevent.

   The interactive surface has no path to either — the only producers are
   `internal/app/headless.go` and the resize `App.Loop` records — so today this is
   reachable from a harness, a test, or an embedder, and the D-14 map latch
   already prevents the one case that used to happen by accident. The gap is that
   nothing *enforces* it: the check `Reset` makes is the check these two owe and
   do not make. Closing it is small — the same `LiveSession`/`IsSessionCoordinator`
   guard, refusing on a guest and crossing on the authority — and it is worth
   closing because "caller discipline" is not a boundary.

7. **Domain-boundary debt.** Ambient-local stamping exemptions,
   `event.EmitDeath`'s direct queue path (which takes the domain from its
   entities rather than from the ambient tag, and so bypasses `World.PushEvent`),
   route-anchor casts, and combat telemetry that aggregates both domains into one
   set of counters — `combat.` and `kills.` are excluded from the compared shared
   surface for exactly that reason. Each is individually defensible and
   collectively they are the reason the compared surface has a denylist. Long
   outstanding; the cheapest order is to make each exemption explicit at its site,
   then delete the ones that turn out to be unnecessary, and only then consider
   moving the telemetry.

8. **Tower ownership binds a shared structure to one cursor.** A tower's
   `CombatComponent.OwnerEntity` is the cursor its spawn request named, which
   comes from the FSM's `player_entity` capture variable — one cursor for the
   whole machine. Damage attribution (`ResolveCursor(payload.OwnerEntity)` in
   `applyHitDirect` and its area-damage sibling) therefore credits one participant
   for a structure the session shares. There is no quick fix: the options are a
   hybrid shared/local shape like the cursor and its shield, or an explicit
   "every player" ownership value that attribution expands. **To be decided.**

   The quasar half of this entry was wrong and is removed. Quasar escalation gates
   on `kills.drain`, and that is correct: drain is a player-domain species whose
   defeat drives a shared-domain spawn, the FSM that reads the counter is
   authoritative, and its state travels in the correction. A session-wide drain
   total is the intended behaviour, not a domain leak.

9. **Presentation is clipped, not wrong.** A terminal smaller than the session's
   map shows less of the map. It does not produce a different world or a
   misplaced cursor: `applyMapLatch` installs the session bounds before the FSM
   boot script spawns cursor slot zero, and `LockMap` latches the world shared for
   the whole run, which is what fixed the terminal-size divergence several
   iterations ago. Remote cursor motion is drawn at simulation arrival ticks, so
   it steps rather than glides.

   *To see the remainder:* `-serve :7777 -size 200x60`, then join from an 80x24
   terminal. The view is a correct 80x24 window onto a 200x60 world; every cursor
   is where the authority says it is. What is missing is a windowed composite that
   scrolls and optional presentation interpolation, both of which are presentation
   work independent of simulation ordering. If a *wrong* cursor position is ever
   observed across terminal sizes, that is a regression in the map latch and not
   this item.

10. **Cross-platform float drift is repaired live but not in replay.** Verified:
    a correction carries every shared component whole, float fields included, and
    a delta is proved lossless by re-hashing the reconstruction against the
    capture's own integrity hash — so a `float64` that drifted on another platform
    is repaired exactly like any integer that drifted. Two things remain, and
    neither is the original claim:

    - The digest hashes float bits (`digest.f64` is `math.Float64bits`), so a
      one-ULP difference flips `network.digest_mismatches` and sets
      `network.drift_part`. That is diagnostic noise on a mixed-platform session,
      not a simulation fault.
    - A journal replayed on a different platform has no authority to repair
      against, so a float difference there accumulates. Replay determinism is
      guaranteed within one implementation build.

    Reworded rather than dropped: the live path is sound and the statement about
    it was wrong.

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
- The host leaves instead. The guest takes the term — `:session` names it as the
  authority, `network.migrations` reads 1, and `HOST LOST:LOCAL` clears — and the
  host's cursor must be gone from its map rather than standing where it was left.
  A guest that could not reach the successor reports `HOST LOST:LOCAL` and keeps
  playing instead, which is the fork.
