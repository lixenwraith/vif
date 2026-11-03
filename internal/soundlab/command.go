//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrQuit is what the quit verb returns; a front end ends its loop on it
var ErrQuit = errors.New("quit")

type command struct {
	usage    string
	help     string
	min, max int // arg count; max -1 = unlimited
	fn       func(*Session, []string) error
}

// commands is the verb table, the whole API: the REPL, the script runner and the
// TUI dispatch through Execute, so nothing one front end can do is invisible to
// another, which is what keeps the scripted end-to-end test authoritative.
var commands map[string]*command

// init, not a var literal: cmdHelp closes over the table.
func init() {
	commands = map[string]*command{
		"help": {"help [verb|keys]", "list commands or describe one", 0, 1, cmdHelp},
		"quit": {"quit", "stop the engine and exit", 0, 0,
			func(*Session, []string) error { return ErrQuit }},

		"load":     {"load [sound|pattern] <file>", "replace document from file (sound includes resolve within the file's dir; voices come with the sounds, songs with the patterns); the kind alone names the file, which is read by its tables, a .slab run as a script", 1, 2, cmdLoad},
		"merge":    {"merge [sound|pattern] <file>", "overlay file onto document by name; the kind alone names the file, which is read by its tables, a .slab run as a script", 1, 2, cmdMerge},
		"builtin":  {"builtin sound|pattern|voice", "seed document from built-ins (patterns: current registry snapshot; sound seeds the voices too)", 1, 1, cmdBuiltin},
		"save":     {"save sound|pattern [file]", "write document; bare = provenance path (the sound document carries the voices)", 1, 2, cmdSave},
		"write":    {"write <name> <file>", "save one entry as standalone TOML (export: dirty/modified untouched)", 2, 2, cmdWrite},
		"export":   {"export <name> <file.wav> [variant]", "render to WAV; no variant = canonical take", 2, 3, cmdExport},
		"rip":      {"rip <file.wav|file.mid> <name> [bars] [nodrums] [bpm <n>] [lead busy|high|low|loud|soft|long|short]", "hear a recording's beat, melody and bass as patterns of up to bars (4) bars each, <name>_beat_1 ..., and take its tempo and key; nodrums leaves the drum part out; bpm <n> reads it at that tempo, the bar line still decoded; lead names which of the melody register's two lines is the melody, the busier by default, or the higher, lower, louder, softer, longer or shorter in its notes; a .mid is read as a score, its notes the transcript, at its first tempo", 2, 8, cmdRip},
		"render":   {"render <file.wav> [bars]", "write what the slots hold, bars (4) bars at the transport's tempo and key, to a WAV through the engine's offline render", 1, 2, cmdRender},
		"corpus":   {"corpus <dir> [loops] [bars] [aug] [gen] [nowav] [tempos lo-hi|a,b,c] [part k/n]", "render loops (24) of bars (4) bars from the registered patterns into dir, each a WAV, its log-mel frames and the events that sound in it: what tool/rip-train trains the rip's model on; aug puts a room, a mix and voices of random genes behind each; gen draws beats, melodies and bass lines the bank never had; nowav keeps the frames and the labels only, which is all the trainer reads; tempos lo-hi or a,b,c sets the tempos the loops cycle through; a loop already in dir is kept, so a stopped export run again goes on where it stopped; part k/n renders every n-th loop from the k-th, for n exports at once, and leaves corpus.json to a last run without it", 1, 10, cmdCorpus},
		"tune":     {"tune [generations] [file]", "search rip's parameters over loops the engine renders from the registered patterns; the best write to file (the first root's audio/rip.toml) and listen from now on; 8 generations take minutes", 0, 2, cmdTune},
		"notes":    {"notes <name> [file.mid]", "list the transcript of a recording ripped as <name>: each note's time, bar and step, pitch as the key's degree with any accidental, length, loudness and support (how much of the recording's salience over its frames is its pitch's; a low one is a note to doubt), and a melody-register note's bright (its fundamental's share of its first six partials, 1 for a pure tone), by line, after the tuning, the first bar and how many melody-register notes a concurrent note left unmeasured; with a file, write it as a MIDI file at the rip's tempo instead, a track each for melody, under and bass, and the drums on the drum channel", 1, 2, cmdNotes},
		"agree":    {"agree <a> <b>", "compare the transcripts of two rips of one piece, two witnesses: each line's notes aligned in order under the transposition that matches most, the notes only one hears listed, and the time map between them", 2, 2, cmdAgree},
		"truth":    {"truth <name> <file>", "score the transcript of a recording ripped as <name> against notes given in a file in the notes verb's own form (a header 'at N bpm' sets the step; each line '<line> <start>s ... <pitch> ... <length> step(s)' or '<line> <start>s <pitch> <length>s'), corrected by hand, or in a .mid file (tracks named melody, under and bass, else the lowest centred under E3 the bass and the rest melody then under): how many of the notes given it heard, and how many it heard that were not given; a .mid file's drum channel scores the kicks, snares and hats the same way, each hit within half a step under the map that matches most", 2, 2, cmdTruth},
		"fit":      {"fit <name> [generations]", "fit the voices of a recording ripped as <name> again, over generations (12) of candidates the engine plays at its notes; they replace <name>_melody_voice and <name>_bass_voice", 1, 2, cmdFit},
		"compress": {"compress <name> [percent]", "match the bars of a recording ripped as <name> to as few one-bar patterns as a difference of percent (5) allows: the dictionary <name>_<part>_a..., and the song <name> that plays them in order, rendered and ripped back for a score", 1, 2, cmdCompress},
		"refine":   {"refine <name> [rounds]", "close the loop over a song compress made of a recording ripped as <name>: each round (3) fits the voices again and sweeps the tonal steps, each tried a semitone and an octave either way, the recording's strongest pitch there, a step earlier or later, longer or shorter, and silent, then a step a line leaves empty tries that pitch; every move kept when the song sounds more like the recording; minutes a round", 1, 2, cmdRefine},
		"bench":    {"bench [loops] [bars] [verbose]", "grade the analysis, and the model when one loaded, on fresh loops (8) of bars (4) bars the engine renders, clean and augmented: the mean score of each rip against the loop's expected rip; verbose then lists each loop, its tempo, key, kind, generated parts and score, and the three worst by total", 0, 3, cmdBench},
		"run":      {"run <file.slab>", "run a script's lines in this session, its relative paths read from its own directory, strict as -s is: a line that fails stops it with its number; quit in it ends the script, not the session", 1, 1, cmdRun},
		"song":     {"song <name> [file.wav [<instrument>...]|play|score]", "show a song's bars; render it to a WAV, only the instruments named sounding when any are, a line on its own; play it through the sequencer, the slots changed at the bar lines (music stop ends it); or score it, how it sounds against the recording ripped as <name> and how it rips back", 1, 8, cmdSong},

		"ls":    {"ls sound|pattern|voice|song [glob]", "list document entries (* marks dirty)", 1, 2, cmdLs},
		"show":  {"show <name>[.path]", "bare name = full TOML; deeper = leaf value or keys", 1, 1, cmdShow},
		"where": {"where", "transport bar:step and per-slot sounding pattern", 0, 0, cmdWhere},
		"stat":  {"stat", "backend, silent mode, play stats, dirty entries", 0, 0, cmdStat},

		"new":   {"new sound|pattern|voice <name>", "create an entry seeded with a minimal valid spec", 2, 2, cmdNew},
		"cp":    {"cp <src> <dst> | cp <pattern>.track.<n>... <dst> | cp <pattern>.bar.<k>... <dst>", "duplicate an entry, or make a new pattern of the tracks named, from one pattern or several, the first's role kept: a length every one loops in, each repeated to fill it; or of one pattern's bars, in the order named, every track sliced at the bar lines (a bar named twice plays twice; at most 64 steps)", 2, -1, cmdCp},
		"mv":    {"mv <src> <dst>", "rename (sound noise variants re-roll: rng seeds from name)", 2, 2, cmdMv},
		"mix":   {"mix <src> <dst> | mix <pattern>.track.<n>... <dst> | mix <pattern>.track.<n>... <pattern>.track.<m>", "append src's tracks/layers into dst (same kind; src unchanged; validate dst after), or the tracks named into the pattern dst, at a length every one loops in, dst's own tracks and each repeated to fill it (a cut beyond 64 steps is said), or their notes into track m, which keeps its voice, a note already at its step kept once (drum and tonal tracks never merge); with del, a track moves", 2, -1, cmdMix},
		"take":  {"take <pattern> <voice> <pos>:<midi>[:<steps>[:<vel>]]...", "add a track of a tonal voice playing the notes at their steps, each on the key's nearest degree", 3, -1, cmdTake},
		"split": {"split <pattern>.track.<n> <midi>", "part a tonal track at a pitch under the transport's key: its notes from midi up stay, the rest go to a new track of its voice after it; refused when either side would be empty", 2, 2, cmdSplit},
		"shift": {"shift <pattern>.track.<n>... oct|deg|semi|step <±k>", "move every note of the tracks: by octaves; by degrees of the key, carried into the octave; by semitones, spelt again on the key's nearest degree; or by steps, rotated around the pattern's length (a drum track only by step); a note past a step's bounds refuses the whole shift", 3, -1, cmdShift},
		"set":   {"set <name.path> <value...>", "set a leaf field; value may contain spaces", 2, -1, cmdSet},
		"add":   {"add <name.path>", "append a zero element to a list field", 1, 1, cmdAdd},
		"del":   {"del <name>|<name.path.idx>...", "delete document entries or list elements, each as the path reads before any is deleted", 1, -1, cmdDel},
		"unset": {"unset <name.path>", "reset a field to its zero value (nil for optional tables/lists)", 1, 1, cmdUnset},

		"validate": {"validate [name|all]", "check one entry or all (all fails if any is invalid)", 0, 1, cmdValidate},
		"apply":    {"apply [name|all]", "register into the engine; bare/all = dirty entries only", 0, 1, cmdApply},
		"revert":   {"revert [name|all]", "restore from registry; bare/all = dirty entries only", 0, 1, cmdRevert},
		"undo":     {"undo [n]", "step back over the last n (1) edits, a command that changed a document each (a TUI key's commands one): the entries it changed as they were, their marks and order; not the registry or the transport", 0, 1, cmdUndo},

		"play": {"play <name|file.wav> [vol]", "audition document state (canonical take), falling back to the registry; a .wav file plays as it is", 1, 2, cmdPlay},
		"hit":  {"hit kick|snare|hihat|clap [vol]", "audition a drum", 1, 2, cmdHit},
		"note": {"note <midi> [vel] [steps] [voice]", "tonal audition via the melody slot (needs music running); a dirty voice auto-applies", 1, 4, cmdNote},
		"slot": {"slot <0|1|2|3> <pattern|-> [fade_ms] [q]", "assign a pattern (0 rhythm, 1 melody, 2 fill, 3 bass); dirty patterns auto-apply; - = silence", 2, 4, cmdSlot},

		"music":  {"music start|stop|reset", "transport control", 1, 1, cmdMusic},
		"bpm":    {"bpm <n>", "tempo, beat-quantized", 1, 1, cmdBPM},
		"swing":  {"swing <f>", "shuffle 0..0.5", 1, 1, cmdSwing},
		"tuning": {"tuning [cents]", "the cents the transport plays off A440, a semitone either way: a rip sets the recording's, a song its own; bare shows it", 0, 1, cmdTuning},
		"fill":   {"fill on|off", "slot-2 phrase auto-fill; soundlab starts with it off", 1, 1, cmdFill},
		"key":    {"key <root> <scale> [deg...]", "harmony: MIDI root, scale name, chord degrees", 2, -1, cmdKey},
		"mute":   {"mute music|sfx|all|none | mute slot <0-3> on|off | mute track <0-3> <n> on|off", "absolute channel mute state, or one slot's, or one track's of the pattern a slot plays, dropped when the slot changes pattern (it plays on in time)", 1, 4, cmdMute},
		"solo":   {"solo track <0-3> <n> on|off", "sound a slot's soloed tracks alone, its pattern playing on in time; dropped when the slot changes pattern", 4, 4, cmdSolo},
		"vol":    {"vol <f>", "sfx volume 0..1; music keeps its own level", 1, 1, cmdVol},
		"wait":   {"wait <ms>", "sleep (scripted audition pacing)", 1, 1, cmdWait},
	}
}

