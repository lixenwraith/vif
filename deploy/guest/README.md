# Linux node deployment artifacts

These files target a systemd K3s node. The production path is an Arch Linux
guest; the same service and mount files also apply to a bare Ubuntu node. Follow
the host/network boundary in `doc/kube_docker_deploy.md`, and use
`doc/kube-todo.md` as the authoritative batch order.

## Distribution prerequisites

Install the command-line dependencies before the main deployment procedure:

```sh
. /etc/os-release
case "$ID" in
  arch)
    sudo pacman -Syu --needed \
      curl git go jq make python util-linux iptables-nft conntrack-tools \
      ethtool tcpdump nftables docker
    ;;
  ubuntu)
    sudo apt-get update
    sudo apt-get install -y \
      ca-certificates curl git golang-go jq make python3 util-linux iptables \
      conntrack ethtool tcpdump nftables docker.io
    ;;
  *)
    printf 'unsupported distribution: %s\n' "$ID" >&2
    false
    ;;
esac
```

Ubuntu's `python3` and Arch's `python` package both install
`/usr/bin/python3`. Docker builds images only; after each import, the deployment
procedure disables Docker, its socket, and its system containerd again.

## Batch B: volatile fleet-log storage

Run this section from the repository root at the exact revision being deployed.
For a validation revision that has not reached `main`, fetch and switch to that
revision before opening the maintenance window. Prove every Batch B source
artifact is present before any privileged copy or service operation:

```sh
test "$(pwd -P)" = "$(git rev-parse --show-toplevel)"
for artifact in \
  'deploy/guest/var-log-vif\x2dfleet.mount' \
  deploy/guest/k3s.service.d/10-vif-fleet-logs.conf \
  deploy/guest/vif-fleet.sysusers \
  deploy/guest/vif-fleet-log-cleanup.py \
  deploy/guest/vif-fleet-log-cleanup.service \
  deploy/guest/vif-fleet-log-cleanup.timer \
  deploy/k3s/05-log-volume.yaml \
  deploy/k3s/06-log-volume-check.yaml
do
  test -r "$artifact" || {
    printf 'missing Batch B artifact: %s\n' "$artifact" >&2
    false
  }
done
```

This is a maintenance operation. Announce it, keep both allocators and operators
from creating sessions, and verify the fleet-object query is empty before
continuing. K3s restarts once after the mount dependency is installed.

On an upgrade, stop the allocator before the final empty-fleet check. A fresh node
does not have that unit yet and skips only the stop. Then capture the single K3s
node name without placing it in the repository. If any later step stops before an
existing allocator is restored, keep it stopped while diagnosing the node:

```sh
if systemctl cat vif-allocator.service >/dev/null 2>&1; then
  sudo systemctl stop vif-allocator.service
  test "$(systemctl is-active vif-allocator.service)" = inactive
fi
NODE_NAME=$(sudo kubectl get nodes \
  -o jsonpath='{.items[0].metadata.name}')
test -n "$NODE_NAME"
sudo kubectl get node "$NODE_NAME" -o name
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
```

The final command must report no resources. Install the mount, K3s drop-in, and
cleanup units without starting them yet:

```sh
MOUNT_UNIT='var-log-vif\x2dfleet.mount'
sudo install -D -m 0644 \
  deploy/guest/vif-fleet.sysusers \
  /etc/sysusers.d/vif-fleet.conf
sudo systemd-sysusers /etc/sysusers.d/vif-fleet.conf
test "$(id -u vif-fleet)" = 65532
test "$(id -g vif-fleet)" = 65532
sudo install -D -m 0644 \
  "deploy/guest/$MOUNT_UNIT" "/etc/systemd/system/$MOUNT_UNIT"
sudo install -D -m 0644 \
  deploy/guest/k3s.service.d/10-vif-fleet-logs.conf \
  /etc/systemd/system/k3s.service.d/10-vif-fleet-logs.conf
sudo install -D -m 0644 \
  deploy/guest/vif-fleet-log-cleanup.py \
  /usr/local/libexec/vif-fleet-log-cleanup.py
sudo install -D -m 0644 \
  deploy/guest/vif-fleet-log-cleanup.service \
  /etc/systemd/system/vif-fleet-log-cleanup.service
sudo install -D -m 0644 \
  deploy/guest/vif-fleet-log-cleanup.timer \
  /etc/systemd/system/vif-fleet-log-cleanup.timer
sudo systemctl daemon-reload
sudo systemctl enable "$MOUNT_UNIT" vif-fleet-log-cleanup.timer
```

The time-sensitive step starts here. The restart transaction pulls in the mount;
if mounting fails, the K3s dependency must prevent K3s from starting:

```sh
sudo systemctl restart k3s.service

for attempt in $(seq 1 60); do
  sudo kubectl get node "$NODE_NAME" >/dev/null 2>&1 && break
  sleep 2
done
sudo kubectl wait --for=condition=Ready \
  "node/$NODE_NAME" --timeout=120s
if systemctl cat vif-allocator-token.service >/dev/null 2>&1; then
  sudo systemctl restart vif-allocator-token.service
fi
if systemctl cat vif-allocator.service >/dev/null 2>&1; then
  sudo systemctl start vif-allocator.service
fi
sudo systemctl start vif-fleet-log-cleanup.timer
```

Verify the mount and dependency before creating Kubernetes storage objects:

```sh
systemctl is-active "$MOUNT_UNIT" k3s.service vif-fleet-log-cleanup.timer
if systemctl cat vif-allocator.service >/dev/null 2>&1; then
  systemctl is-active vif-allocator.service
fi
systemctl is-enabled "$MOUNT_UNIT" vif-fleet-log-cleanup.timer
systemctl cat k3s.service
systemctl show k3s.service -p Requires -p After
getent passwd vif-fleet
getent group vif-fleet
findmnt -no TARGET,FSTYPE,SIZE,OPTIONS /var/log/vif-fleet
sudo stat -c 'mode=%a uid=%u gid=%g path=%n' /var/log/vif-fleet
```

Expected: `tmpfs`, approximately `256M`, `nodev,nosuid,noexec`, mode `770`, and
numeric owner/group `65532`. The `vif-fleet` host identity must resolve to the
same UID/GID; the containers continue to use only the numeric identity. Apply the
quota first, then render only the node-name placeholder in the storage template:

```sh
sudo kubectl apply -f deploy/k3s/10-quota.yaml
sed "s|\${NODE_NAME}|$NODE_NAME|g" deploy/k3s/05-log-volume.yaml \
  | sudo kubectl apply -f -
sudo kubectl get persistentvolume vif-fleet-logs
sudo kubectl -n vif get persistentvolumeclaim vif-fleet-logs
```

`WaitForFirstConsumer` deliberately leaves the claim `Pending` until the test pod
exists. Render its image from the allocator's current imported image, apply it,
and wait for the five-second unclaimed server to finish:

```sh
if test -r /etc/vif-allocator/allocator.env; then
  VIF_IMAGE=$(sudo sed -n \
    's/^VIF_ALLOCATOR_IMAGE=//p' /etc/vif-allocator/allocator.env)
else
  VIF_IMAGE="docker.io/library/vi-fighter:$(git rev-parse --short=8 HEAD)"
fi
test -n "$VIF_IMAGE"
sudo k3s ctr images ls | grep -F "$VIF_IMAGE"
sed "s|\${IMAGE}|$VIF_IMAGE|g" deploy/k3s/06-log-volume-check.yaml \
  | sudo kubectl apply -f -
sudo kubectl -n vif wait \
  --for=jsonpath='{.status.phase}'=Succeeded \
  pod/vif-log-volume-check --timeout=90s
sudo kubectl get persistentvolume vif-fleet-logs
sudo kubectl -n vif get persistentvolumeclaim vif-fleet-logs
sudo test -s /var/log/vif-fleet/volume-check.jsonl
sudo jq -s -e 'map(select(.sub != null)) as $records |
    ($records | length > 0) and
    all($records[]; .fields.session_id == "volume-check")' \
  /var/log/vif-fleet/volume-check.jsonl
```

Both volume objects must now be `Bound`. Prove Restricted admission still rejects
a direct `hostPath` without creating a temporary object:

```sh
if HOSTPATH_RESULT=$(sudo kubectl create --dry-run=server -f - 2>&1 <<'YAML'
apiVersion: v1
kind: Pod
metadata:
  name: vif-hostpath-must-fail
  namespace: vif
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: check
      image: registry.k8s.io/pause:3.10
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        runAsNonRoot: true
        capabilities: {drop: ["ALL"]}
      volumeMounts:
        - {name: forbidden, mountPath: /forbidden, readOnly: true}
  volumes:
    - name: forbidden
      hostPath: {path: /var/log/vif-fleet, type: Directory}
YAML
); then
  printf 'FAIL: Restricted admission accepted hostPath\n' >&2
  false
else
  printf '%s\n' "$HOSTPATH_RESULT"
  printf '%s\n' "$HOSTPATH_RESULT" | grep -E 'restricted|hostPath'
fi
```

Delete every temporary Batch B resource; the PV/PVC, mount, and timer remain:

```sh
sudo kubectl -n vif delete pod vif-log-volume-check --wait=true
sudo find /var/log/vif-fleet -maxdepth 1 -type f \
  \( -name 'volume-check.jsonl' -o -name 'volume-check_*.jsonl' \) \
  -delete
sudo systemctl start vif-fleet-log-cleanup.service
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo find /var/log/vif-fleet -mindepth 1 -maxdepth 1 -print
```

Both final commands must print nothing. On an existing pre-Batch-C deployment,
finish Batch B with the current common session check in `doc/kube-todo.md`; its
workload still writes stdout until Batch C. On a fresh node, proceed to the manual
session and allocator installation in `doc/kube_docker_deploy.md`; the current
renderer and allocator already require this Bound PVC, and their first common
session check validates file output.

## Batch B rollback

Do not unmount while any pod uses the claim. Stop here and inspect consumers if
this query prints anything:

```sh
sudo kubectl get pods --all-namespaces \
  -o json | jq -e \
  '.items[] | select(any(.spec.volumes[]?;
    .persistentVolumeClaim.claimName == "vif-fleet-logs"))'
```

