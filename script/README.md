# End-to-end checks

`test.sh` holds the checks no Go test makes, because they need a built binary on a
real terminal, `make` or the deploy scripts. Build first: `make dev`.

```sh
./script/test.sh list        # every check
./script/test.sh all         # the asserted set; prints PASS/FAIL
```

Overrides: `BIN` (default `./bin/vif`), `HOST` (`127.0.0.1`), `PORT` (`7777`),
`PROBE_PORT` (`7778`), and for `load` `SCENARIO`, `GUEST_BOTS`, `LIMIT_MB` and
`GOMEMLIMIT`.

## Asserted

`all` runs these in order and stops at the first FAIL.

| Check | Asserts |
|---|---|
| `bundle` | the release wad archive extracts to a config root that resolves and carries a corpus |
| `scenario` | `:n td` then `:n main` each rebuild the run: three scenario records, the last matching the first. Needs a `script(1)` that can give it a pty, and skips rather than fails without one |
| `follow` | a host with a guest types `:n blank`: the guest is told, rebuilds, redials, receives the new scenario off the wire and installs the session world again. Two terminals, so it needs a pty like `scenario` |
| `fleet` | the hand-rendered session template and the router's own render carry the same arguments, mounts and claims |
| `deploy` | what a node needs before a pod can start, none of which a normal build reaches: the image's `vif_headless` tag compiles, no manifest leaves an image placeholder unquoted, the wad installer refuses a scenario that does not load, naming it, before staging anything, and `td` resolves through the pod's `scenario/`+`image/` mount, reads no corpus off it, and names itself in the log the commissioning check reads |

`scenario` and `follow` are the only runs of the restart loop `app.Run` owns: a Go
test drives an `App` directly, which has none.

## Measured

These print what they measured and are not part of `all`.

| Check | What it measures |
|---|---|
| `botfitness [graph] [seeds] [secs]` | default's fitness summed per scenario (embedded, td) over seeds 1..N, flat out; given a graph file, it fails unless that graph beats default on every scenario |
| `load [sessions] [bots] [secs]` | Linux only, since it reads `/proc`: per session a `-serve` host seating its own bots at 120x40 under the pod's `GOMEMLIMIT`, and a headless bot joining it (`GUEST_BOTS` of its own; `SCENARIO` serves a wad scenario); prints each host's memory, CPU, tick, guests, peers, evictions and digest mismatches, and fails on a slipping clock, an eviction, a short roster or a peak past `LIMIT_MB` (default the pod's limit for its bots) |

## The sparring pair

`sparring-host.toml` and `sparring-guest.toml` are authored `-script` schedules
that play a fixed host or guest against a person, or against each other headless;
[Development](../doc/development.md#a-scripted-participant) has the commands.
`go test ./internal/journal` compiles every `script/*.toml`.
