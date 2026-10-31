# rip-train

The trainer of the rip's front-end model, `pkg/audio/ripnet`: PyTorch, outside
the Go module, writing the weight file `pkg/transformer` loads. Where the model
stands in the pipeline, and why its features are made in Go, is in
[Rip design](../../doc/rip-design.md) §2 and §3.

## Setup

```bash
python3 -m venv .venv && . .venv/bin/activate
pip install -r requirements.txt
```

A CPU-only PyTorch is enough to train the default model slowly and to check the
path: `pip install torch --index-url https://download.pytorch.org/whl/cpu`.

## Corpus

Soundlab renders it, from the patterns and voices registered in its session: the
shipped bank, plus anything applied before the verb runs. Write a script and run
it headless:

```bash
printf 'corpus corpus/ 200 4\nquit\n' > corpus.slab
go run ./cmd/soundlab -headless -s corpus.slab
```

Each loop is `loop_NNN.wav` as rendered, `loop_NNN.mel`, the log-mel frames the
model hears (two little-endian `uint32`, frames and bands, then `float32` rows),
and `loop_NNN.json`, the events that sound in it: a frame, an end frame, a drum
(kick 0, snare 1, hihat 2, or -1 for a note), a MIDI note, and the wave index and
voice genes of the voice it was played with. `corpus.json` names the layout. Two
hundred loops of four bars are about fifteen minutes of music, 300 MB of WAV and
60 MB of frames; the trainer reads the frames and the events alone.

More variety is more patterns and voices applied before `corpus`: `new voice`,
`set`, `apply` make an instrument the loops draw. A room, a mix and a codec
between the render and the frames are on the plan, not here yet.

## Train

```bash
python3 train.py --corpus corpus/ --out ripnet.bin --steps 20000 --batch 16 --checkpoint ripnet.pt
```

The defaults are `ripnet.Spec`: eighty bands in, width 128, four heads, four
layers, feed-forward 512, a window of 64 frames either side, about 800 thousand
weights. Each step trains on `--batch` windows of `--window` frames (512, six
seconds) cropped at random from the loops. On a laptop CPU a step takes about a
second at batch 8; a GPU is ten to twenty times faster. Two thousand steps show
whether the losses move; the lines name each head's loss, onsets, drums and
pitches as weighted binary cross-entropy, voicing likewise, the wave as
cross-entropy and the voice genes as squared error, the last two over frames a
note sounds in. `--checkpoint` saves every `--every` steps and resumes from the
file when it exists; `--dim`, `--layers`, `--heads`, `--ffn` and
`--attention-window` reshape the model, and the weight file carries the shape, so
`ripnet` loads any of them as long as the inputs and heads are its.

## Deploy

```bash
cp ripnet.bin ~/.config/vif/audio/ripnet.bin
```

The first configuration root's `audio/ripnet.bin` is what soundlab's `rip` hears
with; soundlab says `rip model ... weights` when it starts and `stat` names it.
Remove the file to hear with the built-in analysis again. `-config-dir` points
at another root, as for the game.

## Parity

The Go forward pass and this model agree to `float32` rounding: exact GELU,
layer norm with eps 1e-5, ALiBi slopes of 2^(-8h/H) per head on the frame
distance, attention banded to the window, and the output layer's slices as the
heads, in `ripnet.HEADS` order. `export` writes the tensors by the names and in
the order `pkg/transformer` reads them, with an FNV-1a 64 trailer the loader
checks. A change to either side is a change to both: check a random model's
outputs on a corpus loop from the two before trusting a trained file.
