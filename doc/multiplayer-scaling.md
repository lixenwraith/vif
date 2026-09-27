# Multiplayer scaling — baseline and implemented improvements

The original audit is preserved below. The implementation follow-up closes three local changes:

- Guest owner-state relays preserve `MsgStateSync`; the authority returns committed guest artifacts through the incoming intermediary. `TestGuestArtifactsReturnThroughTheirRelay` fails before the fixes and passes in stars, chains, branches and cycles without corrections hiding missed delivery.
- Each correction instance reuses an immutable wall section after comparing every detached wall entry. The comparison owns its values; pointer writes, `Each`, capture aliases, removal/reinsertion and ordering cannot silently reuse stale rows. Other sections and the root are rebuilt normally. JSON, schema, compared surface and cadence are unchanged.
- A baseline retains its compressed correction envelope for fallback recipients. Publication, fresh capture, restart and succession replace or invalidate it. Join and correction envelopes remain distinct.

Follow-up measurements use Go 1.27.1, GOMAXPROCS=1 and three repetitions. Operation timings use warmed real two-participant captures; fallback timings use a synthetic wall-heavy baseline and 16 successful responses, including the first encoding. Values are medians; allocation units are decimal MB.

| Measured operation | 239×64 before → after | 500×250 before → after |
|---|---:|---:|
| Manifest build | 9.08 → 3.37 ms | 77.89 → 22.76 ms |
| Manifest allocations | 5.11 → 3.09 MB | 46.51 → 24.22 MB |
| 16 fallback responses | 65.99 → 3.47 ms | 545.88 → 34.83 ms |
| Fallback allocations | 18.12 → 1.75 MB | 169.25 → 16.56 MB |
| Combined CPU, short live session | 1113 → 844 ms (−24%) | 5967 → 4873 ms (−18%) |
| Combined allocations, same session | 620 → 534 MB (−14%) | 3054 → 2588 MB (−15%) |

Live comparisons use the original deterministic input/link setup with two participants, 120 warmup ticks and 120 measured ticks (six simulated seconds), three paired repetitions per map. Both binaries include the relay fixes and fallback reuse; only wall reuse differs. Hash-only counts are unchanged: 18/22 small and 6/10 large in these short windows. These are CPU improvements, not a claim that the proposed convergence or fleet budgets are met. Full-builder and reused manifest roots match in the operation comparisons. Raw follow-up samples and methodology are in `implementation_followup` in the [data](multiplayer-scaling-data.json).

Regression tests compare full index contents and retained indexes after mutation, and exact fallback bytes after publication, refresh, restart and succession. Removing the wall equality check, owned comparison copy, refresh invalidation or succession invalidation makes the corresponding test fail. Broader section reuse, integrity/capture costs, typed comparisons, navigation, delayed relay convergence, scheduling and projection remain open.


The baseline is `lixenwraith/vif` at `2d8c5b65577036f485625da01eae1f3216aaf5ed`, on the local branch `codex/multiplayer-scaling-audit`. Measurements used temporary `internal/app/zz_scaling_audit_test.go` and `zz_scaling_detail_test.go` harnesses. They were archived outside the repository and removed before committing, as requested.

Native matrix: AMD EPYC 9V74, Go 1.27.1 linux/amd64, GOMAXPROCS=1, GOMEMLIMIT=6500MiB. Each cell has three runs, each with 120 warm-up ticks and 600 measured ticks (30 simulated seconds at 20 Hz). Mesh links use four-tick latency, no configured loss, and no bandwidth cap. Seed is 0x70E4, input RNG seed 7, movement chance 1/3 and firing chance 1/6 per participant per tick. The host leads its direct guests by four ticks. The small map runs the tower region; the large map runs td's natural 500×250 scenario. No rendering, socket compression, replay journal or periodic log snapshots run inside the matrix.

