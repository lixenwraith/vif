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

	// biasName names a unit's bias among its written weights, and hiddenName an
	// output's weights over the hidden units
	biasName   = "bias"
	hiddenName = "hidden"
)

// networkTable is one of a graph's [network.<name>] tables, its weights aside: the
// sensors it reads after the document's own, in the order its weights take them,
// its hidden units and a table of weights for each, the outputs it has, special
// and move when it lists none
type networkTable struct {
	Inputs  []string `toml:"inputs"`
	Hidden  int      `toml:"hidden"`
	Units   []any    `toml:"units"`
	Outputs []string `toml:"outputs"`
}

// network is one policy of a graph: tanh hidden units and a direct term from the
// inputs to its outputs' units. Its genes are each hidden unit's input weights and
// bias, then each output unit's direct weights, hidden weights and bias.
type network struct {
	name   string
	names  []string // its inputs' names, in the order its genes take them
	inputs []reading
	wants  []want   // the kinds its inputs read, each to the furthest rank one asks for
	units  []string // its outputs' units, in the order its genes hold them
	acts   []acting // its outputs, in the order Steer acts on them
	hidden int
	genes  []float64
	blank  bool   // every gene is zero: it asks for nothing, and Steer reads nothing for it
	asked  uint64 // the update, one past its tick, that out was last read and passed for
	digest string
	in     []float64 // scratch: the inputs read, the hidden units' outputs after them
	out    []float64 // scratch: its units' sums
	near   []nearest // scratch: the nearest things of each kind it wants
}

// want is a kind a network reads and how many of its nearest
type want struct {
	kind  kind
	ranks int
}

// acting is an output a network has: its name, where its units stand among the
// network's, how many they are and what acts on them
type acting struct {
	name      string
	at, units int
	act       func(m *mind, out []float64) bool
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

// parseNetworks reads a graph's [network] table, a network a name, in name order,
// each reading shared ahead of its own inputs; a name is part of a population's
// file name, so it is lowercase letters, digits and underscores
func parseNetworks(raw any, shared []string) ([]*network, error) {
	tables, ok := raw.(map[string]any)
	if raw != nil && !ok {
		return nil, errors.New("holds a [network.<name>] table a network")
	}
	var nets []*network
	for _, name := range slices.Sorted(maps.Keys(tables)) {
		if name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
			return nil, fmt.Errorf("name %q is not lowercase letters, digits and underscores", name)
		}
		table, ok := tables[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%q is no table: a network is named, [network.<name>]", name)
		}
		n, err := parseNetwork(name, table, shared)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		nets = append(nets, n)
	}
	return nets, nil
}