With no consumers, stop K3s, disable cleanup, remove only the new dependency,
and make the uncovered root-filesystem directory unwritable before restarting:

```sh
MOUNT_UNIT='var-log-vif\x2dfleet.mount'
sudo systemctl stop k3s.service
sudo systemctl disable --now vif-fleet-log-cleanup.timer
sudo systemctl stop "$MOUNT_UNIT"
sudo chmod 000 /var/log/vif-fleet
sudo rm /etc/systemd/system/k3s.service.d/10-vif-fleet-logs.conf
sudo systemctl daemon-reload
sudo systemctl restart k3s.service
if systemctl cat vif-allocator-token.service >/dev/null 2>&1; then
  sudo systemctl restart vif-allocator-token.service
fi
if systemctl cat vif-allocator.service >/dev/null 2>&1; then
  sudo systemctl start vif-allocator.service
fi
```

Keep the Retain PV/PVC for diagnosis. Do not schedule a PVC writer after rollback;
either restore the mount/drop-in or deliberately remove the unused claim and PV
before retrying Batch B.

## Batch C: switch session Jobs to file logging

Batch C is an upgrade procedure for a live Batch B node and changes only newly
allocated sessions. A fresh node using the current repository already installs the
file-writing renderer and allocator after Batch B and skips this upgrade section.
For an existing node, the already imported game image is unchanged. Start with the
Batch B gate intact and an empty fleet:

```sh
systemctl is-active \
  'var-log-vif\x2dfleet.mount' vif-fleet-log-cleanup.timer \
  k3s.service vif-allocator.service
sudo kubectl get persistentvolume vif-fleet-logs
sudo kubectl -n vif get persistentvolumeclaim vif-fleet-logs
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
```

The four units must be active, both volume objects must be `Bound`, and the final
query must be empty. Build the allocator before opening the short maintenance
window:

```sh
make allocator
test -x bin/vif-allocator
```

Stop allocation, repeat the empty-fleet check, preserve the Batch B allocator for
rollback, and install the new binary. The backup path must not already exist:

```sh
sudo systemctl stop vif-allocator.service
test "$(systemctl is-active vif-allocator.service)" = inactive
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
test ! -e /usr/local/libexec/vif-allocator.batch-b
sudo install -o root -g root -m 0755 \
  /usr/local/bin/vif-allocator \
  /usr/local/libexec/vif-allocator.batch-b
sudo install -o root -g root -m 0755 \
  bin/vif-allocator /usr/local/bin/vif-allocator
sudo systemctl start vif-allocator.service
for attempt in $(seq 1 25); do
  curl --connect-timeout 1 --max-time 2 -fsS \
    http://127.0.0.1:9080/healthz >/dev/null 2>&1 && break
  sleep 1
done
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz
```

Finish with the common session check in `doc/kube-todo.md`. The session Job must
have exactly one `session` container, mount the `vif-fleet-logs` claim only there,
omit direct `hostPath` and `-log-stdout`, and produce `<session-id>.jsonl` whose
application records all carry the same `fields.session_id`. Delete the Job and its
files after the check.

### Batch C rollback

Rollback affects newly allocated sessions, so first stop the allocator and prove
the fleet is empty. Restore the preserved Batch B binary; leave the mounted volume
and its Bound PV/PVC in place:

```sh
FLEET_OBJECTS=$(sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet -o name)
test -z "$FLEET_OBJECTS"
sudo systemctl stop vif-allocator.service
sudo test -x /usr/local/libexec/vif-allocator.batch-b
sudo install -o root -g root -m 0755 \
  /usr/local/libexec/vif-allocator.batch-b \
  /usr/local/bin/vif-allocator
sudo systemctl start vif-allocator.service
for attempt in $(seq 1 25); do
  curl --connect-timeout 1 --max-time 2 -fsS \
    http://127.0.0.1:9080/healthz >/dev/null 2>&1 && break
  sleep 1
done
systemctl is-active vif-allocator.service
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz
unset FLEET_OBJECTS
```

## Batch D: remove the unused pod-log permission

Batch C must have passed its allocator-created file, remote-join, state, and
cleanup gates before this change. Batch D changes only the allocator Role; it does
not restart K3s, rebuild an image, alter the session workload, or deploy LogWisp.
Start with the file-writing allocator healthy, the volume Bound, and the fleet
empty:

```sh
systemctl is-active \
  'var-log-vif\x2dfleet.mount' vif-fleet-log-cleanup.timer \
  k3s.service vif-allocator.service
sudo kubectl get persistentvolume vif-fleet-logs
sudo kubectl -n vif get persistentvolumeclaim vif-fleet-logs
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
```

Announce a short allocation pause. Stop the allocator before the final empty-fleet
check so no request can race the Role update, then apply only the checked-in Role
and RoleBinding:

```sh
sudo systemctl stop vif-allocator.service
test "$(systemctl is-active vif-allocator.service)" = inactive
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo kubectl apply -f deploy/k3s/40-allocator-rbac.yaml
```

The fleet query must be empty. Prove the removed subresource is denied and every
allocator operation needed for allocation and readiness remains allowed:

```sh
test "$(sudo kubectl auth can-i get pods --subresource=log \
  --as=system:serviceaccount:vif:vif-allocator -n vif)" = no
test "$(sudo kubectl auth can-i create jobs.batch \
  --as=system:serviceaccount:vif:vif-allocator -n vif)" = yes
test "$(sudo kubectl auth can-i delete jobs.batch \
  --as=system:serviceaccount:vif:vif-allocator -n vif)" = yes
test "$(sudo kubectl auth can-i create services \
  --as=system:serviceaccount:vif:vif-allocator -n vif)" = yes
test "$(sudo kubectl auth can-i list services \
  --as=system:serviceaccount:vif:vif-allocator -n vif)" = yes
test "$(sudo kubectl auth can-i get pods \
  --as=system:serviceaccount:vif:vif-allocator -n vif)" = yes
test "$(sudo kubectl auth can-i list endpointslices.discovery.k8s.io \
  --as=system:serviceaccount:vif:vif-allocator -n vif)" = yes
```

Use the explicit `--subresource=log` form. Positional `pods/log` can be parsed as
`TYPE/NAME` and return `yes` because this Role intentionally retains permission to
read Pods; that does not test the log subresource.

Restart the allocator and wait for both probes:

```sh
sudo systemctl start vif-allocator.service
for attempt in $(seq 1 25); do
  curl --connect-timeout 1 --max-time 2 -fsS \
    http://127.0.0.1:9080/healthz >/dev/null 2>&1 && break
  sleep 1
done
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz
```

Finish with the common session check in `doc/kube-todo.md`. It must prove that
create/list/readiness, off-box join, occupied/vacant state, the self-tagged node
file, and cleanup still work without reading a pod log.

### Batch D rollback

If an allocator control operation fails specifically because the removed
subresource is required, stop allocation, prove the fleet is empty, restore only
that exact read grant, and restart the allocator:

```sh
FLEET_OBJECTS=$(sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet -o name)
test -z "$FLEET_OBJECTS"
sudo systemctl stop vif-allocator.service
sudo kubectl -n vif patch role vif-allocator --type=json \
  -p='[{"op":"add","path":"/rules/-","value":{"apiGroups":[""],"resources":["pods/log"],"verbs":["get"]}}]'
sudo systemctl start vif-allocator.service
for attempt in $(seq 1 25); do
  curl --connect-timeout 1 --max-time 2 -fsS \
    http://127.0.0.1:9080/healthz >/dev/null 2>&1 && break
  sleep 1
done
systemctl is-active vif-allocator.service
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz
unset FLEET_OBJECTS
```

Do not restore `pods/log` for a node-file, LogWisp, or public-stream failure: none
of those components is allowed to use the Kubernetes log API. A rollback leaves
the live Role different from the repository and must be diagnosed before Batch E.

## Batch E: install the standalone LogWisp service

Batch D must have passed before this section. The initial Batch E slice installs
and inspects the independent reader only; it does not change or restart the
allocator, K3s, a session workload, or the PVC. Its isolation and loopback checks
must pass before the later simultaneous-session and outage/replay gates.

Start from an empty fleet and empty log directory. The mount, K3s, allocator, and
cleanup timer must stay active, the volumes must stay `Bound`, and the Role must
still deny the explicit Pod log subresource:

```sh
systemctl is-active \
  'var-log-vif\x2dfleet.mount' \
  vif-fleet-log-cleanup.timer \
  k3s.service \
  vif-allocator.service

sudo kubectl get persistentvolume vif-fleet-logs
sudo kubectl -n vif get persistentvolumeclaim vif-fleet-logs
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo find /var/log/vif-fleet \
  -mindepth 1 -maxdepth 1 -print

test "$(sudo kubectl auth can-i get pods --subresource=log \
  --as=system:serviceaccount:vif:vif-allocator -n vif)" = no
```

The fleet query and `find` must print nothing. The installer resolves the
vi-fighter root from its own path, stops on the first error, and verifies the
40-character upstream revision before making host changes. The shared builder
additionally fetches upstream `main` and its tags and requires the pin to be an
ancestor of that head, so it fails before starting Docker when
`deploy/logwisp/REVISION` names an unreachable commit. A previous LogWisp
installation is not an in-place Batch E upgrade; stop and inspect it if any of
the three destination paths already exists:

```sh
for artifact in \
  deploy/logwisp/REVISION \
  deploy/logwisp/aggregator.toml \
  deploy/guest/logwisp.sysusers \
  deploy/guest/logwisp.service \
  deploy/guest/build-logwisp.sh \
  deploy/guest/install-logwisp.sh
do
  test -r "$artifact" || {
    printf 'missing Batch E artifact: %s\n' "$artifact" >&2
    false
  }
done
test -x deploy/guest/install-logwisp.sh
test -x deploy/guest/build-logwisp.sh

LOGWISP_REVISION=$(tr -d '[:space:]' \
  < deploy/logwisp/REVISION)
printf 'LogWisp revision: %s\n' "$LOGWISP_REVISION"
printf '%s\n' "$LOGWISP_REVISION" |
  grep -Eq '^[0-9a-f]{40}$'

test ! -e /usr/local/bin/logwisp
test ! -e /etc/logwisp/vif-fleet.toml
test ! -e /etc/systemd/system/logwisp.service
getent group vif-fleet
```

