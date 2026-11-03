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
- **Scripts**: `-s <file.slab>` runs a script and exits; `run <file.slab>` runs one in the session in hand, its relative paths read from the script's own directory, so a recipe beside its files runs from anywhere (a nested `run` from its own); from the REPL or the TUI's command line, so a recipe (rip, compress, save) is one word. A line that fails stops the script with its number; `quit` in it ends the script alone.
- **Voices**: the tonal instruments are `[[voice]]` entries of the sound document, listed by `ls voice`, made by `new voice`, edited by path like a sound and applied into the running mixer, where the next note takes the new voice; a new name is a new instrument a pattern's track names. They load and save with the sounds.

## How rip works

`rip` listens to a recording and writes what soundlab needs to play it back: the tempo and where the bars fall, the key and the chord each bar leans on, the drum hits, the melody and bass lines as patterns on the sixteenth grid, the voices that play them, and a song that plays the bars in order. All of it is the engine's own vocabulary, so the result plays at once in soundlab and in the game, and is checked by ear and by number against the recording.

Two halves meet in the middle. Ripping turns a recording into patterns; training teaches a model to hear the recordings ripping starts from. Both run on the same engine, and either front end, the built-in analysis or the trained model, feeds the same decoders.

```text
  RIPPING                                         TRAINING
  recording (WAV)                                 patterns: the bank's and generated ones
      |                                               |
  1 hear     spectrogram -> front end -> frames   engine renders loops; labels are exact
      |      (the analysis, or the model) <---+       |
  2 read     tempo, bars, key, drums, lines   |   room, mix and voices drawn per loop
      |                                       |       |
  3 write    patterns, voices, transcript     |   log-mel frames + labels -> trainer
      |                                       |       |
  4 compress a dictionary of bars, a song     +------ weights (ripnet.bin)
      |
  5 refine   render, compare with the recording, keep what sounds closer
      |
  6 check    truths, two rips as witnesses, the bench
```

### 1. Hearing: from sound to frames

The recording is read at 22.05 kHz and cut into frames, a 93 ms window every 11.6 ms, about 86 frames a second. Its tuning is read first, the cents its notes sit off A440, so a recording forty cents flat reads as in tune and every later step works on its own pitch table.

A front end hears the frames into the same fields per frame: how strongly something starts (the onset), each drum's strength (kick, snare, hihat), the salience of each of 69 pitches from E1 to C7, and each register's chroma. The decoders after it never know which front end heard.

- **The analysis** is signal processing. Each frame is split into a harmonic and a percussive part by medians across time and across frequency; each drum's onsets are read in its own band; a pitch's salience is the sum of its partials, the bass register's also read from a window four times longer, which tells semitones apart at 110 Hz.
- **The model** (`ripnet`) is a transformer over log-mel frames: 229 bands from 30 Hz up, under a semitone apart from A3, scaled to the recording's peak. Each frame attends to 64 frames on either side (0.74 s) through four layers of width 256 with eight heads, 3.2 million weights run in pure Go. Its heads give, per frame, the onset, the drums, the pitches and whether a note sounds, which pitches are struck, and the wave and the 32 voice genes the note is played with, which the analysis cannot say. A weight file may also put convolutions before the layers, or split the layers into towers for groups of heads. Soundlab hears with the model when the first configuration root holds `audio/ripnet.bin`, and with the analysis otherwise.

### 2. Reading: from frames to music

The decoders are shared, classical and cheap. Every threshold lives in `RipParams`, which `tune` searches and a root's `audio/rip.toml` carries.

- **Tempo** from the autocorrelation of the onsets under a prior centred on 120 bpm, within the sequencer's 80 to 200; `rip ... bpm <n>` pins it, the bar line still decoded.
- **Beats** by dynamic programming that holds to the period.
- **The bar line** at the phase whose downbeats carry a kick, no snare and the key's root in the bass, with the snare on two and four and the most change of chroma. A phrase that leads into the first bar gets a pickup bar.
- **The key** as the root and scale whose tones hold the most chroma, the bass voting for the tonic, and a chord root per bar.
- **Drums** by peak picking: a kick by its rise under 100 Hz, a hihat by its rise above 5 kHz, a snare in between with noise above 5 kHz, and a hit can be two drums at once. `rip ... nodrums` leaves the drums out of a recording that has none.
- **Notes** by a Viterbi path through the pitch salience that pays to move and to start or end a note. The path is cut into notes, their lag behind the beats taken out, and each note lands on a sixteenth and on the key's nearest degree, a semitone left over kept as an accidental. When the model hears strikes, the melody register's notes start where a pitch is struck instead and last while it sounds, any number at once, so a note struck under a pedal's ringing is not lost to the notes still sounding.
- **Lines.** The melody register, from F3 up, is decoded as two lines at once, so a slow line under a pluck is heard too; a second-line note is kept where it reaches `second_voice` (0.5) of the first's strength. The notes are then parted again by what a line keeps: its pitch from note to note, the length and strength of its notes, never two at once. Which line is the melody is a rule, the busier by default (`rip ... lead`). The bass is decoded up to A3, and a note both registers hear goes to the line it continues.

