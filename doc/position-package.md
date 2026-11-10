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
  `PrivateBudget`), indexed `(z*Height+y)*Width+x`; `Scope`; a cell of sixteen
  bytes that holds its occupant while that is one, and a crowd of up to
  `CellCapacity` for a cell of two or more; the presence bits of each class
  (`Present`), the layers' rows in turn, by which a probe passes an empty cell on
  a word that stays in cache, a scan passes empty cells a word at a time, and a
  copy, a clear and the planes visit only the cells that hold anything
  (`EachOccupied`); `Clamp` and `ClampVolume` under the one cell ceiling; the
  gauges, counted as cells fill; and the load and reorder a world copy and an
  install use.
- `Index[E]`: the grid, whose dimensions are the bounds every query answers
  within; the `Frame` it reads the cell aspect through; `blockOf`, the caller's
  word on which shared occupant is a block and its kinds (a `Mask`, 0 asking for
  any block); a plane of bits for each mask a query asked, kept with every block
  change, so a point is a bit and an area a few words a row; the surfaces; the
  block log; `BlockBits`, the plane's words with the surfaces' solid cells for a
  reader deriving whole; and the searches: free area by spiral, by pattern and
  around an exclusion, point and area line of sight, rays, the closest occupant
  in a direction. Each query and mutator takes its cell as either point
  (`Coord`): a `Point`, its cell on the first layer, which a flat game passes
  as it holds it, or a `Point3`, a cell of the volume; a region is a
  `vmath.Area` or a `Box` (`Region`), a body a `Footprint` or a `Footprint3`
  (`Body`). A search answers in the point it was asked in, on its origin's
  layer, with the zero point and false where it finds nothing.
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
  `Shift`, the batch, `Resize` and `Clear`, which place the rows again or drop
  them, the wall flag beside each row that tells the index which moves are a
  wall's, once the grid holds the move, `wallOf` over `Components.Wall` as the
  index's `blockOf`, the typing queries (`TopGlyphAt`, `IsTypeable`,
  `IsScenery`), telemetry, and the rock: `RockField`, the two surfaces with their
  columns. `component.WallBlockMask` is the package's `Mask`.
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
- Blocks, a plane of bits per mask asked, read with the bits of the merged
  surfaces that stop the mask: a probe tests one pointer, which says the plane is
  current, and reads the plane's word joined with the surfaces', so a point, an
  area and each step of a line cost the same loads with surfaces laid or none. A
  plane is built from the cells on the first query of its mask and brought to a
  cell's shared occupants by `BlockChangedAt`, which the binding calls once the
  grid holds a wall's move, so a storm's hundreds of walls cost a cell scan each;
  a resize, a copy loaded, masks rewritten wholesale or a surface laid leave work
  the next probe does first (`spoil`). Scanning the cell's occupants on every
  probe through a predicate put the area test, navigation's direct-path probe
  over a footprint, at a sixth of td's tick. The walls stay entities, which
  species move and break; the planes are derived, never captured.
- Cells, sixteen bytes with the crowds apart: a cell holds its occupant inline
  while it holds one, as 94 to 99.8% of the occupied cells of main, td and
  vanguard do in play, and a cell of two or more keeps them in a 256-byte crowd
  from a slab that reuses its places, a few hundred at most in those games. The
  map ceiling costs 2 MB of cells where it cost 32, and a move reads a cell and a
  presence word where it read a 256-byte cell and the occupied list. Each cell's
  order is what one run of 31 made it, held against a model
  (`TestGridHoldsEachCellAsItsRulesMakeIt`), so `WorldOrder.Cells` and every
  enumeration read as before. A four-byte slot a cell over a dense slab of the
  held cells read moves and installs lower still but an occupant probe at the
  ceiling half again over main, a second load waiting on the first, and its
  swap-removal let a view of one cell show another's occupants after a move
  elsewhere.
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
  again on the first probe after it. A plane holds the words of the merged bits
  that stop its mask (`plane.solid`), bound again at each merge, so a probe reads
  them with its own. Four shapes read worse: each surface tested on its own, by
  its heights, read the glyph renderer a tenth slower at 400×120; a merge of the
  heights on every shift, allocating, read the cave step at twice main's; the
  merged heights compared on each probe, without bits, still read the glyph
  renderer a tenth slower; and the merged bits of every mask tested in turn
  before the plane's, which the plane's own words replaced, read it 9 to 12%
  slower at 120×40 and 200×60. Typing's reads of a glyph's own cell (`TopGlyphIs`)
  take the cell without the presence test, since the cell holds the glyph. The
  game's rock is two surfaces that move together; a floor alone or a ceiling
  alone is one. The surfaces lie on the first layer; a volume's is a
  heightfield, not laid.
