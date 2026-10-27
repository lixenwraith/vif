package system

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/pattern"
	"github.com/lixenwraith/vif/pkg/cave"
	"github.com/lixenwraith/vif/pkg/maze"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// WallSystem manages wall lifecycle, spawning, and entity displacement
type WallSystem struct {
	world *engine.World

	// Push-out tracking - positions needing entity check this tick
	pendingPushChecks []vmath.Point
	cursors           []cursorCell // the roster's cursors as a push check found them

	// Configuration
	pushCheckEveryTick bool // When true, runs full push check in Update()

	// Maze layouts and cave seeds draw from a PCG outside RandResource, whose
	// position needs a D-19 snapshot
	genPCG *rand.PCG
	genRng *rand.Rand

	cave caveState
	// The cave's rock as last laid, and the map it was laid across
	rockCols []engine.RockColumn
	rockMap  [2]int
	// A cave step's scratch: the rock's columns and the walls' cells before the step
	// (then the cells rock arrived on as well), the walls it moves, those that left
	// the map, and the cells rock arrived on
	rockPrev   []engine.RockColumn
	caveRock   map[int]bool
	caveMovers []core.Entity
	caveHeads  map[core.Entity]bool
	caveDoomed []core.Entity
	caveLanded []vmath.Point
	// The composites a push check reached, each moved once as one body
	bodyHits  []compositeHit
	bodySeen  map[core.Entity]bool
	bodyCells []vmath.Point
	body      []core.Entity

	// Metrics
	statEnabled    *atomic.Bool
	statWallCount  *atomic.Int64
	statPushEvents *atomic.Int64
	buffers        bufferTelemetry

	toggle
}

// cursorCell is a roster cursor and the cell it stood on as a push check began
type cursorCell struct {
	entity core.Entity
	x, y   int
}

// compositeHit is a composite a wall reached, and the cell where it first did
type compositeHit struct {
	header core.Entity
	x, y   int
}

// caveState is a cave and its D-19 record. Column i of the profile stands at
// x = i − Offset as the Position's rock field, which the record alone derives.
type caveState struct {
	Active bool                       `json:"active"`           // its rock stands
	Halted bool                       `json:"halted,omitempty"` // its scroll stopped
	Config cave.Config                `json:"config"`
	Period time.Duration              `json:"period"` // a column's scroll
	Phase  time.Duration              `json:"phase"`  // carried toward the next column
	Offset int64                      `json:"offset"`
	Mask   component.WallBlockMask    `json:"mask"`
	Visual component.WallVisualConfig `json:"visual"`
}

// NewWallSystem creates a new wall system
func NewWallSystem(world *engine.World) engine.System {
	s := &WallSystem{
		world: world,
	}

	s.statEnabled = world.Resources.Status.Bools.Get("wall.enabled")
	s.statWallCount = world.Resources.Status.Ints.Get("wall.count")
	s.statPushEvents = world.Resources.Status.Ints.Get("wall.push_events")
	s.buffers = newBufferTelemetry(world.Resources.Status, "wall", "pending_push_checks")

	return s
}

func (s *WallSystem) Init() {
	s.pendingPushChecks = make([]vmath.Point, 0, 64)
	s.pushCheckEveryTick = false
	s.genPCG = rand.NewPCG(
		vmath.DeriveSeed(s.world.Resources.Rand.DomainRoot(core.DomainShared), s.Name()+":maze"), 0)
	s.genRng = rand.New(s.genPCG)
	s.liftCave()
	s.statEnabled.Store(true)
	s.statWallCount.Store(0)
	s.statPushEvents.Store(0)
	s.buffers.Reset()
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
		event.EventCaveStart,
		event.EventCaveStop,
		event.EventWallDespawnAll,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *WallSystem) HandleEvent(ev event.GameEvent) {
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

	case event.EventCaveStart:
		if payload, ok := ev.Payload.(*event.CaveStartPayload); ok {
			s.startCave(payload)
		}

	case event.EventCaveStop:
		s.cave.Halted = s.cave.Active

	case event.EventWallDespawnAll:
		s.handleDespawnAllSilent()
	}
}

