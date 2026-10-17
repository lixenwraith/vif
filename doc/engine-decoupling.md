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
  the renderers, visual parameters, mode and app draw 122 distinct non-ASCII
  runes (box drawing, block elements, geometric shapes, arrows, a few symbols)
  beside printable ASCII, 217 glyphs in all; the corpus and every shipped scenario
  are ASCII; `consoleGlyphs` names the 64 a text console must have. The splash
  font (`internal/asset/splash_font.go`, 95 glyphs of 12x12 bits, edited by
  `tool/font-editor`) is the precedent for a bitmap asset.
- **Cell aspect.** `vmath.Aspect` (§8) scales every authored vertical extent,
  but mount shapes, the 2x1 swarm and every `.vifimg` are cells whatever the
  aspect: a surface with square cells draws them twice as tall. A portrait phone
  shows fewer rows than columns of a landscape terminal, and the map floor in a
  scenario decides whether the view crops.
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
- **The pipe variant, measured.** Routing the terminal module's byte output
  through a pipe into an emulator widget would work with any terminal emulator
  and change nothing in vif, but it serializes a frame to ANSI and parses it
  back on the other side. Benchmarked on the module's own `outputBuffer` with
  120x40 game-like frames (every cell has a background and a glyph):

  | Path | Per frame | Bytes per frame |
  |---|---:|---:|
  | ANSI serialization, 15 % of cells changed | 200 µs | 24,822 |
  | ANSI serialization, every cell changed (a strobe, a resize) | 550 µs | 102,300 |
  | cell copy to a `Surface` | 2.5 µs | 57,600 |

  At 60 fps the pipe spends 12 ms a second serializing before the emulator has
  parsed a byte, and a parser costs at least as much again; the cell path is
  under 1 % of that and already exists. The module's `Backend` interface is the
  seam for a pipe (`Write`, `Read`, `Size`, `SetResizeHandler`) and is
  unexported today: `New` takes only a color mode. One exported constructor would
  open it for tests and tooling; it is not the product path.

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

## 7. Decisions taken and the plan

Decided after the first review of this document.

- **D1, vocabulary: keep.** `terminal.Cell`, `Attr`, `ColorMode` and `Key` from
  `lixenwraith/terminal` are the renderer-neutral model. The four values in
  simulation packages (§3) are therefore not leaks and do not move; the 14 KB the
  headless binary links is accepted.
- **D3, input: stub first.** The first iteration is render-first. The host model
  carries an input source with the right shape and a terminal implementation;
  the Android implementation and `input.Event` are a focused task after it.
- **D4, font: a bitmap atlas as an asset, generated once.** A TOML document in
  `internal/asset/font/`, one table per glyph with its rows as bit strings, the
  glyph cell size in its header, loaded by the host into whatever texture it
  draws with. It covers the 217 glyphs of §4.3. The splash font is reused where
  its 12x12 cells fit (ASCII at a 1:1 aspect); the rest is generated once from a
  monospace TrueType by a `tool/font-atlas` program, so the font dependency (the
  `x/image` rasterizer, or an external converter) stays in the tool. WOFF2 is out:
  decoding it needs Brotli. A TrueType rasterized at load remains the later option
  behind the same loader; the document format is interchangeable.
- **D5, pipe: cell path.** By the measurement in §4.3 the pipe costs 80x the cell
  path and buys nothing the surface does not. The exported backend constructor in
  the terminal module is a tooling item for when a consumer exists.
- **D6, Android drawing: `SurfaceView` and `Canvas` for the pilot**, behind a
  Java-side cell drawer interface so a GLES drawer replaces it without touching
  the Go side; GLES if the pilot misses 60 fps on a target device.
- **D7, store index: noted** as a todo item without further analysis.
- **D8, menu: keep `tui`.** The overlay renderer already adapts forms into cells;
  the configuration menu stays as it is, and Android gets a different
  presentation (a radial menu summoned by holding a tap), a later task with a
  gameplay implication to resolve.
- **D2** is resolved by D1: nothing moves.

Each later phase is one PR.

