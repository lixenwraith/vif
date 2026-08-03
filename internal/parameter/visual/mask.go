package visual

// Render masks categorize buffer cells for selective post-processing
const (
	MaskNone      uint8 = 0
	MaskPing      uint8 = 1 << 0
	MaskGlyph     uint8 = 1 << 1
	MaskField     uint8 = 1 << 2
	MaskTransient uint8 = 1 << 3
	MaskComposite uint8 = 1 << 4
	MaskUI        uint8 = 1 << 5
	MaskAll       uint8 = 0xFF
)