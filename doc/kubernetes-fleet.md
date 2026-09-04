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

**Status, round two.** G1, G3, G4, G5, G6, G7, G9, G15, G16 and G17 are closed.
G8, G10 and G14 remain deferred; G11 and G12 are deliberately held for reasons
recorded against them; G13 is carried forward as its own task; G2 is deferred
because there is nothing in the design to hook it to.

Round two also closed three defects found by playing the thing, tracked here as
P1–P3 alongside the original register, and diagnosed a fourth (**P4**) that is a
design decision rather than a patch — it is the top item in §3 and the subject of
ADR-6. §2 is the register, §3 is the ordered plan a fresh session should work
from. The findings in §5 and the appendices are the original review, left as
written except where a fix changed the number.

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

What was *not* ready was everything around that shape. There were no blockers but
eleven **required** gaps, and three were wire-reachable defects that made an
internet-exposed pod indefensible rather than merely under-hardened: a confirmed
remote panic, a confirmed 64 MiB-per-source allocation primitive, and unbounded
per-tick retention driven by an attacker-chosen apply tick. The lobby also blocked
forever, which in Kubernetes terms is a pod that never becomes Ready and never
terminates.

Those are closed. A `-serve` process now starts on its first guest whatever
`-players` says, admits the rest as they arrive, cannot be panicked or exhausted by
a frame, and cannot be denied by a dialer that connects and says nothing. What
remains is operational rather than structural: a pod still emits nothing on stdout
(G3), has nothing a probe can read (G4), and admits any peer that can open a socket
(G8). One structural gap remains and it is not a defect: there is no game over in
the design, so there is no "match over, exit with a status code" path for
`restartPolicy` to react to (G2, ADR-1). The architecture fits; the process model,
the determinism story and the long-lived-server lifecycle are well-matched.

---

## 2. Gap table

Severity: **blocker** = makes the architecture impossible; **required** = the
deployment is unsafe or unmanageable without it; **desirable** = cost or
hygiene. "Scope" is engineering effort on the application only.

Severity is as first assessed. **Status** is where each gap stands now: *closed*
with the change that closed it, *deferred* with the reason, or *open*.

