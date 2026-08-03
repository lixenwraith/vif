# soundlab

`soundlab` is the terminal-based audio authoring environment and sequencer for `vif`. It runs entirely on the `pkg/audio` engine and provides a reflective, command-driven interface over the TOML audio specifications.

Supports interactive REPL, TUI, and headless script execution.

## Architecture & Concepts

- **Document vs Registry**: Edits mutate the working document in memory. The audio engine operates on the live registry. `apply` pushes document state to the engine (enabling auditioning); `revert` overwrites the document with the canonical registry state.
- **Dotted Path Addressing**: The command grammar interacts directly with Go reflection over `toml` tags. Paths like `blip.layer.0.source.freq` are type-checked and routed to exactly what the TOML deserializer sees.
- **Zero-Value Masking**: Pointer chains and zero values indicate "unset" or "package default", preserving minimal explicit TOML generation via `omitempty`.
- **Validation Bounds**: The REPL bounds checks are purely structural (type width, float finiteness). Domain-specific validation (Nyquist bounds, envelope durations) happens synchronously at `validate` and `apply`.

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
- `-tui`: Full-screen Terminal User Interface.
- `-headless`: Discard audio output (`-ab null`).
- `-s <file>`: Execute a `.slab` script file line-by-line and exit.
- `-snd / -pat <file>`: Preload TOML document definitions.
- `-ab <name>`: Force audio backend (`pacat`, `aplay`, `null`, `wav:out.wav`).

## TUI Modalities

The TUI is a shell over the exact same dispatch table used by the REPL and scripts. All macros strictly construct and execute standard command strings to maintain 1:1 script parity.

- **Global**: `:` enters Command mode (REPL input). `^S` saves modifications. `p` enters Piano mode.
- **Browser (Left Pane)**: `j/k` navigate. `h/l` toggles Sounds vs Patterns. `space` auditions. `0-2` assigns to a sequencer slot. `d` deletes. `n/c/w` new/clone/write. `+` mix/merge.
- **Inspector (Right Pane)**: `Enter` to edit leaf values. `a` adds elements to slices. `x` deletes slice elements. `L/H` expand/collapse all.
- **Piano Mode (`p`)**: Tracker-style two-octave layout. `z`/`q` rows map to white keys, `s`/`2` rows map to black keys. `Tab` cycles instrument. `^S` quantizes a recording to the 16th grid and generates a Pattern.
- **Beat Mode (`b` on patterns)**: Track/step grid macro editor. `space` toggles hits. `+/-` nudges velocity. `Enter` auto-applies and slots the pattern to the sequencer transport.

## Command Reference

| Command | Usage | Description |
|---|---|---|
| `load`/`save` | `save sound <file>` | Replace/write working set from/to TOML. |
| `export` | `export <name> <out.wav>` | Render definition into a PCM WAV. |
| `show` | `show <name.path>` | Dump TOML shape, or print leaf value. |
| `new`/`del` | `new sound <name>` | Seed or discard an entry. `new` seeds a minimal valid spec. |
| `set` | `set <name.path> <val>` | Mutate leaf field via reflection. |
| `add` | `add <name.path>` | Append a zero-element to a slice field. |
| `validate` | `validate [name]` | Run domain validations on spec. |
| `apply` | `apply [name]` | Push valid document into live registry. |
| `play`/`note` | `play <name>` | Audition. `note` runs through the melodic sequencer slot. |
| `slot` | `slot <0\|1\|2> <pat>` | Transport assignment. |

## Scripting & Headless Demo

Scripts drive the engine identically to the REPL. Prepending a `-` tolerates failure; `!` asserts failure.

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
