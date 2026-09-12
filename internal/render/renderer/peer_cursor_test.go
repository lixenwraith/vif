package renderer

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// peerWorld builds a world holding one cursor per slot, with slot 0 local.
func peerWorld(t *testing.T, slots int) (*engine.GameContext, []core.Entity) {
	t.Helper()
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())

	cursors := make([]core.Entity, 0, slots)
	for slot := range slots {
		e := w.CreateEntity(core.DomainShared)
		w.Components.Cursor.SetComponent(e, component.CursorComponent{Slot: uint8(slot)})
		w.Positions.SetPosition(e, component.PositionComponent{X: 5 + slot, Y: 3})
		w.Resources.Player.Bind(uint8(slot), e)
		cursors = append(cursors, e)
	}
	w.Resources.Player.SetLocal(0)
	return ctx, cursors
}

// peerContext is the render context for a world, built directly: the renderer
// reads only the viewport and map geometry from it.
func peerContext(ctx *engine.GameContext) render.RenderContext {
	cfg := ctx.World.Resources.Config
	return render.RenderContext{
		ViewportWidth: cfg.ViewportWidth, ViewportHeight: cfg.ViewportHeight,
		MapWidth: cfg.MapWidth, MapHeight: cfg.MapHeight,
	}
}

// TestPeerCursorsAreDrawnAndTheLocalOneIsNot is the defect this closes: a
// participant used to be visible only through the effects it happened to be
// projecting, so one holding no shield was not on the map at all.
//
// The local cursor is excluded because its own renderer draws it, with the mode
// and the cell colour a player needs and a peer must not borrow.
func TestPeerCursorsAreDrawnAndTheLocalOneIsNot(t *testing.T) {
	t.Parallel()
	gameCtx, cursors := peerWorld(t, 3)
	rc := peerContext(gameCtx)

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 80, 24)
	r := NewPeerCursorRenderer(gameCtx)
	if !r.IsVisible() {
		t.Fatal("the peer renderer hid itself with peers on the map")
	}
	r.Render(rc, buf)

	cellFor := func(e core.Entity) terminal.Cell {
		t.Helper()
		pos, ok := gameCtx.World.Positions.GetPosition(e)
		if !ok {
			t.Fatal("a cursor this test placed has no position")
		}
		x, y, visible := rc.MapToScreen(pos.X, pos.Y)
		if !visible {
			t.Fatalf("cell (%d,%d) is outside a viewport this test placed it inside", pos.X, pos.Y)
		}
		return buf.CellAt(x, y)
	}

	if got := cellFor(cursors[0]).Bg; got == peerCursorColor(0) {
		t.Fatal("the peer renderer drew the local cursor")
	}
	for slot := 1; slot < len(cursors); slot++ {
		want := peerCursorColor(uint8(slot))
		if got := cellFor(cursors[slot]).Bg; got != want {
			t.Fatalf("slot %d drew background %v, want its slot colour %v", slot, got, want)
		}
	}
}

// TestASoloRunDrawsNoPeers keeps the renderer free on the path most runs take.
func TestASoloRunDrawsNoPeers(t *testing.T) {
	t.Parallel()
	gameCtx, _ := peerWorld(t, 1)
	if NewPeerCursorRenderer(gameCtx).IsVisible() {
		t.Fatal("a solo run drew peer cursors")
	}
}

// TestEverySlotHasItsOwnColour is what makes the colour identifying rather than
// decorative: two participants that shared one would read as one participant.
func TestEverySlotHasItsOwnColour(t *testing.T) {
	t.Parallel()
	seen := make(map[color.RGB]uint8, len(visual.RgbPeerCursor))
	for slot := range uint8(len(visual.RgbPeerCursor)) {
		c := peerCursorColor(slot)
		if prev, dup := seen[c]; dup {
			t.Fatalf("slots %d and %d share a colour", prev, slot)
		}
		seen[c] = slot
	}
	// Past the palette a slot wraps rather than losing its colour entirely: an
	// unrecognisable peer is still better than an invisible one.
	if peerCursorColor(uint8(len(visual.RgbPeerCursor))) != peerCursorColor(0) {
		t.Fatal("a slot past the palette did not wrap")
	}
}

func TestPeerShieldBlendsAtThirtyPercent(t *testing.T) {
	t.Parallel()
	gameCtx, cursors := peerWorld(t, 2)
	gameCtx.World.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	rc := peerContext(gameCtx)

	positions := []component.PositionComponent{{X: 20, Y: 10}, {X: 50, Y: 10}}
	for i, cursor := range cursors {
		gameCtx.World.Positions.SetPosition(cursor, positions[i])
		gameCtx.World.Components.Energy.SetComponent(cursor, component.EnergyComponent{Current: 1000})
		gameCtx.World.Components.Shield.SetComponent(cursor, component.ShieldComponent{
			Type: component.ShieldTypePlayer, Active: true,
		})
	}

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 80, 24)
	NewShieldRenderer(gameCtx).Render(rc, buf)

	cfg := &visual.ShieldConfigs[component.ShieldTypePlayer]
	alpha := 0.8 * cfg.MaxOpacity
	wantLocal := color.Screen(visual.RgbBackground, cfg.Color, alpha)
	wantPeer := color.Screen(visual.RgbBackground, cfg.Color, alpha*visual.PeerFieldBlend)
	if got := buf.CellAt(positions[0].X+8, positions[0].Y).Bg; got != wantLocal {
		t.Fatalf("local shield = %v, want %v", got, wantLocal)
	}
	if got := buf.CellAt(positions[1].X+8, positions[1].Y).Bg; got != wantPeer {
		t.Fatalf("peer shield = %v, want %v", got, wantPeer)
	}
}

