# Position

`pkg/position` is a map's spatial index and the geometry over it, free of the game
and of `engine`: a dense grid of cells, a map or a volume of layers, holding what
stands in each; the blocks among them kept as a bit a cell for each mask asked;
the surfaces no entity holds; the log of the cells whose block answer changed;
and the searches a placement, a line or a ray makes over them. `engine.Position`
binds it to a `World`: the rows (entity to cell), the component mask, which shared
occupant is a block and its kinds, telemetry and typing's top glyph, with the
index embedded, so the game asks it by the package's names. This document holds
the boundary, the terms, the decisions and their costs; what is left to do is in
[todo](todo.md#position-blocking-and-navigation).

## 1. The boundary

In `pkg/position`:

- `Grid[E]`: the cells, a volume of `Depth` layers, one on a flat map, each two
  halves (`ClassShared` from the front, `ClassPrivate` from the back within
  `PrivateBudget`), indexed `(z*Height+y)*Width+x`; `Scope`; the occupied list,
  each cell knowing its slot so one that empties is swapped out in constant time;
  the presence bits of each class (`Present`), the layers' rows in turn, by which
  a probe passes an empty cell on a word that stays in cache and a scan passes
  empty cells a word at a time; `Clamp` and `ClampVolume` under the one cell
  ceiling; stats; and the load and reorder a world copy and an install use.
- `Index[E]`: the grid, whose dimensions are the bounds every query answers
  within; the `Frame` it reads the cell aspect through; `blockOf`, the caller's
  word on which shared occupant is a block and its kinds (a `Mask`, 0 asking for
  any block); a plane of bits for each mask a query asked, kept with every block
  change, so a point is a bit and an area a few words a row; the surfaces; the
  block log; `BlockBits`, the plane's words with the surfaces' solid cells for a
  reader deriving whole; and the searches: free area by spiral, by pattern and
  around an exclusion, point and area line of sight, rays, batches, the closest
  occupant in a direction. A query taking a column and a row answers for the
  first layer, as a flat game's do; one taking a `Point3` (`BlocksAt`, `BlocksBox`,
  `IsBlockedAt`, `EntitiesIn`, `HasAnyEntityIn`, `HasLineOfSightBetween`,
  `HasBoxLineOfSight`) for any, a line through the volume walking `vmath.Line3`;
  the mutators take a `Point3`, a flat caller placing on the first layer.
- `Bits`: the grid of bits the planes and navigation share, each row starting on
  a word: a point a bit, an area a few words a row, a row shifted a column in a
  word, two grids diffed by XOR.
- `Surface`: a boundary no entity holds on the first layer, a height for each
  column with the cells on one `Side` of it solid (`Above`, a ceiling; `Below`, a
  floor), blocking its mask as a block does; laid from its writer's record,
  shifted a column at a time with the heights ahead of it for a frame drawn
  between shifts. The surfaces of one mask are merged for the probes into a
  ceiling and a floor per column and the bits those make, laid again on the first
  probe after a lay, and moved a column once every surface of the mask has
  shifted, so a cell is one bit whatever their number, answered before the cell's
  occupants.

The terms, the package's and the game's:

- A *block* is a cell an occupant blocks for a mask, the kinds `blockOf` names:
  in the game a *wall*, the entity with a `WallComponent`, its kinds the movers
  its `BlockMask` stops and the box style it draws in. `Blocks`, `BlocksArea`,
  `BlockBits` answer; `BlockChangedAt`, `BlocksChanged`, `BlockChanges` keep the
  planes and the log, and the game asks by those names through `World.Positions`.
  The binding keeps `WallChanged`, which finds a wall's cell from its row, and
  `wallOf`, which names a wall and its kinds; the *wall log* is the game's name
  for the block log.
- A *surface* is a height a column with a solid side: in the game the cave's
  *rock*, two surfaces, the ceiling above and the floor below, which
  `engine.RockField` keeps with one mask, one origin and the `RockColumn` pairs the
  renderer and the wall system read. `SetRock`, `ShiftRock` and `SetRockAhead`
  lay, shift and extend both through `AddSurface`/`LaySurface`, `ShiftSurface` and
  `SetSurfaceAhead`; a frame drawn between shifts asks `BlocksCarried`. The rock's
  look is the wall system's, laid in `CaveResource` for the rock renderer.
