# vi-fighter — multi-instance phase plan

Companion to `domain-design.md`. Rules referenced as D-n.

## Phase 4 — Domain boundary · complete

| Step | State |
|---|---|
| 4.1 Cell partition, `DomainScope`, player budget | done |
| 4.2 Shared systems scoped | done |
| 4.3 Combat and soft-collision RNG split | done |
| 4.4 Combat payload reduced to geometry | done |
| 4.5 Player-domain projectiles; cleaner and materialize stamped | done |
| 4.6 Cursor view components (`CursorViewComponent`) | done |
| 4.7 Nugget cursor leak resolved via `ClosestCursor` | superseded in Phase 8 — personal |
| 4.8 Fuse player-domain, geometry-only crossing, spirit stamped | done |
| 4.9 Config split | withdrawn — one struct, no benefit |
| 4.10 Drain flocking | resolved by declaration |
| 4.11 `ViewResource` split | done |
| 4.12 `CommitShared` placement gate | done |
| 4.13 Personal loot, player-domain, per-slot pity | done |
| 4.14 Map-size guard and lock telemetry (D-14) | done |
| 4.15 Per-domain counters, scoped digest, `SnapshotShared` | done |
| 4.16 Verification | done |
| 4.17 System domain profiles, dependency resolver in core, FSM wiring (D-15) | done |

**Exit criterion, met.** `TestSharedSnapshotParityAcrossTerminalSizes` steps two
instances of one seed in lockstep on different terminal sizes and asserts
`SnapshotShared()` equality at every step.

### 4.16 as landed

| Test | Package | Asserts |
|---|---|---|
| `TestSystemDomainProfiles` | `internal/system` | Each declared profile against the RNG streams, entity domains and component stores its file touches; resolves hoisted store aliases |
| `TestAllowedDomainAccessIsLive` | `internal/system` | The exemption list cannot outlive the access it excuses |
| `TestHelperFilesArePinned` | `internal/system` | The unattributed file set (`blast.go`, `interaction.go`, `sweep.go`, `targeting.go`, `telemetry.go`) is fixed, so a new helper is visible |
| `TestSystemsDeclareNoDomainMethod` | `internal/system` | No system re-declares `Domain()`/`Requires()`; the manifest is the only declaration site |
| `TestCombatKnockbackDrawsFromTheTargetsStream`, `TestSoftCollisionImpulseDrawsFromTheTargetsStream` | `internal/system` | D-8: a player-target impulse never advances the shared RNG stream, the shared case proving it non-vacuous |
| `TestBusPayloadsNameOnlySharedEntities` | `internal/app` | D-4 over a soak, via a dispatch tap: emitter fields always, target fields only on crossings |
| `TestDomainAuditSoakClean` | `internal/app` | Zero component-domain violations over a 3000-step soak, with retained descriptions |
| `TestMapSizeLockedWithSecondCursor`, `TestMapSizeCropsWithOneCursor` | `internal/app` | D-14, with the crop path as its own negative control |
| `TestSharedGlyphsAreGoldMembersOnly` | `internal/app` | Every shared-domain glyph is a gold composite member |
| `TestSharedSnapshotParityAcrossTerminalSizes` | `internal/app` | The exit criterion |

Supporting machinery: `engine.PinDomainAudit`/`DomainMismatches`/
`DomainViolations`; per-system audit attribution in `UpdateLocked`, falling back
to `"event"` for settle-pass attaches; `ClockScheduler.SetDispatchTap` and
`App.SetDispatchTap`; `ScriptDriver` exported with `Step()` for lockstep
driving; `ScriptOptions.Resizes`/`MapMotionsOnly`; `FastRand.State()`.

**4.16(4) closed by construction.** There are no journal fixtures on disk.
Every journal test uses the in-memory `app.Capture` sink, so nothing needed
regenerating. `internal/journal`, the JSONL reader, has exactly one non-test
importer — `internal/app/play.go` — and zero test coverage; that gap is carried
into Phase 6.

## Phase 5 — Ownership consolidation · landed

1. **Glyph classified.** Content glyphs are player-domain; gold sequence members
   are the only shared entities carrying `GlyphComponent`. `GlyphBit` stays
   unlisted in `manifest.Components`, since the bit legitimately attaches in
   either domain. Player-domain mechanics guard by
   `e.Domain() != core.DomainPlayer` — one invariant replacing three accidental
   protections.
2. **Reward tagging.** `World.PushLocal` stamps `Domain = DomainPlayer` and is
   applied across owner-authored grants (energy, heat, boost, shield, weapon)
   and D-6 effects (sound, blinks, flash, fadeout, splash, strobe, grayout).
   Behaviour-neutral: the tag never affects dispatch, and these all carry
   `OriginSystem`, so nothing is journaled differently.
