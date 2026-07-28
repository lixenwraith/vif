# vi-fighter

A terminal-based game built with Go that combines vi/vim motion commands with fast-paced typing and rogue-like 2D shooter action gameplay.

Built from scratch using the Go standard library, featuring a custom game engine with zero CGO dependencies.

## Features

*   **Custom ECS Engine:** High-performance Entity-Component-System architecture with zero-allocation hot paths.
*   **Terminal Rendering:** Double-buffered ANSI renderer with TrueColor/256-color support and sub-pixel Unicode manipulation.
*   **Deterministic Physics:** Q32.32 fixed-point mathematics driving 2D and 3D kinetic interactions, flocking, and collisions.
*   **Adaptive AI:** Enemies evolve via Genetic Algorithms and utilize Flow Field pathing with EXP3 multi-armed bandit route selection.
*   **Procedural Audio:** Pure Go PCM synthesizer featuring dynamic, APM-driven background music and real-time sound effect mixing.
*   **Data-Driven Logic:** Game phases and states controlled by a custom Hierarchical Finite State Machine (HFSM) defined in TOML.
*   **Vi/Vim Emulation:** Accurate input parsing for normal, insert, visual, and command modes, including macros and motion operators.
*   **Mouse Support:** Mixed keyboard and mouse, keyboard-only, mouse-only (free and click-based) gameplay.
*   **Platform:** Linux, FreeBSD, and WASM (limited to embedded data file usage).

## Build and run

```bash
git clone https://github.com/lixenwraith/vi-fighter --depth 1
cd vi-fighter
make release
bin/vi-fighter
```

## License

BSD-3 Clause