| ID | Sev | Status | Description and disposition |
|---|---|---|---|
| **G1** | required | **closed** | Lobby blocked indefinitely: `waitForStartup` looped until `PeerCount() == remoteCount`, so a pod started with `-players 3` that received two guests never became Ready and never terminated. The fix is not a timeout but a correction to what `-players` means. `sessionCapacity` and `lobbyQuorum` split the one number that used to be both: on a server the flag is a **ceiling** on guests, defaulting to `parameter.MaxPlayers`, and the quorum is one. A server starts on its first guest and admits the rest through the mid-run gate — the same path a dropped peer returns through, and therefore already the path that has to work. An interactive `-host` is unchanged: its lobby is a party that starts together, so there quorum and capacity remain one value. `internal/app/session.go`, `serve.go`, `loop.go`. |
| **G2** | required | **deferred — nothing to hook** | There is no game-over in the design: players fall back to the start, or continue at rising difficulty, or a level ends in a victory message and restarts. `Serve` therefore has no terminal state to exit on, and inventing one would be designing gameplay from the deployment inwards. What this means for the pod is recorded in ADR-1: with no match end, the ephemeral process model has nothing to trigger it, and a long-lived reusable server is the shape the code actually has. |
| **G3** | required | **closed** | Structured logs never reached stdout: `vlog` built with `EnableConsole(false)`, so a pod emitted nothing, and an unwritable log directory warned once and then ran the whole session unlogged. `-log-stdout` selects a stdout JSON sink, exclusive with the file one — a console write into a run that owns the alternate screen is corruption rather than output, so the default is unchanged for a run that presents. A log session that was asked for and cannot start is now **fatal** (exit 73) rather than reported at exit: for a supervised process, playing a whole session unlogged loses the record it was started to produce. `internal/vlog/vlog.go`, `stub.go`, `cmd/vif/main.go`. |
| **G4** | required | **closed** | No health or readiness surface existed. `internal/probe` is a stdlib HTTP server on `-probe <addr>` with `/healthz` and `/readyz`. The two answer different questions on purpose: liveness is the tick counter advancing (sampled across reads, because a probe cannot wait for a tick; a pause resets the window rather than accumulating under it, and a clock that has not started is not a fault), readiness is whether a dial would be admitted. **Readiness goes false at capacity**, which is what the original review required — verified live: `-players 1` with one guest answers 503 `session at capacity` while `/healthz` stays 200. It binds before the lobby, because a run waiting for its first guest is a run a supervisor is watching start. `internal/probe/`, `internal/app/probe.go`, `serve.go`. |
| **G5** | required | **closed** | Wire-reachable panic. `EventLevelSetup` is `ClassShared`, so any peer could name a map whose `width*height` overflowed `int` and reach `make([]Cell, need)` — reproduced as `runtime error: makeslice: len out of range`, which under `core.Go` is `os.Exit(1)`. `engine.ClampMapSize` is now the single gate in front of the allocation, bounded by `parameter.MaxMapWidth`, `MaxMapHeight` and `MaxMapCells`. It clamps rather than rejects because the payload is replicated: a clamp reaches the same bounds on every instance, where a drop on one and an apply on another is a divergence. `World.SetupLevel` clamps before it records the dimensions, `SpatialGrid.Resize` clamps again before it allocates, and `updateGameArea` clamps the viewport the reset and resize paths derive a map from. `internal/engine/spatial_grid.go`, `world.go`, `game_context.go`, `internal/parameter/engine.go`. |
| **G6** | required | **closed** | Wire-reachable allocation primitive: `SnapshotAssembly` reserved the declared body length up front, so one 20-byte header bought 64 MiB per source and ~1.06 GiB across the array. Two changes. The ceiling drops from 64 MiB to 4 MiB — measured captures of this world are 3.5 KiB and the documented storm high water is 15.4 KiB, so that is three orders of magnitude of headroom. And the ceiling is no longer the defence: the reservation is capped at `snapshotReserve` (64 KiB) and the buffer grows with the bytes that arrive, so what a peer can make a receiver hold is what that peer sends. `internal/network/snapshot.go`. |
| **G7** | required | **closed** | Wire-reachable unbounded retention: nothing bounded a frame's `ApplyTick` against the local tick, and `applyDue` keeps what is not yet due, so a peer walking `ProducedTick` forward grew `scheduled` at line rate with `relayBatch` amplifying it. A forward window now runs *before* the epoch window — deliberately, because `epochWindow.admit` takes any tick above a source's high-water mark, so one frame naming a large one would carry that mark there and make every ordinary epoch that followed look late, on this instance and everything it relays to. The window is `NetworkApplyWindowTicks` = the join catch-up ceiling plus one convergence floor, which clears the largest gap two participants legitimately hold. `NetworkScheduledMax` and `NetworkScheduledBytes` bound what is held inside it, counted separately from a decode failure. `internal/system/network.go`, `internal/parameter/network.go`. |
| **G8** | required | deferred | No authentication and no transport security. Any TCP peer that completes the handshake still receives the anchor — including absolute host filesystem paths — a roster slot, a world capture, and the ability to inject every `ClassShared`/`ClassBus` event. G5, G6, G7 and G9 remove the crashes and the allocation primitives; they do not make an unauthenticated peer harmless. See ADR-5. |
| **G9** | required | **closed** | The pre-authentication handshake ran synchronously on the accept goroutine, so one dialer that connected and never wrote held every other join for a whole `ConnectTimeout`. Each accepted connection now handshakes on a goroutine of its own, bounded by `Config.MaxHandshakes`; past the budget a dial is refused rather than queued, because queueing would put the accept loop back behind the same read. The budget is released the moment `AcceptSession` returns — it bounds unauthenticated work, and what follows concerns a peer the roster ceiling bounds. `Stop` closes the in-flight connections before waiting, so a handshake blocked on its read no longer adds its deadline to the time between SIGTERM and exit. `App.midRunGate` serialises the admission that follows, whose `ReadyCount` wait is session-cumulative and could not otherwise tell two concurrent joiners apart. Paired with it: `Coordinator.Admit` and `app.admissionLimiter` bound how often one dialling address may be admitted, because the admission that follows a handshake reads and sends a whole world and a peer cycling through it spends one connect per capture. `internal/network/transport.go`, `config.go`, `session.go`, `internal/app/admission.go`, `host.go`. |
| **G10** | required | deferred | No build identity on join: `anchorIdentity` compares a config *path*, not its content, and carries no binary version, VCS revision, protocol version or manifest hash. Two builds that differ in simulation math but resolve the same path join and diverge silently. See ADR-3. |
| **G11** | required | **deferred deliberately** | No `GOMEMLIMIT`, `GOGC` or explicit `GOMAXPROCS`. Held on purpose: an OOMKill is the signal wanted right now. A soft heap limit converts the clearest possible symptom into a GC pause that has to be inferred from a profile, and until the ceiling in §5 is measured under a full roster and the tower scenario, the kill is the measurement. Revisit once that number exists. |
| **G12** | desirable | **deferred deliberately** | The spatial grid is a fixed 30.5 MiB allocation regardless of map size, and grow-only. The grow-only half is intended: it stops a drag-resize from reallocating the grid on every intermediate size. Multiplayer maps hold a fixed size with `crop_on_resize` false, so the case for right-sizing is weaker than the review assumed and the shape of the fix depends on gameplay testing still to come. What did change: `MaxMapCells` is now exactly the pre-allocated grid, so for any legal map the grid never grows at all and the 30.5 MiB is a ceiling rather than a floor to build on. |
| **G13** | desirable | **carried forward as its own task** | No build tag excludes terminal, renderer and audio from a server binary; all three are linked into a 17.0 MB image that `ModeServer` never uses. The package structure permits the split — presentation is reached only through the `Mode` predicates in `App.init`, and `internal/manifest` is the single renderer registration point — but it is a real refactor of that registration and wants a sweep of its own rather than a corner of this one. |
| **G14** | desirable | deferred | `SIGHUP` terminates like `SIGTERM` and `SIGINT`. Harmless in a pod; it forecloses a reload semantic later. |
| **G15** | required | **closed** | On a crash with no terminal, `HandleCrash` fell through to `terminal.EmergencyReset(os.Stdout)`, writing escape sequences into the pod log. It is now gated on `core.StdoutIsTerminal`, a `Stat` for `os.ModeCharDevice`: a run whose stdout is a pipe, a file or a container log has no terminal to reset, and the escape sequence would be the only thing it ever wrote. `internal/core/crash_handler.go`, `crash_handler_unix.go`. |
| **G16** | desirable | **closed** | Telemetry was rich and file-only. `/metrics` renders `internal/status` in the Prometheus text format — 849 series on a live server, including `vif_network_peers`, the FSM region set, `spatial.*` and the correction cadence. Nothing was instrumented for it: registry keys are renamed onto the Prometheus grammar and every value is reported as a gauge, because the counters among them are monotone only within a run and a reset re-bases them. String cells are omitted; their natural exposition is a label set this does not model. `internal/probe/metrics.go`. |
| **G17** | desirable | **closed** | `README.md` advertised three runtime shapes, omitted `-serve`, and claimed "a participant still joins only at startup" with the snapshot join described as planned. All three were wrong: there are four shapes, mid-run join and reconnect are active, and the snapshot join is implemented. Corrected there and in `doc/runtime.md`, `development.md`, `architecture.md`, `multi-player-enhancement.md`, `services-and-networking.md`, `ecs-and-events.md`, `logging-and-diagnostics.md` and `telemetry-audit.md`. |
| **G18** | desirable | **not a gap** | `-check` is intended as separate tooling and its inability to combine with `-serve` is by design. It exits non-zero on invalid config, which is what an init container needs. |

