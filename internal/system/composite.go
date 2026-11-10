package system

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// CompositeSystem keeps headers and members consistent: member positions propagated
// from the phantom head, liveness tombstones, compaction, integrity breach events and
// destroy requests, where it reads what the body showed for the effect its end leaves
type CompositeSystem struct {
	world   *engine.World
	headers []core.Entity // the tick's headers, detached: syncMembers and destroyHead move the store
	members []core.Entity // an ending composite's living members
	toggle
}

// NewCompositeSystem creates a new composite system
func NewCompositeSystem(world *engine.World) engine.System {
	s := &CompositeSystem{
		world: world,
	}
	return s
}

// Init resets session state for new game
func (s *CompositeSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *CompositeSystem) Name() string {
	return "composite"
}

func (s *CompositeSystem) Priority() int {
	return parameter.PriorityComposite
}

func (s *CompositeSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCompositeMemberDestroyed,
		event.EventCompositeDestroyRequest,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *CompositeSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventCompositeMemberDestroyed:
		if payload, ok := ev.Payload.(*event.CompositeMemberDestroyedPayload); ok {
			s.destroyMember(payload.HeaderEntity, payload.MemberEntity)
		}

	case event.EventCompositeDestroyRequest:
		if payload, ok := ev.Payload.(*event.CompositeDestroyRequestPayload); ok {
			s.destroyComposite(payload.HeaderEntity, payload.Effect)
		}
	}
}

func (s *CompositeSystem) Update() {
	if !s.enabled {
		return
	}

	// syncMembers and destroyHead can structurally mutate Header/Member stores;
	// preserve the original header order and keep component values detached.
	s.headers = append(s.headers[:0], s.world.Components.Header.Entities()...)

	for _, headerEntity := range s.headers {
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerPos, ok := s.world.Positions.GetPosition(headerEntity)
		if !ok {
			continue
		}

		// Count living members before sync
		countBefore := s.countLiving(&headerComp)

		// Sync: validate liveness, propagate positions, tombstone dead
		s.syncMembers(&headerComp, headerPos.X, headerPos.Y)

		// Compact tombstones
		if headerComp.Dirty {
			s.compactMembers(&headerComp)
			headerComp.Dirty = false
		}

		// Calculate external deaths
		countAfter := len(headerComp.MemberEntries)
		externalDeaths := countBefore - countAfter

		// Emit integrity breach if external deaths detected
		if externalDeaths > 0 {
			s.world.PushEvent(event.EventCompositeIntegrityBreach, &event.CompositeIntegrityBreachPayload{
				HeaderEntity:   headerEntity,
				Behavior:       headerComp.Behavior,
				LostCount:      externalDeaths,
				RemainingCount: countAfter,
			})
		}

		// Check if composite still exists after potential destruction from event handling
		if !s.world.Components.Header.HasEntity(headerEntity) {
			continue
		}

		// Generic ablative detection via Header HP=0 for header destruction
		// Ablative composites: Header HP=0, authoritative system handles destruction
		// Non-ablative composites: Header HP>0, CompositeSystem destroys when empty
		if len(headerComp.MemberEntries) == 0 {
			if combat, ok := s.world.Components.Combat.GetComponent(headerEntity); ok && combat.HitPoints == 0 {
				s.world.Components.Header.SetComponent(headerEntity, headerComp)
				continue
			}
			s.destroyHead(headerEntity)
			continue
		}

		s.world.Components.Header.SetComponent(headerEntity, headerComp)
	}
}

func (s *CompositeSystem) countLiving(header *component.HeaderComponent) int {
	count := 0
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			count++
		}
	}
	return count
}

// destroyComposite ends a composite: its members die and its head goes. A kind
// with a body leaves the effect of what it showed, a scatter bursting its shell,
// its members dying silently, and another's members leave it one by one, as the
// death system reads them.
func (s *CompositeSystem) destroyComposite(headerEntity core.Entity, effect component.DeathEffect) {
	header, ok := s.world.Components.Header.GetPtr(headerEntity)
	if !ok {
		return
	}

	s.members = s.members[:0]
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			s.members = append(s.members, m.Entity)
		}
	}

	switch effect {
	case component.DeathFlash, component.DeathFadeout, component.DeathScatter:
		if body, ok := s.bodyOf(headerEntity, header); ok {
			s.leaveBody(effect, header.MemberEntries, &body)
			if effect == component.DeathScatter {
				s.burstShell(headerEntity, &body)
			}
			effect = component.DeathSilent
		}
	}
	event.EmitDeath(s.world.Resources.Event.Queue, effect, s.members...)

	s.destroyHead(headerEntity)
}

