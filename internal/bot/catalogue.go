package bot

import (
	"math"
	"slices"
	"strconv"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
)

// kind is a class of thing the world holds: what a graph's guards and PointAt take
// as a target, and what a network reads the nearest of
type kind uint8

const (
	kindGlyph kind = iota
	kindGold
	kindNugget
	kindSpecies
	kindKraken
	kindCursor
	kindThreat
	kindButton
	kindRandom
	kindLanding
	kindCount

	// targetCount is how many kinds a graph names as a target: those before kindLanding
	targetCount = kindLanding
)

// thing is one of a kind as its scan finds it: the cell it stands on, a body's
// header, and what its kind knows of it
type thing struct {
	x, y    int
	entity  core.Entity
	enraged bool
	soon    float64
	reach   float64
}

// visitor is told each thing a kind's scan finds
type visitor interface{ visit(thing) }

// kinds is indexed by kind: the name a graph uses and the scan that visits each
// one the world holds, and for a kind a network reads, how many of the nearest
// it may read and what one knows beyond where it is. A button is a rectangle a
// scenario lays out and random a cell, so neither is scanned.
var kinds = [kindCount]struct {
	name   string
	scan   func(m *mind, to visitor)
	ranks  int
	covers bool // a network sights it by the nearest cell its members cover
	knows  []field
	doc    string
}{
	kindGlyph: {name: "glyph", doc: "typeable text: a glyph neither gold nor part of a composite",
		scan: func(m *mind, to visitor) {
			c := m.ctx.World.Components
			c.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
				if g.Type != component.GlyphGold && !c.Member.HasEntity(e) && m.ctx.World.Positions.IsTypeable(e) {
					m.at(e, thing{}, to)
				}
				return true
			})
		}},
	kindGold: {name: "gold", doc: "a gold sequence member",
		scan: func(m *mind, to visitor) {
			m.ctx.World.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
				if g.Type == component.GlyphGold {
					m.at(e, thing{}, to)
				}
				return true
			})
		}},
	kindNugget: {name: "nugget", doc: "a nugget",
		scan: func(m *mind, to visitor) {
			for _, e := range m.ctx.World.Components.Nugget.Entities() {
				m.at(e, thing{}, to)
			}
		}},
	kindSpecies: {name: "species", doc: "a combat entity other than a cursor or a tower",
		scan: scanSpecies, ranks: rankMax, knows: []field{fieldEnraged}},
	kindKraken: {name: "kraken", doc: "a Kraken, by its body, which may stand past a view that shows a leg; its nearness is that of the nearest cell it covers, legs and all",
		scan: func(m *mind, to visitor) {
			m.ctx.World.Components.Kraken.Each(func(e core.Entity, k *component.KrakenComponent) bool {
				m.at(e, thing{reach: k.AttackT}, to)
				return true
			})
		}, ranks: 1, covers: true, knows: []field{fieldReach}},
	kindCursor: {name: "cursor", doc: "another participant's cursor",
		scan: func(m *mind, to visitor) {
			own := m.ctx.World.Resources.Player.Entity
			for _, e := range m.ctx.World.Components.Cursor.Entities() {
				if e != own {
					m.at(e, thing{}, to)
				}
			}
		}},
	kindThreat: {name: "threat", doc: "a species, a hostile bullet or a hostile missile, all weighted alike",
		scan: func(m *mind, to visitor) {
			scanSpecies(m, to)
			c := m.ctx.World.Components
			c.Bullet.Each(func(e core.Entity, b *component.BulletComponent) bool {
				if b.Hostile {
					m.at(e, thing{}, to)
				}
				return true
			})
			c.Missile.Each(func(e core.Entity, missile *component.MissileComponent) bool {
				if missile.Hostile {
					m.at(e, thing{}, to)
				}
				return true
			})
		}},
	kindButton: {name: "button", doc: "a scenario's button, or with key the one on that key, at its cell nearest the cursor"},
	kindRandom: {name: "random", doc: "a cell the view shows; PointAt only"},
	kindLanding: {name: "landing", doc: "a cell something is about to land on: a raider's landing marker, where its charge or teleport ends, or the cell a Kraken's charge ends on while it spins up and charges",
		scan: func(m *mind, to visitor) {
			c := m.ctx.World.Components
			c.Behavior.Each(func(_ core.Entity, b *component.BehaviorComponent) bool {
				if b.Landing {
					to.visit(thing{x: b.TargetX, y: b.TargetY, soon: b.Machine.Progress()})
				}
				return true
			})
			c.Kraken.Each(func(_ core.Entity, k *component.KrakenComponent) bool {
				switch k.State {
				case component.KrakenSpin:
					spun := 1 - k.StateRemaining.Seconds()/parameter.KrakenSpinDuration.Seconds()
					to.visit(thing{x: int(k.TargetX), y: int(k.TargetY), soon: min(max(spun, 0), 1)})
				case component.KrakenMove:
					to.visit(thing{x: int(k.TargetX), y: int(k.TargetY), soon: 1})
				}
				return true
			})
		}, ranks: rankMax, knows: []field{fieldSoon}},
}

