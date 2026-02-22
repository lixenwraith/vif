package system

import (
	"slices"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// TargetGroup holds a combat target with hit members for area attacks
type TargetGroup struct {
	Target  core.Entity   // Header for composites, entity itself for singles
	Members []core.Entity // Members within area, or entity itself for singles
}

// TargetAssignment holds a resolved target with closest member for directed attacks
type TargetAssignment struct {
	Target core.Entity // Header for composites, entity itself for singles
	Hit    core.Entity // Closest member, or entity itself for singles
	DistSq int64       // Squared distance from query origin to Hit
}

// ResolveTargetFromEntity resolves combat target chain for a single entity found at a position
// Returns (target, hit, valid):
//   - target: header entity for composite members, entity itself for unit headers and singles
//   - hit: the input entity (spatial occupant that was encountered)
//   - valid: true if entity is a combat-relevant target
//
// Container headers and ablative headers return invalid (not directly targetable)
// selfEntity is excluded. Does not filter by ownership
func ResolveTargetFromEntity(w *engine.World, entity, selfEntity core.Entity) (core.Entity, core.Entity, bool) {
	if entity == 0 || entity == selfEntity {
		return 0, 0, false
	}

	// Header entity — route by CompositeType
	if headerComp, ok := w.Components.Header.GetComponent(entity); ok {
		switch headerComp.Type {
		case component.CompositeTypeUnit:
			return entity, entity, true
		case component.CompositeTypeAblative, component.CompositeTypeContainer:
			return 0, 0, false
		}
	}

	// Member entity — resolve upward to header
	if memberComp, ok := w.Components.Member.GetComponent(entity); ok {
		headerEntity := memberComp.HeaderEntity
		headerComp, ok := w.Components.Header.GetComponent(headerEntity)
		if !ok {
			return 0, 0, false
		}
		if !w.Components.Combat.HasEntity(headerEntity) {
			return 0, 0, false
		}
		switch headerComp.Type {
		case component.CompositeTypeUnit, component.CompositeTypeAblative:
			return headerEntity, entity, true
		default:
			return 0, 0, false
		}
	}

	// Simple combat entity (drain, etc.)
	if w.Components.Combat.HasEntity(entity) {
		return entity, entity, true
	}

	return 0, 0, false
}

// HasCombatTargetAt returns true if any enemy combat entity exists at (x, y).
// Excludes selfEntity from resolution and ownerEntity-owned entities from results.
func HasCombatTargetAt(w *engine.World, x, y int, selfEntity, ownerEntity core.Entity) bool {
	entities := w.Positions.GetAllEntityAt(x, y)
	for _, e := range entities {
		target, _, valid := ResolveTargetFromEntity(w, e, selfEntity)
		if !valid {
			continue
		}
		if isOwnedBy(w, target, ownerEntity) {
			continue
		}
		return true
	}
	return false
}

// FindTargetsInEllipse returns all combat targets with members inside the ellipse
// Results grouped by target: one TargetGroup per composite header or single entity
// ownerEntity-owned entities excluded
//
// Iterates Combat store (singles) and Member store (composites) for species-agnostic resolution
func FindTargetsInEllipse(w *engine.World, cx, cy int, invRxSq, invRySq int64, ownerEntity core.Entity) []TargetGroup {
	groups := make(map[core.Entity]*TargetGroup)

	// 1. Simple combat entities (no Header, no Member component)
	for _, e := range w.Components.Combat.GetAllEntities() {
		if w.Components.Header.HasEntity(e) || w.Components.Member.HasEntity(e) {
			continue
		}
		if isOwnedBy(w, e, ownerEntity) {
			continue
		}
		pos, ok := w.Positions.GetPosition(e)
		if !ok || !vmath.EllipseContainsPoint(pos.X, pos.Y, cx, cy, invRxSq, invRySq) {
			continue
		}
		groups[e] = &TargetGroup{Target: e, Members: []core.Entity{e}}
	}

	// 2. Composite members — covers Unit hitbox members and Ablative combat members.
	// Container children are filtered by header type check.
	for _, memberEntity := range w.Components.Member.GetAllEntities() {
		memberComp, ok := w.Components.Member.GetComponent(memberEntity)
		if !ok {
			continue
		}
		headerEntity := memberComp.HeaderEntity
		headerComp, ok := w.Components.Header.GetComponent(headerEntity)
		if !ok || headerComp.Type == component.CompositeTypeContainer {
			continue
		}
		if !w.Components.Combat.HasEntity(headerEntity) {
			continue
		}
		if isOwnedBy(w, headerEntity, ownerEntity) {
			continue
		}
		pos, ok := w.Positions.GetPosition(memberEntity)
		if !ok || !vmath.EllipseContainsPoint(pos.X, pos.Y, cx, cy, invRxSq, invRySq) {
			continue
		}

		if g, exists := groups[headerEntity]; exists {
			g.Members = append(g.Members, memberEntity)
		} else {
			groups[headerEntity] = &TargetGroup{
				Target:  headerEntity,
				Members: []core.Entity{memberEntity},
			}
		}
	}

	result := make([]TargetGroup, 0, len(groups))
	for _, g := range groups {
		result = append(result, *g)
	}
	return result
}

// FindNearestTargets returns up to count targets, composite-grouped with closest member per header
// Composites prioritized over distance-sorted singles.
// If count exceeds available targets, results cycle through available targets (overflow distribution)
// ownerEntity-owned entities excluded
func FindNearestTargets(w *engine.World, fromX, fromY int64, count int, ownerEntity core.Entity) []TargetAssignment {
	if count <= 0 {
		return nil
	}

	composites := make(map[core.Entity]*TargetAssignment)
	var singles []TargetAssignment

	// 1. Simple combat entities
	for _, e := range w.Components.Combat.GetAllEntities() {
		if w.Components.Header.HasEntity(e) || w.Components.Member.HasEntity(e) {
			continue
		}
		if isOwnedBy(w, e, ownerEntity) {
			continue
		}
		pos, ok := w.Positions.GetPosition(e)
		if !ok {
			continue
		}
		px, py := vmath.CenteredFromGrid(pos.X, pos.Y)
		distSq := vmath.MagnitudeSq(px-fromX, py-fromY)
		singles = append(singles, TargetAssignment{Target: e, Hit: e, DistSq: distSq})
	}

	// 2. Composite members — closest member per header
	for _, memberEntity := range w.Components.Member.GetAllEntities() {
		memberComp, ok := w.Components.Member.GetComponent(memberEntity)
		if !ok {
			continue
		}
		headerEntity := memberComp.HeaderEntity
		headerComp, ok := w.Components.Header.GetComponent(headerEntity)
		if !ok || headerComp.Type == component.CompositeTypeContainer {
			continue
		}
		if !w.Components.Combat.HasEntity(headerEntity) {
			continue
		}
		if isOwnedBy(w, headerEntity, ownerEntity) {
			continue
		}
		pos, ok := w.Positions.GetPosition(memberEntity)
		if !ok {
			continue
		}
		px, py := vmath.CenteredFromGrid(pos.X, pos.Y)
		distSq := vmath.MagnitudeSq(px-fromX, py-fromY)

		if existing, exists := composites[headerEntity]; exists {
			if distSq < existing.DistSq {
				existing.Hit = memberEntity
				existing.DistSq = distSq
			}
		} else {
			composites[headerEntity] = &TargetAssignment{
				Target: headerEntity,
				Hit:    memberEntity,
				DistSq: distSq,
			}
		}
	}

	// Composites first (priority, distance-sorted), then singles by distance
	compositesSlice := make([]TargetAssignment, 0, len(composites))
	for _, a := range composites {
		compositesSlice = append(compositesSlice, *a)
	}
	slices.SortStableFunc(compositesSlice, func(a, b TargetAssignment) int {
		if a.DistSq < b.DistSq {
			return -1
		}
		if a.DistSq > b.DistSq {
			return 1
		}
		return 0
	})

	result := make([]TargetAssignment, 0, len(compositesSlice)+len(singles))
	result = append(result, compositesSlice...)

	slices.SortStableFunc(singles, func(a, b TargetAssignment) int {
		if a.DistSq < b.DistSq {
			return -1
		}
		if a.DistSq > b.DistSq {
			return 1
		}
		return 0
	})
	result = append(result, singles...)

	if len(result) == 0 {
		return nil
	}
	if len(result) >= count {
		return result[:count]
	}

	// Overflow: cycle through available targets
	final := make([]TargetAssignment, count)
	copy(final, result)
	for i := len(result); i < count; i++ {
		final[i] = result[i%len(result)]
	}
	return final
}

// isOwnedBy returns true if entity is the owner or its CombatComponent,OwnerEntity matches
func isOwnedBy(w *engine.World, entity, ownerEntity core.Entity) bool {
	if entity == ownerEntity {
		return true
	}
	combat, ok := w.Components.Combat.GetComponent(entity)
	if !ok {
		return false
	}
	return combat.OwnerEntity == ownerEntity
}

// ResolveClosestMember finds the nearest living member of a composite header
func ResolveClosestMember(w *engine.World, headerEntity core.Entity, fromX, fromY int64) (core.Entity, int64, int64, bool) {
	headerComp, ok := w.Components.Header.GetComponent(headerEntity)
	if !ok {
		return 0, 0, 0, false
	}

	var best core.Entity
	var bestX, bestY int64
	var bestDistSq int64 = -1

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}
		pos, ok := w.Positions.GetPosition(member.Entity)
		if !ok {
			continue
		}
		mx, my := vmath.CenteredFromGrid(pos.X, pos.Y)
		d := vmath.MagnitudeSq(mx-fromX, my-fromY)
		if bestDistSq < 0 || d < bestDistSq {
			bestDistSq = d
			best = member.Entity
			bestX, bestY = mx, my
		}
	}

	if best == 0 {
		return 0, 0, 0, false
	}
	return best, bestX, bestY, true
}

