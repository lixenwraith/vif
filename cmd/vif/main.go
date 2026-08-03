package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/app"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
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
	flagContentPath  = flag.String("f", "", "Content directory or single content file")
	flagGameScript   = flag.String("g", "", "Game config: game.toml path or map directory")
	flagDefault      = flag.Bool("d", false, "Force embedded FSM script and content, ignore -g and -f")
	flagKeymapPath   = flag.String("k", "", "Keymap config file path (TOML)")
	flagCheck        = flag.Bool("check", false, "Validate FSM and content config, then exit")
	flagSchema       = flag.Bool("schema", false, "Print FSM schema JSON and exit")

	flagLog   logFlag
	flagLevel levelFlag
	flagScope scopeFlag
	flagStat  statFlag
	flagDev   devFlag
)

func init() {
	// works: `-l`, `-lv info`, `-l=./tmp -lv trace -ls=afs -lt 1`
	usage := "Enable logging; -l=DIR overrides " + parameter.LogDir
	flag.Var(&flagLog, "l", usage)
	flag.Var(&flagLog, "log", "Alias of -l")
	flag.Var(&flagLevel, "lv", "Log level: trace, debug, info, warn, error; implies -l")
	flag.Var(&flagScope, "ls", "Log scope: app+fsm+stat | afs | all | none | +event | -lock; implies -l")
	flag.Var(&flagScope, "log-scope", "Alias of -ls")
	flag.Var(&flagStat, "lt", "Status snapshot period in game ticks, 0 disables; implies -l")
	flag.Var(&flagDev, "dev", "Capture runtime stderr to a file; defaults on for -race builds, -dev=false disables")
}

func main() {
	flag.Parse()

	logStatus := setupDiagnostics()

	var err error
	switch {
	case *flagSchema:
		err = app.Schema(os.Stdout)
	case *flagCheck:
		err = app.Check(buildConfig(), os.Stdout)
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

	dir := flagLog.dir
	if dir == "" {
		dir = parameter.LogDir
	}
	vlog.Configure(vlog.Config{
		Dir:   dir,
		Level: flagLevel.value,
		Scope: flagScope.value,
		Spawn: core.Go, // processor panics reach HandleCrash, terminal restored
	})

	// -l is a boolean flag, so the space form leaves the path unparsed
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "ignoring arguments %v; use -l=DIR\n", flag.Args())
	}

	status := 0
	if flagLog.set || flagLevel.set || flagScope.set || flagStat.set {
		path, err := vlog.Start()
		if err != nil {
			fmt.Fprintf(os.Stderr, "logging disabled: %v\n", err)
			status = exitLogSetup
		} else {
			fmt.Printf("logging enabled: %s (level %s, scope %s)\n",
				path, vlog.LevelName(), vlog.ScopeString(vlog.Scopes()))
		}
	}

	if flagDev.enabled() {
		path, err := core.CaptureStderr(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "runtime capture disabled: %v\n", err)
			return status
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
	return status
}

// shutdownDiagnostics drains what the runtime wrote, closes the logger, then
// hands fd 2 back so late runtime output reaches the restored terminal
func shutdownDiagnostics() {
	core.StopStderrDrain()
	core.DrainStderr(logRuntimeReport) // last blocks, while the sink lives

	vlog.Shutdown(logShutdownTimeout)

	if path := core.CloseCapture(); path != "" {
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
}

// buildConfig translates parsed flags into the runtime configuration
func buildConfig() app.Config {
	cfg := app.Config{
		AudioBackend: *flagAudioBackend,
		AudioMuted:   true, // default muted
		ContentPath:  *flagContentPath,
		GameScript:   *flagGameScript,
		ForceDefault: *flagDefault,
		KeymapPath:   *flagKeymapPath,
		LogScope:     flagScope.value,
		StatTicks:    flagStat.value,
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

// --- Flag types ---

// logFlag is a boolean flag that also accepts an optional directory.
// The space form (-l DIR) is not supported by the flag package; use -l=DIR.
type logFlag struct {
	dir string
	set bool
}

func (f *logFlag) String() string   { return f.dir }
func (f *logFlag) IsBoolFlag() bool { return true }

func (f *logFlag) Set(v string) error {
	switch v {
	case "false":
		f.set, f.dir = false, ""
	case "", "true":
		f.set = true
	default:
		f.set, f.dir = true, v
	}
	return nil
}

// levelFlag records that a level was given, so -lv alone enables logging
type levelFlag struct {
	value string
	set   bool
}

func (f *levelFlag) String() string { return f.value }
func (f *levelFlag) Set(v string) error {
	f.set, f.value = true, v
	return nil
}

// scopeFlag records a scope spec, so -ls alone enables logging
type scopeFlag struct {
	value string
	set   bool
}

func (f *scopeFlag) String() string { return f.value }
func (f *scopeFlag) Set(v string) error {
	if _, err := vlog.ParseScopes(v, vlog.ScopeAll); err != nil {
		return err
	}
	f.set, f.value = true, v
	return nil
}

// statFlag records a snapshot period, so -lt alone enables logging
type statFlag struct {
	value int
	set   bool
}

func (f *statFlag) String() string { return strconv.Itoa(f.value) }
func (f *statFlag) Set(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fmt.Errorf("must be a non-negative tick count")
	}
	f.set, f.value = true, n
	return nil
}

// devFlag is tri-state: unset defers to the build, -dev forces on, -dev=false off
type devFlag struct {
	value bool
	set   bool
}

func (f *devFlag) String() string   { return strconv.FormatBool(f.value) }
func (f *devFlag) IsBoolFlag() bool { return true }
func (f *devFlag) Set(v string) error {
	b, err := strconv.ParseBool(v)
	if err != nil {
		return err
	}
	f.set, f.value = true, b
	return nil
}

// enabled resolves capture: an explicit flag wins, otherwise race builds capture
func (f *devFlag) enabled() bool {
	if f.set {
		return f.value
	}
	return core.RaceEnabled
}
