# Agent Notes

What agents working on this repository learned that no rule in
[AGENTS.md](../AGENTS.md) and no design document states: the traps any task meets,
and how to check them. Read it before starting. When a task teaches you one, add it
in the same PR, one bullet under its topic. A note here is a trap or a technique the
next task meets whatever its area; one that only a task in one area meets goes in
that area's design document, and a technique for one kind of work in
[Development](development.md). Keep it short: delete a note that proves wrong, whose
trap is gone, or that a document or a code comment now states, and link that
instead.

Where the rest lives:

- Builds in a container (the APK, the session image, another architecture, dead
  code): [Development §12](development.md#12-builds-in-a-container).
- Looking at the game without a device (tmux, sessions, headless probes, the window
  under Xvfb, real keys, the browser under Playwright, the emulator, Wine, sound):
  [Development §13](development.md#13-looking-at-the-game-without-a-device).
- Measuring (two builds compared, benches that mislead, the compiler's output,
  profiles, a terminal's bytes, threads and clocks, long runs):
  [Development §14](development.md#14-measuring).
- Writing a session test:
  [Multiplayer §9.1](multi-player.md#91-writing-a-session-test); a bot's reading:
  [Bots: Taking a reading](bots.md#taking-a-reading).
- A subsystem's own traps: its design document, as
  [Multiplayer](multi-player.md) for sessions,
  [Auth and session design](auth-session-design.md) for admission,
  [Build profiles](multi-platform.md), [Position](position-package.md),
  [Domains](domain-design.md),
  [Netcode §3.5](netcode-design.md#35-the-divergence-harness),
  [FSM](fsm-and-configuration.md) and [its reference](fsm-reference.md),
  [Behavior machines](machine-design.md), [Rendering](rendering.md),
  [Gameplay](gameplay.md), [AI and physics](ai-physics-and-evolution.md),
  [Input](input-and-modes.md) and
  [Filesystem §3](filesystem-layout.md#3-resolution-policy) for settings.

Topics: Toolchain · Dependencies · Generated code and versions · Events and ticks ·
Tests · Locks · Docs · Git and branches.

## Toolchain

- The `gofmt` on a container's `PATH` can be older than the module's Go and reject
  generic methods ("method must have no type parameters"); run
  `"$(go env GOROOT)/bin/gofmt" -l`.
- A long session's Go build cache fills the disk allowance, 27 GB after some sixty
  scratch builds, and a link then fails with "no space left on device":
  `go clean -cache` frees it and the next build takes 40 s.
- `git ls-files <path>` before creating a file: `internal/app` names its test files
  for concepts (`door_test.go`), and a write to a path that exists replaces it.
- A session's scratchpad can sit on a `/tmp` that a reboot empties: keep a long
  task's notes and working files in the checkout's `tmp/`, which `.gitignore` holds.
  A `.go` file there is a package to `go build ./...` and `go vet ./...` all the
  same, so a scratch program or a copy of a source kept there stops both unless it
  carries a build tag of its own.
- `internal/app`'s tests do not build with `-tags vif_headless`: `config_test.go`,
  `geometry_test.go`, `join_press_test.go` and `watch_test.go` drive the presentation
  that profile leaves out, so its gate is `go build -tags vif_headless ./...` alone.
- A `vif_headless` or `vif_noaudio` build links no `lixenwraith/audio` but its
  `model` package: `make verify` checks the headless one with `go list -deps`
  ([Build profiles §3](multi-platform.md#3-reference-artifact-measurements));
  nothing checks the `vif_noaudio` one, CI checks neither, and a build passes either
  way. A package those builds import takes an audio bound from `internal/parameter`
  (`AudioBufferMsMin`), which an audio-tagged test holds equal to the library's.
- A change to a package's API is compiled against every test file by
  `go test -count=1 -run '^$' ./...`, which builds each test binary and runs
  nothing; `go build ./...` and `go vet` on the touched packages miss a test in a
  package the change never touched. `android/` is a module of its own that no root
  `go test` compiles: a method added to `game.Listener` stops `android/game`'s tests
  building until its double (`menuListener`) has it, so run
  `go -C android test ./game`.
- Go 1.27 takes a promoted field as a struct literal's key, though not through an
  embedded pointer, so fields moved into an embedded struct keep every literal,
  selector and method call compiling.
- Claude Code's Bash safety check refuses any command whose text holds `rm -rf` on
  a variable, a heredoc that only edits such a Makefile recipe included: edit the
  recipe with the Edit tool, and read what a recipe would remove with `make -n`
  (`make -n install-config purge VIF_CONFIG_DIR=<any path>` prints the recipe and
  runs nothing); run a purge only on a root made for it.

## Dependencies

- The gates against a dependency's branch (AGENTS.md, Commits and PRs) run under a
  `go.work` kept outside both trees and named by `GOWORK=`: `use` each checkout, a
  nested module's too (`terminal/window`). Once a `go.mod` there requires a version
  no tag serves, as the window's does of terminal, every build stops ("reading
  .../go.mod at revision vX: unknown revision"), since a workspace still reads the
  required version's `go.mod`: the `go.work` takes `replace <module> vX => <its
  checkout>` as well. `make notices NOTICES_OF=android` lists `./game` from
  `android/`, so under a `GOWORK` that leaves `android/` out it stops ("main module
  ... does not contain package .../android/game"): run it with `GOWORK=off`. A
  scratch copy of the tree under a directory that holds a `go.work` is in that
  workspace too.
- The `go.sum` lines for a dependency's untagged release come from a file
  `GOPROXY` serving the commit the tag will name (`golang.org/x/mod`'s
  `zip.CreateFromVCS` writes `<module>/@v/<v>.zip`; `<v>.mod`, `<v>.info` and `list`
  beside it), listed first with `GONOSUMDB` naming the module and a scratch
  `GOMODCACHE`: `go get`, then `go mod tidy` and `go -C android mod tidy`, write
  them, and they verify against the tag once a squash merge keeps that tree. The
  last tag's commit zipped the same way must hash (`dirhash.HashZip`) to the `h1`
  main's `go.sum` holds, which proves the zip before the new line is trusted, and
  `GOFLAGS=-modcacherw` lets the scratch cache be removed.
- `gomobile bind` builds `./gobind` in a temporary module of its own, which no
  `go.work` lists ("directory gobind is contained in a module that is not one of the
  workspace modules listed in go.work"), so a workspace cannot stand in for a
  dependency's untagged version in `make android`: a `replace` appended to
  `android/go.mod` and reverted before the commit can. The file `GOPROXY` above is
  environment, which should reach gomobile's module; no build has tried it.
- A bump in the root `go.mod` also runs `go -C android mod tidy` in the same commit
  ([android/README.md](../android/README.md#build)), which `android/toolchain.sh`
  checks before Gradle.
- `cmd/vif/gui.go` alone imports `terminal/window`: while that module does not
  verify against `go.sum` ("checksum mismatch"), `-tags vif_nogui` still builds and
  gates everything else.
- A probe of a dependency's API against the checkout's files runs from a module of
  its own outside the tree: its `go.mod` takes this module's `go` line (a lower one
  asks for a toolchain nothing serves) and its `go.sum` a copy of this one's, and
  `GOFLAGS=-mod=mod GOPROXY=off go run .` builds it from the module cache with no
  network. A dependency of that dependency stops it on the older version it names
  unless the scratch `go.mod` requires this module's too, as `golang.org/x/crypto`
  for `lixenwraith/auth`.
- audio, genetic, ascimage, transformer and stapes are modules of their own
  ([package map §8](package-map.md#8-external-module-boundary)): a change to one
  lands in its repository, under its own `AGENTS.md` and agent notes, and vif takes
  its tag.

## Generated code and versions

- `go generate ./internal/manifest` type-checks `internal/event` from source, its
  generated files included, so removing a payload struct, a field of one or an
  event stops the generator on the stale use of it ("type-check ...: has no field",
  "undefined"): delete those lines from `payload_gen.go` and `registry_gen.go`
  first, and the run writes them anew.
- A registered struct payload decodes from TOML, so every enum it holds at any depth
  must take names (`TestEveryEnumAPayloadHoldsTakesNames`), and a payload that is a
  component fails on that component's enums. Annotate the event with the qualified
  type (`(component.KrakenBurn)`), which registers nil: right only for a `[local]`
  event a system pushes, which is neither journaled nor sent.
- A new crossing type adds a row to `crossingRules` (`internal/system/network.go`)
  naming its author and, where its payload names its producer's cursor or slot, the
  reader of it: with no row every instance refuses it, its producer's own included
  (`TestEveryCrossingASystemPushesHasItsRule`). One only the authority may make is
  `byAuthority` there, and joins `barrierBound` when it sets shared state from one
  tick on; with no payload it needs no codec (`crossSession(et, nil)`). A peer's
  frame is held to the tag its producer stamps, Player on a Bus type and Shared on a
  Stamped one, so a frame a test builds by hand carries it; a player-domain push of
  a replicated event is named in `crossingPushes` too
  ([Combat §8](combat.md#8-adding-a-weapon)).
- A new event type moves the number of every one declared after it, so it takes the
  next `network.ProtocolVersion` and `event.JournalSchema`. A shipped scenario
  written in a form an older build's expander refuses takes the next
  `ProtocolVersion` too: a joiner is sent the scenario's files and expands them
  itself, so the older build is refused at the door instead. The capture carries
  `registry.SpeciesState`, which embeds the streaming engine's checkpoint, so a
  change to `genetic.StreamingState`'s layout, or to how a state is continued, bumps
  `snapshot.Schema` beside `StreamingStateVersion`. Take the next `snapshot.Schema`,
  `event.JournalSchema` or `manifest.SchemaVersion` past main's, and check again
  after merging main, where another PR may have taken it.

## Events and ticks

- An event a system pushes in its `Update`, a crossing due at the tick and an FSM
  action's event all dispatch in the tick's one settle, after every system's
  `Update` ([Runtime §5](runtime.md#5-simulation-tick-steps)), pass by pass, breadth
  first: what a handler pushes runs after every event of the pass it ran in. A
  consumer that must act on one inside the same tick, before its own motion, reads a
  derived resource the producer writes instead, as the particle system reads
  `CaveResource` and the glyph spawner `DriftResource`, or makes the thing itself,
  as a mount creates the pulse it fires in its own `Update`. A derived resource is
  created in `engine.newGameContext`, rewritten by its owner every tick and never
  captured, so it bumps no schema; systems run in ascending `Priority`, so only a
  later system reads it the same tick.
- A journaled event, of any origin but a system or a device, is never pushed inside
  the tick body ([Logging §9](logging-and-diagnostics.md#position-lattice)): the
  tick's settle would dispatch it live while a replay injects it after the tick. The
  barrier's are the exception, stamped as the body's (settle group 0), which a replay
  queues for its own tick (`ReplayTarget.PushBody`, `EventQueue.Defer`). What only
  the operator does or sees, a pause, an overlay request, the speakers, is pushed
  `OriginDevice`, never journaled and outside the event tallies, so a replay, which
  has an operator of its own, still matches: one left on the ambient origin diverges
  every replay whose recording made it.
- A run's tick is `World.TickInterval`, which its host may choose (`-tick`,
  [Runtime §3](runtime.md#3-runtime-clocks-and-cadence));
  `parameter.GameUpdateInterval` is only the default. Code converting ticks to time
  reads the run's, as tests may keep the constant. A comment or doc giving ticks in
  seconds says "at the default tick", 20 a second: 300 ticks once read five seconds.

## Tests

- A run's wiring and the platform's `Host` reach it only in its `runConfig`
  (`wiring`, `host`), never in a `Config`: `NewHeadless` on a `Config` drops a
  session's `-players`, so a test of what a join holds to passes on nothing, and a
  presenting entry point built on a `Config` alone draws on the terminal, not the
  window, page or Android view, which no test catches for a watched script or bot
  (`Run`, `RunScript` and `RunBot` fill `host` from the `Host` handed beside the
  `Config`). A test that presents builds its run with `newApp` on a `runConfig`, and
  one that builds a joiner as a join does goes through `configForJoin` into
  `newDriven`. `RunBot` and `RunScript` return counters alone: a test reads the
  world a driven run is built on through `drive`, whose `build` is handed the App
  and ends the run before it presents by returning an error
  (`TestABotAlonePlaysOneMapWatchedOrNot`), and a run's size from its journal's
  anchor (`journal.NewCapture().Anchors()`). A run that shows nothing reads its
  terminal off any standard stream still on it (`startedIn`): under `go test` there
  is none, and a test names one by swapping it.
- `engine.NewGameContextWithClock(w, 80, 40, ...)` takes the terminal's size; the
  map is what the status bar and rulers leave of it (79x36 there), so a test of a
  cell at an edge reads `Positions.Width` and `Height`.
- The two lints in `internal/system`
  ([Development §5](development.md#5-validation-and-tests)) read files and names,
  not behavior. `TestSystemDomainProfiles` reads a system's file, not its guards: a
  shared system that touches `Boost`, `Energy` or another D-13 store fails it even
  behind `ResolveOwnedCursor`, so what it wants of one stays where the store's owner
  reads it, as the square's boost cap and energy step ride `ShieldComponent`; it
  narrows a dual system by what its file creates (`PulseSystem` creating a cursor's
  pulse reads as player alone, so `WeaponSystem` fires the death wave); and a Shared
  system touching a Player-only store needs an allowlist entry with its reason
  (`"wall:Particle"`). `TestNoOrderDependentMapRange` matches a ranged identifier by
  name against every map-typed name in `internal/system`, so a local slice named
  `drains` trips on `ParticleSystem`'s map field: rename the local, or allowlist it
  with its reason. It lints `internal/fsm`, `fsm/std`, `engine`, `event` and others
  too (`lintDirs`), keyed by function name, so renaming an allowlisted function
  fails it.
- A new non-ASCII rune in a drawing package or a shipped scenario's TOML must be one
  the terminal module's atlas holds (`atlas.Hack()`), and only
  `go test ./internal/asset`, a package the change never touches, catches it
  ([Content §7](content-assets-and-tools.md#7-embedded-assets)). A rune it lacks is
  one Hack lacks: pick another, since nothing is regenerated in vif and only
  terminal's `tool/font-atlas` could draw it. The test reads every string in
  `internal/mode`, `app`, `help` and `prof` too, and a comment after code on its
  line as code, so a key nobody sees and that comment stay ASCII.
- A test that reads a shipped scenario skips, not fails, when it is missing (an
  `os.Stat` or `ScenarioPath` guard), so moving one under `wad/scenario/` passes
  green while testing nothing: run those tests with `-v` and read the SKIP lines,
  and search the `filepath.Join(..., "scenario", ...)` form beside the slash one.
- `os.UserConfigDir` reads `XDG_CONFIG_HOME` only on Linux and the BSDs, so a test
  setting it alone writes the developer's own files on macOS and Windows: point
  `HOME` and `AppData` there too and skip where `paths.UserRoot` still lies
  elsewhere, or write a `-config-dir` root instead. A store test never names
  `/dev/null`, which a broken refusal renames over as root: use a FIFO.
- A test is proven to pin its rule by undoing the rule. One file's mutation compiles
  in its place under `go test -overlay <json>`
  (`{"Replace":{"<the file's absolute path>":"<the scratch file>"}}`), several in a
  `tar` copy of the tree, so the working tree stays editable. A mutation that
  survives is run against main's own tests in a `git worktree`, where surviving too
  makes it an old gap rather than a lost pin. One that only fails to build, a name
  left unused, proves nothing: keep the name read (`false && x`). One that
  deadlocks hangs where it should fail: run it under `timeout`, whose 124 is the
  test holding, and give a run past ten minutes `-test.timeout 0`.
- The cadence tests (`internal/app/cadence_test.go`) run a shaped mesh on its
  virtual clock and are deterministic: one fails every run alone or never, so it is
  not load. A capture a few bytes larger or smaller reshapes every publication over
  a thin link: bisect, then print the host's `LinkMetric` and `Cadence()` each tick.
- A few `internal/app` tests over real sockets fail on main alone, rarely and more
  under `-race` or load (`TestAJoinerLandsInAGameWaitingForItsHostsStart`: `shared
  snapshot diverged` at its last parity, the two a tick apart); a failure there
  takes about 16.5 s, a 15 s socket wait running out. Before calling one a
  regression, build each tree's binary once (`go test -c`, with `-race` for a race
  only CI saw) and run the test alone in fresh processes, alternating main's and the
  branch's under the same load, near twice the CPU count: eight processes of one
  test on four CPUs failed a quarter of them where a quiet run never did. At five
  times the CPU count a socket join's catch-up is refused ("behind the session ...
  retry in a moment"), the harness's host ticking every 10 ms faster than the loaded
  joiner closes the gap, a refusal a production seat retries. Eight race processes
  starve a host to 11 to 18 ticks a second, under the game's 20, so a gate's 5 s and
  a test's 15 s end before a convergence floor of 60 ticks does: a join three relays
  deep then fails by load alone
  (`TestARunBehindARelaySeatsItsBotsAndRelaysForOthers`). A flake that appears
  after a merge can be older: count it in one quiet process at `-test.count=40` on
  the commit before and after. Run a new socket test once under `-race`: a second
  goroutine ticking one run passes `go test` and fails CI's race run.
- The race run reports two accesses only with no lock between them, and a lock
  both goroutines take in between hides the pair: a race load does not show is
  forced in a worktree copy, by a sleep that lands the read while the writer holds
  its own lock, ahead of its write. `internal/app` tests start no log sink, so a
  socket test that times out says nothing of why: put
  `fmt.Fprintf(os.Stderr, ...)` probes in a `git worktree` copy, build its race
  binary there and run it under the same load, keeping the branch clean, and probe
  after the wait has run out, since a dump ahead of the racing step moves it. A
  probe in `World.removeEntity` printing the stack when the entity is a cursor
  names whatever ended one without a despawn.
- A closed listener's port is no refusing address: under `go test ./...` another
  package's listener can be given it and answer. Dial the near end of a held
  connection instead, which stays bound with nothing listening (vif-router's
  `TestEitherRouteRefusesBeforeDialing`).

## Locks

- The order is the world lock (`engine.UpdateMutex`, taken by `RunSafe`, not
  reentrant), then the membership's `sessionMu`, then converge's `Authority.mu`.
  Nothing floods the mesh under `sessionMu` or the world lock; a flood runs in the
  correction loop, between ticks. The session half of a run's config (`m.cfg`,
  `m.wiring`: addresses, name, policy, ceiling) changes on the run's driving
  goroutine alone, under both, so a reader elsewhere holds either: a handshake reads
  an offer's fields under `sessionMu` alone (`offerLocked`), a router's door builds
  its acceptor on each connection's goroutine (`relayAcceptor`), and a redial copies
  the whole config under the world lock (`JoinBase`), where `persistPlay` writes too.
- A flag a goroutine reads as it starts is raised before the call that starts it,
  and lowered again if nothing started: `Instance.Redial`'s dial loop may read
  `Authority.Redialing` before `Redial` returns, so `beginSuccession` and
  `redialRelay` raise their wait first.
- `World.CopyIn` overwrites `Config`, `Time`, `Player`, `Transient` and `View` whole,
  so a goroutine off the world lock (a bot seat, the drive loop, the correction loop)
  reads none of their fields, even one that never changes: the race detector flags
  the copy's write. Tests reach a play run's paths by switching a running App's
  `cfg.Mode` under `RunSafe`, so such a goroutine reads the copy of the config it
  started with instead.
- A player's setting the engine reads, as the camera edge, is a `World` atomic
  outside `Resources`, not a `Config` field: a copy a seek installs would put back
  the value it was captured with, and `livePlay` reads it off the lock. A replay's
  `player.adopt` must carry each such setting to the copy it presents (HUD, frame
  rate, camera edge), and the console's lines, or the viewer's are lost at the next
  seek. A setting only the speakers read sits on `engine.AudioResource` instead
  (`Pref`, `Bed`), which `system.HandOverAudio` moves whole to the copy; what its
  system last sent the engine goes over in that system's `takeOver`.

## Docs

- Nothing checks a doc link's file or anchor, or a backticked name, so a broken one
  outlives sweeps. After editing links or headings, run a short script that slugs
  each heading as GitHub does (lowercase, punctuation and `§` dropped, spaces to
  hyphens) over every `*.md`; its only false hits are `machine-design.md`'s
  `New[S, C](observe)` and `Decode[A](raw)`, and this note quoting them. A renamed
  or removed identifier outlives a sweep that reads prose: list every backticked
  name and path in the docs and look each up in the sources (a `go/parser` pass
  over this module and those it requires, and the words of every file but Markdown
  for flags, keys and tags); a name found nowhere is stale.
- Code, docs and scenarios spell American; `codespell --builtin en-GB_to_en-US`
  finds what slips in. Its hits that stay are Actions' `cancelled()` and the tests
  proving a `colour` key and a `programme` field refused, as `Materialise` is; it
  passes over ARIA's `aria-labelledby` and `sandbox/3d`'s `disc`, a discriminant.

## Git and branches

- A cloud checkout is shallow (`git rev-parse --is-shallow-repository`): `git log
  -S` names the boundary commit as a line's origin, and
  `git fetch --deepen=3000 origin main` reaches the rest.
- `git checkout <file>` to take back one line takes the file's whole uncommitted
  work with it: undo a line with an edit, and commit a checkpoint before a long
  tuning run.
- A session pushes only its own branch. A rebase its owner asked for goes up with
  `git push --force-with-lease=<branch>:<the head its PR shows>`, which is taken; a
  force push with no lease was refused, so a branch whose PR merged starts again
  from main only once its owner deletes it: ask.
- An agent's `git rm` stages its deletions in the checkout it shares, and the
  parent's next `git commit` takes them with another agent's work: a commit that
  does not build alone. An agent deletes with `rm`; the parent commits by path
  (`git commit -- <paths>`).
- A sub-agent's worktree lies inside the checkout, under `.claude/worktrees`: keep
  it out of commits (`.git/info/exclude`) and out of a search
  (`--exclude-dir=.claude`), and name the checkout in every command, as a `cd` into
  the worktree moves the shell there.
- A rebase replays each commit on main as it stands, so what the branch's merges of
  main settled comes back as conflicts, and what a merge commit changed beyond them
  is lost. Merge main on a scratch branch first: the rebase is done when `git diff
  <rebased> <scratch>` is empty and `git rebase -x 'go build ./...' origin/main`
  passes each commit.
