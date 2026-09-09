// The help text, as one table.
//
// Flags are registered where the group that owns them lives; this decides how they
// are *presented*, which is a different problem with a different shape. The `flag`
// package prints one paragraph per registered name, so a flag with a short and a
// long form spends four lines saying one thing and an alias reads as a separate
// option. Here a flag is one line, its forms share it, and the sections are the
// order somebody looking for a flag would look in: what this process is, what it
// loads, what it shows, what it runs, and what it records.
//
// The hint is a hint. It says what the flag selects and what the default is when
// that is not obvious; why it exists belongs in doc/ and is linked from there.
//
// TestHelpListsEveryFlag walks the registered set against this table in both
// directions, so a flag added without a line fails a test rather than going
// unmentioned.

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

// helpSections is the whole of the help, in print order.
func helpSections() []flagSection {
	return []flagSection{{
		title: "Session",
		lines: []flagLine{
			{names: []string{"host"}, arg: "<addr>", hint: "Bind and play a session, e.g. :7777"},
			{names: []string{"serve"}, arg: "<addr>", hint: "Bind a headless session with no local cursor"},
			{names: []string{"join"}, arg: "<addr>", hint: "Join a session at host:port, or at the vif://host:port/name a link carries"},
			{names: []string{"name"}, arg: "<name>", hint: "Name this host answers to, so one address can serve several sessions"},
			{names: []string{"players"}, arg: "<n>", hint: fmt.Sprintf(
				"Roster ceiling including self, 2..%d; unset holds the whole roster and starts on the first guest",
				parameter.MaxPlayers)},
			{names: []string{"authority"}, arg: "host|migrate",
				hint: "Where authorship goes when the authoring participant leaves; default host with -serve, migrate otherwise"},
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
			{names: []string{"d", "config-embedded"}, hint: "Use the embedded FSM and content, ignoring -g and -f"},
			{names: []string{"config-dir"}, arg: "<dir>", hint: "Configuration root holding game/ input/ audio/ content/"},
			{names: []string{"g", "config-game"}, arg: "<name|path>", hint: "Installed game name, game.toml, or a game directory"},
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
			{names: []string{"check"}, hint: "Validate the resolved game, keymap, audio and content config, then exit"},
			{names: []string{"schema"}, hint: "Print the FSM schema as JSON, then exit"},
		},
	}, {
		title: "Diagnostics",
		lines: []flagLine{
			{names: []string{"l", "log"}, arg: "[=DIR]", hint: "Enable logging; DIR overrides " + paths.DefaultLogDir()},
			{names: []string{"lv", "log-level"}, arg: "<level>", hint: "trace, debug, info, warn or error; implies -l"},
			{names: []string{"ls", "log-scope"}, arg: "<spec>", hint: "Which subsystems log; see Scopes below; implies -l"},
			{names: []string{"lt", "log-stat"}, arg: "<ticks>", hint: "Status snapshot period in game ticks, 0 disables; implies -l"},
			{names: []string{"lr", "log-recorder"}, arg: "<ticks>", hint: "Flight recorder depth in game ticks, 0 disables; implies -l"},
			{names: []string{"log-stdout"}, hint: "Write the log to stdout as JSON instead of to a file; implies -l"},
			{names: []string{"j", "journal"}, arg: "[=DIR]", hint: "Record a replay journal; DIR overrides " + paths.DefaultJournalDir()},
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

// scopeRow is one selectable scope: the word and the letter that mean it. It is a
// copy of vlog's own table rather than an export of it, because help text is not a
// runtime dependency worth widening a package's surface for —
// TestScopeNoteMatchesTheParser feeds every entry back through vlog.ParseScopes,
// so a scope renamed there fails a test here.
var scopeRows = []struct{ name, letter string }{
	{"app", "a"}, {"fsm", "f"}, {"event", "e"}, {"dispatch", "d"}, {"push", "p"},
	{"input", "i"}, {"stat", "s"}, {"rec", "r"}, {"lock", "l"}, {"tap", "t"},
}

// scopeNote is the one piece of grammar a hint cannot carry, so it is printed once
// rather than crammed into -ls.
//
// It is also the correction to what the old hint implied by listing `all` beside
// `+dispatch`: `all` is every scope, dispatch included, so `all+dispatch` says the
// same thing twice.
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

// writeUsage prints the whole help. The caller decides where: stdout and exit
// zero when it was asked for, stderr and a failing exit when the flags were wrong.
func writeUsage(w io.Writer) {
	fmt.Fprint(w, "vi-fighter — a modal-motion arcade game\n\nUsage:\n  vif [flags]\n")

	width := 0
	for _, section := range helpSections() {
		for _, line := range section.lines {
			if n := len(line.render()); n > width {
				width = n
			}
		}
	}
	for _, section := range helpSections() {
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

// registeredFlagNames is every name this binary's flag set answers to, for the
// test that keeps the table above honest.
//
// `testing` registers its own flags on the same set when the package is built as a
// test binary, so `-test.` is skipped: those belong to the harness rather than to
// the program, and no help this program prints should mention them.
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
