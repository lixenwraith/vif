package system

import (
	"sync/atomic"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
)

// DeathSystem routes death batches through protection checks and effect emission;
// entities the crop cleanup tags with DeathComponent are destroyed silently in Update
type DeathSystem struct {
	world *engine.World

	// Reusable buffer for two-pass batch processing (reset each call)
	destroyBuf []core.Entity
	buffers    bufferTelemetry

	statKilled            *atomic.Int64
	statTagged            *atomic.Int64
	statBatchCount        *atomic.Int64
	statBatchEntities     *atomic.Int64
	statBatchSizeMax      *atomic.Int64
	statBatchSilent       *atomic.Int64
	statBatchFlash        *atomic.Int64
	statBatchBlossom      *atomic.Int64
	statBatchDecay        *atomic.Int64
	statBatchFadeout      *atomic.Int64
	statBatchDust         *atomic.Int64
	statBatchOther        *atomic.Int64
	statProtectedRejects  *atomic.Int64
	statZeroRejects       *atomic.Int64
	statMissingEntities   *atomic.Int64
	statMissingEffectData *atomic.Int64
	statPayloadRejects    *atomic.Int64
	statDisabledRejects   *atomic.Int64

	toggle
}

func NewDeathSystem(world *engine.World) engine.System {
	s := &DeathSystem{
		world: world,
	}

	s.destroyBuf = make([]core.Entity, 0, 256)

	reg := s.world.Resources.Status
	s.statKilled = reg.Ints.Get("death.killed")
	s.statTagged = reg.Ints.Get("death.tagged")
	s.statBatchCount = reg.Ints.Get("death.batch_count")
	s.statBatchEntities = reg.Ints.Get("death.batch_entities_total")
	s.statBatchSizeMax = reg.Ints.Get("death.batch_size_max")
	s.statBatchSilent = reg.Ints.Get("death.batch_silent")
	s.statBatchFlash = reg.Ints.Get("death.batch_flash")
	s.statBatchBlossom = reg.Ints.Get("death.batch_blossom")
	s.statBatchDecay = reg.Ints.Get("death.batch_decay")
	s.statBatchFadeout = reg.Ints.Get("death.batch_fadeout")
	s.statBatchDust = reg.Ints.Get("death.batch_dust")
	s.statBatchOther = reg.Ints.Get("death.batch_other")
	s.statProtectedRejects = reg.Ints.Get("death.protected_rejects")
	s.statZeroRejects = reg.Ints.Get("death.zero_rejects")
	s.statMissingEntities = reg.Ints.Get("death.missing_entities")
	s.statMissingEffectData = reg.Ints.Get("death.missing_effect_data")
	s.statPayloadRejects = reg.Ints.Get("death.payload_rejects")
	s.statDisabledRejects = reg.Ints.Get("death.disabled_rejects")
	s.buffers = newBufferTelemetry(reg, "death", "destroy")

	return s
}

// Init resets session state for new game
func (s *DeathSystem) Init() {
	s.destroyBuf = s.destroyBuf[:0]
	for _, stat := range []*atomic.Int64{
		s.statKilled,
		s.statTagged,
		s.statBatchCount,
		s.statBatchEntities,
		s.statBatchSizeMax,
		s.statBatchSilent,
		s.statBatchFlash,
		s.statBatchBlossom,
		s.statBatchDecay,
		s.statBatchFadeout,
		s.statBatchDust,
		s.statBatchOther,
		s.statProtectedRejects,
		s.statZeroRejects,
		s.statMissingEntities,
		s.statMissingEffectData,
		s.statPayloadRejects,
		s.statDisabledRejects,
	} {
		stat.Store(0)
	}
	s.buffers.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *DeathSystem) Name() string {
	return "death"
}

func (s *DeathSystem) Priority() int {
	return parameter.PriorityDeath
}

func (s *DeathSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventDeathBatch,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *DeathSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
		return
	}

	p, ok := ev.Payload.(*event.DeathRequestPayload)
	switch {
	case !ok:
		s.statPayloadRejects.Add(1)
	case !s.enabled:
		s.statDisabledRejects.Add(1)
		event.ReleaseDeathRequest(p)
	default:
		s.processBatch(p)
	}
}

