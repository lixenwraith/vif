package system

import (
	"math"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// DevourerSystem grows the Devourer in from the map's edges, stands a member on every
// cell its body and its guts cover, lights the omens its guts form along and strikes
// what touches them. Its walls move a frame at a time: a tick inside a frame rebuilds
// nothing, and the footprints stand on what they were built from, so equal state
// on every instance strikes alike.
type DevourerSystem struct {
	world *engine.World
	toggle
	rng vmath.FastRand

	shape component.DevourerShape
	body  footprint
	guts  [parameter.DevourerGutCap]footprint
	built devourerBuilt
	sweep cellSweep
}

// devourerBuilt is what the footprints stand built from: a gut by its entity and
// where it hangs, which a correction may give another number
type devourerBuilt struct {
	entity        core.Entity
	seed          uint64
	frame         int64
	width, height int
	guts          [parameter.DevourerGutCap]core.Entity
	hangs         [parameter.DevourerGutCap]component.DevourerGutComponent
}

func NewDevourerSystem(world *engine.World) engine.System {
	return &DevourerSystem{world: world}
}

func (s *DevourerSystem) Name() string  { return "devourer" }
func (s *DevourerSystem) Priority() int { return parameter.PriorityDevourer }

func (s *DevourerSystem) Init() {
	s.enabled = true
	s.built = devourerBuilt{}
}

func (s *DevourerSystem) EventTypes() []event.EventType {
	return []event.EventType{event.EventDevourerSpawnRequest, event.EventSpeciesLandRequest,
		event.EventDevourerCancelRequest, event.EventMetaSystemCommandRequest}
}

func (s *DevourerSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventMetaSystemCommandRequest:
		if p, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && p.SystemName == s.Name() {
			s.enabled = p.Enabled
		}
	case event.EventDevourerCancelRequest:
		for _, e := range s.world.Components.Devourer.Entities() {
			if d, ok := s.world.Components.Devourer.GetPtr(e); ok {
				s.end(e, d)
			}
		}
	case event.EventDevourerSpawnRequest:
		if s.enabled {
			hp := 0
			if p, ok := ev.Payload.(*event.DevourerSpawnRequestPayload); ok {
				hp = p.HP
			}
			s.spawn(hp)
		}
	case event.EventSpeciesLandRequest:
		// A land request naming the devourer is its spawn request at its defaults
		if p, ok := ev.Payload.(*event.SpeciesLandRequestPayload); ok && s.enabled && p.Species == component.SpeciesDevourer {
			s.spawn(0)
		}
	}
}

func (s *DevourerSystem) spawn(hp int) {
	cfg := s.world.Resources.Config
	if cfg.MapWidth < parameter.DevourerMinWidth || cfg.MapHeight < parameter.DevourerMinHeight ||
		s.world.Components.Devourer.CountEntities() > 0 {
		s.world.PushEvent(event.EventDevourerSpawnFailed, nil)
		return
	}
	if hp <= 0 {
		hp = parameter.DevourerInitialHP
	}
	e := s.world.CreateEntity(core.DomainShared)
	// The header stands in a corner, which the bezel covers from its first frame
	s.world.Positions.SetPosition(e, component.PositionComponent{})
	s.world.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectAll ^ component.ProtectFromDeath})
	s.world.Components.Combat.SetComponent(e, component.CombatComponent{
		OwnerEntity: e, CombatEntityType: component.CombatEntityDevourer, HitPoints: hp,
	})
	s.world.Components.Header.SetComponent(e, component.HeaderComponent{
		Behavior: component.BehaviorDevourer, Type: component.CompositeTypeUnit, SkipPositionSync: true,
	})
	s.seed(e)
	s.world.Components.Devourer.SetComponent(e, component.DevourerComponent{
		MaxHitPoints: hp, Seed: s.rng.Next(), NextOmen: s.omenInterval(),
	})
	d, _ := s.world.Components.Devourer.GetPtr(e)
	s.build(e, d)
	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity: e, Species: component.SpeciesDevourer, X: cfg.MapWidth / 2, Y: cfg.MapHeight / 2, MemberCount: len(s.body.cells),
	})
}

func (s *DevourerSystem) Update() {
	if !s.enabled {
		return
	}
	dt := s.world.Resources.Time.DeltaTime
	for _, e := range s.world.Components.Devourer.Entities() {
		combat, ok := s.world.Components.Combat.GetPtr(e)
		d, alive := s.world.Components.Devourer.GetPtr(e)
		if !ok || !alive {
			continue
		}
		cfg := s.world.Resources.Config
		if combat.HitPoints <= 0 {
			// Its drops burst from the open space, where its killer stands
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity: e, Species: component.SpeciesDevourer, KillerEntity: combat.LastDamagedBy,
				X: cfg.MapWidth / 2, Y: cfg.MapHeight / 2,
			})
			s.world.PushLocal(event.EventDevourerRecedeRequest, &component.DevourerRecede{
				Entity: e, Devourer: *d, Width: cfg.MapWidth, Height: cfg.MapHeight, Grown: d.Grown(),
			})
			s.end(e, d)
			continue
		}
		if cfg.MapWidth < parameter.DevourerMinWidth || cfg.MapHeight < parameter.DevourerMinHeight {
			s.end(e, d)
			s.world.PushEvent(event.EventDevourerSpawnFailed, nil)
			continue
		}
		d.Elapsed += dt
		s.seed(e)
		s.reap(d)
		s.foretell(e, d, dt)
		s.build(e, d)
		s.strike(d)
	}
}

