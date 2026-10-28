package component

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// LootType names one loot kind: the code's, which LootSpecs holds, then those a
// scenario declares (LootSet.Kinds) from LootCount in sorted-name order
type LootType uint8

const (
	LootRod LootType = iota
	LootLauncher
	LootDisruptor
	LootHeat
	LootEnergy
	LootTurret
	LootEmitter
	LootGun
	LootCount // the code's kinds; a scenario's follow
)

// LootMax bounds a session's kinds, so every table indexed by LootType holds LootMax rows
const LootMax = LootCount + parameter.LootKindsMax

// RewardType is what collecting a loot kind changes
type RewardType uint8

const (
	RewardNone RewardType = iota
	RewardWeapon
	RewardHeat
	RewardEnergy
)

// RewardNames are the rewards a scenario's kind may grant, none naming RewardNone
var RewardNames = [...]string{RewardNone: "", RewardWeapon: "weapon", RewardHeat: "heat", RewardEnergy: "energy"}

// LootSpec is one loot kind: the name a document calls it by, and its reward, a
// charge of Weapon, or Delta heat or energy
type LootSpec struct {
	Name   string
	Reward RewardType
	Weapon WeaponType
	Delta  int
}

// LootSpecs is indexed by LootType
var LootSpecs = [LootCount]LootSpec{
	LootRod:       {Name: "rod", Reward: RewardWeapon, Weapon: WeaponRod},
	LootLauncher:  {Name: "launcher", Reward: RewardWeapon, Weapon: WeaponLauncher},
	LootDisruptor: {Name: "disruptor", Reward: RewardWeapon, Weapon: WeaponDisruptor},
	LootHeat:      {Name: "heat", Reward: RewardHeat, Delta: parameter.LootHeatRewardValue},
	LootEnergy:    {Name: "energy", Reward: RewardEnergy, Delta: parameter.LootEnergyRewardValue},
	LootTurret:    {Name: "turret", Reward: RewardWeapon, Weapon: WeaponTurret},
	LootEmitter:   {Name: "emitter", Reward: RewardWeapon, Weapon: WeaponEmitter},
	LootGun:       {Name: "gun", Reward: RewardWeapon, Weapon: WeaponGun},
}

// LootLook is how a drop shows: its rune in Color within a shield glowing Glow
type LootLook struct {
	Rune  rune
	Color color.RGB
	Glow  color.RGB
}

// LootKind is a loot kind whole: what collecting it grants and how a drop of it looks
type LootKind struct {
	LootSpec
	Look LootLook
}

// DropEntry is one loot kind a tier can roll
type DropEntry struct {
	Loot          LootType
	BaseRate      float64
	Count         int // items dropped on success (0 treated as 1)
	FallbackCount int // added to every later non-unique tier when this unique entry is skipped
}

// DropTier is one roll among its entries. A unique tier skips the weapons a cursor
// holds at full charge and any kind it already has in flight.
type DropTier struct {
	Entries []DropEntry
	Unique  bool
}

// DropKiller is whose kill rolls a drop table
type DropKiller uint8

const (
	DropKillerAny    DropKiller = iota // any kill, a player's or none's
	DropKillerPlayer                   // a kill a cursor dealt, KillerEntity set
)

// DropKillerNames are a table's killer keys, DropKillerAny's also the default
var DropKillerNames = [...]string{DropKillerAny: "any", DropKillerPlayer: "player"}

// DropTable is what a kill rolls: its tiers in order, when its killer qualifies
type DropTable struct {
	Tiers  []DropTier
	Killer DropKiller
}

// LootSet is the loot a scenario declares: its kinds, LootCount onward in
// sorted-name order, and its drop tables by name, one named for a code species
// replacing that species' table
type LootSet struct {
	Kinds  []LootKind
	Tables map[string]DropTable
}

// Names lists the set's kinds in LootType order
func (s *LootSet) Names() []string {
	names := make([]string, len(s.Kinds))
	for i := range s.Kinds {
		names[i] = s.Kinds[i].Name
	}
	return names
}

// LootComponent represents a collectible loot drop entity
type LootComponent struct {
	Type LootType
	Look LootLook // its kind's, carried so a drop draws from itself alone

	// Owner is the cursor this drop belongs to; loot is personal
	Owner core.Entity

	// Grid tracking
	LastIntX int
	LastIntY int
}
