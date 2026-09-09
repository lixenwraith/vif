# TODO

Scratch note that travels with the repository. One line per item: what, and the
smallest thing that would close it. Delete an entry when it lands — this file is
not a changelog. Source files do not carry parallel TODO comments; a code-originated
item names its source here.

Priority is P0 blocks a release, P1 wanted next, P2 when convenient, P3 idea.

## Packaging

Detail and per-distribution steps are in [Packaging](packaging.md).

| P | Item |
|---|---|
| P0 | Tag a `v0.1.0` release with a published source tarball and checksum. |
| P1 | Man page `vif.1` generated from the flag table, installed by `make install`. |
| P2 | `.desktop` entry with `Terminal=true`, and an icon. |
| P2 | Shell completion for `vif`. |
| P3 | `vi-fighter-git` AUR package alongside the release one. |

## Assets and configuration

| P | Item |
|---|---|
| P1 | `WallPatternSpawnRequest.path` resolves against the process working directory, so an installed `image/` tree cannot be named portably from a config. Decide whether it resolves against config roots, then document `image/` as a real category. |
| P2 | `.vifimg` carries an author anchor (`-ax`/`-ay`) that `PatternResult` loads and nothing reads; the spawn event supplies the coordinate instead. Either consume it as the default anchor or drop it from the format. |

## Audio

| P | Item | Source | Prerequisite |
|---|---|---|---|
| P1 | Surface a malformed user `sounds.toml` during play; fallback currently succeeds silently while `-check` reports the error. | `internal/service/adapter_audio.go` | Choose the in-game surface, then expose the latched `AudioEngine.SpecError()` without making fallback fatal. |
| P2 | Move built-in music patterns from Go literals to `internal/asset/audio/music.toml`, so `soundlab` can edit the same data the game loads. | `cmd/soundlab/session.go`, `pkg/audio` | Define and validate the pattern document before replacing registry seeding. |
| P3 | Make the 50 ms mixer buffer adjustable instead of a compile-time constant. | `pkg/audio/params.go` | Define a construction-time option and recompute dependent buffer sizes before either mixer or backend starts. |
| P3 | Decide whether manual intensity decreases should use per-bar track reveal; manual changes currently always reveal while automatic changes reveal only when rising. | `internal/system/music.go` | Add deterministic rising/falling transition coverage around `applyArrangement` before changing the policy. |

## Runtime structure

| P | Item | Source | Prerequisite |
|---|---|---|---|
| P2 | Refactor drain population reconciliation: target count, pending materialization, stagger timing, and failed-placement backoff currently share one update block. | `internal/system/drain.go` | Pin spawn, pause/resume, materialize-completion, and exponential-backoff behavior with focused deterministic tests. |
| P3 | Fold context-scoped systems into the main manifest system list. `MetaSystem` is still constructed separately because it needs `GameContext`, not only `World`. | `internal/manifest/definition.go`, `internal/app/app.go` | Give generated construction a capability/context input without weakening dependency and snapshot-profile checks. |

## Testing

| P | Item | Source | Prerequisite |
|---|---|---|---|
| P2 | Cover wall displacement classification for every species, including kinetic headers, non-physical composite anchors, particles, and ordinary spawn blockers. | `internal/system/wall.go` | Write down the expected header/member mask for each species, then encode it as a table-driven fixture. |
| P2 | Split the port-binding scenarios from checks that can run in a packaging chroot or alongside `go test`. | `test/scenario.sh` | Classify the scenarios by socket and process requirements. |
