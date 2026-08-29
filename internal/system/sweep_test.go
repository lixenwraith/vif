package system

import (
	"sync/atomic"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
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
