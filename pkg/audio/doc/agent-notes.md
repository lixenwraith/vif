# Agent Notes

What agents working on this repository learned that no rule in
[AGENTS.md](../AGENTS.md) and nothing in the [README](../README.md) or
[audio.md](audio.md) states: facts that cost a session an hour to rediscover,
and how to check them. Read it before starting. When a task teaches you one, add
it in the same PR, one bullet under its topic; delete a note that proves wrong or
that a document now states, and link the document instead.

## Toolchain

- The `gofmt` on a container's `PATH` can be older than the module's Go; run
  `"$(go env GOROOT)/bin/gofmt" -l`.

## Tests

- A test that needs voices registers stand-ins: the root package's tests run
  with an empty voice registry (`VoiceDefByName` is nil until an embedder hands
  the engine the bank's). `ResetRegistries` in a cleanup.
- The mixer drops ticks under load, so audio time lags the wall clock and one tick
  can take 200 ms under `-race`: a test waiting on mixer state gives it seconds,
  not periods. Force it with a sleep at the top of `Mixer.renderTick`.
- A labelled corpus for the rip renders offline with no device: register a bank and
  voices, `freezeSounds`, `newSoundCache().preloadAll(nil)`, `buildKit`,
  `NewSequencer(bpm, kit)` with patterns placed in its slots, then `Generate` over
  whole bars (`rip_test.go`'s `renderLoop`). The music fades in over
  `MusicStartFadeIn`, so the first bar is quiet and a recall test skips it.
- `melody_gen` is no fixed pattern: every sequencer that plays it rewrites the
  registered lead in place from its second bar on, so an offline render's first
  bar replays the last render's last bar and a label read off the pattern after
  the render gives every bar the last one's notes. Stapes' corpus exported
  whole and in `part`s must match byte for byte; that comparison found it, 43
  loops in 9468.

## Voices and strings

- A string or modal voice ignores the ADSR's decay and sustain, yet the shipped
  ones keep `sustain = 1`: stapes' augmented corpus swaps a voice's wave a third of
  the time and keeps its genes, and an unset sustain reads as the gene's floor,
  so the swapped oscillator would fall silent within 5 ms.
- Levels re a note's peak and a mel distance are both blind to how loud a voice
  is: a bass fit went to a 50 Hz cutoff at Q 0.1, its notes 20 dB down, and
  scored best. Hold a fit's first half second RMS to main's (`RenderNote` over
  a few notes and velocities) before shipping it.
- A modal voice's two strings cancel a partial when the beat leaves them half a
  cycle apart where the slower overtakes the faster: with the piano's genes
  that took the fundamental out of B4 to E5 within half a second, and a refit
  moved the band (D5 to A5) rather than closing it. `excite` now holds the
  hand-over to a quarter cycle. Iowa's notes have such dips (C5 -29 dB at 0.5
  s), one note at a time, never a band.
- Read a decay after a high-pass: a delay line's burst holds a mean that rings
  at the loop's loss alone, so the old string's RMS read a 1.45 s T60 at C7
  where its fundamental fell 60 dB in 0.24 s.
- `PatternPlayer.TriggerNoteMIDI` stamps a voice's serial after `Trigger`, so a
  note seeded by its serial in `Trigger` reads the slot's previous note's:
  still a replay's same, and different for two notes of one pitch.
- Audacity's Pluck is Nyquist's `snd-pluck` (`tran/pluck.alg`, after M4C's
  Pluck.t). GitHub outside the session's repositories is refused; the
  SourceForge Nyquist tree and `sources.debian.org` serve it.
- **A voice-like trait that is not a gene rides beside the genes, not in them.**
  Vibrato and a vowel's formants went on `VoiceDef` as plain fields, kept by
  the augmentation after `VoiceFromGenes`, rather than as new genes: a gene
  changes the model's voice head and every shipped weight file's shape, and the
  fit would search a space the recording rarely needs. Add a gene when a corpus
  with the trait has trained and the fit wants it, not before.

## Recordings to measure against

- Real notes to measure a voice against reach a cloud container: the University
  of Iowa's MIS samples (`theremin.music.uiowa.edu`, free for any use) hold one
  piano note a file at pp, mf and ff, and runs of guitar and pizzicato double
  bass; `ffmpeg -ac 1 -c:a pcm_f32le` makes the WAV `ReadWAV` takes. Their mf
  piano falls 15 to 25 dB in a note's first second from C3 up.
- Iowa's pizzicato double bass comes as runs, one string a file, edited to
  digital silence between notes: cut a note at an onset whose peak is within
  22 dB of the run's (fainter ones are the string's buzz) and end it at its
  first 50 ms under an RMS of 1e-5, or its level reads -240 dB at 3 s.