3. **Owner-authored write test.** `ownerAuthoredStores` restricted to
   cursor-exclusive components. `Shield` and `Combat` are excluded because they
   also carry quasar, loot and species state that is re-derived rather than
   transported (D-13).
4. **Contested-vs-personal audit done.** All `ClosestCursor` sites are
   contested — deterministic slot order, positions only. One fix:
   `NuggetSystem.emitBeacon` now uses `PushLocal`, so a shared nugget beacon
   draws each instance's own cleaners instead of one shared cleaner carrying
   pure visuals. Phase 8 supersedes this arrangement by making the nugget itself
   personal; the beacon remains local.
5. **`MetaSystem` profile confirmed** `shared`, rationale corrected: its world
   writes are replicated or are the D-14 map-bounds write, and the context
   state it writes is not world state.

Also landed, ahead of its phase: **gold contributor attribution**.
`CompositeMemberDestroyedPayload.Entity` carries the typist, `GoldSystem`
tallies per roster slot, and `GoldCompletionPayload.Entity` names the cursor
that typed the most members — ties to the lowest slot, so every instance
credits the same one. Timeout and destruction leave it zero. This is the D-3
gold crossing row.

**Exit criterion, met.** Every component bit appears in `manifest.Components`,
and every system's declared profile is asserted in both directions.

## Phase 5.5 — Manifest convergence · complete

Was listed under "Deferred, own context" as *component_domain.go / system
profile convergence*. Both tables were stable after Phase 5, so it was pulled
forward.

Component domain and system profile are now data in
`internal/manifest/definition.go`, generated into
`internal/engine/component_domain_gen.go` and `internal/manifest/build_gen.go`.
`System` no longer declares `Domain()` or `Requires()`; `World.AddSystem(sys,
profile)` takes a `manifest.ProfileFor(name)`. The methods were deleted, not
asserted — one declaration site, no drift.
`internal/system/network.go` carried a `TODO(phase7)`: written but registered
nowhere, exempted by name in `domain_test.go`. Phase 7 registered it.

## Phase 6 — Event classification and journal completeness · landed

The prerequisite for any wire format. Largest mechanical phase; 167 event types
(`EventTypeCount`, including `EventNone`).

**The finding that shapes this phase.** `EventCombatAttackDirectRequest` is
`Stamped`, and `Stamped` is not "resolved from the ambient domain at push". Its
class is per-instance, resolved by the *target's* domain: the same producer, in
the same tick, under the same ambient domain, pushes a hit that crosses when the
target is shared and does not when the target is player. **No static per-type
table can carry it.** Resolved as landed: the combat producers stamp
`GameEvent.Domain` from the target's own domain at all four push sites, and both
`event.Replicated` and `event.OnWire` read the tag. `crossingTargets()` in
`internal/app/bus_purity_test.go` is gone; that test now runs against
`event.Replicated`.

1. **Registry classes.** Add `Shared|Bus|Local|Stamped`, derived from a
   doc-comment annotation the way payloads already are. The generator is
   `internal/gen-manifest/main.go` (*not* `internal/manifest/cmd/main.go`,
   which does not exist); `docPayload` already cuts the constant name and an
   optional `(Payload)` off the doc line, so a class marker in the remainder
   parses with a sibling `docClass` and no second mechanism:

   ```
   // EventFoo (FooPayload) [bus] short description
   // EventFoo [local] short description
   ```

   Land it in two passes: warn on a missing token first, so one generator run
   enumerates the unclassified set; populate; then promote the warning to an
   error so the exit criterion is structural. The class belongs beside
   `typeToPayload` in `registry.go` as a `typeToClass` map with a `ClassOf`
   accessor, populated from the same generated `InitRegistry` call.

2. **`EmitDeath` stamping.** `event.EmitDeath` takes the queue, not the world,
   so it cannot read the ambient tag and writes `GameEvent` directly — the
   `TODO(phase6)` in `sweep.go` and `fuse.go`. Add the domain parameter and
   thread it through. The plan previously named five files; the real surface is
   38 call sites in eleven: `cleaner.go`, `composite.go`, `decay.go`,
   `drain.go`, `dust.go`, `fuse.go`, `splash.go`, `sweep.go`, `typing.go`,
   `wall.go`, `weapon.go`. `cellSweep` already emits one batch per domain, so
   every site has the domain in hand.