All instances share one process and GC. Combined CPU is measured with getrusage; host and guest attribution uses timed sequential work, so those columns are CPU estimates, not isolated process measurements. Shared GC can move attribution between participants. GOMAXPROCS=1 establishes serialization, not a browser/WASM speed multiplier. These are repeatable 30-second scenarios, not a claim about every later storm or long-running soak. Traffic counts include the 12-byte application frame header and any snapshot chunk header, before the connection's streaming deflate. MB and kB are decimal unless explicitly labelled MiB.

| Map | Participants | Host CPU ms/s* | Mean guest CPU ms/s* | Simulation tick mean, host / guest (ms) | Host framed kB/s | Combined allocation MB/s |
|---|---:|---:|---:|---:|---:|---:|
| 239×64 | 2 | 123 | 91 | 1.17 / 1.51 | 13.8 | 113 |
| 239×64 | 4 | 169 | 93 | 1.39 / 1.70 | 96.9 | 225 |
| 239×64 | 8 | 135 | 88 | 1.46 / 1.68 | 403.8 | 362 |
| 239×64 | 16 | 129 | 92 | 1.78 / 1.86 | 1677.3 | 681 |
| 500×250 | 2 | 446 | 430 | 6.77 / 7.38 | 285.0 | 426 |
| 500×250 | 4 | 1294 | 663 | 7.82 / 9.73 | 262.4 | 1993 |
| 500×250 | 8 | 1466 | 623 | 8.80 / 10.41 | 1228.9 | 3256 |
| 500×250 | 16 | 1716 | 660 | 9.26 / 10.62 | 3792.7 | 5606 |

*Attribution estimates; combined process CPU is measured directly. Guest means span the guests before taking medians across repetitions.


At 16 participants, the small map costs about 6.4 ms of host work and 4.6 ms per guest per simulated tick, including amortized correction work. The large map costs 85.8 ms and 33.0 ms respectively. A 500m fleet CPU budget supplies only 500 CPU-ms per real second. The measured large-map playing host requires roughly 1,716 CPU-ms per simulated second, before compression and rendering. Inability to keep up can cause additional late crossings and repairs, so a simple steady-state extrapolation can be optimistic.

The small map contains about 6,100–6,300 shared positions and 5,305 walls. The large map contains about 51,200–51,300 positions and 50,190 walls. Cells grow 8.17× and wall rows 9.46×. Movement, firing and adaptive cadence mean participant scaling is not monotonic: the host published 46, 214, 244 and 256 rounds per 600 ticks at large-map N=2,4,8,16. N=2 paid for 15 keyframe publications; N=16 paid for five. Hash-only fractions of received manifests were 88.2%, 97.4%, 90.8% and 96.3%, respectively. All small-map configurations were about 97.2%. Matrix transport loss/refusal counters were zero.

Ranked findings and recommended treatment:

