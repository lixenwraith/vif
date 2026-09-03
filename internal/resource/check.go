package resource

import (
	"fmt"
	"io"
	"maps"
	"os"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/service"
	"github.com/lixenwraith/vi-fighter/pkg/audio"
)

// Check validates every resolved external config without starting the game.
func Check(o Options, w io.Writer) error {
	if err := o.Validate(); err != nil {
		return err
	}
	event.EnsureRegistry()

	if err := checkFSM(o, w); err != nil {
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

func checkAudio(o Options, w io.Writer) error {
	src, err := Audio(o)
	if err != nil {
		return err
	}
	if src.MusicPath == "" && src.SoundPath == "" {
		fmt.Fprintln(w, "audio ok: embedded defaults")
		return nil
	}
	if src.MusicPath != "" {
		data, err := os.ReadFile(src.MusicPath)
		if err != nil {
			return fmt.Errorf("music %s: %w", src.MusicPath, err)
		}
		if _, err := audio.LoadPatternsTOML(data); err != nil {
			return fmt.Errorf("music %s: %w", src.MusicPath, err)
		}
		fmt.Fprintln(w, "music ok:", src.MusicPath)
	}
	if src.SoundPath != "" {
		data, err := os.ReadFile(src.SoundPath)
		if err != nil {
			return fmt.Errorf("sounds %s: %w", src.SoundPath, err)
		}
		if _, err := audio.LoadSoundsTOML(data); err != nil {
			return fmt.Errorf("sounds %s: %w", src.SoundPath, err)
		}
		fmt.Fprintln(w, "sounds ok:", src.SoundPath)
	}
	return nil
}

// checkFSM loads the resolved FSM config and reports its source
func checkFSM(o Options, w io.Writer) error {
	m := fsm.NewMachine[*engine.World]()
	manifest.RegisterFSMComponents(m)

	path, err := GameConfig(o)
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
