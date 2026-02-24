package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/maze"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/pattern"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// WallSystem manages wall lifecycle, spawning, and entity displacement
type WallSystem struct {
	world *engine.World

	// Push-out tracking - positions needing entity check this tick
	pendingPushChecks []core.Point

	// Configuration
	pushCheckEveryTick bool // When true, runs full push check in Update()

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
		event.EventWallBatchSpawnRequest,
		event.EventWallCompositeSpawnRequest,
		event.EventWallPatternSpawnRequest,
		event.EventWallDespawnRequest,
		event.EventWallMaskChangeRequest,
		event.EventWallPushCheckRequest,
		event.EventMazeSpawnRequest,
		event.EventWallDespawnAll,
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

	case event.EventWallBatchSpawnRequest:
		if payload, ok := ev.Payload.(*event.WallBatchSpawnRequestPayload); ok {
			s.executeBatchSpawn(payload)
			event.ReleaseWallBatchRequest(payload)
		}

	case event.EventWallCompositeSpawnRequest:
		if payload, ok := ev.Payload.(*event.WallCompositeSpawnRequestPayload); ok {
			s.handleSpawnComposite(payload)
		}

	case event.EventWallPatternSpawnRequest:
		if payload, ok := ev.Payload.(*event.WallPatternSpawnRequestPayload); ok {
			s.handlePatternSpawn(payload)
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

	case event.EventMazeSpawnRequest:
		if payload, ok := ev.Payload.(*event.MazeSpawnRequestPayload); ok {
			s.handleMazeSpawn(payload)
		}

	case event.EventWallDespawnAll:
		s.handleDespawnAllSilent()
	}
}

