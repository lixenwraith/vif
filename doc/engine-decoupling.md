# Engine decoupling and the host model

Working document for the push that makes vif's engine usable under another
renderer and on Android. It records what the code is today, with numbers, what
this push changed, what the terminal-visualization rendering strategy costs and
risks, how the host adapters should plug in, and the decisions the next steps
wait on. [Multi-platform](multi-platform.md) holds the build-profile contract,
[Rendering](rendering.md) the compositor, [Behavior machines](machine-design.md)
the data-driven species this split has to leave room for.

## 1. Verdict on the "hyper-coupled" criticism

The criticism names the wrong place. `internal/gen-manifest` hardcodes no
component: it parses `internal/manifest/definition.go` with `go/ast` and emits
templates over whatever that file lists. The only name the generator knows is
`Position`, the store with a spatial index (30 references in `main.go`). `Heat`,
`Energy` and `Cursor` are rows of the game's manifest, which is data in the sense
this repository already uses: a new component is a row, never a generator change.

What is coupled is the package graph. `internal/engine` imports
`internal/component`, because the generated stores, the capture and the delta are
emitted into the engine package, so the engine cannot be imported without the
game's components. That is a build-time dependency, not a design that names game
concepts: `engine.Store[T]`, `World`, the scheduler, the event queue, `converge`,
`snapshot` and `network` are written over the generated `SharedWorldState` rows and
never over a named component. The one place netcode names cursor components is
`NetworkSystem.readCursorState`, the D-13 owner-authored set, and that is a game
system in `internal/system`, where it belongs.

Measured on the `vif_headless` binary (`go tool nm -size`, text and data bytes):

| Where the manifest expands | Bytes | Source |
|---|---:|---|
| Shared-world capture, install, delta and page rows | 876,588 | `snapshot_world_gen.go` (1,581 lines), `snapshot_pages_gen.go` (722) |
| `Store[T]` instantiations, 53 components | 98,741 | `component_store_gen.go` (786) |
| The rest of `internal/engine` | 316,766 | hand-written |
| `lixenwraith/terminal` linked into the headless binary | 11,926 | through `component`, `engine`, `pattern`, `system`, `core` |
| `lixenwraith/terminal/tui` linked into the headless binary | 2,411 | through `internal/mode`'s configuration menu |

The headless pod carrying the terminal module is a dependency leak, not a size
one: the linker drops all but 14 KB. The leak that matters for a port is the
vocabulary, listed in §3.

Where the engine is already generic, by evidence:

- `pkg/` imports nothing under `internal/` (`make arch-check`); `pkg/navigation`,
  `pkg/vmath`, `pkg/audio`, `pkg/genetic`, `pkg/linkpace`, `pkg/websocket` take
  game data from their callers.
- A driven `App` is a pure function of seed, config and injected events
  (`NewHeadless`, `Tick`, `Inject(intents)`, `Settle`), which is what the
  benchmark and the determinism check in §4 rest on.
- Presentation is selected by `!vif_headless`; the dedicated build compiles
  neither `internal/render` nor the renderer registry (§3 of
  [Multi-platform](multi-platform.md)).
- Input reaches the world only as `input.Intent` through `mode.Router`; bots and
  scripts produce the same values a terminal does.

## 2. What this push changed

Three commits, each gated by `go build ./...`, `gofmt`, the touched packages'
tests, and for the render seam the `vif_headless` and `js/wasm` builds.

**Benchmark and determinism check.** `BenchmarkHeadlessTicks` in `internal/app`
ticks a driven App: the embedded scenario played by one cursor at 160x50, and
`td` at 200x60, which an unattended run restarts every ~110 ticks, so its ticks
carry the maze spawn (50,000 walls) and the route graphs a level costs. Run with
`-benchtime=1500x` so every run simulates the same ticks of seed 42.
`SnapshotSimulation` after 1,600 ticks of both runs was recorded before the
changes and is byte-identical after each one.

| Run | Before | After store access | After navigation |
|---|---|---|---|
| `main-160x50` | 33 µs/tick, 2,503 B, 18 allocs | same | same |
| `td-200x60` | 1.85 ms/tick, 669,397 B, 65 allocs | 636,052 B, 63 allocs | 1.80 ms, 515,319 B, 36 allocs |

