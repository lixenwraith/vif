package system

import (
	"math/bits"
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

// SquareSystem keeps a magic square: nine blocks a player selects by digit and turns
// a row or column at a time (squareTurns), under the shield of the cursor standing
// on it, which it sizes to cover them. A line holding three blocks of a kind fires
// the kind's effect (component.SquareKinds) as that cursor's each time its cooldown
// runs out, the middle row and column only a kind with no side to strike.
type SquareSystem struct {
	world *engine.World
	on    []core.Entity // a body's members on the blocks, reused
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
		event.EventSpeciesKilled,
		event.EventEnergyCrossedZero,
		event.EventSquareShieldFell,
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
	case *event.SpeciesKilledPayload:
		// A kill its cursor made charges the square's dust
		s.world.Components.Square.Each(func(_ core.Entity, sq *component.SquareComponent) bool {
			if p.KillerEntity != 0 && p.KillerEntity == s.owner(sq) {
				sq.Dust = min(sq.Dust+parameter.SquareDustKill, parameter.SquareDustFull)
			}
			return true
		})
	case *event.EnergyCrossedZeroPayload:
		// Its own cursor's shield falling with its energy is every instance's to hear
		cursor := s.world.ResolveOwnedCursor(p.Entity)
		shield, ok := s.world.Components.Shield.GetPtr(cursor)
		s.world.Components.Square.Each(func(_ core.Entity, sq *component.SquareComponent) bool {
			if ok && !shield.Active && s.owner(sq) == cursor {
				s.world.PushCrossing(event.EventSquareShieldFell, &event.SquareHitPayload{Entity: cursor})
			}
			return true
		})
	case *event.SquareHitPayload:
		// The shield over it fell: the square sheds its magic
		s.world.Components.Square.Each(func(_ core.Entity, sq *component.SquareComponent) bool {
			if p.Entity != 0 && p.Entity == s.owner(sq) {
				s.shed(sq)
			}
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
		sq.Dust = min(sq.Dust+dt, parameter.SquareDustFull)
		for kind := range sq.Effects {
			effect := &sq.Effects[kind]
			effect.Active, effect.Cooldown = max(effect.Active-dt, 0), max(effect.Cooldown-dt, 0)
		}
		s.trigger(e, sq)
		s.spell(sq, dt)
		s.orbStrike(sq)
		return true
	})
}

// orbStrike lands each striking kind's family from its orb, as the square's
// cursor's, on every body with a member within the orb's reach
func (s *SquareSystem) orbStrike(sq *component.SquareComponent) {
	cfg := s.world.Resources.Config
	cursor := s.owner(sq)
	invRx, invRy := vmath.EllipseInvRadiiSq(parameter.SquareOrbReach, cfg.CellAspect.FromCircular(parameter.SquareOrbReach))
	for kind, spec := range component.SquareKinds {
		if spec.Attack == component.CombatAttackNone {
			continue
		}
		at := sq.OrbOf(component.SquareKind(kind), s.world.Resources.Time.GameElapsed(), cfg.CellAspect, cfg.MapWidth, cfg.MapHeight)
		s.land(cursor, spec.Attack, func(x, y int) bool { return vmath.EllipseContainsPoint(x, y, at.X, at.Y, invRx, invRy) })
	}
}

// spell runs the square's spell a tick: it lights its places a wait after it last
// went out, and a kind's blocks filling them before its hold runs out fire the
// kind's spell; filled or not, it goes out and waits again
func (s *SquareSystem) spell(sq *component.SquareComponent, dt time.Duration) {
	sq.SpellTimer -= dt
	switch kind := sq.SpellKind(sq.Spell); {
	case sq.SpellLit && kind < component.SquareKindCount:
		s.castSpell(sq, kind)
		sq.SpellLit, sq.SpellTimer, sq.SpellCast = false, parameter.SquareSpellWait, uint8(kind)+1
	case sq.SpellTimer > 0:
	case sq.SpellLit:
		sq.SpellLit, sq.SpellTimer = false, parameter.SquareSpellWait
	default:
		sq.Spell, sq.SpellLit, sq.SpellTimer, sq.SpellCast = s.spellPlaces(sq), true, parameter.SquareSpellHold, 0
	}
}

// spellPlaces draws the places a spell lights from the tick, alike on every
// instance: SquareSpellPlaces of them no kind's blocks fill already, not the last
func (s *SquareSystem) spellPlaces(sq *component.SquareComponent) uint16 {
	draw := vmath.MixChain(s.world.Resources.Game.State.GetGameTicks(), s.world.Resources.Rand.DomainRoot(core.DomainShared))
	for {
		var places uint16
		for bits.OnesCount16(places) < parameter.SquareSpellPlaces {
			draw = vmath.Mix64(draw)
			places |= 1 << (draw % component.SquareBlocks)
		}
		if places != sq.Spell && sq.SpellKind(places) == component.SquareKindCount {
			return places
		}
	}
}

// castSpell casts a kind's spell as the square's cursor's: its family on every body
// with a member within reach, all round, and its effect held SpellBoost long, the
// shield boosted and raised as a defense's fire raises it
func (s *SquareSystem) castSpell(sq *component.SquareComponent, kind component.SquareKind) {
	spec := &component.SquareKinds[kind]
	cursor := s.owner(sq)
	if spec.Spell != component.CombatAttackNone {
		s.land(cursor, spec.Spell, sq.Within(s.world.Resources.Config.CellAspect))
	}
	if effect := &sq.Effects[kind]; spec.SpellBoost > 0 && cursor != 0 {
		effect.Active = max(effect.Active, spec.SpellBoost)
		s.world.PushLocal(event.EventBoostActivate, &event.BoostActivatePayload{Entity: cursor, Duration: effect.Active})
	}
}

// Reflow keeps a square's middle by the edge or the middle it stood by as the map
// resizes, its blocks as tall as the new map has room for
func (s *SquareSystem) Reflow(stand func(x, y int) (int, int)) {
	cfg := s.world.Resources.Config
	s.world.Components.Square.Each(func(_ core.Entity, sq *component.SquareComponent) bool {
		x, y := stand(sq.Center())
		sq.Fit(x, y, cfg.MapWidth, cfg.MapHeight, cfg.CellAspect)
		w, h := sq.Size()
		sq.X, sq.Y = x-w/2, y-h/2
		return true
	})
}

// spawn lays the square whole on the map, centered as near the asked cell as that
// allows, its blocks as tall as the map has room for; a second square, a map too
// small for it, or a layout and groups that do not each name the nine digits once,
// is refused
func (s *SquareSystem) spawn(p *event.SquareSpawnRequestPayload) {
	if s.world.Components.Square.CountEntities() > 0 {
		return
	}
	kinds, ok := squareKinds(p.Layout, p.Groups)
	if !ok {
		return
	}
	cfg := s.world.Resources.Config
	sq := component.SquareComponent{Width: p.BlockWidth, MaxHeight: p.BlockHeight, Kinds: kinds, ShieldStep: p.ShieldStep}
	if sq.Width <= 0 {
		sq.Width = parameter.SquareBlockWidth
	}
	if sq.MaxHeight <= 0 {
		sq.MaxHeight = max(1, cfg.CellAspect.YInt(parameter.SquareBlockHeight))
	}
	sq.Fit(p.X, p.Y, cfg.MapWidth, cfg.MapHeight, cfg.CellAspect)
	w, h := sq.Size()
	if w > cfg.MapWidth || h > cfg.MapHeight {
		return
	}
	sq.SpellTimer = parameter.SquareSpellWait
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

// cover has its cursor's shield reach past the square's corners, step by the
// square's energy, stand while a boosting kind holds and leave its rays' stems off
// the blocks, on the instance that simulates the cursor (D-13). With that shield
// down the blocks take the touches.
func (s *SquareSystem) cover(sq *component.SquareComponent) {
	cursor := s.world.ResolveOwnedCursor(s.owner(sq))
	shield, ok := s.world.Components.Shield.GetPtr(cursor)
	if !ok {
		return
	}
	shield.Step, shield.Stemless, shield.Raised = sq.ShieldStep, true, false
	for kind, spec := range component.SquareKinds {
		shield.Raised = shield.Raised || spec.Boost && sq.Effects[kind].Active > 0
	}
	if want := sq.ShieldRadius(s.world.Resources.Config.CellAspect); shield.Radius != want {
		shield.Radius = want
		s.world.UpdateBoundsRadius()
	}
	if !shield.Active {
		s.breach(sq, cursor)
	}
}

// breach lands each body standing on the blocks: its touch costs the cursor heat
// and it is thrown off as a shield throws it, once a touch, a body still flying
// from one landing no other
func (s *SquareSystem) breach(sq *component.SquareComponent, cursor core.Entity) {
	w, h := sq.Size()
	land := func(body core.Entity, touch component.CursorDamage) {
		c, ok := s.world.Components.Combat.GetPtr(body)
		header, composite := s.world.Components.Header.GetPtr(body)
		if !ok || !composite || c.RemainingKineticImmunity > 0 {
			return
		}
		s.on = s.on[:0]
		for _, m := range header.MemberEntries {
			if at, ok := s.world.Positions.GetPosition(m.Entity); ok && m.Entity != 0 &&
				at.X >= sq.X && at.X < sq.X+w && at.Y >= sq.Y && at.Y < sq.Y+h {
				s.on = append(s.on, m.Entity)
			}
		}
		if len(s.on) == 0 {
			return
		}
		s.world.PushCrossing(event.EventCombatAttackAreaCrossingRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   component.CombatAttackShield,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: body,
			HitEntities:  slices.Clone(s.on),
		})
		strikeCursor(s.world, cursor, touch)
		s.world.PushCrossing(event.EventSquareHit, &event.SquareHitPayload{Entity: cursor})
	}
	for _, swarm := range s.world.Components.Swarm.Entities() {
		land(swarm, profile.Contact[component.SpeciesSwarm])
	}
	protos := s.world.Resources.Behaviors
	s.world.Components.Behavior.Each(func(body core.Entity, b *component.BehaviorComponent) bool {
		if int(b.Program) < len(protos) {
			land(body, protos[b.Program].Contact)
		}
		return true
	})
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
// round to the first, fills the meter of a magic state and fires each ready kind
// a line now holds three of
func (s *SquareSystem) turn(e core.Entity, sq *component.SquareComponent, digit uint8, column bool, step int) {
	place := slices.Index(sq.Digits[:], digit)
	if digit == 0 || place < 0 {
		return
	}
	places := component.LinePlaces(false, place/component.SquareSide)
	if column {
		places = component.LinePlaces(true, place%component.SquareSide)
	}
	var was [component.SquareSide]uint8
	for i, p := range places {
		was[i] = sq.Digits[p]
	}
	for i, d := range was {
		sq.Digits[places[(i+step+component.SquareSide)%component.SquareSide]] = d
	}
	sq.Lines = squareLines(sq)
	if sq.IsMagic() {
		sq.Dust = parameter.SquareDustFull
	}
	s.trigger(e, sq)
}

// shed empties the square's dust meter into dust, the share of SquareDustShed it
// held, in the blocks' digits evenly round the shield's rim, on this instance for
// its own player: what special fire then detonates. A meter short of one keeps it.
// A kind marked Shed sheds it as it fires, and its cursor's shield falling does.
func (s *SquareSystem) shed(sq *component.SquareComponent) {
	n := int(sq.Dust * parameter.SquareDustShed / parameter.SquareDustFull)
	if n == 0 {
		return
	}
	sq.Dust = 0
	aspect := s.world.Resources.Config.CellAspect
	cx, cy := sq.Center()
	rim := sq.ShieldRadius(aspect) + 1
	batch := event.ParticleBatchPool.Acquire()
	for i := range n {
		x, y := vmath.AngleToGridPos(float64(i)*vmath.TwoPi/float64(n), cx, cy, rim, aspect.FromCircular(rim))
		batch.Entries = append(batch.Entries, event.ParticleSpawnEntry{
			Behavior: component.ParticleDust, Level: component.GlyphBright,
			Char: rune('0' + sq.Digits[i%len(sq.Digits)]), X: x, Y: y,
		})
	}
	s.world.PushLocal(event.EventParticleSpawnBatch, batch)
}

// squareLines reads the kind each line holds three of and fires: the middle row and
// column face no side, so none fires a kind that strikes
func squareLines(sq *component.SquareComponent) (lines [component.SquareLines]uint8) {
	for l := range lines {
		places := component.PlacesOf(l)
		kind := sq.KindAt(places[0])
		if kind < component.SquareKindCount && sq.KindAt(places[1]) == kind && sq.KindAt(places[2]) == kind &&
			(l < component.SquareEdges || component.SquareKinds[kind].Attack == component.CombatAttackNone) {
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

// fire runs each part of a kind's effect as its cursor's: its family on what stands
// where line strikes, a boost for as long as it holds, its wind about the square,
// and the square's dust shed
func (s *SquareSystem) fire(e core.Entity, sq *component.SquareComponent, kind component.SquareKind, line int) {
	spec := &component.SquareKinds[kind]
	cursor := s.owner(sq)
	sq.Effects[kind] = component.SquareEffect{Active: spec.Duration, Cooldown: spec.Cooldown,
		Struck: spec.Attack != component.CombatAttackNone && s.land(cursor, spec.Attack, sq.Strikes(line, s.world.Resources.Config.CellAspect))}
	if spec.Boost && cursor != 0 {
		s.world.PushLocal(event.EventBoostActivate, &event.BoostActivatePayload{Entity: cursor, Duration: spec.Duration})
	}
	if cx, cy := sq.Center(); spec.Wind.Force > 0 {
		s.world.PushEvent(event.EventWindStart, &event.WindStartPayload{
			Force: spec.Wind.Force, Direction: spec.Wind.Turn, Duration: spec.Wind.Duration, Radial: true, X: cx, Y: cy,
			Reach: sq.ShieldRadius(s.world.Resources.Config.CellAspect) + spec.Wind.Reach,
		})
	}
	if spec.Shed {
		s.shed(sq)
	}
	s.world.PushEvent(event.EventSquareAligned, &event.SquareAlignedPayload{Kind: kind, Line: line})
}

// land lands a family as cursor's on every body with a member in a part of the map,
// and reports whether any stood there
func (s *SquareSystem) land(cursor core.Entity, attack component.CombatAttackType, part func(x, y int) bool) bool {
	if cursor == 0 {
		return false
	}
	bodies := FindTargetsIn(s.world, part, position.ScopeShared, cursor)
	for _, g := range bodies {
		s.world.PushLocal(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   attack,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: g.Target,
			HitEntities:  g.Members,
		})
	}
	return len(bodies) > 0
}
