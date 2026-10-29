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
	due                  bool // the composite field fell due this tick: its targets moved enough to matter
}

// NavigationSystem resolves target groups, maintains per-group point and composite
// flow fields, tracks composite passability, and computes gateway route graphs
type NavigationSystem struct {
	world *engine.World

	// Per-group flow field management
	groups  map[uint8]*targetGroupNav
	retired []*targetGroupNav // removed groups, whose caches the next new group takes
	solver  navigation.RouteSolver
	costs   navigation.Costs // the cell aspect every field and route is weighted at

	// Composite passability, moved cell by cell as the walls change
	compositePassability *navigation.CompositePassability
	// walls are what everything derived stands on, wallsW×wallsH, read from the
	// cells the wall log named since wallsSeq; scratch takes a whole re-read
	walls, scratch []bool
	wallsW, wallsH int
	wallsSeq       uint64

	// grid and comp are the generations of the walls and of the composite
	// passability: each moves when its tier changes, so a field or route graph
	// stamped with it when computed is exactly what recomputing it now would give.
	// derived is false until passability and the grids were derived whole.
	grid, comp uint64
	derived    bool

	// Per-tick resolved target snapshot; avoids per-entity TargetResource locking
	targets [component.MaxTargetGroups + parameter.MaxPlayers]engine.TargetGroupState

	// Ticks since last gateway route graph recompute (rebuild budget)
	routeRebuildTicks int

	// The grids every field and reach of a tier runs on, the generation they stand
	// at, and the cells a tick's wall change moved on each
	pointGrid, compositeGrid                         navigation.SearchGrid
	gridsAt                                          uint64
	pointOpened, pointClosed, compOpened, compClosed []vmath.Point
	reach                                            navigation.Reach
	recomputes                                       int64 // fields that fell due this tick

	statEntities   *atomic.Int64
	statRecomputes *atomic.Int64
	statROICells   *atomic.Int64
	buffers        bufferTelemetry

	toggle
}

func NewNavigationSystem(world *engine.World) engine.System {
	s := &NavigationSystem{
		world:  world,
		groups: make(map[uint8]*targetGroupNav),
		grid:   1,
		comp:   1,
	}

	s.statEntities = world.Resources.Status.Ints.Get("nav.entities")
	s.statRecomputes = world.Resources.Status.Ints.Get("nav.recomputes")
	s.statROICells = world.Resources.Status.Ints.Get("nav.roi_cells")
	s.buffers = newBufferTelemetry(world.Resources.Status, "nav", "groups")

	return s
}

func (s *NavigationSystem) Init() {
	s.costs = navigation.CostsFor(s.world.Resources.Config.CellAspect)
	s.solver.Costs = s.costs
	s.statEntities.Store(0)
	s.statRecomputes.Store(0)
	s.statROICells.Store(0)
	s.buffers.Reset()
	s.enabled = true
	s.groups = make(map[uint8]*targetGroupNav)
	clear(s.targets[:])
	s.routeRebuildTicks = 0
	s.gridsAt, s.wallsSeq, s.derived = 0, 0, false

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
	if s.world.Resources.Pursuit == nil {
		s.world.Resources.Pursuit = &engine.PursuitResource{}
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
		event.EventMetaSystemCommandRequest,
		event.EventLevelSetup,
		event.EventTargetGroupUpdate,
		event.EventTargetGroupRemove,
		event.EventNavigationRegraph,
		event.EventRouteGraphRequest,
	}
}

func (s *NavigationSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
		return
	}

	switch ev.Type {
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
			s.retire(payload.GroupID)
			s.world.Resources.Target.SetGroup(payload.GroupID, engine.TargetGroupState{})
		}

	case event.EventRouteGraphRequest:
		if payload, ok := ev.Payload.(*event.RouteGraphRequestPayload); ok {
			s.handleRouteGraphRequest(payload)
		}

	case event.EventNavigationRegraph:
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

// resizePassability sizes the composite passability grid to the map, creating it
// the first time; applyWalls derives it whole next.
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
	s.derived = false
}

