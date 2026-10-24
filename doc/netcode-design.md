# Netcode design

The netcode as built, the readings that judge it, and what is still to change. vif
is not lockstep: every instance runs the whole Shared simulation at 20 ticks a
second, a guest predicting it and the authority correcting it (multi-player §10.3),
and the wire carries what a receiver cannot re-derive. §1 is the wire, §2 the
correction pipeline, §3 the readings, §4 phase 2, the approaches not yet landed in
their batches, each with what of it is in and what it still needs, and §5 their
order by iteration. Phase 1, four iterations in 2026-10, is what §1–§3 describe as
built. Bare section numbers
are this document's; "multi-player §n" is [Multiplayer](multi-player.md), which
holds the session contract, event timing, membership and succession; "services §n"
is [Services and networking](services-and-networking.md), which holds the
transport, its handshake and its messages; D-n is the [domain model](domain-design.md).
Figures are §3's unless a line names another source.

## 1. The wire

Four streams carry what a receiver cannot re-derive:

- **Epoch** (`MsgEvent`): one source's closed tick of D-3 crossings, empty ones
  included. A JSON `WireBatch` whose frames carry the journal's TOML text. Every
  tick per source: raw up the one link toward the authority, the committed copy
  down every link, the producer's own included.
- **Owner sync** (`MsgStateSync`): one cursor's D-13 cells, the ones that are set.
  The epoch's envelope over a TOML payload. Every `NetworkSyncTicks` (6) per
  cursor, committed and relayed as an epoch is.
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
(`MessageType.Precompressed`), since a body compressed once at the level the wire
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
section, rows bucketed by a hash of their identity; rows, hashed with byte-serial
FNV-1a over each row's JSON, where row reuse keeps the JSON and hash of each row
whose owned copy still compares equal. Equal roots send no state. The root travels
with its partition, the page count of each section paged more than once, so a
receiver whose root differs answers at once with the page hashes of every section
under it: one round trip from the root to the repair.

Per simulated second a host costs 20·T_sim + R·(capture + seal + index) plus the
bodies it serves, R being its publication rounds, and a guest 20·T_sim +
A·(capture + index) plus its installs, A being the manifests it answers. At 16
players (§3.1, which times seal, index, keyframe and tick on a slower container):

```
                                        Tower 239x64        td 500x250
Host egress before deflate              1.64 MB/s           5.05 MB/s
  of it epochs and owner syncs          85%                 56%
  of it bodies and repairs              10%                 43%
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
   whole bodies for a few publications. A relay, which measures no keyframe, bounds
   by the transfer ceiling ([todo](todo.md#bound-a-relayed-repair-by-a-keyframe)).
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
deflate, and times stages rather than their parts; §3.3 reads the socket path. Each
reading still missing decides an approach of §4:

- Keyframes by reason, schedule, floor, request, repair wider than the keyframe,
  failed proof, the bodies after a fallback, and how many are encoded for how many
  sent: keyframes from cached sections.
- Plain bytes per section of a td keyframe: cached sections, the binary row codec.
- Lock hold by part, stores, carriers, status, comparison, write, each projected
  tick: converged answers without a capture, less work under the live lock.
- Repairs by cause, the sections carried and whether the authority committed one of
  the receiver's crossings late or void since its last proof: the next divergence
  fix.
- Heap by owner, retention ring, staging world, encoder caches: the binary row
  codec, retention by reference.

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
Host egress, framed before deflate      20 kB/s     1.64 MB/s   0.28 MB/s   5.05 MB/s
  of it epochs and owner syncs          5 kB/s      1.39 MB/s   11 kB/s     2.82 MB/s
  of it correction bodies and repairs   11 kB/s     0.17 MB/s   0.27 MB/s   2.18 MB/s
Heap in use per instance                70 MiB      90 MiB      210 MiB     250 MiB
```

Per round the host captures under the world lock (about 1.2 ms tower, 14 ms td),
then seals and indexes the capture; a keyframe is about 26 / 360 kB compressed. The
seal, the index and a keyframe reuse the bytes and hash of every row whose owned
copy still compares equal, and navigation rebuilds its wall grid only when a wall
moved and compares grids only when their generation moved. At 16 players, rounds
four ticks apart (on a slower container than the figures above), the seal costs
1.8 ms on the tower map and 5 ms on td, the index 2.4 and 12 ms, a keyframe 7.6 and
58 ms (mostly deflate), and a simulation tick 1.1 and 2.6 ms. An install that
projects nothing holds the live lock 7–10 / 40–70 ms, and each projected tick adds up
to a simulation tick. Sealing, indexing and encoding run outside the lock, but on
the one thread a browser guest has.

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

