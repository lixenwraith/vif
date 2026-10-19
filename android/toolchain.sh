#!/bin/sh
# What make android needs: Go, a JDK, and an Android SDK holding the platform and
# NDK app/build.gradle.kts names. "gradlew ARGS" names every missing piece and
# every module go.mod lists below its build, else runs the wrapper; "adb ARGS"
# runs adb from PATH or the SDK; "install" sets the missing pieces up: the only
# command here that downloads (README.md).
set -eu
cd "$(dirname "$0")"

# The command-line tools an SDK is bootstrapped from, as published with their
# SHA-256 at https://developer.android.com/studio#command-tools
tools_build=15859902

die() {
	printf '%s\n' "$*" >&2
	exit 1
}

platform=$(sed -n 's/^ *compileSdkVersion("\(.*\)")$/\1/p' app/build.gradle.kts)
ndk=$(sed -n 's/^ *ndkVersion = "\(.*\)"$/\1/p' app/build.gradle.kts)
if [ -z "$platform" ] || [ -z "$ndk" ]; then
	die "android/toolchain.sh: no compileSdkVersion or ndkVersion in app/build.gradle.kts"
fi

# The SDK the Android Gradle plugin reads: sdk.dir, then ANDROID_HOME; with
# neither, gomobile's default, where Android Studio installs one.
sdk_dir=$(sed -n 's/^sdk\.dir *[=:] *//p' local.properties 2>/dev/null | tail -n 1)
sdk=${sdk_dir:-${ANDROID_HOME:-}}
if [ -z "$sdk" ]; then
	case $(uname -s) in
	Darwin) sdk=$HOME/Library/Android/sdk ;;
	*) sdk=$HOME/Android/Sdk ;;
	esac
fi

sdkmanager=
for d in "$sdk/cmdline-tools/latest" "$sdk"/cmdline-tools/*; do
	if [ -x "$d/bin/sdkmanager" ]; then
		sdkmanager=$d/bin/sdkmanager
		break
	fi
done

# gradlew and sdkmanager start the java JAVA_HOME names, else PATH's; gomobile
# runs javac from PATH.
java=java
[ -z "${JAVA_HOME:-}" ] || java=$JAVA_HOME/bin/java

java_major() {
	command -v "$java" >/dev/null 2>&1 || return 0
	"$java" -version 2>&1 | sed -n 's/.* version "1\.\([0-9]*\).*/\1/p; s/.* version "\([0-9]*\).*/\1/p' | head -n 1
}

have_jdk() {
	command -v "$java" >/dev/null 2>&1 && command -v javac >/dev/null 2>&1
}

jdk_package() {
	if [ -f /etc/arch-release ]; then
		echo "sudo pacman -S jdk21-openjdk"
	elif [ -f /etc/debian_version ]; then
		v=21
		apt-cache show openjdk-21-jdk-headless >/dev/null 2>&1 || v=17
		echo "sudo apt install openjdk-$v-jdk-headless"
	elif [ -f /etc/fedora-release ]; then
		echo "sudo dnf install java-21-openjdk-devel"
	elif [ "$(uname -s)" = Darwin ] && command -v brew >/dev/null 2>&1; then
		echo "brew install --cask temurin@21"
	fi
}

sdk_host() {
	case $(uname -s) in
	Linux | Darwin | *_NT*) ;;
	*) die "Google builds the Android SDK's tools and NDK for Linux, macOS and Windows only: build the APK on one of those." ;;
	esac
}

gaps=
gap() {
	[ -n "$gaps" ] || echo "make android is missing:" >&2
	gaps=1
	printf '  %s\n' "$1" >&2
	[ -z "${2:-}" ] || printf '      %s\n' "$2" >&2
}