// applyWalls brings passability, the grids and every field up to the walls as
// they stand, re-testing only the cells the wall log names: a cell closed drops
// the fields of its tier, cells opened repair them in place, and the composite
// tier moves only where a footprint's fit changed. A lost log re-reads every cell;
// a wholesale change, or the first derivation, derives everything whole.
func (s *NavigationSystem) applyWalls() {
	config := s.world.Resources.Config
	w, h := config.MapWidth, config.MapHeight
	isWall := func(x, y int) bool {
		return x >= 0 && y >= 0 && x < s.wallsW && y < s.wallsH && s.walls[y*s.wallsW+x]
	}
	// A restore carries the walls and passability the fields stand on, not the grids
	// searched on them: those are refilled before any change, which may find nothing
	if s.derived && s.gridsAt != s.grid {
		s.pointGrid.Fill(w, h, isWall)
		s.compositeGrid.Fill(w, h, s.compositePassability.IsBlocked)
		s.gridsAt = s.grid
	}
	cells, seq, logged := s.world.Positions.WallChanges(s.wallsSeq)
	s.wallsSeq = seq
	gw, gh := s.world.Positions.GridDimensions()
	ww, wh := max(min(w, gw), 0), max(min(h, gh), 0) // the cells a wall test covers
	whole := !s.derived || ww != s.wallsW || wh != s.wallsH
	if logged && !whole && len(cells) == 0 {
		return
	}
	s.pointOpened, s.pointClosed = s.pointOpened[:0], s.pointClosed[:0]
	s.compOpened, s.compClosed = s.compOpened[:0], s.compClosed[:0]
	// From an eighth of the map on, deriving whole costs no more than moving the
	// grids, even when every turned cell refits a whole footprint of headers
	changed, bulk := 0, ww*wh/8
	turned := func(x, y int, blocked bool) {
		if changed++; changed > bulk || x >= w || y >= h {
			return
		}
		if blocked {
			s.pointClosed = append(s.pointClosed, vmath.Point{X: x, Y: y})
		} else {
			s.pointOpened = append(s.pointOpened, vmath.Point{X: x, Y: y})
		}
	}
	switch {
	case whole || !logged:
		test := s.world.Positions.WallTest(component.WallBlockKinetic, &s.scratch)
		test(-1, -1) // the first call copies the grid
		if !whole {
			navigation.Diff(s.walls, s.scratch, ww, turned)
		}
		s.walls, s.scratch, s.wallsW, s.wallsH = s.scratch, s.walls, ww, wh
	default:
		for _, c := range cells {
			if c.X < 0 || c.Y < 0 || c.X >= ww || c.Y >= wh {
				continue
			}
			i := c.Y*ww + c.X
			if blocked := s.world.Positions.HasBlockingWallAt(c.X, c.Y, component.WallBlockKinetic); blocked != s.walls[i] {
				s.walls[i] = blocked
				turned(c.X, c.Y, blocked)
			}
		}
	}
	if !whole {
		if changed == 0 {
			return // the named cells stand as they stood
		}
		whole = changed > bulk
	}
	s.grid++
	if whole {
		s.compositePassability.Compute(isWall)
		s.pointGrid.Fill(w, h, isWall)
		s.compositeGrid.Fill(w, h, s.compositePassability.IsBlocked)
		s.comp, s.gridsAt, s.derived = s.comp+1, s.grid, true
		for _, g := range s.groups {
			g.pointFlowCache.Closed()
			g.compositeFlowCache.Closed()
			g.pointAt, g.compositeAt = s.grid, s.comp
		}
		return
	}
	s.pointGrid.Move(s.pointClosed, s.pointOpened)
	// Every header whose footprint covers a changed cell may fit differently.
	footW, footH, _, _ := s.compositePassability.GetFootprint()
	s.statROICells.Add(int64((len(s.pointClosed) + len(s.pointOpened)) * footW * footH))
	s.compositePassability.Moved(s.pointClosed, s.pointOpened, func(x, y int, valid bool) {
		if valid {
			s.compOpened = append(s.compOpened, vmath.Point{X: x, Y: y})
		} else {
			s.compClosed = append(s.compClosed, vmath.Point{X: x, Y: y})
		}
	})
	compMoved := len(s.compOpened)+len(s.compClosed) > 0
	if compMoved {
		s.comp++
		s.compositeGrid.Move(s.compClosed, s.compOpened)
	}
	s.gridsAt = s.grid
	for _, g := range s.groups {
		g.pointFlowCache.Moved(&s.pointGrid, s.pointClosed, s.pointOpened)
		g.pointAt = s.grid
		if compMoved {
			g.compositeFlowCache.Moved(&s.compositeGrid, s.compClosed, s.compOpened)
			g.compositeAt = s.comp
		}
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

	s.applyWalls()
	s.refreshRouteGraphs()

	// Build routes before choosing a pursuit target.
	s.recomputes = 0
	for groupID, g := range s.groups {
		s.refresh(groupID, g)
	}
	for slot := range parameter.MaxPlayers {
		var c *navigation.FlowFieldCache
		if g := s.groups[uint8(component.MaxTargetGroups+slot)]; g != nil {
			c = g.pointFlowCache
		}
		s.world.Resources.Pursuit.Publish(&s.pointGrid, uint8(slot), c)
	}

	// Classify entities and check LOS to the same target their route follows.
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

		if s.world.Components.SnakeHead.HasEntity(entity) {
			s.lockSnakeTarget(entity, navComp)
		}
		groupID := s.getEntityGroup(entity)
		if _, groupExists := s.groups[groupID]; !groupExists {
			groupID = 0
		}

		groupState := s.targets[groupID]
		if !groupState.Valid || groupState.Count == 0 {
			navComp.HasDirectPath = false
			navComp.FlowX = 0
			navComp.FlowY = 0
			continue
		}

		// Retrieve closest target coordinate dynamically
		targetX, targetY, validTarget := resolveBaseTarget(s.world, entity)
		if !validTarget {
			navComp.HasDirectPath, navComp.FlowX, navComp.FlowY = false, 0, 0
			continue
		}

		var gridX, gridY int
		if kinetic, ok := s.world.Components.Kinetic.GetPtr(entity); ok {
			cell := vmath.PointAt(kinetic.PreciseX, kinetic.PreciseY)
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

	// Resolve movement from the selected cache.
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
		groupState := s.targets[groupID]
		if !groupState.Valid || groupState.Count == 0 {
			navComp.FlowX = 0
			navComp.FlowY = 0
			continue
		}

		var preciseX, preciseY float64
		if kinetic, ok := s.world.Components.Kinetic.GetPtr(entity); ok {
			preciseX, preciseY = kinetic.PreciseX, kinetic.PreciseY
		} else if pos, ok := s.world.Positions.GetPosition(entity); ok {
			preciseX, preciseY = vmath.Point{X: pos.X, Y: pos.Y}.Center()
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
					fx, fy = s.costs.InterpolatedDirection(field, preciseX, preciseY)
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
			navComp.FlowX, navComp.FlowY = s.getCompositeFlowDirection(preciseX, preciseY, s.flow(group, true))
		} else {
			navComp.FlowX, navComp.FlowY = s.costs.InterpolatedDirection(s.flow(group, false), preciseX, preciseY)
		}

	}
	s.statRecomputes.Store(s.recomputes)

	if dbg := s.world.Resources.NavigationDebug; dbg != nil {
		if g, ok := s.groups[dbg.GroupID]; ok {
			if dbg.ShowFlow || dbg.ShowComposite {
				s.flow(g, false)
				s.flow(g, true)
			}
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
	if payload.GroupID >= component.MaxTargetGroups {
		return
	}
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
	var g *targetGroupNav
	if n := len(s.retired); n > 0 {
		g, s.retired = s.retired[n-1], s.retired[:n-1]
		*g = targetGroupNav{pointFlowCache: g.pointFlowCache, compositeFlowCache: g.compositeFlowCache}
		g.pointFlowCache.Reset(config.MapWidth, config.MapHeight)
		g.compositeFlowCache.Reset(config.MapWidth, config.MapHeight)
	} else {
		g = &targetGroupNav{
			pointFlowCache: navigation.NewFlowFieldCache(
				config.MapWidth, config.MapHeight,
				parameter.NavFlowMinTicksBetweenCompute,
				parameter.NavFlowDirtyDistance,
				parameter.NavFlowSettleTicks,
			),
			compositeFlowCache: navigation.NewFlowFieldCache(
				config.MapWidth, config.MapHeight,
				parameter.NavFlowMinTicksBetweenCompute,
				parameter.NavFlowDirtyDistance,
				parameter.NavFlowSettleTicks,
			),
		}
		g.pointFlowCache.Lazy, g.compositeFlowCache.Lazy = true, true
		g.pointFlowCache.Field.Costs, g.compositeFlowCache.Field.Costs = s.costs, s.costs
	}
	s.groups[groupID] = g
	s.buffers.Observe(0, len(s.groups))
	return g
}

// retire removes a group, keeping its caches for the next group created
func (s *NavigationSystem) retire(groupID uint8) {
	if g, ok := s.groups[groupID]; ok {
		s.retired = append(s.retired, g)
		delete(s.groups, groupID)
	}
}

// refresh lets a group's fields fall due on this tick's targets and grids.
func (s *NavigationSystem) refresh(groupID uint8, g *targetGroupNav) {
	g.due = false
	groupState := s.targets[groupID]
	if !groupState.Valid || groupState.Count == 0 {
		return
	}
	var targets [parameter.MaxTargetsPerGroup]vmath.Point
	for i := range groupState.Count {
		targets[i] = vmath.Point{X: groupState.Targets[i].PosX, Y: groupState.Targets[i].PosY}
	}
	if g.pointFlowCache.Update(targets[:groupState.Count], &s.pointGrid) {
		s.recomputes++
		g.pointAt = s.grid
	}
	if g.compositeFlowCache.Update(targets[:groupState.Count], &s.compositeGrid) {
		s.recomputes++
		g.compositeAt, g.due = s.comp, true
	}
}

// flow is a group's field for an entity of this shape, computed now if its
// throttle let it wait for a reader. Only Update may call it: the grids are its.
func (s *NavigationSystem) flow(g *targetGroupNav, composite bool) *navigation.FlowFieldCache {
	if composite {
		g.compositeFlowCache.Materialize(&s.compositeGrid)
		return g.compositeFlowCache
	}
	g.pointFlowCache.Materialize(&s.pointGrid)
	return g.pointFlowCache
}

func (s *NavigationSystem) getEntityGroup(entity core.Entity) uint8 {
	if nav, ok := s.world.Components.Navigation.GetPtr(entity); ok && nav.LockedTarget != 0 {
		if slot, ok := s.world.CursorSlot(nav.LockedTarget); ok {
			return component.MaxTargetGroups + slot
		}
	}
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
		if cursorState.Count >= parameter.MaxTargetsPerGroup {
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
		if st.Count >= parameter.MaxTargetsPerGroup {
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
	cell := vmath.PointAt(preciseX, preciseY)
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
		return s.costs.Unit[escDir][0], s.costs.Unit[escDir][1]
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

	v00x, v00y, valid00 := s.costs.FlowVector(src, x0, y0)
	v10x, v10y, valid10 := s.costs.FlowVector(src, x0+1, y0)
	v01x, v01y, valid01 := s.costs.FlowVector(src, x0, y0+1)
	v11x, v11y, valid11 := s.costs.FlowVector(src, x0+1, y0+1)

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
		return vmath.Normalize2D(resX, resY)
	}
	return 0, 0
}

// resolveRouteField returns the field of an entity's route, or nil for an invalid
// route or a graph whose goal no longer matches a group target (a retargeted
// tower), which sends the entity to the shared group field
func (s *NavigationSystem) resolveRouteField(graphID uint32, routeID int, groupID uint8) navigation.Source {
	if graphID == 0 {
		return nil
	}
	graph := s.world.Resources.RouteGraph.Get(graphID)
	if graph == nil || routeID < 0 || routeID >= len(graph.Routes) || !s.routeGraphFresh(graph, groupID) {
		return nil
	}
	if field := graph.Routes[routeID].Field; field != nil {
		return field
	}
	return nil
}

// snapshotTargets caches resolved group state for the tick, and keeps a private
// group for every cursor on the map: the same groups on every instance, so the
// phase each carries agrees, and a field only its readers' instance computes.
func (s *NavigationSystem) snapshotTargets() {
	tr := s.world.Resources.Target
	for gid := range component.MaxTargetGroups {
		s.targets[gid] = tr.GetGroup(uint8(gid))
	}
	for slot := range parameter.MaxPlayers {
		if !s.pursue(uint8(slot)) {
			gid := uint8(component.MaxTargetGroups + slot)
			s.targets[gid] = engine.TargetGroupState{}
			s.retire(gid)
		}
	}
}

// pursue publishes the private group of one cursor's slot, which steers the heads
// locked on it and the drops it owns, and reports whether the cursor stands on the map.
func (s *NavigationSystem) pursue(slot uint8) bool {
	e := s.world.Resources.Player.Slot(slot)
	pos, ok := s.world.Positions.GetPosition(e)
	if !ok {
		return false
	}
	gid := uint8(component.MaxTargetGroups) + slot
	s.targets[gid] = engine.TargetGroupState{Type: component.TargetCursor, Count: 1, Valid: true}
	s.targets[gid].Targets[0] = engine.TargetData{Entity: e, PosX: pos.X, PosY: pos.Y}
	s.getOrCreateGroup(gid)
	return true
}

// lockSnakeTarget holds a head on the cursor it reaches soonest. One reach from
// the head over the composite grid measures every cursor at once, when the head
// has no reachable lock, the roster moved enough for its field to fall due, or
// the settle interval passed; a lock holds until another route is over a third
// shorter, and a cursor the head cannot reach is never chosen.
func (s *NavigationSystem) lockSnakeTarget(entity core.Entity, nav *component.NavigationComponent) {
	groupID := uint8(0)
	if target, ok := s.world.Components.Target.GetPtr(entity); ok {
		groupID = target.GroupID
	}
	state := s.world.Resources.Target.GetGroup(groupID)
	if !state.Valid || state.Count == 0 {
		state, groupID = s.targets[0], 0
	}
	if state.Type != component.TargetCursor {
		nav.LockedTarget = 0
		return
	}
	pos, ok := s.world.Positions.GetPosition(entity)
	if !ok {
		nav.LockedTarget = 0
		return
	}
	var cursors [parameter.MaxPlayers]core.Entity
	var goals [parameter.MaxPlayers]vmath.Point
	n, held := 0, -1
	for slot := range parameter.MaxPlayers {
		e := s.world.Resources.Player.Slot(uint8(slot))
		p, ok := s.world.Positions.GetPosition(e)
		if !ok {
			continue
		}
		if e == nav.LockedTarget {
			held = n
		}
		cursors[n], goals[n] = e, vmath.Point{X: p.X, Y: p.Y}
		n++
	}
	nav.LockTicks++
	g := s.groups[groupID]
	if !(held < 0 && nav.LockTicks >= parameter.NavFlowMinTicksBetweenCompute ||
		g != nil && g.due || nav.LockTicks >= parameter.NavFlowSettleTicks) {
		return
	}
	nav.LockTicks = 0
	var costs [parameter.MaxPlayers]int32
	s.reach.Measure(&s.compositeGrid, &s.costs, pos.X, pos.Y, goals[:n], costs[:n])
	best, bestCost, heldCost := core.Entity(0), navigation.CostUnreachable, navigation.CostUnreachable
	for i := range n {
		cost := int(costs[i])
		if i == held {
			heldCost = cost
		}
		if cost < bestCost {
			best, bestCost = cursors[i], cost
		}
	}
	if heldCost < navigation.CostUnreachable && bestCost*3 >= heldCost*2 {
		return
	}
	nav.LockedTarget = best
}

// routeGraphFresh reports whether a graph's goal still matches a live target of the group
func (s *NavigationSystem) routeGraphFresh(graph *navigation.RouteGraph, groupID uint8) bool {
	if graph.Grid != s.comp || int(groupID) >= len(s.targets) {
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
	s.applyWalls()

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
	rg := s.solver.Compute(
		sourceX, sourceY, targetX, targetY,
		config.MapWidth, config.MapHeight,
		parameter.EyeWidth, parameter.EyeHeight,
		parameter.EyeHeaderOffsetX, parameter.EyeHeaderOffsetY,
		s.compositePassability.IsBlocked,
	)
	if rg != nil {
		rg.Grid = s.comp
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

	// Targets is every resolved target group: one an update set and no anchor holds
	// is in no other state, and nothing rebuilds it.
	Targets []navTargetGroup `json:"targets,omitempty"`
}

// navTargetGroup is one target group's resolved targets, named by group ID.
type navTargetGroup struct {
	GroupID uint8                   `json:"group_id"`
	State   engine.TargetGroupState `json:"state"`
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
	for id := range uint8(component.MaxTargetGroups) {
		if st := s.world.Resources.Target.GetGroup(id); st != (engine.TargetGroupState{}) {
			snap.Targets = append(snap.Targets, navTargetGroup{GroupID: id, State: st})
		}
	}
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
		if rg := graphs.Get(r.GraphID); rg != nil && rg.Grid == s.comp &&
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

	s.applyWalls()
	for id := range uint8(len(s.targets)) {
		if !slices.ContainsFunc(snap.Groups, func(phase navGroupPhase) bool { return phase.GroupID == id }) {
			delete(s.groups, id)
		}
	}
	for _, phase := range snap.Groups {
		if int(phase.GroupID) >= len(s.targets) {
			return fmt.Errorf("navigation: invalid cache group %d", phase.GroupID)
		}
		g := s.getOrCreateGroup(phase.GroupID)
		if g == nil {
			continue
		}
		g.pointAt = restoreField(g.pointFlowCache, g.pointAt, s.grid, &s.pointGrid,
			phase.PointTicks, phase.PointPending, phase.PointComputed, phase.PointLastTargets)
		g.compositeAt = restoreField(g.compositeFlowCache, g.compositeAt, s.comp, &s.compositeGrid,
			phase.CompositeTicks, phase.CompositePending, phase.CompositeComputed, phase.CompositeLastTargets)
	}
	s.deriveRouteGraphs(snap.Routes)
	for id := range uint8(component.MaxTargetGroups) {
		s.world.Resources.Target.SetGroup(id, engine.TargetGroupState{})
	}
	for _, g := range snap.Targets {
		if int(g.GroupID) >= component.MaxTargetGroups || g.State.Count < 0 || g.State.Count > len(g.State.Targets) {
			return fmt.Errorf("navigation: invalid target group %d", g.GroupID)
		}
		s.world.Resources.Target.SetGroup(g.GroupID, g.State)
	}
	return nil
}

// restoreField sets one cache to a captured phase and returns the generation its
// field now stands at. The field is rebuilt from the phase's targets unless the one
// held was computed from them at its tier's generation gen; a phase with no field
// clears the stamp, since the targets it leaves are no longer the field's.
func restoreField(c *navigation.FlowFieldCache, at, gen uint64, grid *navigation.SearchGrid,
	ticks int, pending, computed bool, targets []vmath.Point) uint64 {
	if c == nil {
		return 0
	}
	held := at == gen && c.Field.Valid && slices.Equal(c.LastTargets, targets)
	c.TicksSinceCompute, c.PendingUpdate = ticks, pending
	c.LastTargets = append(c.LastTargets[:0], targets...)
	switch {
	case !computed:
		return 0
	case held:
		return at
	}
	c.Rebuild(grid)
	return gen
}

type navState struct {
	groups            map[uint8]targetGroupNav
	passability       *navigation.CompositePassability
	walls             []bool
	wallsW, wallsH    int
	grid, comp        uint64
	derived           bool
	targets           [component.MaxTargetGroups + parameter.MaxPlayers]engine.TargetGroupState
	routeRebuildTicks int
	graphs            map[uint32]*navigation.RouteGraph
}

// CopyState carries every field and route a copy would otherwise rebuild, and the
// phase each was rebuilt in, which LoadShared re-derives as a joiner would.
func (s *NavigationSystem) CopyState() any {
	c := navState{groups: make(map[uint8]targetGroupNav, len(s.groups)), walls: slices.Clone(s.walls),
		wallsW: s.wallsW, wallsH: s.wallsH, grid: s.grid, comp: s.comp, derived: s.derived, targets: s.targets, routeRebuildTicks: s.routeRebuildTicks,
		graphs: s.world.Resources.RouteGraph.Graphs()}
	if s.compositePassability != nil {
		c.passability = s.compositePassability.Clone()
	}
	for id, g := range s.groups {
		c.groups[id] = targetGroupNav{pointFlowCache: g.pointFlowCache.Clone(), compositeFlowCache: g.compositeFlowCache.Clone(),
			pointAt: g.pointAt, compositeAt: g.compositeAt}
	}
	return c
}

func (s *NavigationSystem) RestoreState(v any) error {
	c := v.(navState)
	clear(s.groups)
	for id, g := range c.groups {
		s.groups[id] = &targetGroupNav{pointFlowCache: g.pointFlowCache.Clone(), compositeFlowCache: g.compositeFlowCache.Clone(),
			pointAt: g.pointAt, compositeAt: g.compositeAt}
	}
	s.compositePassability = nil
	if c.passability != nil {
		s.compositePassability = c.passability.Clone()
	}
	if dbg := s.world.Resources.NavigationDebug; dbg != nil {
		dbg.CompositePassability = s.compositePassability
	}
	s.walls, s.wallsW, s.wallsH, s.grid, s.comp, s.derived = slices.Clone(c.walls), c.wallsW, c.wallsH, c.grid, c.comp, c.derived
	s.gridsAt, s.wallsSeq = 0, 0
	s.targets, s.routeRebuildTicks = c.targets, c.routeRebuildTicks
	s.world.Resources.RouteGraph.SetGraphs(c.graphs)
	return nil
}
