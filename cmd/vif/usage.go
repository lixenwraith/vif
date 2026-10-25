package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
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
func helpSections(logDir, journalDir, musicDir string) []flagSection {
	return []flagSection{{
		title: "Session",
		lines: []flagLine{
			{names: []string{"host"}, arg: "<doors>", hint: "Play at once and open the game to participants at one or more doors, comma-separated: :7777 for tcp, ws://:7777 or wss://:7777 for WebSocket"},
			{names: []string{"serve"}, arg: "<doors>", hint: "Bind a headless session with no local cursor at one or more doors, e.g. :7777 or :7777,wss://:7778"},
			{names: []string{"cert"}, arg: "<file>", hint: "Certificate chain a wss:// door serves, PEM; for -host, -serve or :host"},
			{names: []string{"key"}, arg: "<file>", hint: "The certificate's private key, PEM; default the -cert file"},
			{names: []string{"selfsign"}, hint: "Serve wss:// with a certificate made for this run, in place of -cert: encrypted, and taken as it is by a terminal that joins"},
			{names: []string{"ca"}, arg: "<file|sha256:hex>", hint: "What a wss:// host's certificate is trusted by when joining, beyond the system's roots and a self-signed one taken as it is: a PEM file of its issuer, or the certificate's own fingerprint"},
			{names: []string{"join"}, arg: "<addr>", hint: "Join a session at [tcp://|vif://]host:port[/name] or a ws(s):// URL, or a new one an http(s):// site creates; tcp when no scheme is given"},
			{names: []string{"name"}, arg: "<name>", hint: "Name this host answers to, so one address can serve several sessions"},
			{names: []string{"players"}, arg: "<n>", hint: fmt.Sprintf(
				"Roster ceiling including self, 2..%d (with -serve, guests only, 1..%d); unset holds the whole roster; with a -join site, the one requested",
				parameter.MaxPlayers, parameter.MaxPlayers)},
			{names: []string{"bot"}, arg: "[N[:graph]|graph]", hint: "Add bots alongside you (default 1:default); -host/-join keep your player"},
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
				hint: "Keep this participant's address out of the session; -join only, never a succession candidate"},
			{names: []string{"relay"}, arg: "<addr>",
				hint: "Admit participants that cannot reach the host here and carry their traffic; -join only"},
			{names: []string{"size"}, arg: "<WxH>", hint: "Terminal-equivalent size for a run that has no terminal"},
			{names: []string{"probe"}, arg: "<addr>", hint: "Health and metrics endpoint; -serve only"},
			{names: []string{"first-join"}, arg: "<dur>", hint: "Exit if no guest connects within this; -serve only, 0 waits forever"},
			{names: []string{"empty"}, arg: "<dur>", hint: "Exit this long after the last guest leaves; -serve only, 0 keeps the session"},
			{names: []string{"drain"}, arg: "<dur>", hint: "How long a termination signal waits for an empty roster; -serve only, 0 exits at once"},
		},
	}, {
		title: "Configuration",
		lines: []flagLine{
			{names: []string{"d", "config-embedded"}, hint: "Use the embedded scenario and content; refuses -s and -f and ignores vif.toml's scenario and content"},
			{names: []string{"config-dir"}, arg: "<dir>", hint: "Configuration root holding scenario/ input/ audio/ content/ image/ bot/"},
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
			{names: []string{"g", "gui"}, arg: "[=false]", hint: "Open the game in a desktop window (Xlib, EGL, GLES 2 at run time; Linux, FreeBSD) instead of the terminal; the default of a vif-gui build or with vif.toml's [presentation] window = true, which -g=false turns off"},
			{names: []string{"cli"}, hint: "Play in the terminal whatever vif.toml's [presentation] or a vif-gui build says; wins over -g"},
			{names: []string{"maximized"}, arg: "[=false]", hint: "Open the desktop window maximized; vif.toml's [presentation] maximized is the default, and a terminal run ignores it"},
			{names: []string{"fullscreen"}, arg: "[=false]", hint: "Open the desktop window full screen; vif.toml's [presentation] fullscreen is the default, and a terminal run ignores it"},
			{names: []string{"scale"}, arg: "<n>", hint: "Draw the desktop window's font n times its pixels, for a HiDPI screen; with -g (default 1)"},
			{names: []string{"mute"}, arg: "[=false]", hint: "Start muted, or with sound as -mute=false; unset, vif.toml's [play] channels stand, sound on by default"},
			{names: []string{"ab", "audio-backend"}, arg: "<name>", hint: "Force an audio backend instead of detecting one"},
		},
	}, {
		title: "Run",
		lines: []flagLine{
			{names: []string{"nw", "no-wait"}, arg: "[=false]", hint: "Start each new game and seat each join at once, or at a key, click or tap as -nw=false; unset, vif.toml's [play] wait stands, waiting by default"},
			{names: []string{"seed"}, arg: "<n>", hint: "Root RNG seed; 0 draws one and logs it"},
			{names: []string{"speed"}, arg: "<rate>", hint: `Simulation rate 1/8 1/4 1/2 1 2 4 8; with -script or autonomous bots also "max" for no wall pacing`},
			{names: []string{"script"}, arg: "<path>", hint: "Run an authored deterministic TOML tick script"},
			{names: []string{"headless"}, hint: "With -bot, run bots without a human player or terminal; solo runs are unpaced. With -replay, verify the journal's digests unattended"},
			{names: []string{"watch"}, hint: "Watch the first bot or a script on this terminal, with no human player"},
			{names: []string{"r", "replay"}, arg: "<path>", hint: "Replay a recorded journal instead of playing"},
			{names: []string{"check"}, hint: "Validate the resolved settings, scenario, keymap, audio, content and bot graphs, then exit"},
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
			{names: []string{"mw", "music-wav"}, arg: "[=DIR]", hint: "Record the music alone, as it plays, to a WAV file; DIR overrides " + musicDir},
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
	{"input", "i"}, {"stat", "s"}, {"rec", "r"}, {"lock", "l"}, {"net", "n"},
	{"session", "m"}, {"converge", "c"}, {"journal", "j"}, {"domain", "o"}, {"tap", "t"},
}

// scopeNote is the one piece of grammar a hint cannot carry, so it is printed once
// rather than crammed into -ls.
func scopeNote() string {
	const indent, width = "            ", 80
	var names strings.Builder
	col := len(indent)
	for i, row := range scopeRows {
		cell := row.name + " (" + row.letter + ")"
		switch {
		case i == 0:
		case col+2+len(cell) > width:
			names.WriteString("\n" + indent)
			col = len(indent)
		default:
			names.WriteString("  ")
			col += 2
		}
		names.WriteString(cell)
		col += len(cell)
	}
	return "Scopes (-ls)\n" +
		"  Names     " + names.String() + "\n" +
		"  Sets      all is every scope, none is nothing\n" +
		`  Combine   join names or letters with +, a comma or a space — "app+fsm+stat" and "afs" are one set` + "\n" +
		"  Adjust    lead with + or - to add to or remove from the set already selected"
}

// summary names the program in the help and in the manual's NAME section.
const summary = "a modal-motion arcade game"

// writeUsage prints the whole help. The caller decides where: stdout and exit
// zero when it was asked for, stderr and a failing exit when the flags were wrong.
func writeUsage(w io.Writer) {
	fmt.Fprint(w, "vif — "+summary+"\n\nUsage:\n  vif [flags]\n")

	sections := helpSections(paths.DefaultLogDir(), paths.DefaultJournalDir(), paths.DefaultMusicDir())
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
	if strings.HasPrefix(l.arg, "[=") {
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

// generatedBy heads every file rendered from the help table and committed:
// TestGeneratedFilesAreTheHelpTable keeps each equal to its renderer.
const generatedBy = "Generated from cmd/vif/usage.go by TestGeneratedFilesAreTheHelpTable; do not edit."

// portableSections is the table as committed files print it: the XDG form of the
// state directories rather than the generating user's home.
func portableSections() []flagSection {
	dir := "$XDG_STATE_HOME/" + paths.AppDirName + "/"
	return helpSections(dir+paths.LogDirName, dir+paths.JournalDirName, dir+paths.MusicDirName)
}

// writeManual renders doc/vif.6.
func writeManual(w io.Writer) {
	fmt.Fprintf(w, `.\" %s
.TH VIF 6 "" vif
.SH NAME
vif \- %s
.SH SYNOPSIS
.B vif
.RI [ flags ]
.SH DESCRIPTION
Without flags,
.B vif
starts a solo game in the terminal, or in a desktop window when a vif-gui build
or the window key of vif.toml's [presentation] asks for one.
The flags below also host, join or serve a networked session,
replay or script a run, play a bot, and validate the installed configuration.
.PP
Each resource resolves from its own flag, then
.BR \-config\-dir ,
then the user configuration root, then the
.I wad
of the checkout whose
.I bin
holds the binary, then each system root,
and finally the copy compiled into the binary.
The first root holding a
.I vif.toml
supplies the defaults of the path flags, the audio buffer, the desktop window and
the [play] settings; a flag given on the command line wins.
.SH OPTIONS
`, generatedBy, roff(summary))
	for _, section := range portableSections() {
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
.SH IN-GAME CONFIGURATION
Ctrl-G or F10, :g (:config) or a press held on the status bar opens the
configuration menu, and F1 or :help the help. Select a category or setting with
Up/Down or j/k. Left/Right or h/l changes values; Enter/Space chooses. A press
selects a row, and on the selected row acts as Enter. Escape or q returns to
categories, then closes; Ctrl-G or F10 closes from any page. Home/End and
PageUp/PageDown navigate longer pages.
.PP
Game pauses until Pause is turned off again, as :pause (:p) does; in a session
only the host can, and every participant stops at the same tick. It also starts a
new game, as :n does, or another scenario: a row for each one the configuration
roots hold, or one given by name or path. In a session only the host can, and its
participants rejoin.
.PP
Bots adds local bots by count and policy graph, and removes this run's admitted
bots by cursor slot, or cancels a joining bot. Leaving bots show their state.
:bot lists this run's bots; :bot add [N[:graph]|graph] seats more.
Host before adding bots to solo play if remote players should join; otherwise
bots open a session only they can join.
.PP
Multiplayer hosts this run, joins an address/link, or requests a session from
an HTTP(S) vif-router site with a player count and scenario. Player count 0
and a blank scenario use server defaults. The request asks for no bots of the
session's own; the Bots page adds bots held by this run once it has joined.
Hosts can remove individual bots or players with all their bots
on the Multiplayer page. :player lists slots; :player drop <slot> removes a player
and its bots. :bot drop <slot> removes your bot, or any bot when hosting. Slots
accept decimal 10, hexadecimal A or 0xA for the cursor marked A.
Current session status is available on the same page.
Forms use Up/Down between fields, Left/Right and Home/End to move the text
cursor, Backspace/Delete to edit, Enter to submit, and Escape to cancel.
Session actions require Enter/Space; arrows only adjust settings.
Successful submissions resume play; asynchronous progress and failures appear
in the status bar. A failed join preserves the current run.
.PP
Audio controls sound effects, their volume and music. Controls adjusts mouse
input, pointer following and auto-fire. Display controls the telemetry HUD and
navigation overlays. Simulation changes solo speed and restarts a replay. Diagnostics controls logging, log level and
scope presets, snapshot interval, flight-recorder depth, FSM trigger and flush,
and the runtime profiler, and captures profiles, an execution trace, a telemetry
snapshot and a replay journal. Values in brackets are unavailable or read only; the
selected row explains why. Startup settings explains the relevant file or flag.
.PP
The Audio and Controls settings and the HUD are vif.toml's [play]: a run writes
each back as it changes, so the next run starts as this one was left: to the
vif.toml it read when that is in the user root or the \-config-dir root, else to
the user root's, created at the first change from the one read. Every other change
lasts the run. Solo play pauses while the menu is
open, and so may a host whose only other participants are its own bots; any other
session keeps running. The menu shares the command line's replay and session
restrictions. The keymap binds config_menu and help in normal_keys, text_keys and
overlay_keys; a key names one action, and an action can have several keys.
.SH FILES
.TP
.I $XDG_CONFIG_HOME/vif
User configuration root, normally
.IR ~/.config/vif .
.TP
.I vif.toml
Settings at the top of a configuration root: another root, the log, journal,
music, scenario, content and keymap paths, the audio mixer buffer, whether the
desktop window opens and how, the zoom Android and a browser keep, and the [play]
settings the menu writes back.
.TP
.I bot/<name>.toml
Bot graphs that \-bot and :bot add name; a path also works. The default
graph is compiled in and resolves last.
.TP
.IR /usr/local/etc/vif ", " /etc/vif
System configuration roots, in that order;
.B VIF_SYSTEM_ROOTS
names others in their place, separated by colons.
.TP
.I $XDG_STATE_HOME/vif
Logs, journals and music recordings, normally
.IR ~/.local/state/vif .
.SH SEE ALSO
https://lixen.com/vif/, https://github.com/lixenwraith/vif
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

// completion is what a flag's argument completes to, read from its arg column: an
// [=value] is no separate argument, and a free <word> such as <addr> offers nothing.
type completion int

const (
	completeNone completion = iota
	completeWord
	completeFile
	completeDir
	completeChoice // the |-separated words of arg
)

func (l flagLine) completion() completion {
	switch {
	case l.arg == "" || strings.HasPrefix(l.arg, "["):
		return completeNone
	case l.arg == "<dir>":
		return completeDir
	case strings.Contains(l.arg, "path"):
		return completeFile
	case !strings.HasPrefix(l.arg, "<"):
		return completeChoice
	}
	return completeWord
}

// forms is every name of the flag with its dash.
func (l flagLine) forms() []string {
	out := make([]string, len(l.names))
	for i, name := range l.names {
		out[i] = "-" + name
	}
	return out
}

func portableLines() []flagLine {
	var out []flagLine
	for _, section := range portableSections() {
		out = append(out, section.lines...)
	}
	return out
}

// writeBashCompletion renders deploy/package/vif.bash.
func writeBashCompletion(w io.Writer) {
	fmt.Fprintf(w, "# %s\n_vif() {\n\tlocal cur=${COMP_WORDS[COMP_CWORD]}\n\tCOMPREPLY=()\n", generatedBy)
	fmt.Fprint(w, "\tcase ${COMP_WORDS[COMP_CWORD-1]} in\n")
	var all []string
	for _, line := range portableLines() {
		all = append(all, line.forms()...)
		action := ""
		switch line.completion() {
		case completeNone:
			continue
		case completeFile:
			action = " compopt -o default"
		case completeDir:
			action = " compopt -o dirnames"
		case completeChoice:
			action = ` mapfile -t COMPREPLY < <(compgen -W '` + strings.ReplaceAll(line.arg, "|", " ") + `' -- "$cur")`
		}
		fmt.Fprintf(w, "\t%s)%s ;;\n", strings.Join(line.forms(), "|"), action)
	}
	fmt.Fprintf(w, "\t*) mapfile -t COMPREPLY < <(compgen -W '%s' -- \"$cur\") ;;\n", strings.Join(all, " "))
	fmt.Fprint(w, "\tesac\n}\ncomplete -F _vif vif\n")
}

// writeZshCompletion renders deploy/package/_vif; aliases exclude one another.
func writeZshCompletion(w io.Writer) {
	fmt.Fprintf(w, "#compdef vif\n# %s\n_arguments", generatedBy)
	quote := strings.NewReplacer(`'`, `'\''`, `[`, `\[`, `]`, `\]`)
	for _, line := range portableLines() {
		forms := line.forms()
		spec := "'" + forms[0]
		if len(forms) > 1 {
			spec = "'(" + strings.Join(forms, " ") + ")'{" + strings.Join(forms, ",") + "}'"
		}
		spec += "[" + quote.Replace(line.hint) + "]"
		switch line.completion() {
		case completeWord:
			spec += ":" + strings.Trim(line.arg, "<>") + ": "
		case completeFile:
			spec += ":path:_files"
		case completeDir:
			spec += ":dir:_files -/"
		case completeChoice:
			spec += ":value:(" + strings.ReplaceAll(line.arg, "|", " ") + ")"
		}
		fmt.Fprintf(w, " \\\n  %s'", spec)
	}
	fmt.Fprintln(w)
}

// writeFishCompletion renders deploy/package/vif.fish.
func writeFishCompletion(w io.Writer) {
	fmt.Fprintf(w, "# %s\ncomplete -c vif -f\n", generatedBy)
	quote := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	for _, line := range portableLines() {
		out := "complete -c vif -o " + strings.Join(line.names, " -o ")
		switch line.completion() {
		case completeWord:
			out += " -x"
		case completeFile:
			out += " -r -F"
		case completeDir:
			out += " -x -a '(__fish_complete_directories)'"
		case completeChoice:
			out += " -x -a '" + strings.ReplaceAll(line.arg, "|", " ") + "'"
		}
		fmt.Fprintf(w, "%s -d '%s'\n", out, quote.Replace(line.hint))
	}
}

// writeVersion prints what a downstream package and a bug report need; see
// asset.Version for where each comes from.
func writeVersion(w io.Writer) {
	version, revision := asset.Version()
	if revision != "" {
		fmt.Fprintf(w, "vif %s (%s)\n", version, revision)
		return
	}
	fmt.Fprintf(w, "vif %s\n", version)
}
