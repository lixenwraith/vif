# rip-train

The trainer of the rip's front-end model, `pkg/audio/ripnet`: PyTorch, outside
the Go module, writing the weight file `pkg/transformer` loads. Where the model
stands in the pipeline, and why its features are made in Go, is in
[Rip design](../../doc/rip-design.md) §2 and §3; the runs so far and their
numbers are in its §8. This file is the hands-on guide: setup, the pipeline end
to end, and how to turn a run toward the music you care about.

## What a run is

1. **A corpus.** Soundlab renders loops from the patterns and voices registered
   in its session and labels every event in them exactly, because the engine
   played them: the frames a model hears (`loop_NNN.mel`), the events
   (`loop_NNN.json`: frame, end, drum or MIDI note, the wave and genes of the
   voice), and `corpus.json` naming the layout. Nothing is hand-labelled; the
   synthesizer is the ground truth, and the augmentation is what keeps the
   model from learning the synthesizer.
2. **A model.** `train.py` trains `ripnet.Spec` on random windows of the loops,
   holds some loops out, scores them every so often and exports the step whose
   onset, drum, pitch and voicing heads score best on them, the heads the rip's
   decoders read.
3. **A grade.** Soundlab's `bench` rips fresh loops, clean and augmented, through
   the built-in analysis and through the model, and scores each rip against the
   loop's expected rip. A model that scores under the analysis is not ready for
   a recording; a recording is judged by ear, with `rip`, `notes` and `song score`.
4. **Data.** The weight file goes to the configuration root as
   `audio/ripnet.bin`; soundlab's `rip` hears with it when it is there and with
   the analysis when it is not.

## Setup

- Go, as for vif; a CPU is enough.
- Python 3 with PyTorch:

```bash
cd tool/rip-train
python3 -m venv .venv && . .venv/bin/activate
pip install -r requirements.txt
pip install torch --index-url https://download.pytorch.org/whl/cpu   # a CPU PyTorch; a GPU build trains ten to twenty times faster
```

Everything below runs from the vif checkout.

## The pipeline

`pipeline.sh` runs the four stages with every knob an environment variable:

```bash
tool/rip-train/pipeline.sh all            # corpus, train, bench, install, with the defaults below
tool/rip-train/pipeline.sh corpus         # one stage at a time
tool/rip-train/pipeline.sh train
tool/rip-train/pipeline.sh bench
tool/rip-train/pipeline.sh install
```

| Knob | Default | What it does |
|---|---|---|
| `CORPUS` | `corpus` | where the loops go |
| `LOOPS`, `BARS` | 20000, 4 | loops rendered, bars a loop; 200 loops is a smoke test |
| `AUG` | `aug` | a room, a mix, voices of random genes near the registered ones, a third of the loops driven into a soft clip, each loop rendered off A440 by up to a quarter tone; empty for clean renders |
| `GEN` | `gen` | beats, melodies and bass lines the bank never had, three loops in five per part; empty for the bank alone |
| `WAV` | `nowav` | frames and labels only; empty keeps each loop's WAV too (300 MB per 200 loops) |
| `TEMPOS` | the default spread | `80-120` for six tempos over a range, `96,128,160` for a list; the sequencer's range is 80 to 200 |
| `FOCUS` | none | a soundlab script run before the corpus renders: the voices and patterns the loops draw from, see below |
| `STEPS`, `BATCH` | 20000, 8 | training steps and windows a step |
| `HOLDOUT`, `EVERY` | 200, 1000 | loops held out, and how often they are scored and a checkpoint written |
| `OUT`, `CHECKPOINT` | `$CORPUS/ripnet.bin`, `$CORPUS/ripnet.pt` | the weight file, and the checkpoint a stopped run resumes from |
| `ROOT` | `~/.config/vif` | the configuration root `install` copies the weights into |
| `BENCH_LOOPS`, `BENCH_BARS` | 12, 4 | the fresh loops `bench` grades on |

A smoke test of the whole path, under ten minutes on a laptop:

```bash
LOOPS=200 STEPS=300 HOLDOUT=20 EVERY=100 BENCH_LOOPS=4 CORPUS=/tmp/smoke tool/rip-train/pipeline.sh all
```

What to expect at scale, on four CPU cores: 20000 loops of four bars export in
about an hour (`nowav`; the frames and labels are 4.5 GB), 20000 steps of batch
8 train in two and three quarter hours when the machine is otherwise idle,
`bench 12 4` grades in a few minutes. The export and the training run one after
the other and share the cores with anything else the machine does.

## Reading a run

- The training lines name each head's loss; the line that matters is the
  held-out one every `EVERY` steps: `step N held-out X, decoders Y (onset ...
  drums ... pitch ... voicing ...)`, with `(best)` on the step `OUT` will get.
  A training loss that keeps falling after the held-out loss turns is the
  model learning the corpus by heart; more loops, not more steps, is the cure.
- `bench` prints four lines: the analysis and the model, on clean and on
  augmented loops, each the mean score with its parts (tempo, bars, drums,
  melody, bass). The fourth run, on 20000 loops rendered at A440, stood at
  0.45 against the analysis's 0.49 clean and 0.51 against 0.48 augmented,
  ahead on drums and behind on pitch; the fifth, on loops off A440 and
  saturated, is measured in the design document's §8.
