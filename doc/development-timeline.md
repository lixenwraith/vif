# vif Development Timeline

High-level progress collapsed from commit messages and `doc/`, April 2025 through
2026-10-19. The project was vi-fighter until it was renamed vif in September 2026.
Each line folds many commits, so dates within a section are approximate.

## 2025-04 to 2025-11 — Prototype and core game

- The repository opened in April 2025; the vi-fighter prototype began in November.
- Vim-style movement established as the primary control model.
- Relative coordinates and cursor highlighting added.
- Counts, motions, and character sequences expanded.
- Normal, Insert, Search, and Command modes introduced.
- Scoring, heat, ping, and cursor feedback added.
- Delete operators and find motions completed.
- Search repeat and coordinate highlighting refined.
- Decay became the first persistent world effect.
- Code blocks loaded from files became the content the player types.
- Gold, Cleaners, Nuggets, and Drain became the core mechanics.
- The initial monolith was split into game modules.
- Trail effects and game timers were introduced.
- Audio playback and sound effects were added.
- Clock handling was unified across frame and game time.
- Pause drift and ticker lifecycle problems were fixed.
- CI ran race tests added.
- Score semantics were replaced by energy.

## 2025-11 — ECS, events, and rendering foundations

- Gameplay state began migrating into an ECS.
- A generics ECS replaced the reflection-based one.
- Cleaner and phantom-cleaner lifecycles moved into systems.
- Position storage replaced scattered coordinate ownership.
- A dense zero-allocation spatial grid was introduced.
- Materialization became a first-class game effect.
- Four-direction cleaner spawning was completed.
- Event queues moved behind a centralized router.
- Immediate input dispatch removed fixed input latency.
- Input handling was separated from gameplay logic.
- Mode handling moved into a state machine.
- Overlay windows and status indicators were added.
- Rendering gained explicit z-index ordering.
- Shield visuals and blending were introduced.
- Renderer migration restored cleaner frame accounting.
- Gold sequencing was separated from bootstrap state.
- Reset behavior was separated from transient game state.
- Cleaner and materialize trails moved to ring buffers.
- Heat-based multi-drain mechanics were added.
- Rapid drain spawning and resize corruption were fixed.

## 2025-12 — Terminal, effects, and architecture

- The renderer was isolated behind a render package.
- A dedicated terminal package replaced `tcell`.
- Truecolor and xterm-256 modes became explicit.
- Resize, tmux zoom, and narrow-status handling stabilized.
- Frame-copy flushing was removed from the terminal path.
- The external `beep` dependency was replaced by piped audio.
- Decay, cleaners, and materializers adopted position storage.
- High-entity queries and spatial limits were expanded.
- Color blending and destruction-flash paths were optimized.
- Mask-based post-processing became available to renderers.
- Splash effects gained generated fonts and timed lifecycles.
- Splash and gold destruction event handling was corrected.
- Crash handling was centralized in the core package.
- Heat rendering gained dual-mode visuals.
- Shield state moved into cursor-owned components.
- Delete operations were routed through events.
- Energy and gold systems were decoupled through events.
- Tag-and-cull lifecycle handling was introduced.
- Out-of-bounds state became a general death marker.
- Energy and heat migrated into ECS components.
- Ping effects migrated into ECS components.
- Mode handling became thread-safe.
- Input intent parsing was separated from mode routing.
- Timer components and a timekeeper system were added.
- Hierarchy Map was built for dependency exploration and LLM context filtering.
- Boost and materialize behavior moved to events.
- Destruction flash became event-driven.
- Systems received the world instead of game context.
- Spawn handling was decoupled from game state.
- A generic manifest registry centralized system wiring.
- FSM control became data-driven through game scripts.
- TOML replaced JSON for runtime configuration.
- External resources moved behind service interfaces.
- Service lifecycle callbacks and hooks were added.
- Composite entities formalized header/member actors.
- Death handling absorbed cull responsibilities.

## 2025-12 to 2026-01 — Physics and combat expansion

- Blossom joined decay as a mirrored particle behavior.
- Negative-energy variants were added to cleaners and nuggets.
- Fixed-point vector operations supported sub-cell motion.
- Kinetic components became reusable physics state.
- Cleaner and materializer motion adopted vector math.
- Magnifier splash effects were introduced.
- Q16.16 elliptical math powered curved effects.
- Overlay rendering moved onto reusable TUI components.
- Materialize occlusion and shield reset bugs were fixed.
- Glyphs replaced the earlier typeable-character model.
- Lightning became a sub-pixel effect.
- Spirit replaced lightning as the quasar spawner.
- Drain-cleaner collision gained explicit physics.
- Quasar movement migrated into the kinetic model.
- Quasar acceleration, orbit, and deflection were refined.
- Dust gained components, systems, physics, and timers.
- Dust explosions could reshuffle the level.
- Grayout state moved into the environment domain.
- Combat and kinetic responsibilities were separated.
- A combat interaction matrix centralized hit rules.
- Composite targets gained priority-aware hit selection.
- Shield collisions and knockback moved into combat.
- Vampire, buff, rod, and overload mechanics were added.
- Visual mode, bound-aware motions, Alt keys, and mouse handling extended input.
- Gameplay values were centralized in the parameter package.
- Music sequencing and additional sound generation added.

