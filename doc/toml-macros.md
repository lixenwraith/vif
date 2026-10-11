# TOML Macros

Decision record and authoring reference for macros in vif's TOML documents.
`pkg/tomlmacro` is the one expander; scenarios and bot graphs use it today, and
behavior programs and the map editor are meant to.

## 1. Decision

**Context.** Scenario regions repeat whole shapes with a few values changed. Before
this change `wad/scenario/td` spent 716 region lines on about a dozen shapes: a
spawn request awaited with a capture and a timeout fallback (fifteen states), a
gateway attach (nine), guarded retargets on a tower's loss (fourteen actions), and
flank escalation mirrored per side. Copies drift, and a reviewer cannot tell a deliberate
difference from a stale one. The same need is coming from data-driven behavior
programs ([Behavior machines](machine-design.md) §8) and from the map editor,
whose placeable blocks are the same idea.

**Decision.** Macros are data in the document, expanded on the parsed tree
between `toml.Parse` and decode:

- a macro is a `[macros.<Name>]` table with typed parameters, defaults, a doc
  string and a body;
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
  `td_main.toml: states.TdSpawnQ1: macro "QuadrantTower": body.fallback_actions[0]:
  macro "Retarget": unknown argument "x"`.
- An editor reads and writes macros and invocations as ordinary tables with the
  same parser and encoder, and needs to understand expansion only to preview it.

## 2. Definitions

```toml
[macros.Pylon]
doc = "Spawn a pylon at (x, y) into gw_anchor and go to `attach`, or skip to `next`."
params = { x = "int", y = "int", attach = "string", next = "string", timeout_ms = "int" }
defaults = { timeout_ms = 500 }
body = { parent = "Root", on_enter = [
    { action = "EmitEvent", event = "EventPylonSpawnRequest", payload = { x = "${x}", y = "${y}" } },
], transitions = [
    { trigger = "EventSpeciesCreated", target = "${attach}", guard = "PayloadIntCompare",
      guard_args = { field = "species", op = "eq", value = 5 }, capture_vars = { entity = "gw_anchor" } },
    { trigger = "EventPylonSpawnFailed", target = "${next}" },
    { trigger = "Tick", target = "${next}", guard = "StateTimeExceeds", guard_args = { ms = "${timeout_ms}" } },
] }
```

| Key | Meaning |
|---|---|
| `params` | Parameter name → type: `int`, `float` (an integer is accepted), `bool`, `string`, `table`, `array` or `any`. `macro` and `each` are reserved. |
| `defaults` | Values for optional parameters; every other parameter is required. |
| `doc` | One line for authors and for the editor's palette. |
| `body` | Any value: a table, an array, a string or a number. |

Definitions are checked when read: an unknown key, an unknown type, a default of
the wrong type and a `${name}` naming no parameter are errors before any use.

## 3. References

Inside a body, `${name}` names a parameter, in a string or in a key.

- A string that is exactly one reference becomes the argument itself, of any type:
  `value = "${species}"` is the integer 5, `payload = "${payload}"` a table.
- Elsewhere the argument is spliced into the text, and must be a string, an
  integer or a boolean: `capture = "${id}_anchor"`.
- `$$` is a literal `$`; any other `$` in a body is an error.
- Only bodies are templates. An argument's own text is never scanned, so a `$` or
  `${x}` an author passes arrives as written.

## 4. Invocations

A table with a `macro` key is an invocation; its other keys are arguments, checked
against the parameters' types. Where it stands decides what it becomes:

| Position | Result |
|---|---|
| A value (a state, a payload, a guard argument) | Replaced by the expansion. |
| An array element | An array expansion is spliced into the array; any other is one element. |
| An array element with `each` | Expanded once per element of the zipped arrays `each` names, each result spliced or appended as above. |

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
table of one line per state. `each` takes a table of parameter = array, all of one
length; arguments outside it are shared by every expansion.

Bodies may invoke other macros, and arguments may be invocations: arguments are
expanded where they are written, then bound, then the body is substituted and
expanded in turn. A macro reached again through its own expansion is an error, as
is nesting deeper than 32 or producing more than 2^18 values through one set.
Those bounds exist because a scenario can arrive from a peer.

## 5. Where macros live

| Document | Defines | Invokes |
|---|---|---|
| Scenario entry | its own `[macros]`, plus every file named by `macro_files = ["x.toml"]` | yes |
| Macro file | only `[macros]` | — |
| Region file | nothing: `[macros]` there is an error | yes |
| One-document scenario (`Machine.LoadScenario`), bot graph | its own `[macros]` | yes |

Every file of a scenario expands against the entry's one set, so a name defined
twice is an error rather than a shadow. Macro file paths resolve like region files,
relative to the entry and never outside it. There is no built-in library: a
macro the binary supplied would expand differently on two builds holding the same
scenario bytes, which the digest could not see. A scenario that wants a shared
block library carries a copy.

## 6. Shipped scenarios

`wad/scenario/td` keeps its blocks in `td_macros.toml`: spawn-and-await states for
towers and pylons, corner and flank gateways, flank escalation and its closing
state, tower-loss retargeting, kill-or-left-the-map exits, announcements and
resets. Its regions fell from 716 to 330 lines. Two pairs of variables were renamed
so one `side` parameter names a flank's counters: `left_dead`/`right_dead` became
`l_dead`/`r_dead`, and `p2l_done`/`p2r_done` became `l_p2_done`/`r_p2_done`. The
embedded campaign's `macros.toml` holds the tower region's corner pylon and gateway
pair and the quasar and storm regions' left-the-map exit. State names are
unchanged in both, and each resolved scenario decodes equal to its previous form.

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
- **Placing the expander in `lixenwraith/toml`.** It needs only parsed trees, so it
  can move there once another module wants it; the bounds and the reserved keys
  are vif's choices until then.

## 8. Next consumers

- **Behavior programs.** Program files listed by a scenario expand against the
  scenario's set, like region files ([Behavior machines](machine-design.md) §8.3).
- **Map and scenario editor.** Blocks are macros and placed blocks are
  invocations; the parameter metadata the editor still needs is in
  [TODO](todo.md) under the editor item.
