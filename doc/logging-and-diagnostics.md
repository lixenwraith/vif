# Logging, Telemetry, and Diagnostics

vif's diagnostic surface has six cooperating layers: a structured JSON
Lines session log, a status metric registry with periodic snapshots, an
in-memory flight recorder that flushes only on a trigger, a dedicated replay
journal, a runtime stderr capture that folds Go runtime output back into
the log, and an opt-in profiler that publishes into the registry. Ordinary diagnostics join on `run` and `tick`; the journal uses its own
replay position `(run, tick, boundary)`.

This document is the authoritative reference for what is emitted, how to
control it, and what each record means. For the process lifecycle that
produces these records, see [Runtime and concurrency](runtime.md).

## 1. Layer map

```mermaid
flowchart TD
    Call["call sites: engine, systems, fsm, input, mode"] --> Vlog["internal/vlog facade"]
    Status["internal/status registry"] --> Snapshot["periodic snapshot"]
    Status --> Recorder["flight recorder ring"]
    Snapshot --> Vlog
    Recorder --> Vlog
    Stderr["fd 2 runtime output"] --> Capture["internal/core capture"]
    Capture --> Vlog
    Prof["internal/prof profiler"] --> Status
    Vlog --> Sink["lixenwraith/log sink"]
    Sink --> File["vif-log-*.jsonl"]
    Recorder -.no session log.-> Side["vif-rec-*.jsonl"]
    Journal["journaled events and own crossings"] --> JSink["a dedicated sink per journal"]
    JSink --> JFile["vif-jrn-*.jsonl"]
```

| Layer | Package | Cost when idle | Written when |
|---|---|---|---|
| Session log | `internal/vlog` | one atomic load per guarded call site | continuously, level and scope permitting |
| Status registry | `internal/status` | one atomic store per metric update | never; values are read by snapshot and recorder |
| Periodic snapshot | `internal/status` | one modulo per tick | every `StatSnapshotTicks` ticks |
| Flight recorder | `internal/status` | one atomic load/store per frozen metric per tick | on a trigger only |
| Replay journal | `internal/event`, `internal/journal`, `internal/vlog` | payload encode on each journaled event | every journaled event and own crossing, independent of diagnostic filters |
| Runtime capture | `internal/core` | one `Stat` per drain interval | when the Go runtime writes to fd 2 |
| Profiler | `internal/prof` | three atomic loads per timed call | its groups once per second of running game while `:d prof` is on; captures on command |

## 2. Record shape

Every record is one JSON object on one line. The envelope is fixed; the
payload is an open key-value map.

```json
{"time":"2026-08-06T02:48:31.124075906-04:00","level":"INFO","sub":"stat",
 "run":0,"tick":100,
 "fields":{"msg":"fsm","state":"MainDecayWait","state_index":1}}
```

| Envelope key | Meaning |
|---|---|
| `time` | RFC3339 nanosecond wall clock, assigned at emission |
| `level` | `TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR`, or `PROC` for logger heartbeats |
| `sub` | Subsystem tag; resolves to a scope (§4) |
| `run` | Reset generation; incremented by game reset, so `:new` starts run 1 |
| `tick` | Simulation tick at emission |
| `fields` | Record payload; `msg` names a diagnostic record, `type` a logger heartbeat (which has no `sub`, and `run`/`tick` 0), `ev` a journal event; the journal's other records are named by `sub` (§9) |
| `trace` | Present only on `Trace` records: a `->` joined call chain |

`fields.instance` names the runtime when a process runs more than one: `seat N`
for a bot seat, `staging` for the world a received capture is resolved in. The
process's own runtime carries none.

`-log-session-id=<id>` adds `fields.session_id` to every record emitted through
the vif logging facade. The key is absent when the flag is absent. IDs
use the DNS-safe lowercase alphanumeric-and-hyphen subset of multiplayer session
names. With file output, the active file is `<id>.jsonl`, so concurrent fleet
sessions cannot collide on the ordinary timestamp-derived filename.
`session_id` is distinct from the RNG/replay field named `session`. The deployment
field stays in the payload because `sub`, `run`, and `tick` are the stable
envelope consumed by `vif-log`; deployment components forward the JSON line
without rewriting it.

`fields.msg` is the first payload key on every diagnostic record. Viewers
index it as a column and filter on it directly. The second string field is
conventionally the record's *follow key* — `region` on FSM records, `ev` on
per-event dispatch records — so a viewer's follow-value navigation walks one
region or one event type.

### Naming a peer, counting participants

A **peer** is one end of a session link. `peer` is the identity a record is
*about*; where a record describes both ends, the far one keeps `peer` and the
local one is `local`. The emitting instance is otherwise not repeated per record
— it is stated once by `network session active` (`local`, `slot`) and in every
`session summary` — because within one log it is a constant, and across a fleet
`session_id` already names the process.

Every peer holds a participant identity ([Multi-player](multi-player.md) §1), but
anything that counts participants counts cursors, so the two are not the same
count. A dedicated host is peer 1 with `slot` 255 (`parameter.NoPlayerSlot`) and
drives none, which is why its first guest is peer 2 and the session's only cursor.
`peers` counts live transport links; `roster` counts admitted peers, the
cursorless coordinator included.

### Correlation stamps

`run` and `tick` are the writing runtime's. Each runtime writes through its own
`vlog.Log` handle, bound to its status registry and reached as `World.Log()` or
`GameContext.Log`, which holds its stamp, its `instance` tag and a mute:

| Stamp | Owner | Advances |
|---|---|---|
| `run` | `MetaSystem.handleGameReset` | once per game reset, with the replay tick rebased to zero |
| `tick` | `Scheduler.processTick` | once per simulation tick, before the tick body |

Installing a received world, a join's capture or a correction, moves both to its
header's (`corrector.adoptClockLocked`). `tick` is stamped with the tick *about to
execute*, so records emitted inside `processTick` carry the tick they describe
rather than the previous one. A played or served runtime adopts `vlog.Default()`,
which also stamps the process's own records (flags, runtime capture, a crash); a
driven one keeps its own, so a process stepping several stamps each. A replay's copies are muted until presented, and a
mute passes failures; see [Runtime and concurrency](runtime.md), Replay playback.

The render frame is not a stamp. Nothing logs from the render goroutine, so it
correlates no record, and a headless run never advances it at all; the counter
is published as the `context.frame` metric and read once per snapshot instead.
Emitters describing one instant — snapshots and recorder flushes — use
`Log.EmitSet`, which binds one explicit stamp for the whole set.

## 3. Levels

| Level | Constant | Used for |
|---|---|---|
| `TRACE` | `vlog.LevelTrace` | per-item taps: individual event dispatch, event push |
| `DEBUG` | `vlog.LevelDebug` | per-pass and per-item detail: dispatch summaries, input intents, FSM transitions, published corrections |
| `INFO` | `vlog.LevelInfo` | lifecycle and state change: startup, services, FSM region ops, status snapshots, recorder flushes, long lock holds |
| `WARN` | `vlog.LevelWarn` | recoverable anomalies: event queue overflow, a lost link, a refused or unapplied artifact |
| `ERROR` | `vlog.LevelError` | failures and runtime reports: service init/start failure, panic records, race and fatal reports |

The threshold is a single process-wide value. A long lock hold is contention, not
a defect, so it is `INFO`; FSM transitions run to thousands a session, so `-lv info`
leaves them out. `ERROR` and above **bypass the
scope mask** — a scope can silence noise but never a failure. Level still
applies to errors.

Set with `-lv <level>` at startup or `:log level <level>` at runtime. The
default when logging is enabled without an explicit level is `debug`.

## 4. Scopes

Scopes are a category mask orthogonal to level, resolved from the record's
`sub` tag. They exist so one noisy subsystem can be silenced without lowering
the level for everything else.

| Scope | Letter | `sub` tags mapped to it |
|---|---|---|
| `app` | `a` | `app`, `service`, `race`, `crash` |
| `fsm` | `f` | `fsm`, `system` |
| `event` | `e` | `event` |
| `dispatch` | `d` | `dispatch` |
| `push` | `p` | `push` |
| `input` | `i` | `input` |
| `stat` | `s` | `stat` |
| `rec` | `r` | `rec` |
| `lock` | `l` | `lock` |
| `net` | `n` | `net`: links, listeners, transport loss, the playout barrier |
| `session` | `m` | `session`, `admit`: membership, start gate, authority and succession, parking |
| `converge` | `c` | `converge`: corrections, repairs, manifests, captures staged and installed |
| `journal` | `j` | `journal`: the journal file, replay open, moves, digests and checkpoints |
| `domain` | `o` | `domain`: the D-1/D-14 audit, which runs while this scope is on at debug |
| `tap` | `t` | any unmapped `sub` |

An unrecognized `sub` falls into `tap`, so an ad-hoc debugging tap is visible
by default and is silenced as a group with `-ls -t`.

### Scope grammar

A spec is parsed against the current mask. A leading `+` adds, a leading `-`
removes, and no prefix replaces. Tokens split on `+`, `,` or space; each token
is a long name, `all`, `none`/`off`, or a run of short letters.

| Spec | Result |
|---|---|
| `all` | every scope, `dispatch` included |
| `none` | no scope; errors still emit |
| `app+fsm+stat` | exactly those three |
| `afs` | same, short form |
| `+dispatch` | add per-event dispatch to the current mask |
| `-event,push` | remove both from the current mask |
| `-event+push` | remove both from the current mask |

