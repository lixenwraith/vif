package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// WallSystem manages wall lifecycle, spawning, and entity displacement
type WallSystem struct {
	world *engine.World

	// Push-out tracking - positions needing entity check this tick
	pendingPushChecks []core.Point

	// Configuration
	pushCheckEveryTick bool // When true, runs full push check in Update()

	// Maze generation (dev/testing)
	mazeGenerated bool

	// Metrics
	statEnabled    *atomic.Bool
	statWallCount  *atomic.Int64
	statPushEvents *atomic.Int64

	enabled bool
}

// NewWallSystem creates a new wall system
func NewWallSystem(world *engine.World) engine.System {
	s := &WallSystem{
		world: world,
	}

	s.statEnabled = world.Resources.Status.Bools.Get("wall.enabled")
	s.statWallCount = world.Resources.Status.Ints.Get("wall.count")
	s.statPushEvents = world.Resources.Status.Ints.Get("wall.push_events")

	s.Init()
	return s
}

func (s *WallSystem) Init() {
	s.pendingPushChecks = make([]core.Point, 0, 64)
	s.pushCheckEveryTick = false
	s.mazeGenerated = false
	s.statEnabled.Store(true)
	s.statWallCount.Store(0)
	s.statPushEvents.Store(0)
	s.enabled = true
}

func (s *WallSystem) Name() string {
	return "wall"
}

func (s *WallSystem) Priority() int {
	return parameter.PriorityWall
}

func (s *WallSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventWallSpawnRequest,
		event.EventWallCompositeSpawnRequest,
		event.EventWallDespawnRequest,
		event.EventWallMaskChangeRequest,
		event.EventWallPushCheckRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *WallSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
				s.statEnabled.Store(payload.Enabled)
			}
		}
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventWallSpawnRequest:
		if payload, ok := ev.Payload.(*event.WallSpawnRequestPayload); ok {
			s.handleSpawnSingle(payload)
		}

	case event.EventWallCompositeSpawnRequest:
		if payload, ok := ev.Payload.(*event.WallCompositeSpawnRequestPayload); ok {
			s.handleSpawnComposite(payload)
		}

	case event.EventWallDespawnRequest:
		if payload, ok := ev.Payload.(*event.WallDespawnRequestPayload); ok {
			s.handleDespawn(payload)
		}

	case event.EventWallMaskChangeRequest:
		if payload, ok := ev.Payload.(*event.WallMaskChangeRequestPayload); ok {
			s.handleMaskChange(payload)
		}

	case event.EventWallPushCheckRequest:
		s.runFullPushCheck()
	}
}

func (s *WallSystem) Update() {
	if !s.enabled {
		return
	}

	// One-time maze generation for testing
	if !s.mazeGenerated {
		s.generateMaze()
		s.mazeGenerated = true
	}

	// Process pending checks from this tick's spawns
	s.processPendingPushChecks()

	// Optional full check (toggled for performance testing or special game states)
	if s.pushCheckEveryTick {
		s.runFullPushCheck()
	}

	s.statWallCount.Store(int64(s.world.Components.Wall.CountEntities()))
}

// handleSpawnSingle creates a single wall entity
func (s *WallSystem) handleSpawnSingle(payload *event.WallSpawnRequestPayload) {
	config := s.world.Resources.Config
	if payload.X < 0 || payload.X >= config.GameWidth ||
		payload.Y < 0 || payload.Y >= config.GameHeight {
		return
	}

	if s.world.Positions.HasAnyWallAt(payload.X, payload.Y) {
		return
	}

	entity := s.world.CreateEntity()
	s.world.Positions.SetPosition(entity, component.PositionComponent{
		X: payload.X,
		Y: payload.Y,
	})

	s.world.Components.Wall.SetComponent(entity, component.WallComponent{
		BlockMask: payload.BlockMask,
		Char:      payload.Char,
		FgColor:   payload.FgColor,
		BgColor:   payload.BgColor,
		RenderFg:  payload.RenderFg,
		RenderBg:  payload.RenderBg,
	})

	if payload.BlockMask.IsBlocking() {
		s.pendingPushChecks = append(s.pendingPushChecks, core.Point{X: payload.X, Y: payload.Y})
	}

	s.world.PushEvent(event.EventWallSpawned, &event.WallSpawnedPayload{
		X: payload.X, Y: payload.Y, Width: 1, Height: 1, Count: 1,
	})
}

