package system

import (
	"slices"
	"time"
	"unicode/utf8"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/position"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// SquareSystem keeps a magic square: nine ablative blocks a player selects by digit
// and turns a row or column at a time with h, j, k and l. A line holding three
// blocks of a kind fires the kind's effect (component.SquareKinds) each time its
// cooldown runs out, a swarm wears the cells it covers, and the square falls with
// its last block.
type SquareSystem struct {
	world    *engine.World
	contacts []squareContact // one block's this tick, reused
	faced    []core.Entity   // the gateways a fire's line faces, reused
	toggle
}

// squareContact is a body touching a block and the block's cells it covers
type squareContact struct {
	body  core.Entity
	cells []core.Entity
}

// squareTurns are the keys that turn the selected block's line a place: its row
// left and right, its column up and down
var squareTurns = [...]struct {
	key    rune
	column bool
	step   int
}{{'h', false, -1}, {'l', false, 1}, {'k', true, -1}, {'j', true, 1}}

func NewSquareSystem(world *engine.World) engine.System {
	return &SquareSystem{world: world}
}

func (s *SquareSystem) Init() {
	s.enabled = true
}

func (s *SquareSystem) Name() string {
	return "square"
}

func (s *SquareSystem) Priority() int {
	return parameter.PrioritySquare
}

func (s *SquareSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSquareSpawnRequest,
		event.EventInputPressed,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *SquareSystem) HandleEvent(ev event.GameEvent) {
	if p, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
		if p.SystemName == s.Name() {
			s.enabled = p.Enabled
		}
		return
	}
	if !s.enabled {
		return
	}
	switch p := ev.Payload.(type) {
	case *event.SquareSpawnRequestPayload:
		s.spawn(p)
	case *event.InputPayload:
		key, _ := utf8.DecodeRuneInString(p.Key)
		s.world.Components.Square.Each(func(_ core.Entity, sq *component.SquareComponent) bool {
			s.press(sq, p.Slot, key)
			return true
		})
	}
}

func (s *SquareSystem) Update() {
	if !s.enabled {
		return
	}
	dt := s.world.Resources.Time.DeltaTime
	s.world.Components.Square.Each(func(e core.Entity, sq *component.SquareComponent) bool {
		s.age(e, sq, dt)
		s.trigger(sq)
		s.wear(sq)
		s.fall(sq)
		s.anchor(sq)
		return true
	})
}

// spawn lays the square whole on the map, centred as near the asked cell as that
// allows; a second square, a map too small for it, or a layout and groups that do
// not each name the nine digits once, is refused
func (s *SquareSystem) spawn(p *event.SquareSpawnRequestPayload) {
	if s.world.Components.Square.CountEntities() > 0 {
		return
	}
	kinds, ok := squareKinds(p.Layout, p.Groups)
	if !ok {
		return
	}
	cfg := s.world.Resources.Config
	sq := component.SquareComponent{Width: p.BlockWidth, Height: p.BlockHeight, Group: p.TargetGroupID}
	if sq.Width <= 0 {
		sq.Width = parameter.SquareBlockWidth
	}
	if sq.Height <= 0 {
		sq.Height = max(1, cfg.CellAspect.YInt(parameter.SquareBlockHeight))
	}
	hp := p.HP
	if hp <= 0 {
		hp = parameter.SquareBlockHP
	}
	w, h := sq.Size()
	if w > cfg.MapWidth || h > cfg.MapHeight {
		return
	}
	sq.X = min(max(p.X-w/2, 0), cfg.MapWidth-w)
	sq.Y = min(max(p.Y-h/2, 0), cfg.MapHeight-h)

	square := s.world.CreateEntity(core.DomainShared)
	for place := range sq.Places {
		digit := p.Layout[place] - '0'
		sq.Places[place] = s.block(square, &sq, place, digit, kinds[digit-1], hp)
	}
	sq.Lines = s.lines(&sq)
	s.world.Components.Square.SetComponent(square, sq)
}