- **Map**: tower, td. `TestATowerGuestAnswersHashOnly` (§3.1's shape),
  `TestCostAtTheStormHighWater`, `BenchmarkCaptureAtStormHighWater`.
- **Players**: 2, 4, 16, seated with bots on the same harnesses.
- **Link**: in-process; four-tick links; staged latency, loss and byte budget.
  `network.LinkShape`, `TestStagedLinkShapingKeepsCorrectionsBoundedAndRecovers`.
- **Path**: in-process mesh; TCP with deflate; WebSocket to a browser.
  `TestNetcodeReadingsOverSockets` (§3.1's shape over sockets,
  `VIF_NETCODE_READINGS=1`, a CPU profile per row under `VIF_NETCODE_PROFILE=<dir>`);
  a browser joiner.
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

`TestNetcodeReadingsOverSockets` on 2026-10-24, one process on four cores, every
instance paced at the tick: the host's egress per simulated second, framed before
each link's deflate by what the frames carried, and on the wire after it, in kB/s.

```
Row        Epochs  Syncs  Digests  Bodies  Repairs  Control  Framed  Wire   Wire/framed
tower, 2      4.8    1.6      0.8       0      3.3      0.4    10.9   2.11   0.19
tower, 4     40.7   15.1      2.4       0     10.2      1.3    69.8   9.18   0.13
tower, 16   836    390       12.2       0     65.7      6.7  1311   108      0.08
td, 2         8.3    1.3      0.8    68.1      4.5      0.4    83.3  71.5    0.86
td, 4        78.1   12.3      2.3    58.2     10.3      1.3   162    68.9    0.42
```

Beside them: 5 rounds a second on every row; every tower manifest answered
hash-only, 95.9% and 98.2% of td's at two and four players; the whole process 0.10,
0.17 and 0.66–0.73 CPU-s per simulated second on the tower at 2, 4 and 16 players
and 0.47 and 0.70 at td with 2 and 4, its heap 35, 76 and 354 MiB and 301 and 909 MiB.
td's bodies and CPU move run to run with what its storm does to the roster (none
to half a keyframe a second at four players), so its rows compare only where the
keyframe rate agrees, and the tower's wire has a run-to-run spread of about 2%. The
framed figures agree with §3.1's in-process ones. Against the first reading of the
day, before bodies passed the link raw, syncs carried their set cells, repairs were
chunked, the descent took one round trip, links were bundled and a leaf's echo lost
its frames, the tower's wire read 2.32, 9.99 and 125 kB/s, its framed bytes 16.0,
91.3 and 1,552.

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

- **The per-link stream takes 86–92% off the command stream.** Epoch and sync
  envelopes and TOML payloads compress 7–12× against their own history, better as
  players grow, so the binary codec's worth is CPU, JSON and TOML at 7–10%, the
  deflate that scales with their framed bytes at 12% with 16 players, and the
  allocation behind the collector's 11–30%, and the browser guest's one thread. One
  dictionary deflate for every link in place of the per-link stream is refuted: no
  preset dictionary matches a per-link history that compresses this well.
- **At td the bodies are the egress**: whole corrections are 77–86% of the framed
  bytes. The link's level-7 stream took a fifth off the snapshot codec's level-1
  output, which is why a body is compressed once at level 6 and passes the link
  raw: a td keyframe is 360 kB against 398 after both passes and 501 at level 1, the
  codec's pass 33 ms instead of 12 plus 9 a link.
- **FNV is 7–11% and the seal and index 2–4%**, so a faster hash pays, before or
  after the in-place root; the digest's hash is part of that share.
- **The command stream's constants are small on the wire.** Bundled links took 4%
  off the wire at four players and 11% at 16, where 16 frame headers a tick a link
  became two; the leaf's echo 14% at two players and 8% at four, 1/N of the epochs
  on a star; syncs of the set cells 36–43% of their framed bytes and 1–3% of the
  wire. Closing the drain's copies into the tick's own frame was measured and
  refused: the copies left a tick body later, the harness's first guests read them
  a tick late, and the tower's hash-only share fell to 99.3%.
- **Readings not yet taken**, and the harness is where they go: keyframes by reason
  (`keyframes` and `keyframe_fallbacks` alone are read), plain bytes per section,
  lock hold by part, repairs by cause, heap by owner, and the WebSocket path to a
  browser.

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