### Playtest register

Four defects found by running a real session rather than by reading. P1–P3 are
closed; P4 is a design decision and is §3's first item.

| ID | Sev | Status | Description and disposition |
|---|---|---|---|
| **P1** | required | **closed** | A server with no `-size` served a **77x21** map, and every guest adopted it however large its own terminal was. Not caused by G5's clamp, which was the first suspicion: measured, `Config.Normalize` defaults a run with no terminal to 80x24 and the margins take it to 77x21, which is far below any clamp. The joiner's acceptance now carries its own geometry (`network.JoinerReport`) and `App.adoptLobbyGeometry` applies the **first** guest's before the roster closes, so the bounds it produces are the ones the offer names and the capture contains. First rather than smallest, because guests arrive throughout the run through the mid-run gate and sizing from the smallest would mean shrinking the map under participants already playing on it. An explicit `-size` wins; a scenario that fixes its own bounds (`crop_on_resize = false`) is left alone. Verified live: 160x45 guest → 157x42 map. `internal/network/session.go`, `internal/app/session.go`, `config.go`. |
| **P2** | required | **closed** | `Config.Normalize` was not idempotent — a bug in P1's own first cut. It runs twice on the way to a session (once resolving the handshake, once inside `New`), so a flag derived from "Width is non-zero" was true on the second pass whatever the operator had said. The marker is now set only on the pass that actually fills a zero. Caught by the change not working, which is the only reason it is worth recording: a second pass over defaulted values is a shape this codebase has more than one of. |
| **P3** | desirable | **closed** | A participant was visible to another only through the effects it happened to be projecting — its shield, its ember — so one holding none was not on the map at all. `peer_cursor` draws every cursor this instance does not drive, one colour per roster slot, directly under the local cursor so an overlap resolves in favour of the one the player is steering. Deliberately not the local renderer with a loop around it: the local cursor takes the colour of what it stands on and follows the input mode, and a peer that did the same would be indistinguishable exactly when it matters. `internal/render/renderer/peer_cursor.go`. |
| **P4** | **required** | **open — ADR-6** | **A correction that rebases a guest's FSM across a state boundary strands every instance-local effect that boundary would have released.** `ImportFSM` deliberately does not re-run entry actions, and deletes regions the capture does not name. That is right for effects the capture carries and wrong for the ones it does not: an `on_enter` emitting a `ClassLocal` event produces state no capture contains. `EventGrayoutStart` and `EventDrainPause` latch in `ViewResource` and `DrainSystem` — neither snapshot-carrying — and their only release is the `on_enter` of the state the region exits through. A guest a few ticks behind never runs it. Confirmed against the uploaded log: `quasar terminate at QuasarEscalate`, then `main` never reaches `MainEscalate` again because `kills.drain` never climbs, because drains never spawn. Diagnosed and documented; **not fixed** — see ADR-6. `internal/fsm/export.go`, `internal/engine/clock_scheduler.go`, `internal/system/drain.go`, `internal/system/transient.go`, `config/main/quasar.toml`. |

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

### What has landed

**Round one — an exposed pod survives its own input.** G5, G6, G7 (input
validation, no wire-format change), then G9 and G1's transport half (the handshake
off the accept goroutine, with a budget and a per-address admission rate), then
G1's lobby semantics (`-players` became a ceiling, the quorum became one), then
G15 and G17.

**Round two — a supervised pod can be watched, and a real session plays.** G3
(stdout logging, fatal on a log that was asked for and could not start), G4 and
G16 (`-probe`: liveness, readiness, metrics), P1 (first-guest geometry), P3 (peer
cursors). P2 was a bug in P1's first cut. P4 was diagnosed and deliberately not
patched.

### What is next, in order

