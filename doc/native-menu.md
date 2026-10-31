# Native menu

Requirements and plan for a menu the host draws with its own widgets: Android
first, the browser next. The terminal and the desktop window keep the `tui` menu.
This takes option 2 of the
[todo item](todo.md#give-the-configuration-menu-a-touch-target-on-android).

## 1. Problem and scope

- A menu row is one cell, 12 dp tall at the default zoom, against Android's 48 dp
  touch target: the text is hard to read and a tap lands on a neighbour.
- In scope: the [menu](config-menu.md) alone, however it is summoned (Ctrl-G, F10,
  `:menu`/`:g`/`:config`, a press held on the status bar). Help, telemetry, about,
  the profiler report and the command line stay `tui` overlays.
- Hosts: Android in phases 1–5, the browser in phase 6. The terminal and the
  desktop window have no widget toolkit and keep the `tui` menu.
- Not in scope: new rows, editing launch options or the keymap.

## 2. Decisions

- **Runtime model, no generator.** Go sends the whole menu as one JSON snapshot
  whenever it changes. The host draws it with one renderer per node kind and
  answers with hooks naming a node by its id. The Go library and the Java app ship
  in one APK built from one commit, so they cannot drift at runtime. A generated
  template would miss the rows built at run time (scenarios, players, bots), and
  every label would be duplicated. The contract is the node kinds, the hook ops and
  the field input types. A Go test reads them from the Java source, and later from
  `web/terminal.js`, and fails on a mismatch. If `android/` ever leaves this
  repository, the snapshot gains a schema version then.
- **Android first, browser next**, from the same snapshot and hooks. Until the
  browser's lands, `multi-platform.md` and `todo.md` record the gap.
- **General tree.** Pages, their sections and rows become one tree of nodes of any
  depth, which both menus draw. The `tui` menu enters a top-level group as a page
  and draws a deeper one as a section title over its rows, as it does now. The
  native menu expands groups in place. The content keeps its present shape: page,
  section, row.
- **Destructive rows confirm on native hosts.** A node marked `confirm` asks in a
  native dialog before its hook is sent. These are Restart game, Start
  &lt;scenario&gt;, Leave the session, Drop &lt;slot&gt;, and Remove or Cancel joining
  for a bot. The `tui` menu keeps its select-then-act press, which already asks
  twice.

## 3. What exists

This section describes main after #928, which reorganized the menu.

- `internal/mode/config_menu.go` defines `configPages`, six of them: Game,
  Scenarios, Multiplayer, Settings, Developer, Launch options. Each `configPage` has
  static `options` and an `extra` func for rows built at run time
  (`replayConfigOptions`, `scenarioConfigOptions`, `multiplayerConfigOptions`).
  `section(label)` is a header row keyed `#<label>`, never selected, that titles the
  rows after it.
- A `configOption`'s kind is implied by the fields it sets:
  - `choices` with `command`: cycles named values (`auto`, `level`, `pause_menu`);
  - `steps` with `command`: numeric presets (`stat`, `rec`);
  - `change`: a func taking a direction. `sfx` and `music` toggle with it;
    `sfx_volume`, `fps` and `speed` step with it;
  - `action`: Enter runs `command` and closes the menu (Pause, Restart game, Start,
    Leave, the profiles). Pause's label follows the state through `name`
    (Pause, Resume, Pause everyone);
  - `pick`: a row of a pick list, which a press marks at once. The mark is
    `OverlayMenu.Choice`, which Start reads (`pickConfigRow`, keys `pick/<name>`);
  - `text`: a one-field form typed in on its own row (Other scenario, Join a
    session). `configForm.inline` holds it;
  - `activate`: a func (the longer forms, the bot and player rows);
  - none of these: read only (Launch options, Current session).

  A host cannot tell from these that `sfx` is a switch or `fps` a stepper, so the
  kind becomes explicit.
- `read`, `name` and `disabled` are called live. `configOption.unavailable` folds in
  `commandUnavailable`, which covers replay viewers and driven runs.
- `/` searches every page. `configMenuView` matches all the typed words against the
  page, section, label and description, lists the matches as `Page / Row`, and keys
  them `page:key`, which `configOptionFor` splits.
- `config_form.go`: `configForms` gives each form its fields (label, default, help)
  and a `submit` that returns an error. `Router.configForm` holds a
  `tui.FormState`; one field is drawn inline, more open a window.
- `Router.changeConfigMenu(direction, activate)` is where a row acts, under
  `event.OriginCommand`. `moveConfigMenu`, `pointConfigMenu`,
  `handleOverlayActivate` and `configMenuBack` reach it through the selection
  (`GameContext.OverlaySelection`). `configMenuBack` steps out of a form, then a
  search, then a page, then the menu.
- `handleConfigCommand` opens the overlay and calls `PauseForMenu`, which pauses a
  game that may pause unless `[play] pause_menu` is off. `Router.closeOverlay`
  resumes and returns to Normal. Every summon ends in `handleConfigCommand`, so no
  host needs to know how the menu was opened.
- `App.renderContext` calls `Router.RefreshConfigMenu` every frame under the world
  lock. `publishConfigMenu` keeps the old content when nothing changed. On the
  Scenarios page, and on every page during a search, that refresh calls
  `resource.Scenarios`, an `os.ReadDir` per configuration root, every frame.
- `core.OverlayMenu` is the snapshot the `tui` renderer draws: `Page`, `Choice`,
  rows (`Key`, `Label`, `Value`, `Description`, `Disabled`, `Header`, `Text`,
  `Typing`, `Cursor`) and the form.
- Android: `android/game` binds `Start`, `Listener` (`Exited`) and the input calls.
  The view posts `terminal.Event`s on one channel and its away state on another
  (`Away`, an optional interface on `Presentation` that the loop selects on).
  gomobile passes strings, byte slices, numbers, bools and interfaces, but not
  slices of structs, so a tree crosses the binding as JSON. The app has no androidx
  or Material dependency: `StartScreen` builds platform widgets in the site's
  colours. The activity is `adjustNothing`, so the keyboard never resizes the grid.
- Browser: the Worker posts typed messages to the page (`web/worker.js`); a menu
  would be one more type.

## 4. Model

A snapshot is built only while the menu is open on a host that draws its own. It is
marshalled to JSON and pushed when its bytes differ from the last one pushed.

```json
{
  "title": "Menu · game paused",
  "status": {"text": "Only the host can restart the game in a session", "kind": "notice"},
  "form": {"id": "multiplayer/join", "error": "Join failed: connection refused"},
  "query": "",
  "found": [],
  "nodes": [
    {"id": "game", "kind": "group", "label": "Game", "description": "...", "nodes": [
      {"id": "game/pause", "kind": "action", "label": "Resume"},
      {"id": "game/restart", "kind": "action", "label": "Restart game", "confirm": true},
      {"id": "game/speed", "kind": "step", "label": "Game speed", "value": "1x"}]},
    {"id": "scenarios", "kind": "group", "label": "Scenarios", "nodes": [
      {"id": "scenarios/#Installed", "kind": "group", "label": "Installed", "nodes": [
        {"id": "scenarios/pick/main", "kind": "pick", "label": "main", "value": "current", "marked": true}]},
      {"id": "scenarios/start", "kind": "action", "label": "Start main", "confirm": true},
      {"id": "scenarios/#Other", "kind": "group", "label": "Other", "nodes": [
        {"id": "scenarios/scenario", "kind": "form", "label": "Other scenario",
         "fields": [{"label": "Scenario", "value": "", "input": "text"}]}]}]},
    {"id": "settings", "kind": "group", "label": "Settings", "nodes": [
      {"id": "settings/#Sound", "kind": "group", "label": "Sound", "nodes": [
        {"id": "settings/sfx", "kind": "toggle", "label": "Sound effects", "value": "on"},
        {"id": "settings/sfx_volume", "kind": "step", "label": "Effects volume", "value": "80%",
         "disabled": "Audio unavailable in this run"}]},
      {"id": "settings/#Controls", "kind": "group", "label": "Controls", "nodes": [
        {"id": "settings/auto", "kind": "choice", "label": "Auto-fire", "value": "main",
         "choices": ["off", "main", "on"]}]}]}
  ]
}
```

- Node kinds:
  - `group`: a page or a section, holding nodes;
  - `toggle`: two states, drawn as a switch;
  - `choice`: one of n named values;
  - `step`: moved by a direction, drawn as − and +;
  - `pick`: one of a pick list; `marked` is the one marked;
  - `action`: runs when tapped;
  - `form`: text fields; with one field the host draws it on the row, as the
    `tui` menu does, and with more it opens a sheet;
  - `info`: read only.
- `id` is opaque to the host, which only echoes it back. Go builds it from the keys
  from the root and resolves it. The run-time keys hold `/` (`pick/<name>`,
  `drop/<id>`, `player/<entity>`) and a search's hold `:`, so Go picks one encoding
  and a test holds every key to it.
- `label` is what `name` gives, when a node has one. `value` is the display string
  `read` gives. The host shows it, and reads state from it only for a `toggle`
  (`on`/`off`, `enable`/`disable`) and a `choice` (one of `choices`).
- `disabled` is the reason, the same sentence the command gives
  ([unavailable](term-index.md)). A disabled node is drawn but sends no hook.
- `fields[].input` is `text`, `number` or `uri`, which picks the keyboard: Count and
  Players take numbers; Address, Target and Site take URIs.
- `status` is the status bar's line and its kind. The native menu covers the bar, so
  a refusal (`setCommandError`) shows here instead.
- `form` is the error from the last submit that failed, by id. The host keeps that
  form open and shows the error.
- `query` and `found`: a search is Go's, as in the `tui` menu. `found` lists the
  matching ids with their `Page / Row` labels, and the host shows them flat in
  place of the tree.

## 5. Hooks

The host makes one call, which never blocks its UI thread: `Game.menu(op, id, arg)`
on Android, a `menu` message from the page to the Worker in the browser. The view
queues the op on a channel. The loop selects on it, as it does on `Away`, applies it
with `Router.MenuOp` under `RunSafe`, and dispatches what it pushed.

- `set id value`: sets a `toggle` or `choice` to that value. The router checks the
  value against `choices` and runs `command value`. A toggle made with `change`
  runs `change(+1)` when the value differs.
- `step id ±1`: moves a `step` node, as Left and Right do.
- `activate id`: runs an `action`, marks a `pick`, or runs a run-time row's
  `activate`. For a `confirm` node, the host sends it after the confirm.
- `submit id fields`: a form's values, as a JSON array of strings. Go validates and
  submits as `handleConfigForm` does. Success closes the menu; failure sets `form`.
- `search text`: sets the overlay filter, as typing after `/` does; empty clears it.
- `close`: Back or × at the menu's top level. Runs `Router.closeOverlay`.

An op resolves through the same function the `tui` path calls once it has the
selection. A native tap and a key therefore run the same code under the same origin,
and a replay matches. An op on an id that no longer exists, such as a bot that left,
is dropped. An op on a disabled id answers with its reason on `status`.

The host never closes the menu by itself. It sends `close` and dismisses the menu
when a snapshot says it is hidden. A hidden snapshot (an action ran, Ctrl-G, the run
restarted) dismisses whatever is up, picker and form included.

## 6. Go changes

- **Tree:** `configPage`, `section` and `configOption` fold into one `configNode`
  with `kind`, `children` and `extra`. `OverlayMenu.Page` becomes a path, and the
  `tui` view walks the tree to the page the path names, drawing its subgroups as
  section titles. At today's depth the `tui` menu behaves as it does now, search
  included.
- **Kind:** explicit on every node. A test holds every static node to a kind its
  fields serve: a `toggle` has two choices or a `change`, a `step` has `steps` or a
  `change`, a `pick` has an `activate`, and so on.
- **Id-addressed apply:** `changeConfigMenu` splits into the selection lookup and
  `applyConfigNode(id, op, arg)`. `handleConfigForm`'s submit splits out into a
  function that a native submit calls with the values.
- **Snapshot builder:** sits beside `configMenuView` and walks the whole tree.
  `resource.Scenarios` is read once when the menu opens, not every frame, which the
  `tui` Scenarios page and search gain too.
- **Host seam** in `internal/app/presentation.go`: an optional `menuPresentation`
  with `Menu(snapshot []byte)` (nil hides) and `MenuOps() <-chan MenuOp`. When a
  presentation has it, the renderer leaves the menu layout undrawn (a
  `RenderContext` flag), and `renderContext` pushes the snapshot after
  `RefreshConfigMenu`. The router still holds the overlay content, so modes,
  `pause_menu` and the summons stay one mechanism. The `vif_headless` profile
  builds without it.
- **Words:** descriptions name keys ("Left slows down; Right or Enter speeds up")
  and action rows show "Enter" as their value (`enterValue`). Descriptions move to
  host-neutral words that every host can show
  ([AGENTS.md](../AGENTS.md#platforms)). The `tui` menu keeps drawing "Enter" where
  a native host draws a button. Form errors start with a capital (`sentence`),
  which "bot count must be 1..N", "name a scenario" and "players must be a number"
  do not today.
- **Cost:** a benchmark builds and marshals the full tree at the menu's present
  size, in a session with players, bots and the scenario list. Its result is
  reported next to the per-frame refresh it replaces.

## 7. Android

- `android/game`: `Listener` gains `Menu(String json)`, where an empty string
  hides the menu. `Listener` then has two methods and stops being a lambda target,
  so `GameView` passes an anonymous class. `Game.menu(op, id, arg)` queues an op,
  and the view implements `menuPresentation`. Tests go beside `game_test.go`.
- `MenuDialog.java`, one new class: a `Dialog` nearly the screen's size, like the
  start screen's touch controls window. It has its own window, so
  `SOFT_INPUT_ADJUST_RESIZE` lifts a field above the keyboard while the activity
  stays `adjustNothing` and the grid is not cropped. It uses the site's dark theme
  and Roboto, from `StartScreen`'s constants.
- Layout:
  - at the top, the title (what the game does meanwhile) and ×, then a search field;
  - below, a `ScrollView` of rows at least 48 dp tall;
  - labels at 16 sp; values and descriptions at 14 sp in the muted colour. Sizes are
    in sp, so the system's font scale applies;
  - a page expands in place under a chevron, and its sections show as titles over
    their rows;
  - a disabled row is dimmed and shows its reason first;
  - the status line sits at the foot.
- Widgets by kind:
  - `toggle`: a `Switch`;
  - `choice`: a row showing its value, which opens a radio list;
  - `step`: −, the value, +;
  - `pick`: a radio row; the Start action under the list names the marked one;
  - `action`: a row that acts when tapped, after a confirm dialog when `confirm`;
  - `form`: one field is an `EditText` on the row, whose keyboard action submits,
    with the error beside it. More fields open a sheet, with the focused field's
    help under it and the error above Submit and Cancel. The keyboard follows
    `input`, and paste works;
  - `info`: a label over its description, which shows the session summary on
    several lines.
- Controlled widgets: a tap sends its hook while the widget keeps showing the
  snapshot's value, so a refused change never flickers on and off; the next
  snapshot says what happened.
- Updates in place: views are keyed by id. When a snapshot arrives, labels, values,
  enabled states and choices are rewritten, and rows are added or removed only
  where the tree's shape changed. Expansion, scroll position and focus are kept.
  Text typed into a field belongs to the host until submit, and a snapshot never
  overwrites it. Which pages are expanded is remembered for the run but not saved.
- Back closes the innermost of confirm, picker and form, then clears a search, then
  sends `close`. This is `configMenuBack`'s order, with expansion taking the place
  of the page level.
- Hardware keys, mapped in the dialog's `onKeyDown` and never in a field being
  typed in:
  - arrows, Tab, Enter and Space move focus and act, as Android's focus does;
  - `j`/`k` move focus, and `h`/`l` step or cycle the focused row;
  - `/` focuses the search field;
  - `q` and Escape go back;
  - Ctrl-G, F10 and F1 go to the run as keys, which close the menu or open help
    over it, as in the `tui` menu.
- TalkBack reads the platform widgets, and each row has a content description of
  its label and value.
- Platform widgets only: no androidx or Material dependency and no `RecyclerView`.
  The menu has fewer than a hundred rows.

## 8. Browser

- `cmd/vif/browser.go` implements `menuPresentation` over the Worker. A `menu`
  message carries the snapshot to the page, and the page's `menu` message carries an
  op back.
- `web/terminal.js` and `web/index.html` draw it as a `<dialog>` with the layout
  and kinds of §7; `web/terminal.css` holds its rules in the site's tokens.
- To decide then: whether a browser without touch draws it too. Recommended: every
  browser does, as every Android device does even with a keyboard attached, so each
  host has one look.

## 9. Phases

Each phase is one commit on the task's branch, pushed before the next one starts.

1. **Go tree and kinds:** `internal/mode/config_menu.go`, `config_form.go`,
   `overlay.go`, `router.go`, `internal/core/overlay.go`,
   `internal/mode/overlay_test.go`. A player sees no change in the `tui` menu.
   Descriptions and errors are reworded, and scenarios are scanned once per open.
2. **Snapshot, ops and seam:** `internal/mode` (`MenuOp`, the builder),
   `internal/app/presentation.go`, `loop.go`,
   `internal/render/renderer/overlay.go`, the benchmark.
3. **Binding:** `android/game/game.go`, `view.go`, `game_test.go`.
4. **Android UI:** `MenuDialog.java`, `MainActivity.java` and `GameView.java` (the
   listener), and the contract test.
5. **Docs:** `config-menu.md` (a section for hosts that draw their own menu),
   `input-and-modes.md` (the press and Escape rows), `multi-platform.md` §8 (the
   decision, and the browser's gap until phase 6), `android/README.md` (Controls),
   `vif.6`, `term-index.md` (menu, `menuPresentation`, snapshot), and `todo.md`
   (the Android item closed, the browser's left open).
6. **Browser**, as §8 describes, as a task of its own.

## 10. Verification

- Go gates: `go build ./...`, `GOOS=js GOARCH=wasm go build ./...`,
  `go build -tags vif_headless ./...`, `gofmt -l` on changed files,
  `go test ./internal/mode ./internal/app -run <the menu tests>`, and
  `go -C android test ./...`.
- Tests, one per rule:
  - every node's kind matches its fields, and every key fits the id encoding;
  - a native op and the key path reach the same state, for each kind;
  - an op on a vanished id is dropped;
  - an op on a disabled id answers with its reason;
  - a failed submit keeps the form and its error, and a successful one closes it;
  - `search` finds what `/` finds;
  - `close` resumes a game the menu paused;
  - the snapshot is pushed only when it changes;
  - the Java contract matches Go's.
- Java: type-check with `javac` against Robolectric's `android-all`
  ([agent notes](agent-notes.md#toolchain)). A container that cannot reach the SDK
  hosts cannot build the APK, so the device pass is the user's:
  - open the menu by each summon;
  - try every kind, a search, and a one-field form;
  - fill the request form with the keyboard up;
  - rotate the device with the menu open;
  - press Back at each layer;
  - join a session as a guest, where the host-only rows are disabled;
  - use it with TalkBack.

## 11. Risks

- **Live values:** values, labels, disabled reasons and run-time rows change while
  the menu is open: a bot joins then leaves, the host pauses, the session summary
  changes. Controlled widgets and keyed in-place updates handle this. A confirm
  dialog open on a row that disappears cancels itself.
- **Ordering:** snapshots come from the loop's goroutine and reach the UI thread in
  order through `runOnUiThread`. Ops go the other way through one channel, also in
  order. A tap made on a stale snapshot is resolved by id against the current tree.
- **Cost:** the byte comparison keeps pushes rare, but a full-tree build calls every
  `read`, `name` and `disabled`, and it runs every frame while the menu is open. The
  benchmark decides whether it moves to the input tick instead.
- **Parity:** the `tui` menu drills into a page while the native one expands it in
  place. The ops, the Back order and the keys of §7 keep everything else alike.
  `multi-platform.md` records the difference as one the host forces.
- **Form errors** belong to the form, not to a field, so a message names its field
  ("Bot count must be …").
- **Verification:** a cloud container has no emulator, so the user's device pass is
  the acceptance.

## 12. Open

- Whether `sfx_volume` becomes a slider: a `step` keeps it to one op, while a slider
  needs `set` with a number.
- Whether a search result's page expands around it when tapped, or the result acts
  in place as the `tui` search's does. Recommended: it acts in place.
