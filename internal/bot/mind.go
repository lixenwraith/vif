package bot

import (
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/std"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// mind is what a graph's actions and guards see. The world is read only inside
// Driver.Step's lock and never written: actions queue intents, which reach it
// through the router after the lock is released.
type mind struct {
	ctx   *engine.GameContext
	rng   *vmath.FastRand
	queue *intentQueue

	// room is how many more intents this step releases before its tick. An action
	// that reads the world acts only when all it queues fits, or it would land on a
	// world that has moved on.
	room int

	// Nearest target per class, found once per update however many ask
	seen  [targetCount]sighting
	fresh [targetCount]bool
}

// target is a class of thing a graph points at or asks about
type target uint8

const (
	targetGlyph target = iota
	targetGold
	targetNugget
	targetSpecies
	targetCursor
	targetThreat
	targetButton
	targetRandom
	targetCount
)

// targets is indexed by target: the name a graph uses and the scan that visits
// every entity of the class. A button is a rectangle a scenario lays out, sighted
// by button, and random a cell; neither is an entity to scan.
var targets = [targetCount]struct {
	name string
	scan func(m *mind, visit func(core.Entity))
}{
	targetGlyph: {"glyph", func(m *mind, visit func(core.Entity)) {
		c := m.ctx.World.Components
		c.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
			if g.Type != component.GlyphGold && !c.Member.HasEntity(e) {
				visit(e)
			}
			return true
		})
	}},
	targetGold: {"gold", func(m *mind, visit func(core.Entity)) {
		m.ctx.World.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
			if g.Type == component.GlyphGold {
				visit(e)
			}
			return true
		})
	}},
	targetNugget: {"nugget", func(m *mind, visit func(core.Entity)) {
		for _, e := range m.ctx.World.Components.Nugget.Entities() {
			visit(e)
		}
	}},
	targetSpecies: {"species", scanSpecies},
	targetCursor: {"cursor", func(m *mind, visit func(core.Entity)) {
		own := m.ctx.World.Resources.Player.Entity
		for _, e := range m.ctx.World.Components.Cursor.Entities() {
			if e != own {
				visit(e)
			}
		}
	}},
	targetThreat: {"threat", func(m *mind, visit func(core.Entity)) {
		scanSpecies(m, visit)
		c := m.ctx.World.Components
		c.Bullet.Each(func(e core.Entity, b *component.BulletComponent) bool {
			if b.Hostile {
				visit(e)
			}
			return true
		})
		c.Missile.Each(func(e core.Entity, missile *component.MissileComponent) bool {
			if missile.Hostile {
				visit(e)
			}
			return true
		})
	}},
	targetButton: {name: "button"},
	targetRandom: {name: "random"},
}

// scanSpecies visits every combat entity but a cursor or a tower
func scanSpecies(m *mind, visit func(core.Entity)) {
	m.ctx.World.Components.Combat.Each(func(e core.Entity, c *component.CombatComponent) bool {
		if c.CombatEntityType != component.CombatEntityCursor && c.CombatEntityType != component.CombatEntityTower {
			visit(e)
		}
		return true
	})
}

// parseTarget resolves a graph's target name, refusing those its user can't take
func parseTarget(raw any, refused ...target) (target, error) {
	name, _ := raw.(string)
	for t := range targetCount {
		if targets[t].name == name && !slices.Contains(refused, t) {
			return t, nil
		}
	}
	return 0, fmt.Errorf("unknown target %q", name)
}

// buttonKey reads the key that picks one button; only the button target takes it
func buttonKey(t target, raw any) (rune, error) {
	if raw == nil || raw == "" {
		return 0, nil
	}
	if t != targetButton {
		return 0, errors.New("key applies only to the button target")
	}
	text, _ := raw.(string)
	key, size := utf8.DecodeRuneInString(text)
	if size == 0 || size != len(text) {
		return 0, fmt.Errorf("key must be one character, got %v", raw)
	}
	return key, nil
}

// sighting is the nearest target of one class to the bot's own cursor
type sighting struct {
	x, y int
	// dist2 is the squared cell distance, rows counted twice as the terminal draws
	// them; -1 for a button on a map with no cursor
	dist2 int
	found bool
}