// handleSpawnComposite creates a multi-cell wall using Header/Member pattern
func (s *WallSystem) handleSpawnComposite(payload *event.WallCompositeSpawnRequestPayload) {
	if len(payload.Cells) == 0 {
		return
	}

	config := s.world.Resources.Config

	// Create phantom head
	headerEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{
		X: payload.X,
		Y: payload.Y,
	})
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	members := make([]component.MemberEntry, 0, len(payload.Cells))
	count := 0
	minX, minY := config.GameWidth, config.GameHeight
	maxX, maxY := 0, 0

	for _, cell := range payload.Cells {
		x := payload.X + cell.OffsetX
		y := payload.Y + cell.OffsetY

		if x < 0 || x >= config.GameWidth || y < 0 || y >= config.GameHeight {
			continue
		}

		if s.world.Positions.HasAnyWallAt(x, y) {
			continue
		}

		entity := s.world.CreateEntity()
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

		s.world.Components.Wall.SetComponent(entity, component.WallComponent{
			BlockMask: payload.BlockMask,
			Char:      cell.Char,
			FgColor:   cell.FgColor,
			BgColor:   cell.BgColor,
			RenderFg:  cell.RenderFg,
			RenderBg:  cell.RenderBg,
		})

		s.world.Components.Member.SetComponent(entity, component.MemberComponent{
			HeaderEntity: headerEntity,
		})

		members = append(members, component.MemberEntry{
			Entity:  entity,
			OffsetX: cell.OffsetX,
			OffsetY: cell.OffsetY,
			Layer:   component.LayerGlyph,
		})

		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}

		if payload.BlockMask.IsBlocking() {
			s.pendingPushChecks = append(s.pendingPushChecks, core.Point{X: x, Y: y})
		}

		count++
	}

	if count == 0 {
		s.world.DestroyEntity(headerEntity)
		return
	}

	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorNone,
		MemberEntries: members,
	})

	s.world.PushEvent(event.EventWallSpawned, &event.WallSpawnedPayload{
		X: minX, Y: minY,
		Width: maxX - minX + 1, Height: maxY - minY + 1,
		Count:        count,
		HeaderEntity: headerEntity,
	})
}

// handleDespawn removes walls in specified area
func (s *WallSystem) handleDespawn(payload *event.WallDespawnRequestPayload) {
	if payload.All {
		wallEntities := s.world.Components.Wall.GetAllEntities()
		for _, entity := range wallEntities {
			s.world.DestroyEntity(entity)
		}
		return
	}

	width := max(1, payload.Width)
	height := max(1, payload.Height)

	wallEntities := s.world.Components.Wall.GetAllEntities()
	for _, entity := range wallEntities {
		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		if pos.X >= payload.X && pos.X < payload.X+width &&
			pos.Y >= payload.Y && pos.Y < payload.Y+height {
			s.world.DestroyEntity(entity)
		}
	}
}

// handleMaskChange modifies blocking behavior of existing walls
func (s *WallSystem) handleMaskChange(payload *event.WallMaskChangeRequestPayload) {
	width := max(1, payload.Width)
	height := max(1, payload.Height)

	wallEntities := s.world.Components.Wall.GetAllEntities()
	for _, entity := range wallEntities {
		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		if pos.X < payload.X || pos.X >= payload.X+width ||
			pos.Y < payload.Y || pos.Y >= payload.Y+height {
			continue
		}

		wall, ok := s.world.Components.Wall.GetComponent(entity)
		if !ok {
			continue
		}

		wasBlocking := wall.BlockMask.IsBlocking()
		wall.BlockMask = payload.BlockMask
		s.world.Components.Wall.SetComponent(entity, wall)

		if !wasBlocking && payload.BlockMask.IsBlocking() {
			s.pendingPushChecks = append(s.pendingPushChecks, core.Point{X: pos.X, Y: pos.Y})
		}
	}
}

