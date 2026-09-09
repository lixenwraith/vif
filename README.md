# Vi-Fighter

Vi-Fighter is a real-time terminal game that combines Vim-style navigation and
text operations with typing, shooting, procedural encounters, adaptive species
movement, and generative audio.

The game is written in Go and has no application CGO requirement. Terminal,
TOML, color, and logging primitives are maintained as separate Go modules;
Vi-Fighter owns the ECS, gameplay, input semantics, compositor, scenario
configuration, audio policy, and reusable simulation libraries.

## Highlights

- Vim-inspired Normal, Insert, Visual, Search, Command, and Overlay modes with
  counts, motions, delete operators, find/search repeat, undo, and concurrent
  recorded macros.
- A typed sparse-set ECS, fixed-step scheduler, bounded event settling,
  spatial grid, composite actors, FSM-owned player cursors, and a single
  explicit world-lock boundary.
- Four runtime shapes: interactive play, a dedicated host with no terminal and no
  cursor of its own, deterministic caller-driven or authored-script headless runs,
  and terminal playback of recorded runs.
- TOML-authored hierarchical state machines with parallel regions, dynamic
  encounters, payload capture/injection, guards, delayed actions, and system
  control.
- A layered terminal-cell compositor with truecolor/xterm-256 support, blend
  modes, semantic masks, camera transforms, and post-processing.
- Pure-Go synthesis, SFX mixing, and a three-slot sequencer whose tempo and
  arrangement respond to player APM; PCM is streamed to common host audio
  tools, FreeBSD OSS, a null sink, or WAV capture.
- Aspect-aware flow fields, footprint-aware route graphs, online route
  adaptation, streaming genetic optimization, and cell-centered `float64`
  motion, geometry, and physics.
- Plain-text and authored TOML typing corpora, embedded fallback scenarios and
  tutorial content, image-to-terminal wall assets, and dedicated audio/image/
  visual authoring tools.
- A replay journal, versioned tick-script runner, manual-clock harness, exact
  rational time controls,
  per-region FSM telemetry, structured logs, status snapshots, and a triggered
  flight recorder for reproduction and diagnosis.
- Multi-participant play over framed TCP, with deterministic shared simulation,
  owner-authored cursor state, a fixed-delay crossing barrier that relays
  artifacts to participants a producer never linked to, roster changes that land
  on one agreed tick, selective authoritative correction with bounded keyframe
  fallback, and clean continuation after a peer disconnects.

Interactive play is not advertised as globally bit-for-bit deterministic:
simulation math uses `float64`, which is not a cross-platform lockstep
contract, and live goroutine scheduling affects event timing. Headless and
replay Apps instead use a manual clock and are deterministic for one build from
their seed, config, and injected event sequence; bit-exact replay is claimed
for headless recordings, not arbitrary live sessions or across platforms.

## Build and run

The module currently declares Go 1.27.1.

```bash
git clone https://github.com/lixenwraith/vi-fighter --depth 1
cd vi-fighter
make release
./bin/vif
```

Useful targets include `make dev`, `make test`, `make verify`, `make tools`,
`make wasm`, and `make serve`. `make install-config` copies the `wad/` payload
and the default keymap under the user config root without replacing existing
files; `make install` stages the same payload for a distribution package. Audio starts muted; press `Ctrl-S` to cycle audio channels or
launch with `-mute=false`. Run `./bin/vif -h` for all flags — it prints to
stdout, so it pipes into `grep` without redirecting stderr.

Primary native targets are Linux and FreeBSD. The repository also contains a
constrained xterm.js/WASM build and an experimental Windows cross-build.

## Configuration and tools

- `-g <game.toml|directory>` selects an encounter configuration.
- `-f <content-file|directory>` selects typeable `.txt`/`.toml` content.
- `-k <keymap.toml>` applies sparse key overrides.
- `-config-dir <root>` puts one categorized config tree ahead of user/system
  discovery; `-config-music` and `-config-sounds` select audio overrides.
- `-check` validates resolved FSM, keymap, audio, and content without opening
  the game.
- `-schema` exports the current event/action/guard schema as JSON.
- `-version` prints the module version and commit a package should report.
- `-seed <n>` selects the root RNG seed and `-speed <rate>` selects an exact
  startup rate from `1/8` through `8`.
- `-j[=DIR]` records replay input to a dedicated journal; `-replay <file>`
  presents a journal on the terminal with fixed playback controls.
