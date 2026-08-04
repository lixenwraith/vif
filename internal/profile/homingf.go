package profile

import (
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// DrainHomingF: no arrival steering, default settling
var DrainHomingF = physics.HomingProfileF{
	BaseSpeed:   parameter.DrainBaseSpeedFloat,
	HomingAccel: parameter.DrainHomingAccelFloat,
	Drag:        parameter.DrainDragFloat,
}

// SwarmHomingF: chase at 4x drain speed, no arrival steering
var SwarmHomingF = physics.HomingProfileF{
	BaseSpeed:   parameter.DrainBaseSpeedFloat * parameter.SwarmChaseSpeedMultiplier,
	HomingAccel: parameter.SwarmHomingAccelFloat,
	Drag:        parameter.SwarmDragFloat,
}

// QuasarHomingF: arrival steering with 4x drag at target
var QuasarHomingF = physics.HomingProfileF{
	BaseSpeed:        parameter.QuasarBaseSpeedFloat,
	HomingAccel:      parameter.QuasarHomingAccelFloat,
	Drag:             parameter.QuasarDragFloat,
	ArrivalRadius:    3.0,
	ArrivalDragBoost: 3.0,
	DeadZone:         0.5,
}

// SnakeHomingF: arrival steering, 3x drag at target
var SnakeHomingF = physics.HomingProfileF{
	BaseSpeed:        parameter.SnakeBaseSpeedFloat,
	HomingAccel:      parameter.SnakeHomingAccelFloat,
	Drag:             parameter.SnakeDragFloat,
	ArrivalRadius:    2.0,
	ArrivalDragBoost: 2.0,
	DeadZone:         0.5,
}

// LootHomingF: aggressive arrival drag kills orbital momentum for reliable capture
var LootHomingF = physics.HomingProfileF{
	BaseSpeed:        parameter.LootHomingMaxSpeedFloat,
	HomingAccel:      parameter.LootHomingAccelFloat,
	Drag:             2.0,
	ArrivalRadius:    5.0,
	ArrivalDragBoost: 25.0,
	DeadZone:         0.5,
}

// MissileHomingF: BaseSpeed 0 makes drag continuous; full accel through arrival
var MissileHomingF = physics.HomingProfileF{
	HomingAccel:      parameter.MissileHomingAccelFloat,
	Drag:             parameter.MissileDragFloat,
	ArrivalRadius:    parameter.MissileArrivalRadiusFloat,
	ArrivalDragBoost: 2.0,
	ArrivalAccelMin:  1.0,
	DeadZone:         0.1,
}

// EyeHomingProfilesF mirrors EyeHomingProfiles from the same source table
var EyeHomingProfilesF [parameter.EyeTypeCount]physics.HomingProfileF

func init() {
	for i := range parameter.EyeTypeCount {
		p := &parameter.EyeTypeTable[i]
		EyeHomingProfilesF[i] = physics.HomingProfileF{
			BaseSpeed:   p.BaseSpeed,
			HomingAccel: p.HomingAccel,
			Drag:        p.Drag,
		}
	}
}