// cmdRun runs a script in the session in hand, as -s does from the start, so a
// recipe of verbs is one word in the REPL or the TUI's command line
func cmdRun(s *Session, a []string) error {
	switch strings.ToLower(filepath.Ext(a[0])) {
	case ".toml":
		return fmt.Errorf("%s: a document; load or merge it", a[0])
	case ".wav":
		return fmt.Errorf("%s: a recording; rip <file.wav> <name> hears it", a[0])
	case ".mid", ".midi":
		return fmt.Errorf("%s: a score; rip <file.mid> <name> reads it", a[0])
	}
	return RunScriptFile(s, s.path(a[0]))
}

// RunScriptFile runs the script at path as RunScript does, the script's relative
// paths read from its own directory while it runs, a nested run from its own.
func RunScriptFile(s *Session, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	prev := s.base
	s.base = filepath.Dir(abs)
	defer func() { s.base = prev }()
	return RunScript(s, f)
}

// Execute runs one line. '#' starts a comment; a leading '-' tolerates
// failure, '!' requires it — the test harness needs both directions.
func Execute(s *Session, raw string) error {
	line := raw
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	tolerate, mustFail := false, false
	switch line[0] {
	case '-':
		tolerate, line = true, strings.TrimSpace(line[1:])
	case '!':
		mustFail, line = true, strings.TrimSpace(line[1:])
	}
	if line == "" {
		return nil
	}

	s.beginLine(strings.Fields(line)[0])
	err := dispatch(s, line)
	s.endLine()
	switch {
	case errors.Is(err, ErrQuit):
		return err
	case mustFail && err == nil:
		return fmt.Errorf("expected failure: %s", line)
	case mustFail:
		fmt.Fprintf(s.out, "expected error: %v\n", err)
		return nil
	case tolerate && err != nil:
		fmt.Fprintf(s.out, "ignored: %v\n", err)
		return nil
	}
	return err
}