Ordered by what a fresh session should do first. Each entry names the shape of the
work, not just the goal.

**1. P4 — instance-local effects vs. an installed FSM position. `blocker for
multiplayer`, `high impact`, `high complexity`, needs a decision before code.**

This is the only open item that makes the game visibly wrong rather than the
deployment unmanageable, and it is first for that reason. It is a design decision
(ADR-6), not a patch — every mechanical fix considered has a case where it is
worse than the bug. Start by picking an option in ADR-6, then implement; do not
start by writing code. The diagnosis is complete and reproducible, so the expensive
part is already done. Budget: a day to decide, a day to build, and a two-instance
playtest to confirm, because the failure is a race and a unit test will not see it.

**2. G8 — authentication and transport security. `required before exposure`,
`high impact`, `medium complexity`, gated on ADR-5.**

Decide ADR-5 first: if clients never reach pods directly, this collapses from "a
handshake with credentials" to a network policy plus a gateway that speaks the
framed-TCP protocol on both sides. What is reachable today by anyone who can open
a socket: a game reset, a map resize within the clamp, arbitrary system
enable/disable, arbitrary FSM region spawn/kill, and the host's absolute
filesystem paths out of the pre-authentication offer. The memory-safety half of
this is already closed; what is left is authorisation.

**3. G10 — build identity on join. `medium impact`, `medium complexity`, ordered
before G8 because it changes the same handshake.**

`anchorIdentity` compares a config *path*, not its content, and carries no binary
version, VCS revision, protocol version or manifest hash. Two builds that differ in
simulation math but resolve the same path join and diverge silently. Wire-format
break: it changes the anchor and bumps `JournalSchema`, so it must land before
anything else touches the handshake. ADR-3 has the options; the third (a hash over
the resolved manifest, the event registry and `internal/parameter`) is the one that
lets a client and a server differ in rendering without differing in simulation.

**4. G13 — a server build tag. `low risk`, `medium complexity`, self-contained.**

`internal/render`, `pkg/audio` and the terminal module are linked into a 17 MB
binary `ModeServer` never uses. The package structure permits the split —
presentation is reached only through the `Mode` predicates in `App.init`, and
`internal/manifest` is the single renderer registration point — but it is a real
refactor of that registration and wants its own sweep. Good work for a session
that wants a bounded, mechanical task.

**5. G11 — `GOMEMLIMIT`. `low complexity`, blocked on measurement, not on code.**

Held deliberately: an OOMKill is the wanted signal until the ceiling is measured
under a full roster and the tower scenario (U1). Do U1 first; the flag is an
afternoon once the number exists.

**6. G12 — right-size the spatial grid. `low complexity`, `deferred pending
gameplay`.**

30.5 MiB of every instance is a fixed 500x250 grid. Grow-only is deliberate (it
stops a drag-resize reallocating on every intermediate size) and fixed-size
multiplayer maps weaken the case further, so this waits on gameplay testing. When
it comes, it roughly halves per-pod memory.

**7. G2, G14 — `low impact`, `low complexity`, whenever.**

G2 needs a match-end concept the design does not have (ADR-1). G14 is one line and
forecloses a reload semantic nobody has asked for yet.

### Cross-cutting notes for whoever picks this up

- **The three unknowns this round added** (U11–U13) are cheap to close and worth
  closing early: whether the admission burst bites behind a NAT, whether the map
  clamp ever reduces a real map, and whether serialising the mid-run gate matters
  when a full roster arrives at once.
- **U1 is the gating measurement** for anything memory-shaped. It is one harness
  run away — the fixture already exists.
- **Nothing above is blocked on anything else** except the two orderings stated:
  ADR-5 before G8, and G10 before G8.

**Additive vs. modifying.** Round two was almost entirely additive: `internal/probe`
is new, the peer cursor is a new renderer, and the joiner report is a new optional
field on an existing reply. Three things were not: `Config.Normalize` gained a
marker whose absence was a bug (P2), `setupDiagnostics` stopped returning a status
and started exiting, and `CursorRenderer`'s cell scan was extracted so both cursor
renderers share it. Of what remains, G10 and G8 modify the handshake, G13 refactors
renderer registration, and the rest are additive.

## 4. ADR candidates

### ADR-1 — Dedicated-server process model: per-match ephemeral vs. long-lived reusable — **decided: long-lived**

The review recommended per-match ephemeral. That recommendation assumed a match
has an end, and it does not: players fall back to the start, or continue at rising
difficulty, or a level ends in a victory message and restarts. There is no terminal
state for a process to exit on, so the ephemeral model has nothing to trigger it
and would have to invent one — designing gameplay from the deployment inwards.

The code already implements the long-lived shape and is explicit about it: "A
server outlives its guests". `hostNetworkConfig` installs `OnAdmit` for
`Mode.Serves()`, and `Serve` arms it after the scheduler so a departed participant
can dial back into the slot its departure released. After G1 that path carries the
whole roster rather than just returning peers, which is what makes long-lived the
natural model rather than a tolerated one.

What this costs, and what now answers it:

- **The memory question becomes "what is the ceiling over an unbounded number of
  cycles".** §5 answers it: measured over 400 consecutive resets, heap-in-use is
  flat within 8 KiB and the object count converges on a fixed asymptote by reset
  150. There is no per-cycle retention to accumulate.
