# Position package

`pkg/position` is a map's spatial index and the geometry over it, free of the game
and of `engine`: a dense grid of cells holding what stands in each, blockers
answering what each cell stops, the log of the cells whose answer changed, and the
searches a placement, a line or a ray makes over them. `engine.Position` binds it
to a `World`: the rows (entity to cell), the component mask, the wall entities as a
blocker, the domain names, telemetry and typing's top glyph. This document holds
the move's decisions and its phases; the item in [todo](todo.md#move-position-into-a-package-of-its-own-walls-and-rock-one-blocking-model)
states what the end must keep.

## 1. The boundary

In `pkg/position`:

- `Grid[E]`: the cells, each two halves (`ClassShared` from the front, `ClassPrivate`
  from the back within `PrivateBudget`), `Scope`, the occupied list, `Clamp` and the
  map ceiling, stats, and the load and reorder a world copy and an install use.
- `Index[E]`: the grid with its `Frame` (the bounds a query answers within and the
  cell aspect, read live), its blockers, the wall epoch and log, the per-mask
  `WallTest` grid, and the searches: free area by spiral, by pattern and around an
  exclusion, point and area line of sight, rays, batches, the closest occupant in a
  direction.
- `Blocker`: one source of blocked cells (point, area, and the cells it marks for a
  wall test), for a `Mask` (0 is any). `Rock` is the package's own: a ceiling and a
  floor per column, shifted a column at a time, its columns ahead for a frame drawn
  between ticks.

In `engine`:

- `Position`: the rows and their sparse index (shared with `Store`), `SetPosition`
  and its kin over rows and grid, `Shift`, batches, the mask upkeep that tells the
  index which moves are a wall's, `wallEntities` (the blocker over `Components.Wall`),
  the typing queries (`TopGlyphAt`, `IsTypeable`, `IsScenery`), telemetry, and the
  rock's look.
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
- Bounds, read live through `Frame`: `Config.MapWidth` and `MapHeight` are Shared
  authority (D-14), written on seven paths of which only some resize the grid, and a
  copy restores the config whole. An index owning its bounds would have to be told
  on each, and the grid is what it is told. The grid can be smaller than the map
  for a tick; a wall test covers the smaller of the two, as before.
- Blockers, an interface: the rock is tested first, the engine's wall entities
  after, so a probe answers as it did. The next phase replaces the entity blocker's
  cell scan with per-mask row bitsets kept from the log, so a point, an area or a
  line is answered in words with no component lookup; whether a maze's walls stay
  entities a cluster indexes or become one is open.
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

Phase 1, the same binaries alternated five times each on one container, medians:

```
                                  main        phase 1
BenchmarkCaveStep                 (pending)   (pending)
BenchmarkCaveStepNavigation       (pending)   (pending)
BenchmarkNavigationWallMoves      (pending)   (pending)
BenchmarkGlyphRender              (pending)   (pending)
```