- A *shape*, a line or polygon rasterised into blocks or a volume into solid
  cells, is the third kind a game may want; none is implemented, and it would
  enter as a surface does, laid from its writer's record.

In `engine`:

- `Position`: embeds the index, so its queries are `World.Positions`'s own, and
  keeps what the rows, the components or the rock need: the rows and their sparse
  index (shared with `Store`), `SetPosition` and its kin over rows and grid,
  `Shift`, batches, `Resize` and `Clear`, which place the rows again or drop them,
  the mask upkeep that tells the index which moves are a wall's, once the grid
  holds the move, `wallOf` over `Components.Wall` as the index's `blockOf`, the
  typing queries (`TopGlyphAt`, `IsTypeable`, `IsScenery`), telemetry, and the
  rock: `RockField`, the two surfaces with their columns. `component.WallBlockMask`
  is the package's `Mask`.
- The names the game reads are the package's: `position.Scope` with `ScopeShared`,
  `ScopePrivate` and `ScopeAll`, `position.Pattern` and its kinds,
  `position.Exclusion`, `position.Rect` (the ping's bounds embed one),
  `position.CellOrder` in the world order; `RockField`, `RockColumn` and `RockLook`
  are the engine's own. A class is an entity's domain
  (`position.Class(e.Domain())`), which `EntityClass` states once.
- The snapshot and copy paths, which read the rows and the cells.

## 2. Decisions

Each names the choice taken and what the other would have cost.

- Location, `pkg/position`: another program imports it, which `internal/` forbids,
  and `pkg/` never imports `internal/`, so the package names no entity, component,
  parameter or world type of the game. `internal/position` would have moved the
  code once and the types a second time.
- Entity, a type parameter `E comparable`, with the class passed by the caller
  (`Set(e, class, p)`, `Holds`, `Reorder`): the grid's slices are `[]core.Entity`
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
  stale, and the next query rebuilds them from the occupied cells. Scanning the
  cell's occupants on every probe through a predicate put the area test,
  navigation's direct-path probe over a footprint, at a sixth of td's tick, most
  of it cell reads of 256 bytes each. The walls stay entities, which species move
  and break; the planes are derived, never captured.
- Kinds, the mask's bits: a block's kinds are what its `blockOf` names, in the
  game the movers a wall stops and the box style it draws in (`component.BoxKind`,
  two bits no `BlockMask` carries), so the box characters' neighbour test reads a
  bit of the style's plane where it copied a cell and looked every occupant's
  wall up. `WallBlockAll` is the four mover kinds, never every bit, so a query
  with it matches no styled wall that stops nothing. A second word of kinds
  beside the mask would have doubled the planes' key for two bits.
- Surfaces, heights with a side, merged per mask for the probes: a shift moves a
  slice of heights and logs the rows crossed, and the mask's merged bits move a
  column once every surface of the mask has shifted, the last column laid from
  their last heights; a lay, an add or a removal lays the merged heights and bits
  again on the first probe after it. A probe is one bit a mask, as the rock's was
  when the package held the rock itself. Three shapes read worse: each surface
  tested on its own, by its heights, read the glyph renderer a tenth slower at
  400×120; a merge of the heights on every shift, allocating, read the cave step
  at twice main's; the merged heights compared on each probe, without bits, still
  read the glyph renderer a tenth slower. Typing's reads of a glyph's own cell
  (`TopGlyphIs`) take the cell without the presence test, since the cell holds
  the glyph. The game's rock is two surfaces that move together; a floor alone or
  a ceiling alone is one. The surfaces lie on the first layer; a volume's is a
  heightfield, not laid.
- The mask, the package's: `component.WallBlockMask` is `position.Mask` by alias,
  since the package names no game type and the game's constants stay where they
  are, passed to the package's methods unchanged.
- Presence bits, a `Bits` a class kept by the grid as cells fill and empty: an
  occupant probe (`EntitiesAt`, `HasAnyEntityAt`) tests the bit before it reads
  the cell, so the empty cells a sweep, a particle or a shot asks about cost a
  word in cache rather than 256 bytes out of it, and the directional scan the
  motions make passes an empty row or column on one word. An occupied cell pays
  one more load, inside the band.
- Navigation reads words: a composite passability is a bit a header, each row the
  OR of the walls' words under the footprint's rows shifted to the header's column
  (`CompositePassability.Compute`), and a change derives again the header rows
  and words the changed cells' footprints reach, reporting the headers whose bit
  turned (`Moved`); the lost log's re-read is the words diffed by XOR
  (`position.Diff`); the route solver and the flow fields fill their search grids
  from the bits, and no reader asks a cell at a time. Counting the walls under
  each footprint, which a changed cell moved in place, went: a count is a second
  record of the walls to keep in step. Re-testing each header over a changed cell
  read a cave step, which turns hundreds of cells, at twice main's, and deriving
  whole rows with a scan of every row read one wall's move a fifth slower on the
  largest map; the rows a change reaches, in the words it reaches, read both at
  main or under.
- The direct path, the body flown from its header: `HasAreaLineOfSight` takes the
  body's anchor, the cell it moves by, and tests the body with its anchor on each
  cell of the line, so the cells it tests are the body's. A box centred on the
  line sat beside any body whose header is not its centre, and accepting it
  turned 90° accepted a body that cannot turn; the two erred both ways, rejecting
  clear bodies about as often as accepting blocked ones.
- Rows, in `engine`: the sparse index is `Store`'s, and the row type is
  `component.PositionComponent`, `vmath.Point` by alias, the flat cell of two
  ints; the binding places it on the first layer at the index boundary
  (`Point.Layer`), which inlines.
- Names: the package's classes are shared and private, the game's shared and
  player; the game reads the package's types by the package's names, never
  through an engine alias, since a method cannot be declared on an alias and a
  second name sends a reader to two docs.
- The surface, the index promoted through `World.Positions` rather than forwarded
  method by method, so a query the package gains needs no binding, and the
  binding keeps only what the rows, the components and the rock need. The grid's
  own writes (`Set`, `RemoveEntityAt`, `Load`, `Reorder`) come with the promotion
  and move no row; a view type hiding them would cost the forwarding the
  promotion saves, and the hazard is named in the type's comment and the agent
  notes. `Resize` and `Clear` are the binding's, shadowing the grid's. Methods
  nothing called were deleted rather than moved or promoted: `ScanLine`,
  `ScanLineFirst`, `WallGeneration`, the `Unsafe` twins, `IsPointValidForOrbit`
  and `IsPathBlocked`, negations of `IsBlocked` and `HasLineOfSight`.
- The cell aspect, read live through `Frame`: the searches round a radius on it
  (`SpiralSearch`, `MapSearchRadius`, `FindFreeFromPattern`), so a graphical host
  drawing square cells asks the same index; the one definition is
  `vmath.Aspect` ([Rendering §2.2](rendering.md#22-the-cell-aspect)).

## 3. The volume

The third axis is an actual 3D space: a position with a Z, a body of depth as
well as width and height, a line of sight through a volume. The same index
serves a flat map as a volume one cell deep, at no cost to it.

- The point, two integer types and no sentinel: `vmath.Point` is the flat cell,
  `{X, Y int}`, which the rows, the captures and the digest carry; `vmath.Point3`
  is a cell of the volume, `{X, Y, Z int}`, which the index's mutators and volume
  queries take, and `Point.Layer` places a flat cell on a layer. One point with a
  Z of zero on a flat map cost every row, log entry and capture column a third
  more for nothing, and read as a 3D type among 2D functions. `Vec3` stays the
  float vector of a body's physics. A generic index over the coordinate type is
  the todo's [item](todo.md#a-generic-index-over-the-coordinate-type) for when
  vmath and the physics go to three axes.
- The grid: `Depth` layers, one on a flat map, the cells indexed
  `(z*Height+y)*Width+x`, the planes and the presence bits each one `Bits` whose
  rows are the layers' in turn, so a cell's index names its row and a flat query
  costs what it did. The one ceiling bounds the volume's cells as the map's
  (`ClampVolume`), so a volume stays small while every cell is dense, 100×50×25
  at most.
- The queries: one taking a column and a row answers for the first layer, so no
  call site of a flat game moves; one taking a `Point` answers for any; a line of
  sight walks `vmath.Line3`, the axis of the longest span stepping every cell and
  in one plane the cells `Line` walks; a body of depth tests a box a step. The
  placement searches, spiral, pattern and exclusion, stay a layer's, as placing
  on a floor.
- A body with a height on a flat map, the storm: its circles keep a float `Pos3D`
  and `Vel3D` for their physics, and their Z, which picks the half drawn in
  front, never reaches their position. A body's continuous height belongs in its
  physics state and is projected to a layer only in a volume, where the point's
  Z is the layer the cell index reads: the storm is 3D physics drawn flattened,
  not a body a render in depth targets.
- Presentation: a flat presentation reads an entity at its layer, as the renderer
  will once a volume stands; a presentation in depth, as `sandbox/3d` draws,
  orders what overlaps by a depth buffer over every layer projected, while a flat
  render's overlap stays the render priorities', which do not conflict with it.

What a game in depth still needs, and why it waits, is the todo's
[volume item](todo.md#play-a-volume-the-third-axiss-remaining-foundation).

## 4. Invariants

What the index keeps, across multiplayer, netcode and interactions:

- Determinism: Shared positions, each cell's order (`WorldOrder.Cells`) and every
  query's answer alike on every instance; the journal digest hashes positions,
  and the divergence harness and the cross-instance tests read as before.
- Domains: the Shared and Player halves of a cell (`PrivateBudget`), geometry
  over both (D-12), bounds as Shared authority (D-14), prediction's private
  writes (D-18), the staging world's grid.
- Records: positions ship as rows and the index is derived; the rock derives from
  the cave's record, so a capture carries neither the planes nor the surfaces;
  any change to what ships moves `snapshot.Schema` and `JournalSchema`.
- Interactions: the wall push and crush (`displaceComposite`,
  `World.ShiftComposite`), drift's layer `Shift` and scenery behind rock and walls,
  splash, loot and spawn placement, tracer and line walks, typing's top glyph,
  every renderer's read.
- Cost: a change here is measured before and after on `TestTickProfileTD16`,
  `BenchmarkCaveStep`, `BenchmarkCaveStepNavigation`, `BenchmarkCaveNavigation`,
  `BenchmarkNavigationWallMoves`, `BenchmarkMazeSpawn`, `BenchmarkRouteGraph` and
  the renderer benches, as §5 reads them.

## 5. Costs

Benchmarks in a cloud container move a fifth between runs, so a change is read
as main's and the branch's test binaries alternated five rounds on one container,
medians compared, the branch's bench files on both; td's tick is
`TestTickProfileTD16`, 500×250 with sixteen cursors over 5,000 ticks on one core.
A difference inside the band is noise, and a reading that holds in every round is
not. What each change read, against the main it landed on:

- The move itself, the grid and the geometry behind the binding: parity, once the
  bounds were the grid's and the wall test a predicate rather than an interface.
- The planes: td's tick 0.40 to 0.34 ms; the area query, a sixth of the tick,
  off the top of the profile; a wall's move pays two cell scans.
- Navigation over the words: a whole derivation of a 5×3 footprint's passability
  and its search grid on a 500×250 map 2.4 to 0.7 ms; the cave navigation benches
  3 to 10% under main.
- Surfaces and presence bits: the cave step 19 to 38% under main, the cave
  navigation 6 to 15% under, the presence bits alone a quarter off the step on
  the largest map; the glyph renderer 8% over at 200×60 and 400×120, 3 to 7 µs a
  frame, its probe reading the merged surfaces through a slice where it read the
  rock's bits from a field. That is the price of several surfaces behind one
  probe; a fixed array for the first few masks would take it back.
- The call sites and the binding, and the volume: parity, the compiler inlining
  the promoted calls as it inlined the forwarders, and a flat query's body and a
  cell's index unchanged at one layer.
- The direct path from the header, 16 headless vanguard games on main and on the
  branch: the direct ticks whose body was blocked 1.6% and 1.2% to 0.3% and 0.1%
  at 120×40 and 200×60, the rest the body's motion within the tick after
  navigation decided; the direct share 71% to 73% and 78% to 79%.
- The readers that paid a cell: the maze spawn, some 3,700 box-drawn walls laid
  and taken down whole, 7.5% under main at 200×60, 18% at 400×120 and 11% at
  500×254; the route graph 3.5% under, its fill a small share of its Dijkstra.

Across the changes td's tick read from 0.40 ms before the move to 0.29 ms, each
step against its own main; the tables each PR carried are in the git history.
