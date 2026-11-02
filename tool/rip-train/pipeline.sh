#!/usr/bin/env bash
# The rip model's training pipeline: corpus, train, bench and install, or all four
# in order; shard and merge run a training round over several workers. Run from
# the vif checkout; every knob is an environment variable, and the README is the
# guide.
set -euo pipefail

CORPUS=${CORPUS:-corpus}             # where the loops go
LOOPS=${LOOPS:-20000}                # loops to render; a smoke test is 200
PARTS=${PARTS:-1}                    # exports at once, each rendering every PARTS-th loop: a core each
BARS=${BARS:-4}                      # bars a loop
AUG=${AUG:-aug}                      # aug: a room, a mix, random voices, saturation, a tuning off A440; empty for clean
GEN=${GEN:-gen}                      # gen: beats, melodies and bass lines the bank never had; empty for the bank alone
WAV=${WAV:-nowav}                    # nowav: frames and labels only; empty to keep each loop's WAV
TEMPOS=${TEMPOS:-}                   # e.g. 80-120 or 96,128,160; empty for the default spread
FOCUS=${FOCUS:-}                     # a .slab run before corpus: voices and patterns to draw from
STEPS=${STEPS:-20000}                # training steps
BATCH=${BATCH:-8}                    # windows a step
DIM=${DIM:-256}                      # the model's width, attention heads, layers and feed-forward width
HEADS=${HEADS:-8}
LAYERS=${LAYERS:-4}
FFN=${FFN:-1024}
HOLDOUT=${HOLDOUT:-200}              # loops held out, scored every EVERY steps
EVERY=${EVERY:-1000}
OUT=${OUT:-$CORPUS/ripnet.bin}       # the weight file
CHECKPOINT=${CHECKPOINT:-$CORPUS/ripnet.pt}
ROOT=${ROOT:-${XDG_CONFIG_HOME:-$HOME/.config}/vif}   # the configuration root rip hears with
BENCH_LOOPS=${BENCH_LOOPS:-12}
BENCH_BARS=${BENCH_BARS:-4}
INIT=${INIT:-}                       # shard: the .pt the round starts from, the last merge's
SHARD=${SHARD:-0}                    # shard: this worker's stream of windows, its own in every round
MERGE_FILES=${MERGE_FILES:-}         # merge: the round's shard checkpoints, blank-separated, globs too
TRIM=${TRIM:-0}                      # merge: share of the files dropped at each end of each weight
MERGED=${MERGED:-$CORPUS/merged.pt}  # merge: the mean as a .pt, the next round's INIT; OUT gets its weights
PYTHON=${PYTHON:-python3}

here=$(cd "$(dirname "$0")" && pwd)
stage=${1:-all}

soundlab() { go run ./cmd/soundlab -headless "$@"; }

# corpus_slab writes the export's script to $1, its corpus line ending in $2
corpus_slab() {
	{
		[ -n "$FOCUS" ] && printf 'run %s\n' "$(realpath "$FOCUS")"
		printf 'corpus %s %s %s %s %s %s %s %s\n' "$(realpath "$CORPUS")" "$LOOPS" "$BARS" "$AUG" "$GEN" "$WAV" "${TEMPOS:+tempos $TEMPOS}" "${2:-}"
	} > "$1"
}

# The parts render at once; the last export, alone, renders any loop they left
# and writes corpus.json. Every export keeps the loops on disk, so a stopped
# corpus run again goes on where it stopped.
corpus() {
	mkdir -p "$CORPUS"
	local k
	corpus_slab "$CORPUS/corpus.slab"
	echo "== corpus: $(cat "$CORPUS/corpus.slab"), in $PARTS part(s)"
	if [ "$PARTS" -gt 1 ]; then
		for ((k = 0; k < PARTS; k++)); do
			corpus_slab "$CORPUS/corpus$k.slab" "part $k/$PARTS"
			soundlab -s "$CORPUS/corpus$k.slab" &
		done
		wait
	fi
	soundlab -s "$CORPUS/corpus.slab"
}

train() {
	echo "== train: $STEPS steps of batch $BATCH on $CORPUS, $HOLDOUT loops held out, width $DIM, $HEADS heads, $LAYERS layers, feed-forward $FFN"
	(cd "$here" && "$PYTHON" train.py --corpus "$(realpath "$CORPUS")" --out "$(realpath -m "$OUT")" --steps "$STEPS" --batch "$BATCH" --every "$EVERY" --holdout "$HOLDOUT" --checkpoint "$(realpath -m "$CHECKPOINT")" \
		--dim "$DIM" --heads "$HEADS" --layers "$LAYERS" --ffn "$FFN" "$@")
}

# A shard's files are named by SHARD, so a round never resumes an earlier one's checkpoint.
shard() {
	[ -f "$INIT" ] || { echo "shard: INIT names the .pt the round starts from; '$INIT' is no file" >&2; exit 2; }
	echo "== shard $SHARD from $INIT"
	OUT=$CORPUS/shard$SHARD.bin CHECKPOINT=$CORPUS/shard$SHARD.pt train --init "$(realpath "$INIT")" --shard-seed "$SHARD"
}

merge() {
	[ -n "$MERGE_FILES" ] || { echo "merge: MERGE_FILES names the round's shard checkpoints" >&2; exit 2; }
	echo "== merge: $MERGE_FILES into $MERGED and $OUT"
	# shellcheck disable=SC2086 # blank-separated, and a glob expands
	"$PYTHON" "$here/average.py" --pt "$MERGED" --out "$OUT" --trim "$TRIM" $MERGE_FILES
}

bench() {
	local root
	root=$(mktemp -d)
	mkdir -p "$root/audio"
	cp "$OUT" "$root/audio/ripnet.bin"
	local slab="$root/bench.slab"
	{
		[ -n "$FOCUS" ] && printf 'run %s\n' "$(realpath "$FOCUS")"
		printf 'bench %s %s\n' "$BENCH_LOOPS" "$BENCH_BARS"
	} > "$slab"
	echo "== bench: the analysis and $OUT on $BENCH_LOOPS fresh loops, clean and augmented"
	soundlab -config-dir "$root" -s "$slab"
	rm -rf "$root"
}

install() {
	mkdir -p "$ROOT/audio"
	cp "$OUT" "$ROOT/audio/ripnet.bin"
	echo "== installed $OUT as $ROOT/audio/ripnet.bin; soundlab's rip hears with it, remove the file to hear with the analysis"
}

case "$stage" in
corpus | train | shard | merge | bench | install) "$stage" ;;
all) corpus; train; bench; install ;;
*) echo "usage: $0 corpus|train|shard|merge|bench|install|all" >&2; exit 2 ;;
esac
