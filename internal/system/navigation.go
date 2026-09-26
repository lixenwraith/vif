package system

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/navigation"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// targetGroupNav holds per-group flow fields and the grid generation each was last
// computed on; zero is none, since the generation starts at one
type targetGroupNav struct {
	pointFlowCache       *navigation.FlowFieldCache // For point entities (1×1)
	compositeFlowCache   *navigation.FlowFieldCache // For composite entities (footprint-aware)
	pointAt, compositeAt uint64
}

// NavigationSystem resolves target groups, maintains per-group point and composite
// flow fields, tracks composite passability, and computes gateway route graphs
type NavigationSystem struct {
	world *engine.World

	// Per-group flow field management
	groups map[uint8]*targetGroupNav

	// Composite passability grid (shared, recomputed on wall changes)
	compositePassability *navigation.CompositePassability
	walls                []bool // the WallTest grid a derivation reads

	// grid is the generation of the wall grid and the passability derived from it:
	// it moves whenever either changes, so a field or route graph stamped with it
	// when computed is exactly what recomputing it now would give. seenWalls is the
	// wall grid it last moved for.
	grid      uint64
	seenWalls []bool

	// Per-tick resolved target snapshot; avoids per-entity TargetResource locking
	targets [component.MaxTargetGroups]engine.TargetGroupState

	// Ticks since last gateway route graph recompute (rebuild budget)
	routeRebuildTicks int

	statEntities   *atomic.Int64
	statRecomputes *atomic.Int64
	statROICells   *atomic.Int64
	buffers        bufferTelemetry

	enabled bool
}

func NewNavigationSystem(world *engine.World) engine.System {
	s := &NavigationSystem{
		world:  world,
		groups: make(map[uint8]*targetGroupNav),
		grid:   1,
	}

	s.statEntities = world.Resources.Status.Ints.Get("nav.entities")
	s.statRecomputes = world.Resources.Status.Ints.Get("nav.recomputes")
	s.statROICells = world.Resources.Status.Ints.Get("nav.roi_cells")
	s.buffers = newBufferTelemetry(world.Resources.Status, "nav", "groups")

	s.Init()
	return s
}

func (s *NavigationSystem) Init() {
	s.statEntities.Store(0)
	s.statRecomputes.Store(0)
	s.statROICells.Store(0)
	s.buffers.Reset()
	s.enabled = true
	s.groups = make(map[uint8]*targetGroupNav)
	s.targets = [component.MaxTargetGroups]engine.TargetGroupState{}
	s.routeRebuildTicks = 0

	s.getOrCreateGroup(0)

	config := s.world.Resources.Config
	if config.MapWidth > 0 && config.MapHeight > 0 {
		s.resizePassability(config.MapWidth, config.MapHeight)
		for _, g := range s.groups {
			g.pointFlowCache.Resize(config.MapWidth, config.MapHeight)
			g.compositeFlowCache.Resize(config.MapWidth, config.MapHeight)
		}
	}

	if s.world.Resources.Target == nil {
		s.world.Resources.Target = &engine.TargetResource{}
	}

	if s.world.Resources.RouteGraph == nil {
		s.world.Resources.RouteGraph = &engine.RouteGraphResource{}
	}

	if dbg := s.world.Resources.NavigationDebug; dbg != nil {
		if g, ok := s.groups[dbg.GroupID]; ok {
			dbg.Flow = g.pointFlowCache
			dbg.CompositeFlow = g.compositeFlowCache
		}
		dbg.CompositePassability = s.compositePassability
	}
}

func (s *NavigationSystem) Name() string {
	return "navigation"
}

func (s *NavigationSystem) Priority() int {
	return parameter.PriorityNavigation
}

func (s *NavigationSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameResetRequest,
		event.EventMetaSystemCommandRequest,
		event.EventCursorMoved,
		event.EventCursorDespawned,
		event.EventLevelSetup,
		event.EventTargetGroupUpdate,
		event.EventTargetGroupRemove,
		event.EventNavigationRegraph,
		event.EventRouteGraphRequest,
		event.EventWallSpawned,
		event.EventWallDespawned,
	}
}