The build is intentionally identical on bare Arch Linux and Ubuntu: it uses the
pinned builder in LogWisp's own Dockerfile rather than the host's Go minor
release. Docker is temporary. The installer removes its container, image, and
temporary worktree, stops and disables Docker plus the distribution containerd,
and restores `FORWARD ACCEPT` before returning.

If a prior manual attempt failed, first restore that build-daemon and forwarding
baseline and prove no destination was partially installed:

```sh
sudo systemctl disable \
  docker.service docker.socket containerd.service
sudo systemctl stop docker.socket
sudo systemctl stop docker.service containerd.service

test "$(systemctl is-active docker.service)" = inactive
test "$(systemctl is-active docker.socket)" = inactive
test "$(systemctl is-active containerd.service)" = inactive

if test "$(sudo iptables -S FORWARD | sed -n '1p')" != \
  '-P FORWARD ACCEPT'
then
  sudo iptables -P FORWARD ACCEPT
fi

test ! -e /usr/local/bin/logwisp
test ! -e /etc/logwisp/vif-fleet.toml
test ! -e /etc/systemd/system/logwisp.service
```

The portable default needs only the vi-fighter checkout. It clones LogWisp into
the temporary build directory and removes that source before returning:

```sh
./deploy/guest/install-logwisp.sh
```

An existing LogWisp checkout is an optional download optimization, never a
required working directory. If supplied, the helper creates a temporary detached
worktree at the pinned revision and does not switch, pull, clean, or modify its
current branch. Use a placeholder rather than a site path in shared instructions:

```sh
./deploy/guest/install-logwisp.sh '<existing-logwisp-checkout>'
```

Both forms install the binary, dedicated locked identity, root-owned
configuration, and unit, then start the service. The service's supplementary
`vif-fleet` group is used only to read the tmpfs; its unit gives it a read-only
mount view and hides K3s and allocator credential paths.

No simultaneous or deadline-sensitive client action occurs in this first slice.
Inspect the service, its isolated read-only view, and the loopback endpoint. Do
not proceed if the listener is wildcard/public, if the service is not in group
65532, or if either installed text file differs from the repository:

```sh
systemctl is-active logwisp.service
systemctl is-enabled logwisp.service
/usr/local/bin/logwisp --version

systemctl show logwisp.service \
  -p User -p Group -p SupplementaryGroups \
  -p Requires -p Wants -p After \
  -p BindReadOnlyPaths -p InaccessiblePaths \
  -p IPAddressDeny -p IPAddressAllow \
  -p Result -p ExecMainStatus

LOGWISP_PID=$(systemctl show logwisp.service \
  -p MainPID --value)
test "$LOGWISP_PID" -gt 1
sudo grep '^Groups:' "/proc/$LOGWISP_PID/status" |
  grep -Eq '(^|[[:space:]])65532([[:space:]]|$)'

findmnt -no TARGET,FSTYPE,OPTIONS /var/log/vif-fleet
sudo nsenter -t "$LOGWISP_PID" -m -- \
  findmnt -no TARGET,FSTYPE,OPTIONS /var/log/vif-fleet

sudo cmp -s deploy/logwisp/aggregator.toml \
  /etc/logwisp/vif-fleet.toml
sudo cmp -s deploy/guest/logwisp.service \
  /etc/systemd/system/logwisp.service

sudo ss -ltnp 'sport = :8081'
test "$(sudo ss -ltnH 'sport = :8081' |
  awk 'NR == 1 {print $4}')" = 127.0.0.1:8081
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status | jq -e .

sudo journalctl -u logwisp.service -n 20 --no-pager
unset LOGWISP_PID LOGWISP_REVISION
```

The host `findmnt` output remains `rw`. Inside LogWisp's mount namespace,
`findmnt` may show the underlying `rw` tmpfs followed by the read-only bind at the
same target; the final/effective entry must contain `ro`. `RequiresMountsFor=` may
add a mount requirement, but `Requires`, `Wants`, and `After` must not name
`k3s.service` or `vif-allocator.service`. The pinned Dockerfile does not set a
build timestamp, so `built: unknown` is expected; its full embedded commit must
match `deploy/logwisp/REVISION`.

### Updating LogWisp independently

After the first installation, use the update helper whenever
`deploy/logwisp/REVISION`, the pipeline configuration, or the hardened unit
changes. It runs from any directory and does not inspect, stop, or modify K3s,
the allocator, or the session workload, so it can also maintain a standalone
LogWisp installation. On a fleet node, the plan still requires allocation to be
stopped and the fleet to be empty before changing a logging service. Announce
the allocation pause and LogWisp stream interruption before running this gate.

`deploy/logwisp/REVISION` must name a commit reachable from upstream LogWisp
`main`, never a pull-request head, which a squash merge discards. The builder
fetches `main` and tags on both the default clone and the optional worktree and
requires the pin to be an ancestor of that head, failing with a repin diagnostic
before Docker starts. That check is the authority; do not pre-resolve by hand:

```sh
for artifact in \
  deploy/logwisp/REVISION \
  deploy/logwisp/aggregator.toml \
  deploy/guest/logwisp.service \
  deploy/guest/build-logwisp.sh \
  deploy/guest/update-logwisp.sh
do
  test -r "$artifact"
done
test -x deploy/guest/build-logwisp.sh
test -x deploy/guest/update-logwisp.sh

if (
  set -eu
  restart_allocator() {
    sudo systemctl start vif-allocator.service
  }
  trap restart_allocator EXIT
  trap 'exit 1' HUP INT TERM

  sudo systemctl stop vif-allocator.service
  test "$(systemctl is-active vif-allocator.service)" = inactive
  FLEET_OBJECTS=$(sudo kubectl -n vif get job,pod,service \
    -l app.kubernetes.io/part-of=vi-fighter-fleet -o name)
  test -z "$FLEET_OBJECTS"
  test -z "$(sudo find /var/log/vif-fleet \
    -mindepth 1 -maxdepth 1 -print -quit)"

  ./deploy/guest/update-logwisp.sh
)
then
  for attempt in $(seq 1 25); do
    curl --connect-timeout 1 --max-time 2 -fsS \
      http://127.0.0.1:9080/healthz >/dev/null 2>&1 && break
    sleep 1
  done
  curl --connect-timeout 2 --max-time 5 -fsS \
    http://127.0.0.1:9080/healthz
  curl --connect-timeout 2 --max-time 5 -fsS \
    http://127.0.0.1:9080/readyz
else
  printf 'LogWisp update gate failed; allocator was restarted\n' >&2
  false
fi
```

The guarded subshell aborts before the updater when either fleet objects or files
remain, and its trap restarts the allocator on success or failure. The Docker
build finishes before the short LogWisp-only restart; no Kubernetes object,
workload image, or allocator binary is changed. If the updater itself fails, it
restores the previous LogWisp set before the allocator restart.

A build that stops on the revision check leaves the running LogWisp, its binary,
and every Kubernetes and tmpfs object untouched; only the allocator is restarted
by the trap. The still-installed previous binary then fails the pin and status
checks below — repin and rerun rather than reading that as a failure of the new
revision.

An existing upstream checkout is optional and is never switched or modified:

```sh
./deploy/guest/update-logwisp.sh '<existing-logwisp-checkout>'
```

The helper retains the previous binary, configuration, and unit at the printed
`.previous` paths, restores them automatically if service verification fails,
cleans its temporary worktree/container/image, and restores the disabled build
daemons and `FORWARD ACCEPT`. Verify the initialized service and newly exposed
queue bounds:

```sh
LOGWISP_REVISION=$(tr -d '[:space:]' < deploy/logwisp/REVISION)
/usr/local/bin/logwisp --version | grep -F "$LOGWISP_REVISION"
systemctl is-active logwisp.service
systemctl is-active vif-allocator.service
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status | jq -e '
    .server.client_buffer_size == 512 and
    .server.max_connections == 32 and
    .server.write_timeout_ms == 5000 and
    .statistics.auth_rejected == 0 and
    .statistics.rejected_clients == 0'

LOGWISP_INVOCATION=$(systemctl show logwisp.service \
  -p InvocationID --value)
test -n "$LOGWISP_INVOCATION"
sudo journalctl "_SYSTEMD_INVOCATION_ID=$LOGWISP_INVOCATION" \
  --no-pager
unset LOGWISP_REVISION LOGWISP_INVOCATION
```

Judge the journal only by the invocation currently running the updated binary,
here and in every later gate. Earlier `Watcher failed` entries with
`error "watcher stopped"` came from the replaced binary retiring a watched file
on normal fleet cleanup; they are historical and say nothing about the installed
revision. Retirement is proven by a later session create/delete cycle, not by
this window, which precedes any session.

This gate passed on 2026-09-14. A first attempt failed inside the fresh clone
because the pin named LogWisp PR #5's squash-discarded head; the trap restarted
the allocator and nothing else changed. After repinning to the merged commit the
build resolved, the binary was replaced during a LogWisp-only restart, the
installed `--version` reported
`6046f5c56b583ce3800f69c639874048b3dd8b69`, `/status` carried the three bounds,
and allocator health and readiness returned `ok`.

### Batch E two-session fan-in gate

This is the first simultaneous Batch E step. Prepare two development-machine
terminals with the matching `bin/vif` before allocating. The first session's
90-second join clock starts before the second `POST`, so do not begin until both
clients are ready. The node terminal must remain open until its variables and
temporary captures are cleaned.

Start with the standalone service healthy, the fleet and directory empty, and no
stream client connected:

