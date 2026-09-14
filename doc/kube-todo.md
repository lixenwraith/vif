# Kubernetes fleet: remaining implementation plan

This is the authoritative order for unfinished Kubernetes fleet work. Completed
design and operating detail lives in:

- [the deployment procedure](kube_docker_deploy.md);
- [the fleet architecture and verification matrix](kubernetes-fleet.md); and
- [the Linux node artifacts and batch procedures](../deploy/guest/README.md).

Status on 2026-09-14: Batches A-F are deployed and passed their live gates, and
the site publishes the two API routes. What those gates established is recorded
in the fleet plan and the deployment procedure linked above; this file holds only
what is left.

Two things a reader needs before continuing. `deploy/logwisp/REVISION` is
`6046f5c56b583ce3800f69c639874048b3dd8b69`. The node runs the allocator that the
Batch F rollback restored, which is one update behind the repository, so
`./deploy/guest/update-vif-allocator.sh` is due before anything else is measured
on it.

## 1. Invariants and batch discipline

These constraints apply to every remaining batch:

- Keep the `vif` namespace at Pod Security `restricted`.
- Session pods mount only the `vif-fleet-logs` PVC. Never add a direct
  `hostPath`; the PV alone maps the node's `/var/log/vif-fleet` tmpfs.
- The game writes complete JSONL records. No component tails Kubernetes pod logs,
  splices allocator JSON, or inserts a session field after serialization.
- LogWisp is one independent node service. It is never an allocator child or a
  per-session container and receives no Kubernetes credential.
- The allocator may proxy LogWisp bytes but must not parse, normalize, buffer, or
  own the stream processor.
- Stop allocation and prove the fleet is empty before changing a live workload,
  allocator binary, Role, mount, or logging service.
- Pin LogWisp to a commit reachable from upstream `main`, never a pull-request
  head, and judge its journal only by the invocation running the pinned binary.
- Announce restarts, simultaneous clients, and deadline-sensitive joins before
  running them.
- Put placeholders in repository commands; never commit real machine addresses.
- Remove rendered files, probe pods, verification Jobs/Services, and verification
  JSONL after each gate. Preserve the mounted tmpfs and Bound PV/PVC.
- Finish every batch with the common single-session check in §3 plus its
  batch-specific gate.
- Keep the previous allocator binary/configuration available until the next live
  gate passes.

The node procedure must remain runnable from a bare systemd-based Arch Linux or
Ubuntu installation. Distribution branches are allowed only where package or
service defaults differ.

## 2. Remaining phases and their goals

Where the pivot ends: a player's browser reads its own session's log lines from
the website over one same-origin route, while nothing in that path can read a
Kubernetes pod log, hold a cluster credential, or end a game by failing.

| Batch | Goal | State |
|---|---|---|
| H15 — public edge | A browser reads its own session's lines over one same-origin `EventSource`, and the probe endpoints stay on the node. | Routes published and verified live 2026-09-14; the site's own session page remains. |
| G — final reconciliation | Leave a deployment a stranger can install from bare Arch or Ubuntu, described only as deployed, with limits justified by measurement instead of single-guest history. | Next. |

§5 holds the remaining non-logging gates. H3's sizing measurement runs inside G;
H1, H11 and H12 bound what a stranger can do to an open game port, and gate
public exposure rather than this pivot.

## 3. Common end-of-batch session check

Run this after every remaining batch. A batch whose own gate interleaves with it,
as Batch F's F5 does, runs these blocks in that order instead of separately. Have
the remote development-machine terminal ready before allocation: the 90-second
first-join clock starts when `POST` returns.

On the node:

```sh
unset SESSION_JSON SESSION_ID JOIN_TARGET

command -v jq
curl -fsS http://127.0.0.1:9080/healthz
curl -fsS http://127.0.0.1:9080/readyz

SESSION_JSON=$(curl -fsS -X POST \
  -H 'Content-Type: application/json' -d '{}' \
  http://127.0.0.1:9080/vif/api/sessions) &&
SESSION_ID=$(printf '%s' "$SESSION_JSON" |
  jq -er '.id | strings | select(length > 0)') &&
JOIN_TARGET=$(printf '%s' "$SESSION_JSON" |
  jq -er '.join_target | strings | select(length > 0)') &&
printf 'session=%s join=%s\n' "$SESSION_ID" "$JOIN_TARGET"
```

Stop if either value is empty. Immediately join from the prepared development
machine:

```sh
bin/vif -join '<join_target>'
```

While it remains connected, verify the Job shape and occupied state:

```sh
sudo kubectl -n vif get job "vif-session-$SESSION_ID" -o json |
  jq -e --arg id "$SESSION_ID" '
    .spec.template.spec as $pod |
    ($pod.automountServiceAccountToken == false) and
    ($pod.containers | length == 1) and
    ($pod.containers[0].name == "session") and
    ($pod.containers[0].args |
      index("-l=/var/log/vif-fleet") != null) and
    ($pod.containers[0].args |
      index("-log-session-id=" + $id) != null) and
    ($pod.containers[0].args | index("-log-stdout") == null) and
    any($pod.containers[0].volumeMounts[]?;
      .name == "fleet-logs" and
      .mountPath == "/var/log/vif-fleet") and
    any($pod.volumes[]?;
      .name == "fleet-logs" and
      .persistentVolumeClaim.claimName == "vif-fleet-logs") and
    all($pod.volumes[]?; has("hostPath") | not) and
    all($pod.initContainers[]?;
      ((.volumeMounts // []) | length) == 0) and
    ($pod.containers[0].securityContext.allowPrivilegeEscalation == false) and
    ($pod.containers[0].securityContext.readOnlyRootFilesystem == true) and
    ($pod.containers[0].securityContext.runAsNonRoot == true) and
    ($pod.containers[0].securityContext.capabilities.drop |
      index("ALL") != null)'

curl -fsS http://127.0.0.1:9080/vif/api/sessions |
  jq -e --arg id "$SESSION_ID" '
    any(.sessions[]; .id == $id and
      .state.phase == "occupied" and .state.guests >= 1)' &&
  printf 'session occupied\n'
```