// scanSpecies visits every combat entity but a cursor or a tower
func scanSpecies(m *mind, to visitor) {
	m.ctx.World.Components.Combat.Each(func(e core.Entity, c *component.CombatComponent) bool {
		if c.CombatEntityType != component.CombatEntityCursor && c.CombatEntityType != component.CombatEntityTower {
			m.at(e, thing{enraged: c.IsEnraged}, to)
		}
		return true
	})
}

// at tells to of t at the cell e stands on; an entity with no place is no thing
func (m *mind) at(e core.Entity, t thing, to visitor) {
	if pos, ok := m.ctx.World.Positions.GetPosition(e); ok {
		t.x, t.y, t.entity = pos.X, pos.Y, e
		to.visit(t)
	}
}

// field is what a network reads of one thing: where it is, which every kind
// knows, or what its own kind knows
type field uint8

const (
	fieldX field = iota
	fieldY
	fieldNearX
	fieldNearY
	fieldEnraged
	fieldSoon
	fieldReach
	fieldCount

	// fieldsWhere is how many fields say where a thing is
	fieldsWhere = fieldEnraged
)

// fields is indexed by field: the name an input ends in, its scale and what it reads
var fields = [fieldCount]struct {
	name  string
	scale scale
	doc   string
}{
	fieldX:       {"x", scaleAcross, "its offset across"},
	fieldY:       {"y", scaleDown, "its offset down"},
	fieldNearX:   {"near_x", scaleNear, "its nearness across"},
	fieldNearY:   {"near_y", scaleNear, "its nearness down"},
	fieldEnraged: {"enraged", scaleFlag, "1 while it is enraged"},
	fieldSoon:    {"soon", scaleShare, "how soon it lands, from 0 as its warning starts to 1 as the body leaves for it"},
	fieldReach:   {"reach", scaleShare, "how far out its striking legs are"},
}

// scale is how a reading is kept within 1 of zero, the one rule every input holds
// to; nothing sighted reads 0
type scale uint8

const (
	scaleAcross scale = iota
	scaleDown
	scaleNear
	scalePlace
	scaleFlag
	scaleShare
	scaleHalf
	scaleCount
)

// scales is indexed by scale: its name, the least it reads and what it means
var scales = [scaleCount]struct {
	name string
	low  float64
	doc  string
}{
	scaleAcross: {"across", -1, "an offset from the cursor's cell in view widths, within 1 of zero for a thing the view shows"},
	scaleDown:   {"down", -1, "an offset from the cursor's cell in view heights, within 1 of zero for a thing the view shows"},
	scaleNear:   {"near", -1, "one part of a nearness: a vector toward the thing, 1 long beside the cursor and falling to nothing a quarter of the view's width away, rows counted twice"},
	scalePlace:  {"place", -1, "a place in the view, -1 at one edge and 1 at the other"},
	scaleFlag:   {"flag", 0, "0 or 1"},
	scaleShare:  {"share", 0, "how much of its full it stands at"},
	scaleHalf:   {"half", 0, "a count c read as c/(c+half): 0 with none and toward 1, a half at the count its row names"},
}

