package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/app"
)

// CLI flags
var (
	flagColor256     = flag.Bool("cx", false, "Force 256-color mode")
	flagColorTrue    = flag.Bool("ct", false, "Force truecolor mode")
	flagAudioBackend = flag.String("ab", "", "Force audio backend by name")
	flagAudioMute    = flag.Bool("am", false, "Start with audio muted")
	flagAudioUnmute  = flag.Bool("au", false, "Start with audio unmuted")
	flagContentPath  = flag.String("f", "", "Content file path or glob pattern")
	flagGameScript   = flag.String("g", "", "Game config: game.toml path or map directory")
	flagGameDefault  = flag.Bool("gd", false, "Force embedded default FSM script")
	flagKeymapPath   = flag.String("k", "", "Keymap config file path (TOML)")
	flagCheck        = flag.Bool("check", false, "Validate FSM config and exit")
	flagSchema       = flag.Bool("schema", false, "Print FSM schema JSON and exit")
)

func main() {
	flag.Parse()

	var err error
	switch {
	case *flagSchema:
		err = app.Schema(os.Stdout)
	case *flagCheck:
		err = app.Check(buildConfig(), os.Stdout)
	default:
		err = app.Run(buildConfig())
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// buildConfig translates parsed flags into the runtime configuration
func buildConfig() app.Config {
	cfg := app.Config{
		AudioBackend: *flagAudioBackend,
		AudioMuted:   true, // default muted
		ContentPath:  *flagContentPath,
		GameScript:   *flagGameScript,
		ForceDefault: *flagGameDefault,
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
