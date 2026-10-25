# soundlab

`soundlab` is the terminal-based audio authoring environment and sequencer for `vif`. It runs entirely on the `pkg/audio` engine and provides a reflective, command-driven interface over the TOML audio specifications.

Supports interactive REPL, TUI, and headless script execution.

## Architecture & Concepts

- **Document vs Registry**: Edits mutate the working document in memory. The audio engine operates on the live registry. `apply` pushes document state to the engine (enabling auditioning); `revert` overwrites the document with the canonical registry state.
- **Dotted Path Addressing**: The command grammar interacts directly with Go reflection over `toml` tags. Paths like `blip.layer.0.source.freq` are type-checked and routed to exactly what the TOML deserializer sees.
- **Zero-Value Masking**: Pointer chains and zero values indicate "unset" or "package default", preserving minimal explicit TOML generation via `omitempty`.
- **Validation Bounds**: The REPL bounds checks are purely structural (type width, float finiteness). Domain-specific validation (Nyquist bounds, envelope durations) happens synchronously at `validate` and `apply`.
- **Slots**: 0 rhythm, 1 melody, 2 fill, 3 bass, as the game draws them; a melody pattern carries no bass, so slot 1 alone is the melody as authored. The engine's phrase auto-fill is off in soundlab so slots stay deterministic; `fill on` restores it to audition the fill bank.
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
- `-tui`, `-T`: Full-screen Terminal User Interface. Empty documents are seeded from the built-ins; the REPL and scripts leave that to `builtin`.
- `-headless`: Discard audio output (`-ab null`).
- `-s <file>`: Execute a `.slab` script file line-by-line and exit.
- `-config-dir <dir>`, `-config-music <file>`, `-config-sounds <file>`: vif's flags, resolved the same way, with `vif.toml`'s `[paths] root` and `[audio] buffer_ms` read as vif reads them; any key this build does not know is named and skipped. The overrides vif would load are registered and opened as the working documents; `save` writes where the next vif run reads first.
- `-ab <name>`: Force audio backend (`pacat`, `aplay`, `null`, `wav:out.wav`).
- `-vol <f>`: Sound effect volume 0..1 (default 0.7); music keeps its own level.

## TUI

The TUI is a shell over the exact same dispatch table used by the REPL and scripts. Its keys build and execute standard command strings, so a script can reproduce them; only a take's commit writes its pattern into the document directly. `Tab`/`Shift-Tab` move the keys between the panes on screen, the same in every view, the pane that has them framed in green, and `Esc` brings them back from the mixer or the log to the view's own pane. `?` shows the keys of the view in hand, `:` opens the command line, `^S` saves modified documents (a first save offers the file the next vif run reads), and `^Q`/`^C` quits, warning once about unsaved changes.

- **Browse**: the browser beside the mixer, over the log. The browser's tabs are patterns then sounds (`h/l`); a sound's role says whether it is an sfx or a piece of the drum kit. `j/k`, `PgUp/PgDn` and `g/G` move, `/` finds the next name containing a term, `space` plays a sound or puts a pattern in its role's slot (again pauses), `0-3` puts the pattern in a slot or takes it out, on the next bar while the transport runs. `Enter` opens the inspector (`l/h` fold, `L/H` all, `Enter` edits a leaf, `a` adds, `x` deletes, `Esc` closes). `a/v/r` apply/validate/revert, `n/c/w` new/clone/write, `+` mixes into another, `d` twice deletes, `m` resets the transport. The mixer draws every slot on one timeline, as many bars of the sequencer's four-bar cycle as fit, under a shared playhead; on it `j/k`, `PgUp/PgDn` and `g/G` move a cursor over the slots' titles and tracks, and rows it cannot show scroll under the cursor and stay where it leaves them.
- **Keys (`p` in the browser or the beat view)**: a scratch keyboard of the tonal voices, over whatever the slots play and bound to no pattern, on a drawn keyboard of two octaves and a third from C: `z s x … m` the first octave, `q 2 w … u` the second, `i 9 o 0 p` the third's C to E (`, l . ; /` play the second's again). A dot marks each note of the key `:key` set, its root lit. `↑/↓` voice, `←/→` or `{ }` octave, `- =` velocity, `[ ]` note length, the keys the beat view edits a note's with. `^R` records a take of 1, 2 or 4 bars (`\`), against the transport; `^R` again commits it as `rec_N`, a track per voice played, each note on the degree of the key that sounds it (a note outside the key moves to the nearest). A take is the one thing the keys write, and a committed one leaves them clean. `Backspace` drops the take's last note; `Esc` goes back to the browser or the beat view it opened over, committing a take and stopping a transport the view started.
- **Beat (`b` on a pattern)**: the pattern's tracks on a step grid with a bar ruler and the playhead: a drum hit is a velocity meter, a tonal note its degree with its length drawn on, a chance under 1 dimmed. `h/l` step, `H/L` a beat, `PgUp/PgDn` a bar, `j/k` track; `space` toggles a hit or note (a note goes on from the line's last), `1-9 0` and `+/-` velocity, `c` chance, `[ ]` length, `, .` degree, `{ }` octave, `t`/`T` voice in the family or the other family, `f` follow the chord, `a` adds a track, `x` twice deletes one, `s` sets the steps. `r` shows a tonal track as a piano roll, a row per degree of the key, where `↑/↓` move the pitch and `space` places a note on it. `Enter` applies the pattern and plays it in its role's slot, and `p` opens the keys over it.
- **Size**: below 60×16 the screen shows only a centered notice of the size it needs, and keys other than quit do nothing. A large screen scales the views: wider steps and taller tracks in the beat view, a larger keyboard. Beneath an editor the mixer takes the rest of the screen, over the log's last line.

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
| `mute`/`vol`/`wait` | `mute music\|sfx\|all\|none` | Channel mute, sfx volume (music keeps its level), scripted pause. |
| `help`/`quit` | `help [verb\|keys]` | Usage, the TUI key reference, exit. |

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