- Recorded drum one-shots free for any use reach a cloud container: Freesound's
  sound pages serve an HQ MP3 preview without a login (`cdn.freesound.org/previews/
  .../<id>_<user>-hq.mp3`), each page naming its licence by its "Go to the full
  license text" link (filter searches with `f=license:"Creative Commons 0"`); the
  CC0 Sonor kick set holds one head at six pressures, and Iowa's MIS has a hihat
  at pp, mf and ff by tip and shoulder. Previews stop near 16 kHz: read every
  figure under it, the kit's too.
- A spectrum of a one-shot under a Hann window reads its attack at nothing: the
  window is near zero where the hit starts, so a click or a stick vanished from
  the band shares. Start the segment a millisecond before the onset and taper
  only its end. Weigh a stroke-to-stroke spread by each band's energy, or a short
  noise's quiet bands differ by 10 dB between two takes alike to the ear.

## MIDI

- A MIDI note-off names no note: `ReadMIDI` ends the earliest sounding note of
  its channel and pitch, so a note nested inside a longer one of its pitch reads
  back with the two ends exchanged. A rip's line never holds such a pair; a file
  from a sequencer can. `pip install --user mido` reads a written file for a
  check from outside the package (`mido.MidiFile(path).tracks`).
- A score's notes quantize on its ticks, not its seconds: the grid's `place`
  sizes a note by the mean step over every bar, so under a tempo change a
  quarter in seconds read as eleven steps where the file holds eight.
  `MIDIScore.Rip` converts seconds back to ticks (`ticks`) for the grid and the
  beats to seconds after it.
