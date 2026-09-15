# Kubernetes fleet: remaining implementation plan

This is the authoritative order for unfinished Kubernetes fleet work. Completed
design and operating detail lives in:

- [the deployment procedure](kube_docker_deploy.md);
- [the fleet architecture and verification matrix](kubernetes-fleet.md); and
- [the Linux node artifacts and batch procedures](../deploy/guest/README.md).

Status on 2026-09-15: Batches A-F, H1 and H11 are deployed and passed their live
gates, and the site publishes the two API routes and its own fleet page. What
those gates established is recorded in the fleet plan and the deployment
procedure linked above; this file holds only what is left.

One thing a reader needs before continuing: `deploy/logwisp/REVISION` is
`6046f5c56b583ce3800f69c639874048b3dd8b69`.

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
- `id` is a session's public identifier. `page_url` and `join_target` are opaque
  strings the allocator produces; nothing else may build either from a port,
  because H8 will key both on the identifier instead.

The node procedure must remain runnable from a bare systemd-based Arch Linux or
Ubuntu installation. Distribution branches are allowed only where package or
service defaults differ.

## 2. Remaining batches, in order

Where the pivot ends: a player's browser reads its own session's log lines from
the website over one same-origin route, while nothing in that path can read a
Kubernetes pod log, hold a cluster credential, or end a game by failing.

H15 is done: the two API routes were verified live on 2026-09-14 and the site's
fleet page — a separate repository — followed on 2026-09-15, with the same
properties `deploy/website/vif-log-viewer.html` demonstrates and session controls
built from the `limits` the allocator advertises. What remains follows here in
the order it should be done. Each batch finishes with the common check in §3 as
well as its own gate.

### Batch H1 — handshake abuse bounds

Landed except its fuzz coverage. The tick-zero gate is bounded by one world
install (`parameter.NetworkJoinReadyTimeout`, the same bound a mid-run join
gets) and waits on whoever is still linked rather than on the roster the lobby
closed on, so a silent peer is dropped at that bound and a departing one is
excused. A confirmation is keyed to the link it arrived on, so no peer passes the
gate for another and a released identity installs its own world before the next
gate admits it.

An abandoned lobby continues into the run rather than returning to `waiting`:
`lifecycle` moves forward only, and a roster that emptied is `vacant` by its own
definition. The session therefore starts, parks, and its empty grace — the same
ninety seconds — decides, while a new dial arrives through the tested mid-run
gate. What H1 required is that a stranger cannot end a match or hold a fresh
session; both hold.

Remaining: the handshake fuzz target `kubernetes-fleet.md` asks for — malformed,
oversized, replayed and half-open — which `internal/network` has no equivalent
of.

Gate: an off-box peer that completes the handshake and sends nothing leaves the
session allocatable, and a real client joins it afterwards.

### Batch H11 — source-address preservation

Done, gated live on 2026-09-15. The coordinator emits `peer admitted` under its own
`admit` sub, naming the accepted socket's address and what the peer declared;
LogWisp excludes that sub from the published stream beside its `TRACE` pattern.
One filter is all that separates the two, which is why the gate proves both
halves at once.

What the gate found: an off-box join named the client's own public address, so
`externalTrafficPolicy: Local` plus the `pf rdr` hop does reach the pod with the
player's address and the per-address admission limiter is per player rather than
one budget for the fleet. The same record was absent from the loopback stream
(212 records carried, none) and from the published route (111 carried, none).

### Batch H12 — occupied lifecycle matrix

Written and rehearsed off the fleet, not yet run on it. `test/scenario.sh` proves
the lifetime policy inside one process; these four prove it where a Job, a
Service, a kubelet grace period and an off-box client are also involved:
the empty grace ending a session nobody returned to, a guest returning inside
that grace into the slot its departure released, a termination draining the match
rather than cutting it, and a full session refusing the next dial at the
handshake.

The procedure is
[Batch H12 in the node README](../deploy/guest/README.md#batch-h12-occupied-lifecycle-gates),
which also carries the per-session cost measurement Batch G's sizing needs. Read
it before starting: each gate allocates its own session and needs a development
terminal ready before the `POST` returns.

Gate: all four observed on the node, with the refusal text and the exit reason
quoted from the session's own JSONL.

### Batch G — reconcile and hand off

Leave a deployment a stranger can install from bare Arch or Ubuntu, described
only as deployed, with limits justified by measurement instead of single-guest
history.

1. Reduce `doc/kube_docker_deploy.md`, `doc/kubernetes-fleet.md`,
   `deploy/README.md`, and `deploy/guest/README.md` to the deployed design.
   Replace batch-by-batch narration with measured outcomes, and delete the
   superseded console/sidecar/stdout-public-path prose that remains. Replace the
   real host names left in `tool/vif-allocator/README.md` with placeholders.
2. Rehearse the complete deployment from bare Arch Linux and bare Ubuntu. Record
   package/service differences and fix every command that assumes this node.
3. Run the ten-session/four-player measurement (H3). Record CPU, memory, tmpfs
   usage, log rate, tick slips, rotations, LogWisp drops/replay, and browser
   reconnect behavior; revise the provisional 256 MiB and 8 MB caps only from it.
4. Reboot with no session and repeat node readiness, mount, timer, Restricted
   labels, allocator probes, Bound PVC, empty directory, and §3.

Gate: both rehearsals reach a first session without an undocumented step, and
every resource limit in `deploy/k3s/30-session.yaml` cites a number from step 3.

### Batch H16 — automated image delivery

Reproduce the manual import boundary in CI without giving anything inbound
cluster credentials: CI builds and publishes the session image, the node pulls or
imports it, and `update-vif-image.sh` stays the only thing that points the
allocator at a new tag.

Gate: a tagged commit produces an image the node installs through the existing
updater, with no credential held outside the node.

### Batch H8 — path-routed sessions

Design only until the batches above are done. Today a session is reached by its
own NodePort and the allocator builds `page_url` from that port; the intended end
is one public endpoint where the opaque session identifier in the path selects the
container. Evaluate the allocator proxying game traffic, an ingress, and the
built-and-tested single-port `-name` alternative in `deploy/frontdoor/` against
measurement rather than preference, and record the decision as an ADR — whose
home in `doc/` this batch also has to choose, because none exists yet.

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

## 4. Final acceptance and rollback boundary

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