func (s *NavigationSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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
	case event.EventCursorMoved, event.EventCursorDespawned:
		g := s.getOrCreateGroup(0)
		g.pointFlowCache.MarkDirty()
		g.compositeFlowCache.MarkDirty()

	case event.EventLevelSetup:
		if payload, ok := ev.Payload.(*event.LevelSetupPayload); ok {
			s.resizePassability(payload.Width, payload.Height)
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

	case event.EventRouteGraphRequest:
		if payload, ok := ev.Payload.(*event.RouteGraphRequestPayload); ok {
			s.handleRouteGraphRequest(payload)
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

		// RouteIDs do not survive a rebuild: detach before invalidating
		s.clearAllRouteAssignments()
		s.world.Resources.RouteGraph.Clear()
		// Staged rebuild; refreshRouteGraphs drains one graph per interval
		s.routeRebuildTicks = parameter.NavRouteRebuildInterval
	}
}

// clearAllRouteAssignments detaches every routed entity; used when all graphs are invalidated
func (s *NavigationSystem) clearAllRouteAssignments() {
	s.world.Components.Navigation.Each(func(_ core.Entity, nav *component.NavigationComponent) bool {
		if nav.UseRouteGraph {
			nav.UseRouteGraph = false
			nav.RouteID = -1
		}
		return true
	})
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
	clampedMinX := max(0, minX)
	clampedMinY := max(0, minY)
	clampedMaxX := min(s.compositePassability.Width-1, maxX)
	clampedMaxY := min(s.compositePassability.Height-1, maxY)
	if clampedMinX <= clampedMaxX && clampedMinY <= clampedMaxY {
		s.statROICells.Add(int64((clampedMaxX - clampedMinX + 1) * (clampedMaxY - clampedMinY + 1)))
	}

	isWall := func(x, y int) bool {
		return s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic)
	}
	if s.compositePassability.ComputeROI(isWall, minX, minY, maxX, maxY) {
		s.grid++
	}
}

// resizePassability sizes the composite passability grid to the map, creating it
// the first time, and derives it from the walls.
func (s *NavigationSystem) resizePassability(width, height int) {
	if s.compositePassability == nil {
		s.compositePassability = navigation.NewCompositePassability(
			width, height,
			parameter.EyeWidth, parameter.EyeHeight,
			parameter.EyeHeaderOffsetX, parameter.EyeHeaderOffsetY,
		)
		if dbg := s.world.Resources.NavigationDebug; dbg != nil {
			dbg.CompositePassability = s.compositePassability
		}
	} else {
		s.compositePassability.Resize(width, height)
	}
	s.grid++
	s.recomputeCompositePassability()
}

// recomputeCompositePassability derives the passability grid from the walls as they
// stand and returns the wall test it read them through.
func (s *NavigationSystem) recomputeCompositePassability() navigation.WallChecker {
	isWall := s.world.Positions.WallTest(component.WallBlockKinetic, &s.walls)
	isWall(-1, -1) // the first call builds the grid
	s.noteWalls()
	if s.compositePassability != nil && s.compositePassability.Compute(isWall) {
		s.grid++
	}
	return isWall
}

// noteWalls moves grid when the wall grid last built differs from the one before.
func (s *NavigationSystem) noteWalls() {
	if !slices.Equal(s.walls, s.seenWalls) {
		s.seenWalls = append(s.seenWalls[:0], s.walls...)
		s.grid++
	}
}