Tick time moved within noise (1.70–1.99 ms over six runs). The win is garbage:
td's allocation per tick fell 23 % and its allocation count 45 %. For the browser,
where one thread runs the Go runtime and its collector, allocation is frame time
([todo](todo.md#coarsen-navigation-fields-if-busy-maps-still-stall-a-browser-frame)).

**Store access** (`internal/system`). Four per-tick loops took a detached entity
list that `GetAllEntities` allocated every tick (composite, eye, loot, gateway);
each system now refills a slice it owns. Fourteen reads of a 256-byte `Combat` or
a 48-byte `Kinetic` by value, and the particle loop's copy-in and copy-out of two
components per particle per tick, became live pointers where nothing between the
read and the last use moves the store. The composite loop keeps its copy: a
snake's head and body headers are members of its root, so `syncMembers` can
remove a header mid-loop. Splash and drain keep theirs on purpose (their comments
say why). Component sizes, for the next such review: `SnakeHead` 2,104 bytes,
`Missile` 648, `Combat` 256, `Tracer` 232, `Weapon` and `Swarm` 152, `Splash` 128;
everything else is 120 bytes or under, where a copy is not worth a pointer's
contract.

**Navigation scratch** (`pkg/navigation`, `internal/system/navigation.go`).
`ComputeRouteGraph` allocated its Dijkstra scratch, penalty grid, corridor marks
and one search per route field on every call: about 3 MB per graph on td's
500x250 map, 62 calls in 1,600 ticks. `RouteSolver` keeps that scratch between
calls (`ComputeRouteGraph` stays, on a fresh solver). A removed target group's two
flow caches, 1.1 MB each, are reset and handed to the next group rather than
rebuilt; `FlowFieldCache.Reset` restores the built state so the D-17 throttle
phase is the new cache's. A graph's own fields are not recycled: `CopyState`
carries route graphs by pointer into a prediction world, so a graph is immutable
once computed. Two tests pin the rules.

**Render surface** (`internal/render`). The orchestrator used three of the
terminal's fifteen methods. `render.Surface` names them (`ColorMode`, `Flush`,
`Sync`); the terminal satisfies it unchanged, and a graphical or Android host
implements exactly that over the same `terminal.Cell` grid. The two test stubs
shrank from fifteen methods to three. This is the plug point of §5.

## 3. Status: what still names the terminal

Counted with `grep` over non-test files. "Vocabulary" is a value type the code
reads or stores; "host" is a call on the live terminal.

| Package | Files | What | Kind |
|---|---:|---|---|
| `internal/render/renderer` | 49 | `Cell`, `Attr*`, `ColorMode*` (60 `AttrNone`, 39 `ColorMode256`, 14 `Cell`); `overlay.go` and `pinned_state.go` draw with `terminal/tui` (77 refs) | vocabulary |
| `internal/render` | 7 | the same, plus `Surface` | vocabulary |
| `internal/app` | 10 | `Event`, `Key*`, `EventResize`, `DetectColorMode`, `Size`, `Blocked`, `SetMouseMode` | host |
| `internal/input` | 5 | `Machine.Process(terminal.Event)`, 14 `Key` refs, mouse buttons and actions | vocabulary |
| `internal/mode` | 6 | `Key*` in command editing; the configuration menu's form is `tui.FormState` | vocabulary, and the only `tui` user outside renderers |
| `internal/component` | 1 | `WallComponent.Attrs terminal.Attr` | vocabulary |
| `internal/engine` | 1 | `ConfigResource.ColorMode terminal.ColorMode` | vocabulary |
| `internal/pattern`, `internal/system/death.go` | 2 | `AttrFg256`/`AttrBg256` on wall cells | vocabulary |
| `internal/core` | 1 | `EmergencyReset` in the Unix crash handler | host |
| `internal/service` | 1 | `TerminalService`: `Init`, `PollEvent`, `PostEvent`, `Fini` | host |

The `Terminal` interface has fifteen methods; vif calls ten of them, in three
places: the service (lifecycle and polling), the app (size, mouse mode, blocked
time) and the orchestrator (the three of `Surface`). `lixenwraith/color` is used
in 82 files and is the RGB vocabulary everywhere, including event payloads; it is
pure Go, has no host, and is not a port problem.

So the renderer-neutral model the todo asks for is four value types (`Cell`,
`Attr`, `ColorMode`, `Key`/`Event`), all of them already plain Go values in a
pure-Go module with Unix, Windows and wasm backends. The decision in §7 is whether
to keep that module as the vocabulary and plug hosts underneath it, or to copy
those four types into vif.

## 4. The terminal-visualization rendering strategy

The plan: the game keeps composing a cell grid, and a graphical or Android host
draws that grid with a glyph atlas. Nothing in `internal/render/renderer` changes.

### 4.1 Pure-Go GPU access

- **Desktop, Vulkan or OpenGL without cgo.** Feasible. `ebitengine/purego` calls
  C ABI functions from dynamically loaded libraries without cgo (`dlopen` on
  Linux, FreeBSD best-effort; `LoadLibrary` on Windows). Ebitengine's 2.10 release notes (a draft when
  read) state its Linux/BSD and macOS implementations no longer depend on cgo on
  that basis. A `vif_gui` surface could therefore load `libvulkan.so.1` or
  `libGLESv2.so` at run time and keep `CGO_ENABLED=0`. It is a new dependency, and
  the window and input side (X11/Wayland protocol) is the larger part of that work.
