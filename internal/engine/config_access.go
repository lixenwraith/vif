package engine

import (
	"maps"
	"slices"
	"sync/atomic"
)

// Script-visible ConfigResource fields, the single authority for
// ConfigToVar, ConfigIntCompare, ConfigBoolCompare, and schema export. private is
// set for a machine no other instance runs.
var configIntAccessors = map[string]func(w *World, private bool) int64{
	"map_width":  func(w *World, _ bool) int64 { return int64(w.Resources.Config.MapWidth) },
	"map_height": func(w *World, _ bool) int64 { return int64(w.Resources.Config.MapHeight) },
	"viewport_width": func(w *World, private bool) int64 {
		return drawableWidth(w, private, w.Resources.Config.ViewportWidth, w.Resources.Config.MapWidth)
	},
	"viewport_height": func(w *World, private bool) int64 {
		return drawableWidth(w, private, w.Resources.Config.ViewportHeight, w.Resources.Config.MapHeight)
	},
	"camera_x":   func(w *World, _ bool) int64 { return int64(w.Resources.Config.CameraX) },
	"camera_y":   func(w *World, _ bool) int64 { return int64(w.Resources.Config.CameraY) },
	"color_mode": func(w *World, _ bool) int64 { return int64(w.Resources.Config.ColorMode) },
}

var configBoolAccessors = map[string]func(*World) bool{
	"crop_on_resize": func(w *World) bool { return w.Resources.Config.CropOnResize },
}

// drawableWidth is the extent a script means by "the viewport": this terminal for a
// private machine or a run that owns its bounds, and the latched map otherwise, as a
// shared script names the area it draws on, the same on every instance (D-14).
func drawableWidth(w *World, private bool, viewport, mapped int) int64 {
	if private || w.MapSizeLocal() {
		return int64(viewport)
	}
	return int64(mapped)
}

// replicatedConfigKeys are the script-visible fields every instance agrees on: the
// map bounds, the extent a script draws on, and the crop flag, written only by the
// map script's EventLevelSetup and a reset (D-14). The rest describe this terminal
// — camera and color mode — and a guard branching on one takes a different arm on
// each instance.
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
	w.Log().Warn("domain", "msg", "non-replicated config read under a locked map",
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
		return fn(w, false)
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

// PrivateConfigIntAccessor is ConfigIntAccessor for a machine no other instance
// runs, such as a bot's graph: its own terminal's fields are what it means to read,
// so its viewport is the area it sees, never the extent a map script draws on.
func PrivateConfigIntAccessor(field string) (func(*World) int64, bool) {
	fn, ok := configIntAccessors[field]
	if !ok {
		return nil, false
	}
	return func(w *World) int64 { return fn(w, true) }, true
}

// PrivateConfigBoolAccessor is PrivateConfigIntAccessor's bool counterpart
func PrivateConfigBoolAccessor(field string) (func(*World) bool, bool) {
	fn, ok := configBoolAccessors[field]
	return fn, ok
}

// ConfigIntFields returns the sorted script-visible int field names
func ConfigIntFields() []string { return slices.Sorted(maps.Keys(configIntAccessors)) }

// ConfigBoolFields returns the sorted script-visible bool field names
func ConfigBoolFields() []string { return slices.Sorted(maps.Keys(configBoolAccessors)) }
