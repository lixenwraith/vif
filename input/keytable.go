package input

import "github.com/lixenwraith/vi-fighter/terminal"

// KeyBehavior classifies how a key is processed
type KeyBehavior uint8

const (
	BehaviorNone KeyBehavior = iota
	BehaviorMotion
	BehaviorCharWait
	BehaviorOperator
	BehaviorPrefix
	BehaviorModeSwitch
	BehaviorSpecial
	BehaviorSystem
	BehaviorAction
)

// KeyEntry describes a key's behavior without function pointers
type KeyEntry struct {
	Behavior   KeyBehavior
	Motion     MotionOp
	Special    SpecialOp
	ModeTarget ModeTarget
	IntentType IntentType
}

// KeyTable maps keys to behaviors for all modes
type KeyTable struct {
	// Special keys (Ctrl+*, arrows, function keys)
	SpecialKeys map[terminal.Key]KeyEntry

	// Normal mode rune bindings
	NormalRunes map[rune]KeyEntry

	// Motions valid after operator (d)
	OperatorMotions map[rune]KeyEntry

	// Keys after g prefix
	PrefixG map[rune]KeyEntry

	// Overlay mode bindings
	OverlayRunes map[rune]KeyEntry
	OverlayKeys  map[terminal.Key]KeyEntry

	// Text mode navigation keys (Insert/Search/Command)
	TextNavKeys map[terminal.Key]KeyEntry
}

