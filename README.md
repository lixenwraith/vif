# Vi-Fighter

Vi-Fighter is a real-time terminal game that combines Vim-style navigation and
text operations with typing, shooting, procedural encounters, adaptive enemy
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
  spatial grid, composite actors, and a single explicit world-lock boundary.
- TOML-authored hierarchical state machines with parallel regions, dynamic
  encounters, payload capture/injection, guards, delayed actions, and system
  control.
- A layered terminal-cell compositor with truecolor/xterm-256 support, blend
  modes, semantic masks, camera transforms, and post-processing.
- Pure-Go synthesis, SFX mixing, and a three-slot sequencer whose tempo and
  arrangement respond to player APM; PCM is streamed to common host audio
  tools, FreeBSD OSS, a null sink, or WAV capture.
- Aspect-aware flow fields, footprint-aware route graphs, online route
  adaptation, streaming genetic optimization, Q32.32 storage, and both fixed
  and floating-point physics/math paths.
- Plain-text and authored TOML typing corpora, embedded fallback scenarios and
  tutorial content, image-to-terminal wall assets, and dedicated audio/image/
  visual authoring tools.

The simulation is not advertised as globally bit-for-bit deterministic: several
Q32.32 operations intentionally use hardware floating point, and real-time
scheduling/random seeds are part of normal play.

## Build and run

The module currently declares Go 1.26.5.

```bash
git clone https://github.com/lixenwraith/vi-fighter --depth 1
cd vi-fighter
make release
./bin/vif
```

Useful targets include `make dev`, `make test`, `make verify`, `make tools`,
`make wasm`, and `make serve`. Audio starts muted; press `Ctrl-S` to cycle audio
channels or launch with `-au`. Run `./bin/vif -h` for all flags.

Primary native targets are Linux and FreeBSD. The repository also contains a
constrained xterm.js/WASM build and an experimental Windows cross-build.

## Configuration and tools

- `-g <game.toml|directory>` selects an encounter configuration.
- `-f <content-file|directory>` selects typeable `.txt`/`.toml` content.
- `-k <keymap.toml>` applies sparse key overrides.
- `-check` validates resolved FSM/content without opening the game.
- `-schema` exports the current event/action/guard schema as JSON.
- `cmd/soundlab` authors and auditions sounds/music.
- `cmd/ascimage` converts and previews dual-mode `.vifimg` assets.

## Documentation

Start with the [engineering documentation index](doc/README.md). It links the
high-level architecture, medium-level package/runtime diagrams, and detailed
references for gameplay, ECS/events, input, FSM configuration, rendering,
audio, navigation/evolution, content/assets, services/networking, and
development.

## License

BSD-3-Clause.