## 2026-01 to 2026-02 — Weapons, species, and navigation

- Wall physics, bounds, fading, and exterior glyphs landed.
- Maze generation began driving wall-based scenarios.
- Loot became a dedicated system with energy rewards.
- Missiles gained targeting, seeking, and collision behavior.
- Orb firing moved behind weapon and buff systems.
- Storm gained physics, rendering, combat, and soft collision.
- Storm circles adopted an ablative composite model.
- Camera follow with a dead zone was introduced.
- Flow-field navigation supported obstructed maps.
- Navigation requests and corner handling were optimized.
- Genetic resources were decoupled from navigation.
- Bullets and multiple loot drops were added.
- WASM builds became a supported demonstration target.
- Mouse input and free-mouse play were introduced.
- The status bar gained persistent messages and typing state.
- Disruptor added a polarity-sensitive pulse weapon.
- Orb rendering gained dual-mode visual treatment.
- Difficulty scaling moved into the FSM game cycle.
- Transient renderer state was centralized.
- Strobe effects became a reusable presentation layer.
- Ember introduced heat-draining invincibility.
- Gameplay balance was adjusted around Ember.
- Shield and Ember behavior were unified.
- Pylon added a new component, system, and renderer.
- Species collisions moved into a shared collision system.
- Snake introduced a moving ablative composite species.
- Generic palettes replaced scattered RGB definitions.
- Eye added ranged behavior, rendering, and genetic tuning.
- Composite targeting and pathfinding were corrected.
- Tower introduced another composite enemy type.
- Gateway component and system moved repeated spawning out of game scripts.
- FSM actions learned to capture payload fields.
- Route graphs enabled stateful path selection.
- Genetic adaptation was separated into a bandit learner.
- Tower-defense gateways used route adaptation.
- Entity signatures accelerated component removal.

## 2026-03 to 2026-07 — Portability and modularization

- Component masks returned as maps after concurrency review.
- Position and component-mask access was simplified.
- Windows terminal support was added experimentally.
- FSM region resume restored configured system state.
- Dust interaction with glyphs and particles was removed.
- Cluster-missile staging was eliminated.
- Weapons switched from heat-scaled to charge-based shots.
- FSM and map embedding received a major overhaul.
- Terminal and TOML became standalone modules.
- Truecolor support moved into a standalone color module.
- Render code adopted the external color package.
- Gold destruction and composite death counting were fixed.
- The genetic integration was rebuilt in phases.
- Adaptation changed to a cleaner route-selection strategy.
- A large tower-defense map was added.
- The app was split into composition and bootstrap layers.
- Event registration moved to generated definitions.
- Audio and music were restructured for standalone use.
- Soundlab grew REPL, TUI, and piano workflows.
- Headless audio paths supported tools and tests.
- Renderer flushing moved outside the world lock.
- Store, position, and mode-router mutexes were removed.
- Live component views reduced targeting allocations.
- Mouse and auto-fire preferences became persistent defaults.

## 2026-08 — Diagnostics, replay, and domains

- Structured logging, tracing, and signal handling landed.
- Diagnostics moved into a dedicated status layer.
- Panic logs and development-build reporting were added.
- Navigation and route switching were refined.
- The genetic package was fully decoupled from the game.
- Vmath and physics moved into reusable packages.
- Combat profiles centralized collision parameters.
- Float-backed math replaced fixed-point internals.
- Physics and vmath APIs reached feature parity.
- Overlay navigation gained pins and a debug HUD.
- Exact time scaling and single-tick stepping were added.
- Operator preferences survived ordinary game resets.
- Clock and RNG ownership were prepared for replay.
- Event origins established the journal foundation.
- Headless replay recorded deterministic input streams.
- Play, headless, and replay became explicit app modes.
- Cursor state migrated from singleton to entity ownership.
- Multiplayer preparation swept cursor-aware systems.
- Replication domains were assigned to entities and events.
- Shared and player-local behavior became explicit.
- Domain coverage expanded across systems and snapshots.
- A network system, framed TCP transport, lobby, and playout barrier brought lockstep play.
- Live divergence detection named the record a session diverged on.
- The vif-log tool indexed journals and attributed records by domain.