// RunScript runs lines until the end or a quit, strict: any outcome the line did
// not ask for with - or ! aborts with its line number, since an unprefixed failure
// in a script is a bug in the script or the editor.
func RunScript(s *Session, in io.Reader) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	ln := 0
	for sc.Scan() {
		ln++
		err := Execute(s, sc.Text())
		if errors.Is(err, ErrQuit) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("line %d: %w", ln, err)
		}
	}
	return sc.Err()
}

func dispatch(s *Session, line string) error {
	fields := splitArgs(line)
	if len(fields) == 0 {
		return nil
	}
	c, ok := commands[fields[0]]
	if !ok {
		return fmt.Errorf("unknown command %q (try help)", fields[0])
	}
	args := fields[1:]
	if len(args) < c.min || (c.max >= 0 && len(args) > c.max) {
		return fmt.Errorf("usage: %s", c.usage)
	}
	return c.fn(s, args)
}

// splitArgs splits a line on blanks, an argument that opens with a double quote
// running to the closing one, so a path with a space can be named; the quotes go
func splitArgs(line string) []string {
	var out []string
	var cur strings.Builder
	in, open := false, false
	for _, r := range line {
		switch {
		case r == '"' && (in || !open):
			in, open = !in, true
		case !in && (r == ' ' || r == '\t' || r == '\r' || r == '\n'):
			if open {
				out = append(out, cur.String())
				cur.Reset()
				open = false
			}
		default:
			cur.WriteRune(r)
			open = true
		}
	}
	if open {
		out = append(out, cur.String())
	}
	return out
}