// squareKinds reads each digit's kind, false unless layout holds the nine digits
// once each and byKind, the digits of each kind in order, names each digit once
func squareKinds(layout string, byKind []string) (kinds [component.SquareBlocks]component.SquareKind, ok bool) {
	if len(layout) != component.SquareBlocks || len(byKind) != int(component.SquareKindCount) {
		return kinds, false
	}
	var seen [component.SquareBlocks + 1]int
	for i := range layout {
		if layout[i] < '1' || layout[i] > '9' {
			return kinds, false
		}
		seen[layout[i]-'0']++
	}
	for kind, digits := range byKind {
		for i := range digits {
			if digits[i] < '1' || digits[i] > '9' {
				return kinds, false
			}
			seen[digits[i]-'0'] += component.SquareBlocks
			kinds[digits[i]-'1'] = component.SquareKind(kind)
		}
	}
	for d := 1; d <= component.SquareBlocks; d++ {
		if seen[d] != component.SquareBlocks+1 {
			return kinds, false
		}
	}
	return kinds, true
}

// block lays the block at place p: an ablative composite of cells with hp each, its
// header at the block's middle
func (s *SquareSystem) block(square core.Entity, sq *component.SquareComponent, p int, digit uint8, kind component.SquareKind, hp int) core.Entity {
	x, y := sq.Place(p)
	hx, hy := x+sq.Width/2, y+sq.Height/2
	header := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(header, component.PositionComponent{X: hx, Y: hy})
	s.world.Components.Protection.SetComponent(header, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})
	// Ablative: the header holds no hit points, its cells do
	s.world.Components.Combat.SetComponent(header, component.CombatComponent{
		OwnerEntity:      header,
		CombatEntityType: component.CombatEntityTower,
	})

	members := make([]component.MemberEntry, 0, sq.Width*sq.Height)
	for row := range sq.Height {
		for col := range sq.Width {
			cell := s.world.CreateEntity(core.DomainShared)
			s.world.Positions.SetPosition(cell, component.PositionComponent{X: x + col, Y: y + row})
			s.world.Components.Protection.SetComponent(cell, component.ProtectionComponent{
				Mask: component.ProtectFromParticle | component.ProtectFromSpecies,
			})
			s.world.Components.Combat.SetComponent(cell, component.CombatComponent{
				OwnerEntity:      header,
				CombatEntityType: component.CombatEntityTower,
				HitPoints:        hp,
			})
			s.world.Components.Member.SetComponent(cell, component.MemberComponent{HeaderEntity: header})
			members = append(members, component.MemberEntry{Entity: cell, OffsetX: x + col - hx, OffsetY: y + row - hy})
		}
	}
	s.world.Components.Header.SetComponent(header, component.HeaderComponent{
		Behavior:         component.BehaviorBlock,
		Type:             component.CompositeTypeAblative,
		MemberEntries:    members,
		SkipPositionSync: true, // the square moves its cells as it turns
	})
	s.world.Components.Block.SetComponent(header, component.BlockComponent{
		Square: square,
		Digit:  digit,
		Kind:   kind,
		HP:     hp,
	})
	return header
}

// press selects the block a digit names for the pressing slot, or turns its
// selected block's line
func (s *SquareSystem) press(sq *component.SquareComponent, slot uint8, key rune) {
	if int(slot) >= len(sq.Selected) {
		return
	}
	if key >= '1' && key <= '9' {
		for _, block := range sq.Places {
			if b, ok := s.world.Components.Block.GetPtr(block); ok && rune(b.Digit) == key-'0' {
				sq.Selected[slot] = block
			}
		}
		return
	}
	for _, t := range squareTurns {
		if t.key == key {
			s.turn(sq, sq.Selected[slot], t.column, t.step)
		}
	}
}

// turn moves every block of selected's row or column step places along it, the
// last round to the first, and fires each ready kind a line now holds three of
func (s *SquareSystem) turn(sq *component.SquareComponent, selected core.Entity, column bool, step int) {
	place := -1
	for p, block := range sq.Places {
		if block == selected && block != 0 {
			place = p
		}
	}
	if place < 0 {
		return
	}
	line := place / component.SquareSide
	if column {
		line = component.SquareSide + place%component.SquareSide
	}
	places := component.LinePlaces(line)
	var was [component.SquareSide]core.Entity
	for i, p := range places {
		was[i] = sq.Places[p]
	}
	for i, block := range was {
		p := places[(i+step+component.SquareSide)%component.SquareSide]
		sq.Places[p] = block
		s.move(sq, block, p)
	}

	sq.Lines = s.lines(sq)
	s.trigger(sq)
}

