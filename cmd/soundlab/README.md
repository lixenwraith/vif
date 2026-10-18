# soundlab

`soundlab` is the terminal-based audio authoring environment and sequencer for `vif`. It runs entirely on the `pkg/audio` engine and provides a reflective, command-driven interface over the TOML audio specifications.

Supports interactive REPL, TUI, and headless script execution.

## Architecture & Concepts

- **Document vs Registry**: Edits mutate the working document in memory. The audio engine operates on the live registry. `apply` pushes document state to the engine (enabling auditioning); `revert` overwrites the document with the canonical registry state.
- **Dotted Path Addressing**: The command grammar interacts directly with Go reflection over `toml` tags. Paths like `blip.layer.0.source.freq` are type-checked and routed to exactly what the TOML deserializer sees.
- **Zero-Value Masking**: Pointer chains and zero values indicate "unset" or "package default", preserving minimal explicit TOML generation via `omitempty`.
- **Validation Bounds**: The REPL bounds checks are purely structural (type width, float finiteness). Domain-specific validation (Nyquist bounds, envelope durations) happens synchronously at `validate` and `apply`.
- **Slots**: 0 rhythm, 1 melody, 2 fill, 3 bass, as the game draws them; a melody pattern no longer carries its own bass, so slot 1 alone is the melody as authored. The engine's phrase auto-fill is off in soundlab so slots stay deterministic; `fill on` restores it to audition the fill bank.
- **Audition**: `play`, `hit` and `export` render a spec as authored; vif's per-sound volume and shape table is not applied.

## Usage

```bash
# TUI mode
go run ./cmd/soundlab -tui

# REPL mode
go run ./cmd/soundlab

# Headless batch processing
go run ./cmd/soundlab -headless -s script.slab
```

### CLI Flags
- `-tui`: Full-screen Terminal User Interface. Empty documents are seeded from the built-ins; the REPL and scripts leave that to `builtin`.
- `-headless`: Discard audio output (`-ab null`).
- `-s <file>`: Execute a `.slab` script file line-by-line and exit.
- `-config-dir <dir>`, `-config-music <file>`, `-config-sounds <file>`: vif's flags, resolved the same way, with `vif.toml`'s `[paths] root` and `[audio] buffer_ms` read as vif reads them. The overrides vif would load are registered and opened as the working documents; `save` writes where the next vif run reads first.
- `-ab <name>`: Force audio backend (`pacat`, `aplay`, `null`, `wav:out.wav`).
- `-vol <f>`: Master volume 0..1 (default 0.7).

## TUI Modalities

The TUI is a shell over the exact same dispatch table used by the REPL and scripts. Its macros construct and execute standard command strings, so a script can reproduce them; only the piano's recording commit writes its pattern into the document directly.

- **Global**: `:` enters Command mode (REPL input, `Up/Down` history). `Tab` cycles focus between browser, inspector and log. `p` enters Piano mode, starting the transport if it is stopped (music must be unmuted); `b` Beat mode on a pattern; `m` resets the transport. `^S` saves modified documents (prompting for a path when one has none). `^Q`/`^C` quits, warning once about unsaved changes.
- **Browser (Left Pane)**: `j/k` navigate, `PgUp/PgDn` a page, `g/G` jump to ends, `/` finds the next name containing a term (`/` and Enter again walk the matches). `h/l` (or arrows) toggles Sounds vs Patterns. `Enter` opens the inspector. `space` auditions; on a pattern it assigns a free slot or toggles the transport. `0-3` assigns to (or clears from) a sequencer slot. `a/v/r` apply/validate/revert. `d` twice deletes. `n/c/w` new/clone/write. `+` mix.
- **Inspector (Right Pane)**: `j/k` navigate, `g/G` ends. `l/h` expand/collapse, `L/H` all. `Enter` edits a leaf value. `a` adds elements to slices. `x` deletes slice elements. `space` auditions. `Esc` returns to the browser.
- **Log**: `j/k` and `PgUp/PgDn` scroll, `g/G` top/bottom.
- **Piano Mode (`p`)**: Tracker-style two-octave layout. `z`/`q` rows map to white keys, `s`/`2` rows map to black keys. `[`/`]` shift the octave, `Up/Down` the velocity. `Tab` cycles the tonal instruments (bass, piano, pad, lead, pluck). `^S` starts recording and `^S` again commits it, quantized to the 16th grid, as a one-bar pattern `rec_N`. `Esc` exits, committing a take in progress and stopping a transport the piano started.
- **Beat Mode (`b` on patterns)**: Track/step grid macro editor. `h/j/k/l` move, `g/G` first/last step. `space` toggles hits. `+/-` nudge velocity, `1-9` and `0` set it. `t` cycles the track's instrument. `a/x` add/delete a track. `Enter` applies the pattern, puts it in slot 0 and starts the transport. `^S` saves. `Esc` exits.

