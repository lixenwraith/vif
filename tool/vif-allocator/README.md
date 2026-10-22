# vif-allocator

`vif-allocator` is the narrow boundary between the vif website and the `vif`
namespace, and the router on two of a session's three routes. Kubernetes still
schedules, isolates, limits, terminates and garbage-collects every session. The
allocator only performs the fixed transaction that Kubernetes has no anonymous
endpoint for:

1. reserve a free NodePort from 31700–31709;
2. create one non-retryable Job, whose pod answers to the session's identifier;
3. read its UID and create the owner-referenced Service;
4. wait for a ready EndpointSlice and `live=true ready=true` from `/health`;
5. return the session's join targets and health-derived state.

Each Job mounts the read-only `vif-fleet-wad` claim at `/wad/scenario` and
`/wad/image` in its config-check init container and its session container, and the
node-affine `vif-fleet-logs` PVC only in the session container. The game writes
`/var/log/vif-fleet/<session-id>.jsonl` and tags every application record with
`fields.session_id`; the allocator neither reads nor rewrites those bytes. One
standalone LogWisp service reads those files and binds its SSE endpoint to
loopback. The allocator's `/vif/api/logs` handler is a byte proxy: it does not
parse, retain, or reserialize stream events.

It has no database. Jobs and Services are the durable state, and startup
reconciliation deletes a Job left without its Service or a Service left without a
valid Job owner. Completed Jobs are never resurrected.

Build it from the repository root:

```sh
make allocator
```

