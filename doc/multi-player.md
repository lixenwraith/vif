# Multiplayer

This document is the whole of multiplayer except the domain rules, which live in
[Multi-instance domain model](domain-design.md). §1–§9 are the runtime as it
exists: the contract, the correction and ordering boundaries, authority
continuity, the operating point, the diagnostics, and what remains. §10 is where
the protocol came from — the incident that prompted it and the options it was
chosen against — and is history rather than a description of the runtime.
For hosting, joining and router requests during play, see the
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
- A run's **session role** is what it is to its session, read from what the
  session wrote: solo, seats (a session of its own with no door: one it opened for
  its bots, or the one `:leave` or a fork goes on in), host, server, guest, left
  (a guest whose session has nobody left but its own bots), or successor (a guest
  that took the term over, hosting at its own port until `:host` opens a door).
  `:join`, `:host`, `:leave` and `:session` answer by it from one table.
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
| Local input | The producer stamps every crossing with its own lead and applies its own copy at that tick; the authority commits it there, or at its own next tick when it arrives late, and every other instance applies the committed copy (§3.5, §8 item 4). The D-18 prediction answers the keystroke and the weapons fire from the predicted cell; the shared store moves when the crossing applies. A typed gold member leaves at its tick like everything else; the typist validates its next keystroke against the run without the members it has already typed. |
| Shared authority | The host's Shared world is canonical. A guest's predicted result is provisional until the next correction. |
| Player state | Each instance simulates only its Player domain. Owner-authored cursor values have one writer and travel as values; a receiver keeps the values it authors across an install. |
| Predicted derivations | A shared death a predicting instance derives is stamped `PhasePredicted` and held in a ledger. Presentation follows it; the player-domain rewards behind it are paid once, when an authoritative world proves the entity gone. |
| Global environment | Shared wind events are re-derived rather than sent. Every instance consumes two draws from the same Shared environment stream, applies the gust to its own drains and predicted Shared species, and restores active wind state through corrections. Glyph drift is re-derived the same way and draws nothing: each instance steps its own glyphs from the same per-level phases, which corrections restore without moving a glyph. |
| Corrections | A correction starts with a versioned hash index. Equal roots send no state. Mismatches descend to independently proved pages. Compressed whole keyframes remain the bounded fallback. |
| Correction playout | The clock never moves backwards. A correction behind this instance's tick is projected: simulated forward in the staging world, over what this instance applied since, and written at the present. One ahead of the clock within `NetworkHoldTicks` waits for its tick; the newest waiting one wins, and one further ahead is taken as the jump it is. |
| Replay | A guest retains a bounded suffix of its own crossings and of what it applied from others; a projection re-applies each past the installed capture's fence for its source, at its own tick. |
| Crossing ordering | The capture header carries one applied-sequence fence per participant. A receiver removes ordinary frames the installed world already holds — including ones whose nominal receive tick is still ahead — and keeps the ones it does not, including ones whose receive tick is long past. |
| Local FSM lifecycle | A live install replays only config-marked persistent `ClassLocal` exit/entry events for crossed state paths; staging and all ordinary actions remain side-effect free. |
| Join and reconnect | A running game can begin hosting; join and reconnect install a current capture through the same staging path. Every host arms the same mid-run gate once its own lobby is done, so a reconnect takes one path whether the session started with `-host`, `-serve`, a script, or `:host`. A person's join prepares on a preview that holds no place and is seated at their press (§5). |
| Roster | Every admitted peer holds an identity; a roster slot binds it to a cursor and makes it a participant. The coordinator of a dedicated host holds no slot, so it is a peer and not a participant, and a session of one guest has one participant in a roster of two. |
| Cadence | Each direct link gets a bounded correction plan derived from round-trip time, variation, delivered bytes, saturation, and correction demand. The whole-world convergence floor is fixed: a join whose link cannot carry one whole world per floor window is refused (`Corrections.AdmitLink`). Past admission it is reported as `snapshot.cadence_floor_breached` (D-24): by the authority for a link whose plan cannot hold it, and by a guest no whole world reached within the floor plus its grace. |
| Playout lead | Per participant (§3.5): the worst recent age at which the authority committed its epochs, read off the committed copies, plus a relay tick; a participant behind a relay reads it off the same copies. The authority's is one tick. Nobody defers by anyone else's. Changes are local, journaled events. |
| Commit and pacing | The authority commits every guest crossing — as stamped, late at its next tick, or void past `NetworkCommitLateTicks` — and relays only the committed copy. Every guest paces its tick interval so the authority's epochs land inside its band. A participant whose own crossings stay late is evicted by policy (§3.5). |
| Mesh and relay | The session is a tree rooted at the authority. Raw epochs and syncs go up a participant's one link toward it — the authority, else the relay it joined through or adopted — bundled with those of the participants behind it; committed epochs, owner state, correction chunks and the producer's own committed copy go down, to the links that send raw artifacts or were served a join, a leaf's own copy as its age alone; succession records and departure notices flood every link. Each has its own duplicate suppression (epoch and sequence windows, sync sequence, chunk assembly, record content). A relay serves selective repair to the participants behind it from retained authority content, bounded by the transfer ceiling since it has no whole world to send instead, asks the authority for a whole world it cannot author, which reaches them through it, forwards its own committed copies to them, and names each with the tick it last proved in its own answer, which the authority's floor and flood count as a direct answer. The authority takes a participant's raw artifacts on its own link, else its holder's, else — a person behind a relay — on any. |
| Reachability | In a migrate session a guest binds a port of its own and declares it, and the coordinator publishes the whole succession chain on `MsgPeerList`. Every participant holds a link to the first candidate. `-no-advertise`, a failed bind, or `-authority host` leaves a participant a leaf: it plays normally and is never a candidate (§5.1, §5.3). |
| Host loss | `-authority migrate` (default off `-serve`): every survivor walks **the succession list**, each candidate taking a term of its own in its turn, with no vote, because every survivor reads the list from state it already holds identically; a guest whose own link dropped redials its host first. `-authority host` (default on `-serve`): nobody takes it, and every survivor continues alone once its redial ends. |
| Hold | Only the host stops a session: its `:pause` crosses, every participant stops after the same tick, a joiner lands held, and the host's next epoch resumes everyone (§3.6). |
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
item 4). Input feel is the D-18 prediction's job, not the store's
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
| `EventGameHold` | stops every participant's clock after the same tick (§3.6) |
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
live tick can land between the tick the projection reached and the write. The write
compares the staging world's stores against the live ones where they stand and
moves the cells that differ; neither world is captured for it.

