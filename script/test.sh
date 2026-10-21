#!/bin/sh
# End-to-end checks no Go test makes, run by hand: ./script/test.sh <name> [args],
# or list. They need a built binary on a real terminal, make or the deploy scripts.
# The asserted ones print PASS/FAIL and `all` runs them; the measured ones print
# what they measured and fail on what their rows name. See script/README.md.
set -eu

BIN=${BIN:-./bin/vif}
HOST=${HOST:-127.0.0.1}
PORT=${PORT:-7777}
PROBE_PORT=${PROBE_PORT:-7778}

fail() { printf 'FAIL %s\n' "$*" >&2; exit 1; }
pass() { printf 'PASS %s\n' "$*"; }
note() { printf '\n== %s\n' "$*"; }

need_bin() {
	[ -x "$BIN" ] || fail "$BIN not built; run: make dev"
}

# probe_get fetches one probe path, from PROBE_PORT unless a port is given.
probe_get() {
	if command -v curl >/dev/null 2>&1; then
		curl -sS --max-time 5 "http://$HOST:${2:-$PROBE_PORT}$1"
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

# own_ports refuses to start a host while something already answers on the probe
# port. A leftover host passes the probe check, takes the guest and logs to its own
# directory, which reads as this host never serving.
own_ports() {
	if probe_get /health >/dev/null 2>&1; then
		fail "something already answers on $HOST:$PROBE_PORT; stop it or set PORT and PROBE_PORT"
	fi
}

# pty_ok reports whether this machine has a script(1) that can give vif a terminal.
# The two flavours take their arguments in opposite orders; a machine with neither
# is missing the harness rather than the game, so its checks skip.
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

# stop prints a region that quits a bot graph after $1 ms of game time, appended to
# a graph so a run ends on its own. botstat reads one field of the "bot stopped"
# record a graph that quit left in log $1.
stop() {
	printf '\n[regions.stop]\ninitial = "StopWait"\n\n[states.StopWait]\n'
	printf 'transitions = [{ trigger = "Tick", target = "StopQuit", guard = "StateTimeExceeds", guard_args = { ms = %s } }]\n' "$1"
	printf '\n[states.StopQuit]\non_enter = [{ action = "Intent", payload = { name = "quit" } }]\n'
}
botstat() { sed -n "s/.*\"msg\":\"bot stopped\",.*\"quit\":true.*\"$2\":\(-*[0-9]*\).*/\1/p" "$1"; }

# Every line here has to succeed: under `set -e` a failing command in an EXIT trap
# becomes the script's exit status, and a process that already ended is the normal
# case rather than an error.
cleanup() {
	for pid in ${LOAD_PIDS:-}; do kill -9 "$pid" 2>/dev/null || true; done
	return 0
}
trap cleanup EXIT INT TERM

case "${1:-}" in

list|'')
	cat <<'EOF'
Asserted (print PASS/FAIL; `all` runs them)
  bundle            the release wad archive extracts to a working config root
  scenario          :n <name> rebuilds the run on another scenario and back
  follow            a host changes scenario and its guest rebuilds with it
  fleet             the session template and the allocator render one workload
  deploy            the wad installer's refusal, the manifests and a mounted scenario
  all               every asserted check above

