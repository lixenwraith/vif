# Troubleshooting: two-instance defects (2026-09-10)

Defects observed with host and guest on one machine, so the receive lead is the
only latency in play. Sources are trace logs of a `-host` and a `-join` run; the
figures quoted are from them and the logs themselves are not kept in the tree.
Section 7 is a second round after the first five landed.

Three of the first four are the same defect wearing different clothes, so that
shape is stated once and each issue refers to it. The invariants are in
[Multi-instance domain model](domain-design.md); the operating contract is in
[Multiplayer architecture](multi-player.md).

## 1. The shape behind issues 1, 2 and 4

A guest reaches a shared position two ways: it re-derives the transition, or an
authority capture installs it. The two are not equivalent, and the difference is
what every one of these defects is made of.

**A — an installed state runs no entry action.** `ImportFSM` replays only the
`reconcile = true` `ClassLocal` lifecycle actions of a crossed state path.
Everything else an `on_enter` would have done — a spawn request, a
`ResetStatusInt`, a splash request, a status message, the cycle damage multiplier
— is skipped on the instance that arrived by install. That is deliberate: re-running
`EmitEvent EventGoldSpawnRequest` would spawn a second gold. The consequence is
not: whatever the action established has to be in the capture instead, and
sometimes was not.

**B — a crossing the capture is assumed to contain is discarded.** §3.2 of the
plan prunes an ordinary frame whose `Seq` is at or below the capture's fence for
its source. The authority applies its own crossing immediately and captures
later, so almost every host frame is pruned on the guest. In this run, by tick
600, the host had sent 245 crossings and the guest had applied **80**, discarding
**165** as `artifacts_authority_superseded`. The rule is sound *for the shared
component stores*, which the capture carries. It is wrong for every other effect
that frame would have had — a status counter the capture excludes, or any
player-domain effect the receiver would have raised.

Both reduce to one sentence: **a shared effect that is re-derived rather than
carried is lost whenever the install replaces the derivation.**

A third, narrower rule made B worse than it looks. `snapshot.SharedKey` answers
two different questions with one predicate: what two instances must *agree* on,
and what a capture *carries* — `captureStatusLocked` filters through it. A cell
excluded as a mixed-domain aggregate is therefore also excluded from the
correction, and if an FSM guard reads it, the two instances silently drift apart.
`kills.*` is exactly that cell, and every escalation guard in the shipped game
reads one.

## 2. Issue 1 — the gold timer splash disappears on the guest

**Cause.** `spawnGold` raises the countdown with one `EventSplashTimerRequest`
(shape A). The splash is a player-domain entity anchored to the shared header; no
later event re-raises it.

**In the logs.** Both instances time the gold out at tick 401 and enter
`QuasarDustAll` at 402. The host takes `QuasarDustAll → QuasarGoldSpawn →
QuasarGoldActive` at 403 and pushes `EventGoldSpawned` + `EventSplashTimerRequest`.
The guest pushes neither: the capture for host tick 403 commits before the guest's
own tick-403 FSM update, so the guest's `quasar` region resumes from
`QuasarGoldActive` and its `QuasarGoldSpawn.on_enter` never runs. The install
carries the gold — `correction_entries 67, correction_entities 24` — and
`GoldSystem.LoadShared` restores liveness, header and deadline. Nothing restores
the splash, and the guest plays 40 ticks of gold with no timer. The next gold, at
tick 444, is spawned locally on both and the timer is back: hence "about 30% of
the time".

**Fix.** `GoldSystem.LoadShared` marks a sequence whose header this instance did
not spawn, and `Update` raises the timer for the remaining duration. `Update` runs
only on the live world, so the staging world does not queue one. This also covers
the join case, where the first capture always arrives without a local spawn.

**Since.** The inverse — a timer for a gold that is not there. A mispredicted
composite spawn shifts a guest's shared allocation, so its gold can hold an id the
authority gives to something else, and the timer anchored there kept counting over
it. `LoadShared` now cancels the timer of the header an install replaces.

## 3. Issue 2 — the cycle damage multiplier does not reach the guest

**Cause.** Both halves at once. `EventCycleDamageMultiplierIncrease` is
`ClassShared`, so it is never transported (correctly — a transported copy would
double it), and it is raised only by `StormDecision.on_enter`, which a corrected
guest never runs (shape A). The value it set lived in `EnergySystem.damageMultiplier`
— a private field of a **player**-profile system, which `TestSnapshotCarriersAreSharedOrDual`
forbids from carrying anything, so it was in no capture. Its published cell,
`energy.damage_multiplier`, is excluded from the captured surface by the `energy.`
prefix. The multiplier was therefore unrecoverable on a guest by construction: not
re-derived, not carried, not on the wire. The field's own comment already said what
it was — "a world property, not a player one".

