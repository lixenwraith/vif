package system

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// GlyphKey is a spawnable glyph type/level pair
type GlyphKey struct {
	Type  component.GlyphType
	Level component.GlyphLevel
}

// Spawnable types and levels; census slots are indexed by position here,
// so the enum values themselves are free to change
var glyphSpawnTypes = [...]component.GlyphType{component.GlyphBlue, component.GlyphGreen}
var glyphSpawnLevels = [...]component.GlyphLevel{component.GlyphDark, component.GlyphNormal, component.GlyphBright}

const glyphCensusSlots = len(glyphSpawnTypes) * len(glyphSpawnLevels)

// glyphCensus counts live glyphs per type/level slot
type glyphCensus [glyphCensusSlots]int

// censusSlot maps a type/level pair to its slot, -1 when not spawnable
func censusSlot(t component.GlyphType, l component.GlyphLevel) int {
	ti := -1
	for i, st := range glyphSpawnTypes {
		if st == t {
			ti = i
			break
		}
	}
	if ti < 0 {
		return -1
	}
	for i, sl := range glyphSpawnLevels {
		if sl == l {
			return ti*len(glyphSpawnLevels) + i
		}
	}
	return -1
}

// glyphPlacement is a staged entity awaiting batch position commit
type glyphPlacement struct {
	entity core.Entity
	pos    component.PositionComponent
	char   rune
}

// GlyphSystem handles glyph sequence generation and spawning
type GlyphSystem struct {
	world *engine.World

	rng *vmath.FastRand

	// Glyph census
	census glyphCensus

	// Spawn timing and rate
	nextSpawnTimer time.Duration
	rateMultiplier float64 // 0.5x, 1.0x, 2.0x based on screen fill

	// Reused placement scratch
	placement []glyphPlacement

	// Lines still entering across an upwind edge while glyphs drift
	entering []engine.EntryLine

	// Drift as the environment publishes it; read only, for the entry edge, the pace
	// and the kind of text placed
	statDriftActive    *atomic.Bool
	statDriftDirection *status.AtomicFloat
	statDriftSpeed     *status.AtomicFloat
	statDriftScenery   *atomic.Bool

	// Cached metric pointers
	statEnabled     *atomic.Bool
	statDensity     *status.AtomicFloat
	statRateMult    *status.AtomicFloat
	statNextSpawnMS *atomic.Int64
	statOrphanGlyph *atomic.Int64
	buffers         bufferTelemetry

	toggle
}

// NewGlyphSystem creates a new glyph system
func NewGlyphSystem(world *engine.World) engine.System {
	s := &GlyphSystem{
		world: world,
	}

	// Cache metric pointers
	s.statEnabled = world.Resources.Status.Bools.Get("glyph.enabled")
	s.statDensity = world.Resources.Status.Floats.Get("glyph.density")
	s.statRateMult = world.Resources.Status.Floats.Get("glyph.rate_mult")
	s.statNextSpawnMS = world.Resources.Status.Ints.Get("glyph.next_spawn_ms")
	s.statOrphanGlyph = world.Resources.Status.Ints.Get("glyph.orphan_glyph")
	s.buffers = newBufferTelemetry(world.Resources.Status, "glyph", "placement")
	s.statDriftActive = world.Resources.Status.Bools.Get("environment.drift_active")
	s.statDriftDirection = world.Resources.Status.Floats.Get("environment.drift_direction")
	s.statDriftSpeed = world.Resources.Status.Floats.Get("environment.drift_speed")
	s.statDriftScenery = world.Resources.Status.Bools.Get("environment.drift_scenery")

	return s
}

// Init resets session state for new game
func (s *GlyphSystem) Init() {
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	s.census = glyphCensus{}

	s.nextSpawnTimer = time.Duration(0)
	s.rateMultiplier = 1.0
	s.placement = s.placement[:0]
	s.entering = s.entering[:0]
	s.publishEntering()
	s.statEnabled.Store(true)
	s.statDensity.Set(0)
	s.statRateMult.Set(1.0)
	s.statNextSpawnMS.Store(0)
	s.statOrphanGlyph.Store(0)
	s.buffers.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *GlyphSystem) Name() string {
	return "glyph"
}

// Priority returns the system's priority
func (s *GlyphSystem) Priority() int {
	return parameter.PriorityGlyph
}

