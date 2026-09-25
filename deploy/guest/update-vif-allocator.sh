#!/bin/sh
# Build and deploy the allocator from this vif revision, with its settings
# from deploy/guest/vif-allocator.env. New allocations pause during the short
# cutover, and one known-good binary/config/unit is kept. --render prints the env
# file it would install; deploy/update.sh shows it against the installed one.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
render_only=false
case ${1:-} in
	-h|--help) sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	--render) render_only=true ;;
	'') ;;
	*) echo "usage: $0 [--render]" >&2; exit 2 ;;
esac

binary=/usr/local/bin/vif-allocator
installed_env=/etc/vif-allocator/allocator.env
installed_unit=/etc/systemd/system/vif-allocator.service
source_unit=$repo_root/deploy/guest/vif-allocator.service
source_env=$repo_root/deploy/guest/vif-allocator.env
backup_binary=/usr/local/libexec/vif-allocator.previous
backup_env=/etc/vif-allocator/allocator.env.previous
backup_unit=/etc/systemd/system/vif-allocator.service.previous

# The repository's settings plus the node's VIF_ALLOCATOR_IMAGE, which names what
# this node last built: update-vif-image.sh writes it and this carries it over.
render_env() {
	image_line=$(sudo cat "$installed_env" | grep '^VIF_ALLOCATOR_IMAGE=' | tail -n 1 || true)
	[ -n "$image_line" ] || {
		echo "$0: $installed_env names no VIF_ALLOCATOR_IMAGE; run update-vif-image.sh first" >&2
		return 1
	}
	cat "$source_env"
	printf '%s\n' "$image_line"
}
if [ "$render_only" = true ]; then
	render_env
	exit
fi

test "$(git -C "$repo_root" rev-parse --show-toplevel)" = "$repo_root"
if [ -n "$(git -C "$repo_root" status --porcelain)" ]; then
	echo "$0: the vif worktree differs from HEAD" >&2
	exit 1
fi
# /etc/vif-allocator is root:vif-allocator 0750, so an operator account cannot
# stat inside it and an installed env file would read as missing.
for installed in "$binary" "$installed_env" "$installed_unit"; do
	sudo test -f "$installed" ||
		{ echo "$0: missing installed file: $installed" >&2; exit 1; }
done
[ -r "$source_unit" ] || { echo "$0: missing unit: $source_unit" >&2; exit 1; }
[ -r "$source_env" ] || { echo "$0: missing settings: $source_env" >&2; exit 1; }
systemctl is-active --quiet vif-allocator.service || {
	echo "$0: vif-allocator.service must be active before an update" >&2
	exit 1
}

stage_root=$(mktemp -d "${TMPDIR:-/tmp}/vif-allocator-update.XXXXXX")
allocator_stopped=false
rollback_required=false
rollback() {
	echo "$0: update failed; restoring previous allocator" >&2
	sudo journalctl -u vif-allocator.service -n 5 --no-pager -o cat >&2 || true
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

render_env >"$stage_root/allocator.env.next"

bridge_image=$(sed -n 's/^VIF_ALLOCATOR_WS_BRIDGE_IMAGE=//p' "$source_env")
if [ -n "$bridge_image" ] && ! sudo k3s crictl inspecti "$bridge_image" >/dev/null 2>&1; then
	echo "$0: $bridge_image is not in K3s; run deploy/guest/update-vif-ws-bridge.sh first" >&2
	exit 1
fi

make -C "$repo_root" allocator
[ -x "$repo_root/bin/vif-allocator" ]

if ! "$repo_root/deploy/k3s/session.sh" blockers; then
	echo "$0: the fleet must be empty before the allocator update" >&2
	exit 1
fi

echo "pausing new allocations for the allocator cutover"
sudo systemctl stop vif-allocator.service
allocator_stopped=true
if ! "$repo_root/deploy/k3s/session.sh" blockers; then
	echo "$0: a fleet object remained after allocation stopped" >&2
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

systemctl is-active --quiet vif-allocator.service
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:9080/readyz
sudo cmp -s "$source_unit" "$installed_unit"
sudo cmp -s "$repo_root/bin/vif-allocator" "$binary"
sudo cat "$installed_env" | cmp -s "$stage_root/allocator.env.next" -
# A published browser route answers a plain GET with not_an_upgrade, not 501.
if [ -n "$bridge_image" ]; then
	curl --connect-timeout 2 --max-time 5 -sS \
		http://127.0.0.1:9080/vif/ws/0000000000000000 | grep -q not_an_upgrade
fi

rollback_required=false
allocator_stopped=false
echo "updated vif-allocator at revision $(git -C "$repo_root" rev-parse HEAD)"