// rankMax is how many of a kind a network may read, the nearest ones: a network
// that reads them all drops none of a cave's raiders, which fly three at most
const rankMax = 3

// ordinals names each rank in what an input reads
var ordinals = [rankMax]string{"nearest", "second nearest", "third nearest"}

// sighted is one thing as a network reads it, indexed by field
type sighted [fieldCount]float64

// sight is the thing at dx, dy cells from the cursor in a view width cells wide
// and height tall; one on the cursor's own cell reads as beside it on the right
func sight(dx, dy int, width, height float64, enraged bool) (s sighted) {
	s[fieldX], s[fieldY] = float64(dx)/width, float64(dy)/height
	if enraged {
		s[fieldEnraged] = 1
	}
	reach, r := width/4, math.Sqrt(float64(dist2(dx, dy)))
	switch {
	case r == 0:
		s[fieldNearX] = 1
	case r < reach:
		s[fieldNearX], s[fieldNearY] = (1-r/reach)*float64(dx)/r, (1-r/reach)*float64(2*dy)/r
	}
	return s
}

// view is what one Steer reads through: the cursor's cell, the cells the view
// shows and the rock's edges at each place a reading has asked for
type view struct {
	from                   component.PositionComponent
	minX, minY, maxX, maxY int
	width, height          float64
	rocks                  [len(places)][2]float64
	rockRead               [len(places)]bool
}

func (v *view) shown(x, y int) bool { return x >= v.minX && x <= v.maxX && y >= v.minY && y <= v.maxY }

// nearest keeps the nearest things of a kind the view shows, nearest first and
// ties to the first found: what a network's scan of that kind tells
type nearest struct {
	m     *mind
	kind  kind
	ranks int
	n     int
	dist  [rankMax]int
	at    [rankMax]sighted
}

func (k *nearest) visit(t thing) {
	v := &k.m.view
	x, y, ok := t.x, t.y, v.shown(t.x, t.y)
	if kinds[k.kind].covers {
		x, y, ok = k.m.covered(t.entity)
	}
	if !ok {
		return
	}
	i := k.place(dist2(x-v.from.X, y-v.from.Y))
	if i < 0 {
		return
	}
	s := sight(t.x-v.from.X, t.y-v.from.Y, v.width, v.height, t.enraged)
	if kinds[k.kind].covers {
		cell := sight(x-v.from.X, y-v.from.Y, v.width, v.height, false)
		s[fieldNearX], s[fieldNearY] = cell[fieldNearX], cell[fieldNearY]
	}
	s[fieldSoon], s[fieldReach] = t.soon, t.reach
	k.at[i] = s
}

// place makes room among the nearest for a thing at dist2 d and returns its rank,
// -1 when ranks nearer ones are held
func (k *nearest) place(d int) int {
	i := k.n
	if i < k.ranks {
		k.n++
	} else if i--; d >= k.dist[i] {
		return -1
	}
	for ; i > 0 && d < k.dist[i-1]; i-- {
		k.at[i], k.dist[i] = k.at[i-1], k.dist[i-1]
	}
	k.dist[i] = d
	return i
}

// covered is, of the cells e's members stand on that the view shows, the one
// nearest the cursor
func (m *mind) covered(e core.Entity) (x, y int, ok bool) {
	header, _ := m.ctx.World.Components.Header.GetPtr(e)
	if header == nil {
		return 0, 0, false
	}
	v, best := &m.view, -1
	for i := range header.MemberEntries {
		pos, has := m.ctx.World.Positions.GetPosition(header.MemberEntries[i].Entity)
		if d := dist2(pos.X-v.from.X, pos.Y-v.from.Y); has && v.shown(pos.X, pos.Y) && (best < 0 || d < best) {
			x, y, best = pos.X, pos.Y, d
		}
	}
	return x, y, best >= 0
}

