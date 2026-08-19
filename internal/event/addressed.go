package event

import "github.com/lixenwraith/vi-fighter/internal/core"

// Addressed is implemented by command payloads naming the cursor to act on.
// A zero entity selects the local cursor: interim glue for producers not yet
// migrated, to be replaced by a rejection once every producer stamps one.
// Notification payloads deliberately do not implement it — they name a cursor
// that already exists, and resolving one against "local" would be a silent lie.
type Addressed interface {
	Target() core.Entity
}

// --- Cursor addressing ---
// Target reports the cursor a command addresses; zero selects the local cursor

func (p *CursorMoveRequestPayload) Target() core.Entity   { return p.Entity }
func (p *EnergyAddPayload) Target() core.Entity           { return p.Entity }
func (p *EnergySetPayload) Target() core.Entity           { return p.Entity }
func (p *EnergyGlyphConsumedPayload) Target() core.Entity { return p.Entity }
func (p *EnergyBlinkPayload) Target() core.Entity         { return p.Entity }
func (p *EnergyBlinkStopPayload) Target() core.Entity     { return p.Entity }
func (p *HeatAddRequestPayload) Target() core.Entity      { return p.Entity }
func (p *HeatSetRequestPayload) Target() core.Entity      { return p.Entity }
func (p *ShieldActivatePayload) Target() core.Entity      { return p.Entity }
func (p *ShieldDeactivatePayload) Target() core.Entity    { return p.Entity }
func (p *ShieldDrainRequestPayload) Target() core.Entity  { return p.Entity }
func (p *PingGridRequestPayload) Target() core.Entity     { return p.Entity }
func (p *CharacterTypedPayload) Target() core.Entity      { return p.Entity }
func (p *BoostActivatePayload) Target() core.Entity       { return p.Entity }
func (p *BoostExtendPayload) Target() core.Entity         { return p.Entity }
func (p *BoostDeactivatePayload) Target() core.Entity     { return p.Entity }
func (p *WeaponAddRequestPayload) Target() core.Entity    { return p.Entity }
func (p *WeaponFireRequestPayload) Target() core.Entity   { return p.Entity }
func (p *FireSpecialRequestPayload) Target() core.Entity  { return p.Entity }
