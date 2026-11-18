package bot

import (
	"math"
	"slices"
	"strconv"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
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
	kindBullet
	kindMissile
	kindCount

	// targetCount is how many kinds a graph names as a target: those before kindLanding
	targetCount = kindLanding
)

// thing is one of a kind as its scan finds it: the cell it stands on, its entity,
// a body's its header, and told, the field its kind's row has the scan tell
type thing struct {
	x, y   int
	entity core.Entity
	told   float64
}

// visitor is told each thing a kind's scan finds
type visitor interface{ visit(thing) }

// kinds is indexed by kind: the name a graph uses and the scan that visits each
// one the world holds, and for a kind a network reads, how many of the nearest
// it may read and what one knows beyond where it is: tells, which its scan reads
// off the component it walks, none where zero, and the rest, read of the entity.
// A button is a rectangle a scenario lays out and random a cell: neither is scanned.
var kinds = [kindCount]struct {
	name   string
	scan   func(m *mind, to visitor)
	ranks  int
	covers bool // a network sights it by the nearest cell its members cover
	tells  field
	knows  []field
	doc    string
}{
	kindGlyph: {name: "glyph", doc: "typeable text: a glyph neither gold nor part of a composite",
		scan: func(m *mind, to visitor) {
			c := m.ctx.World.Components
			c.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
				if g.Type != component.GlyphGold && !c.Member.HasEntity(e) && m.ctx.World.Positions.IsTypeable(e) {
					m.at(e, 0, to)
				}
				return true
			})
		}, ranks: rankMax},
	kindGold: {name: "gold", doc: "a gold sequence member",
		scan: func(m *mind, to visitor) {
			m.ctx.World.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
				if g.Type == component.GlyphGold {
					m.at(e, 0, to)
				}
				return true
			})
		}, ranks: rankMax},
	kindNugget: {name: "nugget", doc: "a nugget",
		scan: func(m *mind, to visitor) {
			for _, e := range m.ctx.World.Components.Nugget.Entities() {
				m.at(e, 0, to)
			}
		}, ranks: rankMax},
	kindSpecies: {name: "species", doc: "a combat entity other than a cursor or a tower",
		scan: scanSpecies, ranks: rankMax, tells: fieldEnraged, knows: []field{fieldEnraged, fieldHits, fieldSize, fieldVX, fieldVY}},
	kindKraken: {name: "kraken", doc: "a Kraken, by its body, which may stand past a view that shows a leg; its nearness is that of the nearest cell it covers, legs and all",
		scan: func(m *mind, to visitor) {
			m.ctx.World.Components.Kraken.Each(func(e core.Entity, k *component.KrakenComponent) bool {
				m.at(e, k.AttackT, to)
				return true
			})
		}, ranks: 1, covers: true, tells: fieldReach, knows: []field{fieldReach, fieldHits, fieldHealth, fieldVX, fieldVY}},
	kindCursor: {name: "cursor", doc: "another participant's cursor",
		scan: func(m *mind, to visitor) {
			own := m.ctx.World.Resources.Player.Entity
			for _, e := range m.ctx.World.Components.Cursor.Entities() {
				if e != own {
					m.at(e, 0, to)
				}
			}
		}, ranks: rankMax},
	kindThreat: {name: "threat", doc: "a species, a hostile bullet or a hostile missile, all weighted alike",
		scan: func(m *mind, to visitor) {
			scanSpecies(m, to)
			scanBullets(m, to)
			scanMissiles(m, to)
		}},
	kindButton: {name: "button", doc: "a scenario's button, or with key the one on that key, at its cell nearest the cursor"},
	kindRandom: {name: "random", doc: "a cell the view shows; PointAt only"},
	kindLanding: {name: "landing", doc: "a cell something is about to land on: a raider's landing marker, where its charge or teleport ends, or the cell a Kraken's charge ends on while it spins up and charges",
		scan: func(m *mind, to visitor) {
			c := m.ctx.World.Components
			c.Behavior.Each(func(e core.Entity, b *component.BehaviorComponent) bool {
				if b.Landing {
					to.visit(thing{b.TargetX, b.TargetY, e, b.Machine.Progress()})
				}
				return true
			})
			c.Kraken.Each(func(e core.Entity, k *component.KrakenComponent) bool {
				switch k.State {
				case component.KrakenSpin:
					spun := 1 - k.StateRemaining.Seconds()/parameter.KrakenSpinDuration.Seconds()
					to.visit(thing{int(k.TargetX), int(k.TargetY), e, min(max(spun, 0), 1)})
				case component.KrakenMove:
					to.visit(thing{int(k.TargetX), int(k.TargetY), e, 1})
				}
				return true
			})
		}, ranks: rankMax, tells: fieldSoon, knows: []field{fieldSoon}},
	kindBullet: {name: "bullet", doc: "a hostile bullet",
		scan: scanBullets, ranks: rankMax, knows: []field{fieldVX, fieldVY}},
	kindMissile: {name: "missile", doc: "a hostile missile",
		scan: scanMissiles, ranks: rankMax, knows: []field{fieldVX, fieldVY}},
}

