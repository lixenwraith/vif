package system

// TODO: think of a way to plug this in

// Environment holds global environmental effects
// Applied to composites during movement integration
type Environment struct {
	// Global wind velocity in 16.16 fixed-point
	WindVelX int64
	WindVelY int64
}

// TODO: this needs proper conversion to Q32.32
// SetWind configures global wind velocity
// velocity is in units per second, converted to 16.16 per-tick
func (e *Environment) SetWind(velX, velY float64, ticksPerSecond int) {
	e.WindVelX = int64((velX / float64(ticksPerSecond)) * 65536)
	e.WindVelY = int64((velY / float64(ticksPerSecond)) * 65536)
}

// ClearWind disables wind effect
func (e *Environment) ClearWind() {
	e.WindVelX = 0
	e.WindVelY = 0
}