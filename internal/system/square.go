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
	"github.com/lixenwraith/vif/pkg/vmath"
)

// SquareSystem keeps a magic square: nine blocks a player selects by digit and turns
// a row or column at a time (squareTurns), under the shield of the cursor standing
// on it, which it sizes to cover them. A line holding three blocks of a
// kind fires the kind's effect (component.SquareKinds) as that cursor's each time
// its cooldown runs out.
type SquareSystem struct {
	world *engine.World
	faced []core.Entity // the gateways a fire's line faces, reused
	toggle
}

// squareTurns are the keys that turn the selected block's line a place: its row
// left and right on h and l, its column down and up on j and k, and for the other
// hand a and f, d and s
var squareTurns = [...]struct {
	key    rune
	column bool
	step   int
}{
	{'h', false, -1}, {'j', true, 1}, {'k', true, -1}, {'l', false, 1},
	{'a', false, -1}, {'d', true, 1}, {'s', true, -1}, {'f', false, 1},
}

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
		s.cover(sq)
		for kind := range sq.Effects {
			effect := &sq.Effects[kind]
			effect.Active, effect.Cooldown = max(effect.Active-dt, 0), max(effect.Cooldown-dt, 0)
		}
		s.trigger(e, sq)
		return true
	})
}

// spawn lays the square whole on the map, centered as near the asked cell as that
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
	sq := component.SquareComponent{Width: p.BlockWidth, Height: p.BlockHeight, Kinds: kinds}
	if sq.Width <= 0 {
		sq.Width = parameter.SquareBlockWidth
	}
	if sq.Height <= 0 {
		sq.Height = max(1, cfg.CellAspect.YInt(parameter.SquareBlockHeight))
	}
	w, h := sq.Size()
	if w > cfg.MapWidth || h > cfg.MapHeight {
		return
	}
	sq.X = min(max(p.X-w/2, 0), cfg.MapWidth-w)
	sq.Y = min(max(p.Y-h/2, 0), cfg.MapHeight-h)
	for place := range sq.Digits {
		sq.Digits[place] = p.Layout[place] - '0'
	}
	sq.Lines = squareLines(&sq)
	s.world.Components.Square.SetComponent(s.world.CreateEntity(core.DomainShared), sq)
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

// owner is the cursor the square fights through and whose shield covers it: the
// first on the roster standing on the square, 0 when none does
func (s *SquareSystem) owner(sq *component.SquareComponent) core.Entity {
	w, h := sq.Size()
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		if at, ok := s.world.Positions.GetPosition(cursor); ok &&
			at.X >= sq.X && at.X < sq.X+w && at.Y >= sq.Y && at.Y < sq.Y+h {
			return cursor
		}
	}
	return 0
}

// cover has its cursor's shield reach past the square's corners and hold its boost
// to parameter.SquareBoostCap, on the instance that simulates the cursor (D-13)
func (s *SquareSystem) cover(sq *component.SquareComponent) {
	shield, ok := s.world.Components.Shield.GetPtr(s.world.ResolveOwnedCursor(s.owner(sq)))
	if !ok {
		return
	}
	shield.BoostCap = parameter.SquareBoostCap
	if want := sq.ShieldRadius(s.world.Resources.Config.CellAspect); shield.Radius != want {
		shield.Radius = want
		s.world.UpdateBoundsRadius()
	}
}

// press selects the block a digit names for the pressing slot, or turns the line of
// the block it has selected
func (s *SquareSystem) press(e core.Entity, sq *component.SquareComponent, slot uint8, key rune) {
	if int(slot) >= len(sq.Selected) {
		return
	}
	if key >= '1' && key <= '9' {
		sq.Selected[slot] = uint8(key - '0')
		return
	}
	for _, t := range squareTurns {
		if t.key == key {
			s.turn(e, sq, sq.Selected[slot], t.column, t.step)
		}
	}
}

// turn moves every block of a digit's row or column step places along it, the last
// round to the first, and fires each ready kind a line now holds three of
func (s *SquareSystem) turn(e core.Entity, sq *component.SquareComponent, digit uint8, column bool, step int) {
	place := slices.Index(sq.Digits[:], digit)
	if digit == 0 || place < 0 {
		return
	}
	line := place / component.SquareSide
	if column {
		line = component.SquareSide + place%component.SquareSide
	}
	places := component.LinePlaces(line)
	var was [component.SquareSide]uint8
	for i, p := range places {
		was[i] = sq.Digits[p]
	}
	for i, d := range was {
		sq.Digits[places[(i+step+component.SquareSide)%component.SquareSide]] = d
	}
	sq.Lines = squareLines(sq)
	s.trigger(e, sq)
}

