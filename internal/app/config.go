package app

import (
	"errors"
	"fmt"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// Headless terminal-equivalent dimensions applied when Width or Height is unset
const (
	HeadlessDefaultWidth  = 80
	HeadlessDefaultHeight = 24
)

// Smallest game area a headless run accepts; below it spawn placement and
// cursor exclusion degenerate to a single cell
const (
	HeadlessMinViewportWidth  = 20
	HeadlessMinViewportHeight = 5
)

// Config is the resolved startup configuration
// Built from CLI flags by cmd/vi-fighter, or programmatically by embedders
// (map editor, wasm entry, headless harness) that have no flag set
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

	// StatTicks overrides the status snapshot period in game ticks;
	// 0 = parameter default, negative = disabled
	StatTicks int

	// RecTicks overrides the flight-recorder depth in game ticks;
	// 0 = parameter default, negative = disabled
	RecTicks int

	// TimeScaleSpec is the initial simulation rate ladder token; "" = real time
	TimeScaleSpec string

	// System seed for RNG
	Seed uint64

	// Headless runs with no terminal, audio or renderer, on a manual clock
	// advanced only by ClockScheduler.RunTicks
	Headless bool

	// Journal enables the replay journal, written to its own file
	Journal bool

	// JournalSink overrides the journal destination; nil opens the vlog journal
	// file. Harnesses set it to capture records in memory.
	JournalSink event.JournalSink

	// Width and Height are the terminal-equivalent dimensions a headless run
	// assumes; margins apply as usual, so the viewport is smaller than these.
	// Ignored when a terminal is present; zero selects the defaults.
	Width, Height int
}

// Normalize fills unset fields that carry a defined default
func (c *Config) Normalize() {
	if !c.Headless {
		return
	}
	if c.Width == 0 {
		c.Width = HeadlessDefaultWidth
	}
	if c.Height == 0 {
		c.Height = HeadlessDefaultHeight
	}
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
	if c.TimeScaleSpec != "" {
		if _, ok := engine.ParseScale(c.TimeScaleSpec); !ok {
			return fmt.Errorf("-speed %q is not a ladder rate (1/8 1/4 1/2 1 2 4 8)", c.TimeScaleSpec)
		}
	}
	if c.Headless {
		return c.validateHeadless()
	}
	return nil
}

// validateHeadless rejects settings a headless run cannot honour, rather than
// accepting a flag that would silently do nothing
func (c Config) validateHeadless() error {
	if c.ColorModeSet {
		return errors.New("headless: color mode is unused, no terminal is created")
	}
	if c.AudioBackend != "" {
		return errors.New("headless: audio backend is unused, no audio service is created")
	}
	if c.TimeScaleSpec != "" {
		return errors.New("headless: the manual clock is driven by Tick, not by a rate")
	}

	vw := c.Width - parameter.LeftMargin
	vh := c.Height - parameter.TopMargin - parameter.BottomMargin
	if vw < HeadlessMinViewportWidth || vh < HeadlessMinViewportHeight {
		return fmt.Errorf("headless: %dx%d yields a %dx%d viewport, below the %dx%d minimum",
			c.Width, c.Height, vw, vh, HeadlessMinViewportWidth, HeadlessMinViewportHeight)
	}
	return nil
}