// move stands a block's header and cells at place p
func (s *SquareSystem) move(sq *component.SquareComponent, block core.Entity, p int) {
	x, y := sq.Place(p)
	hx, hy := x+sq.Width/2, y+sq.Height/2
	s.world.Positions.SetPosition(block, component.PositionComponent{X: hx, Y: hy})
	header, ok := s.world.Components.Header.GetPtr(block)
	if !ok {
		return
	}
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			s.world.Positions.SetPosition(m.Entity, component.PositionComponent{X: hx + m.OffsetX, Y: hy + m.OffsetY})
		}
	}
}

// lines reads the kind each line holds three of, a fallen block's counting as a
// standing one's
func (s *SquareSystem) lines(sq *component.SquareComponent) (lines [component.SquareLines]uint8) {
	for l := range lines {
		var kinds [component.SquareSide]component.SquareKind
		whole := true
		for i, p := range component.LinePlaces(l) {
			b, ok := s.world.Components.Block.GetPtr(sq.Places[p])
			if !ok {
				whole = false
				break
			}
			kinds[i] = b.Kind
		}
		for _, k := range kinds[1:] {
			whole = whole && k == kinds[0]
		}
		if whole {
			lines[l] = uint8(kinds[0]) + 1
		}
	}
	return lines
}

// trigger fires each kind a line holds three of whose cooldown has run out
func (s *SquareSystem) trigger(sq *component.SquareComponent) {
	for l, kind := range sq.Lines {
		if kind != 0 && sq.Effects[kind-1].Ready() {
			s.fire(sq, component.SquareKind(kind-1), l)
		}
	}
}

// fire runs a kind's effect from line: a shield over the whole square, any other
// kind on the gateways the line faces and the swarms they sent
func (s *SquareSystem) fire(sq *component.SquareComponent, kind component.SquareKind, line int) {
	spec := &component.SquareKinds[kind]
	sq.Effects[kind] = component.SquareEffect{Active: spec.Duration, Cooldown: spec.Cooldown}
	if spec.Shield {
		s.ward(sq, spec.Duration)
	} else {
		s.aim(sq, spec, line)
	}
	s.world.PushEvent(event.EventSquareAligned, &event.SquareAlignedPayload{Kind: kind, Line: line})
}

// aim holds each gateway line faces if the kind halts them and lands the kind's
// family whole on every swarm such a gateway sent, a bolt flying to each gateway
// and swarm from the line's block nearest it
func (s *SquareSystem) aim(sq *component.SquareComponent, spec *component.SquareKindSpec, line int) {
	s.faced = s.faced[:0]
	s.world.Components.Gateway.Each(func(gw core.Entity, g *component.GatewayComponent) bool {
		at, ok := s.world.Positions.GetPosition(g.AnchorEntity)
		if !ok || !sq.Faces(line, at.X, at.Y) {
			return true
		}
		s.faced = append(s.faced, gw)
		if spec.Halt {
			s.world.PushEvent(event.EventGatewayHoldRequest, &event.GatewayHoldRequestPayload{
				DurationMs:   int(spec.Duration / time.Millisecond),
				AnchorEntity: g.AnchorEntity,
			})
		}
		block, bx, by := s.nearest(sq, line, at.X, at.Y)
		s.bolt(block, bx, by, at.X, at.Y, spec.Bolt)
		return true
	})
	s.world.Components.Swarm.Each(func(swarm core.Entity, sw *component.SwarmComponent) bool {
		at, ok := s.world.Positions.GetPosition(swarm)
		if !ok || !slices.Contains(s.faced, sw.Gateway) {
			return true
		}
		block, bx, by := s.nearest(sq, line, at.X, at.Y)
		if s.hit(spec, block, bx, by, swarm, nil) {
			s.bolt(block, bx, by, at.X, at.Y, spec.Bolt)
		}
		return true
	})
}

