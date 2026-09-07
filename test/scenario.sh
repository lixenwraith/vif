#!/bin/sh
# Named game setups, for verifying behaviour by hand on a dev machine.
#
#   ./test/scenario.sh <name> [args...]
#   ./test/scenario.sh list
#
# Scenarios marked (auto) assert and print PASS/FAIL; the rest launch something and
# leave it in the foreground. See test/README.md.
set -eu

BIN=${BIN:-./bin/vif}
HOST=${HOST:-127.0.0.1}
PORT=${PORT:-7777}
PROBE_PORT=${PROBE_PORT:-7778}
IMAGE=${IMAGE:-vi-fighter:dev}

fail() { printf 'FAIL %s\n' "$*" >&2; exit 1; }
pass() { printf 'PASS %s\n' "$*"; }
note() { printf '\n== %s\n' "$*"; }

need_bin() {
	[ -x "$BIN" ] || fail "$BIN not built; run: make dev"
}

# probe_get fetches one probe path, preferring curl and falling back to the Go
# binary's own toolchain-free reach. A dev box has one of the two.
probe_get() {
	if command -v curl >/dev/null 2>&1; then
		curl -sS --max-time 5 "http://$HOST:$PROBE_PORT$1"
	else
		fail "curl not found; install it or read the probe by hand"
	fi
}

# wait_for evaluates a condition in this shell — not a subshell — so it can use the
# helpers above. Polls for at most N seconds.
wait_for() {
	seconds=$1; shift
	i=0
	while [ "$i" -lt "$((seconds * 10))" ]; do
		if eval "$*" >/dev/null 2>&1; then return 0; fi
		sleep 0.1
		i=$((i + 1))
	done
	return 1
}

alive() { kill -0 "$1" 2>/dev/null; }
gone() { ! kill -0 "$1" 2>/dev/null; }

# serve_bg starts a dedicated host with the given extra flags, setting SERVE_PID and
# LOG. Not a command substitution: that would run it in a subshell and lose both.
serve_bg() {
	LOG=$(mktemp)
	"$BIN" -serve "$HOST:$PORT" -probe "$HOST:$PROBE_PORT" \
		-d -size 120x40 -log-stdout -lv info "$@" >"$LOG" 2>&1 &
	SERVE_PID=$!
}

# guest_bg starts one scripted guest, keeping its output so a scenario that ends
# unexpectedly can say whether the guest left or was left. GUEST_LOG is the file.
guest_bg() {
	GUEST_LOG=$(mktemp)
	"$BIN" -script script/sparring-guest.toml -join "$HOST:$PORT" >"$GUEST_LOG" 2>&1 &
	GUEST_PID=$!
}

# Every line here has to succeed: under `set -e` a failing command in an EXIT trap
# becomes the script's exit status, and a process that already ended is the normal
# case rather than an error.
cleanup() {
	if [ -n "${GUEST_PID:-}" ]; then kill -9 "$GUEST_PID" 2>/dev/null || true; fi
	if [ -n "${SERVE_PID:-}" ]; then kill -9 "$SERVE_PID" 2>/dev/null || true; fi
	return 0
}
trap cleanup EXIT INT TERM

case "${1:-}" in

list|'')
	cat <<'EOF'
Interactive (runs in the foreground until you stop it)
  solo              single player, embedded config
  host [players]    interactive host; with no argument it starts on its first
                    guest and admits the whole roster, with one it is a party
                    of that size that starts together
  join [addr]       join a host
  serve [players]   dedicated host, runs until Ctrl-C  (no lifetime bounds)
  serve-fleet       dedicated host with the deployed 90s/90s/20s bounds
  pair              scripted host + scripted guest, both headless
  watch             scripted host presented on this terminal; join it by hand

Observed (runs a scenario and prints what happened; asserts nothing)
  probe             read /health and /metrics of a running -serve
  guests <n> [addr] launch n scripted guests against an address
  host-loss [n]     scripted host + n scripted guests, then kill the host and
                    report what each guest concluded  (AUTHORITY=migrate|host)
  vacant            watch a dedicated host park when its last guest leaves

Automated (assert, and used by `all`)
  check             validate every shipped config tree
  lifetime          unclaimed expiry, then vacancy expiry
  drain             SIGTERM drains instead of cutting a match
  identity          a peer running a different build is refused (runs the tests)
  all               every automated scenario above

Container (needs docker or podman; not part of `all`)
  image             build the container image and run its own -check
EOF
	;;

solo)
	need_bin; exec "$BIN" -d
	;;

host)
	# No -players unless one is asked for. Unset is not "two": it is the whole
	# roster, starting on the first guest, with the rest arriving through the
	# mid-run gate. An explicit count is the other meaning of the same flag — a
	# party that says how big it is starts together.
	need_bin
	note "guests join with: $0 join $HOST:$PORT"
	if [ -n "${2:-}" ]; then
		exec "$BIN" -d -host "$HOST:$PORT" -players "$2"
	fi
	exec "$BIN" -d -host "$HOST:$PORT"
	;;

