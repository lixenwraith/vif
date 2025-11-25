package modes

// CommandCapability defines the capabilities and requirements of a vi command
type CommandCapability struct {
	// AcceptsCount indicates if the command can be prefixed with a count (e.g., 5j, 2fa)
	AcceptsCount bool

	// MultiKeystroke indicates if the command requires additional keystrokes to complete (e.g. 'f', 'd', 'g')
	MultiKeystroke bool

	// RequiresMotion indicates if the command is an operator that requires a motion (e.g. 'd')
	RequiresMotion bool
}

// commandCapabilities maps vi commands to their capabilities
// This provides a systematic way to determine which commands accept counts
// and which are multi-keystroke commands that need to preserve count through phases
var commandCapabilities = map[rune]CommandCapability{
	// Basic motions - all accept count
	'h': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	'j': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	'k': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	'l': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	' ': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false}, // Space acts like 'l'

	// Line motions - all accept count
	'0': {AcceptsCount: false, MultiKeystroke: false, RequiresMotion: false}, // Line start (no count)
	'^': {AcceptsCount: false, MultiKeystroke: false, RequiresMotion: false}, // First non-whitespace
	'$': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},  // Line end (count: Nth line down)

	// Word motions - all accept count
	'w': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	'W': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	'b': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	'B': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	'e': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	'E': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},

	// Screen motions
	'H': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},  // High (count: Nth line from top)
	'M': {AcceptsCount: false, MultiKeystroke: false, RequiresMotion: false}, // Middle
	'L': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},  // Low (count: Nth line from bottom)
	'G': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},  // Go to line (count: line number)
	// 'g' is handled as multi-keystroke prefix below

	// Paragraph motions - accept count
	'{': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	'}': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false},
	'%': {AcceptsCount: false, MultiKeystroke: false, RequiresMotion: false}, // Matching bracket

	// Find/Search - 'f' and 'F' accept count and are multi-keystroke
	'f': {AcceptsCount: true, MultiKeystroke: true, RequiresMotion: false},
	'F': {AcceptsCount: true, MultiKeystroke: true, RequiresMotion: false},
	// Future: 't' (till), 'T' (till backward)

	// Delete operations
	'x': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false}, // Delete char
	'd': {AcceptsCount: true, MultiKeystroke: true, RequiresMotion: true},   // Delete operator
	'D': {AcceptsCount: true, MultiKeystroke: false, RequiresMotion: false}, // Delete to end of line

	// Search (no count support in vi-fighter yet)
	'/': {AcceptsCount: false, MultiKeystroke: false, RequiresMotion: false},
	'n': {AcceptsCount: false, MultiKeystroke: false, RequiresMotion: false},
	'N': {AcceptsCount: false, MultiKeystroke: false, RequiresMotion: false},

	// Mode switches (no count)
	'i': {AcceptsCount: false, MultiKeystroke: false, RequiresMotion: false},
}

// TODO: motion expand
// GetCommandCapability returns the capability for a given command
// Returns a zero-value CommandCapability if the command is not registered
func GetCommandCapability(cmd rune) CommandCapability {
	capability, exists := commandCapabilities[cmd]
	if !exists {
		// Default: commands don't accept count and aren't multi-keystroke
		return CommandCapability{
			AcceptsCount:   false,
			MultiKeystroke: false,
			RequiresMotion: false,
		}
	}
	return capability
}