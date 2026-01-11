package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/status"
)

const diagnosticsSampleInterval = 100

// DiagnosticsSystem collects ECS telemetry for memory leak detection
type DiagnosticsSystem struct {
	world *engine.World

	tickCounter int64

	// Store counts
	statPositionCount *atomic.Int64
	statGlyphCount    *atomic.Int64
	statSigilCount    *atomic.Int64
	statMemberCount   *atomic.Int64
	statHeaderCount   *atomic.Int64
	statNuggetCount   *atomic.Int64
	statDeathCount    *atomic.Int64
	statShieldCount   *atomic.Int64
	statPingCount     *atomic.Int64
	statBoostCount    *atomic.Int64

	// Grid metrics
	statGridWidth         *atomic.Int64
	statGridHeight        *atomic.Int64
	statGridCellsTotal    *atomic.Int64
	statGridCellsOccupied *atomic.Int64
	statGridEntitiesTotal *atomic.Int64
	statGridMaxOccupancy  *atomic.Int64
	statGridFragmentation *status.AtomicFloat

	// Consistency checks
	statOrphanGlyph  *atomic.Int64
	statOrphanMember *atomic.Int64
	statEmptyHeader  *atomic.Int64

	// Entity lifecycle
	statEntityCreated   *atomic.Int64
	statEntityDestroyed *atomic.Int64
	statEntityLive      *atomic.Int64

	enabled bool
}

// NewDiagnosticsSystem creates a new diagnostics system
func NewDiagnosticsSystem(world *engine.World) engine.System {
	reg := world.Resources.Status

	s := &DiagnosticsSystem{
		world: world,

		// Store counts
		statPositionCount: reg.Ints.Get("store.position.count"),
		statGlyphCount:    reg.Ints.Get("store.glyph.count"),
		statSigilCount:    reg.Ints.Get("store.sigil.count"),
		statMemberCount:   reg.Ints.Get("store.member.count"),
		statHeaderCount:   reg.Ints.Get("store.header.count"),
		statNuggetCount:   reg.Ints.Get("store.nugget.count"),
		statDeathCount:    reg.Ints.Get("store.death.count"),
		statShieldCount:   reg.Ints.Get("store.shield.count"),
		statPingCount:     reg.Ints.Get("store.ping.count"),
		statBoostCount:    reg.Ints.Get("store.boost.count"),

		// Grid metrics
		statGridWidth:         reg.Ints.Get("grid.width"),
		statGridHeight:        reg.Ints.Get("grid.height"),
		statGridCellsTotal:    reg.Ints.Get("grid.cells_total"),
		statGridCellsOccupied: reg.Ints.Get("grid.cells_occupied"),
		statGridEntitiesTotal: reg.Ints.Get("grid.entities_total"),
		statGridMaxOccupancy:  reg.Ints.Get("grid.max_occupancy"),
		statGridFragmentation: reg.Floats.Get("grid.fragmentation"),

		// Consistency
		statOrphanGlyph:  reg.Ints.Get("consistency.glyph_without_position"),
		statOrphanMember: reg.Ints.Get("consistency.member_without_anchor"),
		statEmptyHeader:  reg.Ints.Get("consistency.header_without_members"),

		// Lifecycle
		statEntityCreated:   reg.Ints.Get("entity.created_total"),
		statEntityDestroyed: reg.Ints.Get("entity.destroyed_total"),
		statEntityLive:      reg.Ints.Get("entity.live_estimate"),
	}

	s.Init()
	return s
}

func (s *DiagnosticsSystem) Init() {
	s.tickCounter = 0
	s.enabled = true
}

func (s *DiagnosticsSystem) Priority() int {
	return constant.PriorityDiagnostics
}

func (s *DiagnosticsSystem) EventTypes() []event.EventType {
	return []event.EventType{event.EventGameReset}
}

func (s *DiagnosticsSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
	}
}

func (s *DiagnosticsSystem) Update() {
	if !s.enabled {
		return
	}

	s.tickCounter++

	// Sample expensive operations
	if s.tickCounter%diagnosticsSampleInterval != 0 {
		return
	}

	s.collectStoreCounts()
	s.collectGridMetrics()
	s.collectConsistencyChecks()
	s.collectLifecycleMetrics()
}

// TODO: add to code gen
func (s *DiagnosticsSystem) collectStoreCounts() {
	s.statPositionCount.Store(int64(s.world.Positions.CountEntity()))
	s.statGlyphCount.Store(int64(s.world.Components.Glyph.CountEntity()))
	s.statSigilCount.Store(int64(s.world.Components.Sigil.CountEntity()))
	s.statMemberCount.Store(int64(s.world.Components.Member.CountEntity()))
	s.statHeaderCount.Store(int64(s.world.Components.Header.CountEntity()))
	s.statNuggetCount.Store(int64(s.world.Components.Nugget.CountEntity()))
	s.statDeathCount.Store(int64(s.world.Components.Death.CountEntity()))
	s.statShieldCount.Store(int64(s.world.Components.Shield.CountEntity()))
	s.statPingCount.Store(int64(s.world.Components.Ping.CountEntity()))
	s.statBoostCount.Store(int64(s.world.Components.Boost.CountEntity()))
}

func (s *DiagnosticsSystem) collectGridMetrics() {
	width, height := s.world.Positions.GridDimensions()
	stats := s.world.Positions.GridStats()

	s.statGridWidth.Store(int64(width))
	s.statGridHeight.Store(int64(height))
	s.statGridCellsTotal.Store(int64(width * height))
	s.statGridCellsOccupied.Store(int64(stats.CellsOccupied))
	s.statGridEntitiesTotal.Store(int64(stats.EntitiesTotal))
	s.statGridMaxOccupancy.Store(int64(stats.MaxOccupancy))

	if stats.CellsOccupied > 0 {
		frag := float64(stats.EntitiesTotal) / float64(stats.CellsOccupied)
		s.statGridFragmentation.Set(frag)
	} else {
		s.statGridFragmentation.Set(0)
	}
}

func (s *DiagnosticsSystem) collectConsistencyChecks() {
	var orphanGlyph, orphanMember, emptyHeader int64

	// Glyph without Positions
	for _, e := range s.world.Components.Glyph.AllEntity() {
		if !s.world.Positions.HasEntity(e) {
			orphanGlyph++
		}
	}

	// Member without valid anchor
	for _, e := range s.world.Components.Member.AllEntity() {
		member, ok := s.world.Components.Member.GetComponent(e)
		if !ok {
			continue
		}
		if !s.world.Components.Header.HasEntity(member.HeaderEntity) {
			orphanMember++
		}
	}

	// HeaderEntity with no live members
	for _, e := range s.world.Components.Header.AllEntity() {
		header, ok := s.world.Components.Header.GetComponent(e)
		if !ok {
			continue
		}
		liveCount := 0
		for _, m := range header.MemberEntries {
			if m.Entity != 0 {
				liveCount++
			}
		}
		if liveCount == 0 {
			emptyHeader++
		}
	}

	s.statOrphanGlyph.Store(orphanGlyph)
	s.statOrphanMember.Store(orphanMember)
	s.statEmptyHeader.Store(emptyHeader)
}

func (s *DiagnosticsSystem) collectLifecycleMetrics() {
	created := s.world.CreatedCount()
	destroyed := s.world.DestroyedCount()

	s.statEntityCreated.Store(created)
	s.statEntityDestroyed.Store(destroyed)
	s.statEntityLive.Store(created - destroyed)
}