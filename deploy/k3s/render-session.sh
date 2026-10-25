#!/bin/sh
# Render one session manifest from the template, as the router builds it, so a
# person can create the same objects by hand and tell a router fault from a
# workload one. Without JOB_UID the Service has no owner reference, as the Job does
# not exist yet; deploy/k3s/session.sh performs the two-stage create and cleanup.
set -eu

usage() {
	echo "usage: $0 SESSION_ID GAME_NODEPORT [IMAGE] [PLAYERS] [MAP_SIZE]" >&2
	echo "       SCENARIO, LOG_LEVEL, BOTS, FIRST_JOIN, EMPTY_GRACE and JOB_UID override" >&2
	echo "       the defaults from the environment" >&2
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
BOTS=${BOTS:-0}
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
case "$BOTS" in
	'' | *[!0-9]* ) echo "$0: BOTS must be a number" >&2; exit 2 ;;
esac
case "$PLAYERS" in
	'' | *[!0-9]* ) echo "$0: PLAYERS must be a number" >&2; exit 2 ;;
esac
# The router's bound: a bot is a roster guest, and six fill the LimitRange
most=$((PLAYERS - 1))
[ "$most" -le 6 ] || most=6
if [ "$BOTS" -gt "$most" ]; then
	echo "$0: bots must be between 0 and $most for $PLAYERS players" >&2
	exit 2
fi
if [ "$GAME_NODEPORT" -lt 31700 ] || [ "$GAME_NODEPORT" -gt 31709 ]; then
	echo "$0: GAME_NODEPORT $GAME_NODEPORT is outside the fleet range 31700-31709" >&2
	exit 2
fi
# The router's envelope: each bot adds a seat's cost (tool/vif-router/manifest.go).
CPU_REQUEST=$((100 + 64 * BOTS))m
MEMORY_REQUEST=$((96 + 32 * BOTS))Mi
MEMORY_LIMIT=$((480 + 32 * BOTS))Mi
GOMEMLIMIT=$((448 + 32 * BOTS))MiB

template=$(dirname "$0")/30-session.yaml
[ -r "$template" ] || { echo "$0: cannot read $template" >&2; exit 1; }

# sed rather than envsubst: the latter is not installed everywhere and would also
# expand anything else in the file that happens to look like a variable.
rendered=$(sed \
	-e "s|\${SESSION_ID}|$SESSION_ID|g" \
	-e "s|\${GAME_NODEPORT}|$GAME_NODEPORT|g" \
	-e "s|\${IMAGE}|$IMAGE|g" \
	-e "s|\${PLAYERS}|$PLAYERS|g" \
	-e "s|\${BOTS}|$BOTS|g" \
	-e "s|\${CPU_REQUEST}|$CPU_REQUEST|g" \
	-e "s|\${MEMORY_REQUEST}|$MEMORY_REQUEST|g" \
	-e "s|\${MEMORY_LIMIT}|$MEMORY_LIMIT|g" \
	-e "s|\${GOMEMLIMIT}|$GOMEMLIMIT|g" \
	-e "s|\${MAP_SIZE}|$MAP_SIZE|g" \
	-e "s|\${FIRST_JOIN}|$FIRST_JOIN|g" \
	-e "s|\${EMPTY_GRACE}|$EMPTY_GRACE|g" \
	-e "s|\${SCENARIO}|$SCENARIO|g" \
	-e "s|\${LOG_LEVEL}|$LOG_LEVEL|g" \
	-e "s|\${JOB_UID}|$JOB_UID|g" \
	"$template")

if [ -z "$JOB_UID" ]; then
	rendered=$(printf '%s\n' "$rendered" | sed '/ownerReferences:/,/blockOwnerDeletion: true/d')
fi

printf '%s\n' "$rendered"
