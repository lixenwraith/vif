# soundlab

`soundlab` is the terminal-based audio authoring environment and sequencer for `vif`. It runs entirely on the `pkg/audio` engine and provides a reflective, command-driven interface over the TOML audio specifications.

Supports interactive REPL, TUI, and headless script execution. The session, its documents, the verb table and the TUI are `internal/soundlab`, which runs over any `terminal.Terminal` and takes a host's own speakers as its sink; this command adds the flags, the REPL and the process's terminal and signals.

## Architecture & Concepts

- **Document vs Registry**: Edits mutate the working document in memory. The audio engine operates on the live registry. `apply` pushes document state to the engine (enabling auditioning); `revert` overwrites the document with the canonical registry state.
- **Dotted Path Addressing**: The command grammar interacts directly with Go reflection over `toml` tags. Paths like `blip.layer.0.source.freq` are type-checked and routed to exactly what the TOML deserializer sees.
- **Zero-Value Masking**: Pointer chains and zero values indicate "unset" or "package default", preserving minimal explicit TOML generation via `omitempty`.
- **Validation Bounds**: The REPL bounds checks are purely structural (type width, float finiteness). Domain-specific validation (Nyquist bounds, envelope durations) happens synchronously at `validate` and `apply`.
- **Slots**: 0 rhythm, 1 melody, 2 fill, 3 bass, as the game draws them; a melody pattern carries no bass, so slot 1 alone is the melody as authored. The engine's phrase auto-fill is off in soundlab so slots stay deterministic; `fill on` restores it to audition the fill bank.
- **Audition**: `play`, `hit` and `export` render a spec as authored; vif's per-sound volume and shape table is not applied.
- **Rip**: `rip <file.wav> <name> [bars]` hears a recording's beat, melody and bass as patterns of up to `bars` bars each (`<name>_beat_1`, `<name>_melody_1`, `<name>_bass_1`, ...), added to the document dirty, and sets the transport's tempo and key to what it heard; `slot` them to hear the estimate, `notes <name>` to read the transcript, then edit. A line the front end heard no voice for gets one fitted by the engine's own renders of its notes (`<name>_melody_voice`, `<name>_under_voice` for the second line the melody register carries under the louder, `<name>_bass_voice`, registered at once; `fit <name> [generations]` fits them again, longer); the melody pattern carries the second line as a track of its own. The recording's tuning is read first (a clip forty cents flat reads as in tune), a phrase leading into the first bar gets a pickup bar, a note off the key lands on its nearest degree with an accidental (`acc`), and a bass note's attack can read as a kick ([Audio §11](../doc/audio.md#11-ripping-a-recording)).
- **Scripts**: `-s <file.slab>` runs a script and exits; `run <file.slab>` runs one in the session in hand, from the REPL or the TUI's command line, so a recipe (rip, compress, save) is one word. A line that fails stops the script with its number; `quit` in it ends the script alone.
- **Songs**: `compress <name> [percent]` matches a rip's bars to as few one-bar patterns as a difference of `percent` (5) of their steps allows, per part (`<name>_beat_a`, `_b`, ... `<name>_melody_a` ... `<name>_bass_a` ...), and writes the song `<name>`: which bar plays which, at the rip's tempo and key, a `[[song]]` the pattern document carries, listed by `ls song`, shown by `song <name>`, saved and loaded with the patterns. The song is rendered and ripped back on its own grid, tempo and downbeat pinned, and that rip scored against the recording's, so what the compression and the fitted voices lost is a number. `song <name> <file.wav>` renders it offline; `song <name> play` conducts it through the live sequencer, each bar's patterns landing on its bar line (`music stop` ends it). A rip is kept under its name for the session, so `fit`, `compress`, `refine` and `song` need no file ([Rip design](../doc/rip-design.md)). `refine <name> [rounds]` closes the loop: each round fits the voices again and searches the tonal steps' velocities and lengths, each step kept only when the song's render rips back nearer the recording's rip. `rip ... nodrums` leaves the drum part out of a recording the listener knows has none.
- **Measuring**: `bench [loops] [bars]` grades the analysis, and the model when one loaded, on fresh loops the engine renders with generated parts among the bank's, clean and augmented, by the mean score of each rip against the loop's expected rip; `corpus ... aug gen` exports loops through a room, a mix and voices of random genes, a third of them saturated, each rendered off A440 by up to a quarter tone, with beats, melodies and bass lines drawn from the key beside the bank's, for the trainer.
- **Two voices in a register**: the melody register is tracked twice, the second path with the first's notes and their partials cancelled, and the second path's notes kept where they reach `second_voice` (0.5) of the first's strength, so a slow line under a pluck is heard too, as a track of its own through its own fitted voice; `second_voice = 0` in `rip.toml` hears one.
- **Voices**: the tonal instruments are `[[voice]]` entries of the sound document, listed by `ls voice`, made by `new voice`, edited by path like a sound and applied into the running mixer, where the next note takes the new voice; a new name is a new instrument a pattern's track names. They load and save with the sounds.