- **A rolling update has no natural drain point.** Unresolved, and the real
  remaining cost of this decision. A server holds its guests until they leave, so
  draining means either waiting them out or dropping a live session.
- **SIGTERM during a match and between matches are different events.** Also
  unresolved: the first drops a session mid-play with no notice to its guests
  (U4), the second is free.

### ADR-2 — Lobby admission and timeout policy — **decided: quorum of one, ceiling from `-players`**

The original framing offered a hard deadline, a deadline that starts short-handed,
and a resetting soft deadline. All three answered the wrong question: they treated
the lobby as a party that must be assembled before play, and asked how long to wait
for it. On a fleet host it is not — the session exists to be joined.

The lobby's quorum is therefore one guest, and `-players` is a ceiling with a
default of the full roster. There is no timeout because there is nothing to time
out: the server waits for somebody, and everybody after the first arrives mid-run
through a path that already had to work for reconnection.

The consequences that had to be handled:

- **Readiness is not binary.** A partially-filled session is still accepting; a
  full one is not. Readiness is `len(sessionRoster) <= sessionCapacity()`, which
  G4 will read.
- **The closing window.** Between the lobby reading its roster and the mid-run gate
  arming there is an interval neither gate can serve — the lobby's offers are out,
  and the mid-run gate waits for a capture a clock that has not started never
  reaches. A dial landing there is refused with `ErrSessionStarting`, a refusal the
  dialer retries. Arming moved to after `scheduler.Start` for the same reason.
- **Churn is now the cost centre.** A lobby that admits throughout the run is one
  whose expensive path — a whole world read, encoded and sent — is reachable at
  any time. `parameter.NetworkAdmitBurst` per `NetworkAdmitWindow` per dialling
  address is what bounds it. The budget is per address rather than per identity
  because an identity is what the attack consumes and is released the moment the
  connection drops.
- **Still open:** whether the allocator or the Service does placement, and whether
  a server with zero guests for a long period should exit. The second is the
  nearest thing to a match end that exists, and it is a deployment policy rather
  than a gameplay one.

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

### ADR-4 — Memory ceiling strategy — **partly decided: cap the map, keep the kill**

The third option was taken and the first two were not, for now.

- **Cap the map size the fleet will serve, and derive the limit from it —
  taken.** `MaxMapCells` is the pre-allocated grid, so a legal map is one the grid
  never grows for and the 30.5 MiB is a hard ceiling rather than a floor to build
  on. This was a security fix (G5) before it was a dimensioning one; that it also
  settles the grid's size is the useful accident.
- **Right-size the grid (G12) — held.** The grow-only behaviour is deliberate: it
  stops a drag-resize from reallocating on every intermediate size. Multiplayer
  maps are fixed-size with `crop_on_resize` false, so the case is weaker than the
  review assumed, and the shape of any fix depends on gameplay testing still to
  come.
- **Set `GOMEMLIMIT` — held deliberately.** An OOMKill is the wanted signal right
  now. A soft heap limit converts the clearest symptom available into a GC pause
  that has to be inferred from a profile, and until U1 gives a ceiling measured
  under a full roster and the tower scenario, the kill *is* the measurement.
- Still to decide when that time comes: whether `GOMEMLIMIT` is read from the
  cgroup at startup (a hostile dependency under a read-only rootfs and a
  distroless image) or supplied as an environment variable by the manifest. The
  latter is simpler and testable.

### ADR-5 — Client trust boundary (implied by G8, and it gates G8's size)

The protocol is plaintext and trusted-peer by design and the README says so.
The ADR is not "should we add TLS" but "do clients reach pods at all".

- **Direct exposure.** Requires G8 in full (authentication, transport security)
  plus per-connection rate limiting on top of what Phase 1 added. Largest scope.
- **Authenticating gateway that terminates client sessions and re-originates
  to pods on the cluster network.** The pod's trusted-peer assumption becomes
  true rather than assumed; G8 collapses to a network policy. Costs a component
  that speaks the framed-TCP protocol on both sides and a decision about whether
  it is transparent (proxy) or translating.
- Phase 0's three fixes were required under **either** option, because a
  compromised or buggy gateway is still a peer. They have landed, so this decision
  is no longer urgent — but it is also not resolved by them: what they removed is
  the ability of a peer to crash the process or exhaust its memory, not the
  ability of an unauthenticated one to reset the game, resize the map, disable
  systems or read the host's filesystem paths out of the pre-authentication
  offer.

### ADR-6 — Instance-local effects and an installed FSM position — **open, and P4 waits on it**

`ImportFSM` places every region where a capture found it, does not re-run entry
actions, and deletes regions the capture does not name. Each of those is correct
for the effects a capture carries. None of them is correct for the effects it does
not: an `on_enter` emitting a `ClassLocal` event produces instance-local state that
no capture contains, and whose only release is the `on_enter` of the state its
region exits through.

The tension is real and neither side is obviously right. Immediate agreement with
the authority is what a correction is *for*; running the exit path is what the
local effect's lifecycle assumes. Today the first wins silently and the second is
simply lost.

