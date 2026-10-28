package system

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/behavior"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// BehaviorSystem runs the data species: each program of the scenario's steps its
// instances through its compiled machine, over the scaffold every composite species
// shares of spawn, contacts and lifecycle
type BehaviorSystem struct {
	world *engine.World
	set   *BehaviorSet
	ctx   behaviorCtx // the instance being stepped, refilled per entity

	// chanceRoot seeds the Chance guard from the tick, entity and site, so no
	// stream is drawn and a predicted death shifts no later draw (D-8)
	chanceRoot uint64

	// Telemetry
	statActive          *atomic.Bool
	statCount           *atomic.Int64
	statProtected       *atomic.Int64
	statProtectedPlayer *atomic.Int64
	statUnknown         *atomic.Int64
	programs            []behaviorTelemetry // by program index
	motion              bounceTelemetry
	sweep               cellSweep

	toggle
}

// behaviorTelemetry is one program's rows, keyed behavior.<program>
type behaviorTelemetry struct {
	count     *atomic.Int64
	lifecycle lifecycleTelemetry
}

// BehaviorSet is a scenario's compiled programs, named without the context its verbs read
type BehaviorSet = behavior.Set[*behaviorCtx]

// CompileBehaviors compiles a scenario's programs, keyed by name, against the game
// vocabulary
func CompileBehaviors(byName map[string]map[string]any) (*BehaviorSet, error) {
	return behavior.Compile(BehaviorVocabulary(), byName)
}

// NewBehaviorSystem runs set's programs; Resources.Behaviors holds their prototypes
func NewBehaviorSystem(world *engine.World, set *BehaviorSet) engine.System {
	s := &BehaviorSystem{world: world, set: set}

	reg := world.Resources.Status
	s.statActive = reg.Bools.Get("behavior.active")
	s.statCount = reg.Ints.Get("behavior.count")
	s.statProtected = reg.Ints.Get("behavior.protected_rejects")
	s.statProtectedPlayer = reg.Ints.Get("behavior.protected_player_rejects")
	s.statUnknown = reg.Ints.Get("behavior.unknown_program_rejects")
	s.motion = newBounceTelemetry(reg, "behavior")
	for _, name := range set.Names() {
		s.programs = append(s.programs, behaviorTelemetry{
			count:     reg.Ints.Get("behavior." + name + ".count"),
			lifecycle: newLifecycleTelemetry(reg, "behavior."+name),
		})
	}
	return s
}

func (s *BehaviorSystem) Init() {
	s.chanceRoot = vmath.DeriveSeed(s.world.Resources.Rand.DomainRoot(core.DomainShared), s.Name())
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.statProtected.Store(0)
	s.statProtectedPlayer.Store(0)
	s.statUnknown.Store(0)
	for i := range s.programs {
		s.programs[i].count.Store(0)
		s.programs[i].lifecycle.Reset()
	}
	s.motion.Reset()
	s.enabled = true
}

func (s *BehaviorSystem) Name() string { return "behavior" }

func (s *BehaviorSystem) Priority() int { return parameter.PriorityBehavior }

func (s *BehaviorSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBehaviorSpawnRequest,
		event.EventBehaviorCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventSpeciesKilled,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *BehaviorSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventMetaSystemCommandRequest:
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && payload.SystemName == s.Name() {
			s.enabled = payload.Enabled
		}
		return
	case event.EventSpeciesKilled:
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok {
			if i := int(payload.Species) - int(component.SpeciesDataFirst); i >= 0 && i < len(s.programs) {
				s.programs[i].lifecycle.RecordKill(s.world, payload.KillerEntity)
			}
		}
		return
	}

	switch payload := ev.Payload.(type) {
	case *event.BehaviorSpawnRequestPayload:
		i, ok := s.set.Lookup(payload.Program)
		switch {
		case !ok:
			s.statUnknown.Add(1)
		case !s.enabled:
			s.programs[i].lifecycle.spawnFailures.Add(1)
		default:
			s.spawn(i, payload.X, payload.Y)
		}

	case *event.BehaviorCancelRequestPayload:
		if s.enabled {
			s.cancel(payload.Program)
		}

	case *event.CompositeIntegrityBreachPayload:
		// OOB or another mechanic destroyed members
		if s.enabled && payload.Behavior == component.BehaviorData {
			if b, ok := s.world.Components.Behavior.GetComponent(payload.HeaderEntity); ok && int(b.Program) < len(s.programs) {
				s.programs[b.Program].lifecycle.despawned.Add(1)
			}
			s.terminate(payload.HeaderEntity)
		}
	}
}