func (s *WallSystem) Update() {
	if !s.enabled {
		return
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
	if payload.X < 0 || payload.X >= config.MapWidth ||
		payload.Y < 0 || payload.Y >= config.MapHeight {
		return
	}

	isBlocked := s.world.Positions.IsBlocked(payload.X, payload.Y, component.WallBlockAll)

	if isBlocked {
		switch payload.CollisionMode {
		case event.WallBatchOverwrite:
			var entityBuf [parameter.MaxEntitiesPerCell]core.Entity
			var toDestroy []core.Entity
			s.world.Positions.Lock()
			n := s.world.Positions.GetAllAtIntoUnsafe(payload.X, payload.Y, entityBuf[:])
			s.world.Positions.Unlock()
			for j := 0; j < n; j++ {
				if s.world.Components.Wall.HasEntity(entityBuf[j]) {
					toDestroy = append(toDestroy, entityBuf[j])
				}
			}
			if len(toDestroy) > 0 {
				s.world.DestroyEntitiesBatch(toDestroy)
			}
		default:
			// SkipBlocked (0) and FailIfBlocked both skip for single spawn
			return
		}
	}

	entity := s.world.CreateEntity()
	s.world.Positions.SetPosition(entity, component.PositionComponent{
		X: payload.X,
		Y: payload.Y,
	})

	s.world.Components.Wall.SetComponent(entity, component.WallComponent{
		BlockMask: payload.BlockMask,
		Rune:      payload.Char,
		FgColor:   payload.FgColor,
		BgColor:   payload.BgColor,
		RenderFg:  payload.RenderFg,
		RenderBg:  payload.RenderBg,
	})

	// Compute box char if box-drawing enabled
	if payload.BoxStyle != component.BoxDrawNone {
		wallComp := component.WallComponent{
			BlockMask: payload.BlockMask,
			Rune:      s.computeBoxChar(payload.X, payload.Y, payload.BoxStyle),
			FgColor:   payload.FgColor,
			BgColor:   payload.BgColor,
			RenderFg:  payload.RenderFg,
			RenderBg:  payload.RenderBg,
			BoxStyle:  payload.BoxStyle,
		}
		s.world.Components.Wall.SetComponent(entity, wallComp)
		s.invalidateBoxNeighbors(payload.X, payload.Y)
	}

	if payload.BlockMask != component.WallBlockNone {
		s.pendingPushChecks = append(s.pendingPushChecks, core.Point{X: payload.X, Y: payload.Y})
	}

	s.world.PushEvent(event.EventWallSpawned, &event.WallSpawnedPayload{
		X: payload.X, Y: payload.Y, Width: 1, Height: 1, Count: 1,
	})
}

// batchSpawnResult holds outcome of executeBatchSpawn
type batchSpawnResult struct {
	count        int
	headerEntity core.Entity
	minX, minY   int
	maxX, maxY   int
}

// executeBatchSpawn is the unified wall creation path for all batch operations
// Handles collision modes, composite structure, position batching, box chars, and push checks
// Emits EventWallSpawned on success
func (s *WallSystem) executeBatchSpawn(payload *event.WallBatchSpawnRequestPayload) batchSpawnResult {
	result := batchSpawnResult{}

	if len(payload.Cells) == 0 {
		return result
	}

	config := s.world.Resources.Config

	// 1. Resolve absolute positions, filter OOB
	type resolvedCell struct {
		x, y int
		idx  int // Index into payload.Cells
	}
	resolved := make([]resolvedCell, 0, len(payload.Cells))

	for i, cell := range payload.Cells {
		x := payload.X + cell.OffsetX
		y := payload.Y + cell.OffsetY
		if x < 0 || x >= config.MapWidth || y < 0 || y >= config.MapHeight {
			continue
		}
		resolved = append(resolved, resolvedCell{x: x, y: y, idx: i})
	}

	if len(resolved) == 0 {
		return result
	}

	// 2. Collision handling based on mode
	switch payload.CollisionMode {
	case event.WallBatchSkipBlocked:
		points := make([]core.Point, len(resolved))
		for i, rc := range resolved {
			points[i] = core.Point{X: rc.x, Y: rc.y}
		}
		blocked := s.world.Positions.CheckBlockedBatch(points, component.WallBlockAll)
		filtered := resolved[:0]
		for i, rc := range resolved {
			if !blocked[i] {
				filtered = append(filtered, rc)
			}
		}
		resolved = filtered

	case event.WallBatchOverwrite:
		var toDestroy []core.Entity
		var entityBuf [parameter.MaxEntitiesPerCell]core.Entity

		s.world.Positions.Lock()
		for _, rc := range resolved {
			n := s.world.Positions.GetAllAtIntoUnsafe(rc.x, rc.y, entityBuf[:])
			for j := 0; j < n; j++ {
				if s.world.Components.Wall.HasEntity(entityBuf[j]) {
					toDestroy = append(toDestroy, entityBuf[j])
				}
			}
		}
		s.world.Positions.Unlock()

		if len(toDestroy) > 0 {
			s.world.DestroyEntitiesBatch(toDestroy)
		}

	case event.WallBatchFailIfBlocked:
		points := make([]core.Point, len(resolved))
		for i, rc := range resolved {
			points[i] = core.Point{X: rc.x, Y: rc.y}
		}
		if s.world.Positions.IsAnyBlockedInSet(points, component.WallBlockAll) {
			return result
		}
	}

	if len(resolved) == 0 {
		return result
	}

	// 3. Create header if composite
	var headerEntity core.Entity
	if payload.Composite {
		headerEntity = s.world.CreateEntity()
		s.world.Positions.SetPosition(headerEntity, component.PositionComponent{
			X: payload.X, Y: payload.Y,
		})
		s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
			Mask: component.ProtectAll ^ component.ProtectFromDeath,
		})
	}

	// 4. Create entities, set components, batch positions
	members := make([]component.MemberEntry, 0, len(resolved))
	posBatch := s.world.Positions.BeginBatch()

	result.minX, result.minY = config.MapWidth, config.MapHeight
	result.maxX, result.maxY = 0, 0

	for _, rc := range resolved {
		cell := payload.Cells[rc.idx]
		entity := s.world.CreateEntity()

		s.world.Components.Wall.SetComponent(entity, component.WallComponent{
			BlockMask: payload.BlockMask,
			Rune:      cell.Char,
			FgColor:   cell.FgColor,
			BgColor:   cell.BgColor,
			RenderFg:  cell.RenderFg,
			RenderBg:  cell.RenderBg,
			BoxStyle:  payload.BoxStyle,
			Attrs:     cell.Attrs,
		})

		if payload.Composite {
			s.world.Components.Member.SetComponent(entity, component.MemberComponent{
				HeaderEntity: headerEntity,
			})
			s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
				Mask: component.ProtectAll ^ component.ProtectFromDeath,
			})
			members = append(members, component.MemberEntry{
				Entity:  entity,
				OffsetX: cell.OffsetX,
				OffsetY: cell.OffsetY,
			})
		}

		posBatch.Add(entity, component.PositionComponent{X: rc.x, Y: rc.y})

		if rc.x < result.minX {
			result.minX = rc.x
		}
		if rc.x > result.maxX {
			result.maxX = rc.x
		}
		if rc.y < result.minY {
			result.minY = rc.y
		}
		if rc.y > result.maxY {
			result.maxY = rc.y
		}

		if payload.BlockMask != component.WallBlockNone {
			s.pendingPushChecks = append(s.pendingPushChecks, core.Point{X: rc.x, Y: rc.y})
		}

		result.count++
	}

	// 5. Single-lock position commit
	posBatch.CommitForce()

	// 6. Finalize composite header or cleanup
	if payload.Composite {
		if result.count > 0 {
			s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
				Behavior:      component.BehaviorNone,
				MemberEntries: members,
			})
			result.headerEntity = headerEntity
		} else {
			s.world.DestroyEntity(headerEntity)
		}
	}

	// 7. Box char computation
	if payload.BoxStyle != component.BoxDrawNone && result.count > 0 {
		s.computeBoxCharsInArea(
			result.minX, result.minY,
			result.maxX-result.minX+1, result.maxY-result.minY+1,
			payload.BoxStyle,
		)
	}

	// 8. Emit notification
	if result.count > 0 {
		s.world.PushEvent(event.EventWallSpawned, &event.WallSpawnedPayload{
			X: result.minX, Y: result.minY,
			Width: result.maxX - result.minX + 1, Height: result.maxY - result.minY + 1,
			Count:        result.count,
			HeaderEntity: result.headerEntity,
		})
	}

	return result
}

