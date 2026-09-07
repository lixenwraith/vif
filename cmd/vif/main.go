package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/app"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/lifecycle"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/paths"
	"github.com/lixenwraith/vi-fighter/internal/resource"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// Exit codes
const (
	exitFailure  = 1
	exitLogSetup = 73 // EX_CANTCREAT: logging requested but unavailable
)

const logShutdownTimeout = 2 * time.Second

// colourModes are the -color words. Auto is the default and means "ask the
// terminal", which is what every run that does not care wants.
const (
	colourAuto = "auto"
	colour256  = "256"
	colourTrue = "true"
)

// CLI flags. Every one of them is described in helpSections; the usage strings
// here are what `flag` prints on a parse error before that table is reachable, and
// are deliberately the same sentence.
var (
	flagColor  = flag.String("color", colourAuto, "Colour depth: auto, 256 or true")
	flagMute   = flag.Bool("mute", true, "Start muted; -mute=false starts with sound")
	flagCheck  = flag.Bool("check", false, "Validate the resolved config, then exit")
	flagSchema = flag.Bool("schema", false, "Print the FSM schema as JSON, then exit")
	flagSpeed  = flag.String("speed", "", "Simulation rate: 1/8 1/4 1/2 1 2 4 8, or max with -script")
	flagSeed   = flag.Uint64("seed", 0, "Root RNG seed; 0 draws one and logs it")
	flagReplay = flag.String("replay", "", "Replay a recorded journal instead of playing")
	flagScript = flag.String("script", "", "Run an authored deterministic TOML tick script")
	flagWatch  = flag.Bool("watch", false, "Present a -script run on this terminal")
	flagHelp   = flag.Bool("h", false, "Print the flag help and exit")

	flagAudioBackend string
	flagConfig       = newConfigFlags()
	flagLogs         = newLogFlags()
	flagSession      sessionFlags
	flagJournal      = newSetFlag(true, parseOutputDirFlag)
	flagDev          = newSetFlag(true, parseBoolFlag)
)

func init() {
	flagConfig.register(flag.CommandLine)
	flagLogs.register(flag.CommandLine)
	flagSession.register(flag.CommandLine)
	audioHint := "Force an audio backend instead of detecting one"
	flag.StringVar(&flagAudioBackend, "ab", "", audioHint)
	flag.StringVar(&flagAudioBackend, "audio-backend", "", audioHint)
	flag.BoolVar(flagHelp, "help", false, "Print the flag help and exit")
	journalHint := "Record a replay journal; -j=DIR overrides the user-state directory"
	flag.Var(&flagJournal, "j", journalHint)
	flag.Var(&flagJournal, "journal", journalHint)
	flag.Var(&flagDev, "dev", "Capture runtime stderr to a file; -dev=false disables")

	// The `flag` package writes its own usage to stderr and exits non-zero, which
	// is right for a mistake and wrong for a question. Asking is handled in main.
	flag.Usage = func() { writeUsage(flag.CommandLine.Output()) }
}

func main() {
	flag.Parse()
	if *flagHelp {
		// Asked for, so it is output rather than a diagnostic: stdout, exit zero,
		// greppable without redirecting stderr.
		writeUsage(os.Stdout)
		return
	}

	setupDiagnostics()

	var err error
	sessionErr := validateInvocation(*flagSchema, *flagCheck, *flagReplay, *flagScript, *flagWatch, flagSession)
	switch {
	case sessionErr != nil:
		err = sessionErr
	case *flagSchema:
		err = manifest.Schema(os.Stdout)
	case *flagCheck:
		err = resource.Check(buildConfig().Resources, os.Stdout)
	case *flagReplay != "":
		err = app.PlayJournal(*flagReplay)
	case *flagScript != "":
		cfg := buildConfig()
		if *flagWatch {
			cfg.Mode = app.ModeScript
		}
		_, err = app.RunScript(cfg, *flagScript)
	case flagSession.serve != "":
		err = app.RunServer(buildConfig())
	default:
		err = app.Run(buildConfig())
	}

	shutdownDiagnostics()

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitFailure)
	}
}

