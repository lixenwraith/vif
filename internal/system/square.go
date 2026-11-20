package system

import (
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
// and turns a row or column at a time with h, j, k and l. A line that comes to hold
// three blocks of a kind fires the kind's effect (component.SquareKinds), a swarm
// wears the cells it covers, and the square falls with its last block.
type SquareSystem struct {
	world    *engine.World
	contacts []squareContact // one block's this tick, reused
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
		s.world.Components.Square.Each(func(e core.Entity, sq *component.SquareComponent) bool {
			s.press(e, sq, p.Slot, key)
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
func (s *SquareSystem) press(e core.Entity, sq *component.SquareComponent, slot uint8, key rune) {
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
			s.turn(e, sq, sq.Selected[slot], t.column, t.step)
		}
	}
}

// turn moves every block of selected's row or column step places along it, the
// last round to the first, and fires each kind a line comes to hold three of
func (s *SquareSystem) turn(e core.Entity, sq *component.SquareComponent, selected core.Entity, column bool, step int) {
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

	before := sq.Lines
	sq.Lines = s.lines(sq)
	for l, kind := range sq.Lines {
		if kind != 0 && kind != before[l] {
			s.fire(e, sq, component.SquareKind(kind-1), l)
		}
	}
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

// fire runs a kind's effect from line, unless it holds or recharges still
func (s *SquareSystem) fire(e core.Entity, sq *component.SquareComponent, kind component.SquareKind, line int) {
	effect := &sq.Effects[kind]
	if !effect.Ready() {
		return
	}
	spec := &component.SquareKinds[kind]
	effect.Active = spec.Duration
	if spec.Duration <= 0 {
		effect.Recharge = spec.Recharge
	}
	if spec.Shield {
		s.ward(sq, spec.Duration)
	} else {
		origin := sq.Places[component.LinePlaces(line)[1]]
		at, _ := s.world.Positions.GetPosition(origin)
		s.land(e, spec, origin, at.X, at.Y, nil)
	}
	if spec.Halt {
		s.world.PushEvent(event.EventGatewayHoldRequest, &event.GatewayHoldRequestPayload{
			DurationMs: int(spec.Duration / time.Millisecond),
		})
	}
	s.world.PushEvent(event.EventSquareAligned, &event.SquareAlignedPayload{Kind: kind, Line: line})
}

// land lands a kind's family from origin, a block, on every target inside covers,
// every one on the map when covers is nil, that the combat matrix lets a tower
// strike with it
func (s *SquareSystem) land(e core.Entity, spec *component.SquareKindSpec, origin core.Entity, x, y int, covers func(x, y int) bool) {
	inside := covers
	if inside == nil {
		inside = func(int, int) bool { return true }
	}
	for _, t := range FindTargetsIn(s.world, inside, position.ScopeShared, e) {
		c, ok := s.world.Components.Combat.GetPtr(t.Target)
		if !ok || profile.Attack(spec.Attack, component.CombatEntityTower, c.CombatEntityType) == nil {
			continue
		}
		hits := t.Members
		if covers == nil {
			hits = nil // the whole body, as one hit
		}
		s.world.PushLocal(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   spec.Attack,
			OwnerEntity:  origin,
			OriginEntity: origin,
			TargetEntity: t.Target,
			HitEntities:  hits,
			HasOrigin:    true,
			OriginX:      x,
			OriginY:      y,
		})
		if to, ok := s.world.Positions.GetPosition(t.Target); ok && spec.Bolt {
			s.world.PushLocal(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
				Owner:     origin,
				OriginX:   x,
				OriginY:   y,
				TargetX:   to.X,
				TargetY:   to.Y,
				Duration:  parameter.SquareBoltDuration,
				ColorType: component.LightningRed,
			})
		}
	}
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

// age runs each kind's effect down and then its recharge; a shield lands its push
// on what it covers each tick it holds
func (s *SquareSystem) age(e core.Entity, sq *component.SquareComponent, dt time.Duration) {
	for kind := range sq.Effects {
		effect, spec := &sq.Effects[kind], &component.SquareKinds[kind]
		if effect.Active <= 0 {
			effect.Recharge = max(effect.Recharge-dt, 0)
			continue
		}
		if spec.Shield {
			cx, cy, invRxSq, invRySq := s.shield(sq)
			s.land(e, spec, sq.Places[component.SquareBlocks/2], cx, cy, func(x, y int) bool {
				return vmath.EllipseContainsPoint(x, y, cx, cy, invRxSq, invRySq)
			})
		}
		if effect.Active -= dt; effect.Active <= 0 {
			effect.Active, effect.Recharge = 0, spec.Recharge
		}
	}
}

// shield is the square's shield: an ellipse through its corners, a margin wider
func (s *SquareSystem) shield(sq *component.SquareComponent) (cx, cy int, invRxSq, invRySq float64) {
	w, h := sq.Size()
	cx, cy = sq.X+w/2, sq.Y+h/2
	invRxSq, invRySq = vmath.EllipseInvRadiiSq(sq.ShieldRadii(s.world.Resources.Config.CellAspect))
	return cx, cy, invRxSq, invRySq
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
