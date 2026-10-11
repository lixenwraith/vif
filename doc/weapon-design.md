# Weapons: statuses, max-charge effects, the main gun

Working design for three linked changes: a status mechanism on combat entities
(stun today, burn next, slow later), what each weapon kind gains at its charge cap,
and the reshaping of the cleaner into the always-held main gun. §7 lists the
decisions the plan waits on; the sections before it give the recommended answer
and why. When the work lands this condenses into [Combat and weapons](combat.md).

## 1. What the code constrains

- 53 components and the position bit use 54 of the 64 mask bits. Nothing here adds
  a component: statuses live in `CombatComponent`, max-charge data in the
  `WeaponSpecs` row, the sustained ray in `RayComponent`, the beacon's style in a
  table by `SpawnType`.
- Charges only rise. `addWeapon` caps at `MaxCharges`; the one decrease is
  `removeAllWeapons` on an energy zero crossing. A max-charge effect is therefore
  a state the cursor keeps until it loses its loadout, not a buff with a lifetime.
- A cursor's hit on a Shared species crosses as an artifact (D-3): a direct request
  stamped to the target's domain, or explosion geometry re-derived on every
  instance. A status applied inside that resolution is applied identically
  everywhere and ticks in `combat.Update` from tick-derived time (D-21), so it
  needs no crossing of its own.
- `CombatComponent` carries five timers of three shapes: stun (target-wide, never
  refreshed), damage immunity (per attacker and family), kinetic immunity
  (target-wide, attacker recorded), hit flash (presentation), enrage
  (species-authored). The immunities are rate limits, not effects. Stun is the one
  effect, and burn is its sibling.
- The lightning zap is drawn by combat's energy-drain effect from the request's
  origin cell to the target: one direct request from any cell is already a bolt
  between two points.
- The shared digest names combat fields one by one. A new field is captured by the
  generated JSON on its own but compared only once `digest.go` names it.
- The ray renderer fades by `Remaining/Duration` and skips a zero `Duration`, so
  a ray that never ends needs a flag, not the lightning's zero-duration lease.

## 2. Statuses

One slot per kind in the combat component, no stacking:

```go
type StatusKind uint8

const (
	StatusStun StatusKind = iota // movement suppressed; replaces StunnedRemaining
	StatusBurn                   // periodic damage credited to Source
	StatusCount                  // StatusSlow and later kinds append here
)

type Status struct {
	Remaining time.Duration    // zero: inactive
	Period    time.Duration    // between ticks; zero: no periodic effect
	Timer     time.Duration    // to the next tick
	Magnitude int              // damage per tick; a slow's permille later
	Source    core.Entity      // crediting cursor; cursors are Shared, so capture-safe
	Attack    CombatAttackType // family the damage records under and immunity checks
}

// In CombatComponent, replacing StunnedRemaining
Status [StatusCount]Status
```

- **Apply.** One policy table by kind. Stun rejects while one runs: the no-refresh
  rule stays, a target under fire from two players still leaves the lockdown. Burn
  refreshes: `Remaining` and `Magnitude` take the larger, `Source` the refresher.
  Credit follows the last hit, as `LastDamagedBy` already does.
- **Tick.** In `combat.Update`, in the loop that ages the immunities. `Remaining`
  falls by `dt`; while `Period > 0` and `Timer <= 0`, `Timer += Period` and one
  tick of damage lands unless HP is 0 or `DamageImmuneTo(AttackerBit(source),
  Attack)`. A sealed window (species invulnerability) blocks a tick; a spent
  window is respected but never spent by a tick, so a player's own burn cannot
  block their direct hits. Dealt damage records under `Attack`, sets the hit flash
  and `LastDamagedBy` on the entity and, for a member, on its header. HP 0 retires
  through the species' own death check, as any hit does.
- **Expire.** The slot zeroes. Entity death takes it with the component.
- **Readers.** `Stunned()` replaces the six `StunnedRemaining > 0` reads; stun
  still clears enrage; burn leaves it. The digest names every slot field.
- **Profiles.** `CombatEffectBurn` joins the effect mask. Burn's figures are three
  constants (`BurnDamage` 1, `BurnPeriod` 1 s, `BurnDuration` 3 s) until a second
  burn source exists; the slot stores magnitude and period so the ticker has no
  per-kind switch. Over-time is an effect of a hit, not a way the hit reaches HP,
  so the unused `CombatDamageOverTime` resolver placeholder is deleted: the
  profile's `DamageType` keeps choosing between the direct and the area resolver.
- **Cursor later.** A cursor's status is owner-authored (D-13): it travels in
  `CursorStatePayload`, and its burn ticks `strikeCursor` on the owner. The slot
  layout needs no change for it.

