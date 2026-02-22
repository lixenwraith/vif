package input

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
		"toggle_effect_mute": {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentToggleEffectMute},
		"toggle_music_mute":  {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentToggleMusicMute},

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

		// Char-wait (f/F/t/T)
		"char_find_forward": {BehaviorCharWait, MotionFindForward, SpecialNone, ModeTargetNone, IntentNone},
		"char_find_back":    {BehaviorCharWait, MotionFindBack, SpecialNone, ModeTargetNone, IntentNone},
		"char_till_forward": {BehaviorCharWait, MotionTillForward, SpecialNone, ModeTargetNone, IntentNone},
		"char_till_back":    {BehaviorCharWait, MotionTillBack, SpecialNone, ModeTargetNone, IntentNone},

		// Operator
		"operator_delete": {BehaviorOperator, MotionNone, SpecialNone, ModeTargetNone, IntentNone},

		// Prefix keys
		"prefix_g":          {BehaviorPrefix, MotionNone, SpecialNone, ModeTargetNone, IntentNone},
		"prefix_macro_play": {BehaviorPrefixMacro, MotionNone, SpecialNone, ModeTargetNone, IntentNone},

		// Marker start (g + direction)
		"marker_glyph_left":  {BehaviorMarkerStart, MotionColoredGlyphLeft, SpecialNone, ModeTargetNone, IntentNone},
		"marker_glyph_right": {BehaviorMarkerStart, MotionColoredGlyphRight, SpecialNone, ModeTargetNone, IntentNone},
		"marker_glyph_up":    {BehaviorMarkerStart, MotionColoredGlyphUp, SpecialNone, ModeTargetNone, IntentNone},
		"marker_glyph_down":  {BehaviorMarkerStart, MotionColoredGlyphDown, SpecialNone, ModeTargetNone, IntentNone},

		// Mode switches
		"mode_insert":  {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetInsert, IntentNone},
		"mode_visual":  {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetVisual, IntentNone},
		"mode_search":  {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetSearch, IntentNone},
		"mode_command": {BehaviorModeSwitch, MotionNone, SpecialNone, ModeTargetCommand, IntentNone},

		// Special commands
		"special_delete_char":     {BehaviorSpecial, MotionNone, SpecialDeleteChar, ModeTargetNone, IntentNone},
		"special_delete_to_end":   {BehaviorSpecial, MotionNone, SpecialDeleteToEnd, ModeTargetNone, IntentNone},
		"special_search_next":     {BehaviorSpecial, MotionNone, SpecialSearchNext, ModeTargetNone, IntentNone},
		"special_search_prev":     {BehaviorSpecial, MotionNone, SpecialSearchPrev, ModeTargetNone, IntentNone},
		"special_repeat_find":     {BehaviorSpecial, MotionNone, SpecialRepeatFind, ModeTargetNone, IntentNone},
		"special_repeat_find_rev": {BehaviorSpecial, MotionNone, SpecialRepeatFindRev, ModeTargetNone, IntentNone},

		// Actions
		"fire_main":           {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentFireMain},
		"fire_special":        {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentFireSpecial},
		"nugget_jump":         {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentNuggetJump},
		"gold_jump":           {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentGoldJump},
		"append":              {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentAppend},
		"undo":                {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentUndo},
		"macro_record_toggle": {BehaviorAction, MotionNone, SpecialNone, ModeTargetNone, IntentMacroRecordToggle},

		// Overlay
		"overlay_close":     {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayClose},
		"overlay_activate":  {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayActivate},
		"overlay_page_up":   {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayPageUp},
		"overlay_page_down": {BehaviorSystem, MotionNone, SpecialNone, ModeTargetNone, IntentOverlayPageDown},

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

// IsActionName returns true if name is a registered action
func IsActionName(name string) bool {
	_, ok := actionRegistry[name]
	return ok
}

// ActionNames returns all registered action names (for documentation/validation)
func ActionNames() []string {
	names := make([]string, 0, len(actionRegistry))
	for name := range actionRegistry {
		names = append(names, name)
	}
	return names
}