// sight is the nearest target of a class, a button by its key
func (m *mind) sight(t target, key rune) sighting {
	if t == targetButton {
		cfg := m.ctx.World.Resources.Config
		return m.button(key, 0, 0, cfg.MapWidth-1, cfg.MapHeight-1)
	}
	return m.nearest(t)
}

// button sights the button on key, or any button for key zero, by its cells from
// x0, y0 to x1, y1: the one nearest the cursor, where dist2 zero is the cursor on
// the button, ties to the left. With no cursor on the map it is the leftmost
// button's middle cell, at no distance.
func (m *mind) button(key rune, x0, y0, x1, y1 int) sighting {
	from, cursor := m.ctx.World.LocalCursor()
	cfg := m.ctx.World.Resources.Config
	buttons := m.ctx.World.Components.Button
	count := buttons.CountEntities()
	var s sighting
	slot := 0
	buttons.Each(func(_ core.Entity, b *component.ButtonComponent) bool {
		x, y, w, h := component.ButtonRect(b.Slot, count, cfg.MapWidth, cfg.MapHeight)
		left, top, right, bottom := max(x, x0), max(y, y0), min(x+w-1, x1), min(y+h-1, y1)
		if key != 0 && b.Key != key || left > right || top > bottom {
			return true
		}
		c := sighting{x: (left + right) / 2, y: (top + bottom) / 2, dist2: -1, found: true}
		if cursor {
			c.x, c.y = min(max(from.X, left), right), min(max(from.Y, top), bottom)
			c.dist2 = dist2(c.x-from.X, c.y-from.Y)
		}
		if !s.found || c.dist2 < s.dist2 || c.dist2 == s.dist2 && b.Slot < slot {
			s, slot = c, b.Slot
		}
		return true
	})
	return s
}

// nearest finds the entity of a class closest to the cursor's predicted cell, the
// cell a person sees; ties go to store order, which is deterministic.
func (m *mind) nearest(t target) sighting {
	if m.fresh[t] {
		return m.seen[t]
	}
	m.fresh[t] = true
	var s sighting
	from, ok := m.ctx.World.LocalCursor()
	if ok {
		targets[t].scan(m, func(e core.Entity) {
			pos, ok := m.ctx.World.Positions.GetPosition(e)
			if !ok {
				return
			}
			if d := dist2(pos.X-from.X, pos.Y-from.Y); !s.found || d < s.dist2 {
				s = sighting{x: pos.X, y: pos.Y, dist2: d, found: true}
			}
		})
	}
	m.seen[t] = s
	return s
}

// glyphUnder is the rune typing would validate at the cursor: its cell's top glyph,
// as TypingSystem selects it.
func (m *mind) glyphUnder() (rune, bool) {
	pos, ok := m.ctx.World.LocalCursor()
	if !ok {
		return 0, false
	}
	return m.glyphAt(pos.X, pos.Y, false)
}

func (m *mind) glyphAt(x, y int, standalone bool) (rune, bool) {
	e, ok := m.ctx.World.Positions.TopGlyphAt(x, y)
	if !ok {
		return 0, false
	}
	g, _ := m.ctx.World.Components.Glyph.GetComponent(e)
	return g.Rune, !standalone || g.Type != component.GlyphGold && !m.ctx.World.Components.Member.HasEntity(e)
}

// push queues intents in order, spending the step's room
func (m *mind) push(intents ...input.Intent) {
	for _, intent := range intents {
		m.pushHeld(intent, 0)
	}
}

// pushHeld queues one intent; a press keeps its key down for hold
func (m *mind) pushHeld(intent input.Intent, hold int) {
	m.queue.push(queued{intent: intent, hold: hold})
	m.room--
}

