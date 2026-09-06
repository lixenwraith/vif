#!/bin/sh
# Render one session manifest from the template, for a manual test.
#
# The allocator does this itself against the Kubernetes API; this exists so a
# person can create the same objects by hand and get the same result — which is
# what makes "the allocator is broken" and "the workload is broken" separable
# questions.
#
#   ./render-session.sh 7f3c1a 31777 | kubectl apply -f -
#   ./render-session.sh 7f3c1a 31777 | kubectl delete -f -
#
# The Service's owner reference is dropped here rather than filled in: the Job's
# UID does not exist until the Job does, so a hand-rendered pair is deleted by hand.
set -eu

usage() {
	echo "usage: $0 SESSION_ID GAME_NODEPORT [IMAGE] [PLAYERS] [MAP_SIZE]" >&2
	exit 2
}

[ $# -ge 2 ] || usage

SESSION_ID=$1
GAME_NODEPORT=$2
IMAGE=${3:-vi-fighter:dev}
PLAYERS=${4:-4}
MAP_SIZE=${5:-120x40}

case "$SESSION_ID" in
	'' | *[!a-z0-9-]* ) echo "$0: SESSION_ID must be lowercase alphanumeric or '-'" >&2; exit 2 ;;
esac
case "$GAME_NODEPORT" in
	'' | *[!0-9]* ) echo "$0: GAME_NODEPORT must be a number" >&2; exit 2 ;;
esac
if [ "$GAME_NODEPORT" -lt 30000 ] || [ "$GAME_NODEPORT" -gt 32767 ]; then
	echo "$0: GAME_NODEPORT $GAME_NODEPORT is outside the default NodePort range 30000-32767" >&2
	exit 2
fi

template=$(dirname "$0")/30-session.yaml
[ -r "$template" ] || { echo "$0: cannot read $template" >&2; exit 1; }

# sed rather than envsubst: the latter is not installed everywhere and would also
# expand anything else in the file that happens to look like a variable.
sed \
	-e "s|\${SESSION_ID}|$SESSION_ID|g" \
	-e "s|\${GAME_NODEPORT}|$GAME_NODEPORT|g" \
	-e "s|\${IMAGE}|$IMAGE|g" \
	-e "s|\${PLAYERS}|$PLAYERS|g" \
	-e "s|\${MAP_SIZE}|$MAP_SIZE|g" \
	-e '/ownerReferences:/,/blockOwnerDeletion: true/d' \
	"$template"
