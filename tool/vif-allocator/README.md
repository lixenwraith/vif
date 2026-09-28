# vif-allocator

`vif-allocator` is the narrow boundary between the vif website and the `vif`
namespace, and the proxy on two of a session's three routes. Kubernetes still
schedules, isolates, limits, terminates and garbage-collects every session. The
allocator only performs the fixed transaction that Kubernetes has no anonymous
endpoint for:

1. reserve a free NodePort from 31700–31709;
2. create one non-retryable Job, whose pod answers to the session's identifier;
3. read its UID and create the owner-referenced Service;
4. wait for a ready EndpointSlice and `live=true ready=true` from `/health`;
5. return the session's join targets and health-derived state.

Each Job mounts the node-affine `vif-fleet-logs` PVC only in its session
container. The game writes `/var/log/vif-fleet/<session-id>.jsonl` and tags every
application record with `fields.session_id`; the allocator neither reads nor
rewrites those bytes. One standalone LogWisp service reads those files and binds
its SSE endpoint to loopback. The allocator's `/vif/api/logs` handler is a byte
proxy: it does not parse, retain, or reserialize stream events.

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
| `POST /vif/api/sessions` with an empty body, `{}`, or `{"players":N,"log_level":"info"}` | `201` and one created session. Those two fields are the whole of what a caller may choose; an omitted one takes the deployment default, an unknown one is a `400`. |
| `GET /vif/api/sessions` | `200` and `{ "sessions": [...], "limits": {...} }` for live, non-completed Jobs. |
| `GET /healthz` | Process liveness. |
| `GET /readyz` | Verifies that the current token can reach the Kubernetes API. |
| `GET /vif/api/logs` | Proxies the loopback LogWisp SSE response byte-for-byte. An unavailable upstream returns `503 log_stream_unavailable`. |
| `HEAD /vif/api/logs` | `200` and the stream's headers, answered here: upstream refuses a HEAD on the stream path, and a probe should not open a stream. Only the `GET` reports upstream availability. |
| `GET /vif/ws/<session>` | `101` and one browser participant, proxied to the resolved pod's bridge sidecar. Published only where `-web-origin` and `-ws-bridge-image` are both set. |

One session row has this shape:

```json
{
  "id": "3c3a8eec0cbb6f17",
  "port": 31700,
  "join_target": "vif://play.example.com:7777/3c3a8eec0cbb6f17",
  "direct_target": "vif://play.example.com:31700/3c3a8eec0cbb6f17",
  "ws_url": "wss://play.example.com/vif/ws/3c3a8eec0cbb6f17",
  "created_at": "2026-09-11T12:00:00Z",
  "routable": true,
  "state": {
    "live": true,
    "ready": true,
    "capacity": 4,
    "clock": "stopped",
    "expires_in": "1m27s",
    "guests": 0,
    "phase": "waiting",
    "tick": 0
  }
}
```

`state` is the pod's `/health` body, except that `expires_in` is the sooner of the
game's countdown and the Job's `activeDeadlineSeconds`. An occupied game runs no
countdown, so the Job's deadline is what it reports.

`limits` names what this deployment will accept — `players_max` and the allowed
`log_levels`, most verbose first — so a caller offers only choices that would be
granted rather than discovering them by refusal. `-players-max` defaults to
`-players`, and `-log-level-min` defaults to `debug`: publishing the API must not
hand an anonymous caller a sixteen-player world or the fleet's shared log rate, so
both open only as far as an operator sets them. A value outside them is refused with
`400 invalid_request` naming the bound, never clamped.

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

The browser route validates before it upgrades — method, identifier syntax,
`Origin` against `-web-origin`, the session's liveness and readiness in reconciled
Kubernetes state, and `-route-max` connections for that session, counted with the
front door's — and then reverse-proxies the upgrade to the pod's bridge sidecar with
`net/http/httputil.ReverseProxy`. It never translates a frame and never accepts an
upstream a caller named: the identifier is a routing key looked up in Kubernetes,
not an address supplied. Why the WebSocket lives in a sidecar rather than here or
in the game is in
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

`-route-listen` (`:7777`) is one TCP port for every session. A dialer's first frame
names the session; the allocator resolves it as the browser route does, dials the
pod's game port, replays the name and copies bytes both ways until each side has
closed. It refuses before any dial, in a join reply the dialer prints: a first frame
that is not a route, an identifier that is not 16 hex, a dialling address past the
game's own budget of six joins a minute, a session unknown, unready or full, and one
holding `-route-max` connections over both routes. Logs name the session, never the
dialler.