```sh
unset SESSION_ONE_JSON SESSION_ONE_ID SESSION_ONE_JOIN
unset SESSION_TWO_JSON SESSION_TWO_ID SESSION_TWO_JOIN
unset BATCH_E_CAPTURE BATCH_E_STREAM_PID
unset BATCH_E_ONE_SNAPSHOT BATCH_E_TWO_SNAPSHOT

systemctl is-active \
  'var-log-vif\x2dfleet.mount' \
  vif-fleet-log-cleanup.timer \
  k3s.service \
  vif-allocator.service \
  logwisp.service

sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo find /var/log/vif-fleet \
  -mindepth 1 -maxdepth 1 -print

curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status |
  jq -e '
    .service == "LogWisp" and
    .version == "v0.18.0" and
    .server.host == "127.0.0.1" and
    .server.port == 8081 and
    .server.auth == "none" and
    .server.tls == false and
    .server.buffer_size == 4096 and
    .server.active_clients == 0 and
    .statistics.auth_rejected == 0 and
    .statistics.dropped_writes == 0 and
    .statistics.rejected_clients == 0'
```

The fleet query and `find` must print nothing. Connect a temporary loopback SSE
reader before creating either file, then prove LogWisp counted that client:

```sh
BATCH_E_CAPTURE=$(mktemp \
  /tmp/vif-logwisp-baseline.XXXXXX.sse)
curl --no-buffer --fail --silent --show-error \
  http://127.0.0.1:8081/stream \
  >"$BATCH_E_CAPTURE" &
BATCH_E_STREAM_PID=$!

for attempt in $(seq 1 50); do
  curl --connect-timeout 1 --max-time 2 -fsS \
    http://127.0.0.1:8081/status |
    jq -e '.server.active_clients == 1' \
      >/dev/null 2>&1 && break
  sleep 0.1
done

test -s "$BATCH_E_CAPTURE"
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status |
  jq -e '.server.active_clients == 1'
```

Allocate the two sessions sequentially. Stop if any assignment fails or either
ID/target is empty:

```sh
SESSION_ONE_JSON=$(curl -fsS -X POST \
  -H 'Content-Type: application/json' -d '{}' \
  http://127.0.0.1:9080/vif/api/sessions) &&
SESSION_ONE_ID=$(printf '%s' "$SESSION_ONE_JSON" |
  jq -er '.id | strings | select(length > 0)') &&
SESSION_ONE_JOIN=$(printf '%s' "$SESSION_ONE_JSON" |
  jq -er '.join_target | strings | select(length > 0)') &&
SESSION_TWO_JSON=$(curl -fsS -X POST \
  -H 'Content-Type: application/json' -d '{}' \
  http://127.0.0.1:9080/vif/api/sessions) &&
SESSION_TWO_ID=$(printf '%s' "$SESSION_TWO_JSON" |
  jq -er '.id | strings | select(length > 0)') &&
SESSION_TWO_JOIN=$(printf '%s' "$SESSION_TWO_JSON" |
  jq -er '.join_target | strings | select(length > 0)') &&
test "$SESSION_ONE_ID" != "$SESSION_TWO_ID" &&
printf 'client one: bin/vif -join %s\n' "$SESSION_ONE_JOIN" &&
printf 'client two: bin/vif -join %s\n' "$SESSION_TWO_JOIN"
```

Immediately join from the two prepared development-machine terminals, one target
per client:

```sh
bin/vif -join '<first_join_target>'
```

```sh
bin/vif -join '<second_join_target>'
```

Once both clients are visibly running, prove both allocator states, files, and
stream identities. Then snapshot each file while the stream remains connected:

```sh
curl -fsS http://127.0.0.1:9080/vif/api/sessions |
  jq -e --arg one "$SESSION_ONE_ID" \
    --arg two "$SESSION_TWO_ID" '
    [.sessions[] |
      select(.id == $one or .id == $two) |
      select(.state.phase == "occupied" and
             .state.guests >= 1)] |
    length == 2'

for attempt in $(seq 1 50); do
  if sudo test -s "/var/log/vif-fleet/$SESSION_ONE_ID.jsonl" &&
     sudo test -s "/var/log/vif-fleet/$SESSION_TWO_ID.jsonl" &&
     grep -Fq "\"session_id\":\"$SESSION_ONE_ID\"" \
       "$BATCH_E_CAPTURE" &&
     grep -Fq "\"session_id\":\"$SESSION_TWO_ID\"" \
       "$BATCH_E_CAPTURE"
  then
    break
  fi
  sleep 0.2
done

sudo test -s "/var/log/vif-fleet/$SESSION_ONE_ID.jsonl"
sudo test -s "/var/log/vif-fleet/$SESSION_TWO_ID.jsonl"
grep -Fq "\"session_id\":\"$SESSION_ONE_ID\"" \
  "$BATCH_E_CAPTURE"
grep -Fq "\"session_id\":\"$SESSION_TWO_ID\"" \
  "$BATCH_E_CAPTURE"

sudo jq -s -e --arg id "$SESSION_ONE_ID" '
  map(select(.sub != null)) as $records |
  ($records | length > 0) and
  all($records[]; .fields.session_id == $id)
' "/var/log/vif-fleet/$SESSION_ONE_ID.jsonl"
sudo jq -s -e --arg id "$SESSION_TWO_ID" '
  map(select(.sub != null)) as $records |
  ($records | length > 0) and
  all($records[]; .fields.session_id == $id)
' "/var/log/vif-fleet/$SESSION_TWO_ID.jsonl"

BATCH_E_ONE_SNAPSHOT=$(mktemp \
  /tmp/vif-logwisp-baseline-one.XXXXXX.jsonl)
BATCH_E_TWO_SNAPSHOT=$(mktemp \
  /tmp/vif-logwisp-baseline-two.XXXXXX.jsonl)
sudo cp -- "/var/log/vif-fleet/$SESSION_ONE_ID.jsonl" \
  "$BATCH_E_ONE_SNAPSHOT"
sudo cp -- "/var/log/vif-fleet/$SESSION_TWO_ID.jsonl" \
  "$BATCH_E_TWO_SNAPSHOT"
```

Wait until the last non-TRACE snapshot line from each file reaches the stream,
then stop the temporary reader so the capture is stable:

```sh
LAST_ONE=$(awk '
  index($0, "\"level\":\"TRACE\"") == 0 {last = $0}
  END {print last}
' "$BATCH_E_ONE_SNAPSHOT")
LAST_TWO=$(awk '
  index($0, "\"level\":\"TRACE\"") == 0 {last = $0}
  END {print last}
' "$BATCH_E_TWO_SNAPSHOT")
test -n "$LAST_ONE"
test -n "$LAST_TWO"

for attempt in $(seq 1 50); do
  if grep -Fqx -- "data: $LAST_ONE" "$BATCH_E_CAPTURE" &&
     grep -Fqx -- "data: $LAST_TWO" "$BATCH_E_CAPTURE"
  then
    break
  fi
  sleep 0.2
done

grep -Fqx -- "data: $LAST_ONE" "$BATCH_E_CAPTURE"
grep -Fqx -- "data: $LAST_TWO" "$BATCH_E_CAPTURE"

curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status |
  jq -e '
    .server.active_clients == 1 and
    .statistics.total_processed > 0 and
    .statistics.auth_rejected == 0 and
    .statistics.dropped_writes == 0 and
    .statistics.rejected_clients == 0'

kill "$BATCH_E_STREAM_PID"
wait "$BATCH_E_STREAM_PID" 2>/dev/null || true
```

Every non-TRACE source line in both snapshots must appear as one exact SSE data
payload. This compares the original bytes rather than JSON reserialization:

```sh
verify_preserved_snapshot() {
  snapshot=$1
  capture=$2
  records=0
  while IFS= read -r line; do
    case "$line" in
      *'"level":"TRACE"'*) continue ;;
    esac
    records=$((records + 1))
    grep -Fqx -- "data: $line" "$capture" || {
      printf 'missing exact source line from %s\n' \
        "$snapshot" >&2
      return 1
    }
  done <"$snapshot"
  test "$records" -gt 0
  printf 'preserved_records=%s source=%s\n' \
    "$records" "$snapshot"
}

verify_preserved_snapshot \
  "$BATCH_E_ONE_SNAPSHOT" "$BATCH_E_CAPTURE"
verify_preserved_snapshot \
  "$BATCH_E_TWO_SNAPSHOT" "$BATCH_E_CAPTURE"

curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status | jq .
sed -n '1,12p' "$BATCH_E_CAPTURE"
unset -f verify_preserved_snapshot
unset LAST_ONE LAST_TWO
```

Quit both remote clients. Verify both sessions become vacant, then delete their
objects, commissioned files, and every temporary capture from this gate:

```sh
curl -fsS http://127.0.0.1:9080/vif/api/sessions |
  jq -e --arg one "$SESSION_ONE_ID" \
    --arg two "$SESSION_TWO_ID" '
    [.sessions[] |
      select(.id == $one or .id == $two) |
      select(.state.phase == "vacant" and
             .state.guests == 0)] |
    length == 2'

for id in "$SESSION_ONE_ID" "$SESSION_TWO_ID"; do
  ./deploy/k3s/session.sh delete "$id"
  sudo kubectl -n vif wait --for=delete \
    "job/vif-session-$id" --timeout=60s
  sudo kubectl -n vif wait --for=delete pod \
    -l "vif.lixenwraith.dev/session=$id" \
    --timeout=60s
  sudo find /var/log/vif-fleet -maxdepth 1 -type f \
    \( -name "$id.jsonl" -o -name "${id}_*.jsonl" \) \
    -delete
done

for temporary in \
  "$BATCH_E_CAPTURE" \
  "$BATCH_E_ONE_SNAPSHOT" \
  "$BATCH_E_TWO_SNAPSHOT"
do
  case "$temporary" in
    /tmp/vif-logwisp-baseline.*|\
    /tmp/vif-logwisp-baseline-one.*|\
    /tmp/vif-logwisp-baseline-two.*)
      rm -f -- "$temporary"
      ;;
    *)
      printf 'refusing unsafe temporary path: %s\n' \
        "$temporary" >&2
      false
      ;;
  esac
done

sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo find /var/log/vif-fleet \
  -mindepth 1 -maxdepth 1 -print
systemctl is-active logwisp.service vif-allocator.service

for attempt in $(seq 1 50); do
  curl --connect-timeout 1 --max-time 2 -fsS \
    http://127.0.0.1:8081/status |
    jq -e '.server.active_clients == 0' \
      >/dev/null 2>&1 && break
  sleep 0.1
done

curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status |
  jq -e '
    .server.active_clients == 0 and
    .statistics.total_processed > 0 and
    .statistics.auth_rejected == 0 and
    .statistics.dropped_writes == 0 and
    .statistics.rejected_clients == 0'

unset SESSION_ONE_JSON SESSION_ONE_ID SESSION_ONE_JOIN
unset SESSION_TWO_JSON SESSION_TWO_ID SESSION_TWO_JOIN
unset BATCH_E_CAPTURE BATCH_E_STREAM_PID
unset BATCH_E_ONE_SNAPSHOT BATCH_E_TWO_SNAPSHOT
```

