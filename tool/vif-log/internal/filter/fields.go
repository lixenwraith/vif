package filter

import "github.com/lixenwraith/vi-fighter/tool/vif-log/internal/logfile"

func init() {
	Register(Desc{Kind: "fields", Help: "regexp over the parsed fields, smart-case", New: newFields})
}

// Fields applies a regex to the fields column. This forces disk reads for
// surviving rows to evaluate the parsed JSON, so it evaluates last.
type Fields struct {
	Query string
	pat   Pattern
}

func newFields(arg string) (Filter, error) {
	p, err := NewPattern(arg)
	if err != nil {
		return nil, err
	}
	return &Fields{Query: arg, pat: p}, nil
}

func (f *Fields) Kind() string { return "fields" }

func (f *Fields) Needs() Need {
	if f.pat.Empty() {
		return 0
	}
	return NeedFields
}

func (f *Fields) Match(c *Ctx) bool {
	if f.pat.Empty() {
		return true
	}
	// Scopes the regex exactly to the rendered "key=value" string,
	// excluding the discriminator (msg).
	return f.pat.MatchBytes(c.Record().ColumnBytes(logfile.ColFields))
}

func (f *Fields) Label() string {
	if f.pat.Empty() {
		return ""
	}
	return "fields:" + f.Query
}
