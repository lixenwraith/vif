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
)

const (
	// weightBound is the most a network weight holds either side of zero, so a
	// gene's range is twice it
	weightBound = 4.0

	// hiddenMax bounds a network's hidden units
	hiddenMax = 32

	// layoutVersion is in every layout digest: a change to what a sensor reads or to
	// the pass through the network takes the next, and populations start over
	layoutVersion = 1

	// energyHalf is the energy the energy sensor reads as a half
	energyHalf = 1000

	// biasName names an output's bias among its hand-written weights
	biasName = "bias"
)

// networkTable is a graph's [network] table: the sensors its network reads, in the
// order its weights take them, its hidden units, and the weights written by hand,
// each output's from the inputs it names and its bias. Every other weight is zero.
type networkTable struct {
	Inputs []string           `toml:"inputs"`
	Hidden int                `toml:"hidden"`
	X      map[string]float64 `toml:"x"`
	Y      map[string]float64 `toml:"y"`
}

// network is a graph's policy: tanh hidden units and a direct term from the inputs
// to its two outputs, the offset Steer points at. Its genes are each hidden unit's
// input weights and bias, then each output's direct weights, hidden weights and bias.
type network struct {
	inputs []sensor
	hidden int
	genes  []float64
	digest string
	in     []float64 // scratch: the inputs read, the hidden units' outputs after them
}