## Command Reference

`help` prints the full usage of every verb; `help <verb>` describes one.

| Command | Usage | Description |
|---|---|---|
| `load`/`merge` | `load sound\|pattern <file>` | Replace the document from a file, or overlay a file onto it by name. |
| `builtin` | `builtin sound\|pattern` | Seed the document from the built-ins. |
| `save`/`write` | `save sound\|pattern [file]` | Write the document (bare = provenance path); `write <name> <file>` saves one entry. |
| `export` | `export <name> <out.wav> [variant]` | Render definition into a PCM WAV. |
| `ls`/`show` | `show <name>[.path]` | List entries (`*` marks dirty); dump TOML shape, or print leaf value. |
| `where`/`stat` | `stat` | Transport position and slot patterns; backend, play stats, dirty entries. |
| `new`/`cp`/`mv`/`del` | `new sound\|pattern <name>` | Seed, duplicate, rename or discard an entry. `new` seeds a minimal valid spec; `del <name.path.idx>` removes a list element, and a discarded entry keeps any applied registry copy. |
| `mix` | `mix <src> <dst>` | Append src's tracks/layers into dst. |
| `set`/`unset` | `set <name.path> <val>` | Mutate a leaf field via reflection, or reset it to its zero value. |
| `add` | `add <name.path>` | Append a zero-element to a slice field. |
| `validate`/`apply`/`revert` | `apply [name\|all]` | Run domain validations, push into the live registry, or restore from it; bare or `all` = every entry for `validate`, dirty entries for `apply`/`revert`. |
| `play`/`hit`/`note` | `play <name> [vol]` | Audition. `hit` plays a drum; `note` runs through the melodic sequencer slot. |
| `slot` | `slot <0\|1\|2\|3> <pat\|-> [fade_ms] [q]` | Transport assignment; dirty patterns auto-apply. |
| `music`/`bpm`/`swing`/`fill`/`key` | `music start\|stop\|reset` | Transport, tempo, shuffle, slot-2 auto-fill, harmony. |
| `mute`/`vol`/`wait` | `mute music\|sfx\|all\|none` | Channel mute, master volume, scripted pause. |
| `help`/`quit` | `help [verb]` | Usage, exit. |

## Scripting & Headless Demo

Scripts drive the engine identically to the REPL. `#` starts a comment. Prepending a `-` tolerates failure; `!` asserts failure. Any other failure aborts the script with its line number.

**Note:** `new sound` immediately seeds a minimal valid struct (1 `osc` layer + 1 `ar` chain proc). The script below demonstrates modifying the pre-seeded defaults, applying them to the engine, and rendering directly to a `.wav`.

```shell
# Create blip.slab and run:
#   go run ./cmd/soundlab -headless -s blip.slab

new sound blip
set blip.duration 0.2

# Mutate the pre-seeded layer 0
set blip.layer.0.source.kind osc
set blip.layer.0.source.freq 880
set blip.layer.0.chain.0.kind ar
set blip.layer.0.chain.0.release 0.05

# Push to engine and write to disk
validate blip
apply blip
play blip
export blip blip.wav
save sound blip_sound.toml
stat
quit
```
