package snapshot

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
)

// TestCombatDigestCoversStatusSlots: a status a hit left on a shared combatant is
// compared state, so two instances disagreeing on one disagree on the digest.
func TestCombatDigestCoversStatusSlots(t *testing.T) {
	w := engine.NewWorld()
	e := w.CreateEntity(core.DomainShared)
	w.Components.Combat.SetComponent(e, component.CombatComponent{HitPoints: 3})
	before := DigestWorld(w, engine.ScopeShared).Combat

	c, _ := w.Components.Combat.GetPtr(e)
	c.Status[component.StatusStun].Remaining = time.Second
	if DigestWorld(w, engine.ScopeShared).Combat == before {
		t.Fatal("a stun did not move the combat digest")
	}
}
