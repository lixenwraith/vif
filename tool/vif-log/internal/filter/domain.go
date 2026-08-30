package filter

import (
	"fmt"
	"strings"

	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/logfile"
)

func init() {
	Register(Desc{Kind: "dom", Help: "journal domain: both | shared | player", New: newDomain})
}

// Domain restricts the view to one journal replication scope. It is three-state
// rather than a per-domain mask: a record belongs to exactly one domain, and
// the question a capture answers is "whose state is this", not "which subset".
// DomNone is the unconstrained state, so a diagnostic log — where no record
// carries a domain at all — is unaffected until the user asks for one.
//
// Index-only: the domain is resolved during the scan, so selecting one costs a
// byte comparison per record and never reads a line.
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