// nearest is the block along line l nearest a cell, rows counting double as they
// stand on screen, and the block's own cell nearest it, clear of its digit
func (s *SquareSystem) nearest(sq *component.SquareComponent, l, x, y int) (block core.Entity, bx, by int) {
	best := -1
	for _, p := range component.LinePlaces(l) {
		px, py := sq.Place(p)
		cx, cy := min(max(x, px), px+sq.Width-1), min(max(y, py), py+sq.Height-1)
		if d := (x-cx)*(x-cx) + 4*(y-cy)*(y-cy); best < 0 || d < best {
			block, bx, by, best = sq.Places[p], cx, cy, d
		}
	}
	return block, bx, by
}

// hit lands a kind's family from origin, standing at (x, y), on target's hits, all
// of it when hits is nil, if the combat matrix lets a tower strike it with it
func (s *SquareSystem) hit(spec *component.SquareKindSpec, origin core.Entity, x, y int, target core.Entity, hits []core.Entity) bool {
	c, ok := s.world.Components.Combat.GetPtr(target)
	if !ok || profile.Attack(spec.Attack, component.CombatEntityTower, c.CombatEntityType) == nil {
		return false
	}
	s.world.PushLocal(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
		AttackType:   spec.Attack,
		OwnerEntity:  origin,
		OriginEntity: origin,
		TargetEntity: target,
		HitEntities:  hits,
		HasOrigin:    true,
		OriginX:      x,
		OriginY:      y,
	})
	return true
}

// bolt flies a fire's bolt from a block's cell to another
func (s *SquareSystem) bolt(block core.Entity, bx, by, x, y int, tint component.LightningColorType) {
	s.world.PushLocal(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
		Owner:     block,
		OriginX:   bx,
		OriginY:   by,
		TargetX:   x,
		TargetY:   y,
		Duration:  parameter.SquareBoltDuration,
		ColorType: tint,
	})
}

// ward has every standing cell refuse each hit for d
func (s *SquareSystem) ward(sq *component.SquareComponent, d time.Duration) {
	for _, block := range sq.Places {
		header, ok := s.world.Components.Header.GetPtr(block)
		if !ok {
			continue
		}
		for _, m := range header.MemberEntries {
			if c, ok := s.world.Components.Combat.GetPtr(m.Entity); ok && m.Entity != 0 {
				c.Status[component.StatusWard] = component.Status{Remaining: d}
			}
		}
	}
}

// age runs each kind's hold and cooldown down; a shield lands its push on what it
// covers each tick it holds
func (s *SquareSystem) age(e core.Entity, sq *component.SquareComponent, dt time.Duration) {
	for kind := range sq.Effects {
		effect, spec := &sq.Effects[kind], &component.SquareKinds[kind]
		if effect.Active > 0 && spec.Shield {
			s.shove(e, sq, spec)
		}
		effect.Active = max(effect.Active-dt, 0)
		effect.Cooldown = max(effect.Cooldown-dt, 0)
	}
}

// shove lands the shield's push from the square's middle on the members of each
// target the shield covers: an ellipse through the square's corners, a margin wider
func (s *SquareSystem) shove(e core.Entity, sq *component.SquareComponent, spec *component.SquareKindSpec) {
	w, h := sq.Size()
	cx, cy := sq.X+w/2, sq.Y+h/2
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(sq.ShieldRadii(s.world.Resources.Config.CellAspect))
	for _, t := range FindTargetsInEllipse(s.world, cx, cy, invRxSq, invRySq, position.ScopeShared, e) {
		s.hit(spec, sq.Places[component.SquareBlocks/2], cx, cy, t.Target, t.Members)
	}
}

// wear lands each body's touch on the cells of a block it covers, a contact a body
// and a block, where the combat matrix gives the body's touch on a tower
func (s *SquareSystem) wear(sq *component.SquareComponent) {
	for _, block := range sq.Places {
		header, ok := s.world.Components.Header.GetPtr(block)
		if !ok {
			continue
		}
		s.contacts = s.contacts[:0]
		for _, m := range header.MemberEntries {
			pos, ok := s.world.Positions.GetPosition(m.Entity)
			if m.Entity == 0 || !ok {
				continue
			}
			var occupants [parameter.MaxEntitiesPerCell]core.Entity
			n := copy(occupants[:], s.world.Positions.EntitiesAt(vmath.Point{X: pos.X, Y: pos.Y}, position.ScopeShared))
			for _, o := range occupants[:n] {
				body, _, ok := ResolveTargetFromEntity(s.world, o, block)
				if !ok || body == block {
					continue
				}
				c, ok := s.world.Components.Combat.GetPtr(body)
				if !ok || profile.Attack(component.CombatAttackContact, c.CombatEntityType, component.CombatEntityTower) == nil {
					continue
				}
				s.touch(body, m.Entity)
			}
		}
		for _, c := range s.contacts {
			s.world.PushLocal(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
				AttackType:   component.CombatAttackContact,
				OwnerEntity:  c.body,
				OriginEntity: c.body,
				TargetEntity: block,
				HitEntities:  c.cells,
			})
		}
	}
}

