# Manual test scenarios

Command reference for verifying behaviour on a dev machine. Build first: `make dev`.

```sh
./test/scenario.sh list        # every scenario
./test/scenario.sh all         # every automated one; prints PASS/FAIL
```

Overrides: `BIN` (default `./bin/vif`), `HOST`, `PORT`, `PROBE_PORT`, `PLAYERS`, `IMAGE`.

## Interactive

| Scenario | Command it runs |
|---|---|
| `solo` | `vif -d` |
| `host` | `vif -d -host $HOST:$PORT -players 2` |
| `join [addr]` | `vif -join addr` |
| `serve` | dedicated host, **no lifetime bounds — runs until Ctrl-C** |
| `serve-fleet` | dedicated host with the deployed `-first-join 90s -empty 90s -drain 20s` |
| `probe` | `GET /health` and `/metrics` of a running `-serve` |
| `pair` | scripted host + scripted guest, both headless |

Two terminals for a real session:

```sh
./test/scenario.sh host        # terminal 1
./test/scenario.sh join        # terminal 2
```

## Automated

| Scenario | Asserts |
|---|---|
| `check` | embedded, `config/main` and `config/td` all resolve |
| `lifetime` | an unclaimed session exits 0 on its first-guest window; an emptied one exits 0 on its vacancy grace, each naming why |
| `drain` | `SIGTERM` keeps the match running, reports `live=true ready=false phase=draining`, then exits on the drain deadline |
| `identity` | a peer running a different build or session is refused (runs the Go tests that can construct one) |
| `image` | the container image builds and validates its own config as a non-root read-only user |

## If `-serve` seems to hang

It is not hanging. Without `-first-join` / `-empty` a dedicated host waits forever,
because the operator who started it is its supervisor. `serve-fleet` is the bounded
shape the containers run. See [Runtime](../doc/runtime.md) §1.2.
