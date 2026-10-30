# Position package

`pkg/position` is a map's spatial index and the geometry over it, free of the game
and of `engine`: a dense grid of cells holding what stands in each, the blocks
among them kept as a bit a cell for each mask asked, the surfaces no entity holds,
the log of the cells whose block answer changed, and the searches a placement, a
line or a ray makes over them. `engine.Position` binds it to a `World`: the rows
(entity to cell), the component mask, which shared occupant is a block and what it
stops, the domain names, telemetry and typing's top glyph. The package's terms
and the game's are mapped in §1. This document holds
the move's decisions and its phases; the item in [todo](todo.md#move-position-into-a-package-of-its-own-walls-and-rock-one-blocking-model)
states what the end must keep.

## 1. The boundary

In `pkg/position`:

- `Grid[E]`: the cells, each two halves (`ClassShared` from the front, `ClassPrivate`
  from the back within `PrivateBudget`), `Scope`, the occupied list, `Clamp` and the
  map ceiling, stats, and the load and reorder a world copy and an install use.
- `Index[E]`: the grid, whose dimensions are the bounds every query answers
  within; the `Frame` it reads the cell aspect through; `blockOf`, the caller's
  word on which shared occupant is a block and what it stops (a `Mask`, 0 asking
  for any block); a plane of bits for each mask a query asked, kept with every
  block change, so a point is a bit and an area a few words a row; the surfaces;
  the block log; `BlockBits`, the plane's words with the surfaces' solid cells for
  a reader deriving whole; and the searches: free area by spiral, by pattern and
  around an exclusion, point and area line of sight, rays, batches, the closest
  occupant in a direction.
- `Bits`: the grid of bits the planes and navigation share, each row starting on
  a word: a point a bit, an area a few words a row, a row shifted a column in a
  word, two grids diffed by XOR.
- `Surface`: a boundary no entity holds, a height for each column with the cells
  on one `Side` of it solid (`Above`, a ceiling; `Below`, a floor), blocking its
  mask as a block does; laid from its writer's record, shifted a column at a time
  with the heights ahead of it for a frame drawn between shifts, and answered
  before the cell's occupants by its heights alone, no bits to keep.

The terms, the package's and the game's:

- A *block* is a cell an occupant blocks for a mask: in the game a *wall*, the
  entity with a `WallComponent`, whose `BlockMask` is the mask. `Blocks`,
  `BlocksArea`, `BlockBits` answer; `BlockChangedAt`, `BlocksChanged`,
  `BlockChanges` keep the planes and the log. The binding keeps the game's
  names (`HasBlockingWallAt`, `WallChanged`, `WallsChanged`, `WallChanges`,
  `WallBits`, the *wall log*).
- A *surface* is a height a column with a solid side: in the game the cave's
  *rock*, two surfaces, the ceiling above and the floor below, which
  `engine.RockField` keeps with one mask, one origin and the `RockColumn` pairs the
  renderer and the wall system read. `SetRock`, `ShiftRock`, `SetRockAhead` and
  `HasBlockingWallDrawnAt` are the binding's names for `AddSurface`/`LaySurface`,
  `ShiftSurface`, `SetSurfaceAhead` and `BlocksCarried`.
- A *shape*, a line or polygon rasterised into blocks or a volume into solid
  cells, is the third kind a game may want; none is implemented, and it would
  enter as a surface does, laid from its writer's record.

In `engine`:

- `Position`: the rows and their sparse index (shared with `Store`), `SetPosition`
  and its kin over rows and grid, `Shift`, batches, the mask upkeep that tells the
  index which moves are a wall's, once the grid holds the move, `wallOf` over
  `Components.Wall` as the index's `blockOf`, the typing queries (`TopGlyphAt`,
  `IsTypeable`, `IsScenery`), telemetry, and the rock: `RockField`, the two
  surfaces with their columns and look. `component.WallBlockMask` is the package's
  `Mask`.
- The names the game reads: `DomainScope`, `ScopeShared`, `ScopePlayer`, `ScopeBoth`,
  `Cell`, `SpatialGrid`, `Exclusion`, the pattern kinds, as aliases of the
  package's; `RockField` and `RockColumn` are the engine's own. A class is an
  entity's domain (`position.Class(e.Domain())`), which `EntityClass` states once.
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
- Blocks, a plane of bits per mask asked: the surfaces answer first, then the
  bit. A plane is built from the cells on the first query of its mask and brought
  to a cell's shared occupants by `BlockChangedAt`, which the binding calls once
  the grid holds a wall's move, so a storm's hundreds of walls cost a cell scan
  each; a resize, a copy loaded or masks rewritten wholesale mark every plane
  stale, and the next query rebuilds them from the occupied cells. The first phase
  scanned the cell's occupants on every probe through a predicate; the td profile
  put the area test, navigation's direct-path probe over a footprint, at a sixth
  of the tick, most of it cell reads of 256 bytes each. The walls stay entities,
  which species move and break; the planes are derived, never captured.
- Surfaces, heights with a side rather than bits: a probe compares a row to a
  height, as cheap as a bit, a shift moves a slice of heights and logs the rows
  crossed, and only a reader deriving whole (`BlockBits`) rasterises them. The
  rock kept bits for one phase, shifted a column at a time with it; two surfaces
  would have doubled that shift for nothing a probe needs. The game's rock is two
  surfaces that move together; a floor alone or a ceiling alone is one.