## Usage

```bash
# TUI mode
go run ./cmd/soundlab -tui

# REPL mode
go run ./cmd/soundlab

# Headless batch processing
go run ./cmd/soundlab -headless -s script.slab
```

To rip a recording to a document and hear it, in the REPL or the TUI's command line:

```text
rip song.wav bite nodrums        # the patterns, the fitted voices; nodrums when it has no kit
compress bite                    # the dictionary and the song bite
song bite play                   # conducts it; music stop ends it
save pattern bite_patterns.toml  # the patterns and the song
write bite_melody_voice bite_melody_voice.toml
write bite_bass_voice bite_bass_voice.toml
```

and later, to hear the document again:

```text
load sound bite_melody_voice.toml      # the voices first, registered by apply
apply bite_melody_voice
merge sound bite_bass_voice.toml
apply bite_bass_voice
load pattern bite_patterns.toml        # the patterns and the song that name them
song bite play                         # or: song bite out.wav
```

Put either in a `.slab` file and `run` it. Without the voices the tracks fall back to the shipped lead and bass. In the TUI, `^O` opens the files overlay: `Enter` on a `.toml` loads it, on a `.wav` rips it, and `s` saves into the directory shown.

### CLI Flags
- `-tui`, `-T`: Full-screen Terminal User Interface. Empty documents are seeded from the built-ins; the REPL and scripts leave that to `builtin`.
- `-headless`: Discard audio output (`-ab null`).
- `-s <file>`: Execute a `.slab` script file line-by-line and exit.
- `-config-dir <dir>`, `-config-music <file>`, `-config-sounds <file>`: vif's flags, resolved the same way, with `vif.toml`'s `[paths] root` and `[audio] buffer_ms` read as vif reads them; any key this build does not know is named and skipped. The overrides vif would load are registered and opened as the working documents; `save` writes where the next vif run reads first. The first root's `audio/rip.toml`, when there is one, is what `rip` listens with and where `tune` writes, and its `audio/ripnet.bin` the model `rip` hears with in place of the built-in analysis ([rip-train](../../tool/rip-train/README.md)).
- `-ab <name>`: Force audio backend (`pacat`, `aplay`, `null`, `wav:out.wav`).
- `-vol <f>`: Sound effect volume 0..1 (default 0.7); music keeps its own level.

## TUI

The TUI is a shell over the exact same dispatch table used by the REPL and scripts. Its keys build and execute standard command strings, a recorded take included (`take`), so a script can reproduce everything it does. `Tab`/`Shift-Tab` move the keys between the panes on screen, the same in every view, the pane that has them framed in green, and `Esc` brings them back from the mixer or the log to the view's own pane. `<`/`>` slow the tempo down or speed it up by 5 bpm, `?` shows the keys of the view in hand, `:` opens the command line, `^O` opens the files overlay, `^S` saves modified documents (a first save offers the file the next vif run reads), and `^Q`/`^C` quits, warning once about unsaved changes.

