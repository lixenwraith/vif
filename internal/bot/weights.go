package bot

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/toml"
)

// Weights is a genome a network, by the network's name: what a document writes,
// what a population fields, or what one bot takes of another's
type Weights map[string][]float64

// Weights is what g's document writes, a network's genome under its name
func (g *Graph) Weights() Weights {
	w := make(Weights, len(g.nets))
	for _, n := range g.nets {
		w[n.name] = slices.Clone(n.genes)
	}
	return w
}

// Bare is g's networks with no weight: what a bot that starts from nothing writes
func (g *Graph) Bare() Weights {
	w := make(Weights, len(g.nets))
	for _, n := range g.nets {
		w[n.name] = make([]float64, n.geneCount())
	}
	return w
}

// Fit is what src's networks named weigh as g's own take them, each of src's
// where none is named. A weight goes by its input's name, its unit and its hidden
// unit's place, and one g has that src lacks is zero. A network, an input, a unit
// or a hidden unit g lacks fits nothing: a model is taken whole or not at all.
func (g *Graph) Fit(src *Graph, networks ...string) (Weights, error) {
	if len(networks) == 0 {
		networks = src.Networks()
	}
	w := make(Weights, len(networks))
	for _, name := range networks {
		from, to := src.net(name), g.net(name)
		switch {
		case from == nil:
			return nil, fmt.Errorf("bot %s has no network %s", src.Name, name)
		case to == nil:
			return nil, fmt.Errorf("bot %s has no network %s to take %s's", g.Name, name, src.Name)
		case from.hidden > to.hidden:
			return nil, fmt.Errorf("network %s of %s has %d hidden units, and %s's %d", name, src.Name, from.hidden, g.Name, to.hidden)
		}
		// at is where each of from's inputs, then its hidden units, stands in a row of to's
		at := make([]int, 0, len(from.names)+from.hidden)
		for _, input := range from.names {
			i := slices.Index(to.names, input)
			if i < 0 {
				return nil, fmt.Errorf("network %s of %s reads %s, which %s's does not", name, src.Name, input, g.Name)
			}
			at = append(at, i)
		}
		for h := range from.hidden {
			at = append(at, len(to.names)+h)
		}
		genes := make([]float64, to.geneCount())
		for h := range from.hidden {
			row, into := from.genes[from.unit(h):from.unit(h+1)], genes[to.unit(h):to.unit(h+1)]
			for i := range from.names {
				into[at[i]] = row[i]
			}
			into[len(into)-1] = row[len(row)-1]
		}
		for o, unit := range from.units {
			p := slices.Index(to.units, unit)
			if p < 0 {
				return nil, fmt.Errorf("network %s of %s has the output %s, which %s's does not", name, src.Name, unit, g.Name)
			}
			row, into := from.genes[from.output(o):from.output(o+1)], genes[to.output(p):to.output(p+1)]
			for i, place := range at {
				into[place] = row[i]
			}
			into[len(into)-1] = row[len(row)-1]
		}
		w[name] = genes
	}
	return w, nil
}

// net is g's network name, nil for none
func (g *Graph) net(name string) *network {
	if i := slices.IndexFunc(g.nets, func(n *network) bool { return n.name == name }); i >= 0 {
		return g.nets[i]
	}
	return nil
}

