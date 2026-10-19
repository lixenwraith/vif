# Navigation fields and route graphs

Working analysis of whether the flow fields and the route graphs should fold into
one structure, and where navigation's time actually goes. §1 is the verdict, §2
what the code did before this work against the questions that prompted it, §3 the
measurements, §4 the program with what has landed, §5 the snake lock, §6 what
custom maps will meet, §7 the decisions. Items 1 to 5 of §4 have landed; 6 and
7 wait for a need.

## 1. Decision proposed

Do not merge. A group field and a route graph answer different questions — "from
every cell, the way to the nearest goal" against "from one source, several
distinct corridors to one goal, kept apart so the bandit can score them" — and the
point and composite fields of one group are searches over different graphs whose
distances and directions differ almost everywhere in a maze (§3.1). Folding the
tiers into one pass saves nothing: a Dijkstra costs what it settles, and a tier
seeded from a larger footprint's distances still settles every cell of its own.

What the measurements do show is that the fields eyes and route graphs use are a
small part of the cost. Two readers pay for almost all field computes: the snake
lock, which keeps one composite field per cursor on the map, and loot, which keeps
one point field per owner outside navigation. Three changes take most of it — one
search per snake head instead of one field per cursor, one filled grid and
direction-mask table per passability shared by every field on it, and a wall
change that dirties fields instead of invalidating all of them at once — and a
fourth folds loot's private fields into navigation's so the game has one
mechanism for "a field toward one cursor". A footprint tier registry derived from
the live navigators is the forward-looking part, and it builds on the lazy reads
navigation already has.

## 2. What runs today, against the questions asked

Three structures, one passability grid beyond the walls:

| Structure | Graph it searches | Goal | Readers |
|---|---|---|---|
| Point field, one per group | the walls (`Position.WallTest`) | every target of the group, nearest wins | drains (group 0; Player-domain, so one instance's drains) |
| Composite field, one per group | 5×3 `CompositePassability` (eye footprint, centred header) | same | eye, quasar, snake head, and the 4×2 swarm, which rides the 5×3 grid as a safe superset |
| Route graph, one per gateway | the same 5×3 passability | the group's first anchor, one source | eyes spawned by that gateway while the graph is fresh and the eye inside its corridor |

Loot keeps a fourth, outside navigation: one point field per owner cursor in
`LootSystem.ownerRoutes`, eager, compared against the wall grid every tick.

Against the premises this exercise started from:

- *The flow field always points to the cursor and is local.* Group 0's two
  fields point at every live cursor, up to the eight-target cap, and are read by
  swarms, quasars and an unlocked snake head as well as by drains. Script groups
  1–15 point at tower anchors and are read by every eye that has no fresh route,
  which during a storm is every eye. Their point fields have no reader today — no
  1×1 navigator carries a `TargetComponent` — so they cost a reachability test.
- *Route graph group 0 is the local cursor; other groups are slots or targets.*
  Group 0 is the whole roster, nearest wins. A route graph belongs to a gateway,
  not a group, runs to the first anchor of the gateway's group, and today only
  towers get one: both snake gateways set `use_route_graph = false`. The
  per-slot groups (`MaxTargetGroups` + slot) exist only while a snake head lives
  and serve only its lock.
- *Flow field for 1×1, route graph for 5×3.* The split is point field against
  composite field; the route graph is a third structure on the composite grid,
  not the composite counterpart of the point field.
- *Narrow routes are the divergence.* The divergence is everywhere: on the td
  maze 78% of composite-valid cells get a different direction from the composite
  field than from the point field at the same cell (25% on the tower map). A
  5-wide corridor leaves one composite-valid column, and the point search prefers
  a diagonal toward a wall at cost 22 over a straight step and a turn at 30. The
  two are different graphs with different distances, not one graph with gaps.
- *Storm shortcuts.* Confirmed: a storm circle despawns 1×1 wall cells on contact
  (52 ticks with a wall change in a two-minute td storm), each a gap drains and
  loot can use and composites cannot.
- *Tiered computation, larger footprint first.* A field seeded from a larger
  tier's distances pops those seeds like any other cell and then settles every
  remaining cell, so it costs the same as a plain field. It would also make the
  smaller entity's distances an upper bound — the larger entity's detour wherever
  the smaller one has a shortcut. The nested property itself holds (a centred 5×3
  fits only where a centred 4×2 and a 1×1 fit) and is what lets one wall-grid
  diff drive every tier's passability (§4, item 3).

Two duplications are real, neither the one proposed: loot's per-owner field
beside navigation's per-cursor private groups, and navigation's own passability
update, where a wall event's region recompute is followed in the same tick by
`noteWalls` recomputing the whole grid because the wall slice moved.

## 3. Measurements

### 3.1 Isolated, on the scenario maze shape

Generated with `pkg/maze` at cell 5×3, braiding 0.5, rooms as the scenarios lay
them out; native, one core.

| | tower 239×64 | td 500×250 |
|---|---:|---:|
| 5×3 passability, full | 0.43 ms | 2.5 ms |
| point field, 8 targets | 0.52 ms | 3.9 ms |
| composite field, 8 targets | 0.31 ms | 1.5 ms |
| route graph, corner to centre | 3.0 ms (5 routes) | 7.8 ms (2 routes) |
| storage per route field, share of cells used | 138 kB, 3.3% | 1.1 MB, 1.6% |

### 3.2 A td run, headless

Eight cursors walking a cell a tick, the monitor region stopped and towers
healed so the run does not restart, 100–320 eyes on route graphs, two to seven
snake heads. Eight local cursors overstate loot (an instance holds only its own
cursor's drops, D-2); the snake share is what every instance pays with that many
players.

| Phase | ms/tick | worst tick | field computes | wall-change ticks |
|---|---:|---:|---:|---:|
| play, 600 ticks | 8.5 | 47 ms | 1,440 (2.4/tick, 8 at most) | 0 |
| storm, 2,400 ticks | 11.4 | 132 ms | 6,848 (2.9/tick, 32 at most) | 52 |

Where the tick went, by cumulative share:

| | play | storm |
|---|---:|---:|
| loot owner point fields (`refreshOwnerRoutes`) | 63% | 51% |
| snake lock reading one composite field per cursor (`lockSnakeTarget`) | 26% | 23% |
| wall-slice comparisons (`slices.Equal`, loot's every tick, navigation's on a generation move) | 10% | 6% |
| route graph rebuilds | <1% | 2% |
| line-of-sight checks, 150–300 composites | 2% | 3% |

Inside a field compute: the direction mask (`open`) 33%, re-reading passability
into the bordered grid (`fill`) 11–13%, the index translation (`pad`) 10%.

The storm's worst tick is every live field invalidated by one despawned wall and
recomputed in the same tick: 32 fields, the 8 private, 6 eye groups and group 0
twice. With 16 players that is 16 private fields alone, at 1.5 ms each on td.

### 3.3 The same run after items 1 and 2

Same seed, same walk, and the same eye and snake counts at every checkpoint, so
the lock decided as before.

| Phase | ms/tick | worst tick | field computes |
|---|---:|---:|---:|
| play, 600 ticks | 4.0 (was 8.5) | 19 ms (was 47) | 480 (2 at most; was 1,440) |
| storm, 2,400 ticks | 6.8 (was 11.4) | 69 ms (was 132) | 2,568 (16 at most; was 6,848) |

The snake lock now scales with heads rather than players: 11–15% of the tick with
three to seven of them. Loot's per-owner fields, which eight local cursors
overstate, are most of what remains, and the worst tick is still the wall change
invalidating every live field at once (item 3).

### 3.4 The same run after item 3

| Phase | ms/tick | worst tick | field computes |
|---|---:|---:|---:|
| play, 600 ticks | 4.4 | 23 ms | 480 (2 at most) |
| storm, 2,400 ticks | 6.8 | 47 ms (was 69) | 1,920 (2 at most; was 16) |

Fifty-four wall-change ticks moved the composite passability on a handful of
header cells in all, and no wall change computed more than two fields in a tick;
the 47 ms tick is no longer a field recompute. In the storm tick: loot's owner
fields 47% (eight local cursors), the snake reaches 15%, composite line of sight
8%, route graph rebuilds with their per-route grid fills 3%.

### 3.5 The same run after item 4

| Phase | ms/tick | worst tick |
|---|---:|---:|
| play, 600 ticks | 4.2 | 21 ms |
| storm, 2,400 ticks | 6.6 | 37 ms (was 47) |

The owner fields cost what they did, now materialized through the pursuit
resource for the drops out of sight of their owner: 46% of the storm tick with
eight local cursors, which is one local cursor's share eight times over. The wall
compares are gone from the profile.

### 3.6 The same run after item 5

| Phase | ms/tick | worst tick |
|---|---:|---:|
| play, 600 ticks | 4.4 | 28 ms |
| storm, 2,400 ticks | 6.4 | 30 ms (was 37) |

Route graph rebuilds fell from 3.6% of the storm tick to 1.4%, and the heap in
use after a collection at the storm's high water is 71 MiB. Over the five items
the storm's worst tick went from 132 ms to 30 and its average from 11.4 ms to
6.4, on the same seed and walk.

## 4. Options, in order of payoff

1. **Lock a snake by one search from its head** (landed). One composite-tier Dijkstra
   from the head cell, settling until every cursor's seed set is passed — a
   cursor's cost is its cell's distance when the footprint fits there, else the
   least distance plus gap over the first passable ring, exactly what the private
   field reports at the head today, by symmetry of the step costs. Fields then
   exist only for cursors a head is locked on, through the private groups that
   already exist, created on lock and retired when no head holds them. The lock
   evaluation takes a throttle of its own, held in the head's component so the
   capture carries it (D-17 for free, since the head is a Shared entity). This
   also measures every cursor at one instant instead of through fields of
   different staleness (§5). Cost goes from one field per player to one bounded
   search per head.
2. **Share the search scaffolding per passability** (landed). One `SearchGrid`
   per tier, filled once per grid generation with the direction mask precomputed
   per cell, and every field on that tier computed on it; the mask makes the
   index translation go as well. About half of a field compute whenever the grid
   has not moved, which is every recompute a moving target causes. The route
   solver keeps its own fill: a graph is rebuilt at most once in twenty ticks.
3. **One wall-change mechanism** (landed). The wall slice is diffed once per
   tick, cell by cell, in place of both the event-driven region recompute and
   `noteWalls`' whole-grid recompute: passability is recomputed around the
   changed cells only, each tier has its own generation, and the composite tier
   moves only where a footprint's fit changed, which a storm's single-cell holes
   rarely do. Cells that opened repair a computed field in place
   (`FlowField.Opened`), exactly as a recompute would, ties broken canonically so
   a repaired field and a recomputed one agree bit for bit; a cell closed still
   drops the field. Dirtying instead was rejected: a field left standing on an
   older grid has no reproducible input at an install, so a joiner would steer
   differently until the cooldown passed. A route graph is stamped with the
   composite generation and stays fresh across any change that fits no footprint
   differently; staleness by corridor mask was rejected for the same reason,
   since a joiner computes on the grid it has.
4. **Loot reads navigation's private groups** (landed). Navigation keeps a
   private group for every cursor on the map, not only the locked ones: the
   groups, and so the phases the carrier holds, are then the same on every
   instance, which a group whose existence followed this instance's drops could
   not be, since loot is player-domain. A field's arrays are allocated by its
   first compute, so the sixteen groups cost nothing until read. Navigation
   publishes each slot's point field through `engine.PursuitResource`; loot reads
   its owner's slot, materializing the field on the instance that holds the drops
   and nowhere else (D-17: when a field is computed is local). `LootSystem`'s
   own caches, wall copy and per-tick compare are gone, and navigation's own
   compare is gated by `Position.WallGeneration`, so a tick without a wall change
   compares nothing.
5. **Corridor-sparse route fields** (landed). A route's field is its corridor's
   cells alone (`CorridorField`): distance and step per cell, searched on the
   graph's one grid with everything outside the corridor a wall, a diagonal's
   corners included, and stored sorted by cell for a binary-search read, which
   serves `GetDirection`, `GetDistance`, the neighbour escape and the adaptation
   recovery ring unchanged. A td route is about 36 kB in place of 1.1 MB, the
   per-route grid fill is gone, and a td graph computes in 3.8 ms where it took
   7.8 (a tower graph 1.5 ms for 3.0).
6. **A footprint tier registry.** Navigation derives the set of footprints from
   the live `NavigationComponent`s and keeps one passability grid and one field
   slot per group per tier in use, computed on first read like today's two. A
   species footprint table — width, height and header offsets in one place —
   replaces the parameters spread over `internal/parameter` and the `width/2`
   half-boxes the area line of sight assumes, which are a cell off for the 4×2
   swarm. The swarm then steers by its own exact tier; a 10×5 or a 3×5 species
   from a scenario costs one row.
7. **A per-tick field budget**, after 1–3: at most K computes a tick in canonical
   group order, the rest deferred. The decision is deterministic and the counters
   are already captured, so the phase stays shared. Only worth adding if a tier
   registry multiplies fields again.
8. **The debug views** (`:flow`, `:graph`) are not a constraint; they are dropped
   or rewired as the representations change.

Not proposed: a 2×2 coarse field (the todo item) — the shared mask table and the
dirty policy recover more without changing the answer.

## 5. The snake lock as it stands

`lockSnakeTarget` runs every tick per head, reads the head's distance in every
cursor's private composite field (the least over the eight neighbours plus a
diagonal when the head straddles a blocked cell), keeps a reachable lock unless
another cursor is over a third nearer, and takes the lowest slot on a tie. It is
deterministic, Shared (the lock lives in the head's `NavigationComponent`), and
`TestSnakePursuitKeepsItsRouteUntilAReachableTargetIsMuchCloser` pins the
hysteresis, the departed-cursor case and the capture round trip.

Its weak points were cost and skew, not the rule, and item 1 took both: the
reach measures every cursor at one instant, where each private field was throttled
on its own cursor's drift and the comparison read one computed this tick beside
one computed for a cell up to a second old. The rule itself stays as it was, by
decision: a lock is sticky, and sight is deliberately not a factor. A cursor jumps
at no cost, so a head that switched to whichever cursor it could see would be
switching all the time; a lock drops only when its cursor is unreachable or gone.

Two edges remain as they were. A head with no cursor reachable locks nothing,
`resolveBaseTarget` reports no target, and the head holds still until something
moves it — a knockback into a pocket the footprint cannot leave by one step
freezes it, where an eye in the same place homes straight at its target. And the
ring around a blocked cursor cell reaches 8 cells, the field's constant; a cursor
with no composite-valid cell within it is unreachable for every head.

## 6. What custom maps will meet

- A gateway with `use_route_graph = true` feeding a moving group (cursors) is a
  trap: the graph is stale as soon as the target moves and is rebuilt every
  twenty ticks at 8 ms on td. Fields are for moving goals, graphs for fixed ones;
  the loader should warn or steer such a gateway by field.
- A group with several anchors gets routes to its first anchor only
  (`resolveTargetPosition`), while the field picks the nearest; an eye off its
  corridor may change goal.
- Any footprint beyond 5×3 and 1×1 today rides the 5×3 grid or none; the tier
  registry is what makes a data-driven species' footprint cost a row.
- Target groups cap at 8 targets and 16 groups; the private per-player groups
  are the only per-player structure and option 1 keeps them to the locked few.
- Fields scale with the map: a 1,000×500 map is four td fields per compute, and
  every instance computes the same ones; options 2, 3 and 7 are what keep that
  within a browser frame.

## 7. Decisions

1. Taken: no merge; §4 is the program.
2. Taken: 1 and 2 first, then 3, then 4 and 5; 6 when a second footprint tier is
   wanted. Each is one branch, one PR, with the capture schema bumped where the
   carrier or a component changes.
3. Taken: the lock stays sticky and sight stays out of it (§5).
4. Taken: an opened cell repairs a field in place rather than dirtying it, and a
   graph's freshness is its tier's generation (§4 item 3).
5. Taken: loot folds into navigation's private groups, kept for every cursor so
   the carrier agrees across instances (§4 item 4).
