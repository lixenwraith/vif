package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// DeathSystem routes death requests through protection checks and effect emission
// Game entities route through here; effect entities bypass via direct DeathComponent
type DeathSystem struct {
	world *engine.World

	// Reusable buffer for two-pass batch processing (reset each call)
	destroyBuf []core.Entity

	statKilled *atomic.Int64

	enabled bool
}

func NewDeathSystem(world *engine.World) engine.System {
	// res := engine.GetResourceStore(world)
	s := &DeathSystem{
		world: world,
	}

	s.destroyBuf = make([]core.Entity, 0, 256)

	s.statKilled = s.world.Resources.Status.Ints.Get("death.killed")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *DeathSystem) Init() {
	s.destroyBuf = s.destroyBuf[:0]
	s.statKilled.Store(0)
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
		event.EventDeathOne,
		event.EventDeathBatch,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *DeathSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
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
		return
	}

	switch ev.Type {
	case event.EventDeathOne:
		// HOT PATH: Priority check for bit-packed uint64
		if packed, ok := ev.Payload.(uint64); ok {
			// Bit-pack decode, skipping heap allocation, use event.EmitDeathOne
			entity := core.Entity(packed & 0xFFFFFFFFFFFF)
			effect := event.EventType(packed >> 48)
			s.markForDeath(entity, effect)
			return
		}

		// DEV/SAFETY PATH: Fallback for direct core.Entity calls
		if entity, ok := ev.Payload.(core.Entity); ok {
			s.markForDeath(entity, 0)
			return
		}

	case event.EventDeathBatch:
		if p, ok := ev.Payload.(*event.DeathRequestPayload); ok {
			s.processBatch(p)
		}
	}
}

// markForDeath performs protection checks, triggers effects, and DESTROYS the entity immediately. Used for singular or unoptimized batch effect processing
func (s *DeathSystem) markForDeath(entity core.Entity, effect event.EventType) {
	if entity == 0 {
		return
	}

	// 1. Protection Check
	if s.isProtected(entity) {
		return
	}

	// 2. Emit effect event
	if effect != 0 {
		s.emitEffect(entity, effect)
	}

	// 3. Immediate destruction: removes entity and its components from all stores
	s.world.DestroyEntity(entity)

	s.statKilled.Add(1)
}

func (s *DeathSystem) emitEffect(entity core.Entity, effectEvent event.EventType) {
	entityPos, ok := s.world.Positions.GetPosition(entity)
	if !ok {
		return
	}

	// Fadeout handles its own data extraction from WallComponent
	if effectEvent == event.EventFadeoutSpawnOne {
		if wallComp, ok := s.world.Components.Wall.GetComponent(entity); ok {
			s.world.PushEvent(event.EventFadeoutSpawnOne, &event.FadeoutSpawnPayload{
				X:       entityPos.X,
				Y:       entityPos.Y,
				Char:    wallComp.Rune,
				FgColor: wallComp.FgColor,
				BgColor: wallComp.BgColor,
			})
		}
		return
	}

	// Extract char: glyph first, sigil fallback
	var char rune
	var level component.GlyphLevel
	if glyphComp, ok := s.world.Components.Glyph.GetComponent(entity); ok {
		char = glyphComp.Rune
		level = glyphComp.Level
	} else if sigilComp, ok := s.world.Components.Sigil.GetComponent(entity); ok {
		char = sigilComp.Rune
	} else {
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

	deathEntities := s.world.Components.Death.GetAllEntities()
	if len(deathEntities) == 0 {
		return
	}

	for _, deathEntity := range deathEntities {
		// Route through markForDeath to ensure protection checks and visual effects are applied
		s.markForDeath(deathEntity, 0)
	}
}

// --- Batch processing (two-pass: collect → destroy → emit) ---

// processBatch routes batch death requests through the generic pipeline
func (s *DeathSystem) processBatch(p *event.DeathRequestPayload) {
	defer event.ReleaseDeathRequest(p)

	if p.EffectEvent == 0 {
		s.processBatchSilent(p.Entities)
		return
	}

	switch p.EffectEvent {
	case event.EventFlashSpawnOneRequest:
		processBatchWith(s, event.FlashBatchPool, event.EventFlashSpawnBatchRequest, p.Entities, s.extractFlash)
	case event.EventBlossomSpawnOne:
		processBatchWith(s, event.BlossomBatchPool, event.EventBlossomSpawnBatch, p.Entities, s.extractBlossom)
	case event.EventDecaySpawnOne:
		processBatchWith(s, event.DecayBatchPool, event.EventDecaySpawnBatch, p.Entities, s.extractDecay)
	case event.EventFadeoutSpawnOne:
		processBatchWith(s, event.FadeoutBatchPool, event.EventFadeoutSpawnBatch, p.Entities, s.extractFadeout)
	case event.EventDustSpawnOneRequest:
		processBatchWith(s, event.DustBatchPool, event.EventDustSpawnBatchRequest, p.Entities, s.extractDust)
	default:
		for _, entity := range p.Entities {
			s.markForDeath(entity, p.EffectEvent)
		}
	}
}

// processBatchSilent destroys entities without effect emission using batch API
func (s *DeathSystem) processBatchSilent(entities []core.Entity) {
	if len(entities) == 0 {
		return
	}

	// Filter protected entities
	toDestroy := make([]core.Entity, 0, len(entities))
	for _, e := range entities {
		if e == 0 || s.isProtected(e) {
			continue
		}
		toDestroy = append(toDestroy, e)
	}

	if len(toDestroy) == 0 {
		return
	}

	s.world.DestroyEntitiesBatch(toDestroy)
	s.statKilled.Add(int64(len(toDestroy)))
}

// processBatchWith is the generic two-pass batch processor
// Pass 1: extract effect data from live entities, collect for destruction
// Pass 2: destroy collected entities, emit single batch event
func processBatchWith[T any](s *DeathSystem, pool *event.BatchPool[T], eventType event.EventType, entities []core.Entity, extract func(core.Entity) (T, bool)) {
	batch := pool.Acquire()
	s.destroyBuf = s.destroyBuf[:0]

	for _, entity := range entities {
		if entity == 0 || s.isProtected(entity) {
			continue
		}
		entry, ok := extract(entity)
		if !ok {
			continue
		}
		batch.Entries = append(batch.Entries, entry)
		s.destroyBuf = append(s.destroyBuf, entity)
	}

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
	wallComp, ok := s.world.Components.Wall.GetComponent(entity)
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
	protComp, ok := s.world.Components.Protection.GetComponent(entity)
	if !ok {
		return false
	}
	if protComp.Mask&component.ProtectFromDeath != 0 || protComp.Mask == component.ProtectAll {
		// If immortal, remove tag to not process again in Update()
		s.world.Components.Death.RemoveEntity(entity)
		return true
	}
	return false
}

// extractCharData reads character rune and glyph level from entity
// Glyph first, sigil fallback
func (s *DeathSystem) extractCharData(entity core.Entity) (char rune, level component.GlyphLevel, ok bool) {
	if glyphComp, has := s.world.Components.Glyph.GetComponent(entity); has {
		return glyphComp.Rune, glyphComp.Level, true
	}
	if sigilComp, has := s.world.Components.Sigil.GetComponent(entity); has {
		return sigilComp.Rune, 0, true
	}
	return 0, 0, false
}

// destroyCollected destroys all entities in destroyBuf using batch API
func (s *DeathSystem) destroyCollected() {
	if len(s.destroyBuf) == 0 {
		return
	}
	s.world.DestroyEntitiesBatch(s.destroyBuf)
	s.statKilled.Add(int64(len(s.destroyBuf)))
}