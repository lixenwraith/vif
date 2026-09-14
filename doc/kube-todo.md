# Kubernetes fleet: remaining implementation plan

This is the authoritative order for unfinished Kubernetes fleet work. Completed
design and operating detail lives in:

- [the deployment procedure](kube_docker_deploy.md);
- [the fleet architecture and verification matrix](kubernetes-fleet.md); and
- [the Linux node artifacts and batch procedures](../deploy/guest/README.md).

Status on 2026-09-14:

- Batches A-E are deployed and passed their live gates: the commissioned writer,
  the fail-closed 256 MiB tmpfs behind one Bound local PV/PVC and its cleanup
  timer, the Restricted single-container PVC workload, the allocator Role that
  denies `pods/log`, and the standalone loopback LogWisp service. Measured:
  two-session fan-in preserved 606 sampled non-TRACE records byte-for-byte with
  no drops; a stop/restart replayed an exact pre-outage sentinel while processing
  1,841 records with 86 bounded client-queue drops and no authorization or
  connection rejections.
- The pinned LogWisp revision update passed live on 2026-09-14 through the
  guarded empty-fleet trap. `deploy/logwisp/REVISION` is
  `6046f5c56b583ce3800f69c639874048b3dd8b69`; the installed binary reports that
  commit, `/status` carries `client_buffer_size`, `max_connections`, and
  `write_timeout_ms`, and allocator health and readiness returned `ok`. Its
  watcher-retirement fix is running but not yet proven: proof needs a session
  create/delete cycle, which Batch F's gate supplies.
- Batch F's live cutover ran on 2026-09-14. F1-F3 passed; F4 confirmed `405`
  method handling and an unstalled stream; F5 reached `occupied` and proved the
  outage slice — stable `503 log_stream_unavailable`, `ok` health and readiness,
  the occupied game still advancing, and a new session still created while
  LogWisp was stopped; F6 and F7 returned the node to an empty, five-unit steady
  state. F5.4's byte-exact sentinel and F5.6's retained replay through the proxy
  printed no verdict under the procedure of the day and are the only outstanding
  Batch F evidence. The rollback path was then exercised, so the node runs the
  previous allocator until `./deploy/guest/update-vif-allocator.sh` runs again.
  Batch G has not started.

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
- Finish every batch with the common single-session check in §4 plus its
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
| F — allocator byte proxy | Make `/vif/api/logs` the same-origin edge for LogWisp's SSE bytes, so the website needs no second host, port, or credential, and neither service can take the other down. | Cut over 2026-09-14; two byte-preservation proofs outstanding. |
| G — final reconciliation | Leave a deployment a stranger can install from bare Arch or Ubuntu, described only as deployed, with limits justified by measurement instead of single-guest history. | Blocked on F. |

§6 holds the non-logging gates. H15 is what consumes F's route and unblocks with
it; H3's sizing measurement runs inside G; H1, H11 and H12 bound what a stranger
can do to an open game port, and gate public exposure rather than this pivot.

## 3. Batch F — cut the allocator over to the byte proxy

The validated `-log-stream-url` option, the `httputil.ReverseProxy` handler, the
SSE lifetime separated from finite create/API deadlines, and their tests are
merged. What remains is the live cutover. Each slice below is one reportable
step, labelled as in `deploy/guest/README.md` "Batch F: deploy the allocator byte
proxy", which holds the exact commands:

1. **F1** preflight: five units active, PV/PVC Bound, fleet and tmpfs empty,
   LogWisp `/status` carrying the three bounds, and the deployed allocator still
   answering `501 log_stream_not_configured`.
2. **F2** `./deploy/guest/update-vif-allocator.sh`. It builds first, refuses a
   non-empty fleet, pauses only allocation for the replacement, and restores its
   own previous set if health or readiness fails.
3. **F3** independence: `vif-allocator.service` may want or order after LogWisp
   and must never `Require=` or execute it; health and readiness return `ok`.
4. **F4** proxy shape: `GET`/`HEAD` only with `405 method_not_allowed` otherwise,
   upstream `text/event-stream`, `no-cache` and `x-accel-buffering: no` preserved,
   the first `event: connected` frame flushed after a `HEAD` on the same route,
   and one counted sink client.
5. **F5** live session gate, in seven sub-steps. It needs a second machine: its
   proofs all require an `occupied` session and the first-join window is 90
   seconds. Reader started, session created and joined, occupancy confirmed, a
   byte-exact non-TRACE sentinel through the proxy; then with LogWisp stopped,
   stable `503 log_stream_unavailable` while create, list, health, readiness and
   the occupied game continue; then an exact retained sentinel after restart;
   then vacancy, deletion and cleanup. F5 is Batch F's §4 run, interleaved with
   the outage, so §4 is not run separately for this batch. A missed join
   invalidates the step: delete the session and restart it.
6. **F6** watcher retirement: the LogWisp invocation that was running for F5's
   session deletion carries no `Watcher failed` entry. An invocation that saw no
   file removed proves nothing, and earlier ones ran the replaced binary.
7. **F7** remove every verification session, file, and capture; keep the previous
   allocator set until Batch G completes.

The node has no display, so `deploy/guest/vif-log-viewer.html` is not gated here;
`curl` carries the byte proof and the viewer is verified with nginx in H15.

Rollback: restore the previous allocator set, whose endpoint returns 501, or
disable the nginx log route. Keep LogWisp and file-writing games independently
operable.

## 4. Common end-of-batch session check

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
    .sessions[] | select(.id == $id) | .state |
    select(.phase == "occupied" and .guests >= 1)'
```

Quit the remote client and immediately verify vacancy and the commissioned file:

```sh
curl -fsS http://127.0.0.1:9080/vif/api/sessions |
  jq -e --arg id "$SESSION_ID" '
    .sessions[] | select(.id == $id) | .state |
    select(.phase == "vacant" and .guests == 0)'

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

## 5. Batch G — reconcile and hand off

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
   labels, allocator probes, Bound PVC, empty directory, and §4.
5. Produce the separate website implementation prompt (H15): same-origin
   `EventSource`, `fields.session_id`, bounded retained rows/render rate/reconnect
   backoff, duplicate tolerance, and degradation independent of allocation. Gate
   `deploy/guest/vif-log-viewer.html`, the bounded browser reference, there.

## 6. Remaining non-logging fleet gates

| Item | Required before | Completion evidence |
|---|---|---|
| Startup-handshake abuse bounds (H1) | broad public exposure | Silent/half-open first peers time out and an abandoned lobby returns to waiting. |
| Source-address preservation (H11) | per-address admission claims | An operator-only admitted-participant record names the off-box source; it never enters public SSE. |
| Occupied lifecycle matrix (H12) | website launch | Empty expiry, near-deadline rejoin, SIGTERM drain, and capacity gates pass. |
| Ten-session/full-roster sizing (H3) | final resource limits | One-hour measurements justify CPU, memory, tmpfs, rotation, and stream bounds. |
| Automated image delivery (H16) | production release automation | CI reproduces the manual import/update boundary without inbound cluster credentials. |
| Website/nginx integration (H15) | after Batch F | Same-origin create/list/log routes, session page, and raw game join all pass. |

## 7. Final acceptance and rollback boundary

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
- §4 passes after reboot.

Keep the previous allocator binary and configuration until this gate passes.
Rollback selects the preceding allocator/workload, restores the 501 handler or
disables its nginx route, and stops LogWisp. It never lowers Pod Security, mounts
direct `hostPath`, deletes a Bound PV/PVC in use, or unmounts tmpfs while K3s is
running.
