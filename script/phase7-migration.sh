#!/bin/sh
# Three-process acceptance for Phase 7 authority continuity, over real sockets.
#
# The unit suites prove the succession rule as a function, and the session suites
# prove the whole handoff over an in-process mesh, where a topology that is not a
# star can be expressed. This is the third thing neither can be: three operating
# system processes, real sockets, a real clock, a coordinator that is *killed*
# rather than stopped, and — where the machine allows it — a shaped link
# underneath.
#
#   script/phase7-migration.sh            # unshaped, needs nothing but jq
#   sudo script/phase7-migration.sh       # with tc netem stages, if tc is present
#
# What it needs: `jq` and a build. `tc` and root are optional: without them the
# run is unshaped and says so.
#
# Read the warning before running it as root: shaping applies to **all loopback
# traffic on the machine** for the length of the run. The qdisc is removed on
# exit, including on interrupt.
#
# WHAT THIS RUN CAN AND CANNOT SHOW
#
# The CLI dials one address, so three processes over sockets form a star: both
# participants link to the coordinator and to nothing else. Killing the centre of
# a star leaves each survivor able to reach one participant out of three, which is
# not a strict majority — so the succession opens, finds nothing eligible, and
# falls back to local continuation. That is a real acceptance of requirement 4:
# a partition that cannot reach a majority does not elect, it says so, and it must
# not quietly produce two authorities.
#
# The path where a successor *is* elected needs a topology a star is not, and the
# CLI does not build one — multi-link beyond what the relay role needs is an
# explicit non-goal. That half is proved by the mesh suite in internal/app, which
# can express a chain and a full graph. This script says which half it ran.
set -eu

DEV=${DEV:-lo}
ADDR=${ADDR:-127.0.0.1:7777}
LOGDIR=${LOGDIR:-log}
STAGE_SECONDS=${STAGE_SECONDS:-10}
BIN=${BIN:-bin/vif}

die() { echo "phase7-migration: $*" >&2; exit 1; }
note() { echo "== $*"; }

command -v jq >/dev/null 2>&1 || die "jq not found"
[ -x "$BIN" ] || die "$BIN not built; run 'make dev' or 'make release' first"

SHAPING=0
if command -v tc >/dev/null 2>&1 && [ "$(id -u)" = 0 ]; then
	SHAPING=1
else
	note "tc or root unavailable: running unshaped"
fi

