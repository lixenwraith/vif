# Agent Notes

What agents working on stapes learned that no rule in [AGENTS.md](../AGENTS.md)
and nothing in the [README](../README.md), [stapes.md](stapes.md) or
[rip-design.md](rip-design.md) states: facts that cost a session an hour to
rediscover, and how to check them. Read it before starting. When a task teaches
you one, add it in the same PR, one bullet under its topic; delete a note that
proves wrong or that a document now states, and link the document instead.

## Toolchain

- The `gofmt` on a container's `PATH` can be older than the module's Go and reject
  generic methods ("method must have no type parameters"); run
  `"$(go env GOROOT)/bin/gofmt" -l`.
- The engine is `github.com/lixenwraith/audio` and the searches' engine
  `github.com/lixenwraith/genetic`: a change to either lands there, under its own
  `AGENTS.md` and `doc/agent-notes.md`, and stapes takes its tag.
- `lixenwraith/transformer` and `tool/rip-train` agree to float32 rounding: redo
  the check its [agent notes](https://github.com/lixenwraith/transformer/blob/main/doc/agent-notes.md#the-float32-check)
  give before trusting a trained file after any change to either side. A corpus
  loop's frames come from `stapes -s` over a script of `corpus <dir> 2 4`,
  about a minute; `corpus 8 4` plus sixty training steps of a width-32 model
  also take under a minute, enough to exercise the whole training path.

## Tests that flake

- A test waiting on mixer state gives it seconds, since the mixer drops ticks
  under load ([audio notes](https://github.com/lixenwraith/audio/blob/main/doc/agent-notes.md#tests)): load
  the root package with ~24 race processes pinned to one CPU (`taskset -c 0`)
  to see one time out.

## Verbs, words and documents

- `slot` is asynchronous: the mixer applies the pattern a tick later and
  `SlotPattern` reads silence until then, so a verb that acts on what was just
  slotted reads the session's own `slots`, as `render` does; a test that checks
  the engine's readout waits a tick.
- The searches stapes runs are sized by fields a test shrinks
  (`Session.fit`, `Session.compressOpt`), not by constants: the rip test fits two
  notes over one generation of four and still walks every path. Seeds past the
  pool size are dropped by the engine without a word, so a pool smaller than the
  registered voices fits from the first few only.
- A command line splits on blanks (`splitArgs`); a path with a space travels in
  double quotes, which the files overlay adds. Quotes at an argument's start are
  grammar now, so a `set` value in quotes loses them.
- A recipe's relative paths were the process's: `run /elsewhere/play.slab`
  failed to find the documents beside it, and an installed stapes has no
  working directory worth the name. A running script's relative paths are
  read from its own directory (`Session.base`, `RunScriptFile`), nested runs
  from theirs, and the scratch recipes name their files from where they lie.
- `-config-dir` names the root alone: the search path still holds vif's audio
  folder under `os.UserConfigDir()`, so a run that must play the bank alone, a
  corpus or a bench to compare, points `XDG_CONFIG_HOME` at an empty directory
  too.
- **A word is a path by its dot, never by its digits.** `badName` refuses
  dots, so a dotted word is always a path; a bare number is a valid entry name
  (`cp p.track.0 5` makes the pattern `5`). An index argument goes only where no
  name can stand: `mv`'s second word after a dotted first, `add`'s and `dup`'s
  optional second, never a last word `cp` reads as a new name.
- **A document changes only through its methods.** `put`, `del`, `rename`,
  `markDirty` and `replaceAll` record an entry for undo from the document's
  shadow before they change it, since a command mutates an entry in place and
  marks it after. A write to `byName` or `order` that skips them is one undo
  cannot see, and it then restores a stale order.

## Songs and the TUI

- A song plays through the live sequencer by scheduling each bar's patterns
  quantized while the bar before plays (`conduct`), never by setting them on the
  tick that sees the bar line, which lands them a tick late and loses the
  downbeat's hits. The melody slot's boundary is its pattern's cycle, so a
  four-bar pattern there holds the change for up to four bars; the dictionary's
  one-bar patterns change every bar. A front end that ticks sets `Session.ticked`
  and calls `conduct`; without one `song play` conducts itself until the song
  ends, with a deadline past its length in case the transport never moves.
- A verb that takes seconds freezes the TUI if it runs in the event loop: the
  first rip from the files overlay read as a crash. `longVerbs` run on a
  goroutine behind a working box; while one runs the screen draws the last frame
  and the log's tail only, so nothing reads the session the goroutine owns, and
  `LogBuffer` takes a lock because both sides write and read it. A test of such
  a verb ticks (`KeyNone`) until `busy` clears.

## The rip and its truths

- Write a truth from a score or a MIDI file, never from memory: a remembered
  Billie Jean riff had its first note an octave low, the synthesizer's doubling
  the analysis hears, and turned a 96-to-94 tie into a 110-to-84 loss.
- Score the three recordings pooled (every note once) beside their mean: the
  origin and the intro have 83 notes each, where five notes move F1 by 0.06.
- The test loop scores 0.98 under `DefaultRipParams` and loops of the shipped bank
  0.51 (`CompareRips.Total`): busy drum lanes whose chance hits count as misses,
  several tracks a slot and melodies under chord changes are where the model falls
  short, not the thresholds. `tune 4` over 12 four-bar loops took 36 s on four
  cores and reached 0.59; a set tuned on renders alone pushes `tightness` and
  `kick_share` to their range's ends, which a recording with tempo drift would
  pay for, so the defaults stay hand-set until the corpus has real material.
- Two transcripts of one piece are a check no single rip gives: the sample and
  its origin (three semitones apart, one stretched, on different metrical
  levels) agree on 41 of 49 melody notes, 27 of 29 under notes and all the
  bass after transposition and a fitted time map (a scratch comparison over
  two `notes` listings), so the pitch content is mostly consistent and what
  the ear misses is elsewhere: the two lines swapping a held dyad between them,
  and durations. Ask for the melody as notes and for a solo take of the lead;
  they are the ground truth the numbers lack.
- The sweep's moves were all relative to the note the rip heard, so a note it
  missed had no way in and one wrong by a third no way out. The recording's
  own strongest pitch over a step (its salience rows, which the fidelity's
  sound already holds) is the proposal: tried as a move on a step that has a
  note and as an insertion on one that has none, kept when the bar gains.
- A search over every step's genes at once (312 genes, six generations of
  twelve) kept no move on the sample: one note's move changes one frame in a
  thousand of the fidelity, under the noise of a dozen genes moving with it.
  A sweep that tries each step's five moves alone and keeps a gain sees it;
  five renders a step is the price, minutes a round on a four-bar song.
- **A score of the mix drifts the parts.** Fidelity grades the whole render,
  so a voice that fills the spectrum scores above the voice that sounds like
  its line, and `refine` turned every voice electronic while the number rose.
  Where a part has a measure of its own, the fit's distance from the
  recording's notes, hold the part to it and let the whole's score choose
  among what passes.
- The dictionary search (`dictionary.go`) rarely beats the greedy grouping it
  is seeded with under a Jaccard distance, which is expected, not a bug: the
  size term and the greedy pass apply the same pressure.

## Corpus and bench

- The user judges the corpus by ear, as music: measure what they hear before
  changing the generator. The bank's drums and bass put 82 to 89% of a loop's
  energy under 250 Hz (Für Elise and the sample 7 to 14%, Billie Jean 76%), a
  white floor a recording's mix would cover sits bare over a synthesizer's empty
  treble (a recording's hiss is about 39 dB under its median frame), and a
  reverb after the clip passes full scale.
- `bench` and `tune` loops shorter than four seconds give no rip (a two-bar loop
  at 176 bpm is 2.7 s): size a test's loops by bars, not loops, and read the
  count `bench` prints; `bench ... verbose` names each loop that gave none.
- An augmented corpus draws its room, mix and voices from a stream seeded apart
  from the loop's patterns and key, so `corpus` with and without `aug` renders
  the same music; a test comparing the two relies on it. Generated parts register
  under `corpus_*` names replaced in place each loop, and `corpus` leaves those
  names out of the bank's pools, or the next draw would include the last loop's
  generated patterns and no two runs would draw alike.
- `bench` grades the analysis as it is today, so a run's numbers compare only
  with the analysis benched beside them: the seventh pass moved the analysis
  from 0.47 to 0.49 on clean loops, and the fourth model run's 0.45 and 0.51
  stand against that, not against the third run's table. The model's corpus
  is rendered at A440, so nothing it saw was detuned; a recording off the
  standard is a recording it was never shown, whatever its held-out loss.
- A `corpus <dir> from` loop is known by its `source`, the CSV's
  `audio_filename`: the set may move, but an entry renamed joins as new. A clap
  in a test file is a hit for a song, not for `truth`.
- NSynth's notes are each normalised: on the test set a pitch's velocity layer
  peaks a median 0.1 to 0.6 dB from its 127's, nine in ten within -1 to +4 dB,
  so a layer is timbre, not loudness. `corpus ... sampled` takes each note's level
  out and plays it at its step's velocity, as the engine's voices do. A release
  (from 3 s) falls 20 dB in a median 108 ms, an organ's 40 and a plucked or
  struck one's 65 to 82, which the cut's crossfade follows.
- The bench's loops play no piano: a piano change moves no bench number, a bass
  change does.
- A drum change is benched piece by piece without editing the tree:
  `stapes -config-sounds piece.toml -s bench.slab` overrides one sound of the
  built-in kit. The analysis hears a kick still ringing at 50 to 60 Hz as the bass
  line, and a snare's fixed modes move the melody's key: the bench's clean bass
  rests on three loops of twelve, which a kick of the old 42 Hz glide restored.

## Training rounds

- A training round runs from its own copy of the tree: `pipeline.sh` runs
  stapes through `go run` at every stage, so an edit to the checkout while a
  round trains changes the corpus or the bench under it. The bench draws its
  loops from the tree's code too, so rounds whose `aug` or `gen` differ bench on
  different loops: bench one round's weights through the other's stapes.
- A model's decoder thresholds belong to its weights: a new head's scale moves
  them, so sweep `strike`, `strike_again` and `release` before judging a round
  (10a read 0.530 pooled at 8b's, 0.650 at its own), and pin them in the root's
  `rip.toml` beside the weights. Compare rounds on one tree's bench loops.
- A round that gains on the bench may still lose on the recordings (10a over 8b):
  the bench's loops are the corpus's own kind. Judge by the recordings.
- Towers (`transformer.Tower`, `TOWERS=` in `tool/rip-train`) are in no shipped
  weight file, yet are not dead code: round 7d found no gain for 43% more
  weights, and they are to be tried again once new heads (offset, velocity,
  pedal) compete with the strike head for the shared layers. Keep them, their
  loader and their round-trip test.
- A training loss that keeps falling says nothing about fresh loops: 12000
  steps on 300 loops reached 0.31 and benched worse than 3000 steps on 120,
  each loop seen three hundred times. `train.py` holds a tenth of the loops
  out, scores them every `--every` steps and exports the best step; judge a run
  by its held-out line and by `bench` on the file, never by the last training
  loss, and prefer more loops to more steps.
- A `.pt`'s held-out loss, a merge's or any checkpoint's, is read without
  training: `train.load_corpus`, `train.validation` over the last `--holdout`
  loops and `train.evaluate` in a scratch script, the model built from the
  file's `cfg`. `--steps 0` scores nothing, since the score runs in the loop. A
  `pip install --user` torch is not seen under `python3 -I`, which drops the
  user's site directory.
- `train.py` reads each window's frames from its `.mel` as it draws it
  (`MelFile`) and builds the window's targets from the loop's events then
  (`Label.window`): rows per frame held up front came to 207 bytes a frame,
  5.2 GB for 40000 loops, and MAESTRO's 159 training hours would add 10 GB. A
  `np.memmap` a loop would keep a file descriptor open each, 40000 of them,
  past a 1024 limit or macOS's 256.
- A training run shares the cores with everything else the session does: the
  fourth run slowed from 0.4 to 4 seconds a step under a rip and a test run,
  and the two-hour waiter on it timed out twice. Size a waiter by the run
  alone and expect to re-arm it, or follow the run by a reminder instead.
