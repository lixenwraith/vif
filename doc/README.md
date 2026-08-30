# Vi-Fighter Engineering Documentation

This directory describes the architecture and design of the current Vi-Fighter
codebase. It was last audited on 2026-08-30, through the two-participant startup
networking surface. The implementation, generated manifest, and shipped
configuration were treated as authoritative where older prose disagreed with the
code.

Vi-Fighter is a terminal action game that combines vi-style text navigation,
typing, shooting, data-driven encounters, adaptive species navigation, procedural
audio, and a custom ECS. The documentation separates those concerns so that a
reader can start with the application shape and then descend into a subsystem.

## Reading guide

| Document | Level | Primary questions answered |
|---|---|---|
| [Architecture overview](architecture.md) | High | What are the major parts, boundaries, and design constraints? |
| [Package map](package-map.md) | Medium | Which Go packages own each responsibility and how may they depend on one another? |
| [Runtime and concurrency](runtime.md) | Medium/detail | How does the process start, tick, render, pause, reset, and shut down safely? |
| [ECS and events](ecs-and-events.md) | Medium/detail | How are entities stored, systems ordered, spatial queries performed, and events settled? |
| [Logging and diagnostics](logging-and-diagnostics.md) | Medium/detail | How do scopes, telemetry, the replay journal, snapshots, and the flight recorder work? |
| [Multi-instance domain model](domain-design.md) | Medium/detail | How are entities, events, RNG streams and systems split between shared and player domains, who holds authority over what, and what is still missing? |
| [Gameplay systems](gameplay.md) | Domain detail | What are the player mechanics, world mechanics, species, encounters, and system responsibilities? |
| [Input and modes](input-and-modes.md) | Domain detail | How do terminal events become vi commands, gameplay intents, macros, mouse actions, and commands? |
| [HFSM and configuration](fsm-and-configuration.md) | Domain detail | How are parallel regions, hierarchical transitions, actions, guards, and shipped scenarios composed? |
| [Rendering](rendering.md) | Domain detail | How are ECS state, compositing, color modes, masks, post-processing, and terminal output connected? |
| [Audio](audio.md) | Domain detail | How are effects synthesized, music sequenced, APM mapped to arrangements, and backends selected? |
| [AI, navigation, physics, and evolution](ai-physics-and-evolution.md) | Domain detail | How do flow fields, route learning, genetics, float64 geometry, and collision/steering work together? |
| [Content, assets, and tools](content-assets-and-tools.md) | Domain detail | How are corpora and embedded assets resolved, parsed, validated, and authored? |
| [Services and networking](services-and-networking.md) | Domain detail | How are I/O resources managed, and how do startup sessions, framing, polling, and disconnect work? |
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
| System domain profiles and dependencies | `SystemDef.Domain`/`Requires` in `internal/manifest/definition.go` | `manifest.ProfileFor`/`SystemProfiles`, `World.SystemInitOrder`, `app.checkSystems` |
| Event names, payload association, replication class | `internal/event/type.go` comments and constants | `internal/event/registry_gen.go` |
| Runtime shape and deterministic harness | `internal/app/config.go`, `headless.go` | `App`, `ClockScheduler`, services |
| Replay journal format and producer origins | `internal/event/journal.go`, `origin.go` | `internal/journal`, `internal/app/replay.go` |
| Cursor lifecycle, roster, and local selection | `internal/system/cursor.go`, `internal/engine/resource.go` | FSM cursor events, mode routing, per-slot metrics |
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
  not expose a complete end-to-end feature. Mid-run join/reconnect and authenticated
  multiplayer are examples; startup two-participant networking is optional and active.

Performance and determinism statements are intentionally scoped. Several hot
paths reuse buffers and avoid routine allocation, but the application is not
globally allocation-free. A caller-driven App is a pure function of its seed,
config, and injected events for one implementation build. Simulation motion,
geometry, and physics use `float64`, so that is not a cross-platform lockstep
guarantee; live play adds concurrent scheduling and is not the source class for
which bit-exact replay is claimed.