Neither log shows the event, because neither run reached a storm; the defect is
structural rather than observed.

**Fix.** `MetaSystem` — shared profile, already the writer of the world counters —
owns the multiplier, publishes the same cell, and carries it. `EnergySystem`
reads the cell when scaling a penalty; the private copy is gone, so there is one
writer and one value.

## 4. Issue 3 — species stop taking damage when both cursors attack

**Cause.** `RemainingDamageImmunity` was one window per *target*: any landed hit
opened 150 ms in which the target refused everyone. Two consequences, and the
second is the one that reads as "no damage at all":

- The window is a budget the roster divides. Two participants firing at one swarm
  land one hit per 150 ms **between them**, so each does roughly half the damage
  they do alone, with more weapons on screen.
- 150 ms is exactly the receive lead (three ticks at 20 Hz). A guest's hit is
  applied locally at once and reaches the host three ticks later — reliably inside
  the shadow of a host hit the guest could not have seen. The guest watches hit
  points fall, the host refuses the same hit, and the next correction puts them
  back. Repeatedly.

**In the logs.** At tick 600 the host had absorbed 1751 damage against swarms and
dealt 308; the guest 1046 against 198. `combat.rejects.immune` reads 408 on the
host and 235 on the guest, `kinetic_immune` 653 and 394. A high absorb ratio is
expected from a single player with every weapon; what the counters cannot show,
and the structure does, is which participant's hit each rejection belonged to.

**Fix.** The window stays the target's; its budget is now per attacker.
`CombatComponent.DamageImmunitySpent` is a bitmask of the roster slots that have
already landed inside the open window, with the top bit for an attack no cursor
owns. A hit is refused only when its own bit is set; a second attacker lands and
sets its bit without extending the window. One attacker is unchanged — the same
one hit per 150 ms — and one participant can no longer spend another's budget.
The mask travels with owner-authored cursor state and is in the shared combat
digest.

An attacker joining an open window late may land twice inside one duration. The
rate stays bounded at two hits per window per attacker, and the alternative is
sixteen timers on every storm member.

Species-authored invulnerability is a different thing and says so: quasar shields,
storm phase and the snake head call `SealDamageImmunity`, which opens a window no
attacker may spend.

**Since.** Kinetic immunity is per attacker as well, with the opener replacing the
velocity and every joiner adding to it, so a window composes to one vector in any
order. Kill credit follows the same set rather than the last writer (§8 gap 11 of
[Multiplayer](multi-player.md)).

**Incomplete.** This was the right change and not the whole one: the swarms that
would not die were not being refused damage, they were never asked. §7.1.

## 5. Issue 4 — the storm region retires as soon as it spawns

Not in either log: neither run reached three quasar kills. Two mechanisms are
present in the code, both consistent with "rare" and with "in sync on both
instances", since the authority authors the region and the FSM travels in the
correction.

**A refused spawn falls through the defensive guard.** `StormSetup` emitted
`EventStormSpawnRequest` and then waited a flat 500 ms before entering
`StormActive`, learning nothing about whether the storm exists. `spawnStorm`
returns without spawning when `findCirclePosition` fails for any one of the three
circles — a crowded map centre, which is what 5–10 swarms and a maze make — or when
`s.rootEntity` is still set. `StormActive` then finds `storm.active == false`,
waits its 2 s defensive timeout and takes `StormVictory`, retiring the region as if
the encounter had been fought. Every sibling region already handles this:
`MainSpawnGold` and `QuasarGoldSpawn` retry on `EventGoldSpawnFailed`, and
`QuasarFuse` waits for `EventSpeciesCreated` rather than for a clock.

**The escalation guards read state the correction drops.** `StormDecision`
branches on `kills.swarm >= 20` and `kills.storm >= 3`; `QuasarGoldCycle` on
`kills.quasar`; `MainCycle` on `kills.drain`. All are `kills.*`, which the capture
excluded (§1), and all are reset by an `on_enter`/`on_exit` action an installed
state never runs (shape A). The counters drift, and a region entered by install can
satisfy an escalation guard on its very first tick. In this run at tick 600 the
host held `kills {drain 12, quasar 1, swarm 8}` and the guest
`{drain 4, quasar 0, swarm 9}` — while both were in the same FSM state.