- **Android.** `gomobile bind` produces an `.aar` through cgo and JNI; the Android
  build needs the NDK whatever the engine does. The engine stays pure Go; the
  adapter does not. Vulkan or GLES from Go on Android adds nothing over drawing
  from the Java/Kotlin side: the cell grid is 4,800 cells at 120x40 (58 KB of
  12-byte `terminal.Cell` values), and a `SurfaceView` with a glyph atlas draws it in one pass.
- **Fallback.** A software rasterizer into an RGBA framebuffer is pure Go and always
  available: 120x40 cells of 8x16 pixels is 960x640 pixels, 2.4 MB a frame,
  a memory copy per cell plus a blit, well inside a 16 ms frame on anything that
  runs the game. It is also the test harness for the glyph atlas.

Sources: the purego module page at pkg.go.dev (v0.10.1 lists Android amd64 and
arm64 as tier 1, FreeBSD as tier 2), the Ebitengine 2.10 release notes, and the
Ebitengine site's platform list, which marks Android as cgo-required.

### 4.2 What the strategy buys

- Every renderer, mask, blend, finalizer and the void margin stay as they are;
  `.vifimg` assets, the console palette and `-color` keep their meaning.
- The surface contract is three methods (§2) and `Flush` is already non-blocking
  by contract: a host that stops consuming holds the newest frame, as the terminal
  module does, and `Blocked` tells the frame loop to stop drawing.
- Determinism and the session fingerprint are untouched: presentation is not part
  of simulation identity, and a GUI guest and a terminal host already differ by
  more (audio, logging) without changing the fingerprint.
- Geometry flows through the existing `EventScreenResize`: the host reports its
  grid, `ConfigResource` crops or centres (D-14), the pointer transform
  `ViewportToMap` is the renderer's inverse, so a tap lands on the cell drawn
  under it.

### 4.3 Costs, pitfalls and risks

- **Glyph atlas.** The host takes on text. The set is bounded, not Unicode:
  `TestConsoleGlyphsAreInEveryConsoleFont` already confines effects to glyphs every
  console font has, and typeable text is printable ASCII. Rasterizing a bitmap
  font at load (the splash font in `internal/asset` is the precedent) avoids a
  font-shaping dependency; a TrueType path needs `golang.org/x/image/font`, which
  is a dependency decision (D4).
- **Cell aspect.** Navigation costs (`CostX` 10, `CostY` 20), mount shapes, the
  2x1 swarm and every `.vifimg` assume a 2:1 cell. The surface must keep cells at
  that ratio, so a portrait phone shows fewer rows than columns of a landscape
  terminal, and the map floor in a scenario decides whether the view crops.
- **Sub-cell motion.** Physics is `float64` but projection is `PointAt` (floor).
  A GPU surface shows the same snapping; smoothing would be a second renderer
  stack, not a surface. Accept for the first build.
- **Frame pacing.** The app draws every third of a tick (60 fps) on its own
  ticker. On Android the surface should post the frame to the UI thread and let the
  Go goroutines run; the browser's "one thread, Go returns only when idle" problem
  does not exist on Android.
- **Command mode and text.** `:` commands, search and insert need a keyboard. The
  browser launcher already shows a key strip on touch screens
  (`web/terminal.js`, `pointer: coarse`); the Android host needs the same or the
  system keyboard. Captured keys (`EventKeyCaptureRequest`) are scenario buttons
  and work as taps.
- **The pipe variant.** Routing the terminal module's byte output through a pipe
  into an emulator widget would work with any terminal emulator and change
  nothing in vif, but it serializes a frame to ANSI and parses it back on the
  other side. The module's `Backend` interface is the seam for that (`Write`,
  `Read`, `Size`, `SetResizeHandler`), and it is unexported today: `New` takes
  only a color mode and `newTerm(Backend, ColorMode)` is private. One exported
  constructor would open it. Keep it as a test and tmux-style option; the cell
  path is the product path.

## 5. Host model: how adapters plug in

### 5.1 What exists

