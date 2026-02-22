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
	// Precompute Q32.32 normalized vectors for flow directions
	for i, vec := range navigation.DirVectors {
		fx := vmath.FromInt(vec[0])
		fy := vmath.FromInt(vec[1])
		// Normalize diagonals
		if vec[0] != 0 && vec[1] != 0 {
			fx, fy = vmath.Normalize2D(fx, fy)
		}
		flowDirLUT[i] = [2]int64{fx, fy}
	}
}

// TODO: debug
var DebugFlow *navigation.FlowFieldCache
var DebugShowFlow bool

// NavigationSystem calculates flow field and wall avoidance for kinetic entities
type NavigationSystem struct {
	world *engine.World

	flowCache *navigation.FlowFieldCache

	// Cached cursor position (updated via EventCursorMoved)
	cursorX, cursorY int
	cursorValid      bool

	// Per-tick entity position buffer (reused to avoid allocations)
	entityPosBuf [][2]int

	statEntities   *atomic.Int64
	statRecomputes *atomic.Int64
	statROICells   *atomic.Int64 // Track ROI size for telemetry

	enabled bool
}

func NewNavigationSystem(world *engine.World) engine.System {
	config := world.Resources.Config
	s := &NavigationSystem{
		world: world,
		flowCache: navigation.NewFlowFieldCache(
			config.MapWidth,
			config.MapHeight,
			parameter.NavFlowMinTicksBetweenCompute,
			parameter.NavFlowDirtyDistance,
		),
	}

	s.statEntities = world.Resources.Status.Ints.Get("nav.entities")
	s.statRecomputes = world.Resources.Status.Ints.Get("nav.recomputes")
	s.statROICells = world.Resources.Status.Ints.Get("nav.roi_cells")

	s.Init()
	return s
}

func (s *NavigationSystem) Init() {
	s.enabled = true
	// Resize to current map dimensions at startup/new game
	config := s.world.Resources.Config
	if config.MapWidth > 0 && config.MapHeight > 0 {
		s.flowCache.Resize(config.MapWidth, config.MapHeight)
	}
	if s.flowCache != nil {
		s.flowCache.Field.Invalidate()
	}

	// Seed cursor position from world to prevent stale (0,0) default
	// At app start/new game: cursor created before NavigationSystem init
	if s.world.Resources.Player != nil {
		if pos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity); ok {
			s.cursorX = pos.X
			s.cursorY = pos.Y
			s.cursorValid = true
		}
	}

	// TODO: remove later
	DebugFlow = s.flowCache
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
			// Resize flow field on level change
			s.flowCache.Resize(payload.Width, payload.Height)
		}
	}
}

