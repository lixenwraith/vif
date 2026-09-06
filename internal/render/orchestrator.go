package render

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
)

type rendererEntry struct {
	renderer SystemRenderer
	priority RenderPriority
	index    int // registration order for stable sort
}

// RenderOrchestrator coordinates the render pipeline
type RenderOrchestrator struct {
	term      terminal.Terminal
	buffer    *RenderBuffer
	renderers []rendererEntry
	regCount  int
}

// NewRenderOrchestrator creates an orchestrator with the given terminal and dimensions
func NewRenderOrchestrator(term terminal.Terminal, width, height int) *RenderOrchestrator {
	return &RenderOrchestrator{
		term:      term,
		buffer:    NewRenderBuffer(term.ColorMode(), width, height),
		renderers: make([]rendererEntry, 0, 32),
	}
}

// Register adds a renderer at the specified priority. Maintains sorted order via insertion sort
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

// Resize updates buffer dimensions and syncs terminal
func (o *RenderOrchestrator) Resize(width, height int) {
	o.buffer.Resize(width, height)
	o.term.Sync()
}

// RenderFrame executes the render pipeline: clear, render all, flush, show
func (o *RenderOrchestrator) RenderFrame(ctx RenderContext, world *engine.World) {
	// Buffer is orchestrator-owned; no lock needed for clear
	o.buffer.Clear()

	// A map smaller than the viewport is centred in the game area, and the margin
	// that leaves is not addressable by any simulation coordinate. The clip is set
	// per layer here so no renderer has to re-derive the bound, and the margin is
	// declared so finalize can present it as out of play rather than as empty map.
	playfield := ctx.PlayfieldRect()
	o.buffer.SetVoidRegion(ctx.GameAreaRect(), playfield, visual.RgbVoid)

	world.Lock()
	for _, entry := range o.renderers {
		// Skip if renderer implements VisibilityToggle and is not visible
		if vt, ok := entry.renderer.(VisibilityToggle); ok && !vt.IsVisible() {
			continue
		}
		if entry.priority.ClipsToPlayfield() {
			o.buffer.SetClip(playfield)
		} else {
			o.buffer.ClearClip()
		}
		entry.renderer.Render(ctx, o.buffer)
	}
	o.buffer.ClearClip()
	world.Unlock()

	// Terminal I/O outside the world lock: stalled terminal write mustn't block evel loop
	o.buffer.FlushToTerminal(o.term)
}
