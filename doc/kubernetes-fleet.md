# Vi-Fighter as a Kubernetes-hosted dedicated-server fleet

Feasibility review of running headless `vif` host processes as a k3s pod fleet
(linux/amd64, `FROM scratch`/distroless, static CGO-free binary, non-root UID,
read-only rootfs, no added capabilities, fixed memory limit, SIGTERM/SIGKILL
shutdown contract), with remote clients connecting over the existing framed-TCP
protocol from outside the cluster.

Scope: the application. Cluster engineering — manifests, ingress, allocator,
topology — is deliberately absent.

Reviewed at `f1d1f49` (`main`). Measurements were taken on linux/amd64, 4 vCPU,
Go 1.26.5, `CGO_ENABLED=0`, against a binary built from that commit.

---

## 1. Verdict

**The runtime shape already exists.** `-serve <bind-address>` constructs
`app.ModeServer` (`internal/app/config.go:45`), which is headless (no terminal,
no renderer, no audio, no PTY), real-time clocked (the `PausableClock` and the
scheduler goroutine, not the manual clock — `Mode.Driven()` returns false for
`ModeServer`, `internal/app/config.go:64`), and accepts live remote client input
over the network (`App.Serve`, `internal/app/serve.go:53`, running the full
host/lobby/authority/correction path). It holds no local player cursor by
construction: its roster slot is `parameter.NoPlayerSlot` and
`App.localPlayers()` returns zero (`internal/app/serve.go:118`). Clock source,
terminal ownership, renderer, audio sink and input source are selected as
independent predicates on `Mode` — `Presents()`, `Driven()`, `Audio()`,
`OwnsGeometry()`, `OwnsInput()`, `Serves()` — and `App.init` consults each
separately (`internal/app/app.go:125`), so the axes are not bundled into
App-shape constructors. The FSM, ECS and event layers are already agnostic: none
of them reference a terminal, and the only geometry input on this path is the
`-size WxH` flag. I verified this by running it: a `-serve` process with `HOME`
and `XDG_CONFIG_HOME` pointed at a nonexistent directory, no TTY, no
controlling terminal, started, bound, accepted a `-join` guest, and simulated a
6,000-tick session to completion. **This review is planning, not archaeology.**

The README is stale — it still advertises "three runtime shapes" and describes
`-host` as the only hosting form (`README.md`). `-serve` is correctly documented
in `doc/runtime.md` §1.2, `doc/development.md` and `doc/architecture.md`.

What is *not* ready is everything around that shape. There are no blockers, but
there are eleven **required** gaps, and three of them are wire-reachable defects
that make an internet-exposed pod indefensible rather than merely
under-hardened: a confirmed remote panic, a confirmed 64 MiB-per-source
allocation primitive, and unbounded per-tick retention driven by an
attacker-chosen apply tick. The lobby also blocks forever with no timeout, which
in Kubernetes terms is a pod that never becomes Ready and never terminates —
and there is no "match over, exit with a status code" path at all, so
`restartPolicy` has nothing to react to. Logging is file-only by explicit
construction (`EnableConsole(false)`, `internal/vlog/vlog.go`), so a pod
produces zero stdout. Close these and the architecture fits well; the process
model, the determinism story and the ephemeral-pod lifecycle are unusually
well-matched.

---

## 2. Gap table

Severity: **blocker** = makes the architecture impossible; **required** = the
deployment is unsafe or unmanageable without it; **desirable** = cost or
hygiene. "Scope" is engineering effort on the application only.