// setupDiagnostics installs the crash hook and session defaults unconditionally,
// starts a log session if any log flag was given, and starts runtime capture if
// enabled. Runs before the terminal enters the alternate screen.
//
// A log session that was asked for and could not start is fatal here rather than
// reported at exit: the run has no other way to say so, and a supervised process
// that plays a whole session unlogged has lost the record of whatever it was
// started to investigate.
func setupDiagnostics() {
	core.SetCrashHook(vlog.CrashHook)
	vlog.SetCrashFlush(status.CrashFlush) // drains while the sink is still live

	logDir := flagLogs.dir.value
	if logDir == "" {
		logDir = paths.DefaultLogDir()
	}
	journalDir := flagJournal.value
	if journalDir == "" {
		journalDir = paths.DefaultJournalDir()
	}
	vlog.Configure(vlog.Config{
		Dir:        logDir,
		JournalDir: journalDir,
		Level:      flagLogs.level.value,
		Scope:      flagLogs.scope.value,
		Console:    flagLogs.console,
		Spawn:      core.Go, // processor panics reach HandleCrash, terminal restored
	})

	// -l and -j are boolean flags, so the space form leaves a path unparsed.
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "ignoring arguments %v; use -l=DIR or -j=DIR\n", flag.Args())
	}

	if flagLogs.enabled() {
		path, err := vlog.Start()
		if err != nil {
			// Fatal rather than degraded. Logging was asked for, and a run that
			// cannot write it has no way to say so afterwards: the old behaviour
			// played the whole session unlogged and reported the failure at exit,
			// which for a supervised process is a silent one.
			fmt.Fprintf(os.Stderr, "logging unavailable: %v\n", err)
			os.Exit(exitLogSetup)
		}
		fmt.Printf("logging enabled: %s (level %s, scope %s)\n",
			path, vlog.LevelName(), vlog.ScopeString(vlog.Scopes()))
	}

	if flagDev.valueOr(core.RaceEnabled) {
		path, err := core.CaptureStderr(logDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "runtime capture disabled: %v\n", err)
			return
		}
		reason := "-dev"
		if !flagDev.set {
			reason = "race build"
		}
		fmt.Printf("runtime capture: %s (%s)\n", path, reason)
		vlog.Info("app", "msg", "runtime capture",
			"path", path, "reason", reason, "race", core.RaceEnabled)
		core.StartStderrDrain(parameter.DevDrainInterval, logRuntimeReport)
	}

}

// shutdownDiagnostics drains what the runtime wrote, closes the logger, then
// hands fd 2 back so late runtime output reaches the restored terminal
func shutdownDiagnostics() {
	core.StopStderrDrain()
	core.DrainStderr(logRuntimeReport) // last blocks, while the sink lives

	vlog.Shutdown(logShutdownTimeout)

	if path := vlog.LastJournalPath(); path != "" {
		fmt.Fprintf(os.Stderr, "replay journal: %s\n", path)
	}
	if path := core.CloseCapture(); path != "" && core.CaptureCount() > 0 {
		fmt.Fprintf(os.Stderr, "runtime output captured: %s (%d report(s))\n",
			path, core.CaptureCount())
	}
}

// logRuntimeReport records a pointer to one captured block
// Error level so the scope mask never filters a race or fatal report
func logRuntimeReport(r core.RuntimeReport) {
	vlog.Error("race", "msg", "runtime report",
		"kind", r.Kind,
		"path", r.Path,
		"offset", r.Offset,
		"bytes", r.Bytes,
		"lines", r.Lines,
		"head", r.Head,
		"at", r.At)
	status.Trigger(status.TrigRace)
}