// processPendingPushChecks displaces entities from newly blocking positions
func (s *WallSystem) processPendingPushChecks() {
	if len(s.pendingPushChecks) == 0 {
		return
	}

	var pushCount int64

	for _, pt := range s.pendingPushChecks {
		pushCount += s.pushEntitiesAtPosition(pt.X, pt.Y)
	}

	s.statPushEvents.Add(pushCount)
	s.pendingPushChecks = s.pendingPushChecks[:0]
}

// runFullPushCheck iterates all blocking walls and pushes out any entities
func (s *WallSystem) runFullPushCheck() {
	var pushCount int64

	wallEntities := s.world.Components.Wall.GetAllEntities()
	for _, wallEntity := range wallEntities {
		wall, ok := s.world.Components.Wall.GetComponent(wallEntity)
		if !ok || !wall.BlockMask.IsBlocking() {
			continue
		}

		pos, ok := s.world.Positions.GetPosition(wallEntity)
		if !ok {
			continue
		}

		pushCount += s.pushEntitiesAtPosition(pos.X, pos.Y)
	}

	s.statPushEvents.Add(pushCount)
}

// pushEntitiesAtPosition displaces all non-wall entities at given position
func (s *WallSystem) pushEntitiesAtPosition(x, y int) int64 {
	var pushCount int64
	cursorEntity := s.world.Resources.Player.Entity

	// Check cursor
	if cursorPos, ok := s.world.Positions.GetPosition(cursorEntity); ok {
		if cursorPos.X == x && cursorPos.Y == y {
			if _, _, moved := s.world.PushEntityFromBlocked(cursorEntity, component.WallBlockCursor); moved {
				pushCount++
			}
		}
	}

	// Check other entities
	entities := s.world.Positions.GetAllEntityAt(x, y)
	for _, entity := range entities {
		if entity == cursorEntity {
			continue
		}
		if s.world.Components.Wall.HasEntity(entity) {
			continue
		}

		mask := s.getMaskForEntity(entity)
		if mask == component.WallBlockNone {
			continue
		}

		if _, _, moved := s.world.PushEntityFromBlocked(entity, mask); moved {
			pushCount++
		}
	}

	return pushCount
}

// getMaskForEntity returns appropriate wall block mask for entity type
func (s *WallSystem) getMaskForEntity(entity core.Entity) component.WallBlockMask {
	if s.world.Components.Kinetic.HasEntity(entity) {
		return component.WallBlockKinetic
	}
	if s.world.Components.Decay.HasEntity(entity) || s.world.Components.Blossom.HasEntity(entity) {
		return component.WallBlockParticle
	}
	return component.WallBlockSpawn
}

// SetPushCheckEveryTick enables or disables per-tick full push check
func (s *WallSystem) SetPushCheckEveryTick(enabled bool) {
	s.pushCheckEveryTick = enabled
}

// --- TEST ---

