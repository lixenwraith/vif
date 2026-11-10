# TOML Macros

Decision record and authoring reference for macros in vif's TOML documents.
`lixenwraith/toml/macro` is the one expander; scenarios with their behavior
programs, bot graphs and the audio banks (`sounds.toml`, `music.toml`) use it
today, and the map editor is meant to.

## 1. Decision

**Context.** Scenario regions repeat whole shapes with a few values changed. Before
this change `wad/scenario/base/td` spent 716 region lines on about a dozen shapes: a
spawn request awaited with a capture and a timeout fallback (fifteen states), a
gateway attach (nine), guarded retargets on a tower's loss (fourteen actions), and
flank escalation mirrored per side. Copies drift, and a reviewer cannot tell a deliberate
difference from a stale one. Behavior programs
([Behavior machines §6](machine-design.md#6-data-species)) share the need, and so
will the map editor, whose placeable blocks are the same idea.

**Decision.** Macros are data in the document, expanded on the parsed tree
between parsing and `toml.Decode`:

- a macro is a `[macros.<Name>]` table with typed parameters, defaults, a doc
  string and a body, or in the body's place the macro it is written over and
  the arguments it fixes;
- an invocation is any table carrying a `macro` key, replaced by the body with
  the arguments substituted;
- expansion is a pure function of the documents read, bounded, and deterministic.

**Consequences.**

- Nothing downstream of decode changes. The FSM, its validation, the schema and
  the runtime see plain tables, so a macro cannot express anything the dialect
  cannot.
- A scenario's macro files are scenario files: they enter `ScenarioFiles`, the
  digest, the size bounds and the transfer to a joiner. Equal bytes expand to
  equal graphs on every peer, and no identity field is added.
- Errors name the invocation path through every macro involved, for example
  `td_main.toml: states.TdSpawnQ1: macro "QuadrantTower": with.fallback_actions[0]:
  macro "Retarget": unknown argument "x"`; a macro over a base says `with` where
  one with a body says `body`. A key no bare key spells is quoted, and what an
  error repeats of a file is cut to 32 bytes and its path to 64 steps.
- An editor reads and writes macros and invocations as ordinary tables with the
  same parser and encoder, and needs to understand expansion only to preview it.
- The expander lives in the TOML module (`lixenwraith/toml/macro`): it needs only
  parsed trees, and the audio engine's own module needs it outside vif. The bounds
  and the reserved keys went with it; this document stays the authoring reference.

## 2. Definitions

td's `SpawnAwait`, less its fallback actions, and `Pylon` written over it:

```toml
[macros.SpawnAwait]
doc = "Emit `request`; once `species` exists go to `next`, its entity in `capture`, else to `fallback`."
params = { request = "table", species = "string", capture = "string", next = "string",
           refused = "string", fallback = "string", timeout_ms = "int" }
defaults = { timeout_ms = 500 }
body = { parent = "Root", on_enter = ["${request}"], transitions = [
    { trigger = "EventSpeciesCreated", target = "${next}", guard = "PayloadSpeciesIn",
      guard_args = { field = "species", names = ["${species}"] }, capture_vars = { entity = "${capture}" } },
    { trigger = "${refused}", target = "${fallback}" },
    { trigger = "Tick", target = "${fallback}", guard = "StateTimeExceeds", guard_args = { ms = "${timeout_ms}" } },
] }

[macros.Pylon]
doc = "Spawn a pylon at (x, y) into gw_anchor and go to `attach`, or skip to `next`."
base = "SpawnAwait"
params = { x = "int", y = "int", attach = "string", next = "string" }

[macros.Pylon.with]
species = "pylon"
capture = "gw_anchor"
next = "${attach}"
refused = "EventPylonSpawnFailed"
fallback = "${next}"
request = { action = "EmitEvent", event = "EventPylonSpawnRequest", payload = { x = "${x}", y = "${y}" } }
```

- `params`: parameter name to type: `int`, `float` (an integer is accepted),
  `bool`, `string`, `table`, `array` or `any`. `macro` and `each` are reserved.
- `defaults`: values for optional parameters; every other parameter is required.
- `doc`: one line for authors and for the editor's palette.
- `body`: any value: a table, an array, a string or a number.
- `base` and `with`, in place of `body` and never beside it: the macro is `base`
  with the arguments in `with` fixed.

A macro that is one invocation of another is written over it:

- `base` is a macro's name as written, never a template: a macro of the same
  file, in any order, or of a file read before it. `macro_files` are read in the
  order listed and the entry's own macros last. A macro that is its own base
  through any chain is an error.
- `with` holds arguments of the base and nothing else, so neither `macro` nor
  `each`, and is a template over the macro's parameters as a body is (§3).
- The base's parameters that `with` does not set stay open: the macro takes them
  with the base's type and default, as `Pylon` takes `timeout_ms`.
- `params` declares the macro's own: a new name (`x`, `y`, `attach`), or a name
  `with` sets, where the base's parameter is fixed and the macro's own stands
  under the name: `Pylon`'s `next` is where it skips to, and `SpawnAwait`'s is
  given `attach`. Declaring a name the base leaves open is an error.
- `defaults` gives or replaces the default of any of them.
- It expands exactly as the body `{ macro = <base>, <with>, p = "${p}" }` would,
  with a `p` for each open parameter: the same tree for the same budget (§4), one
  invocation deeper than its base.

Definitions are checked when read: an unknown key, an unknown type, a default of
the wrong type, a `${name}` naming no parameter and a `base` or `with` breaking a
rule above are errors before any use. A value in `with` written without `$` that
invokes nothing is checked against the base's type then, any other at expansion.

## 3. References

Inside a body or a `with`, `${name}` names a parameter, in a string or in a key.

- A string that is exactly one reference becomes the argument itself, of any type:
  `ms = "${timeout_ms}"` is the integer 500, `payload = "${payload}"` a table.
- Elsewhere the argument is spliced into the text, and must be a string, an
  integer or a boolean: `capture = "${id}_anchor"`.
- `$$` is a literal `$`; any other `$` there is an error.
- Only a body and a `with` are templates. An argument's own text is never
  scanned, so a `$` or `${x}` an author passes arrives as written.

## 4. Invocations

A table with a `macro` key is an invocation; its other keys are arguments, checked
against the parameters' types. Where it stands decides what it becomes:

- a value (a state, a payload, a guard argument): replaced by the expansion;
- an array element: an array expansion is spliced into the array, any other is
  one element;
- an array element with `each`: expanded once per element of the arrays `each`
  zips, each result spliced or appended as above.

```toml
[states]
TdP1TL = { macro = "Pylon", x = 20, y = 15, attach = "TdGwTL", next = "TdP1TR" }

[states.TdSetup]
on_enter = [
    { macro = "Zero", each = { name = ["setup_done", "setup_failed", "l_p1n"] } },
    { macro = "Announce", color = { r = 255, g = 255, b = 255 }, message = "GO" },
]
```

A state written as an invocation keeps its name in the key, so a chain reads as a
table of one line per state. `each` is a table of arguments, read once they are
expanded, and outside an array it is an error:

- an array is zipped, an element to an expansion; the arrays are all of one
  length, and with none `each` expands once;
- any other value is shared: every expansion takes it, as it takes an argument
  written beside `each`. Dummy's `each = { name = ["pad_x", "pad_left",
  "pad_right"], from = "cx", delta = [44, 42, 46] }` is three offsets from `cx`;
- an array in `each` is always zipped, whatever its parameter's type, so one
  every expansion takes whole is written beside `each`;
- a parameter named in `each` and also beside it is an error.

Bodies may invoke other macros, and arguments may be invocations: arguments are
expanded where they are written, then bound, then the body is instantiated. A
default expands where it is used, so one that invokes is written as a `table` or
`any` parameter's.

A scenario can arrive from a peer, so expansion is bounded and passing a bound is
an error:

- a macro reached again through its own expansion, and invocations nested deeper
  than 32, a base counting as one;
- more than 2^18 values written through one set: each value a body writes, a
  copied argument's or default's among them, so a scenario's files share one
  budget (td spends 1830, vanguard 6281);
- a file or a result nested deeper than 1000 levels;
- a macro of more than 64 parameters, those its base leaves open among them;
- more than 2^16 parameters left open by bases through one set: each costs its
  macro what a declared one does, though no file writes it (td's four macros
  over a base take 14, vanguard's four ranks 24).

A body is compiled when its macro is defined and instantiated in one pass, and an
error's path is built only when one is shown, so an expansion costs what it reads
and writes. Parsed and expanded, td (28 KB in 8 files) takes 1.09 ms where it took
2.73 ms, and vanguard (55 KB in 34 files) 2.17 ms where it took 7.7 ms; what the
bounds are for is the file that took seconds: a 196 KB file of one long key over
a long array took 6.1 s with no macro in it, and takes 1.5 ms.

## 5. Where macros live

- Scenario entry: defines its own `[macros]`, plus every file named by
  `macro_files = ["x.toml"]`, and invokes.
- Macro file: only `[macros]`.
- Region file, behavior program file: define nothing, `[macros]` there being an
  error, and invoke.
- One-document scenario (`Machine.LoadScenario`), bot graph: define their own
  `[macros]` and invoke.

Every file of a scenario expands against the entry's one set, so a name defined
twice is an error rather than a shadow. Macro file paths resolve like region files,
relative to the entry and never outside it. There is no built-in library: a
macro the binary supplied would expand differently on two builds holding the same
scenario bytes, which the digest could not see. A scenario that wants a shared
block library carries a copy.

## 6. Shipped scenarios

`wad/scenario/base/td` keeps its blocks in `td_macros.toml`: spawn-and-await states for
towers and pylons, corner and flank gateways, flank escalation and its closing
state, tower-loss retargeting, kill-or-left-the-map exits, announcements and
resets. `Tower` and `Pylon` are written over `SpawnAwait`, `QuadrantTower` over
`Tower` and `FlankGateway` over `Gateway`. A flank's counters share a prefix
(`l_dead`, `r_p2_done`), so one `side` parameter names them. The embedded
campaign's `macros.toml` holds the tower region's corner pylon and gateway pair
and the quasar and storm regions' left-the-map exit.
`wad/scenario/base/vanguard`'s `macros.toml` holds the raiders' one attack cycle
(`RaiderStates`) and the stun edge (`RaiderStun`), `Raider`, which makes a whole
program of a look, stats and timings, the four ranks written over it, each fixing
its stats and timings, and each cave's palette and console colour, which `Raider`
invokes by name (`{ macro = "${theme}Fg" }`). A raider type's program file is one
invocation at its root: its rank's macro with its cave and its look, frames given
as `[[frames]]`. `wad/scenario/base/simon` is one document that defines its own.

## 7. Rejected alternatives

- **Text preprocessing** (m4, Go templates, includes with substitution). It breaks
  TOML structure, reports errors against generated text, and an editor cannot
  round-trip a file it would have to render first.
- **New engine actions per pattern** (`SetVars`, `SpawnAwait` as Go). Each widens
  every runtime's vocabulary to save authoring lines. A shape that is a mechanic
  still belongs in a system; a shape that is only repetition belongs here.
- **A built-in library.** Expansion would depend on the binary rather than the
  scenario's bytes (§5).
- **Conditionals, loops beyond `each`, arithmetic.** They make the format a
  language. Decisions belong to the FSM's guards and variables, and an editor
  needs a block's output to follow from its fields alone.
- **Expanding at the root into several states or regions** (`[[expand]]`).
  Deferred to the map editor, whose encounter blocks need it; state-valued
  invocations cover the shipped scenarios. Key interpolation is already supported
  so the extension is additive.

## 8. Next consumer

- **Map and scenario editor.** Blocks are macros and placed blocks are
  invocations; the parameter metadata the editor still needs is in
  [TODO](todo.md#build-the-map-and-scenario-editor).
