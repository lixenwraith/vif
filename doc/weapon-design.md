# Weapons: statuses, max-charge effects, the main gun

Working design for three linked changes: a status mechanism on combat entities
(stun today, burn next, slow later), what each weapon kind gains at its charge cap,
and the reshaping of the cleaner into the always-held main gun. §7 records the
decisions taken; §5 is the order the work lands in. When it lands this condenses
into [Combat and weapons](combat.md).

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
- **Profiles.** A burn is its own attack family, `CombatAttackBurn`, whose profile
  has `DamageType: CombatDamageOverTime`: `DamageValue` is the damage per tick and
  the profile's `Period` and `Duration` are the slot's. It reaches a target as a
  chained direct request, so the direct resolver routes an over-time profile to
  the slot instead of to HP. Ticks record under the burn family and check
  immunity against it: nothing spends that family, so only a sealed window
  (species invulnerability) blocks a tick or an application. The slot stores
  magnitude and period, so the ticker has no per-kind switch.
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
MaxedCooldown time.Duration // replaces Cooldown at the cap
Stream        int           // bullets per stream shot while the wave cooldown runs
ArcRange      float64       // cells a hit arcs to the nearest other target within
Sustained     bool          // the ray never ends
```

A charged hit is the fifth: every delivery learns whether its weapon is at the cap
and the pulse carries it as `Charged` on its artifact (§3.3).

| Kind | Cap | At the cap | Row |
|---|---|---|---|
| Rod | 10 | each hit arcs to the nearest other target | `ArcRange` |
| Launcher | 10 | cooldown halved | `MaxedCooldown` |
| Disruptor | 1 → 2 | the pulse also burns | `Charged` on the pulse, chain in the profile |
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

No roll: a range gate is deterministic, costs no stream position, and leaves the
outcome to positioning rather than luck. A target hit or arced to this volley is
skipped rather than refused by its immunity window, which keeps the queue clean.
`FindNearestTargets` lists composites before singles, so the arc keeps the rod's
own priority; the candidate count is the volley's primaries plus one so an unhit
candidate is always among them when one exists.
One hop; `ChainDepth` stays 0 because the arc is a new request, not a follow-up
of the hit. Cost: one store scan and sort per unique primary per volley, at most
ten per 500 ms, far below the per-tick scan a ray already does. `ArcRange` starts
at the orb's orbit radius, 12 cells.

### 3.2 Launcher

`MaxedCooldown` is half the launcher's. `fireAllWeapons` sets the cooldown from
the row and the charge count instead of the row alone.

### 3.3 Disruptor: the charged pulse

`MaxCharges` becomes 2; the orb shows `1` below the cap as every other kind does.
At the cap the pulse's explosion artifact and the area requests derived from it
carry `Charged`. The pulse profile gains `Charged: &burnProfiles[d]`, the chain a
charged hit adds beside the unconditional `Chain` every profile may have; combat
emits it per hit entity exactly as it emits `Chain`, as a direct request of the
burn family stamped to the target's domain (D-5: derived, never transported).
The direct resolver sees `CombatDamageOverTime` and refreshes the burn slot on
the member an ablative hit named, or the target otherwise. The host and every
guest derive the same burn from the same artifact; telemetry gains
`damage_family_burn`; the immunity array grows by one family. The hosted
disruptor is untouched; a mount strikes cursors with `HostedDamage`.

### 3.4 Turret: the stream

At the cap the turret streams on its own clock: `WeaponComponent.Stream`, one
timer per kind beside `Cooldown`, armed by a wave and ticked in `Update` only
while that wave's cooldown runs. Each `TurretStreamInterval` (250 ms) it fires
`Stream` bullets (1) at the nearest target from the orb with the usual spread,
without resetting the cooldown or flashing the orb; the flash stays the wave's
cue. Waves every 500 ms give the cadence 5, 1, 5, 1 with no silent gap, and a
player who stops firing gets at most one trailing stream shot. The timer is
owner-authored like the cooldowns and travels with them in `CursorStatePayload`.

### 3.5 Emitter: the sustained ray

Below the cap nothing changes: duration and damage scale with charges and the
cooldown runs from firing. At the cap the ray is continuous. `RayComponent.
Sustained`: `advanceRays` lays a ray on the orb whenever none exists, phase
firing, `Scale` the charge count, `Remaining` held at `Duration` so the renderer
draws it at full strength; a timed ray still burning when the last charge
arrives is converted in place, so there is no gap or flicker between the last
timed cycle and the sustained one. `fireRay` at the cap returns false: main fire
neither resets the cooldown nor flashes the orb, and no target is needed. The ray
leaves with the orb on an energy zero crossing and only then. It strikes every
tick as today, and a crossing is pushed only while the band covers a Shared
target. The todo's "spokes" item builds on this and stays deferred.

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
- `CleanerSystem` dissolves into `weapon`, as the ray already is: the loadout
  spawns tracers in `fireAllWeapons` and advances them in `Update`, in a second
  file of the weapon system (`internal/system/tracer.go`) holding the swept
  traversal and the glyph effects. One system owns every cursor discharge, the
  manifest loses a system and an event, and a region that removes the gun has
  nothing else to disable. Tracers are Player-domain, as the fire path already
  made them; the `dual` stamping goes with the event.
- Upgrades: no `LootSpecs` row yet, so lanes come from `EventWeaponAddRequest`
  raised by a scenario (`EmitEvent` takes any authorable payload) or god mode.
  A drop row is a later addition.
- Mounted: no hosted range or damage; `MountSystem.attach` rejects a kind without
  them. A hostile tracer is a later row edit.

### 4.4 Name

533 references in 40 files, most in the four cleaner-named files the merge
renames anyway; the rest are docs and telemetry keys. The rename rides the merge
PR. Kinds are devices and deliveries are effects (rod and lightning, turret and
bullet), so the pair should read the same way:

Kind `Gun`, delivery `Tracer`: a gun fires tracers, a fast streak with a glowing
tail, which is the visual. `WeaponGun`, `DeliveryTracer`, `TracerComponent`,
`CombatAttackTracer` (today's `CombatAttackProjectile`), the `tracer` renderer,
`Tracer*` parameters and colours, `weapon.gun*` telemetry; `:auto main` keeps
its name and `gun` replaces the `cleaner` alias.

## 5. Plan

Three branches, each starting from the one before it, merged in this order:

1. **This branch**: the design.
2. **Untangle the cleaner**, two commits: the sweep removal with its command and
   producer (§4.1); the nugget beacon as a materialize (§4.2). After it the
   cleaner has one producer, the gun's fire path.
3. **Weapons**, one commit per step: statuses with stun migrated; the burn
   profile, the charged pulse and the second disruptor charge; rod arcs; launcher
   and turret; the sustained ray; the cleaner folded into `weapon` as the tracer
   gun with the rename; docs condensed into `combat.md`, tuning to `todo.md`, and
   this document deleted. Tests, one per rule: a burn ticks and credits its last
   refresher; a sealed window blocks a tick; a running stun is not refreshed; the
   digest names the slots; the pulse chains a burn only when charged; an arc skips
   this volley's targets and respects its range; a stream shot fires between
   waves and stops with the cooldown; a sustained ray persists without a target
   and main fire leaves it alone; the gun survives an energy zero crossing and a
   remove request takes it away.

## 6. Tuning after play tests

- Arc reward: an arc pays the lightning's energy reward like any hit; halve it if
  the cap doubles the rod's income too far.
- Sustained ray: 3 damage per covered member per immunity window, continuously.
- Burn figures, and whether the charged pulse's stun should shorten to offset it.
- `ArcRange`, `TurretStreamInterval`, `Stream`, lane offsets at two gun charges.
- Chain visuals: `LightningZapDuration` is two ticks; an arc may want longer.
- A burn tint on species renderers; the hit flash per tick is the only cue now.

## 7. Decisions

Taken, in review of the first draft.

1. Burn credit on refresh goes to the last refresher.
2. Stun moves into the slot array now.
3. The charged pulse is a `Charged` flag on its artifact and a chained over-time
   profile, not a second pulse family: the chain already exists for follow-ups
   and an over-time damage type is what the placeholder was for.
4. Arcs are range-gated only; nothing random decides a hit. Arcs pay the usual
   lightning reward.
5. Ray duration and damage keep scaling with charges; the cap makes it continuous.
6. The turret stream has its own timer, at the main-fire rate but not wired to it.
7. The sweeping cleaner goes, with the `:cleaner` command and the burst producer.
8. The cleaner folds into the weapon system, which also lets `fireAllWeapons`
   and its neighbours be named for what they now do.
9. Gun and tracer.
10. Two branches after this one, in the order of §5.
11. An energy zero crossing keeps the gun, as it keeps the cleaner today.
