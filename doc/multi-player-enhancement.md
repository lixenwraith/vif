# Multiplayer strategy after Phase 7

This is the current multiplayer plan. It replaces the former phase diary, incident
log and answered-question backlog. The implemented domain rules remain in
[Multi-instance domain model](domain-design.md), and the protocol as built is in
its §6; this file is what the phases were for and what is left.

## 1. Vocabulary and boundaries

Use these terms consistently:

- A **network peer** is an endpoint exchanging session frames. A peer holds one of
  three session roles: host, relay or peer.
- The **game host** owns the canonical Shared-domain world, session identity,
  roster and correction stream. It is the participant *currently* authoring, which
  is participant 1 until a handoff moves it.
- A **relay** is a participant with more than one link that forwards the
  authority's artifacts to the participants behind it and retains what it
  forwards, so it can answer their selective requests. It authors nothing.
- The **authority term** is the authority's generation: a monotonically increasing
  `uint64`, one per session, incremented exactly once per successful handoff.
  Every authoritative artifact carries the term it was produced under. The word is
  Raft's because the invariant is Raft's — at most one authority per term, and a
  term never goes backwards on any instance — and `epoch` was unavailable, because
  in this codebase an epoch is a closed barrier production epoch.
- A **game guest** simulates the Shared domain as a predictor and adopts host
  corrections. A guest is not a thin renderer.
- The **Shared domain** is the world all participants can observe: shared species,
  cursors, gold, walls, towers, gateways, map state, shared FSM state and time.
- The **Player domain** belongs to one local participant: corpus glyphs, weapons,
  projectiles, drains, nuggets and visual effects. It is never reconstructed on
  another instance.

Network role, simulation domain, roster ownership and authority term are
orthogonal. Do not encode "host" or "guest" into entity domains, do not assume a
network topology from a player slot, and do not read authorship off a participant
ID — read it off the term. The relay role is the first thing this orthogonality
actually bought: it is a role a participant takes on because of how many links it
has, and nothing about the flood or about who authors changes with it.

## 2. Current contract

Phases 1–7 are implemented.

| Area | Current behavior |
|---|---|
| Local input | The producing peer applies ordinary crossings immediately. Remote peers retain the receive-side playout lead. |
| Gold typing | A correctly typed shared gold member is destroyed on the producing peer before the next frame or tick; the destruction still crosses and is corrected authoritatively. |
| Shared authority | The host's Shared world is canonical. Guests run the same deterministic simulation between corrections and may differ provisionally. |
| Player state | Each instance simulates only its local Player domain. D-13 cursor values have exactly one owner and travel as values; a capture carries them so a joiner can materialise a cursor, and a receiver keeps its own over any it authors. No component of a shared entity names a player-domain entity. |
| Corrections | A correction leads with a versioned hash index over the shared capture. Equal roots end it with no state at all; a mismatch descends to pages and repairs only those. Whole keyframes and exact deltas remain the bounded fallback, in the same compressed envelope and the same chunk transport. |
| Authority term | Every authoritative artifact — offer, capture header, manifest, request, shard set — carries the term it was produced under. A receiver ignores an older term, acts on its own, and refuses one it was never handed. |
| Retention | Every instance keeps an index over each authoritative capture it can prove it holds, bounded by `SnapshotManifestRetention`. It is a successor's eligibility evidence and a relay's ability to answer, and it is one mechanism. |
| Relayed sessions | A participant with more than one link forwards the index onward and answers from its retention. A session whose relays hold retention keeps the selective stream; one with a relay that cannot answer keeps the whole-body flood, and says so. |
| Local replay | Each instance retains a bounded suffix of its own accepted crossings and replays the ones whose authoritative apply tick is after a correction's baseline, so both newer actions and earlier-but-still-in-flight actions survive it. |
| Join/reconnect | A running solo game may use `:host <addr>`; join and reconnect install a current capture through the same staging path. |
| Cadence | Each directly linked peer gets a bounded plan derived from measured link state, priced from the bytes the selective protocol actually sends. The convergence floor remains non-negotiable. |
| Host loss | The survivors run a succession over the closed roster: report, vote, handoff. A successor that reaches a strict majority and holds current retention adopts authorship under the next term and moves the membership with it. A partition that cannot reach a majority elects nothing and continues locally with persistent `HOST LOST:LOCAL`, exactly as before. |
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

## 3. Post-Phase 7 operating point

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

Phase 7 adds three costs, measured by
`TestAuthorityContinuityCostAtTheStormHighWater` on the same world:

