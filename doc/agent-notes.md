# Agent Notes

What agents working on this repository learned that no rule in
[AGENTS.md](../AGENTS.md) and no design document states: facts that cost a session
an hour to rediscover, and how to check them. Read it before starting. When a task
teaches you one, add it in the same PR, one bullet under its topic. A note is a
trap or a technique the next task meets; how a feature works belongs in its design
document. Keep it short: delete a note that proves wrong, whose trap is gone, or
that a document or a code comment now states, and link that instead.

Topics: Toolchain · Events and ticks · Sessions · Runs and their wiring ·
Determinism and the harness · Session tests · Tests that flake · Locks · Networking
and admission · Position · Math and physics · Glyphs and widgets · Looking at the
game without a device · Measuring · Scenario TOML · Corpus · Docs · Libraries ·
Branches.

## Toolchain

- The `gofmt` on a container's `PATH` can be older than the module's Go and reject
  generic methods ("method must have no type parameters"); run
  `"$(go env GOROOT)/bin/gofmt" -l`.
- `git ls-files <path>` before creating a file: `internal/app` names its test files
  for concepts (`door_test.go`), and a write to a path that exists replaces it.
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
  checkout's `tmp/`, which `.gitignore` holds. A `.go` file there is a package to
  `go build ./...` and `go vet ./...` all the same: a scratch program or a copy of a
  source kept under `tmp/` stops both.
- `qemu-user-static` from apt runs a test binary built for another architecture
  (`GOARCH=arm64 go test -c`, then `qemu-aarch64-static ./x.test`): arm64's NEON
  path under `GOEXPERIMENT=simd`, and a big-endian `s390x` build, run that way.
- A bump to a dependency's untagged release: serve the commit the tag will name
  from a file `GOPROXY` (`golang.org/x/mod`'s `zip.CreateFromVCS` writes
  `<module>/@v/<v>.zip`; `<v>.mod`, `<v>.info` and `list` beside it), listed first
  with `GONOSUMDB` naming the module and a scratch `GOMODCACHE`. `go get`, then
  `go mod tidy` and `go -C android mod tidy`, write the `go.sum` lines, which
  verify against the tag once a squash merge keeps that tree.
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
- deadcode lists code that is reserved and stays: `pkg/vmath` and
  `pkg/vmath/physics` (other repositories import them), `appendF32`/`readF32` (what
  gen-manifest emits for a float32 field) and the vlog stub's mirrors of live
  functions.
- A cloud checkout is shallow, about 50 commits: `git log -S` names the boundary
  commit as a line's origin. `git fetch --deepen=3000 origin main` reaches the rest.
- Claude Code's Bash safety check refuses any command whose text holds `rm -rf` on
  a variable, a heredoc that only edits the Makefile's install recipe included:
  edit that recipe with the Edit tool, and read what `CLEAN=1` would remove with
  `make -n install-config CLEAN=1 VIF_CONFIG_DIR=<any path>`, never by running it.

## Events and ticks

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
- The settle dispatches pass by pass, breadth first: what a handler pushes runs
  after every event of the pass it ran in. An arrival and a departure applying in
  one tick both run before the cursor the arrival asks for is made
  (`rosterDue.spawning`), and what the departure asks of that cursor is queued
  behind the spawn.
- A journaled event, of any origin but a system or a device, is never pushed inside
  the tick body: the tick's settle would dispatch it live while a replay injects it
  after the tick. The barrier's are the exception, stamped as the body's (settle
  group 0), which a replay queues for its own tick (`ReplayTarget.PushBody`,
  `EventQueue.Defer`) instead of settling before it.
- A registered struct payload decodes from TOML, so every enum it holds at any
  depth must take names (`TestEveryEnumAPayloadHoldsTakesNames`), and a payload
  that is a component fails on that component's enums. Annotate the event with the
  qualified type (`(component.KrakenBurn)`), which registers nil: right only for a
  `[local]` event a system pushes, which is neither journaled nor sent.
- A run's tick is `World.TickInterval`, which its host may choose (`-tick`);
  `parameter.GameUpdateInterval` is only the default. Code converting ticks to time
  reads the run's, as tests may keep the constant.
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

## Sessions

- A new bus crossing whose payload names its producer's cursor or slot adds a row to
  `crossingActors` (`internal/system/network.go`); without one, a misbound or
  modified participant can act as another's cursor through it.
- A crossing only the authority may make joins the list in `admissibleFromSource`,
  and `barrierBound` when it sets shared state from one tick on; with no payload it
  needs no codec (`crossSession(et, nil)`). A new event type moves the number of
  every one declared after it, so it takes the next `network.ProtocolVersion` and
  `event.JournalSchema`.
- A joiner's arrival can reach it inside a correction rather than as the crossing,
  which the capture's fence then drops: state a joiner takes on arriving is derived
  in `RebindCursorRoster` too, never only in the arrival's handler.
- A joiner's world holds no entry for it until its arrival crosses there, while the
  authority's may already: what the run itself is, a bot seat, is its own word
  (`Instance.Seated`), never read off its roster entry.
