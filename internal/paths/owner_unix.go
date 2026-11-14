//go:build unix

package paths

import (
	"io/fs"
	"os"
	"syscall"
)

// keepOwner gives f the owner and group info has, as far as the writer may, so a
// root process rewriting a player's file leaves it theirs
func keepOwner(f *os.File, info fs.FileInfo) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		_ = f.Chown(int(st.Uid), int(st.Gid))
	}
}