// destroyHead destroys the phantom head directly
func (s *CompositeSystem) destroyHead(headerEntity core.Entity) {
	s.world.DestroyEntity(headerEntity)
}

// destroyMember ends one member its owner found dead, tombstoning its entry; an
// ablative kind's cell fades out where it stood
func (s *CompositeSystem) destroyMember(headerEntity, memberEntity core.Entity) {
	if header, ok := s.world.Components.Header.GetPtr(headerEntity); ok {
		for i := range header.MemberEntries {
			if header.MemberEntries[i].Entity != memberEntity {
				continue
			}
			if header.Type == component.CompositeTypeAblative {
				if body, ok := s.bodyOf(headerEntity, header); ok {
					s.leaveBody(component.DeathFadeout, header.MemberEntries[i:i+1], &body)
				}
			}
			header.MemberEntries[i].Entity = 0
			header.Dirty = true
			break
		}
	}
	s.world.DestroyEntity(memberEntity)
}

// bodyLook is what a composite's members show at rest, read as it ends: a glyph
// table or a look's frame laid over its footprint, or one ground under every cell
type bodyLook struct {
	foot  vmath.Footprint
	glyph func(frame, row, col int) rune // a code species' table, drawn in color
	look  *component.Look                // an eye's or a data species'
	frame int
	// color is a table's glyphs', a look's glyphs' where tinted, or the ground
	color   color.RGB
	tinted  bool
	ground  bool
	console bool // a 256-color host shows a console look's pair
	shell   bodyShell
}

// bodyShell is the outline an ablative kind's glow stands on, round cells it
// loses one by one: its radii in cells and the glow's color. No radius is no shell.
type bodyShell struct {
	radiusX, radiusY float64
	color            color.RGB
}

func swarmGlyph(frame, row, col int) rune    { return visual.SwarmPatternChars[frame][row][col] }
func swarmGlyph256(frame, row, col int) rune { return visual.SwarmPatternChars256[frame][row][col] }
func quasarGlyph(_, row, col int) rune       { return visual.QuasarChars[row][col] }
func snakeHeadGlyph(_, row, col int) rune    { return visual.SnakeHeadChars[row][col] }
func snakeHeadGlyph256(_, _, _ int) rune     { return visual.SnakeHeadChar256 }

