# Deployment artifacts

The container image, the K3s objects an allocated session is made of, and the
node-side files that carry them. The procedure that installs them, in order, is
[doc/kube_docker_deploy.md](../doc/kube_docker_deploy.md) — **start there** — and
the design and open work behind it are
[doc/kubernetes-fleet.md](../doc/kubernetes-fleet.md).

| Path | What it is |
|---|---|
| `docker/Dockerfile` | Multi-stage build: pinned Go builder, `scratch` final layer holding one static non-root binary and nothing else. Built with `make image` from the repository root. |
| `k3s/00-namespace.yaml` | The `vif` namespace with `restricted` Pod Security enforced, and the permissionless service account a session runs as. |
| `k3s/05-log-volume.yaml` | The no-provisioner StorageClass, node-affine local PV, and one shared volatile PVC. Render `${NODE_NAME}` before applying. |
| `k3s/06-log-volume-check.yaml` | A Restricted writer probe that binds the claim on a fresh node. Render `${IMAGE}`, verify its JSONL, then delete the pod and the file. |
| `k3s/10-quota.yaml` | The fleet ceiling: ten concurrent sessions, their compute total, and exactly one 256 MiB shared log claim. |
| `k3s/20-networkpolicy.yaml` | Default deny in both directions; the game port from anywhere, the operator ports from monitoring only, no egress. |
| `k3s/30-session.yaml` | The per-session template: one Restricted Job writing its commissioned JSONL through the shared local PVC, and one owned NodePort Service. |
| `k3s/40-allocator-rbac.yaml` | The namespace Role for the allocator's fixed Job/Service transaction and readiness observation. It deliberately cannot read `pods/log`. |
| `k3s/render-session.sh` | Renders the template with lifetime overrides. `JOB_UID=<uid>` retains the Service owner reference. |
| `k3s/session.sh` | The fleet command line: `allocate`/`state`/`delete` drive one session through the allocator, `blockers` is the annotated emptiness assertion every update helper runs, `drain [--force]` empties the fleet for them, and `create` is the manual render path that bypasses the allocator. It creates the Job first, owns the Service by the returned Job UID, selects a free fleet port when omitted, and cleans a partial create. |
| `runbook.md` | Day-to-day node operations: status, emptying the fleet before an update, the updates themselves, what each helper's refusal means, and where to watch the stream. |
| `guest/` | The node's identities, mounts, units and update helpers, and how to back one out. See [`guest/README.md`](guest/README.md). |
| `logwisp/REVISION` | Exact upstream LogWisp source revision used for the standalone node binary. It must be reachable from upstream `main`; a pull-request head does not survive a squash merge. |
| `logwisp/aggregator.toml` | Standalone raw file-source pipeline over `/var/log/vif-fleet/*.jsonl`, with bounded flow/clients and a loopback-only HTTP sink. |
| `website/vif.nginx.example` | Public edge for the two allocator API routes: finite timeouts for create/list, and an unbuffered, uncached, long-read location for the SSE stream. Placeholders only; the probe endpoints are not published. |
| `website/vif-log-viewer.html` / `.js` | Bounded same-origin browser reference for `/vif/api/logs`. Caps rendered rows, its pending render queue, and its duplicate fingerprint set; it reaches nothing but its own origin. The script is a separate file because a site that forbids inline script would otherwise silently not run it. |
| `frontdoor/haproxy.cfg` | Not deployed. The worked alternative: every session behind one public port, routed on the name a dialer sends before the handshake. Kept for the routing question in the fleet plan §9; the deployed shape reaches a session on its own port. |

Nothing here installs itself. Once installed, the allocator creates a session only
when a player asks for one; between requests, the namespace holds no pods.
