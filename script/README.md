# End-to-end setups

Command reference for verifying behaviour on a dev machine. Build first: `make dev`.

```sh
./script/test.sh list        # every scenario
./script/test.sh all         # the automated set but maxmap; prints PASS/FAIL
```

Overrides: `BIN` (default `./bin/vif`), `HOST` (`127.0.0.1`), `PORT` (`7777`),
`PROBE_PORT` (`7778`), `PLAYERS`, `SESSION`, `AUTHORITY`, `IMAGE` (`vif:dev`) and
`CONTAINER_ENGINE` for `image`, and for `load` `SCENARIO`, `GUEST_BOTS`,
`LIMIT_MB` and `GOMEMLIMIT`.

## Interactive

Each runs in the foreground until you stop it.

| Scenario | Command it runs |
|---|---|
| `solo` | `vif -d` |
| `host [players]` | `vif -d -host $HOST:$PORT`, with `-players` only when the argument is given |
| `join [addr]` | `vif -join addr` |
| `serve [players]` | dedicated host, **no lifetime bounds — runs until Ctrl-C** |
| `serve-fleet` | dedicated host with the deployed flag set: `-name $SESSION -authority host -players $PLAYERS -first-join 90s -empty 90s -drain 20s` (`SESSION` defaults to `0123456789abcdef`, `PLAYERS` to 4); join it at `vif://$HOST:$PORT/$SESSION` |
| `pair` | scripted host + scripted guest, both headless |
| `watch` | scripted host presented on this terminal; join it by hand |

Two terminals for a real session:

```sh
./script/test.sh host        # terminal 1
./script/test.sh join        # terminal 2
```

`host` plays at once and admits guests mid-run, up to the whole roster; `host 3`
caps the roster at three, the host included. Any terminal or scripted guest can
join whenever it likes. Only a scripted or bot-driven host that names
`-players`, as `pair` and `host-loss` run one, holds tick zero until that whole
party has arrived.

## Observed

These run a scenario and print what happened, and are not part of `all`. Most
assert nothing, because what they show is a design choice rather than a promise;
`botfitness` and `load` fail on what their rows name.

| Scenario | What it shows |
|---|---|
| `probe` | `GET /health` and `/metrics` of a running `-serve` |
| `guests <n> [addr]` | n scripted guests against one address, for a roster wider than the terminals to hand |
| `host-loss [n]` | host + n guests, then the host goes; prints what each guest concluded. `AUTHORITY=migrate` (default) or `AUTHORITY=host` |
| `vacant` | a dedicated host parking its clock when its last guest leaves |
| `botfitness [graph] [seeds] [secs]` | default's fitness summed per scenario (embedded, td, kraken) over seeds 1..N, flat out; given a graph file, it fails unless that graph beats default on every scenario |
| `load [sessions] [bots] [secs]` | Linux only, since it reads `/proc`: per session a `-serve` host seating its own bots at 120x40 under the pod's `GOMEMLIMIT`, and a headless bot joining it (`GUEST_BOTS` of its own; `SCENARIO` serves a wad scenario); prints each host's memory, CPU, tick, guests, peers, evictions and digest mismatches, and fails on a slipping clock, an eviction, a short roster or a peak past `LIMIT_MB` (default the pod's limit for its bots) |

`host-loss` is the one to read carefully. Under `migrate` every guest binds a port
and keeps a link to the successor, so the survivors elect the chain's first
survivor and continue in one session, which the guests report as `authority handed
off`. A survivor that cannot reach the successor forks after the succession window
(`continuing locally`). Under `host` each survivor continues alone at once. See
[Multiplayer](../doc/multi-player.md#53-reachability-under--authority-migrate).

## Automated

`./script/test.sh all` runs every one of these except `maxmap`, whose guest
holds a ceiling-sized td session for over 40 s, and prints PASS/FAIL.

| Scenario | Asserts |
|---|---|
| `check` | embedded/main fallback and named `wad/scenario/td` all resolve, with every bot graph they reach |
| `bundle` | the release wad archive extracts to a config root that resolves and carries a corpus |
| `scenario` | `:n td` then `:n main` each rebuild the run: three scenario records, the last matching the first. Needs a `script(1)` that can give it a pty, and skips rather than fails without one |
| `transfer` | a guest joins a host playing a nonce-stamped copy of the embedded scenario, which no root on the machine can hold: the host serves it, the guest receives it and installs the session world on it |
| `maxmap` | a guest joins a host serving `td` (500x250, the largest map the grid holds) and installs a second whole world from a correction, so the session carries it and not just the handshake |
| `follow` | a host with a guest types `:n blank`: the guest is told, rebuilds, redials, receives the new scenario off the wire and installs the session world again. Two terminals, so it needs a pty like `scenario` |
| `corpus` | a guest whose `content/` holds one file the host does not have joins a host reading the wad's corpus: glyphs are player domain, so nothing is reconciled and no `content_id` appears in either log |
| `fleet` | the hand-rendered session template and the allocator's own render carry the same arguments, mounts and claims |
| `deploy` | what a node needs before a pod can start, none of which a normal build reaches: the image's `vif_headless` tag compiles, no manifest leaves an image placeholder unquoted, the wad installer refuses a scenario that does not load, naming it, before staging anything, and a scenario resolves through the pod's `scenario/`+`image/` mount, reads no corpus off it, and names itself in the log the commissioning check reads |
| `lifetime` | an unclaimed session exits 0 on its first-guest window; an emptied one ends on its vacancy grace, each naming why |
| `drain` | `SIGTERM` keeps the match running, reports `live=true ready=false phase=draining`, keeps the clock moving, then ends itself |
| `bot` | every shipped graph plays solo without overflowing its queue, and `default` types; then it joins a dedicated host with two bots, types and leaves with them, and the emptied host ends; a `-serve -bots 2` host outlives its `-empty` grace and its bots leave on drain |
| `identity` | a peer running a different build or session is refused (runs the Go tests that can construct one) |

`drain` asserts that the clock still advances after the signal, not which reason
ended the session: whether its scripted guest outlives the drain window is a
wall-clock race that says nothing about the host.

## Container

Not part of `all`: it needs a container engine rather than a built binary, and what
it exercises is the image rather than the game.

| Scenario | Asserts |
|---|---|
| `image` | the container image builds and validates its own config as a non-root read-only user. It runs `make image` with `IMAGE` split into name and tag at its last colon and `CONTAINER_ENGINE` set to the engine found, `docker` first, unless the caller set it |

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
