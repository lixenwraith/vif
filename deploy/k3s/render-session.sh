#!/bin/sh
# Render one session manifest from the template, for a manual test.
#
# The allocator does this itself against the Kubernetes API; this exists so a
# person can create the same objects by hand and get the same result — which is
# what makes "the allocator is broken" and "the workload is broken" separable
# questions.
#
#   ./render-session.sh 7f3c1a 31707 > /tmp/7f3c1a.yaml
#   FIRST_JOIN=20m EMPTY_GRACE=20m ./render-session.sh 7f3c1a 31707
#   SCENARIO=td LOG_LEVEL=debug ./render-session.sh 7f3c1a 31707
#   WS_BRIDGE_IMAGE=vif-ws-bridge:pinned ./render-session.sh 7f3c1a 31707
#   JOB_UID=<uid> ./render-session.sh 7f3c1a 31707
#
# WS_BRIDGE_IMAGE is what a browser reaches the session through. Unset, the bridge
# sidecar is dropped and the rendered session is the native-clients-only one, which
# is the shape to render when the question is whether the game itself is broken.
#
# The one session container writes directly to the shared tmpfs-backed local PVC.
# No allocator log follower or per-session LogWisp sidecar exists. One standalone
# node service reads the files; the allocator only proxies its SSE bytes.
#
# Without JOB_UID the Service owner reference is omitted because the Job does not
# exist yet. deploy/k3s/session.sh performs the two-stage create and cleanup.
set -eu

usage() {
	echo "usage: $0 SESSION_ID GAME_NODEPORT [IMAGE] [PLAYERS] [MAP_SIZE]" >&2
	echo "       SCENARIO and LOG_LEVEL override the defaults from the environment" >&2
	exit 2
}

[ $# -ge 2 ] && [ $# -le 5 ] || usage

SESSION_ID=$1
GAME_NODEPORT=$2
IMAGE=${3:-vif:dev}
PLAYERS=${4:-4}
MAP_SIZE=${5:-120x40}
FIRST_JOIN=${FIRST_JOIN:-90s}
EMPTY_GRACE=${EMPTY_GRACE:-90s}
SCENARIO=${SCENARIO:-main}
LOG_LEVEL=${LOG_LEVEL:-info}
WS_BRIDGE_IMAGE=${WS_BRIDGE_IMAGE:-}
JOB_UID=${JOB_UID:-}

case "$SESSION_ID" in
	'' | *[!a-z0-9-]* ) echo "$0: SESSION_ID must be lowercase alphanumeric or '-'" >&2; exit 2 ;;
esac
case "$GAME_NODEPORT" in
	'' | *[!0-9]* ) echo "$0: GAME_NODEPORT must be a number" >&2; exit 2 ;;
esac
# A scenario name is a single path element the resolver looks up under scenario/;
# anything else would be an operator asking this template to reach off the volume.
case "$SCENARIO" in
	'' | *[!a-zA-Z0-9_-]* ) echo "$0: SCENARIO must be a plain installed name" >&2; exit 2 ;;
esac
case "$LOG_LEVEL" in
	trace|debug|info|warn|error ) ;;
	* ) echo "$0: LOG_LEVEL must be trace, debug, info, warn or error" >&2; exit 2 ;;
esac
if [ "$GAME_NODEPORT" -lt 31700 ] || [ "$GAME_NODEPORT" -gt 31709 ]; then
	echo "$0: GAME_NODEPORT $GAME_NODEPORT is outside the fleet range 31700-31709" >&2
	exit 2
fi

template=$(dirname "$0")/30-session.yaml
[ -r "$template" ] || { echo "$0: cannot read $template" >&2; exit 1; }

# sed rather than envsubst: the latter is not installed everywhere and would also
# expand anything else in the file that happens to look like a variable.
rendered=$(sed \
	-e "s|\${SESSION_ID}|$SESSION_ID|g" \
	-e "s|\${GAME_NODEPORT}|$GAME_NODEPORT|g" \
	-e "s|\${IMAGE}|$IMAGE|g" \
	-e "s|\${PLAYERS}|$PLAYERS|g" \
	-e "s|\${MAP_SIZE}|$MAP_SIZE|g" \
	-e "s|\${FIRST_JOIN}|$FIRST_JOIN|g" \
	-e "s|\${EMPTY_GRACE}|$EMPTY_GRACE|g" \
	-e "s|\${SCENARIO}|$SCENARIO|g" \
	-e "s|\${LOG_LEVEL}|$LOG_LEVEL|g" \
	-e "s|\${WS_BRIDGE_IMAGE}|$WS_BRIDGE_IMAGE|g" \
	-e "s|\${JOB_UID}|$JOB_UID|g" \
	"$template")

if [ -z "$WS_BRIDGE_IMAGE" ]; then
	rendered=$(printf '%s\n' "$rendered" |
		sed '/^        - name: ws-bridge$/,/^      containers:$/{/^      containers:$/!d;}')
fi

if [ -z "$JOB_UID" ]; then
	rendered=$(printf '%s\n' "$rendered" | sed '/ownerReferences:/,/blockOwnerDeletion: true/d')
fi

printf '%s\n' "$rendered"