// edges are the rock's two edges a network reads at a column, and places the
// columns: the cursor's own and those a share of the view's width ahead of it
var (
	edges = [...]struct{ name, doc string }{
		{"ceiling", "the rock's edge above"},
		{"floor", "the rock's edge below"},
	}
	places = [...]struct {
		suffix string
		share  int // the view's width over this is how far ahead, 0 for none
		doc    string
	}{
		{"", 0, "the cursor's column"},
		{"_near", 8, "the column an eighth of the view ahead"},
		{"_far", 4, "the column a quarter of the view ahead"},
	}
)

// rock is the rock's edges above and below the column at place, as offsets down
// from the cursor: the view's own rows where no rock stands
func (m *mind) rock(place int) [2]float64 {
	v := &m.view
	if v.rockRead[place] {
		return v.rocks[place]
	}
	ahead := 0
	if share := places[place].share; share > 0 {
		ahead = (v.maxX - v.minX + 1) / share
	}
	top, bottom := v.minY-1, v.maxY+1
	if rock := m.ctx.World.Positions.Rock(); rock != nil {
		if col, ok := rock.Column(min(v.from.X+ahead, v.maxX)); ok {
			top, bottom = max(top, col.Ceil-1), min(bottom, col.Floor)
		}
	}
	v.rocks[place], v.rockRead[place] = [2]float64{float64(top-v.from.Y) / v.height, float64(bottom-v.from.Y) / v.height}, true
	return v.rocks[place]
}

// energyHalf and heatHalf are the energy and the heat their readings take as a half
const (
	energyHalf = 1000
	heatHalf   = 10
)

// self is what a network reads of the bot's own cursor: its place in the view and
// what it holds, a count's half named where its scale is one
var self = [...]struct {
	name  string
	scale scale
	half  int
	doc   string
	read  reading
}{
	{"column", scalePlace, 0, "the cursor's place across the view, -1 at its left edge and 1 at its right",
		func(m *mind, _ *network) float64 {
			v := &m.view
			return 2*float64(v.from.X-v.minX)/max(v.width-1, 1) - 1
		}},
	{"energy", scaleHalf, energyHalf, "its energy, whichever its polarity",
		func(m *mind, _ *network) float64 {
			e, ok := m.ctx.World.Components.Energy.GetComponent(m.ctx.World.Resources.Player.Entity)
			if !ok {
				return 0
			}
			level := math.Abs(float64(e.Current))
			return level / (level + energyHalf)
		}},
	{"heat", scaleHalf, heatHalf, "its heat",
		func(m *mind, _ *network) float64 {
			h, ok := m.ctx.World.Components.Heat.GetComponent(m.ctx.World.Resources.Player.Entity)
			if !ok {
				return 0
			}
			return float64(h.Current) / float64(h.Current+heatHalf)
		}},
}

// reading is one input of a network: what it takes from the world through the
// view its mind steers by
type reading func(m *mind, n *network) float64

// sensor is one name a network's inputs take: the scale it reads in, what it
// reads, and bind, which has a network come to read it
type sensor struct {
	name  string
	scale scale
	doc   string
	bind  func(n *network) reading
}

// sensors is every name a network's inputs take: the rock's edges by place, each
// kind's nearest by rank and field, the second and third carrying their rank in
// the name, and the bot's own
var sensors = func() (rows []sensor) {
	for p, place := range places {
		for e, edge := range edges {
			rows = append(rows, sensor{edge.name + place.suffix, scaleDown, edge.doc + ", at " + place.doc,
				func(*network) reading { return func(m *mind, _ *network) float64 { return m.rock(p)[e] } }})
		}
	}
	for k := range kindCount {
		for rank := range kinds[k].ranks {
			prefix := kinds[k].name
			if rank > 0 {
				prefix += strconv.Itoa(rank + 1)
			}
			for f := range fieldCount {
				if f >= fieldsWhere && !slices.Contains(kinds[k].knows, f) {
					continue
				}
				rows = append(rows, sensor{prefix + "_" + fields[f].name, fields[f].scale,
					"the " + ordinals[rank] + " " + kinds[k].name + ": " + fields[f].doc,
					func(n *network) reading {
						slot := n.asks(k, rank+1)
						return func(_ *mind, n *network) float64 { return n.near[slot].at[rank][f] }
					}})
			}
		}
	}
	for _, own := range self {
		doc := own.doc
		if own.half > 0 {
			doc += ", a half at " + strconv.Itoa(own.half)
		}
		rows = append(rows, sensor{own.name, own.scale, doc, func(*network) reading { return own.read }})
	}
	return rows
}()

