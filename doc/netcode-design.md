# Netcode design

The netcode as built, the readings that judge it, and what is still to change. vif
is not lockstep: every instance runs the whole Shared simulation at 20 ticks a
second, a guest predicting it and the authority correcting it (multi-player §10.3),
and the wire carries what a receiver cannot re-derive. §1 is the wire, §2 the
correction pipeline, §3 the readings, §4 phase 2's approaches, landed or decided on
§3's readings, and §5 what follows. Phase 1, four iterations in 2026-10, is what
§1–§3 describe as built, with phase 2's five: the wire codec, the authority's FSM
transitions, answers indexed in place under a word hash, and keyframes from cached
sections; §3.6 is phase 2 in measurements. Bare section numbers are this
document's; "multi-player §n" is [Multiplayer](multi-player.md), which
holds the session contract, event timing, membership and succession; "services §n"
is [Services and networking](services-and-networking.md), which holds the
transport, its handshake and its messages; D-n is the [domain model](domain-design.md).
Figures are §3's unless a line names another source.

## 1. The wire

Four streams carry what a receiver cannot re-derive:

- **Epoch** (`MsgEvent`): one source's closed tick of D-3 crossings, empty ones
  included. A `WireBatch` in the binary envelope (services §8): each frame its
  event as the registry's index, its domain in a flag byte, its sequence and apply
  tick as varint deltas, and its payload in the generated codec the journal writes
  too (`payload_gen.go`): a presence mask, then each set field in declaration
  order as a varint, a float64's bits, or a length-prefixed string or slice. Every
  tick per source: raw up the one link toward the authority, the committed copy
  down every link, the producer's own included.
- **Owner sync** (`MsgStateSync`): one cursor's D-13 cells, the ones that are set,
  which the codec's mask names. The epoch's envelope. Every `NetworkSyncTicks` (6)
  per cursor, committed and relayed as an epoch is.
- **Correction**: a manifest root with its partition, then the receiver's page
  hashes and the shards that differ; or a keyframe. JSON, with keyframes and shard
  sets deflated by the snapshot codec at level 6. Per peer every 2–60 ticks; a whole
  world or a hash-only proof within each 60-tick floor.
- **Control**: link probe and echo at 5 Hz, the state digest every 6 ticks, roster
  and succession records. Small, per link.

Each link is one TCP or WebSocket stream per direction, deflated at level 7
(`NetworkStreamLevel`) against its own history, so a broadcast is deflated once per
link and compresses 7–12× against that history. The stream is segmented: a deflate
run of frames, opened by a kind byte, its wire length and its frame count, or one
raw frame whose body the snapshot codec deflated already
(`Traits.Precompressed`), since a body compressed once at the level the wire
gets costs the host one pass and the link none. A run that would end on the
inflater's window keeps the raw frame due next, whose last frame is delivered
before the flush marker is read.

The port packs what a link is sent of epochs and syncs into one `MsgBundle`
(`MessageType.Bundles`) and releases each link's when a drain ends and when the tick
closes, so a link reads one or two frames a tick whatever the roster; a frame of any
other kind releases the link's bundle before it, so a link reads its frames in the
order they were sent, which the session layer relies on when a handoff record
follows the epochs a holder relayed. A correction chunk travels alone. A producer
reads its own committed copy for its age, the path it measures and the link it
proves; one with nobody behind it marks its epochs and syncs `Leaf`, and whoever
sends its copy down gives its own link the epoch without its frames and no sync at
all, since it holds both.

The manifest is a tree: root; a section per store and per carrier; up to 64 pages a
section, rows bucketed by a hash of their identity; rows, hashed a word at a time
(`vmath.Hash64`) over each row's JSON, where row reuse keeps the JSON and hash of
each row whose owned copy still compares equal. A guest indexes the live rows in
place as the tick a manifest names closes and captures nothing while the roots
agree. Equal roots send no state. The root travels
with its partition, the page count of each section paged more than once, so a
receiver whose root differs answers at once with the page hashes of every section
under it: one round trip from the root to the repair.

