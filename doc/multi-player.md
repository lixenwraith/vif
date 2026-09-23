# Multiplayer

This document is the whole of multiplayer except the domain rules, which live in
[Multi-instance domain model](domain-design.md). §1–§9 are the runtime as it
exists: the contract, the correction and ordering boundaries, authority
continuity, the operating point, the diagnostics, and what remains. §10 is where
the protocol came from — the incident that prompted it and the options it was
chosen against — and is history rather than a description of the runtime.

## 1. Vocabulary and boundaries

Use these terms consistently:

- A **network peer** is an endpoint exchanging session frames. "Peer" is
  relational, not a second namespace: a link is addressed by the identity of the
  participant on its far end, so `network.PeerID` is both who and which link, and
  no instance is ever its own peer.
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

Two numbering spaces, each with its own reserved zero. A **participant identity**
runs `1..MaxPlayers+1`; zero means *no participant*, which is what makes an absent
crossing fence, an unused epoch window and every `id == 0` guard answer the same
way. A **roster slot** runs `0..MaxPlayers-1` and names a cursor, with
`NoPlayerSlot` for a participant that drives none. Slot zero is the first cursor
rather than the local one — an instance identifies itself by its participant
identity, and giving it a second name would cost the identity space its sentinel.

## 2. Current contract

| Area | Current behaviour |
|---|---|
| Local input | Every crossing applies at one agreed tick, a playout lead after its production, on its producer as on its peers. The D-18 prediction answers the keystroke at once; the shared store moves when the crossing does. The one exception is a typed gold member, which the producer removes at once because the next keystroke validates against the live run. |
| Shared authority | The host's Shared world is canonical. A guest's predicted result is provisional until the next correction. |
| Player state | Each instance simulates only its Player domain. Owner-authored cursor values have one writer and travel as values; a receiver keeps the values it authors across an install. |
| Predicted derivations | A shared death a predicting instance derives is stamped `PhasePredicted` and held in a ledger. Presentation follows it; the player-domain rewards behind it are paid once, when an authoritative world proves the entity gone. |
| Global environment | Shared wind events are re-derived rather than sent. Every instance consumes two draws from the same Shared environment stream, applies the gust to its own drains and predicted Shared species, and restores active wind state through corrections. |
| Corrections | A correction starts with a versioned hash index. Equal roots send no state. Mismatches descend to independently proved pages. Compressed whole keyframes remain the bounded fallback. |
| Correction playout | The clock never moves backwards. A correction behind this instance's tick is projected: simulated forward in the staging world, over what this instance applied since, and written at the present. One ahead of the clock inside the lead waits for its tick; the newest waiting one wins, and one further ahead is taken as the jump it is. |
| Local replay | A guest retains a bounded canonical suffix of its own crossings; a projection re-applies those past the installed capture's fence for its source, at their own ticks. |
| Crossing ordering | Snapshot schema 5 carries one applied-sequence fence per participant. A receiver removes ordinary frames the installed world already holds — including ones whose nominal receive tick is still ahead — and keeps the ones it does not, including ones whose receive tick is long past. |
| Local FSM lifecycle | A live install replays only config-marked persistent `ClassLocal` exit/entry events for crossed state paths; staging and all ordinary actions remain side-effect free. |
| Join and reconnect | A running game can begin hosting; join and reconnect install a current capture through the same staging path. Every host arms the same mid-run gate once its own lobby is done, so a reconnect takes one path whether the session started with `-host`, `-serve`, a script, or `:host`. |
| Roster | Every admitted peer holds an identity, a term and a vote; a roster slot binds it to a cursor and makes it a participant. The coordinator of a dedicated host holds no slot, so it is a peer and not a participant, and a session of one guest has one participant in a roster of two. |
| Cadence | Each direct link gets a bounded correction plan derived from round-trip time, variation, delivered bytes, saturation, and correction demand. The whole-world convergence floor is fixed. |
| Playout lead | Re-derived by the authority from the worst measured round trip — the whole round trip plus a reordering allowance, times the topology's hop count — and published as a barrier-bound crossing. No lead at all with nobody on the far end, `NetworkBarrierDelayTicks` with nothing measured, `NetworkBarrierMinDelayTicks` to `NetworkBarrierMaxDelayTicks` once a link has been probed. It widens at once and narrows on a window. |
| Playout ceiling | A link whose *smallest* observed round trip asks for more than `NetworkBarrierMaxDelayTicks` is dropped rather than absorbed. The smoothed round trip is not the test: a backlogged link is the cadence controller's problem, not the barrier's. |
| Mesh and relay | Epochs, owner state, corrections, and authority records flood with per-source duplicate suppression. A relay with retained authority content keeps selective repair available to participants behind it. |
| Reachability | In a migrate session a guest binds a port of its own and declares it, and the coordinator publishes the whole succession chain on `MsgPeerList`. Every participant holds a link to the current successor. `-no-advertise`, a failed bind, or `-authority host` leaves a participant a leaf: it plays normally and is never elected (§5.3). |
| Host loss | `-authority migrate` (default off `-serve`): **the first survivor in the succession chain** takes the next term, with no vote, because every survivor computes it from state it already holds identically. `-authority host` (default on `-serve`): nobody takes it and every survivor continues alone. |
| Trust | Links are plaintext and unauthenticated by decision. What the coordinator *does* check is identity: a joiner reports its protocol, simulation fingerprint, capture and journal schemas, and tick interval, and a peer that does not match the offer is refused before it takes a roster slot. The seed, session and scenario are adopted rather than compared — `PeerIdentity.SessionFrom` takes the coordinator's, and a scenario no local root holds arrives over the wire. The corpus is neither: glyphs are player domain, so each participant reads its own and a peer with different text still joins. |
| Allocated lifetime | A dedicated host may bound its own life: a first-guest window, an empty-roster grace, and a drain a termination signal opens. Draining and expired sessions refuse a dial with `ErrSessionEnding`, distinct from the retryable `ErrSessionStarting`. See [Runtime](runtime.md) §1.2. |