3. **Local-class producers.** Mostly landed by Phase 5 item 2: every
   system-side producer of the grant and drain family already pushes through
   `PushLocal`, storm red bullets included. What remains is the operator
   commands in `internal/mode/commands.go`, which push with the ambient shared
   tag under `OriginCommand` and are therefore journaled — retagging them
   changes recorded record domains, so it batches with item 4. The FSM-emitted
   effects above are the second remaining group; `std.Handlers.Emit` is the one
   seam that would let them carry a class-derived tag.

4. **Journal filter.** Less remains here than the plan assumed.
   `JournalRecord.Domain` already exists, `vlogSink.Record` already writes
   `domain`, and `internal/journal/read.go` already parses it back through
   `core.ParseDomain`. The work is the filter itself: the transported set is
   `Shared ∪ Bus`, which the domain tag alone cannot express, because a Bus
   event is player-tagged by definition. So the filter keys on `ClassOf(Type)`,
   with a per-instance predicate for `Stamped` — see the open question below.
   Schema went to 7 as landed: `Domain` became meaningful, so a 6 and a 7 journal
   are not comparable. Phase 7 took it to 8 for the anchor's map latch.

5. **FSM view-key instrumentation.** The plan named `internal/fsm/config_access.go`
   and `internal/fsm/std/`; neither is where this lives. The accessors are
   `internal/engine/config_access.go`, eight keys in two maps, reached only
   through `manifest/fsm_bridge.go`. Three are replicated shared state
   (`map_width`, `map_height`, `crop_on_resize` — D-14); five are per-instance
   (`viewport_width`, `viewport_height`, `camera_x`, `camera_y`, `color_mode`).
   Tag them in the map, wrap the five in a once-warn, and fire it when one is
   read while the map is locked. `GameContext.mapSizeLocal` reads only
   `World.Resources`, so lift it to a `World` method — the accessors take a
   `*World` and cannot see the context. Keys stay (D-14).

**Open question, resolved as landed: producers stamp, the filter reads the tag.** `Stamped` is per-instance
for at least two types, so no static entry can carry the class:
`EventCombatAttackDirectRequest` crosses only when
`ChainDepth == 0 && TargetEntity.Domain() == DomainShared`, and the census
shows `EventSpeciesKilled` mixed 53 player to 1 shared from a single producer.
Either the journal filter carries the same predicate the test does — today it
lived only in a test predicate — or the producers stamp `GameEvent.Domain` from
the target's domain and the filter keys on the tag. The second was taken. It does
make the domain tag mean two different things depending on class, and Phase 7 hit
that squarely: for `Bus` the tag is the producer's domain, for `Stamped` it is the
target's, so `event.OnWire` reads it in opposite directions per class.

**Exit criterion.** `TestBusPayloadsNameOnlySharedEntities` runs against the
declared Bus set rather than its hand-list, and every event type has a class.
Second, cheap assertion worth adding at the same time: over a soak, every
`Local`-classed event carries `Domain == DomainPlayer` at push. That turns item
3 from an inspection into a test.

## Phase 7 — Transport · landed

1. **Join handshake.** `JournalAnchor` gained the D-14 map latch (`map_w`,
   `map_h`, `crop_on_resize`); journal schema 8, records unchanged. `VerifyAnchor`
   split into `anchorIdentity` — schema, tick rate, seed, session counter, config
   and corpus identity — plus terminal geometry, which only a replay compares.
   `App.Join` runs the identity table, refuses an anchor whose position it cannot
   reconstruct (`ErrJoinMidRun`: nothing transports world state), and adopts the
   latch through `SetupLevel`, the D-14 authority, rather than by writing Config.

2. **`NetworkSystem` wiring.** Declared in `manifest.Systems` with a `dual`
   profile at `PriorityNetwork`; `unregisteredSystems` is now empty. The port is
   read per tick, so `App.AttachTransport` needs no re-registration. Transport
   work is not in `Update`: `event.WireSink` gained `Receive` (tick open, before
   the settle) and `Flush` (tick close, after it), both driven by
   `ClockScheduler`, so a peer receives one tick's artifacts as one tick's worth.

3. **Remote cursor lifecycle.** `World.SimulatesLocally` and
   `World.ResolveOwnedCursor` are the D-2 admission check; the five grant handlers
   and the five per-tick loops that aged a rostered cursor now go through them.
   `UpdateBoundsRadius` is local-only and `setLocal` clears the departing slot.
   Covered by three tests in `multi_cursor_test.go`, built on the existing harness.

4. **Owner-authored replication.** `CursorStatePayload` carries the D-13 set with
   Shield and Combat split to their cursor fields; `NetworkSystem` is its only
   writer and writes only what `SimulatesLocally` rejects. `CursorViewComponent.Orbs`
   is excluded (D-4).