// The units of a network's outputs, in the order its weights hold them: the
// offset Steer points at, across and down, and special
const (
	outputX = iota
	outputY
	outputSpecial
	outputCount
)

// unitNames is indexed by unit: the table a graph writes an output's weights in
var unitNames = [outputCount]string{outputX: "x", outputY: "y", outputSpecial: "special"}

// outputs is what a network asks for, in the order Steer acts on it: a key ahead
// of the move, as a hand presses one on its way. at is where an output's units
// stand among a network's, and act reports whether the step has room for more.
var outputs = [...]struct {
	name      string
	at, units int
	doc       string
	act       func(m *mind, out []float64) bool
}{
	{"special", outputSpecial, 1, "asks for the special attack while it is positive, ahead of the move and as often as auto-fire would", (*mind).askSpecial},
	{"move", outputX, 2, "points at a cell: x and y are its offset from the cursor in view widths and heights, kept to the cells the view shows", (*mind).point},
}

// specialIntent is how a network asks for the special attack, as a key does
var specialIntent, _ = input.IntentFor("fire_special", 1, "")

func (m *mind) askSpecial(out []float64) bool {
	if out[0] > 0 && m.tick >= m.special {
		m.special = m.tick + uint64(max(parameter.AutoFireInterval/m.ctx.World.TickInterval(), 1))
		m.push(specialIntent)
	}
	return m.room >= 1
}

// point moves the pointer to the cell out names, landed as PointAt lands: a mouse
// reaches no further than the view shows
func (m *mind) point(out []float64) bool {
	v := &m.view
	goalX := v.from.X + int(math.Round(min(max(out[0], -1), 1)*v.width))
	goalY := v.from.Y + int(math.Round(min(max(out[1], -1), 1)*v.height))
	x, y, ok := m.land(min(max(goalX, v.minX), v.maxX), min(max(goalY, v.minY), v.maxY), goalX, goalY)
	if ok && (x != v.from.X || y != v.from.Y) {
		m.push(input.Intent{Type: input.IntentMouseMove, X: x, Y: y, MapCell: true})
	}
	return true
}

// Input is one name a network's inputs take and Output one thing it may ask for;
// Scale is one way a reading is kept within 1 of zero
type (
	Input struct {
		Name  string `json:"name"`
		Scale string `json:"scale"`
		Doc   string `json:"doc"`
	}
	Output struct {
		Name  string   `json:"name"`
		Units []string `json:"units"`
		Doc   string   `json:"doc"`
	}
	Scale struct {
		Name string `json:"name"`
		Doc  string `json:"doc"`
	}
	Listing struct {
		Inputs  []Input  `json:"inputs"`
		Outputs []Output `json:"outputs"`
		Scales  []Scale  `json:"scales"`
	}
)

// Catalogue lists what a network reads and may ask for, for -schema
func Catalogue() (l Listing) {
	for _, s := range sensors {
		l.Inputs = append(l.Inputs, Input{s.name, scales[s.scale].name, s.doc})
	}
	for _, o := range outputs {
		l.Outputs = append(l.Outputs, Output{o.name, unitNames[o.at : o.at+o.units], o.doc})
	}
	for _, s := range scales {
		l.Scales = append(l.Scales, Scale{s.name, s.doc})
	}
	return l
}