- The MIDI writers cut a note where its key sounds again in the same track
  (`WriteSongMIDI`'s `held`), since the reader pairs a note-off with the earliest
  note: a long pattern note under a short one of its pitch would read back with
  the lengths exchanged. Changing the reader to pair the latest instead would
  move every transcript read so far (stapes' `truth` and `corpus <dir> from`).
- `ReadMIDI`'s hits are the kick, snare and hihat only (`midiDrum(key,
  InstrClap)`), the drum channel's other keys `Struck`; `MIDIScore.Song` reads
  the clap too (`InstrClap+1`). A clap in a test file is a hit for a song, not
  for a transcript. `firstBar` reads a signature that holds exactly one bar
  before the next as a pickup, so a 3/4 bar ahead of 6/8 gives `Meter` 6/8.

## The analysis

- The rip's spectrogram frames are centred, so every onset reads about two frames
  (23 ms) early in every band alike; the beat grid and the events share the bias,
  and the sixteenth quantization cancels it. Do not correct one without the other.
- The default drum rules were set from the band shares a scratch test printed for
  the rendered loop's hits (a kick's rise under 100 Hz was 0.09 to 0.19 of its
  rise, a hat's above 5 kHz 0.45, a snare's noise above 5 kHz 0.15 to 0.21):
  print them again before moving a threshold, and expect a bass note's attack to
  read as a kick.
- A long analysis window reads an onset early by about a quarter of the
  window: the bass's notes from the 4×2048 window started a step before the
  grid said, and lagging its rows made them start late instead. Multiply the
  long window's salience by the short window's (their geometric mean) so the
  short one keeps the timing and the long one the pitch; and give a pinned
  grid the two-frame lead the beat tracker already has, or a note on the beat
  lands on the step before.
- A bass that wobbles between two semitones leaks its octave into the melody
  register on the frames the cancellation picks the other bin: cancel the
  partials of the strongest bass pitch and of a neighbour within two semitones
  at seventy per cent of it, and the ghosts go while a clean render's melody
  keeps its score.
- A sustained pad changes the key the chroma votes for, and a note the key
  lacks snaps to its neighbour: a melody score that falls when a pad is added
  may be the key, not the pitch paths. Read the rip's root before blaming the
  tracker.
- A second voice in a register is found by masking the first path's notes with
  their partials (the octave, the twelfth, the double octave and the seventeenth
  and nineteenth, a semitone either side) before the path runs again; masking
  the fundamental alone finds the octave ghost. The gate is relative to the first
  path's mean note strength, so a clean render's second pass yields nothing.
- A recording need not sit at A440: the sample is forty cents flat, which put
  every pitch between two salience rows and read as semitone wobble (A#2 and
  A2 taking turns), chopped sustains and a wrong scale. Measure the tuning (a
  histogram of spectral peaks' offsets from the nearest semitone, what
  `tuning` does) before blaming the tracker; the front end reads it first now,
  and since a render is in tune, a bench of renders cannot see this failure.
- The first full bar is not where the music starts: the grid dropped the
  sample's opening phrase, two and a half seconds before its first bar line.
  A pickup bar is extrapolated when a note or a hit leads in by more than half
  a step; a render's silence before bar 0 opens none, so the corpus reads the
  same. `Downbeat` is negative then, `progression` reads no frame before zero,
  and `CompareRips` skips the pickup bar against a truth without one.
- The pitch paths start late against the onsets the beats sit on, a sixth of a
  step on the sample: the median phase of the notes is taken out before
  quantizing, which lifted the test loop's melody from 0.95 to 0.98 and a cut
  recording's from 0.93 to 0.98. Under that sharper grid the second pass's
  fragments cost the pad test's melody two hundredths against the solo pass,
  so that guard allows five.
- The first and second pitch paths swap roles on a held dyad: the louder note
  changes hands from frame to frame, so each line hears a trill where the
  music holds a chord, and two fitted voices play it as two instruments
  alternating. Assign concurrent notes to lines by pitch order (`uncross`).
- The sequencer's range is 80 to 200 bpm and the tempo decoder folds into it,
  so the origin recording, near 74 bpm, reads at 148 while the intro, sped up
  to 86, reads at 86: the same piece on two metrical levels, a sixteenth 101
  ms in one rip and 174 in the other. A `Grid` with a negative downbeat pins the
  tempo alone, so the two rips can be put on one level.
- `RipNote.Bright` on the test loop's lead through its 2.5 kHz low-pass reads
  0.97 on a sine, 0.73 on a square and 0.49 on a saw (an unfiltered saw's six
  partials give 0.41). A melody note at the bass's octave, twelfth or double
  octave is `RipUnmeasured`: the loop's root and fifth over the bass's leave a
  quarter of its melody-register notes so. A concurrent note counts on a frame
  while it sounds anywhere under the window (eight frames either side); counted
  only on its own frames, the edge frames read the fifth's fundamental through
  the bass's partial and gave it a value up to 20% low.
- A `Ripper` of zero parameters hangs: zero costs let the pitch path jump every
  frame and the tempo prior degenerate. `Samples` and `Hear` take the defaults
  for the zero value now; a scratch tool that forgot `DefaultRipParams` ran
  for ten minutes on a twelve-second clip.

## Front ends and fidelity

- The rip's front end is a seam: `analysis` and `ripmodel` both fill `RipFrames`,
  and the decoders read nothing else, so a front end is checked by the frames it
  makes and the rip they give, as `ripmodel`'s test does with an untrained model.
- A head trained with a positive weight w (`bce(..., pos_weight)` in
  `train.py`) scores a posterior p as wp/(wp + 1 - p): the strike head's
  (w 20) half-sure is 0.95 and the pitch head's (w 12) 0.92. Sweep a
  decoder's threshold on that scale against the truths; 0.5 floods.
- Under a pedal the model's pitch output sits at its ceiling for every ringing
  note, and it rises a few frames before a strike: a "was it sounding" test on
  the frames before a strike reads nearly every note as ringing. Ask the
  decoder's own state (a note still open) instead, and print a window's strikes
  and salience per pitch from a scratch test before moving a strike rule.
- The rip-back cannot judge the rip's own errors: a note misheard in the
  recording is misheard the same way in the render, so a search on it polishes
  the wrong transcript. `Fidelity` compares the render's frames with the
  recording's through one front end; a render a semitone sharp scores 0.56 in
  pitch against 1.0 for itself, with its onsets still at 1.0.
- A render at A440 beside a recording forty cents flat is heard as the same
  notes sharpened, "a stretched string", before any note is judged: the ear
  reads a tuning offset as timbre. Render, play and fit at the recording's
  tuning (`SongDef.Tuning`, `SetTuning`, `RenderNoteAt`); the rip reads it.

## Decoders

- **Fix the cut, not the labels.** A stream assignment over the notes two
  passes had already cut, by pitch, timbre and level, made the two recordings
  of one piece agree less, not more: the first pass had cut the louder pitch
  of each frame into notes that belonged to neither line, and no relabelling
  mends a note cut across two lines. Decoding the two lines together, where
  the cut is made, did. Try the change at the stage the error is made.
- **A joint decoder is cheap when its transition cost is separable.** Pairs
  of forty-five states are two thousand; a step over them is two sweeps of a
  one-line distance transform, since the jump cost is linear in semitones, not
  a product over pairs, and backpointers kept a block at a time from
  checkpoints of the scores hold a twenty-minute recording in a few megabytes.
- **Weigh a cue on a scale you chose.** The first parting of the two lines
  followed loudness alone, whatever the other weights said, because a note's
  strength was the raw salience, a hundred times the other terms; against the
  register's reference it took its intended share. Check the magnitude of each
  term on real data before trusting a weighted sum.
- **The louder line is not the lead.** On the origin recording the lead the
  user hears is the softer line, short notes at a steady cadence, under a
  counter line of held notes that the decoder followed as its first line and
  that the harmonic profile could not tell apart from the lead. Which line is
  the melody is a listener's call with a default, not a rule the signal
  settles; make it a word the listener can give.
- **A register boundary cuts a line that crosses it.** Transposed three
  semitones, the origin's bass D#3 became the intro's F#3, above the bass
  register's top, and surfaced as a low note of the melody with no bass note
  under it, which the stream parting then had to place. Let a decoder reach
  past its register's rows and settle the overlap by a rule.
- **Measure the lines, not the register.** The two-witness script matched any
  note of the same pitch within a window, whatever line held it, so it could
  not see the lines change hands; the agreement per line was an illusion.
  `MatchNotes` matches within a line and reports the register as one line
  beside it, so the two numbers show the parting's own error.
- **A rule that fits a synthetic case can be refuted by one recording.** A
  note cut where its salience rose again by a third after falling off its
  peak heard thirty-two of thirty-two repeated piano strikes in a test and,
  on the recording, cut eleven of the lead's twenty-five plucked notes in two
  and the bass's held notes too, since those strings beat. The rhythm the
  user tapped out showed it in one run; the bench and the suite had not.
  Measure a decoder change against the recording's own truth before it
  ships, and keep the test only when the rule stays.
