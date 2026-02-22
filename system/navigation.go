package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
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

		// Calculate bilinear interpolated flow direction
		navComp.FlowX, navComp.FlowY = s.getInterpolatedFlowDirection(preciseX, preciseY)
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