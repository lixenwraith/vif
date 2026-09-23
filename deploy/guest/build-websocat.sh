#!/bin/sh
# Build the pinned websocat release from GitHub into OUTPUT, for a node that has
# none installed. Needs git and cargo (Arch: pacman -S rust). Default features off:
# the sidecar only listens for WebSocket and dials TCP, so it links no TLS library.
# Usage: build-websocat.sh OUTPUT
set -eu

tag=v1.14.1
source=https://github.com/vi/websocat

usage() { sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//'; }
case ${1:-} in
	-h|--help) usage; exit 0 ;;
	'') usage >&2; exit 2 ;;
esac
[ "$#" -eq 1 ] || { echo "usage: $0 OUTPUT" >&2; exit 2; }
output=$1
[ ! -e "$output" ] || { echo "$0: refusing to replace $output" >&2; exit 1; }
for tool in git cargo; do
	command -v "$tool" >/dev/null 2>&1 ||
		{ echo "$0: $tool is required (Arch: sudo pacman -S git rust)" >&2; exit 1; }
done

work=$(mktemp -d "${TMPDIR:-/tmp}/vif-websocat-build.XXXXXX")
trap 'rm -rf -- "$work"' EXIT HUP INT TERM

echo "building websocat $tag from $source"
git -c advice.detachedHead=false clone --quiet --depth 1 --branch "$tag" "$source" "$work/src"
RUSTFLAGS='-A warnings' cargo build --quiet --release --locked --no-default-features \
	--manifest-path "$work/src/Cargo.toml" --target-dir "$work/target"
install -D -m 0755 "$work/target/release/websocat" "$output"
"$output" --version
