#!/bin/sh
# What make's check-go runs, from the repository root, when the Go command GO names
# is missing: shows the install by hand and offers the system's package or Go's
# latest release unpacked as DIST, which make puts last on PATH (script/README.md).
# Usage: script/toolchain.sh GO DIST
set -eu

go=$1
dist=$2

die() {
	printf '%s\n' "$*" >&2
	exit 1
}

[ -z "${SUDO_USER:-}" ] || die "Go compiler ($go) not found; sudo may reset PATH: make release as $SUDO_USER, then sudo make install."
case $go in */*) die "Go compiler ($go) not found: install Go, or set GO to the one installed." ;; esac

pkg=
if [ -f /etc/arch-release ]; then
	pkg="sudo pacman -S go"
elif [ -f /etc/debian_version ]; then
	# apt's golang lags go.mod; the snap follows Go's releases
	if command -v snap >/dev/null 2>&1; then pkg="sudo snap install go --classic"; fi
elif [ "$(uname -s)" = FreeBSD ]; then
	pkg="sudo pkg install $go"
elif [ "$(uname -s)" = Darwin ] && command -v brew >/dev/null 2>&1; then
	pkg="brew install go"
fi

# fetch writes URL $1 to $2, - for stdout; with neither curl nor wget it fails.
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --proto =https -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		return 1
	fi
}

sum256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1"
	else
		shasum -a 256 "$1"
	fi | cut -d ' ' -f 1
}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
x86_64) arch=amd64 ;;
aarch64) arch=arm64 ;;
i?86) arch=386 ;;
armv*) arch=armv6l ;;
loongarch64) arch=loong64 ;;
*) arch=$(uname -m) ;;
esac
# go.dev lists the latest stable release first, each file with its SHA-256
release=$(fetch 'https://go.dev/dl/?mode=json' - 2>/dev/null | awk -v want=".$os-$arch.tar.gz\"" '
	index($0, "\"filename\"") && index($0, want) { split($0, q, "\""); file = q[4]; next }
	file != "" && index($0, "\"sha256\"") { split($0, q, "\""); print file, q[4]; exit }')
file=${release% *}
sum=${release#* }
ver=${file%".$os-$arch.tar.gz"}
url=https://dl.google.com/go/$file

echo "Go compiler ($go) not found; go.mod asks for Go $(sed -n 's/^go //p' go.mod) or newer."
keys=
if [ -n "$pkg" ]; then
	echo "  p: $pkg"
	keys=p
fi
if [ -n "$release" ]; then
	echo "  d: download Go ${ver#go} for $os/$arch into $dist, for builds in this checkout alone"
	keys=${keys:+$keys, }d
	echo "  To install it by hand, as https://go.dev/doc/install does:"
	echo "      curl -fLO $url    # SHA-256 $sum"
	echo "      sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf $file"
	echo "      and add /usr/local/go/bin to PATH"
else
	echo "  To install it by hand: unpack a release from https://go.dev/dl/ as /usr/local/go and add /usr/local/go/bin to PATH."
fi
[ -n "$keys" ] || exit 1
printf '%s, or anything else to stop: ' "$keys"
read -r yn || yn=
case $keys,$yn in
p*,[Pp]*)
	$pkg
	command -v "$go" >/dev/null 2>&1 || die "$go is still not on PATH: add the directory $pkg installed it in."
	exit 0
	;;
*d,[Dd]*) ;;
*) die "Stopped: install Go to continue." ;;
esac

[ ! -e "$dist" ] || die "$dist holds no bin/$go: remove it and run make again."
mkdir -p "$(dirname "$dist")"
tmp=$(mktemp -d "$(dirname "$dist")/.go-download.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
echo "Downloading $url"
fetch "$url" "$tmp/$file"
[ "$(sum256 "$tmp/$file")" = "$sum" ] || die "$url does not match the SHA-256 go.dev lists, $sum."
tar -C "$tmp" -xzf "$tmp/$file"
mv "$tmp/go" "$dist"
[ "$go" = go ] || ln -s go "$dist/bin/$go"
echo "Builds here use Go ${ver#go} from $dist until a Go is installed; then remove $dist."
