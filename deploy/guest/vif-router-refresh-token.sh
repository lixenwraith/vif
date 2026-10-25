#!/bin/sh
# Atomically replace the router's short-lived ServiceAccount token. The
# router reads this file for every API request, so no process restart is
# needed and a failed refresh leaves the previous token intact.
set -eu

router_user=${ROUTER_USER:-vif-router}
router_group=${ROUTER_GROUP:-vif-router}
namespace=${VIF_NAMESPACE:-vif}
service_account=${VIF_ROUTER_SERVICE_ACCOUNT:-vif-router}
token_dir=${VIF_ROUTER_TOKEN_DIR:-/etc/vif-router}
token_file=$token_dir/token

install -d -o root -g "$router_group" -m 0750 "$token_dir"
temporary=$(mktemp --tmpdir="$token_dir" .token.XXXXXX)
cleanup() {
	rm -f "$temporary"
}
trap cleanup EXIT HUP INT TERM

attempt=1
while ! /usr/local/bin/kubectl --request-timeout=5s -n "$namespace" \
	create token "$service_account" --duration=24h >"$temporary"; do
	if [ "$attempt" -ge 6 ]; then
		echo "$0: could not mint a router token after $attempt attempts" >&2
		exit 1
	fi
	attempt=$((attempt + 1))
	sleep 5
done
test -s "$temporary"
chown "$router_user:$router_group" "$temporary"
chmod 0600 "$temporary"
mv -f "$temporary" "$token_file"
trap - EXIT HUP INT TERM
