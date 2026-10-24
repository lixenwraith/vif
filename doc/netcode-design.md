# Netcode design

The netcode as built, the readings that judge it, and what is still to change. vif
is not lockstep: every instance runs the whole Shared simulation at 20 ticks a
second, a guest predicting it and the authority correcting it (multi-player §10.3),
and the wire carries what a receiver cannot re-derive. §1 is the wire, §2 the
correction pipeline, §3 the readings, §4 the approaches not yet landed and §5 their
order and the decisions taken. Bare section numbers are this document's;
"multi-player §n" is [Multiplayer](multi-player.md), which holds the session
contract, event timing, membership and succession; "services §n" is
[Services and networking](services-and-networking.md), which holds the transport,
its handshake and its messages; D-n is the [domain model](domain-design.md).
Figures are §3's unless a line names another source. An approach that lands
leaves §4 and is described where it belongs above.

## 1. The wire

Four streams carry what a receiver cannot re-derive:

| Stream | Carries | Encoding | Rate and fan-out |
|---|---|---|---|
| Epoch (`MsgEvent`) | one source's closed tick of D-3 crossings, empty ones included | JSON `WireBatch`; each frame's payload is the journal's TOML text | every tick per source; raw up in one `MsgBundle`, the committed copies down every link in one `MsgBundle` a tick with the authority's own, the producer's own included |
| Owner sync (`MsgStateSync`) | one cursor's D-13 cells | the epoch's JSON envelope over a TOML payload of the cells that are set | every `NetworkSyncTicks` (6) per cursor, committed and relayed like an epoch |
| Correction | a manifest root and partition, then page hashes and shards; or a keyframe | JSON; keyframes and shard sets deflated by the snapshot codec | per peer every 2–60 ticks; a whole world or a hash-only proof within each 60-tick floor |
| Control | link probe and echo (5 Hz), state digest (every 6 ticks), roster, succession | small | per link |

Each link is one TCP or WebSocket stream per direction, deflated at level 7
(`NetworkStreamLevel`) against its own history, so a broadcast is deflated once per
link; a body the snapshot codec deflated passes it raw. The manifest
is a tree — root; a section per store and per carrier; up to 64 pages a section,
rows bucketed by a hash of their identity; rows — hashed with byte-serial FNV-1a
over each row's JSON, and row reuse keeps the JSON and hash of each row whose owned
copy still compares equal. Equal roots send no state. The root travels with its
partition, so a mismatch costs one round trip after it, for the receiver's page
hashes, before the shards arrive.

Per simulated second a host costs 20·T_sim + R·(capture + seal + index) plus the
bodies it serves, R being its publication rounds, and a guest 20·T_sim +
A·(capture + index) plus its installs, A being the manifests it answers. At 16
players (§3.1, which times seal, index, keyframe and tick on a slower
container):

| | Tower 239×64 | td 500×250 |
|---|---:|---:|
| Host egress before deflate | 1.64 MB/s | 5.05 MB/s |
| … of it epochs and owner syncs | 85% | 56% |
| … of it bodies and repairs | 10% | 43% |
| Rounds a second | 4.6 | 7.2 |
| Capture under the world lock, a round | 1.2 ms | 14 ms |
| Seal, index, keyframe | 1.8, 2.4, 7.6 ms | 5, 12, 58 ms |
| Simulation tick | 1.1 ms | 2.6 ms |
| Install projecting nothing, live lock held | 7–10 ms | 40–70 ms |

Four limits follow, and §4 is sorted against them:

1. **Bandwidth is quadratic at the root.** A star's authority sends every source's
   epoch and sync down every other link — 4,444 epochs and 736 syncs a second at 16
   players. Envelopes, encodings and the producer's own echo are constants on that
   term; only topology changes its order (N14).
2. **Protocol, not simulation, bounds the map.** A td host's rounds cost about
   220 ms of CPU a second (7.2 × (14 + 5 + 12)), its keyframes up to as much again,
   its simulation 52 ms.
3. **The live lock is held by O(E) work:** captures (100 ms a second at td), an
   install's comparison and write, and each projected tick.
4. **A browser guest has one thread** for simulation, inflate, its own captures and
   indexes, and installs.

### 1.1 Constraints

Every approach keeps these; one that needs another is out of scope here.

- **The whole world everywhere.** Any candidate may take the term and must answer
  the next manifest from retention (multi-player §5.1), so nothing filters crossings
  or corrections by interest. Relevance schedules a peer's cadence
  (`SnapshotRelevanceRadius`) and never filters.
- **Lossless, proved, whole repairs.** A repair reconstructs the authority's root and
  applies whole (D-23): an encoding may change, a value may not be rounded, and pages
  from two ticks never mix.
- **Order and containment.** The clock never rewinds, membership is by fence
  (multi-player §3.2), and a crossing is D-3's smallest outcome, never raw input.
