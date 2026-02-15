package input

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Machine is the input state machine
// Parses terminal.Event into semantic Intent
type Machine struct {
	mode     InputMode
	state    InputState
	keyTable *KeyTable

	// Normal mode state
	count1     int
	count2     int
	operator   OperatorOp
	charMotion MotionOp
	prefix     rune

	// Marker state - direction pending color selection
	markerDirection MotionOp

	// Command buffer for visual feedback
	cmdBuffer []rune
}

// NewMachine creates a new input machine
func NewMachine() *Machine {
	return &Machine{
		mode:      ModeNormal,
		state:     StateIdle,
		keyTable:  DefaultKeyTable(),
		cmdBuffer: make([]rune, 0, 16),
	}
}

// SetMode updates the parser's mode context
// Called by mode.Router when game mode changes
func (m *Machine) SetMode(mode InputMode) {
	m.mode = mode
	if mode == ModeNormal {
		m.Reset()
	}
}

// GetPendingCommand returns the current command buffer for UI display
func (m *Machine) GetPendingCommand() string {
	if len(m.cmdBuffer) == 0 {
		return ""
	}
	return string(m.cmdBuffer)
}

// Reset clears all pending state
func (m *Machine) Reset() {
	m.state = StateIdle
	m.count1 = 0
	m.count2 = 0
	m.operator = OperatorNone
	m.charMotion = MotionNone
	m.prefix = 0
	m.markerDirection = MotionNone
	m.cmdBuffer = m.cmdBuffer[:0]
}

// Process parses a terminal event and returns an Intent
// Returns nil if input is incomplete
func (m *Machine) Process(ev terminal.Event) *Intent {
	switch ev.Type {
	case terminal.EventResize:
		return &Intent{Type: IntentResize}
	case terminal.EventKey:
		return m.processKey(ev)
	case terminal.EventMouse:
		return m.processMouse(ev)
	case terminal.EventClosed, terminal.EventError:
		return &Intent{Type: IntentQuit}
	}
	return nil
}

func (m *Machine) processKey(ev terminal.Event) *Intent {
	switch m.mode {
	case ModeNormal, ModeVisual:
		return m.processNormal(ev)
	case ModeInsert:
		return m.processInsert(ev)
	case ModeSearch:
		return m.processSearch(ev)
	case ModeCommand:
		return m.processCommand(ev)
	case ModeOverlay:
		return m.processOverlay(ev)
	}
	return nil
}

func (m *Machine) processMouse(ev terminal.Event) *Intent {
	switch m.mode {
	case ModeNormal, ModeVisual, ModeInsert:
		switch ev.MouseBtn {
		case terminal.MouseBtnLeft:
			switch ev.MouseAction {
			case terminal.MouseActionPress:
				return &Intent{
					Type:  IntentMouseLeftDown,
					Count: ev.MouseX,
					Char:  rune(ev.MouseY),
				}
			case terminal.MouseActionRelease:
				return &Intent{Type: IntentMouseLeftUp}
			case terminal.MouseActionDrag:
				return &Intent{
					Type:  IntentMouseDrag,
					Count: ev.MouseX,
					Char:  rune(ev.MouseY),
				}
			}

		case terminal.MouseBtnRight:
			switch ev.MouseAction {
			case terminal.MouseActionPress:
				return &Intent{Type: IntentMouseRightDown}
			case terminal.MouseActionRelease:
				return &Intent{Type: IntentMouseRightUp}
			}
			// Right drag intentionally ignored

		case terminal.MouseBtnWheelUp, terminal.MouseBtnWheelDown:
			if ev.MouseAction == terminal.MouseActionPress {
				return &Intent{
					Type:  IntentMouseWheelMove,
					Count: ev.MouseX,
					Char:  rune(ev.MouseY),
				}
			}
		}
	}
	return nil
}

func (m *Machine) SetState(state InputState) {
	m.state = state
	// Clear any partial command buffer when state is set externally
	m.cmdBuffer = m.cmdBuffer[:0]
}

