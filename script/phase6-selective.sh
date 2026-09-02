#!/bin/sh
# Two-process acceptance for the Phase 6 selective correction, over a real socket.
#
# The unit suites prove the index, the descent and the repair on captures, and the
# session suites prove the exchange over an in-process link. This is the third
# thing neither can be: two operating-system processes, a real socket, a real
# clock, and — where the machine allows it — a shaped link underneath.
#
#   script/phase6-selective.sh            # unshaped, needs nothing but jq
#   sudo script/phase6-selective.sh       # with tc netem stages, if tc is present
#
# What it needs: `jq` and a build. `tc` and root are optional: without them the
# run is unshaped and says so, which still answers the question this script is
# mainly for — what the wire actually carries when two processes agree.
#
# Read the warning before running it as root: shaping applies to **all loopback
# traffic on the machine** for the length of the run. The qdisc is removed on
# exit, including on interrupt.
set -eu

DEV=${DEV:-lo}
ADDR=${ADDR:-127.0.0.1:7777}
LOGDIR=${LOGDIR:-log}
STAGE_SECONDS=${STAGE_SECONDS:-12}
BIN=${BIN:-bin/vif}

die() { echo "phase6-selective: $*" >&2; exit 1; }
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
GUEST_PID=""
cleanup() {
	clear_shape
	[ -n "$HOST_PID" ] && kill "$HOST_PID" 2>/dev/null || true
	[ -n "$GUEST_PID" ] && kill "$GUEST_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

mkdir -p "$LOGDIR"
clear_shape

note "host: starting solo, opening the session at tick 200"
"$BIN" -script script/phase6-host.toml \
	-l="$LOGDIR/phase6-host" -lv info -ls afs -lt 100 -j >"$LOGDIR/phase6-host.out" 2>&1 &
HOST_PID=$!

# The host runs flat out to tick 200 and only then binds, so the guest waits for
# the announcement rather than for a fixed delay. The log is the portable probe:
# /dev/tcp is a bash extension and this script runs under /bin/sh.
waited=0
while ! grep -q "hosting opened mid-run" "$LOGDIR"/phase6-host/*.jsonl 2>/dev/null; do
	waited=$((waited + 1))
	[ "$waited" -gt 300 ] && die "host never opened $ADDR (see $LOGDIR/phase6-host.out)"
	sleep 0.1
	kill -0 "$HOST_PID" 2>/dev/null || die "host exited early (see $LOGDIR/phase6-host.out)"
done

note "guest: joining $ADDR"
"$BIN" -join "$ADDR" -script script/phase6-guest.toml \
	-l="$LOGDIR/phase6-guest" -lv info -ls afs -lt 100 -j >"$LOGDIR/phase6-guest.out" 2>&1 &
GUEST_PID=$!
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
		kill -0 "$GUEST_PID" 2>/dev/null || return 0
	done
}

run_stage baseline
run_stage latency delay 60ms
run_stage loss delay 40ms loss 3%
run_stage bandwidth rate 512kbit delay 40ms
run_stage recovery

note "waiting for both halves to finish their tick budgets"
wait "$GUEST_PID" 2>/dev/null || true
wait "$HOST_PID" 2>/dev/null || true
HOST_PID=""; GUEST_PID=""
clear_shape

host_log=$(ls -1t "$LOGDIR"/phase6-host/*.jsonl 2>/dev/null | head -1)
guest_log=$(ls -1t "$LOGDIR"/phase6-guest/*.jsonl 2>/dev/null | head -1)
[ -n "$host_log" ] || die "no host log in $LOGDIR/phase6-host"
[ -n "$guest_log" ] || die "no guest log in $LOGDIR/phase6-guest"

group() {
	jq -r --arg g "$1" 'select(.sub=="stat" and .fields.msg==$g)
	       | "\(.tick) \(.fields|del(.msg)|to_entries|map("\(.key)=\(.value)")|join(" "))"' "$2" | tail -3
}

echo
note "what the host put on the wire (last samples)"
group snapshot.index "$host_log"
group snapshot.repair "$host_log"
group snapshot.correction "$host_log"
group snapshot.cadence "$host_log"

echo
note "what the guest received and did with it"
group snapshot.index "$guest_log"
group snapshot.repair "$guest_log"
group snapshot.replay "$guest_log"
group snapshot.correction "$guest_log"

echo
note "the wire totals, which is what this run is for"
totals() {
	jq -r --arg side "$1" '
		select(.sub=="stat" and (.fields.msg=="snapshot.index" or .fields.msg=="snapshot.repair"
		       or .fields.msg=="snapshot.correction"))
		| .fields
		| {manifest: (.manifest_bytes_sent // .manifest_bytes_received // 0),
		   shard:    (.shard_bytes_sent // .shard_bytes_received // 0),
		   body:     (.correction_bytes_sent // 0),
		   hashonly: (.corrections_hash_only // 0)}' "$2" |
	jq -s --arg side "$1" 'reduce .[] as $r ({manifest:0,shard:0,body:0,hashonly:0};
		{manifest: ([.manifest, $r.manifest]|max),
		 shard:    ([.shard, $r.shard]|max),
		 body:     ([.body, $r.body]|max),
		 hashonly: ([.hashonly, $r.hashonly]|max)})
		| "\($side): manifest \(.manifest) bytes, shards \(.shard) bytes, whole bodies \(.body) bytes, hash-only corrections \(.hashonly)"'
}
totals host "$host_log"
totals guest "$guest_log"

echo
note "anything either half called a problem"
jq -c 'select(.level=="WARN" or .level=="ERROR") | {tick, level, fields}' "$host_log" "$guest_log" || true
