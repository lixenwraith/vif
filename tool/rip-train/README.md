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

`pipeline.sh` runs the four stages with every knob an environment variable,
and two more, `shard` and `merge`, that spread a run over several machines
([Training in parallel](#training-in-parallel)):

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
| `GEN` | `gen` | beats, melodies, bass lines and accompaniments the bank never had, three loops in five per part: a broken chord in eighths or sixteenths in the free slot, and in half the loops a melody of longer notes over it; empty for the bank alone |
| `WAV` | `nowav` | frames and labels only; empty keeps each loop's WAV too (300 MB per 200 loops) |
| `TEMPOS` | the default spread | `80-120` for six tempos over a range, `96,128,160` for a list; the sequencer's range is 80 to 200 |
| `FOCUS` | none | a soundlab script run before the corpus renders: the voices and patterns the loops draw from, see below |
| `STEPS`, `BATCH` | 20000, 8 | training steps and windows a step |
| `DIM`, `HEADS`, `LAYERS`, `FFN` | 256, 8, 4, 1024 | the model's width, attention heads, layers and feed-forward width, `train.py`'s `--dim`, `--heads`, `--layers` and `--ffn`; a `CHECKPOINT` or `INIT` of another shape is refused |
| `HOLDOUT`, `EVERY` | 200, 1000 | loops held out, and how often they are scored and a checkpoint written |
| `OUT`, `CHECKPOINT` | `$CORPUS/ripnet.bin`, `$CORPUS/ripnet.pt` | the weight file, and the checkpoint a stopped run resumes from |
| `ROOT` | `~/.config/vif` | the configuration root `install` copies the weights into |
| `BENCH_LOOPS`, `BENCH_BARS` | 12, 4 | the fresh loops `bench` grades on |
| `INIT`, `SHARD` | none, 0 | `shard`: the `.pt` the round starts from, and the worker's stream of windows; it writes `$CORPUS/shard$SHARD.pt` and `.bin` |
| `MERGE_FILES`, `TRIM` | none, 0 | `merge`: the round's shard checkpoints, blank-separated or a glob, and the share of them dropped at each end of each weight before the mean |
| `MERGED` | `$CORPUS/merged.pt` | `merge`: the mean as a `.pt`, the next round's `INIT`; its weight file goes to `OUT` |

A smoke test of the whole path, under ten minutes on a laptop:

```bash
LOOPS=200 STEPS=300 HOLDOUT=20 EVERY=100 BENCH_LOOPS=4 CORPUS=/tmp/smoke tool/rip-train/pipeline.sh all
```

What to expect at scale, on a 2017 laptop (an i7-7700HQ, four cores): the
export renders on one core, about 2.5 loops a second, so 20000 loops of four
bars take two and a quarter hours (`nowav`; the frames and labels are 5 GB);
a step of batch 8 at the default shape takes 1.45 s on the four cores and
0.11 s on its GTX 1060, so 20000 steps take eight hours or forty minutes (the
first runs' width 128 took 0.65 s on the cores); `bench 12 4` grades in a few
minutes. Each stage shares the cores with anything else the machine does.

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

## A round on a development machine

What a training round needs, as a hand-off to whoever or whatever runs it,
and what to bring back.

**The machine.** A vif checkout at a main that has the accompaniment corpus
(the thirteenth pass, [lixenwraith/vif#959](https://github.com/lixenwraith/vif/pull/959),
or later); Go as vif builds with; Python 3.11 or later with PyTorch (the
[setup](#setup) above; a CPU build trains the present model in a few hours, a
GPU build in well under one); 5 GB of free disk for a corpus of 20000 loops of
four bars (frames and labels, 4.5 GB; `WAV=` to keep each loop's audio adds
about 30 GB) and 200 MB for the checkpoints; four cores or more. The run
survives a stop: `train` resumes from `CHECKPOINT` when it is present, so the
same command run again continues.

**The run**, from the checkout's root, one corpus for the round and every
knob at its default unless named:

```bash
export CORPUS=$HOME/ripcorpus6          # anywhere with the disk
tool/rip-train/pipeline.sh corpus       # 20000 loops, aug gen: an hour or two on four cores
tool/rip-train/pipeline.sh train        # 20000 steps of 8 windows; three hours on four cores, a GPU in twenty minutes
tool/rip-train/pipeline.sh bench        # the model against the analysis on fresh loops, clean and augmented
```

`install` copies the weights into the configuration root the engine reads
(`ROOT`); do it only when the bench says the model beats the analysis on both
lines, which no run has yet. For a larger model pass the shape through
`train.py`'s own flags (`--dim 256 --layers 6`, or `--dim 384 --layers 8`),
with `LOOPS` and `STEPS` raised together, since the bigger model overfits a
small corpus sooner; the held-out line every `EVERY` steps is the guide, and
the run is done when it stops falling.

**What to bring back.** `$CORPUS/ripnet.bin` (the weights), `$CORPUS/ripnet.pt`
(the checkpoint, to resume or to start a round from), the training log with
its held-out lines, the bench's two summary lines, and, if the recordings are
to hand, `soundlab -headless -config-dir <root> -s <recipe>` for each recording
in `lixenwraith/scratch` with the weights installed in `<root>/audio/ripnet.bin`,
whose `truth` lines against the rhythm are the number that matters.

**A wrapper.** The same as a script that logs and resumes, for a machine
nobody watches:

```bash
#!/usr/bin/env bash
# round.sh: one training round from a vif checkout; rerun it to resume
set -euo pipefail
cd "$(dirname "$0")"                      # the vif checkout
export CORPUS=${CORPUS:-$HOME/ripcorpus6} LOOPS=${LOOPS:-20000} STEPS=${STEPS:-20000}
mkdir -p "$CORPUS"
[ -f "$CORPUS/loop_0.json" ] || tool/rip-train/pipeline.sh corpus 2>&1 | tee -a "$CORPUS/round.log"
tool/rip-train/pipeline.sh train 2>&1 | tee -a "$CORPUS/round.log"
tool/rip-train/pipeline.sh bench 2>&1 | tee -a "$CORPUS/round.log"
```

## Training in parallel

A run trains on one machine; a round spreads one over several. Every worker
starts from the same weights, trains on a stream of windows of its own, and the
round's checkpoints are averaged into the next round's start:

1. **The start.** The first round's `INIT` is an ordinary run's checkpoint, a
   few hundred steps on one machine, so the workers part from weights past the
   first steps, which move them most; each later round's is the last merge.
2. **The shards.** Each worker runs `shard` from `INIT` with a `SHARD` no worker
   has had in this round or an earlier one (worker k of W in round r takes
   r·W + k), so no two draw the same windows and no round resumes an earlier
   one's checkpoint. A shard writes `$CORPUS/shard$SHARD.pt`, the checkpoint the
   merge reads and a stopped shard resumes from, and `shard$SHARD.bin`, its best
   held-out step.
3. **The merge.** On one machine, with the shards' `.pt` files gathered, `merge`
   averages them weight by weight into `MERGED`, the next round's `INIT`, and
   writes the mean's weight file to `OUT`, which `bench` and `install` take as
   they take a run's.

A smoke round on one machine, two shards from one start, in a minute or two:

```bash
export CORPUS=/tmp/round HOLDOUT=4 EVERY=20
LOOPS=24 tool/rip-train/pipeline.sh corpus
STEPS=20 CHECKPOINT=/tmp/round/start.pt tool/rip-train/pipeline.sh train
INIT=/tmp/round/start.pt SHARD=0 STEPS=40 tool/rip-train/pipeline.sh shard
INIT=/tmp/round/start.pt SHARD=1 STEPS=40 tool/rip-train/pipeline.sh shard
MERGE_FILES='/tmp/round/shard*.pt' tool/rip-train/pipeline.sh merge   # /tmp/round/merged.pt, the next INIT
```

Every worker trains on the same corpus and holds out the same loops, so their
held-out lines compare: copy it, or render it on each from the same checkout
with the same knobs, since `corpus` draws from a fixed seed. A shard starts with
a fresh optimizer and a cosine schedule of its own, so each round is a warm
restart, and the merge takes each shard's last step, not its best held-out one.
`TRIM=0.1` drops each weight's highest and lowest tenth of the files before the
mean, so a worker that diverged does not drag the rest; it counts whole files,
rounded down, so under ten files it drops none.

**Why independent seeds cannot be averaged.** A network's hidden units can be
permuted, with the weights that read them, without changing what it computes,
and two runs from different random starts settle on different orders: what one
run's unit 7 does, the other's unit 31 does. Their mean, weight by weight,
blends units that mean different things and lands on the ridge of the loss
between the two, worse than either. Runs from one start, trained apart for a
while, keep its order and stay in one basin, where the mean of their weights is
about as good as each: hence one `INIT` a round, and rounds short enough that
the shards stay close. `average.py` refuses files of different shapes, but two
runs of one shape from different starts look alike to it; only the protocol
keeps them apart.

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
  `pipeline.sh` passes `DIM`, `HEADS`, `LAYERS` and `FFN`; keep a head 32 wide
  (`DIM=384 HEADS=12 FFN=1536`). A checkpoint records its shape, and `train.py`
  refuses to resume or start from one of another, naming both.

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
cropped at random from the loops; `--checkpoint` saves every `--every` steps,
with the best held-out step's weights, and resumes from the file when it
exists, `--init` starts from a `.pt`'s weights alone (no optimizer state, step
0), and `--shard-seed` moves the stream the windows are drawn from while
`--seed` keeps the starting weights; `average.py --pt next.pt --out next.bin
[--trim 0.1] a.pt b.pt ...` is the merge. The defaults are `ripnet.Spec`:
eighty bands in, width 256, eight heads, four layers, feed-forward 1024, a
window of 64 frames either side, 3.2 million weights. The losses: onsets,
drums and pitches as weighted binary cross-entropy, voicing likewise, the wave
as cross-entropy and the voice genes as squared error, the last two over
frames a note sounds in.

## Parity

The Go forward pass and this model agree to `float32` rounding: exact GELU,
layer norm with eps 1e-5, ALiBi slopes of 2^(-8h/H) per head on the frame
distance, attention banded to the window, and the output layer's slices as the
heads, in `ripnet.HEADS` order. `export` writes the tensors by the names and in
the order `pkg/transformer` reads them, with an FNV-1a 64 trailer the loader
checks. A change to either side is a change to both: check a random model's
outputs on a corpus loop from the two before trusting a trained file.