- **A. Re-run the local half of the entry actions on install.** For a region whose
  active state changed, re-emit only the `EmitEvent` actions naming `ClassLocal`
  event types; skip the shared ones, whose results are already in the capture.
  Principled — it says exactly "the capture carries shared effects, local ones must
  be re-derived" — and it fixes a guest placed *into* a holding or releasing state.
  It does **not** fix the retirement case, which is the one in the log: the region
  is gone, so there is no state to re-enter.
- **B. Let the guest run its own exit instead of deleting the region.** The guest
  is behind by at most the playout lead; its own simulation would reach
  `QuasarExit` within a few ticks and release the holds correctly. Deleting the
  region is what skips that. Costs: region membership is compared shared state, so
  the two instances disagree for those ticks by construction, and the desync probe
  would have to learn that this disagreement is expected.
- **C. Capture the local hold sets and install them.** `DrainSystem`'s
  `pausedAll`/`pausedFor` and `ViewResource.Grayout` are derived from a shared,
  deterministic FSM, so every instance's copy should already agree; making them
  snapshot-carrying would make the install restore them like any other system
  state. Costs: `drain` is `Domain: "player"` with no `Snapshot:` declaration, so
  this puts player-domain state into the shared capture and needs a D-8/D-10
  ruling first.
- **D. Invert the dependency: derive rather than latch.** `DrainSystem` asks each
  tick whether any region stands in a state that pauses it, instead of remembering
  that one told it to. Automatically correct under any install, and the largest
  change — it needs the FSM to expose "which states hold which local effect", which
  the config expresses only as entry actions today.

**A + C together** cover both the entered and the retired case and are the smallest
combination that is complete. **D** is the design the codebase would probably have
if this had been noticed earlier. Whoever takes P4 should also decide whether the
storm's unscoped `EventDrainPause` and the quasar's cursor-scoped one are meant to
be the same mechanism — the release-all form clears both, which is why a naive
"release everything on retirement" fix would resume drains in the middle of a
storm.

Whichever is chosen: the failure is a race against the correction cadence, so a
unit test will not see it. The acceptance test is a two-instance session driven
long enough to escalate through the quasar twice, asserting that the guest's
`drain.paused` and `effects.grayout_active` both return to false.

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

The number is unchanged by the remediation, which is the point: G5, G6 and G7
removed the ways a peer could make the process exceed it, not the size of the
process. G12 was held (ADR-4), so the 30.5 MiB grid term stands — but
`MaxMapCells` now equals the pre-allocated grid, so that term is a ceiling that
no legal map grows past rather than a floor that a large one builds on. Right-sizing
it later would take the same fleet to roughly **96 MiB limit / 48 MiB request**,
because both the floor and the staging world lose about 29 MiB each.

The measurement was repeated against the post-remediation binary with two guests
joining at different ticks: 14.6 MB in the lobby, 31.4 MB with one guest, 44.6 MB
with two, plateauing at 63.5 MB. That is within a megabyte of the pre-remediation
figures at comparable load, so nothing in the new bounds costs steady-state memory.

**Confidence.**

- *High* for the 40 MiB floor and its composition — measured directly, and the
  dominant term is a single named allocation whose size is arithmetic
  (500 × 250 × 256).
- *High* for "there is no leak across game cycles" — experiment (C), 400 cycles,
  two independent paths converging on the same asymptote.
- *Medium* for 192 MiB. It is not measured at four guests, and it is not
  measured under `config/main` with the tower/storm regions active, which is the
  documented high-water scenario and the one the soak tests target. This is U1 and
  it is what ADR-4 is waiting on.
