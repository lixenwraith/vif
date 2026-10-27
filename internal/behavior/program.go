// Package behavior compiles a scenario's behavior programs, each the states, edges
// and prototype of a species defined by files, into pkg/machine graphs over the
// verbs a host's Vocabulary names. Names resolve once, at load; a compiled program
// runs only the host's native code.
package behavior

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/profile"
)

// programDoc is one program as its file holds it: the fsm dialect's state keys
// with the machine's additions, and the species' prototype
type programDoc struct {
	Initial   string               `toml:"initial"`
	Suspend   *guardDoc            `toml:"suspend"`
	Prototype *prototypeDoc        `toml:"prototype"`
	States    map[string]*stateDoc `toml:"states"`
	Global    []globalDoc          `toml:"global"`
}

type guardDoc struct {
	Guard     string         `toml:"guard"`
	GuardArgs map[string]any `toml:"guard_args"`
}

type stateDoc struct {
	DurationMs  int             `toml:"duration_ms"`
	Flags       []string        `toml:"flags"`
	Probe       string          `toml:"probe"`
	ProbeArgs   map[string]any  `toml:"probe_args"`
	OnEnter     []actionDoc     `toml:"on_enter"`
	OnUpdate    []actionDoc     `toml:"on_update"`
	OnExit      []actionDoc     `toml:"on_exit"`
	Cues        []cueDoc        `toml:"cues"`
	Transitions []transitionDoc `toml:"transitions"`
}

type actionDoc struct {
	Action  string `toml:"action"`
	Event   string `toml:"event"`
	Payload any    `toml:"payload"`
}

// cueDoc is an effect at At thousandths of the entry's duration
type cueDoc struct {
	At      uint16 `toml:"at"`
	Action  string `toml:"action"`
	Event   string `toml:"event"`
	Payload any    `toml:"payload"`
}

// transitionDoc is a state's edge; target is a name or a list of candidates, and
// fallback "hold" or { retry_ms }
type transitionDoc struct {
	Trigger   string         `toml:"trigger"`
	Guard     string         `toml:"guard"`
	GuardArgs map[string]any `toml:"guard_args"`
	Target    any            `toml:"target"`
	Actions   []actionDoc    `toml:"actions"`
	Fallback  any            `toml:"fallback"`
}

// globalDoc is an edge tried first in every step of the states From names
type globalDoc struct {
	From      []string       `toml:"from"`
	Guard     string         `toml:"guard"`
	GuardArgs map[string]any `toml:"guard_args"`
	Target    any            `toml:"target"`
	Actions   []actionDoc    `toml:"actions"`
	Fallback  any            `toml:"fallback"`
}

type candidateDoc struct {
	State      string         `toml:"state"`
	Guard      string         `toml:"guard"`
	GuardArgs  map[string]any `toml:"guard_args"`
	DurationMs int            `toml:"duration_ms"`
}

type retryDoc struct {
	RetryMs int `toml:"retry_ms"`
}

type prototypeDoc struct {
	Width      int        `toml:"width"`
	Height     int        `toml:"height"`
	Header     cellDoc    `toml:"header"`
	HP         int        `toml:"hp"`
	Combat     string     `toml:"combat"`
	Homing     string     `toml:"homing"`
	Mass       float64    `toml:"mass"`
	Drops      string     `toml:"drops"`
	Contact    contactDoc `toml:"contact"`
	LifetimeMs int        `toml:"lifetime_ms"`
	PatternMs  int        `toml:"pattern_ms"`

	// The look: frames, the palettes their cells index, the colour of a glyph
	// without an entry, and the console pair a 256-colour host draws
	Frames []visual.LookFrame `toml:"frames"`
	Fg     []color.RGB        `toml:"fg"`
	Bg     []color.RGB        `toml:"bg"`
	Color  color.RGB          `toml:"color"`
	Fg256  *uint8             `toml:"fg256"`
	Bg256  *uint8             `toml:"bg256"`
}

type cellDoc struct {
	X int `toml:"x"`
	Y int `toml:"y"`
}

// contactDoc is energy drained through a shield and heat a bare cursor loses
type contactDoc struct {
	Energy int `toml:"energy"`
	Heat   int `toml:"heat"`
}

// combatRows are the rows a data species may fight as: the unit composites, whose
// damage lands on the header its members belong to
var combatRows = []component.CombatEntityType{component.CombatEntitySwarm, component.CombatEntityQuasar}

