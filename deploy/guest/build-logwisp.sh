#!/bin/sh
# Build the pinned LogWisp revision, which must be reachable from upstream main,
# into one caller-owned output path, then restore the disabled Docker baseline.
#
#   build-logwisp.sh OUTPUT [LOGWISP_CHECKOUT]
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)

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
[ "$#" -le 2 ] || { echo "usage: $0 OUTPUT [LOGWISP_CHECKOUT]" >&2; exit 2; }

output=$1
case "$output" in /*) ;; *) output=$(pwd)/$output ;; esac
[ ! -e "$output" ] || { echo "$0: refusing to replace output: $output" >&2; exit 1; }
[ -d "$(dirname -- "$output")" ] || {
	echo "$0: output directory does not exist: $(dirname -- "$output")" >&2
	exit 1
}

revision_file=$repo_root/deploy/logwisp/REVISION
[ -r "$revision_file" ] || { echo "$0: missing artifact: $revision_file" >&2; exit 1; }
revision=$(tr -d '[:space:]' <"$revision_file")
printf '%s\n' "$revision" | grep -Eq '^[0-9a-f]{40}$' || {
	echo "$0: invalid LogWisp revision: $revision" >&2
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
image=local/logwisp-build:$(printf '%.12s' "$revision")
container=
docker_started=false
source_repository=
worktree_added=false
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
		sudo docker image rm "$image" >/dev/null 2>&1 || cleanup_status=$?
	fi
	if [ "$worktree_added" = true ]; then
		git -C "$source_repository" worktree remove --force "$build_source" \
			>/dev/null 2>&1 || cleanup_status=$?
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

# A squash-merged pull-request head stops being reachable while a stale local
# object still resolves, so only ancestry of freshly fetched upstream main
# proves the pin. Fetching main last leaves it alone in FETCH_HEAD.
assert_revision_on_upstream_main() {
	repository=$1
	git -C "$repository" fetch --quiet --tags origin ||
		{ echo "$0: cannot fetch LogWisp tags" >&2; exit 1; }
	git -C "$repository" fetch --quiet origin main ||
		{ echo "$0: cannot fetch LogWisp main" >&2; exit 1; }
	upstream_main=$(git -C "$repository" rev-parse FETCH_HEAD)
	git -C "$repository" cat-file -e "$revision^{commit}" 2>/dev/null || {
		echo "$0: pinned revision is absent from LogWisp main: $revision" >&2
		echo "$0: upstream main is $upstream_main" >&2
		echo "$0: repin deploy/logwisp/REVISION to a commit on upstream main" >&2
		exit 1
	}
	git -C "$repository" merge-base --is-ancestor "$revision" "$upstream_main" || {
		echo "$0: pinned revision is not an ancestor of LogWisp main: $revision" >&2
		echo "$0: upstream main is $upstream_main" >&2
		echo "$0: repin deploy/logwisp/REVISION to a commit on upstream main" >&2
		exit 1
	}
}

if [ "$#" -eq 2 ]; then
	source_repository=$(CDPATH= cd -- "$2" && pwd)
	git -C "$source_repository" rev-parse --git-dir >/dev/null
	origin_url=$(git -C "$source_repository" config --get remote.origin.url)
	case "$origin_url" in
		https://github.com/lixenwraith/logwisp|https://github.com/lixenwraith/logwisp.git|\
		git@github.com:lixenwraith/logwisp|git@github.com:lixenwraith/logwisp.git) ;;
		*) echo "$0: unexpected LogWisp origin: $origin_url" >&2; exit 1 ;;
	esac
	assert_revision_on_upstream_main "$source_repository"
	git -C "$source_repository" worktree add --detach "$build_source" "$revision"
	worktree_added=true
else
	git clone --no-checkout https://github.com/lixenwraith/logwisp.git "$build_source"
	assert_revision_on_upstream_main "$build_source"
	git -C "$build_source" checkout --detach "$revision"
fi

[ "$(git -C "$build_source" rev-parse HEAD)" = "$revision" ]
version=$(git -C "$build_source" describe --tags --always)
echo "building LogWisp $version at revision $revision"
sudo systemctl start docker.service
docker_started=true
restore_forward_policy
sudo docker build --pull \
	--build-arg VERSION="$version" \
	--build-arg REVISION="$revision" \
	-t "$image" "$build_source"
container=$(sudo docker create "$image")
sudo docker cp "$container:/logwisp" "$output"
[ -x "$output" ]
"$output" --version | grep -F "$revision"
output_complete=true
echo "built LogWisp $version at revision $revision"