**Fix.** `StormSetup` now transitions on `EventSpeciesCreated` for the storm, admits
a receiver whose FSM was installed by testing `storm.active` instead, and otherwise
falls to `StormSetupRetry` and asks again — the shape `MainSpawnGoldRetry` already
has. `MetaSystem` carries the kill tallies, the combined defeat latch and the
multiplier, so guards read the same numbers on every instance. `StormSystem`
becomes a carrier for its root entity and pending blue spawns: without them a
receiver corrected past `StormSetup` held the storm's entities and simulated none
of them, because `Update` returns immediately on `rootEntity == 0`.

**Uncertain.** No capture of the failure exists, so neither mechanism is confirmed
as *the* cause, and both are addressed. `storm.spawn_failures` and `fsm.storm`
distinguish them in a field log: a spawn refusal increments the counter and shows as
`StormSetupRetry` rather than a silent 2.5 s in `StormActive`. `td`'s breach takes
the same retry (`StSpawn`).

## 6. What these fixes do not close

Closed since (2026-09-24):

- **The pruning rule.** A capture follows the epochs it fences on one ordered link,
  and every copy of a crossing now applies at one tick, so a fenced frame has been
  applied before the capture claiming it installs. A 3-tick latency mesh with both
  participants moving every other tick counted zero
  `network.artifacts_authority_superseded` in 1200 ticks; a non-zero count in a
  field log reopens the inventory.
- **`SharedKey` answering two questions.** Every excluded cell a shared guard or
  system reads — `kills.*`, `energy.damage_multiplier`, `session.all_defeated` — is
  carried and compared through `MetaSystem`'s record, and the other excluded groups
  are telemetry. A second predicate would be a second carrier for the same values.
- **`MonitorWarmup`**, which guarded on owner-authored keys and nothing entered, is
  deleted.

## 7. Second round (2026-09-10, after the above landed)

### 7.1 The undying swarm

Per-attacker damage windows (§4) were a real fix and the wrong suspect. A swarm
that will not die is not being refused damage; its own system never looks.

```go
// SwarmSystem.Update, before
if combatComp.StunnedRemaining > 0 { ...; continue }   // stun
if combatComp.HitPoints <= 0 { ...despawn...; continue } // hit points
```

A stunned swarm returned before the hit-point check, so it did not die however
much damage it took, and before `activeCount++`, so it left the tally as well.
The session log says both halves in one line: `swarm.count 0` beside
`combat.live.swarm 3`, with `swarm.spawned 31` and `killed_by_player 28` — three
swarms alive, unkillable, and invisible to the system that owns them.

Quasar and eye order the same two checks the other way round, with the stun
branch counting itself active. Swarm was the outlier.

The stun stayed on because `applyStunEffect` wrote
`StunnedRemaining = PulseStunDuration` unconditionally on every pulse hit — a
2-second window refreshed by each of the two participants in turn. That is the
timer reset §4 looked for and did not find: the reported behaviour was a stun
lock, not a damage window.

**Fix.** The hit-point and charge checks move ahead of the stun check and the
stun branch counts as active, matching quasar and eye. A running stun is no
longer refreshed: a second hit is refused (`combat.rejects.stun_immune`) rather
than extending the lockdown. Damage and kinetic windows already behaved this way
— neither is extended by a later hit, and after §4 damage is budgeted per
attacker as well.

### 7.2 Gold: what the spawn actually depends on

The hypothesis was that gold's spawn clearance reads both domains. It does not:
`findValidPosition` rejects a cell through `IsBlocked` → `HasBlockingWallAt`,
which views `ScopeShared` and matches only walls, and `PositionBatch.CommitShared`
gates on `HasAnySharedEntityAt`. Both were already shared-only, so neither is a
source of divergence.

What the search does read is **cursor positions**, and that is a source. An
ordinary crossing applies at once on its producer and a playout lead later
everywhere else (§3.1 of the plan), so at any tick the two instances hold the
guest's cursor at different cells. The exclusion band is `|dx| <= 5` **or**
`|dy| <= 3` around every cursor — roughly a quarter of the map per cursor — so
the two instances accept and reject different candidates. Each rejected candidate
had already consumed two draws from the shared gold stream, so a single
disagreement left the stream at a different position on each instance, and every
*later* sequence and position differed too, not just this one. That is the
"one gold here, another gold there" report, and it is why it did not settle.

**Fix.** Every candidate is drawn before any is examined, so the stream advances
by a fixed amount whatever the filters decide. A mispredicted gold is now one
gold's worth of divergence that the next correction closes, instead of a
permanently offset stream.