Measured (print what they measured; not part of `all`)
  botfitness [graph] [seeds] [secs]
                    sum default's fitness per scenario over seeds 1..N (4) of secs
                    (60) each; given a graph file, fail unless it beats default
  load [sessions] [bots] [secs]
                    per session a host seating its own bots and a networked bot
                    joiner; prints each host's cost and fails on a slipping clock,
                    an eviction, a short roster or a peak past LIMIT_MB
                    (SCENARIO=name, GUEST_BOTS=n for the joiner's own)
EOF
	;;

botfitness)
	# A graph's fitness is summed per scenario over a fixed seed set, flat out at one
	# size. blank is left out: it disables glyph, nugget and gold, so nothing there
	# separates two graphs. A graph given is also measured against default.
	need_bin
	graph=${2:-}
	seeds=${3:-4}
	secs=${4:-60}
	dir=$(mktemp -d)
	{ cat internal/asset/bot/default.toml; stop $((secs * 1000)); } >"$dir/default.toml"
	if [ -n "$graph" ]; then
		[ -f "$graph" ] || fail "no graph file $graph"
		{ cat "$graph"; stop $((secs * 1000)); } >"$dir/graph.toml"
	fi
	# fitness plays one graph on one scenario and seed and prints its fitness
	fitness() {
		# shellcheck disable=SC2086 # $2 is a flag list
		"$BIN" -headless -bot "$1" $2 -seed "$3" -speed max -size 120x40 -log-stdout -lv info \
			>"$dir/run.log" 2>&1 || fail "$1 did not end cleanly on $2 seed $3: $dir/run.log"
		v=$(botstat "$dir/run.log" fitness)
		[ -n "$v" ] || fail "$1 left no fitness on $2 seed $3: $dir/run.log"
		echo "$v"
	}
	lost=
	for scenario in embedded td; do
		case $scenario in
		embedded) flags=-d ;;
		*) flags="-config-dir wad -s $scenario" ;;
		esac
		note "$scenario, $seeds seeds of ${secs}s"
		printf '%-6s %10s %10s\n' seed default "${graph:+$(basename "$graph" .toml)}"
		base=0 mine=0 s=1
		while [ "$s" -le "$seeds" ]; do
			b=$(fitness "$dir/default.toml" "$flags" "$s") || exit 1
			m=
			if [ -n "$graph" ]; then
				m=$(fitness "$dir/graph.toml" "$flags" "$s") || exit 1
				mine=$((mine + m))
			fi
			base=$((base + b))
			printf '%-6s %10s %10s\n' "$s" "$b" "$m"
			s=$((s + 1))
		done
		printf '%-6s %10s %10s\n' sum "$base" "${graph:+$mine}"
		if [ -n "$graph" ] && [ "$mine" -le "$base" ]; then lost="$lost $scenario"; fi
	done
	rm -rf "$dir"
	if [ -n "$graph" ]; then
		[ -z "$lost" ] || fail "$graph did not beat default on:$lost"
		pass "$graph beat default on every scenario"
	fi
	;;