The fleet query and final `find` must print nothing. This gate passed on
2026-09-13 with two simultaneously occupied sessions: 255 and 351 sampled
non-TRACE records were preserved byte-for-byte with distinct session self-tags,
zero sink drops, and zero rejected clients.

### Batch E outage/replay and common session gate

This is a deliberate availability test, not a logging deployment change. It
briefly stops and restarts only `logwisp.service`; its binary and configuration,
the tmpfs/PV/PVC, K3s, allocator, and session workload remain unchanged. Keep one
remote game connected and visibly playing throughout the outage and restart.

The allocation is deadline-sensitive: prepare the development-machine terminal
with the matching `bin/vif` before creating the session. Later, the replay reader
must start while LogWisp is still stopped, immediately before the announced
service restart. Keep the node terminal open until all variables and temporary
files are cleaned.

Begin with an empty fleet and log directory, no LogWisp stream client, all five
services active, and the persistent volumes still `Bound`:

```sh
unset SESSION_JSON SESSION_ID JOIN_TARGET
unset OUTAGE_TICK_BEFORE OUTAGE_TICK_AFTER OUTAGE_TICK_RESTARTED
unset OUTAGE_PROBE_JSON OUTAGE_PROBE_ID
unset BATCH_E_REPLAY_SNAPSHOT BATCH_E_REPLAY_CAPTURE
unset BATCH_E_REPLAY_STREAM_PID REPLAY_RECORDS REPLAY_SENTINEL

systemctl is-active \
  'var-log-vif\x2dfleet.mount' \
  vif-fleet-log-cleanup.timer \
  k3s.service \
  vif-allocator.service \
  logwisp.service

sudo kubectl get persistentvolume vif-fleet-logs
sudo kubectl -n vif get persistentvolumeclaim vif-fleet-logs
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo find /var/log/vif-fleet \
  -mindepth 1 -maxdepth 1 -print

curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status |
  jq -e '
    .server.active_clients == 0 and
    .statistics.auth_rejected == 0 and
    .statistics.dropped_writes == 0 and
    .statistics.rejected_clients == 0'
```

The fleet query and `find` must print nothing. Allocate one session, reject empty
response fields, and immediately join it from the prepared remote terminal:

```sh
SESSION_JSON=$(curl -fsS -X POST \
  -H 'Content-Type: application/json' -d '{}' \
  http://127.0.0.1:9080/vif/api/sessions) &&
SESSION_ID=$(printf '%s' "$SESSION_JSON" |
  jq -er '.id | strings | select(length > 0)') &&
JOIN_TARGET=$(printf '%s' "$SESSION_JSON" |
  jq -er '.join_target | strings | select(length > 0)') &&
printf 'session=%s join=%s\n' \
  "$SESSION_ID" "$JOIN_TARGET"
```

```sh
bin/vif -join '<join_target>'
```

While the client remains connected, verify the common Restricted workload shape,
occupied state, commissioned file, and its pre-outage tick:

```sh
sudo kubectl -n vif get job \
  "vif-session-$SESSION_ID" -o json |
  jq -e --arg id "$SESSION_ID" '
    .spec.template.spec as $pod |
    ($pod.automountServiceAccountToken == false) and
    ($pod.containers | length == 1) and
    ($pod.containers[0].name == "session") and
    ($pod.containers[0].args |
      index("-l=/var/log/vif-fleet") != null) and
    ($pod.containers[0].args |
      index("-log-session-id=" + $id) != null) and
    ($pod.containers[0].args |
      index("-log-stdout") == null) and
    any($pod.containers[0].volumeMounts[]?;
      .name == "fleet-logs" and
      .mountPath == "/var/log/vif-fleet") and
    any($pod.volumes[]?;
      .name == "fleet-logs" and
      .persistentVolumeClaim.claimName == "vif-fleet-logs") and
    all($pod.volumes[]?;
      has("hostPath") | not) and
    all($pod.initContainers[]?;
      ((.volumeMounts // []) | length) == 0) and
    ($pod.containers[0].securityContext.allowPrivilegeEscalation == false) and
    ($pod.containers[0].securityContext.readOnlyRootFilesystem == true) and
    ($pod.containers[0].securityContext.runAsNonRoot == true) and
    ($pod.containers[0].securityContext.capabilities.drop |
      index("ALL") != null)'

for attempt in $(seq 1 50); do
  sudo test -s "/var/log/vif-fleet/$SESSION_ID.jsonl" && break
  sleep 0.2
done
sudo test -s "/var/log/vif-fleet/$SESSION_ID.jsonl"

OUTAGE_TICK_BEFORE=$(curl -fsS \
  http://127.0.0.1:9080/vif/api/sessions |
  jq -er --arg id "$SESSION_ID" '
    .sessions[] | select(.id == $id) | .state |
    select(.phase == "occupied" and .guests >= 1) |
    .tick')
printf 'tick_before_outage=%s\n' "$OUTAGE_TICK_BEFORE"
```

Announce the interruption to the remote player, who must keep moving or firing
for the next several seconds. Stop only LogWisp and prove that its loopback
listener is gone while the allocator stays healthy and ready:

```sh
sudo systemctl stop logwisp.service
test "$(systemctl is-active logwisp.service)" = inactive
test -z "$(sudo ss -ltnH 'sport = :8081')"

if curl --connect-timeout 1 --max-time 2 -fsS \
  http://127.0.0.1:8081/status >/dev/null 2>&1
then
  printf 'LogWisp unexpectedly remained reachable\n' >&2
  false
else
  printf 'LogWisp unavailable as expected\n'
fi

curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz
```

While LogWisp remains stopped, commission a second session to prove allocation
does not depend on it. Do not join this probe. Verify the original session stays
occupied, the new session is waiting, and the original game clock advances:

```sh
OUTAGE_PROBE_JSON=$(curl -fsS -X POST \
  -H 'Content-Type: application/json' -d '{}' \
  http://127.0.0.1:9080/vif/api/sessions) &&
OUTAGE_PROBE_ID=$(printf '%s' "$OUTAGE_PROBE_JSON" |
  jq -er '.id | strings | select(length > 0)') &&
test "$OUTAGE_PROBE_ID" != "$SESSION_ID" &&
printf 'outage_probe=%s\n' "$OUTAGE_PROBE_ID"

curl -fsS http://127.0.0.1:9080/vif/api/sessions |
  jq -e --arg primary "$SESSION_ID" \
    --arg probe "$OUTAGE_PROBE_ID" '
    any(.sessions[];
      .id == $primary and
      .state.phase == "occupied" and
      .state.guests >= 1) and
    any(.sessions[];
      .id == $probe and
      .state.phase == "waiting" and
      .state.guests == 0)'

sleep 2
OUTAGE_TICK_AFTER=$(curl -fsS \
  http://127.0.0.1:9080/vif/api/sessions |
  jq -er --arg id "$SESSION_ID" '
    .sessions[] | select(.id == $id) | .state |
    select(.phase == "occupied" and .guests >= 1) |
    .tick')
test "$OUTAGE_TICK_AFTER" -gt "$OUTAGE_TICK_BEFORE"
printf 'tick_during_outage=%s -> %s\n' \
  "$OUTAGE_TICK_BEFORE" "$OUTAGE_TICK_AFTER"
```

Delete the unjoined probe while LogWisp is still stopped. Remove only its log
files and retain the occupied session's JSONL for replay:

```sh
./deploy/k3s/session.sh delete "$OUTAGE_PROBE_ID"
sudo kubectl -n vif wait --for=delete \
  "job/vif-session-$OUTAGE_PROBE_ID" --timeout=60s
sudo kubectl -n vif wait --for=delete pod \
  -l "vif.lixenwraith.dev/session=$OUTAGE_PROBE_ID" \
  --timeout=60s
sudo find /var/log/vif-fleet -maxdepth 1 -type f \
  \( -name "$OUTAGE_PROBE_ID.jsonl" \
     -o -name "${OUTAGE_PROBE_ID}_*.jsonl" \) \
  -delete
sudo kubectl -n vif get job,pod,service \
  -l "vif.lixenwraith.dev/session=$OUTAGE_PROBE_ID"
```

Snapshot the retained occupied-session file while LogWisp is stopped. The
sentinel is an exact non-TRACE record that existed before the restart, so seeing
it after restart proves replay rather than delivery of a new line:

```sh
BATCH_E_REPLAY_SNAPSHOT=$(mktemp \
  /tmp/vif-logwisp-replay.XXXXXX.jsonl)
sudo cp -- "/var/log/vif-fleet/$SESSION_ID.jsonl" \
  "$BATCH_E_REPLAY_SNAPSHOT"

REPLAY_RECORDS=$(awk '
  index($0, "\"level\":\"TRACE\"") == 0 {count++}
  END {print count + 0}
' "$BATCH_E_REPLAY_SNAPSHOT")
REPLAY_SENTINEL=$(awk '
  index($0, "\"level\":\"TRACE\"") == 0 {print; exit}
' "$BATCH_E_REPLAY_SNAPSHOT")
test "$REPLAY_RECORDS" -gt 0
test -n "$REPLAY_SENTINEL"
printf 'retained_non_trace_records=%s\n' "$REPLAY_RECORDS"
```