// EventTypes returns the event types SpawnSystem handles
func (s *GlyphSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMetaSystemCommandRequest,
	}
}

// HandleEvent processes spawn configuration events
func (s *GlyphSystem) HandleEvent(ev event.GameEvent) {

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
				s.statEnabled.Store(payload.Enabled)
				if !s.enabled {
					// A line part-way in would stand at the edge; it is dropped
					s.entering = s.entering[:0]
					s.publishEntering()
				}
			}
		}
	}

	if !s.enabled {
		return
	}
}

// Update runs the spawn system logic
func (s *GlyphSystem) Update() {
	if !s.enabled {
		return
	}
	s.feedEntering()

	config := s.world.Resources.Config

	// Calculate current density and update rate multiplier
	glyphCount := s.countPlacedKind()
	screenCapacity := config.MapWidth * config.MapHeight
	density := s.calculateDensity(glyphCount, screenCapacity)
	s.updateRateMultiplier(density)

	// Check if spawn is due
	dt := s.world.Resources.Time.DeltaTime
	s.nextSpawnTimer -= dt

	// Update metrics
	s.statDensity.Set(density)
	s.statRateMult.Set(s.rateMultiplier)
	s.statNextSpawnMS.Store(s.nextSpawnTimer.Milliseconds())

	maybeNewSpawnTimer := s.calculateNextSpawn()

	if s.nextSpawnTimer <= 0 || maybeNewSpawnTimer <= s.nextSpawnTimer {
		s.nextSpawnTimer = maybeNewSpawnTimer
		// Generate and spawn a new sequence of glyphs
		s.spawnGlyphs()
	}
	s.publishEntering()
}

// calculateDensity returns entity density as fraction of screen capacity
func (s *GlyphSystem) calculateDensity(entityCount, screenCapacity int) float64 {
	if screenCapacity <= 0 {
		return 0
	}
	return float64(entityCount) / float64(screenCapacity)
}

// updateRateMultiplier adjusts spawn rate by map density against the parameter thresholds
func (s *GlyphSystem) updateRateMultiplier(density float64) {
	if density < parameter.SpawnDensityLowThreshold {
		s.rateMultiplier = parameter.SpawnRateFast
	} else if density > parameter.SpawnDensityHighThreshold {
		s.rateMultiplier = parameter.SpawnRateSlow
	} else {
		s.rateMultiplier = parameter.SpawnRateNormal
	}
}

// calculateNextSpawn is the interval to the next spawn, shorter as the map empties
// and as a drift outpaces DriftFillSpeed, carrying text off the map sooner
func (s *GlyphSystem) calculateNextSpawn() time.Duration {
	baseDelay := time.Duration(parameter.SpawnIntervalMs) * time.Millisecond
	fill := max(1, s.statDriftSpeed.Get()/parameter.DriftFillSpeed)
	return time.Duration(float64(baseDelay) / s.rateMultiplier / fill)
}

// countPlacedKind counts the glyphs of the kind placeLine stamps now that a player
// sees, scenery or typeable text, so each keeps its own fill where both live side by
// side and scenery passing behind rock or a wall fills nothing. Scenery is counted off
// the protection store, which a map without it keeps small.
func (s *GlyphSystem) countPlacedKind() int {
	glyphs, placing := s.world.Components.Glyph, s.statDriftScenery.Load()
	scenery, shown := 0, 0
	s.world.Components.Protection.Each(func(e core.Entity, p *component.ProtectionComponent) bool {
		if p.Mask&component.ProtectFromTyping == 0 || !glyphs.HasEntity(e) {
			return true
		}
		scenery++
		if pos, ok := s.world.Positions.GetPosition(e); placing && ok && !s.hides(pos.X, pos.Y) {
			shown++
		}
		return true
	})
	if placing {
		return shown
	}
	return glyphs.CountEntities() - scenery
}

// hides reports a cell of rock or a wall that keeps text out and scenery behind it
func (s *GlyphSystem) hides(x, y int) bool {
	return s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockSpawn)
}