func (s *BehaviorSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	var counts [parameter.BehaviorProgramsMax]int64
	total := int64(0)

	for _, header := range s.world.Components.Behavior.Entities() {
		b, ok := s.world.Components.Behavior.GetPtr(header)
		if !ok || !s.fill(header, b, dt) {
			continue
		}
		c := &s.ctx

		// Hit points ahead of everything, as every species orders it: a stunned
		// instance still dies
		if c.combat.HitPoints <= 0 {
			killX, killY := -1, -1
			if pos, ok := s.world.Positions.GetPosition(header); ok {
				killX, killY = pos.X, pos.Y
			}
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity:       header,
				KillerEntity: c.combat.LastDamagedBy,
				Species:      c.proto.Species,
				X:            killX,
				Y:            killY,
			})
			s.terminate(header)
			continue
		}
		if timer, ok := s.world.Components.Timer.GetPtr(header); ok && timer.Expired(component.TimerDeadline) {
			s.programs[b.Program].lifecycle.killedLifecycle.Add(1)
			s.terminate(header)
			continue
		}

		// A stun freezes the animation as it freezes the step
		if c.combat.Status[component.StatusStun].Remaining <= 0 && len(c.proto.Look.Frames) > 1 {
			b.PatternElapsed += dt
			for b.PatternElapsed >= c.proto.Pattern {
				b.PatternElapsed -= c.proto.Pattern
				b.PatternIndex = (b.PatternIndex + 1) % len(c.proto.Look.Frames)
			}
		}

		flags, suspended := s.set.Graph(int(b.Program)).Step(&b.Machine, c, dt)

		// The state's outputs, written every step; the ward needs its refresh
		shielded := flags&behaviorShielded != 0
		c.combat.IsEnraged = flags&behaviorEnraged != 0
		b.Shielded = shielded
		b.Beaming = flags&behaviorBeam != 0
		b.ChargeLine = flags&behaviorChargeLine != 0
		b.Landing = flags&behaviorLanding != 0
		if shielded {
			c.combat.Status[component.StatusWard] = component.Status{Remaining: parameter.CombatDamageImmunityDuration}
		}

		if !suspended {
			breakGoldUnder(s.world, &s.sweep, header)
			strikeContacts(s.world, header, c.proto.Contact, true)
		}
		counts[b.Program]++
		total++
	}

	for i := range s.programs {
		s.programs[i].count.Store(counts[i])
	}
	s.statCount.Store(total)
	s.statActive.Store(total > 0)
}

// fill points the context at one instance, false when it lacks a component every
// verb may read
func (s *BehaviorSystem) fill(header core.Entity, b *component.BehaviorComponent, dt time.Duration) bool {
	combat, hasCombat := s.world.Components.Combat.GetPtr(header)
	kinetic, hasKinetic := s.world.Components.Kinetic.GetPtr(header)
	if !hasCombat || !hasKinetic || int(b.Program) >= s.set.Len() {
		return false
	}
	s.ctx = behaviorCtx{
		sys:      s,
		entity:   header,
		behavior: b,
		combat:   combat,
		kinetic:  kinetic,
		proto:    &s.set.Prototypes()[b.Program],
		dt:       dt,
		dtSec:    min(dt.Seconds(), parameter.MaxSimulationDeltaSeconds),
	}
	return true
}