// Document is g's document with w for the weights of each network w names: the
// tables that wrote them go, and every weight not zero is written at its end, a
// table a hidden unit and one an output's unit. Every other line stays, and what
// the document then reads is w bit for bit, or it is not returned.
func (g *Graph) Document(w Weights) ([]byte, error) {
	doc, err := toml.ParseDocument(g.text)
	if err != nil {
		return nil, fmt.Errorf("bot %s: %w", g.Name, err)
	}
	var tables bytes.Buffer
	for _, name := range slices.Sorted(maps.Keys(w)) {
		n := g.net(name)
		if n == nil || len(w[name]) != n.geneCount() {
			return nil, fmt.Errorf("bot %s: no network %s of %d weights", g.Name, name, len(w[name]))
		}
		for _, key := range append([]string{"units"}, n.units...) {
			if _, err := doc.Delete(toml.Key{"network", name, key}); err != nil {
				return nil, fmt.Errorf("bot %s: %w", g.Name, err)
			}
		}
		n.spell(&tables, w[name])
	}
	text := doc.Bytes()
	if tables.Len() > 0 {
		text = slices.Concat(bytes.TrimRight(text, "\n"), []byte("\n"), tables.Bytes())
	}
	back, err := ParseGraph(g.Name, text)
	if err != nil {
		return nil, err
	}
	for name, genes := range w {
		if !slices.EqualFunc(back.net(name).genes, genes, func(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }) {
			return nil, fmt.Errorf("bot %s: network %s does not read back the weights written", g.Name, name)
		}
	}
	return text, nil
}

// spell writes genes as the tables a document gives a network's weights in: each
// hidden unit's, up to the last that has any, then each output unit's
func (n *network) spell(b *bytes.Buffer, genes []float64) {
	number := func(w float64) string {
		s := strconv.FormatFloat(w, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0" // a float in the document too, as it is in the genome
		}
		return s
	}
	written := func(row []float64) bool { return slices.ContainsFunc(row, func(w float64) bool { return w != 0 }) }
	row := func(at int, output bool) {
		for i, name := range n.names {
			if w := genes[at+i]; w != 0 {
				fmt.Fprintf(b, "%s = %s\n", name, number(w))
			}
		}
		at += len(n.names)
		if output {
			if hidden := genes[at : at+n.hidden]; written(hidden) {
				spelt := make([]string, len(hidden))
				for h, w := range hidden {
					spelt[h] = number(w)
				}
				fmt.Fprintf(b, "%s = [%s]\n", hiddenName, strings.Join(spelt, ", "))
			}
			at += n.hidden
		}
		if genes[at] != 0 {
			fmt.Fprintf(b, "%s = %s\n", biasName, number(genes[at]))
		}
	}
	last := -1
	for h := range n.hidden {
		if written(genes[n.unit(h):n.unit(h+1)]) {
			last = h
		}
	}
	for h := 0; h <= last; h++ {
		fmt.Fprintf(b, "\n[[network.%s.units]]\n", n.name)
		row(n.unit(h), false)
	}
	for o, unit := range n.units {
		if written(genes[n.output(o):n.output(o+1)]) {
			fmt.Fprintf(b, "\n[network.%s.%s]\n", n.name, unit)
			row(n.output(o), true)
		}
	}
}

// Kept is what a population's document holds for a network: the rounds scored
// and their recent mean, and the member a run fields, with its score and the
// rounds behind it, nil while none is proved.
type Kept struct {
	Rounds  int
	Recent  float64
	Fielded []float64
	Score   float64
	Proved  int
}

// Kept reads the population store keeps for g's network name, false where it
// keeps none a run would learn on from (readPopulation)
func (g *Graph) Kept(name string, store Store) (Kept, bool, error) {
	n := g.net(name)
	if n == nil {
		return Kept{}, false, fmt.Errorf("bot %s has no network %s", g.Name, name)
	}
	data, err := store.Load()
	if err != nil {
		return Kept{}, false, err
	}
	doc, members, err := readPopulation(data, n.digest, n.geneCount())
	if err != nil {
		return Kept{}, false, fmt.Errorf("%s population could not be parsed: %w", name, err)
	}
	if doc == nil {
		return Kept{}, false, nil
	}
	k := Kept{Rounds: doc.Rounds, Recent: doc.Recent}
	if f, _ := fieldedOf(members, doc.Recent); f >= 0 {
		k.Fielded, k.Score, k.Proved = members[f].Data, members[f].Score, members[f].Count
	}
	return k, true, nil
}
