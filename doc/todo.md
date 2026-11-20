# TODO

Scratch notes that travel with the repository. Each item states what remains and
the smallest thing that would close it. Delete an item when it lands; this file
is not a changelog. Source files do not carry parallel TODO comments, so a
code-originated item names its source here.

Priorities: P0 blocks a release or leads the feature development push, P1 is
wanted next, P2 is convenient follow-up, and P3 is an idea.

## Cave

The side-scroller's refinements after its first play test (2026-10-27) lead, grouped
by area so an agent can take a group. The faster scroll, the jagged cave, the stone
art, the raider types by cave, the scenery text, the boss stage, the machine's
certain faults, navigation's cost over the cave and the position package
([Position](position-package.md)) have landed. What is left is the machine
follow-ups. A cave's shape stays in `pkg/cave` and its scenario, the rock behind
`Positions.Rock()` and the wall system and renderers that read it.

### Machine follow-ups

What `cave` met in the machine and is still open, its certain faults having
landed.

#### Give a program's entrance an entering state

- Priority: P2
- Affected files: `internal/behavior/program.go`, `internal/system/behavior.go`
  (`enter`), `internal/component/behavior.go` (`BehaviorEntrance`)
- Guide: [Behavior machines §4.4](machine-design.md#44-spawns-that-ride-their-materializes-and-spirits)

A prototype's `entrance = "materialize"` plays its own materialize style over the
footprint for its `entrance_ms`, the style's whole when that is 0, and lands the body
as it ends, beside any rock that came over it, and nothing of it reaches the machine,
which starts in `initial` the tick the body stands. Lixen expects entrances to grow for machines (2026-10-27).
What a program would declare next:

- Whether a countdown shows over the materialize.
- An entering state the machine runs while the materialize holds, so a program can
  telegraph, re-aim or cancel its arrival; while it runs the body neither strikes
  nor takes damage, as no body stands yet.
- A landing its probe chooses (`FindLanding` or its own) as the materialize ends,
  in place of the nearest clear footprint.

The materialize is Shared, so the entering state's clock and probe must run alike
on every instance and a capture mid-materialize must restore them
(`snapshot.Schema`).

### Position, blocking and navigation

The move into `pkg/position` is done ([Position](position-package.md)); what
remains stands on its own.

#### Play a volume: the third axis's remaining foundation

- Priority: P3, not now: it needs other foundation changes first
- Affected files: `internal/engine/position.go`, `internal/app/internal/config`,
  `internal/snapshot`, `internal/system/navigation.go`, `pkg/navigation`,
  `internal/render/renderer`, `pkg/vmath`, `pkg/position/surface.go`
- Guide: [Position §3](position-package.md#3-the-volume),
  [Rendering §2](rendering.md#2-render-context-and-coordinates)

The index, the math and the physics are laid for a volume: `pkg/position` holds
`Depth` layers and answers a `Point3`, a `Box` and a `Footprint3` on any of them,
its surfaces on the layer they name, and `pkg/vmath` and its physics have a form
through a volume beside each flat one. What a game in depth still needs is not
laid:

- The binding and the records: a depth in the config the binding resizes to
  (`Position.Resize` passes one layer), and a row that carries its layer, which
  moves the journal, the captures and the digest, a row being a flat `Point`.
- Navigation: a footprint with depth and a passability per layer, where one
  flat grid is derived from `BlockBits`.
- Rendering: a flat renderer reading the layer it looks at, and a render in
  depth ordering overlap by a depth buffer over every layer projected
  (`sandbox/3d`).
- The forms left flat for want of a use: the cell strip `Ray` and `Octants`,
  the arcs on a sphere, a depth in `vmath.Aspect`; the searches that cross
  layers are [their own item](#placement-searches-that-cross-a-volumes-layers).
- A heightfield along the third axis: a height for each column and row, solid
  through the layers under or over it. A `Surface` a layer is a field over
  columns and layers only.
- The ceiling: `MaxCells` bounds a volume as it does a map (`ClampVolume`), 2 MB
  of cells, 100×50×25 or 200×60 ten layers deep. A game in depth raises the one
  constant, and past what stays dense needs cells chunked on demand.

#### An index typed by its coordinate, `Index[E, P]`

- Priority: P3, to evaluate when flat and volume games are to run on the engine together
- Affected files: `pkg/position`, `internal/engine/position.go`,
  `internal/engine/world.go`, every call site of a query
- Guide: [Position §3](position-package.md#3-the-volume)

The choice made: one `Index[E]` whose depth is set at `Resize`, each query a
generic method taking a `Point`, its cell on the first layer, or a `Point3`, the
two told apart by size at compile time, at no cost to a probe.

What the other form does: the point is the index's second type parameter, a flat
map an `Index[E, Point]` and a volume an `Index[E, Point3]`. Its methods are plain
ones taking `P`, the block log is in `P`, and the compiler refuses a `Point3` on
a flat map.

What it entails: the engine's store embeds the index and every game shares one
`World`, so `P` is one type for the whole binary. A volume game beside flat ones
then needs a `World` generic over `P`, or a second store, or `Point3`
throughout: every game's rows, log and captures carrying a Z, and every flat
call site, the 155 the conversion rewrote among them, passing a `Point3`. Go
derives no area or footprint type from `P`, so those queries stay split by name
or stay generic methods.

Why it was not chosen: with one `World` for every game, a flat system passes the
`Point` its rows hold and a volume's a `Point3` to the same index, a flat row
stays two ints, and a volume comes up a system at a time. The form taken pays
for that with generic methods, new in Go 1.27, which no interface can hold and
tools may trail, a type parameter on every signature, and nothing at compile
time between a `Point3` and a flat map. A probe costs the same under both.

Evaluate it when a flat game and a volume game run on the engine in one build:
if each then has a world of its own type, the point belongs on the index.

#### Placement searches that cross a volume's layers

- Priority: P3, when a game in a volume needs a place found off its origin's layer
- Affected files: `pkg/position/search.go`, `pkg/vmath` (`Aspect`, the diagonal scale)
- Guide: [Position §3](position-package.md#3-the-volume)

The spiral, the pattern search and the exclusion search each work the layer
their origin names, a body with depth tested through the layers it spans, and so
does the directional scan. What a layer means, a storey or a slice of depth, is
the game's, so none was guessed. A form of all three that crosses layers
searches 26 directions where a layer has 8, and has to settle: the order the
layers are tried in, a depth in `vmath.Aspect` beside the cell's width and
height, the scale of a corner diagonal (1/√3, beside the 1/√2 of
`DiagonalNum`/`DiagonalDen`), what clockwise means off the plane, whether the
pattern's area gains a depth, and the slots before and behind an exclusion that
has one. It goes beside the layer forms and changes no caller of them.

#### Trim td's storm ticks

- Priority: P3
- Affected files: `internal/system/navigation.go`, `internal/system/storm.go`,
  `pkg/navigation/flowfield.go`
- Guide: [Navigation §10](navigation-design.md#10-costs)

With eight cursors walking and a storm raised, a td storm tick averages 1.9 ms
and its worst is 12–15 ms of the 50 ms tick (Navigation §10), the snake lock's
reaches stopping at their choice. Walls going are no peak: 235 at once cost the
wall and death events 0.3–2 ms, and a storm left alone takes 24 a tick at most.
What the slowest hundredth of the storm's ticks holds, each measured there:

- Fields, 43%: three fifths point fields, the private fields of the cursors the
  snake heads lock and the drops follow, recomputed at the cooldown as each
  cursor walks; two fifths the eyes' composite fields. Settling a field only out
  to its farthest reader reopens a decision taken on the `cave` scenario's numbers
  (Navigation §12).
- The eyes' own update, 24%, and the reaches, 21%.
- A storm circle's X and Y checks each ask for the walls they meet, so a wall in
  both footprints is asked for twice: twice the requests where 235 go, whose wall
  event costs 0.1–1 ms.

A td reset is the one place the wall system costs tens of milliseconds, laying
the maze again (Trim what a tick spends outside the position index).

## Armada

Armada is a vertical scroller with no terrain: the drift carries its stars down
past the ship, its ranks stand overhead and its squadrons fly routes into circles
([Gameplay §10](gameplay.md#10-embedded-campaign-progression)). A terrain under
that scroll is what is left, likely the first scenario built on a painted map
([Authoring](#authoring)); its items follow the editor's foundation. The bots'
armada figures ([Bots](bots.md#examples)) want reading again on it.

### Scroll a terrain past armada

- Priority: P2
- Affected files: `wad/scenario/base/armada/`, `internal/system/wall.go` (the cave
  step), `pkg/position/surface.go`, `internal/engine/position.go` (`RockField`),
  `internal/system/particle.go`, `internal/render/renderer/rock.go`,
  `internal/event/payload.go` (`CaveStartPayload`)
- Prerequisite: [Lay a painted map a scenario carries](#lay-a-painted-map-a-scenario-carries),
  for the first form
- Guide: [Gameplay §9](gameplay.md#9-walls-maps-navigation-and-environment),
  [Position §1](position-package.md#1-the-boundary)

The map comes down past the ship as its stars do, with the ranks and squadrons in
it. The game has one scroll, the cave's: a profile of columns shifted left a column
at a time (`ShiftRock`), which pushes what it meets left, crushes a cursor against
the left edge and carries particles left. Given a direction and a source, that one
scroll is each form a terrain may take:

- **A painted map, likely the first.** The map's rows enter at the top as the
  scroll steps. Its tiles are a third kind of surface, blocks laid from the map's
  record as rock is from the cave's (Position §1 leaves a *shape* of that kind
  unbuilt), where walls as entities would move every one in view each step. What
  a row carries beside walls, a program's spawn or a loot, is raised as it enters.
- **Procedural rock.** The cave on its side, endless: `pkg/cave`'s profile is a
  function of an index and serves rows as it serves columns.

What the direction needs, whatever the source: a surface of widths a row with a
solid side, where `pkg/position` holds heights a column (`Above`, `Below`) and
shifts a column; the cave step's push, its crush edge (`EventCursorCrushed`, here
the bottom) and the particle carry along rows; the rock renderer; and names, the
events and `CaveResource` being the cave's.

To settle with the design:

- The painted map's other route, a view scrolling over a map laid whole. It is a
  second scroll beside the cave's, and every reader of the map's edges (a
  cursor's bounds, a spawn search, the glyph spawner, a bounce, the camera) then
  needs a window beside the map.
- The pace: the rock steps whole cells, and a row is twice a column's height on
  screen, so rock at the drift's pace in rows reads twice as fast, and a 40-row map
  shows a third of what a 120-column side-scroller does ahead (`[map] min_height`).
- Whether the ranks keep their place on the screen, as now, or pass with the map,
  and how a squadron's routes, fractions of the map, meet the rock.

[Gameplay §10](gameplay.md#10-embedded-campaign-progression) and the soak test
follow the terrain.

## Bots

### Have a scenario describe the bots that play it

- Priority: P1
- Affected files: `internal/bot`, `internal/fsm/file_loader.go`,
  `internal/resource/scenario.go`, `internal/app/internal/membership/seat.go`,
  `internal/app/script.go`, `internal/mode/commands.go`, `cmd/vif`
- Guide: [Bots](bots.md#networks-and-learning), [Bot catalogue](bot-catalogue.md)

A scenario carries the bots made for its map among its own files, and the user
root's `bot/` holds a player's alone: what they made, changed or trained
([Bots](bots.md#writing-a-graph)). Built: the catalogue a network reads through
([Bot catalogue](bot-catalogue.md)), with no `loot` and `threat` a target alone;
the bot document, a network's `while`, a `Steer` over a list, `InState`,
`outputs`, `[bot] inputs` and every weight written; a scenario's `bots`, a seat
that names no graph playing the first; `cave`'s two in its scenario, `seed`
the start written by hand and `cave` a model learned from it, run K's best of
four, which fields 300,260 over 100 rounds of seeds 101 to 104 where `seed`
fields 27,614; the verbs, one grammar after `:bot` and `-bot`, that keep a
player's bot files ([Bots](bots.md#a-players-bot-files)); and `-check` holding
a scenario's bot to the regions and states its `InState` names. Decided (Lixen,
2026-11-18), what is left, a PR a step:

- **The verbs' edges.** `export` writes a network's weights at the document's
  end, not beside its inputs; a word after a verb is split at spaces, so a path
  holding one is not taken there; an address names one network or all, so two
  of three take two commands; a population of another scenario is reached by
  `-s` alone; the menu's Bots page names populations only through its Bot
  files row; and `clear` in a run learning the population is undone by that
  run's next save.
- **No trainer for `main` or `armada`** (Lixen, 2026-11-20). The embedded
  scenario's typing bot cannot lose, and a round's score moves only by how
  fast it gets on, so the walk six minutes fielded there is left as it is
  ([Bots](bots.md#examples)). `armada` waits for its design to settle.
- **Training on from `cave`'s model.** Every run adds its rounds to the one
  population, a few seconds' too, and a run can field less than it began
  with: of eight read on the model before this one, two field 284,269 and
  285,950 over 200 held-out rounds where the weights they began with field
  308,501 ([Bots](bots.md#the-population-file)). Four runs of fifty minutes
  on from that model field 341,701 and ship as this one; what limits it was
  read on the one before, over 100 held-out rounds:
  - **The score has a ceiling.** A round closed at its half hour pays by the
    Krakens shot down in it, 375,000 for nine, 214,000 for eight and 132,000
    for seven, and none shoots ten: `cave`'s model has nine in 72 rounds and
    fields 300,260. A bot document's `round_seconds` is its own to raise.
  - **Twelve rounds are lost in the first 1,900 ticks**, nine crushed at the
    rock. The model stands within a row of the rock 80 ticks in 100, since its
    `raid` learned a `ceiling` weight of -0.058 on `y` where `floor` kept
    0.05: a push down whatever the gap. Set back to 0.05 by hand it is crushed
    once and fields 187,300, a Kraken slower in most rounds. Learned on from
    so, fifteen million ticks field 201,942 on seed 1 and 267,101 on seed 2,
    one round and none crushed, at the rock 73 and 76 ticks in 100, nine
    Krakens down in 29 rounds and 62: seed 1's `raid`, its archive then ranked
    by the plain mean, kept no member of more than three rounds and fielded
    the weights it began with, and seed 2 lost 17 rounds early to the
    raiders. From `cave`'s own weights the same ticks field a `raid` that
    still stands at the rock 80 ticks in 100, loses five rounds early, two
    crushed, and shoots nine Krakens down in 80.
  - **A nudge in the pay**, tried in a scratch tree and not built: a learner
    is paid no column scrolled while a block stands within two rows above or
    below its ship. Under the rule that fielded the best of eight rounds it
    is crushed less and fields no more for it, within what luck moves: after
    three million ticks 313,827 on seed 1 and 287,986 on seed 2, five rounds
    and one crushed and ten and twelve lost early, and after fifteen million
    on seed 1 323,960, four crushed and six lost. It is `cave`'s own
    knowledge in the bot package's scoring: a bot document would declare it,
    as it does `while`.
  - **A lower limit does not help.** Trained with rounds closed at 150
    seconds, where the early losses are, three million ticks fielded 20,441
    on seed 1 and 107,101 on seed 2: `kraken` is scored on fights cut before
    the kill pays, and `raid` on its first cave alone. That `raid` beside
    `cave`'s own `kraken` fielded 315,649 on seed 1, four rounds lost early,
    none crushed and 96 at the limit, and the 107,101 on seed 2, twenty lost.
  - **A trainer found one model and no more.** Nineteen cycles of fifty
    minutes scored 105,519 `raid` rounds and 60,241 `kraken` rounds: the
    fourth's pair ships, a later `kraken` title read level with it, -0.07 se
    over 800 rounds a side, and the trainer is stopped, its populations kept
    on `claude/cave-training-state`. Each title and its rival have 7,425 and
    3,084 rounds apiece, a seventh and a tenth of all played, and no other
    proved member can pass the rival: a `raid` at eight rounds would need a
    mean of 71,268, over what a full round scores. None should. The twelve
    whose means over 8 to 37 rounds stand above their titles, held 100
    rounds each beside the shipped other network, read under the shipped
    pair in eleven, by 3,168 to 85,159, and the twelfth level, -0.11 se over
    400.
  - **What ranks, and who is tested**, after the archive that ranks by what
    rounds prove ([Bots](bots.md#learning)). Two arms were read on the starts
    and seeds of [the population file](bots.md#the-population-file), each
    alone and both with the step fixed, and neither is built: each does
    inside a population what it was made for, and no run fields more for it.
    - **The trust weight**, the rounds at the recent mean a member's mean is
      weighed with. A repeat's round is foretold level at 1.7 for `raid` and
      2.2 for `kraken` in populations played on from `cave`'s and `seed`'s
      weights, at 5.1 and 7.1 from nothing, and at 3.7 and 3.3 over every
      run, where eight foretells 13% under. The networks differ by less than
      a start's own spread, and the starts do not. Run at two and at four,
      the proof left at eight rounds, it moved the rank, a candidate's entry
      and the guess proved next by nothing one run tells from another, and
      the title: over fifteen million ticks on `cave`'s, both seeds, `raid`'s
      title turns went to 10 members under eight, 19 under two and 13 under
      four, and the title changed hands 9, 38 and 23 times.
    - **The rival as the proved member with the highest plain mean.** Under
      eight a round finds 1.98 and 1.24 proved `raid` members, seeds 1 and
      2, with a plain mean above the fielded one's and no round in the last
      fifty, five on each seed as the run ends, the stalest 277 and 753
      rounds back. The weight leaves 0.11 and 0.28 at two and 0.19 and 0.23
      at four. The rival leaves none, its turns going to 22 members and the
      title changing hands 11 times.
    - Held out, the mean of 100 rounds, from nothing that of twelve seeds'
      means, `together` the weight at four, the rival and a step of 0.03:

      ```
      start          seed    eight      two     four    rival  together
      cave's   3M       1   300260   300260   300260   300260    312488
                        2   318164   318164   300260   318164    300260
              15M       1   321365   311679   317867   284145    263009
                        2   318164   328955   306664   330962    338154
      seed's   3M       1    47746    58870    52928    49647     46348
                        2    68569    49434    79985    67052     55661
                        3    78865    54655    59690    78865     54252
      nothing     1 to 12     5466     5301     6441     5392      6083
      ```

    - No start tells an arm from luck, which on `cave`'s is wide: two
      learners alike past a population's fifteenth round field 28,511 apart
      ([Bots](bots.md#the-population-file)). The widest gap is seed 1 after
      fifteen million ticks under the rival, alone and with the weight: over
      200 rounds its pairs field 281,099 and 254,257 where the learner's as
      built fields 321,544, and lose 33 and 40 before the first Kraken for 12.
    - Giving promise its turn passes the title on fewer rounds. As fifteen
      million ticks end `raid`'s title has 55 and 72 rounds behind it as
      built, 45 and 48 under the rival and 14 and 39 under the two together,
      and `kraken`'s 40 and 32, 15 and 28, 11 and 19. A fielded `raid`'s
      full round scores 65,000 to 71,000 and one lost before the first
      Kraken 3,000 to 7,000, four in fifty, so its mean over 33 to 72 rounds
      carries 3,000 either way and fourteen with none lost say little: both
      of seed 1's pairs field a `raid` its own rounds put high, 55,864 over
      45 and 61,735 over 14. A `kraken`'s rounds score the cap, 306,500, or
      less, and one took the title on fifteen at the cap.
    - **A title held only on 24 rounds**, the proof left at eight for rank
      and breeding, was read alone and with the weight at four and the rival
      each on top, and is not built. It was judged by what luck does not
      pick, on six seeds a start: the candidates' recent mean, a hold of
      every proved member, twelve rounds each beside the other network's
      most trusted, and beside them the pair a run fields over 100. The
      starts are nothing after 600,000 ticks, `seed`'s weights after three
      million and the model fielding 341,701 after fifteen; a figure is the
      seeds' mean and its standard error, `recent` and `proved` `raid`'s:

      ```
      start   read        as built        title   and weight    and rival
      nothing recent      3781±446     3549±263     3945±136     3782±486
              proved      4607±485     5008±892     4858±651     4166±777
              fields      6778±974     3225±176     3150±169     3429±335
      seed's  recent     19420±427    18474±891    18157±838   18193±1145
              proved    51662±5055   47869±3607   46480±2174   49579±6094
              fields    57236±4837   52923±3572   47273±4179   41276±2647
      cave's  recent     31589±386    30580±984    31325±994    31105±743
              proved   289582±6492 292002±10666  304584±9523  311333±8612
              fields  310945±10266  325885±8216  322200±5863  319848±6209
      ```

    - Nothing luck does not pick moves past two standard errors under any
      of the three, `kraken`'s reads as `raid`'s; the widest is `raid`'s
      proved under the rival on the model, 21,751 over at 2.0. What a run
      fields moves from nothing, where no `kraken` has 24 rounds inside the
      600,000 ticks and none is fielded: with the most trusted of the proved
      fielded where nothing is written and none holds the title, the rule
      fields 5,643±687. From `seed`'s the rival on top fields 15,960 less
      than the learner as built: it spreads the title's turns over
      claimants, and two runs in six end with no `kraken` of 24 rounds and
      the written one fielded.
    - The rule does what it says in a young population and nothing in an
      old one. As built a title changes hands 12.2 times a run from `seed`'s
      for `raid` and 6.0 for `kraken`, 5.8 and every one of them to a member
      under 24 rounds, and on the model 5.8 and 5.8, 2.3 and 4.3 so. Under
      the rule none does, and no member holds a title for 28 and 57 rounds
      in 100 from `seed`'s and 15 and 32 on the model, the written weights
      fielded meanwhile. On the trainer's populations (above) it is the
      learner as built, byte for byte: of fifteen million ticks' 153 `raid`
      title turns, 74 go to the title and 79 to its rival.
    - On those populations the weight and the rival each play the twelve
      and crown them. Under the weight a title passes at 24 rounds, `raid`'s
      6 and 13 times in fifteen million ticks on two seeds and `kraken`'s 11
      and 10. Under the rival `raid`'s goes at 39 and 40 rounds to the
      member of 57,289 over 18, which holds 302,886 beside the shipped
      `kraken` for the pair's 341,453, and it holds the title as both runs
      end. Held out as they end, the four runs field 326,325 to 331,531
      where the populations' own titles field 351,075.
    - Rounds do not tell a `raid` challenger from its title, as its own
      score does not say what it holds. Played by the rival, five of the six
      fell to the title's own mean inside 16 to 44 rounds, 51,673 to 52,193
      for its 52,325, though held out they stand from level with the pair to
      85,159 under. The sixth kept 54,793 over 228 further rounds of four
      runs, 2.0 standard errors over the title, and holds 38,567 under.
    - Where members differ, a title's own measure does tell them. Of 24
      runs a start, the most trusted of the proved holds 5,339±1,400 over
      the other proved from `seed`'s and 24,693±7,428 on the model for
      `raid`, 6,088±1,587 and 10,798±5,941 for `kraken`; the highest plain
      mean holds 12,508±10,535 over them on the model for `raid`.
    - Owed before a title's test is read again: a score for `raid` that
      says what its member holds beside the other network, since no count
      of rounds by its own score told the twelve from their title.
- **`default`'s network `walk`** learns in a run alone, a population a
  scenario, which a seat fields too, and writes no weight
  ([Bots](bots.md#writing-a-graph)). Owed:
  - a use for the wheel, or its end. `td` and `recall` are all text, where
    it is the network's one say, and 142 rounds of `td` learned nothing by
    it; the embedded scenario's model takes none from text and walks less
    than the unlearned, 13 to 14 ticks in 100 for 16 to 18. A start written
    by hand that asks for it as a threat nears is the one use left to try;
  - a bot of its own for `td` and, once its design settles, `armada`,
    carrying what `default` learns there as the embedded scenario and `cave`
    carry theirs: what one scenario teaches loses in another, so `default`
    writes none;
  - the gun for a bot whose auto-fire is off: the special attack is every
    shipped network's to ask for, and no output asks for the gun, which
    auto-fire `main` fires for each of them;
  - when to ask. The one weight written asks always, four of `default`'s
    sixteen actions a second, and no run has yet learned to ask for less;
  - a round that needs no new game: closing the embedded one each half hour
    earns a tenth less than an unbroken game does, and waits on a death that
    resets none;
  - which of its seventeen inputs earn their weights: sets of 13 to 25
    around them read alike over three seeds of the fixed-shooter `armada`,
    where the fielded members of one set ran from 6,489 to 50,826, so six
    seeds and longer runs are owed before one is told from another.
- **A move that names a place**, not built: `outputs` leaves room for one. An
  output's units are its own and a network has those of the outputs it lists,
  so a move to a place in the view takes `move`'s place in a list, where `move`
  is an offset from the ship. Networks that list none keep `special` and
  `move`. A five-minute probe from nothing left the edges, 42% of the raid in
  the outer tenths where it was 96%, and scored no better.

The population file stays JSON: one cut short never parses, where a TOML one
can read as fewer members. A learner in a run takes an import or a clear from
its next run.

### Let a scenario expose values of its own to a bot

- Priority: P3
- Affected files: `internal/bot` (the catalogue), `internal/engine/resource.go`,
  `internal/fsm`, `internal/resource/scenario.go`
- Prerequisite: [a scenario's bots](#have-a-scenario-describe-the-bots-that-play-it)

A network reads the world and its `while` the states of the scenario's regions,
and neither a variable of the scenario's own, as `cave`'s `raid_rank` or
`cave_speed`, though `ScenarioResource` lends them to behavior programs. An
input over one needs a scale its scenario declares, and a bot listing it plays
only scenarios that do, so it waits on a scenario carrying its bots and on
`-check` holding a bot to its scenario.

### Train a network bot live in `cave`

- Priority: P2
- Affected files: `internal/bot`, `internal/app/script.go`, `internal/paths`,
  `lixenwraith/genetic`
- Guide: [Bots](bots.md#networks-and-learning),
  [AI §3](ai-physics-and-evolution.md#3-genetic-library)

A solo watched or headless bot playing the `cave` scenario's own bot learns its
networks' weights a round at a time and keeps a population for each in the user
root ([Bots](bots.md#networks-and-learning)). `cave` is the one scenario
worked on until a bot plays it near perfectly: armada's wants rework first, and
in main a bot that types and jumps to nuggets lives for ever. Decided, and
built so:

- **The game is `cave` as shipped, raiders on**, starting with 10 heat. Its
  ramp, boss and faster caves are the curriculum, and a defeat resets the game,
  which ends the round.
- **The genome is a network's weights.** `cave` carries a model learned from
  `seed`'s, the start written by hand, and a population learns on from what its
  graph writes; one whose graph writes none starts from genomes drawn at
  random, as every run measured below as from nothing did.
- **A network a phase.** `raid` steers while the raiders fly and `kraken` while
  the Kraken is up, picked by a guard on the world, each with a learner and a
  population of its own. Both play a candidate, each scored by the ticks its
  network steered: `kraken` from the Kraken's rise to its death or the round's
  end, `raid` all the rest.
- **A round scores how far it travelled**, the columns its cave scrolled, and
  what it earned, never its time: a clock that closes a round could be
  outlasted, as by hopping about a Kraken barely scratched. The scroll halts
  for the Kraken, whose kill pays by its pace, and `round_seconds` only ends
  the game.
- **The special attack is the bot's to ask for**: the run starts on auto-fire
  `main`, and a network's `special` asks while it is positive.
- **The inputs are what a player reads**: the rock, all three raiders, every
  landing and how soon it lands, the Kraken, energy and heat. A raider's size
  and the cave's number steer nothing a person steers by.
- **A member ranks by what its rounds prove**: its mean weighed with eight
  rounds at the population's recent mean, the eight-round proved, eight at
  most, above the 24 on probation. It enters a full archive on a second round,
  in the place of the last on probation and of no proved member. Fielded is
  the first of the proved, and a third of the rounds go by turns to proving the
  most trusted guess and to testing the fielded against its rival.
- **A game on every core.** A headless run alone plays `-games` games at once,
  each its own instance on a seed drawn from the run's, all feeding one learner
  a network and its one file, their rounds closing by tick and then by game.
- **The learner lives with the driver**, outside the world and its captures,
  its store a JSON document of the bot package's own.

What was measured, at 120x40 headless and flat out, and still holds. A core
plays 4,700 to 7,000 ticks a second, by the machine, whatever steers. A round
is mostly luck, and it snowballs: a ship that outlives the first cave has its
gun raised and 50,000 energy, so a round's score runs from 150 to 10,000 before
anything is learned, and weights drawn at random have shot down three Krakens
within a population's first thousand rounds. An archive's own score for a
member runs well over what it holds on rounds it never played, as only members
whose luck held are still there, which is why a candidate enters on a second
round and eight prove a member. Auto-fire's special attack spent in a game's
first seconds the heat that is the ship's last buffer.

A written start is learned from, where for a time it was lost. Since `cave`
began from nothing a population's first 32 rounds drew every weight about its
start, the written one too: the archive filled with copies that flew worse and
the start left it. From the weights last written by hand, which field 12,170
with a median of 13,573 over 100 rounds of seeds 101 to 104, five minutes of
five games on seed 1, 8.5 and 9.9 million ticks, fielded held out:

```
                                  fielded   median   reach the Kraken
every weight drawn about them        4863     1018          34 of 100
starting from them alone            40865    32688           57 of 60
```

- Drawn about, no member of the archive was the written one, the oldest born
  at round 235, and what it fielded had drifted: 0.11 of bias down, four rows
  an update, and `species_near_x` at -0.09 for -0.25.
- Any run that learned from written weights between the two is a reading of
  the lost start, not of the learner.

A raider's kill pays twelve times its hit points, 96 in the first cave where
it paid 8, and populations start over under it (`populationVersion`). What a
graph's written weights field over the same 100 rounds of seeds 101 to 104
under each pay, with no learner; a round plays the same under both, its score
the old one plus eleven times the hit points it shot down:

```
                                    paid 8         paid 96      reach the
                                  mean  median   mean  median      Kraken
no weight, the ship holds still    152     146    159     146    0 of 100
species_y alone at 0.9             243     197    412     285           0
column, species_near_x, species_y 2213    3055   5889    8473          60
the hand weights, both networks  12170   13566  27614   33174          73
```

- A ship that only follows a raider's row fields 2.6 times a still one, where
  it fielded 1.6, and the hand weights 174 times, where 80.
- The row of three leaves `kraken` unwritten, so its ship holds still at the
  Kraken.

From nothing, what one game fields after three million ticks, seven minutes of
a core, over 40 rounds of four seeds it never played, each closed at the limit,
by the spread its first genomes were drawn with:

```
spread            0.03, as shipped         0.1
seed                 1       2          1       2
fielded, held out 2906    3654        974    1339
  its median      1030    1555        795     541
  met a Kraken       6      10          5       7
```

After 22 minutes on four cores, what each run from nothing fielded over 60
rounds of seeds 101 to 104, each closed at the limit, beside its learners' own
lines; on this score the weights last written by hand field 11,896 there, their
median 13,525, lose 16 rounds before the first Kraken, shoot five down in 1 and
reach the limit in none:

```
                                seed 1       2       3       4
five games, 24.9 to 25.3 million ticks
  raid: rounds                    8395    8855    5788    6595
    recent                        2480    4272    4327    4232
    fielding                      8677   11615   12787    9919
      over                          16       8       9      18
  kraken: rounds                  2306    2927    1811    1808
    recent                        6798    7037    7876    8536
    fielding                     14741   14142   15013   16869
      over                           8       8       9       8
  fielded, held out               3720    5464    6141    5891
    its median                    1277    1070    2320    1344
  lost before the Kraken            37      36      30      42
  five Krakens or more down          0       0       0       0
  rounds at the limit                0       0       0       0
one game, 6.2 to 6.4 million ticks
  raid: rounds                    2406    3230    1632    3051
    recent                        2196    3362    4326    2202
    fielding                      8012    7655   12378    9557
      over                          11       8       8      10
  kraken: rounds                   358     595     551     577
    recent                        5826    5480    5347    4889
    fielding                     16262   13443   15144   10631
      over                           8       8       8       8
  fielded, held out               3887    4556    4753    3159
    its median                    1105    1057    1382    1198
  lost before the Kraken            49      43      37      46
  five Krakens or more down          0       0       0       0
  rounds at the limit                0       0       0       0
```

- Five games play 19,000 ticks a second here and one game 4,700 to 4,800
  beside three others: four times the ticks, two to three and a half times the
  rounds.
- Every run loses half its rounds or more before the first Kraken, most to a
  defeat in the first cave's raid and the rest crushed by its rock. A ship
  that gets past it mostly shoots that Kraken down and falls to a later cave's,
  the second to the fifth. Five games lose fewer there on every seed, 30 to 42
  against 37 to 49, and field more on three.
- `fielding S over C` is 1.7 to 3 times what the member holds out. The member
  a five-game run fields changes a dozen times in 22 minutes, and in two runs
  of four one born in the first three minutes was fielded again in the last ten.

What each member does with the special in those rounds, and the same members
with `special` held off, its weights zeroed and its bias negative in a copy of
the files. A blast is a special that detonated and spent its heat:
`detonateDust` turns every dark glyph the player owns to dust anywhere on the
map, at no distance from the ship, and detonates up to 256 centres of radius
12. A blast clears the screen wherever the ship is, which is why it stands in
for flying.

```
                                    seed 1       2       3       4
five games
  blasts before the Kraken, median      10      10    12.5      10
  the first's tick, median             188     114     196     178
    the tenth's                       1019     513    1066     946
  rounds they left no heat              14      39      24      26
  heat as the Kraken rises, median      11      10    14.5      10
  special held off: fielded           1005     252    1475     840
    lost before the Kraken              60      60      60      60
one game
  blasts before the Kraken, median      10      10      10      10
  the first's tick, median             112     139     193     131
    the tenth's                        898     750     980    1014
  rounds they left no heat              29      18      21      18
  heat as the Kraken rises, median      13      10      10    11.5
  special held off: fielded            946     443     670     627
    lost before the Kraken              60      60      60      60
```

- Every member blasts in the first cave in 53 to 60 rounds of 60, 27 of the
  5,047 blasts there in a round's first 100 ticks and the tenth 26 to 53
  seconds in, where a round that reaches the Kraken meets it at a median tick
  of 1,100 to 2,900.
- Held off, what the runs field scores 252 to 1,475, where a ship that holds
  still scores 165, and loses all 60 rounds before the first Kraken, where the
  hand weights, which never ask for the special, lose 16: `raid` learned a
  trigger and almost no flying.
- None reached the limit, and a defeat ends at heat 0 by its rule, energy and
  heat both at nothing, so no lost round ends with heat left. In 14 to 39
  rounds of 60 the blasts spent every heat the round had, its 10 and its
  loot's, and in the other 21 to 46 touches took the last; a ship that reaches
  the Kraken has a median 10 to 15.

What the special follows, before the first Kraken in the same rounds: each ask,
an `EventFireSpecialRequest`, beside every 20th tick, over the eight members.
They ask 10 to 18 times a round there and blast 9 to 13, and the asks fill 4 to
11% of what an output always positive asks, one every 5 ticks:

```
                                at an ask   every 20th tick
raiders flying                    2.4-3.0           1.5-2.1
  none                                 0%             8-17%
the nearest, median cells           13-33             22-38
an enraged raider                 75-100%            29-52%
a landing marked                   69-95%            28-51%
heat                              5.2-7.6          7.3-10.7
  none                             10-31%             2-12%

heat                         0     1-3     4-6     7-9     10+
asked, of always on     10-28%   4-10%    2-8%   4-12%    2-5%
```

- The raid's danger gates the ask: none with no raider flying and almost none
  with one, 0 to 15% of always on with two, 5 to 35% with three, and 75 to
  100% of the asks come with a raider enraged or about to land. It answers the
  raiders crowding and charging, closer than usual but not by distance alone.
- Asking does not fall as the heat does: it is highest at heat 0, where an ask
  does nothing, 3 to 10 times its rate at 10 or more. Nothing in it holds heat
  back, and the heat a round keeps is what the raiders leave.

What five games fielded as they learned, their files as saved nearest 2, 5, 10
and 20 million ticks (1.8 to 2.3, 5.0 to 5.2, 9.8 to 10.3, 19.8 to 20.2), when
`raid` had scored 900 to 1,550, 1,560 to 2,760, 2,560 to 4,290 and 4,780 to
7,290 rounds, each held out over 40 rounds of the same seeds:

```
millions of ticks              2       5      10      20
fielded, held out
  seed 1                    1483    4030    2211    3240
  seed 2                    1995    1817    2949    3639
  seed 3                    4994    4539    4002    3884
  seed 4                    3371    4640    3552    6557
its median
  seed 1                    1102    1168    1001    1327
  seed 2                    1029    1043    1035    1007
  seed 3                    2027    2143    1962    1717
  seed 4                    1282    1701    1255    1702
lost before the Kraken, of 40
  seed 1                      30      24      26      26
  seed 2                      26      32      26      24
  seed 3                      25      21      32      30
  seed 4                      31      25      29      28
```

- The medians hold from two million ticks, two minutes, as do the rounds lost
  before the Kraken; the means move with a few long rounds. Every copy blasts
  about ten times before the Kraken and loses all 40 rounds there with the
  special held off.

Whether its selection is luck: every `raid` member of each five-game population
held out 24 rounds, 6 on each of seeds 101 to 104, the same games for every
member, beside the `kraken` member the run fields; and the curve's zero, a new
population stopped as its first 32 rounds end, one game on seeds 1 and 2, its
32 draws played the same way. Genes apart is the root mean square of two
members' difference, as a share of a weight's range:

```
                               seed 1      2      3      4  zero 1  zero 2
archive score against held out, rank correlation
  with the mean                  0.26  -0.17  -0.03   0.17    0.63    0.80
  with the median                0.31  -0.35   0.01   0.37    0.65    0.78
the member fielded, its rank       14     32     20      6       -       -
the halves against each other   -0.20   0.29  -0.23   0.28    0.88    0.93
held out, the members' median    3067   4471   4859   4473     144     149
  best                           6963   9044   7952   8655     342     231
  worst                          1470   2405   1864   1921      96      86
genes apart, mean, %              1.5    1.4    1.3    1.3     2.1     2.1
  most                            2.4    2.1    2.0    2.0     2.3     2.3
blasts before the Kraken          11     12     11     10       4      10
```

- A learned archive is 32 near-copies that 24 rounds cannot tell apart, their
  halves correlating -0.23 to 0.29, and its own score ranks them no better:
  the member fielded held out 6th to 32nd of 32. Over the members with eight
  rounds or more the correlations move by 0.07 at most. Choosing among them is
  luck.
- Random draws are told apart, by 24 rounds and by their one round alike: they
  field about a still ship, 144 and 149, lose every round in the first cave and
  already blast. From there the search reached a trigger fielding 1,500 to
  5,000 by two million ticks, and no further.

`raid` learned from nothing with `special` held off, learning and held out
alike, as run C: a switch in a scratch copy of the tree has `raid`'s `steer`
never ask, `kraken` as shipped, five games on four cores for 22 minutes on
seeds 1 to 4 as above. Off, the copy keeps main's population bytes; on, it
plays row 2's members as their files' ablation does, round for round. Held out
under it, 60 rounds at the end and 40 a copy near 2, 5, 10 and 20 million
ticks:

```
                                seed 1       2       3       4
run C, 31.6 to 33.7 million ticks
  raid: rounds                   18857   12147   15372   12735
    recent                        1080    1802    1498    1751
    fielding                      2487    5086    3617    6237
      over                           8      11       8      13
  kraken: rounds                     0      64      41      41
    recent                           -    7805    3259    2941
    fielding                         -   20790       0       0
      over                           -       8       0       0
  fielded, held out               1355    1940    2370    1762
    its median                    1249    1204    1644    1328
  lost before the Kraken            60      59      60      60
    the median tick they end      1254    1198    1643    1333
  five Krakens or more down          0       0       0       0
  rounds at the limit                0       0       0       0
```

```
                                     2       5      10      20
copies, fielded
  seed 1                           871     947    1104    1161
  seed 2                           841    1850    1811    1692
  seed 3                           953     953    1387    1439
  seed 4                           742    1020    1518    2047
its median
  seed 1                           672     764     799    1181
  seed 2                           721    1486    1416    1494
  seed 3                           607     607     986    1028
  seed 4                           763     831    1110    1185
the median tick they end
  seed 1                           677     769     804    1186
  seed 2                           726    1483    1421    1499
  seed 3                           612     612     991    1033
  seed 4                           768     836    1115    1190
```

Beside the other players of those rounds, with the median tick a round lost
before the Kraken ended at:

```
                            fielded   median     lost  at tick
the hand weights              11896    13525       16      666
row 2 held off, seed 1         1005      729       60      734
row 2 held off, seed 2          252      248       60      253
row 2 held off, seed 3         1475     1300       60     1305
row 2 held off, seed 4          840      652       60      657
run C, seed 1                  1355     1249       60     1254
run C, seed 2                  1940     1204       59     1198
run C, seed 3                  2370     1644       60     1643
run C, seed 4                  1762     1328       60     1333
```

- Without blasts five games play 23,900 to 25,500 ticks a second, and a
  learning round reached the Kraken 0 to 64 times a run, so `kraken` learned
  little.
- Every copy loses all 40 rounds before the Kraken and what each run fields 59
  or 60 of 60, as row 2's members held off do, so the reading fixed before the
  runs that has the search fly does not hold. Nor is it flat: what it fields
  grows 1.6 to 2.5 times from two million ticks to the end, its lost rounds
  lasting from a median tick of 612 to 768 to one of 1,198 to 1,643, past row
  2's members held off on every seed. It finds some flying on a clean signal,
  slowly, and none that passes the cave the hand weights pass in 44 rounds of
  60.

The same reading of selection on run C's populations, under the switch:

```
                                seed 1      2      3      4
archive score against held out, rank correlation
  with the mean                   0.09   0.42  -0.06   0.10
  with the median                 0.19   0.21  -0.09   0.42
the member fielded, its rank        17     15      1     24
the halves against each other     0.04   0.29   0.09   0.36
held out, the members' median     1362   1997   1691   1635
  best                            1690   3699   2845   2510
  worst                            915   1078   1076    946
genes apart, mean, %               1.4    1.5    1.3    1.1
  most                             1.9    2.2    1.9    1.9
```

- Held to flying the archive is the same: near-copies 1.1 to 1.5% of a
  weight's range apart, halves correlating 0.04 to 0.36, its score ranking
  them -0.06 to 0.42 against the held-out mean, within 0.12 over the members
  with eight rounds or more, and the member fielded 1st to 24th of 32. No
  round snowballs here, none passing the first cave, and still a round's luck
  outweighs what sets the members apart: the stall is selection's, with the
  special or without it.

What the runs earn before the first Kraken: the same held-out rounds again,
with the raiders the ship shot down. The cave's gun fires one shot forward at
its first level, so a kill takes the raider's row where a dodge leaves it; a
kill pays 8 where a tick pays 1, and the twelfth brings the Kraken:

```
                                seed 1      2      3      4
kills, median
  row 2, as fielded                 14     13   17.5     14
  row 2, held off                    0      0      0      0
  run C                              0      0      0      0
rounds with none, %
  row 2, as fielded                 13      0      0      0
  row 2, held off                   90     95     88     90
  run C                             93     77     62     75
  run C's copy at 2 million         95     90     90    100
    5                               98     82     90     92
    10                              98     82     68     90
    20                             100     78     75     85
```

- The hand weights kill a median 21 there and none in no round. Held off,
  row 2's members kill almost nothing, and so does run C at every copy: every
  such row scores its median tick less 5, earning nothing and paid only for
  lasting. Row 2 as fielded kills by its blasts.

What the same games for a candidate and its parent would buy, from reading B's
rounds: of a round's variance among a population's members, the part its game
explains, the part its member does and what is left, and the correlation of
two members' scores over the same games. Round i of a seed is one game for
every member, its streams drawn from the root seed and the game's number:

```
                                seed 1      2      3      4
row 2
  the game, %                        4      9      9     10
  the member, %                      2      1      0      2
  left, %                           94     89     91     89
  two members' correlation        0.05   0.10   0.09   0.11
run C
  the game, %                       14      3      7     10
  the member, %                      0      2      2      1
  left, %                           86     96     91     88
  two members' correlation        0.15   0.07   0.08   0.15
```

- Two members on one game part as soon as they differ, and what follows
  decides the round: a paired comparison keeps 86 to 97% of an unpaired one's
  variance, so the same games would buy a tenth at most. A member is 0 to 2%
  of a round, so its mean reaches half reliability after 50 to 75 rounds,
  paired or not, and in two populations of eight never, where a learner proves
  one on eight. Random draws are 36 and 40% member, told apart in two rounds.

Run F, a probe and no proposal: run C again with a raider's kill worth 100 to
the learner in place of its hit points, a constant the scratch copy reads from
the environment, each seed stopped at run C's ticks by a stop region and its
copies taken by ticks as before. Held out on the shipped score, a kill worth its
hit points again, with `raid`'s special held off:

```
                                seed 1       2       3       4
run F, 31.6 to 33.7 million ticks
  raid: rounds                   13797   13147   16854    4969
    recent                        1561    1769    1091    4072
    fielding                      3857    4280    2989   15504
      over                          10       8      10      12
  kraken: rounds                    12       0      47     385
  fielded, held out               2043    1852    1483    7202
    its median                    1403    1298     820    2699
  lost before the Kraken            60      60      60      49
    the median tick they end      1408    1303     825    2131
  kills before it, mean            1.2     0.1     1.3     7.6
    rounds with none, %             72      92      73      38
  rounds at the limit                0       0       0       1
run C, the same seeds
  fielded, held out               1355    1940    2370    1762
  lost before the Kraken            60      59      60      60
  kills before it, mean            0.1     0.8     1.9     0.8
    rounds with none, %             93      77      62      75
```

```
millions of ticks              2       5      10      20
copies, fielded
  seed 1                     910    1217    1392    1457
  seed 2                     854     944    1315    1845
  seed 3                     893    1054    1000    1001
  seed 4                    3538    4647    5993    6806
lost before the Kraken, of 40
  seeds 1 to 3                40      40      40      40
  seed 4                      39      37      32      35
rounds with no kill, %
  seed 1                     100      93      98      78
  seed 2                     100     100      95      90
  seed 3                      95      83      85      88
  seed 4                      45      38      40      30
```

- On seed 4 the first reading fixed before the run holds: paid for kills its
  members take the gun from two million ticks on, and what it fields kills a
  median 3 before the Kraken, 21 to 23 in each of the 11 rounds of 60 that
  reach it, one of them lasting to the limit, where run C on that seed passed
  none. On seeds 1 to 3, F plays as C did.
- So the first cave's pay holds the search off the gun: paid 100 a kill one
  run in four took it within run C's ticks, and none of run C's four did.

Run H, stopped with the fix: run F with `raid` reading `ceiling`, `floor`,
`species_x`, `species_y`, `species_near_x` and `column` through no hidden unit,
21 weights, and `kraken` as shipped. Seeds 1 and 2 ran to run C's ticks and
were held out as F was; seed 3 stopped at 32.2 of its 33.7 million ticks, not
held out, and seed 4 did not run:

```
                                seed 1       2
fielded, held out
  run H                          10901   11292
  run F                           2043    1852
kills before the Kraken, median
  run H                              8       4
  run F                              0       0
rounds that meet the Kraken, of 60
  run H                             13      13
  run F                              0       0
raid in the outer tenths, %
  run H                             97      96
```

- Where F took no gun on those seeds, H took it on both. The reading fixed
  before the run wants three seeds of four, so two leave it undecided, and
  its ship still keeps to the rock's edges, as a run from nothing does
  ([above](#have-a-scenario-describe-the-bots-that-play-it)).

Runs I and J did not run: from nothing with a kill paid 100 to the learner
and `special` free, J with x and y naming a place in the view. Nor did the
re-read of the outer tenths on 60 rounds, but for the hand weights: 7% of the
raid there, as on 20 to 40 rounds.

Run G learned from the hand weights before the fix, so it read the lost start
(above) and not the learner, and is cited nowhere. Run K is that run on main
after the fix: the graph of 901ac22 less the `ticks` main no longer reads,
`learn` on, `special` free in both networks, a kill paid its hit points as it
was then, five games stopped at run C's ticks and copies taken by ticks as
before. With no weight written the fix keeps the population bytes of the main
before it, five games and two million ticks on seeds 1 and 2, and main, with
the bot document since, keeps the fix's on seed 1, from nothing and from that
graph. Held out beside the hand weights unlearned, which field 11,896 over the
60 rounds as before, its standard error 1,116 and its median 13,525, lose 16
rounds before the first Kraken and kill a median 21 before it. The same rounds
paid 96 a kill, which play the same, close the table, where the hand weights
field 27,154 with a median of 33,047:

```
                                seed 1       2       3       4
run K, 31.6 to 33.7 million ticks
  raid: rounds                    1138    1073    1896    1252
    recent                       32115  111568  105662   48693
    fielding                     62763  332141  156782  147566
      over                           8       8       8       8
  kraken: rounds                   989     889    1668     992
    recent                       34710   99547   82905   47895
    fielding                     61570  310932  330100  119537
      over                           8       8       8       8
  fielded, held out              53113  260594  223843   86438
    its median                   57614  332000  332047   97782
  lost before the Kraken             5       8       8       7
  kills before it, median           20      19      20      19
  Krakens down a round             5.4     7.6     7.2     6.1
  five or more down                 54      52      51      53
  rounds at the limit                3      50      44      12
  special held off: fielded      23032  193953  157984    9093
    its median                   33759  332974   55574     555
    lost before the Kraken          21      22      25      51
  paid 96 a kill: fielded        84633  296637  259003  118570
    its median                   91959  373684  373570  134122
```

```
millions of ticks              2       5      10      20
copies, fielded
  seed 1                   23320   40206   51676   50291
  seed 2                   12536   44264   69935  148232
  seed 3                   29639   25995   29185   66903
  seed 4                   14261   46517   36047   50554
lost before the Kraken, of 40
  seed 1                      13       3       1       2
  seed 2                      10       8       2      11
  seed 3                       1       8       3       8
  seed 4                       3       5      16      14
```

- The learner climbs from the hand weights on every seed: what K fields is
  over theirs by 12 to 15 standard errors of the difference under either pay,
  its median 4 to 25 times theirs paid 8 and 3 to 11 times paid 96, and it
  loses 5 to 8 rounds before the first Kraken where they lose 16.
- With `special` held off in both networks the same members lose 21 to 51
  rounds there, more than the hand weights' 16, and down fewer Krakens than
  with it: K passes the first cave more often than they do by the blast. Past
  it, seeds 1 to 3 last to the limit without one in 35 to 38 rounds of 60.

What K's members do with the special, as reading A:

```
                                      seed 1       2       3       4
before the first Kraken
  asks a round                           5.9     6.9    16.5    11.0
  blasts a round, median                   6       7     6.5      10
  a raider within 15 cells, %
    at its asks                           12      17      10      32
    at any tick                            4       4       5       5
  asked, of always on, at heat 0, %       12       -      57       5
    at heat 10 or more, %                  1       2       1       3
heat as the Kraken rises, median          15      14    16.5      11
rounds that spent all their heat          42       0      44      39
```

- It blasts 6 to 10 times before the first Kraken, where a person blasts once
  or twice, asked as raiders crowd the ship, seed 1's also as its energy runs
  low, a median 14,594 at its asks against 31,547. It meets the Kraken with 11
  to 16.5 heat where the hand weights, which never ask, keep 20. Seed 3 asks
  most at heat 0, where an ask does nothing; seed 2 never runs out.

The rest was read while the graph wrote its weights by hand and a round scored
its ticks, a Kraken's fight aside. Games that learn together, in ticks a second
on a four-core machine by the cores they were given, where one game plays
6,900:

```
cores   games   ticks a second
2       2       12600
2       3       13500
2       4       12400
3       3       18700
3       4       18300
3       5       19800
4       4       21400 to 22500
4       5       27000 to 27400
4       6       26300 to 26500
4       8       27000
```

- Four games on four cores waited at 222 of 302 round closes, 20 of their 187
  game-seconds, where the same games closing in any order play 27,000; the
  learners' lock was held 0.06 s of them.
- Two runs of one seed and five games keep the same files, 87,142 bytes after
  two million ticks, and one game keeps what a lone learner does, byte for byte.

What a run fielded after 22 minutes on four cores, over 60 rounds of four seeds
it never played, 101 to 104, each closed at the limit, where the weights written
by hand scored 10,033, lost 16 rounds before the first Kraken and shot five
Krakens down in 1. One game is nine million ticks of a core, four such runs
side by side, and five games 35 million on the four:

```
seed                              1       2        3       4
one game
  rounds learned on            1237    1358     1539    1493
  fielded, held out           37621   16003    12617   13876
  lost before the Kraken         11       4        5       6
  five Krakens or more down      49      12        1       3
  rounds at the limit            38       0        0       0
five games
  rounds learned on            5057    3326     2703    3609
  fielded, held out           35177   65154   201735   60530
  lost before the Kraken          6      11       13       6
  five Krakens or more down      33      47       47      54
  rounds at the limit             5      12       47      46
```

- The four average 20,029 with one game and 90,649 with five. Seed 1 is level:
  60 rounds place a mean within 2,400 to 3,500 there.
- Five games play four times one game's ticks and two to four times its
  rounds, 8,000 to 12,600 an hour for 3,300 to 4,100, as their members last
  longer; a new population's first two million ticks ran 20,900 an hour.
- A Kraken's pay doubles a cave with its hit points: seed 4's rounds at the
  limit were worth 48,000 and seed 3's 320,000, on six and eight Krakens.
- What they lost was still the first cave, as often as the weights they began
  from.

What remains:

- **Whether it finds the heat.** A person gets through the first cave, where
  the gun has no upgrade and a touch finds no shield, by blasting the raiders
  that crowd the ship, once or twice, and holding a heat back. From nothing a
  run learns the special as a trigger in place of flying: it asks when raiders
  crowd the ship or charge, about ten blasts in the first cave, and most at
  heat 0, where an ask does nothing, so nothing in it holds heat back (above).
  From the hand weights, which fly that cave, the learner adds 6 to 10 blasts
  before the first Kraken, asked as raiders crowd the ship, and leans on them:
  held off, its members lose more rounds there than the hand weights. It meets
  the Kraken with 11 to 16.5 heat of their 20 (run K).
- **How fast it learns.** From nothing, what five games field after two
  million ticks, two minutes on four cores, is what they field after 25, and
  one game fields after 22 minutes, six million ticks, about what five games
  did at five million: a game on every core multiplies the ticks four times and
  gets to that level sooner, no further. A game whose round closes ahead of the
  others' ticks waits for them, which is what makes a seed's search one search,
  and a game more than the cores takes up the core it frees: that count is read
  on two to four cores, and three did better with two more, so read it on more,
  where the learners' one lock may bind first. With
  [Train bot graphs offline](#train-bot-graphs-offline).
- **A scenario that opens on the Kraken** scores `kraken` every round and
  fielded nothing better on four seeds, so none is built. A ship arrives there
  with 43,000 to 69,000 energy (the quartiles of 44 fights, the median 50,000),
  20 heat and the gun seven levels up; a copy of `cave` starting so, its ramp
  skipped, meets the Kraken at tick 134, and the weights then written by hand
  won 15 first fights of 16 there, as in the game. `kraken` learning alone for
  nine million ticks of five games, then held out in `cave` beside those `raid`
  weights:

  ```
  seed                          1       2       3       4
  learned in cave           27383   34211   23074   51120
    its rounds scored        1021     883    1064     698
  learned in the copy       20666   21433   32686   50244
    its rounds scored        1393    1477    1227    1018
  ```

  The copy's first fight starts from one state where `cave`'s vary; a learner
  fed by both scenarios is not tried.
- **What a round can still be paid for.** The first cave scrolls for as long as
  its tier is not shot down, so a ship that kills no twelfth raider travels on
  at the slowest pace: watch for a population that settles there. Whether
  what a run fields hops about the Kraken or keeps the gun on it is not read.
  And two five-game runs of four fielded members that reached the limit three
  rounds in four: decide whether it grows as they do.
- **The search.** Of the settings read so far only the rate is told from the
  shipped one, and that from weights written by hand. Twelve seeds a row, as
  the library read its own, each five million ticks of five games and what it
  fielded held out for 40 rounds, from those weights, which scored 10,033 and
  2.2 Krakens a round:

  ```
                            fielded  geometric  Krakens     lost   five   at the
                               mean       mean  a round    early   down    limit
  as shipped                  29241      23065  3.73±0.33   0.20   0.45     0.24
  ranked by the mean log      23939      21040  3.44±0.26   0.25   0.42     0.24
  a repeat every second       26917      23147  3.91±0.34   0.17   0.49     0.17
  a repeat every fifth        24300      21704  3.70±0.27   0.17   0.45     0.17
  a step floor of 0.06        24304      22411  3.58±0.27   0.23   0.48     0.18
  a step floor of 0.015       20613      18437  3.28±0.31   0.25   0.43     0.12
  a rate of 0.1               14690      13606  2.72±0.25   0.32   0.24     0.05
  a rate of 0.01              17100      15372  2.98±0.23   0.23   0.25     0.09
  ```

  - Only the rate is told from the shipped 0.03. At 0.1 a run fields 0.6 times
    as much and a Kraken a round less, 2.4 standard errors, its candidates
    falling early enough to double its rounds to 1,806; at 0.01, 0.67 times,
    1.8 standard errors. The other rows lie within one.
  - The shipped row's seeds fielded 8,721 to 105,159, so twelve carry a
    geometric mean within a factor of 1.2 and a setting is read only once it
    moves what is fielded 1.7 times, or a Kraken a round. A row's runs learned
    on 800 to 1,010 rounds, a seventh of the ticks of the equal-time runs
    above.
  - Not read: a rank by the median; any row at the equal-time runs' 35 million
    ticks, where what a run fields reaches the limit; and any row from nothing,
    where only the spread a first genome is drawn with was tried, on two seeds
    (the table above).
  - Every figure above was read with the learners exploring in turn, a round
    each scored whole from where its network first steered. Both exploring at
    once, each scored by its own network's ticks, as built since: from `seed`'s
    weights four games' three million ticks, from nothing one game's 600,000,
    what each fields held out over 100 rounds of seeds 101 to 104, where
    `seed`'s own weights field 27,614 and a still ship 159:

    ```
                         rounds scored      fields            reach the  Krakens
    start      seed      raid   kraken     mean   median  Kraken of 100     down
    in turn
      seed's      1       325      276    53852    60184             87      419
                  2       372      325    51435    55947             81      410
                  3       202      147   113776   126384             72      518
      nothing     1       536       64     3029     2230             22        0
                  2       792       50     2614     2002             18        0
    by phase
      seed's      1       814      408    51440    46982             98      423
                  2       767      437    51679    52348             93      421
                  3       570      305   140138   127276             97      633
      nothing     1       864      125     6115     2726             30       35
                  2       774      115     6640     3340             28       39
    ```

    From `seed` the means are not told apart, a geometric mean of 68,051
    against 71,954, and the rounds lost in the first cave fall from 20 in 100
    to 4. From nothing what is fielded doubles, and `kraken`, which in turn had
    no member eight rounds proved, fields one.
  - Not built, from a read of the learner. A step at the floor
    moves a weight 0.24, a normal draw of 0.03 of its range of 8. And from a
    written start a hidden unit does nothing until a weight into it and one
    out of it have both moved, each alone a child that plays as its parent:
    258 of `raid`'s 378 weights in `seed` wait on drift.
- **A page's store.** A browser keeps no population: `bot.Store` is a document's
  bytes, so one kept in the page's storage as its settings are
  (`browser.KeepSettings`), for each graph, scenario and network, handed over at
  start.
- **Sessions.** A run in a session fields and does not learn. The first widening
  is a session whose only participant is the bot, the fleet's standing one
  ([Keep a standing session for the lobby](#keep-a-standing-session-for-the-lobby)):
  `bot.Learn`'s `alone` is the seam, and its stores need one writer.
- **The operator's side.** A watched run's viewer may still turn auto-fire or
  the pointer off under the bot, which its rounds then score; the change is
  saved as theirs, as any `[play]` change is. Two runs learning one graph in
  one scenario keep the file of the one further on; what the other learned is
  not merged into it.
- **Reading rounds.** `script/test.sh botfitness` sums fitness over a fixed time
  on the embedded scenario and `td`; rounds are read by a scratch test
  ([agent notes](agent-notes.md#measuring)). A code species' kill still weighs
  5, the Kraken's aside: its hit points want a table.
- **The archive stalls from nothing** in the first cave, with the special and
  without it (above). Its members become near-copies, each 0 to 2% of a
  round's variance, that 24 rounds do not tell apart, and its own score picks
  among them by luck. The same games for a candidate and its parent would buy
  little: two members on one game part as soon as they differ, and pairing
  keeps 86 to 97% of a comparison's variance. The first cave pays a tick where
  a kill pays 8, so held to flying the search lasts and kills nothing; paid
  100 a kill, one run in four took the gun and passed the cave in 11 rounds of
  60 (run F). From the hand weights, after the fix, it climbs on every seed
  (run K). To decide: what a kill pays in the first cave, 100 being a probe
  and no proposal, and whether a candidate is judged on the 50 to 75 rounds
  that tell near-copies apart, or by an evolution strategy with a step per
  gene, an engine of its own in `lixenwraith/genetic`, which averages many
  rounds into one step.

What `lixenwraith/genetic` measured on a stand-in, a 148-weight network of this
shape on a noisy scrolling game, twelve seeds a row and 12,000 rounds each, read
as the fielded member's mean over held-out rounds
([its notes](https://github.com/lixenwraith/genetic/blob/main/doc/agent-notes.md)):

```
no repeats, one round an evaluation            3070
no repeats, the mean of three                  3360
every third a Reevaluate, merged, one round    3545
the same on the mean of three                  3570
separable CMA-ES on the mean of three          3290
```

- A step of 0.1 of a weight's range left one seed in twelve at the hand-written
  genome for 4,000 evaluations; 0.02 to 0.05 left none.
- `AdaptiveStrength` falls to its `lo` within a few hundred noisy evaluations, so
  `lo` is the step it runs at: 0.02 ended level with the best fixed step and 0.005
  stalled.

### Draw a watched run's wait on its screen

- Priority: P3
- Affected files: `internal/app/presentation_terminal.go` (`showStartupStatus`),
  `internal/app/play.go` (`player.frame`)
- Guide: [Runtime §1.1](runtime.md#11-script-pacing-and-presentation)

A watched script or bot that hosts or joins waits in its lobby drawn at its run's
size, 120x40 unless `-size` names another, in the screen's top left corner: the
wait's frame is a played run's (`App.frame`), laid out for the simulated screen,
where each frame after it is laid out for the viewer's (`player.frame`). Lay the
wait out as `player.frame` does, the view centred and the bar on the screen's
last rows.

### Train bot graphs offline

- Priority: P3
- Affected files: `internal/bot`, `lixenwraith/genetic`,
  `internal/system/nugget.go`
- Prerequisite: [Train a network bot live in `cave`](#train-a-network-bot-live-in-cave)
- Guide: [Bots](bots.md#fitness-and-comparing-graphs)

The genome is a live learner's network, and the result its population file,
`bot/<graph>.<scenario>.<network>.json`. A solo `-headless` run already
learns with a game on every core, each round a new game of its seed.
`lixenwraith/genetic`'s `Engine` evaluates whole headless runs over a matrix of
scenarios and seeds, fixed for the run since the batch engine never scores an
elite again, and `lixenwraith/genetic/fitness` scalarises the result. A solo run
depends only on its seed, so evaluation needs no mesh. Trained values ship as the
weights of a scenario's bot
([above](#have-a-scenario-describe-the-bots-that-play-it)). Decide whether a
graph's own constants become genes too: a `[bot.params]` table, or bounded macro
parameters shared with the editor's macro metadata. Bot
genomes never enter the in-game genetic registry, which captures carry. Fitness
weighs no nugget, since nothing pushes `EventNuggetCollected`: push it in
`NuggetSystem.collectNugget` and drop that function's own `statCollected`
increment, which the event's handler repeats.

### Teach a bot its player's style, for an auto-pilot

- Priority: P3, an aspiration for the last stages of the bot work
- Prerequisite: [Train a network bot live in `cave`](#train-a-network-bot-live-in-cave)
- Guide: [Bots](bots.md#networks-and-learning)

Wanted (Lixen, 2026-11-18), and not to be built yet: a bot that learns to play
as the person it plays beside does, and an auto-pilot a player turns on to have
that bot fly their ship until they take it back, as when a session cannot pause
and they must leave the keys for a few minutes.

- **A different learner.** Not the search over a population a solo run does:
  a slow, streaming one that reads the player's own play, what they saw and
  where they moved, and updates one set of weights live, at each death, in the
  game the bot and the player share. What it is, a network pulled toward the
  player's moves or something else, is to be defined.
- **Before it**, the game's own changes. A bot's death no longer starts the
  game over beside a person ([Multiplayer §5.4](multi-player.md#54-wraiths)):
  it comes back a second later, whole as a new game makes it.
- **To decide then**: what of a player is read and where it is kept, since a
  style learned over weeks outlives a session; what the auto-pilot does with
  the special attack, a typed scenario and a player's settings; and how the
  hand back is told to the session's others, if at all.
- An older sketch here was a model of each other participant for one session,
  with a bandit over trained genomes: it kept nothing.

## Authoring

The map editor leads this section. Its foundation is the P1 items, in the order
they stand; the editor follows at P2, on a terminal and in the desktop window, with
[armada's terrain](#scroll-a-terrain-past-armada) its first scenario.
The rest wait.

### Settle what a bot's defeat leaves standing

- Priority: P3
- Affected files: `internal/system/meta.go`, `internal/engine/world.go`,
  `internal/app/internal/membership/session.go`, `internal/status`
- Guide: [Multiplayer §5.4](multi-player.md#54-wraiths)

A bot's defeat ends a game only where every cursor in play is a bot's
(`World.DefeatDecides`), and beside a person it comes back a second later. What
that leaves:

- A dedicated host sizes its map by the first joiner that names no holder, a
  run a bot plays itself among them (`noteJoinerReport`'s `firstPerson`).
- No status key says which slots are bots', so a scenario's guard cannot ask.

### Hold every scenario to the shared-region rule at load

- Priority: P1
- Affected files: `internal/resource/check.go` (`ValidateScenario`),
  `internal/app/domain_test.go` (`scenarioTrees`),
  `wad/scenario/base/tutorial/scenario.toml`
- Guide: [D-20](domain-design.md#d-20--shared-fsm-regions-move-on-the-authoritys-transitions)

A region a local event moves advances on the instance that produced the event and
nowhere else. `TestFSMTriggersAreReplicated` holds the shipped scenarios to that
through a list kept by hand (`scenarioTrees`), which leaves out `armada`, which
passes, and `tutorial`, which fails: its lessons step on a player's edit requests,
typed glyphs and mode changes and count `EventEnergyGlyphConsumed`. A scenario an
author writes is never read by it: it loads, plays alone and comes apart in a
session.

What closes it: `ValidateScenario` refuses a transition a local event triggers in
a scenario seating more than one player, naming the state and the trigger, so
`-check`, a start, `:n` and the editor hold every scenario alike; the tutorial
takes `max_players = 1`; and `scenarioTrees`' list goes with the rule's move.

### Describe a scenario document in the schema

- Priority: P1
- Affected files: `internal/manifest/schema.go`, `internal/event` (the registry
  generator), `internal/fsm/config.go`, `internal/resource/scenario.go`
  (`entryKeys`), `internal/behavior/program.go`
- Guide: [Behavior machines §5](machine-design.md#5-dialect-kernel)

The editor's forms and pickers come from one description, the one `-schema`
prints, and it holds too little for them: 209 events with their fields, the names
of 38 enums, three vocabularies with their arguments and what a bot's network
reads and asks for. Missing:

- **An event's class and line.** Nothing says an event is local, shared or a
  crossing, or what it does, though its type's comment holds both, so nothing
  tells a picker which a scenario may emit or trigger on.
- **A field's shape.** A type is its Go spelling: a nested one
  (`[]event.MazeRoomSpec`, `component.WallVisualConfig`, `color.RGB`) is a name
  with no fields, a `time.Duration` hides that it takes nanoseconds, and nothing
  marks the pair that is a cell, the entity a capture fills, or the string naming
  one of the scenario's own (a program, a loot kind).
- **The document's shape.** The keys of the entry (`entryKeys` and the hand
  readers of `[map]`, `[content]` and `[navigation]`), of a region, a state, a
  transition and an action's modifiers, of a program and its `[prototype]`, and
  of a bot document's `[bot]` and `[network.<name>]`, each with its kind, as
  `dialect.Fields` gives an argument's.
- **The systems.** The names a scenario toggles and what each requires, which
  `-check` holds a scenario to and nothing lists.
- **The status rows.** What `StatusIntCompare` and `StatusToVar` may read lives in
  a built world's registry, some rows the scenario's own
  (`behavior.<program>.count`, `fsm.<region>.*`): `-schema -s <name>` would print
  them from a world built for that scenario and never run.

Each is a field more in the JSON under one `SchemaVersion`. The editor reads the
same description in the binary, never a table of its own.

### Name a load fault by its file and key

- Priority: P1
- Affected files: `internal/resource/check.go`, `internal/fsm/loader.go`,
  `file_loader.go`, `internal/behavior/compile.go`, `loot.go`
- Guide: [FSM §12](fsm-and-configuration.md#12-validation-rules-and-diagnostics)

A fault is a line of text naming its place in its stage's own words (`state 'A':
unknown key "on_entr"`, `program 'scarab': state 'Lock': on_update[0]: …`), joined
with its stage's others, and a state's fault names no file. `ValidateScenario`
stops at the first stage that fails, of the programs, the loot, the machine and
the systems, so an author clears one to meet the next. The editor has to land each
fault on the node or the cell that made it.

What closes it: one fault type, a file, a key path and a message, that every stage
returns and `-check` prints as it prints text today, and a validation that goes
on to every stage a failed one does not starve. A path counts an array's elements
as expanded, so an action past an `each` maps back to the element written by
expanding each written element alone, which asks nothing of the macro library.

### Lay a painted map a scenario carries

- Priority: P1
- Affected files: `internal/behavior/maps.go`, `internal/component/declared.go`
  (`MapLayout`), `internal/system/wall.go`, `internal/pattern`,
  `internal/resource/scenario.go` (the bounds)
- Guide: [FSM reference: Maps](fsm-reference.md#maps),
  [Content §8](content-assets-and-tools.md#8-image-and-pattern-pipeline)

A scenario carries maps: `[maps]` in its entry, each inline or a file of its own,
read, bounded, digested and sent as a program is and compiled once a run; `rows`
of one width over a `legend`; and `EventMapLayRequest { map, x, y }`, which a
request naming no map of the scenario fails the load on. A legend lays a
program's spawn and nothing else, so a formation is drawn and a level is not. A
wall event still takes its cells one by one, or lays a maze, a cave or an `.aimg`
from the `image/` category, which the digest leaves out and a joiner is not sent.
What remains, tiles first and [images after](#carry-an-image-in-the-scenario):

- **A tile.** A legend entry that is a wall: its glyph, colours, box style and
  block mask named once. `MapLayout` gains its cells beside `Spawns`, and the wall
  system lays them on the lay request as it lays a pattern, as one composite or as
  loose walls, a batch a block mask and box style. A 500 by 250 map is 125 KB of
  rows, half a file's bound.
- **Its own size.** A map is as large as its rows, and the scenario holds the map
  to it (`[map] fixed`, its level setup). A layout that stretches with the
  terminal is not this item's.
- **A loot, and a named cell** a payload takes, the marker the editor drags, each
  a kind of legend entry its own system lays.

Laying a large map costs what td's maze does at a reset, tens of milliseconds
([Engine](#trim-what-a-tick-spends-outside-the-position-index)).

### Write a scenario back as `vif.toml` is written

- Priority: P1
- Affected files: `internal/resource` (a scenario store beside `SaveDownload`),
  `internal/paths` (`OwnRoot`, `Shared`), `internal/app/internal/membership`
  (`RestartAsk`)
- Guide: [Filesystem §2](filesystem-layout.md#2-installed-configuration-tree),
  [§3](filesystem-layout.md#3-resolution-policy)

An edit goes into the author's file through `toml.Document`, as a setting goes
into `vif.toml` (`saveTable`), and the library writes only what then reads
otherwise: the editor changes the tree it read and sets it. Run against the
shipped scenarios, one action changed in the 40 lines of td's `TdSetup.on_enter`
changes that line alone, its 10 comments kept; an argument of an invocation is
set in place among the others; a state under a header has its keys set, added
and deleted; and a raider's `[[frames]]` keeps every table that reads the same.
What it leaves out is a name for one table of an array of tables: the array is
set. The rest is vif's:

- **Whose file.** A scenario in one of the player's roots is written in place
  (`paths.OwnRoot`). One in a shared root, the checkout's `wad/` or a system
  root, or among the downloads, is copied under a name of the author's into the
  user root's `scenario/` at the first change, as `SettingsFor` seeds a `vif.toml`.
- **Nothing the author did not say.** A store writes the edit and adds no
  default: a key left out stays out, and a scenario's `[keymap]` is never filled
  as a player's keymap is, only held to what this build reads.
- **Never stored broken.** What is stored validates, as `saveTable` reads its
  document again, and a save over several files lands whole or not at all, as
  `SaveDownload`'s does. To decide: what becomes of edits that do not validate
  yet.
- **A test play before a save.** `Options.Provided` already runs a scenario held
  in memory, a joiner's, but a restart names one by name
  (`RestartAsk.Scenario`): the ask carries the documents as edited.

### Read a block's form from its macro

- Priority: P1
- Affected files: `internal/fsm` (beside `file_loader.go`),
  `internal/manifest/schema.go`
- Prerequisite: [Describe a scenario document in the schema](#describe-a-scenario-document-in-the-schema)
- Design: [TOML macros](toml-macros.md)

The palette is the scenario's macro set and a block's form its parameters. The
macro library defines, expands and folds and lists nothing of a set, and need
not: a definition is plain data in the parsed document, its `params`, `defaults`
and `doc`, and its `base` and `with`, whose open parameters a walk of the bases
gives. What a form needs beyond that is derived here:

- **A parameter's kind.** A `string` may name a state, a region, a variable, an
  event or a species, and two `int`s a cell. Expanding the macro once over marker
  arguments shows where each lands in the document's shape: in a `target` it
  names a state and takes a picker and an edge of the graph, in a payload's cell
  a marker on the map. A name spelled from an argument (`"${theme}Fg"`) cannot be
  probed and stays a plain field.
- **A fault's block.** A state written as an invocation keeps its name, so a
  state's fault is its block's
  ([Name a load fault](#name-a-load-fault-by-its-file-and-key)).
- **Blocks from plain tables.** `Fold` writes back as invocations what a set's
  macros expand to, so a scenario written out by hand takes a library's blocks.

The library's part is wanted, not needed to start. A table has no order and a
definition refuses a key it does not know, so a form lists its fields by name
under the macro's one `doc`: a parameter's place and a line of its own are two
keys of a definition (P2). A block of several states is the root `[[expand]]`
[TOML macros §7](toml-macros.md#7-rejected-alternatives) defers (P3).

### Keep a block library a scenario copies from

- Priority: P2
- Affected files: `wad/block/`, `internal/resource/resolve.go`, `internal/paths`,
  `Makefile` (`install-config`)
- Design: [TOML macros §5](toml-macros.md#5-where-macros-live)

No macro comes with the binary, since a scenario must expand alike on two builds,
so a block a scenario did not write is copied into it. Nothing holds blocks to
copy: td's, cave's and the campaign's macro files are their scenarios' own, and
each scenario writes the common shapes again. What closes it:

- **A category in the roots**, `block/`, each file a macro file, shipped in
  `wad/block/` and resolved as every resource is.
- **A copy on first use.** The macro, and those its body or its base names, go
  into a macro file of the scenario that `macro_files` lists. A set refuses a
  name defined twice, so a copy meeting one of the scenario's is refused or
  renamed.
- **Its first blocks**, from what the shipped scenarios repeat: the player's
  lifecycle seven of them copy (boot, its retry, the defeat watch on
  `session.any_defeated`, the strobe and the reset), td's `SpawnAwait` and
  `Announce`, cave's `RaiderStates`, armada's `Ship` and its wave loop.

A copy never follows its library: a scenario plays its own bytes.

### Build the map and scenario editor

- Priority: P2
- Affected files: `internal/mode`, `internal/render/renderer`, `internal/resource`,
  `internal/fsm`, `doc/multi-platform.md`
- Prerequisite: the P1 items above
- Design: [TOML macros](toml-macros.md)

The editor writes ordinary scenarios: a directory under a root's `scenario/` that
loads, digests, transfers and lists as one written by hand, so nothing at run time
knows an editor made it. It brings no format and no table of its own: documents
are edited in place, blocks are macros, forms come from the schema and a fault
lands on what made it, each an item above.

- **In the game.** An operator's mode beside Command and Overlay
  ([Input §2](input-and-modes.md#2-modes)): it holds a solo game, is no part of
  the journal, and is refused in a live session and in a replay. Applying an edit
  validates and restarts on it as `:n` does, since a run plays one scenario.
- **The map is the canvas.** The held world draws as it plays. The cursor moves
  by Normal's motions, counts, marks and search, a selection is Visual's, and the
  mouse paints. A tile, a spawn and a marker are entries of the map's legend.
- **Forms are menu pages.** The palette, a block's form and the scenario's
  settings are pages of the [menu](config-menu.md), searched, drawn and
  navigated as it is.
- **The graph.** A scenario's regions, states and blocks as nodes and each
  `target` an edge, drawn through an overlay as `:graph` draws a route graph,
  read-only first.
- **Hosts.** A terminal and the desktop window first. Android and a browser
  refuse it with the sentence `unavailable` gives
  ([next item](#edit-on-android-and-in-a-browser)), a difference
  `doc/multi-platform.md` records.

In order: the read-only graph; placing and editing blocks; painting a map and its
markers; the test play; library copies.

### Edit on Android and in a browser

- Priority: P2
- Affected files: `internal/resource`, `cmd/vif/browser.go`, `web/terminal.js`,
  `android/app`, `doc/multi-platform.md`
- Prerequisite: [Build the map and scenario editor](#build-the-map-and-scenario-editor)

A browser has no root to write a scenario in, and its storage is the few MiB the
settings and the places share; Android's user root is in the app's files, out of
its player's reach. Both need the store the downloads ask for
([Manage the downloads](#manage-the-downloads-from-the-menu-and-the-command-line)),
keyed by name for an author's own; a scenario file taken in and handed out, the
`.vifscen` of [Fetch a bundle](#fetch-a-content-addressed-bundle-over-http); and
the gestures a canvas takes of a touch screen, a drag that paints and two fingers
that pan.

### Carry an image in the scenario

- Priority: P3
- Affected files: `internal/resource/scenario.go` (`validScenarioName`, the
  bounds), `internal/service/adapter_file.go`, `internal/system/wall.go`
- Prerequisite: [Lay a painted map a scenario carries](#lay-a-painted-map-a-scenario-carries)

An `.aimg` or `.aimgz` the entry names joins the scenario's files, under its
bounds and its digest, so a backdrop reaches a joiner and a browser as its maps
do. The container admits `.toml` alone, `WallPatternSpawnRequest` opens a name
below a root's `image/` through the file service, and an image's block mask is
its request's, one for every cell.

### Give the vim keys what their second round left

- Priority: P3
- Affected files: `internal/input/machine.go`, `actionnames.go`,
  `internal/mode/operators.go`, `router.go`
- Guide: [Input and modes §3](input-and-modes.md#3-normal-mode-grammar),
  [§10](input-and-modes.md#10-keymap-override-files)

The second round is in: a register for `y` and the puts, a change undo and redo,
marks, `o O`, `s S X ~`, counted searches, `.` with its Insert text, and Visual's
`d c y r ~ *` over the ping bounds. A vim key whose letter vif reads its own way
has an action no default key binds, which the vim keymap (`-k vim`) and the
tutorial attach. What is left:

- **What the vim keymap cannot give back.** `H` and `L` go to the map's top and
  bottom rows where vim's go to the window's, and `J`, `K`, Ctrl-F, Ctrl-B, Ctrl-E
  and Ctrl-Y have no action: a join, a lookup, a whole page and a scrolled view.
  Enter, Space, Backspace, Tab, `[`, `]` and `gh gj gk gl` stay the game's.

- **Named registers.** `"a` before a yank, a delete or a put would name one of 26
  registers beside the unnamed one, sharing the macros' letters but not their
  store.
- **A count on undo and redo.** Each press takes back one change: a second undo
  in the same press reads cells the first has not restored yet, so a count needs
  the settle between them that `.` gets for its Insert text (`Machine.Next`).
- **Visual's other keys.** `x`, `X`, `D`, `C`, `s`, `S` and the puts act there as
  in Normal; vim's take the selection, `p` replacing it.
- **Stacked glyphs.** The register and an undo keep the glyph drawn in a cell,
  where a delete also takes the layers under it
  ([below](#decide-what-a-delete-removes-under-a-drift-stack)).

### Let a scenario ask for a play mode

- Priority: P3
- Affected files: `internal/mode/router.go`, `internal/system/meta.go`,
  `wad/scenario/base/tutorial/scenario.toml`
- Guide: [Tutorial §3](tutorial.md#3-the-next-round)

A tutorial lesson that ends on a typed glyph leaves the next one in Insert, where
its first key types, and the lesson can only say so on the status bar. A live run's
mode changes through the router alone, which keeps the input machine in step, so a
request from the FSM needs the router to take it, and the journal and a session to
carry it. The same request would let a capture answer a key with a mode change, for
a key that does what no action does. Rewriting intents in the router for one
scenario stays out: a second mechanism beside the key table and the capture.

### Grow the tutorial

- Priority: P3
- Affected files: `wad/scenario/base/tutorial/`
- Guide: [Tutorial §3](tutorial.md#3-the-next-round)

The tutorial runs twenty-two lessons: the motions, counts, `x dd`, `d` with a
motion, `i` and typing, `A I a`, `o O`, `c cc`, `r ~`, `.`, `y p P`, `u` and
Ctrl-R, `%`, `/ n`, `* #`, `m '`, `gg G M`, `v`, then what a game makes of the
keys it gave back to vi, and free practice; `s` skips and `S` goes back. The next
round adds `[ ]`, the markers and macros, practises the keys it only tells
(`N`, `M`, `C`, `X`, `P`), lays a lesson again on a resize, centres from the
map's edge (`origin`, [FSM reference](fsm-reference.md#event-emission)),
and cues a lesson cleared.

## Browser sessions

### Fetch a content-addressed bundle over HTTP

- Priority: P1
- Affected files: `internal/resource`, `cmd/vif/browser.go`, `pkg/browser`,
  `web/terminal.js`, `web/worker.js`, `Makefile` (`wasm`); on the site, `make vif`,
  `make compress` and `_partials/apps/publish.html`
- Prerequisite: the decisions in the last paragraph

The page plays the embedded `main` alone: a guest is served its coordinator's
scenario and a native player unpacks the wad, but nothing reaches a client with no
config root and no peer, so `wad/scenario/` is out of the site's solo Play. The
corpus needs no route, being player domain. A URL cannot carry one: the launcher
takes 1,024 characters an argument, `cave` deflates to about 13 KB, and a query
is logged and kept in history
([Multi-platform §6](multi-platform.md#6-browser-launch-arguments)).

The container is decided: `resource.Scenario`'s canonical form, one file a
scenario, its SHA-256 the digest a session names. The plan, same origin only:

1. `make wasm` has `vif` write `web/scenario/<name>.vifscen`, each scenario's
   `Marshal`, uncompressed so the file's SHA-256 is its digest (the site gzips it
   as it does the binary), and `index.json` beside them: name, description,
   players, fixed map, digest. The bytes are then the ones a coordinator sends.
2. The site publishes each under its content hash, which is that digest (`make
   vif` refuses a mismatch), and the index under its own, named to the launcher
   as `data-scenarios`, as `data-wasm` names the binary.
3. An HTTP provider in `internal/resource`, on the `net/http` `pkg/network`
   already links (fetch in a page), reads the index, fetches a file, holds it to
   `UnmarshalScenario`'s bounds and the index's digest, and hands it on as
   `Options.Provided`. The page's `-s` (`?arg=-s=td`) resolves through it, the
   menu's Scenarios page lists the index, and a hosting page
   ([Publish a player's session](#publish-a-players-session-at-the-router)) offers
   any listed scenario.

Served from the binary's own origin, a scenario adds no trust root, and the page's
CSP (`connect-src 'self'`) already admits the fetch. To decide: the extension, and
how the site's edge caches and precompresses it (the site's `doc/nginx.md`); every
`wad/scenario/` or only those a scenario marks for publishing; where the site
offers them (a picker by the lobby's Play solo, links per scenario); a visitor's
own scenario, picked or dropped on the start screen or carried in a link's
fragment, which no server sees (about 17 KB for `cave`), and whether one from
a stranger may run; a native `-s https://…`, which needs an index at a fixed path,
and from another origin a publisher's signature; any cache by digest beyond
HTTP's own, which a native run has in its downloads.

### Manage the downloads from the menu and the command line

- Priority: P2
- Affected files: `internal/mode` (`config_menu.go`, `commands.go`),
  `internal/resource` (`SaveDownload`), `cmd/vif/browser.go`, `web/terminal.js`
- Guide: [Filesystem layout §2](filesystem-layout.md#2-installed-configuration-tree)

`[session] save_downloads` keeps every scenario a host sends in
`scenario/downloads/`, and only a file manager removes one, so a player who joins
many sessions collects them, up to 1 MiB each. The Scenarios page would list the
downloads apart from the player's own, with size and age, and remove one or all
behind a confirm, as a command would (`:downloads`, `:downloads rm <name>`); a
Settings row would turn saving on where `vif.toml` is out of reach, as on Android.
A browser saves none: a page has only the site's storage, a few MiB that the
settings and places share, so saving there waits on these controls, a cap in
bytes that evicts the oldest, and the digest as the key (IndexedDB). A save cut
off mid-write leaves a hidden `.<name>.*` directory beside them for the same
clean-up.

### Launch the page and Android without their start screen

- Priority: P3
- Affected files: `web/terminal.js`, `web/worker.js`, `pkg/browser`,
  `android/app/src/main/java/com/lixen/vif/StartScreen.java`

Both open on their start screen whatever `[play] wait` says. The page's is up
before its program runs, so it cannot read the setting in time, and its press is
also the gesture a browser needs before it plays sound; Android's could close at
once, but would flash. Running the page's program behind its screen, as Android's
runs behind its own (`Host.Away`), would let `wait = false` skip both.

### Join a pasted game link from the Android app

- Priority: P2
- Affected files: `android/app/src/main/java/com/lixen/vif/StartScreen.java`,
  `MainActivity.java`, `android/game/game.go`

The site hands out a game's link in three shapes: the terminal command (`vif -join
vif://lixen.com:7777/<game>`), the bare `vif://` link, and the browser's
`https://lixen.com/vif/wasm/?arg=-join=wss://…`. The app takes none of them short
of typing `:join` and the link on the soft keyboard. A Join button on its start
screen could read the clipboard (or a field to paste into), strip a leading `vif
-join` and the browser link's `?arg=-join=` wrapper, keep the `vif://` or `wss://`
link and join it as `:join` does; a `vif://` intent filter would open links
tapped elsewhere. Unchecked without an emulator or a phone, so not built yet.

### Warn in the site's lobby when a session runs another build

- Priority: P2
- Affected files: the Hugo site's `vif-lobby.js`, `.github/workflows/release.yml`

Each row of the router's session list names the build its session runs
(`state.build`), as the refusal a page of another build reads does. The site's
lobby can show it, and warn before a Play that would be refused once it knows the
page's own build, which the release's web bundle does not name yet.

## Packaging

Distribution-specific detail lives in [Packaging](packaging.md).

### Promote a nightly build to the first stable release

- Priority: P1
- Affected files: `doc/packaging.md`, `README.md`
- Prerequisite: choose a nightly commit after the complete verification gate
  passes

Tag the selected commit `v0.1.0` and publish the draft release the tag creates,
which already carries the source archive and its checksum; then drop the "no final
release" lines in `README.md` and [Packaging](packaging.md) §2.

### Rewrite the history without the third-party names and files

- Priority: P1
- Affected files: every commit
- Prerequisite: the names PR merged and no other PR open

The names audit (2026-11-14) took third-party game, brand and song names out of
the tree, replaced a downloaded picture (`wad/image/test.aimg`) and deleted a
stray 13.5 MB binary (`vif-router`). Older trees, commit messages and squash
titles still carry them. One `git filter-repo` pass takes them out of every
branch and tag:
`--strip-blobs-with-ids` with the binary's blob
`d6c2405eab3fc7ddc344b63a0513af09ebf1703d` and the old picture's
`dfede4b85aa34b34e968bba8b73180e11876cfed`, `--invert-paths` for the deleted
plan doc, `--path-rename` for each renamed scenario, test and doc, since a text
replacement renames no path, and `--replace-text` with `--replace-message`, from
the list of names and paths kept in the project's files, outside the repository,
since it spells them. What it costs, and what it does not reach:

- Every hash changes: each clone and fork re-clones, open branches are rebased
  by hand, signed commits lose their signatures, and each release tag is set
  again on its rewritten commit; release assets already uploaded keep the old
  files.
- vif has no version tags, only `nightly`, so no published version's checksum
  is at stake, but proxy.golang.org keeps any pseudo-version someone fetched as
  it was, the binary and the names with it.
- GitHub keeps every PR's `refs/pull/<n>/head`, which no push deletes, so the
  old commits stay reachable there and by hash in cached views. GitHub Support
  removes only sensitive data that rotating a credential cannot make safe, so it
  will not purge them. PR titles and bodies are edited on GitHub by hand.

### Ship the third-party notices with every binary

- Priority: P1
- Affected files: `Makefile`, `.github/workflows/release.yml`,
  `android/app/build.gradle.kts`, `internal/system/meta.go`

A binary carries the licences of what it links, and they ask that their notices
travel with every binary copy: Go's standard library, `golang.org/x/sys`,
`x/term`, `x/crypto` and, on Android, `x/mobile` (BSD-3-Clause),
`golang-jwt/jwt/v5` (MIT), `ebitengine/purego` (Apache-2.0) and the lixenwraith
modules (BSD-3-Clause). The archives and `make install` carry vif's `LICENSE`,
purego's (`LICENSE.purego`, with the desktop window) and the glyph atlas's font's
(`LICENSE.font`), and the browser bundle its page fonts' (`web/fonts/LICENSE`),
but none carries the Go modules' notices; the APK carries no licence at all,
vif's own included, and the Play build must before it lists. One notices file
made from `go.sum`'s modules (each module's licence from the module cache, by a
make target, so a dependency added later comes with its own) goes into the
archives beside the binary and into the APK's assets with the licence files,
and the About overlay points to it or lists the names.

### Sign the Android build with a release key

- Priority: P2
- Affected files: `.github/workflows/release.yml`, `android/app/build.gradle.kts`,
  `android/README.md`

The nightly and a tagged build are signed with `android/debug.keystore`, which
anyone can sign with, so a build from elsewhere installs over them. Before a
release a device should trust: generate a release keystore (`keytool -genkeypair
-keyalg RSA -keysize 4096 -validity 10000`), add it base64-encoded as
`ANDROID_KEYSTORE_BASE64` with its passwords and alias as the three other secrets
the workflow reads, and keep the keystore out of the repository; a key once
published cannot be rotated for installed apps. The first signed build will not
install over a debug-signed one: uninstall first, which drops the app's settings.
A store listing (Play wants an AAB and a `targetSdk` within a year of the newest
release) is a separate decision.

### Publish a development AUR package

- Priority: P3
- Affected files: AUR packaging metadata and `doc/packaging.md`
- Prerequisite: establish the release-package metadata first

Publish `vif-git` alongside the release-based AUR package.

### Move the Android toolchain from sdkmanager to the Android CLI

- Priority: P3
- Affected files: `android/toolchain.sh`, `android/README.md`

The command-line tools `toolchain.sh` pins (22.0) warn that `sdkmanager` is
deprecated for `android sdk`, which ships in the same package. Switch `setup` to it
once its install, licence and SDK-root flags are settled, bumping the pinned build
and its three SHA-256s together.

### Give the Windows zip sound, and confirm it in Windows Terminal

- Priority: P3
- Affected files: `Makefile`, `.github/workflows/release.yml`
- Guide: [Build profiles](multi-platform.md) §4

The zip has no sound: try a build without `vif_noaudio` with ffplay on `PATH`.
Its mouse comes from Windows 10's own console host, and likely its flicker (§4);
confirm that Windows Terminal has neither, with the terminal module's synchronized
frames and re-hidden cursor. `sandbox/termbench` tells hosts apart: `rawin` prints the
records a mouse report arrives as, `live -lib terminal|tcell` plays a scene on
each library's own backend.

### Report key releases on Windows

- Priority: P3
- Affected files: the terminal module's Windows backend and key protocol
- Guide: [Input and modes](input-and-modes.md) §1

A key on Windows is a tap: the console reads as VT input, which carries presses
alone. A console host with win32-input-mode (Windows Terminal's) reports each
key's press and release once asked with DECSET 9001, as `CSI Vk;Sc;Uc;Kd;Cs;Rc _`
(tcell asks for it and drops the releases). A Windows key source would ask for it
as the protocol source asks for the key protocol, answer presses where the host
lacks it, and pair releases through `KeyHolds`. Decide first whether holds on
Windows are worth it.

## Fleet

The design and what it cost are in [The session fleet](kubernetes-fleet.md), and
the procedure in [Fleet deploy](kube-docker-deploy.md).

### Hand off a deployment a stranger can install

- Priority: P1
- Affected files: `doc/kube-docker-deploy.md`, `deploy/`
- Guide: [Fleet deploy](kube-docker-deploy.md#13-first-session) §13

Rehearse the procedure from bare Arch Linux and from bare Ubuntu, record their
package and service differences, and fix every command that assumes the
production node. Done when both reach a first session with no step their operator
had to invent, and every resource value in `deploy/k3s/30-session.yaml` cites a
number from [Measure a full roster](#measure-a-full-roster).

### Measure a full roster

- Priority: P1
- Affected files: `deploy/k3s/30-session.yaml`, `deploy/k3s/10-quota.yaml`,
  `tool/vif-router/manifest.go`, `deploy/guest/vif-router.env`, the Hugo site's
  `vif-lobby.js`
- Guide: [Session fleet](kubernetes-fleet.md#6-resources) §6

Drive nine sessions with headless joiners for the fleet-level readings (CPU,
memory, tmpfs, log rate, rotations), plus one real four-player session over real
links, through a tower and a storm and on `wad/scenario/base/td`, for the hour the
Job's deadline allows, which tick slips and correction magnitude need. Requests,
limits and the quota totals then come from that measurement rather than from
single-guest history and estimates. It passes with no OOM, liveness restart,
sustained tick slip or growing correction magnitude, the deadline ending each
session. The socket readings (`TestNetcodeReadingsOverSockets`) cannot seat
sixteen td guests inside their join wait, so the td-16 column of netcode design
§3.1 has not been re-read since phase 1 and is this measurement's to take.

A session's own bots are read here before a request may ask for them
(`VIF_ROUTER_BOTS_MAX`, 0 until then):
- K=3 with `./script/test.sh load` or the deploy guide, replacing the sandbox
  figures the per-bot envelope stands on (2 sessions of 2 bots for 30 s: about
  190–200 ms of CPU a second and 90–95 MiB peak per host, no evictions), with the
  digest mismatches that run counted, 0–5 per host, watched as the load grows.
- A seat's cost on a map past the 4,800 cells bots are held to (`botMapCells`),
  which keeps them off td.
- The quota: its 1000m of CPU fills at three sessions of three bots, and the
  router refuses the next as `fleet_full`. Raise it, or accept the fewer sessions.
- Then the site's `vif-lobby.js` sends `bots` and reads `bots_max`.

### Apply the fleet log retention on the node

- Priority: P1
- Affected files: `deploy/guest/vif-fleet-log-cleanup.py`, `doc/kube-docker-deploy.md`

Decided: the cleanup's stale age is 75 minutes, just past the session Job's
`activeDeadlineSeconds`, so the tmpfs holds live sessions and the ones that just
ended instead of five hours of finished ones; a byte budget or a larger tmpfs
were the alternatives. Apply on the node: reinstall the cleanup script as
[Fleet deploy](kube-docker-deploy.md) §7 does, then watch `findmnt` after a full
hour of play.

### Promote verified images to the node

- Priority: P2
- Affected files: `deploy/guest/update-vif-image.sh`, `.github/workflows/release.yml`,
  `deploy/k3s/30-session.yaml`, `tool/vif-router`
- Guide: [Fleet deploy](kube-docker-deploy.md#8-the-session-image) §8

Nightly CI publishes the headless image to GHCR under moving and commit-addressed
tags, and the node builds and imports its own. What remains is choosing the
node's registry and promotion policy, authenticating pulls without a long-lived
off-node deployment credential, and moving new sessions to a verified digest while
existing ones finish on theirs.

### Put a cluster through its failures

- Priority: P2
- Affected files: `doc/kube-docker-deploy.md`
- Guide: [Session fleet](kubernetes-fleet.md#7-verification) §7

Not yet tried on a cluster: `tc netem` latency, loss and reordering, which should
recover at the next bounded correction or keyframe; a pod delete or a node
failure, whose connected-client behaviour should be recorded, with no replacement
advertised as the same match; and a full log tmpfs, which should lose a bounded
amount of log without ending a game.

### Provision audio to the fleet when a host needs it

- Priority: P3
- Affected files: `deploy/guest/update-vif-wad.sh`, `tool/vif-router/manifest.go`,
  `deploy/k3s/30-session.yaml`

What a fleet session reads from the node volume is one list in three places —
`categories` in the installer, `wadCategories` in the router, and the mounts in
the template. `scenario` and `image` are on it. `audio` goes on it the day a
dedicated host renders anything, and `content` never does. Nothing else changes;
the volume already carries whatever the installer puts there.

### Keep a standing session for the lobby

- Priority: P2, with [Train a network bot live in `cave`](#train-a-network-bot-live-in-cave)
- Affected files: `deploy/k3s/session.sh`, `deploy/k3s/30-session.yaml`,
  `tool/vif-router` (`allocator.go`, `config.go`, `manifest.go`), `internal/bot`,
  `internal/lifecycle`, `deploy/guest/vif-router.env`, the Hugo site's
  `vif-lobby.js`
- Guide: [Session fleet §9](kubernetes-fleet.md#9-the-session-routes),
  [Multiplayer §5.8](multi-player.md#58-watching-a-session)

The site's lobby is to show one game always, and the game is a bot learning `cave`
(Lixen, 2026-11-16): one pod, made by a script, seats one bot whose learner evolves
its genes round after round, saves them outside the pod and goes on from them in
the next, while visitors spectate it through the router at a copy's cost each
(fleet §9). One session then shows the learner, the game, spectating and bots.

What stands: a dedicated host's session that its own bots alone occupy starts and
plays on with no guest, a create carries `bots`, and `BOTS=1 SCENARIO=cave
./deploy/k3s/session.sh create <id>` makes that pod beside the router, whatever
`bots_max` offers a visitor. What it needs:

- **The learner on a seat.** It learns on a solo run's driver, and a seat fields
  what that kept; here the bot is the seat of a host nobody plays, in a session
  of one, at a dedicated host's one rate, 1x: 300 to 500 rounds an hour by that
  item's reading.
- **Its graph in the pod.** The pod's `-bot` names a count, which plays `default`:
  the learner's is named there (`N:graph`), embedded or served from the node's
  volume, which carries `scenario` and `image` alone (`wadCategories`).
- **A store outside the pod.** The population is saved where a setting is, and a
  session pod has a read-only root and two claims, its log and the wad: a third,
  writable, at the root the learner saves under. One writer at a time, since two
  pods of one population would each lose the other's rounds.
- **A lifetime.** The Job's hour ends it, bots or not, which costs a learner the
  round in flight. Its keeper is a router setting naming the scenario and the
  bots, which makes the session at start and again whenever it is gone, charged to
  no address's budget and counted in the quota; a timer running the script until
  then. A spectator of the one that went is told it ended and starts a local game
  (`Membership.feedEnded`): a name the router answers for whichever stands, or a
  feed's end that names its successor, keeps a page watching.
- **Its cost while nobody looks**: a pod around the clock. The router knows a
  session with no spectator past its linger (`audiences`), and a pod its idle hub
  (`FeedHub.Idle`), so the game could hold until a watch dial resumes it; a learner
  wants the rounds nobody watches, so decide which it is.
- Whether a visitor may also take a seat in it, whose defeat then resets the
  learner's round, or only watches: a roster its bots fill is still shown to a
  spectator, and the script and the router both keep a seat free
  (`bots` ≤ `players` − 1).
- The lobby showing it first, with Watch as what its row offers
  ([Watch a session as a spectator](#watch-a-session-as-a-spectator)).

## Multiplayer

### Hear a participant again once its link reopens

- Priority: P1
- Affected files: `internal/system/network.go` (`announceDeparture`,
  `removeParticipant`, `addParticipant`, `receiveAuthority`, `receiveDeparture`),
  `internal/converge/authority.go` (`onReport`, `takeBack`, `reconcile`),
  `internal/converge/reach.go` (`admitPeerLink`), `internal/app/migration_test.go`
- Guide: [Multi-player §5.1](multi-player.md#51-succession),
  [§5.2](multi-player.md#52-the-roster-after-a-loss),
  [§5.3](multi-player.md#53-reachability-under--authority-migrate)

An instance marks a participant departed as it announces its lost link
(`announceDeparture`): the coordinator any link, a guest one the participant was
behind. The coordinator's crossing marks it on every instance (`removeParticipant`).
Only an arrival clears the mark (`addParticipant`), or on a guest a join through it
(`drainWith`), and while it stands `receiveAuthority` and `receiveDeparture` drop
whatever that participant sends. A reopened peer link clears nothing:
`admitPeerLink` admits any roster member, a Wraith included, so the link stands and
the succession hears nothing on it. A person whose link to its host drops is
unaffected, since its redial proves its key and the key's arrival clears the mark.
Two cases are not:
- A participant that joined through a guest holds a link to the host too, as every
  participant does to a successor (§5.3 item 3), and its link to that guest drops
  and reopens. The guest floods a notice the host, linked to the participant,
  ignores (`receiveDeparture`), so nothing crosses and nothing clears, and the guest
  ignores it for the rest of the session. When the host goes and the guest takes
  the term, it drops the other's notices (`MsgAuthorityReport`) and crosses its
  departure at the floor, and the other takes the next term.
- A survivor's link to the candidate drops once the candidate holds the term and
  reopens before the survivor adopts a record. The new host crossed its departure,
  so the notices on the reopened link are dropped, the survivor is never answered,
  and it takes a term of its own. Only a report another participant relays passes
  the mark; one on the survivor's own link never reaches `takeBack`.

Either way the session splits: two hosts, each holding the other and the lost host
as Wraiths. A survivor with no path to the candidate is meant to split off (§5.1);
one whose link came back is not. A three-participant `meshSession` probe, 2-3 cut
with `MeshPort.Disconnect` and linked again with `Mesh.Peer`, ended with 2 on term
2 and 3 on term 3 in both: 3 joined through 2 and linked to the host beside (links
1-2, 2-3, 3-1), cut before the host closes, and each joined through the host (1-2,
2-3, 1-3), cut once 2 holds term 2. The second shape cut before the host closes, or
linked again as 2 takes the term, no longer splits: neither guest marks the other,
and both end on term 2.

A relayed person the silence window retired (§5.4) that finds another path only
afterwards is a third case, with no split: the host admits its epochs for a Wraith,
and it plays on unseen until its key comes back through a door. Its authority is
not lost, so it walks nothing and sends no notice for the remedy below to answer;
an epoch it produced after its departure applied is the evidence a host has.

What would close it:
- A link admitted after the mark was set clears it, so a mark nothing crossed is
  gone the moment the link is back. That fixes the first case alone.
- For a participant whose departure crossed, the reopened link carries its walk: the
  host answers its notice, takes its membership report, and gives the place back
  through `CrossReturns`, whose arrival clears the mark everywhere. `takeBack`
  declines a report that lands before the floor (`reconcileAt` still set), and
  `recordReport` never offers it again, so the floor has to give back an away
  participant that reported at its term. One that lands after the floor crossed
  the departure and before it applies finds nobody away yet and is lost the same
  way (read off `takeBack`, not reproduced).
- The coordinator still crosses one departure per participant: clearing the mark
  must not let a second drop of the same link cross a second departure for a Wraith.
- A test per case on the probe's shape, with `meshSession` handing back its mesh:
  participant 3 follows 2 at term 2, and no Wraith but the lost host's remains.
  `TestALateSurvivorsPlaceIsGivenBackWithItsBots` reads the host alone: the host
  closes a survivor's own link as the floor's departure applies
  (`removeParticipant`), so the survivor's side of a place given back needs the
  reopened link this item brings.

### Close what a relayed player's departure still leaves open

- Priority: P3
- Affected files: `internal/converge/authority.go` (`driveSilence`),
  `internal/system/network.go` (`removeParticipant`, `receiveDeparture`,
  `announceDeparture`, `publishBarrierTelemetry`),
  `internal/app/internal/membership/session.go` (`keyedLocked`, `leaveRoster`)
- Guide: [Multi-player §3.6](multi-player.md#36-holding-a-session),
  [§5](multi-player.md#5-membership-topology-and-authority-continuity),
  [§5.4](multi-player.md#54-wraiths)

The authority retires a person it holds no link to once it has had no news of it
for the silence window (§5.4), crosses out behind its arrival a joiner lost before
that applies, and hears of a lost link only from the instance its participant is
behind. What those rules leave:
- **Under a hold** the window does not run, a held authority taking no tick. A
  person whose relay left under one stays live until the hold ends and the window
  passes, its key refused `RefusalReturning` meanwhile, past its dial's 30 s. And a
  departure the window crosses within a lead of a hold stays scheduled through it,
  the relay's link standing: a key that comes back through another door, its old
  link dropping after, is announced by the old relay behind its arrival
  (`announceDeparture`) and retired again as the hold ends, which costs it a redial.
- **An arrival and a departure applying in one tick**, as those crossed under a
  hold do, both run before the arrival's cursor is made. A bot arriving as its
  holder departs is missed by the holder's group (`removeParticipant`) and stays
  live and undriven until its holder returns or is dismissed, the window judging no
  bot. A seat its holder drops before its arrival applies is read as no seat
  (`receiveDeparture`) and waits a Wraith's two minutes. An arrival whose spawn
  finds no free cell leaves the door's entry away with no Wraith to time it out.
- **A relay more than a redial behind its authority** applies the departure a
  returned player came back from after that player's gate, a tick ahead of the
  arrival, and closes the new link, leaving the player to the window and a second
  dial (read off `removeParticipant`, not reproduced).
- **Telemetry.** A guest that takes a departure from a capture rather than its
  crossing never marks the source, and it marks no participant it was not carrying:
  a person whose relay left, playing on behind another guest, counts that relay in
  `network.barrier_peer_lag_ticks` and `network.barrier_ran_without_peer` for the
  rest of the run. Nothing reads either.

What would close them: under a hold, the first rule below kept to one, the door
reading as departing a person whose epochs last came in on a link that is; for one
tick, the handlers asking what an arrival of that tick is about to make (`rosterDue`
keeping its holder, and the spawn's refusal reaching the door); for telemetry, the
capture's rebind marking each Wraith's source.

Two rules were weighed against the window and not taken:
- **Only after another departure.** The authority counts from the departure of the
  link a person's epochs came in on. A stall alone retires nobody and a hold needs
  no clock, but a person lost with no departure before it stays a live cursor
  nobody drives, as one its adopter stopped hearing before the link died or whose
  notice outran its relay's word that it had joined, and it takes a case list and
  a record of each person's last link.
- **No timer, only a returning key.** A proved key takes back a place the host
  holds no link to. Nobody is retired while it plays, but a lost player holds its
  slot as a live cursor until it comes back, a key cannot tell a lost relay from a
  live one whose link is half open, and the old relay's late notice then retires
  the returned player again.

### Refuse a combat crossing that acts as another cursor or emitter

- Priority: P1
- Affected files: `internal/system/network.go` (`crossingActors`,
  `admissibleFromSource`), `internal/system/combat.go` (`applyHitDirect`,
  `applyHitArea`), `internal/app/correction_test.go`
- Guide: [Multiplayer §2](multi-player.md#2-current-contract) (Trust)

`crossingActors` holds no row for `EventCombatAttackDirectRequest`, so a modified
participant's rod, tracer or bullet hit may name another participant's cursor as
its `OwnerEntity`: every instance then spends that cursor's immunity allowance
against the target (its own hits are refused for the window), credits it the kill,
and its own instance pays it the drain reward. Neither combat crossing checks
`OriginEntity`, the area crossing's row reading the owner alone, yet both resolve
the attacker's profile from it: an area crossing naming an eye with
`CombatAttackSelfDestruct` deals the eye's 5 against a Shared species where a
cursor weapon deals 1. Every legitimate combat crossing names its own cursor as
both, since a Player-domain emitter is never named and a Shared one (an eye, a
mount) never crosses. What would close it: a row for the direct request on its
owner, and on both requests an emitter that is zero or the owner; a refusal test
for each beside the `network.artifacts_refused` ones.

A crossing's target is an entity number too, its producer's: an instance that
applied an arrival late allocates the cursor after what the others allocated
first, and holds both under other numbers until a correction. A typed member's
end checks what it names (`CompositeSystem.destroyMember`), after a host destroyed
a Wraith's cursor under a gold member's number; a hit's `TargetEntity` and
`HitEntity` are not checked against what their producer meant.

### Publish a player's session at the router

- Priority: P3
- Affected files: `tool/vif-router`, `internal/app/internal/membership/host.go`,
  `internal/network`, `web/terminal.js`
- Guide: [Multiplayer](multi-player.md#56-relay-doors-and-the-router) §5.6

A run publishes its `-relay` door through the router's reverse tunnel and is
reached at the site; a host's door published the same way is a player's own session
reached there, the router neither allocating nor authoring. The deployment this
leads to serves as many pods as the node has resources for (`-sessions-max`, ten
today) and routes every session beyond them. The tunnel carries a door already, and
the foundation stands: a name is resumed only by the run it was issued to, within
`NetworkTunnelResumeWindow`, so no published name shadows a pod's; one address
holds at most `-doors-per-address` doors; every connection placed is accounted for
as it closes (`session route closed`: session, door or pod, bytes each way,
seconds); and the listing's `limits` carry `sessions_max`, `doors` and `doors_max`,
so the site can say when the fleet is full and offer a hosted session instead.
Joins and creates are budgeted by account where a sign-in names one; a publication
is budgeted by address alone, and its `Resume` token is the last bearer secret on
the wire ([Auth and session design §2](auth-session-design.md#2-what-authenticates)).

What the feature itself needs, best shaped by deploying it step by step:
- `-host vif://<site>:7777` opening the door through the router rather than binding
  one, and the host's run reporting its state on the control connection (players,
  ceiling, scenario, whether it admits) so the router lists it without a `/health`
  read through the tunnel.
- The session's row in `/vif/api/sessions`, with a `kind` beside the pods', its
  lifetime (the control connection's, plus the resume window), and the create
  budget charged to the publisher's address as a pod's is.
- The publish ticket: the publish frame carries the run's key and, over TLS, its
  sign-in (`PublishRequest` is JSON, so a field is added without a protocol change);
  the router asks which account the sign-in names and refuses a publication by none
  (D12: publishing a door takes an account), budgets doors by account beside
  `-doors-per-address`, and a resume signs the router's nonce with the run's key in
  place of presenting the bearer `Resume` token, so no bearer secret is left on the
  wire ([Auth and session design §3.6](auth-session-design.md#36-admission-tickets-and-accounts),
  [§3.7](auth-session-design.md#37-budgets)). Implication: a `-relay` or hosted
  publication needs `:login` first; an anonymous run still joins through a published
  door.
- The front door's `-tunnels` ceiling split between relay doors and hosted
  sessions, or one budget for both, decided from how the first deployments fill.
- A page as the publisher. `DoorTunnel` dials through `link.Dial`, which speaks
  `wss://` in a page, but the browser route wants a session id in its path before
  the first frame, so a publish needs a path of its own. A hosting page must be
  exempt from `LEAVE_HIDDEN_MS`, holds only the embedded `main` until bundles are
  fetched over HTTP, and is never a successor door. Wasm runs host work about 3.9x
  slower than a native core: `main` with eight participants takes under half the
  page's thread and `td` does not fit, so the authority belongs in a Worker of its
  own.
- `-doors-per-address` counted as the join budget counts an address. It counts the
  dialler's whole host (`addressHost`), so one IPv6 /64 holds that many doors an
  address, up to `-tunnels`; the join budget and a session's spectators are
  counted by `network.AdmissionKey`.

### Project a correction outside the live lock

- Priority: P2
- Affected files: `internal/app/internal/corrector/snapshot_stage.go`, `internal/system/network.go`
- Guide: [Netcode design](netcode-design.md#less-work-under-the-live-lock)

An install compares the staging world's stores against the live ones in place and
writes the cells that differ, with no capture of either (`ReconcileSharedFrom`); the
deepest td install of netcode design §3.7 holds the live lock 8 ms, 7.7 of them the
write, down from 48. What the lock still holds is the projection: each projected
tick adds a simulation tick, 2.6 ms at td, so a repair 16 ticks behind holds it
past 45 ms. Moving it out: project to the live tick off the lock, then under it feed
what the barrier retained since the first feed, project the ticks the live world
advanced meanwhile, and write. Any out-of-lock or incremental design must keep
[Multi-player](multi-player.md) §3.3: fence membership, pending barrier crossings
not fed, the ledger settled before projection, the journal's place and mark, and no
live tick between the final projection and the write; the second feed needs a
retention watermark the barrier does not keep, since `RetainedAfter` selects by
tick and would feed the first feed's records again. WASM gains scheduling time, not
parallel CPU.

### Split combat telemetry by domain

- Priority: P3
- Affected files: `internal/system/combat.go`, `internal/status/key.go`
- Guide: [Domain model §7](domain-design.md#7-current-limits)

Combat's counters mix Shared results with per-instance ones, so two instances'
Shared combat outcomes do not compare directly. Splitting them is a telemetry
redesign rather than a domain-boundary fix.

### Offer lixen.com games on the start screen

- Priority: P1
- Affected files: `android/app/src/main/java/com/lixen/vif/StartScreen.java`,
  `android/app/src/main/java/com/lixen/vif/MainActivity.java`, `android/game/game.go`

Build it once the join refactor lands and its join matrix passes. Android opens on
a start screen with one choice, "Tap to start", a solo run; the site is reached only
from inside the run, through the menu's Multiplayer page (join a link, or request a
session from `https://lixen.com`, its default; `:register` and `:login` keep a
sign-in in the app's files, which a request to the site sends). Put a second choice beside the tap:
host a game at the site, as that request does, and/or join one of the running games
the site lists (`GET /vif/api/sessions`), so `game.Start` takes a join target. The
page needs nothing: the site's lobby (`/vif/lobby/`) is its choice, and its Play
link opens the page with `-join`, which joins at its start screen's tap. A run that
ends in an error, such as a refused join, shows it on the start screen, as the page
does; today Android finishes the activity and leaves the reason in logcat. Open:
host, list or both; a second screen or rows on this one; what a session that ends
returns to.

### Run nearby games on a phone

- Priority: P1
- Affected files: `android/app/src/main/java/com/lixen/vif/MainActivity.java`,
  `android/game/game.go`, `pkg/network/discovery.go`
- Guide: [Multiplayer §5.7](multi-player.md#57-games-on-the-local-network)

Finding games on the local network was run between two runs on one Linux machine;
on Android the APK builds and no device has run it. On one Wi-Fi, a phone and a
desktop or two phones, a desktop's firewall passing 7777 in for tcp and udp: the
host enters `:host :7777` and `:lan on`; the other
opens Menu, Multiplayer and sees it under Nearby games within two seconds, and
its row joins it; then the roles swap. What only a device shows:

- the multicast lock is taken with no crash, and dropped when the app is left
  (`adb shell dumpsys wifi`, the multicast locks held);
- a phone hosting on its own hotspot is found by its guests;
- the list empties five seconds after the host enters `:lan off`;
- Android 17 and GrapheneOS reach the local network at target 36.

### Ask for the local network on Android at target SDK 37

- Priority: P3
- Affected files: `android/app/build.gradle.kts`,
  `android/app/src/main/AndroidManifest.xml`,
  `android/app/src/main/java/com/lixen/vif/MainActivity.java`, `android/game/game.go`
- Guide: [Multiplayer §5.7](multi-player.md#57-games-on-the-local-network)

The app targets SDK 36, where local network access comes with `INTERNET`. From
target 37 Android blocks every packet to or from a local address, tcp and udp
alike, until the player grants `ACCESS_LOCAL_NETWORK` (the `NEARBY_DEVICES`
group), and its documentation says not to declare or ask for it below 37. So the
bump declares it and asks on the player's gesture: turning Visible on this
network on, opening Nearby games, `:host`, and a `:join` to a local address,
each refused in the player's words when it is denied. Play has asked new apps and
updates for target 36 since 31 August 2026 and by its yearly step would ask for
37 a year on, which it has not announced; a sideloaded APK is never asked. The
system's own picker (`NsdManager`, `DiscoveryRequest.FLAG_SHOW_PICKER`) needs no
permission, but it finds mDNS services, which vif does not announce.

### Watch a session as a spectator

- Priority: P2
- Affected files: `internal/app/internal/membership/watch.go`, `host.go`,
  `internal/mode/commands.go` (`handleSpectateCommand`), `config_form.go`,
  `cmd/vif`, `tool/vif-router/audience.go`,
  `internal/app/internal/scribe/journal.go`, `internal/system/particle.go`,
  `glyph.go`, the Hugo site's `vif-lobby.js`
- Guide: [Multiplayer §5.8](multi-player.md#58-watching-a-session),
  [Session fleet §9](kubernetes-fleet.md#9-the-session-routes)

A run spectates a session from the one feed its authority writes, a fleet
session's through the router, and looks from any player's cursor (§5.8). What
that leaves:

- **The site's Watch link.** The listing carries `spectators` in each session and
  `spectators_max` in `limits`, and the lobby reads neither. Beside Play, a Watch
  link that opens the page with `?arg=-spectate=<ws_url>`, offered where
  `spectators_max` is above 0 and the game has started, with the count in the row.
- **Consent.** Who may be watched is a ceiling set where a session is served:
  `-spectators` on a host, 8 unless set and a flag alone, and the router's for its
  whole fleet. Missing: "Let others watch" on the Multiplayer page, saved with the
  rest of `Play`, and a ceiling a host changes mid-session; a fleet session whose
  creator asks for none (a field of the create request, the pod's `-spectators 0`,
  the listing saying so); and a player's own switch that keeps the view off its
  cursor, carried on the roster entry as `Key` is, seats taking their holder's.
  Players are told nothing: only a host's `:session` counts its spectators, and a
  fleet pod counts one, the router.
- **A passphrase on `:spectate` and the menu's form.** A spectator shows a gated
  door the phrase its run already holds (`-passphrase`, an earlier `:join`);
  neither `:spectate <target> [slot]` nor Spectate a session takes one, as
  `:join <target> [passphrase]` does.
- **A session whose host went.** A viewer that lost its feed's link dials the
  address it watched and starts a local game when nothing answers
  (`Membership.feedEnded`). Under `-authority migrate` the session plays on at a
  successor's port, which a viewer is never shown (`SessionOffer.Watched()` carries
  no chain), so that nobody's address reaches a run that only watches. Following
  takes a door that names no participant: the session's own at the router
  ([Publish a player's session at the router](#publish-a-players-session-at-the-router)).
- **A door a run published, and a relay's.** A guest's door serves joins alone and
  refuses a watch dial (`errNotHosting`; the router's `ErrUnwatched` for a tunnel).
  A guest subscribed to its host's feed could repeat it at its door
  (`FeedHub.Repeat`, as the router does), which would also take the audience's
  bytes off the host.
- **A lobby that has not started.** A watch dial at a session in its lobby is
  refused as not started (`ErrUnstarted`), for a person to retry; held at the door
  and fed from the first tick, a spectator would see the start.
- **Spectators behind one address.** A watch dial is one of an address's six a
  minute at a host's door, and the router adds `-spectators-per-address` (4): a
  classroom behind one NAT shares both. A viewer shows no sign-in, so no account's
  budget applies. Options: a wider budget for a dial that only watches, or an
  account's share for a viewer that chooses to show one.
- **A spectator's journal.** A viewer keeps none: `-spectate` refuses `-journal`
  and `:journal` is unavailable while spectating. Its feed from a starting point
  on is a whole recording of the shared world: written to a file as it arrives, a
  viewer replays the session from it, with its changes of view journaled as local
  events so the replay looks from the same players.
- **The periodic world.** Every `FeedSyncTicks` (600) each viewer is sent a whole
  world, 3.9 to 9.1 kB and a tenth to a fifth of the feed, which one already
  reading only checks itself against. A digest would check the same in a few
  bytes, the whole world kept for a newcomer alone; read what that does to the
  tail a newcomer catches up through (netcode design §3.8, `VIF_FEED=1`).
- **A shorter record.** `FeedRecordTicks` (10) puts a viewer 9 to 13 ticks behind.
  Fewer ticks a record bring it closer for more records and less to deflate
  against: read both at 5 before choosing.
- **An audience on the node.** The copy's cost was read on loopback in a container
  (netcode design §3.8). Read 256 spectators of a four-player session through the
  edge: the router's CPU and egress beside the pod's, and what the edge's own
  connection limits pass.
- **Waves and a scenario's lines.** The decay and blossom waves a shared state
  machine raises fall on a spectator's screen as on every run's
  (`ParticleSystem.spawnWave`), over no text, while the lines a scenario lays, a
  tutorial's lessons and scenery, are glyphs and so are not drawn there
  (`EventGlyphLineRequest`). If either reads wrong, a viewer's world skips the waves
  as it skips the field, or draws a scenario's scenery lines.

A spectator sees the Shared domain, what any remote participant sees, from the
cursor of the player it follows, whose owner-synced values the status bar reads;
its own run lays no glyph or nugget. Showing a player's own domain is not pursued
(Lixen, 2026-11-14): Player-domain state is never rebuilt on another instance
(D-2) and the authority holds none of it, so that player's glyphs, drains,
nuggets, projectiles and loot stay on its run.

Beside it, from the same review of the journal's slot handling: a journal records
its slot from `LocalSlot` at the opening (`JournalCapture.Slot`,
`JournalAnchor.Slot`), its replay binds input there (`Scribe.OpenedLocked`), and
later installs bind by identity (`RebindCursorRoster`). One a joiner opens before
its arrival may record `NoPlayerSlot` and must bind when the arrival or a
correction brings the cursor, which wants a test.

### Serve a session's feed as cacheable segments

- Priority: P3, to evaluate when a cache fronts the site or an audience outgrows
  the sockets one router holds
- Affected files: `pkg/network/feed.go`, `tool/vif-router/audience.go`,
  `internal/app/internal/membership/watch.go`, `web/terminal.js`
- Guide: [Multiplayer §5.8](multi-player.md#58-watching-a-session),
  [Netcode design §3.8](netcode-design.md#38-the-feed)

The choice made: one feed, written once and copied to every viewer down a socket
each holds open, by the host's hub or the router's. A viewer stands half a second
behind, and each costs its feeder the feed's bytes again, 0.9 to 1.9 kB/s, and
about 10 µs of CPU a record.

What the other form does: the router also cuts the feed into segments, a few
seconds each, and serves them over HTTP beside a short list of the newest, as
live video is served. A viewer fetches the list, a starting point and each segment
as it appears, and holds no socket. A cache in front of the site answers every
viewer after the first, so the node sends each segment about once however many
watch.

What it entails: a segment is the records between two cuts (`FeedCut` restarts the
deflate history already), named by its session, game and first tick, with the
starting point fetched apart, since a segment carrying one would be mostly world;
a route at the router serving them with cache headers and expiring them; a
`FeedPort` that reads fetched segments, `net/http` natively and `fetch` in a page;
and a viewer three to six seconds behind, a segment's length and its fetch,
against half a second. It can sit on the feed as built: the router's subscription
is the source of both.

Why it was not chosen: its cost is flat only where a cache fronts the site, which
is the edge's to say and no part of this repository. With none, the node answers
every fetch itself, a request a viewer a segment, which costs more than a held
socket's copy; and the lag is paid either way.

Evaluate it when a cache or CDN serves the site's paths, or when an audience is
wanted past what one router holds open (`FeedAudienceMax`, 1024 a session). Ask
which the site has; the repository cannot say.

### Seat a spectator as a guest with no cursor

- Priority: P3, to evaluate if a spectator must stand on its host's tick
- Affected files: `internal/network/session.go` (`RosterEntry`, `SessionOffer`),
  `internal/app/internal/membership/join.go`, `internal/converge`,
  `internal/engine/world.go`
- Guide: [Multiplayer §5.8](multi-player.md#58-watching-a-session),
  [Domain design §1](domain-design.md#1-domains-and-identity)

The choice made: a viewer is nobody in its session. It reads the one feed every
viewer is sent, runs a tick only once the feed closed it, and so needs no
correction and sends nothing.

What the other form does: a spectator joins as a participant with no cursor, a
second kind of cursorless roster entry beside a dedicated host's coordinator. It
holds a peer link, predicts the shared world on its own clock as a guest does and
is corrected over that link, so it stands on the session's tick, not a record
behind.

What it entails: the least new code, since the join, the capture at the seat and
the corrections exist; an entry not counted in `MaxPlayers`, never a successor and
no stall for the start gate or the hold; and a link each at the authority, with
its cadence, manifests and repairs, 1.8 to 2.5 kB/s measured to a guest against
0.9 to 1.9 for the feed every viewer shares. The spectator is in the roster, so
the players' runs are told of it, and it sends: one more link a session must
bound and can be stalled by.

Why it was not chosen: each spectator costs a guest's share of the host's CPU and
uplink, which holds an audience to about what a session seats, far from a lobby's
hundreds; and a roster entry is a place, which a spectator is kept from holding,
so that it never takes a session over and nothing names it or where it listens.

Evaluate it for a few spectators who must be live to the tick, as a referee or a
commentator. The feed's half second is `FeedRecordTicks`, and a shorter record is
the cheaper answer to try first.

## Netcode

### Price the log's and journal's 50 ms sync

- Priority: P3
- Affected files: `internal/vlog/vlog.go`

With `-log DEBUG` and a journal open, the same session's terminal ran 14% system CPU
and about 6,000 block writes a second (`proc.io_write_s`) for some 80 kB/s of
records: both files sync every `flushIntervalMs` (50 ms). The journal's crash
promise needs it; the log's does not. Measure a log that syncs on its own cadence,
or only on WARN and above.

### Read a touch client's uplink

- Priority: P3
- Affected files: `internal/mode/router.go`, `android/game/view.go`,
  `internal/system/network.go`

`network.link_bps` is the rate the far end reports receiving this instance's bytes
at, so a client reading two to seven times another's on the same link, as the
Android client did against a terminal at the pod, is sending that much more unless
`network.link_saturated` says the link was the limit. The candidate is input shape:
a drag moves the cursor to every cell the finger crosses (`Router.moveMouseCursor`,
`PushPointerMove`), a crossing each, where a key moves one cell a press, and god
mode multiplies the actions. Compare the two clients' `network.sent_epoch_bytes`,
`network.sent_sync_bytes` and `network.crossings_sent` over one window, and
`network.pace_late_ticks` for a client the OS throttles; if the epochs carry it,
decide whether a drag's intermediate cells are a move each or one move to the cell
the tick ends on.

### Land a joining seat inside its pace band

- Priority: P2
- Affected files: `internal/app/internal/membership/host.go`
  (`ResumeJoinedSession`), `internal/app/script.go` (`drive`),
  `internal/converge/correction.go` (`ProvedWorld`)
- Guide: [Multi-player §3.5](multi-player.md#35-command-frames-the-commit-and-pacing),
  [§3.6](multi-player.md#36-holding-a-session)

A join's catch-up runs the joiner to the newest epoch it heard and its first tick
follows at once, so a seat in its holder's process stands a tick ahead of it nine
samples in ten, the late edge of its band, where its pace rests: an authority
crossing reaches it with what the join took as its whole margin. Over 3 s on an
idle machine `network.barrier_late` read 2 to 12, a correction each. A hold that
late lands between ticks (§3.6), but a guest that ran a whole tick further stands
past its host, and a relay there refuses every join for the hold's length
(`ProvedWorld` waits for a proof at its own tick). What would close it: the
joiner's first tick waits part of an interval, or the catch-up stops a tick short,
read over the divergence harness's four shapes and a socket seat's
`network.barrier_late`; and a held relay serves the proof it holds.

### Settle a replay's placements as its run's install did

- Priority: P3
- Affected files: `internal/system/network.go` (`AdoptSnapshot`),
  `internal/app/replay.go` (`replayTarget.Install`), `internal/engine/resource.go`
  (`PlayerResource.Reconcile`)
- Guide: [Multi-player §3.3](multi-player.md#33-projection),
  [Logging: Written worlds and notes](logging-and-diagnostics.md#written-worlds-and-notes)

A run settles its D-18 queue when a world it installs already contains placements
it had in flight (`AdoptSnapshot` reconciles one prediction for each own move it
prunes). No record says so, and a replay has no barrier to prune, so its queue
keeps them: from that tick its view record's `predicted` depth differs, and with it
the cell the camera and a shot from the local cursor read. It takes a guest behind
its host with moves in flight, which a correction then carries forward: a newcomer
joined under a hold whose host ran 80 ticks before its first tick diverged in 6 of
6 runs. What would close it: the install's record carries how many placements it
settled, or the settle is journaled as a note, and `replayTarget.Install`
reconciles as many.

### Let the selective-apply test see an adopted owner-authored cell

- Priority: P2
- Affected files: `internal/app/migration_test.go`

`TestSelectiveApplyKeepsItsExclusionsAcrossAHandoffAndARelay` lowers the
follower's energy so a correction adopting it would show, but owner sync hands
the successor the new value first and the receiver compares its tick-close
capture, so no correction carries a differing cell: the test stays green with
every D-13 guard off at once (`applyShard`'s owner check, the encoder's
exclusion, both owner-authored cursor sets, `RebindCursorRoster`'s restore).
Perturb where the successor's mirror still differs at a tick close, and show the
test then fails with the guards off.

### Move the netcode out of the network system, then split what stays

- Priority: P3
- Affected files: `internal/system/network.go`, `pkg/network`, `internal/event`
  (the wire batch), `internal/system/domain_test.go`

`NetworkSystem` is one file of about 3,600 lines: the roster and departures,
crossings and the barrier, the relay tree, the lead and pacing, owner syncs, the
replay suffix and the snapshot's adoption, and telemetry. The netcode is meant to
plug into other projects, so the first step moves every part that is not this
game's to the package that should own it, `pkg/network` or one beside it: the
sequence `window`, the barrier's schedule of artifacts by apply tick and source,
the replay suffix, the relay tree (`up`, `down`, `carries`, `behind`,
`Downstream`), the flooding of departures, and the lead's pacing (`paceDecision`,
`driveOwnLead`). A `pkg/` package imports nothing under `internal/`, so what moves
takes a frame type of its own or a type parameter where it holds `event.WireBatch`,
and a small interface where it reads the world, the log or the status registry.
What stays is the game's: a cursor's owner syncs (`readCursorState`, D-13), the
actor a crossing may name (`actsAsOwn`), the status cells and the ECS hooks.

Then what stays splits by concern, as pure moves. The domain suite credits a file
to the system whose `Name` it declares (`declaredName`), so it must first credit a
file by its methods' receiver, or each new file is an unchecked helper that
`TestHelperFilesArePinned` refuses. Both steps conflict with every open branch on
the file, so they wait for a pause between waves of netcode work.

### Tell a host that stalls from one that is gone

- Priority: P2
- Affected files: `internal/converge/authority.go` (`driveShedding`, `peerLost`),
  `internal/converge/correction.go` (`observeFloor`), `internal/converge/reach.go`
  (`Reach.cutOff`), `internal/render/renderer/status_bar.go`
- Guide: [Multi-player §3.5](multi-player.md#35-command-frames-the-commit-and-pacing),
  [§5.1](multi-player.md#51-succession)

A guest reads its host's silence from its link alone: one that stops answering is
dead at `NetworkLinkDeadAfter`, and the guest redials or walks (§5.1). Two stalls
fall between that and a host that only slows:
- **A host whose loop stalls while its transport answers**, a deadlock on the world
  lock or a pathological tick: the probe is answered on transport goroutines, so the
  link stays up and the guest predicts alone for as long as the stall lasts. Its
  badge stays green, since `slow!` needs a cadence only a publisher writes, and the
  one sign is the floor's 8 s alert. What would close it: a floor that stays
  breached over a link that answers counts as the authority's loss and opens the
  walk `Reach.cutOff` opens, and the badge shows a breach with no cadence.
- **A host frozen for 1.1 to 5 s**, which every link survives: its clock projects
  through the freeze, the scheduler drops the debt at once, and `driveShedding`
  takes that one drop for overload, shedding the newest bot, else the newest guest
  ("Removed by the host to keep its tick"). In a scratch converge test a 1 s freeze
  shed nobody, and 1.5 s and 4 s shed the guest. What would close it: a single drop
  longer than the link-dead time is not counted, or the share has to hold over two
  windows.

### Carry what no capture holds to a guest that lost its frame

- Priority: P3
- Affected files: `internal/system/network.go` (`leaveRun`, `snapshotContainsLocked`),
  `internal/snapshot/capture.go` (`CaptureHeader`), `internal/converge/correction.go`
  (`resolve`)
- Guide: [Multi-player §3.2](multi-player.md#32-the-capture-boundary),
  [§3.6](multi-player.md#36-holding-a-session)

A hold, a rate and a run's end are in no capture, so a guest that loses the frame
carrying one hears of it from nothing else. A run's end it takes from its
authority's first epoch of the next (`leaveRun`); three cases stay open:
- a new game that waits for its host's press sends no epoch, so a guest the reset
  never reached plays the old game on until the press, then starts unheld;
- a run only counts up, so a guest a run ahead of its authority, a successor having
  taken the term before the reset reached it, refuses all that authority sends;
- a lost hold lasts until its host resumes, and a lost rate until the next one.

What would close them: the capture header carries the hold, its kind and the rate
beside the run, an install adopts them, and a whole world naming another run is
taken as a join's is.

### Correct a guest that joined while its link read as silent

- Priority: P2
- Affected files: `internal/converge/selective.go` (`publishManifest`, `silenceLimit`,
  `serveOne`), `internal/converge/correction.go` (`publishRound`, `takeKeyframe`)
- Guide: [Netcode design §2](netcode-design.md#2-the-correction-pipeline)

A person that rejoins at its pinned fork's `-relay` door can go seconds with no
authoritative world. Its link is in the relay's publication set while its run still
joins, so the manifests sent meanwhile go unanswered, and past `silenceLimit` it is
sent bodies alone, deltas. A keyframe the relay takes for its own lagging bot
(`serveOne`, `takeKeyframe`) moves their baseline past the one the person holds and
keeps the scheduled keyframe from coming due, so each is refused until the limit's
jitter lets a manifest through. A bot the person adds meanwhile waits in its relayed
start gate (`Corrections.ProvedWorld`) until a proof comes or, past
`NetworkJoinReadyTimeout`, the gate refuses it as behind and its seat dials again.
Probed runs of
`TestAPinnedForkTakesThePeopleItRelayedAlong` stalled 2 to 3.4 s in 3 of 5. What
would close it: manifests sent before a link's participant installed its gate world
count toward no silence, and a peer fed deltas alone is owed the keyframe a fallback
took.

## Session membership

A suggestion for the words, not a decision: a *session* is one world timeline under
one authority — a fleet pod, a terminal host, any instance — and its participants,
and solo play is a session of one. Commands then move participants between sessions:
`:open`/`:close` in place of `:host`, since every instance already is its own
session's authority; `:join`/`:leave` for a participant; `:drop` for an authority
removing one; `:merge`/`:split` for whole sessions.

### A passphrase prompt in the TUI and the launcher

- Priority: P3, in a session of its own
- Affected files: `internal/mode`, `internal/app/loop.go`, `web/terminal.js`,
  `web/index.html`
- Guide: [Auth and session design §3.5](auth-session-design.md#35-passphrase-doors),
  [§3.6](auth-session-design.md#36-admission-tickets-and-accounts)

A dial that meets a passphrase door with none is refused with the advice to join
with one, and a player retries with `:join <target> <passphrase>`: the refusal is
the prompt. A prompt proper would ask in place, in the command line's form on a
terminal and in the launcher's join row on a page, and dial again with the answer;
the same form takes `:login`'s name and password, which today ride the command
line and so the terminal's history. Implication: the account commands become
usable on a page, where a password on the command line is not. The same form is
where a phrase would be cleared: `:host <doors> passphrase` sets or replaces one
and nothing takes it off, so a run that set one opens every later door gated
until it restarts.

### Tighten what a relay and a successor are trusted with

- Priority: P2, in a session of its own; every mechanism it audits is in place
- Affected files: `internal/network`, `internal/app/internal/membership`,
  `internal/converge`, `tool/vif-router`
- Guide: [Auth and session design §3.4](auth-session-design.md#34-session-passes),
  [§3.5](auth-session-design.md#35-passphrase-doors),
  [Multiplayer §5](multi-player.md#5-membership-topology-and-authority-continuity)

The premise: a participant, once admitted, is trusted with the session. A relay
carries others' crossings and runs the admission exchange for newcomers; a successor
takes the term, signs the handoff record and issues passes; both hold the passphrase
door's verifier, with which a participant can test guesses offline at Argon2's cost
and stand in for the door to others. A host that took over through a router-published
door cannot publish again through the router, so its session is reached through
relays alone. The scope: list what each role can do that the admission did not grant
it, decide which of it needs a check (the authority confirming a relay's admission,
a successor's record carrying no more than the roster lets it, the verifier's reach),
which needs a limit, and which is accepted as the price of play without a server; then
plug the holes found, one PR each. A relay names the relay behind it that asked for a
join (`RelayJoin.Path`), which the authority holds only to naming nobody it has a
link to: a relay can ask as any participant behind another.

### Review what a relay hosts once its host is gone

- Priority: P2, an audit (2026-11-14)
- Affected files: `internal/app/internal/membership/host.go` (`LeaveSession`,
  `leaveSessionLocked`, `relayedLocked`, `sessionRoleLocked`),
  `internal/app/internal/membership/relay.go` (`reopenRelayDoor`, `returnAcceptor`,
  `relaySeatingLocked`), `internal/app/internal/membership/converge.go` (`redialDoor`),
  `internal/converge/authority.go` (`beginSuccession`, `driveWalk`, `endFollow`,
  `followGone`)
- Guide: [Multi-player §5.1](multi-player.md#51-succession),
  [§5.5](multi-player.md#55-leaving-a-session),
  [§5.6](multi-player.md#56-relay-doors-and-the-router); Tighten what a relay and a
  successor are trusted with, above

The concern, Lixen's: a relay in a session pinned to its host, which never chose to
be a candidate to host the whole game, now goes on hosting one for the people it
relays once that host is gone. That is fine in general, since running `-relay` already
shared its resources, and what passes through it, with those people, in a limited
scope; but it needs a second look that the boundaries hold. And the words no longer
fit what is built (2026-11-16): a relay acts as the authority of what is behind it,
its bots, the people it relays and theirs, without advertising it to anyone, where a
take-over is advertised to every participant and asked for at both ends.

What to decide first, as one definition in place of the cases: every run is the
authority of what is behind it and follows what is ahead of it, a fork is that
authority taking effect when the way up is lost, and a take-over is a run becoming
the authority of runs that are not behind it, which alone needs an address shared
and a player's word. One rule would then say whose place a run keeps, whose door
takes whom back and what a session advertises, where today the roles (`sessionRoles`:
host, successor, kept, seats, left), `relaySeatingLocked`, `relayedLocked` and
`leaveSessionLocked`'s `keepAll` each answer part of it.

What the fork does: it keeps the places of everyone behind it as it forks, the people
linked to it at its door and those behind a door of theirs, with their bots', and
nobody else's (`relayedLocked`, `NetworkSystem.Relays`); its `-relay` door, at the
address they joined at, goes on into the session it opens and takes back a key that
holds a person's place there, under the left session's name and with no passphrase,
refusing any other (`returnAcceptor`); nobody new is seated through those people
either, by a bot of theirs or a door of their own, and no spectator is shown the
session, until its player runs `:host` (`relaySeatingLocked`, `watchParticipant`);
each person waits on its relay up to `NetworkFollowWindow`, frozen, then dials the
door it joined at for one succession window, again while the run behind that door is
itself on its way back, before it forks alone; and while it holds their places the
session's role refuses `:join` and takes `:leave` and `:host`. `./script/test.sh tree`
runs it over three terminals.

What to look at:
- Whether a relay hosts its people only once it is asked to, by a switch of its own as
  Take over as host is, off by default, or by the `-relay` flag's word, rather than by
  default. Nothing is shared that was not before: each person dials the address it
  already used, and nobody's address goes to anyone new.
- Whether the door, which asks no passphrase, should ask the session's, and what it
  trusts a returning player's record with.
- The fork issues no passes (Issue a pass in a session opened past its first term).
- Gaps seen while building it, none worse than before, when each person forked alone at
  once: a person whose link to the relay blipped as it forked is not kept, nor one
  behind a door of theirs that passed nothing on within the epoch window; a relay
  that takes longer than a succession window to come back to its own relay leaves
  those behind it to fork alone; a host that stalled rather than went takes its
  relay back, and the person redials that relay's new run, whose host may still hold
  the person's place as linked; and a door the router publishes is covered by no test.
- A holder is the joiner's own word at the host (`noteJoinerReport`), so a place a
  direct guest of the host claimed was a relayed person's bot is kept too, and that
  person's own `-relay` door could seat it.
- `:host <addr> migrate` on the kept session moves a policy its people already hold
  as pinned: `beginHostingLocked` guards only a successor asking to pin, so the
  people who came back at `returnAcceptor` give up at this host's loss while its
  newcomers walk the list. Refuse a word other than the session's, as there.
- A relayed join whose relay loses its uplink before the join crosses in may leave
  its roster entry on the host (`relayRelease` drops `tellAuthority`'s false), read
  in code alone: that key would then be refused through that relay as a stale link.

### Derive a door's passphrase off the world lock

- Priority: P3
- Affected files: `internal/app/internal/membership/host.go` (`beginHostingLocked`,
  `changeScenarioLocked`), `internal/network/admission.go` (`NewPassphraseDoor`)
- Guide: [Auth and session design §3.5](auth-session-design.md#35-passphrase-doors)

`:host <addr> passphrase` derives the door's verifier, Argon2id over 64 MiB, under
the world lock and `sessionMu`, where an account command runs its KDF in a
goroutine: the ticks it takes are dropped for every participant. It is no one-line
move, since `openSessionLocked` closes the door it finds. What would close it: read
`engine.ticks_dropped` across the command first, then open the session, derive off
the lock and serve the listeners under it again.

### Widen how a guest offers to take a session over

- Priority: P3
- Affected files: `internal/app/internal/membership/converge.go` (`TakesOver`),
  `join.go`, `internal/converge/reach.go`, `internal/network` (`ChainEntry`),
  `internal/mode` (`handleJoinCommand`, the join form)
- Guide: [Multi-player §5.0](multi-player.md#50-who-listens-who-authors-who-can-be-reached),
  [§5.3](multi-player.md#53-reachability-under--authority-migrate)

A session migrates only where its host asked, and only to a guest whose player
turned Take over as host on: a saved switch, off by default, read as the run joins
(§5.3). That was chosen over two other shapes, and leaves two gaps.
- **Asked at each join, not taken.** A word on `:join`, `-join` and the join form,
  nothing saved: no standing exposure, since each session gets its own yes, but
  typed every time, and the menu's join rows, the page's launcher and Android each
  grow a field. The saved switch won as one habit in the place Relay for others
  already lives. It reopens if a switch left on from a private game is found
  sharing an address with a public one.
- **The host's migrate covering its guests, not taken.** Guests of a migrate
  session declare a port unless they opt out, as before this rule: least change,
  and a session never lacks a candidate, but a phone is exposed without its own
  yes, which is what the rule exists to stop. It reopens only for a session whose
  players already see each other's addresses, as one found on the local network.
- **The switch is the next session's.** Turned on in a session, it binds nothing
  there: the port is bound and declared as the offer is answered. The chain is
  append-only, so a late declaration is legal; it needs the bind moved behind the
  gate (`Reach`) and a message, or the join's declaration sent again.
- **Relaying needs the port taking over binds.** Relay for others takes a dropped
  guest's rejoin at the run's own port (`admitHosted`), which a guest that does
  not take over never binds, so the switch alone now carries nobody but those at
  its `-relay` door. A chain entry naming a door and no address, as a leaf's
  `-relay` door has (`SuccessionChain.WithDoor`), would publish the port for
  rejoins without making the run a candidate.

### Keep a site's certificate pin for the session it joined

- Priority: P3
- Affected files: `internal/app/internal/membership/host.go` (`joinSessionLocked`),
  `membership.go` (`RestartAsk`), `internal/app/config.go` (`restarted`),
  `internal/app/internal/config/membership.go`
- Guide: [Multi-player §5.6](multi-player.md#56-relay-doors-and-the-router),
  [Auth and session design §3.5](auth-session-design.md#35-passphrase-doors)

A site hands out a door's certificate fingerprint with its link, and the dial pins
it where `-ca` named nothing (`joinSessionLocked`, `link.TrustTLS`). The pin lives
on the dial's own copy of the config: the run the join builds holds none, so its
redial of that door after a dropped link verifies against the system roots and a
door the router certified itself is refused, the run forking where it should have
come back. What would close it: the restart carries the pin (`RestartAsk`) into a
field of its own, which the session's dials read ahead of `Trust` and the next
`:join` clears, so one site's pin never answers for another's door.

### Decide what going away ahead of a loss costs

- Priority: P2, a rule of play to choose before anything is built
- Affected files: `internal/system/cursor.go` (`restore`, `wraith`),
  `internal/system/meta.go` (`publishDefeatState`),
  `internal/app/internal/membership/session.go`
- Guide: [Multi-player §5.4](multi-player.md#54-wraiths)

A Wraith is out of play: nothing reaches it, a session counts none toward its
defeat, and its player comes back with what the cursor held where the game is
still the one it left (§5.4). A reset ends that, so leaving ahead of a defeat
brings nothing into the next game. Inside one game it still pays: a player who
drops its link as its cursor is about to die waits untouched and returns whole,
while those who stayed took the damage. The choices, none of them the netcode's:
- **A return costs something**: the record held to a floor, or a wait before the
  place is given back. Small (`CursorSystem.restore`, the door), but an honest
  drop pays it too.
- **A Wraith stays in play for a grace**: the world reaches it for some seconds
  after its link goes. Closes the dodge exactly, but the authority has to drive a
  cursor it does not own through the grace (D-13), and the Wraith's stores rest on
  it holding nothing.
- **Leave it**: a drop is a drop, and friends police themselves; a public session
  is where it would bite.

### Drop a participant and drain from the router

- Priority: P3
- Affected files: `tool/vif-router` (a loopback listener, `allocator.go`),
  `internal/app/internal/membership/host.go`, the pod's `/health` neighbour,
  `deploy/guest/vif-router.env`, `deploy/update.sh`
- Guide: [Auth and session design D16](auth-session-design.md#0-decisions),
  [§3.4](auth-session-design.md#34-session-passes),
  [Session fleet §9](kubernetes-fleet.md#9-the-session-routes)

`:player drop <slot>` drops a player with its bots, or one bot, keeps the dropped
keys and account out of the session at every door and tells the dropped person, who
plays on alone as after `:leave`; a dedicated host has no console to type it on. The
operator's authority is a loopback listener of the router's (`-operator-listen`),
reached over ssh and nothing else: the sessions with their participants by slot and
subject; a drop of one by slot, key or account, carried to the pod on an operator
port the network policy admits from the node alone, where the pod runs the drop
its console would; and a drain that stops creates, lets the sessions end and
reports when the fleet is empty, which `update.sh`'s empty-fleet gate reads
instead of polling the listing. Implication: no operator account exists, so the
listener binds loopback and takes no credential; its routes are not under
`/vif/api/` and the edge never publishes them.

### Name a fleet session's creator to its pod

- Priority: P3
- Affected files: `tool/vif-router/server.go`, `tcp_proxy.go`,
  `internal/network/admission.go` (`AdmitCreate`), `internal/app/internal/membership`
- Guide: [Auth and session design D12, D16](auth-session-design.md#0-decisions),
  [§3.6](auth-session-design.md#36-admission-tickets-and-accounts)

A fleet session is created by whoever posts to `/vif/api/sessions`, and the pod
knows nothing of them: on a dedicated host nobody may `:player drop`. The create
ticket: when an account creates a session, the router mints a ticket of kind
`create` naming the account and hands it out with the link; the creator's first
dial shows it beside its play ticket, the pod records the account as the session's
owner on the roster entry, and the owner may drop a participant, as a host's console
can, and nothing more. An anonymous create names no owner, and the operator's
route (above) stays the only other authority. Open: whether an owner's right
outlives its own departure, and whether a later `:login` can claim an unowned
session.

### Take the auth readings

- Priority: P2
- Affected files: `tool/vif-router/tcp_proxy.go` (`splice`, `stallBounded`),
  `script/`, `doc/auth-session-design.md` §8
- Guide: [Auth and session design §4](auth-session-design.md#8-ten-thousand-routed-players-on-one-machine),
  [D10](auth-session-design.md#0-decisions)

What the design projects and has not measured: the router holding 10,000
connections through a stand-in door on a second process, its RSS and CPU before
and after moving the splice's stall deadline off the writer so TCP-to-TCP copies
take the kernel's `splice(2)` path, the admission exchange's cost per connection,
and `-route-max` and `routePlacing` tuned from them; the passphrase door's Argon2
(64 MiB, three passes, one lane) timed on a page and a phone, since a dial pays it
once a join; the Android binary's growth from `lixenwraith/auth`, which the native
and wasm readings put under one percent. The identity service's own readings (RSS at
10,000 verifiers, the launch-day introspection rate) are its repository's todo.
Implication: the budget in §8 stands as a projection until these are in; a reading
that misses it changes `-route-max`, `MemoryMax` or the splice before anything else.

### Give up a place to a newcomer while the session is held

- Priority: P3
- Affected files: `internal/app/internal/membership/session.go` (`yieldLocked`,
  `assignFor`), `internal/system/network.go` (`expireWraiths`, `addParticipant`)
- Guide: [Multi-player §3.6](multi-player.md#36-holding-a-session),
  [§5.4](multi-player.md#54-wraiths)

A held session takes a player back (§5.4) and admits a newcomer to a free place
(§3.6), but a full one gives no place up. The yield asks the authority to dismiss
the Wraith away longest, which it crosses from a tick (`expireWraiths`), and a held
session takes none: the dial is refused `RefusalYielding` until the hold ends, and
a player crossed out under the hold is not away to a newcomer at all. It was left
out of the held return because it is another rule, not the door's reading: a
place's identity and slot return to the pool as its dismissal applies
(`leaveRoster`), so the door would hand out a slot the newcomer's world still holds
as another's cursor, its arrival crossing behind the dismissal, and the dismissed
player's own key, its bots' places and a new game under the hold each need an
answer. Decide first whether a paused session should turn anyone out for a newcomer.

### Take a returned run into its successor's session

- Priority: P3
- Affected files: `internal/converge/authority.go` (`adopt`, `driveWalk`,
  `takeBack`), `internal/app/internal/membership/converge.go` (`takeOwnPlaceLocked`)
- Guide: [Multi-player §5.1](multi-player.md#51-succession),
  [§5.4](multi-player.md#54-wraiths)

A run that takes its place back under a hold waits for its own arrival (§5.4),
which only the authority it came back through scheduled: a held host flushes
nothing, so no successor's group holds it. If that authority goes first, the run
gives the session up and plays on alone in its own cursor (`arrivalLost`), and its
place waits as a Wraith in the successor's session for its key. Following the
successor instead left it in the session for good, its cursor live and driven by
nobody (a socket probe: host, successor and a guest that dropped and came back
under the hold, then the host closed). What would keep it in: the successor gives a
survivor that reports itself waiting a departure and an arrival of its own term,
which is the walk "Hear a participant again once its link reopens" gives a
reopened link (`CrossReturns`); do it with that item.

### Keep a Wraith's stats in the session

- Priority: P2
- Affected files: `internal/app/internal/membership/place.go`, `identity.go`,
  `host.go`, `internal/system/cursor.go`, `internal/system/network.go`
- Guide: [Multi-player](multi-player.md#54-wraiths) §5.4

A player coming back to its Wraith brings its cursor's stats itself
(`event.CursorRecord`: energy, heat, health and loadout): the last owner-state sync
its run made in that session, frozen once the run leaves it, so play in a fork since
never comes back. This is a compromise. The values are owner-authored
([D-13](domain-design.md#d-13--owner-authored-shared-values-have-one-writer)) and a
Wraith gives up everything the world reaches it through, so neither the host nor the
shared world keeps them, and the session takes the returning run's word for them.
`CursorSystem.restore` takes a record only of the game still played (`CursorRecord.Run`
against the queue's run, which a reset advances) and inside the ranges a sync is held
to (`heldInRange`), so a reset's new game and an edited record both leave the
template. Inside those the word is still the returning run's, as a live sync is its
owner's: energy has no bound, and an edited record names any game.

The fix shares some data of every cursor, so the session holds what each Wraith
resumes with and a successor inherits it. That moves those values off the one-writer
rule (D-2, D-13) for every system that writes them owner-side, and costs bandwidth,
which is why it waits. A cheaper step, a Wraith keeping the copy the sync committed
at its departure tick, is outside the hash and the repair (D-13), so nothing holds
the instances' copies equal.

The record is built to be replaced: one type, made in one place
(`NetworkResource.OnOwnState` into the place keeper) and read in one
(`CursorSystem.restore`), carried by the arrival crossing the coordinator fills
(`ParticipantJoinedPayload.Record`). What would close it: decide which values the
session holds, have the coordinator fill the arrival from its own world instead of
the joiner's report, and drop the record from the keeper and the report.

### Merge two sessions

- Priority: P3
- Prerequisite: a redirect message, "rejoin at this link", which the split shares

Two sessions, each under its own authority, become one by keeping one world: B's
authority redirects its participants to A's link, they rejoin A as fresh players, and
B's world ends — `:leave` and the drop above are the same move for one participant.
Merging two worlds is not planned: two timelines share no state to reconcile, which
is why a higher term from a fork is refused today.

### Split a session under a second authority

- Priority: P3
- Prerequisite: the redirect message, and an authority seeded from a live world

For a session that outgrows its authority — a large roster or a heavy scenario
lagging — twelve participants under A become seven under A and five under B. B is a
participant at first, ideally a fresh fleet pod. A hands B its current world under a
new session identity rather than a new term, redirects the five, and the two worlds
diverge from there. Late joiners already install a mid-run world; what is missing is
the redirect and an authority, for a pod a router request, that starts from a
transferred world instead of a scenario.

### Run a scenario with no players

- Priority: P3
- Affected files: `internal/app/internal/membership/session.go`,
  `internal/app/serve.go`, `wad/scenario/base/td`
- Guide: [FSM and configuration](fsm-and-configuration.md#3-root-schema) §3

`min_players = 0` is accepted and, like 1, waits for one player. A game that runs
with nobody in it, such as td's two sides attacking each other's towers, needs a
lobby quorum of 0 so a server starts without a guest, `holdVacant` parking only
below the scenario's minimum, the ownerless boot cursor dropped without a park, and
spawns that do not anchor on a cursor: the gold cycle fails and retries with none.

### Issue a pass in a session opened past its first term

- Priority: P3
- Affected files: `internal/app/internal/membership/host.go` (`openSessionLocked`),
  `internal/app/internal/membership/session.go` (`issuePassLocked`, `checkPass`),
  `internal/converge/authority.go` (`extendTermKeys`)
- Guide: [Multi-player §5.4](multi-player.md#54-wraiths) (the pass)

A pass names the session's first authority key, `TermKeys[0]`. A session a run opens
after `:leave` or a fork starts at the next term with no term keys, so `Open` fills
the earlier terms with empty keys: `issuePassLocked` signs over an empty authority
key and fails ("pass not issued", logged for every bot the leave seats again), and
`checkPass` holds every pass shown to that empty key. Nobody there is given a pass,
so a passphrase `:host` sets on such a session later spares no redial its phrase.
What would close it: a pass names the first key the session's own terms hold, or a
reopened session keeps its own term's key first.

### Publish a new session's link once its port runs

- Priority: P3
- Affected files: `internal/app/internal/membership/host.go` (`openSessionLocked`),
  `internal/system/network.go` (`publishConnectionTelemetry`)
- Guide: [Multi-player §7](multi-player.md#7-diagnostics-and-operations)

`openSessionLocked` activates the session, which publishes `network.state`, before
it starts the port, so a port not yet running reads `down` from inside the command
until the next tick. The frame loop draws between ticks, so `:host` on a solo run,
`:bot add`, `:leave` and a fork can show `Net: down` until that tick, and after
`:host` the badge's 500 ms hold (`StatusNetworkHoldDuration`) keeps it up before
`Net: wait`; where the next tick reads `idle` the badge goes at once. Read in review,
not seen on a screen. What would close it: publish again once the port runs, or
start it before the session activates.

### Say what a run alone after a loss is

- Priority: P3
- Affected files: `internal/app/internal/membership/host.go` (`sessionSummaryLocked`),
  `internal/system/network.go` (`reportDisconnect`, `publishConnectionTelemetry`),
  `internal/app/loop.go` (`setAway`), `internal/engine/world.go` (`LatchSession`)
- Guide: [Multi-player §5.5](multi-player.md#55-leaving-a-session),
  [§7](multi-player.md#7-diagnostics-and-operations)

A fork, a `:leave` and a dropped player go on in a session with no door, which
shows no badge and keeps the places §5.5 says. What the run says of itself is
still off:
- `:session` reads `Session hosting` there, in a browser too, which cannot host,
  and a run frozen for a redial or a walk reads "continuing locally from the last
  authoritative state".
- A relayed person cut off from its relay is told "Host connection lost", though
  its host may be alive, and reads `Net: down` for 10 s and then nothing while it
  redials the relay and walks.
- A run with its own bots shows their in-process links as a network session's,
  `2P:0 4ms`.
- On Android, a fork made while the player is away plays on unpaused, since away
  is applied only when it changes (`setAway`); and a guest the phone froze past its
  host's Wraith wait forks on thaw saying its host connection was lost, though the
  host never went quiet.
- A replay's badge reads the journaled session latch (`publishConnectionTelemetry`),
  which says only whether a peer is linked, so a replay never reads `idle` or
  `waiting`: where the run showed no badge (`idle`) or `Net: wait`, at a fresh door or
  at one opened where the run's bots had been, the replay shows `Net: down` for 10 s,
  then nothing. The latch would carry what the port reads (`idle`, `waiting` or
  `down`) as it carries `Peers`, for presentation only, at the cost of a journal
  field.

### Keep the other guests' places in a host-policy fork

- Priority: P3, an option not taken (2026-11-14)
- Affected files: `internal/app/internal/membership/host.go` (`LeaveSession`,
  `leaveSessionLocked`), `internal/app/host_test.go`
  (`TestAPinnedForkKeepsNobodyItDidNotBring`)
- Guide: [Multi-player §5.5](multi-player.md#55-leaving-a-session),
  [§5.4](multi-player.md#54-wraiths)

The choice made: a fork in a session pinned to its host keeps its own place, its
bots' and those of the people it relayed (Multi-player §5.5), so the host's Wraith,
the host's bots and every other guest's place go at the fork. Another guest of that
host that later dials the door the fork's `:host` opens comes in as a new player,
without the cursor and record it had in that world.

What the other option does: the fork drops only the lost host and its bots, the
places of the authority `giveUp` leaves named (`m.authorityID()`), and keeps every
other place as a Wraith for `WraithTimeout`, which `:host` starts over. A guest that
dials a door opened in that time takes its place back by its key, as it could before
this rule, when those Wraiths waited for a door with no limit.

What it entails: those Wraiths stand on the fork's map for their 2 minutes whether
anyone comes back or not, as the host's did in the report that led here. A return
takes the fork's player opening a door and the other guest finding it: a host-policy
guest has no door for them, its `-relay` door taking back the people it relayed alone,
nothing tells the others its address (§5.0), and local network
visibility is off by default (§5.7). The three-player test inverts, and §5.1 and
§5.5 change with it.

Why it was not chosen: nobody in a host-policy session signed up to come back to a
guest's run, any more than that guest signed up for its host's Wraith, and keeping a
session's people together is what `-authority migrate` with a guest that takes over
is for.

Reopen it if players of a host-policy session regroup in one guest's fork often
enough that losing their progress there is felt.

## Combat

### Let mounted weapons strike more than cursors

- Priority: P3
- Affected files: `internal/system/mount.go`, `internal/profile/combat.go`

A mounted ray destroying walls and glyphs, mounted weapons striking other species, and each instance's own drains; Shared outcomes must resolve from Shared geometry, not a Player-domain shot.

### Let cursor weapons strike other cursors

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/system/bullet.go`, `internal/system/pulse.go`, `internal/system/interaction.go`, `internal/profile/combat.go`

PvP: a hit on a remote cursor crosses as its impact and the victim's owner applies it, as `strikeCursor` does for mounts.

A cursor then takes what an attack family does rather than a fixed `CursorDamage`, and a mount's pulse already carries its family for it (`PulseSpec.Attack`, `Charged`, set per mount), so mounted weapons gain the same effects:
- Profiles with `CombatEntityCursor` as the defender, a row per family that may strike a cursor; `strikeCursor` resolves one, a mount striking as its host's combat row.
- Owner-authored cursor statuses (D-13): a stun the owner's input and its D-18 prediction obey, never refreshed while it runs; a burn ticking heat on the owner each period.
- Knockback as a push: the owner moves its own cursor along the impulse through `PushCursorMove` to a free cell, or a Shared pulse re-derives the move on every instance, as the cave rock's push does.
- Per-attacker, per-family immunity on the cursor, as species keep it, so a sustained pulse cannot stun-lock.

### Place a pulse on a map cell

- Priority: P3
- Affected files: `internal/system/mount.go`, `internal/event/payload.go`

`EventMountRequest` naming a cell and no host places a bare Shared anchor carrying the mount, so a scenario can lay a sustained, hidden pulse over part of the map, such as one that pushes cursors back once they take pushes; a level clear and a request remove it.

### Let a scenario shape the cursor's pulse

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/event/payload.go`

A request overriding the disruptor's `PulseSpec` for every cursor field by field, as a mount's is, carried in the weapon system's local save as the gun's aim is (`EventGunAimRequest`).

### Let a scenario mount and shape a shield

- Priority: P3
- Affected files: `internal/system/shield.go`, `internal/system/interaction.go`,
  `internal/event/payload.go`

A request naming an entity, a radius and a colour, so a scenario mounts a shield on
a tower or a wall and sizes or tints it from what it watches, as a cursor's energy
turns its shield on and off and sets its polarity. `ShieldComponent` carries both,
the capture with it, and any shield draws at them; missing are the request, which
the entity's owner applies as a cursor's owner writes its own shield (D-2), and a
contact rule: `CursorContactAt` reads cursors' shields alone and only a cursor's
drains energy, so a mounted shield guards nothing yet.

## Species

### Play test the death visuals

- Priority: P1
- Affected files: `internal/parameter/effect.go`, `internal/parameter/kraken.go`,
  `internal/parameter/visual/spirit.go`, `internal/parameter/visual/kraken.go`,
  `internal/system/composite.go` (`bodyOf`, `leaveBody`, `burstShell`),
  `internal/system/transient.go` (`updateKrakenBurns`)
- Design: [ECS §7](ecs-and-events.md#death-effects)

A species' end leaves an effect, and none has been judged in play. What to look at,
each with the smallest change that would answer it:

- A correction inside an effect. An effect is its instance's alone and outlives
  what raised it, so a species a correction brings back stands again while its
  scatter, flash or fade runs on under it, for half a second at most. Only a
  Kraken's burn ends as its Kraken returns (`KrakenBurn.Entity`). Play a fight over
  the divergence harness's four link shapes. If a body back over its own effect
  reads badly, a body's cells need a batch of their own naming the dead header,
  which the effect's system drops once that entity stands again, as
  `updateKrakenBurns` does.
- The timings, each a constant: a scatter's 350 ms and reach
  (`SpiritScatterDuration`, `SpiritScatter*`), the Kraken's burn of a second and
  how hard its legs thrash, draw in, crinkle and swing (`KrakenBurn*`), and how
  far apart a burst's shards stand (`ShellShardSpacing`).
- A chipped cell fades in the color its kind goes critical in, not the color it
  showed, so a healthy cell an attack takes whole turns that color as it goes.
  `bodyOf` reads one ground for the body, because a member's own shade is its
  renderer's (hit points, gradient, dither); reading it would take that shading as
  a shared read, as a Kraken's shape is.
- A tower's burst is in its type's glow color whether or not the tower was active.
- A glyph fading alone, a flash and a scattered part dim toward black and not
  toward what is under them, so their last frames are darker than the background.
  Mixing toward `RenderBuffer.BackgroundAt` would end them in it.
- A storm circle's end was offered a collapse instead of the burst: the dying
  circle shrinking to a bright core, about 40 lines in the storm renderer, with
  nothing for a pylon or a tower. The burst was chosen as one mechanism for all
  three. The collapse is worth building if a circle's burst reads too much like a
  pylon's.
- A cell still flashing or fading no longer holds off what is placed there. Each
  was an entity standing in its cell, which a glyph line's and a nugget's
  placement refuses (`PositionBatch.Commit`), so nothing landed on a dead glyph
  for the half second of its flash; the cells a batch holds stand nowhere. A line
  can now land on one, most often after a clear of the whole field, and shows the
  dead glyph's letter until the flash ends, as text drifting through a flash
  always did. No effect then decides where anything spawns. Two other forms were
  not built:
  - Holding the cells as before: a count a cell that the flash and fadeout
    systems keep and placement reads, built again after a world copy and a
    resize. A body that now flashes or fades (an eye, an expired swarm, a chipped
    cell) would hold its cells too, where a dead body held none. Worth building
    if lines landing in a field still flashing refill it too fast.
  - A glyph standing in a cell drawn over the effect there: one read a cell in
    the flash and fadeout renderers, as a scatter's part makes. The letter shown
    is then always the live one, but a body's flash goes unseen where text
    stands and a ploughed trail's flashes go out as text drifts under them.
    Worth building if the letter under a flash misleads typing.

### Show a species' machine state in a debug overlay

- Priority: P3
- Affected files: `pkg/machine/machine.go` (`Graph.States`, `Graph.Flags`),
  `internal/render/renderer`

`Graph.States` lists a graph's states and `Graph.Flags` a state's flags, for an
overlay that labels each instance of a species with its state and progress, as
`:graph` draws a route graph. A data species' per-state rows read `States` and
`Name` (`behavior.<program>.state.<State>`); nothing reads `Flags`: build the
overlay, or delete that read until one is wanted.

### Let a program's event name its own cell

- Priority: P3
- Affected files: `internal/system/behavior.go` (`EmitEvent`),
  `internal/fsm/std/payload.go` (`emitOrigin`)

A program's `EmitEvent` payload is fixed at load, and the scenario's `origin`
is the map's alone, so a body cannot drop loot where it stands, raise an effect
on its post or lay a map from itself. The smallest form: an `origin` on the verb
that also takes what `to` does (`self`, `stored`, `post`), moving the payload's
`x` and `y` as the scenario's does, the move shared by both.

### Let a data species' machine arm its mount

- Priority: P3
- Affected files: `internal/system/behavior.go`, `internal/system/mount.go`

A `[prototype.mount]` fires whenever a cursor is in range, a stunned or diving
host's included, where a storm circle's fires only while its state is flagged
`Armed` and never while held. The smallest form: an `Armed` flag in the
vocabulary and a `trigger = "armed"` on the mount, written to `Mount.Armed` each
step as the circles write theirs.

### Push a data species with the wind

- Priority: P3
- Affected files: `internal/system/environment.go` (`applyPlanarWind`)

`applyPlanarWind` accelerates each mobile species by its profile mass, and a data
species is none of them, so a wind leaves a raider where it stands though its
prototype has a `mass`. The smallest fix ranges the `Behavior` store in the Shared
pass with each prototype's mass from `Resources.Behaviors`, skipping a massless one.

### Count a data species' stalled transitions

- Priority: P3
- Affected files: `internal/behavior/compile.go`, `internal/system/behavior.go`

A program's transition whose candidates all fail retries or holds without a trace,
where the swarm counts its in `swarm.transition_stalls`. The swarm's observer is a
method value bound in `NewSwarmSystem`, reaching its counter through the system:
one graph, one counter. A program's graph is compiled before the generic system
exists, with no observer, so a per-program `behavior.<program>.transition_stalls`
needs an observer that reaches a counter bound later, in `NewBehaviorSystem` with
the lifecycle rows.

### Let a player mechanic or a program fuse composites

- Priority: P3
- Affected files: `internal/system/fuse.go`, `internal/event/type.go`,
  `internal/system/behavior.go` (`EmitEvent`)
- Design: [D-3](domain-design.md#d-3--cross-the-smallest-shared-outcome)

A fusion takes unit composites only from a Shared request, one a region raises on
every instance, each consuming them alike. A participant's own request, pushed
player, takes its drains alone and fails naming a composite, and a program's
`EmitEvent` takes no stamped event, so neither a player mechanic nor a program can
fuse composites. A player mechanic would cross the Shared outcome as a bus artifact
naming the composites and the target's landing, barrier-bound since it retires
Shared entities; a program's request would push the request Shared, as a region's
does.

## Environment

### Carry the cursor with the text it is on

- Priority: P2
- Affected files: `internal/system/environment.go`, `internal/system/cursor.go`

Glyph drift moves the line under a cursor but not the cursor, so a player chases
moving text. Carrying it would move each local cursor with the glyph it stands on,
as a Player-domain cursor write per layer step; a peer's cursor would follow
through its owner's move, never through the drift.

### Decide what a delete removes under a drift stack

- Priority: P2
- Affected files: `internal/system/typing.go`

`handleDeleteRequest` removes every player glyph in its range, including farther
layers hidden under a cell's top glyph, while typing, rendering and motions act
on the top glyph only. Deleting only each cell's `TopGlyphAt` would make a
delete remove exactly what is shown.

### Refresh motion markers as glyphs drift

- Priority: P2
- Affected files: `internal/system/motion_marker.go`

Markers are recomputed on cursor movement and mode changes, so under a drift they
sit on cells the glyphs have left. Recomputing after a drift step, which the
environment could announce as a local event, would keep them on the text.

### Widen what drifts

- Priority: P3
- Affected files: `internal/system/environment.go`, `internal/parameter/environment.go`

Composite members (gold), particles, nuggets and species stay put; a side-scroller
may want some of them carried with the nearest layer. Decay and blossom change a
glyph's level, so a glyph they touch changes layer and speed mid-flight. In 256
colours normal and bright glyphs share one console entry, so only the dark layer
reads as farther; a distinct attribute for bright would separate the three.

## Rendering

### Zoom the window and Android out under the atlas

- Priority: P3
- Affected files: `tool/font-atlas/font-atlas.py`, `internal/asset/font`,
  `pkg/glyph/atlas.go` (`Atlas.Cell`, `Zoom.Scale`), `pkg/window`, `android/game`
- Guide: [Platforms §8](multi-platform.md#8-adding-another-renderer-or-android-host)

The desktop window and Android draw one 12x24 atlas, stretched up and never
down, so their zoom stops at it: a window at `-scale 1` only zooms in, 24 to 40,
where a page, which draws its glyphs at any size, goes down to 4. Ship the atlas
at smaller cells too, the generator taking a size, and draw a zoom from the
largest one its cell holds, so a zoom out shows more map with whole glyphs.

### Console follow-ups

- Priority: P3
- Affected files: `internal/mode/console.go`, `internal/engine/overlay_geometry.go`,
  `internal/render/renderer/overlay_console.go`, `internal/mode/overlay.go`

The console landed ([Input §9](input-and-modes.md#9-command-mode)). Open:

- `StatusAnnounce` has its rank and colours, but nothing posts one: a server's word
  to its players needs a message on the wire first.
- A finger scrolls the console, as it does help, only on its one-cell scrollbar; a
  drag over its rows would suit a phone better.
- Help and telemetry still centre on the whole screen under a touch keyboard, whose
  covered rows only the console keeps above (`ComputeConsoleGeometry`).

The options Lixen did not take (2026-11-14), each with what would reopen it:

- Placement: a drop-down from the top, as Quake's, or help's centred window. The band
  at the foot keeps the newest line beside the status bar it came from and leaves the
  pinned HUD at the top-left in view. Reopen if a scenario needs its map's foot seen
  while the console is open.
- A line of the console's own for commands: command mode's line, opened under it,
  already edits, completes and recalls, and a second would be two mechanisms for one
  job. Reopen if the console comes to take input that is not a command, as chat.
- On a touch screen, a key strip button (the page has a strip, Android none, so the
  hosts would differ) or a tap on the bar's message (the hosts' triple tap reaches
  the run tap by tap, so telling a lone tap apart waits 500 ms). The menu's Console
  row and `:console` work on every host. Reopen when Android has a key strip.
- History kept in the state directory, as a shell's: its lines carry addresses
  (`:session`) and what the run did, which the address privacy rule keeps off the
  disk by default. Reopen if players ask for it across launches, behind a setting
  that is off by default.
- Under a touch keyboard, hanging from the screen's top while it is up (Go alone,
  but landscape still hides its foot), or staying under it.

### Configuration menu follow-up

- Priority: P2
- Affected files: `internal/mode/config_menu.go`, `internal/paths/settings.go`
- `[play]` persists the audio channels, sfx volume, auto-fire, mouse, pointer
  following and the HUD, and a scenario change carries them over. What `:new`
  keeps beyond those (rate, pins, standing breakpoints) a restart or scenario
  change still drops. A Music volume row under Sound, stepped as Effects volume
  is and kept as `[play] music_volume`, needs a music gain in the mixer with an
  authoritative readback first; the row then follows `sfx_volume`'s shape on
  every host, the native menu's stepper included. Keymap, backend, paths and
  colour depth remain startup settings; `:d break` stays on the command line,
  since a row cannot take its arguments. A Keymap row choosing among the built-in
  table and the keymaps a name finds (`vim`) would reach them where no flag does,
  on Android and for a browser's player, and needs the key table rebuilt in a run.

### Camera edge follow-ups

- Priority: P3
- Affected files: `internal/engine/resource.go` (`EdgeCells`, `FollowCamera`),
  `internal/engine/world.go` (`PushPointerMove`)

One `[play] camera_edge` holds for every input ([Rendering §2](rendering.md#2-render-context-and-coordinates)).
The options Lixen did not take (2026-11-14), each with what would reopen it:

- The edge as a share of the shorter side, as far from every side: a wide screen's
  long sides get the short side's distance, so the view looks less far ahead there
  than the area's side gives. Reopen if wide screens feel cramped beside tall ones.
- The same share of each side: simplest, but a wide screen's sides sit further in
  than its top and bottom, so the view follows sooner sideways than up and down.
  Reopen if a percent should read as a share of each side.
- A narrower edge for a pointer than for keys, as before: a key after the mouse moved
  the view by the gap when the cursor sat between the two. Reopen with a camera that
  eases to its new place rather than jumping, which would smooth the change of hands.
- A cap on how far one pointer report scrolls: a pointer pushed into the edge scrolls
  by its depth each report, so a slow pass over the 15% edge scrolls about 280
  columns on a full terminal, against 36 over the old 8-column pointer margin. A cap
  leaves the cursor inside the edge, so the next key jumps the view again. Reopen if
  the edge scrolls too hard under a mouse or a finger.

### Give each shipped `[play]` key a comment of its own

- Priority: P3
- Affected files: `internal/asset/vif.toml`
- The `[play]` keys before `camera_edge` share one block above the table's header,
  which an upgrade never brings to a player's copy that has the table, so a key
  added there arrives bare and the block goes stale ([Filesystem §3](filesystem-layout.md#3-resolution-policy)).
  A comment line above each key, the block cut to what the table is, carries each
  later key's explanation with it; a copy keeps its old block until the player
  edits it.

### Drop a settings file's keys in one read

- Priority: P3
- Affected files: `lixenwraith/toml` (`Document.Delete`, `Document.Set`),
  `internal/paths/settings.go` (`UpgradeSettings`), `internal/input/keyconfig.go`
  (`UpgradeKeymap`)
- Each `Delete` reads the document again, so an upgrade dropping n keys costs n
  reads: a `vif.toml` or keymap holding 3000 unknown keys takes seconds to start.
  A `Delete` of many keys in one edit makes it one read. No file vif writes comes
  near; one a player pastes another program's settings into could. A `Set` over
  tables written under headers costs the same, a read a statement it sets: 300
  of 3,000 tables changed in one array took 2.7 s.

### Write a hard-linked settings file in place

- Priority: P3
- Affected files: `internal/paths/settings.go` (`FileStore.Store`)
- A store replaces `vif.toml` or a keymap by rename, which leaves a hard link's
  other names on the old file. Writing in place keeps every name but tears the
  file for a run reading it, or for good when the write is cut short; a link count
  above one could choose it, with the tear the price. The rename stays, as the
  write before the upgrades did.

### Keep what a system root's vif.toml names shared in the player's copy

- Priority: P3
- Affected files: `internal/paths/settings.go` (`FileStore.Load`),
  `internal/resource/resolve.go` (`Options.ApplySettings`)
- A system root's `vif.toml` naming a `[paths] root` or `keymap` outside the
  system roots makes it another's, read and never written. The user root's copy
  its first change makes names it too, as the player's own, so a run writes a
  keymap there back up to date where it may. The copy could leave out what the
  seed's `[paths]` names, or mark it; one inside a system root stays shared
  (`paths.Shared`).

### Let a run on a shared config root keep its changes

- Priority: P3
- Affected files: `internal/paths/settings.go` (`SettingsFor`, `LoadSettings`)
- A `-config-dir` naming the checkout's `wad/` or a system root reads that root's
  `vif.toml` before the user root's, while the copy the first change makes in the
  user root takes every later one, so such a run starts from the shared file each
  time. Reading the user root's copy first when there is one would keep them, at
  the price of the copy shadowing what `-config-dir` names; the shared file stays
  unwritten.

### Review the settings store's write path

- Priority: P1
- Affected files: `internal/paths/settings.go` (`FileStore.Store`, `storeTarget`),
  `internal/paths/owner_unix.go` (`keepOwner`), `tool/vif-settings`
- A start rewrites the player's own `vif.toml` and keymap, and `make install` the
  installed ones through `tool/vif-settings`, as root under `sudo`: by rename,
  through a link, keeping the file's mode and owner. No review of that path's
  safety has been finished. One reading the code, with tests only in temporary
  directories, closes it, asking whether the file a link names can change between
  `storeTarget`'s stat and the rename, what mode and owner the temporary file has
  before its `Chmod`, what `keepOwner` does in a run as root, and what a link into
  a directory other users may write leads to.
- Seen in an audit, read in code alone: `-check` says a run writes a file back by
  the file's type and mode (`WriteBack`), where `Store` also needs its directory
  writable, so on a read-only mount the two disagree; two runs saving at once each
  rename over the other's key, `saving` being one process's; and a crash between
  `CreateTemp` and the rename leaves a `vif.toml.<n>.tmp` nothing removes.

### Review when a start writes the settings and what it shows

- Priority: P2
- Affected files: `cmd/vif/main.go` (`upgradeOwn`, `sayNotice`),
  `internal/app/play.go` (`player.begin`), `internal/paths/settings.go` (`Shared`,
  `resolved`), `internal/parameter/audio.go`
- The last fixes to the upgrade have tests but no second reader, as their review
  stopped before it reported: the upgrade runs once the invocation is validated
  and its site session taken, so a refused run writes nothing; a replay, script or
  bot shows the notice on the status line; a shared root is found through links;
  and the `[audio] buffer_ms` bounds sit in `internal/parameter`, keeping headless
  and audio-free builds off the audio library. An independent reading closes it.

### Check in CI that a headless build links no audio

- Priority: P2
- Affected files: `.github/workflows/test.yml`, `Makefile` (`verify`)
- `make verify` fails when a `vif_headless` build of `cmd/vif` imports
  `internal/render` or the `lixenwraith/audio` package. CI builds the profile but
  never asks, so it passes a change that links either, as the `[audio] buffer_ms`
  check once did. The same `go list -deps` step in `test.yml` closes it.

### Use sixteen console backgrounds on Linux

- Priority: P3
- Affected files: `internal/render/console.go`

The framebuffer console shows a bright background under SGR 5, which vgacon blinks instead; telling them apart would give Linux FreeBSD's sixteen backgrounds.

### Move the browser and Android frame pipeline into terminal

- Priority: P2
- Affected files: `pkg/browser`, `pkg/glyph` (`Frames`, `Raster`, `Texture`),
  `android/game/view.go`, `web/terminal.js`, `web/worker.js`, lixenwraith/terminal

What turns a cell frame into what a page or an Android view draws (the frames a
drawer takes, the texture, the raster fallback, the page's bridge and its key and
pointer events) lives in vif. Move it into lixenwraith/terminal as backends of its
own, so another program renders in a browser or on Android through the same
pipeline without wrapping vif; vif then takes them from terminal. For its own
session.

## Audio

The audio library's own items are in [its todo](https://github.com/lixenwraith/audio/blob/main/doc/todo.md),
and those of stapes (formerly soundlab), the rip's among them, in [its
own](https://github.com/lixenwraith/stapes/blob/main/doc/todo.md). An item here
is vif's part of a change; a part the library or stapes makes is an item there,
linked from it.

### Bring stapes to Android (stapes mobile)

- Priority: P1
- Affected files: `android/game`, `android/app`, `android/go.mod`,
  `doc/multi-platform.md`

Stapes runs on Android in-process, as the game does, over the same engine and
the user root's `audio/` documents, so a pattern written on the phone plays in the
phone's game. The foundation is in place: the `stapes` package holds the session,
documents, verbs and TUI, `NewSession` takes the host's speakers as its sink
(`Options.Sink`), and `RunTUI` runs over any `terminal.Terminal`, the host posting
`EventClosed` to end it; the Android host already turns taps, drags, keys and the
system keyboard's text into `terminal.Event`s and flushes cell frames through
`glyph.Frames`. The TUI's side, taps and a layout a phone held portrait fits, is
[stapes' todo](https://github.com/lixenwraith/stapes/blob/main/doc/todo.md#play-the-screen-by-touch).
What remains here:

1. A `terminal.Terminal` over the Android view (its grid as `Size`, its event
   queue as `PollEvent`, `glyph.Frames.Flush`) runs it, started by a second bound
   entry beside `game.Start` that hands `NewSession` the `Speaker` as its sink;
   the start screen offers stapes beside the game.
2. The system keyboard serves `:` and the prompts, as in the game, and the pinch
   zoom gives the views their 60×16 cells on a phone held portrait.
3. Files: the verbs name paths, which reach stapes' root and the user root's
   `audio/` in the app's storage; a `.mid` or a `.wav` another app holds comes
   through the system's picker, copied into the user root's `audio/` first, and
   a written file goes back out through it ([Multi-platform
   §8](multi-platform.md#stapes-files)).

### Rip live on Android

- Priority: P2
- Affected files: `android/game`, `android/app`
- Prerequisite: [Bring stapes to Android](#bring-stapes-to-android-stapes-mobile)

The library rips a recording as it plays (`RipStream`: blocks of samples in,
each bar frozen once two follow it from the sixth bar on, `Final` the whole rip
once the recording ends), and stapes is to show the rip being made and save the
song ([its todo](https://github.com/lixenwraith/stapes/blob/main/doc/todo.md#show-a-live-rip-as-it-is-made)).
What remains here is the microphone: read in the app (`AudioRecord`, behind the
`RECORD_AUDIO` permission asked at first use) and handed to stapes' session as
its source, as the `Speaker` is its sink.

### Render a conductor timeline offline

- Priority: P3
- Affected files: `tool/vif-log`, `internal/system/music.go`

`RenderTimeline` plays a timeline of slot patterns offline and stapes' `song`
writes a `SongDef` through it; the conductor's own timeline, patterns by tier from an APM
trace, is what still lacks a path to it. The offline conductor render that
compared bass voicings for the music-smoothing review, an APM timeline through
the sequencer with no device, is a test-only
harness in a scratch repository. `AudioEngine.RenderMusic` now renders placed
patterns offline; a verb of vif's, since stapes imports nothing of the game,
would drive an APM timeline through the conductor's tier and tempo policy over
it and write the WAV.

### Play a song in the game

- Priority: P3
- Affected files: `internal/system/music.go`

The conductor plays tiers, not bars. A level that wants a fixed piece would play
a `SongDef` through the sequencer as stapes conducts one, each bar's patterns
scheduled while the bar before plays.

### Add binaural beats to the music

- Priority: P3
- Affected files: `internal/parameter/music.go`, `internal/system/music.go`,
  `internal/paths/settings.go`, `internal/mode/config_menu.go`,
  `internal/mode/router.go`, `internal/event`, `go.mod`, `doc/audio.md`,
  `doc/config-menu.md`, `doc/vif.6`
- Guide: [Audio §2](audio.md#2-adaptive-game-conductor)
- Prerequisite: the library's bed ([its
  todo](https://github.com/lixenwraith/audio/blob/main/doc/todo.md#give-the-mix-a-binaural-bed))

The music's tempo already follows the player's pace (100 to 200 BPM off the
five-second APM). A binaural beat would add a sensation of that pace: a tone
of `f − Δ/2` in the left ear and `f + Δ/2` in the right, heard as a beat at
`Δ`, which the library is to mix under the music (`SetBinaural`), its todo
weighing where. Off by default, as a setting with three modes:

- **off**: today's output, byte for byte (L equal to R);
- **fixed**: a preset's `Δ`, whatever the pace; presets in one table in
  `internal/parameter` (a row each), first rows `relax` 10 Hz (alpha), `focus`
  15 Hz (low beta), `hyper` 25 Hz (high beta);
- **dynamic**: `Δ` follows the APM, by an algorithm still to choose (below).

Dynamic `Δ`, candidates, each glided at most about 0.5 Hz a second:

- **follow**: `Δ` rises with the APM on the tempo's own square root, from
  `relax` at rest to `hyper` at the ceiling. Reinforces the pace, as the tempo
  does; it may also feed a frantic player more.
- **tempo-locked**: `Δ = k·BPM/60`, so it inherits the conductor's curve and
  slew with one constant; `k = 8` (a 32nd note) runs 13.3 to 26.7 Hz over 100 to
  200 BPM, all beta, `k = 4` 6.7 to 13.3 Hz, theta to alpha. Coherent with the
  rhythm, but it never leaves its band.
- **by tier**: one `Δ` a tier, a row each in the preset table, through
  `TierTracker`'s hysteresis; steps the glide smooths, five values to tune.
- **counter**: `Δ` falls as the APM rises, steadying a busy player against the
  rising tempo; the two may read as at odds.

What it takes: `[play] binaural = "off" | "fixed" | "dynamic"` and
`binaural_preset`, validated in `Play.check` as `auto_fire` is and saved by the
one settings rule; a Sound row in the configuration menu (`kindChoice`) and the
native menu's, the preset row live under fixed; a command and an event pushed
`OriginDevice` as the mute is, so the journal carries none and a replay plays
the viewer's setting, dynamic following the replayed APM; `MusicSystem` sends
the target each update and `music.binaural` shows the mode and `Δ` on the music
card. Decided with it: whether the bed rides the music bit of the audio mask and
the `music` system's gate (simplest, no new bit) or a toggle of its own, and a
level of its own or a fixed one under the music. The beat needs headphones:
through speakers the ears share both tones and it becomes a quieter monaural
beat, and a phone's single speaker loses it; the row's description says so, the
same on every host. The evidence that a beat entrains anything is mixed: the
manual describes it as a sound, not an effect. The dynamic mapping is tested in
`parameter`'s music test. New terms (binaural beat, bed, carrier) go in both term
indexes when they land.

## Engine

### Trim what a tick spends outside the position index

- Priority: P2
- Affected files: `internal/system/interaction.go`, `internal/engine/store.go`,
  `internal/engine/world.go`, `internal/event/queue.go`
- Guide: [Position §5](position-package.md#5-costs)

A td tick with sixteen cursors on its 500×250 map (`TestTickProfileTD16`, sixteen
runs merged) is led by costs outside the index, the math and the physics. Each
share is measured and none of its code changed:

- A species' contact with the cursors (`CheckCursorOverlaps`, `overlapOf`), 15%:
  every entity reads each roster slot's cursor and shield again before it tests
  its cells against them. Reading the roster once a tick is the smallest step.
- The stores' entity lookup (`sparseIndex.get`), 9.5% across the systems, a
  third of it under a position read.
- The route graph (`RouteSolver.Compute`), 12%, and the maze a reset lays again
  (`executeBatchSpawn`), 15%, a third of that `World.componentMask`, a Go map
  written once a component. A reset's tick takes about 20 ms, 15 of them the wall
  system's event.
- Three allocations every tick: a `Stamp` in `EventQueue.BeginTick`, one in
  `NextBoundary`, and the system list the membership's off-tick drain copies
  (`World.Systems`).
