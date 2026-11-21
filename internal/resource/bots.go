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

// Populations is where a run keeps what its bots learned, a document a name,
// <graph>.<scenario>.<network>: the user root's bot/ on a host with files, and on
// one with none what its Options name, as a browser's page. Names lists those
// that hold a document, and Remove leaves a name with none.
type Populations interface {
	Names() []string
	Open(name string) bot.Store
	Remove(name string) error
}

// populationDir keeps a population as <name>.json in a directory
type populationDir string

func (d populationDir) Names() []string { return stems(string(d), ".json") }
func (d populationDir) Open(name string) bot.Store {
	return paths.PlaceFile{Path: filepath.Join(string(d), name+".json")}
}
func (d populationDir) Remove(name string) error {
	return os.Remove(filepath.Join(string(d), name+".json"))
}

// populations is where a run under o keeps them, nil for a run that keeps none
func (o Options) populations() Populations {
	if o.Populations != nil {
		return o.Populations
	}
	if dir := paths.PopulationDir(); dir != "" {
		return populationDir(dir)
	}
	return nil
}

// Population is where a run under o keeps what bot graph learned in scenario
// for network, nil for a run that keeps none
func (o Options) Population(graph, scenario, network string) bot.Store {
	if kept := o.populations(); kept != nil {
		return kept.Open(graph + "." + scenario + "." + network)
	}
	return nil
}

// BotFiles runs one of engine.BotVerbs that keeps bot files, its word first, for a
// run of sc, and returns what it tells the operator, a line each. A player's
// graphs are the user root's bot/<name>.toml, and what every graph learned its
// populations; a word names a graph, some of its networks and the scenario its
// populations are of (addressed), sc where it names none.
func BotFiles(o Options, sc Scenario, words []string) ([]string, error) {
	verb, rest := "", words
	if len(words) > 0 && words[0] != "add" && words[0] != "drop" {
		verb, rest = words[0], words[1:]
	}
	// A graph is a file; what one learned is a document a host may keep without files
	dir, kept := paths.PopulationDir(), o.populations()
	if verb != "list" && (kept == nil || dir == "" && verb != "clear") {
		return nil, errors.New("bot files unavailable in this run")
	}
	var told string
	var err error
	switch {
	case verb == "list" && len(rest) == 0:
		return listBots(o, sc), nil
	case verb == "list" && len(rest) == 1:
		in, err := named(o, rest[0])
		if err != nil {
			return nil, err
		}
		return listBots(o, in), nil
	case verb == "create" && len(rest) == 1:
		told, err = createBot(o, sc, dir, rest[0], "", false)
	case verb == "create" && len(rest) == 3 && rest[1] == "import":
		told, err = createBot(o, sc, dir, rest[0], rest[2], true)
	case verb == "import" && len(rest) == 2:
		told, err = importBot(o, sc, dir, rest[0], rest[1])
	case verb == "export" && len(rest) == 2:
		told, err = exportBot(o, sc, rest[0], rest[1])
	case verb == "clear" && len(rest) == 1:
		told, err = clearBot(kept, sc, rest[0])
	default:
		return nil, errors.New("usage: bot " + cmp.Or(engine.BotUsage(verb), engine.BotUsage("")))
	}
	if err != nil {
		return nil, err
	}
	return []string{told}, nil
}

// graphOf resolves and parses the bot spec names for a run of sc
func graphOf(o Options, sc Scenario, spec string) (*bot.Graph, error) {
	data, name, err := BotGraph(o, sc, spec)
	if err != nil {
		return nil, err
	}
	return bot.ParseGraph(name, data)
}

// named is the scenario of that name, as -s finds one
func named(o Options, name string) (Scenario, error) {
	o.Scenario, o.Provided, o.Embedded = name, nil, false
	return LoadScenario(o)
}

// found is the document of the graph spec names as a seat of this run loads one,
// by name or path, or failing that of the own bot of the scenario spec names
func found(o Options, sc Scenario, spec string) ([]byte, string, error) {
	data, name, err := BotGraph(o, sc, spec)
	if err == nil || spec == "" {
		return data, name, err
	}
	if osc, lerr := named(o, spec); lerr == nil && len(osc.Bots()) > 0 {
		data, _ = osc.Bot(osc.Bots()[0])
		return data, osc.Bots()[0], nil
	}
	return nil, "", err
}