// === Normal Mode Processing ===

func (m *Machine) processNormal(ev terminal.Event) *Intent {
	// Handle special keys first
	if ev.Key != terminal.KeyRune {
		// Macro stop (Ctrl+@) works in all modes
		if ev.Key == terminal.KeyCtrlSpace {
			return &Intent{Type: IntentMacroStopAll}
		}

		if entry, ok := m.keyTable.SpecialKeys[ev.Key]; ok {
			return m.handleNormalEntry(entry, 0)
		}
		return nil
	}

	// Rune handling depends on state
	switch m.state {
	case StateIdle, StateCount:
		return m.processIdleOrCount(ev.Rune)
	case StateCharWait:
		return m.completeCharMotion(ev.Rune)
	case StateOperatorWait:
		return m.processOperatorWait(ev.Rune)
	case StateOperatorCharWait:
		return m.completeOperatorCharMotion(ev.Rune)
	case StatePrefixG:
		return m.processPrefixG(ev.Rune)
	case StateOperatorPrefixG:
		return m.processOperatorPrefixG(ev.Rune)
	case StateMarkerAwaitColor:
		return m.processMarkerAwaitColor(ev.Rune)
	case StateMacroRecordAwait:
		return m.processMacroRecordAwait(ev.Rune)
	case StateMacroPlayAwait:
		return m.processMacroPlayAwait(ev.Rune)
	case StateMacroInfiniteAwait:
		return m.processMacroInfiniteAwait(ev.Rune)
	}
	return nil
}

func (m *Machine) processIdleOrCount(key rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, key)

	// Handle count accumulation
	if key >= '1' && key <= '9' {
		m.accumulateCount1(key)
		m.state = StateCount
		return nil
	}
	if key == '0' && m.count1 > 0 {
		m.accumulateCount1(key)
		return nil
	}

	entry, ok := m.keyTable.NormalRunes[key]
	if !ok {
		m.Reset()
		return nil
	}

	return m.handleNormalEntry(entry, key)
}

func (m *Machine) handleNormalEntry(entry KeyEntry, key rune) *Intent {
	switch entry.Behavior {
	case BehaviorMotion:
		return m.buildMotionIntent(entry.Motion)

	case BehaviorCharWait:
		m.charMotion = entry.Motion
		m.state = StateCharWait
		return nil

	case BehaviorOperator:
		m.operator = OperatorDelete
		m.state = StateOperatorWait
		return nil

	case BehaviorPrefix:
		m.prefix = key
		if key == '@' {
			m.state = StateMacroPlayAwait
		} else {
			m.state = StatePrefixG
		}
		return nil

	case BehaviorModeSwitch:
		return m.buildModeSwitchIntent(entry.ModeTarget)

	case BehaviorSpecial:
		return m.buildSpecialIntent(entry.Special)

	case BehaviorSystem:
		return m.buildSystemIntent(entry.IntentType)

	case BehaviorAction:
		return m.buildActionIntent(entry.IntentType)
	}

	return nil
}

func (m *Machine) completeCharMotion(char rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, char)
	count := m.effectiveCount()
	motion := m.charMotion
	cmd := m.captureCommand()
	m.Reset()

	return &Intent{
		Type:    IntentCharMotion,
		Motion:  motion,
		Count:   count,
		Char:    char,
		Command: cmd,
	}
}