- *Improved, still not high, for a pod facing untrusted clients.* Before
  remediation the wire-reachable ceiling was ~1.06 GiB from G6 alone (17 sources ×
  64 MiB) plus unbounded growth from G7, and no limit was defensible at all. Those
  are closed: the assembly now reserves 64 KiB rather than a declared total, the
  schedule is bounded by count and by bytes inside a forward window, and the
  handshake budget bounds what unauthenticated connections cost. What remains is
  not a memory bound but an authorisation one (G8) — an unauthenticated peer can
  still reset the game, resize the map within the clamp, and disable systems, none
  of which a memory limit answers.

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
| U3 | Actual goroutine count at steady state per instance. Read-derived: 2 scheduler (`schedulerLoop`, `eventLoop`) + 1 correction pump + 1 accept loop + 1 probe loop + 3 per peer (`readLoop`, `writeLoop`, `monitorPeer`) + vlog's processor when logging is on, plus up to `Config.MaxHandshakes` transient handshake goroutines. Fixed plus 3N. Not verified — `runtime.NumGoroutine()` was only observed in the driven harness (2), which spawns none of the above. | Add a `runtime.NumGoroutine()` field to the `serveReportInterval` log line, or take a `goroutine` pprof profile from a live server with N guests. |
| U4 | Whether SIGTERM during an active session is genuinely clean for the *guests* — the server closes listeners and peers but sends no `MsgDisconnect` goodbye, so guests observe a stream error and enter succession rather than a graceful end. | Run a 2-guest session, `kill -TERM` the server, and record what each guest logs and how long it takes to settle. |
| U5 | Worst-case SIGTERM→exit latency. Read-derived bound: `scheduler.Stop` ≤ ~100 ms (`awaitFrame` timeout) + the in-flight tick; `corrections.close` waits on the pump; `SocketPort.Close` ≤ `NetworkProbeInterval` (200 ms); `vlog.Shutdown(2 s)`. The two attacker-triggerable terms are gone: `Transport.Stop` now closes in-flight handshake connections before waiting on them (covered by `TestStopDoesNotWaitOutAHandshakeDeadline`), and `keyframeAt` selects on the shutdown signal rather than polling out its 5 s join deadline. So ~2–3 s, well inside a 30 s grace period. Still read-derived under load. | Time `SIGTERM`→exit with (a) an idle server, (b) an active 4-guest session, (c) a half-open connection deliberately held mid-handshake. |
| U6 | Whether the `RemoveComponentMask` insert-on-missing-key path (`internal/engine/world.go:140`) is reachable in practice. It self-heals across a reset and experiment (C) shows no unbounded growth, so it is latent rather than active. | Add a temporary assertion (`if _, ok := w.componentMask[e]; !ok { panic }`) in `RemoveComponentMask` and run the full soak suite. |
| U7 | Whether the `-serve` process is actually startable under a fully read-only rootfs with **no** writable mount at all. Verified here with `HOME`/`XDG_CONFIG_HOME` pointed at nonexistent paths and no `-l` — it started and ran, discovery falling through to the embedded assets. Not verified against a genuinely read-only filesystem where even `os.MkdirAll` on a fallback path fails. | Run the binary in a container with a read-only rootfs, no `emptyDir`, `-d -serve`, and again with `-l` to confirm the failure is loud rather than silent (it currently is not — see G3). |
| U8 | Whether `GOMAXPROCS` genuinely tracks a cgroup CPU limit on this toolchain, or whether it must be pinned. | Log `runtime.GOMAXPROCS(0)` at startup in a pod with `limits.cpu: 500m`. |
| U9 | Whether the correction cadence degrades gracefully when a guest's uplink cannot carry the convergence floor — the code refuses such a link at admission (`admitMeasuredLink`, `internal/app/session.go:357`) and reports mid-session, but the behaviour under cluster-egress conditions (NAT, LB, variable RTT) is unmeasured. | `script/phase5-linkshape.sh` against a pod behind a real NodePort. |
| U10 | Per-match CPU at realistic map sizes and rosters. Measured here: **0.2 % of one core idle in the lobby, 4.9 % of one core with one guest at 80×24 with the embedded scenario** (97 jiffies / 20 s). The tick loop is 20 Hz (`GameUpdateInterval = 50 ms`); the frame handshake ticks at 62.5 Hz and the event loop at 250 Hz even when idle, so a pod has a floor of roughly 340 timer wakeups per second regardless of load. Not measured at 4 guests or 160×50. | Same harness as U1, with `/proc/<pid>/stat` sampling. Memory, not CPU, is the binding density constraint at current numbers: ~62 MiB versus ~0.05 core per match. |
| U11 | Whether `NetworkAdmitBurst` = 6 per minute per address is right in front of a NAT or a CGNAT, where a whole site is one key. It is generous for a person reconnecting and tight for a shared egress that legitimately produces many joins. | Run a fleet behind one egress address and count `MsgJoinReply` refusals carrying an admission reason. If it bites, the key has to become something the session assigns rather than something the network does, which is G10 and G8 territory. |
| U14 | Whether the peer-cursor palette reads correctly in 256-colour mode. The renderer writes RGB and the compositor down-converts, as every entity colour does, but eight slot colours chosen to be distinct in truecolor may not stay distinct through that conversion. | Run a two-guest session with `-cx` and compare the slot colours by eye; if they collide, the fix is a parallel `Palette256` set beside `RgbPeerCursor`, which `ShieldStyle` already has a pattern for. |
| U12 | Whether `MaxMapCells` = the pre-allocated grid ever clamps a map somebody wanted. Nothing in `config/` exceeds it and no terminal approaches it, but the clamp is silent by design — it has to be, because rejecting on one instance and applying on another is a divergence. | Log at `warn` when `ClampMapSize` actually reduces a request, and watch for it across the scenario set and a session on an unusually large terminal. |
| U13 | Whether serialising the mid-run gate (`App.midRunGate`) is a throughput problem when many guests arrive at once. Each admission reads and sends a whole world and waits for the joiner's confirmation, bounded by `NetworkJoinReadyTimeout` = 5 s; sixteen arriving together serialise behind that. | Dial a full roster simultaneously against one server and measure time-to-admitted for the last one. If it matters, the fix is a per-peer ready signal rather than the session-cumulative `ReadyCount` the gate currently waits on. |

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
- **Under-filled lobby: was a permanent block** (G1). Confirmed by running it — a
  `-serve -players 1` process sat at 12 MB RSS and 0.2 % CPU indefinitely,
  producing no output at all. It now starts on its first guest and treats
  `-players` as a ceiling, so there is no under-filled state to be stuck in; a
  server with nobody attached waits in the same cheap loop, which is the correct
  behaviour for a host nobody has joined yet rather than a hang.
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
- **Timeouts.** Accept/handshake: `ConnectTimeout` 5 s deadline, now applied on a
  goroutine of the connection's own and bounded by `MaxHandshakes` (G9); the
  budget is spent before `Assign` allocates anything, because `Coordinator.Admit`
  runs first. Dial: 5 s. Start gate: **unbounded by design** on both sides. Idle
  connections: `DisconnectTimeout` 30 s read deadline with a 10 s heartbeat,
  applied per read in `Peer.readLoop` — so an established connection that goes
  silent is reaped. Write: 5 s.
