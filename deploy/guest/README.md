# Linux node artifacts

The host-side files a K3s node is commissioned with: identities, mounts, units,
and the helpers that replace one component at a time. The production path is an
Arch Linux guest; the same files apply to a bare Ubuntu node.

**These are not a procedure.** The order they install in is
[`doc/kube-docker-deploy.md`](../../doc/kube-docker-deploy.md), which is the entry
point for a new deployment. [`../runbook.md`](../runbook.md) is what you run once
the node is up. This file says where each artifact lands, what it needs, and how to
back one out.

## Where each artifact installs

| Artifact | Destination | Owner and mode | Requires |
|---|---|---|---|
| `vif-fleet.sysusers` | `/etc/sysusers.d/vif-fleet.conf` | root 0644 | `systemd-sysusers`; creates the locked identity at UID/GID 65532 |
| `var-log-vif\x2dfleet.mount` | `/etc/systemd/system/` | root 0644 | the identity above; mounts 256 MiB `nodev,nosuid,noexec` tmpfs at mode 770 |
| `k3s.service.d/10-vif-fleet-logs.conf` | `/etc/systemd/system/k3s.service.d/` | root 0644 | the mount unit; makes K3s require and start after it |
| `vif-fleet-log-cleanup.py` | `/usr/local/libexec/` | root 0644 | `python3`; refuses a non-tmpfs path |
| `vif-fleet-log-cleanup.service` / `.timer` | `/etc/systemd/system/` | root 0644 | runs the cleanup as `vif-fleet` once per minute |
| `nftables.conf` | `/etc/nftables.conf` | root 0644 | the include below; owns one `inet vif` table and never flushes the ruleset |
| `vif-operator.nft.example` | `/etc/nftables.d/vif-operator.nft` | root 0644 | site values: the one address allowed to reach the node, and its ports |
| `logwisp.sysusers` | `/etc/sysusers.d/` | root 0644 | creates the locked `logwisp` identity; tmpfs read access comes from the `vif-fleet` supplementary group alone |
| `logwisp.service` | `/etc/systemd/system/` | root 0644 | read-only view of the fleet tmpfs, inaccessible K3s and allocator credential paths, no dependency on games or the allocator |
| `../logwisp/aggregator.toml` | `/etc/logwisp/vif-fleet.toml` | root 0644 | `raw = true`, `from = "start"`, bounded flow and clients, loopback-only sink |
| `vif-allocator.env.example` | `/etc/vif-allocator/allocator.env` | `root:vif-allocator` 0640 | the imported image tag, the public join host, the session page base, and the scenarios a request may select |
| `vif-allocator.service` | `/etc/systemd/system/` | root 0644 | `/usr/local/bin/vif-allocator`, the CA copy and the token file; `Type=notify` |
| `vif-allocator-token.service` / `.timer` | `/etc/systemd/system/` | root 0644 | root-only atomic rotation of the short-lived ServiceAccount token, every six hours |
| `vif-allocator-refresh-token.sh` | `/usr/local/libexec/vif-allocator-refresh-token` | root 0755 | the rotation the oneshot runs |

`/etc/vif-allocator` is `root:vif-allocator` 0750, so reading `allocator.env` needs
`sudo`. A helper that reports it missing without one is out of date.

## Helpers

Each builds first, keeps one rollback set at `.previous`, restores it if
verification fails, and leaves Docker, its socket and the distribution containerd
disabled with `FORWARD ACCEPT` restored.

| Helper | What it replaces | What it refuses |
|---|---|---|
| `install-logwisp.sh [checkout]` | first install of the pinned binary, identity, configuration and unit | an existing installation; a pin that is not an ancestor of upstream `main` |
| `update-logwisp.sh [checkout]` | LogWisp's binary, configuration and unit only | a stopped `logwisp.service`; the same unreachable pin |
| `update-vif-allocator.sh` | the allocator binary, env and unit | a dirty worktree; a non-empty fleet |
| `update-vif-image.sh [tag]` | the headless session image, and `VIF_ALLOCATOR_IMAGE` with it; rejects an image without the `headless` profile label | a dirty worktree; an occupied fleet |
| `update-vif-wad.sh [dir]` | the node's scenario volume at `/var/db/vif/wad`, by atomic rename; validates every scenario against the session image first | a tree that is not laid out like `wad/`; a scenario the image cannot load. **Not** an occupied fleet: a running match keeps the tree it mounted |