// touch notes that body covers cell, once however many of its parts stand there
func (s *SquareSystem) touch(body, cell core.Entity) {
	for i := range s.contacts {
		if c := &s.contacts[i]; c.body == body {
			if c.cells[len(c.cells)-1] != cell {
				c.cells = append(c.cells, cell)
			}
			return
		}
	}
	s.contacts = append(s.contacts, squareContact{body: body, cells: []core.Entity{cell}})
}

// fall ends each cell its hit points left and marks a block fallen as its last
// cell goes, counting down the blocks still standing
func (s *SquareSystem) fall(sq *component.SquareComponent) {
	standing := 0
	for _, block := range sq.Places {
		if b, ok := s.world.Components.Block.GetPtr(block); ok && !b.Fallen {
			standing++
		}
	}
	for _, block := range sq.Places {
		b, ok := s.world.Components.Block.GetPtr(block)
		header, hok := s.world.Components.Header.GetPtr(block)
		if !ok || !hok || b.Fallen {
			continue
		}
		living := 0
		for _, m := range header.MemberEntries {
			c, ok := s.world.Components.Combat.GetPtr(m.Entity)
			switch {
			case m.Entity == 0 || !ok:
			case c.HitPoints > 0:
				living++
			default:
				s.world.PushEvent(event.EventCompositeMemberDestroyed, &event.CompositeMemberDestroyedPayload{
					HeaderEntity: block,
					MemberEntity: m.Entity,
				})
			}
		}
		if living == 0 {
			b.Fallen = true
			standing--
			s.world.PushEvent(event.EventSquareBlockFallen, &event.SquareBlockFallenPayload{
				Digit:     int(b.Digit),
				Remaining: standing,
			})
		}
	}
}

// anchor gives each standing block's cell nearest its middle the square's target
// anchor, so a swarm wears a block down to its last cell. A group holds
// parameter.MaxTargetsPerGroup anchors: while all nine stand, the block at the
// middle place, which the other eight ring, holds none.
func (s *SquareSystem) anchor(sq *component.SquareComponent) {
	standing := 0
	for _, block := range sq.Places {
		if b, ok := s.world.Components.Block.GetPtr(block); ok && !b.Fallen {
			standing++
		}
	}
	for p, block := range sq.Places {
		b, ok := s.world.Components.Block.GetPtr(block)
		if !ok {
			continue
		}
		var want core.Entity
		if sq.Group != 0 && !b.Fallen && (standing < component.SquareBlocks || p != component.SquareBlocks/2) {
			want = s.core(block)
		}
		if want == b.Anchor {
			continue
		}
		if b.Anchor != 0 {
			s.world.Components.TargetAnchor.RemoveEntity(b.Anchor)
		}
		if want != 0 {
			s.world.Components.TargetAnchor.SetComponent(want, component.TargetAnchorComponent{GroupID: sq.Group})
		}
		b.Anchor = want
	}
}

// core is a block's living cell nearest its middle, rows counting double as they
// stand on screen; the first such in the block's order
func (s *SquareSystem) core(block core.Entity) core.Entity {
	header, ok := s.world.Components.Header.GetPtr(block)
	if !ok {
		return 0
	}
	var best core.Entity
	bestDist := -1
	for _, m := range header.MemberEntries {
		c, ok := s.world.Components.Combat.GetPtr(m.Entity)
		if m.Entity == 0 || !ok || c.HitPoints <= 0 {
			continue
		}
		if d := m.OffsetX*m.OffsetX + 4*m.OffsetY*m.OffsetY; bestDist < 0 || d < bestDist {
			best, bestDist = m.Entity, d
		}
	}
	return best
}
