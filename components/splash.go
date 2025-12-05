// FILE: components/splash.go
package components

import "github.com/lixenwraith/vi-fighter/terminal"

// SplashComponent holds state for the singleton splash effect
// Length == 0 indicates inactive state
type SplashComponent struct {
	Content   [8]rune      // Pre-allocated content buffer
	Length    int          // Active character count; 0 = inactive
	Color     terminal.RGB // Flat render color
	AnchorX   int          // Game-relative X (top-left of first char)
	AnchorY   int          // Game-relative Y (top-left of first char)
	StartNano int64        // GameTime.UnixNano() at activation
}