// generateMaze creates a test maze using recursive backtracking
// Fills entire game area with ~10-cell corridors, leaves center clear for cursor spawn
func (s *WallSystem) generateMaze() {
	config := s.world.Resources.Config
	gameW := config.GameWidth
	gameH := config.GameHeight

	const cellSize = 10 // 9 corridor + 1 wall

	mazeW := gameW / cellSize
	mazeH := gameH / cellSize

	if mazeW < 3 || mazeH < 3 {
		return
	}

	// Wall arrays: hWalls[y][x] = top wall of cell, vWalls[y][x] = left wall of cell
	hWalls := make([][]bool, mazeH)
	vWalls := make([][]bool, mazeH)
	visited := make([][]bool, mazeH)
	for my := 0; my < mazeH; my++ {
		hWalls[my] = make([]bool, mazeW)
		vWalls[my] = make([]bool, mazeW)
		visited[my] = make([]bool, mazeW)
		for mx := 0; mx < mazeW; mx++ {
			hWalls[my][mx] = true
			vWalls[my][mx] = true
		}
	}

	// Recursive backtracking from center
	rng := vmath.NewFastRand(uint64(time.Now().UnixNano()))
	startMX, startMY := mazeW/2, mazeH/2

	var stack [][2]int
	visited[startMY][startMX] = true
	stack = append(stack, [2]int{startMX, startMY})

	for len(stack) > 0 {
		mx, my := stack[len(stack)-1][0], stack[len(stack)-1][1]

		// Collect unvisited neighbors
		type dir struct {
			nx, ny           int
			removeH, removeV bool
			hy, hx, vy, vx   int
		}
		var neighbors []dir

		if my > 0 && !visited[my-1][mx] { // North
			neighbors = append(neighbors, dir{mx, my - 1, true, false, my, mx, 0, 0})
		}
		if mx < mazeW-1 && !visited[my][mx+1] { // East
			neighbors = append(neighbors, dir{mx + 1, my, false, true, 0, 0, my, mx + 1})
		}
		if my < mazeH-1 && !visited[my+1][mx] { // South
			neighbors = append(neighbors, dir{mx, my + 1, true, false, my + 1, mx, 0, 0})
		}
		if mx > 0 && !visited[my][mx-1] { // West
			neighbors = append(neighbors, dir{mx - 1, my, false, true, 0, 0, my, mx})
		}

		if len(neighbors) == 0 {
			stack = stack[:len(stack)-1]
			continue
		}

		n := neighbors[rng.Intn(len(neighbors))]
		if n.removeH {
			hWalls[n.hy][n.hx] = false
		}
		if n.removeV {
			vWalls[n.vy][n.vx] = false
		}

		visited[n.ny][n.nx] = true
		stack = append(stack, [2]int{n.nx, n.ny})
	}

	// Clear spawn area: 3x3 cells around center (+1 for boundary walls)
	centerMX, centerMY := mazeW/2, mazeH/2
	for dy := -1; dy <= 2; dy++ {
		for dx := -1; dx <= 2; dx++ {
			mx, my := centerMX+dx, centerMY+dy
			if mx >= 0 && mx < mazeW && my >= 0 && my < mazeH {
				hWalls[my][mx] = false
				vWalls[my][mx] = false
			}
		}
	}

	// Render maze walls
	for my := 0; my < mazeH; my++ {
		for mx := 0; mx < mazeW; mx++ {
			baseX := mx * cellSize
			baseY := my * cellSize

			if hWalls[my][mx] {
				for wx := 0; wx < cellSize && baseX+wx < gameW; wx++ {
					s.spawnMazeWall(baseX+wx, baseY)
				}
			}

			if vWalls[my][mx] {
				for wy := 0; wy < cellSize && baseY+wy < gameH; wy++ {
					s.spawnMazeWall(baseX, baseY+wy)
				}
			}
		}
	}

	// Right boundary
	rightX := mazeW * cellSize
	if rightX < gameW {
		for y := 0; y < gameH; y++ {
			s.spawnMazeWall(rightX, y)
		}
	}

	// Bottom boundary
	bottomY := mazeH * cellSize
	if bottomY < gameH {
		for x := 0; x < gameW; x++ {
			s.spawnMazeWall(x, bottomY)
		}
	}
}

// spawnMazeWall creates a single maze wall cell synchronously
func (s *WallSystem) spawnMazeWall(x, y int) {
	s.handleSpawnSingle(&event.WallSpawnRequestPayload{
		X:         x,
		Y:         y,
		BlockMask: component.WallBlockAll,
		Char:      '█',
		FgColor:   visual.RgbWallDefault,
		BgColor:   visual.RgbWallDefault,
		RenderFg:  true,
		RenderBg:  true,
	})
}