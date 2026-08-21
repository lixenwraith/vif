package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// DeathSystem routes death requests through protection checks and effect emission
// Game entities route through here; effect entities bypass via direct DeathComponent
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

	enabled bool
}

func NewDeathSystem(world *engine.World) engine.System {
	// res := engine.GetResourceStore(world)
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

	s.Init()
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
		event.EventGameResetRequest,
	}
}

func (s *DeathSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		if ev.Type == event.EventDeathBatch {
			s.statDisabledRejects.Add(1)
			if p, ok := ev.Payload.(*event.DeathRequestPayload); ok {
				event.ReleaseDeathRequest(p)
			}
		}
		return
	}

	switch ev.Type {
	case event.EventDeathBatch:
		if p, ok := ev.Payload.(*event.DeathRequestPayload); ok {
			s.processBatch(p)
		} else {
			s.statPayloadRejects.Add(1)
		}
	}
}

func (s *DeathSystem) emitEffect(entity core.Entity, effectEvent event.EventType) {
	entityPos, ok := s.world.Positions.GetPosition(entity)
	if !ok {
		s.statMissingEffectData.Add(1)
		return
	}

	// Fadeout handles its own data extraction from WallComponent
	if effectEvent == event.EventFadeoutSpawnOne {
		if wallComp, ok := s.world.Components.Wall.GetPtr(entity); ok {
			s.world.PushEvent(event.EventFadeoutSpawnOne, &event.FadeoutSpawnPayload{
				X:       entityPos.X,
				Y:       entityPos.Y,
				Char:    wallComp.Rune,
				FgColor: wallComp.FgColor,
				BgColor: wallComp.BgColor,
			})
		} else {
			s.statMissingEffectData.Add(1)
		}
		return
	}

	// Extract char: glyph first, sigil fallback
	var char rune
	var level component.GlyphLevel
	if glyphComp, ok := s.world.Components.Glyph.GetPtr(entity); ok {
		char = glyphComp.Rune
		level = glyphComp.Level
	} else if sigilComp, ok := s.world.Components.Sigil.GetPtr(entity); ok {
		char = sigilComp.Rune
	} else {
		s.statMissingEffectData.Add(1)
		return
	}

	switch effectEvent {
	case event.EventFlashSpawnOneRequest:
		s.world.PushEvent(event.EventFlashSpawnOneRequest, &event.FlashRequestPayload{
			X:    entityPos.X,
			Y:    entityPos.Y,
			Char: char,
		})

	case event.EventBlossomSpawnOne:
		s.world.PushEvent(event.EventBlossomSpawnOne, &event.BlossomSpawnPayload{
			X:             entityPos.X,
			Y:             entityPos.Y,
			Char:          char,
			SkipStartCell: true,
		})

	case event.EventDecaySpawnOne:
		s.world.PushEvent(event.EventDecaySpawnOne, &event.DecaySpawnPayload{
			X:             entityPos.X,
			Y:             entityPos.Y,
			Char:          char,
			SkipStartCell: true,
		})

	case event.EventDustSpawnOneRequest:
		s.world.PushEvent(event.EventDustSpawnOneRequest, &event.DustSpawnOneRequestPayload{
			X:     entityPos.X,
			Y:     entityPos.Y,
			Char:  char,
			Level: level,
		})
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

// processBatch routes batch death requests through the generic pipeline
func (s *DeathSystem) processBatch(p *event.DeathRequestPayload) {
	defer event.ReleaseDeathRequest(p)
	s.statBatchCount.Add(1)
	s.statBatchEntities.Add(int64(len(p.Entities)))
	storeMax(s.statBatchSizeMax, int64(len(p.Entities)))

	if p.EffectEvent == 0 {
		s.statBatchSilent.Add(1)
		s.processBatchSilent(p.Entities)
		return
	}

	switch p.EffectEvent {
	case event.EventFlashSpawnOneRequest:
		s.statBatchFlash.Add(1)
		processBatchWith(s, event.FlashBatchPool, event.EventFlashSpawnBatchRequest, p.Entities, s.extractFlash)
	case event.EventBlossomSpawnOne:
		s.statBatchBlossom.Add(1)
		processBatchWith(s, event.BlossomBatchPool, event.EventBlossomSpawnBatch, p.Entities, s.extractBlossom)
	case event.EventDecaySpawnOne:
		s.statBatchDecay.Add(1)
		processBatchWith(s, event.DecayBatchPool, event.EventDecaySpawnBatch, p.Entities, s.extractDecay)
	case event.EventFadeoutSpawnOne:
		s.statBatchFadeout.Add(1)
		processBatchWith(s, event.FadeoutBatchPool, event.EventFadeoutSpawnBatch, p.Entities, s.extractFadeout)
	case event.EventDustSpawnOneRequest:
		s.statBatchDust.Add(1)
		processBatchWith(s, event.DustBatchPool, event.EventDustSpawnBatchRequest, p.Entities, s.extractDust)
	default:
		s.statBatchOther.Add(1)
		for _, entity := range p.Entities {
			s.markForDeath(entity, p.EffectEvent)
		}
	}
}

// markForDeath is the batch processor's fallback for effects without a
// specialized extractor. Known effects use the two-pass batch path above.
func (s *DeathSystem) markForDeath(entity core.Entity, effect event.EventType) {
	if entity == 0 {
		s.statZeroRejects.Add(1)
		return
	}
	_, existed := s.world.GetComponentMask(entity)
	if !existed {
		s.statMissingEntities.Add(1)
	}
	if s.isProtected(entity) {
		return
	}
	if effect != 0 {
		s.emitEffect(entity, effect)
	}
	s.world.DestroyEntity(entity)
	if existed {
		s.statKilled.Add(1)
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
		s.world.PushEvent(eventType, batch)
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

func (s *DeathSystem) extractFlash(entity core.Entity) (event.FlashSpawnEntry, bool) {
	x, y, char, _, ok := s.extractPosChar(entity)
	if !ok {
		return event.FlashSpawnEntry{}, false
	}
	return event.FlashSpawnEntry{X: x, Y: y, Char: char}, true
}

func (s *DeathSystem) extractBlossom(entity core.Entity) (event.BlossomSpawnEntry, bool) {
	x, y, char, _, ok := s.extractPosChar(entity)
	if !ok {
		return event.BlossomSpawnEntry{}, false
	}
	return event.BlossomSpawnEntry{X: x, Y: y, Char: char, SkipStartCell: true}, true
}

func (s *DeathSystem) extractDecay(entity core.Entity) (event.DecaySpawnEntry, bool) {
	x, y, char, _, ok := s.extractPosChar(entity)
	if !ok {
		return event.DecaySpawnEntry{}, false
	}
	return event.DecaySpawnEntry{X: x, Y: y, Char: char, SkipStartCell: true}, true
}

func (s *DeathSystem) extractDust(entity core.Entity) (event.DustSpawnEntry, bool) {
	x, y, char, level, ok := s.extractPosChar(entity)
	if !ok {
		return event.DustSpawnEntry{}, false
	}
	return event.DustSpawnEntry{X: x, Y: y, Char: char, Level: level}, true
}

func (s *DeathSystem) extractFadeout(entity core.Entity) (event.FadeoutSpawnEntry, bool) {
	pos, ok := s.world.Positions.GetPosition(entity)
	if !ok {
		return event.FadeoutSpawnEntry{}, false
	}
	wallComp, ok := s.world.Components.Wall.GetPtr(entity)
	if !ok {
		return event.FadeoutSpawnEntry{}, false
	}
	return event.FadeoutSpawnEntry{
		X: pos.X, Y: pos.Y,
		Char:    wallComp.Rune,
		FgColor: wallComp.FgColor,
		BgColor: wallComp.BgColor,
	}, true
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
	killed := int64(0)
	for _, entity := range s.destroyBuf {
		if _, ok := s.world.GetComponentMask(entity); ok {
			killed++
		} else {
			s.statMissingEntities.Add(1)
		}
	}
	s.world.DestroyEntitiesBatch(s.destroyBuf)
	s.statKilled.Add(killed)
}