// address is what <graph>[@<scenario>][.<network>[,<network>]] names: a graph
// with its document, those of its networks named, every one where none is
// (whole), and in, the scenario its populations are of
type address struct {
	g        *bot.Graph
	text     []byte
	networks []string
	whole    bool
	in       Scenario
}

// addressed resolves word for a run of sc: the graph it names whole (found), or
// else the one before its last @ in the scenario after it, or before its last
// dot, with the networks listed after
func addressed(o Options, sc Scenario, word string) (address, error) {
	a := address{in: sc, whole: true}
	text, name, err := found(o, sc, word)
	if err != nil {
		graph, listed := word, ""
		if at := strings.LastIndex(word, "@"); at > 0 {
			var scenario string
			scenario, listed, _ = strings.Cut(word[at+1:], ".")
			if a.in, err = named(o, scenario); err != nil {
				return a, err
			}
			graph = word[:at]
		} else if at := strings.LastIndex(word, "."); at > 0 {
			graph, listed = word[:at], word[at+1:]
		} else {
			return a, err
		}
		if listed != "" {
			a.networks, a.whole = strings.Split(listed, ","), false
		}
		if text, name, err = found(o, a.in, graph); err != nil {
			return a, err
		}
	}
	if a.g, err = bot.ParseGraph(name, text); err != nil {
		return a, err
	}
	for i, network := range a.networks {
		if !slices.Contains(a.g.Networks(), network) || slices.Contains(a.networks[:i], network) {
			return a, fmt.Errorf("bot %s has no network %q, or it is named twice", a.g.Name, network)
		}
	}
	if a.text = text; a.whole {
		a.networks = a.g.Networks()
	}
	return a, nil
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
// a root's first, the player's own among them, then the scenario's and the
// shipped, each with what it has learned in sc: a population's rounds, their
// recent mean and the member it fields
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
	for i := range graphs {
		graphs[i].Learned = learned(o, sc, graphs[i].Name)
	}
	return graphs
}

// learned says what the graph of that name has learned in sc, a population a
// network; "" on a host that keeps none
func learned(o Options, sc Scenario, name string) string {
	g, err := graphOf(o, sc, name)
	if err != nil {
		return err.Error()
	}
	var told []string
	for _, network := range g.Networks() {
		store := o.Population(name, sc.Shown(), network)
		if store == nil {
			return ""
		}
		switch k, ok, err := g.Kept(network, store); {
		case err != nil:
			told = append(told, err.Error()) // which names the network
		case !ok:
		case k.Fielded == nil:
			told = append(told, fmt.Sprintf("%s %d rounds, recent %.0f, none proved to field", network, k.Rounds, k.Recent))
		default:
			told = append(told, fmt.Sprintf("%s %d rounds, recent %.0f, fields %.0f over %d", network, k.Rounds, k.Recent, k.Score, k.Proved))
		}
	}
	if len(told) == 0 {
		return "nothing learned in " + sc.Shown()
	}
	return strings.Join(told, "; ")
}

// listBots names every bot a seat of a run of sc can play with what each has
// learned there (BotGraphs), a line each
func listBots(o Options, sc Scenario) []string {
	graphs := BotGraphs(o, sc)
	lines := make([]string, len(graphs))
	for i, g := range graphs {
		if lines[i] = fmt.Sprintf("%s (%s)", g.Name, g.Owner); g.Learned != "" {
			lines[i] += ": " + g.Learned
		}
	}
	return lines
}

