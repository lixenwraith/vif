package asset

import (
	"embed"
	"io/fs"
)

//go:embed content/*.toml
var contentFS embed.FS

// DefaultContent is the embedded fallback corpus filesystem
var DefaultContent fs.FS

func init() {
	sub, err := fs.Sub(contentFS, "content")
	if err != nil {
		panic("asset: embedded content missing")
	}
	DefaultContent = sub
}