Set with `-ls <spec>` at startup or `:log scope <spec>` at runtime.

### Recommended masks

| Goal | Mask |
|---|---|
| Follow the FSM path | `-ls afs` |
| Diagnose an event that never arrives | `-ls +d -lv trace` |
| Watch input translation | `-ls ai` |
| Quiet run with snapshots and recorder only | `-ls asr` |
| Lock contention | `-ls al -lv debug` |
| A session's membership and convergence | `-ls amnc` |

## 5. Subsystem catalog

What each `sub` carries, and the records worth naming. The session subs narrate
whole protocols and grow with them, so their tables name the records an operator
greps for rather than enumerating every refusal.

### `sub="app"`

| `msg` | Level | Fields | Emitted by |
|---|---|---|---|
| `init started` / `init completed` | INFO | `mode` / `width`, `height`, `systems` | `app.init` |
| `seed resolved` / `scenario loaded` | INFO | `seed` / `name`, `digest`, `files` | `app.init` |
| `rng session advanced` | INFO | `session` | `Scheduler.executeReset`, once per game reset |
| `session purged` | INFO | — | `MetaSystem`, a game reset whose payload sets `purge` |
| `run restarting` | INFO | `scenario`, `host`, `join`, `rejoin` | the play loop, building the run that replaces this one |
| `scenario change requested` / `scenario served` / `scenario received` | INFO | `scenario`, `digest`, plus `host` / `bytes` / `files`, `bytes` | `:new <scenario>`; a host sending its scenario to a joiner, and the joiner |
| `shutdown started` / `shutdown completed` | INFO | — / `us` | `app.Close` |
| `signal received` | INFO | `signal`, plus `phase`, `guests`, `reason` under `-serve` | main loop, the `-serve` loop |
| `runtime capture started` | INFO | `path`, `reason`, `race` | `setupDiagnostics` |
| `logging started` / `logging stopped by command` | INFO | `path`, `level` / — | `:log on`/`off` |
| `log level changed` / `log scope changed` | INFO | `level` / `scope` | `:log` |
| `stat interval changed` / `recorder depth changed` | INFO | `ticks` | `:log stat`, `:log rec N` |
| `snapshot saved` / `capture saved` | INFO | `path`, plus `kind` | `:t save`, `:d heap` and the timed `:d` captures |
| `time scale changed` / `steps granted` / `break armed` | INFO | `scale` / `ticks` / `on`, `scale`, `expiry` | operator time control, `MetaSystem` |
| `breakpoint hit` | INFO | `on`, `cause`, `pause`, plus `scale` for a run-until | `Scheduler` |
| `breakpoint set` / `breakpoints cleared` | INFO | `on` / `count` | `:d break` |
| `profiler toggled` | INFO | `on` | `:d prof` |
| `cursor spawned` / `cursor despawned` / `local cursor bound` | INFO | `entity`, `slot`, plus `x`, `y` on a spawn | `CursorSystem` |
| `cursor spawn failed` | WARN | `reason` | `CursorSystem`, a refused spawn the FSM retries |
| `map size locked` | INFO | `map_w`, `map_h`, `viewport_w`, `viewport_h` | a resize under crop-on-resize while the map is not this instance's to crop |
| `bot seated` / `bot left` / `bot stopped` / `script completed` | INFO | the graph or script and its counters | a bot seat; an autonomous `-bot` run; a `-script` run; see [Bots](bots.md) |
| `bot not seated` / `bots not seated` | WARN | `graph`, `error` / `error` | one seat that could not join; the seating as a whole, which costs the bots, not the run |
| `presented run failed` | ERROR | `error`, plus `step` on a journal replay | a presented replay, script or bot stream failing a step; the stream ends there |
| `music recording started` / `music not recorded` | INFO / WARN | `path`, plus `error` | `-mw`, once the mixer runs |
| `probe listening` | INFO | `address` | a `-serve` run's `-probe` |

### `sub="net"`

| `msg` | Level | Fields | Emitted by |
|---|---|---|---|
| `peer link opened` / `peer link lost` / `seat link closed` | INFO / WARN / INFO | `peer`, plus `address` on the dial and `authority_lost`, `remaining_peers` on the loss; a seat this run dropped closes rather than is lost, with no alert | `Reach.dial`, `NetworkSystem.reportDisconnect` |
| `peer listener bound` / `no listening port; this participant is a leaf` | INFO / WARN | `bound`, `declared` / `address`, `error` | `Reach.AdoptListener`, `BindAdvertised` |
| `relay door noted` | INFO | `peer`, `door` | `Reach.NoteDoor`: the coordinator put a relay's door in the chain, completed from the relay's connection (Multiplayer §5.6) |
| `playout lead adopted` | INFO | `ticks`, `tick` | a change of this instance's own lead, at the tick its journaled event dispatched |
| `transport loss` | WARN | `inbound_dropped`, `outbound_refused` | `NetworkSystem`, when either count moves |
| `barrier schedule full` | WARN | `source`, `produced_tick`, `dropped`, `held`, `bytes` | the playout barrier |

### `sub="session"`

| `msg` | Level | Fields | Emitted by |
|---|---|---|---|
| `network session active` | INFO | `local`, `slot`, `coordinator`, `barrier_delay_ticks`, `peers` | this instance's one statement of who it is |
| `server running` | INFO | `address`, `capacity` | the `-serve` loop, once its session is open |
| `session summary` | INFO | `summary` | the `-serve` loop, every 30 s when the line changed; the same line `:session` prints. With peer links it names the slowest link and the correction plan's totals over all of them; with none it names neither |
| `participant evicted as too slow` / `participant evicted for what its link sends` | WARN | `peer`, `late_per_s`, `bytes_per_s`, `speed`, `window` / `peer`, `bytes_per_s`, `carried`, `speed`, `window`; each a second a second of game time at `speed` | the authority's slow and ingress policies (multi-player.md §3.5) |
| `authority handed off` / `authority lost; walking the succession list` | WARN | `term`, `authority`, `predecessor`, `roster`, `evidence_tick`, `local`, `via` / `peer`, `term`, `local`, `redial` | `Authority`; `via` is the candidate a survivor follows its host through, zero for its own link |
| `authority reached on a link of this instance's own` | INFO | `authority`, `term` | `Authority.reachedHost`: a survivor that followed its host through a relay opened a link of its own to it, which its artifacts take from here (Multiplayer §5.1) |
| `late survivor taken back` | INFO | `peer`, `term`, `places` | `Authority.takeBack`: a membership report that reached the host after the floor gives the survivor its place back, with its bots' (Multiplayer §5.2) |
| `authoring under a new term` / `following a new authority` / `continuing locally` | WARN / INFO / WARN | `term`, `seeded_from_tick`, `have_baseline` / `term`, `authority` / `held_term`, `authority`, `reason` | the candidate that took the term; every survivor that follows it; a survivor left playing alone, which reached no candidate, lost its path, or is under a pinned authority |
| `redialling the lost host` / `redialling the lost relay` / `the redial was refused` / `the redial admitted this run back` / `the redial found no place for this run` | INFO | `authority`, `dial` / `relay`, `dial` / `peer`, `error` / `peer` / `peer` | a person's run redialling the host it joined where the chain says or at its own door, or the relay it joined through at that relay's door (multi-player.md §5.1) |
| `a candidate refused the rejoin` / `a candidate took this run back to its host` / `the host holds no place for this run` | INFO | `candidate`, `error` / `candidate`, `authority` / `candidate` | `membership.rejoinThrough`: a guest whose redial could not reach its host rejoining through the candidates it walks to, one relaying for others taking it back (multi-player.md §5.1) |
| `relaying joins through a router` / `router door lost` | INFO / WARN | `router`, `door` / `router`, `error` | `membership.publishDoor`: the door this run publishes through a router was named, or its control connection ended and is being reopened (Multiplayer §5.6) |
| `path to the authority lost` | WARN | `relay`, `redial` | `Authority.redialRelay`: a participant cut off at the relay it joined through, and whether its run redials that relay's door before it walks |
| `uncommitted crossings resent at handoff` | INFO | `crossings`, `dropped` | a guest resending to the new authority the crossings no authority committed (`NetworkSystem.ResendUncommitted`); `dropped` are those past one frame, which the next correction takes back |
| `join installed the session world` | INFO | `tick`, `run`, `stage_us`, `commit_us` | `App.JoinSessionAt` |
| `opened session not taken as written` | ERROR | `error` | a run taking the world its session's opening wrote as an install takes it, which refused it: a defect |
| `mid-run participant admitted` / `join caught up` | INFO | `peer`, `slot`, `snapshot_tick`, `bytes` / `held_frames`, `caught_up_ticks`, `tick`, `lag_ticks` | the authority's mid-run gate; the joiner once its catch-up closes |
| `session parked` / `session ended` | INFO | `tick`, plus `expires_in` or `restart_in` / `phase`, `reason`, `guests`, `address` | the `-serve` loop |
| `parked session resumed` / `parked session restarted` | INFO | `tick`, plus `vacant` on the restart | the `-serve` loop: a guest arrived, or an unbounded host reset the abandoned world |

### `sub="admit"`

