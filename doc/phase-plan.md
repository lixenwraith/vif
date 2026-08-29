# vi-fighter — multi-instance phase plan

Companion to `domain-design.md`. Rules referenced as D-n.

## Phase 4 — Domain boundary · landed

| Step | State |
|---|---|
| 4.1 Cell partition, `DomainScope`, player budget | done |
| 4.2 Shared systems scoped | done |
| 4.3 Combat and soft-collision RNG split | done |
| 4.4 Combat payload reduced to geometry | done |
| 4.5 Player-domain projectiles; cleaner and materialize stamped | done |
| 4.6 Cursor view components (`CursorViewComponent`) | done |
| 4.7 Nugget cursor leak resolved via `ClosestCursor` | done |
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
   also carry quasar, loot and species state, which is re-derived rather than
   transported.
4. **Contested-vs-personal audit.** Every `ClosestCursor` site is contested:
   deterministic slot order, positions only. One fix landed —
   `NuggetSystem.emitBeacon` now uses `PushLocal`, so a shared nugget beacon
   draws each instance's own cleaners instead of one shared cleaner carrying
   pure visuals.
5. **`MetaSystem` profile confirmed** `shared`, with the rationale corrected:
   its world writes are replicated or are the D-14 map-bounds writer, and the
   context state it writes is not world state. It now declares that profile in
   `manifest.ContextSystems` rather than sitting outside the manifest.

Also landed, out of the original scope: gold contributor attribution.
`CompositeMemberDestroyedPayload.Entity` carries the typist, `GoldSystem`
tallies per roster slot, and `GoldCompletionPayload.Entity` names the cursor
that typed the most members (ties to the lowest slot, so every instance credits
the same one). Timeout and destruction leave it zero.

## Phase 5.5 — Manifest convergence · landed

Pulled forward from "Deferred, own context" once both tables stopped moving.

Component domain and system profile are data in
`internal/manifest/definition.go`, generated into
`internal/engine/component_domain_gen.go` and `internal/manifest/build_gen.go`.
`System` no longer declares `Domain()` or `Requires()`; `World.AddSystem(sys,
profile)` takes a `manifest.ProfileFor(name)`. The methods were deleted rather
than asserted — one declaration site, so nothing can drift.
`internal/system/network.go` carries a `TODO(phase7)`: it is written but
registered nowhere, and `TestSystemDomainProfiles` exempts it by name.

## Phase 6 — Event classification and journal completeness

The prerequisite for any wire format. Largest mechanical phase; 167 event
types across 53 sections of `internal/event/type.go`.

**Findings that reshape this phase.** A dispatch-tap census over three seeds
(`DefaultScript`, 4000 steps each) observed 91 of the 166 real types
(`EventTypeCount` is 167 including the `EventNone` sentinel). Of those, 70
carry a `shared` tag, 20 `player`, and one — `EventSpeciesKilled` — both. Two
consequences:

- 75 types never fire in a soak, so no runtime pass can populate the table.
  The class must be declared and only *checked* where observed.
- The tag is opt-in (D-7): the ambient domain defaults to shared and is not
  derived from the system profile, so `shared` on a record means "nobody said
  otherwise", not "shared". FSM-emitted D-6 effects (`EventGrayoutStart`,
  `EventStrobeRequest`, `EventDecaySpawnOne`) are tagged shared today.

1. **Registry classes.** Add `Shared|Bus|Local|Stamped` to `RegisterType`,
   derived from a doc-comment annotation the way payloads already are.
   `internal/gen-manifest/main.go` owns the grammar: `docPayload` parses the
   parenthesized payload from the line opening with the constant name, and
   `collectEvents` enforces the contract. Extend that rather than adding a
   second mechanism — proposed form, a bracketed token after the optional
   payload:

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
   Schema stays 6 unless the filter adds a record field; `ConfigFromAnchor` and
   `VerifyAnchor` follow only if it does. No fixtures exist to regenerate.

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

