package filter

import (
	"fmt"
	"sort"

	"github.com/lixenwraith/vif-log/internal/logfile"
)

// Need declares what a filter must touch beyond the index row.
type Need uint8

const (
	NeedRaw    Need = 1 << iota // raw line bytes
	NeedFields                  // parsed fields
)

// Ctx is the per-record evaluation context. Raw bytes and parsed fields are
// fetched lazily, so index-only predicates never touch the file.
type Ctx struct {
	Idx  *logfile.Index
	Meta logfile.Meta
	I    int

	rd    *logfile.Reader
	raw   []byte
	rec   logfile.Record
	rawOK bool
	recOK bool
}

// Bind attaches the index and the line reader used for raw access.
func (c *Ctx) Bind(idx *logfile.Index, rd *logfile.Reader) { c.Idx, c.rd = idx, rd }

// Reset points the context at record i.
func (c *Ctx) Reset(i int, m logfile.Meta) {
	c.I, c.Meta = i, m
	c.raw, c.rawOK, c.recOK = nil, false, false
}

// Raw returns the record's line bytes, nil when unavailable.
func (c *Ctx) Raw() []byte {
	if !c.rawOK {
		c.rawOK = true
		if c.rd != nil {
			c.raw, _ = c.rd.Line(c.Meta)
		}
	}
	return c.raw
}

// Record returns the parsed record.
func (c *Ctx) Record() *logfile.Record {
	if !c.recOK {
		c.recOK = true
		c.rec.Parse(c.Meta, c.Raw())
	}
	return &c.rec
}

// Filter is one predicate in the stack.
type Filter interface {
	Kind() string
	Label() string
	Needs() Need
	Match(*Ctx) bool
}

// Entry wraps a filter with the stack-level toggles, so negation and disabling
// need no per-filter code.
type Entry struct {
	F       Filter
	Enabled bool
	Negate  bool
}

// Stack is the composed filter chain, evaluated cheapest-first.
type Stack struct {
	Entries []Entry
	order   []int
	needs   Need
}

// Compile recomputes the evaluation order; call after mutating Entries.
func (s *Stack) Compile() {
	s.order = s.order[:0]
	s.needs = 0
	for i, e := range s.Entries {
		if !e.Enabled {
			continue
		}
		s.order = append(s.order, i)
		s.needs |= e.F.Needs()
	}
	// Index-only predicates first: raw readers then only see survivors.
	sort.SliceStable(s.order, func(a, b int) bool {
		return s.Entries[s.order[a]].F.Needs() < s.Entries[s.order[b]].F.Needs()
	})
}

// Needs reports the union of the enabled filters' requirements.
func (s *Stack) Needs() Need { return s.needs }

// Match evaluates the enabled filters in cost order.
func (s *Stack) Match(c *Ctx) bool {
	for _, i := range s.order {
		e := s.Entries[i]
		if e.F.Match(c) == e.Negate {
			return false
		}
	}
	return true
}

// Add appends an enabled filter and recompiles.
func (s *Stack) Add(f Filter) {
	s.Entries = append(s.Entries, Entry{F: f, Enabled: true})
	s.Compile()
}

// Find returns the first entry of the given kind.
func (s *Stack) Find(kind string) (*Entry, bool) {
	for i := range s.Entries {
		if s.Entries[i].F.Kind() == kind {
			return &s.Entries[i], true
		}
	}
	return nil, false
}

// Set replaces the entry of f's kind, appending when absent.
func (s *Stack) Set(f Filter) {
	for i := range s.Entries {
		if s.Entries[i].F.Kind() == f.Kind() {
			s.Entries[i].F, s.Entries[i].Enabled = f, true
			s.Compile()
			return
		}
	}
	s.Add(f)
}

// Remove drops the entry of the given kind.
func (s *Stack) Remove(kind string) bool {
	for i := range s.Entries {
		if s.Entries[i].F.Kind() == kind {
			s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
			s.Compile()
			return true
		}
	}
	return false
}

// Summary renders the stack for the status bar: (disabled) and !negated.
func (s *Stack) Summary() []string {
	out := make([]string, 0, len(s.Entries))
	for _, e := range s.Entries {
		l := e.F.Label()
		if l == "" {
			continue // filter is inert; the header strip or nothing reports it
		}
		if e.Negate {
			l = "!" + l
		}
		if !e.Enabled {
			l = "(" + l + ")"
		}
		out = append(out, l)
	}
	return out
}

// Desc describes a registered filter kind for menus and help.
type Desc struct {
	Kind string
	Help string
	New  func(arg string) (Filter, error)
}

var registry []Desc

// Register makes a filter kind constructible by name. A new kind is a new file
// plus one Register call; no render-path switch changes.
func Register(d Desc) { registry = append(registry, d) }

// Kinds returns the registered filter kinds.
func Kinds() []Desc { return registry }

// New constructs a registered filter.
func New(kind, arg string) (Filter, error) {
	for _, d := range registry {
		if d.Kind == kind {
			return d.New(arg)
		}
	}
	return nil, fmt.Errorf("filter: unknown kind %q", kind)
}