// spawn builds program i's composite with its header at the cell, or the nearest
// place its footprint fits, searched over the whole map as a fuse's beam is: a
// scenario names a cell blind to the walls, a cave's rock among them, and to the
// body, a cell on an edge or past it searching from the footprint flush against it
func (s *BehaviorSystem) spawn(i, x, y int) {
	p := &s.set.Prototypes()[i]
	left, top, found := s.world.Positions.FindFreeAreaInside(x, y, p.Width, p.Height, p.HeaderX, p.HeaderY, component.WallBlockSpawn, -1)
	if !found {
		s.programs[i].lifecycle.spawnFailures.Add(1)
		return
	}
	headerX, headerY := left+p.HeaderX, top+p.HeaderY
	s.clearFootprint(p, headerX, headerY, 0)

	header := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(header, component.PositionComponent{X: headerX, Y: headerY})
	s.world.Components.Protection.SetComponent(header, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})
	s.world.Components.Behavior.SetComponent(header, component.BehaviorComponent{Program: uint8(i)})

	preciseX, preciseY := vmath.Point{X: headerX, Y: headerY}.Center()
	s.world.Components.Kinetic.SetComponent(header, component.KineticComponent{
		Kinetic: physics.Kinetic{PreciseX: preciseX, PreciseY: preciseY},
	})
	s.world.Components.Navigation.SetComponent(header, component.NavigationComponent{
		Width:         p.Width,
		Height:        p.Height,
		FlowLookahead: parameter.NavFlowLookaheadDefault,
	})
	s.world.Components.Combat.SetComponent(header, component.CombatComponent{
		OwnerEntity:      header,
		CombatEntityType: p.Combat,
		HitPoints:        p.HitPoints,
	})
	if p.Lifetime > 0 {
		armTimer(s.world, header, component.TimerDeadline, p.Lifetime)
	}

	members := make([]component.MemberEntry, 0, p.Width*p.Height)
	for row := range p.Height {
		for col := range p.Width {
			member := s.world.CreateEntity(core.DomainShared)
			s.world.Positions.SetPosition(member, component.PositionComponent{X: left + col, Y: top + row})
			s.world.Components.Protection.SetComponent(member, component.ProtectionComponent{
				Mask: component.ProtectFromParticle | component.ProtectFromSpecies,
			})
			s.world.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
			members = append(members, component.MemberEntry{Entity: member, OffsetX: col - p.HeaderX, OffsetY: row - p.HeaderY})
		}
	}
	s.world.Components.Header.SetComponent(header, component.HeaderComponent{
		Behavior:      component.BehaviorData,
		Type:          component.CompositeTypeUnit,
		MemberEntries: members,
	})

	// Sampled once placed, so a spawn that found no room costs no evaluation
	genes, evalID := s.world.Resources.Genetics.Sample(uint8(p.Species))

	// The machine starts once every component a verb may read is in place
	b, _ := s.world.Components.Behavior.GetPtr(header)
	s.fill(header, b, 0)
	s.set.Graph(i).Start(&b.Machine, &s.ctx, s.set.Initial(i), 0)

	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity:      header,
		Species:     p.Species,
		X:           headerX,
		Y:           headerY,
		MemberCount: len(members),
		Genes:       genes,
		EvalID:      evalID,
	})
	s.programs[i].lifecycle.spawned.Add(1)
	// Counted now, not at the next step: a beam lands its instance after this
	// tick's step, and a region reading the count meanwhile must not see none
	s.programs[i].count.Add(1)
	s.statCount.Add(1)
	s.statActive.Store(true)
}

// clearFootprint empties a footprint in both domains (D-12), each victim leaving
// with effect
func (s *BehaviorSystem) clearFootprint(p *component.BehaviorPrototype, headerX, headerY int, effect event.EventType) {
	s.sweep.reset()
	for row := range p.Height {
		for col := range p.Width {
			s.sweep.collect(s.world, headerX-p.HeaderX+col, headerY-p.HeaderY+row, func(e core.Entity) bool {
				return speciesClearable(s.world, e, s.statProtected, s.statProtectedPlayer)
			})
		}
	}
	s.sweep.emit(s.world, effect)
}