// parseNetwork reads one [network.<name>] table, whose keys named for an output's
// unit are that unit's weights
func parseNetwork(name string, raw map[string]any, shared []string) (*network, error) {
	table, weights := map[string]any{}, map[string]any{}
	for key, value := range raw {
		into := table
		for _, o := range outputs {
			if slices.Contains(o.units, key) {
				into = weights
			}
		}
		into[key] = value
	}
	var t networkTable
	if err := dialect.DecodeInto(table, &t); err != nil {
		return nil, err
	}
	names := slices.Concat(shared, t.Inputs)
	if len(names) == 0 || t.Hidden < 0 || t.Hidden > hiddenMax {
		return nil, fmt.Errorf("needs inputs and 0 to %d hidden units, got %d and %d", hiddenMax, len(names), t.Hidden)
	}
	n := &network{name: name, names: names, hidden: t.Hidden}
	for i, name := range names {
		s := slices.IndexFunc(sensors, func(row sensor) bool { return row.name == name })
		if s < 0 || slices.Contains(names[:i], name) {
			return nil, fmt.Errorf("inputs: %q is no sensor, or is named twice", name)
		}
		n.inputs = append(n.inputs, sensors[s].bind(n))
	}
	had, err := n.have(t.Outputs)
	if err != nil {
		return nil, err
	}
	n.genes = make([]float64, n.geneCount())
	n.in, n.out, n.near = make([]float64, len(n.inputs)+n.hidden), make([]float64, len(n.units)), make([]nearest, len(n.wants))

	for _, unit := range slices.Sorted(maps.Keys(weights)) {
		o := slices.Index(n.units, unit)
		if o < 0 {
			return nil, fmt.Errorf("%s: the network lists no output of that unit", unit)
		}
		if err := n.write(n.genes[n.output(o):n.output(o+1)], weights[unit], names, true); err != nil {
			return nil, fmt.Errorf("%s: %w", unit, err)
		}
	}
	if len(t.Units) > n.hidden {
		return nil, fmt.Errorf("units: %d tables of weights for %d hidden units", len(t.Units), n.hidden)
	}
	for h, unit := range t.Units {
		if err := n.write(n.genes[n.unit(h):n.unit(h+1)], unit, names, false); err != nil {
			return nil, fmt.Errorf("units[%d]: %w", h, err)
		}
	}

	// The outputs are named only where they are not those a network listing none
	// has: the digest of such a network is the one its populations are kept under
	layout := fmt.Appendf(nil, "%d %s %d", layoutVersion, strings.Join(names, ","), t.Hidden)
	if usual, _ := (&network{}).have(nil); !slices.Equal(had, usual) {
		layout = fmt.Appendf(layout, " %s", strings.Join(had, ","))
	}
	sum := sha256.Sum256(layout)
	n.digest = hex.EncodeToString(sum[:6])
	return n, nil
}

// have gives the network the outputs listed, or with none listed every one that
// is not asked, and returns their names: their units in the order of at, as its
// genes hold them, and what acts on each in the order Steer does
func (n *network) have(listed []string) (had []string, err error) {
	var at [len(outputs)]int
	has := func(o int) bool {
		return len(listed) == 0 && !outputs[o].asked || slices.Contains(listed, outputs[o].name)
	}
	for place := range outputs {
		for o, row := range outputs {
			if row.at == place && has(o) {
				at[o] = len(n.units)
				n.units = append(n.units, row.units...)
			}
		}
	}
	for o, row := range outputs {
		if has(o) {
			had = append(had, row.name)
			n.acts = append(n.acts, acting{row.name, at[o], len(row.units), row.act})
		}
	}
	for i, name := range listed {
		if !slices.Contains(had, name) || slices.Contains(listed[:i], name) {
			return nil, fmt.Errorf("outputs: %q is no output, or is named twice", name)
		}
	}
	return had, nil
}

// write sets the weights a table gives in one unit's row of genes: an input's by
// its name, the row's last by bias, and those an output gives the hidden units,
// in their order, by hidden
func (n *network) write(row []float64, raw any, names []string, output bool) error {
	table, ok := raw.(map[string]any)
	if !ok {
		return errors.New("is no table of weights")
	}
	for _, key := range slices.Sorted(maps.Keys(table)) {
		at, values := slices.Index(names, key), []any{table[key]}
		switch {
		case key == biasName:
			at = len(row) - 1
		case key == hiddenName && output:
			at = len(names)
			if values, ok = table[key].([]any); !ok || len(values) > n.hidden {
				return fmt.Errorf("%s: wants a weight a hidden unit, %d at most", hiddenName, n.hidden)
			}
		}
		if at < 0 {
			return fmt.Errorf("weight %q: not an input, %s or an output's %s", key, biasName, hiddenName)
		}
		for i, value := range values {
			w, ok := floatValue(value)
			if !ok || math.Abs(w) > weightBound || w != w {
				return fmt.Errorf("weight %q = %v: no number within %g of zero", key, value, weightBound)
			}
			row[at+i] = w
		}
	}
	return nil
}

// floatValue reads a number as the TOML parser produces one
func floatValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

// geneCount is how many weights the layout holds
func (n *network) geneCount() int { return n.output(len(n.units)) }

