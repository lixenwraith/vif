package system

import (
	"cmp"
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

// navTier is one passability tier: the headers a footprint fits on, the grid every
// field, reach and route graph of the footprint searches, the generation it last
// moved at, and the cells a tick's wall change turned on it
type navTier struct {
	foot           navigation.Footprint
	pass           *navigation.CompositePassability // nil on the point tier, whose passability is the walls
	grid           navigation.SearchGrid
	gen            uint64
	opened, closed []vmath.Point
}

// tierField is a group's field on one tier, the tier's generation it was last
// computed on (zero is none, since generations start at one), and whether it fell
// due this tick: its targets moved enough to matter
type tierField struct {
	cache *navigation.FlowFieldCache
	at    uint64
	due   bool
}

// targetGroupNav holds a group's field on every tier, in the system's tier order
type targetGroupNav struct {
	fields []tierField
}

// eyeFootprint is the composite tier every body it covers steers by
var eyeFootprint = navigation.Footprint{
	Width: parameter.EyeWidth, Height: parameter.EyeHeight,
	HeaderX: parameter.EyeHeaderOffsetX, HeaderY: parameter.EyeHeaderOffsetY,
}

// NavigationSystem resolves target groups, maintains each group's flow field on
// every passability tier, keeps the tiers up to the walls, and computes gateway
// route graphs
type NavigationSystem struct {
	world *engine.World

	// Per-group flow field management
	groups  map[uint8]*targetGroupNav
	retired []*targetGroupNav // removed groups, whose caches the next new group takes
	solver  navigation.RouteSolver
	costs   navigation.Costs // the cell aspect every field and route is weighted at

	// tiers are the point tier, the eye's, which every body it covers rides, then one
	// for each other footprint a navigator or a route graph needs, in footprint order
	tiers []*navTier
	// walls are what everything derived stands on, wallsW×wallsH, read from the
	// cells the wall log named since wallsSeq; scratch takes a whole re-read
	walls, scratch []bool
	wallsW, wallsH int
	wallsSeq       uint64

	// gen is the last generation a tier took: a tier takes the next as it moves, so
	// a field or route graph stamped with its tier's when computed is exactly what
	// recomputing it now would give. derived is false until the tiers were derived
	// whole, and gridsAt is the point tier's generation the grids were filled at.
	gen     uint64
	derived bool
	gridsAt uint64

	// Per-tick resolved target snapshot; avoids per-entity TargetResource locking
	targets [component.MaxTargetGroups + parameter.MaxPlayers]engine.TargetGroupState

	// Ticks since last gateway route graph recompute (rebuild budget), and since a
	// reader out of sight last read its field
	routeRebuildTicks, refreshTicks int

	reach      navigation.Reach
	recomputes int64                  // fields that fell due this tick
	need       []navigation.Footprint // syncTiers' scratch

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
	s.routeRebuildTicks, s.refreshTicks = 0, 0
	s.gridsAt, s.wallsSeq, s.derived = 0, 0, false
	s.tiers = []*navTier{{foot: navigation.Point}, {foot: eyeFootprint}}

	config := s.world.Resources.Config
	s.resize(config.MapWidth, config.MapHeight)
	s.getOrCreateGroup(0)

	if s.world.Resources.Target == nil {
		s.world.Resources.Target = &engine.TargetResource{}
	}

	if s.world.Resources.RouteGraph == nil {
		s.world.Resources.RouteGraph = &engine.RouteGraphResource{}
	}
	if s.world.Resources.Pursuit == nil {
		s.world.Resources.Pursuit = &engine.PursuitResource{}
	}

	s.showDebug()
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
			s.resize(payload.Width, payload.Height)
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
			for i := 1; i < len(g.fields); i++ {
				g.fields[i].cache.MarkDirty()
			}
		}

		// RouteIDs do not survive a rebuild: detach before invalidating
		s.clearAllRouteAssignments()
		s.world.Resources.RouteGraph.Clear()
		// Staged rebuild; refreshRouteGraphs drains one graph per interval
		s.routeRebuildTicks = s.rebuildEvery()
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

// resize sizes every tier's passability and every group's fields to the map; the
// next applyWalls derives the tiers whole
func (s *NavigationSystem) resize(width, height int) {
	for _, t := range s.tiers[1:] {
		if t.pass == nil {
			t.pass = navigation.NewCompositePassability(width, height, t.foot)
		} else {
			t.pass.Resize(width, height)
		}
	}
	for _, g := range s.groups {
		for _, f := range g.fields {
			f.cache.Resize(width, height)
		}
	}
	s.derived = false
	s.showDebug()
}

// wallAt is the walls everything derived stands on, none off them
func (s *NavigationSystem) wallAt(x, y int) bool {
	return x >= 0 && y >= 0 && x < s.wallsW && y < s.wallsH && s.walls[y*s.wallsW+x]
}

// blocked is the test a tier's grid and route graphs read: the walls on the point
// tier, the cells no header fits on on a composite one
func (t *navTier) blocked(s *NavigationSystem) navigation.WallChecker {
	if t.pass == nil {
		return s.wallAt
	}
	return t.pass.IsBlocked
}

// applyWalls brings every tier, its grid and its fields up to the walls as they
// stand, re-testing only the cells the wall log names: a cell closed drops the
// fields of its tier, cells opened repair them in place, and a composite tier
// moves only where its footprint's fit changed. A lost log re-reads every cell; a
// wholesale change, or the first derivation, derives everything whole.
func (s *NavigationSystem) applyWalls() {
	config := s.world.Resources.Config
	w, h := config.MapWidth, config.MapHeight
	point := s.tiers[0]
	// A restore carries the walls and passability the fields stand on, not the grids
	// searched on them: those are refilled before any change, which may find nothing
	if s.derived && s.gridsAt != point.gen {
		for _, t := range s.tiers {
			t.grid.Fill(w, h, t.blocked(s))
		}
		s.gridsAt = point.gen
	}
	cells, seq, logged := s.world.Positions.WallChanges(s.wallsSeq)
	s.wallsSeq = seq
	gw, gh := s.world.Positions.GridDimensions()
	ww, wh := max(min(w, gw), 0), max(min(h, gh), 0) // the cells a wall test covers
	whole := !s.derived || ww != s.wallsW || wh != s.wallsH
	if logged && !whole && len(cells) == 0 {
		return
	}
	for _, t := range s.tiers {
		t.opened, t.closed = t.opened[:0], t.closed[:0]
	}
	// From an eighth of the map on, deriving whole costs no more than moving the
	// grids, even when every turned cell refits a whole footprint of headers
	changed, bulk := 0, ww*wh/8
	turned := func(x, y int, blocked bool) {
		if changed++; changed > bulk || x >= w || y >= h {
			return
		}
		if blocked {
			point.closed = append(point.closed, vmath.Point{X: x, Y: y})
		} else {
			point.opened = append(point.opened, vmath.Point{X: x, Y: y})
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
	if whole {
		for _, t := range s.tiers {
			s.derive(t)
		}
		s.gridsAt, s.derived = point.gen, true
		for _, g := range s.groups {
			for i, t := range s.tiers {
				g.fields[i].cache.Closed()
				g.fields[i].at = t.gen
			}
		}
		return
	}
	point.grid.Move(point.closed, point.opened)
	s.gen++
	point.gen = s.gen
	for _, t := range s.tiers[1:] {
		// Every header whose footprint covers a changed cell may fit differently.
		s.statROICells.Add(int64((len(point.closed) + len(point.opened)) * t.foot.Width * t.foot.Height))
		t.pass.Moved(point.closed, point.opened, func(x, y int, valid bool) {
			if valid {
				t.opened = append(t.opened, vmath.Point{X: x, Y: y})
			} else {
				t.closed = append(t.closed, vmath.Point{X: x, Y: y})
			}
		})
		if len(t.opened)+len(t.closed) > 0 {
			t.grid.Move(t.closed, t.opened)
			s.gen++
			t.gen = s.gen
		}
	}
	s.gridsAt = point.gen
	for _, g := range s.groups {
		for i, t := range s.tiers {
			if i == 0 || len(t.opened)+len(t.closed) > 0 {
				g.fields[i].cache.Moved(&t.grid, t.closed, t.opened)
				g.fields[i].at = t.gen
			}
		}
	}
}

// derive computes a tier whole from the walls and moves its generation
func (s *NavigationSystem) derive(t *navTier) {
	config := s.world.Resources.Config
	if t.pass != nil {
		t.pass.Compute(s.wallAt)
	}
	t.grid.Fill(config.MapWidth, config.MapHeight, t.blocked(s))
	s.gen++
	t.gen = s.gen
}

// validBody reports whether a footprint is one a body can have: its header on it,
// and no larger than the largest a data species may declare
func validBody(f navigation.Footprint) bool {
	return f.Width >= 1 && f.Height >= 1 && f.Width <= parameter.BehaviorFootprintMax && f.Height <= parameter.BehaviorFootprintMax &&
		f.HeaderX >= 0 && f.HeaderY >= 0 && f.HeaderX < f.Width && f.HeaderY < f.Height
}

// bodyOf is the footprint a navigator declares
func bodyOf(nav *component.NavigationComponent) navigation.Footprint {
	return navigation.Footprint{Width: max(nav.Width, 1), Height: max(nav.Height, 1), HeaderX: nav.HeaderX, HeaderY: nav.HeaderY}
}

// tierOf is the index of the tier a body steers by: the point tier for one cell
// across, the eye's for a body its cells hold, else the body's own, -1 while no
// tier is kept for it
func (s *NavigationSystem) tierOf(foot navigation.Footprint) int {
	switch {
	case foot.Width <= 1 && foot.Height <= 1:
		return 0
	case eyeFootprint.Covers(foot):
		return 1
	}
	for i := 2; i < len(s.tiers); i++ {
		if s.tiers[i].foot == foot {
			return i
		}
	}
	return -1
}

// keepTier is the tier a body steers by, derived from the walls as they stand
// when the body is the first to need it, every group taking a field on it
func (s *NavigationSystem) keepTier(foot navigation.Footprint) int {
	if i := s.tierOf(foot); i >= 0 {
		return i
	}
	config := s.world.Resources.Config
	t := &navTier{foot: foot, pass: navigation.NewCompositePassability(config.MapWidth, config.MapHeight, foot)}
	i := 2 + slices.IndexFunc(s.tiers[2:], func(o *navTier) bool { return footOrder(foot, o.foot) < 0 })
	if i < 2 {
		i = len(s.tiers)
	}
	s.tiers = slices.Insert(s.tiers, i, t)
	for _, g := range s.groups {
		g.fields = slices.Insert(g.fields, i, s.newField())
	}
	if s.derived {
		s.derive(t)
	}
	return i
}

// syncTiers keeps a tier for each footprint the eye's does not cover that a
// navigator or a gateway's route graph steers by, and no other
func (s *NavigationSystem) syncTiers() {
	need := s.need[:0]
	keep := func(foot navigation.Footprint) {
		if foot.Width > 1 || foot.Height > 1 {
			if !eyeFootprint.Covers(foot) && !slices.Contains(need, foot) {
				need = append(need, foot)
			}
		}
	}
	s.world.Components.Navigation.Each(func(_ core.Entity, nav *component.NavigationComponent) bool {
		keep(bodyOf(nav))
		return true
	})
	for _, e := range s.world.Components.Gateway.Entities() {
		if gw, ok := s.world.Components.Gateway.GetPtr(e); ok && gw.RouteDistID != 0 {
			if rg := s.world.Resources.RouteGraph.Get(gw.RouteDistID); rg != nil {
				keep(rg.Footprint)
			}
		}
	}
	for i := len(s.tiers) - 1; i >= 2; i-- {
		if !slices.Contains(need, s.tiers[i].foot) {
			s.tiers = slices.Delete(s.tiers, i, i+1)
			for _, g := range s.groups {
				g.fields = slices.Delete(g.fields, i, i+1)
			}
		}
	}
	for _, foot := range need {
		s.keepTier(foot)
	}
	s.need = need
}

// footOrder orders footprints by width, height and header, the order extra tiers keep
func footOrder(a, b navigation.Footprint) int {
	return cmp.Or(cmp.Compare(a.Width, b.Width), cmp.Compare(a.Height, b.Height),
		cmp.Compare(a.HeaderX, b.HeaderX), cmp.Compare(a.HeaderY, b.HeaderY))
}

// showDebug points the debug view at its group's point field and the eye tier's
// field and passability
func (s *NavigationSystem) showDebug() {
	dbg := s.world.Resources.NavigationDebug
	if dbg == nil || len(s.tiers) < 2 {
		return
	}
	dbg.CompositePassability = s.tiers[1].pass
	dbg.Flow, dbg.CompositeFlow = nil, nil
	if g, ok := s.groups[dbg.GroupID]; ok {
		dbg.Flow, dbg.CompositeFlow = g.fields[0].cache, g.fields[1].cache
	}
}

func (s *NavigationSystem) Update() {
	if !s.enabled {
		return
	}

	config := s.world.Resources.Config
	if p := s.tiers[1].pass; config.MapWidth != p.Width || config.MapHeight != p.Height {
		s.resize(config.MapWidth, config.MapHeight)
	}

	s.resolveGroupTargets()
	s.snapshotTargets()

	s.applyWalls()
	s.syncTiers()
	s.refreshRouteGraphs()

	// Build routes before choosing a pursuit target.
	s.recomputes = 0
	for groupID, g := range s.groups {
		s.refresh(groupID, g)
	}
	// A reader out of sight reads its field every NavRefreshTicks and holds what it
	// read between, so a field over a moving map is computed once in as many ticks
	fresh := s.refreshTicks == 0
	s.refreshTicks = (s.refreshTicks + 1) % max(config.NavRefreshTicks, 1)
	for slot := range parameter.MaxPlayers {
		var c *navigation.FlowFieldCache
		if g := s.groups[uint8(component.MaxTargetGroups+slot)]; g != nil {
			c = g.fields[0].cache
		}
		s.world.Resources.Pursuit.Publish(&s.tiers[0].grid, !fresh, uint8(slot), c)
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

	// Resolve movement from the selected cache; between reads a navigator out of sight
	// holds its flow, and one that went out of sight since homes on its target
	for _, entity := range entities {
		navComp, ok := navigations.GetPtr(entity)
		if !ok || navComp.HasDirectPath || !fresh {
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

		tier := s.keepTier(bodyOf(navComp))
		isComposite := tier > 0

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
			navComp.FlowX, navComp.FlowY = s.getCompositeFlowDirection(preciseX, preciseY, s.flow(group, tier))
		} else {
			navComp.FlowX, navComp.FlowY = s.costs.InterpolatedDirection(s.flow(group, tier), preciseX, preciseY)
		}

	}
	s.statRecomputes.Store(s.recomputes)

	if dbg := s.world.Resources.NavigationDebug; dbg != nil {
		if g, ok := s.groups[dbg.GroupID]; ok && (dbg.ShowFlow || dbg.ShowComposite) {
			s.flow(g, 0)
			s.flow(g, 1)
		}
		s.showDebug()
	}
}

// handleGroupUpdate registers or retargets a group and dirties its flow caches
func (s *NavigationSystem) handleGroupUpdate(payload *event.TargetGroupUpdatePayload) {
	if payload.GroupID >= component.MaxTargetGroups {
		return
	}
	g := s.getOrCreateGroup(payload.GroupID)
	for _, f := range g.fields {
		f.cache.MarkDirty()
	}

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

// getOrCreateGroup returns group nav state, a field on every tier, taking the
// caches of a retired group before allocating
func (s *NavigationSystem) getOrCreateGroup(groupID uint8) *targetGroupNav {
	if g, ok := s.groups[groupID]; ok {
		return g
	}
	config := s.world.Resources.Config
	g := &targetGroupNav{}
	if n := len(s.retired); n > 0 {
		g, s.retired = s.retired[n-1], s.retired[:n-1]
	}
	kept := g.fields
	g.fields = make([]tierField, len(s.tiers))
	for i := range g.fields {
		if i < len(kept) {
			kept[i].cache.Reset(config.MapWidth, config.MapHeight)
			g.fields[i] = tierField{cache: kept[i].cache}
		} else {
			g.fields[i] = s.newField()
		}
	}
	s.groups[groupID] = g
	s.buffers.Observe(0, len(s.groups))
	return g
}

// newField is a lazy cache at the map's size, weighted at the run's cell aspect
func (s *NavigationSystem) newField() tierField {
	config := s.world.Resources.Config
	c := navigation.NewFlowFieldCache(config.MapWidth, config.MapHeight,
		parameter.NavFlowMinTicksBetweenCompute, parameter.NavFlowDirtyDistance, parameter.NavFlowSettleTicks)
	c.Lazy, c.Field.Costs = true, s.costs
	return tierField{cache: c}
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
	for i := range g.fields {
		g.fields[i].due = false
	}
	groupState := s.targets[groupID]
	if !groupState.Valid || groupState.Count == 0 {
		return
	}
	var targets [parameter.MaxTargetsPerGroup]vmath.Point
	for i := range groupState.Count {
		targets[i] = vmath.Point{X: groupState.Targets[i].PosX, Y: groupState.Targets[i].PosY}
	}
	for i, t := range s.tiers {
		if f := &g.fields[i]; f.cache.Update(targets[:groupState.Count], &t.grid) {
			s.recomputes++
			f.at, f.due = t.gen, true
		}
	}
}

// flow is a group's field on a tier, computed now if its throttle let it wait for
// a reader. Only Update may call it: the grids are its.
func (s *NavigationSystem) flow(g *targetGroupNav, tier int) *navigation.FlowFieldCache {
	c := g.fields[tier].cache
	c.Materialize(&s.tiers[tier].grid)
	return c
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
// the head over its tier's grid measures every cursor at once, when the head
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
	tier := s.keepTier(bodyOf(nav))
	g := s.groups[groupID]
	if !(held < 0 && nav.LockTicks >= parameter.NavFlowMinTicksBetweenCompute ||
		g != nil && g.fields[tier].due || nav.LockTicks >= parameter.NavFlowSettleTicks) {
		return
	}
	nav.LockTicks = 0
	var costs [parameter.MaxPlayers]int32
	s.reach.Measure(&s.tiers[tier].grid, &s.costs, pos.X, pos.Y, goals[:n], costs[:n])
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

// routeGraphFresh reports whether a graph stands on its tier as it is and its goal
// still matches a live target of the group
func (s *NavigationSystem) routeGraphFresh(graph *navigation.RouteGraph, groupID uint8) bool {
	if tier := s.tierOf(graph.Footprint); tier < 0 || graph.Grid != s.tiers[tier].gen || int(groupID) >= len(s.targets) {
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
	if s.routeRebuildTicks < s.rebuildEvery() {
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

		// A gateway's routes are for the eyes it spawns, unless a request named another body
		foot := eyeFootprint
		if graph != nil {
			foot = graph.Footprint
		}
		s.routeRebuildTicks = 0
		s.handleRouteGraphRequest(&event.RouteGraphRequestPayload{
			RouteGraphID:  gw.RouteDistID,
			SourceX:       anchorPos.X + gw.OffsetX,
			SourceY:       anchorPos.Y + gw.OffsetY,
			TargetGroupID: gw.GroupID,
			Width:         foot.Width, Height: foot.Height, HeaderX: foot.HeaderX, HeaderY: foot.HeaderY,
		})
		return
	}
}

// rebuildEvery is how many ticks a stale gateway route graph waits between rebuilds
func (s *NavigationSystem) rebuildEvery() int {
	if n := s.world.Resources.Config.NavRebuildTicks; n > 0 {
		return n
	}
	return parameter.NavRouteRebuildInterval
}

// handleRouteGraphRequest computes a route graph for a gateway-target pair
// Resolves target position from TargetResource or TargetAnchor fallback
func (s *NavigationSystem) handleRouteGraphRequest(payload *event.RouteGraphRequestPayload) {
	if len(s.tiers) == 0 {
		return
	}
	s.applyWalls()

	targetX, targetY, found := s.resolveTargetPosition(payload.TargetGroupID)
	if !found {
		return
	}

	foot := eyeFootprint
	if payload.Width > 0 || payload.Height > 0 {
		foot = navigation.Footprint{Width: payload.Width, Height: payload.Height, HeaderX: payload.HeaderX, HeaderY: payload.HeaderY}
		if !validBody(foot) {
			return
		}
	}
	rg := s.computeRouteGraph(payload.SourceX, payload.SourceY, targetX, targetY, foot)
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

// computeRouteGraph routes a body from source to target over the tier it steers by
// and stamps the graph with the tier's generation it read.
func (s *NavigationSystem) computeRouteGraph(sourceX, sourceY, targetX, targetY int, foot navigation.Footprint) *navigation.RouteGraph {
	config := s.world.Resources.Config
	t := s.tiers[s.keepTier(foot)]
	rg := s.solver.Compute(sourceX, sourceY, targetX, targetY, config.MapWidth, config.MapHeight, foot, t.blocked(s))
	if rg != nil {
		rg.Grid = t.gen
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
	RefreshTicks      int             `json:"refresh_ticks,omitempty"`
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
	GraphID   uint32               `json:"graph_id"`
	SourceX   int                  `json:"source_x"`
	SourceY   int                  `json:"source_y"`
	TargetX   int                  `json:"target_x"`
	TargetY   int                  `json:"target_y"`
	Footprint navigation.Footprint `json:"footprint"`
}

// navGroupPhase is one target group's throttle phase on every tier, named by group
// ID so the capture survives groups being created or destroyed between capture and
// install.
type navGroupPhase struct {
	GroupID uint8           `json:"group_id"`
	Fields  []navFieldPhase `json:"fields"`
}

// navFieldPhase is a group's throttle phase on one tier, named by the tier's
// footprint so it survives tiers being kept or dropped.
type navFieldPhase struct {
	Footprint   navigation.Footprint `json:"footprint"`
	Ticks       int                  `json:"ticks"`
	Pending     bool                 `json:"pending"`
	Computed    bool                 `json:"computed"`
	LastTargets []vmath.Point        `json:"last_targets"`
}

// SaveShared carries the D-17 recompute phase (D-19). The fields themselves are
// derived at install and deliberately not serialized.
func (s *NavigationSystem) SaveShared() ([]byte, error) {
	snap := navSnapshot{RouteRebuildTicks: s.routeRebuildTicks, RefreshTicks: s.refreshTicks}
	ids := make([]uint8, 0, len(s.groups))
	for id := range s.groups {
		ids = append(ids, id)
	}
	// Canonical order: a capture is compared as well as installed, so map order
	// must not reach the bytes.
	slices.Sort(ids)
	for _, id := range ids {
		phase := navGroupPhase{GroupID: id}
		for i, t := range s.tiers {
			c := s.groups[id].fields[i].cache
			phase.Fields = append(phase.Fields, navFieldPhase{Footprint: t.foot, Ticks: c.TicksSinceCompute,
				Pending: c.PendingUpdate, Computed: c.Computed(), LastTargets: slices.Clone(c.LastTargets)})
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
			Footprint: rg.Footprint,
		})
	}
	return out
}

// deriveRouteGraphs holds exactly the gateway route graphs the capture named, between
// the cells and for the bodies it named, and drops every other: a receiver's own
// graphs may be for gateways or cells the sender's are not, with route indices the
// installed entities name. A held graph stamped on its tier as it stands for those
// cells is kept, since recomputing it would give the same routes. Nothing is
// emitted; an install is not a run event.
func (s *NavigationSystem) deriveRouteGraphs(routes []navRouteGraph) {
	graphs := s.world.Resources.RouteGraph
	if graphs == nil || len(s.tiers) == 0 {
		return
	}
	held := make(map[uint32]*navigation.RouteGraph, len(routes))
	for _, r := range routes {
		if rg := graphs.Get(r.GraphID); rg != nil && rg.Footprint == r.Footprint &&
			rg.Grid == s.tiers[s.keepTier(r.Footprint)].gen &&
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
			rg = s.computeRouteGraph(r.SourceX, r.SourceY, r.TargetX, r.TargetY, r.Footprint)
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
// carry: the tiers it names from the installed walls, each computed field from the
// targets its phase belongs to, and the route graphs. Left to the next tick, Update
// would compute from this tick's targets and reset the phase (D-17). A field or graph
// already computed from the same inputs on its tier as it stands is kept.
func (s *NavigationSystem) LoadShared(data []byte) error {
	var snap navSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("navigation: phase: %w", err)
	}
	s.routeRebuildTicks, s.refreshTicks = snap.RouteRebuildTicks, snap.RefreshTicks

	s.applyWalls()
	var feet []navigation.Footprint
	for _, phase := range snap.Groups {
		if int(phase.GroupID) >= len(s.targets) {
			return fmt.Errorf("navigation: invalid cache group %d", phase.GroupID)
		}
		for _, f := range phase.Fields {
			if !validBody(f.Footprint) {
				return fmt.Errorf("navigation: invalid tier %+v", f.Footprint)
			}
			feet = append(feet, f.Footprint)
		}
	}
	for i := len(s.tiers) - 1; i >= 2; i-- {
		if !slices.Contains(feet, s.tiers[i].foot) {
			s.tiers = slices.Delete(s.tiers, i, i+1)
			for _, g := range s.groups {
				g.fields = slices.Delete(g.fields, i, i+1)
			}
		}
	}
	for id := range uint8(len(s.targets)) {
		if !slices.ContainsFunc(snap.Groups, func(phase navGroupPhase) bool { return phase.GroupID == id }) {
			delete(s.groups, id)
		}
	}
	for _, phase := range snap.Groups {
		for _, fp := range phase.Fields {
			i := s.keepTier(fp.Footprint)
			f, t := &s.getOrCreateGroup(phase.GroupID).fields[i], s.tiers[i]
			f.at = restoreField(f.cache, f.at, t.gen, &t.grid, fp.Ticks, fp.Pending, fp.Computed, fp.LastTargets)
		}
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
	s.showDebug()
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

// navState is a copy of the tiers without their grids, which a restore refills
type navState struct {
	groups            map[uint8]targetGroupNav
	tiers             []navTier
	walls             []bool
	wallsW, wallsH    int
	gen               uint64
	derived           bool
	targets           [component.MaxTargetGroups + parameter.MaxPlayers]engine.TargetGroupState
	routeRebuildTicks int
	refreshTicks      int
	graphs            map[uint32]*navigation.RouteGraph
}

// CopyState carries every field and route a copy would otherwise rebuild, and the
// phase each was rebuilt in, which LoadShared re-derives as a joiner would.
func (s *NavigationSystem) CopyState() any {
	c := navState{groups: make(map[uint8]targetGroupNav, len(s.groups)), walls: slices.Clone(s.walls),
		wallsW: s.wallsW, wallsH: s.wallsH, gen: s.gen, derived: s.derived, targets: s.targets, routeRebuildTicks: s.routeRebuildTicks,
		refreshTicks: s.refreshTicks, graphs: s.world.Resources.RouteGraph.Graphs()}
	for _, t := range s.tiers {
		c.tiers = append(c.tiers, t.copy())
	}
	for id, g := range s.groups {
		c.groups[id] = cloneGroup(g)
	}
	return c
}

func (s *NavigationSystem) RestoreState(v any) error {
	c := v.(navState)
	clear(s.groups)
	for id, g := range c.groups {
		n := cloneGroup(&g)
		s.groups[id] = &n
	}
	s.tiers = s.tiers[:0]
	for _, t := range c.tiers {
		n := t.copy()
		s.tiers = append(s.tiers, &n)
	}
	s.walls, s.wallsW, s.wallsH, s.gen, s.derived = slices.Clone(c.walls), c.wallsW, c.wallsH, c.gen, c.derived
	s.gridsAt, s.wallsSeq = 0, 0
	s.targets, s.routeRebuildTicks, s.refreshTicks = c.targets, c.routeRebuildTicks, c.refreshTicks
	s.world.Resources.RouteGraph.SetGraphs(c.graphs)
	s.showDebug()
	return nil
}

// copy is the tier sharing no storage with t, without its grid
func (t *navTier) copy() navTier {
	n := navTier{foot: t.foot, gen: t.gen}
	if t.pass != nil {
		n.pass = t.pass.Clone()
	}
	return n
}

// cloneGroup is a group's fields sharing no storage with g's
func cloneGroup(g *targetGroupNav) targetGroupNav {
	n := targetGroupNav{fields: slices.Clone(g.fields)}
	for i := range n.fields {
		n.fields[i].cache = n.fields[i].cache.Clone()
	}
	return n
}