func (s *NavigationSystem) Update() {
	if !s.enabled {
		return
	}

	config := s.world.Resources.Config

	// Handle map resize: one passability rebuild, then per-group caches
	if s.compositePassability != nil &&
		(config.MapWidth != s.compositePassability.Width || config.MapHeight != s.compositePassability.Height) {
		s.resizePassability(config.MapWidth, config.MapHeight)
	}
	for _, g := range s.groups {
		if config.MapWidth != g.pointFlowCache.Field.Width || config.MapHeight != g.pointFlowCache.Field.Height {
			g.pointFlowCache.Resize(config.MapWidth, config.MapHeight)
			g.compositeFlowCache.Resize(config.MapWidth, config.MapHeight)
		}
	}

	s.resolveGroupTargets()
	s.snapshotTargets()
	s.refreshRouteGraphs()

	isBlockedPoint := s.world.Positions.WallTest(component.WallBlockKinetic, &s.walls)

	// Wall checker for composites (uses pre-computed passability)
	isBlockedComposite := s.compositePassability.IsBlocked

	// Phase 1: Classify entities, perform LOS checks
	navigations := s.world.Components.Navigation
	// Navigation membership is stable across all three phases; event delivery
	// occurs outside Update, and these phases only overwrite existing values.
	entities := navigations.Entities()
	s.statEntities.Store(int64(len(entities)))

	for _, entity := range entities {
		navComp, ok := navigations.GetPtr(entity)
		if !ok {
			continue
		}

		groupID := s.getEntityGroup(entity)
		if _, groupExists := s.groups[groupID]; !groupExists {
			groupID = 0
		}

		groupState := s.world.Resources.Target.GetGroup(groupID)
		if !groupState.Valid || groupState.Count == 0 {
			navComp.HasDirectPath = false
			navComp.FlowX = 0
			navComp.FlowY = 0
			continue
		}

		// Retrieve closest target coordinate dynamically
		targetX, targetY, validTarget := resolveBaseTarget(s.world, entity)
		if !validTarget {
			continue
		}

		var gridX, gridY int
		if kinetic, ok := s.world.Components.Kinetic.GetComponent(entity); ok {
			cell := vmath.PointAtF(kinetic.PreciseX, kinetic.PreciseY)
			gridX, gridY = cell.X, cell.Y
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
			hasLOS = s.world.Positions.HasLineOfSight(gridX, gridY, targetX, targetY, component.WallBlockKinetic)
		} else {
			hasLOS = s.world.Positions.HasAreaLineOfSightRotatable(gridX, gridY, targetX, targetY, width, height, component.WallBlockKinetic)
		}

		if hasLOS {
			navComp.HasDirectPath = true
			navComp.FlowX = 0
			navComp.FlowY = 0
		} else {
			navComp.HasDirectPath = false
		}
	}

	// Phase 2: Update flow fields
	totalRecomputes := int64(0)
	var targetsBuffer [engine.MaxTargetsPerGroup]vmath.Point

	for groupID, g := range s.groups {
		groupState := s.world.Resources.Target.GetGroup(groupID)
		if !groupState.Valid || groupState.Count == 0 {
			continue
		}

		for i := range groupState.Count {
			targetsBuffer[i] = vmath.Point{X: groupState.Targets[i].PosX, Y: groupState.Targets[i].PosY}
		}
		targetsSlice := targetsBuffer[:groupState.Count]

		if g.pointFlowCache.Update(targetsSlice, isBlockedPoint) {
			totalRecomputes++
			s.noteWalls()
			g.pointAt = s.grid
		}

		if g.compositeFlowCache.Update(targetsSlice, isBlockedComposite) {
			totalRecomputes++
			g.compositeAt = s.grid
		}
	}
	s.statRecomputes.Store(totalRecomputes)

	// Phase 3: Update flow directions from cached fields
	for _, entity := range entities {
		navComp, ok := navigations.GetPtr(entity)
		if !ok || navComp.HasDirectPath {
			continue
		}

		groupID := s.getEntityGroup(entity)
		group, ok := s.groups[groupID]
		if !ok {
			groupID = 0
			group = s.groups[0]
		}
		groupState := s.world.Resources.Target.GetGroup(groupID)
		if !groupState.Valid || groupState.Count == 0 {
			navComp.FlowX = 0
			navComp.FlowY = 0
			continue
		}

		var preciseX, preciseY float64
		if kinetic, ok := s.world.Components.Kinetic.GetComponent(entity); ok {
			preciseX, preciseY = kinetic.PreciseX, kinetic.PreciseY
		} else if pos, ok := s.world.Positions.GetPosition(entity); ok {
			preciseX, preciseY = vmath.Point{X: pos.X, Y: pos.Y}.CenterF()
		} else {
			continue
		}

		isComposite := navComp.Width > 1 || navComp.Height > 1

		// Route graph: use per-route flow field when assigned
		if navComp.UseRouteGraph && navComp.RouteID >= 0 {
			if field := s.resolveRouteField(navComp.RouteGraphID, navComp.RouteID, groupID); field != nil {
				var fx, fy float64
				if isComposite {
					fx, fy = s.getCompositeFlowDirection(preciseX, preciseY, field)
				} else {
					fx, fy = navigation.InterpolatedDirection(field, preciseX, preciseY)
				}
				// zero flow = entity outside its narrow route corridor
				// (knockback, spawn relocation); fall through to shared field
				// instead of stalling
				if fx != 0 || fy != 0 {
					navComp.FlowX, navComp.FlowY = fx, fy
					continue
				}
			}
		}

		// Shared group flow field (default)
		if isComposite {
			navComp.FlowX, navComp.FlowY = s.getCompositeFlowDirection(preciseX, preciseY, group.compositeFlowCache)
		} else {
			navComp.FlowX, navComp.FlowY = navigation.InterpolatedDirection(group.pointFlowCache, preciseX, preciseY)
		}

	}

	if dbg := s.world.Resources.NavigationDebug; dbg != nil {
		if g, ok := s.groups[dbg.GroupID]; ok {
			dbg.Flow = g.pointFlowCache
			dbg.CompositeFlow = g.compositeFlowCache
		} else {
			dbg.Flow = nil
			dbg.CompositeFlow = nil
		}
	}
}

