# audio

A pure-Go synthesis, sound effect and music engine. It streams 16-bit stereo PCM
at 44.1 kHz to a player process it finds on the host, an OSS device, a WAV file,
or a writer the host supplies, and it reads what it plays from TOML documents.
Its design is [doc/audio.md](doc/audio.md).

Three words name what it plays:

- **audio** is everything: the engine, its backend, the channel mutes, the pause.
- **music** is what the sequencer plays: patterns on four crossfading slots
  (rhythm, melody, a free layer or the phrase fill, bass), arranged by group,
  tier and motif, over a harmony of root, scale and chord progression.
- **sfx**, a sound effect, is a sound played on a trigger outside the music.

A **sound** is the synthesized one-shot both draw on, layers of sources through
processor chains and buses, pre-rendered into variants; a **voice** is a tonal
instrument, an oscillator stack through a filter and an ADSR, or a string as a
delay line or as its partials; a **pattern** is steps of drum hits and notes on a
sixteenth grid, its notes scale degrees, an accidental for one off the key,
the harmony resolves at play time.

## Packages

- `audio`, the engine, imports the standard library, `github.com/lixenwraith/toml`
  and that module's `macro` expander alone. It ships no sounds, voices or
  patterns of its own: the embedder's banks are data it is handed.
- `audio/model` holds the identifiers an embedder's protocol carries, sound,
  instrument, pattern, scale and tier, with no mixer, backend or synthesis
  dependency, for builds that leave the engine out.
- `audio/ripmodel` is the rip's learned front end, a transformer
  (`github.com/lixenwraith/transformer`) over mel frames.
- `audio/bank` is the set vif and stapes ship, embedded: the sound effects, the
  drum kit, the tonal voices and the music patterns. Any other embedder may
  ignore it and hand the engine its own; the high-quality tonal voices are the
  part meant to be shared.
- [doc/audio.md](doc/audio.md): the design; [doc/term-index.md](doc/term-index.md):
  its terms; [doc/todo.md](doc/todo.md): where it may grow;
  [doc/agent-notes.md](doc/agent-notes.md): what earlier work learned.

stapes, the authoring tool, runs over this engine alone.

## Embedding

```go
sounds, err := bank.Sounds()     // or audio.LoadSoundsFS(own, "sfx.toml", "voices.toml")
patterns, err := bank.Patterns() // or audio.LoadPatternsTOML(ownMusic)

cfg := audio.DefaultAudioConfig()
cfg.Enabled = true
cfg.BaseSounds, cfg.BaseVoices, cfg.BasePatterns = sounds.Sound, sounds.Voice, patterns
cfg.SfxLevels, cfg.SoundShapes = levels, shapes      // the embedder's mix, by sound name
cfg.SoundFS, cfg.SoundFile = os.DirFS(userDir), "sounds.toml" // overrides by name
cfg.PatternTOML = userMusic
cfg.Sink = speakers // the host's own writer; else ForceBackend names one, "" detects

eng, err := audio.NewAudioEngine(cfg)
if err := eng.Start(); errors.Is(err, audio.ErrNoAudioBackend) {
	// silent, still valid: every Play is counted and dropped
}
coin := eng.SoundID("coin") // resolve once, after Start
eng.Play(coin)
eng.SetHarmony(40, audio.ScalePhrygian, nil)
eng.SetIntensity(audio.IntensityNormal, 0, true, true)
eng.StartMusic()
eng.FadeOut()
eng.Stop()
```

`Start` registers the base banks, loads the sound document with its includes,
freezes the sound registry, pre-renders every sound's variants, builds the kit
of drums and voices, registers the patterns and builds the arrangement, then
probes backends in order: `pacat`, `pw-cat`, `aplay`, `sox`, `ffplay`, `/dev/dsp`
on FreeBSD. `null` discards, `wav:<path>` captures the stream, `Sink` replaces
detection. A backend that dies fails over to the next; none left latches silence.
Control flows through one command channel into the mixer goroutine, so playback,
transport, tempo, patterns, harmony and hot reloads keep their order.

## Documents

The sound document is `[[sound]]` and `[[voice]]` tables, with `include` for
files beside it; the music document is `[[pattern]]` tables whose `role`,
`groups`, `tiers` and `motif` place a pattern in the drawn arrangement. Both may
define `[macros]` and invoke them, so a drum lane or a note line is written
once. A later definition of a name replaces the earlier in place, keeping its
runtime ID; a new voice name adds an instrument patterns name as they name any.
`LoadSoundsTOML`, `LoadSoundsFS`, `LoadPatternDefs` and `LoadPatternsTOML` read
them, rejecting unknown keys; `MarshalSoundsFile`, `MarshalSounds`,
`MarshalVoices`, `MarshalPatternDefs` and `MarshalPatterns` write them back, and
a reload plays identically.

## Live editing

`DefineSound`, `DefineVoice` and `DefinePattern` register and hot-swap while the
mixer runs: a sounding note or hit finishes on what it began with, the next
trigger takes the new definition. `RenderPreview` renders one take of a sound off
the registry, `PlayBuffer` auditions it, `RenderVariants` renders the set,
`WriteWAV` exports a buffer, `RecordMusic` taps the music bus to a file, and
`Transport`, `SlotPattern`, `MusicGroup`, `Stats` and `Rejections` report what
plays. `ResetRegistries` clears everything between a Stop and the next Start.

## Ripping a recording

`RipWAV` and `RipSamples` listen to a recording and estimate its tempo, bars,
key, each bar's chord root and, per bar, the kick, snare and hihat hits, the
melody and the bass line as steps of the grid; `Rip.Patterns` writes them as
patterns of up to four bars. `RipParams` holds every threshold and
`DefaultRipParams` the hand-set start, which the engine's own rendered music
checks (`RenderMusic`, `ExpectedRip`, `CompareRips`), and `Fidelity` grades a
render against the recording itself, both heard into a `RipSound` by
`Ripper.Sound`. The front end is a seam:
a `Ripper` hears through the package's analysis or through any `RipFrontEnd`
that fills `RipFrames` from `MelFrames`, as the `ripmodel` subpackage does with a
transformer, which also hears the voices the notes are played with
(`VoiceDef.Genes`). `ReadWAV` reads any PCM or float width to mono. The
primitives a search fits a rip with are here and the searches are not:
`RenderNote` plays a voice at a note offline and `MelDistance` compares mel
frames, `Rip.Notes` is the transcript they fit to, which `WriteMIDI` and
`ReadMIDI` carry to and from a Standard MIDI File; `BarDistance`, `Cluster` and
`Rip.Cluster` group a rip's bars within a threshold and `Rip.Song` writes the
groups as one-bar patterns and a `SongDef`, the `[[song]]` of a pattern document
that `RenderTimeline` plays offline, the patterns changed at the bar lines.

## Determinism

Every render and draw is seeded: a sound's noise realizations by its name and
variant index, so one bank member cannot perturb another; the sequencer's pool
draws and its step chances on separate streams of one seed, so a bar of timing
skew between two runs moves no later draw. With the same seed and the same
bar-aligned commands the music is the same.