| `msg` | Level | Fields | Source |
|---|---|---|---|
| `peer admitted` | INFO | `peer`, `holder`, `remote`, `declared` | `membership.noteJoinerReport`, on the coordinator |

`remote` is the accepted socket's address and `declared` what the peer said it
listens on. They are here and nowhere else: it is the one value that proves a
deployment reached the pod with the player's address rather than its gateway's,
and the per-address admission budget is keyed on it. The sub sits in the `session`
scope and exists so the fleet's published stream can drop the whole category —
[`deploy/logwisp/aggregator.toml`](../deploy/logwisp/aggregator.toml) excludes it
beside `TRACE` — while the node-local file keeps it for an operator.

### `sub="converge"`

| `msg` | Level | Fields | Emitted by |
|---|---|---|---|
| `capture staged` / `capture installed` | INFO | `tick`, `streams`, `systems`, `stage_us` / `tick`, `projected_ticks`, `stage_us`, `commit_us`, `project_us`, `write_us`, `correction_entries`, `correction_entities`, `correction_cells` | `App.stageProved`, `StagedInstall.Commit` |
| `session captured` | INFO | `tick`, `bytes`, `streams`, `systems` | the authority taking a keyframe outside the cadence: a join's snapshot, a fallback |
| `correction published` / `repair applied` | DEBUG | `tick`, `keyframe`, `bytes`, `chunks`, `whole_bodies`, `peers`, `of`, `cadence_ticks`, `keyframe_period_ticks`, `encode_us` / `tick`, `pages`, `sections`, `cells` | `Corrections` |
| `correction not applied` / `repair failed its proof` | WARN | `tick`, `error` / `error` | `Corrections`; a failed proof asks the authority for a keyframe |
| `keyframe fallback sent` | DEBUG | `peer`, `tick`, `bytes` | the authority answering a peer selective repair could not serve |
| `link cannot sustain the convergence floor` / `link is carrying the convergence floor again` | WARN / INFO | `peer`, `floor_ticks`, `floor_bps`, `budget_bps` / `floor_ticks` | the relevance budget, once on onset and once on recovery |
| `no authoritative world inside the convergence floor` / `authoritative world arrived inside the convergence floor` | WARN / INFO | `age_ticks`, plus `floor_ticks`, `grace_ticks` on the loss | `Corrections`; the instance predicts alone between them |

### `sub="journal"`

| `msg` | Level | Fields | Emitted by |
|---|---|---|---|
| `journal opened` / `journal closed` | INFO | `path`, plus `run`, `tick` on a mid-run opening, `records`, `encode_failed` on a close | `app.initJournal` and `:journal start`; `app.Close` and `:journal stop` |
| `journal not opened` / `journal not reopened` | WARN | `error` | a `:journal start` the run refused or could not open (§9); a journal a replaced run carried that the next run could not open |
| `run may hold part of an opening it does not match` | ERROR | `error` | a `:journal start` whose adoption of the opening failed after it wrote: a defect |
| `seat journal opened` / `seat journal closed` / `seat journal not changed` / `seat journal not closed` | INFO / INFO / WARN / WARN | `slot`, plus `path`, or `start` and `error`; `reason` on a close as the bot leaves | the holder, for a `:journal start\|stop <slot>` its bot seat served, and for a seat whose bot left |
| `journal not closed` / `journal closed: a written world was not recorded` | ERROR | `error`, plus `path`, `tick` on the second | `app.Close` and `:journal stop`; a written world the journal could not take, which closes it there |
| `journal incomplete` | WARN | `error` | `PlayJournal` playing on past a sequence gap (§9) |
| `replay opened` | INFO | `records`, `digests`, `seed`, `speed` | `PlayJournal` |
| `replay moved` | INFO | `run`, `tick`, `from_run`, `from_tick`, `delta` | each jump of the presented tick |
| `replay diverged` | WARN | `run`, `tick`, `error` | the first digest the replay does not reproduce (§9) |
| `replay checkpoint left the run` | WARN | `error` | a restored copy not reproducing its checkpoint's digest, or a copy standing or diverging where the run did not; checkpoints are dropped |
| `replay checkpoint failed` / `replay step back failed` | WARN | `error` | a checkpoint could not be taken; a copy could not be built, so nothing replays behind |

### `sub="domain"`

| `msg` | Level | Fields |
|---|---|---|
| `component domain mismatch` | WARN | `component`, `want`, `got`, `id`, `system` |
| `shared system wrote player entity` | WARN | `system`, `id` |
| `non-replicated config read under a locked map` | WARN | `field`, `rule` |

The component audit runs only while the `domain` scope is on at debug, refreshed
per tick; the config read warns once per key per process.

### `sub="system"`

| `msg` | Level | Fields |
|---|---|---|
| `disable refused` / `dependents degraded` | WARN / INFO | `system`, `required_by` / `optional_for` |

### `sub="service"`

| `msg` | Level | Fields |
|---|---|---|
| `service initialized` / `service started` | INFO | `service`, `us` |
| `services stopped` | INFO | `services` in stop order, `us` for all of them; each one's `service stopped` is at DEBUG |
| `service init failed` / `service start failed` / `service stop failed` | ERROR | `service`, `error` |

### `sub="fsm"`

The FSM path is reported by observation hooks on `fsm.Machine`, not by
sampling state once per tick. Sampling collapses intra-tick transition chains
and cannot see background regions; hooks report every committed change in
order, for every region.

| `msg` | Level | Fields |
|---|---|---|
| `transition` | DEBUG | `region`, `from`, `to`, `via`, `index`, `max_ms` |
| `internal` | DEBUG | `region`, `state`, `via` |
| `region` | INFO | `region`, `op`, `state` |
| `region requested` / `region request failed` | INFO / ERROR | `op`, `region`, `state`, plus `error` on a failure |
| `session reset` | INFO | — |
| `import reconciled local lifecycle` | DEBUG | `actions`; a live capture install replaying the `reconcile=true` lifecycle actions its position crossed |
| `system config not applied` | ERROR | `error`; the reset re-applying the scenario's system configuration |

`via` is the triggering event name, or `Tick` for an automatic transition.
`op` is one of `init`, `spawn`, `terminate`, `pause`, `resume`. `index` is the
state's deterministic index within the loaded config, and `max_ms` is the
`StateTimeExceeds` bound on the target state when one exists.

An `internal` record covers two distinct cases, both reporting `from == to`:

- a transition declared `internal = true`, which consumes the event and runs its
  actions with no exit or enter phase and no effect on `TimeInState`;
- a transition whose `target` resolves to the state already active. A
  self-transition **degrades to internal**: exit and enter actions do not run,
  `TimeInState` is not reset, and only the transition's own `actions` execute.
  A config author expecting re-entry semantics gets a silent no-op, and this
  record is the only evidence of it.

`region` is deliberately the first string field: a viewer's follow-value
navigation walks one region's path with it.

Hooks fire **before** the exit phase, so a `transition` record precedes every
record its actions produce. A chain of transitions completing inside one tick
appears as several records sharing a `tick`.

### `sub="event"`

| `msg` | Level | Fields |
|---|---|---|
| `pass` | DEBUG | `src`, `n`, `fsm`, `sys`, `dead` |
| `queue overflow` | WARN | `dropped`, `delta` |

One `pass` record summarizes one `dispatchOnePass` call. `src` names the
caller:

| `src` | Call site |
|---|---|
| `wire` | `processTick` settling the wire artifacts due before the tick opens |
| `pre` | `processTick` initial settling |
| `post` | `processTick` settling after the FSM update |
| `close` | `processTick` settling after the systems, before the tick closes |
| `loop` | inter-tick event loop |
| `settle` | `Settle`, from a driven or headless caller |
| `input` | `DispatchEventsImmediately`, from an intent or macro, and `processTick` settling the pointer placement before the tick opens |
| `reset` | `executeReset` settling |

`n` is the batch size. `fsm` counts events some active region consumed, `sys`
counts events with at least one registered system handler, and `dead` counts
events neither consumed — the diagnostic for a config referencing an event
nothing emits, or a system that stopped registering.

`queue overflow` reports real state loss: producers outran the single
consumer and the ring evicted unread events. It also triggers a recorder flush.

### `sub="dispatch"`

| `msg` | Level | Fields |
|---|---|---|
| `ev` | TRACE | `ev`, `sys`, `fsm` |

One record per dispatched event, gated by the `dispatch` scope *and* the trace
level. This is the highest-volume record in the system — roughly 80/s in normal
play — and is off in every default configuration. `ev` is the first string
field, so follow-value walks one event type.

`sys` is the count of registered system handlers; `fsm` is whether any active
region consumed it. `sys:0 fsm:false` means the event reached nothing.

### `sub="push"`

| `msg` | Level | Fields |
|---|---|---|
| `push` | TRACE | `ev` |

Emitted by `World.pushEvent`, which every push path shares, with a four-frame
stack trace, so the record identifies the producing system. Separate from
`dispatch` so the producer side can be traced without the consumer flood.

### `sub="input"`

| `msg` | Level | Fields |
|---|---|---|
| `intent` | DEBUG | `type`, `motion`, `operator`, `special`, `mode_target`, `scroll`, `count`, `char`, `x`, `y`, `cmd`, `macro` |
| `capture refused keys` | WARN | `keys` |

