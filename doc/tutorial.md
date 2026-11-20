# Tutorial scenario

A working doc: what the tutorial is, what its foundation already holds, what is
still to lay, and the decisions it waits on. The todo item points here.

## 1. What it is

A scenario, `wad/scenario/base/tutorial`, that teaches the game and its vi-style
moving, typing and editing the way Vim's `:Tutor` does: one lesson at a time on a
responsive map, combat off. Each lesson is laid out the same way, top to bottom:

- a heading in the small splash face (`EventSplashTextRequest`, `font = "small"`):
  the key or keys to press, as `press w`;
- an explanation of two or three lines, laid as scenery glyph lines
  (`EventGlyphLineRequest`, `scenery = true`), which nothing types or deletes;
- a practice line, laid as typeable glyphs, and the cursor moved onto it
  (`EventCursorMoveRequest`);
- a status-bar prompt (`EventMetaStatusMessageRequest`) for what the lesson waits
  on, repeated as an alert when the player stalls.

A lesson ends when its state's guard sees the result: the cursor at a cell
(`EventCursorMoved` with `PayloadIntCompare` on `x` and `y`), an edit asked for
(`EventDeleteRequest` by its range, `EventGlyphReplaceRequest` by its text,
`EventGlyphPutRequest`), a glyph typed (`EventCharacterTyped` by its `char` and
cell: a line's last, never a count of glyphs, which one deleted another way would
leave short for good), a mode entered
(`EventModeChanged`), or a captured key pressed (`EventInputPressed`)
([FSM reference](fsm-reference.md#splash-text-and-glyph-lines)). The map is laid
from `map_width` and `map_height` (`ConfigToVar`, then the arithmetic actions): a
heading centred at `(map_width - 6 * len) / 2`, lessons re-laid when the state is
entered again.

## 2. What it holds

`wad/scenario/base/tutorial` runs twenty-two lessons and a free practice; `vif -s
tutorial` plays it, `vif -s tutorial -check` validates it with its corpus. In order:
`h j k l`, `w b e`, `0 ^ $`, `f t ; ,`, counts, `x dd`, `dw D`, `i` and typing,
`A I a`, `o O`, `c cc`, `r ~`, `.` with its Insert text, `y p P`, `u` and
Ctrl-R, `%`, `/ n`, `* #`, ``m ' ` ``, `gg G M`, `v` and its `d`, then what a game
makes of the keys the tutorial gave back to vi. What it stands on:

- Two splash faces (`asset.SplashFaces`): the large one as before, and a small one
  six by twelve pixels, drawn two pixel rows a cell with half blocks and a cell a
  pixel on a text console ([Rendering §7](rendering.md#7-layer-inventory)).
- `EventSplashTextRequest` writes a heading at a cell; `EventGlyphLineRequest` lays
  a typeable or scenery line, under the cursor too; `EventGlyphClearRequest`
  removes the glyphs in a rectangle at once, which a lesson's exit uses: dust-all
  would turn the text into dust that orbits the cursor and hides the next lesson.
  The glyph system lays and clears while it is disabled, so no corpus line lands
  on a lesson, and a clear ends the change history, so `u` takes back nothing of
  the lesson before.
- A scenario carries its corpus: the entry's `[content] files` names TOML corpus
  files, in its container under its digest, which the run types in place of its
  roots'. The tutorial's is code in blocks of two to five lines.
- `[content] blocks = true` lays a corpus block whole, its lines on consecutive
  rows from one column, rows nothing else holds with an empty one each side, so
  `%` finds a bracket's pair in its block and `{` `}` hop between blocks; a block
  no slot holds is passed over, and every scenario that leaves the key unset
  spreads lines as before. A block is five lines at most, the corpus's limit, so
  no longer one is split.
- `EventKeyCaptureRequest` takes `modes` and `idle`: the tutorial captures `s` and
  `S` in Normal and Visual with nothing pending, so `fs` and `qs` still take
  their letter and Insert types it.
- The entry's `[keymap.normal]`, `[keymap.normal_keys]` and `[keymap.operator]`
  bind, over the player's keymap, `a` to `mode_append`, `p` and `P` to the puts,
  `o` and `O` to `mode_open_below` and `mode_open_above`, `u` and Ctrl-R to
  `undo_change` and `redo_change`, `m` to `mark_set`, and the backtick, the
  console's key, to `mark_jump`, after an operator too, so `` `a `` returns to the
  marked cell and `:console` opens the console
  ([Input §10](input-and-modes.md#10-keymap-override-files)). The vim keymap
  binds each the same, so `-k vim` plays any game on the keys taught here, and
  the last lesson says so.

A lesson is a state under `Lesson` whose own exit clears it (`Clear`: every
glyph and the heading, under a white flash; the parent's exit runs only when the
last lesson leaves) and whose entry lays its text; `s` goes to the next and `S`
to the one before, and a lesson in steps nests each further step as a child,
which keeps the text laid. A lesson that ends on a typed glyph ends in a step
that waits for esc, so the next starts in Normal; `.`, which ends its replay in
Normal, passes it at once. `Stand` places the cursor a tick after the entry,
behind the step right that typing a lesson's last glyph still owes. The map
floor is 79 by 20, the view an 80 by 24 terminal shows, so no lesson there
scrolls and `G` leaves the next heading in sight: the heading at row 1, the
notes at rows 8 to 12 and the practice lines at rows 14 to 17, all from column 4
and ending by column 78. The Visual lesson practises on five rows and turns the
shield system on for itself alone, so the ping bounds have a reach to show.

The lesson's text is each instance's own, laid by `reconcile = true` actions, so a
join or a reload that installs a later lesson lays that lesson's text in place of
the first one its own boot laid. Lesson 1's x is a typeable glyph, which `$`, `w`
and `e` reach as they reach a word.

The tutorial seats one player (`max_players = 1`) and takes no bot. Its lessons end
on a player's edits, typed glyphs and mode changes, local events that move the
machine only where that player's instance holds it (D-20). A guest of a server
holding it moves on by the cursor, `s` and `S` alone; `:leave` plays on alone,
where every lesson ends.

## 3. The next round

- **More lessons**: `[ ]`, `gl` and the colour markers, `q` and `@` macros, and
  `N`, `M`, `C`, `X`, `P` and `s S` practised rather than told (`s` and `S` skip
  and go back here).
- **Re-layout on resize**: a lesson laid for one map stays where it was laid; a
  transition on `EventScreenResize` through a relay state would lay it again,
  since a state re-entering itself runs no entry actions.
- **Centring from the map's edge**: a text or line placed from the right or the
  bottom edge, or the centre (an `origin` on the request,
  [FSM reference](fsm-reference.md#event-emission)), would spare the scenario
  its arithmetic.
- **The magnifier**: a cursor resting on a glyph shows it large some way off, which
  a lesson could do without; the splash system has no switch for it yet.
- **Sound**: a chime on a lesson cleared, as recall cues.

## 4. Decisions taken

- A vim key whose letter a game reads its own way has an action no default key
  binds, and the tutorial binds it through its keymap, over the player's, rather
  than through a capture answering the key.
- A put glyph is the glyph yanked, rune, colour and level, and scores as any.
- Visual's selection is the ping bounds, and `d`, `y`, `r` and `~` stay in
  Visual; vim's block insert has no counterpart, since Insert lays no text, so
  `I` and `A` leave Visual for Insert as elsewhere.
- The practice text is the scenario's own corpus, not the run's, and block
  placement is the tutorial's choice: the main scenario and `td` spread lines as
  before.
- The tutorial's cursor has no shield outside the Visual lesson, so Visual there
  moves and edits as Normal does.
- The tutorial runs no particle system: Space, a game's special fire, which auto
  fire presses too, would turn the practice code's dark glyphs to dust.
