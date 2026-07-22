package main

// The verb table is the whole API: the REPL, script runner and (next pass)
// the TUI all dispatch through Execute, so nothing one front-end can do is
// invisible to another — that is what keeps the scripted E2E meaningful.

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
		"help": {"help [verb]", "list commands or describe one", 0, 1, cmdHelp},
		"quit": {"quit", "stop the engine and exit", 0, 0,
			func(*Session, []string) error { return errQuit }},

		"load":    {"load sound|pattern <file>", "replace document from file (sound includes resolve within the file's dir)", 2, 2, cmdLoad},
		"merge":   {"merge sound|pattern <file>", "overlay file onto document by name", 2, 2, cmdMerge},
		"builtin": {"builtin sound|pattern", "seed document from built-ins (patterns: current registry snapshot)", 1, 1, cmdBuiltin},
		"save":    {"save sound|pattern [file]", "write document; bare = provenance path", 1, 2, cmdSave},
		"export":  {"export <name> <file.wav> [variant]", "render to WAV; no variant = canonical take", 2, 3, cmdExport},

		"ls":    {"ls sound|pattern [glob]", "list document entries (* marks dirty)", 1, 2, cmdLs},
		"show":  {"show <name>[.path]", "bare name = full TOML; deeper = leaf value or keys", 1, 1, cmdShow},
		"where": {"where", "transport bar:step and per-slot sounding pattern", 0, 0, cmdWhere},
		"stat":  {"stat", "backend, silent mode, play stats, dirty entries", 0, 0, cmdStat},

		"new": {"new sound|pattern <name>", "create an empty entry (invalid until fields are set)", 2, 2, cmdNew},
		"cp":  {"cp <src> <dst>", "duplicate an entry", 2, 2, cmdCp},
		"mv":  {"mv <src> <dst>", "rename (sound noise variants re-roll: rng seeds from name)", 2, 2, cmdMv},
		"set": {"set <name.path> <value...>", "set a leaf field; value may contain spaces", 2, -1, cmdSet},
		"add": {"add <name.path>", "append a zero element to a list field", 1, 1, cmdAdd},
		"del": {"del <name>|<name.path.idx>", "delete a document entry or a list element", 1, 1, cmdDel},

		"validate": {"validate [name|all]", "check one entry or all (all fails if any is invalid)", 0, 1, cmdValidate},
		"apply":    {"apply [name|all]", "register into the engine; bare/all = dirty entries only", 0, 1, cmdApply},
		"revert":   {"revert [name|all]", "restore from registry; bare/all = dirty entries only", 0, 1, cmdRevert},

		"play": {"play <name> [vol]", "audition document state (canonical take); falls back to registry", 1, 2, cmdPlay},
		"hit":  {"hit kick|snare|hihat|clap [vol]", "audition a drum", 1, 2, cmdHit},
		"note": {"note <midi> [vel] [steps] [instr]", "tonal audition via the melody slot (needs music running)", 1, 4, cmdNote},
		"slot": {"slot <0|1|2> <pattern|-> [fade_ms] [q]", "assign a pattern; dirty patterns auto-apply; - = silence", 2, 4, cmdSlot},

		"music": {"music start|stop|reset", "transport control", 1, 1, cmdMusic},
		"bpm":   {"bpm <n>", "tempo, bar-quantized", 1, 1, cmdBPM},
		"swing": {"swing <f>", "shuffle 0..0.5", 1, 1, cmdSwing},
		"fill":  {"fill on|off", "slot-2 auto-fill (off while auditioning slot 2)", 1, 1, cmdFill},
		"key":   {"key <root> <scale> [deg...]", "harmony: MIDI root, scale name, chord degrees", 2, -1, cmdKey},
		"mute":  {"mute music|sfx|all|none", "absolute channel mute state", 1, 1, cmdMute},
		"vol":   {"vol <f>", "master volume 0..1", 1, 1, cmdVol},
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

func cmdHelp(s *Session, a []string) error {
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
