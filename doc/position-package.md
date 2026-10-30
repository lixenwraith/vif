# Position package

`pkg/position` is a map's spatial index and the geometry over it, free of the game
and of `engine`: a dense grid of cells holding what stands in each, the walls among
them kept as a bit a cell for each mask asked, the rock no entity holds, the log of
the cells whose wall answer changed, and the searches a placement, a line or a ray
makes over them. `engine.Position` binds it to a `World`: the rows (entity to
cell), the component mask, which shared occupant is a wall and what it stops, the
domain names, telemetry and typing's top glyph. This document holds
the move's decisions and its phases; the item in [todo](todo.md#move-position-into-a-package-of-its-own-walls-and-rock-one-blocking-model)
states what the end must keep.

## 1. The boundary

In `pkg/position`:

- `Grid[E]`: the cells, each two halves (`ClassShared` from the front, `ClassPrivate`
  from the back within `PrivateBudget`), `Scope`, the occupied list, `Clamp` and the
  map ceiling, stats, and the load and reorder a world copy and an install use.
- `Index[E]`: the grid, whose dimensions are the bounds every query answers
  within; the `Frame` it reads the cell aspect through; `wallOf`, the caller's word
  on which shared occupant is a wall and what it stops (a `Mask`, 0 asking for any
  wall); a plane of bits for each mask a query asked, kept with every wall change,
  so a point is a bit and an area a few words a row; the wall log; `WallTest`,
  which copies a plane into the caller's grid; and the searches: free area by
  spiral, by pattern and around an exclusion, point and area line of sight, rays,
  batches, the closest occupant in a direction.
- `Rock`: the cluster no entity holds, a ceiling and a floor per column, shifted a
  column at a time, its columns ahead for a frame drawn between ticks; a wall query
  reads it before the cell's occupants.

In `engine`:

- `Position`: the rows and their sparse index (shared with `Store`), `SetPosition`
  and its kin over rows and grid, `Shift`, batches, the mask upkeep that tells the
  index which moves are a wall's, once the grid holds the move, `wallOf` over
  `Components.Wall`, the typing queries (`TopGlyphAt`, `IsTypeable`, `IsScenery`),
  telemetry, and the rock's look. `component.WallBlockMask` is the package's `Mask`.
- The names the game reads: `DomainScope`, `ScopeShared`, `ScopePlayer`, `ScopeBoth`,
  `Cell`, `SpatialGrid`, `RockField`, `RockColumn`, `Exclusion`, the pattern kinds,
  as aliases of the package's. A class is an entity's domain
  (`position.Class(e.Domain())`), which `entityClass` states once.
- The snapshot and copy paths, which read the rows and the cells.

## 2. Decisions

Each names the choice taken and what the other would have cost.

- Location, `pkg/position`: another program imports it, which `internal/` forbids,
  and `pkg/` never imports `internal/`, so the package names no entity, component,
  parameter or world type of the game. `internal/position` would have moved the
  code once and the types a second time.
- Entity, a type parameter `E comparable`, with the class passed by the caller
  (`Set(e, class, x, y)`, `Holds`, `Reorder`): the grid's slices are `[]core.Entity`
  with no conversion, and no call decodes an id, so no layout is assumed and no
  indirect call is paid on the hot path. A constraint with a method, or a class
  function, would cost a dictionary or indirect call per insert and lookup; an
  entity type of the package's, aliased by `core`, would put identity in a
  geometry package.
- Bounds, the grid's: `Config.MapWidth` and `MapHeight` stay Shared authority
  (D-14), and every path that writes them resizes the grid before a query runs (a
  level setup, the reset, a crop, a copy), so the index answers within its grid and
  reads no bounds live. Reading them through `Frame` on every probe cost the glyph
  renderer, which asks a wall per scenery glyph a frame, a quarter of its time; a
  writer that does not resize the grid is a bug, and `TestSetupLevelRecordsOnlyBoundsTheGridHolds`
  pins the pair.
- Walls, a plane of bits per mask asked: the rock answers first, then the bit. A
  plane is built from the cells on the first query of its mask and brought to a
  cell's shared occupants by `WallChangedAt`, which the binding calls once the grid
  holds a wall's move, so a storm's hundreds of walls cost a cell scan each; a
  resize, a copy loaded or masks rewritten wholesale mark every plane stale, and
  the next query rebuilds them from the occupied cells. The first phase scanned the
  cell's occupants on every probe through a predicate; the td profile put the area
  test, navigation's direct-path probe over a footprint, at a sixth of the tick,
  most of it cell reads of 256 bytes each. The walls stay entities, which species
  move and break; the planes are derived, never captured.
- The mask, the package's: `component.WallBlockMask` is `position.Mask`, since the
  package names no game type and the game's constants stay where they are.
- Rows, in `engine` for now: the sparse index is `Store`'s, and the row type is
  `component.PositionComponent`. Moving the rows means a point type of the package's
  aliased by `component`, and the generator walking the alias; the third axis
  enters with that type, so both wait for the same phase.