// handleGroupUpdate registers or retargets a group and dirties its flow caches
func (s *NavigationSystem) handleGroupUpdate(payload *event.TargetGroupUpdatePayload) {
	g := s.getOrCreateGroup(payload.GroupID)
	g.pointFlowCache.MarkDirty()
	g.compositeFlowCache.MarkDirty()

	posX, posY := payload.PosX, payload.PosY
	if payload.Type == component.TargetEntity && payload.Entity != 0 {
		// Retarget payloads carry no coordinates; resolve now to avoid a (0,0) tick
		pos, ok := s.world.Positions.GetPosition(payload.Entity)
		if !ok {
			return // dead entity: keep the current target
		}
		posX, posY = pos.X, pos.Y
	}

	var state engine.TargetGroupState
	state.Type = payload.Type
	state.Valid = true
	state.Count = 1
	state.Targets[0] = engine.TargetData{Entity: payload.Entity, PosX: posX, PosY: posY}

	s.world.Resources.Target.SetGroup(payload.GroupID, state)
}

// getOrCreateGroup returns group nav state, allocating flow caches on first use
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
	s.buffers.Observe(0, len(s.groups))
	return g
}

func (s *NavigationSystem) getEntityGroup(entity core.Entity) uint8 {
	if tc, ok := s.world.Components.Target.GetComponent(entity); ok {
		return tc.GroupID
	}
	return 0
}

// resolveGroupTargets refreshes TargetResource each tick: cursor (group 0),
// TargetAnchor scan, position sync for non-anchored groups, validity cleanup
func (s *NavigationSystem) resolveGroupTargets() {
	tr := s.world.Resources.Target

	// Group 0 contains every live cursor up to the navigation target cap.
	var cursorState engine.TargetGroupState
	cursorState.Type = component.TargetCursor
	for i := range parameter.MaxPlayers {
		if cursorState.Count >= engine.MaxTargetsPerGroup {
			break
		}
		e := s.world.Resources.Player.Slot(uint8(i))
		pos, ok := s.world.Positions.GetPosition(e)
		if !ok {
			continue
		}
		cursorState.Targets[cursorState.Count] = engine.TargetData{Entity: e, PosX: pos.X, PosY: pos.Y}
		cursorState.Count++
	}
	cursorState.Valid = cursorState.Count > 0
	tr.SetGroup(0, cursorState)

	// Accumulate anchors per group, publish once: per-anchor SetGroup zeroed earlier slots
	var anchorStates [component.MaxTargetGroups]engine.TargetGroupState
	var anchored [component.MaxTargetGroups]bool

	for _, entity := range s.world.Components.TargetAnchor.Entities() {
		anchor, ok := s.world.Components.TargetAnchor.GetPtr(entity)
		if !ok || anchor.GroupID == 0 || int(anchor.GroupID) >= component.MaxTargetGroups {
			continue
		}

		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		// Ensure flow caches exist for anchored groups
		s.getOrCreateGroup(anchor.GroupID)
		anchored[anchor.GroupID] = true

		st := &anchorStates[anchor.GroupID]
		if st.Count >= engine.MaxTargetsPerGroup {
			continue
		}
		st.Type = component.TargetEntity
		st.Valid = true
		st.Targets[st.Count] = engine.TargetData{Entity: entity, PosX: pos.X, PosY: pos.Y}
		st.Count++
	}

	// Resolve non-anchored groups and clean up obsolete anchors
	for groupID := uint8(1); groupID < component.MaxTargetGroups; groupID++ {
		if anchored[groupID] {
			tr.SetGroup(groupID, anchorStates[groupID])
			continue
		}

		state := tr.GetGroup(groupID)
		if !state.Valid || state.Count == 0 {
			continue
		}

		switch state.Type {
		case component.TargetEntity:
			if pos, ok := s.world.Positions.GetPosition(state.Targets[0].Entity); ok {
				state.Targets[0].PosX = pos.X
				state.Targets[0].PosY = pos.Y
			} else {
				state.Valid = false
			}
			tr.SetGroup(groupID, state)

		case component.TargetCursor:
			tr.SetGroup(groupID, cursorState)
		}
	}
}