- `-script <file>` runs a bounded authored TOML input/event schedule headlessly;
  it can be combined with `-host` or `-join` for repeatable two-process runs.
- `-host <bind-address>` hosts a session and `-players <n>` sets the lobby size;
  `-join <host:port>` joins it and adopts the host's seed/config/content identity.
- `-authority host|migrate` decides where authorship goes when the participant
  holding it leaves — default `host` with `-serve`, `migrate` otherwise. In a
  migrate session every guest also binds a port so the session can reach it after a
  handoff: `-listen <addr>` pins one, the default is the host's own port falling
  back to an OS-assigned one, and `-no-advertise` keeps this participant out of the
  address map, which leaves it playing normally and never elected.
- `-serve <bind-address>` runs a dedicated host: no terminal, no renderer, no
  audio and no cursor of its own. It starts on its first guest and admits the rest
  as they arrive, where `-players <n>` is a ceiling rather than a requirement.
  `-size <WxH>` gives it the geometry it has no terminal to derive; without it the
  first guest's terminal sizes the session.
- `-probe <bind-address>` serves `/health` and Prometheus `/metrics` for a `-serve`
  run, and `-log-stdout` writes the session log to stdout as JSON.
- `-first-join <d>`, `-empty <d>` and `-drain <d>` bound a `-serve` session that was
  allocated on somebody's behalf: end it if no guest arrives, end it after the last
  one leaves, and let a termination signal wait for the roster instead of cutting
  the match. Omitting them is the long-lived host an operator starts by hand.
- `-l`, `-ls`, `-lt`, and `-lr` — long forms `-log`, `-log-scope`, `-log-stat`,
  `-log-recorder` — enable structured logging, scoped snapshots, and
  flight-recorder history.
- `cmd/soundlab` authors and auditions sounds/music.
- `cmd/ascimage` converts and previews dual-mode `.vifimg` assets.

Editable files ship in `wad/`, laid out exactly as they install; everything the
binary can play without a filesystem is embedded in `internal/asset`. User
configuration defaults to `$XDG_CONFIG_HOME/vi-fighter`; logs and journals
default to separate directories under `$XDG_STATE_HOME/vi-fighter`. See the
[external filesystem layout](doc/filesystem-layout.md) for exact precedence,
installation, and WASM behavior, and [packaging](doc/packaging.md) for the
distribution checklists.

For a local two-terminal session, run `./bin/vif -d -host 127.0.0.1:7777`
in the first terminal and `./bin/vif -join 127.0.0.1:7777` in the second; add
`-players <n>` to the host for a larger lobby. A participant joins at startup or
mid-run: the host sends an authoritative shared snapshot of the world at whatever
tick the session has reached, which is also how a peer that dropped comes back.
Live installs reconcile configuration-marked persistent local FSM effects, so a
correction that skips an encounter exit cannot leave that participant's drains or
screen state latched.
The session is plaintext and trusted-peer — a dial is rate-limited per address and
bounded in what it can allocate, but it is not authenticated, so a host reachable
from an untrusted network needs one in front of it.

For an automatic 2,000-tick headless pair, use `script/phase3-host.toml` and
`script/phase3-guest.toml` as documented in `doc/development.md`. For a host
nobody sits at, `./bin/vif -serve :7777 -size 120x40` waits for its first guest
and then runs the session on its own.

`deploy/` holds the container image and the K3s objects that run one such session
per player request: a `scratch` image of one static non-root binary, a namespace
capped at ten concurrent sessions, default-deny network policy, a per-session Job
and Service, and a log-streaming sidecar. `make image` builds it; the installation
and operating procedure is [doc/kube_docker_deploy.md](doc/kube_docker_deploy.md)
and the plan and work list behind it is
[doc/kubernetes-fleet.md](doc/kubernetes-fleet.md).

`test/scenario.sh` runs named game setups for verifying behaviour by hand —
`solo`, `host`/`join`, `serve`, `serve-fleet`, `probe`, and automated checks for the
session lifetime, the drain, and join-identity refusal. See
[test/README.md](test/README.md).

## Documentation

Start with the [engineering documentation index](doc/README.md). It links the
high-level architecture, medium-level package/runtime diagrams, and detailed
references for gameplay, ECS/events, input, FSM configuration, rendering,
audio, navigation/evolution, content/assets, services/networking, and
development.

## License

BSD-3-Clause.