load)
	# Networked bots at the fleet's shape: per session a dedicated host seating its
	# own bots on PORT and PROBE_PORT stepped by two, and a headless bot joining it as
	# a person would, relaying GUEST_BOTS of its own. Each host runs under the pod's
	# GOMEMLIMIT and is held to its memory limit (LIMIT_MB), both as the session
	# template renders them. Digest mismatches are reported, not failed.
	need_bin
	[ -r /proc/self/stat ] || fail "load reads /proc; run it on Linux"
	sessions=${2:-2} bots=${3:-2} secs=${4:-60}
	guest_bots=${GUEST_BOTS:-0}
	want=$((bots + 1 + guest_bots))
	pod=$(BOTS=$bots deploy/k3s/render-session.sh load 31700 vif:dev "$want") || fail "no pod renders for $bots bot(s)"
	limit=${LIMIT_MB:-$(printf '%s\n' "$pod" | sed -n 's/^ *memory: \([0-9]*\)Mi$/\1/p' | tail -1)}
	gomem=${GOMEMLIMIT:-$(printf '%s\n' "$pod" | sed -n '/name: GOMEMLIMIT/{n;s/^ *value: "\(.*\)"$/\1/p;}')}
	rate=20 # ticks a second: parameter.GameUpdateInterval
	every=5
	hz=$(getconf CLK_TCK)
	config=-d
	[ -z "${SCENARIO:-}" ] || config="-config-dir wad -s $SCENARIO"
	relayed=
	[ "$guest_bots" = 0 ] || relayed="-bots $guest_bots"
	D=$(mktemp -d)
	note "$sessions session(s) of $bots host bot(s) and a joiner with $guest_bots for ${secs}s; logs in $D"
	n=0
	while [ "$n" -lt "$sessions" ]; do
		probe=$((PROBE_PORT + 2 * n))
		if probe_get /health "$probe" >/dev/null 2>&1; then
			fail "something already answers on $HOST:$probe; set PORT and PROBE_PORT"
		fi
		# shellcheck disable=SC2086
		GOMEMLIMIT=$gomem "$BIN" -serve "$HOST:$((PORT + 2 * n))" -probe "$HOST:$probe" $config -size 120x40 \
			-players "$want" -bots "$bots" -log-stdout -lv info >"$D/host$n.log" 2>&1 &
		eval "host$n=\$!"
		LOAD_PIDS="${LOAD_PIDS:-} $!"
		n=$((n + 1))
	done
	# admits is whether the host probed on port $1 takes a dial now: ready, with its
	# clock running. Its own bots start that clock at once, and a dial landing while
	# they do is refused with "session is starting", which a -join does not retry.
	admits() {
		body=$(probe_get /health "$1") || return 1
		case $body in *ready=true*) ;; *) return 1 ;; esac
		printf '%s\n' "$body" | grep -qx clock=running
	}
	n=0
	while [ "$n" -lt "$sessions" ]; do
		probe=$((PROBE_PORT + 2 * n))
		wait_for 20 "admits $probe" || fail "host $n never admitted a dial: $D/host$n.log"
		# shellcheck disable=SC2086
		"$BIN" -headless -bot default -join "$HOST:$((PORT + 2 * n))" $relayed \
			-log-stdout -lv info >"$D/join$n.log" 2>&1 &
		LOAD_PIDS="$LOAD_PIDS $!"
		wait_for 60 "probe_get /health $probe | grep -qx guests=$want" \
			|| fail "host $n never held its $want guests: $D/host$n.log $D/join$n.log"
		n=$((n + 1))
	done

	# since is how far counter $1 moved from $2, counting from zero across a reset
	since() { if [ "$1" -ge "$2" ]; then echo $(($1 - $2)); else echo "$1"; fi; }
	# loadsample reads host $1's process and probe into the r_ variables.
	loadsample() {
		eval "pid=\$host$1"
		alive "$pid" || fail "host $1 exited: $D/host$1.log"
		r_rss=$(sed -n 's/^VmRSS:[^0-9]*\([0-9]*\).*/\1/p' "/proc/$pid/status")
		r_hwm=$(sed -n 's/^VmHWM:[^0-9]*\([0-9]*\).*/\1/p' "/proc/$pid/status")
		r_cpu=$(awk '{ print $14 + $15 }' "/proc/$pid/stat")
		body=$(probe_get /health "$((PROBE_PORT + 2 * $1))") || fail "host $1 stopped answering its probe"
		r_tick=$(printf '%s\n' "$body" | sed -n 's/^tick=//p')
		r_guests=$(printf '%s\n' "$body" | sed -n 's/^guests=//p')
		body=$(probe_get /metrics "$((PROBE_PORT + 2 * $1))") || fail "host $1 stopped answering its probe"
		r_peers=$(printf '%s\n' "$body" | sed -n 's/^vif_network_peers //p')
		r_evicted=$(printf '%s\n' "$body" | sed -n 's/^vif_network_evicted //p')
		r_mismatch=$(printf '%s\n' "$body" | sed -n 's/^vif_network_digest_mismatches //p')
	}
	start=$(date +%s)
	last=$start
	n=0
	while [ "$n" -lt "$sessions" ]; do
		loadsample "$n"
		eval "tick_$n=$r_tick ticks_$n=0 cpu0_$n=$r_cpu cpu_$n=$r_cpu mismatch_$n=${r_mismatch:-0} grew_$n=0"
		n=$((n + 1))
	done
	printf '\n%5s %4s %8s %8s %9s %7s %6s %5s %7s %9s\n' \
		secs host rss_mib hwm_mib cpu_ms/s tick guests peers evicted mismatch
	problems=
	elapsed=0
	while [ "$elapsed" -lt "$secs" ]; do
		step=$((secs - elapsed))
		[ "$step" -le "$every" ] || step=$every
		sleep "$step"
		now=$(date +%s)
		elapsed=$((now - start))
		n=0
		while [ "$n" -lt "$sessions" ]; do
			loadsample "$n"
			eval "cpu=\$cpu_$n"
			printf '%5s %4s %8s %8s %9s %7s %6s %5s %7s %9s\n' "$elapsed" "$n" \
				"$((r_rss / 1024))" "$((r_hwm / 1024))" \
				"$(( (r_cpu - cpu) * 1000 / hz / (now - last) ))" "$r_tick" "$r_guests" \
				"${r_peers:-?}" "${r_evicted:-0}" "${r_mismatch:-0}"
			[ "$r_guests" = "$want" ] || problems="$problems
