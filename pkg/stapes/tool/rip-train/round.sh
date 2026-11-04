#!/usr/bin/env bash
# One training round for a machine nobody watches: the corpus, then train and
# bench, every line kept in $CORPUS/round.log. Run it again to resume: the corpus
# goes on from the loops it has and train from CHECKPOINT. The knobs are
# pipeline.sh's; the README's "A round on a development machine" is the guide.
set -euo pipefail
CORPUS=$(realpath -m "${CORPUS:-corpus}")
export CORPUS
cd "$(dirname "$0")/../.."
mkdir -p "$CORPUS"
log=$CORPUS/round.log
[ -f "$CORPUS/corpus.json" ] || tool/rip-train/pipeline.sh corpus 2>&1 | tee -a "$log"
tool/rip-train/pipeline.sh train 2>&1 | tee -a "$log"
tool/rip-train/pipeline.sh bench 2>&1 | tee -a "$log"