// getCompositeFlowDirection returns flow direction from composite-aware flow field
// Handles case where entity's current cell is blocked in passability
func (s *NavigationSystem) getCompositeFlowDirection(preciseX, preciseY float64, src navigation.Source) (float64, float64) {
	cell := vmath.PointAtF(preciseX, preciseY)
	x0, y0 := cell.X, cell.Y

	dir := src.GetDirection(x0, y0)
	// At goal: zero flow lets the caller home directly instead of orbiting the cell
	if dir == navigation.DirTarget {
		return 0, 0
	}
	// Blocked/unvisited — escape to best neighbor
	if dir < 0 || dir >= navigation.DirCount {
		escDir := navigation.BestNeighborDirection(src, x0, y0)
		if escDir < 0 || escDir >= navigation.DirCount {
			return 0, 0
		}
		return navigation.UnitVectors[escDir][0], navigation.UnitVectors[escDir][1]
	}

	// Bilinear interpolation, header-anchored (no half-cell offset, unlike point entities)
	u := preciseX - float64(x0)
	v := preciseY - float64(y0)
	invU := 1.0 - u
	invV := 1.0 - v

	w00 := invU * invV
	w10 := u * invV
	w01 := invU * v
	w11 := u * v

	v00x, v00y, valid00 := navigation.FlowVector(src, x0, y0)
	v10x, v10y, valid10 := navigation.FlowVector(src, x0+1, y0)
	v01x, v01y, valid01 := navigation.FlowVector(src, x0, y0+1)
	v11x, v11y, valid11 := navigation.FlowVector(src, x0+1, y0+1)

	var sumX, sumY, totalWeight float64

	if valid00 {
		sumX += v00x * w00
		sumY += v00y * w00
		totalWeight += w00
	}
	if valid10 {
		sumX += v10x * w10
		sumY += v10y * w10
		totalWeight += w10
	}
	if valid01 {
		sumX += v01x * w01
		sumY += v01y * w01
		totalWeight += w01
	}
	if valid11 {
		sumX += v11x * w11
		sumY += v11y * w11
		totalWeight += w11
	}

	if totalWeight == 0 {
		return 0, 0
	}

	resX := sumX / totalWeight
	resY := sumY / totalWeight

	if resX != 0 || resY != 0 {
		return vmath.Normalize2DF(resX, resY)
	}
	return 0, 0
}

// findBestNeighborDirection finds direction toward lowest-distance passable neighbor, used when entity is at a blocked cell
// resolveRouteField returns the per-route flow field for an entity's route assignment
// Returns nil for invalid routes or graphs whose goal no longer matches the group
// target (retargeted tower), forcing fallback to the shared group flow field
func (s *NavigationSystem) resolveRouteField(graphID uint32, routeID int, groupID uint8) *navigation.FlowField {
	if graphID == 0 {
		return nil
	}

	graph := s.world.Resources.RouteGraph.Get(graphID)
	if graph == nil || routeID < 0 || routeID >= len(graph.Routes) {
		return nil
	}

	if !s.routeGraphFresh(graph, groupID) {
		return nil
	}

	field := graph.Routes[routeID].Field
	if field == nil || !field.Valid {
		return nil
	}

	return field
}

// snapshotTargets caches resolved group state for the tick
func (s *NavigationSystem) snapshotTargets() {
	tr := s.world.Resources.Target
	for gid := range s.targets {
		s.targets[gid] = tr.GetGroup(uint8(gid))
	}
}

// routeGraphFresh reports whether a graph's goal still matches a live target of the group
func (s *NavigationSystem) routeGraphFresh(graph *navigation.RouteGraph, groupID uint8) bool {
	if int(groupID) >= len(s.targets) {
		return false
	}
	state := &s.targets[groupID]
	if !state.Valid || state.Count == 0 {
		return false
	}
	for i := 0; i < state.Count; i++ {
		if state.Targets[i].PosX == graph.TargetX && state.Targets[i].PosY == graph.TargetY {
			return true
		}
	}
	return false
}

