#!/bin/sh
# One-time rewiring of a node commissioned with vif-allocator, the router's name
# before it routed relays: the vif-router identity, settings, token, Role and units
# take the allocator's place, and ./deploy/update.sh runs as before from then on.
# Run it once, with the fleet empty; it refuses a node already migrated.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
case ${1:-} in
	-h|--help) sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	'') ;;
	*) echo "usage: $0" >&2; exit 2 ;;
esac

old_dir=/etc/vif-allocator
old_env=$old_dir/allocator.env
new_dir=/etc/vif-router
new_env=$new_dir/router.env
binary=/usr/local/bin/vif-router
token_script=/usr/local/libexec/vif-router-refresh-token
units="vif-router.service vif-router-token.service vif-router-token.timer"

sudo test -f "$old_env" || {
	echo "$0: no vif-allocator settings at $old_env; nothing to migrate" >&2
	exit 1
}
if sudo test -f "$new_env"; then
	echo "$0: $new_env exists: this node is migrated, and ./deploy/update.sh keeps it current" >&2
	exit 1
fi
test "$(git -C "$repo_root" rev-parse --show-toplevel)" = "$repo_root"
if [ -n "$(git -C "$repo_root" status --porcelain)" ]; then
	echo "$0: the vif worktree differs from HEAD" >&2
	exit 1
fi
if ! "$repo_root/deploy/k3s/session.sh" blockers; then
	echo "$0: the fleet must be empty before the migration" >&2
	exit 1
fi
# The allocator could not have started without its image line, which the router's
# settings carry over; update-vif-image.sh writes the router's from here on.
image_line=$(sudo cat "$old_env" | sed -n 's/^VIF_ALLOCATOR_IMAGE=/VIF_ROUTER_IMAGE=/p' | tail -n 1)
[ -n "$image_line" ] || {
	echo "$0: $old_env names no VIF_ALLOCATOR_IMAGE; add the line the allocator ran with and rerun" >&2
	exit 1
}

make -C "$repo_root" router
[ -x "$repo_root/bin/vif-router" ]

# undo removes what this run installed, so the migration can be run again, and
# starts the allocator as it was; the allocator's files are never touched before
# the router answers.
undo() {
	echo "$0: migration failed; vif-allocator runs as it was" >&2
	sudo journalctl -u vif-router.service -n 10 --no-pager -o cat >&2 || true
	# shellcheck disable=SC2086
	sudo systemctl disable --now $units >/dev/null 2>&1 || true
	for unit in $units; do
		sudo rm -f "/etc/systemd/system/$unit"
	done
	sudo rm -f "$binary" "$token_script"
	sudo rm -rf "$new_dir"
	sudo systemctl daemon-reload
	sudo systemctl enable --now vif-allocator-token.timer vif-allocator.service
}

echo "installing the vif-router Role, identity, settings, token and units"
# The Role's new ServiceAccount, before a token is minted for it; update.sh
# objects finds it current afterwards.
sudo kubectl apply -f "$repo_root/deploy/k3s/40-router-rbac.yaml"
getent group vif-router >/dev/null || sudo groupadd --system vif-router
id -u vif-router >/dev/null 2>&1 || sudo useradd --system \
	--gid vif-router --home-dir / --shell /usr/bin/nologin vif-router
sudo install -d -o root -g vif-router -m 0750 "$new_dir"
sudo install -o root -g vif-router -m 0640 "$old_dir/server-ca.crt" "$new_dir/server-ca.crt"
# The repository's settings plus the node's image line, as update-vif-router.sh
# renders them: the allocator's other lines were the repository's too.
{ cat "$repo_root/deploy/guest/vif-router.env"; printf '%s\n' "$image_line"; } |
	sudo install -o root -g vif-router -m 0640 /dev/stdin "$new_env"
sudo install -D -o root -g root -m 0755 "$repo_root/deploy/guest/vif-router-refresh-token.sh" "$token_script"
for unit in $units; do
	sudo install -o root -g root -m 0644 "$repo_root/deploy/guest/$unit" "/etc/systemd/system/$unit"
done
sudo install -o root -g root -m 0755 "$repo_root/bin/vif-router" "$binary"
sudo systemctl daemon-reload
sudo systemctl start vif-router-token.service || { undo; exit 1; }
sudo systemctl enable vif-router-token.timer vif-router.service

echo "stopping vif-allocator and starting vif-router in its place"
sudo systemctl disable --now vif-allocator-token.timer vif-allocator.service
if ! sudo systemctl start vif-router.service ||
	! curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:9080/healthz ||
	! curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:9080/readyz; then
	undo
	exit 1
fi

echo "removing vif-allocator; its settings stay at $new_dir/vif-allocator.migrated"
sudo rm -f /etc/systemd/system/vif-allocator.service /etc/systemd/system/vif-allocator.service.previous \
	/etc/systemd/system/vif-allocator-token.service /etc/systemd/system/vif-allocator-token.timer \
	/usr/local/bin/vif-allocator /usr/local/libexec/vif-allocator-refresh-token \
	/usr/local/libexec/vif-allocator.previous
sudo systemctl daemon-reload
sudo mv "$old_dir" "$new_dir/vif-allocator.migrated"
sudo chown -R root:vif-router "$new_dir/vif-allocator.migrated"
sudo kubectl -n vif delete serviceaccount,role,rolebinding vif-allocator --ignore-not-found
sudo userdel vif-allocator 2>/dev/null || true
sudo groupdel vif-allocator 2>/dev/null || true
echo "migrated: vif-router runs in vif-allocator's place at revision $(git -C "$repo_root" rev-parse HEAD);"
echo "  ./deploy/update.sh brings the node to HEAD as before"
