package filter

import (
	"strconv"

	"github.com/lixenwraith/vif-log/internal/logfile"
)

func init() {
	Register(Desc{Kind: "find", Help: "regexp over the focused column, smart-case", New: newFind})
}

// Find keeps records whose focused column matches the pattern. Time, tick, sub
// and msg resolve from the index row, so only fields and all read the file.
type Find struct {
	Query   string
	Col     logfile.Column
	pat     Pattern
	scratch []byte
}

// NewFind returns an inert find filter.
func NewFind() *Find { return &Find{} }

func newFind(arg string) (Filter, error) {
	f := NewFind()
	if err := f.Set(arg, logfile.ColAll); err != nil {
		return nil, err
	}
	return f, nil
}

// Set replaces the pattern and its column scope.
func (f *Find) Set(q string, col logfile.Column) error {
	p, err := NewPattern(q)
	if err != nil {
		return err
	}
	f.Query, f.Col, f.pat = q, col, p
	return nil
}

// Active reports whether the filter constrains anything.
func (f *Find) Active() bool { return !f.pat.Empty() }

func (f *Find) Kind() string { return "find" }

func (f *Find) Needs() Need {
	if !f.Active() {
		return 0
	}
	switch f.Col {
	case logfile.ColFields, logfile.ColAll:
		return NeedFields
	}
	return 0
}

func (f *Find) Match(c *Ctx) bool {
	if !f.Active() {
		return true
	}
	switch f.Col {
	case logfile.ColTime:
		f.scratch = append(f.scratch[:0], logfile.StampText(c.Meta)...)
	case logfile.ColTick:
		f.scratch = strconv.AppendUint(f.scratch[:0], uint64(c.Meta.Tick), 10)
	case logfile.ColSub:
		f.scratch = append(f.scratch[:0], c.Idx.SubName(c.Meta.Sub)...)
	case logfile.ColMsg:
		f.scratch = append(f.scratch[:0], c.Idx.MsgName(c.Meta.Msg)...)
	default:
		return f.pat.MatchBytes(c.Record().ColumnBytes(f.Col))
	}
	return f.pat.MatchBytes(f.scratch)
}

func (f *Find) Label() string {
	if !f.Active() {
		return ""
	}
	return "/" + f.Col.String() + ":" + f.Query
}
