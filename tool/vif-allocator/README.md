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
[`doc/kube_docker_deploy.md`](../../doc/kube_docker_deploy.md#10-the-allocator-and-website-contract).

## HTTP API

| Request | Result |
|---|---|
| `POST /vif/api/sessions` with an empty body or `{}` | `201` and one created session. Callers cannot select an image or workload field. |
| `GET /vif/api/sessions` | `200` and `{ "sessions": [...] }` for live, non-completed Jobs. |
| `GET /healthz` | Process liveness. |
| `GET /readyz` | Verifies that the current token can reach the Kubernetes API. |
| `GET /vif/api/logs` | `501` until the node LogWisp fan-in is installed. |

One session row has this shape:

```json
{
  "id": "3c3a8eec0cbb6f17",
  "port": 31700,
  "page_url": "https://lixen.com/projects/vi-fighter/session/31700/",
  "join_target": "lixen.com:31700",
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

The allocator deliberately exposes no public delete endpoint: this API is
anonymous behind the site, and one player must not be able to terminate another
player's match. Sessions expire themselves; operators retain `kubectl` and
`deploy/k3s/session.sh delete` for exceptional cleanup.