// place moves a header and its members with it
func (s *BehaviorSystem) place(header core.Entity, x, y int) {
	s.world.Positions.SetPosition(header, component.PositionComponent{X: x, Y: y})
	h, ok := s.world.Components.Header.GetPtr(header)
	if !ok {
		return
	}
	for _, m := range h.MemberEntries {
		if m.Entity != 0 {
			s.world.Positions.SetPosition(m.Entity, component.PositionComponent{X: x + m.OffsetX, Y: y + m.OffsetY})
		}
	}
}

// cancel ends one program's instances, or every program's when it names none
func (s *BehaviorSystem) cancel(program string) {
	i, known := s.set.Lookup(program)
	if program != "" && !known {
		s.statUnknown.Add(1)
		return
	}
	for _, header := range s.world.Components.Behavior.Entities() {
		b, ok := s.world.Components.Behavior.GetComponent(header)
		if !ok || int(b.Program) >= len(s.programs) || program != "" && int(b.Program) != i {
			continue
		}
		s.programs[b.Program].lifecycle.despawned.Add(1)
		s.terminate(header)
	}
}

// terminate ends an instance: its state's exit, then the composite
func (s *BehaviorSystem) terminate(header core.Entity) {
	// The exit ends what the state started, a telegraph above all
	if b, ok := s.world.Components.Behavior.GetPtr(header); ok && s.fill(header, b, 0) {
		s.set.Graph(int(b.Program)).Stop(&b.Machine, &s.ctx)
	}
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: header,
	})
}

// integrate is every tick's movement unless Freeze ran: the footprint bounces off
// walls and the map edge, and the members follow the header. The header's last
// column and row keep the footprint inside the map, and the edge's reflection
// sets it back on the cell it stood on, which the walls left free.
func (s *BehaviorSystem) integrate(c *behaviorCtx) {
	pos, ok := s.world.Positions.GetPosition(c.entity)
	if c.frozen || !ok {
		return
	}
	p, config := c.proto, s.world.Resources.Config
	wallCheck := func(left, top int) bool {
		return s.world.Positions.HasBlockingWallInArea(left, top, p.Width, p.Height, component.WallBlockKinetic)
	}
	x, y, motion := physics.IntegrateWithBounceStats(
		&c.kinetic.Kinetic,
		c.dtSec,
		p.HeaderX, p.HeaderY,
		p.HeaderX, config.MapWidth-(p.Width-p.HeaderX),
		p.HeaderY, config.MapHeight-(p.Height-p.HeaderY),
		p.Restitution,
		wallCheck,
	)
	s.motion.Record(motion)
	c.hitWall = motion.Hit()
	if x != pos.X || y != pos.Y {
		s.place(c.entity, x, y)
	}
}

// teleport moves the composite to the stored cell, or the nearest place its
// footprint fits, clearing the landing with a flash
func (s *BehaviorSystem) teleport(c *behaviorCtx) {
	p := c.proto
	left, top, found := s.world.Positions.FindFreeAreaInside(
		c.behavior.TargetX, c.behavior.TargetY, p.Width, p.Height, p.HeaderX, p.HeaderY, component.WallBlockSpawn, 0)
	if !found {
		return
	}
	x, y := left+p.HeaderX, top+p.HeaderY
	s.clearFootprint(p, x, y, event.EventFlashSpawnOneRequest)
	c.kinetic.PreciseX, c.kinetic.PreciseY = vmath.Point{X: x, Y: y}.Center()
	c.kinetic.VelX, c.kinetic.VelY = 0, 0
	s.place(c.entity, x, y)
}

func (s *BehaviorSystem) SaveLocal() ([]byte, error) { return json.Marshal(s.chanceRoot) }

func (s *BehaviorSystem) LoadLocal(data []byte) error {
	if err := json.Unmarshal(data, &s.chanceRoot); err != nil {
		return fmt.Errorf("behavior: %w", err)
	}
	return nil
}

// === The vocabulary ===

// Behavior flags, which Update writes to the components after every step
const (
	behaviorEnraged    uint64 = 1 << iota // combat's enrage, which refuses kinetic knockback
	behaviorShielded                      // ward held and stun refused: no damage, no stun
	behaviorBeam                          // a teleport line from the header to the stored cell
	behaviorChargeLine                    // the swarm's charge line pulsing toward the stored cell
	behaviorLanding                       // the landing marker blinking where the body lands on the stored cell
)