- The mask, the package's: `component.WallBlockMask` is `position.Mask` by alias,
  since the package names no game type and the game's constants stay where they
  are, passed to the package's methods unchanged.
- Presence bits, a `Bits` a class kept by the grid as cells fill and empty: an
  occupant probe (`EntitiesAt`, `HasAnyEntityAt`) tests the bit before it reads
  the cell, so the empty cells a sweep, a particle or a shot asks about cost a
  word in cache rather than a cell out of it, and the directional scan the
  motions make reads the rows' words alone, the band's joined for a scan along a
  row, and visits the cells whose bit is set. An occupied cell pays one more
  load, inside the band.
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
  (`Point.Layer`), which inlines. A flag beside each row says its entity is a
  wall, read from the wall store when the row is made, when the wall's component
  comes, goes or is rewritten (`WallChanged`) and when rows are written whole (a
  copy in, an order installed), so a move tests a byte of the row it holds where
  it looked the entity up in the world's component masks, a map. A destroy
  empties the wall store before the row goes, and the flag still names the wall
  whose cell the log takes. A store keeps one batch, begun again in its own
  storage, whose commit sorts the additions' cells to find two on one cell.
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
  float vector of a body's physics.
- One index for both, its methods generic over the point: `Index[E]` keeps its
  depth as a value, and a query is a generic method over `Coord`, `Region` or
  `Body`, so the one `World` every game shares asks one index, a flat system
  with the `Point` its rows hold and a volume's with a `Point3`, and a volume
  can come up a system at a time. The form is read from the type's size, a
  constant in each instantiation (`at`, `as`, `box`, `body`: the package's one
  use of `unsafe`, held at compile time to two forms that differ in size), so
  the flat instantiation compiles to the flat function's instructions; a type
  switch on the parameter read a probe 10 to 20% slower and a constraint with
  methods a quarter. A box or a deep footprint comes back from those helpers in
  parts: six fields leave the registers four stay in, which cost the area probe
  half again to twice. What it costs: no interface can hold a generic method,
  every signature carries a type parameter, and nothing at compile time stops a
  `Point3` on a flat map, where a layer past the first is outside it. An index
  typed by its coordinate, `Index[E, P]`, which the compiler would hold to one
  point, is the todo's [item](todo.md#an-index-typed-by-its-coordinate-indexe-p)
  for when flat and volume games run on the engine together.
- The grid: `Depth` layers, one on a flat map, the cells indexed
  `(z*Height+y)*Width+x`, the planes and the presence bits each one `Bits` whose
  rows are the layers' in turn, so a cell's index names its row and a flat query
  costs what it did. The one ceiling bounds the volume's cells as the map's
  (`ClampVolume`), so a volume stays small while every cell is dense, 100×50×25
  at most.
