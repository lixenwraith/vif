#!/bin/sh
# Staged link shaping for the Phase 5 adaptive cadence, over a real socket.
#
# This is the manual acceptance the plan asks for, driven rather than typed:
# shape the link down in stages and confirm play degrades smoothly — the cadence
# falls, prediction carries more, the correction magnitude rises but stays
# bounded, and nothing forks or disconnects. It runs the checked-in phase5 script
# pair, walks `tc netem` through four shapes, clears them, and reads the answer
# out of both journals.
#
#   sudo script/phase5-linkshape.sh
#
# What it needs: iproute2's `tc`, root (a qdisc on `lo` is a system-wide change),
# `jq`, and a build. It refuses rather than half-runs if any is missing.
#
# Read the warning first: this shapes **all loopback traffic on the machine** for
# the length of the run. Nothing else that talks over `lo` — a database, another
# test suite, an editor's language server — will enjoy it. The qdisc is removed
# on exit, including on interrupt.
#
# The four stages are the four the plan names, in the order a link actually
# degrades: distance, then instability, then loss, then the bottleneck. Each one
# runs long enough for the estimator to settle (it smooths over eight probes at
# five a second) and for the controller's stepped recovery to be visible between
# them.
set -eu

DEV=${DEV:-lo}
ADDR=${ADDR:-127.0.0.1:7777}
LOGDIR=${LOGDIR:-log}
STAGE_SECONDS=${STAGE_SECONDS:-20}
BIN=${BIN:-bin/vif}

die() { echo "phase5-linkshape: $*" >&2; exit 1; }
note() { echo "== $*"; }

command -v tc  >/dev/null 2>&1 || die "tc not found; install iproute2"
command -v jq  >/dev/null 2>&1 || die "jq not found"
[ "$(id -u)" = 0 ] || die "root is required to install a qdisc on $DEV"

clear_shape() { tc qdisc del dev "$DEV" root 2>/dev/null || true; }
shape() {
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

[ -x "$BIN" ] || die "$BIN not built; run 'make dev' or 'make release' first"
mkdir -p "$LOGDIR"
clear_shape

note "host: starting solo, opening the session at tick 400"
"$BIN" -script script/phase5-host.toml \
	-l="$LOGDIR/phase5-host" -lv info -ls afs -lt 100 -j >"$LOGDIR/phase5-host.out" 2>&1 &
HOST_PID=$!

# The host runs flat out to tick 400 and only then binds, so the guest waits for
# the socket rather than for a fixed delay.
waited=0
while ! (exec 3<>"/dev/tcp/${ADDR%:*}/${ADDR##*:}") 2>/dev/null; do
	waited=$((waited + 1))
	[ "$waited" -gt 300 ] && die "host never opened $ADDR (see $LOGDIR/phase5-host.out)"
	sleep 0.1
	kill -0 "$HOST_PID" 2>/dev/null || die "host exited early (see $LOGDIR/phase5-host.out)"
done
exec 3<&- 2>/dev/null || true

note "guest: joining $ADDR"
"$BIN" -join "$ADDR" -script script/phase5-guest.toml \
	-l="$LOGDIR/phase5-guest" -lv info -ls afs -lt 100 -j >"$LOGDIR/phase5-guest.out" 2>&1 &
GUEST_PID=$!
sleep 3

# The stages. `rate` is netem's own shaper, so a bandwidth reduction needs no
# second qdisc and stacks with the delay it is measured under.
run_stage() {
	name=$1
	shift
	note "stage: $name ${*:-(unshaped)}"
	shape "$@"
	i=0
	while [ "$i" -lt "$STAGE_SECONDS" ]; do
		sleep 1
		i=$((i + 1))
		kill -0 "$GUEST_PID" 2>/dev/null || die "guest exited during stage $name"
	done
}

run_stage baseline
run_stage latency   delay 80ms
run_stage jitter    delay 80ms 40ms distribution normal
run_stage loss      delay 40ms loss 3%
run_stage bandwidth rate 512kbit delay 40ms
run_stage recovery

note "waiting for both halves to finish their tick budgets"
wait "$GUEST_PID" 2>/dev/null || true
wait "$HOST_PID" 2>/dev/null || true
HOST_PID=""; GUEST_PID=""
clear_shape

host_log=$(ls -1t "$LOGDIR"/phase5-host*.log 2>/dev/null | head -1) ||
	die "no host journal in $LOGDIR"
guest_log=$(ls -1t "$LOGDIR"/phase5-guest*.log 2>/dev/null | head -1) ||
	die "no guest journal in $LOGDIR"

echo
note "the operating point over the run (host)"
jq -r 'select(.sub=="stat" and (.fields.msg=="snapshot.cadence" or .fields.msg=="network.link"))
       | "\(.tick) \(.fields.msg) \(.fields|del(.msg)|to_entries|map("\(.key)=\(.value)")|join(" "))"' \
	"$host_log" | tail -40

echo
note "the correction magnitude over the run (guest) — this is the trend that matters"
jq -r 'select(.sub=="stat" and .fields.msg=="snapshot.correction")
       | "\(.tick) \(.fields|del(.msg)|to_entries|map("\(.key)=\(.value)")|join(" "))"' \
	"$guest_log" | tail -40

echo
note "anything either half called a problem"
jq -c 'select(.level=="WARN" or .level=="ERROR") | {tick, level, fields}' "$host_log" "$guest_log" || true

echo
note "the floor, which no stage may cross"
floor=$(jq -r 'select(.sub=="stat" and .fields.msg=="snapshot.cadence")
               | .fields.keyframe_period_ticks // empty' "$host_log" | sort -n | tail -1)
age=$(jq -r 'select(.sub=="stat" and .fields.msg=="snapshot.cadence")
             | .fields.keyframe_age_ticks // empty' "$guest_log" | sort -n | tail -1)
echo "longest keyframe period the host planned:      ${floor:-unknown} ticks"
echo "longest the guest actually waited for a world:  ${age:-unknown} ticks"
echo "the convergence floor is SnapshotFloorKeyframeTicks (60). The first number"
echo "larger than that is a defect; the second larger than 60 + SnapshotFloorGraceTicks"
echo "(40) is the condition the guest is expected to have reported as a breach."

echo
note "done; the qdisc on $DEV has been removed"
