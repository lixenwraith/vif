# Troubleshooting: four two-instance defects (2026-09-10)

Four defects observed with host and guest on one machine, so the receive lead is
the only latency in play. Sources: `bin/vif -host :7777 -lv trace -ls all`
(participant 1) and `bin/vif -join :7777` (participant 2), captured as
`tmp/vif-log-260910-0227{17,18}.jsonl`.

Three of the four are the same defect wearing different clothes, so that shape is
stated once and each issue refers to it. The invariants are in
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