Per simulated second a host costs 20·T_sim + R·(capture + seal + index) plus the
bodies it serves, R being its publication rounds, and a guest 20·T_sim +
A·(capture + index) plus its installs, A being the manifests it answers. At 16
players (§3.1, which times seal, index, keyframe and tick on a slower container;
the tower's egress is the binary codec's, td's from before it):

```
                                        Tower 239x64        td 500x250
Host egress before deflate              0.23 MB/s           5.05 MB/s
  of it epochs and owner syncs          69%                 56%
  of it bodies and repairs              28%                 43%
Rounds a second                         4.6                 7.2
Capture under the world lock, a round   1.2 ms              14 ms
Seal, index, keyframe                   1.8, 2.4, 7.6 ms    5, 12, 58 ms
Simulation tick                         1.1 ms              2.6 ms
Install projecting nothing, lock held   7-10 ms             40-70 ms
```

Four limits follow, and §4 is sorted against them:

1. **Bandwidth is quadratic at the root.** A star's authority sends every source's
   epoch and sync down every other link: 4,444 epochs and 736 syncs a second at 16
   players. Envelopes and encodings are constants on that term; only topology
   changes its order.
2. **Protocol, not simulation, bounds the map.** A td host's rounds cost about
   220 ms of CPU a second (7.2 × (14 + 5 + 12)), its keyframes up to as much again
   before cached sections took their deflate to the stores that moved (§4), its
   simulation 52 ms.
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
  ([services §7.3](services-and-networking.md#73-why-a-byte-stream-and-not-udp-or-icmp)).
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
   whole bodies for a few publications. A relay, which has no whole world to send
   instead, bounds by the transfer ceiling alone.
5. Validate every page hash, reconstruct the authority root, stage the result and
   commit it between ticks. A set is one baseline, applied whole or not at all.
6. Refuse stale, foreign, malformed, or unverifiable repairs. One that fails its
   proof or its install asks its manifest's sender for a whole world. A relay
   cannot author one, so it answers `MsgStateUnserved`, as it does for a tick it
   does not retain, and asks its own source for a whole world in its next answer,
   which reaches the receiver through it.

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
and `Apply` keeps such a body at the head of its inbox until the reset enters it. A
body or a repair leaves in `SnapshotCorrectionChunkBytes` chunks while the link's
backlog, the bytes sent beyond what its latest echo reports received, drains within
`SnapshotBulkQueue` past the round trip, so an epoch waits behind a chunk, not a
world. What a link still delivering one is owed next, whether body, index answer or
repair, waits behind it, the newest replacing the older and nothing partial
replacing a whole world, and leaves once the link drains, released between every
two ticks rather than by the next publication: a late or lost frame costs a round
trip, never a stall. A request naming a tick the authority has let go was overtaken
by the index since and is not answered unless it asks for a whole world, nor is one
naming a tick the peer has since proved it holds, whose answer the receiver would
refuse as outgrown, as it refuses an index or a repair no newer than the world it
last proved rather than comparing it off its tick. Indexes to a peer are spaced by
its round trip, so a descent's request finds its tick retained. A link that cannot
deliver a keyframe within `SnapshotKeyframeDrain` takes none on schedule: it meets
the floor with proofs, takes a whole world on request and reports constrained.
Echoes carry the answering side's clock, and delivery rate is timed on it, since
echoes a stalled path releases together arrive microseconds apart.

### 2.1 The playout buffer

The paths deliver worlds of different ages: a whole body is one one-way delay old,
a selective repair three, the root and then a round trip for the page hashes.
Adopting each capture's tick would step the clock by up to two one-way delays and
every shared actor with it, so a capture behind the clock is projected
(multi-player §3.3) and moves nothing; the buffer is for the other direction.

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

A guest sits a playout behind its relay, so a roster crossing it holds may be
unapplied when a handoff record reaches it; the record is therefore validated
against the players alone, and a seat, which crosses at its holder's word, is
reconciled after the handoff as the roster is (multi-player §5.2).

## 3. Readings

§3.1 counts bytes framed before deflate on the in-process mesh, which does not
deflate, and times stages rather than their parts; §3.3 reads the socket path, §3.5
the harness, §3.7 a td keyframe's bytes by section and layout, the install's lock by
part and the heap by owner, which decided §4's last batch. Still unread: repairs by
cause, the sections carried and whether the authority committed one of the
receiver's crossings late or void since its last proof, where the next divergence
fix would start; and keyframes by reason.

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

```
Players                                 Tower 2     Tower 16    td 2        td 16
Publication rounds a second             4.5         4.6         1.5         7.2
Manifests answered hash-only            94%         94%         78%         95%
Host egress, framed before deflate      5 kB/s      0.23 MB/s   0.07 MB/s   5.05 MB/s
  of it epochs and owner syncs          1 kB/s      0.16 MB/s   2 kB/s      2.82 MB/s
  of it correction bodies and repairs   3 kB/s      0.07 MB/s   70 kB/s     2.18 MB/s
Heap in use per instance                70 MiB      90 MiB      210 MiB     250 MiB
```

The egress rows are the binary wire codec's (protocol 24, and the same at 25),
read over sockets in §3.3's shape, which the in-process figures agreed with; td
16's are phase 1's,
before the codec, since the socket harness cannot seat its roster
([todo](todo.md#measure-a-full-roster)).

Per round the host captures under the world lock (about 1.2 ms tower, 14 ms td),
then seals and indexes the capture; a keyframe is about 26 / 360 kB compressed. The
seal, the index and a keyframe reuse the bytes and hash of every row whose owned
copy still compares equal, and navigation rebuilds its wall grid only when a wall
moved and compares grids only when their generation moved. At 16 players, rounds
four ticks apart (on a slower container than the figures above), the seal costs
1.8 ms on the tower map and 5 ms on td, the index 2.4 and 12 ms, a keyframe 7.6 and
58 ms (mostly deflate), and a simulation tick 1.1 and 2.6 ms. From cached sections
(protocol 26) a keyframe's deflate falls to the stores that moved: on four cores a
td keyframe that one deflate took 72.7 ms encodes in 2.2 while its stores hold and
8.6 a tick later, at the same bytes within 0.4%, and a session's first in 150
against 158 (`BenchmarkKeyframeEncodeTD`, `TDMoving`, `TDCold`). An install that
projects nothing holds the live lock 7–10 / 40–70 ms, and each projected tick adds up
to a simulation tick. Sealing, indexing and encoding run outside the lock, but on
the one thread a browser guest has.

Per simulated second the host costs 20·T_sim + R·(capture + seal + index) plus the
whole bodies it serves, R being the union of its peers' due ticks rather than a fixed
5 Hz; a guest costs 20·T_sim + A·(capture + index) + installs·(stage + projection +
compare and write), A being the manifests it answers. A star's authority relays every
participant's epochs and owner syncs to every other, (N−1)² of each: at 16 players
4,444 epochs (20 B on the tower in the binary codec, 56 with its envelope alone
and 190 before it; td's 510 B is from before it) and 736 syncs (about 95 B, the
cells the mask names set) a second.

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
  is 69% of the tower host's egress at 16 players, the manifests' page hashes most
  of the rest; at td whole bodies come close.
- **The live lock is held by the O(E) capture and comparison** before any projection,
  so shrinking them comes before moving projection out of the lock.

### 3.2 Scenarios

One matrix judges every approach, on harnesses that exist:

- **Map**: tower, td. `TestATowerGuestAnswersHashOnly` (§3.1's shape),
  `TestCostAtTheStormHighWater`, `BenchmarkCaptureAtStormHighWater`.
- **Players**: 2, 4, 16, seated with bots on the same harnesses.
- **Link**: in-process; four-tick links; staged latency, loss and byte budget.
  `network.LinkShape`, `TestStagedLinkShapingKeepsCorrectionsBoundedAndRecovers`.
- **Path**: in-process mesh; TCP with deflate; WebSocket to a browser.
  `TestNetcodeReadingsOverSockets` (§3.1's shape over sockets,
  `VIF_NETCODE_READINGS=1`, a CPU profile per row under `VIF_NETCODE_PROFILE=<dir>`),
  `TestNetcodeReadingsForBatch3` (§3.7, `VIF_NETCODE_BATCH3=1`); a browser joiner.
- **Host**: native at `GOMAXPROCS=1`; a fleet pod at 500m CPU; a WASM guest.
  `BenchmarkHeadlessTicks`; [Measure a full roster](todo.md#measure-a-full-roster).
- **Load**: idle; a move every third tick and a shot every sixth; the storm high
  water. §3.1's profile, the storm fixture.

Metrics, from multi-player §7's telemetry where it exists: CPU-s/s by stage;
world-lock hold, p50 and p99; egress before and after deflate by kind
(`network.sent_<kind>_bytes` framed before each link's deflate, `network.wire_bytes_out`
after it); the hash-only share; repair and keyframe bytes; `snapshot.correction_*`
and `snapshot.projected_ticks`; `network.barrier_delay_ticks` and
`network.commit_late`; heap in use. An approach lands when it moves its target and
leaves the hash-only share, correction magnitude and the protocol tests where they
were.

### 3.3 Readings over sockets

`TestNetcodeReadingsOverSockets` on 2026-10-24 with the binary wire codec
(protocol 24), one process on four cores, every instance paced at the tick: the
host's egress per simulated second, framed before each link's deflate by what the
frames carried, and on the wire after it, in kB/s.

```
Row        Epochs  Syncs  Bodies  Repairs  Control  Framed  Wire   Wire/framed
tower, 2      1.0    0.3       0      3.3      0.4     5.0   1.54   0.30
tower, 4      5.8    2.8       0     10.2      1.3    20.2   6.45   0.32
tower, 16    88.6   69.7       0     65.6      6.7   231    74.7   0.32
td, 2         1.9    0.2    67.3      3.1      0.4    73.1  68.9    0.94
td, 4        14.4    2.2    61.0     10.0      1.3    89.0  68.3    0.77
```

Beside them: 5 rounds a second on every row; every manifest answered hash-only,
but for one of three tower-16 readings, on which a guest that projected 11 ticks
repaired 15 pages and took two keyframes; the whole process 0.08, 0.14 and
0.48–0.50 CPU-s per simulated second on the tower at 2, 4 and 16 players and
0.36 and 0.54 at td with 2 and 4, its heap 40, 84 and 324–471 MiB and 311 and
663 MiB. With the codec's field tables in place of its generated functions,
three tower-16 readings later the same day read 0.54–0.55, which the codec's
own microbenchmarks cannot account for and a quieter machine should settle; with
the authority's FSM transitions (protocol 25) the tower's rows read the same
within the spread (epochs 1.0, 5.9 and 89.1 kB/s framed, the wire 1.56, 6.47 and
73.1) and the process 0.09, 0.17 and 0.59, a transition being one crossing of
some twenty bytes a lead apart; with the word hash and the index read in place
(manifest version 4) the bytes read the same again and the process 0.09, 0.15
and 0.49, a guest tick 12.5 ms against 16.8 at 16 players, td 0.36 at 2; with
keyframes from cached sections (protocol 26) the tower reads the same again
(epochs 1.0, 5.9 and 89.8 kB/s framed, the wire 1.55, 6.44 and 75.2) and the
process 0.08, 0.14 and 0.47, a guest tick 11.9 ms at 16 players, td 0.34 at 2 and
0.46 at 4 with a keyframe every 14 s on both rows, its bodies 45.6 and 38.6 kB/s
framed, its wire 47.3 and 45.0 and its heap 360 and 906 MiB. The in-place answer
never runs here: these guests tick in step with the host, so a
manifest reaches them after the tick it names and the promised tick is captured
as before; the paced guest of §3.5, a lead behind, answers 248 of 261 manifests
in place. td's bodies and CPU move run to run with what its storm does to the
roster (a keyframe every eight seconds on both td rows here, none to half a
second of them on other readings), so its rows compare only where the keyframe
rate agrees, and the tower's wire has a run-to-run spread of about 2%. The framed
figures agree with §3.1's in-process ones. The same day before the codec, the
tower's epochs framed 4.8, 41.0 and 825 kB/s and its syncs 1.6, 15.2 and 382, its
wire 1.70, 7.94 and 101 kB/s at 0.17, 0.12 and 0.08 of the framed bytes, the
process 0.08, 0.15 and 0.58 CPU-s/s, and td's epochs 7.4 and 84.0; with the
envelope alone, epochs 1.7, 15.5 and 247, syncs 1.1, 10.0 and 246, the wire 1.60,
7.09 and 88.0 and the process 0.52 at 16 players; before bodies passed the link
raw, syncs carried their set cells, repairs were chunked, the descent took one
round trip, links were bundled and a leaf's echo lost its frames, the tower's
wire read 2.32, 9.99 and 125 kB/s, its framed bytes 16.0, 91.3 and 1,552.

CPU by stage, the share of the process's sampled time in the measured window,
every instance together, so the runtime's shares are the process's and not one
host's, profiled on that first reading:

```
Stage                                   tower, 4   tower, 16   td, 2   td, 4
Simulation and everything unlisted      40%        39%         28%     35%
Allocation and garbage collection       12%        11%         30%     24%
Scheduler and other runtime             11%        11%         19%     15%
Sockets and system calls                13%        6.5%        3.1%    1.6%
Per-link deflate                        4.7%       12%         0.2%    4.2%
Inflate                                 -          0.4%        0.7%    1.2%
JSON encode and decode                  5.3%       6.9%        4.2%    7.0%
TOML encode and decode                  -          2.6%        -       -
FNV hashing                             9.9%       7.0%        11%     7.7%
Seal, index and keyframe encode         4.1%       3.5%        2.1%    3.4%
Protocol (converge, network)            0.6%       0.2%        1.9%    0.9%
```

What the readings decide:

- **The codec took 79–89% off the tower's framed epochs, 80–82% off its syncs and
  10–26% off its wire**, the envelope and the payload about half each. The per-link
  stream had compressed the JSON and TOML keys against its history already, 7–12×
  over the command stream, so the framed bytes fell five times further than the
  wire, and the stream now takes 3× off what is left, which is the values. The
  codec's worth is CPU and allocation: the process 0.58 to 0.49 CPU-s per
  simulated second at 16 players, the JSON and TOML shares and the deflate that
  scaled with their bytes gone from every receiver, the browser guest's one thread
  included. The manifests' page hashes, 66 kB/s framed and JSON still, are the
  largest framed stream after the epochs now, which converged answers and the
  binary row codec take up. One dictionary deflate for every link in place of the
  per-link stream is refuted: no preset dictionary matches a per-link history that
  compresses this well.
- **At td the bodies are the egress**: whole corrections are 77–86% of the framed
  bytes. The link's level-7 stream took a fifth off the snapshot codec's level-1
  output, which is why a body is compressed once at level 6 and passes the link
  raw: a td keyframe is 360 kB against 398 after both passes and 501 at level 1, the
  codec's pass 33 ms instead of 12 plus 9 a link.
- **FNV was 7–11% and the seal and index 2–4%**, so a faster hash paid: the word
  hash of iteration 4 folds eight bytes for what a byte cost, and the profile
  above is the one taken before it.
- **The command stream's constants are small on the wire.** Bundled links took 4%
  off the wire at four players and 11% at 16, where 16 frame headers a tick a link
  became two; the leaf's echo 14% at two players and 8% at four, 1/N of the epochs
  on a star; syncs of the set cells 36–43% of their framed bytes and 1–3% of the
  wire. Closing the drain's copies into the tick's own frame was measured and
  refused: the copies left a tick body later, the harness's first guests read them
  a tick late, and the tower's hash-only share fell to 99.3%.
- **Readings not yet taken**, and the harness is where they go: keyframes by reason
  (`keyframes` and `keyframe_fallbacks` alone are read), repairs by cause, and the
  WebSocket path to a browser. §3.7 took the keyframe's bytes by section, the
  install's lock by part and the heap by owner.

### 3.4 At the site

A terminal guest of the pod at the site, with an Android client holding a bot and
the pod authoring, 76 s at DEBUG on 2026-10-24, read from its log and journal:

- **The link**: a 107–202 ms round trip with 14–85 ms of jitter, up to 1% loss,
  saturated twice in seven samples; 12 kB/s in and 3 kB/s out on the wire.
- **Pacing**: the barrier delay renegotiated between 4 and 11 ticks, 38 pace steps
  and a playout lead adopted 25 times in the window; 533 of the guest's crossings
  committed late over the session, about two a second, each a misprediction the
  next manifest found.
- **Corrections**: 82% of manifests answered hash-only in the window against 100%
  on the harness; the rest brought 8 repaired pages a second, 5.5 kB/s of shards
  down and 4 kB/s of page hashes up, half the downlink and most of the uplink. The
  `status` section was repaired most often (31 times, 19–22 cells each), then
  `w.combat`, `systems` and `w.positions`; a storm spawn was followed within four
  ticks by repairs of 499 and 558 cells across `meta`, `status`, `w.combat`,
  `w.header`, `w.member`, `w.positions` and `w.protection`, then `w.stormcircle`
  pages, whose cause §3.5 found: the FSM's transition tick.
- **The storm's bandwidth**: the committed copies the guest received carried 25–50 kB
  of crossing payload per five seconds between storms and 134–144 kB during one,
  three quarters of it `CombatAttackAreaCrossingRequest` and
  `CombatAttackDirectRequest` at 290–425 bytes of TOML each, the rest explosions.
  Omitting their zero-valued fields would take 16–18% off those payloads before
  deflate and little after it; the binary codec is what shrinks them. The guest's
  own crossings, 40% as many bytes again, came back to it in full as the echo until
  a leaf's copy lost its frames (§1).

A second terminal guest of the pod the same day, with the codec in place, a
browser and an Android client beside it and a bot on the Android one, read no
issue: the only warnings were a bot refused on a full session, the journal shows a
run reset and the bot seated under its holder, and the lead sat between 8 and 19
ticks on a link of about three ticks' round trip, held up by the worst age in its
300-tick window while the client's terminal lost its window focus, a stall of the
client rather than the link.

### 3.5 The divergence harness

`VIF_DIVERGENCE=1 go test ./internal/app -run TestADivergenceOverAShapedLinkNamesItsRows -v`
fights a host and a guest for 1,200 ticks over an in-process mesh link shaped both
ways (`VIF_DIVERGENCE_SHAPE=latency,jitter,every,loss`, in ticks and frames: the
default `3,3,97,0`, field-like `2,2,3,100`, sparse `3,3,251,0`, heavy `4,3,41,50`),
paces the guest from its real trim and step, raises a storm at one simulation tick
on both ends, and prints what the link cost (late commits, late copies, pages and
cells repaired, keyframes, the lead held and the highest held) with every repair
and the rows it moved (`snapshot.RepairRows`, the debug record `repair rows`). It
is deterministic run to run, so a change is read against the same fight; the fight
is chaotic, so pages repaired move with any change of timing and the late counts
are the robust measure. Pacing's dynamics were also modelled apart, by integrating
`paceDecision` against periodic spikes.

Its first day found and fixed, in phase 1: the lead reading the newest epoch's age
rather than the worst since its last reading, so a spike never taught it (field-like
link: late commits 309 to 1); a spike's reading falling out of a 60-tick window
before a slow link's next spike, now held 300 ticks (`NetworkBarrierRenegotiateTicks`;
rare spikes: late commits 33 to 0); a pace window of ten epochs that forgot a
down-path spike, now 60 (`NetworkPaceWindow`; heavy jitter: late copies 71 to 0),
both costing a jittery link one to three ticks of lead and a steady link nothing;
two per-instance counts inside the compared surface, a player-domain death pending
adaptation and `explosion.triggered`, moved out of it; and the storm-spawn
divergence at the site, which was the FSM's transition tick, each end entering
`StormSetup` on its own `kills.quasar`, closed by batch 2's first item.

The four shapes before and after, 1,200 ticks each (pg pages repaired, kf
keyframes, lc late commits, lcp late copies, lead held at the end with the highest
in brackets; before is the lead's newest-age reading at the 60-tick and ten-epoch
windows, measured before the harness paced):

```
shape      before                                after
fieldlike  pg 298  kf 14  lc 309  lead 5 (6)     pg 10  kf 2  lc 1  lcp 0  lead 8 (9)
rare       pg 16   kf 3   lc 43   lead 7 (7)     pg 3   kf 3  lc 0  lcp 6  lead 11 (13)
sparse     pg 1    kf 2   lc 6    lead 7 (7)     pg 1   kf 1  lc 3  lcp 4  lead 9 (10)
heavy      pg 300  kf 7   lc 57   lead 9 (9)     pg 49  kf 7  lc 0  lcp 0  lead 14 (15)
```

Phase 2's iterations over the same four shapes: on the main each started from
(the staleness change had moved the lossy shapes' pages from the table above),
with the envelope, with the payload codec, with the authority's FSM transitions
(`fsm`), with the index read in place under the word hash and the events a
parked region replays (`place`), and with keyframes from cached sections
(`blobs`), 1,200 ticks each:

```
shape      stage     pg  kf  lc  lcp  lead     manifests
fieldlike  main      31   3   1    0  8 (9)    284
           envelope  18   4   1    0  8 (9)    276
           payload   31   2   1    0  8 (9)    270
           fsm       23   5   1    0  8 (9)    175
           place     56   5   1    0  8 (9)    170
           blobs     34   5   1    0  8 (9)    179
rare       main       3   3   0    6  11 (13)  273
           envelope   8   2   0   11  11 (13)  267
           payload    3   2   0    6  11 (13)  261
           fsm       29   2   0    4  11 (13)  261
           place     24   2   0    4  11 (13)  261
           blobs     24   2   0    4  11 (13)  261
sparse     main       1   1   3    4  9 (10)   292
           envelope   1   1   3    4  9 (10)   287
           payload    1   1   3    4  9 (10)   284
           fsm        6   2   3   10  9 (10)   281
           place      5   2   3    6  9 (10)   279
           blobs      5   2   3    6  9 (10)   279
heavy      main      61   5   0    0  14 (15)  134
           envelope  84   7   0    3  13 (15)  132
           payload   99   5   0    0  14 (15)   97
           fsm      119   4   0    1  14 (15)  124
           place    116   4   0    1  14 (15)  124
           blobs    116   4   0    1  14 (15)  124
```

**A relayed topology.** `TestARelayedTopologyConvergesUnderShape` runs always: a
host, a relay and a person behind it over the default shape, 300 ticks, the storm,
600 more, asserting that both guests answer most of their manifests hash-only, that
the authority sends to its one link alone, that the relay forwards and refuses
nobody it carries, and that the person's crossings reach the authority through the
relay. Its first run found the person stuck: its first repair through the relay was
refused as wider than the keyframe the relay had installed early, when the world was
small, which made every later answer a whole-world request the relay cannot author,
answered `MsgStateUnserved` 44 times of 46 while the authority, told nothing, sent
no keyframe for a link it judged unable to drain one on schedule. A relay now bounds
a repair by the transfer ceiling alone and asks its own source for a whole world it
cannot author (person: hash-only 64.8% → 96.4%, repairs served through the relay
0 → 5, keyframes 2 → 1; the relay 100% on both).

Late commits and the lead hold on every shape through every step; pages and late
copies move where the link is lossy or jittery, since the link meter reads the
bytes it delivered and the correction cadence follows its throughput, so a
command stream of a fraction of the bytes is a different fight from the first
repair on. The manifests the guest received fall with the bytes on every shape,
to 97 from 134 on heavy loss: a thinner stream reads as a slower link to the
meter, which is the cadence's to answer
([todo](todo.md#read-the-cadence-against-a-thin-command-stream)). The `fsm` step
is a different fight again from the storm region's entry on: the region's own
storm now spawns when the raised one dies instead of the region adopting the
raised one by liveness, so the rows after it are not the codec's to compare;
the late counts and the lead are, and hold. The `place` step moves the sparse
fight once more: an event a parked region used to drop now replays, so the
scenario's own storm comes at tick 508 instead of 904 and the harness's raise
finds one active; late counts and lead hold again. The `blobs` step leaves three
shapes as they were and moves the field-like one's pages from 56 to 34 with nine
more manifests, the lossy link reading the bytes it delivered; late counts and
lead hold.

The harness prints the storm region's transitions too, from both ends' records:
the scenario enters `StormSetup` from the quasar kills at tick 798–928 by shape,
retries its refused spawn every 12 ticks while the storm the harness raised lives
(the retry loop the liveness transition used to cut short), takes `StormActive` on
its own spawn and ends through `StormDecision` and `StormVictory` on its death,
every transition at one tick on both ends over every shape, and how many
manifests the guest answered in place, 248 of 261 on the default shape. What it
cannot yet read: the pacing knobs other than their steady state.

### 3.6 Phase 2 in measurements

What each iteration changed, in the unit a player or an operator feels, read
before and after on the same harness the same day (§3.3's sockets readings, §3.5's
four shapes, the package benchmarks), as AGENTS.md asks of a change made for
performance. Tower rows are 2 / 4 / 16 players.

```
Iteration                      Measured                              Before → after
Wire codec (protocol 23, 24)   Tower epochs, framed                  4.8 / 41.0 / 825 → 1.0 / 5.8 / 88.6 kB/s
                               Tower syncs, framed                   1.6 / 15.2 / 382 → 0.3 / 2.8 / 69.7 kB/s
                               Tower wire                            1.70 / 7.94 / 101 → 1.54 / 6.45 / 74.7 kB/s
                               Process at 16 players                 0.58 → 0.49 CPU-s/s
Authority's FSM (protocol 25)  Storm region's transitions, two ends  by each end's own tick → one tick, four shapes
                               Tower wire, process at 16             within the 2% spread; 0.54–0.55 → 0.59 CPU-s/s
Answers in place (manifest 4)  Hash of a 181-byte row                0.25 → 4.6 GB/s
                               Paced guest's manifests, in place     0 → 248 of 261
                               Guest tick at 16 players              16.8 → 12.5 ms
                               Process at 16 players                 0.59 → 0.49 CPU-s/s
Cached sections (protocol 26)  td keyframe encode, stores held       72.7 → 2.2 ms
                               td keyframe encode, a tick apart      61.2 → 8.6 ms
                               td keyframe bytes                     363.2 → 361.9 kB
                               Storm high-water capture              13.3 → 12.9 kB
                               Process at 16, guest tick             0.49 → 0.47 CPU-s/s, 12.5 → 11.9 ms
```

Over the five the tower host's wire at 16 players fell from 101 to 75 kB/s and the
process from 0.58 to 0.47 CPU-s per simulated second, a td keyframe's encode from
one deflate of the world to the stores that moved, and §3.5's late commits and
lead held on every shape at every step. What did not move: td's bodies, which are
its egress, and the heap; §3.7 reads both and §4's last batch is decided on them.

### 3.7 Readings for the last batch

`TestNetcodeReadingsForBatch3` (`VIF_NETCODE_BATCH3=1`, a heap profile under
`VIF_NETCODE_PROFILE=<dir>`) on 2026-10-25: td with two players over sockets, god
mode, 420 ticks at the tick, four cores. The keyframe at tick 85, each store as the
JSON rows the wire carries, as the same cells in fixed binary rows, the entity a
varint and each field in declaration order, and in columns, one per field path with
the entities as deltas in canonical order, deflated at level 6:

```
Store            Rows  JSON plain  deflated  Binary rows  deflated    Columns  deflated
positions      50,853   1,685,130   189,955      319,995   252,349    234,867     6,348
wall           50,565   8,512,486   153,676      997,709   114,366    913,089     5,536
combat            618     298,195     4,331       30,371     2,170     29,137       305
header             11      26,544     2,648        3,124     1,849      3,104     1,298
protection        618      16,713     1,497        2,485     1,393      1,374       121
member            605      23,596     1,318        3,630       929      2,422        63
13 others          44       4,582     1,550          720       723        658       650
all stores             10,567,246   354,975    1,358,034   373,779  1,184,651    14,321
```

Binary rows do not pay: deflate had taken the field names already, and a row's
entropy is its entity id, which deflated JSON spends about three bytes a row on and
varints no fewer. Columns do: entities as deltas are a run of ones, a maze's x
repeats its cycle and its y its runs, and a wall's eight fields repeat their values,
so the keyframe is 14 kB against 355, 96% less; at td-2's 0.07 keyframes a second
that is 25 of its 47 kB/s of wire. The guest decodes the 361 kB body in 141 ms,
`json.Unmarshal` of 10.6 MB, and installs it in 79 ms, stage and commit with no tick
projected; its deepest install held the live lock 48 ms (`snapshot.commit_us`), 11
the staging world's capture (`project_us`) and 36 the live capture, the comparison
and the write (`write_us`). The host's capture peaked at 128 ms and its encode at
73, the session's first keyframe. The heap after a collection, both instances and
the guest's staging world, is 240 MiB: spatial grids 92 MB (37%; 30 MB a world over
three worlds), the retained indexes' per-row arrays and cloned row JSON 47 MB
(`newStoreSection` 33, `ManifestRow` 14), the row encoders' owned cell JSON 24 MB,
the ECS stores 15 MB, navigation 15 MB, and the retained captures themselves about
3 MB each.

## 4. Phase 2

Each approach below says what of it is in, what is left and what it needs before
it can land. The costs name what it lowers (−), raises (+) and leaves (·), in the
order CPU, memory, bandwidth, latency; projections come from §1's figures and §3
confirms or refutes them. Phase 1 closed with the six-tick wire digest retired
(protocol 22): parity is the correction exchange's, and a repair names its sections
in `snapshot.repair_sections`. Phase 2's first four iterations landed the binary
wire codec, its envelope (protocol 23) and its payload (protocol 24, journal
schema 37), the authority's FSM transitions (protocol 25, journal schema 38), the
index read in place under a word hash (manifest version 4, capture schema 24) and
keyframes from cached sections (protocol 26); the last batch's three are decided on
§3.7 and wait in the todo. The approaches are in three batches, and §5 orders them.

### Batch 1: the wire

### A binary wire codec

- Costs: − − − ·; complexity low for the envelope, medium for the payload.
- Measured, framed with the message header: the empty epoch 52 to 19 bytes, four
  moves 575 to 147 to 55, a sync 391 to 249 to 82, six shield hits 2,307 to 1,543
  to 151, the envelope first and the payload after it; the tower's epochs and
  syncs at 16 players are read in §3.1 and §3.3.
- In: the envelope (protocol 23). A `WireFrame` names its event by registry index
  and its domain by value; the batch is a flag byte, source, run, produced tick,
  hops, age and frame count, each frame its event, a flag byte holding the domain,
  its sequence and apply tick as varint deltas from the frame before (the first
  apply tick from the produced tick) and its payload behind a length. The payload
  (protocol 24, journal schema 37): `gen-manifest` type-checks the event package
  and writes `payload_gen.go`, a field table for every registered payload struct
  and every struct reachable from one, each field's offset, kind and width, which
  one walk encodes and decodes through (`codec.go`): a presence mask, a bit a
  field, then each set field in declaration order, signed integers as zigzag
  varints, unsigned as varints, float64 as its bits, strings and slices behind a
  length, a fixed array as its fields (`DamageImmunitySpent` is the component's
  type again), a nested struct inline, a pointer by its bit. A table a struct
  rather than a function a struct took 554 kB off the browser build and 238 kB off
  the native one, and the path reads no type information: the registry's
  prototypes are generated constructors. A decode costs what the generated
  functions did, 0.24–0.44 µs with one allocation per slice; an encode 0.15–0.25
  µs against their 0.09–0.13, a switch a field, at some twenty encodes a second
  per instance. The journal writes the same bytes, base64 in its record,
  and `vif-log` decodes them with the same codec and shows the prototype as its
  TOML, so the two cannot drift; the TOML tags stay for that, for authored scripts
  and for `:emit`.
- Left: the correction exchange's envelope: manifests, requests and shard sets
  carry the header's per-tick fields and leave the session identity the handshake
  checked.
- Why: it removes reflection-driven JSON and TOML from every crossing at every
  receiver, allocation and garbage a WASM guest pays for on its one thread, and
  shrinks the replay suffix (`SnapshotReplayBytes`) and the barrier schedule.
- Decided (2026-10-24): both steps land, on the codec simulation over the site's
  storm traffic, where the attack crossings' TOML deflates 16× and the binary 3.6×
  and the binary halves the framed bytes. Fixed arrays encode as fixed fields,
  which closes the TOML encoder's zero-array omission (`DamageImmunitySpent`
  returns to the component's type). A batch event for the attack crossings is
  subsumed: a hit's cost is its envelope, and hits leave seven sites for one
  handler, so a batch would be a second mechanism for what the envelope removes.
- Needs: nothing further; the codec's layout is the protocol's and the journal
  schema's, and class, stamped-domain and `Derived` rules stay in the registry.

### Owner syncs that carry what changed

- Costs: − · − ·; complexity low.
- Projected: 736 syncs a second at 16 players, 0.56 MB/s before deflate at their
  old size, to about a tenth with a presence mask and less again while cursors idle.
- In: a sync carries the cells that are set, named by the codec's presence mask,
  a bit a cell, so an unset cell costs nothing: a fighting cursor's sync is 82
  bytes framed with the header, 391 before the codec.
- Left: a sync sent only when a cell differs from the last one sent, with a
  refresh every K syncs, so a frame a full queue refused costs bounded staleness.
  A sync stays a whole state, never a delta on an unacknowledged base. Cells every
  receiver re-derives (the shield's inverse squares from its radii) leave the
  payload.
- Needs: determinism and latency are unchanged, a sync still committed at one
  tick for every instance and mirrors already lagging by `NetworkSyncTicks`.

### Batch 2: one tick for every instance

### Shared FSM transitions committed by the authority

- Costs: · · · +; complexity medium. Measured: a playout lead on every transition
  a region takes, nothing on a tick, and a crossing of some twenty bytes each.
- In (protocol 25, journal schema 38): in a live session the authority's machine
  alone evaluates a region's guards, and a transition a guard passes is a crossing
  (`EventFSMTransition`: the region, the state owning the transition, its index
  there, its trigger and the variables it captured) the authority commits as any
  other, applied at one tick on every instance, the authority's included, where
  the transition's actions and the target's on_enter run, so a storm spawns from
  the same draws everywhere. A guest's machine evaluates nothing and follows
  (`Machine.Steer`, `Machine.Apply`); so do a replay of a live session and a
  projection, from their records. A region with a transition in flight takes
  nothing until it lands and parks the events the state it is entering reacts
  to, replaying them there, so a quasar killed while its region's gold cycle
  crossed still ends the region; an internal transition parks nothing, so every
  event it counts crosses, in order. A reset's own settle, one artifact every instance
  applies at one tick, takes its transitions in place, as a solo run does. Guards
  that read per-instance status (`kills.*`, `TimeInState`) are the authority's by
  construction (D-20). The `fsm` section stays as the proof, `StormSetup`'s
  liveness transition is gone, and §3.5's harness reads the storm region's
  transitions at one tick on both ends over every shape.
- Left: a chain of states that each hand on by an unguarded `Tick` pays a lead a
  hop, the tower's setup ten of them
  ([todo](todo.md#fold-an-unguarded-chain-into-one-crossing)).
- Needs: nothing further; the site's storm-spawn keyframes (§3.4) are the reading
  that judges it.

### Converged answers without a capture

- Costs: − · · −; complexity medium. Measured: §3.3.
- In: a guest holding a manifest as the tick it names closes indexes the live
  stores in place under the tick's lock (`Encoder.BuildLive`, `RowEncoder.EncodeLive`):
  each shared cell is compared with the encoder's owned copy where it stands, only
  a cell that moved is encoded and hashed again, and the sections and root come
  from the same reuse a capture's index has. The capture's other parts (the
  allocator's counters, the streams, the carriers, the status surface, the
  machine's position) are read beside it; the store rows are not copied. When the
  root agrees the answer needs nothing more (`snapshot.manifests_in_place`); when
  it differs, or a join waits on this instance for a world it proves, the whole
  capture is taken at that same close and its index reuses the rows just encoded.
  A manifest that arrives after its tick, or one promised by `Next` whose index has
  not arrived, takes the captured path as before, counted off tick when neither
  reading was taken. One test holds the in-place index to a capture's, root,
  sections and pages, normalisation and the owner-authored exclusions included.
- Left: nothing; the encoder still compares values it owns.
- Needs: nothing further.

### A faster deterministic hash

- Costs: − · · ·; complexity low. Measured: 4.6 GB/s against FNV-1a's 0.25 on a
  181-byte row (`BenchmarkHash64Row`), eighteen times.
- In: `vmath.Hash64`, a word-at-a-time hash of the xxHash64 round and avalanche,
  one multiply, rotate and multiply per eight bytes, fixed across platforms and
  processes, which `hash/maphash` is not; strings fold without a copy, a tail as one
  word and the length after it. Every snapshot hash (row, page, section, root,
  integrity, the journal digest) is its sum; manifest version 4 and capture schema
  24.
- Left: nothing.
- Needs: nothing further.

### Batch 3: formats and structure

### Keyframes from cached sections

- Costs: − + · ·; complexity medium.
- Projected: keyframe deflate in proportion to the sections that moved.
- In (protocol 26): a capture travels as sections, codec 2 of the `VIFS` envelope:
  a count, then each part's name, plain and deflated lengths and its own deflate,
  each bounded at the receiver by the plain size it declares. A store of
  `SnapshotSectionBlobBytes` (16 KiB) of JSON or more is a part of its own, its
  deflate kept by the encoder while its rows are the slice the row encoder
  returned before; the header, the carriers, the status, the FSM and the smaller
  stores are one `rest` object deflated each time, since alone each starts deflate
  cold and costs a quarter of its bytes (a two-player tower keyframe 3,623 bytes
  against 2,907 as one deflate; as `rest`, 2,935). A keyframe and a join's capture
  are one body (`EncodeCapture`, `DecodeCapture`); integrity stays over rows.
- Measured, four cores: a td keyframe 72.7 ms as one deflate, 2.2 while its stores
  hold and 8.6 a tick later (61.2 before), a session's first 158 to 150; bytes
  363.2 to 361.9 kB at td and 13.3 to 12.9 at the storm high water
  (`BenchmarkKeyframeEncodeTD*`, `BenchmarkCaptureAtStormHighWater`). The cached
  blobs are each large store's deflate, the keyframe's bytes once over.

### Less work under the live lock

- Costs: · − · −; complexity high.
- Projected: a deep install's lock from past 100 ms to about a tick at td.
- In: an answer captures nothing under the lock, and neither does an install: the
  commit compares the staging world's stores against the live ones in place and
  writes the cells that differ (`World.ReconcileSharedFrom`, in the order a capture
  is written, so the two leave one world), everything beside the rows read light; a
  journal takes the delta that comparison recorded and the written rows' integrity
  hashed where they stand. Measured on §3.7's run: the deepest td install holds the
  lock 8.2 ms (write 7.7, project 0.03) against 47.2 (write 34.5, project 12.0), no
  tick projected on either; the keyframe's install, stage and commit, 52 ms against 62.
- Measured before it (§3.7): the deepest td install held the lock 48 ms with no tick
  projected, 11 of them the staging world's capture and 36 the live capture, the
  comparison and the write; a projected tick adds a simulation tick, 2.6 ms at td.
- Left: the projection, which the lock still holds, a simulation tick per tick behind,
  as [Project a correction outside the live lock](todo.md#project-a-correction-outside-the-live-lock)
  sets out, under everything multi-player §3.3 requires.

### A columnar keyframe codec

- Costs: − − − ·; complexity high.
- Projected: rows 3–10× smaller before deflate.
- Measured (§3.7): binary rows are 7.8× smaller plain and 5% larger deflated than
  the JSON rows, a row's entropy being its entity id; the same cells in columns
  deflate to 14 kB against 355, and the guest's 141 ms decode of 10.6 MB of JSON
  goes with them.
- Decided: the codec is columnar, not row-wise. A keyframe lays each section out by
  column, entities as varint deltas in canonical order and then each field of the
  component in declaration order, integers as varints, float64 as its bits, strings
  behind a length, generated per component in `internal/gen-manifest` beside the
  stores; a delta correction carries its changed rows the same way, and `vif-log`
  renders a body as rows. The manifest's partition and the seal keep their shape
  over each row's canonical encoding; `Schema` and the protocol bump, the manifest
  version with the cell encoding if it changes. Carriers, status and the FSM stay
  JSON, being few rows:
  [Lay keyframe sections out in columns](todo.md#lay-keyframe-sections-out-in-columns).
- Needs: the cursor's `Control` normalisation and the owner-authored exclusions
  move with every store; retention by reference below builds on it.

### Retention by reference

- Costs: + − · ·; complexity medium.
- Projected: retention from about four worlds to about one.
- Measured (§3.7): the retained captures are about 3 MB each at td; what retention
  holds is its indexes, 47 MB of per-row arrays and cloned row JSON over four
  captures, beside the row encoders' own 24 MB of cells, and the largest owner is
  the spatial grid, 30 MB a world over three worlds.
- Decided: a retained index shares the pages whose rows held with the index before
  it and references the encoder's immutable cell bytes rather than cloning them,
  about a fifth of a td participant's heap; the grid is
  [its own item](todo.md#shrink-the-spatial-grid-on-ceiling-sized-maps):
  [Hold retained indexes by reference](todo.md#hold-retained-indexes-by-reference).
- Needs: the columnar codec first where the cell bytes change with it.

### Dropped

- A dictionary deflate for every link in place of the per-link stream: refuted by
  §3.3, no preset dictionary matches a per-link history that compresses 7–12×.
- Relay trees chosen by the authority: the star's wire at 16 players on the tower is
  108 kB/s after deflate, which a home uplink carries, and a fleet pod runs out of
  CPU first; a relayed participant's crossings and corrections take one more hop
  and its lead rises by it. A relay stays what a person chooses (`-relay`), reached
  through the chain's doors and the router's tunnel ([Multiplayer
  §5.6](multi-player.md#56-relay-doors-and-the-router)): correctness, not topology.
- A wire digest beside the manifest exchange: retired in phase 1's last iteration,
  3.6–4.1% of a guest's CPU for a category the repair names exactly.
- Binary rows without columns: refuted by §3.7, 5% larger deflated than the JSON
  rows, whose entropy is the entity id either way.
- `encoding/json/v2` in place of v1, which Go 1.27 already runs on the v2 engine
  under v1 semantics: measured on the td world, unmarshal 140 to 109 ms, marshal
  64 to 66 ms, the cells' marshal 42 to 45 ms. The one gain is the keyframe decode
  the columnar codec removes, against 230 call sites, 225 `omitempty` tags whose
  meaning v2 changes, durations and nil slices encoded differently and a version
  bump for each.

## 5. Order and iterations

Phase 2 is landed: five iterations in this order, each one task, branch and PR,
re-running §3.1's matrix and §3.5's four shapes, measured in §3.6.

1. The codec's envelope (batch 1): protocol 23 (§4).
2. The codec's payload (batch 1): protocol 24 and journal schema 37, the syncs'
   presence mask with it (§4).
3. Shared FSM transitions committed by the authority (batch 2): protocol 25 and
   journal schema 38, the harness reading the storm region's transitions (§3.5, §4).
4. Converged answers without a capture and the faster hash (batch 2): manifest
   version 4 and capture schema 24 (§3.3, §4).
5. Keyframes from cached sections (batch 3): protocol 26 (§3.1, §3.6, §4).

What follows is decided on §3.7 and waits in the todo, in this order: the
[columnar keyframe codec](todo.md#lay-keyframe-sections-out-in-columns), the
[install that compares in place](todo.md#project-a-correction-outside-the-live-lock)
and [retained indexes by reference](todo.md#hold-retained-indexes-by-reference).
Each lands alone, with its measurement before and after in §3.
