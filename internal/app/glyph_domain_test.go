package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// sharedGlyphs returns the shared-domain glyph entities and the ones that are not
// gold composite members
func sharedGlyphs(a *App) (count int, bad []string) {
	a.World().RunSafe(func() {
		w := a.World()
		w.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
			if e.Domain() != core.DomainShared {
				return true
			}
			count++
			if !w.Components.Member.HasEntity(e) || g.Type != component.GlyphGold {
				bad = append(bad, fmt.Sprintf("entity %d type %d", e.ID(), g.Type))
			}
			return true
		})
	})
	return count, bad
}

// TestSharedGlyphsAreGoldMembersOnly pins the one shared glyph population. Every
// other glyph is player-domain, which is what lets typing, cleaner and dust consume
// them without a crossing, and what keeps screen noise off the wire.
func TestSharedGlyphsAreGoldMembersOnly(t *testing.T) {
	a := mustHeadless(t, 0x901D, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	// Deterministic phase: force a gold sequence, so the invariant is not vacuous
	a.Context().PushEventOrigin(event.EventGoldSpawnRequest, nil, event.OriginDebug)
	a.Settle()
	a.Tick(2)

	count, bad := sharedGlyphs(a)
	if count != parameter.GoldSequenceLength {
		t.Fatalf("%d shared glyphs after a gold spawn, want %d", count, parameter.GoldSequenceLength)
	}
	if len(bad) > 0 {
		t.Fatalf("shared glyphs that are not gold members:\n  %s", strings.Join(bad, "\n  "))
	}

	// Soak phase: no other shared glyph population may appear
	if _, err := journal.RunFuzz(a, journal.DefaultFuzz(0x901D, 1200)); err != nil {
		t.Fatalf("soak: %v", err)
	}
	if _, bad = sharedGlyphs(a); len(bad) > 0 {
		t.Fatalf("shared glyphs that are not gold members:\n  %s", strings.Join(bad, "\n  "))
	}
}