- **Lifecycle.** `service.Hub` orders `Init`/`Start`/`Stop` by declared dependency
  and rolls back on failure; `ResourceContributor` is the only way a service
  reaches the world, by filling a capability field on `engine.Resource`.
- **Composition.** `App.initServices` calls `initPresentationService`,
  `initNetworkService`, `initAudioService`, then registers files and content.
  Each `init*` is chosen by build tag (`presentation_terminal.go` vs
  `presentation_headless.go`, `audio.go` vs `audio_none.go`, `network_socket.go`
  vs `network_wasm.go`). The mode predicates (`Presents`, `OwnsGeometry`,
  `OwnsInput`, `Audio`) decide which run gets which.
- **Entry points.** `app.Run(cfg)`, `app.Serve`, `app.PlayJournal`,
  `app.RunScript`, `app.RunBot`, `app.NewHeadless`. `Run` owns the terminal loop.
- **Input.** `input.Machine.Process(terminal.Event) *Intent`; `App.Inject(intents)`
  feeds intents straight to the router in a driven run; bots do the same.
- **Audio.** `pkg/audio` writes PCM to an `io.Writer` chosen by `BackendConfig.Type`
  (process stdin, OSS device, WAV file, discard).
- **Files and content.** `engine.FileProvider` and `ContentProvider` behind the
  file and content services; a transferred scenario arrives as
  `resource.Options.Provided`.

### 5.2 Proposed shape

One value, built by the entry point, carries every host adapter; the App composes
from it instead of from build-tagged methods:

```go
// app.Host is what a platform supplies: each field nil where the platform has none.
type Host struct {
	Presentation Presentation // surface, geometry, pointer and key source, blocked time
	Audio        audio.Sink   // an io.WriteCloser the mixer streams PCM to
	Files        engine.FileProvider
	Content      engine.ContentProvider
	Network      NetworkDialer // how a session is dialled or bound, if at all
}
```

- `Presentation` is the terminal today: `render.Surface` plus `Size`, `Events`
  (keys and pointer), `SetPointerMode`, `Blocked`. An Android host implements the
  same interface over a view; `presentation_headless.go` becomes a nil field.
- `audio.Sink` adds one `BackendType` whose writer is injected, so an Android
  `AudioTrack` or a browser worklet is a backend like `wav:` is, selected by the
  host rather than by name.
- Build tags then guard constructors only: `cmd/vif` builds the terminal `Host`,
  a `vif_gui` command builds the GL one, the Android library builds its own. The
  simulation, the manifest and the fingerprint do not see the field values.
- The hub stays. What makes it robust for this is already there: dependency
  order, rollback, the contributor seam. Two additions earn their place:
  `Hub.Register` refusing a service after `InitAll` (today a late `Register`
  clears the order and `StartAll` then starts nothing, silently), and a `Service` optionally implementing
  `Pause()/Resume()` so a host lifecycle (Android background, browser hidden tab)
  reaches audio and network through one call rather than through app code.

Two plug points must not be confused. Host adapters are I/O and plug through
`Host`. Simulation modules (components, systems, events) plug through the manifest
and the generator, and the way to add species without touching the manifest is
the data front-end in [Behavior machines](machine-design.md) §8, not a second
component registry: an external compiled component would need the generator to
read a second definition file and the fingerprint to cover it, which is possible
but has no consumer yet.

### 5.3 Android specifics

- `gomobile bind` the library entry point; keep `cmd/vif` the terminal CLI.
- Input: touch → `(x, y)` → cell → `Intent{MapCell}` through the router, the path
  bots use; no ANSI mouse encoding. A pointer-only game already works: the browser
  on a phone plays by tap today.
- Geometry: the view reports a grid from its pixel size and the 2:1 cell; rotation
  is an `EventScreenResize`.
- Audio: 44.1 kHz 16-bit stereo PCM, the mixer period `AudioBuffer`, into
  `AudioTrack` through the sink above.
- Files: the APK's assets through `FileProvider`; the user root under the app's
  files directory for journals and logs; `vlog` writes files, so nothing changes.
- Lifecycle: `onPause` pauses the time control and the mixer; `onResume` repaints
  whole (`Sync`) as a hidden browser tab does; a killed process is a dropped
  participant, which the session already handles.
- Networking: WebSocket to the site's route or TCP; both exist.

## 6. Performance findings beyond this push

From the td profile (1,600 ticks, including the resets):

