# Term index

A reverse glossary of the audio module: look up a word met in its documents or
its code to learn what it means here, where it is explained, and where it lives.
A word with several meanings lists each sense on its own line, and **bold** names
in a definition are entries of their own. Links point to the section of the
[design](audio.md) that explains the term; code locations name the package
(`audio`, `ripmodel`, `bank`) and the main identifier.

[A](#a) · [B](#b) · [C](#c) · [D](#d) · [E](#e) · [F](#f) · [G](#g) · [H](#h) · [L](#l) · [M](#m) · [O](#o) · [P](#p) · [R](#r) · [S](#s) · [T](#t) · [V](#v)

## A

**accidental** (also: `acc`, `Step.Acc`, `StepPitch`, `PitchStep`) — A pattern step's semitones off its scale degree, at most two either way, so a note outside the key is written exactly and still resolves through the harmony and follows a chord: the **rip** writes one for a note off the key it heard. [Audio §7](audio.md#7-music-sequencer) · [Audio §9](audio.md#9-ripping-a-recording) · `audio` `Step`, `StepDef`, harmony.go

**admission** (also: admit) — The mixer admits an sfx under its caps, two per sound and 12 in all, stealing a voice when full. [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` `Mixer`

**agreement** (also: `MatchNotes`, `Agreement`, two witnesses) — The comparison of two **rip**s of one piece where no score is to hand: each line's notes aligned in order by the longest common subsequence of their pitches under the transposition that matches most, so a tempo that drifts or a phrase played freer does not break the match, the melody register also as one line in case the two part its lines differently, the notes only one hears listed, and the time map the matched notes fit (`Agreement.At`). What both hear is likely; what one hears is to doubt. See also **ground truth**. [Audio §9](audio.md#9-ripping-a-recording) · `audio` match.go

**audio** — Everything the engine plays, sound effects and music together: the engine, its backend, the channel mutes and the pause. See also **sfx**, **music**, **sound**. [Audio](audio.md) · `audio`

**audio backend** (also: `BackendConfig`, `ForceBackend`, `Sink`) — The external PCM player the engine pipes samples to: `pacat`, `pw-cat`, `aplay`, `sox`, `ffplay`, or `oss` on FreeBSD, tried in that order with failover; `ForceBackend` forces one, or the synthetic `null` or `wav:<path>`, and a host's `Sink` replaces detection. [Audio §3](audio.md#3-backend-detection-and-failover) · `audio` `BackendConfig`

**audio engine** (also: AudioEngine) — The thread-safe, pure-Go control surface of the module: sound and pattern registries, backend lifecycle and failover, and an ordered command channel to the mixer goroutine. [Audio §1](audio.md#1-layered-design) · `audio` `AudioEngine`

## B

**bright** (also: `RipNote.Bright`, `RipUnmeasured`) — A **rip**'s brightness of a melody-register note: its fundamental's share of its first six partials' magnitudes over the note's frames, each partial read within 3% of its frequency under a 4096-point window and left out where a partial of a note under that window falls, so a pure tone reads one and a saw about a half, the reverse of a spectral centroid's sense; `RipUnmeasured` (-1) where the fundamental was covered throughout, and for bass notes and notes given as text. [Audio §9](audio.md#9-ripping-a-recording) · `audio` rip.go `bright`, `RipUnmeasured`

**bus** — A separate floating-point submix: the mixer renders music and sfx into their own buses, and a sound definition may route layers through named buses. [Audio §2](audio.md#2-stream-contract) · [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` `Mixer`

## C

**chain** (also: processor chain) — A sound's processor chain of filters, envelopes, shaping and gain between a source and its bus. [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio`

## D

**decay** — The `decay` stage of a sound's processor chain, an attack followed by an exponential fall with time constant `tau`. [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` sound_render.go, generator.go

**drum kit** (also: drums.toml) — The sequencer's four percussion instruments (kick, snare, hihat, clap), each bound by name to the pre-rendered **velocity layers** of a sound in the `drums.toml` bank, so a drum voice plays a cached stroke, brighter as well as louder the harder it is hit, and a user sound of the same name replaces the piece. [Audio §7](audio.md#7-music-sequencer) · [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` kit.go `kit.take`, `drumTakes`

## E

**event** (also: step) — A pattern track's event, one note trigger setting step position, velocity, scale degree, octave, **accidental**, duration and probability, at most 512 per track. [Audio §7](audio.md#7-music-sequencer) · `audio` `Step`, `StepDef`

## F

**fidelity** (also: `Fidelity`, `RipFidelity`, `RipSound`, `Ripper.Sound`) — How much a render sounds like the recording it stands for, judged on the recording's own frames rather than on its **rip**: both heard through one front end into a `RipSound` (the frames, the log-mel bands and the beats), paired bar by bar on each one's beats, the cosine of their pitch salience, of their mel bands and of their onset strength per beat averaged into a total, each one where they sound alike. [Audio §9](audio.md#9-ripping-a-recording) · `audio` fidelity.go

**fill** (also: phrase fill, SetAutoFill) — In the music sequencer, slot 2's seeded substitute pattern, drawn from fill-role patterns of the same group and tier, on the last bar of each eight-bar phrase and restored on the downbeat; it is on by default, and `SetAutoFill(false)` turns it off. [Audio §7](audio.md#7-music-sequencer) · `audio` `AudioEngine.SetAutoFill`

**frame** (also: PCM frame) — A PCM frame, one stereo pair of signed 16-bit samples (4 bytes) at 44.1 kHz, 441 per 10 ms of buffer. [Audio §2](audio.md#2-stream-contract) · `audio` `AudioBytesPerFrame`

**freeze** — The sound registry freezes its name-to-ID map at engine start, so a later file may replace a sound but not add a name. [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` `RegisterSound`

## G

**General MIDI** (also: GM, program, `midiInstruments`, `midiProgram`, `midiVoice`, drum channel, channel 10) — The standard a **MIDI file**'s instruments are named in, read and written through one table with a row per shipped instrument (`midiInstruments`): a kit piece's keys on channel 10 (kick 36 and 35, snare 38, 40 and cross-stick 37, hihat 42, 44, 46, E-GMD's edges 22 and 26, the ride 51, 59 and 53, tambourine 54 and shaker 82, clap 39), the first written; a voice's program (piano 0, bass 32, pluck 24, pad 89, lead 81) and the families of eight programs read back as it, so a program no row writes plays on its family's voice (`midiVoice`). A voice a document added is written on the program of the shipped voice nearest it, of its wave first, then by its genes (`midiProgram`). See also **MIDI file**, **voice**. [Audio §8](audio.md#8-midi-files) · `audio` midi.go

**ground truth** (also: `ParseTranscript`, `NoteLevels`, `MatchHits`, transcript corrected by hand, rhythm tab) — Notes a **rip**'s transcript is scored against: text a listener corrects by hand (a header `at N bpm` sets the step; each note a line `<line> <start>s ... <pitch> ... <length> step(s)` or `<line> <start>s <pitch> <length>s`, `ParseTranscript`), or a rhythm tapped out without pitches (`unit eighth` or `unit 203ms`, `start 0.34s`, then `melody (1-2-3---4-56---)x3 a-a-a-a-a-a-a`: a character a note a unit long, `-` lengthening the note before it, `.` a rest, `(...)x3` a repeat), matched by when the notes start, or a **MIDI file**: aligned as an **agreement** aligns two rips, the notes with pitches placed on the recording through the notes the register matched (`Agreement.At`) and read for the level the recording plays them at (`NoteLevels`), and a MIDI file's drum hits each within half a step of the rip's under the map that matches most (`MatchHits`). [Audio §9](audio.md#9-ripping-a-recording) · `audio` match.go

**group** (also: pattern group) — A pattern group, the patterns whose `groups` name it, a style anchored by its bass patterns (the shipped bank has rolling, offbeat, breaks, pump and trance); the sequencer draws rhythm, melody and bass from one group, keeps the bass through the tiers it covers, draws the member played least along the melody's motif ladder, and moves to another group after four phrases. [Audio §7](audio.md#7-music-sequencer) · `audio` `Pattern.Groups`, `RoleBass`

## H

**harmony** — In the music sequencer, the current root note, scale and chord progression through which pattern scale degrees resolve at trigger time. [Audio §7](audio.md#7-music-sequencer) · `audio` `harmony.go`

## L

**layer** (also: sound layer) — A sound layer is one source through one processor chain inside a `SoundDef`, mixed into a bus or the master. [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` `Layer`

**lead rule** (also: `LeadRule`, `Ripper.Lead`, busy, high, low, loud, soft, stream parting, `streams`) — Which of the melody register's two lines a **rip** calls the melody once their notes are parted: after the two lines are decoded together, their notes are parted again by what a line keeps, its pitch from note to note, the length of its notes, their strength and no two at once, since the decoder follows the louder line and the lead is often the softer; the lead is then the busier line by default, the one with the most notes for its sounding time, or the higher, lower, louder, softer, or longer or shorter in its notes when the listener names it. [Audio §9](audio.md#9-ripping-a-recording) · `audio` streams.go

## M

**MIDI file** (also: `ReadMIDI`, `WriteMIDI`, `MIDIScore`, `WriteSongMIDI`, `WritePatternMIDI`, `MIDIScore.Song`, `MIDIScore.Voices`, Standard MIDI File, `.mid`, score) — Patterns and songs in and out of a sequencer, and a **rip**'s transcript. `WriteSongMIDI` writes a **song** as a Standard MIDI File of type 1, a first track of its tempo, key signature and time signatures, then a track a pattern track on its **General MIDI** program, the drums on channel 10, each note its steps long at its velocity; `WritePatternMIDI` writes a pattern as a song that plays it once, a bar 16 steps, else 12 (3/4), else the pattern whole (`barSteps`). `MIDIScore.Song` reads a file back as patterns, a part a bar named as `Rip.Song` names them, and the song that plays them, a bar its signature's steps (3/4 and 6/8 twelve, 3/8 six), each note at its velocity and length, and says what it could not keep (a tempo change, a meter, a program off the table, a key the kit lacks). For the transcript: `WriteMIDI` writes a Standard MIDI File of type 1 at a tempo, a track each named melody, under and bass and the drums on the drum channel, every note and hit at its seconds within half a tick; `ReadMIDI` reads type 0 or 1 in seconds under its tempo map, a note-off ending the earliest sounding note of its channel and pitch, a track named for a line taking that line, and of the other parts (a track's notes on one channel) the lowest the bass when its notes centre under E3 (`RipBassTop`), kept whole, the rest the melody then the under in file order, any further part left out, and the drum channel's General MIDI kicks, snares and hats as hits (`RipHit`), its other strikes as onsets of no line (`MIDIScore.Struck`), a file of neither and no notes refused (`ErrMIDIUnlined`); a note read is unmeasured (`RipUnmeasured`). A file read is a **rip** too (`MIDIScore.Rip`): the notes quantized on its ticks at its first tempo, folded by octaves into the sequencer's range, four-beat bars from where its time signature starts (4/4 when it names none), the key from its notes' pitch classes (`keyOf`), a note outside E1 to C7 dropped and counted, each line on the shipped voice its program plays on (`MIDIScore.Voices`). See also **ground truth**, **General MIDI**. [Audio §8](audio.md#8-midi-files) · [Audio §9](audio.md#9-ripping-a-recording) · `audio` midi.go

**mixer** (also: `audio.Mixer`) — The single goroutine that owns every voice, the sequencer and the buses; each pass it drains commands, renders music and sfx, ducks music under sfx, applies the pause ramp and writes one PCM buffer. [Audio §2](audio.md#2-stream-contract) · [Audio §4](audio.md#4-concurrency-and-command-ownership) · `audio` `Mixer`

**modal voice** (also: partial bank, waveModal, inharmonicity, coupled pair, body) — A tonal voice that is a string as its partials: one decaying phasor per partial at `k·f0` stretched by the string's inharmonicity, each at the amplitude the strike position, the spectral tilt, the body (a third-order high-pass at `bright` kHz) and the hammer (a low-pass at `hammer_hz` times the velocity to the 1.5) leave it and each decaying faster the higher it sits, the lowest the body sounds doubled by a detuned second string whose slower aftersound makes the double decay, the two never a cancelling half cycle apart where it takes over. The piano and the bass are modal voices; the pluck is a **string voice**. [Audio §7](audio.md#7-music-sequencer) · `audio` `VoiceDef`, `TonalVoice.excite`, `TonalVoice.modal`

**motif** (also: ladder, Pattern.Motif) — The idea a melody pattern is one weight of: patterns of one group sharing a `motif` form a ladder from calm to peak, and a tier change draws the playing motif's member at the new tier while a phrase variation draws another motif's, after the melody rests through the fill bar. Every shipped ladder covers all five tiers, so a tier change never leaves its idea. [Audio §7](audio.md#7-music-sequencer) · `audio` `Pattern.Motif`, `Sequencer.draw`, `Sequencer.planPhrase`

**music** — What the sequencer plays: patterns (beats, melodies, basses and fills) on the music bus, arranged by group, tier and motif and ducked under sounding sfx. See also **sequencer**, **pattern**, **sfx**. [Audio §7](audio.md#7-music-sequencer) · `audio` `Sequencer`

**music recording** (also: `RecordMusic`) — The music bus alone, before ducking, written to a WAV file as it plays; muted or paused stretches add nothing, and it needs a running mixer. [Audio §3](audio.md#3-backend-detection-and-failover) · `audio` `RecordMusic`, wav.go

**mute** (also: `SetSfxMuted`, `SetMusicMuted`, `SetSlotMuted`) — Independent sfx and music mute bits; a sequencer slot also mutes alone, its pattern playing on in time so it comes back on the step it has reached, and so do tracks of the pattern it plays, through the slot's **track mask**. [Audio §6](audio.md#6-mixer-buses-mute-and-pause) · `audio` `SetSlotMuted`, `SetTrackMask`

## O

**output limiter** (also: LimiterCeiling, LimiterLookahead) — The mixer's last stage: it follows the peak of the summed buses 64 samples ahead of what it writes and ramps its gain down over that time, holding the output under 0.9 so stacked kicks duck without the step a gain change inside one sample would put on them. [Audio §2](audio.md#2-stream-contract) · `audio` `Mixer.renderTick`

## P

**pattern** — A step grid of up to 64 steps and 32 tracks with a role, groups and a tier bitmask that place it in the drawn arrangement, and a motif naming the idea it is one weight of, defined in a pattern document such as `bank/music.toml`. [Audio §7](audio.md#7-music-sequencer) · `audio` `Pattern`, `Pattern.Motif`

**pause** — A mixer output state that ramps master gain over 250 ms and freezes music in place for resume. [Audio §6](audio.md#6-mixer-buses-mute-and-pause) · `audio` `Mixer`

**pickup bar** — The bar a **rip** opens before the first full one when a note or a hit sounds more than half a step before its line, its beats extrapolated a beat at a time, into the time before the recording when it begins inside that bar (`Rip.Downbeat` is negative then), so a phrase that leads into the first bar is kept; `CompareRips` skips it against a truth without one. [Audio §9](audio.md#9-ripping-a-recording) · `audio` rip.go `pickup`

**pool** — The sequencer builds each group's pool of patterns per tier and role, from which it draws. [Audio §7](audio.md#7-music-sequencer) · `audio`

## R

**registry** — The sound and pattern name-to-ID registry, frozen at startup. [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` `sound_registry.go`

**rest bar** — The last bar of a phrase whose downbeat moves the melody to another motif or every slot to another group: the melody fades out under the fill and the new idea enters alone on the one; a step along the melody's own ladder needs none. See also **motif**, **fill**. [Audio §7](audio.md#7-music-sequencer) · `audio` `Sequencer.planPhrase`

**reveal** — A music build-up on a rising tier: the incoming pattern starts from as many tracks as the outgoing one sounded and adds one a bar, so a build-up never starts thinner than what it replaces. [Audio §7](audio.md#7-music-sequencer) · `audio` `sequencer.go`

**rip** (also: `Rip`, `RipWAV`, `RipSamples`, `RipParams`) — The engine's estimate of a recording, or a **MIDI file**'s score read the same way (`MIDIScore.Rip`), as what the sequencer would play it with: its tempo and bars, its key and each bar's chord root, and per bar the kick, snare and hihat hits, the melody and the bass line as steps of the sixteenth grid, written by `Patterns` as patterns of up to four bars. `RipParams` holds its thresholds, each in a range and a gene vector, `DefaultRipParams` the hand-set start and a `[rip]` table a tuned set (`LoadRipParams`); the engine grades one by ripping music it renders of known patterns (`RenderMusic`, `ExpectedRip`, `CompareRips`). Its front end is a seam (`RipFrontEnd`, `RipFrames`): the built-in analysis or **ripmodel**. It keeps its transcript (`Rip.Notes`, which `WriteMIDI` writes as a **MIDI file**, each melody-register note with its brightness, **bright**), which a voice is fitted to by the engine's own renders (`RenderNote`, `MelDistance`), and its bars compress into a dictionary of one-bar patterns and a **song** (`BarDistance`, `Cluster`, `Rip.Song`); **fidelity** grades a render beside the recording through one front end. The melody register is decoded as two lines at once, each its own continuity and the first the louder by the `cross` cost, the second a line of its own (`RipBar.Under`). The front end reads the recording's **tuning** first (`Rip.Tuning`), a **pickup bar** opens before the first full bar when sound leads into it, and a note off the key keeps its pitch as an **accidental** (`Step.Acc`). See also **pattern**, **ripmodel**, **song**. [Audio §9](audio.md#9-ripping-a-recording) · `audio` rip.go, song.go, dsp.go

**ripmodel** (also: RipModel, `ripmodel`, rip model, front-end model) — The rip's learned front end: a transformer over the log-mel frames `MelFrames` makes, each band read in dB less the frame's mean and the mean as one more input (`features`, the trainer's `ripmodel.features`), with heads for the onset, the three drums, the sixty-nine pitches, voicing, the wave and the voice genes of the note under each frame, each pitch let go and how hard each was struck (`HeadRelease`, `HeadVelocity`, `RipFrames.Release`, `Velocity`, a note ending at its release), and each pitch struck (`HeadStrike`, `RipFrames.Strike`, from which the melody register's notes are read, any number at once, a strike that never sounds, a stronger one's partial in the register or one within `StrikeGap` of the pitch's last starting none, and one under `StrikeAgain` or under a neighbour's leaving a sounding note ringing), read into the frames the rip's decoders take; `Spec` its default shape, which a weight file may change but for the inputs and heads, and `Load` reads a weight file into a `FrontEnd`. See also **rip**, **transformer**, **voice**. [Audio §9](audio.md#9-ripping-a-recording) · `ripmodel`

**role** — A pattern's role, rhythm, melody, fill or bass, names the sequencer slot it plays in and, with groups and tiers, where it can be drawn. [Audio §7](audio.md#7-music-sequencer) · `audio` `Role`

## S

**scale** (also: `ScaleID`) — The musical scale the harmony resolves pattern degrees through (minor, harmonic minor, Phrygian, Dorian, minor pentatonic, major), Phrygian by default. [Audio §7](audio.md#7-music-sequencer) · `audio` `ScaleID`

**sequencer** (also: music sequencer) — The music pattern player: a 16th-note grid in 4/4 bars with four crossfading slots (rhythm, melody, free or phrase fill, bass), tempo clamped to 80–200 BPM, pattern transitions immediate or quantized to the bar, and drawn music chosen by group and tier. [Audio §7](audio.md#7-music-sequencer) · `audio` `Sequencer`

**sfx** (also: sound effect, `SfxVolume`, `SfxLevels`) — A sound played on a trigger outside the music, through the sfx bus: `AudioEngine.Play` takes its sound's ID, under two per sound and 12 in all, rapid-fire dampening and the sfx volume, and the music ducks while it sounds; the embedder gives each its level and shape by name. See also **sound**, **music**, **audio**. [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` `AudioEngine.Play`, `AudioConfig.SfxLevels`

**slot** (also: music slot) — A music slot is one of the sequencer's four crossfading layers: 0 rhythm, 1 melody, 2 free or phrase fill, 3 bass, the group's anchor; each holds three players so a transition mid-fade cuts no tail. [Audio §7](audio.md#7-music-sequencer) · `audio` `MusicSlots`, `slotPlayers`

**song** (also: SongDef, `[[song]]`, timeline, dictionary) — A timeline as data: the patterns each slot plays bar by bar at a tempo, in a key and at a **tuning**, a `[[song]]` of the pattern document beside the patterns it names (`LoadPatternFile`, `MarshalPatternFile`, `ValidateSong`), resolved to pattern IDs (`Timeline`) that `RenderTimeline` plays offline, the patterns changed at the bar lines, or one line of it alone (`RenderLines`). `MIDIScore.Song` reads one from a **MIDI file** with its patterns, and `Rip.Song` writes one from a **rip** with its dictionary, the one-bar patterns `<name>_<part>_a`, `_b`, ... that bars within a threshold of one another share. See also **rip**, **pattern**. [Audio §9](audio.md#9-ripping-a-recording) · `audio` `SongDef`, `Cluster`, `RenderTimeline`

**sound** (also: SoundDef, sound ID, SoundShape) — A synthesized one-shot: layers of sources through processor chains and optional buses, validated, registered by name and pre-rendered into variants under the embedder's shape; an sfx plays one, as does each drum of the kit, and a later document's sound of the same name replaces it. See also **sfx**, **drum kit**, **registry**. [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` `SoundDef` `SoundID`

**stream contract** (also: stream) — The stream contract is the PCM output every backend writes: 44.1 kHz stereo signed 16-bit, in 50 ms buffers by default. [Audio §2](audio.md#2-stream-contract) · `audio` `AudioSampleRate`

**string voice** (also: waveString, Karplus-Strong, Jaffe-Smith, pluck, ring) — A tonal voice that is a plucked string as a delay line: a seeded noise burst through the comb of where it is plucked and a velocity's low-pass, rung in a line of the period's whole samples and an all-pass for its fraction, its loop's two-point low-pass and loss per period setting the fundamental's T60 to `ring` seconds at middle C, shorter above and longer below by `decay_track` as a **modal voice**'s decay is. Like a **modal voice** it is never held at a sustain: it fades as it rings, and the note-off damps it. The pluck is one. [Audio §7](audio.md#7-music-sequencer) · `audio` `TonalVoice.strike`, `TonalVoice.pluck`

## T

**track mask** (also: `SetTrackMask`) — A slot's mask of the tracks it sounds of the pattern it names, whenever that pattern begins in the slot, which the **reveal** also drives. See also **mute**. [Audio §6](audio.md#6-mixer-buses-mute-and-pause) · `audio` `SetTrackMask`

**transformer** (also: `lixenwraith/transformer`, weight file) — Inference over sequences of frames in pure Go, float32, the module's one dependency beside `lixenwraith/toml`: **ripmodel** runs on it, and `Load` reads the weight file a trainer writes. [Weight file](https://github.com/lixenwraith/transformer/blob/main/doc/format.md) · `lixenwraith/transformer`

**tuning** (also: `Rip.Tuning`, `RipFrames.Tuning`, `SongDef.Tuning`, `SetTuning`, cents) — The cents a recording's pitches sit off equal temperament at A440, read by the **rip**'s front end before anything else: over windows four times the frame, every spectral peak votes its offset from the nearest semitone with its height, and the pitch table is read on the mean offset, so a clip forty cents flat reads as one in tune instead of wobbling between two rows. The engine plays and renders at a tuning too (`AudioEngine.SetTuning`, within a semitone of A440): a **song** made of a rip carries the recording's and plays there, and a voice fit renders its candidate notes there (`RenderNoteAt`). [Audio §9](audio.md#9-ripping-a-recording) · `audio` rip.go `tuning`

## V

**variant** (also: sound variant) — A sound variant is one of a sound's pre-rendered renderings, normally three, which the mixer rotates to reduce repetition; a drum's variants are its strokes, one set a **velocity layer**. [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` `RenderVariants`

**velocity layer** (also: `velocities`, `vel_gain`, `vel_track`, stroke) — A sound's variant set rendered at one of the velocities its spec lists, each part following the velocity by its power (`vel_gain` for a layer's gain, `vel_track` for a start frequency, a filter's frequency, a decay's tau or a shaper's drive), each variant a stroke landing at its own place in [-1, 1] that `stroke` scales by; the **drum kit** plays the layer nearest a hit's velocity. [Audio §5](audio.md#5-sounds-and-sound-effects) · `audio` `SoundDef.Velocities`, `renderTakes`, `variance.respond`

**vibrato** (also: `vibrato_hz`, `vibrato_cents`, `vibrato_delay`, **vowel**, formant, `vowel`) — Two voice-like traits of an oscillator **voice**, neither a gene: a vibrato, a sine on its pitch of `vibrato_cents` either way at `vibrato_hz`, coming in over `vibrato_delay` seconds from the note's start, as a singer's or a bowed string's does; and a vowel, a, e, i, o or u, a bank of three resonances at the vowel's formant frequencies the oscillator waves pass through before the low-pass, so a sung line or a choir pad is a line a **rip** corpus can hold. A string and a modal voice hold their pitch and keep their own spectrum. [Audio §7](audio.md#7-music-sequencer) · `audio` `VoiceDef`, `TonalVoice.Sample`, `bandpass`

**voice** (also: VoiceDef, `[[voice]]`, voices.toml, tonal voice) — A tonal instrument as data: a `[[voice]]` of the sound document naming an oscillator stack, with a **vibrato** and a vowel's formants if it has them, through a resonant low-pass under a filter envelope into an ADSR, or a string as a delay line or as its partials; an oscillator's unison starts at seeded phases and drifts. The five `bank` ships are its `voices.toml`; a document's voice of a shipped name retunes that instrument in place, a new name adds an instrument patterns name as any, and `DefineVoice` hot-swaps one, the next note taking it while a sounding one keeps its own. A voice is also a gene vector (`Genes`, `VoiceFromGenes`: thirty-two parameters in [0, 1] and a wave), the form a **rip** fits one in by the engine's renders of a recording's notes (`RenderNote`, `MelDistance`) and a trained front end hears one in. See also **sound**, **pattern**, **rip**. [Audio §7](audio.md#7-music-sequencer) · [Audio §9](audio.md#9-ripping-a-recording) · `audio` `VoiceDef` `RegisterVoice` `DefineVoice`; `bank` voices.toml