- Names: the package's classes are shared and private, the game's shared and player.
  `component.WallBlockMask` converts to `position.Mask` at the binding; an alias
  there is the smaller change when the call sites move.
- The surface: `World.Positions` keeps every method a caller had, so no call site
  moves in the first phase. Methods nothing called are deleted rather than moved:
  `ScanLine`, `ScanLineFirst`, `WallGeneration`.

## 3. Phases

1. The package and the binding, no behaviour change, measured: this document's
   boundary. Done.
2. The blocking model: the planes, the mask alias, td's tick with sixteen cursors
   and the benches measured before and after. Done.
2b. Navigation over the planes' words, if the profile asks: `CompositePassability`
   derived a row of words at a time and the lost-log re-read diffed by XOR instead
   of a cell at a time (`applyWalls`, `navigation.Diff`).
3. The call sites: readers take the package's names (`position.ScopeAll`,
   `position.Column`) and the binding narrows to what the rows need; `SetRock` loses
   its look, which the wall system publishes in `CaveResource` for the rock renderer.
4. Footprints and axes: the direct-path test from the body's header, the 1:1 cell
   for a graphical host (authored geometry stays at `RefAspect`, the package owns the
   conversion), the third axis in the row type, the sparse grid for a ceiling-sized
   empty map.

## 4. Measurements

Phase 1, main's and the branch's test binaries alternated five rounds on one
container, medians of ns/op. A reading there moves a fifth between rounds, so a
difference inside that band is noise; the first cut of the binding, which read
the bounds through an interface on every probe and asked an interface over the
wall entities, read +25% to +40% on `BenchmarkGlyphRender` in every round, and the
binding as merged reads inside the band.

```
                                   main   phase 1   change
CaveStep/200x60                   9984      9821    -1.6%
CaveStep/400x120                 21859     22124    +1.2%
CaveStep/500x254                 30416     27567    -9.4%
CaveStepNavigation/200x60        35845     35399    -1.2%
CaveStepNavigation/400x120       73981     75553    +2.1%
CaveStepNavigation/500x254       95456    105666   +10.7%
NavigationWallMoves/200x60        4077      3958    -2.9%
NavigationWallMoves/400x120       3932      3886    -1.2%
NavigationWallMoves/500x254       3986      4403   +10.5%
GlyphRender/120x40               33221     33285    +0.2%
GlyphRender/200x60               51926     53526    +3.1%
GlyphRender/400x120              96740    100285    +3.7%
RockRender/200x60/visual        155989    164271    +5.3%
RockRender/200x60/stone         277727    287299    +3.4%
RockRender/200x60/redraw       2150936   2197494    +2.2%
RockRender/400x120/visual       771866    819161    +6.1%
RockRender/400x120/stone       1075013   1156223    +7.6%
RockRender/400x120/redraw     10885271  10964935    +0.7%
RockRender/500x254/visual      2654428   2979538   +12.2%
RockRender/500x254/stone       4897718   4444327    -9.3%
RockRender/500x254/redraw     31366822  33556171    +7.0%
```

Phase 2, the same way, main at phase 1 against the planes. td's tick with sixteen
cursors (`TestTickProfileTD16`, 500×250, 5,000 ticks on one core), ms a tick,
five readings each:

```
main      0.40  0.40  0.40  0.38  0.37   median 0.40
phase 2   0.36  0.40  0.34  0.32  0.34   median 0.34
```

In main's profile the area query, navigation's direct-path probe over a
footprint, was 16% of the tick and the cell view under it 9%; in phase 2's the
area query is off the top of the profile and the index's share of the tick fell
from 0.56 s to 0.47 s of 1.8 s and 1.7 s. The benches, medians of ns/op:

```
                                   main   phase 2   change
CaveStep/200x60                   9625     10538    +9.5%
CaveStep/400x120                 22044     24673   +11.9%
CaveStep/500x254                 28149     27295    -3.0%
CaveStepNavigation/200x60        34076     31485    -7.6%
CaveStepNavigation/400x120       70025     67554    -3.5%
CaveStepNavigation/500x254       91356     92495    +1.2%
NavigationWallMoves/200x60        3991      3916    -1.9%
NavigationWallMoves/400x120       3744      4041    +7.9%
NavigationWallMoves/500x254       4039      4266    +5.6%
GlyphRender/120x40               31823     32451    +2.0%
GlyphRender/200x60               52047     53878    +3.5%
GlyphRender/400x120              94424     86311    -8.6%
RockRender/200x60/stone         274489    259675    -5.4%
RockRender/400x120/stone       1137142   1035326    -9.0%
RockRender/500x254/stone       4059707   4300285    +5.9%
```

`CaveStep` turns rock alone, no wall entity, and its readings overlap (9.3 to
10.5 µs on main against 9.9 to 11.2 on phase 2 at 200×60): the band. What the
planes do cost is a wall's move, which brings them to two cells, a cell scan
each, paid once where every probe of the cell paid before; nothing measured
moves walls in bulk but a cave with pattern walls riding its scroll, which a
vanguard profile should read when one lands.
