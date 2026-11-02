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
  (Robolectric's `org.robolectric:android-all` from Maven Central, the release
  `compileSdk` names: `16-robolectric-13921718` for `android-36.1`) over
  `android/app/src/main/java` and stubs for `R` and the bound `Game`, `Speaker` and
  `Listener`; the jar's `UnsupportedAppUsage` annotation warnings are noise to
  filter. Maven Central answers a shared egress 429 at times, its listing too: fetch
  the jar by its full path and retry after a pause.
- The emulator runs without KVM: `emulator -avd <name> -no-window -no-audio
  -no-boot-anim -gpu swiftshader_indirect -accel off -memory 2048 -cores 2
  -no-snapshot` on the `android-36` default x86_64 image boots in about 14 minutes
  on 4 vCPUs and takes `adb install`, `am start`, `input tap`, `input swipe x y x
  y 3000` (a held press) and `exec-out screencap -p`. It is some 30x slower than a
  phone, so the app, the launcher and system apps hit Android's 5 s input limit:
  an "isn't responding" dialog swallows every tap until its Wait is tapped (read
  its place from `dumpsys window windows`), `settings put global
  hide_error_dialogs 1` hides them but then kills the app at its first ANR, and
  `setprop ro.hw_timeout_multiplier 10` as root followed by `stop; start` (a
  minute) multiplies the limits for the next framework. Even so a press and its
  release can reach the app together after a stall, which no hold survives: the
  menu's device pass belongs on a phone or an accelerated emulator. The
  navigation bar takes the screen's bottom 126 px, so a hold on the game's status
  bar lands about 150 px above the screen's foot; the vanguard game restarts
  itself when nobody plays it, which looks like a reset the test caused.
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