5. **Bus event transport.** `event.OnWire` is the send predicate. **The finding
   that shaped it: the class alone cannot decide.** Every Bus type has producers of
   both kinds — a player mechanic crossing and a shared system re-deriving its own
   copy of the same type — so `World.PushCrossing` stamps the D-3 artifact player
   and `OnWire` requires the stamp. `TestCrossingPushesAreLive` fails a crossing
   that does not use it. For `Stamped` the tag is the target's domain, so
   `stampedCrossings` names the one type a player producer aims at a shared target,
   and a chain follow-up opts out through `event.Derived` (D-5).

6. **The unclosed crossings.** The nine `crossingPushes` sites plus the two
   `mode/router.go` jump requests and `mode/operators.go` now stamp. The weapon
   pulse is not closed and is carried forward.

**Exit criterion, met, one step short of Phase 8's.**
`TestObserverSharedStateTracksTheLiveParticipant` runs a live participant and an
observer that simulates no cursor, sharing a seed and a `network.Loopback` pipe,
and asserts `SnapshotShared()` equality at every tick boundary for 200 steps —
the observer's shared state arriving over the wire rather than re-derived. Two
*live* participants need the produce-exchange-apply barrier of Phase 8; see the
domain document's §7.

What Phase 7 also had to widen, none of it visible while both parity instances ran
identical player-domain simulations: the shared snapshot's deny rules (every
player- and dual-profile system's group, the both-domain aggregates, scratch
high-water marks, this participant's APM and corpus consumption), the `ctx|player`
record split, and the exclusion of cursor combat from the shared digest.

## Phase 8 — Multi-instance verification

Two *live* in-process instances sharing a seed and an event pipe, driven by
`RunScript`, asserting `SnapshotShared()` equality per tick and reporting the
first divergent record via `FirstDiff`/`Diff`.

Blocker to resolve first: **the lockstep barrier**. A crossing pushed during a
settle applies locally in that settle but reaches the peer at the next tick's
opening. Exact per-tick agreement needs produce, exchange, then apply — each
instance deferring its own crossings to the same relative point its peer applies
them at. Phase 7's one-directional test holds to 200 steps without it; two live
participants will not.

Second blocker, **resolved: nugget is personal and uncontested.** The component,
system, RNG and event family are player-domain/local. Collection reads only the
local binding, remote cursors cannot claim it, and a jump transports only the
resulting shared cursor move. The two-live parity test keeps nugget enabled.

Third, from `headless.go`, **resolved:** the status recorder trigger hook,
navigation debug state, help key table and vlog correlation stamp now belong to
their App-owned registry or `GameContext`. `TestConcurrentAppsKeepProcessStateScoped`
drives two Apps through resize and debug mutations without cross-talk.

The extended observer soak also closed four latent shared-outcome leaks:

- personal drain deaths now cross `EventDrainDefeated` before advancing
  `kills.drain`;
- `EventCombatHealRequest`, already pushed as a crossing, is correctly classified
  `Bus` rather than `Shared`;
- shared species resolve a shield impact only for a locally owned cursor and cross
  the exact shared area-hit target/member set;
- the global-reset guard folds crossed per-owner defeat state and fires only when
  every rostered cursor is defeated, rather than reading slot-zero heat/energy.

`TestSharedCursorOverlapOutcomesStayOwnerResolved`,
`TestSharedSpeciesCrossesOnlyOwnedShieldImpact`,
`TestCursorDefeatTransitionCrossesCombinedOwnerState` and
`TestMetaDefeatGateRequiresEveryRosteredCursor` pin those shapes. The protection
rejection counters of shared species are split by victim domain so the shared
snapshot compares their shared half.

## Carried-forward gaps

Small and self-contained; none blocks Phase 8. Closed in Phase 7: the `ctx|player`
record split, the `spatial.indexed_shared` allow-list, `World.UpdateBoundsRadius`,
and `internal/journal` round-trip coverage, which `read_test.go` now carries.
Closed in Phase 8: the disruptor pulse now crosses one combat-only explosion
artifact and resolves player targets before the crossing.

- **Operator grant commands** in `internal/mode/commands.go` push the
  owner-authored family with the ambient shared tag under `OriginCommand`.
  Harmless while those types are `Local` class, which the wire never reads.
- **`uint32(entity)` narrowing** at `gateway.go` and `adaptation.go`, safe only
  while route-graph anchors are shared.
- **`combat.` telemetry is a mixed aggregate** and is dropped whole from the
  shared snapshot. Splitting the counters per target domain would return the group
  to the comparison.

## Deferred, own context

- **Windowed composite / vision box.** A recording or session on a terminal
  smaller than the map is clipped by the render buffer; `play.go`'s pan offset
  is a placeholder. Pure presentation, no shared state, no abuse surface.
  Needs its own focused session.
