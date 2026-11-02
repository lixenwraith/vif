#!/usr/bin/env bash
# The rip model's training pipeline, end to end: a corpus rendered by soundlab,
# a model trained on it, benched against the built-in analysis, installed as
# data. Run from the vif checkout; every knob is an environment variable.
#
#   tool/rip-train/pipeline.sh corpus   # render LOOPS loops into CORPUS
#   tool/rip-train/pipeline.sh train    # train STEPS steps, weights to OUT
#   tool/rip-train/pipeline.sh bench    # grade OUT against the analysis
#   tool/rip-train/pipeline.sh install  # copy OUT into ROOT/audio/ripnet.bin
#   tool/rip-train/pipeline.sh all      # the four in order
#
# A FOCUS script runs in soundlab before the corpus renders: it registers the
# voices and patterns the loops draw from (see focus/ and the README).
set -euo pipefail

CORPUS=${CORPUS:-corpus}             # where the loops go
LOOPS=${LOOPS:-20000}                # loops to render; a smoke test is 200
BARS=${BARS:-4}                      # bars a loop
AUG=${AUG:-aug}                      # aug: a room, a mix, random voices, saturation, a tuning off A440; empty for clean
GEN=${GEN:-gen}                      # gen: beats, melodies and bass lines the bank never had; empty for the bank alone
WAV=${WAV:-nowav}                    # nowav: frames and labels only; empty to keep each loop's WAV
TEMPOS=${TEMPOS:-}                   # e.g. 80-120 or 96,128,160; empty for the default spread
FOCUS=${FOCUS:-}                     # a .slab run before corpus: voices and patterns to draw from
STEPS=${STEPS:-20000}                # training steps
BATCH=${BATCH:-8}                    # windows a step
HOLDOUT=${HOLDOUT:-200}              # loops held out, scored every EVERY steps
EVERY=${EVERY:-1000}
OUT=${OUT:-$CORPUS/ripnet.bin}       # the weight file
CHECKPOINT=${CHECKPOINT:-$CORPUS/ripnet.pt}
ROOT=${ROOT:-${XDG_CONFIG_HOME:-$HOME/.config}/vif}   # the configuration root rip hears with
BENCH_LOOPS=${BENCH_LOOPS:-12}
BENCH_BARS=${BENCH_BARS:-4}
PYTHON=${PYTHON:-python3}

here=$(cd "$(dirname "$0")" && pwd)
stage=${1:-all}

soundlab() { go run ./cmd/soundlab -headless "$@"; }

corpus() {
	mkdir -p "$CORPUS"
	local slab="$CORPUS/corpus.slab"
	{
		[ -n "$FOCUS" ] && printf 'run %s\n' "$(realpath "$FOCUS")"
		printf 'corpus %s %s %s %s %s %s %s\n' "$(realpath "$CORPUS")" "$LOOPS" "$BARS" "$AUG" "$GEN" "$WAV" "${TEMPOS:+tempos $TEMPOS}"
	} > "$slab"
	echo "== corpus: $(cat "$slab")"
	soundlab -s "$slab"
}

train() {
	echo "== train: $STEPS steps of batch $BATCH on $CORPUS, $HOLDOUT loops held out"
	(cd "$here" && "$PYTHON" train.py --corpus "$(realpath "$CORPUS")" --out "$(realpath -m "$OUT")" --steps "$STEPS" --batch "$BATCH" --every "$EVERY" --holdout "$HOLDOUT" --checkpoint "$(realpath -m "$CHECKPOINT")")
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
corpus | train | bench | install) "$stage" ;;
all) corpus; train; bench; install ;;
*) echo "usage: $0 corpus|train|bench|install|all" >&2; exit 2 ;;
esac
