package system

import (
	"slices"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
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

// ResolveTargetFromEntity returns the combat target and struck member, excluding self.
// Container and ablative headers have no direct hit surface; ownership is not filtered.
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

// targetCell returns an entity's cell, rejecting one outside the map. The finders
// below walk component stores rather than the spatial grid, which holds no off-map
// cell; without this an entity a crop left outside the bounds stays targetable
// while nothing renders it.
func targetCell(w *engine.World, e core.Entity) (int, int, bool) {
	pos, ok := w.Positions.GetPosition(e)
	if !ok {
		return 0, 0, false
	}
	config := w.Resources.Config
	if pos.X < 0 || pos.X >= config.MapWidth || pos.Y < 0 || pos.Y >= config.MapHeight {
		return 0, 0, false
	}
	return pos.X, pos.Y, true
}

// HasCombatTargetAt reports whether a non-player combat target occupies a cell.
// scope selects the enumerated domains; shared species pass ScopeShared, weapons ScopeBoth.
// It excludes self, every cursor, cursor-owned orbs, and entities owned by ownerEntity.
func HasCombatTargetAt(w *engine.World, x, y int, scope engine.DomainScope, selfEntity, ownerEntity core.Entity) bool {
	_, _, ok := CombatTargetAt(w, x, y, scope, selfEntity, ownerEntity)
	return ok
}

// CombatTargetAt is HasCombatTargetAt naming the first target found and the occupant it was hit through
func CombatTargetAt(w *engine.World, x, y int, scope engine.DomainScope, selfEntity, ownerEntity core.Entity) (target, hit core.Entity, ok bool) {
	var entities [parameter.MaxEntitiesPerCell]core.Entity
	count := w.Positions.GetEntitiesAtInto(x, y, scope, entities[:])
	for i := range count {
		target, hit, valid := ResolveTargetFromEntity(w, entities[i], selfEntity)
		if !valid || isCursorOrOwnedOrb(w, target) || isOwnedBy(w, target, ownerEntity) {
			continue
		}
		return target, hit, true
	}
	return 0, 0, false
}

// FindTargetsInEllipse returns all combat targets with members inside the ellipse; see FindTargetsIn
func FindTargetsInEllipse(w *engine.World, cx, cy int, invRxSq, invRySq float64, scope engine.DomainScope, ownerEntity core.Entity) []TargetGroup {
	return FindTargetsIn(w, func(x, y int) bool {
		return vmath.EllipseContainsPointF(x, y, cx, cy, invRxSq, invRySq)
	}, scope, ownerEntity)
}

// FindTargetsIn returns all combat targets with members in the cells contains accepts,
// grouped: one TargetGroup per composite header or single entity. ownerEntity-owned
// entities are excluded. Order is store order, never map order: callers emit one
// event per group and combat resolution consumes RNG per event.
func FindTargetsIn(w *engine.World, contains func(x, y int) bool, scope engine.DomainScope, ownerEntity core.Entity) []TargetGroup {
	index := make(map[core.Entity]int)
	result := make([]TargetGroup, 0, 8)

	// 1. Simple combat entities (no Header, no Member component)
	for _, e := range w.Components.Combat.Entities() {
		if !scope.Selects(e) {
			continue
		}
		if w.Components.Header.HasEntity(e) || w.Components.Member.HasEntity(e) {
			continue
		}
		if isCursorOrOwnedOrb(w, e) {
			continue
		}
		if isOwnedBy(w, e, ownerEntity) {
			continue
		}
		x, y, ok := targetCell(w, e)
		if !ok || !contains(x, y) {
			continue
		}
		index[e] = len(result)
		result = append(result, TargetGroup{Target: e})
	}

	// 2. Composite members — covers Unit hitbox members and Ablative combat members.
	// Members share their header's domain by construction, so filtering here covers both.
	headers := headerFilter{owner: ownerEntity}
	w.Components.Member.Each(func(memberEntity core.Entity, memberComp *component.MemberComponent) bool {
		headerEntity := memberComp.HeaderEntity
		if !scope.Selects(memberEntity) || !headers.targets(w, headerEntity) {
			return true
		}
		x, y, ok := targetCell(w, memberEntity)
		if !ok || !contains(x, y) {
			return true
		}

		if i, exists := index[headerEntity]; exists {
			result[i].Members = append(result[i].Members, memberEntity)
			return true
		}
		index[headerEntity] = len(result)
		result = append(result, TargetGroup{
			Target:  headerEntity,
			Members: []core.Entity{memberEntity},
		})
		return true
	})

	return result
}

// headerFilter answers whether a composite's members are hits for an owner's weapons.
// The answer is per header, and a header's members mostly sit together in the member
// store, so it is held across a run of them rather than looked up per member.
type headerFilter struct {
	owner, last core.Entity
	valid       bool
}

func (f *headerFilter) targets(w *engine.World, header core.Entity) bool {
	if header != f.last {
		f.last = header
		headerComp, ok := w.Components.Header.GetPtr(header)
		f.valid = ok && headerComp.Type != component.CompositeTypeContainer &&
			w.Components.Combat.HasEntity(header) &&
			!isCursorOrOwnedOrb(w, header) && !isOwnedBy(w, header, f.owner)
	}
	return f.valid
}

// Nearest targets prioritize composites, then singles, excluding owner-owned entities.
// Store order breaks distance ties; overflow cycles through the available targets.
func FindNearestTargets(w *engine.World, fromX, fromY float64, count int, scope engine.DomainScope, ownerEntity core.Entity) []TargetAssignment {
	if count <= 0 {
		return nil
	}

	compositeIdx := make(map[core.Entity]int)
	var composites []TargetAssignment
	var singles []TargetAssignment

	// 1. Simple combat entities
	for _, e := range w.Components.Combat.Entities() {
		if !scope.Selects(e) {
			continue
		}
		if w.Components.Header.HasEntity(e) || w.Components.Member.HasEntity(e) {
			continue
		}
		if isCursorOrOwnedOrb(w, e) {
			continue
		}
		if isOwnedBy(w, e, ownerEntity) {
			continue
		}
		x, y, ok := targetCell(w, e)
		if !ok {
			continue
		}
		px, py := vmath.Point{X: x, Y: y}.CenterF()
		distSq := vmath.MagnitudeSqF(px-fromX, py-fromY)
		singles = append(singles, TargetAssignment{Target: e, Hit: e, DistSq: distSq})
	}

	// 2. Composite members — closest member per header
	headers := headerFilter{owner: ownerEntity}
	w.Components.Member.Each(func(memberEntity core.Entity, memberComp *component.MemberComponent) bool {
		headerEntity := memberComp.HeaderEntity
		if !scope.Selects(memberEntity) || !headers.targets(w, headerEntity) {
			return true
		}
		x, y, ok := targetCell(w, memberEntity)
		if !ok {
			return true
		}
		px, py := vmath.Point{X: x, Y: y}.CenterF()
		distSq := vmath.MagnitudeSqF(px-fromX, py-fromY)

		if i, exists := compositeIdx[headerEntity]; exists {
			if distSq < composites[i].DistSq {
				composites[i].Hit = memberEntity
				composites[i].DistSq = distSq
			}
			return true
		}
		compositeIdx[headerEntity] = len(composites)
		composites = append(composites, TargetAssignment{
			Target: headerEntity,
			Hit:    memberEntity,
			DistSq: distSq,
		})
		return true
	})

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

// traceRay is how many steps a ray runs before the first wall that blocks kinetics
// or the map edge
func traceRay(w *engine.World, r vmath.Ray) int {
	if r.DX == 0 && r.DY == 0 {
		return 0
	}
	limit := w.Resources.Config.MapWidth + w.Resources.Config.MapHeight
	for i := 1; i <= limit; i++ {
		x, y := r.Center(i)
		if w.Positions.IsOutOfBounds(x, y) || w.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic) {
			return i - 1
		}
	}
	return limit
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
	if nav, ok := w.Components.Navigation.GetPtr(entity); ok && nav.LockedTarget != 0 {
		return targetCell(w, nav.LockedTarget)
	}
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

	if state.Type == component.TargetCursor && w.Components.SnakeHead.HasEntity(entity) {
		return 0, 0, false // Navigation has not found a reachable cursor.
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
