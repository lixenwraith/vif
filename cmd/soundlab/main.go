package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// CLI flags, matching the game binary's house style.
var (
	flagBackend  = flag.String("ab", "", "Force audio backend by name (pacat, aplay, null, wav:out.wav, ...)")
	flagHeadless = flag.Bool("headless", false, "Discard audio output (equivalent to -ab null)")
	flagScript   = flag.String("s", "", "Execute script file and exit")
	flagSound    = flag.String("snd", "", "Sound TOML to load at startup")
	flagPat      = flag.String("pat", "", "Pattern TOML to load at startup")
	flagVol      = flag.Float64("vol", 0.7, "Master volume 0..1")
	flagTUI      = flag.Bool("tui", false, "Full-screen TUI (default: line REPL)")
)

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	backend := *flagBackend
	if *flagHeadless {
		backend = "null"
	}
	s, err := NewSession(backend, *flagVol, os.Stdout)
	if err != nil {
		return err
	}
	defer s.Close()
	if s.startErr != nil {
		fmt.Fprintf(s.out, "audio backend: %v (silent mode; edit/validate/export still work)\n", s.startErr)
	}

	if *flagSound != "" {
		if err := s.loadSoundFile(*flagSound, true); err != nil {
			return err
		}
	}
	if *flagPat != "" {
		if err := s.loadPatternFile(*flagPat, true); err != nil {
			return err
		}
	}

	if *flagScript != "" {
		f, err := os.Open(*flagScript)
		if err != nil {
			return err
		}
		defer f.Close()
		return runScript(s, f)
	}
	if *flagTUI {
		// runTUI owns terminal teardown; signals become a clean loop exit so
		// the deferred Fini always runs. The REPL's exit-on-signal handler
		// must not be installed alongside it.
		return runTUI(s)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; s.Close(); os.Exit(130) }()
	runREPL(s, os.Stdin)
	return nil
}