The central choice is that guests keep simulating. Determinism fills time between
corrections, makes a converged exchange hash-only, and preserves responsive local
terminal input. Correction magnitude measures prediction distance; it is not a
request to turn the guest into a renderer. What a correction may never do is show
the player a state the world has already left: it neither rewinds the clock nor
undoes an artifact every instance applies at the same tick, so a transition —
a kill, a knockback, a region's species changing — happens once on every screen.

Directional Storm fire uses a Shared aim and Player-domain results. On every active
tick, the red circle selects the nearest Shared cursor in deterministic roster
order and refreshes one aim point used by both bullets and muzzle presentation.
The existing coordinates travel in the ordinary Shared component correction, so
direction converges without adding target identity to the wire; bullets remain
local effects.

## 3. Event timing and correction containment

### 3.1 Ordinary and barrier-bound crossings

Every wire frame carries four ordering values:

- `Source`: the participant that produced it;
- `ProducedTick`: the source epoch that batched it;
- `ApplyTick`: the absolute receive-side simulation tick;
- `Seq`: the source-local order within the crossing stream.

A crossing has one application time. Every copy, the producer's included, waits
until `ApplyTick` and is ordered by `(ApplyTick, Source, Seq)`, so the lead is a
receive buffer for reordered traffic and never a difference between two worlds. It
used to have two: the producer published its own copy at once, and every correction
read inside the lead described a world without it — the kill was undone and
re-derived, the knockback rewound, the region's species torn down and rebuilt.
Input feel is the D-18 prediction's job, not the store's (§3.4 of
[domain-design.md](domain-design.md)); the one copy still published at once is a
typed gold member, because the next keystroke validates against the live run.

An artifact that decides what the world *is*, rather than what happens to a world
both instances already have, is `barrierBound`: it also closes the capture fence at
production rather than at application, because it never enters the replay suffix.
A correction must not be repairing entity allocation or run numbering.

| Barrier-bound crossing | What it decides |
|---|---|
| `EventParticipantJoined` / `EventParticipantDeparted` | creates or destroys a shared cursor |
| `EventGameResetRequest` | replaces the run |
| `EventSwarmSpawnRequest` / `EventQuasarSpawnRequest` | allocates a shared species from a drain fusion |
| `EventDrainDefeated` | advances the shared progression a region gates its spawns on |
| `EventCursorDefeatState` | folds into `session.all_defeated`, which `MonitorGlobalReset` rebuilds the level on |
| `EventPlayoutLead` | changes the lead every instance defers its own crossings by |

The last two are the least obvious and each was a visible defect. A producer that
counted its ninth drain a lead early entered the escalation a lead early and built
that region's world before anyone else had it — the storm seen flickering into
existence. A participant whose own cursor went down flipped its latch a lead before
the rest and ran `MonitorGlobalReset` on its own: it cancelled every region, cleared
the level and strobed, and the next correction — published while the authority was
still in `MonitorActive` — put the pre-reset world back. The player saw the strobe
and no reset, and went on taking hits that should each have ended the run.

Nobody's input waits on any of these. What the player did is the drain dying or the
cursor going down, both Player-domain and immediate; the shared consequence waits
the same lead everywhere instead of on all but one instance.

A latch the barrier feeds needs a second rule: whatever the agreed artifact sets
must be cleared by the world it rebuilds, not by a later artifact. `MetaSystem`
clears the defeat latch on the `EventLevelSetup` that carries `clear_entities`,
because the un-defeat crossing arrives a lead after `MonitorArm` has already
re-armed the roster — long enough for the guard to fire a second reset.

