package render

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
)

type rendererEntry struct {
	renderer SystemRenderer
	priority RenderPriority
	index    int // registration order for stable sort
}

// RenderOrchestrator coordinates the render pipeline.
type RenderOrchestrator struct {
	screen    tcell.Screen
	buffer    *RenderBuffer
	renderers []rendererEntry
	regCount  int
}

// NewRenderOrchestrator creates an orchestrator with the given screen and dimensions.
func NewRenderOrchestrator(screen tcell.Screen, width, height int) *RenderOrchestrator {
	return &RenderOrchestrator{
		screen:    screen,
		buffer:    NewRenderBuffer(width, height),
		renderers: make([]rendererEntry, 0, 16),
	}
}

// Register adds a renderer at the specified priority. Maintains sorted order via insertion sort.
func (o *RenderOrchestrator) Register(r SystemRenderer, priority RenderPriority) {
	entry := rendererEntry{
		renderer: r,
		priority: priority,
		index:    o.regCount,
	}
	o.regCount++

	// Insertion sort: find position and insert
	pos := len(o.renderers)
	for i, e := range o.renderers {
		if priority < e.priority || (priority == e.priority && entry.index < e.index) {
			pos = i
			break
		}
	}

	o.renderers = append(o.renderers, rendererEntry{})
	copy(o.renderers[pos+1:], o.renderers[pos:])
	o.renderers[pos] = entry
}

// Resize updates buffer dimensions and syncs screen.
func (o *RenderOrchestrator) Resize(width, height int) {
	o.buffer.Resize(width, height)
	o.screen.Sync()
}

// RenderFrame executes the render pipeline: clear, render all, flush, show.
func (o *RenderOrchestrator) RenderFrame(ctx RenderContext, world *engine.World) {
	o.buffer.Clear()

	for _, entry := range o.renderers {
		// Skip if renderer implements VisibilityToggle and is not visible
		if vt, ok := entry.renderer.(VisibilityToggle); ok && !vt.IsVisible() {
			continue
		}
		entry.renderer.Render(ctx, world, o.buffer)
	}

	o.buffer.Flush(o.screen)
	o.screen.Show()
}

// Buffer returns the underlying buffer for legacy adapter access.
func (o *RenderOrchestrator) Buffer() *RenderBuffer {
	return o.buffer
}