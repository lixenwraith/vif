package filter

import (
	"fmt"
	"strings"

	"github.com/lixenwraith/vif/tool/vif-log/internal/logfile"
)

func init() {
	Register(Desc{Kind: "dom", Help: "journal domain: both | shared | player", New: newDomain})
}

// Domain restricts the view to one journal replication scope: three states rather
// than a mask, since a record belongs to exactly one domain. DomNone admits every
// record, so a diagnostic log, where none carries a domain, is unaffected until one
// is asked for. Index-only: the scan resolved the domain, so a match is a byte
// comparison and never reads a line.
type Domain struct{ State logfile.Domain }

// NewDomain returns an unconstrained domain filter.
func NewDomain() *Domain { return &Domain{} }

func newDomain(arg string) (Filter, error) {
	d, ok := logfile.DomainByName(strings.ToLower(strings.TrimSpace(arg)))
	if !ok {
		return nil, fmt.Errorf("filter/dom: want both, shared or player, got %q", arg)
	}
	return &Domain{State: d}, nil
}

func (f *Domain) Kind() string { return "dom" }
func (f *Domain) Needs() Need  { return 0 }

func (f *Domain) Match(c *Ctx) bool {
	return f.State == logfile.DomNone || c.Meta.Dom == f.State
}

// Cycle advances the three states: both, shared, player.
func (f *Domain) Cycle() {
	f.State = (f.State + 1) % logfile.DomCount
}

// Set selects a state directly.
func (f *Domain) Set(d logfile.Domain) {
	if d < logfile.DomCount {
		f.State = d
	}
}

// Active reports whether the filter constrains anything.
func (f *Domain) Active() bool { return f.State != logfile.DomNone }

func (f *Domain) Label() string {
	if !f.Active() {
		return "" // the header strip already shows the unconstrained state
	}
	return "dom:" + f.State.String()
}