| Finding | Evidence | Opportunity |
|---|---|---|
| `wallGrid.current` walks every wall to prove the cached wall grid current | 10 % of CPU flat, `WallTest.func1` 18 % cumulative, with 50,000 walls and two callers a tick | a wall epoch bumped by the one in-place `BlockMask` write (`handleMaskChange`) and by store mutation would make the check O(1); the scan exists only because that write is in place |
| Map lookups in the sparse-set index | `mapaccess2_fast64` 24 % cumulative | entity IDs are per-domain counters; an index slice per domain would turn the lookup into an array read; measure before changing `Store` |
| Maze spawn builds a 50,000-cell payload and the batch spawn grows four slices | 145 MB and 118 MB flat over 22 spawns | reuse the payload cells and pre-size the batch; event payloads are pooled elsewhere (`event.pool`) |
| Drain's exclusion map is built per spawn | 37 MB | a map the system keeps and clears |
| `SpatialGrid.Resize` | 68 MB over the run | one allocation per level; grow-only already |

The main scenario's tick is 33 µs and 2.5 KB: there is nothing to win there. The
`Store` copy-versus-pointer question is settled by the component sizes above; the
machine design's host contract (`GetPtr` hosts pass the live instance) is the
right default and the detached style is for the loops that reorder commits.

## 7. Plan and the decisions it waits on

Phase 0 is this push. Each later phase is one PR.

1. **Minimum visual contract.** Decide D1 and D2, then move the four leaks out of
   the simulation packages: `WallComponent.Attrs` and the pattern pipeline's
   palette flags, `ConfigResource.ColorMode`, and the death system's attribute
   test. The headless binary then links no terminal code, and the dependency
   check in `make verify` can assert it.
2. **Host model.** `app.Host` (§5.2), the terminal host built in `cmd/vif`, a
   nil host for headless, `audio.Sink`, and `Presentation` as the interface the
   frame loop and resize path already use. No simulation change.
3. **Library entry point and input source.** A constructor that takes a `Host`
   and a `Config` and returns the loop to drive; pointer and key events as a
   small `input.Event` the terminal adapter translates to, so `Machine.Process`
   stops taking `terminal.Event` (D3).
4. **Android host.** `gomobile bind`, a `SurfaceView` drawing the cell grid with a
   bitmap atlas from the Java side, touch to intents, `AudioTrack` sink, asset
   provider, lifecycle hooks. Target: the one-month minimally polished build.
5. **Desktop GPU surface** (optional, after 4): a `vif_gui` command drawing the
   same grid through purego GLES or Vulkan, with the software rasterizer as its
   fallback and test.
6. **Performance follow-ups** from §6, in order of measured weight: the wall
   epoch, then the index slice, then the maze payload.

Decisions:

- **D1. Vocabulary.** Keep `terminal.Cell`, `Attr`, `ColorMode` and `Key` from
  `lixenwraith/terminal` as the renderer-neutral model, or copy them into a vif
  package (`internal/core` or a new `pkg/cell`). Recommended: keep the module's
  types. They are plain values, the module is pure Go with three backends already,
  and 56 render files use them; copying buys a name and costs a conversion layer.
- **D2. Where the leaks go.** `WallComponent.Attrs` becomes a two-bit palette
  flag in `component` that renderers map to `Attr`, or stays `terminal.Attr`
  under D1. Under D1 it stays; only `engine.ConfigResource.ColorMode` moves to a
  render-owned setting, since a GUI host is always truecolor.
- **D3. Input boundary.** Keep `Machine.Process(terminal.Event)` and have hosts
  synthesize terminal events (cheapest; Android taps become mouse events), or
  introduce `input.Event` (cleaner; touches the keymap and five input files).
  Recommended: synthesize for the first Android build, introduce `input.Event`
  with the host model PR.
- **D4. Font.** A bitmap atlas shipped as an asset (no dependency, fixed glyph set)
  or a TrueType rasterized at load (`golang.org/x/image/font`). Recommended:
  bitmap, generated once from a monospace font by a tool under `tool/`.
- **D5. Terminal module.** Export a constructor taking a `Backend` so a pipe or
  an in-process emulator can be plugged for tests and tooling. Low cost, not on
  the product path; do it when a consumer exists.
- **D6. Android drawing side.** Java `SurfaceView` and `Canvas` first, GLES only
  if a device misses 60 fps. The grid is small; expect Canvas to be enough.
- **D7. Store index.** Whether to try the per-domain index slice in `Store`
  (24 % of td's CPU in map lookups). It touches the ECS foundation; it needs its
  own measurement on `main` as well as `td` before a decision.
- **D8. Config menu.** `internal/mode` draws the Ctrl-G menu with `terminal/tui`;
  it is the one presentation dependency in a common package. Move the form to the
  overlay renderer, or accept the 2.4 KB in headless. Recommended: move it with
  the host model, when the overlay gains a non-terminal drawer anyway.