- An event a system pushes in its `Update`, a crossing due at the tick and an FSM
  action's event all dispatch in the tick's one settle, after every system's
  `Update` that tick ([runtime §5](runtime.md#5-simulation-tick-steps)): a system
  first sees them in its next `Update`. A consumer that must act on it inside the
  same tick, before its own motion, reads a derived resource the producer writes
  instead, as the particle system reads `CaveResource` and the glyph spawner
  `DriftResource`.
- A derived resource is created in `engine.newGameContext`, rewritten by its owner
  every tick and never captured, so it bumps no schema. Systems run in ascending
  `Priority`, so only a later system reads it the same tick.
- A journaled event, of any origin but a system or a device, is never pushed inside
  the tick body: the tick's settle would dispatch it live while a replay injects it
  after the tick. The barrier's are the exception, stamped as the body's (settle
  group 0), which a replay queues for its own tick (`ReplayTarget.PushBody`,
  `EventQueue.Defer`) instead of settling before it.
- `NetworkSystem.localTick` is the completed tick count, not the queue stamp's
  tick: `Receive` runs inside the body, where the stamp names the tick being made,
  and reading it there judged every on-time crossing a tick late and raised the
  guest's lead by two on every shape of the divergence harness.
- A mount creates the pulse it fires in its own `Update` rather than by request, as
  missiles are: a request dispatches in the tick's settle, a tick behind the machine
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
  tick's settle, so a hold it releases at a crossing's apply tick sees everything
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
  several processes at once; load exposes it, quiet runs do not. Keep it near twice
  the CPU count: at five times, a socket join's catch-up is refused ("behind the
  session ... retry in a moment"), the harness's host ticking every 10 ms faster
  than the loaded joiner closes the gap, a refusal a production seat retries.
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
  periods. Load `internal/soundlab` with ~24 race processes pinned to one CPU
  (`taskset -c 0`), or force it with a sleep at the top of `renderTick`.
- A closed listener's port is no refusing address: under `go test ./...` another
  package's listener can be given it and answer. Dial the near end of a held
  connection instead, which stays bound with nothing listening (vif-router's
  `TestEitherRouteRefusesBeforeDialling`).

## Audio

- A labelled corpus for the rip renders offline with no device: register a bank and
  voices, `freezeSounds`, `newSoundCache().preloadAll(nil)`, `buildKit`,
  `NewSequencer(bpm, kit)` with patterns placed in its slots, then `Generate` over
  whole bars (`rip_test.go`'s `renderLoop`). The music fades in over
  `MusicStartFadeIn`, so the first bar is quiet and a recall test skips it.
- The rip's spectrogram frames are centred, so every onset reads about two frames
  (23 ms) early in every band alike; the beat grid and the events share the bias,
  and the sixteenth quantization cancels it. Do not correct one without the other.
- The default drum rules were set from the band shares a scratch test printed for
  the rendered loop's hits (a kick's rise under 100 Hz was 0.09 to 0.19 of its
  rise, a hat's above 5 kHz 0.45, a snare's noise above 5 kHz 0.15 to 0.21):
  print them again before moving a threshold, and expect a bass note's attack to
  read as a kick.
- The rip's front end is a seam: `analysis` and `ripnet` both fill `RipFrames`,
  and the decoders read nothing else, so a front end is checked by the frames it
  makes and the rip they give, as `ripnet`'s test does with an untrained model.
- `pkg/transformer` and `tool/rip-train` agree to float32 rounding, checked by
  exporting a random `ripnet` model from Python, running both on a corpus loop's
  `.mel` frames (`train.read_mel` and a scratch Go module with a `replace` to the
  checkout) and comparing the first outputs; redo that check before trusting a
  trained file after any change to either side. A CPU PyTorch installs in this
  container in about three minutes (`pip install --user torch --index-url
  https://download.pytorch.org/whl/cpu`); `corpus 8 4` plus sixty training steps
  of a width-32 model take under a minute, enough to exercise the whole path.
- `slot` is asynchronous: the mixer applies the pattern a tick later and
  `SlotPattern` reads silence until then, so a verb that acts on what was just
  slotted reads the session's own `slots`, as `render` does; a test that checks
  the engine's readout waits a tick.
- The test loop scores 0.98 under `DefaultRipParams` and loops of the shipped bank
  0.51 (`CompareRips.Total`): busy drum lanes whose chance hits count as misses,
  several tracks a slot and melodies under chord changes are where the model falls
  short, not the thresholds. `tune 4` over 12 four-bar loops took 36 s on four
  cores and reached 0.59; a set tuned on renders alone pushes `tightness` and
  `kick_share` to their range's ends, which a recording with tempo drift would
  pay for, so the defaults stay hand-set until the corpus has real material.
- A song plays through the live sequencer by scheduling each bar's patterns
  quantized while the bar before plays (`conduct`), never by setting them on the
  tick that sees the bar line, which lands them a tick late and loses the
  downbeat's hits. The melody slot's boundary is its pattern's cycle, so a
  four-bar pattern there holds the change for up to four bars; the dictionary's
  one-bar patterns change every bar. A front end that ticks sets `Session.ticked`
  and calls `conduct`; without one `song play` conducts itself until the song
  ends, with a deadline past its length in case the transport never moves.
- The searches soundlab runs are sized by fields a test shrinks
  (`Session.fit`, `Session.compressOpt`), not by constants: the rip test fits two
  notes over one generation of four and still walks every path. Seeds past the
  pool size are dropped by the engine without a word, so a pool smaller than the
  registered voices fits from the first few only.
- A command line splits on blanks (`splitArgs`); a path with a space travels in
  double quotes, which the files overlay adds. Quotes at an argument's start are
  grammar now, so a `set` value in quotes loses them.
- A verb that takes seconds freezes the TUI if it runs in the event loop: the
  first rip from the files overlay read as a crash. `longVerbs` run on a
  goroutine behind a working box; while one runs the screen draws the last frame
  and the log's tail only, so nothing reads the session the goroutine owns, and
  `LogBuffer` takes a lock because both sides write and read it. A test of such
  a verb ticks (`KeyNone`) until `busy` clears.
- `bench` and `tune` loops shorter than four seconds give no rip (a two-bar loop
  at 176 bpm is 2.7 s): size a test's loops by bars, not loops, and read the
  count `bench` prints.
- An augmented corpus draws its room, mix and voices from a stream seeded apart
  from the loop's patterns and key, so `corpus` with and without `aug` renders
  the same music; a test comparing the two relies on it. Generated parts register
  under `corpus_*` names replaced in place each loop, and `corpus` leaves those
  names out of the bank's pools, or the next draw would include the last loop's
  generated patterns and no two runs would draw alike.
- A long analysis window reads an onset early by about a quarter of the
  window: the bass's notes from the 4×2048 window started a step before the
  grid said, and lagging its rows made them start late instead. Multiply the
  long window's salience by the short window's (their geometric mean) so the
  short one keeps the timing and the long one the pitch; and give a pinned
  grid the two-frame lead the beat tracker already has, or a note on the beat
  lands on the step before.
- A training loss that keeps falling says nothing about fresh loops: 12000
  steps on 300 loops reached 0.31 and benched worse than 3000 steps on 120,
  each loop seen three hundred times. `train.py` holds a tenth of the loops
  out, scores them every `--every` steps and exports the best step; judge a run
  by its held-out line and by `bench` on the file, never by the last training
  loss, and prefer more loops to more steps.
- A bass that wobbles between two semitones leaks its octave into the melody
  register on the frames the cancellation picks the other bin: cancel the
  partials of the strongest bass pitch and of a neighbour within two semitones
  at seventy per cent of it, and the ghosts go while a clean render's melody
  keeps its score.
- A sustained pad changes the key the chroma votes for, and a note the key
  lacks snaps to its neighbour: a melody score that falls when a pad is added
  may be the key, not the pitch paths. Read the rip's root before blaming the
  tracker.
- A second voice in a register is found by masking the first path's notes with
  their partials (the octave, the twelfth, the double octave and the seventeenth
  and nineteenth, a semitone either side) before the path runs again; masking
  the fundamental alone finds the octave ghost. The gate is relative to the first
  path's mean note strength, so a clean render's second pass yields nothing.
- A recording need not sit at A440: the sample is forty cents flat, which put
  every pitch between two salience rows and read as semitone wobble (A#2 and
  A2 taking turns), chopped sustains and a wrong scale. Measure the tuning (a
  histogram of spectral peaks' offsets from the nearest semitone, what
  `tuning` does) before blaming the tracker; the front end reads it first now,
  and since a render is in tune, `bench` cannot see this failure.
- The first full bar is not where the music starts: the grid dropped the
  sample's opening phrase, two and a half seconds before its first bar line.
  A pickup bar is extrapolated when a note or a hit leads in by more than half
  a step; a render's silence before bar 0 opens none, so the corpus reads the
  same. `Downbeat` is negative then, `progression` reads no frame before zero,
  and `CompareRips` skips the pickup bar against a truth without one.
- The pitch paths start late against the onsets the beats sit on, a sixth of a
  step on the sample: the median phase of the notes is taken out before
  quantizing, which lifted the test loop's melody from 0.95 to 0.98 and a cut
  recording's from 0.93 to 0.98. Under that sharper grid the second pass's
  fragments cost the pad test's melody two hundredths against the solo pass,
  so that guard allows five.
- The rip-back cannot judge the rip's own errors: a note misheard in the
  recording is misheard the same way in the render, so a search on it polishes
  the wrong transcript. `Fidelity` compares the render's frames with the
  recording's through one front end; a render a semitone sharp scores 0.56 in
  pitch against 1.0 for itself, with its onsets still at 1.0.
- A `Ripper` of zero parameters hangs: zero costs let the pitch path jump every
  frame and the tempo prior degenerate. `Samples` and `Hear` take the defaults
  for the zero value now; a scratch tool that forgot `DefaultRipParams` ran
  for ten minutes on a twelve-second clip.
- `bench` grades the analysis as it is today, so a run's numbers compare only
  with the analysis benched beside them: the seventh pass moved the analysis
  from 0.47 to 0.49 on clean loops, and the fourth model run's 0.45 and 0.51
  stand against that, not against the third run's table. The model's corpus
  is rendered at A440, so nothing it saw was detuned; a recording off the
  standard is a recording it was never shown, whatever its held-out loss.
- A recipe's relative paths were the process's: `run /elsewhere/play.slab`
  failed to find the documents beside it, and an installed soundlab has no
  working directory worth the name. A running script's relative paths are
  read from its own directory (`Session.base`, `RunScriptFile`), nested runs
  from theirs, and the scratch recipes name their files from where they lie.
- A render at A440 beside a recording forty cents flat is heard as the same
  notes sharpened, "a stretched string", before any note is judged: the ear
  reads a tuning offset as timbre. Render, play and fit at the recording's
  tuning (`SongDef.Tuning`, `SetTuning`, `RenderNoteAt`); the rip reads it.
- Two transcripts of one piece are a check no single rip gives: the sample and
  its origin (three semitones apart, one stretched, on different metrical
  levels) agree on 41 of 49 melody notes, 27 of 29 under notes and all the
  bass after transposition and a fitted time map (a scratch comparison over
  two `notes` listings), so the pitch content is mostly consistent and what
  the ear misses is elsewhere: the two lines swapping a held dyad between them,
  and durations. Ask for the melody as notes and for a solo take of the lead;
  they are the ground truth the numbers lack.
- The first and second pitch paths swap roles on a held dyad: the louder note
  changes hands from frame to frame, so each line hears a trill where the
  music holds a chord, and two fitted voices play it as two instruments
  alternating. Assign concurrent notes to lines by pitch order (`uncross`).
- The sequencer's range is 80 to 200 bpm and the tempo decoder folds into it,
  so the origin recording, near 74 bpm, reads at 148 while the intro, sped up
  to 86, reads at 86: the same piece on two metrical levels, a sixteenth 101
  ms in one rip and 174 in the other. `rip ... bpm <n>` pins the tempo alone
  (a grid with a negative downbeat), so the two rips can be put on one level.
- The sweep's moves were all relative to the note the rip heard, so a note it
  missed had no way in and one wrong by a third no way out. The recording's
  own strongest pitch over a step (its salience rows, which the fidelity's
  sound already holds) is the proposal: tried as a move on a step that has a
  note and as an insertion on one that has none, kept when the bar gains.
- A search over every step's genes at once (312 genes, six generations of
  twelve) kept no move on the sample: one note's move changes one frame in a
  thousand of the fidelity, under the noise of a dozen genes moving with it.
  A sweep that tries each step's five moves alone and keeps a gain sees it;
  five renders a step is the price, minutes a round on a four-bar song.
- A training run shares the cores with everything else the session does: the
  fourth run slowed from 0.4 to 4 seconds a step under a rip and a test run,
  and the two-hour waiter on it timed out twice. Size a waiter by the run
  alone and expect to re-arm it, or follow the run by a reminder instead.

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
  Regenerate into scratch and diff: only the new glyph changes. The test reads
  every string in `internal/mode`, `app`, `help` and `prof` too, so a key nobody
  sees stays ASCII, and a comment after code on its line as code, so that comment
  does too.
- Linux console fonts (console-setup's Uni and Lat sets) have █ ░ ▒ and single box
  lines but no ▀ ▄ ▌ ▐, quadrants or ▓, and draw double lines single; the 256 tier
  uses only what they have.
- The 256 tier's entries 0–15 take an emulator's theme, so no contrast between two
  of them is promised; a colour, or 15, on entry 0 is safe.
- On the Linux console the 256 tier shows sixteen colours: a pale tint over a rim
  falls to the rim's own, so a glow that must read there is near white, as the
  Wraith field's is.

## Widgets

- `tui.TextFieldOpts.Border`'s zero value is `LineSingle`, a box three rows high, so
  a one-row text box (the menu's) passes `tui.LineNone` or draws nothing.

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
  ends the run at once with a refused join's message. A `devices['Pixel 7']`
  context is the phone's page, where a held `mouse.down` on the canvas's last
  rows is the finger that opens the menu; `page.keyboard.press('Control+g')` is
  the desktop's. `pkg/browser`'s tests run under node with
  `GOOS=js GOARCH=wasm go test -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec"`.
- The scheduler loop's cadence is read with a probe system whose `Update` records
  `time.Now()` and sleeps on one tick, under a goroutine that answers each
  `updateDone` with a `frameReady` after a frame's delay (`NewScheduler`, `Start`);
  the gaps between starts around the slow tick show what a late frame does.
- Whether a motion reads smooth is a frame question, not a tick one: render each of
  a tick's three frames with the context's `GameTime` set that far past
  `Time.PaceTime` and count the cells the layer moved. A pace off a multiple of
  twenty a second, drawn only where its tick left it, moves in uneven steps however
  fast it is.

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
- A frame diff of a renderer compares each cell's `BackgroundAt` with a sentinel
  fallback, not the theme background: a write that leaves a cell's colour as it
  was still marks it drawn and adds the layer's mask, which dim, grayout, strobe
  and a later `SetBgScreen` base read.
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

## Genetic

- The capture carries `registry.SpeciesState`, which embeds the streaming
  engine's checkpoint, so a change to `genetic.StreamingState`'s layout bumps
  `snapshot.Schema` beside `StreamingStateVersion`.
- A sandbox over `pkg/genetic` is tuned by its `-headless` table, several seeds
  at once in the background with a `sed` variant per constant, not by watching
  it: `sandbox/genetic`'s crash rate at one setting varies by half between
  seeds, and its archive statistics read well long before its streams do.
- An elitist archive under a noisy objective selects for luck. Score a
  genotype by the mean of several evaluations and re-evaluate archived members
  (`sandbox/genetic`'s trials and refreshes) before touching the operators.
- `Engine.Checkpoint`'s members alias the engine's buffers, which the next
  generation writes over: copy them before running the source engine on, as its
  test does. `Restore` into an engine of another configuration or pool size is
  refused rather than reshaped.
- An assignment genome under `CategoricalPerturbator` wants a rate of one over
  its length and a `MixProbability` near one, so most children differ from a
  parent by one move; under a Jaccard distance the search rarely beats the
  greedy grouping it is seeded with, which is expected, not a bug: the size
  term and the greedy pass apply the same pressure.

## Branches

- A session pushes only its own branch and a force push is refused, so a branch
  whose PR merged starts again from main only once its owner deletes it: ask.
- Take the next `snapshot.Schema`, `event.JournalSchema` or `manifest.SchemaVersion`
  past main's, and check again after merging main, where another PR may have taken
  it.

- **A voice-like trait that is not a gene rides beside the genes, not in them.**
  Vibrato and a vowel's formants went on `VoiceDef` as plain fields, kept by
  the augmentation after `VoiceFromGenes`, rather than as new genes: a gene
  changes the model's voice head and every shipped weight file's shape, and the
  fit would search a space the recording rarely needs. Add a gene when a corpus
  with the trait has trained and the fit wants it, not before.
