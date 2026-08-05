package profile

import (
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// DrainHoming: no arrival steering, default settling
var DrainHoming = physics.HomingProfile{
	BaseSpeed:   parameter.DrainBaseSpeedFloat,
	HomingAccel: parameter.DrainHomingAccelFloat,
	Drag:        parameter.DrainDragFloat,
}

// SwarmHoming: chase at 4x drain speed, no arrival steering
var SwarmHoming = physics.HomingProfile{
	BaseSpeed:   parameter.DrainBaseSpeedFloat * parameter.SwarmChaseSpeedMultiplier,
	HomingAccel: parameter.SwarmHomingAccelFloat,
	Drag:        parameter.SwarmDragFloat,
}

// QuasarHoming: arrival steering with 4x drag at target
var QuasarHoming = physics.HomingProfile{
	BaseSpeed:        parameter.QuasarBaseSpeedFloat,
	HomingAccel:      parameter.QuasarHomingAccelFloat,
	Drag:             parameter.QuasarDragFloat,
	ArrivalRadius:    3.0,
	ArrivalDragBoost: 3.0,
	DeadZone:         0.5,
}

// SnakeHoming: arrival steering, 3x drag at target
var SnakeHoming = physics.HomingProfile{
	BaseSpeed:        parameter.SnakeBaseSpeedFloat,
	HomingAccel:      parameter.SnakeHomingAccelFloat,
	Drag:             parameter.SnakeDragFloat,
	ArrivalRadius:    2.0,
	ArrivalDragBoost: 2.0,
	DeadZone:         0.5,
}

// LootHomingF: aggressive arrival drag kills orbital momentum for reliable capture
var LootHoming = physics.HomingProfile{
	BaseSpeed:        parameter.LootHomingMaxSpeedFloat,
	HomingAccel:      parameter.LootHomingAccelFloat,
	Drag:             2.0,
	ArrivalRadius:    5.0,
	ArrivalDragBoost: 25.0,
	DeadZone:         0.5,
}

// MissileHoming: BaseSpeed 0 makes drag continuous; full accel through arrival
var MissileHoming = physics.HomingProfile{
	HomingAccel:      parameter.MissileHomingAccelFloat,
	Drag:             parameter.MissileDragFloat,
	ArrivalRadius:    parameter.MissileArrivalRadiusFloat,
	ArrivalDragBoost: 2.0,
	ArrivalAccelMin:  1.0,
	DeadZone:         0.1,
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