Wind start/cancel are the opposite case: they are `ClassShared` outputs suitable
for a Shared FSM transition. They appear identically in every journal but never
on the wire, because a transported copy would duplicate the transition's local
output. Wind variation is sampled once per tick, before entity iteration, so a
participant's private drain count cannot perturb Shared RNG order. If a future
Player mechanic starts wind, it must cross a separate Bus request whose Shared
resolution installs the wind at the agreed tick.

### 3.2 The capture boundary

`ApplyTick` does not say whether a capture contains a given ordinary frame: a guest
whose link misses the lead produces a frame for T+3 that has not reached the
authority when it reads its world at T+9, so the capture is missing one whose
`ApplyTick` is already six ticks old — and the authority applies it late when it
arrives, in the order it arrives.

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
tick rule is the whole rule for barrier-bound ones: they apply at one agreed tick
on every instance including their producer, so the tick is exact for them and
nothing else is needed.

This closes the guest-action pattern — a correction undoes the player's own
keystroke, and the next one puts it back a cadence later — because a frame the
capture never saw is kept even when its receive deadline is long past. The
host-cursor pattern — a correction installs a new host position and queued older
absolute positions then walk the guest backward — is closed by construction: the
authority no longer applies its own frame before the tick every copy applies at.
The same fences are retained after installation, so a stale batch arriving later
is classified the same way.

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

### 3.3 Projection

A correction may describe a host tick behind the guest's present, and a world is
never written at a tick the receiver has run. The capture is resolved into the
staging world, which is then made this instance's predictor — it drives no cursor,
holds this instance's owner-authored cursor values, and defers by the session's
lead under the session's authority — and simulated forward to the live tick. What
is written into the live world is that projection, at the tick the live world is
on. Everything the projection re-derives as the live world already has it diffs to
nothing, so a transition the guest has already made is neither torn down nor
rebuilt, and the clock never moves backwards. `snapshot.projected_ticks` is the
distance; a capture level with the clock projects zero ticks and still takes the
open epoch, which is where a typed member and a late-arriving record live.

The projection is fed what the capture does not contain and this instance applied
after it: its own ordinary crossings past the capture's fence for its source, in
their encoded wire form, and the applied barrier-bound artifacts due after the
capture's tick, each at its own apply tick. Production ticks bound retention age;
they do not choose membership, and neither does the apply tick. A crossing still
pending on the live barrier is not fed — the barrier applies it at its tick, and
an install leaves the barrier and the D-18 queue alone. The suffix is bounded by
ticks, records, and encoded bytes; if retention has a hole, the guest installs the
authority alone and reports the skipped replay rather than guessing at a partial
history. Ordinary crossings from *other* participants applied inside the window
are not retained, so a projection loses them until the next correction; that gap
is in `doc/todo.md`.

A capture ahead of the clock is a join or a jump and is adopted at its own tick;
inside the lead it waits (§4.1).

Consecutive simulation events are not coalesced on the wire. Two cursor placements
may consume different glyphs or cause different collision and progression effects;
discarding the intermediate event would change gameplay. Batching, selective
state repair, and presentation work are the safe optimisation layers.

### 3.4 Predicted derivations

A guest's Shared state is not monotonic across a correction that disagrees with
it: a correction can restore a species the guest killed with a crossing the
authority had not yet applied, and the projection kills it a second time. For
Shared state that is right: the FSM, the kill tallies, the adaptation and the
genetic registry are all in the capture, so they are repaired and re-derived
together, and a projection re-derives them at the tick the guest had them.

Player-domain state is not. Loot, the boost grant and every presentation burst are
in no capture, nothing rolls them back, and nothing stopped the second derivation
paying them again. Measured on a two-participant mesh at a two-tick link delay: one
swarm, killed once by the player, announced killed twice and rewarded twice.

The bridge is therefore phased. A Shared-domain `EventSpeciesKilled` raised while
`World.PredictsShared` — a live session in which somebody else authors — is stamped
`event.PhasePredicted` and its payload entered in a ledger keyed by the dying
entity. Recording is idempotent, so a re-derivation after a rollback adds nothing.
An install settles the ledger against the world it just wrote: an entity the
authority does not have is proved dead and raised once as `EventSpeciesKillConfirmed`,
and one the authority still holds a convergence floor later is dropped as the
misprediction it was. `LootSystem` and `BoostSystem` consume the confirmed form and
ignore the predicted one; everything else consumes `EventSpeciesKilled` exactly as
before, because the capture already reconciles what it holds.

Three cases close the edges. An instance that stops predicting — it took the term,
or the last peer went — releases everything it holds, because the world it predicted
is the only one there is. A reset drops the ledger, for the reason it drops the
replay suffix. Past `parameter.PredictionLedgerMax` the oldest entry is released
unproved rather than discarded: losing a player's progression to a bound is worse
than paying it a cadence early.

This is the edge-triggered half of what the FSM's `reconcile = true` lifecycle
replay does for the level-triggered half (D-19). Neither covers the other: a hold
that follows a Shared region is re-derived from the imported state, and a one-shot
reward is held until the state that caused it is proved.