// clearRouteAssignments detaches entities from one graph so they use the shared group field
func (s *NavigationSystem) clearRouteAssignments(graphID uint32) {
	s.world.Components.Navigation.Each(func(_ core.Entity, nav *component.NavigationComponent) bool {
		if nav.UseRouteGraph && nav.RouteGraphID == graphID {
			nav.UseRouteGraph = false
			nav.RouteID = -1
		}
		return true
	})
}

// refreshRouteGraphs recomputes one stale gateway route graph per interval
// Dijkstra + per-route field cost forbids an unbudgeted sweep
func (s *NavigationSystem) refreshRouteGraphs() {
	s.routeRebuildTicks++
	if s.routeRebuildTicks < parameter.NavRouteRebuildInterval {
		return
	}

	for _, e := range s.world.Components.Gateway.Entities() {
		gw, ok := s.world.Components.Gateway.GetPtr(e)
		if !ok || gw.RouteDistID == 0 {
			continue
		}

		graph := s.world.Resources.RouteGraph.Get(gw.RouteDistID)
		if graph != nil && s.routeGraphFresh(graph, gw.GroupID) {
			continue
		}

		anchorPos, ok := s.world.Positions.GetPosition(gw.AnchorEntity)
		if !ok {
			continue
		}

		s.routeRebuildTicks = 0
		s.handleRouteGraphRequest(&event.RouteGraphRequestPayload{
			RouteGraphID:  gw.RouteDistID,
			SourceX:       anchorPos.X + gw.OffsetX,
			SourceY:       anchorPos.Y + gw.OffsetY,
			TargetGroupID: gw.GroupID,
		})
		return
	}
}

// handleRouteGraphRequest computes a route graph for a gateway-target pair
// Resolves target position from TargetResource or TargetAnchor fallback
func (s *NavigationSystem) handleRouteGraphRequest(payload *event.RouteGraphRequestPayload) {
	if s.compositePassability == nil {
		return
	}

	targetX, targetY, found := s.resolveTargetPosition(payload.TargetGroupID)
	if !found {
		return
	}

	rg := s.computeRouteGraph(payload.SourceX, payload.SourceY, targetX, targetY)
	replacing := s.world.Resources.RouteGraph.Get(payload.RouteGraphID) != nil
	if rg == nil {
		// Unreachable target: drop the stale graph rather than leave it authoritative
		if replacing {
			s.world.Resources.RouteGraph.Remove(payload.RouteGraphID)
			s.clearRouteAssignments(payload.RouteGraphID)
		}
		return
	}

	// Route indices change on recompute: detach in-flight assignments
	if replacing {
		s.clearRouteAssignments(payload.RouteGraphID)
	}
	s.world.Resources.RouteGraph.Set(payload.RouteGraphID, rg)

	s.world.PushEvent(event.EventRouteGraphComputed, &event.RouteGraphComputedPayload{
		RouteGraphID: payload.RouteGraphID,
		RouteCount:   len(rg.Routes),
	})
}

// computeRouteGraph routes source to target over the composite passability and
// stamps the graph with the grid generation it read.
func (s *NavigationSystem) computeRouteGraph(sourceX, sourceY, targetX, targetY int) *navigation.RouteGraph {
	config := s.world.Resources.Config
	rg := navigation.ComputeRouteGraph(
		sourceX, sourceY, targetX, targetY,
		config.MapWidth, config.MapHeight,
		parameter.EyeWidth, parameter.EyeHeight,
		parameter.EyeHeaderOffsetX, parameter.EyeHeaderOffsetY,
		s.compositePassability.IsBlocked,
	)
	if rg != nil {
		rg.Grid = s.grid
	}
	return rg
}

