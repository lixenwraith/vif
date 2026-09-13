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
sudo systemctl stop vif-allocator.service
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo test -x /usr/local/libexec/vif-allocator.batch-b
sudo install -o root -g root -m 0755 \
  /usr/local/libexec/vif-allocator.batch-b \
  /usr/local/bin/vif-allocator
sudo systemctl start vif-allocator.service
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz
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
sudo systemctl stop vif-allocator.service
sudo kubectl -n vif get job,pod,service \
  -l app.kubernetes.io/part-of=vi-fighter-fleet
sudo kubectl -n vif patch role vif-allocator --type=json \
  -p='[{"op":"add","path":"/rules/-","value":{"apiGroups":[""],"resources":["pods/log"],"verbs":["get"]}}]'
sudo systemctl start vif-allocator.service
curl --connect-timeout 2 --max-time 5 -fsS \
  http://127.0.0.1:9080/readyz
```

Do not restore `pods/log` for a node-file, LogWisp, or public-stream failure: none
of those components is allowed to use the Kubernetes log API. A rollback leaves
the live Role different from the repository and must be diagnosed before Batch E.

## Batch E: install the standalone LogWisp service

Batch D must have passed before this section. This first Batch E slice installs
and inspects the independent reader only; it does not change or restart the
allocator, K3s, a session workload, or the PVC. Stop after the status inspection
and record its output before attempting the later simultaneous-session and
outage/replay gates in `doc/kube-todo.md`.

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

The fleet query and `find` must print nothing. Check every source artifact and
the exact 40-character upstream revision before making host changes. A previous
LogWisp installation is not an in-place Batch E upgrade; stop and inspect it if
any of the three destination paths already exists:

```sh
for artifact in \
  deploy/logwisp/REVISION \
  deploy/logwisp/aggregator.toml \
  deploy/guest/logwisp.sysusers \
  deploy/guest/logwisp.service
do
  test -r "$artifact" || {
    printf 'missing Batch E artifact: %s\n' "$artifact" >&2
    false
  }
done

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

The build is intentionally identical on bare Arch Linux and Ubuntu: use the
pinned builder in LogWisp's own Dockerfile rather than depending on the host's Go
minor release. Docker is temporary and returns to the inactive/disabled node
baseline as soon as the binary has been extracted. The cleanup trap accepts only
the `mktemp` directory it created:

```sh
test "$(systemctl is-active docker.service)" = inactive
test "$(systemctl is-active docker.socket)" = inactive
test "$(systemctl is-active containerd.service)" = inactive

LOGWISP_BUILD_DIR=$(mktemp -d)
case "$LOGWISP_BUILD_DIR" in
  /tmp/*) ;;
  *) printf 'unexpected temporary path: %s\n' \
       "$LOGWISP_BUILD_DIR" >&2; false ;;
esac
LOGWISP_IMAGE="local/logwisp-build:$(printf '%.12s' \
  "$LOGWISP_REVISION")"
LOGWISP_CONTAINER=

cleanup_logwisp_build() {
  if test -n "$LOGWISP_CONTAINER"; then
    sudo docker rm -f "$LOGWISP_CONTAINER" >/dev/null 2>&1 || true
  fi
  sudo docker image rm "$LOGWISP_IMAGE" >/dev/null 2>&1 || true
  case "$LOGWISP_BUILD_DIR" in
    /tmp/*) rm -rf -- "$LOGWISP_BUILD_DIR" ;;
    *) return 1 ;;
  esac
  sudo systemctl disable --now \
    docker.socket docker.service containerd.service
}
trap cleanup_logwisp_build EXIT HUP INT TERM

git clone https://github.com/lixenwraith/logwisp.git \
  "$LOGWISP_BUILD_DIR/source"
git -C "$LOGWISP_BUILD_DIR/source" checkout \
  --detach "$LOGWISP_REVISION"
test "$(git -C "$LOGWISP_BUILD_DIR/source" rev-parse HEAD)" = \
  "$LOGWISP_REVISION"

sudo systemctl start docker.service
sudo docker build --pull \
  --build-arg VERSION=v0.18.0 \
  --build-arg REVISION="$LOGWISP_REVISION" \
  -t "$LOGWISP_IMAGE" \
  "$LOGWISP_BUILD_DIR/source"
LOGWISP_CONTAINER=$(sudo docker create "$LOGWISP_IMAGE")
sudo docker cp "$LOGWISP_CONTAINER:/logwisp" \
  "$LOGWISP_BUILD_DIR/logwisp"
test -x "$LOGWISP_BUILD_DIR/logwisp"
"$LOGWISP_BUILD_DIR/logwisp" --version

sudo install -o root -g root -m 0755 \
  "$LOGWISP_BUILD_DIR/logwisp" /usr/local/bin/logwisp

cleanup_logwisp_build
trap - EXIT HUP INT TERM
unset -f cleanup_logwisp_build
unset LOGWISP_BUILD_DIR LOGWISP_IMAGE LOGWISP_CONTAINER

test "$(systemctl is-active docker.service)" = inactive
test "$(systemctl is-active docker.socket)" = inactive
test "$(systemctl is-active containerd.service)" = inactive
```

Install the dedicated locked identity, root-owned configuration, and unit. The
service's supplementary `vif-fleet` group is used only to read the tmpfs; the
unit gives the process a read-only mount view and hides K3s and allocator
credential paths:

```sh
sudo install -D -o root -g root -m 0644 \
  deploy/guest/logwisp.sysusers \
  /etc/sysusers.d/logwisp.conf
sudo systemd-sysusers /etc/sysusers.d/logwisp.conf
getent passwd logwisp
getent group logwisp

sudo install -d -o root -g root -m 0755 /etc/logwisp
sudo install -o root -g root -m 0644 \
  deploy/logwisp/aggregator.toml \
  /etc/logwisp/vif-fleet.toml
sudo install -o root -g root -m 0644 \
  deploy/guest/logwisp.service \
  /etc/systemd/system/logwisp.service
sudo systemctl daemon-reload
sudo systemctl enable --now logwisp.service
```

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

The host `findmnt` output remains `rw`; the view inside LogWisp's mount namespace
must contain `ro`. `RequiresMountsFor=` may add a mount requirement, but `Requires`,
`Wants`, and `After` must not name `k3s.service` or `vif-allocator.service`.
The status JSON and journal are the evidence needed to select exact watcher and
replay assertions for the next live slice.

### Batch E initial-install rollback

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