func (s *WallSystem) Update() {
	if !s.enabled {
		return
	}

	// Spawned walls push from the cells they took, before a cave step moves them
	s.processPendingPushChecks()

	// Optional full check (toggled for performance testing or special game states)
	if s.pushCheckEveryTick {
		s.runFullPushCheck()
	}

	s.scrollCave()

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
			n := s.world.Positions.GetEntitiesAtInto(payload.X, payload.Y, engine.ScopeShared, entityBuf[:])
			for j := range n {
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

	entity := s.world.CreateEntity(core.DomainShared)
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
		s.pendingPushChecks = append(s.pendingPushChecks, vmath.Point{X: payload.X, Y: payload.Y})
		s.buffers.Observe(0, len(s.pendingPushChecks))
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
		points := make([]vmath.Point, len(resolved))
		for i, rc := range resolved {
			points[i] = vmath.Point{X: rc.x, Y: rc.y}
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

		for _, rc := range resolved {
			n := s.world.Positions.GetEntitiesAtInto(rc.x, rc.y, engine.ScopeShared, entityBuf[:])
			for j := range n {
				if s.world.Components.Wall.HasEntity(entityBuf[j]) {
					toDestroy = append(toDestroy, entityBuf[j])
				}
			}
		}

		if len(toDestroy) > 0 {
			s.world.DestroyEntitiesBatch(toDestroy)
		}

	case event.WallBatchFailIfBlocked:
		points := make([]vmath.Point, len(resolved))
		for i, rc := range resolved {
			points[i] = vmath.Point{X: rc.x, Y: rc.y}
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
		headerEntity = s.world.CreateEntity(core.DomainShared)
		s.world.Positions.SetPosition(headerEntity, component.PositionComponent{
			X: payload.X, Y: payload.Y,
		})
		s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
			Mask: component.ProtectAll ^ component.ProtectFromDeath,
		})
	}

	// 4. Create entities, set components, batch positions
	var members []component.MemberEntry
	if payload.Composite {
		members = make([]component.MemberEntry, 0, len(resolved))
	}
	posBatch := s.world.Positions.BeginBatch(len(resolved))

	result.minX, result.minY = config.MapWidth, config.MapHeight
	result.maxX, result.maxY = 0, 0
	if payload.BlockMask != component.WallBlockNone {
		s.pendingPushChecks = slices.Grow(s.pendingPushChecks, len(resolved))
	}

	for _, rc := range resolved {
		cell := payload.Cells[rc.idx]
		entity := s.world.CreateEntity(core.DomainShared)

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
			s.pendingPushChecks = append(s.pendingPushChecks, vmath.Point{X: rc.x, Y: rc.y})
			s.buffers.Observe(0, len(s.pendingPushChecks))
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

// handlePatternSpawn loads a .vifimg pattern and spawns it as a composite wall.
func (s *WallSystem) handlePatternSpawn(payload *event.WallPatternSpawnRequestPayload) {
	file, err := s.world.Resources.Files.Open(paths.ImageDirName, payload.Path)
	if err != nil {
		s.world.DebugPrint("pattern file failed: " + err.Error())
		return
	}
	defer file.Close()

	result, err := pattern.ReadDualModePattern(file, s.world.Resources.Config.ColorMode)
	if err != nil {
		s.world.DebugPrint("pattern load failed: " + err.Error())
		return
	}
	if result.Empty() {
		return
	}

	s.executeBatchSpawn(&event.WallBatchSpawnRequestPayload{
		X:             payload.X,
		Y:             payload.Y,
		BlockMask:     payload.BlockMask,
		CollisionMode: payload.CollisionMode,
		Composite:     true,
		Cells:         result.ToWallCellDefs(),
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

	// The area's cells rather than every wall: a footprint asks one cell at a time
	gridW, gridH := s.world.Positions.GridDimensions()
	var entities [parameter.MaxEntitiesPerCell]core.Entity
	for y := max(payload.Y, 0); y < min(payload.Y+height, gridH); y++ {
		for x := max(payload.X, 0); x < min(payload.X+width, gridW); x++ {
			n := s.world.Positions.GetEntitiesAtInto(x, y, engine.ScopeShared, entities[:])
			for _, entity := range entities[:n] {
				if wall, ok := s.world.Components.Wall.GetPtr(entity); ok {
					s.classifyWallForDespawn(entity, *wall, &flashTargets, &fadeoutTargets, &silentTargets)
				}
			}
		}
	}

	count := len(flashTargets) + len(fadeoutTargets) + len(silentTargets)

	if len(flashTargets) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, flashTargets...)
	}
	if len(fadeoutTargets) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFadeoutSpawnOne, fadeoutTargets...)
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

// despawnAllWalls handles All=true despawn with proper effects; the cave's rock
// lifts at once
func (s *WallSystem) despawnAllWalls() {
	s.liftCave()
	var flashTargets []core.Entity
	var fadeoutTargets []core.Entity
	var silentTargets []core.Entity

	wallEntities := s.world.Components.Wall.Entities()
	for _, entity := range wallEntities {
		wall, ok := s.world.Components.Wall.GetPtr(entity)
		if !ok {
			silentTargets = append(silentTargets, entity)
			continue
		}

		s.classifyWallForDespawn(entity, *wall, &flashTargets, &fadeoutTargets, &silentTargets)
	}

	count := len(flashTargets) + len(fadeoutTargets) + len(silentTargets)

	if len(flashTargets) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, flashTargets...)
	}
	if len(fadeoutTargets) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFadeoutSpawnOne, fadeoutTargets...)
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

	wallEntities := s.world.Components.Wall.Entities()
	for _, entity := range wallEntities {
		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		if pos.X < payload.X || pos.X >= payload.X+width ||
			pos.Y < payload.Y || pos.Y >= payload.Y+height {
			continue
		}

		wall, ok := s.world.Components.Wall.GetPtr(entity)
		if !ok {
			continue
		}

		wasBlocking := wall.BlockMask != component.WallBlockNone
		if wall.BlockMask != payload.BlockMask {
			wall.BlockMask = payload.BlockMask
			s.world.Positions.WallsChanged()
		}

		if !wasBlocking && payload.BlockMask != component.WallBlockNone {
			s.pendingPushChecks = append(s.pendingPushChecks, vmath.Point{X: pos.X, Y: pos.Y})
			s.buffers.Observe(0, len(s.pendingPushChecks))
		}
	}
}