func (m *Machine) processOperatorWait(key rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, key)

	// CountEntities after operator
	if key >= '1' && key <= '9' {
		m.accumulateCount2(key)
		return nil
	}
	if key == '0' && m.count2 > 0 {
		m.accumulateCount2(key)
		return nil
	}

	// Doubled operator (dd)
	if key == 'd' && m.operator == OperatorDelete {
		count := m.effectiveCount()
		cmd := m.captureCommand()
		m.Reset()
		return &Intent{
			Type:     IntentOperatorLine,
			Operator: OperatorDelete,
			Count:    count,
			Command:  cmd,
		}
	}

	// g prefix after operator
	if key == 'g' {
		m.prefix = 'g'
		m.state = StateOperatorPrefixG
		return nil
	}

	entry, ok := m.keyTable.OperatorMotions[key]
	if !ok {
		m.Reset()
		return nil
	}

	if entry.Behavior == BehaviorCharWait {
		m.charMotion = entry.Motion
		m.state = StateOperatorCharWait
		return nil
	}

	if entry.Behavior == BehaviorPrefix {
		m.prefix = key
		m.state = StateOperatorPrefixG
		return nil
	}

	// Standard motion after operator
	count := m.effectiveCount()
	operator := m.operator
	cmd := m.captureCommand()
	m.Reset()

	return &Intent{
		Type:     IntentOperatorMotion,
		Operator: operator,
		Motion:   entry.Motion,
		Count:    count,
		Command:  cmd,
	}
}

func (m *Machine) completeOperatorCharMotion(char rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, char)
	count := m.effectiveCount()
	motion := m.charMotion
	operator := m.operator
	cmd := m.captureCommand()
	m.Reset()

	return &Intent{
		Type:     IntentOperatorCharMotion,
		Operator: operator,
		Motion:   motion,
		Count:    count,
		Char:     char,
		Command:  cmd,
	}
}

func (m *Machine) processPrefixG(key rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, key)

	entry, ok := m.keyTable.PrefixG[key]
	if !ok {
		m.Reset()
		return nil
	}

	if entry.Behavior == BehaviorMarkerStart {
		m.markerDirection = entry.Motion
		m.state = StateMarkerAwaitColor
		return &Intent{
			Type:    IntentMotionMarkerShow,
			Motion:  entry.Motion,
			Command: m.captureCommand(),
		}
	}

	return m.buildMotionIntent(entry.Motion)
}

func (m *Machine) processOperatorPrefixG(key rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, key)

	entry, ok := m.keyTable.PrefixG[key]
	if !ok {
		m.Reset()
		return nil
	}

	count := m.effectiveCount()
	operator := m.operator
	cmd := m.captureCommand()
	m.Reset()

	return &Intent{
		Type:     IntentOperatorMotion,
		Operator: operator,
		Motion:   entry.Motion,
		Count:    count,
		Command:  cmd,
	}
}

func (m *Machine) processMarkerAwaitColor(key rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, key)

	// Direction repeat (gll, ghh, etc.) - jump to first glyph any color
	directionKey := m.motionToDirectionKey(m.markerDirection)
	if key == directionKey {
		motion := m.markerDirection
		count := m.effectiveCount()
		cmd := m.captureCommand()
		m.Reset()
		return &Intent{
			Type:    IntentMotionMarkerJump,
			Motion:  motion,
			Char:    0, // 0 = any color
			Count:   count,
			Command: cmd,
		}
	}

	// Color selection (r/g/b)
	if key == 'r' || key == 'g' || key == 'b' {
		motion := m.markerDirection
		count := m.effectiveCount()
		cmd := m.captureCommand()
		m.Reset()
		return &Intent{
			Type:    IntentMotionMarkerJump,
			Motion:  motion,
			Char:    key, // 'r', 'g', 'b' - resolved to GlyphType in router
			Count:   count,
			Command: cmd,
		}
	}

	// Any other key cancels
	m.Reset()
	return nil
}

func (m *Machine) processMacroRecordAwait(key rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, key)

	// q@ -> stop all macros (new: accessible from record-await too)
	if key == '@' {
		cmd := m.captureCommand()
		m.Reset()
		return &Intent{
			Type:    IntentMacroStopAll,
			Command: cmd,
		}
	}

	if key >= 'a' && key <= 'z' {
		cmd := m.captureCommand()
		m.Reset()
		return &Intent{
			Type:    IntentMacroRecordStart,
			Char:    key,
			Command: cmd,
		}
	}

	// Invalid label cancels
	m.Reset()
	return nil
}

