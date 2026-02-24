package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/navigation"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// LUT for normalized Q32.32 flow vectors
var flowDirLUT [8][2]int64

func init() {
	aspectY := vmath.Scale / 2
	for i, vec := range navigation.DirVectors {
		fx := vmath.FromInt(vec[0])
		fy := vmath.Mul(vmath.FromInt(vec[1]), aspectY)
		if fx != 0 || fy != 0 {
			fx, fy = vmath.Normalize2D(fx, fy)
		}
		flowDirLUT[i] = [2]int64{fx, fy}
	}
}

// targetGroupNav holds per-group flow fields and entity buffers
type targetGroupNav struct {
	pointFlowCache     *navigation.FlowFieldCache // For point entities (1×1)
	compositeFlowCache *navigation.FlowFieldCache // For composite entities (footprint-aware)
}

var DebugFlow *navigation.FlowFieldCache
var DebugShowFlow bool

var DebugCompositeFlow *navigation.FlowFieldCache
var DebugCompositePassability *navigation.CompositePassability
var DebugShowCompositeNav bool // New flag for composite debug view

// NavigationSystem calculates flow fields for kinetic entities
type NavigationSystem struct {
	world *engine.World

	// Per-group flow field management
	groups map[uint8]*targetGroupNav

	// Composite passability grid (shared, recomputed on wall changes)
	compositePassability *navigation.CompositePassability

	// Cached cursor position
	cursorX, cursorY int
	cursorValid      bool

	statEntities   *atomic.Int64
	statRecomputes *atomic.Int64
	statROICells   *atomic.Int64

	enabled bool
}

func NewNavigationSystem(world *engine.World) engine.System {
	s := &NavigationSystem{
		world:  world,
		groups: make(map[uint8]*targetGroupNav),
	}

	s.statEntities = world.Resources.Status.Ints.Get("nav.entities")
	s.statRecomputes = world.Resources.Status.Ints.Get("nav.recomputes")
	s.statROICells = world.Resources.Status.Ints.Get("nav.roi_cells")

	s.Init()
	return s
}

func (s *NavigationSystem) Init() {
	s.enabled = true
	s.groups = make(map[uint8]*targetGroupNav)

	s.getOrCreateGroup(0)

	config := s.world.Resources.Config
	if config.MapWidth > 0 && config.MapHeight > 0 {
		// Initialize composite passability
		s.compositePassability = navigation.NewCompositePassability(
			config.MapWidth, config.MapHeight,
			parameter.EyeWidth, parameter.EyeHeight,
			parameter.EyeHeaderOffsetX, parameter.EyeHeaderOffsetY,
		)
		s.recomputeCompositePassability()

		for _, g := range s.groups {
			g.pointFlowCache.Resize(config.MapWidth, config.MapHeight)
			g.compositeFlowCache.Resize(config.MapWidth, config.MapHeight)
		}
	}

	if s.world.Resources.Player != nil {
		if pos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity); ok {
			s.cursorX = pos.X
			s.cursorY = pos.Y
			s.cursorValid = true
		}
	}

	if s.world.Resources.Target == nil {
		s.world.Resources.Target = &engine.TargetResource{}
	}

	// Debug exposure
	if g, ok := s.groups[0]; ok {
		DebugFlow = g.pointFlowCache
		DebugCompositeFlow = g.compositeFlowCache
	}
	DebugCompositePassability = s.compositePassability
}

func (s *NavigationSystem) Name() string {
	return "navigation"
}

func (s *NavigationSystem) Priority() int {
	return parameter.PriorityNavigation
}

func (s *NavigationSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameReset,
		event.EventMetaSystemCommandRequest,
		event.EventCursorMoved,
		event.EventLevelSetup,
		event.EventTargetGroupUpdate,
		event.EventTargetGroupRemove,
		event.EventNavigationRegraph,
		event.EventWallSpawned,
		event.EventWallDespawned,
	}
}

