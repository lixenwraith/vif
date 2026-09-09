# TODO

Scratch notes that travel with the repository. Each item states what remains and
the smallest thing that would close it. Delete an item when it lands; this file
is not a changelog. Source files do not carry parallel TODO comments, so a
code-originated item names its source here.

Priorities: P0 blocks a release, P1 is wanted next, P2 is convenient follow-up,
and P3 is an idea.

## Packaging

Distribution-specific detail lives in [Packaging](packaging.md).

### Publish the first release

- Priority: P0
- Affected files: release artifacts and `doc/packaging.md`
- Prerequisite: choose a release commit after the complete verification gate
  passes

Tag `v0.1.0` and publish a byte-stable source tarball with its checksum.

### Install a manual page

- Priority: P1
- Affected files: `cmd/vif/usage.go`, `doc/vif.1`, `Makefile`
- Prerequisite: keep the flag table as the source of truth

Generate `vif.1` from the flag table and install it through `make install`.

### Add a desktop launcher

- Priority: P2
- Affected files: packaging assets and `Makefile`
- Prerequisite: select an installable icon

Add a `.desktop` entry with `Terminal=true` and install its icon.

### Add shell completion

- Priority: P2
- Affected files: `cmd/vif/usage.go`, completion assets, `Makefile`
- Prerequisite: choose generated or maintained completion definitions

Provide shell completion for `vif` and install it in the appropriate data path.

### Publish a development AUR package

- Priority: P3
- Affected files: AUR packaging metadata and `doc/packaging.md`
- Prerequisite: establish the release-package metadata first

Publish `vi-fighter-git` alongside the release-based AUR package.

## Audio

### Surface malformed user sound configuration during play

- Priority: P1
- Affected files: `internal/service/adapter_audio.go`
- Prerequisite: choose the in-game error surface

Fallback currently succeeds silently during play while `-check` reports the
error. Expose the latched `AudioEngine.SpecError()` without making fallback
fatal.

### Move built-in music patterns into an editable asset

- Priority: P2
- Affected files: `cmd/soundlab/session.go`, `pkg/audio`,
  `internal/asset/audio/music.toml`
- Prerequisite: define and validate the pattern document

Replace the built-in Go registry literals with `internal/asset/audio/music.toml`
so `soundlab` edits the same data the game loads.

### Make the mixer buffer configurable

- Priority: P3
- Affected files: `pkg/audio/params.go`
- Prerequisite: define a construction-time option and recompute dependent buffer
  sizes before the mixer or backend starts

Replace the compile-time 50 ms mixer buffer with a construction-time setting.

### Decide reveal behavior for manual intensity decreases

- Priority: P3
- Affected files: `internal/system/music.go`
- Prerequisite: deterministic rising and falling transition coverage around
  `applyArrangement`

Manual intensity changes always use per-bar track reveal, while automatic
changes reveal only when intensity rises. Decide whether manual decreases should
retain that distinction.

## Runtime structure

### Separate drain population reconciliation concerns

- Priority: P2
- Affected files: `internal/system/drain.go`
- Prerequisite: focused deterministic coverage for spawn, pause/resume,
  materialization completion, and exponential backoff

Refactor the update block that currently combines target count, pending
materialization, stagger timing, and failed-placement backoff.