### 3. Writing: from music to soundlab

- **Patterns** of up to `bars` (4) bars per part, `<name>_beat_1`, `<name>_melody_1` with the second line as a track of its own, `<name>_bass_1`, and on, added to the document and playable at once; the transport takes the tempo, key and tuning.
- **Voices.** Each line gets one. Through the model, its voice head names the genes of every note, averaged over the line. A line no voice was heard for, every line under the analysis, gets one fitted by ear: the engine plays a candidate voice at the line's longest and loudest notes, at the recording's pitch, loudness and tuning, compares the render's mel frames with the recording around each note, and searches the 32 genes and the wave from every registered voice. They are `<name>_melody_voice`, `<name>_under_voice` and `<name>_bass_voice`; `fit` searches again, longer.
- **The transcript** keeps every note in seconds before the grid, with how much of the recording's salience supports it and, for a melody note, how bright it is. `notes` lists it and writes it as a MIDI file to correct in a sequencer.

A rip is kept under its name for the session, so `fit`, `compress`, `refine` and `song` need no file.

A MIDI file is a rip already: `rip score.mid name` hears nothing, its notes the transcript and its drum channel the hits, the tempo the file's first, the bars four beats each from where its time signature starts, the key from its notes' pitch classes; its lines play copies of the shipped lead and bass. `compress`, `song`, `notes`, `agree` and `truth` work on it as on a recording's rip, so a score is heard beside the rip of the same piece; what hears the recording (`fit`, `refine`, `song ... score`, `notes`' support) has none to hear.

### 4. Compressing: bars into a song

`compress` turns the rip's bars into as few one-bar patterns as it can. Per part, two bars differ by the steps they do not share over the steps either has; bars within `percent` (5) of each other form a group whose middle bar stands for it (`<name>_<part>_a`, `_b`, ...), and a search refines which bar joins which. The song `<name>` lists the patterns each bar plays at the recording's tempo, key and tuning: `song <name>` shows it, `song <name> <file.wav>` renders it, `song <name> play` conducts it through the live sequencer. Its render is ripped back on its own grid and scored against the rip, so what compressing lost is a number.

### 5. Refining: closing the loop

The engine is the ground truth of its own vocabulary, so a rip is good when its render sounds like the recording. `refine` works in rounds: it fits the voices again, then tries each tonal step a semitone and an octave either way, at the recording's strongest pitch there, a step earlier or later, longer or shorter, and silent, and tries the strongest pitch where a line is silent. A move stays when the bars that play it sound closer to the recording, the render and the recording heard through one front end and their pitch salience, mel bands and onsets compared bar by bar. A voice may not drift more than a tenth from its fit to the notes.

### 6. Checking: how good a rip is

- `truth` scores a rip against notes given: a transcript corrected by hand, a rhythm tapped out, or a MIDI file, whose drum channel scores the kicks, snares and hihats hit by hit. Per line it counts the notes heard as given, those missed and those extra.
- `agree` compares two rips of one piece where no score exists.
- `bench` rips fresh loops the engine renders, whose answer is known, clean and through a room and a mix, and scores the analysis and the model side by side.
- `tune` searches the decoders' thresholds over rendered loops.

What a rip does not tell apart yet: a bass note's attack from a kick, a plucked chord's attack from a snare, a change of key from accidentals, and a third line or the chord a pedal rings, which two lines cannot hold. Soft notes are heard less often than loud ones.

### 7. Learning: how the model is trained

The model learns from music the engine renders itself, since the engine knows exactly what it played.

