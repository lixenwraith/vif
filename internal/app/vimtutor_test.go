package app

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/resource"
)

// A tutor lesson clears itself as it exits and the next lays its own text, with
// the cursor stood on the practice line's first glyph: the first lesson's x and
// notes are gone once the cursor reaches the x, and the second's stand.
func TestVimtutorALessonClearsItselfAndTheNextLaysItsOwn(t *testing.T) {
	opts := resource.Options{Dir: soakRoot, Scenario: "vimtutor"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("vimtutor scenario not present")
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
