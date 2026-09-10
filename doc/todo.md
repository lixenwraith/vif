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

## Multiplayer

Diagnoses and what each item follows from are in
[Troubleshooting](troubleshooting.md).

### Split "compared" from "carried" in the status surface

- Priority: P1
- Affected files: `internal/snapshot/surface.go`, `internal/app/capture.go`
- Prerequisite: name every excluded cell an FSM guard or system reads

`SharedKey` decides both what two instances compare and what a capture carries,
so a mixed-domain cell is dropped from the correction as well. `meta` carries
`kills.*` and `energy.damage_multiplier` around it; a second predicate removes the
workaround.

### Audit what a pruned crossing loses

- Priority: P1
- Affected files: `internal/system/network.go`, `internal/app/barrier.go`
- Prerequisite: the split above, so a counter can be carried instead of replayed

An authority frame the capture's fence already claims is discarded on the
receiver. That is correct for shared component state and wrong for every other
effect the frame would have had. Enumerate them.

### Decide whether kinetic immunity is per attacker

- Priority: P2
- Affected files: `internal/system/combat.go`, `internal/component/combat.go`
- Prerequisite: a two-participant repro of a swarm that stops steering

Damage immunity is now budgeted per attacker; kinetic immunity is still one
window per target, and it suppresses homing as well as knockback. Two impulses on
one body is a physics decision, not a networking one. Stun is deliberately not
per attacker: a running one is refused rather than refreshed, which is what a
lockdown window should be.

### Give a splash anchor a generation

- Priority: P2
- Affected files: `internal/system/splash.go`, `internal/component/splash.go`
- Prerequisite: decide whether shared ids may be re-issued at all

A capture restores `NextEntity`, so an install that rolls the allocator back
re-issues shared ids. A timer splash whose anchor died can find a different
composite under the same id and keep counting.

### Confirm the storm skip

- Priority: P2
- Affected files: `wad/game/main/storm.toml`, `internal/system/storm.go`
- Prerequisite: a run that reaches three quasar kills with a crowded map centre

The spawn retry and the carried kill counters address both candidate mechanisms
without either being confirmed. `storm.spawn_failures` and a `StormSetupRetry` in
`fsm.storm` tell them apart. `wad/game/td/td_storm.toml` still waits blind.

### Prove or rule out a stale gold surviving a correction

- Priority: P1
- Affected files: `internal/app/correction_selective.go`, `internal/snapshot/manifest.go`
- Prerequisite: a two-instance repro that leaves the guest holding two sequences

A guest was seen holding a gold the host had destroyed, and once two at a time.
Every destruction path clears the carrier and destroys the composite, and
`ReconcileSharedWorld` drops shared entities the capture does not name, so the
remaining candidate is a selective repair whose page reconstruction keeps an
entity only the receiver holds.

### Let a scripted participant survive a tick jump

- Priority: P2
- Affected files: `internal/journal/script.go`
- Prerequisite: decide whether a replay must still refuse the same overshoot

`ScriptDriver.applyCurrent` fails the run when the world tick has passed an
action's target tick. A correction moves the world tick, so a scripted guest in a
live session ends itself for a reason the session is entitled to. A replay has no
corrections and should keep refusing it.

### Keep shared FSM guards off owner-authored keys

- Priority: P3
- Affected files: `wad/game/main/monitor.toml`
- Prerequisite: a replicated liveness signal to replace the slot mirror

`MonitorWarmup` guards on `player.0.heat.current` and `player.0.energy.current`.
Both are owner-authored, so a receiver never writes them and no capture carries
them. The state is unreachable today, which is why it is a latent hole.

## Runtime structure

### Separate drain population reconciliation concerns

- Priority: P2
- Affected files: `internal/system/drain.go`
- Prerequisite: focused deterministic coverage for spawn, pause/resume,
  materialization completion, and exponential backoff

Refactor the update block that currently combines target count, pending
materialization, stagger timing, and failed-placement backoff.