// buildConfig translates parsed flags into the runtime configuration
func buildConfig() app.Config {
	cfg := app.Config{
		AudioBackend:  flagAudioBackend,
		AudioMuted:    true, // -mute defaults on
		Resources:     flagConfig.options(),
		LogScope:      flagLogs.scope.value,
		StatTicks:     flagLogs.stat.value,
		RecTicks:      flagLogs.rec.value,
		TimeScaleSpec: *flagSpeed,
		Seed:          *flagSeed,
		Journal:       flagJournal.set,
		HostAddress:   flagSession.host,
		JoinAddress:   flagSession.join,
		Participants:  flagSession.players,
	}

	if flagSession.serve != "" {
		cfg.HostAddress = flagSession.serve
		cfg.ProbeAddress = flagSession.probe
		cfg.Lifetime = flagSession.lifetime()
	}
	// The default differs by shape and the flag overrides either way: a dedicated
	// host *is* the session, so an orchestrator replacing it at the same address is
	// the reconnect its guests want; a person's machine is not, so there the
	// surviving guest continuing the game is worth more than the address staying put.
	cfg.FixedAuthority = flagSession.serve != ""
	if flagSession.authority != "" {
		cfg.FixedAuthority = flagSession.authority == authorityHost
	}
	if flagSession.size != "" {
		cfg.Width, cfg.Height, _ = parseSize(flagSession.size) // validated in validateInvocation
	}

	cfg.AudioMuted = *flagMute

	switch *flagColor {
	case colourTrue:
		cfg.ColorMode, cfg.ColorModeSet = terminal.ColorModeTrueColor, true
	case colour256:
		cfg.ColorMode, cfg.ColorModeSet = terminal.ColorMode256, true
	}
	// colourAuto leaves ColorModeSet false, which is the terminal deciding.

	return cfg
}

// configFlags groups every runtime file override. Short flags remain for
// compatibility; config-* aliases make the family discoverable in CLI help.
type configFlags struct {
	dir      string
	game     string
	content  string
	keymap   string
	music    string
	sounds   string
	embedded bool
}

func newConfigFlags() *configFlags { return &configFlags{} }

// options is the resolved resource override set these flags describe.
func (f *configFlags) options() resource.Options {
	return resource.Options{
		Dir:      f.dir,
		Game:     f.game,
		Content:  f.content,
		Keymap:   f.keymap,
		Music:    f.music,
		Sounds:   f.sounds,
		Embedded: f.embedded,
	}
}

func (f *configFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.dir, "config-dir", "", "Configuration root holding game/ input/ audio/ content/")
	fs.StringVar(&f.music, "config-music", "", "Music pattern override TOML")
	fs.StringVar(&f.sounds, "config-sounds", "", "Sound definition override TOML")

	for _, alias := range []struct {
		short, long, hint string
		into              *string
	}{
		{"g", "config-game", "game.toml, or a map directory", &f.game},
		{"f", "config-content", "Content directory, or a single content file", &f.content},
		{"k", "config-keymap", "Keymap TOML", &f.keymap},
	} {
		fs.StringVar(alias.into, alias.short, "", alias.hint)
		fs.StringVar(alias.into, alias.long, "", alias.hint)
	}

	embedded := "Use the embedded FSM and content, ignoring -g and -f"
	fs.BoolVar(&f.embedded, "d", false, embedded)
	fs.BoolVar(&f.embedded, "config-embedded", false, embedded)
}

// sessionFlags expose startup hosting/joining and the cap a later :host inherits.
type sessionFlags struct {
	host      string
	join      string
	serve     string
	probe     string
	size      string
	players   int
	authority string

	// firstJoin, empty and drain bound an allocated session's life. They are zero
	// on an interactively started host, which is supervised by the person who
	// started it, and set by a deployment whose sessions are created on a player's
	// behalf and have nobody to notice that nobody came.
	firstJoin time.Duration
	empty     time.Duration
	drain     time.Duration
}

// lifetime is the policy these flags describe.
func (f sessionFlags) lifetime() lifecycle.Policy {
	return lifecycle.Policy{FirstJoin: f.firstJoin, Empty: f.empty, Drain: f.drain}
}

