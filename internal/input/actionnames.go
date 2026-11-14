package input

import (
	"fmt"
	"unicode/utf8"
)

// actionRegistry maps canonical action names to KeyEntry structs
// Used by keymap config loader to resolve TOML action strings to bindings
var actionRegistry map[string]KeyEntry

func init() {
	actionRegistry = buildActionRegistry()
}

func buildActionRegistry() map[string]KeyEntry {
	return map[string]KeyEntry{
		// Unbind sentinel
		"none": {},

		// System
		"quit":               {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentQuit},
		"escape":             {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentEscape},
		"toggle_audio_cycle": {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentToggleAudioCycle},
		"config_menu":        {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentConfigMenu},
		"help":               {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentHelp},
		"pause":              {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentPause},

		// Basic motions
		"motion_left":             {BehaviorMotion, MotionLeft, SpecialNone, ModeTargetNone, IntentNone},
		"motion_right":            {BehaviorMotion, MotionRight, SpecialNone, ModeTargetNone, IntentNone},
		"motion_up":               {BehaviorMotion, MotionUp, SpecialNone, ModeTargetNone, IntentNone},
		"motion_down":             {BehaviorMotion, MotionDown, SpecialNone, ModeTargetNone, IntentNone},
		"motion_word_forward":     {BehaviorMotion, MotionWordForward, SpecialNone, ModeTargetNone, IntentNone},
		"motion_word_forward_big": {BehaviorMotion, MotionWORDForward, SpecialNone, ModeTargetNone, IntentNone},
		"motion_word_back":        {BehaviorMotion, MotionWordBack, SpecialNone, ModeTargetNone, IntentNone},
		"motion_word_back_big":    {BehaviorMotion, MotionWORDBack, SpecialNone, ModeTargetNone, IntentNone},
		"motion_word_end":         {BehaviorMotion, MotionWordEnd, SpecialNone, ModeTargetNone, IntentNone},
		"motion_word_end_big":     {BehaviorMotion, MotionWORDEnd, SpecialNone, ModeTargetNone, IntentNone},
		"motion_line_start":       {BehaviorMotion, MotionLineStart, SpecialNone, ModeTargetNone, IntentNone},
		"motion_line_end":         {BehaviorMotion, MotionLineEnd, SpecialNone, ModeTargetNone, IntentNone},
		"motion_first_non_ws":     {BehaviorMotion, MotionFirstNonWS, SpecialNone, ModeTargetNone, IntentNone},

		// Screen motions
		"motion_screen_vertical_mid":   {BehaviorMotion, MotionScreenVerticalMid, SpecialNone, ModeTargetNone, IntentNone},
		"motion_screen_horizontal_mid": {BehaviorMotion, MotionScreenHorizontalMid, SpecialNone, ModeTargetNone, IntentNone},
		"motion_screen_top":            {BehaviorMotion, MotionScreenTop, SpecialNone, ModeTargetNone, IntentNone},
		"motion_screen_bottom":         {BehaviorMotion, MotionScreenBottom, SpecialNone, ModeTargetNone, IntentNone},

		// Paragraph motions
		"motion_para_back":    {BehaviorMotion, MotionParaBack, SpecialNone, ModeTargetNone, IntentNone},
		"motion_para_forward": {BehaviorMotion, MotionParaForward, SpecialNone, ModeTargetNone, IntentNone},

		// Bracket
		"motion_match_bracket": {BehaviorMotion, MotionMatchBracket, SpecialNone, ModeTargetNone, IntentNone},

		// g-prefix motions
		"motion_origin": {BehaviorMotion, MotionOrigin, SpecialNone, ModeTargetNone, IntentNone},
		"motion_end":    {BehaviorMotion, MotionEnd, SpecialNone, ModeTargetNone, IntentNone},
		"motion_center": {BehaviorMotion, MotionCenter, SpecialNone, ModeTargetNone, IntentNone},

		// Half-page motions
		"motion_half_page_left":  {BehaviorMotion, MotionHalfPageLeft, SpecialNone, ModeTargetNone, IntentNone},
		"motion_half_page_right": {BehaviorMotion, MotionHalfPageRight, SpecialNone, ModeTargetNone, IntentNone},
		"motion_half_page_up":    {BehaviorMotion, MotionHalfPageUp, SpecialNone, ModeTargetNone, IntentNone},
		"motion_half_page_down":  {BehaviorMotion, MotionHalfPageDown, SpecialNone, ModeTargetNone, IntentNone},

		// Column motions
		"motion_column_up":   {BehaviorMotion, MotionColumnUp, SpecialNone, ModeTargetNone, IntentNone},
		"motion_column_down": {BehaviorMotion, MotionColumnDown, SpecialNone, ModeTargetNone, IntentNone},

		// Char-wait (f/F/t/T); r's char completes a special in place of a motion
		"char_find_forward": {BehaviorCharWait, MotionFindForward, SpecialNone, ModeTargetNone, IntentNone},
		"char_find_back":    {BehaviorCharWait, MotionFindBack, SpecialNone, ModeTargetNone, IntentNone},
		"char_till_forward": {BehaviorCharWait, MotionTillForward, SpecialNone, ModeTargetNone, IntentNone},
		"char_till_back":    {BehaviorCharWait, MotionTillBack, SpecialNone, ModeTargetNone, IntentNone},
		"char_replace":      {BehaviorCharWait, MotionNone, SpecialReplaceChar, ModeTargetNone, IntentNone},

		// Operators; c deletes as d does, then enters Insert
		"operator_delete": {BehaviorOperator, MotionNone, SpecialNone, ModeTargetNone, IntentNone},
		"operator_change": {BehaviorOperator, MotionNone, SpecialNone, ModeTargetInsert, IntentNone},

		// Prefix keys
		"prefix_g":          {BehaviorPrefix, MotionNone, SpecialNone, ModeTargetNone, IntentNone},
		"prefix_macro_play": {BehaviorPrefixMacro, MotionNone, SpecialNone, ModeTargetNone, IntentNone},

		// Marker start (g + direction)
		"marker_glyph_left":  {BehaviorMarkerStart, MotionColoredGlyphLeft, SpecialNone, ModeTargetNone, IntentNone},
		"marker_glyph_right": {BehaviorMarkerStart, MotionColoredGlyphRight, SpecialNone, ModeTargetNone, IntentNone},
		"marker_glyph_up":    {BehaviorMarkerStart, MotionColoredGlyphUp, SpecialNone, ModeTargetNone, IntentNone},
		"marker_glyph_down":  {BehaviorMarkerStart, MotionColoredGlyphDown, SpecialNone, ModeTargetNone, IntentNone},

		// Mode switches; a motion naming a mode enters it once moved. vim's append
		// is no default key's, since a cycles auto fire: a keymap or scenario binds it.
		"mode_insert":              {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetInsert, IntentNone},
		"mode_visual":              {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetVisual, IntentNone},
		"mode_search":              {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetSearch, IntentNone},
		"mode_command":             {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetCommand, IntentNone},
		"mode_append":              {BehaviorMotion, MotionRight, SpecialNone, ModeTargetInsert, IntentNone},
		"mode_insert_first_non_ws": {BehaviorMotion, MotionFirstNonWS, SpecialNone, ModeTargetInsert, IntentNone},
		"mode_insert_line_end":     {BehaviorMotion, MotionLastGlyph, SpecialNone, ModeTargetInsert, IntentNone},

		// Special commands
		"special_delete_char":         {BehaviorSpecial, MotionNone, SpecialDeleteChar, ModeTargetNone, IntentNone},
		"special_delete_to_end":       {BehaviorSpecial, MotionNone, SpecialDeleteToEnd, ModeTargetNone, IntentNone},
		"special_search_next":         {BehaviorSpecial, MotionNone, SpecialSearchNext, ModeTargetNone, IntentNone},
		"special_search_prev":         {BehaviorSpecial, MotionNone, SpecialSearchPrev, ModeTargetNone, IntentNone},
		"special_repeat_find":         {BehaviorSpecial, MotionNone, SpecialRepeatFind, ModeTargetNone, IntentNone},
		"special_repeat_find_rev":     {BehaviorSpecial, MotionNone, SpecialRepeatFindRev, ModeTargetNone, IntentNone},
		"special_change_to_end":       {BehaviorSpecial, MotionNone, SpecialDeleteToEnd, ModeTargetInsert, IntentNone},
		"special_search_word_forward": {BehaviorSpecial, MotionNone, SpecialSearchWordForward, ModeTargetNone, IntentNone},
		"special_search_word_back":    {BehaviorSpecial, MotionNone, SpecialSearchWordBack, ModeTargetNone, IntentNone},

		// . completes the last change again
		"repeat_change": {BehaviorRepeat, MotionNone, SpecialNone, ModeTargetNone, IntentNone},

		// Actions
		"fire_main":           {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentFireMain},
		"fire_special":        {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentFireSpecial},
		"nugget_jump":         {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentNuggetJump},
		"gold_jump":           {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentGoldJump},
		"toggle_auto_fire":    {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentToggleAutoFire},
		"undo":                {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentUndo},
		"macro_record_toggle": {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentMacroRecordToggle},

		// Overlay
		"overlay_close":     {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayClose},
		"overlay_activate":  {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayActivate},
		"overlay_page_up":   {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayPageUp},
		"overlay_page_down": {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayPageDown},
		"overlay_filter":    {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayFilter},

		// Text mode
		"text_backspace":      {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentTextBackspace},
		"text_delete_current": {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentInsertDeleteCurrent},
		"text_confirm":        {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentTextConfirm},
	}
}

