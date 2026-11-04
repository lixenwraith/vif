# rip-train

The trainer of the rip's front-end model, `lixenwraith/audio/ripmodel`: PyTorch,
outside the Go module, writing the weight file `lixenwraith/transformer` loads
([format](https://github.com/lixenwraith/transformer/blob/main/doc/format.md)).
Where the model stands in the pipeline, and why its features are made in Go, is
in [Rip design](../../doc/rip-design.md) §2 and §3; the runs so far and their
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
2. **A model.** `train.py` trains `ripmodel.Spec` on random windows of the loops,
   holds some loops out, scores them every so often and exports the step whose
   onset, drum, pitch and voicing heads score best on them, the heads the rip's
   decoders read.
3. **A grade.** Soundlab's `bench` rips fresh loops, clean and augmented, through
   the built-in analysis and through the model, and scores each rip against the
   loop's expected rip. A model that scores under the analysis is not ready for
   a recording; a recording is judged by ear, with `rip`, `notes` and `song score`.
4. **Data.** The weight file goes to the configuration root as
   `audio/ripmodel.bin`; soundlab's `rip` hears with it when it is there and with
   the analysis when it is not.

## Setup

- Go, as for vif; a CPU is enough.
- Python 3 with PyTorch:

```bash
cd tool/rip-train
python3 -m venv .venv && . .venv/bin/activate
pip install torch --index-url https://download.pytorch.org/whl/cpu   # a CPU PyTorch, first, or requirements.txt pulls PyPI's
pip install -r requirements.txt
```

A GPU trains an order of magnitude faster (below): `whl/cu130` for a recent
NVIDIA card, and `torch==2.14.* --index-url https://download.pytorch.org/whl/cu126`
for a GTX 10-series (Pascal), whose last build that is; `train.py` picks the
GPU by itself. The CUDA build is 6 GB installed.

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
| `PARTS` | 1 | exports at once, each rendering every `PARTS`-th loop on a core of its own; a last export renders any loop they left and writes `corpus.json` |
| `AUG` | `aug` | a room, a mix, voices of random genes near the registered ones, a pedal's ringing in a third of the loops, a soft clip in a third, a compressor and a noise floor in half, each loop rendered off A440 by up to a quarter tone; empty for clean renders |
| `GEN` | `gen` | beats, melodies, bass lines and accompaniments the bank never had, three loops in five per part: a broken chord in eighths or sixteenths in the free slot, and in half the loops a melody of longer notes over it; a third of the generated melodies and accompaniments half 6 to 18 dB softer; three generated loops in ten without their drums and one in seven without its bass; empty for the bank alone |
| `WAV` | `nowav` | frames and labels only; empty keeps each loop's WAV too (300 MB per 200 loops) |
| `CODEC` | empty | `codec`: a third of the loops through Opus, MP3 or AAC at 64 to 192 kbps and back, by ffmpeg, which the export then needs |
| `SAMPLED` | empty | a directory of NSynth notes (`family_source_NNN-pitch-velocity.wav`, a partial set too): each tonal line plays on one of its instruments in half the loops, labelled with no voice; empty for the engine's voices alone |
| `TEMPOS` | the default spread | `80-120` for six tempos over a range, `96,128,160` for a list; the sequencer's range is 80 to 200 |
| `FOCUS` | none | a soundlab script run before the corpus renders: the voices and patterns the loops draw from, see below |
| `STEPS`, `BATCH` | 20000, 8 | training steps and windows a step |
| `DIM`, `HEADS`, `LAYERS`, `FFN` | 256, 8, 4, 1024 | the model's width, attention heads, layers and feed-forward width, `train.py`'s `--dim`, `--heads`, `--layers` and `--ffn`; a `CHECKPOINT` or `INIT` of another shape is refused |
| `CONVS`, `TOWERS` | none | convolutions before the input layer, and towers of layers over it, each computing a run of the heads, `train.py`'s `--convs` and `--towers` |
| `MIX` | empty | another corpus and the share of windows drawn from it, `dir:share`, such as recordings with their MIDI files (`corpus <dir> from`) |
| `HOLDOUT`, `EVERY` | 200, 1000 | loops held out, and how often they are scored and a checkpoint written |
| `OUT`, `CHECKPOINT` | `$CORPUS/ripmodel.bin`, `$CORPUS/ripmodel.pt` | the weight file, and the checkpoint a stopped run resumes from |
| `ROOT` | `~/.config/vif` | the configuration root `install` copies the weights into |
| `BENCH_LOOPS`, `BENCH_BARS` | 12, 4 | the fresh loops `bench` grades on |
| `INIT`, `SHARD` | none, 0 | `shard`: the `.pt` the round starts from, and the worker's stream of windows; it writes `$CORPUS/shard$SHARD.pt` and `.bin` |
| `MERGE_FILES`, `TRIM` | none, 0 | `merge`: the round's shard checkpoints, blank-separated or a glob, and the share of them dropped at each end of each weight before the mean |
| `MERGED` | `$CORPUS/merged.pt` | `merge`: the mean as a `.pt`, the next round's `INIT`; its weight file goes to `OUT` |

A smoke test of the whole path, under ten minutes on a laptop:

```bash
LOOPS=200 STEPS=300 HOLDOUT=20 EVERY=100 BENCH_LOOPS=4 CORPUS=/tmp/smoke tool/rip-train/pipeline.sh all
```

What to expect at scale, on a 2017 laptop (an i7-7700HQ, four cores): an
export renders about 2.5 loops a second on one core and `PARTS=4` 7.0 on the
four, so 40000 loops of four bars take four and a half hours or 95 minutes
(`nowav`; the frames and labels are 9.5 GB); a step of batch 8 at the default
shape takes 1.45 s on the four cores and 0.11 s on its GTX 1060, 0.19 s in a
whole run, which reads the frames from disk and scores the held-out loops, so
40000 steps take two hours on the GPU (the first runs' width 128 took 0.65 s
on the cores); `bench 12 4` grades in a few minutes. Each stage shares the
cores with anything else the machine does.

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
  ahead on drums and behind on pitch; the sixth, at 4× on 40000 loops, at
  0.51 against 0.47 and 0.460 against 0.458, the first to pass both lines,
  ahead on drums and behind on melodies on the recordings' truths: the
  design document's §8.
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
[setup](#setup) above); 10 GB of free disk for a corpus of 40000 loops of four
bars (frames and labels; `WAV=` to keep each loop's audio adds about 60 GB)
and 50 MB for the checkpoint; memory for the labels' events, each window's
frames and targets being read and built as the training draws them; four
cores or more.

**The run**, from the checkout's root, in a corpus directory of its own (a
checkpoint of another shape there is refused) and every knob at its default
unless named:

```bash
. tool/rip-train/.venv/bin/activate   # the setup's PyTorch
export CORPUS=$HOME/ripcorpus6 LOOPS=40000 STEPS=40000 HOLDOUT=400 EVERY=1000 PARTS=4
tool/rip-train/round.sh               # corpus, train, bench, logging to $CORPUS/round.log, resumable
```

`round.sh` renders the corpus unless its `corpus.json`, which the export
writes last, is there, then trains and benches, every line appended to
`$CORPUS/round.log`. Run it again after a stop and it goes on: the corpus from
the loops on disk, whose randomness the export draws all the same, and the
training from `CHECKPOINT`, which carries the best held-out step; either way
the result is an unbroken run's. On the laptop above the corpus takes 95
minutes in four parts and the 40000 steps two hours on its GPU, about sixteen
on its cores.

`install` copies the weights into the configuration root the engine reads
(`ROOT`); do it when the bench says the model beats the analysis on both
lines, which the sixth run was the first to do, and judge it by ear on the
recordings, whose truths still favour the analysis (Rip design §8). For
another shape set `DIM`, `HEADS`, `LAYERS`
and `FFN`, with `LOOPS` and `STEPS` raised together, since a bigger model
overfits a small corpus sooner; the held-out line every `EVERY` steps is the
guide, and the run is done when it stops falling.

**What to bring back.** `$CORPUS/ripmodel.bin` (the weights), `$CORPUS/ripmodel.pt`
(the checkpoint, to resume or to start a round from), the training log with
its held-out lines, the bench's two summary lines, and, if the recordings are
to hand, `soundlab -headless -config-dir <root> -s <recipe>` for each recording
in `lixenwraith/scratch` with the weights installed in `<root>/audio/ripmodel.bin`,
whose `truth` lines against the rhythm are the number that matters.

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

- **Instruments.** A `[[voice]]` is data (`pkg/audio/doc/audio.md` §7): wave, oscillator
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
- **Real recordings.** One with the MIDI file it was played from is a loop of
  its own: `corpus <dir> from <list.csv> [split <name>] [hours <h>] [codec]`
  reads a CSV with `audio_filename` and `midi_filename` columns (and `split`
  and `duration`, as E-GMD's metadata has them), and `train.py --mix
  <dir>:<share>` draws that share of the windows from it, labelled by the
  MIDI's notes, velocities and hits, a tom's or a cymbal's an onset of no drum,
  the voices unknown. A batch run into the same directory joins it, numbered
  after the loops there, so a batch's recordings can be deleted once it is in.
  Weights trained on a corpus carry its licence (E-GMD's CC BY 4.0 asks for
  credit; MAESTRO's CC BY-NC-SA 4.0 binds them). A recording without its MIDI
  is held out, judged by ear and by the two-witness check (two recordings of
  one piece, their transcripts compared, as `doc/rip-design.md` §8 does).
- **The model's shape.** `--dim`, `--layers`, `--heads`, `--ffn` and
  `--attention-window` on `train.py` reshape it; the weight file carries the
  shape, so `ripmodel` loads any of them as long as the inputs and heads are its.
  `pipeline.sh` passes `DIM`, `HEADS`, `LAYERS` and `FFN`; keep a head 32 wide
  (`DIM=384 HEADS=12 FFN=1536`). A checkpoint records its shape, and `train.py`
  refuses to resume or start from one of another, naming both.

## By hand

The stages are three commands; the script only strings them together.

```bash
printf 'corpus corpus/ 20000 4 aug gen nowav\n' > corpus.slab
go run ./cmd/soundlab -headless -s corpus.slab
python3 tool/rip-train/train.py --corpus corpus/ --out ripmodel.bin --steps 20000 --batch 8 --every 1000 --holdout 200 --checkpoint ripmodel.pt
printf 'bench 12 4\n' > bench.slab
mkdir -p root/audio && cp ripmodel.bin root/audio/ripmodel.bin
go run ./cmd/soundlab -headless -config-dir root -s bench.slab
cp ripmodel.bin ~/.config/vif/audio/ripmodel.bin
```

`train.py` trains on `--batch` windows of `--window` frames (512, six seconds)
cropped at random from the loops, the next batch built while the device
trains on this one; `--checkpoint` saves every `--every` steps,
with the best held-out step's weights, and resumes from the file when it
exists, `--init` starts from a `.pt`'s weights alone (no optimizer state, step
0), and `--shard-seed` moves the stream the windows are drawn from while
`--seed` keeps the starting weights; `average.py --pt next.pt --out next.bin
[--trim 0.1] a.pt b.pt ...` is the merge. The defaults are `ripmodel.Spec`:
230 inputs, the corpus's 229 bands in dB over the frame's mean and the mean
itself (`ripmodel.features`, derived from the stored frames), width 256, eight
heads, four layers, feed-forward 1024, a
window of 64 frames either side, 3.2 million weights. `--convs
16x3x3/1,32x3x3/2` puts convolutions before the input layer, each its
channels, frames by bands kernel and the pooling over bands; `--towers 2:2,4:7`
splits the layers into towers over the shared input layer, each its layers and
the run of heads it computes in `ripmodel.HEADS` order: the onset and the drums
on two layers, the rest on four. The losses: onsets,
drums, pitches and each pitch's strikes and releases (within a frame of a
note's start and end) as weighted binary cross-entropy, voicing likewise, the
wave as cross-entropy and the voice genes as squared error over frames a note
sounds in, and each strike's velocity as squared error where the corpus gives
one.

## Parity

The Go forward pass and this model agree to `float32` rounding: exact GELU,
layer norm with eps 1e-5, ALiBi slopes of 2^(-8h/H) per head on the frame
distance, attention banded to the window, and the output layer's slices as the
heads, in `ripmodel.HEADS` order. `export` writes the tensors by the names and in
the order `lixenwraith/transformer` reads them, with an FNV-1a 64 trailer the
loader checks ([format](https://github.com/lixenwraith/transformer/blob/main/doc/format.md)).
A change to either side is a change to both: check a random model's outputs on
a corpus loop from the two before trusting a trained file, as the module's
[agent notes](https://github.com/lixenwraith/transformer/blob/main/doc/agent-notes.md)
describe.
