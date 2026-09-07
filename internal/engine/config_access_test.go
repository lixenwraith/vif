package engine

import (
	"slices"
	"testing"
)

// TestEveryConfigKeyIsClassified fails when a script-visible field is added
// without deciding whether every instance derives the same value (D-14). The
// answer drives the divergent-read warning, so an unclassified key is a silent
// per-instance guard rather than a warned one.
func TestEveryConfigKeyIsClassified(t *testing.T) {
	known := map[string]bool{
		"map_width": true, "map_height": true, "crop_on_resize": true,
		"viewport_width": true, "viewport_height": true,
		"camera_x": true, "camera_y": true, "color_mode": true,
	}

	fields := slices.Concat(ConfigIntFields(), ConfigBoolFields())
	for _, f := range fields {
		if !known[f] {
			t.Errorf("%s: new script-visible field; classify it in replicatedConfigKeys "+
				"and list it here", f)
		}
	}
	for f := range known {
		if !slices.Contains(fields, f) {
			t.Errorf("%s: classified but no longer script-visible", f)
		}
	}

	// The split itself, stated once so a silent flip fails rather than drifts.
	for _, f := range []string{"map_width", "map_height", "crop_on_resize",
		"viewport_width", "viewport_height"} {
		if !ConfigKeyReplicated(f) {
			t.Errorf("%s must be replicated: it is the area a map script draws on (D-14)", f)
		}
	}
	for _, f := range []string{"camera_x", "camera_y", "color_mode"} {
		if ConfigKeyReplicated(f) {
			t.Errorf("%s must not be replicated: it describes this terminal", f)
		}
	}
}

// TestTheDrawableExtentFollowsTheLatch is the D-14 read a map script makes. A run
// that owns its bounds draws on its terminal; under the latch the terminal is one
// instance's and the map is everyone's, so a script sizing a level from "the
// viewport" must not resize the shared map to whichever terminal grew.
func TestTheDrawableExtentFollowsTheLatch(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 200, 60, NewManualClock())
	w.Resources.Config.MapWidth, w.Resources.Config.MapHeight = 120, 40
	w.Resources.Config.ViewportWidth, w.Resources.Config.ViewportHeight = 200, 60

	read := func(field string) int64 {
		fn, ok := ConfigIntAccessor(field)
		if !ok {
			t.Fatalf("%s does not resolve", field)
		}
		return fn(w)
	}
	if got := read("viewport_width"); got != 200 {
		t.Fatalf("a run that owns its bounds draws on %d columns, want its terminal's 200", got)
	}
	w.MarkSessionShared()
	if got, got2 := read("viewport_width"), read("viewport_height"); got != 120 || got2 != 40 {
		t.Fatalf("under the latch a script draws on %dx%d, want the map's 120x40", got, got2)
	}
}

// TestConfigAccessorsWatchEveryNonReplicatedKey asserts the warning covers the
// whole non-replicated surface, so adding a key cannot quietly escape it.
func TestConfigAccessorsWatchEveryNonReplicatedKey(t *testing.T) {
	for _, f := range slices.Concat(ConfigIntFields(), ConfigBoolFields()) {
		_, watched := divergentReads[f]
		if watched == ConfigKeyReplicated(f) {
			t.Errorf("%s: replicated=%v but watched=%v; the two must be opposites",
				f, ConfigKeyReplicated(f), watched)
		}
	}
}

// TestConfigAccessorsResolve checks the wrapper did not break resolution, in both
// directions, since every reader now goes through it.
func TestConfigAccessorsResolve(t *testing.T) {
	if _, ok := ConfigIntAccessor("map_width"); !ok {
		t.Error("map_width does not resolve")
	}
	if _, ok := ConfigBoolAccessor("crop_on_resize"); !ok {
		t.Error("crop_on_resize does not resolve")
	}
	if _, ok := ConfigIntAccessor("no_such_field"); ok {
		t.Error("an unknown int field resolved")
	}
	if _, ok := ConfigBoolAccessor("no_such_field"); ok {
		t.Error("an unknown bool field resolved")
	}
}
