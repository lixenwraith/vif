package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
)

const (
	// weightBound is the most a network weight holds either side of zero, so a
	// gene's range is twice it
	weightBound = 4.0

	// hiddenMax bounds a network's hidden units
	hiddenMax = 32

	// layoutVersion is in every layout digest: a change to what a sensor reads or to
	// the pass through the network takes the next, and populations start over
	layoutVersion = 2

	// energyHalf and heatHalf are the energy and the heat their sensors read as a half
	energyHalf = 1000
	heatHalf   = 10

	// biasName names an output's bias among its hand-written weights
	biasName = "bias"

	// specialOver is what a network's special output passes to ask for the special
	// attack, so one with no weights never asks
	specialOver = 1.0
)

// The outputs of a network: the offset Steer points at, across and down, and
// special, which asks for the special attack while it is over specialOver
const (
	outputX = iota
	outputY
	outputSpecial
	outputCount
)

// networkTable is one of a graph's [network.<name>] tables: the sensors the
// network reads, in the order its weights take them, its hidden units, and the
// weights written by hand, each output's from the inputs it names and its bias.
// Every other weight is zero.
type networkTable struct {
	Inputs  []string           `toml:"inputs"`
	Hidden  int                `toml:"hidden"`
	X       map[string]float64 `toml:"x"`
	Y       map[string]float64 `toml:"y"`
	Special map[string]float64 `toml:"special"`
}

// network is one policy of a graph: tanh hidden units and a direct term from the
// inputs to its outputs. Its genes are each hidden unit's input weights and bias,
// then each output's direct weights, hidden weights and bias.
type network struct {
	name   string
	inputs []sensor
	hidden int
	genes  []float64
	digest string
	in     []float64 // scratch: the inputs read, the hidden units' outputs after them
}

// parseNetworks reads a graph's [network] table, a network a name, in name order;
// a name is part of a population's file name, so it is lowercase letters, digits
// and underscores
func parseNetworks(raw any) ([]*network, error) {
	tables, ok := raw.(map[string]any)
	if raw != nil && !ok {
		return nil, errors.New("holds a [network.<name>] table a network")
	}
	var nets []*network
	for _, name := range slices.Sorted(maps.Keys(tables)) {
		if name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
			return nil, fmt.Errorf("name %q is not lowercase letters, digits and underscores", name)
		}
		if _, ok := tables[name].(map[string]any); !ok {
			return nil, fmt.Errorf("%q is no table: a network is named, [network.<name>]", name)
		}
		n, err := parseNetwork(name, tables[name])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		nets = append(nets, n)
	}
	return nets, nil
}

