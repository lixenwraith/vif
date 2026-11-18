package resource

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/paths"
)

// PopulationFile is where the user root keeps what bot graph learned in scenario
// for network; "" without a user root
func PopulationFile(graph, scenario, network string) string {
	dir := paths.PopulationDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, graph+"."+scenario+"."+network+".json")
}

// BotFiles runs one of engine.BotVerbs that keeps bot files, its word first, for a
// run of sc, and returns what it tells the operator, a line each. A player's bots
// are the user root's bot/<name>.toml, beside what every bot learned, and a
// source is a bot as a seat names one or another scenario's, <scenario>[.<bot>].
func BotFiles(o Options, sc Scenario, words []string) ([]string, error) {
	verb, rest := "", words
	if len(words) > 0 && words[0] != "add" && words[0] != "drop" {
		verb, rest = words[0], words[1:]
	}
	dir := paths.PopulationDir()
	if dir == "" && verb != "list" {
		return nil, errors.New("bot files unavailable in this run")
	}
	var told string
	var err error
	switch {
	case verb == "list" && len(rest) == 0:
		return listBots(o, sc, dir), nil
	case verb == "create" && (len(rest) == 1 || len(rest) >= 3 && rest[1] == "import"):
		told, err = createBot(o, sc, dir, rest[0], rest[1:])
	case verb == "import" && len(rest) >= 2:
		told, err = importBot(o, sc, dir, rest[0], rest[1], rest[2:])
	case verb == "export" && len(rest) == 2:
		told, err = exportBot(o, sc, rest[0], rest[1])
	case verb == "clear" && len(rest) == 1:
		told, err = clearBot(dir, rest[0])
	default:
		return nil, errors.New("usage: bot " + cmp.Or(engine.BotUsage(verb), engine.BotUsage("")))
	}
	if err != nil {
		return nil, err
	}
	return []string{told}, nil
}

// graphOf resolves and parses the bot spec names for a run of sc, with its document
func graphOf(o Options, sc Scenario, spec string) (*bot.Graph, []byte, error) {
	data, name, err := BotGraph(o, sc, spec)
	if err != nil {
		return nil, nil, err
	}
	g, err := bot.ParseGraph(name, data)
	return g, data, err
}

// sourceOf resolves what a bot takes weights from: a bot as a seat of this run
// names one, or where that finds none another scenario's by <scenario>[.<bot>],
// its own with no bot named
func sourceOf(o Options, sc Scenario, spec string) (*bot.Graph, []byte, error) {
	data, name, err := BotGraph(o, sc, spec)
	for _, at := range []int{len(spec), strings.LastIndex(spec, ".")} {
		if err == nil || at <= 0 {
			continue
		}
		other := o
		other.Scenario, other.Provided, other.Embedded = spec[:at], nil, false
		osc, lerr := LoadScenario(other)
		if lerr != nil {
			continue
		}
		if name = strings.TrimPrefix(spec[at:], "."); name == "" && len(osc.Bots()) > 0 {
			name = osc.Bots()[0]
		}
		var ok bool
		if data, ok = osc.Bot(name); !ok {
			return nil, nil, fmt.Errorf("scenario %s carries no bot %s", osc.Shown(), cmp.Or(name, "of its own"))
		}
		err = nil
	}
	if err != nil {
		return nil, nil, err
	}
	g, err := bot.ParseGraph(name, data)
	return g, data, err
}

// stems lists the files of dir with the extension ext, each without it
func stems(dir, ext string) []string {
	files, _ := fs.Glob(os.DirFS(dir), "*"+ext) // inside dir, whose own path is no pattern
	for i, file := range files {
		files[i] = strings.TrimSuffix(file, ext)
	}
	return files
}

// listBots names every bot a seat of this run can play, a root's first, with what
// each has learned in the run's scenario: a population's rounds, their recent
// mean and the member it fields
func listBots(o Options, sc Scenario, dir string) []string {
	var names, whose []string
	add := func(name, owner string) {
		if !slices.Contains(names, name) {
			names, whose = append(names, name), append(whose, owner)
		}
	}
	for _, root := range newResolver(o).roots {
		at, owner := filepath.Join(root, paths.BotDirName), "installed"
		if at == dir {
			owner = "yours"
		}
		for _, name := range stems(at, ".toml") {
			add(name, owner)
		}
	}
	for _, name := range sc.Bots() {
		add(name, "scenario "+sc.Shown()+"'s")
	}
	add(DefaultBot, "shipped")

	lines := make([]string, 0, len(names))
	for i, name := range names {
		g, _, err := graphOf(o, sc, name)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s (%s): %v", name, whose[i], err))
			continue
		}
		var learned []string
		for _, network := range g.Networks() {
			if dir == "" {
				break
			}
			k, ok, err := g.Kept(network, paths.PlaceFile{Path: PopulationFile(name, sc.Shown(), network)})
			told := network + " "
			switch {
			case err != nil:
				told += err.Error()
			case !ok:
				continue
			case k.Fielded == nil:
				told += fmt.Sprintf("%d rounds, recent %.0f, none proved to field", k.Rounds, k.Recent)
			default:
				told += fmt.Sprintf("%d rounds, recent %.0f, fields %.0f over %d", k.Rounds, k.Recent, k.Score, k.Proved)
			}
			if k.Stale {
				told += ", scored by an older rule so a run starts it over"
			}
			learned = append(learned, told)
		}
		switch {
		case dir == "": // a host that keeps no population
			lines = append(lines, fmt.Sprintf("%s (%s)", name, whose[i]))
			continue
		case len(learned) == 0:
			learned = []string{"nothing learned in " + sc.Shown()}
		}
		lines = append(lines, fmt.Sprintf("%s (%s): %s", name, whose[i], strings.Join(learned, "; ")))
	}
	return lines
}

