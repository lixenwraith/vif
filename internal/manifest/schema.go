package manifest

import (
	"encoding/json"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
	"github.com/lixenwraith/vif/internal/system"
)

// SchemaVersion is the FSM schema contract version -schema prints.
const SchemaVersion = 3

// Schema writes the machine schema as JSON for -schema and authoring tools. It
// needs no terminal, services or World instance.
func Schema(w io.Writer) error {
	event.EnsureRegistry()

	m := fsm.NewMachine[*engine.World]()
	RegisterFSMComponents(m)

	type field struct {
		Name   string `json:"name"`    // toml tag (authoring name)
		GoName string `json:"go_name"` // reflection fallback name
		Type   string `json:"type"`
	}
	type eventSchema struct {
		Name   string  `json:"name"`
		Fields []field `json:"fields,omitempty"`
	}
	// Each vocabulary lists its guards and actions with their args: a scenario's,
	// a bot graph's, which refuses the actions that write the world, and a behavior
	// program's, with its probes, sustained verbs and flags
	schema := struct {
		SchemaVersion    int                        `json:"schema_version"`
		Events           []eventSchema              `json:"events"`
		Vocabularies     map[string][]dialect.Entry `json:"vocabularies"`
		Ops              []string                   `json:"ops"`
		ConfigIntFields  []string                   `json:"config_int_fields"`
		ConfigBoolFields []string                   `json:"config_bool_fields"`
	}{
		SchemaVersion:    SchemaVersion,
		Vocabularies:     map[string][]dialect.Entry{"scenario": m.Vocabulary(), "bot": bot.Vocabulary(), "behavior": system.BehaviorVocabulary().Entries()},
		Ops:              dialect.Ops(),
		ConfigIntFields:  engine.ConfigIntFields(),
		ConfigBoolFields: engine.ConfigBoolFields(),
	}

	event.RangeEvents(func(name string, et event.EventType, payload any) {
		es := eventSchema{Name: name}
		if payload != nil {
			t := reflect.TypeOf(payload)
			if t.Kind() == reflect.Ptr {
				t = t.Elem()
			}
			for f := range t.Fields() {
				tag := f.Tag.Get("toml")
				n := f.Name
				if tag != "" && tag != "-" {
					if idx := strings.Index(tag, ","); idx >= 0 {
						tag = tag[:idx]
					}
					n = tag
				}
				es.Fields = append(es.Fields, field{Name: n, GoName: f.Name, Type: f.Type.String()})
			}
		}
		schema.Events = append(schema.Events, es)
	})
	sort.Slice(schema.Events, func(i, j int) bool { return schema.Events[i].Name < schema.Events[j].Name })

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(schema)
}