- A recording: `rip`, then `notes <name>` for the transcript with each note's
  support, `compress`, `refine`, and `song <name> score` for how the song
  sounds against the recording's own frames and how it rips back. The ear is
  the verifier; the recordings in `lixenwraith/scratch` are the held-out set.

## Turning a run toward your music

The corpus is whatever soundlab's session holds when `corpus` runs, so a focus
is a script that loads and applies voices and patterns first, named by `FOCUS`
and run by the pipeline before the export (and before `bench`, so the grade
draws from the same bank). Two are in `focus/`:

- **`focus/vocal.slab`: music with vocals.** A vocal is a line to the rip, pitch
  and onsets per frame, never words; nothing here transcribes lyrics, and the
  model has no notion of them. What the model needs is lines that sound like
  voices among its loops, so the focus adds a sung-line voice and a choir pad
  (`focus/vocal.toml`) beside the shipped lead and bass; the generated melodies
  draw any tonal instrument, these among them, and the augmentation redraws
  each voice's genes near the registered one, keeping its vowel and vibrato,
  so the loops hold many voices like them. A voice has a `vowel` (a, e, i, o
  or u: three formants the oscillator passes through) and a vibrato (a rate,
  a depth in cents, a delay before it is full); neither is a gene, so the
  model's voice head and the fit do not search them, and a real vocal
  recording in the held-out set is the check that matters.
- **`focus/plucked.slab`: guitars and keys.** Nylon and steel strings and a
  plucked bass (`focus/plucked.toml`) beside the modal piano.

To write your own:

- **Instruments.** A `[[voice]]` is data (`doc/audio.md` §7): wave, oscillator
  stack, filter, envelopes, or a string or modal bank. Write a sound document
  with the voices and `merge sound file.toml` then `apply name` for each, or
  make them in the REPL with `new voice`, `set` and `apply`; `fit` and `refine`
  find voices for a recording's lines, and `write` exports any voice as a
  document to start from. Every registered tonal voice is one the generated
  melodies and bass lines are played with.
- **Music.** The shipped pattern bank is the game's; `load pattern file.toml`
  or `merge pattern` brings in yours, in the pattern document's form
  (`[[pattern]]` with tracks of steps), and `gen` adds parts the bank never
  had. A bank that is all one genre makes a corpus of that genre; `gen` keeps
  it from being only that bank's few melodies. The rhythm patterns set the
  drums the model learns.
- **Tempos and keys.** `TEMPOS` sets the spread; the keys cycle the roots and
  scales the engine knows. A piece whose tempo the sequencer's range does not
  hold reads at a multiple: the origin recording near 74 bpm reads at 148.
- **Rooms, mixes, saturation, tuning.** `AUG` is on by default and is what
  stands between the synthesizer and a recording: leave it on for anything a
  recording will be ripped with; turn it off only to measure the clean case.
- **Real recordings.** They cannot be labelled by the engine, so they are not
  trained on; they are the held-out set judged by ear and by the two-witness
  check (two recordings of one piece, their transcripts compared, as
  `doc/rip-design.md` §8 does). A melody as notes, a solo take of a line, and
  the same piece on other instruments and tempos are the most useful such
  material, in that order.
- **The model's shape.** `--dim`, `--layers`, `--heads`, `--ffn` and
  `--attention-window` on `train.py` reshape it; the weight file carries the
  shape, so `ripnet` loads any of them as long as the inputs and heads are its.
  Pass them through `pipeline.sh` by editing its `train` function, or run
  `train.py` by hand with the flags below.

## By hand

The stages are three commands; the script only strings them together.

```bash
printf 'corpus corpus/ 20000 4 aug gen nowav\n' > corpus.slab
go run ./cmd/soundlab -headless -s corpus.slab
python3 tool/rip-train/train.py --corpus corpus/ --out ripnet.bin --steps 20000 --batch 8 --every 1000 --holdout 200 --checkpoint ripnet.pt
printf 'bench 12 4\n' > bench.slab
mkdir -p root/audio && cp ripnet.bin root/audio/ripnet.bin
go run ./cmd/soundlab -headless -config-dir root -s bench.slab
cp ripnet.bin ~/.config/vif/audio/ripnet.bin
```

`train.py` trains on `--batch` windows of `--window` frames (512, six seconds)
cropped at random from the loops; `--checkpoint` saves every `--every` steps
and resumes from the file when it exists. The defaults are `ripnet.Spec`:
eighty bands in, width 128, four heads, four layers, feed-forward 512, a window
of 64 frames either side, about 800 thousand weights. The losses: onsets, drums
and pitches as weighted binary cross-entropy, voicing likewise, the wave as
cross-entropy and the voice genes as squared error, the last two over frames a
note sounds in.

## Parity

The Go forward pass and this model agree to `float32` rounding: exact GELU,
layer norm with eps 1e-5, ALiBi slopes of 2^(-8h/H) per head on the frame
distance, attention banded to the window, and the output layer's slices as the
heads, in `ripnet.HEADS` order. `export` writes the tensors by the names and in
the order `pkg/transformer` reads them, with an FNV-1a 64 trailer the loader
checks. A change to either side is a change to both: check a random model's
outputs on a corpus loop from the two before trusting a trained file.
