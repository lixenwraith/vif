package system

import (
	"sync/atomic"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

func TestSpeciesProtectionTelemetrySeparatesVictimDomains(t *testing.T) {
	w := engine.NewWorld()
	shared := w.CreateEntity(core.DomainShared)
	player := w.CreateEntity(core.DomainPlayer)
	protection := component.ProtectionComponent{Mask: component.ProtectFromSpecies}
	w.Components.Protection.SetComponent(shared, protection)
	w.Components.Protection.SetComponent(player, protection)

	var sharedRejects atomic.Int64
	var playerRejects atomic.Int64
	if speciesClearable(w, shared, &sharedRejects, &playerRejects) {
		t.Fatal("shared protected entity was clearable")
	}
	if speciesClearable(w, player, &sharedRejects, &playerRejects) {
		t.Fatal("player protected entity was clearable")
	}
	if got := sharedRejects.Load(); got != 1 {
		t.Errorf("shared rejects = %d, want 1", got)
	}
	if got := playerRejects.Load(); got != 1 {
		t.Errorf("player rejects = %d, want 1", got)
	}
}

// A species body covering a gold member destroys that member alone: the player
// glyph and nugget sharing the cell, and the member's own body, survive.
func TestSpeciesBodyBreaksTheGoldItCovers(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	gold := w.CreateEntity(core.DomainShared)
	member := w.CreateEntity(core.DomainShared)
	w.Components.Glyph.SetComponent(member, component.GlyphComponent{Rune: 'A', Type: component.GlyphGold})
	w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: gold})
	w.Positions.SetPosition(member, component.PositionComponent{X: 10, Y: 5})
	glyph := w.CreateEntity(core.DomainPlayer)
	w.Components.Glyph.SetComponent(glyph, component.GlyphComponent{Rune: 'b', Type: component.GlyphBlue})
	w.Positions.SetPosition(glyph, component.PositionComponent{X: 10, Y: 5})
	nugget := w.CreateEntity(core.DomainPlayer)
	w.Components.Nugget.SetComponent(nugget, component.NuggetComponent{})
	w.Positions.SetPosition(nugget, component.PositionComponent{X: 10, Y: 5})

	species := w.CreateEntity(core.DomainShared)
	body := w.CreateEntity(core.DomainShared)
	w.Components.Member.SetComponent(body, component.MemberComponent{HeaderEntity: species})
	w.Positions.SetPosition(body, component.PositionComponent{X: 10, Y: 5})
	w.Components.Header.SetComponent(species, component.HeaderComponent{MemberEntries: []component.MemberEntry{{Entity: body}}})

	var sweep cellSweep
	breakGoldUnder(w, &sweep, species)
	var died []core.Entity
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.DeathRequestPayload); ok {
			if p.EffectEvent != event.EventFlashSpawnOneRequest {
				t.Fatalf("effect = %v, want the flash a shared body leaves", p.EffectEvent)
			}
			died = append(died, p.Entities...)
		}
	}
	if len(died) != 1 || died[0] != member {
		t.Fatalf("deaths = %v, want the gold member %d alone", died, member)
	}
}
