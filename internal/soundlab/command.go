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

		"load":     {"load sound|pattern <file>", "replace document from file (sound includes resolve within the file's dir; voices come with the sounds, songs with the patterns)", 2, 2, cmdLoad},
		"merge":    {"merge sound|pattern <file>", "overlay file onto document by name", 2, 2, cmdMerge},
		"builtin":  {"builtin sound|pattern|voice", "seed document from built-ins (patterns: current registry snapshot; sound seeds the voices too)", 1, 1, cmdBuiltin},
		"save":     {"save sound|pattern [file]", "write document; bare = provenance path (the sound document carries the voices)", 1, 2, cmdSave},
		"write":    {"write <name> <file>", "save one entry as standalone TOML (export: dirty/modified untouched)", 2, 2, cmdWrite},
		"export":   {"export <name> <file.wav> [variant]", "render to WAV; no variant = canonical take", 2, 3, cmdExport},
		"rip":      {"rip <file.wav> <name> [bars] [nodrums]", "hear a recording's beat, melody and bass as patterns of up to bars (4) bars each, <name>_beat_1 ..., and take its tempo and key; nodrums leaves the drum part out", 2, 4, cmdRip},
		"render":   {"render <file.wav> [bars]", "write what the slots hold, bars (4) bars at the transport's tempo and key, to a WAV through the engine's offline render", 1, 2, cmdRender},
		"corpus":   {"corpus <dir> [loops] [bars] [aug] [gen] [nowav]", "render loops (24) of bars (4) bars from the registered patterns into dir, each a WAV, its log-mel frames and the events that sound in it: what tool/rip-train trains the rip's model on; aug puts a room, a mix and voices of random genes behind each; gen draws beats, melodies and bass lines the bank never had; nowav keeps the frames and the labels only, which is all the trainer reads", 1, 6, cmdCorpus},
		"tune":     {"tune [generations] [file]", "search rip's parameters over loops the engine renders from the registered patterns; the best write to file (the first root's audio/rip.toml) and listen from now on; 8 generations take minutes", 0, 2, cmdTune},
		"notes":    {"notes <name>", "list the transcript of a recording ripped as <name>: each note's time, bar and step, pitch as the key's degree with any accidental, length and loudness, by line, after the tuning and the first bar", 1, 1, cmdNotes},
		"fit":      {"fit <name> [generations]", "fit the voices of a recording ripped as <name> again, over generations (12) of candidates the engine plays at its notes; they replace <name>_melody_voice and <name>_bass_voice", 1, 2, cmdFit},
		"compress": {"compress <name> [percent]", "match the bars of a recording ripped as <name> to as few one-bar patterns as a difference of percent (5) allows: the dictionary <name>_<part>_a..., and the song <name> that plays them in order, rendered and ripped back for a score", 1, 2, cmdCompress},
		"refine":   {"refine <name> [rounds]", "close the loop over a song compress made of a recording ripped as <name>: each round (3) fits the voices again and sweeps the tonal steps, each tried a semitone and an octave either way, the recording's strongest pitch there, a step earlier or later, longer or shorter, and silent, then a step a line leaves empty tries that pitch; every move kept when the song sounds more like the recording; minutes a round", 1, 2, cmdRefine},
		"bench":    {"bench [loops] [bars]", "grade the analysis, and the model when one loaded, on fresh loops (8) of bars (4) bars the engine renders, clean and augmented: the mean score of each rip against the loop's expected rip", 0, 2, cmdBench},
		"run":      {"run <file.slab>", "run a script's lines in this session, its relative paths read from its own directory, strict as -s is: a line that fails stops it with its number; quit in it ends the script, not the session", 1, 1, cmdRun},
		"song":     {"song <name> [file.wav|play|score]", "show a song's bars; render it to a WAV; play it through the sequencer, the slots changed at the bar lines (music stop ends it); or score it, how it sounds against the recording ripped as <name> and how it rips back", 1, 2, cmdSong},

		"ls":    {"ls sound|pattern|voice|song [glob]", "list document entries (* marks dirty)", 1, 2, cmdLs},
		"show":  {"show <name>[.path]", "bare name = full TOML; deeper = leaf value or keys", 1, 1, cmdShow},
		"where": {"where", "transport bar:step and per-slot sounding pattern", 0, 0, cmdWhere},
		"stat":  {"stat", "backend, silent mode, play stats, dirty entries", 0, 0, cmdStat},

		"new":   {"new sound|pattern|voice <name>", "create an entry seeded with a minimal valid spec", 2, 2, cmdNew},
		"cp":    {"cp <src> <dst>", "duplicate an entry", 2, 2, cmdCp},
		"mv":    {"mv <src> <dst>", "rename (sound noise variants re-roll: rng seeds from name)", 2, 2, cmdMv},
		"mix":   {"mix <src> <dst>", "append src's tracks/layers into dst (same kind; src unchanged; validate dst after)", 2, 2, cmdMix},
		"take":  {"take <pattern> <voice> <pos>:<midi>[:<steps>[:<vel>]]...", "add a track of a tonal voice playing the notes at their steps, each on the key's nearest degree", 3, -1, cmdTake},
		"set":   {"set <name.path> <value...>", "set a leaf field; value may contain spaces", 2, -1, cmdSet},
		"add":   {"add <name.path>", "append a zero element to a list field", 1, 1, cmdAdd},
		"del":   {"del <name>|<name.path.idx>", "delete a document entry or a list element", 1, 1, cmdDel},
		"unset": {"unset <name.path>", "reset a field to its zero value (nil for optional tables/lists)", 1, 1, cmdUnset},

		"validate": {"validate [name|all]", "check one entry or all (all fails if any is invalid)", 0, 1, cmdValidate},
		"apply":    {"apply [name|all]", "register into the engine; bare/all = dirty entries only", 0, 1, cmdApply},
		"revert":   {"revert [name|all]", "restore from registry; bare/all = dirty entries only", 0, 1, cmdRevert},

		"play": {"play <name> [vol]", "audition document state (canonical take); falls back to registry", 1, 2, cmdPlay},
		"hit":  {"hit kick|snare|hihat|clap [vol]", "audition a drum", 1, 2, cmdHit},
		"note": {"note <midi> [vel] [steps] [voice]", "tonal audition via the melody slot (needs music running); a dirty voice auto-applies", 1, 4, cmdNote},
		"slot": {"slot <0|1|2|3> <pattern|-> [fade_ms] [q]", "assign a pattern (0 rhythm, 1 melody, 2 fill, 3 bass); dirty patterns auto-apply; - = silence", 2, 4, cmdSlot},

		"music":  {"music start|stop|reset", "transport control", 1, 1, cmdMusic},
		"bpm":    {"bpm <n>", "tempo, beat-quantized", 1, 1, cmdBPM},
		"swing":  {"swing <f>", "shuffle 0..0.5", 1, 1, cmdSwing},
		"tuning": {"tuning [cents]", "the cents the transport plays off A440, a semitone either way: a rip sets the recording's, a song its own; bare shows it", 0, 1, cmdTuning},
		"fill":   {"fill on|off", "slot-2 phrase auto-fill; soundlab starts with it off", 1, 1, cmdFill},
		"key":    {"key <root> <scale> [deg...]", "harmony: MIDI root, scale name, chord degrees", 2, -1, cmdKey},
		"mute":   {"mute music|sfx|all|none | mute slot <0-3> on|off", "absolute channel mute state, or one slot's (its pattern plays on in time)", 1, 3, cmdMute},
		"vol":    {"vol <f>", "sfx volume 0..1; music keeps its own level", 1, 1, cmdVol},
		"wait":   {"wait <ms>", "sleep (scripted audition pacing)", 1, 1, cmdWait},
	}
}

// cmdRun runs a script in the session in hand, as -s does from the start, so a
// recipe of verbs is one word in the REPL or the TUI's command line
func cmdRun(s *Session, a []string) error {
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

	err := dispatch(s, line)
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
		{"c w", "clone, write one to a file"},
		{"+ / d d", "mix into another / delete"},
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
		{"a / x x", "add a track of the voice in hand / delete the track"},
		{"s", "steps: the pattern's length"},
		{"r", "piano roll of a tonal track, and back"},
	}}
	helpMixer = helpSection{"mixer", []keyHelp{
		{"j k ↑ ↓", "the slots' titles and tracks; PgUp PgDn a page, g G the ends"},
		{"", "the rows stay where the cursor leaves them"},
		{"space m", "mute the cursor's slot, or let it sound; it plays on in time"},
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
