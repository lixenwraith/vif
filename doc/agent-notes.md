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
- After bumping `lixenwraith/terminal`, build `./...`, not only `./cmd/vif`: the
  tools and sandboxes break on a change the game does not use.
- Every shipped target is 64-bit, and `internal/core/arch32.go` refuses the rest.

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

- A few `internal/app` tests over real sockets fail on main alone, a few percent of
  runs and more under `-race` or load: `TestAStillGuestSeatsBotsAfterItsHostLeaves`
  and `TestAPersonBehindARelayReachesTheHostThatTookOver` most, a relay-join hold
  and a mid-session journal test rarely. A failure there takes about 16.5 s, a 15 s
  socket wait running out. Before calling one a regression, run it alone in fresh
  processes, alternating main's binary and the branch's under the same load.
- A race only CI saw: build once with `go test -race -c`, then run the test in
  several processes at once; load exposes it, quiet runs do not.
- In a CI log a `--- FAIL` line sits inside its package's block, not at the end:
  save the whole log and search it. For a hang, find `panic: test timed out` and
  match the goroutines blocked in `sync.Mutex.Lock` against what each holds.

## Locks

- The order is the world lock (`engine.UpdateMutex`, taken by `RunSafe`, not
  reentrant), then `App.sessionMu`, then converge's `Authority.mu`. Nothing floods
  the mesh under `sessionMu` or the world lock; a flood runs in the correction loop,
  between ticks.

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

## Words

- Materialize names the materialize effect alone; a teleport's or a charge's line is
  a line, never a beam.

## Branches

- A session pushes only its own branch and a force push is refused, so a branch
  whose PR merged starts again from main only once its owner deletes it: ask.
- Take the next `snapshot.Schema`, `event.JournalSchema` or `manifest.SchemaVersion`
  past main's, and check again after merging main, where another PR may have taken
  it.
