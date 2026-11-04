# Stapes

Stapes is the authoring environment over
[`lixenwraith/audio`](https://github.com/lixenwraith/audio), the engine and
banks vif plays. The engine's own design, from its stream contract to the rip,
is the [audio library's document](https://github.com/lixenwraith/audio/blob/main/doc/audio.md)
(cited below as "library §n"); the [README](../README.md) is the command and key
reference, and [Rip design](rip-design.md) is where the rip is going.

## 1. What it offers

The package `stapes` holds the session, its documents, the verb table and the
TUI, which runs over any `terminal.Terminal`; `cmd/stapes` is its terminal
command, the flags and the REPL, and a host with its own screen and speakers
runs the package in-process, handing `NewSession` the speakers. It offers:

- a command REPL;
- a full-screen TUI: a browser of patterns, sounds and voices beside a mixer that
  draws every slot on one timeline, mutes a slot or a track and solos a track, a
  structural inspector, and the pattern editor, new or on an existing pattern: a
  drawn keyboard that records takes onto the key's degrees, and a step grid with a
  piano roll;
- headless `.slab` script execution;
- validation, live registry apply/revert, audition, sequencer slot assignment,
  TOML save, WAV export, and MIDI both ways: a pattern or a song written as a
  Standard MIDI File, and a MIDI file loaded as patterns and a song.

It registers the sound and pattern banks vif ships, from the audio module's
`bank`, and the overrides its search path holds (§2), as vif registers them, so
its registry and that of a game run on the same user root hold identical specs. It does not apply vif's sfx
levels and sound shapes, so its auditions and exports play a spec as authored.

The working document and live registry are separate: edits do not affect audio
until `apply`; `revert` restores the document from the canonical registry. Its
dotted path grammar follows TOML tags and Go types, while domain validation
runs at `validate`/`apply`.

## 2. Its files

The root is `-config-dir`, or else `os.UserConfigDir()` plus `stapes`
(`~/.config/stapes` on Linux and FreeBSD), and holds stapes' own files, flat:

- `stapes.toml`, the settings: `[audio] buffer_ms`, the mixer period, the
  engine's 50 ms when unset; a key it has no field for is refused by name;
- `rip.toml`, the rip's parameters: what `rip` listens with when it is there,
  and where `tune` writes;
- `ripmodel.bin`, the rip's learned front end: what `rip` hears with when it is
  there, the built-in analysis when it is not
  ([rip-train](../tool/rip-train/README.md)).

The documents, `music.toml` and `sounds.toml`, are found along a search path,
whose one folder is vif's audio folder, `os.UserConfigDir()` plus `vif/audio`:
the overrides vif reads from its user root, opened as the working documents. A
document saves back to the file it came from, and an untitled save lands in the
search path's first folder, where vif reads it next run. `-config-music` and
`-config-sounds` name a document's file outright, read and saved there.
`ResolveFiles` reads nothing of vif's code and never probes the working
directory; `-config-dir` names the root alone, so the search path holds vif's
folder whatever it names.

A user `sounds.toml` may `include` files inside its own directory, which vif,
its `-check` and stapes resolve alike, so a file stapes saves with its includes
is what play loads.

### Paths on every host

Stapes names its files by path on every host, each verb alike: `load`, `merge`
and `save` of a document, `write` of an entry (TOML, or a `.mid` for a pattern or
a song), `rip` and `truth` of a `.wav` or a `.mid`, and `render`, `export` and
`song` to a WAV or a `.mid`. A relative path reads from the working directory, or
from a running script's own, and a bare `save` writes back to the document's file
or the search path's first folder, so a terminal, over ssh too, needs no dialog;
the files overlay (`^O`) browses the same paths. Stapes has no browser host,
though the audio library writes and reads MIDI there too, from any reader or
writer.

On Android, where vif's app is to host it in-process
([vif's todo](https://github.com/lixenwraith/vif/blob/main/doc/todo.md#bring-stapes-to-android-stapes-mobile)),
the root and the search path are in the app's own storage, which these paths
reach as they are. A MIDI file or a recording another app holds, a download or a
shared file, is a content URI behind the Storage Access Framework, with no path.
There the host opens the system's picker (`ACTION_OPEN_DOCUMENT`), copies what
was picked into the search path's first folder and runs the verb on the copy; a
file to hand back is written there and offered through `ACTION_CREATE_DOCUMENT`
or a share. The verbs stay as they are, and so does every other host.

## 3. MIDI

MIDI goes through the library's one table, `midiInstruments` (library
[§8](https://github.com/lixenwraith/audio/blob/main/doc/audio.md#8-midi-files)): `write <pattern|song> <file.mid>`
writes a pattern or a song (`WriteSongMIDI`, `WritePatternMIDI`), and `load` and
`merge <file.mid> [name]` read a file as patterns and a song (`MIDIScore.Song`),
whose bars play out of time where the file is not in 4/4 until songs carry a
meter ([vif's todo](https://github.com/lixenwraith/vif/blob/main/doc/todo.md#give-songs-and-patterns-a-meter)).
`truth` and `corpus <dir> from` read `ReadMIDI`'s transcript. Stapes turns the
phrase fill off (`SetAutoFill(false)`), and its mixer mutes slots and tracks and
solos tracks (library
[§6](https://github.com/lixenwraith/audio/blob/main/doc/audio.md#6-mixer-buses-mute-and-pause)).

## 4. The rip's verbs

Over the library's rip (library
[§9](https://github.com/lixenwraith/audio/blob/main/doc/audio.md#9-ripping-a-recording)), stapes' verbs:

- `rip` adds a rip's patterns to the document, dirty, and sets the transport's
  tempo and key, so a slot plays the estimate at once; `rip ... bpm <n>` pins
  the tempo alone, `rip ... lead` names the rule that picks the melody
  (`Ripper.Lead`), and `rip <file.mid> <name>` reads a MIDI file as a rip
  (`MIDIScore.Rip`). For each register the front end heard no voice for, `rip`
  searches the voice's genes and wave for the nearest (`RenderNote`,
  `MelDistance`), seeded with every registered voice; `fit` runs it again with
  more generations.
- `agree` compares two rips of one piece (`MatchNotes`); `truth` scores a rip
  against notes given as text or a tapped rhythm (`ParseTranscript`) or a MIDI
  file (`truth <name> <file.mid>`), counting the notes heard by level as well
  (`NoteLevels`); `notes` lists a transcript, each note's brightness with the
  unmeasured counted, and `notes <name> <file.mid>` writes one (`WriteMIDI`) to
  correct in a sequencer. `corpus <dir> from` labels a file's other drum strikes
  (`MIDIScore.Struck`) as onsets alone.
- `rip` listens with the root's `rip.toml` when one is there, and hears with
  the root's `ripmodel.bin`, which `tool/rip-train` trains on loops stapes
  exports (`corpus`); `render` writes what the slots hold to a WAV through
  `RenderMusic`, so an estimate is heard without a device.
- `tune` searches the rip's parameters over loops rendered from the registered
  patterns across tempos and keys with `lixenwraith/genetic`'s generational
  engine, the current set seeding the pool, and writes a set that beats it
  ([todo](todo.md#tune-the-rip) for what the corpus still lacks).
- `compress` runs the grouping (`Rip.Cluster`), refines the assignment with
  `lixenwraith/genetic` under a term for the patterns in use, rips the song's
  render back on the song's own grid (`Ripper.Grid`) and scores it against the
  rip; `song` shows, renders or conducts a song through the live sequencer, or
  renders one line of it alone, the instruments named after the file.
- `refine` closes the loop: the voices fitted again and the tonal steps'
  velocities and lengths searched with `Fidelity` as the fitness, a step kept
  only when it rises, each voice held within a tenth of its tonal floor, the
  fit's own distance from the recording's notes, so the score of the mix does not
  drift a voice into one that fills the mix.
- `bench` grades the analysis and a loaded model on fresh renders, clean and
  augmented (`corpus ... aug`). With `sampled <dir>`, both play half of a loop's
  tonal lines on NSynth's recorded notes in place of the voices: an instrument
  whose pitches reach the line's notes within three semitones, each note shifted
  from the nearest, cut into its recording's release at the note-off, mixed
  against the rest of the loop, and labelled with no voice.

[Rip design](rip-design.md) is where the rip is going from here: the loop that
refines voices and patterns against that score.
