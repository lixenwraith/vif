package profile

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
)

func TestMatrixCoversCursorAttacks(t *testing.T) {
	families := []component.CombatAttackType{
		component.CombatAttackProjectile,
		component.CombatAttackShield,
		component.CombatAttackLightning,
		component.CombatAttackExplosion,
		component.CombatAttackMissile,
		component.CombatAttackPulse,
	}
	for _, atk := range families {
		for _, d := range cursorDefenders {
			if Attack(atk, component.CombatEntityCursor, d) == nil {
				t.Errorf("missing profile: attack %d vs defender %d", atk, d)
			}
		}
	}
	for _, d := range eyeTargets {
		if Attack(component.CombatAttackSelfDestruct, component.CombatEntityEye, d) == nil {
			t.Errorf("missing self-destruct profile vs defender %d", d)
		}
	}
}

func TestAttackBoundsSafe(t *testing.T) {
	if Attack(component.CombatAttackTypeCount, component.CombatEntityCursor, component.CombatEntityDrain) != nil {
		t.Error("out-of-range attack type returned a profile")
	}
	if Attack(-1, component.CombatEntityCursor, component.CombatEntityDrain) != nil {
		t.Error("negative attack type returned a profile")
	}
	if Attack(component.CombatAttackShield, component.CombatEntityCount, component.CombatEntityDrain) != nil {
		t.Error("out-of-range attacker returned a profile")
	}
	if Attack(component.CombatAttackShield, component.CombatEntityCursor, component.CombatEntityCount) != nil {
		t.Error("out-of-range defender returned a profile")
	}
}

// eachProfile visits every registered profile exactly once
func eachProfile(fn func(*AttackProfile)) {
	seen := make(map[*AttackProfile]bool)
	for atk := range attackMatrix {
		for a := range attackMatrix[atk] {
			for d := range attackMatrix[atk][a] {
				p := attackMatrix[atk][a][d]
				if p == nil || seen[p] {
					continue
				}
				seen[p] = true
				fn(p)
			}
		}
	}
}

func TestKineticEffectImpliesCollision(t *testing.T) {
	eachProfile(func(p *AttackProfile) {
		hasKinetic := p.EffectMask&component.CombatEffectKinetic != 0
		if hasKinetic && p.Collision == nil {
			t.Errorf("attack %d vs %d: kinetic effect without collision profile", p.AttackType, p.Defender)
		}
		if hasKinetic && p.KineticImmunity <= 0 {
			t.Errorf("attack %d vs %d: kinetic effect without immunity window", p.AttackType, p.Defender)
		}
		if !hasKinetic && p.Collision != nil {
			t.Errorf("attack %d vs %d: unreachable collision profile", p.AttackType, p.Defender)
		}
	})
}

func TestMatrixSelfConsistent(t *testing.T) {
	for atk := range attackMatrix {
		for a := range attackMatrix[atk] {
			for d := range attackMatrix[atk][a] {
				p := attackMatrix[atk][a][d]
				if p == nil {
					continue
				}
				if int(p.AttackType) != atk || int(p.Attacker) != a || int(p.Defender) != d {
					t.Fatalf("slot [%d][%d][%d] holds profile [%d][%d][%d]",
						atk, a, d, p.AttackType, p.Attacker, p.Defender)
				}
			}
		}
	}
}

func TestChainsAreDirect(t *testing.T) {
	// Chains are re-emitted as EventCombatAttackDirectRequest, which rejects
	// anything that is not CombatDamageDirect
	eachProfile(func(p *AttackProfile) {
		if p.Chain != nil && p.Chain.DamageType != component.CombatDamageDirect {
			t.Errorf("attack %d vs %d: chain is not a direct attack", p.AttackType, p.Defender)
		}
	})
}

func TestChainTargetsSameDefender(t *testing.T) {
	eachProfile(func(p *AttackProfile) {
		if p.Chain != nil && p.Chain.Defender != p.Defender {
			t.Errorf("attack %d vs %d: chain targets defender %d", p.AttackType, p.Defender, p.Chain.Defender)
		}
	})
}