func (s *NavigationSystem) Update() {
	if !s.enabled {
		return
	}

	// Detect map dimension changes (terminal resize with CropOnResize, or other config mutations)
	config := s.world.Resources.Config
	if config.MapWidth != s.flowCache.Field.Width || config.MapHeight != s.flowCache.Field.Height {
		s.flowCache.Resize(config.MapWidth, config.MapHeight)
	}

	// Update flow field (with caching/throttling)
	isBlocked := func(x, y int) bool {
		return s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic)
	}

	// 1. Pre-filter entities and collect ROI contributors
	entities := s.world.Components.Navigation.GetAllEntities()
	s.statEntities.Store(int64(len(entities)))

	// Reset position buffer
	s.entityPosBuf = s.entityPosBuf[:0]

	for _, entity := range entities {
		navComp, ok := s.world.Components.Navigation.GetComponent(entity)
		if !ok {
			continue
		}

		// Get entity position
		var gridX, gridY int
		if kinetic, ok := s.world.Components.Kinetic.GetComponent(entity); ok {
			gridX, gridY = vmath.GridFromCentered(kinetic.PreciseX, kinetic.PreciseY)
		} else if pos, ok := s.world.Positions.GetPosition(entity); ok {
			gridX, gridY = pos.X, pos.Y
		} else {
			continue
		}

		// Area LOS check using entity dimensions
		width, height := navComp.Width, navComp.Height
		if width == 0 {
			width = 1
		}
		if height == 0 {
			height = 1
		}

		hasLOS := false
		if width == 1 && height == 1 {
			// Point entity: use fast point LOS
			hasLOS = s.world.Positions.HasLineOfSight(gridX, gridY, s.cursorX, s.cursorY, component.WallBlockKinetic)
		} else {
			// Area entity: use swept bbox LOS with rotation fallback
			hasLOS = s.world.Positions.HasAreaLineOfSightRotatable(gridX, gridY, s.cursorX, s.cursorY, width, height, component.WallBlockKinetic)
		}

		if hasLOS {
			navComp.HasDirectPath = true
			navComp.FlowX = 0
			navComp.FlowY = 0
			s.world.Components.Navigation.SetComponent(entity, navComp)
			continue
		}

		// No LOS - entity needs flow field, add to ROI
		navComp.HasDirectPath = false
		s.world.Components.Navigation.SetComponent(entity, navComp)
		s.entityPosBuf = append(s.entityPosBuf, [2]int{gridX, gridY})

		// Store entity for second pass (after flow field update)
		// We'll update flow direction below
	}

	// 2. Update flow field with ROI
	recomputed := s.flowCache.Update(s.cursorX, s.cursorY, isBlocked, s.entityPosBuf)
	if recomputed {
		s.statRecomputes.Add(1)
		if roi := s.flowCache.GetROI(); roi != nil {
			roiCells := (roi.MaxX - roi.MinX + 1) * (roi.MaxY - roi.MinY + 1)
			s.statROICells.Store(int64(roiCells))
		}
	}

	// 3. Update flow directions for entities without LOS
	for _, entity := range entities {
		navComp, ok := s.world.Components.Navigation.GetComponent(entity)
		if !ok || navComp.HasDirectPath {
			continue // Skip entities with direct path (already updated)
		}

		var preciseX, preciseY int64
		if kinetic, ok := s.world.Components.Kinetic.GetComponent(entity); ok {
			preciseX, preciseY = kinetic.PreciseX, kinetic.PreciseY
		} else if pos, ok := s.world.Positions.GetPosition(entity); ok {
			preciseX, preciseY = vmath.CenteredFromGrid(pos.X, pos.Y)
		} else {
			continue
		}

		// Composite footprint sampling with wall-constraint projection
		if navComp.Width > 1 || navComp.Height > 1 {
			gridX, gridY := vmath.GridFromCentered(preciseX, preciseY)
			navComp.FlowX, navComp.FlowY = s.getCompositeFlowDirection(entity, gridX, gridY)

			// Tabu suppression: prevent 2-cycle oscillation
			navComp.FlowX, navComp.FlowY = suppressTabuDirection(&navComp, gridX, gridY, navComp.FlowX, navComp.FlowY)

			// Record current position in ring buffer
			navComp.TabuPos[navComp.TabuHead] = [2]int{gridX, gridY}
			navComp.TabuHead = (navComp.TabuHead + 1) % 2
			if navComp.TabuTick < 2 {
				navComp.TabuTick++
			}
		} else {
			navComp.FlowX, navComp.FlowY = s.getInterpolatedFlowDirection(preciseX, preciseY)
		}
		s.world.Components.Navigation.SetComponent(entity, navComp)
	}
}

// getInterpolatedFlowDirection performs bilinear interpolation masking out blocked cells
func (s *NavigationSystem) getInterpolatedFlowDirection(preciseX, preciseY int64) (int64, int64) {
	sampleX := preciseX - vmath.CellCenter
	sampleY := preciseY - vmath.CellCenter

	x0 := vmath.ToInt(sampleX)
	y0 := vmath.ToInt(sampleY)

	// Fraction (u, v) in Q32.32 [0, Scale)
	u := sampleX & vmath.Mask
	v := sampleY & vmath.Mask

	// Inverted weights
	invU := vmath.Scale - u
	invV := vmath.Scale - v

	// Base Weights for 4 neighbors
	// TL(0,0), TR(1,0), BL(0,1), BR(1,1)
	w00 := vmath.Mul(invU, invV)
	w10 := vmath.Mul(u, invV)
	w01 := vmath.Mul(invU, v)
	w11 := vmath.Mul(u, v)

	// Get Vectors and Validity
	v00x, v00y, valid00 := s.getFlowVectorAndValidity(x0, y0)
	v10x, v10y, valid10 := s.getFlowVectorAndValidity(x0+1, y0)
	v01x, v01y, valid01 := s.getFlowVectorAndValidity(x0, y0+1)
	v11x, v11y, valid11 := s.getFlowVectorAndValidity(x0+1, y0+1)

	var sumX, sumY, totalWeight int64

	// Accumulate only valid vectors
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

	// If no valid neighbors (trapped in wall?) or weight 0, return 0
	if totalWeight == 0 {
		return 0, 0
	}

	// Renormalize result: divide by totalWeight
	resX := vmath.Div(sumX, totalWeight)
	resY := vmath.Div(sumY, totalWeight)

	// Final normalization to ensure unit vector consistency
	if resX != 0 || resY != 0 {
		return vmath.Normalize2D(resX, resY)
	}

	return 0, 0
}