| ID | Description | Sev | Affected files | Scope | Depends on |
|---|---|---|---|---|---|
| **G1** | Lobby blocks indefinitely. `waitForStartup` loops until `PeerCount() == remoteCount` with no deadline; the only exits are a signal or a stream error. A pod started with `-players 3` that receives two guests never becomes Ready and never terminates. | required | `internal/app/session.go:404` (`waitForStartup`), `internal/app/session.go:278` (`startHostSessionOn`) | S — add a deadline parameter and an admission policy hook | — |
| **G2** | No match-over exit. `Serve` loops on a frame ticker and a report ticker until a signal; there is no game-over event, no idle-timeout, no exit code. Measured: the server kept ticking and stayed resident after its only guest disconnected. | required | `internal/app/serve.go:82–104`, `internal/system/meta.go` (no terminal state), `cmd/vif/main.go:83–92` | M — needs a match-lifecycle concept, not just a timer | ADR-1 |
| **G3** | Structured logs never reach stdout. `vlog` builds its logger with `EnableConsole(false)` ("console writes corrupt the alternate screen") and `EnableFile(true)`; JSON goes to `$XDG_STATE_HOME/vi-fighter/log/`. A pod emits nothing. Worse: if `-l` is given and the directory is unwritable, `setupDiagnostics` prints one line to stderr, sets exit 73, **and runs the whole session unlogged anyway**. | required | `internal/vlog/vlog.go:133–164` (`buildLogger`), `cmd/vif/main.go:64,107–147` | S — a console sink and a fail-fast switch | — |
| **G4** | No health/readiness surface. No HTTP listener, no Unix socket, no status file, nothing a probe can read (verified: no `net/http` import anywhere under `internal/` or `cmd/vif`). Readiness in particular must go **false** when the lobby fills, or the Service keeps routing clients to a match they cannot enter — and `assignParticipant` already returns "session is full" only *after* the TCP connect and a roster-slot allocation. | required | new; sources exist in `internal/status/registry.go`, `App.SessionSummary` (`internal/app/host.go:166`) | M | G1 |
| **G5** | **Wire-reachable panic (confirmed).** `EventLevelSetup` is `ClassShared` (`internal/event/registry_gen.go:194`), so it replicates and any peer may inject it. `MetaSystem.handleLevelSetup` passes attacker-chosen `Width`/`Height` unvalidated to `World.SetupLevel` → `SpatialGrid.Resize`, which computes `need := newWidth * newHeight` and calls `make([]Cell, need)` with no overflow or magnitude check. Reproduced: `SetupLevel(1<<31, 1<<31)` → `runtime error: makeslice: len out of range`. Under `core.Go`'s crash handling that is `os.Exit(1)`. | required | `internal/engine/spatial_grid.go:185`, `internal/system/meta.go:376`, `internal/event/payload.go:18` | S — clamp in the payload validator and in `Resize` | — |
| **G6** | **Wire-reachable allocation primitive (confirmed by reading).** `NetworkSystem` holds `snapshots [MaxPlayers+1]network.SnapshotAssembly` (`internal/system/network.go:126`), indexed by source. `SnapshotAssembly.AddChunk` accepts any declared body length up to `MaxSnapshotBytes = 64 << 20` and immediately does `make([]byte, 0, total)`. One 20-byte header per source buys 64 MiB; 17 sources buy ~1.06 GiB. | required | `internal/network/snapshot.go:30,98–160`, `internal/system/network.go:126,1366` | S — bound `total` against a measured world ceiling, not 64 MiB | — |
| **G7** | **Wire-reachable unbounded retention.** `scheduleCrossings` appends admitted frames to `NetworkSystem.scheduled` with the frame's own `ApplyTick`; `applyDue` only drains entries whose `applyTick <= nextTick`. Nothing bounds `applyTick` relative to the local tick. A peer sending batches with monotonically increasing `ProducedTick` (which `epochWindow.admit` accepts unconditionally when `tick > w.high`) and a far-future `applyTick` grows `scheduled` at line rate; `relayBatch` then amplifies it to every other peer. | required | `internal/system/network.go:1645` (`scheduleCrossings`), `:1726` (`applyDue`), `epochWindow.admit`, `internal/parameter/network.go:NetworkEpochWindow` | M — needs a forward apply-tick window and a `scheduled` cap | — |
| **G8** | No authentication and no transport security. Both roles construct `network.DebugConfig(...)`, which hard-codes `TLS: nil` (`internal/network/config.go:117`); `MsgAuthRequest`/`MsgAuthResponse` are reserved and unused (`internal/network/protocol.go`). Any TCP peer that completes the handshake receives the host's `JoinAnchor` — including `ConfigID`/`ContentID`, which are **absolute host filesystem paths** — a roster slot, a full world capture, and the ability to inject every `ClassShared`/`ClassBus` event: `EventGameResetRequest`, `EventLevelSetup`, `EventMetaSystemCommandRequest` (enable/disable systems), `EventFSMRegionRequest`, and every spawn request. `admissibleFromSource` restricts only participant join/depart. | required | `internal/network/config.go:97–123`, `internal/network/session.go:118` (`HostAcceptor`), `internal/system/network.go:admissibleFromSource`, `internal/app/app.go:412` (`buildAnchor`) | L | ADR-3 |
| **G9** | Serial pre-auth handshake starves the accept loop. `Transport.acceptLoop` calls `config.AcceptSession(conn)` **synchronously** (`internal/network/transport.go:129`). `HostAcceptor` allocates a roster slot *first* (`c.Assign()`), then sets a `ConnectTimeout` (5 s) deadline and blocks in `Decode`. One connection that opens and never writes stalls every other join for 5 s; a trickle of them denies the lobby entirely. It also extends worst-case SIGTERM latency, because `Transport.Stop` waits on this goroutine. | required | `internal/network/transport.go:99–152`, `internal/network/session.go:118–165` | M — handshake off the accept goroutine, with a concurrency cap | G8 |
| **G10** | No build identity on join. `anchorIdentity` compares schema (a *record layout* version), seed, session counter, `config_id` (a **path string**, not a content hash), `content_id`, a content pin, three corpus counters, and `tick_ns`. There is no binary version, no VCS revision, no protocol version, and no hash of the resolved FSM/system manifest. Two builds that differ in simulation math or system ordering but resolve the same config path join successfully and diverge silently; the D-11 digest probe reports it only after the fact, `NetworkDesyncSamples` later. Determinism is claimed "for one build" and nothing enforces "one build". | required | `internal/app/replay.go:102–127`, `internal/event/journal.go:64–107`, `internal/app/join.go:67` | M — add an identity field and a manifest hash to the anchor; bump `JournalSchema` | ADR-4 |
| **G11** | No `GOMEMLIMIT`, no `GOGC`, no explicit `GOMAXPROCS` anywhere in the tree (grep across all of `internal/`, `pkg/`, `cmd/` returns nothing). Under a hard pod memory limit the Go heap has no soft target, so a transient allocation spike is an OOMKill rather than a GC pause. | required | new; `cmd/vif/main.go` | S | Memory report |
| **G12** | Spatial grid is a fixed 30.5 MiB allocation, independent of map size, and grow-only. `NewPosition` builds `NewSpatialGrid(DefaultGridWidth=500, DefaultGridHeight=250)`; `Cell` is exactly 256 bytes, so 125,000 × 256 = 32,000,000 B. `SpatialGrid.Resize` shrinks `len` but never `cap` (`g.Cells = g.Cells[:need]`). Measured: **82.3 % of a headless run's heap-in-use is this single allocation**, for a map that needs 8,000 cells. A guest that installs a capture builds a second world and pays it twice. | desirable (but it is the density lever) | `internal/engine/position.go:44`, `internal/engine/spatial_grid.go:57,185`, `internal/parameter/engine.go:61,70,73` | S — size the grid from resolved geometry | — |
| **G13** | No build tag excludes terminal, renderer and audio from a server binary. `internal/render`, `pkg/audio` and the terminal module are linked unconditionally (17.0 MB binary). `ModeServer` never constructs any of them, so this is pure attack surface and image size. The package structure permits the split: presentation is reached only through `Mode.Presents()`/`Mode.Audio()` predicates in `App.init`, and `internal/manifest` is the single registration point for renderers. | desirable | `internal/app/app.go:171,322`, `internal/manifest/definition.go`, `Makefile` | M | — |
| **G14** | `SIGHUP` is handled identically to `SIGTERM`/`SIGINT` — all three terminate (`internal/app/signal_unix.go:14`). Conventionally SIGHUP is reload. Harmless in a pod, but it forecloses a reload semantic later. | desirable | `internal/app/signal_unix.go` | XS | — |
| **G15** | On crash with no terminal, `core.HandleCrash` falls through to `terminal.EmergencyReset(os.Stdout)` because `crashTerminal` is nil outside `Mode.Presents()`. Escape sequences land in the pod log. | desirable | `internal/core/crash_handler_unix.go:26–32`, `internal/app/app.go:233` | XS — skip when no terminal was ever claimed | — |
| **G16** | Telemetry exists but is not exposable. `internal/status.Registry` holds every counter a metrics endpoint would want (`network.peers`, tick counters, per-region FSM state, `spatial.*`, correction cadence) and `snapshot.go` can serialise it — but only to a periodic file under the log directory. No exposition format, no endpoint. | desirable | `internal/status/registry.go`, `internal/status/snapshot.go`, `internal/app/host.go:166` | S once G4 exists | G4 |
| **G17** | README advertises three runtime shapes and omits `-serve`. | desirable | `README.md` | XS | — |
| **G18** | `-check` cannot be combined with `-serve` (`sessionFlags.validateInvocation` rejects the pair). An init container must re-supply the resource flags without the session flags. Not a defect, but it means the validated config and the served config are two invocations that can drift. `-check` does exit non-zero on invalid config (`resource.Check` error → `os.Exit(exitFailure)`), so it is otherwise a correct init step. | desirable | `cmd/vif/main.go:291–305`, `internal/resource/check.go:21` | XS | — |

### Explicitly not a gap

- **Address advertisement.** The wire protocol never embeds or advertises an
  address back to clients. `SessionOffer` (`internal/network/session.go:22`)
  carries an anchor, a roster of `{ID, Slot}` pairs, a term and snapshot
  metadata — no host or peer addresses. Every peer talks only over the
  connection it dialled, and relaying uses `BroadcastExcept` over established
  peers. **NodePort or LoadBalancer address translation is safe as-is.** This
  was the single most likely architectural landmine and it is not there.