func (s *NavigationSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
		return
	}

	switch ev.Type {
	case event.EventCursorMoved:
		if payload, ok := ev.Payload.(*event.CursorMovedPayload); ok {
			s.cursorX = payload.X
			s.cursorY = payload.Y
			s.cursorValid = true
		}

	case event.EventLevelSetup:
		if payload, ok := ev.Payload.(*event.LevelSetupPayload); ok {
			s.compositePassability.Resize(payload.Width, payload.Height)
			s.recomputeCompositePassability()
			for _, g := range s.groups {
				g.pointFlowCache.Resize(payload.Width, payload.Height)
				g.compositeFlowCache.Resize(payload.Width, payload.Height)
			}
		}

	case event.EventTargetGroupUpdate:
		if payload, ok := ev.Payload.(*event.TargetGroupUpdatePayload); ok {
			s.handleGroupUpdate(payload)
		}

	case event.EventTargetGroupRemove:
		if payload, ok := ev.Payload.(*event.TargetGroupRemovePayload); ok {
			delete(s.groups, payload.GroupID)
			s.world.Resources.Target.SetGroup(payload.GroupID, engine.TargetGroupState{})
		}

	case event.EventWallSpawned:
		if payload, ok := ev.Payload.(*event.WallSpawnedPayload); ok {
			s.recomputeCompositePassabilityROI(payload.X, payload.Y, payload.Width, payload.Height)
		}
		for _, g := range s.groups {
			g.compositeFlowCache.MarkDirty()
		}

	case event.EventWallDespawned:
		if payload, ok := ev.Payload.(*event.WallDespawnedPayload); ok {
			s.recomputeCompositePassabilityROI(payload.X, payload.Y, payload.Width, payload.Height)
		}
		for _, g := range s.groups {
			g.compositeFlowCache.MarkDirty()
		}

	case event.EventNavigationRegraph:
		s.recomputeCompositePassability()
		for _, g := range s.groups {
			g.compositeFlowCache.MarkDirty()
		}
	}
}

// recomputeCompositePassabilityROI recomputes passability for header positions
// affected by wall changes within the given bounds
// Expansion accounts for footprint: any header whose footprint overlaps the wall region
func (s *NavigationSystem) recomputeCompositePassabilityROI(wallX, wallY, wallW, wallH int) {
	if s.compositePassability == nil {
		return
	}

	footW, footH, offX, offY := s.compositePassability.GetFootprint()

	// Minkowski expansion: wall bounds → affected header positions
	minX := wallX - footW + 1 + offX
	minY := wallY - footH + 1 + offY
	maxX := wallX + wallW - 1 + offX
	maxY := wallY + wallH - 1 + offY

	isWall := func(x, y int) bool {
		return s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic)
	}
	s.compositePassability.ComputeROI(isWall, minX, minY, maxX, maxY)
}

func (s *NavigationSystem) recomputeCompositePassability() {
	if s.compositePassability == nil {
		return
	}
	isWall := func(x, y int) bool {
		return s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic)
	}
	s.compositePassability.Compute(isWall)
}

