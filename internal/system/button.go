package system

import (
	"time"
	"unicode/utf8"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// ButtonSystem keeps the row of buttons a scenario lays out: it adds them, lights
// them on request and on a press of their input, and plays each one's sound as it
// lights. Whether a press was right is the scenario's call, never the button's.
// An own press is presented by its echo at production, so its applied crossing
// lights the shared button silently; a peer's press sounds as it applies.
type ButtonSystem struct {
	world *engine.World
	toggle
}

func NewButtonSystem(world *engine.World) engine.System {
	s := &ButtonSystem{world: world}
	s.Init()
	return s
}

func (s *ButtonSystem) Init() {
	s.enabled = true
}

func (s *ButtonSystem) Name() string {
	return "button"
}

func (s *ButtonSystem) Priority() int {
	return parameter.PriorityButton
}

func (s *ButtonSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventButtonSpawnRequest,
		event.EventButtonLightRequest,
		event.EventInputPressed,
		event.EventInputReleased,
		event.EventInputPressEcho,
		event.EventInputReleaseEcho,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *ButtonSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventGameResetRequest:
		s.Init()
		return
	case event.EventMetaSystemCommandRequest:
		if p, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && p.SystemName == s.Name() {
			s.enabled = p.Enabled
		}
		return
	}
	if !s.enabled {
		return
	}

	switch p := ev.Payload.(type) {
	case *event.ButtonSpawnRequestPayload:
		s.spawn(p)
	case *event.ButtonLightRequestPayload:
		if e := s.bySlot(p.Button); e != 0 {
			s.light(e, 0, false, time.Duration(p.DurationMs)*time.Millisecond, true)
		}
	case *event.InputPayload:
		key, _ := utf8.DecodeRuneInString(p.Key)
		s.input(ev, p, key)
	}
}

// input applies a press or release to the button on key: the crossing to the shared
// button, the echo to this instance's view alone
func (s *ButtonSystem) input(ev event.GameEvent, p *event.InputPayload, key rune) {
	e := s.byKey(key)
	if e == 0 {
		return
	}
	echo := &s.world.Resources.View.ButtonEcho
	switch ev.Type {
	case event.EventInputPressed:
		s.light(e, p.Entity, p.Source == event.InputPointer, parameter.ButtonPressLight, ev.Origin == event.OriginNetwork)
	case event.EventInputReleased:
		if b, _ := s.world.Components.Button.GetPtr(e); b.PressedBy == p.Entity {
			b.Held = false
		}
	case event.EventInputPressEcho:
		*echo = engine.ButtonEchoState{Key: key, Remaining: parameter.ButtonPressLight, Held: p.Source == event.InputPointer}
		s.sound(e)
	case event.EventInputReleaseEcho:
		if echo.Key == key {
			echo.Held = false
		}
	}
}

// Update ages the echo; a shared button lights on its timer's deadline slot, which
// TimerSystem ages
func (s *ButtonSystem) Update() {
	echo := &s.world.Resources.View.ButtonEcho
	echo.Remaining = max(echo.Remaining-s.world.Resources.Time.DeltaTime, 0)
}

// spawn appends a button; a second button on one key is refused, since a press
// names its button by key and must find the same one on every instance
func (s *ButtonSystem) spawn(p *event.ButtonSpawnRequestPayload) {
	key, size := utf8.DecodeRuneInString(p.Key)
	if size == 0 || size != len(p.Key) || s.byKey(key) != 0 {
		return
	}
	e := s.world.CreateEntity(core.DomainShared)
	s.world.Components.Button.SetComponent(e, component.ButtonComponent{
		Slot:  s.world.Components.Button.CountEntities() + 1,
		Key:   key,
		Color: p.Color,
		Sound: p.Sound,
	})
}

// light lights a button for d, pressed by a cursor or by the scenario when by is zero
func (s *ButtonSystem) light(e, by core.Entity, held bool, d time.Duration, audible bool) {
	b, ok := s.world.Components.Button.GetPtr(e)
	if !ok || d <= 0 {
		return
	}
	b.PressedBy, b.Held = by, held
	armTimer(s.world, e, component.TimerDeadline, d)
	if audible {
		s.sound(e)
	}
}

// sound plays a button's sound on this instance alone
func (s *ButtonSystem) sound(e core.Entity) {
	if b, ok := s.world.Components.Button.GetComponent(e); ok && b.Sound != "" {
		s.world.PushLocal(event.EventSoundRequest, &event.SoundRequestPayload{Name: b.Sound})
	}
}

func (s *ButtonSystem) byKey(key rune) core.Entity {
	return s.find(func(b *component.ButtonComponent) bool { return b.Key == key })
}

func (s *ButtonSystem) bySlot(slot int) core.Entity {
	return s.find(func(b *component.ButtonComponent) bool { return b.Slot == slot })
}

func (s *ButtonSystem) find(match func(*component.ButtonComponent) bool) core.Entity {
	var found core.Entity
	s.world.Components.Button.Each(func(e core.Entity, b *component.ButtonComponent) bool {
		if match(b) {
			found = e
			return false
		}
		return true
	})
	return found
}