This restart step is simultaneous: start the waiting reader first while port
8081 is still closed, then immediately start LogWisp. The reader polls the local
listen table for at most 30 seconds and replaces itself with `curl` as soon as
the sink binds. Do not reverse these two commands or the source can replay before
a client is listening:

```sh
BATCH_E_REPLAY_CAPTURE=$(mktemp \
  /tmp/vif-logwisp-replay-capture.XXXXXX.sse)
(
  for attempt in $(seq 1 3000); do
    if ss -ltnH 'sport = :8081' | grep -q .; then
      exec curl --no-buffer --fail --silent --show-error \
        --connect-timeout 1 \
        http://127.0.0.1:8081/stream
    fi
    sleep 0.01
  done
  printf 'LogWisp listener did not return within 30 seconds\n' >&2
  exit 1
) >"$BATCH_E_REPLAY_CAPTURE" &
BATCH_E_REPLAY_STREAM_PID=$!

sudo systemctl start logwisp.service

for attempt in $(seq 1 100); do
  curl --connect-timeout 1 --max-time 2 -fsS \
    http://127.0.0.1:8081/status 2>/dev/null |
    jq -e '
      .server.active_clients == 1 and
      .statistics.total_processed > 0 and
      .statistics.auth_rejected == 0 and
      .statistics.rejected_clients == 0' \
      >/dev/null 2>&1 && break
  sleep 0.1
done

test -s "$BATCH_E_REPLAY_CAPTURE"
REPLAY_STATUS=$(curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status) &&
printf '%s\n' "$REPLAY_STATUS" | jq -e '
    .server.active_clients == 1 and
    .statistics.total_processed > 0 and
    .statistics.auth_rejected == 0 and
    .statistics.dropped_writes >= 0 and
    .statistics.dropped_writes <= .statistics.total_processed and
    .statistics.rejected_clients == 0' &&
REPLAY_DROPPED_WRITES=$(printf '%s\n' "$REPLAY_STATUS" |
  jq -er '.statistics.dropped_writes') &&
printf 'replay_dropped_writes=%s\n' "$REPLAY_DROPPED_WRITES"
```

The HTTP sink deliberately gives each client a bounded 512-entry queue. A file
replay can fill it faster than even a loopback reader drains it, so zero replay
drops are preferred but are not a correctness requirement. A nonzero
`replay_dropped_writes` is bounded loss, not a game or allocator failure; record
it for Batch G sizing. The exact sentinel below remains mandatory, as do zero
authorization and client-rejection counters. Normal two-session fan-in has a
separate zero-drop gate.

Wait for the exact pre-restart sentinel, stabilize the capture, and record the
accepted replay evidence:

```sh
for attempt in $(seq 1 100); do
  grep -Fqx -- "data: $REPLAY_SENTINEL" \
    "$BATCH_E_REPLAY_CAPTURE" && break
  sleep 0.1
done
grep -Fqx -- "data: $REPLAY_SENTINEL" \
  "$BATCH_E_REPLAY_CAPTURE"

printf 'replay_sentinel=accepted retained_records=%s\n' \
  "$REPLAY_RECORDS"
sed -n '1,8p' "$BATCH_E_REPLAY_CAPTURE"

kill "$BATCH_E_REPLAY_STREAM_PID"
wait "$BATCH_E_REPLAY_STREAM_PID" 2>/dev/null || true

for attempt in $(seq 1 50); do
  curl --connect-timeout 1 --max-time 2 -fsS \
    http://127.0.0.1:8081/status |
    jq -e '.server.active_clients == 0' \
      >/dev/null 2>&1 && break
  sleep 0.1
done
```

The remote game must still be playable. Verify it remains occupied and advances
again after LogWisp restarts, and inspect only LogWisp's service journal:

```sh
OUTAGE_TICK_RESTARTED=$(curl -fsS \
  http://127.0.0.1:9080/vif/api/sessions |
  jq -er --arg id "$SESSION_ID" '
    .sessions[] | select(.id == $id) | .state |
    select(.phase == "occupied" and .guests >= 1) |
    .tick')
test "$OUTAGE_TICK_RESTARTED" -gt "$OUTAGE_TICK_AFTER"
printf 'tick_after_restart=%s -> %s\n' \
  "$OUTAGE_TICK_AFTER" "$OUTAGE_TICK_RESTARTED"

curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz
sudo journalctl -u logwisp.service -n 20 --no-pager
```

The pre-update v0.18.0 build reported `Watcher failed` with `error "watcher
stopped"` when normal fleet cleanup removed a watched file. The pinned revision
treats that retirement as a normal exit and prevents a retiring watcher from
removing a replacement for the same recreated path. Those entries persist in the
journal; scope every later judgement to the invocation started by the update, in
which any `Watcher failed` line is an actual failure to investigate.

Quit the remote client. Finish the common gate by verifying vacancy and complete
self-tagging before deleting the session:

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
  all($records[];
    .fields.session_id == $id)
' "/var/log/vif-fleet/$SESSION_ID.jsonl"

./deploy/k3s/session.sh delete "$SESSION_ID"
sudo kubectl -n vif wait --for=delete \
  "job/vif-session-$SESSION_ID" --timeout=60s
sudo kubectl -n vif wait --for=delete pod \
  -l "vif.lixenwraith.dev/session=$SESSION_ID" \
  --timeout=60s
sudo find /var/log/vif-fleet -maxdepth 1 -type f \
  \( -name "$SESSION_ID.jsonl" \
     -o -name "${SESSION_ID}_*.jsonl" \) \
  -delete
```

Remove both temporary replay files using only their expected `mktemp` paths,
then prove the full Batch E steady state:

```sh
for temporary in \
  "$BATCH_E_REPLAY_SNAPSHOT" \
  "$BATCH_E_REPLAY_CAPTURE"
do
  case "$temporary" in
    /tmp/vif-logwisp-replay.*|\
    /tmp/vif-logwisp-replay-capture.*)
      rm -f -- "$temporary"
      ;;
    *)
      printf 'refusing unsafe temporary path: %s\n' \
        "$temporary" >&2
      false
      ;;
  esac
done

sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo find /var/log/vif-fleet \
  -mindepth 1 -maxdepth 1 -print

systemctl is-active \
  'var-log-vif\x2dfleet.mount' \
  vif-fleet-log-cleanup.timer \
  k3s.service \
  vif-allocator.service \
  logwisp.service
systemctl show vif-fleet-log-cleanup.service \
  -p User -p Group -p Result -p ExecMainStatus

sudo kubectl get namespace vif --show-labels
sudo kubectl get persistentvolume vif-fleet-logs
sudo kubectl -n vif get persistentvolumeclaim vif-fleet-logs
test "$(sudo kubectl auth can-i get pods --subresource=log \
  --as=system:serviceaccount:vif:vif-allocator -n vif)" = no

FINAL_REPLAY_STATUS=$(curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status) &&
printf '%s\n' "$FINAL_REPLAY_STATUS" | jq -e \
  --argjson observed "$REPLAY_DROPPED_WRITES" '
    .server.active_clients == 0 and
    .statistics.total_processed > 0 and
    .statistics.auth_rejected == 0 and
    .statistics.dropped_writes >= $observed and
    .statistics.dropped_writes <= .statistics.total_processed and
    .statistics.rejected_clients == 0' &&
FINAL_REPLAY_DROPS=$(printf '%s\n' "$FINAL_REPLAY_STATUS" |
  jq -er '.statistics.dropped_writes') &&
printf 'final_replay_dropped_writes=%s\n' "$FINAL_REPLAY_DROPS"

unset SESSION_JSON SESSION_ID JOIN_TARGET
unset OUTAGE_TICK_BEFORE OUTAGE_TICK_AFTER OUTAGE_TICK_RESTARTED
unset OUTAGE_PROBE_JSON OUTAGE_PROBE_ID
unset BATCH_E_REPLAY_SNAPSHOT BATCH_E_REPLAY_CAPTURE
unset BATCH_E_REPLAY_STREAM_PID REPLAY_RECORDS REPLAY_SENTINEL
unset REPLAY_STATUS REPLAY_DROPPED_WRITES
unset FINAL_REPLAY_STATUS FINAL_REPLAY_DROPS
```

The fleet query and `find` must print nothing; all five services must be active,
the cleanup result successful as `vif-fleet`, Restricted labels intact, PV/PVC
`Bound`, allocator Pod-log access denied, and final LogWisp expression `true`.
Record the printed replay-drop count; it may be nonzero, but must not exceed the
number of processed records in this single-client gate.
If replay fails, keep the game connected, collect the capture, status, and
journal, kill the retrying curl if it is still running, and do not proceed to
Batch F.

The live gate completed on 2026-09-13. LogWisp was unavailable while the
allocator created a second waiting session and the occupied game advanced from
tick 316 to 1558. After restart the waiting stream received an exact pre-outage
sentinel from a retained 1,234-record snapshot, the game advanced to tick 4380,
and cleanup returned the fleet and tmpfs to empty. The bounded 512-entry client
queue recorded 86 dropped writes while processing 1,841 replay/live records;
authorization and connection rejections remained zero. This measured replay
loss is retained for Batch G sizing rather than misreported as a game failure.

### Batch E service rollback

If the service, isolation, or loopback checks fail, remove only its live listener.
Keep the pinned binary, configuration, and locked identity for diagnosis; do not
touch the allocator, K3s, writer, PV/PVC, tmpfs, or Restricted namespace:

```sh
sudo systemctl disable --now logwisp.service
sudo rm /etc/systemd/system/logwisp.service
sudo systemctl daemon-reload
test "$(systemctl is-active logwisp.service)" = inactive
sudo ss -ltnH 'sport = :8081'
```

The final `ss` command must print nothing.

## Batch F: deploy the allocator byte proxy

Batch E is complete and the pinned LogWisp update above was deployed on
2026-09-14, so this cutover may run. The allocator change does not touch the
workload, Role, PVC, tmpfs, or LogWisp process. It adds one validated loopback
upstream and proxies SSE framing bytes without parsing or retaining records.

What the batch proves: `/vif/api/logs` becomes the same-origin byte proxy for
LogWisp's SSE stream, and neither service depends on the other. F1-F4 and F7 are
node-only and can be run whenever; F5 needs a second machine and a 90-second join
window, and F6 reads the journal F5 produced. Its seven steps are labelled F1-F7
and match `doc/kube-todo.md` §3. Report each step's output before the next.

### F1 - preflight

**Purpose.** Establish the before-state this cutover is measured against: the
node at its Batch E steady state, and the deployed allocator still answering
`501`. **Node terminal only; read-only.**

```sh
systemctl is-active \
  'var-log-vif\x2dfleet.mount' \
  vif-fleet-log-cleanup.timer \
  k3s.service vif-allocator.service logwisp.service