| Measurement | Storm high water |
|---|---:|
| One succession report | 110 B |
| One vote | 34 B |
| One handoff record at a roster of 16 | 818 B |
| A whole handoff — succession plus first correction, per survivor | 2.5 KiB |
| A whole handoff — the same, across 15 survivors | 36.1 KiB |
| The same adoption answered with a keyframe to each survivor | 231 KiB |
| A relayed repair of 50 pages | 14.3 KiB, byte-identical to the direct one |
| The authority's link for that repair, direct | 16.6 KiB |
| The authority's link for that repair, relayed | 1.4 KiB |
| A relay's retention: 4 indexes over 58 sections | 2,121 indexed rows |

The first three rows are why succession costs what it does: a report and a vote
are fixed, and only the record grows with the roster, because only the record
carries membership. The pair of adoption rows is requirement 5 as a number — a
successor seeds its baseline from the capture every survivor already installed, so
adoption is an ordinary indexed correction rather than a keyframe storm, and the
difference is 6.4×. The relayed rows are requirement 6: the pages are identical
because a relay serves the authority's own content, and what moves is which link
carries them.

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

### 4.3 Losing the authority: the migration contract

Authorship is a **term**, not a property of participant one. A session opens at
term 1 under the instance that opened it, and every successful handoff adds one.
Every authoritative artifact carries the term it was produced under, and a receiver
does exactly three things with that field:

- **older than the one it holds:** ignore it. That is the ordinary in-flight case
  across a handoff, and it is counted (`network.term_stale`) rather than warned
  about.
- **equal:** act on it.
- **newer:** *refuse* it, count it (`network.term_refused`) and say so. A term is
  never adopted from an artifact — only granted by a handoff record — so an
  unheralded higher term is a split brain to report, not a fast successor to
  follow.

**The succession is report, vote, handoff, and it has no timers in it.**

1. A survivor that loses the participant currently authoring floods an
   `AuthorityReport`: which roster members it is directly linked to, the newest
   authoritative tick it retains an index over, and how many such records it holds.
   The report is information, so it is idempotent and revisable, and the flood is
   also how a participant that never saw the disconnect learns of it — the
   departure crossing that would have carried that news is produced by the
   authority, which is the thing that is gone.
2. Once a survivor's view covers a strict majority of the closed roster **and**
   every participant it is directly linked to has reported, it casts one vote,
   immutably, for the lowest-numbered candidate that (a) is directly linked to a
   strict majority of the roster and (b) holds retention as new as the newest any
   survivor reports. (a) stops a minority partition from electing; (b) stops a
   participant that has been silently behind from becoming the thing everyone else
   adopts. Both are computed from the same reports on every survivor, so the answer
   is a function of the roster and the survivor set rather than of who noticed
   first.
3. A candidate holding a strict majority of votes publishes a `HandoffRecord` and
   begins authoring under term N+1. The record carries the votes it was elected on
   and the membership it is taking over — roster, slot assignments, `JoinAnchor`
   and barrier delay — so adopting it is one decision rather than a term change
   followed by a roster negotiation. Its `EvidenceTick` is the newest tick the
   successor's retained ring holds, which is requirement 3's claim made checkable:
   the evidence is the ring, not a fresh capture, because a fresh capture proves
   only what the successor believes.

**Split brain is impossible rather than unlikely,** and the reason is the vote:
each participant grants one per term and never revises it, so two candidates
cannot both reach a strict majority of one closed roster. A receiver checks the
same thing from the other side — a second, different record for a term it has
already adopted is refused, and so is one that skips a term or carries fewer votes
than a majority.

**A partition that cannot elect keeps the old behaviour, unchanged.** The
succession opens, its deadline (`NetworkSuccessionTicks`, one convergence floor)
passes with nothing eligible, and each survivor continues its own game from the
last authoritative state with `network.host_lost` set and the persistent
`HOST LOST:LOCAL` badge. `network.fork` records that this instance is a local
continuation rather than part of a session, and it is what makes a later encounter
with a higher term a refusal that says so rather than a merge to attempt.
Partition merging remains a non-goal; refusing to merge is the deliverable.

**Adoption is not a keyframe storm.** A successor seeds its publication baseline
from the capture it last installed — which is the capture every other survivor
installed too — so the first correction under the new term is an ordinary indexed
one. A survivor whose world agrees answers it with a hash and receives no state;
§3 measures the difference against a keyframe to each survivor at 6.4×.

**A successor authors the Shared domain and nothing else.** It does not begin
authoring the D-13 owner-authored cells of cursors it does not simulate, its
correction index keeps the same two exclusions, and no Player-domain state crosses
as part of the transfer. `SimulatesLocally` and `ResolveOwnedCursor` remain the
only admission checks. What it does gain is the admission surface: a joiner
dialling mid-succession is refused with a distinguishable tag
(`network.IsHandoffRefusal`) and may retry against whatever authority emerges,
because a participant admitted under a term that is about to end would hold a roster
slot the successor's record does not carry.

