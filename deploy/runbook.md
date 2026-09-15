# Node runbook

Day-to-day operations on the K3s node, from the vi-fighter repository root. The
batch procedures in [`guest/README.md`](guest/README.md) commission a node; this
page is what you run afterwards. Every command here is the canonical one — if a
procedure shows a longer variant, it is proving something extra.

## Where things stand

```sh
./deploy/k3s/session.sh status
./deploy/k3s/session.sh blockers
```

`status` gives every fleet object a verdict — a match in play, a Job that
finished and is waiting out its `ttlSecondsAfterFinished`, a pod still
terminating — then the fleet log files and the five units. `blockers` is that
report as an assertion: `the fleet is empty` and exit 0, or the same annotated
lines and exit 1. It is the check every update helper runs first, so run it by
hand to learn why one refused without starting another.

## Empty the fleet

The update helpers refuse to run while any fleet object exists, and the LogWisp
gate also requires an empty tmpfs.

```sh
./deploy/k3s/session.sh drain
./deploy/k3s/session.sh drain --force    # only after the plain form timed out
```

It deletes every fleet Job and Service, waits up to 60 seconds for the
background-cascaded pods, removes the session log files, and fails loudly if
anything survives. `--force` then abandons the surviving pods with a zero grace
period: nothing replaces them, because their Jobs are already gone, but a
container can outlive its object until the kubelet reaps it and its NodePort
frees only then. **Either form destroys retained log evidence** — snapshot
first if a gate still needs it:

```sh
sudo cp -a /var/log/vif-fleet/. "$(mktemp -d /tmp/vif-logs.XXXXXX)/"
```

A single session goes without touching the rest:

```sh
./deploy/k3s/session.sh delete '<session-id>'
```

## Why a helper refused

| Message | Cause | Fix |
|---|---|---|
| `the fleet is not empty` | Printed by `blockers` above one annotated line per surviving object, then by the helper that called it. A finished Job is the usual cause: it outlives the match by its 120-second `ttlSecondsAfterFinished`, so the public session list is already empty while the update still refuses. | Wait out the TTL the report names, or `./deploy/k3s/session.sh drain`. |
| `a fleet object remained after allocation stopped` | A session was created between the first check and the stop. | Re-run `drain`; it waits for the cascade. |
| `the fleet did not drain` | A pod outlived the background cascade by more than 60 seconds. | `./deploy/k3s/session.sh drain --force` |
| `the vi-fighter worktree differs from HEAD` | Uncommitted changes. The updater builds from HEAD, so it refuses to install something the tree does not describe. | Commit, stash, or check out the revision you mean to deploy. |
| `missing installed file: /etc/vif-allocator/allocator.env` | Not actually missing: that directory is `root:vif-allocator` 0750. An older helper tested it unprivileged. | Update the checkout; the helper reads it through `sudo`. |
| `pinned revision is not an ancestor of LogWisp main` | `deploy/logwisp/REVISION` names a commit that upstream `main` does not contain, usually a pull-request head a squash merge discarded. | Repin to the merged commit on `main`. |
| `logwisp.service must be active before an update` | The updater replaces a running service and keeps one rollback set; it will not install onto a stopped one. | `sudo systemctl start logwisp.service` |
| `docker.service must be inactive before the temporary build` | Docker is a build tool here, not a runtime, and the node baseline keeps it disabled. | `sudo systemctl disable --now docker.service docker.socket containerd.service` |

## Updates

Each builds first, keeps one rollback set at `.previous`, and restores it if
verification fails. Announce the pause: no session can be created while the
allocator is down, and the stream stops while LogWisp restarts.

```sh
./deploy/guest/update-vif-allocator.sh          # allocator binary, env, unit
./deploy/guest/update-logwisp.sh                # pinned LogWisp only
./deploy/guest/update-vif-image.sh              # session image, tag from HEAD
./deploy/guest/update-vif-image.sh v1.2.3       # session image, explicit tag
```

The allocator and image updaters check for an idle fleet but do not empty one —
drain first. The LogWisp one does not touch K3s or the allocator, so on a fleet
node run it inside
the guarded gate in [`guest/README.md`](guest/README.md), which stops allocation
and restarts it through a trap.

Roll one back by restoring its `.previous` set; the per-batch rollback blocks in
[`guest/README.md`](guest/README.md) carry the exact commands, including the
readiness wait. `.previous` is one update back, not a fixed version.

## Watch the stream

On the node:

```sh
curl --no-buffer -fsS http://127.0.0.1:9080/vif/api/logs
curl -fsS http://127.0.0.1:8081/status | jq '.server, .statistics'
```

Through the published edge, from anywhere:

```sh
curl --no-buffer -fsS https://<site-host>/vif/api/logs
```

`deploy/website/vif-log-viewer.html` is the browser equivalent; serve it and its
`.js` from the site's document root.

## Health

```sh
curl -fsS http://127.0.0.1:9080/healthz
curl -fsS http://127.0.0.1:9080/readyz
sudo journalctl -u vif-allocator.service -n 50 --no-pager
sudo journalctl -u logwisp.service -n 50 --no-pager
```

Judge LogWisp's journal by the invocation running the pinned binary; earlier
entries came from whatever it replaced:

```sh
sudo journalctl \
  "_SYSTEMD_INVOCATION_ID=$(systemctl show logwisp.service -p InvocationID --value)" \
  --no-pager
```

## Preserve

`drain` and the cleanup timer both remove log files. Neither touches the mounted
tmpfs or the Bound PV/PVC, and nothing here should: deleting either is a
commissioning operation, not an operational one.
