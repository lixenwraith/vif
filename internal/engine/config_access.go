package engine

import (
	"maps"
	"slices"
)

// Script-visible ConfigResource fields, the single authority for
// ConfigToVar, ConfigIntCompare, ConfigBoolCompare, and schema export
var configIntAccessors = map[string]func(*World) int64{
	"map_width":       func(w *World) int64 { return int64(w.Resources.Config.MapWidth) },
	"map_height":      func(w *World) int64 { return int64(w.Resources.Config.MapHeight) },
	"viewport_width":  func(w *World) int64 { return int64(w.Resources.Config.ViewportWidth) },
	"viewport_height": func(w *World) int64 { return int64(w.Resources.Config.ViewportHeight) },
	"camera_x":        func(w *World) int64 { return int64(w.Resources.Config.CameraX) },
	"camera_y":        func(w *World) int64 { return int64(w.Resources.Config.CameraY) },
	"color_mode":      func(w *World) int64 { return int64(w.Resources.Config.ColorMode) },
}

var configBoolAccessors = map[string]func(*World) bool{
	"crop_on_resize": func(w *World) bool { return w.Resources.Config.CropOnResize },
}

// ConfigIntAccessor resolves a script-visible int field to a reader
func ConfigIntAccessor(field string) (func(*World) int64, bool) {
	fn, ok := configIntAccessors[field]
	return fn, ok
}

// ConfigBoolAccessor resolves a script-visible bool field to a reader
func ConfigBoolAccessor(field string) (func(*World) bool, bool) {
	fn, ok := configBoolAccessors[field]
	return fn, ok
}

// ConfigIntFields returns the sorted script-visible int field names
func ConfigIntFields() []string { return slices.Sorted(maps.Keys(configIntAccessors)) }

// ConfigBoolFields returns the sorted script-visible bool field names
func ConfigBoolFields() []string { return slices.Sorted(maps.Keys(configBoolAccessors)) }