preflight() {
	sdk_host
	command -v go >/dev/null 2>&1 || gap "Go, which builds gomobile and the binding"
	if ! have_jdk; then
		if command -v "$java" >/dev/null 2>&1 && [ -x "${JAVA_HOME:-/nonexistent}/bin/javac" ]; then
			gap "javac on PATH, where gomobile runs it from" "export PATH=\"\$JAVA_HOME/bin:\$PATH\""
		else
			pkg=$(jdk_package)
			gap "a JDK: Gradle starts on its java, and gomobile compiles the binding with javac" "${pkg:-Any JDK will do; Gradle builds on a JDK 21 of its own.}"
		fi
	fi
	if [ ! -d "$sdk" ]; then
		gap "an Android SDK at $sdk (sdk.dir in android/local.properties, else ANDROID_HOME)"
		return 1
	fi
	pkgs=
	[ -f "$sdk/platforms/$platform/android.jar" ] || pkgs="$pkgs \"platforms;$platform\""
	[ -f "$sdk/ndk/$ndk/source.properties" ] || pkgs="$pkgs \"ndk;$ndk\""
	[ -z "$pkgs" ] || gap "SDK packages$pkgs in $sdk" "${sdkmanager:+$sdkmanager$pkgs}"
	# The build fetches the build tools its plugin wants under this licence.
	[ -f "$sdk/licenses/android-sdk-license" ] || gap "the SDK licence, accepted in $sdk" "${sdkmanager:+$sdkmanager --licenses}"
	[ -z "$gaps" ]
}

# go.mod here lists every module the binding builds from, at no version below the
# root's go.mod raises it to through the replace; one it misses stops go, as
# gomobile loads the packages, with a tidy hint naming neither module nor
# directory. gomod has go update a scratch copy and names each module it changed.
gomod() {
	err=$(GOOS=android go list -deps ./... 2>&1 >/dev/null) && return 0
	tmp=$(mktemp -d)
	cp go.mod go.sum "$tmp"
	drift=
	if GOOS=android go list -modfile="$tmp/go.mod" -mod=mod -deps ./... >/dev/null 2>&1 &&
		go mod graph -modfile="$tmp/go.mod" >"$tmp/graph" 2>/dev/null; then
		diff go.mod "$tmp/go.mod" >"$tmp/diff" || :
		drift=$(awk -v d="$tmp/diff" '
			FILENAME == d {
				if (!/^[<>] /) next
				s = substr($0, 3)
				sub(/^[ \t]*(require[ \t]+)?/, "", s)
				if (split(s, f, /[ \t]+/) < 2) next
				if (/^</) { was[f[1]] = f[2]; next }
				now[f[1]] = f[2]; mods[++n] = f[1]; want[f[1] "@" f[2]] = f[1]
				next
			}
			index($1, "@") && ($2 in want) {
				m = want[$2]; r = $1; sub(/@.*/, "", r)
				by[m] = by[m] (by[m] == "" ? " required by " : ", ") r
			}
			END {
				for (i = 1; i <= n; i++) {
					m = mods[i]
					if ((m in was) && was[m] == now[m]) continue
					printf "  %s: %s here, %s%s\n", m, ((m in was) ? was[m] : "not listed"), now[m], ((by[m] == "") ? " selected" : by[m])
				}
			}' "$tmp/diff" "$tmp/graph")
	fi
	rm -rf "$tmp"
	if [ -n "$drift" ]; then
		echo "android/go.mod is behind the modules its build requires:" >&2
		printf '%s\n' "$drift" >&2
		echo "Every go command in android/, gomobile's too, stops until it is level; from the repository root:" >&2
		echo "  go -C android mod tidy" >&2
	else
		echo "go stops in android/ before the build:" >&2
		printf '%s\n' "$err" | sed 's/^/  /' >&2
		echo "A command go suggests runs in android/: go -C android <command>, from the repository root." >&2
	fi
	return 1
}

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fL --proto =https --progress-bar -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -O "$2" "$1"
	else
		die "Neither curl nor wget is installed to download $1."
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1"
	else
		shasum -a 256 "$1"
	fi | cut -d ' ' -f 1
}