// parseNetwork reads one [network.<name>] table
func parseNetwork(name string, raw any) (*network, error) {
	var t networkTable
	if err := dialect.DecodeInto(raw, &t); err != nil {
		return nil, err
	}
	if len(t.Inputs) == 0 || t.Hidden < 0 || t.Hidden > hiddenMax {
		return nil, fmt.Errorf("needs inputs and 0 to %d hidden units, got %d and %d", hiddenMax, len(t.Inputs), t.Hidden)
	}
	n := &network{name: name, hidden: t.Hidden}
	for _, name := range t.Inputs {
		s := slices.IndexFunc(sensors[:], func(row sensorRow) bool { return row.name == name })
		if s < 0 || slices.Contains(n.inputs, sensor(s)) {
			return nil, fmt.Errorf("inputs: %q is no sensor, or is named twice", name)
		}
		n.inputs = append(n.inputs, sensor(s))
	}
	n.genes = make([]float64, n.geneCount())
	n.in = make([]float64, len(n.inputs)+n.hidden)
	for o, weights := range [outputCount]map[string]float64{outputX: t.X, outputY: t.Y, outputSpecial: t.Special} {
		for _, name := range slices.Sorted(maps.Keys(weights)) {
			i := slices.Index(t.Inputs, name)
			if name == biasName {
				i = len(n.inputs) + n.hidden
			}
			if w := weights[name]; i < 0 || math.Abs(w) > weightBound || w != w {
				return nil, fmt.Errorf("weight %q = %g: not an input or %s, or past %g", name, w, biasName, weightBound)
			}
			n.genes[n.output(o)+i] = weights[name]
		}
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%d %s %d", layoutVersion, strings.Join(t.Inputs, ","), t.Hidden))
	n.digest = hex.EncodeToString(sum[:6])
	return n, nil
}

// geneCount is how many weights the layout holds
func (n *network) geneCount() int { return n.output(outputCount) }

// unit is where hidden unit h's genes start: its input weights, then its bias
func (n *network) unit(h int) int { return h * (len(n.inputs) + 1) }

// output is where output o's genes start: its direct weights, its hidden units'
// and its bias
func (n *network) output(o int) int { return n.unit(n.hidden) + o*(len(n.inputs)+n.hidden+1) }

// blocks are the genes that work only together: each hidden unit's own with the
// weights the outputs give it, and each output's direct weights with its bias
func (n *network) blocks() [][]int {
	width := len(n.inputs)
	blocks := make([][]int, 0, n.hidden+outputCount)
	for h := range n.hidden {
		var block []int
		for o := range outputCount {
			block = append(block, n.output(o)+width+h)
		}
		for i := range width + 1 {
			block = append(block, n.unit(h)+i)
		}
		blocks = append(blocks, block)
	}
	for o := range outputCount {
		block := []int{n.output(o) + width + n.hidden}
		for i := range width {
			block = append(block, n.output(o)+i)
		}
		blocks = append(blocks, block)
	}
	return blocks
}

// playing is a network of its own for one driver, whose weights a learner replaces
func (n *network) playing() *network {
	c := *n
	c.genes, c.in = slices.Clone(n.genes), make([]float64, len(n.in))
	return &c
}

// run reads the scene through the network and returns its outputs
func (n *network) run(s *scene) (out [outputCount]float64) {
	width := len(n.inputs)
	in, hid := n.in[:width], n.in[width:]
	for i, kind := range n.inputs {
		in[i] = sensors[kind].read(s)
	}
	for h := range hid {
		row := n.genes[n.unit(h):]
		sum := row[width]
		for i, v := range in {
			sum += row[i] * v
		}
		hid[h] = math.Tanh(sum)
	}
	for o := range out {
		row := n.genes[n.output(o):]
		sum := row[width+n.hidden]
		for i, v := range n.in {
			sum += row[i] * v
		}
		out[o] = sum
	}
	return out
}

// scene is what the sensors read, taken once an update from the cells the view
// shows: an offset is from the cursor's cell, in view widths across and view
// heights down, and nothing sighted reads zero.
type scene struct {
	// rock is the ceiling's and the floor's edge at the cursor's column, an eighth
	// of the view ahead and a quarter ahead
	rock    [3][2]float64
	species [slots]sighted // the nearest species, nearest first
	landing [slots]sighted // the nearest cells a raider is about to land on, or a Kraken to charge to
	kraken  sighted        // the Kraken's body, its nearness that of the nearest cell it covers
	reach   float64        // how far out its striking legs are, 0 to 1
	column  float64        // -1 at the view's left edge, 1 at its right
	energy  float64        // 0 with none, a half at energyHalf, toward 1
	heat    float64        // 0 with none, a half at heatHalf, toward 1
}

// slots is how many of a kind a scene holds, the nearest ones: a network that reads
// them all drops none of a cave's raiders, which fly three at most
const slots = 3

// sighted is one thing a scene holds: its offset, whether it is enraged, and its
// nearness, a vector toward it that is 1 long beside the cursor and falls to
// nothing a quarter of the view's width away, rows counted twice
type sighted struct {
	x, y, enraged, nearX, nearY float64
}

// sight is the thing at dx, dy cells from the cursor in a view width cells wide
// and height tall; one on the cursor's own cell reads as beside it on the right
func sight(dx, dy int, width, height float64, enraged bool) sighted {
	s := sighted{x: float64(dx) / width, y: float64(dy) / height}
	if enraged {
		s.enraged = 1
	}
	reach, r := width/4, math.Sqrt(float64(dist2(dx, dy)))
	switch {
	case r == 0:
		s.nearX = 1
	case r < reach:
		s.nearX, s.nearY = (1-r/reach)*float64(dx)/r, (1-r/reach)*float64(2*dy)/r
	}
	return s
}

// nearest collects the slots nearest things of a kind, nearest first, ties to the
// first seen
type nearest struct {
	at   [slots]sighted
	dist [slots]int
	n    int
}

// add offers the thing at dist2 d
func (k *nearest) add(d int, s sighted) {
	i := k.n
	if i < slots {
		k.n++
	} else if i--; d >= k.dist[i] {
		return
	}
	for ; i > 0 && d < k.dist[i-1]; i-- {
		k.at[i], k.dist[i] = k.at[i-1], k.dist[i-1]
	}
	k.at[i], k.dist[i] = s, d
}

// sensor is one input a network may read, an index into sensors
type sensor uint8

type sensorRow struct {
	name string
	read func(*scene) float64
}

// sensors is each sensor's name, as a [network] lists it, and its reading. The
// second and third nearest of a kind carry their place in the name.
var sensors = func() (rows []sensorRow) {
	add := func(name string, read func(*scene) float64) { rows = append(rows, sensorRow{name, read}) }
	for i, ahead := range [...]string{"", "_near", "_far"} {
		add("ceiling"+ahead, func(s *scene) float64 { return s.rock[i][0] })
		add("floor"+ahead, func(s *scene) float64 { return s.rock[i][1] })
	}
	for i, place := range [slots]string{"", "2", "3"} {
		add("species"+place+"_x", func(s *scene) float64 { return s.species[i].x })
		add("species"+place+"_y", func(s *scene) float64 { return s.species[i].y })
		add("species"+place+"_enraged", func(s *scene) float64 { return s.species[i].enraged })
		add("species"+place+"_near_x", func(s *scene) float64 { return s.species[i].nearX })
		add("species"+place+"_near_y", func(s *scene) float64 { return s.species[i].nearY })
		add("landing"+place+"_x", func(s *scene) float64 { return s.landing[i].x })
		add("landing"+place+"_y", func(s *scene) float64 { return s.landing[i].y })
		add("landing"+place+"_near_x", func(s *scene) float64 { return s.landing[i].nearX })
		add("landing"+place+"_near_y", func(s *scene) float64 { return s.landing[i].nearY })
	}
	add("kraken_x", func(s *scene) float64 { return s.kraken.x })
	add("kraken_y", func(s *scene) float64 { return s.kraken.y })
	add("kraken_near_x", func(s *scene) float64 { return s.kraken.nearX })
	add("kraken_near_y", func(s *scene) float64 { return s.kraken.nearY })
	add("kraken_reach", func(s *scene) float64 { return s.reach })
	add("column", func(s *scene) float64 { return s.column })
	add("energy", func(s *scene) float64 { return s.energy })
	add("heat", func(s *scene) float64 { return s.heat })
	return rows
}()

// look reads the scene around the cursor at from, of the cells minX, minY to maxX,
// maxY the view shows. Nearness is by dist2, ties to store order.
func (m *mind) look(from component.PositionComponent, minX, minY, maxX, maxY int) scene {
	w := m.ctx.World
	width, height := float64(maxX-minX+1), float64(maxY-minY+1)
	shown := func(x, y int) bool { return x >= minX && x <= maxX && y >= minY && y <= maxY }
	var s scene

	rock := w.Positions.Rock()
	for i, ahead := range [...]int{0, (maxX - minX + 1) / 8, (maxX - minX + 1) / 4} {
		top, bottom := minY-1, maxY+1
		if rock != nil {
			if col, ok := rock.Column(min(from.X+ahead, maxX)); ok {
				top, bottom = max(top, col.Ceil-1), min(bottom, col.Floor)
			}
		}
		s.rock[i] = [2]float64{float64(top-from.Y) / height, float64(bottom-from.Y) / height}
	}

	var species, landings nearest
	w.Components.Combat.Each(func(e core.Entity, c *component.CombatComponent) bool {
		pos, ok := w.Positions.GetPosition(e)
		if ok && shown(pos.X, pos.Y) && c.CombatEntityType != component.CombatEntityCursor && c.CombatEntityType != component.CombatEntityTower {
			species.add(dist2(pos.X-from.X, pos.Y-from.Y), sight(pos.X-from.X, pos.Y-from.Y, width, height, c.IsEnraged))
		}
		return true
	})
	w.Components.Behavior.Each(func(_ core.Entity, b *component.BehaviorComponent) bool {
		if b.Landing && shown(b.TargetX, b.TargetY) {
			landings.add(dist2(b.TargetX-from.X, b.TargetY-from.Y), sight(b.TargetX-from.X, b.TargetY-from.Y, width, height, false))
		}
		return true
	})

	covered := -1
	w.Components.Kraken.Each(func(e core.Entity, k *component.KrakenComponent) bool {
		if tx, ty := int(k.TargetX), int(k.TargetY); (k.State == component.KrakenSpin || k.State == component.KrakenMove) && shown(tx, ty) {
			landings.add(dist2(tx-from.X, ty-from.Y), sight(tx-from.X, ty-from.Y, width, height, false))
		}
		header, _ := w.Components.Header.GetPtr(e)
		body, ok := w.Positions.GetPosition(e)
		if !ok || header == nil {
			return true
		}
		for i := range header.MemberEntries {
			pos, ok := w.Positions.GetPosition(header.MemberEntries[i].Entity)
			if d := dist2(pos.X-from.X, pos.Y-from.Y); ok && shown(pos.X, pos.Y) && (covered < 0 || d < covered) {
				cell := sight(pos.X-from.X, pos.Y-from.Y, width, height, false)
				covered, s.kraken, s.reach = d, sight(body.X-from.X, body.Y-from.Y, width, height, false), k.AttackT
				s.kraken.nearX, s.kraken.nearY = cell.nearX, cell.nearY
			}
		}
		return true
	})
	s.species, s.landing = species.at, landings.at

	own := w.Resources.Player.Entity
	s.column = 2*float64(from.X-minX)/max(width-1, 1) - 1
	if e, ok := w.Components.Energy.GetComponent(own); ok {
		level := math.Abs(float64(e.Current))
		s.energy = level / (level + energyHalf)
	}
	if h, ok := w.Components.Heat.GetComponent(own); ok {
		s.heat = float64(h.Current) / float64(h.Current+heatHalf)
	}
	return s
}

// specialIntent is how a network asks for the special attack, as a key does
var specialIntent, _ = input.IntentFor("fire_special", 1, "")

// steer plays network i for an update. It points at the cell the outputs name: an
// offset from the cursor's in view widths and heights, kept to the cells the view
// shows, as a mouse reaches no further, and landed as PointAt lands. Before that it
// asks for the special attack where special says to, as often as auto-fire would.
func (m *mind) steer(i int) {
	from, cursor := m.ctx.World.LocalCursor()
	minX, minY, maxX, maxY, ok := m.ctx.World.Resources.Config.VisibleMapRect()
	if !cursor || !ok || m.room < 1 {
		return
	}
	m.steered[i] = true
	s := m.look(from, minX, minY, maxX, maxY)
	out := m.nets[i].run(&s)
	if out[outputSpecial] > specialOver && m.tick >= m.special {
		m.special = m.tick + uint64(max(parameter.AutoFireInterval/m.ctx.World.TickInterval(), 1))
		if m.push(specialIntent); m.room < 1 {
			return
		}
	}
	goalX := from.X + int(math.Round(min(max(out[outputX], -1), 1)*float64(maxX-minX+1)))
	goalY := from.Y + int(math.Round(min(max(out[outputY], -1), 1)*float64(maxY-minY+1)))
	x, y, ok := m.land(min(max(goalX, minX), maxX), min(max(goalY, minY), maxY), goalX, goalY)
	if !ok || x == from.X && y == from.Y {
		return
	}
	m.push(input.Intent{Type: input.IntentMouseMove, X: x, Y: y, MapCell: true})
}

// compileSteer resolves the network a Steer names among the graph's, which a graph
// declaring one may leave out
func compileSteer(nets []*network) func(*steerPayload) (int, error) {
	return func(p *steerPayload) (int, error) {
		i := slices.IndexFunc(nets, func(n *network) bool { return n.name == p.Network })
		if p.Network == "" && len(nets) == 1 {
			i = 0
		}
		if i < 0 {
			return 0, fmt.Errorf("network %q is none of the graph's [network.<name>] tables", p.Network)
		}
		return i, nil
	}
}
