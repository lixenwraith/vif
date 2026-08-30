package filter

import "github.com/lixenwraith/vi-fighter/tool/vif-log/internal/logfile"

func init() {
	Register(Desc{Kind: "sub", Help: "subsystem regexp, e.g. sub:^(fsm|rec)$", New: newSub})
	Register(Desc{Kind: "msg", Help: "message regexp, e.g. msg:transition", New: newMsg})
}

// Text matches one of the interned vocabulary columns. The pattern is applied
// once per interned id into a bitset, so the per-record cost is a bit probe
// and no line is ever read.
type Text struct {
	kind  string
	Query string
	pat   Pattern
	mask  []uint64
	n     int
}

func newSub(arg string) (Filter, error) { return newText("sub", arg) }
func newMsg(arg string) (Filter, error) { return newText("msg", arg) }

func newText(kind, arg string) (Filter, error) {
	p, err := NewPattern(arg)
	if err != nil {
		return nil, err
	}
	return &Text{kind: kind, Query: arg, pat: p}, nil
}

func (f *Text) Kind() string { return f.kind }
func (f *Text) Needs() Need  { return 0 }

func (f *Text) Match(c *Ctx) bool {
	if f.pat.Empty() {
		return true
	}
	var id int
	var tab []string
	if f.kind == "sub" {
		id, tab = int(c.Meta.Sub), c.Idx.Subs()
	} else {
		id, tab = int(c.Meta.Msg), c.Idx.Msgs()
	}
	f.ensure(tab)
	if id < 0 || id >= f.n {
		return false
	}
	return f.mask[id>>6]&(1<<uint(id&63)) != 0
}

// ensure rebuilds the bitset when the interned table has grown.
func (f *Text) ensure(tab []string) {
	if f.mask != nil && f.n == len(tab) {
		return
	}
	need := (len(tab) + 63) / 64
	if cap(f.mask) < need {
		f.mask = make([]uint64, need)
	} else {
		f.mask = f.mask[:need]
		clear(f.mask)
	}
	for i, s := range tab {
		if f.pat.MatchString(s) {
			f.mask[i>>6] |= 1 << uint(i&63)
		}
	}
	f.n = len(tab)
}

func (f *Text) Label() string {
	if f.pat.Empty() {
		return ""
	}
	return f.kind + ":" + f.Query
}

// SubOf resolves the column a text filter binds to; used by the help overlay.
func (f *Text) Column() logfile.Column {
	if f.kind == "sub" {
		return logfile.ColSub
	}
	return logfile.ColMsg
}
