# Vi-Fighter Architecture Overview

Vi-Fighter is a terminal-based action-typing game built on a customized, performance-oriented engine entirely in Go (standard library). The architecture enforces separation of concerns, deterministic execution, and zero-allocation hot paths to maintain high frame rates in standard terminal environments.

## 1. Core Philosophy

The engine is built around a data-oriented design. Memory layout, state transitions, and logic execution are heavily decoupled.
*   **Zero-Allocation Hot Paths:** Rendering, physics integration, and core ECS updates do not allocate on the heap.
*   **Determinism:** Game logic utilizes fixed-point math and seeded random number generators to ensure predictable mechanics across varying hardware.
*   **Data-Driven:** Major game flows, enemy patterns, and user interfaces are driven by external TOML configurations rather than hardcoded logic.

## 2. Entity Component System (ECS)

The core engine utilizes a custom ECS to manage all game objects and logic.
*   **Entities:** Represented as simple 64-bit integer IDs.
*   **Components:** Pure data structures stored in contiguous, type-safe arrays (Sparse Set pattern). This layout guarantees cache-friendly iteration.
*   **Systems:** Encapsulated logic modules executed in a strictly defined priority order during the main game loop.
*   **Spatial Grid:** A specialized, fixed-capacity 2D grid caches entity positions. It enables O(1) spatial queries, line-of-sight (Bresenham), and area-of-effect calculations without scanning all entities.
*   **Composite Pattern:** Large entities (like bosses or swarms) use a "Header-Member" pattern. An invisible Phantom Head controls logic, physics, and pathfinding, while visually distinct Members sync their relative positions to the head.

## 3. Hierarchical Finite State Machine (HFSM)

Game phases, progression, and high-level enemy behaviors are managed by a custom HFSM.
*   **Data-Driven Transitions:** States, transitions, delays, and guards are defined via TOML configuration files.
*   **Parallel Regions:** The HFSM can execute multiple independent state machines (regions) concurrently, allowing background systems (like dynamic ambient environments) to run alongside the main gameplay loops.
*   **Payload Injection:** The machine intercepts standard game events and can conditionally transition states based on the event's data payload.

## 4. Physics and Mathematics

To guarantee cross-platform determinism and avoid floating-point inaccuracies, the engine implements a custom math library.
*   **Fixed-Point Math (Q32.32):** All kinetic logic, velocities, accelerations, and angles use 64-bit fixed-point arithmetic.
*   **2D Kinetics:** Standard entities use 2D acceleration, velocity integration, and boundary reflection. 
*   **3D Kinetics:** Specific complex encounters utilize 3D physics (X, Y, Z coordinates) projected down to 2D screen space, featuring Z-axis gravity, orbital mechanics, and spring-based equilibrium.
*   **Collision and Steering:** The engine supports hard collisions (bouncing off walls/shields), soft collisions (flocking and inter-entity repulsion), and complex homing algorithms with dynamic drag and arrival steering.

## 5. AI, Pathfinding, and Evolution

Enemy navigation and difficulty scaling rely on a combination of flow fields, machine learning, and genetic algorithms.
*   **Flow Fields (Dijkstra):** Instead of individual A-star pathfinding, target groups generate flow fields that wash over the map, allowing massive swarms of entities to navigate around dynamic walls and obstacles efficiently.
*   **Multi-Armed Bandit (EXP3):** Route selection for spawned enemies is managed by an adaptation system. It monitors the success rate (fitness) of entities taking different paths and dynamically shifts spawn probabilities toward more effective routes.
*   **Genetic Algorithms (GA):** Specific enemy species mutate over time. A streaming genetic engine alters parameters (like speed, health, and drag) based on how well past generations performed against the player, forcing the player to constantly adapt.

## 6. Rendering Engine

Terminal output is managed via a custom, double-buffered ANSI renderer.
*   **Color Modes:** Supports dynamic fallback between TrueColor (24-bit) and 256-color palettes, altering rendering techniques (e.g., using specific Unicode half-blocks vs quadrant blocks) based on terminal capabilities.
*   **Layering and Masking:** Rendering is divided by masks (Background, UI, Transient, Composite, Field). This allows selective post-processing like screen-wide grayouts, dimming, or strobe flashes without corrupting UI elements.
*   **Sub-pixel Rendering:** Kinetic entities calculate position in high-precision math, which the renderer translates into sub-cell Unicode block characters to create the illusion of smooth diagonal movement in a rigid terminal grid.

## 7. Procedural Audio Synthesizer

The game ships with a pure Go, zero-CGO audio synthesizer that pipes PCM data directly to system audio daemons (PulseAudio, ALSA, PipeWire, OSS).
*   **Waveform Generation:** Sound effects are generated procedurally using mathematical oscillators (sine, square, saw, noise) and shaped with ADSR envelopes.
*   **Dynamic Conductor:** A music sequencer reacts to the player's Actions-Per-Minute (APM). As the player types faster, the conductor dynamically shifts the BPM and arrangement intensity (adding drums, arpeggios, and melodies).
*   **Real-time Mixing:** Multi-channel audio is mixed in float64 space, passed through a soft-limiter to prevent clipping, and converted to 16-bit PCM on the fly.

## 8. Input and Context Routing

Player input is strictly separated from game logic to support complex vim-style keybindings.
*   **State Machine Parser:** Raw keystrokes enter a parser that accumulates counts, operators, and motions into a semantic "Intent".
*   **Context Router:** The Intent is passed to a mode-aware router (Normal, Insert, Visual, Search, Command). The router safely queries the ECS world (e.g., looking for the next word boundary) and translates the Intent into concrete game events (e.g., move cursor, delete entity, fire weapon).
*   **Macro System:** Players can record and playback sequences of Intents, executed iteratively by the game loop over time.

## 9. Services and Integration Hub

External I/O is abstracted through a Service Hub.
*   **Dependency Injection:** Terminal polling, Audio engines, Network transports, and File/Content loaders are registered as discrete services.
*   **Lifecycle Management:** The Hub ensures services are initialized, started, and cleanly stopped in topological dependency order.
