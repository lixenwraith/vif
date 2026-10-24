# vif Development Timeline

How vif's features, and the defects chased along the way, followed one another
from the first commit to 2026-10-20: a section a month, a line a topic, each line
folding every commit on that topic within its month. The project was vi-fighter
until its rename to vif in September 2026.

## 2025-04 to 2025-11 — Prototype and core game

- Repository opened in April; the prototype from November 16
- Vim motions, counts, operators, find/till and `%`
- Normal, Insert, Search and Command modes
- Ping grid, coordinate highlighting, heat and score
- Decay, then a Matrix-style falling decay
- Code blocks from files as the text to type
- Gold, cleaners, nuggets with the `Tab` jump, drains
- Sound effects, then an audio engine with `Ctrl-S` mute
- Race conditions and decay ticker bugs; race tests in CI
- Clock scheduler, monotonic time and pause
- Spatial transactions for atomic spawns
- Lock-free event queue, then an event router
- A generics ECS replaced the reflection one
- Resources replaced the god object; score became energy
- Dense zero-allocation spatial grid
- Materialize spawns, destruction flash, four-way cleaners
- FPS, game time, APM; help and debug overlays
- Renderers as systems; an orchestrator; z-index layers
- Input as a state machine; bootstrap and reset split out
- Shield zones, heat-based multi-drain, ring-buffer trails

## 2025-12 — Terminal, effects and architecture

- Own terminal package replaced `tcell`; resize fixes
- Pure-Go piped audio replaced `beep`
- Truecolor and 256-color dual-mode rendering
- Blending, masks and stencil post-processing; a blend tester
- Splash text from a generated bitmap font; the magnifier
- Central crash handling
- Systems decoupled by events; tag-and-cull death
- Timer components and a timekeeper
- Hierarchy Map for dependency exploration and LLM context filtering
- Manifest registry wiring every system
- Data-driven FSM game script; TOML configuration
- External resources as services; a TCP/TLS network stub
- Composite entities, gold first; a 1 ms event-loop scheduler
- Blossom and negative-energy variants
- Fixed-point vmath and sub-cell kinetic physics
- TUI components, the overlays rebuilt on them
- Fuse and quasar, with charging and zaps
- Glyphs replaced typeable characters
- Lightning and spirit effects

## 2026-01 — Dust, combat and physics

- Quasar hit points, hit flash and shield knockback
- Dust: shield pickup, timers and explosion shuffle
- Component stores generated from the manifest
- Synthesized sound generators; the music system
- Terminal WASM support
- Q32.32 vmath and unified physics
- FSM parallel regions in a TOML game script
- Explosion system rewritten
- Visual mode and bound-aware motions
- Diagnostics system; live system enable/disable
- Splash timers decoupled from gold; grayout as environment
- Vampire, buffs, rod and overheat
- Combat interaction matrix, split from kinetics
- Swarm, and soft collisions between enemies
- Gameplay values in a parameter package
- Alt keys, mouse and motion markers
- First genetic algorithm trial, on drains

## 2026-02 — Species, walls and navigation

- Genetic package generalized; a TOML encoder
- Recorded input macros
- Orbs, missiles, bullets and the polarity disruptor
- Walls, wall fade and a maze generator with rooms
- Mouse click-to-move, then free and auto modes
- Loot system and its drops
- Health bars; `ascimage` for wall images
- Storm: three ablative spheres and their attacks
- Camera dead-zone follow for maps beyond the screen
- Flow-field navigation, then composite pathfinding
- FSM variables, guards, background regions and difficulty
- WASM build target
- Status bar typing and messages; strobe effects
- Ember's heat-draining invincibility; movement undo
- Pylon, snake, eye and tower species
- Species collisions and targeting centralized
- Command-line history; keymap overrides from TOML
- Tower defense: gateways, route graphs, a bandit learner
- Batched events and entity signatures in the ECS

## 2026-03 to 2026-06 — A quiet spell

- Component masks back to maps after a concurrency review
- Nugget spawn delay; genetic pool race and maze panic fixed

