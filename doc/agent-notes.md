# Agent Notes

What agents working on this repository learned that no rule in
[AGENTS.md](../AGENTS.md) and no design document states: facts that cost a session
an hour to rediscover, and how to check them. Read it before starting. When a task
teaches you one, add it in the same PR, one bullet under its topic. Keep it short:
delete a note that proves wrong, whose trap is gone, or that a document or a code
comment now states, and link that instead.

## Toolchain

- The `gofmt` on a container's `PATH` can be older than the module's Go and reject
  generic methods ("method must have no type parameters"); run
  `"$(go env GOROOT)/bin/gofmt" -l`.
- `android/` is its own module: a dependency bump in the root `go.mod` also needs
  `go -C android mod tidy`, which `android/toolchain.sh` checks before Gradle. A
  container without egress to `dl.google.com` cannot build the APK; with it, the
  steps of `android/toolchain.sh install` by hand (the command-line tools zip it
  pins, then `sdkmanager --licenses` and the platform, NDK, build-tools and
  platform-tools) set the SDK up under `~/Android/Sdk` in minutes, and
  `sdk.dir=` in `android/local.properties` names it. Gradle then stops on Maven
  Central's 429 to the shared egress even for an artifact another repository holds:
  an init script hooking `beforeSettings` and re-pointing every `mavenCentral()` at
  Google's mirror (`maven-central.storage-download.googleapis.com/maven2/`), passed
  with `-I`, builds the APKs in about four minutes.
- The Java app type-checks without an SDK: `javac --release 17 -cp android-all.jar`
  (Robolectric's `org.robolectric:android-all` from Maven Central, at the release
  `compileSdk` names: `16-robolectric-13921718` for `android-36.1`) over
  `android/app/src/main/java` and stubs for `R` and the bound `Game`, `Speaker` and
  `Listener`; the jar's `UnsupportedAppUsage` warnings are noise.
- The emulator boots without KVM (`-no-window -gpu swiftshader_indirect -accel
  off`, about 14 minutes on 4 vCPUs) but runs some 30x slower than a phone, so
  Android's 5 s input limit swallows taps behind "isn't responding" dialogs: a
  device pass belongs on a phone or an accelerated emulator. `adb shell input` taps
  one finger; `adb shell uinput -` injects multi-touch from JSON, after one frame
  with no finger down, or the first tap reads as a 3 s press.
- After bumping `lixenwraith/terminal`, build `./...`, not only `./cmd/vif`: the
  tools and sandboxes break on a change the game does not use.
- `deploy/docker/Dockerfile` pins the Go image's index digest: HEAD
  `registry-1.docker.io/v2/library/golang/manifests/<tag>` with an `auth.docker.io`
  pull token and the OCI index Accept header, and read `Docker-Content-Digest`. HEAD
  the current pin first, which must match. Docker Hub answers a shared egress 429
  at times: retry after a pause.
- A session's scratchpad can sit on a `/tmp` that a reboot empties (a tmpfs on a
  development machine): keep a long task's notes and working files in the
  checkout's `tmp/`, which `.gitignore` holds.
- `qemu-user-static` from apt runs a test binary built for another architecture
  (`GOARCH=arm64 go test -c`, then `qemu-aarch64-static ./x.test`): arm64's NEON
  path under `GOEXPERIMENT=simd`, and a big-endian `s390x` build, run that way.
