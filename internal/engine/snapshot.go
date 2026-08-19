package engine

import (
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// SnapshotContext emits the state an on-demand dump needs that the registry does
// not carry: geometry, camera, cursor placement, and the operator session.
// Four records: context, player and world are simulation state; session is
// operator-owned, per the contract above ResetSessionState. A consumer comparing
// two runs drops the session record — see App.SnapshotSimulation.
// Caller MUST hold updateMutex — reads Config, Positions and Systems.
func (ctx *GameContext) SnapshotContext(emit func(sub string, args ...any)) {
	cfg := ctx.World.Resources.Config

	emit(status.SubStat, "msg", "context",
		"mode", int(ctx.GetMode()),
		"screen_w", ctx.Width, "screen_h", ctx.Height,
		"game_x", ctx.GameXOffset, "game_y", ctx.GameYOffset,
		"map_w", cfg.MapWidth, "map_h", cfg.MapHeight,
		"viewport_w", cfg.ViewportWidth, "viewport_h", cfg.ViewportHeight,
		"camera_x", cfg.CameraX, "camera_y", cfg.CameraY,
		"crop_on_resize", cfg.CropOnResize,
		"color_mode", int(cfg.ColorMode))

	// Cursor placement and ping bounds have no registry mirror
	player := ctx.World.Resources.Player
	args := []any{"msg", "player",
		"entity", uint64(player.Entity),
		"slot", player.LocalSlot(),
		"count", player.Count()}
	if pos, ok := ctx.World.Positions.GetPosition(player.Entity); ok {
		args = append(args, "x", pos.X, "y", pos.Y)
	}
	if ping, ok := ctx.World.Components.Ping.GetComponent(player.Entity); ok {
		args = append(args, "bounds_active", ping.BoundsActive,
			"bounds_rx", ping.BoundsRadiusX, "bounds_ry", ping.BoundsRadiusY)
	}
	emit(status.SubStat, args...)

	emit(status.SubStat, "msg", "world",
		"created", ctx.World.CreatedCount(),
		"destroyed", ctx.World.DestroyedCount(),
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