## 2026-07 — Modules and audio tooling

- Experimental Windows terminal build
- Float vmath beside Q32.32
- FSM region resume and map handling overhaul
- Charge-based shots; cluster missiles and dust simplified
- `terminal`, `toml` and `color` extracted as modules
- Genetic integration rebuilt; route adaptation reworked
- A large tower-defense map
- App split into bootstrap and composition; generated events
- Audio as its own package, with external sound effects
- Soundlab: REPL, TUI and piano
- Fewer mutexes; a reset deadlock fixed
- Persistent mouse and auto-fire defaults; content service

## 2026-08 — Determinism, replay and domains

- Structured logging, tracing and signals; status diagnostics
- `genetic`, `vmath` and `physics` as reusable packages
- Faster ECS update and render iteration
- Fixed point replaced by float64
- FSM transition logs; debug HUD and dynamic key help
- Time scaling and tick stepping
- Deterministic clock and RNG for replay
- Journal, headless replay and the play/headless/replay modes
- Cursors as entities, for several players
- Telemetry across systems, engine and events
- Unified species creation and death events
- Shared and player domains on entities, events and systems
- Framed TCP sessions in lockstep behind a playout barrier
- `vif-log` journal indexer
- Mesh relay, lobby and mid-run joins
- Live divergence detection
- Desync study; host authority replaced the replay join

## 2026-09 — Multiplayer correction and the K3s fleet

- Local cursor prediction, host authority, adaptive cadence
- Join anytime: world capture and staged install
- Selective correction, authority terms, succession, relays
- Dedicated hosts, headless scripts, scripted participants
- `wad/` layout, a release archive and a live fleet volume
- K3s fleet: a Job per session, then an allocator
- Fleet logs through LogWisp; one-command node deploys
- Two-instance defect rounds: gold, storm, swarm, kill credit
- Wind, unified particles, dimmed peer shields
- Corrections played out, guests paced, links compressed
- Special attacks, auto-fire cycle, heat-driven drains
- Scenarios by content, changed live, sent to joiners
- `script/test.sh` replaced the phase scripts
- Browser route via a `websocat` bridge; Go in a Worker
- Pointer play, gesture APM, music that follows APM
- 256-color text-console tier
- Weapon spec table; turret and beam
- Packaging and tagged releases; renamed vif from `vi-fighter`
- Telemetry cards and `:debug` profiling
- Scaling audit: snapshot reuse, relay stopgap, paced bodies
- `vif.toml` defaults, music to WAV, `-host` plays at once
- Guest journals replayed from their join
- Name routing, own WebSocket, `websocat` retired
- FSM-graph bots in participant seats
- In-game configuration menu

## 2026-10 — Kraken, replay and the host model

- Kraken, its maze before Tower, health bars over species
- Behavior machine design
- Replay step back, seek, digests and per-runtime logs
- Combat statuses, charge caps, ray, the cleaner as gun
- Go 1.27.2; terminal v0.2's themed TUI
- Network failures name the next step; messages stay up
- User installs, uninstall, root-owned archives
- TOML macros for scenarios and bot graphs
- Bot fitness and threat evasion; genetic archive dedup
- Snapshot row reuse, owed busy links, bot relays
- Timer slots, tick-closing settle, dust into particles
- Parallax glyph drift; the input hook and simon
- Component logic moved into its systems
- Docs swept against the code; a term index
- Netplay stalls, lead and repairs; the session as a tree
- Browser touch play, then cell frames without xterm.js
- Host model: Presentation, audio sink, font atlas
- Desktop window through purego, EGL and GLES 2
- Android host via `gomobile bind`, then APK nightlies
- Window in every build; menu settings saved to `[play]`
- Music rework: bass, motif ladders, limiter
- Gold across main and quasar; a defeat is a new game
- Hot paths: wall epoch, store index, renderer frame costs
- Navigation rework: td's worst storm tick 132 to 30 ms
- Journals across resets, then opened and closed mid-run
- Windows play notes; a README for each release archive
- Netcode design: bodies raw past the link, bundled links, one-trip descent, leaf echo
