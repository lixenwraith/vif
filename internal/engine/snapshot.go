package engine

import (
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// SnapshotContext emits the state an on-demand dump needs that the registry does
// not carry: geometry, camera, cursor placement, and the operator session.
// Five records. context, world and player are shared simulation state and must
// match across instances; view and session are owner-authored, and a consumer
// comparing two instances drops both — see App.SnapshotShared. The player record
// carries the shared roster size only: the local binding lives in view, because a
// remote participant binds a different slot to a different entity.
// Caller MUST hold updateMutex — reads Config, Positions and Systems.
func (ctx *GameContext) SnapshotContext(emit func(sub string, args ...any)) {
	cfg := ctx.World.Resources.Config

	emit(status.SubStat, "msg", "context",
		"map_w", cfg.MapWidth, "map_h", cfg.MapHeight,
		"crop_on_resize", cfg.CropOnResize)

	// View: how this instance is looking at the map, not what the map is.
	view := []any{"msg", "view",
		"mode", int(ctx.GetMode()),
		"screen_w", ctx.Width, "screen_h", ctx.Height,
		"game_x", ctx.GameXOffset, "game_y", ctx.GameYOffset,
		"viewport_w", cfg.ViewportWidth, "viewport_h", cfg.ViewportHeight,
		"camera_x", cfg.CameraX, "camera_y", cfg.CameraY,
		"color_mode", int(cfg.ColorMode)}

	// Roster size is shared; which slot this instance drives, and where that cursor
	// stands, is this instance's binding and travels with the view record.
	//
	// The cell reported is the one this instance is looking at and typing against,
	// so it is the D-18 prediction while this participant's own placements are
	// outstanding, with the depth of that queue beside it. This is the only record
	// the prediction appears in: it is owner-authored input the shared world has not
	// agreed yet, and a consumer comparing two instances drops the whole record.
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