// resolveTargetPosition returns the position for a target group
// Checks TargetResource first, falls back to scanning TargetAnchor components
func (s *NavigationSystem) resolveTargetPosition(groupID uint8) (int, int, bool) {
	// Primary: TargetResource (populated by previous tick's Update)
	groupState := s.world.Resources.Target.GetGroup(groupID)
	if groupState.Valid && groupState.Count > 0 {
		return groupState.Targets[0].PosX, groupState.Targets[0].PosY, true
	}

	// Fallback: scan TargetAnchor components (handles same-tick registration)
	anchorEntities := s.world.Components.TargetAnchor.Entities()
	for _, e := range anchorEntities {
		anchor, ok := s.world.Components.TargetAnchor.GetPtr(e)
		if !ok || anchor.GroupID != groupID {
			continue
		}
		pos, ok := s.world.Positions.GetPosition(e)
		if !ok {
			continue
		}
		return pos.X, pos.Y, true
	}

	return 0, 0, false
}

// navSnapshot is the navigation system's D-17 derivation phase: where in the
// throttle cycle the capture stood, and the targets and cells each field and route
// graph was computed for. The fields, passability and graphs are re-derived at
// install rather than carried; instances recomputing on different ticks, or for
// different cells, steer differently.
type navSnapshot struct {
	RouteRebuildTicks int             `json:"route_rebuild_ticks"`
	Groups            []navGroupPhase `json:"groups"`

	// Routes names each gateway route graph held and the cells it was computed
	// between, so a receiver derives the sender's graphs rather than keeping its own.
	Routes []navRouteGraph `json:"routes,omitempty"`
}

// navRouteGraph is one gateway route graph's derivation inputs, named by the graph
// ID the gateway carries so it survives gateways being added or removed.
type navRouteGraph struct {
	GraphID uint32 `json:"graph_id"`
	SourceX int    `json:"source_x"`
	SourceY int    `json:"source_y"`
	TargetX int    `json:"target_x"`
	TargetY int    `json:"target_y"`
}

// navGroupPhase is one target group's throttle phase, named by group ID so the
// capture survives groups being created or destroyed between capture and install.
type navGroupPhase struct {
	GroupID          uint8         `json:"group_id"`
	PointTicks       int           `json:"point_ticks"`
	PointPending     bool          `json:"point_pending"`
	PointComputed    bool          `json:"point_computed"`
	PointLastTargets []vmath.Point `json:"point_last_targets"`

	CompositeTicks       int           `json:"composite_ticks"`
	CompositePending     bool          `json:"composite_pending"`
	CompositeComputed    bool          `json:"composite_computed"`
	CompositeLastTargets []vmath.Point `json:"composite_last_targets"`
}

// SaveShared carries the D-17 recompute phase (D-19). The fields themselves are
// derived at install and deliberately not serialized.
func (s *NavigationSystem) SaveShared() ([]byte, error) {
	snap := navSnapshot{RouteRebuildTicks: s.routeRebuildTicks}
	ids := make([]uint8, 0, len(s.groups))
	for id := range s.groups {
		ids = append(ids, id)
	}
	// Canonical order: a capture is compared as well as installed, so map order
	// must not reach the bytes.
	slices.Sort(ids)
	for _, id := range ids {
		g := s.groups[id]
		if g == nil {
			continue
		}
		phase := navGroupPhase{GroupID: id}
		if c := g.pointFlowCache; c != nil {
			phase.PointTicks, phase.PointPending = c.TicksSinceCompute, c.PendingUpdate
			phase.PointComputed = c.Computed()
			phase.PointLastTargets = slices.Clone(c.LastTargets)
		}
		if c := g.compositeFlowCache; c != nil {
			phase.CompositeTicks, phase.CompositePending = c.TicksSinceCompute, c.PendingUpdate
			phase.CompositeComputed = c.Computed()
			phase.CompositeLastTargets = slices.Clone(c.LastTargets)
		}
		snap.Groups = append(snap.Groups, phase)
	}
	snap.Routes = s.saveRouteGraphs()
	return json.Marshal(snap)
}

// saveRouteGraphs records what every live gateway route graph was computed between,
// in graph-ID order so two instances holding equal state produce equal bytes.
func (s *NavigationSystem) saveRouteGraphs() []navRouteGraph {
	if s.world.Resources.RouteGraph == nil {
		return nil
	}
	ids := make([]uint32, 0, 8)
	for _, e := range s.world.Components.Gateway.Entities() {
		gw, ok := s.world.Components.Gateway.GetPtr(e)
		if !ok || gw.RouteDistID == 0 {
			continue
		}
		ids = append(ids, gw.RouteDistID)
	}
	slices.Sort(ids)

	out := make([]navRouteGraph, 0, len(ids))
	for _, id := range ids {
		rg := s.world.Resources.RouteGraph.Get(id)
		if rg == nil {
			continue
		}
		out = append(out, navRouteGraph{
			GraphID: id,
			SourceX: rg.SourceX, SourceY: rg.SourceY,
			TargetX: rg.TargetX, TargetY: rg.TargetY,
		})
	}
	return out
}

