package engine

import (
	"maps"
	"slices"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// Script-visible ConfigResource fields, the single authority for
// ConfigToVar, ConfigIntCompare, ConfigBoolCompare, and schema export
var configIntAccessors = map[string]func(*World) int64{
	"map_width":  func(w *World) int64 { return int64(w.Resources.Config.MapWidth) },
	"map_height": func(w *World) int64 { return int64(w.Resources.Config.MapHeight) },
	"viewport_width": func(w *World) int64 {
		return drawableWidth(w, w.Resources.Config.ViewportWidth, w.Resources.Config.MapWidth)
	},
	"viewport_height": func(w *World) int64 {
		return drawableWidth(w, w.Resources.Config.ViewportHeight, w.Resources.Config.MapHeight)
	},
	"camera_x":   func(w *World) int64 { return int64(w.Resources.Config.CameraX) },
	"camera_y":   func(w *World) int64 { return int64(w.Resources.Config.CameraY) },
	"color_mode": func(w *World) int64 { return int64(w.Resources.Config.ColorMode) },
}

var configBoolAccessors = map[string]func(*World) bool{
	"crop_on_resize": func(w *World) bool { return w.Resources.Config.CropOnResize },
}

// drawableWidth is the extent a map script means by "the viewport": this terminal
// on a run that owns its bounds, and the latched map otherwise. A script laying a
// level out around it is naming the area it draws on, and under the latch that area
// is the same on every instance (D-14) — a terminal that grew is not.
func drawableWidth(w *World, viewport, mapped int) int64 {
	if w.MapSizeLocal() {
		return int64(viewport)
	}
	return int64(mapped)
}

// replicatedConfigKeys are the script-visible fields every instance agrees on: the
// map bounds, the extent a script draws on, and the crop flag, whose only
// authoritative writer is EventLevelSetup from the map script (D-14). The rest
// describe this terminal — camera and color mode — and diverge silently once the
// map is locked, because a guard branching on one takes a different arm on each
// instance.
var replicatedConfigKeys = map[string]bool{
	"map_width": true, "map_height": true, "crop_on_resize": true,
	"viewport_width": true, "viewport_height": true,
}

// divergentReads latches one warning per non-replicated key. A script reads a
// guard every tick, so warning per read would bury the first one.
var divergentReads = func() map[string]*atomic.Bool {
	m := make(map[string]*atomic.Bool, len(configIntAccessors)+len(configBoolAccessors))
	for _, k := range slices.Concat(ConfigIntFields(), ConfigBoolFields()) {
		if !replicatedConfigKeys[k] {
			m[k] = new(atomic.Bool)
		}
	}
	return m
}()

// ConfigKeyReplicated reports whether every instance derives the same value for
// a script-visible field
func ConfigKeyReplicated(field string) bool { return replicatedConfigKeys[field] }

// noteDivergentRead warns the first time a script reads a per-instance field while
// the map is locked. The key stays readable: D-14 retains the whole surface and
// this only marks where a map script has made itself instance-dependent.
func noteDivergentRead(w *World, field string) {
	seen, watched := divergentReads[field]
	if !watched || !w.SessionShared() || seen.Swap(true) {
		return
	}
	vlog.Warn("fsm", "msg", "non-replicated config read under a locked map",
		"field", field, "rule", "D-14")
}

// ConfigIntAccessor resolves a script-visible int field to a reader
func ConfigIntAccessor(field string) (func(*World) int64, bool) {
	fn, ok := configIntAccessors[field]
	if !ok {
		return nil, false
	}
	return func(w *World) int64 {
		noteDivergentRead(w, field)
		return fn(w)
	}, true
}

// ConfigBoolAccessor resolves a script-visible bool field to a reader
func ConfigBoolAccessor(field string) (func(*World) bool, bool) {
	fn, ok := configBoolAccessors[field]
	if !ok {
		return nil, false
	}
	return func(w *World) bool {
		noteDivergentRead(w, field)
		return fn(w)
	}, true
}

// ConfigIntFields returns the sorted script-visible int field names
func ConfigIntFields() []string { return slices.Sorted(maps.Keys(configIntAccessors)) }

// ConfigBoolFields returns the sorted script-visible bool field names
func ConfigBoolFields() []string { return slices.Sorted(maps.Keys(configBoolAccessors)) }
