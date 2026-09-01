package journal

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
)

// ScriptSchema is the authored deterministic-run format version.
const ScriptSchema uint64 = 1

// Script is a bounded caller-driven run. Ticks is the number of simulation ticks
// to execute; actions run at the named completed (run, tick) position before the
// next tick. Width and Height are optional terminal-equivalent headless geometry.
type Script struct {
	Schema  uint64         `toml:"schema"`
	Ticks   uint64         `toml:"ticks"`
	Width   int            `toml:"width,omitempty"`
	Height  int            `toml:"height,omitempty"`
	Actions []ScriptAction `toml:"action"`
}

// ScriptAction expresses exactly one input. Intent names a canonical keymap
// action, Text emits semantic text-character intents, Command performs a complete
// ex-command round trip, and Event injects a typed event with OriginDebug.
type ScriptAction struct {
	Run     uint64 `toml:"run,omitempty"`
	Tick    uint64 `toml:"tick"`
	Intent  string `toml:"intent,omitempty"`
	Text    string `toml:"text,omitempty"`
	Command string `toml:"command,omitempty"`
	Event   string `toml:"event,omitempty"`
	Payload string `toml:"payload,omitempty"`
	Domain  string `toml:"domain,omitempty"`
	Count   int    `toml:"count,omitempty"`
	Char    string `toml:"char,omitempty"`
}

// LoadScript parses and validates one authored TOML script.
func LoadScript(path string) (Script, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Script{}, err
	}
	s, err := ParseScript(b)
	if err != nil {
		return Script{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// ParseScript parses and validates authored TOML bytes.
func ParseScript(b []byte) (Script, error) {
	raw, err := toml.NewParser(b).Parse()
	if err != nil {
		return Script{}, fmt.Errorf("script decode: %w", err)
	}
	if err := validateScriptKeys(raw); err != nil {
		return Script{}, err
	}
	var s Script
	if err := toml.Decode(raw, &s); err != nil {
		return Script{}, fmt.Errorf("script decode: %w", err)
	}
	if _, err := compileScript(s); err != nil {
		return Script{}, err
	}
	return s, nil
}

func validateScriptKeys(raw map[string]any) error {
	if key := firstUnknownKey(raw, map[string]bool{
		"schema": true, "ticks": true, "width": true, "height": true, "action": true,
	}); key != "" {
		return fmt.Errorf("unknown script field %q", key)
	}
	actions, ok := raw["action"]
	if !ok {
		return nil
	}
	list, ok := actions.([]map[string]any)
	if !ok {
		return errors.New("script action must be an array of tables")
	}
	allowed := map[string]bool{
		"run": true, "tick": true, "intent": true, "text": true, "command": true,
		"event": true, "payload": true, "domain": true, "count": true, "char": true,
	}
	for i, action := range list {
		if key := firstUnknownKey(action, allowed); key != "" {
			return fmt.Errorf("action %d: unknown field %q", i+1, key)
		}
	}
	return nil
}

func firstUnknownKey(values map[string]any, allowed map[string]bool) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if !allowed[key] {
			return key
		}
	}
	return ""
}

type scriptActionKind uint8

const (
	scriptIntent scriptActionKind = iota + 1
	scriptText
	scriptCommand
	scriptEvent
)

type compiledScriptAction struct {
	run, tick uint64
	index     int
	kind      scriptActionKind
	intent    input.Intent
	text      string
	eventType event.EventType
	payload   string
	domain    core.Domain
}

func compileScript(s Script) ([]compiledScriptAction, error) {
	event.EnsureRegistry()
	if s.Schema != ScriptSchema {
		return nil, fmt.Errorf("script schema %d, this build reads %d", s.Schema, ScriptSchema)
	}
	if s.Ticks == 0 {
		return nil, errors.New("script ticks must be greater than zero")
	}
	if (s.Width == 0) != (s.Height == 0) || s.Width < 0 || s.Height < 0 {
		return nil, errors.New("script width and height must both be positive or both be omitted")
	}

	out := make([]compiledScriptAction, 0, len(s.Actions))
	for i, spec := range s.Actions {
		a, err := compileScriptAction(i, spec)
		if err != nil {
			return nil, fmt.Errorf("action %d at run %d tick %d: %w", i+1, spec.Run, spec.Tick, err)
		}
		out = append(out, a)
	}
	slices.SortStableFunc(out, func(a, b compiledScriptAction) int {
		if a.run != b.run {
			if a.run < b.run {
				return -1
			}
			return 1
		}
		if a.tick < b.tick {
			return -1
		}
		if a.tick > b.tick {
			return 1
		}
		return 0
	})
	return out, nil
}