// Update processes entities tagged with DeathComponent
func (s *DeathSystem) Update() {
	if !s.enabled {
		return
	}

	deaths := s.world.Components.Death
	if deaths.CountEntities() == 0 {
		return
	}

	// Protection checks may remove from this store, so preserve tick-start order.
	s.destroyBuf = append(s.destroyBuf[:0], deaths.Entities()...)
	s.buffers.Observe(0, len(s.destroyBuf))
	s.statTagged.Add(int64(len(s.destroyBuf)))
	s.processBatchSilent(s.destroyBuf)
	s.destroyBuf = s.destroyBuf[:0]
}

// --- Batch processing (two-pass: collect → destroy → emit) ---

// processBatch destroys a batch's entities and sends what its effect draws of them
func (s *DeathSystem) processBatch(p *event.DeathRequestPayload) {
	defer event.ReleaseDeathRequest(p)
	s.statBatchCount.Add(1)
	s.statBatchEntities.Add(int64(len(p.Entities)))
	storeMax(s.statBatchSizeMax, int64(len(p.Entities)))

	switch p.Effect {
	case component.DeathSilent:
		s.statBatchSilent.Add(1)
		s.processBatchSilent(p.Entities)
	case component.DeathFlash:
		s.statBatchFlash.Add(1)
		processBatchWith(s, event.EffectBatchPool, event.EventFlashSpawnBatchRequest, p.Entities, s.extractFlash)
	case component.DeathFadeout:
		s.statBatchFadeout.Add(1)
		processBatchWith(s, event.EffectBatchPool, event.EventFadeoutSpawnBatch, p.Entities, s.extractFadeout)
	case component.DeathParticle:
		switch p.Behavior {
		case component.ParticleDecay:
			s.statBatchDecay.Add(1)
		case component.ParticleBlossom:
			s.statBatchBlossom.Add(1)
		case component.ParticleDust:
			s.statBatchDust.Add(1)
		default:
			s.statBatchOther.Add(1)
			s.processBatchSilent(p.Entities)
			return
		}
		processBatchWith(s, event.ParticleBatchPool, event.EventParticleSpawnBatch, p.Entities,
			func(entity core.Entity) (event.ParticleSpawnEntry, bool) {
				return s.extractParticle(entity, p.Behavior)
			})
	default:
		// An effect this system draws nothing for
		s.statBatchOther.Add(1)
		s.processBatchSilent(p.Entities)
	}
}

// processBatchSilent destroys entities without effect emission using batch API
func (s *DeathSystem) processBatchSilent(entities []core.Entity) {
	if len(entities) == 0 {
		return
	}

	// Filter protected entities
	s.destroyBuf = s.destroyBuf[:0]
	for _, e := range entities {
		if e == 0 {
			s.statZeroRejects.Add(1)
			continue
		}
		if s.isProtected(e) {
			continue
		}
		s.destroyBuf = append(s.destroyBuf, e)
	}
	s.buffers.Observe(0, len(s.destroyBuf))
	s.destroyCollected()
}

// processBatchWith is the generic two-pass batch processor
// Pass 1: extract effect data from live entities, collect for destruction
// Pass 2: destroy collected entities, emit single batch event
func processBatchWith[T any](s *DeathSystem, pool *event.BatchPool[T], eventType event.EventType, entities []core.Entity, extract func(core.Entity) (T, bool)) {
	batch := pool.Acquire()
	s.destroyBuf = s.destroyBuf[:0]

	for _, entity := range entities {
		if entity == 0 {
			s.statZeroRejects.Add(1)
			continue
		}
		if s.isProtected(entity) {
			continue
		}
		// Effect data is optional; destruction is unconditional
		if entry, ok := extract(entity); ok {
			batch.Entries = append(batch.Entries, entry)
		} else {
			s.statMissingEffectData.Add(1)
		}
		s.destroyBuf = append(s.destroyBuf, entity)
	}
	s.buffers.Observe(0, len(s.destroyBuf))

	s.destroyCollected()

	if len(batch.Entries) > 0 {
		s.world.PushLocal(eventType, batch)
	} else {
		pool.Release(batch)
	}
}

// --- Batch extractors ---

func (s *DeathSystem) extractPosChar(entity core.Entity) (int, int, rune, component.GlyphLevel, bool) {
	pos, ok := s.world.Positions.GetPosition(entity)
	if !ok {
		return 0, 0, 0, 0, false
	}
	char, level, ok := s.extractCharData(entity)
	if !ok {
		return 0, 0, 0, 0, false
	}
	return pos.X, pos.Y, char, level, true
}

