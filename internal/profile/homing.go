package profile

import (
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// DrainHoming: no arrival steering, default settling
var DrainHoming = physics.HomingProfile{
	BaseSpeed:   parameter.DrainBaseSpeed,
	HomingAccel: parameter.DrainHomingAccel,
	Drag:        parameter.DrainDrag,
}

// SwarmHoming: chase at 4x drain speed, no arrival steering
var SwarmHoming = physics.HomingProfile{
	BaseSpeed:   parameter.DrainBaseSpeed * parameter.SwarmChaseSpeedMultiplier,
	HomingAccel: parameter.SwarmHomingAccel,
	Drag:        parameter.SwarmDrag,
}

// QuasarHoming: arrival steering with 4x drag at target
var QuasarHoming = physics.HomingProfile{
	BaseSpeed:        parameter.QuasarBaseSpeed,
	HomingAccel:      parameter.QuasarHomingAccel,
	Drag:             parameter.QuasarDrag,
	ArrivalRadius:    parameter.QuasarArrivalRadius,
	ArrivalDragBoost: parameter.QuasarArrivalDragBoost,
	DeadZone:         parameter.QuasarDeadZone,
}

// SnakeHoming: arrival steering, 3x drag at target
var SnakeHoming = physics.HomingProfile{
	BaseSpeed:        parameter.SnakeBaseSpeed,
	HomingAccel:      parameter.SnakeHomingAccel,
	Drag:             parameter.SnakeDrag,
	ArrivalRadius:    parameter.SnakeArrivalRadius,
	ArrivalDragBoost: parameter.SnakeArrivalDragBoost,
	DeadZone:         parameter.SnakeDeadZone,
}

// Homing names the species profiles a behavior program's prototype may steer by
var Homing = map[string]*physics.HomingProfile{
	"drain":  &DrainHoming,
	"quasar": &QuasarHoming,
	"snake":  &SnakeHoming,
	"swarm":  &SwarmHoming,
}

// LootHoming: aggressive arrival drag kills orbital momentum for reliable capture.
// LootSystem applies the cornering brake beside it, as drains, swarms, quasars and eyes do.
var LootHoming = physics.HomingProfile{
	BaseSpeed:        parameter.LootHomingMaxSpeed,
	HomingAccel:      parameter.LootHomingAccel,
	Drag:             parameter.LootHomingDrag,
	ArrivalRadius:    parameter.LootHomingArrivalRadius,
	ArrivalDragBoost: parameter.LootHomingArrivalDrag,
	DeadZone:         parameter.LootHomingDeadZone,
}

// MissileHoming: BaseSpeed 0 makes drag continuous; full accel through arrival
var MissileHoming = physics.HomingProfile{
	HomingAccel:      parameter.MissileHomingAccel,
	Drag:             parameter.MissileDrag,
	ArrivalRadius:    parameter.MissileArrivalRadius,
	ArrivalDragBoost: parameter.MissileArrivalDragBoost,
	ArrivalAccelMin:  parameter.MissileArrivalAccelMin,
	DeadZone:         parameter.MissileDeadZone,
}

// EyeHomingProfiles mirrors EyeHomingProfiles from the same source table
var EyeHomingProfiles [parameter.EyeTypeCount]physics.HomingProfile

func init() {
	for i := range parameter.EyeTypeCount {
		p := &parameter.EyeTypeTable[i]
		EyeHomingProfiles[i] = physics.HomingProfile{
			BaseSpeed:   p.BaseSpeed,
			HomingAccel: p.HomingAccel,
			Drag:        p.Drag,
		}
	}
}