One thing the succession reads from the world rather than from the handshake: the
closed roster. A mid-run joiner is offered the lobby as it stood when it dialled,
so two participants admitted a minute apart hold two different lists and would
count two different majorities. The cursor roster does not have that problem — an
arrival and a departure are barrier-bound crossings every instance applies at one
agreed tick (D-11) — so the succession is computed over that.

### 4.4 Simulation and terminology remain extensible

Game guests continue simulating both the replicated Shared predictor and their own
Player domain. Keep role checks at authority/session seams, domain checks at
simulation ownership seams, participant IDs at roster seams and the term at the
authorship seam. The relay role is what the shaping was for, and it arrived as
predicted: a peer with retention rather than a change of topology. Nested player
domains and hierarchy remain unimplemented.

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

**Phase 7 makes the exposure strictly larger, and this is the plain statement of
it rather than a partial mitigation.** Nothing in the succession is
authenticated. A participant that lies about its direct links or its retention in
an `AuthorityReport` can make itself eligible; one that ignores the one-vote rule
can vote twice; one that fabricates a `HandoffRecord` with invented voters can
make every receiver adopt it, because a receiver counts the voter list against the
roster it holds and has no way to establish that those participants actually
voted. The structural checks that remain — a term is never skipped, one record per
term, a majority of roster members must appear — bound *accidents and races*, not a
hostile peer. A hostile peer that can now also *become* the authority is exactly
why authentication is the next security-shaped piece of work, and until it exists a
session is for trusted peers in a stronger sense than it was before this phase.

The relay role adds nothing to that exposure, and it is worth saying why: a relay
serves pages it did not author, and the receiver binds the answer to the
*authority's* root — the one it was sent in the manifest — and then re-derives that
root from the repaired capture. A relay that substitutes, truncates or answers
from a baseline of its own fails one of those two checks, by exactly the mechanism
that catches a corrupt wire.

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
- partition merging — refusing to merge is built, merging is not;
- multi-link CLI beyond what the relay role needs, and nested Player domains;
- tower ownership/config changes;
- adaptive playout lead;
- replacing deterministic guest simulation with thin clients.

These are explicit scope boundaries, not forgotten questions. Two items left the
list in Phase 7: coordinated host election and migration (§4.3), and the relay-peer
role (§7). What is left of the topology item is the CLI: `-join` dials one address,
so the links a shipped binary can build are still a star. The relay role works over
any graph the protocol is given — the mesh harness expresses several — and giving
the CLI a second link is a CLI change rather than a protocol one.

## 7. Known behaviour worth naming

Two things the current protocol does that a reader should not mistake for defects,
and one that is genuinely open.

**A relayed session keeps the selective stream when its relays hold retention.**
The gate used to ask "is every participant directly linked", because only a
directly linked one could answer the exchange. It asks "can every participant be
answered" now, and a relayed one can, because the neighbour that forwards to it
holds retention of its own.

Four properties make that a role rather than a routing layer:

- **One hop.** A relayed receiver's request travels to the neighbour that
  forwarded the manifest it names, and the answer returns along that edge. A relay
  that does not hold the named manifest does **not** forward the request onward: it
  answers "cannot serve", and the receiver degrades to the whole world the keyframe
  cadence is flooding anyway.
- **A relay cannot forge.** It serves pages it did not author, so the answer is
  bound to the authority's manifest twice over: the shard set must declare the root
  the receiver was sent, and the repaired capture must reproduce it. Substitution,
  truncation and a self-consistent set from another baseline each fail one of the
  two.
- **Retention is why it may answer at all.** An index enters a relay's ring only
  when the capture under it is provably the authority's — a whole correction that
  re-checked its own integrity hash, or a comparison that reproduced the
  authority's root. A relay therefore never holds a baseline of its own to serve
  from, and mixed-baseline assembly stays unreachable. It also forwards an index
  only for a tick it can already serve, so a participant behind it is never sent a
  question its neighbour would have to refuse.
- **Bounded staleness is stated, not hidden.** A relay's ring is
  `SnapshotManifestRetention` deep, so it is older than the authority's by
  construction. A request naming a tick it has dropped is answered in words, and
  the counters say how often (`snapshot.relay_served`, `snapshot.relay_unserved`,
  `snapshot.relay_retained`).

A session with a relay that holds nothing — one that has not yet installed an
authoritative capture — keeps today's whole-body flood, unchanged, and the reason
is logged rather than silent. The authority learns which participants can be
answered from the relaying neighbour's own answer, because it is the only instance
that knows.