What it found on 2026-10-24, each fixed where it says:

- The lead never learned a spike. A spike on a stream holds a burst of epochs that
  return committed in one tick, and `noteOwnCommit` kept the newest epoch's age,
  so `ownLead` read the steady age every tick. It reads the worst age since its
  last reading now. Symmetric field-like link: late commits 309 to 1, keyframes 14
  to 6, hash-only 52% to 92%.
- A spike's reading fell out of a 60-tick window before a slow link's next spike
  came, so every spike beyond a second cost a correction; a reading holds 300
  ticks (`NetworkBarrierRenegotiateTicks`). Rare spikes: late commits 33 to 0,
  pages repaired 32 to 3.
- The pace window of ten epochs forgot a down-path spike before the next; it reads
  60 (`NetworkPaceWindow`; heavy jitter: late copies 71 to 0, pages repaired 215 to
  49). Both settings cost a jittery link one to three ticks of lead and a steady
  link nothing.
- The recurring eight-cell `systems` repair at the site was adaptation and genetic
  pending every species death: a player-domain drain's death pends on one instance
  alone. Only a tracked death pends.
- A 22-cell `status` repair was `explosion.triggered`, a shared system counting a
  player-domain death's burst where it happened; `explosion.*` left the compared
  surface. `nav.recomputes` and `swarm.physics_steps` differ too, only when the
  state they derive from does, and stay: they are the cheap detector a navigation
  test relies on, and their page repair rides the state's.
- A storm raised at one simulation tick on both ends never diverges, over any
  shape. The storm-spawn divergence at the site is the FSM's transition tick:
  `StormSetup` is entered from `QuasarEscalate` on `kills.quasar`, a per-instance
  counter (`kills.*` is outside the compared surface and re-syncs only through a
  `meta` repair), and a guest's FSM ticks on predicted kills too. Two instances
  enter the state on different ticks, spawn from different draws, and a keyframe
  follows. The fix is structural: §4's first item of batch 2.

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

What the harness cannot yet read: a Shared FSM transition (the embedded scenario
has none in its 1,200 ticks), and the pacing knobs other than their steady state.

## 4. Phase 2

Each approach below says what of it is in, what is left and what it needs before
it can land. The costs name what it lowers (−), raises (+) and leaves (·), in the
order CPU, memory, bandwidth, latency; projections come from §1's figures and §3
confirms or refutes them. Phase 1 closed with the six-tick wire digest retired
(protocol 22): parity is the correction exchange's, and a repair names its sections
in `snapshot.repair_sections`. The approaches are in three batches, and §5 orders
them by iteration.

### Batch 1: the wire

### A binary wire codec

- Costs: − − − ·; complexity low for the envelope, medium for the payload.
- Projected: a four-move epoch from about 567 bytes to about 50 before deflate; less
  after, since the stream already compresses repeated keys against its history.
- In: nothing. A `WireFrame` names its event and domain by string
  (`"EventCursorMoveRequest"`, `"shared"`) and carries the journal's TOML text
  inside a JSON batch.
- Left, in two steps:
  1. **Envelope.** The event as its registry index, the domain in a flag byte,
     sequence and apply tick as varint deltas from the epoch's produced tick and the
     frame before. Payloads stay the journal's TOML, so one payload encoder still
     serves replay and transport. Manifests, requests and shard sets carry the
     header's per-tick fields and leave the session identity the handshake checked.
  2. **Payload.** A codec generated beside `registry_gen.go` for every payload
     struct, fixed field order, varints, float64 as its bits, which the journal
     uses too, with a name table in its anchor so it reads across builds and
     `vif-log` rendering the text it shows today. A binary wire beside a TOML
     journal would be two encoders for one payload.
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
- Needs: step 2 makes the journal binary and bumps its schema; slices
  (`WeaponCharges`) take a length prefix; class, stamped-domain and `Derived` rules
  stay in the registry; the codec's version joins the protocol the handshake
  compares.

### Owner syncs that carry what changed

- Costs: − · − ·; complexity low.
- Projected: 736 syncs a second at 16 players, 0.56 MB/s before deflate at their
  old size, to about a tenth with a presence mask and less again while cursors idle.
- In: a sync carries the cells that are set (`omitempty` on `CursorStatePayload`,
  the immunity array a slice so an unspent one is omitted): 36–43% off its framed
  bytes, 1–3% off the wire.
