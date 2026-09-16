# vif-allocator

`vif-allocator` is the narrow HTTP boundary between the vi-fighter website and
the `vif` namespace. Kubernetes still schedules, isolates, limits, terminates and
garbage-collects every session. The allocator only performs the fixed transaction
that Kubernetes has no anonymous endpoint for:

1. reserve a free NodePort from 31700–31709;
2. create one non-retryable Job;
3. read its UID and create the owner-referenced Service;
4. wait for a ready EndpointSlice and `live=true ready=true` from `/health`;
5. return the page URL, raw-TCP join target and health-derived state.

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
[`doc/kube_docker_deploy.md`](../../doc/kube_docker_deploy.md#11-the-allocator).

## HTTP API

| Request | Result |
|---|---|
| `POST /vif/api/sessions` with an empty body, `{}`, or `{"players":N,"log_level":"info"}` | `201` and one created session. Those two fields are the whole of what a caller may choose; an omitted one takes the deployment default, an unknown one is a `400`. |
| `GET /vif/api/sessions` | `200` and `{ "sessions": [...], "limits": {...} }` for live, non-completed Jobs. |
| `GET /healthz` | Process liveness. |
| `GET /readyz` | Verifies that the current token can reach the Kubernetes API. |
| `GET /vif/api/logs` | Proxies the loopback LogWisp SSE response byte-for-byte. An unavailable upstream returns `503 log_stream_unavailable`. |
| `HEAD /vif/api/logs` | `200` and the stream's headers, answered here: upstream refuses a HEAD on the stream path, and a probe should not open a stream. Only the `GET` reports upstream availability. |

One session row has this shape:

```json
{
  "id": "3c3a8eec0cbb6f17",
  "port": 31700,
  "page_url": "https://play.example.com/projects/vi-fighter/session/31700/",
  "join_target": "play.example.com:31700",
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

`limits` names what this deployment will accept — `players_max` and the allowed
`log_levels`, most verbose first — so a caller offers only choices that would be
granted rather than discovering them by refusal. `-players-max` defaults to
`-players`, and `-log-level-min` defaults to `debug`: publishing the API must not
hand an anonymous caller a sixteen-player world or the fleet's shared log rate, so
both open only as far as an operator sets them. A value outside them is refused with
`400 invalid_request` naming the bound, never clamped.

`id` is the session's stable public identifier. `page_url` and `join_target` are
opaque strings this allocator produces: no caller may rebuild either from `port`,
because path-routed sessions will key both on the identifier instead.

The allocator deliberately exposes no public delete endpoint: this API is
anonymous behind the site, and one player must not be able to terminate another
player's match. Sessions expire themselves; operators retain `kubectl` and
`deploy/k3s/session.sh delete` for exceptional cleanup.

`-log-stream-url` is required and accepts only an absolute `http` URL with a
loopback IP, explicit port, exact `/stream` path, and no credentials, query, or
fragment. The production unit orders after and wants LogWisp for normal startup,
but it does not require or execute it. Allocation, state, health, and readiness
therefore remain independent while a missing stream produces only the stable 503.
