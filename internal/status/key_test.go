package status

import (
	"slices"
	"testing"
)

func TestSplitKeyUsesBoundedSemanticGroups(t *testing.T) {
	tests := []struct {
		key, group, name string
	}{
		{"adapt.buf_pending_deaths_hwm", "adapt.buffers", "pending_deaths_hwm"},
		{"combat.absorbed_attacker_cursor", "combat.absorbed.attacker", "cursor"},
		{"combat.damage_defender_drain", "combat.damage.defender", "drain"},
		{"combat.chain_depth_max", "combat.chain", "depth_max"},
		{"combat.effect_stun", "combat.effects", "stun"},
		{"combat.immune_rejects", "combat.rejects", "immune"},
		{"death.batch_entities_total", "death.batch", "entities_total"},
		{"death.missing_entities", "death.rejects", "missing_entities"},
		{"event.settle_pre", "event.settle", "pre"},
		{"eye.ga.generation", "eye.ga", "generation"},
		{"fsm.main.state", "fsm.main", "state"},
		{"fsm.state", "fsm", "state"},
		{"network.map_latched", "network.session", "map_latched"},
		{"player.0.boost.active", "player.0", "boost.active"},
		{"player.0.weapon.rod", "player.0.weapon", "rod"},
		{"player.count", "player", "count"},
		{"storm.protected_player_rejects", "storm.protection", "protected_player_rejects"},
		{"undotted", GroupMisc, "undotted"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			group, name := SplitKey(tt.key)
			if group != tt.group || name != tt.name {
				t.Fatalf("SplitKey(%q) = (%q, %q), want (%q, %q)",
					tt.key, group, name, tt.group, tt.name)
			}
		})
	}
}

func TestInactivePlayerGroupsStayOutOfViewsAndSnapshots(t *testing.T) {
	reg := NewRegistry()
	player0 := reg.Ints.Get("player.0.entity")
	reg.Ints.Get("player.0.energy.current")
	reg.Bools.Get("player.0.weapon.rod")
	player1 := reg.Ints.Get("player.1.entity")
	reg.Ints.Get("player.1.energy.current")
	reg.Bools.Get("player.1.weapon.rod")
	reg.Ints.Get("player.count")
	reg.Freeze()

	if groups := visibleGroupNames(reg); slices.Contains(groups, "player.0") || slices.Contains(groups, "player.1") {
		t.Fatalf("inactive player groups visible: %v", groups)
	}
	view, ok := reg.GroupView("player.0.weapon")
	if !ok || view.Visible() {
		t.Fatalf("inactive bound view = (%v, %v), want present but hidden", ok, view.Visible())
	}

	player0.Store(7)
	groups := visibleGroupNames(reg)
	for _, want := range []string{"player", "player.0", "player.0.weapon"} {
		if !slices.Contains(groups, want) {
			t.Errorf("active slot group %q missing from %v", want, groups)
		}
	}
	if slices.Contains(groups, "player.1") || slices.Contains(groups, "player.1.weapon") {
		t.Errorf("inactive slot 1 leaked into %v", groups)
	}
	if !view.Visible() {
		t.Fatal("bound player view did not become visible when its entity appeared")
	}

	player0.Store(0)
	player1.Store(9)
	snapshot := snapshotGroupNames(reg)
	if slices.Contains(snapshot, "player.0") || slices.Contains(snapshot, "player.0.weapon") {
		t.Errorf("inactive slot 0 leaked into snapshot: %v", snapshot)
	}
	for _, want := range []string{"player.1", "player.1.weapon"} {
		if !slices.Contains(snapshot, want) {
			t.Errorf("active snapshot group %q missing from %v", want, snapshot)
		}
	}
}

func TestRecorderKeepsPlayerGroupWhenActiveInsideWindow(t *testing.T) {
	reg := NewRegistry()
	reg.Ints.Get("player.0.entity")
	reg.Bools.Get("player.0.weapon.rod")
	reg.Freeze()

	rc := &Recorder{depth: 2}
	rc.bind(reg.groups())
	rc.slots = append(rc.slots[:0], 0, 1)

	var weapon *recGroup
	for i := range rc.groups {
		if rc.groups[i].name == "player.0.weapon" {
			weapon = &rc.groups[i]
			break
		}
	}
	if weapon == nil || weapon.visible < 0 {
		t.Fatal("player weapon recorder group is not bound to its entity column")
	}
	if rc.groupVisible(weapon, 2) {
		t.Fatal("never-active player group was retained")
	}

	stride := len(rc.srcI)
	rc.bufI[rc.slots[1]*stride+weapon.visible] = 42
	if !rc.groupVisible(weapon, 2) {
		t.Fatal("player group active in the second sample was suppressed")
	}
}

func visibleGroupNames(reg *Registry) []string {
	views := reg.VisibleViews()
	names := make([]string, len(views))
	for i := range views {
		names[i] = views[i].Name()
	}
	return names
}

func snapshotGroupNames(reg *Registry) []string {
	var names []string
	reg.Snapshot(func(_ string, args ...any) {
		if len(args) >= 2 && args[0] == "msg" {
			names = append(names, args[1].(string))
		}
	})
	return names
}

func TestActivityGatedGroupsAppearOnlyWhenTheyHaveReadings(t *testing.T) {
	reg := NewRegistry()
	dealt := reg.Ints.Get("combat.damage_attacker_cursor")
	reg.Ints.Get("combat.damage_attacker_drain")
	reg.Ints.Get("combat.hits_direct")
	reg.Freeze()

	if groups := visibleGroupNames(reg); slices.Contains(groups, "combat.damage.attacker") {
		t.Fatalf("silent partition visible: %v", groups)
	}
	if snapshot := snapshotGroupNames(reg); slices.Contains(snapshot, "combat.damage.attacker") {
		t.Fatalf("silent partition emitted: %v", snapshot)
	}
	if !slices.Contains(visibleGroupNames(reg), "combat") {
		t.Fatal("ungated combat group was hidden")
	}

	dealt.Store(7)
	if !slices.Contains(visibleGroupNames(reg), "combat.damage.attacker") {
		t.Fatal("partition stayed hidden after damage was recorded")
	}
	if !slices.Contains(snapshotGroupNames(reg), "combat.damage.attacker") {
		t.Fatal("partition stayed out of the snapshot after damage was recorded")
	}
}

func TestRecorderDropsSilentActivityGroupFromWindow(t *testing.T) {
	reg := NewRegistry()
	reg.Ints.Get("combat.damage_attacker_cursor")
	reg.Freeze()

	rc := &Recorder{depth: 2}
	rc.bind(reg.groups())
	rc.slots = append(rc.slots[:0], 0, 1)

	var damage *recGroup
	for i := range rc.groups {
		if rc.groups[i].name == "combat.damage.attacker" {
			damage = &rc.groups[i]
			break
		}
	}
	if damage == nil || damage.gate != GateActivity {
		t.Fatal("combat damage partition is not activity gated")
	}
	if rc.groupVisible(damage, 2) {
		t.Fatal("all-zero window was retained")
	}

	stride := len(rc.srcI)
	rc.bufI[rc.slots[1]*stride+damage.cols[0].idx] = 3
	if !rc.groupVisible(damage, 2) {
		t.Fatal("group active in the second sample was suppressed")
	}
}
