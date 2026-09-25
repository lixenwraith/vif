package main

import (
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/paths"
)

// flagLine is one flag: every name it answers to, the argument it takes, and the
// single line that describes it.
type flagLine struct {
	names []string
	arg   string
	hint  string
}

// flagSection groups the lines under one heading.
type flagSection struct {
	title string
	lines []flagLine
}

// helpSections is the whole of the help and the manual, one line per flag with every
// form on it, in the order somebody looking for a flag would look. The log and
// journal defaults are parameters: the help prints resolved paths, the manual XDG.
func helpSections(logDir, journalDir string) []flagSection {
	return []flagSection{{
		title: "Session",
		lines: []flagLine{
			{names: []string{"host"}, arg: "<addr>", hint: "Bind and play a session, e.g. :7777"},
			{names: []string{"serve"}, arg: "<addr>", hint: "Bind a headless session with no local cursor"},
			{names: []string{"join"}, arg: "<addr>", hint: "Join a session at host:port, at the vif://host:port/name a link carries, or at a wss:// route"},
			{names: []string{"name"}, arg: "<name>", hint: "Name this host answers to, so one address can serve several sessions"},
			{names: []string{"players"}, arg: "<n>", hint: fmt.Sprintf(
				"Roster ceiling including self, 2..%d; unset holds the whole roster and starts on the first guest",
				parameter.MaxPlayers)},
			{names: []string{"authority"}, arg: "host|migrate",
				hint: "Where authorship goes when the authoring participant leaves; default host with -serve, migrate otherwise"},
			{names: []string{"slow-window"}, arg: "<dur>",
				hint: fmt.Sprintf("Window a participant's lateness is judged over; host only, 0 never evicts (default %s)", parameter.NetworkSlowWindow)},
			{names: []string{"slow-late"}, arg: "<n>",
				hint: fmt.Sprintf("Evict at this many late epochs per second over the window; 0 ignores lateness (default %g)", parameter.NetworkSlowLatePerSecond)},
			{names: []string{"slow-bytes"}, arg: "<n>",
				hint: fmt.Sprintf("...and at least this many bytes per second on its link; 0 ignores throughput (default %d)", parameter.NetworkSlowBytesPerSecond)},
			{names: []string{"listen"}, arg: "<addr>",
				hint: "Address this participant is dialled back on; -join only, default the host's port then an ephemeral one"},
			{names: []string{"no-advertise"},
				hint: "Keep this participant's address out of the session; -join only, and it is never elected"},
			{names: []string{"size"}, arg: "<WxH>", hint: "Terminal-equivalent size for a run that has no terminal"},
			{names: []string{"probe"}, arg: "<addr>", hint: "Health and metrics endpoint; -serve only"},
			{names: []string{"first-join"}, arg: "<dur>", hint: "Exit if no guest connects within this; -serve only, 0 waits forever"},
			{names: []string{"empty"}, arg: "<dur>", hint: "Exit this long after the last guest leaves; -serve only, 0 keeps the session"},
			{names: []string{"drain"}, arg: "<dur>", hint: "How long a termination signal waits for an empty roster; -serve only, 0 exits at once"},
		},
	}, {
		title: "Configuration",
		lines: []flagLine{
			{names: []string{"d", "config-embedded"}, hint: "Use the embedded scenario and content, ignoring -s and -f"},
			{names: []string{"config-dir"}, arg: "<dir>", hint: "Configuration root holding scenario/ input/ audio/ content/ image/"},
			{names: []string{"s", "config-scenario"}, arg: "<name|path>", hint: "Installed scenario name, scenario.toml, or a scenario directory"},
			{names: []string{"f", "config-content"}, arg: "<path>", hint: "Content directory, or a single content file"},
			{names: []string{"k", "config-keymap"}, arg: "<path>", hint: "Keymap TOML"},
			{names: []string{"config-music"}, arg: "<path>", hint: "Music pattern override TOML"},
			{names: []string{"config-sounds"}, arg: "<path>", hint: "Sound definition override TOML"},
		},
	}, {
		title: "Presentation and audio",
		lines: []flagLine{
			{names: []string{"color"}, arg: "auto|256|true", hint: "Colour depth; auto detects the terminal"},
			{names: []string{"mute"}, arg: "[=false]", hint: "Start muted, which is the default; -mute=false starts with sound"},
			{names: []string{"ab", "audio-backend"}, arg: "<name>", hint: "Force an audio backend instead of detecting one"},
		},
	}, {
		title: "Run",
		lines: []flagLine{
			{names: []string{"seed"}, arg: "<n>", hint: "Root RNG seed; 0 draws one and logs it"},
			{names: []string{"speed"}, arg: "<rate>", hint: `Simulation rate 1/8 1/4 1/2 1 2 4 8; with -script also "max" for no wall pacing`},
			{names: []string{"script"}, arg: "<path>", hint: "Run an authored deterministic TOML tick script"},
			{names: []string{"watch"}, hint: "Present a -script run on this terminal instead of running it headlessly"},
			{names: []string{"replay"}, arg: "<path>", hint: "Replay a recorded journal instead of playing"},
			{names: []string{"check"}, hint: "Validate the resolved scenario, keymap, audio and content, then exit"},
			{names: []string{"schema"}, hint: "Print the FSM schema as JSON, then exit"},
		},
	}, {
		title: "Diagnostics",
		lines: []flagLine{
			{names: []string{"l", "log"}, arg: "[=DIR]", hint: "Enable logging; DIR overrides " + logDir},
			{names: []string{"lv", "log-level"}, arg: "<level>", hint: "trace, debug, info, warn or error; implies -l"},
			{names: []string{"ls", "log-scope"}, arg: "<spec>", hint: "Which subsystems log; see Scopes below; implies -l"},
			{names: []string{"lt", "log-stat"}, arg: "<ticks>", hint: "Status snapshot period in game ticks, 0 disables; implies -l"},
			{names: []string{"lr", "log-recorder"}, arg: "<ticks>", hint: "Flight recorder depth in game ticks, 0 disables; implies -l"},
			{names: []string{"log-session-id"}, arg: "<id>", hint: "Attach a session ID to every application log record; implies -l"},
			{names: []string{"log-stdout"}, hint: "Write the log to stdout as JSON instead of to a file; implies -l"},
			{names: []string{"j", "journal"}, arg: "[=DIR]", hint: "Record a replay journal; DIR overrides " + journalDir},
			{names: []string{"dev"}, arg: "[=false]", hint: "Capture runtime stderr to a file; on by default for -race builds"},
		},
	}, {
		title: "Help",
		lines: []flagLine{
			{names: []string{"h", "help"}, hint: "Print this and exit"},
			{names: []string{"version"}, hint: "Print the module version and commit a package should report"},
		},
	}}
}