## 2026-09 — Multiplayer and dedicated hosting

- Selective snapshot correction replaced full-world repair.
- Correction indexes identified only divergent state.
- Authority terms and succession enabled host migration.
- Relays preserved participants behind indirect links.
- Guest crossings survived correction rewinds.
- Corrections were fenced by authority sequence.
- Authored scripts could participate in live sessions.
- Dedicated hosts ran without terminals or cursors.
- Mid-run joins received authoritative shared state.
- Session geometry and remote cursors became observable.
- Health probes and structured stdout logs were added.
- Allocated sessions gained first-join and empty timeouts.
- Crossing membership switched from ticks to sequence IDs.
- Simulation layers were clipped to the playable map.
- Roster ceilings and pinned authority were exposed.
- Reachable successor addresses supported migration.
- Correction floods on cyclic meshes were terminated.
- Named sessions connected runtime identity to deployment.
- Session IDs entered structured log records.
- Wind became a deterministic environment effect.
- Decay and blossom collapsed into unified particles.
- Particle RNG and wall destruction behavior were unified.
- Splash timers were anchored to simulation time.
- Storm targeting and rendering were streamlined.
- Peer shields and Ember fields gained dimmed rendering.
- Local player effects were rendered last.
- Guest color-mode options stayed local during joins.

## 2026-09 — K3s fleet and production operations

- K3s feasibility and dedicated-server gaps were documented.
- Per-session Jobs and Services defined the fleet model.
- Session lifetime cleanup was wired into deployment.
- Join identity checks and manual scenarios were added.
- A session allocator exposed bounded game allocation.
- Deployment probes verified readiness and endpoints.
- Node-local logging replaced direct pod-log dependence.
- File-backed session logs prepared durable replay.
- RBAC was reduced around explicit log access.
- LogWisp became the standalone fleet log collector.
- Fan-in, outage replay, and maintenance gates were tested.
- Allocator log proxying exposed session streams.
- LogWisp revisions were pinned to reachable commits.
- Queue sizing was aligned with fleet rate limits.
- The public edge exposed allocation and log routes.
- The log viewer moved into its own served asset.
- Fleet clearance became a single operational command.
- Convergence logic moved out of the app composition root.
- Corrections adopted the same playout discipline as crossings.
- RNG streams were scoped by replication domain.
- Predicted kills were settled exactly once.
- Playout lead became link-specific and measured.
- Crossings applied in lockstep on their producer.
- Late corrections projected forward to the live tick.
- Headless and WASM build capabilities were separated.
- Nightly artifacts added native, browser, and server builds.
- The headless container became the fleet runtime image.
- WebSocket browser-session routing became the deployment target.

## 2026-09 — Scenarios, browser sessions, and the vif name

- Special attacks, three-state auto-fire, and heat-driven drains were reworked.
- The loaded map became a content-identified scenario that `:n` could change mid-session.
- Joiners that lacked a scenario received it over net.
- The corpus became player domain, resolved on each instance and never reconciled.
- `script/test.sh` replaced the phase scripts as the test harness.
- The fleet served scenarios from a node volume replaceable while live.
- Releases carried the wad as one archive, the config root a player extracts.
- A WebSocket bridge with `websocat` sidecar opened the first browser route.
- `web/` became the launcher the deployment serves.
- A node deploy became one command, `deploy/update.sh`, which `--diff` previews.
- A running game could `:join` a session.
- The browser build moved its Go program into a Worker.
- Every crossing was committed at the authority, and guests were paced against it.
- Each link became one compressed stream.
- Corrections were installed at the tick they describe.
- A refused link dropped one participant rather than the session.
- Kill credit went to the engaged set or the cursor that landed the killing hit.
- Pointer play crossed one placement per tick, and APM counted gestures and travel.
- Music followed APM through seeded, embedded pattern groups.
- 256-color text-console render tier expanded with console-safe glyphs.
- Cursor weapons moved into one spec table.
- Turret and beam weapons arrived, mountable on shared hosts.
- Packaging added FreeBSD and cross-OS installs, a manual, completions, and desktop files.
- Stable releases were drafted from version tags.
- The repository, module, and fleet were renamed vif (formerly `vi-fighter`).
- The debug overlay became telemetry cards, and `:debug` gained profiling.

## 2026-09 to 2026-10 — Scaling, WebSocket, and bots

