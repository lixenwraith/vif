package input

// IntentType discriminates semantic actions
type IntentType uint8

const (
	IntentNone IntentType = iota

	// System-level intents
	IntentQuit             // Ctrl+Q, Ctrl+C
	IntentEscape           // ESC key (context-dependent)
	IntentToggleEffectMute // Ctrl+S
	IntentToggleMusicMute  // Ctrl+G
	IntentResize           // Terminal resize event

	// Normal mode navigation
	IntentMotion     // h,j,k,l,w,b,0,$,G,gg,arrows,etc
	IntentCharMotion // f,F,t,T + target char

	// Normal mode operators
	IntentOperatorMotion     // d + motion (e.g., dw, d2w)
	IntentOperatorLine       // dd (line-wise delete)
	IntentOperatorCharMotion // d + f/t + char (e.g., df;)

	// Normal mode special commands
	IntentSpecial     // x, D, n, N, ;, ,
	IntentNuggetJump  // Tab
	IntentGoldJump    // Shift+Tab
	IntentFireMain    // Enter in Normal mode
	IntentFireSpecial // \ in Normal mode

	// Motion markers
	IntentMotionMarkerShow // gl/gh/gk/gj - show markers, await color
	IntentMotionMarkerJump // r/g/b after marker show - jump to colored glyph

	// Macro
	IntentMacroRecordStart  // q + label - start recording to label
	IntentMacroRecordStop   // q while recording - stop recording
	IntentMacroPlay         // [count]@label - play macro
	IntentMacroPlayInfinite // @@label - play indefinitely
	IntentMacroPlayAll      // @@@ - play all recorded macros indefinitely
	IntentMacroStopOne      // q + label while playing - stop that macro
	IntentMacroStopAll      // q@ while playing - stop all macros
	IntentMacroRecordToggle // q key - placeholder, Router interprets based on context

	// Mode switching
	IntentModeSwitch // i, /, :
	IntentAppend     // a

	// Text entry modes (Insert/Search/Command)
	IntentTextChar            // Printable character
	IntentTextBackspace       // Backspace
	IntentTextConfirm         // Enter (execute search/command)
	IntentTextNav             // Arrow navigation in text modes
	IntentInsertDeleteCurrent // Delete key in Insert mode
	IntentInsertDeleteForward // Space in Insert mode (delete + move)
	IntentInsertDeleteBack    // Backspace in Insert mode (delete prev + move)

	// Overlay mode
	IntentOverlayScroll   // j/k/arrows
	IntentOverlayActivate // Enter/Space (future: section toggle)
	IntentOverlayClose    // ESC/q
	IntentOverlayPageUp   // PgUp
	IntentOverlayPageDown // PgDn

	// Mouse
	IntentMouseClick // Left-click to move cursor
)

// MotionOp identifies motion algorithm
type MotionOp uint8

const (
	MotionNone                MotionOp = iota
	MotionLeft                         // h, Left arrow, Backspace
	MotionRight                        // l, Right arrow, Space
	MotionUp                           // k, Up arrow
	MotionDown                         // j, Down arrow
	MotionWordForward                  // w
	MotionWORDForward                  // W
	MotionWordBack                     // b
	MotionWORDBack                     // B
	MotionWordEnd                      // e
	MotionWORDEnd                      // E
	MotionLineStart                    // 0, Home
	MotionLineEnd                      // $, End
	MotionFirstNonWS                   // ^
	MotionScreenVerticalMid            // M
	MotionScreenHorizontalMid          // m
	MotionScreenTop                    // gg
	MotionScreenBottom                 // G
	MotionParaBack                     // {
	MotionParaForward                  // }
	MotionMatchBracket                 // %
	MotionOrigin                       // go
	MotionEnd                          // g$
	MotionCenter                       // gm
	MotionFindForward                  // f + char
	MotionFindBack                     // F + char
	MotionTillForward                  // t + char
	MotionTillBack                     // T + char
	MotionHalfPageLeft                 // H
	MotionHalfPageRight                // L
	MotionHalfPageUp                   // K, PgUp
	MotionHalfPageDown                 // J, PgDown
	MotionColumnUp                     // [, u
	MotionColumnDown                   // ], o
	MotionColoredGlyphRight            // gl + color
	MotionColoredGlyphLeft             // gh + color
	MotionColoredGlyphUp               // gk + color
	MotionColoredGlyphDown             // gj + color
)

// OperatorOp identifies operator type
type OperatorOp uint8

const (
	OperatorNone OperatorOp = iota
	OperatorDelete
)

// SpecialOp identifies special commands
type SpecialOp uint8

const (
	SpecialNone          SpecialOp = iota
	SpecialDeleteChar              // x
	SpecialDeleteToEnd             // D
	SpecialSearchNext              // n
	SpecialSearchPrev              // N
	SpecialRepeatFind              // ;
	SpecialRepeatFindRev           // ,
)

// ModeTarget identifies mode switch destination
type ModeTarget uint8

const (
	ModeTargetNone ModeTarget = iota
	ModeTargetInsert
	ModeTargetSearch
	ModeTargetCommand
	ModeTargetNormal
	ModeTargetVisual
)

// ScrollDir for overlay navigation
type ScrollDir int8

const (
	ScrollNone ScrollDir = 0
	ScrollUp   ScrollDir = -1
	ScrollDown ScrollDir = 1
)

// Intent represents a parsed semantic action
// Pure data struct with no function pointers or engine dependencies
type Intent struct {
	Type          IntentType
	Motion        MotionOp
	Operator      OperatorOp
	Special       SpecialOp
	ModeTarget    ModeTarget
	ScrollDir     ScrollDir
	Count         int    // Effective count (minimum 1)
	Char          rune   // Target char for f/t motions or typed char
	Command       string // Captured sequence for visual feedback
	MacroPlayback bool   // True if intent originated from macro playback
}