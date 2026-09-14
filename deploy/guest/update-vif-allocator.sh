#!/bin/sh
# Build and deploy the allocator from this vi-fighter revision. New allocations
# pause during the short cutover, and one known-good binary/config/unit is kept.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
case ${1:-} in
	-h|--help) sed -n '2,3p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	'') ;;
	*) echo "usage: $0" >&2; exit 2 ;;
esac

binary=/usr/local/bin/vif-allocator
installed_env=/etc/vif-allocator/allocator.env
installed_unit=/etc/systemd/system/vif-allocator.service
source_unit=$repo_root/deploy/guest/vif-allocator.service
backup_binary=/usr/local/libexec/vif-allocator.previous
backup_env=/etc/vif-allocator/allocator.env.previous
backup_unit=/etc/systemd/system/vif-allocator.service.previous

test "$(git -C "$repo_root" rev-parse --show-toplevel)" = "$repo_root"
if [ -n "$(git -C "$repo_root" status --porcelain)" ]; then
	echo "$0: the vi-fighter worktree differs from HEAD" >&2
	exit 1
fi
# /etc/vif-allocator is root:vif-allocator 0750, so an operator account cannot
# stat inside it and an installed env file would read as missing.
for installed in "$binary" "$installed_env" "$installed_unit"; do
	sudo test -f "$installed" ||
		{ echo "$0: missing installed file: $installed" >&2; exit 1; }
done
[ -r "$source_unit" ] || { echo "$0: missing unit: $source_unit" >&2; exit 1; }
systemctl is-active --quiet vif-allocator.service || {
	echo "$0: vif-allocator.service must be active before an update" >&2
	exit 1
}

make -C "$repo_root" allocator
[ -x "$repo_root/bin/vif-allocator" ]

stage_root=$(mktemp -d "${TMPDIR:-/tmp}/vif-allocator-update.XXXXXX")
allocator_stopped=false
rollback_required=false
rollback() {
	echo "$0: update failed; restoring previous allocator"
	sudo systemctl stop vif-allocator.service >/dev/null 2>&1 || true
	sudo install -o root -g root -m 0755 "$backup_binary" "$binary"
	sudo install -o root -g vif-allocator -m 0640 "$backup_env" "$installed_env"
	sudo install -o root -g root -m 0644 "$backup_unit" "$installed_unit"
	sudo systemctl daemon-reload
	sudo systemctl start vif-allocator.service
}
cleanup() {
	status=$?
	trap - EXIT HUP INT TERM
	if [ "$status" -ne 0 ]; then
		if [ "$rollback_required" = true ]; then
			rollback || status=1
		elif [ "$allocator_stopped" = true ]; then
			sudo systemctl start vif-allocator.service || status=1
		fi
	fi
	case "$stage_root" in
		*/vif-allocator-update.*) rm -rf -- "$stage_root" || status=1 ;;
		*) echo "$0: refusing unsafe cleanup path: $stage_root" >&2; status=1 ;;
	esac
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

sudo cat "$installed_env" >"$stage_root/allocator.env"
if grep -q '^VIF_ALLOCATOR_LOG_STREAM_URL=' "$stage_root/allocator.env"; then
	sed 's|^VIF_ALLOCATOR_LOG_STREAM_URL=.*|VIF_ALLOCATOR_LOG_STREAM_URL=http://127.0.0.1:8081/stream|' \
		"$stage_root/allocator.env" >"$stage_root/allocator.env.next"
else
	cp "$stage_root/allocator.env" "$stage_root/allocator.env.next"
	printf '%s\n' 'VIF_ALLOCATOR_LOG_STREAM_URL=http://127.0.0.1:8081/stream' \
		>>"$stage_root/allocator.env.next"
fi

fleet_objects=$(sudo kubectl -n vif get job,pod,service \
	-l app.kubernetes.io/part-of=vi-fighter-fleet -o name)
if [ -n "$fleet_objects" ]; then
	echo "$0: the fleet must be empty before the allocator update" >&2
	printf '%s\n' "$fleet_objects" >&2
	exit 1
fi

echo "pausing new allocations for the allocator cutover"
sudo systemctl stop vif-allocator.service
allocator_stopped=true
fleet_objects=$(sudo kubectl -n vif get job,pod,service \
	-l app.kubernetes.io/part-of=vi-fighter-fleet -o name)
if [ -n "$fleet_objects" ]; then
	echo "$0: a fleet object remained after allocation stopped" >&2
	printf '%s\n' "$fleet_objects" >&2
	exit 1
fi

sudo install -D -o root -g root -m 0755 "$binary" "$backup_binary"
sudo install -o root -g vif-allocator -m 0640 "$installed_env" "$backup_env"
sudo install -o root -g root -m 0644 "$installed_unit" "$backup_unit"
rollback_required=true

sudo install -o root -g root -m 0755 "$repo_root/bin/vif-allocator" "$binary"
sudo install -o root -g vif-allocator -m 0640 \
	"$stage_root/allocator.env.next" "$installed_env"
sudo install -o root -g root -m 0644 "$source_unit" "$installed_unit"
sudo systemctl daemon-reload
sudo systemctl start vif-allocator.service

for attempt in $(seq 1 25); do
	curl --connect-timeout 1 --max-time 2 -fsS http://127.0.0.1:9080/healthz \
		>/dev/null 2>&1 && break
	sleep 1
done
systemctl is-active --quiet vif-allocator.service
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:9080/readyz
sudo cmp -s "$source_unit" "$installed_unit"
sudo cmp -s "$repo_root/bin/vif-allocator" "$binary"
sudo grep -Fx 'VIF_ALLOCATOR_LOG_STREAM_URL=http://127.0.0.1:8081/stream' \
	"$installed_env" >/dev/null

rollback_required=false
allocator_stopped=false
echo "updated vif-allocator at revision $(git -C "$repo_root" rev-parse HEAD)"