// behaviorCtx is what one instance's step reads and its verbs write through. Verbs
// reach the world only through sys, so one vocabulary serves every program.
type behaviorCtx struct {
	sys      *BehaviorSystem
	entity   core.Entity
	behavior *component.BehaviorComponent
	combat   *component.CombatComponent
	kinetic  *component.KineticComponent
	proto    *component.BehaviorPrototype
	dt       time.Duration
	dtSec    float64 // dt capped against tunneling

	probeX, probeY int  // the cell the entry's probe resolved
	probed         bool // a probe resolved one for the entry being taken
	hitWall        bool // this step's movement met a wall or the map edge
	frozen         bool // Freeze ran: this tick does not move
}

// target is the cell the instance's group aims at, the cursor's by default
func (c *behaviorCtx) target() (int, int, bool) { return resolveBaseTarget(c.sys.world, c.entity) }

// targetDistance measures header to target with rows scaled by the cell aspect, so
// a distance reads alike on both axes
func (c *behaviorCtx) targetDistance() (float64, bool) {
	x, y, ok := c.target()
	pos, placed := c.sys.world.Positions.GetPosition(c.entity)
	if !ok || !placed {
		return 0, false
	}
	dy := c.sys.world.Resources.Config.CellAspect.Circular(float64(y - pos.Y))
	return vmath.Magnitude(float64(x-pos.X), dy), true
}

func (c *behaviorCtx) probe(x, y int) { c.probeX, c.probeY, c.probed = x, y, true }

// aim reads the cell a verb's to names: the target by default, or with "stored"
// the cell StoreTarget kept, which holds still however the cursors move after it
func (a *targetArgs) aim() (func(*behaviorCtx) (int, int, bool), error) {
	switch a.To {
	case "", "target":
		return (*behaviorCtx).target, nil
	case "stored":
		return func(c *behaviorCtx) (int, int, bool) { return c.behavior.TargetX, c.behavior.TargetY, true }, nil
	}
	return nil, fmt.Errorf("to %q is not one of target, stored", a.To)
}

// telegraphColors are the colours a program names a telegraph's splash by
var telegraphColors = []struct {
	name string
	rgb  color.RGB
}{{"cyan", visual.RgbSplashCyan}, {"white", visual.RgbSplashWhite}}

// spawnRequests are the code species SpawnSpecies spawns, each by its request
var spawnRequests = []struct {
	species component.SpeciesType
	request func(x, y int) (event.EventType, any)
}{
	{component.SpeciesSwarm, func(x, y int) (event.EventType, any) {
		return event.EventSwarmSpawnRequest, &event.SwarmSpawnRequestPayload{X: x, Y: y}
	}},
	{component.SpeciesQuasar, func(x, y int) (event.EventType, any) {
		return event.EventQuasarSpawnRequest, &event.QuasarSpawnRequestPayload{X: x, Y: y}
	}},
}

// The args the verbs decode, each strictly, once at load
type (
	behaviorNoArgs struct{}
	distanceArgs   struct {
		Op    dialect.Op `toml:"op"`
		Cells float64    `toml:"cells"`
	}
	targetArgs struct {
		To string `toml:"to"`
	}
	fractionArgs struct {
		Op    dialect.Op `toml:"op"`
		Value float64    `toml:"value"`
	}
	chanceArgs struct {
		Percent int `toml:"percent"`
	}
	paramArgs struct {
		Index int        `toml:"index"`
		Op    dialect.Op `toml:"op"`
		Value float64    `toml:"value"`
	}
	colorArgs struct {
		Color string `toml:"color"`
	}
	materializeArgs struct {
		Spawn string `toml:"spawn"`
	}
	spawnSpeciesArgs struct {
		Species string `toml:"species"`
		X       int    `toml:"x"`
		Y       int    `toml:"y"`
	}
	dragArgs struct {
		Rate float64 `toml:"rate"`
	}
)