- **Same-build determinism** over float64 (multi-player §8 item 3).
- **Reliable ordered streams**
  ([services and networking §7.3](services-and-networking.md#73-why-a-byte-stream-and-not-udp-or-icmp)).
- **Caches compare values they own;** per-store write counters stay rejected
  ([todo](todo.md#trim-what-remains-of-a-tick-and-a-checkpoint-at-the-map-limit)).
- **No compatibility kept:** an approach bumps `Schema`, `ManifestVersion`, the
  journal schema or the protocol.

## 2. The correction pipeline

The host publishes one authoritative capture on the session timeline, then serves
each direct peer according to that link's cadence, a power of two of ticks the peer
is due at multiples of, so peers on different cadences share rounds and the host's
round rate is its fastest peer's:

1. Build a deterministic manifest over component stores, allocator values, RNG
   streams, declared system state, status surface, and shared FSM state.
2. Send the root and header, with the partition: the page count of each section
   paged more than once.
3. If the receiver produces the same root, acknowledge a hash-only correction and
   install the authority header without transferring state. That answer proves the
   receiver holds the authority's world, so while every peer has proved it within
   half the convergence floor the keyframe cadence sends none. A participant behind
   a relay proves it in the relay's own answer.
4. Otherwise the receiver answers with the page hashes of every section of its own
   index under that partition, and the authority returns the pages that differ,
   chunked as a body is: one round trip from the root to the repair. A repair wider
   than the keyframe it stands in for is not sent: the peer gets the keyframe, and
   whole bodies for a few publications.
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
and `Apply` keeps such a body at the head of its inbox until the reset enters it. A body or a repair leaves in
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
comparing it off its tick. Indexes to a peer are spaced by its round trip, so a
descent's request finds its tick retained. A link that
cannot deliver a keyframe within `SnapshotKeyframeDrain` takes none on schedule: it
meets the floor with proofs, takes a whole world on request and reports
constrained. Echoes carry the answering side's clock, and delivery rate is
timed on it, since echoes a stalled path releases together arrive microseconds apart.

### 2.1 The playout buffer

The paths deliver worlds of different ages: a whole body is one one-way delay old,
a selective repair three — the root, then a round trip for the page hashes.
Adopting each capture's tick would step the clock by up to two one-way delays and
every shared actor with it, so a capture behind the clock is projected (multi-player §3.3) and
moves nothing; the buffer is for the other direction.

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

## 3. Readings

§3.1 counts bytes framed before deflate on the in-process mesh, which does not
deflate, and times stages rather than their parts; §3.3 reads the socket path, and
§3.4 each phase-1 item as it landed. Each reading still missing decides approaches:

| Reading | Decides |
|---|---|
| Wire bytes after the link's deflate, by message kind, on the socket path | N1–N3 |
| CPU by stage on host and guest: JSON and TOML encode and decode, per-link deflate and inflate, FNV against row encoding inside seal and index, the digest | N2, N9, N12, N15 |
| Keyframes by reason — schedule, floor, request, repair wider than the keyframe, failed proof, the bodies after a fallback — and how many are encoded for how many sent | N8 |
| Plain bytes per section of a td keyframe | N8, N9 |
| Lock hold by part: stores, carriers, status, comparison, write, each projected tick | N10, N11 |
| Repairs by cause: the sections carried, and whether the authority committed one of the receiver's crossings late or void since its last proof | the next divergence fix |
| Heap by owner: retention ring, staging world, encoder caches | N9, N13 |

### 3.1 Operating point on the in-process mesh

The same shape over sockets, with each link's deflate, is read in §3.3: the wire
carries 8% of the framed command stream at 16 players on the tower, and td's whole
bodies as framed, since a body the snapshot codec deflated passes each link raw.

Measured on the in-process mesh at `GOMAXPROCS=1` (a 2.1 GHz Xeon container), in the
shape of `TestATowerGuestAnswersHashOnly`: a star with four-tick links, the authority
playing, every participant moving one tick in three and firing one in six, 300 ticks
after 120 of warm-up. Rates are per simulated second; host and guest figures time
their own calls in one shared process, so garbage collection falls on whoever
allocates. Absolute values move with the machine and the scenario; the shape is what
to plan by. Tower is the 239×64 tower region, td the 500×250 `td` scenario.

| Players | Tower, 2 | Tower, 16 | td, 2 | td, 16 |
|---|---:|---:|---:|---:|
| Publication rounds a second | 4.5 | 4.6 | 1.5 | 7.2 |
| Manifests answered hash-only | 94% | 94% | 78% | 95% |
| Host egress, framed before deflate | 20 kB/s | 1.64 MB/s | 0.28 MB/s | 5.05 MB/s |
| … of it epochs and owner syncs | 5 kB/s | 1.39 MB/s | 11 kB/s | 2.82 MB/s |
| … of it correction bodies and repairs | 11 kB/s | 0.17 MB/s | 0.27 MB/s | 2.18 MB/s |
| Heap in use per instance | 70 MiB | 90 MiB | 210 MiB | 250 MiB |

Per round the host captures under the world lock (about 1.2 ms tower, 14 ms td),
then seals and indexes the capture; a keyframe is about 26 / 360 kB compressed. The seal,
the index and a keyframe reuse the bytes and hash of every row whose owned copy
still compares equal, and navigation rebuilds its wall grid only when a wall moved
and compares grids only when their generation moved. At 16 players, rounds four
ticks apart (on a slower container than the table's), the seal costs 1.8 ms on the
tower map and 5 ms on td, the index 2.4 and 12 ms, a keyframe 7.6 and 58 ms (mostly
deflate), and a simulation tick 1.1 and 2.6 ms. An install that projects nothing
holds the live lock 7–10 / 40–70 ms, and each projected tick adds up to a simulation
tick. Sealing, indexing and encoding run outside the lock, but on the one thread a
browser guest has.

Per simulated second the host costs 20·T_sim + R·(capture + seal + index) plus the
whole bodies it serves, R being the union of its peers' due ticks rather than a fixed
5 Hz; a guest costs 20·T_sim + A·(capture + index) + installs·(stage + projection +
compare and write), A being the manifests it answers. A star's authority relays every
participant's epochs and owner syncs to every other, (N−1)² of each: at 16 players
4,444 epochs (190 B tower, 510 B td) and 736 syncs (about 540 B, the cells that are
set) a second.

What that means for scaling:

- **Protocol, not simulation, bounds the map limit.** Before row reuse, td with 16
  players needed about 1.4 cores of a host, 86% of it protocol: rounds of capture,
  seal and index cost about 100 ms each, 0.7 CPU-s/s at 7.2 rounds, and whole bodies
  most of the rest; the fleet pod's 500m CPU held the tower map at 16 players and
  not td. Row reuse takes about nine tenths off the seal and index, which leaves the
  capture under the lock the largest part of a round.
- **A td guest cost 0.4–0.5 CPU-s/s natively before row reuse,** more than a
  browser's single WASM thread holds; the tower map cost a guest about 0.1.
- **Bandwidth is quadratic before it is anything else.** Epoch and owner-sync fan-out
  is 85% of the tower host's egress at 16 players; at td whole bodies come close.
- **The live lock is held by the O(E) capture and comparison** before any projection,
  so shrinking them comes before moving projection out of the lock.

### 3.2 Scenarios

One matrix judges every approach, on harnesses that exist:

| Axis | Values | Harness |
|---|---|---|
| Map | tower, td | `TestATowerGuestAnswersHashOnly` (§3.1's shape), `TestCostAtTheStormHighWater`, `BenchmarkCaptureAtStormHighWater` |
| Players | 2, 4, 16 | the same, seated with bots |
| Link | in-process; four-tick links; staged latency, loss and byte budget | `network.LinkShape`, `TestStagedLinkShapingKeepsCorrectionsBoundedAndRecovers` |
| Path | in-process mesh; TCP with deflate; WebSocket to a browser | `TestNetcodeReadingsOverSockets` (§3.1's shape over sockets, `VIF_NETCODE_READINGS=1`, a CPU profile per row under `VIF_NETCODE_PROFILE=<dir>`); a browser joiner |
| Host | native at `GOMAXPROCS=1`; a fleet pod at 500m CPU; a WASM guest | `BenchmarkHeadlessTicks`; [Measure a full roster](todo.md#measure-a-full-roster) |
| Load | idle; a move every third tick and a shot every sixth; the storm high water | §3.1's profile, the storm fixture |

Metrics, from multi-player §7's telemetry where it exists: CPU-s/s by stage;
world-lock hold, p50 and p99; egress before and after deflate by kind
(`network.sent_<kind>_bytes` framed before each link's deflate, `network.wire_bytes_out`
after it); the hash-only share; repair and keyframe bytes; `snapshot.correction_*`
and `snapshot.projected_ticks`; `network.barrier_delay_ticks` and
`network.commit_late`; heap in use. An approach lands when it moves its target and leaves the hash-only
share, correction magnitude and the protocol tests where they were.

### 3.3 Readings over sockets

`TestNetcodeReadingsOverSockets` on 2026-10-24, one process on four cores, every
instance paced at the tick: the host's egress per simulated second, framed before
each link's deflate by what the frames carried, and on the wire after it.

| Row | Epochs | Syncs | Digests | Bodies | Repairs | Control | Framed | Wire | Wire/framed |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tower, 2 | 6.8 kB/s | 5.2 kB/s | 0.8 kB/s | 0 | 2.8 kB/s | 0.4 kB/s | 16 kB/s | 2.3 kB/s | 0.14 |
| tower, 4 | 47 kB/s | 32 kB/s | 2.4 kB/s | 0 | 8.7 kB/s | 1.3 kB/s | 91 kB/s | 10 kB/s | 0.11 |
| tower, 16 | 841 kB/s | 633 kB/s | 12 kB/s | 0 | 58 kB/s | 6.7 kB/s | 1.55 MB/s | 124 kB/s | 0.08 |
| td, 2 | 13 kB/s | 4.8 kB/s | 0.8 kB/s | 96 kB/s | 2.7 kB/s | 0.4 kB/s | 117 kB/s | 79 kB/s | 0.68 |
| td, 4 | 97 kB/s | 29 kB/s | 2.3 kB/s | 489 kB/s | 20 kB/s | 1.3 kB/s | 638 kB/s | 411 kB/s | 0.64 |

Beside them: 5 rounds a second on every row; keyframes none on the tower, 0.13 and
0.27 a second at td with no fallback; every tower manifest and 98.6% of td's at two
players answered hash-only, 69% at td with four, where repairs ran at 1.4 shard sets
and 12 pages a second (8 kB/s framed); a host tick with its publication 1.3–1.7 ms
on the tower and 7.3–8.1 ms at td; the whole process 0.09, 0.17 and 0.61 CPU-s per
simulated second on the tower at 2, 4 and 16 players and 0.37 and 0.84 at td with 2
and 4, its heap 38, 118 and 493 MiB and 328 and 992 MiB. The framed figures agree
with §3.1's in-process ones (1.55 against 1.64 MB/s at 16 players).

CPU by stage, the share of the process's sampled time in the measured window, every
instance together, so the runtime's shares are the process's and not one host's:

| Stage | tower, 4 | tower, 16 | td, 2 | td, 4 |
|---|---:|---:|---:|---:|
| Simulation and everything unlisted | 40% | 39% | 28% | 35% |
| Allocation and garbage collection | 12% | 11% | 30% | 24% |
| Scheduler and other runtime | 11% | 11% | 19% | 15% |
| Sockets and system calls | 13% | 6.5% | 3.1% | 1.6% |
| Per-link deflate | 4.7% | 12% | 0.2% | 4.2% |
| Inflate | – | 0.4% | 0.7% | 1.2% |
| JSON encode and decode | 5.3% | 6.9% | 4.2% | 7.0% |
| TOML encode and decode | – | 2.6% | – | – |
| FNV hashing | 9.9% | 7.0% | 11% | 7.7% |
| Seal, index and keyframe encode | 4.1% | 3.5% | 2.1% | 3.4% |
| Protocol (`converge`, `network`) | 0.6% | 0.2% | 1.9% | 0.9% |

What the readings decide:

- **The per-link stream takes 86–92% off the command stream.** Epoch and sync
  envelopes and TOML payloads compress 7–12× against their own history, better as
  players grow, so N1, N2 and N3 cut framed bytes 2–10× and wire bytes by a fraction
  of 124 kB/s at 16 players; their worth is CPU — JSON and TOML 7–10%, the deflate
  that scales with their framed bytes 12% at 16 players, and the allocation behind
  the collector's 11–30% — and the browser guest's one thread. One dictionary
  deflate for every link in place of the per-link stream is refuted: no preset
  dictionary matches a per-link history that compresses this well.
- **At td the bodies are the egress**: whole corrections are 77–82% of the framed
  bytes, and the 0.64–0.68 ratio was not the command stream's alone: the link's
  level-7 stream took a fifth off the snapshot codec's level-1 output. Compressing
  a body once at the level the wire gets and carrying it raw past the stream keeps
  that fifth and the host's second pass (landed; §3.4). N6 and N5 follow it there,
  where 31% of manifests at four players are not answered hash-only and keyframes
  run at a quarter a second.
- **FNV is 7–11% and the seal and index 2–4%**, so N12 pays, before or after N10's
  in-place root; N15's digest hash is part of that share.
- Not yet taken, and the harness is where they go: keyframes by reason (`keyframes`
  and `keyframe_fallbacks` alone are read), plain bytes per section, lock hold by
  part, repairs by cause, heap by owner, and the WebSocket path to a browser.

### 3.4 Phase 1, item by item

The same harness on the same machine, 2026-10-24, one run per item on top of the
ones before it: the host's egress per simulated second framed and on the wire, in
kB/s, and the process's CPU-s per simulated second. td's rows compare only where
their keyframe rate agrees, since what its storm does to the roster differs run to
run (none to 0.5 keyframes a second at four players), and the tower's wire is read
against a run-to-run spread of about 2%.

| Change | tower, 2 | tower, 4 | tower, 16 | td, 2 | td, 4 |
|---|---:|---:|---:|---:|---:|
| Baseline, framed → wire; CPU | 16.0 → 2.32; 0.09 | 91.3 → 9.99; 0.16 | 1,552 → 125; 0.64 | 94.7 → 61.5; 0.40 (0.13 keyframes/s) | 514 → 359; 1.06 (0.53/s) |
| Bodies raw past the stream, codec at level 6 | 16.0 → 2.46; 0.09 | 91.3 → 10.4; 0.16 | 1,559 → 127; 0.67 | 80.8 → 62.0; 0.43 (0.13/s) | 140 → 13.6; 0.77 (none) |
| Syncs of the cells that are set | 14.1 → 2.43; 0.09 | 80.0 → 10.4; 0.16 | 1,326 → 124; 0.64 | 88.6 → 71.1; 0.44 (0.13/s) | 175 → 60.9; 0.67 (0.13/s) |
| Repairs chunked, bounded by the keyframe | 14.1 → 2.43; 0.09 | 80.0 → 10.3; 0.17 | 1,324 → 124; 0.62 | 76.9 → 59.7; 0.42 (0.13/s) | 189 → 73.4; 0.71 (0.13/s) |
| The descent in one round trip | 14.6 → 2.43; 0.08 | 81.5 → 10.3; 0.16 | 1,330 → 123; 0.59 | 88.0 → 70.4; 0.40 (0.13/s) | 207 → 92.0; 0.76 (0.20/s) |
| Cadences a power of two, due ticks aligned | 14.6 → 2.44; 0.09 | 81.4 → 10.4; 0.16 | 1,331 → 123; 0.60 | 87.9 → 70.3; 0.40 (0.13/s) | 193 → 77.7; 0.80 (0.13/s) |
| The down path bundled, one frame a link a drain and a close | 14.7 → 2.44; 0.09 | 80.3 → 10.0; 0.16 | 1,291 → 109; 0.60 | 88.2 → 70.5; 0.43 (0.13/s) | 129 → 14.6; 0.67 (none) |
| The bundle the port's, a link's order kept | 14.7 → 2.46; 0.09 | 80.4 → 10.0; 0.16 | 1,291 → 111; 0.60 | 20.4 → 3.20; 0.37 (none) | 163 → 49.7; 0.74 (0.07/s) |

- **Bodies raw, codec at level 6.** A td keyframe is 360 kB where level 1 gave 501
  and the stream's second pass over that 398: td's framed body bytes fall a fifth
  and its wire stays where the two passes had it, now one pass. The codec's pass is
  33 ms a keyframe instead of 12, and the stream's 9 ms a link over it is gone, so
  the host gains from three links up and a guest inflates each body once. The
  tower, which sends no bodies, pays the run header — a kind byte, a wire length
  and a frame count a burst a link — about 2% of its wire. Deflate's share of the
  process is unchanged inside its run-to-run spread (6–12% at 16 players).
- **Syncs of the cells that are set.** Sync bytes fall 36% framed on the tower and
  43% at td, where cursors idle more — a tower sync at 16 players from about 850 to
  540 B framed — and the wire by 1–3%, since the stream was already compressing the
  zero cells against their predecessors; TOML's share of the process at 16 players
  goes from 2.9% to 1.9%, inside the spread. The immunity array became a slice so
  an unspent one is omitted too.
- **Repairs chunked, bounded by the keyframe.** No repair in these runs was wider
  than a frame (`keyframe_fallbacks` 0 on every row before and after), so the rows
  read as the run before them; the step it removes, a 48 kB repair giving way to a
  360 kB keyframe, is held by its test and waits for a run where td's storm
  provokes it. What changed in the rows: an epoch behind a repair now waits one
  4 KiB chunk rather than up to 48 kB.
- **The descent in one round trip.** The partition on every manifest adds a fifth
  to the manifest bytes framed (2.8 → 3.3 kB/s at two players) and nothing to the
  wire, where it repeats; a differing root costs the receiver's whole page vector
  up the link, 1.6 → 2.1 kB/s of ingress at td with two players. What moved: the
  ticks a td guest projects on a repair fell from 3–8 to 1, the repair landing a
  round trip sooner, and in a session at the site, where 6% of repairs arrived
  after the receiver had moved past their tick, that round trip is the one that
  was lost.
- **Cadences a power of two, due ticks aligned.** The harness's peers already
  shared one cadence and one phase, so its rounds were 5 a second before and after;
  the rows read as the run before them, and the rule is held by its test. It pays
  where cadences differ: at 16 players on td §3.1 counted 7.2 rounds a
  second against a nominal 5.
- **The down path bundled.** The copies the authority commits in a drain leave each
  link as one `MsgBundle` when the drain ends, and its own epoch and syncs as one at
  the close, in place of a frame per source: 4% off the wire at four players and 11%
  at 16, where 16 frame headers a tick a link became two. Bundling the close with the
  drain's copies into one frame was measured too and refused: the copies left one
  tick body later, and in the harness's lockstep, where a guest sits no closer than
  the host's next tick, the first guests read them a tick late and the tower's
  hash-only share fell to 99.3%. The bundle is the port's, and a frame of any other
  kind releases the link's before it: the first cut bundled in the network system,
  where the handoff record a holder sends when its host drops, which the session
  layer puts on the port directly, overtook the epochs the holder had relayed in the
  same drain. What that exposed is the receiver's side of a handoff: a bot a playout
  behind its relay read the record against a world that had not applied its own
  seat's crossing yet, refused it as a different roster, and forked. The record's
  check now compares the players alone (multi-player §5.2); a seat crosses at its
  holder's word and is reconciled after the handoff as the roster is.

## 4. What remains

In the table − lowers a cost, + raises it and · leaves it. Projections come from
§1's figures; §3 confirms or refutes them.

| | Approach | CPU | Mem | Bandwidth | Latency | Complexity | Projected |
|---|---|:-:|:-:|:-:|:-:|---|---|
| N1 | A frames-less echo | · | · | − | · | low | a leaf's own epochs off its downlink |
| N2 | A binary wire codec | − | − | − | · | low, then medium | a four-move epoch 567 → ~50 B before deflate |
| N3 | Owner syncs that carry what changed | − | · | − | · | low | syncs ~10× smaller before deflate |
| N8 | Keyframes from cached sections | − | + | + | · | medium | keyframe deflate in proportion to the sections that moved |
| N9 | A binary row codec | − | − | − | · | high | rows 3–10× smaller before deflate |
| N10 | Converged answers without a capture | − | · | · | − | medium | a guest's capture on 95% of its answers |
| N11 | Less work under the live lock | · | − | · | − | high | a deep install's lock from past 100 ms to about a tick at td |
| N12 | A faster deterministic hash | − | · | · | · | low | hashing ~10× faster, at a share §3 measures |
| N13 | Retention by reference | + | − | · | · | medium-high | retention from ~4 worlds to ~1 |
| N14 | Relay trees chosen by the authority | · | · | − | + | high | root egress O(N²) → O(N·k) |
| N15 | One parity mechanism | − | · | − | · | low | an O(E) hash every 6 ticks per instance |

### N1. A frames-less echo

The authority sends each link one frame a tick, the copies it committed in the
tick's drain and its own epoch and syncs bundled (landed; §3.4). What the bundle
still carries is each producer's own committed copy with its frames, which the
producer already holds: a link with nothing behind it needs only the marker — the
epoch's `Age`, and which frames were committed late or void — and a relay's link
needs the frames for the participants behind it alone. In the fleet run the echo was
a quarter of a two-seat guest's downlink.

- *Bandwidth*: a leaf's own epochs off its downlink, 1/N of it on a star.
- *Challenges*: the authority has to know which links have nobody behind them
  (`Relayed` in their answers, the roster's holders); `noteOwnCommit` has to take a
  marker that carries no frames; with N2's envelope the marker is a few bytes.

### N2. A binary wire codec

A `WireFrame` names its event and domain by string (`"EventCursorMoveRequest"`,
`"shared"`) and carries the journal's TOML text inside a JSON batch; a four-move
epoch is about 567 bytes. Two steps:

1. **Envelope.** The event as its registry index, the domain in a flag byte, sequence
   and apply tick as varint deltas from the epoch's produced tick and the frame
   before. Payloads stay the journal's TOML, so one payload encoder still serves
   replay and transport. Manifests, requests and shard sets carry the header's
   per-tick fields and leave the session identity the handshake checked.
2. **Payload.** A codec generated beside `registry_gen.go` for every payload struct —
   fixed field order, varints, float64 as its bits — which the journal uses too,
   with a name table in its anchor so it reads across builds and `vif-log`
   rendering the text it shows today. A binary wire beside a TOML journal would be
   two encoders for one payload.

- *CPU*: removes reflection-driven JSON and TOML from every crossing at every
  receiver: allocation and garbage a WASM guest pays for on its one thread.
- *Bandwidth*: the four-move epoch to about 50 bytes before deflate; less after,
  since the stream already compresses repeated keys against its history.
- *Memory*: a smaller replay suffix (`SnapshotReplayBytes`) and barrier schedule.
- *Challenges*: step 2 bumps the journal schema; slices (`WeaponCharges`) take a
  length prefix; class, stamped-domain and `Derived` rules stay in the registry; the
  codec's version joins the protocol the handshake compares.

### N3. Owner syncs that carry what changed

A sync is 27 cells every six ticks per cursor, and syncs were a fifth of the command
stream. `omitempty` on `CursorStatePayload` landed (§3.4): a sync carries the cells
that are set, and the immunity array became a slice so an unspent one is omitted
too. Next, in order of cost: with N2 a presence mask; then a sync sent only when a
cell differs from the last one sent, with a refresh every K syncs, so a frame a full
queue refused costs bounded staleness. A sync stays a whole state, never a delta on
an unacknowledged base, so a loss costs one refresh rather than a broken chain.
Cells every receiver re-derives (the shield's inverse squares from its radii) leave
the payload.

- *Bandwidth*: 736 syncs a second of 760 bytes at 16 players, 0.56 MB/s before
  deflate, about a tenth with a mask, less again while cursors idle; how much less
  depends on which cells change every tick (heat, energy).
- *Determinism and latency*: unchanged — a sync is still committed at one tick for
  every instance, and mirrors already lag by `NetworkSyncTicks`.

### N8. Keyframes from cached sections

Row reuse makes a keyframe's JSON cheap, but its deflate still runs over the whole
capture: 58 ms at td. Compress each section on its own, keep its compressed bytes
while its section hash holds, and send a keyframe as a container of section blobs.
td is a sprawling maze in which every wall cell is a Shared entity with a position
and a wall row, so most of a keyframe would be blobs already compressed. A join's
capture (`EncodeCapture`) takes the same container.

- *CPU*: §3.1's figures put up to four 58 ms keyframes a second on a td
  host with 16 players, as much as its rounds; this takes the static sections out of
  each.
- *Bandwidth*: a few percent more, each section losing the history of those before.
- *Memory*: the cached blobs, hundreds of kilobytes at td.
- *Challenges*: a container format and its decoder. Integrity is over rows rather
  than body bytes, so the proof is unchanged.

### N9. A binary row codec

Rows are JSON: an entity id, field names, and floats in shortest form — a kinetic
row's six float64s take up to about 170 bytes where 48 suffice. Generate a binary
encoding per component in `internal/gen-manifest`, beside the stores — fixed field
order, varints, float64 as its bits — and seal, hash and send that. A keyframe lays
each section out by column: entities as varint deltas in canonical order and each
field delta-coded, which shrinks static, spatially coherent sections such as walls
further than deflate alone.

- *CPU*: no reflection or float formatting on encode or decode, and seal, index and
  deflate over a fraction of the bytes.
- *Memory*: encoder caches, retained indexes and staging inputs by the same ratio.
- *Bandwidth*: smaller repairs and keyframes, mostly before deflate.
- *Challenges*: every store, the cursor's `Control` normalisation and the
  owner-authored exclusions move to the codec; carriers, status and the FSM can stay
  JSON at first, being few rows; a capture needs a readable rendering for diagnosis
  (`vif-log`).

### N10. Converged answers without a capture

A guest answers each manifest with a capture and an index, as the host builds them,
although 94–95% of answers at 16 players are hash-only and need only a root. Keep an
owned copy and row hash for each live row, as the encoder does for a capture; at the
close of the tick the manifest names (`Corrections.TickClosed`, under the tick's
lock) walk the live stores, compare each row with its copy, rehash only the rows
that differ and compute the root in place; capture only when the root differs, at
that same close. A paced guest holds the manifest before it runs the tick
(§2.1), so the root is in hand as the tick closes; a manifest that
arrives later takes today's path.

- *CPU and lock*: a converged answer is an O(E) comparison with no allocation rather
  than a capture's copies and a JSON pass — the host's capture alone is 14 ms at td —
  and a WASM guest gains the most.
- *Memory*: the owned copies, which the encoder already keeps.
- *Challenges*: the in-place root must equal what a capture and index produce,
  normalisation and owner-authored exclusions included, and one test holds them
  equal. The cache compares values it owns, so it is not the rejected write counter.

### N11. Less work under the live lock

Two todo items, in the order
[Project a correction outside the live lock](todo.md#project-a-correction-outside-the-live-lock)
sets. First the capture becomes a copy: stores and carriers read into reusable
buffers under the lock, and canonical order, encoding and hashing after it. Then the
projection leaves the lock: simulate the staging world unlocked to the tick the live
world had reached, take the lock, simulate the ticks it ran meanwhile with the
crossings it applied, compare and write. The locked tail is the ticks that passed
during the unlocked part, and the write.

- *Lock*: the ~100 ms a second of capture at td falls toward the cost of a copy; an
  install 16 ticks behind holds the lock for about a tick and its write rather than
  past 100 ms.
- *CPU*: about the same; the tail simulates only the ticks the live world ran
  meanwhile.
- *Memory*: buffers reused each round rather than allocated.
- *Challenges*: everything multi-player §3.3 requires — fence membership, pending
  barrier crossings not fed, the ledger settled before projection, the journal's
  place, no live tick between the final projection and the write; a WASM guest gains
  scheduling, not parallel CPU.

### N12. A faster deterministic hash

FNV-1a is byte-serial: one dependent 64-bit multiply per byte, a few hundred
megabytes a second. A word-at-a-time hash of the xxh64 class, written in `pkg/vmath`
beside the FNV constants, runs about ten times faster. It must be fixed across
platforms and processes, which rules out `hash/maphash`. Row reuse already limits
hashing to rows that changed, so this pays only if §3 finds hashing a real share of
seal, index and digest; it bumps `ManifestVersion` and `Schema`.

### N13. Retention by reference

The authority retains `SnapshotManifestRetention` (4) captures with their indexes and
every guest what it proved, each a whole set of rows although consecutive ones share
most of them. Hold retained rows as immutable page blocks that consecutive captures
share, so the ring costs one world and what changed, or hold retained pages encoded
and decode those a request names. A td participant is about 300 MB, mostly captures
and the staging world; this takes the retention share. CPU rises only on serving a
page, which is rare, and N9's encoding makes the pages small.

### N14. Relay trees chosen by the authority

The protocol already relays down a tree: `-relay`, `forwardDown`, proofs carried in
a relay's answer. The authority could assign participants with measured headroom to
relay subtrees, turning its egress from O(N²) into O(N·k). §5 decided against it on
§3.3's wire reading until a measured uplink needs it; the open parts are in
[Reach a relay](todo.md#reach-a-relay-without-an-address-in-hand).

- *Bandwidth*: the root's egress divided by the fan-out; each relay pays its
  subtree's.
- *Latency*: a relayed participant's crossings and corrections take one more hop, and
  its lead rises by that hop.
- *CPU*: per-link deflate and sends move to the relays.
- *Challenges*: choosing and re-choosing relays, re-parenting when one leaves, a
  relayed participant inheriting its relay's cadence
  ([domain model §7](domain-design.md#7-current-limits)), and a browser relaying only
  its own bots. It pays where the host's uplink is the limit, a person hosting at
  home; a fleet pod runs out of CPU first.

### N15. One parity mechanism

Every instance hashes its Shared world for a `MsgStateDigest` every
`NetworkDigestTicks` (6), a diagnostic that feeds `network.digest_mismatches` and
`network.drift_part` and nothing else (D-23), while the manifest exchange proves
parity every round, section by section: two mechanisms for one job. Name the first
differing section in the manifest answer, a finer `network.drift_part` than a digest
category, and retire the digest — one O(E) hash every six ticks per instance less,
priced by §3. Drift is then seen at the cadence (2–60 ticks) rather than every six,
and the digest's noise on a mixed-platform session (multi-player §8 item 3) goes
with it.

## 5. Order

Each approach lands alone, re-runs §3's matrix and updates §3.1.

| Phase | Approaches | Why |
|---|---|---|
| 0 | §3's readings | Taken in §3.3 for bytes by kind on the wire and CPU by stage; the rest go into the same harness as an approach needs them |
| 1 | N1, N2 step 1 | Low complexity, local to `internal/system/network.go`, `internal/converge` and `internal/network`: the command stream's constants for their CPU |
| 2 | N10, N12, N8, N11's capture as a copy | CPU and lock at the map limit, on the host and the browser guest |
| 3 | N2 step 2, N9, N13, N11's projection, N15, N14 | Formats and structure: the journal, the capture schema, retention, topology |

N2's second step follows its first; N13 and the column layout build on N9; N10's
row cache is cheaper on N9's rows.

**Decided, on §3.4.** The down path is bundled (N1's first step): the port packs
what a link is sent of epochs and syncs, up or down, and releases it when a drain
ends, when the tick closes and before a frame of any other kind, so a link keeps
its order; the frames-less echo remains §4's N1. A peer's cadence is a power of two of ticks
and it is due at multiples of it (N7), so peers on different cadences share rounds
and the host's round rate is its fastest peer's. The descent is one round trip (N5):
the manifest carries its partition, a receiver whose root differs answers with the
page hashes of every section under it, and the index message is gone; the shard set
still carries every section summary. A repair is chunked and paced as a body is and
bounded by the keyframe it stands in for (N6): the measured one at the authority,
the transfer ceiling at a relay, which measures none
([todo](todo.md#bound-a-relayed-repair-by-a-keyframe)). Syncs carry the cells that
are set (N3's `omitempty`), 36–43% off their framed bytes and 1–3% off the wire; the
presence mask and the send-on-change step wait for N2. Bodies pass the link raw,
compressed once at level 6 (N4's first step): the codec's level was 1 and the link's
level-7 stream took a fifth off its output, so a level-1 body carried raw would have
cost td a quarter more wire, where at level 6 a td keyframe is 360 kB against 398
after both passes and the host's pass is 33 ms instead of 12 plus 9 a link. A run
cut on the inflater's window would deliver its last frame before its marker, so a
raw frame due there travels in the run. N4's second step, a dictionary deflate, is
dropped on §3.3.

**Decided, on §3.3.** The star stays, with the authority the only publisher and
every participant's epochs and syncs relayed down every link: its wire cost at 16
players on the tower is 124 kB/s after deflate, which a home uplink carries, so
relay trees chosen by the authority (N14) wait for a measured uplink that does not.
The command stream's constants are cut in phase 1 as the wire column judges them,
not the framed one: N1's echo, which a guest with nothing behind it was reading the
marker of, then N2's envelope.
