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
		// Who is authoring and under which generation, plus the two facts that
		// only mean anything beside it: how many handoffs this session has run,
		// and whether this instance is a local fork of it rather than part of it.
		case name == "term", name == "authority", name == "migrations",
			name == "fork", name == "host_lost", name == "migrating",
			name == "term_refused", name == "term_stale", name == "handoff_bytes":
			return "network.authority", name, ""
		case strings.HasPrefix(name, "barrier_"):
			return "network.barrier", strings.TrimPrefix(name, "barrier_"), ""
		case strings.HasPrefix(name, "artifacts_"):
			return "network.barrier", name, ""
		case strings.HasPrefix(name, "relay_"), strings.HasPrefix(name, "transport_"),
			strings.HasPrefix(name, "link_"):
			return "network.link", name, ""
		}

	case "snapshot":
		// Six cards, six questions. The correction counters say how far this
		// instance's prediction was from the authority and how much of the
		// authority arrived; the cadence card says what operating point the link
		// put the session at; the index card what proving equality cost and how
		// often it succeeded outright; the repair card what a selective correction
		// asked for, carried and refused; the replay card what this participant's
		// own actions cost to preserve across one; and what is left describes what
		// one capture cost.
		if metric, ok := strings.CutPrefix(name, "cadence_"); ok {
			return "snapshot.cadence", metric, ""
		}
		if name == "cadence" {
			return "snapshot.cadence", "ticks", ""
		}
		if group, ok := snapshotSelectiveGroup(name); ok {
			return group, name, ""
		}
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

// snapshotSelectiveGroup partitions the Phase 6 correction counters into the three
// cards their questions divide into: what the index cost, what the repair moved,
// and what replaying this participant's own actions across one took.
//
// It is a prefix table rather than three CutPrefix chains because the keys do not
// share one stem — "hash_us" and "selective_bytes" belong with the manifests, and
// "corrections_hash_only" has to be claimed here before the "correction" prefix
// below takes it.
func snapshotSelectiveGroup(name string) (string, bool) {
	switch name {
	case "corrections_hash_only", "hash_us", "selective_bytes",
		"sections_compared", "pages_compared":
		return "snapshot.index", true
	case "request_bytes", "keyframe_fallbacks", "proof_failures", "baseline_refusals":
		return "snapshot.repair", true
	}
	switch {
	case strings.HasPrefix(name, "relay_"):
		return "snapshot.relay", true
	case strings.HasPrefix(name, "manifest"):
		return "snapshot.index", true
	case strings.HasPrefix(name, "shard"), strings.HasPrefix(name, "pages_repaired"),
		strings.HasPrefix(name, "entities_repaired"), strings.HasPrefix(name, "cells_repaired"):
		return "snapshot.repair", true
	case strings.HasPrefix(name, "replay"):
		return "snapshot.replay", true
	}
	return "", false
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