- **Pre-handshake allocation.** `Decode` allocates `make([]byte, payloadLen)`
  with `payloadLen` a `uint16` — capped at 64 KiB per frame. What an
  unauthenticated connection costs is now bounded on three axes: how many may be
  in flight (`MaxHandshakes`), how often one address may be admitted
  (`NetworkAdmitBurst` per `NetworkAdmitWindow`), and a `bufio.Reader`/`Writer`
  pair at 64 KiB each once admitted.
- **What an untrusted client could cause, and what remains.** Closed: the process
  panic (G5), ~64 MiB of allocation per source and ~1 GiB across the array (G6),
  unbounded retention in `scheduled` with relay amplification (G7), and
  accept-loop starvation (G9). Still reachable, and all of it G8: a game reset, a
  map resize within the clamp, arbitrary system enable/disable, arbitrary FSM
  region spawn/kill, arbitrary entity spawns, and disclosure of the host's
  absolute config and content filesystem paths via the pre-authentication
  `MsgJoinOffer`. The distinction matters for deployment — what is left is an
  authorisation problem, where what was fixed was a memory-safety one. No
  arbitrary filesystem *paths in payloads* were found: payload types are a closed
  registry (`internal/event/registry_gen.go`) decoded into fixed prototypes, and
  the bounds-checked ones I read (`writeCursorState` validating
  `int(p.Slot) >= parameter.MaxPlayers` and cursor ownership,
  `validateSessionOffer` checking slot and ID ranges,
  `SnapshotAssembly.AddChunk` checking index/count/total consistency) are
  correct. The defects were in magnitude bounds, not in type confusion.

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

### Verified after remediation

Against a binary built from the branch that closes G1, G5, G6, G7, G9 and G15:

9.  `go test ./...` and `go test ./internal/... -race` both clean.
10. `TestASilentDialerDoesNotStallTheAcceptLoop` and
    `TestStopDoesNotWaitOutAHandshakeDeadline` were run against the *unfixed*
    `transport.go` and both fail there, so they are regression tests rather than
    tests that happen to pass.
11. `vif -serve 127.0.0.1:7801 -d` with **no `-players`**, `HOME` and
    `XDG_CONFIG_HOME` at nonexistent paths, no TTY: logged
    `quorum=1 capacity=16`, waited, and reached `server running` on its first
    guest.
12. A second guest joined that running session and was admitted mid-run —
    `mid-run participant admitted, participant 3, slot 1, snapshot_tick 242,
    bytes 3517`.
13. Both guests ran to the end of their scripts and departed; the host logged
    each disconnect and continued with `remaining_peers 0`.
14. RSS across that session: 14.6 MB in the lobby, 31.4 MB with one guest,
    44.6 MB with two, plateauing at 63.5 MB.
15. Ten rapid dials from one address against a fresh server: six received a
    `MsgJoinOffer`, the seventh onward received
    `{"error":"admission: 127.0.0.1 has joined 6 times within 1m0s"}`.
16. Capture bodies measured across five configurations (embedded and
    `config/main`, 80×24 through 500×250, 409 to 1487 live entities): peak 3,545
    bytes in every case, which is what put the 64 MiB assembly ceiling at roughly
    19,000× a real capture and set the new one at 4 MiB.

### Verified in round two

Against a binary built from the branch that closes G3, G4, G16, P1 and P3:

17. `go test ./...`, `go test ./internal/... -race`, `go vet ./...`, the `novlog`
    build and the `js/wasm` build all clean.
18. A server's default geometry measured directly: no `-size` → ctx 80×24,
    viewport and map **77×21**; `-size 120x40` → 117×37; `-size 200x60` → 197×57.
    This is what exonerated G5's clamp and located P1.
19. `vif -g config/main -serve :7901` with a 160×45 guest logged
    `session sized from its first guest terminal_w=160 terminal_h=45 map_w=157
    map_h=42`.
20. `-log-stdout` emitted the session log as JSON on stdout with no file written,
    under `HOME=/nonexistent`.
21. Probe, during the lobby: `/healthz` and `/readyz` both 200 with
    `reason=clock not running`, `guests=0 capacity=16`.
22. Probe, running with one guest: both 200, `tick=159 guests=1 capacity=16`.
23. Probe, at capacity (`-players 1`, one guest): `/readyz` **503**
    `reason=session at capacity` while `/healthz` stayed **200** — the separation
    the endpoint exists to make.
24. `/metrics` served 849 series including `vif_network_peers 1`,
    `vif_drain_paused 0`, `vif_effects_grayout_active 0`.
25. The quasar failure was traced through the uploaded log and journal to
    `ImportFSM` (P4): the FSM transition list ends with
    `quasar: QuasarGoldActive -> QuasarEscalate` and a `terminate`, after which
    `main` reaches `MainGoldTimeout` rather than `MainEscalate` on every
    subsequent cycle — no drains, so no `kills.drain`, so no further quasar.
