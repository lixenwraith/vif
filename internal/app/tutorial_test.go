package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
)

// A tutorial lesson clears itself as it exits and the next lays its own text, with
// the cursor stood on the practice line's first glyph: the first lesson's x and
// notes are gone once the cursor reaches the x, and the second's stand.
func TestTutorialALessonClearsItselfAndTheNextLaysItsOwn(t *testing.T) {
	opts := resource.Options{Dir: soakRoot, Scenario: "tutorial"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("tutorial scenario not present")
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
		t.Fatalf("on the x the tutorial stands in %s with glyphs %v, want Words with the first lesson's gone and its own laid", state, second)
	}
	if cursor != (component.PositionComponent{X: 4, Y: 14}) {
		t.Fatalf("the cursor stands at %v, want the practice line's first glyph (4, 14)", cursor)
	}
}

// tutorialKeys types keys into a headless tutorial as its player would, a tick or two
// after each so a lesson's delayed placement lands: \x1b is Escape, \x12 Ctrl-R.
func tutorialKeys(t *testing.T, a *App, keys string) {
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

// The tutorial's keymap holds over the player's for its run: with an installed keymap
// binding a to auto fire and p to pause, as a copy of the defaults does, a steps
// right into Insert, as vim's append, and p puts and never pauses.
func TestTutorialsKeysHoldOverThePlayersKeymap(t *testing.T) {
	sc, err := resource.LoadScenario(resource.Options{Dir: soakRoot, Scenario: "tutorial"})
	if err != nil {
		t.Skip("tutorial scenario not present")
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
	tutorialKeys(t, a, "a")
	after, _ := a.World().LocalCursor()
	if !a.Context().IsInsertMode() || after.X != before.X+1 || after.Y != before.Y {
		t.Fatalf("a took the cursor from %v to %v, Insert %v; want one cell right, in Insert", before, after, a.Context().IsInsertMode())
	}
	tutorialKeys(t, a, "\x1bp")
	if a.Context().TimeCtl.IsPaused() || !a.Context().IsNormalMode() {
		t.Fatalf("p paused the tutorial or left Normal mode")
	}
}

// The vim keymap gives back each key the tutorial's keymap does, to the same
// action, and a run that names it plays them so in any game: a steps into Insert
// where the built-in keymap cycles auto fire.
func TestTheVimKeymapPlaysTheKeysTheTutorialTaught(t *testing.T) {
	sc, err := resource.LoadScenario(resource.Options{Dir: soakRoot, Scenario: "tutorial"})
	if err != nil {
		t.Skip("tutorial scenario not present")
	}
	data, _ := fs.ReadFile(asset.Keymaps, "vim.toml")
	vim, err := input.LoadKeyConfig(data)
	if err != nil || len(sc.Keys.NormalRunes) == 0 {
		t.Fatalf("the vim keymap, %v; the tutorial binds %d keys", err, len(sc.Keys.NormalRunes))
	}
	for r, entry := range sc.Keys.NormalRunes {
		if vim.NormalRunes[r] != entry {
			t.Errorf("%q: the tutorial binds %+v, the vim keymap %+v", r, entry, vim.NormalRunes[r])
		}
	}
	for k, entry := range sc.Keys.SpecialKeys {
		if vim.SpecialKeys[k] != entry {
			t.Errorf("%s: the tutorial binds %+v, the vim keymap %+v", input.SpecialKeyName(k), entry, vim.SpecialKeys[k])
		}
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 100, Height: 30,
		Resources: resource.Options{Embedded: true, Keymap: "vim"}})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	t.Cleanup(a.Close)
	tickUntilCursor(t, a)
	if tutorialKeys(t, a, "a"); !a.Context().IsInsertMode() {
		t.Fatal("a did not enter Insert in a run on the vim keymap")
	}
}

// Each lesson on an edit ends on the keys it asks for, played through the real
// systems: a delete by motion, the Insert entries, a change, a retype, a repeat
// with its Insert text, a yank and its puts, an undo and redo, which take back no
// earlier lesson's change, the word searches, a mark and Visual, whose lesson alone
// raises the shield its d takes a block under.
func TestTutorialsEditLessonsEndOnTheKeysTheyAskFor(t *testing.T) {
	opts := resource.Options{Dir: soakRoot, Scenario: "tutorial"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("tutorial scenario not present")
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
		tutorialKeys(t, a, c.keys)
		state, after, shield := read()
		if state != c.want || shield {
			t.Fatalf("%q left the tutorial in %s with the shield up %v, want %s with it down", c.keys, state, shield, c.want)
		}
		// The last lesson's delete is no change of this one's for u to take back
		if c.keys == "u" && after != before {
			t.Fatalf("u on a new lesson left %d glyphs of %d, want what the lesson laid", after, before)
		}
	}
	tutorialKeys(t, a, "sv")
	state, laid, shield := read()
	if state != "Visual" || !shield {
		t.Fatalf("s and v left the tutorial in %s with the shield up %v, want Visual with it up", state, shield)
	}
	tutorialKeys(t, a, "d")
	if state, left, _ := read(); state != "VisualLeave" || laid-left < 20 {
		t.Fatalf("d in Visual left the tutorial in %s having taken %d glyphs, want VisualLeave and a block of them", state, laid-left)
	}
	tutorialKeys(t, a, "\x1bs")
	if state, _, shield := read(); state != "Free" || shield {
		t.Fatalf("past the last lesson the tutorial stands in %s with the shield up %v, want Free with it down", state, shield)
	}
}

// tutorialRow reads the glyphs on row y from column 4, a gap as a space.
func tutorialRow(a *App, y int) string {
	w := a.World()
	cells := map[int]rune{}
	last := -1
	w.RunSafe(func() {
		w.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
			if pos, ok := w.Positions.GetPosition(e); ok && pos.Y == y && pos.X >= 4 {
				cells[pos.X] = g.Rune
				last = max(last, pos.X)
			}
			return true
		})
	})
	var row []rune
	for x := 4; x <= last; x++ {
		if r, ok := cells[x]; ok {
			row = append(row, r)
		} else {
			row = append(row, ' ')
		}
	}
	return string(row)
}