// keyHelp is one line of the TUI's key reference: the keys and what they do
type keyHelp struct{ keys, what string }

// helpSection is the key reference of one view; the ? overlay shows the view's
// own and the global keys, `help keys` all of them
type helpSection struct {
	title string
	lines []keyHelp
}

var (
	helpBrowse = helpSection{"browser", []keyHelp{
		{"j k PgUp PgDn", "move; g G the ends; / finds the next name containing a term"},
		{"h l", "patterns, sounds, voices"},
		{"space", "play a sound or a voice's note; put a pattern in its role's slot, or play and pause"},
		{"0-3", "put the pattern in that slot, or take it out"},
		{"n", "new: a pattern opens in the editor, empty, playing along in a free slot; a sound in the inspector"},
		{"e", "edit: a pattern in the editor's grid, a sound in the inspector"},
		{"enter", "inspect: j k move, h l fold, H L all, enter edit, a add, x delete"},
		{"a v r", "apply, validate, revert"},
		{"u", "undo the last edit"},
		{"c w", "clone, write one to a file"},
		{"+ / d d", "mix into another / delete"},
		{"p", "a new pattern of the tracks yanked in the editor"},
		{"m", "the music back to its first bar"},
	}}
	helpEditor = helpSection{"editor", []keyHelp{
		{"tab ⇧tab", "the keys, the grid, then the mixer"},
		{"enter", "apply, and play it in its slot"},
		{"^R", "record a take over the pattern's length; again writes it in, a track a voice"},
		{"^D ^D", "delete the pattern, out of its slot too"},
		{"esc", "close; the pattern stays, ^S saves it"},
	}}
	helpKeys = helpSection{"keys", []keyHelp{
		{"z s x … m", "play the first octave from C, q 2 w … u the second"},
		{"i 9 o 0 p", "the third's C to E; , l . ; / the second's again"},
		{"• ●", "a note of the key, its root"},
		{"↑ ↓", "previous, next voice"},
		{"← → { }", "octave"},
		{"- =", "velocity"},
		{"[ ]", "note length"},
		{"\\", "the pattern's length: 1, 2 or 4 bars"},
		{"⌫", "drop the take's last note"},
	}}
	helpGrid = helpSection{"grid", []keyHelp{
		{"h l ← →", "step; H L a beat, PgUp PgDn a bar, g G the ends"},
		{"j k ↑ ↓", "track; in the roll, pitch (PgUp PgDn an octave)"},
		{"space", "hit or note on and off"},
		{"1-9 0 + -", "velocity, .1 to 1; nudge it"},
		{"c", "chance: 1, .75, .5, .25"},
		{"[ ] , . { }", "length, degree, octave of a note"},
		{"t T", "next voice of the family, the other family"},
		{"f", "follow the chord"},
		{"m", "mute the track in the slot the pattern plays in, or let it sound"},
		{"a / x x", "add a track of the voice in hand / delete the track"},
		{"y Y / p", "yank the track, add it to the yank or take it out / paste the yank into this pattern"},
		{"s", "steps: the pattern's length"},
		{"u", "undo the last edit"},
		{"r", "piano roll of a tonal track, and back"},
	}}
	helpMixer = helpSection{"mixer", []keyHelp{
		{"j k ↑ ↓", "the slots' titles and tracks; PgUp PgDn a page, g G the ends"},
		{"", "the rows stay where the cursor leaves them"},
		{"space m", "mute the cursor's slot or track, or let it sound; it plays on in time"},
		{"s", "solo the cursor's track: the slot's soloed tracks sound alone"},
		{"e / n", "edit what the cursor's slot plays / a new pattern in it"},
	}}
	helpGlobal = helpSection{"everywhere", []keyHelp{
		{"tab ⇧tab", "next, previous pane: the browser, mixer and log, or the editor's tabs and the mixer"},
		{"esc", "from the mixer or the log back to the view's own pane"},
		{"< >", "tempo down, up by 5 bpm"},
		{":", "command line; help lists the commands"},
		{"^O", "files: load or merge a document, rip a recording, save the tab's document into a directory"},
		{"? / ^S / ^Q", "these keys / save / quit"},
	}}
	helpFiles = helpSection{"files", []keyHelp{
		{"j k PgUp PgDn", "move; g G the ends"},
		{"enter l →", "open the directory; load the document (sounds or patterns by its tables); rip the recording under a name asked for"},
		{"m", "merge the document onto the one in hand instead"},
		{"s", "save the tab's document, patterns or sounds, into this directory under a name asked for"},
		{"h ← ⌫", "up a directory"},
		{"esc ^O", "close"},
	}}
)