// scopeRows copies vlog's table rather than widening its surface for help text;
// TestScopeNoteMatchesTheParser feeds every entry back through vlog.ParseScopes.
var scopeRows = []struct{ name, letter string }{
	{"app", "a"}, {"fsm", "f"}, {"event", "e"}, {"dispatch", "d"}, {"push", "p"},
	{"input", "i"}, {"stat", "s"}, {"rec", "r"}, {"lock", "l"}, {"tap", "t"},
}

// scopeNote is the one piece of grammar a hint cannot carry, so it is printed once
// rather than crammed into -ls.
func scopeNote() string {
	var names, letters strings.Builder
	for _, row := range scopeRows {
		width := max(len(row.name), len(row.letter)) + 2
		fmt.Fprintf(&names, "%-*s", width, row.name)
		fmt.Fprintf(&letters, "%-*s", width, row.letter)
	}
	return "Scopes (-ls)\n" +
		"  Names     " + strings.TrimRight(names.String(), " ") + "\n" +
		"  Letters   " + strings.TrimRight(letters.String(), " ") + "\n" +
		"  Sets      all is every scope, none is nothing\n" +
		`  Combine   join names or letters with + or , — "app+fsm+stat" and "afs" are one set` + "\n" +
		"  Adjust    lead with + or - to add to or remove from the set already selected"
}

// summary names the program in the help and in the manual's NAME section.
const summary = "a modal-motion arcade game"

