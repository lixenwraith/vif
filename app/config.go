package app

import (
	"errors"

	"github.com/lixenwraith/terminal"
)

// Config is the resolved startup configuration
// Built from CLI flags by cmd/vi-fighter, or programmatically by embedders
// (map editor, wasm entry) that have no flag set
type Config struct {
	// ColorMode overrides terminal detection when ColorModeSet is true
	ColorMode    terminal.ColorMode
	ColorModeSet bool

	// AudioBackend forces a named backend; "" = auto-detect priority chain
	AudioBackend string

	// AudioMuted is the initial effect mute state
	AudioMuted bool

	// ContentPath is a file path or glob for typing content; "" = default discovery
	ContentPath string

	// GameScript is a game.toml path or a map directory; "" = config discovery
	GameScript string

	// ForceDefault selects the embedded FSM config and ignores GameScript
	ForceDefault bool

	// KeymapPath is a keymap TOML path; "" = keymap discovery
	KeymapPath string
}

// Validate reports configuration conflicts
func (c Config) Validate() error {
	if c.ForceDefault && c.GameScript != "" {
		return errors.New("game script and forced default are mutually exclusive")
	}
	return nil
}
