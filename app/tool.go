package app

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/asset"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/fsm"
	"github.com/lixenwraith/vi-fighter/fsm/std"
	"github.com/lixenwraith/vi-fighter/manifest"
)

// schemaVersion is the FSM schema contract version consumed by the map editor
const schemaVersion = 1

// Check validates the resolved FSM config without starting the game
func Check(cfg Config, w io.Writer) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	event.InitRegistry()

	m := fsm.NewMachine[*engine.World]()
	manifest.RegisterFSMComponents(m)

	path, err := ResolveGameConfig(cfg)
	if err != nil {
		return err
	}
	if path == "" {
		if err := fsm.LoadConfigFromFS(m, asset.DefaultFSMConfig, asset.DefaultFSMEntry); err != nil {
			return err
		}
		fmt.Fprintln(w, "config ok: embedded default")
		return nil
	}
	if err := fsm.LoadConfigFromPath(m, path); err != nil {
		return err
	}
	fmt.Fprintln(w, "config ok:", path)
	return nil
}

// Schema writes the machine schema as JSON for the map editor
// Requires no terminal, services, or World instance
func Schema(w io.Writer) error {
	event.InitRegistry()

	m := fsm.NewMachine[*engine.World]()
	manifest.RegisterFSMComponents(m)

	type field struct {
		Name   string `json:"name"`    // toml tag (authoring name)
		GoName string `json:"go_name"` // reflection fallback name
		Type   string `json:"type"`
	}
	type eventSchema struct {
		Name   string  `json:"name"`
		Fields []field `json:"fields,omitempty"`
	}
	schema := struct {
		SchemaVersion    int           `json:"schema_version"`
		Events           []eventSchema `json:"events"`
		Guards           []string      `json:"guards"`
		Actions          []string      `json:"actions"`
		Ops              []string      `json:"ops"`
		ConfigIntFields  []string      `json:"config_int_fields"`
		ConfigBoolFields []string      `json:"config_bool_fields"`
	}{
		SchemaVersion:    schemaVersion,
		Guards:           m.RegisteredGuards(),
		Actions:          m.RegisteredActions(),
		Ops:              std.Ops(),
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
