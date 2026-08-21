package renderer

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

func TestPinnedHUDOriginIsTopLeft(t *testing.T) {
	ctx := render.RenderContext{GameXOffset: 7, GameYOffset: 3, ViewportWidth: 100}
	x, y := pinnedHUDOrigin(ctx)
	if x != 7+parameter.HudMarginX || y != 3+parameter.HudMarginY {
		t.Fatalf("origin = (%d, %d), want viewport top-left margin", x, y)
	}
}

func TestPinnedHUDPackWrapsWholeGroupsAndReportsOverflow(t *testing.T) {
	r := &PinnedStatsRenderer{groups: []hudGroup{
		{name: "combat", count: 8},
		{name: "dust", count: 8},
		{name: "event", count: 8},
		{name: "small", count: 2},
	}}
	r.pack(10, 2)

	if len(r.columns) != 2 || r.columns[0].height != 10 || r.columns[1].height != 8 {
		t.Fatalf("columns = %+v, want later small group to use remaining space", r.columns)
	}
	if !slices.Equal(r.hidden, []string{"event"}) {
		t.Fatalf("hidden = %v, want [event]", r.hidden)
	}
	if r.groups[0].column != 0 || r.groups[1].column != 1 ||
		r.groups[2].column != -1 || r.groups[3].column != 0 {
		t.Fatalf("group columns = %d,%d,%d,%d",
			r.groups[0].column, r.groups[1].column, r.groups[2].column, r.groups[3].column)
	}
}

func TestPinnedHUDPackSkipsOversizedGroupWithoutBlankingHUD(t *testing.T) {
	r := &PinnedStatsRenderer{groups: []hudGroup{
		{name: "oversized", count: 12},
		{name: "fits", count: 5},
	}}
	r.pack(10, 1)

	if len(r.columns) != 1 || r.groups[1].column != 0 {
		t.Fatalf("later fitting group was not rendered: columns=%+v groups=%+v", r.columns, r.groups)
	}
	if !slices.Equal(r.hidden, []string{"oversized"}) {
		t.Fatalf("hidden = %v, want [oversized]", r.hidden)
	}
}

func TestPinnedHUDWidthUsesDelayedShrink(t *testing.T) {
	r := &PinnedStatsRenderer{}
	if got := r.stabilizeWidth(24); got != 24 {
		t.Fatalf("initial width = %d, want 24", got)
	}
	for i := 1; i < parameter.HudWidthShrinkFrames; i++ {
		if got := r.stabilizeWidth(12); got != 24 {
			t.Fatalf("width shrank on frame %d: %d", i, got)
		}
	}
	if got := r.stabilizeWidth(12); got != 12 {
		t.Fatalf("settled width = %d, want 12", got)
	}
	if got := r.stabilizeWidth(20); got != 20 {
		t.Fatalf("expansion was delayed: %d", got)
	}
}