- **Browse**: the browser beside the mixer, over the log. The browser's tabs are patterns, sounds and voices (`h/l`); a sound's role says whether it is an sfx or a piece of the drum kit. `j/k`, `PgUp/PgDn` and `g/G` move, `/` finds the next name containing a term, `space` plays a sound, a voice's middle C, or puts a pattern in its role's slot (again pauses), `0-3` puts the pattern in a slot or takes it out, on the next bar while the transport runs. `n` makes a new entry and `e` edits the selected one: a pattern in the pattern editor, a sound or a voice in the inspector, which say whether what they hold is new or edited. `Enter` opens the inspector on either (`l/h` fold, `L/H` all, `Enter` edits a leaf, `a` adds, `x` deletes, `Esc` closes). `a/v/r` apply/validate/revert, `c/w` clone/write, `+` mixes into another, `d` twice deletes, `m` resets the transport.
- **Mixer**: every slot on one timeline, as many bars of the sequencer's four-bar cycle as fit, under a shared playhead, beside the browser or beneath the editor. `j/k`, `PgUp/PgDn` and `g/G` move a cursor over the slots' titles and tracks, and rows it cannot show scroll under the cursor and stay where it leaves them. `space` or `m` mutes the cursor's slot or lets it sound again: its pattern plays on in time, so it comes back in step. `e` edits what the slot plays, and `n` makes a new pattern in it.
- **Pattern editor** (`n` new, `e` edit): one pattern on two tabs, `keys` and `grid`, which `Tab` walks before the mixer; the frame and the header say whether the pattern is new or edited and the slot it plays in. A new pattern starts empty, in the first silent slot (slot 2, the free layer, when all play), beside whatever plays there, so it loops with them before it is ever saved. `Enter` applies the pattern and plays it in its slot; `^R` records a take; `^D` twice deletes the pattern, out of its slot too; `Esc` closes it, kept in the document for `^S`.
  - **keys**: the tonal voices on a drawn keyboard of two octaves and a third from C: `z s x … m` the first octave, `q 2 w … u` the second, `i 9 o 0 p` the third's C to E (`, l . ; /` play the second's again). A dot marks each note of the key `:key` set, its root lit. `↑/↓` voice, `←/→` or `{ }` octave, `- =` velocity, `[ ]` note length, the keys the grid edits a note's with; `\` walks the pattern through 1, 2 and 4 bars, refusing a length that would cut a note off. `^R` plays the pattern and records over its length; `^R` again writes the take into it through `take`, a track per voice played, each note on the degree of the key that sounds it (a note outside the key keeps its pitch as an accidental on the nearest degree), and applies it, so the loop plays it from its next pass. `Backspace` drops the take's last note.
  - **grid**: the tracks on a step grid with a bar ruler and the playhead: a drum hit is a velocity meter, a tonal note its degree, `#` or `b` after it for an accidental, with its length drawn on, a chance under 1 dimmed. `h/l` step, `H/L` a beat, `PgUp/PgDn` a bar, `j/k` track; `space` toggles a hit or note (a note goes on from the line's last), `1-9 0` and `+/-` velocity, `c` chance, `[ ]` length, `, .` degree, `{ }` octave, `t`/`T` voice in the family or the other family, `f` follow the chord, `a` adds a track, `x` twice deletes one, `s` sets the steps. `r` shows a tonal track as a piano roll, a row per degree of the key, where `↑/↓` move the pitch and `space` places a note on it.
- **Working**: a verb that takes seconds or minutes (`rip`, `fit`, `compress`, `refine`, `tune`, `bench`, `corpus`, `render`, `song`, `run`) runs behind the screen under a working box that counts the seconds and shows the last lines it said; keys wait for it, `^Q` twice leaves it. A rip from the files overlay ends with the browser on what it heard.
- **Files** (`^O`): a directory's entries over the panes, directories first, then files newest first, opened where it last was, else the pattern document's directory, else the working directory. `j/k`, `PgUp/PgDn` and `g/G` move, `h`/`←`/`Backspace` go up, `Enter` (or `l`) opens a directory, loads a `.toml` document, sounds or patterns by the tables it holds, or rips a `.wav` under a name a prompt asks for, offered from the file's name; `m` merges a document onto the one in hand instead; `s` saves the tab's document, patterns or sounds, into the directory under a name asked for, offered from its own; `Esc` or `^O` closes. A path with a space travels in quotes, which the overlay adds.
- **Size**: below 60×16 the screen shows only a centered notice of the size it needs, and keys other than quit do nothing. A large screen scales the editor: wider steps and taller tracks in the grid, a larger keyboard. Beneath the editor the mixer takes the rest of the screen, over the log's last line.

## Command Reference

`help` prints the full usage of every verb; `help <verb>` describes one.

| Command | Usage | Description |
|---|---|---|
| `load`/`merge` | `load sound\|pattern <file>` | Replace the document from a file, or overlay a file onto it by name; voices come with the sounds, songs with the patterns. Quote a path with a space. |
| `builtin` | `builtin sound\|pattern\|voice` | Seed the document from the built-ins; `sound` seeds the voices too. |
| `save`/`write` | `save sound\|pattern [file]` | Write the document (bare = provenance path); `write <name> <file>` saves one entry. |
| `export` | `export <name> <out.wav> [variant]` | Render definition into a PCM WAV. |
| `rip` | `rip <file.wav> <name> [bars] [nodrums]` | Hear a recording's beat, melody and bass as patterns of up to `bars` (4) bars each, named `<name>_beat_1`..., take its tempo and key, and fit a voice to each register the front end heard none for; `nodrums` leaves the drum part out. The recording's tuning is read first, and a phrase before the first bar line gets a pickup bar. |
| `notes` | `notes <name>` | List the transcript of a recording ripped as `<name>`: each note's time, bar and step, pitch as the key's degree with any accidental, length and loudness, by line, after the tuning and where the first bar falls. |
| `fit` | `fit <name> [generations]` | Fit the voices of a recording ripped as `<name>` again, over `generations` (12) of candidates the engine plays at its notes; they replace `<name>_melody_voice` and `<name>_bass_voice`. |
| `compress` | `compress <name> [percent]` | Match the bars of a recording ripped as `<name>` to as few one-bar patterns as a difference of `percent` (5) allows: the dictionary `<name>_<part>_a`..., and the song `<name>` that plays them in order, rendered and ripped back for a score. |
| `song` | `song <name> [file.wav\|play\|score]` | Show a song's bars; render it to a WAV; play it through the sequencer, the slots changed at the bar lines (`music stop` ends it); or score it: how it sounds against the recording ripped as `<name>` (pitch, timbre, onsets) and how it rips back. |
| `refine` | `refine <name> [rounds]` | Close the loop over a song `compress` made: each round (3) fits the voices again and sweeps the tonal steps, each tried a semitone and an octave either way and silent (a step left silent is dropped), every move kept when the song's render sounds more like the recording's own frames (pitch, timbre and onsets, bar by bar), the rip-back reported beside it. Minutes a round. |
| `bench` | `bench [loops] [bars]` | Grade the analysis, and the model when one loaded, on fresh loops (8) of `bars` (4) bars with generated parts among the bank's, clean and augmented: the mean score of each rip against the loop's expected rip. |
| `run` | `run <file.slab>` | Run a script's lines in this session, strict as `-s`: a failing line stops it with its number; `quit` in it ends the script alone. |
| `render` | `render <file.wav> [bars]` | Write what the slots hold, `bars` (4) bars at the transport's tempo and key, to a WAV through the engine's offline render: a rip's estimate heard without a device. |
| `corpus` | `corpus <dir> [loops] [bars] [aug] [gen] [nowav]` | Render `loops` (24) of `bars` (4) bars from the registered patterns into `dir`, each a WAV, its log-mel frames and the events that sound in it: what `tool/rip-train` trains the rip's model on; `aug` puts a room, a mix and voices of random genes behind each, drives a third of them into a soft clip and renders each off A440 by up to a quarter tone, `gen` draws beats, melodies and bass lines the bank never had, `nowav` keeps the frames and labels only. |
| `tune` | `tune [generations] [file]` | Search `rip`'s parameters over loops the engine renders from the registered patterns; a set that beats the current one is written (default the first root's `audio/rip.toml`) and listened with from then on. Eight generations take minutes. |
| `ls`/`show` | `ls sound\|pattern\|voice\|song [glob]`, `show <name>[.path]` | List entries (`*` marks dirty); dump TOML shape, or print leaf value. |
| `where`/`stat` | `stat` | Transport position and slot patterns, a muted slot marked; backend, play stats, dirty entries. |
| `new`/`cp`/`mv`/`del` | `new sound\|pattern\|voice <name>` | Seed, duplicate, rename or discard an entry. `new` seeds a minimal valid spec; `del <name.path.idx>` removes a list element, and a discarded entry keeps any applied registry copy. |
| `mix` | `mix <src> <dst>` | Append src's tracks/layers into dst. |
| `take` | `take <pattern> <voice> <pos>:<midi>[:<steps>[:<vel>]]...` | Add a track of a tonal voice playing the notes at their steps, each on the key's nearest degree with an accidental for the semitone left: what the editor's keys write when a recording ends. |
| `set`/`unset` | `set <name.path> <val>` | Mutate a leaf field via reflection, or reset it to its zero value. |
| `add` | `add <name.path>` | Append a zero-element to a slice field. |
| `validate`/`apply`/`revert` | `apply [name\|all]` | Run domain validations, push into the live registry, or restore from it; bare or `all` = every entry for `validate`, dirty entries for `apply`/`revert`. |
| `play`/`hit`/`note` | `play <name> [vol]`, `note <midi> [vel] [steps] [voice]` | Audition. `hit` plays a drum; `note` runs a voice through the melodic sequencer slot, applying it first when it is dirty. |
| `slot` | `slot <0\|1\|2\|3> <pat\|-> [fade_ms] [q]` | Transport assignment; dirty patterns auto-apply. |
| `music`/`bpm`/`swing`/`fill`/`key`/`tuning` | `music start\|stop\|reset` | Transport, tempo, shuffle, slot-2 auto-fill, harmony.; `tuning [cents]` shows or sets the cents the transport plays off A440, within a semitone: a rip sets the recording's, a song its own when played. |
| `mute`/`vol`/`wait` | `mute music\|sfx\|all\|none`, `mute slot <0-3> on\|off` | Channel mute, or one slot's while its pattern plays on in time; sfx volume (music keeps its level); scripted pause. |
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