// getFlowVectorAndValidity retrieves vector and validity flag
func (s *NavigationSystem) getFlowVectorAndValidity(x, y int) (int64, int64, bool) {
	dir := s.flowCache.GetDirection(x, y)
	if dir < 0 || dir >= navigation.DirCount {
		return 0, 0, false
	}
	return flowDirLUT[dir][0], flowDirLUT[dir][1], true
}

// compositeFootprint caches bounding box derived from HeaderComponent member layout
type compositeFootprint struct {
	entries                            []component.MemberEntry
	minOffX, maxOffX, minOffY, maxOffY int
	footW, footH                       int
}

func newCompositeFootprint(entries []component.MemberEntry) (compositeFootprint, bool) {
	if len(entries) == 0 {
		return compositeFootprint{}, false
	}

	fp := compositeFootprint{
		entries: entries,
		minOffX: entries[0].OffsetX, maxOffX: entries[0].OffsetX,
		minOffY: entries[0].OffsetY, maxOffY: entries[0].OffsetY,
	}

	for _, m := range entries[1:] {
		if m.OffsetX < fp.minOffX {
			fp.minOffX = m.OffsetX
		}
		if m.OffsetX > fp.maxOffX {
			fp.maxOffX = m.OffsetX
		}
		if m.OffsetY < fp.minOffY {
			fp.minOffY = m.OffsetY
		}
		if m.OffsetY > fp.maxOffY {
			fp.maxOffY = m.OffsetY
		}
	}

	fp.footW = fp.maxOffX - fp.minOffX + 1
	fp.footH = fp.maxOffY - fp.minOffY + 1
	return fp, true
}

// sampleFlowSum sums flow vectors at live footprint cells relative to (gridX, gridY)
// Lock-free: reads from flowCache only
func (s *NavigationSystem) sampleFlowSum(fp *compositeFootprint, gridX, gridY int) (int64, int64) {
	var sumX, sumY int64
	for _, m := range fp.entries {
		if m.Entity == 0 {
			continue
		}
		dir := s.flowCache.GetDirection(gridX+m.OffsetX, gridY+m.OffsetY)
		if dir < 0 || dir >= navigation.DirCount {
			continue
		}
		sumX += flowDirLUT[dir][0]
		sumY += flowDirLUT[dir][1]
	}
	return sumX, sumY
}

// projectFlowAgainstWalls zeroes axis components the composite cannot physically follow
// Caller MUST hold Position write lock
func (s *NavigationSystem) projectFlowAgainstWalls(
	sumX, sumY int64,
	gridX, gridY int,
	fp *compositeFootprint,
) (int64, int64) {
	topLeftX := gridX + fp.minOffX
	topLeftY := gridY + fp.minOffY
	mapW := s.world.Resources.Config.MapWidth
	mapH := s.world.Resources.Config.MapHeight
	mask := component.WallBlockKinetic

	if sumX > 0 {
		if topLeftX+fp.footW >= mapW ||
			s.world.Positions.HasBlockingWallInAreaUnsafe(topLeftX+1, topLeftY, fp.footW, fp.footH, mask) {
			sumX = 0
		}
	} else if sumX < 0 {
		if topLeftX <= 0 ||
			s.world.Positions.HasBlockingWallInAreaUnsafe(topLeftX-1, topLeftY, fp.footW, fp.footH, mask) {
			sumX = 0
		}
	}

	if sumY > 0 {
		if topLeftY+fp.footH >= mapH ||
			s.world.Positions.HasBlockingWallInAreaUnsafe(topLeftX, topLeftY+1, fp.footW, fp.footH, mask) {
			sumY = 0
		}
	} else if sumY < 0 {
		if topLeftY <= 0 ||
			s.world.Positions.HasBlockingWallInAreaUnsafe(topLeftX, topLeftY-1, fp.footW, fp.footH, mask) {
			sumY = 0
		}
	}

	return sumX, sumY
}

// canCompositeOccupy checks if composite footprint fits at (gridX, gridY) without wall/bounds collision
// Caller MUST hold Position write lock
func (s *NavigationSystem) canCompositeOccupy(gridX, gridY int, fp *compositeFootprint) bool {
	topLeftX := gridX + fp.minOffX
	topLeftY := gridY + fp.minOffY
	mapW := s.world.Resources.Config.MapWidth
	mapH := s.world.Resources.Config.MapHeight

	if topLeftX < 0 || topLeftY < 0 || topLeftX+fp.footW > mapW || topLeftY+fp.footH > mapH {
		return false
	}

	return !s.world.Positions.HasBlockingWallInAreaUnsafe(
		topLeftX, topLeftY, fp.footW, fp.footH, component.WallBlockKinetic,
	)
}

