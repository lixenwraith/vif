package modes

import (
	"github.com/lixenwraith/vi-fighter/engine"
)

// InputState represents the current state of the input state machine
type InputState uint8

const (
	StateIdle InputState = iota
	StateCount
	StateCharWait
	StateOperatorWait
	StateOperatorCharWait // Operator + char command (e.g., dfa)
	StatePrefixG
	StateOperatorPrefixG
)

// InputMachine encapsulates all normal-mode input state
// Private to InputHandler; GameContext never sees internals
type InputMachine struct {
	state     InputState
	count1    int    // Count before operator
	count2    int    // Count after operator (for d2w)
	operator  rune   // Pending operator ('d')
	charCmd   rune   // Pending char command ('f', 'F', 't', 'T')
	prefix    rune   // Pending prefix ('g')
	cmdBuffer []rune // Buffer for current command keystrokes
}

// ProcessResult contains the outcome of processing a key
type ProcessResult struct {
	Handled       bool
	Continue      bool                      // false = exit game
	ModeChange    engine.GameMode           // 0 = no change
	Action        func(*engine.GameContext) // nil = no action
	CommandString string                    // Captured command string on success
}

// NewInputMachine creates a new input machine in idle state
func NewInputMachine() *InputMachine {
	return &InputMachine{
		state:     StateIdle,
		cmdBuffer: make([]rune, 0, 8),
	}
}

// Reset clears all state (ESC handler)
func (m *InputMachine) Reset() {
	m.state = StateIdle
	m.count1 = 0
	m.count2 = 0
	m.operator = 0
	m.charCmd = 0
	m.prefix = 0
	m.cmdBuffer = m.cmdBuffer[:0]
}

// captureCommand returns the current buffer as string
func (m *InputMachine) captureCommand() string {
	return string(m.cmdBuffer)
}

// EffectiveCount returns the multiplied count (count1 * count2, minimum 1)
func (m *InputMachine) EffectiveCount() int {
	c1, c2 := m.count1, m.count2
	if c1 == 0 {
		c1 = 1
	}
	if c2 == 0 {
		c2 = 1
	}
	return c1 * c2
}

// State returns current state (for debugging)
func (m *InputMachine) State() InputState {
	return m.state
}

// Process handles a single key and returns the result
// All `process*` functions share the signature `(key rune, bindings *BindingTable) ProcessResult`. This:
// - Makes `Process()` dispatch uniform
// - Allows future extension (e.g., configurable cancel keys in char-wait state)
func (m *InputMachine) Process(key rune, bindings *BindingTable) ProcessResult {
	// Append key to buffer
	m.cmdBuffer = append(m.cmdBuffer, key)

	switch m.state {
	case StateIdle, StateCount:
		return m.processIdleOrCount(key, bindings)
	case StateCharWait:
		return m.processCharWait(key, bindings)
	case StateOperatorWait:
		return m.processOperatorWait(key, bindings)
	case StateOperatorCharWait:
		return m.processOperatorCharWait(key, bindings)
	case StatePrefixG:
		return m.processPrefixG(key, bindings)
	case StateOperatorPrefixG:
		return m.processOperatorPrefixG(key, bindings)
	}
	return ProcessResult{Handled: false, Continue: true}
}

// processIdleOrCount handles key input when machine is idle or accumulating count digits
func (m *InputMachine) processIdleOrCount(key rune, bindings *BindingTable) ProcessResult {
	// Handle digit accumulation (1-9 always, 0 only if count started)
	if key >= '1' && key <= '9' {
		m.state = StateCount
		m.accumulateCount1(key)
		return ProcessResult{Handled: true, Continue: true}
	}
	if key == '0' && m.count1 > 0 {
		m.accumulateCount1(key)
		return ProcessResult{Handled: true, Continue: true}
	}

	// Look up binding
	binding, ok := bindings.normal[key]
	if !ok {
		m.Reset()
		return ProcessResult{Handled: false, Continue: true}
	}

	switch binding.Action {
	case ActionMotion:
		count := m.EffectiveCount()
		cmdStr := m.captureCommand()
		m.Reset()
		executor := binding.Executor
		return ProcessResult{
			Handled:       true,
			Continue:      true,
			CommandString: cmdStr,
			Action: func(ctx *engine.GameContext) {
				executor(ctx, count)
			},
		}

	case ActionCharWait:
		m.charCmd = binding.Target
		m.state = StateCharWait
		return ProcessResult{Handled: true, Continue: true}

	case ActionOperator:
		m.operator = binding.Target
		m.state = StateOperatorWait
		return ProcessResult{Handled: true, Continue: true}

	case ActionPrefix:
		m.prefix = binding.Target
		m.state = StatePrefixG
		return ProcessResult{Handled: true, Continue: true}

	case ActionModeSwitch:
		mode := binding.Target
		// Mode switch resets buffer
		m.Reset()
		return m.handleModeSwitch(mode)

	case ActionSpecial:
		count := m.EffectiveCount()
		cmdStr := m.captureCommand()
		m.Reset()
		executor := binding.Executor
		return ProcessResult{
			Handled:       true,
			Continue:      true,
			CommandString: cmdStr,
			Action: func(ctx *engine.GameContext) {
				executor(ctx, count)
			},
		}
	}

	return ProcessResult{Handled: false, Continue: true}
}