// updateCensus counts the glyphs of the placed kind a player sees per spawn slot, as
// countPlacedKind counts them, called once per spawn, O(n)
func (s *GlyphSystem) updateCensus() {
	s.census = glyphCensus{}
	scenery := s.statDriftScenery.Load()
	var orphanGlyph int64
	s.world.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
		pos, ok := s.world.Positions.GetPosition(e)
		if !ok {
			orphanGlyph++
			return true
		}
		if s.world.Positions.IsScenery(e) != scenery || scenery && s.hides(pos.X, pos.Y) {
			return true
		}
		if slot := censusSlot(g.Type, g.Level); slot >= 0 {
			s.census[slot]++
		}
		return true
	})
	s.statOrphanGlyph.Store(orphanGlyph)
}

// nextGlyphToSpawn returns the least represented type/level pair on the map.
// Ties break uniformly at random so equal counts don't lock onto one slot
func (s *GlyphSystem) nextGlyphToSpawn() GlyphKey {
	best, ties := 0, 1
	for slot := 1; slot < len(s.census); slot++ {
		switch {
		case s.census[slot] < s.census[best]:
			best, ties = slot, 1
		case s.census[slot] == s.census[best]:
			ties++
			if s.rng.Intn(ties) == 0 {
				best = slot
			}
		}
	}
	return GlyphKey{
		Type:  glyphSpawnTypes[best/len(glyphSpawnLevels)],
		Level: glyphSpawnLevels[best%len(glyphSpawnLevels)],
	}
}

// spawnGlyphs pulls the next content block and places its lines
func (s *GlyphSystem) spawnGlyphs() {
	res := s.world.Resources.Content
	if res == nil || res.Provider == nil {
		return
	}

	block, ok := res.Provider.NextBlock()
	if !ok {
		return
	}

	s.updateCensus()
	key := s.nextGlyphToSpawn()

	for _, line := range block.Lines {
		s.placeLine(line, key.Type, key.Level)
	}
}

// placeLine attempts to place a single line on the map, or while glyphs drift starts
// it entering across an upwind edge. Lines wider than the map are cropped; the map
// width is the only crop policy.
func (s *GlyphSystem) placeLine(line string, glyphType component.GlyphType, glyphLevel component.GlyphLevel) bool {
	config := s.world.Resources.Config

	// Read in place, a rune a column, so a try that places nothing allocates nothing
	text, lineLength := line, 0
	for i := range line {
		if lineLength == config.MapWidth {
			text = line[:i]
			break
		}
		lineLength++
	}
	if lineLength == 0 || s.world.Resources.Player.Count() == 0 {
		return false
	}
	if s.statDriftActive.Load() {
		return s.enterLine(text, lineLength, glyphType, glyphLevel)
	}

	for range parameter.MaxPlacementTries {
		row := s.rng.Intn(config.MapHeight)
		startCol := s.rng.Intn(config.MapWidth - lineLength + 1)
		if s.nearCursor(row, startCol, lineLength) || s.blocked(row, startCol, lineLength) {
			continue
		}

		// 1. Stage entities, skipping spaces
		s.placement = s.placement[:0]
		col := startCol
		for _, r := range text {
			if r != ' ' {
				s.placement = append(s.placement, glyphPlacement{
					entity: s.world.CreateEntity(core.DomainPlayer),
					pos:    component.PositionComponent{X: col, Y: row},
					char:   r,
				})
			}
			col++
		}
		s.buffers.Observe(0, len(s.placement))

		// 2. Batch position validation and commit
		batch := s.world.Positions.BeginBatch(len(s.placement))
		for _, gp := range s.placement {
			batch.Add(gp.entity, gp.pos)
		}
		if err := batch.Commit(); err != nil {
			for _, gp := range s.placement {
				s.world.DestroyEntity(gp.entity)
			}
			continue
		}

		// 3. Attach glyph components; text placed under a scenery drift is scenery for life
		scenery := s.statDriftScenery.Load()
		for _, gp := range s.placement {
			s.attachGlyph(gp.entity, gp.char, glyphType, glyphLevel, scenery)
		}
		return true
	}

	return false
}

// attachGlyph makes a placed entity a glyph, and scenery for life when placed as such
func (s *GlyphSystem) attachGlyph(e core.Entity, r rune, glyphType component.GlyphType, glyphLevel component.GlyphLevel, scenery bool) {
	s.world.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: r, Type: glyphType, Level: glyphLevel})
	if scenery {
		s.world.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectScenery})
	}
}

