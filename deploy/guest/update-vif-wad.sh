#!/bin/sh
# Replace the node's scenario volume with the checkout's wad/, atomically.
#
# Run as the ordinary repository user:
#
#   ./deploy/guest/update-vif-wad.sh              # the current worktree's wad/
#   ./deploy/guest/update-vif-wad.sh /path/to/wad # an explicit tree
#
# Unlike update-vif-image.sh this does **not** require an idle fleet, and that is
# the point. The swap is a rename: a running match keeps the directory inode its
# pod already mounted, and the next pod mounts the new one. Nothing reloads, and
# nothing has to.
#
# What it refuses: a tree that is not laid out like wad/, and a tree the fleet
# cannot load. Every scenario is validated by -check against the same mount layout
# a session gets, so one that would have failed an init container fails here first.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)

case ${1:-} in
	-h|--help)
		sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
esac
[ "$#" -le 1 ] || { echo "usage: $0 [WAD_DIR]" >&2; exit 2; }

source_wad=${1:-$repo_root/wad}
wad_root=${VIF_WAD_ROOT:-/var/db/vif/wad}
staging="$wad_root.new"
previous="$wad_root.previous"
layout=

die() { echo "$0: $*" >&2; exit 1; }
note() { echo "== $*"; }
cleanup() { [ -z "$layout" ] || rm -rf "$layout"; }
trap cleanup EXIT INT TERM

[ -d "$source_wad/scenario" ] || die "$source_wad has no scenario/ directory"
[ -d "$source_wad/image" ] || die "$source_wad has no image/ directory"

scenarios=$(find "$source_wad/scenario" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
[ -n "$scenarios" ] || die "$source_wad/scenario holds no scenarios"
for name in $scenarios; do
	[ -r "$source_wad/scenario/$name/scenario.toml" ] \
		|| die "scenario/$name has no scenario.toml"
done
note "scenarios found: $(echo "$scenarios" | tr '\n' ' ')"

# Built here rather than run from a container: the node keeps Docker stopped
# outside an image build, so a runtime is the one thing this cannot count on, and a
# dead socket would read as a broken scenario. Same worktree the image is built
# from. The layout is scenario/ and image/ only, as a pod gets, so a scenario
# needing a corpus it will not have on the node fails here rather than in a pod.
checker=$repo_root/bin/vif-headless
if command -v go >/dev/null 2>&1 && make -C "$repo_root" headless >/dev/null; then
	layout=$(mktemp -d)
	ln -s "$source_wad/scenario" "$layout/scenario"
	ln -s "$source_wad/image" "$layout/image"
	note "validating every scenario with $checker"
	for name in $scenarios; do
		"$checker" -check -config-dir "$layout" -s "$name" >/dev/null \
			|| die "scenario $name does not load"
		echo "  ok    $name"
	done
else
	echo "$0: no Go toolchain here; scenarios not validated" >&2
	echo "$0: the init container still refuses a broken one, but later" >&2
fi

note "staging $source_wad into $staging"
sudo install -d -o root -g root -m 0755 "$(dirname "$wad_root")"
sudo rm -rf "$staging"
sudo cp -a "$source_wad" "$staging"
# Directories 0755 and files 0644, owned by root: every session reads this and
# none of them writes it.
sudo chown -R root:root "$staging"
sudo chmod -R u=rwX,go=rX "$staging"

# The swap. A running pod's bind mount resolves the inode it was given, so it
# keeps the tree it started on; the next pod mounts what is at the path now.
note "swapping $wad_root"
sudo rm -rf "$previous"
if [ -d "$wad_root" ]; then
	sudo mv "$wad_root" "$previous"
fi
sudo mv "$staging" "$wad_root"

note "installed:"
sudo find "$wad_root/scenario" -mindepth 2 -maxdepth 2 -name scenario.toml -printf '  %h\n' | sort
[ -d "$previous" ] && echo "  (previous tree retained at $previous)"
echo "done. Sessions already running keep the scenarios they started on."