// resolveBaseTarget returns the grid-coordinate target for an entity based on its group
// Falls back to cursor position for group 0 or uninitialized groups
func resolveBaseTarget(w *engine.World, entity core.Entity) (x, y int, valid bool) {
	groupID := uint8(0)
	if tc, ok := w.Components.Target.GetComponent(entity); ok {
		groupID = tc.GroupID
	}

	state := w.Resources.Target.GetGroup(groupID)
	if state.Valid {
		return state.PosX, state.PosY, true
	}

	// Fallback: cursor
	if pos, ok := w.Positions.GetPosition(w.Resources.Player.Entity); ok {
		return pos.X, pos.Y, true
	}
	return 0, 0, false
}

// ResolveMovementTarget computes the effective homing target for a kinetic entity
// Encapsulates the target resolution + navigation routing pattern shared by all species
// Returns (targetX, targetY int64, usingDirectPath bool)
func ResolveMovementTarget(w *engine.World, entity core.Entity, kineticComp *component.KineticComponent) (int64, int64, bool) {
	baseX, baseY, ok := resolveBaseTarget(w, entity)
	if !ok {
		return kineticComp.PreciseX, kineticComp.PreciseY, true
	}

	baseXFixed, baseYFixed := vmath.CenteredFromGrid(baseX, baseY)

	navComp, hasNav := w.Components.Navigation.GetComponent(entity)
	if !hasNav {
		return baseXFixed, baseYFixed, true
	}

	if navComp.HasDirectPath {
		return baseXFixed, baseYFixed, true
	}

	if navComp.FlowX != 0 || navComp.FlowY != 0 {
		tx := kineticComp.PreciseX + vmath.Mul(navComp.FlowX, navComp.FlowLookahead)
		ty := kineticComp.PreciseY + vmath.Mul(navComp.FlowY, navComp.FlowLookahead)
		return tx, ty, false
	}

	// Flow zero (stuck): snap to target if close
	dist := vmath.DistanceApprox(kineticComp.PreciseX-baseXFixed, kineticComp.PreciseY-baseYFixed)
	if dist < vmath.FromInt(2) {
		return baseXFixed, baseYFixed, true
	}

	return baseXFixed, baseYFixed, true
}

// ResolveBaseTargetFixed returns centered Q32.32 target coordinates for an entity
// For use when species systems need the raw target position without navigation routing
// (e.g. swarm lock phase, quasar zap range check, homing settled snap)
func ResolveBaseTargetFixed(w *engine.World, entity core.Entity) (int64, int64, bool) {
	x, y, ok := resolveBaseTarget(w, entity)
	if !ok {
		return 0, 0, false
	}
	fx, fy := vmath.CenteredFromGrid(x, y)
	return fx, fy, true
}