// bodyOf reads what a composite's members show. A wall's and a gold's are glyphs
// of their own, a container has none and a kraken's its renderer shapes: those
// have no body here.
func (s *CompositeSystem) bodyOf(headerEntity core.Entity, header *component.HeaderComponent) (b bodyLook, ok bool) {
	c := &s.world.Components
	b.console = s.world.Resources.Config.ColorMode == terminal.ColorMode256
	// ground is an ablative kind's cells, in the color they go critical in, and its
	// shell. A console draws no glow: its shell is the color of the cells.
	ground := func(tc color.RGB, console uint8, shell bodyShell) (bodyLook, bool) {
		b.ground, b.color, b.shell = true, tc, shell
		if b.console {
			b.color = visual.Palette256RGB(console)
			b.shell.color = b.color
		}
		return b, true
	}
	b.color = visual.RgbDrain
	if combat, ok := c.Combat.GetPtr(headerEntity); ok && combat.IsEnraged {
		b.color, b.tinted = visual.RgbCombatEnraged, true
	}

	switch header.Behavior {
	case component.BehaviorSwarm:
		swarm, ok := c.Swarm.GetPtr(headerEntity)
		if !ok {
			return b, false
		}
		b.foot, b.glyph = component.Footprints[component.SpeciesSwarm], swarmGlyph
		if b.console {
			b.glyph = swarmGlyph256
		}
		if swarm.PatternIndex > 0 && swarm.PatternIndex < parameter.SwarmPatternCount {
			b.frame = swarm.PatternIndex
		}
	case component.BehaviorQuasar:
		b.foot, b.glyph = component.Footprints[component.SpeciesQuasar], quasarGlyph
	case component.BehaviorEye:
		eye, ok := c.Eye.GetPtr(headerEntity)
		if !ok || int(eye.Type) >= parameter.EyeTypeCount {
			return b, false
		}
		b.foot, b.look = component.Footprints[component.SpeciesEye], &visual.EyeLooks[eye.Type]
		b.frame, b.tinted = eye.FrameIndex%len(b.look.Frames), false
	case component.BehaviorData:
		data, ok := c.Behavior.GetPtr(headerEntity)
		if !ok || int(data.Program) >= len(s.world.Resources.Behaviors) {
			return b, false
		}
		proto := &s.world.Resources.Behaviors[data.Program]
		b.foot, b.look, b.frame = proto.Footprint, &proto.Look, data.PatternIndex%len(proto.Look.Frames)
	case component.BehaviorSnake:
		switch {
		case c.SnakeBody.HasEntity(headerEntity):
			return ground(visual.RgbSnakeBodyBright, visual.Snake256Body, bodyShell{})
		case !c.SnakeHead.HasEntity(headerEntity):
			return b, false
		}
		b.foot, b.glyph, b.color = component.Footprints[component.SpeciesSnake], snakeHeadGlyph, visual.RgbSnakeHeadBright
		if b.console {
			b.glyph, b.color = snakeHeadGlyph256, visual.Palette256RGB(visual.Snake256Head)
		}
	case component.BehaviorStorm:
		circle, ok := c.StormCircle.GetPtr(headerEntity)
		if !ok {
			return b, false
		}
		tc := visual.StormCircleColors[circle.Index%len(visual.StormCircleColors)]
		radiusY := s.world.Resources.Config.CellAspect.Y(parameter.StormCircleRadiusY)
		return ground(tc, visual.Storm256Bodies[circle.Index%len(visual.Storm256Bodies)],
			bodyShell{parameter.StormCircleRadiusX, radiusY, tc})
	case component.BehaviorPylon:
		pylon, ok := c.Pylon.GetPtr(headerEntity)
		if !ok {
			return b, false
		}
		return ground(visual.RgbPylonCriticalBright, visual.Pylon256Critical,
			bodyShell{float64(pylon.RadiusX), float64(pylon.RadiusY), visual.RgbPylonGlow})
	case component.BehaviorTower:
		tower, ok := c.Tower.GetPtr(headerEntity)
		if !ok || int(tower.Type) >= len(visual.TowerTypes) {
			return b, false
		}
		colors := &visual.TowerTypes[tower.Type]
		return ground(colors.CriticalBright, colors.Palette256Critical,
			bodyShell{float64(tower.RadiusX), float64(tower.RadiusY), colors.GlowColor})
	default:
		return b, false
	}
	return b, true
}

// cellOf is the body's cell a member shows, its place left to the caller: false
// where it shows nothing. A look's colors are its painter's.
func (b *bodyLook) cellOf(m *component.MemberEntry) (cell component.EffectCell, ok bool) {
	if b.ground {
		return component.EffectCell{Bg: b.color, Solid: true}, true
	}
	row, col := m.OffsetY+b.foot.HeaderY, m.OffsetX+b.foot.HeaderX
	if row < 0 || row >= b.foot.Height || col < 0 || col >= b.foot.Width {
		return cell, false
	}
	if b.glyph != nil {
		cell.Rune, cell.Fg = b.glyph(b.frame, row, col), b.color
		return cell, cell.Rune != 0
	}

	at, console := &b.look.Frames[b.frame][row*b.foot.Width+col], b.console && b.look.Console
	cell.Rune, cell.Fg, cell.Solid = at.Rune, b.look.Color, at.Bg >= 0
	switch {
	case b.tinted:
		cell.Fg = b.color
	case console:
		cell.Fg = visual.Palette256RGB(b.look.Fg256)
	case at.Fg >= 0:
		cell.Fg = b.look.Fg[at.Fg]
	}
	switch {
	case !cell.Solid:
	case console:
		cell.Bg = visual.Palette256RGB(b.look.Bg256)
	default:
		cell.Bg = b.look.Bg[at.Bg]
	}
	return cell, cell.Rune != 0 || cell.Solid
}

