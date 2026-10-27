package resource

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/manifest"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/system"
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
	if err := checkContent(o, w); err != nil {
		return err
	}
	return checkBots(o, w)
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

// ValidateScenario resolves and reads a scenario, compiles its behavior programs,
// loads it into a throwaway machine, whose species names resolve against them, and
// checks every system it names against this build. It is what `-check` proves before
// a run starts and what an operator changing scenario mid-run proves before the run
// it would replace is torn down.
func ValidateScenario(o Options) (Scenario, error) {
	sc, err := LoadScenario(o)
	if err != nil {
		return Scenario{}, err
	}
	var set *system.BehaviorSet
	programs, err := sc.Behaviors()
	if err == nil {
		set, err = system.CompileBehaviors(programs)
	}
	if err == nil {
		m := fsm.NewMachine[*engine.World]()
		manifest.RegisterFSMComponents(m, set.Prototypes())
		if err = fsm.LoadScenarioFromFS(m, sc.FS(), sc.Entry()); err == nil {
			err = checkSystems(m)
		}
	}
	if err != nil {
		return Scenario{}, fmt.Errorf("scenario %s: %w", sc.Name, err)
	}
	return sc, nil
}

// checkScenario reports what a peer would compare: the name, the digest, and
// where it came from, and the players it runs with.
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
	fmt.Fprintf(w, "scenario ok: %s (%s, %d files, sha256:%s, players %d..%d)\n",
		source, sc.Name, sc.Files(), sc.Short(), sc.MinPlayers, sc.MaxPlayers)
	fmt.Fprintln(w, "systems ok")
	programs, err := sc.Behaviors()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "behaviors ok: %d", len(programs))
	if len(programs) > 0 {
		fmt.Fprintf(w, " (%s)", strings.Join(slices.Sorted(maps.Keys(programs)), ", "))
	}
	fmt.Fprintln(w)
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

	global := m.GetSystemsConfig()
	if global != nil {
		check("[systems]", global.DisabledSystems)
		check("[systems]", global.OnlySystems)
	}
	for _, r := range m.DeclaredRegions() {
		cfg := m.GetRegionConfig(r)
		if cfg == nil {
			continue
		}
		check("region "+r, cfg.EnabledSystems)
		check("region "+r, cfg.DisabledSystems)
		check("region "+r, cfg.OnlySystems)
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown system names:\n  %s", strings.Join(unknown, "\n  "))
	}

	return checkSystemDependencies(m, profiles, global)
}

// checkSystemDependencies reports every enabled system whose required
// dependency the config disables. Each region is evaluated alone, on its
// only_systems or the global baseline: running beside others, the union stays
// closed and ApplySystems keeps a disabled requirement on.
func checkSystemDependencies(m *fsm.Machine[*engine.World], profiles []manifest.SystemProfile,
	global *fsm.SystemsConfig) error {

	base := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		base[p.Name] = global.Enabled(p.Name)
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
		if cfg.OnlySystems != nil {
			for _, p := range profiles {
				enabled[p.Name] = slices.Contains(cfg.OnlySystems, p.Name)
			}
		}
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

// checkBots parses every graph a name can resolve to, in each root and embedded, so
// a broken one fails here rather than when -bot or :bot add first loads it.
func checkBots(o Options, w io.Writer) error {
	var faults []error
	// Globbed inside the directory, so a root's own path is never read as a pattern
	parse := func(fsys fs.FS, where func(file string) string) {
		files, _ := fs.Glob(fsys, "*.toml")
		for _, file := range files {
			data, err := fs.ReadFile(fsys, file)
			if err == nil {
				_, err = bot.ParseGraph(where(file), data)
			}
			if err != nil {
				faults = append(faults, err)
				continue
			}
			fmt.Fprintln(w, "bot ok:", where(file))
		}
	}
	for _, root := range newResolver(o).roots {
		dir := filepath.Join(root, paths.BotDirName)
		parse(os.DirFS(dir), func(file string) string { return filepath.Join(dir, file) })
	}
	parse(asset.DefaultBots, func(file string) string { return "embedded " + strings.TrimSuffix(file, ".toml") })
	return errors.Join(faults...)
}