// handleSpawnComposite creates a multi-cell wall using Header/Member pattern
// Delegates to executeBatchSpawn with composite mode
func (s *WallSystem) handleSpawnComposite(payload *event.WallCompositeSpawnRequestPayload) {
	if len(payload.Cells) == 0 {
		return
	}

	s.executeBatchSpawn(&event.WallBatchSpawnRequestPayload{
		X:             payload.X,
		Y:             payload.Y,
		BlockMask:     payload.BlockMask,
		BoxStyle:      payload.BoxStyle,
		CollisionMode: payload.CollisionMode,
		Composite:     true,
		Cells:         payload.Cells,
	})
}

// handlePatternSpawn loads .vfimg pattern and spawns as composite wall
// Converts pattern cells to WallCellDef and delegates to executeBatchSpawn
func (s *WallSystem) handlePatternSpawn(payload *event.WallPatternSpawnRequestPayload) {
	colorMode := s.world.Resources.Config.ColorMode

	patternResult, err := pattern.LoadDualModePattern(payload.Path, colorMode)
	if err != nil {
		s.world.DebugPrint("pattern load failed: " + err.Error())
		return
	}

	if patternResult.Empty() {
		return
	}

	cells := make([]component.WallCellDef, len(patternResult.Cells))
	for i, c := range patternResult.Cells {
		cells[i] = component.WallCellDef{
			OffsetX: c.OffsetX,
			OffsetY: c.OffsetY,
			WallVisualConfig: component.WallVisualConfig{
				Char:     c.Rune,
				FgColor:  c.Fg,
				BgColor:  c.Bg,
				RenderFg: c.RenderFg,
				RenderBg: c.RenderBg,
			},
			Attrs: c.Attrs,
		}
	}

	s.executeBatchSpawn(&event.WallBatchSpawnRequestPayload{
		X:             payload.X,
		Y:             payload.Y,
		BlockMask:     payload.BlockMask,
		CollisionMode: payload.CollisionMode,
		Composite:     true,
		Cells:         cells,
	})
}

