# vi-fighter

A terminal-based game built with Go that combines vi/vim motion commands with fast-paced typing and rogue-like 2D shooter action gameplay.

Built using Entity-Component-System (ECS) architecture and Go standard library (no dependency).

## Design Summary

World is the source of truth
Entities are identifiers
Components are data
Systems are logic
Synced game and frame
Events run concurrently (MPSC)
Resources are shared data in the world
Data-driven HFSM engine (TOML)
Physics is absolute
Large pops adapt and evolve
Services are bridged as resources
Double-buffered mixed-mode rendering
Direct ANSI I/O
Input translates into intent

## License

BSD-3 Clause