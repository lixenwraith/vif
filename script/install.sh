#!/bin/sh
# make, with the one flag it cannot take itself: install.sh <make's own line>
# -purge. An install goal then first removes what an earlier install put in its
# config root, a player's bot files, keymap.toml and vif.toml with it
# (doc/filesystem-layout.md §4).
set -eu
for arg in "$@"; do
	shift
	case $arg in -purge) arg=VIF_PURGE=1 ;; esac
	set -- "$@" "$arg"
done
exec make "$@"