- **Generating.** `corpus` renders loops of `bars` (4) bars from the registered rhythm, melody and bass patterns across tempos and keys. With `gen`, three loops in five per part play a beat, a melody or a bass line drawn from the key instead, and an accompaniment in the free slot, a broken chord under a melody of longer notes; a generated loop leaves out its drums three times in ten and its bass once in seven, so a line is heard bare too.
- **Labelling.** Each loop is kept as its log-mel frames and, per frame, what the engine played: the onsets, the drums, the pitches sounding, and the wave and 32 genes of each note's voice. The labels cost nothing and are exact.
- **Roughening.** With `aug`, each loop's voices are redrawn near the registered ones, a third of them on another wave, in a third of the loops with a pedal's long releases; the mix passes through a room drawn per loop (a gain and swell, a low-pass, two equaliser bands, a soft clip, a compressor, a booth's or a hall's reverb, a noise floor), and the loop plays off A440 by up to a quarter tone. The labels stay the notes as played.
- **Training.** `tool/rip-train` holds the model's PyTorch twin. It trains on random six-second windows of the loops (512 frames, eight a step), scores the loops it holds out every thousand steps and keeps the best, and writes the weight file the Go model loads, the two agreeing to float32 rounding.
- **A round.** `tool/rip-train/pipeline.sh` renders the corpus in parts at once, trains, benches and installs; 40000 loops and 40000 steps take about four hours on a laptop with a GTX 1060. Weights ship only when they beat the analysis on the bench and on real recordings' truths; [Rip design §8](../../doc/rip-design.md#8-measured-on-the-sample-recording) holds every round's numbers and [the trainer's README](../../tool/rip-train/README.md) the instructions.

### 8. The process, end to end

1. `rip song.wav name` writes the patterns, the voices and the transcript, and sets the transport; `rip song.mid score` does the same from a score.
2. Listen: `slot` the patterns or `render` them to a WAV, and read `notes name`.
3. Check: `truth name score.mid` against a score, or `agree` two rips of the piece.
4. `compress name` writes the dictionary and the song, and `song name play` plays it.
5. `refine name` closes the loop; `fit name` refits the voices alone.
6. Save the patterns, the song and the voices to keep them, as Usage shows next.

[Audio §11](../../doc/audio.md#11-ripping-a-recording) is the engine's side of each stage and [Rip design](../../doc/rip-design.md) the plan and the measurements.

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

Put either in a `.slab` file and `run` it. Without the voices the tracks fall back to the shipped lead and bass. In the TUI, `^O` opens the files overlay: `Enter` on a `.toml` loads it, on a `.slab` runs it behind the screen, on a `.wav` or a `.mid` rips it, and `s` saves into the directory shown.

### CLI Flags
- `-tui`, `-T`: Full-screen Terminal User Interface. Empty documents are seeded from the built-ins; the REPL and scripts leave that to `builtin`.
- `-headless`: Discard audio output (`-ab null`).
- `-s <file>`: Execute a `.slab` script file line-by-line and exit.
- `-config-dir <dir>`, `-config-music <file>`, `-config-sounds <file>`: vif's flags, resolved the same way, with `vif.toml`'s `[paths] root` and `[audio] buffer_ms` read as vif reads them; any key this build does not know is named and skipped. The overrides vif would load are registered and opened as the working documents; `save` writes where the next vif run reads first. The first root's `audio/rip.toml`, when there is one, is what `rip` listens with and where `tune` writes, and its `audio/ripnet.bin` the model `rip` hears with in place of the built-in analysis ([rip-train](../../tool/rip-train/README.md)).
- `-ab <name>`: Force audio backend (`pacat`, `aplay`, `null`, `wav:out.wav`).
- `-vol <f>`: Sound effect volume 0..1 (default 0.7); music keeps its own level.

## TUI

The TUI is a shell over the exact same dispatch table used by the REPL and scripts. Its keys build and execute standard command strings, a recorded take included (`take`), so a script can reproduce everything it does. `Tab`/`Shift-Tab` move the keys between the panes on screen, the same in every view, the pane that has them framed in green, and `Esc` brings them back from the mixer or the log to the view's own pane. `<`/`>` slow the tempo down or speed it up by 5 bpm, `?` shows the keys of the view in hand, `:` opens the command line, `^O` opens the files overlay, `^S` saves modified documents (a first save offers the file the next vif run reads), and `^Q`/`^C` quits, warning once about unsaved changes.

- **Browse**: the browser beside the mixer, over the log. The browser's tabs are patterns, sounds and voices (`h/l`); a sound's role says whether it is an sfx or a piece of the drum kit. `j/k`, `PgUp/PgDn` and `g/G` move, `/` finds the next name containing a term, `space` plays a sound, a voice's middle C, or puts a pattern in its role's slot (again pauses), `0-3` puts the pattern in a slot or takes it out, on the next bar while the transport runs. `n` makes a new entry and `e` edits the selected one: a pattern in the pattern editor, a sound or a voice in the inspector, which say whether what they hold is new or edited. `Enter` opens the inspector on either (`l/h` fold, `L/H` all, `Enter` edits a leaf, `a` adds, `x` deletes, `Esc` closes). `a/v/r` apply/validate/revert, `u` undoes the last edit, `c/w` clone/write, `+` mixes into another, `p` makes a new pattern of the tracks the editor yanked, `d` twice deletes, `m` resets the transport.
- **Mixer**: every slot on one timeline, as many bars of the sequencer's four-bar cycle as fit, under a shared playhead, beside the browser or beneath the editor. `j/k`, `PgUp/PgDn` and `g/G` move a cursor over the slots' titles and tracks, and rows it cannot show scroll under the cursor and stay where it leaves them. `space` or `m` mutes the cursor's slot or lets it sound again: its pattern plays on in time, so it comes back in step. On a track row they mute that track, and `s` solos it: the slot's soloed tracks sound alone. A silent track is drawn dimmed, a soloed one lit; the mutes go when the slot changes pattern, as the log says. `e` edits what the slot plays, and `n` makes a new pattern in it.
- **Pattern editor** (`n` new, `e` edit): one pattern on two tabs, `keys` and `grid`, which `Tab` walks before the mixer; the frame and the header say whether the pattern is new or edited and the slot it plays in. A new pattern starts empty, in the first silent slot (slot 2, the free layer, when all play), beside whatever plays there, so it loops with them before it is ever saved. `Enter` applies the pattern and plays it in its slot; `^R` records a take; `^D` twice deletes the pattern, out of its slot too; `Esc` closes it, kept in the document for `^S`.
  - **keys**: the tonal voices on a drawn keyboard of two octaves and a third from C: `z s x … m` the first octave, `q 2 w … u` the second, `i 9 o 0 p` the third's C to E (`, l . ; /` play the second's again). A dot marks each note of the key `:key` set, its root lit. `↑/↓` voice, `←/→` or `{ }` octave, `- =` velocity, `[ ]` note length, the keys the grid edits a note's with; `\` walks the pattern through 1, 2 and 4 bars, refusing a length that would cut a note off. `^R` plays the pattern and records over its length; `^R` again writes the take into it through `take`, a track per voice played, each note on the degree of the key that sounds it (a note outside the key keeps its pitch as an accidental on the nearest degree), and applies it, so the loop plays it from its next pass. `Backspace` drops the take's last note.
  - **grid**: the tracks on a step grid with a bar ruler and the playhead: a drum hit is a velocity meter, a tonal note its degree, `#` or `b` after it for an accidental, with its length drawn on, a chance under 1 dimmed. `h/l` step, `H/L` a beat, `PgUp/PgDn` a bar, `j/k` track; `space` toggles a hit or note (a note goes on from the line's last), `1-9 0` and `+/-` velocity, `c` chance, `[ ]` length, `, .` degree, `{ }` octave, `t`/`T` voice in the family or the other family, `f` follow the chord, `m` mutes the track in the slot the pattern plays in, `a` adds a track, `x` twice deletes one, `J/K` move it down or up (`mv`), the cursor with it, the order a build-up brings the tracks in, `y` yanks one, `Y` adds it to the yank or takes it out, `p` pastes the yank into the pattern open (`mix`, below), `s` sets the steps, `u` undoes the last edit, all a key did. `r` shows a tonal track as a piano roll, a row per degree of the key, where `↑/↓` move the pitch and `space` places a note on it.
- **Working**: a verb that takes seconds or minutes (`rip`, `fit`, `compress`, `refine`, `tune`, `bench`, `corpus`, `render`, `song`, `run`) runs behind the screen under a working box that counts the seconds and shows the last lines it said; keys wait for it, `^Q` twice leaves it. A rip from the files overlay ends with the browser on what it heard.
- **Files** (`^O`): a directory's entries over the panes, directories first, then files newest first, opened where it last was, else the pattern document's directory, else the working directory. `j/k`, `PgUp/PgDn` and `g/G` move, `h`/`←`/`Backspace` go up, `Enter` (or `l`) opens a directory, loads a `.toml` document, sounds or patterns by the tables it holds, or rips a `.wav` or a `.mid` under a name a prompt asks for, offered from the file's name; `m` merges a document onto the one in hand instead; `s` saves the tab's document, patterns or sounds, into the directory under a name asked for, offered from its own; `Esc` or `^O` closes. A path with a space travels in quotes, which the overlay adds.
- **Size**: below 60×16 the screen shows only a centered notice of the size it needs, and keys other than quit do nothing. A large screen scales the editor: wider steps and taller tracks in the grid, a larger keyboard. Beneath the editor the mixer takes the rest of the screen, over the log's last line.

## Command Reference

`help` prints the full usage of every verb, `help <verb>` describes one and `help keys` lists the TUI's keys.

- **Documents**
  - `load [sound|pattern] <file>`, `merge [sound|pattern] <file>`: replace the document from a file, or overlay a file onto it by name; voices come with the sounds, songs with the patterns. Quote a path with a space. The kind alone names the file, which is read by its tables; a `.slab` runs as a script, and a `.wav` or a `.mid` is refused with the `rip` that reads it.
  - `builtin sound|pattern|voice`: seed the document from the built-ins; `sound` seeds the voices too.
  - `save sound|pattern [file]`, `write <name> <file>`: write the document (bare = provenance path), or one entry.
  - `ls sound|pattern|voice|song [glob]`, `show <name>[.path]`: list entries (`*` marks dirty); dump an entry as TOML, or print a leaf value.
  - `new sound|pattern|voice <name>`, `cp <src> <dst>`, `mv <src> <dst>`, `del <name>...`: seed an entry with a minimal valid spec, duplicate, rename or discard one; a discarded entry keeps any applied registry copy.
  - `validate|apply|revert [name|all]`: run the domain validations, push into the live registry, or restore from it; bare or `all` is every entry for `validate`, the dirty ones for `apply` and `revert`.
  - `undo [n]`: step back over the last `n` (1) edits, an edit being a command that changed a document (a nested `run` folds into its line, and every command one TUI key runs is one edit): the entries it changed come back as they were, one it made goes and one it deleted returns, with their order and `*` marks; an entry an apply has since registered stays marked, since the registry and the transport are left as they are. It says what it restored; the history holds the last 100 edits.
- **Editing**
  - `set <name.path> <val>`, `unset <name.path>`: set a leaf field through reflection, or reset it to its zero value.
  - `add <name.path> [at]`, `del <name.path.idx>...`: insert a zero element into a list field at index `at` (bare appends), or delete elements, several at once, each as its path reads before any is deleted.
  - `dup <name.path.i> [j]`, `mv <name.path.i> <j>`: insert a copy of a list element at index `j` (bare, right after it), sharing nothing with it, or move an element to `j`, those between shifting by one: a pattern's tracks, a track's events, a sound's layers, a song's bars (`dup bite.bars.3 5` plays bar 3 again at 5). An edit of a song's lists then lists its bars as `song` does, numbered as the paths address them.
  - `mix <src> <dst>`: append src's tracks or layers into dst, of the same kind; src is unchanged.
  - `cp <pattern>.track.<n>... <new>`, `mix <pattern>.track.<n>... <dst>`, `mix <pattern>.track.<n>... <pattern>.track.<m>`: make a new pattern of the tracks named, from one pattern or several, with the first's role, append them to the pattern `dst`, or merge their notes into track `m`, which keeps its voice: a note the same as one already at its step (place, degree, octave and accidental) is kept once and counted, and a drum track never merges into a tonal one or the reverse. With `del`, tracks move. Tracks of other lengths meet at the least common multiple of their lengths, each repeated to fill it, the destination pattern's own too; past 64 steps the pattern keeps its length and a longer loop is cut, as the command says. A pattern holds 32 tracks.
  - `cp <pattern>.bar.<k>... <new>`: make a new pattern of one pattern's bars, in the order named, every track sliced at the bar lines (a bar is 16 steps, a pattern's last one may be short); a bar named twice plays twice, as a loop or a song built by hand from a rip wants, up to 64 steps.
  - `take <pattern> <voice> <pos>:<midi>[:<steps>[:<vel>]]...`: add a track of a tonal voice playing the notes at their steps, each on the key's nearest degree with an accidental for the semitone left: what the editor's keys write when a recording ends.
- **Audition and transport**
  - `play <name|file.wav> [vol]`, `hit kick|snare|hihat|clap [vol]`, `note <midi> [vel] [steps] [voice]`: audition an entry, a drum, or a voice through the melodic slot, applying a dirty one first; a `.wav` plays as it is.
  - `export <name> <out.wav> [variant]`: render an entry to a WAV.
  - `render <file.wav> [bars]`: write what the slots hold, `bars` (4) bars at the transport's tempo and key, to a WAV through the engine's offline render: a rip's estimate heard without a device.
  - `slot <0|1|2|3> <pattern|-> [fade_ms] [q]`: put a pattern in a slot, `-` for silence; a dirty pattern applies first.
  - `music start|stop|reset`, `bpm <n>`, `swing <f>`, `fill on|off`, `key <root> <scale> [deg...]`: transport, tempo, shuffle, slot 2's auto-fill and harmony; `tuning [cents]` shows or sets the cents the transport plays off A440, within a semitone: a rip sets the recording's, a song its own when played.
  - `mute music|sfx|all|none`, `mute slot <0-3> on|off`, `vol <f>`, `wait <ms>`: mute a channel, or one slot while its pattern plays on in time; the sfx volume (music keeps its level); a scripted pause.
  - `mute track <0-3> <n> on|off`, `solo track <0-3> <n> on|off`: mute track `n` of the pattern a slot plays, or solo it, its pattern playing on in time: a slot's soloed tracks sound alone, its muted ones never. An audition aid the pattern does not save, dropped, as the log says, when the slot changes pattern or an apply moves the tracks.
  - `where`, `stat`: the transport's position and the slots' patterns, a muted slot and its muted and soloed tracks marked; the backend, play stats, the muted slots and tracks, and dirty entries.
- **Ripping and measuring**
  - `rip <file.wav|file.mid> <name> [bars] [nodrums] [bpm <n>] [lead busy|high|low|loud|soft|long|short]`: hear a recording's beat, melody and bass as patterns of up to `bars` (4) bars each, named `<name>_beat_1`..., take its tempo and key, and fit a voice to each register the front end heard none for; `nodrums` leaves the drum part out. A `.mid` file is read, not heard: its lines and drums as `truth` reads them, quantized as a rip's notes are on the file's own ticks, at its first tempo (an octave faster or slower until the sequencer plays it; a change after it is said and played at the first), in bars of four beats from where its time signature starts (4/4 when it names none; a short first bar is a pickup), in the key its notes' pitch classes hold most, the lines played by copies of the shipped lead and bass under the rip's voice names; a note outside E1 to C7 is dropped and counted, and `bpm` and `lead` are refused. The recording's tuning is read first, and a phrase before the first bar line gets a pickup bar; `bpm <n>` reads it at that tempo, the bar line still decoded, for a recording whose tempo is known or wanted on another metrical level. `lead` names which of the melody register's two lines is the melody once they are parted: the busier by default, the one with the most notes for its sounding time, or the higher, lower, louder, softer, or longer or shorter in its notes when you know the piece.
  - `notes <name> [file.mid]`: list the transcript of a recording ripped as `<name>`: each note's time, bar and step, pitch as the key's degree with any accidental, length, loudness and support (how much of the recording's salience over its frames is its pitch's; a low one is a note to doubt), and a melody-register note's `bright` (its fundamental's share of its first six partials, 1 for a pure tone; none where a concurrent note covers the fundamental throughout), by line, after the tuning, where the first bar falls and how many melody-register notes went unmeasured. With a `.mid` file, write the transcript there instead as a Standard MIDI File at the rip's tempo, a track each named `melody`, `under` and `bass` and the drums as a track named `drums` on the drum channel (General MIDI's kick, snare and closed hat), the notes and hits at their seconds from the recording's start, to correct in a sequencer and give back to `truth`.
  - `agree <a> <b>`: compare the transcripts of two rips of one piece, two witnesses where no score is to hand: each line's notes aligned in order under the transposition that matches most, so a tempo that drifts does not break the match, the notes only one hears listed, the melody register also as one line in case the two part its lines differently, and the time map between them.
  - `truth <name> <file>`: score the transcript of a recording ripped as `<name>` against notes given in a file in the `notes` verb's own form, corrected by hand or written out (a header `at N bpm` sets the step; each line `<line> <start>s ... <pitch> ... <length> step(s)` or `<line> <start>s <pitch> <length>s`, the rest ignored), or as a rhythm tapped out without pitches (`unit eighth` or `unit 203ms`, `start 0.34s` or where the rip's melody register first sounds, then `melody (1-2-3---4-56---)x3 a-a-a-a-a-a-a`: a character a note a unit long, `-` lengthening the note before it, `.` a rest, `(...)x3` a repeat), or in a `.mid` file of type 0 or 1, read under its tempo map (a track named `melody`, `under` or `bass` is that line; of the rest, the lowest is the bass when its notes centre under E3, kept whole, and the others are the melody, then the under, in file order; any further track is left out): how many of the notes given the rip heard, by pitch in order or by onset, and how many it heard that were not given, per line, with the given notes cut in two and the heard notes spanning two counted for a rhythm. Notes given with pitches are also counted by how loud the recording plays each, in 6 dB bands under the loudest tenth of them, and by whether another note given sounds at their onset, per line and for the melody register as one. A `.mid` file's drum channel scores the drums the same way, kick (keys 35, 36), snare (38, 40) and hihat (42, 44, 46) apart: each hit given takes the rip's nearest of its drum within half a step, under the stretch and offset that match most.
  - `fit <name> [generations]`: fit the voices of a recording ripped as `<name>` again, over `generations` (12) of candidates the engine plays at its notes; they replace `<name>_melody_voice` and `<name>_bass_voice`.
  - `compress <name> [percent]`: match the bars of a recording ripped as `<name>` to as few one-bar patterns as a difference of `percent` (5) allows: the dictionary `<name>_<part>_a`..., and the song `<name>` that plays them in order, rendered and ripped back for a score.
  - `song <name> [file.wav [<instrument>...]|play|score]`: show a song's bars; render it to a WAV, only the instruments named sounding when any are (a line on its own, to hear against the recording); play it through the sequencer, the slots changed at the bar lines (`music stop` ends it); or score it: how it sounds against the recording ripped as `<name>` (pitch, timbre, onsets) and how it rips back.
  - `refine <name> [rounds]`: close the loop over a song `compress` made: each round (3) fits the voices again and sweeps the tonal steps, each tried a semitone and an octave either way, the recording's strongest pitch at its step, a step earlier or later, longer or shorter, and silent (a step left silent is dropped), then a step a line leaves empty tries that strongest pitch; every move kept when the bars that play its pattern sound more like the recording's own frames (pitch, timbre and onsets), the rip-back reported beside it. A voice may not leave its tonal floor, its distance from the recording's notes as `fit` measures it, by more than a tenth, and a proposed pitch keeps within a fifth of its track's nearest note. Minutes a round.
  - `bench [loops] [bars] [verbose]`: grade the analysis, and the model when one loaded, on fresh loops (8) of `bars` (4) bars with generated parts among the bank's, clean and augmented: the mean score of each rip against the loop's expected rip. `verbose` then lists each loop as each front end ripped it, its index, tempo, key, clean or augmented, the parts generated (`bank` for none) and its score's fields, and the three worst by total.
  - `corpus <dir> [loops] [bars] [aug] [gen] [nowav] [tempos lo-hi|a,b,c] [part k/n]`: render `loops` (24) of `bars` (4) bars from the registered patterns into `dir`, each a WAV, its log-mel frames and the events that sound in it: what `tool/rip-train` trains the rip's model on; `aug` puts a room, a mix and voices of random genes behind each (How rip works, 7, lists what it draws) and renders each off A440 by up to a quarter tone, `gen` draws beats, melodies, bass lines and accompaniments the bank never had and leaves the drums or the bass out of some, `nowav` keeps the frames and labels only, `tempos` sets the tempos the loops cycle through; a loop already in `dir` is kept, so a stopped export run again goes on where it stopped, and `part k/n` renders every `n`-th loop from the `k`-th for `n` exports at once, leaving `corpus.json` to a last run without it (`tool/rip-train/README.md` is the training guide).
  - `tune [generations] [file]`: search `rip`'s parameters over loops the engine renders from the registered patterns; a set that beats the current one is written (default the first root's `audio/rip.toml`) and listened with from then on. Eight generations take minutes.
- **Scripts**
  - `run <file.slab>`: run a script's lines in this session, strict as `-s`: a failing line stops it with its number; `quit` in it ends the script alone, and outside a script ends the session.

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
