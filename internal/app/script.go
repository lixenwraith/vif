// Package app: seeded action generator.
//
// Drives a caller-driven App through a reproducible action sequence for the
// determinism soak. Actions are drawn from the script's own stream and never from
// world state, so one seed always produces one sequence.
package app

import (
	"errors"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// ScriptOptions configures the random driver
type ScriptOptions struct {
	// Perturb runs between actions, for asserting that operator-owned state
	// reaches no simulation snapshot. It must not draw from the script's stream.
	Perturb func(*App)

	Seed    uint64
	Steps   int  // action count, excluding warmup and trailing settle
	Resets  bool // allow game reset, which re-bases the journal tick counter
	Regions bool // allow FSM region operations
	Resizes bool // allow terminal resize, which a parity run must hold fixed

	// DisableTicks leaves clock advancement to a multi-participant harness.
	DisableTicks bool
	// DisableCommands excludes commands that mutate one participant's operator surface.
	DisableCommands bool
	// DisableOverlays excludes the paused overlay round trip, which advances one clock.
	DisableOverlays bool

	// MapSetups allows the operator level setup. It is the D-14 authority and is
	// replicated only because every instance runs the same map script; one injected
	// into a single participant is an operator action its peers never see.
	MapSetups bool

	// RegionSet names the regions actRegion may target; the FSM is scheduler-owned
	// and exposes no region list to a harness, so it is declared here
	RegionSet []ScriptRegion

	// MapMotionsOnly drops the viewport-relative motions, which two instances of one
	// session resolve against different geometry
	MapMotionsOnly bool
}

// DefaultScript returns the soak profile: every action class enabled
func DefaultScript(seed uint64, steps int) ScriptOptions {
	return ScriptOptions{Seed: seed, Steps: steps, Resets: true, Regions: true,
		Resizes: true, MapSetups: true, RegionSet: EmbeddedRegions}
}

// ScriptDriver applies a drawn action sequence to one App. Two drivers built from
// one seed produce identical sequences, which is what lets instances step in lockstep.
type ScriptDriver struct {
	a       *App
	rng     *vmath.FastRand
	regions []ScriptRegion
	motions []input.MotionOp

	table []scriptAction
	total int
}

// scriptAction is one weighted entry in the action table
type scriptAction struct {
	run    func(*ScriptDriver) bool
	weight int
}

// NewScriptDriver binds an option set to an App for caller-paced stepping
func NewScriptDriver(a *App, opt ScriptOptions) *ScriptDriver {
	d := &ScriptDriver{a: a, rng: vmath.NewSeededRand(opt.Seed, "script"), regions: opt.RegionSet}
	d.motions = scriptMotions[:]
	if opt.MapMotionsOnly {
		d.motions = mapMotions()
	}
	d.table = actionTable(opt)
	for _, e := range d.table {
		d.total += e.weight
	}
	return d
}

// Step applies one drawn action; false means the action quit the game
func (d *ScriptDriver) Step() bool { return d.pick()(d) }

// pick draws one weighted action
func (d *ScriptDriver) pick() func(*ScriptDriver) bool {
	r := d.rng.Intn(d.total)
	for i := range d.table {
		if r < d.table[i].weight {
			return d.table[i].run
		}
		r -= d.table[i].weight
	}
	return d.table[0].run
}

// RunScript drives an App through the option's action sequence, returning the
// position it ended at
func RunScript(a *App, opt ScriptOptions) (event.Stamp, error) {
	d := NewScriptDriver(a, opt)

	a.Tick(1) // warmup: the tick-1 APM fold commits an empty bucket
	for range opt.Steps {
		if !d.Step() {
			return a.Position(), errors.New("script quit the game")
		}
		if opt.Perturb != nil {
			opt.Perturb(a)
		}
	}
	a.Tick(2) // settle trailing work so the comparison is not mid-effect
	return a.Position(), nil
}

// actionTable builds the weighted action set. Tick dominates so the simulation
// actually runs between injections.
func actionTable(opt ScriptOptions) []scriptAction {
	// Weight zero rather than omission: the enabled sequence must not shift
	tick := 30
	if opt.DisableTicks {
		tick = 0
	}
	command := 4
	if opt.DisableCommands {
		command = 0
	}
	overlay := 2
	if opt.DisableOverlays {
		overlay = 0
	}
	resize := 3
	if !opt.Resizes {
		resize = 0
	}
	level := 2
	if !opt.MapSetups {
		level = 0
	}
	t := []scriptAction{
		{(*ScriptDriver).actTick, tick},
		{(*ScriptDriver).actMotion, 20},
		{(*ScriptDriver).actType, 10},
		{(*ScriptDriver).actFire, 8},
		{(*ScriptDriver).actInputTick, 8},
		{(*ScriptDriver).actMode, 6},
		{(*ScriptDriver).actSpecial, 6},
		{(*ScriptDriver).actCharMotion, 4},
		{(*ScriptDriver).actCommand, command},
		{(*ScriptDriver).actResize, resize},
		{(*ScriptDriver).actLevel, level},
		{(*ScriptDriver).actSearch, 2},
		{(*ScriptDriver).actOverlay, overlay},
	}
	if opt.Regions && len(opt.RegionSet) > 0 {
		t = append(t, scriptAction{(*ScriptDriver).actRegion, 2})
	}
	if opt.Resets {
		t = append(t, scriptAction{(*ScriptDriver).actReset, 1})
	}
	return t
}

// --- Alphabets ---

// scriptChars is the typing alphabet, drawn from what a source corpus contains so a
// keystroke has a real chance of matching a glyph
const scriptChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 (){}[];:=.,*&_-+/"

var scriptMotions = [...]input.MotionOp{
	input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown,
	input.MotionWordForward, input.MotionWORDForward, input.MotionWordBack,
	input.MotionWORDBack, input.MotionWordEnd, input.MotionWORDEnd,
	input.MotionLineStart, input.MotionLineEnd, input.MotionFirstNonWS,
	input.MotionScreenTop, input.MotionScreenBottom, input.MotionScreenVerticalMid,
	input.MotionScreenHorizontalMid, input.MotionHalfPageUp, input.MotionHalfPageDown,
	input.MotionHalfPageLeft, input.MotionHalfPageRight, input.MotionParaBack,
	input.MotionParaForward, input.MotionMatchBracket, input.MotionOrigin,
	input.MotionEnd, input.MotionCenter, input.MotionColumnUp, input.MotionColumnDown,
}

// viewportMotions are the only entries in scriptMotions that read ViewportWidth or
// ViewportHeight; everything else resolves against the map or the ping bounds.
var viewportMotions = map[input.MotionOp]bool{
	input.MotionHalfPageLeft:  true,
	input.MotionHalfPageRight: true,
	input.MotionHalfPageUp:    true,
	input.MotionHalfPageDown:  true,
}

// mapMotions returns scriptMotions without the viewport-relative entries
func mapMotions() []input.MotionOp {
	out := make([]input.MotionOp, 0, len(scriptMotions))
	for _, m := range scriptMotions {
		if !viewportMotions[m] {
			out = append(out, m)
		}
	}
	return out
}

var scriptCharMotions = [...]input.MotionOp{
	input.MotionFindForward, input.MotionFindBack,
	input.MotionTillForward, input.MotionTillBack,
}

// Command mode is absent: actCommand owns the whole round trip, so no stray
// confirm can execute a partially typed command
var scriptModes = [...]input.ModeTarget{
	input.ModeTargetInsert, input.ModeTargetNormal, input.ModeTargetVisual,
	input.ModeTargetInsert, input.ModeTargetNormal,
}

var scriptSpecials = [...]input.SpecialOp{
	input.SpecialDeleteChar, input.SpecialDeleteToEnd,
	input.SpecialSearchNext, input.SpecialSearchPrev,
	input.SpecialRepeatFind, input.SpecialRepeatFindRev,
}

// scriptCommands are the ex commands the soak issues. Excluded: anything that quits,
// opens a file, or takes a free-form payload.
var scriptCommands = [...]string{
	"energy 500", "energy -200", "heat 80", "heat 0", "boost", "god", "demon",
	"blossom", "decay", "cleaner", "dust", "content",
	"free on", "free off", "auto on", "auto off",
	"mouse enable", "mouse disable",
	"speed 2", "speed 1/2", "speed reset", "step off",
	"d hud", "d unpin", "flow", "graph", "flow 0",
	"system typing disable", "system typing enable",
	"system decay disable", "system decay enable",
	"region list",
}

var scriptOverlays = [...]string{"d", "h", "about"}

// ScriptRegion pairs a declared region with the state a spawn enters it at
type ScriptRegion struct{ Name, State string }

// EmbeddedRegions are the embedded config's declared regions and entry states
var EmbeddedRegions = []ScriptRegion{
	{"main", "MainSpawnGold"},
	{"quasar", "QuasarFuse"},
	{"storm", "StormSetup"},
	{"monitor", "MonitorWarmup"},
	{"placeholder", "PlaceholderSetup"},
}

var scriptRegionOps = [...]string{
	event.RegionList, event.RegionSpawn, event.RegionPause,
	event.RegionResume, event.RegionTerminate,
}

// --- Actions ---

// actTick advances the simulation, which is what makes the other actions land
func (d *ScriptDriver) actTick() bool {
	d.a.Tick(1 + d.rng.Intn(4))
	return true
}

// actMotion injects one cursor motion
func (d *ScriptDriver) actMotion() bool {
	return d.a.Inject(&input.Intent{
		Type:   input.IntentMotion,
		Motion: d.motions[d.rng.Intn(len(d.motions))],
		Count:  1 + d.rng.Intn(6),
	})
}

// actCharMotion injects an f/F/t/T motion against a drawn target character
func (d *ScriptDriver) actCharMotion() bool {
	return d.a.Inject(&input.Intent{
		Type:   input.IntentCharMotion,
		Motion: scriptCharMotions[d.rng.Intn(len(scriptCharMotions))],
		Count:  1 + d.rng.Intn(3),
		Char:   d.char(),
	})
}

// actType injects a keystroke; it reaches the world only in Insert mode, which
// actMode enters often enough to cover the typing path
func (d *ScriptDriver) actType() bool {
	return d.a.Inject(&input.Intent{Type: input.IntentTextChar, Char: d.char(), Count: 1})
}

// actMode switches mode, or leaves the current one with Escape
func (d *ScriptDriver) actMode() bool {
	if d.rng.Intn(4) == 0 {
		return d.a.Inject(&input.Intent{Type: input.IntentEscape, Count: 1})
	}
	return d.a.Inject(&input.Intent{
		Type:       input.IntentModeSwitch,
		ModeTarget: scriptModes[d.rng.Intn(len(scriptModes))],
		Count:      1,
	})
}

// actSpecial injects a delete, search-repeat or find-repeat command
func (d *ScriptDriver) actSpecial() bool {
	if d.rng.Intn(5) == 0 {
		return d.a.Inject(&input.Intent{
			Type: input.IntentOperatorLine, Operator: input.OperatorDelete,
			Count: 1 + d.rng.Intn(3),
		})
	}
	return d.a.Inject(&input.Intent{
		Type:    input.IntentSpecial,
		Special: scriptSpecials[d.rng.Intn(len(scriptSpecials))],
		Count:   1 + d.rng.Intn(3),
	})
}

// actFire requests a weapon or special discharge
func (d *ScriptDriver) actFire() bool {
	t := input.IntentFireMain
	if d.rng.Intn(3) == 0 {
		t = input.IntentFireSpecial
	}
	return d.a.Inject(&input.Intent{Type: t, Count: 1})
}

// actInputTick advances auto-fire and macro playback, which emit on their own cadence
func (d *ScriptDriver) actInputTick() bool { return d.a.InputTick() }

// actCommand runs one ex command, keystroke by keystroke so each lands in its own
// settle group, as a live run produces
func (d *ScriptDriver) actCommand() bool {
	return d.typeCommand(scriptCommands[d.rng.Intn(len(scriptCommands))])
}

// actOverlay opens an overlay and closes it, covering the paused overlay mode round trip
func (d *ScriptDriver) actOverlay() bool {
	if !d.typeCommand(scriptOverlays[d.rng.Intn(len(scriptOverlays))]) {
		return false
	}
	d.a.Tick(1)
	return d.a.Inject(&input.Intent{Type: input.IntentOverlayClose, Count: 1})
}

// actSearch enters search mode, types a short pattern and confirms it
func (d *ScriptDriver) actSearch() bool {
	if !d.a.Inject(&input.Intent{Type: input.IntentEscape, Count: 1}) ||
		!d.a.Inject(&input.Intent{Type: input.IntentModeSwitch, ModeTarget: input.ModeTargetSearch, Count: 1}) {
		return false
	}
	for range 1 + d.rng.Intn(3) {
		if !d.a.Inject(&input.Intent{Type: input.IntentTextChar, Char: d.char(), Count: 1}) {
			return false
		}
	}
	return d.a.Inject(&input.Intent{Type: input.IntentTextConfirm, Count: 1})
}

// actResize reports a terminal change; dimensions stay above the margins so
// ScreenSize keeps inverting updateGameArea exactly
func (d *ScriptDriver) actResize() bool {
	d.a.Resize(20+d.rng.Intn(140), 8+d.rng.Intn(44))
	return true
}

// actLevel resizes the map independently of the viewport, in both crop modes
func (d *ScriptDriver) actLevel() bool {
	d.a.SetupLevel(20+d.rng.Intn(100), 10+d.rng.Intn(30),
		d.rng.Intn(2) == 0, d.rng.Intn(2) == 0)
	return true
}

// actRegion applies one FSM region operation; an invalid one is reported and dropped,
// which is itself worth reproducing
func (d *ScriptDriver) actRegion() bool {
	r := d.regions[d.rng.Intn(len(d.regions))]
	d.a.Region(scriptRegionOps[d.rng.Intn(len(scriptRegionOps))], r.Name, r.State)
	return true
}

// actReset restarts the game through either the debug path or the ex command
func (d *ScriptDriver) actReset() bool {
	purge := d.rng.Intn(4) == 0
	if d.rng.Intn(2) == 0 {
		cmd := "new"
		if purge {
			cmd = "new!"
		}
		return d.typeCommand(cmd)
	}
	d.a.Reset(purge)
	return true
}

// --- Helpers ---

// char draws one printable character
func (d *ScriptDriver) char() rune { return rune(scriptChars[d.rng.Intn(len(scriptChars))]) }

// typeCommand runs one ex command as a full round trip: the mode switch pauses, the
// confirm unpauses and executes
func (d *ScriptDriver) typeCommand(cmd string) bool {
	if !d.a.Inject(&input.Intent{Type: input.IntentModeSwitch, ModeTarget: input.ModeTargetCommand, Count: 1}) {
		return false
	}
	for _, c := range cmd {
		if !d.a.Inject(&input.Intent{Type: input.IntentTextChar, Char: c, Count: 1}) {
			return false
		}
	}
	return d.a.Inject(&input.Intent{Type: input.IntentTextConfirm, Count: 1})
}
