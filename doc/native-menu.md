# Native menu

The configuration menu as a host draws it with its own widgets. Android does, from
one JSON snapshot of the menu's tree, answering with ops on a node's id; the browser
is to follow from the same snapshot and ops
([todo](todo.md#draw-the-configuration-menu-with-the-pages-own-widgets)). The
terminal and the desktop window keep the `tui` menu.

## 1. Problem and scope

- A `tui` menu row is one cell, 12 dp tall at Android's default zoom, against its
  48 dp touch target: the text is hard to read and a tap lands on a neighbour.
- In scope: the [menu](config-menu.md) alone, however it is summoned (Ctrl-G, F10,
  `:menu`/`:g`/`:config`, a press held on the status bar). Help, telemetry, about,
  the profiler report and the command line stay `tui` overlays.
- Not in scope: new rows, editing launch options or the keymap.

## 2. Decisions

- **Runtime model, no generator.** Go sends the whole menu as one JSON snapshot
  whenever it changes, and the host draws it with one renderer per node kind and
  answers with hooks naming a node by its id. The Go library and the Java app ship
  in one APK built from one commit, so they cannot drift at runtime; a generated
  template would miss the rows built at run time (scenarios, players, bots) and
  duplicate every label. The contract is the node kinds, the hook ops and the field
  input types (`mode.MenuKinds`, `MenuOps`, `MenuInputs`); a test reads them from the
  Java source and fails on a mismatch. If `android/` ever leaves this repository,
  the snapshot gains a schema version then.
- **General tree.** Pages, sections and rows are one tree of nodes of any depth,
  which both menus draw: the `tui` menu enters a top-level group as a page and draws
  a deeper one as a section title over its rows; the native menu expands a page in
  place and titles its sections.
- **Destructive rows confirm on native hosts.** A node marked `confirm` asks in a
  native dialog before its hook is sent: Restart game, Start &lt;scenario&gt;, Leave
  the session, Drop &lt;slot&gt;, and Remove or Cancel joining for a bot. The `tui`
  menu keeps its select-then-act press, which already asks twice.
- **Built on the frame.** The snapshot is built every frame the menu is open, under
  the world lock after the `tui` refresh, and pushed when its bytes changed. Measured
  on the build container (4 vCPU, five runs of `go test -bench`): the full tree with
  three bots, three other players and the scenario list marshals in 104–132 µs
  (median 113 µs), 14.5 KB of JSON, 46.6 KB and 228 allocations; the `tui` refresh of
  the Multiplayer page it replaces costs 15.5–17.3 µs (median 16.4 µs), 12.5 KB and
  92 allocations. At 60 frames a second that is 0.7% of a core while the menu is
  open, so it stays on the frame rather than moving to the input tick.
- **A search's rows act in place**, as the `tui` search's do; the host shows them
  flat in place of the tree. **`sfx_volume` stays a step**: one op, no number crossing.

## 3. The tree

- `internal/mode/config_menu.go`: `configPages` is six `configNode` groups (Game,
  Scenarios, Multiplayer, Settings, Developer, Launch options), each holding
  `section(label, nodes)` groups and rows, and an `extra` func for rows built at run
  time (`replayConfigOptions`, `scenarioConfigOptions`, `multiplayerConfigOptions`).
- Every node has an explicit `kind`: `group`, `toggle` (two choices, a `change`, or a
  bare command that turns it over), `choice` (more than two), `step` (`steps` or a
  `change`), `pick`, `action` (a command or its own `activate`), `form` (a spec in
  `configForms`) or `info`. `TestEveryNodeHasTheKindItsFieldsServeAndAnIdOfItsOwn`
  holds each kind to its fields.
- A node's id is its keys from the root joined by `/`: `settings/#Sound/sfx`,
  `scenarios/#Installed/pick/main`, `multiplayer/#Bots/drop/10`. Run-time keys hold
  `/` themselves, which `configNodeFor`'s walk down the groups on the id's path
  allows; the same test holds the tree to unique ids. The `tui` menu keys its rows,
  a search's included, by the same id, so `changeConfigMenu` is the selection lookup
  over `applyConfigNode`, the one function a key and a host's op reach.
- `OverlayMenu.Page` is the id of the group shown as a page, empty for the pages;
  `OverlayMenu.Scenarios` is the scenario list, scanned once when the menu opens
  rather than every frame.
- `config_form.go`: `configForms` gives each form its fields (label, default, help,
  and `input`: `text`, `number` or `uri`, which picks a host's keyboard) and a
  `submit`; `formRow(key)` is the row it opens from, a one-field form typed in on
  its row in the `tui` menu. `submitConfigForm` is the one submit, for the `tui`
  form and a host's values alike; its errors are sentences (`sentence`).

## 4. Snapshot

Built by `Router.MenuSnapshot` only while the menu is open on a host that draws its
own, nil otherwise; the app pushes it when its bytes differ from the last pushed.

```json
{
  "title": "Menu · game paused",
  "status": {"text": "Only the host can restart the game in a session", "kind": "notice"},
  "form": {"id": "multiplayer/#Session/host", "error": "Address in use"},
  "query": "pause",
  "found": [{"id": "game/pause", "label": "Game / Resume"}],
  "nodes": [
    {"id": "game", "kind": "group", "label": "Game", "description": "...", "nodes": [
      {"id": "game/pause", "kind": "action", "label": "Resume", "description": "..."},
      {"id": "game/restart", "kind": "action", "label": "Restart game", "confirm": true},
      {"id": "game/speed", "kind": "step", "label": "Game speed", "value": "1x"}]},
    {"id": "scenarios", "kind": "group", "label": "Scenarios", "nodes": [
      {"id": "scenarios/#Installed", "kind": "group", "label": "Installed", "nodes": [
        {"id": "scenarios/#Installed/pick/main", "kind": "pick", "label": "main", "value": "current", "marked": true}]},
      {"id": "scenarios/start", "kind": "action", "label": "Start main", "confirm": true},
      {"id": "scenarios/#Other", "kind": "group", "label": "Other", "nodes": [
        {"id": "scenarios/#Other/scenario", "kind": "form", "label": "Other scenario",
         "fields": [{"label": "Scenario", "value": "", "input": "text"}]}]}]},
    {"id": "settings", "kind": "group", "label": "Settings", "nodes": [
      {"id": "settings/#Sound", "kind": "group", "label": "Sound", "nodes": [
        {"id": "settings/#Sound/sfx", "kind": "toggle", "label": "Sound effects", "value": "on", "choices": ["off", "on"]},
        {"id": "settings/#Sound/sfx_volume", "kind": "step", "label": "Effects volume", "value": "80%",
         "disabled": "Audio unavailable in this run"}]},
      {"id": "settings/#Controls", "kind": "group", "label": "Controls", "nodes": [
        {"id": "settings/#Controls/auto", "kind": "choice", "label": "Auto-fire", "value": "main",
         "choices": ["off", "main", "on"]}]}]}
  ]
}
```

- Node kinds: `group` holds nodes; `toggle` has two `choices`, `off`/`on` unless the
  row has its own (`disable`/`enable`), and reads the second when on; `choice` one
  of its `choices`; `step` a value moved by a direction; `pick` one of a list, the
  one `marked`; `action` runs when activated, after a confirm when `confirm`; `form`
  has `fields`, each with `label`, default `value`, `input` and `help`; `info` is
  read only. An action, a form and an info row carry no `value`.
- `id` is opaque to the host, which only echoes it back. `label` follows the state
  where the row's action does (Pause, Resume); `description` is the row's help.
- `disabled` is the reason, the sentence the command refuses with
  ([unavailable](term-index.md)). A disabled node is drawn dimmed, its reason first,
  and sends no hook; an info row carries none.
- `status` is the status bar's line and its kind (`notice`, `progress`, `alert`):
  the native menu covers the bar, so a refusal shows here instead.
- `form` is the error of the last submit that failed, by id, until the next op. The
  host shows it on that form.
- `query` and `found`: a search is Go's, as in the `tui` menu. `found` lists the
  matching ids with their `Page / Row` labels; the host shows those nodes flat in
  place of the tree, and "Nothing matches" for none.

## 5. Hooks

The host makes one call, which never blocks its UI thread: `Game.menu(op, id, arg)`
on Android. The view queues the op on a channel, the loop selects on it as it does on
`Away`, applies it with `Router.MenuOp` under `RunSafe` and dispatches what it pushed.

- `set id value`: a `toggle` or a `choice` to one of its choices; a value the node
  lacks or already shows changes nothing.
- `step id ±1`: moves a `step`, as Left and Right do.
- `activate id`: runs an `action` or marks a `pick`; for a `confirm` node the host
  sends it after its dialog. A form is the host's to open.
- `submit id fields`: a form's values as a JSON array of strings, fields left out
  taking their defaults. Success closes the menu; failure sets `form`.
- `search text`: sets the overlay filter, as typing after `/` does; empty clears it.
- `close`: Back or × at the menu's top level. Runs `Router.closeOverlay`.

An op resolves through `applyConfigNode`, which the key path calls once it has the
selection, under the same origin, so a replay matches. An op on an id that no longer
exists, a bot that left, is dropped; one on a disabled id answers with its reason on
`status`; any op drops the form error standing from the last submit.

The host never closes the menu by itself. It sends `close` and dismisses the menu
when a snapshot says it is hidden. A hidden snapshot (an action ran, Ctrl-G, the run
restarted) dismisses whatever is up, picker, confirm and form included.

## 6. Host seam

`internal/app/presentation.go`: `menuPresentation`, an optional interface with
`Menu(snapshot []byte)` (nil hides) and `MenuOps() <-chan mode.MenuOp`. When the
presentation has it, `renderContext` builds the snapshot after `RefreshConfigMenu`
and `pushMenu` hands it over when its bytes changed, the first frame included, so a
run replacing another dismisses what that one left up; `RenderContext.NativeMenu`
leaves the `tui` menu layout undrawn; the loop applies each op with `App.menuOp`.
The router still holds the overlay content, so modes, `pause_menu` and the summons
stay one mechanism. The `vif_headless` profile builds without it.

## 7. Android

- `android/game`: `Listener.Menu(json)`, empty for a closed menu; `Game.Menu(op, id,
  arg)` queues an op; the view implements `menuPresentation`.
- `MenuDialog.java`: a `Dialog` nearly the screen's size, like the start screen's
  touch controls window, in the site's dark theme and Roboto from `StartScreen`'s
  constants. It has its own window, so `SOFT_INPUT_ADJUST_RESIZE` lifts a field
  above the keyboard while the activity stays `adjustNothing` and the grid is never
  cropped; `MainActivity` ignores the keyboard's insets while it is up.
- Layout: the title (what the game does meanwhile) and ×, then a search field; below,
  a `ScrollView` of rows at least 48 dp tall, labels at 16 sp, values and
  descriptions at 14 sp in the muted colour, all in sp so the system's font scale
  applies; a page expands in place under a chevron and its sections show as titles
  over their rows; a disabled row is dimmed and shows its reason first; the status
  line sits at the foot.
- Widgets by kind: `toggle` a `Switch`; `choice` a row showing its value, which
  opens a radio list; `step` −, the value, +; `pick` a radio row, the Start action
  under the list naming the marked one; `action` a row that acts when tapped, after
  a confirm dialog when `confirm`; `form` one field as an `EditText` on the row,
  whose keyboard action submits, with the error under it, and more fields in a
  sheet with the focused field's help under it and the error above Submit and
  Cancel, the keyboard following `input`; `info` a label over its description.
- Controlled widgets: a tap sends its hook while the widget keeps showing the
  snapshot's value, so a refused change never flickers; the next snapshot says what
  happened. Updates land in place: views are keyed by id, labels, values, enabled
  states and choices rewritten, rows added or removed only where the tree's shape
  changed, so expansion, scroll position and focus are kept. Text typed into a field
  belongs to the host until submit, and a snapshot never overwrites it. Which pages
  are expanded is remembered for the run but not saved.
- Back closes the innermost of confirm, picker and form, then clears a search, then
  sends `close`: `configMenuBack`'s order, with expansion in place of the page level.
  A tap outside the window does the same.
- Hardware keys, in the dialog's `onKeyDown` and never in a field being typed in:
  arrows, Tab, Enter and Space move focus and act, as Android's focus does; `j`/`k`
  move focus and `h`/`l` step or cycle the focused row; `/` focuses the search
  field; `q` and Escape go back; Ctrl-G, F10 and F1 go to the run as keys, which
  close the menu or open help over it, as in the `tui` menu.
- TalkBack reads the platform widgets, and each row describes its label, value and
  reason. Platform widgets only: no androidx or Material dependency and no
  `RecyclerView`; the menu has fewer than a hundred rows.

## 8. Browser

The next task, from the same snapshot and ops: `cmd/vif/browser.go` implements
`menuPresentation` over the Worker, a `menu` message carrying the snapshot to the
page and the page's `menu` message an op back; `web/terminal.js` and
`web/index.html` draw it as a `<dialog>` with the layout and kinds of §7, and
`web/terminal.css` holds its rules in the site's tokens; the contract test reads
`web/terminal.js` too. Recommended: every browser draws it, as every Android device
does even with a keyboard attached, so each host has one look.

## 9. Verification

- Go gates: `go build ./...`, `GOOS=js GOARCH=wasm go build ./...`,
  `go build -tags vif_headless ./...`, `gofmt -l` on changed files,
  `go test ./internal/mode ./internal/render/renderer` and the menu and
  presentation tests of `./internal/app`, and `go -C android test ./...`.
- Tests, one per rule: every node's kind matches its fields and ids are unique; a
  host's op and the key path reach the same state, for each kind; an op on a
  vanished id is dropped; an op on a disabled id answers with its reason; a failed
  submit keeps its error and a successful one closes the menu; `search` finds what
  `/` finds; `close` resumes a game the menu paused; the snapshot changes only with
  the menu, and a host is handed it only then, the first frame included; the overlay
  leaves the menu undrawn under a host's; the Java contract matches Go's; the
  binding carries snapshots out and ops in.
- Java: type-check with `javac` against Robolectric's `android-all`
  ([agent notes](agent-notes.md#toolchain)), build the APK with `make android`,
  and on a device or emulator: open the menu by each summon; try every kind, a
  search and a one-field form; fill the request form with the keyboard up; rotate
  the device with the menu open; press Back at each layer; join a session as a
  guest, where the host-only rows are disabled; use it with TalkBack.

## 10. Notes

- **Live values:** values, labels, disabled reasons and run-time rows change while
  the menu is open: a bot joins then leaves, the host pauses, the session summary
  changes. Controlled widgets and keyed in-place updates handle this; a confirm,
  picker or form open on a row that disappears is dismissed.
- **Ordering:** snapshots come from the loop's goroutine and reach the UI thread in
  order through `runOnUiThread`. Ops go the other way through one channel, also in
  order. A tap made on a stale snapshot is resolved by id against the current tree.
- **Parity:** the `tui` menu drills into a page while the native one expands it in
  place. The ops, the Back order and the keys of §7 keep everything else alike;
  `multi-platform.md` records the difference as one the host forces.
- **Form errors** belong to the form, not to a field, so a message names its field
  ("Bot count must be 1..8").
