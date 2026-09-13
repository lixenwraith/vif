#!/bin/sh
# Build the exact LogWisp revision selected by vi-fighter, install its standalone
# host service, and restore the node's disabled Docker/containerd baseline.
#
# Run from any directory. An existing LogWisp checkout may be supplied without
# changing its current branch or worktree:
#
#   ./deploy/guest/install-logwisp.sh
#   ./deploy/guest/install-logwisp.sh /path/to/logwisp
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)

case ${1:-} in
	-h|--help)
		sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
esac
[ "$#" -le 1 ] || { echo "usage: $0 [LOGWISP_CHECKOUT]" >&2; exit 2; }

revision_file=$repo_root/deploy/logwisp/REVISION
config_file=$repo_root/deploy/logwisp/aggregator.toml
sysusers_file=$repo_root/deploy/guest/logwisp.sysusers
unit_file=$repo_root/deploy/guest/logwisp.service

for artifact in "$revision_file" "$config_file" "$sysusers_file" "$unit_file"; do
	[ -r "$artifact" ] || { echo "$0: missing artifact: $artifact" >&2; exit 1; }
done

revision=$(tr -d '[:space:]' <"$revision_file")
printf '%s\n' "$revision" | grep -Eq '^[0-9a-f]{40}$' || {
	echo "$0: invalid LogWisp revision: $revision" >&2
	exit 1
}

for destination in \
	/usr/local/bin/logwisp \
	/etc/logwisp/vif-fleet.toml \
	/etc/systemd/system/logwisp.service
do
	[ ! -e "$destination" ] || {
		echo "$0: refusing to replace existing path: $destination" >&2
		exit 1
	}
done

getent group vif-fleet >/dev/null || {
	echo "$0: the Batch B vif-fleet identity is missing" >&2
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
build_root=$(mktemp -d "$tmp_parent/vif-logwisp.XXXXXX")
build_source=$build_root/source
image=local/logwisp-build:$(printf '%.12s' "$revision")
container=
docker_started=false
source_repository=
worktree_added=false

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
		"$tmp_parent"/vif-logwisp.*) rm -rf -- "$build_root" || cleanup_status=$? ;;
		*) echo "$0: refusing unsafe cleanup path: $build_root" >&2; cleanup_status=1 ;;
	esac

	sudo systemctl disable docker.service docker.socket containerd.service \
		>/dev/null 2>&1 || cleanup_status=$?
	sudo systemctl stop docker.socket >/dev/null 2>&1 || cleanup_status=$?
	sudo systemctl stop docker.service containerd.service \
		>/dev/null 2>&1 || cleanup_status=$?
	restore_forward_policy || cleanup_status=$?

	if [ "$status" -eq 0 ] && [ "$cleanup_status" -ne 0 ]; then
		status=$cleanup_status
	fi
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

if [ "$#" -eq 1 ]; then
	source_repository=$(CDPATH= cd -- "$1" && pwd)
	git -C "$source_repository" rev-parse --git-dir >/dev/null
	origin_url=$(git -C "$source_repository" config --get remote.origin.url)
	case "$origin_url" in
		https://github.com/lixenwraith/logwisp|https://github.com/lixenwraith/logwisp.git|\
		git@github.com:lixenwraith/logwisp|git@github.com:lixenwraith/logwisp.git) ;;
		*) echo "$0: unexpected LogWisp origin: $origin_url" >&2; exit 1 ;;
	esac
	if ! git -C "$source_repository" cat-file -e "$revision^{commit}" 2>/dev/null; then
		git -C "$source_repository" fetch --tags origin main
	fi
	[ "$(git -C "$source_repository" rev-parse "$revision^{commit}")" = "$revision" ]
	git -C "$source_repository" worktree add --detach "$build_source" "$revision"
	worktree_added=true
else
	git clone --no-checkout https://github.com/lixenwraith/logwisp.git "$build_source"
	git -C "$build_source" checkout --detach "$revision"
fi

[ "$(git -C "$build_source" rev-parse HEAD)" = "$revision" ]
echo "building LogWisp revision $revision"

sudo systemctl start docker.service
docker_started=true
restore_forward_policy

sudo docker build --pull \
	--build-arg VERSION=v0.18.0 \
	--build-arg REVISION="$revision" \
	-t "$image" "$build_source"
container=$(sudo docker create "$image")
sudo docker cp "$container:/logwisp" "$build_root/logwisp"
[ -x "$build_root/logwisp" ]
"$build_root/logwisp" --version

sudo install -o root -g root -m 0755 "$build_root/logwisp" /usr/local/bin/logwisp
sudo install -D -o root -g root -m 0644 "$sysusers_file" /etc/sysusers.d/logwisp.conf
sudo systemd-sysusers /etc/sysusers.d/logwisp.conf

logwisp_shell=$(getent passwd logwisp | awk -F: '{print $7}')
case "$logwisp_shell" in
	*/nologin|*/false) ;;
	*) echo "$0: logwisp is not a locked service identity" >&2; exit 1 ;;
esac

sudo install -d -o root -g root -m 0755 /etc/logwisp
sudo install -o root -g root -m 0644 "$config_file" /etc/logwisp/vif-fleet.toml
sudo install -o root -g root -m 0644 "$unit_file" /etc/systemd/system/logwisp.service
sudo systemctl daemon-reload
sudo systemctl enable --now logwisp.service
systemctl is-active --quiet logwisp.service

echo "installed LogWisp revision $revision"
