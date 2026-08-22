package system

import (
	"slices"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// TargetGroup holds a combat target with hit members for area attacks
type TargetGroup struct {
	Members []core.Entity // Members within area, or entity itself for singles
	Target  core.Entity   // Header for composites, entity itself for singles
}

// TargetAssignment holds a resolved target with closest member for directed attacks
type TargetAssignment struct {
	Target core.Entity // Header for composites, entity itself for singles
	Hit    core.Entity // Closest member, or entity itself for singles
	DistSq float64     // Squared distance from query origin to Hit
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
	if headerComp, ok := w.Components.Header.GetPtr(entity); ok {
		switch headerComp.Type {
		case component.CompositeTypeUnit:
			return entity, entity, true
		case component.CompositeTypeAblative, component.CompositeTypeContainer:
			return 0, 0, false
		}
	}

	// Member entity — resolve upward to header
	if memberComp, ok := w.Components.Member.GetPtr(entity); ok {
		headerEntity := memberComp.HeaderEntity
		headerComp, ok := w.Components.Header.GetPtr(headerEntity)
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

// HasCombatTargetAt reports whether a non-player combat target occupies a cell.
// It excludes self, every cursor, cursor-owned orbs, and entities owned by ownerEntity.
func HasCombatTargetAt(w *engine.World, x, y int, selfEntity, ownerEntity core.Entity) bool {
	var entities [parameter.MaxEntitiesPerCell]core.Entity
	count := w.Positions.GetAllEntitiesAtInto(x, y, entities[:])
	for i := range count {
		e := entities[i]
		target, _, valid := ResolveTargetFromEntity(w, e, selfEntity)
		if !valid {
			continue
		}
		if isCursorOrOwnedOrb(w, target) {
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
// Iterates Combat store (singles) and Member store (composites) for species-agnostic resolution.
// Result order is store order, never map order: callers emit one event per group and
// combat resolution consumes RNG per event.
func FindTargetsInEllipse(w *engine.World, cx, cy int, invRxSq, invRySq float64, ownerEntity core.Entity) []TargetGroup {
	index := make(map[core.Entity]int)
	result := make([]TargetGroup, 0, 8)

	// 1. Simple combat entities (no Header, no Member component)
	for _, e := range w.Components.Combat.Entities() {
		if w.Components.Header.HasEntity(e) || w.Components.Member.HasEntity(e) {
			continue
		}
		if isCursorOrOwnedOrb(w, e) {
			continue
		}
		if isOwnedBy(w, e, ownerEntity) {
			continue
		}
		pos, ok := w.Positions.GetPosition(e)
		if !ok || !vmath.EllipseContainsPointF(pos.X, pos.Y, cx, cy, invRxSq, invRySq) {
			continue
		}
		index[e] = len(result)
		result = append(result, TargetGroup{Target: e})
	}

	// 2. Composite members — covers Unit hitbox members and Ablative combat members.
	// Container children are filtered by header type check.
	for _, memberEntity := range w.Components.Member.Entities() {
		memberComp, ok := w.Components.Member.GetPtr(memberEntity)
		if !ok {
			continue
		}
		headerEntity := memberComp.HeaderEntity
		headerComp, ok := w.Components.Header.GetPtr(headerEntity)
		if !ok || headerComp.Type == component.CompositeTypeContainer {
			continue
		}
		if !w.Components.Combat.HasEntity(headerEntity) {
			continue
		}
		if isCursorOrOwnedOrb(w, headerEntity) {
			continue
		}
		if isOwnedBy(w, headerEntity, ownerEntity) {
			continue
		}
		pos, ok := w.Positions.GetPosition(memberEntity)
		if !ok || !vmath.EllipseContainsPointF(pos.X, pos.Y, cx, cy, invRxSq, invRySq) {
			continue
		}

		if i, exists := index[headerEntity]; exists {
			result[i].Members = append(result[i].Members, memberEntity)
			continue
		}
		index[headerEntity] = len(result)
		result = append(result, TargetGroup{
			Target:  headerEntity,
			Members: []core.Entity{memberEntity},
		})
	}

	return result
}

// FindNearestTargets returns up to count targets, composite-grouped with closest member per header
// Composites prioritized over distance-sorted singles.
// If count exceeds available targets, results cycle through available targets (overflow distribution)
// ownerEntity-owned entities excluded
//
// Composites are accumulated in Member store order, so the stable sort breaks
// distance ties identically every run.
func FindNearestTargets(w *engine.World, fromX, fromY float64, count int, ownerEntity core.Entity) []TargetAssignment {
	if count <= 0 {
		return nil
	}

	compositeIdx := make(map[core.Entity]int)
	var composites []TargetAssignment
	var singles []TargetAssignment

	// 1. Simple combat entities
	for _, e := range w.Components.Combat.Entities() {
		if w.Components.Header.HasEntity(e) || w.Components.Member.HasEntity(e) {
			continue
		}
		if isCursorOrOwnedOrb(w, e) {
			continue
		}
		if isOwnedBy(w, e, ownerEntity) {
			continue
		}
		pos, ok := w.Positions.GetPosition(e)
		if !ok {
			continue
		}
		px, py := vmath.Point{X: pos.X, Y: pos.Y}.CenterF()
		distSq := vmath.MagnitudeSqF(px-fromX, py-fromY)
		singles = append(singles, TargetAssignment{Target: e, Hit: e, DistSq: distSq})
	}

	// 2. Composite members — closest member per header
	for _, memberEntity := range w.Components.Member.Entities() {
		memberComp, ok := w.Components.Member.GetPtr(memberEntity)
		if !ok {
			continue
		}
		headerEntity := memberComp.HeaderEntity
		headerComp, ok := w.Components.Header.GetPtr(headerEntity)
		if !ok || headerComp.Type == component.CompositeTypeContainer {
			continue
		}
		if !w.Components.Combat.HasEntity(headerEntity) {
			continue
		}
		if isCursorOrOwnedOrb(w, headerEntity) {
			continue
		}
		if isOwnedBy(w, headerEntity, ownerEntity) {
			continue
		}
		pos, ok := w.Positions.GetPosition(memberEntity)
		if !ok {
			continue
		}
		px, py := vmath.Point{X: pos.X, Y: pos.Y}.CenterF()
		distSq := vmath.MagnitudeSqF(px-fromX, py-fromY)

		if i, exists := compositeIdx[headerEntity]; exists {
			if distSq < composites[i].DistSq {
				composites[i].Hit = memberEntity
				composites[i].DistSq = distSq
			}
			continue
		}
		compositeIdx[headerEntity] = len(composites)
		composites = append(composites, TargetAssignment{
			Target: headerEntity,
			Hit:    memberEntity,
			DistSq: distSq,
		})
	}

	byDist := func(a, b TargetAssignment) int {
		if a.DistSq < b.DistSq {
			return -1
		}
		if a.DistSq > b.DistSq {
			return 1
		}
		return 0
	}

	// Composites first (priority, distance-sorted), then singles by distance
	slices.SortStableFunc(composites, byDist)
	slices.SortStableFunc(singles, byDist)

	result := make([]TargetAssignment, 0, len(composites)+len(singles))
	result = append(result, composites...)
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
	combat, ok := w.Components.Combat.GetPtr(entity)
	if !ok {
		return false
	}
	return combat.OwnerEntity == ownerEntity
}

// isCursorOrOwnedOrb excludes every player and every weapon orb owned by one.
func isCursorOrOwnedOrb(w *engine.World, entity core.Entity) bool {
	if w.Components.Cursor.HasEntity(entity) {
		return true
	}
	orb, ok := w.Components.Orb.GetPtr(entity)
	return ok && w.Components.Cursor.HasEntity(orb.OwnerEntity)
}

// ResolveClosestMember finds the nearest living member of a composite header
func ResolveClosestMember(w *engine.World, headerEntity core.Entity, fromX, fromY float64) (core.Entity, float64, float64, bool) {
	headerComp, ok := w.Components.Header.GetPtr(headerEntity)
	if !ok {
		return 0, 0, 0, false
	}

	var best core.Entity
	var bestX, bestY float64
	bestDistSq := -1.0

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}
		pos, ok := w.Positions.GetPosition(member.Entity)
		if !ok {
			continue
		}
		mx, my := vmath.Point{X: pos.X, Y: pos.Y}.CenterF()
		d := vmath.MagnitudeSqF(mx-fromX, my-fromY)
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

// resolveBaseTarget returns the closest grid-coordinate target for an entity based on its group
// Falls back to cursor position for group 0 or uninitialized groups
func resolveBaseTarget(w *engine.World, entity core.Entity) (x, y int, valid bool) {
	groupID := uint8(0)
	if tc, ok := w.Components.Target.GetComponent(entity); ok {
		groupID = tc.GroupID
	}

	state := w.Resources.Target.GetGroup(groupID)
	if !state.Valid || state.Count == 0 {
		// Uninitialized groups fall back to the roster-backed cursor group.
		state = w.Resources.Target.GetGroup(0)
		if !state.Valid || state.Count == 0 {
			return 0, 0, false
		}
	}

	if state.Count == 1 {
		return state.Targets[0].PosX, state.Targets[0].PosY, true
	}

	// Pick Euclidean closest target to entity
	var ex, ey int
	if pos, ok := w.Positions.GetPosition(entity); ok {
		ex, ey = pos.X, pos.Y
	} else {
		return state.Targets[0].PosX, state.Targets[0].PosY, true
	}

	bestDistSq := -1
	bestX, bestY := state.Targets[0].PosX, state.Targets[0].PosY

	for i := range state.Count {
		t := state.Targets[i]
		dx := ex - t.PosX
		dy := ey - t.PosY
		distSq := dx*dx + dy*dy
		if bestDistSq == -1 || distSq < bestDistSq {
			bestDistSq = distSq
			bestX, bestY = t.PosX, t.PosY
		}
	}

	return bestX, bestY, true
}

// ResolveMovementTarget computes the effective homing target for a kinetic entity
// Encapsulates the target resolution + navigation routing pattern shared by all species
// Returns (targetX, targetY in cells, usingDirectPath bool)
func ResolveMovementTarget(w *engine.World, entity core.Entity, kineticComp *component.KineticComponent) (float64, float64, bool) {
	baseX, baseY, ok := resolveBaseTarget(w, entity)
	if !ok {
		return kineticComp.PreciseX, kineticComp.PreciseY, true
	}

	baseCenterX, baseCenterY := vmath.Point{X: baseX, Y: baseY}.CenterF()

	navComp, hasNav := w.Components.Navigation.GetComponent(entity)
	if !hasNav {
		return baseCenterX, baseCenterY, true
	}

	if navComp.HasDirectPath {
		return baseCenterX, baseCenterY, true
	}

	if navComp.FlowX != 0 || navComp.FlowY != 0 {
		tx := kineticComp.PreciseX + navComp.FlowX*navComp.FlowLookahead
		ty := kineticComp.PreciseY + navComp.FlowY*navComp.FlowLookahead
		return tx, ty, false
	}

	return baseCenterX, baseCenterY, true
}

// ResolveBaseTargetPrecise returns centered sub-cell target coordinates for an entity
// For use when species systems need the raw target position without navigation routing
// (e.g. swarm lock phase, quasar zap range check, homing settled snap)
func ResolveBaseTargetPrecise(w *engine.World, entity core.Entity) (float64, float64, bool) {
	x, y, ok := resolveBaseTarget(w, entity)
	if !ok {
		return 0, 0, false
	}
	px, py := vmath.Point{X: x, Y: y}.CenterF()
	return px, py, true
}