// pointAt queues the pointer at the nearest target of a class, clamped to the cells
// the viewport shows, as a mouse can reach no further; the camera follows it.
func (m *mind) pointAt(p pointArgs) {
	need := 1
	if p.fire {
		need = 2
	}
	minX, minY, maxX, maxY, ok := m.ctx.World.Resources.Config.VisibleMapRect()
	if !ok || m.room < need {
		return
	}
	var x, y int
	switch p.target {
	case targetRandom:
		x, y = minX+m.rng.Intn(maxX-minX+1), minY+m.rng.Intn(maxY-minY+1)
	case targetButton:
		if x, y, ok = m.aimButton(p, minX, minY, maxX, maxY); !ok {
			return
		}
	default:
		s := m.nearest(p.target)
		if !s.found || p.away && s.dist2 == 0 {
			return
		}
		from, _ := m.ctx.World.LocalCursor()
		goalX, goalY := s.x, s.y
		if p.away {
			goalX, goalY = 2*from.X-s.x, 2*from.Y-s.y
		}
		x, y = min(max(goalX, minX), maxX), min(max(goalY, minY), maxY)
		// Start a newly acquired text run at its left edge instead of eating it backwards.
		if p.target == targetGlyph && !p.away {
			for x > minX {
				if _, ok := m.glyphAt(x-1, y, true); !ok {
					break
				}
				x--
			}
		}
		if x, y, ok = m.land(x, y, goalX, goalY); !ok {
			return
		}
		// Pinned to the view's edge in line with the threat, the mirror is the cursor's
		// own cell; flee to the shown open cell farthest from it, if that gains ground.
		if p.away && x == from.X && y == from.Y {
			x, y, ok = m.openCell(func(cx, cy int) int { return -dist2(cx-s.x, cy-s.y) })
			if !ok || dist2(x-s.x, y-s.y) <= s.dist2 {
				return
			}
		}
	}
	if p.fire {
		m.push(input.Intent{Type: input.IntentMouseLeftDown, X: x, Y: y, MapCell: true},
			input.Intent{Type: input.IntentMouseLeftUp})
		return
	}
	if pos, ok := m.ctx.World.LocalCursor(); ok && pos.X == x && pos.Y == y {
		return
	}
	m.push(input.Intent{Type: input.IntentMouseMove, X: x, Y: y, MapCell: true})
}

// aimButton is the cell PointAt aims at for a button: of its shown cells, the one
// nearest the cursor, which a click presses wherever the cursor is. A move heads
// for a button the view does not show; a click there would fire where it landed,
// and a map with no cursor has none to move.
func (m *mind) aimButton(p pointArgs, minX, minY, maxX, maxY int) (int, int, bool) {
	s := m.button(p.key, minX, minY, maxX, maxY)
	if p.fire {
		return s.x, s.y, s.found
	}
	if _, cursor := m.ctx.World.LocalCursor(); !cursor {
		return 0, 0, false
	}
	if !s.found {
		s = m.sight(targetButton, p.key)
	}
	if !s.found {
		return 0, 0, false
	}
	return m.land(min(max(s.x, minX), maxX), min(max(s.y, minY), maxY), s.x, s.y)
}

// land keeps x, y unless the cursor may not enter it, as the router refuses that
// move and the bot would re-queue it every update; it then takes the shown open cell
// nearest the goal.
func (m *mind) land(x, y, goalX, goalY int) (int, int, bool) {
	if !m.ctx.World.Positions.IsBlocked(x, y, component.WallBlockCursor) {
		return x, y, true
	}
	return m.openCell(func(cx, cy int) int { return dist2(cx-goalX, cy-goalY) })
}

// openCell is the shown cell the cursor may enter that ranks lowest, ties to the
// first in row order.
func (m *mind) openCell(rank func(x, y int) int) (x, y int, found bool) {
	minX, minY, maxX, maxY, _ := m.ctx.World.Resources.Config.VisibleMapRect()
	best := 0
	for cy := minY; cy <= maxY; cy++ {
		for cx := minX; cx <= maxX; cx++ {
			if r := rank(cx, cy); (!found || r < best) && !m.ctx.World.Positions.IsBlocked(cx, cy, component.WallBlockCursor) {
				x, y, best, found = cx, cy, r, true
			}
		}
	}
	return x, y, found
}

// dist2 is a squared cell distance with rows counted twice, as the terminal draws them
func dist2(dx, dy int) int { return dx*dx + 4*dy*dy }

// pointArgs is a compiled PointAt payload; key picks a button, and away aims at the
// target's mirror through the cursor
type pointArgs struct {
	target     target
	key        rune
	fire, away bool
}