- The queries: a `Point` is its cell on the first layer and a `Point3` answers
  in its own; a line of sight through a volume walks `vmath.Line3` in place, the
  axis of the longest span stepping every cell and in one plane the cells `Line`
  walks; a body of depth is tested through the layers it spans (`plane.deep`),
  one layer being one probe. Every search, the spiral, the pattern, the
  exclusion and the directional scan, works the layer its origin names, as
  placing on a floor: what a layer means, a storey or a slice of depth, is the
  game's, so a search crossing layers is the todo's
  [item](todo.md#placement-searches-that-cross-a-volumes-layers) for a game
  that needs one.
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
as main's and the branch's test binaries alternated five rounds or more on one
container, medians compared, the branch's bench files on both; td's tick is
`TestTickProfileTD16`, 500×250 with sixteen cursors over 5,000 ticks on one core.
A difference inside the band is noise, and a reading that holds in every round is
not; a tick reads in two modes there, a third apart, so a few percent is read by
the minimum of seven rounds beside the median. The package's own benches
(`pkg/position` `bench_test.go`) read each operation on a scrolling level's map
and on the ceiling's with a maze's walls. What each change read, against the
main it landed on:

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
  rock's bits from a field, which the plane holding their words took back below.
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
- The flat point, two ints where three stood: a position costs 16 bytes in a row,
  a log entry or a trail cell where it cost 24, and a capture no column of
  zeros; the world copy at the map limit 8% under main fresh and 3% reused, td's
  headless ticks 6% under, td's tick with sixteen cursors at parity.

- Sixteen-byte cells, the plane read with its surfaces, and the scan over the
  presence words, on the package's benches at 200×60 and at the ceiling: a probe
  41% under main, under two surfaces 20 and 26%, a body's area 8 and 21%, its
  line of sight 17 and 23%, an occupant probe 6 to 8%; a move 28 and 50%, a
  layer's shift 33 and 48%, a wall's move 26 and 31%; the directional scan 46 and
  52%; an install 77 and 70%; the gauges 1.6 and 301 µs to 20 ns. In the game:
  td's tick with sixteen cursors 0.129 to 0.108 ms; headless ticks 9% under on
  main, 11% on td and 20% on vanguard; the maze spawn 9 to 13% under; the glyph
  renderer 12 and 9% under at 120×40 and 200×60 and at parity at 400×120, the 8%
  the surfaces cost it taken back; the cave, navigation and route benches inside
  the band or under; the world copy 3 to 5% under. A td world's grid holds 2.1
  MB where it held 32, its crowds 64 kB of that.
- The rows' wall flag, a shift keeping each entity's row between its phases and
  the one batch, on `BenchmarkPlacements` at 200×60 against the cells above: a
  shared body's move 45 to 32 ns, a wall's 10 to 18% under, a layer's shift of
  1,800 glyphs 2 to 4% under, a batch of ten half the time, its 760 B in six
  allocations gone. In the game the headless tick allocates four times where it
  did six on main and eight where ten on td, a fifth to three tenths fewer bytes,
  and the maze spawn 24 times where 27; the ticks' time reads 1 to 4% under by
  the minimum, inside the band.
- The volume's line walk, its axes kept from the longest span's on and stepped
  in place (`Line3.Advance`) where each step copied the walk out and back, on a
  100×50×25 volume with no wall, segments some forty cells long: a line of sight
  800 to 230 ns, a 3×3×2 body's 1.05 to 0.8 µs; `vmath`'s `BenchmarkLine3` 68 to
  10 µs.
- One index generic over the point, against the line walk above: a flat probe,
  area, line of sight, occupant read, move, shift and scan compile to the
  instructions they had and read level on the package's benches and the game's.
  What read over was link layout: the gauges and an occupant probe at the
  ceiling 12 to 17% over in one build, their instructions the same, and the
  glyph renderer at 200×60 10% over in one and level across six
  (`-ldflags=-randlayout`), the earlier build's own six 18% apart. The placement
  searches on a flat map read under, the spiral 4 to 8% and the pattern search
  6 to 12%, a candidate being one area test where it bounded itself and then
  asked `IsAreaFree`; the exclusion search level on an open map and 28% under
  in a maze, its last resort counting a slot's cells off one plane. A lone
  `IsAreaFree` reads half a nanosecond over, its layers bounded at run time.

Across the changes td's tick read from 0.40 ms before the move to 0.29 ms, each
step against its own main, and 0.13 to 0.11 ms with the cells; the tables each PR
carried are in the git history.