func (s *NavigationSystem) Update() {
	if !s.enabled {
		return
	}

	config := s.world.Resources.Config

	// Handle map resize
	for _, g := range s.groups {
		if config.MapWidth != g.pointFlowCache.Field.Width || config.MapHeight != g.pointFlowCache.Field.Height {
			g.pointFlowCache.Resize(config.MapWidth, config.MapHeight)
			g.compositeFlowCache.Resize(config.MapWidth, config.MapHeight)
			s.compositePassability.Resize(config.MapWidth, config.MapHeight)
			s.recomputeCompositePassability()
		}
	}

	s.resolveGroupTargets()

	// Wall checker for point entities
	isBlockedPoint := func(x, y int) bool {
		return s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic)
	}

	// Wall checker for composites (uses pre-computed passability)
	isBlockedComposite := s.compositePassability.IsBlocked

	// Phase 1: Classify entities, perform LOS checks
	entities := s.world.Components.Navigation.GetAllEntities()
	s.statEntities.Store(int64(len(entities)))

	for _, entity := range entities {
		navComp, ok := s.world.Components.Navigation.GetComponent(entity)
		if !ok {
			continue
		}

		groupID := s.getEntityGroup(entity)
		groupExists := false
		if _, groupExists = s.groups[groupID]; !groupExists {
			groupID = 0
		}

		groupState := s.world.Resources.Target.GetGroup(groupID)
		if !groupState.Valid {
			continue
		}

		var gridX, gridY int
		if kinetic, ok := s.world.Components.Kinetic.GetComponent(entity); ok {
			gridX, gridY = vmath.GridFromCentered(kinetic.PreciseX, kinetic.PreciseY)
		} else if pos, ok := s.world.Positions.GetPosition(entity); ok {
			gridX, gridY = pos.X, pos.Y
		} else {
			continue
		}

		isComposite := navComp.Width > 1 || navComp.Height > 1
		width, height := navComp.Width, navComp.Height
		if width == 0 {
			width = 1
		}
		if height == 0 {
			height = 1
		}

		hasLOS := false
		if !isComposite {
			hasLOS = s.world.Positions.HasLineOfSight(gridX, gridY, groupState.PosX, groupState.PosY, component.WallBlockKinetic)
		} else {
			hasLOS = s.world.Positions.HasAreaLineOfSightRotatable(gridX, gridY, groupState.PosX, groupState.PosY, width, height, component.WallBlockKinetic)
		}

		if hasLOS {
			navComp.HasDirectPath = true
			navComp.FlowX = 0
			navComp.FlowY = 0
		} else {
			navComp.HasDirectPath = false
		}
		s.world.Components.Navigation.SetComponent(entity, navComp)
	}

	// Phase 2: Update flow fields
	totalRecomputes := int64(0)
	for groupID, g := range s.groups {
		groupState := s.world.Resources.Target.GetGroup(groupID)
		if !groupState.Valid {
			continue
		}

		if recomputed := g.pointFlowCache.Update(groupState.PosX, groupState.PosY, isBlockedPoint); recomputed {
			totalRecomputes++
		}

		if recomputed := g.compositeFlowCache.Update(groupState.PosX, groupState.PosY, isBlockedComposite); recomputed {
			totalRecomputes++
		}
	}
	s.statRecomputes.Store(totalRecomputes)

	// Phase 3: Update flow directions
	for _, entity := range entities {
		navComp, ok := s.world.Components.Navigation.GetComponent(entity)
		if !ok || navComp.HasDirectPath {
			continue
		}

		groupID := s.getEntityGroup(entity)
		group, ok := s.groups[groupID]
		if !ok {
			group = s.groups[0]
		}

		var preciseX, preciseY int64
		if kinetic, ok := s.world.Components.Kinetic.GetComponent(entity); ok {
			preciseX, preciseY = kinetic.PreciseX, kinetic.PreciseY
		} else if pos, ok := s.world.Positions.GetPosition(entity); ok {
			preciseX, preciseY = vmath.CenteredFromGrid(pos.X, pos.Y)
		} else {
			continue
		}

		isComposite := navComp.Width > 1 || navComp.Height > 1

		if isComposite {
			// Band routing: only active when BudgetMultiplier explicitly set above 1.0 (Scale)
			// Non-GA species retain zero-value (0), always taking optimal flow path
			if navComp.BudgetMultiplier > vmath.Scale {
				navComp.FlowX, navComp.FlowY = s.getBandRoutedDirection(preciseX, preciseY, navComp, group.compositeFlowCache)
			} else {
				navComp.FlowX, navComp.FlowY = s.getCompositeFlowDirection(preciseX, preciseY, group.compositeFlowCache)
			}
		} else {
			navComp.FlowX, navComp.FlowY = s.getInterpolatedFlowDirection(preciseX, preciseY, group.pointFlowCache)
		}

		s.world.Components.Navigation.SetComponent(entity, navComp)
	}

	if g, ok := s.groups[0]; ok {
		DebugFlow = g.pointFlowCache
	}
}

func (s *NavigationSystem) handleGroupUpdate(payload *event.TargetGroupUpdatePayload) {
	g := s.getOrCreateGroup(payload.GroupID)
	g.pointFlowCache.MarkDirty()
	g.compositeFlowCache.MarkDirty()

	s.world.Resources.Target.SetGroup(payload.GroupID, engine.TargetGroupState{
		Type:   payload.Type,
		Entity: payload.Entity,
		PosX:   payload.PosX,
		PosY:   payload.PosY,
		Valid:  true,
	})
}