// nearCursor reports a run of cells on a row inside any cursor's exclusion region:
// interval overlap on X, distance on Y
func (s *GlyphSystem) nearCursor(row, startCol, length int) bool {
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		cursorPos, ok := s.world.Positions.GetPosition(cursor)
		if ok && vmath.IntAbs(row-cursorPos.Y) <= parameter.CursorExclusionY &&
			startCol <= cursorPos.X+parameter.CursorExclusionX &&
			startCol+length > cursorPos.X-parameter.CursorExclusionX {
			return true
		}
	}
	return false
}

// blocked reports a run of cells on a row that leaves the map or that a wall or rock
// keeps text out of
func (s *GlyphSystem) blocked(row, startCol, length int) bool {
	for i := range length {
		if s.world.Positions.IsBlocked(startCol+i, row, component.WallBlockSpawn) {
			return true
		}
	}
	return false
}

// enterLine starts text, length runes, entering across an upwind edge, each drawn in
// proportion to the drift crossing it: across a side a rune at a time, the last first
// from the left, across an end a whole row at once. The cells it meets the map at must
// be free and clear of cursors and of a line of its level still entering; it starts as
// far past them as its layer stepped last tick, so its first frame shows it at the edge.
func (s *GlyphSystem) enterLine(text string, length int, glyphType component.GlyphType, glyphLevel component.GlyphLevel) bool {
	config := s.world.Resources.Config
	width, height := config.MapWidth, config.MapHeight
	sin, cos := math.Sincos(s.statDriftDirection.Get())
	side := math.Abs(cos) * float64(height)
	end := config.CellAspect.FromCircular(math.Abs(sin)) * float64(width)
	fromSide := s.rng.Float64()*(side+end) < side
	heading := [2]int{int(vmath.Sign(cos)), int(vmath.Sign(sin))}
	line := engine.EntryLine{Dir: 1, Type: glyphType, Level: glyphLevel, Scenery: s.statDriftScenery.Load()}
	if fromSide && cos > 0 {
		line.Dir = -1
	}

	// Where it first meets the map: one cell of a side, or its row along an end
	step := s.world.Resources.Drift.Step[glyphLevel]
	for range parameter.MaxPlacementTries {
		var col, row, cells int
		switch {
		case fromSide && cos < 0:
			col, row, cells = width-1, s.rng.Intn(height), 1
			line.X, line.Y = col+max(0, -step[0]), row
		case fromSide:
			col, row, cells = 0, s.rng.Intn(height), 1
			line.X, line.Y = col-max(0, step[0]), row
		case sin > 0:
			col, row, cells = s.rng.Intn(width-length+1), 0, length
			line.X, line.Y = col, row-max(0, step[1])
		default:
			col, row, cells = s.rng.Intn(width-length+1), height-1, length
			line.X, line.Y = col, row+max(0, -step[1])
		}
		if s.nearCursor(row, col, cells) || s.blocked(row, col, cells) || s.occupied(row, col, cells) ||
			s.overlaysEntering(&line, length) {
			continue
		}
		line.Text = text
		if line.Dir < 0 {
			runes := []rune(text)
			slices.Reverse(runes)
			line.Text = string(runes)
		}
		if s.feed(&line, heading) {
			s.entering = append(s.entering, line)
		}
		return true
	}
	return false
}

// occupied reports a run of cells on a row that any entity stands in
func (s *GlyphSystem) occupied(row, startCol, length int) bool {
	for i := range length {
		if s.world.Positions.HasAnyEntityAt(startCol+i, row) {
			return true
		}
	}
	return false
}

// overlaysEntering reports a line of the same level still entering on line's row
// whose runes would take the same cells as line's length runes
func (s *GlyphSystem) overlaysEntering(line *engine.EntryLine, length int) bool {
	lo, hi := entrySpan(line.X, line.Dir, length)
	for i := range s.entering {
		other := &s.entering[i]
		if other.Level != line.Level || other.Y != line.Y {
			continue
		}
		if otherLo, otherHi := entrySpan(other.X, other.Dir, utf8.RuneCountInString(other.Text)); lo <= otherHi && otherLo <= hi {
			return true
		}
	}
	return false
}

