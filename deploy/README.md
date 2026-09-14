# Deployment artifacts

The container image and the K3s objects an allocated session is made of. The
procedure that uses them is [doc/kube_docker_deploy.md](../doc/kube_docker_deploy.md);
the plan and the gap register behind it are
[doc/kubernetes-fleet.md](../doc/kubernetes-fleet.md). The staged node-local log
migration is authoritative in [doc/kube-todo.md](../doc/kube-todo.md).


| Path | What it is |
|---|---|
| `docker/Dockerfile` | Multi-stage build: pinned Go builder, `scratch` final layer holding one static non-root binary and nothing else. Built with `make image` from the repository root. |
| `k3s/00-namespace.yaml` | The `vif` namespace with `restricted` Pod Security enforced, and the permissionless service account a session runs as. |
| `k3s/05-log-volume.yaml` | Batch B template for the no-provisioner StorageClass, node-affine local PV, and one shared volatile PVC. Render `${NODE_NAME}` before applying. |
| `k3s/06-log-volume-check.yaml` | Temporary Restricted Batch B writer probe. Render `${IMAGE}`, verify its JSONL on the node, then delete the pod and file. |
| `k3s/10-quota.yaml` | The fleet ceiling: ten concurrent sessions, their compute total, and exactly one 256 MiB shared log claim. |
| `k3s/20-networkpolicy.yaml` | Default deny in both directions; the game port from anywhere, the operator ports from monitoring only, no egress. |
| `k3s/30-session.yaml` | The per-session manual template: one Restricted Job writing its commissioned JSONL through the shared local PVC, and one owned NodePort Service. |
| `k3s/40-allocator-rbac.yaml` | The namespace Role for the allocator's fixed Job/Service transaction and readiness observation. It deliberately cannot read `pods/log`. |
| `k3s/render-session.sh` | Renders the file-logging template with lifetime overrides. `JOB_UID=<uid>` retains the Service owner reference; there is no per-session LogWisp branch. |
| `k3s/session.sh` | Repeatable manual create/list/delete path. It creates the Job first, owns the Service by the returned Job UID, selects a free fleet port when omitted, and cleans a partial create. |
| `logwisp/REVISION` | Exact upstream LogWisp source revision used for the standalone node binary. It must be reachable from upstream `main`; a pull-request head does not survive a squash merge. |
| `logwisp/aggregator.toml` | Standalone raw file-source pipeline over `/var/log/vif-fleet/*.jsonl`, with bounded flow/clients and a loopback-only HTTP sink. |
| `website/vif.nginx.example` | Public edge for the two allocator API routes: finite timeouts for create/list, and an unbuffered, uncached, long-read location for the SSE stream. Placeholders only; the probe endpoints are not published. |
| `website/vif-log-viewer.html` / `.js` | Bounded same-origin browser reference for `/vif/api/logs`. Caps rendered rows, its pending render queue, and its duplicate fingerprint set; it reaches nothing but its own origin. The script is a separate file because a site that forbids inline script would otherwise silently not run it. |
| `frontdoor/haproxy.cfg` | Not deployed. The worked alternative: every session behind one public port, routed on the name a dialer sends before the handshake. Kept for the routing exploration; the deployed shape reaches a session on its own port. |
| `guest/nftables.conf` | The guest's own filter: one `inet vif` table, replaced on every load, never `flush ruleset`. Its input hook runs after kube-proxy so an endpoint-less NodePort consistently rejects. |
| `guest/vif-operator.nft.example` | Site values the filter includes: the one address allowed to reach the node directly and the node ports it may open. Install as `/etc/nftables.d/vif-operator.nft`. |
| `guest/var-log-vif\x2dfleet.mount` | The 256 MiB `nodev,nosuid,noexec` tmpfs mount; numeric UID/GID 65532 owns its root. |
| `guest/k3s.service.d/10-vif-fleet-logs.conf` | Makes K3s require and start after the fleet-log mount so failure cannot fall through to root storage. |
| `guest/vif-fleet.sysusers` | Portably creates the locked `vif-fleet` system identity at UID/GID 65532 for the host cleanup service. |
| `guest/vif-fleet-log-cleanup.py` | Refuses non-tmpfs paths, keeps two recent rotations per session after a reader grace, and removes files stale beyond the match ceiling plus margin. |
| `guest/vif-fleet-log-cleanup.service` / `.timer` | Runs the shared-directory cleanup as `vif-fleet` (UID/GID 65532) once per minute. |
| `guest/logwisp.sysusers` | Creates the dedicated locked `logwisp` host identity; the service receives read access through the `vif-fleet` supplementary group only. |
| `guest/logwisp.service` | Hardened standalone file reader with a read-only view of the fleet tmpfs, inaccessible Kubernetes/allocator credentials, and no dependency on games or the allocator. |
| `guest/build-logwisp.sh` | Shared exact-revision builder used by the install and update helpers. Both paths fetch upstream `main` and tags and fail before Docker unless the pin is an ancestor of that head. It can reuse an existing checkout through a temporary detached worktree and restores the disabled Docker/containerd and `FORWARD ACCEPT` baseline. |
| `guest/install-logwisp.sh` | First-install helper for the pinned standalone reader and its locked identity, configuration, and unit. |
| `guest/update-logwisp.sh` | Rebuilds and replaces only LogWisp, retains one known-good binary/config/unit, restarts it, and verifies the new revision and listener. It does not control K3s or the allocator; the fleet procedure supplies their empty-fleet maintenance gate. |
| `guest/vif-allocator.env.example` | Site values for the allocator's imported image, public join host and session-page base URL. |
| `guest/vif-allocator.service` | Hardened host service for the website-to-K3s allocator on the systemd K3s node. |
| `guest/vif-allocator-token.service` / `.timer` | Root-only, atomic rotation of the allocator's short-lived ServiceAccount token. |
| `guest/vif-allocator-refresh-token.sh` | Token rotation implementation used by the oneshot service. |
| `guest/update-vif-allocator.sh` | Builds and updates the allocator as one guarded cutover after pausing allocation and proving the fleet empty; retains one known-good binary/config/unit. |
| `guest/update-vif-image.sh` | Repeatable manual release path: one Docker build/check, K3s import, allocator image update, old-image cleanup, then build daemons disabled again. |

Nothing here installs itself. Once installed, the allocator creates a session only
when a player asks for one; between requests, the namespace holds no pods.