func (s *NavigationSystem) getOrCreateGroup(groupID uint8) *targetGroupNav {
	if g, ok := s.groups[groupID]; ok {
		return g
	}
	config := s.world.Resources.Config
	g := &targetGroupNav{
		pointFlowCache: navigation.NewFlowFieldCache(
			config.MapWidth, config.MapHeight,
			parameter.NavFlowMinTicksBetweenCompute,
			parameter.NavFlowDirtyDistance,
		),
		compositeFlowCache: navigation.NewFlowFieldCache(
			config.MapWidth, config.MapHeight,
			parameter.NavFlowMinTicksBetweenCompute,
			parameter.NavFlowDirtyDistance,
		),
	}
	s.groups[groupID] = g
	return g
}

func (s *NavigationSystem) getEntityGroup(entity core.Entity) uint8 {
	if tc, ok := s.world.Components.Target.GetComponent(entity); ok {
		return tc.GroupID
	}
	return 0
}

func (s *NavigationSystem) resolveGroupTargets() {
	tr := s.world.Resources.Target

	// Cursor always group 0
	if s.cursorValid {
		tr.Groups[0] = engine.TargetGroupState{
			Type:  component.TargetCursor,
			PosX:  s.cursorX,
			PosY:  s.cursorY,
			Valid: true,
		}
	}

	// Scan TargetAnchor components — entity-based group registration
	// Anchors override any previous entity-type assignment for their group
	anchorEntities := s.world.Components.TargetAnchor.GetAllEntities()
	anchoredGroups := make(map[uint8]bool, len(anchorEntities))

	for _, entity := range anchorEntities {
		anchor, ok := s.world.Components.TargetAnchor.GetComponent(entity)
		if !ok || anchor.GroupID == 0 {
			continue
		}

		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			// Entity destroyed or position removed — invalidate
			tr.Groups[anchor.GroupID] = engine.TargetGroupState{Valid: false}
			anchoredGroups[anchor.GroupID] = true
			continue
		}

		// Ensure flow caches exist for anchored groups so fallback logic doesn't route to cursor
		s.getOrCreateGroup(anchor.GroupID)

		tr.Groups[anchor.GroupID] = engine.TargetGroupState{
			Type:   component.TargetEntity,
			Entity: entity,
			PosX:   pos.X,
			PosY:   pos.Y,
			Valid:  true,
		}
		anchoredGroups[anchor.GroupID] = true
	}

	// Resolve non-anchored groups (event-assigned via EventTargetGroupUpdate)
	for groupID := uint8(1); groupID < component.MaxTargetGroups; groupID++ {
		if anchoredGroups[groupID] {
			continue // Anchor takes precedence
		}

		state := tr.Groups[groupID]
		if !state.Valid {
			continue
		}

		switch state.Type {
		case component.TargetEntity:
			if pos, ok := s.world.Positions.GetPosition(state.Entity); ok {
				state.PosX = pos.X
				state.PosY = pos.Y
			} else {
				state.Valid = false
			}
			tr.Groups[groupID] = state

		case component.TargetCursor:
			state.PosX = s.cursorX
			state.PosY = s.cursorY
			tr.Groups[groupID] = state
		}
	}
}

