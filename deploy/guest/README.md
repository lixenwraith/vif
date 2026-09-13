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
      curl git jq make python util-linux iptables-nft conntrack-tools \
      ethtool tcpdump nftables docker
    ;;
  ubuntu)
    sudo apt-get update
    sudo apt-get install -y \
      ca-certificates curl git jq make python3 util-linux iptables \
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

Stop the allocator before the final empty-fleet check, then capture the single
K3s node name without placing it in the repository. If any later step stops
before the allocator is restored, keep it stopped while diagnosing the node:

```sh
sudo systemctl stop vif-allocator.service
test "$(systemctl is-active vif-allocator.service)" = inactive
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
sudo systemctl restart vif-allocator-token.service
sudo systemctl start vif-allocator.service vif-fleet-log-cleanup.timer
```

Verify the mount and dependency before creating Kubernetes storage objects:

```sh
systemctl is-active "$MOUNT_UNIT" k3s.service vif-allocator.service
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
VIF_IMAGE=$(sudo sed -n \
  's/^VIF_ALLOCATOR_IMAGE=//p' /etc/vif-allocator/allocator.env)
test -n "$VIF_IMAGE"
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

Both final commands must print nothing. Finish Batch B with the common session
check in `doc/kube-todo.md` §5; the real workload still writes stdout until Batch
C.

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
sudo systemctl restart vif-allocator-token.service
sudo systemctl start vif-allocator.service
```

Keep the Retain PV/PVC for diagnosis. Do not schedule a PVC writer after rollback;
either restore the mount/drop-in or deliberately remove the unused claim and PV
before retrying Batch B.
