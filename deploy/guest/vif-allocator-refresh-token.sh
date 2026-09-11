#!/bin/sh
# Atomically replace the allocator's short-lived ServiceAccount token. The
# allocator reads this file for every API request, so no process restart is
# needed and a failed refresh leaves the previous token intact.
set -eu

allocator_user=${ALLOCATOR_USER:-vif-allocator}
allocator_group=${ALLOCATOR_GROUP:-vif-allocator}
namespace=${VIF_NAMESPACE:-vif}
service_account=${VIF_ALLOCATOR_SERVICE_ACCOUNT:-vif-allocator}
token_dir=${VIF_ALLOCATOR_TOKEN_DIR:-/etc/vif-allocator}
token_file=$token_dir/token

install -d -o root -g "$allocator_group" -m 0750 "$token_dir"
temporary=$(mktemp --tmpdir="$token_dir" .token.XXXXXX)
cleanup() {
	rm -f "$temporary"
}
trap cleanup EXIT HUP INT TERM

attempt=1
while ! /usr/local/bin/kubectl --request-timeout=5s -n "$namespace" \
	create token "$service_account" --duration=24h >"$temporary"; do
	if [ "$attempt" -ge 6 ]; then
		echo "$0: could not mint an allocator token after $attempt attempts" >&2
		exit 1
	fi
	attempt=$((attempt + 1))
	sleep 5
done
test -s "$temporary"
chown "$allocator_user:$allocator_group" "$temporary"
chmod 0600 "$temporary"
mv -f "$temporary" "$token_file"
trap - EXIT HUP INT TERM
