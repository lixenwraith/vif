#!/bin/sh
# Build LogWisp from a fresh clone, at REVISION or upstream main's head, into one
# caller-owned output path, then restore the disabled Docker baseline.
#
#   build-logwisp.sh OUTPUT [REVISION]
set -eu

case ${1:-} in
	-h|--help)
		sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	'')
		sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//' >&2
		exit 2
		;;
esac
[ "$#" -le 2 ] || { echo "usage: $0 OUTPUT [REVISION]" >&2; exit 2; }

output=$1
case "$output" in /*) ;; *) output=$(pwd)/$output ;; esac
[ ! -e "$output" ] || { echo "$0: refusing to replace output: $output" >&2; exit 1; }
[ -d "$(dirname -- "$output")" ] || {
	echo "$0: output directory does not exist: $(dirname -- "$output")" >&2
	exit 1
}

revision=${2:-}
[ -z "$revision" ] || printf '%s\n' "$revision" | grep -Eq '^[0-9a-f]{40}$' || {
	echo "$0: invalid LogWisp revision: $revision" >&2
	exit 1
}

# LogWisp's Dockerfile needs BuildKit, and buildx reaches it with no fallback to the
# legacy builder; checked before Docker starts.
sudo docker buildx version >/dev/null || {
	echo "$0: Docker has no buildx plugin, which LogWisp's build needs; install docker-buildx" >&2
	exit 1
}

for build_unit in docker.service docker.socket containerd.service; do
	if systemctl is-active --quiet "$build_unit"; then
		echo "$0: $build_unit must be inactive before the temporary build" >&2
		exit 1
	fi
	if systemctl is-enabled --quiet "$build_unit"; then
		echo "$0: $build_unit must be disabled before the temporary build" >&2
		exit 1
	fi
done

tmp_parent=${TMPDIR:-/tmp}
tmp_parent=$(CDPATH= cd -- "$tmp_parent" && pwd)
build_root=$(mktemp -d "$tmp_parent/vif-logwisp-build.XXXXXX")
build_source=$build_root/source
container=
docker_started=false
output_complete=false

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
	if [ "$docker_started" = true ]; then
		if [ -n "$container" ]; then
			sudo docker rm -f "$container" >/dev/null 2>&1 || cleanup_status=$?
		fi
		# Everything, not just the image: --pull leaves each earlier base behind, and
		# BuildKit its cache.
		sudo docker system prune --all --force >/dev/null 2>&1 || cleanup_status=$?
	fi
	case "$build_root" in
		"$tmp_parent"/vif-logwisp-build.*) rm -rf -- "$build_root" || cleanup_status=$? ;;
		*) echo "$0: refusing unsafe cleanup path: $build_root" >&2; cleanup_status=1 ;;
	esac
	if [ "$output_complete" != true ]; then
		rm -f -- "$output" || cleanup_status=$?
	fi
	sudo systemctl disable docker.service docker.socket containerd.service \
		>/dev/null 2>&1 || cleanup_status=$?
	sudo systemctl stop docker.socket >/dev/null 2>&1 || cleanup_status=$?
	sudo systemctl stop docker.service containerd.service \
		>/dev/null 2>&1 || cleanup_status=$?
	restore_forward_policy || cleanup_status=$?
	if [ "$status" -eq 0 ] && [ "$cleanup_status" -ne 0 ]; then status=$cleanup_status; fi
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

# A squash-merged pull-request head, or a commit a rewritten main dropped, can
# still resolve, so only ancestry of freshly fetched main proves a revision.
git clone --quiet --no-checkout https://github.com/lixenwraith/logwisp.git "$build_source"
upstream_main=$(git -C "$build_source" rev-parse origin/main)
revision=${revision:-$upstream_main}
git -C "$build_source" merge-base --is-ancestor "$revision" "$upstream_main" 2>/dev/null || {
	echo "$0: revision is not on LogWisp main: $revision" >&2
	echo "$0: upstream main is $upstream_main" >&2
	exit 1
}
git -C "$build_source" checkout --quiet --detach "$revision"
image=local/logwisp-build:$(printf '%.12s' "$revision")

[ "$(git -C "$build_source" rev-parse HEAD)" = "$revision" ]
version=$(git -C "$build_source" describe --tags --always)
echo "building LogWisp $version at revision $revision"
sudo systemctl start docker.service
docker_started=true
restore_forward_policy
sudo docker buildx build --load --pull \
	--build-arg VERSION="$version" \
	--build-arg REVISION="$revision" \
	-t "$image" "$build_source"
container=$(sudo docker create "$image")
# The image ships /lw since LogWisp renamed its binary, /logwisp before
sudo docker cp "$container:/lw" "$output" 2>/dev/null ||
	sudo docker cp "$container:/logwisp" "$output"
[ -x "$output" ]
"$output" --version | grep -F "$revision"
output_complete=true
echo "built LogWisp $version at revision $revision"
