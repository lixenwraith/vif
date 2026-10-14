# Deployment artifacts

| You want to | Go to |
|---|---|
| Deploy a change to the node | `git pull && ./deploy/update.sh --diff`, then `./deploy/update.sh` |
| Operate it | [`runbook.md`](runbook.md) |
| Commission a new node | [`doc/kube-docker-deploy.md`](../doc/kube-docker-deploy.md) |
| Read the design and open work | [`doc/kubernetes-fleet.md`](../doc/kubernetes-fleet.md) |

| Path | What it is |
|---|---|
| `update.sh` | The node's one deploy command: diffs every component against HEAD and LogWisp's binary against upstream `main` (installed green, incoming red), gates on an empty fleet where a component needs it, and runs the helpers below in dependency order, skipping what is current. |
| `docker/Dockerfile` | Multi-stage build: pinned Go builder, `scratch` final layer holding the static non-root `vif_headless` binary and nothing else. Built from the repository root by `./deploy/update.sh image` on the node, or `make image`. |
| `k3s/00-namespace.yaml` | The `vif` namespace with `restricted` Pod Security enforced, and the permissionless service account a session runs as. |
| `k3s/05-log-volume.yaml` | The no-provisioner StorageClass, node-affine local PV, and one shared volatile PVC. Render `${NODE_NAME}` before applying. |
| `k3s/06-log-volume-check.yaml` | A Restricted probe that binds both claims on a fresh node: it writes a record through the log claim and loads a scenario through the wad claim. Render `${IMAGE}` and `${SCENARIO}`, verify its JSONL, then delete the pod and the file. |
| `k3s/07-wad-volume.yaml` | The read-only, node-affine scenario volume every session mounts: its own no-provisioner StorageClass, a `local` PV at `/var/db/vif/wad`, and one `ReadOnlyMany` claim. Render `${NODE_NAME}` before applying. |
| `k3s/10-quota.yaml` | The fleet ceiling: ten concurrent sessions, their compute total, and the two shared local claims — the 256 MiB log tmpfs and the 8 MiB scenario volume. |
| `k3s/20-networkpolicy.yaml` | Default deny in both directions; the game port from anywhere, the operator ports from monitoring only, no egress. |
| `k3s/30-session.yaml` | The per-session template: one Restricted Job, answering to its session ID, writing its commissioned JSONL through the shared log PVC and reading its scenario through the read-only wad PVC, and one owned NodePort Service, the session's direct route. The allocator builds the same objects (`tool/vif-allocator/manifest.go`); a request chooses `${PLAYERS}`, `${BOTS}`, `${SCENARIO}` and `${LOG_LEVEL}`, and each bot grows the resource envelope. Fleet-wide environment such as `GODEBUG` comes from an optional `vif-session-env` ConfigMap, which cannot override `GOMEMLIMIT`. |
| `k3s/40-allocator-rbac.yaml` | The allocator's ServiceAccount and namespace Role for its fixed Job/Service transaction and readiness observation. It deliberately cannot read `pods/log`. |
| `k3s/render-session.sh` | Renders the template as the allocator builds it, bots' envelope included; `SCENARIO`, `LOG_LEVEL`, `BOTS`, `FIRST_JOIN` and `EMPTY_GRACE` override from the environment. `JOB_UID=<uid>` retains the Service owner reference. |
| `k3s/session.sh` | The fleet command line: `allocate` and `state` drive one session through the allocator, `delete` removes one session's objects and log files, `list` and `status` report the fleet, `blockers` is the annotated emptiness assertion `update.sh` and the image and allocator helpers run, `drain [--force]` empties the fleet for them, and `create` is the manual render path that bypasses the allocator. It creates the Job first, owns the Service by the returned Job UID, selects a free fleet port when omitted, and cleans a partial create. |
| `runbook.md` | Day-to-day node operations: status, emptying the fleet before an update, the updates themselves, why a helper refused, and where to watch the stream. |
| `guest/` | The node's identities, mounts, units and update helpers, and how to back one out. See [`guest/README.md`](guest/README.md). |
| `logwisp/aggregator.toml` | Standalone raw file-source pipeline over `/var/log/vif-fleet/*.jsonl`, with bounded flow/clients, TRACE and `admit` records left out, and a loopback-only HTTP sink. |
| `package/` | Distribution files `make install` puts outside the config root: the desktop entry, icons, and bash, zsh and fish completion. |
| `website/vif.nginx.example` | Public edge for the allocator API, the SSE route and the browser session Upgrade route, with the edge rate limits that route needs. Placeholders only; the probe endpoints are not published. |
| `website/vif-log-viewer.html` / `.js` | Bounded same-origin browser reference for `/vif/api/logs`. Caps rendered rows, its pending render queue, and its duplicate fingerprint set; it reaches nothing but its own origin. The script is a separate file because a site that forbids inline script would otherwise silently not run it. |

Nothing here installs itself. Once installed, the allocator creates a session only
when a player asks for one; between requests, the namespace holds no pods.
