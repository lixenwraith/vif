package status

import (
	"strconv"
	"strings"
)

// GroupMisc receives metrics registered without a "group.name" key
const GroupMisc = "misc"

// SplitKey maps a stable metric key to its bounded display/log group and field.
// Keys without a separator fall into GroupMisc.
func SplitKey(key string) (group, name string) {
	group, name, _ = splitKey(key)
	return group, name
}

// splitKey also returns the roster slot that controls group visibility.
func splitKey(key string) (group, name, playerSlot string) {
	domain, name, ok := strings.Cut(key, ".")
	if !ok {
		return GroupMisc, key, ""
	}

	// Player cards are slot-scoped, with the weapon inventory split out so
	// every active-slot card stays within the UI cardinality bound.
	if domain == "player" {
		if slot, metric, ok := splitPlayerMetric(name); ok {
			if weapon, ok := strings.CutPrefix(metric, "weapon."); ok {
				return "player." + slot + ".weapon", weapon, slot
			}
			return "player." + slot, metric, slot
		}
	}

	// Allocation-shaped telemetry is easier to scan and tune as one card per
	// owner, instead of being interleaved with behavioral counters.
	if metric, ok := strings.CutPrefix(name, "buf_"); ok {
		return domain + ".buffers", metric, ""
	}

	switch domain {
	case "combat":
		for _, partition := range combatMetricPartitions {
			if metric, ok := strings.CutPrefix(name, partition.prefix); ok {
				return partition.group, metric, ""
			}
		}
		if metric, ok := strings.CutPrefix(name, "chain_"); ok {
			return "combat.chain", metric, ""
		}
		if metric, ok := strings.CutPrefix(name, "effect_"); ok {
			return "combat.effects", metric, ""
		}
		if metric, ok := strings.CutSuffix(name, "_rejects"); ok {
			return "combat.rejects", metric, ""
		}
		if name == "unprofiled" {
			return "combat.rejects", name, ""
		}

	case "death":
		if metric, ok := strings.CutPrefix(name, "batch_"); ok {
			return "death.batch", metric, ""
		}
		if metric, ok := strings.CutSuffix(name, "_rejects"); ok {
			return "death.rejects", metric, ""
		}
		if strings.HasPrefix(name, "missing_") {
			return "death.rejects", name, ""
		}

	case "event":
		if metric, ok := strings.CutPrefix(name, "settle_"); ok {
			return "event.settle", metric, ""
		}

	case "eye":
		if metric, ok := strings.CutPrefix(name, "ga."); ok {
			return "eye.ga", metric, ""
		}
		if strings.HasPrefix(name, "protected_") {
			return "eye.protection", name, ""
		}

	case "fsm":
		if region, metric, ok := strings.Cut(name, "."); ok {
			return "fsm." + region, metric, ""
		}

	case "network":
		switch {
		case name == "state", name == "peers", name == "connected", name == "map_latched":
			return "network.session", name, ""
		case strings.HasPrefix(name, "barrier_"):
			return "network.barrier", strings.TrimPrefix(name, "barrier_"), ""
		case strings.HasPrefix(name, "relay_"), strings.HasPrefix(name, "transport_"):
			return "network.link", name, ""
		}

	case "snapshot":
		// The correction counters are a card of their own: they describe the
		// authority's cadence and how far this instance's prediction was from it,
		// where the rest of the group describes what one capture cost.
		if strings.HasPrefix(name, "correction") {
			return "snapshot.correction", name, ""
		}

	case "quasar", "snake", "storm", "swarm":
		if strings.HasPrefix(name, "protected_") {
			return domain + ".protection", name, ""
		}
	}

	return domain, name, ""
}

// splitPlayerMetric recognizes player.<slot>.<metric> without depending on
// the configured roster bound.
func splitPlayerMetric(name string) (slot, metric string, ok bool) {
	slot, metric, ok = strings.Cut(name, ".")
	if !ok || metric == "" {
		return "", "", false
	}
	n, err := strconv.Atoi(slot)
	if err != nil || n < 0 {
		return "", "", false
	}
	return slot, metric, true
}

var combatMetricPartitions = [...]struct {
	prefix string
	group  string
}{
	{"absorbed_attacker_", "combat.absorbed.attacker"},
	{"absorbed_defender_", "combat.absorbed.defender"},
	{"damage_attacker_", "combat.damage.attacker"},
	{"damage_defender_", "combat.damage.defender"},
}

// activityGatedGroups names group prefixes whose cards are noise until something
// happens: wide per-type partitions that stay zero in most sessions. Declare a
// prefix here rather than teaching a consumer to special-case a group.
var activityGatedGroups = [...]string{
	"combat.absorbed.",
	"combat.damage.",
}

// groupGate returns the visibility rule a group selects. A non-empty slot marks a
// roster-scoped group, which gates on its slot's entity cell.
func groupGate(group, slot string) GroupGate {
	if slot != "" {
		return GateSentinel
	}
	for _, prefix := range activityGatedGroups {
		if strings.HasPrefix(group, prefix) {
			return GateActivity
		}
	}
	return GateAlways
}

// PlayerKey builds a per-slot metric key: PlayerKey(2, "energy.current") = "player.2.energy.current"
// Called at construction only; the resulting cell pointer is cached by the caller.
func PlayerKey(slot int, suffix string) string {
	return "player." + strconv.Itoa(slot) + "." + suffix
}