One `intent` record per semantic intent produced by the input machine. `macro`
distinguishes replayed intents from physical input. `capture refused keys` names
the keys a scenario's key-capture request asked for outside printable ASCII,
which no capture takes.

### `sub="stat"`

| `msg` | Level | Fields |
|---|---|---|
| `<group>` | INFO | one key per metric in the group |
| `context` / `view` / `player` / `world` / `session` | INFO | on-demand snapshot only (§7) |

One record per metric group per snapshot. All records of one snapshot share
`run`/`tick` by construction. See §6.

### `sub="rec"`

| `msg` | Level | Fields |
|---|---|---|
| `window` | INFO | `reason`, `t0`, `t1`, `n`, `groups` |
| `<group>` | INFO | `t0`, `n`, then one key per metric |
| `recorder flushed` / `recorder flush failed` | INFO / ERROR | `reason`, `t0`, `ticks`, `records`, `us` / `reason`, `error` |

One flush is a `window` header followed by one record per group. See §8.

### `sub="lock"`

| `msg` | Level | Fields |
|---|---|---|
| `long hold` | INFO | `us`, plus a `trace` call chain |

Emitted when a world-lock hold exceeds `LockHoldWarn` (20 ms, 200 ms in a race
build). The trace identifies the holder. Also triggers a recorder flush.

Hold sampling is a per-tick decision, refreshed in `processTick`: it is active
when the `lock` scope is enabled at debug level **or** a flight recorder is
installed. The world lock is the hottest lock in the process, so it is not
probed per acquisition.

### `sub="race"`

| `msg` | Level | Fields |
|---|---|---|
| `runtime report` | ERROR | `kind`, `path`, `offset`, `bytes`, `lines`, `head`, `at` |

A pointer to a block of captured runtime output. See §10.

### `sub="crash"`

| `msg` | Level | Fields |
|---|---|---|
| `panic` | ERROR | `panic`, `stack` |

Written by the crash hook before terminal restoration, after a recorder flush and
a journal flush, and followed by a bounded flush of its own.

## 6. Status registry and periodic snapshots

`status.Registry` holds four typed metric maps — `Bools`, `Ints`, `Floats`,
`Strings` — keyed by dotted strings. Systems call `Get(key)` once during
construction and cache the returned pointer; hot paths then perform one atomic
store with no map lookup.

### Key convention

`SplitKey` maps each stable dotted key to a bounded semantic group. Keys without
a dot land in `misc`. The group becomes the record's `msg`, and the projected
short name becomes the field:

```
engine.ticks   → msg="engine"  ticks=<v>
fsm.td_main.state → msg="fsm.td_main"  state=<v>
player.0.energy.current → msg="player.0"  energy.current=<v>
player.0.weapon.rod → msg="player.0.weapon"  rod=<v>
combat.damage_attacker_cursor → msg="combat.damage.attacker"  cursor=<v>
adapt.buf_cdf_hwm → msg="adapt.buffers"  cdf_hwm=<v>
```

The projection gives per-region FSM state and each domain's `buf_*` marks
(`<domain>.buffers`) their own groups, splits the wide combat, weapon, death,
event, eye, network, snapshot and profiler domains into sub-groups and each
species' protection counters into `<species>.protection`, and gives each player
slot a card with its weapon inventory split out (`player.<N>.weapon`). Every
resulting group stays within `parameter.OverlayCardMaxEntries` (15) metrics, a
bound `TestTelemetryGroupsFitOverlayCards` holds rather than a runtime truncation,
so the same index remains navigable as overlay cards and readable as log records.
The metric registry keys themselves do not change.

### High-water marks

`_hwm` is a high-water mark in the ordinary sense: **the largest value the thing
reached since the last reset**, kept by a compare-and-swap that only ever moves
upward (`storeMax` in `internal/system/telemetry.go`,
`storeAtomicMax` in `internal/engine/position.go`). The terminology is used
correctly, and the two things worth knowing about it are what it measures and when
it is cleared.

*What it measures.* `<domain>.buf_<name>_hwm` is the peak **length** a reusable
slice reached — how much of the buffer was used, not how much it holds. Systems
register one through `newBufferTelemetry`, always for a slice that is
retained between ticks and re-sliced rather than reallocated, so the number answers
"how large did this actually have to get" and a change in it across builds is a
change in behaviour rather than in allocation strategy. `spatial.positions_hwm`,
`spatial.cell_occupancy_hwm` and `spatial.position_batch_hwm` are the same idea
outside a slice: the most entities indexed at once, the most in any single grid
cell, and the largest position batch committed.

*When it is cleared.* On `Init` — that is, per game. A reset zeroes every mark, so
what these report is the peak of the current run and not of the process. They are
also excluded from the cross-instance comparison surface (`snapshot.SharedKey`),
because a peak is a property of one instance's execution rather than of the shared
world, and two participants that agree about the world will disagree about it.

One neighbouring key is worth reading carefully because it is *not* a mark:
`spatial.max_cell_occupancy` is a plain gauge, stored rather than maximised, and it
means "the fullest cell **at its last publication**": `Position.PublishTelemetry`
computes it with the other grid gauges every `parameter.StatSnapshotTicks` ticks,
whatever period `-lt` sets. `spatial.cell_occupancy_hwm` is the same quantity
maximised over time, kept on every cell insertion. Max-across-cells and
max-across-cells-and-time are different numbers and both names are accurate; only
reading one for the other is wrong.

The bounded roster still registers every slot before `Freeze`, but inactive
slots do not produce periodic snapshot records or telemetry cards. The flight
recorder emits a player's groups when that slot was active anywhere in the
flushed history window. A bare key such as `energy.current` mirrors the slot this
instance drives (`Registry.SetLocalSlot`) and reads zero with none. It is
instance-local, so no shared FSM guard reads it (D-20); a guard names its slot.

### Integer units

Integer metrics carry their unit in the key suffix; no metadata is stored and
the log emits raw values. `status.IntUnit` and `status.FormatInt` are the sole
owners of the convention:

| Suffix | Scale |
|---|---|
| `.timer`, `.duration`, `.max_duration`, `.elapsed`, `.remaining` | nanoseconds |
| `_ns` | nanoseconds |
| `_us` | microseconds |
| `_ms` | milliseconds |
| anything else | plain count |

Display consumers — the telemetry overlay, the status bar, log viewers — resolve
through `FormatInt`. The log stores the raw integer. Where a value has to fit a
column rather than a line, `FormatCount` gives it a thousandfold suffix and
`FormatLatency` picks the time unit; both are display only.

The telemetry overlay (`:t`) shows one card per visible group. `/` filters the
cards by group or metric name, and `j`/`k` scrolls through the full height of a
clipped selected card before moving to its neighbour. Space pins a card; a new
pin turns on the HUD (`:hud`), which is anchored at the top-left, wraps whole
cards into additional columns, and reports any groups it cannot fit in a
one-line `hidden` notice. HUD width expands immediately but contracts only after
a stable narrow interval, preventing value-width jitter.

### Freeze

`Registry.Freeze` is called once from `Scheduler.Prepare`, reached by `Start` in
play and server mode, by `RunTicks`, and by a driven or scripted App's
construction. It follows `World.Seal` and precedes the first tick; every system and
renderer has registered by then. Freezing:

- closes all four maps to new keys;
- builds the group index once and caches it lock-free, so the per-tick path
  performs no generation probe;
- publishes `stat.groups` and `stat.metrics`;
- lays out the flight recorder's ring.

A `Get` on an unknown key after freeze returns a **detached cell** and
increments `stat.late`. The value is usable, so the caller does not crash, but
it appears in no snapshot and no recorder window. **A non-zero `stat.late` is a
regression**, not a supported pattern: it means something registers a metric
after the first tick.

### Periodic emission

`Registry.Tick(n)` is called once per simulation tick from `processTick`,
after the world lock is released and after every tick-owned write has
committed. It:

1. samples the flight recorder;
2. mirrors `stat.late`;
3. emits the periodic snapshot when `n` is a multiple of the configured period
   and the `stat` scope is enabled at info level;
4. drains a pending recorder flush request.

The snapshot emits through `Log.EmitSet` with one explicit stamp, so all
records of one snapshot name the instant they describe.

A snapshot is stamped with tick *n*, but it is not a barrier. The world lock is
released before `Registry.Tick` runs, so the event loop, the input path and the
render goroutine can commit between the end of the locked tick body and the
read. A value written inside the locked body is exact for tick *n*; everything
else reads "at or after tick *n*" — `event.dispatches`, `event.queue_len` and
`engine.fps` in particular, which is why they move at points that do not line up
with tick boundaries. The recorder sample shares this call and the same
guarantee.

Default period is `parameter.StatSnapshotTicks` = 200 ticks (10 s at a 50 ms
tick). The flight recorder holds fine-grained history, so the periodic
snapshot is a coarse heartbeat rather than the primary time series.

Override with `-lt <ticks>` at startup or `:log stat <ticks>` at runtime; `0`
disables periodic emission entirely without affecting the recorder.

## 7. On-demand snapshot

`:t save` writes a standalone file `vif-snap-<timestamp>.jsonl` into the log
directory through a second logger instance, independent of the session logger's
state, level, and scopes. The records are captured while command mode holds the
world lock, so the values are one coherent tick, and stamped with that tick.

The file contains the full registry snapshot plus five records that have no
registry mirror, emitted by `GameContext.SnapshotContext`:

| `msg` | Contents |
|---|---|
| `context` | map size and the crop-on-resize flag |
| `view` | mode, screen size, game offsets, viewport, camera, color mode; the local cursor's entity, slot, outstanding prediction depth, position and ping bounds |
| `player` | roster count |
| `world` | shared and local entity created/destroyed counts, and system count |
| `session` | frame, pause, macro recording/playback, mouse preferences, and auto-fire |

`session` is operator-owned and omitted by `App.SnapshotSimulation`, which a
replay comparison reads. `App.SnapshotShared`, the cross-instance surface, also
drops `view`, which is this instance's binding. The full snapshot keeps all five
for `:t save` and perturbation diagnostics.

The file is written off the lock on its own goroutine, so a live session's tick
never waits on disk; the status bar reports the path once the drain completes.

## 8. Flight recorder

Per-tick snapshots at 20 Hz are affordable to *record* but not to *write*. The
recorder holds the last N ticks of the full registry in memory and writes only
when something goes wrong.

A run installs one of `parameter.RecorderDepthTicks` (200) ticks when a session
log is running as it is built; `-lr`/`Config.RecTicks` sets the depth, `-lr 0` (a
negative `RecTicks`) installs none, and a bare run installs none, so no trigger
writes a file nobody asked for. When a replay presents another copy, that copy
takes over the recorder with its depth and FSM trigger (`Registry.TakeRecorder`);
its window starts empty.

### Storage

Slot-major: one tick's values are contiguous across four kind-partitioned
planes — `[]int64`, `[]float64`, a `[]uint64` bitset for bools, `[]string` for
strings — plus a parallel tick-stamp array. The per-tick write is a linear
walk of the frozen metric set with no allocation and no lock.
`AtomicString.Load` returns an existing header, so storing a string retains
rather than copies.

A `head` counter is stored **after** the slot's writes, so a reader never
observes a torn slot.

Memory grows linearly with depth and with the declared metric set; per-region
FSM telemetry is included because it is registered before `Freeze`.

### Sampling

Sampling happens inside `Registry.Tick`, on the tick goroutine, after the world
lock is released. The recorder and the periodic snapshot share the same frozen
group index — one index build serves both.

### Triggers

`Registry.Trigger(reason)` is callable from any goroutine; `status.Trigger`
reaches the most recently installed recorder from a path that holds no registry,
the runtime-report drain. It sets an atomic flag and a reason; the tick goroutine
performs the flush at the end of `Registry.Tick`, off the world lock and off the
caller's stack.

No other path may call `Recorder.Flush` directly. The ring is written by
`sample` on the tick goroutine with no lock, so a flush from the input or
command path would race it — and the string plane tears a header, not just a
value. `:log rec flush` therefore *requests* a flush and returns; the window
appears on the next tick.

| Reason | Source |
|---|---|
| `event.dropped` | queue overflow detected in `processTick` |
| `lock.hold` | world-lock hold exceeding `LockHoldWarn` |
| `race` | a `sub="race"` runtime report was drained |
| `crash` | the panic path, via `vlog.SetCrashFlush` |
| `manual` | `:log rec flush`, and the fallback when no reason was set |
| `break` | a run-until breakpoint matched or expired, or a standing `:d break` matched |
| `fsm:<region>` | an FSM transition, **disabled by default** |

Only the newest reason survives: repeated triggers before the tick goroutine
drains collapse into one flush carrying the last reason set.

Flushes are throttled to one per `depth/4` ticks; suppressed requests increment
`rec.skipped`. A repeating fault therefore cannot flood the log. The throttle
applies to `manual` as well — a `:log rec flush` issued within `depth/4` ticks of
the previous flush is counted and writes nothing. `rec.skipped` also counts a
flush discarded because the `rec` scope or the level suppressed it, so a rising
`rec.skipped` with `rec.flushes` flat means windows are being requested and
thrown away: check the scope mask before the throttle.

The FSM trigger is off by default because transitions are frequent enough to
flush continuously, which defeats the purpose. Enable per session with
`:log rec fsm on`.

The crash path is the one exception to the tick-goroutine rule.
`Recorder.CrashFlush` writes from the panicking goroutine and may race a
concurrent `sample`; that is deliberate, since the process is ending and the
alternative is losing the window. It is still bounded by the `flushing` flag, so
it cannot interleave with a normal flush. It is a no-op without a running
session log — opening files during a panic is worse than losing the window.

### Flush encoding

One `window` header record plus one record per metric group. Within a group,
a metric whose value never changed over the window is emitted as a **scalar of
its native type**; a metric that varied is emitted as a compact string:

| Kind | Varying form |
|---|---|
| int | comma-separated decimals |
| float | comma-separated shortest round-trip |
| bool | a run of `0`/`1` digits, one per sample |
| string | comma-separated |

`,` is the series separator and is reserved: an embedded comma becomes `;` in
**both** forms, so a constant string that happens to carry one is not
indistinguishable from a two-sample series. A decoder separates constant from
series by JSON type for int, float and bool; for string both forms are JSON
strings, so it splits on `,` and compares the element count against `n` — one
element means the column was constant.

`t0` names the first tick and `n` the sample count, so position *k* in a series
belongs to tick `t0+k`.

`msg`, `t0` and `n` are reserved field names in a group record, as are `reason`,
`t1` and `groups` in the `window` header. A metric key of the form `<group>.t0`
or `<group>.n`, or a group named `window`, collides with the envelope and
silently overwrites it. The same reservation applies to the `stat` records of an
on-demand snapshot, where `context`, `view`, `player`, `world` and `session` are
taken (§7).

```json
{"fields":{"msg":"window","reason":"event.dropped","t0":180,"t1":379,"n":200,"groups":107}}
{"fields":{"msg":"drain","t0":180,"n":200,"count":"10,10,9,9,...","pending":0}}
{"fields":{"msg":"storm","t0":180,"n":200,"active":false,"circle_count":0}}
```

Constant collapse typically reduces well over half the groups to one-line
scalars. A 200-tick window of an idle headless game is about a hundred records,
comfortably inside the sink's 8,192-record queue.

### Destination

The flush targets the session log when one is running. With no session sink and
a configured log directory, it opens a standalone
`vif-rec-<timestamp>.jsonl`, drains, and closes. `-lr` implies `-l`; after
`:log off`, the recorder remains active and later triggers use this sidecar path.
An embedder can also set `Config.RecTicks` without starting a session logger;
its windows reach disk only once `vlog.Configure` names a directory, and with
neither a sink nor a directory a flush writes and counts nothing.

The native default is `$XDG_STATE_HOME/vif/log/` (normally
`~/.local/state/vif/log/`), overridden by `-l=DIR` or `paths.log` in `vif.toml`.
Runtime stderr captures and on-demand snapshots use the same diagnostic
directory.

Both files carry the same envelope and open together in one viewer instance,
correlated by `run` and `tick`.

## 9. Replay journal

`-j[=DIR]` / `-journal[=DIR]` asks `internal/journal.Recorder` to attach an
`event.Journal` to the event queue and open a dedicated `vif-jrn-*.jsonl`
logger, one per journal the process keeps: its own run's and each bot seat's. A
journal opened within the second another was takes a `-2`, `-3`… suffix, and the
process prints every path it opened when it exits. The Recorder owns attachment,
final counters, and file drain; an injected
in-memory `journal.Capture` remains caller-owned. Capture records every event
whose origin is `Journaled` (all but `system` and `device`) and every own crossing
whatever its origin. The journal logger is separate from the session logger and
has no level or scope gate: `:log off`, `-lv error`, or `-ls none` cannot silence
a capture.

Its default is the separate `$XDG_STATE_HOME/vif/journal/` directory, overridden
by `-j=DIR` or `paths.journal` in `vif.toml`. Only a platform with no resolvable
user-state or cache location falls back to `./vif/journal/`. See
[External filesystem layout](filesystem-layout.md).

`event.Origin` identifies the producer, not the consumer:

| Origin | Producer path |
|---|---|
| `system` | Simulation-internal event; journaled only as an own crossing the barrier applied (it then carries `crossing`). |
| `input` | Physical key/mouse input through the mode router, including resize. |
| `macro` | Macro playback and auto-fire. |
| `command` | Ex command line, including a typed `:region`, and the configuration menu's forms. |
| `network` | Remote producer. |
| `debug` | Harness or out-of-band APIs such as `App.Region`. |
| `session` | Roster/lifecycle observation from the session layer, and what it settled: kill confirmations and the session state the simulation reads. |
| `device` | This machine's own output, the audio mute; never journaled. |

The dispatcher does not branch on origin. The value exists for APM admission
and replay capture; replay injects the same event with the recorded origin.

### Position lattice

Each record is stamped synchronously at queue push with
`(run, tick, boundary)`:

| Counter | Advances when |
|---|---|
| `run` | `EventGameResetRequest` rebuilds the world and rebases tick to zero. |
| `tick` | The next tick body opens, before its pre-settle/FSM/system phases. |
| `boundary` | A non-empty explicit settle group completes; it resets to zero at each tick/run and written world. |

