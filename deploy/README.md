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
| `k3s/20-networkpolicy.yaml` | Default deny in both directions; the game port from anywhere, the probe port from monitoring only, no egress. |
| `k3s/30-session.yaml` | The per-session template — one Job, one Service. Rendered per session; not applied as it stands. |
| `k3s/40-allocator-rbac.yaml` | The exact permissions the website's allocator needs, and no others. The allocator itself is not in this repository. |
| `k3s/render-session.sh` | Renders the template from a shell, for creating a session by hand. |

Nothing here is applied automatically. A session is created when a player asks for
one; between requests, the namespace holds no pods.
