# Agent Notes

What agents working on this repository learned that no rule in
[AGENTS.md](../AGENTS.md) and no design document states: facts that cost a session
an hour to rediscover, and how to check them. Read it before starting. When a task
teaches you one, add it in the same PR, one bullet under its topic; delete a note
that proves wrong or that a document now states, and link the document instead.

## Toolchain

- The `gofmt` on a container's `PATH` can be older than the module's Go and reject
  generic methods ("method must have no type parameters"); run
  `"$(go env GOROOT)/bin/gofmt" -l`.
- `android/` is its own module: a dependency bump in the root `go.mod` also needs
  `go -C android mod tidy`, which `android/toolchain.sh` checks before Gradle. A
  container without egress to the Android SDK and JDK hosts cannot build the APK;
  that check runs on a device.
- The Java app type-checks without an SDK: `javac -cp android-all.jar` (Robolectric's
  `org.robolectric:android-all` from Maven Central, the release `compileSdk` names)
  over `android/app/src/main/java` and stubs for `R` and the bound `Game`, `Speaker`
  and `Listener`.
- `adb shell input` taps one finger only, and the emulator's user image refuses
  `sendevent` (SELinux). `adb shell uinput -` registers a touchscreen from JSON and
  injects multi-touch frames with millisecond delays, about 3 s after it starts. Its
  first frame is stamped with the register's time, so inject one with no finger
  down first, or a first tap reads as a 3 s press. `dumpsys window` puts
  `visibleFrame=` between the keyboard inset's `frame=` and `visible=`.
- After bumping `lixenwraith/terminal`, build `./...`, not only `./cmd/vif`: the
  tools and sandboxes break on a change the game does not use.
- `deploy/docker/Dockerfile` pins the Go image's index digest: HEAD
  `registry-1.docker.io/v2/library/golang/manifests/<tag>` with an `auth.docker.io`
  pull token and the OCI index Accept header, and read `Docker-Content-Digest`. HEAD
  the current pin first, which must match. Docker Hub answers a shared egress 429
  at times: retry after a pause.
- Every shipped target is 64-bit, and `internal/core/arch32.go` refuses the rest.
- `bindGo` strips the Android library (`-s -w`), so `go tool nm` reads nothing in
  it: bind with `-ldflags=-w` to sum `go tool nm -size` by package. A generic over
  component structs compiles once per component, and each copy's name spells the
  struct's fields, costing a third as much as its code.

## Ticks and events