A record `(R,T,b)` was produced after tick `T` of run `R` completed, in the
`b`th settle group since. Separate boundaries matter: merging two injected
groups can change their order relative to system events. A tick's own events
settle inside it, before its digest, so the replay's tick reproduces them. Every
between-tick dispatch is a whole settle, the interactive event loop's included,
so a group that held no record is one settle a replay reproduces before the next
group it reaches. A journaled event is therefore never pushed before a tick
body's last settle: the session layer raises its records at the epoch flush,
after the closing settle. Nor is one pushed by a handler: a pending pointer
placement crosses before the next tick or ahead of the next journaled push, not
from a crossing some handler raises, which a replay would dispatch ahead of
that handler's earlier events. A `:new` or crossed reset is recorded in run `R`,
so replay follows that boundary rather than fabricating it; one the scenario
requests is derived, so replay plays run `R` on until it opens `R+1`. Either
ends the tick it lands in, so run `R+1` counts from tick 1 alike.

### Record and anchor shapes

Journal record fields are:

| Field | Meaning |
|---|---|
| `jseq` | Dense journal sequence; a gap means exactly one lost record. |
| `seq` | Original queue slot/order; legitimately sparse because system events share the queue. |
| `jrun`, `jtick`, `boundary` | Replay lattice position. |
| `origin`, `domain`, `ev` | Producer class, the domain the event was pushed in, and registered event name. |
| `payload` | The registered payload prototype in its generated codec (`event/payload_gen.go`), base64; absent for a nil payload. |
| `encode_err` | Why a payload could not be captured, absent otherwise; replay refuses that record. |
| `crossing` | This instance's own crossing sequence the barrier applied it under, absent otherwise. Every such crossing is a record, a system-derived one included, at the place it interleaved with its peers'; a replay's barrier applies none of its own, and restores the identity, by which an own placement settles the D-18 queue. |

An anchor is emitted when capture opens, after reset, and every 600 ticks so a
rotated file soon receives a self-description. It carries:

- journal schema, dense sequence position, root seed, RNG session, and exact
  speed ladder token;
- current `run`/`tick` plus `start_run`/`start_tick`, which remember where the
  journal originally opened;
- the scenario's name and the digest of its canonical form, and the content
  source and optional pinned file;
- the loaded corpus fingerprint (`content_files`, `content_blocks`,
  `content_lines`), because a path alone does not prove what content loaded;
- fixed tick interval and terminal-equivalent `width`/`height` used by the
  simulation;
- the map latch (`map_w`, `map_h`, `crop_on_resize`) and the cell aspect
  (`cell_aspect`), whether the map was session-shared (`session_shared`), and
  the local `slot`.

Closing the journal writes an `end` record, `jrun`, `jtick` and `boundary`, so a
replay also plays the input-free ticks after the last record.

Each line names its kind by `sub`: `journal` for an event record or note,
`anchor`, `capture` for a written world (below), `digest` and `end`. A rotation
renames the full file to `vif-jrn-<time>_<yymmdd_hhmmss>.jsonl` (§13).
`journal.Load` accepts a journal's files in any order, sorts records and anchors
by `jseq`, written worlds by `cseq` and digests by position, removes overlap and
keeps the latest `end`; the first anchor is the earliest. `Set.CheckDense`
reports the first `jseq` gap. `app.PlayJournal` and `app.VerifyJournal` take the
same file list, while `-replay` passes one file. `PlayJournal` warns
`journal incomplete` and plays on past a gap; `VerifyJournal`
(`-replay -headless`) refuses one.

`ConfigFromAnchor` rebuilds the run's configuration: it checks schema, tick
interval and a non-zero seed. The scenario is asked for by name, a lookup hint
only, so a journal recorded against one that is not installed replays under the
same `-config-dir` the run used. After construction, `App.VerifyAnchor` proves
what loaded is what ran: schema, seed, RNG session, scenario digest, tick
interval, the content source, pin and fingerprint, and geometry.

### Opening mid-run

`:journal start` (Diagnostics → Replay journal) opens a journal where the run
stands, solo, hosting or joined, and the run plays on as it was. Its replay boots
afresh from the anchor (non-zero `start_run` or `start_tick`), so the journal's
first `capture`, flagged `opens`, carries everything that boot would otherwise
decide: the live shared world, the operator state (`mode`, `paused`, `held` and `start`, the
toggled systems that are `off`, the `slot` input follows) and the run's own side (`own`).
That is its player domain under the same entity IDs, the camera, APM and gesture
tracking, timed effects, the player domain's streams, the cells its input has in
flight, each system's own state (`engine.LocalStateSaver`), the corpus position,
the event tallies, every status cell but this instance's own, and the order the run holds both domains'
entities in by store and by cell (`engine.WorldOrder`), which systems visit them
in and a re-insertion would not keep. The opening writes it over a muted
headless twin of the boot as a replay does, and the run adopts the twin's result,
itself, before the journal attaches; only the transport, audio and music keep
their own state. A start is refused while a new game is starting, between a
reset's cleanup and its boot spawn. A replay writes the opening as any written
world, binds its slot, writes the own side over what that settle derived, takes up
the operator state and engages its crossing barrier at once, as the run's was;
`app.loadJournal` refuses a mid-run journal whose files lack the opening. A `capture`
a join into a held session writes carries `held` and `start` too, the hold the world
it installed landed in, so a replay holds there as the run did.

`:journal stop` closes the journal once the run is back in play, after its command
and any menu that paused the run, so a replay ends in play rather than on the command
line or the menu; the close writes the `end` record and releases the file. Asks the
loop has not served apply in order: a start after a stop opens another file once
the stop has closed, and a stop before a start is served cancels it. With a slot, `:journal start|stop <slot>` does the
same for a bot seat this process runs: the seat's loop opens its journal in its
own run and the holder's status line reports the path. The journal closes, its
`end` written, when the seat leaves, whether dropped, quitting or left with no
session, and the holder says so; a start the seat never served is refused then,
and one asked of a leaving seat at once. A remote participant's run is not
journaled here, since its player domain never leaves its machine. A run
journaling, or asked to start, when a scenario change or a join replaces it
journals on in the next run's file from its start, as the next run's status line
says, and plays on unjournaled if that file cannot open; its seats' journals
close with it, named on the same line. An opening too large for one journal line
(`parameter.JournalLineBytes`) is refused before the run is touched; a later
written world it cannot record closes the journal where it still verifies, as
the status line says.

### Written worlds and notes

A participant writes worlds it did not simulate: the capture its join installed
and each correction after it, and a host the world it opened its session on — the
owners, latch and lead that `:host`, and `-host` once its clock runs, write. A
journal opened mid-run begins with one more, its opening.
Each is a `capture` record carrying `jseq` (the records before it), `cseq` (the
writes up to it, since two with no record between share a `jseq` and can match in
every other field), its lattice position, the local `participant`, the
`authority`, `barrier` (whether the write left the crossing barrier on) and, as
base64 `body`, a `snapshot.WrittenDelta`: what the write changed against the world
held just before it, sealed with the written world's integrity hash. Replay
rebuilds it from its own world at that place, refuses a rebuild whose hash differs
(it diverged before the write) and installs it under that identity, so a guest's
journal replays from its join. The install leaves the replay where the write left
the run: on the barrier it left, which a host's write engages in the same hold (else
crossings dispatched after it apply at once and again where the journal records
them), and at settle group 0, a host's write included. A host takes its own write
as an install takes it, journaled or not, so what an install derives from the world
it writes (the spatial gauges, what carriers publish as they load) stands on the run
as on its replay. A write can land between
ticks while records of its settle group are still queued, as a seat added from
another goroutine does between a keystroke's push and its settle. They dispatch with
what is pushed at group 0 before the next settle, so replay pushes them on both
sides of the write and settles them once, after. A correction that changed nothing
costs a few hundred bytes. Owner syncs and kill confirmations are records; the
authority's worlds that proved them are not.

Two events are notes, journaled and applied by replay but never dispatched:
`EventCursorPredicted`, the D-18 placement a keystroke made, and
`EventSessionState`, the session the simulation reads (`engine.SessionState`:
live, this participant, the authority, whether its only peers are its own seats,
and the peer count the network badge shows). `World.LatchSession` reads it from the
transport under the world lock when a tick opens, a transport attaches or goes, the
authority moves or a link comes or goes, and notes a change; prediction derives from
it. A replay holds no transport: it follows these notes, and a written world names
the participant and authority it is installed as, since a join writes before its
transport attaches. A replay settles only a group that queued an event, as the
recorded run did.

Every `parameter.DigestIntervalTicks` (20) ticks the run writes a `digest` record,
`jrun`, `jtick` and four hex hashes from `snapshot.DigestWorld` over both domains:
`positions`, `kinetics`, `combat` and `entities`. It is taken last in the tick
body, after the tick's closing settle; a replay compares
it after the tick that reaches the same place, before injecting that tick's
groups. The first one it does not reproduce names the tick, the classes that
differ and the last digest that matched, so a replay that left its run is caught
within 20 ticks instead of at the next written world, which a host journal never
has. A digest costs about 75 µs at the map limit, against a 12 ms tick, and adds
about 5% to a bot's journal. `-replay <file> -headless` verifies a journal
unattended and exits non-zero at the first digest it does not reproduce.

`app.PlayJournal` presents the replay with fixed viewer controls rather than
the keymap. [Runtime and concurrency](runtime.md), Replay playback, covers its
keys, seeking, how it goes back, sound, and how a replay logs.

