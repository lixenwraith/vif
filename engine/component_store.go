package engine

import (
	"github.com/lixenwraith/vi-fighter/component"
)

// ComponentStore provides cached pointer to typed component store
// Initialized once per system to eliminate runtime map lookup
type ComponentStore struct {
	// Core gameplay
	Glyph      *Store[component.GlyphComponent]
	Sigil      *Store[component.SigilComponent]
	Nugget     *Store[component.NuggetComponent]
	Cursor     *Store[component.CursorComponent]
	Protection *Store[component.ProtectionComponent]

	// Player state
	Energy *Store[component.EnergyComponent]
	Heat   *Store[component.HeatComponent]
	Shield *Store[component.ShieldComponent]
	Boost  *Store[component.BoostComponent]

	// Entity
	Drain       *Store[component.DrainComponent]
	Decay       *Store[component.DecayComponent]
	Cleaner     *Store[component.CleanerComponent]
	Blossom     *Store[component.BlossomComponent]
	Quasar      *Store[component.QuasarComponent]
	Dust        *Store[component.DustComponent]
	Lightning   *Store[component.LightningComponent]
	Spirit      *Store[component.SpiritComponent]
	Materialize *Store[component.MaterializeComponent]

	// Composite
	Header *Store[component.CompositeHeaderComponent]
	Member *Store[component.MemberComponent]

	// Effect
	Flash  *Store[component.FlashComponent]
	Splash *Store[component.SplashComponent]
	Ping   *Store[component.PingComponent]

	// Lifecycle
	Death *Store[component.DeathComponent]
	Timer *Store[component.TimerComponent]
}

// GetComponentStore populates ComponentStore from world
// Call once during system construction; pointer remain valid for application lifetime
func GetComponentStore(w *World) ComponentStore {
	return ComponentStore{
		Glyph:      GetStore[component.GlyphComponent](w),
		Sigil:      GetStore[component.SigilComponent](w),
		Nugget:     GetStore[component.NuggetComponent](w),
		Cursor:     GetStore[component.CursorComponent](w),
		Protection: GetStore[component.ProtectionComponent](w),

		Energy: GetStore[component.EnergyComponent](w),
		Heat:   GetStore[component.HeatComponent](w),
		Shield: GetStore[component.ShieldComponent](w),
		Boost:  GetStore[component.BoostComponent](w),

		Drain:       GetStore[component.DrainComponent](w),
		Decay:       GetStore[component.DecayComponent](w),
		Cleaner:     GetStore[component.CleanerComponent](w),
		Blossom:     GetStore[component.BlossomComponent](w),
		Quasar:      GetStore[component.QuasarComponent](w),
		Dust:        GetStore[component.DustComponent](w),
		Lightning:   GetStore[component.LightningComponent](w),
		Spirit:      GetStore[component.SpiritComponent](w),
		Materialize: GetStore[component.MaterializeComponent](w),

		Header: GetStore[component.CompositeHeaderComponent](w),
		Member: GetStore[component.MemberComponent](w),

		Flash:  GetStore[component.FlashComponent](w),
		Splash: GetStore[component.SplashComponent](w),
		Ping:   GetStore[component.PingComponent](w),

		Death: GetStore[component.DeathComponent](w),
		Timer: GetStore[component.TimerComponent](w),
	}
}