// getCompositeFlowDirection samples flow at all footprint cells with wall-constraint projection
// and escape probing for L-corner stuck states
func (s *NavigationSystem) getCompositeFlowDirection(headerEntity core.Entity, gridX, gridY int) (int64, int64) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok || len(headerComp.MemberEntries) == 0 {
		return 0, 0
	}

	fp, ok := newCompositeFootprint(headerComp.MemberEntries)
	if !ok {
		return 0, 0
	}

	// Phase 1: flow sum at current position (lock-free)
	rawX, rawY := s.sampleFlowSum(&fp, gridX, gridY)
	if rawX == 0 && rawY == 0 {
		return 0, 0
	}

	// Phase 2+3: wall projection and escape probing under single lock
	s.world.Positions.Lock()

	projX, projY := s.projectFlowAgainstWalls(rawX, rawY, gridX, gridY, &fp)
	if projX != 0 || projY != 0 {
		s.world.Positions.Unlock()
		return vmath.Normalize2D(projX, projY)
	}

	// Phase 3: escape probing — projection yielded (0,0) from non-zero raw sum
	// Probe opposite direction of each blocked axis
	// Order: larger-magnitude axis first (clearing dominant blocked direction is higher impact)
	type escapeProbe struct{ dx, dy int }
	var probes [2]escapeProbe
	nProbes := 0

	absRawX, absRawY := rawX, rawY
	if absRawX < 0 {
		absRawX = -absRawX
	}
	if absRawY < 0 {
		absRawY = -absRawY
	}

	// Build ordered probe list
	addProbeX := func() {
		if rawX > 0 {
			probes[nProbes] = escapeProbe{-1, 0}
		} else if rawX < 0 {
			probes[nProbes] = escapeProbe{1, 0}
		} else {
			return
		}
		nProbes++
	}
	addProbeY := func() {
		if rawY > 0 {
			probes[nProbes] = escapeProbe{0, -1}
		} else if rawY < 0 {
			probes[nProbes] = escapeProbe{0, 1}
		} else {
			return
		}
		nProbes++
	}

	if absRawX >= absRawY {
		addProbeX()
		addProbeY()
	} else {
		addProbeY()
		addProbeX()
	}

	for i := 0; i < nProbes; i++ {
		probeGridX := gridX + probes[i].dx
		probeGridY := gridY + probes[i].dy

		if !s.canCompositeOccupy(probeGridX, probeGridY, &fp) {
			continue
		}

		// Re-sample and re-project at probed position
		probeSumX, probeSumY := s.sampleFlowSum(&fp, probeGridX, probeGridY)
		if probeSumX == 0 && probeSumY == 0 {
			continue
		}

		probeProjX, probeProjY := s.projectFlowAgainstWalls(
			probeSumX, probeSumY, probeGridX, probeGridY, &fp,
		)
		if probeProjX != 0 || probeProjY != 0 {
			// Viable escape — return cardinal direction toward probe position
			s.world.Positions.Unlock()
			return vmath.Normalize2D(vmath.FromInt(probes[i].dx), vmath.FromInt(probes[i].dy))
		}
	}

	s.world.Positions.Unlock()
	return 0, 0
}

// suppressTabuDirection checks if flow would return composite to blacklisted position
// Returns adjusted (flowX, flowY), zeroing the axis that causes regression
func suppressTabuDirection(navComp *component.NavigationComponent, gridX, gridY int, flowX, flowY int64) (int64, int64) {
	if navComp.TabuTick < 2 {
		return flowX, flowY // Buffer not full
	}

	// Older entry is opposite of current write head
	tabuIdx := (navComp.TabuHead + 1) % 2
	tabuX := navComp.TabuPos[tabuIdx][0]
	tabuY := navComp.TabuPos[tabuIdx][1]

	// Predict next grid position from flow direction
	nextX, nextY := gridX, gridY
	if flowX > 0 {
		nextX++
	} else if flowX < 0 {
		nextX--
	}
	if flowY > 0 {
		nextY++
	} else if flowY < 0 {
		nextY--
	}

	if nextX != tabuX || nextY != tabuY {
		return flowX, flowY // Not regressing
	}

	// Suppress axis that causes regression
	if gridX != tabuX {
		flowX = 0
	}
	if gridY != tabuY {
		flowY = 0
	}

	return flowX, flowY
}