Quit the remote client and immediately verify vacancy and the commissioned file:

```sh
curl -fsS http://127.0.0.1:9080/vif/api/sessions |
  jq -e --arg id "$SESSION_ID" '
    any(.sessions[]; .id == $id and
      .state.phase == "vacant" and .state.guests == 0)' &&
  printf 'session vacant\n'

sudo test -s "/var/log/vif-fleet/$SESSION_ID.jsonl"
sudo jq -s -e --arg id "$SESSION_ID" '
  map(select(.sub != null)) as $records |
  ($records | length > 0) and
  all($records[]; .fields.session_id == $id)
' "/var/log/vif-fleet/$SESSION_ID.jsonl"
```

Delete the session before its empty grace/TTL removes the evidence, wait for the
background-cascaded pod, and remove only that session's files:

```sh
./deploy/k3s/session.sh delete "$SESSION_ID"
sudo kubectl -n vif wait --for=delete \
  "job/vif-session-$SESSION_ID" --timeout=60s
sudo kubectl -n vif wait --for=delete pod \
  -l "vif.lixenwraith.dev/session=$SESSION_ID" --timeout=60s
sudo kubectl -n vif get job,pod,service \
  -l "vif.lixenwraith.dev/session=$SESSION_ID"

sudo find /var/log/vif-fleet -maxdepth 1 -type f \
  \( -name "$SESSION_ID.jsonl" -o -name "${SESSION_ID}_*.jsonl" \) \
  -delete
sudo find /var/log/vif-fleet \
  -mindepth 1 -maxdepth 1 -print
```

The two fleet queries and final `find` must be empty. Finish with:

```sh
systemctl is-active \
  'var-log-vif\x2dfleet.mount' k3s.service \
  vif-allocator.service vif-fleet-log-cleanup.timer
systemctl show vif-fleet-log-cleanup.service \
  -p User -p Group -p Result -p ExecMainStatus
sudo kubectl get namespace vif --show-labels
sudo kubectl get persistentvolume vif-fleet-logs
sudo kubectl -n vif get persistentvolumeclaim vif-fleet-logs
```

Expected: all four units active, cleanup last result successful as `vif-fleet`,
Restricted labels intact, and PV/PVC Bound.

## 4. Batch G — reconcile and hand off

After Batch F passes live:

1. Reduce `doc/kube_docker_deploy.md`, `doc/kubernetes-fleet.md`,
   `deploy/README.md`, and `deploy/guest/README.md` to the deployed design.
   Replace batch-by-batch narration with measured outcomes, and delete the
   superseded console/sidecar/stdout-public-path prose that remains.
2. Rehearse the complete deployment from bare Arch Linux and bare Ubuntu. Record
   package/service differences and fix every command that assumes this node.
3. Run the ten-session/four-player measurement (H3). Record CPU, memory, tmpfs
   usage, log rate, tick slips, rotations, LogWisp drops/replay, and browser
   reconnect behavior; revise the provisional 256 MiB and 8 MB caps only from it.
4. Reboot with no session and repeat node readiness, mount, timer, Restricted
   labels, allocator probes, Bound PVC, empty directory, and §3.
5. Produce the separate website implementation prompt (H15): same-origin
   `EventSource`, `fields.session_id`, bounded retained rows/render rate/reconnect
   backoff, duplicate tolerance, and degradation independent of allocation. Gate
   `deploy/website/vif-log-viewer.html`, the bounded browser reference, there.

## 5. Remaining non-logging fleet gates

| Item | Required before | Completion evidence |
|---|---|---|
| Startup-handshake abuse bounds (H1) | broad public exposure | Silent/half-open first peers time out and an abandoned lobby returns to waiting. |
| Source-address preservation (H11) | per-address admission claims | An operator-only admitted-participant record names the off-box source; it never enters public SSE. |
| Occupied lifecycle matrix (H12) | website launch | Empty expiry, near-deadline rejoin, SIGTERM drain, and capacity gates pass. |
| Ten-session/full-roster sizing (H3) | final resource limits | One-hour measurements justify CPU, memory, tmpfs, rotation, and stream bounds. |
| Automated image delivery (H16) | production release automation | CI reproduces the manual import/update boundary without inbound cluster credentials. |
| Website/nginx integration (H15) | website launch | Create/list and log routes pass (done 2026-09-14); the session page and raw game join remain. |

## 6. Final acceptance and rollback boundary

The logging pivot is complete only when:

- ten concurrent files have distinct names and matching self-tags;
- session pods remain Restricted, tokenless, and free of direct `hostPath`;
- allocator RBAC cannot read `pods/log`;
- tmpfs is hard-capped, K3s fails closed without it, and reboot clears it;
- a full tmpfs causes bounded log loss without ending a game;
- standalone LogWisp has no Kubernetes credential and binds only loopback;
- stopping LogWisp does not stop allocation, state, or gameplay;
- SSE preserves source bytes and remains bounded under slow/reconnecting clients;
- public logs exclude host, K3s, credential, and exact client-address data; and
- §3 passes after reboot.

Keep the previous allocator binary and configuration until this gate passes.
Rollback selects the preceding allocator/workload, restores the 501 handler or
disables its nginx route, and stops LogWisp. It never lowers Pod Security, mounts
direct `hostPath`, deletes a Bound PV/PVC in use, or unmounts tmpfs while K3s is
running.
