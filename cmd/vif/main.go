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

// CLI flags
var (
	flagColor256     = flag.Bool("cx", false, "Force 256-color mode")
	flagColorTrue    = flag.Bool("ct", false, "Force truecolor mode")
	flagAudioBackend = flag.String("ab", "", "Force audio backend by name")
	flagAudioMute    = flag.Bool("am", false, "Start with audio muted")
	flagAudioUnmute  = flag.Bool("au", false, "Start with audio unmuted")
	flagCheck        = flag.Bool("check", false, "Validate resolved game, keymap, audio, and content config, then exit")
	flagSchema       = flag.Bool("schema", false, "Print FSM schema JSON and exit")
	flagSpeed        = flag.String("speed", "", "Simulation rate: 1/8 1/4 1/2 1 2 4 8; with -script also \"max\" for no wall pacing")
	flagSeed         = flag.Uint64("seed", 0, "Root RNG seed; 0 draws one and logs it")
	flagReplay       = flag.String("replay", "", "Replay a recorded journal file instead of playing")
	flagScript       = flag.String("script", "", "Run an authored deterministic TOML script")
	flagWatch        = flag.Bool("watch", false, "Present a -script run on this terminal instead of running it headlessly")

	flagConfig  = newConfigFlags()
	flagLogs    = newLogFlags()
	flagSession sessionFlags
	flagJournal = newSetFlag(true, parseOutputDirFlag)
	flagDev     = newSetFlag(true, parseBoolFlag)
)

func init() {
	flagConfig.register(flag.CommandLine)
	flagLogs.register(flag.CommandLine)
	flagSession.register(flag.CommandLine)
	flag.Var(&flagJournal, "j", "Record a replay journal; -j=DIR overrides the user-state directory")
	flag.Var(&flagJournal, "journal", "Alias of -j")
	flag.Var(&flagDev, "dev", "Capture runtime stderr to a file; defaults on for -race builds, -dev=false disables")
}

func main() {
	flag.Parse()

	logStatus := setupDiagnostics()

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
	os.Exit(logStatus)
}

// setupDiagnostics installs the crash hook and session defaults unconditionally,
// starts a log session if any log flag was given, and starts runtime capture if
// enabled. Runs before the terminal enters the alternate screen.
// Log failure is non-fatal: the game runs unlogged and main exits exitLogSetup.
func setupDiagnostics() int {
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
		Spawn:      core.Go, // processor panics reach HandleCrash, terminal restored
	})

	// -l and -j are boolean flags, so the space form leaves a path unparsed.
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "ignoring arguments %v; use -l=DIR or -j=DIR\n", flag.Args())
	}

	diagStatus := 0
	if flagLogs.enabled() {
		path, err := vlog.Start()
		if err != nil {
			fmt.Fprintf(os.Stderr, "logging disabled: %v\n", err)
			diagStatus = exitLogSetup
		} else {
			fmt.Printf("logging enabled: %s (level %s, scope %s)\n",
				path, vlog.LevelName(), vlog.ScopeString(vlog.Scopes()))
		}
	}

	if flagDev.valueOr(core.RaceEnabled) {
		path, err := core.CaptureStderr(logDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "runtime capture disabled: %v\n", err)
			return diagStatus
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
	return diagStatus
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
		AudioBackend:  *flagAudioBackend,
		AudioMuted:    true, // default muted
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
	}
	if flagSession.size != "" {
		cfg.Width, cfg.Height, _ = parseSize(flagSession.size) // validated in validateInvocation
	}

	if *flagAudioUnmute {
		cfg.AudioMuted = false
	} else if *flagAudioMute {
		cfg.AudioMuted = true
	}

	switch {
	case *flagColorTrue:
		cfg.ColorMode, cfg.ColorModeSet = terminal.ColorModeTrueColor, true
	case *flagColor256:
		cfg.ColorMode, cfg.ColorModeSet = terminal.ColorMode256, true
	}
	// Neither flag: terminal auto-detects

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
	fs.StringVar(&f.dir, "config-dir", "", "Configuration root (game/, input/, audio/, content/)")

	fs.StringVar(&f.game, "g", "", "Game config: game.toml path or map directory")
	fs.StringVar(&f.game, "config-game", "", "Alias of -g")
	fs.StringVar(&f.content, "f", "", "Content directory or single content file")
	fs.StringVar(&f.content, "config-content", "", "Alias of -f")
	fs.StringVar(&f.keymap, "k", "", "Keymap config file path (TOML)")
	fs.StringVar(&f.keymap, "config-keymap", "", "Alias of -k")
	fs.StringVar(&f.music, "config-music", "", "Music pattern override file (TOML)")
	fs.StringVar(&f.sounds, "config-sounds", "", "Sound definition override file (TOML)")

	usage := "Force embedded FSM script and content, ignoring -g and -f"
	fs.BoolVar(&f.embedded, "d", false, usage)
	fs.BoolVar(&f.embedded, "config-embedded", false, "Alias of -d")
}

// sessionFlags expose startup hosting/joining and the cap a later :host inherits.
type sessionFlags struct {
	host    string
	join    string
	serve   string
	size    string
	players int
}

func (f *sessionFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.host, "host", "", "Host a session on bind address, e.g. :7777")
	fs.StringVar(&f.join, "join", "", "Join a session at host:port")
	fs.StringVar(&f.serve, "serve", "", "Host a headless session with no local player, e.g. :7777")
	fs.StringVar(&f.size, "size", "", "Simulated terminal size WxH for a run that has no terminal of its own")
	fs.IntVar(&f.players, "players", 0, fmt.Sprintf(
		"Host lobby size, itself included (2..%d; default 2 with -host, max with later :host)", parameter.MaxPlayers))
}

func (f sessionFlags) validateInvocation(schema, check bool, replay string) error {
	if (f.host != "" || f.join != "" || f.serve != "" || f.players != 0) && (schema || check || replay != "") {
		return fmt.Errorf("-host, -join, -serve, and -players are available only in interactive play")
	}
	if f.players != 0 && f.join != "" {
		return fmt.Errorf("-players configures a host, not -join")
	}
	if f.serve != "" && (f.host != "" || f.join != "") {
		return fmt.Errorf("-serve is a host of its own; it does not combine with -host or -join")
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
	dir   setFlag[string]
	level setFlag[string]
	scope setFlag[string]
	stat  setFlag[int]
	rec   setFlag[int]
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
	usage := "Enable logging; -l=DIR overrides " + paths.DefaultLogDir()
	fs.Var(&f.dir, "l", usage)
	fs.Var(&f.dir, "log", "Alias of -l")
	fs.Var(&f.level, "lv", "Log level: trace, debug, info, warn, error; implies -l")
	fs.Var(&f.scope, "ls", "Log scope: app+fsm+stat | afs | all | none | +dispatch | -event; implies -l")
	fs.Var(&f.scope, "log-scope", "Alias of -ls")
	fs.Var(&f.stat, "lt", "Status snapshot period in game ticks, 0 disables; implies -l")
	fs.Var(&f.rec, "lr", "Flight recorder depth in game ticks, 0 disables; implies -l")
}

// enabled reports whether any logging flag was supplied.
func (f *logFlags) enabled() bool {
	return f.dir.set || f.level.set || f.scope.set || f.stat.set || f.rec.set
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