clear_shape() { [ "$SHAPING" = 1 ] && tc qdisc del dev "$DEV" root 2>/dev/null; return 0; }
shape() {
	[ "$SHAPING" = 1 ] || return 0
	clear_shape
	[ $# -eq 0 ] && return 0
	tc qdisc add dev "$DEV" root netem "$@"
}

HOST_PID=""
G1_PID=""
G2_PID=""
cleanup() {
	clear_shape
	[ -n "$HOST_PID" ] && kill "$HOST_PID" 2>/dev/null || true
	[ -n "$G1_PID" ] && kill "$G1_PID" 2>/dev/null || true
	[ -n "$G2_PID" ] && kill "$G2_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

mkdir -p "$LOGDIR"
clear_shape

note "coordinator: starting solo, opening the session at tick 200"
"$BIN" -script script/phase7-host.toml \
	-l="$LOGDIR/phase7-host" -lv info -ls afs -lt 100 -j >"$LOGDIR/phase7-host.out" 2>&1 &
HOST_PID=$!

# The coordinator runs flat out to tick 200 and only then binds, so the
# participants wait for the announcement rather than for a fixed delay. The log is
# the portable probe: /dev/tcp is a bash extension and this script runs under
# /bin/sh.
waited=0
while ! grep -q "hosting opened mid-run" "$LOGDIR"/phase7-host/*.jsonl 2>/dev/null; do
	waited=$((waited + 1))
	[ "$waited" -gt 300 ] && die "coordinator never opened $ADDR (see $LOGDIR/phase7-host.out)"
	sleep 0.1
	kill -0 "$HOST_PID" 2>/dev/null || die "coordinator exited early (see $LOGDIR/phase7-host.out)"
done

note "participant 2: joining $ADDR"
"$BIN" -join "$ADDR" -script script/phase7-guest.toml \
	-l="$LOGDIR/phase7-p2" -lv info -ls afs -lt 100 -j >"$LOGDIR/phase7-p2.out" 2>&1 &
G1_PID=$!
sleep 2

note "participant 3: joining $ADDR"
"$BIN" -join "$ADDR" -script script/phase7-guest.toml \
	-l="$LOGDIR/phase7-p3" -lv info -ls afs -lt 100 -j >"$LOGDIR/phase7-p3.out" 2>&1 &
G2_PID=$!
sleep 2

run_stage() {
	name=$1
	shift
	note "stage: $name ${*:-(unshaped)}"
	shape "$@"
	i=0
	while [ "$i" -lt "$STAGE_SECONDS" ]; do
		sleep 1
		i=$((i + 1))
		kill -0 "$G1_PID" 2>/dev/null || return 0
	done
}

# The same stages Phase 5 and 6 apply, so the retained ring fills under a link
# that is not perfect before anything is killed.
run_stage baseline
run_stage latency delay 60ms
run_stage loss delay 40ms loss 3%

note "killing the coordinator mid-storm"
kill -9 "$HOST_PID" 2>/dev/null || true
wait "$HOST_PID" 2>/dev/null || true
HOST_PID=""

# Long enough for the succession deadline to elapse: it is one convergence floor,
# and the survivors have to be seen deciding rather than caught mid-decision.
run_stage aftermath
run_stage recovery

note "waiting for the survivors to finish their tick budgets"
wait "$G1_PID" 2>/dev/null || true
wait "$G2_PID" 2>/dev/null || true
G1_PID=""; G2_PID=""
clear_shape

host_log=$(ls -1t "$LOGDIR"/phase7-host/*.jsonl 2>/dev/null | head -1)
p2_log=$(ls -1t "$LOGDIR"/phase7-p2/*.jsonl 2>/dev/null | head -1)
p3_log=$(ls -1t "$LOGDIR"/phase7-p3/*.jsonl 2>/dev/null | head -1)
[ -n "$host_log" ] || die "no coordinator log in $LOGDIR/phase7-host"
[ -n "$p2_log" ] || die "no participant 2 log in $LOGDIR/phase7-p2"
[ -n "$p3_log" ] || die "no participant 3 log in $LOGDIR/phase7-p3"

group() {
	jq -r --arg g "$1" 'select(.sub=="stat" and .fields.msg==$g)
	       | "\(.tick) \(.fields|del(.msg)|to_entries|map("\(.key)=\(.value)")|join(" "))"' "$2" | tail -3
}

echo
note "who each instance believes is authoring, at the end of its run"
for pair in "coordinator:$host_log" "participant 2:$p2_log" "participant 3:$p3_log"; do
	name=${pair%%:*}
	log=${pair#*:}
	printf '%s\n' "-- $name"
	group network.authority "$log"
done

echo
note "the succession as each survivor logged it"
jq -r 'select(.fields.msg=="authority lost; succession opened"
	or .fields.msg=="succession vote cast"
	or .fields.msg=="authority handed off"
	or .fields.msg=="no succession possible; continuing locally"
	or .fields.msg=="handoff refused"
	or .fields.msg=="authoritative artifact refused")
	| "\(.tick) \(.fields.msg) \(.fields|del(.msg)|to_entries|map("\(.key)=\(.value)")|join(" "))"' \
	"$p2_log" "$p3_log" || true

echo
note "the retention each survivor was eligible on, and what it answered from"
for pair in "coordinator:$host_log" "participant 2:$p2_log" "participant 3:$p3_log"; do
	name=${pair%%:*}
	log=${pair#*:}
	printf '%s\n' "-- $name"
	group snapshot.relay "$log"
	group snapshot.index "$log"
	group snapshot.repair "$log"
done

echo
note "the wire totals, which is what this run is for"
totals() {
	jq -r 'select(.sub=="stat" and (.fields.msg=="snapshot.index" or .fields.msg=="snapshot.repair"
	       or .fields.msg=="snapshot.correction" or .fields.msg=="network.authority"))
		| .fields
		| {manifest: ((.manifest_bytes_sent // 0) + (.manifest_bytes_received // 0)),
		   shard:    ((.shard_bytes_sent // 0) + (.shard_bytes_received // 0)),
		   body:     (.correction_bytes_sent // 0),
		   handoff:  (.handoff_bytes // 0),
		   term:     (.term // 0),
		   migrations: (.migrations // 0)}' "$2" |
	jq -s --arg side "$1" 'reduce .[] as $r ({manifest:0,shard:0,body:0,handoff:0,term:0,migrations:0};
		{manifest: ([.manifest, $r.manifest]|max),
		 shard:    ([.shard, $r.shard]|max),
		 body:     ([.body, $r.body]|max),
		 handoff:  ([.handoff, $r.handoff]|max),
		 term:     ([.term, $r.term]|max),
		 migrations: ([.migrations, $r.migrations]|max)})
		| "\($side): term \(.term), \(.migrations) handoff(s), manifest \(.manifest) bytes, shards \(.shard) bytes, whole bodies \(.body) bytes, succession \(.handoff) bytes"'
}
totals coordinator "$host_log"
totals "participant 2" "$p2_log"
totals "participant 3" "$p3_log"

echo
note "the invariant this run exists to check"
terms=$(jq -r 'select(.sub=="stat" and .fields.msg=="network.authority") | .fields.term // 0' \
	"$p2_log" "$p3_log" | sort -n | tail -1)
authorities=$(jq -r 'select(.sub=="stat" and .fields.msg=="network.authority" and (.fields.term // 0) > 1)
	| "\(.fields.term):\(.fields.authority)"' "$p2_log" "$p3_log" | sort -u | wc -l)
if [ "$terms" -le 1 ]; then
	echo "no succession was possible from a star, which is the documented fallback:"
	echo "both survivors continue locally and say so. See the header for why, and the"
	echo "mesh suite in internal/app for the elected-successor half."
	jq -r 'select(.sub=="stat" and .fields.msg=="network.authority")
		| "  fork=\(.fields.fork) host_lost=\(.fields.host_lost // "n/a") term=\(.fields.term)"' \
		"$p2_log" "$p3_log" | tail -2
else
	echo "term reached $terms with $authorities distinct (term, authority) pair(s);"
	echo "more than one pair for a term would be the split brain this phase forbids."
	[ "$authorities" -le 1 ] || die "two authorities claimed one term"
fi

echo
note "anything any half called a problem"
jq -c 'select(.level=="WARN" or .level=="ERROR") | {tick, level, fields}' \
	"$host_log" "$p2_log" "$p3_log" || true
