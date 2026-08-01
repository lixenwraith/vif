package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/app"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vlog"
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
)

func init() {
	// works: `-l`, `-lv info`, `-l=./tmp -lv trace`
	usage := "Enable logging; -l=DIR overrides " + parameter.LogDir
	flag.Var(&flagLog, "l", usage)
	flag.Var(&flagLog, "log", "Alias of -l")
	flag.Var(&flagLevel, "lv", "Log level: trace, debug, info, warn, error; implies -l")
}

// logFlag is a boolean flag that also accepts an optional directory.
// The space form (-l DIR) is not supported by the flag package; use -l=DIR.
type logFlag struct {
	dir string
	set bool
}

func (f *logFlag) String() string   { return f.dir }
func (f *logFlag) IsBoolFlag() bool { return true }

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

func main() {
	flag.Parse()

	logStatus := setupLogging()

	var err error
	switch {
	case *flagSchema:
		err = app.Schema(os.Stdout)
	case *flagCheck:
		err = app.Check(buildConfig(), os.Stdout)
	default:
		err = app.Run(buildConfig())
	}

	vlog.Shutdown(logShutdownTimeout)

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitFailure)
	}
	os.Exit(logStatus)
}

// setupLogging installs the crash hook and session defaults unconditionally,
// then starts a session if either log flag was given. The defaults are stored
// even when logging is off so ':log on' can start one later.
// Failure is non-fatal: the game runs unlogged and main exits exitLogSetup.
func setupLogging() int {
	core.SetCrashHook(vlog.CrashHook)

	dir := flagLog.dir
	if dir == "" {
		dir = parameter.LogDir
	}
	vlog.Configure(vlog.Config{
		Dir:   dir,
		Level: flagLevel.value,
		Spawn: core.Go, // processor panics reach HandleCrash, terminal restored
	})

	// -l is a boolean flag, so the space form leaves the path unparsed
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "ignoring arguments %v; use -l=DIR\n", flag.Args())
	}

	if !flagLog.set && !flagLevel.set {
		return 0
	}

	path, err := vlog.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging disabled: %v\n", err)
		return exitLogSetup
	}
	fmt.Printf("logging enabled: %s (level %s)\n", path, vlog.LevelName())
	return 0
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