### 3.5 The lead the session defers by

The playout lead is session identity — every instance defers its own crossings by
the same number of ticks, and a reproduction of the run defers by the same again —
so it is authored by one instance and travels rather than being measured
independently. The authority re-derives it between ticks from the links it measures
and publishes each value as the barrier-bound crossing in §3.1's table; the offer
and the handoff record carry the same number to a participant with no history to
apply.

Three answers rather than a constant. A roster with nobody on the far end takes no
lead at all: nothing is sent, so nothing is waiting for it, which is what makes a
hosted solo run cost no input latency. A roster with nothing measured keeps
`NetworkBarrierDelayTicks`, because an unready link is no evidence rather than a
fast one. A measured link asks for its whole round trip plus a reordering
allowance, times the hop count, between `NetworkBarrierMinDelayTicks` and
`NetworkBarrierMaxDelayTicks` — so an in-process or loopback session defers by one
tick where it used to pay three. The round trip and not half of it, because the
authority reads a guest's crossing a correction's age after the guest produced it:
a lead of one way keeps the peer's copy on time and the authority's a correction
late, and every capture read in between is missing it.

The two directions are not symmetric. Widening is immediate, because an artifact
that misses the lead costs a correction and one that clears it costs nothing;
narrowing waits `NetworkBarrierRenegotiateTicks` of the lower measurement holding.
That same window re-announces an unchanged lead, so a change a peer never received
costs a window rather than the rest of the session. A narrowing lead would also
number the next artifact below one already scheduled, and a receiver orders by
`(ApplyTick, Source, Seq)`; the barrier therefore holds each source's newest apply
tick and lets the production epoch catch up to it, which decays the lead by a tick
per tick instead of reordering two of one source's crossings.

The ceiling is a refusal, not a clamp. It is judged on the link's *smallest*
observed round trip, because a backlogged link reports an inflated one that the
cadence controller is already narrowing the correction stream to relieve — dropping
that participant would be answering a bandwidth problem by ending somebody's game.
What the floor says is what the link costs with nothing queued on it, and past the
ceiling that is not a session anyone can play.

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

The environment is a declared system-state carrier. A capture therefore includes
its base force, normalized direction, remaining duration and application phase;
the general RNG section includes the environment stream position, and Shared
component pages include the species kinetic result. Player drains remain local
and resume from the same per-tick samples after an install.

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

### 4.1 The playout buffer

The paths deliver worlds of different ages: a whole body is one one-way delay old,
a selective repair three. When an install adopted the capture's tick, alternating
between them stepped the clock by up to four one-way delays and every shared actor
moved that many ticks with it — the knocked-back swarm a guest saw jittering. A
capture behind the clock is now projected (§3.3) and moves nothing; the buffer is
for the other direction.

A correction describing a tick this instance has not reached, within the lead, is
held until it has, and it is compared there too: a manifest over a tick this
instance has not run would name every page different and buy a repair of nothing,
so it waits with the same rule. The newest arrival while one waits replaces it —
an older one describes a world the authority has left — so at most one is ever
delayed and never by more than the lead. One further ahead than the lead is not
a correction this instance can wait for; it is adopted at its own tick as the jump
it is, counted in `snapshot.corrections_jumped`. A hash-only acknowledgement
behind the clock is not adopted at all: the world already equals it, and adopting
its tick would be the rewind. `snapshot.corrections_held` counts the deferrals; a
session whose paths deliver worlds of one age holds none.

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
| Who binds a port? | The coordinator always: the instance started with `-host` or `-serve`, or a solo run that opened itself with `:host`. In a **migrate** session every other participant binds one too — `-listen` pins it, the default is the coordinator's own port, and a machine already using that port falls back to an OS-assigned one. `-no-advertise`, a bind that fails, a browser build, and every `-authority host` session leave a participant with no port of its own. |
| Who authors? | The participant holding the current term. It is the coordinator until a handoff moves it, and a handoff moves *authorship only*. |
| How is a participant reached? | The coordinator is reached by `-join <address>`, or by the `vif://address/name` link a deployment issues when one address serves several sessions; there is still no discovery and no rendezvous. Everyone else is reached through the **succession chain**: every participant that declared a port, in join order, with the address it declared. The coordinator publishes it whole on `MsgPeerList` and carries it in the offer and the handoff record, beside the roster. |

The three used to disagree, and the whole of `-authority migrate` was the cost.
Nothing but the coordinator listened and no artifact in the protocol carried an
address, so **a successor authored but could not be dialled**: it did not bind the
port its predecessor held — it is usually on another machine and could not bind
that address anyway — and no guest had ever been told where it was. In the star
every `-join` builds, the outcome was one solo game per survivor, and the only
difference the succession made was which of them believed it was hosting one.

