package engine

import (
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/status"
)

// SnapshotContext emits the five records an on-demand dump needs that the registry
// does not carry. context, world and player are shared simulation state and must
// match across instances; view and session are owner-authored, and a consumer
// comparing two instances drops both (App.SnapshotShared).
// Caller MUST hold updateMutex — reads Config, Positions and Systems.
func (ctx *GameContext) SnapshotContext(emit func(sub string, args ...any)) {
	cfg := ctx.World.Resources.Config

	emit(status.SubStat, "msg", "context",
		"map_w", cfg.MapWidth, "map_h", cfg.MapHeight,
		"crop_on_resize", cfg.CropOnResize, "cell_aspect", float64(cfg.CellAspect))

	// View: how this instance is looking at the map, not what the map is.
	view := []any{"msg", "view",
		"mode", int(ctx.GetMode()),
		"screen_w", ctx.Width, "screen_h", ctx.Height,
		"game_x", ctx.GameXOffset, "game_y", ctx.GameYOffset,
		"viewport_w", cfg.ViewportWidth, "viewport_h", cfg.ViewportHeight,
		"camera_x", cfg.CameraX, "camera_y", cfg.CameraY,
		"color_mode", int(cfg.ColorMode)}

	// Roster size is shared; which slot this instance drives, and where that cursor
	// stands, is this instance's binding and travels with the view record: the cell
	// it types against, the D-18 prediction while its own placements are outstanding,
	// with the queue's depth. No other record carries the prediction, input the
	// shared world has not agreed yet, so a comparison drops the whole record.
	player := ctx.World.Resources.Player
	view = append(view,
		"entity", uint64(player.Entity),
		"slot", player.LocalSlot(),
		"predicted", player.PredictedDepth())
	if pos, ok := ctx.World.LocalCursor(); ok {
		view = append(view, "x", pos.X, "y", pos.Y)
	}
	// Ping bounds are cursor view state, so they travel with the view record
	if ping, ok := ctx.World.Components.Ping.GetComponent(player.Entity); ok {
		view = append(view, "bounds_active", ping.BoundsActive,
			"bounds_rx", ping.BoundsRadiusX, "bounds_ry", ping.BoundsRadiusY)
	}
	emit(status.SubStat, "msg", "player", "count", player.Count())
	emit(status.SubStat, view...)

	emit(status.SubStat, "msg", "world",
		"created", ctx.World.CreatedCountDomain(core.DomainShared),
		"destroyed", ctx.World.DestroyedCountDomain(core.DomainShared),
		"created_local", ctx.World.CreatedCountDomain(core.DomainPlayer),
		"destroyed_local", ctx.World.DestroyedCountDomain(core.DomainPlayer),
		"systems", len(ctx.World.Systems()))

	// Session: how the run is being observed and driven, not the run itself.
	// None of it is event-driven, so none of it replays; none of it is read by a
	// system either, so nothing in the simulation depends on it.
	emit(status.SubStat, "msg", "session",
		"frame", ctx.GetFrameNumber(),
		"paused", ctx.TimeCtl.IsPaused(),
		"macro_recording", ctx.MacroRecording.Load(),
		"macro_playing", ctx.MacroPlaying.Load(),
		"mouse_free", ctx.MouseFreeMode.Load(),
		"mouse_disabled", ctx.MouseDisabled.Load(),
		"auto_fire", ctx.AutoFire.Load())
}

// SharedAllocator reads the shared allocator's counters, a capture's meta section
// without any row. Caller MUST hold updateMutex.
func (w *World) SharedAllocator() (next uint64, created, destroyed int64) {
	return w.nextEntityID[core.DomainShared], w.createdCount[core.DomainShared].Load(), w.destroyedCount[core.DomainShared].Load()
}