// processPendingPushChecks displaces entities from newly blocking positions
func (s *WallSystem) processPendingPushChecks() {
	if len(s.pendingPushChecks) == 0 {
		return
	}

	var pushCount int64
	cursors := s.readCursors()

	for _, pt := range s.pendingPushChecks {
		pushCount += s.pushEntitiesAtPosition(pt.X, pt.Y, cursors)
	}
	pushCount += s.displaceBodies(false)

	s.statPushEvents.Add(pushCount)
	s.pendingPushChecks = s.pendingPushChecks[:0]
}

// runFullPushCheck iterates all blocking walls and pushes out any entities
func (s *WallSystem) runFullPushCheck() {
	var pushCount int64
	cursors := s.readCursors()

	wallEntities := s.world.Components.Wall.Entities()
	for _, wallEntity := range wallEntities {
		wallComp, ok := s.world.Components.Wall.GetPtr(wallEntity)
		if !ok || wallComp.BlockMask == component.WallBlockNone {
			continue
		}

		wallPos, ok := s.world.Positions.GetPosition(wallEntity)
		if !ok {
			continue
		}

		pushCount += s.pushEntitiesAtPosition(wallPos.X, wallPos.Y, cursors)
	}
	s.inRock(func(x, y int) { pushCount += s.pushEntitiesAtPosition(x, y, cursors) })
	pushCount += s.displaceBodies(false)

	s.statPushEvents.Add(pushCount)
}

// readCursors takes the roster's cursors and their cells once a push check, in slot
// order: a push only queues a cursor's move, so the cells hold for the whole check
func (s *WallSystem) readCursors() []cursorCell {
	s.cursors = s.cursors[:0]
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		if pos, ok := s.world.Positions.GetPosition(cursor); ok {
			s.cursors = append(s.cursors, cursorCell{entity: cursor, x: pos.X, y: pos.Y})
		}
	}
	return s.cursors
}

// pushEntitiesAtPosition displaces all non-wall entities at given position
func (s *WallSystem) pushEntitiesAtPosition(x, y int, cursors []cursorCell) int64 {
	var pushCount int64

	for _, cursor := range cursors {
		if cursor.x != x || cursor.y != y {
			continue
		}
		newX, newY, found := s.world.ResolveFreeCell(x, y, component.WallBlockCursor)
		if found && (newX != x || newY != y) {
			s.world.PushEvent(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{
				Entity: cursor.entity,
				X:      newX,
				Y:      newY,
			})
			pushCount++
		}
	}

	// Check other entities
	// Both domains: a new wall claims its cell and displaces whoever holds it (D-12)
	var entities [parameter.MaxEntitiesPerCell]core.Entity
	count := s.world.Positions.GetAllEntitiesAtInto(x, y, entities[:])
	for i := range count {
		entity := entities[i]
		if s.world.Components.Cursor.HasEntity(entity) {
			continue
		}
		if s.world.Components.Wall.HasEntity(entity) || s.reachBody(entity, x, y) {
			continue
		}

		mask := s.getMaskForEntity(entity)
		if mask == component.WallBlockNone {
			continue
		}

		pushCount += s.pushOut(entity, x, y, mask)
	}

	return pushCount
}

// reachBody queues the composite e moves with, a header with a body to fly or one of
// its members, to move as one body once the check has reached all it reaches
func (s *WallSystem) reachBody(e core.Entity, x, y int) bool {
	header := e
	if member, ok := s.world.Components.Member.GetComponent(e); ok {
		header = member.HeaderEntity
	}
	if !s.world.Components.Header.HasEntity(header) || !s.world.Components.Kinetic.HasEntity(header) ||
		s.getMaskForEntity(header) == component.WallBlockNone {
		return false
	}
	if s.bodySeen == nil {
		s.bodySeen = make(map[core.Entity]bool)
	}
	if !s.bodySeen[header] {
		s.bodySeen[header] = true
		s.bodyHits = append(s.bodyHits, compositeHit{header: header, x: x, y: y})
	}
	return true
}