§5.3 is what closes that, and it closes it without rebinding anything: the
predecessor's address is not reused, so there is no `TIME_WAIT` race to back off
from and no window in which an address is half-released. What moves is which
address the survivors dial, not which address answers.

`-authority host` is unchanged and remains the default for `-serve`. There the
address *is* the session: an orchestrator replacing the pod at the same address is
the reconnect its guests actually want, and a guest's own port would be for
nothing. It is also the setting for a deployment where the world may only ever
live on the machine that started it.

### 5.1 Succession

If the authority disappears, the successor is **the first survivor in the
succession chain**, falling back to the roster's lowest surviving identity when the
chain names nobody still present. It is a pure function of the chain, the roster and
the participant that went, all three of which every survivor already holds, so every
survivor names the same successor without exchanging anything. The chain is
append-only, so a survivor holding a prefix of it elects the same participant as one
holding the whole — which is why it needs no agreement step.

A *local* dial result is never an input, and that is the rule that matters most:
if each instance filtered candidates by what it could reach, two survivors would
compute two successors, which is the single outcome this design rules out. A
survivor that cannot reach the elected successor falls back to the timeout and
forks.

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
3. **Otherwise wait for the record, dialling while it waits.** A handoff carries
   the roster, the slot assignments, the session anchor, the barrier delay and the
   chain, so adopting it is one decision rather than a term change followed by a
   roster negotiation. A survivor with no link to whoever is taking over walks the
   succession list — every candidate once, the list again a second later, the
   attempt count on the status bar — and announces itself on each link it opens, so
   an authority elected before that link existed answers with its record instead of
   letting the survivor time out.
4. **After `parameter.NetworkSuccessionTicks`, give up.** No record arrived, so
   this instance cannot reach the successor and continues as an explicit local
   fork.

Retention *ordering* is deliberately not an eligibility test any more. With one
designated candidate there is no alternative to prefer, and a successor a cadence
behind a peer moves that peer back by a cadence — which is what a correction is.
What the rule gives up instead is stated in §8.

A survivor that forks continues locally with `network.fork` and a persistent
`Host lost` badge; encountering a higher term later is refused because partition
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

### 5.3 Reachability under `-authority migrate`

Built. `-authority host` does not change: the address is the session, one instance
binds it, and authorship never leaves the machine that started the world.

1. A guest binds a listening port before it answers the offer, so it declares the
   port it *actually* bound. `-listen <addr>` pins one; the default is the
   coordinator's own port, so a session is one firewall rule and the port a guest
   opens is one somebody already chose to open. A machine already using it — two
   guests on one machine — takes an OS-assigned port instead. `-no-advertise`, a
   failed bind, or `-authority host` leaves a participant a **leaf**: it plays
   normally, declares nothing and is never elected.
2. The coordinator puts every declared address in the chain and publishes the whole
   chain on `MsgPeerList`, again as the session opens so a participant admitted
   early is not left holding a prefix. It completes a declared `:7777` from the join
   connection's remote address, because a participant knows which port it bound and
   not which address the world reaches it at.
3. Every participant dials the current successor and keeps that link warm, so the
   instance that will have to author already has one when it does. A **successor
   chain** rather than a full mesh: N links instead of N², and the chain is the same
   either way, so the shape is one dial policy and can be widened later without
   changing an artifact.
4. Two participants that dial each other exchange `MsgConnect`/`MsgAck` — who is
   calling, running which build — and nothing else: they already hold identities the
   coordinator assigned, so a peer link allocates nothing, offers nothing and
   captures nothing. Admission is roster membership, read from the world rather than
   from the coordinator's own list, and deliberately *not* the term: a survivor
   still electing is exactly who needs to open one.

#### Why the chain is beside the roster and not in it

**An address is not roster identity.** `RosterEntry` is compared by value:
`SameRoster` sorts two rosters and calls `slices.Equal`, and
`HandoffRecord.Validate` refuses a record whose roster is not byte-identical to the
one the session closed on. An `Addr` field on that struct would make a guest that
rebound its port look like a different roster and fail every handoff.

**Append-only is what makes it a legal succession input.** `DesignatedSuccessor`
must be a pure function of state every survivor holds identically, and a broadcast
is identical only eventually. Appending never reorders, so any two participants hold
a chain and one of its prefixes — and a prefix and its extension name the same first
survivor. That is the whole of the agreement, and it needs no barrier crossing.

**A stale chain must not resurrect a departed peer.** `MsgPeerList` carries the
authority's term and is refused below the term the receiver holds, like every other
authoritative artifact. A departure drops the entry locally on every instance,
because the departure crossing already reaches every instance at one agreed tick.

**Binding is eager, not lazy.** Binding at the moment of succession would fail at
the worst possible time. The port is held for the session and used only after a
handoff.

#### The decisions this was built on

