#!/bin/sh
# Build the dedicated-session image once, import it into K3s containerd, point
# vif-allocator at it, and remove older vi-fighter tags. This is the manual
# release path until CI delivers the same artifact automatically.
#
# Run as the ordinary repository user (a member of the docker group):
#
#   ./deploy/guest/update-vif-image.sh             # tag from current commit
#   ./deploy/guest/update-vif-image.sh v1.2.3      # explicit release tag
#
# The script intentionally requires an idle fleet. It stops the allocator before
# checking so no session can appear between the check and old-image removal.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
cd "$repo_root"

case ${1:-} in
	-h|--help)
		sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
esac
[ "$#" -le 1 ] || { echo "usage: $0 [TAG]" >&2; exit 2; }

worktree_changes=$(git status --porcelain)
if [ -n "$worktree_changes" ]; then
	echo "$0: the worktree differs from HEAD; commit or clean it before building" >&2
	exit 1
fi

revision=$(git rev-parse HEAD)
tag=${1:-$(git rev-parse --short=8 HEAD)}
case "$tag" in
	''|*[!A-Za-z0-9_.-]*) echo "$0: invalid image tag: $tag" >&2; exit 2 ;;
esac

image_name=${VIF_IMAGE_NAME:-vi-fighter}
runtime_repository=${VIF_RUNTIME_REPOSITORY:-docker.io/library/vi-fighter}
local_image=$image_name:$tag
runtime_image=$runtime_repository:$tag
allocator_env=${VIF_ALLOCATOR_ENV:-/etc/vif-allocator/allocator.env}
allocator_group=${VIF_ALLOCATOR_GROUP:-vif-allocator}

archive=$(mktemp "${TMPDIR:-/tmp}/vif-image.XXXXXX.tar")
env_copy=$(mktemp "${TMPDIR:-/tmp}/vif-allocator-env.XXXXXX")
allocator_was_active=false
if systemctl is-active --quiet vif-allocator.service 2>/dev/null; then
	allocator_was_active=true
fi

restore_forward_policy() {
	policy=$(sudo iptables -S FORWARD | sed -n '1p')
	if [ "$policy" != "-P FORWARD ACCEPT" ]; then
		echo "restoring FORWARD policy to ACCEPT"
		sudo iptables -P FORWARD ACCEPT
	fi
}

cleanup() {
	status=$?
	cleanup_status=0
	trap - EXIT HUP INT TERM
	rm -f "$archive" "$env_copy" "$env_copy.next" || cleanup_status=$?

	# Docker and the distribution containerd are build tools on this node, not
	# workload runtimes. Always restore the established disabled baseline.
	sudo systemctl disable --now docker.service docker.socket containerd.service \
		>/dev/null 2>&1 || cleanup_status=$?
	restore_forward_policy || cleanup_status=$?

	if [ "$allocator_was_active" = true ]; then
		sudo systemctl start vif-allocator.service || cleanup_status=$?
	fi
	if [ "$status" -eq 0 ] && [ "$cleanup_status" -ne 0 ]; then
		status=$cleanup_status
	fi
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

if [ "$allocator_was_active" = true ]; then
	echo "pausing new allocations"
	sudo systemctl stop vif-allocator.service
fi

session_jobs=$(sudo kubectl -n vif get jobs \
	-l app.kubernetes.io/part-of=vi-fighter-fleet -o name)
session_pods=$(sudo kubectl -n vif get pods \
	-l app.kubernetes.io/part-of=vi-fighter-fleet \
	-o name)
if [ -n "$session_jobs$session_pods" ]; then
	echo "$0: session Jobs or pods remain; wait for TTL cleanup or delete them, then rerun" >&2
	printf '%s\n' "$session_jobs" "$session_pods" | sed '/^$/d' >&2
	exit 1
fi

echo "starting Docker for one image build"
sudo systemctl start docker.service
restore_forward_policy

docker build --network host \
	-f deploy/docker/Dockerfile \
	--build-arg VERSION="$tag" \
	--build-arg REVISION="$revision" \
	-t "$local_image" .

# This is the same configuration check run by the pod's init container, without
# giving the scratch image a writable root, capabilities or network access.
docker run --rm --read-only --user 65532:65532 \
	--network none --cap-drop ALL --security-opt no-new-privileges \
	"$local_image" -check -d

echo "importing $runtime_image into K3s containerd"
docker save --output "$archive" "$local_image"
sudo k3s ctr -n k8s.io images import "$archive"
sudo k3s crictl inspecti "$runtime_image" >/dev/null

if sudo test -f "$allocator_env"; then
	sudo cat "$allocator_env" >"$env_copy"
	if ! grep -q '^VIF_ALLOCATOR_IMAGE=' "$env_copy"; then
		echo "$0: $allocator_env has no VIF_ALLOCATOR_IMAGE entry" >&2
		exit 1
	fi
	sed "s|^VIF_ALLOCATOR_IMAGE=.*|VIF_ALLOCATOR_IMAGE=$runtime_image|" \
		"$env_copy" >"$env_copy.next"
	sudo install -o root -g "$allocator_group" -m 0640 "$env_copy.next" "$allocator_env"
	echo "updated $allocator_env"
fi

# Remove named runtime references only after the new image is imported and the
# allocator configuration is updated. Shared content still needed by the new
# image remains in containerd's content store.
old_runtime_images=$(sudo k3s ctr -n k8s.io images list -q | awk -v keep="$runtime_image" '
	/^docker[.]io\/library\/vi-fighter:/ && $0 != keep { print }
')
for old_image in $old_runtime_images; do
	echo "removing old K3s image $old_image"
	sudo k3s crictl rmi "$old_image"
done

old_docker_images=$(docker image ls "$image_name" --format '{{.Repository}}:{{.Tag}}' | awk -v keep="$local_image" '$0 != keep && $0 !~ /:<none>$/')
for old_image in $old_docker_images; do
	echo "removing old Docker image $old_image"
	docker image rm "$old_image"
done

echo "ready: VIF_ALLOCATOR_IMAGE=$runtime_image revision=$revision"