join)
	need_bin; exec "$BIN" -join "${2:-$HOST:$PORT}"
	;;

guests)
	# n scripted guests against one address, for a roster bigger than the number of
	# terminals to hand. Ctrl-C takes them all down.
	need_bin
	n=${2:?usage: $0 guests <n> [addr]}
	addr=${3:-$HOST:$PORT}
	note "$n scripted guests joining $addr; Ctrl-C ends them"
	i=0
	while [ "$i" -lt "$n" ]; do
		"$BIN" -script script/sparring-guest.toml -join "$addr" >/dev/null 2>&1 &
		i=$((i + 1))
	done
	wait
	;;

watch)
	need_bin
	note "scripted host on this terminal; join it with: $0 join $HOST:$PORT"
	exec "$BIN" -script script/sparring-host.toml -watch -host "$HOST:$PORT"
	;;

serve)
	# The case that looks like a hang and is not: with no lifetime bounds a
	# dedicated host waits forever, because the operator who started it is its
	# supervisor. Ctrl-C ends it; see serve-fleet for the bounded shape. A roster
	# that empties parks its clock rather than simulating an empty world, so a tick
	# counter that stops moving here is the session waiting rather than a stall.
	need_bin
	note "no lifetime bounds: this runs until Ctrl-C. Probe: $0 probe"
	set -- "$@" # keep $2 addressable under set -u
	if [ -n "${2:-}" ]; then
		exec "$BIN" -serve "$HOST:$PORT" -probe "$HOST:$PROBE_PORT" -players "$2" \
			-d -size 120x40 -log-stdout -lv info -ls all
	fi
	exec "$BIN" -serve "$HOST:$PORT" -probe "$HOST:$PROBE_PORT" \
		-d -size 120x40 -log-stdout -lv info -ls all
	;;

serve-fleet)
	need_bin
	note "ends 90s after start if nobody joins, or 90s after the last guest leaves"
	note "join it with: $BIN -join vif://$HOST:$PORT/${NAME:-7f3c1a}"
	exec "$BIN" -serve "$HOST:$PORT" -probe "$HOST:$PROBE_PORT" \
		-d -size 120x40 -log-stdout -lv info -name "${NAME:-7f3c1a}" \
		-players "${PLAYERS:-4}" -first-join 90s -empty 90s -drain 20s
	;;

probe)
	note "/health"; probe_get /health
	note "/metrics (first 20)"; probe_get /metrics | head -20
	;;

pair)
	need_bin
	note "scripted host and guest, both headless"
	"$BIN" -script script/sparring-host.toml -host "$HOST:$PORT" -players 2 &
	h=$!
	sleep 2
	"$BIN" -script script/sparring-guest.toml -join "$HOST:$PORT" || true
	wait $h || true
	;;

host-loss)
	# The authority question, run end to end: a host and n guests, then the host
	# goes. What each guest concludes is the whole output, because the two answers
	# are legitimate and which one is right is a deployment decision.
	#
	#   AUTHORITY=migrate  the roster's next survivor takes the term
	#   AUTHORITY=host     nobody does; every survivor plays on alone
	#
	# Read the result knowing what migration can and cannot do: a successor authors
	# but does not listen, and the handoff never reaches a guest that had no link to
	# it — so in a star each survivor ends up in a game of its own either way, and
	# the difference is only which of them believes it is hosting one.
	need_bin
	n=${2:-2}
	auth=${AUTHORITY:-migrate}
	D=$(mktemp -d)
	note "host + $n guests, -authority $auth; logs in $D"
	# -players sizes the lobby so every guest starts at tick zero. An authored
	# script is anchored to absolute ticks, so one that joined mid-run would find
	# its first action already past and stop — see doc/runtime.md §1.1.
	"$BIN" -script script/sparring-host.toml -host "$HOST:$PORT" -authority "$auth" \
		-players "$((n + 1))" >"$D/host.log" 2>&1 &
	HOST_PID=$!
	sleep 1
	i=1
	while [ "$i" -le "$n" ]; do
		"$BIN" -script script/sparring-guest.toml -join "$HOST:$PORT" \
			-log-stdout -lv info >"$D/guest$i.log" 2>&1 &
		eval "GUEST${i}_PID=\$!"
		i=$((i + 1))
	done
	sleep 5
	note "the host leaves"
	kill -9 "$HOST_PID" 2>/dev/null || true
	sleep 8
	i=1
	while [ "$i" -le "$n" ]; do
		eval "pid=\$GUEST${i}_PID"
		if alive "$pid"; then state="still playing"; else state="exited"; fi
		printf '\n-- guest %d (%s)\n' "$i" "$state"
		grep -oE '"msg":"(authority handed off|continuing locally|cursor despawn)"[^}]*' \
			"$D/guest$i.log" | tail -4
		kill -9 "$pid" 2>/dev/null || true
		i=$((i + 1))
	done
	;;