- Left: with the envelope a presence mask; then a sync sent only when a cell
  differs from the last one sent, with a refresh every K syncs, so a frame a full
  queue refused costs bounded staleness. A sync stays a whole state, never a delta
  on an unacknowledged base. Cells every receiver re-derives (the shield's inverse
  squares from its radii) leave the payload.
- Needs: the envelope first; determinism and latency are unchanged, a sync still
  committed at one tick for every instance and mirrors already lagging by
  `NetworkSyncTicks`.

### Batch 2: one tick for every instance

### Shared FSM transitions committed by the authority

- Costs: · · · +; complexity medium. The lead's latency on a transition, nothing on
  a tick.
- Projected: the storm-spawn keyframe (§3.4, 499 and 558 cells) and the repairs that
  follow a region transition gone; the `StormSetup` liveness workaround with them.
- In: the FSM position is compared and repaired (`fsm` section), and an installed
  state runs no on_enter, so a region whose on_enter spawns carries a liveness
  transition for a receiver corrected into it.
- Left: the authority's FSM alone evaluates the guards of a Shared region; a
  transition it takes is a crossing (`EventFSMTransition`: region, target)
  committed and applied at one tick on every instance, which runs on_enter there,
  so a storm spawns from the same draws everywhere. A guest's FSM stops evaluating
  Shared guards and follows. The `fsm` section stays as the proof.
- Needs: a decision on guards that read per-instance status (`StatusIntCompare` on
  `kills.*`, `StateTimeExceeds` on the tick-derived `TimeInState`), which become
  the authority's by construction; the journal records the crossing as any other;
  the harness runs over a transition once a scenario the embedded one spawns has
  one within its ticks.

### Converged answers without a capture

- Costs: − · · −; complexity medium.
- Projected: a guest's capture on 95% of its answers.
- In: nothing. A guest answers each manifest with a capture and an index, as the
  host builds them, although 94–95% of answers at 16 players are hash-only and need
  only a root.