An optional checkout argument is a download optimisation, never a working
directory: the builder creates a temporary detached worktree at the pinned revision
and does not switch, pull, clean or modify the branch it finds. Every refusal
message is explained in [`../runbook.md`](../runbook.md).

## Backing one component out

Each block assumes allocation is stopped and the fleet is empty:
`./deploy/k3s/session.sh blockers`, then `drain` if it refuses. None of them lowers
Pod Security, deletes a Bound PV/PVC in use, or unmounts the tmpfs while K3s runs.

### The allocator, or LogWisp

Restore the updater's retained set. `.previous` is one update back, not a fixed
version — each run of the updater overwrites it.

```sh
sudo systemctl stop vif-allocator.service
sudo install -o root -g root -m 0755 \
  /usr/local/libexec/vif-allocator.previous /usr/local/bin/vif-allocator
sudo install -o root -g vif-allocator -m 0640 \
  /etc/vif-allocator/allocator.env.previous /etc/vif-allocator/allocator.env
sudo install -o root -g root -m 0644 \
  /etc/systemd/system/vif-allocator.service.previous \
  /etc/systemd/system/vif-allocator.service
sudo systemctl daemon-reload
sudo systemctl start vif-allocator.service
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:9080/readyz
```

`update-logwisp.sh` prints the equivalent `.previous` paths for the LogWisp binary,
configuration and unit, and restores them itself when its own verification fails.

If LogWisp's isolation or loopback checks fail, remove only its live listener and
keep the pinned binary, configuration and locked identity for diagnosis. Nothing
else is touched: the allocator, K3s, the writer, the PV/PVC, the tmpfs and the
Restricted namespace all stay as they are.

```sh
sudo systemctl disable --now logwisp.service
sudo rm /etc/systemd/system/logwisp.service
sudo systemctl daemon-reload
sudo ss -ltnH 'sport = :8081'      # must print nothing
```

### The allocator Role

If an allocator operation fails specifically because a removed permission is
required, restore only that exact grant and diagnose before deploying anything
else: a live Role that differs from the repository is a drift, not a fix.

```sh
sudo systemctl stop vif-allocator.service
sudo kubectl -n vif patch role vif-allocator --type=json \
  -p='[{"op":"add","path":"/rules/-","value":{"apiGroups":[""],"resources":["pods/log"],"verbs":["get"]}}]'
sudo systemctl start vif-allocator.service
```

Do not restore `pods/log` for a node-file, LogWisp, or public-stream failure. None
of those components is allowed to use the Kubernetes log API.

### The fleet-log storage

Decommissioning, not an operational step. Stop if this prints anything — a pod
still holds the claim:

```sh
sudo kubectl get pods --all-namespaces -o json | jq -e \
  '.items[] | select(any(.spec.volumes[]?;
    .persistentVolumeClaim.claimName == "vif-fleet-logs"))'
```

With no consumers, stop K3s, remove only the new dependency, and make the uncovered
root-filesystem directory unwritable before restarting, so a later writer cannot
quietly fill `/`:

```sh
MOUNT_UNIT='var-log-vif\x2dfleet.mount'
sudo systemctl stop k3s.service
sudo systemctl disable --now vif-fleet-log-cleanup.timer
sudo systemctl stop "$MOUNT_UNIT"
sudo chmod 000 /var/log/vif-fleet
sudo rm /etc/systemd/system/k3s.service.d/10-vif-fleet-logs.conf
sudo systemctl daemon-reload
sudo systemctl restart k3s.service
sudo systemctl restart vif-allocator-token.service
sudo systemctl start vif-allocator.service
```

Keep the Retain PV/PVC for diagnosis, and do not schedule a PVC writer afterwards:
either restore the mount and the drop-in, or deliberately remove the unused claim
and PV before retrying.