func (f *sessionFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.host, "host", "", "Host a session on bind address, e.g. :7777")
	fs.StringVar(&f.join, "join", "", "Join a session at host:port")
	fs.StringVar(&f.serve, "serve", "", "Host a headless session with no local player, e.g. :7777")
	fs.StringVar(&f.probe, "probe", "", "Serve liveness, readiness and metrics for a -serve run, e.g. :7788")
	fs.StringVar(&f.size, "size", "", "Simulated terminal size WxH for a run that has no terminal of its own")
	fs.DurationVar(&f.firstJoin, "first-join", 0,
		"With -serve, exit if no guest has connected within this duration, e.g. 90s; 0 waits forever")
	fs.DurationVar(&f.empty, "empty", 0,
		"With -serve, exit this long after the last guest leaves, e.g. 90s; 0 keeps the session")
	fs.DurationVar(&f.drain, "drain", 0,
		"With -serve, how long a termination signal waits for the roster to empty before exiting anyway; 0 exits at once")
	fs.IntVar(&f.players, "players", 0, fmt.Sprintf(
		"Ceiling on the roster, itself included (2..%d; default the whole roster). "+
			"With -host it also sizes the startup lobby, which then waits for exactly that "+
			"many; unset, a host starts on its first guest and admits the rest as they arrive",
		parameter.MaxPlayers))
	fs.StringVar(&f.authority, "authority", "", fmt.Sprintf(
		"What losing the authoring participant does: %q hands the session to the "+
			"roster's next survivor, %q ends it and leaves every survivor playing alone. "+
			"Default %q with -serve and %q otherwise",
		authorityMigrate, authorityHost, authorityHost, authorityMigrate))
}

// authorityMigrate and authorityHost are the two -authority words. They name the
// question the flag answers — where authorship lives when the participant holding
// it goes — rather than a mechanism, because the mechanism is the part that may
// change.
const (
	authorityMigrate = "migrate"
	authorityHost    = "host"
)

func (f sessionFlags) validateInvocation(schema, check bool, replay string) error {
	if f.authority != "" && f.authority != authorityMigrate && f.authority != authorityHost {
		return fmt.Errorf("-authority %q is not %q or %q", f.authority, authorityMigrate, authorityHost)
	}
	if f.authority != "" && f.host == "" && f.serve == "" {
		return fmt.Errorf("-authority is the policy a host sets for its session; a guest adopts the one it is offered")
	}
	if (f.host != "" || f.join != "" || f.serve != "" || f.probe != "" || f.players != 0 ||
		f.authority != "" || f.lifetime().Bounded()) && (schema || check || replay != "") {
		return fmt.Errorf("-host, -join, -serve, -probe, -players, -authority and the session lifetime bounds are available only in interactive play")
	}
	if f.players != 0 && f.join != "" {
		return fmt.Errorf("-players configures a host, not -join")
	}
	if f.serve != "" && (f.host != "" || f.join != "") {
		return fmt.Errorf("-serve is a host of its own; it does not combine with -host or -join")
	}
	if f.probe != "" && f.serve == "" {
		return fmt.Errorf("-probe answers for a -serve run; nothing else has a supervisor to answer")
	}
	if f.serve == "" && f.lifetime().Bounded() {
		return fmt.Errorf("-first-join, -empty and -drain bound an allocated -serve session; an interactive run is ended by its operator")
	}
	if err := f.lifetime().Validate(); err != nil {
		return err
	}
	if f.size != "" {
		if _, _, err := parseSize(f.size); err != nil {
			return err
		}
	}
	return nil
}

// parseSize reads a WxH geometry for a run that derives none from a terminal.
func parseSize(spec string) (width, height int, err error) {
	w, h, ok := strings.Cut(spec, "x")
	if !ok {
		return 0, 0, fmt.Errorf("-size %q is not WxH, for example 120x40", spec)
	}
	if width, err = strconv.Atoi(w); err != nil || width <= 0 {
		return 0, 0, fmt.Errorf("-size %q has no usable width", spec)
	}
	if height, err = strconv.Atoi(h); err != nil || height <= 0 {
		return 0, 0, fmt.Errorf("-size %q has no usable height", spec)
	}
	return width, height, nil
}

