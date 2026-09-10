# Troubleshooting: two-instance defects (2026-09-10)

Defects observed with host and guest on one machine, so the receive lead is the
only latency in play. Sources are trace logs of a `-host` and a `-join` run; the
figures quoted are from them and the logs themselves are not kept in the tree.
Section 7 is a second round after the first five landed.

Three of the first four are the same defect wearing different clothes, so that
shape is stated once and each issue refers to it. The invariants are in
[Multi-instance domain model](domain-design.md); the operating contract is in
[Multiplayer architecture](multi-player-enhancement.md).

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

**Not fixed.** The rare inverse — a timer for a gold that is not there. A splash
anchors to a bare `core.Entity`, and a capture restores `NextEntity`, so an
install that rolls the allocator back re-issues shared ids. A splash whose anchor
died can therefore find a *different* composite under the same id and keep
counting. See `doc/todo.md`.

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

**Not fixed.** Kinetic immunity is still one window per target, and it suppresses
homing as well as knockback — a swarm under continuous two-player fire barely
steers. Whether two participants should be able to knock one body around twice as
hard is a physics question, not a networking one. See `doc/todo.md`.

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
as *the* cause. `storm.spawn_failures` and `fsm.storm` distinguish them: a spawn
refusal increments the counter and now shows as `StormSetupRetry` rather than as a
silent 2.5 s in `StormActive`.

## 6. What these fixes do not close

- The pruning rule in §1B is unchanged, and it is right for what it was written
  for. What is missing is an inventory of what else a host crossing does. Two such
  effects are now carried; the rest of the audit is open.
- `SharedKey` still answers "compare this?" and "carry this?" with one predicate.
  Two carriers work around it. Splitting the predicate is the real fix.
- `MonitorWarmup` guards on `player.0.heat.current` and `player.0.energy.current`.
  They are owner-authored (D-13), so a guest never writes them and the capture
  never carries them. The state is unreachable in the shipped config, so it is a
  latent D-20 hole rather than a live defect.

Each of these is one line in [TODO](todo.md).

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

**Not closed.** Neither of these explains a guest holding a gold the host has
destroyed, or two at once. Every destruction path pushes
`EventCompositeDestroyRequest` and clears the carrier's state, and
`ReconcileSharedWorld` removes shared entities the capture does not name, so a
stale sequence should not survive a correction. The remaining candidate is the
selective repair: an entity only the receiver holds must land in a page whose
hash differs, and the reconstruction must then drop it. That path is not proved
either way here. See `doc/todo.md`.

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
guest is no longer part of the claim. That a scripted participant cannot survive
a tick jump is real and is in `doc/todo.md`.
