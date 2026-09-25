#!/bin/sh
# Bring the node to this checkout's HEAD. Each component that differs is shown as a
# diff (installed -, incoming +) and then updated, in dependency order; a current
# one is skipped. --diff shows and changes nothing, and exits 1 if anything differs.
# Usage: deploy/update.sh [--diff] [objects wad bridge image allocator logwisp]
set -eu

deploy=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$deploy/.." && pwd)
guest=$deploy/guest
all="objects wad bridge image allocator logwisp"
wad_root=${VIF_WAD_ROOT:-/var/db/vif/wad}

diff_only=false
selected=
for arg in "$@"; do
	case $arg in
		-h|--help) sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		--diff) diff_only=true ;;
		objects|wad|bridge|image|allocator|logwisp) selected="$selected $arg" ;;
		*) echo "usage: $0 [--diff] [$all]" >&2; exit 2 ;;
	esac
done
[ -n "$selected" ] || selected=$all

die() { echo "$0: $*" >&2; exit 1; }
kube() { sudo kubectl "$@"; }
git_() { git -C "$repo_root" "$@"; }
if [ "$diff_only" = false ] && [ -n "$(git_ status --porcelain)" ]; then
	die "the worktree differs from HEAD; commit or stash what you mean to deploy"
fi

# Installed lines green, incoming lines red, and only on a terminal.
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	esc=$(printf '\033')
	old="${esc}[32m" new="${esc}[31m" bold="${esc}[1m" off="${esc}[0m"
else
	old='' new='' bold='' off=''
fi
paint() {
	awk 'NR <= 200 { print } END { if (NR > 200) print "... " NR - 200 " more lines" }' |
		sed -e "s/^--- .*/$bold&$off/" -e "s/^+++ .*/$bold&$off/" \
			-e "s/^-.*/$old&$off/" -e "s/^+.*/$new&$off/"
}
say() { printf '%s== %s%s\n' "$bold" "$*" "$off"; }
# show LABEL INSTALLED INCOMING: paints the difference, returns 1 when there is one.
show() {
	rc=0
	out=$(diff -u --label "installed $1" --label "incoming $1" "$2" "$3" 2>&1) || rc=$?
	[ "$rc" -eq 0 ] || printf '%s\n' "$out" | paint
	return "$rc"
}
sudo_copy() { sudo cat "$1" >"$2" 2>/dev/null || : >"$2"; }

work=$(mktemp -d "${TMPDIR:-/tmp}/vif-update.XXXXXX")
trap 'rm -rf -- "$work"' EXIT HUP INT TERM

# --- what each component differs by: 0 current, 1 pending -----------------------

check_objects() {
	status=0
	for name in 00-namespace 10-quota 20-networkpolicy 40-allocator-rbac; do
		rc=0
		out=$(kube diff -f "$repo_root/deploy/k3s/$name.yaml" 2>&1) || rc=$?
		case $rc in
			0) ;;
			1) printf '%s\n' "$out" | paint; status=1 ;;
			*) printf '%s\n' "$out" >&2; die "kubectl diff failed on $name" ;;
		esac
	done
	# Node-pinned volumes: nodeAffinity is immutable, so a change is a migration
	# somebody runs deliberately (doc/kube-docker-deploy.md §9), never an apply here.
	node=$(kube get nodes -o jsonpath='{.items[0].metadata.name}')
	for name in 05-log-volume 07-wad-volume; do
		rc=0
		out=$(sed "s|\${NODE_NAME}|$node|g" "$repo_root/deploy/k3s/$name.yaml" |
			kube diff -f - 2>&1) || rc=$?
		[ "$rc" -eq 0 ] && continue
		printf '%s\n' "$out" | paint
		echo "$name differs from the cluster and is not applied by this script" >&2
	done
	return "$status"
}

# The installer swaps in exactly its categories, so the whole installed root is
# compared: a removed file or a category no longer served is deleted by the update.
check_wad() {
	categories=$(sed -n 's/^categories="\(.*\)"$/\1/p' "$guest/update-vif-wad.sh")
	[ -n "$categories" ] || return 2
	mkdir "$work/incoming"
	for category in $categories; do
		ln -s "$repo_root/wad/$category" "$work/incoming/$category"
	done
	rc=0
	out=$(cd "$work" && diff -ru --unidirectional-new-file "$wad_root" incoming 2>&1) || rc=$?
	[ "$rc" -ne 0 ] || return 0
	printf '%s\n' "$out" | sed "s|^Only in \([^:]*\): \(.*\)|${old}deleted \1/\2$off|" | paint
	return 1
}

check_bridge() {
	"$guest/update-vif-ws-bridge.sh" --diff
}

