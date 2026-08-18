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

	// RegionSet names the regions actRegion may target; the FSM is scheduler-owned
	// and exposes no region list to a harness, so it is declared here
	RegionSet []ScriptRegion
}

// DefaultScript returns the soak profile: every action class enabled
func DefaultScript(seed uint64, steps int) ScriptOptions {
	return ScriptOptions{Seed: seed, Steps: steps, Resets: true, Regions: true, RegionSet: EmbeddedRegions}
}

// RunScript drives an App through the option's action sequence, returning the
// position it ended at
func RunScript(a *App, opt ScriptOptions) (event.Stamp, error) {
	d := &scriptDriver{a: a, rng: vmath.NewSeededRand(opt.Seed, "script"), regions: opt.RegionSet}
	table := actionTable(opt)

	total := 0
	for _, e := range table {
		total += e.weight
	}

	a.Tick(1) // warmup: the tick-1 APM fold commits an empty bucket
	for range opt.Steps {
		if !d.pick(table, total)(d) {
			return a.Position(), errors.New("script quit the game")
		}
		if opt.Perturb != nil {
			opt.Perturb(a)
		}
	}
	a.Tick(2) // settle trailing work so the comparison is not mid-effect
	return a.Position(), nil
}

// scriptDriver holds the generator stream and the App it drives
type scriptDriver struct {
	a       *App
	rng     *vmath.FastRand
	regions []ScriptRegion
}

// scriptAction is one weighted entry in the action table
type scriptAction struct {
	weight int
	run    func(*scriptDriver) bool
}

// actionTable builds the weighted action set. Tick dominates so the simulation
// actually runs between injections.
func actionTable(opt ScriptOptions) []scriptAction {
	t := []scriptAction{
		{30, (*scriptDriver).actTick},
		{20, (*scriptDriver).actMotion},
		{10, (*scriptDriver).actType},
		{8, (*scriptDriver).actFire},
		{8, (*scriptDriver).actInputTick},
		{6, (*scriptDriver).actMode},
		{6, (*scriptDriver).actSpecial},
		{4, (*scriptDriver).actCharMotion},
		{4, (*scriptDriver).actCommand},
		{3, (*scriptDriver).actResize},
		{2, (*scriptDriver).actLevel},
		{2, (*scriptDriver).actSearch},
		{2, (*scriptDriver).actOverlay},
	}
	if opt.Regions && len(opt.RegionSet) > 0 {
		t = append(t, scriptAction{2, (*scriptDriver).actRegion})
	}
	if opt.Resets {
		t = append(t, scriptAction{1, (*scriptDriver).actReset})
	}
	return t
}

// pick draws one weighted action
func (d *scriptDriver) pick(table []scriptAction, total int) func(*scriptDriver) bool {
	r := d.rng.Intn(total)
	for i := range table {
		if r < table[i].weight {
			return table[i].run
		}
		r -= table[i].weight
	}
	return table[0].run
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
func (d *scriptDriver) actTick() bool {
	d.a.Tick(1 + d.rng.Intn(4))
	return true
}

// actMotion injects one cursor motion
func (d *scriptDriver) actMotion() bool {
	return d.a.Inject(&input.Intent{
		Type:   input.IntentMotion,
		Motion: scriptMotions[d.rng.Intn(len(scriptMotions))],
		Count:  1 + d.rng.Intn(6),
	})
}

// actCharMotion injects an f/F/t/T motion against a drawn target character
func (d *scriptDriver) actCharMotion() bool {
	return d.a.Inject(&input.Intent{
		Type:   input.IntentCharMotion,
		Motion: scriptCharMotions[d.rng.Intn(len(scriptCharMotions))],
		Count:  1 + d.rng.Intn(3),
		Char:   d.char(),
	})
}

// actType injects a keystroke; it reaches the world only in Insert mode, which
// actMode enters often enough to cover the typing path
func (d *scriptDriver) actType() bool {
	return d.a.Inject(&input.Intent{Type: input.IntentTextChar, Char: d.char(), Count: 1})
}

// actMode switches mode, or leaves the current one with Escape
func (d *scriptDriver) actMode() bool {
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
func (d *scriptDriver) actSpecial() bool {
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
func (d *scriptDriver) actFire() bool {
	t := input.IntentFireMain
	if d.rng.Intn(3) == 0 {
		t = input.IntentFireSpecial
	}
	return d.a.Inject(&input.Intent{Type: t, Count: 1})
}

// actInputTick advances auto-fire and macro playback, which emit on their own cadence
func (d *scriptDriver) actInputTick() bool { return d.a.InputTick() }

// actCommand runs one ex command, keystroke by keystroke so each lands in its own
// settle group, as a live run produces
func (d *scriptDriver) actCommand() bool {
	return d.typeCommand(scriptCommands[d.rng.Intn(len(scriptCommands))])
}

// actOverlay opens an overlay and closes it, covering the paused overlay mode round trip
func (d *scriptDriver) actOverlay() bool {
	if !d.typeCommand(scriptOverlays[d.rng.Intn(len(scriptOverlays))]) {
		return false
	}
	d.a.Tick(1)
	return d.a.Inject(&input.Intent{Type: input.IntentOverlayClose, Count: 1})
}

// actSearch enters search mode, types a short pattern and confirms it
func (d *scriptDriver) actSearch() bool {
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
func (d *scriptDriver) actResize() bool {
	d.a.Resize(20+d.rng.Intn(140), 8+d.rng.Intn(44))
	return true
}

// actLevel resizes the map independently of the viewport, in both crop modes
func (d *scriptDriver) actLevel() bool {
	d.a.SetupLevel(20+d.rng.Intn(100), 10+d.rng.Intn(30),
		d.rng.Intn(2) == 0, d.rng.Intn(2) == 0)
	return true
}

// actRegion applies one FSM region operation; an invalid one is reported and dropped,
// which is itself worth reproducing
func (d *scriptDriver) actRegion() bool {
	r := d.regions[d.rng.Intn(len(d.regions))]
	d.a.Region(scriptRegionOps[d.rng.Intn(len(scriptRegionOps))], r.Name, r.State)
	return true
}

// actReset restarts the game through either the debug path or the ex command
func (d *scriptDriver) actReset() bool {
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
func (d *scriptDriver) char() rune { return rune(scriptChars[d.rng.Intn(len(scriptChars))]) }

// typeCommand runs one ex command as a full round trip: the mode switch pauses, the
// confirm unpauses and executes
func (d *scriptDriver) typeCommand(cmd string) bool {
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
