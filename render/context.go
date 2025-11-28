package render

import "time"

// RenderContext provides frame state. Passed by value.
type RenderContext struct {
	GameTime    time.Time
	FrameNumber int64
	DeltaTime   float64
	IsPaused    bool
	CursorX     int
	CursorY     int
	GameX       int
	GameY       int
	GameWidth   int
	GameHeight  int
	Width       int
	Height      int
}