// displaceBodies moves every composite the check reached, in the order it reached
// them, and forgets them for the next check
func (s *WallSystem) displaceBodies(cave bool) int64 {
	var pushes int64
	for _, hit := range s.bodyHits {
		pushes += s.displaceComposite(hit, cave)
	}
	s.bodyHits = s.bodyHits[:0]
	clear(s.bodySeen)
	return pushes
}

// displaceComposite moves a composite a wall reached as one body: in a cave a column
// left and a row toward the channel, else straight left, as the rock travels; else to
// the nearest place its footprint fits. One with nowhere to go is crushed, and no
// one is rewarded for it.
func (s *WallSystem) displaceComposite(hit compositeHit, cave bool) int64 {
	positions := s.world.Positions
	header, ok := s.world.Components.Header.GetComponent(hit.header)
	headerPos, placed := positions.GetPosition(hit.header)
	if !ok || !placed {
		return 0
	}
	mask := s.getMaskForEntity(hit.header)
	s.bodyCells = s.bodyCells[:0]
	for _, m := range header.MemberEntries {
		if pos, ok := positions.GetPosition(m.Entity); ok && m.Entity != 0 {
			s.bodyCells = append(s.bodyCells, vmath.Point{X: pos.X, Y: pos.Y})
		}
	}
	if len(s.bodyCells) == 0 {
		s.bodyCells = append(s.bodyCells, vmath.Point{X: headerPos.X, Y: headerPos.Y})
	}
	fits := func(dx, dy int) bool {
		for _, c := range s.bodyCells {
			if positions.IsBlocked(c.X+dx, c.Y+dy, mask) {
				return false
			}
		}
		return true
	}
	if fits(0, 0) {
		return 0
	}

	dx, dy, found := 0, 0, false
	if cave {
		if row := s.towardChannel(hit.x, hit.y, mask); row != 0 && fits(-1, row) {
			dx, dy, found = -1, row, true
		} else if fits(-1, 0) {
			dx, found = -1, true
		}
	}
	if !found {
		left, top := s.bodyCells[0].X, s.bodyCells[0].Y
		right, bottom := left, top
		for _, c := range s.bodyCells[1:] {
			left, top, right, bottom = min(left, c.X), min(top, c.Y), max(right, c.X), max(bottom, c.Y)
		}
		var x, y int
		x, y, found = positions.FindFreeAreaSpiral(headerPos.X, headerPos.Y, right-left+1, bottom-top+1,
			headerPos.X-left, headerPos.Y-top, mask, 0)
		dx, dy = x-left, y-top
	}
	if !found {
		s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{HeaderEntity: hit.header})
		return 0
	}
	s.body = s.world.ShiftComposite(hit.header, dx, dy, s.body)
	return sharedCount(hit.header)
}

// pushOut moves a non-cursor entity to the nearest cell its wall leaves free, or
// destroys it stuck if it is a combatant no cursor owns. Only a shared entity counts:
// the count is compared between instances, which hold their own player entities.
func (s *WallSystem) pushOut(entity core.Entity, x, y int, mask component.WallBlockMask) int64 {
	if _, _, moved := s.world.PushEntityFromBlocked(entity, mask); moved {
		return sharedCount(entity)
	}
	// Not moved: either this wall does not block the entity, or no free cell exists
	if !s.world.Positions.IsBlocked(x, y, mask) {
		return 0
	}
	if combat, ok := s.world.Components.Combat.GetPtr(entity); ok {
		if !s.world.Components.Cursor.HasEntity(combat.OwnerEntity) {
			event.EmitDeath(s.world.Resources.Event.Queue, 0, entity)
		}
	}
	return 0
}

// sharedCount is 1 for a shared entity: what a push counts toward the compared total
func sharedCount(e core.Entity) int64 {
	if e.Domain() == core.DomainShared {
		return 1
	}
	return 0
}

// getMaskForEntity returns appropriate wall block mask for entity type
func (s *WallSystem) getMaskForEntity(entity core.Entity) component.WallBlockMask {
	if s.world.Components.Kraken.HasEntity(entity) {
		return component.WallBlockNone
	}
	if member, ok := s.world.Components.Member.GetPtr(entity); ok && s.world.Components.Kraken.HasEntity(member.HeaderEntity) {
		return component.WallBlockNone
	}

	if s.world.Components.Kinetic.HasEntity(entity) {
		return component.WallBlockKinetic
	}
	if s.world.Components.Particle.HasEntity(entity) {
		return component.WallBlockParticle
	}
	// Phantom heads are non-physical anchors, not subject to wall displacement
	if s.world.Components.Header.HasEntity(entity) {
		return component.WallBlockNone
	}
	return component.WallBlockSpawn
}

// SetPushCheckEveryTick enables or disables per-tick full push check
func (s *WallSystem) SetPushCheckEveryTick(enabled bool) {
	s.pushCheckEveryTick = enabled
}

