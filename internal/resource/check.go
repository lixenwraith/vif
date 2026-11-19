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

	sc, err := checkScenario(o, w)
	if err != nil {
		return err
	}
	if err := checkKeymap(o, w); err != nil {
		return err
	}
	if err := checkAudio(o, w); err != nil {
		return err
	}
	if err := checkContent(o, sc, w); err != nil {
		return err
	}
	return checkBots(o, sc, w)
}

func checkKeymap(o Options, w io.Writer) error {
	path, data, own, err := keymap(o)
	if err != nil {
		return err
	}
	switch {
	case path != "":
		if data, err = os.ReadFile(path); err != nil {
			return fmt.Errorf("keymap %s: %w", path, err)
		}
	case data == nil:
		_ = input.DefaultKeyTable() // parse and validate the embedded document
		fmt.Fprintln(w, "keymap ok: embedded default")
		return nil
	default:
		path = "embedded " + o.Keymap
	}
	_, notes, err := input.UpgradeKeymap(data)
	switch {
	case err != nil:
		return fmt.Errorf("keymap %s: %w", path, err)
	case len(notes) == 0:
		fmt.Fprintln(w, "keymap ok:", path)
	case own:
		fmt.Fprintf(w, "keymap %s: %s: %s\n", path, paths.WriteBack(path), strings.Join(notes, "; "))
	default:
		fmt.Fprintf(w, "keymap %s: a run reads it up to date: %s\n", path, strings.Join(notes, "; "))
	}
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
	m, err := machineOf(sc)
	if err == nil {
		err = checkSystems(m)
	}
	if err != nil {
		return Scenario{}, fmt.Errorf("scenario %s: %w", sc.Name, err)
	}
	return sc, nil
}

// machineOf compiles sc's machine as a run does, its regions and states declared
func machineOf(sc Scenario) (*fsm.Machine[*engine.World], error) {
	programs, err := sc.Behaviors()
	if err != nil {
		return nil, err
	}
	loot, err := sc.Loot()
	if err != nil {
		return nil, err
	}
	set, err := system.CompileBehaviors(programs, loot)
	if err != nil {
		return nil, err
	}
	m := fsm.NewMachine[*engine.World]()
	manifest.RegisterFSMComponents(m, set.Declared())
	return m, fsm.LoadScenarioFromFS(m, sc.FS(), sc.Entry())
}

// heldTo is the fault of a graph sc carries whose InState names a region or a
// state sc's machine lacks: it loads all the same, and never holds
func heldTo(g *bot.Graph, m *fsm.Machine[*engine.World]) error {
	for _, ask := range g.InStates() {
		if !slices.Contains(m.DeclaredRegions(), ask.Region) {
			return fmt.Errorf("bot %s: InState asks of region %q, which the scenario has not", g.Name, ask.Region)
		}
		for _, state := range ask.States {
			if _, ok := m.GetStateID(state); !ok {
				return fmt.Errorf("bot %s: InState names state %q, which the scenario has not", g.Name, state)
			}
		}
	}
	return nil
}

// checkScenario reports what a peer would compare: the name, the digest, and
// where it came from, and the players it runs with.
func checkScenario(o Options, w io.Writer) (Scenario, error) {
	sc, err := ValidateScenario(o)
	if err != nil {
		return Scenario{}, err
	}
	source, err := ScenarioPath(o)
	if err != nil {
		return Scenario{}, err
	}
	if source == "" {
		source = "embedded main"
	}
	fmt.Fprintf(w, "scenario ok: %s (%s, %d files, sha256:%s, players %d..%d)\n",
		source, sc.Name, sc.Files(), sc.Short(), sc.MinPlayers, sc.MaxPlayers)
	fmt.Fprintln(w, "systems ok")
	programs, err := sc.Behaviors()
	if err != nil {
		return Scenario{}, err
	}
	loot, err := sc.Loot()
	if err != nil {
		return Scenario{}, err
	}
	kinds, _ := loot["loot"].(map[string]any)
	tables, _ := loot["drops"].(map[string]any)
	layouts, _ := loot["maps"].(map[string]any)
	for _, line := range []struct {
		key   string
		names []string
	}{
		{"behaviors", slices.Sorted(maps.Keys(programs))},
		{"loot", slices.Sorted(maps.Keys(kinds))},
		{"drops", slices.Sorted(maps.Keys(tables))},
		{"maps", slices.Sorted(maps.Keys(layouts))},
	} {
		fmt.Fprintf(w, "%s ok: %d", line.key, len(line.names))
		if len(line.names) > 0 {
			fmt.Fprintf(w, " (%s)", strings.Join(line.names, ", "))
		}
		fmt.Fprintln(w)
	}
	return sc, nil
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
func checkContent(o Options, sc Scenario, w io.Writer) error {
	src, err := Corpus(o, sc)
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

// checkBots parses every graph a name can resolve to, in each root, in the
// scenario and embedded, so a broken one fails here rather than when -bot or
// :bot add first loads it, and holds those the scenario carries to its own
// regions and states (heldTo).
func checkBots(o Options, sc Scenario, w io.Writer) error {
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
	for _, name := range sc.Bots() {
		data, _ := sc.Bot(name)
		g, err := bot.ParseGraph(name, data)
		if err == nil {
			var m *fsm.Machine[*engine.World]
			if m, err = machineOf(sc); err == nil {
				err = heldTo(g, m)
			}
		}
		if err != nil {
			faults = append(faults, fmt.Errorf("scenario %s: %w", sc.Shown(), err))
			continue
		}
		fmt.Fprintf(w, "bot ok: scenario %s %s\n", sc.Shown(), name)
	}
	parse(asset.DefaultBots, func(file string) string { return "embedded " + strings.TrimSuffix(file, ".toml") })
	return errors.Join(faults...)
}