func (m *Machine) processMacroPlayAwait(key rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, key)

	// @@ -> infinite mode
	if key == '@' {
		m.state = StateMacroInfiniteAwait
		return nil
	}

	// @<label> -> play with count
	if key >= 'a' && key <= 'z' {
		count := m.effectiveCount()
		cmd := m.captureCommand()
		m.Reset()
		return &Intent{
			Type:    IntentMacroPlay,
			Char:    key,
			Count:   count,
			Command: cmd,
		}
	}

	m.Reset()
	return nil
}

func (m *Machine) processMacroInfiniteAwait(key rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, key)

	// @@@ -> play all macros infinitely
	if key == '@' {
		cmd := m.captureCommand()
		m.Reset()
		return &Intent{
			Type:    IntentMacroPlayAll,
			Command: cmd,
		}
	}

	// @@ -> play macro infinitely
	if key >= 'a' && key <= 'z' {
		cmd := m.captureCommand()
		m.Reset()
		return &Intent{
			Type:    IntentMacroPlayInfinite,
			Char:    key,
			Command: cmd,
		}
	}

	m.Reset()
	return nil
}

func (m *Machine) processMacroStopAwait(key rune) *Intent {
	m.cmdBuffer = append(m.cmdBuffer, key)

	// q@ -> stop all
	if key == '@' {
		cmd := m.captureCommand()
		m.Reset()
		return &Intent{
			Type:    IntentMacroStopAll,
			Command: cmd,
		}
	}

	// q<label> -> stop specific
	if key >= 'a' && key <= 'z' {
		cmd := m.captureCommand()
		m.Reset()
		return &Intent{
			Type:    IntentMacroStopOne,
			Char:    key,
			Command: cmd,
		}
	}

	m.Reset()
	return nil
}

func (m *Machine) motionToDirectionKey(motion MotionOp) rune {
	switch motion {
	case MotionColoredGlyphRight:
		return 'l'
	case MotionColoredGlyphLeft:
		return 'h'
	case MotionColoredGlyphUp:
		return 'k'
	case MotionColoredGlyphDown:
		return 'j'
	}
	return 0
}

// === Insert Mode Processing ===

func (m *Machine) processInsert(ev terminal.Event) *Intent {
	// Check navigation/system keys first
	if ev.Key != terminal.KeyRune {
		// Macro stop (Ctrl+@) works in all modes
		if ev.Key == terminal.KeyCtrlSpace {
			return &Intent{Type: IntentMacroStopAll}
		}

		// Insert-mode game actions (Tab, Enter) take precedence
		switch ev.Key {
		case terminal.KeyTab:
			return &Intent{Type: IntentNuggetJump}
		case terminal.KeyBacktab:
			return &Intent{Type: IntentGoldJump}
		case terminal.KeyEnter:
			return &Intent{Type: IntentFireMain}
		}

		// Standard text navigation/system keys
		if entry, ok := m.keyTable.TextNavKeys[ev.Key]; ok {
			return m.handleTextModeEntry(entry)
		}
		return nil
	}

	// Space deletes character and moves right
	if ev.Rune == ' ' {
		return &Intent{Type: IntentInsertDeleteForward}
	}

	// Printable character
	return &Intent{
		Type: IntentTextChar,
		Char: ev.Rune,
	}
}

// === Search Mode Processing ===

func (m *Machine) processSearch(ev terminal.Event) *Intent {
	// Check system/nav keys first
	if ev.Key != terminal.KeyRune {
		// Macro stop (Ctrl+@) works in all modes
		if ev.Key == terminal.KeyCtrlSpace {
			return &Intent{Type: IntentMacroStopAll}
		}

		if entry, ok := m.keyTable.TextNavKeys[ev.Key]; ok {
			return m.handleTextModeEntry(entry)
		}
		return nil
	}

	// Printable character for search text
	return &Intent{
		Type: IntentTextChar,
		Char: ev.Rune,
	}
}

// === Command Mode Processing ===