**Also fixed, as asked.** Gold now clears its footprint of player-domain
occupants before it takes the cells, the way every other shared spawner does
(D-12). It was the only shared spawn landing on top of each participant's own
glyphs, drains and nuggets — a different set on each instance inside a replicated
footprint. Shared occupancy stays `CommitShared`'s to refuse, which is what keeps
the change to the domain the report named.

**Ruled out since.** Neither of these explains a guest holding a gold the host had
destroyed, or two at once, and the last candidate, the selective repair, cannot
either: `ApplyShardSet` refuses a splice whose rebuilt root differs, and the root
counts each section's rows, so an entity only the receiver holds is dropped or the
repair is refused (pinned in `TestSeveralSectionsRepairWithoutAnUnrelatedOne`).
The typed member shown again inside the lead and the timer on a re-issued anchor
are both closed since: a typed member leaves at its agreed tick, and an install
cancels the timer of the gold header it replaces.

### 7.3 `scenario.sh drain`

Reworked rather than removed. It asserted that the drain "waited out its deadline
*holding* the guest", which needed a scripted guest to outlive a wall-clock
window. `ScriptDriver.applyCurrent` fails the run when the world tick has passed
an action's target tick — and a correction moves the world tick, so on a loaded
machine the guest ended itself and the scenario reported a failure the host had
no part in.

The scenario now asserts what the signal is actually promising: the probe reports
`live=true ready=false phase=draining`, the process survives the signal, **the
tick advances while draining**, and the session ends itself naming a reason. The
guest is no longer part of the claim. A scripted participant in a session now
schedules on the ticks it issues, so a jump or a reset no longer ends it.

## 8. Third round (2026-09-20, internet host, two remote guests)

Two of the three are one shape: a player-domain reader resolved the local cursor
from the shared store while the renderer drew it from the D-18 prediction, so
everything keyed to the drawn cell trailed the keystroke by a whole playout lead.
The third is the correction boundary seen from the other side — a player-domain
effect keyed to a shared entity the install removed.

### 8.1 The camera trails the cursor

`CameraSystem` anchored on `EventCursorMoved`, the announcement `CursorSystem`
makes when the crossing applies. On a guest that is a playout lead after the
keystroke: the cursor moved at once and the viewport followed ~150 ms later. The
camera is local view state (D-14) and D-18 already names it a reader of the
prediction, so nothing was being kept in agreement by the wait.

**Fix.** One anchor, `World.FollowLocalCursor`, reading the cell the renderer
draws. `predictCursorMove` calls it as the prediction advances, and an
announcement re-anchors through the same accessor rather than from its payload —
an older queued absolute cell would otherwise walk the viewport backwards. The
resize reflow and the `CameraEnabled` gate collapse into that path.

### 8.2 Nugget absorption waits for the announcement

`collectionCursor` tested the ember and shield ellipses against
`Positions.GetPosition` while `ShieldRenderer` and `EmberRenderer` draw them at
`CursorCell`, so the nugget sat visibly inside the shield for a lead before it was
absorbed. Nothing in the collection crosses — the nugget is a player entity and
the heat it pays is a `ClassLocal` write to an owner-authored component (D-13) —
so the delay bought no agreement. It was the store read, not a deliberate pairing
of absorption with the heat it grants.

**Fix.** Both cursor reads in `NuggetSystem` resolve through `CursorCell`.

### 8.3 A quasar zap that outlived its quasar

The tracked bolt is player-domain (D-6), keyed to the quasar header by `Owner`,
and ends when `QuasarSystem` pushes `EventLightningDespawnRequest`. A correction
removes a shared entity by writing the world, not by replaying its lifecycle, so a
guest whose predicted quasar the authority did not have kept a bolt with
`Duration == 0` and an hour `Remaining`: it drew from the dead header's last cell
for the rest of the session.

**Fix.** A tracked bolt lives on a lease its owner renews with every target
update (`LightningTrackedLease`). An install that removes the quasar, or clears
`IsZapping` under a live one, stops the renewals and the bolt retires within the
lease. `QuasarSystem` is shared-profile and may not read the player store, so the
lease, not a lookup, is the one mechanism.

## 9. Fourth round (2026-09-23, fleet, a terminal and a browser guest)

A terminal guest at 150 ms trailed its own shots by 300–500 ms, played worse while a
browser guest was present, and saw small corrections several times a second.
Measured with two scripted guests behind a 150 ms delay proxy on `main`:

- **One lead for everyone.** The authority set a session-wide lead from the worst
  link, so a stalling tab's inflated round trip raised everybody's to ten ticks. Each
  participant now stamps its own, and the authority commits (multi-player.md §3.5).
