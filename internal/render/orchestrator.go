package render

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/prof"
)

type rendererEntry struct {
	name     string
	renderer SystemRenderer
	priority RenderPriority
	index    int         // registration order for stable sort
	timer    *prof.Timer // bound the first frame the profiler runs
}

// Surface is what a finished frame is handed to: the terminal in a terminal build,
// and all another host drawing the same cells, a window or an Android view, has to
// provide. Flush must not wait on the host; a held frame is dropped for a newer one.
type Surface interface {
	ColorMode() terminal.ColorMode
	Flush(cells []terminal.Cell, width, height int)
	Sync() // the next frame is drawn whole
}

// RenderOrchestrator coordinates the render pipeline
type RenderOrchestrator struct {
	surface   Surface
	buffer    *RenderBuffer
	renderers []rendererEntry
	regCount  int
	prof      *prof.Profiler // profiler the timers are bound to
}

// NewRenderOrchestrator creates an orchestrator drawing to surface at the given dimensions
func NewRenderOrchestrator(surface Surface, width, height int) *RenderOrchestrator {
	return &RenderOrchestrator{
		surface:   surface,
		buffer:    NewRenderBuffer(surface.ColorMode(), width, height),
		renderers: make([]rendererEntry, 0, 32),
	}
}

// Register adds a renderer at its priority. Maintains sorted order via insertion sort
func (o *RenderOrchestrator) Register(reg Registration) {
	priority := reg.Priority
	entry := rendererEntry{
		name:     reg.Name,
		renderer: reg.Renderer,
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

// SetConsole finishes every frame on a text console's palette
func (o *RenderOrchestrator) SetConsole(c *Console) { o.buffer.SetConsole(c) }

// Size is the terminal the frame buffer is laid out for.
func (o *RenderOrchestrator) Size() (int, int) { return o.buffer.width, o.buffer.height }

// Resize updates buffer dimensions and syncs the surface
func (o *RenderOrchestrator) Resize(width, height int) {
	o.buffer.Resize(width, height)
	o.surface.Sync()
}

// RenderFrame executes the render pipeline: clear, render all, flush, show
func (o *RenderOrchestrator) RenderFrame(ctx RenderContext, world *engine.World) {
	p := world.Resources.Prof
	if p.On() && o.prof != p {
		o.prof = p
		for i := range o.renderers {
			o.renderers[i].timer = p.Timer(prof.KindRender, o.renderers[i].name)
		}
	}
	defer p.BeginPhase(prof.PhaseFrame).End()

	// Buffer is orchestrator-owned; no lock needed for clear
	o.buffer.Clear()

	// A map smaller than the viewport is centred in the game area, and the margin
	// that leaves is not addressable by any simulation coordinate. The clip is set
	// per layer here so no renderer has to re-derive the bound, and the frame outside
	// the map is declared so finalize presents it as out of play, not as empty map.
	playfield := ctx.PlayfieldRect()
	o.buffer.SetVoidRegion(ctx.FrameRect(), playfield, visual.RgbVoid)

	wait := p.BeginPhase(prof.PhaseFrameWait)
	world.Lock()
	wait.End()
	render := p.BeginPhase(prof.PhaseRender)
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
		span := p.Begin(entry.timer)
		entry.renderer.Render(ctx, o.buffer)
		span.End()
	}
	o.buffer.ClearClip()
	render.End()
	world.Unlock()

	// Host I/O outside the world lock: a stalled write must not block the event loop
	defer p.BeginPhase(prof.PhaseFlush).End()
	o.buffer.FlushToTerminal(o.surface)
}

// RenderStatus draws the status bar, the start screen while it is up and the overlay
// the operator opened, every other cell out of play: the frame for a world that is
// not the one to show yet, as a joiner's is until its session's arrives.
func (o *RenderOrchestrator) RenderStatus(ctx RenderContext, world *engine.World) {
	o.buffer.Clear()
	o.buffer.SetVoidRegion(o.buffer.Bounds(), Rect{}, visual.RgbVoid)
	world.Lock()
	for _, entry := range o.renderers {
		switch entry.priority {
		case PriorityStatusBar, PriorityStartScreen, PriorityOverlay:
		default:
			continue
		}
		if vt, ok := entry.renderer.(VisibilityToggle); ok && !vt.IsVisible() {
			continue
		}
		entry.renderer.Render(ctx, o.buffer)
	}
	world.Unlock()
	o.buffer.FlushToTerminal(o.surface)
}
