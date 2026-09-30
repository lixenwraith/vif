package bot

import (
	"fmt"
	"slices"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/std"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
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
	targetRandom
	targetCount
)

// targets is indexed by target: the name a graph uses and the scan that visits
// every entity of the class. Random has no scan; it is a cell, not an entity.
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
	targetSpecies: {"species", func(m *mind, visit func(core.Entity)) {
		m.ctx.World.Components.Combat.Each(func(e core.Entity, c *component.CombatComponent) bool {
			if c.CombatEntityType != component.CombatEntityCursor && c.CombatEntityType != component.CombatEntityTower {
				visit(e)
			}
			return true
		})
	}},
	targetCursor: {"cursor", func(m *mind, visit func(core.Entity)) {
		own := m.ctx.World.Resources.Player.Entity
		for _, e := range m.ctx.World.Components.Cursor.Entities() {
			if e != own {
				visit(e)
			}
		}
	}},
	targetRandom: {name: "random"},
}

// parseTarget resolves a graph's target name; a random cell is refused where an
// entity is needed.
func parseTarget(raw any, entity bool) (target, error) {
	name, _ := raw.(string)
	for t := range targetCount {
		if targets[t].name == name && (!entity || targets[t].scan != nil) {
			return t, nil
		}
	}
	return 0, fmt.Errorf("unknown target %q", name)
}

// sighting is the nearest entity of one class to the bot's own cursor
type sighting struct {
	x, y  int
	dist2 int // squared cell distance, rows counted twice as the terminal draws them
	found bool
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
			dx, dy := pos.X-from.X, 2*(pos.Y-from.Y)
			if d := dx*dx + dy*dy; !s.found || d < s.dist2 {
				s = sighting{x: pos.X, y: pos.Y, dist2: d, found: true}
			}
		})
	}
	m.seen[t] = s
	return s
}

// glyphUnder is the rune typing would validate at the cursor: the first glyph in
// its cell, as TypingSystem selects it.
func (m *mind) glyphUnder() (rune, bool) {
	pos, ok := m.ctx.World.LocalCursor()
	if !ok {
		return 0, false
	}
	var buf [parameter.MaxEntitiesPerCell]core.Entity
	n := m.ctx.World.Positions.GetAllEntitiesAtInto(pos.X, pos.Y, buf[:])
	for _, e := range buf[:n] {
		if g, ok := m.ctx.World.Components.Glyph.GetComponent(e); ok {
			return g.Rune, true
		}
	}
	return 0, false
}

// push queues intents in order, spending the step's room
func (m *mind) push(intents ...input.Intent) {
	for _, intent := range intents {
		m.queue.push(intent)
	}
	m.room -= len(intents)
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
	if p.target == targetRandom {
		x, y = minX+m.rng.Intn(maxX-minX+1), minY+m.rng.Intn(maxY-minY+1)
	} else {
		s := m.nearest(p.target)
		if !s.found {
			return
		}
		x, y = min(max(s.x, minX), maxX), min(max(s.y, minY), maxY)
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

// pointArgs is a compiled PointAt payload
type pointArgs struct {
	target target
	fire   bool
}

// register installs std's generic vocabulary, its status guards reading the bot's
// own registry, whose bare keys mirror its own slot, its config guards reading the
// bot's own view, and the bot's actions and guards.
func register(m *fsm.Machine[*mind]) {
	std.Register(m, std.Host[*mind]{
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
	m.RegisterAction("Intent", func(m *mind, args any) { m.push(args.(intentChoice).draw(m.rng)) })
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
			Fire   bool   `toml:"fire"`
		}
		if err := decodePayload(cfg.Payload, &p, "target", "fire"); err != nil {
			return nil, err
		}
		t, err := parseTarget(p.Target, false)
		return pointArgs{target: t, fire: p.Fire}, err
	})

	m.RegisterGuardFactory("HasTarget", func(_ *fsm.Machine[*mind], args map[string]any) (fsm.GuardFunc[*mind], error) {
		t, err := parseTarget(args["target"], true)
		within := std.ParseIntArg(args, "within")
		return func(m *mind, _ *fsm.RegionState, _ any) bool {
			s := m.nearest(t)
			return s.found && (within <= 0 || s.dist2 <= int(within*within))
		}, err
	})
	m.RegisterGuardFactory("OnTarget", func(_ *fsm.Machine[*mind], args map[string]any) (fsm.GuardFunc[*mind], error) {
		t, err := parseTarget(args["target"], true)
		return func(m *mind, _ *fsm.RegionState, _ any) bool {
			s := m.nearest(t)
			return s.found && s.dist2 == 0
		}, err
	})
	m.RegisterGuardFactory("InMode", func(_ *fsm.Machine[*mind], args map[string]any) (fsm.GuardFunc[*mind], error) {
		name, _ := args["mode"].(string)
		mode := slices.Index(core.ModeNames[:], name)
		if mode < 0 {
			return nil, fmt.Errorf("InMode: unknown mode %q", name)
		}
		return func(m *mind, _ *fsm.RegionState, _ any) bool {
			return m.ctx.GetMode() == core.GameMode(mode)
		}, nil
	})
	m.RegisterGuardFactory("Chance", func(_ *fsm.Machine[*mind], args map[string]any) (fsm.GuardFunc[*mind], error) {
		percent := int(std.ParseIntArg(args, "percent"))
		if percent < 1 || percent > 100 {
			return nil, fmt.Errorf("Chance: percent must be 1..100, got %d", percent)
		}
		return func(m *mind, _ *fsm.RegionState, _ any) bool {
			return m.rng.Intn(100) < percent
		}, nil
	})
}

// intentChoice is a compiled Intent payload: one keymap action or a list drawn
// from, with a fixed count or a [min, max] range drawn from. A payload with no
// choice draws nothing, so a fixed sequence leaves the bot's stream alone.
type intentChoice struct {
	intents  []input.Intent
	min, max int // the count range; max zero keeps each intent's own count
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
// count one number or a [min, max] range, char the rune a char-wait action targets.
func compileIntent(payload any) (intentChoice, error) {
	table, _ := payload.(map[string]any)
	if key := unknownKey(table, "name", "count", "char"); key != "" {
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
	char, _ := table["char"].(string)
	for _, name := range names {
		intent, err := input.IntentFor(name, c.min, char)
		if err != nil {
			return intentChoice{}, err
		}
		c.intents = append(c.intents, intent)
	}
	return c, nil
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