- A guest's own crossings apply a lead after it produced them even with host and
  guest on one machine: the default lead plus the relay slack is four ticks. What a
  crossing's resolution draws (a hit's zap) shows that late on its producer, so a
  cell the producer captured is stale by then; draw a shot as it fires, its ends
  naming entities (`pushDrainBolt`).
- A reset clears the barrier and every crossing scheduled on it: what the authority
  still owes the session past one is state its system keeps and crosses again in
  the new game ([Multiplayer §5.4](multi-player.md#54-wraiths)). The reset's tick
  settles on after `MetaSystem.handleGameReset` cleared the world and closes with no
  flush; `Scheduler.executeReset` then takes the world lock again, and
  `NetworkSystem.Init` empties the barrier bar what `keepLost` keeps before the
  boot's spawn rebuilds the cursors. What a crossing applied at that tick raised,
  and a goroutine in between (a correction's off-tick drain, the accept loop), meet
  no cursor, only the roster waiting in `PlayerResource.Restores`: an event that
  writes every cursor or Wraith writes that roster too (`JoinRestore`,
  `RestartWaits`). A queue event pushed in between goes with the queue.
- A held host flushes nothing, so what it crosses under a hold reaches no guest,
  relay or successor until the resume
  ([Multiplayer §3.6](multi-player.md#36-holding-a-session)): a guest's barrier holds no
  roster change there, so a guest's rule reading `arrivalScheduled`, or a mark an
  arrival clears, needs another source.
- A session policy on the wall clock lives in `Authority.drive`, which the
  correction loop reaches between ticks and a driven run once per `App.Tick` call,
  however many ticks that advances. One that crosses needs a running transport,
  which a replay has none of, and restarts its own watch as it crosses, since the
  roster it reads (`WorldRoster`) shows the crossing only as it applies, a lead
  later.
- Session state a replay must follow changes only through the latch's note
  (`World.LatchSession`, `Supersede`): a flag an install or a link event flips with
  no note binds the replay's cursor at another tick. A change a dispatch makes needs
  none, since the replay dispatches too (`World.TakePlace`).
- A redial lends the run's place to its dial for the whole walk (`Membership.lent`),
  and `m.place` is nil meanwhile: what signs for the run, a link, a notice or the
  record that takes a term, reads `LocalKey`, which answers through the loan.
- What a player brings back to its Wraith (`event.CursorRecord`) is judged where it
  is written, `CursorSystem.restore` on every instance, never at the door: by the
  game it names (`Run` against the queue's run, which a reset advances) and by
  `heldInRange`, which the owner sync's check shares. A field added to the record
  that play bounds joins `heldInRange`.

## Runs and their wiring

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
- The run a restart builds starts from the launch line and what earlier restarts
  named (`runConfig.restarted`), never from the live `m.cfg`: what a command wrote
  there, a phrase, a policy, a door, is gone at the next restart unless the ask
  carries it (`RestartAsk`, `restartRequest`, which `replaceWith` alone latches).

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
- A test proven to pin a rule by mutating the rule's line runs from a copy of the
  tree (`tar` it out), so the working tree stays editable; a mutation that survives
  is run against main's own tests in a `git worktree`, where surviving too makes it
  an old gap rather than a lost pin. One that only fails to build, a name left
  unused, proves nothing: keep the name read (`false && x`).
- A correction leaves a cursor's owner-authored cells (D-13) alone four ways:
  `applyShard`'s owner check, the encoder's exclusion, the owner-authored cursor
  sets (`ownerAuthoredCursors`, `liveOwnerAuthoredCursors`) and
  `RebindCursorRoster`'s restore. Each masks a mutation of the others, so a test
  proving the rule turns them all off.
- The divergence harness
  ([netcode design §3.5](netcode-design.md#35-the-divergence-harness)) compares two
  binaries: `git worktree add` main, build `go test -c ./internal/app` in each tree,
  run the four shapes on both from their package directories, a second or so each,
  and read late commits and late copies; pages repaired move with any change of
  timing. Diff the two outputs whole, the link's B/s aside: the transitions
  logged with their ticks show a simulation that moved while every count stood,
  as a storm going active two ticks later did.
- The harness's counts read the compared surface, which leaves out every D-13 cell,
  so an owner sync a receiver drops moves none of them. A change to what a sync may
  write is checked on a copy whose drop prints the sync instead
  (`NetworkSystem.stateInRange`), over the four shapes and the `internal/app` suite
  under `-test.v -test.parallel=1`, which puts each print under the test that sent it.
- In a bare world whose cursor entity carries no `CursorComponent`,
  `PushCursorMove` predicts nothing and only crosses `EventCursorMoveRequest`, which
  no `CursorSystem` applies, so `LocalCursor` stays put. A router test that chains
  keys, a motion then what acts from its cell, applies each move from the queue
  itself (`lineWorld` in `internal/mode/router_test.go`).

## Session tests

- In `meshSession`, `mode.ExecuteCommand(apps[0].Context(), "n")` then
  `apps[0].Settle()` crosses the host's new game in the epoch at hand, and closing a
  participant's `link.MeshPort` under its world lock drops it. The host's lead is 3
  near tick 10 and 1 later, so a test of what lands inside a lead runs early. A mesh
  opens no door (`Membership.GuestCount` reads 0): what the door's roster keeps is
  tested over sockets (`mustHosting`, `mustSocketJoiner`, `arrival`). A pair led by
  1 is a join through the host, and one whose ends are both in already is a link
  beside (`meshOf`): a second link to the host is `{3, 1}`, after the pair that
  brings 3 in.
- `meshSession`'s idle cursors hold no energy and run out of heat near tick 29, so
  the game resets near tick 35; a session test that waits a fixed count of ticks
  past that reads the reset. Wait for the condition, bounded.
- A joiner's `LocalSlot` stays `NoPlayerSlot` until its arrival crosses on its own
  world, which takes the joiner's own ticks: a test waiting on it ticks the joiner
  (`arrival` in `host_test.go`).
- A spawn request naming a participant joins a reset's roster while one waits
  (`PlayerResource.JoinRestore`) and spawns nothing, so a unit test that leaves a
  restore pending rebuilds through the boot's request, one with no `PeerID`.
- A host admits `NetworkAdmitBurst` (6) dials from one address per
  `NetworkAdmitWindow` (a minute), a fixed window, and loopback is one address: a
  test lobby past six dials fills a window more per six (`lobbySession` sizes its
  wait so), a dial refused after admission counts and a budget refusal does not.
  A dial reaching a host with `MaxHandshakes` (8) handshakes in flight is closed
  unanswered, which the dialer reads as `io.EOF` ("connected, but no vif session
  answered there"), and is retried as a refusal is (`scriptedRun`).
- A held run takes no tick, so an event pushed at it dispatches only on `Settle`:
  end a test's hold with the request and `Settle`, as `mode.ExecuteCommand` callers
  do. A test-driven guest (`mustSocketJoiner`) applies no correction in `Tick`: run
  `settleCorrections` before `alignTicks` and `assertSharedParity`, and close no
  host publication while a hold's end or a new game is still to land.
- Hold only a session whose guests are in step. One more than `NetworkHoldTicks`
  behind, as a holder still seating its bot is, is carried past the hold and plays
  on unheld ([todo](todo.md#hold-a-guest-its-correction-carries-past-the-hold)):
  wait for the seat and the cursors on the guest first, and for the hold on the
  host alone, since under `-race` the guest of a host ticked in the background
  falls that far behind about every other run. A replay test levels its guest with
  its host before it plays, or an install that carries it forward leaves the
  replay's `predicted` depth behind the run's.
- `EventQueue.Consume` returns the queue's own slice, which the next `Consume`
  overwrites: a test dispatching handlers that push clones it first. A unit
  `NetworkSystem` reads its identity at `Init` from the latched session, so latch
  before building it, and `Cross` takes nothing until `barrierActive` is set.
- A headless run ticks `WraithTimeout` of game time in a fraction of a second, its
  resets included (`a.Tick(int(d / DeltaTime))`), so a test of a wait waits it out.
  `replayMatches` wants fifty compared ticks.
- A new game starts the tick count over (`Stamp.Run` moves), so a tick read before
  one bounds nothing after it: a wait for a tick ends when the run moves
  (`KeyframeAt`, `ProvedWorld`, `gateJoiner`). The harness's host ticks every 10 ms,
  five times the game's rate, so a wait that outlasts a 5 s deadline in play is
  over in a test unless the test ticks its host by hand.
- A host a test does not tick parks a mid-run gate in its lead, and gates run one
  at a time (`midRunGate`): a raw dial's reply holds the gate (`dialSession`,
  `Complete`), a second joiner's waits behind it, and what the test crosses
  meanwhile lands before that gate reads its offer.
- Only a play run redials the host it lost (`Membership.redial`), so the mesh and
  headless succession tests never do: a rule about a person's run losing its host
  sets the guest's `cfg.Mode` to `ModePlay` under `RunSafe` before the host goes
  (`TestAGuestThatRedialsItsLostHostTakesItsTurn`).
- `:join`, the menu's join and a redial dial on the run they replace and hand the
  dial to the next (`wiring.dialled`): a test of that path calls
  `membership.DialJoin` on a wiring of its own and builds its run with `driveRun`
  on a `runConfig` carrying the dial.
- A fixture's session takes the default policy, `host`, and its guests join as
  leaves: a test of the succession opens with `hostMigrating` and joins with
  `takingOver()` or a `ListenAddress`, or nothing walks and each guest forks as its
  host goes.
- A guest whose host closed its link plays on as a fork or a successor, on its own
  clock, and still applies what that host crossed before the close: a departure
  crossed for the guest itself turns its bots Wraith there a lead later, and a
  successor's freeze only delays it. A test reading that guest's world reads a
  race; read the instance the rule belongs to.
- One goroutine ticks a run. `waitForRosterPair` ticks both of its runs, so a run
  already under `tickInBackground`, which `gatedHost` returns, is waited on with
  `waitForCursors` or `waitUntil`, which only read. A second ticker passes
  `go test` and fails CI's race run: run a new socket test once under `-race`.

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
  reentrant), then the membership's `sessionMu`, then converge's `Authority.mu`.
  Nothing floods the mesh under `sessionMu` or the world lock; a flood runs in the
  correction loop, between ticks.
- A flag a goroutine reads as it starts is raised before the call that starts it,
  and lowered again if nothing started: `Instance.Redial`'s dial loop may read
  `Authority.Redialing` before `Redial` returns, so `beginSuccession` and
  `redialRelay` raise their wait first.
- `Router.ProcessMacroTick` runs off the world lock (`loop.go`, `headless.go`): a
  component read there races. Read a status cell, or take `RunSafe`.
- Tests reach a play run's paths by switching a running App's `cfg.Mode` under
  `RunSafe`, so a goroutine that reads the run's config off the world lock (the
  drive loop, a bot seat) reads the copy it started with instead.
- `World.CopyIn` overwrites `Config`, `Time`, `Player`, `Transient` and `View`
  whole, so a goroutine off the world lock (a bot seat, the drive loop, the
  correction loop) reads none of their fields, even one that never changes: the
  race detector flags the copy's write.

## Networking and admission

- `io.Copy` between two sockets reaches Go's kernel `splice(2)` path only when both
  ends are a `*net.TCPConn` or `*net.UnixConn` themselves: a wrapper that renews a
  deadline on each write (the router's `stallBounded`) hides `ReadFrom`, so each
  direction copies through a 32 KiB user buffer. Put the deadline on the socket, not
  on a wrapper, before reading a splice's memory.
- `encoding/json` compacts a `json.RawMessage` when it marshals one, so a signature
  over raw JSON bytes survives a round trip only when the signer emitted compact JSON,
  which `json.Marshal` does; a token re-encoded with whitespace by another writer
  fails its signature, which is the intended refusal, not a bug to relax.
- A challenge-response handshake cannot be fuzzed from a static script past its
  challenge: a proof over a random nonce is never in the corpus. The join target
  pins the nonce through `Coordinator.Nonce` and seeds proofs over it; the peer
  link target asserts instead that no script ever opens a link.
- A public key travels as its base64 string (`network.EncodeKey`), not `[]byte`:
  `RosterEntry` is compared with `slices.Equal` (`SameRoster`), a payload field is
  TOML a scenario could name, and the generated capture codec has string and slice
  kinds and no bytes kind. Decode at the boundary that verifies.
- `auth`'s SCRAM client refuses a challenge below the cost it was built with, and
  an empty channel binding is its error, not its unbound: pass `WithChannelBinding`
  only over TLS (`bindingOption`), and read the leaf from under a websocket
  (`tlsOf`), since `*tls.Conn` has a `NetConn` of its own to stop at.
- The router intercepts exactly one frame each way of the admission exchange, the
  door's question and the dial's answer, and splices everything after, so a
  passphrase door's later steps pass through it untouched; a stand-in pod in a test
  hands its connection to `HostAcceptor` whole, which reads the route frame itself.
- nginx's `proxy_protocol on` writes a version 1 header and nothing else; the
  router reads version 2 too for other edges. A probe of the front door behind the
  edge writes the header first or is closed unanswered.
- A join address keeps its `vifs://` scheme (`Endpoint.Dialable`): a bare host:port
  stored in `Joining` or the flags dials a TLS door in the clear. A sign-in is shown
  over TLS alone (`tlsOf`), whatever the door asks.
- An account command runs its KDF and requests in a goroutine and reports on the
  status line: a `SessionController` method runs under the world lock, which cannot
  wait on a site.
- A door reads what gates it as each dial arrives (`Coordinator.Passphrase` is a
  func): a run's port is made before its player's `:host ... passphrase`, behind
  its bots or a session it left, so a credential captured as the acceptor is built
  is the wrong one. The run's bots dial the same acceptor on a stream inside the
  process (`seatLink`) holding no phrase or ticket, so a gate is set only for a
  door (`sessionAcceptor`'s `door`).
- What a dial reads before it holds a place, the preview and a seat's offer, goes
  through `handshake`, which strips the chain: a `SessionOffer` field only a seated
  participant should hold is stripped there too.
- A browser trusts a self-signed door only by being told to: pinning from a page
  needs WebTransport's `serverCertificateHashes`, which the route does not speak,
  so a page joins a player's door over the site or by a passphrase bound to that
  door's certificate; a native run pins the fingerprint the sessions API carries.
- Two runs on one machine find each other over UDP broadcast
  ([Multiplayer §5.7](multi-player.md#57-games-on-the-local-network)): a datagram
  to an interface's directed broadcast reaches local sockets too, from that
  interface's address, and the second run to bind UDP 7777 takes an OS-assigned
  port. `ss -lnup` names who holds the port, and a few lines of Python sending the
  512-byte probe (`vif?`, 1, 2, then zeros) read a visible host's beacon back.

## Position

- [Position](position-package.md) states the index's rules: bounds that are the
  grid's, the package's names, the point a query takes, the planes
  `BlockChangedAt` keeps, `WallBlockAll`. What it leaves out:
- `pkg/position` knows no entity: a test over walls or glyphs needs a world and
  stays in `internal/engine`; a grid-only test goes to the package with a test id
  whose class is its top byte.
- A body's size is one `vmath.Footprint`, its header offset inside it, and a
  species' is read through `Resource.Footprint`, never as four ints or a parallel
  parameter: the searches, line of sight, passability tiers and a materialize take
  the struct, and an event's or a component's cell names the header, never the
  top-left.
- `engine.NewGameContextWithClock(w, 80, 40, ...)` takes the terminal's size; the
  map is what the status bar and rulers leave of it (79x36 there), so a test of a
  cell at an edge reads `Positions.Width` and `Height`.
- A cell's view (`EntitiesAt`, `Occupants`, `EachOccupied`) is the cell's own
  storage or its crowd's, and a cell going from one occupant to two, or back,
  moves them: copy a view before writing its cell, as every call site does
  (`copy(buf[:], ...)`).
- The probe path lives on the inliner's budget: `go build -gcflags=-m=2
  ./pkg/position` prints each function's cost against 80, and `probe` stands at
  79. A table of indices beside a stale flag cost it 88 and a call a probe.
- A row's wall flag (`Position.wall`) is read when the row is made, on
  `WallChanged`, and for every row once rows are written whole (`readWalls`, in
  `CopyIn` and the order install). A new path writing `dense` and `entities`
  whole calls it; the fuzz test's invariant fails one that does not.
- `BeginBatch` returns the store's one batch, begun again: commit it before the
  next begins, and hold none across a call that may begin one.
- `PositionBatch.Commit` refuses a cell any entity holds, a Player one included,
  as `GlyphSystem.occupied` does for a line drifting in: an entity put on the
  index or taken off it, an effect's too, moves where glyph lines and nuggets
  land and the glyph stream with them, and from there the fight.
- A query is a generic method over its point (`Blocks[P Coord]`), the form read
  by size in `at`, `as`, `box` and `body`, a constant in each instantiation: a
  flat instantiation must keep the flat function's instructions, which `go tool
  objdump -s 'Index.*\.Blocks' <test binary>` counts for both. A helper that
  returns six fields (a `Box`, a `Footprint3`) goes through memory: return the
  four-field form and two ints. A local named `at` shadows the helper.
- A search names the zero point with false where it finds nothing: the swarm's
  `findRoom` and the storm's attack landing keep the point without reading
  `found`, and what they build from it is in the digests.
- A loop around a call spills every live value before its first pass, so a
  helper that calls once in the common case takes it straight (`clear`'s one
  layer) and loops in a function of its own (`plane.deep`).
- The volume tests hold a layer against a flat map through the same code, so
  they see no flat rule broken: a flat rule needs a model of its own, as
  `placedAround` is the exclusion search's. The block log is lost past
  `cells/8+64` entries, so a test reads it before that many changes.
- Two builds with the same instructions can read 15% apart on one probe by link
  layout alone: rebuild both with `-ldflags=-randlayout=N` for a few N and
  compare the spreads before believing either, or, where the hot function did
  not change, diff its `go tool objdump -s` in both with the addresses masked.
- A surface a column ahead of the others of its mask and layer leaves
  `staleShift`, so a probe lays the surfaces again; a scroll of every one leaves
  `stale` clear, which the surface test holds and `BenchmarkCaveStep` shows. A
  test that probes after each change never lets a scroll finish: change in
  batches, as that test does.

## Math and physics

- `pkg/vmath` names a flat form's twin through a volume with a 3 (`Point3`,
  `Line3`, `Footprint3`, `PointAt3`, `GridTraverser3`) and a vector operation
  `V3...`; a twin is tested held to one plane against its flat form, exactly
  where no table rounds an angle.
- The compiler keeps a struct in registers only up to 32 bytes, four fields a
  level and no array, which is `Line`'s eight int32s in four pairs. A larger
  walk steps in place through a pointer method and names its fields:
  `Line3` read 68 µs over the bench segments as arrays stepped by value, 106 as
  named fields by value and 10 as named fields in place (`Advance`).
- A helper's scratch of a known ceiling lives on its caller's stack: an `Append`
  form leaks `dst` only to its result, so `var buf [N]T` passed as `buf[:0]`
  allocates nothing, as `updateOrbs` samples its orbit. `-gcflags=-m` printing no
  `moved to heap` for the buffer confirms it.
- Appending a two-int array a sample read twice the time of growing the slice
  once and writing each element in place (`AppendEllipseGrid`, 620 to 310 ns for
  64 samples).
- The sine tables hold `math.Sin` and `math.Cos` at `TwoPi*i/LUTSize`, the very
  angles a sample count dividing `LUTSize` asks for: reading them there gives
  the computed cells bit for bit. `Sin` and `Cos` floor any other angle to a bin.
- A benchmark that feeds a pure function constants measures nothing: the call
  folds at compile time, and `vmath`'s older benches read 0.9 ns for a sine that
  costs 5.5. Feed package variables, and read a loop over varied inputs beside
  the lone call, since inlining and what one iteration waits on differ there.
- On amd64 at its default level `math.Floor`, `Ceil` and `Trunc` compile to a CPU
  feature test with a call for the fallback. Inlined in a loop the rounding also
  waits on its output register's last value, chaining the iterations: the table
  sine read 5.5 ns, a rotation 7.5. `lutIndex` truncates and steps down by a
  borrow (`bits.Sub64`); the same step as an `if` compiled to a branch in some
  callers and missed on angles of either sign (a collision 9 to 18 ns). Built
  at `GOAMD64=v2` or later the floor is one instruction, a lone sine 0.8 ns
  against the borrow's 1.2, so `floorChecked` is a build constraint
  (`amd64 && !amd64.v2`) and such a build keeps the floor.
- The same truncation in `PointAt` reads a third faster alone and costs more
  than it saves: its inline cost goes from 12 to 50, `physics.Integrate` (55 to
  93) and three more stop inlining, and a system's loop over bodies reads 20 to
  40% slower. To name every function a change stops inlining, list
  `go build -gcflags='github.com/lixenwraith/vif/...=-m' ./...` before and
  after and compare the sorted `can inline` lines.
- `pkg/vmath/physics` names a 3D twin with the suffix its older functions carry
  (`Integrate3D`, `ApplyHoming3D`), beside its flat form in the same file and on
  the flat form's scalar helper where one exists (`HomingProfile.arrival`,
  `impulseMagnitude`, `reflectCell`, `bounceSteps`). Two tests hold a twin: in
  one plane it is the flat form, and with its axes cycled it answers alike.
- A twin's test compares floats within 1e-9, never by `==`: an architecture that
  fuses a multiply into an add (arm64, amd64 at `GOAMD64=v3`) rounds two orders
  of one sum apart. Cells and counts compare exactly.
- A flat form's result is simulation state. A refactor of one is compared with
  its previous body, kept in a scratch test, on random inputs by
  `math.Float64bits`, negative zeros among them, before the headless digests are
  read: `v -= v*0` turns a negative zero positive, so a branch that skips a
  subtraction is not one that subtracts nothing.

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
- The menu's rows are built again each frame the menu shows them, and a host that
  draws its own menu takes the whole tree each frame (`Router.MenuSnapshot`), as a
  `tui` search does: a row builder with a side effect, as Nearby games' `Look`,
  runs on every page there.
- A method the menu comes to call on `engine.SessionController` panics in
  `internal/mode`'s tests, whose double (`menuSession`) embeds the interface as
  nil, and one added to `game.Listener` stops `android/game`'s tests building
  (`menuListener`), which no `go test ./...` at the root compiles: give both
  doubles the method, and run `go -C android test ./game`.

## Looking at the game without a device

- Run `bin/vif` in a tmux pane (`-nw` starts it without waiting for a key) and read
  it with `tmux capture-pane -p -e`, which keeps the SGR colours; turned into HTML
  spans and shot with Playwright, it shows real colours. `TERM=linux COLORTERM=`
  gives the console tier. The capture carries SGR state from one line into the
  next, so a converter that resets colours at each line paints wrong backgrounds.
- A status message longer than its room rises onto the row above the status line
  (the bar's `spill`), and the last row reads empty: read a capture's last two rows.
- A run started with `XDG_CONFIG_HOME` and `XDG_STATE_HOME` in a scratch directory
  reads and writes no file of the player's, so a setting it writes back never
  reaches their `vif.toml`; `-mute` keeps it silent, and a tmux server of its own
  (`tmux -L <name>`) keeps it out of their sessions.
- A host loss is three such runs: a host opened with `:host`, two guests that
  `:join` it mid-run, then `:q` on the host. `:session` on each guest, and its log's
  `session` and `net` lines (`-l=<dir>`), say who took the term or why nobody did.
- Drive it with `tmux send-keys`: `:emit <Event> { ... }` raises any event, `:god`
  arms every weapon, `a` toggles auto-fire and `:log on` writes the JSONL log under
  `~/.local/state/vif`, where every install logs; vif never writes `/var/log`.
- `tmux send-keys` drops a `;`: one alone is its command separator and a trailing
  one in a `-l` string is cut, so `fx;;;;` sends three repeats; send `'\;'` as its
  own argument. A capture shows the cursor on a glyph as the glyph, so read the
  cursor from the telemetry overlay's player card (`:t`, `/player.0`) instead.
- `-l <dir>` is `-l=<dir>`: with a space the log lands under `~/.local/state/vif/log`.
- To watch one moment tick by tick, `:step ev <Event> pause` runs to the tick the
  event is dispatched, `:step 1` advances a tick and `:step off` resumes. A paused
  frame is dimmed by half (`DimFactor`): for true colors resume and capture every
  50 ms, which lands one capture a tick. A drain's end raises `EventSpeciesKilled`
  too, so `:system drain disable` first.
- `:god`'s weapons hide what dies under their fire. `:energy 900000000` survives
  without it, `:w rod 9` or `:w launcher 9` arms one weapon, and `-s blank` has
  nothing else on the map; a swarm there with no weapon armed lives out its 35 s,
  under five at `:speed 8`.
- A renderer's own test does not show how layers compose. A flash's glyph under a
  fading ground was blended to nothing, and a fade over a cell nothing painted
  (black in staging, the theme background after `finalize`) went through black:
  both showed only in the running game, read from a capture's colors, not its glyphs.
- Xvfb, Alacritty and xdotool install in an Ubuntu container for real key and mouse
  input: `xdotool key` types, and tmux inside the window reads the screen back.
  Alacritty 0.13 from apt also needs `libxkbcommon-x11-0` and runs under
  `LIBGL_ALWAYS_SOFTWARE=1`; it speaks the key protocol, and Xvfb autorepeats a
  key held by `xdotool keydown` (660 ms, then 25 Hz), so presses, repeats and
  releases reach vif as from a desk. tmux in between drops the protocol. With no
  window manager `xdotool windowfocus`, not `windowactivate`, gives it the keys.
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
- An init script wrapping `window.Worker` hears the worker's `atlas` and `frame`
  messages before the page does: the atlas's runes, read as `runesOf` reads them,
  and each frame's cell slots, kept as `applyFrame` keeps them, give the screen's
  text. The status bar is two rows, its items (`PAUSED` for a hold, `GT:` the tick)
  above its line; a menu's or an away's pause shows nothing there, but the map
  cells changed over a second tell a running game from a paused one, and the first
  frame after a return against the last before it tells whether the game ran
  while away. `page.goBack({ waitUntil: 'commit' })` is the Back button, landing
  on the entries the page pushes in a gesture, which a key `press` in the game is.

## Measuring

- A browser-build bench runs under Node (`BenchmarkKrakenRegion`'s comment), which
  refuses a run whose arguments and environment pass about 8 KB ("total length of
  command line and environment variables exceeds limit"), as a cloud container's
  environment does: run it under `env -i PATH=... HOME=/tmp`. An `internal/app` test
  file calling what `js/wasm` lacks takes `//go:build !js`, as `readings_test.go`
  does, or that package's wasm test binary stops building.
- Benchmarks in a cloud container move a fifth between runs, and benchstat is not
  installed: build main's and the branch's test binaries (`go test -c` in a `git
  worktree` of main) and compare medians of alternating runs. A bench file the
  branch adds is copied into the worktree, kept on main's API where the branch
  changed one. A tick reads in two modes, one binary 22 µs in a round and 28 to
  32 in the next: for a few percent, read the minimum of seven to nine rounds
  beside the median.
- A bot graph is compared in seconds: a 60 s round at 120x40 under `-headless
  -speed max` takes 0.1 to 0.2 s (td 0.8 s). Read rounds from the log, not the
  fitness, which weighs no crush: at `-lv debug -log-stdout` each FSM transition
  carries a tick that restarts with the game, so vanguard's `"to":"PlayerCrushed"`
  and `"to":"PlayerDied"` lines are round lengths. A scenario copied to
  `<dir>/scenario/base/<name>` runs as `-config-dir <dir> -s <name>`.
- valgrind's callgrind counts the instructions one function runs
  (`--toggle-collect='*Reach*.Nearest'`), which link layout does not move, so it
  tells a build that does more work from one laid out differently (Position's
  layout note). It asserts on Go's preemption signal: run the test binary with
  `GODEBUG=asyncpreemptoff=1 GOMAXPROCS=1`.
- B/op can rise as retained memory falls: a smaller heap collects sooner, and
  each collection empties the event batch `sync.Pool`s, which then allocate
  again. Read both sides with `GOGC=off` before blaming an allocation: td's
  headless ticks read 6% more bytes with the grid 30 MB smaller, and the same
  bytes with the collector off.
- Two binaries simulate alike when a scratch test in each prints
  `snapshot.DigestWorld` and `Positions.Order()` every fifty ticks of the headless
  shapes and the outputs diff empty; the divergence harness reads the link, this
  reads the world.
- `snapshot.DigestWorld` hashes entity ids, so a change in how many Player
  entities live differs from its first tick and says nothing. Trace without ids,
  a tick at a time on the harness's host (`shapedFight`): each shared section's
  rows (`CaptureShared`, `snapshot.BuildManifest`; `w.cursor` holds a session key
  and differs every run), every stream's state (`Rand.SaveStreams`) and the
  counts of glyphs, drains and particles. The first stream to differ names the
  system that drew differently. The cause is proven when main, made to do the
  same in a scratch edit, traces as the branch does on every tick.
- A renderer's first call in a live frame runs about twice its hot-bench time,
  its tables and code gone cold under the frame's other work: the ember paints in
  9-12 µs when repeated at once and 20-26 µs as the frame's first. Read a gain
  live, with `:d prof` under `:log on` (`prof.top` lines) or a `time.Now` probe
  round the call in a scratch build, before calling a bench gain felt.
- A value kept live across a hot per-cell loop can cost more than the work it
  guards: a burn's cut held across the Kraken renderer's stamping loop slowed a
  living Kraken's frames 4% with nothing burning. Filter before the loop, keep the
  loop in a function that takes only what it reads (`KrakenRenderer.stamp`), and
  measure the common path against main.
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
- td's storm is read headless with eight cursors walking a cell a tick, the
  `td_monitor` region paused, towers healed and `EventStormSpawnRequest` pushed
  after 2,400 ticks. A storm left alone takes few walls a tick (Navigation §10):
  a tick where hundreds go is made by moving its circles into untouched maze.
  Label each tick with `pprof.Do` and profile at the default 100 Hz; at
  1,000-2,000 Hz a cloud container kept a quarter of the samples or fewer.
  `nav.recomputes` is a gauge, not a count.
- tmux can throw frames away while vif sees nothing: it reads a pane at once, so
  writes return and held frames stay near zero. Attach a client (Alacritty under
  Xvfb, or a Python pty reader sized with `TIOCSWINSZ`) and poll `tmux list-clients
  -F '#{client_discarded}'`; a rising count means the client is repainted every
  100 ms. To count what a client saw, replay its byte stream through `pyte`,
  sampling 60 times a second, and count the samples where a pane changed.

## Scenario TOML

- A test that reads a shipped scenario skips, not fails, when it is missing (an
  `os.Stat` or `ScenarioPath` guard), so moving one under `wad/scenario/` passes
  green while testing nothing: run those tests with `-v` and read the SKIP lines,
  and search the `filepath.Join(..., "scenario", ...)` form beside the slash one.
- A payload's untagged fields decode by their Go names: a wall batch's cells take
  `OffsetX`, `OffsetY` and `WallVisualConfig = { ... }`, and a rune field such as
  `char` takes the character's code, `43` for `+` (`wad/scenario/base/dummy`'s pads).
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
- `IncrementVar` and its kin read a `delta` of 0 as 1, so a list of offsets a macro
  zips with `each` starts at 1, from a base one cell short (armada's `Rank`).
- An edge into a state with a probe can fail, so the load demands a `fallback` on
  each, a global edge included. Instances spawned on one tick time out together:
  a roll on a `Timeout` sends a salvo, a `Chance` on a `Tick` edge one at a time.
- To watch a scenario play, capture `tmux capture-pane -e -p` and render the SGR
  to HTML for a headless browser screenshot; a headless `App` ticked with a
  dispatch tap, a scratch test in `internal/app`, reads what a capture cannot.
- A rewrite of scenario files that must leave what a scenario does alone is
  proven on the resolved tree: a program under `tmp/` writes
  `fsm.ResolveScenario`'s result for every folder holding a `scenario.toml`, keys
  sorted, each value with its kind and a float by its bits, and the runs before
  and after compare by bytes. What a set spends
  ([TOML macros §4](toml-macros.md#4-invocations)) is `macro.Set`'s unexported
  `spent` and `open`: a scratch test in `internal/fsm` reads them by reflection
  off the set `readExpanded` returns, the entry's behaviors and regions loaded
  through it as `loadAndResolve` loads them.
- A shipped scenario written in a form an older build's expander refuses takes
  the next `network.ProtocolVersion`: a joiner is sent the scenario's files and
  expands them itself, so the older build is refused at the door instead.
- A system the manifest drops leaves every `only_systems` that names it, which
  `-check` and `internal/resource`'s systems test refuse as unknown: search
  `wad/scenario/` for its name (`vimtutor` lists its systems one by one).

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
- A renamed or removed identifier outlives a sweep that reads prose: list every
  backticked name and path in the docs and look each up in the sources (a
  `go/parser` pass over this module and those it requires, and the words of every
  file but Markdown for flags, keys and tags); a name found nowhere is stale.

## Libraries

- audio, genetic, ascimage, transformer and stapes (formerly soundlab) left this
  repository for their own ([package map §8](package-map.md#8-external-module-boundary)):
  a change to one lands there, under its own `AGENTS.md` and agent notes, and vif
  takes its tag. Stapes keeps its verbs, the rip and the model's training over
  transformer, which vif does not require; `make tools` builds ascimage's command
  from the version `go.mod` requires.
- The capture carries `registry.SpeciesState`, which embeds the streaming
  engine's checkpoint, so a change to `genetic.StreamingState`'s layout bumps
  `snapshot.Schema` beside `StreamingStateVersion`.

## Branches

- A session pushes only its own branch and a force push is refused, so a branch
  whose PR merged starts again from main only once its owner deletes it: ask.
- Take the next `snapshot.Schema`, `event.JournalSchema` or `manifest.SchemaVersion`
  past main's, and check again after merging main, where another PR may have taken
  it.
