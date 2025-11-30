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
	StateOperatorCharWait
	StatePrefixG
	StateOperatorPrefixG
)

// InputMachine encapsulates all normal-mode input state
type InputMachine struct {
	state      InputState
	count1     int
	count2     int
	operator   rune
	charMotion CharMotionFunc
	charCmd    rune
	prefix     rune
	cmdBuffer  []rune
}

// ProcessResult contains the outcome of processing a key
type ProcessResult struct {
	Handled       bool
	Continue      bool
	ModeChange    engine.GameMode
	Action        func(*engine.GameContext)
	CommandString string
}

// NewInputMachine creates a new input machine in idle state
func NewInputMachine() *InputMachine {
	return &InputMachine{
		state:     StateIdle,
		cmdBuffer: make([]rune, 0, 8),
	}
}

// Reset clears all state
func (m *InputMachine) Reset() {
	m.state = StateIdle
	m.count1 = 0
	m.count2 = 0
	m.operator = 0
	m.charMotion = nil
	m.charCmd = 0
	m.prefix = 0
	m.cmdBuffer = m.cmdBuffer[:0]
}

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
func (m *InputMachine) Process(key rune, bindings *BindingTable) ProcessResult {
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

func (m *InputMachine) processIdleOrCount(key rune, bindings *BindingTable) ProcessResult {
	// Digit accumulation
	if key >= '1' && key <= '9' {
		m.state = StateCount
		m.accumulateCount1(key)
		return ProcessResult{Handled: true, Continue: true}
	}
	if key == '0' && m.count1 > 0 {
		m.accumulateCount1(key)
		return ProcessResult{Handled: true, Continue: true}
	}

	binding, ok := bindings.normal[key]
	if !ok {
		m.Reset()
		return ProcessResult{Handled: false, Continue: true}
	}

	switch binding.Action {
	case ActionMotion:
		count := m.EffectiveCount()
		cmdStr := m.captureCommand()
		motion := binding.Motion
		target := binding.Target
		m.Reset()
		return ProcessResult{
			Handled:       true,
			Continue:      true,
			CommandString: cmdStr,
			Action: func(ctx *engine.GameContext) {
				pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
				if !ok {
					return
				}
				result := motion(ctx, pos.X, pos.Y, count)
				OpMove(ctx, result, target)
			},
		}

	case ActionCharWait:
		m.charMotion = binding.CharMotion
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
		m.Reset()
		return m.handleModeSwitch(mode)

	case ActionSpecial:
		count := m.EffectiveCount()
		cmdStr := m.captureCommand()
		target := binding.Target
		m.Reset()
		return ProcessResult{
			Handled:       true,
			Continue:      true,
			CommandString: cmdStr,
			Action: func(ctx *engine.GameContext) {
				executeSpecial(ctx, target, count)
			},
		}
	}

	return ProcessResult{Handled: false, Continue: true}
}

func (m *InputMachine) processCharWait(key rune, bindings *BindingTable) ProcessResult {
	count := m.EffectiveCount()
	charMotion := m.charMotion
	charCmd := m.charCmd
	cmdStr := m.captureCommand()
	m.Reset()

	return ProcessResult{
		Handled:       true,
		Continue:      true,
		CommandString: cmdStr,
		Action: func(ctx *engine.GameContext) {
			pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
			if !ok {
				return
			}
			result := charMotion(ctx, pos.X, pos.Y, count, key)
			OpMove(ctx, result, charCmd)

			// State tracking for ; and ,
			if result.Valid {
				ctx.LastFindChar = key
				ctx.LastFindType = charCmd
				ctx.LastFindForward = (charCmd == 'f' || charCmd == 't')
			}
		},
	}
}

func (m *InputMachine) processOperatorWait(key rune, bindings *BindingTable) ProcessResult {
	// Count after operator
	if key >= '1' && key <= '9' {
		m.accumulateCount2(key)
		return ProcessResult{Handled: true, Continue: true}
	}
	if key == '0' && m.count2 > 0 {
		m.accumulateCount2(key)
		return ProcessResult{Handled: true, Continue: true}
	}

	// Doubled operator (dd)
	if key == m.operator {
		count := m.EffectiveCount()
		cmdStr := m.captureCommand()
		m.Reset()
		return ProcessResult{
			Handled:       true,
			Continue:      true,
			CommandString: cmdStr,
			Action: func(ctx *engine.GameContext) {
				pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
				if !ok {
					return
				}
				endY := pos.Y + count - 1
				if endY >= ctx.GameHeight {
					endY = ctx.GameHeight - 1
				}
				result := MotionResult{
					StartX: 0, StartY: pos.Y,
					EndX: ctx.GameWidth - 1, EndY: endY,
					Type: RangeLine, Style: StyleInclusive,
					Valid: true,
				}
				if OpDelete(ctx, result) {
					ctx.State.SetHeat(0)
				}
			},
		}
	}

	// g prefix
	if key == 'g' {
		m.prefix = 'g'
		m.state = StateOperatorPrefixG
		return ProcessResult{Handled: true, Continue: true}
	}

	binding, ok := bindings.operatorMotions[key]
	if !ok {
		m.Reset()
		return ProcessResult{Handled: false, Continue: true}
	}

	if binding.Action == ActionCharWait {
		m.charMotion = binding.CharMotion
		m.charCmd = binding.Target
		m.state = StateOperatorCharWait
		return ProcessResult{Handled: true, Continue: true}
	}

	// Standard motion after operator
	count := m.EffectiveCount()
	motion := binding.Motion
	charCmd := binding.Target
	cmdStr := m.captureCommand()
	m.Reset()

	return ProcessResult{
		Handled:       true,
		Continue:      true,
		CommandString: cmdStr,
		Action: func(ctx *engine.GameContext) {
			pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
			if !ok {
				return
			}
			result := motion(ctx, pos.X, pos.Y, count)
			if OpDelete(ctx, result) {
				ctx.State.SetHeat(0)
			}
			// State tracking for find motions
			if result.Valid && (charCmd == 'f' || charCmd == 'F' || charCmd == 't' || charCmd == 'T') {
				ctx.LastFindType = charCmd
				ctx.LastFindForward = (charCmd == 'f' || charCmd == 't')
			}
		},
	}
}

func (m *InputMachine) processOperatorCharWait(key rune, bindings *BindingTable) ProcessResult {
	count := m.EffectiveCount()
	charMotion := m.charMotion
	charCmd := m.charCmd
	cmdStr := m.captureCommand()
	m.Reset()

	return ProcessResult{
		Handled:       true,
		Continue:      true,
		CommandString: cmdStr,
		Action: func(ctx *engine.GameContext) {
			pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
			if !ok {
				return
			}
			result := charMotion(ctx, pos.X, pos.Y, count, key)
			if OpDelete(ctx, result) {
				ctx.State.SetHeat(0)
			}
			// State tracking
			if result.Valid {
				ctx.LastFindChar = key
				ctx.LastFindType = charCmd
				ctx.LastFindForward = (charCmd == 'f' || charCmd == 't')
			}
		},
	}
}

func (m *InputMachine) processPrefixG(key rune, bindings *BindingTable) ProcessResult {
	binding, ok := bindings.prefixG[key]
	if !ok {
		m.Reset()
		return ProcessResult{Handled: false, Continue: true}
	}

	count := m.EffectiveCount()
	motion := binding.Motion
	target := binding.Target
	cmdStr := m.captureCommand()
	m.Reset()

	return ProcessResult{
		Handled:       true,
		Continue:      true,
		CommandString: cmdStr,
		Action: func(ctx *engine.GameContext) {
			pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
			if !ok {
				return
			}
			result := motion(ctx, pos.X, pos.Y, count)
			OpMove(ctx, result, target)
		},
	}
}

func (m *InputMachine) processOperatorPrefixG(key rune, bindings *BindingTable) ProcessResult {
	binding, ok := bindings.prefixG[key]
	if !ok {
		m.Reset()
		return ProcessResult{Handled: false, Continue: true}
	}

	count := m.EffectiveCount()
	motion := binding.Motion
	cmdStr := m.captureCommand()
	m.Reset()

	return ProcessResult{
		Handled:       true,
		Continue:      true,
		CommandString: cmdStr,
		Action: func(ctx *engine.GameContext) {
			pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
			if !ok {
				return
			}
			result := motion(ctx, pos.X, pos.Y, count)
			if OpDelete(ctx, result) {
				ctx.State.SetHeat(0)
			}
		},
	}
}

func (m *InputMachine) accumulateCount1(key rune) {
	m.count1 = m.count1*10 + int(key-'0')
	if m.count1 > 9999 {
		m.count1 = 9999
	}
}

func (m *InputMachine) accumulateCount2(key rune) {
	m.count2 = m.count2*10 + int(key-'0')
	if m.count2 > 9999 {
		m.count2 = 9999
	}
}

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