// register installs std's generic vocabulary, its status guards reading the bot's
// own registry, whose bare keys mirror its own slot, its config guards reading the
// bot's own view, and the bot's actions and guards.
func register(m *fsm.Machine[*mind]) {
	std.Register(m, std.Host[*mind]{
		Refuse: "writes the world; a bot acts only through intents",
		ConfigInt: func(field string) (func(*mind) int64, bool) {
			read, ok := engine.PrivateConfigIntAccessor(field)
			if !ok {
				return nil, false
			}
			return func(m *mind) int64 { return read(m.ctx.World) }, true
		},
		ConfigBool: func(field string) (func(*mind) bool, bool) {
			read, ok := engine.PrivateConfigBoolAccessor(field)
			if !ok {
				return nil, false
			}
			return func(m *mind) bool { return read(m.ctx.World) }, true
		},
		StatusInt: func(m *mind, key string) (int64, bool) {
			reg := m.ctx.World.Resources.Status
			if !reg.Ints.Has(key) {
				return 0, false
			}
			return reg.Ints.Get(key).Load(), true
		},
		StatusBool: func(m *mind, key string) (bool, bool) {
			reg := m.ctx.World.Resources.Status
			if !reg.Bools.Has(key) {
				return false, false
			}
			return reg.Bools.Get(key).Load(), true
		},
	})

	queueAll := func(m *mind, args any) { m.push(args.([]input.Intent)...) }
	m.RegisterAction("Intent", func(m *mind, args any) {
		c := args.(intentChoice)
		m.pushHeld(c.draw(m.rng), c.hold)
	})
	m.RegisterActionArgs("Intent", func(_ *fsm.Machine[*mind], cfg fsm.ActionConfig, _ fsm.StateResolver) (any, error) {
		return compileIntent(cfg.Payload)
	})
	m.RegisterAction("Text", queueAll)
	m.RegisterActionArgs("Text", func(_ *fsm.Machine[*mind], cfg fsm.ActionConfig, _ fsm.StateResolver) (any, error) {
		text, err := textPayload(cfg.Payload)
		out := make([]input.Intent, 0, len(text))
		for _, char := range text {
			out = append(out, input.Intent{Type: input.IntentTextChar, Char: char, Count: 1})
		}
		return out, err
	})
	m.RegisterAction("Command", queueAll)
	m.RegisterActionArgs("Command", func(_ *fsm.Machine[*mind], cfg fsm.ActionConfig, _ fsm.StateResolver) (any, error) {
		text, err := textPayload(cfg.Payload)
		if text = input.CommandLine(text); err == nil && text == "" {
			err = errors.New("command must not be empty")
		}
		return input.AppendCommand(nil, text), err
	})
	m.RegisterAction("TypeGlyph", func(m *mind, _ any) {
		char, ok := m.glyphUnder()
		insert := m.ctx.IsInsertMode()
		if !ok || !insert && m.room < 2 || m.room < 1 {
			return
		}
		if !insert {
			m.push(input.Intent{Type: input.IntentModeSwitch, ModeTarget: input.ModeTargetInsert, Count: 1})
		}
		m.push(input.Intent{Type: input.IntentTextChar, Char: char, Count: 1})
	})
	m.RegisterAction("PointAt", func(m *mind, args any) { m.pointAt(args.(pointArgs)) })
	m.RegisterActionArgs("PointAt", func(_ *fsm.Machine[*mind], cfg fsm.ActionConfig, _ fsm.StateResolver) (any, error) {
		var p struct {
			Target string `toml:"target"`
			Key    string `toml:"key"`
			Fire   bool   `toml:"fire"`
			Away   bool   `toml:"away"`
		}
		if err := decodePayload(cfg.Payload, &p, "target", "key", "fire", "away"); err != nil {
			return nil, err
		}
		if p.Fire && p.Away {
			return nil, errors.New("fire and away do not combine")
		}
		var refused []target
		if p.Away {
			refused = []target{targetButton, targetRandom}
		}
		t, err := parseTarget(p.Target, refused...)
		if err != nil {
			return nil, err
		}
		key, err := buttonKey(t, p.Key)
		return pointArgs{target: t, key: key, fire: p.Fire, away: p.Away}, err
	})

	guard := func(name string, args []string, build func(map[string]any) (fsm.GuardFunc[*mind], error)) {
		m.RegisterGuardFactory(name, func(_ *fsm.Machine[*mind], given map[string]any) (fsm.GuardFunc[*mind], error) {
			if key := unknownKey(given, args...); key != "" {
				return nil, fmt.Errorf("unknown guard arg %q", key)
			}
			return build(given)
		})
	}
	guard("HasTarget", []string{"target", "key", "within"}, func(args map[string]any) (fsm.GuardFunc[*mind], error) {
		t, key, err := guardTarget(args)
		within := std.ParseIntArg(args, "within")
		return func(m *mind, _ *fsm.RegionState, _ any) bool {
			s := m.sight(t, key)
			return s.found && (within <= 0 || s.dist2 >= 0 && s.dist2 <= int(within*within))
		}, err
	})
	guard("OnTarget", []string{"target", "key"}, func(args map[string]any) (fsm.GuardFunc[*mind], error) {
		t, key, err := guardTarget(args)
		return func(m *mind, _ *fsm.RegionState, _ any) bool {
			s := m.sight(t, key)
			return s.found && s.dist2 == 0
		}, err
	})
	guard("InMode", []string{"mode"}, func(args map[string]any) (fsm.GuardFunc[*mind], error) {
		name, _ := args["mode"].(string)
		mode := slices.Index(core.ModeNames[:], name)
		if mode < 0 {
			return nil, fmt.Errorf("InMode: unknown mode %q", name)
		}
		return func(m *mind, _ *fsm.RegionState, _ any) bool {
			return m.ctx.GetMode() == core.GameMode(mode)
		}, nil
	})
	guard("Chance", []string{"percent"}, func(args map[string]any) (fsm.GuardFunc[*mind], error) {
		percent := int(std.ParseIntArg(args, "percent"))
		if percent < 1 || percent > 100 {
			return nil, fmt.Errorf("Chance: percent must be 1..100, got %d", percent)
		}
		return func(m *mind, _ *fsm.RegionState, _ any) bool {
			return m.rng.Intn(100) < percent
		}, nil
	})
	// Idle lets a graph wait for what it queued instead of overflowing the queue
	guard("Idle", nil, func(map[string]any) (fsm.GuardFunc[*mind], error) {
		return func(m *mind, _ *fsm.RegionState, _ any) bool { return m.queue.n == 0 }, nil
	})
	guard("Own", []string{"field"}, func(args map[string]any) (fsm.GuardFunc[*mind], error) {
		field, _ := args["field"].(string)
		if field == "" {
			return nil, errors.New("Own: field must name a payload field")
		}
		return func(m *mind, _ *fsm.RegionState, payload any) bool { return m.owns(payload, field) }, nil
	})
}