sudo kubectl get persistentvolume vif-fleet-logs
sudo kubectl -n vif get persistentvolumeclaim vif-fleet-logs
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo find /var/log/vif-fleet -mindepth 1 -maxdepth 1 -print

curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status | jq -e '
    .server.client_buffer_size == 512 and
    .server.max_connections == 32 and
    .server.write_timeout_ms == 5000'

OLD_LOG_STATUS=$(curl --connect-timeout 2 --max-time 5 -sS \
  -o /tmp/vif-batch-f-old-log.json -w '%{http_code}' \
  http://127.0.0.1:9080/vif/api/logs)
test "$OLD_LOG_STATUS" = 501
jq -e '.error.code == "log_stream_not_configured"' \
  /tmp/vif-batch-f-old-log.json
rm -f /tmp/vif-batch-f-old-log.json
unset OLD_LOG_STATUS
```

The fleet query and `find` must print nothing.

### F2 - install the proxying allocator

**Purpose.** Replace the allocator binary, env and unit with the proxying set.
**Announce a short allocation pause first**; existing games are untouched, but no
session can be created while the service is down. The updater builds first, then stops the
allocator, repeats the empty-fleet check, preserves one known-good
binary/config/unit, installs the new set, and waits for health and readiness. It
restores the previous set automatically if verification fails. It requires a
clean worktree at the revision being deployed, and reads
`/etc/vif-allocator/allocator.env` through `sudo`, since that directory is
`root:vif-allocator` 0750:

```sh
test -x deploy/guest/update-vif-allocator.sh
./deploy/guest/update-vif-allocator.sh
```

### F3 - service independence

**Purpose.** Prove the new unit did not make allocation depend on logging. Only
K3s may be required; LogWisp may be wanted and ordered after for normal startup,
but is never a requirement and never an allocator child. **Node terminal only.**

```sh
systemctl is-active vif-allocator.service logwisp.service
systemctl show vif-allocator.service -p Requires -p Wants -p After
if systemctl show vif-allocator.service -p Requires --value |
   tr ' ' '\n' | grep -Fx logwisp.service
then
  printf 'FAIL: allocator requires LogWisp\n' >&2
  false
fi
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz
```

### F4 - proxy shape

**Purpose.** Prove the route accepts only `GET` and `HEAD`, and that LogWisp's
own stream headers and first frame cross the proxy unchanged. **Node terminal
only; nothing is time-sensitive.** The three header values and the
`event: connected` frame all originate in LogWisp's HTTP sink, so this measures
preservation, not allocator behaviour. Each reader here ends by itself:

```sh
BATCH_F_HEADERS=$(mktemp /tmp/vif-batch-f-headers.XXXXXX)
curl --connect-timeout 2 --max-time 5 -fsSI \
  http://127.0.0.1:9080/vif/api/logs >"$BATCH_F_HEADERS"
grep -Eiq '^content-type: text/event-stream' "$BATCH_F_HEADERS"
grep -Eiq '^cache-control: no-cache' "$BATCH_F_HEADERS"
grep -Eiq '^x-accel-buffering: no' "$BATCH_F_HEADERS"

POST_STATUS=$(curl --connect-timeout 2 --max-time 5 -sS \
  -X POST -o /tmp/vif-batch-f-method.json -w '%{http_code}' \
  http://127.0.0.1:9080/vif/api/logs)
test "$POST_STATUS" = 405
jq -e '.error.code == "method_not_allowed"' \
  /tmp/vif-batch-f-method.json
rm -f /tmp/vif-batch-f-method.json

BATCH_F_FIRST=$(curl --no-buffer -fsS --max-time 5 \
  http://127.0.0.1:9080/vif/api/logs 2>/dev/null | head -n 1)
printf 'first frame: %s\n' "$BATCH_F_FIRST"
test "$BATCH_F_FIRST" = 'event: connected'
```

A `HEAD` must not disturb the `GET` that follows it. If the first frame is empty
or the stream answers `503 log_stream_unavailable` here while `logwisp.service`
is active, the allocator predates the fix that stops a `HEAD` reply from stalling
the next stream; re-run F2 from a checkout that contains it.

`deploy/website/vif-log-viewer.html` is the bounded browser reference for the
same route. The node has no display, so it is verified behind nginx in H15, where
`doc/kube_docker_deploy.md` §10.5 holds that gate.

### F5 - live session gate

**Purpose.** Prove the whole path with a real game on it: one player's records
reach a stream client byte-for-byte through the proxy, the allocator keeps
allocating and the game keeps running while LogWisp is stopped, and the retained
file replays exactly when it returns.

**F5 is Batch F's §4 run**, interleaved with the outage, so §4 of
`doc/kube-todo.md` is never run separately for this batch and its first block is
never pasted again here: that creates a second session and discards `SESSION_ID`.

**This step is invalid without a second machine.** Its proofs all require an
`occupied` session, and the first-join window is 90 seconds from the `POST` in
F5.2. Have the development machine's `bin/vif` built and its terminal in the
foreground before starting. If the join is missed, delete the session (F5.7) and
restart at F5.1 rather than continuing: every later command reads state that only
exists while a guest is connected.

#### F5.1 - start the stream reader

**Node terminal. Not time-sensitive, but it must precede F5.2** so the session's
first records are captured live rather than replayed.

```sh
BATCH_F_CAPTURE=$(mktemp /tmp/vif-batch-f-stream.XXXXXX.sse)
curl --no-buffer --fail --silent --show-error \
  http://127.0.0.1:9080/vif/api/logs >"$BATCH_F_CAPTURE" &
BATCH_F_STREAM_PID=$!
for attempt in $(seq 1 50); do
  grep -Fxq 'event: connected' "$BATCH_F_CAPTURE" && break
  sleep 0.1
done
grep -Fxq 'event: connected' "$BATCH_F_CAPTURE" &&
  printf 'reader: connected\n'

for attempt in $(seq 1 50); do
  curl --connect-timeout 2 --max-time 5 -fsS \
    http://127.0.0.1:8081/status |
    jq -e '.server.active_clients == 1' >/dev/null 2>&1 && break
  sleep 0.1
done
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status |
  jq -e '.server.active_clients == 1'
```

One reader must mean exactly one upstream client. A larger count is a leak: print
`.server.active_clients`, stop the reader, and re-check that it returns to zero
before going on.

#### F5.2 - allocate and join

**Time-critical: the 90-second first-join clock starts when `POST` returns.** Run
the node block, then join from the waiting development machine immediately.

```sh
unset SESSION_JSON SESSION_ID JOIN_TARGET
SESSION_JSON=$(curl -fsS -X POST \
  -H 'Content-Type: application/json' -d '{}' \
  http://127.0.0.1:9080/vif/api/sessions) &&
SESSION_ID=$(printf '%s' "$SESSION_JSON" |
  jq -er '.id | strings | select(length > 0)') &&
JOIN_TARGET=$(printf '%s' "$SESSION_JSON" |
  jq -er '.join_target | strings | select(length > 0)') &&
printf 'session=%s join=%s\n' "$SESSION_ID" "$JOIN_TARGET"
test -n "$SESSION_ID"
```

On the development machine, with the printed target:

```sh
bin/vif -join '<join_target>'
```

That block is §4's creation step. Run only §4's Job-shape block next — the one
beginning `sudo kubectl -n vif get job "vif-session-$SESSION_ID" -o json` — and
its occupied-state query, then stop and return here. Do not paste §4 from its
top; F5.7 below carries the rest of §4 inline.

**Expect:** a session id and join target, then `true` from both §4 queries.

#### F5.3 - occupancy gate

**Node terminal, while the player stays connected and moving.** Stop here if this
prints nothing: everything below reads an occupied session.

```sh
BATCH_F_TICK_BEFORE=$(curl -fsS \
  http://127.0.0.1:9080/vif/api/sessions |
  jq -er --arg id "$SESSION_ID" '
    .sessions[] | select(.id == $id) | .state |
    select(.phase == "occupied" and .guests >= 1) | .tick')
test -n "$BATCH_F_TICK_BEFORE"
printf 'occupied tick=%s\n' "$BATCH_F_TICK_BEFORE"
```

#### F5.4 - byte preservation

Select one exact non-TRACE record from the commissioned file and require it,
unchanged, in the reader's capture:

```sh
for attempt in $(seq 1 50); do
  sudo test -s "/var/log/vif-fleet/$SESSION_ID.jsonl" && break
  sleep 0.2
done
BATCH_F_SENTINEL=$(sudo awk '
  index($0, "\"level\":\"TRACE\"") == 0 {print; exit}
' "/var/log/vif-fleet/$SESSION_ID.jsonl")
test -n "$BATCH_F_SENTINEL"
for attempt in $(seq 1 50); do
  grep -Fqx -- "data: $BATCH_F_SENTINEL" "$BATCH_F_CAPTURE" && break
  sleep 0.2
done
grep -Fqx -- "data: $BATCH_F_SENTINEL" "$BATCH_F_CAPTURE" &&
  printf 'proxy sentinel: byte-exact\n'
```

**Expect:** `proxy sentinel: byte-exact`. Its absence is a failure, whatever else
the block printed.

#### F5.5 - LogWisp outage independence

**Announce the LogWisp-only interruption to the player first; the game must stay
connected throughout.** Stop the reader, stop LogWisp, then prove the proxy's
stable failure response while allocation, probes and gameplay continue. The
second session is an unjoined probe and is deleted before LogWisp returns:

```sh
kill "$BATCH_F_STREAM_PID"
wait "$BATCH_F_STREAM_PID" 2>/dev/null || true

sudo systemctl stop logwisp.service
test "$(systemctl is-active logwisp.service)" = inactive

BATCH_F_ERROR=$(mktemp /tmp/vif-batch-f-error.XXXXXX.json)
BATCH_F_ERROR_STATUS=$(curl --connect-timeout 2 --max-time 5 -sS \
  -o "$BATCH_F_ERROR" -w '%{http_code}' \
  http://127.0.0.1:9080/vif/api/logs)
test "$BATCH_F_ERROR_STATUS" = 503
jq -e '.error.code == "log_stream_unavailable"' "$BATCH_F_ERROR"
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz

BATCH_F_PROBE_JSON=$(curl -fsS -X POST \
  -H 'Content-Type: application/json' -d '{}' \
  http://127.0.0.1:9080/vif/api/sessions) &&
BATCH_F_PROBE_ID=$(printf '%s' "$BATCH_F_PROBE_JSON" |
  jq -er '.id | strings | select(length > 0)')
curl -fsS http://127.0.0.1:9080/vif/api/sessions |
  jq -e --arg primary "$SESSION_ID" --arg probe "$BATCH_F_PROBE_ID" '
    any(.sessions[];
      .id == $primary and .state.phase == "occupied" and .state.guests >= 1) and
    any(.sessions[];
      .id == $probe and .state.phase == "waiting" and .state.guests == 0)'
sleep 2
BATCH_F_TICK_DOWN=$(curl -fsS \
  http://127.0.0.1:9080/vif/api/sessions |
  jq -er --arg id "$SESSION_ID" '
    .sessions[] | select(.id == $id) | .state.tick')
test -n "$BATCH_F_TICK_DOWN"
test "$BATCH_F_TICK_DOWN" -gt "$BATCH_F_TICK_BEFORE" &&
  printf 'game advanced while LogWisp was down: %s -> %s\n' \
    "$BATCH_F_TICK_BEFORE" "$BATCH_F_TICK_DOWN"

./deploy/k3s/session.sh delete "$BATCH_F_PROBE_ID"
sudo kubectl -n vif wait --for=delete \
  "job/vif-session-$BATCH_F_PROBE_ID" --timeout=60s
sudo kubectl -n vif wait --for=delete pod \
  -l "vif.lixenwraith.dev/session=$BATCH_F_PROBE_ID" --timeout=60s
sudo find /var/log/vif-fleet -maxdepth 1 -type f \
  \( -name "$BATCH_F_PROBE_ID.jsonl" \
     -o -name "${BATCH_F_PROBE_ID}_*.jsonl" \) -delete
```

#### F5.6 - retained replay

**The reader and the restart are simultaneous; paste the whole block at once.**
The waiting reader polls only the local listen table, then connects through the
allocator as soon as LogWisp binds, and must receive the exact pre-outage
sentinel from the retained file:

```sh
BATCH_F_RESTART_CAPTURE=$(mktemp \
  /tmp/vif-batch-f-restart.XXXXXX.sse)
(
  for attempt in $(seq 1 3000); do
    if ss -ltnH 'sport = :8081' | grep -q .; then
      exec curl --no-buffer --fail --silent --show-error \
        http://127.0.0.1:9080/vif/api/logs
    fi
    sleep 0.01
  done
  exit 1
) >"$BATCH_F_RESTART_CAPTURE" &
BATCH_F_RESTART_PID=$!
sudo systemctl start logwisp.service

for attempt in $(seq 1 100); do
  grep -Fqx -- "data: $BATCH_F_SENTINEL" \
    "$BATCH_F_RESTART_CAPTURE" && break
  sleep 0.1
done
grep -Fqx -- "data: $BATCH_F_SENTINEL" \
  "$BATCH_F_RESTART_CAPTURE" &&
  printf 'retained replay: exact pre-outage sentinel\n'
BATCH_F_TICK_RESTARTED=$(curl -fsS \
  http://127.0.0.1:9080/vif/api/sessions |
  jq -er --arg id "$SESSION_ID" '
    .sessions[] | select(.id == $id) | .state.tick')
test -n "$BATCH_F_TICK_RESTARTED"
test "$BATCH_F_TICK_RESTARTED" -gt "$BATCH_F_TICK_DOWN" &&
  printf 'game advanced after restart: %s -> %s\n' \
    "$BATCH_F_TICK_DOWN" "$BATCH_F_TICK_RESTARTED"
kill "$BATCH_F_RESTART_PID"
wait "$BATCH_F_RESTART_PID" 2>/dev/null || true
```

#### F5.7 - release the session

**Required before F6, and required even if an earlier sub-step failed.** F6 has
nothing to observe until this deletion happens with LogWisp running: removing the
watched file is the retirement it measures. Quit the remote game first, then run
this here rather than returning to §4:

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
sudo find /var/log/vif-fleet -mindepth 1 -maxdepth 1 -print
unset SESSION_JSON SESSION_ID JOIN_TARGET
```

**Expect:** `true` and `session vacant`, then `true` for the self-tag check, then
`No resources found` and an empty `find`. A missing `session vacant` means the
remote client is still connected. Delete before the empty grace or Job TTL
removes the evidence.

### F6 - watcher retirement

**Purpose.** Prove the pinned LogWisp retires a watched file without logging a
failure. The evidence is F5.7's deletion, so this is only valid if that deletion
happened while the current LogWisp invocation was running: a clean journal from
an invocation that saw no file removed proves nothing, and entries from earlier
invocations belong to the replaced binary. Silence from the pipeline is a pass:

```sh
BATCH_F_INVOCATION=$(systemctl show logwisp.service \
  -p InvocationID --value)
test -n "$BATCH_F_INVOCATION"
! sudo journalctl "_SYSTEMD_INVOCATION_ID=$BATCH_F_INVOCATION" \
  --no-pager | grep -F 'Watcher failed' &&
  printf 'watcher retirement: no failure in this invocation\n'
```

**Expect:** `watcher retirement: no failure in this invocation`.

### F7 - cleanup and steady state

**Purpose.** Return the node to the Batch E steady state and prove the batch left
nothing behind. Remove every Batch F temporary file, then require an empty fleet,
an empty tmpfs, no stream client, and five active units:

```sh
for temporary in \
  "$BATCH_F_HEADERS" "$BATCH_F_CAPTURE" \
  "$BATCH_F_ERROR" "$BATCH_F_RESTART_CAPTURE"
do
  case "$temporary" in
    '') ;;
    /tmp/vif-batch-f-*) rm -f -- "$temporary" ;;
    *) printf 'refusing unsafe temporary path: %s\n' "$temporary" >&2; false ;;
  esac
done

sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo find /var/log/vif-fleet -mindepth 1 -maxdepth 1 -print
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:8081/status |
  jq -e '.server.active_clients == 0'
systemctl is-active \
  'var-log-vif\x2dfleet.mount' \
  vif-fleet-log-cleanup.timer \
  k3s.service vif-allocator.service logwisp.service

unset POST_STATUS BATCH_F_FIRST BATCH_F_HEADERS BATCH_F_CAPTURE
unset BATCH_F_STREAM_PID
unset BATCH_F_SENTINEL BATCH_F_TICK_BEFORE BATCH_F_TICK_DOWN
unset BATCH_F_ERROR BATCH_F_ERROR_STATUS BATCH_F_PROBE_JSON BATCH_F_PROBE_ID
unset BATCH_F_RESTART_CAPTURE BATCH_F_RESTART_PID BATCH_F_TICK_RESTARTED
unset BATCH_F_INVOCATION
```

The Kubernetes query and `find` must print nothing, all five units must be
active, and the stream must have no remaining client. Preserve the Bound PV/PVC
and the `.previous` allocator files until Batch G completes.

### Batch F rollback

Stop allocation and require an empty fleet before restoring the updater's
previous allocator set. LogWisp and the PVC-backed writer remain deployed:

```sh
FLEET_OBJECTS=$(sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet -o name)
test -z "$FLEET_OBJECTS"
sudo test -f /usr/local/libexec/vif-allocator.previous
sudo test -f /etc/vif-allocator/allocator.env.previous
sudo test -f /etc/systemd/system/vif-allocator.service.previous
sudo systemctl stop vif-allocator.service

sudo install -o root -g root -m 0755 \
  /usr/local/libexec/vif-allocator.previous \
  /usr/local/bin/vif-allocator
sudo install -o root -g vif-allocator -m 0640 \
  /etc/vif-allocator/allocator.env.previous \
  /etc/vif-allocator/allocator.env
sudo install -o root -g root -m 0644 \
  /etc/systemd/system/vif-allocator.service.previous \
  /etc/systemd/system/vif-allocator.service
sudo systemctl daemon-reload
sudo systemctl start vif-allocator.service

for attempt in $(seq 1 25); do
  curl --connect-timeout 1 --max-time 2 -fsS \
    http://127.0.0.1:9080/healthz >/dev/null 2>&1 && break
  sleep 1
done
systemctl is-active vif-allocator.service
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz
ROLLBACK_LOG_STATUS=$(curl --connect-timeout 2 --max-time 2 -sS \
  -o /dev/null -w '%{http_code}' \
  http://127.0.0.1:9080/vif/api/logs 2>/dev/null || true)
printf 'restored /vif/api/logs status: %s\n' "$ROLLBACK_LOG_STATUS"
unset FLEET_OBJECTS ROLLBACK_LOG_STATUS
```

The allocator binds its listener after startup reconciliation, so probe only
through the wait loop; a `curl` immediately after `systemctl start` reports a
refused connection that means nothing.

A proxying build answers `200` and then holds the stream open, so that probe is
bounded and its timeout discarded; only the status is read.

`.previous` is one update back, not a fixed version: each
`update-vif-allocator.sh` run overwrites it. It holds the 501 allocator only
until a second update runs, after which this restores the prior proxying build
instead — the printed `/vif/api/logs` status says which. To reach the 501
endpoint again from a node that has updated twice, run the updater from a
checkout at the pre-Batch-F revision. Disabling a future nginx log location is
the matching public rollback.