// parseNetwork reads a [network] table, nil for a graph with none
func parseNetwork(raw any) (*network, error) {
	if raw == nil {
		return nil, nil
	}
	var t networkTable
	if err := dialect.DecodeInto(raw, &t); err != nil {
		return nil, err
	}
	if len(t.Inputs) == 0 || t.Hidden < 0 || t.Hidden > hiddenMax {
		return nil, fmt.Errorf("needs inputs and 0 to %d hidden units, got %d and %d", hiddenMax, len(t.Inputs), t.Hidden)
	}
	n := &network{hidden: t.Hidden}
	for _, name := range t.Inputs {
		s := slices.IndexFunc(sensors[:], func(row sensorRow) bool { return row.name == name })
		if s < 0 || slices.Contains(n.inputs, sensor(s)) {
			return nil, fmt.Errorf("inputs: %q is no sensor, or is named twice", name)
		}
		n.inputs = append(n.inputs, sensor(s))
	}
	n.genes = make([]float64, n.geneCount())
	n.in = make([]float64, len(n.inputs)+n.hidden)
	for o, weights := range [...]map[string]float64{t.X, t.Y} {
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
func (n *network) geneCount() int { return n.output(2) }

// unit is where hidden unit h's genes start: its input weights, then its bias
func (n *network) unit(h int) int { return h * (len(n.inputs) + 1) }

// output is where output o's genes start: its direct weights, its hidden units'
// and its bias
func (n *network) output(o int) int { return n.unit(n.hidden) + o*(len(n.inputs)+n.hidden+1) }

// blocks are the genes that work only together: each hidden unit's own with the
// weights the outputs give it, and each output's direct weights with its bias
func (n *network) blocks() [][]int {
	width := len(n.inputs)
	blocks := make([][]int, 0, n.hidden+2)
	for h := range n.hidden {
		block := []int{n.output(0) + width + h, n.output(1) + width + h}
		for i := range width + 1 {
			block = append(block, n.unit(h)+i)
		}
		blocks = append(blocks, block)
	}
	for o := range 2 {
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
	if n == nil {
		return nil
	}
	c := *n
	c.genes, c.in = slices.Clone(n.genes), make([]float64, len(n.in))
	return &c
}

// run reads the scene through the network and returns its two outputs
func (n *network) run(s *scene) (x, y float64) {
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
	var out [2]float64
	for o := range out {
		row := n.genes[n.output(o):]
		sum := row[width+n.hidden]
		for i, v := range n.in {
			sum += row[i] * v
		}
		out[o] = sum
	}
	return out[0], out[1]
}

// scene is what the sensors read, taken once an update from the cells the view
// shows: an offset is from the cursor's cell, in view widths across and view
// heights down, and nothing sighted reads zero.
type scene struct {
	// rock is the ceiling's and the floor's edge at the cursor's column, an eighth
	// of the view ahead and a quarter ahead
	rock    [3][2]float64
	species [2]struct{ x, y, enraged float64 }
	landing [2]float64
	loot    [2]float64
	column  float64 // -1 at the view's left edge, 1 at its right
	energy  float64 // 0 with none, a half at energyHalf, toward 1
}

// sensor is one input a network may read
type sensor uint8

const (
	sensorCeiling sensor = iota
	sensorFloor
	sensorCeilingNear
	sensorFloorNear
	sensorCeilingFar
	sensorFloorFar
	sensorSpeciesX
	sensorSpeciesY
	sensorSpeciesEnraged
	sensorSpecies2X
	sensorSpecies2Y
	sensorSpecies2Enraged
	sensorLandingX
	sensorLandingY
	sensorLootX
	sensorLootY
	sensorColumn
	sensorEnergy
	sensorCount
)

type sensorRow struct {
	name string
	read func(*scene) float64
}

// sensors is indexed by sensor: the name a [network] lists it by and its reading
var sensors = [sensorCount]sensorRow{
	sensorCeiling:         {"ceiling", func(s *scene) float64 { return s.rock[0][0] }},
	sensorFloor:           {"floor", func(s *scene) float64 { return s.rock[0][1] }},
	sensorCeilingNear:     {"ceiling_near", func(s *scene) float64 { return s.rock[1][0] }},
	sensorFloorNear:       {"floor_near", func(s *scene) float64 { return s.rock[1][1] }},
	sensorCeilingFar:      {"ceiling_far", func(s *scene) float64 { return s.rock[2][0] }},
	sensorFloorFar:        {"floor_far", func(s *scene) float64 { return s.rock[2][1] }},
	sensorSpeciesX:        {"species_x", func(s *scene) float64 { return s.species[0].x }},
	sensorSpeciesY:        {"species_y", func(s *scene) float64 { return s.species[0].y }},
	sensorSpeciesEnraged:  {"species_enraged", func(s *scene) float64 { return s.species[0].enraged }},
	sensorSpecies2X:       {"species2_x", func(s *scene) float64 { return s.species[1].x }},
	sensorSpecies2Y:       {"species2_y", func(s *scene) float64 { return s.species[1].y }},
	sensorSpecies2Enraged: {"species2_enraged", func(s *scene) float64 { return s.species[1].enraged }},
	sensorLandingX:        {"landing_x", func(s *scene) float64 { return s.landing[0] }},
	sensorLandingY:        {"landing_y", func(s *scene) float64 { return s.landing[1] }},
	sensorLootX:           {"loot_x", func(s *scene) float64 { return s.loot[0] }},
	sensorLootY:           {"loot_y", func(s *scene) float64 { return s.loot[1] }},
	sensorColumn:          {"column", func(s *scene) float64 { return s.column }},
	sensorEnergy:          {"energy", func(s *scene) float64 { return s.energy }},
}

// look reads the scene around the cursor at from, of the cells minX, minY to maxX,
// maxY the view shows. The nearest of a kind is by dist2, ties to store order.
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

	near := [2]int{-1, -1}
	w.Components.Combat.Each(func(e core.Entity, c *component.CombatComponent) bool {
		pos, ok := w.Positions.GetPosition(e)
		if !ok || !shown(pos.X, pos.Y) || c.CombatEntityType == component.CombatEntityCursor || c.CombatEntityType == component.CombatEntityTower {
			return true
		}
		d := dist2(pos.X-from.X, pos.Y-from.Y)
		for i := range near {
			if near[i] < 0 || d < near[i] {
				copy(near[i+1:], near[i:])
				copy(s.species[i+1:], s.species[i:])
				near[i] = d
				s.species[i].x, s.species[i].y = float64(pos.X-from.X)/width, float64(pos.Y-from.Y)/height
				s.species[i].enraged = 0
				if c.IsEnraged {
					s.species[i].enraged = 1
				}
				break
			}
		}
		return true
	})

	best := -1
	w.Components.Behavior.Each(func(_ core.Entity, b *component.BehaviorComponent) bool {
		if d := dist2(b.TargetX-from.X, b.TargetY-from.Y); b.Landing && shown(b.TargetX, b.TargetY) && (best < 0 || d < best) {
			best, s.landing = d, [2]float64{float64(b.TargetX-from.X) / width, float64(b.TargetY-from.Y) / height}
		}
		return true
	})

	own := w.Resources.Player.Entity
	best = -1
	w.Components.Loot.Each(func(e core.Entity, l *component.LootComponent) bool {
		pos, ok := w.Positions.GetPosition(e)
		if d := dist2(pos.X-from.X, pos.Y-from.Y); ok && l.Owner == own && shown(pos.X, pos.Y) && (best < 0 || d < best) {
			best, s.loot = d, [2]float64{float64(pos.X-from.X) / width, float64(pos.Y-from.Y) / height}
		}
		return true
	})

	s.column = 2*float64(from.X-minX)/max(width-1, 1) - 1
	if e, ok := w.Components.Energy.GetComponent(own); ok {
		level := math.Abs(float64(e.Current))
		s.energy = level / (level + energyHalf)
	}
	return s
}

// steer points at the cell the network's outputs name: an offset from the cursor's
// in view widths and heights, kept to the cells the view shows, as a mouse reaches
// no further, and landed as PointAt lands
func (m *mind) steer() {
	from, cursor := m.ctx.World.LocalCursor()
	minX, minY, maxX, maxY, ok := m.ctx.World.Resources.Config.VisibleMapRect()
	if !cursor || !ok || m.room < 1 {
		return
	}
	s := m.look(from, minX, minY, maxX, maxY)
	outX, outY := m.net.run(&s)
	goalX := from.X + int(math.Round(min(max(outX, -1), 1)*float64(maxX-minX+1)))
	goalY := from.Y + int(math.Round(min(max(outY, -1), 1)*float64(maxY-minY+1)))
	x, y, ok := m.land(min(max(goalX, minX), maxX), min(max(goalY, minY), maxY), goalX, goalY)
	if !ok || x == from.X && y == from.Y {
		return
	}
	m.push(input.Intent{Type: input.IntentMouseMove, X: x, Y: y, MapCell: true})
}

// compileSteer refuses Steer in a graph that declares no network to steer by
func compileSteer(net *network) func(*struct{}) (struct{}, error) {
	return func(*struct{}) (struct{}, error) {
		if net == nil {
			return struct{}{}, errors.New("the graph declares no [network] to steer by")
		}
		return struct{}{}, nil
	}
}