| Priority | Location | Evidence and scaling | Fix, expected gain and risk | Contract impact |
|---|---|---|---|---|
| 1 | `snapshot/manifest.go:BuildManifest`, `newSection`; generated `SharedWorldStoreRows`; `converge/selective.go:answerManifest` | Manifest construction takes 9.72/82.02 ms on the small/large final N=16 worlds, allocating 5.54/47.11 MB. It accounts for 50.0%/51.1% of combined CPU at N=16. Every participant repeats O(E) serialization, sorting and hashing. | Compare immutable detached values to previous values and reuse identical canonical rows/sections. Frozen wall encoding alone costs 3.26/33.47 ms and 1.70/19.20 MB; exact wall equality costs 0.016/0.153 ms with no operation allocations. The wall-only implementation is measured above; broader reuse remains open. Preserve membership, order normalization, nilness, float encoding distinctions, page counts and ownership. Never trust missed write counters. | Exact implementation can preserve JSON bytes, roots and every D-rule. No version bump if outputs are identical. |
| 2 | `converge/correction.go:publishRound`, `app/capture.go:sealCapture`, `snapshot/capture.go:Integrity` | Even with no body sent, publication captures, seals, indexes and retains an index. Frozen capture costs 0.99/6.04 ms under the world lock; seal costs 6.03/52.28 ms outside it. Different peer deadlines produced 8.53 publication rounds/s at large N=16, rather than nominal 5/s. | Reuse canonical JSON fragments only after exact detached-value comparisons; retain immutable sections without repeated row copies. Shared publication slots could reduce duplicated rounds, but are a separate scheduling decision. Removing integrity from manifests or hashing a new representation is not a local optimization. | Exact serialization reuse is local. Cadence coalescing, lazy/changed integrity semantics, binary hashing or a different hashed surface need approval and applicable version changes. |
| 3 | `system/navigation.go:Update`; `navigation/flowfield.go:Compute`; `navigation/cache.go:Update`; `routegraph.go:computePathFields` | Navigation Update is 17.9%/17.4% of N=16 CPU; FlowField.Compute is 16.8%/18.9% including other callers. Weighted Dijkstra costs O(G C log C), with point and composite fields. Target movement of five cells resets the throttle, so three ticks is not a strict upper frequency bound. Route fields also allocate whole-map storage for narrow corridors. | Reduce storage and scratch duplication, specialize hot loops, and retain exact distances, directions, heap tie behavior and throttle phase. A new queue is exact only after equivalence is proved. Coarser fields, changed ties or reduced navigation frequency change simulation and are outside this optimization contract. | Storage/loop changes can be exact and local. Changed navigation behavior requires a separate decision and violates the stated exactness target. |
| 4 | `engine/spatial_grid.go:NewSpatialGrid`; flow fields; `snapshot/manifest.go:newSection`; guest staging and retained indexes | Post-GC heap at large N=16 is approximately 4,444 MiB across 16 live and 15 staging worlds: flow fields 1,584 MiB, spatial grids 946 MiB, JSON clones 580 MiB, section allocations 476 MiB and manifest rows 310 MiB. Matrix allocation rate reaches 5.61 GB per simulated second across the process. | Share proven immutable canonical sections, eliminate duplicate ownership copies, and reduce exact derived storage. Each full spatial grid is 32 MB at 125,000 cells; do not confuse the combined heap with a single host's RSS. Shrinking retained history without measuring repair reachability can increase fallback traffic. | Immutable sharing is local if aliases cannot mutate retained evidence. Retention policy changes need explicit review. |
| 5 | `app/snapshot_stage.go:stageProved`, `Commit`; `engine/snapshot_delta.go:countStoreDifference`, `reconcileStore`; `snapshot/delta.go:DiffWritten` | Warm stage costs about 4/35–50 ms. Large-map zero-lag commit still has ~31 ms of capture/comparison. At lag 16, the controlled experiment spends 49 ms projecting and ~75 ms capturing/comparing/writing. Once something moves, reconciliation writes every entry. | Generate exact typed comparisons for comparable components, detach slice-bearing components correctly, and skip genuinely unchanged writes without changing dense order, lifecycle effects or carriers. Address capture allocation and comparison before moving projection. Reconcile staging only if future projection is proven independent of dense store order. | Local exact changes need equivalence tests. Out-of-lock orchestration needs review of every §3.3 invariant. No root-based adoption shortcut. |
| 6 | `system/network.go:flushCrossings`, `sendCursorState`, `relayBatch`; `network/probe.go:linkMeter.observe` | Host epoch and owner-sync egress grow quadratically in a star. At N=16 they are 1.522/2.379 MB/s before stream compression. An unlimited delayed mesh still recorded three saturated samples in 75 observations, with cadence temporarily reaching 56 ticks. | Verify and fix relay routing defects separately. Distinguish propagation bytes from queued bytes in capacity estimation; test on delayed unconstrained and rate-limited links. Do not remove empty epochs or coalesce crossings: epochs also carry pace/sequence information. Any replacement/batching policy must preserve authority decisions and per-frame membership. | Estimator and cadence policy need approval. Changing message contents, set or encoding needs versioning and mismatch refusal. |
| 7 | `converge/correction.go:encodeBodyLocked`; `selective.go:sendKeyframeTo`; `snapshot/delta.go:DiffCapture` | A full correction and bare join capture are separately encoded: 6.23+6.35 ms small, 47.63+48.82 ms large. Fallback service re-encodes the same correction for each peer. Same-world DiffCapture costs 3.04/29.17 ms; DiffWritten costs 9.25/78.60 ms and another integrity pass. | Cache each immutable baseline's correction encoding and reuse it across recipients. Reuse JSON work while retaining the distinct join and correction envelopes. Generate typed comparisons where equivalent. The no-body path already skips both body encoders; do not count them as steady hash-only waste. | Exact local reuse can preserve both formats. Unifying envelopes or changing journal encoding requires approval/versioning. |
| 8 | `system/network.go:sendStateDigest`; simulation systems and status | SharedDigest costs 0.43/2.67 ms per call, about 1.66%/1.47% of combined N=16 CPU. Large-map spatial EntitiesAt is 1.72%, event dispatch 0.56%, route graph recomputation 0.11%, filtered status 0.10%, genetics Update 0.023%, FSM Update 0.026%, soft collision Update 0.029%. Loot is 1.31%, Eye 0.57%; most other species are below 0.1% in this scenario. Inclusive shares overlap. | Preserve the diagnostic initially; optimize higher-cost mechanisms first. These small shares do not prove worst-case late-game AI is cheap. Keep targeted population/soak coverage after changes. | Removing or re-rating the diagnostic requires approval. |

