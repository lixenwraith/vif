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
// run of sc, and returns what it tells the operator, a line each. A player's
// graphs are the user root's bot/<name>.toml, beside what every graph learned;
// <graph>[.<network>] names a graph, or one network of it (addressed), and the
// scenario its populations are of is sc.
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
	case verb == "create" && len(rest) == 1:
		told, err = createBot(o, sc, dir, rest[0], "", false)
	case verb == "create" && len(rest) == 3 && rest[1] == "import":
		told, err = createBot(o, sc, dir, rest[0], rest[2], true)
	case verb == "import" && len(rest) == 2:
		told, err = importBot(o, sc, dir, rest[0], rest[1])
	case verb == "export" && len(rest) == 2:
		told, err = exportBot(o, sc, rest[0], rest[1])
	case verb == "clear" && len(rest) == 1:
		told, err = clearBot(dir, sc, rest[0])
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

// found is the document of the graph spec names as a seat of this run loads one,
// by name or path, or failing that of the own bot of the scenario spec names
func found(o Options, sc Scenario, spec string) ([]byte, string, error) {
	data, name, err := BotGraph(o, sc, spec)
	if err == nil || spec == "" {
		return data, name, err
	}
	other := o
	other.Scenario, other.Provided, other.Embedded = spec, nil, false
	if osc, lerr := LoadScenario(other); lerr == nil && len(osc.Bots()) > 0 {
		data, _ = osc.Bot(osc.Bots()[0])
		return data, osc.Bots()[0], nil
	}
	return nil, "", err
}

// addressed resolves <graph>[.<network>]: the graph the word names whole (found),
// or else the one it names before its last dot and that graph's network after
// it; network is "" where the word names a whole graph
func addressed(o Options, sc Scenario, word string) (g *bot.Graph, text []byte, network string, err error) {
	text, name, err := found(o, sc, word)
	if at := strings.LastIndex(word, "."); err != nil && at > 0 {
		network = word[at+1:]
		text, name, err = found(o, sc, word[:at])
	}
	if err != nil {
		return nil, nil, "", err
	}
	if g, err = bot.ParseGraph(name, text); err == nil && network != "" && !slices.Contains(g.Networks(), network) {
		err = fmt.Errorf("bot %s has no network %s", g.Name, network)
	}
	return g, text, network, err
}

// stems lists the files of dir with the extension ext, each without it
func stems(dir, ext string) []string {
	files, _ := fs.Glob(os.DirFS(dir), "*"+ext) // inside dir, whose own path is no pattern
	for i, file := range files {
		files[i] = strings.TrimSuffix(file, ext)
	}
	return files
}

// BotGraphs names every graph a seat of a run of sc can play, as a name finds one:
// a root's first, the player's own among them, then the scenario's and the shipped
func BotGraphs(o Options, sc Scenario) []engine.BotGraph {
	var graphs []engine.BotGraph
	add := func(name, owner string) {
		if !slices.ContainsFunc(graphs, func(g engine.BotGraph) bool { return g.Name == name }) {
			graphs = append(graphs, engine.BotGraph{Name: name, Owner: owner})
		}
	}
	dir := paths.PopulationDir()
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
	return graphs
}

// listBots names every bot a seat of this run can play (BotGraphs) with what each
// has learned in the run's scenario: a population's rounds, their recent mean and
// the member it fields
func listBots(o Options, sc Scenario, dir string) []string {
	graphs := BotGraphs(o, sc)
	lines := make([]string, 0, len(graphs))
	for _, graph := range graphs {
		name, owner := graph.Name, graph.Owner
		g, _, err := graphOf(o, sc, name)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s (%s): %v", name, owner, err))
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
				told = err.Error() // which names the network
			case !ok:
				continue
			case k.Fielded == nil:
				told += fmt.Sprintf("%d rounds, recent %.0f, none proved to field", k.Rounds, k.Recent)
			default:
				told += fmt.Sprintf("%d rounds, recent %.0f, fields %.0f over %d", k.Rounds, k.Recent, k.Score, k.Proved)
			}
			learned = append(learned, told)
		}
		switch {
		case dir == "": // a host that keeps no population
			lines = append(lines, fmt.Sprintf("%s (%s)", name, owner))
			continue
		case len(learned) == 0:
			learned = []string{"nothing learned in " + sc.Shown()}
		}
		lines = append(lines, fmt.Sprintf("%s (%s): %s", name, owner, strings.Join(learned, "; ")))
	}
	return lines
}

