package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
)

// A tutor lesson clears itself as it exits and the next lays its own text, with
// the cursor stood on the practice line's first glyph: the first lesson's x and
// notes are gone once the cursor reaches the x, and the second's stand.
func TestTutorALessonClearsItselfAndTheNextLaysItsOwn(t *testing.T) {
	opts := resource.Options{Dir: soakRoot, Scenario: "tutor"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("tutor scenario not present")
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 100, Height: 30, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	t.Cleanup(a.Close)
	w := a.World()
	rows := func() map[int]int {
		m := map[int]int{}
		w.RunSafe(func() {
			w.Components.Glyph.Each(func(e core.Entity, _ *component.GlyphComponent) bool {
				pos, _ := w.Positions.GetPosition(e)
				m[pos.Y]++
				return true
			})
		})
		return m
	}
	a.Tick(10)
	first := rows()
	if first[16] == 0 || first[14] == 0 || first[8] == 0 {
		t.Fatalf("the first lesson laid %v, want notes from row 8 and a practice block on 14 to 16", first)
	}

	inject(t, a, &input.Intent{Type: input.IntentMotion, Motion: input.MotionRight, Count: 17},
		&input.Intent{Type: input.IntentMotion, Motion: input.MotionDown, Count: 2})
	a.Tick(2)
	second := rows()
	var state string
	var cursor component.PositionComponent
	w.RunSafe(func() {
		state = w.Resources.Status.Strings.Get("fsm.state").Load()
		cursor, _ = w.Positions.GetPosition(w.Resources.Player.Entity)
	})
	if state != "Words" || second[16] != 0 || second[14] == first[14] || second[8] == first[8] {
		t.Fatalf("on the x the tutor stands in %s with glyphs %v, want Words with the first lesson's gone and its own laid", state, second)
	}
	if cursor != (component.PositionComponent{X: 4, Y: 14}) {
		t.Fatalf("the cursor stands at %v, want the practice line's first glyph (4, 14)", cursor)
	}
}

// tutorKeys types keys into a headless tutor as its player would, a tick or two
// after each so a lesson's delayed placement lands: \x1b is Escape, \x12 Ctrl-R.
func tutorKeys(t *testing.T, a *App, keys string) {
	t.Helper()
	for _, r := range keys {
		ev := terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r}
		switch r {
		case '\x1b':
			ev = terminal.Event{Type: terminal.EventKey, Key: terminal.KeyEscape}
		case '\x12':
			ev = terminal.Event{Type: terminal.EventKey, Key: terminal.KeyCtrlR}
		}
		for intent := a.inputMachine.Process(ev); intent != nil; intent = a.inputMachine.Next() {
			inject(t, a, intent)
		}
		a.Tick(2)
	}
}

// The tutor's keymap holds over the player's for its run: with an installed keymap
// binding a to auto fire and p to pause, as a copy of the defaults does, a steps
// right into Insert, as vim's append, and p puts and never pauses.
func TestTutorsKeysHoldOverThePlayersKeymap(t *testing.T) {
	sc, err := resource.LoadScenario(resource.Options{Dir: soakRoot, Scenario: "tutor"})
	if err != nil {
		t.Skip("tutor scenario not present")
	}
	opts := resource.Options{Dir: t.TempDir(), Provided: &sc}
	path := filepath.Join(opts.Dir, paths.InputDirName, paths.KeymapConfigFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[normal]\na = \"toggle_auto_fire\"\np = \"pause\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 100, Height: 30, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	t.Cleanup(a.Close)
	a.Tick(10)
	before, _ := a.World().LocalCursor()
	tutorKeys(t, a, "a")
	after, _ := a.World().LocalCursor()
	if !a.Context().IsInsertMode() || after.X != before.X+1 || after.Y != before.Y {
		t.Fatalf("a took the cursor from %v to %v, Insert %v; want one cell right, in Insert", before, after, a.Context().IsInsertMode())
	}
	tutorKeys(t, a, "\x1bp")
	if a.Context().TimeCtl.IsPaused() || !a.Context().IsNormalMode() {
		t.Fatalf("p paused the tutor or left Normal mode")
	}
}

// Each lesson on an edit ends on the keys it asks for, played through the real
// systems: a delete by motion, the Insert entries, a change, a retype, a repeat
// with its Insert text, a yank and its puts, an undo and redo, which take back no
// earlier lesson's change, the word searches, a mark and Visual, whose lesson alone
// raises the shield its d takes a block under.
func TestTutorsEditLessonsEndOnTheKeysTheyAskFor(t *testing.T) {
	opts := resource.Options{Dir: soakRoot, Scenario: "tutor"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("tutor scenario not present")
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 100, Height: 30, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	t.Cleanup(a.Close)
	w := a.World()
	read := func() (state string, glyphs int, shield bool) {
		w.RunSafe(func() {
			state = w.Resources.Status.Strings.Get("fsm.state").Load()
			glyphs = w.Components.Glyph.CountEntities()
			s, _ := w.Components.Shield.GetComponent(w.Resources.Player.Entity)
			shield = s.Active
		})
		return state, glyphs, shield
	}
	a.Tick(10)
	for _, c := range []struct{ keys, want string }{
		{"ssssss", "DeleteMotion"},
		{"dwfxD", "Insert"},
		{"s", "Append"},
		{"A}\x1bI{\x1bj^a>", "Open"},
		{"\x1bobelow\x1bkOabove", "Change"},
		{"\x1bcw\x1bjcc\x1b", "Replace"},
		{"fzrsfS5~", "Repeat"},
		{"Ino\x1bj.j.", "Yank"},
		{"yypddp", "Undo"},
		{"u", "Undo"},
		{"ddu\x12", "Bracket"},
		{"ss", "Star"},
		{"**#", "Marks"},
		{"maG'a", "Map"},
	} {
		_, before, _ := read()
		tutorKeys(t, a, c.keys)
		state, after, shield := read()
		if state != c.want || shield {
			t.Fatalf("%q left the tutor in %s with the shield up %v, want %s with it down", c.keys, state, shield, c.want)
		}
		// The last lesson's delete is no change of this one's for u to take back
		if c.keys == "u" && after != before {
			t.Fatalf("u on a new lesson left %d glyphs of %d, want what the lesson laid", after, before)
		}
	}
	tutorKeys(t, a, "sv")
	state, laid, shield := read()
	if state != "Visual" || !shield {
		t.Fatalf("s and v left the tutor in %s with the shield up %v, want Visual with it up", state, shield)
	}
	tutorKeys(t, a, "d")
	if state, left, _ := read(); state != "VisualLeave" || laid-left < 20 {
		t.Fatalf("d in Visual left the tutor in %s having taken %d glyphs, want VisualLeave and a block of them", state, laid-left)
	}
	tutorKeys(t, a, "\x1bs")
	if state, _, shield := read(); state != "Free" || shield {
		t.Fatalf("past the last lesson the tutor stands in %s with the shield up %v, want Free with it down", state, shield)
	}
}