The four-participant relay fixture connects 1–2, 2–3 and 2–4. One 400-tick sample per map gave only 0.98%/0% hash-only answers (small/large), against about 97% in the star. Host egress was 64/363 kB/s and the central relay sent 145/708 kB/s, before stream compression. These runs demonstrate poor convergence, not a successful traffic reduction.

Both routing leads are now confirmed and fixed by the leaf-origin regression above: raw owner state used the epoch message type, and authority commitment excluded an intermediary that still needed the committed copy. The delayed 400-tick relay audit has not been repeated after the fixes, so its broader convergence target remains open.

A separate 400-tick cursorless-host sample with 16 guests (17 instances total) required an estimated 1365 host CPU-ms/s and 637 per guest, with 4204 kB/s host framed egress and 97.0% hash-only answers. This is one sample, not a three-run median.

The CPU model, with cells C, shared entries E, target groups G, host round rate H, guest answer rate A, installs/s I and projected ticks L, is:

`host ≈ 20*Tsim(C,E,G,N) + H*(capture(E)+seal(E)+index(E)) + keyframe/delta/repair service + relay/compression`

`guest ≈ 20*Tsim + A*(scheduled capture + index) + I*(stage + L*Tproject + capture_pair + compare/write + optional journal)`

Per-tick amortized work is per-second work divided by 20. Index and capture operations are at least O(E); sorting/canonicalization adds ordering cost. H is the union of peer due ticks, not a fixed 5 Hz. Each retained index and staging world adds O(E+C*G) memory. The current two-capture guest claim needs correction: TickClosed reads only wanted/promised ticks, deduplicates the schedule, retains two captures, and does not seal those reads. A missed historical capture falls back to CaptureShared and sealing. An outstanding section exchange can reuse its awaiting capture/index.

For a playing host with N total participants, let e be average framed epoch bytes, s owner-sync bytes and d digest bytes. Its star egress is approximately `(N-1)^2 * (20*e + (20/6)*s) + (N-1)*(20/6)*d + manifests + repairs`. A guest sends its own 20 epochs/s, 3.33 owner syncs/s, 3.33 edge-local digests/s, requests and transport probes. With P guests and a cursorless host, owner relays have P(P-1) copies per owner-sync interval, and epochs add the cursorless host's P empty markers per tick. A tree with bounded degree can redistribute the O(N²) total dissemination work; placing all leaves behind one relay moves the bottleneck to that relay. Cyclic/full meshes can add O(N*edges) forwarding copies across all sources, O(N³) in a dense graph. Duplicate suppression prevents repeated application, not every redundant transmission. These models assume correct committed-message delivery.

