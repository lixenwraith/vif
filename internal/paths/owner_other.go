//go:build !unix

package paths

import (
	"io/fs"
	"os"
)

// keepOwner leaves the owner be where a writer sets none
func keepOwner(*os.File, fs.FileInfo) {}