// guardTarget reads the target a guard measures from the cursor, which a random
// cell is not, and the key of a button
func guardTarget(args map[string]any) (target, rune, error) {
	t, err := parseTarget(args["target"], targetRandom)
	if err != nil {
		return 0, 0, err
	}
	key, err := buttonKey(t, args["key"])
	return t, key, err
}

// owns reports whether an event payload's field names the bot's own cursor. Until a
// joiner's slot binds, Player.Entity names another's cursor, which it does not drive.
func (m *mind) owns(payload any, field string) bool {
	own := m.ctx.World.Resources.Player.Entity
	v, ok := std.ExtractIntField(payload, field)
	return ok && own != 0 && v == int64(own) && m.ctx.World.SimulatesLocally(own)
}

// intentChoice is a compiled Intent payload: one keymap action or a list drawn
// from, with a fixed count or a [min, max] range drawn from, and the hold of a
// press. A payload with no choice draws nothing, so a fixed sequence leaves the
// bot's stream alone.
type intentChoice struct {
	intents  []input.Intent
	min, max int // the count range; max zero keeps each intent's own count
	hold     int
}

func (c intentChoice) draw(rng *vmath.FastRand) input.Intent {
	intent := c.intents[0]
	if len(c.intents) > 1 {
		intent = c.intents[rng.Intn(len(c.intents))]
	}
	if c.max > 0 {
		intent.Count = c.min + rng.Intn(c.max-c.min+1)
	}
	return intent
}