func compileScriptAction(index int, spec ScriptAction) (compiledScriptAction, error) {
	a := compiledScriptAction{run: spec.Run, tick: spec.Tick, index: index + 1}
	kinds := 0
	for _, set := range []bool{spec.Intent != "", spec.Text != "", spec.Command != "", spec.Event != ""} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		return a, errors.New("set exactly one of intent, text, command, or event")
	}
	if spec.Count < 0 {
		return a, errors.New("count must not be negative")
	}
	if spec.Count != 0 && spec.Intent == "" {
		return a, errors.New("count applies only to an intent")
	}
	if spec.Payload != "" && spec.Event == "" {
		return a, errors.New("payload applies only to an event")
	}
	if spec.Domain != "" && spec.Event == "" {
		return a, errors.New("domain applies only to an event")
	}
	if spec.Char != "" && spec.Intent == "" {
		return a, errors.New("char applies only to an intent")
	}

	switch {
	case spec.Intent != "":
		intent, err := intentForAction(spec.Intent, spec.Count, spec.Char)
		if err != nil {
			return a, err
		}
		a.kind, a.intent = scriptIntent, intent
	case spec.Text != "":
		a.kind, a.text = scriptText, spec.Text
	case spec.Command != "":
		cmd := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(spec.Command), ":"))
		if cmd == "" {
			return a, errors.New("command must not be empty")
		}
		a.kind, a.text = scriptCommand, cmd
	case spec.Event != "":
		name := spec.Event
		if !strings.HasPrefix(name, "Event") {
			name = "Event" + name
		}
		et, ok := event.GetEventType(name)
		if !ok || et == event.EventNone {
			return a, fmt.Errorf("unknown event %q", name)
		}
		domain, err := scriptEventDomain(et, spec.Domain)
		if err != nil {
			return a, err
		}
		if _, err := DecodePayload(et, spec.Payload); err != nil {
			return a, fmt.Errorf("%s payload: %w", name, err)
		}
		a.kind, a.eventType, a.payload, a.domain = scriptEvent, et, spec.Payload, domain
	}
	return a, nil
}

func intentForAction(name string, count int, char string) (input.Intent, error) {
	entry, ok := input.ActionEntry(name)
	if !ok || name == "none" {
		return input.Intent{}, fmt.Errorf("unknown intent action %q", name)
	}
	if char != "" && entry.Behavior != input.BehaviorCharWait {
		return input.Intent{}, fmt.Errorf("char applies only to a char-wait intent")
	}
	if count == 0 {
		count = 1
	}
	intent := input.Intent{Count: count, Command: name}
	switch entry.Behavior {
	case input.BehaviorMotion:
		intent.Type, intent.Motion = input.IntentMotion, entry.Motion
	case input.BehaviorCharWait:
		if utf8.RuneCountInString(char) != 1 {
			return input.Intent{}, fmt.Errorf("intent %q requires one char", name)
		}
		intent.Type, intent.Motion = input.IntentCharMotion, entry.Motion
		intent.Char, _ = utf8.DecodeRuneInString(char)
	case input.BehaviorModeSwitch:
		intent.Type, intent.ModeTarget = input.IntentModeSwitch, entry.ModeTarget
	case input.BehaviorSpecial:
		intent.Type, intent.Special = input.IntentSpecial, entry.Special
	case input.BehaviorSystem, input.BehaviorAction:
		intent.Type = entry.IntentType
	default:
		return input.Intent{}, fmt.Errorf("intent action %q is a parser prefix; use command or a complete semantic action", name)
	}
	return intent, nil
}

func scriptEventDomain(et event.EventType, text string) (core.Domain, error) {
	if text != "" {
		if d, ok := core.ParseDomain(strings.ToLower(text)); ok {
			return d, nil
		}
		return 0, fmt.Errorf("unknown event domain %q", text)
	}
	switch event.ClassOf(et) {
	case event.ClassLocal, event.ClassBus:
		return core.DomainPlayer, nil
	case event.ClassShared:
		return core.DomainShared, nil
	case event.ClassStamped:
		return 0, fmt.Errorf("stamped event %s requires domain = %q or %q",
			event.GetEventName(et), core.DomainShared, core.DomainPlayer)
	default:
		return 0, fmt.Errorf("event %s has no replication class", event.GetEventName(et))
	}
}