// processCharWait handles target character input after f/F/t/T commands
func (m *InputMachine) processCharWait(key rune, bindings *BindingTable) ProcessResult {
	// Any printable character is the target
	count := m.EffectiveCount()
	charCmd := m.charCmd
	cmdStr := m.captureCommand()
	m.Reset()

	return ProcessResult{
		Handled:       true,
		Continue:      true,
		CommandString: cmdStr,
		Action: func(ctx *engine.GameContext) {
			execCharMotion(ctx, charCmd, key, count)
		},
	}
}

// processOperatorWait handles motion or count input after operator (d) is pressed
func (m *InputMachine) processOperatorWait(key rune, bindings *BindingTable) ProcessResult {
	// Handle count after operator (d2w)
	if key >= '1' && key <= '9' {
		m.accumulateCount2(key)
		return ProcessResult{Handled: true, Continue: true}
	}
	if key == '0' && m.count2 > 0 {
		m.accumulateCount2(key)
		return ProcessResult{Handled: true, Continue: true}
	}

	// Check for doubled operator (dd)
	if key == m.operator {
		count := m.EffectiveCount()
		op := m.operator
		cmdStr := m.captureCommand()
		m.Reset()
		return ProcessResult{
			Handled:       true,
			Continue:      true,
			CommandString: cmdStr,
			Action: func(ctx *engine.GameContext) {
				ExecuteDeleteMotion(ctx, op, count)
			},
		}
	}

	// Handle 'g' prefix (dgg, dgo)
	if key == 'g' {
		m.prefix = 'g'
		m.state = StateOperatorPrefixG
		return ProcessResult{Handled: true, Continue: true}
	}

	// Look up motion binding
	binding, ok := bindings.operatorMotions[key]
	if !ok {
		m.Reset()
		return ProcessResult{Handled: false, Continue: true}
	}

	if binding.Action == ActionCharWait {
		// df{char}, dt{char} - transition to operator+char wait
		m.charCmd = binding.Target
		m.state = StateOperatorCharWait
		return ProcessResult{Handled: true, Continue: true}
	}

	// Execute operator + motion
	count := m.EffectiveCount()
	motion := binding.Target
	cmdStr := m.captureCommand()
	m.Reset()
	return ProcessResult{
		Handled:       true,
		Continue:      true,
		CommandString: cmdStr,
		Action: func(ctx *engine.GameContext) {
			ExecuteDeleteMotion(ctx, motion, count)
		},
	}
}

// processOperatorCharWait handles target character input after operator + char command (df, dt, etc)
func (m *InputMachine) processOperatorCharWait(key rune, bindings *BindingTable) ProcessResult {
	// Target character for df{char}, dt{char}, etc
	count := m.EffectiveCount()
	charCmd := m.charCmd
	cmdStr := m.captureCommand()
	m.Reset()

	return ProcessResult{
		Handled:       true,
		Continue:      true,
		CommandString: cmdStr,
		Action: func(ctx *engine.GameContext) {
			execDeleteWithCharMotion(ctx, charCmd, key, count)
		},
	}
}

// processPrefixG handles second character input after g prefix
func (m *InputMachine) processPrefixG(key rune, bindings *BindingTable) ProcessResult {
	binding, ok := bindings.prefixG[key]
	if !ok {
		m.Reset()
		return ProcessResult{Handled: false, Continue: true}
	}

	count := m.EffectiveCount()
	m.Reset()
	executor := binding.Executor
	return ProcessResult{
		Handled:  true,
		Continue: true,
		Action: func(ctx *engine.GameContext) {
			executor(ctx, count)
		},
	}
}

// processOperatorPrefixG handles operator's second character input after g prefix
func (m *InputMachine) processOperatorPrefixG(key rune, bindings *BindingTable) ProcessResult {
	binding, ok := bindings.prefixG[key]
	if !ok {
		m.Reset()
		return ProcessResult{Handled: false, Continue: true}
	}

	count := m.EffectiveCount()
	motion := binding.Target
	cmdStr := m.captureCommand()
	m.Reset()

	return ProcessResult{
		Handled:       true,
		Continue:      true,
		CommandString: cmdStr,
		Action: func(ctx *engine.GameContext) {
			ExecuteDeleteMotion(ctx, motion, count)
		},
	}
}

// accumulateCount1 adds a digit to the primary count (before operator)
func (m *InputMachine) accumulateCount1(key rune) {
	m.count1 = m.count1*10 + int(key-'0')
	if m.count1 > 9999 {
		m.count1 = 9999
	}
}

// accumulateCount2 adds a digit to the secondary count (after operator)
func (m *InputMachine) accumulateCount2(key rune) {
	m.count2 = m.count2*10 + int(key-'0')
	if m.count2 > 9999 {
		m.count2 = 9999
	}
}

// handleModeSwitch changes mode (enum)
func (m *InputMachine) handleModeSwitch(target rune) ProcessResult {
	switch target {
	case 'i':
		return ProcessResult{
			Handled:    true,
			Continue:   true,
			ModeChange: engine.ModeInsert,
		}
	case '/':
		return ProcessResult{
			Handled:    true,
			Continue:   true,
			ModeChange: engine.ModeSearch,
		}
	case ':':
		return ProcessResult{
			Handled:    true,
			Continue:   true,
			ModeChange: engine.ModeCommand,
			Action: func(ctx *engine.GameContext) {
				ctx.SetPaused(true)
			},
		}
	}
	return ProcessResult{Handled: false, Continue: true}
}