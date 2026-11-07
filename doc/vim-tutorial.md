# Vim tutorial scenario

A working doc: what the tutorial is, what its foundation already holds, what is
still to lay, and the decisions it waits on. The todo item points here.

## 1. What it is

A scenario, `wad/scenario/vimtutor`, that teaches vim's motions the way `:Tutor`
does: one lesson at a time on a responsive map, combat off. Each lesson is laid
out the same way, top to bottom:

- a heading in the small splash face (`EventSplashTextRequest`, `font = "small"`):
  the key or keys to press, as `press w`;
- an explanation of two or three lines, laid as scenery glyph lines
  (`EventGlyphLineRequest`, `scenery = true`), which nothing types or deletes;
- a practice line, laid as typeable glyphs, and the cursor moved onto it
  (`EventCursorMoveRequest`);
- a status-bar prompt (`EventMetaStatusMessageRequest`) for what the lesson waits
  on, repeated as an alert when the player stalls.

A lesson ends when its state's guard sees the result: the cursor at a cell
(`EventCursorMoved` with `PayloadIntCompare` on `x` and `y`), the practice line's
glyphs gone (`StatusIntCompare` on a glyph count, or the deletes typed), a mode
entered (`EventModeChanged`), or a captured key pressed (`EventInputPressed`).
The map is laid from `map_width` and `map_height` (`ConfigToVar`, then the
arithmetic actions): a heading centred at `(map_width - 6 * len) / 2`, lessons
re-laid when the state is entered again.

## 2. The first round

`wad/scenario/vimtutor` runs eight lessons and a free practice; `vif -s vimtutor`
plays it, `vif -s vimtutor -check` validates it with its corpus. What it stands on:

- Two splash faces (`asset.SplashFaces`): the large one as before, and a small one
  six by twelve pixels, drawn two pixel rows a cell with half blocks and a cell a
  pixel on a text console ([Rendering §7](rendering.md#7-layer-inventory)).
- `EventSplashTextRequest` writes a heading or an answer at a cell;
  `EventGlyphLineRequest` lays a typeable or scenery line, under the cursor too;
  `EventGlyphClearRequest` removes the glyphs in a rectangle at once, which a
  lesson's exit uses: dust-all would turn the text into dust that orbits the
  cursor and hides the next lesson. The glyph system lays and clears while it is
  disabled, so no corpus line lands on a lesson ([FSM
  reference](fsm-reference.md#splash-text-and-glyph-lines)).
- A scenario carries its corpus: the entry's `content` names TOML corpus files,
  in its container under its digest, which the run types in place of its roots'.
  The tutor's is code in blocks of two to five lines.
- A corpus block is laid whole, its lines on consecutive rows from one column,
  so `%` finds a bracket's pair across rows and `{` `}` hop between blocks; where
  no slot holds it, line by line as before.
- `EventKeyCaptureRequest` takes `modes` and `idle`: the tutor captures `s`, and
  in its last lesson `a p o O u m y c r n`, in Normal and Visual with nothing
  pending, so `fa` and `qa` still take their letter and Insert types it.

A lesson is a state under `Lesson`, whose exit clears it (`Clear`: every glyph,
the heading and the answer, under a white flash) and whose entry lays the next.
The map floor is 80 by 24: the heading at row 1, the notes at rows 8 to 10, the
practice line at 13, an answer at 16, all from column 4.

## 3. The next round

- **More lessons**: `/` `n` `N`, `gg G M m` and the `g` corners, `o O [ ]`, `gl`
  and the colour markers, `q` and `@` macros, `d` with a motion and `D`, `v`.
- **Re-layout on resize**: a lesson laid for one map stays where it was laid; a
  transition on `EventScreenResize` through a relay state would lay it again,
  since a state re-entering itself runs no entry actions.
- **Centring from the map's edge**: a text or line placed from the right or the
  bottom edge (a negative origin, as the todo's spawn-from-the-edge item asks)
  would spare the scenario its arithmetic.
- **The magnifier**: a cursor resting on a glyph shows it large some way off, which
  a lesson could do without; the splash system has no switch for it yet.
- **Sound**: a chime on a lesson cleared and a buzz on a taken key, as simon cues.

## 4. Decisions taken

- The vim keys vif lacks or reads its own way are not added in code for now: the
  tutor captures them, answers each press with the key's meaning here, and says
  they cannot be tutored yet. The todo item on the cheap ones (`c`, `*` `#`, `A`
  `I`, `.`, `r`) and on the tutor overriding `a` and `p`, which have nothing to
  fire or pause there, is the next step.
- The practice text is the scenario's own corpus, not the run's.
- The ping grid stays; the tutor's cursor has no shield, so Visual mode pings from
  the cursor alone.