// handleMazeSpawn generates maze and spawns wall blocks via batch
func (s *WallSystem) handleMazeSpawn(payload *event.MazeSpawnRequestPayload) {
	if payload.CellWidth <= 0 || payload.CellHeight <= 0 {
		return
	}

	config := s.world.Resources.Config

	mazeWidth := config.MapWidth / payload.CellWidth
	mazeHeight := config.MapHeight / payload.CellHeight

	if mazeWidth < 3 || mazeHeight < 3 {
		return
	}

	vis := orStone(payload.Visual)

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
		Rng:               s.genRng,
	}

	result := maze.Generate(cfg)

	// Derive actual dimensions post-ensureOdd adjustment in the generator
	mazeCols := len(result.Grid[0])
	mazeRows := len(result.Grid)

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
		mx1 := min(mazeCols-1, (x0+roomW-1)/payload.CellWidth+1)
		my1 := min(mazeRows-1, (y0+roomH-1)/payload.CellHeight+1)

		for my := my0; my <= my1; my++ {
			for mx := mx0; mx <= mx1; mx++ {
				result.Grid[my][mx] = false
			}
		}
	}

	walls := 0
	for _, row := range result.Grid {
		for _, isWall := range row {
			if isWall {
				walls++
			}
		}
	}
	batch := event.AcquireWallBatchSpawn(walls * payload.CellWidth * payload.CellHeight)
	defer event.ReleaseWallBatchSpawn(batch)
	for my, row := range result.Grid {
		for mx, isWall := range row {
			if !isWall {
				continue
			}
			bx := mx * payload.CellWidth
			by := my * payload.CellHeight

			for dy := range payload.CellHeight {
				for dx := range payload.CellWidth {
					batch.Cells = append(batch.Cells, component.WallCellDef{
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

	// Anchored at the origin: the offsets are absolute positions
	batch.BlockMask, batch.BoxStyle, batch.CollisionMode = payload.BlockMask, vis.BoxStyle, payload.CollisionMode
	s.executeBatchSpawn(batch)
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
	var entities [parameter.MaxEntitiesPerCell]core.Entity
	for cy := y; cy < endY; cy++ {
		for cx := x; cx < endX; cx++ {
			count := s.world.Positions.GetEntitiesAtInto(cx, cy, engine.ScopeShared, entities[:])
			for i := range count {
				e := entities[i]
				wall, ok := s.world.Components.Wall.GetPtr(e)
				if !ok || wall.BoxStyle != style {
					continue
				}
				wall.Rune = s.computeBoxChar(cx, cy, style)
			}
		}
	}
}

// handleDespawnAllSilent destroys all walls bypassing death system, and lifts the
// cave's rock
func (s *WallSystem) handleDespawnAllSilent() {
	s.liftCave()
	wallEntities := s.world.Components.Wall.GetAllEntities()
	if len(wallEntities) == 0 {
		return
	}
	// Detached copy is required because batch destruction mutates Wall.Entities().
	// Direct destruction bypasses protection; the death pipeline freezes on large map clears.
	s.world.DestroyEntitiesBatch(wallEntities)

	config := s.world.Resources.Config
	s.world.PushEvent(event.EventWallDespawned, &event.WallDespawnedPayload{
		X: 0, Y: 0,
		Width: config.MapWidth, Height: config.MapHeight,
		Count: len(wallEntities),
	})
}

// --- Cave ---

// orStone is vis, or the stone fill a zero visual asks for
func orStone(vis component.WallVisualConfig) component.WallVisualConfig {
	if vis == (component.WallVisualConfig{}) {
		return component.WallVisualConfig{BgColor: visual.RgbWallStone, RenderBg: true}
	}
	return vis
}

// startCave runs the payload's cave in place of any other and lays its rock across
// the map, its mouth at the left edge, over whatever walls stand there; what the
// rock lands on is pushed out as from any spawn
func (s *WallSystem) startCave(p *event.CaveStartPayload) {
	if !(p.Speed >= 0) {
		return
	}
	cfg := cave.Config{
		Seed:       p.Seed,
		Height:     s.world.Resources.Config.MapHeight,
		MinGap:     cmp.Or(p.MinGap, parameter.CaveDefaultMinGap),
		MaxGap:     cmp.Or(p.MaxGap, parameter.CaveDefaultMaxGap),
		MinWall:    p.MinWall,
		Wavelength: cmp.Or(p.Wavelength, parameter.CaveDefaultWavelength),
		Swing:      p.Swing,
		Detail:     p.Detail,
		Ramp:       p.Ramp,
	}
	if err := cfg.Validate(); err != nil {
		s.world.DebugPrint(err.Error())
		return
	}
	if cfg.Seed == 0 {
		cfg.Seed = s.genRng.Uint64()
	}
	// The period clamps before it converts, so no speed overflows it
	period := float64(time.Second) / cmp.Or(p.Speed, parameter.CaveDefaultSpeed)
	period = min(max(period, float64(parameter.CaveMinPeriod)), float64(parameter.CaveMaxPeriod))
	s.cave = caveState{
		Active: true, Config: cfg, Period: time.Duration(math.Round(period)),
		Mask: cmp.Or(p.BlockMask, component.WallBlockAll), Visual: orStone(p.Visual),
	}
	s.layRock()
	s.pushOutOfRock()
}

// liftCave ends any cave and lifts its rock
func (s *WallSystem) liftCave() {
	s.cave = caveState{}
	s.rockMap = [2]int{}
	s.world.Positions.SetRock(component.WallBlockNone, component.WallVisualConfig{}, 0, nil)
}

// caveProfile is the cave's profile at the map's height, or at its start's height
// when the profile cannot fit the map
func (s *WallSystem) caveProfile() cave.Config {
	cfg := s.cave.Config
	if cfg.Height = s.world.Resources.Config.MapHeight; cfg.Validate() != nil {
		return s.cave.Config
	}
	return cfg
}

// rockColumn is profile column i as rock
func rockColumn(cfg cave.Config, i int64) engine.RockColumn {
	col := cfg.At(i)
	return engine.RockColumn{Ceil: col.Top, Floor: cfg.Height - col.Bottom}
}

// layRock lays the cave's rock across the map from its offset: a start, a resize
// and a load each derive the same field from the record and the map
func (s *WallSystem) layRock() {
	config := s.world.Resources.Config
	cfg := s.caveProfile()
	s.rockCols = s.rockCols[:0]
	for x := range config.MapWidth {
		s.rockCols = append(s.rockCols, rockColumn(cfg, s.cave.Offset+int64(x)))
	}
	s.rockMap = [2]int{config.MapWidth, config.MapHeight}
	s.world.Positions.SetRock(s.cave.Mask, s.cave.Visual, s.cave.Offset, s.rockCols)
}

// pushOutOfRock queues a push check at every cell where something stands in the
// rock, as a spawn queues its cells
func (s *WallSystem) pushOutOfRock() {
	s.inRock(func(x, y int) { s.pendingPushChecks = append(s.pendingPushChecks, vmath.Point{X: x, Y: y}) })
}

// inRock calls fn with the cell of every entity standing in the rock field
func (s *WallSystem) inRock(fn func(x, y int)) {
	rock, h := s.world.Positions.Rock(), s.world.Resources.Config.MapHeight
	if rock == nil {
		return
	}
	for _, e := range s.world.Positions.Entities() {
		pos, _ := s.world.Positions.GetPosition(e)
		if col, ok := rock.Column(pos.X); ok && pos.Y >= 0 && pos.Y < h && col.Rock(pos.Y) {
			fn(pos.X, pos.Y)
		}
	}
}

// scrollCave carries the phase a period a column and steps at most a column a tick:
// a cursor's push is a queued move, so a second step would push it from a cell it
// has already left. A resized map takes the rock reshaped to its bounds first.
func (s *WallSystem) scrollCave() {
	if !s.cave.Active {
		return
	}
	if config := s.world.Resources.Config; s.rockMap != [2]int{config.MapWidth, config.MapHeight} {
		s.layRock()
		s.pushOutOfRock()
	}
	if s.cave.Halted {
		return
	}
	elapsed := min(max(s.world.Resources.Time.DeltaTime, 0), time.Duration(parameter.MaxSimulationDeltaSeconds*float64(time.Second)))
	s.cave.Phase = min(s.cave.Phase+elapsed, 2*s.cave.Period-1)
	if s.cave.Phase >= s.cave.Period {
		s.cave.Phase -= s.cave.Period
		s.stepCave()
	}
}

// stepCave moves the cave's rock and every shared wall a column left, and displaces
// what stands where rock arrived. A wall composite's header moves with its members,
// so the composite sync finds them in place.
func (s *WallSystem) stepCave() {
	positions, walls := s.world.Positions, s.world.Components.Wall
	w, h := s.world.Resources.Config.MapWidth, s.world.Resources.Config.MapHeight
	if s.caveRock == nil {
		s.caveRock, s.caveHeads = make(map[int]bool), make(map[core.Entity]bool)
	}
	clear(s.caveRock)
	clear(s.caveHeads)

	// Rock before the step: the field's columns, and the cells of the walls
	s.caveMovers = s.caveMovers[:0]
	for _, e := range walls.Entities() {
		pos, ok := positions.GetPosition(e)
		if !ok || e.Domain() != core.DomainShared {
			continue
		}
		s.caveMovers = append(s.caveMovers, e)
		if wall, _ := walls.GetComponent(e); wall.BlockMask != component.WallBlockNone &&
			pos.X >= 0 && pos.X < w && pos.Y >= 0 && pos.Y < h {
			s.caveRock[pos.Y*w+pos.X] = true
		}
		if member, ok := s.world.Components.Member.GetComponent(e); ok && !s.caveHeads[member.HeaderEntity] &&
			positions.HasPosition(member.HeaderEntity) {
			s.caveHeads[member.HeaderEntity] = true
			s.caveMovers = append(s.caveMovers, member.HeaderEntity)
		}
	}
	rock := positions.Rock()
	s.rockPrev = s.rockPrev[:0]
	if rock != nil {
		s.rockPrev = append(s.rockPrev, rock.Columns()...)
	}

	// A wall the map no longer holds is gone; a header past the edge still anchors
	if len(s.caveMovers) > 0 {
		s.caveDoomed = positions.Shift(s.caveMovers, -1, 0, s.caveDoomed[:0])
		s.caveDoomed = slices.DeleteFunc(s.caveDoomed, func(e core.Entity) bool { return !walls.HasEntity(e) })
		s.world.DestroyEntitiesBatch(s.caveDoomed)
	}
	if rock != nil {
		positions.ShiftRock(rockColumn(s.caveProfile(), s.cave.Offset+int64(rock.Width())))
	}
	s.cave.Offset++

	s.caveLanded = s.caveLanded[:0]
	land := func(x, y int) {
		idx := y*w + x
		if x < 0 || x >= w || y < 0 || y >= h || s.caveRock[idx] || (x < len(s.rockPrev) && s.rockPrev[x].Rock(y)) {
			return
		}
		s.caveRock[idx] = true
		s.caveLanded = append(s.caveLanded, vmath.Point{X: x, Y: y})
	}
	if rock != nil {
		for x := range min(w, rock.Width()) {
			was, now := s.rockPrev[x], rock.Columns()[x]
			for y := max(was.Ceil, 0); y < min(now.Ceil, h); y++ {
				land(x, y)
			}
			for y := max(now.Floor, 0); y < min(was.Floor, h); y++ {
				land(x, y)
			}
		}
	}
	for _, e := range s.caveMovers {
		if wall, ok := walls.GetComponent(e); ok && wall.BlockMask != component.WallBlockNone {
			pos, _ := positions.GetPosition(e)
			land(pos.X, pos.Y)
		}
	}

	cursors := s.readCursors()
	var pushes int64
	for _, pt := range s.caveLanded {
		pushes += s.displaceCave(pt.X, pt.Y, cursors)
	}
	pushes += s.displaceBodies(true)
	s.statPushEvents.Add(pushes)
}

// displaceCave moves what stands where rock arrived a column left and toward the
// channel, the way the rock travels; a composite it reached waits to move as one
// body. Rock that finds a cursor no room crushes it where it stands, and a loose
// glyph is scenery and goes.
func (s *WallSystem) displaceCave(x, y int, cursors []cursorCell) int64 {
	positions := s.world.Positions
	var pushes int64
	for _, cursor := range cursors {
		if cursor.x != x || cursor.y != y || !positions.HasBlockingWallAt(x, y, component.WallBlockCursor) {
			continue
		}
		newX, newY, found := s.caveRoom(x, y, component.WallBlockCursor)
		if !found {
			s.world.PushEvent(event.EventCursorCrushed, &event.CursorCrushedPayload{Entity: cursor.entity})
			continue
		}
		s.world.PushEvent(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{
			Entity: cursor.entity, X: newX, Y: newY,
		})
		pushes++
	}

	var entities [parameter.MaxEntitiesPerCell]core.Entity
	n := positions.GetAllEntitiesAtInto(x, y, entities[:])
	s.caveDoomed = s.caveDoomed[:0]
	for _, e := range entities[:n] {
		// A composite that flies moves whole wherever the rock reached it; any other
		// member is placed by its composite's owner
		if s.world.Components.Cursor.HasEntity(e) || s.world.Components.Wall.HasEntity(e) ||
			s.world.Components.Death.HasEntity(e) || s.reachBody(e, x, y) || s.world.Components.Member.HasEntity(e) {
			continue
		}
		if e.Domain() == core.DomainPlayer && s.world.Components.Glyph.HasEntity(e) {
			s.caveDoomed = append(s.caveDoomed, e)
			continue
		}
		mask := s.getMaskForEntity(e)
		if mask == component.WallBlockNone || !positions.HasBlockingWallAt(x, y, mask) {
			continue
		}
		if newX, newY, found := s.caveRoom(x, y, mask); found {
			s.world.PlaceEntity(e, newX, newY)
			pushes += sharedCount(e)
			continue
		}
		pushes += s.pushOut(e, x, y, mask)
	}
	s.world.DestroyEntitiesBatch(s.caveDoomed)
	return pushes
}

// caveRoom is where rock arriving at (x, y) sends what it displaces: a column left
// and a row toward its column's nearer opening, else straight left
func (s *WallSystem) caveRoom(x, y int, mask component.WallBlockMask) (int, int, bool) {
	positions := s.world.Positions
	if dy := s.towardChannel(x, y, mask); dy != 0 && !positions.IsBlocked(x-1, y+dy, mask) {
		return x - 1, y + dy, true
	}
	if !positions.IsBlocked(x-1, y, mask) {
		return x - 1, y, true
	}
	return 0, 0, false
}

// towardChannel is the row step from (x, y) to the nearer open cell of its column,
// toward the middle row on a tie, or 0 when the column is rock throughout
func (s *WallSystem) towardChannel(x, y int, mask component.WallBlockMask) int {
	positions := s.world.Positions
	h := s.world.Resources.Config.MapHeight
	for d := 1; y-d >= 0 || y+d < h; d++ {
		up := y-d >= 0 && !positions.HasBlockingWallAt(x, y-d, mask)
		down := y+d < h && !positions.HasBlockingWallAt(x, y+d, mask)
		switch {
		case up && down && 2*y < h, down && !up:
			return 1
		case up:
			return -1
		}
	}
	return 0
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

	var entities [parameter.MaxEntitiesPerCell]core.Entity
	count := s.world.Positions.GetEntitiesAtInto(x, y, engine.ScopeShared, entities[:])
	for i := range count {
		if wall, ok := s.world.Components.Wall.GetPtr(entities[i]); ok {
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
	var entities [parameter.MaxEntitiesPerCell]core.Entity

	for _, off := range offsets {
		nx, ny := x+off[0], y+off[1]
		if nx < 0 || nx >= config.MapWidth || ny < 0 || ny >= config.MapHeight {
			continue
		}

		count := s.world.Positions.GetEntitiesAtInto(nx, ny, engine.ScopeShared, entities[:])
		for i := range count {
			e := entities[i]
			wall, ok := s.world.Components.Wall.GetPtr(e)
			if !ok || wall.BoxStyle == component.BoxDrawNone {
				continue
			}
			wall.Rune = s.computeBoxChar(nx, ny, wall.BoxStyle)
		}
	}
}

// genRecordLen is the generator's binary record, rand.PCG's "pcg:" and two words
const genRecordLen = 20

// SaveShared carries what the stores and RandResource cannot: the generator's
// position, then, only while a cave stands, its profile, period, phase and offset,
// from which a load lays its rock; a capture with no cave carries the generator
// alone.
func (s *WallSystem) SaveShared() ([]byte, error) {
	if s.genPCG == nil {
		return nil, errors.New("wall: generator is not initialized")
	}
	gen, err := s.genPCG.MarshalBinary()
	if err != nil || !s.cave.Active {
		return gen, err
	}
	cave, err := json.Marshal(s.cave)
	if err != nil {
		return nil, err
	}
	return append(gen, cave...), nil
}

// LoadShared resumes the generator and the cave at a captured position. The Rand
// wrapper reads through the same source, so no rewrap is needed.
func (s *WallSystem) LoadShared(data []byte) error {
	if s.genPCG == nil {
		return errors.New("wall: generator is not initialized")
	}
	gen, rest := data[:min(len(data), genRecordLen)], data[min(len(data), genRecordLen):]
	var cave caveState
	if len(rest) > 0 {
		if err := json.Unmarshal(rest, &cave); err != nil {
			return fmt.Errorf("wall: cave: %w", err)
		}
		if err := cave.validate(); err != nil {
			return err
		}
	}
	if err := s.genPCG.UnmarshalBinary(gen); err != nil {
		return fmt.Errorf("wall: generator state: %w", err)
	}
	if !cave.Active {
		s.liftCave()
		return nil
	}
	s.cave = cave
	s.layRock()
	return nil
}

// validate accepts exactly the standing caves a start and its steps leave behind
func (c caveState) validate() error {
	if !c.Active {
		return errors.New("wall: a record carries a cave only while one runs")
	}
	if err := c.Config.Validate(); err != nil {
		return fmt.Errorf("wall: %w", err)
	}
	if c.Period < parameter.CaveMinPeriod || c.Period > parameter.CaveMaxPeriod || c.Phase < 0 || c.Phase >= 2*c.Period ||
		c.Offset < 0 || c.Mask == component.WallBlockNone {
		return errors.New("wall: cave scroll out of range")
	}
	return nil
}

type wallLocal struct {
	Shared    []byte        `json:"shared"`
	Pending   []vmath.Point `json:"pending,omitempty"`
	EveryTick bool          `json:"every_tick"`
}

// SaveLocal is the shared record plus the push checks still owed.
func (s *WallSystem) SaveLocal() ([]byte, error) {
	shared, err := s.SaveShared()
	if err != nil {
		return nil, err
	}
	return json.Marshal(wallLocal{Shared: shared, Pending: s.pendingPushChecks, EveryTick: s.pushCheckEveryTick})
}

func (s *WallSystem) LoadLocal(data []byte) error {
	var l wallLocal
	if err := json.Unmarshal(data, &l); err != nil {
		return fmt.Errorf("wall: %w", err)
	}
	s.pendingPushChecks, s.pushCheckEveryTick = append(s.pendingPushChecks[:0], l.Pending...), l.EveryTick
	return s.LoadShared(l.Shared)
}
