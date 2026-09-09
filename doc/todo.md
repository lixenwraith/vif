# TODO

Scratch note that travels with the repository. One line per item: what, and the
smallest thing that would close it. Delete an entry when it lands — this file is
not a changelog.

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
| P2 | `wad/game` and `internal/asset/config` are near-duplicates that drift by hand (the embedded bundle omits `tower`, adds `placeholder`). Decide whether the embedded fallback is a trimmed copy on purpose, and if so state what it must always contain. |
| P3 | `wad/games/` is packaging structure with no discovery. A `-g <name>` shorthand resolving inside `games/` would make the alternates reachable without a path. |

## Audio

| P | Item |
|---|---|
| P1 | `AudioEngine.SpecError()` has no in-game surface: a malformed user `sounds.toml` silently degrades to the shipped bank during play. `-check` reports it; the running game does not. |
| P2 | `soundlab` seeds patterns from the live registry because the built-ins are Go literals in `pkg/audio`, not data. Moving them to `internal/asset/audio/music.toml` would make patterns editable the way sounds already are. |

## Testing

| P | Item |
|---|---|
| P2 | `test/scenario.sh` binds real ports, so it cannot run in a packaging chroot or in CI alongside `go test`. Split the port-binding scenarios from the ones that do not need a socket. |