// deriveRouteGraphs holds exactly the gateway route graphs the capture named, between
// the cells it named, and drops every other: a receiver's own graphs may be for
// gateways or cells the sender's are not, with route indices the installed entities
// name. A held graph stamped on this grid for those cells is kept, since recomputing
// it would give the same routes. Nothing is emitted; an install is not a run event.
func (s *NavigationSystem) deriveRouteGraphs(routes []navRouteGraph) {
	graphs := s.world.Resources.RouteGraph
	if graphs == nil || s.compositePassability == nil {
		return
	}
	held := make(map[uint32]*navigation.RouteGraph, len(routes))
	for _, r := range routes {
		if rg := graphs.Get(r.GraphID); rg != nil && rg.Grid == s.grid &&
			rg.SourceX == r.SourceX && rg.SourceY == r.SourceY &&
			rg.TargetX == r.TargetX && rg.TargetY == r.TargetY {
			held[r.GraphID] = rg
		}
	}
	graphs.Clear()
	live := make(map[uint32]struct{}, len(routes))
	for _, r := range routes {
		rg, ok := held[r.GraphID]
		if !ok {
			rg = s.computeRouteGraph(r.SourceX, r.SourceY, r.TargetX, r.TargetY)
		}
		if rg == nil {
			continue
		}
		graphs.Set(r.GraphID, rg)
		live[r.GraphID] = struct{}{}
	}
	// An entity whose graph did not come back has to stop naming it, or it steers by
	// a route index into a graph that is not there.
	s.world.Components.Navigation.Each(func(_ core.Entity, nav *component.NavigationComponent) bool {
		if !nav.UseRouteGraph {
			return true
		}
		if _, ok := live[nav.RouteGraphID]; !ok {
			nav.UseRouteGraph = false
			nav.RouteID = -1
		}
		return true
	})
}

// LoadShared restores the recompute phase and derives now what the capture does not
// carry: passability from the installed walls, each computed field from the targets
// its phase belongs to, and the route graphs. Left to the next tick, Update would
// compute from this tick's targets and reset the phase (D-17). A field or graph
// already computed from the same inputs on this grid is kept rather than recomputed.
func (s *NavigationSystem) LoadShared(data []byte) error {
	var snap navSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("navigation: phase: %w", err)
	}
	s.routeRebuildTicks = snap.RouteRebuildTicks

	isBlockedPoint := s.recomputeCompositePassability()
	isBlockedComposite := s.compositePassability.IsBlocked
	for _, phase := range snap.Groups {
		g := s.getOrCreateGroup(phase.GroupID)
		if g == nil {
			continue
		}
		g.pointAt = s.restoreField(g.pointFlowCache, g.pointAt, isBlockedPoint,
			phase.PointTicks, phase.PointPending, phase.PointComputed, phase.PointLastTargets)
		g.compositeAt = s.restoreField(g.compositeFlowCache, g.compositeAt, isBlockedComposite,
			phase.CompositeTicks, phase.CompositePending, phase.CompositeComputed, phase.CompositeLastTargets)
	}
	s.deriveRouteGraphs(snap.Routes)
	return nil
}

// restoreField sets one cache to a captured phase and returns the generation its
// field now stands at. The field is rebuilt from the phase's targets unless the one
// held was computed from them on this grid; a phase with no field clears the stamp,
// since the targets it leaves are no longer the field's.
func (s *NavigationSystem) restoreField(c *navigation.FlowFieldCache, at uint64, isBlocked navigation.WallChecker,
	ticks int, pending, computed bool, targets []vmath.Point) uint64 {
	if c == nil {
		return 0
	}
	held := at == s.grid && c.Field.Valid && slices.Equal(c.LastTargets, targets)
	c.TicksSinceCompute, c.PendingUpdate = ticks, pending
	c.LastTargets = append(c.LastTargets[:0], targets...)
	switch {
	case !computed:
		return 0
	case held:
		return at
	}
	c.Rebuild(isBlocked)
	return s.grid
}
