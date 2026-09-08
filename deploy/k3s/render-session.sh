#!/bin/sh
# Render one session manifest from the template, for a manual test.
#
# The allocator does this itself against the Kubernetes API; this exists so a
# person can create the same objects by hand and get the same result — which is
# what makes "the allocator is broken" and "the workload is broken" separable
# questions.
#
#   ./render-session.sh 7f3c1a 31707 | sudo kubectl apply -f -
#   ./render-session.sh 7f3c1a 31707 | sudo kubectl delete -f -
#   FIRST_JOIN=20m EMPTY_GRACE=20m ./render-session.sh 7f3c1a 31707
#   LOGWISP_IMAGE=none ./render-session.sh 7f3c1a 31707
#
# The Service's owner reference is dropped here rather than filled in: the Job's
# UID does not exist until the Job does. The orphaned Service must be deleted by
# hand after its Job or it keeps the NodePort allocated.
set -eu

usage() {
	echo "usage: $0 SESSION_ID GAME_NODEPORT [IMAGE] [PLAYERS] [MAP_SIZE] [LOGWISP_IMAGE]" >&2
	exit 2
}

[ $# -ge 2 ] || usage

SESSION_ID=$1
GAME_NODEPORT=$2
IMAGE=${3:-vi-fighter:dev}
PLAYERS=${4:-4}
MAP_SIZE=${5:-120x40}
LOGWISP_IMAGE=${6:-${LOGWISP_IMAGE:-logwisp:dev}}
FIRST_JOIN=${FIRST_JOIN:-90s}
EMPTY_GRACE=${EMPTY_GRACE:-90s}

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
rendered=$(sed \
	-e "s|\${SESSION_ID}|$SESSION_ID|g" \
	-e "s|\${GAME_NODEPORT}|$GAME_NODEPORT|g" \
	-e "s|\${IMAGE}|$IMAGE|g" \
	-e "s|\${PLAYERS}|$PLAYERS|g" \
	-e "s|\${MAP_SIZE}|$MAP_SIZE|g" \
	-e "s|\${LOGWISP_IMAGE}|$LOGWISP_IMAGE|g" \
	-e "s|\${FIRST_JOIN}|$FIRST_JOIN|g" \
	-e "s|\${EMPTY_GRACE}|$EMPTY_GRACE|g" \
	-e '/ownerReferences:/,/blockOwnerDeletion: true/d' \
	"$template")

if [ "$LOGWISP_IMAGE" != none ]; then
	printf '%s\n' "$rendered"
	exit 0
fi

# A stdout-only render removes the sidecar and both shared volumes. This keeps
# session failures separable from an image or configuration failure in LogWisp.
printf '%s\n' "$rendered" | awk '
	$0 == "            - \"-l=/var/log/vif\"" {
		print "            - \"-log-stdout\""
		next
	}
	$0 == "          volumeMounts:" {
		skip_mounts = 1
		next
	}
	skip_mounts && $0 == "          resources:" {
		skip_mounts = 0
	}
	skip_mounts { next }
	$0 == "        - name: logwisp" {
		skip_sidecar = 1
		next
	}
	skip_sidecar && $0 == "      volumes:" {
		skip_sidecar = 0
		skip_volumes = 1
		next
	}
	skip_sidecar { next }
	skip_volumes && $0 == "---" {
		skip_volumes = 0
		print
		next
	}
	skip_volumes { next }
	{ print }
'