1. **Host model, render-first: done.** `app.Presentation` is what the frame
   loop, the lobby and the playback viewer use of a screen (a service, the
   surface, size, blocked time, events in the terminal's vocabulary, pointer
   mode), and `terminalHost` is the terminal as one. `Config.Host` carries a
   platform's `Presentation` and its speakers' writer; a headless build has an
   empty `Host`. `AudioConfig.Sink` is the host backend, selected over any
   name. The entry point is `app.Run` with a `Host` set: a library binding
   wraps that call and nothing else. Providers and the dialer stay as they are
   (`resource.Options`, the build-tagged `network.dial`) until the Android
   asset provider needs more.
2. **Font atlas: done.** `internal/asset/font/mono.toml`, 191 glyphs of 12x24
   from DejaVu Sans Mono, generated by `tool/font-atlas` (Python, Pillow);
   `pkg/glyph` parses it and `Raster` draws a frame into RGBA, changed cells
   only, 0.5 ms for a 120x40 frame with 15 % changed and no allocation. The
   coverage test fails when a drawing package gains a rune the atlas lacks.
3. **Cell aspect: done.** `vmath.Aspect` is the one definition, the presentation
   reports it, `ConfigResource.CellAspect` holds it and the anchor carries it
   (§8). The simulation surface of both benchmark runs is unchanged at the
   reference.
4. **Android host.** `gomobile bind`, `SurfaceView` drawing the cell grid from the
   atlas, `AudioTrack` sink, asset provider, lifecycle hooks; touch mapped to
   intents as the focused input task; the radial menu after that.
5. **Desktop GPU surface** (optional): a `vif_gui` command through purego GLES or
   Vulkan over the same atlas, the software rasterizer as its fallback.
6. **Performance follow-ups** from §6 in order of measured weight: the wall
   epoch, then the maze payload; the index slice when its measurement is in.

## 8. Geometry flexibility: cell aspect and the position store

Two considerations shape the host model without asking for implementation now.

**The cell aspect is a parameter, not a constant.** A terminal cell is about
twice as tall as it is wide, and the code assumed 2:1 in 29 files across nine
packages. `vmath.Aspect` (width over height, `RefAspect` 0.5) is now the one
definition: `Y` scales a vertical extent authored at the reference, `YInt` a cell
count, `Circular` and `FromCircular` convert a vertical distance to and from
horizontal units. The presentation reports its aspect with its grid
(`Presentation.CellAspect`), `ConfigResource.CellAspect` holds it beside the
color mode, `RenderContext.CellAspect` carries it to the renderers, and every
consumer reads it there: `navigation.CostsFor` builds a field's or solver's
`Costs` (10, 20 and 22 at the reference; 10, 10 and 14 for a square cell) with
the steering unit vectors, `visual.ShieldConfigFor` and each inverse-radius pair
are computed where they are used, ascimage takes it as an argument and its
command as `-aspect`. Footprint radii (the quasar's and the snake head's
collision, the quasar shield) are cells whatever the aspect and do not scale. It
is simulation identity: the anchor carries it (`JournalAnchor.CellAspect`, schema
23), a guest adopts the host's as it adopts the map bounds (D-14) since two
participants at different aspects would steer differently, and the snapshot's
context line names it. At the reference every value is bit-identical to what the
literals gave; a graphical surface with square cells is the first consumer.

**The position store can leave the engine.** `engine.Position` is a sparse set
over `PositionComponent` with a dense `SpatialGrid` and the wall-grid cache, and
it couples to the world only for the component mask, to `component` for the
value and the wall masks, to `parameter` for the cell capacities and to `status`
for telemetry. Everything it answers, cell queries, area queries, line of sight,
free-space search, is 2D over integer cells. Extracted as a package under `pkg/`
that takes its value type as a type parameter and its blocking predicate from the
caller, as `pkg/navigation` already does, it could grow a third axis (a layer or
a height) without the engine knowing, and the engine would keep only the mask
bookkeeping. That is a direction for the design of `app.Host` and the aspect
parameter to stay compatible with, not work for this push.
