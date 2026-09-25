# Node runbook

Day-to-day operations on the K3s node, from the vif repository root.
[`../doc/kube-docker-deploy.md`](../doc/kube-docker-deploy.md) commissions a node;
this page is what you run afterwards.

## Deploy a change

```sh
git pull
./deploy/update.sh --diff      # what differs, per component; changes nothing
./deploy/update.sh             # the same diffs, then the updates, in order
./deploy/update.sh wad bridge  # only the named components
```

Components are `objects` (namespace, quota, policy, Role), `wad`, `bridge`,
`image`, `allocator` and `logwisp`. A current one is skipped. Installed lines print
green and incoming ones red; a file the update removes prints as `deleted <path>`.
`image`, `allocator` and `logwisp` need an empty fleet and refuse otherwise, naming
the components that do not; `wad` and `bridge` never interrupt a match, since a
running pod keeps what it started with. Each component keeps one `.previous` set,
and [`guest/README.md`](guest/README.md) restores it. Settings for the allocator are
[`guest/vif-allocator.env`](guest/vif-allocator.env), committed here and never
edited on the node.

## Where things stand

```sh
./deploy/k3s/session.sh status
./deploy/k3s/session.sh blockers
./deploy/k3s/session.sh state '<session-id>'
```

`status` gives every fleet object a verdict — a match in play, a Job that
finished and is waiting out its `ttlSecondsAfterFinished`, a pod still
terminating — then the fleet log files and the five units. `blockers` is that
report as an assertion: `the fleet is empty` and exit 0, or the same annotated
lines and exit 1. It is the check every update helper runs first, so run it by
hand to learn why one refused without starting another. `state` prints one line
of what the fleet reports about one session — phase, roster, clock, tick, the
window it has left and its join target — and exits non-zero once that session has
left the fleet.

## Empty the fleet

`image`, `allocator` and `logwisp` refuse to update while any fleet object exists.

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

A single session goes without touching the rest. `delete` waits for the
background-cascaded pod, removes only that session's log files, and prints its own
verdict — so read the session's JSONL before running it, not after:

```sh
./deploy/k3s/session.sh delete '<session-id>'
```

## Ask for a session

The path a player takes, from the node. It prints the identity on stdout and the
join target and page beside it, so a shell can capture one and a person can read
the other:

```sh
SESSION_ID=$(./deploy/k3s/session.sh allocate)       # deployment defaults
SESSION_ID=$(./deploy/k3s/session.sh allocate 1 debug)
```

Both arguments are optional and bounded by `-players-max` and `-log-level-min`;
`GET /vif/api/sessions` advertises what this deployment accepts as `limits`.

## Why a helper refused

| Message | Cause | Fix |
|---|---|---|
| `the fleet is not empty` | Printed by `blockers` above one annotated line per surviving object, then by the helper that called it. A finished Job is the usual cause: it outlives the match by its 120-second `ttlSecondsAfterFinished`, so the public session list is already empty while the update still refuses. | Wait out the TTL the report names, or `./deploy/k3s/session.sh drain`. |
| `a fleet object remained after allocation stopped` | A session was created between the first check and the stop. | Re-run `drain`; it waits for the cascade. |
| `the fleet did not drain` | A pod outlived the background cascade by more than 60 seconds. | `./deploy/k3s/session.sh drain --force` |
| `the worktree differs from HEAD` | Uncommitted changes. Updates build from HEAD, so they refuse to install something the tree does not describe; `--diff` still runs. | Commit, stash, or check out the revision you mean to deploy. |
| `empty the fleet first … or update only: …` | `image`, `allocator` or `logwisp` differs while a fleet object exists. | `./deploy/k3s/session.sh drain`, or run only the components it names. |
| `… is not in K3s; run deploy/guest/update-vif-ws-bridge.sh first` | The allocator would name a bridge image containerd does not hold. | `./deploy/update.sh bridge` |
| `websocat 1.x`, `git is required`, `cargo is required` | No websocat package and no toolchain to build the pinned release. | Install websocat (AUR), or `sudo pacman -S git rust`. |
| `pinned revision is not an ancestor of LogWisp main` | `deploy/logwisp/REVISION` names a commit that upstream `main` does not contain, usually a pull-request head a squash merge discarded. | Repin to the merged commit on `main`. |
| `logwisp.service must be active before an update` | The updater replaces a running service and keeps one rollback set; it will not install onto a stopped one. | `sudo systemctl start logwisp.service` |
| `the served stream bounds are not {...}` | The restarted LogWisp is not serving the queue, connection and timeout bounds in `deploy/logwisp/aggregator.toml`, so the install did not take. The previous build is already back. | Compare the message against `curl -fsS http://127.0.0.1:8081/status \| jq .server`. A pinned revision too old to carry a setting is the usual cause. |
| `cannot read the sink bounds from ...aggregator.toml` | The HTTP sink block lost one of `client_buffer_size`, `max_connections` or `write_timeout_ms`, which the verification reads from it. | Restore the setting; the updater will not install a configuration it cannot check. |
| `docker.service must be inactive before the temporary build` | Docker is a build tool here, not a runtime, and the node baseline keeps it disabled. | `sudo systemctl disable --now docker.service docker.socket containerd.service` |

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

`vif-allocator.service` is `Type=notify`, so `systemctl start` returns once the
allocator answers; a probe straight after one is not a race.

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

## Who dialled a session

The accepted socket's address is recorded on the node and dropped from the
published stream, so this answers "where did that player come from" and the SSE
never can:

```sh
sudo jq -c 'select(.sub == "admit")' '/var/log/vif-fleet/<session-id>.jsonl'
```

## Preserve

`drain` and the cleanup timer both remove log files. Neither touches the mounted
tmpfs or the Bound PV/PVC, and nothing here should: deleting either is a
commissioning operation, not an operational one, and
[`guest/README.md`](guest/README.md) is where it lives.

After any change to a live workload, allocator binary, Role, mount or logging
service, finish with
[§13 of the procedure](../doc/kube-docker-deploy.md#13-first-session).
