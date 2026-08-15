package engine

import (
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// SnapshotContext emits the state an on-demand dump needs that the registry
// does not carry: geometry, camera, pause and cursor placement.
// Caller MUST hold updateMutex — reads Config, Positions and Systems.
func (ctx *GameContext) SnapshotContext(emit func(sub string, args ...any)) {
	cfg := ctx.World.Resources.Config

	emit(status.SubStat, "msg", "context",
		"frame", ctx.GetFrameNumber(),
		"paused", ctx.TimeCtl.IsPaused(),
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
	args := []any{"msg", "player", "entity", uint64(player.Entity)}
	if pos, ok := ctx.World.Positions.GetPosition(player.Entity); ok {
		args = append(args, "x", pos.X, "y", pos.Y)
	}
	bounds := player.GetBounds()
	args = append(args, "bounds_active", bounds.Active,
		"bounds_rx", bounds.RadiusX, "bounds_ry", bounds.RadiusY)
	emit(status.SubStat, args...)

	emit(status.SubStat, "msg", "world",
		"created", ctx.World.CreatedCount(),
		"destroyed", ctx.World.DestroyedCount(),
		"systems", len(ctx.World.Systems()),
		"macro_recording", ctx.MacroRecording.Load(),
		"macro_playing", ctx.MacroPlaying.Load(),
		"mouse_free", ctx.MouseFreeMode.Load(),
		"mouse_disabled", ctx.MouseDisabled.Load(),
		"auto_fire", ctx.AutoFire.Load())
}
