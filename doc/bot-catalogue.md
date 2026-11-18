# Bot catalogue

What a bot graph's guards and `PointAt` take as a target, and what a network
reads and asks for ([Bots](bots.md#networks-and-learning)). It is generated from
the tables of `internal/bot/catalogue.go`: change a row there, then run
`VIF_WRITE_GENERATED=1 go test ./internal/bot -run Reference`. `vif -schema`
lists the same inputs, outputs and scales under `bot_catalogue`.

## Kinds

A kind is a class of thing the world holds. A guard or `PointAt` names one as
its `target`, nearest the cursor over the whole world; a network reads the
nearest ones the view shows, each by its fields.

- `glyph`: typeable text: a glyph neither gold nor part of a composite. A
  target.
- `gold`: a gold sequence member. A target.
- `nugget`: a nugget. A target.
- `species`: a combat entity other than a cursor or a tower. A target. A network
  reads the 3 nearest, each by `x`, `y`, `near_x`, `near_y`, `enraged`.
- `kraken`: a Kraken, by its body, which may stand past a view that shows a leg;
  its nearness is that of the nearest cell it covers, legs and all. A target. A
  network reads the nearest, by `x`, `y`, `near_x`, `near_y`, `reach`.
- `cursor`: another participant's cursor. A target.
- `threat`: a species, a hostile bullet or a hostile missile, all weighted
  alike. A target.
- `button`: a scenario's button, or with key the one on that key, at its cell
  nearest the cursor. A target.
- `random`: a cell the view shows; PointAt only. A target.
- `landing`: a cell something is about to land on: a raider's landing marker,
  where its charge or teleport ends, or the cell a Kraken's charge ends on while
  it spins up and charges. A network reads the 3 nearest, each by `x`, `y`,
  `near_x`, `near_y`, `soon`.

## Inputs

A network's `inputs` name what it reads, in the order its weights take them.
`<kind>_<field>` is the nearest of a kind, `<kind>2_<field>` the second nearest
and so on. Every reading keeps to its scale, and what is not sighted reads 0.

### Fields

- `x`, across: its offset across.
- `y`, down: its offset down.
- `near_x`, near: its nearness across.
- `near_y`, near: its nearness down.
- `enraged`, flag: 1 while it is enraged.
- `soon`, share: how soon it lands, from 0 as its warning starts to 1 as the
  body leaves for it.
- `reach`, share: how far out its striking legs are.

### The rock

- `ceiling`, down: the rock's edge above a column, the view's own row where no
  rock stands.
- `floor`, down: the rock's edge below a column, the view's own row where no
  rock stands.

Each is read at a column, by what its name ends in:

- `ceiling`, `floor`: the cursor's column.
- `ceiling_near`, `floor_near`: the column an eighth of the view ahead.
- `ceiling_far`, `floor_far`: the column a quarter of the view ahead.

### The bot's own

- `column`, place: the cursor's place across the view, -1 at its left edge and 1
  at its right.
- `energy`, half at 1000: its energy, whichever its polarity.
- `heat`, half at 10: its heat.

### Scales

- across, -1 to 1: an offset from the cursor's cell in view widths, within 1 of
  zero for a thing the view shows.
- down, -1 to 1: an offset from the cursor's cell in view heights, within 1 of
  zero for a thing the view shows.
- near, -1 to 1: one part of a nearness: a vector toward the thing, 1 long
  beside the cursor and falling to nothing a quarter of the view's width away,
  rows counted twice.
- place, -1 to 1: a place in the view, -1 at one edge and 1 at the other.
- flag, 0 to 1: 0 or 1.
- share, 0 to 1: how much of its full it stands at.
- half, 0 to 1: a count c read as c/(c+half): 0 with none and toward 1, a half
  at the count its row names.

## Outputs

What a network asks for, in the order `Steer` acts on it. A graph writes an
output's weights in a table named for each of its units.

- `special`, units `special`: asks for the special attack while it is positive,
  ahead of the move and as often as auto-fire would.
- `move`, units `x`, `y`: points at a cell: x and y are its offset from the
  cursor in view widths and heights, kept to the cells the view shows.
