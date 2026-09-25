#!/bin/sh
# Build and deploy vif's pinned standalone LogWisp without controlling
# K3s or the allocator. The fleet procedure separately enforces its empty-fleet
# maintenance gate. Retains one known-good binary/config/unit.
#
#   ./deploy/guest/update-logwisp.sh
#   ./deploy/guest/update-logwisp.sh /path/to/logwisp
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
case ${1:-} in -h|--help) sed -n '2,6p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;; esac
[ "$#" -le 1 ] || { echo "usage: $0 [LOGWISP_CHECKOUT]" >&2; exit 2; }

revision_file=$repo_root/deploy/logwisp/REVISION
config_file=$repo_root/deploy/logwisp/aggregator.toml
unit_file=$repo_root/deploy/guest/logwisp.service
builder=$repo_root/deploy/guest/build-logwisp.sh
binary=/usr/local/bin/logwisp
installed_config=/etc/logwisp/vif-fleet.toml
installed_unit=/etc/systemd/system/logwisp.service
backup_binary=/usr/local/libexec/logwisp.previous
backup_config=/etc/logwisp/vif-fleet.toml.previous
backup_unit=/etc/systemd/system/logwisp.service.previous

for artifact in "$revision_file" "$config_file" "$unit_file" "$builder"; do
	[ -r "$artifact" ] || { echo "$0: missing artifact: $artifact" >&2; exit 1; }
done
revision=$(tr -d '[:space:]' <"$revision_file")
for installed in "$binary" "$installed_config" "$installed_unit"; do
	[ -f "$installed" ] || { echo "$0: missing installed file: $installed" >&2; exit 1; }
done
[ -x "$builder" ] || { echo "$0: builder is not executable: $builder" >&2; exit 1; }
systemctl is-active --quiet logwisp.service || {
	echo "$0: logwisp.service must be active before an update" >&2
	exit 1
}

stage_root=$(mktemp -d "${TMPDIR:-/tmp}/vif-logwisp-update.XXXXXX")
rollback_required=false
rollback() {
	echo "$0: update failed; restoring previous LogWisp"
	sudo systemctl stop logwisp.service >/dev/null 2>&1 || true
	sudo install -o root -g root -m 0755 "$backup_binary" "$binary"
	sudo install -o root -g root -m 0644 "$backup_config" "$installed_config"
	sudo install -o root -g root -m 0644 "$backup_unit" "$installed_unit"
	sudo systemctl daemon-reload
	sudo systemctl start logwisp.service
}
cleanup() {
	status=$?
	trap - EXIT HUP INT TERM
	if [ "$status" -ne 0 ] && [ "$rollback_required" = true ]; then rollback || status=1; fi
	case "$stage_root" in
		*/vif-logwisp-update.*) rm -rf -- "$stage_root" || status=1 ;;
		*) echo "$0: refusing unsafe cleanup path: $stage_root" >&2; status=1 ;;
	esac
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

if [ "$#" -eq 1 ]; then "$builder" "$stage_root/logwisp" "$1"; else "$builder" "$stage_root/logwisp"; fi

sudo install -D -o root -g root -m 0755 "$binary" "$backup_binary"
sudo install -o root -g root -m 0644 "$installed_config" "$backup_config"
sudo install -o root -g root -m 0644 "$installed_unit" "$backup_unit"
rollback_required=true

echo "restarting only logwisp.service; retained files will replay"
sudo systemctl stop logwisp.service
sudo install -o root -g root -m 0755 "$stage_root/logwisp" "$binary"
sudo install -o root -g root -m 0644 "$config_file" "$installed_config"
sudo install -o root -g root -m 0644 "$unit_file" "$installed_unit"
sudo systemctl daemon-reload
sudo systemctl start logwisp.service
for attempt in $(seq 1 50); do
	curl --connect-timeout 1 --max-time 2 -fsS http://127.0.0.1:8081/status \
		>/dev/null 2>&1 && break
	sleep 0.1
done
systemctl is-active --quiet logwisp.service
"$binary" --version | grep -F "$revision"
# Read from the configuration just installed rather than restated here: a second
# copy of these bounds drifts the first time one of them is tuned, and the check
# then rejects the build that carries the new value.
bounds=$(awk '
	BEGIN { printf "{" }
	$2 == "=" && $1 ~ /^(client_buffer_size|max_connections|write_timeout_ms)$/ {
		printf "%s\"%s\":%s", separator, $1, $3
		separator = ","
		found++
	}
	END { printf "}"; if (found != 3) exit 1 }
' "$config_file") || {
	echo "$0: cannot read the sink bounds from $config_file" >&2
	exit 1
}
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:8081/status |
	jq -e --argjson bounds "$bounds" \
		'.server as $served | all($bounds | to_entries[]; $served[.key] == .value)' >/dev/null || {
	echo "$0: the served stream bounds are not $bounds" >&2
	exit 1
}
sudo cmp -s "$config_file" "$installed_config"
sudo cmp -s "$unit_file" "$installed_unit"
rollback_required=false
echo "updated $("$binary" --version)"
