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

	"github.com/lixenwraith/vif/internal/fsm/dialect"
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

	// biasName names an output's bias among its hand-written weights
	biasName = "bias"
)

// networkTable is one of a graph's [network.<name>] tables: the sensors the
// network reads, in the order its weights take them, its hidden units, and any
// weights written by hand, each output's from the inputs it names and its bias.
// Every other weight is zero, and a network that learns needs none written.
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
	inputs []reading
	wants  []want // the kinds its inputs read, each to the furthest rank one asks for
	hidden int
	genes  []float64
	digest string
	in     []float64 // scratch: the inputs read, the hidden units' outputs after them
	near   []nearest // scratch: the nearest things of each kind it wants
}

// want is a kind a network reads and how many of its nearest
type want struct {
	kind  kind
	ranks int
}

// asks has the network read the ranks nearest of kind k and returns where among
// near it keeps them
func (n *network) asks(k kind, ranks int) int {
	slot := slices.IndexFunc(n.wants, func(w want) bool { return w.kind == k })
	if slot < 0 {
		slot, n.wants = len(n.wants), append(n.wants, want{kind: k})
	}
	n.wants[slot].ranks = max(n.wants[slot].ranks, ranks)
	return slot
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
	for i, name := range t.Inputs {
		s := slices.IndexFunc(sensors, func(row sensor) bool { return row.name == name })
		if s < 0 || slices.Contains(t.Inputs[:i], name) {
			return nil, fmt.Errorf("inputs: %q is no sensor, or is named twice", name)
		}
		n.inputs = append(n.inputs, sensors[s].bind(n))
	}
	n.genes = make([]float64, n.geneCount())
	n.in, n.near = make([]float64, len(n.inputs)+n.hidden), make([]nearest, len(n.wants))
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
	c.genes, c.in, c.near = slices.Clone(n.genes), make([]float64, len(n.in)), make([]nearest, len(n.near))
	return &c
}

// read takes the inputs from the world as m's view frames it: of each kind they
// name the nearest things, found once, then each input in the weights' order
func (n *network) read(m *mind) {
	for i, w := range n.wants {
		near := &n.near[i]
		near.m, near.want, near.n = m, w, 0
		clear(near.spots[:w.ranks])
		kinds[w.kind].scan(m, near)
	}
	for i, read := range n.inputs {
		n.in[i] = read(m, n)
	}
}

// pass runs the inputs read through the network and returns its outputs
func (n *network) pass() (out [outputCount]float64) {
	width := len(n.inputs)
	in, hid := n.in[:width], n.in[width:]
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

// steer plays network i for an update: it reads the network's inputs through the
// view around the cursor, then acts on each output in turn for as long as the
// step has room
func (m *mind) steer(i int) {
	m.steered[i] = true
	if !m.frame() || m.room < 1 {
		return
	}
	n := m.nets[i]
	n.read(m)
	out := n.pass()
	for _, o := range outputs {
		if !o.act(m, out[o.at:o.at+o.units]) {
			return
		}
	}
}

// frame takes the view an update's readings go through, the cursor's cell and
// the cells shown; false with no cursor on the map or nothing shown
func (m *mind) frame() bool {
	from, cursor := m.ctx.World.LocalCursor()
	minX, minY, maxX, maxY, ok := m.ctx.World.Resources.Config.VisibleMapRect()
	m.view = view{from: from, minX: minX, minY: minY, maxX: maxX, maxY: maxY,
		width: float64(maxX - minX + 1), height: float64(maxY - minY + 1)}
	return cursor && ok
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