// getCompositeFlowDirection returns flow direction from composite-aware flow field
// Handles case where entity's current cell is blocked in passability
func (s *NavigationSystem) getCompositeFlowDirection(preciseX, preciseY int64, cache *navigation.FlowFieldCache) (int64, int64) {
	x0 := vmath.ToInt(preciseX)
	y0 := vmath.ToInt(preciseY)

	// Check if primary cell is blocked/unvisited — escape to best neighbor
	dir := cache.GetDirection(x0, y0)
	if dir < 0 || dir >= navigation.DirCount {
		escDir := s.findBestNeighborDirection(x0, y0, cache)
		if escDir < 0 || escDir >= navigation.DirCount {
			return 0, 0
		}
		return flowDirLUT[escDir][0], flowDirLUT[escDir][1]
	}

	// Bilinear interpolation (same as point entities)
	u := preciseX & vmath.Mask
	v := preciseY & vmath.Mask
	invU := vmath.Scale - u
	invV := vmath.Scale - v

	w00 := vmath.Mul(invU, invV)
	w10 := vmath.Mul(u, invV)
	w01 := vmath.Mul(invU, v)
	w11 := vmath.Mul(u, v)

	v00x, v00y, valid00 := s.getFlowVectorAndValidity(x0, y0, cache)
	v10x, v10y, valid10 := s.getFlowVectorAndValidity(x0+1, y0, cache)
	v01x, v01y, valid01 := s.getFlowVectorAndValidity(x0, y0+1, cache)
	v11x, v11y, valid11 := s.getFlowVectorAndValidity(x0+1, y0+1, cache)

	var sumX, sumY, totalWeight int64

	if valid00 {
		sumX += vmath.Mul(v00x, w00)
		sumY += vmath.Mul(v00y, w00)
		totalWeight += w00
	}
	if valid10 {
		sumX += vmath.Mul(v10x, w10)
		sumY += vmath.Mul(v10y, w10)
		totalWeight += w10
	}
	if valid01 {
		sumX += vmath.Mul(v01x, w01)
		sumY += vmath.Mul(v01y, w01)
		totalWeight += w01
	}
	if valid11 {
		sumX += vmath.Mul(v11x, w11)
		sumY += vmath.Mul(v11y, w11)
		totalWeight += w11
	}

	if totalWeight == 0 {
		return 0, 0
	}

	resX := vmath.Div(sumX, totalWeight)
	resY := vmath.Div(sumY, totalWeight)

	if resX != 0 || resY != 0 {
		return vmath.Normalize2D(resX, resY)
	}
	return 0, 0
}

// findBestNeighborDirection finds direction toward lowest-distance passable neighbor, used when entity is at a blocked cell
func (s *NavigationSystem) findBestNeighborDirection(x, y int, cache *navigation.FlowFieldCache) int8 {
	bestDir := int8(-1)
	bestDist := 1 << 30

	for d := int8(0); d < navigation.DirCount; d++ {
		nx := x + navigation.DirVectors[d][0]
		ny := y + navigation.DirVectors[d][1]
		dist := cache.GetDistance(nx, ny)
		if dist >= 0 && dist < bestDist {
			bestDist = dist
			bestDir = d
		}
	}
	return bestDir
}

// getInterpolatedFlowDirection performs bilinear interpolation for point entities
func (s *NavigationSystem) getInterpolatedFlowDirection(preciseX, preciseY int64, cache *navigation.FlowFieldCache) (int64, int64) {
	sampleX := preciseX - vmath.CellCenter
	sampleY := preciseY - vmath.CellCenter

	x0 := vmath.ToInt(sampleX)
	y0 := vmath.ToInt(sampleY)

	u := sampleX & vmath.Mask
	v := sampleY & vmath.Mask

	invU := vmath.Scale - u
	invV := vmath.Scale - v

	w00 := vmath.Mul(invU, invV)
	w10 := vmath.Mul(u, invV)
	w01 := vmath.Mul(invU, v)
	w11 := vmath.Mul(u, v)

	v00x, v00y, valid00 := s.getFlowVectorAndValidity(x0, y0, cache)
	v10x, v10y, valid10 := s.getFlowVectorAndValidity(x0+1, y0, cache)
	v01x, v01y, valid01 := s.getFlowVectorAndValidity(x0, y0+1, cache)
	v11x, v11y, valid11 := s.getFlowVectorAndValidity(x0+1, y0+1, cache)

	var sumX, sumY, totalWeight int64

	if valid00 {
		sumX += vmath.Mul(v00x, w00)
		sumY += vmath.Mul(v00y, w00)
		totalWeight += w00
	}
	if valid10 {
		sumX += vmath.Mul(v10x, w10)
		sumY += vmath.Mul(v10y, w10)
		totalWeight += w10
	}
	if valid01 {
		sumX += vmath.Mul(v01x, w01)
		sumY += vmath.Mul(v01y, w01)
		totalWeight += w01
	}
	if valid11 {
		sumX += vmath.Mul(v11x, w11)
		sumY += vmath.Mul(v11y, w11)
		totalWeight += w11
	}

	if totalWeight == 0 {
		return 0, 0
	}

	resX := vmath.Div(sumX, totalWeight)
	resY := vmath.Div(sumY, totalWeight)

	if resX != 0 || resY != 0 {
		return vmath.Normalize2D(resX, resY)
	}
	return 0, 0
}