// writeUsage prints the whole help. The caller decides where: stdout and exit
// zero when it was asked for, stderr and a failing exit when the flags were wrong.
func writeUsage(w io.Writer) {
	fmt.Fprint(w, "vif — "+summary+"\n\nUsage:\n  vif [flags]\n")

	sections := helpSections(paths.DefaultLogDir(), paths.DefaultJournalDir())
	width := 0
	for _, section := range sections {
		for _, line := range section.lines {
			if n := len(line.render()); n > width {
				width = n
			}
		}
	}
	for _, section := range sections {
		fmt.Fprintf(w, "\n%s\n", section.title)
		for _, line := range section.lines {
			fmt.Fprintf(w, "  %-*s  %s\n", width, line.render(), line.hint)
		}
	}
	fmt.Fprintf(w, "\n%s\n", scopeNote())
}

// render is the left column: every form of the flag, then its argument.
func (l flagLine) render() string {
	forms := make([]string, len(l.names))
	for i, name := range l.names {
		forms[i] = "-" + name
	}
	out := strings.Join(forms, ", ")
	if l.arg == "" {
		return out
	}
	if strings.HasPrefix(l.arg, "[") {
		return out + l.arg
	}
	return out + " " + l.arg
}

// registeredFlagNames is every name this binary answers to, for the test that keeps
// the table honest; `-test.` flags belong to the test harness, not the program.
func registeredFlagNames() []string {
	var out []string
	flag.VisitAll(func(f *flag.Flag) {
		if strings.HasPrefix(f.Name, "test.") {
			return
		}
		out = append(out, f.Name)
	})
	return out
}

// manualStateDir replaces the resolved state path, which would put the generating
// user's home into a committed page.
const manualStateDir = "$XDG_STATE_HOME/" + paths.AppDirName + "/"

// writeManual renders doc/vif.6 from the help table; TestManualIsTheHelpTable
// keeps the committed page equal to it.
func writeManual(w io.Writer) {
	fmt.Fprintf(w, `.\" Generated from cmd/vif/usage.go by TestManualIsTheHelpTable; do not edit.
.TH VIF 6 "" vif
.SH NAME
vif \- %s
.SH SYNOPSIS
.B vif
.RI [ flags ]
.SH DESCRIPTION
Without flags,
.B vif
starts a solo game in the terminal.
The flags below also host, join or serve a networked session,
replay or script a run, and validate the installed configuration.
.PP
Each resource resolves from its own flag, then
.BR \-config\-dir ,
then the user configuration root, then each system root,
and finally the copy compiled into the binary.
.SH OPTIONS
`, roff(summary))
	for _, section := range helpSections(manualStateDir+paths.LogDirName, manualStateDir+paths.JournalDirName) {
		fmt.Fprintf(w, ".SS %s\n", roff(section.title))
		for _, line := range section.lines {
			fmt.Fprintf(w, ".TP\n.B %s\n%s\n", roff(line.render()), roff(line.hint))
		}
	}
	note := strings.Split(scopeNote(), "\n")
	fmt.Fprintf(w, ".SS %s\n.nf\n", roff(note[0]))
	for _, line := range note[1:] {
		fmt.Fprintln(w, roff(line))
	}
	fmt.Fprint(w, `.fi
.SH FILES
.TP
.I $XDG_CONFIG_HOME/vif
User configuration root, normally
.IR ~/.config/vif .
.TP
.I $XDG_CONFIG_DIRS/vif
System configuration roots, normally
.IR /etc/xdg/vif ,
and on FreeBSD
.I /usr/local/etc/xdg/vif
before it.
.TP
.I $XDG_STATE_HOME/vif
Logs and journals, normally
.IR ~/.local/state/vif .
.SH SEE ALSO
https://github.com/lixenwraith/vi\-fighter
`)
}

// roff escapes one line of man(7) text: backslashes, hyphens as minus, a leading
// control character, and non-ASCII as an escape both mandoc and groff read.
func roff(s string) string {
	var b strings.Builder
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "'") {
		b.WriteString(`\&`)
	}
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\e`)
		case r == '-':
			b.WriteString(`\-`)
		case r > '~':
			fmt.Fprintf(&b, `\[u%04X]`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// writeVersion prints what a downstream package and a bug report need. The Go
// toolchain stamps both from VCS, so no build flag has to supply them; a build
// from an unversioned tree reports "(devel)" and no commit.
func writeVersion(w io.Writer) {
	version, revision := "unknown", ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		version = bi.Main.Version
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				revision = s.Value
			}
		}
	}
	if revision != "" {
		fmt.Fprintf(w, "vif %s (%s)\n", version, revision)
		return
	}
	fmt.Fprintf(w, "vif %s\n", version)
}
