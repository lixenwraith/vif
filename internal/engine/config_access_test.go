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
	for _, f := range []string{"map_width", "map_height", "crop_on_resize"} {
		if !ConfigKeyReplicated(f) {
			t.Errorf("%s must be replicated: it is map bounds authority (D-14)", f)
		}
	}
	for _, f := range []string{"viewport_width", "viewport_height", "camera_x", "camera_y", "color_mode"} {
		if ConfigKeyReplicated(f) {
			t.Errorf("%s must not be replicated: it describes this terminal", f)
		}
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
