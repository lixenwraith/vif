# Multiplayer

This document is the whole of multiplayer except the domain rules, which live in
[Multi-instance domain model](domain-design.md). §1–§9 are the runtime as it
exists: the contract, the correction and ordering boundaries, authority
continuity, the operating point, the diagnostics, and what remains. §10 is where
the protocol came from — the incident that prompted it and the options it was
chosen against — and is history rather than a description of the runtime.
For hosting, joining and allocator requests during play, see the
[configuration menu](config-menu.md#multiplayer).

## 1. Vocabulary and boundaries

Use these terms consistently:

- A **network peer** is an endpoint exchanging session frames. "Peer" is
  relational, not a second namespace: a link is addressed by the identity of the
  participant on its far end, so `network.PeerID` is both who and which link, and
  no instance is ever its own peer.
- The **game host** owns the canonical Shared-domain world, session identity,
  roster, and correction stream. It is the peer currently authoring, which is
  peer 1 until a handoff moves authorship.
- A **game guest** runs the same Shared simulation as a predictor and adopts host
  corrections. It is not a thin renderer.
- A **relay** carries the raw artifacts of the participants behind it up toward
  the authority and the authority's committed copies and corrections down to
  them, retains authoritative captures so it can answer their selective repair
  requests, and reports their proofs upstream. It authors nothing. A guest is the
  relay of its own bots, and with `-relay <addr>` of the people who join there.
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

Two numbering spaces, each with its own reserved zero. A **participant identity**,
which every peer holds despite the name, runs `1..MaxPlayers+1`; zero means *no
participant*, which is what makes an absent crossing fence, an unused epoch
window and every `id == 0` guard answer the same way. A **roster slot** runs `0..MaxPlayers-1` and names a cursor, with
`NoPlayerSlot` for a peer that drives none. Slot zero is the first cursor
rather than the local one — an instance identifies itself by its participant
identity, and giving it a second name would cost the identity space its sentinel.

## 2. Current contract

| Area | Current behaviour |
|---|---|
| Local input | The producer stamps every crossing with its own lead and applies its own copy at that tick; the authority commits it there, or at its own next tick when it arrives late, and every other instance applies the committed copy (§3.5, §8 item 5). The D-18 prediction answers the keystroke and the weapons fire from the predicted cell; the shared store moves when the crossing applies. A typed gold member leaves at its tick like everything else; the typist validates its next keystroke against the run without the members it has already typed. |
| Shared authority | The host's Shared world is canonical. A guest's predicted result is provisional until the next correction. |
| Player state | Each instance simulates only its Player domain. Owner-authored cursor values have one writer and travel as values; a receiver keeps the values it authors across an install. |
| Predicted derivations | A shared death a predicting instance derives is stamped `PhasePredicted` and held in a ledger. Presentation follows it; the player-domain rewards behind it are paid once, when an authoritative world proves the entity gone. |
| Global environment | Shared wind events are re-derived rather than sent. Every instance consumes two draws from the same Shared environment stream, applies the gust to its own drains and predicted Shared species, and restores active wind state through corrections. Glyph drift is re-derived the same way and draws nothing: each instance steps its own glyphs from the same per-level phases, which corrections restore without moving a glyph. |
| Corrections | A correction starts with a versioned hash index. Equal roots send no state. Mismatches descend to independently proved pages. Compressed whole keyframes remain the bounded fallback. |
| Correction playout | The clock never moves backwards. A correction behind this instance's tick is projected: simulated forward in the staging world, over what this instance applied since, and written at the present. One ahead of the clock within `NetworkHoldTicks` waits for its tick; the newest waiting one wins, and one further ahead is taken as the jump it is. |
| Replay | A guest retains a bounded suffix of its own crossings and of what it applied from others; a projection re-applies each past the installed capture's fence for its source, at its own tick. |
| Crossing ordering | The capture header carries one applied-sequence fence per participant. A receiver removes ordinary frames the installed world already holds — including ones whose nominal receive tick is still ahead — and keeps the ones it does not, including ones whose receive tick is long past. |
| Local FSM lifecycle | A live install replays only config-marked persistent `ClassLocal` exit/entry events for crossed state paths; staging and all ordinary actions remain side-effect free. |
| Join and reconnect | A running game can begin hosting; join and reconnect install a current capture through the same staging path. Every host arms the same mid-run gate once its own lobby is done, so a reconnect takes one path whether the session started with `-host`, `-serve`, a script, or `:host`. |
| Roster | Every admitted peer holds an identity; a roster slot binds it to a cursor and makes it a participant. The coordinator of a dedicated host holds no slot, so it is a peer and not a participant, and a session of one guest has one participant in a roster of two. |
| Cadence | Each direct link gets a bounded correction plan derived from round-trip time, variation, delivered bytes, saturation, and correction demand. The whole-world convergence floor is fixed: a join whose link cannot carry one whole world per floor window is refused (`Corrections.AdmitLink`). Past admission it is reported as `snapshot.cadence_floor_breached` (D-24): by the authority for a link whose plan cannot hold it, and by a guest no whole world reached within the floor plus its grace. |
| Playout lead | Per participant (§3.5): the worst recent round trip to the authority plus a tick of margin and a relay tick, less how late the authority's epochs land on it; behind a relay, the round trip is how long its own epochs take to come back committed. The authority's is one tick. Nobody defers by anyone else's. Changes are local, journaled events. |
| Commit and pacing | The authority commits every guest crossing — as stamped, late at its next tick, or void past `NetworkCommitLateTicks` — and relays only the committed copy. Every guest paces its tick interval so the authority's epochs land inside its band. A participant whose own crossings stay late is evicted by policy (§3.5). |
| Mesh and relay | The session is a tree rooted at the authority. Raw epochs and syncs go up a participant's one link toward it — the authority, else the relay it joined through or adopted — bundled with those of the participants behind it; committed epochs, owner state, correction chunks and the producer's own committed copy go down, to the links that send raw artifacts or were served a join; succession records and departure notices flood every link. Each has its own duplicate suppression (epoch and sequence windows, sync sequence, chunk assembly, record content). A relay serves selective repair to the participants behind it from retained authority content, forwards its own committed copies to them, and names each with the tick it last proved in its own answer, which the authority's floor and flood count as a direct answer. The authority takes a participant's raw artifacts on its own link, else its holder's, else — a person behind a relay — on any. |
| Reachability | In a migrate session a guest binds a port of its own and declares it, and the coordinator publishes the whole succession chain on `MsgPeerList`. Every participant holds a link to the current successor. `-no-advertise`, a failed bind, or `-authority host` leaves a participant a leaf: it plays normally and is elected only when the chain names no survivor (§5.1, §5.3). |
| Host loss | `-authority migrate` (default off `-serve`): **the first survivor in the succession chain** takes the next term, with no vote, because every survivor computes it from state it already holds identically. `-authority host` (default on `-serve`): nobody takes it and every survivor continues alone. |
| Trust | Links are plaintext and unauthenticated by decision. What the coordinator *does* check is identity: a joiner reports its protocol, simulation fingerprint, capture and journal schemas, tick interval, and the seed, session and scenario digest its built world carries, and a peer that does not match the offer is refused before it takes a roster slot. The seed, session and scenario are adopted from the offer first, so that half catches a world that came out different; a scenario no local root holds arrives over the wire, as does any but the built-in one under `-d`. The corpus is neither adopted nor compared: glyphs are player domain, so each participant reads its own and a peer with different text still joins. |
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
receive buffer for reordered traffic and never a difference between two worlds: a
producer that applied its own copy at once would have every correction read inside
the lead undo the kill, rewind the knockback and tear down the region's species,
then derive them again. The exception is a crossing that reaches the authority past
its `ApplyTick`: the authority commits it at its own next tick, or voids it past
`NetworkCommitLateTicks`, and its producer alone keeps the stamped tick (§3.5, §8
item 5). Input feel is the D-18 prediction's job, not the store's
([D-18](domain-design.md#d-18--local-prediction-is-private)), and for a typed gold
member it is the typist's own record of what it has typed, which the next keystroke
validates against.

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
| `EventCursorDefeatState` | folds into `session.any_defeated`, which `MonitorGlobalReset` answers with a new game |

The last two are the least obvious. Applied a lead early on its producer, a ninth
drain would enter the escalation and build that region's world before anyone else
had it, and a downed cursor would run `MonitorGlobalReset` alone — cancelling every
region and clearing the level — until the next correction, published while the
authority was still in `MonitorActive`, put the pre-reset world back.

Nobody's input waits on any of these. What the player did is the drain dying or the
cursor going down, both Player-domain and immediate; the shared consequence waits
the same lead everywhere instead of on all but one instance.

A latch the barrier feeds needs a second rule: whatever the agreed artifact sets
must be cleared by the world it rebuilds, not by a later artifact. `MetaSystem`
clears the defeat latch on the `EventLevelSetup` that carries `clear_entities`
without `preserve_loot`, and ignores reports produced before it, because the
un-defeat crossing arrives a lead after `MonitorArm` has already re-armed the
roster — long enough for the guard to fire a second reset.

Wind start/cancel are the opposite case: they are `ClassShared` outputs suitable
for a Shared FSM transition. They appear identically in every journal but never
on the wire, because a transported copy would duplicate the transition's local
output. Wind variation is sampled once per tick, before entity iteration, so a
participant's private drain count cannot perturb Shared RNG order. If a future
Player mechanic starts wind, it must cross a separate Bus request whose Shared
resolution installs the wind at the agreed tick. Drift start/cancel are the same
kind of output and take the same path.

### 3.2 The capture boundary

`ApplyTick` does not say whether a capture contains a given ordinary frame: a guest
whose link misses the lead produces a frame for T+3 that has not reached the
authority when it reads its world at T+9, so the capture is missing one whose
`ApplyTick` is already six ticks old — and the authority applies it late when it
arrives, in the order it arrives.

The capture header therefore records `CaptureHeader.Crossings`: one fence per
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

The sequence rule is the whole rule for ordinary frames — the tick never decides
one — and the tick rule is the whole rule for barrier-bound ones: they apply at one
agreed tick on every instance including their producer, so the tick is exact for
them and nothing else is needed.

This closes the guest-action pattern — a correction undoes the player's own
keystroke, and the next one puts it back a cadence later — because a frame the
capture never saw is kept even when its receive deadline is long past. The
host-cursor pattern — a correction installs a new host position and queued older
absolute positions then walk the guest backward — is closed by construction: the
authority applies its own frame at the tick every copy applies at.
The same fences are retained after installation, so a stale batch arriving later
is classified the same way.

Two deliberate asymmetries in how the fences are computed:

- The **local** entry is a contiguous prefix rather than the highest assigned
  sequence, because local dispatch can complete out of order: a capture racing an
  input that has been encoded but not yet dispatched must not claim it. Completions
  that arrive ahead of a gap are held until the gap closes. An own crossing an
  install discards as already held closes its place too: the written world holds
  its effect, and nothing else would ever dispatch it.
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
holds this instance's owner-authored cursor values, and stamps by this instance's
own lead under the session's authority — and simulated forward to the live tick. The
live world is moved onto that projection at the tick it is on, so a transition the
guest has already made is neither torn down nor rebuilt, and the clock never moves
backwards. A projection the live world already equals on the compared surface and
in every other part is taken as a hash-only answer is: only the clock and the
barrier are written. `snapshot.projected_ticks` is the distance; a capture level
with the clock projects zero ticks and still takes the open epoch, which is where a
late-arriving record lives.

The projection is fed what the capture does not contain and this instance applied
after it: its own ordinary crossings past the capture's fence for its source, the
other participants' ordinary crossings past theirs, and the barrier-bound artifacts
due after the capture's tick, each at its own apply tick. Fences, not ticks, choose
membership; production ticks only bound retention age. A crossing still pending on
the live barrier is not fed — the barrier applies it at its tick, and an install
leaves the barrier and the D-18 queue alone. Retention is bounded by ticks, records
and bytes; if the local suffix has a hole, none of this instance's own crossings
are fed and the replay is reported skipped, rather than guessing at a partial
history; the others' and the barrier-bound ones still are.

The whole commit — projection and write — runs under the live world lock, so no
live tick can land between the tick the projection reached and the write.

A capture ahead of the clock is a join or a jump and is adopted at its own tick;
within `NetworkHoldTicks` it waits (§4.1).

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

Player-domain state is not: loot, the boost grant and every presentation burst are
in no capture and nothing rolls them back, so a second derivation would announce
and reward one kill twice.

The bridge is therefore phased. A Shared-domain `EventSpeciesKilled` raised while
`World.PredictsShared` — a live session in which somebody else authors — is stamped
`event.PhasePredicted` and its payload entered in a ledger keyed by the dying
entity. Recording is idempotent, so a re-derivation after a rollback adds nothing.
An install settles the ledger against the authority's world as installed, before
the projection re-derives over it, and leaves alone a death the capture predates.
An entity the authority does not have is proved dead and raised once as
`EventSpeciesKillConfirmed`, and one the authority still holds a convergence floor
later is dropped as the misprediction it was. So is one past the authority's shared
allocator: it existed only here, and the install re-issues its id to whatever the
authority spawns next.
`LootSystem` and `BoostSystem` consume the confirmed form and ignore the predicted
one; everything else consumes `EventSpeciesKilled` as a solo run does, because the
capture already reconciles what it holds.

Three cases close the edges. An instance that stops predicting — it took the term,
or the last peer went — releases everything it holds, because the world it predicted
is the only one there is. The session it reads is a latch (`World.LatchSession`)
that moves only between ticks — as one opens, and where the transport, a link or
the authority changes — so no derivation's phase shifts inside a tick, and each
change is journaled ([replay journal](logging-and-diagnostics.md#9-replay-journal)).
A reset drops the ledger, for the reason it drops the replay suffix. Past
`parameter.PredictionLedgerMax` the oldest entry is released unproved rather than
discarded: losing a player's progression to a bound is worse than paying it a
cadence early.

This is the edge-triggered half of what the FSM's `reconcile = true` lifecycle
replay does for the level-triggered half (D-19). Neither covers the other: a hold
that follows a Shared region is re-derived from the imported state, and a one-shot
reward is held until the state that caused it is proved.

### 3.5 Command frames, the commit and pacing

Each participant stamps its own crossings with the lead its own link asks for, and
nobody defers by anyone else's. A guest reads its lead every tick (`ownLead`): the
newest probed round trip to the authority (`linkpace.Metrics.LastRTT`) plus a tick
of margin on a reading one probe wide, plus `NetworkRelaySlackTicks` for the
authority to relay it, less how late the authority's epochs already land on it,
held between `NetworkBarrierMinDelayTicks` and `NetworkBarrierMaxDelayTicks`; the
authority's own is the minimum, one tick, and a guest not yet paced uses the default
`NetworkBarrierDelayTicks`. Behind a relay no link measures that trip, so the
reading is the return of the guest's own newest committed epoch, which reaches only
a participant behind a relay. The lead in force is the worst reading of the last
`NetworkBarrierRenegotiateTicks` (`leadOf`): it rises at once and falls only as the
higher readings age out of the window, to the highest still in it, so a momentary
trough is not climbed back a tick at a time and a passed spike does not step down
one tick a window. The readings rather than the smoothed trip and a jitter term: the
lead has to cover every round trip, and a smoothed swing read twice over carried two
ticks the worst trip never needed. Each change is a local, journaled
`EventPlayoutLead`, so a reproduction switches on the same tick.

The authority commits. A guest sends its epoch raw; the authority applies an on-time
crossing at its stamped tick, a late one at its own next tick, and one more than
`NetworkCommitLateTicks` late as void — applied nowhere but on its producer, which
already applied it, it only closes its source's fence, so the next correction takes
it back and its producer's projection stops replaying it. It relays the committed copy
with the ticks it chose. Every other participant applies another's crossings only
from that copy or the authority's own epochs, and passes a raw one on toward the
authority. A producer with a link of its own is heard only on it, and a bot behind
its holder only on the holder's; its identity on another is refused. A slow or
stalling participant therefore delays only its own actions and pays for the
corrections that repair its own early copies.

A guest paces its clock against the authority (`observeAuthorityEpoch`): it trims its
tick interval, by up to `NetworkPacePermilleMax`, until the latest authority epoch of
`NetworkPaceWindow` lands between `NetworkPaceBandTicks` early and
`NetworkAheadTicks` late, and steps whole ticks past `NetworkPaceStepTicks` of error
when the earliest epoch of the window shows it too, so one jittered arrival trims
rather than freezing the world. Epochs read in the `NetworkPaceServedTicks` before a
step is served are not sampled, since they measure the lateness it answers. Wall pacing only: the simulation step is fixed, so a trim shows as the world running a few
percent fast or slow while it settles. In the band every committed crossing reaches
the guest before its tick. `NetworkAheadTicks` lets a guest run ahead: each tick
shortens its own lead by one and lands everyone else's crossings on it up to a tick
late, repaired by its own corrections alone. It is zero: measured at 150 ms, a tick
ahead saved at most one tick of lead and tripled the corrections that moved a
placement on the guest that ran it.

While authoring, the authority evicts a participant whose epochs, over
`-slow-window`, reached it late at least `-slow-late` times a second while its link
carried at least `-slow-bytes` a second (`driveEviction`). Epochs rather than
crossings: a player sweeping the pointer is not a slower link than one standing
still. The first window after arrival is grace, and only a participant's own raw
epochs count, so its actions relayed to the others never mark them. A link is
evicted with the bots it holds once what it sends over the window passes
`NetworkIngressBytesPerSecond` for itself, each bot it holds and each person the
authority has no link to (`driveIngress`). `-slow-window 0` turns off both. The
authority sends every committed copy down each of its links, the producer's
included, so a producer knows which of its epochs were committed. On a handoff a
guest resends to the successor, in its next epoch, the retained crossings of the
epochs that never came back (`ResendUncommitted`); the successor commits them late
and refuses by sequence any it already holds from the predecessor's copies, so a
lost authority costs a player only what the commit bound voids. Every receiver
takes each source's crossing sequence once (`NetworkSequenceWindow`), whatever
epoch carries it.

## 4. Correction pipeline

The host publishes one authoritative capture on the session timeline, then serves
each direct peer according to that link's cadence:

1. Build a deterministic manifest over component stores, allocator values, RNG
   streams, declared system state, status surface, and shared FSM state.
2. Send the root and header alone.
3. If the receiver produces the same root, acknowledge a hash-only correction and
   install the authority header without transferring state. That answer proves the
   receiver holds the authority's world, so while every peer has proved it within
   half the convergence floor the keyframe cadence sends none. A participant behind
   a relay proves it in the relay's own answer.
4. Otherwise the receiver asks for the section summaries, compares page hashes only
   in differing sections, and the authority returns the pages that differ. A repair
   wider than one frame or than the keyframe it stands in for is not sent: the peer
   gets the keyframe, and whole bodies for a few publications.
5. Validate every page hash, reconstruct the authority root, stage the result and
   commit it between ticks. A set is one baseline, applied whole or not at all.
6. Refuse stale, foreign, malformed, or unverifiable repairs. One that fails its
   proof or its install asks its manifest's sender for a whole world. A relay
   cannot author one, so it answers `MsgStateUnserved`, as it does for a tick it
   does not retain, and the receiver takes the next whole world the authority sends.

The environment is a declared system-state carrier. A capture therefore includes
its base force, normalized direction, remaining duration and application phase;
the general RNG section includes the environment stream position, and Shared
component pages include the species kinetic result. Player drains remain local
and resume from the same per-tick samples after an install. The capture also
carries an active drift and its per-level phases; installing them moves no glyph,
so each instance's own text continues from where it is.

The owner-authored cells of every cursor a participant owns, the authority's
included, are outside the hashed surface: each instance holds another's as a mirror
up to `NetworkSyncTicks` old, the sync stream is their carrier, and comparing them
would have a guest whose peer is fighting repair nearly every manifest. Cursor control
assignment is normalised for hashing and repair. The correction magnitude leaves
out the same cells, and all of them are re-bound locally after installation.

The manifest root intentionally excludes tick-local header metadata so a predictor
can compare state with an earlier authority tick. A shard set must nevertheless
match the manifest's authority term, participant, and crossing fence; this binds
the ordering metadata used to prune queued events. Its capture integrity may
differ on a relay whose equal canonical state has another dense-store order.

Nothing acknowledges or retransmits a correction; the newest in the run supersedes,
so loss costs freshness rather than correctness. A reset restarts the tick count, so
on a run change each end drops what it keyed by tick and refuses an artifact naming
another run: a correction by its header (`Corrections.followRun`), an epoch or owner
sync by the run its batch names, since a guest's last epochs of a run reach the
authority a round trip after the reset applied there. The run after this one is
the exception: its first world reaches a guest whose reset is still on its way, so
the chunk header carries the run, the assembly orders transfers by run then tick,
and `Apply` keeps such a body at the head of its inbox until the reset enters it. A body leaves in
`SnapshotCorrectionChunkBytes` chunks while the link's backlog — bytes sent beyond
what its latest echo reports received — drains within `SnapshotBulkQueue` past the
round trip, so an epoch waits behind a chunk, not a world. What a link still
delivering one is owed next, whether body, index answer or repair, waits behind it,
the newest replacing the older and nothing partial replacing a whole world, and
leaves once the link drains, released between every two ticks rather than by the
next publication: a late or lost frame costs a round trip, never a stall. A
request naming a tick the authority has let go was overtaken by the index since and
is not answered unless it asks for a whole world, nor is one naming a tick the peer
has since proved it holds, whose answer the receiver would refuse as outgrown — as
it refuses an index or a repair no newer than the world it last proved, rather than
comparing it off its tick. A section index for a tick a receiver is awaiting a
repair for is answered beside a newer root, so a cadence shorter than the descent
does not strand it. Indexes to a peer are spaced by its round trip, so a descent's
second request finds its tick retained. A link that
cannot deliver a keyframe within `SnapshotKeyframeDrain` takes none on schedule: it
meets the floor with proofs, takes a whole world on request and reports
constrained. Echoes carry the answering side's clock, and delivery rate is
timed on it, since echoes a stalled path releases together arrive microseconds apart.

### 4.1 The playout buffer

The paths deliver worlds of different ages: a whole body is one one-way delay old,
a selective repair three. Adopting each capture's tick would step the clock by up
to four one-way delays and every shared actor with it, so a capture behind the
clock is projected (§3.3) and moves nothing; the buffer is for the other direction.

A correction describing a tick this instance has not reached, within
`NetworkHoldTicks`, is held until it has. A manifest ahead of the clock is held the
same way, and the world it is compared against is read at the close of exactly the
tick it names (`Corrections.TickClosed`, under the tick's lock), so a guest that runs
past that tick before the corrector answers still answers for it. Each manifest also
promises the tick of the next one (`CorrectionManifest.Next`), which a guest reads
as it closes too, so an index that arrives late is still compared tick-exactly. One
whose tick was read neither way is compared against the present and counted in
`snapshot.manifests_off_tick`; the repair it buys is correct but carries state a
tick-exact comparison would not have. The newest arrival while one waits replaces
it, so at most one is ever delayed and never by more than the hold window. One
further ahead is adopted at its own tick as the jump it is, counted in
`snapshot.corrections_jumped`, and supersedes the one still waiting: installed
after it, that one would rewind the world past every crossing applied since. A
hash-only acknowledgement behind the clock is not adopted: the world already equals
it, and adopting its tick would be the rewind.

The host publishes at the close of the tick it describes rather than on a timer's
phase: `TickClosed` wakes its pump, so an index leaves with the epoch of the same
tick and reaches a paced guest before that guest runs the tick.

## 5. Membership, topology, and authority continuity

Roster changes are shared artifacts produced by the coordinator. A direct
neighbour reports a disconnect; the authority turns it into one departure at one
apply tick. A mid-run join is admitted to transport before its capture is read, so
traffic produced during transfer is buffered rather than lost. The join waits for
a capture far enough past admission to include pre-admission epochs and refuses a
catch-up gap past `NetworkJoinCatchUpTicks`, or one still wider than
`NetworkJoinLagTicks` (the default lead) once it has caught up; a bot seat retries
either, since a fresh attempt brings a fresher world.

The session is a tree rooted at the authority: the shipped CLI builds a star, a
guest seating bots or relaying people (`-relay`) adds a level, and in a migrate
session every participant also holds a peer link to the successor (§5.3). Every
link is up, down or beside. A participant's **up** link is the authority if it
holds one, else the relay it joined through (`NetworkResource.Relay`) or adopted;
its **down** links are those that send it raw artifacts or were served a join
within the epoch window, every link at the authority. Raw artifacts go up, one
bundle a tick; committed epochs, owner syncs and correction chunks go down, each
admitted once within a bounded window; a peer link carries succession records and
departure notices, which still flood. Corrections retain the authority's term,
tick, hashes, and chunks across relays.

A relayed join — a guest's own bot on an in-process stream (`relayLink`), or a
person at the guest's `-relay` address — is served by the guest as a coordinator
would: it asks the authority on its own link to allocate the identity and, once
the joiner installed the world the guest last proved, to cross its arrival with
what the joiner reported (`MsgRelayJoin`). A bot declares its holder and dials no
peer links, since it shares its holder's fate; a holder departure retires its bot
group at the same crossing. A person declares no holder: it keeps its slot when
its relay leaves, and, having no link then, walks the chain like a survivor does
and plays on through whichever participant answers (`Reach.cutOff`,
`NetworkSystem.sendUp`). It is a leaf unless `-listen` names an address, since it
could not reach the host itself. The holder identity is retained in the roster and
Shared cursor across captures, resets and succession. Hosts may remove any remote
slot with `:player drop`; removing a bot alone leaves its holder and siblings.
`:bot drop` also lets a guest remove its own bot. Both accept decimal or hex slots.
Holder checks are structural; they do not authenticate a peer. The player's view of
these rules is [Bots](bots.md#ownership-and-leaving).

### 5.0 Who listens, who authors, who can be reached

Three questions the rest of this section depends on, and they have different
answers:

| Question | Answer |
|---|---|
| Who binds a port? | The coordinator always: the instance started with `-host` or `-serve`, or a solo run that opened itself with `:host` or, to seat bots, on loopback. In a **migrate** session every other participant binds one too — `-listen` pins it, the default is the coordinator's own port, and a machine already using that port falls back to an OS-assigned one. `-no-advertise`, a bind that fails, a bot, a browser build, a person who joined through a relay without `-listen`, and every `-authority host` session leave a participant with no port of its own. A joined participant may also bind a `-relay` address, which admits people who cannot reach the host. |
| Who authors? | The participant holding the current term. It is the coordinator until a handoff moves it, and a handoff moves *authorship only*. |
| How is a participant reached? | The coordinator is reached by `-join <address>`, or by the `vif://address/name` link a deployment issues when one address serves several sessions, or through a guest's `-relay` address when the coordinator's cannot be; there is still no discovery and no rendezvous. Everyone else is reached through the **succession chain**: every participant that declared a port, in join order, with the address it declared. The coordinator publishes it whole on `MsgPeerList` and carries it in the offer and the handoff record, beside the roster. |

A successor is usually on another machine and cannot bind its predecessor's
address, so a successor that authors must already be reachable at an address of its
own (§5.3). Nothing is rebound: the predecessor's address is not reused, so there is
no `TIME_WAIT` race to back off from and no window in which an address is
half-released. What moves is which address the survivors dial, not which address
answers.

`-authority host` is the default for `-serve`. There the address *is* the session:
an orchestrator replacing the pod at the same address is the reconnect its guests
actually want, and a guest's own port would be for nothing. It is also the setting
for a deployment where the world may only ever live on the machine that started it.

### 5.1 Succession

If the authority disappears, the successor is **the first survivor in the
succession chain**, falling back to the roster's lowest surviving independent
identity when the chain names nobody still present. Bot leaves are not candidates.
It is a pure function of the chain, the roster and the participant that went, all
three of which every survivor already holds, so every survivor names the same
successor without exchanging anything. The chain is
append-only, so a survivor holding a prefix of it elects the same participant as one
holding the whole — which is why it needs no agreement step.

A *local* dial result is never an input, and that is the rule that matters most:
if each instance filtered candidates by what it could reach, two survivors would
compute two successors, which is the single outcome this design rules out. A
survivor that cannot reach the elected successor falls back to the timeout and
forks.

That determinism *is* the split-brain rule. At most one instance can conclude that
it is the successor, so at most one can ever claim the term — which is what a
quorum would buy, and what a quorum cannot buy here: a rule requiring a strict
majority of the closed roster elects nobody in the smallest session, where one
survivor of a roster of two is not a majority of two, and it is the whole session.

The procedure on each survivor is:

1. **Flood a loss notice.** It decides nothing, but only a direct neighbour of the
   authority sees the link drop, and the departure crossing that would have carried
   that news is produced by the participant that is gone. A survivor two links away
   opens the same succession from the notice.
2. **If the session allows the term to move and the succession names this
   instance, take it.** Immediately — there is nothing to collect and nobody to ask,
   and the session is stalled until somebody authors. The one self-check is
   retention: a successor with no retained authoritative record has no baseline
   for a delta to name and would answer the first manifest with a whole world for
   every survivor at once. A join capture counts as retention, so a guest admitted
   seconds before the loss is eligible. Holding the term, it starts its correction
   pump, a live script's or bot's included; only a harness publishes by hand.
3. **Otherwise wait for the record, dialling while it waits.** A handoff carries
   the roster with its slots and holders, the session anchor, the chain and the
   successor's evidence tick, so adopting it is one decision rather than a term
   change followed by a roster negotiation. A survivor with no link to whoever is
   taking over walks the succession list — every candidate once, the list again a
   second later, the attempt count on the status bar — and announces itself on each
   link it opens, so an authority elected before that link existed answers with its
   record instead of letting the survivor time out.
4. **After `parameter.NetworkSuccessionTicks`, give up.** No record arrived, so
   this instance cannot reach the successor and continues as an explicit local
   fork. Under `-authority host` it gives up at once.

Retention *ordering* is deliberately not an eligibility test. With one designated
candidate there is no alternative to prefer, and a successor a cadence behind a
peer costs that peer one correction.

A survivor that forks continues locally with `network.fork` and a persistent
`Host lost` badge; encountering a higher term later is refused because partition
merging is not implemented.

### 5.2 The roster after a loss

An instance left with **no link at all** — a successor no survivor reached, and
every survivor that could not reach it — drops every cursor it does not simulate:
the authority that went, and behind it the guests that were only ever reachable through
it. Having no link is what makes that exact rather than convenient: a departure is
produced once at one agreed tick because two instances must destroy the same shared
entity together, and here there is no second instance. On a fork the removal is
therefore local; the successor produces the predecessor's as the ordinary crossing,
because it may. A handoff preserves the surviving roster exactly; only the lost
predecessor and its bots may differ when their departures straddle the handoff.

A successor applies that rule once the succession window closes rather than at
once, so a survivor still arriving through a relay hop is not dropped. An instance
that still holds a link keeps its roster: it has peers to agree an apply tick with,
and on a fork no authority to name one (§8).

A departure, however it is produced, also clears what the instance had applied from
that participant. Identities return to the pool and a crossing sequence starts at
one, so a fence kept from the previous holder would claim a capture already contains
crossings the next holder of that identity has not produced — and §3.2's install
rule would then discard exactly those.

### 5.3 Reachability under `-authority migrate`

Under `-authority host` none of this applies: the address is the session, one
instance binds it, and authorship never leaves the machine that started the world.

1. A guest binds a listening port before it answers the offer, so it declares the
   port it *actually* bound. `-listen <addr>` pins one; the default is the
   coordinator's own port, so a session is one firewall rule and the port a guest
   opens is one somebody already chose to open. A machine already using it — two
   guests on one machine — takes an OS-assigned port instead. `-no-advertise`, a
   failed bind, or `-authority host` leaves a participant a **leaf**: it plays
   normally, declares nothing and is elected only when the chain names no survivor.
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
`HandoffRecord.Validate` refuses a record whose surviving roster — everyone but the
predecessor and its bots — is not identical to the receiver's. An `Addr` field on
that struct would make a guest that rebound its port look like a different roster
and fail every handoff.

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

Measured on the in-process mesh at `GOMAXPROCS=1` (a 2.1 GHz Xeon container), in the
shape of `TestATowerGuestAnswersHashOnly`: a star with four-tick links, the authority
playing, every participant moving one tick in three and firing one in six, 300 ticks
after 120 of warm-up. Rates are per simulated second; host and guest figures time
their own calls in one shared process, so garbage collection falls on whoever
allocates. Absolute values move with the machine and the scenario; the shape is what
to plan by. Tower is the 239×64 tower region, td the 500×250 `td` scenario.

| Players | Tower, 2 | Tower, 16 | td, 2 | td, 16 |
|---|---:|---:|---:|---:|
| Host CPU-ms/s: simulation + protocol | 18 + 77 | 38 + 121 | 146 + 267 | 188 + 1,181 |
| Mean guest CPU-ms/s: simulation + protocol | 23 + 32 | 43 + 60 | 159 + 251 | 219 + 312 |
| Simulation tick mean, host / guest | 0.9 / 1.2 ms | 1.9 / 2.1 ms | 7.3 / 7.9 ms | 9.4 / 10.9 ms |
| Publication rounds a second | 4.5 | 4.6 | 1.5 | 7.2 |
| Manifests answered hash-only | 94% | 94% | 78% | 95% |
| Host egress, framed before deflate | 20 kB/s | 1.64 MB/s | 0.28 MB/s | 5.05 MB/s |
| … of it epochs and owner syncs | 5 kB/s | 1.39 MB/s | 11 kB/s | 2.82 MB/s |
| … of it correction bodies and repairs | 11 kB/s | 0.17 MB/s | 0.27 MB/s | 2.18 MB/s |
| Heap in use per instance | 70 MiB | 90 MiB | 210 MiB | 250 MiB |

Per round the host captures under the world lock (about 1.2 ms tower, 14 ms td),
seals the capture (9 / 65 ms) and indexes it (4.5 / 30 ms); a keyframe is 80 / 516 kB
compressed and 9 / 65 ms to encode. The seal, the index and a keyframe reuse the
bytes and hash of every row whose owned copy still compares equal, and navigation
rebuilds its wall grid only when a wall moved and compares grids only when their
generation moved. The table and those seal, index and encode times were measured
without either, so they and the table's protocol and td simulation figures are high.
With both, at 16 players, rounds four ticks apart and on a slower container, the seal
costs 1.8 ms on the tower map and 5 ms on td, the index 2.4 and 12 ms, a keyframe
7.6 and 58 ms (mostly deflate), and a simulation tick 1.1 and 2.6 ms. An install that projects nothing
holds the live lock 7–10 / 40–70 ms, and each projected tick adds up to a simulation
tick. Sealing, indexing and encoding run outside the lock, but on the one thread a
browser guest has.

Per simulated second the host costs 20·T_sim + R·(capture + seal + index) plus the
whole bodies it serves, R being the union of its peers' due ticks rather than a fixed
5 Hz; a guest costs 20·T_sim + A·(capture + index) + installs·(stage + projection +
compare and write), A being the manifests it answers. A star's authority relays every
participant's epochs and owner syncs to every other, (N−1)² of each: at 16 players
4,444 epochs (190 B tower, 510 B td) and 736 syncs (760 B) a second.

What that means for scaling:

- **Protocol, not simulation, bounded the map limit.** At td with 16 players the host
  needed about 1.4 cores, 86% of it protocol: rounds of capture, seal and index cost
  about 100 ms each, 0.7 CPU-s/s at 7.2 rounds, and whole bodies most of the rest.
  Row reuse takes about nine tenths off the seal and index, which leaves the capture
  under the lock the largest part of a round. The fleet pod's 500m CPU and 160 MiB
  `GOMEMLIMIT` held the tower map at 16 players and not td.
- **A td guest costs 0.4–0.5 CPU-s/s natively,** which a browser's single WASM thread
  cannot hold; the tower map costs a guest about 0.1.
- **Bandwidth is quadratic before it is anything else.** Epoch and owner-sync fan-out
  is 85% of the tower host's egress at 16 players; at td whole bodies come close.
- **The live lock is held by the O(E) capture and comparison** before any projection,
  so shrinking them comes before moving projection out of the lock.

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
- `network.lag_ticks`, `network.stale`, and `network.barrier_late`: whether this
  instance is behind the session and applying crossings late, and
  `network.barrier_delay_ticks` its own lead (§3.5); `playout lead adopted` logs each
  change;
- `network.pace_late_ticks`, `network.pace_trim_permille` and `network.pace_steps`:
  how late the authority's epochs land on this guest, and what its pacing does about
  it; `network.commit_late` and `network.commit_void` on the authority count the
  crossings it committed late and voided, `network.commit_refused_raw` on a guest the
  raw epochs it passed on without applying, and `network.evicted` the participants
  the slow policy dropped;
- `network.listening`, `network.chain`, and `network.rejoin_attempts`: whether this
  instance bound a port of its own, how many succession candidates it holds, and how
  far a survivor with no link has walked the succession list;
- `energy.passive_count` and `energy.passive_drained`: the one energy delta class
  no player action produces, which is what separates a stopped shield drain from a
  working one;
- `network.transport_lost_in` and `network.transport_lost_out`: bounded queue
  refusal;
- `snapshot.cadence_*` and `network.link_*`: the selected operating point and the
  measurements behind it; a holder's `snapshot.relay_*`, `shard_bytes_sent` and
  `manifest_bytes_sent` include the loopback links of its own seats;
- `network.term_stale`, `network.term_refused`, `network.fork`, and
  `network.migrations`: authority continuity.

At debug level, `snapshot pruned crossings` records the capture tick, authority,
local fence, fence count, scheduled drops, sequence-fence drops, and pending local
drops. `snapshot refused crossings the installed world holds` records a batch that
arrived after installation already covered by its source's fence.
`import reconciled local lifecycle` reports the number of persistent local
exit/entry actions emitted by an FSM install. It should appear only when a
correction crosses such a boundary or changes a marked action's scope variable.

A healthy run may show a small non-zero correction magnitude; it should not show a
growing correction, persistent lag, repeated suffix unavailability, transport
loss on a healthy local link, or shared actors changing backward after a
correction.

Pause, speed, step, breakpoints and raw shared mutation (`:system`, `:emit`,
`:region`) are refused in a live session. They are instance-local operations and
cannot stop or mutate only one copy of it; a host whose only links are its own bot
seats may still pause (`SessionState.SeatsOnly`). The programmatic surface refuses
by the event's declared class (`App.shareOperator`): `App.SetupLevel` and
`App.Region` carry `ClassShared` payloads, which `event.OnWire` never transports, so
no participant may originate one in a live session — the authority included.
`App.Reset` is `ClassBus`, so it crosses on the authority and is refused on a guest,
as `:new` is.

The status bar renders the whole of this as **one badge**, chosen by severity, so a
worse fact hides a lesser one rather than sitting beside it: `Migrating [n]`,
`Host lost`, `Net: down`, `Net: wait`, or
`<players>P:<local hex slot> <round trip>` with at most one qualifier — `slow!`,
`desync <ticks>`, `loss <pct>%` or `slow` — and a
background that is green, amber or red by the worst of the round trip and that
qualifier. The rest of the measurements — jitter, cadence, keyframe interval,
byte rate, the D-14 latch — are read in the status snapshot and in `:session`,
where they can be compared against each other.

## 8. Remaining gaps

Each entry says what is actually absent rather than what is imperfect, and how to see
it. Planned multiplayer work is in [the todo list](todo.md#multiplayer).

### Deferred by decision

1. **Authentication and confidentiality.** Links are plaintext and a session is
   reached by its address alone. Participant claims, loss notices, handoff records
   and peer links are structurally checked but not authenticated; the rules prevent
   races, not a hostile peer. The deployed fleet accepts this and hardens the open
   port instead; see the [session fleet](kubernetes-fleet.md#4-security-posture)
   for what bounds a stranger today and what does not.

   §5.3 enlarges the surface and is stated as such rather than partly mitigated. A
   migrate session has one listening port per participant rather than one per
   session, and the addresses of all of them are published inside it. Everything on
   those ports is refused unless it names a participant the receiver's own roster
   holds and a term not behind the one it holds — which is the same structural
   check the rest of the protocol makes, and the same non-answer to a peer that can
   claim another's identity. `-no-advertise` is the setting for a participant that
   would rather be a leaf, and `-authority host` for a session that would rather
   have one port.

### Open

2. **Tower ownership binds a shared structure to one cursor.** A tower's
   `CombatComponent.OwnerEntity` is the cursor its spawn request named, which
   comes from the FSM's `player_entity` capture variable — one cursor for the
   whole machine. Damage attribution (`ResolveCursor(payload.OwnerEntity)` in
   `applyHitDirect` and its area-damage sibling) therefore credits one participant
   for a structure the session shares. There is no quick fix: the options are a
   hybrid shared/local shape like the cursor and its shield, or an explicit
   "every player" ownership value that attribution expands. **To be decided.**

   Quasar escalation gating on the session-wide `kills.drain` is not this gap: the
   FSM that reads the counter is authoritative and `MetaSystem`'s record carries it
   in the correction.

3. **Presentation is clipped, not wrong.** A terminal smaller than the session's
   map shows less of the map. It does not produce a different world or a
   misplaced cursor: `applyMapLatch` installs the session bounds before the FSM
   boot script spawns cursor slot zero, and `LockMap` latches the world shared for
   the whole run. Remote cursor motion is drawn at simulation arrival ticks, so
   it steps rather than glides.

   *To see the remainder:* `-serve :7777 -size 200x60`, then join from an 80x24
   terminal. The view is a correct 80x24 window onto a 200x60 world; every cursor
   is where the authority says it is. What is missing is a windowed composite that
   scrolls and optional presentation interpolation, both of which are presentation
   work independent of simulation ordering. If a *wrong* cursor position is ever
   observed across terminal sizes, that is a regression in the map latch and not
   this item.

4. **Cross-platform float drift is repaired live but not in replay.** A correction
   carries every shared component whole, float fields included, and a delta is
   proved lossless by re-hashing the reconstruction against the capture's own
   integrity hash — so a `float64` that drifted on another platform is repaired
   exactly like any integer that drifted. Two things remain:

   - The digest hashes float bits (`digest.f64` is `math.Float64bits`), so a
     one-ULP difference flips `network.digest_mismatches` and sets
     `network.drift_part`. That is diagnostic noise on a mixed-platform session,
     not a simulation fault.
   - A journal replayed on a different platform has no authority to repair
     against, so a float difference there accumulates. Replay determinism is
     guaranteed within one implementation build.

5. **A crossing that misses its lead lands later on the authority than on its
   producer.** The producer applies it as stamped and the authority commits it at
   its own next tick (§3.5). The knockback window covers every attacker, so the
   knockback an instance applies first holds it and the next correction repairs a
   difference. A kill is credited to the cursor whose hit killed it, so two cursors
   finishing one target inside that lateness can each be credited on their own
   instance.

6. **An instance that keeps a link after a loss keeps the whole roster.**
   `DropAbandonedCursors` acts only on an instance left with no link (§5.2). A
   successor that some survivors could not reach, or a fork still linked to its
   own bots, keeps every roster entry, and a participant it has no path to stays a
   cursor nothing moves.

## 9. Verification

The automated suite covers domain boundaries, deterministic continuation,
two-participant and mesh convergence, selective repair and fallback, replay
retention, correction ordering, join/reconnect, link shaping, relay retention,
authority succession, the commit, pacing and own lead of §3.5, the correction
playout buffer, the knockback an artifact rather than a stream position determines,
the identity an explosion's re-derived hits inherit from it, the knockback window
every attacker shares, the one reward a shared death pays however many times a
rollback makes a guest derive it, the eviction that drops only the participant that
stays late, the domain a capture's RNG streams may carry, the placement budget and
the storm burst a shared stream draws before it reads a live position, the
owner-state sync an install must not stall, the agreed tick a shared-identity
crossing waits for on its own producer, and the peer link and succession chain
rules that make a successor reachable. It also forces a capture to enter and
retire a quasar while the receiver skips the release transition, and round-trips a
delayed transition action by compiled identity. Run the generation and repository
gates after focused network tests:

```sh
go generate ./internal/manifest
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

Each guest logs `peer listener bound` with the port it got — an ephemeral one,
since on one machine the host already holds the default port — and the host logs
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
high-churn ECS with an FSM over it does not offer; vif sends resolved
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
