//go:build !unix

package paths

import "io/fs"

// keepOwner leaves the owner be where a writer sets none
func keepOwner(string, fs.FileInfo) {}
