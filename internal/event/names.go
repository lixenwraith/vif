package event

import (
	"fmt"
	"slices"
	"sync"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
	"github.com/lixenwraith/vif/pkg/audio/model"
)

// PayloadNames are the names a payload's enum fields take, each type's from its
// own table. A species field takes a scenario's behavior programs too, which hold
// SpeciesDataFirst onward in sorted-name order.
func PayloadNames(programs []string) *dialect.Names {
	if len(programs) == 0 {
		return codeNames()
	}
	species := slices.Concat(component.SpeciesNames[:], slices.Sorted(slices.Values(programs)))
	return migrating(codeNames().Clone().Enum[component.SpeciesType](species...))
}

// AuthoredPayload makes event et's payload from raw, a table an author wrote, as
// names spell its enum fields; a nil raw is the zero payload, and an event that
// carries no payload takes none
func AuthoredPayload(names *dialect.Names, et EventType, raw any) (any, error) {
	payload := NewPayloadStruct(et)
	switch {
	case payload == nil && raw != nil:
		return nil, fmt.Errorf("event '%s' carries no payload", GetEventName(et))
	case payload == nil:
		return nil, nil
	}
	if err := names.DecodeInto(raw, payload); err != nil {
		return nil, fmt.Errorf("payload for event '%s': %w", GetEventName(et), err)
	}
	return payload, nil
}

// codeNames are the names the code declares, which every scenario shares
var codeNames = sync.OnceValue(func() *dialect.Names {
	EnsureRegistry()
	events := make([]string, EventTypeCount)
	for et := range events {
		events[et] = GetEventName(EventType(et))
	}
	return migrating(dialect.NewNames().
		Enum[component.SpeciesType](component.SpeciesNames[:]...).
		Variant[component.SubType](component.SpeciesEye, component.EyeTypeNames[:]...).
		Variant[component.SubType](component.SpeciesTower, component.TowerTypeNames[:]...).
		Enum[component.LootType](namesOf(component.LootSpecs[:], func(s component.LootSpec) string { return s.Name })...).
		Enum[component.EyeType](component.EyeTypeNames[:]...).
		Enum[component.TowerType](component.TowerTypeNames[:]...).
		Enum[component.TargetType](component.TargetTypeNames[:]...).
		Enum[component.ParticleBehavior](component.ParticleBehaviorNames[:]...).
		Enum[component.EnergyDeltaType](component.EnergyDeltaNames[:]...).
		Enum[component.GlyphType](component.GlyphTypeNames[:]...).
		Enum[component.GlyphLevel](component.GlyphLevelNames[:]...).
		Enum[component.LightningColorType](component.LightningColorNames[:]...).
		Enum[component.MarkerShape](component.MarkerShapeNames[:]...).
		Enum[component.MarkerFade](component.MarkerFadeNames[:]...).
		Enum[component.SpawnType](namesOf(component.MaterializeStyles[:], func(s component.MaterializeStyle) string { return s.Name })...).
		Enum[component.SpiritColor](component.SpiritColorNames[:]...).
		Mask[component.TracerDirection](component.TracerDirectionNames).
		Enum[component.WeaponType](namesOf(component.WeaponSpecs[:], func(s component.WeaponSpec) string { return s.Name })...).
		Enum[component.WeaponPalette](component.WeaponPaletteNames[:]...).
		Mask[component.WallBlockMask](component.WallBlockNames).
		Mask[terminal.Attr](component.WallAttrNames).
		Enum[component.BoxDrawStyle](component.BoxDrawStyleNames[:]...).
		Enum[component.Behavior](component.BehaviorNames[:]...).
		Enum[component.CombatAttackType](component.CombatAttackNames[:]...).
		Enum[core.GameMode](core.ModeNames[:]...).
		Enum[SoundMuteToggleMode](muteToggleModeNames[:]...).
		Enum[InputSource](inputSourceNames[:]...).
		Enum[DeleteRangeType](deleteRangeNames[:]...).
		Enum[ExplosionType](explosionTypeNames[:]...).
		Enum[FuseEffect](fuseEffectNames[:]...).
		Enum[WallBatchCollisionMode](collisionModeNames[:]...).
		Enum[EventType](events...).
		Enum[model.InstrumentType](stringsOf(model.InstrumentCount)...).
		Enum[model.Intensity](stringsOf(model.IntensityCount)...))
})

// migrating lets the types shipped documents still give by number take numbers,
// until each type's documents name them
func migrating(n *dialect.Names) *dialect.Names {
	return n.
		Numbers[component.TargetType]().
		Numbers[component.ParticleBehavior]().
		Numbers[component.EnergyDeltaType]().
		Numbers[component.WallBlockMask]().
		Numbers[component.BoxDrawStyle]()
}

// namesOf lists the name each row of a kind's table gives it
func namesOf[S any](rows []S, name func(S) string) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = name(row)
	}
	return out
}

// stringsOf lists the names an enum's String gives its values below count
func stringsOf[E interface {
	~int32
	fmt.Stringer
}](count E) []string {
	out := make([]string, count)
	for v := range count {
		out[v] = v.String()
	}
	return out
}