func TestPeerShieldPaletteBlendsFromThemeBackground(t *testing.T) {
	t.Parallel()
	gameCtx, cursors := peerWorld(t, 2)
	gameCtx.World.Resources.Config.ColorMode = terminal.ColorMode256
	rc := peerContext(gameCtx)

	peer := cursors[1]
	pos := component.PositionComponent{X: 50, Y: 10}
	gameCtx.World.Positions.SetPosition(peer, pos)
	gameCtx.World.Components.Energy.SetComponent(peer, component.EnergyComponent{Current: 1000})
	gameCtx.World.Components.Shield.SetComponent(peer, component.ShieldComponent{
		Type: component.ShieldTypePlayer, Active: true,
	})

	buf := render.NewRenderBuffer(terminal.ColorMode256, 80, 24)
	NewShieldRenderer(gameCtx).Render(rc, buf)

	cfg := &visual.ShieldConfigs[component.ShieldTypePlayer]
	want := color.RGBTo256(color.Screen(visual.RgbBackground, cfg.Color, visual.PeerFieldBlend))
	got := buf.CellAt(pos.X+8, pos.Y)
	if got.Attrs&terminal.AttrBg256 == 0 || got.Bg.R != want {
		t.Fatalf("peer shield palette = (%d, %v), want (%d, bg256)", got.Bg.R, got.Attrs, want)
	}
}

func TestPeerEmberIsDimmerWithoutDarkeningTheTheme(t *testing.T) {
	t.Parallel()
	gameCtx, cursors := peerWorld(t, 2)
	gameCtx.World.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	rc := peerContext(gameCtx)
	positions := []component.PositionComponent{{X: 20, Y: 10}, {X: 50, Y: 10}}
	for i, cursor := range cursors {
		gameCtx.World.Positions.SetPosition(cursor, positions[i])
		gameCtx.World.Components.Heat.SetComponent(cursor, component.HeatComponent{Current: 100, EmberActive: true})
		gameCtx.World.Components.Shield.SetComponent(cursor, component.ShieldComponent{
			Type: component.ShieldTypePlayer, Active: true,
		})
	}

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 80, 24)
	embers := NewEmberRenderer(gameCtx)
	embers.Render(rc, buf)
	local := buf.CellAt(positions[0].X+5, positions[0].Y).Bg
	peer := buf.CellAt(positions[1].X+5, positions[1].Y).Bg
	if got := embers.painters[0].blendScale; got != 1 {
		t.Fatalf("local ember blend = %v, want 1", got)
	}
	if got := embers.painters[1].blendScale; got != visual.PeerFieldBlend {
		t.Fatalf("peer ember blend = %v, want %v", got, visual.PeerFieldBlend)
	}

	baseChannels := [...]uint8{visual.RgbBackground.R, visual.RgbBackground.G, visual.RgbBackground.B}
	localChannels := [...]uint8{local.R, local.G, local.B}
	peerChannels := [...]uint8{peer.R, peer.G, peer.B}
	for channel := range baseChannels {
		if peerChannels[channel] < baseChannels[channel] || peerChannels[channel] >= localChannels[channel] {
			t.Fatalf("channel %d peer/local/base = %d/%d/%d, want peer between theme and local", channel, peerChannels[channel], localChannels[channel], baseChannels[channel])
		}
	}
}

func TestPeerEmberPaletteBlendsFromThemeBackground(t *testing.T) {
	t.Parallel()
	gameCtx, cursors := peerWorld(t, 2)
	gameCtx.World.Resources.Config.ColorMode = terminal.ColorMode256
	rc := peerContext(gameCtx)

	peer := cursors[1]
	pos := component.PositionComponent{X: 50, Y: 10}
	gameCtx.World.Positions.SetPosition(peer, pos)
	gameCtx.World.Components.Heat.SetComponent(peer, component.HeatComponent{Current: 100, EmberActive: true})
	gameCtx.World.Components.Shield.SetComponent(peer, component.ShieldComponent{
		Type: component.ShieldTypePlayer, Active: true,
	})

	buf := render.NewRenderBuffer(terminal.ColorMode256, 80, 24)
	NewEmberRenderer(gameCtx).Render(rc, buf)

	want := color.RGBTo256(color.Screen(visual.RgbBackground, render.HeatGradientLUT[255], visual.PeerFieldBlend))
	got := buf.CellAt(pos.X+5, pos.Y)
	if got.Attrs&terminal.AttrBg256 == 0 || got.Bg.R != want {
		t.Fatalf("peer ember palette = (%d, %v), want (%d, bg256)", got.Bg.R, got.Attrs, want)
	}
}
