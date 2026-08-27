package app

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"reflect"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
	"github.com/lixenwraith/vi-fighter/internal/fsm/std"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/service"
)

// schemaVersion is the FSM schema contract version consumed by the map editor
const schemaVersion = 1

// Check validates the resolved FSM config and content corpus without starting the game
func Check(cfg Config, w io.Writer) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	event.EnsureRegistry()

	if err := checkFSM(cfg, w); err != nil {
		return err
	}
	return checkContent(cfg, w)
}

// checkFSM loads the resolved FSM config and reports its source
func checkFSM(cfg Config, w io.Writer) error {
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
		return checkSystems(m, w)
	}
	if err := fsm.LoadConfigFromPath(m, path); err != nil {
		return err
	}
	fmt.Fprintln(w, "config ok:", path)
	return checkSystems(m, w)
}

// checkSystems validates every system name the config references, then every
// required dependency the resulting system set would leave unsatisfied
func checkSystems(m *fsm.Machine[*engine.World], w io.Writer) error {
	profiles := manifest.SystemProfiles()
	valid := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		valid[p.Name] = true
	}

	var unknown []string
	check := func(where string, names []string) {
		for _, n := range names {
			if !valid[n] {
				unknown = append(unknown, where+": "+n)
			}
		}
	}

	var globalDisabled []string
	if sc := m.GetSystemsConfig(); sc != nil {
		globalDisabled = sc.DisabledSystems
		check("[systems]", globalDisabled)
	}
	for _, r := range m.DeclaredRegions() {
		cfg := m.GetRegionConfig(r)
		if cfg == nil {
			continue
		}
		check("region "+r, cfg.EnabledSystems)
		check("region "+r, cfg.DisabledSystems)
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown system names:\n  %s", strings.Join(unknown, "\n  "))
	}

	if err := checkSystemDependencies(m, profiles, globalDisabled); err != nil {
		return err
	}
	fmt.Fprintln(w, "systems ok")
	return nil
}

// checkSystemDependencies reports every enabled system whose required
// dependency the config disables. Each region is evaluated against the global
// baseline alone, which is the set ApplyRegionSystemConfigs leaves behind when
// that region spawns or resumes.
func checkSystemDependencies(m *fsm.Machine[*engine.World], profiles []manifest.SystemProfile,
	globalDisabled []string) error {

	base := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		base[p.Name] = true
	}
	for _, n := range globalDisabled {
		base[n] = false
	}

	var broken []string
	collect := func(where string, enabled map[string]bool) {
		for _, p := range profiles {
			if !enabled[p.Name] {
				continue
			}
			for _, dep := range p.Requires {
				if dep.Strength == engine.DepRequired && !enabled[dep.Name] {
					broken = append(broken, fmt.Sprintf("%s: %s requires %s", where, p.Name, dep.Name))
				}
			}
		}
	}

	collect("[systems]", base)
	for _, r := range m.DeclaredRegions() {
		cfg := m.GetRegionConfig(r)
		if cfg == nil {
			continue
		}
		enabled := maps.Clone(base)
		for _, n := range cfg.DisabledSystems {
			enabled[n] = false
		}
		for _, n := range cfg.EnabledSystems {
			enabled[n] = true
		}
		collect("region "+r, enabled)
	}

	if len(broken) > 0 {
		return fmt.Errorf("required systems disabled:\n  %s\n"+
			"Enable the dependency, or disable the system that requires it.",
			strings.Join(broken, "\n  "))
	}
	return nil
}

// checkContent loads the corpus and reports accepted and rejected files
func checkContent(cfg Config, w io.Writer) error {
	src, err := ResolveContent(cfg)
	if err != nil {
		return fmt.Errorf("content path: %w", err)
	}

	svc := service.NewContentService(src, 0) // validation only; block order is irrelevant
	if err := svc.Init(); err != nil {
		return err
	}

	c := svc.Corpus()
	fmt.Fprintf(w, "content ok: %s (%d files, %d blocks, %d lines)\n",
		svc.Label(), len(c.Sources), c.BlockCount(), c.LineCount())

	for _, s := range c.Sources {
		fmt.Fprintf(w, "  ok    %-32s %4d blocks %6d lines\n", s.Name, len(s.Blocks), s.Lines)
	}
	for _, r := range c.Rejected {
		fmt.Fprintf(w, "  skip  %-32s %s\n", r.Name, r.Reason)
	}
	return nil
}

// Schema writes the machine schema as JSON for the map editor
// Requires no terminal, services, or World instance
func Schema(w io.Writer) error {
	event.EnsureRegistry()

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
