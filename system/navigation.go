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

// NavigationSystem calculates flow field and wall avoidance for kinetic entities
type NavigationSystem struct {
	world *engine.World

	flowCache *navigation.FlowFieldCache

	// Cached cursor position (updated via EventCursorMoved)
	cursorX, cursorY int
	cursorValid      bool

	statEntities   *atomic.Int64
	statRecomputes *atomic.Int64

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

	if s.flowCache.Update(s.cursorX, s.cursorY, isBlocked) {
		s.statRecomputes.Add(1)
	}

	// Process navigation entities
	entities := s.world.Components.Navigation.GetAllEntities()
	s.statEntities.Store(int64(len(entities)))

	for _, entity := range entities {
		navComp, ok := s.world.Components.Navigation.GetComponent(entity)
		if !ok {
			continue
		}

		// Use Kinetic precise position for smooth interpolation
		var preciseX, preciseY int64
		if kinetic, ok := s.world.Components.Kinetic.GetComponent(entity); ok {
			preciseX, preciseY = kinetic.PreciseX, kinetic.PreciseY
		} else if pos, ok := s.world.Positions.GetPosition(entity); ok {
			preciseX, preciseY = vmath.CenteredFromGrid(pos.X, pos.Y)
		} else {
			continue
		}

		// Calculate bilinear interpolated flow direction always in case LOS fails
		navComp.FlowX, navComp.FlowY = s.getInterpolatedFlowDirection(preciseX, preciseY)

		// LOS check with direct raycast to cursor, if clear use pure Euclidean homing
		gridX, gridY := vmath.GridFromCentered(preciseX, preciseY)
		if s.world.Positions.HasLineOfSight(gridX, gridY, s.cursorX, s.cursorY, component.WallBlockKinetic) {
			navComp.HasDirectPath = true
		} else {
			navComp.HasDirectPath = false
		}

		s.world.Components.Navigation.SetComponent(entity, navComp)
	}
}

// getInterpolatedFlowDirection performs bilinear interpolation masking out blocked cells
func (s *NavigationSystem) getInterpolatedFlowDirection(preciseX, preciseY int64) (int64, int64) {
	// Shift coordinates to align integer grid with cell centers
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