// BehaviorVocabulary is the game's verbs over a data species, which every program
// compiles against and -schema lists
var BehaviorVocabulary = sync.OnceValue(newBehaviorVocabulary)

func newBehaviorVocabulary() *behavior.Vocabulary[*behaviorCtx] {
	v := behavior.NewVocabulary[*behaviorCtx]()
	reads := func(f func(*behaviorCtx) bool) func(*behaviorNoArgs) (func(*behaviorCtx) bool, error) {
		return func(*behaviorNoArgs) (func(*behaviorCtx) bool, error) { return f, nil }
	}
	does := func(f func(*behaviorCtx)) func(*behaviorNoArgs) (func(*behaviorCtx), error) {
		return func(*behaviorNoArgs) (func(*behaviorCtx), error) { return f, nil }
	}

	// --- Guards: read-only ---
	v.Predicate("TargetDistance", func(a *distanceArgs) (func(*behaviorCtx) bool, error) {
		return func(c *behaviorCtx) bool {
			d, ok := c.targetDistance()
			return ok && a.Op.Holds(d, a.Cells)
		}, nil
	})
	v.Predicate("LineOfSight", func(a *targetArgs) (func(*behaviorCtx) bool, error) {
		aim, err := a.aim()
		if err != nil {
			return nil, err
		}
		return func(c *behaviorCtx) bool {
			x, y, ok := aim(c)
			pos, placed := c.sys.world.Positions.GetPosition(c.entity)
			p := c.proto
			return ok && placed && bodyHasLineOfSight(c.sys.world, pos.X, pos.Y, x, y, p.Width, p.Height, p.HeaderX, p.HeaderY)
		}, nil
	})
	v.Predicate("HasTarget", reads(func(c *behaviorCtx) bool {
		_, _, ok := c.target()
		return ok
	}))
	v.Predicate("Stunned", reads(func(c *behaviorCtx) bool { return c.combat.Status[component.StatusStun].Remaining > 0 }))
	v.Predicate("HPFraction", func(a *fractionArgs) (func(*behaviorCtx) bool, error) {
		return func(c *behaviorCtx) bool {
			return a.Op.Holds(float64(c.combat.HitPoints)/float64(c.proto.HitPoints), a.Value)
		}, nil
	})
	v.Predicate("HitWall", reads(func(c *behaviorCtx) bool { return c.hitWall }))
	v.Seeded("Chance", func(a *chanceArgs, site int) (func(*behaviorCtx) bool, error) {
		if a.Percent < 1 || a.Percent > 100 {
			return nil, fmt.Errorf("percent must be 1..100, got %d", a.Percent)
		}
		return func(c *behaviorCtx) bool {
			tick := c.sys.world.Resources.Game.State.GetGameTicks()
			return vmath.MixChain(c.sys.chanceRoot, tick, uint64(c.entity), uint64(site))%100 < uint64(a.Percent)
		}, nil
	})
	v.Predicate("Param", func(a *paramArgs) (func(*behaviorCtx) bool, error) {
		if a.Index < 0 {
			return nil, fmt.Errorf("index %d is negative", a.Index)
		}
		return func(c *behaviorCtx) bool {
			g, ok := c.sys.world.Components.Genotype.GetComponent(c.entity)
			return ok && a.Index < len(g.Genes) && a.Op.Holds(g.Genes[a.Index], a.Value)
		}, nil
	})

	// --- Probes: read-only, the cell lands in the context ---
	v.Probe("ResolveTarget", reads(func(c *behaviorCtx) bool {
		x, y, ok := c.target()
		if ok {
			c.probe(x, y)
		}
		return ok
	}))
	v.Probe("FindLanding", func(a *targetArgs) (func(*behaviorCtx) bool, error) {
		aim, err := a.aim()
		if err != nil {
			return nil, err
		}
		return func(c *behaviorCtx) bool {
			x, y, ok := aim(c)
			if !ok {
				return false
			}
			p := c.proto
			left, top, found := c.sys.world.Positions.FindFreeAreaInside(x, y, p.Width, p.Height, p.HeaderX, p.HeaderY, component.WallBlockSpawn, 0)
			if found {
				c.probe(left+p.HeaderX, top+p.HeaderY)
			}
			return found
		}, nil
	})

	// --- Effects: each fixes its push ---
	v.Effect("StoreTarget", does(func(c *behaviorCtx) {
		x, y, ok := c.probeX, c.probeY, c.probed
		if !ok {
			x, y, ok = c.target()
		}
		if ok {
			c.behavior.TargetX, c.behavior.TargetY = x, y
		}
	}))
	v.Effect("ZeroVelocity", does(func(c *behaviorCtx) { c.kinetic.VelX, c.kinetic.VelY = 0, 0 }))
	v.Effect("Teleport", does(func(c *behaviorCtx) { c.sys.teleport(c) }))
	v.Effect("Telegraph", func(a *colorArgs) (func(*behaviorCtx), error) {
		i := slices.IndexFunc(telegraphColors, func(t struct {
			name string
			rgb  color.RGB
		}) bool {
			return t.name == a.Color
		})
		if i < 0 {
			return nil, fmt.Errorf("color %q is not one of cyan, white", a.Color)
		}
		rgb := telegraphColors[i].rgb
		// A countdown over what is left of the entry, so a cue's reads true too
		return func(c *behaviorCtx) {
			p, m := c.proto, c.behavior.Machine
			c.sys.world.PushLocal(event.EventSplashTimerRequest, &event.SplashTimerRequestPayload{
				AnchorEntity: c.entity,
				Color:        rgb,
				MarginLeft:   p.HeaderX,
				MarginRight:  p.Width - p.HeaderX,
				MarginTop:    p.HeaderY,
				MarginBottom: p.Height - p.HeaderY,
				Duration:     m.Duration - m.Elapsed,
			})
		}, nil
	})
	v.Effect("CancelTelegraph", does(func(c *behaviorCtx) {
		c.sys.world.PushLocal(event.EventSplashTimerCancel, &event.SplashTimerCancelPayload{AnchorEntity: c.entity})
	}))
	v.Effect("Lightning", func(a *colorArgs) (func(*behaviorCtx), error) {
		i := slices.Index(component.LightningColorNames[:], a.Color)
		if i < 0 {
			return nil, fmt.Errorf("color %q is not one of %s", a.Color, strings.Join(component.LightningColorNames[:], ", "))
		}
		bolt := component.LightningColorType(i)
		// The bolt is presentation; the hit lands on the cursor's owner alone
		return func(c *behaviorCtx) {
			pos, ok := c.sys.world.Positions.GetPosition(c.entity)
			if !ok {
				return
			}
			cursor, x, y, ok := ClosestCursor(c.sys.world, pos.X, pos.Y)
			if !ok {
				return
			}
			strikeCursor(c.sys.world, cursor, c.proto.Contact)
			c.sys.world.PushLocal(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
				Owner:        c.entity,
				OriginX:      pos.X,
				OriginY:      pos.Y,
				TargetX:      x,
				TargetY:      y,
				OriginEntity: c.entity,
				TargetEntity: cursor,
				ColorType:    bolt,
				Duration:     parameter.LightningZapDuration,
			})
		}, nil
	})
	v.Effect("Materialize", func(a *materializeArgs) (func(*behaviorCtx), error) {
		// A drain beam's completion spawns a drain, so only the announcing beams
		spawn := slices.IndexFunc(component.MaterializeStyles[:], func(s component.MaterializeStyle) bool { return s.Name == a.Spawn })
		if spawn < 0 || component.SpawnType(spawn) == component.SpawnTypeDrain {
			return nil, fmt.Errorf("spawn %q is not one of swarm, nugget", a.Spawn)
		}
		return func(c *behaviorCtx) {
			x, y, ok := c.target()
			if !ok {
				return
			}
			p := c.proto
			c.sys.world.PushEvent(event.EventMaterializeAreaRequest, &event.MaterializeAreaRequestPayload{
				X:          x - p.HeaderX,
				Y:          y - p.HeaderY,
				AreaWidth:  p.Width,
				AreaHeight: p.Height,
				Type:       component.SpawnType(spawn),
			})
		}, nil
	})
	v.Effect("SpawnSpecies", func(a *spawnSpeciesArgs) (func(*behaviorCtx), error) {
		i := slices.IndexFunc(spawnRequests, func(r struct {
			species component.SpeciesType
			request func(x, y int) (event.EventType, any)
		}) bool {
			return component.SpeciesNames[r.species] == a.Species
		})
		if i < 0 {
			return nil, fmt.Errorf("species %q is not one of swarm, quasar", a.Species)
		}
		request := spawnRequests[i].request
		return func(c *behaviorCtx) {
			if pos, ok := c.sys.world.Positions.GetPosition(c.entity); ok {
				c.sys.world.PushEvent(request(pos.X+a.X, pos.Y+a.Y))
			}
		}, nil
	})
	v.Names(event.PayloadNames)
	v.Emit("EmitEvent", func(names *dialect.Names, name string, raw any) (func(*behaviorCtx), error) {
		if name == "" {
			return nil, errors.New("requires 'event' field")
		}
		et, ok := event.GetEventType(name)
		if !ok {
			return nil, fmt.Errorf("unknown event type '%s'", name)
		}
		payload, err := event.AuthoredPayload(names, et, raw)
		if err != nil {
			return nil, err
		}
		switch event.ClassOf(et) {
		case event.ClassShared:
			return func(c *behaviorCtx) { c.sys.world.PushEvent(et, payload) }, nil
		case event.ClassLocal:
			return func(c *behaviorCtx) { c.sys.world.PushLocal(et, payload) }, nil
		}
		return nil, fmt.Errorf("event '%s' is %s; EmitEvent takes shared or local events", name, event.ClassOf(et))
	})

	// --- Sustained: every tick the state holds ---
	v.Sustained("Home", does(func(c *behaviorCtx) {
		// A knocked-back body flies before it steers again
		if c.combat.RemainingKineticImmunity > 0 {
			return
		}
		k := &c.kinetic.Kinetic
		x, y, _ := ResolveMovementTarget(c.sys.world, c.entity, c.kinetic)
		turn := physics.TurnSeverity(k, x, y, parameter.NavCorneringBase, 1.0)
		physics.ApplyHoming(k, x, y, &c.proto.Homing, c.dtSec)
		if turn > 0 {
			physics.ApplyLinearDrag(k, turn*parameter.NavCorneringBrake, c.dtSec)
		}
	}))
	v.Sustained("ChargeToStored", does(func(c *behaviorCtx) {
		m := c.behavior.Machine
		if m.Duration <= 0 {
			return
		}
		remaining := max(m.Duration-m.Elapsed, parameter.BehaviorChargeRemainingMin).Seconds()
		x, y := vmath.Point{X: c.behavior.TargetX, Y: c.behavior.TargetY}.Center()
		c.kinetic.VelX = (x - c.kinetic.PreciseX) / remaining
		c.kinetic.VelY = (y - c.kinetic.PreciseY) / remaining
	}))
	v.Sustained("Drag", func(a *dragArgs) (func(*behaviorCtx), error) {
		if a.Rate < 0 {
			return nil, fmt.Errorf("rate %v is negative", a.Rate)
		}
		return func(c *behaviorCtx) { physics.ApplyLinearDrag(&c.kinetic.Kinetic, a.Rate, c.dtSec) }, nil
	})
	v.Sustained("Freeze", does(func(c *behaviorCtx) {
		c.kinetic.VelX, c.kinetic.VelY = 0, 0
		c.frozen = true
	}))

	v.Flag("Enraged", behaviorEnraged)
	v.Flag("Shielded", behaviorShielded)
	v.Flag("Beam", behaviorBeam)
	v.Flag("ChargeLine", behaviorChargeLine)
	v.Flag("Landing", behaviorLanding)
	v.Integrate(func(c *behaviorCtx) { c.sys.integrate(c) })
	v.Entered(func(c *behaviorCtx) { c.probed = false })
	return v
}
