# Node runbook

Day-to-day operations on the K3s node, from the vif repository root.
[`../doc/kube-docker-deploy.md`](../doc/kube-docker-deploy.md) commissions a node;
this page is what you run afterwards.

## Deploy a change

```sh
git pull
./deploy/update.sh --diff      # what differs, per component; changes nothing
./deploy/update.sh             # the same diffs, then the updates, in order
./deploy/update.sh wad image   # only the named components
```

Components are `filter` (the node's `inet vif` table), `objects` (namespace,
quota, policy, Role), `wad`, `image`, `router` and `logwisp`. A current one is
skipped. Installed lines print green and incoming ones red; a file the update
removes prints as `deleted <path>`. `image`, `router` and `logwisp` need an empty
fleet and refuse otherwise, naming the components that do not; `filter` and `wad`
never interrupt a match, since established connections and running pods keep what
they started with. `router` and `logwisp` keep one `.previous` set, which
[`guest/README.md`](guest/README.md) restores; every other component comes back by
running `update.sh` for it at the earlier revision, which rebuilds the session image
that `image` removed. Settings for the router are
[`guest/vif-router.env`](guest/vif-router.env), committed here and never
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
lines and exit 1. It is the check `update.sh` and the image and router helpers
make before they replace anything, so run it by hand to learn why one refused
without starting another. `state` prints one line of what the fleet reports about
one session — phase, roster, clock, tick, the window it has left and its join
target — and exits non-zero once that session has left the fleet.

## Empty the fleet

`image`, `router` and `logwisp` refuse to update while any fleet object exists.

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
join and direct targets on stderr, with the browser link where one is published, so
a shell can capture one and a person can read the other:

```sh
SESSION_ID=$(./deploy/k3s/session.sh allocate)       # deployment defaults
SESSION_ID=$(./deploy/k3s/session.sh allocate 1 debug td)
```

The arguments — players, log level, scenario, bots — are optional, in that order,
and bounded by `-players-max`, `-log-level-min`, the installed scenarios and
`-bots-max`; `GET /vif/api/sessions` advertises what this deployment accepts as
`limits`.

## Why a helper refused

| Message | Cause | Fix |
|---|---|---|
| `the fleet is not empty` | Printed by `blockers` after one annotated line per surviving object; the helper that called it then names its own refusal. A finished Job is the usual cause: it outlives the match by its 120-second `ttlSecondsAfterFinished`, so the public session list is already empty while the update still refuses. | Wait out the TTL the report names, or `./deploy/k3s/session.sh drain`. |
| `a fleet object remained after allocation stopped` | A session was created between the first check and the stop. | Re-run `drain`; it waits for the cascade. |
| `the fleet did not drain` | A pod outlived the background cascade by more than 60 seconds. | `./deploy/k3s/session.sh drain --force` |
| `the worktree differs from HEAD` | Uncommitted changes. Updates build from HEAD, so they refuse to install something the tree does not describe; `--diff` still runs. | Commit, stash, or check out the revision you mean to deploy. |
| `empty the fleet first … or update only: …` | `image`, `router` or `logwisp` differs while a fleet object exists. | `./deploy/k3s/session.sh drain`, or run only the components it names. |
| `<component>: could not tell what is installed`, `… names no VIF_ROUTER_IMAGE; run update-vif-image.sh first` | `update.sh` checks every selected component before it changes any, and could not read this one; the line above says why. For `router` it is an installed env without `VIF_ROUTER_IMAGE`; for `logwisp`, upstream `main` unreachable. | `./deploy/update.sh image` first, or retry once upstream answers. |
| `revision is not on LogWisp main` | A revision given to a LogWisp helper names a commit upstream `main` does not contain: a pull-request head a squash merge discarded, or one a rewritten `main` dropped. | Give a commit on `main`, or none for its head. |
| `logwisp.service must be active before an update`, `vif-router.service must be active …` | The updater replaces a running service and keeps one rollback set; it will not install onto a stopped one. | `sudo systemctl start` the unit it names |
| `missing installed file: …` | The router or LogWisp updater found nothing installed to replace: the component was never installed on this node. | Install it first, [§10](../doc/kube-docker-deploy.md#10-logwisp-the-node-log-reader) or [§11](../doc/kube-docker-deploy.md#11-the-router) of the procedure. |
| `refusing to replace existing path: …` | `install-logwisp.sh` is the first install only, and LogWisp is already there. | `./deploy/update.sh logwisp` |
| `the vif-fleet identity is missing; install the log tmpfs first` | `install-logwisp.sh` reads the session logs through the `vif-fleet` group, which the log tmpfs install creates. | Install the log tmpfs ([§7](../doc/kube-docker-deploy.md#7-the-nodes-log-tmpfs)) first. |
| `the served stream bounds are not {...}` | The restarted LogWisp is not serving the queue, connection and timeout bounds in `deploy/logwisp/aggregator.toml`, so the install did not take. The previous build is already back. | Compare the message against `curl -fsS http://127.0.0.1:8081/status \| jq .server`. A LogWisp revision that lacks a setting is the usual cause. |
| `cannot read the sink bounds from ...aggregator.toml` | The HTTP sink block lost one of `client_buffer_size`, `max_connections` or `write_timeout_ms`, which the verification reads from it. | Restore the setting; the updater will not install a configuration it cannot check. |
| `… must be disabled before the temporary build`, `containerd.service must be inactive …` | Docker is stopped, but a unit the build's cleanup would disable is enabled, or the distribution containerd runs without it. The node baseline keeps all three disabled. | `sudo systemctl disable --now docker.service docker.socket containerd.service` |
| `Docker has no buildx plugin, which this build needs` | The session image and LogWisp builds need BuildKit (cache mounts, a build secret); without the plugin, `docker build` falls back to the legacy builder, which rejects them. Checked before Docker starts. | `sudo pacman -S --needed docker-buildx` |

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

`vif-router.service` is `Type=notify`, so `systemctl start` returns once the
router answers; a probe straight after one is not a race.

```sh
curl -fsS http://127.0.0.1:9080/healthz
curl -fsS http://127.0.0.1:9080/readyz
sudo journalctl -u vif-router.service -n 50 --no-pager
sudo journalctl -u logwisp.service -n 50 --no-pager
```

Judge LogWisp's journal by the invocation running the installed binary; earlier
entries came from whatever it replaced:

```sh
sudo journalctl \
  "_SYSTEMD_INVOCATION_ID=$(systemctl show logwisp.service -p InvocationID --value)" \
  --no-pager
```

## Who dialled a session

The accepted socket's address is recorded on the node and dropped from the
published stream, so this answers "where did that player come from" and the SSE
never can. Only a direct join names the player: one through the front door or the
browser route is recorded at the node's address, since the router dials the pod.

```sh
sudo jq -c 'select(.sub == "admit")' '/var/log/vif-fleet/<session-id>.jsonl'
```

## Preserve

`drain` and the cleanup timer both remove log files. Neither touches the mounted
tmpfs or the Bound PV/PVC, and nothing here should: deleting either is a
commissioning operation, not an operational one, and
[`guest/README.md`](guest/README.md) is where it lives.

After any change to a live workload, router binary, Role, mount or logging
service, finish with
[§13 of the procedure](../doc/kube-docker-deploy.md#13-first-session).