**Open question, decide before writing the table.** `Stamped` is per-instance
for at least two types, so no static entry can carry the class:
`EventCombatAttackDirectRequest` crosses only when
`ChainDepth == 0 && TargetEntity.Domain() == DomainShared`, and the census
shows `EventSpeciesKilled` mixed 53 player to 1 shared from a single producer.
Either the journal filter carries the same predicate the test does — today it
lives only in `crossingTargets()` in `internal/app/bus_purity_test.go` — or the
producers stamp `GameEvent.Domain` from the target's domain and the filter keys
on the tag. The second is cheaper at the filter and D-10 already names
`Stamped` as the mechanism, but it is a wiring change across every combat
producer, and it makes the domain tag mean two different things depending on
class. Resolve it before populating 167 entries, not after.

**Exit criterion.** `TestBusPayloadsNameOnlySharedEntities` runs against the
declared Bus set rather than its hand-list, and every event type has a class.
Second, cheap assertion worth adding at the same time: over a soak, every
`Local`-classed event carries `Domain == DomainPlayer` at push. That turns item
3 from an inspection into a test.

## Phase 7 — Transport

1. **Join handshake.** Carries the D-14 map-size latch, the seed, the session
   counter and the config/content identity. `JournalAnchor` already carries
   exactly this set for replay — extend that shape rather than inventing a
   second one.
2. **`NetworkSystem` wiring.** `NetworkPort.Drain` per tick already exists as a
   poll model, deliberately keeping network goroutines out of the world event
   queue. The interface collapses to a concrete type as the package matures,
   following the audio precedent. Declaring it in `manifest.Systems` retires
   the `TODO(phase7)` and the `TestSystemDomainProfiles` exemption.
3. **Remote cursor lifecycle.** `EventCursorSpawnRequest{Control: ControlRemote,
   PeerID}` already exists and works; the roster, slots and `CursorSystem` need
   no change. Verify with two local cursors first, one marked remote.
4. **Owner-authored replication.** Periodic value sync for the D-13 component
   set, one direction per cursor. This is the only state transfer in the design;
   everything else re-derives.
5. **Bus event transport**, driven by the Phase 6 classification.

## Phase 8 — Multi-instance verification

Two in-process instances sharing a seed and an event pipe, driven by
`RunScript`, asserting `SnapshotShared()` equality per tick and reporting the
first divergent record via `FirstDiff`/`Diff`. This is Phase 4's exit criterion
applied to a real second participant rather than a second terminal size.

Blocker to resolve first: `headless.go` documents four process-wide values that
no snapshot reaches but that prevent concurrent Apps — the status recorder
trigger hook, the navigation debug pointers in `internal/system`, help's key
table, and vlog's correlation stamp. Two live instances in one process needs
those scoped.

Second blocker, smaller: the `ctx|player` snapshot record carries this
instance's cursor binding (`entity`, `slot`, `x`, `y`) in a record the shared
view keeps. Parity holds today only because both instances bind slot 0. Split
it before a real second participant: `count` stays shared, the binding moves to
`view`.

## Carried-forward gaps

Small and self-contained; none blocks Phase 6.

- **`ctx|player` record split** — `internal/engine/snapshot.go`, and the record
  filter in `internal/app/snapshot.go`.
- **`spatial.indexed_shared` allow-list** — the key is genuinely comparable
  across instances and is dropped by the `spatial.` prefix deny. Wants an
  allow-list; the shared position digest covers it meanwhile.
- **`internal/journal` round-trip coverage** — zero tests, one non-test
  importer (`internal/app/play.go`). A `DomainNames` or field-name change
  breaks `vif play` silently.
- **`World.UpdateBoundsRadius`** writes `PingComponent` for every rostered
  cursor including remote ones. Harmless under D-13; restricting it to the
  local slot forces `setLocal` to clear the departing slot.
- **`uint32(entity)` narrowing** at `gateway.go` and `adaptation.go`, safe only
  while route-graph anchors are shared.

## Deferred, own context

- **Windowed composite / vision box.** A recording or session on a terminal
  smaller than the map is clipped by the render buffer; `play.go`'s pan offset
  is a placeholder. Pure presentation, no shared state, no abuse surface.
  Needs its own focused session.
