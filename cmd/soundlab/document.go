package main

import (
	"fmt"
	"path"
	"slices"

	"github.com/lixenwraith/vi-fighter/audio"
)

// doc is an ordered, name-keyed working set. Order is document order — it is
// what save emits and what ls shows — and survives replace-by-name so an edit
// does not shuffle the file.
type doc[T any] struct {
	order   []string
	byName  map[string]T
	dirty   map[string]bool
	src     string   // provenance for bare `save`
	include []string // sound documents only; patterns have no include form

	nameOf   func(T) string
	setName  func(T, string)
	clone    func(T) T
	modified bool // modified tracks divergence from disk
}

func newSoundDoc() *doc[*audio.SoundDef] {
	return &doc[*audio.SoundDef]{
		byName:  map[string]*audio.SoundDef{},
		dirty:   map[string]bool{},
		nameOf:  func(d *audio.SoundDef) string { return d.Name },
		setName: func(d *audio.SoundDef, n string) { d.Name = n },
		clone:   func(d *audio.SoundDef) *audio.SoundDef { return d.Clone() },
	}
}

func newPatternDoc() *doc[*audio.PatternDef] {
	return &doc[*audio.PatternDef]{
		byName:  map[string]*audio.PatternDef{},
		dirty:   map[string]bool{},
		nameOf:  func(d *audio.PatternDef) string { return d.Name },
		setName: func(d *audio.PatternDef, n string) { d.Name = n },
		clone:   clonePatternDef,
	}
}

// clonePatternDef is the deep copy PatternDef lacks. StepDef is a value
// struct, so cloning the event slices is sufficient.
func clonePatternDef(d *audio.PatternDef) *audio.PatternDef {
	c := *d
	c.Track = slices.Clone(d.Track)
	for i := range c.Track {
		c.Track[i].Event = slices.Clone(d.Track[i].Event)
	}
	return &c
}

func (d *doc[T]) has(n string) bool     { _, ok := d.byName[n]; return ok }
func (d *doc[T]) isDirty(n string) bool { return d.dirty[n] }

func (d *doc[T]) get(n string) (T, bool) { v, ok := d.byName[n]; return v, ok }

// put inserts or replaces, preserving position on replace.
func (d *doc[T]) put(v T, dirty bool) {
	n := d.nameOf(v)
	if !d.has(n) {
		d.order = append(d.order, n)
	}
	d.byName[n] = v
	if dirty {
		d.dirty[n] = true
		d.modified = true
	} else {
		delete(d.dirty, n)
	}
}

func (d *doc[T]) del(n string) bool {
	if !d.has(n) {
		return false
	}
	delete(d.byName, n)
	delete(d.dirty, n)
	if i := slices.Index(d.order, n); i >= 0 {
		d.order = slices.Delete(d.order, i, i+1)
	}
	d.modified = true
	return true
}

// rename keeps document position and marks dirty: a renamed entry always
// diverges from whatever the registry holds under either name.
func (d *doc[T]) rename(from, to string) error {
	v, ok := d.byName[from]
	if !ok {
		return fmt.Errorf("%q: not in document", from)
	}
	if d.has(to) {
		return fmt.Errorf("%q: already exists", to)
	}
	d.setName(v, to)
	d.byName[to] = v
	delete(d.byName, from)
	delete(d.dirty, from)
	if i := slices.Index(d.order, from); i >= 0 {
		d.order[i] = to
	}
	d.dirty[to] = true
	d.modified = true
	return nil
}

func (d *doc[T]) markDirty(n string) {
	if d.has(n) {
		d.dirty[n] = true
		d.modified = true
	}
}
func (d *doc[T]) clearDirty(n string) { delete(d.dirty, n) }

func (d *doc[T]) dirtyNames() []string {
	out := make([]string, 0, len(d.dirty))
	for _, n := range d.order {
		if d.dirty[n] {
			out = append(out, n)
		}
	}
	return out
}

func (d *doc[T]) all() []T {
	out := make([]T, 0, len(d.order))
	for _, n := range d.order {
		out = append(out, d.byName[n])
	}
	return out
}

func (d *doc[T]) names(glob string) ([]string, error) {
	if glob == "" || glob == "*" {
		return slices.Clone(d.order), nil
	}
	var out []string
	for _, n := range d.order {
		ok, err := path.Match(glob, n)
		if err != nil {
			return nil, fmt.Errorf("bad glob %q: %w", glob, err)
		}
		if ok {
			out = append(out, n)
		}
	}
	return out, nil
}

func (d *doc[T]) replaceAll(vs []T, src string, dirty bool) {
	d.order = d.order[:0]
	clear(d.byName)
	clear(d.dirty)
	d.src = src
	for _, v := range vs {
		d.put(v, dirty)
	}
	d.modified = true // bulk replacement is a baseline, not an edit
}

func (d *doc[T]) mergeAll(vs []T, dirty bool) {
	for _, v := range vs {
		d.put(v, dirty)
	}
}
