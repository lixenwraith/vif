#!/bin/sh
# Build and deploy the router from this vif revision, with its settings from
# deploy/guest/vif-router.env. New allocations pause during the short cutover, and
# one known-good binary/config/unit is kept. --render prints the env file it would
# install; deploy/update.sh shows it against the installed one.
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

binary=/usr/local/bin/vif-router
installed_env=/etc/vif-router/router.env
installed_unit=/etc/systemd/system/vif-router.service
source_unit=$repo_root/deploy/guest/vif-router.service
source_env=$repo_root/deploy/guest/vif-router.env
backup_binary=/usr/local/libexec/vif-router.previous
backup_env=/etc/vif-router/router.env.previous
backup_unit=/etc/systemd/system/vif-router.service.previous

# The repository's settings plus the node's VIF_ROUTER_IMAGE, which names what this
# node last built: update-vif-image.sh writes it and this carries it over.
render_env() {
	image_line=$(sudo cat "$installed_env" | grep '^VIF_ROUTER_IMAGE=' | tail -n 1 || true)
	[ -n "$image_line" ] || {
		echo "$0: $installed_env names no VIF_ROUTER_IMAGE; run update-vif-image.sh first" >&2
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
# /etc/vif-router is root:vif-router 0750, so an operator account cannot stat
# inside it and an installed env file would read as missing.
for installed in "$binary" "$installed_env" "$installed_unit"; do
	sudo test -f "$installed" ||
		{ echo "$0: missing installed file: $installed" >&2; exit 1; }
done
systemctl is-active --quiet vif-router.service || {
	echo "$0: vif-router.service must be active before an update" >&2
	exit 1
}
[ -r "$source_unit" ] || { echo "$0: missing unit: $source_unit" >&2; exit 1; }
[ -r "$source_env" ] || { echo "$0: missing settings: $source_env" >&2; exit 1; }

stage_root=$(mktemp -d "${TMPDIR:-/tmp}/vif-router-update.XXXXXX")
router_stopped=false
rollback_required=false
rollback() {
	echo "$0: update failed; restoring previous router" >&2
	sudo journalctl -u vif-router.service -n 5 --no-pager -o cat >&2 || true
	sudo systemctl stop vif-router.service >/dev/null 2>&1 || true
	sudo install -o root -g root -m 0755 "$backup_binary" "$binary"
	sudo install -o root -g vif-router -m 0640 "$backup_env" "$installed_env"
	sudo install -o root -g root -m 0644 "$backup_unit" "$installed_unit"
	sudo systemctl daemon-reload
	sudo systemctl start vif-router.service
}
cleanup() {
	status=$?
	trap - EXIT HUP INT TERM
	if [ "$status" -ne 0 ]; then
		if [ "$rollback_required" = true ]; then
			rollback || status=1
		elif [ "$router_stopped" = true ]; then
			sudo systemctl start vif-router.service || status=1
		fi
	fi
	case "$stage_root" in
		*/vif-router-update.*) rm -rf -- "$stage_root" || status=1 ;;
		*) echo "$0: refusing unsafe cleanup path: $stage_root" >&2; status=1 ;;
	esac
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

render_env >"$stage_root/router.env.next"

make -C "$repo_root" router
[ -x "$repo_root/bin/vif-router" ]

if ! "$repo_root/deploy/k3s/session.sh" blockers; then
	echo "$0: the fleet must be empty before the router update" >&2
	exit 1
fi

echo "pausing new allocations for the router cutover"
sudo systemctl stop vif-router.service
router_stopped=true
if ! "$repo_root/deploy/k3s/session.sh" blockers; then
	echo "$0: a fleet object remained after allocation stopped" >&2
	exit 1
fi

sudo install -D -o root -g root -m 0755 "$binary" "$backup_binary"
sudo install -o root -g vif-router -m 0640 "$installed_env" "$backup_env"
sudo install -o root -g root -m 0644 "$installed_unit" "$backup_unit"
rollback_required=true

sudo install -o root -g root -m 0755 "$repo_root/bin/vif-router" "$binary"
sudo install -o root -g vif-router -m 0640 \
	"$stage_root/router.env.next" "$installed_env"
sudo install -o root -g root -m 0644 "$source_unit" "$installed_unit"
sudo systemctl daemon-reload
sudo systemctl start vif-router.service

systemctl is-active --quiet vif-router.service
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:9080/healthz
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:9080/readyz
sudo cmp -s "$source_unit" "$installed_unit"
sudo cmp -s "$repo_root/bin/vif-router" "$binary"
sudo cat "$installed_env" | cmp -s "$stage_root/router.env.next" -
# A published browser route answers a plain GET with not_an_upgrade, not 501.
if [ -n "$(sed -n 's/^VIF_ROUTER_WEB_ORIGIN=//p' "$source_env")" ]; then
	curl --connect-timeout 2 --max-time 5 -sS \
		http://127.0.0.1:9080/vif/ws/0000000000000000 | grep -q not_an_upgrade
fi
# The front door looks up a name no session holds and says so in a join reply.
route_port=$(sed -n 's/^VIF_ROUTER_ROUTE_LISTEN=.*:\([0-9]*\)$/\1/p' "$source_env")
bash -c 'exec 3<>"/dev/tcp/127.0.0.1/$1" &&
	printf "\005\000\000\000\000\000\000\000\000\000\000\020%s" 0000000000000000 >&3 &&
	timeout 5 cat <&3' _ "${route_port:-7777}" | grep -aq 'no such session'

rollback_required=false
router_stopped=false
echo "updated vif-router at revision $(git -C "$repo_root" rev-parse HEAD)"