// handleDespawn removes walls in specified area
func (s *WallSystem) handleDespawn(payload *event.WallDespawnRequestPayload) {
	if payload.All {
		s.despawnAllWalls()
		return
	}

	width := max(1, payload.Width)
	height := max(1, payload.Height)

	var flashTargets []core.Entity
	var fadeoutTargets []core.Entity
	var silentTargets []core.Entity

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

		s.classifyWallForDespawn(entity, wall, &flashTargets, &fadeoutTargets, &silentTargets)
	}

	count := len(flashTargets) + len(fadeoutTargets) + len(silentTargets)

	if len(flashTargets) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, flashTargets)
	}
	if len(fadeoutTargets) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFadeoutSpawnOne, fadeoutTargets)
	}
	if len(silentTargets) > 0 {
		s.world.DestroyEntitiesBatch(silentTargets)
	}

	if count > 0 {
		s.world.PushEvent(event.EventWallDespawned, &event.WallDespawnedPayload{
			X: payload.X, Y: payload.Y,
			Width: width, Height: height,
			Count: count,
		})
	}
}

// despawnAllWalls handles All=true despawn with proper effects
func (s *WallSystem) despawnAllWalls() {
	var flashTargets []core.Entity
	var fadeoutTargets []core.Entity
	var silentTargets []core.Entity

	wallEntities := s.world.Components.Wall.GetAllEntities()
	for _, entity := range wallEntities {
		wall, ok := s.world.Components.Wall.GetComponent(entity)
		if !ok {
			silentTargets = append(silentTargets, entity)
			continue
		}

		s.classifyWallForDespawn(entity, wall, &flashTargets, &fadeoutTargets, &silentTargets)
	}

	count := len(flashTargets) + len(fadeoutTargets) + len(silentTargets)

	if len(flashTargets) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, flashTargets)
	}
	if len(fadeoutTargets) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFadeoutSpawnOne, fadeoutTargets)
	}
	if len(silentTargets) > 0 {
		s.world.DestroyEntitiesBatch(silentTargets)
	}

	if count > 0 {
		config := s.world.Resources.Config
		s.world.PushEvent(event.EventWallDespawned, &event.WallDespawnedPayload{
			X: 0, Y: 0,
			Width: config.MapWidth, Height: config.MapHeight,
			Count: count,
		})
	}
}