// ScriptTarget is the App-independent surface an authored script drives.
type ScriptTarget interface {
	Position() event.Stamp
	Tick(int)
	Inject(...*input.Intent) bool
	Emit(event.EventType, any, core.Domain)
}

// ScriptStats reports one authored run's progress.
type ScriptStats struct {
	Actions  int
	Executed int
	Ticks    uint64
	End      event.Stamp
}

// ScriptDriver executes actions at explicit simulation positions. Same-position
// actions retain file order and settle independently through the target.
type ScriptDriver struct {
	target  ScriptTarget
	script  Script
	actions []compiledScriptAction
	next    int
	ticks   uint64
	done    bool
}

// NewScriptDriver validates and binds an authored script.
func NewScriptDriver(target ScriptTarget, script Script) (*ScriptDriver, error) {
	actions, err := compileScript(script)
	if err != nil {
		return nil, err
	}
	return &ScriptDriver{target: target, script: script, actions: actions}, nil
}

// Stats returns current progress and the target's live position.
func (d *ScriptDriver) Stats() ScriptStats {
	return ScriptStats{
		Actions: len(d.actions), Executed: d.next, Ticks: d.ticks, End: d.target.Position(),
	}
}

// Step applies every action at the current position, then advances one tick. A
// final call after the tick budget applies actions scheduled exactly at the end.
func (d *ScriptDriver) Step() (bool, error) {
	if d.done {
		return false, nil
	}
	if err := d.applyCurrent(); err != nil {
		return false, err
	}
	if d.ticks == d.script.Ticks {
		d.done = true
		if d.next != len(d.actions) {
			a := d.actions[d.next]
			return false, fmt.Errorf("script ended at run %d tick %d before action %d at run %d tick %d",
				d.target.Position().Run, d.target.Position().Tick, a.index, a.run, a.tick)
		}
		return false, nil
	}
	d.target.Tick(1)
	d.ticks++
	return true, nil
}

// RunAll consumes the whole script.
func (d *ScriptDriver) RunAll() error {
	for {
		more, err := d.Step()
		if err != nil || !more {
			return err
		}
	}
}

func (d *ScriptDriver) applyCurrent() error {
	pos := d.target.Position()
	for d.next < len(d.actions) {
		a := d.actions[d.next]
		cmp := compareRunTick(a.run, a.tick, pos.Run, pos.Tick)
		if cmp < 0 {
			return fmt.Errorf("script passed action %d at run %d tick %d; target is at run %d tick %d",
				a.index, a.run, a.tick, pos.Run, pos.Tick)
		}
		if cmp > 0 {
			return nil
		}
		if err := d.execute(a); err != nil {
			return fmt.Errorf("action %d at run %d tick %d: %w", a.index, a.run, a.tick, err)
		}
		d.next++
	}
	return nil
}

func compareRunTick(ar, at, br, bt uint64) int {
	if ar < br || ar == br && at < bt {
		return -1
	}
	if ar > br || ar == br && at > bt {
		return 1
	}
	return 0
}

func (d *ScriptDriver) execute(a compiledScriptAction) error {
	switch a.kind {
	case scriptIntent:
		intent := a.intent
		if !d.target.Inject(&intent) {
			return errors.New("intent quit the game")
		}
	case scriptText:
		for _, char := range a.text {
			if !d.target.Inject(&input.Intent{Type: input.IntentTextChar, Char: char, Count: 1}) {
				return errors.New("text intent quit the game")
			}
		}
	case scriptCommand:
		if err := d.executeCommand(a.text); err != nil {
			return err
		}
	case scriptEvent:
		payload, err := DecodePayload(a.eventType, a.payload)
		if err != nil {
			return err
		}
		d.target.Emit(a.eventType, payload, a.domain)
	default:
		return errors.New("unknown script action")
	}
	return nil
}

func (d *ScriptDriver) executeCommand(command string) error {
	if !d.target.Inject(&input.Intent{Type: input.IntentModeSwitch, ModeTarget: input.ModeTargetCommand, Count: 1}) {
		return errors.New("command mode quit the game")
	}
	for _, char := range command {
		if !d.target.Inject(&input.Intent{Type: input.IntentTextChar, Char: char, Count: 1}) {
			return errors.New("command text quit the game")
		}
	}
	if !d.target.Inject(&input.Intent{Type: input.IntentTextConfirm, Count: 1}) {
		return errors.New("command confirm quit the game")
	}
	return nil
}