- Left: keep an owned copy and row hash for each live row, as the encoder does for a
  capture; at the close of the tick the manifest names (`Corrections.TickClosed`,
  under the tick's lock) walk the live stores, compare each row with its copy,
  rehash only the rows that differ and compute the root in place; capture only
  when the root differs, at that same close. A paced guest holds the manifest
  before it runs the tick (§2.1), so the root is in hand as the tick closes; a
  manifest that arrives later takes today's path.
- Needs: the in-place root must equal what a capture and index produce,
  normalisation and owner-authored exclusions included, held by one test. The cache
  compares values it owns, so it is not the rejected write counter.

### A faster deterministic hash

- Costs: − · · ·; complexity low.
- Projected: hashing about ten times faster, at the 7–11% share §3.3 measured.
- In: nothing. FNV-1a is byte-serial, one dependent 64-bit multiply per byte, a
  few hundred megabytes a second.
- Left: a word-at-a-time hash of the xxh64 class, written in `pkg/vmath` beside the
  FNV constants, fixed across platforms and processes, which rules out
  `hash/maphash`; it bumps `ManifestVersion` and `Schema`.
- Needs: nothing but the bump. Row reuse already limits hashing to rows that
  changed, and §3.3 found the share real.

### Batch 3: formats and structure

### Keyframes from cached sections

- Costs: − + + ·; complexity medium.
- Projected: keyframe deflate in proportion to the sections that moved.
- In: row reuse makes a keyframe's JSON cheap.
- Left: its deflate still runs over the whole capture, 58 ms at td. Compress each
  section on its own, keep its compressed bytes while its section hash holds, and
  send a keyframe as a container of section blobs. td is a sprawling maze in which
  every wall cell is a Shared entity with a position and a wall row, so most of a
  keyframe would be blobs already compressed. A join's capture (`EncodeCapture`)
  takes the same container.
- Needs: a container format and its decoder; the cached blobs, hundreds of
  kilobytes at td; a few percent more bandwidth, each section losing the history of
  those before. Integrity is over rows rather than body bytes, so the proof is
  unchanged.

### Less work under the live lock

- Costs: · − · −; complexity high.
- Projected: a deep install's lock from past 100 ms to about a tick at td.
- In: nothing beyond row reuse.
- Left, in the order
  [Project a correction outside the live lock](todo.md#project-a-correction-outside-the-live-lock)
  sets: first the capture becomes a copy, stores and carriers read into reusable
  buffers under the lock and canonical order, encoding and hashing after it; then
  the projection leaves the lock, simulating the staging world unlocked to the tick
  the live world had reached, taking the lock, simulating the ticks it ran meanwhile
  with the crossings it applied, comparing and writing. The locked tail is the ticks
  that passed during the unlocked part, and the write.
- Needs: everything multi-player §3.3 requires, fence membership, pending barrier
  crossings not fed, the ledger settled before projection, the journal's place, no
  live tick between the final projection and the write. A WASM guest gains
  scheduling, not parallel CPU.

### A binary row codec

- Costs: − − − ·; complexity high.
- Projected: rows 3–10× smaller before deflate.
- In: nothing. Rows are JSON: an entity id, field names, and floats in shortest
  form; a kinetic row's six float64s take up to about 170 bytes where 48 suffice.
- Left: a binary encoding per component generated in `internal/gen-manifest`
  beside the stores, fixed field order, varints, float64 as its bits, which is what
  is sealed, hashed and sent. A keyframe lays each section out by column, entities
  as varint deltas in canonical order and each field delta-coded, which shrinks
  static, spatially coherent sections such as walls further than deflate alone.
- Needs: every store, the cursor's `Control` normalisation and the owner-authored
  exclusions move to the codec; carriers, status and the FSM can stay JSON at first,
  being few rows; a capture needs a readable rendering for diagnosis (`vif-log`).
  Retention by reference and the column layout build on it, and the row cache of
  the converged answer is cheaper on its rows.

### Retention by reference

- Costs: + − · ·; complexity medium to high.
- Projected: retention from about four worlds to about one.
- In: nothing. The authority retains `SnapshotManifestRetention` (4) captures with
  their indexes and every guest what it proved, each a whole set of rows although
  consecutive ones share most of them.
- Left: hold retained rows as immutable page blocks that consecutive captures
  share, so the ring costs one world and what changed, or hold retained pages
  encoded and decode those a request names. A td participant is about 300 MB,
  mostly captures and the staging world; this takes the retention share.
- Needs: the binary row codec first, which makes the pages small; CPU rises only on
  serving a page, which is rare.

### Dropped

- A dictionary deflate for every link in place of the per-link stream: refuted by
  §3.3, no preset dictionary matches a per-link history that compresses 7–12×.
- Relay trees chosen by the authority: the star's wire at 16 players on the tower is
  108 kB/s after deflate, which a home uplink carries, and a fleet pod runs out of
  CPU first; a relayed participant's crossings and corrections take one more hop
  and its lead rises by it. A relay stays what a person chooses (`-relay`), and the
  relay's own open items stay in the todo
  ([Reach a relay](todo.md#reach-a-relay-without-an-address-in-hand) and the two
  after it): they are correctness, not topology.
- A wire digest beside the manifest exchange: retired in phase 1's last iteration,
  3.6–4.1% of a guest's CPU for a category the repair names exactly.

## 5. Order and iterations

Phase 2 is five iterations, one task, branch and PR each, in this order. Each lands
alone, re-runs §3.1's matrix and §3.5's four shapes, and updates §3.

1. The codec's envelope (batch 1): local to `internal/event`,
   `internal/system/network.go` and `internal/network`; protocol 23.
2. The codec's payload (batch 1): the generated payload codec, the journal's binary
   schema with its name table, `vif-log` rendering it, fixed arrays as fixed
   fields; then owner syncs with a presence mask on the envelope.
3. Shared FSM transitions committed by the authority (batch 2), with a scenario the
   harness can run over a transition.
4. Converged answers without a capture and the faster hash (batch 2): CPU and the
   lock at the map limit, on the host and the browser guest.
5. Keyframes from cached sections (batch 3); the binary row codec, retention by
   reference and the projection out of the lock follow it as iterations of their
   own when their batch is reached.

### Brief for the next session

Repository `lixenwraith/vif`, AGENTS.md is the contract. Netcode phase 2, iteration
1 of 5 (§5): the binary wire codec's envelope. Read [netcode design](netcode-design.md)
§1, §4 batch 1 and §5, then `internal/event/wire.go`, `internal/system/network.go`
(`scheduleCrossings`, `forwardDown`, `sendOwn`) and `internal/network/protocol.go`.
Land the envelope alone: event as registry index, domain as a flag byte, sequence
and apply tick as varint deltas, payloads still TOML; bump the protocol; one test
per rule; re-run §3.1's matrix and §3.5's harness over its four shapes and update
§3.1 and §3.5. Issues to close on the way: none open against the envelope; the
payload step (iteration 2) folds the TOML zero-array omission. Do not start the
payload, the FSM crossing or the converged answer in this iteration.