// ActionEntry resolves a canonical action name to its KeyEntry
// Returns zero KeyEntry and false if name is unknown
func ActionEntry(name string) (KeyEntry, bool) {
	entry, ok := actionRegistry[name]
	return entry, ok
}

// PressAction presses the key char a scenario captured. Scripts and bots name it; a
// keymap cannot bind it, since the scenario, not the player, chooses the keys.
const PressAction = "press"

// IntentFor builds the intent one canonical action performs, as an authored script
// or a bot names it: count defaults to one, and char is the one rune a char-wait
// action targets. A parser prefix is refused, since it is half of a key sequence.
func IntentFor(name string, count int, char string) (Intent, error) {
	if name == PressAction {
		r, size := utf8.DecodeRuneInString(char)
		if size == 0 || size != len(char) || !Capturable(r) {
			return Intent{}, fmt.Errorf("intent %q requires one printable ASCII char", name)
		}
		return Intent{Type: IntentKeyPress, Char: r, Count: 1, Command: name}, nil
	}
	entry, ok := ActionEntry(name)
	if !ok || name == "none" {
		return Intent{}, fmt.Errorf("unknown intent action %q", name)
	}
	if char != "" && entry.Behavior != BehaviorCharWait {
		return Intent{}, fmt.Errorf("char applies only to a char-wait intent")
	}
	if count == 0 {
		count = 1
	}
	intent := Intent{Count: count, Command: name, ModeTarget: entry.ModeTarget}
	switch entry.Behavior {
	case BehaviorMotion:
		intent.Type, intent.Motion = IntentMotion, entry.Motion
	case BehaviorCharWait:
		if utf8.RuneCountInString(char) != 1 {
			return Intent{}, fmt.Errorf("intent %q requires one char", name)
		}
		intent.Type, intent.Motion, intent.Special = IntentCharMotion, entry.Motion, entry.Special
		if entry.Special != SpecialNone {
			intent.Type = IntentSpecial
		}
		intent.Char, _ = utf8.DecodeRuneInString(char)
	case BehaviorModeSwitch:
		intent.Type = IntentModeSwitch
	case BehaviorSpecial:
		intent.Type, intent.Special = IntentSpecial, entry.Special
	case BehaviorSystem, BehaviorAction:
		intent.Type = entry.IntentType
	case BehaviorRepeat:
		return Intent{}, fmt.Errorf("intent action %q repeats the parser's last change; name that change", name)
	default:
		return Intent{}, fmt.Errorf("intent action %q is a parser prefix; use command or a complete semantic action", name)
	}
	return intent, nil
}