func (m *Machine) processCommand(ev terminal.Event) *Intent {
	// Check system/nav keys first
	if ev.Key != terminal.KeyRune {
		// Macro stop (Ctrl+@) works in all modes
		if ev.Key == terminal.KeyCtrlSpace {
			return &Intent{Type: IntentMacroStopAll}
		}

		if entry, ok := m.keyTable.TextNavKeys[ev.Key]; ok {
			return m.handleTextModeEntry(entry)
		}
		return nil
	}

	// Printable character for command text
	return &Intent{
		Type: IntentTextChar,
		Char: ev.Rune,
	}
}

// === Overlay Mode Processing ===

func (m *Machine) processOverlay(ev terminal.Event) *Intent {
	// Handle special keys
	if ev.Key != terminal.KeyRune {
		if entry, ok := m.keyTable.OverlayKeys[ev.Key]; ok {
			return m.handleOverlayEntry(entry)
		}
		return nil
	}

	// Handle rune keys
	if entry, ok := m.keyTable.OverlayRunes[ev.Rune]; ok {
		return m.handleOverlayEntry(entry)
	}

	return nil
}

func (m *Machine) handleOverlayEntry(entry KeyEntry) *Intent {
	switch entry.Behavior {
	case BehaviorMotion:
		dir := ScrollDown
		if entry.Motion == MotionUp {
			dir = ScrollUp
		}
		return &Intent{
			Type:      IntentOverlayScroll,
			ScrollDir: dir,
		}
	case BehaviorSystem:
		return &Intent{Type: entry.IntentType}
	}
	return nil
}

func (m *Machine) handleTextModeEntry(entry KeyEntry) *Intent {
	switch entry.Behavior {
	case BehaviorMotion:
		return &Intent{
			Type:   IntentTextNav,
			Motion: entry.Motion,
			Count:  1,
		}
	case BehaviorSystem:
		return &Intent{Type: entry.IntentType}
	}
	return nil
}

// === Helper Methods ===

func (m *Machine) buildMotionIntent(motion MotionOp) *Intent {
	count := m.effectiveCount()
	cmd := m.captureCommand()
	m.Reset()

	return &Intent{
		Type:    IntentMotion,
		Motion:  motion,
		Count:   count,
		Command: cmd,
	}
}

func (m *Machine) buildModeSwitchIntent(target ModeTarget) *Intent {
	m.Reset()
	return &Intent{
		Type:       IntentModeSwitch,
		ModeTarget: target,
	}
}

func (m *Machine) buildSpecialIntent(special SpecialOp) *Intent {
	count := m.effectiveCount()
	cmd := m.captureCommand()
	m.Reset()

	return &Intent{
		Type:    IntentSpecial,
		Special: special,
		Count:   count,
		Command: cmd,
	}
}

func (m *Machine) buildSystemIntent(intentType IntentType) *Intent {
	wasPartial := m.state != StateIdle
	m.Reset()

	// ESC in Normal mode mid-sequence: silent cancel
	if intentType == IntentEscape && m.mode == ModeNormal && wasPartial {
		return nil
	}
	return &Intent{Type: intentType}
}

func (m *Machine) buildActionIntent(intentType IntentType) *Intent {
	m.Reset()
	return &Intent{Type: intentType}
}

func (m *Machine) effectiveCount() int {
	c1, c2 := m.count1, m.count2
	if c1 == 0 {
		c1 = 1
	}
	if c2 == 0 {
		c2 = 1
	}
	return c1 * c2
}

func (m *Machine) captureCommand() string {
	return string(m.cmdBuffer)
}

func (m *Machine) accumulateCount1(key rune) {
	m.count1 = m.count1*10 + int(key-'0')
	if m.count1 > 9999 {
		m.count1 = 9999
	}
}

func (m *Machine) accumulateCount2(key rune) {
	m.count2 = m.count2*10 + int(key-'0')
	if m.count2 > 9999 {
		m.count2 = 9999
	}
}