// A participant that arrives at a lesson by installing it, as a join or a reload
// does, lays that lesson's text in place of the one its own boot laid: the first
// lesson's practice block and x are gone, and the second's line stands.
func TestTutorialAnInstalledLessonLaysItsOwnText(t *testing.T) {
	opts := resource.Options{Dir: soakRoot, Scenario: "tutorial"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("tutorial scenario not present")
	}
	boot := func() *App {
		a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 100, Height: 30, Resources: opts})
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		t.Cleanup(a.Close)
		a.Tick(10)
		return a
	}
	host, joiner := boot(), boot()
	tutorialKeys(t, host, "s")
	if got := tutorialRow(joiner, 16); got != "and right to the x" {
		t.Fatalf("the first lesson laid %q on row 16, want its last practice line", got)
	}

	capture := host.scheduler.ExportFSM()
	joiner.World().RunSafe(func() {
		if err := joiner.scheduler.ImportFSM(capture, true); err != nil {
			t.Errorf("import fsm: %v", err)
		}
	})
	joiner.Settle()
	if got := tutorialRow(joiner, 16); got != "" {
		t.Fatalf("the installed lesson left %q on row 16, want the first lesson's block gone", got)
	}
	if got, want := tutorialRow(joiner, 14), tutorialRow(host, 14); got != want || got == "" {
		t.Fatalf("the installed lesson reads %q on row 14, want the second lesson's %q", got, want)
	}
}

// The first lesson's x ends its third line as a word does, so $, w and e reach it
// as they reach any word's glyph, and h j k l still do.
func TestTutorialsFirstMarkIsAWordsGlyph(t *testing.T) {
	opts := resource.Options{Dir: soakRoot, Scenario: "tutorial"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("tutorial scenario not present")
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 100, Height: 30, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	t.Cleanup(a.Close)
	a.Tick(10)
	state := func() (s string) {
		a.World().RunSafe(func() { s = a.World().Resources.Status.Strings.Get("fsm.state").Load() })
		return s
	}
	for _, keys := range []string{"jj$", "jj4w", "jj5e", "jj" + strings.Repeat("l", 17)} {
		tutorialKeys(t, a, keys)
		if got := state(); got != "Words" {
			t.Fatalf("%q left the tutorial in %s, want Words", keys, got)
		}
		if tutorialKeys(t, a, "S"); state() != "Move" {
			t.Fatalf("S from Words left the tutorial in %s, want Move", state())
		}
	}
}
