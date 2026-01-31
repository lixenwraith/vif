package game

import (
	"sync/atomic"
)

type PhenotypeCache[P any] struct {
	current    atomic.Pointer[P]
	generation atomic.Uint64
}

func NewPhenotypeCache[P any](initial P) *PhenotypeCache[P] {
	c := &PhenotypeCache[P]{}
	c.current.Store(&initial)
	return c
}

func (c *PhenotypeCache[P]) Get() P {
	return *c.current.Load()
}

func (c *PhenotypeCache[P]) Update(p P) {
	c.current.Store(&p)
	c.generation.Add(1)
}

func (c *PhenotypeCache[P]) Generation() uint64 {
	return c.generation.Load()
}