A capture ahead of the clock is a join or a jump and is adopted at its own tick;
within `NetworkHoldTicks` it waits ([Netcode design §2.1](netcode-design.md#21-the-playout-buffer)).

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

Each participant stamps its own crossings with the lead its own path asks for, and
nobody defers by anyone else's. The authority stamps every committed copy with the
epoch's age at commit (`WireBatch.Age`): how many of its own ticks after production
it applied the epoch, which is the up path and the clock offset the producer's lead
has to cover, measured where they are paid. A guest reads its lead every tick
(`ownLead`): the worst age at which its epochs came back committed since the last
reading, the newest epoch's standing in between, since a spike on a stream holds
a burst that returns in one tick and the newest alone would hide it, plus
`NetworkRelaySlackTicks` for the authority to relay it, held between
`NetworkBarrierMinDelayTicks` and `NetworkBarrierMaxDelayTicks`; the authority's
own is the minimum, one tick, and a guest no copy has reached uses the default
`NetworkBarrierDelayTicks`. A participant behind a relay reads the same copy, which
reaches it down its path, so no link has to measure a trip for it. The lead in
force is the worst reading of the last `NetworkBarrierRenegotiateTicks` (`leadOf`):
it rises at once and falls only as the higher readings age out of the window, to
the highest still in it, so a momentary trough is not climbed back a tick at a time
and a passed spike does not step down one tick a window; the window is five
seconds, longer than the spikes a slow link repeats, so a spike's lead holds until
the next ([netcode design §3.5](netcode-design.md#35-the-divergence-harness)). The age rather than a
probed round trip: a probe measures both directions and a stall on the way down
raised a lead the way up never needed. Each change is a local, journaled
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
included, so a producer knows which of its epochs were committed; one with nobody
behind it reads the copy's age alone, its frames left off (netcode §1). On a handoff a
guest resends to the successor, in its next epoch, the retained crossings of the
epochs that never came back (`ResendUncommitted`); the successor commits them late
and refuses by sequence any it already holds from the predecessor's copies, so a
lost authority costs a player only what the commit bound voids. Every receiver
takes each source's crossing sequence once (`NetworkSequenceWindow`), whatever
epoch carries it.

### 3.6 Holding a session

A session stops only as a whole, and only its host stops it. The host's `:pause`
(Game → Pause) crosses `EventGameHold`, admissible from the authority alone, so
every participant applies it at one agreed tick, completes that tick and takes no
other (`TimeControl.SetHeld`). A guest's `:pause` is refused, and a second one
before the first lands asks nothing more. A held participant still drains its
links and answers the link probe off the tick, so a hold times nobody out, and the
host sends the held tick's round once, whatever its schedule said
(`Corrections.publishRound`), so every link proves the held world.

The host's next `:pause` ends its own hold at once, uncrossed: a held host sends
no epoch, so the first one its next tick produces, admitted past a guest's own
tick (`NetworkResource.OnAuthorityAhead`), resumes that guest once the drain that
carried it is done. `:new` ends a hold before its reset crosses, which would
otherwise wait for a tick nobody reaches. A hold outlives a lost host: its
successor keeps it for its own `:pause`, as a person's fork does in the session it
goes on in (§5.5), unless a script or bot drives that run and nobody there can give
one. An end no command made goes through the request a command makes, settled at
once: the hold ends in the pass that dispatches it, on the run as on its replay,
which has no link to hear it.

A new game's start is this hold, marked a start (`engine.HoldStart`). A reset whose
`GameResetPayload.Wait` is set holds every participant as it applies, which the
scheduler's unpause of the new game leaves standing, and the host's first press
ends it as its `:pause` would, each guest resuming on the host's next epoch. A
guest's press does nothing, and its status line reads "Waiting for the host to
start". A host nobody plays (`-serve`, a fleet session) never waits, and a
scenario's own reset carries no `Wait`.

A joiner admitted to a held session lands held. Its gate captures the held tick
itself, since a held session never reaches the playout lead past it, and sends
after the world the crossings still scheduled past that tick
(`NetworkSystem.PendingAfter`), which a capture there does not contain; the
offer's `Held` holds the joiner at the world it installed. A relay's gate does the
same with the world it proved at the held tick (`Corrections.ProvedWorld`), kept
while the hold stands for a join that arrives after the proof. A hold that lands
while a gate waits for its lead ends the wait at the held tick (`KeyframeAt`,
`ProvedWorld`), and one that ends while the held world is read starts the gate over,
running (`membership.gateJoiner`). A joiner admitted while
its host's new game waits for the host's press is held as a start
(`SessionOffer.Start`): it waits as every guest there does, and the host's first
epoch starts it with nothing said.

## 4. Correction pipeline

The pipeline, the descent and the playout buffer are [Netcode design §2](netcode-design.md#2-the-correction-pipeline).

## 5. Membership, topology, and authority continuity

Roster changes are shared artifacts produced by the coordinator. A direct
neighbour reports a disconnect; the authority turns it into one departure at one
apply tick, which leaves the participant's cursor waiting as a **Wraith** (§5.4).
A mid-run join is admitted to transport before its capture is read, so
traffic produced during transfer is buffered rather than lost. The join waits for
a capture far enough past admission to include pre-admission epochs and refuses a
catch-up gap past `NetworkJoinCatchUpTicks`, or one still wider than
`NetworkJoinLagTicks` (the default lead) once it has caught up; a bot seat retries
either, since a fresh attempt brings a fresher world.

A join its player started (`-join` at launch, `:join`, the Multiplayer page) holds
nothing in the session until their press. Its handshake previews the session
([Services §8](services-and-networking.md#8-wire-frame-and-session-messages)); the
run fetches the scenario it lacks, builds and checks its world on the preview, says
it is ready, and asks on its status line: "Click or press a key to join *host*",
"Press a key" with the mouse off, "Tap" after a touch. A key that types or moves, or
a click, seats it; Escape, Tab, the function keys and chords do not, and Ctrl-C
leaves (`joinPress`). The seat binds the identity the run's port and world were
built without (`PendingJoin.Hand`), and the run replies and meets the gate as
any joiner does. The host keeps the handshake for a ready period of 5 s, and tells
its player who is ready to join and waiting for their press; a press
after it dials again and seats the world already built while the session plays the
game it was built for, and a session that started another game meanwhile is joined
anew, already pressed (`membership.takeSeat`). A run no player is pressing for seats at
once: a bot seat, a redial after a lost host, a run following its session to a new
game, a page or Android host that took its own start press, and any run with the
wait off (`-nw`, `[play] wait = false`). Nothing stands in for the waiting player on
the map; its cursor appears with its arrival.

The session is a tree rooted at the authority: the shipped CLI builds a star, a
guest seating bots or relaying people (`-relay`) adds a level, and in a migrate
session every participant also holds a peer link to the first candidate (§5.3). Every
link is up, down or beside. A participant's **up** link is the authority if it
holds one, else the relay it joined through (`NetworkResource.Relay`) or adopted;
its **down** links are those that send it raw artifacts or were served a join
within the epoch window, every link at the authority. Raw artifacts go up;
committed epochs and owner syncs go down with the sender's own; a link bundles
them, released when a drain ends, when the tick closes and before any other frame,
and correction chunks travel beside them, each admitted once within
a bounded window; a peer link carries succession records and
departure notices, which still flood. Corrections retain the authority's term,
tick, hashes, and chunks across relays.

A relayed join — a guest's own bot on an in-process stream (`seatLink`), or a
person at the guest's `-relay` address — is served by the guest as a coordinator
would: it asks the authority on its own link to allocate the identity and, once
the joiner installed the world the guest last proved, to cross its arrival with
what the joiner reported (`MsgRelayJoin`); a guest that took the session over asks
nobody, its door admitting as the host's does into the term it holds. A bot declares its holder and dials no
peer links, since it shares its holder's fate; its holder's departure takes its
bot group at the same crossing, to wait with it or to go with it. A person declares no holder: it keeps its slot when
its relay leaves, and, having no link then, redials the relay's door for a window,
walks the chain like a survivor does and plays on through whichever participant
relays for others (§5.1), forking when none does (`Reach.cutOff`,
`NetworkSystem.sendUp`). It is a leaf unless `-listen`
names an address, since it could not reach the host itself. The holder identity is retained in the roster and
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
| Who binds a port? | The coordinator: the instance started with `-host` or `-serve`, or a solo run that opened itself with `:host`; a session a solo run opens for its bots, or goes on in after `:leave` or a fork, binds none until `:host` opens it. In a **migrate** session every other participant binds one too, for peer links and joins, which it takes once it hosts, and while its player relays for others as its `-relay` door would (§5.3): `-listen` pins it, the default is the coordinator's own port, and a machine already using that port falls back to an OS-assigned one; a successor's `:host` can open a second beside it. `-no-advertise`, a bind that fails, a bot, a browser build, a person who joined through a relay without `-listen`, and every `-authority host` session leave a participant with no port of its own. A joined participant may also bind a `-relay` address, which admits people who cannot reach the host, and as the host's door once its run hosts. |
| Who authors? | The participant holding the current term. It is the coordinator until a handoff moves it, and a handoff moves *authorship only*. |
| How is a participant reached? | The coordinator is reached by `-join <address>`, or by the `vif://address/name` link a deployment issues when one address serves several sessions, or through a guest's `-relay` door when the coordinator's cannot be, which the chain names and `vif-router` reaches through a reverse tunnel (§5.6); there is still no discovery. A participant that took the session over is joined the same way, at the address its `:session` names. Everyone else is reached through the **succession chain**: every participant that declared a port, in join order, with the address it declared and the door it admits people at, if one. The coordinator publishes it whole on `MsgPeerList` and carries it in the offer and the handoff record, beside the roster. |

A successor is usually on another machine and cannot bind its predecessor's
address, so a successor that authors must already be reachable at an address of its
own (§5.3). Nothing is rebound: the predecessor's address is not reused, so there is
no `TIME_WAIT` race to back off from and no window in which an address is
half-released. What moves is which address the survivors dial, not which address
answers, and new players dial the same one: the successor's port takes joins as the
first host's did.

`-authority host` is the default for `-serve`. There the address *is* the session:
an orchestrator replacing the pod at the same address is the reconnect its guests
actually want, and a guest's own port would be for nothing. It is also the setting
for a deployment where the world may only ever live on the machine that started it.

### 5.1 Succession

If the authority disappears, the survivors walk the **succession list**: the
chain's participants in join order (§5.3), less the one that went, one away and a
bot, whose seat has a holder. It is a function of the chain, the roster and who
went, all of which every survivor holds, so every survivor reads the same list
without exchanging anything; a local dial result is never an input. A session whose
chain names nobody still present has no list, and every survivor forks.

Candidate *k* takes the term *T + k*, *T* being the lost authority's, and only when
its turn comes: *k − 1* windows (`parameter.NetworkSuccessionWindow`, 5 s on the wall
clock, since the session stands frozen) after the walk began. A receiver refuses a
record whose authority is not the candidate its own list puts at that term, so no
two hosts ever share one. That is the split-brain rule, and it needs no quorum, which
could not serve the smallest session anyway: one survivor of a roster of two is not a
majority of two. A survivor follows the first record that reaches it for the host it
followed, and only that host from then on, so survivors that reach different
candidates play on as different games, each keeping the others' places (§5.2).

The procedure on each survivor is:

1. **Stop, and flood a loss notice.** Nobody plays on in a world the next host is
   about to replace, so a survivor freezes its clock when it sees its authority go,
   by its own link, a notice or a record reaching it first (`TimeControl.Freeze`).
   The freeze is the hold the session itself asks for, settled at once as the end
   of one is (§3.6), so a replay stands still in the pass the run did. Only a direct
   neighbour of the authority sees the link drop, and the departure crossing that
   would have carried that news is produced by the participant that is gone, so a
   survivor two links away opens the same walk from the notice, which names the lost
   term.
2. **Redial the host.** A person's run with a link of its own to its host redials
   it under the claim its place waits under (§5.4), the first host at the door the
   run came in by and a host that took the session over where the chain says: a link
   that dropped on its side alone leaves a place the host's Wraith gives back, and
   the run admitted back replaces this one, as `:join` does. It waits up to a
   window, however many other survivors lost the host too, so a host that went
   silent costs that window; a host that is gone ends it at once — nothing
   listening, a refusal no retry helps, or no place for this run. A survivor that
   reached its host through another participant walks at once.
3. **Walk the list.** A survivor dials each candidate whose turn has begun, once a
   second, and announces itself on each link it opens and again on every link each
   pass, since the notice sent as the loss was seen is dropped by a peer still
   linked to the lost host, or by a full queue; a host that took the term before
   that link existed, or before that notice was read, answers with its record. A
   candidate whose turn comes takes the term if the session lets it move, with one
   self-check, retention: a host with no retained authoritative record has no
   baseline for a delta to name and would answer the first manifest with a whole
   world for every survivor at once. A join capture counts, so a guest admitted
   seconds before the loss is eligible. A record carries the roster with its slots
   and holders, the anchor, the chain and the evidence tick, so adopting it is one
   decision. The host starts its correction pump, a live script's or bot's included;
   only a harness publishes by hand.
4. **The host resumes its group.** Every survivor that adopts a record reports the
   term it joined, and the host resumes once every survivor's report is in, or a
   window after it took the term for whoever is missing; the lost authority's bots
   went with it, and nothing reports for a Wraith. Each survivor resumes on the
   host's first epoch past its own tick, as from a hold (§3.6).
5. **Past the last candidate's turn, give up.** No record arrived, so this instance
   continues as an explicit local fork. Under `-authority host` nothing walks: a
   survivor forks once its redial ends, at once without one, and only a redial
   freezes it.

Retention *ordering* is deliberately not an eligibility test: the list decides who
is next, and a host a cadence behind a peer costs that peer one correction.

**Relaying is opt-in.** A run carries another participant's raw artifacts toward the
authority only for one that joined through it — its bots, a person at its `-relay`
address — unless its player turned on **Relay for others** (`:relay on`, the
Multiplayer page, `[play] relay`), which a browser cannot. Anything else is dropped
and counted (`network.relay_refused`), so nobody's traffic lands on a run that did
not offer to carry it; a link already carried stays carried when relaying is turned
off. A candidate that is not hosting answers a survivor's notice with the record it
follows only when it carries that survivor, which then follows the host through it
and dials the host at its chain address for a link of its own (`Reach.dialHost`):
its artifacts go there once it opens, the relay lets it go as its epoch window
closes, and it redials that host on a loss as one that had a link does. A host
unreachable from it is tried `NetworkHostDialAttempts` times, each after a longer
wait, then followed through the relay for good, as is the coordinator a relayed
person joined behind, which has no chain address.
A participant with no path to its authority, a person whose relay's link went,
first redials the door it joined that relay at for one window, as step 1 redials a
host, and a relay still running admits it back into its place; refused, nothing
answering, or past the window, it walks every candidate for one that carries it and
forks after a walk down the whole list (`Reach.cutOff`, `Authority.redialRelay`).
A guest whose own link to a host still running dropped, and whose redial cannot
reach that host, rejoins under its claim through the candidates it walks to
(`membership.rejoinThrough`): the port of one relaying for others takes the join as
its `-relay` door would and asks the host, which gives the place its Wraith keeps
back, and the guest plays on behind that candidate; a port not relaying refuses, as
it refuses every join while another participant hosts.

A person's run that forks goes on in a session of its own, as `:leave` takes it
(§5.5): the same identity, the next term, every other player's place kept as a
Wraith, and `:host` or `:join` at once. Under `-authority host` every guest
does so the moment its host goes, so whichever hosts next takes the others back
with their progress. A run with no loop to serve that, a script's or a bot's, stays
the fork, with `network.fork` and a persistent `Host lost` badge; it refuses a
higher term met later, because partition merging is not implemented.

### 5.2 The roster after a loss

A handoff preserves the surviving players exactly; the lost predecessor may differ
when its departure straddles the handoff, and any bot may: a seat crosses at its
holder's word, and a receiver a playout behind its relay may hold that crossing
unapplied when the record reaches it. The host produces the
predecessor's departure as the ordinary crossing, because it may, and the place
waits as a Wraith (§5.4). A convergence floor (`NetworkSuccessionTicks`) after its
group resumes, every independent participant that reported no membership at its
term departs the same way, its bots with it: a survivor that followed another host,
or forked. The floor comes after play resumes rather than at once, so a survivor
still arriving through a relay hop is not sent away. One whose report reaches the
host after the floor is given its place back, with its bots', as a claim's return
gives one (`Authority.takeBack`, `Instance.CrossReturns`): the floor is the deadline
for the report, not for the survivor.

A fork with no loop to take it into a session of its own (§5.1), left with **no link
at all**, drops every cursor it does not simulate. Having no link is what makes the
drop exact rather than convenient: a departure is produced once at one agreed tick
because two instances must destroy the same shared entity together, and here there is
no second instance. A person's fork drops nobody: every place waits as a Wraith in
the session it goes on in (§5.5). A fork still linked keeps its roster while a link
stands: a run with no loop sees its seats end on its fork (§5.5), and the last of
them going leaves it with none, when it drops as one left with none at the fork
does (`membership.reportPeerLost`).

A departure, however it is produced, also clears what the instance had applied from
that participant. An identity returns to the pool once its participant is dismissed
(§5.4), and a crossing sequence starts at one, so a fence kept from the previous
holder would claim a capture already contains crossings the next holder of that
identity has not produced — and §3.2's install rule would then discard exactly those.

### 5.3 Reachability under `-authority migrate`

Under `-authority host` none of this applies: the address is the session, one
instance binds it, and authorship never leaves the machine that started the world.

1. A guest binds a listening port before it answers the offer, so it declares the
   port it *actually* bound. `-listen <addr>` pins one; the default is the
   coordinator's own port, so a session is one firewall rule and the port a guest
   opens is one somebody already chose to open. A machine already using it — two
   guests on one machine — takes an OS-assigned port instead. `-no-advertise`, a
   failed bind, or `-authority host` leaves a participant a **leaf**: it plays
   normally, declares nothing and is never a candidate.
2. The coordinator puts every declared address, and every door a relay tells it of
   (§5.6), in the chain and publishes the whole chain on `MsgPeerList` between two ticks, never under the lock it took the address
   under, and again as the session opens so a participant admitted early is not left
   holding a prefix. It completes a declared `:7777` from the join
   connection's remote address, because a participant knows which port it bound and
   not which address the world reaches it at.
3. Every participant dials the first candidate and keeps that link warm, so the
   instance that will have to author already has one when it does. A **successor
   chain** rather than a full mesh: N links instead of N², and the chain is the same
   either way, so the shape is one dial policy and can be widened later without
   changing an artifact. A candidate the connect does not reach is dialled as a
   followed host is (`Reach.dialSuccessor`): each attempt after a longer wait, then
   left (`NetworkHostDialAttempts`), since the walk dials it again should the
   authority go; another first candidate, or a link that stood and dropped, starts
   the attempts over.
4. Two participants that dial each other exchange `MsgConnect`/`MsgAck` — who is
   calling, running which build — and nothing else: they already hold identities the
   coordinator assigned, so a peer link allocates nothing, offers nothing and
   captures nothing. Admission is roster membership, read from the world rather than
   from the coordinator's own list, and deliberately *not* the term: a survivor
   still electing is exactly who needs to open one.
5. The same port takes joins, which open with the session's name
   (`MsgSessionRoute`, empty for none) where a peer link opens with `MsgConnect`, so
   its first byte says which handshake runs. A join is turned away until this
   participant holds the term, as `RefusalUnknown`, or `RefusalHandoff` while a term
   moves; from then on it is admitted as at the first host's door, under the
   `-players` the session was opened with, which every offer carries
   (`SessionOffer.Players`).

#### Why the chain is beside the roster and not in it

**An address is not roster identity.** `RosterEntry` is compared by value:
`SameRoster` sorts two rosters and calls `slices.Equal`, and
`HandoffRecord.Validate` refuses a record whose surviving players — everyone but the
predecessor and the bots — are not identical to the receiver's. An `Addr` field on
that struct would make a guest that rebound its port look like a different roster
and fail every handoff.

**Append-only is what makes it a legal succession input.** The list
(`SuccessionCandidates`) must be a function of state every survivor holds
identically, and a broadcast is identical only eventually. Appending never reorders,
so any two participants hold a chain and one of its prefixes — and a prefix and its
extension list the same candidates in the same order as far as the prefix goes. That
is the whole of the agreement, and it needs no barrier crossing.

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
| Rejoin after a handoff | Every participant holds the whole chain, so every survivor walks it from the top, each candidate a window's turn, a guest whose own link dropped redialling its host first. |
| Redial wait | A guest redials its own host for a whole window before it walks, however many others lost the host too: a host that went silent costs that window, and one that quit or crashed refuses at once. |
| Relaying | Opt-in and off by default (`:relay`, the Multiplayer page). A run carries only those that joined through it unless its player offers more, so nobody's traffic lands on a run that did not offer to carry it. |
| Joins after a handoff | The successor's own port takes them, the one its survivors already dial, rather than a second door; a join and a peer link each open with a frame of their own. `:host` opens one at the address it names, or names that port, and the successor hosts as the first host did, under the `migrate` policy its players hold. The `-players` ceiling travels in the offer, so it holds whoever hosts. |
| Relay reach | Two halves that compose (§5.6): the chain carries each participant's door beside its port, so a door is learned in-band as an address is, and `vif-router` routes a door published through a reverse tunnel by name as it routes a pod, so a door behind a NAT is reached at the site. Relay trees the authority chooses stay dropped; a relay is what a person chooses. |

### 5.4 Wraiths

A departure leaves the participant's cursor a **Wraith** rather than removing it.
It keeps its entity, slot, owner and holder, and `CursorSystem.wraith` strips
everything the world reaches a cursor through, keeping `Cursor`, `Protection` and a
`Wraith` component that holds the cell it stood on and the game time it turned.
Nothing hits, aims at, collides with or drains it, it touches nothing, and the peer
renderer draws its slot label alone at that cell. Its roster entry reads `Away`,
which succession (`SuccessionCandidates`) and the corrections owed
(`owedLocked`) pass over: nothing reaches a participant that is not there. A
holder's bots wait with it, and its identity and slot stay taken.

A **dismissal** (`ParticipantDepartedPayload.Dismissed`) removes the cursor and its
bots for good and returns the identity and slot to the pool: the host's `:player
drop`, a holder dropping its seat, an abandoned relayed join, an instance with no
link (§5.2), and the authority's own two, which it crosses once
(`NetworkSystem.expireWraiths`):

- **Timeout.** A Wraith is dismissed `WraithTimeout` (2 min) of game time after it
  turned; a bot whose holder waits too goes in its holder's dismissal. A reset
  rebuilds a Wraith as a Wraith, its wait going on from the last game's, so no run
  of resets keeps one.
- **Yield.** A dial a full session cannot otherwise admit has the participant away
  longest dismissed for it (`membership.yieldLocked`) and is refused `RefusalYielding`,
  which seats and first joins retry in a moment; the retry takes the freed place.
  A successor orders who is away by their Wraiths' waits.

A notice dismisses only a seat, which its holder dropped; a player whose link
closed is only ever away. A Wraith counts as a guest, so a dedicated host stays
occupied and its clock runs while one waits, which is what lets its wait run out.
A defeat is the players left: a Wraith neither loses nor holds one up.

**Coming back.** Each place is joined under a **claim**: a random token the
participant's process keeps (`claimKeeper`; a page in its tab's `sessionStorage`,
so a reload keeps it and another tab is another player; a terminal for the
process's life) and whose digest the roster entry and the cursor carry, so a
successor's roster carries it too. The host's own entry carries one. When the
coordinator asks (`MsgJoinClaims`), before it assigns a place, a dial names the
tokens it keeps of its kind, a seat its bots' and a player its own. A token whose
digest an away entry carries takes that place back, a full session's too: the same
identity, slot and cursor, held for that dial until its arrival crosses, so no
newcomer is given it and no wait runs out on it (`NetworkResource.WraithKept`). A
join that does not complete gives it back to the Wraith. A holder's bots come back
the same way once it seats them again.

A player brings back what its cursor held when it left (`event.CursorRecord`: energy,
heat, health and loadout), the last sync its run sent (`NetworkResource.OnOwnState`).
The record stops once the run is out of the session: it closed, or plays on after a
fork (`converge.Instance.Forked`), its own bots still linked or not, while a
successor carrying the session keeps its own; a run redialling the host it lost
stops it at the drop and keeps it again if it stays; a dismissal forgets the claim. The
arrival crossing carries it, and `CursorSystem.reclaim` writes it over the template
at the free cell nearest the one the Wraith held, on every instance. A Wraith is
driven by nobody, its own participant included, until that arrival.

A claim whose place is still linked, which the host has not yet seen go, closes that
link (`membership.closeStaleLink`): its departure crosses as any lost link's does, and the
dial is refused `RefusalReturning`, which it retries in a moment into the place. A
place held through a relay waits for the relay to see its link go, and one another
dial is taking back is refused the same way. The roster carries digests alone and a
dial names its tokens only on its way to the coordinator, through the `-relay` it
joins by if any, which carries all its traffic anyway, so no other peer can take its
place; the port is unauthenticated by decision, so a claim is no credential against
the wire.

### 5.5 Leaving a session

`:leave` takes a run out of its session into one of its own, the world as it
stands: the session a solo run opens for its bots (§5.0), so `:host` opens it to
others, `:join` replaces it and `:bot add` seats more. A host and a guest leave
alike. A solo run and a run whose session holds only its own bots have nothing to
leave, and a server stays in the session it serves, so each refuses it by its
session role. Those left behind see an ordinary loss: the host keeps a guest that
left and its bots as Wraiths, as it keeps one whose link closed (§5.4), the people
a guest relayed keep their slots as when any relay leaves (§5), and the guests of a
host that left run §5.1, which under `-authority host` is the fork.

A person's fork leaves the same way. `converge.Instance.Forked` hands the run the
roster it held, and its loop serves the fork as it serves `:leave` (`membership.forked`,
`membership.leaveSession`), except that it keeps every place rather than its bots' alone:
each other player's cursor waits as a Wraith in the session it opens, for its player
to take back once `:host` opens the door (§5.4). A hold the lost host set stands
there for this run's own `:pause`, and a freeze ends; the status bar says which. A
seat under such a run ends only when the run halts it (`membership.seatEnds`), never on its
own fork or the run's, so the leave finds every bot to seat again. A run with no
loop to serve the fork stays the fork (§5.1).

The run keeps its identity and authors the term after the one it held. Its cursor,
its crossings still scheduled and its journal all name that identity, and nobody it
leaves behind holds it, so the departures it produces for them cannot remove its
own cursor, as they would if it took the coordinator's usual first identity. The
new term puts anything still carrying the old one behind the term it holds, where
every authoritative artifact is ignored. The claim it was in the left session under
is given up with its record as it stood, so it can still take its Wraith back
there (§5.4), and the session it opens issues its own.

Its bots leave first, as they do when it quits, though as lost links rather than
dropped seats, so the session left keeps their places too. The session it opens
keeps them as well, each under a claim of its own (`claimKeeper.carry`: a new token
over a copy of the record the bot left with, which stays the left session's for a
return there), and once the old door is shut each bot is seated again and takes
its place back; a bot's journal closes with its seat. In between, the run's loop
serves the leave, so no other command interleaves:

1. **Open the new session**, under the correction pipeline's apply lock, so nothing
   of the left session is applied across the swap and what it still had queued is
   dropped. Opening is the step that can fail — a journal records the write as it
   records a join's, which captures the world first — so it runs before anything is
   dropped, and a failure leaves the run in the session it could not leave, with its
   bots seated back there.
2. **Drop what the left session sent.** `NetworkSystem.LeaveSession` drops every
   artifact another participant sent, scheduled or applied, with its fences, owner
   syncs and digests, and every roster change still waiting, the run's own included,
   since the session it opens writes its roster afresh; its own ordinary crossings
   carry on. The relay it joined through is forgotten and its peer-link listener
   closes, so nothing the left session was told still answers.
3. **Turn the places kept into Wraiths and remove the rest**, locally as a fork
   with no loop removes them (§5.2), and settle the departures before any identity
   is handed out again: one of theirs given to a bot would be the participant a
   departure still pending removes. A place already a Wraith waits on as one.
4. **Close the old port** outside the world lock, because closing it waits for
   handshakes that take that lock. A dial reaching the old door meanwhile is refused
   as the session ending (`RefusalEnding`), so nobody is admitted into a term that
   ends with it.

Nothing names the left session afterwards: the join and host addresses, the session
name and the `-players` a joined session carried are cleared, so a restart neither
dials back into it nor listens on its address, and `:session` reads `Session
hosting` with no address. A run already being replaced is not left: what replaces
it decides where it goes.

### 5.6 Relay doors and the router

A `-relay` door admits people who cannot reach the host, and until here its address
was handed out of band like the host's: nothing published which participants
relayed, and a NAT between a person and every relay was not crossed. Two decisions
close that, and they compose.

**The chain names doors.** A chain entry carries, beside the port a participant
declared, its **door**: the address it admits people at, as the world reaches it
(`ChainEntry.Door`). A run tells the authority its door once it has one, on its own
link (`MsgRelayJoin`, step `RelayDoor`); the authority completes a bare `:port` from
the run's join connection as it completes a declared port, and the chain goes out
as it always has, on `MsgPeerList`, in the offer and in the handoff record, so every
participant holds every door, `:session` names them, and a run with no path to its
authority rejoins at one (`membership.rejoinThrough` dials a candidate's door before
its port). A door is an attribute of an entry and not a candidacy: a participant with
a door and no port is still a leaf. A relayed person tells nobody, since it has no
link to the authority to tell; a relay has one.

**The router routes doors.** `vif-router` places a dial by the name in its first
frame on the pod that answers to it; a **tunnel** is a door it places the same way. A
run publishes its door through the router (`-relay vif://<site>:7777`, the router's
address and no name) by holding a control connection open to the front door: the
router names the door, from the pods' identifier space, and answers with the link
(`vif://<site>:7777/<id>`) and a token the run resumes the same name with after a
blip; the run declares that link as its door. For each dial the front door places on
it, after the route frame, the budget and the ceiling it applies to a pod, the
router asks the run on the control connection to open a connection back, matches
the two by a ticket and splices them, re-encoding to the door the name the run
answers to; a run that does not open within the route timeout refuses the dial as a
pod out of reach does. So a door behind a NAT is reached at the site's address, with
the budget, the ceiling and the refusals the pods have, the router never dialling an
address a caller named, and the run never binding a port the world can reach. A
browser relays nothing and publishes nothing, as before.

**What was not chosen.** Hole punching needs a rendezvous and UDP or WebRTC, which
the networks that need a relay most block first (services §7.3); a rendezvous in
every host would be a second front door where one already places by name; relay
trees the authority chooses stay dropped (netcode design §4). **Later**: a host's
door published the same way is a player's own session reached through the site, the
router neither allocating nor authoring, which needs its listing and lifetime in
`/vif/api/sessions` and the create budget (todo, Publish a player's session at the
router). A relayed topology is verified under `LinkShape`s by
`TestARelayedTopologyConvergesUnderShape`, asserting hash-only convergence and the
authority's per-link fan-out.

## 6. Current operating point

The operating point and the readings behind it are [Netcode design §3](netcode-design.md#3-readings).

## 7. Diagnostics and operations

The useful runtime signals are:

- `snapshot.correction_entries`, `snapshot.correction_entities`, and
  `snapshot.correction_cells`: how far prediction moved when authority arrived;
- `snapshot.corrections_held` and `snapshot.corrections_jumped`: how often the
  playout buffer deferred one because this instance had not reached the tick it
  describes, and how often one was too far ahead to wait for (netcode design §2.1);
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
- `network.lag_ticks`, `network.stale`, and `network.barrier_late`: how far behind
  the newest peer tick this instance stands (a paced guest's margin), whether its
  pacing reads it out of step, and whether it applies crossings late, and
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
  `manifest_bytes_sent` include the in-process links of its own seats;
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

An own pause, speed, step, breakpoints and raw shared mutation (`:system`, `:emit`,
`:region`) are refused in a live session. They are instance-local operations and
cannot stop or mutate only one copy of it; a host whose only links are its own bot
seats may still pause (`SessionState.SeatsOnly`), and the host's hold stops every
participant (§3.6). The programmatic surface refuses by the event's declared class
(`App.shareOperator`): `App.SetupLevel` and `App.Region` carry `ClassShared`
payloads, which `event.OnWire` never transports, so no participant may originate one
in a live session — the authority included. `App.Reset` is `ClassBus`, so it crosses
on the authority and is refused on a guest, as `:new` is.

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
it. Planned multiplayer work is in [the todo list](todo.md#multiplayer), and what
could make the netcode cheaper in [Netcode design](netcode-design.md).

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

2. **Remote cursors step rather than glide.** Remote cursor motion is drawn at
   simulation arrival ticks, and nothing interpolates between them. A terminal
   smaller than the session's map is a viewport the camera scrolls after the local
   cursor, not a different world or a misplaced cursor: `applyMapLatch` installs
   the session bounds before the FSM boot script spawns cursor slot zero, and
   `LockMap` latches the world shared for the whole run.

   *To see the remainder:* `-serve :7777 -size 200x60`, then join from an 80x24
   terminal. The view is an 80x24 viewport the camera scrolls over the 200x60
   world, and every cursor is where the authority says it is. What is missing is
   optional presentation interpolation, which is presentation work independent of
   simulation ordering. If a *wrong* cursor position is ever observed across
   terminal sizes, that is a regression in the map latch and not this item.

3. **Cross-platform float drift is repaired live but not in replay.** A correction
   carries every shared component whole, float fields included, and a delta is
   proved lossless by re-hashing the reconstruction against the capture's own
   integrity hash — so a `float64` that drifted on another platform is repaired
   exactly like any integer that drifted. Two things remain:

   - A manifest hashes float bits, so a one-ULP difference on a mixed-platform
     session is a page repair each cadence: diagnostic noise, not a simulation
     fault.
   - A journal replayed on a different platform has no authority to repair
     against, so a float difference there accumulates. Replay determinism is
     guaranteed within one implementation build.

4. **A crossing that misses its lead lands later on the authority than on its
   producer.** The producer applies it as stamped and the authority commits it at
   its own next tick (§3.5). The knockback window covers every attacker, so the
   knockback an instance applies first holds it and the next correction repairs a
   difference. A kill is credited to the cursor whose hit killed it, so two cursors
   finishing one target inside that lateness can each be credited on their own
   instance.

5. **A browser guest once kept the previous region's glyphs.** Entering the tower
   region, it kept the region before's glyphs, or kept the glyph system running
   although the tower disables it. It was seen once, on 2026-09-24, and has not
   recurred. *To see the remainder:* a second sighting should record whether that
   guest installed a capture across the region change.

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

Three membership checks are worth running by hand on every host shape, because they
exercise the paths a two-terminal session reaches and nothing else does:

- The guest leaves with `:q` and dials again. It should install a current capture.
  Its old cursor waits as a Wraith, its slot label alone where it stood, until it
  is dismissed two minutes later, or at once when a full session needs its place;
  a browser guest that reloads instead comes back to that place, the cursor its
  Wraith kept, with the energy and health it left with. While it waits at the start
  gate it is not yet in a session and has no world, but the wait is still leavable:
  Ctrl-Q, Ctrl-C and a terminal resize are answered there.
- The host leaves instead. The guest takes the term — `:session` names it as the
  authority, `network.migrations` reads 1, and the `Host lost` badge clears — and
  the host's cursor must wait where it was left as a Wraith rather than play on. A
  guest that could reach no candidate says its host connection was lost and
  plays on in a session of its own, which is the fork: `:session` reads `Session
  hosting` under the same participant, and the host and every other player wait on
  its map as Wraiths.
- Either leaves with `:leave` instead and plays on. Its world goes on from where it
  stood, `:session` reads `Session hosting` with no address under the same
  participant, and its bots come back to the places they had. The others see the
  loss above: a guest that left waits on its host's map as a Wraith, and a host that
  left is lost to its guests. `:host` then opens the run's own session to others.

Reachability is worth a fourth, on a session of three or more, because it is the one
path a two-terminal session never exercises:

```sh
./bin/vif -serve 127.0.0.1:7777 -authority migrate -d -size 120x40 -l -lv info
./bin/vif -join 127.0.0.1:7777              # and again, from a second terminal
```

Each guest logs `peer listener bound` with the port it got — an ephemeral one,
since on one machine the host already holds the default port — and the host logs
`succession chain published` as each is added. On each guest `:session` names how
many candidates the chain holds and whether this one is listening. Kill the host:
each guest's redial is refused, the survivors walk to the list's first candidate and
continue in one session, rather than each continuing alone. The one hosting now
reads `Session hosting` and its port in `:session`, and a third terminal's `-join`
at that port is admitted into the session; `:host :7778` there opens another door,
which `:session` then names. Repeat with `-no-advertise` on the first guest and the
second hosts instead.

## 10. Where this came from

This section records why the protocol has its shape: the incident that prompted
it, the options it was chosen against, and the patterns it draws on. It describes
no runtime; §1–§9 do.

### 10.1 The incident

One instance's log of a session on 2026-08-30, over 2,186 ticks:

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
