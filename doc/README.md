# Vi-Fighter Engineering Documentation

This directory describes the architecture and design of the current Vi-Fighter
codebase. It was audited against commit
`f725c940abe5bcca921ed9f721bc33924ad91a4c` on 2026-08-03. The implementation,
generated manifest, and shipped configuration were treated as authoritative
where older prose disagreed with the code.

Vi-Fighter is a terminal action game that combines vi-style text navigation,
typing, shooting, data-driven encounters, adaptive enemy navigation, procedural
audio, and a custom ECS. The documentation separates those concerns so that a
reader can start with the application shape and then descend into a subsystem.

## Reading guide

| Document | Level | Primary questions answered |
|---|---|---|
| [Architecture overview](architecture.md) | High | What are the major parts, boundaries, and design constraints? |
| [Package map](package-map.md) | Medium | Which Go packages own each responsibility and how may they depend on one another? |
| [Runtime and concurrency](runtime.md) | Medium/detail | How does the process start, tick, render, pause, reset, and shut down safely? |
| [ECS and events](ecs-and-events.md) | Medium/detail | How are entities stored, systems ordered, spatial queries performed, and events settled? |
| [Gameplay systems](gameplay.md) | Domain detail | What are the player mechanics, world mechanics, enemies, encounters, and system responsibilities? |
| [Input and modes](input-and-modes.md) | Domain detail | How do terminal events become vi commands, gameplay intents, macros, mouse actions, and commands? |
| [HFSM and configuration](fsm-and-configuration.md) | Domain detail | How are parallel regions, hierarchical transitions, actions, guards, and shipped scenarios composed? |
| [Rendering](rendering.md) | Domain detail | How are ECS state, compositing, color modes, masks, post-processing, and terminal output connected? |
| [Audio](audio.md) | Domain detail | How are effects synthesized, music sequenced, APM mapped to arrangements, and backends selected? |
| [AI, navigation, physics, and evolution](ai-physics-and-evolution.md) | Domain detail | How do flow fields, route learning, genetics, fixed-point math, and collision/steering work together? |
| [Content, assets, and tools](content-assets-and-tools.md) | Domain detail | How are corpora and embedded assets resolved, parsed, validated, and authored? |
| [Services and networking](services-and-networking.md) | Domain detail | How are I/O resources managed, and what networking code is implemented versus actually wired? |
| [Development and operations](development.md) | Operational detail | How is the project built, generated, tested, diagnosed, and deployed on native and WASM targets? |

Existing focused references remain useful:

- [FSM authoring reference](../config/README.md) documents the TOML surface in
  detail.
- [Content corpus reference](../data/README.md) describes `.txt` and authored
  `.toml` corpora.
- [Keymap example](../internal/input/README.md) shows sparse key overrides.
- [Genetic package reference](../pkg/genetic/README.md) documents the reusable
  optimization library.
- [Soundlab reference](../cmd/soundlab/README.md) covers the audio authoring
  environment.
- [Ascimage reference](../cmd/ascimage/README.md) covers terminal-image
  conversion and viewing.

## Source-of-truth map

The repository uses generated registries and data-driven configuration. When
changing a subsystem, update the source that actually owns its shape.

| Concern | Authoritative source | Generated or runtime consumer |
|---|---|---|
| Components, systems, renderers | `internal/manifest/definition.go` | `internal/manifest/build_gen.go`, `internal/engine/component_store_gen.go` |
| Event names and payload association | `internal/event/type.go` comments and constants | `internal/event/registry_gen.go` |
| Input enum string forms | input enum definitions | `internal/input/strings_gen.go` |
| Default encounter progression | `internal/asset/config/*.toml` | `internal/fsm`, `internal/engine.ClockScheduler` |
| Alternate scenarios | `config/blank`, `config/main`, `config/td` | selected with `-g` |
| Gameplay tuning | `internal/parameter` and `internal/parameter/visual` | systems and renderers |
| Embedded fallback corpus | `internal/asset/content/*.toml` | content service |
| External package versions | `go.mod` | Go module resolver |

Run `make generate` after changing a manifest or generator input. Do not edit a
generated file by hand.

## Accuracy conventions

This documentation distinguishes three states:

- **Active** means constructed and used by the normal `cmd/vif` runtime.
- **Optional** means supported by an explicit flag, config, build tag, or tool.
- **Experimental/incomplete** means code exists but the normal application does
  not expose a complete end-to-end feature. Networking is the principal example.

Performance statements are intentionally scoped. Several hot paths reuse
buffers and avoid routine allocation, but the application is not globally
allocation-free. Gameplay kinetics rely heavily on Q32.32 fixed-point values,
but `vmath` also uses hardware floating-point operations and supplies native
float vector paths; the whole simulation should therefore not be described as
bit-for-bit deterministic across all platforms.
