# vi-fighter — multi-instance phase plan

Companion to `domain-design.md`. Rules referenced as D-n.

## Phase 4 — Domain boundary · landed except verification

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
| 4.17 System domain profiles, dependency resolver in core, FSM wiring (D-15) | done |
| **4.16 Verification** | **outstanding — the whole of the remaining phase** |

### 4.16 work items

1. **Boundary suite** (`internal/system/boundary_test.go`, or wherever 4.17 put
   the profile assertions — extend rather than duplicate):
   - (a) no shared-profile system's `Update` touches a player entity. Runtime
     path: soak with `SetDomainAudit(true)` plus a store-access tap; static
     path: AST walk for `CreateEntity(core.DomainPlayer)` and player-only store
     writes in shared-profile files. 4.17 may have landed part of this — check
     first and close the gap, do not rebuild.
   - (b) every Bus payload names only shared entities. Table-driven over the
     Bus set, reflect over `core.Entity` fields, assert
     `Domain() == DomainShared`. Blocked on the Bus set being declared, which
     is Phase 6 — until then, hand-list it in the test and mark the TODO.
   - (c) player knockback draws no shared RNG. Snapshot the shared stream
     position across a player-target collision in `CombatSystem` and
     `SoftCollisionSystem`.
2. **Map-lock test.** Spawn a second cursor, resize, assert `MapWidth`
   unchanged, no entity destroyed, `context.map_locked` set.
3. **Soak** with `-domain=debug`: zero `component domain mismatch`.
4. **Regenerate** journal fixtures and digests. Expect diffs confined to loot,
   spirit scatter, fuse records and the reshaped `context` snapshot.

**Exit criterion.** Two headless instances of one seed, differing only in
terminal size, produce identical `SnapshotShared()` at every tick boundary.
Nothing downstream is safe to build until this holds.

**Note.** Phase 5 item 1 (glyph) materially affects whether 4.16(a) proves
anything, since glyph is the largest entity population and is currently
unclassified. Consider pulling it forward into this batch.

## Phase 5 — Ownership consolidation

Closes the questions D-13 raised but did not answer. No transport.

1. **Glyph classification.** Decide shared vs. stamped; add the bit to
   `componentDomains` either way. Evidence needed: who creates glyphs
   (`glyph.go` from the content corpus, `gold.go`, dust conversion), who
   destroys them, and whether any player-domain path creates one. If the corpus
   is shared and typing is contested, glyph is shared and the dust conversion is
   the crossing.
2. **Reward path tagging.** Loot collection and contested-objective rewards
   push `EventWeaponAddRequest`, `EventEnergyAddRequest`, `EventHeatAddRequest`
   at a shared cursor from owner-authored context. Tag them so Phase 6 can
   classify them `Local` mechanically rather than by inspection.
3. **Owner-authored write test.** Assert no shared-profile system writes a
   component on the D-13 list. Mirrors 4.16(a) from the other direction.
4. **Contested-vs-personal audit.** `ClosestCursor` remains in `interaction.go`,
   `nugget.go` and `gold.go`. Confirm each is a contested shared outcome
   (correct) rather than an owner lookup (should be an explicit owner field).
   Nugget and gold are contested by design and stay.
5. **`MetaSystem` profile confirmation.** It is registered outside the manifest
   and touches the widest cross-cutting surface (resize, overlay, system
   toggles, status messages). Confirm 4.17 gave it a profile and that the
   profile is honest.

**Exit criterion.** Every component bit appears in `componentDomains`, and every
system's declared profile is asserted by test in both directions.

## Phase 6 — Event classification and journal completeness

The prerequisite for any wire format. Largest mechanical phase; ~167 event
types.

1. **Registry classes.** Add `Shared|Bus|Local|Stamped` to `RegisterType`,
   derive it from a doc-comment annotation the way payloads already are
   (`internal/manifest/cmd/main.go` parses `// EventFoo (FooPayload) ...` —
   extend that grammar rather than adding a second mechanism), populate all
   entries, regenerate. A misclassified event becomes a visible table entry
   rather than a runtime divergence.
2. **`EmitDeath` stamping.** Add the domain parameter, thread it through
   `sweep.go`, `fuse.go`, `drain.go`, `weapon.go`, `typing.go`. Closes the
   `TODO(phase6)`.
3. **Storm red bullets** classified `Local`; loot and objective reward grants
   classified `Local` per Phase 5 item 2.
4. **Journal filter.** Records carry `Domain`; the transported set is
   `Shared ∪ Bus`. Journal schema bumps to 7; `ConfigFromAnchor` and
   `VerifyAnchor` follow. Regenerate fixtures.
5. **FSM view-key instrumentation.** Tag `config_access.go` accessors
   replicated/non-replicated; warn once when a non-replicated key is read while
   `mapSizeLocal()` is false. Keys stay (D-14). Needs `internal/fsm/std/`,
   which no session has seen yet.

**Exit criterion.** 4.16(b) runs against the declared Bus set rather than a
hand-list, and every event type has a class.

## Phase 7 — Transport

1. **Join handshake.** Carries the D-14 map-size latch, the seed, the session
   counter and the config/content identity. `JournalAnchor` already carries
   exactly this set for replay — extend that shape rather than inventing a
   second one.
2. **`NetworkSystem` wiring.** `NetworkPort.Drain` per tick already exists as a
   poll model, deliberately keeping network goroutines out of the world event
   queue. The interface collapses to a concrete type as the package matures,
   following the audio precedent.
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
no snapshot reaches but that prevent concurrent Apps — the recorder trigger
hook, the navigation debug pointers, help's key table, and vlog's correlation
stamp. Two live instances in one process needs those scoped.

## Deferred, own context

- **Windowed composite / vision box.** A recording or session on a terminal
  smaller than the map is clipped by the render buffer; `play.go`'s pan offset
  is a placeholder. Pure presentation, no shared state, no abuse surface.
  Needs its own focused session.
- **`component_domain.go` / system profile convergence.** The file carries
  `TODO: merge into manifest and codegen`, and a system's domain profile is
  largely derivable from which component stores it writes. Worth doing once
  both tables are stable; not worth doing while either is moving.