Rejected: a status component (a mask bit, and a status with no combat component
has nothing to act on); per-player slots (`[kind][MaxPlayers]`, four times the
capture bytes for a credit question the last-hit rule answers); a dynamic list
(allocation on a hot shared component and an unbounded capture).

## 3. Max-charge effects

The `WeaponSpecs` row says what a kind does at its cap; zero values mean "as below
the cap", so a kind opts in by filling one field:

```go
MaxedAttack   CombatAttackType // family fired at the cap; CombatAttackNone keeps Attack
MaxedCooldown time.Duration    // replaces Cooldown at the cap
Stream        int              // bullets per main fire while the wave cooldown runs
ArcRange      float64          // cells a hit arcs to the nearest other target within
Sustained     bool             // the ray never ends
```

| Kind | Cap | At the cap | Row |
|---|---|---|---|
| Rod | 10 | each hit arcs to the nearest other target | `ArcRange` |
| Launcher | 10 | cooldown halved | `MaxedCooldown` |
| Disruptor | 1 → 2 | the pulse also burns | `MaxedAttack` |
| Turret | 5 | single shots between waves | `Stream` |
| Emitter | 3 | the ray stays lit and sweeps with the orb | `Sustained` |

### 3.1 Rod: arcs

In `fireLightning`, for each unique primary target: `FindNearestTargets` from the
primary's hit cell, count 2, both scopes, owner excluded; take the first whose
target is neither the primary nor another primary of this volley and whose
distance is within `ArcRange`; push one more direct request of the same family
with that cell as origin, stamped to the arc target's domain. The energy-drain
effect draws the zap from the origin, so the arc is a bolt from the first target
to the second with no new visual code. It also pays the drain reward, which
doubles the rod's energy income at the cap (§6).

No roll: a range gate is deterministic and costs no stream position; a 50 % draw
is a knob to add only if arcs prove too strong. A target hit this volley is
skipped rather than refused by its immunity window, which keeps the queue clean.
One hop; `ChainDepth` stays 0 because the arc is a new request, not a follow-up
of the hit. Cost: one store scan and sort per unique primary per volley, at most
ten per 500 ms, far below the per-tick scan a ray already does. `ArcRange` starts
at the orb's orbit radius, 12 cells.

### 3.2 Launcher

`MaxedCooldown` is half the launcher's. `fireAllWeapons` sets the cooldown from
the row and the charge count instead of the row alone.

### 3.3 Disruptor: the charged pulse

`MaxCharges` becomes 2; the orb shows `1` below the cap as every other kind does.
`MaxedAttack` is `CombatAttackPulseCharged`, a new family whose profile is the
pulse's (area, 1 damage, stun, kraken unstunnable) plus `CombatEffectBurn`. The
explosion crossing already carries the family, so the host and every guest derive
the same burn; telemetry gains `damage_family_pulse_charged`; the immunity array
grows by one family. A payload flag or a `Scale` threshold was rejected: it would
put the cap rule in combat instead of the table. The hosted disruptor is
untouched; a mount strikes cursors with `HostedDamage`.

### 3.4 Turret: the stream

At the cap, a main fire that finds the wave cooldown running fires `Stream`
bullets (1) at the nearest target with the usual spread, without resetting the
cooldown or flashing the orb; the flash stays the wave's cue. Under auto fire
main fire repeats every 250 ms and the wave every 500 ms, so the cadence is
5, 1, 5, 1 with no silent gap. No timer is added: main fire is the stream's
clock. The shared target query already sizes itself to the largest ready loadout;
a streaming turret contributes `Stream`.

### 3.5 Emitter: the sustained ray

`RayComponent.Sustained`. At the cap `advanceRays` lays a ray on the orb whenever
none exists: phase firing, `Scale` the charge count, `Duration` and `Remaining`
set to `RayDuration` and never decremented, so the renderer draws it at full
strength. `fireRay` at the cap returns false: main fire neither resets the
cooldown nor flashes the orb, and no target is needed. The ray leaves with the
orb on an energy zero crossing and only then. It strikes every tick as today, and
a crossing is pushed only while the band covers a Shared target. The todo's
"spokes" item builds on this and stays deferred. At the cap this is 3 damage per
covered member per immunity window, continuously (§6).

### 3.6 Presentation

The orb already swaps its digit for the bullseye at the cap; no further cue. The
arc is a zap between two species. A burn shows as the hit flash each tick; a tint
is tuning.

## 4. The main gun

### 4.1 Remove the sweeping cleaner

Two producers: the overheat burst in `heat.go` and the `:cleaner` command. Remove
`spawnSweepingCleaners`, `scanTargetRows`, the event and its payload, the command
and its test; the burst keeps its flash and `EventHeatBurst`. About 150 lines,
no replacement: in no current scenario is it an effective attack.

### 4.2 Nugget beacon: a converging materialize

