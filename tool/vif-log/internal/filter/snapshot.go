package filter

import (
	"strings"

	"github.com/lixenwraith/vif-log/internal/logfile"
)

func init() {
	Register(Desc{Kind: "snap", Help: "collapse stat snapshots to one line (arg: off)", New: newCollapse})
}

// Collapse hides stat-snapshot members, turning a ~40-record snapshot into one
// navigable line. On is the global state; exc holds the per-group exceptions
// toggled with Enter. Index-only, so it costs nothing to evaluate.
type Collapse struct {
	On  bool
	exc map[uint32]bool
}

// NewCollapse returns a collapse filter in the given global state.
func NewCollapse(on bool) *Collapse {
	return &Collapse{On: on, exc: make(map[uint32]bool)}
}

func newCollapse(arg string) (Filter, error) {
	return NewCollapse(strings.TrimSpace(arg) != "off"), nil
}

func (f *Collapse) Kind() string { return "snap" }
func (f *Collapse) Needs() Need  { return 0 }

func (f *Collapse) Match(c *Ctx) bool {
	if c.Meta.Snap == 0 || c.Meta.Flags&logfile.FlagSnapHead != 0 {
		return true
	}
	return f.Expanded(c.Meta.Snap)
}

// Expanded reports whether group id shows its members.
func (f *Collapse) Expanded(id uint32) bool { return f.On == f.exc[id] }

// ToggleGroup flips one group against the global state.
func (f *Collapse) ToggleGroup(id uint32) { f.exc[id] = !f.exc[id] }

// ResetGroups drops the per-group exceptions; group ids are file-scoped.
func (f *Collapse) ResetGroups() { clear(f.exc) }

// Toggle flips the global state and drops all per-group exceptions.
func (f *Collapse) Toggle() {
	f.On = !f.On
	clear(f.exc)
}

func (f *Collapse) Label() string {
	s := "snap:expanded"
	if f.On {
		s = "snap:collapsed"
	}
	if len(f.exc) > 0 {
		s += "*"
	}
	return s
}
