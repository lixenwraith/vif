#!/bin/sh
# End-to-end setups, for verifying behaviour by hand on a dev machine.
#
#   ./script/test.sh <name> [args...]
#   ./script/test.sh list
#
# Scenarios marked (auto) assert and print PASS/FAIL; the rest launch something and
# leave it in the foreground. See script/README.md.
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

# own_ports refuses to start a host while something already answers on the probe
# port. A leftover host passes the probe check, takes the guest and logs to its own
# directory, which reads as this host never serving.
own_ports() {
	if probe_get /health >/dev/null 2>&1; then
		fail "something already answers on $HOST:$PROBE_PORT; stop it or set PORT and PROBE_PORT"
	fi
}

# serve_bg starts a dedicated host with the given extra flags, setting SERVE_PID and
# LOG. Not a command substitution: that would run it in a subshell and lose both.
serve_bg() {
	own_ports
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

# pty_ok reports whether this machine has a script(1) that can give vif a terminal.
# The two flavours take their arguments in opposite orders; a machine with neither
# is missing the harness rather than the game, so its scenarios skip.
pty_ok() {
	script -qec true /dev/null >/dev/null 2>&1 && return 0
	script -q /dev/null true >/dev/null 2>&1 && return 0
	return 1
}

# keyfile writes the keystroke script one terminal is driven by, and echoes its path.
keyfile() {
	f=$(mktemp)
	printf '%s\n' "$@" > "$f"
	echo "$f"
}

# pty_bg runs one command under a pty in the background, feeding it a keyfile.
# PTY_PID is the job; a caller driving two terminals keeps its own copy.
pty_bg() {
	keys=$1
	shift
	if script -qec true /dev/null >/dev/null 2>&1; then
		sh "$keys" | script -qec "$*" /dev/null >/dev/null 2>&1 &
	else
		# shellcheck disable=SC2086
		sh "$keys" | script -q /dev/null $* >/dev/null 2>&1 &
	fi
	PTY_PID=$!
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
  check             validate every shipped resource tree
  bundle            the release wad archive extracts to a working config root
  scenario          :n <name> rebuilds the run on another scenario and back
  transfer          a guest with no root receives the session's scenario
  maxmap            a guest joins a host serving the largest map the grid holds
  follow            a host changes scenario and its guest rebuilds with it
  corpus            a guest reading its own content joins a host reading other
  fleet             the session template and the allocator render one workload
  deploy            the node install, the manifests and a mounted scenario
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
	note "join it with: $BIN -join $HOST:$PORT"
	exec "$BIN" -serve "$HOST:$PORT" -probe "$HOST:$PROBE_PORT" \
		-d -size 120x40 -log-stdout -lv info -authority host \
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
	# -players sizes the lobby so every guest starts at tick zero.
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
	for tree in "-d" "-config-dir wad" "-config-dir wad -s td"; do
		# shellcheck disable=SC2086
		"$BIN" -check $tree >/dev/null || fail "resource check: $tree"
	done
	pass "every shipped resource tree resolves"
	;;

bundle)
	# What the release publishes is the wad as the config root it extracts into, so
	# the archive is only right if an empty root filled from it resolves. A binary
	# on its own reaches the embedded scenario and nothing else.
	need_bin
	ARCHIVE=$(mktemp -d)/wad.tar.gz; ROOT=$(mktemp -d)
	make wad-archive WAD_ARCHIVE="$ARCHIVE" >/dev/null || fail "make wad-archive"
	tar -C "$ROOT" -xzf "$ARCHIVE" || fail "the archive did not extract"
	for tree in "-s main" "-s td" ""; do
		# shellcheck disable=SC2086
		"$BIN" -check -config-dir "$ROOT" $tree >/dev/null \
			|| fail "the extracted root did not resolve: $tree"
	done
	"$BIN" -check -config-dir "$ROOT" | grep -q "^content ok: $ROOT/content" \
		|| fail "the archive carries no corpus for a player who has none"
	rm -rf "$ROOT" "$(dirname "$ARCHIVE")"
	pass "the release wad archive extracts to a config root that resolves"
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

	# What the signal has to prove is that it drains rather than kills, so the
	# clock still moving after it is the claim; the guest is not part of it.
	before=$(echo "$body" | sed -n 's/^tick=//p')
	sleep 1
	after=$(probe_get /health | sed -n 's/^tick=//p') || fail "the probe stopped answering"
	[ -n "$before" ] && [ -n "$after" ] || fail "the probe reported no tick: $body"
	[ "$after" -gt "$before" ] || fail "the clock stopped during the drain: $before -> $after"

	wait_for 30 'gone "$SERVE_PID"' || fail "the drain never ended"
	grep -qE 'drain deadline|roster empty|drained' "$LOG" \
		|| fail "the session did not say why it ended: $LOG"
	pass "SIGTERM drained, kept simulating, then ended itself"
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

transfer)
	# A guest with no configuration root of its own joins a host playing an
	# installed scenario. What proves the transfer is that the guest could not have
	# resolved that scenario locally: its root is empty.
	need_bin
	[ -d wad/scenario/main ] || fail "wad/scenario/main is not in this checkout"
	ROOT=$(mktemp -d); HL=$(mktemp -d); GL=$(mktemp -d)
	own_ports
	"$BIN" -serve "$HOST:$PORT" -probe "$HOST:$PROBE_PORT" -config-dir wad -s main \
		-size 120x40 -players 1 -first-join 30s -empty 15s \
		-l="$HL" -lv info -ls app >/dev/null 2>&1 &
	SERVE_PID=$!
	wait_for 15 'probe_get /health' || fail "the host never answered its probe"
	timeout 20 "$BIN" -script script/sparring-guest.toml -join "$HOST:$PORT" \
		-config-dir "$ROOT" -l="$GL" -lv info -ls app >/dev/null 2>&1 || true
	grep -qh '"msg":"scenario served"' "$HL"/*.jsonl 2>/dev/null \
		|| fail "the host never served its scenario: $HL"
	grep -qh '"msg":"scenario received"' "$GL"/*.jsonl 2>/dev/null \
		|| fail "the guest never received a scenario: $GL"
	grep -qh '"msg":"join installed the session world"' "$GL"/*.jsonl 2>/dev/null \
		|| fail "the guest received a scenario but never joined: $GL"
	rm -rf "$ROOT" "$HL" "$GL"
	pass "a guest with no root received the session's scenario and joined on it"
	;;

maxmap)
	# td is 500x250, the most cells the grid holds, and fills them with a maze, so
	# its capture is ten megabytes of JSON that compresses to half of one. Only the
	# bytes that travel are bounded by the wire ceiling; a join refused here means
	# the plain body is being measured against it again.
	need_bin
	[ -d wad/scenario/td ] || fail "wad/scenario/td is not in this checkout"
	ROOT=$(mktemp -d); HL=$(mktemp -d); GL=$(mktemp -d)
	own_ports
	"$BIN" -serve "$HOST:$PORT" -probe "$HOST:$PROBE_PORT" -config-dir wad -s td \
		-size 120x40 -players 1 -first-join 30s -empty 15s \
		-l="$HL" -lv info -ls app >/dev/null 2>&1 &
	SERVE_PID=$!
	wait_for 20 'probe_get /health' || fail "the host never answered its probe"
	# The guest runs until the timeout; its log is what is asserted.
	timeout 40 "$BIN" -script script/sparring-guest.toml -join "$HOST:$PORT" \
		-config-dir "$ROOT" -l="$GL" -lv info -ls app >/dev/null 2>&1 || true
	grep -qh '"msg":"join installed the session world"' "$GL"/*.jsonl 2>/dev/null \
		|| fail "the guest never installed the session world: $GL"
	# A keyframe correction carries the same whole world the join did, so a second
	# install is what proves the session carries it and not just the handshake.
	installs=$(grep -ho '"msg":"capture installed"' "$GL"/*.jsonl 2>/dev/null | wc -l)
	[ "${installs:-0}" -gt 1 ] \
		|| fail "the world arrived once and was never corrected: $GL"
	rm -rf "$ROOT" "$HL" "$GL"
	pass "a guest joined and was corrected on the largest map the spatial grid holds"
	;;

scenario)
	# `:n <name>` rebuilds the run, so the only honest witness is a real terminal
	# going through it.
	need_bin
	[ -d wad/scenario/td ] || fail "wad/scenario/td is not in this checkout"
	pty_ok || { echo "SKIP scenario: no usable script(1) for a pty" >&2; exit 0; }
	LOG=$(mktemp -d)
	KEYS=$(keyfile "sleep 1" "printf ':n td\\r'" "sleep 3" "printf ':n main\\r'" "sleep 3" "printf ':q\\r'" "sleep 1")
	pty_bg "$KEYS" "$BIN -config-dir wad -l=$LOG -lv info -ls app"
	wait "$PTY_PID" 2>/dev/null || true
	got=$(grep -ho '"digest":"[0-9a-f]*"' "$LOG"/*.jsonl 2>/dev/null | sed 's/.*:"//;s/"//' | uniq)
	count=$(printf '%s\n' "$got" | grep -c .)
	[ "$count" -ge 3 ] || fail "expected main, td and main again; the log named $count scenario(s): $(printf '%s ' $got)"
	first=$(printf '%s\n' "$got" | head -1)
	[ "$(printf '%s\n' "$got" | sed -n 3p)" = "$first" ] || fail "the run did not come back to the scenario it started on"
	rm -rf "$LOG" "$KEYS"
	pass "a run changed scenario and came back, rebuilding each time"
	;;

corpus)
	# Glyphs are player domain, so two machines with different content installed are
	# still in one session. The guest's corpus is one file the host does not have;
	# nothing is reconciled and nothing is refused.
	need_bin
	ROOT=$(mktemp -d); HL=$(mktemp -d); GL=$(mktemp -d)
	mkdir -p "$ROOT/content"
	cp -a wad/scenario wad/image "$ROOT/"
	printf 'package solo\n\nfunc OnlyHere() int { return 42 }\n' >"$ROOT/content/solo.go.txt"
	"$BIN" -check -config-dir "$ROOT" -s main | grep -q "content ok: $ROOT/content (1 files" \
		|| fail "the guest root did not resolve to its own single-file corpus"

	own_ports
	"$BIN" -serve "$HOST:$PORT" -probe "$HOST:$PROBE_PORT" -config-dir wad -s main \
		-size 120x40 -players 1 -first-join 30s -empty 15s \
		-l="$HL" -lv info -ls all >/dev/null 2>&1 &
	SERVE_PID=$!
	wait_for 15 'probe_get /health' || fail "the host never answered its probe"
	timeout 25 "$BIN" -script script/sparring-guest.toml -join "$HOST:$PORT" \
		-config-dir "$ROOT" -l="$GL" -lv info -ls all >/dev/null 2>&1 || true

	grep -qh '"msg":"join installed the session world"' "$GL"/*.jsonl 2>/dev/null \
		|| fail "a guest reading its own corpus did not join: $GL"
	grep -qh 'content_id' "$GL"/*.jsonl "$HL"/*.jsonl 2>/dev/null \
		&& fail "the corpus was reconciled; it is the player's: $GL"
	rm -rf "$ROOT" "$HL" "$GL"
	pass "a guest reading its own corpus joined a host reading another"
	;;

follow)
	# The host changes scenario and every guest comes with it. Two real terminals,
	# because both sides restart and only a run Run owns has a loop to restart in.
	# The guest's root is empty, so the scenario it ends on can only have come off
	# the wire — which is the whole of what a session scenario change has to do.
	need_bin
	[ -d wad/scenario/blank ] || fail "wad/scenario/blank is not in this checkout"
	pty_ok || { echo "SKIP follow: no usable script(1) for a pty" >&2; exit 0; }
	HL=$(mktemp -d); GL=$(mktemp -d); ROOT=$(mktemp -d)
	HK=$(keyfile "sleep 7" "printf ':n blank\\r'" "sleep 12" "printf ':q\\r'" "sleep 2")
	GK=$(keyfile "sleep 24" "printf ':q\\r'" "sleep 2")
	pty_bg "$HK" "$BIN -host $HOST:$PORT -config-dir wad -s main -l=$HL -lv info -ls app"
	HOST_PTY=$PTY_PID
	sleep 2
	pty_bg "$GK" "$BIN -join $HOST:$PORT -config-dir $ROOT -l=$GL -lv info -ls app"
	wait "$HOST_PTY" 2>/dev/null || true
	wait "$PTY_PID" 2>/dev/null || true
	grep -qh '"msg":"session restarting"' "$GL"/*.jsonl 2>/dev/null \
		|| fail "the guest was never told the session was rebuilding: $GL"
	grep -qh '"msg":"hosting opened mid-run"' "$HL"/*.jsonl 2>/dev/null \
		|| fail "the rebuilt host never reopened its door: $HL"
	ends=$(grep -ho '"digest":"[0-9a-f]*"' "$GL"/*.jsonl 2>/dev/null | sed 's/.*:"//;s/"//' | uniq | tail -1)
	want=$(grep -ho '"digest":"[0-9a-f]*"' "$HL"/*.jsonl 2>/dev/null | sed 's/.*:"//;s/"//' | uniq | tail -1)
	[ -n "$want" ] && [ "$ends" = "$want" ] \
		|| fail "the guest ended on $ends, the host on $want"
	[ "$(grep -c '"msg":"join installed the session world"' "$GL"/*.jsonl 2>/dev/null)" -ge 2 ] \
		|| fail "the guest did not install the session world a second time: $GL"
	# The host's own player after the change, not before it. One process writes one
	# log across both runs, and the guest's arrival spawns a cursor too, so the
	# count only means something from the restart onward. A scenario that spawns no
	# cursor leaves the host watching its guests play, which is what blank used to do.
	# Slot 0 is the host's own; the guest's arrival spawns one too, so the count
	# alone says nothing.
	own=$(cat "$HL"/*.jsonl 2>/dev/null | awk '
		/"msg":"run restarting"/ { seen = 1; n = 0; next }
		seen && /"msg":"cursor spawn"/ && /"slot":0/ { n++ }
		END { print n + 0 }')
	[ "$own" -ge 1 ] || fail "the rebuilt host spawned no cursor of its own: $HL"
	rm -rf "$HL" "$GL" "$ROOT" "$HK" "$GK"
	pass "the host changed scenario and its guest rebuilt and rejoined on it"
	;;

fleet)
	# The template a person renders by hand and the one the allocator renders have
	# to be the same workload, or "the allocator is broken" and "the workload is
	# broken" stop being separable questions. Compares the arguments, the mounts and
	# the claims rather than the whole document, which carries a session id.
	command -v go >/dev/null 2>&1 || fail "go not found"
	rendered=$(SCENARIO=td LOG_LEVEL=debug PLAYERS=4 \
		./deploy/k3s/render-session.sh fleetcheck 31700 vi-fighter:dev 4 120x40)
	for want in \
		'"-config-dir"' '"-s"' '"td"' '"debug"' \
		'claimName: vif-fleet-wad' 'subPath: scenario' 'subPath: image' \
		'name: vif-session-env'
	do
		printf '%s' "$rendered" | grep -q -- "$want" \
			|| fail "the rendered template is missing $want"
	done
	go test ./tool/vif-allocator/ >/dev/null || fail "allocator tests"
	pass "the session template and the allocator agree on the fleet's workload"
	;;

deploy)
	# What the node needs to be true before a pod can start, none of which the
	# normal build exercises: the image's own build tag, a manifest that survives an
	# unset variable, the installer's report, and a scenario resolving through the
	# mount layout rather than out of the binary.
	command -v go >/dev/null 2>&1 || fail "go not found"
	headless=$(mktemp -d)
	CGO_ENABLED=0 go build -tags=vif_headless -o "$headless/vif" ./cmd/vif \
		|| fail "the image's build tag does not compile"

	# An unquoted placeholder that renders empty ends the line on a colon, which is
	# a YAML scanner error forty lines from the variable that caused it.
	grep -Rn 'image: \${' deploy/k3s/ \
		&& fail "an image placeholder is unquoted; an unset tag becomes a parse error"

	# The installer runs on a node with no container runtime and no wad root yet,
	# which is every node before its first install and this one after any build.
	if sudo -n true 2>/dev/null; then
		node=$(mktemp -d)/nested
		VIF_WAD_ROOT=$node/wad ./deploy/guest/update-vif-wad.sh >"$headless/install" 2>&1 \
			|| fail "the wad installer failed: $headless/install"
		grep -q '^  ok    main$' "$headless/install" \
			|| fail "scenarios were not validated: $headless/install"
		# scenario.toml is three levels under the root; a listing one level short
		# reports a correct install as an empty one.
		for name in blank main td; do
			grep -q "$node/wad/scenario/$name\$" "$headless/install" \
				|| fail "the installer did not report $name: $headless/install"
		done
		# A scenario that does not load is refused by name, and refused before the
		# swap: the tree already on the node is what a running match still reads.
		bad=$(mktemp -d)/wad
		cp -a wad "$bad"
		printf '\n[regions.broken]\nfile = "nope.toml"\n' >>"$bad/scenario/blank/scenario.toml"
		if VIF_WAD_ROOT=$node/wad ./deploy/guest/update-vif-wad.sh "$bad" \
			>"$headless/bad" 2>&1
		then
			fail "the installer accepted a scenario that does not load"
		fi
		grep -q 'scenario blank does not load' "$headless/bad" \
			|| fail "the refusal did not name the scenario: $headless/bad"
		[ -d "$node/wad/scenario/main" ] || fail "a refused tree still replaced the node's"
		# Only the categories the fleet serves are installed. A corpus on the node
		# would be a category nothing mounts and no session would read.
		[ -d "$node/wad/image" ] || fail "the installer skipped image/"
		[ ! -d "$node/wad/content" ] || fail "the installer put a corpus on the node"
		sudo rm -rf "$node" "$bad"
	else
		echo "  skip: the wad installer needs passwordless sudo"
	fi

	# The pod mounts scenario/ and image/ and nothing else, so prove a scenario
	# resolves from exactly that and names itself in the log the commissioning
	# check reads.
	mount=$(mktemp -d)/wad
	mkdir -p "$mount"
	cp -a wad/scenario wad/image "$mount/"
	"$headless/vif" -check -config-dir "$mount" -s main >"$headless/check" \
		|| fail "the init container's check does not resolve through the mount"
	grep -q '^scenario ok' "$headless/check" || fail "no scenario off the mount"
	# A pod reads no corpus: glyphs are player domain and a dedicated host spawns
	# none, so an installed content/ would be a category nothing asked for.
	grep -q '^content ok: embedded' "$headless/check" \
		|| fail "a session resolved a corpus off the volume: $headless/check"
	own_ports
	"$headless/vif" -serve "$HOST:$PORT" -probe "$HOST:$PROBE_PORT" -authority host \
		-l="$mount/log" -log-session-id=volume-check -lv=info \
		-config-dir="$mount" -s=main -first-join=3s -empty=3s -drain=3s >/dev/null 2>&1 \
		|| fail "the probe pod's arguments do not run"
	grep -q '"msg":"scenario"' "$mount/log/volume-check.jsonl" \
		|| fail "no scenario record; the commissioning check has nothing to read"
	grep -q '"name":"main"' "$mount/log/volume-check.jsonl" \
		|| fail "the session did not load main off the mount"
	rm -rf "$mount" "$headless"
	pass "the image builds, the manifests render, and a mounted scenario loads"
	;;

image)
	command -v docker >/dev/null 2>&1 || command -v podman >/dev/null 2>&1 \
		|| fail "no container engine found"
	make image IMAGE_TAG="${IMAGE#*:}" >/dev/null || fail "image build"
	make image-check IMAGE_TAG="${IMAGE#*:}" >/dev/null || fail "image config check"
	pass "$IMAGE builds and validates its own config as a non-root read-only user"
	;;

all)
	for s in check bundle scenario transfer follow corpus fleet deploy lifetime drain identity; do
		note "$s"
		"$0" "$s"
	done
	;;

*)
	fail "unknown scenario '$1'; try: $0 list"
	;;
esac
