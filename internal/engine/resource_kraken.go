package engine

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
)

// KrakenShapeResource holds the tentacle samples the Kraken system built this tick,
// each with the pose it built them from. Systems write, renderers read: a renderer
// shading the same pose takes the samples instead of sampling the shape again. It is
// derived state and never copied with a world, since equal poses give equal samples.
type KrakenShapeResource struct {
	shapes []KrakenShape
	count  int
}

// KrakenShape is one Kraken's samples and the pose they came from
type KrakenShape struct {
	Entity  core.Entity
	Kraken  component.KrakenComponent
	X, Y    float64
	Samples []component.KrakenSample
}

// NewKrakenShapeResource creates an empty resource
func NewKrakenShapeResource() *KrakenShapeResource {
	return &KrakenShapeResource{}
}

// Reset drops every shape and keeps their storage for the next tick's
func (r *KrakenShapeResource) Reset() { r.count = 0 }

// Build samples e's shape at its pose, replacing what e held, and returns the samples
func (r *KrakenShapeResource) Build(e core.Entity, k *component.KrakenComponent, x, y float64) []component.KrakenSample {
	i := 0
	for i < r.count && r.shapes[i].Entity != e {
		i++
	}
	if i == r.count {
		if r.count == len(r.shapes) {
			r.shapes = append(r.shapes, KrakenShape{})
		}
		r.count++
	}
	s := &r.shapes[i]
	s.Entity, s.Kraken, s.X, s.Y = e, *k, x, y
	s.Samples = k.AppendTentacleSamples(s.Samples[:0], x, y)
	return s.Samples
}

// Samples returns e's samples when they were built from this pose
func (r *KrakenShapeResource) Samples(e core.Entity, k *component.KrakenComponent, x, y float64) ([]component.KrakenSample, bool) {
	for i := range r.count {
		if s := &r.shapes[i]; s.Entity == e && s.Kraken == *k && s.X == x && s.Y == y {
			return s.Samples, true
		}
	}
	return nil, false
}
