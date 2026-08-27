package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/navigation"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// LUT for normalized flow direction vectors
var flowDirLUT [8][2]float64

func init() {
	// Y halved for terminal 2:1 cell aspect before normalization
	for i, vec := range navigation.DirVectors {
		fx := float64(vec[0])
		fy := float64(vec[1]) * 0.5
		if fx != 0 || fy != 0 {
			fx, fy = vmath.Normalize2DF(fx, fy)
		}
		flowDirLUT[i] = [2]float64{fx, fy}
	}
}

// flowSource abstracts direction/distance queries over flow field data
// Satisfied by *navigation.FlowFieldCache and *navigation.FlowField
type flowSource interface {
	GetDirection(x, y int) int8
	GetDistance(x, y int) int
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

// DebugFlowGroupID selects which target group's flow fields are exposed to debug renderer
var DebugFlowGroupID uint8

// NavigationSystem resolves target groups, maintains per-group point and composite
// flow fields, tracks composite passability, and computes gateway route graphs
type NavigationSystem struct {
	world *engine.World

	// Per-group flow field management
	groups map[uint8]*targetGroupNav

	// Composite passability grid (shared, recomputed on wall changes)
	compositePassability *navigation.CompositePassability

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

	if s.world.Resources.Target == nil {
		s.world.Resources.Target = &engine.TargetResource{}
	}

	if s.world.Resources.RouteGraph == nil {
		s.world.Resources.RouteGraph = &engine.RouteGraphResource{}
	}

	// Debug exposure
	if g, ok := s.groups[DebugFlowGroupID]; ok {
		DebugFlow = g.pointFlowCache
		DebugCompositeFlow = g.compositeFlowCache
	}
	DebugCompositePassability = s.compositePassability
}

func (s *NavigationSystem) Name() string {
	return "navigation"
}

// Domain reports shared: flow fields and route graphs are derived from shared species alone.
func (s *NavigationSystem) Domain() engine.SystemDomain { return engine.SystemShared }

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
			if s.compositePassability == nil {
				s.compositePassability = navigation.NewCompositePassability(
					payload.Width, payload.Height,
					parameter.EyeWidth, parameter.EyeHeight,
					parameter.EyeHeaderOffsetX, parameter.EyeHeaderOffsetY,
				)
				DebugCompositePassability = s.compositePassability
			}

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

	// Handle map resize: one passability rebuild, then per-group caches
	if s.compositePassability != nil &&
		(config.MapWidth != s.compositePassability.Width || config.MapHeight != s.compositePassability.Height) {
		s.compositePassability.Resize(config.MapWidth, config.MapHeight)
		s.recomputeCompositePassability()
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

	// Wall checker for point entities
	isBlockedPoint := func(x, y int) bool {
		return s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic)
	}

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

		if recomputed := g.pointFlowCache.Update(targetsSlice, isBlockedPoint); recomputed {
			totalRecomputes++
		}

		if recomputed := g.compositeFlowCache.Update(targetsSlice, isBlockedComposite); recomputed {
			totalRecomputes++
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
					fx, fy = s.getInterpolatedFlowDirection(preciseX, preciseY, field)
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
			navComp.FlowX, navComp.FlowY = s.getInterpolatedFlowDirection(preciseX, preciseY, group.pointFlowCache)
		}

	}

	// Update debug pointers for selected group
	if g, ok := s.groups[DebugFlowGroupID]; ok {
		DebugFlow = g.pointFlowCache
		DebugCompositeFlow = g.compositeFlowCache
	} else {
		DebugFlow = nil
		DebugCompositeFlow = nil
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
func (s *NavigationSystem) getCompositeFlowDirection(preciseX, preciseY float64, src flowSource) (float64, float64) {
	cell := vmath.PointAtF(preciseX, preciseY)
	x0, y0 := cell.X, cell.Y

	dir := src.GetDirection(x0, y0)
	// At goal: zero flow lets the caller home directly instead of orbiting the cell
	if dir == navigation.DirTarget {
		return 0, 0
	}
	// Blocked/unvisited — escape to best neighbor
	if dir < 0 || dir >= navigation.DirCount {
		escDir := s.findBestNeighborDirection(x0, y0, src)
		if escDir < 0 || escDir >= navigation.DirCount {
			return 0, 0
		}
		return flowDirLUT[escDir][0], flowDirLUT[escDir][1]
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

	v00x, v00y, valid00 := s.getFlowVectorAndValidity(x0, y0, src)
	v10x, v10y, valid10 := s.getFlowVectorAndValidity(x0+1, y0, src)
	v01x, v01y, valid01 := s.getFlowVectorAndValidity(x0, y0+1, src)
	v11x, v11y, valid11 := s.getFlowVectorAndValidity(x0+1, y0+1, src)

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
func (s *NavigationSystem) findBestNeighborDirection(x, y int, src flowSource) int8 {
	bestDir := int8(-1)
	bestDist := 1 << 30

	for d := range navigation.DirCount {
		nx := x + navigation.DirVectors[d][0]
		ny := y + navigation.DirVectors[d][1]
		dist := src.GetDistance(nx, ny)
		if dist >= 0 && dist < bestDist {
			bestDist = dist
			bestDir = d
		}
	}
	return bestDir
}

// getInterpolatedFlowDirection performs bilinear interpolation for point entities
func (s *NavigationSystem) getInterpolatedFlowDirection(preciseX, preciseY float64, src flowSource) (float64, float64) {
	sampleX := preciseX - vmath.CellCenterF
	sampleY := preciseY - vmath.CellCenterF

	cell := vmath.PointAtF(sampleX, sampleY)
	x0, y0 := cell.X, cell.Y

	u := sampleX - float64(x0)
	v := sampleY - float64(y0)

	invU := 1.0 - u
	invV := 1.0 - v

	w00 := invU * invV
	w10 := u * invV
	w01 := invU * v
	w11 := u * v

	v00x, v00y, valid00 := s.getFlowVectorAndValidity(x0, y0, src)
	v10x, v10y, valid10 := s.getFlowVectorAndValidity(x0+1, y0, src)
	v01x, v01y, valid01 := s.getFlowVectorAndValidity(x0, y0+1, src)
	v11x, v11y, valid11 := s.getFlowVectorAndValidity(x0+1, y0+1, src)

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

func (s *NavigationSystem) getFlowVectorAndValidity(x, y int, src flowSource) (float64, float64, bool) {
	dir := src.GetDirection(x, y)
	if dir < 0 || dir >= navigation.DirCount {
		return 0, 0, false
	}
	return flowDirLUT[dir][0], flowDirLUT[dir][1], true
}

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

	config := s.world.Resources.Config
	rg := navigation.ComputeRouteGraph(
		payload.SourceX, payload.SourceY,
		targetX, targetY,
		config.MapWidth, config.MapHeight,
		parameter.EyeWidth, parameter.EyeHeight,
		parameter.EyeHeaderOffsetX, parameter.EyeHeaderOffsetY,
		s.compositePassability.IsBlocked,
	)
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