The N=16 playing host's measured message inventory is below. Rates include relay copies. Payload averages vary with scenario and input; chunk averages are not complete keyframe sizes.

| Kind | Host messages/s, small / large | Average framed bytes, small / large | Host kB/s, small / large |
|---|---:|---:|---:|
| Owner state, 0x11 | 743 / 743 | 751 / 756 | 558 / 562 |
| Epoch, 0x12 | 4,472 / 4,472 | 216 / 406 | 965 / 1,816 |
| Diagnostic digest, 0x13 | 50 / 50 | 243 / 243 | 12.1 / 12.1 |
| Correction chunks, 0x27 | 1 / 22 | 35,166 / 61,542 | 35.2 / 1,353.9 |
| Manifest, 0x28 | 72.6 / 57.6 | 816 / 833 | 59.2 / 48.0 |
| Shard, 0x2A | 1 / 0 | 48,431 / — | 48.4 / 0 |
| Unserved, 0x2B | 0 / 0.5 | — / 118 | 0 / 0.059 |

Requests (0x29) go guest→holder: one representative guest sent 4.80/3.77 requests/s, averaging 157/150 framed bytes. Mean total guest egress across the star is 7.93/11.59 kB/s; mean host→guest link traffic is 111.82/252.84 kB/s. Transport probes and echoes bypass the harness wrapper: add 5/s of 24+57 bytes = 405 B/s per edge per direction. A heartbeat is 12 bytes every 10 seconds. An empty epoch is 44 framed bytes.

The following kinds are transient and therefore measured at zero steady-state rate in the closed-roster matrix. Every ordinary frame has 12 bytes of framing plus its encoded payload. SessionRoute (0x05) carries at most 64 name bytes; Connect/Ack (0x02/0x04) carry JSON identity records; Disconnect (0x03), PeerList (0x20), JoinOffer/JoinReply/Start (0x22/0x23/0x24), AuthorityReport/Handoff (0x2C/0x2E) have variable JSON bodies. Ready (0x25) has no body, hence 12 bytes. ScenarioRequest (0x16) names a scenario digest; ScenarioBody (0x17), join Snapshot (0x26), and Correction (0x27) carry compressed content in chunks with 20 additional bytes per chunk. SessionRestart (0x18) carries the restart address, bounded by MaxSessionName. Reserved RoleAssign/AuthRequest/AuthResponse (0x21/0x30/0x31) and reserved 0x10/0x2D have no live sender. A final large-map full capture is about 10.54 MB JSON and 526 kB compressed; small-map about 1.32 MB JSON and 87 kB compressed. Transient handshake sizes depend on roster, identity and scenario; no fixed numeric size is asserted for these JSON records.

A separate 30-second localhost TCP check used the headless `-serve` binary, td, one scripted guest and GOMEMLIMIT=160MiB. A byte-counting TCP proxy measured about 225.7 kB/s host→guest and 1.26 kB/s guest→host over seconds 5–30. This includes recovery traffic in a lightly driven one-guest scenario, not a converged 16-guest bandwidth forecast or a WAN/WSS benchmark. `/proc` process counters were inconsistent in this environment and are excluded; per-host RSS and isolated CPU remain unverified.

The todo “Project a correction outside the live lock” is worthwhile only after reducing the residual work. The controlled two-participant phase measurements below are medians of three operations; they mirror the existing commit phases and include sparse movement/firing during the artificial lag. They are not a proof of a new concurrent implementation.