// DefaultKeyTable returns the default key bindings
func DefaultKeyTable() *KeyTable {
	return &KeyTable{
		SpecialKeys: map[terminal.Key]KeyEntry{
			terminal.KeyCtrlQ:     {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentQuit},
			terminal.KeyCtrlC:     {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentQuit},
			terminal.KeyCtrlS:     {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentToggleMute},
			terminal.KeyEscape:    {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentEscape},
			terminal.KeyUp:        {BehaviorMotion, MotionUp, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyDown:      {BehaviorMotion, MotionDown, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyLeft:      {BehaviorMotion, MotionLeft, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyRight:     {BehaviorMotion, MotionRight, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyHome:      {BehaviorMotion, MotionLineStart, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyEnd:       {BehaviorMotion, MotionLineEnd, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyTab:       {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentNuggetJump},
			terminal.KeyBacktab:   {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentGoldJump},
			terminal.KeyEnter:     {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentFireCleaner},
			terminal.KeyBackspace: {BehaviorMotion, MotionLeft, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyPageUp:    {BehaviorMotion, MotionHalfPageUp, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyPageDown:  {BehaviorMotion, MotionHalfPageDown, SpecialNone, ModeTargetNone, IntentNone},
		},

		NormalRunes: map[rune]KeyEntry{
			// Basic motions
			'h': {BehaviorMotion, MotionLeft, SpecialNone, ModeTargetNone, IntentNone},
			'j': {BehaviorMotion, MotionDown, SpecialNone, ModeTargetNone, IntentNone},
			'k': {BehaviorMotion, MotionUp, SpecialNone, ModeTargetNone, IntentNone},
			'l': {BehaviorMotion, MotionRight, SpecialNone, ModeTargetNone, IntentNone},
			' ': {BehaviorMotion, MotionRight, SpecialNone, ModeTargetNone, IntentNone},

			// Append
			'a': {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentAppend},

			// Word motions
			'w': {BehaviorMotion, MotionWordForward, SpecialNone, ModeTargetNone, IntentNone},
			'W': {BehaviorMotion, MotionWORDForward, SpecialNone, ModeTargetNone, IntentNone},
			'b': {BehaviorMotion, MotionWordBack, SpecialNone, ModeTargetNone, IntentNone},
			'B': {BehaviorMotion, MotionWORDBack, SpecialNone, ModeTargetNone, IntentNone},
			'e': {BehaviorMotion, MotionWordEnd, SpecialNone, ModeTargetNone, IntentNone},
			'E': {BehaviorMotion, MotionWORDEnd, SpecialNone, ModeTargetNone, IntentNone},

			// Line motions
			'0': {BehaviorMotion, MotionLineStart, SpecialNone, ModeTargetNone, IntentNone},
			'^': {BehaviorMotion, MotionFirstNonWS, SpecialNone, ModeTargetNone, IntentNone},
			'$': {BehaviorMotion, MotionLineEnd, SpecialNone, ModeTargetNone, IntentNone},

			// Column motions
			'[': {BehaviorMotion, MotionColumnUp, SpecialNone, ModeTargetNone, IntentNone},
			']': {BehaviorMotion, MotionColumnDown, SpecialNone, ModeTargetNone, IntentNone},

			// Screen motions
			'H': {BehaviorMotion, MotionScreenTop, SpecialNone, ModeTargetNone, IntentNone},
			'M': {BehaviorMotion, MotionScreenMid, SpecialNone, ModeTargetNone, IntentNone},
			'L': {BehaviorMotion, MotionScreenBot, SpecialNone, ModeTargetNone, IntentNone},
			'G': {BehaviorMotion, MotionFileEnd, SpecialNone, ModeTargetNone, IntentNone},

			// Paragraph motions
			'{': {BehaviorMotion, MotionParaBack, SpecialNone, ModeTargetNone, IntentNone},
			'}': {BehaviorMotion, MotionParaForward, SpecialNone, ModeTargetNone, IntentNone},

			// Bracket matching
			'%': {BehaviorMotion, MotionMatchBracket, SpecialNone, ModeTargetNone, IntentNone},

			// Char-wait commands
			'f': {BehaviorCharWait, MotionFindForward, SpecialNone, ModeTargetNone, IntentNone},
			'F': {BehaviorCharWait, MotionFindBack, SpecialNone, ModeTargetNone, IntentNone},
			't': {BehaviorCharWait, MotionTillForward, SpecialNone, ModeTargetNone, IntentNone},
			'T': {BehaviorCharWait, MotionTillBack, SpecialNone, ModeTargetNone, IntentNone},

			// Operator
			'd': {BehaviorOperator, MotionNone, SpecialNone, ModeTargetNone, IntentNone},

			// Prefix
			'g': {BehaviorPrefix, MotionNone, SpecialNone, ModeTargetNone, IntentNone},

			// Actions
			'\\': {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentFireSpecial},

			// Mode switches
			'i': {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetInsert, IntentNone},
			'v': {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetVisual, IntentNone},
			'/': {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetSearch, IntentNone},
			':': {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetCommand, IntentNone},

			// Special commands
			'x': {BehaviorSpecial, MotionNone, SpecialDeleteChar, ModeTargetNone, IntentNone},
			'D': {BehaviorSpecial, MotionNone, SpecialDeleteToEnd, ModeTargetNone, IntentNone},
			'n': {BehaviorSpecial, MotionNone, SpecialSearchNext, ModeTargetNone, IntentNone},
			'N': {BehaviorSpecial, MotionNone, SpecialSearchPrev, ModeTargetNone, IntentNone},
			';': {BehaviorSpecial, MotionNone, SpecialRepeatFind, ModeTargetNone, IntentNone},
			',': {BehaviorSpecial, MotionNone, SpecialRepeatFindRev, ModeTargetNone, IntentNone},
		},

		OperatorMotions: map[rune]KeyEntry{
			'w': {BehaviorMotion, MotionWordForward, SpecialNone, ModeTargetNone, IntentNone},
			'W': {BehaviorMotion, MotionWORDForward, SpecialNone, ModeTargetNone, IntentNone},
			'b': {BehaviorMotion, MotionWordBack, SpecialNone, ModeTargetNone, IntentNone},
			'B': {BehaviorMotion, MotionWORDBack, SpecialNone, ModeTargetNone, IntentNone},
			'e': {BehaviorMotion, MotionWordEnd, SpecialNone, ModeTargetNone, IntentNone},
			'E': {BehaviorMotion, MotionWORDEnd, SpecialNone, ModeTargetNone, IntentNone},
			'0': {BehaviorMotion, MotionLineStart, SpecialNone, ModeTargetNone, IntentNone},
			'^': {BehaviorMotion, MotionFirstNonWS, SpecialNone, ModeTargetNone, IntentNone},
			'$': {BehaviorMotion, MotionLineEnd, SpecialNone, ModeTargetNone, IntentNone},
			'G': {BehaviorMotion, MotionFileEnd, SpecialNone, ModeTargetNone, IntentNone},
			'{': {BehaviorMotion, MotionParaBack, SpecialNone, ModeTargetNone, IntentNone},
			'}': {BehaviorMotion, MotionParaForward, SpecialNone, ModeTargetNone, IntentNone},
			'%': {BehaviorMotion, MotionMatchBracket, SpecialNone, ModeTargetNone, IntentNone},
			'h': {BehaviorMotion, MotionLeft, SpecialNone, ModeTargetNone, IntentNone},
			'j': {BehaviorMotion, MotionDown, SpecialNone, ModeTargetNone, IntentNone},
			'k': {BehaviorMotion, MotionUp, SpecialNone, ModeTargetNone, IntentNone},
			'l': {BehaviorMotion, MotionRight, SpecialNone, ModeTargetNone, IntentNone},
			' ': {BehaviorMotion, MotionRight, SpecialNone, ModeTargetNone, IntentNone},
			'f': {BehaviorCharWait, MotionFindForward, SpecialNone, ModeTargetNone, IntentNone},
			'F': {BehaviorCharWait, MotionFindBack, SpecialNone, ModeTargetNone, IntentNone},
			't': {BehaviorCharWait, MotionTillForward, SpecialNone, ModeTargetNone, IntentNone},
			'T': {BehaviorCharWait, MotionTillBack, SpecialNone, ModeTargetNone, IntentNone},
			'g': {BehaviorPrefix, MotionNone, SpecialNone, ModeTargetNone, IntentNone},
		},

		PrefixG: map[rune]KeyEntry{
			'g': {BehaviorMotion, MotionFileStart, SpecialNone, ModeTargetNone, IntentNone},
			'o': {BehaviorMotion, MotionOrigin, SpecialNone, ModeTargetNone, IntentNone},
			'k': {BehaviorMotion, MotionColumnUp, SpecialNone, ModeTargetNone, IntentNone},
			'j': {BehaviorMotion, MotionColumnDown, SpecialNone, ModeTargetNone, IntentNone},
		},

		OverlayRunes: map[rune]KeyEntry{
			'j': {BehaviorMotion, MotionDown, SpecialNone, ModeTargetNone, IntentNone},
			'k': {BehaviorMotion, MotionUp, SpecialNone, ModeTargetNone, IntentNone},
			'q': {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayClose},
		},

		OverlayKeys: map[terminal.Key]KeyEntry{
			terminal.KeyUp:       {BehaviorMotion, MotionUp, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyDown:     {BehaviorMotion, MotionDown, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyEscape:   {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayClose},
			terminal.KeyEnter:    {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayActivate},
			terminal.KeyPageUp:   {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayPageUp},
			terminal.KeyPageDown: {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayPageDown},
		},

		// Navigation keys valid in Insert/Search/Command modes
		TextNavKeys: map[terminal.Key]KeyEntry{
			terminal.KeyUp:        {BehaviorMotion, MotionUp, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyDown:      {BehaviorMotion, MotionDown, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyLeft:      {BehaviorMotion, MotionLeft, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyRight:     {BehaviorMotion, MotionRight, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyHome:      {BehaviorMotion, MotionLineStart, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyEnd:       {BehaviorMotion, MotionLineEnd, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyBackspace: {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentTextBackspace},
			terminal.KeyDelete:    {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentInsertDeleteCurrent},
			terminal.KeyPageUp:    {BehaviorMotion, MotionHalfPageUp, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyPageDown:  {BehaviorMotion, MotionHalfPageDown, SpecialNone, ModeTargetNone, IntentNone},
			terminal.KeyEnter:     {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentTextConfirm},
			terminal.KeyEscape:    {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentEscape},
			terminal.KeyCtrlQ:     {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentQuit},
			terminal.KeyCtrlC:     {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentQuit},
			terminal.KeyCtrlS:     {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentToggleMute},
		},
	}
}