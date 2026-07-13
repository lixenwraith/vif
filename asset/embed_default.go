package asset

import (
	"embed"
	"io/fs"
)

//go:embed default/*.toml
var defaultFS embed.FS

// DefaultFSMConfig is the embedded fallback FSM configuration filesystem
var DefaultFSMConfig fs.FS

// DefaultFSMEntry is the entry filename within DefaultFSMConfig
const DefaultFSMEntry = "game.toml"

func init() {
	sub, err := fs.Sub(defaultFS, "default")
	if err != nil {
		panic("asset: embedded FSM config missing")
	}
	DefaultFSMConfig = sub
}