- Guest journals replayed from their join.
- The journal recorded each written world as the change the write made.
- `vif.toml` supplied flag defaults, and `-mw` recorded the music as WAV.
- `-host` started play at once, and session addresses shared one endpoint grammar.
- A scaling audit narrowed and baselined correction bodies and paced them per link.
- Participants behind a relay converged as a stopgap.
- UDP was dropped in favor of native `wss://` joins.
- The fleet routed sessions by name on one address.
- `pkg/websocket` brought a standard-library RFC 6455 stream.
- vif-allocator terminated the browser route, and the `websocat` sidecar was removed.
- vif could serve WebSocket sessions itself.
- `-join https://<site>` asked a site for a new session.
- Join and creation budgets were keyed on the player's address.
- Hidden browser tabs stopped drawing.
- FSM-graph bots could take a participant seat.
- `vif -bot` and `:bot` seated bots in the run's session.
- Named actions and ex commands resolved in one input layer.
- An in-game configuration menu (`:config`) arrived with live settings.
- Cursors were marked by session slot, and roam and patrol merged into `default`.
- Bots carried their holder's identity, and `:player drop` removed a player with its bots.
- A guest's bots joined and played through the guest's own link.
- Bot graphs reacted to events, evaded threats, and were weighed by fitness.
- `-check` validated bot graphs.
- Fleet sessions could request bots and ended after one hour.

## 2026-10 — Kraken, replay navigation, and refresh

- Kraken arrived as a looping composite species with wall-destroying charges.
- Storm escalation ran through a two-Kraken maze before Tower.
- Scenario progression, pursuit, and encounter persistence were fixed.
- Health bars were drawn over species bodies.
- Knockback immunity was shared across weapons and players.
- The species behavior machine design was written up.
- Journals could start mid-game.
- Host and guest journals replayed tick-exactly.
- Replay gained single-tick step back, seek by tick or time, and restart.
- Checkpoint rings and journaled world digests verified every restored replay copy.
- Replay stepping moved off the frame loop.
- Session state was journaled, so replays reproduced session changes.
- Logging moved to one handle per runtime with per-layer scopes.
- The beam weapon became the ray.
- Drop tiers moved into profiles, and all cursor contact took one strike path.
- Corrections resumed across session resets.
- Overlapping journal files installed each written world once.
- The toolchain moved to Go 1.27.2 with refreshed dependencies.
- Terminal v0.2's themed TUI arrived, and the text-console tier was redrawn on it.
- Network failures named the player's next step.
- A startup `-host` that cannot bind failed before the UI.
- The status bar gained a fixed key cell and ranked messages that stay up to be read.

## 2026-10 — Weapons, macros, timers, and the input hook

- TOML macros expanded scenarios and bot graphs at load.
- Combat statuses moved into one slot array per target.
- Each orb weapon gained a charge-cap effect.
- The cleaner became the gun, an innate tracer weapon.
- The genetic archive rejected duplicates and scored its diversity.
- System roots, a checkout's own wad, and user installs settled the install layout.
- Corrections, td ticks, and replay were made cheaper at the map limit.
- A defeat started a new game.
- Dust folded into particles, and decay orbited the Kraken.
- Timers became fixed slots aged first in each tick.
- Each tick settled what its systems queued before it closed.
- Glyph drift moved loose text in parallax layers.
- Scenarios gained an input hook, system selection, and seeded variables; simon was built on them.
- Component logic moved into the owning systems.
- Standing breakpoints joined the debug command.
- Flow fields were computed only when read, sharing one grid search with route graphs.
- Busy links waited instead of dropping what they were owed.
- Release archives and the browser launcher became deployable as they stand.

## 2026-10 — The host model: window, Android, and browser

- `render.Surface` named what the orchestrator needs from a terminal.
- Live store reads and reused navigation scratch cut td's per-tick garbage.
- `app.Presentation` made the terminal one host among others.
- A bitmap font atlas and a software raster became the reference drawer of a graphical host.
- The cell aspect became one simulation parameter.
- A wall-grid epoch, a page-table store index, and pooled spawn payloads halved td's tick.
- `make gui` drew the game in a desktop window through EGL and GLES 2, cgo off.
- An Android host played the game on a phone through `gomobile bind`.
- The configuration menu was summoned by a held press on every host.
- The cell-frame protocol moved into `pkg/glyph` for every host drawing elsewhere.
- The browser page drew cell frames on a WebGL canvas, and xterm.js was retired.
- Without WebGL the page painted on a 2D canvas.
- Navigation was measured and taken apart in five steps — one search grid per tier with the snake locking by one reach, walls diffed once a tick and fields repaired where cells open, a private group per cursor that loot reads, route fields over their corridors alone — and td's worst storm tick fell from 132 ms to 30; the navigation design became its own document.