- **CGO.** Verified empirically: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go
  build ./cmd/vif` succeeds and produces a **statically linked** ELF (`file`:
  "statically linked"; `go version -m`: `CGO_ENABLED=0`). No `import "C"`
  anywhere in the tree. Audio backends are `exec.Command` shell-outs to
  `pacat`/`pw-cat`/`aplay`/`sox`/`ffplay` plus an `os.OpenFile("/dev/dsp")` OSS
  path and a null/WAV sink (`pkg/audio/detector.go:23–30`,
  `pkg/audio/engine.go:213,221`) — nothing is linked or `dlopen`ed, and
  `ModeServer` never registers the audio service at all (`Mode.Audio()` is false),
  so `DetectBackends` is never called.
- **Embedded assets.** `internal/asset` embeds the default FSM bundle, the
  tutorial corpus, the splash font and the keymap; `pkg/audio` embeds its sound
  and pattern specs. `-d` forces the embedded set and bypasses all discovery.
  A `FROM scratch` image needs no asset layer.
- **Process singletons.** No PID files, no lock files, no `/tmp` usage, no
  fixed ports (the bind address is `-serve`'s argument), no global `math/rand`
  (`vmath.FastRand` streams are seeded from the root seed;
  `internal/engine/resource.go:397`). The only wall-clock seed is
  `cfg.Seed = time.Now().UnixNano()` when `-seed` is absent, and it is logged.
  N instances per node do not collide.
- **Busy-wait.** There is none. `schedulerLoop` is a drift-corrected
  `time.Timer` with a `stopChan` case; `eventLoop` is a 4 ms ticker that
  `continue`s immediately when the queue is empty; `awaitFrame` has a
  `tickInterval*2` timeout. Measured: a `-serve` process blocked in the lobby
  consumed **3 jiffies over 15 s (0.2 % of one core)**.

---

## 3. Sequenced plan

The ordering is driven by three constraints: (a) nothing that changes the wire
format may land after something that depends on the wire format being stable;
(b) the safety fixes are independent of each other and of everything else, so
they go first and can land in parallel; (c) the lifecycle work defines what
"ready" and "done" mean, and the probe surface must be built against those
definitions rather than guessed at.

**Phase 0 — make an exposed pod survivable (G5, G6, G7).**
All three are input-validation fixes in existing code paths. They are additive
(new bounds checks), touch no wire format, and are independently testable with a
fuzz harness against `NetworkSystem.dispatchMessage`. G5 is a two-line clamp;
G6 is replacing the 64 MiB constant with a ceiling derived from the measured
world high water; G7 needs a forward apply-tick window (the natural bound is
`nextTick + SnapshotFloorKeyframeTicks`, since anything beyond the convergence
floor is unusable anyway) plus a hard cap on `len(scheduled)`. **These must land
before any pod is exposed to a network the operator does not control.** Nothing
else in the plan depends on them, which is exactly why they go first.

**Phase 1 — lifecycle (G1, G2).**
`-serve` needs a lobby deadline flag and an exit-on-match-end policy before any
probe can be written, because readiness and liveness are defined in terms of
them. G1 is additive: a deadline parameter on `waitForStartup` and a policy
enum. G2 is not — it requires a match-termination concept the simulation does
not currently have (see ADR-1), and it must decide what `Serve` returns so
`cmd/vif` can map it to an exit code. Do G1 first; it is small and it alone
converts "pod hangs forever" into "pod fails fast".

**Phase 2 — observability (G3, G4, G16).**
G3 is a `vlog` change (a console sink plus a fail-fast switch when the sink
cannot be opened) and is independent of everything. G4 depends on Phase 1
because readiness = "lobby accepting joins" and liveness = "tick counter
advancing", and both need Phase 1's states to exist. G16 is nearly free once G4
adds a listener: `status.Registry` already holds the counters, and
`internal/status/snapshot.go` already walks them. Do G3 first and separately —
it is the cheapest change in this document and it is what makes every subsequent
phase debuggable in-cluster.

**Phase 3 — memory dimensioning (G11, G12).**
G12 is additive and localised: pass resolved geometry into `NewPosition`, and
have `SpatialGrid.Resize` reallocate downward past a hysteresis threshold. It
roughly halves per-pod RSS and therefore doubles achievable density, so it pays
for the whole review on its own. G11 (`GOMEMLIMIT` from the cgroup limit or an
explicit flag) should land *after* G12, because the right soft limit depends on
the post-G12 ceiling. Both should be gated on the property-based cycle harness
described in §5.

**Phase 4 — identity and admission (G10, G9, G8).**
Strictly ordered, because each widens the handshake the next one modifies.
G10 changes the anchor and bumps `JournalSchema`, which is a wire-format break —
it must land before anything else touches the handshake, and it is the change
that makes a mixed-build fleet detectable rather than silently divergent.
G9 restructures the accept path (handshake off the accept goroutine, bounded
concurrency, deadline before slot allocation rather than after). G8 — a shared
secret or mTLS — is last because it is the largest and because it changes the
same code G9 restructures. Note that G8's *value* is conditional on ADR-3: if
the fleet decides clients never reach pods directly (an authenticating gateway
terminates and re-originates), G8 shrinks to "bind only to the pod network".

**Phase 5 — image and hygiene (G13, G14, G15, G17, G18).**
G13 (a `noterm` build tag excluding render/audio/terminal) is a genuine
refactor of the manifest's renderer registration, but it is the only remaining
attack-surface reduction and it is independent of everything above. The rest are
one-line changes that can ride along with any phase.

**Additive vs. modifying.** Additive: G1's deadline, G3's console sink, G4/G16's
listener, G11's limit handling, G12's sizing, G13's build tag, G14/G15/G17/G18.
Modifying existing subsystems: G2 (match lifecycle touches `MetaSystem` and the
FSM's terminal states), G5/G6/G7 (validation inside `internal/system/network.go`
and `internal/network`), G9 (`Transport.acceptLoop` restructure), G10 (anchor
format, schema bump, every reproduction path that reads an anchor).

---

## 4. ADR candidates

### ADR-1 — Dedicated-server process model: per-match ephemeral vs. long-lived reusable

The code currently implements *long-lived reusable* and is explicit about it:
"A server outlives its guests" (`doc/runtime.md` §1.2). `hostNetworkConfig` sets
`MaxPeers = parameter.MaxPlayers` and installs `OnAdmit = admitLateJoiner` only
for `Mode.Serves()`; `Serve` arms `lateJoins` after the lobby closes so a
departed participant can dial back into the slot its departure released
(`internal/app/serve.go:70`, `internal/app/session.go:104`). Measured: the server
stayed resident and kept ticking after its only guest left.

- **Per-match ephemeral.** One pod, one match, exit on completion, allocator
  spawns the next. Fits `restartPolicy: Never` + a Job or an allocator; makes
  the memory ceiling a per-match question rather than a per-lifetime one, which
  neutralises most of §5's uncertainty; gives every match a clean RNG session
  and a clean world. Costs a cold start per match (measured: ~12 MiB and
  sub-second to a bound listener, so this is cheap) and requires G2.
- **Long-lived reusable.** Matches the existing reconnect design and amortises
  startup. But it makes the memory question "what is the ceiling over an
  unbounded number of `:new` cycles", it accumulates whatever per-session state
  is not reset, and it has no natural drain point for a rolling update.
- **Recommendation.** Per-match ephemeral. The measured evidence (§5) says a
  fresh process is cheap and a long-lived one has a plateau, not a leak — so
  the reusable model is *safe*, but ephemeral is strictly simpler to reason
  about under a hard memory limit and it is the only model where "SIGTERM
  during a match" and "SIGTERM between matches" are the same event. Note that
  choosing ephemeral does **not** mean discarding the reconnect path: mid-run
  rejoin within one match is independently valuable.

### ADR-2 — Lobby admission and timeout policy

`waitForStartup` has no deadline; the start gate on the joiner side is
deliberately unbounded too ("The start gate carries no deadline: it is the host
waiting for the rest of the lobby, which is a human-paced wait with no bound
worth guessing at", `internal/network/session.go:388`). That reasoning is
correct for two people on a LAN and wrong for a pod.

- **Hard deadline, then exit non-zero.** Simple; the allocator retries. Wastes
  a scheduled pod when a client is merely slow.
- **Hard deadline, then start short-handed.** Requires the roster to close on
  fewer participants than `-players`, which `startHostSessionOn` currently
  refuses (`if len(offer.Participants)-1 != remoteCount`). Needs a minimum-viable
  roster concept.
- **Soft deadline that resets on each admission.** Tolerates staggered arrival;
  needs an absolute ceiling too or it is not a bound.
- **Open question the ADR must answer:** what does readiness mean between "one
  guest admitted" and "lobby full"? The Service must stop routing at full, but
  a partially-filled lobby is still accepting. This is a readiness *gate*, not a
  binary, and it interacts with whether the allocator or the Service does
  placement.

### ADR-3 — Build-identity enforcement on join

`anchorIdentity` compares a config *path*, not its content, and carries no
binary identity at all. Two builds with the same config path silently diverge.

- **Do nothing; rely on the digest probe.** `MsgStateDigest` at
  `NetworkDigestTicks` detects divergence and reports after
  `NetworkDesyncSamples`. This is detection-after-the-fact, and the doc is
  candid that past `NetworkDivergedSamples` "the two runs are different games".
- **Add a build identity field (VCS revision from `debug.ReadBuildInfo`) and
  refuse a mismatch.** Cheap, strict, and makes rolling updates a hard
  cut-over — a client on the old build cannot join a pod on the new one.
- **Add a *simulation* identity: a hash over the resolved FSM manifest, the
  system set and ordering, the event registry, and `internal/parameter`.**
  Strictly better than a VCS revision because it permits builds that differ only
  in non-simulation code (rendering, logging) to interoperate — which is exactly
  what a client/server split needs. Costs a generator change in
  `internal/manifest` and a decision about what is in the hash.
- **Also unresolved:** should `config_id` become a content hash rather than a
  path? A path proves what was asked for, not what loaded — the corpus already
  carries a fingerprint (`ContentFiles`/`Blocks`/`Lines`) and the FSM config
  carries nothing.

### ADR-4 — Memory ceiling strategy

- **Right-size the grid (G12) and set `GOMEMLIMIT` from the pod limit.**
  Addresses the dominant term directly; measured 82.3 % of heap-in-use.
- **Leave the grid and set a generous limit.** Costs ~30 MiB per pod
  unconditionally — at 60 pods/node that is 1.8 GiB spent on empty cells.
- **Cap the map size the fleet will serve, and derive the limit from it.**
  Makes the number defensible but constrains scenario authoring.
- The ADR must also decide whether `GOMEMLIMIT` is set from the cgroup limit at
  startup (needs reading `/sys/fs/cgroup/memory.max`, which is a hostile
  dependency under a read-only rootfs and a distroless image) or supplied as an
  environment variable by the manifest. The latter is simpler and testable.

### ADR-5 — Client trust boundary (implied by G8, and it gates G8's size)

The protocol is plaintext and trusted-peer by design and the README says so.
The ADR is not "should we add TLS" but "do clients reach pods at all".

- **Direct exposure.** Requires G8 in full (authentication, transport security)
  plus Phase 0, plus per-connection rate limiting. Largest scope.
- **Authenticating gateway that terminates client sessions and re-originates
  to pods on the cluster network.** The pod's trusted-peer assumption becomes
  true rather than assumed; G8 collapses to a network policy. Costs a component
  that speaks the framed-TCP protocol on both sides and a decision about whether
  it is transparent (proxy) or translating.
- Phase 0's three fixes are required under **either** option, because a
  compromised or buggy gateway is still a peer.

---

## 5. Memory report

### What was measured

Three experiments. Two were run against the live `-serve` binary, one against a
throwaway `go test` harness in `internal/app` using the existing
`NewHeadless`/`Tick`/`Reset` API (the harness file was removed after the run;
no repository code was modified by this review).

**(A) Live `-serve`, embedded scenario, default 80×24 geometry, one scripted
guest, RSS sampled from `/proc`:**

| Phase | RSS |
|---|---|
| Bound, waiting in the lobby | **12.0 MB** |
| 10 s after the guest joined | 33.1 MB |
| 60 s | 56.4 MB |
| 120 s | 59.6 MB |
| 270 s | 61.7 MB |
| 390 s (still active) | 61.75 MB (`VmHWM`) |
| ~405 s, ~2 min after the guest left | **40.1 MB** |

The guest process (`-join -script`, also headless) peaked at **95.5 MB** — 33 MB
above the server, which is the second `World` that `StageShared` builds to
resolve an installed capture (`internal/app/snapshot_stage.go:176`) and its own
30.5 MiB grid.

**(B) Headless, 160×50, 40 cycles of 600 ticks each with `Reset(false)` between
them, `HeapInuse` after 4 forced GCs:**

```
cycle  0  heapInuse= 35.828 MiB  objects=3564
cycle 10  heapInuse= 37.055 MiB  objects=4163
cycle 20  heapInuse= 38.430 MiB  objects=4756
cycle 25  heapInuse= 38.672 MiB  objects=4784   <- plateau
cycle 39  heapInuse= 38.430 MiB  objects=4791
```

**(C) 400 consecutive `Reset` cycles with two ticks between them:**

```
reset   0  heapInuse= 35.141 MiB  objects=3458
reset 100  heapInuse= 35.141 MiB  objects=4601
reset 150  heapInuse= 35.148 MiB  objects=4754   <- plateau
reset 399  heapInuse= 35.148 MiB  objects=4809
```

### Which of the three causes apply

- **(i) Genuine retention — live references surviving a game cycle: ruled
  out.** Experiment (C) is the decisive one. Over 400 resets, `HeapInuse` moves
  by 8 KiB total and the object count converges on ~4,800 by reset ~150 and
  stays there. Experiment (B) reaches the *same* asymptote (~4,790 objects) by a
  different path. A leak does not plateau, and it does not plateau at the same
  value from two different access patterns. The early slope in both — which is
  what a short manual observation sees, and what "50 MB → 51 MB within seconds
  of a fresh game" is — is a warm-up curve, not a leak.
- **(ii) Go runtime behaviour: confirmed, and it is the reported symptom.**
  The live drop from **61.75 MB → 40.1 MB roughly two minutes after the guest
  disconnected** is the background scavenger returning freed spans. RSS lags
  heap-in-use by minutes; `:new` returns the *heap* immediately and the *RSS*
  only when the scavenger next runs. This is the whole of "`:new` does not
  return allocation to prior levels".
- **(iii) Unbounded-by-design structures: none found that grow with session
  duration.** The event queue is a fixed array
  (`[parameter.EventQueueSize]GameEvent`, `internal/event/queue.go:16`). The
  flight recorder is a genuinely fixed-capacity ring — `bufI/bufF/bufB/bufS` are
  each `make(..., n*depth)` once in `bind` and indexed `seq % depth`
  (`internal/status/recorder.go:199–204`) — and installs nothing at all unless
  `-lr` or a log session is configured (`internal/app/app.go:262`). The replay
  journal is opt-in (`-j`) and streams to a rotating file. Correction retention
  is `SnapshotManifestRetention = 4` captures (~15 KiB each at the documented
  storm high water). The genetic optimiser in use is `StreamingEngine`, whose
  archive is capped at `PoolSize` with a ring proposal queue and a fixed pending
  table (`pkg/genetic/streaming.go:78–82`) — the batch `Engine` with its
  `history` slice (`pkg/genetic/engine.go:59`) is not on the game path. Flow
  fields and route graphs are sized by the map and cleared on reset
  (`internal/system/navigation.go:215`). The spatial grid is fixed-size. Macros
  and operator session state survive a plain `:new` by design and are cleared by
  `:new!` (`ResetSessionState`, `internal/engine/game_context.go:477`) — bounded
  by what an operator typed, and unreachable on a cursorless server.

### What `:new` actually resets

It **mutates the existing World**; it does not construct a fresh one.
`MetaSystem.handleGameReset` (`internal/system/meta.go:296`) captures the cursor
roster, calls `World.Clear()` — which resets per-domain entity ID counters to 1,
zeroes created/destroyed counters, clears the player roster and calls `wipeAll()`
— resets `GameState`, advances the journal run, restores map bounds (only if
`MapSizeLocal()`, so a session's bounds survive), resizes the grid, resets mode
and status, and signals the FSM reset. Every system then re-runs `Init()` from
its `EventGameResetRequest` handler, which is where subsystem caches are
invalidated: `RouteGraphResource.Clear()`, `AdaptationResource.Entries =
make(...)`, and so on.

Pointer hygiene is good and deliberate. `Store.removeAt` explicitly zeroes the
vacated tail slot "so pointer-bearing components (Genes []float64, MemberEntries,
etc.) release their references" (`internal/engine/store.go:87`);
`ClearAllComponents` uses `clear(s.dense)` for the same reason;
`removeEntity`/`removeEntitiesBatch`/`wipeAll` all `delete`/`clear` the
`componentMask` map rather than leaving zero-valued keys. What is *not* returned
is capacity: `s.dense = s.dense[:last]` keeps the backing array, `clear()` on a
Go map keeps its bucket array (`componentMask` is hinted at 16,384 entries at
construction), and `SpatialGrid.Resize` keeps `cap(g.Cells)`. That capacity
retention is exactly the plateau in (B) and (C), and it is correct behaviour for
a game that will need it again.

One latent hazard, not observed to fire: `World.RemoveComponentMask` does
`w.componentMask[e] &^= bit` (`internal/engine/world.go:140`). On a key that is
absent, Go's compound assignment *inserts* a zero-valued entry. A
`Store.RemoveEntity(e)` (without `skipMask`) on an entity already removed from
the mask map would therefore resurrect a key. `removeEntity` deletes such keys
on its next visit and `wipeAll` clears them, so it self-heals across a reset —
which is consistent with (C) showing no unbounded growth — but it is a
`delete`-shaped operation written as an insert-capable one.

### Composition of the ceiling

For a `-serve` process at rest with a live world and no guests (measured
40.1 MB RSS):

| Term | Size | Source |
|---|---|---|
| Spatial grid | **30.5 MiB** | 500 × 250 × 256 B, `NewSpatialGrid` at `NewWorld` — 82.3 % of profiled heap-in-use |
| Event batch pools, component stores, event registry, router | ~4 MiB | `NewBatchPool`, `NewStore[...]`, `RegisterType`, `Router.Register` |
| Flow field | ~1 MiB | `navigation.NewFlowField` |
| Content corpus + Go runtime | ~4 MiB | `ContentService.Contribute`, runtime structures |

Per-session and per-peer terms above that floor:

- Active simulation working set with one guest, 80×24: **~22 MB** (measured,
  40 → 62).
- `SocketPort` inbound queue: `RecvQueueSize = 256` × up to `MaxPayloadSize =
  64 KiB` = **16 MiB bound**, session-wide.
- Per-peer send queue: `SendQueueSize = 256` × 64 KiB = **16 MiB bound each**,
  plus 64 KiB read and 64 KiB write buffers. Typical occupancy is a small
  fraction of this; the bound only matters under backpressure.
- Staging world, if this instance ever installs a capture (i.e. if it loses
  authority in a succession and becomes a guest): **+30.5 MiB** and a second
  set of stores. A pure authority never builds one — consistent with the
  measured 33 MB gap between server and guest.

### The dimensioning number

**192 MiB pod memory limit, 96 MiB request, `GOMEMLIMIT=160MiB`** — for a fleet
running maps at or below 160×50 with at most four guests per match, on a trusted
network, at current `main`.

Derivation: 40 MiB measured floor + ~22 MiB measured single-guest working set +
~15 MiB for three further guests (per-peer publisher state, controllers,
retention, typical send-queue occupancy) + 30.5 MiB for a staging world
materialised by an authority handoff = ~108 MiB worst realistic. ×1.5 for GC
headroom and allocation spikes the sampler did not catch ≈ 162 MiB, rounded to
192 MiB. `GOMEMLIMIT` at 160 MiB leaves the runtime room to collect before the
cgroup kills it.

**After G12 (right-sized grid), the same fleet fits in 96 MiB limit / 48 MiB
request**, because both the floor and the staging world lose ~29 MiB each.

**Confidence.**

- *High* for the 40 MiB floor and its composition — measured directly, and the
  dominant term is a single named allocation whose size is arithmetic
  (500 × 250 × 256).
- *High* for "there is no leak across game cycles" — experiment (C), 400 cycles,
  two independent paths converging on the same asymptote.
- *Medium* for 192 MiB. It is not measured at four guests, and it is not
  measured under `config/main` with the tower/storm regions active, which is the
  documented high-water scenario and the one the soak tests target.
- *None* for any number at all if pods are exposed to untrusted clients. The
  wire-reachable ceiling today is ~1.06 GiB from G6 alone (17 sources × 64 MiB),
  plus unbounded growth from G7. **No pod memory limit is defensible until
  Phase 0 lands** — the limit would simply convert a memory attack into an
  OOMKill loop.

### Measurement protocol to make this reproducible

1. **Distinguishing (i) from (ii).** `runtime.ReadMemStats`:
   `HeapInuse` and `HeapObjects` after 3–4 forced `runtime.GC()` calls are the
   retention signal — if these plateau, there is no leak, whatever RSS does.
   `HeapReleased` and `HeapIdle` versus RSS are the scavenger signal: RSS ≈
   `HeapSys - HeapReleased` + non-heap, so `HeapIdle - HeapReleased` growing
   while `HeapInuse` is flat is cause (ii) and nothing else. Sample
   `/proc/self/status` `VmRSS`/`VmHWM` alongside, because that is what the cgroup
   actually accounts.
2. **Profiles.** `pprof.Lookup("heap").WriteTo(f, 0)` after the final GC, read
   with `-inuse_space -top`. This is what named `NewSpatialGrid` as 82.3 % in
   one command. `-inuse_objects` separates "one huge allocation" from "many
   small retained ones"; `-base` between two cycle boundaries isolates
   per-cycle retention directly. `GODEBUG=madvdontneed=1` forces eager return to
   the OS, which removes MADV_FREE ambiguity from any RSS measurement.
3. **The harness.** `app.NewHeadless` + `Tick(n)` + `Reset(purge)` is already
   the right instrument — it is the manual-clock runner, it spawns no
   goroutines, and it made all three experiments above take under three seconds
   in total. The property to assert:

   > for all cycles k beyond a warm-up of W: `HeapInuse(k+1) <= HeapInuse(k) + ε`
   > and `HeapObjects(k+1) <= HeapObjects(k) + δ`

   with W ≈ 30 cycles (measured: the plateau is reached by cycle 25 in (B) and
   reset 150 in (C)), ε on the order of 64 KiB and δ on the order of 16 objects.
   Seed-parameterise it and drive it from the existing `soakScale` profiles
   (`internal/app/soak_test.go:36`) so the wide sweep runs nightly and a bounded
   version runs in CI. Extend it with `config/main` and the tower regions — the
   `towerConfig` fixture already exists at `internal/app/soak_test.go:69` — to
   get the storm high water the 192 MiB figure is currently missing.
4. **Fleet dimensioning.** Run the same harness at each map size the fleet will
   serve and record peak `HeapInuse`; the pod limit is that peak plus the
   transport bounds above plus GC headroom, and it should be re-derived rather
   than scaled.

### Runtime knobs

Nothing in the tree sets `GOMEMLIMIT`, `GOGC` or `GOMAXPROCS` (verified by grep
across `internal/`, `pkg/`, `cmd/`). `GOMAXPROCS` is therefore the runtime
default; the module requires Go 1.26.5, and Go 1.25 introduced cgroup-CPU-aware
`GOMAXPROCS` defaulting, so it *should* track the pod's CPU limit — but that is
a property of the toolchain, not of this application, and it should be confirmed
by logging `runtime.GOMAXPROCS(0)` at startup under the real limit rather than
assumed. `GOMEMLIMIT` has no such default and must be supplied.

---

## 6. Unknowns, and the experiment that settles each

| # | Unknown | Experiment |
|---|---|---|
| U1 | Peak memory under `config/main` with the tower and storm regions active and a full roster — the documented high-water case and the gap in the 192 MiB figure. | Extend the cycle harness with the existing `towerConfig` fixture (`internal/app/soak_test.go:69`) and `-players 4`; record peak `HeapInuse` and `VmHWM` per cycle. |
| U2 | Whether the per-peer 16 MiB send-queue bound is ever approached in practice, or whether corrections are always small enough that occupancy stays negligible. | Instrument `Peer.Send` refusals and queue depth (a counter already exists: `SocketPort.refused`); run a 4-guest session with `tc netem` shaping one uplink to well below the convergence floor. `script/phase5-linkshape.sh` already does the shaping half. |
| U3 | Actual goroutine count at steady state per instance. Read-derived: 2 scheduler (`schedulerLoop`, `eventLoop`) + 1 correction pump + 1 accept loop + 1 probe loop + 3 per peer (`readLoop`, `writeLoop`, `monitorPeer`) + vlog's processor when logging is on. Fixed plus 3N. Not verified — `runtime.NumGoroutine()` was only observed in the driven harness (2), which spawns none of the above. | Add a `runtime.NumGoroutine()` field to the `serveReportInterval` log line, or take a `goroutine` pprof profile from a live server with N guests. |
| U4 | Whether SIGTERM during an active session is genuinely clean for the *guests* — the server closes listeners and peers but sends no `MsgDisconnect` goodbye, so guests observe a stream error and enter succession rather than a graceful end. | Run a 2-guest session, `kill -TERM` the server, and record what each guest logs and how long it takes to settle. |
| U5 | Worst-case SIGTERM→exit latency. Read-derived bound: `scheduler.Stop` ≤ ~100 ms (`awaitFrame` timeout) + the in-flight tick; `corrections.close` waits on the pump; `SocketPort.Close` ≤ `NetworkProbeInterval` (200 ms); `Transport.Stop` waits on `acceptLoop`, **which may be blocked inside `AcceptSession` for up to `ConnectTimeout` = 5 s** (this is G9 again); then `vlog.Shutdown(2 s)`. So ~7–8 s worst case, comfortably inside a 30 s grace period — but the 5 s term is attacker-triggerable. | Time `SIGTERM`→exit with (a) an idle server, (b) an active 4-guest session, (c) a half-open connection deliberately held mid-handshake. |
| U6 | Whether the `RemoveComponentMask` insert-on-missing-key path (`internal/engine/world.go:140`) is reachable in practice. It self-heals across a reset and experiment (C) shows no unbounded growth, so it is latent rather than active. | Add a temporary assertion (`if _, ok := w.componentMask[e]; !ok { panic }`) in `RemoveComponentMask` and run the full soak suite. |
| U7 | Whether the `-serve` process is actually startable under a fully read-only rootfs with **no** writable mount at all. Verified here with `HOME`/`XDG_CONFIG_HOME` pointed at nonexistent paths and no `-l` — it started and ran, discovery falling through to the embedded assets. Not verified against a genuinely read-only filesystem where even `os.MkdirAll` on a fallback path fails. | Run the binary in a container with a read-only rootfs, no `emptyDir`, `-d -serve`, and again with `-l` to confirm the failure is loud rather than silent (it currently is not — see G3). |
| U8 | Whether `GOMAXPROCS` genuinely tracks a cgroup CPU limit on this toolchain, or whether it must be pinned. | Log `runtime.GOMAXPROCS(0)` at startup in a pod with `limits.cpu: 500m`. |
| U9 | Whether the correction cadence degrades gracefully when a guest's uplink cannot carry the convergence floor — the code refuses such a link at admission (`admitMeasuredLink`, `internal/app/session.go:357`) and reports mid-session, but the behaviour under cluster-egress conditions (NAT, LB, variable RTT) is unmeasured. | `script/phase5-linkshape.sh` against a pod behind a real NodePort. |
| U10 | Per-match CPU at realistic map sizes and rosters. Measured here: **0.2 % of one core idle in the lobby, 4.9 % of one core with one guest at 80×24 with the embedded scenario** (97 jiffies / 20 s). The tick loop is 20 Hz (`GameUpdateInterval = 50 ms`); the frame handshake ticks at 62.5 Hz and the event loop at 250 Hz even when idle, so a pod has a floor of roughly 340 timer wakeups per second regardless of load. Not measured at 4 guests or 160×50. | Same harness as U1, with `/proc/<pid>/stat` sampling. Memory, not CPU, is the binding density constraint at current numbers: ~62 MiB versus ~0.05 core per match. |

---

## Appendix A — audit findings by area

### A.1 Dedicated-server shape

`App` construction is `New` → `init` (`internal/app/app.go:100,125`), and the
five axes are selected independently:

| Axis | Selector | `ModeServer` |
|---|---|---|
| Terminal + renderer | `Mode.Presents()` gates `initServices`' `TerminalService` registration and `initPresentation` | not built; `a.term`, `a.orchestrator` stay nil |
| Clock | `Mode.Driven()` selects `NewGameContextWithClock(..., NewManualClock())` vs `NewGameContext` | real `PausableClock` + scheduler goroutine |
| Audio | `Mode.Audio()` | not registered |
| Geometry | `Mode.OwnsGeometry()` (true only for `ModePlay`) | from `-size`, defaulting to 80×24 via `Config.Normalize` |
| Input | `Mode.OwnsInput()` gates only the terminal mouse sink; the intent pipeline is built unconditionally because it is the injection path | machine + router built, no terminal source |
| Network | `Mode == ModePlay \|\| HostAddress != "" \|\| JoinAddress != ""` | registered |

`Config.validateDriven` actively rejects flags a shape cannot honour — a colour
mode with no terminal, an audio backend with no audio service, `-speed` on a
server ("a dedicated host runs at the session's rate") — so misconfiguration is
a startup error rather than a silent no-op. This is the right structure and it
is why the primary answer is yes.

**No local cursor is required.** `hostSlot()` returns `parameter.NoPlayerSlot`
for `Mode.Serves()`; `remoteParticipantCount()` treats `-players` as a guest
count rather than a participant count; `SessionOffer.Validate` explicitly permits
exactly one cursorless participant and requires it to be the host
(`internal/network/session.go:76–90`); `configureSessionRoster` skips arming for
a cursorless local participant. N remote participants and zero local ones is the
designed case, not a tolerated one.

**No unconditional terminal I/O.** `terminal.DetectColorMode()` is called only
under `Mode.Presents()`; `fallbackColorMode = terminal.ColorMode256` is used
otherwise (`internal/app/app.go:31`). `TerminalService.Init` — the only caller of
`terminal.New(...).Init()`, and therefore the only source of termios, ioctls and
escape-sequence writes — is registered only under `Mode.Presents()`. No SIGWINCH
handler is installed anywhere; resize arrives as a `terminal.EventResize` on the
poll loop, which does not exist on a server. The one exception is on the crash
path (G15).

### A.2 Process lifecycle

- **Match end: nothing happens.** There is no game-over event and no terminal
  FSM state that ends the process. `Serve`'s loop has exactly three cases: a
  signal, the frame ticker, and the 30 s report ticker.
- **Under-filled lobby: blocks forever** (G1). Confirmed by running it — a
  `-serve -players 1` process sat at 12 MB RSS and 0.2 % CPU indefinitely,
  producing no output at all.
- **Clean exit path: partially.** `RunServer` returns `Serve`'s error, `main`
  prints it and exits 1; a signal makes `Serve` return nil and `main` exits
  `logStatus` (0, or 73 if a requested log session could not start). So the exit
  *mechanism* is sound; what is missing is anything that reaches it on match
  completion. A lobby cancelled by signal is handled cleanly
  (`errSessionCanceled` → return nil).

### A.3 Signals and shutdown

`notifySignals` registers SIGINT, SIGTERM and SIGHUP into a buffered channel
(`internal/app/signal_unix.go`); a `!unix` build returns a nil channel. The
channel is consumed in two places — `waitForStartup` during the lobby, and
`Serve`'s main loop — and in both cases it unwinds to `RunServer`'s deferred
`a.Close()`. **SIGTERM does not hit a terminal-restore handler**: `a.term` is nil
on a server and `TerminalService` was never registered, so `hub.StopAll()` has no
terminal to restore.

`Close` order (`internal/app/app.go:437`): scheduler → pending join → correction
pump/corrector → mid-run port → staging world → service hub (network, content) →
journal recorder. Every wait is bounded (see U5), worst case ~7–8 s dominated by
the 5 s accept-handshake deadline. Peers are *not* notified — no `MsgDisconnect`
is sent — so guests discover the shutdown as a stream error (U4).

### A.4 Memory

See §5.

### A.5 Network binding and identity

- **Bind address.** `-serve <addr>` flows to `Config.HostAddress`, then to
  `network.DebugConfig(RoleHost, addr).Address`, then straight to
  `net.Listen("tcp", addr)` (`internal/network/transport.go:72`). Any Go bind
  string works: `:7777`, `0.0.0.0:7777`, `[::]:7777`. **Nothing assumes
  loopback.** The bound address is read back via `port.Addr()` for logging, which
  correctly handles `:0`.
- **Address advertisement: none.** See "Explicitly not a gap" in §2. This is the
  single most important positive finding for the NodePort/LoadBalancer question.
- **The handshake.** Host: accept → `Coordinator.Assign()` allocates the lowest
  free participant ID and roster slot → send `MsgJoinOffer` carrying the
  `JoinAnchor` and the partial roster → read `MsgJoinReply`. Joiner:
  `DialSession` → decode offer → `ConfigForJoin` adopts seed, resources and the
  D-14 map latch → construct `App` → `JoinAt` verifies identity → reply. Then the
  lobby closes: the host builds the final roster, captures a tick-0 keyframe,
  and sends each joiner `MsgStart` + chunked `MsgStateSnapshot`; each replies
  `MsgReady`; the host measures each link and refuses any that cannot carry a
  whole world per convergence-floor window (`admitMeasuredLink`). The joiner
  installs the capture through the staging world and binds its roster slot.
- **Identity check: insufficient** (G10). See §2 and ADR-3.
- **Timeouts.** Accept/handshake: `ConnectTimeout` 5 s deadline, but set
  *after* `Assign()` allocates a slot, and applied serially (G9). Dial: 5 s.
  Start gate: **unbounded by design** on both sides. Idle connections:
  `DisconnectTimeout` 30 s read deadline with a 10 s heartbeat, applied per read
  in `Peer.readLoop` — so an established connection that goes silent is reaped.
  Write: 5 s.
- **Pre-handshake allocation.** `Decode` allocates `make([]byte, payloadLen)`
  with `payloadLen` a `uint16` — capped at 64 KiB per frame, which is fine. The
  real pre-handshake cost is the roster slot allocated before the deadline is
  set, plus a `bufio.Reader`/`Writer` pair at 64 KiB each once admitted.
- **What an untrusted client can currently cause:** a confirmed process panic
  (G5); ~64 MiB of allocation per source, ~1 GiB across the array (G6);
  unbounded retention in `scheduled` plus relay amplification (G7); a game reset,
  a map resize, arbitrary system enable/disable, arbitrary FSM region
  spawn/kill, and arbitrary entity spawns (G8); accept-loop starvation (G9); and
  disclosure of the host's absolute config and content filesystem paths via the
  pre-authentication `MsgJoinOffer` (G8). No arbitrary filesystem *paths in
  payloads* were found — payload types are a closed registry
  (`internal/event/registry_gen.go`) decoded into fixed prototypes, and the
  bounds-checked ones I read (`writeCursorState` validating
  `int(p.Slot) >= parameter.MaxPlayers` and cursor ownership,
  `validateSessionOffer` checking slot and ID ranges,
  `SnapshotAssembly.AddChunk` checking index/count/total consistency) are
  correct. The defects are in magnitude bounds, not in type confusion.

### A.6 Configuration and filesystem

Every runtime path, and its Kubernetes mapping:

| Path | When | Mapping |
|---|---|---|
| `$XDG_CONFIG_HOME/vi-fighter/{game,input,audio,content}/`, then each `$XDG_CONFIG_DIRS` root, then `./game.toml`, `./config/`, `./data/` | discovery, read-only, only when the corresponding `-config-*` flag is absent | **ConfigMap**, read-only mount — or eliminate entirely with `-d` |
| Explicit `-g`, `-f`, `-k`, `-config-music`, `-config-sounds`, `-config-dir` | read-only, strict (a missing explicit path is an error) | **ConfigMap** |
| `$XDG_STATE_HOME/vi-fighter/log/` — session log, status snapshots, flight-recorder dumps, `-dev` stderr capture | written **only** if a log flag is given; `-dev` defaults on for `-race` builds only | **emptyDir**, or eliminated once G3 puts logs on stdout |
| `$XDG_STATE_HOME/vi-fighter/journal/` | written only with `-j` | **emptyDir** or eliminated |
| `./log/` | fallback when no XDG state root and no user cache dir resolve — i.e. when `HOME` is unset, which is the normal case for a numeric UID with no passwd entry | **must be eliminated**: it is a relative write into the working directory under a read-only rootfs |
| `/dev/dsp` | OSS audio backend | never reached on `ModeServer` |

**Startup with absent XDG dirs: succeeds, quietly.** Verified by running with
`HOME=/nonexistent XDG_CONFIG_HOME=/nonexistent`: discovery finds nothing,
falls through to the embedded FSM bundle and corpus, and the server runs.
**Startup with an unwritable log dir: warns and continues** — `vlog.Start()`
fails, `setupDiagnostics` prints one line to stderr and sets exit 73, and the
session runs unlogged for its entire life, reporting the failure only at exit
(G3). Under a read-only rootfs this must be a clean, early, loud failure instead.

**Full config surface from flags and environment: yes, effectively.** `-d`
forces the embedded FSM and corpus and bypasses discovery entirely; every other
resource takes an explicit path. There is no setting that *requires* file
discovery. Note that the flags are the only interface — no environment variables
are read except the XDG ones consumed by `internal/paths`.

**`-check` exits non-zero on invalid config** (`resource.Check` error →
`fmt.Fprintln(os.Stderr, err)` → `os.Exit(1)`), validating the resolved FSM,
keymap, audio and content. It is the right init-container step, with the caveat
in G18.

### A.7 Health, readiness, observability

Nothing exists (G4). The minimum, in terms of state the process already tracks:

- **Liveness** = the tick counter is advancing. `status.Registry`'s tick counter
  is already maintained by `ClockScheduler`, and `App.Position().Tick` is one
  call away.
- **Readiness** = the lobby is accepting joins. This is
  `len(a.sessionRoster) < a.remoteParticipantCount()+1`, which `assignParticipant`
  already computes. **It must go false once the lobby is full**, or the Service
  keeps routing clients into a `HostAcceptor` that will refuse them *after* the
  TCP connect and a slot allocation.
- **Startup** = the listener is bound. `port.Addr() != nil`.

Logs are JSON (`Format("json")`) but file-only (G3). Telemetry is rich and
already structured — `status.Registry` holds per-region FSM state and elapsed
time, `network.peers`, `network.host_lost`, the correction cadence report
(`CadenceReport`, `internal/app/correction.go:1183`), `spatial.*` occupancy and
high-water marks, event dispatch and drop counters — and
`internal/status/snapshot.go` already walks the registry into a serialised form.
Exposing it needs a transport, not instrumentation (G16).

### A.8 Image and build

- **CGO-free for linux/amd64 across all build tags: confirmed.** No `import "C"`
  anywhere. The build-tag matrix is small and none of it is CGO-conditional:
  `!wasm && !novlog` (4 files), `wasm` (3), `unix`/`!unix` (4), `linux` /
  `unix && !linux` (2), `race`/`!race` (2). `make verify` already builds the
  `novlog` and `js/wasm` variants.
- **Audio backends:** all six are external-process or file sinks
  (`pacat`, `pw-cat`, `aplay`, `sox`, `ffplay`, OSS `/dev/dsp`), plus a null
  sink and a WAV writer. Nothing is linked; nothing is `dlopen`ed. `ModeServer`
  never registers the audio service, so `DetectBackends` never runs. Selecting
  the null sink is not even necessary.
- **Embedded assets: sufficient.** `internal/asset` embeds the FSM bundle,
  tutorial corpus and splash font; `internal/input` embeds the default keymap;
  `pkg/audio` embeds sound and pattern specs. `-d` runs entirely from them.
- **A server-only build tag does not exist** (G13). The package structure
  permits one: presentation is reached only through the `Mode` predicates in
  `App.init`, and `internal/manifest` is the single point where renderers are
  registered (`BuildRenderers`). Excluding `internal/render`, `pkg/audio` and the
  terminal module behind a `noterm` tag would need a stub `BuildRenderers` and a
  stub crash-terminal hook, and would remove the largest body of code a server
  never executes from both the image and the attack surface. Current binary:
  17.0 MB unstripped, `-ldflags="-s -w"` in the release target.

### A.9 Multi-instance density

- **No process singletons.** See "Explicitly not a gap" in §2.
- **Goroutines:** fixed 5 (scheduler, event loop, correction pump, accept loop,
  probe loop) + 3 per peer (`readLoop`, `writeLoop`, `monitorPeer`) + vlog's
  processor when logging is on. Scales with participants, bounded by
  `MaxPlayers = 16`, so ≤ ~54 at maximum roster. Read-derived; see U3.
- **Tick rate and loop shape:** 20 Hz simulation
  (`GameUpdateInterval = 50 ms`), timer-driven with drift correction and a
  `stopChan` case — **not a busy-wait**. The event loop is a 4 ms ticker that
  short-circuits on an empty queue. The frame handshake runs at 62.5 Hz
  (`FrameUpdateInterval = 16 ms`) purely to release the scheduler's render
  backpressure, drawing nothing (`App.releaseFrame`,
  `internal/app/serve.go:104`). Link probes at 5 Hz per peer. So a pod's timer
  floor is roughly 340 wakeups/s regardless of load — worth knowing at 50+ pods
  per node, and the frame ticker is the one that could be dropped to the tick
  rate on a server.
- **Measured CPU:** 0.2 % of one core idle in the lobby; **4.9 % of one core**
  with one guest at 80×24 with the embedded scenario. At current memory numbers
  a 4-vCPU / 8 GiB node is memory-bound long before it is CPU-bound: ~128
  matches by CPU versus ~40 by memory at a 192 MiB limit. Closing G12 moves the
  memory bound to ~85 matches and makes the two constraints comparable.

---

## Appendix B — what this review verified by execution

Everything below was run against a binary built from `f1d1f49`; the rest of this
document is read from source and is marked where it is.

1. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/vif` → statically
   linked ELF, 17.0 MB.
2. `vif -serve 127.0.0.1:7777 -d -players 1` with `HOME` and `XDG_CONFIG_HOME`
   pointing at nonexistent paths, no TTY → bound, ran, produced no output.
3. A `-join -script` guest joined it and ran a 6,000-tick session to completion.
4. RSS sampled from `/proc` across 420 s of that session (table in §5).
5. CPU sampled from `/proc/<pid>/stat` in the lobby and under load.
6. A throwaway `go test` harness in `internal/app` running 40 play+reset cycles,
   40 tick-only blocks, and 400 reset-only cycles with `runtime.ReadMemStats`
   and a heap profile. **The harness file was deleted after the run; no
   repository source was modified.**
7. `pprof -inuse_space -top` on that profile, which attributed 82.3 % of
   heap-in-use to `engine.NewSpatialGrid`.
8. `App.SetupLevel(1<<31, 1<<31)` → `runtime error: makeslice: len out of range`
   (G5).