The same package owns the replay timeline and authored-script timeline. A
`-script` event is emitted with `OriginDebug`; semantic intents retain the
ordinary input/command origin selected by the App router. Combining `-script`
with `-j` therefore produces an ordinary replayable journal of what the script
actually caused, rather than recording the script file itself.

## 10. Runtime output capture

Go runtime diagnostics — race reports, `fatal error:`, unrecovered panics from
goroutines outside `core.Go` — write directly to file descriptor 2. Inside the
alternate screen that output is unreadable and usually lost.

`-dev` redirects fd 2 to `vif-stderr-<timestamp>.log` **before** the terminal
enters the alternate screen, then polls the file every
`parameter.DevDrainInterval` (500 ms). Each complete block becomes one
`sub="race"` pointer record at ERROR level:

| Field | Meaning |
|---|---|
| `kind` | `data race`, `fatal`, `panic`, `summary`, or `output`, classified from the first line |
| `path` | the capture file |
| `offset`, `bytes`, `lines` | block location and size within it |
| `head` | first line, truncated to 200 characters |
| `at` | innermost vif stack frame, empty when absent |

The text stays in the capture file; the log holds a pointer. Blocks are split
on the race delimiter and an incomplete trailing line is held back, so a block
is never split across two records.

At shutdown the drain runs once more, fd 2 is restored, and an empty capture is
removed. `-dev` defaults **on** for race builds and is disabled with
`-dev=false`. The redirect needs a unix platform (`redirect_linux.go`,
`redirect_unix.go`); elsewhere (`redirect_other.go`) the run prints
`runtime capture disabled` and plays on without it.

## 11. Profiler

`:d prof` times every system `Update`, every system's event handlers, every
renderer and the engine phases around them, and samples the process, once per
`ProfWindow` (1 s) of running game. Off, a timed call costs three atomic loads,
since a CPU capture's labels and an execution trace time calls too; on, about
90 ns, plus two ~400 ns reads of the allocation counter around a leaf. A window
closes on a tick and never spans a pause. It publishes into
activity-gated registry groups, so their cards exist only while it runs and reach
the HUD, snapshots, the recorder, `vif-log` and `/metrics` like other telemetry.
The periodic snapshot is ten windows apart, so while the `stat` scope is on at
info each closed window also logs these groups on its own, in the snapshot's
record shape:

| Group | Contents |
|---|---|
| `prof` | Mean µs per tick of `tick`, `tick_wait` (lock acquisition), `fsm`, `dispatch` (every pass, in a tick or between), `systems`, `status`; per frame of `frame`, `frame_wait`, `render`, `flush`; `tick_max_us`, `frame_max_us` |
| `prof.top` | The ten costliest leaf timers as `<kind> <name> <mean> <share of wall time>`; kinds are `sys`, `evt`, `draw` and `core` |
| `proc` | CPU user/sys percent of one core, peak RSS, Go-mapped and live-heap MiB, allocation MiB/s and objects/s, GC cycles/s, goroutines, block reads and writes/s, voluntary and involuntary context switches/s; the operating-system counters read zero off unix |

Starting it pins `prof` and `prof.top`; `:d` opens the whole report, every
module ranked by share with its mean, max, calls per second and allocation rate.
That rate is an estimate: the counter it reads is process-wide and advances a
span of small objects at a time. The groups measure the process: a reset does
not clear them and no comparison reads them.

Captures land in the log directory, also in a `novlog` build, and run whether
or not the profiler is on. A timed capture runs `[s]` seconds,
`parameter.ProfCaptureDefault` (10 s) when omitted and at most
`ProfCaptureMax` (5 min):

| Command | File | Read with |
|---|---|---|
| `:d cpu [s]` | `vif-cpu-<time>.pprof` | `go tool pprof`; samples carry `kind` and `module` labels, so `-tagfocus module=drain` isolates one |
| `:d heap` | `vif-heap-<time>.pprof`, after a GC | `go tool pprof -sample_index=alloc_space` for allocation sites, `inuse_space` for retention |
| `:d mutex [s]` | `vif-mutex-<time>.pprof`; every contention sampled only while it runs | `go tool pprof`; wait time by the stack that held the lock. Cumulative over a process's captures, so `-base` an earlier one to isolate a later one |
| `:d trace [s]` | `vif-trace-<time>.out` | `go tool trace`; every timed call is a region named `<kind> <name>` |

## 12. Control surface

### Startup flags

The full command line is [Development](development.md) §4; these are the flags
that shape diagnostics.

| Flag | Effect |
|---|---|
| `-l`, `-log` | Enable logging in the platform user-state log directory, or `paths.log` from `vif.toml` |
| `-l=DIR` | Enable logging in `DIR`; the space form is not supported |
| `-lv`, `-log-level <level>` | `trace`, `debug`, `info`, `warn`, `error`; implies `-l` |
| `-ls`, `-log-scope <spec>` | Scope spec (§4); implies `-l` |
| `-lt`, `-log-stat <ticks>` | Status snapshot period, `0` disables; implies `-l` |
| `-lr`, `-log-recorder <ticks>` | Flight recorder depth, `0` disables; implies `-l` |
| `-log-session-id <id>` | Tag every record and name the file (§2); implies `-l` |
| `-log-stdout` | Write the session log to stdout as JSON; implies `-l` |
| `-j`, `-journal` | Capture replay input in the user-state journal directory, or `paths.journal`; does not imply a session log |
| `-j=DIR`, `-journal=DIR` | Capture replay input in `DIR`; the space form is not supported |
| `-r`, `-replay <file>` | Play a journal back; with `-headless`, verify it against its digests (§9) |
| `-script <file>` | Execute an authored headless tick schedule; may also host/join and may be journaled with `-j` |
| `-bot [N[:graph]\|graph]` | Add bots beside the human; `-watch` or `-headless` selects autonomous play. `-j` records the viewed instance and its final tick, so replay needs no graph. See [Bots](bots.md) |
| `-dev[=bool]` | Runtime stderr capture; defaults on for race builds |
| `-probe <bind-address>` | Serve `/health` (liveness; readiness in its body) and the status registry as Prometheus text at `/metrics`, for a `-serve` run |

Log setup failure is **fatal**: a run that was asked to log and could not exits
immediately with `73` (`EX_CANTCREAT`): a supervised process that played on
unlogged would lose the very record it was started to produce.

`-log-stdout` writes the session log to stdout as JSON instead of to a file, and
implies `-l`. The two sinks are exclusive: a run that wants its log on stdout is
one whose filesystem is not where anybody will look, and a console write into a
run that owns the alternate screen is corruption rather than output — so it stays
off by default and belongs to runs with no terminal.