// unit is where hidden unit h's genes start: its input weights, then its bias
func (n *network) unit(h int) int { return h * (len(n.inputs) + 1) }

// output is where output unit o's genes start: its direct weights, its hidden
// units' and its bias
func (n *network) output(o int) int { return n.unit(n.hidden) + o*(len(n.inputs)+n.hidden+1) }

// blocks are the genes that work only together: each hidden unit's own with the
// weights the outputs give it, and each output unit's direct weights with its bias
func (n *network) blocks() [][]int {
	width := len(n.inputs)
	blocks := make([][]int, 0, n.hidden+len(n.units))
	for h := range n.hidden {
		var block []int
		for o := range n.units {
			block = append(block, n.output(o)+width+h)
		}
		for i := range width + 1 {
			block = append(block, n.unit(h)+i)
		}
		blocks = append(blocks, block)
	}
	for o := range n.units {
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
	c.in, c.out, c.near = make([]float64, len(n.in)), make([]float64, len(n.out)), make([]nearest, len(n.near))
	c.wear(slices.Clone(n.genes))
	return &c
}

// wear has the network play genes
func (n *network) wear(genes []float64) {
	n.genes, n.blank = genes, !slices.ContainsFunc(genes, func(v float64) bool { return v != 0 })
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

// pass runs the inputs read through the network and returns its units' sums
func (n *network) pass() []float64 {
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
	for o := range n.out {
		row := n.genes[n.output(o):]
		sum := row[width+n.hidden]
		for i, v := range n.in {
			sum += row[i] * v
		}
		n.out[o] = sum
	}
	return n.out
}

// ask reads the inputs and passes them once an update, however many of the
// graph's guards and Steers ask in it, and returns the units' sums
func (n *network) ask(m *mind) []float64 {
	if n.asked != m.tick+1 {
		n.asked = m.tick + 1
		n.read(m)
		n.pass()
	}
	return n.out
}

// steerArgs is a compiled Steer payload: the network it runs and which of that
// network's outputs it acts on, a bit an output
type steerArgs struct {
	net  int
	acts uint
}

// steer plays a's network for an update: it reads the network's inputs through
// the view around the cursor, then acts on each output a takes, in turn, for as
// long as the step has room. One with no weight written neither reads nor asks
func (m *mind) steer(a steerArgs) {
	m.steered[a.net] = true
	n := m.nets[a.net]
	if n.blank || !m.frame() || m.room < 1 {
		return
	}
	out := n.ask(m)
	for i, act := range n.acts {
		if a.acts&(1<<i) != 0 && !act.act(m, out[act.at:act.at+act.units]) {
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

// pick is the network of nets a Steer or an Asks names, the graph's one where
// it names none
func pick(nets []*network, name string) (int, error) {
	i := slices.IndexFunc(nets, func(n *network) bool { return n.name == name })
	if name == "" && len(nets) == 1 {
		i = 0
	}
	if i < 0 {
		return 0, fmt.Errorf("network %q is none of the graph's [network.<name>] tables", name)
	}
	return i, nil
}

// compileSteer resolves the network a Steer names among the graph's and the
// outputs the Steer names of it, every one where it names none
func compileSteer(nets []*network) func(*steerPayload) (steerArgs, error) {
	return func(p *steerPayload) (steerArgs, error) {
		i, err := pick(nets, p.Network)
		if err != nil {
			return steerArgs{}, err
		}
		a := steerArgs{net: i, acts: ^uint(0)}
		if len(p.Outputs) == 0 {
			return a, nil
		}
		a.acts = 0
		for j, output := range p.Outputs {
			at := slices.IndexFunc(nets[i].acts, func(a acting) bool { return a.name == output })
			if at < 0 || slices.Contains(p.Outputs[:j], output) {
				return steerArgs{}, fmt.Errorf("outputs: %q is no output of network %q, or is named twice", output, nets[i].name)
			}
			a.acts |= 1 << at
		}
		return a, nil
	}
}
