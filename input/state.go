package input

// InputMode mirrors game modes for parser context
// Kept in sync by mode.Router via SetMode()
// Values match engine.GameMode for easy conversion
type InputMode uint8

const (
	ModeNormal InputMode = iota
	ModeVisual
	ModeInsert
	ModeSearch
	ModeCommand
	ModeOverlay
)

// InputState tracks Normal-mode parser state machine
type InputState uint8

const (
	StateIdle               InputState = iota // Default state, awaiting initial key
	StateCount                                // Accumulating numeric prefix (1-9 start, 0 continues)
	StateCharWait                             // After f/F/t/T, awaiting target character
	StateOperatorWait                         // After operator (d), awaiting motion or second operator
	StateOperatorCharWait                     // After operator + f/F/t/T, awaiting target character
	StatePrefixG                              // After 'g' prefix, awaiting second key (g/G/l/h/k/j)
	StateOperatorPrefixG                      // After operator + 'g', awaiting motion (e.g., dgg)
	StateMarkerAwaitColor                     // After g+direction, awaiting color (r/g/b) or repeat direction
	StateMacroRecordAwait                     // After 'q', awaiting label [a-z] or '@' (stop-all)
	StateMacroPlayAwait                       // After '@', awaiting label [a-z] or '@' (infinite prefix)
	StateMacroInfiniteAwait                   // After '@@', awaiting label [a-z] for infinite playback
)