// createBot writes a new bot of the player's: the scenario's own with no weight,
// to learn from nothing, or after import the source named, whole, or with the
// weights of the networks listed alone
func createBot(o Options, sc Scenario, dir, name string, rest []string) (string, error) {
	path := filepath.Join(dir, name+".toml")
	if _, err := strconv.Atoi(name); !paths.IsKeymapName(name) || engine.BotUsage(name) != "" || err == nil {
		return "", fmt.Errorf("bot %q: a bot's name is one word that is no path, no count and no word of :bot", name)
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("bot %s is there already: %s", name, path)
	}
	source, networks, told := "", []string(nil), "with no weight, to learn from nothing"
	if len(rest) > 0 {
		source, networks = rest[1], rest[2:]
		told = "with " + weightsOf(networks)
	}
	g, text, err := sourceOf(o, sc, source)
	if err != nil {
		return "", err
	}
	if len(rest) == 0 || len(networks) > 0 {
		w := g.Bare()
		for _, network := range networks {
			if _, ok := w[network]; !ok {
				return "", fmt.Errorf("bot %s has no network %s", g.Name, network)
			}
			w[network] = g.Weights()[network]
		}
		if text, err = g.Document(w); err != nil {
			return "", err
		}
	}
	if err := (paths.FileStore{Path: path}).Store(text); err != nil {
		return "", err
	}
	return fmt.Sprintf("Bot %s created from %s %s: %s", name, g.Name, told, path), nil
}

// importBot writes into the player's bot name the weights of source's networks
// listed, of each where none is, as its own networks take them (bot.Graph.Fit)
func importBot(o Options, sc Scenario, dir, source, name string, networks []string) (string, error) {
	path := filepath.Join(dir, name+".toml")
	data, err := os.ReadFile(path)
	if !paths.IsKeymapName(name) {
		err = errors.New("a bot is named, not given by a path")
	}
	if err != nil {
		return "", fmt.Errorf("bot %s is no bot of yours to write into; create it first: %w", name, err)
	}
	to, err := bot.ParseGraph(name, data)
	if err != nil {
		return "", err
	}
	from, _, err := sourceOf(o, sc, source)
	if err != nil {
		return "", err
	}
	w, err := to.Fit(from, networks...)
	if err != nil {
		return "", err
	}
	text, err := to.Document(w)
	if err != nil {
		return "", err
	}
	if err := (paths.FileStore{Path: path}).Store(text); err != nil {
		return "", err
	}
	told := fmt.Sprintf("Bot %s took %s from %s", name, weightsOf(networks), from.Name)
	// What a population kept outranks what the document writes until it is cleared
	kept := slices.DeleteFunc(stems(dir, ".json"), func(stem string) bool {
		_, network, _ := strings.Cut(strings.TrimPrefix(stem, name+"."), ".")
		return !strings.HasPrefix(stem, name+".") || w[network] == nil
	})
	if len(kept) > 0 {
		told += "; it still fields what it learned, and learns on from that, until cleared: " + strings.Join(kept, ", ")
	}
	return told, nil
}

// weightsOf names the weights taken: the networks listed, or every one
func weightsOf(networks []string) string {
	if len(networks) == 0 {
		return "every weight"
	}
	return "the weights of " + strings.Join(networks, ", ")
}

// exportBot writes to file the document of the bot address names with, for each
// network, what its population fields in the scenario the address names, the
// run's where it names none; a network with none proved keeps what is written
func exportBot(o Options, sc Scenario, address, file string) (string, error) {
	name, scenario, named := strings.Cut(address, ".")
	if !named {
		scenario = sc.Shown()
	}
	if !paths.IsKeymapName(name) {
		return "", fmt.Errorf("bot %q: export names a bot, <bot>[.<scenario>], not a file", address)
	}
	g, text, err := graphOf(o, sc, name)
	if err != nil {
		return "", err
	}
	w, told := bot.Weights{}, make([]string, 0, len(g.Networks()))
	for _, network := range g.Networks() {
		k, ok, err := g.Kept(network, paths.PlaceFile{Path: PopulationFile(g.Name, scenario, network)})
		switch {
		case err != nil:
			return "", err
		case ok && k.Fielded != nil:
			w[network] = k.Fielded
			told = append(told, fmt.Sprintf("%s as fielded in %s, %.0f over %d rounds", network, scenario, k.Score, k.Proved))
		default:
			told = append(told, network+" as written, none being proved in "+scenario)
		}
	}
	if len(w) > 0 {
		if text, err = g.Document(w); err != nil {
			return "", err
		}
	}
	if err := (paths.FileStore{Path: file}).Store(text); err != nil {
		return "", err
	}
	return fmt.Sprintf("Bot %s written to %s: %s", g.Name, file, strings.Join(told, "; ")), nil
}

// clearBot removes the populations address names, every one below a shorter
// address, so what they learned starts over
func clearBot(dir, address string) (string, error) {
	var cleared []string
	for _, stem := range stems(dir, ".json") {
		if stem != address && !strings.HasPrefix(stem, address+".") || strings.Count(stem, ".") < 2 {
			continue
		}
		if err := os.Remove(filepath.Join(dir, stem+".json")); err != nil {
			return "", err
		}
		cleared = append(cleared, stem)
	}
	if len(cleared) == 0 {
		return "", fmt.Errorf("no population of %s is kept in %s", address, dir)
	}
	return "Cleared " + strings.Join(cleared, ", ") + "; a run learning one now keeps it again as it saves", nil
}