func (s *NavigationSystem) getFlowVectorAndValidity(x, y int, cache *navigation.FlowFieldCache) (int64, int64, bool) {
	dir := cache.GetDirection(x, y)
	if dir < 0 || dir >= navigation.DirCount {
		return 0, 0, false
	}
	return flowDirLUT[dir][0], flowDirLUT[dir][1], true
}

// getBandRoutedDirection selects direction within a distance budget around the optimal path
// Entities accept any neighbor within BudgetMultiplier × current distance, scored by ExplorationBias
// Falls back to optimal interpolated flow when near target (FlowLookahead) or no valid band neighbors
// O(16) per call: two passes over 8 neighbors, zero allocations
func (s *NavigationSystem) getBandRoutedDirection(
	preciseX, preciseY int64,
	navComp component.NavigationComponent,
	cache *navigation.FlowFieldCache,
) (int64, int64) {
	gridX := vmath.ToInt(preciseX)
	gridY := vmath.ToInt(preciseY)

	currentDist := cache.GetDistance(gridX, gridY)

	// Unreachable or blocked: escape to best passable neighbor
	if currentDist < 0 {
		escDir := s.findBestNeighborDirection(gridX, gridY, cache)
		if escDir < 0 || escDir >= navigation.DirCount {
			return 0, 0
		}
		return flowDirLUT[escDir][0], flowDirLUT[escDir][1]
	}

	// Within convergence zone: disable band routing, use optimal interpolated flow
	if currentDist <= vmath.ToInt(navComp.FlowLookahead) {
		return s.getCompositeFlowDirection(preciseX, preciseY, cache)
	}

	// Distance budget: currentDist × BudgetMultiplier (Q32.32 → integer)
	maxDist := int((int64(currentDist) * navComp.BudgetMultiplier) >> 32)

	// Pass 1: find optimal (minimum) reachable neighbor distance for scoring reference
	const distSentinel = 1 << 30
	optimalNeighborDist := distSentinel
	for d := int8(0); d < navigation.DirCount; d++ {
		nx := gridX + navigation.DirVectors[d][0]
		ny := gridY + navigation.DirVectors[d][1]
		nd := cache.GetDistance(nx, ny)
		if nd >= 0 && nd < optimalNeighborDist {
			optimalNeighborDist = nd
		}
	}

	// No reachable neighbors at all
	if optimalNeighborDist >= distSentinel {
		return s.getCompositeFlowDirection(preciseX, preciseY, cache)
	}

	// Pass 2: score valid neighbors within budget
	// score = (Scale - ExplorationBias) × progressScore + ExplorationBias × exploreScore
	// progressScore: how much closer than current (positive = toward target)
	// exploreScore: how much farther than optimal neighbor (positive = more divergent)
	invBias := vmath.Scale - navComp.ExplorationBias
	bias := navComp.ExplorationBias

	bestDir := int8(-1)
	var bestScore int64 = -(1 << 62)

	for d := int8(0); d < navigation.DirCount; d++ {
		nx := gridX + navigation.DirVectors[d][0]
		ny := gridY + navigation.DirVectors[d][1]
		nd := cache.GetDistance(nx, ny)
		if nd < 0 {
			continue // blocked or unreachable
		}
		if nd > maxDist {
			continue // exceeds distance budget
		}

		progressScore := int64(currentDist - nd)
		exploreScore := int64(nd - optimalNeighborDist)

		score := progressScore*invBias + exploreScore*bias
		if score > bestScore {
			bestScore = score
			bestDir = d
		}
	}

	// No valid band neighbors: fall back to optimal flow
	if bestDir < 0 {
		return s.getCompositeFlowDirection(preciseX, preciseY, cache)
	}

	return flowDirLUT[bestDir][0], flowDirLUT[bestDir][1]
}