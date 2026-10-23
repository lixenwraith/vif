# Netcode optimization

A working document: where a session spends CPU, memory, bandwidth and latency,
and the approaches that could spend less, each landable and measurable on its own.
§1 summarises the netcode as built and the constraints it sets, §2 the readings
that decide between approaches, §3 the approaches and §4 their order. Nothing in
§3 is implemented. Bare section numbers are this document's; "multi-player §n" is
[Multiplayer](multi-player.md) and D-n the [domain model](domain-design.md).
Figures are multi-player §6's and the fleet run's in
[todo](todo.md#decide-the-authoritys-fan-out-and-publication-cadence) unless a line
names another source. An approach that lands is described in those documents and
leaves this one.

## 1. The netcode today

vif is not lockstep. Every instance runs the whole Shared simulation at 20 ticks a
second, a guest predicting it and the authority correcting it (multi-player §10.3),
and the wire carries what a receiver cannot re-derive:

| Stream | Carries | Encoding | Rate and fan-out |
|---|---|---|---|
| Epoch (`MsgEvent`) | one source's closed tick of D-3 crossings, empty ones included | JSON `WireBatch`; each frame's payload is the journal's TOML text | every tick per source; raw up in one `MsgBundle`, the committed copy down every link, the producer's own included |
| Owner sync (`MsgStateSync`) | one cursor's D-13 cells | the epoch's JSON envelope over a 27-field TOML payload | every `NetworkSyncTicks` (6) per cursor, committed and relayed like an epoch |
| Correction | a manifest root, then index, page hashes and shards; or a keyframe | JSON; keyframes and shard sets deflated by the snapshot codec | per peer every 2–60 ticks; a whole world or a hash-only proof within each 60-tick floor |
| Control | link probe and echo (5 Hz), state digest (every 6 ticks), roster, succession | small | per link |

Each link is one TCP or WebSocket stream per direction, deflated at level 7
(`NetworkStreamLevel`) against its own history, so a broadcast is deflated once per
link and a body the snapshot codec already deflated is deflated again. The manifest
is a tree — root; a section per store and per carrier; up to 64 pages a section,
rows bucketed by a hash of their identity; rows — hashed with byte-serial FNV-1a
over each row's JSON, and row reuse keeps the JSON and hash of each row whose owned
copy still compares equal. Equal roots send no state. The root travels alone, so a
first mismatch costs two round trips after it, for the index and then for the
receiver's page hashes, before the shards arrive.

Per simulated second a host costs 20·T_sim + R·(capture + seal + index) plus the
bodies it serves, R being its publication rounds, and a guest 20·T_sim +
A·(capture + index) plus its installs, A being the manifests it answers. At 16
players (multi-player §6, which times seal, index, keyframe and tick on a slower
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

Four limits follow, and §3 is sorted against them:

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

## 2. Measure first

Multi-player §6 counts bytes framed before deflate on the in-process mesh, which does
not deflate, and times stages rather than their parts. Each missing reading decides
approaches:

| Reading | Decides |
|---|---|
| Wire bytes after the link's deflate, by message kind, on the socket path | N1–N3; N4's second step |
| CPU by stage on host and guest: JSON and TOML encode and decode, per-link deflate and inflate, the second deflate of bodies, FNV against row encoding inside seal and index, the digest | N2, N4, N9, N12, N15 |
| Keyframes by reason — schedule, floor, request, repair wider than a frame, failed proof, the bodies after a fallback — and how many are encoded for how many sent | N6, N8 |
| Plain bytes per section of a td keyframe | N8, N9 |
| Lock hold by part: stores, carriers, status, comparison, write, each projected tick | N10, N11 |
| Repairs by cause: the sections carried, and whether the authority committed one of the receiver's crossings late or void since its last proof | N5, and the next divergence fix |
| Heap by owner: retention ring, staging world, encoder caches | N9, N13 |

### 2.1 Scenarios

One matrix judges every approach, on harnesses that exist:

| Axis | Values | Harness |
|---|---|---|
| Map | tower, td | `TestATowerGuestAnswersHashOnly` (multi-player §6's shape), `TestCostAtTheStormHighWater`, `BenchmarkCaptureAtStormHighWater` |
| Players | 2, 4, 16 | the same, seated with bots |
| Link | in-process; four-tick links; staged latency, loss and byte budget | `network.LinkShape`, `TestStagedLinkShapingKeepsCorrectionsBoundedAndRecovers` |
| Path | in-process mesh; TCP with deflate; WebSocket to a browser | a socket variant of multi-player §6's shape; a browser joiner |
| Host | native at `GOMAXPROCS=1`; a fleet pod at 500m CPU; a WASM guest | `BenchmarkHeadlessTicks`; [Measure a full roster](todo.md#measure-a-full-roster) |
| Load | idle; a move every third tick and a shot every sixth; the storm high water | multi-player §6's profile, the storm fixture |

Metrics, from multi-player §7's telemetry where it exists: CPU-s/s by stage;
world-lock hold, p50 and p99; egress before and after deflate by kind; the hash-only
share; repair and keyframe bytes; `snapshot.correction_*` and
`snapshot.projected_ticks`; `network.barrier_delay_ticks` and `network.commit_late`;
heap in use. An approach lands when it moves its target and leaves the hash-only
share, correction magnitude and the protocol tests where they were.

## 3. Approaches

In the table − lowers a cost, + raises it and · leaves it. Projections come from
§1's figures; §2 confirms or refutes them.

| | Approach | CPU | Mem | Bandwidth | Latency | Complexity | Projected |
|---|---|:-:|:-:|:-:|:-:|---|---|
| N1 | One committed frame per link per tick | − | · | − | + ms | medium | down messages 4,444 → 300 a second at 16 players; ~40% of tower epoch bytes |
| N2 | A binary wire codec | − | − | − | · | low, then medium | a four-move epoch 567 → ~50 B before deflate |
| N3 | Owner syncs that carry what changed | − | · | − | · | low | syncs ~10× smaller before deflate |
| N4 | Compress once | − | · | · | · | medium | the second deflate of td's 2.18 MB/s of bodies |
| N5 | A one-round-trip descent | · | · | ± | − | medium | a repair one round trip sooner |
| N6 | Repairs as wide as the keyframe they replace | − | · | − | − | low | the 48 kB → 516 kB step at td |
| N7 | Aligned publication rounds | − | · | · | + | low | td rounds 7.2 → ~5 a second |
| N8 | Keyframes from cached sections | − | + | + | · | medium | keyframe deflate in proportion to the sections that moved |
| N9 | A binary row codec | − | − | − | · | high | rows 3–10× smaller before deflate |
| N10 | Converged answers without a capture | − | · | · | − | medium | a guest's capture on 95% of its answers |
| N11 | Less work under the live lock | · | − | · | − | high | a deep install's lock from past 100 ms to about a tick at td |
| N12 | A faster deterministic hash | − | · | · | · | low | hashing ~10× faster, at a share §2 measures |
| N13 | Retention by reference | + | − | · | · | medium-high | retention from ~4 worlds to ~1 |
| N14 | Relay trees chosen by the authority | · | · | − | + | high | root egress O(N²) → O(N·k) |
| N15 | One parity mechanism | − | · | − | · | low | an O(E) hash every 6 ticks per instance |

### N1. One committed frame per link per tick

The authority commits and relays inside the drain that opens its tick and sends its
own epoch and syncs as the tick closes, each a message of its own: per link per
tick, one per source. Instead, at the close of tick T it sends each link one frame:
the run and T once, its own epoch and syncs, and every copy committed in T's drain,
each as a source marker `{source, produced tick, frames}` with void frames removed.
The markers keep what reads epochs unchanged: the per-source epoch and sequence
windows, the fences, and pacing (`observeAuthorityEpoch`). The body is encoded once
for every link; each link's copy ends with a tail for the sources it carries — each
epoch's `Age` and the frames committed late or void — in place of frames that
link's producers already hold, which is the todo's frames-less echo. A relay
forwards the body and splits the tail among the links behind it.

- *CPU*: one encode a tick instead of one per epoch; a guest decodes 20 frames a
  second rather than 20·N, and framing and queueing fall alike.
- *Bandwidth*: the per-epoch envelope — about 70 bytes of JSON and a 12-byte frame
  header, around 40% of a tower epoch's 190 — becomes a few bytes per source, and
  the echo, a quarter of a two-seat guest's downlink in the fleet run, loses its
  frames. Every link still receives every source's crossings.
- *Latency*: a copy committed at T's opening leaves at its close, later by the tick
  body (1–3 ms at td), inside `NetworkRelaySlackTicks`.
- *Challenges*: hop counting moves to the frame and duplicate suppression to the
  marker; `adoptRelay` finds its own epochs inside a frame; eviction
  (`driveEviction`) counts raw epochs upstream, which do not change.

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

A sync is 27 fields every six ticks per cursor; in the fleet run 17 were zero in a
595-byte TOML, and syncs were a fifth of the command stream. In order of cost:
`omitempty` on `CursorStatePayload`, as other payloads carry; with N2 a presence
mask; then a sync sent only when a field differs from the last one sent, with a
refresh every K syncs, so a frame a full queue refused costs bounded staleness. A
sync stays a whole state, never a delta on an unacknowledged base, so a loss costs
one refresh rather than a broken chain. Cells every receiver re-derives (the
shield's inverse squares from its radii) leave the payload.

- *Bandwidth*: 736 syncs a second of 760 bytes at 16 players, 0.56 MB/s before
  deflate, about a tenth with a mask, less again while cursors idle; how much less
  depends on which cells change every tick (heat, energy).
- *Determinism and latency*: unchanged — a sync is still committed at one tick for
  every instance, and mirrors already lag by `NetworkSyncTicks`.

### N4. Compress once

Keyframes and shard sets leave the snapshot codec deflated, then pass through every
link's level-7 stream, whose matcher finds nothing to take, once per link.

1. Carry a pre-compressed body beside the stream: the link frames its deflate output
   in segments, so a frame flagged `FlagCompressed`, reserved today, travels raw
   between two segments.
2. Then weigh a broadcast frame (N1's shared body) compressed once with a preset
   dictionary (`flate.NewWriterDict`) against once per link with each link's history.
   The per-link stream was chosen for small repetitive frames; §2's after-deflate
   reading says where the two cross.

- *CPU*: the host stops re-deflating body bytes — 2.18 MB/s at td with 16 players,
  tens of milliseconds a second at level 7 by estimate — and a browser inflates each
  body once.
- *Bandwidth*: none; a raw body is no larger than one deflated twice.
- *Challenges*: the link format changes on the TCP and WebSocket paths alike
  (`flateStream`), and its reader interleaves segments and raw frames on one stream.

### N5. A one-round-trip descent

A receiver cannot send page hashes with its first answer because it does not know
the sender's partition (`SectionSummary.Pages`). Carry the partition beside the root
— a page exponent byte per multi-page section — and a receiver whose root differs
answers with its whole index: every section hash, and the page hashes of every
multi-page section under the sender's partition. The authority, or a relay from
retention (`serveRelayed`), compares it with the retained index and answers with the
shards: root, index, shards. The shard set then drops the summaries of sections it
does not repair, which the receiver's own index proves — the todo's candidate.

- *Latency*: a repair lands a round trip sooner — at a 150 ms round trip, three ticks
  less to project and three fewer of divergence before it lands — and fewer requests
  are overtaken by a newer index.
- *Bandwidth*: the root grows by tens of bytes for every peer, a differing answer by
  the receiver's index (a few kilobytes at td, on a minority of manifests), and the
  index message goes. Exponents can go only to peers whose last answer differed if
  the bytes on every manifest matter.
- *CPU*: none new; the receiver builds those page hashes to compute its root.
- *Projected*: matters where repairs are frequent — at td with two players 22% of
  manifests are not answered hash-only.

### N6. Repairs as wide as the keyframe they replace

A repair wider than `SnapshotShardBytesMax` (48 kB, one frame) is replaced by a
keyframe — 516 kB at td — and the peer takes whole bodies for a few publications
after. Chunk shard sets as bodies are chunked
([Pace repairs as bodies are paced](todo.md#pace-repairs-as-bodies-are-paced)) and
bound a repair by the keyframe it stands in for rather than by a frame, which keeps
multi-player §4's rule that a repair never costs more than that keyframe.

- *Bandwidth and CPU*: removes the step from a 48 kB repair to a 516 kB keyframe, and
  that keyframe's seal and deflate; §2's keyframes by reason say how much of td's
  2.18 MB/s it is.
- *Latency*: a chunked repair waits on its link's backlog as a body does, and the
  epochs behind it wait one chunk.
- *Challenges*: few — the chunk assembly exists for bodies, and a set still applies
  whole.

### N7. Aligned publication rounds

The round rate is the union of the peers' due ticks — 7.2 a second at td with 16
players against a nominal 5 — and each round is a capture under the lock, a seal and
an index. Quantise each peer's cadence to a power of two and its due ticks to one
phase (`tick % cadence == 0`): peers on different cadences then share rounds, and R
is the fastest cadence present rather than the union.

- *CPU and lock*: up to 2.2 rounds a second fewer at td, about 70 ms of CPU a second,
  nearly half of it under the lock.
- *Latency*: after a plan change a peer waits up to one cadence for its phase.
- *Challenges*: few; the floor, the hold window and the `Next` promise are untouched,
  since `Next` already names the next tick a peer is indexed at.

### N8. Keyframes from cached sections

Row reuse makes a keyframe's JSON cheap, but its deflate still runs over the whole
capture: 58 ms at td. Compress each section on its own, keep its compressed bytes
while its section hash holds, and send a keyframe as a container of section blobs.
td is a sprawling maze in which every wall cell is a Shared entity with a position
and a wall row, so most of a keyframe would be blobs already compressed. A join's
capture (`EncodeCapture`) takes the same container.

- *CPU*: multi-player §6's figures put up to four 58 ms keyframes a second on a td
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
(multi-player §4.1), so the root is in hand as the tick closes; a manifest that
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
hashing to rows that changed, so this pays only if §2 finds hashing a real share of
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
relay subtrees, turning its egress from O(N²) into O(N·k). The open parts are in
[Decide the authority's fan-out](todo.md#decide-the-authoritys-fan-out-and-publication-cadence)
and [Reach a relay](todo.md#reach-a-relay-without-an-address-in-hand).

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
priced by §2. Drift is then seen at the cadence (2–60 ticks) rather than every six,
and the digest's noise on a mixed-platform session (multi-player §8 item 3) goes
with it.

## 4. Order

Each approach lands alone, re-runs §2's matrix and updates multi-player §6.

| Phase | Approaches | Why |
|---|---|---|
| 0 | §2's readings | Every projection in §3 rests on bytes before deflate or whole-stage timings |
| 1 | N3's `omitempty`, N1, N2 step 1, N6, N7, N5, N4 step 1 | Low complexity, local to `internal/system/network.go`, `internal/converge` and `internal/network`: the command stream's constants and the repair path's latency |
| 2 | N10, N8, N11's capture as a copy, N12 if measured | CPU and lock at the map limit, on the host and the browser guest |
| 3 | N2 step 2, N9, N13, N11's projection, N15, N14 | Formats and structure: the journal, the capture schema, retention, topology |

N4's second step needs N1; N2's second step follows its first; N13 and the column
layout build on N9; N10's row cache is cheaper on N9's rows. §2's after-deflate
reading orders phase 1: if the stream already compresses JSON envelopes well, N1 and
N2 pay in CPU more than in bytes, and N5 and N6 move up.
