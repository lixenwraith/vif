package filter

import (
	"fmt"
	"strconv"
	"strings"
)

func init() {
	Register(Desc{Kind: "tick", Help: "tick range: N | N-M | N- | -M", New: newTickSpan})
	Register(Desc{Kind: "run", Help: "run range: N | N-M | N- | -M", New: newRunSpan})
}

// Span is an inclusive numeric range over an index-resident counter.
type Span struct {
	field  string
	Lo, Hi uint32
	Query  string
}

func newTickSpan(arg string) (Filter, error) { return newSpan("tick", arg) }
func newRunSpan(arg string) (Filter, error)  { return newSpan("run", arg) }

func newSpan(field, arg string) (Filter, error) {
	lo, hi, err := parseSpan(arg)
	if err != nil {
		return nil, fmt.Errorf("filter/%s: %w", field, err)
	}
	return &Span{field: field, Lo: lo, Hi: hi, Query: arg}, nil
}

// parseSpan accepts N, N-M, N- and -M; an empty spec is the full range.
func parseSpan(arg string) (uint32, uint32, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, ^uint32(0), nil
	}
	lo, hi := arg, arg
	if i := strings.IndexByte(arg, '-'); i >= 0 {
		lo, hi = arg[:i], arg[i+1:]
	}
	var l uint32
	h := ^uint32(0)
	if lo != "" {
		v, err := strconv.ParseUint(lo, 10, 32)
		if err != nil {
			return 0, 0, err
		}
		l = uint32(v)
	}
	if hi != "" {
		v, err := strconv.ParseUint(hi, 10, 32)
		if err != nil {
			return 0, 0, err
		}
		h = uint32(v)
	}
	if l > h {
		l, h = h, l
	}
	return l, h, nil
}

func (f *Span) Kind() string { return f.field }
func (f *Span) Needs() Need  { return 0 }

func (f *Span) Match(c *Ctx) bool {
	v := c.Meta.Tick
	if f.field == "run" {
		v = c.Meta.Run
	}
	return v >= f.Lo && v <= f.Hi
}

func (f *Span) Label() string {
	if f.Lo == 0 && f.Hi == ^uint32(0) {
		return ""
	}
	return f.field + ":" + f.Query
}