// scanSpecies visits every combat entity but a cursor or a tower
func scanSpecies(m *mind, to visitor) {
	m.ctx.World.Components.Combat.Each(func(e core.Entity, c *component.CombatComponent) bool {
		if c.CombatEntityType != component.CombatEntityCursor && c.CombatEntityType != component.CombatEntityTower {
			m.at(e, flag(c.IsEnraged), to)
		}
		return true
	})
}

func scanBullets(m *mind, to visitor) {
	m.ctx.World.Components.Bullet.Each(func(e core.Entity, b *component.BulletComponent) bool {
		if b.Hostile {
			m.at(e, 0, to)
		}
		return true
	})
}

func scanMissiles(m *mind, to visitor) {
	m.ctx.World.Components.Missile.Each(func(e core.Entity, missile *component.MissileComponent) bool {
		if missile.Hostile {
			m.at(e, 0, to)
		}
		return true
	})
}

// at tells to of the thing e is at the cell it stands on, with what its scan
// tells of it; an entity with no place is none
func (m *mind) at(e core.Entity, told float64, to visitor) {
	if pos, ok := m.ctx.World.Positions.GetPosition(e); ok {
		to.visit(thing{pos.X, pos.Y, e, told})
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
	fieldHits
	fieldHealth
	fieldSize
	fieldVX
	fieldVY
	fieldCount

	// fieldsWhere is how many fields say where a thing is
	fieldsWhere = fieldEnraged
)

// What a count or a rate of a thing reads as a half: its hit points, the cells
// its body covers and the cells a second it moves
const (
	hitsHalf = 32
	sizeHalf = 16
	rateHalf = 20
)

// fields is indexed by field: the name an input ends in, its scale with the half
// of a count's or a rate's, what it reads, and of, how one a scan does not tell
// is read of a thing's entity, as an input asks
var fields = [fieldCount]struct {
	name  string
	scale scale
	half  int
	doc   string
	of    func(m *mind, e core.Entity) float64
}{
	fieldX:       {name: "x", scale: scaleAcross, doc: "its offset across"},
	fieldY:       {name: "y", scale: scaleDown, doc: "its offset down"},
	fieldNearX:   {name: "near_x", scale: scaleNear, doc: "its nearness across"},
	fieldNearY:   {name: "near_y", scale: scaleNear, doc: "its nearness down"},
	fieldEnraged: {name: "enraged", scale: scaleFlag, doc: "1 while it is enraged"},
	fieldSoon:    {name: "soon", scale: scaleShare, doc: "how soon it lands, from 0 as its warning starts to 1 as the body leaves for it"},
	fieldReach:   {name: "reach", scale: scaleShare, doc: "how far out its striking legs are"},
	fieldHits: {name: "hp", scale: scaleHalf, half: hitsHalf, doc: "the hit points it has left",
		of: func(m *mind, e core.Entity) float64 {
			if c, ok := m.ctx.World.Components.Combat.GetPtr(e); ok {
				return count(c.HitPoints, hitsHalf)
			}
			return 0
		}},
	fieldHealth: {name: "health", scale: scaleShare, doc: "the hit points it has left, of those it rose with",
		of: func(m *mind, e core.Entity) float64 {
			c, ok := m.ctx.World.Components.Combat.GetPtr(e)
			if k, rose := m.ctx.World.Components.Kraken.GetPtr(e); ok && rose {
				return share(float64(c.HitPoints), float64(k.MaxHitPoints))
			}
			return 0
		}},
	fieldSize: {name: "size", scale: scaleHalf, half: sizeHalf, doc: "the cells its body covers",
		of: func(m *mind, e core.Entity) float64 {
			if header, ok := m.ctx.World.Components.Header.GetPtr(e); ok {
				return count(len(header.MemberEntries), sizeHalf)
			}
			return count(1, sizeHalf)
		}},
	fieldVX: {name: "vx", scale: scaleRate, half: rateHalf, doc: "the cells a second it moves across",
		of: func(m *mind, e core.Entity) float64 {
			if k, ok := m.ctx.World.Components.Kinetic.GetPtr(e); ok {
				return rate(k.VelX, rateHalf)
			}
			return 0
		}},
	fieldVY: {name: "vy", scale: scaleRate, half: rateHalf, doc: "the cells a second it moves down",
		of: func(m *mind, e core.Entity) float64 {
			if k, ok := m.ctx.World.Components.Kinetic.GetPtr(e); ok {
				return rate(k.VelY, rateHalf)
			}
			return 0
		}},
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
	scaleRate
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
	scaleRate:   {"rate", -1, "a rate v read as v/(|v|+half): 0 at rest and toward 1 either way, a half at the rate its row names"},
}

// share is part of full as a share reads it, nothing of no full
func share(part, full float64) float64 {
	if full <= 0 {
		return 0
	}
	return min(max(part/full, 0), 1)
}

// flag is on as a flag reads it, with no branch for a scan to mispredict
func flag(on bool) float64 {
	var bit int
	if on {
		bit = 1
	}
	return float64(bit)
}

// count is c as a count reads it
func count(c, half int) float64 {
	if c <= 0 {
		return 0
	}
	return float64(c) / float64(c+half)
}

// rate is v as a rate reads it
func rate(v, half float64) float64 { return v / (math.Abs(v) + half) }

// rankMax is how many of a kind a network may read at most, the nearest ones
const rankMax = 8

// ordinals names each rank in what an input reads
var ordinals = [rankMax]string{"nearest", "second nearest", "third nearest", "fourth nearest",
	"fifth nearest", "sixth nearest", "seventh nearest", "eighth nearest"}

// sighted is where one thing is as a network reads it, indexed by field
type sighted [fieldsWhere]float64

// sight is the thing at dx, dy cells from the cursor in a view width cells wide
// and height tall; one on the cursor's own cell reads as beside it on the right
func sight(dx, dy int, width, height float64) (s sighted) {
	s[fieldX], s[fieldY] = float64(dx)/width, float64(dy)/height
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
	m *mind
	want
	n     int
	spots [rankMax]spot
}

// spot is one thing a network keeps of a kind: how far it is, where, what its
// scan told of it, and its entity, of which what the scan does not tell is read
type spot struct {
	dist   int
	at     sighted
	told   float64
	entity core.Entity
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
	s := sight(t.x-v.from.X, t.y-v.from.Y, v.width, v.height)
	if kinds[k.kind].covers {
		cell := sight(x-v.from.X, y-v.from.Y, v.width, v.height)
		s[fieldNearX], s[fieldNearY] = cell[fieldNearX], cell[fieldNearY]
	}
	kept := &k.spots[i]
	kept.at, kept.told, kept.entity = s, t.told, t.entity
}

// place makes room among the nearest for a thing at dist2 d and returns its rank,
// -1 when ranks nearer ones are held
func (k *nearest) place(d int) int {
	i := k.n
	if i < k.ranks {
		k.n++
	} else if i--; d >= k.spots[i].dist {
		return -1
	}
	for ; i > 0 && d < k.spots[i-1].dist; i-- {
		k.spots[i] = k.spots[i-1]
	}
	k.spots[i].dist = d
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

// blocks are the ways a network reads how far the cursor may go before a cell
// blocks it: a wall, the rock or the map's edge
var blocks = [...]struct {
	name   string
	dx, dy int
	doc    string
}{
	{"block_up", 0, -1, "the first cell above the cursor that blocks it"},
	{"block_down", 0, 1, "the first cell below the cursor that blocks it"},
	{"block_left", -1, 0, "the first cell left of the cursor that blocks it"},
	{"block_right", 1, 0, "the first cell right of the cursor that blocks it"},
}

// block is the offset of the first cell that blocks the cursor along dx, dy: the
// cell past the view's edge where none it shows does
func (m *mind) block(dx, dy int) float64 {
	v := &m.view
	x, y := v.from.X+dx, v.from.Y+dy
	for v.shown(x, y) && !m.ctx.World.Positions.IsBlocked(vmath.Point{X: x, Y: y}, component.WallBlockCursor) {
		x, y = x+dx, y+dy
	}
	if dx != 0 {
		return float64(x-v.from.X) / v.width
	}
	return float64(y-v.from.Y) / v.height
}

// energyHalf and heatHalf are the energy and the heat their readings take as a half
const (
	energyHalf = 1000
	heatHalf   = 10
)

// own is one reading of the bot's own cursor, a count's half named where its
// scale is one
type own struct {
	name  string
	scale scale
	half  int
	doc   string
	read  reading
}

// self is what a network reads of the bot's own cursor: its place in the view,
// what it holds, and of each weapon kind the charges it has of those it may
var self = func() []own {
	rows := []own{
		{"column", scalePlace, 0, "the cursor's place across the view, -1 at its left edge and 1 at its right",
			func(m *mind, _ *network) float64 {
				v := &m.view
				return 2*float64(v.from.X-v.minX)/max(v.width-1, 1) - 1
			}},
		{"row", scalePlace, 0, "the cursor's place down the view, -1 at its top edge and 1 at its bottom",
			func(m *mind, _ *network) float64 {
				v := &m.view
				return 2*float64(v.from.Y-v.minY)/max(v.height-1, 1) - 1
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
		{"overheat", scaleShare, 0, "its overheat, of what lights its ember",
			func(m *mind, _ *network) float64 {
				h, _ := m.ctx.World.Components.Heat.GetComponent(m.ctx.World.Resources.Player.Entity)
				return share(float64(h.Overheat), parameter.HeatMaxOverheat)
			}},
		{"ember", scaleFlag, 0, "1 while its ember burns",
			func(m *mind, _ *network) float64 {
				h, _ := m.ctx.World.Components.Heat.GetComponent(m.ctx.World.Resources.Player.Entity)
				return flag(h.EmberActive)
			}},
		{"shield", scaleFlag, 0, "1 while its shield is up",
			func(m *mind, _ *network) float64 {
				s, _ := m.ctx.World.Components.Shield.GetComponent(m.ctx.World.Resources.Player.Entity)
				return flag(s.Active)
			}},
		{"boost", scaleShare, 0, "its boost, by the time it has left of what it was lit for",
			func(m *mind, _ *network) float64 {
				b, _ := m.ctx.World.Components.Boost.GetComponent(m.ctx.World.Resources.Player.Entity)
				if !b.Active {
					return 0
				}
				return share(b.Remaining.Seconds(), b.TotalDuration.Seconds())
			}},
	}
	for t, spec := range component.WeaponSpecs {
		rows = append(rows, own{"weapon_" + spec.Name, scaleShare, 0, "its " + spec.Name + ", by the charges it holds of those it may",
			func(m *mind, _ *network) float64 {
				w, ok := m.ctx.World.Components.Weapon.GetPtr(m.ctx.World.Resources.Player.Entity)
				if !ok {
					return 0
				}
				return share(float64(w.Charges[t]), float64(w.ChargeCap(component.WeaponType(t))))
			}})
	}
	return rows
}()

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

// sensors is every name a network's inputs take: the rock's edges by place, the
// blocks about the cursor, each kind's nearest by rank and field, the second and
// later carrying their rank in the name, and the bot's own
var sensors = func() (rows []sensor) {
	// halved names the half a count's or a rate's line reads at
	halved := func(doc string, half int) string {
		if half > 0 {
			doc += ", a half at " + strconv.Itoa(half)
		}
		return doc
	}
	for p, place := range places {
		for e, edge := range edges {
			rows = append(rows, sensor{edge.name + place.suffix, scaleDown, edge.doc + ", at " + place.doc,
				func(*network) reading { return func(m *mind, _ *network) float64 { return m.rock(p)[e] } }})
		}
	}
	for _, b := range blocks {
		scale := scaleDown
		if b.dx != 0 {
			scale = scaleAcross
		}
		rows = append(rows, sensor{b.name, scale, b.doc,
			func(*network) reading { return func(m *mind, _ *network) float64 { return m.block(b.dx, b.dy) } }})
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
					"the " + ordinals[rank] + " " + kinds[k].name + ": " + halved(fields[f].doc, fields[f].half),
					func(n *network) reading {
						slot, of := n.asks(k, rank+1), fields[f].of
						switch {
						case f < fieldsWhere:
							return func(_ *mind, n *network) float64 { return n.near[slot].spots[rank].at[f] }
						case f == kinds[k].tells:
							return func(_ *mind, n *network) float64 { return n.near[slot].spots[rank].told }
						}
						return func(m *mind, n *network) float64 {
							if near := &n.near[slot]; rank < near.n {
								return of(m, near.spots[rank].entity)
							}
							return 0
						}
					}})
			}
		}
	}
	for _, own := range self {
		rows = append(rows, sensor{own.name, own.scale, halved(own.doc, own.half), func(*network) reading { return own.read }})
	}
	return rows
}()

// outputs is what a network asks for, in the order Steer acts on it: a key ahead
// of the move, as a hand presses one on its way. units names the tables a graph
// writes an output's weights in, a unit each, which a network's weights hold in
// the order of at, and act reports whether the step has room for more.
var outputs = [...]struct {
	name  string
	at    int
	units []string
	doc   string
	act   func(m *mind, out []float64) bool
}{
	{"special", 1, []string{"special"}, "asks for the special attack while it is positive, ahead of the move and as often as auto-fire would", (*mind).askSpecial},
	{"move", 0, []string{"x", "y"}, "points at a cell: x and y are its offset from the cursor in view widths and heights, kept to the cells the view shows", (*mind).point},
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
		l.Outputs = append(l.Outputs, Output{o.name, o.units, o.doc})
	}
	for _, s := range scales {
		l.Scales = append(l.Scales, Scale{s.name, s.doc})
	}
	return l
}