func (s *DevourerSystem) seed(e core.Entity) {
	seed := vmath.DeriveSeed(s.world.Resources.Rand.DomainRoot(core.DomainShared), s.Name())
	s.rng.Reseed(vmath.MixChain(seed, uint64(e), s.world.Resources.Game.State.GetGameTicks()))
}

func (s *DevourerSystem) omenInterval() time.Duration {
	return parameter.DevourerOmenMin + time.Duration(s.rng.Float64()*float64(parameter.DevourerOmenMax-parameter.DevourerOmenMin))
}

// reap ends each gut shot through, fading it where it hung, and empties the slot of
// any gone; a gut leaves nothing, and is no kill
func (s *DevourerSystem) reap(d *component.DevourerComponent) {
	for i, gut := range d.Guts {
		if gut == 0 {
			continue
		}
		combat, ok := s.world.Components.Combat.GetPtr(gut)
		if ok && s.world.Components.DevourerGut.HasEntity(gut) && combat.HitPoints > 0 {
			continue
		}
		if ok {
			s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
				HeaderEntity: gut, Effect: component.DeathFadeout,
			})
		}
		d.Guts[i] = 0
	}
}

// foretell runs the omen, forming its gut as it ends, and lights the next while a
// slot stands empty, its ends half a turn apart round the open space
func (s *DevourerSystem) foretell(e core.Entity, d *component.DevourerComponent, dt time.Duration) {
	if d.Omen.Remaining > 0 {
		if d.Omen.Remaining -= dt; d.Omen.Remaining <= 0 {
			d.Omen.Remaining = 0
			s.form(e, d)
		}
		return
	}
	slot := emptyGut(d)
	if d.Grown() < 1 || slot < 0 {
		return
	}
	if d.NextOmen -= dt; d.NextOmen > 0 {
		return
	}
	from := s.rng.Float64() * 2 * math.Pi
	to := math.Mod(from+math.Pi+(s.rng.Float64()-0.5)*parameter.DevourerOmenSpread, 2*math.Pi)
	d.Omen = component.DevourerOmen{Remaining: parameter.DevourerOmen, From: from, To: to}
	d.NextOmen = s.omenInterval()
}

func emptyGut(d *component.DevourerComponent) int {
	for i, gut := range d.Guts {
		if gut == 0 {
			return i
		}
	}
	return -1
}

// form grows a gut along the omen ending; it stands from this tick, as the omen goes
func (s *DevourerSystem) form(e core.Entity, d *component.DevourerComponent) {
	slot := emptyGut(d)
	if slot < 0 {
		return
	}
	g := s.world.CreateEntity(core.DomainShared)
	s.world.Components.Protection.SetComponent(g, component.ProtectionComponent{Mask: component.ProtectAll ^ component.ProtectFromDeath})
	s.world.Components.Combat.SetComponent(g, component.CombatComponent{
		OwnerEntity: g, CombatEntityType: component.CombatEntityDevourerGut, HitPoints: parameter.DevourerGutHP,
	})
	s.world.Components.Header.SetComponent(g, component.HeaderComponent{
		Behavior: component.BehaviorDevourer, Type: component.CompositeTypeUnit, SkipPositionSync: true,
	})
	s.world.Components.DevourerGut.SetComponent(g, component.DevourerGutComponent{
		Devourer: e, From: d.Omen.From, To: d.Omen.To, Formed: d.Frame(), Phase: s.rng.Float64() * 2 * math.Pi,
	})
	d.Guts[slot] = g
}