func (s *DeathSystem) extractFlash(entity core.Entity) (component.EffectCell, bool) {
	if x, y, char, _, ok := s.extractPosChar(entity); ok {
		return component.EffectCell{X: x, Y: y, Rune: char}, true
	}
	// A wall that draws a glyph and no background flashes it
	pos, ok := s.world.Positions.GetPosition(entity)
	wall, isWall := s.world.Components.Wall.GetPtr(entity)
	if !ok || !isWall || !wall.RenderFg || wall.Rune == 0 {
		return component.EffectCell{}, false
	}
	return component.EffectCell{X: pos.X, Y: pos.Y, Rune: wall.Rune}, true
}

func (s *DeathSystem) extractParticle(entity core.Entity, behavior component.ParticleBehavior) (event.ParticleSpawnEntry, bool) {
	x, y, char, level, ok := s.extractPosChar(entity)
	if !ok {
		return event.ParticleSpawnEntry{}, false
	}
	return event.ParticleSpawnEntry{
		Behavior: behavior, X: x, Y: y, Char: char, Level: level, SkipStartCell: true,
	}, true
}

func (s *DeathSystem) extractFadeout(entity core.Entity) (component.EffectCell, bool) {
	pos, ok := s.world.Positions.GetPosition(entity)
	if !ok {
		return component.EffectCell{}, false
	}
	wallComp, ok := s.world.Components.Wall.GetPtr(entity)
	if !ok {
		return component.EffectCell{}, false
	}
	cell := component.EffectCell{X: pos.X, Y: pos.Y, Solid: wallComp.RenderBg}
	cell.Fg, cell.Bg = wallFadeoutColors(wallComp)
	if wallComp.RenderFg {
		cell.Rune = wallComp.Rune
	}
	return cell, true
}

// wallFadeoutColors returns a wall's colours as RGB for the fadeout to blend: a
// 256-colour pattern wall holds palette indices in its colours' R bytes
func wallFadeoutColors(wall *component.WallComponent) (color.RGB, color.RGB) {
	fg, bg := wall.FgColor, wall.BgColor
	if wall.Attrs&terminal.AttrFg256 != 0 {
		fg = visual.Palette256RGB(fg.R)
	}
	if wall.Attrs&terminal.AttrBg256 != 0 {
		bg = visual.Palette256RGB(bg.R)
	}
	return fg, bg
}

// --- Shared helpers ---

// isProtected checks death protection and removes DeathComponent tag if protected
func (s *DeathSystem) isProtected(entity core.Entity) bool {
	protComp, ok := s.world.Components.Protection.GetPtr(entity)
	if !ok {
		return false
	}
	if protComp.Mask&component.ProtectFromDeath != 0 {
		// If immortal, remove tag to not process again in Update()
		s.world.Components.Death.RemoveEntity(entity, false)
		s.statProtectedRejects.Add(1)
		return true
	}
	return false
}

// extractCharData reads character rune and glyph level from entity
// Glyph first, sigil fallback
func (s *DeathSystem) extractCharData(entity core.Entity) (char rune, level component.GlyphLevel, ok bool) {
	if glyphComp, has := s.world.Components.Glyph.GetPtr(entity); has {
		return glyphComp.Rune, glyphComp.Level, true
	}
	if sigilComp, has := s.world.Components.Sigil.GetPtr(entity); has {
		return sigilComp.Rune, 0, true
	}
	return 0, 0, false
}

// destroyCollected destroys all entities in destroyBuf using batch API
func (s *DeathSystem) destroyCollected() {
	if len(s.destroyBuf) == 0 {
		return
	}
	// An owner may destroy an entity whose death batch is already queued (a bullet
	// striking on its expiry tick); destroying it again would count it twice
	live := s.destroyBuf[:0]
	for _, entity := range s.destroyBuf {
		if _, ok := s.world.GetComponentMask(entity); ok {
			live = append(live, entity)
		} else {
			s.statMissingEntities.Add(1)
		}
	}
	s.destroyBuf = live
	s.world.DestroyEntitiesBatch(s.destroyBuf)
	s.statKilled.Add(int64(len(s.destroyBuf)))
}