Materialize already converges: fill runs from the map edges to the target, holds,
then recedes toward the target. The beacon becomes `EventMaterializeRequest` with
`SpawnTypeNugget` from the nugget's cell every `NuggetBeaconInterval`; the
drain's completion handler already filters on its own type. Style is one table
by `SpawnType`: duration and the fill and hold fractions in `parameter`, the
colour in `visual`. A zero entry falls back to today's values, so drain and swarm
are unchanged; the nugget's is 600 ms, fill to 0.5, hold to 0.6, nugget orange.
`CleanerColorNugget`, its gradient and the cleaner's visual-only branch go.

### 4.3 The directional cleaner as a weapon kind

The cleaner joins `WeaponSpecs` as the kind every cursor holds:

- `WeaponGun` (§4.4), `DeliveryTracer`, `CombatAttackTracer` (today's
  `CombatAttackProjectile`, renamed). `Cooldown` 0: it fires on every main-fire
  trigger, and `MainFireCooldown` stays the trigger's gate. `MaxCharges` 3: one,
  two or three parallel lanes per direction (offsets 0; ±1; −1, 0, +1), which is
  the split for free; a spiral is a renderer idea for later.
- `Innate` on the row: granted at cursor spawn, kept by `removeAllWeapons`, never
  dropped by loot (no `LootSpecs` row). A pacifist region or a typing-only
  scenario takes it away with `EventWeaponRemoveRequest{Entity, Weapon}`, the
  first weapon event a level can raise to remove a kind; `EventWeaponAddRequest`
  gives it back.
- `FromCursor` on the row: no orb; the shot leaves from the cursor's cell, which
  `emitterCell` already falls back to.
- `Directions`, a cardinal mask with all four set. The scenario override (one
  direction for a side-scroller) waits on scenario authoring; the spawn takes the
  mask now so that is one field later.
- `CleanerSystem` stays as the delivery system, as `bullet` and `missile` are for
  theirs: the loadout fires it through `EventCleanerDirectionalRequest` and it owns
  the projectile's lifecycle. Folding it into `weapon.go` would move 300 lines of
  swept traversal for no gain. Its `dual` domain stays.
- Mounted: no hosted range or damage; `MountSystem.attach` rejects a kind without
  them. A hostile tracer is a later row edit.

### 4.4 Name

533 references in 40 files, most in the four cleaner-named files the merge
renames anyway; the rest are docs and telemetry keys. The rename rides the merge
PR. Kinds are devices and deliveries are effects (rod and lightning, turret and
bullet), so the pair should read the same way:

| Kind | Delivery | Reads as |
|---|---|---|
| Gun | Tracer | a gun fires tracers: a fast streak with a glowing tail, which is the visual |
| Lancer | Lance | a straight directional strike; "lance" has no trail connotation |
| Blaster | Streak | generic; "blast" collides with the explosion vocabulary |

Gun and tracer are recommended.

## 5. Plan

One task, one PR. §3 rests on §2, so they are one PR with one commit per step;
§4 is two more.

1. **Max-charge effects** (this branch): statuses with stun migrated; the charged
   pulse and burn; rod arcs; launcher and turret; the sustained ray; docs condensed
   into `combat.md` and `telemetry-audit.md`. Tests, one per rule: a burn ticks
   and credits its source; a sealed window blocks a tick and a spent one is not
   spent; a running stun is not refreshed; the digest names the slots; an arc
   skips this volley's targets and respects its range; a sustained ray persists
   without a target and main fire leaves it alone; a stream shot fires between
   waves; the charged pulse crosses as its family.
2. **Sweep removal and nugget beacon** (§4.1, §4.2).
3. **Main gun merge and rename** (§4.3, §4.4).

## 6. Tuning after play tests

- Arc reward: whether an arc should pay the full drain reward, half, or none.
- Sustained ray scale: the charge count or a lower `RaySustainedScale`.
- Burn figures, and whether the charged pulse's stun should shorten to offset it.
- `ArcRange`, `Stream`, and whether a stream shot should flash the orb faintly.
- Chain visuals: `LightningZapDuration` is two ticks; an arc may want longer.

## 7. Decisions

Each with the recommended answer first.

1. Burn credit on refresh: the last refresher, or the first applier.
2. Stun moves into the slot array now, or stays a field beside it.
3. The charged pulse is its own attack family, or a flag on the explosion payload.
4. Arcs are range-gated only, or also rolled at 50 %; arcs pay the drain reward.
5. Sustained ray damage scales with charges, or is fixed at 1.
6. The turret stream rides the main-fire clock, or gets its own timer.
7. The sweeping cleaner is removed, with the `:cleaner` command.
8. The main gun is a weapon kind with its delivery system kept, or is folded into
   `weapon.go`, or stays outside the loadout.
9. Gun and tracer, lancer and lance, or blaster and streak.
10. The split in §5, which needs two more branches after this one.
11. An energy zero crossing keeps the gun.