vacant)
	# A dedicated host with nobody in it stops its clock rather than simulating an
	# empty world, and starts a fresh run if nobody comes back. This shows the park;
	# the restart is a minute later, which is longer than a scenario should sit.
	need_bin
	serve_bg
	wait_for 15 'probe_get /health' || fail "probe never answered"
	guest_bg
	wait_for 15 'probe_get /health | grep -q clock=running' || fail "the session never started"
	note "the guest leaves"
	kill -9 "$GUEST_PID" 2>/dev/null || true
	GUEST_PID=
	wait_for 15 'probe_get /health | grep -q clock=paused' || fail "the emptied session kept ticking"
	note "/health with nobody in it"
	probe_get /health
	printf '\n'
	first=$(probe_get /health | tr ' ' '\n' | grep '^tick=')
	sleep 3
	second=$(probe_get /health | tr ' ' '\n' | grep '^tick=')
	note "tick after 3s parked: $first -> $second"
	;;

check)
	need_bin
	for tree in "" "-g config/main" "-g config/td"; do
		# shellcheck disable=SC2086
		"$BIN" -check $tree >/dev/null || fail "config check: ${tree:--d embedded}"
	done
	pass "every shipped config tree resolves"
	;;

lifetime)
	need_bin
	note "a session nobody joins ends itself"
	LOG=$(mktemp)
	start=$(date +%s)
	"$BIN" -serve "$HOST:$PORT" -d -size 120x40 -log-stdout -lv info \
		-first-join 3s >"$LOG" 2>&1 || fail "unclaimed session exited non-zero"
	elapsed=$(( $(date +%s) - start ))
	[ "$elapsed" -ge 3 ] || fail "ended after ${elapsed}s, before its own window closed"
	[ "$elapsed" -le 20 ] || fail "took ${elapsed}s to end a 3s window"
	grep -q 'no guest connected' "$LOG" || fail "the session did not say why it ended: $LOG"
	pass "unclaimed session exited 0 after ${elapsed}s"

	note "a session whose last guest leaves ends itself"
	serve_bg -first-join 30s -empty 4s
	wait_for 15 'probe_get /health' || fail "probe never answered"
	guest_bg
	wait_for 15 'probe_get /health | grep -q clock=running' || fail "the session never started"
	kill -9 "$GUEST_PID" 2>/dev/null || true
	wait_for 30 'gone "$SERVE_PID"' || fail "the emptied session did not end"
	grep -q 'roster empty for' "$LOG" || fail "the session did not say why it ended: $LOG"
	pass "emptied session exited on its vacancy grace"
	;;

drain)
	need_bin
	serve_bg -first-join 30s -empty 5m -drain 6s
	wait_for 15 'probe_get /health' || fail "probe never answered"
	guest_bg
	wait_for 15 'probe_get /health | grep -q clock=running' || fail "the session never started"

	kill -TERM "$SERVE_PID"
	sleep 1
	body=$(probe_get /health) || fail "the probe stopped answering during the drain"
	echo "$body" | grep -q 'live=true' || fail "a draining session reported itself dead: $body"
	echo "$body" | grep -q 'ready=false' || fail "a draining session still reported itself ready: $body"
	echo "$body" | grep -q 'phase=draining' || fail "the probe does not say it is draining: $body"
	alive "$SERVE_PID" || fail "the signal cut the match instead of draining it"

	wait_for 30 'gone "$SERVE_PID"' || fail "the drain never ended"
	# The claim is that the drain waited out its deadline *holding* the guest, so a
	# guest that left first makes the run prove nothing rather than fail a promise
	# the host kept. Its own log says which happened.
	if ! grep -q 'drain deadline' "$LOG"; then
		if grep -q 'roster empty\|drained' "$LOG"; then
			fail "the guest left during the drain, so the deadline was never reached; guest log: $GUEST_LOG, host log: $LOG"
		fi
		fail "the session did not say why it ended: $LOG"
	fi
	pass "SIGTERM drained, kept playing, then exited on its deadline"
	;;

identity)
	# A mismatch cannot be staged from a shell: a joiner adopts the host's seed,
	# configuration and corpus from the anchor, so two runs of one binary always
	# agree. What can differ is the build — a different protocol, simulation or
	# capture layout — and constructing one of those is a job for the tests.
	go test -count=1 -run 'TestTheHostRefuses|TestTheOfferCarries|TestADifferentCorpus|TestVerify|TestSessionFrom' \
		./internal/app/ ./internal/network/ >/dev/null || fail "identity refusal tests"
	pass "a peer running a different build or session is refused by the host"
	;;

image)
	command -v docker >/dev/null 2>&1 || command -v podman >/dev/null 2>&1 \
		|| fail "no container engine found"
	make image IMAGE_TAG="${IMAGE#*:}" >/dev/null || fail "image build"
	make image-check IMAGE_TAG="${IMAGE#*:}" >/dev/null || fail "image config check"
	pass "$IMAGE builds and validates its own config as a non-root read-only user"
	;;

all)
	for s in check lifetime drain identity; do
		note "$s"
		"$0" "$s"
	done
	;;

*)
	fail "unknown scenario '$1'; try: $0 list"
	;;
esac
