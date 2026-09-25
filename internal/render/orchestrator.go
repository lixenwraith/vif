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

// RenderOrchestrator coordinates the render pipeline
type RenderOrchestrator struct {
	term      terminal.Terminal
	buffer    *RenderBuffer
	renderers []rendererEntry
	regCount  int
	prof      *prof.Profiler // profiler the timers are bound to
}

// NewRenderOrchestrator creates an orchestrator with the given terminal and dimensions
func NewRenderOrchestrator(term terminal.Terminal, width, height int) *RenderOrchestrator {
	return &RenderOrchestrator{
		term:      term,
		buffer:    NewRenderBuffer(term.ColorMode(), width, height),
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

// Resize updates buffer dimensions and syncs terminal
func (o *RenderOrchestrator) Resize(width, height int) {
	o.buffer.Resize(width, height)
	o.term.Sync()
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
	// per layer here so no renderer has to re-derive the bound, and the margin is
	// declared so finalize can present it as out of play rather than as empty map.
	playfield := ctx.PlayfieldRect()
	o.buffer.SetVoidRegion(ctx.GameAreaRect(), playfield, visual.RgbVoid)

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

	// Terminal I/O outside the world lock: stalled terminal write mustn't block evel loop
	defer p.BeginPhase(prof.PhaseFlush).End()
	o.buffer.FlushToTerminal(o.term)
}
