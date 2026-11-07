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

## 2. Foundation laid

- Two splash faces (`asset.SplashFaces`): the large one as before, and a small one
  six by twelve pixels, drawn two pixel rows a cell with half blocks and a cell a
  pixel on a text console ([Rendering §7](rendering.md#7-layer-inventory)). A
  countdown takes `font`, and a large one with no clear slot takes the small face.
- `EventSplashTextRequest`: a line of splash text at a cell, replaced by the next
  text at that cell, cleared by an empty one, or timed out.
- `EventGlyphLineRequest`: a line of glyphs laid at a cell, typeable or scenery,
  honoured while the glyph system is disabled, so a scenario lays its own text and
  no corpus line lands on a lesson ([FSM
  reference](fsm-reference.md#splash-text-and-glyph-lines)).
- The embedded primer (`internal/asset/content/tutorial.toml`) names where vif
  reads a vim key differently, so a player meets the differences before the
  tutorial exists.

## 3. Still to lay

- **A scope on the key capture.** `EventKeyCaptureRequest` holds a key in Insert
  and over pending grammar too, so a tutorial that captures `a` to answer the press
  could not let the player type an `a` in the practice line or find one with `fa`.
  The payload needs the modes it holds in (`modes = "normal"`), and the parser
  must leave a key alone while a count or an operator is pending.
- **Combat off.** `only_systems` naming the cursor, timer, death, splash, glyph,
  energy and heat systems, the gun taken on `EventCursorSpawned`
  (`EventWeaponRemoveRequest { entity, weapon = "gun" }`), so Enter, Space and
  auto-fire do nothing, and no species spawned.
- **The lesson count.** The lessons, in order: `h j k l`; `w b e` and the big
  forms; `0 ^ $`; `f t ; ,`; counts; `x`, `d` with a motion, `dd`, `D`; `i` and
  typing the line; `/`, `n`, `N`; `gg G M m` and the `g` corners; `o O [ ]`, the
  column finds vif adds; `gl` and the colour markers; `q` and `@` macros. Each is
  one state; a lesson the player wants to skip has a status message naming the
  key that skips it (a captured key, `s`, in Normal only once the capture is
  scoped).
- **Keys vif binds differently** (`a` auto-fire, `p` pause, `o` `O` `[` `]` column
  finds, `m` the middle column, `H J K L` half pages, `u` cursor undo, Enter, Space
  and Backspace fire) and keys it binds to nothing (`c`, `y`, `r`, `A`, `I`, `P`,
  `.`, `*`, `#`, `~`). The lesson text says so where a lesson meets one; what else
  to do with them is the decision below.
- **Centring from the map's edge.** A text or a line placed from the right or the
  bottom edge (a negative origin, as the todo's spawn-from-the-edge item asks)
  would spare the scenario the arithmetic; until then the macros compute it.

## 4. Decisions

1. **Which vim keys to add in code**, rather than script or drop. In order of
   cost: `c` with a motion, `cc` and `C` (the delete operator's grammar, then
   Insert); `*` and `#` (search for the glyph run under the cursor, which search
   mode already does); `A` and `I` (Insert at the line's end or first glyph);
   `a` (Insert after the cursor, which clashes with auto-fire: move auto-fire to
   `:auto` and another key, or leave `a` as it is); `r` (retype the glyph under the
   cursor, which needs a glyph replace event and a rule for whether it counts as
   typing); `.` (the last change recorded in the input machine). `y` and `p` have
   nothing to yank or put in a glyph field, and `p` pauses: drop them, as the
   primer says.
2. **How a lesson answers a key it cannot teach**: capture it and say so on the
   press (needs the capture scope above), or leave the key its vif meaning and
   name the difference in the lesson text alone.
3. **Whether the practice text is the scenario's own** (laid lines, as planned) or
   a corpus the scenario carries (`content/` in its container, under its digest),
   which the todo's authoring item leaves open. Laid lines need nothing new.