**Relayed bytes are priced against the edge that carries them.** A repair a relay
serves is folded into the relay's own link plan, never the authority's; §3 measures
the same repair at 14.3 KiB either way, with 15.2 KiB of it moved off the
authority's uplink.

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
cursor, and `EventQuasarSpawnRequest` still crosses once. Only the per-instance effects changed.

The strobe follows the same cursor now. It was the one effect left session-wide,
and the reason was mechanical rather than designed — it shares no payload with the
other two — so `StrobeRequestPayload` grew a `cursor` field of its own, zero meaning
session-wide, admitted through the same check. What remains session-wide is the
region itself, one quasar at a time, because a per-cursor region would mean nested
player-domain state machines, which §6 rules out.

Beside it, an artifact an FSM region emits is now stamped player when its class is
Local. A region's actions run on every instance, so an effect one of them raises is
per-instance unless its class says otherwise — and the Local class already says so,
since it is what keeps the artifact off the wire. That emptied seven entries out of
`unstampedLocal`.

**A quasar's trigger is still a session total.** `kills.drain` counts every
participant's drain defeats, so the tenth in the session fuses a quasar rather than
one player's tenth. The answer is probably per-player, and the reason it is not
done here is specific rather than a shrug: the region it gates is *shared*, so
every instance has to enter it at the same tick, which means the guard has to read
a key every instance agrees on. What would move is therefore not the counter to a
per-slot key but the *tally the shared guard reads* — a per-cursor streak published
as one shared key, with the reset clearing the fusing cursor's rather than the
session's. That is a balance change, and it belongs with the tower ownership phase
(§4.5) rather than inside an authority phase.

## 8. Verification gates

Phase 7 is proved by:

- a three-participant session loses its coordinator and every survivor elects the
  same participant — the roster-lowest survivor with current retention, not the
  first to notice;
- a survivor that is behind is not elected even when the roster would choose it,
  and a partition that reaches no majority elects nothing and falls back to local
  continuation with `network.host_lost` on every survivor;
- two candidates cannot both reach a majority of one closed roster, and a second,
  different record for a term already adopted is refused rather than applied over
  the first;
- an artifact from the previous term is ignored and one from a term never handed is
  refused, neither moving the term this instance holds;
- roster, slot assignments, join anchor, barrier delay and every cursor entity are
  identical on every survivor across a handoff;
- a joiner dialling mid-handoff is refused with the distinguishable tag and lands
  in the same slot discipline on retry against the new authority;
- the first correction under a new term carries no keyframe and no whole body, and
  the exchange reaches hash-only at a bounded index and ack;
- a local fork that meets a higher term refuses it, reports it, and takes no state
  from either side;
- a three-participant chain keeps the selective stream, with the far participant
  answered from its neighbour's retention rather than by a whole-body flood, and
  the wire totals reported against the direct-link case;
- a page mutated, truncated or rebased at a relay is refused by the authority's own
  root and reaches the bounded keyframe fallback;
- a request naming a manifest the relay has dropped is answered "cannot serve" and
  degrades, never with a body from another baseline;
- a relay with no retention leaves the session on the whole-body flood and the
  reason is reported;
- selective apply still changes no Player-domain state and no D-13 owner-authored
  cell of a cursor the receiver authors, through a handoff and through a relayed
  answer;
- every survivor holds the same Shared world after the successor's first two
  corrections;
- the storm report publishes what a handoff, a relayed repair and a relay's
  retention actually cost.

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

Phase 7's is `script/phase7-migration.sh`, which runs *three* real processes over
sockets, applies the same stages, kills the coordinator mid-storm and reads the
term, authority, migration and repair counters out of all three logs. It states
which half of the phase it can prove and why: `-join` dials one address, so three
processes form a star, and a star's leaves cannot reach a strict majority — so what
that run demonstrates over real sockets is the fallback, the succession opening on
both survivors and finding nothing eligible, `network.fork` and
`network.host_lost` set on each, and — the invariant the run exists for — no term
ever claimed by two authorities. The elected-successor half needs a topology a star
is not, and is proved by the mesh suite in `internal/app`, which can express one.

Two things that run surfaced are worth recording because neither was visible with
one guest. A second mid-run join failed outright: the join gate holds a raw stream
until the start record arrives and knows which session frames may cross it in the
meantime, and Phase 6's manifest was never added to that list, so the first index
frame arriving mid-gate was read where the start record was wanted. And
`corrections_hash_only` is zero over a real socket on this fixture — it is zero on
the pre-Phase-7 tree too, so it is not a regression but the tick-alignment property
§7 already names, made worse by a wall clock and a storm.

Keep performance measurements reported rather than tied to wall-time assertions;
assert correctness, bounded allocation and meaningful byte reduction.
