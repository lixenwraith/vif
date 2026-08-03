package app

import (
	"errors"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
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

	// ContentPath is a corpus directory or a single content file;
	// "" = discovery, falling back to the embedded corpus
	ContentPath string

	// GameScript is a game.toml path or a map directory; "" = config discovery
	GameScript string

	// ForceDefault selects the embedded FSM config and corpus, ignoring GameScript and ContentPath
	ForceDefault bool

	// KeymapPath is a keymap TOML path; "" = keymap discovery
	KeymapPath string

	// LogScope is the initial scope spec; "" = all
	LogScope string

	// StatTicks overrides the status snapshot period in game ticks; 0 = parameter default
	StatTicks int
}

// Validate reports configuration conflicts
func (c Config) Validate() error {
	if c.ForceDefault && (c.GameScript != "" || c.ContentPath != "") {
		return errors.New("-d is mutually exclusive with -g and -f")
	}
	if c.LogScope != "" {
		if _, err := vlog.ParseScopes(c.LogScope, vlog.ScopeAll); err != nil {
			return err
		}
	}
	if c.StatTicks < 0 {
		return errors.New("-lt must be >= 0")
	}
	return nil
}
