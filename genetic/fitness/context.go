package fitness

// Context provides additional information for fitness calculation
type Context interface {
	Get(key string) (float64, bool)
}

// MapContext is a simple map-based Context implementation
type MapContext map[string]float64

func (c MapContext) Get(key string) (float64, bool) {
	v, ok := c[key]
	return v, ok
}

// Standard context keys
const (
	ContextThreatLevel      = "threat_level"
	ContextEnergyManagement = "energy_management"
	ContextHeatManagement   = "heat_management"
	ContextTypingAccuracy   = "typing_accuracy"
)