- **Unpaced clocks.** A lobby guest ran four ticks ahead of the authority and applied
  99.6% of its peer's crossings late; no manifest matched at the tick it described.
  Guests now pace against the authority's epochs.
- **Lossy projection.** A projection dropped peer crossings and raced a live tick;
  both closed (multi-player.md §3.3).
- **Shots from the shared cell.** Weapons read the store, a lead behind the drawn
  cursor; they read `CursorCell` now.

Hash-only corrections went from 0% to 85–94%, installs from four a second to about
one, and installs that moved a placement from half of them to 2–9%; a guest that
freezes 150 ms in every 400 raises only its own lead. Owner-authored syncs are
committed to a tick like crossings, which is what closed most of that last gap.

## 10. Fifth round (2026-09-24, fleet, a terminal and a browser guest on one machine)

The browser guest's round trip and lag swung widely and it dropped out; the terminal
beside it played normally. Measured with the browser build in headless Chromium,
websocat as the pod runs it, and a 150 ms delay proxy on `main`:

- **Go shared the page's thread with xterm.** Each new glyph and colour pair is a
  rasterise and readback in xterm's WebGL atlas, about a hundred a second here; a
  frame that stalls stalls every tick, probe echo and socket read with it. The
  program now runs in a Worker: round trip 390 → 176 ms, jitter 150 → 16 ms, late
  commits 158 → 9 and void 29 → 0 in 90 s, with the page equally saturated.
- **Loss that was latency.** A probe counted as lost when the next went out first,
  so every round trip over 200 ms read as up to half lost. It now counts once no echo
  has come back for `NetworkProbeLostAfter`.
- **websocat without `TCP_NODELAY`.** A minor share of the jitter; see `doc/todo.md`.

The session pod stayed near 7 ms of CPU per 100 ms throughout, far inside its limit.

After the fix the tower region still corrected every few seconds, and `td` slowed.
Measured with a scripted guest and the `-script` benchmark:

- **A local flag in the compared surface.** `context.map_locked` derives from a
  `CropOnResize` the capture leaves local, so a guest that joined after a level setup
  never matched: 0% hash-only in the tower region, now 59%. Entered with the guest
  present it is 77%, against 95% on the main map; the rest is prediction divergence
  among its interacting eyes, snakes and pylons (§11).
- **Walls probed per cell.** Flow fields asked the spatial grid and the wall store
  about every neighbour of every cell. They now read a grid built from the store once
  per derivation (`Position.WallTest`): the `td` script runs in 28 s, from 49.5 s,
  with identical stat records throughout.

A drop then stuck inside a wall. No field has ever covered a wall cell, and
`PushEntityFromBlocked` moved the grid cell but left the sub-cell position in the
wall, so a pushed kinetic entity walked back in. It now moves both, and loot pushes
itself out of a wall a correction installed under it. In the browser build in the
tower region, navigation is about 3% of the thread; answering manifests and
applying corrections is about half of it. §11 removed most of the repairs and
installs; the capture and hash every answer takes remain (`doc/todo.md`).

Once, in the tower region, a browser guest kept the previous region's glyphs, or
kept the glyph system running although the region disables it. It did not recur
and is noted rather than tracked; a second sighting should record whether that
guest had installed a capture across the region change.

## 11. Sixth round (2026-09-25, the tower region's hash-only rate)

Measured in-process: a host and a guest meshed on `main`'s tower region, both in
`:god`, moving and firing at random, the guest trailing the host by the link. Four
causes, fixed in order; the fraction is the guest's manifests answered hash-only.

- **The authority's cursor was compared.** Its owner-authored cells are a mirror on
  every guest, refreshed every `NetworkSyncTicks`, and a weapon cooldown or a
  shield's last drain moves every tick: 2% of manifests matched while both fought.
  Every participant's cursor is now outside the hashed surface (multi-player.md §4).
- **A dispatch phase that depended on traffic.** The wire phase settled only when
  this instance received something, so the last tick's leftovers were dispatched
  before `Time.Update` on one instance and after it on another; an eye's genotype
  took a spawn time one tick apart. In a session it now settles every tick.
- **A snake body aliased the live world.** `SnakeBodyComponent` had no
  `DetachSnapshot`, so a retained capture's segments kept moving after it was taken,
  and a repair served pages that no longer matched the hashes published for them.
- **Overlay flags in the compared surface.** `effects.strobe_active` and
  `effects.grayout_active` are `TransientSystem`'s, scoped to one cursor.

With all four, 599 of 599 at zero latency and 593 of 595 at three ticks
(`TestATowerGuestAnswersHashOnly` holds 95% at two).