// squareLines reads the kind each line holds three of
func squareLines(sq *component.SquareComponent) (lines [component.SquareLines]uint8) {
	for l := range lines {
		places := component.LinePlaces(l)
		kind := sq.KindAt(places[0])
		if kind < component.SquareKindCount && sq.KindAt(places[1]) == kind && sq.KindAt(places[2]) == kind {
			lines[l] = uint8(kind) + 1
		}
	}
	return lines
}

// trigger fires each kind a line holds three of whose cooldown has run out
func (s *SquareSystem) trigger(e core.Entity, sq *component.SquareComponent) {
	for l, kind := range sq.Lines {
		if kind != 0 && sq.Effects[kind-1].Ready() {
			s.fire(e, sq, component.SquareKind(kind-1), l)
		}
	}
}

// fire runs a kind's effect from line as its cursor's: a boost of the shield, as
// many rewards as its hold is of a kill's, any other kind on the gateways the line
// faces and the bodies they sent
func (s *SquareSystem) fire(e core.Entity, sq *component.SquareComponent, kind component.SquareKind, line int) {
	spec := &component.SquareKinds[kind]
	sq.Effects[kind] = component.SquareEffect{Active: spec.Duration, Cooldown: spec.Cooldown}
	cursor := s.owner(sq)
	if !spec.Boost {
		s.aim(e, sq, cursor, spec, line)
	} else if cursor != 0 {
		for range int(spec.Duration / parameter.BoostBaseDuration) {
			s.world.PushLocal(event.EventBoostReward, &event.BoostRewardPayload{Entity: cursor})
		}
	}
	s.world.PushEvent(event.EventSquareAligned, &event.SquareAlignedPayload{Kind: kind, Line: line})
}

// aim holds each gateway line faces if the kind halts them, and lands the kind's
// family on every swarm and data species such a gateway sent, a bolt flying to each
// from the line's orb nearest it
func (s *SquareSystem) aim(e core.Entity, sq *component.SquareComponent, cursor core.Entity, spec *component.SquareKindSpec, line int) {
	orbs, n := sq.Orbs(line, s.world.Resources.Config.CellAspect)
	from := func(x, y int) vmath.Point {
		if n > 1 && vmath.IntAbs(x-orbs[1].X)+2*vmath.IntAbs(y-orbs[1].Y) < vmath.IntAbs(x-orbs[0].X)+2*vmath.IntAbs(y-orbs[0].Y) {
			return orbs[1]
		}
		return orbs[0]
	}
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
			s.bolt(e, from(at.X, at.Y), at.X, at.Y, spec.Bolt)
		}
		return true
	})
	strike := func(body, gateway core.Entity) {
		at, ok := s.world.Positions.GetPosition(body)
		if !ok || !slices.Contains(s.faced, gateway) {
			return
		}
		if orb := from(at.X, at.Y); s.hit(spec, cursor, orb, body) {
			s.bolt(e, orb, at.X, at.Y, spec.Bolt)
		}
	}
	s.world.Components.Swarm.Each(func(swarm core.Entity, sw *component.SwarmComponent) bool {
		strike(swarm, sw.Gateway)
		return true
	})
	s.world.Components.Behavior.Each(func(body core.Entity, b *component.BehaviorComponent) bool {
		strike(body, b.Gateway)
		return true
	})
}

// hit lands a kind's family whole on target as cursor's, from an orb's cell, if the
// combat matrix lets a cursor strike it with it
func (s *SquareSystem) hit(spec *component.SquareKindSpec, cursor core.Entity, from vmath.Point, target core.Entity) bool {
	c, ok := s.world.Components.Combat.GetPtr(target)
	if !ok || cursor == 0 || profile.Attack(spec.Attack, component.CombatEntityCursor, c.CombatEntityType) == nil {
		return false
	}
	s.world.PushLocal(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
		AttackType:   spec.Attack,
		OwnerEntity:  cursor,
		OriginEntity: cursor,
		TargetEntity: target,
		HasOrigin:    true,
		OriginX:      from.X,
		OriginY:      from.Y,
	})
	return true
}

// bolt flies a fire's bolt from an orb's cell to another
func (s *SquareSystem) bolt(owner core.Entity, from vmath.Point, x, y int, tint component.LightningColorType) {
	s.world.PushLocal(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
		Owner:     owner,
		OriginX:   from.X,
		OriginY:   from.Y,
		TargetX:   x,
		TargetY:   y,
		Duration:  parameter.SquareBoltDuration,
		ColorType: tint,
	})
}
