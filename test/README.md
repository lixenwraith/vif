# Manual test scenarios

Command reference for verifying behaviour on a dev machine. Build first: `make dev`.

```sh
./test/scenario.sh list        # every scenario
./test/scenario.sh all         # every automated one; prints PASS/FAIL
```

Overrides: `BIN` (default `./bin/vif`), `HOST`, `PORT`, `PROBE_PORT`, `PLAYERS`,
`AUTHORITY`, `IMAGE`.

## Interactive

Each runs in the foreground until you stop it.

| Scenario | Command it runs |
|---|---|
| `solo` | `vif -d` |
| `host [players]` | `vif -d -host $HOST:$PORT`, with `-players` only when the argument is given |
| `join [addr]` | `vif -join addr` |
| `serve [players]` | dedicated host, **no lifetime bounds — runs until Ctrl-C** |
| `serve-fleet` | dedicated host with the deployed flag set: `-authority host -first-join 90s -empty 90s -drain 20s` |
| `pair` | scripted host + scripted guest, both headless |
| `watch` | scripted host presented on this terminal; join it by hand |

Two terminals for a real session:

```sh
./test/scenario.sh host        # terminal 1
./test/scenario.sh join        # terminal 2
```

`host` with no argument admits the whole roster and starts on its first guest, so
more terminals can join whenever they like. `host 3` is the other meaning of
`-players`: a party of exactly three that starts together, which is what a scripted
guest needs, because an authored script can only enter a session at tick zero.

## Observed

These run a scenario and print what happened. They assert nothing, because what
they show is a design choice rather than a promise.

| Scenario | What it shows |
|---|---|
| `probe` | `GET /health` and `/metrics` of a running `-serve` |
| `guests <n> [addr]` | n scripted guests against one address, for a roster wider than the terminals to hand |
| `host-loss [n]` | host + n guests, then the host goes; prints what each guest concluded. `AUTHORITY=migrate` (default) or `AUTHORITY=host` |
| `vacant` | a dedicated host parking its clock when its last guest leaves |

`host-loss` is the one to read carefully. A successor authors but does not listen,
and the handoff never reaches a guest that had no link to it, so in a star each
survivor ends up in a game of its own either way — the policy only decides which of
them believes it is hosting one. See
[Multiplayer](../doc/multi-player-enhancement.md) §5.0.

## Automated

`./test/scenario.sh all` runs every one of these and prints PASS/FAIL.

| Scenario | Asserts |
|---|---|
| `check` | embedded, default `wad/game/main`, and named `wad/game/td` all resolve |
| `lifetime` | an unclaimed session exits 0 on its first-guest window; an emptied one exits 0 on its vacancy grace, each naming why |
| `drain` | `SIGTERM` keeps the match running, reports `live=true ready=false phase=draining`, then exits on the drain deadline |
| `identity` | a peer running a different build or session is refused (runs the Go tests that can construct one) |

`drain` needs its guest to outlive the drain window to prove anything. If the guest
leaves first the run says so and names both logs, because a session that ended on an
empty roster kept its promise — it just did not demonstrate the one being tested.

## Container

Not part of `all`: it needs a container engine rather than a built binary, and what
it exercises is the image rather than the game.

| Scenario | Asserts |
|---|---|
| `image` | the container image builds and validates its own config as a non-root read-only user |

## If `-serve` seems to hang

Two different things look like a hang and neither is one.

Without `-first-join` / `-empty` a dedicated host waits forever for a guest, because
the operator who started it is its supervisor. `serve-fleet` is the bounded shape
the containers run.

With nobody in it, a dedicated host stops its clock: `/health` answers
`live=true ready=true clock=paused phase=vacant` and the tick counter stops moving.
That is the session waiting rather than a stall — a dial releases it — and after a
minute it starts a fresh run rather than handing the next guest an abandoned match.
See [Runtime](../doc/runtime.md) §1.2.