// leaveBody sends what the members showed to the effect their end leaves, on this
// instance alone: a fadeout takes every cell whole, and a flash or a scatter the
// glyphs, the grounds under them fading out
func (s *CompositeSystem) leaveBody(effect component.DeathEffect, members []component.MemberEntry, body *bodyLook) {
	glyphs, grounds := event.EffectBatchPool.Acquire(), event.EffectBatchPool.Acquire()
	for i := range members {
		if members[i].Entity == 0 {
			continue
		}
		pos, ok := s.world.Positions.GetPosition(members[i].Entity)
		if !ok {
			continue
		}
		cell, ok := body.cellOf(&members[i])
		if !ok {
			continue
		}
		cell.X, cell.Y = pos.X, pos.Y
		if effect == component.DeathFadeout {
			grounds.Entries = append(grounds.Entries, cell)
			continue
		}
		if cell.Rune != 0 {
			glyphs.Entries = append(glyphs.Entries, component.EffectCell{X: cell.X, Y: cell.Y, Rune: cell.Rune, Fg: cell.Fg})
		}
		if cell.Solid {
			grounds.Entries = append(grounds.Entries, component.EffectCell{X: cell.X, Y: cell.Y, Bg: cell.Bg, Solid: true})
		}
	}

	glyphEvent := event.EventFlashSpawnBatchRequest
	if effect == component.DeathScatter {
		glyphEvent = event.EventSpiritScatterRequest
	}
	s.pushCells(glyphEvent, glyphs)
	s.pushCells(event.EventFadeoutSpawnBatch, grounds)
}

// pushCells hands a batch of cells to the effect an event asks for, an empty one
// back to its pool
func (s *CompositeSystem) pushCells(eventType event.EventType, cells *event.BatchPayload[component.EffectCell]) {
	if len(cells.Entries) == 0 {
		event.EffectBatchPool.Release(cells)
		return
	}
	s.world.PushLocal(eventType, cells)
}

// burstShell throws shards out from a killed body's shell, in the glow's color:
// an ablative kind has lost its last cell as it dies, and its glow goes with it
func (s *CompositeSystem) burstShell(headerEntity core.Entity, body *bodyLook) {
	center, ok := s.world.Positions.GetPosition(headerEntity)
	shell := &body.shell
	if !ok || shell.radiusX <= 0 || shell.radiusY <= 0 {
		return
	}
	glyphs := visual.DensityChars[:]
	if body.console {
		glyphs = visual.Density256Chars[:]
	}
	// The outline's length is measured with heights in the units of a width
	aspect := s.world.Resources.Config.CellAspect
	count := int(vmath.TwoPi * (shell.radiusX + aspect.Circular(shell.radiusY)) / 2 / visual.ShellShardSpacing)
	centerX, centerY := (vmath.Point{X: center.X, Y: center.Y}).Center()

	shards := event.EffectBatchPool.Acquire()
	for i := range count {
		angle := vmath.TwoPi * float64(i) / float64(count)
		at := vmath.PointAt(centerX+shell.radiusX*vmath.Cos(angle), centerY+shell.radiusY*vmath.Sin(angle))
		shards.Entries = append(shards.Entries, component.EffectCell{X: at.X, Y: at.Y, Rune: glyphs[i%len(glyphs)], Fg: shell.color})
	}
	s.pushCells(event.EventSpiritScatterRequest, shards)
}

// syncMembers updates member positions and validates liveness
func (s *CompositeSystem) syncMembers(headerComp *component.HeaderComponent, headerX, headerY int) {
	config := s.world.Resources.Config

	for i := range headerComp.MemberEntries {
		memberEntry := &headerComp.MemberEntries[i]

		// Skip tombstones
		if memberEntry.Entity == 0 {
			continue
		}

		// Liveness check: if entity no longer has position, it was destroyed
		if !s.world.Components.Member.HasEntity(memberEntry.Entity) {
			memberEntry.Entity = 0 // Tombstone
			headerComp.Dirty = true
			continue
		}

		// Owner system manages member positions
		if headerComp.SkipPositionSync {
			continue
		}

		// Propagate offset
		newX := headerX + int(memberEntry.OffsetX)
		newY := headerY + int(memberEntry.OffsetY)

		// Bounds check - destroy before tombstoning
		if newX < 0 || newX >= config.MapWidth || newY < 0 || newY >= config.MapHeight {
			s.world.DestroyEntity(memberEntry.Entity)
			memberEntry.Entity = 0
			headerComp.Dirty = true
			continue
		}

		s.world.Positions.SetPosition(memberEntry.Entity, component.PositionComponent{
			X: newX,
			Y: newY,
		})
	}
}

// compactMembers removes tombstones via swap-remove
func (s *CompositeSystem) compactMembers(headerComp *component.HeaderComponent) {
	write := 0
	for read := range len(headerComp.MemberEntries) {
		if headerComp.MemberEntries[read].Entity != 0 {
			if write != read {
				headerComp.MemberEntries[write] = headerComp.MemberEntries[read]
			}
			write++
		}
	}
	headerComp.MemberEntries = headerComp.MemberEntries[:write]
}