| Question | Decision |
|---|---|
| Mesh shape | Successor chain. Full mesh is N² links for redundancy nothing yet asks for; the chain is the same either way, so widening it later is one dial policy. Moving authorship down the list for latency or loss is deliberately *not* here. |
| Declining advertisement | `-no-advertise` plays as a leaf. Refusing such a participant would turn a privacy preference into a lockout. |
| Default listen port | The coordinator's own port, falling back to ephemeral, declaring what was bound. A port that is harmless on the machine that chose it is not necessarily harmless on somebody else's. |
| Rejoin after a handoff | Every participant holds the whole chain, so a survivor with no link walks it — every candidate once, the list again a second later — with the attempt count on the status bar. |

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
- `snapshot.corrections_held` and `snapshot.corrections_jumped`: how often the
  playout buffer deferred one because this instance had not reached the tick it
  describes, and how often one was too far ahead to wait for (§4.1);
- `snapshot.projected_ticks`: how far behind the clock the last correction was,
  which is the distance the projection simulated (§3.3);
- `snapshot.replay_records`, `snapshot.replay_skipped`, and
  `snapshot.replay_suffix_unavailable`: whether local predicted work survived;
- `snapshot.predictions_pending`, `snapshot.predictions_confirmed` and
  `snapshot.predictions_dropped`: the §3.4 ledger's depth, the rewards an
  authoritative world proved, and the derivations it refused. A guest that is
  converging confirms nearly everything it predicts; a rising drop count is a guest
  whose crossings are not reaching the authority;
- `network.artifacts_pre_install`: frames discarded because an installed capture
  already represented them;
- `network.artifacts_authority_superseded`: the subset discarded by the authority
  sequence fence rather than by tick;
- `network.lag_ticks`, `network.stale`, and `network.barrier_late`: whether the
  receive lead is being missed, and `network.barrier_delay_ticks` what the lead
  currently is. It moves during a session now (§3.5): `playout lead renegotiated`
  on the authority and `playout lead adopted` on every instance name each change,
  and the two ticks must agree across the session;
- `network.listening`, `network.chain`, and `network.rejoin_attempts`: whether this
  instance bound a port of its own, how many succession candidates it holds, and how
  far a survivor with no link has walked the succession list;
- `energy.passive_count` and `energy.passive_drained`: the one energy delta class
  no player action produces, which is what separates a stopped shield drain from a
  working one;
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
stop or mutate only one copy of a live session. The programmatic surface refuses
the same things for the same reason: `App.SetupLevel` and `App.Region` carry
`ClassShared` payloads, which `event.OnWire` never transports, so no participant
may originate one in a live session — the authority included. `App.Reset` still
crosses, because its type is `ClassBus` and a crossing is what it becomes.

The status bar renders the whole of this as **one badge**, chosen by severity, so a
worse fact hides a lesser one rather than sitting beside it: `Host lost`,
`Migrating [n]`, `Net: down`, `Net: wait`, or `Net: <peers> <round trip>` with at
most one qualifier — `slow!`, `desync <ticks>`, `loss <pct>%` or `slow` — and a
background that is green, amber or red by the worst of the round trip and that
qualifier. The rest of the measurements — jitter, cadence, keyframe interval,
byte rate, the D-14 latch — are read in the status snapshot and in `:session`,
where they can be compared against each other.

## 8. Remaining gaps

Reviewed against the code on 2026-09-07, and again after the closure below. Each
entry says what is actually absent rather than what is imperfect, and how to see it.

### Deferred by decision

1. **Authentication and confidentiality.** Links are plaintext and a session is
   reached by its address alone. Participant claims, loss notices, handoff records
   and peer links are structurally checked but not authenticated; the rules prevent
   races, not a hostile peer. The deployed fleet accepts this and hardens the open
   port instead; see the [fleet plan](kubernetes-fleet.md) §4 for what bounds a
   stranger today and what does not.

   §5.3 enlarges the surface and is stated as such rather than partly mitigated. A
   migrate session now has one listening port per participant instead of one per
   session, and the addresses of all of them are published inside it. Everything on
   those ports is refused unless it names a participant the receiver's own roster
   holds and a term not behind the one it holds — which is the same structural
   check the rest of the protocol makes, and the same non-answer to a peer that can
   claim another's identity. `-no-advertise` is the setting for a participant that
   would rather be a leaf, and `-authority host` for a session that would rather
   have one port.

### Closed since the last review

2. **The playout lead is now measured, and re-measured.** `BarrierDelayTicks` travelled from
   `SessionOffer` through `HandoffRecord` to `NetworkSystem.delayTicks` and every
   writer put the same constant in it. It is derived at lobby close from the worst
   measured round trip — the round trip plus a reordering allowance, times the hop count,
   floored at the constant and capped at `NetworkBarrierMaxDelayTicks` — and
   published as `network.barrier_delay_ticks`. It was chosen once because no
   artifact between offers and handoffs could tell anyone it had changed; §3.5's
   barrier-bound crossing is that artifact, so the authority now re-derives it
   between ticks and the floor is a measurement rather than a constant. A session
   that closes before a probe completes still keeps the constant, which is the
   answer it had before.