// createBot writes a new graph of the player's: the scenario's own with no
// weight, to learn from nothing, or taking, the one source addresses, whole or
// with the weights of the network it names alone
func createBot(o Options, sc Scenario, dir, name, source string, taking bool) (string, error) {
	path := filepath.Join(dir, name+".toml")
	if _, err := strconv.Atoi(name); !paths.IsKeymapName(name) || engine.BotUsage(name) != "" || err == nil {
		return "", fmt.Errorf("bot %q: a bot's name is one word that is no path, no count and no word of :bot", name)
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("bot %s is there already: %s", name, path)
	}
	g, text, network, err := addressed(o, sc, source)
	if err != nil {
		return "", err
	}
	told := "with every weight"
	if !taking || network != "" {
		w := g.Bare()
		if told = "with no weight, to learn from nothing"; network != "" {
			w[network], told = g.Weights()[network], "with the weights of "+network
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

// importBot writes into the player's graph name the weights source addresses,
// every network's or the one's it names, as name's own networks take them
// (bot.Graph.Fit)
func importBot(o Options, sc Scenario, dir, source, name string) (string, error) {
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
	from, _, network, err := addressed(o, sc, source)
	if err != nil {
		return "", err
	}
	var networks []string
	told := "every weight"
	if network != "" {
		networks, told = []string{network}, "the weights of "+network
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
	told = fmt.Sprintf("Bot %s took %s from %s", name, told, from.Name)
	// What a population kept outranks what the document writes until it is cleared
	kept := slices.DeleteFunc(stems(dir, ".json"), func(stem string) bool {
		_, network, _ := strings.Cut(strings.TrimPrefix(stem, name+"."), ".")
		if !strings.HasPrefix(stem, name+".") || w[network] == nil {
			return true
		}
		_, read, _ := to.Kept(network, paths.PlaceFile{Path: filepath.Join(dir, stem+".json")})
		return !read
	})
	if len(kept) > 0 {
		told += "; it still fields what it learned, and learns on from that, until cleared: " + strings.Join(kept, ", ")
	}
	return told, nil
}

// exportBot writes to file the document of the graph address names with what
// its populations field in sc, each network's or the one's the address names; a
// network none has proved keeps what the document writes
func exportBot(o Options, sc Scenario, address, file string) (string, error) {
	g, text, only, err := addressed(o, sc, address)
	if err != nil {
		return "", err
	}
	w, told := bot.Weights{}, make([]string, 0, len(g.Networks()))
	for _, network := range g.Networks() {
		if only != "" && network != only {
			continue
		}
		k, ok, err := g.Kept(network, paths.PlaceFile{Path: PopulationFile(g.Name, sc.Shown(), network)})
		switch {
		case err != nil:
			return "", err
		case ok && k.Fielded != nil:
			w[network] = k.Fielded
			told = append(told, fmt.Sprintf("%s as fielded in %s, %.0f over %d rounds", network, sc.Shown(), k.Score, k.Proved))
		default:
			told = append(told, network+" as written, none being proved in "+sc.Shown())
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

// clearBot removes what the graph address names by its name learned in sc, every
// network's population or the one's the address names, so it starts over
func clearBot(dir string, sc Scenario, address string) (string, error) {
	graph, network, _ := strings.Cut(address, ".")
	var cleared []string
	for _, stem := range stems(dir, ".json") {
		kept, ok := strings.CutPrefix(stem, graph+"."+sc.Shown()+".")
		if !ok || network != "" && kept != network {
			continue
		}
		if err := os.Remove(filepath.Join(dir, stem+".json")); err != nil {
			return "", err
		}
		cleared = append(cleared, stem)
	}
	if len(cleared) == 0 {
		return "", fmt.Errorf("no population of %s in %s is kept in %s", address, sc.Shown(), dir)
	}
	return "Cleared " + strings.Join(cleared, ", ") + "; a run learning one now keeps it again as it saves", nil
}
