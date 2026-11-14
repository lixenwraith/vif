//go:build unix

package paths

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestAStoreRefusesWhatIsNoRegularFile: a pipe, named or reached through a link,
// is special and never written: a store refuses it and it stays a pipe.
func TestAStoreRefusesWhatIsNoRegularFile(t *testing.T) {
	dir := t.TempDir()
	pipe, link := filepath.Join(dir, SettingsFile), filepath.Join(dir, "link")
	if err := syscall.Mkfifo(pipe, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pipe, link); err != nil {
		t.Fatal(err)
	}
	for _, at := range []string{pipe, link} {
		if !Special(at) {
			t.Errorf("%s is not special", at)
		}
		if err := (FileStore{Path: at}).Store([]byte("new")); err == nil {
			t.Errorf("%s was written", at)
		}
		if info, err := os.Lstat(pipe); err != nil {
			t.Fatal(err)
		} else if info.Mode().Type() != os.ModeNamedPipe {
			t.Fatalf("after a store to %s the pipe is %v", at, info.Mode())
		}
	}
	if was, err := os.Readlink(link); err != nil || was != pipe {
		t.Errorf("the link now leads to %q, %v", was, err)
	}
}