- The mask, the package's: `component.WallBlockMask` is `position.Mask`, since the
  package names no game type and the game's constants stay where they are.
- Navigation reads words: a composite passability is a bit a header, each row the
  OR of the walls' words under the footprint's rows shifted to the header's column
  (`CompositePassability.Compute`), and a change derives again the header rows
  and words the changed cells' footprints reach, reporting the headers whose bit
  turned (`Moved`); the lost log's re-read is the words diffed by XOR
  (`position.Diff`). Counting the walls under each footprint, which a changed
  cell moved in place, went with it: a count is a second record of the walls to
  keep in step. Two shapes were tried on the way: re-testing each header over a
  changed cell read a cave step, which turns hundreds of cells, at twice main's,
  and deriving whole rows with a scan of every row read one wall's move a fifth
  slower on the largest map; the rows a change reaches, in the words it reaches,
  read both at main or under.
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
2b. Navigation over the planes' words: `CompositePassability` derived a row of
   words at a time, the lost log's re-read diffed by XOR, the search grids filled
   from bits. Done. The route solver and the flow fields still ask a cell at a time
   through `WallChecker`.
3. The call sites and the point: readers take the package's names
   (`position.ScopeAll`, `position.Column`), the binding narrows to what the rows
   need, `SetRock` loses its look, which the wall system publishes in `CaveResource`
   for the rock renderer, and the row type becomes the package's `Pos`, with the
   third axis (§4), `component.PositionComponent` its alias.
4. Footprints and the volume: the direct-path test from the body's header, the 1:1
   cell for a graphical host (authored geometry stays at `RefAspect`, the package
   owns the conversion), a depth to the grid, chunked cells for a volume and the
   ceiling-sized empty map, a 3D line of sight.
5. Batch skips: a presence grid per class, kept as cells fill and empty, so an
   area scan of occupants passes empty cells a word at a time.

## 4. The third axis

The axis is an actual 3D space: a position with a Z, a body of depth as well as
width and height, a line of sight through a volume. The same index serves a 2D
map as a volume one cell deep, at no cost to it. What enters, and where:

- The point: `position.Pos{X, Y, Z int}`, `component.PositionComponent` its
  alias, so a 2D caller writes `Pos{X: x, Y: y}` and reads Z as zero. A capture
  gains a column of zeros, which deflates to nothing, and the digest hashes three
  coordinates: a schema bump, taken with phase 3.
- The grid: a `Depth`, 1 on a 2D map, cells indexed `(z*Height+y)*Width+x`, the
  planes and the rock a `Bits` per layer, the arithmetic in one place. A dense
  grid cannot hold a volume at 256 bytes a cell: a volume allocates cells by chunk
  on demand, which also serves the ceiling-sized empty 2D map
  ([Shrink the spatial grid](todo.md#shrink-the-spatial-grid-on-ceiling-sized-maps));
  the dense grid stays the path while every map is one layer.
- The queries: a point and an area gain a depth (`BlocksBox`), a line of sight
  walks a 3D line (`vmath` gains the stepper), a body of depth tests a box a step.
  The placement searches, spiral, pattern and exclusion, stay a layer's, as
  placing on a floor, and take a 3D form when a game asks for one.
- Navigation: a footprint gains a depth and a passability a layer; a flow field
  through a volume is a later ask.

To settle with the game that first needs it: whether a 2D-only reader of a
Z-aware entity reads its layer or ignores Z (a one-layer map has no difference),
and whether overlapping spaces that share cells (a room in three timelines) are a
Z range of one index or an index per space.

## 5. Measurements

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

Phase 2b, navigation over the words, main at phase 2 against the branch. td's tick
with sixteen cursors, ms a tick, five readings each:

```
main      0.35  0.30  0.33  0.31  0.35   median 0.33
phase 2b  0.31  0.32  0.29  0.34  0.40   median 0.32
```

A whole derivation of a 5×3 footprint's passability and its search grid on a
500×250 map three tenths walls (`CompositePassability.Compute` and the grid's
fill, three readings each, ms): main 2.35, 2.75, 2.43; phase 2b 0.67, 0.63, 0.71.
The benches, medians of ns/op:

```
                                   main  phase 2b   change
CaveStep/200x60                   9783     10280    +5.1%
CaveStep/400x120                 22420     23799    +6.2%
CaveStep/500x254                 30098     28526    -5.2%
CaveStepNavigation/200x60        31521     29644    -6.0%
CaveStepNavigation/400x120       64195     57859    -9.9%
CaveStepNavigation/500x254       79784     75900    -4.9%
CaveNavigation/200x60/near       75660     75286    -0.5%
CaveNavigation/200x60/far       134611    124159    -7.8%
CaveNavigation/400x120/near     106149     96458    -9.1%
CaveNavigation/400x120/far      151718    148477    -2.1%
CaveNavigation/500x254/near     126652    121319    -4.2%
CaveNavigation/500x254/far      195127    188969    -3.2%
NavigationWallMoves/200x60        3923      3983    +1.5%
NavigationWallMoves/400x120       3951      3903    -1.2%
NavigationWallMoves/500x254       4276      4222    -1.3%
GlyphRender/200x60               53428     55661    +4.2%
GlyphRender/400x120              86806     81071    -6.6%
```
