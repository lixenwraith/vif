#!/bin/sh
# Build the session image, import it into K3s pinned, point vif-router at it and
# remove older vif tags; allocation pauses meanwhile and the fleet must be empty.
# `./deploy/update.sh image` runs it. Run as a docker-group user, with an optional tag:
#
#   ./deploy/guest/update-vif-image.sh [TAG]       # default: the commit's short hash
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
cd "$repo_root"

case ${1:-} in
	-h|--help)
		sed -n '2,6p' "$0" | sed 's/^# \{0,1\}//'
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

# The Dockerfile's cache mounts need BuildKit, which buildx reaches with no fallback
# to the legacy builder; checked before allocation pauses.
docker buildx version >/dev/null || {
	echo "$0: Docker has no buildx plugin, which this build needs; install docker-buildx" >&2
	exit 1
}

image_name=${VIF_IMAGE_NAME:-vif}
runtime_repository=${VIF_RUNTIME_REPOSITORY:-docker.io/library/vif}
local_image=$image_name:$tag
runtime_image=$runtime_repository:$tag
router_env=${VIF_ROUTER_ENV:-/etc/vif-router/router.env}
router_group=${VIF_ROUTER_GROUP:-vif-router}
build_cache_limit=${VIF_BUILD_CACHE_LIMIT:-3gb}

archive=$(mktemp "${TMPDIR:-/tmp}/vif-image.XXXXXX.tar")
env_copy=$(mktemp "${TMPDIR:-/tmp}/vif-router-env.XXXXXX")
router_was_active=false
if systemctl is-active --quiet vif-router.service 2>/dev/null; then
	router_was_active=true
fi
# A Docker already running serves something else on this host: build with it,
# leave it and the FORWARD policy as they were, and take only this image back out.
docker_running=false
if systemctl is-active --quiet docker.service docker.socket 2>/dev/null; then
	docker_running=true
fi
docker_started=false
image_built=false

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

	if [ "$image_built" = true ]; then
		docker image rm "$local_image" >/dev/null || cleanup_status=$?
	fi
	# The store keeps the base and the Go caches for the next build, shedding the
	# least recently used past the cap; a Docker serving other work keeps its own.
	if [ "$docker_started" = true ]; then
		docker buildx prune --force --max-used-space "$build_cache_limit" \
			>/dev/null || cleanup_status=$?
	fi
	# Docker and the distribution containerd are build tools on this node, not
	# workload runtimes. Restore the established disabled baseline.
	if [ "$docker_running" = false ]; then
		sudo systemctl disable --now docker.service docker.socket containerd.service \
			>/dev/null 2>&1 || cleanup_status=$?
		restore_forward_policy || cleanup_status=$?
	fi

	if [ "$router_was_active" = true ]; then
		sudo systemctl start vif-router.service || cleanup_status=$?
	fi
	if [ "$status" -eq 0 ] && [ "$cleanup_status" -ne 0 ]; then
		status=$cleanup_status
	fi
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

if [ "$router_was_active" = true ]; then
	echo "pausing new allocations"
	sudo systemctl stop vif-router.service
fi

if ! "$repo_root/deploy/k3s/session.sh" blockers; then
	echo "$0: the old image cannot be removed while a fleet object still names it" >&2
	exit 1
fi

if [ "$docker_running" = true ]; then
	echo "building with the Docker already running, which stays running"
else
	echo "starting Docker for one image build"
	sudo systemctl start docker.service
	docker_started=true
	restore_forward_policy
fi

docker buildx build --load --network host \
	-f deploy/docker/Dockerfile \
	--build-arg VERSION="$tag" \
	--build-arg REVISION="$revision" \
	-t "$local_image" .
image_built=true

image_profile=$(docker image inspect \
	--format '{{ index .Config.Labels "dev.lixenwraith.vif.build-profile" }}' \
	"$local_image")
if [ "$image_profile" != headless ]; then
	echo "$0: refusing image with build profile '$image_profile' (expected headless)" >&2
	exit 1
fi

# This is the same configuration check run by the pod's init container, without
# giving the scratch image a writable root, capabilities or network access.
docker run --rm --read-only --user 65532:65532 \
	--network none --cap-drop ALL --security-opt no-new-privileges \
	"$local_image" -check -d

echo "importing $runtime_image into K3s containerd"
docker save --output "$archive" "$local_image"
sudo k3s ctr -n k8s.io images import "$archive"
sudo k3s crictl inspecti "$runtime_image" >/dev/null
# Pinned, because nothing here can pull it back: the kubelet deletes unused images
# once the disk passes 85%, and between matches no pod uses this one.
sudo k3s ctr -n k8s.io images label "$runtime_image" io.cri-containerd.pinned=pinned >/dev/null

if sudo test -f "$router_env"; then
	sudo cat "$router_env" >"$env_copy"
	# The one setting deploy/guest/vif-router.env does not carry.
	{ grep -v '^VIF_ROUTER_IMAGE=' "$env_copy" || true
	  printf 'VIF_ROUTER_IMAGE=%s\n' "$runtime_image"; } >"$env_copy.next"
	sudo install -o root -g "$router_group" -m 0640 "$env_copy.next" "$router_env"
	echo "updated $router_env"
fi

# Remove named runtime references, a leftover vif-ws-bridge tag among them, only
# after the new image is imported and the router configuration is updated.
# Shared content still needed by the new image remains in containerd's store.
old_runtime_images=$(sudo k3s ctr -n k8s.io images list -q | awk -v keep="$runtime_image" '
	/^docker[.]io\/library\/vif(-ws-bridge)?:/ && $0 != keep { print }
')
for old_image in $old_runtime_images; do
	echo "removing old K3s image $old_image"
	sudo k3s crictl rmi "$old_image"
done

echo "ready: VIF_ROUTER_IMAGE=$runtime_image revision=$revision"
