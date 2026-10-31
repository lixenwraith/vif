# Navigation

How species find their way: flow fields and the grids they run on, passability
tiers and what a wall change does to them, the throttle whose phase is shared
state, target groups and the private group each cursor has, the snake lock,
loot's seam, route graphs and their corridors, and what each costs. Route
*learning* and genetics are in [AI, physics, and evolution](ai-physics-and-evolution.md);
the domain rules that bound a shared derivation are D-17 and D-19 in
[Domain design](domain-design.md#d-17--throttle-phase-is-state).

## 1. Structures

`pkg/navigation` is a leaf over `pkg/vmath`: it takes grids, cells and callbacks
and never reads the ECS. `NavigationSystem` (`internal/system/navigation.go`) is
the adapter: it reads the walls and the cursors, keeps the structures below, and
writes each navigator's component once a tick. Species systems turn that output
into motion; loot reads one resource; `AdaptationSystem` reads route graphs.

| Structure | What it is | Readers |
|---|---|---|
| `SearchGrid` | One passability read into a bordered grid with the open directions of every cell precomputed; every Dijkstra on that tier runs on it | fields, reaches, route solver |
| `FlowField`, `FlowFieldCache` | A field toward a target group's cells, in a cache that throttles its recompute | species through `NavigationComponent` |
| `Footprint`, `CompositePassability` | A body's cells around its header, and the header cells at which they fit | each composite tier's grid |
| `Reach` | One search from a cell measuring its distance to several goals | the snake lock |
| `RouteGraph`, `CorridorField` | A gateway's distinct corridors to its target, each with a field over its corridor alone | routed eyes, adaptation |
| `TargetResource`, `RouteGraphResource`, `PursuitResource` | The resolved groups, the graphs by gateway, the field toward each cursor | species, gateways, loot |
| `NavigationComponent` | Footprint, lock, route and this tick's output | species systems |

## 2. Flow fields

A `FlowField` runs weighted Dijkstra outward from one or more target cells and
stores, for every reachable cell, its distance and the direction that descends
toward a target.

| Step | Weighted cost |
|---|---|
| Horizontal | 10 |
| Vertical | 20 |
| Diagonal | 22 |

The costs come from the cell aspect (`navigation.CostsFor`): 10, 20 and 22 at a
terminal's 2:1, equal on both axes for a square cell. Eight-way movement is
allowed, but a diagonal cannot cut a corner either cardinal neighbour of which is
blocked. A blocked target cell seeds the first passable ring around it, out to
eight cells, as virtual targets at axis-weighted gap costs, so a large actor
navigates toward an occupied anchor without pretending its footprint fits there.
`DirNone` marks a blocked or unreached cell and `DirTarget` a seed.

Every Dijkstra in the package, the route search included, runs on a
`SearchGrid`: the wall test asked once per cell into a grid with a blocked
border, the directions open from each cell as one precomputed mask with the
corner rule folded in, so settling a cell is one load, and, since step costs
are small integers, a ring of distance buckets rather than a heap, which settles
a cell in constant time. The grid moves with the walls (`Move`), remasking each
cell within a step of a change once. A field computes on a grid it is handed
(`ComputeOn`); one reading its own walls (`Compute`) fills a private grid. A
field's arrays are allocated by its first compute, so a field nothing reads
costs nothing. Generation stamps mark cells current without clearing the arrays.

A cell reached two shortest ways takes the lower-numbered direction, so a
direction is a function of the distances alone. That is what makes repair
exact: after cells open, `Opened` settles only what the openings improve, from
the opening and the neighbours within a step of it, and ends with the field a
recompute would give, bit for bit. It declines, for a recompute, where an
opening lies on or within the seed ring of a target. The field is fuzzed
against a reference heap search, and the repair against a recompute.

## 3. Passability tiers and wall changes

Point passability is a wall-free cell. A larger body needs more:
`CompositePassability` holds, for every header cell, whether the whole footprint
fits, its cells placed by the header's offset from their top-left, so a path over
it guarantees footprint clearance. Each footprint navigation steers by is a tier
with its own passability and `SearchGrid`: the point tier; the eye's 5×3, which
every body it holds around the same header rides, the quasar, the snake head, the
4×2 swarm and vanguard's raiders among them; and one for each other footprint a
navigator declares (`NavigationComponent` carries the header's offset) or a route
graph is requested for, derived whole as it is first needed and dropped once
nothing needs it (`syncTiers`). Riding the eye's tier keeps one composite tier's
upkeep and fields; it only refuses a gap a smaller body fits.

Navigation keeps the walls everything stands on and brings them up to the walls
once a tick, re-testing only the cells the index's block log names
(`Index.BlockChanges`): a moving map costs the cells that moved, not the map.
When the log no longer reaches back, after an install, a resize or a restore, it
takes the walls' bits whole (`Index.BlockBits`) and diffs them a word at a time
(`position.Diff`). A changed cell moves the point tier. A composite passability is
a bit a header, derived from the walls' bits a row of words at a time (`Compute`),
and a change derives again only the header rows, and the words of them, the
changed cells' footprints reach (`Moved`), so a composite tier moves only where
its footprint's fit changed,
which a storm's single-cell holes rarely do; a header two cells turned the same
tick is named only if its fit did change. Each tier has a generation that a field
or route graph is stamped with when computed. A wholesale change, more than an
eighth of the map — the first derivation, a resize, a maze — derives everything
whole: moving the grids costs as much at an eighth when the turned cells are
scattered, each refitting a whole footprint of headers, and a cave's, a few to a
column, cost a tenth of that. Between ticks a route graph request brings
passability up to date first. A scrolling cave, which turns about 400 cells a
column on the largest map, costs 71 µs a step this way where the re-read and diff
cost 230 µs; one wall moving on an open map costs 3.6 µs at any size
(`BenchmarkCaveStepNavigation`, `BenchmarkNavigationWallMoves`).

What a tier's change does to its fields: a cell closed drops them (`Closed`),
since a direction may now point into it, and they recompute when next read;
cells that only opened repair a computed field in place (`Moved`), which is not
a recompute and leaves the throttle phase alone. Dirtying a field instead was
rejected: a field left standing on an older grid has no reproducible input at an
install, so a joiner would steer differently until the cooldown passed.

## 4. Throttle, phase and laziness

Computing a field over a large map every 50 ms tick is too expensive, so
`FlowFieldCache` throttles. A field falls due:

- at once when it has no field, the target count changes, or a cell of its tier
  closed;
- after a cooldown (`NavFlowMinTicksBetweenCompute`, 5 ticks), when marked dirty
  (a target group update, and for composite fields a regraph event) or a target
  drifted the Manhattan dirty distance (`NavFlowDirtyDistance`, 5 cells);
- after `NavFlowSettleTicks` (20) for a smaller drift: a field a few cells off
  points the same way everywhere but beside the target, where sight takes over.

A cursor's motion reaches a field only through that drift. The caches are lazy:
a field that falls due is computed when something first reads it that tick — a
pursuer without sight of its target, a drop out of sight of its owner, the debug
view — so a group nobody chases costs a cheap reachability check. The throttle
decides exactly as it would eagerly, which is what keeps the phase identical
between an instance that reads a field and one that does not (D-17). The
carrier (`SaveShared`) holds each group's counters, pending flag, computed flag
and last targets, the route rebuild budget, each gateway graph's endpoints and
every resolved target group, since one an update set and no anchor holds is in
no other state; an install re-derives passability and the grids from the installed walls,
rebuilds every field whose stamp is not its tier's current generation or whose
targets differ, keeps the rest, and derives the named graphs. The fields
themselves are never carried.

A reader out of sight reads its field every `field_refresh_ticks` (the scenario's
`[navigation]`, every tick when unset) and holds the way it read between: a
navigator its `FlowX`, `FlowY`, a drop its `LootComponent.FlowX`, `FlowY`. One that
went out of sight since the last read holds none and homes on its target, as one in
sight does. Over a moving map, where a closing drops the fields every tick, a field
is then computed once in as many ticks. What a reader holds is component state a
capture carries, and the reading ticks are navigation's phase (`refresh_ticks`).

## 5. Target groups

A target group is one navigation goal for many actors. Group 0 is the live
cursor roster up to the eight-target cap (`MaxTargetsPerGroup`); script groups
1–15 (`MaxTargetGroups` is 16) hold up to eight `TargetAnchor` entities, follow
one entity, hold a fixed cell, or mirror the roster. A field seeded from a group
selects the nearest reachable target at each cell, which is the right answer for
a hostile species and the wrong one for anything that belongs to a particular
participant.

Every cursor on the map also has a private group (`MaxTargetGroups` + roster
slot), the same on every instance so the phase each carries agrees; its point
field is published through `PursuitResource` and its composite field steers the
snake heads locked on it. A private group costs nothing until read.

`NavigationComponent` records the footprint, whether an actor follows a group
field or a route, the graph and route IDs, a snake head's lock, and each tick's
output: `HasDirectPath` when the target is in sight, the body flown from its
header along the line with every cell of it clear, otherwise the flow vector, bilinear over the four cells around a
point and header-anchored for a composite, with the neighbour nearest the goal
as the escape from a cell the field does not cover. Species systems turn that
into a homing target through `ResolveMovementTarget` (`targeting.go`) — the
target itself in sight, else a point `FlowLookahead` cells along the flow — and
integrate their own motion.

## 6. The snake lock

A snake head locks the cursor it reaches soonest (`LockedTarget`). One `Reach`
from the head over its tier's grid measures every cursor at once — what a
field seeded at each cursor would read at the head, since a step costs the same
both ways; a blocked cursor cell from its first passable ring, a blocked head
cell from its neighbours a diagonal away — stopping once every cursor is
settled. It runs when the head has no reachable lock, the roster moved enough
for its group field to fall due, or `NavFlowSettleTicks` passed; `LockTicks`
rides the head's component, so the capture carries the phase. A reachable lock
holds until another route is over a third shorter, a cursor the head cannot
reach is never chosen, and a head that can reach none holds still. Sight is
deliberately not a factor: a cursor jumps at no cost, so a head that switched to
whichever cursor it could see would be switching all the time.

## 7. Loot's seam

A drop is owned by the cursor whose kill produced it, is player-domain, and only
that cursor may collect it. Steered by group 0 it would walk to whichever cursor
is nearer and stall there, and a line-of-sight flag read against that cursor
would drive it into the wall between it and its own. A drop therefore steers by
its owner's private group through `PursuitResource`, with its own line-of-sight
check against the same cursor. A field is computed when first read, so only the
instance holding a cursor's drops ever materializes it; of the field, a capture
holds only the way a drop read between refreshes (§4). No field covers a wall
cell, so a drop a wall lands on without displacing it — a correction installs
shared walls under player-domain loot — is pushed out as a spawn displaces it.
Anything else that becomes owned rather than contested reads the same resource;
do not add a target group per participant.

## 8. Route graphs

A route graph is several meaningfully distinct, near-optimal corridors between a
gateway and its target: a weighted Dijkstra sets the optimum; a path within the
tolerance is accepted, its dilated corridor penalized, and the search rerun to
push the next candidate elsewhere; a candidate overlapping accepted corridors
past the limit is rejected; the graph stops at the route limit, the attempt
limit, no path, or excessive true cost. A graph's searches share one
`SearchGrid`, read once per graph, with the bucket ring sized past the largest
penalty a cell carries; the search is fuzzed against relaxation to a fixpoint.

Each accepted route stores decimated waypoints, true weighted distance, a
normalized inverse-distance initial weight, and its `CorridorField`: the
corridor's cells with their distance and step, searched on the graph's grid with
everything outside the corridor a wall, a diagonal's corners included, and kept
sorted by cell, so a route costs its corridor rather than the map. The graph
records the body it routes and its tier's generation it was computed on: a
request names the body (`width`, `height`, `header_x`, `header_y`), the eye's when
it names none, and a gateway's routes are for its eyes.

A gateway spawned to route by graph requests one at once
(`EventRouteGraphRequest`); graphs live in `RouteGraphResource` by the gateway's
`RouteDistID`, and each computed graph (`EventRouteGraphComputed`) starts its
adaptation entry, from which spawns pop a route. A route steers only while its
graph is fresh — computed at its tier's current generation, so a wall
change that fits no footprint differently leaves it fresh, toward a cell a live
group target still occupies — and only while the actor is inside its corridor;
otherwise the actor steers by its group field. Navigation rebuilds at most one
stale graph per `route_rebuild_ticks` (the scenario's `[navigation]`,
`NavRouteRebuildInterval`'s 20 ticks when unset), and `EventNavigationRegraph`
drops every graph and route assignment for that budget to rebuild. A graph to a
moving goal is a trap: it is stale as soon as the goal moves. Fields are for
moving goals, graphs for fixed ones.

## 9. Debug views and telemetry

`:flow [group]` draws a group's point or composite field and `:graph` a gateway's
routes through the debug renderer; neither is a feature. `nav.entities` counts
navigators, `nav.recomputes` the fields that fell due this tick, `nav.roi_cells`
the header cells rechecked for wall changes, and `nav.buf_groups_hwm` the most
groups held.

## 10. Costs

Measured native on the scenario maze shape (cell 5×3, braiding 0.5) and on a
headless td run with eight cursors walking a cell a tick, the monitor stopped
and towers healed, 100–340 eyes, two to seven snake heads, a storm raised.

```
Isolated                      tower 239×64   td 500×250
composite passability, whole  0.4 ms         2.5 ms
point field, 8 targets        0.5 ms         3.9 ms
composite field, 8 targets    0.3 ms         1.5 ms
route graph                   1.5 ms         3.8 ms
route storage                 22 kB a route  36 kB a route

td run                             Reading
play, a tick on average            4.4 ms
storm, a tick on average           6.4 ms
storm, the worst tick              30 ms
fields computed in the worst tick  2
```

The cost model: a group field toward static anchors is computed once
and repaired or rebuilt only as its tier's passability moves; group 0's fields
and each cursor's private field recompute as cursors move, at the cooldown; the
snake lock scales with heads, one reach each on its throttle; a drop's field
costs its owner's instance alone; a route graph is rebuilt only when the
composite passability changes and at most one per twenty ticks. What the storm
tick still holds is one point field per local cursor with drops in flight, the
reaches, and composite line of sight. td's worst ticks are walls going, not
fields: under a raised storm with eight cursors walking, the ticks where hundreds
of walls go reach 40–60 ms and compute no field, the wall system's event peaking
at 34–51 ms and navigation at 15–17 ms, and no tick computes more than four
fields.

Vanguard's jagged cave steps a column every tick, and more in a later cave. At
500×254 a step turns about 350 cells and the wall upkeep is 71 µs
(`BenchmarkCaveStepNavigation`); a tick with eight composites, most of them out of
sight of the cursor, costs 180 µs, and 103 µs with them near it
(`BenchmarkCaveNavigation`). In play a step turns more than a fiftieth of a small
map on a third of the ticks. Played headless as a player plays it (`:god`, the
ship sweeping from the middle to near the left edge every three seconds, firing),
navigation took 95–100 µs a tick at 120×40 and 220–245 µs at 200×60 while a
fiftieth derived whole, two thirds of it those derivations, and takes 27–43 µs and
64–79 µs at an eighth; at 400×120, 145–165 µs before and 90–120 µs.

What is left is the fields. A cell closes every tick, so a raider out of sight
computes its group's composite field on every tick it reads one, a fifth of the
ticks at 120×40 and a seventh at 200×60, and a drop out of sight its owner's point
field. Vanguard reads them every fourth tick (`field_refresh_ticks`, §4), which
computes a quarter of them. Medians of nine games of 6,000 ticks played as above,
navigation and the whole tick, in µs:

```
         main         unset        refresh 4
         nav   tick   nav   tick   nav   tick
120×40    91    340    34    276    23    278
200×60   182    567    72    492    46    448
400×120  147   1137    98   1081    80   1017
```

The direct path is the body flown from its header along the line of sight. In
16 such games of 2,400 ticks, 8 seeds at 120×40 and 200×60, the box centred on
the line and accepted turned 90° that preceded it called 71% and 78% of chase
ticks direct with 1.6% and 1.2% of those blocked for the body as it stands; the
body flown as it is calls 73% and 79% direct with 0.3% and 0.1% blocked, the
rest the body's motion within the tick after navigation decided
([Position §5](position-package.md#5-costs)).

Over nine such games at 120×40 and 200×60 for each refresh of 1, 4, 6, 10 and
1,000, the raiders' contacts, charges, teleports, kills and wall hits, the share of
their chase ticks out of sight spent standing (11–15%) and the length of a chase
out of sight (7–8 ticks) stay within the spread between games; a raider stuck out
of sight a second, about 1% of those ticks reading every tick, is rarer reading
every fourth or thousandth. Out of sight a third of a second at a time, a raider
steers alike by a field a few ticks old.

## 11. Custom maps

- A gateway with `use_route_graph = true` feeding a moving group (cursors) is
  rebuilt every twenty ticks for nothing; feed it a tower, or let it steer by
  field.
- A group with several anchors gets routes to its first anchor only
  (`resolveTargetPosition`); the field picks the nearest, so an eye off its
  corridor may change goal.
- A body the eye's 5×3 holds around its header rides the eye's tier, so it never
  takes a gap narrower than an eye's; any other footprint costs a tier of its own
  while it navigates, its passability moved on every wall change and a field for
  each group.
- Eight targets a group, sixteen groups, sixteen private groups; a map's size
  scales every field, and every instance computes the same ones.

## 12. Decisions

- Fields and route graphs stay separate: a field is the way to the nearest goal
  from everywhere, a graph several corridors from one source kept apart for the
  bandit; the point and composite tiers are searches over different graphs, and
  seeding one tier from the other saves nothing, since a Dijkstra costs what it
  settles.
- The snake lock is sticky and sight stays out of it (§6).
- An opened cell repairs a field rather than dirtying it (§3), and a graph's
  freshness is its tier's generation rather than a corridor mask, for the same
  reason: a joiner computes on the grid it has. A reader between refreshes holds
  the way it read (§4) rather than reading a field left over an older grid: what it
  holds is in its component, which a capture carries.
- Loot folds into navigation's private groups, kept for every cursor so the
  carrier agrees across instances (§7).
- A field over a moving map is read less often (§4), never settled less: a settle
  stopping at the farthest reader out of sight would skip a tenth of a raider's
  field at 120×40 and under a hundredth at 400×120, and a third to a half of a
  drop's, which costs under a hundredth of a tick. Repairing a field where cells
  closed pays on an open map, not on a cave, where every bend moves each column.
- No per-tick field budget and no coarse field: td's worst ticks compute no field
  (§10), so deferring one, which changes what a reader steers by, would bound
  nothing that stalls, and a 2×2 field changes the answer for a cost the shared
  grid already took.
- Vanguard reads every fourth tick, not less often: past four, navigation's cost is
  the wall upkeep and sight, and a longer hold saves under 10 µs a tick.

## 13. Extension guidance and source map

- Use a shared field for many actors with the same footprint and targets; never
  one per entity. An actor whose goal is one participant's cursor reads that
  cursor's private group.
- A navigator declares its footprint and header offset in `NavigationComponent`,
  and navigation keeps the tier that steers it; a route graph request for a body
  other than the eye names its footprint.
- A new shared derivation with a throttle carries its phase (D-17) and must
  decide identically whether or not anything reads it.

| Concern | Primary source |
|---|---|
| Fields, grid, repair | `pkg/navigation/flowfield.go` |
| Cache and throttle | `pkg/navigation/cache.go` |
| Reach | `pkg/navigation/reach.go` |
| Steering reads | `pkg/navigation/steering.go` |
| Composite passability | `pkg/navigation/composite.go` |
| Route graphs and corridor fields | `pkg/navigation/routegraph.go` |
| ECS adapter, wall diff, lock, carrier | `internal/system/navigation.go` |
| Movement target from navigation output | `internal/system/targeting.go` |
| Resources | `internal/engine/resource.go` |
| Loot's read | `internal/system/loot.go` |
| Tunables | `internal/parameter/navigation.go` |