// entrySpan is the columns length runes take from x on along dir
func entrySpan(x, dir, length int) (lo, hi int) {
	if dir < 0 {
		return x - length + 1, x
	}
	return x, x + length - 1
}

// feedEntering moves each entering line on by the step its layer took last tick and
// places what that brought onto the map
func (s *GlyphSystem) feedEntering() {
	var heading [2]int
	if s.statDriftActive.Load() {
		sin, cos := math.Sincos(s.statDriftDirection.Get())
		heading = [2]int{int(vmath.Sign(cos)), int(vmath.Sign(sin))}
	}
	drift := s.world.Resources.Drift
	kept := s.entering[:0]
	for _, line := range s.entering {
		step := drift.Step[line.Level]
		line.X, line.Y = line.X+step[0], line.Y+step[1]
		if s.feed(&line, heading) {
			kept = append(kept, line)
		}
	}
	s.entering = kept
}

// feed places the runes of an entering line its layer has brought onto the map and
// reports whether any are still on their way, off the map where the drift's heading,
// a sign per axis, carries them onto it. A space leaves its cell empty, and a rune the
// drift no longer carries in, as once it stops or turns, is lost.
func (s *GlyphSystem) feed(line *engine.EntryLine, heading [2]int) bool {
	config := s.world.Resources.Config
	for line.Text != "" {
		r, size := utf8.DecodeRuneInString(line.Text)
		if !s.world.Positions.IsOutOfBounds(line.X, line.Y) {
			if r != ' ' {
				s.enterRune(line, r)
			}
		} else if closing(line.X, config.MapWidth, heading[0]) && closing(line.Y, config.MapHeight, heading[1]) {
			return true
		}
		line.Text = line.Text[size:]
		line.X += line.Dir
	}
	return false
}

// enterRune places an entering rune in its cell as the drift carries a glyph there:
// rock or a wall that blocks spawns keeps typeable text out, leaving a hole, and
// scenery enters behind it
func (s *GlyphSystem) enterRune(line *engine.EntryLine, r rune) {
	if !line.Scenery && s.hides(line.X, line.Y) {
		return
	}
	e := s.world.CreateEntity(core.DomainPlayer)
	s.world.Positions.SetPosition(e, component.PositionComponent{X: line.X, Y: line.Y})
	s.attachGlyph(e, r, line.Type, line.Level, line.Scenery)
}

// closing reports a coordinate on a span of n cells, or off it and heading toward it
func closing(c, n, heading int) bool {
	return c >= 0 && c < n || c < 0 && heading > 0 || c >= n && heading < 0
}

// publishEntering leaves the lines still entering for the glyph renderer
func (s *GlyphSystem) publishEntering() {
	entry := s.world.Resources.Entry
	entry.Lines = append(entry.Lines[:0], s.entering...)
}

type glyphLocal struct {
	Census    glyphCensus   `json:"census"`
	NextSpawn time.Duration `json:"next_spawn"`
	Rate      float64       `json:"rate"`
	// Lines still entering, each where its layer stands after the tick's step, since
	// a copy's environment holds no step of its own yet
	Entering []engine.EntryLine `json:"entering,omitempty"`
}

func (s *GlyphSystem) SaveLocal() ([]byte, error) {
	entering := make([]engine.EntryLine, len(s.entering))
	for i, line := range s.entering {
		step := s.world.Resources.Drift.Step[line.Level]
		line.X, line.Y = line.X+step[0], line.Y+step[1]
		entering[i] = line
	}
	return json.Marshal(glyphLocal{Census: s.census, NextSpawn: s.nextSpawnTimer, Rate: s.rateMultiplier, Entering: entering})
}

func (s *GlyphSystem) LoadLocal(data []byte) error {
	var l glyphLocal
	if err := json.Unmarshal(data, &l); err != nil {
		return fmt.Errorf("glyph: %w", err)
	}
	for _, line := range l.Entering {
		if censusSlot(line.Type, line.Level) < 0 || line.Dir != 1 && line.Dir != -1 {
			return fmt.Errorf("glyph: entering line of type %d, level %d, heading %d", line.Type, line.Level, line.Dir)
		}
	}
	s.census, s.nextSpawnTimer, s.rateMultiplier = l.Census, l.NextSpawn, l.Rate
	s.entering = append(s.entering[:0], l.Entering...)
	s.publishEntering()
	return nil
}
