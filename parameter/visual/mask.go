package visual

// Render masks categorize buffer cells for selective post-processing
const (
	MaskNone      uint8 = 0
	MaskPing      uint8 = 1 << 0 // Background ping and ping grid
	MaskGlyph     uint8 = 1 << 1 // Interactable non-composite characters: glyphs, nugget
	MaskField     uint8 = 1 << 2 // Shields
	MaskTransient uint8 = 1 << 3 // Decay, blossom, cleaner, flash, materialize, drain
	MaskComposite uint8 = 1 << 4 // Composites (Gold, Quasar)
	MaskUI        uint8 = 1 << 5 // Heat meter, status bar, line numbers, cursor, overlay
	MaskAll       uint8 = 0xFF
)