// reservedNames share kills.* with the species counters
var reservedNames = []string{"total", "uncredited"}

// checkName holds a program name to what its status keys and kill counter need
func checkName(name string) error {
	ok := len(name) > 0 && len(name) <= parameter.BehaviorNameMax && name[0] >= 'a' && name[0] <= 'z'
	for i := 1; ok && i < len(name); i++ {
		c := name[i]
		ok = c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
	}
	switch {
	case !ok:
		return fmt.Errorf("program name %.32q is not [a-z][a-z0-9_]* of at most %d bytes", name, parameter.BehaviorNameMax)
	case slices.Contains(component.SpeciesNames[:], name):
		return fmt.Errorf("program name %q is a code species", name)
	case slices.Contains(reservedNames, name):
		return fmt.Errorf("program name %q is reserved by the kill counters", name)
	}
	return nil
}

// prototype validates d and makes it the species' plain data
func (d *prototypeDoc) prototype(name string, species component.SpeciesType) (component.BehaviorPrototype, error) {
	p := component.BehaviorPrototype{
		Name: name, Species: species,
		Width: d.Width, Height: d.Height, HeaderX: d.Header.X, HeaderY: d.Header.Y,
		HitPoints: d.HP, Mass: d.Mass,
		Contact:  component.CursorDamage{EnergyDrain: d.Contact.Energy, HeatDelta: -d.Contact.Heat},
		Lifetime: time.Duration(d.LifetimeMs) * time.Millisecond,
		Pattern:  time.Duration(d.PatternMs) * time.Millisecond,
	}
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	limit := parameter.BehaviorFootprintMax
	if d.Width < 1 || d.Width > limit || d.Height < 1 || d.Height > limit {
		fail("width and height %d×%d outside 1..%d", d.Width, d.Height, limit)
	} else {
		if d.Header.X < 0 || d.Header.X >= d.Width || d.Header.Y < 0 || d.Header.Y >= d.Height {
			fail("header {%d, %d} outside the %d×%d footprint", d.Header.X, d.Header.Y, d.Width, d.Height)
		}
		art := visual.LookArt{Fg: d.Fg, Bg: d.Bg, Color: d.Color, Frames: d.Frames}
		if d.Fg256 != nil && d.Bg256 != nil {
			art.Fg256, art.Bg256, art.Console = *d.Fg256, *d.Bg256, true
		}
		var err error
		if p.Look, err = visual.CompileLook(d.Width, d.Height, art); err != nil {
			errs = append(errs, err)
		}
	}
	if (d.Fg256 == nil) != (d.Bg256 == nil) {
		fail("fg256 and bg256 come together, the pair a 256-colour host draws")
	}
	if len(d.Frames) > 1 && d.PatternMs <= 0 {
		fail("pattern_ms %d: %d frames need a positive period", d.PatternMs, len(d.Frames))
	}
	if d.HP <= 0 {
		fail("hp %d is not positive", d.HP)
	}
	if d.Mass < 0 {
		fail("mass %v is negative", d.Mass)
	}
	if d.Contact.Energy < 0 || d.Contact.Heat < 0 {
		fail("contact energy and heat are what a touch costs, not negative")
	}
	if d.LifetimeMs < 0 {
		fail("lifetime_ms %d is negative", d.LifetimeMs)
	}

	if i := slices.IndexFunc(combatRows, func(r component.CombatEntityType) bool {
		return component.CombatEntityNames[r] == d.Combat
	}); i >= 0 {
		p.Combat = combatRows[i]
	} else {
		names := make([]string, len(combatRows))
		for i, r := range combatRows {
			names[i] = component.CombatEntityNames[r]
		}
		fail("combat %q is not one of %s", d.Combat, strings.Join(names, ", "))
	}
	if h, ok := profile.Homing[d.Homing]; ok {
		p.Homing = *h
	} else {
		fail("homing %q is not one of %s", d.Homing, strings.Join(slices.Sorted(maps.Keys(profile.Homing)), ", "))
	}
	if d.Drops != "" {
		i := slices.Index(component.SpeciesNames[:], d.Drops)
		switch {
		case i < 0:
			fail("drops %q names no species", d.Drops)
		case len(profile.Drops[i]) == 0:
			fail("drops %q: that species drops nothing", d.Drops)
		default:
			p.Drops = component.SpeciesType(i)
		}
	}

	if len(errs) > 0 {
		return component.BehaviorPrototype{}, errors.Join(errs...)
	}
	return p, nil
}
