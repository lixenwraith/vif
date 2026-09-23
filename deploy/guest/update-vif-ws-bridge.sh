#!/bin/sh
# Package websocat 1.x as the session pods' WebSocket sidecar and import it into
# K3s as VIF_ALLOCATOR_WS_BRIDGE_IMAGE, with no Docker. The binary is the one on
# PATH, else the pinned GitHub release build-websocat.sh builds into bin/. An image
# already current is left alone; --diff only says whether it is, exiting 1 if not.
# Usage: update-vif-ws-bridge.sh [--diff] [websocat]
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
diff_only=false
case ${1:-} in
	-h|--help) sed -n '2,6p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	--diff) diff_only=true; shift ;;
esac
[ "$#" -le 1 ] || { echo "usage: $0 [--diff] [websocat]" >&2; exit 2; }

ref=$(sed -n 's/^VIF_ALLOCATOR_WS_BRIDGE_IMAGE=//p' "$repo_root/deploy/guest/vif-allocator.env")
[ -n "$ref" ] || { echo "$0: vif-allocator.env names no VIF_ALLOCATOR_WS_BRIDGE_IMAGE" >&2; exit 2; }
tag=$(sed -n 's/^tag=//p' "$script_dir/build-websocat.sh")
built=$repo_root/bin/websocat-$tag
source_bin=${1:-$(command -v websocat || true)}
[ -n "$source_bin" ] || source_bin=$built
if [ ! -x "$source_bin" ]; then
	if [ "$diff_only" = true ]; then
		echo "websocat is not installed: would build $tag from GitHub and import $ref"
		exit 1
	fi
	"$script_dir/build-websocat.sh" "$built"
fi
version=$("$source_bin" --version 2>/dev/null || true)
case "$version" in
	'websocat 1.'*) ;;
	*) echo "$0: $source_bin reports '$version'; the sidecar's arguments are websocat 1.x's" >&2; exit 1 ;;
esac

case $(uname -m) in
	x86_64) arch=amd64 ;;
	aarch64) arch=arm64 ;;
	*) echo "$0: unsupported machine $(uname -m)" >&2; exit 1 ;;
esac

work=$(mktemp -d "${TMPDIR:-/tmp}/vif-ws-bridge.XXXXXX")
trap 'rm -rf -- "$work"' EXIT HUP INT TERM
rootfs=$work/rootfs
layout=$work/layout
mkdir -p "$rootfs" "$layout/blobs/sha256"

install -m 0555 "$source_bin" "$rootfs/ws-bridge"
# A dynamically linked build brings its loader and libraries at the paths it was
# linked against; a static one brings nothing. The pod mounts nothing from the node.
libs=$(ldd "$source_bin" 2>/dev/null | awk '$2 == "=>" && $3 ~ /^\// { print $3 } $1 ~ /^\// { print $1 }' || true)
lib_dirs=
for lib in $libs; do
	mkdir -p "$rootfs$(dirname "$lib")"
	cp -L "$lib" "$rootfs$lib"
	chmod 0555 "$rootfs$lib"
	case ":$lib_dirs:" in *":$(dirname "$lib"):"*) ;; *) lib_dirs=${lib_dirs:+$lib_dirs:}$(dirname "$lib") ;; esac
done
find "$rootfs" -type d -exec chmod 0755 {} + # not the operator's umask
env_json=
[ -z "$lib_dirs" ] || env_json="\"Env\":[\"LD_LIBRARY_PATH=$lib_dirs\"],"

blob() {
	digest=$(sha256sum "$1" | cut -d' ' -f1)
	mv "$1" "$layout/blobs/sha256/$digest"
	echo "$digest $(wc -c <"$layout/blobs/sha256/$digest")"
}

tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 -C "$rootfs" -cf "$work/layer" .
read -r layer_digest layer_size <<EOF
$(blob "$work/layer")
EOF

# The pod spec states the same user and arguments; these make the image run alone.
printf '%s' "{\"architecture\":\"$arch\",\"os\":\"linux\",\"config\":{\"User\":\"65532:65532\",$env_json\"Entrypoint\":[\"/ws-bridge\"],\"Cmd\":[\"--binary\",\"--exit-on-eof\",\"ws-l:0.0.0.0:7779\",\"tcp:127.0.0.1:7777\"],\"ExposedPorts\":{\"7779/tcp\":{}},\"Labels\":{\"org.opencontainers.image.version\":\"${version#websocat }\"}},\"rootfs\":{\"type\":\"layers\",\"diff_ids\":[\"sha256:$layer_digest\"]}}" >"$work/config"
read -r config_digest config_size <<EOF
$(blob "$work/config")
EOF

printf '%s' "{\"schemaVersion\":2,\"mediaType\":\"application/vnd.oci.image.manifest.v1+json\",\"config\":{\"mediaType\":\"application/vnd.oci.image.config.v1+json\",\"digest\":\"sha256:$config_digest\",\"size\":$config_size},\"layers\":[{\"mediaType\":\"application/vnd.oci.image.layer.v1.tar\",\"digest\":\"sha256:$layer_digest\",\"size\":$layer_size}]}" >"$work/manifest"
read -r manifest_digest manifest_size <<EOF
$(blob "$work/manifest")
EOF

printf '%s' '{"imageLayoutVersion":"1.0.0"}' >"$layout/oci-layout"
printf '%s' "{\"schemaVersion\":2,\"manifests\":[{\"mediaType\":\"application/vnd.oci.image.manifest.v1+json\",\"digest\":\"sha256:$manifest_digest\",\"size\":$manifest_size,\"platform\":{\"architecture\":\"$arch\",\"os\":\"linux\"},\"annotations\":{\"io.containerd.image.name\":\"$ref\",\"org.opencontainers.image.ref.name\":\"${ref##*:}\"}}]}" >"$layout/index.json"
tar -C "$layout" -cf "$work/image.tar" oci-layout index.json blobs

# The layout is deterministic, so an unchanged websocat is an unchanged digest.
imported=$(sudo k3s ctr -n k8s.io images ls | awk -v ref="$ref" '$1 == ref { print $3 }')
if [ "$imported" = "sha256:$manifest_digest" ]; then
	[ "$diff_only" = true ] || echo "current: $ref ($version)"
	exit 0
fi
if [ "$diff_only" = true ]; then
	echo "$ref: ${imported:-not imported} -> sha256:$manifest_digest ($version from $source_bin)"
	exit 1
fi

echo "importing $ref ($version, $(echo "$libs" | grep -c . || true) linked libraries)"
sudo k3s ctr -n k8s.io images import "$work/image.tar"
sudo k3s crictl inspecti "$ref" >/dev/null

# The pod's identity and read-only root: a missing library or a mode the pod user
# cannot execute fails here rather than in the first browser join.
check=vif-ws-bridge-check-$$
sudo k3s ctr -n k8s.io run --rm --read-only --user 65532:65532 "$ref" "$check" /ws-bridge --version
echo "ready: VIF_ALLOCATOR_WS_BRIDGE_IMAGE=$ref sha256:$manifest_digest"