| Map | Lag | Staging, outside lock | Projection under lock | Capture pair under lock | Compare/write under lock |
|---|---:|---:|---:|---:|---:|
| 239×64 | 0 | 4.15 ms | 0.01 ms | 1.68 ms | 1.96 ms |
| 239×64 | 4 | 3.46 ms | 0.76 ms | 1.62 ms | 2.27 ms |
| 239×64 | 16 | 3.59 ms | 2.56 ms | 1.71 ms | 4.33 ms |
| 239×64 | 32 | 3.64 ms | 4.86 ms | 1.70 ms | 4.37 ms |
| 500×250 | 0 | 39.11 ms | 0.03 ms | 14.32 ms | 17.00 ms |
| 500×250 | 4 | 39.11 ms | 1.18 ms | 14.77 ms | 35.70 ms |
| 500×250 | 16 | 50.06 ms | 49.03 ms | 17.22 ms | 58.23 ms |
| 500×250 | 32 | 51.25 ms | 118.18 ms | 20.47 ms | 51.95 ms |

Live N=16 small-map commits average roughly 12–16 ms per guest and reach 23 ms in the profiled run; large-map commits average 46–59 ms and reach 136 ms. Fresh staging can exceed 200 ms. Telemetry commit_us includes lock acquisition and post-lock staging cleanup; it is not a pure lock-hold timer. snapshot.capture_us includes sealing, while hash_us does not represent all BuildManifest work. The phase harness is used for the lock attribution above.

Cheaper staging ticks reduce both total CPU and lock time. Fewer projected ticks can help by avoiding queued/stale work, but dropping suffix ticks or crossings is not exact. Incremental projection could bound scheduling chunks on WASM but needs a validated final tail. Native out-of-lock projection can free the live tick goroutine; single-threaded WASM does not acquire parallel compute capacity by moving work between Go goroutines.

An acceptable out-of-lock design must freeze the projection inputs and owner state, classify feed membership by fences, exclude crossings still pending on the live barrier, settle predictions against the authority world before re-derivation, and preserve the journal's place and mark. Before commit, it must validate run/session/term/map/clock and newly retained input, project a bounded final tail under the live lock or safely retry, and perform the write without a live tick intervening. Merely releasing the lock around staging.Tick loses those guarantees. Whole-body root comparison and unproven staging reconciliation remain rejected.

Proposed acceptance budgets for 500×250 and 16 players, for approval rather than claims of current capability:

| Budget | Proposed target |
|---|---|
| Native dedicated host total CPU | ≤400 CPU-ms/s including compression and GC, leaving headroom below 500m |
| Native simulation tick | Mean ≤5 ms, P95 ≤10 ms; simulation plus amortized protocol work ≤15 ms/tick |
| Native guest total work | Mean ≤20 ms per simulated tick; benchmark WASM separately on an agreed reference browser/device |
| Live lock | Tick P95 ≤10 ms; correction commit P95 ≤10 ms, P99 ≤20 ms, including a 16-tick projection case |
| Converged guest compressed stream | ≤32 kB/s down, ≤8 kB/s up, averaged over 30 seconds |
| Dedicated host compressed uplink | ≤512 kB/s steady for 16 guests |
| Recovery reserve under existing 3-second floor | Allow 600 kB/full capture, ≤256 kB/s per guest and ≤4 MB/s host egress over the recovery window; do not conflate this with converged traffic |
| Memory | Dedicated host RSS ≤160 MiB steady and <192 MiB peak; this is a separate blocking gate, not guaranteed by GOMEMLIMIT |
| Convergence | ≥95% hash-only after warm-up in the agreed direct and relayed scenarios, with exact roots/world/replay equivalence for optimization changes |

Remaining order: reduce integrity/capture and typed-comparison costs, measure further immutable sharing, then optimize navigation/storage with exact distances, directions and phase. Recheck delayed relay convergence. Cadence coalescing, propagation-aware link estimation, diagnostics changes and projection outside the live lock require their architectural review; the proposed budgets remain unapproved.

Verification passed: `go test ./internal/system ./internal/app ./internal/converge ./internal/snapshot -count=1`, `go build ./...`, and `GOOS=js GOARCH=wasm go build ./cmd/...`. The package tests include convergence/navigation/no-op/tamper/replay/suffix/ordering/soak checks. Changed-file formatting and whitespace checks pass. No full repository test/vet sweep or race run was performed. Temporary measurement harnesses were removed before committing.
