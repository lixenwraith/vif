package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	link "github.com/lixenwraith/vif/pkg/network"
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
// after each so a lesson's delayed placement lands, and the input tick a live run
// gives the router, which takes a lesson's ask for a mode: \x1b is Escape, \x12 Ctrl-R.
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
		a.InputTick()
	}
}

// newTutorial boots a headless tutorial on a width by height terminal with its first
// lesson laid, or skips where the scenario is not present
func newTutorial(t *testing.T, width, height int) *App {
	t.Helper()
	opts := resource.Options{Dir: soakRoot, Scenario: "tutorial"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("tutorial scenario not present")
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: width, Height: height, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	t.Cleanup(a.Close)
	a.Tick(10)
	return a
}

// tutorialState is the state the tutorial stands in
func tutorialState(a *App) (state string) {
	a.World().RunSafe(func() { state = a.World().Resources.Status.Strings.Get("fsm.state").Load() })
	return state
}

// tutorialTo skips to lesson with s, as a player may
func tutorialTo(t *testing.T, a *App, lesson string) {
	t.Helper()
	for range 30 {
		if tutorialState(a) == lesson {
			return
		}
		tutorialKeys(t, a, "s")
	}
	t.Fatalf("s never reached %s; the tutorial stands in %s", lesson, tutorialState(a))
}

// tutorialInstall imports host's machine into joiner as a join or a reload does
func tutorialInstall(t *testing.T, host, joiner *App) {
	t.Helper()
	capture := host.scheduler.ExportFSM()
	joiner.World().RunSafe(func() {
		if err := joiner.scheduler.ImportFSM(capture, true); err != nil {
			t.Errorf("import fsm: %v", err)
		}
	})
	joiner.Settle()
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
	for r, entry := range sc.Keys.OperatorMotions {
		if vim.OperatorMotions[r] != entry {
			t.Errorf("%q after an operator: the tutorial binds %+v, the vim keymap %+v", r, entry, vim.OperatorMotions[r])
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
// raises the shield its d takes a block under. One that ends typing waits for esc.
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
		{"A}\x1bI{\x1bj^a>", "AppendEsc"},
		{"\x1b", "Open"},
		{"obelow\x1bkOabove", "OpenEsc"},
		{"\x1b", "Change"},
		{"cw\x1bjcc", "Replace"},
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

// Insert and Repeat end on their last glyph, not on a count of glyphs typed, so a
// glyph gone some other way never strands them: a typo's Space deletes the glyph
// it missed, and a delete repeated with . clears no as a repeated I no does.
func TestTutorialsTypingLessonsEndHoweverTheirGlyphsGo(t *testing.T) {
	for _, c := range []struct{ lesson, keys, want string }{
		{"Insert", "itypo me\x1bj^ithen me\x1b", "Append"},
		{"Repeat", "dwj.j.", "Yank"},
		{"Repeat", "2xj.j.", "Yank"},
	} {
		a := newTutorial(t, 100, 30)
		tutorialTo(t, a, c.lesson)
		if tutorialKeys(t, a, c.keys); tutorialState(a) != c.want {
			t.Errorf("%q left the tutorial in %s, want %s", c.keys, tutorialState(a), c.want)
		}
	}
}

// The tutorial's map floor is the view an 80 by 24 terminal shows, so no lesson
// there scrolls: G lands on a row in view and the next heading shows whole.
func TestTutorialsMapIsTheViewOfAnEightyByTwentyFourTerminal(t *testing.T) {
	a := newTutorial(t, 80, 24)
	var width, height int
	a.World().RunSafe(func() { width, height = a.World().Resources.Config.MapWidth, a.World().Resources.Config.MapHeight })
	if vw, vh := engine.Viewport(80, 24); width != vw || height != vh {
		t.Fatalf("at 80 by 24 the tutorial's map is %d by %d, want the view's %d by %d", width, height, vw, vh)
	}
}

// Free practice lays the corpus for the player alone to type and edit: Space, a
// game's special fire, which auto fire presses too, turns none of its dark glyphs
// to dust, since the tutorial runs no particle system.
func TestTutorialsFreePracticeKeepsItsCode(t *testing.T) {
	a := newTutorial(t, 100, 30)
	w := a.World()
	dark := func() (n int) {
		w.RunSafe(func() {
			w.Components.Glyph.Each(func(_ core.Entity, g *component.GlyphComponent) bool {
				if g.Level == component.GlyphDark {
					n++
				}
				return true
			})
		})
		return n
	}
	tutorialTo(t, a, "Free")
	a.Tick(600)
	before := dark()
	if before == 0 {
		t.Fatal("Free laid no dark glyph to keep")
	}
	if tutorialKeys(t, a, "  "); dark() < before {
		t.Fatalf("Space in Free took %d of %d dark glyphs, want none", before-dark(), before)
	}
}

// The tutorial gives the backtick to vi with m: `a returns to the marked cell
// itself, which ends the marks lesson, and opens no console over it.
func TestTutorialsBacktickReturnsToTheMarkedCell(t *testing.T) {
	a := newTutorial(t, 100, 30)
	tutorialTo(t, a, "Marks")
	tutorialKeys(t, a, "maG`a")
	if got := tutorialState(a); got != "Map" || !a.Context().IsNormalMode() {
		t.Fatalf("ma, G and `a left the tutorial in %s, Normal %v; want Map, in Normal", got, a.Context().IsNormalMode())
	}
}

// Free lays each corpus block on rows of its own, an empty row each side, so %
// pairs a bracket inside its block: every run of rows holding glyphs is one block,
// five rows at most, its brackets balanced.
func TestTutorialsFreeLaysEachBlockAlone(t *testing.T) {
	a := newTutorial(t, 80, 24)
	w := a.World()
	tutorialTo(t, a, "Free")
	a.Tick(3000)
	var height int
	rows := map[int][]rune{}
	w.RunSafe(func() {
		height = w.Resources.Config.MapHeight
		w.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
			pos, _ := w.Positions.GetPosition(e)
			rows[pos.Y] = append(rows[pos.Y], g.Rune)
			return true
		})
	})
	runs := 0
	for y := 0; y < height; y++ {
		if len(rows[y]) == 0 || len(rows[y-1]) != 0 {
			continue
		}
		var text string
		end := y
		for ; len(rows[end]) != 0; end++ {
			text += string(rows[end])
		}
		runs++
		if end-y > 5 {
			t.Errorf("rows %d to %d hold glyphs, more than one block's five", y, end-1)
		}
		for _, pair := range []string{"{}", "()", "[]"} {
			if open, shut := strings.Count(text, pair[:1]), strings.Count(text, pair[1:]); open != shut {
				t.Errorf("rows %d to %d hold %d %c and %d %c, want a block's pairs", y, end-1, open, pair[0], shut, pair[1])
			}
		}
	}
	if runs < 2 {
		t.Fatalf("Free laid %d blocks, want its corpus", runs)
	}
}

// Stand places the cursor last spawned: one that went and came back as a new
// entity, as a rejoin after its wraith went does, is stood on the next lesson's
// practice line all the same.
func TestTutorialStandsACursorThatCameBack(t *testing.T) {
	a := newTutorial(t, 100, 30)
	w := a.World()
	var first, second core.Entity
	w.RunSafe(func() { first = w.Resources.Player.Entity })
	a.Context().PushEventOrigin(event.EventCursorDespawnRequest, &event.CursorDespawnRequestPayload{Slot: 0}, event.OriginDebug)
	a.Settle()
	a.Context().PushEventOrigin(event.EventCursorSpawnRequest,
		&event.CursorSpawnRequestPayload{X: 40, Y: 20, Heat: 100, Energy: 100}, event.OriginDebug)
	a.Settle()
	w.RunSafe(func() { second = w.Resources.Player.Entity })
	if second == 0 || second == first {
		t.Fatalf("the cursor came back as entity %d, want a new one beside %d", second, first)
	}
	tutorialKeys(t, a, "s")
	if pos, _ := w.LocalCursor(); pos.X != 4 || pos.Y != 14 {
		t.Fatalf("s left the new cursor at (%d, %d), want it stood at (4, 14)", pos.X, pos.Y)
	}
}

// The change lesson goes on however its deletes were made: c leaves insert mode
// and d stays in normal mode, where esc would open the menu, so no step waits for
// an esc that d never needs, and the next lesson asks for normal mode.
func TestTutorialsChangeLessonTakesADeleteToo(t *testing.T) {
	for _, keys := range []string{"cw\x1bjcc", "dwjdd", "cw\x1bjdd", "dejcc"} {
		a := newTutorial(t, 100, 30)
		tutorialTo(t, a, "Change")
		if tutorialKeys(t, a, keys); tutorialState(a) != "Replace" || !a.Context().IsNormalMode() {
			t.Errorf("%q in the change lesson left the tutorial in %s, Normal %v, want Replace in Normal",
				keys, tutorialState(a), a.Context().IsNormalMode())
		}
	}
}

// Every lesson opens in normal mode, whatever mode the key that ended the last one
// left: s pressed in Visual skips to the next lesson in Normal, and Free starts there.
func TestTutorialsLessonsOpenInNormalMode(t *testing.T) {
	a := newTutorial(t, 100, 30)
	if tutorialKeys(t, a, "vs"); tutorialState(a) != "Words" || !a.Context().IsNormalMode() {
		t.Fatalf("v then s left the tutorial in %s, Normal %v, want Words in Normal", tutorialState(a), a.Context().IsNormalMode())
	}
	tutorialTo(t, a, "Keys")
	if tutorialKeys(t, a, "vs"); tutorialState(a) != "Free" || !a.Context().IsNormalMode() {
		t.Fatalf("v then s in the last lesson left the tutorial in %s, Normal %v, want Free in Normal", tutorialState(a), a.Context().IsNormalMode())
	}
}

// Marks and Map end on the map's bottom row as it stands, read each tick, so G still
// ends them after a window is resized or a phone turned in the lesson.
func TestTutorialsBottomRowLessonsFollowAResize(t *testing.T) {
	for _, c := range []struct {
		lesson, keys, want string
	}{{"Marks", "maG", "MarksBack"}, {"Map", "G", "MapTop"}} {
		for _, size := range [][2]int{{100, 40}, {100, 27}, {60, 20}} {
			a := newTutorial(t, 100, 30)
			tutorialTo(t, a, c.lesson)
			a.Resize(size[0], size[1])
			a.Tick(3)
			if tutorialKeys(t, a, c.keys); tutorialState(a) != c.want {
				t.Errorf("%q in %s after a resize to %dx%d left the tutorial in %s, want %s",
					c.keys, c.lesson, size[0], size[1], tutorialState(a), c.want)
			}
		}
	}
}

// The shield that gives Visual its reach drains while it stands, so the lesson sets
// energy enough that it still stands for a learner who takes their time.
func TestTutorialsVisualShieldOutlastsASlowLearner(t *testing.T) {
	a := newTutorial(t, 100, 30)
	w := a.World()
	tutorialTo(t, a, "Visual")
	a.Tick(3000)
	var shield component.ShieldComponent
	w.RunSafe(func() { shield, _ = w.Components.Shield.GetComponent(w.Resources.Player.Entity) })
	if state := tutorialState(a); state != "Visual" || !shield.Active {
		t.Fatalf("a while into the lesson the tutorial stands in %s with the shield up %v, want Visual with it up", state, shield.Active)
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
	host, joiner := newTutorial(t, 100, 30), newTutorial(t, 100, 30)
	tutorialKeys(t, host, "s")
	if got := tutorialRow(joiner, 16); got != "and right to the x" {
		t.Fatalf("the first lesson laid %q on row 16, want its last practice line", got)
	}
	tutorialInstall(t, host, joiner)
	if got := tutorialRow(joiner, 16); got != "" {
		t.Fatalf("the installed lesson left %q on row 16, want the first lesson's block gone", got)
	}
	if got, want := tutorialRow(joiner, 14), tutorialRow(host, 14); got != want || got == "" {
		t.Fatalf("the installed lesson reads %q on row 14, want the second lesson's %q", got, want)
	}
}

// A participant that installs Visual or Free runs what that lesson runs, as a
// reload into either does: the shield, whose reach Visual's block shows, and the
// glyph spawner, which lays Free's code. An install re-derives a region's systems.
func TestTutorialAnInstalledLessonRunsItsSystems(t *testing.T) {
	runs := func(a *App, lesson string) (on bool) {
		w := a.World()
		w.RunSafe(func() {
			if lesson == "Free" {
				on = w.Resources.Status.Bools.Get("glyph.enabled").Load()
				return
			}
			shield, _ := w.Components.Shield.GetComponent(w.Resources.Player.Entity)
			on = shield.Active
		})
		return on
	}
	for _, lesson := range []string{"Visual", "Free"} {
		host, joiner := newTutorial(t, 100, 30), newTutorial(t, 100, 30)
		if tutorialTo(t, host, lesson); !runs(host, lesson) {
			t.Fatalf("the host in %s runs none of what the lesson turns on", lesson)
		}
		tutorialInstall(t, host, joiner)
		if joiner.Tick(5); !runs(joiner, lesson) {
			t.Errorf("a participant that installed %s runs none of what the lesson turns on", lesson)
		}
	}
}

// The first lesson's x ends its third line as a word does, so $, w and e reach it
// as they reach any word's glyph, and h j k l still do.
func TestTutorialsFirstMarkIsAWordsGlyph(t *testing.T) {
	a := newTutorial(t, 100, 30)
	for _, keys := range []string{"jj$", "jj4w", "jj5e", "jj" + strings.Repeat("l", 17)} {
		if tutorialKeys(t, a, keys); tutorialState(a) != "Words" {
			t.Fatalf("%q left the tutorial in %s, want Words", keys, tutorialState(a))
		}
		if tutorialKeys(t, a, "S"); tutorialState(a) != "Move" {
			t.Fatalf("S from Words left the tutorial in %s, want Move", tutorialState(a))
		}
	}
}

// A count before s or S is dropped, so 2S goes back one lesson and empties no
// practice line as vi's S would, and the s after q names a macro, skipping nothing.
func TestTutorialsSkipKeysDropACountAndLeaveQItsLabel(t *testing.T) {
	a := newTutorial(t, 100, 30)
	glyphs := func() (n int) {
		a.World().RunSafe(func() { n = a.World().Components.Glyph.CountEntities() })
		return n
	}
	if tutorialKeys(t, a, "qs"); tutorialState(a) != "Move" || !a.Context().MacroRecording.Load() {
		t.Fatalf("qs left the tutorial in %s, recording %v, want Move recording the macro s",
			tutorialState(a), a.Context().MacroRecording.Load())
	}
	if tutorialKeys(t, a, "q2s"); tutorialState(a) != "Words" {
		t.Fatalf("2s left the tutorial in %s, want Words", tutorialState(a))
	}
	laid := glyphs()
	if tutorialKeys(t, a, "2s2S"); tutorialState(a) != "Words" || glyphs() != laid || !a.Context().IsNormalMode() {
		t.Fatalf("2s then 2S left the tutorial in %s with %d glyphs, Normal %v, want Words with its %d laid, in Normal",
			tutorialState(a), glyphs(), a.Context().IsNormalMode(), laid)
	}
}

// The tutorial seats one player, so a bot asked for is refused before any session
// opens for it, saying why, and the run plays on alone.
func TestTutorialSeatsNoBot(t *testing.T) {
	a := newTutorial(t, 100, 30)
	injectExCommand(t, a, "bot")
	a.Tick(2)
	if got := a.Context().GetStatusMessage(); !strings.Contains(got, "seats one player, so no bot joins it") {
		t.Fatalf(":bot in the tutorial reports %q", got)
	}
	if a.membership.SessionTransport() != nil {
		t.Fatal(":bot opened a session for a bot the tutorial has no seat for")
	}
	injectExCommand(t, a, "bot list")
	a.Tick(2)
	if got := a.Context().GetStatusMessage(); !strings.Contains(got, "No bots: scenario tutorial seats one player") {
		t.Fatalf(":bot list in the tutorial reports %q, want why it seats none", got)
	}
}

// A lesson's prompt is a progress line, up till the next lesson's rather than for a
// notice's five seconds, and the lesson a step reaches posts its own the same way.
func TestTutorialsPromptsStayUp(t *testing.T) {
	a := newTutorial(t, 100, 30)
	shown := func() (string, parameter.StatusKind) {
		return a.Context().ShownStatus(a.Context().TimeCtl.RealTime().Add(time.Minute).UnixNano())
	}
	if text, kind := shown(); kind != parameter.StatusProgress || !strings.HasPrefix(text, "Lesson 1 of 22") {
		t.Fatalf("a minute on the bar shows %q, a %s line; want lesson 1's prompt", text, parameter.StatusKindNames[kind])
	}
	if tutorialKeys(t, a, "s"); tutorialState(a) != "Words" {
		t.Fatalf("s left the tutorial in %s", tutorialState(a))
	}
	if text, kind := shown(); kind != parameter.StatusProgress || !strings.HasPrefix(text, "Lesson 2 of 22") {
		t.Fatalf("a minute into Words the bar shows %q, a %s line; want lesson 2's prompt", text, parameter.StatusKindNames[kind])
	}
}

// tutorialServer is a dedicated host of the tutorial serving its lobby, one place
// its only guest's, or skips where the scenario is not present
func tutorialServer(t *testing.T) (*App, string) {
	t.Helper()
	opts := resource.Options{Dir: soakRoot, Scenario: "tutorial"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("tutorial scenario not present")
	}
	a, addr, _ := lobbyHost(t, Config{Mode: ModeServer, Membership: Membership{Participants: 1},
		Width: 100, Height: 30, Resources: opts, Seed: fixtureSeed})
	return a, addr
}

// A bot's own run that dials a server of the tutorial is refused at the door, as a
// seat is where it is added, and leaves the place to the person who dials next; a
// dial past that person is told the scenario seats one player.
func TestTutorialsDoorTakesNoBotsOwnRun(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	a, addr := tutorialServer(t)
	dial, offered := seatedDial(t, addr)
	if err := dial.Complete(nil, network.JoinerReport{Identity: offered.Identity, Bot: true}); err != nil {
		t.Fatalf("a bot's own run's reply: %v", err)
	}
	if _, err := dial.WaitStart(); err == nil || !strings.Contains(err.Error(), "seats one player, so no bot joins it") {
		t.Fatalf("a bot's own run joining the tutorial's server reached its start gate with %v", err)
	}
	waitUntil(t, func() bool { return a.membership.GuestCount() == 0 }, "the refused bot's place released")
	seatedDial(t, addr)
	_, _, err := network.DialSession(addr, network.DebugConfig(link.RolePeer, ""))
	if !errors.Is(err, &network.Refusal{Code: network.RefusalFull}) || !strings.Contains(err.Error(), "scenario tutorial seats one player") {
		t.Fatalf("a dial past the tutorial's one player got %v, want a full refusal naming its one seat", err)
	}
}

// A scripted host of the tutorial, whose own cursor fills its one seat, refuses a
// lobby that would wait for a guest forever.
func TestTutorialsScriptedHostRefusesALobbyThatNeverStarts(t *testing.T) {
	opts := resource.Options{Dir: soakRoot, Scenario: "tutorial"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("tutorial scenario not present")
	}
	_, _, gate := lobbyHost(t, Config{Mode: ModeHeadless, Width: 100, Height: 30, Resources: opts, Seed: fixtureSeed})
	select {
	case err := <-gate:
		if err == nil || !strings.Contains(err.Error(), "never starts") {
			t.Fatalf("the tutorial's scripted host opened its lobby with %v", err)
		}
	case <-time.After(socketWait):
		t.Fatal("the tutorial's scripted host waits for a guest its one seat has no place for")
	}
}