// createBot writes a new graph of the player's: the scenario's own with no
// weight, to learn from nothing, or taking, the one source addresses, whole or
// with the weights of the networks it names alone
func createBot(o Options, sc Scenario, dir, name, source string, taking bool) (string, error) {
	path := filepath.Join(dir, name+".toml")
	if _, err := strconv.Atoi(name); !paths.IsKeymapName(name) || engine.BotUsage(name) != "" || err == nil {
		return "", fmt.Errorf("bot %q: a bot's name is one word that is no path, no count and no word of :bot", name)
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("bot %s is there already: %s", name, path)
	}
	a, err := addressed(o, sc, source)
	if err != nil {
		return "", err
	}
	text, told := a.text, "with every weight"
	if !taking || !a.whole {
		w := a.g.Bare()
		if told = "with no weight, to learn from nothing"; taking {
			for _, network := range a.networks {
				w[network] = a.g.Weights()[network]
			}
			told = "with the weights of " + strings.Join(a.networks, ", ")
		}
		if text, err = a.g.Document(w); err != nil {
			return "", err
		}
	}
	if err := (paths.FileStore{Path: path}).Store(text); err != nil {
		return "", err
	}
	return fmt.Sprintf("Bot %s created from %s %s: %s", name, a.g.Name, told, path), nil
}

// importBot writes into the player's graph name the weights source addresses,
// every network's or those of the ones it names, as name's own networks take
// them (bot.Graph.Fit)
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
	from, err := addressed(o, sc, source)
	if err != nil {
		return "", err
	}
	told := "every weight"
	if !from.whole {
		told = "the weights of " + strings.Join(from.networks, ", ")
	}
	w, err := to.Fit(from.g, from.networks...)
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
	told = fmt.Sprintf("Bot %s took %s from %s", name, told, from.g.Name)
	// What a population kept outranks what the document writes until it is cleared
	kept := o.populations()
	still := slices.DeleteFunc(kept.Names(), func(stem string) bool {
		_, network, _ := strings.Cut(strings.TrimPrefix(stem, name+"."), ".")
		if !strings.HasPrefix(stem, name+".") || w[network] == nil {
			return true
		}
		_, read, _ := to.Kept(network, kept.Open(stem))
		return !read
	})
	if len(still) > 0 {
		told += "; it still fields what it learned, and learns on from that, until cleared: " + strings.Join(still, ", ")
	}
	return told, nil
}

// exportBot writes to file the document of the graph word addresses with what
// its populations field in the scenario addressed, each network's or those of
// the ones named; a network none has proved keeps what the document writes
func exportBot(o Options, sc Scenario, word, file string) (string, error) {
	a, err := addressed(o, sc, word)
	if err != nil {
		return "", err
	}
	text, w, told := a.text, bot.Weights{}, make([]string, 0, len(a.networks))
	for _, network := range a.networks {
		k, ok, err := a.g.Kept(network, o.Population(a.g.Name, a.in.Shown(), network))
		switch {
		case err != nil:
			return "", err
		case ok && k.Fielded != nil:
			w[network] = k.Fielded
			told = append(told, fmt.Sprintf("%s as fielded in %s, %.0f over %d rounds", network, a.in.Shown(), k.Score, k.Proved))
		default:
			told = append(told, network+" as written, none being proved in "+a.in.Shown())
		}
	}
	if len(w) > 0 {
		if text, err = a.g.Document(w); err != nil {
			return "", err
		}
	}
	if err := (paths.FileStore{Path: file}).Store(text); err != nil {
		return "", err
	}
	return fmt.Sprintf("Bot %s written to %s: %s", a.g.Name, file, strings.Join(told, "; ")), nil
}

// clearBot removes what the graph word names by its name learned, in sc or the
// scenario after its @: every network's population or those of the ones named,
// which start over. The graph need not be found: a name alone clears.
func clearBot(kept Populations, sc Scenario, word string) (string, error) {
	head, listed, _ := strings.Cut(word, ".")
	graph, scenario, _ := strings.Cut(head, "@")
	scenario = cmp.Or(scenario, sc.Shown())
	var cleared []string
	for _, stem := range kept.Names() {
		network, ok := strings.CutPrefix(stem, graph+"."+scenario+".")
		if !ok || listed != "" && !slices.Contains(strings.Split(listed, ","), network) {
			continue
		}
		if err := kept.Remove(stem); err != nil {
			return "", err
		}
		cleared = append(cleared, stem)
	}
	if len(cleared) == 0 {
		return "", fmt.Errorf("no population of %s in %s is kept", word, scenario)
	}
	return "Cleared " + strings.Join(cleared, ", ") + "; a run learning one starts it over as it next saves", nil
}