- A bump to a dependency's untagged release builds through a `go.work` whose
  `replace` names the checkout (a `use` still fetches the tag's `go.mod`). Its
  `go.sum` lines come from `golang.org/x/mod`'s `zip.CreateFromVCS` and
  `dirhash.HashZip` over the commit the tag will name, and verify once a squash
  merge keeps that tree.
- `bindGo` strips the Android library (`-s -w`), so `go tool nm` reads nothing in
  it: bind with `-ldflags=-w` to sum `go tool nm -size` by package. A generic over
  component structs compiles once per component, and each copy's name spells the
  struct's fields, costing a third as much as its code.
- `internal/app`'s tests do not build with `-tags vif_headless`: `config_test.go`,
  `geometry_test.go` and `join_press_test.go` drive the presentation that profile
  leaves out, so its gate is `go build -tags vif_headless ./...` alone.
- `go generate ./internal/manifest` type-checks `internal/event` from source, its
  generated files included, so removing a payload struct, a field of one or an
  event stops the generator on the stale use of it ("type-check ...: has no field",
  "undefined"): delete those lines from `payload_gen.go` and `registry_gen.go`
  first, and the run writes them anew.
- A change to a package's API is compiled against every test file by
  `go test -count=1 -run '^$' ./...`, which builds each test binary and runs
  nothing; `go build ./...` and `go vet` on the touched packages miss a test in a
  package the change never touched.
- Go 1.27 takes a promoted field as a struct literal's key, though not through an
  embedded pointer, so fields moved into an embedded struct keep every literal,
  selector and method call compiling.
- `go install golang.org/x/tools/cmd/deadcode@latest` outside the module builds with
  the tool's own minimum Go (1.26), which refuses this module ("package requires
  newer Go version go1.27"): install it, and staticcheck, under
  `GOTOOLCHAIN=go1.27.3`.
- deadcode reads one profile: `pkg/browser`'s `keyCode` reads dead on linux and is
  live on js/wasm, `glyph.Texture.Atlas` and `Frames.ColorMode` on js/wasm and
  Android. Call a function dead only where every profile compiling its file reports
  it. Its `-test` run stops on `vif_headless` and windows, where `internal/app`'s
  tests do not build: list the packages without it. The Android module has no
  `main`; a scratch one under `android/tmp/` calling every bound function, run with
  `GOOS=android` and `-filter '^github.com/lixenwraith/vif(/|$)'`, stands in for
  gomobile's wrapper.
- deadcode lists code that is reserved and stays: `internal/system/composite.go`'s
  lifecycle methods (their todo item), `pkg/vmath` and `pkg/vmath/physics` (other
  repositories import them),
  `appendF32`/`readF32` (what gen-manifest emits for a float32 field) and the vlog
  stub's mirrors of live functions.
- A cloud checkout is shallow, about 50 commits: `git log -S` names the boundary
  commit as a line's origin. `git fetch --deepen=3000 origin main` reaches the rest.

## Ticks and events

- An event a system pushes in its `Update`, a crossing due at the tick and an FSM
  action's event all dispatch in the tick's one settle, after every system's
  `Update` that tick ([runtime §5](runtime.md#5-simulation-tick-steps)): a system
  first sees them in its next `Update`. A consumer that must act on it inside the
  same tick, before its own motion, reads a derived resource the producer writes
  instead, as the particle system reads `CaveResource` and the glyph spawner
  `DriftResource`, or makes the thing itself, as a mount creates the pulse it fires
  in its own `Update`.
- A derived resource is created in `engine.newGameContext`, rewritten by its owner
  every tick and never captured, so it bumps no schema. Systems run in ascending
  `Priority`, so only a later system reads it the same tick.
- A journaled event, of any origin but a system or a device, is never pushed inside
  the tick body: the tick's settle would dispatch it live while a replay injects it
  after the tick. The barrier's are the exception, stamped as the body's (settle
  group 0), which a replay queues for its own tick (`ReplayTarget.PushBody`,
  `EventQueue.Defer`) instead of settling before it.
- A new bus crossing whose payload names its producer's cursor or slot adds a row to
  `crossingActors` (`internal/system/network.go`); without one, a misbound or
  modified participant can act as another's cursor through it.
- A joiner's arrival can reach it inside a correction rather than as the crossing,
  which the capture's fence then drops: state a joiner takes on arriving is derived
  in `RebindCursorRoster` too, never only in the arrival's handler.
- A joiner's world holds no entry for it until its arrival crosses there, while the
  authority's may already: what the run itself is, a bot seat, is its own word
  (`Instance.Seated`), never read off its roster entry. Its `LocalSlot` stays
  `NoPlayerSlot` until then, which takes the joiner's own ticks: a test waiting on it
  ticks the joiner (`arrival` in `host_test.go`).
- A guest's own crossings apply a lead after it produced them even with host and
  guest on one machine: the default lead plus the relay slack is four ticks. What a
  crossing's resolution draws (a hit's zap) shows that late on its producer, so a
  cell the producer captured is stale by then; draw a shot as it fires, its ends
  naming entities (`pushDrainBolt`).
- `NetworkSystem.Flush` runs at the tick's close under the world lock, after the
  tick's settle, so a hold it releases at a crossing's apply tick sees everything
  that crossing paid in that tick (`NetworkSystem.crossSettled`).
- `meshSession`'s idle cursors hold no energy and run out of heat near tick 29, so
  the game resets near tick 35; a session test that waits a fixed count of ticks
  past that reads the reset. Wait for the condition, bounded.
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
- `World.PushLocal` stamps player in any scope, and the FSM bridge pushes every
  class but local in the ambient domain, Shared during a region's actions. A
  request a region raises on every instance that changes Shared state is
  `[stamped]`, as `EventFuseRequest` is: Shared from the region, player from a
  participant's own producer's `PushLocal`; a `[local]` one may not reach Shared
  state (D-10).
- A push is its participant's own only inside the producer scope the world's update
  loop and the scheduler open around a player-profile system's code: a test calling
  that system's `Update` or `HandleEvent` directly pushes nothing as its own, so a
  crossing it expects needs `EventQueue.SetOwn(true)` around the call, or the real
  loop (`TestAPushCrossesByItsProducersDomain`).
- What a Shared request does to Shared state may turn only on what every instance
  holds alike. An election or a guard that one instance alone sees decides nothing
  there: a fusion that consumes composites on every instance lands them by a Shared
  carrier on each, not by the elected instance's own (`FuseSystem.fuse`).
- A player-domain mechanic reading the whole roster (`ClosestCursor`,
  `CheckCursorOverlaps`, navigation's group 0) meets other participants' cursors,
  which never see it: `strikeCursor`'s owner gate stops the damage but not a
  knockback, a death or the crossing either pays. A drain locks on its own cursor and
  meets no other (`DrainSystem.handleDrainInteractions`).
- What only the operator does or sees, a pause, an overlay request, the speakers,
  is pushed `OriginDevice`: never journaled and outside the event tallies, so a
  replay, which has an operator of its own, still matches. One left on the ambient
  origin diverges every replay whose recording made it.

## Determinism and the harness

- A choice every instance must make alike is tested over two worlds holding one
  roster, each latched as its own participant (`World.AdoptSession`) and driving its
  own slot, fed the same events
  (`TestASweepNamingNoCursorTakesTheFirstParticipantsDrains`); a mesh session adds
  only the order crossings reach them in.
- `TestSystemDomainProfiles` holds each system to its declared domain: a Shared one
  touching a Player-only store needs an allowlist entry with its reason (the wall
  system's `"wall:Particle"` mask lookup), while a Player system may read Shared
  state.
- `TestNoOrderDependentMapRange` matches a ranged identifier by name against every
  map-typed name in `internal/system`: a local slice named `drains` trips on
  `ParticleSystem`'s map field. Rename the local, or allowlist it with its reason.
  It lints `internal/fsm`, `fsm/std`, `engine`, `event` and others too (`lintDirs`),
  keyed by function name, so renaming an allowlisted function there fails it.
- The divergence harness
  ([netcode design §3.5](netcode-design.md#35-the-divergence-harness)) compares two
  binaries: `git worktree add` main, build `go test -c ./internal/app` in each tree,
  run the four shapes on both from their package directories, a second or so each,
  and read late commits and late copies; pages repaired move with any change of
  timing.
- The harness's counts read the compared surface, which leaves out every D-13 cell,
  so an owner sync a receiver drops moves none of them. A change to what a sync may
  write is checked on a copy whose drop prints the sync instead
  (`NetworkSystem.stateInRange`), over the four shapes and the `internal/app` suite
  under `-test.v -test.parallel=1`, which puts each print under the test that sent it.
- A host admits `NetworkAdmitBurst` (6) dials from one address per
  `NetworkAdmitWindow` (a minute), a fixed window, and loopback is one address: a
  test lobby past six dials fills a window more per six (`lobbySession` sizes its
  wait so), a dial refused after admission counts and a budget refusal does not.
  A dial reaching a host with `MaxHandshakes` (8) handshakes in flight is closed
  unanswered, which the dialer reads as `io.EOF` ("connected, but no vif session
  answered there"), and is retried as a refusal is (`scriptedRun`).
- A test proven to pin a rule by mutating the rule's line runs from a copy of the
  tree (`tar` it out), so the working tree stays editable; a mutation that survives
  is run against main's own tests in a `git worktree`, where surviving too makes it
  an old gap rather than a lost pin.
- A correction leaves a cursor's owner-authored cells (D-13) alone four ways:
  `applyShard`'s owner check, the encoder's exclusion, the owner-authored cursor
  sets (`ownerAuthoredCursors`, `liveOwnerAuthoredCursors`) and
  `RebindCursorRoster`'s restore. Each masks a mutation of the others, so a test
  proving the rule turns them all off.
- A run's wiring travels only in a `runConfig` (`wiring`), never in a `Config`: a
  test that builds a joiner as a join does goes through `configForJoin` into
  `newDriven`, since `NewHeadless` on its `Config` drops the session's `-players`,
  and a test of what a join holds to then passes on nothing.
- The platform's `Host` reaches a run only in its `runConfig` (`host`), which `Run`,
  `RunScript` and `RunBot` fill from the `Host` handed beside the `Config`: a
  presenting entry point built on a `Config` alone draws on the terminal, not the
  window, page or Android view, and no test catches that for a watched script or
  bot. A test that presents builds its run with `newApp` on a `runConfig`.
- A type `internal/app` aliases from `internal/app/internal/config` carries its
  exported methods to `cmd/vif` and Android, which cannot import the package, and
  `app` cannot add methods to it: a rule that reads a run's wiring stays on `app`'s
  `runConfig`, as `runConfig.Validate` adds the wiring's rules to `Config.Validate`'s.

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
  several processes at once; load exposes it, quiet runs do not. Keep it near twice
  the CPU count: at five times, a socket join's catch-up is refused ("behind the
  session ... retry in a moment"), the harness's host ticking every 10 ms faster
  than the loaded joiner closes the gap, a refusal a production seat retries.
- `internal/app` tests start no log sink, so a socket test that times out says
  nothing of why: put `fmt.Fprintf(os.Stderr, ...)` probes in a `git worktree`
  copy, build its race binary there and run it under the same load, keeping the
  branch clean.
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

## Networking

- `io.Copy` between two sockets reaches Go's kernel `splice(2)` path only when both
  ends are a `*net.TCPConn` or `*net.UnixConn` themselves: a wrapper that renews a
  deadline on each write (the router's `stallBounded`) hides `ReadFrom`, so each
  direction copies through a 32 KiB user buffer. Put the deadline on the socket, not
  on a wrapper, before reading a splice's memory.
- `encoding/json` compacts a `json.RawMessage` when it marshals one, so a signature
  over raw JSON bytes survives a round trip only when the signer emitted compact JSON,
  which `json.Marshal` does; a token re-encoded with whitespace by another writer
  fails its signature, which is the intended refusal, not a bug to relax.

## Admission

- A challenge-response handshake cannot be fuzzed from a static script past its
  challenge: a proof over a random nonce is never in the corpus. The join target
  pins the nonce through `Coordinator.Nonce` and seeds proofs over it; the peer
  link target asserts instead that no script ever opens a link.
- A public key travels as its base64 string (`network.EncodeKey`), not `[]byte`:
  `RosterEntry` is compared with `slices.Equal` (`SameRoster`), a payload field is
  TOML a scenario could name, and the generated capture codec has string and slice
  kinds and no bytes kind. Decode at the boundary that verifies.
- A door that takes a pass names the session's first authority key, never its
  name: names differ along a route (the router rewrites a published door's; a
  relay's `SessionName` is the route it dialled), keys do not. Match passes by key.
- The term keys and the keep-out list are membership state, carried whole by the
  offer and the handoff record and fed by the departure crossing, not a captured
  resource: every door holds them with no snapshot schema or codec change.
- `auth`'s SCRAM client refuses a challenge below the cost it was built with, and
  an empty channel binding is its error, not its unbound: pass `WithChannelBinding`
  only over TLS (`bindingOption`), and read the leaf from under a websocket
  (`tlsOf`), since `*tls.Conn` has a `NetConn` of its own to stop at.
- A host that drops a run closes the link as the drop applies on its side, before
  the dropped run applies it; the dropped run reads the loss as its dismissal's
  (`NetworkSystem.dismissalScheduled`) rather than as a host lost, or it would
  redial under the key just kept out.
- The router intercepts exactly one frame each way of the admission exchange, the
  door's question and the dial's answer, and splices everything after, so a
  passphrase door's later steps pass through it untouched; a stand-in pod in a test
  hands its connection to `HostAcceptor` whole, which reads the route frame itself.
- nginx's `proxy_protocol on` writes a version 1 header and nothing else; the
  router reads version 2 too for other edges. A probe of the front door behind the
  edge writes the header first or is closed unanswered.
- A join address keeps its `vifs://` scheme (`Endpoint.Dialable`): `Joining` and the
  flags once stored the bare host:port, which dialled a TLS door in the clear. A
  sign-in is shown over TLS alone (`tlsOf`), whatever the door asks.
- An account command runs its KDF and requests in a goroutine and reports on the
  status line: a `SessionController` method runs under the world lock, which cannot
  wait on a site.
- A browser trusts a self-signed door only by being told to: pinning from a page
  needs WebTransport's `serverCertificateHashes`, which the route does not speak,
  so a page joins a player's door over the site or by a passphrase bound to that
  door's certificate; a native run pins the fingerprint the sessions API carries.

## Position

- [Position](position-package.md) states the index's rules: bounds that are the
  grid's, the package's names, flat and `Point3` queries, the planes
  `BlockChangedAt` keeps, `WallBlockAll`. What it leaves out:
- `pkg/position` knows no entity: a test over walls or glyphs needs a world and
  stays in `internal/engine`; a grid-only test goes to the package with a test id
  whose class is its top byte.
- A body's size is one `vmath.Footprint`, its header offset inside it, and a
  species' is read through `Resource.Footprint`, never as four ints or a parallel
  parameter: the searches, line of sight, passability tiers and a materialize take
  the struct, and an event's or a component's cell names the header, never the
  top-left.

## Glyphs and widgets

- A new non-ASCII rune in a drawing package or a shipped scenario's TOML needs the
  desktop font atlas regenerated (`tool/font-atlas/README.md`), and only
  `go test ./internal/asset`, a package the change never touches, catches it.
  Regenerate into scratch and diff: only the new glyph changes. The test reads
  every string in `internal/mode`, `app`, `help` and `prof` too, so a key nobody
  sees stays ASCII, and a comment after code on its line as code, so that comment
  does too.
- `tui.TextFieldOpts.Border`'s zero value is `LineSingle`, a box three rows high, so
  a one-row text box (the menu's) passes `tui.LineNone` or draws nothing.

## Looking at the game without a device

- Run `bin/vif` in a tmux pane (`-nw` starts it without waiting for a key) and read
  it with `tmux capture-pane -p -e`, which keeps the SGR colours; turned into HTML
  spans and shot with Playwright, it shows real colours. `TERM=linux COLORTERM=`
  gives the console tier. The capture carries SGR state from one line into the
  next, so a converter that resets colours at each line paints wrong backgrounds.
- Drive it with `tmux send-keys`: `:emit <Event> { ... }` raises any event, `:god`
  arms every weapon, `a` toggles auto-fire and `:log on` writes the JSONL log under
  `~/.local/state/vif`, where every install logs; vif never writes `/var/log`.
- `tmux send-keys` drops a `;`: one alone is its command separator and a trailing
  one in a `-l` string is cut, so `fx;;;;` sends three repeats; send `'\;'` as its
  own argument. A capture shows the cursor on a glyph as the glyph, so read the
  cursor from the telemetry overlay's player card (`:t`, `/player.0`) instead.
- `-l <dir>` is `-l=<dir>`: with a space the log lands under `~/.local/state/vif/log`.
- Xvfb, Alacritty and xdotool install in an Ubuntu container for real key and mouse
  input: `xdotool key` types, and tmux inside the window reads the screen back.
  Alacritty 0.13 from apt also needs `libxkbcommon-x11-0` and runs under
  `LIBGL_ALWAYS_SOFTWARE=1`; it speaks the key protocol, and Xvfb autorepeats a
  key held by `xdotool keydown` (660 ms, then 25 Hz), so presses, repeats and
  releases reach vif as from a desk. tmux in between drops the protocol.
  Alacritty also needs `libxkbcommon-x11-0` and `LIBGL_ALWAYS_SOFTWARE=1`, and with
  no window manager `xdotool windowfocus`, not `windowactivate`, gives it the keys.
  `ffmpeg -f x11grab` records the window to set against a probe's wall-clock lines.
- A headless probe in `internal/app` drives a scenario by intents
  (`input.IntentFireMain`) and reads the world under `RunSafe`. A vanguard crush
  starts a new game, which `Positions.Rock().Origin` stepping back marks, so a probe
  clears what it recorded there.
- The browser launcher runs end to end in the container: `GOOS=js GOARCH=wasm go
  build -o <copy>/vif.wasm ./cmd/vif` into a copy of `web/`, serve the copy with
  `python3 -m http.server` and drive it with Playwright. `?arg=-join=ws://127.0.0.1:9/x`
  ends the run at once with a refused join's message. A `devices['Pixel 7']`
  context is the phone's page, where a held `mouse.down` on the canvas's last
  rows is the finger that opens the menu; `page.keyboard.press('Control+g')` is
  the desktop's. `pkg/browser`'s tests run under node with
  `GOOS=js GOARCH=wasm go test -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec"`.

## Measuring

- A browser-build bench runs under Node (`BenchmarkKrakenRegion`'s comment), which
  refuses a run whose arguments and environment pass about 8 KB ("total length of
  command line and environment variables exceeds limit"), as a cloud container's
  environment does: run it under `env -i PATH=... HOME=/tmp`. An `internal/app` test
  file calling what `js/wasm` lacks takes `//go:build !js`, as `readings_test.go`
  does, or that package's wasm test binary stops building.
- Benchmarks in a cloud container move a fifth between runs, and benchstat is not
  installed: build main's and the branch's test binaries (`go test -c` in a `git
  worktree` of main) and compare medians of alternating runs.
- A renderer's first call in a live frame runs about twice its hot-bench time,
  its tables and code gone cold under the frame's other work: the ember paints in
  9-12 µs when repeated at once and 20-26 µs as the frame's first. Read a gain
  live, with `:d prof` under `:log on` (`prof.top` lines) or a `time.Now` probe
  round the call in a scratch build, before calling a bench gain felt.
- An indirect call on a per-cell probe shows: reading the bounds through an
  interface on each `Blocks` cost `BenchmarkGlyphRender` a quarter, and an
  interface over the wall entities a tenth more. Measure a probe path with the
  renderer's benches, not only the tick's.
- Find an allocation before removing it: `-memprofile` with `-memprofilerate 1`,
  then `pprof -sample_index=alloc_objects`. A concatenation that does not escape
  (`-gcflags=-m`) and fits 32 bytes stays on the stack: the overlay's titles a todo
  blamed never allocated, while `tui`'s box edges did.
- A renderer bench reusing one `RenderBuffer` calls its `Clear` each iteration, as
  a frame does, or `KeepExact`'s rects pile up as bytes with no allocation behind
  them. A bare `engine.NewWorld` draws as an emulator in 256 colors, `ColorMode`'s
  zero value.
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
- A payload's `duration` is nanoseconds (`3000000000` for three seconds), where a
  `duration_ms` field is milliseconds; `vif -schema` prints each field's type.
- `EventDustAllRequest` turns every glyph into dust that orbits the cursor for as
  long as nothing bursts it, over whatever is laid next: a scenario clears text
  with `EventGlyphClearRequest`. A transition between two children of one parent
  runs neither the parent's exit nor its entry, so a lesson's clear sits on the
  lesson, and a state re-entering itself runs no entry actions at all.
- A splash text cleared and written at one cell in one pass is one entity: the
  clear destroys it at once rather than by a death batch, or the write reuses an
  entity the batch then takes.
- A line delete's `EventDeleteRequest` carries rows alone (`start_x` stays 0), so
  a guard on `dd` compares `range_type` and `start_y`; `x` fills every field.
- Reading a pane: the status bar's first item is the FSM state and its time, the
  column ruler counts from the cursor, and the large glyph some way off the cursor
  is the magnifier, the glyph under it in the large face.
- The entry's top-level keys the FSM loader does not know are the embedder's
  (`resource.entryKeys` lists them), so a new entry key needs no loader change
  beyond reading its files into the container.
- A drop table's rolls are player-domain, each instance rolling its own: a test of a
  kill whose drop is below certain shoots until the table's pity makes it certain
  (vanguard's U within two kills), and counts a kill's drops as the loot new since
  it, since earlier drops may still be flying (`internal/app/vanguard_test.go`).
- The behavior vocabulary is built before any program compiles, so a verb whose
  arg names what programs extend (a species, a materialize style) registers with
  `Named`, whose build gets the set's payload names; `Effect` decodes without them.

## Corpus

- `vif -d -check` parses the embedded corpus and prints its block and line counts,
  the quick test of an edit to `internal/asset/content/tutorial.toml`. A line wider
  than the map is cropped (`GlyphSystem.placeLine`), so the tutorial's stay under
  40 columns.
- An edit to the embedded corpus moves every line a seeded test sees: lines land
  at random rows, and `w` without a ping keeps to the cursor's row, which a third
  of seeds leave empty for 160 ticks. A test that needs a glyph under the cursor
  steps rows until one is reached, never counts on its seed.

## Docs

- Nothing checks a doc link's file or anchor, so a broken one outlives sweeps.
  After editing links or headings, run a short script that slugs each heading as
  GitHub does (lowercase, punctuation and `§` dropped, spaces to hyphens) over
  every `*.md`; its only false hits are `machine-design.md`'s `New[S, C](observe)`
  and `Decode[A](raw)`.

## Words

- Materialize names the materialize effect alone; a teleport's or a charge's line is
  a line, never a beam.

## Libraries

- audio, stapes (formerly soundlab), genetic and ascimage are repositories of their
  own ([package map §8](package-map.md#8-external-module-boundary)): a change to
  one lands there, under its own `AGENTS.md` and agent notes, and vif takes its
  tag. Stapes keeps its verbs, the rip and the model's training; `make tools`
  builds ascimage's command from the version `go.mod` requires.
- The capture carries `registry.SpeciesState`, which embeds the streaming
  engine's checkpoint, so a change to `genetic.StreamingState`'s layout bumps
  `snapshot.Schema` beside `StreamingStateVersion`.

## Branches

- A session pushes only its own branch and a force push is refused, so a branch
  whose PR merged starts again from main only once its owner deletes it: ask.
- Take the next `snapshot.Schema`, `event.JournalSchema` or `manifest.SchemaVersion`
  past main's, and check again after merging main, where another PR may have taken
  it.