// helpFor is the key reference of where the keys are: the browser, or the editor
// and the tab it shows
func (a *tuiApp) helpFor() []helpSection {
	if a.files.open {
		return []helpSection{helpFiles}
	}
	if a.view != viewEdit || a.ed == nil {
		return []helpSection{helpBrowse}
	}
	if a.ed.tab == focusKeys {
		return []helpSection{helpEditor, helpKeys}
	}
	return []helpSection{helpEditor, helpGrid}
}

func cmdHelp(s *Session, a []string) error {
	if len(a) == 1 && a[0] == "keys" {
		for _, sec := range []helpSection{helpBrowse, helpEditor, helpKeys, helpGrid, helpMixer, helpFiles, helpGlobal} {
			fmt.Fprintln(s.out, sec.title)
			for _, l := range sec.lines {
				fmt.Fprintf(s.out, "  %-14s %s\n", l.keys, l.what)
			}
		}
		return nil
	}
	if len(a) == 1 {
		c, ok := commands[a[0]]
		if !ok {
			return fmt.Errorf("unknown command %q", a[0])
		}
		fmt.Fprintf(s.out, "%s\n  %s\n", c.usage, c.help)
		return nil
	}
	fmt.Fprintln(s.out, "prefix '-' tolerates failure, '!' requires it; '#' comments; quote a path with a space")
	for _, k := range slices.Sorted(maps.Keys(commands)) {
		fmt.Fprintf(s.out, "  %-40s %s\n", commands[k].usage, commands[k].help)
	}
	return nil
}

// The kinds a document entry is of, in the order the browser's tabs stand
const (
	kindPattern = iota
	kindSound
	kindVoice
	kindCount
)

var kindWords = [kindCount]string{"pattern", "sound", "voice"}

func kindWord(kind int) string { return kindWords[kind] }

func kindArg(s string) (int, error) {
	if i := slices.Index(kindWords[:], s); i >= 0 {
		return i, nil
	}
	return 0, fmt.Errorf("want sound|pattern|voice, got %q", s)
}