# bootstrap unpacks the command-line tools as cmdline-tools/latest, the layout
# sdkmanager expects to find its SDK from.
bootstrap() {
	case $(uname -s)/$(uname -m) in
	Linux/*) host=linux sum=4e4c464f145a7512b57d088ac6c278c03c9eea610886b35a5e0804e74eedf583 ;;
	Darwin/arm64) host=mac_arm64 sum=835b62a26162b229b441d1f6d4680383815a270809eb33522c0d480fa5002c4e ;;
	Darwin/x86_64) host=mac_x86_64 sum=c5a6378ab5cf7e0d5701921405115befff13e9ff7417fb588389338f8bd050f3 ;;
	*) die "Unpack the command-line tools from https://developer.android.com/studio#command-tools as $sdk/cmdline-tools/latest, then run make android-toolchain again." ;;
	esac
	[ ! -e "$sdk/cmdline-tools/latest" ] || die "$sdk/cmdline-tools/latest holds no bin/sdkmanager: remove it and run make android-toolchain again."
	command -v unzip >/dev/null 2>&1 || die "unzip is needed to unpack the command-line tools."
	url=https://dl.google.com/android/repository/commandlinetools-$host-${tools_build}_latest.zip
	mkdir -p "$sdk/cmdline-tools"
	tmp=$(mktemp -d "$sdk/cmdline-tools/.download.XXXXXX")
	trap 'rm -rf "$tmp"' EXIT
	echo "Downloading $url"
	fetch "$url" "$tmp/tools.zip"
	[ "$(sha256 "$tmp/tools.zip")" = "$sum" ] || die "$url does not match its published SHA-256 $sum."
	unzip -q "$tmp/tools.zip" -d "$tmp"
	mv "$tmp/cmdline-tools" "$sdk/cmdline-tools/latest"
	sdkmanager=$sdk/cmdline-tools/latest/bin/sdkmanager
}

setup() {
	sdk_host
	set --
	[ -x "$sdk/platform-tools/adb" ] || set -- "$@" platform-tools
	[ -f "$sdk/platforms/$platform/android.jar" ] || set -- "$@" "platforms;$platform"
	[ -f "$sdk/ndk/$ndk/source.properties" ] || set -- "$@" "ndk;$ndk"
	license=
	[ -f "$sdk/licenses/android-sdk-license" ] || license=1
	# sdkmanager runs on Java 17 or newer; the build starts on any JDK.
	need=0
	if [ $# -gt 0 ] || [ -n "$license" ]; then need=17; fi
	major=$(java_major)
	if ! have_jdk && [ "${major:-0}" -ge "$need" ] && [ -x "${JAVA_HOME:-/nonexistent}/bin/javac" ]; then
		die "javac is not on PATH, where gomobile runs it from: export PATH=\"\$JAVA_HOME/bin:\$PATH\""
	fi
	if ! have_jdk || [ "${major:-0}" -lt "$need" ]; then
		pkg=$(jdk_package)
		[ -n "$pkg" ] || die "Install a JDK, 17 or newer, then run make android-toolchain again."
		echo "Gradle and gomobile need a JDK, and sdkmanager Java 17 or newer${major:+; java here is $major}."
		printf 'Proposed installation: %s\nInstall now? [y/N] ' "$pkg"
		read -r yn || yn=
		case $yn in
		[Yy]*) $pkg ;;
		*) die "Aborted. Install a JDK to continue." ;;
		esac
		major=$(java_major)
		if ! have_jdk || [ "${major:-0}" -lt "$need" ]; then
			die "java is ${major:-missing} or javac is not on PATH: point JAVA_HOME and PATH at the JDK just installed."
		fi
	fi
	if [ $# -gt 0 ] || [ -n "$license" ]; then
		if [ -z "$sdkmanager" ]; then bootstrap; fi
		if [ $# -gt 0 ]; then
			echo "Installing into $sdk: $*"
			"$sdkmanager" --sdk_root="$sdk" --install "$@"
		else
			"$sdkmanager" --sdk_root="$sdk" --licenses
		fi
	fi
	if [ -z "$sdk_dir" ] && [ -z "${ANDROID_HOME:-}" ]; then
		printf 'sdk.dir=%s\n' "$sdk" >>local.properties
		echo "Recorded sdk.dir=$sdk in android/local.properties."
	fi
	preflight || exit 1
	echo "Ready: make android builds the APK; its first run downloads Gradle, a JDK 21 and the SDK build tools."
}

verb=${1:-}
[ $# = 0 ] || shift
case $verb in
gradlew)
	preflight || die "make android-toolchain sets these up, asking before it installs a JDK."
	gomod || exit 1
	export ANDROID_HOME="${ANDROID_HOME:-$sdk}"
	exec ./gradlew "$@"
	;;
adb)
	adb=$(command -v adb || echo "$sdk/platform-tools/adb")
	[ -x "$adb" ] || die "No adb on PATH or in $sdk/platform-tools: make android-toolchain installs it, as does the system's adb or android-tools package."
	exec "$adb" "$@"
	;;
install) setup ;;
*) die "usage: android/toolchain.sh gradlew ARGS | adb ARGS | install" ;;
esac
