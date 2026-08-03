package core

type Kinetic struct {
	// PreciseX and PreciseY are sub-pixel coordinates in Q32.32 format
	PreciseX, PreciseY int64
	// VelX and VelY represent velocity in cells per second (Q32.32)
	VelX, VelY int64
	// AccelX and AccelY represent acceleration in cells per second squared (Q32.32)
	AccelX, AccelY int64
}