Which one a deployed session uses is decided by who has to read it. `-log-stdout`
is `kubectl logs` and nothing else. `-l=DIR` onto a shared volume is what lets an
independent [LogWisp](https://github.com/lixenwraith/logwisp) service read the
directory and serve those exact lines as Server-Sent Events — which also makes the
log a metric stream, because §6's periodic snapshot emits the whole status registry
into it. The deployed fleet uses the second form; see
[deployment](kube-docker-deploy.md) §7.

At the `app.Config` boundary, zero means "use the parameter default" (for
`RecTicks`, only with a session log running, §8) while a negative
`StatTicks`/`RecTicks` means disabled. The CLI therefore maps an
explicit `-lt=0` or `-lr=0` to `-1`; embedders must preserve the same
convention.

`-ls` is validated through `vlog.ParseScopes` during flag parsing, before the
terminal enters its alternate screen. The flight recorder resolves its
directory from log configuration but does not require the session to remain
running; after `:log off`, a trigger writes a sidecar instead. Replay journals
use their independently configured journal directory.

### Runtime commands

| Command | Effect |
|---|---|
| `:log` | Report current state |
| `:log on` / `off` | Start or stop a session (`start`/`stop` too); stop drains asynchronously |
| `:log <level>` | Shorthand for `:log level <level>` |
| `:log level <level>` | Set the threshold |
| `:log scope <spec>` | Apply a scope spec |
| `:log stat <ticks>` | Set the snapshot period, `0` disables |
| `:log rec` | Report state |
| `:log rec <ticks>` | Set the recorder depth, `0` removes it; discards history |
| `:log rec flush` | Request a window flush on the next tick |
| `:log rec fsm [on\|off]` | Toggle the FSM transition trigger |
| `:t save` | Write a standalone snapshot (§7) |
| `:journal`, `:journal start\|stop [slot]` | Report the journals, open one where the run stands or close it; with a slot, a bot seat's (§9) |
| `:d`, `:d prof [on\|off]` | Profiler report; start or stop the profiler (§11) |
| `:d cpu [s]`, `:d heap`, `:d mutex [s]`, `:d trace [s]` | Write a CPU, heap or mutex profile, or an execution trace (§11) |
| `:d break ...` | Set, list or clear standing breakpoints that pause on the command line ([Input](input-and-modes.md)) |
| `:content` | Corpus telemetry in the status bar |

`:log` reports `log <path> | level <L> | scope <S> | stat <N> | rec <M>`. The
configuration menu's Diagnostics page (`:g`) carries the same controls as
presets ([Configuration menu](config-menu.md)).

Starting a session opens a file under the world lock — a deliberate operator
cost. Stopping detaches the sink immediately and drains on another goroutine,
so a command handler never waits on disk while holding the lock.

## 13. Sink policy

| Policy | Value |
|---|---|
| Queue | 8,192 records |
| Flush interval | 50 ms (one simulation tick) |
| File rotation | 64 MB |
| Total budget | 512 MB |
| Minimum free disk | 100 MB |
| Retention | 24 hours |
| Heartbeat | 60 s, level 1: a `PROC` record of processed and dropped counts |
| Process-exit drain | 2 s |
| Panic flush | 200 ms |
| Snapshot, recorder sidecar and journal close drain | 3 s |

Every file-writing logger instance — the session log, a journal, a snapshot or a
recorder sidecar — applies this policy to the directory it writes in, not to its
own file. Its hourly retention check deletes every other `*.jsonl` there older
than 24 hours, and its disk check deletes the oldest of them first while the
directory is over 512 MB or the disk under 100 MB free. A rotation renames the
active file to `<name>_<yymmdd_hhmmss>.jsonl` and opens a fresh file under the
active name. The journal directory is therefore pruned by whichever journal is
recording: copy a journal worth keeping out of it. Stderr captures (`.log`) and
profiles (`.pprof`, `.out`) are never pruned.

Commissioned `-log-session-id` processes use an 8 MB per-file rotation cap and
disable the logger's directory-wide total-size, minimum-free-space cleanup, and
age retention. The fleet's capped tmpfs and node cleanup unit own those shared
directory limits; ordinary desktop logging keeps the policy above.

The sink **drops** records when the queue is full rather than blocking a game
goroutine. Drops are counted and reported only by the next `PROC` heartbeat,
total and since the last one. A dropped record is lost; there is no retry.

The 50 ms flush interval matches the simulation tick, so a crash loses at most
one tick of buffered records.

Each logger instance is independent: the session log, each replay journal, an
on-demand snapshot, and a recorder sidecar can be open simultaneously without
contending for one buffer.

## 14. Call-site rules

**Argument lifetime.** Records are formatted asynchronously on the logger
goroutine, up to a full queue (8,192 records) after the call. Pass primitives
and value copies only — never `Store.GetPtr` pointers, pooled event payloads,
dense entity slices, or reused scratch buffers. Their contents may change before
formatting.

**Write through the runtime's handle.** Simulation, session and replay code logs
through `World.Log()`, `GameContext.Log` or its App's handle. The package functions
write the process's own handle, so a seat, a staging world or a replay copy using
them would carry another runtime's stamp and escape its own mute; they are for
records with no runtime: flags, process lifecycle before an App exists, the crash.

**Gate hot call sites.** The variadic slice is built before the call and
escapes to the heap. Guard with `On(sub, level)` and hoist the handle and the gate
out of loops:

```go
log := s.world.Log()
trace := log.On("dispatch", vlog.LevelTrace)
for _, ev := range events {
    if trace {
        log.Detail("dispatch", "msg", "ev", "ev", event.GetEventName(ev.Type))
    }
}
```

**Name records by rule.** `msg` is a constant lowercase phrase and values go in
fields. An event is a subject and past participle (`journal opened`, `peer link
lost`), a failure says what did not happen (`correction not applied`) or what
failed, a periodic report is a noun phrase (`session summary`), and a per-item tap
names its item (`ev`, `intent`, `push`). A measured span is an integer with its
unit as suffix (`stage_us`); bare `us` is the record's own duration, and `_ms` is
for game-time bounds (`max_ms`). The sub is its scope's name unless a narrower view
needs a tag of its own (`system`, `admit`).

**Choose the right entry point.** All but `vlog.Dump` are `vlog.Log` methods.

| Method | Use |
|---|---|
| `Debug`, `Info`, `Warn`, `Error` | ordinary records |
| `Detail` | trace level without a stack trace; per-item taps |
| `Trace(sub, level, depth, ...)` | records that need a call chain; depth is raised to skip the wrapper |
| `EmitSet(sub, run, tick, fill)` | a correlated set that must share one stamp |
| `vlog.Dump(run, tick, fill)` | a standalone `vif-snap` file, blocking; call it off the world lock |

**Never log inside the world lock at volume.** A guarded call is a channel send
and is cheap, but a per-entity record inside a system update extends the
critical section. Prefer a status metric and let the snapshot or recorder carry
it.

**Status over logging for state.** A value that has a current reading belongs
in the registry. A value that describes a *transition* belongs in the log.

## 15. Build variants

`internal/vlog` is build-tagged. On `wasm` or with the `novlog` tag, `vlog.Linked`
is false, emission entry points are no-ops, a session or journal start and a
standalone file report `vlog.ErrDisabled` (`logging unavailable in this build`, or
`in a browser`), and `lixenwraith/log` is not linked. Scope constants remain,
and `ParseScopes` accepts any spec without checking it. `status` still functions:
metrics are registered and updated, while snapshot/recorder writes resolve to
no-ops. Asking such a build to log exits with `73` at startup and `-j` fails the
run's construction; `:log on`, `:t save` and `:journal start` check `vlog.Linked`
and refuse with their menu rows' reason, so a recording requires a
logging-enabled native build.

`make nolog` produces a release build with the tag; the experimental
`make windows` build carries it too.

A `vif_headless` build keeps logging, journals and `-replay -headless`
verification; only presentation goes, so a bare `-replay` there fails with
`journal presentation is unavailable in a vif_headless build`.

## 16. Diagnostic playbooks

**"The FSM is in the wrong state."** `-ls afs -lv debug`. Filter `sub=fsm`. The
`transition` records give the complete ordered path including intra-tick
chains, background regions, and region lifecycle. `via` identifies the cause of
each step; a step with `via=Tick` means a guard passed.

**"My event does nothing."** `-ls +d -lv trace`. Filter `sub=dispatch` and the
event name. `sys:0 fsm:false` means nothing consumed it — check the router
registration and the active region's triggers. Absent entirely means it was
never pushed: add `-ls +p` to see the producer side.

**"The game stutters."** `-ls al -lv debug`. `sub=lock msg="long hold"` records
carry the holder's call chain. Each one also triggers a recorder flush, so the
20 Hz state history around the stall is in the same file. `:d prof on` names
the modules the tick and frame spend on; `:d trace 5` across a stutter shows
each one on the timeline.

**"State was lost."** `sub=event msg="queue overflow"` reports the count. The
recorder flushes automatically with `reason=event.dropped`; the window shows
what the producers were doing in the 200 ticks before.

**"It crashed."** `sub=crash` carries the panic and stack. The recorder window
immediately precedes it. On a race build, `sub=race` records point into the
stderr capture file.

**"Reproduce this input sequence."** Start with `-j` and copy every
`vif-jrn-*` rotation that spans the run out of the journal directory before its
retention prunes them (§13). A journal is independent of diagnostic
level/scope; inspect anchor fingerprints and `jseq` density before blaming a
replay difference on the simulator.

**"A metric is missing from snapshots."** Check `stat.late` in any snapshot. A
non-zero value means a metric was registered after `Freeze` and is invisible to
both the snapshot and the recorder.

## 17. Source map

| Concern | Primary source |
|---|---|
| Facade, levels, sink lifecycle | `internal/vlog/vlog.go` |
| Per-runtime handles: stamp, instance tag, mute | `internal/vlog/handle.go` |
| Scopes and spec parsing | `internal/vlog/scope.go` |
| Standalone files, correlated sets | `internal/vlog/dump.go` |
| WASM/`novlog` stub | `internal/vlog/stub.go` |
| Metric maps, freeze, late counting | `internal/status/metric_map.go`, `registry.go` |
| Group index, periodic emission | `internal/status/snapshot.go` |
| Key convention, integer units | `internal/status/key.go`, `format.go` |
| Per-slot metrics and the local-slot mirror; overlay group views | `internal/status/player.go`, `view.go` |
| Atomic float and string cells | `internal/status/atomic_float.go`, `atomic_string.go` |
| Flight recorder | `internal/status/recorder.go` |
| Journal record/anchor schema and queue sink | `internal/event/journal.go`, `journal_sink.go`, `origin.go` |
| Recording lifecycle, capture, loading, replay/fuzz/script drivers | `internal/journal` |
| Journal start and stop mid-run, the opening and its adoption, seat journals | `internal/app/journal.go`, `seat.go` |
| Replay/config verification and presentation, replay checkpoints | `internal/app/replay.go`, `play.go`, `clone.go` |
| Comparison surfaces and world digests | `internal/app/surface.go`, `internal/snapshot/surface.go`, `digest.go` |
| Tick stamping, FSM taps, dispatch tap, triggers | `internal/engine/scheduler.go` |
| Lock hold sampling | `internal/engine/sync_std.go` |
| Frame counter | `internal/engine/game_context.go` |
| On-demand context snapshot | `internal/engine/snapshot.go` |
| FSM observation hooks | `internal/fsm/machine.go`, `types.go` |
| Runtime stderr capture, fd redirect, crash handler | `internal/core/dev.go`, `redirect_*.go`, `crash_handler*.go` |
| Profiler, captures, process sampling | `internal/prof` |
| Flags, `vif.toml` path defaults, diagnostics setup | `cmd/vif/main.go` |
| Runtime commands | `internal/mode/commands.go` |
| Periods and intervals | `internal/parameter/engine.go` |
| Default log and journal directories; `vif.toml` `paths.log`, `paths.journal` | `internal/paths/paths.go`, `settings.go` |
| `/health` and `/metrics` for `-probe` | `internal/probe` |