// build stands the body and each gut on the frame the Devourer stands at, unless
// they stand there already, devouring what the cells they come to hold
func (s *DevourerSystem) build(e core.Entity, d *component.DevourerComponent) {
	cfg := s.world.Resources.Config
	key := devourerBuilt{entity: e, seed: d.Seed, frame: d.Frame(), width: cfg.MapWidth, height: cfg.MapHeight, guts: d.Guts}
	for i, gut := range d.Guts {
		if g, ok := s.world.Components.DevourerGut.GetComponent(gut); ok {
			key.hangs[i] = g
		}
	}
	if key == s.built {
		return
	}
	moved := key.entity != s.built.entity || key.seed != s.built.seed || key.frame != s.built.frame ||
		key.width != s.built.width || key.height != s.built.height
	s.built = key
	s.shape.Build(d.Seed, key.frame, d.Grown(), cfg.MapWidth, cfg.MapHeight)
	if moved {
		s.body.reset(cfg.MapWidth, cfg.MapHeight)
		for y := range cfg.MapHeight {
			lo, hi := s.shape.Span(y, 0)
			for x := 0; x < cfg.MapWidth; x++ {
				if x == lo && lo <= hi {
					x = hi
					continue
				}
				if s.shape.Covers(x, y) {
					s.body.add(x, y)
				}
			}
		}
		s.body.stand(s.world, e, 0, 0)
		s.devour(s.body.claimed)
	}
	for i, gut := range d.Guts {
		f := &s.guts[i]
		f.reset(cfg.MapWidth, cfg.MapHeight)
		if gut == 0 || !s.world.Components.DevourerGut.HasEntity(gut) {
			continue
		}
		mx, my := s.layGut(f, &key.hangs[i], key.frame)
		s.world.Positions.SetPosition(gut, component.PositionComponent{X: mx, Y: my})
		f.stand(s.world, gut, mx, my)
		s.devour(f.claimed)
	}
}

// layGut claims the cells within DevourerGutRadius of a gut's centreline that the
// body leaves open, and returns the cell its middle hangs over
func (s *DevourerSystem) layGut(f *footprint, g *component.DevourerGutComponent, frame int64) (int, int) {
	x0, y0, x1, y1 := s.shape.GutEnds(g.From, g.To)
	age := float64(frame-g.Formed) * parameter.DevourerFrame.Seconds()
	r := parameter.DevourerGutRadius
	steps := max(int(math.Hypot(x1-x0, y1-y0)/(r/4)), 1)
	for i := range steps + 1 {
		x, y := component.GutPoint(x0, y0, x1, y1, float64(i)/float64(steps), age, g.Phase)
		cx0, cx1 := max(0, int(math.Ceil(x-r-vmath.CellCenter))), min(f.gridW-1, int(math.Floor(x+r-vmath.CellCenter)))
		cy0, cy1 := max(0, int(math.Ceil((y-r)/2-vmath.CellCenter))), min(f.gridH-1, int(math.Floor((y+r)/2-vmath.CellCenter)))
		for cy := cy0; cy <= cy1; cy++ {
			dy := 2*(float64(cy)+vmath.CellCenter) - y
			for cx := cx0; cx <= cx1; cx++ {
				dx := float64(cx) + vmath.CellCenter - x
				if dx*dx+dy*dy <= r*r && !s.body.covers(cx, cy) {
					f.add(cx, cy)
				}
			}
		}
	}
	mx, my := component.GutPoint(x0, y0, x1, y1, 0.5, age, g.Phase)
	return min(max(int(mx), 0), f.gridW-1), min(max(int(my/2), 0), f.gridH-1)
}

// devour takes what the cells a member came to hold: a glyph, a player's or a gold
// member, flashes away, and a nugget goes
func (s *DevourerSystem) devour(cells []vmath.Point) {
	if len(cells) == 0 {
		return
	}
	s.sweep.reset()
	clearable := func(target core.Entity) bool {
		if !speciesClearable(s.world, target, nil, nil) {
			return false
		}
		if s.world.Components.Nugget.HasEntity(target) {
			s.world.PushLocal(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: target})
			return true
		}
		return s.world.Components.Glyph.HasEntity(target)
	}
	for _, c := range cells {
		s.sweep.collect(s.world, c.X, c.Y, clearable)
	}
	s.sweep.emit(s.world, component.DeathFlash)
}

// strike costs each cursor this instance simulates that the body or a gut touches its
// contact, once a tick however much of it touches
func (s *DevourerSystem) strike(d *component.DevourerComponent) {
	damage := profile.Contact[component.SpeciesDevourer]
	for slot := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(slot))
		if cursor == 0 || !s.world.SimulatesLocally(cursor) {
			continue
		}
		pos, ok := s.world.Positions.GetPosition(cursor)
		if !ok {
			continue
		}
		touched := s.body.touches(s.world, cursor, pos.X, pos.Y)
		for i := range d.Guts {
			touched = touched || d.Guts[i] != 0 && s.guts[i].touches(s.world, cursor, pos.X, pos.Y)
		}
		if touched {
			strikeCursor(s.world, cursor, damage)
		}
	}
}

// end removes a Devourer, its guts fading where they hung
func (s *DevourerSystem) end(e core.Entity, d *component.DevourerComponent) {
	for i, gut := range d.Guts {
		if gut != 0 {
			s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
				HeaderEntity: gut, Effect: component.DeathFadeout,
			})
			d.Guts[i] = 0
		}
	}
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{HeaderEntity: e})
}
