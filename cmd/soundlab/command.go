//go:build !vif_headless && !vif_noaudio && !wasm

package main

// The verb table is the whole API: the REPL, script runner and TUI all
// dispatch through Execute, so nothing one front-end can do is invisible to
// another — that is what keeps the scripted E2E meaningful.

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

var errQuit = errors.New("quit")

type command struct {
	usage    string
	help     string
	min, max int // arg count; max -1 = unlimited
	fn       func(*Session, []string) error
}

var commands map[string]*command

// init, not a var literal: cmdHelp closes over the table.
func init() {
	commands = map[string]*command{
		"help": {"help [verb|keys]", "list commands or describe one", 0, 1, cmdHelp},
		"quit": {"quit", "stop the engine and exit", 0, 0,
			func(*Session, []string) error { return errQuit }},

		"load":    {"load sound|pattern <file>", "replace document from file (sound includes resolve within the file's dir)", 2, 2, cmdLoad},
		"merge":   {"merge sound|pattern <file>", "overlay file onto document by name", 2, 2, cmdMerge},
		"builtin": {"builtin sound|pattern", "seed document from built-ins (patterns: current registry snapshot)", 1, 1, cmdBuiltin},
		"save":    {"save sound|pattern [file]", "write document; bare = provenance path", 1, 2, cmdSave},
		"write":   {"write <name> <file>", "save one entry as standalone TOML (export: dirty/modified untouched)", 2, 2, cmdWrite},
		"export":  {"export <name> <file.wav> [variant]", "render to WAV; no variant = canonical take", 2, 3, cmdExport},

		"ls":    {"ls sound|pattern [glob]", "list document entries (* marks dirty)", 1, 2, cmdLs},
		"show":  {"show <name>[.path]", "bare name = full TOML; deeper = leaf value or keys", 1, 1, cmdShow},
		"where": {"where", "transport bar:step and per-slot sounding pattern", 0, 0, cmdWhere},
		"stat":  {"stat", "backend, silent mode, play stats, dirty entries", 0, 0, cmdStat},

		"new":   {"new sound|pattern <name>", "create an entry seeded with a minimal valid spec", 2, 2, cmdNew},
		"cp":    {"cp <src> <dst>", "duplicate an entry", 2, 2, cmdCp},
		"mv":    {"mv <src> <dst>", "rename (sound noise variants re-roll: rng seeds from name)", 2, 2, cmdMv},
		"mix":   {"mix <src> <dst>", "append src's tracks/layers into dst (same kind; src unchanged; validate dst after)", 2, 2, cmdMix},
		"set":   {"set <name.path> <value...>", "set a leaf field; value may contain spaces", 2, -1, cmdSet},
		"add":   {"add <name.path>", "append a zero element to a list field", 1, 1, cmdAdd},
		"del":   {"del <name>|<name.path.idx>", "delete a document entry or a list element", 1, 1, cmdDel},
		"unset": {"unset <name.path>", "reset a field to its zero value (nil for optional tables/lists)", 1, 1, cmdUnset},

		"validate": {"validate [name|all]", "check one entry or all (all fails if any is invalid)", 0, 1, cmdValidate},
		"apply":    {"apply [name|all]", "register into the engine; bare/all = dirty entries only", 0, 1, cmdApply},
		"revert":   {"revert [name|all]", "restore from registry; bare/all = dirty entries only", 0, 1, cmdRevert},

		"play": {"play <name> [vol]", "audition document state (canonical take); falls back to registry", 1, 2, cmdPlay},
		"hit":  {"hit kick|snare|hihat|clap [vol]", "audition a drum", 1, 2, cmdHit},
		"note": {"note <midi> [vel] [steps] [instr]", "tonal audition via the melody slot (needs music running)", 1, 4, cmdNote},
		"slot": {"slot <0|1|2|3> <pattern|-> [fade_ms] [q]", "assign a pattern (0 rhythm, 1 melody, 2 fill, 3 bass); dirty patterns auto-apply; - = silence", 2, 4, cmdSlot},

		"music": {"music start|stop|reset", "transport control", 1, 1, cmdMusic},
		"bpm":   {"bpm <n>", "tempo, beat-quantized", 1, 1, cmdBPM},
		"swing": {"swing <f>", "shuffle 0..0.5", 1, 1, cmdSwing},
		"fill":  {"fill on|off", "slot-2 phrase auto-fill; soundlab starts with it off", 1, 1, cmdFill},
		"key":   {"key <root> <scale> [deg...]", "harmony: MIDI root, scale name, chord degrees", 2, -1, cmdKey},
		"mute":  {"mute music|sfx|all|none | mute slot <0-3> on|off", "absolute channel mute state, or one slot's (its pattern plays on in time)", 1, 3, cmdMute},
		"vol":   {"vol <f>", "sfx volume 0..1; music keeps its own level", 1, 1, cmdVol},
		"wait":  {"wait <ms>", "sleep (scripted audition pacing)", 1, 1, cmdWait},
	}
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
	case errors.Is(err, errQuit):
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

func dispatch(s *Session, line string) error {
	fields := strings.Fields(line)
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
		{"h l", "patterns ⇄ sounds"},
		{"space", "play a sound; put a pattern in its role's slot, or play and pause"},
		{"0-3", "put the pattern in that slot, or take it out"},
		{"b", "beat editor on the pattern"},
		{"p", "keys: a scratch keyboard of the tonal voices over the music; a take is the one thing it writes"},
		{"enter", "inspect: j k move, h l fold, H L all, enter edit, a add, x delete"},
		{"a v r", "apply, validate, revert"},
		{"n c w", "new, clone, write one to a file"},
		{"+ / d d", "mix into another / delete"},
		{"m", "the music back to its first bar"},
	}}
	helpKeys = helpSection{"keys", []keyHelp{
		{"z s x … m", "play the first octave from C, q 2 w … u the second"},
		{"i 9 o 0 p", "the third's C to E; , l . ; / the second's again"},
		{"• ●", "a note of the key, its root"},
		{"↑ ↓", "previous, next voice"},
		{"← → { }", "octave"},
		{"- =", "velocity"},
		{"[ ]", "note length"},
		{"^R", "record; again commits the take as a pattern, rec_N"},
		{"\\", "take of 1, 2 or 4 bars"},
		{"⌫", "drop the take's last note"},
		{"esc", "back to the browser or the beat view, committing a take"},
	}}
	helpBeat = helpSection{"beat", []keyHelp{
		{"h l ← →", "step; H L a beat, PgUp PgDn a bar, g G the ends"},
		{"j k ↑ ↓", "track; in the roll, pitch (PgUp PgDn an octave)"},
		{"space", "hit or note on and off"},
		{"enter", "apply, and play it in its role's slot"},
		{"1-9 0 + -", "velocity, .1 to 1; nudge it"},
		{"c", "chance: 1, .75, .5, .25"},
		{"p", "keys over the music, the pattern left as it is; esc comes back"},
		{"[ ] , . { }", "length, degree, octave of a note"},
		{"t T", "next voice of the family, the other family"},
		{"f", "follow the chord"},
		{"a / x x", "add a track of the voice in hand / delete the track"},
		{"s", "steps: the pattern's length"},
		{"r", "piano roll of a tonal track, and back"},
		{"esc", "close"},
	}}
	helpMixer = helpSection{"mixer", []keyHelp{
		{"j k ↑ ↓", "the slots' titles and tracks; PgUp PgDn a page, g G the ends"},
		{"", "the rows stay where the cursor leaves them"},
		{"space m", "mute the cursor's slot, or let it sound; it plays on in time"},
	}}
	helpGlobal = helpSection{"everywhere", []keyHelp{
		{"tab ⇧tab", "next, previous pane: the browser, mixer and log, or an editor and the mixer"},
		{"esc", "from the mixer or the log back to the view's own pane"},
		{"< >", "tempo down, up by 5 bpm"},
		{":", "command line; help lists the commands"},
		{"? / ^S / ^Q", "these keys / save / quit"},
	}}
)

func helpFor(v view) helpSection {
	switch v {
	case viewKeys:
		return helpKeys
	case viewBeat:
		return helpBeat
	}
	return helpBrowse
}

func cmdHelp(s *Session, a []string) error {
	if len(a) == 1 && a[0] == "keys" {
		for _, sec := range []helpSection{helpBrowse, helpKeys, helpBeat, helpMixer, helpGlobal} {
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
	fmt.Fprintln(s.out, "prefix '-' tolerates failure, '!' requires it; '#' comments; paths cannot contain spaces")
	for _, k := range slices.Sorted(maps.Keys(commands)) {
		fmt.Fprintf(s.out, "  %-40s %s\n", commands[k].usage, commands[k].help)
	}
	return nil
}

func kindArg(s string) (sound bool, err error) {
	switch s {
	case "sound":
		return true, nil
	case "pattern":
		return false, nil
	}
	return false, fmt.Errorf("want sound|pattern, got %q", s)
}
