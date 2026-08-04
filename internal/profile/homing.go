package profile

import (
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// DrainHoming: no arrival steering, default settling
var DrainHoming = physics.HomingProfile{
	BaseSpeed:   parameter.DrainBaseSpeed,
	HomingAccel: parameter.DrainHomingAccel,
	Drag:        parameter.DrainDrag,
}

// SwarmHoming: chase at 4x drain speed, no arrival steering
var SwarmHoming = physics.HomingProfile{
	BaseSpeed:   parameter.SwarmChaseSpeed,
	HomingAccel: parameter.SwarmHomingAccel,
	Drag:        parameter.SwarmDrag,
}

// QuasarHoming: arrival steering with 4x drag at target
var QuasarHoming = physics.HomingProfile{
	BaseSpeed:        parameter.QuasarBaseSpeed,
	HomingAccel:      parameter.QuasarHomingAccel,
	Drag:             parameter.QuasarDrag,
	ArrivalRadius:    vmath.FromFloat(3.0),
	ArrivalDragBoost: vmath.FromFloat(3.0),
	DeadZone:         vmath.Scale / 2,
}

// SnakeHoming: arrival steering, 3x drag at target
var SnakeHoming = physics.HomingProfile{
	BaseSpeed:        parameter.SnakeBaseSpeed,
	HomingAccel:      parameter.SnakeHomingAccel,
	Drag:             parameter.SnakeDrag,
	ArrivalRadius:    vmath.FromFloat(2.0),
	ArrivalDragBoost: vmath.FromFloat(2.0),
	DeadZone:         vmath.Scale / 2,
}

// LootHoming: aggressive arrival drag kills orbital momentum for reliable capture
var LootHoming = physics.HomingProfile{
	BaseSpeed:        parameter.LootChaseSpeed,
	HomingAccel:      parameter.LootHomingAccel,
	Drag:             vmath.FromFloat(2.0),
	ArrivalRadius:    vmath.FromFloat(5.0),
	ArrivalDragBoost: vmath.FromFloat(25.0),
	DeadZone:         vmath.Scale / 2,
}

// MissileHoming: BaseSpeed 0 makes drag continuous; full accel through arrival
var MissileHoming = physics.HomingProfile{
	HomingAccel:      parameter.MissileHomingAccel,
	Drag:             parameter.MissileDrag,
	ArrivalRadius:    parameter.MissileArrivalRadius,
	ArrivalDragBoost: vmath.FromFloat(2.0),
	ArrivalAccelMin:  vmath.Scale,
	DeadZone:         vmath.Scale / 10,
}

// EyeHomingProfiles holds per-type homing built from the eye parameter table
var EyeHomingProfiles [parameter.EyeTypeCount]physics.HomingProfile

func init() {
	for i := range parameter.EyeTypeCount {
		p := &parameter.EyeTypeTable[i]
		EyeHomingProfiles[i] = physics.HomingProfile{
			BaseSpeed:   vmath.FromFloat(p.BaseSpeed),
			HomingAccel: vmath.FromFloat(p.HomingAccel),
			Drag:        vmath.FromFloat(p.Drag),
		}
	}
}