host $n held $r_guests guests at ${elapsed}s, want $want"
			# A reset restarts the tick and the mismatch count, so both are summed per sample
			eval "ticks_$n=\$((ticks_$n + \$(since $r_tick \$tick_$n))) grew_$n=\$((grew_$n + \$(since ${r_mismatch:-0} \$mismatch_$n)))"
			eval "cpu_$n=$r_cpu tick_$n=$r_tick hwm_$n=$r_hwm evicted_$n=${r_evicted:-0} mismatch_$n=${r_mismatch:-0}"
			n=$((n + 1))
		done
		last=$now
	done

	note "each host over ${elapsed}s"
	n=0
	while [ "$n" -lt "$sessions" ]; do
		eval "ticks=\$ticks_$n cpu=\$(( (cpu_$n - cpu0_$n) * 1000 / hz / elapsed ))"
		eval "hwm=\$((hwm_$n / 1024)) evicted=\$evicted_$n grew=\$grew_$n"
		printf 'host %s: %s ticks, %s ms of CPU a second, peak %s MiB, %s new digest mismatch(es)\n' \
			"$n" "$ticks" "$cpu" "$hwm" "$grew"
		[ "$ticks" -ge $((secs * rate * 9 / 10)) ] || problems="$problems
host $n advanced $ticks ticks in ${secs}s, under 90% of $rate a second"
		[ "$evicted" = 0 ] || problems="$problems
host $n evicted $evicted participant(s)"
		[ "$hwm" -le "$limit" ] || problems="$problems
host $n peaked at $hwm MiB, past $limit"
		n=$((n + 1))
	done
	[ -z "$problems" ] || fail "networked bots under load:$problems
logs in $D"
	rm -rf "$D"
	pass "$sessions session(s) held $bots host bot(s) and a joiner with $guest_bots for ${secs}s"
	;;