func validateInvocation(schema, check bool, replay, script string, watch bool, session sessionFlags) error {
	modes := 0
	for _, selected := range []bool{schema, check, replay != "", script != ""} {
		if selected {
			modes++
		}
	}
	if modes > 1 {
		return fmt.Errorf("-schema, -check, -replay, and -script are mutually exclusive")
	}
	if watch && script == "" {
		return fmt.Errorf("-watch presents a -script run and has no other subject")
	}
	return session.validateInvocation(schema, check, replay)
}

// --- Flag types ---

type flagParser[T any] func(string, T) (value T, set bool, err error)

// setFlag records whether a parsed value was explicitly supplied.
type setFlag[T any] struct {
	value   T
	set     bool
	boolean bool
	parse   flagParser[T]
}

func newSetFlag[T any](boolean bool, parse flagParser[T]) setFlag[T] {
	return setFlag[T]{boolean: boolean, parse: parse}
}

func (f *setFlag[T]) String() string   { return fmt.Sprint(f.value) }
func (f *setFlag[T]) IsBoolFlag() bool { return f.boolean }

func (f *setFlag[T]) Set(s string) error {
	value, set, err := f.parse(s, f.value)
	if err != nil {
		return err
	}
	f.value, f.set = value, set
	return nil
}

// valueOr returns an explicit value or the supplied default.
func (f *setFlag[T]) valueOr(fallback T) T {
	if f.set {
		return f.value
	}
	return fallback
}

type logFlags struct {
	dir     setFlag[string]
	level   setFlag[string]
	scope   setFlag[string]
	stat    setFlag[int]
	rec     setFlag[int]
	console bool
}

func newLogFlags() *logFlags {
	return &logFlags{
		dir:   newSetFlag(true, parseOutputDirFlag),
		level: newSetFlag(false, parseStringFlag),
		scope: newSetFlag(false, parseScopeFlag),
		stat:  newSetFlag(false, parseTicksFlag),
		rec:   newSetFlag(false, parseTicksFlag),
	}
}

// register installs the logging flags and their aliases.
func (f *logFlags) register(fs *flag.FlagSet) {
	for _, alias := range []struct {
		short, long, hint string
		value             flag.Value
	}{
		{"l", "log", "Enable logging; -l=DIR overrides " + paths.DefaultLogDir(), &f.dir},
		{"lv", "log-level", "Log level: trace, debug, info, warn or error; implies -l", &f.level},
		{"ls", "log-scope", "Which subsystems log; see the Scopes note in -h; implies -l", &f.scope},
		{"lt", "log-stat", "Status snapshot period in game ticks, 0 disables; implies -l", &f.stat},
		{"lr", "log-recorder", "Flight recorder depth in game ticks, 0 disables; implies -l", &f.rec},
	} {
		fs.Var(alias.value, alias.short, alias.hint)
		fs.Var(alias.value, alias.long, alias.hint)
	}
	fs.BoolVar(&f.console, "log-stdout", false,
		"Write the log to stdout as JSON instead of to a file; implies -l")
}

// enabled reports whether any logging flag was supplied.
func (f *logFlags) enabled() bool {
	return f.dir.set || f.level.set || f.scope.set || f.stat.set || f.rec.set || f.console
}

// parseOutputDirFlag keeps -l and -j boolean while accepting -l=DIR/-j=DIR.
func parseOutputDirFlag(s, current string) (string, bool, error) {
	switch s {
	case "false":
		return "", false, nil
	case "", "true":
		return current, true, nil
	default:
		return s, true, nil
	}
}

func parseStringFlag(s, _ string) (string, bool, error) {
	return s, true, nil
}

func parseScopeFlag(s, _ string) (string, bool, error) {
	if _, err := vlog.ParseScopes(s, vlog.ScopeAll); err != nil {
		return "", false, err
	}
	return s, true, nil
}

// parseTicksFlag maps an explicit zero to the runtime disable sentinel.
func parseTicksFlag(s string, _ int) (int, bool, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false, fmt.Errorf("must be a non-negative tick count")
	}
	if n == 0 {
		n = -1
	}
	return n, true, nil
}

func parseBoolFlag(s string, _ bool) (bool, bool, error) {
	b, err := strconv.ParseBool(s)
	return b, err == nil, err
}
