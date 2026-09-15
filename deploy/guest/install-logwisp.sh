#!/bin/sh
# Build the pinned LogWisp revision, install its standalone host service, and
# initialize it. Run from any directory; an existing checkout is optional.
#
#   ./deploy/guest/install-logwisp.sh
#   ./deploy/guest/install-logwisp.sh /path/to/logwisp
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
case ${1:-} in -h|--help) sed -n '2,6p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;; esac
[ "$#" -le 1 ] || { echo "usage: $0 [LOGWISP_CHECKOUT]" >&2; exit 2; }

config_file=$repo_root/deploy/logwisp/aggregator.toml
sysusers_file=$repo_root/deploy/guest/logwisp.sysusers
unit_file=$repo_root/deploy/guest/logwisp.service
builder=$repo_root/deploy/guest/build-logwisp.sh
for artifact in "$config_file" "$sysusers_file" "$unit_file" "$builder"; do
	[ -r "$artifact" ] || { echo "$0: missing artifact: $artifact" >&2; exit 1; }
done
[ -x "$builder" ] || { echo "$0: builder is not executable: $builder" >&2; exit 1; }

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
	echo "$0: the vif-fleet identity is missing; install the log tmpfs first" >&2
	exit 1
}

stage_root=$(mktemp -d "${TMPDIR:-/tmp}/vif-logwisp-install.XXXXXX")
cleanup() {
	status=$?
	trap - EXIT HUP INT TERM
	case "$stage_root" in
		*/vif-logwisp-install.*) rm -rf -- "$stage_root" ;;
		*) echo "$0: refusing unsafe cleanup path: $stage_root" >&2; status=1 ;;
	esac
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

if [ "$#" -eq 1 ]; then "$builder" "$stage_root/logwisp" "$1"; else "$builder" "$stage_root/logwisp"; fi

sudo install -o root -g root -m 0755 "$stage_root/logwisp" /usr/local/bin/logwisp
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
for attempt in $(seq 1 50); do
	curl --connect-timeout 1 --max-time 2 -fsS http://127.0.0.1:8081/status \
		>/dev/null 2>&1 && break
	sleep 0.1
done
systemctl is-active --quiet logwisp.service
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:8081/status |
	jq -e '
		.server.client_buffer_size == 512 and
		.server.max_connections == 32 and
		.server.write_timeout_ms == 5000' >/dev/null
echo "installed $(/usr/local/bin/logwisp --version)"