bundle)
	# What the release publishes is the wad as the vif/ config root it extracts as,
	# so the archive is only right if that root resolves and nothing lands beside
	# it. A binary on its own reaches the embedded scenario and nothing else.
	need_bin
	ARCHIVE=$(mktemp -d)/wad.tar.gz; DEST=$(mktemp -d); ROOT=$DEST/vif
	make wad-archive WAD_ARCHIVE="$ARCHIVE" >/dev/null || fail "make wad-archive"
	tar -C "$DEST" -xzf "$ARCHIVE" || fail "the archive did not extract"
	[ "$(ls -A "$DEST")" = vif ] || fail "the archive unpacked beside vif/: $(ls -A "$DEST")"
	for tree in "-s main" "-s td" ""; do
		# shellcheck disable=SC2086
		"$BIN" -check -config-dir "$ROOT" $tree >/dev/null \
			|| fail "the extracted root did not resolve: $tree"
	done
	"$BIN" -check -config-dir "$ROOT" | grep -q "^content ok: $ROOT/content" \
		|| fail "the archive carries no corpus for a player who has none"
	rm -rf "$DEST" "$(dirname "$ARCHIVE")"
	pass "the release wad archive extracts to a config root that resolves"
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
	pty_bg "$HK" "$BIN -host $HOST:$PORT -config-dir wad -s main -l=$HL -lv info -ls app+session"
	HOST_PTY=$PTY_PID
	sleep 2
	pty_bg "$GK" "$BIN -join $HOST:$PORT -config-dir $ROOT -l=$GL -lv info -ls app+session"
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
	# The host's own cursor (slot 0) after the change: one process logs both runs and
	# the guest's arrival spawns a cursor too, so only slot 0 from the restart counts.
	own=$(cat "$HL"/*.jsonl 2>/dev/null | awk '
		/"msg":"run restarting"/ { seen = 1; n = 0; next }
		seen && /"msg":"cursor spawned"/ && /"slot":0/ { n++ }
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
	# Three bots, the envelope TestBuildJobUsesFixedSessionShape pins for the allocator.
	rendered=$(SCENARIO=td LOG_LEVEL=debug PLAYERS=4 BOTS=3 \
		./deploy/k3s/render-session.sh fleetcheck 31700 vif:dev 4 120x40)
	for want in \
		'"-name"' '"-config-dir"' '"-s"' '"td"' '"debug"' \
		'claimName: vif-fleet-wad' 'subPath: scenario' 'subPath: image' \
		'name: vif-session-env' \
		'cpu: 292m$' 'memory: 192Mi$' 'memory: 576Mi$' 'value: "544MiB"'
	do
		printf '%s' "$rendered" | grep -q -- "$want" \
			|| fail "the rendered template is missing $want"
	done
	printf '%s\n' "$rendered" | grep -A1 -- '"-bots"' | grep -q '"3"' \
		|| fail "the rendered template does not seat the bots it was given"
	go test ./tool/vif-allocator/ >/dev/null || fail "allocator tests"
	pass "the session template and the allocator agree on the fleet's workload"
	;;

deploy)
	# What the node needs to be true before a pod can start, none of which the
	# normal build exercises: the image's own build tag, a manifest that survives an
	# unset variable, the installer's refusal, and a scenario resolving through the
	# mount layout rather than out of the binary.
	command -v go >/dev/null 2>&1 || fail "go not found"
	headless=$(mktemp -d)
	CGO_ENABLED=0 go build -tags=vif_headless -o "$headless/vif" ./cmd/vif \
		|| fail "the image's build tag does not compile"

	# An unquoted placeholder that renders empty ends the line on a colon, which is
	# a YAML scanner error forty lines from the variable that caused it.
	grep -Rn 'image: \${' deploy/k3s/ \
		&& fail "an image placeholder is unquoted; an unset tag becomes a parse error"

	# The installer refuses a scenario that does not load, by name, before it stages
	# anything. Installing needs root; sudo here is a stub that fails, so the check
	# never prompts and nothing privileged can run whatever the installer does.
	stub=$(mktemp -d)
	printf '#!/bin/sh\nexit 1\n' >"$stub/sudo"
	chmod +x "$stub/sudo"
	bad=$(mktemp -d)/wad
	cp -a wad "$bad"
	printf '\n[regions.broken]\nfile = "nope.toml"\n' >>"$bad/scenario/blank/scenario.toml"
	node=$(mktemp -d)/wad
	if PATH="$stub:$PATH" VIF_WAD_ROOT=$node ./deploy/guest/update-vif-wad.sh "$bad" \
		>"$headless/bad" 2>&1
	then
		fail "the installer accepted a scenario that does not load"
	fi
	grep -q 'scenario blank does not load' "$headless/bad" \
		|| fail "the refusal did not name the scenario: $headless/bad"
	[ ! -e "$node" ] && [ ! -e "$node.new" ] || fail "a refused tree reached the node: $node"
	rm -rf "$stub" "$(dirname "$bad")" "$(dirname "$node")"

	# A pod has no user or system configuration root, so neither may these runs: a
	# corpus installed on this machine would otherwise stand in for the embedded one.
	bare=$(mktemp -d)
	pod() { env HOME="$bare" XDG_CONFIG_HOME="$bare" VIF_SYSTEM_ROOTS="$bare" "$@"; }

	# The pod mounts scenario/ and image/ and nothing else, so prove a scenario
	# resolves from exactly that and names itself in the log the commissioning
	# check reads. main is the embedded campaign, on no volume, so td stands in.
	mount=$(mktemp -d)/wad
	mkdir -p "$mount"
	cp -a wad/scenario wad/image "$mount/"
	pod "$headless/vif" -check -config-dir "$mount" -s td >"$headless/check" \
		|| fail "the init container's check does not resolve through the mount"
	grep -q "^scenario ok: $mount/scenario/td/" "$headless/check" \
		|| fail "no scenario off the mount: $headless/check"
	# A pod reads no corpus: glyphs are player domain and a dedicated host spawns
	# none, so an installed content/ would be a category nothing asked for.
	grep -q '^content ok: embedded' "$headless/check" \
		|| fail "a session resolved a corpus off the volume: $headless/check"
	own_ports
	pod "$headless/vif" -serve "$HOST:$PORT" -probe "$HOST:$PROBE_PORT" -authority host \
		-l="$mount/log" -log-session-id=volume-check -lv=info \
		-config-dir="$mount" -s=td -first-join=3s -empty=3s -drain=3s >/dev/null 2>&1 \
		|| fail "the probe pod's arguments do not run"
	grep -q '"msg":"scenario loaded"' "$mount/log/volume-check.jsonl" \
		|| fail "no scenario record; the commissioning check has nothing to read"
	grep -q '"name":"td"' "$mount/log/volume-check.jsonl" \
		|| fail "the session did not load td off the mount"
	rm -rf "$mount" "$headless" "$bare"
	pass "the image builds, the installer refuses a broken tree, and a mounted scenario loads"
	;;

all)
	for s in bundle scenario follow fleet deploy; do
		note "$s"
		"$0" "$s"
	done
	;;

*)
	fail "unknown check '$1'; try: $0 list"
	;;
esac