3, 4, 5. **Reachability, and what it makes decidable.** A guest binds and declares,
   the coordinator publishes the whole chain, and every participant dials the current
   successor (§5.3). Migration now moves the session rather than only its authorship,
   and the succession rule reads the chain, so a leaf is skipped rather than elected
   into a game of its own.

   Gap 5 closes with it. Every participant holds the whole chain, so losing the
   authority *and* the participant elected after it leaves the rest walking the
   list rather than forking; a link opened after the record was flooded asks for it
   rather than timing out. Only a session whose survivors are all leaves still
   forks, which is the leaf's own decision.

6. **The programmatic operator surface is closed.** `App.SetupLevel` and
   `App.Region` now go through the same guard `App.Reset` does, and the guard reads
   the event's own declared class, so a method added later inherits the right
   refusal from its type. The plan proposed refusing on a guest and crossing on the
   authority; `event.OnWire` admits `ClassBus` and `ClassStamped` and nothing else,
   so a `ClassShared` event reaches no peer whoever pushes it and the authority is
   refused too.

7. **The domain exemptions are named.** The thirty ambient-Shared pushes of
   local-class events are pinned by `TestAmbientLocalPushesArePinned` rather than
   fixed, because fixing them is thirty gameplay judgements and not one refactor —
   what the pin buys is that each is deliberate and a new one is a test failure.
   `event.EmitDeath` stays where it is: moving it to `World` was a preference, not
   a boundary fix.

   What is left of gap 7 is the last item on its list: `combat.` and `kills.` are
   excluded from the compared shared surface because they aggregate both domains
   into one set of counters. Splitting them per domain is what would let them back
   in, and that is a telemetry redesign rather than a boundary fix.

### Open

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

11. **Closed: a producer's own crossing no longer lands a lead before the
    authority's copy.** Every copy applies at the agreed tick (§3.1), so the offset
    the first correction after a hit used to remove — the producer integrating an
    impulse the authority had not — is gone, and with it the phase difference in
    the kinetic immunity window. What a hit still costs is the lead itself, paid
    as the distance between the D-18 prediction and the store rather than as input
    latency. What remains is a two-attacker residual: a crossing that misses the
    lead is applied late by the authority in arrival order, so which of two hits
    on one body `SpendKineticImmunity` reports as opening the window can still
    differ for the length of that lateness; it is in `doc/todo.md` as a choice
    about who owns the override.

## 9. Verification

The automated suite covers domain boundaries, deterministic continuation,
two-participant and mesh convergence, selective repair and fallback, replay
retention, correction ordering, join/reconnect, link shaping, relay retention,
authority succession, the playout lead's choice over a shaped link, the correction
playout buffer, the knockback an artifact rather than a stream position determines,
the identity an explosion's re-derived hits inherit from it, the per-attacker window
and additive join that make two participants' knockbacks compose the same way in
either order, the one reward a shared death pays however many times a rollback makes
a guest derive it, the lead a measured link narrows the session to and the tick every
barrier adopts it on, the link the ceiling drops rather than absorbs,
the domain a capture's RNG streams may carry, the placement budget and
the storm burst a shared stream draws before it reads a live position, the owner-state
sync an install must not stall, the agreed tick a shared-identity crossing waits for
on its own producer, and the
peer link and succession chain rules that make a successor reachable. It also forces a capture to enter and retire a quasar while
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
  authority, `network.migrations` reads 1, and the `Host lost` badge clears — and
  the host's cursor must be gone from its map rather than standing where it was
  left. A guest that could not reach the successor shows `Host lost` and keeps
  playing instead, which is the fork.

Reachability is worth a third, on a session of three or more, because it is the one
path a two-terminal session never exercises:

```sh
./bin/vif -serve 127.0.0.1:7777 -authority migrate -d -size 120x40 -l -lv info
./bin/vif -join 127.0.0.1:7777              # and again, from a second terminal
```

Each guest logs `peer listener bound` with the port it got — the second one falls
back to an ephemeral port, because the first took the host's — and the host logs
`succession chain published` as each is added. On each guest `:session` names how
many candidates the chain holds and whether this one is listening. Kill the host:
the survivors elect the chain's first survivor and continue in one session, rather
than each continuing alone. Repeat with `-no-advertise` on the first guest and the
second is elected instead.

## 10. Where this came from

This section is the record of the decision, condensed from the 2026-08-30
desynchronisation diagnosis it replaces. It is history and the option space, not a
description of the runtime; §1–§9 are that.

### 10.1 The incident

One instance's log, `vif-log-260830-192013.jsonl`, over 2,186 ticks:

| First reported tick | First category | Outcome in the trace |
|---:|---|---|
| 792 | kinetics | escalated at 810; agreement restored at 822 |
| 1,146 | kinetics | escalated at 1,164; agreement restored at 1,200 |
| 1,704 | positions | escalated at 1,722; persistent through exit |

`late=0`, no transport loss, no dropped queue records: nothing in the transport
explained it. The permanent mismatch began at 1,704 and `StormSetup` at 1,773, so
the storm amplified a difference rather than causing one.

The cause was found in the local view. Binding the local participant emitted
`EventCursorMoved`, which dirtied the throttled navigation cache on that instance
only, shifted its recomputation phase, and produced kinetic differences a few
hundred ticks later — the progression from kinetics to positions, and the apparent
recovery when affected movers died. That is D-17, and
`TestLocalViewChangesLeaveTheFlowFieldPhaseAlone` pins it.

The run also dimensioned the problem: at tick 2,000 (100 s) about 256 entity
creations and 247 destructions per second, 10.4 local and 20.6 received crossings
per second, and a shared positioned high-water of 500. Those numbers are why the
correction path is a capture with a selective exchange over it rather than a
per-frame input stream: the artifact stream is small and the world is not.

### 10.2 Why determinism alone was not enough

Deterministic re-simulation reduces bandwidth and makes defects observable, but it
is not a continuity protocol. A deterministic program still diverges when an
artifact is lost, arrives after its apply tick, or is applied twice; when local
input, a timer, iteration order, floating-point behavior or a cache phase leaks
into shared state; when a participant is gone beyond retained history; or when
partitioned groups continue separately. Journaling reproduces a run; it does not
recover one. Recovery needs an authority, a checkpoint identity, a complete state
format, a retained ordered suffix, and rules for the local state deliberately left
out of it.

### 10.3 The options, and why this one

Scored 1–5 for this codebase, weighted: domain fit 25%, recovery correctness 25%,
delivery tractability 15%, bandwidth 15%, latency 10%, continuity 10%.

| Option | Fit | Recovery | Tract. | Bandwidth | Latency | Continuity | Weighted |
|---|---:|---:|---:|---:|---:|---:|---:|
| End the session and restart | 5 | 1 | 5 | 5 | 3 | 1 | 3.40 |
| Replay the whole log from tick zero | 5 | 3 | 4 | 4 | 1 | 2 | 3.50 |
| Strict delayed lockstep | 4 | 3 | 3 | 5 | 1 | 2 | 3.25 |
| **Host keyframe + canonical suffix** | **5** | **5** | **3** | **4** | **3** | **4** | **4.25** |
| Periodic full-state stream | 3 | 5 | 3 | 2 | 4 | 4 | 3.55 |
| Authoritative delta stream | 3 | 5 | 1 | 5 | 4 | 4 | 3.70 |
| Input prediction and rollback | 2 | 4 | 1 | 4 | 5 | 3 | 3.05 |
| Dedicated authoritative server | 2 | 5 | 1 | 3 | 4 | 4 | 3.15 |
| CRDT / eventual merge | 1 | 1 | 1 | 4 | 5 | 2 | 1.95 |

Lockstep turns network variance into a global stall and still needs reconnect
state. Full state streaming makes host uplink scale with clients. Rollback wants
cheap frequent saves, bounded side effects and many-tick re-simulation, which a
high-churn ECS with an FSM over it does not offer; vi-fighter sends resolved
crossings rather than a compact per-frame input stream, so it buys less here than
it costs. CRDT merging cannot order collision, death and contested progression.

What was built is the winning row plus the two things the table does not score:
guests keep simulating between corrections, and the correction itself is selective,
so a converged link costs hashes.

### 10.4 Primary references for the patterns

- Ensemble's [Age of Empires lockstep account](https://www.gamedeveloper.com/programming/1500-archers-on-a-28-8-network-programming-in-age-of-empires-and-beyond):
  small commands over a deterministic simulation, determinism as a strict contract.
- Valve's [Source multiplayer networking](https://developer.valvesoftware.com/wiki/Source_Multiplayer_Networking):
  authoritative snapshots, deltas from acknowledged baselines, full fallback,
  interpolation, prediction, lag compensation.
- The [GGPO developer guide](https://github.com/pond3r/ggpo/blob/master/doc/DeveloperGuide.md):
  rollback's prerequisites — deterministic simulation, fully serializable state,
  load/save, and frame advance without rendering.
- Unity Netcode's [client prediction](https://docs.unity3d.com/Packages/com.unity.netcode%401.4/manual/intro-to-prediction.html):
  applying an authoritative snapshot and rolling predicted entities back.
- Unity's [host migration](https://docs.unity.com/en-us/mps-sdk/session-host-migration)
  and [host election](https://docs.unity.com/en-us/mps-sdk/session-op-host): capturing
  and applying synchronised data is a separate problem from choosing a new host.
