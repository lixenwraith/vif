# Deployment artifacts

The container image and the K3s objects an allocated session is made of. The
procedure that uses them is [doc/kube_docker_deploy.md](../doc/kube_docker_deploy.md);
the plan and the gap register behind it are
[doc/kubernetes-fleet.md](../doc/kubernetes-fleet.md).


| Path | What it is |
|---|---|
| `docker/Dockerfile` | Multi-stage build: pinned Go builder, `scratch` final layer holding one static non-root binary and nothing else. Built with `make image` from the repository root. |
| `k3s/00-namespace.yaml` | The `vif` namespace with `restricted` Pod Security enforced, and the permissionless service account a session runs as. |
| `k3s/10-quota.yaml` | The fleet ceiling: ten concurrent sessions, and the compute total ten of them may occupy. |
| `k3s/20-networkpolicy.yaml` | Default deny in both directions; the game port from anywhere, the operator ports from monitoring only, no egress. |
| `k3s/30-session.yaml` | The per-session template — one Job, one Service, and the sidecar the render drops unless an image is named. Rendered per session; not applied as it stands. |
| `k3s/40-allocator-rbac.yaml` | The exact namespace permissions prepared for the website's allocator, and no others. The allocator is not implemented yet. |
| `k3s/50-logwisp.yaml` | The optional in-pod LogWisp sidecar configuration used only by the deferred H9 experiment. |
| `k3s/render-session.sh` | Renders the template with lifetime overrides. `JOB_UID=<uid>` retains the Service owner reference; `LOGWISP_IMAGE=<tag>` adds the optional sidecar. |
| `k3s/session.sh` | Repeatable manual create/list/delete path. It creates the Job first, owns the Service by the returned Job UID, selects a free fleet port when omitted, and cleans a partial create. |
| `logwisp/aggregator.toml` | Prepared, not deployed: one LogWisp on the Arch guest will receive enriched pod-log lines from the allocator and serve a merged SSE stream on loopback. |
| `frontdoor/haproxy.cfg` | Not deployed. The worked alternative: every session behind one public port, routed on the name a dialer sends before the handshake. Kept for the routing exploration; the deployed shape reaches a session on its own port. |
| `guest/nftables.conf` | The guest's own filter: one `inet vif` table, replaced on every load, never `flush ruleset`. Its input hook runs after kube-proxy so an endpoint-less NodePort consistently rejects. |
| `guest/vif-operator.nft.example` | Site values the filter includes: the one address allowed to reach the node directly and the node ports it may open. Install as `/etc/nftables.d/vif-operator.nft`. |

Nothing here is applied automatically. A session is created when a player asks for
one; between requests, the namespace holds no pods.