check_image() {
	sudo_copy /etc/vif-allocator/allocator.env "$work/allocator.env"
	installed=$(sed -n 's/^VIF_ALLOCATOR_IMAGE=//p' "$work/allocator.env" | tail -n 1)
	tag=${installed##*:}
	incoming=vif:$(git_ rev-parse --short=8 HEAD)
	if [ -n "$tag" ] && git_ rev-parse -q --verify "$tag^{commit}" >/dev/null; then
		if git_ diff --quiet "$tag" HEAD -- cmd internal pkg go.mod go.sum deploy/docker/Dockerfile &&
			sudo k3s crictl inspecti "$installed" >/dev/null 2>&1; then
			return 0
		fi
		echo "$installed -> $incoming:$(git_ diff --shortstat "$tag" HEAD -- \
			cmd internal pkg go.mod go.sum deploy/docker/Dockerfile)"
		return 1
	fi
	echo "${installed:-no session image} -> $incoming"
	return 1
}

check_allocator() {
	status=0
	"$guest/update-vif-allocator.sh" --render >"$work/allocator.env.next" || return 2
	sudo_copy /etc/vif-allocator/allocator.env "$work/allocator.env"
	show allocator.env "$work/allocator.env" "$work/allocator.env.next" || status=1
	show vif-allocator.service /etc/systemd/system/vif-allocator.service \
		"$guest/vif-allocator.service" || status=1
	revision=$(go version -m /usr/local/bin/vif-allocator 2>/dev/null |
		awk '$1 == "build" && $2 ~ /^vcs\.revision=/ { sub(/^vcs\.revision=/, "", $2); print $2 }')
	if [ -z "$revision" ] || ! git_ rev-parse -q --verify "$revision^{commit}" >/dev/null; then
		echo "binary: unknown revision -> $(git_ rev-parse --short=8 HEAD)"
		status=1
	elif ! git_ diff --quiet "$revision" HEAD -- tool/vif-allocator go.mod go.sum; then
		echo "binary: $(printf '%.8s' "$revision") -> $(git_ rev-parse --short=8 HEAD):$(git_ diff \
			--shortstat "$revision" HEAD -- tool/vif-allocator go.mod go.sum)"
		status=1
	fi
	return "$status"
}

check_logwisp() {
	status=0
	revision=$(tr -d '[:space:]' <"$deploy/logwisp/REVISION")
	if ! /usr/local/bin/logwisp --version 2>/dev/null | grep -qF "$revision"; then
		echo "binary: -> LogWisp $(printf '%.12s' "$revision")"
		status=1
	fi
	sudo_copy /etc/logwisp/vif-fleet.toml "$work/logwisp.toml"
	show vif-fleet.toml "$work/logwisp.toml" "$deploy/logwisp/aggregator.toml" || status=1
	show logwisp.service /etc/systemd/system/logwisp.service "$guest/logwisp.service" || status=1
	return "$status"
}

# --- how each is brought to HEAD ------------------------------------------------

apply_objects() {
	for name in 00-namespace 10-quota 20-networkpolicy 40-allocator-rbac; do
		kube apply -f "$repo_root/deploy/k3s/$name.yaml"
	done
}
apply_wad() { "$guest/update-vif-wad.sh"; }
apply_bridge() { "$guest/update-vif-ws-bridge.sh"; }
apply_image() { "$guest/update-vif-image.sh"; }
apply_allocator() { "$guest/update-vif-allocator.sh"; }
# LogWisp's updater leaves the allocator alone, and the allocator reads its stream.
apply_logwisp() {
	sudo systemctl stop vif-allocator.service
	rc=0
	"$guest/update-logwisp.sh" || rc=$?
	sudo systemctl start vif-allocator.service
	return "$rc"
}

pending=
for component in $all; do
	case " $selected " in *" $component "*) ;; *) continue ;; esac
	say "$component"
	verdict=0
	"check_$component" || verdict=$?
	case $verdict in
		0) echo "current" ;;
		1) pending="$pending $component" ;;
		*) die "$component: could not tell what is installed" ;;
	esac
done

if [ -z "$pending" ]; then
	say "everything selected is current"
	exit 0
fi
if [ "$diff_only" = true ]; then
	say "would update:$pending"
	exit 1
fi

# These restart the allocator or replace what a session runs, so no match may be live.
case "$pending" in
	*image*|*allocator*|*logwisp*)
		"$deploy/k3s/session.sh" blockers ||
			die "empty the fleet first (./deploy/k3s/session.sh drain), or update only:$(
				for c in $pending; do case $c in objects|wad|bridge) printf ' %s' "$c" ;; esac; done)"
		;;
esac

for component in $pending; do
	say "updating $component"
	"apply_$component"
done
say "updated:$pending"