- An event a system pushes in its `Update` dispatches in the close settle, after
  every system's `Update` that tick
  ([runtime §5](runtime.md#5-simulation-tick-steps)). A consumer that must act on it
  inside the same tick, before its own motion, reads a derived resource the producer
  writes instead, as the particle system reads `CaveResource` and the glyph spawner
  `DriftResource`.
- A derived resource is created in `engine.newGameContext`, rewritten by its owner
  every tick and never captured, so it bumps no schema. Systems run in ascending
  `Priority`, so only a later system reads it the same tick.
- A journaled event, of any origin but a system or a device, is never pushed inside
  the tick body: the close settle would dispatch it live while a replay injects it
  after the tick.
- A clock started inside a tick before its owner's `Update` shows one tick short of
  its span ([runtime §5](runtime.md#5-simulation-tick-steps)).
- A mount creates the pulse it fires in its own `Update` rather than by request, as
  missiles are: a request dispatches in the close settle, a tick behind the machine
  that armed the mount, and the pulse, which holds with that machine, falls out of
  step with it and lingers into the cooldown.
- A new bus crossing whose payload names its producer's cursor or slot adds a row to
  `crossingActors` (`internal/system/network.go`); without one, a misbound or
  modified participant can act as another's cursor through it.
- A joiner's arrival can reach it inside a correction rather than as the crossing,
  which the capture's fence then drops: state a joiner takes on arriving is derived
  in `RebindCursorRoster` too, never only in the arrival's handler.
- A joiner's world holds no entry for it until its arrival crosses there, while the
  authority's may already: what the run itself is, a bot seat, is its own word
  (`Instance.Seated`), never read off its roster entry.
- `NetworkSystem.Flush` runs at the tick's close under the world lock, after the
  close settle, so a hold it releases at a crossing's apply tick sees everything
  that crossing paid in that tick (`NetworkSystem.crossSettled`).
- `meshSession`'s idle cursors hold no energy and run out of heat near tick 29, so
  the game resets near tick 35; a session test that waits a fixed count of ticks
  past that reads the reset. Wait for the condition, bounded.
- `Config.Validate` refuses a rate on a driven run that is neither a script nor a
  bot: a harness's or a replay's clock takes one through `TimeCtl` instead.
- A run's tick is `World.TickInterval`, which its host may choose (`-tick`);
  `parameter.GameUpdateInterval` is only the default. Code converting ticks to time
  reads the run's, as tests may keep the constant.
- `World.CopyIn` overwrites `Config`, `Time`, `Player`, `Transient` and `View`
  whole, so a goroutine off the world lock (a bot seat, the drive loop, the
  correction loop) reads none of their fields, even one that never changes: the
  race detector flags the copy's write.
- `GameState.Mode` is the play mode (Normal, Visual, Insert, Search), which the
  world reads and the journal records; Command and Overlay are the operator's, held
  on the context over it (`GameMode.Operator`), and reach neither. A live run's
  mode changes only through the router (`transitionMode`), which keeps the input
  machine in step: a path that sets it elsewhere leaves every key but Ctrl+Q dead.
- What only the operator does or sees, a pause, an overlay request, the speakers,
  is pushed `OriginDevice`: never journaled and outside the event tallies, so a
  replay, which has an operator of its own, still matches. One left on the ambient
  origin diverges every replay whose recording made it.

## Determinism and the harness

- `TestSystemDomainProfiles` holds each system to its declared domain: a Shared one
  touching a Player-only store needs an allowlist entry with its reason (the wall
  system's `"wall:Particle"` mask lookup), while a Player system may read Shared
  state.
- The divergence harness
  ([netcode design §3.5](netcode-design.md#35-the-divergence-harness)) compares two
  binaries: `git worktree add` main, build `go test -c ./internal/app` in each tree,
  run the four shapes on both from their package directories, a second or so each,
  and read late commits and late copies; pages repaired move with any change of
  timing.

## Tests that flake

- The cadence tests (`internal/app/cadence_test.go`) run a shaped mesh on its
  virtual clock and are deterministic: one fails every run alone or never, so it is
  not load. A capture a few bytes larger or smaller reshapes every publication over
  a thin link: bisect, then print the host's `LinkMetric` and `Cadence()` each tick.
- A few `internal/app` tests over real sockets have failed on main alone, rarely and
  more under `-race` or load: a relay-join hold and a mid-session journal test. A
  failure there takes about 16.5 s, a 15 s socket wait running out. Before calling
  one a regression, run it alone in fresh processes, alternating main's binary and
  the branch's under the same load.
- A race only CI saw: build once with `go test -race -c`, then run the test in
  several processes at once; load exposes it, quiet runs do not.
- A race between a test's write and another goroutine's read reproduces at will:
  sleep that goroutine before its read and move the write ahead of the sleep. Print
  from neither side, as two writes to `os.Stderr` order them and hide the race.
- `internal/app` tests start no log sink, so a socket test that times out says
  nothing of why: put `fmt.Fprintf(os.Stderr, ...)` probes in a `git worktree`
  copy, build its race binary there and run it under the same load, keeping the
  branch clean.
- In a CI log a `--- FAIL` line sits inside its package's block, not at the end:
  save the whole log and search it. For a hang, find `panic: test timed out` and
  match the goroutines blocked in `sync.Mutex.Lock` against what each holds.
- The mixer drops ticks under load, so audio time lags the wall clock and one tick
  can take 200 ms under `-race`: a test waiting on mixer state gives it seconds, not
  periods. Load `cmd/soundlab` with ~24 race processes pinned to one CPU
  (`taskset -c 0`), or force it with a sleep at the top of `renderTick`.
- A closed listener's port is no refusing address: under `go test ./...` another
  package's listener can be given it and answer. Dial the near end of a held
  connection instead, which stays bound with nothing listening (vif-router's
  `TestEitherRouteRefusesBeforeDialling`).

## Locks

- The order is the world lock (`engine.UpdateMutex`, taken by `RunSafe`, not
  reentrant), then `App.sessionMu`, then converge's `Authority.mu`. Nothing floods
  the mesh under `sessionMu` or the world lock; a flood runs in the correction loop,
  between ticks.
- A flag a goroutine reads as it starts is raised before the call that starts it,
  and lowered again if nothing started: `Instance.Redial`'s dial loop may read
  `Authority.Redialing` before `Redial` returns, so `beginSuccession` and
  `redialRelay` raise their wait first.
- `Router.ProcessMacroTick` runs off the world lock (`loop.go`, `headless.go`): a
  component read there races. Read a status cell, or take `RunSafe`.
- Tests reach a play run's paths by switching a running App's `cfg.Mode` under
  `RunSafe`, so a goroutine that reads the run's config off the world lock (the
  drive loop, a bot seat) reads the copy it started with instead.

## Position

- The index's bounds are its grid's: every writer of `Config.MapWidth` resizes the
  grid before a query runs (`SetupLevel`, the reset, a crop, `CopyIn`), and a new
  writer must too, or every wall query and bounds check answers for the old map.
- An indirect call on a per-cell probe shows: reading the bounds through an
  interface on each `Blocks` cost `BenchmarkGlyphRender` a quarter, and an
  interface over the wall entities a tenth more. Measure a probe path with the
  renderer's benches, not only the tick's.
- The game reads the package's types by the package's names (`position.Scope`,
  `position.Pattern`), never through an engine alias: a method cannot be declared
  on an alias, and a second name sends a reader to two docs. A scope selects a
  class through `engine.EntityClass(e)`, which the two guard arrays in
  `internal/engine/position.go` hold to the domain numbering.
- `pkg/position` knows no entity: a test over walls or glyphs needs a world and
  stays in `internal/engine`; a grid-only test goes to the package with a test id
  whose class is its top byte.
- A block's kinds are its mask bits, and the engine reports a wall's box style as
  two of them (`component.BoxKind`) beside the movers it stops, so `WallBlockAll`
  is the four mover kinds and never `0xFF`: a query with it must not match a
  styled wall that stops nothing.
- The flat queries (`Blocks`, `EntitiesAt`, the searches) answer for the first layer
  and the `Point3` forms (`BlocksAt`, `EntitiesIn`) for any, so a flat game's call
  sites never gain a layer argument, and the mutators take a `Point3` the binding
  makes with `Point.Layer(0)`; the planes' and the presence bits' rows are
  the layers' in turn, so a cell's grid index `i` is `(i%Width, i/Width)` in them
  whatever its layer.
- `World.Positions` embeds the index, so the grid's writes (`Set`, `RemoveEntityAt`,
  `Load`, `Reorder`) are reachable from every system and move no row: a placement
  goes through `SetPosition`, `Move` or `Shift`; `Resize` and `Clear` are the
  binding's, placing the rows again or dropping them with the rock.
- A wall plane is brought to a cell when `BlockChangedAt` runs, so the binding
  calls `wallMoved` only once the grid holds the move: a wall logged before its
  insert leaves its bit stale, which no test but the fuzz test's oracle, a scan of
  the cell's own wall components, catches, since every other reader asks the plane.
- A change to a package's API is compiled against every test file by
  `go test -count=1 -run '^$' ./...`, which builds each test binary and runs
  nothing; `go build ./...` and `go vet` on the touched packages miss a test in a
  package the change never touched.

## Glyphs and colours

- A new non-ASCII rune in a drawing package or a shipped scenario's TOML needs the
  desktop font atlas regenerated (`tool/font-atlas/README.md`), and only
  `go test ./internal/asset`, a package the change never touches, catches it.
  Regenerate into scratch and diff: only the new glyph changes.
- Linux console fonts (console-setup's Uni and Lat sets) have █ ░ ▒ and single box
  lines but no ▀ ▄ ▌ ▐, quadrants or ▓, and draw double lines single; the 256 tier
  uses only what they have.
- The 256 tier's entries 0–15 take an emulator's theme, so no contrast between two
  of them is promised; a colour, or 15, on entry 0 is safe.
- On the Linux console the 256 tier shows sixteen colours: a pale tint over a rim
  falls to the rim's own, so a glow that must read there is near white, as the
  Wraith field's is.

## Looking at the game without a device

- Run `bin/vif` in a tmux pane (`-nw` starts it without waiting for a key) and read
  it with `tmux capture-pane -p -e`, which keeps the SGR colours; turned into HTML
  spans and shot with Playwright, it shows real colours. `TERM=linux COLORTERM=`
  gives the console tier.
- Drive it with `tmux send-keys`: `:emit <Event> { ... }` raises any event, `:god`
  arms every weapon, `a` toggles auto-fire and `:log on` writes the JSONL log under
  `~/.local/state/vif`, where every install logs; vif never writes `/var/log`.
- Xvfb, Alacritty and xdotool install in an Ubuntu container for real key and mouse
  input: `xdotool key` types, and tmux inside the window reads the screen back.
- A headless probe in `internal/app` drives a scenario by intents
  (`input.IntentFireMain`) and reads the world under `RunSafe`. A vanguard crush
  starts a new game, which `Positions.Rock().Origin` stepping back marks, so a probe
  clears what it recorded there.
- The browser launcher runs end to end in the container: `GOOS=js GOARCH=wasm go
  build -o <copy>/vif.wasm ./cmd/vif` into a copy of `web/`, serve the copy with
  `python3 -m http.server` and drive it with Playwright. `?arg=-join=ws://127.0.0.1:9/x`
  ends the run at once with a refused join's message.
- Whether a motion reads smooth is a frame question, not a tick one: render each of
  a tick's three frames with the context's `GameTime` set that far past
  `Time.PaceTime` and count the cells the layer moved. A pace off a multiple of
  twenty a second, drawn only where its tick left it, moves in uneven steps however
  fast it is.

## Measuring

- Benchmarks in a cloud container move a fifth between runs, and benchstat is not
  installed: build main's and the branch's test binaries (`go test -c` in a `git
  worktree` of main) and compare medians of alternating runs.
- Navigation's cost in vanguard depends on how the ship is flown: one that keeps
  still keeps sight of the raiders and shows little of it. Measure as a player
  flies, headless under `:god` with auto-fire, the ship sweeping between the map's
  middle and near its left edge every three seconds; a group cache's
  `FlowField.CurrentGen` counts the fields actually computed.
- A headless test reads the checkout's `wad/` (`soakRoot`), so main's test binary run
  from the branch's package directory reads the branch's scenarios and refuses a key
  main does not know: run main's from its worktree's package directory.
- Navigation's per-tick wall path is read by two benches with opposite shapes:
  `BenchmarkNavigationWallMoves` moves one wall, `BenchmarkCaveStepNavigation`
  turns hundreds of cells a step. A cost per changed cell shows in the second
  alone, a cost per row or per map in the first alone, at the largest map; read
  both before calling a change to `applyWalls` or `CompositePassability` neutral.
- Vanguard's fourth-rank raiders fly 4.5 cells a second, so a raider that moved two
  cells in half a second is not stuck; read a stuck raider as one out of sight a full
  second that moved less than a cell in it.
- tmux can throw frames away while vif sees nothing: it reads a pane at once, so
  writes return and held frames stay near zero. Attach a client (Alacritty under
  Xvfb, or a Python pty reader sized with `TIOCSWINSZ`) and poll `tmux list-clients
  -F '#{client_discarded}'`; a rising count means the client is repainted every
  100 ms. To count what a client saw, replay its byte stream through `pyte`,
  sampling 60 times a second, and count the samples where a pane changed.

## Scenario TOML

- A payload's untagged fields decode by their Go names: a wall batch's cells take
  `OffsetX`, `OffsetY` and `WallVisualConfig = { ... }`, and a rune field such as
  `char` takes the character's code, `43` for `+` (`wad/scenario/dummy`'s pads).
- An event drop belongs to a cursor, so drops a scenario lays before
  `EventCursorSpawned` go to nobody: lay them on that event.
- A drop table's rolls are player-domain, each instance rolling its own: a test of a
  kill whose drop is below certain shoots until the table's pity makes it certain
  (vanguard's U within two kills), and counts a kill's drops as the loot new since
  it, since earlier drops may still be flying (`internal/app/vanguard_test.go`).

## Words

- Materialize names the materialize effect alone; a teleport's or a charge's line is
  a line, never a beam.

## Branches

- A session pushes only its own branch and a force push is refused, so a branch
  whose PR merged starts again from main only once its owner deletes it: ask.
- Take the next `snapshot.Schema`, `event.JournalSchema` or `manifest.SchemaVersion`
  past main's, and check again after merging main, where another PR may have taken
  it.