// compileIntent reads an Intent payload: name is one action or a list of them,
// count one number or a [min, max] range, char the rune a char-wait action, press
// or release targets, and hold how many ticks a press keeps its key down, or true
// until a release.
func compileIntent(payload any) (intentChoice, error) {
	table, _ := payload.(map[string]any)
	if key := unknownKey(table, "name", "count", "char", "hold"); key != "" {
		return intentChoice{}, fmt.Errorf("unknown payload field %q", key)
	}
	var names []string
	switch v := table["name"].(type) {
	case string:
		names = []string{v}
	case []any:
		for _, n := range v {
			name, ok := n.(string)
			if !ok {
				return intentChoice{}, fmt.Errorf("name list holds %v, not an action name", n)
			}
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return intentChoice{}, fmt.Errorf("name must be an action or a list of them")
	}
	var c intentChoice
	switch v := table["count"].(type) {
	case nil:
	case []any:
		var lo, hi int
		ok := len(v) == 2
		if ok {
			var okLo, okHi bool
			lo, okLo = intValue(v[0])
			hi, okHi = intValue(v[1])
			ok = okLo && okHi && lo >= 1 && hi >= lo
		}
		if !ok {
			return intentChoice{}, fmt.Errorf("count range must be [min, max] with 1 <= min <= max, got %v", v)
		}
		c.min, c.max = lo, hi
	default:
		n, ok := intValue(v)
		if !ok || n < 0 {
			return intentChoice{}, fmt.Errorf("count must be a non-negative number or a [min, max] range, got %v", v)
		}
		c.min = n
	}
	switch v := table["hold"].(type) {
	case nil:
	case bool:
		if !v {
			return intentChoice{}, errors.New("hold must be a number of ticks or true, got false")
		}
		c.hold = holdOpen
	default:
		n, ok := intValue(v)
		if !ok || n < 1 {
			return intentChoice{}, fmt.Errorf("hold must be a number of ticks or true, got %v", v)
		}
		c.hold = n
	}
	char, _ := table["char"].(string)
	for _, name := range names {
		if c.hold != 0 && name != input.PressAction {
			return intentChoice{}, fmt.Errorf("hold applies only to %s", input.PressAction)
		}
		intent, err := intentFor(name, c.min, char)
		if err != nil {
			return intentChoice{}, err
		}
		c.intents = append(c.intents, intent)
	}
	return c, nil
}

// releaseAction lets go of a key the bot holds. Only a bot names it: a script's
// presses tap.
const releaseAction = "release"

// intentFor is input.IntentFor with the release of a key the bot holds
func intentFor(name string, count int, char string) (input.Intent, error) {
	if name != releaseAction {
		return input.IntentFor(name, count, char)
	}
	key, size := utf8.DecodeRuneInString(char)
	if size == 0 || size != len(char) || !input.Capturable(key) {
		return input.Intent{}, fmt.Errorf("intent %q requires one printable ASCII char", name)
	}
	return input.Intent{Type: input.IntentKeyRelease, Char: key, Count: 1}, nil
}

// intValue reads a whole number as the TOML parser produces one
func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int64:
		return int(n), true
	case int:
		return n, true
	case float64:
		return int(n), n == float64(int(n))
	}
	return 0, false
}

// decodePayload decodes an action payload, refusing a key the action does not read
func decodePayload(payload any, into any, allowed ...string) error {
	table, _ := payload.(map[string]any)
	if key := unknownKey(table, allowed...); key != "" {
		return fmt.Errorf("unknown payload field %q", key)
	}
	if table == nil {
		return nil
	}
	return toml.Decode(table, into)
}

// textPayload reads the one non-empty text field Text and Command take
func textPayload(payload any) (string, error) {
	var p struct {
		Text string `toml:"text"`
	}
	if err := decodePayload(payload, &p, "text"); err != nil {
		return "", err
	}
	if p.Text == "" {
		return "", fmt.Errorf("text must not be empty")
	}
	return p.Text, nil
}