The production service reads a short-lived ServiceAccount token from a file on
every Kubernetes request, so the root-owned token timer can replace that file
atomically without restarting the allocator. See
[`doc/kube-docker-deploy.md`](../../doc/kube-docker-deploy.md#11-the-allocator).

## HTTP API

| Request | Result |
|---|---|
| `POST /vif/api/sessions` with an empty body, `{}`, or `{"players":N,"bots":K,"log_level":"info","scenario":"td","size":"160x48"}` | `201` and one created session. Those five fields are the whole of what a caller may choose; an omitted one takes the deployment default (no bots), an unknown one is a `400`. `429 create_budget` once the caller's address has asked for `-client-creates` sessions in one `-first-join` window. |
| `GET /vif/api/sessions` | `200` and `{ "sessions": [...], "limits": {...} }` for live, non-completed Jobs. |
| `GET /healthz` | Process liveness. |
| `GET /readyz` | Verifies that the current token can reach the Kubernetes API. |
| `GET /vif/api/logs` | Proxies the loopback LogWisp SSE response byte-for-byte. An unavailable upstream returns `503 log_stream_unavailable`. |
| `HEAD /vif/api/logs` | `200` and the stream's headers, answered here: upstream refuses a HEAD on the stream path, and a probe should not open a stream. Only the `GET` reports upstream availability. |
| `GET /vif/ws/<session>` | `101` and one browser participant, whose socket this allocator terminates and splices to the resolved pod's game port. Published only where `-web-origin` is set; elsewhere `501 web_route_not_configured`. |

An error is `{"error":{"code":"…","message":"…"}}`. A create can also answer
`413 request_too_large` for a body over 1 KiB, `415 content_type` for one not sent
as `application/json`, `503 fleet_full` with `Retry-After: 10` when no port is free,
`504 session_not_ready` or `408 request_canceled` (the session was removed either
way), and `502 kubernetes_error`.

One session row has this shape:

```json
{
  "id": "3c3a8eec0cbb6f17",
  "port": 31700,
  "join_target": "vif://play.example.com:7777/3c3a8eec0cbb6f17",
  "direct_target": "vif://play.example.com:31700/3c3a8eec0cbb6f17",
  "ws_url": "wss://play.example.com/vif/ws/3c3a8eec0cbb6f17",
  "scenario": "td",
  "size": "first",
  "created_at": "2026-09-11T12:00:00Z",
  "routable": true,
  "state": {
    "live": true,
    "ready": true,
    "capacity": 4,
    "clock": "stopped",
    "expires_in": "1m27s",
    "guests": 0,
    "bots": 0,
    "map": "500x250",
    "build": "v0.1.0 (1154b8a6)",
    "phase": "waiting",
    "tick": 0
  }
}
```

`state` is the pod's `/health` body without its `address`, `reason` included when
there is one, except that `expires_in` is the sooner of the game's countdown and the
Job's `activeDeadlineSeconds`. An occupied game runs no countdown, so the Job's
deadline is what it reports. `guests` counts the session's own bots too, since each
holds a seat; `bots` is how many of them are bots. A pod that is not ready, or
whose probe fails, reports `live=false` with its pod phase or `starting`, and
`routable` is false until its address is a ready endpoint. `scenario` and `size` are
what the session was created with, kept as annotations on its Job; `map` is the map
it has, from the first tick on. `build` is the binary the session runs, named as a
refusal of another build names it, so a page can be told before it dials one.

`limits` names what this deployment will accept — `players_max`, `bots_max`, the
allowed `log_levels`, most verbose first, and the installed `scenarios`, default
first — and what an omitted choice gets, so a caller offers only choices that would
be granted, and can name the defaults, rather than discovering either by refusal:

```json
{
  "players_max": 4,
  "players_default": 4,
  "bots_max": 0,
  "log_levels": ["debug", "info", "warn", "error"],
  "log_level_default": "info",
  "size_default": "first",
  "scenarios": ["main", "blank", "simon", "td"],
  "scenario_descriptions": {
    "main": "Survive escalating storms, lure Krakens through mazes, and defend the tower.",
    "blank": "An empty arena for experimenting with commands and building scenarios.",
    "simon": "Simon: listen to the notes, then repeat them with keys 1-4 or by clicking the buttons.",
    "td": "Defend two central towers in a sprawling maze against escalating enemy waves."
  },
  "scenario_fixed_maps": {"td": "500x250"},
  "scenario_min_players": {"main": 1, "blank": 1, "simon": 1, "td": 1},
  "scenario_max_players": {"main": 16, "blank": 16, "simon": 16, "td": 16}
}
```

`scenario_descriptions` is each scenario's top-level `description`,
`scenario_min_players` and `scenario_max_players` its `min_players` and
`max_players`, and `scenario_fixed_maps` its `[map] fixed`, the size it says its map
holds throughout, so the screen a request names does not size it. All are read from
its `scenario.toml` on every request as the names are; `main` off the volume is the
game's embedded fallback, described by that. A scenario that states one past the
file's first 4 KiB, or a top-level one after any table, is absent from that object
and still offered; with none at all the key is omitted. `players` must also fall in
the scenario's range, which an omitted one is moved into from the default, and a
scenario needing more than `-players-max` is refused. `-players-max`
defaults to `-players`, `-bots-max` to `0` and `-log-level-min` to `debug`:
publishing the API must not hand an anonymous caller a sixteen-player world, the
fleet's CPU or its shared log rate, so each opens only as far as an operator sets
it. A value outside them is refused with `400 invalid_request` naming the bound,
never clamped. `size` is `first`, or a `WxH` screen whose game area (the screen less
the game's gutters and status rows) is a map the grid can hold.

`bots` seats that many of the session's own bots, each a roster guest playing the
embedded `default` graph. A session is ready only while a seat is free, so a request
is also refused bots that leave no seat for a person: at most `players - 1`. The
bots occupy the session as people do, so one nobody else joins plays on until a
drain or the Job's one-hour `activeDeadlineSeconds` ends it.
`-bots-max` is at most 6, the most whose pod envelope fits the namespace's
`LimitRange`.

`id` is the session's stable public identifier and the name its pod answers to.
`join_target`, `direct_target` and `ws_url` are opaque strings this allocator
produces, and no caller may rebuild one from `port`: `join_target` is the front
door below, `direct_target` the session's own NodePort. `ws_url` is absent where the
deployment publishes no browser route, which is what a page keyed on it reads as
"this fleet is for native clients".

`-web-origin` is one origin, not a list, and `ws_url` is built from it. A site
answering several hostnames must therefore canonicalize: a page loaded from another
one of them is handed a `ws_url` whose origin this allocator refuses, and a page
that checks the URL it was given against its own location will hide the link rather
than offer a broken one.

The browser route validates what HTTP must before it upgrades — method, identifier
syntax, that the request is an upgrade, `Origin` against `-web-origin` (`403`) and
the handshake itself (`426` for another version, `400` for a bad key) — then answers
the `101` with `pkg/websocket` and places the stream as the front door places a
dial, 32 at once: the route frame, the player's join budget (the front door's,
below), the session's liveness and readiness in reconciled Kubernetes state,
`-route-max` connections for that session counted with the front door's, and the
pod's game port. Each of those is refused as the join reply a page reads, since a
browser is never shown a status, and the stream is then spliced to the pod. It never
accepts an upstream a caller named: the identifier is a routing key looked up in
Kubernetes, not an address supplied. The decision record is in
[`doc/multi-platform.md`](../../doc/multi-platform.md#5-browser-networking).

The allocator deliberately exposes no public delete endpoint: this API is
anonymous behind the site, and one player must not be able to terminate another
player's match. Sessions expire themselves; operators retain `kubectl` and
`deploy/k3s/session.sh delete` for exceptional cleanup.

`-log-stream-url` is required and accepts only an absolute `http` URL with a
loopback IP, explicit port, exact `/stream` path, and no credentials, query, or
fragment. The production unit orders after and wants LogWisp for normal startup,
but it does not require or execute it. Allocation, state, health, and readiness
therefore remain independent while a missing stream produces only the stable 503.

## Front door

`-route-listen` (`:7777`) is one TCP port for every session, placing at most 32
connections at once. A dialer's first frame names the session; the allocator
resolves it as the browser route does, dials the pod's game port, replays the name
and copies bytes both ways until each side has closed, or until a write has stalled
ten seconds or the other direction ended ten seconds before. A stream that does not
open with a route frame is closed unanswered. Every other refusal precedes the dial,
in a join reply the dialer prints: an identifier that is not 16 hex, a dialling
address past `-client-joins` (the game's own budget of six joins a minute, shared
with the browser route), a session unknown, unready or full, and one holding
`-route-max` connections over both routes. Logs name the session, never the dialler.

## Settings

The production unit sets the flags it names from the installed copy of
`deploy/guest/vif-allocator.env`, which `update-vif-image.sh` gives the
`VIF_ALLOCATOR_IMAGE` line; a variable left empty keeps the flag's default. The
others, the Kubernetes endpoint, namespace and credential files among them, keep
their defaults; `vif-allocator -h` lists every flag.

| Flag | Default | What it sets |
|---|---|---|
| `-listen`, `-route-listen` | `:9080`, `:7777` | the HTTP API and probes; the front door |
| `-join-host` | required | the host every `join_target` and `direct_target` names |
| `-web-origin` | empty | the one origin the browser route answers and `ws_url` is built from; empty publishes no browser route |
| `-image` | required | the session image; `update-vif-image.sh` writes it on the node |
| `-log-stream-url` | required | the loopback LogWisp stream `/vif/api/logs` proxies |
| `-players`, `-players-max` | `4`, `-players` | the default guest ceiling and the highest a request may select (at most 16) |
| `-bots-max` | `0` | the most bots a request may seat (at most 6) |
| `-log-level`, `-log-level-min` | `info`, `debug` | the default session level and the most verbose a request may select |
| `-scenario`, `-wad` | `main`, `/var/db/vif/wad` | the default scenario and the volume scanned for the others |
| `-size` | `first` | the default session screen, the host's `-size`: `WxH`, or `first` for the first person's terminal, which a session seating bots plays at 120x40 instead; a scenario may or may not make it its map |
| `-first-join`, `-empty`, `-drain` | `90s`, `90s`, `20s` | the session's lifetime bounds, passed to its pod |
| `-route-max` | `8` | connections one session may hold over both proxied routes (1–16) |
| `-client-address-header` | empty | the header the site's edge overwrites with the player's address; empty charges no budget on the API or the browser route |
| `-client-joins`, `-client-creates` | `6`, `4` | joins per address per minute over both proxied routes; sessions per address per `-first-join` window |
| `-port-first`, `-port-last` | `31700`, `31709` | the NodePort pool, and so the fleet's size |
| `-ready-timeout`, `-poll-interval` | `75s`, `1s` | how long a create waits for readiness, and how often it looks |
