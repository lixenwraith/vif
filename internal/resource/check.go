package resource

import (
	"fmt"
	"io"
	"maps"
	"os"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/service"
)

// Check validates every resolved external resource without starting the game.
func Check(o Options, w io.Writer) error {
	if err := o.Validate(); err != nil {
		return err
	}
	event.EnsureRegistry()

	if err := checkScenario(o, w); err != nil {
		return err
	}
	if err := checkKeymap(o, w); err != nil {
		return err
	}
	if err := checkAudio(o, w); err != nil {
		return err
	}
	return checkContent(o, w)
}

func checkKeymap(o Options, w io.Writer) error {
	path, err := Keymap(o)
	if err != nil {
		return err
	}
	if path == "" {
		_ = input.DefaultKeyTable() // parse and validate the embedded document
		fmt.Fprintln(w, "keymap ok: embedded default")
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("keymap %s: %w", path, err)
	}
	if _, err := input.LoadKeyConfig(data); err != nil {
		return fmt.Errorf("keymap %s: %w", path, err)
	}
	fmt.Fprintln(w, "keymap ok:", path)
	return nil
}

// ValidateScenario resolves and reads a scenario, loads it into a throwaway
// machine, and checks every system it names against this build. It is what
// `-check` proves before a run starts and what an operator changing scenario
// mid-run proves before the run it would replace is torn down.
func ValidateScenario(o Options) (Scenario, error) {
	sc, err := LoadScenario(o)
	if err != nil {
		return Scenario{}, err
	}
	m := fsm.NewMachine[*engine.World]()
	manifest.RegisterFSMComponents(m)
	if err := fsm.LoadScenarioFromFS(m, sc.FS(), sc.Entry()); err != nil {
		return Scenario{}, err
	}
	if err := checkSystems(m); err != nil {
		return Scenario{}, err
	}
	return sc, nil
}

// checkScenario reports what a peer would compare: the name, the digest, and
// where it came from.
func checkScenario(o Options, w io.Writer) error {
	sc, err := ValidateScenario(o)
	if err != nil {
		return err
	}
	source, err := ScenarioPath(o)
	if err != nil {
		return err
	}
	if source == "" {
		source = "embedded default"
	}
	fmt.Fprintf(w, "scenario ok: %s (%s, %d files, sha256:%s)\n",
		source, sc.Name, sc.Files(), sc.Short())
	fmt.Fprintln(w, "systems ok")
	return nil
}

// checkSystems validates every system name the config references, then every
// required dependency the resulting system set would leave unsatisfied
func checkSystems(m *fsm.Machine[*engine.World]) error {
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

	return checkSystemDependencies(m, profiles, globalDisabled)
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
func checkContent(o Options, w io.Writer) error {
	src, err := Corpus(o)
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