// classifyWallForDespawn routes wall entity to appropriate effect category
func (s *WallSystem) classifyWallForDespawn(
	entity core.Entity,
	wall component.WallComponent,
	flashTargets *[]core.Entity,
	fadeoutTargets *[]core.Entity,
	silentTargets *[]core.Entity,
) {
	hasFg := wall.RenderFg && wall.Rune != 0
	hasBg := wall.RenderBg

	if hasFg && !hasBg {
		// Fg-only: flash effect via death system
		*flashTargets = append(*flashTargets, entity)
	} else if hasBg {
		// Bg or Fg+Bg: fadeout effect via death system
		*fadeoutTargets = append(*fadeoutTargets, entity)
	} else {
		// No visual: silent destruction
		*silentTargets = append(*silentTargets, entity)
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

		wasBlocking := wall.BlockMask != component.WallBlockNone
		wall.BlockMask = payload.BlockMask
		s.world.Components.Wall.SetComponent(entity, wall)

		if !wasBlocking && payload.BlockMask != component.WallBlockNone {
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
		wallComp, ok := s.world.Components.Wall.GetComponent(wallEntity)
		if !ok || wallComp.BlockMask == component.WallBlockNone {
			continue
		}

		wallPos, ok := s.world.Positions.GetPosition(wallEntity)
		if !ok {
			continue
		}

		pushCount += s.pushEntitiesAtPosition(wallPos.X, wallPos.Y)
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

		// Push failed - entity is stuck
		// Destroy non-cursor-owned combat entities that cannot escape
		if combat, ok := s.world.Components.Combat.GetComponent(entity); ok {
			if combat.OwnerEntity != cursorEntity {
				event.EmitDeathOne(s.world.Resources.Event.Queue, entity, 0)
			}
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

// handleMazeSpawn generates maze and spawns wall blocks via batch
func (s *WallSystem) handleMazeSpawn(payload *event.MazeSpawnRequestPayload) {
	config := s.world.Resources.Config

	mazeWidth := config.MapWidth / payload.CellWidth
	mazeHeight := config.MapHeight / payload.CellHeight

	if mazeWidth < 3 || mazeHeight < 3 {
		return
	}

	// Resolve visual config (apply defaults if zero-value)
	vis := payload.Visual
	isZero := vis.Char == 0 &&
		vis.FgColor == (terminal.RGB{}) &&
		vis.BgColor == (terminal.RGB{}) &&
		!vis.RenderFg &&
		!vis.RenderBg &&
		vis.BoxStyle == component.BoxDrawNone
	if isZero {
		vis = component.WallVisualConfig{
			BgColor:  visual.RgbWallStone,
			RenderBg: true,
		}
	}

	// Convert event rooms to maze.RoomSpec (best-effort placement by generator)
	var rooms []maze.RoomSpec
	for _, r := range payload.Rooms {
		mCX := r.CenterX / payload.CellWidth
		if mCX%2 == 0 {
			if r.CenterX%payload.CellWidth > payload.CellWidth/2 {
				mCX++
			} else {
				mCX--
			}
		}
		mCY := r.CenterY / payload.CellHeight
		if mCY%2 == 0 {
			if r.CenterY%payload.CellHeight > payload.CellHeight/2 {
				mCY++
			} else {
				mCY--
			}
		}

		rooms = append(rooms, maze.RoomSpec{
			CenterX: mCX,
			CenterY: mCY,
			Width:   r.Width / payload.CellWidth,
			Height:  r.Height / payload.CellHeight,
		})
	}

	// Generate maze
	cfg := maze.Config{
		Width:             mazeWidth,
		Height:            mazeHeight,
		Braiding:          payload.Braiding,
		RemoveBorders:     true,
		RoomCount:         payload.RoomCount,
		Rooms:             rooms,
		DefaultRoomWidth:  payload.DefaultRoomWidth / payload.CellWidth,
		DefaultRoomHeight: payload.DefaultRoomHeight / payload.CellHeight,
	}
	result := maze.Generate(cfg)

	// Force-clear explicit rooms in world space post-generation
	// Generator may reject rooms near edges due to margin constraints;
	// this guarantees room areas are passable regardless
	// Pad by 1 maze cell on each side for entity visual overflow (glow, radius)
	for _, r := range payload.Rooms {
		if r.CenterX == 0 && r.CenterY == 0 {
			continue
		}
		roomW := r.Width
		if roomW <= 0 {
			roomW = payload.DefaultRoomWidth
		}
		roomH := r.Height
		if roomH <= 0 {
			roomH = payload.DefaultRoomHeight
		}

		x0 := r.CenterX - roomW/2
		y0 := r.CenterY - roomH/2

		mx0 := max(0, x0/payload.CellWidth-1)
		my0 := max(0, y0/payload.CellHeight-1)
		mx1 := min(mazeWidth-1, (x0+roomW-1)/payload.CellWidth+1)
		my1 := min(mazeHeight-1, (y0+roomH-1)/payload.CellHeight+1)

		for my := my0; my <= my1; my++ {
			for mx := mx0; mx <= mx1; mx++ {
				result.Grid[my][mx] = false
			}
		}
	}

	// Collect all wall cell entries from maze grid
	cells := make([]component.WallCellDef, 0, mazeWidth*mazeHeight)

	for my, row := range result.Grid {
		for mx, isWall := range row {
			if !isWall {
				continue
			}
			bx := mx * payload.CellWidth
			by := my * payload.CellHeight

			for dy := 0; dy < payload.CellHeight; dy++ {
				for dx := 0; dx < payload.CellWidth; dx++ {
					cells = append(cells, component.WallCellDef{
						OffsetX: bx + dx,
						OffsetY: by + dy,
						WallVisualConfig: component.WallVisualConfig{
							Char:     vis.Char,
							FgColor:  vis.FgColor,
							BgColor:  vis.BgColor,
							RenderFg: vis.RenderFg,
							RenderBg: vis.RenderBg,
						},
					})
				}
			}
		}
	}

	// Batch spawn with anchor at origin (offsets are absolute positions)
	s.executeBatchSpawn(&event.WallBatchSpawnRequestPayload{
		X:             0,
		Y:             0,
		BlockMask:     payload.BlockMask,
		BoxStyle:      vis.BoxStyle,
		CollisionMode: payload.CollisionMode,
		Cells:         cells,
	})
}

// computeBoxCharsInArea recomputes box-drawing characters for walls in rectangular area
// Called after bulk wall spawns to resolve neighbor topology
func (s *WallSystem) computeBoxCharsInArea(x, y, width, height int, style component.BoxDrawStyle) {
	if style == component.BoxDrawNone {
		return
	}

	config := s.world.Resources.Config

	// Clamp to map bounds
	endX := x + width
	endY := y + height
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if endX > config.MapWidth {
		endX = config.MapWidth
	}
	if endY > config.MapHeight {
		endY = config.MapHeight
	}

	// Iterate cells in area
	for cy := y; cy < endY; cy++ {
		for cx := x; cx < endX; cx++ {
			entities := s.world.Positions.GetAllEntityAt(cx, cy)
			for _, e := range entities {
				wall, ok := s.world.Components.Wall.GetComponent(e)
				if !ok || wall.BoxStyle != style {
					continue
				}
				wall.Rune = s.computeBoxChar(cx, cy, style)
				s.world.Components.Wall.SetComponent(e, wall)
			}
		}
	}
}

// handleDespawnAllSilent destroys all walls bypassing death system
func (s *WallSystem) handleDespawnAllSilent() {
	wallEntities := s.world.Components.Wall.GetAllEntities()
	if len(wallEntities) == 0 {
		return
	}
	// Direct destruction without protection check - death pipeline freezes on large map clears
	s.world.DestroyEntitiesBatch(wallEntities)

	config := s.world.Resources.Config
	s.world.PushEvent(event.EventWallDespawned, &event.WallDespawnedPayload{
		X: 0, Y: 0,
		Width: config.MapWidth, Height: config.MapHeight,
		Count: len(wallEntities),
	})
}

// --- Box ---

// Box-drawing neighbor bitmask: N=1, E=2, S=4, W=8
const (
	boxNeighborN uint8 = 1
	boxNeighborE uint8 = 2
	boxNeighborS uint8 = 4
	boxNeighborW uint8 = 8
)

// computeBoxChar returns appropriate box character based on neighbor topology
// Uses void-aware arm selection:
// - Edges (1 cardinal void): arms parallel to boundary
// - Corners (2 adjacent voids): arms toward interior walls
// - Inner corners (all cardinals wall, diagonal void): arms toward cardinal neighbors adjacent to void
func (s *WallSystem) computeBoxChar(x, y int, style component.BoxDrawStyle) rune {
	if style == component.BoxDrawNone {
		return 0
	}

	// Identify cardinal voids and walls
	// Unrolled for performance
	var voidBits, wallBits uint8

	// North (0, -1)
	if s.hasWallWithStyle(x, y-1, style) {
		wallBits |= boxNeighborN
	} else {
		voidBits |= boxNeighborN
	}
	// East (1, 0)
	if s.hasWallWithStyle(x+1, y, style) {
		wallBits |= boxNeighborE
	} else {
		voidBits |= boxNeighborE
	}
	// South (0, 1)
	if s.hasWallWithStyle(x, y+1, style) {
		wallBits |= boxNeighborS
	} else {
		voidBits |= boxNeighborS
	}
	// West (-1, 0)
	if s.hasWallWithStyle(x-1, y, style) {
		wallBits |= boxNeighborW
	} else {
		voidBits |= boxNeighborW
	}

	// Outer perimeter: at least one cardinal void
	if voidBits != 0 {
		var mask uint8
		voidCount := popCount8(voidBits)

		if voidCount == 1 {
			// Edge: arms perpendicular to the single void
			switch voidBits {
			case boxNeighborN, boxNeighborS:
				mask = (boxNeighborE | boxNeighborW) & wallBits
			case boxNeighborE, boxNeighborW:
				mask = (boxNeighborN | boxNeighborS) & wallBits
			}
		} else {
			// Corner (2 adjacent), corridor (2 opposite), peninsula (3): arms toward all walls
			mask = wallBits
		}

		if style == component.BoxDrawDouble {
			return visual.BoxDrawDoubleLUT[mask]
		}
		return visual.BoxDrawSingleLUT[mask]
	}

	// Inner corner: all cardinals are walls, check diagonals for voids
	// To preserve wall continuity, we must connect the two cardinal neighbors
	// that border the diagonal void.
	var mask uint8

	// NE Void (1, -1) -> Neighbors N and E are adjacent -> Connect N|E
	if !s.hasWallWithStyle(x+1, y-1, style) {
		mask |= boxNeighborN | boxNeighborE
	}
	// SE Void (1, 1) -> Neighbors S and E are adjacent -> Connect S|E
	if !s.hasWallWithStyle(x+1, y+1, style) {
		mask |= boxNeighborS | boxNeighborE
	}
	// SW Void (-1, 1) -> Neighbors S and W are adjacent -> Connect S|W
	if !s.hasWallWithStyle(x-1, y+1, style) {
		mask |= boxNeighborS | boxNeighborW
	}
	// NW Void (-1, -1) -> Neighbors N and W are adjacent -> Connect N|W
	if !s.hasWallWithStyle(x-1, y-1, style) {
		mask |= boxNeighborN | boxNeighborW
	}

	if mask == 0 {
		return 0 // True interior (all 8 neighbors are walls)
	}

	if style == component.BoxDrawDouble {
		return visual.BoxDrawDoubleLUT[mask]
	}
	return visual.BoxDrawSingleLUT[mask]
}

// popCount8 returns number of set bits in uint8
func popCount8(b uint8) int {
	b = b - ((b >> 1) & 0x55)
	b = (b & 0x33) + ((b >> 2) & 0x33)
	return int((b + (b >> 4)) & 0x0F)
}

// hasWallWithStyle checks if position contains wall entity with matching BoxStyle
func (s *WallSystem) hasWallWithStyle(x, y int, style component.BoxDrawStyle) bool {
	config := s.world.Resources.Config
	if x < 0 || x >= config.MapWidth || y < 0 || y >= config.MapHeight {
		return false
	}

	entities := s.world.Positions.GetAllEntityAt(x, y)
	for _, e := range entities {
		if wall, ok := s.world.Components.Wall.GetComponent(e); ok {
			if wall.BoxStyle == style {
				return true
			}
		}
	}
	return false
}

// isPerimeterWall returns true if wall at (x,y) has at least one non-wall neighbor
func (s *WallSystem) isPerimeterWall(x, y int, style component.BoxDrawStyle) bool {
	offsets := [4][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}
	for _, off := range offsets {
		if !s.hasWallWithStyle(x+off[0], y+off[1], style) {
			return true
		}
	}
	return false
}

// invalidateBoxNeighbors recomputes box chars for adjacent walls
func (s *WallSystem) invalidateBoxNeighbors(x, y int) {
	config := s.world.Resources.Config
	offsets := [4][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

	for _, off := range offsets {
		nx, ny := x+off[0], y+off[1]
		if nx < 0 || nx >= config.MapWidth || ny < 0 || ny >= config.MapHeight {
			continue
		}

		entities := s.world.Positions.GetAllEntityAt(nx, ny)
		for _, e := range entities {
			wall, ok := s.world.Components.Wall.GetComponent(e)
			if !ok || wall.BoxStyle == component.BoxDrawNone {
				continue
			}
			wall.Rune = s.computeBoxChar(nx, ny, wall.BoxStyle)
			s.world.Components.Wall.SetComponent(e, wall)
		}
	}
}