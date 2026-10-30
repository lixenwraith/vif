# Position package

`pkg/position` is a map's spatial index and the geometry over it, free of the game
and of `engine`: a dense grid of cells holding what stands in each, the walls among
them and the rock no entity holds, the log of the cells whose wall answer changed,
and the searches a placement, a line or a ray makes over them. `engine.Position`
binds it to a `World`: the rows (entity to cell), the component mask, which shared
occupant is a wall, the domain names, telemetry and typing's top glyph. This document holds
the move's decisions and its phases; the item in [todo](todo.md#move-position-into-a-package-of-its-own-walls-and-rock-one-blocking-model)
states what the end must keep.

## 1. The boundary

In `pkg/position`:

- `Grid[E]`: the cells, each two halves (`ClassShared` from the front, `ClassPrivate`
  from the back within `PrivateBudget`), `Scope`, the occupied list, `Clamp` and the
  map ceiling, stats, and the load and reorder a world copy and an install use.
- `Index[E]`: the grid, whose dimensions are the bounds every query answers
  within; the `Frame` it reads the cell aspect through; `isWall`, the caller's word
  on which shared occupant blocks a `Mask` (0 is any); the wall epoch and log; the
  per-mask `WallTest` grid; and the searches: free area by spiral, by pattern and
  around an exclusion, point and area line of sight, rays, batches, the closest
  occupant in a direction.
- `Rock`: the cluster no entity holds, a ceiling and a floor per column, shifted a
  column at a time, its columns ahead for a frame drawn between ticks; a wall query
  reads it before the cell's occupants.

In `engine`:

- `Position`: the rows and their sparse index (shared with `Store`), `SetPosition`
  and its kin over rows and grid, `Shift`, batches, the mask upkeep that tells the
  index which moves are a wall's, `isWall` over `Components.Wall`, the typing
  queries (`TopGlyphAt`, `IsTypeable`, `IsScenery`), telemetry, and the rock's look.
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
- Walls, a predicate: the rock answers first, then each shared occupant of the
  cell through the caller's `isWall`, so an open cell costs no indirect call. An
  interface over the wall entities, asked on every probe, cost the same renderer a
  tenth. The next phase keeps per-mask row bitsets from the log, so a point, an
  area or a line is answered in words with no component lookup, and brings the
  cluster surface a second cluster needs; whether a maze's walls stay entities a
  cluster indexes or become one is open.
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
2. The blocking model: bitset or run clusters in the package, updated from the log;
   the entity blocker reduced to keeping them; td's storm ticks measured before
   and after (the wall system's event and navigation, netcode design §3.7's shape).
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
